package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
)

// ─── advanceByFrequency table tests ──────────────────────────────────────────

func TestAdvanceByFrequency(t *testing.T) {
	// 2026-07-05 = Sunday
	base := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	dom5 := 5
	dom31 := 31
	custom7 := 7

	tests := []struct {
		name           string
		current        time.Time
		freq           string
		dayOfMonth     *int
		dayOfWeek      *int
		customInterval *int
		want           time.Time
	}{
		{
			name:    "daily adds 1 day",
			current: base,
			freq:    "daily",
			want:    time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "weekly adds 7 days",
			current: base,
			freq:    "weekly",
			want:    time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "biweekly adds 14 days",
			current: base,
			freq:    "biweekly",
			want:    time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "monthly without dom adds 1 month",
			current: base,
			freq:    "monthly",
			want:    time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "monthly with dom 5 → Aug 5",
			current:    base,
			freq:       "monthly",
			dayOfMonth: &dom5,
			want:       time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "monthly dom 31 from Jan 31 clamps to Feb 28 (no drift)",
			current:    time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
			freq:       "monthly",
			dayOfMonth: &dom31,
			want:       time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "monthly dom 31 recovers anchor after clamped month",
			current:    time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
			freq:       "monthly",
			dayOfMonth: &dom31,
			want:       time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "monthly without dom clamps overflow (Jan 31 → Feb 28)",
			current: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
			freq:    "monthly",
			want:    time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "yearly from Feb 29 clamps to Feb 28 on non-leap year",
			current: time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC),
			freq:    "yearly",
			want:    time.Date(2029, 2, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "yearly adds 1 year",
			current: base,
			freq:    "yearly",
			want:    time.Date(2027, 7, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name:           "custom 7 days",
			current:        base,
			freq:           "custom",
			customInterval: &custom7,
			want:           time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := advanceByFrequency(tc.current, tc.freq, tc.dayOfMonth, tc.dayOfWeek, tc.customInterval)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ─── clampToActivePeriod tests ───────────────────────────────────────────────

func TestClampToActivePeriod(t *testing.T) {
	period := sqlc.TrackingPeriod{
		StartDate: pgDateOf(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:   pgDateOf(time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)),
	}
	today := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

	t.Run("occurrence within period", func(t *testing.T) {
		occ := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
		got := clampToActivePeriod(occ, today, period)
		assert.Equal(t, occ, got)
	})

	t.Run("occurrence before period start → period start", func(t *testing.T) {
		occ := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)
		got := clampToActivePeriod(occ, today, period)
		assert.Equal(t, period.StartDate.Time, got)
	})

	t.Run("occurrence after period end → period end", func(t *testing.T) {
		occ := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
		got := clampToActivePeriod(occ, today, period)
		assert.Equal(t, period.EndDate.Time, got)
	})

	t.Run("occurrence after today → today", func(t *testing.T) {
		occ := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
		futureToday := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
		got := clampToActivePeriod(occ, futureToday, period)
		assert.Equal(t, futureToday, got)
	})
}

// ─── Engine mock store ───────────────────────────────────────────────────────

// mockEngineStore stubs the Store methods needed by RecurringEngineService.
type mockEngineStore struct {
	database.Store // embed to satisfy the interface; unexpected calls will panic

	listDue              func(context.Context, pgtype.Date) ([]sqlc.RecurringTransaction, error)
	listForUser          func(context.Context, uuid.UUID) ([]sqlc.RecurringTransaction, error)
	getActive            func(context.Context, uuid.UUID) (sqlc.TrackingPeriod, error)
	closePeriodTx        func(context.Context, uuid.UUID, uuid.UUID) (database.ClosePeriodResult, error)
	materialiseRecurring func(context.Context, database.MaterialiseRecurringParams) (sqlc.Transaction, error)
	advanceRecurring     func(context.Context, sqlc.AdvanceRecurringTransactionParams) (sqlc.RecurringTransaction, error)
}

func (m *mockEngineStore) ListDueRecurringTransactions(ctx context.Context, d pgtype.Date) ([]sqlc.RecurringTransaction, error) {
	return m.listDue(ctx, d)
}
func (m *mockEngineStore) ListRecurringTransactionsForUser(ctx context.Context, id uuid.UUID) ([]sqlc.RecurringTransaction, error) {
	return m.listForUser(ctx, id)
}
func (m *mockEngineStore) GetActiveTrackingPeriod(ctx context.Context, id uuid.UUID) (sqlc.TrackingPeriod, error) {
	return m.getActive(ctx, id)
}
func (m *mockEngineStore) ClosePeriodTx(ctx context.Context, pid, uid uuid.UUID) (database.ClosePeriodResult, error) {
	return m.closePeriodTx(ctx, pid, uid)
}
func (m *mockEngineStore) MaterialiseRecurringTx(ctx context.Context, arg database.MaterialiseRecurringParams) (sqlc.Transaction, error) {
	return m.materialiseRecurring(ctx, arg)
}
func (m *mockEngineStore) AdvanceRecurringTransaction(ctx context.Context, arg sqlc.AdvanceRecurringTransactionParams) (sqlc.RecurringTransaction, error) {
	return m.advanceRecurring(ctx, arg)
}

// newEngineFixedClock returns a RecurringEngineService pinned to 2026-07-15.
func newEngineFixedClock(store database.Store) *RecurringEngineService {
	return &RecurringEngineService{
		store: store,
		log:   zerolog.Nop(),
		now:   func() time.Time { return time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC) },
	}
}

// activeperiodFixture returns a period valid on 2026-07-15.
func activePeriodFor(userID uuid.UUID) sqlc.TrackingPeriod {
	return sqlc.TrackingPeriod{
		ID:        uuid.New(),
		UserID:    userID,
		StartDate: pgDateOf(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:   pgDateOf(time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)),
		Status:    "active",
	}
}

// monthlyTemplate returns a monthly recurring template due on `dueDate`.
func monthlyTemplate(userID uuid.UUID, dueDate time.Time) sqlc.RecurringTransaction {
	dom := int16(dueDate.Day())
	return sqlc.RecurringTransaction{
		ID:              uuid.New(),
		UserID:          userID,
		AccountID:       uuid.New(),
		Name:            "Rent",
		TransactionType: "expense",
		Amount:          decimal.NewFromInt(1_500_000),
		Currency:        "COP",
		Frequency:       "monthly",
		DayOfMonth:      &dom,
		StartDate:       pgDateOf(dueDate),
		NextDueDate:     pgDateOf(dueDate),
		IsActive:        true,
	}
}

// ─── Engine tests ─────────────────────────────────────────────────────────────

// TestProcessDueRecurring_GeneratesOneOccurrence verifies that a single due
// template results in exactly one materialisation call.
func TestProcessDueRecurring_GeneratesOneOccurrence(t *testing.T) {
	userID := uuid.New()
	period := activePeriodFor(userID)
	tmpl := monthlyTemplate(userID, time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))

	materialiseCalls := 0
	store := &mockEngineStore{
		listDue: func(_ context.Context, _ pgtype.Date) ([]sqlc.RecurringTransaction, error) {
			return []sqlc.RecurringTransaction{tmpl}, nil
		},
		getActive: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return period, nil
		},
		materialiseRecurring: func(_ context.Context, arg database.MaterialiseRecurringParams) (sqlc.Transaction, error) {
			materialiseCalls++
			// Verify the occurrence date is correct (July 5)
			assert.Equal(t, time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC), arg.OccurrenceDate)
			// Verify next_due_date advances by one month to Aug 5
			require.NotNil(t, arg.NextDueDate)
			assert.Equal(t, time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), *arg.NextDueDate)
			assert.True(t, arg.StillActive)
			return sqlc.Transaction{}, nil
		},
	}

	svc := newEngineFixedClock(store)
	n, err := svc.ProcessDueRecurring(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 1, materialiseCalls)
}

