package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// Valid values for the Summary view parameter.
const (
	ViewFull     = "full"
	ViewBiweekly = "biweekly"
	ViewWeekly   = "weekly"
)

// SubPeriodTotals holds the financial aggregates for one date sub-range within a
// tracking period (used by the biweekly / weekly breakdown views).
type SubPeriodTotals struct {
	Index                   int
	From                    time.Time
	To                      time.Time
	TotalIncome             decimal.Decimal
	TotalExpenses           decimal.Decimal
	TotalTransfers          decimal.Decimal
	NetSavings              decimal.Decimal
	SavingsRate             decimal.Decimal
	TransactionCount        int32
	ExpenseTransactionCount int32
	IncomeTransactionCount  int32
}

// PeriodSummary is the computed financial summary for a tracking period. Totals
// are always the full-period values regardless of the requested view; SubPeriods
// is populated only for biweekly / weekly.
type PeriodSummary struct {
	PeriodID                uuid.UUID
	View                    string
	TotalIncome             decimal.Decimal
	TotalExpenses           decimal.Decimal
	TotalTransfers          decimal.Decimal
	NetSavings              decimal.Decimal
	SavingsRate             decimal.Decimal
	TransactionCount        int32
	ExpenseTransactionCount int32
	IncomeTransactionCount  int32
	TopExpenseCategoryID    *uuid.UUID
	TopExpenseCategoryTotal *decimal.Decimal
	SubPeriods              []SubPeriodTotals
}

// PeriodQueryService provides read access to tracking periods. It is separate
// from PeriodService (which handles the scheduled-close logic).
type PeriodQueryService struct {
	store database.Store
	now   func() time.Time
}

// NewPeriodQueryService builds the service. Dates are resolved in
// America/Bogota so "today" matches the Colombian user regardless of server tz.
func NewPeriodQueryService(store database.Store) *PeriodQueryService {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		loc = time.UTC
	}
	return &PeriodQueryService{
		store: store,
		now:   func() time.Time { return time.Now().In(loc) },
	}
}

// List returns all tracking periods for a user ordered newest-first.
func (s *PeriodQueryService) List(ctx context.Context, userID uuid.UUID) ([]sqlc.TrackingPeriod, error) {
	return s.store.ListTrackingPeriodsByUser(ctx, userID)
}

// GetActive returns the user's active period, lazily closing (and rolling over)
// any that have already ended — a fallback for when the nightly scheduler
// hasn't run. Returns domain.ErrNoActivePeriod when there is none.
func (s *PeriodQueryService) GetActive(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
	return s.activePeriod(ctx, userID)
}

