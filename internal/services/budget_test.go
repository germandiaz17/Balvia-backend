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

type mockBudgetStore struct {
	database.Store
	getActivePeriod func(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error)
	getCategory     func(ctx context.Context, arg sqlc.GetCategoryForUserParams) (sqlc.Category, error)
	createBudget    func(ctx context.Context, arg sqlc.CreateBudgetParams) (sqlc.Budget, error)
	getBudget       func(ctx context.Context, arg sqlc.GetBudgetParams) (sqlc.Budget, error)
	getPeriodByID   func(ctx context.Context, id uuid.UUID) (sqlc.TrackingPeriod, error)
	updateBudget    func(ctx context.Context, arg sqlc.UpdateBudgetParams) (sqlc.Budget, error)
	createCalled    bool
}

func (m *mockBudgetStore) GetActiveTrackingPeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
	return m.getActivePeriod(ctx, userID)
}
func (m *mockBudgetStore) GetCategoryForUser(ctx context.Context, arg sqlc.GetCategoryForUserParams) (sqlc.Category, error) {
	return m.getCategory(ctx, arg)
}
func (m *mockBudgetStore) CreateBudget(ctx context.Context, arg sqlc.CreateBudgetParams) (sqlc.Budget, error) {
	m.createCalled = true
	return m.createBudget(ctx, arg)
}
func (m *mockBudgetStore) GetBudget(ctx context.Context, arg sqlc.GetBudgetParams) (sqlc.Budget, error) {
	return m.getBudget(ctx, arg)
}
func (m *mockBudgetStore) GetTrackingPeriodByID(ctx context.Context, id uuid.UUID) (sqlc.TrackingPeriod, error) {
	return m.getPeriodByID(ctx, id)
}
func (m *mockBudgetStore) UpdateBudget(ctx context.Context, arg sqlc.UpdateBudgetParams) (sqlc.Budget, error) {
	return m.updateBudget(ctx, arg)
}

func budgetStore() *mockBudgetStore {
	periodID := uuid.New()
	return &mockBudgetStore{
		getActivePeriod: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return sqlc.TrackingPeriod{
				ID:        periodID,
				StartDate: pgDateOf(time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)),
				EndDate:   pgDateOf(time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)),
				Status:    "active",
			}, nil
		},
		getCategory: func(_ context.Context, _ sqlc.GetCategoryForUserParams) (sqlc.Category, error) {
			return sqlc.Category{ID: uuid.New()}, nil
		},
		createBudget: func(_ context.Context, arg sqlc.CreateBudgetParams) (sqlc.Budget, error) {
			return sqlc.Budget{ID: uuid.New(), TrackingPeriodID: arg.TrackingPeriodID}, nil
		},
	}
}

func newBudgetSvc(store database.Store) *BudgetService {
	return &BudgetService{store: store, now: func() time.Time {
		return time.Date(2026, 6, 25, 14, 0, 0, 0, time.UTC)
	}}
}

func TestCreateBudget_HappyDefaults(t *testing.T) {
	store := budgetStore()
	var captured sqlc.CreateBudgetParams
	store.createBudget = func(_ context.Context, arg sqlc.CreateBudgetParams) (sqlc.Budget, error) {
		captured = arg
		return sqlc.Budget{ID: uuid.New()}, nil
	}

	svc := newBudgetSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), BudgetInput{
		Amount: decimal.RequireFromString("300000"),
	})

	require.NoError(t, err)
	assert.True(t, captured.Amount.Equal(decimal.RequireFromString("300000")))
	assert.Equal(t, "COP", captured.Currency, "currency defaults to COP")
	assert.True(t, captured.AlertThresholdWarning.Equal(decimal.RequireFromString("80")))
	assert.True(t, captured.AlertThresholdCritical.Equal(decimal.RequireFromString("100")))
	assert.False(t, captured.CategoryID.Valid, "no category => overall budget")
}

func TestCreateBudget_InvalidAmount(t *testing.T) {
	store := budgetStore()
	svc := newBudgetSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), BudgetInput{
		Amount: decimal.RequireFromString("-1"),
	})
	require.ErrorIs(t, err, domain.ErrInvalidAmount)
	assert.False(t, store.createCalled)
}

func TestCreateBudget_InvalidThreshold(t *testing.T) {
	store := budgetStore()
	over := decimal.RequireFromString("120")
	svc := newBudgetSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), BudgetInput{
		Amount:           decimal.RequireFromString("1000"),
		WarningThreshold: &over,
	})
	require.ErrorIs(t, err, domain.ErrInvalidThreshold)
	assert.False(t, store.createCalled)
}

func TestCreateBudget_NoActivePeriod(t *testing.T) {
	store := budgetStore()
	store.getActivePeriod = func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
		return sqlc.TrackingPeriod{}, pgx.ErrNoRows
	}
	svc := newBudgetSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), BudgetInput{
		Amount: decimal.RequireFromString("1000"),
	})
	require.ErrorIs(t, err, domain.ErrNoActivePeriod)
	assert.False(t, store.createCalled)
}

func TestCreateBudget_CategoryNotFound(t *testing.T) {
	store := budgetStore()
	store.getCategory = func(_ context.Context, _ sqlc.GetCategoryForUserParams) (sqlc.Category, error) {
		return sqlc.Category{}, pgx.ErrNoRows
	}
	svc := newBudgetSvc(store)
	catID := uuid.New()
	_, err := svc.Create(context.Background(), uuid.New(), BudgetInput{
		Amount:     decimal.RequireFromString("1000"),
		CategoryID: &catID,
	})
	require.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.False(t, store.createCalled)
}

func TestUpdateBudget_RefusesClosedPeriod(t *testing.T) {
	store := budgetStore()
	budgetID := uuid.New()
	closedPeriodID := uuid.New()
	store.getBudget = func(_ context.Context, _ sqlc.GetBudgetParams) (sqlc.Budget, error) {
		return sqlc.Budget{ID: budgetID, TrackingPeriodID: closedPeriodID}, nil
	}
	store.getPeriodByID = func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
		return sqlc.TrackingPeriod{ID: closedPeriodID, Status: "closed"}, nil
	}
	updateCalled := false
	store.updateBudget = func(_ context.Context, arg sqlc.UpdateBudgetParams) (sqlc.Budget, error) {
		updateCalled = true
		return sqlc.Budget{}, nil
	}

	svc := newBudgetSvc(store)
	_, err := svc.Update(context.Background(), uuid.New(), budgetID, BudgetInput{
		Amount: decimal.RequireFromString("5000"),
	})
	require.ErrorIs(t, err, domain.ErrPeriodClosed)
	assert.False(t, updateCalled)
}
