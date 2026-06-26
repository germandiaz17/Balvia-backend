package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

type mockTxnStore struct {
	database.Store
	getActivePeriod func(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error)
	getAccount      func(ctx context.Context, arg sqlc.GetAccountParams) (sqlc.Account, error)
	getCategory     func(ctx context.Context, arg sqlc.GetCategoryForUserParams) (sqlc.Category, error)
	createTxn       func(ctx context.Context, arg sqlc.CreateTransactionParams) (sqlc.Transaction, error)
	createCalled    bool
}

func (m *mockTxnStore) GetActiveTrackingPeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
	return m.getActivePeriod(ctx, userID)
}
func (m *mockTxnStore) GetAccount(ctx context.Context, arg sqlc.GetAccountParams) (sqlc.Account, error) {
	return m.getAccount(ctx, arg)
}
func (m *mockTxnStore) GetCategoryForUser(ctx context.Context, arg sqlc.GetCategoryForUserParams) (sqlc.Category, error) {
	return m.getCategory(ctx, arg)
}
func (m *mockTxnStore) CreateTransactionTx(ctx context.Context, arg sqlc.CreateTransactionParams) (sqlc.Transaction, error) {
	m.createCalled = true
	return m.createTxn(ctx, arg)
}

func pgDateOf(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: true} }

// activePeriodStore returns a mock whose active period spans the 30 days from
// 2026-06-25, with an account in COP. Override fields per test as needed.
func activePeriodStore() *mockTxnStore {
	periodID := uuid.New()
	return &mockTxnStore{
		getActivePeriod: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return sqlc.TrackingPeriod{
				ID:        periodID,
				StartDate: pgDateOf(time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)),
				EndDate:   pgDateOf(time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)),
				Status:    "active",
			}, nil
		},
		getAccount: func(_ context.Context, _ sqlc.GetAccountParams) (sqlc.Account, error) {
			return sqlc.Account{Currency: "COP"}, nil
		},
		createTxn: func(_ context.Context, arg sqlc.CreateTransactionParams) (sqlc.Transaction, error) {
			return sqlc.Transaction{ID: uuid.New(), AccountID: arg.AccountID, TransactionType: arg.TransactionType}, nil
		},
	}
}

func newTxnSvc(store database.Store) *TransactionService {
	return &TransactionService{store: store, now: func() time.Time {
		return time.Date(2026, 6, 25, 14, 0, 0, 0, time.UTC)
	}}
}

func TestCreateTransaction_HappyExpense(t *testing.T) {
	store := activePeriodStore()
	var captured sqlc.CreateTransactionParams
	store.createTxn = func(_ context.Context, arg sqlc.CreateTransactionParams) (sqlc.Transaction, error) {
		captured = arg
		return sqlc.Transaction{ID: uuid.New()}, nil
	}

	svc := newTxnSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), CreateTransactionInput{
		AccountID:       uuid.New(),
		TransactionType: "expense",
		Amount:          decimal.RequireFromString("5000"),
	})

	require.NoError(t, err)
	assert.Equal(t, "expense", captured.TransactionType)
	assert.True(t, captured.Amount.Equal(decimal.RequireFromString("5000")))
	assert.Equal(t, "COP", captured.Currency, "currency defaults to the account's")
	assert.Equal(t, "2026-06-25", captured.TransactionDate.Time.Format("2006-01-02"))
	assert.False(t, captured.CategoryID.Valid)
}

func TestCreateTransaction_InvalidAmount(t *testing.T) {
	store := activePeriodStore()
	svc := newTxnSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), CreateTransactionInput{
		AccountID:       uuid.New(),
		TransactionType: "income",
		Amount:          decimal.RequireFromString("-5"),
	})
	require.ErrorIs(t, err, domain.ErrInvalidAmount)
	assert.False(t, store.createCalled)
}

func TestCreateTransaction_NoActivePeriod(t *testing.T) {
	store := activePeriodStore()
	store.getActivePeriod = func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
		return sqlc.TrackingPeriod{}, pgx.ErrNoRows
	}
	svc := newTxnSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), CreateTransactionInput{
		AccountID: uuid.New(), TransactionType: "income", Amount: decimal.RequireFromString("100"),
	})
	require.ErrorIs(t, err, domain.ErrNoActivePeriod)
	assert.False(t, store.createCalled)
}

func TestCreateTransaction_AccountNotFound(t *testing.T) {
	store := activePeriodStore()
	store.getAccount = func(_ context.Context, _ sqlc.GetAccountParams) (sqlc.Account, error) {
		return sqlc.Account{}, pgx.ErrNoRows
	}
	svc := newTxnSvc(store)
	_, err := svc.Create(context.Background(), uuid.New(), CreateTransactionInput{
		AccountID: uuid.New(), TransactionType: "income", Amount: decimal.RequireFromString("100"),
	})
	require.ErrorIs(t, err, domain.ErrAccountNotFound)
	assert.False(t, store.createCalled)
}

func TestCreateTransaction_TransferRequiresDistinctDestination(t *testing.T) {
	store := activePeriodStore()
	svc := newTxnSvc(store)

	// Missing transfer_account_id.
	_, err := svc.Create(context.Background(), uuid.New(), CreateTransactionInput{
		AccountID: uuid.New(), TransactionType: "transfer", Amount: decimal.RequireFromString("100"),
	})
	require.ErrorIs(t, err, domain.ErrInvalidTransfer)

	// Destination equal to source.
	same := uuid.New()
	_, err = svc.Create(context.Background(), uuid.New(), CreateTransactionInput{
		AccountID: same, TransactionType: "transfer", Amount: decimal.RequireFromString("100"), TransferAccountID: &same,
	})
	require.ErrorIs(t, err, domain.ErrInvalidTransfer)
	assert.False(t, store.createCalled)
}

func TestCreateTransaction_DateOutsidePeriod(t *testing.T) {
	store := activePeriodStore()
	svc := newTxnSvc(store)
	outside := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC) // day before period start
	_, err := svc.Create(context.Background(), uuid.New(), CreateTransactionInput{
		AccountID: uuid.New(), TransactionType: "income", Amount: decimal.RequireFromString("100"), TransactionDate: &outside,
	})
	require.ErrorIs(t, err, domain.ErrDateOutsidePeriod)
	assert.False(t, store.createCalled)
}
