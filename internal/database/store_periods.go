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

// ClosePeriodResult holds the artifacts produced by closing a tracking period.
type ClosePeriodResult struct {
	Closed        sqlc.TrackingPeriod
	Next          sqlc.TrackingPeriod
	Summary       sqlc.TrackingPeriodSummary
	BudgetsCopied int
}

// ClosePeriodTx implements Store. In a single transaction it:
//  1. marks the period closed (only if currently active),
//  2. snapshots a summary (core financial aggregates),
//  3. generates the next contiguous period (start = closed end + 1 day),
//  4. copies the closed period's budgets onto the new period.
func (s *SQLStore) ClosePeriodTx(ctx context.Context, periodID, userID uuid.UUID) (ClosePeriodResult, error) {
	var res ClosePeriodResult

	err := s.execTx(ctx, func(q *sqlc.Queries) error {
		closed, err := q.ClosePeriod(ctx, periodID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound // not found or already closed
			}
			return fmt.Errorf("close period: %w", err)
		}
		res.Closed = closed

		summary, err := buildSummary(ctx, q, closed, userID)
		if err != nil {
			return err
		}
		res.Summary = summary

		settings, err := q.GetUserSettingsByUserID(ctx, userID)
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}

		start := closed.EndDate.Time.AddDate(0, 0, 1)
		end := start.AddDate(0, 0, int(settings.TrackingDurationDays)-1)
		next, err := q.CreateTrackingPeriod(ctx, sqlc.CreateTrackingPeriodParams{
			UserID:             userID,
			StartDate:          pgtype.Date{Time: start, Valid: true},
			EndDate:            pgtype.Date{Time: end, Valid: true},
			Status:             "active",
			SequenceNumber:     closed.SequenceNumber + 1,
			ConfigStartDay:     settings.TrackingStartDay,
			ConfigDurationDays: settings.TrackingDurationDays,
		})
		if err != nil {
			return fmt.Errorf("create next period: %w", err)
		}
		res.Next = next

		budgets, err := q.ListBudgetsByPeriod(ctx, periodID)
		if err != nil {
			return fmt.Errorf("list budgets: %w", err)
		}
		for _, b := range budgets {
			if _, err := q.CreateBudget(ctx, sqlc.CreateBudgetParams{
				UserID:                 userID,
				TrackingPeriodID:       next.ID,
				CategoryID:             b.CategoryID,
				Amount:                 b.Amount,
				Currency:               b.Currency,
				AlertThresholdWarning:  b.AlertThresholdWarning,
				AlertThresholdCritical: b.AlertThresholdCritical,
				Notes:                  b.Notes,
			}); err != nil {
				return fmt.Errorf("copy budget: %w", err)
			}
		}
		res.BudgetsCopied = len(budgets)

		return nil
	})

	return res, err
}

// buildSummary computes the core financial snapshot for a closed period.
func buildSummary(ctx context.Context, q *sqlc.Queries, period sqlc.TrackingPeriod, userID uuid.UUID) (sqlc.TrackingPeriodSummary, error) {
	totals, err := q.SummarizePeriodTotals(ctx, period.ID)
	if err != nil {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("summarize totals: %w", err)
	}

	netSavings := totals.TotalIncome.Sub(totals.TotalExpenses)

	var savingsRate decimal.NullDecimal
	if totals.TotalIncome.IsPositive() {
		rate := netSavings.Div(totals.TotalIncome).Mul(decimal.NewFromInt(100)).Round(2)
		savingsRate = decimal.NullDecimal{Decimal: rate, Valid: true}
	}

	var topCatID uuid.NullUUID
	var topCatAmt decimal.NullDecimal
	if top, err := q.GetTopExpenseCategory(ctx, period.ID); err == nil {
		topCatID = top.CategoryID
		topCatAmt = decimal.NullDecimal{Decimal: top.Total, Valid: true}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.TrackingPeriodSummary{}, fmt.Errorf("top expense category: %w", err)
	}

	return q.CreateTrackingPeriodSummary(ctx, sqlc.CreateTrackingPeriodSummaryParams{
		TrackingPeriodID:         period.ID,
		UserID:                   userID,
		TotalIncome:              totals.TotalIncome,
		TotalExpenses:            totals.TotalExpenses,
		TotalTransfers:           totals.TotalTransfers,
		NetSavings:               netSavings,
		SavingsRate:              savingsRate,
		TransactionCount:         totals.TransactionCount,
		ExpenseTransactionCount:  totals.ExpenseTransactionCount,
		IncomeTransactionCount:   totals.IncomeTransactionCount,
		TopExpenseCategoryID:     topCatID,
		TopExpenseCategoryAmount: topCatAmt,
	})
}
