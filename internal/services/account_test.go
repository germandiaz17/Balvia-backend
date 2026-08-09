package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

type mockAccountStore struct {
	database.Store
	txnCount      int64
	countCalled   bool
	updateAccount func(ctx context.Context, arg sqlc.UpdateAccountParams) (sqlc.Account, error)
}

func (m *mockAccountStore) CountTransactionsByAccount(_ context.Context, _ sqlc.CountTransactionsByAccountParams) (int64, error) {
	m.countCalled = true
	return m.txnCount, nil
}

func (m *mockAccountStore) UpdateAccount(ctx context.Context, arg sqlc.UpdateAccountParams) (sqlc.Account, error) {
	return m.updateAccount(ctx, arg)
}

func accountStore(txnCount int64) (*mockAccountStore, *sqlc.UpdateAccountParams) {
	var captured sqlc.UpdateAccountParams
	store := &mockAccountStore{txnCount: txnCount}
	store.updateAccount = func(_ context.Context, arg sqlc.UpdateAccountParams) (sqlc.Account, error) {
		captured = arg
		return sqlc.Account{ID: arg.ID, Name: arg.Name}, nil
	}
	return store, &captured
}

// Onboarding leans on this: the default "Efectivo" account is created with a
// zero opening balance and the wizard restates it.
func TestUpdateAccount_SetsOpeningBalanceWhenUntouched(t *testing.T) {
	store, captured := accountStore(0)
	svc := NewAccountService(store)

	opening := decimal.RequireFromString("250000")
	_, err := svc.Update(context.Background(), uuid.New(), uuid.New(), UpdateAccountInput{
		Name:           "Efectivo",
		AccountType:    "cash",
		InitialBalance: &opening,
	})

	require.NoError(t, err)
	assert.True(t, store.countCalled, "must check for movements before restating the balance")
	require.True(t, captured.InitialBalance.Valid)
	assert.True(t, opening.Equal(captured.InitialBalance.Decimal))
}

func TestUpdateAccount_RejectsOpeningBalanceWithTransactions(t *testing.T) {
	store, _ := accountStore(3)
	svc := NewAccountService(store)

	opening := decimal.RequireFromString("250000")
	_, err := svc.Update(context.Background(), uuid.New(), uuid.New(), UpdateAccountInput{
		Name:           "Efectivo",
		AccountType:    "cash",
		InitialBalance: &opening,
	})

	require.ErrorIs(t, err, domain.ErrAccountHasTransactions)
}

// Renaming or archiving an account must never touch its balance, whatever its
// movement history.
func TestUpdateAccount_LeavesBalanceAloneWhenOmitted(t *testing.T) {
	store, captured := accountStore(42)
	svc := NewAccountService(store)

	_, err := svc.Update(context.Background(), uuid.New(), uuid.New(), UpdateAccountInput{
		Name:        "Bancolombia",
		AccountType: "checking",
		IsArchived:  true,
	})

	require.NoError(t, err)
	assert.False(t, store.countCalled, "no need to count movements when the balance is not restated")
	assert.False(t, captured.InitialBalance.Valid, "NULL keeps the opening balance untouched")
}
