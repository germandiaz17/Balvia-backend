package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// CreateContributionTxParams carries the inputs for CreateContributionTx. The
// TrackingPeriodID must already be resolved by the caller (active period).
type CreateContributionTxParams struct {
	SavingsGoalID    uuid.UUID
	UserID           uuid.UUID
	TrackingPeriodID uuid.UUID
	Amount           decimal.Decimal
	ContributionDate pgtype.Date
	Notes            *string
}

// CreateContributionTxResult holds both the new contribution row and the
// updated savings goal (with current_amount and possibly status updated).
type CreateContributionTxResult struct {
	Contribution sqlc.SavingsGoalContribution
	Goal         sqlc.SavingsGoal
}

// CreateContributionTx implements Store. In a single transaction it:
//  1. verifies the goal exists and belongs to the user,
//  2. inserts the contribution row,
//  3. increments current_amount on the goal (and marks it achieved if met).
func (s *SQLStore) CreateContributionTx(ctx context.Context, arg CreateContributionTxParams) (CreateContributionTxResult, error) {
	var res CreateContributionTxResult

	err := s.execTx(ctx, func(q *sqlc.Queries) error {
		// Verify goal ownership. GetSavingsGoal already filters deleted_at IS NULL.
		_, err := q.GetSavingsGoal(ctx, sqlc.GetSavingsGoalParams{
			ID:     arg.SavingsGoalID,
			UserID: arg.UserID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrGoalNotFound
			}
			return fmt.Errorf("get savings goal: %w", err)
		}

		contrib, err := q.CreateSavingsGoalContribution(ctx, sqlc.CreateSavingsGoalContributionParams{
			SavingsGoalID:    arg.SavingsGoalID,
			UserID:           arg.UserID,
			TrackingPeriodID: arg.TrackingPeriodID,
			Amount:           arg.Amount,
			ContributionDate: arg.ContributionDate,
			Notes:            arg.Notes,
		})
		if err != nil {
			return fmt.Errorf("insert contribution: %w", err)
		}
		res.Contribution = contrib

		updated, err := q.ApplyGoalContribution(ctx, sqlc.ApplyGoalContributionParams{
			Amount: arg.Amount,
			ID:     arg.SavingsGoalID,
			UserID: arg.UserID,
		})
		if err != nil {
			return fmt.Errorf("apply goal contribution: %w", err)
		}
		res.Goal = updated

		return nil
	})

	return res, err
}
