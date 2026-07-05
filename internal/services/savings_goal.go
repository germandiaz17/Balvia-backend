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

// ValidGoalStatuses is the set of status values the client may supply.
// The handler validates the incoming status against this set before calling
// the service, so the service itself assumes a valid value.
var ValidGoalStatuses = map[string]bool{
	"active":    true,
	"achieved":  true,
	"abandoned": true,
	"paused":    true,
}

// GoalInput is the validated business input for creating a savings goal.
type GoalInput struct {
	Name            string
	Description     *string
	Icon            *string
	Color           *string
	TargetAmount    decimal.Decimal
	Currency        string
	StartDate       time.Time
	TargetDate      time.Time
	LinkedAccountID *uuid.UUID
}

// GoalUpdateInput is the validated business input for updating a savings goal.
// start_date is not editable; status can be set by the client manually.
type GoalUpdateInput struct {
	Name            string
	Description     *string
	Icon            *string
	Color           *string
	TargetAmount    decimal.Decimal
	TargetDate      time.Time
	Status          string
	LinkedAccountID *uuid.UUID
}

// ContributionInput is the validated input for adding a contribution.
type ContributionInput struct {
	Amount           decimal.Decimal
	ContributionDate *time.Time // nil = default to today in America/Bogota
	Notes            *string
}

// SavingsGoalService manages savings goals and their contributions.
type SavingsGoalService struct {
	store database.Store
	now   func() time.Time
}

// NewSavingsGoalService builds the service. Dates are resolved in
// America/Bogota so "today" matches the Colombian user regardless of server tz.
func NewSavingsGoalService(store database.Store) *SavingsGoalService {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		loc = time.UTC
	}
	return &SavingsGoalService{
		store: store,
		now:   func() time.Time { return time.Now().In(loc) },
	}
}

// Create adds a new savings goal for the user.
func (s *SavingsGoalService) Create(ctx context.Context, userID uuid.UUID, in GoalInput) (sqlc.SavingsGoal, error) {
	if !in.TargetAmount.IsPositive() {
		return sqlc.SavingsGoal{}, domain.ErrInvalidAmount
	}

	if !in.TargetDate.After(in.StartDate) {
		return sqlc.SavingsGoal{}, domain.ErrInvalidGoalDates
	}

	linkedAccID, err := s.resolveLinkedAccount(ctx, userID, in.LinkedAccountID)
	if err != nil {
		return sqlc.SavingsGoal{}, err
	}

	currency := in.Currency
	if currency == "" {
		currency = defaultCurrency
	}

	return s.store.CreateSavingsGoal(ctx, sqlc.CreateSavingsGoalParams{
		UserID:          userID,
		Name:            in.Name,
		Description:     in.Description,
		Icon:            in.Icon,
		Color:           in.Color,
		TargetAmount:    in.TargetAmount,
		Currency:        currency,
		StartDate:       pgtype.Date{Time: dateOnly(in.StartDate), Valid: true},
		TargetDate:      pgtype.Date{Time: dateOnly(in.TargetDate), Valid: true},
		LinkedAccountID: linkedAccID,
	})
}

// List returns all non-deleted savings goals for the user.
func (s *SavingsGoalService) List(ctx context.Context, userID uuid.UUID) ([]sqlc.SavingsGoal, error) {
	return s.store.ListSavingsGoalsForUser(ctx, userID)
}

// Get returns a single savings goal owned by the user.
func (s *SavingsGoalService) Get(ctx context.Context, userID, id uuid.UUID) (sqlc.SavingsGoal, error) {
	goal, err := s.store.GetSavingsGoal(ctx, sqlc.GetSavingsGoalParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.SavingsGoal{}, domain.ErrGoalNotFound
	}
	return goal, err
}