// TestProcessDueRecurring_CatchupGeneratesMultiple verifies that if a template
// is overdue by several periods (catch-up scenario), all missed occurrences are
// generated.
func TestProcessDueRecurring_CatchupGeneratesMultiple(t *testing.T) {
	userID := uuid.New()
	period := activePeriodFor(userID)
	// Template was due May 15 — 2 months overdue (May 15, June 15, then
	// July 15 is "today" so July 15 is also due).
	tmpl := monthlyTemplate(userID, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))

	materialiseCalls := 0
	occurrenceDates := []time.Time{}
	store := &mockEngineStore{
		listDue: func(_ context.Context, _ pgtype.Date) ([]sqlc.RecurringTransaction, error) {
			return []sqlc.RecurringTransaction{tmpl}, nil
		},
		getActive: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return period, nil
		},
		materialiseRecurring: func(_ context.Context, arg database.MaterialiseRecurringParams) (sqlc.Transaction, error) {
			materialiseCalls++
			occurrenceDates = append(occurrenceDates, arg.OccurrenceDate)
			return sqlc.Transaction{}, nil
		},
	}

	svc := newEngineFixedClock(store)
	n, err := svc.ProcessDueRecurring(context.Background())

	require.NoError(t, err)
	// May 15, June 15, July 15 → 3 occurrences
	assert.Equal(t, 3, n)
	assert.Equal(t, 3, materialiseCalls)
	assert.Equal(t, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC), occurrenceDates[0])
	assert.Equal(t, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC), occurrenceDates[1])
	assert.Equal(t, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC), occurrenceDates[2])
}