// Get returns a single tracking period owned by userID.
// Returns domain.ErrNotFound when it does not exist or belongs to another user.
func (s *PeriodQueryService) Get(ctx context.Context, userID, id uuid.UUID) (sqlc.TrackingPeriod, error) {
	period, err := s.store.GetTrackingPeriodForUser(ctx, sqlc.GetTrackingPeriodForUserParams{
		ID:     id,
		UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.TrackingPeriod{}, domain.ErrNotFound
	}
	return period, err
}

// Summary returns the financial summary for a period. view must be "full",
// "biweekly" or "weekly" (any other value is treated as "full"). Totals are
// computed live from transactions, so it works for both active and closed
// periods. net_savings = income - expenses; savings_rate = net_savings /
// income * 100 (0 when income is zero).
func (s *PeriodQueryService) Summary(ctx context.Context, userID, id uuid.UUID, view string) (PeriodSummary, error) {
	sum, _, err := s.SummaryWithSnapshot(ctx, userID, id, view)
	return sum, err
}

// SummaryWithSnapshot is like Summary but also returns the stored DB snapshot
// (non-nil only for closed periods that have a summary row). The snapshot
// carries the JSONB breakdown fields that are shown in the API response.
func (s *PeriodQueryService) SummaryWithSnapshot(ctx context.Context, userID, id uuid.UUID, view string) (PeriodSummary, *sqlc.TrackingPeriodSummary, error) {
	// Validate ownership first.
	period, err := s.Get(ctx, userID, id)
	if err != nil {
		return PeriodSummary{}, nil, err
	}

	// Normalize view value.
	switch view {
	case ViewBiweekly, ViewWeekly:
		// keep as-is
	default:
		view = ViewFull
	}

	// Full-period aggregates (always computed).
	totals, err := s.store.SummarizePeriodTotals(ctx, id)
	if err != nil {
		return PeriodSummary{}, nil, err
	}

	netSavings := totals.TotalIncome.Sub(totals.TotalExpenses)
	savingsRate := decimal.Zero
	if totals.TotalIncome.IsPositive() {
		savingsRate = netSavings.Div(totals.TotalIncome).Mul(decimal.NewFromInt(100)).Round(2)
	}

	// Top expense category (optional; pgx.ErrNoRows is fine — period may have
	// no expense transactions yet).
	var topCatID *uuid.UUID
	var topCatAmt *decimal.Decimal
	if top, err := s.store.GetTopExpenseCategory(ctx, id); err == nil {
		if top.CategoryID.Valid {
			uid := top.CategoryID.UUID
			topCatID = &uid
		}
		amt := top.Total
		topCatAmt = &amt
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return PeriodSummary{}, nil, err
	}

	periodSummary := PeriodSummary{
		PeriodID:                id,
		View:                    view,
		TotalIncome:             totals.TotalIncome,
		TotalExpenses:           totals.TotalExpenses,
		TotalTransfers:          totals.TotalTransfers,
		NetSavings:              netSavings,
		SavingsRate:             savingsRate,
		TransactionCount:        totals.TransactionCount,
		ExpenseTransactionCount: totals.ExpenseTransactionCount,
		IncomeTransactionCount:  totals.IncomeTransactionCount,
		TopExpenseCategoryID:    topCatID,
		TopExpenseCategoryTotal: topCatAmt,
	}

	// Sub-period breakdown for biweekly / weekly views.
	if view != ViewFull {
		blocks := 2
		if view == ViewWeekly {
			blocks = 4
		}
		subPeriods, err := s.buildSubPeriods(ctx, id, period, blocks)
		if err != nil {
			return PeriodSummary{}, nil, err
		}
		periodSummary.SubPeriods = subPeriods
	}

	// For closed periods, also fetch the stored snapshot (JSONB breakdowns).
	var snapshot *sqlc.TrackingPeriodSummary
	if period.Status == "closed" {
		snap, err := s.store.GetTrackingPeriodSummary(ctx, id)
		if err == nil {
			snapshot = &snap
		}
		// If summary not found (shouldn't happen for closed periods), just omit it.
	}

	return periodSummary, snapshot, nil
}

// buildSubPeriods divides [start_date, end_date] into n contiguous date blocks
// and queries the transaction totals for each one.
func (s *PeriodQueryService) buildSubPeriods(
	ctx context.Context,
	periodID uuid.UUID,
	period sqlc.TrackingPeriod,
	n int,
) ([]SubPeriodTotals, error) {
	start := period.StartDate.Time
	end := period.EndDate.Time

	// Integer division; the last block absorbs the remainder so all days are
	// covered and no day is counted twice.
	totalDays := int(end.Sub(start).Hours()/24) + 1
	blockSize := totalDays / n
	if blockSize < 1 {
		blockSize = 1
	}

	result := make([]SubPeriodTotals, 0, n)
	cursor := start

	for i := 0; i < n; i++ {
		blockEnd := cursor.AddDate(0, 0, blockSize-1)
		if i == n-1 || blockEnd.After(end) {
			blockEnd = end
		}

		row, err := s.store.SummarizePeriodTotalsInRange(ctx, sqlc.SummarizePeriodTotalsInRangeParams{
			TrackingPeriodID: periodID,
			FromDate:         pgtype.Date{Time: cursor, Valid: true},
			ToDate:           pgtype.Date{Time: blockEnd, Valid: true},
		})
		if err != nil {
			return nil, err
		}

		net := row.TotalIncome.Sub(row.TotalExpenses)
		rate := decimal.Zero
		if row.TotalIncome.IsPositive() {
			rate = net.Div(row.TotalIncome).Mul(decimal.NewFromInt(100)).Round(2)
		}

		result = append(result, SubPeriodTotals{
			Index:                   i + 1,
			From:                    cursor,
			To:                      blockEnd,
			TotalIncome:             row.TotalIncome,
			TotalExpenses:           row.TotalExpenses,
			TotalTransfers:          row.TotalTransfers,
			NetSavings:              net,
			SavingsRate:             rate,
			TransactionCount:        row.TransactionCount,
			ExpenseTransactionCount: row.ExpenseTransactionCount,
			IncomeTransactionCount:  row.IncomeTransactionCount,
		})

		cursor = blockEnd.AddDate(0, 0, 1)
		if cursor.After(end) {
			break
		}
	}

	return result, nil
}

// GetInsights returns insights for the given tracking period:
//   - Active period: performs a lazy refresh of ALL "during" insights and
//     returns the freshly computed set (spending_pace, budget_warning,
//     budget_exceeded, ant_expenses_early, unusual_expense, vs_previous_partial,
//     goal_progress_alert).
//   - Closed period: returns the immutable "final" insights generated at close
//     time (top_categories, top_merchants, …, goal_achievement_summary).
//
// Returns domain.ErrNotFound when the period does not belong to the user.
func (s *PeriodQueryService) GetInsights(ctx context.Context, userID, periodID uuid.UUID) ([]sqlc.TrackingPeriodInsight, error) {
	// Validate ownership and obtain the period status.
	period, err := s.Get(ctx, userID, periodID)
	if err != nil {
		return nil, err
	}

	if period.Status == "active" {
		// Lazy-refresh: recalculate all "during" insights and return them.
		return s.store.RefreshAllDuringInsights(ctx, periodID, userID)
	}

	// Closed period: return the immutable "final" insights.
	return s.store.GetFinalInsights(ctx, periodID, userID)
}

// activePeriod returns the user's active period, lazily closing (and rolling
// over) any that have already ended. Mirrors BudgetService.activePeriod.
func (s *PeriodQueryService) activePeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
	const maxRollovers = 120
	today := dateOnly(s.now())

	for i := 0; i < maxRollovers; i++ {
		period, err := s.store.GetActiveTrackingPeriod(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.TrackingPeriod{}, domain.ErrNoActivePeriod
		} else if err != nil {
			return sqlc.TrackingPeriod{}, err
		}

		if !dateOnly(period.EndDate.Time).Before(today) {
			return period, nil
		}

		// Period has ended: close it (generates the next one) and loop.
		if _, err := s.store.ClosePeriodTx(ctx, period.ID, period.UserID); err != nil {
			return sqlc.TrackingPeriod{}, err
		}
	}

	return sqlc.TrackingPeriod{}, domain.ErrNoActivePeriod
}
