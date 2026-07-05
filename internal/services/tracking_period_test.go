package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// mockPeriodStore is a fake Store for PeriodQueryService unit tests.
// Embed database.Store (interface) so that only overridden methods need to be
// defined; any unexpected call will panic, making test failures obvious.
type mockPeriodStore struct {
	database.Store

	getActivePeriod        func(context.Context, uuid.UUID) (sqlc.TrackingPeriod, error)
	getPeriodForUser       func(context.Context, sqlc.GetTrackingPeriodForUserParams) (sqlc.TrackingPeriod, error)
	summarizeTotals        func(context.Context, uuid.UUID) (sqlc.SummarizePeriodTotalsRow, error)
	summarizeTotalsInRange func(context.Context, sqlc.SummarizePeriodTotalsInRangeParams) (sqlc.SummarizePeriodTotalsInRangeRow, error)
	getTopExpenseCategory  func(context.Context, uuid.UUID) (sqlc.GetTopExpenseCategoryRow, error)
	closePeriodTx          func(context.Context, uuid.UUID, uuid.UUID) (database.ClosePeriodResult, error)
	closePeriodTxCallCount int
}

func (m *mockPeriodStore) GetActiveTrackingPeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
	return m.getActivePeriod(ctx, userID)
}
func (m *mockPeriodStore) GetTrackingPeriodForUser(ctx context.Context, arg sqlc.GetTrackingPeriodForUserParams) (sqlc.TrackingPeriod, error) {
	return m.getPeriodForUser(ctx, arg)
}
func (m *mockPeriodStore) SummarizePeriodTotals(ctx context.Context, id uuid.UUID) (sqlc.SummarizePeriodTotalsRow, error) {
	return m.summarizeTotals(ctx, id)
}
func (m *mockPeriodStore) SummarizePeriodTotalsInRange(ctx context.Context, arg sqlc.SummarizePeriodTotalsInRangeParams) (sqlc.SummarizePeriodTotalsInRangeRow, error) {
	return m.summarizeTotalsInRange(ctx, arg)
}
func (m *mockPeriodStore) GetTopExpenseCategory(ctx context.Context, id uuid.UUID) (sqlc.GetTopExpenseCategoryRow, error) {
	return m.getTopExpenseCategory(ctx, id)
}
func (m *mockPeriodStore) ClosePeriodTx(ctx context.Context, periodID, userID uuid.UUID) (database.ClosePeriodResult, error) {
	m.closePeriodTxCallCount++
	return m.closePeriodTx(ctx, periodID, userID)
}

// newPeriodQuerySvcFixed returns a PeriodQueryService pinned to 2026-07-04.
func newPeriodQuerySvcFixed(store database.Store) *PeriodQueryService {
	return &PeriodQueryService{
		store: store,
		now:   func() time.Time { return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC) },
	}
}

// activePeriodFixture is an active period that is still valid on 2026-07-04.
func activePeriodFixture() sqlc.TrackingPeriod {
	return sqlc.TrackingPeriod{
		ID:                 uuid.New(),
		UserID:             uuid.New(),
		StartDate:          pgDateOf(time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)),
		EndDate:            pgDateOf(time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)),
		Status:             "active",
		SequenceNumber:     1,
		ConfigStartDay:     25,
		ConfigDurationDays: 30,
	}
}

// TestSummary_NetSavingsAndSavingsRate verifies that net_savings and
// savings_rate are computed correctly from the raw totals returned by the DB.
func TestSummary_NetSavingsAndSavingsRate(t *testing.T) {
	period := activePeriodFixture()

	store := &mockPeriodStore{
		getPeriodForUser: func(_ context.Context, _ sqlc.GetTrackingPeriodForUserParams) (sqlc.TrackingPeriod, error) {
			return period, nil
		},
		summarizeTotals: func(_ context.Context, _ uuid.UUID) (sqlc.SummarizePeriodTotalsRow, error) {
			return sqlc.SummarizePeriodTotalsRow{
				TotalIncome:             decimal.RequireFromString("500000"),
				TotalExpenses:           decimal.RequireFromString("350000"),
				TotalTransfers:          decimal.RequireFromString("0"),
				TransactionCount:        15,
				ExpenseTransactionCount: 10,
				IncomeTransactionCount:  5,
			}, nil
		},
		getTopExpenseCategory: func(_ context.Context, _ uuid.UUID) (sqlc.GetTopExpenseCategoryRow, error) {
			return sqlc.GetTopExpenseCategoryRow{}, pgx.ErrNoRows
		},
	}

	svc := newPeriodQuerySvcFixed(store)
	summary, err := svc.Summary(context.Background(), period.UserID, period.ID, ViewFull)
	require.NoError(t, err)

	// net_savings = 500000 - 350000 = 150000
	assert.True(t, summary.NetSavings.Equal(decimal.RequireFromString("150000")),
		"net_savings got %s", summary.NetSavings.String())

	// savings_rate = 150000 / 500000 * 100 = 30.00
	assert.True(t, summary.SavingsRate.Equal(decimal.RequireFromString("30")),
		"savings_rate got %s", summary.SavingsRate.String())

	assert.Equal(t, int32(15), summary.TransactionCount)
	assert.Nil(t, summary.TopExpenseCategoryID, "no top category when no expenses")
}

