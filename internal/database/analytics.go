package database

// analytics.go — "final" insight generators for ClosePeriodTx.
//
// Each generator receives the full dataset already queried within the
// close-transaction and returns zero or more insightDraft values. A draft is
// omitted (nil return) when there is not enough data to produce a meaningful
// insight. The caller (store_periods.go) persists every non-nil draft via
// CreateTrackingPeriodInsight.
//
// Insight types (all calculation_phase = "final"):
//   top_categories           — top spending categories
//   top_merchants            — top merchants / payee descriptions
//   ant_expenses_final       — high-frequency low-amount "ant expenses"
//   reduction_opportunity    — biggest savings opportunity by category
//   savings_summary          — net savings and rate wrap-up
//   budget_compliance        — overall budget adherence
//   vs_previous_final        — comparison with previous period
//   monthly_wrap_up          — single summary card
//   goal_achievement_summary — goals contributed to during the period

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
)

// insightDraft is the data required to persist one tracking_period_insights row.
type insightDraft struct {
	InsightType       string
	CalculationPhase  string
	Severity          string
	Title             string
	Message           string
	ActionLabel       *string
	ActionTarget      *string
	Data              []byte
	RelatedCategoryID uuid.NullUUID
	RelatedAccountID  uuid.NullUUID
	RelatedGoalID     uuid.NullUUID
	ValidUntil        pgtype.Timestamptz
}

// closePeriodData bundles everything collected during the close transaction so
// individual generators do not need to query the DB themselves.
type closePeriodData struct {
	Period            sqlc.TrackingPeriod
	Totals            sqlc.SummarizePeriodTotalsRow
	ExpByCategory     []sqlc.ExpenseByCategoryRow
	IncByCategory     []sqlc.IncomeByCategoryRow
	ExpByAccount      []sqlc.ExpenseByAccountRow
	ExpByDay          []sqlc.ExpenseByDayRow
	TopMerchants      []sqlc.TopMerchantsRow
	Budgets           []sqlc.Budget
	GoalContribsTotal decimal.Decimal
	PrevSummary       *sqlc.TrackingPeriodSummary // nil if no previous period
}

// generateFinalInsights returns the set of insights to persist at period close.
// Generators that cannot produce a meaningful result return nil and are skipped.
func generateFinalInsights(d closePeriodData) []insightDraft {
	generators := []func(closePeriodData) *insightDraft{
		genTopCategories,
		genTopMerchants,
		genAntExpensesFinal,
		genReductionOpportunity,
		genSavingsSummary,
		genBudgetCompliance,
		genVsPreviousFinal,
		genMonthlyWrapUp,
		genGoalAchievementSummary,
	}

	var result []insightDraft
	for _, gen := range generators {
		if draft := gen(d); draft != nil {
			result = append(result, *draft)
		}
	}
	return result
}

// mustJSON marshals v to JSON bytes; panics on failure (only unreachable for
// our controlled payload structs).
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("analytics: mustJSON: %v", err))
	}
	return b
}

// ─── Generators ──────────────────────────────────────────────────────────────

// genTopCategories produces a top_categories insight listing the 3 highest
// expense categories. Omitted when there are no expense transactions.
func genTopCategories(d closePeriodData) *insightDraft {
	if len(d.ExpByCategory) == 0 {
		return nil
	}

	type entry struct {
		CategoryID   *string `json:"category_id"`
		CategoryName *string `json:"category_name"`
		Total        string  `json:"total"`
		TxnCount     int32   `json:"txn_count"`
		PctOfTotal   string  `json:"pct_of_total"`
	}

	totalExpenses := d.Totals.TotalExpenses
	limit := len(d.ExpByCategory)
	if limit > 3 {
		limit = 3
	}

	entries := make([]entry, 0, limit)
	for _, row := range d.ExpByCategory[:limit] {
		var catIDStr *string
		if row.CategoryID.Valid {
			s := row.CategoryID.UUID.String()
			catIDStr = &s
		}
		pct := decimal.Zero
		if totalExpenses.IsPositive() {
			pct = row.Total.Div(totalExpenses).Mul(decimal.NewFromInt(100)).Round(1)
		}
		entries = append(entries, entry{
			CategoryID:   catIDStr,
			CategoryName: row.CategoryName,
			Total:        row.Total.String(),
			TxnCount:     row.TxnCount,
			PctOfTotal:   pct.String(),
		})
	}

	payload := map[string]any{
		"categories":     entries,
		"total_expenses": totalExpenses.String(),
	}

	topName := "Sin categoría"
	if d.ExpByCategory[0].CategoryName != nil {
		topName = *d.ExpByCategory[0].CategoryName
	}

	var relCatID uuid.NullUUID
	if d.ExpByCategory[0].CategoryID.Valid {
		relCatID = d.ExpByCategory[0].CategoryID
	}

	return &insightDraft{
		InsightType:       "top_categories",
		CalculationPhase:  "final",
		Severity:          "info",
		Title:             "Top categorías de gasto",
		Message:           fmt.Sprintf("Tu mayor gasto fue en %s con %s.", topName, d.ExpByCategory[0].Total.String()),
		Data:              mustJSON(payload),
		RelatedCategoryID: relCatID,
	}
}

