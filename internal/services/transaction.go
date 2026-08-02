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

// CreateTransactionInput is the validated business input for a new transaction.
// Date and Currency are optional (defaulted from "today" and the account).
type CreateTransactionInput struct {
	AccountID         uuid.UUID
	TransactionType   string
	Amount            decimal.Decimal
	Currency          string
	CategoryID        *uuid.UUID
	Description       *string
	Notes             *string
	TransactionDate   *time.Time
	TransferAccountID *uuid.UUID
	ClientID          *string

	// AI categorization metadata (optional). Set when the category came from an
	// AI suggestion the user accepted; used to track model accuracy.
	AICategorized         bool
	AIConfidence          *decimal.Decimal
	AISuggestedCategoryID *uuid.UUID
}

// TransactionService implements the transaction business rules, all anchored to
// the user's active tracking period.
type TransactionService struct {
	store database.Store
	now   func() time.Time
}

// NewTransactionService builds the service. "Today" is computed in
// America/Bogota so dates match the Colombian user regardless of server tz.
func NewTransactionService(store database.Store) *TransactionService {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		loc = time.UTC
	}
	return &TransactionService{
		store: store,
		now:   func() time.Time { return time.Now().In(loc) },
	}
}

// Create validates inputs against the active tracking period, then inserts the
// transaction and adjusts account balance(s) atomically.
func (s *TransactionService) Create(ctx context.Context, userID uuid.UUID, in CreateTransactionInput) (sqlc.Transaction, error) {
	if !in.Amount.IsPositive() {
		return sqlc.Transaction{}, domain.ErrInvalidAmount
	}

	period, err := s.activePeriod(ctx, userID)
	if err != nil {
		return sqlc.Transaction{}, err
	}

	// Source account must exist and belong to the user.
	acct, err := s.store.GetAccount(ctx, sqlc.GetAccountParams{ID: in.AccountID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Transaction{}, domain.ErrAccountNotFound
	} else if err != nil {
		return sqlc.Transaction{}, err
	}

	transferAccount, err := s.resolveTransferAccount(ctx, userID, in)
	if err != nil {
		return sqlc.Transaction{}, err
	}

	category, err := s.resolveCategory(ctx, userID, in.CategoryID)
	if err != nil {
		return sqlc.Transaction{}, err
	}

	// Resolve and validate the date against the active period (the DB trigger
	// is the hard guarantee; this gives a friendlier error).
	txnDay := dateOnly(s.now())
	if in.TransactionDate != nil {
		txnDay = dateOnly(*in.TransactionDate)
	}
	if txnDay.Before(dateOnly(period.StartDate.Time)) || txnDay.After(dateOnly(period.EndDate.Time)) {
		return sqlc.Transaction{}, domain.ErrDateOutsidePeriod
	}

	currency := in.Currency
	if currency == "" {
		currency = acct.Currency
	}

	return s.store.CreateTransactionTx(ctx, sqlc.CreateTransactionParams{
		UserID:                userID,
		TrackingPeriodID:      period.ID,
		AccountID:             in.AccountID,
		CategoryID:            category,
		TransactionType:       in.TransactionType,
		Amount:                in.Amount,
		Currency:              currency,
		Description:           in.Description,
		Notes:                 in.Notes,
		TransactionDate:       pgtype.Date{Time: txnDay, Valid: true},
		TransferAccountID:     transferAccount,
		ClientID:              in.ClientID,
		AiCategorized:         in.AICategorized,
		AiConfidence:          decimalPtrToNull(in.AIConfidence),
		AiSuggestedCategoryID: ptrToNullUUID(in.AISuggestedCategoryID),
	})
}

// decimalPtrToNull converts an optional decimal into sqlc's NullDecimal.
func decimalPtrToNull(d *decimal.Decimal) decimal.NullDecimal {
	if d == nil {
		return decimal.NullDecimal{}
	}
	return decimal.NullDecimal{Decimal: *d, Valid: true}
}

// Get returns a single non-deleted transaction owned by the user.
func (s *TransactionService) Get(ctx context.Context, userID, id uuid.UUID) (sqlc.Transaction, error) {
	txn, err := s.store.GetTransaction(ctx, sqlc.GetTransactionParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Transaction{}, domain.ErrNotFound
	}
	return txn, err
}

// List returns the user's transactions for a tracking period. If periodID is
// nil, the active period is used.
func (s *TransactionService) List(ctx context.Context, userID uuid.UUID, periodID *uuid.UUID) ([]sqlc.Transaction, error) {
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

	return s.store.ListTransactionsByPeriod(ctx, sqlc.ListTransactionsByPeriodParams{
		UserID:           userID,
		TrackingPeriodID: pid,
	})
}

// Update re-specifies a transaction's fields, atomically reversing the old
// balance effect and applying the new one. The transaction stays in its period.
func (s *TransactionService) Update(ctx context.Context, userID, id uuid.UUID, in CreateTransactionInput) (sqlc.Transaction, error) {
	if !in.Amount.IsPositive() {
		return sqlc.Transaction{}, domain.ErrInvalidAmount
	}

	// Validate the (possibly changed) source account and counter-account.
	if _, err := s.store.GetAccount(ctx, sqlc.GetAccountParams{ID: in.AccountID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Transaction{}, domain.ErrAccountNotFound
		}
		return sqlc.Transaction{}, err
	}
	transferAccount, err := s.resolveTransferAccount(ctx, userID, in)
	if err != nil {
		return sqlc.Transaction{}, err
	}
	category, err := s.resolveCategory(ctx, userID, in.CategoryID)
	if err != nil {
		return sqlc.Transaction{}, err
	}

	current, err := s.store.GetTransaction(ctx, sqlc.GetTransactionParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Transaction{}, domain.ErrNotFound
		}
		return sqlc.Transaction{}, err
	}

	// Date must remain within the transaction's own period.
	period, err := s.store.GetTrackingPeriodByID(ctx, current.TrackingPeriodID)
	if err != nil {
		return sqlc.Transaction{}, err
	}
	txnDay := current.TransactionDate.Time
	if in.TransactionDate != nil {
		txnDay = dateOnly(*in.TransactionDate)
	}
	if txnDay.Before(dateOnly(period.StartDate.Time)) || txnDay.After(dateOnly(period.EndDate.Time)) {
		return sqlc.Transaction{}, domain.ErrDateOutsidePeriod
	}

	currency := in.Currency
	if currency == "" {
		currency = current.Currency
	}

	return s.store.UpdateTransactionTx(ctx, sqlc.UpdateTransactionParams{
		AccountID:         in.AccountID,
		CategoryID:        category,
		TransactionType:   in.TransactionType,
		Amount:            in.Amount,
		Currency:          currency,
		Description:       in.Description,
		Notes:             in.Notes,
		TransactionDate:   pgtype.Date{Time: txnDay, Valid: true},
		TransferAccountID: transferAccount,
		ID:                id,
		UserID:            userID,
	})
}

