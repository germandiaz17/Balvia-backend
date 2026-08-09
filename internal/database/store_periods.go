package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// ClosePeriodResult holds the artifacts produced by closing a tracking period.
type ClosePeriodResult struct {
	Closed        sqlc.TrackingPeriod
	Next          sqlc.TrackingPeriod
	Summary       sqlc.TrackingPeriodSummary
	BudgetsCopied int
	Insights      []sqlc.TrackingPeriodInsight
}

// ClosePeriodTx implements Store. In a single transaction it:
//  1. marks the period closed (only if currently active),
//  2. snapshots a summary (core financial aggregates + JSONB breakdowns),
//  3. generates the next contiguous period, shaped by the user's current mode
//     (rolling block or calendar month — see domain.NextPeriodRange),
//  4. copies the closed period's budgets onto the new period, prorated if
//     exactly one of the two is a transition bridge,
//  5. generates all 9 "final" insights (idempotent: skipped if already exist).
func (s *SQLStore) ClosePeriodTx(ctx context.Context, periodID, userID uuid.UUID) (ClosePeriodResult, error) {
	var res ClosePeriodResult

	err := s.execTx(ctx, func(q *sqlc.Queries) error {
		closed, err := q.ClosePeriod(ctx, periodID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound // not found or already closed
			}
			return fmt.Errorf("close period: %w", err)
		}
		res.Closed = closed

		// ── 1. Build summary ──────────────────────────────────────────────
		summary, err := buildSummary(ctx, q, closed, userID)
		if err != nil {
			return err
		}
		res.Summary = summary

		// ── 2. Generate next period ───────────────────────────────────────
		settings, err := q.GetUserSettingsByUserID(ctx, userID)
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}

		// The mode and duration are read here, at close time, so a settings
		// change only ever shapes the period being born — never the one the user
		// just lived through. See UserSettingsService.
		rng := domain.NextPeriodRange(
			closed.EndDate.Time,
			settings.TrackingPeriodMode,
			int(settings.TrackingDurationDays),
		)
		next, err := q.CreateTrackingPeriod(ctx, sqlc.CreateTrackingPeriodParams{
			UserID:             userID,
			StartDate:          pgtype.Date{Time: rng.Start, Valid: true},
			EndDate:            pgtype.Date{Time: rng.End, Valid: true},
			Status:             "active",
			SequenceNumber:     closed.SequenceNumber + 1,
			ConfigStartDay:     settings.TrackingStartDay,
			ConfigDurationDays: settings.TrackingDurationDays,
			ConfigPeriodMode:   settings.TrackingPeriodMode,
			IsTransition:       rng.IsTransition,
		})
		if err != nil {
			return fmt.Errorf("create next period: %w", err)
		}
		res.Next = next

		// ── 3. Copy budgets ───────────────────────────────────────────────
		budgets, err := q.ListBudgetsByPeriod(ctx, periodID)
		if err != nil {
			return fmt.Errorf("list budgets: %w", err)
		}
		proration := budgetProrationFactor(closed, next)
		for _, b := range budgets {
			if _, err := q.CreateBudget(ctx, sqlc.CreateBudgetParams{
				UserID:                 userID,
				TrackingPeriodID:       next.ID,
				CategoryID:             b.CategoryID,
				Amount:                 prorateAmount(b.Amount, proration),
				Currency:               b.Currency,
				AlertThresholdWarning:  b.AlertThresholdWarning,
				AlertThresholdCritical: b.AlertThresholdCritical,
				Notes:                  b.Notes,
			}); err != nil {
				return fmt.Errorf("copy budget: %w", err)
			}
		}
		res.BudgetsCopied = len(budgets)

		// ── 4. Clean up "during" insights before generating "final" ones ────
		// "during" insights are ephemeral; once the period closes they become
		// stale. Deleting them keeps the stored set coherent: the closed period
		// will only ever return "final" insights from this point on.
		if err := q.DeleteDuringInsightsByPeriod(ctx, sqlc.DeleteDuringInsightsByPeriodParams{
			TrackingPeriodID: periodID,
			UserID:           userID,
		}); err != nil {
			return fmt.Errorf("delete during insights on close: %w", err)
		}

		// ── 5. Generate final insights (idempotent) ───────────────────────
		existingCount, err := q.CountFinalInsightsByPeriod(ctx, periodID)
		if err != nil {
			return fmt.Errorf("count final insights: %w", err)
		}
		if existingCount > 0 {
			// Insights already generated (e.g. retry after partial failure).
			return nil
		}

		data, err := collectClosePeriodData(ctx, q, closed, userID, budgets)
		if err != nil {
			return err
		}

		drafts := generateFinalInsights(data)
		for _, draft := range drafts {
			insight, err := q.CreateTrackingPeriodInsight(ctx, sqlc.CreateTrackingPeriodInsightParams{
				TrackingPeriodID:  periodID,
				UserID:            userID,
				InsightType:       draft.InsightType,
				CalculationPhase:  draft.CalculationPhase,
				Severity:          draft.Severity,
				Title:             draft.Title,
				Message:           draft.Message,
				ActionLabel:       draft.ActionLabel,
				ActionTarget:      draft.ActionTarget,
				Data:              draft.Data,
				RelatedCategoryID: draft.RelatedCategoryID,
				RelatedAccountID:  draft.RelatedAccountID,
				RelatedGoalID:     draft.RelatedGoalID,
				ValidUntil:        draft.ValidUntil,
			})
			if err != nil {
				return fmt.Errorf("create insight %s: %w", draft.InsightType, err)
			}
			res.Insights = append(res.Insights, insight)
		}

		return nil
	})

	return res, err
}