// TestSummary_ZeroIncomeRateIsZero checks that savings_rate is 0 (not a
// division-by-zero) when income is zero.
func TestSummary_ZeroIncomeRateIsZero(t *testing.T) {
	period := activePeriodFixture()

	store := &mockPeriodStore{
		getPeriodForUser: func(_ context.Context, _ sqlc.GetTrackingPeriodForUserParams) (sqlc.TrackingPeriod, error) {
			return period, nil
		},
		summarizeTotals: func(_ context.Context, _ uuid.UUID) (sqlc.SummarizePeriodTotalsRow, error) {
			return sqlc.SummarizePeriodTotalsRow{
				TotalIncome:   decimal.Zero,
				TotalExpenses: decimal.RequireFromString("100000"),
			}, nil
		},
		getTopExpenseCategory: func(_ context.Context, _ uuid.UUID) (sqlc.GetTopExpenseCategoryRow, error) {
			return sqlc.GetTopExpenseCategoryRow{}, pgx.ErrNoRows
		},
	}

	svc := newPeriodQuerySvcFixed(store)
	summary, err := svc.Summary(context.Background(), period.UserID, period.ID, ViewFull)
	require.NoError(t, err)
	assert.True(t, summary.SavingsRate.Equal(decimal.Zero), "savings_rate must be 0 when income is 0")
}

// TestGetActive_LazyClosesExpiredPeriod verifies that an active period whose
// end_date has already passed is closed exactly once and the fresh period is
// returned.
func TestGetActive_LazyClosesExpiredPeriod(t *testing.T) {
	freshPeriod := activePeriodFixture() // valid on 2026-07-04

	expiredPeriod := sqlc.TrackingPeriod{
		ID:     uuid.New(),
		UserID: freshPeriod.UserID,
		// End date in the past relative to the fixed clock (2026-07-04).
		StartDate: pgDateOf(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:   pgDateOf(time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)),
		Status:    "active",
	}

	callCount := 0
	store := &mockPeriodStore{
		getActivePeriod: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			callCount++
			if callCount == 1 {
				return expiredPeriod, nil
			}
			return freshPeriod, nil
		},
		closePeriodTx: func(_ context.Context, _, _ uuid.UUID) (database.ClosePeriodResult, error) {
			return database.ClosePeriodResult{}, nil
		},
	}

	svc := newPeriodQuerySvcFixed(store)
	result, err := svc.GetActive(context.Background(), freshPeriod.UserID)

	require.NoError(t, err)
	assert.Equal(t, freshPeriod.ID, result.ID, "should return the fresh period after rollover")
	assert.Equal(t, 1, store.closePeriodTxCallCount, "expired period must be closed exactly once")
}

// TestGetActive_NoActivePeriod verifies that domain.ErrNoActivePeriod is
// returned when the store returns no rows.
func TestGetActive_NoActivePeriod(t *testing.T) {
	store := &mockPeriodStore{
		getActivePeriod: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return sqlc.TrackingPeriod{}, pgx.ErrNoRows
		},
	}

	svc := newPeriodQuerySvcFixed(store)
	_, err := svc.GetActive(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrNoActivePeriod)
}

// TestGet_NotFound verifies that a 404 domain error is returned when the
// period does not exist or belongs to another user.
func TestGet_NotFound(t *testing.T) {
	store := &mockPeriodStore{
		getPeriodForUser: func(_ context.Context, _ sqlc.GetTrackingPeriodForUserParams) (sqlc.TrackingPeriod, error) {
			return sqlc.TrackingPeriod{}, pgx.ErrNoRows
		},
	}

	svc := newPeriodQuerySvcFixed(store)
	_, err := svc.Get(context.Background(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, domain.ErrNotFound)
}