// TestProcessDueRecurring_EndDateExhaustsTemplate verifies that when the next
// occurrence after the generated one would exceed end_date, the template is
// deactivated (StillActive = false, NextDueDate = nil).
func TestProcessDueRecurring_EndDateExhaustsTemplate(t *testing.T) {
	userID := uuid.New()
	period := activePeriodFor(userID)
	tmpl := monthlyTemplate(userID, time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	// end_date is July 31: the next occurrence after July 5 would be Aug 5 which
	// is past the end_date, so after generating July 5 the template deactivates.
	endDate := pgDateOf(time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC))
	tmpl.EndDate = endDate

	var capturedArg database.MaterialiseRecurringParams
	store := &mockEngineStore{
		listDue: func(_ context.Context, _ pgtype.Date) ([]sqlc.RecurringTransaction, error) {
			return []sqlc.RecurringTransaction{tmpl}, nil
		},
		getActive: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return period, nil
		},
		materialiseRecurring: func(_ context.Context, arg database.MaterialiseRecurringParams) (sqlc.Transaction, error) {
			capturedArg = arg
			return sqlc.Transaction{}, nil
		},
	}

	svc := newEngineFixedClock(store)
	n, err := svc.ProcessDueRecurring(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, n, "only the July 5 occurrence should be generated")
	assert.False(t, capturedArg.StillActive, "template must be deactivated")
	assert.Nil(t, capturedArg.NextDueDate, "next_due_date must be nil when exhausted")
}

// TestProcessDueRecurring_NoActivePeriod verifies that templates for users
// without an active period are silently skipped (no error, no generation).
func TestProcessDueRecurring_NoActivePeriod(t *testing.T) {
	userID := uuid.New()
	tmpl := monthlyTemplate(userID, time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))

	store := &mockEngineStore{
		listDue: func(_ context.Context, _ pgtype.Date) ([]sqlc.RecurringTransaction, error) {
			return []sqlc.RecurringTransaction{tmpl}, nil
		},
		getActive: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return sqlc.TrackingPeriod{}, pgx.ErrNoRows
		},
		materialiseRecurring: func(_ context.Context, _ database.MaterialiseRecurringParams) (sqlc.Transaction, error) {
			t.Fatal("materialise should not be called when there is no active period")
			return sqlc.Transaction{}, nil
		},
	}

	svc := newEngineFixedClock(store)
	n, err := svc.ProcessDueRecurring(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, n, "no occurrences should be generated")
}