// collectClosePeriodData gathers all the data required by the insight
// generators in a single pass within the existing transaction.
func collectClosePeriodData(
	ctx context.Context,
	q *sqlc.Queries,
	period sqlc.TrackingPeriod,
	userID uuid.UUID,
	budgets []sqlc.Budget,
) (closePeriodData, error) {
	data := closePeriodData{
		Period:  period,
		Budgets: budgets,
	}

	totals, err := q.SummarizePeriodTotals(ctx, period.ID)
	if err != nil {
		return data, fmt.Errorf("totals for insights: %w", err)
	}
	data.Totals = totals

	expByCategory, err := q.ExpenseByCategory(ctx, period.ID)
	if err != nil {
		return data, fmt.Errorf("expense by category: %w", err)
	}
	data.ExpByCategory = expByCategory

	incByCategory, err := q.IncomeByCategory(ctx, period.ID)
	if err != nil {
		return data, fmt.Errorf("income by category: %w", err)
	}
	data.IncByCategory = incByCategory

	expByAccount, err := q.ExpenseByAccount(ctx, period.ID)
	if err != nil {
		return data, fmt.Errorf("expense by account: %w", err)
	}
	data.ExpByAccount = expByAccount

	expByDay, err := q.ExpenseByDay(ctx, period.ID)
	if err != nil {
		return data, fmt.Errorf("expense by day: %w", err)
	}
	data.ExpByDay = expByDay

	topMerchants, err := q.TopMerchants(ctx, period.ID)
	if err != nil {
		return data, fmt.Errorf("top merchants: %w", err)
	}
	data.TopMerchants = topMerchants

	goalContribsTotal, err := q.GoalContributionsTotalForPeriod(ctx, period.ID)
	if err != nil {
		return data, fmt.Errorf("goal contributions total: %w", err)
	}
	data.GoalContribsTotal = goalContribsTotal

	// Previous period summary (optional — used for vs_previous_final).
	// pgx.ErrNoRows is expected for period #1 or when the previous period has
	// no summary; in that case PrevSummary stays nil and the generator skips.
	//
	// It is also left nil when either side is a transition bridge: the insight
	// compares raw totals with no per-day normalisation, so measuring a 20-day
	// bridge against a full month would announce a spending drop the user never
	// made. Skipping beats lying.
	prevPeriod, err := q.GetPreviousTrackingPeriod(ctx, sqlc.GetPreviousTrackingPeriodParams{
		UserID:         userID,
		SequenceNumber: period.SequenceNumber,
	})
	switch {
	case err == nil:
		if period.IsTransition || prevPeriod.IsTransition {
			break
		}
		prevSummary, err := q.GetTrackingPeriodSummaryForPeriod(ctx, prevPeriod.ID)
		switch {
		case err == nil:
			data.PrevSummary = &prevSummary
		case !errors.Is(err, pgx.ErrNoRows):
			return data, fmt.Errorf("previous period summary: %w", err)
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return data, fmt.Errorf("previous period: %w", err)
	}

	return data, nil
}

// buildSummary computes the core financial snapshot + JSONB breakdowns for a
// closed period, creating (and immediately updating) the summary row.
func buildSummary(ctx context.Context, q *sqlc.Queries, period sqlc.TrackingPeriod, userID uuid.UUID) (sqlc.TrackingPeriodSummary, error) {
	totals, err := q.SummarizePeriodTotals(ctx, period.ID)
	if err != nil {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("summarize totals: %w", err)
	}

	netSavings := totals.TotalIncome.Sub(totals.TotalExpenses)

	var savingsRate decimal.NullDecimal
	if totals.TotalIncome.IsPositive() {
		rate := netSavings.Div(totals.TotalIncome).Mul(decimal.NewFromInt(100)).Round(2)
		savingsRate = decimal.NullDecimal{Decimal: rate, Valid: true}
	}

	var topCatID uuid.NullUUID
	var topCatAmt decimal.NullDecimal
	if top, err := q.GetTopExpenseCategory(ctx, period.ID); err == nil {
		topCatID = top.CategoryID
		topCatAmt = decimal.NullDecimal{Decimal: top.Total, Valid: true}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("top expense category: %w", err)
	}

	summary, err := q.CreateTrackingPeriodSummary(ctx, sqlc.CreateTrackingPeriodSummaryParams{
		TrackingPeriodID:         period.ID,
		UserID:                   userID,
		TotalIncome:              totals.TotalIncome,
		TotalExpenses:            totals.TotalExpenses,
		TotalTransfers:           totals.TotalTransfers,
		NetSavings:               netSavings,
		SavingsRate:              savingsRate,
		TransactionCount:         totals.TransactionCount,
		ExpenseTransactionCount:  totals.ExpenseTransactionCount,
		IncomeTransactionCount:   totals.IncomeTransactionCount,
		TopExpenseCategoryID:     topCatID,
		TopExpenseCategoryAmount: topCatAmt,
	})
	if err != nil {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("create summary: %w", err)
	}

	// ── Compute JSONB breakdowns ──────────────────────────────────────────
	// Errors must propagate: a failed query aborts the surrounding Postgres
	// transaction, so continuing would only fail later with a confusing error.
	expByCategory, err := q.ExpenseByCategory(ctx, period.ID)
	if err != nil {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("expense by category: %w", err)
	}
	incByCategory, err := q.IncomeByCategory(ctx, period.ID)
	if err != nil {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("income by category: %w", err)
	}
	expByAccount, err := q.ExpenseByAccount(ctx, period.ID)
	if err != nil {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("expense by account: %w", err)
	}
	expByDay, err := q.ExpenseByDay(ctx, period.ID)
	if err != nil {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("expense by day: %w", err)
	}
	goalContribsTotal, err := q.GoalContributionsTotalForPeriod(ctx, period.ID)
	if err != nil {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("goal contributions total: %w", err)
	}

	// Budget performance breakdown
	budgets, err := q.ListBudgetsByPeriod(ctx, period.ID)
	if err != nil {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("list budgets: %w", err)
	}
	budgetPerformance := buildBudgetPerformance(expByCategory, budgets, totals.TotalExpenses)

	// vs_previous_period comparison. pgx.ErrNoRows is expected for period #1
	// (no previous period) and when the previous period has no summary yet.
	var vsPreviousPeriod []byte
	prevPeriod, err := q.GetPreviousTrackingPeriod(ctx, sqlc.GetPreviousTrackingPeriodParams{
		UserID:         userID,
		SequenceNumber: period.SequenceNumber,
	})
	switch {
	case err == nil:
		prevSummary, err := q.GetTrackingPeriodSummaryForPeriod(ctx, prevPeriod.ID)
		switch {
		case err == nil:
			vsPreviousPeriod, _ = buildVsPreviousPeriod(totals, prevSummary)
		case !errors.Is(err, pgx.ErrNoRows):
			return sqlc.TrackingPeriodSummary{}, fmt.Errorf("previous period summary: %w", err)
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("previous period: %w", err)
	}

	// Serialize breakdowns to JSON
	expByCategoryJSON, _ := marshalExpenseByCategory(expByCategory)
	incByCategoryJSON, _ := marshalIncomeByCategory(incByCategory)
	expByAccountJSON, _ := marshalExpenseByAccount(expByAccount)
	expByDayJSON, _ := marshalExpenseByDay(expByDay)
	budgetPerfJSON, _ := json.Marshal(budgetPerformance)

	updated, err := q.UpdateTrackingPeriodSummaryBreakdowns(ctx, sqlc.UpdateTrackingPeriodSummaryBreakdownsParams{
		ID:                     summary.ID,
		ExpenseByCategory:      expByCategoryJSON,
		IncomeByCategory:       incByCategoryJSON,
		ExpenseByAccount:       expByAccountJSON,
		ExpenseByDay:           expByDayJSON,
		BudgetPerformance:      budgetPerfJSON,
		GoalContributionsTotal: goalContribsTotal,
		VsPreviousPeriod:       vsPreviousPeriod,
	})
	if err != nil {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("update summary breakdowns: %w", err)
	}

	return updated, nil
}

// ── JSON marshalling helpers ──────────────────────────────────────────────────

type expByCategoryEntry struct {
	CategoryID   *string `json:"category_id"`
	CategoryName *string `json:"category_name"`
	Total        string  `json:"total"`
	TxnCount     int32   `json:"txn_count"`
}

func marshalExpenseByCategory(rows []sqlc.ExpenseByCategoryRow) ([]byte, error) {
	entries := make([]expByCategoryEntry, 0, len(rows))
	for _, row := range rows {
		var catID *string
		if row.CategoryID.Valid {
			s := row.CategoryID.UUID.String()
			catID = &s
		}
		entries = append(entries, expByCategoryEntry{
			CategoryID:   catID,
			CategoryName: row.CategoryName,
			Total:        row.Total.String(),
			TxnCount:     row.TxnCount,
		})
	}
	return json.Marshal(entries)
}

type incByCategoryEntry struct {
	CategoryID   *string `json:"category_id"`
	CategoryName *string `json:"category_name"`
	Total        string  `json:"total"`
	TxnCount     int32   `json:"txn_count"`
}

func marshalIncomeByCategory(rows []sqlc.IncomeByCategoryRow) ([]byte, error) {
	entries := make([]incByCategoryEntry, 0, len(rows))
	for _, row := range rows {
		var catID *string
		if row.CategoryID.Valid {
			s := row.CategoryID.UUID.String()
			catID = &s
		}
		entries = append(entries, incByCategoryEntry{
			CategoryID:   catID,
			CategoryName: row.CategoryName,
			Total:        row.Total.String(),
			TxnCount:     row.TxnCount,
		})
	}
	return json.Marshal(entries)
}

type expByAccountEntry struct {
	AccountID   string `json:"account_id"`
	AccountName string `json:"account_name"`
	Total       string `json:"total"`
	TxnCount    int32  `json:"txn_count"`
}

func marshalExpenseByAccount(rows []sqlc.ExpenseByAccountRow) ([]byte, error) {
	entries := make([]expByAccountEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, expByAccountEntry{
			AccountID:   row.AccountID.String(),
			AccountName: row.AccountName,
			Total:       row.Total.String(),
			TxnCount:    row.TxnCount,
		})
	}
	return json.Marshal(entries)
}

type expByDayEntry struct {
	Date     string `json:"date"`
	Total    string `json:"total"`
	TxnCount int32  `json:"txn_count"`
}

func marshalExpenseByDay(rows []sqlc.ExpenseByDayRow) ([]byte, error) {
	entries := make([]expByDayEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, expByDayEntry{
			Date:     row.TransactionDate.Time.Format("2006-01-02"),
			Total:    row.Total.String(),
			TxnCount: row.TxnCount,
		})
	}
	return json.Marshal(entries)
}

