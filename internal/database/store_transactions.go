package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// CreateTransactionTx implements Store.
func (s *SQLStore) CreateTransactionTx(ctx context.Context, arg sqlc.CreateTransactionParams) (sqlc.Transaction, error) {
	var txn sqlc.Transaction

	err := s.execTx(ctx, func(q *sqlc.Queries) error {
		// The validate_transaction_period trigger enforces date-in-range and
		// "period not closed" at INSERT time.
		t, err := q.CreateTransaction(ctx, arg)
		if err != nil {
			return fmt.Errorf("insert transaction: %w", err)
		}
		txn = t
		return applyBalanceEffect(ctx, q, t, decimal.NewFromInt(1))
	})
	if err != nil {
		return txn, err
	}

	// Refresh immediate during insights in a separate (non-blocking) transaction.
	// Errors here are non-fatal: the transaction was already committed successfully.
	_ = s.RefreshImmediateDuringInsights(ctx, txn.UserID, txn.TrackingPeriodID)

	return txn, nil
}

// SoftDeleteTransactionTx implements Store.
func (s *SQLStore) SoftDeleteTransactionTx(ctx context.Context, id, userID uuid.UUID) (sqlc.Transaction, error) {
	var txn sqlc.Transaction

	err := s.execTx(ctx, func(q *sqlc.Queries) error {
		t, err := q.SoftDeleteTransaction(ctx, sqlc.SoftDeleteTransactionParams{ID: id, UserID: userID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound
			}
			return fmt.Errorf("soft delete transaction: %w", err)
		}
		txn = t
		// Reverse the original balance effect (sign -1).
		return applyBalanceEffect(ctx, q, t, decimal.NewFromInt(-1))
	})
	if err != nil {
		return txn, err
	}

	// Refresh immediate during insights (non-fatal if it fails).
	_ = s.RefreshImmediateDuringInsights(ctx, txn.UserID, txn.TrackingPeriodID)

	return txn, nil
}

// UpdateTransactionTx implements Store. It reverses the previous balance
// effect, applies the update (re-validated by the period trigger), then applies
// the new balance effect — all atomically.
func (s *SQLStore) UpdateTransactionTx(ctx context.Context, arg sqlc.UpdateTransactionParams) (sqlc.Transaction, error) {
	var updated sqlc.Transaction

	err := s.execTx(ctx, func(q *sqlc.Queries) error {
		old, err := q.GetTransaction(ctx, sqlc.GetTransactionParams{ID: arg.ID, UserID: arg.UserID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound
			}
			return fmt.Errorf("load transaction: %w", err)
		}

		if err := applyBalanceEffect(ctx, q, old, decimal.NewFromInt(-1)); err != nil {
			return err
		}

		nw, err := q.UpdateTransaction(ctx, arg)
		if err != nil {
			return fmt.Errorf("update transaction: %w", err)
		}
		updated = nw

		return applyBalanceEffect(ctx, q, nw, decimal.NewFromInt(1))
	})
	if err != nil {
		return updated, err
	}

	// Refresh immediate during insights (non-fatal if it fails).
	_ = s.RefreshImmediateDuringInsights(ctx, updated.UserID, updated.TrackingPeriodID)

	return updated, nil
}

// applyBalanceEffect adjusts account balance(s) for a transaction, scaled by
// sign (+1 to apply on create, -1 to reverse on delete):
//
//   - income:   account_id += amount
//   - expense:  account_id -= amount
//   - transfer: account_id -= amount, transfer_account_id += amount
func applyBalanceEffect(ctx context.Context, q *sqlc.Queries, t sqlc.Transaction, sign decimal.Decimal) error {
	amount := t.Amount.Mul(sign)

	adjust := func(accountID uuid.UUID, delta decimal.Decimal) error {
		return q.AdjustAccountBalance(ctx, sqlc.AdjustAccountBalanceParams{
			Delta:  delta,
			ID:     accountID,
			UserID: t.UserID,
		})
	}

	switch t.TransactionType {
	case "income":
		return adjust(t.AccountID, amount)
	case "expense":
		return adjust(t.AccountID, amount.Neg())
	case "transfer":
		if err := adjust(t.AccountID, amount.Neg()); err != nil {
			return err
		}
		// transfer_account_id is guaranteed non-null for transfers (DB CHECK).
		return adjust(t.TransferAccountID.UUID, amount)
	default:
		return fmt.Errorf("unknown transaction_type %q", t.TransactionType)
	}
}