// genTopMerchants lists the top merchants by expense total.
// Omitted when no transactions have a description.
func genTopMerchants(d closePeriodData) *insightDraft {
	if len(d.TopMerchants) == 0 {
		return nil
	}

	type entry struct {
		Description string `json:"description"`
		Total       string `json:"total"`
		TxnCount    int32  `json:"txn_count"`
	}

	entries := make([]entry, 0, len(d.TopMerchants))
	for _, row := range d.TopMerchants {
		desc := ""
		if row.Description != nil {
			desc = *row.Description
		}
		entries = append(entries, entry{
			Description: desc,
			Total:       row.Total.String(),
			TxnCount:    row.TxnCount,
		})
	}

	topDesc := entries[0].Description

	return &insightDraft{
		InsightType:      "top_merchants",
		CalculationPhase: "final",
		Severity:         "info",
		Title:            "Comercios más frecuentes",
		Message:          fmt.Sprintf("Gastaste más en %s.", topDesc),
		Data:             mustJSON(map[string]any{"merchants": entries}),
	}
}

// genAntExpensesFinal detects "ant expenses": many small transactions (< 10%
// of average transaction amount) grouped by category.
// Omitted when fewer than 5 expense transactions exist.
func genAntExpensesFinal(d closePeriodData) *insightDraft {
	if d.Totals.ExpenseTransactionCount < 5 {
		return nil
	}

	avgTxn := d.Totals.TotalExpenses.Div(decimal.NewFromInt(int64(d.Totals.ExpenseTransactionCount)))
	threshold := avgTxn.Mul(decimal.NewFromFloat(0.1))

	type small struct {
		CategoryID   *string `json:"category_id"`
		CategoryName *string `json:"category_name"`
		Total        string  `json:"total"`
		TxnCount     int32   `json:"txn_count"`
	}

	var antTotal decimal.Decimal
	var antCount int32
	var breakdown []small

	for _, row := range d.ExpByCategory {
		if row.TxnCount < 3 {
			continue
		}
		perTxn := row.Total.Div(decimal.NewFromInt(int64(row.TxnCount)))
		if perTxn.LessThan(threshold) {
			antTotal = antTotal.Add(row.Total)
			antCount += row.TxnCount
			var catIDStr *string
			if row.CategoryID.Valid {
				s := row.CategoryID.UUID.String()
				catIDStr = &s
			}
			breakdown = append(breakdown, small{
				CategoryID:   catIDStr,
				CategoryName: row.CategoryName,
				Total:        row.Total.String(),
				TxnCount:     row.TxnCount,
			})
		}
	}

	if antCount < 5 || antTotal.IsZero() {
		return nil
	}

	pct := decimal.Zero
	if d.Totals.TotalExpenses.IsPositive() {
		pct = antTotal.Div(d.Totals.TotalExpenses).Mul(decimal.NewFromInt(100)).Round(1)
	}

	return &insightDraft{
		InsightType:      "ant_expenses_final",
		CalculationPhase: "final",
		Severity:         "warning",
		Title:            "Gastos hormiga del periodo",
		Message:          fmt.Sprintf("%d gastos pequeños sumaron %s (%s%% de tu total).", antCount, antTotal.String(), pct.String()),
		Data: mustJSON(map[string]any{
			"ant_total":    antTotal.String(),
			"ant_count":    antCount,
			"pct_of_total": pct.String(),
			"breakdown":    breakdown,
		}),
	}
}

// genReductionOpportunity highlights the single category with the biggest
// reduction potential (highest expense, more than 1 transaction).
// Omitted when no categorized expenses exist.
func genReductionOpportunity(d closePeriodData) *insightDraft {
	for _, row := range d.ExpByCategory {
		if !row.CategoryID.Valid {
			continue // skip uncategorized
		}
		if row.TxnCount < 2 {
			continue
		}

		pct := decimal.Zero
		if d.Totals.TotalExpenses.IsPositive() {
			pct = row.Total.Div(d.Totals.TotalExpenses).Mul(decimal.NewFromInt(100)).Round(1)
		}

		catName := "Sin categoría"
		if row.CategoryName != nil {
			catName = *row.CategoryName
		}

		return &insightDraft{
			InsightType:      "reduction_opportunity",
			CalculationPhase: "final",
			Severity:         "info",
			Title:            "Oportunidad de reducción",
			Message: fmt.Sprintf(
				"%s representó el %s%% de tus gastos (%s en %d transacciones). Reducirlo un 20%% te ahorraría %s.",
				catName, pct.String(), row.Total.String(), row.TxnCount,
				row.Total.Mul(decimal.NewFromFloat(0.2)).String(),
			),
			Data: mustJSON(map[string]any{
				"category_id":            row.CategoryID.UUID.String(),
				"category_name":          catName,
				"total":                  row.Total.String(),
				"txn_count":              row.TxnCount,
				"pct_of_total":           pct.String(),
				"potential_saving_20pct": row.Total.Mul(decimal.NewFromFloat(0.2)).String(),
			}),
			RelatedCategoryID: row.CategoryID,
		}
	}
	return nil
}