type budgetPerfEntry struct {
	BudgetID      string  `json:"budget_id"`
	CategoryID    *string `json:"category_id"`
	BudgetAmount  string  `json:"budget_amount"`
	SpentAmount   string  `json:"spent_amount"`
	CompliancePct string  `json:"compliance_pct"`
	Exceeded      bool    `json:"exceeded"`
}

func buildBudgetPerformance(
	expByCategory []sqlc.ExpenseByCategoryRow,
	budgets []sqlc.Budget,
	totalExpenses decimal.Decimal,
) []budgetPerfEntry {
	// build category→spend lookup
	catSpend := make(map[uuid.UUID]decimal.Decimal)
	for _, row := range expByCategory {
		if row.CategoryID.Valid {
			catSpend[row.CategoryID.UUID] = row.Total
		}
	}

	entries := make([]budgetPerfEntry, 0, len(budgets))
	for _, b := range budgets {
		var spent decimal.Decimal
		if b.CategoryID.Valid {
			spent = catSpend[b.CategoryID.UUID]
		} else {
			spent = totalExpenses
		}
		compliancePct := decimal.Zero
		if b.Amount.IsPositive() {
			compliancePct = spent.Div(b.Amount).Mul(decimal.NewFromInt(100)).Round(1)
		}
		var catIDStr *string
		if b.CategoryID.Valid {
			s := b.CategoryID.UUID.String()
			catIDStr = &s
		}
		entries = append(entries, budgetPerfEntry{
			BudgetID:      b.ID.String(),
			CategoryID:    catIDStr,
			BudgetAmount:  b.Amount.String(),
			SpentAmount:   spent.String(),
			CompliancePct: compliancePct.String(),
			Exceeded:      spent.GreaterThan(b.Amount),
		})
	}
	return entries
}