// TestProcessUserRecurring_SkipsInactive verifies that inactive templates are
// not processed by the lazy trigger.
func TestProcessUserRecurring_SkipsInactive(t *testing.T) {
	userID := uuid.New()
	tmpl := monthlyTemplate(userID, time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	tmpl.IsActive = false

	store := &mockEngineStore{
		listForUser: func(_ context.Context, _ uuid.UUID) ([]sqlc.RecurringTransaction, error) {
			return []sqlc.RecurringTransaction{tmpl}, nil
		},
		materialiseRecurring: func(_ context.Context, _ database.MaterialiseRecurringParams) (sqlc.Transaction, error) {
			t.Fatal("materialise should not be called for inactive template")
			return sqlc.Transaction{}, nil
		},
	}

	svc := newEngineFixedClock(store)
	n, err := svc.ProcessUserRecurring(context.Background(), userID)

	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// TestProcessUserRecurring_SkipsNotYetDue verifies templates whose
// next_due_date is in the future are left alone.
func TestProcessUserRecurring_SkipsNotYetDue(t *testing.T) {
	userID := uuid.New()
	// next_due_date is July 20 — today (fixed) is July 15 so it is not due yet.
	tmpl := monthlyTemplate(userID, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))

	store := &mockEngineStore{
		listForUser: func(_ context.Context, _ uuid.UUID) ([]sqlc.RecurringTransaction, error) {
			return []sqlc.RecurringTransaction{tmpl}, nil
		},
		materialiseRecurring: func(_ context.Context, _ database.MaterialiseRecurringParams) (sqlc.Transaction, error) {
			t.Fatal("materialise should not be called for a future template")
			return sqlc.Transaction{}, nil
		},
	}

	svc := newEngineFixedClock(store)
	n, err := svc.ProcessUserRecurring(context.Background(), userID)

	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// TestProcessDueRecurring_LazyClosesExpiredPeriod verifies that when the
// active period is expired the engine closes it and retries with the new one.
func TestProcessDueRecurring_LazyClosesExpiredPeriod(t *testing.T) {
	userID := uuid.New()
	tmpl := monthlyTemplate(userID, time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))

	// First period expired on June 30; second is valid on July 15.
	expiredPeriod := sqlc.TrackingPeriod{
		ID:        uuid.New(),
		UserID:    userID,
		StartDate: pgDateOf(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:   pgDateOf(time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)),
		Status:    "active",
	}
	freshPeriod := activePeriodFor(userID)

	closeCount := 0
	getCount := 0
	store := &mockEngineStore{
		listDue: func(_ context.Context, _ pgtype.Date) ([]sqlc.RecurringTransaction, error) {
			return []sqlc.RecurringTransaction{tmpl}, nil
		},
		getActive: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			getCount++
			if getCount == 1 {
				return expiredPeriod, nil
			}
			return freshPeriod, nil
		},
		closePeriodTx: func(_ context.Context, _, _ uuid.UUID) (database.ClosePeriodResult, error) {
			closeCount++
			return database.ClosePeriodResult{}, nil
		},
		materialiseRecurring: func(_ context.Context, arg database.MaterialiseRecurringParams) (sqlc.Transaction, error) {
			// Must be assigned to the fresh period, not the expired one.
			assert.Equal(t, freshPeriod.ID, arg.TrackingPeriodID)
			return sqlc.Transaction{}, nil
		},
	}

	svc := newEngineFixedClock(store)
	n, err := svc.ProcessDueRecurring(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 1, closeCount, "expired period must be closed exactly once")
}
