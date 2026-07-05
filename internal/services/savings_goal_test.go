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

// ─── Fake store ─────────────────────────────────────────────────────────────

// mockGoalStore embeds database.Store so only the methods under test need to
// be implemented; unexpected calls panic to make test failures obvious.
type mockGoalStore struct {
	database.Store

	getActivePeriod      func(context.Context, uuid.UUID) (sqlc.TrackingPeriod, error)
	getAccount           func(context.Context, sqlc.GetAccountParams) (sqlc.Account, error)
	createGoal           func(context.Context, sqlc.CreateSavingsGoalParams) (sqlc.SavingsGoal, error)
	getGoal              func(context.Context, sqlc.GetSavingsGoalParams) (sqlc.SavingsGoal, error)
	createContributionTx func(context.Context, database.CreateContributionTxParams) (database.CreateContributionTxResult, error)
	closePeriodTx        func(context.Context, uuid.UUID, uuid.UUID) (database.ClosePeriodResult, error)
}

func (m *mockGoalStore) GetActiveTrackingPeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
	return m.getActivePeriod(ctx, userID)
}
func (m *mockGoalStore) GetAccount(ctx context.Context, arg sqlc.GetAccountParams) (sqlc.Account, error) {
	return m.getAccount(ctx, arg)
}
func (m *mockGoalStore) CreateSavingsGoal(ctx context.Context, arg sqlc.CreateSavingsGoalParams) (sqlc.SavingsGoal, error) {
	return m.createGoal(ctx, arg)
}
func (m *mockGoalStore) GetSavingsGoal(ctx context.Context, arg sqlc.GetSavingsGoalParams) (sqlc.SavingsGoal, error) {
	return m.getGoal(ctx, arg)
}
func (m *mockGoalStore) CreateContributionTx(ctx context.Context, arg database.CreateContributionTxParams) (database.CreateContributionTxResult, error) {
	return m.createContributionTx(ctx, arg)
}
func (m *mockGoalStore) ClosePeriodTx(ctx context.Context, periodID, userID uuid.UUID) (database.ClosePeriodResult, error) {
	if m.closePeriodTx != nil {
		return m.closePeriodTx(ctx, periodID, userID)
	}
	return database.ClosePeriodResult{}, nil
}

// newGoalSvc pins "today" to a fixed date (2026-07-04) inside the active period.
func newGoalSvc(store database.Store) *SavingsGoalService {
	return &SavingsGoalService{
		store: store,
		now:   func() time.Time { return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC) },
	}
}

// activePeriodStub returns a period that spans 2026-07-04 → 2026-08-02.
func activePeriodStub() sqlc.TrackingPeriod {
	return sqlc.TrackingPeriod{
		ID:        uuid.New(),
		StartDate: pgDateOf(time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)),
		EndDate:   pgDateOf(time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)),
		Status:    "active",
	}
}

// ─── Create tests ────────────────────────────────────────────────────────────

func TestCreateGoal_InvalidAmount(t *testing.T) {
	store := &mockGoalStore{}
	svc := newGoalSvc(store)

	_, err := svc.Create(context.Background(), uuid.New(), GoalInput{
		Name:         "Vacaciones",
		TargetAmount: decimal.NewFromInt(0),
		StartDate:    time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		TargetDate:   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
	})
	require.ErrorIs(t, err, domain.ErrInvalidAmount)
}

func TestCreateGoal_InvalidDates_TargetBeforeStart(t *testing.T) {
	store := &mockGoalStore{}
	svc := newGoalSvc(store)

	_, err := svc.Create(context.Background(), uuid.New(), GoalInput{
		Name:         "Meta",
		TargetAmount: decimal.NewFromInt(1000000),
		StartDate:    time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		TargetDate:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), // before start
	})
	require.ErrorIs(t, err, domain.ErrInvalidGoalDates)
}

func TestCreateGoal_InvalidDates_TargetEqualsStart(t *testing.T) {
	store := &mockGoalStore{}
	svc := newGoalSvc(store)

	sameDay := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	_, err := svc.Create(context.Background(), uuid.New(), GoalInput{
		Name:         "Meta",
		TargetAmount: decimal.NewFromInt(1000000),
		StartDate:    sameDay,
		TargetDate:   sameDay, // equal, not strictly after
	})
	require.ErrorIs(t, err, domain.ErrInvalidGoalDates)
}

