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

// ─── Helpers ─────────────────────────────────────────────────────────────────

func intPtr(v int) *int { return &v }

// ─── Fake store ──────────────────────────────────────────────────────────────

type mockRTStore struct {
	database.Store
	getAccount   func(ctx context.Context, arg sqlc.GetAccountParams) (sqlc.Account, error)
	getCategory  func(ctx context.Context, arg sqlc.GetCategoryForUserParams) (sqlc.Category, error)
	createRT     func(ctx context.Context, arg sqlc.CreateRecurringTransactionParams) (sqlc.RecurringTransaction, error)
	getRT        func(ctx context.Context, arg sqlc.GetRecurringTransactionParams) (sqlc.RecurringTransaction, error)
	updateRT     func(ctx context.Context, arg sqlc.UpdateRecurringTransactionParams) (sqlc.RecurringTransaction, error)
	softDeleteRT func(ctx context.Context, arg sqlc.SoftDeleteRecurringTransactionParams) (uuid.UUID, error)
	createCalled bool
}

func (m *mockRTStore) GetAccount(ctx context.Context, arg sqlc.GetAccountParams) (sqlc.Account, error) {
	return m.getAccount(ctx, arg)
}
func (m *mockRTStore) GetCategoryForUser(ctx context.Context, arg sqlc.GetCategoryForUserParams) (sqlc.Category, error) {
	return m.getCategory(ctx, arg)
}
func (m *mockRTStore) CreateRecurringTransaction(ctx context.Context, arg sqlc.CreateRecurringTransactionParams) (sqlc.RecurringTransaction, error) {
	m.createCalled = true
	return m.createRT(ctx, arg)
}
func (m *mockRTStore) GetRecurringTransaction(ctx context.Context, arg sqlc.GetRecurringTransactionParams) (sqlc.RecurringTransaction, error) {
	return m.getRT(ctx, arg)
}
func (m *mockRTStore) UpdateRecurringTransaction(ctx context.Context, arg sqlc.UpdateRecurringTransactionParams) (sqlc.RecurringTransaction, error) {
	return m.updateRT(ctx, arg)
}
func (m *mockRTStore) SoftDeleteRecurringTransaction(ctx context.Context, arg sqlc.SoftDeleteRecurringTransactionParams) (uuid.UUID, error) {
	return m.softDeleteRT(ctx, arg)
}

// defaultRTStore returns a mock store where account + category lookups succeed
// and the create/update/delete stubs return minimal valid responses.
func defaultRTStore() *mockRTStore {
	return &mockRTStore{
		getAccount: func(_ context.Context, _ sqlc.GetAccountParams) (sqlc.Account, error) {
			return sqlc.Account{ID: uuid.New()}, nil
		},
		getCategory: func(_ context.Context, _ sqlc.GetCategoryForUserParams) (sqlc.Category, error) {
			return sqlc.Category{ID: uuid.New()}, nil
		},
		createRT: func(_ context.Context, arg sqlc.CreateRecurringTransactionParams) (sqlc.RecurringTransaction, error) {
			return sqlc.RecurringTransaction{ID: uuid.New(), Frequency: arg.Frequency}, nil
		},
		updateRT: func(_ context.Context, arg sqlc.UpdateRecurringTransactionParams) (sqlc.RecurringTransaction, error) {
			return sqlc.RecurringTransaction{ID: arg.ID}, nil
		},
		softDeleteRT: func(_ context.Context, arg sqlc.SoftDeleteRecurringTransactionParams) (uuid.UUID, error) {
			return arg.ID, nil
		},
	}
}

func newRTSvc(store database.Store) *RecurringTransactionService {
	return &RecurringTransactionService{store: store}
}

// baseInput is a valid recurring transaction input used as the starting point
// for test cases that only vary one field at a time.
func baseInput() RecurringTransactionInput {
	return RecurringTransactionInput{
		AccountID:       uuid.New(),
		Name:            "Monthly rent",
		TransactionType: "expense",
		Amount:          decimal.NewFromInt(1_500_000),
		Currency:        "COP",
		Frequency:       "monthly",
		StartDate:       time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
		IsActive:        true,
	}
}