// genSavingsSummary produces a savings_summary insight.
// Always generated (even if savings are negative).
func genSavingsSummary(d closePeriodData) *insightDraft {
	netSavings := d.Totals.TotalIncome.Sub(d.Totals.TotalExpenses)
	savingsRate := decimal.Zero
	if d.Totals.TotalIncome.IsPositive() {
		savingsRate = netSavings.Div(d.Totals.TotalIncome).Mul(decimal.NewFromInt(100)).Round(1)
	}

	severity := "info"
	var msg string
	if netSavings.IsNegative() {
		severity = "warning"
		msg = fmt.Sprintf("Gastaste %s más de lo que ingresaste.", netSavings.Abs().String())
	} else if savingsRate.GreaterThanOrEqual(decimal.NewFromInt(20)) {
		severity = "success"
		msg = fmt.Sprintf("¡Excelente! Ahorraste %s (%s%% de ingresos).", netSavings.String(), savingsRate.String())
	} else {
		msg = fmt.Sprintf("Ahorraste %s (%s%% de tus ingresos).", netSavings.String(), savingsRate.String())
	}

	return &insightDraft{
		InsightType:      "savings_summary",
		CalculationPhase: "final",
		Severity:         severity,
		Title:            "Resumen de ahorro del periodo",
		Message:          msg,
		Data: mustJSON(map[string]any{
			"total_income":       d.Totals.TotalIncome.String(),
			"total_expenses":     d.Totals.TotalExpenses.String(),
			"net_savings":        netSavings.String(),
			"savings_rate":       savingsRate.String(),
			"goal_contributions": d.GoalContribsTotal.String(),
		}),
	}
}

// genBudgetCompliance summarises how well the user followed their budgets.
// Omitted when no budgets exist.
func genBudgetCompliance(d closePeriodData) *insightDraft {
	if len(d.Budgets) == 0 {
		return nil
	}

	type budgetStatus struct {
		BudgetID      string  `json:"budget_id"`
		CategoryID    *string `json:"category_id"`
		BudgetAmount  string  `json:"budget_amount"`
		SpentAmount   string  `json:"spent_amount"`
		CompliancePct string  `json:"compliance_pct"`
		Exceeded      bool    `json:"exceeded"`
	}

	catSpend := make(map[uuid.UUID]decimal.Decimal)
	for _, row := range d.ExpByCategory {
		if row.CategoryID.Valid {
			catSpend[row.CategoryID.UUID] = row.Total
		}
	}

	var statuses []budgetStatus
	exceededCount := 0
	compliantCount := 0

	for _, b := range d.Budgets {
		var spent decimal.Decimal
		if b.CategoryID.Valid {
			spent = catSpend[b.CategoryID.UUID]
		} else {
			spent = d.Totals.TotalExpenses
		}

		compliancePct := decimal.Zero
		if b.Amount.IsPositive() {
			compliancePct = spent.Div(b.Amount).Mul(decimal.NewFromInt(100)).Round(1)
		}
		exceeded := spent.GreaterThan(b.Amount)
		if exceeded {
			exceededCount++
		} else {
			compliantCount++
		}

		var catIDStr *string
		if b.CategoryID.Valid {
			s := b.CategoryID.UUID.String()
			catIDStr = &s
		}

		statuses = append(statuses, budgetStatus{
			BudgetID:      b.ID.String(),
			CategoryID:    catIDStr,
			BudgetAmount:  b.Amount.String(),
			SpentAmount:   spent.String(),
			CompliancePct: compliancePct.String(),
			Exceeded:      exceeded,
		})
	}

	severity := "success"
	msg := fmt.Sprintf("Cumpliste %d de %d presupuestos.", compliantCount, len(d.Budgets))
	if exceededCount > 0 {
		severity = "warning"
		msg = fmt.Sprintf("Superaste %d de %d presupuestos.", exceededCount, len(d.Budgets))
	}
	if exceededCount == len(d.Budgets) {
		severity = "critical"
	}

	return &insightDraft{
		InsightType:      "budget_compliance",
		CalculationPhase: "final",
		Severity:         severity,
		Title:            "Cumplimiento de presupuestos",
		Message:          msg,
		Data: mustJSON(map[string]any{
			"budgets":         statuses,
			"compliant_count": compliantCount,
			"exceeded_count":  exceededCount,
			"total_budgets":   len(d.Budgets),
		}),
	}
}

