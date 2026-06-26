package services

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// AccountService manages a user's financial accounts. Balances are not edited
// here — they change only through transactions.
type AccountService struct {
	store database.Store
}

func NewAccountService(store database.Store) *AccountService {
	return &AccountService{store: store}
}

type CreateAccountInput struct {
	Name           string
	AccountType    string
	Currency       string
	InitialBalance decimal.Decimal
	Icon           *string
	Color          *string
	DisplayOrder   int32
}

type UpdateAccountInput struct {
	Name         string
	AccountType  string
	Icon         *string
	Color        *string
	DisplayOrder int32
	IsArchived   bool
}

func (s *AccountService) Create(ctx context.Context, userID uuid.UUID, in CreateAccountInput) (sqlc.Account, error) {
	currency := in.Currency
	if currency == "" {
		currency = defaultCurrency
	}
	return s.store.CreateAccount(ctx, sqlc.CreateAccountParams{
		UserID:         userID,
		Name:           in.Name,
		AccountType:    in.AccountType,
		Currency:       currency,
		InitialBalance: in.InitialBalance,
		Icon:           in.Icon,
		Color:          in.Color,
		DisplayOrder:   in.DisplayOrder,
	})
}

func (s *AccountService) List(ctx context.Context, userID uuid.UUID) ([]sqlc.Account, error) {
	return s.store.ListAccounts(ctx, userID)
}

func (s *AccountService) Get(ctx context.Context, userID, id uuid.UUID) (sqlc.Account, error) {
	acct, err := s.store.GetAccount(ctx, sqlc.GetAccountParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Account{}, domain.ErrNotFound
	}
	return acct, err
}

func (s *AccountService) Update(ctx context.Context, userID, id uuid.UUID, in UpdateAccountInput) (sqlc.Account, error) {
	acct, err := s.store.UpdateAccount(ctx, sqlc.UpdateAccountParams{
		Name:         in.Name,
		AccountType:  in.AccountType,
		Icon:         in.Icon,
		Color:        in.Color,
		DisplayOrder: in.DisplayOrder,
		IsArchived:   in.IsArchived,
		ID:           id,
		UserID:       userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Account{}, domain.ErrNotFound
	}
	return acct, err
}

func (s *AccountService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.store.SoftDeleteAccount(ctx, sqlc.SoftDeleteAccountParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}