func TestCreateGoal_HappyPath(t *testing.T) {
	goalID := uuid.New()
	store := &mockGoalStore{
		createGoal: func(_ context.Context, arg sqlc.CreateSavingsGoalParams) (sqlc.SavingsGoal, error) {
			return sqlc.SavingsGoal{
				ID:            goalID,
				Name:          arg.Name,
				TargetAmount:  arg.TargetAmount,
				CurrentAmount: decimal.Zero,
				Currency:      arg.Currency,
				Status:        "active",
			}, nil
		},
	}
	svc := newGoalSvc(store)

	goal, err := svc.Create(context.Background(), uuid.New(), GoalInput{
		Name:         "Fondo de emergencia",
		TargetAmount: decimal.NewFromInt(5000000),
		StartDate:    time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		TargetDate:   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	assert.Equal(t, goalID, goal.ID)
	assert.Equal(t, "COP", goal.Currency, "currency defaults to COP")
}

// ─── AddContribution / achieved tests ────────────────────────────────────────

// TestAddContribution_GoalAchieved verifies that when current_amount + contribution
// meets the target, the returned goal has status = "achieved".
func TestAddContribution_GoalAchieved(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	periodID := uuid.New()

	// Goal is at 90_000 out of 100_000.
	existingGoal := sqlc.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		TargetAmount:  decimal.NewFromInt(100_000),
		CurrentAmount: decimal.NewFromInt(90_000),
		Status:        "active",
	}
	// After adding 10_000 it reaches exactly 100_000 → achieved.
	achievedGoal := sqlc.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		TargetAmount:  decimal.NewFromInt(100_000),
		CurrentAmount: decimal.NewFromInt(100_000),
		Status:        "achieved",
	}

	store := &mockGoalStore{
		getActivePeriod: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return sqlc.TrackingPeriod{
				ID:        periodID,
				StartDate: pgDateOf(time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)),
				EndDate:   pgDateOf(time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)),
				Status:    "active",
			}, nil
		},
		getGoal: func(_ context.Context, _ sqlc.GetSavingsGoalParams) (sqlc.SavingsGoal, error) {
			return existingGoal, nil
		},
		createContributionTx: func(_ context.Context, arg database.CreateContributionTxParams) (database.CreateContributionTxResult, error) {
			return database.CreateContributionTxResult{
				Contribution: sqlc.SavingsGoalContribution{
					ID:               uuid.New(),
					SavingsGoalID:    goalID,
					UserID:           userID,
					TrackingPeriodID: periodID,
					Amount:           arg.Amount,
				},
				Goal: achievedGoal,
			}, nil
		},
	}

	svc := newGoalSvc(store)
	result, err := svc.AddContribution(context.Background(), userID, goalID, ContributionInput{
		Amount: decimal.NewFromInt(10_000),
	})

	require.NoError(t, err)
	assert.Equal(t, "achieved", result.Goal.Status)
	assert.True(t, result.Goal.CurrentAmount.Equal(decimal.NewFromInt(100_000)))
}

// TestAddContribution_GoalNotAchievedYet verifies partial contributions keep
// the goal in "active" status.
func TestAddContribution_GoalNotAchievedYet(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	periodID := uuid.New()

	store := &mockGoalStore{
		getActivePeriod: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return sqlc.TrackingPeriod{
				ID:        periodID,
				StartDate: pgDateOf(time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)),
				EndDate:   pgDateOf(time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)),
				Status:    "active",
			}, nil
		},
		getGoal: func(_ context.Context, _ sqlc.GetSavingsGoalParams) (sqlc.SavingsGoal, error) {
			return sqlc.SavingsGoal{
				ID:            goalID,
				TargetAmount:  decimal.NewFromInt(100_000),
				CurrentAmount: decimal.NewFromInt(10_000),
				Status:        "active",
			}, nil
		},
		createContributionTx: func(_ context.Context, arg database.CreateContributionTxParams) (database.CreateContributionTxResult, error) {
			return database.CreateContributionTxResult{
				Contribution: sqlc.SavingsGoalContribution{Amount: arg.Amount},
				Goal: sqlc.SavingsGoal{
					Status:        "active",
					CurrentAmount: decimal.NewFromInt(20_000),
				},
			}, nil
		},
	}

	svc := newGoalSvc(store)
	result, err := svc.AddContribution(context.Background(), userID, goalID, ContributionInput{
		Amount: decimal.NewFromInt(10_000),
	})

	require.NoError(t, err)
	assert.Equal(t, "active", result.Goal.Status)
}

// TestAddContribution_NoActivePeriod verifies the correct domain error is
// returned when there is no active tracking period.
func TestAddContribution_NoActivePeriod(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()

	store := &mockGoalStore{
		getGoal: func(_ context.Context, _ sqlc.GetSavingsGoalParams) (sqlc.SavingsGoal, error) {
			return sqlc.SavingsGoal{ID: goalID}, nil
		},
		getActivePeriod: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return sqlc.TrackingPeriod{}, pgx.ErrNoRows
		},
	}

	svc := newGoalSvc(store)
	_, err := svc.AddContribution(context.Background(), userID, goalID, ContributionInput{
		Amount: decimal.NewFromInt(5_000),
	})
	require.ErrorIs(t, err, domain.ErrNoActivePeriod)
}

// TestAddContribution_GoalNotFound verifies ErrGoalNotFound when the goal is
// not owned by the user.
func TestAddContribution_GoalNotFound(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()

	store := &mockGoalStore{
		getGoal: func(_ context.Context, _ sqlc.GetSavingsGoalParams) (sqlc.SavingsGoal, error) {
			return sqlc.SavingsGoal{}, pgx.ErrNoRows
		},
	}

	svc := newGoalSvc(store)
	_, err := svc.AddContribution(context.Background(), userID, goalID, ContributionInput{
		Amount: decimal.NewFromInt(5_000),
	})
	require.ErrorIs(t, err, domain.ErrGoalNotFound)
}

// TestAddContribution_InvalidAmount ensures zero/negative amounts are rejected.
func TestAddContribution_InvalidAmount(t *testing.T) {
	svc := newGoalSvc(&mockGoalStore{})
	_, err := svc.AddContribution(context.Background(), uuid.New(), uuid.New(), ContributionInput{
		Amount: decimal.NewFromInt(-1),
	})
	require.ErrorIs(t, err, domain.ErrInvalidAmount)
}
