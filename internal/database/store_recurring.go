package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
)

// decimalOne is the sign multiplier for applying a positive balance effect.
var decimalOne = decimal.NewFromInt(1)

// dateOnlyUTC normalises a time.Time to UTC midnight so DATE comparisons are
// stable regardless of the source timezone (mirrors services.dateOnly).
func dateOnlyUTC(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// pgxNullUUID wraps a uuid.UUID in a uuid.NullUUID with Valid = true.
func pgxNullUUID(id uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: id, Valid: true}
}

// MaterialiseRecurringTx implements Store. In a single transaction it:
//  1. Checks idempotency: if a non-deleted transaction for
//     (template.ID, occurrenceDate) already exists, returns it immediately.
//  2. Inserts the transaction and applies the balance effect on the account.
//  3. Advances the template: sets last_generated_date and next_due_date,
//     and deactivates it when arg.StillActive is false.
func (s *SQLStore) MaterialiseRecurringTx(ctx context.Context, arg MaterialiseRecurringParams) (sqlc.Transaction, error) {
	var txn sqlc.Transaction

	err := s.execTx(ctx, func(q *sqlc.Queries) error {
		occDate := pgtype.Date{Time: dateOnlyUTC(arg.OccurrenceDate), Valid: true}

		// ── Idempotency check ──────────────────────────────────────────────────
		// We check against occurrence_date (the original scheduled date), NOT
		// transaction_date (which may be clamped to the period bounds). This
		// ensures that catch-up occurrences with different original dates are
		// each generated exactly once even when they all fall in the same period.
		exists, err := q.HasRecurringTransactionForDate(ctx, sqlc.HasRecurringTransactionForDateParams{
			RecurringTransactionID: pgxNullUUID(arg.Template.ID),
			OccurrenceDate:         occDate,
		})
		if err != nil {
			return fmt.Errorf("idempotency check: %w", err)
		}
		if exists {
			// Already generated; advance the template state and return early.
			// We still need to advance next_due_date so the engine doesn't loop.
			return advanceTemplate(ctx, q, arg)
		}

		// ── Insert the transaction ─────────────────────────────────────────────
		txDate := pgtype.Date{Time: dateOnlyUTC(arg.TransactionDate), Valid: true}
		rtID := pgxNullUUID(arg.Template.ID)

		t, err := q.CreateTransaction(ctx, sqlc.CreateTransactionParams{
			UserID:                 arg.Template.UserID,
			TrackingPeriodID:       arg.TrackingPeriodID,
			AccountID:              arg.Template.AccountID,
			CategoryID:             arg.Template.CategoryID,
			TransactionType:        arg.Template.TransactionType,
			Amount:                 arg.Template.Amount,
			Currency:               arg.Template.Currency,
			Description:            arg.Template.Description,
			TransactionDate:        txDate,
			RecurringTransactionID: rtID,
			OccurrenceDate:         occDate, // the original scheduled occurrence date
		})
		if err != nil {
			return fmt.Errorf("insert recurring transaction: %w", err)
		}
		txn = t

		// ── Adjust account balance ─────────────────────────────────────────────
		if err := applyBalanceEffect(ctx, q, t, decimalOne); err != nil {
			return err
		}

		// ── Advance the template ───────────────────────────────────────────────
		return advanceTemplate(ctx, q, arg)
	})

	return txn, err
}

// advanceTemplate updates the recurring template's last_generated_date,
// next_due_date and is_active fields after a successful (or idempotent)
// materialisation step.
func advanceTemplate(ctx context.Context, q *sqlc.Queries, arg MaterialiseRecurringParams) error {
	var nextDue pgtype.Date
	if arg.NextDueDate != nil {
		nextDue = pgtype.Date{Time: dateOnlyUTC(*arg.NextDueDate), Valid: true}
	}

	_, err := q.AdvanceRecurringTransaction(ctx, sqlc.AdvanceRecurringTransactionParams{
		LastGeneratedDate: pgtype.Date{Time: dateOnlyUTC(arg.OccurrenceDate), Valid: true},
		NextDueDate:       nextDue,
		IsActive:          arg.StillActive,
		ID:                arg.Template.ID,
	})
	if err != nil {
		return fmt.Errorf("advance recurring template: %w", err)
	}
	return nil
}