// ─── Validation tests ─────────────────────────────────────────────────────────

func TestCreateRT_InvalidTransactionType(t *testing.T) {
	store := defaultRTStore()
	in := baseInput()
	in.TransactionType = "transfer" // not allowed for recurring
	svc := newRTSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), in)
	require.ErrorIs(t, err, domain.ErrInvalidRecurringConfig)
	assert.False(t, store.createCalled)
}

func TestCreateRT_InvalidAmount_Zero(t *testing.T) {
	store := defaultRTStore()
	in := baseInput()
	in.Amount = decimal.Zero
	svc := newRTSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), in)
	require.ErrorIs(t, err, domain.ErrInvalidAmount)
	assert.False(t, store.createCalled)
}

func TestCreateRT_InvalidAmount_Negative(t *testing.T) {
	store := defaultRTStore()
	in := baseInput()
	in.Amount = decimal.NewFromInt(-100)
	svc := newRTSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), in)
	require.ErrorIs(t, err, domain.ErrInvalidAmount)
	assert.False(t, store.createCalled)
}

func TestCreateRT_InvalidFrequency(t *testing.T) {
	store := defaultRTStore()
	in := baseInput()
	in.Frequency = "fortnightly" // not in the enum
	svc := newRTSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), in)
	require.ErrorIs(t, err, domain.ErrInvalidFrequency)
	assert.False(t, store.createCalled)
}

func TestCreateRT_CustomFrequency_MissingInterval(t *testing.T) {
	store := defaultRTStore()
	in := baseInput()
	in.Frequency = "custom"
	// CustomIntervalDays not set → invalid
	svc := newRTSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), in)
	require.ErrorIs(t, err, domain.ErrInvalidRecurringConfig)
	assert.False(t, store.createCalled)
}

func TestCreateRT_CustomFrequency_ZeroInterval(t *testing.T) {
	store := defaultRTStore()
	in := baseInput()
	in.Frequency = "custom"
	in.CustomIntervalDays = intPtr(0) // must be > 0
	svc := newRTSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), in)
	require.ErrorIs(t, err, domain.ErrInvalidRecurringConfig)
	assert.False(t, store.createCalled)
}

func TestCreateRT_DayOfMonth_OutOfRange(t *testing.T) {
	store := defaultRTStore()
	in := baseInput()
	in.DayOfMonth = intPtr(32) // max is 31
	svc := newRTSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), in)
	require.ErrorIs(t, err, domain.ErrInvalidRecurringConfig)
	assert.False(t, store.createCalled)
}

func TestCreateRT_DayOfWeek_OutOfRange(t *testing.T) {
	store := defaultRTStore()
	in := baseInput()
	in.DayOfWeek = intPtr(7) // max is 6
	svc := newRTSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), in)
	require.ErrorIs(t, err, domain.ErrInvalidRecurringConfig)
	assert.False(t, store.createCalled)
}

func TestCreateRT_EndDate_NotAfterStartDate(t *testing.T) {
	store := defaultRTStore()
	in := baseInput()
	same := in.StartDate
	in.EndDate = &same // equal → invalid (must be strictly after)
	svc := newRTSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), in)
	require.ErrorIs(t, err, domain.ErrInvalidRecurringConfig)
	assert.False(t, store.createCalled)
}

func TestCreateRT_AccountNotFound(t *testing.T) {
	store := defaultRTStore()
	store.getAccount = func(_ context.Context, _ sqlc.GetAccountParams) (sqlc.Account, error) {
		return sqlc.Account{}, pgx.ErrNoRows
	}
	svc := newRTSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), baseInput())
	require.ErrorIs(t, err, domain.ErrAccountNotFound)
	assert.False(t, store.createCalled)
}

func TestCreateRT_CategoryNotFound(t *testing.T) {
	store := defaultRTStore()
	store.getCategory = func(_ context.Context, _ sqlc.GetCategoryForUserParams) (sqlc.Category, error) {
		return sqlc.Category{}, pgx.ErrNoRows
	}
	catID := uuid.New()
	svc := newRTSvc(store)
	in := baseInput()
	in.CategoryID = &catID
	_, err := svc.Create(context.Background(), uuid.New(), in)
	require.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.False(t, store.createCalled)
}