type vsPreviousEntry struct {
	CurrentIncome      string `json:"current_income"`
	CurrentExpenses    string `json:"current_expenses"`
	CurrentNetSavings  string `json:"current_net_savings"`
	PreviousIncome     string `json:"previous_income"`
	PreviousExpenses   string `json:"previous_expenses"`
	PreviousNetSavings string `json:"previous_net_savings"`
	DeltaExpenses      string `json:"delta_expenses"`
	DeltaIncome        string `json:"delta_income"`
	ExpenseChangePct   string `json:"expense_change_pct"`
	PreviousPeriodID   string `json:"previous_period_id"`
}

func buildVsPreviousPeriod(totals sqlc.SummarizePeriodTotalsRow, prev sqlc.TrackingPeriodSummary) ([]byte, error) {
	currentNet := totals.TotalIncome.Sub(totals.TotalExpenses)
	prevNet := prev.TotalIncome.Sub(prev.TotalExpenses)
	deltaExpenses := totals.TotalExpenses.Sub(prev.TotalExpenses)
	deltaIncome := totals.TotalIncome.Sub(prev.TotalIncome)

	expenseChangePct := decimal.Zero
	if prev.TotalExpenses.IsPositive() {
		expenseChangePct = deltaExpenses.Div(prev.TotalExpenses).Mul(decimal.NewFromInt(100)).Round(1)
	}

	return json.Marshal(vsPreviousEntry{
		CurrentIncome:      totals.TotalIncome.String(),
		CurrentExpenses:    totals.TotalExpenses.String(),
		CurrentNetSavings:  currentNet.String(),
		PreviousIncome:     prev.TotalIncome.String(),
		PreviousExpenses:   prev.TotalExpenses.String(),
		PreviousNetSavings: prevNet.String(),
		DeltaExpenses:      deltaExpenses.String(),
		DeltaIncome:        deltaIncome.String(),
		ExpenseChangePct:   expenseChangePct.String(),
		PreviousPeriodID:   prev.TrackingPeriodID.String(),
	})
}