// Update edits the mutable fields of a savings goal. start_date and
// current_amount are never changed here; the status enum is validated.
func (s *SavingsGoalService) Update(ctx context.Context, userID, id uuid.UUID, in GoalUpdateInput) (sqlc.SavingsGoal, error) {
	if !in.TargetAmount.IsPositive() {
		return sqlc.SavingsGoal{}, domain.ErrInvalidAmount
	}

	// Fetch the existing goal so we can validate target_date > start_date.
	existing, err := s.Get(ctx, userID, id)
	if err != nil {
		return sqlc.SavingsGoal{}, err
	}

	if !dateOnly(in.TargetDate).After(dateOnly(existing.StartDate.Time)) {
		return sqlc.SavingsGoal{}, domain.ErrInvalidGoalDates
	}

	linkedAccID, err := s.resolveLinkedAccount(ctx, userID, in.LinkedAccountID)
	if err != nil {
		return sqlc.SavingsGoal{}, err
	}

	goal, err := s.store.UpdateSavingsGoal(ctx, sqlc.UpdateSavingsGoalParams{
		Name:            in.Name,
		Description:     in.Description,
		Icon:            in.Icon,
		Color:           in.Color,
		TargetAmount:    in.TargetAmount,
		TargetDate:      pgtype.Date{Time: dateOnly(in.TargetDate), Valid: true},
		Status:          in.Status,
		LinkedAccountID: linkedAccID,
		ID:              id,
		UserID:          userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.SavingsGoal{}, domain.ErrGoalNotFound
	}
	return goal, err
}

// Delete soft-deletes a savings goal owned by the user.
func (s *SavingsGoalService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.store.SoftDeleteSavingsGoal(ctx, sqlc.SoftDeleteSavingsGoalParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrGoalNotFound
	}
	return err
}

// AddContribution adds a contribution to a savings goal atomically. The
// contribution is tied to the user's active tracking period. current_amount is
// updated inside the same transaction and the goal is marked achieved if the
// target is reached.
func (s *SavingsGoalService) AddContribution(ctx context.Context, userID, goalID uuid.UUID, in ContributionInput) (database.CreateContributionTxResult, error) {
	if !in.Amount.IsPositive() {
		return database.CreateContributionTxResult{}, domain.ErrInvalidAmount
	}

	// Verify goal ownership before resolving the active period.
	if _, err := s.Get(ctx, userID, goalID); err != nil {
		return database.CreateContributionTxResult{}, err
	}

	period, err := s.activePeriod(ctx, userID)
	if err != nil {
		return database.CreateContributionTxResult{}, err
	}

	contribDate := pgtype.Date{Time: dateOnly(s.now()), Valid: true}
	if in.ContributionDate != nil {
		contribDate = pgtype.Date{Time: dateOnly(*in.ContributionDate), Valid: true}
	}

	return s.store.CreateContributionTx(ctx, database.CreateContributionTxParams{
		SavingsGoalID:    goalID,
		UserID:           userID,
		TrackingPeriodID: period.ID,
		Amount:           in.Amount,
		ContributionDate: contribDate,
		Notes:            in.Notes,
	})
}

// ListContributions returns all contributions for a goal owned by the user.
func (s *SavingsGoalService) ListContributions(ctx context.Context, userID, goalID uuid.UUID) ([]sqlc.SavingsGoalContribution, error) {
	// Verify goal ownership first.
	if _, err := s.Get(ctx, userID, goalID); err != nil {
		return nil, err
	}
	return s.store.ListContributionsForGoal(ctx, sqlc.ListContributionsForGoalParams{
		SavingsGoalID: goalID,
		UserID:        userID,
	})
}

// resolveLinkedAccount validates that the account belongs to the user and
// returns a NullUUID suitable for the store. Returns ErrAccountNotFound when
// the account does not exist or is not the user's.
func (s *SavingsGoalService) resolveLinkedAccount(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID) (uuid.NullUUID, error) {
	if accountID == nil {
		return uuid.NullUUID{}, nil
	}
	if _, err := s.store.GetAccount(ctx, sqlc.GetAccountParams{
		ID:     *accountID,
		UserID: userID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.NullUUID{}, domain.ErrAccountNotFound
		}
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: *accountID, Valid: true}, nil
}

// activePeriod returns the user's active period, lazily closing (and rolling
// over) any that have already ended. Mirrors BudgetService.activePeriod.
func (s *SavingsGoalService) activePeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
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

		if _, err := s.store.ClosePeriodTx(ctx, period.ID, userID); err != nil {
			return sqlc.TrackingPeriod{}, err
		}
	}

	return sqlc.TrackingPeriod{}, domain.ErrNoActivePeriod
}