// Delete soft-deletes a transaction and reverses its balance effect.
func (s *TransactionService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.store.SoftDeleteTransactionTx(ctx, id, userID)
	return err
}

// activePeriod returns the user's active period, lazily closing it (and rolling
// over to the next one) if it has already ended — a fallback for when the
// nightly scheduler hasn't run. Loops in case several periods are overdue.
func (s *TransactionService) activePeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
	const maxRollovers = 120 // safety bound (~10 years of monthly periods)
	today := dateOnly(s.now())

	for i := 0; i < maxRollovers; i++ {
		period, err := s.store.GetActiveTrackingPeriod(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.TrackingPeriod{}, domain.ErrNoActivePeriod
		} else if err != nil {
			return sqlc.TrackingPeriod{}, err
		}

		if !dateOnly(period.EndDate.Time).Before(today) {
			return period, nil // still current
		}

		if _, err := s.store.ClosePeriodTx(ctx, period.ID, userID); err != nil {
			return sqlc.TrackingPeriod{}, err
		}
	}

	return sqlc.TrackingPeriod{}, domain.ErrNoActivePeriod
}

func (s *TransactionService) resolveTransferAccount(ctx context.Context, userID uuid.UUID, in CreateTransactionInput) (uuid.NullUUID, error) {
	if in.TransactionType != "transfer" {
		if in.TransferAccountID != nil {
			return uuid.NullUUID{}, domain.ErrInvalidTransfer // only transfers may set it
		}
		return uuid.NullUUID{}, nil
	}

	if in.TransferAccountID == nil || *in.TransferAccountID == in.AccountID {
		return uuid.NullUUID{}, domain.ErrInvalidTransfer
	}
	if _, err := s.store.GetAccount(ctx, sqlc.GetAccountParams{ID: *in.TransferAccountID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.NullUUID{}, domain.ErrAccountNotFound
		}
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: *in.TransferAccountID, Valid: true}, nil
}

func (s *TransactionService) resolveCategory(ctx context.Context, userID uuid.UUID, categoryID *uuid.UUID) (uuid.NullUUID, error) {
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

// dateOnly strips the time-of-day, normalizing to UTC midnight so DATE
// comparisons are stable regardless of the source timezone.
func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