// budgetProrationFactor returns the multiplier to apply to budget amounts when
// carrying them from one period to the next.
//
// A transition bridge is not a comparable slice of time, so copying amounts
// across one verbatim would either fire bogus "exceeded" alerts (going into a
// short bridge) or leave the budget uselessly slack (coming out of a long one).
// Scaling by the change in length keeps the intent of the budget intact and
// roughly restores the original amount once the bridge is over.
//
// Between two regular periods the factor is exactly 1: a 30-to-31-day drift
// silently moving someone's budget would just be baffling.
func budgetProrationFactor(from, to sqlc.TrackingPeriod) decimal.Decimal {
	one := decimal.NewFromInt(1)
	if from.IsTransition == to.IsTransition {
		return one
	}
	fromDays, ok := periodDays(from)
	if !ok {
		return one
	}
	toDays, ok := periodDays(to)
	if !ok {
		return one
	}
	return decimal.NewFromInt(int64(toDays)).Div(decimal.NewFromInt(int64(fromDays)))
}

// prorateAmount applies a proration factor, leaving the amount untouched when
// the factor is exactly 1 so an unscaled copy stays bit-for-bit identical.
func prorateAmount(amount, factor decimal.Decimal) decimal.Decimal {
	if factor.Equal(decimal.NewFromInt(1)) {
		return amount
	}
	return amount.Mul(factor).Round(2)
}

// periodDays returns the inclusive length of a period, and false when its dates
// are missing or inverted. That would be a data bug, but letting it through
// would divide the rollover by zero and take down the close for that user, so we
// degrade to an unscaled copy instead.
func periodDays(p sqlc.TrackingPeriod) (int, bool) {
	if !p.StartDate.Valid || !p.EndDate.Valid || p.EndDate.Time.Before(p.StartDate.Time) {
		return 0, false
	}
	return domain.PeriodRange{Start: p.StartDate.Time, End: p.EndDate.Time}.DurationDays(), true
}