// genVsPreviousFinal compares key metrics with the previous period's snapshot.
// Omitted when no previous period summary exists.
func genVsPreviousFinal(d closePeriodData) *insightDraft {
	if d.PrevSummary == nil {
		return nil
	}
	prev := d.PrevSummary

	deltaExpenses := d.Totals.TotalExpenses.Sub(prev.TotalExpenses)
	deltaIncome := d.Totals.TotalIncome.Sub(prev.TotalIncome)

	expenseChangePct := decimal.Zero
	if prev.TotalExpenses.IsPositive() {
		expenseChangePct = deltaExpenses.Div(prev.TotalExpenses).Mul(decimal.NewFromInt(100)).Round(1)
	}

	direction := "subieron"
	if deltaExpenses.IsNegative() {
		direction = "bajaron"
	}

	severity := "info"
	if deltaExpenses.IsNegative() {
		severity = "success"
	} else if expenseChangePct.GreaterThan(decimal.NewFromInt(20)) {
		severity = "warning"
	}

	msg := fmt.Sprintf("Tus gastos %s un %s%% respecto al periodo anterior.",
		direction, expenseChangePct.Abs().String())

	return &insightDraft{
		InsightType:      "vs_previous_final",
		CalculationPhase: "final",
		Severity:         severity,
		Title:            "Comparativa con el periodo anterior",
		Message:          msg,
		Data: mustJSON(map[string]any{
			"current_income":     d.Totals.TotalIncome.String(),
			"current_expenses":   d.Totals.TotalExpenses.String(),
			"previous_income":    prev.TotalIncome.String(),
			"previous_expenses":  prev.TotalExpenses.String(),
			"delta_expenses":     deltaExpenses.String(),
			"delta_income":       deltaIncome.String(),
			"expense_change_pct": expenseChangePct.String(),
			"previous_period_id": prev.TrackingPeriodID.String(),
		}),
	}
}

// genMonthlyWrapUp produces a single "month in a nutshell" card.
// Always generated.
func genMonthlyWrapUp(d closePeriodData) *insightDraft {
	netSavings := d.Totals.TotalIncome.Sub(d.Totals.TotalExpenses)
	savingsRate := decimal.Zero
	if d.Totals.TotalIncome.IsPositive() {
		savingsRate = netSavings.Div(d.Totals.TotalIncome).Mul(decimal.NewFromInt(100)).Round(1)
	}

	topCatName := "sin categoría dominante"
	if len(d.ExpByCategory) > 0 && d.ExpByCategory[0].CategoryName != nil {
		topCatName = *d.ExpByCategory[0].CategoryName
	}

	msg := fmt.Sprintf(
		"En este periodo tuviste %d transacciones: ingresos %s, gastos %s, ahorro neto %s (%s%%). Tu categoría principal fue %s.",
		d.Totals.TransactionCount,
		d.Totals.TotalIncome.String(),
		d.Totals.TotalExpenses.String(),
		netSavings.String(),
		savingsRate.String(),
		topCatName,
	)

	return &insightDraft{
		InsightType:      "monthly_wrap_up",
		CalculationPhase: "final",
		Severity:         "info",
		Title:            "Resumen del mes",
		Message:          msg,
		Data: mustJSON(map[string]any{
			"total_income":      d.Totals.TotalIncome.String(),
			"total_expenses":    d.Totals.TotalExpenses.String(),
			"net_savings":       netSavings.String(),
			"savings_rate":      savingsRate.String(),
			"transaction_count": d.Totals.TransactionCount,
			"top_category":      topCatName,
		}),
	}
}

// genGoalAchievementSummary summarises savings goal contributions made during
// the period. Omitted when no contributions exist.
func genGoalAchievementSummary(d closePeriodData) *insightDraft {
	if d.GoalContribsTotal.IsZero() {
		return nil
	}

	return &insightDraft{
		InsightType:      "goal_achievement_summary",
		CalculationPhase: "final",
		Severity:         "success",
		Title:            "Aportes a metas de ahorro",
		Message:          fmt.Sprintf("Aportaste %s a tus metas de ahorro durante este periodo.", d.GoalContribsTotal.String()),
		Data: mustJSON(map[string]any{
			"goal_contributions_total": d.GoalContribsTotal.String(),
		}),
	}
}
