package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// Default alert thresholds (percent of the budgeted amount) when the caller
// does not specify them. They mirror the DB column defaults.
var (
	defaultWarningThreshold  = decimal.RequireFromString("80")
	defaultCriticalThreshold = decimal.RequireFromString("100")
	hundred                  = decimal.RequireFromString("100")
)

// BudgetService manages per-category budgets. Every budget is anchored to a
// tracking period: new budgets go to the user's active period, and budgets of a
// closed (immutable) period cannot be modified.
type BudgetService struct {
	store database.Store
	now   func() time.Time
}

// NewBudgetService builds the service. "Today" is computed in America/Bogota so
// period roll-over matches the Colombian user regardless of server tz.
func NewBudgetService(store database.Store) *BudgetService {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		loc = time.UTC
	}
	return &BudgetService{
		store: store,
		now:   func() time.Time { return time.Now().In(loc) },
	}
}

// BudgetInput is the validated business input for creating/updating a budget.
// Optional fields are nil when the caller omits them.
type BudgetInput struct {
	CategoryID        *uuid.UUID
	Amount            decimal.Decimal
	Currency          string
	WarningThreshold  *decimal.Decimal
	CriticalThreshold *decimal.Decimal
	Notes             *string
}

// Create adds a budget to the user's active tracking period.
func (s *BudgetService) Create(ctx context.Context, userID uuid.UUID, in BudgetInput) (sqlc.Budget, error) {
	warning, critical, err := s.normalize(in)
	if err != nil {
		return sqlc.Budget{}, err
	}

	period, err := s.activePeriod(ctx, userID)
	if err != nil {
		return sqlc.Budget{}, err
	}

	category, err := s.resolveCategory(ctx, userID, in.CategoryID)
	if err != nil {
		return sqlc.Budget{}, err
	}

	currency := in.Currency
	if currency == "" {
		currency = defaultCurrency
	}

	budget, err := s.store.CreateBudget(ctx, sqlc.CreateBudgetParams{
		UserID:                 userID,
		TrackingPeriodID:       period.ID,
		CategoryID:             category,
		Amount:                 in.Amount,
		Currency:               currency,
		AlertThresholdWarning:  warning,
		AlertThresholdCritical: critical,
		Notes:                  in.Notes,
	})
	if isUniqueViolation(err) {
		return sqlc.Budget{}, domain.ErrBudgetExists
	}
	return budget, err
}

// List returns the user's budgets for a tracking period. If periodID is nil the
// active period is used.
func (s *BudgetService) List(ctx context.Context, userID uuid.UUID, periodID *uuid.UUID) ([]sqlc.Budget, error) {
	pid := uuid.Nil
	if periodID != nil {
		pid = *periodID
	} else {
		period, err := s.activePeriod(ctx, userID)
		if err != nil {
			return nil, err
		}
		pid = period.ID
	}

	return s.store.ListBudgetsForUser(ctx, sqlc.ListBudgetsForUserParams{
		UserID:           userID,
		TrackingPeriodID: pid,
	})
}

// Get returns a single budget owned by the user.
func (s *BudgetService) Get(ctx context.Context, userID, id uuid.UUID) (sqlc.Budget, error) {
	budget, err := s.store.GetBudget(ctx, sqlc.GetBudgetParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Budget{}, domain.ErrNotFound
	}
	return budget, err
}

// Update re-specifies a budget's amount, category, thresholds and notes. It
// refuses to touch budgets of a closed (immutable) period.
func (s *BudgetService) Update(ctx context.Context, userID, id uuid.UUID, in BudgetInput) (sqlc.Budget, error) {
	warning, critical, err := s.normalize(in)
	if err != nil {
		return sqlc.Budget{}, err
	}

	if err := s.ensureMutable(ctx, userID, id); err != nil {
		return sqlc.Budget{}, err
	}

	category, err := s.resolveCategory(ctx, userID, in.CategoryID)
	if err != nil {
		return sqlc.Budget{}, err
	}

	currency := in.Currency
	if currency == "" {
		currency = defaultCurrency
	}

	budget, err := s.store.UpdateBudget(ctx, sqlc.UpdateBudgetParams{
		CategoryID:             category,
		Amount:                 in.Amount,
		Currency:               currency,
		AlertThresholdWarning:  warning,
		AlertThresholdCritical: critical,
		Notes:                  in.Notes,
		ID:                     id,
		UserID:                 userID,
	})
	if isUniqueViolation(err) {
		return sqlc.Budget{}, domain.ErrBudgetExists
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Budget{}, domain.ErrNotFound
	}
	return budget, err
}

// Delete removes a budget. Budgets of a closed period cannot be deleted.
func (s *BudgetService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	if err := s.ensureMutable(ctx, userID, id); err != nil {
		return err
	}
	_, err := s.store.DeleteBudget(ctx, sqlc.DeleteBudgetParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

// normalize validates the amount and thresholds and applies defaults, returning
// the effective warning/critical thresholds.
func (s *BudgetService) normalize(in BudgetInput) (warning, critical decimal.Decimal, err error) {
	if !in.Amount.IsPositive() {
		return warning, critical, domain.ErrInvalidAmount
	}

	warning = defaultWarningThreshold
	if in.WarningThreshold != nil {
		warning = *in.WarningThreshold
	}
	critical = defaultCriticalThreshold
	if in.CriticalThreshold != nil {
		critical = *in.CriticalThreshold
	}
	for _, t := range []decimal.Decimal{warning, critical} {
		if t.IsNegative() || t.GreaterThan(hundred) {
			return warning, critical, domain.ErrInvalidThreshold
		}
	}
	return warning, critical, nil
}

// ensureMutable loads the budget (checking ownership) and refuses if its period
// is closed.
func (s *BudgetService) ensureMutable(ctx context.Context, userID, id uuid.UUID) error {
	budget, err := s.store.GetBudget(ctx, sqlc.GetBudgetParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	} else if err != nil {
		return err
	}
	period, err := s.store.GetTrackingPeriodByID(ctx, budget.TrackingPeriodID)
	if err != nil {
		return err
	}
	if period.Status == "closed" {
		return domain.ErrPeriodClosed
	}
	return nil
}

func (s *BudgetService) resolveCategory(ctx context.Context, userID uuid.UUID, categoryID *uuid.UUID) (uuid.NullUUID, error) {
	if categoryID == nil {
		return uuid.NullUUID{}, nil
	}
	if _, err := s.store.GetCategoryForUser(ctx, sqlc.GetCategoryForUserParams{
		ID:     *categoryID,
		UserID: uuid.NullUUID{UUID: userID, Valid: true},
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.NullUUID{}, domain.ErrCategoryNotFound
		}
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: *categoryID, Valid: true}, nil
}

// activePeriod returns the user's active period, lazily closing (and rolling
// over) any that have already ended — a fallback for when the nightly scheduler
// hasn't run. Mirrors TransactionService.activePeriod.
func (s *BudgetService) activePeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
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