func TestCreateRT_DefaultCurrency(t *testing.T) {
	store := defaultRTStore()
	var captured sqlc.CreateRecurringTransactionParams
	store.createRT = func(_ context.Context, arg sqlc.CreateRecurringTransactionParams) (sqlc.RecurringTransaction, error) {
		captured = arg
		return sqlc.RecurringTransaction{ID: uuid.New()}, nil
	}
	svc := newRTSvc(store)
	in := baseInput()
	in.Currency = "" // omitted → defaults to COP
	_, err := svc.Create(context.Background(), uuid.New(), in)
	require.NoError(t, err)
	assert.Equal(t, "COP", captured.Currency)
	assert.True(t, store.createCalled)
}

func TestDeleteRT_NotFound(t *testing.T) {
	store := defaultRTStore()
	store.softDeleteRT = func(_ context.Context, _ sqlc.SoftDeleteRecurringTransactionParams) (uuid.UUID, error) {
		return uuid.UUID{}, pgx.ErrNoRows
	}
	svc := newRTSvc(store)
	err := svc.Delete(context.Background(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, domain.ErrNotFound)
}

// ─── computeNextDueDate table tests ──────────────────────────────────────────

// July 5, 2026 is a Sunday (time.Sunday = 0).
var ndTestStart = time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)

func TestComputeNextDueDate(t *testing.T) {
	tests := []struct {
		name       string
		freq       string
		dayOfMonth *int
		dayOfWeek  *int
		customDays *int
		want       time.Time
	}{
		// ── daily / yearly / custom always return start ──────────────────────
		{
			name: "daily returns start",
			freq: "daily",
			want: ndTestStart,
		},
		{
			name: "yearly returns start",
			freq: "yearly",
			want: ndTestStart,
		},
		{
			name:       "custom returns start",
			freq:       "custom",
			customDays: intPtr(14),
			want:       ndTestStart,
		},
		// ── weekly with day_of_week ──────────────────────────────────────────
		{
			name:      "weekly same weekday as start (Sunday=0)",
			freq:      "weekly",
			dayOfWeek: intPtr(0), // Sunday — same as start
			want:      ndTestStart,
		},
		{
			name:      "weekly next Wednesday from Sunday",
			freq:      "weekly",
			dayOfWeek: intPtr(3), // Wednesday; diff = (3-0+7)%7 = 3
			want:      time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "weekly Saturday from Sunday",
			freq:      "weekly",
			dayOfWeek: intPtr(6), // Saturday; diff = (6-0+7)%7 = 6
			want:      time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "weekly without day_of_week returns start",
			freq: "weekly",
			want: ndTestStart,
		},
		// ── biweekly shares the same weekday logic as weekly ─────────────────
		{
			name:      "biweekly with day_of_week",
			freq:      "biweekly",
			dayOfWeek: intPtr(3), // Wednesday
			want:      time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "biweekly without day_of_week returns start",
			freq: "biweekly",
			want: ndTestStart,
		},
		// ── monthly with day_of_month ────────────────────────────────────────
		{
			// start = July 5, target day = 5 → same day (not Before)
			name:       "monthly same day as start",
			freq:       "monthly",
			dayOfMonth: intPtr(5),
			want:       ndTestStart,
		},
		{
			// start = July 5, target day = 10 → July 10 (future, same month)
			name:       "monthly future day this month",
			freq:       "monthly",
			dayOfMonth: intPtr(10),
			want:       time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		},
		{
			// start = July 5, target day = 3 → July 3 is Before start → August 3
			name:       "monthly past day rolls to next month",
			freq:       "monthly",
			dayOfMonth: intPtr(3),
			want:       time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "monthly without day_of_month returns start",
			freq: "monthly",
			want: ndTestStart,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeNextDueDate(ndTestStart, tc.freq, tc.dayOfMonth, tc.dayOfWeek, tc.customDays)
			assert.Equal(t, tc.want, got, "start=%s freq=%s dayOfMonth=%v dayOfWeek=%v",
				ndTestStart.Format("2006-01-02"), tc.freq, tc.dayOfMonth, tc.dayOfWeek)
		})
	}
}
