package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// mockSyncStore is a minimal Store mock used by SyncService tests.
// It embeds database.Store so unimplemented methods panic rather than silently
// returning zero values.
type mockSyncStore struct {
	database.Store

	// Pull stubs (all optional; default to returning empty slices).
	pullTransactions func(ctx context.Context, arg sqlc.SyncPullTransactionsParams) ([]sqlc.Transaction, error)
	pullAccounts     func(ctx context.Context, arg sqlc.SyncPullAccountsParams) ([]sqlc.Account, error)
	pullCategories   func(ctx context.Context, arg sqlc.SyncPullCategoriesParams) ([]sqlc.Category, error)
	pullBudgets      func(ctx context.Context, arg sqlc.SyncPullBudgetsParams) ([]sqlc.Budget, error)
	pullGoals        func(ctx context.Context, arg sqlc.SyncPullSavingsGoalsParams) ([]sqlc.SavingsGoal, error)
	pullContribs     func(ctx context.Context, arg sqlc.SyncPullGoalContributionsParams) ([]sqlc.SavingsGoalContribution, error)
	pullRecurrings   func(ctx context.Context, arg sqlc.SyncPullRecurringTransactionsParams) ([]sqlc.RecurringTransaction, error)
	pullPeriods      func(ctx context.Context, arg sqlc.SyncPullTrackingPeriodsParams) ([]sqlc.TrackingPeriod, error)

	// Push stubs
	getByClientID   func(ctx context.Context, arg sqlc.GetTransactionByClientIDParams) (sqlc.Transaction, error)
	getActivePeriod func(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error)
	getPeriodByID   func(ctx context.Context, id uuid.UUID) (sqlc.TrackingPeriod, error)
	getAccount      func(ctx context.Context, arg sqlc.GetAccountParams) (sqlc.Account, error)
	getCategory     func(ctx context.Context, arg sqlc.GetCategoryForUserParams) (sqlc.Category, error)
	createTxnTx     func(ctx context.Context, arg sqlc.CreateTransactionParams) (sqlc.Transaction, error)
	getTransaction  func(ctx context.Context, arg sqlc.GetTransactionParams) (sqlc.Transaction, error)
	updateTxnTx     func(ctx context.Context, arg sqlc.UpdateTransactionParams) (sqlc.Transaction, error)
	softDeleteTxnTx func(ctx context.Context, id, userID uuid.UUID) (sqlc.Transaction, error)
	closePeriodTx   func(ctx context.Context, periodID, userID uuid.UUID) (database.ClosePeriodResult, error)
}

// Pull stubs
func (m *mockSyncStore) SyncPullTransactions(ctx context.Context, arg sqlc.SyncPullTransactionsParams) ([]sqlc.Transaction, error) {
	if m.pullTransactions != nil {
		return m.pullTransactions(ctx, arg)
	}
	return nil, nil
}
func (m *mockSyncStore) SyncPullAccounts(ctx context.Context, arg sqlc.SyncPullAccountsParams) ([]sqlc.Account, error) {
	if m.pullAccounts != nil {
		return m.pullAccounts(ctx, arg)
	}
	return nil, nil
}
func (m *mockSyncStore) SyncPullCategories(ctx context.Context, arg sqlc.SyncPullCategoriesParams) ([]sqlc.Category, error) {
	if m.pullCategories != nil {
		return m.pullCategories(ctx, arg)
	}
	return nil, nil
}
func (m *mockSyncStore) SyncPullBudgets(ctx context.Context, arg sqlc.SyncPullBudgetsParams) ([]sqlc.Budget, error) {
	if m.pullBudgets != nil {
		return m.pullBudgets(ctx, arg)
	}
	return nil, nil
}
func (m *mockSyncStore) SyncPullSavingsGoals(ctx context.Context, arg sqlc.SyncPullSavingsGoalsParams) ([]sqlc.SavingsGoal, error) {
	if m.pullGoals != nil {
		return m.pullGoals(ctx, arg)
	}
	return nil, nil
}
func (m *mockSyncStore) SyncPullGoalContributions(ctx context.Context, arg sqlc.SyncPullGoalContributionsParams) ([]sqlc.SavingsGoalContribution, error) {
	if m.pullContribs != nil {
		return m.pullContribs(ctx, arg)
	}
	return nil, nil
}
func (m *mockSyncStore) SyncPullRecurringTransactions(ctx context.Context, arg sqlc.SyncPullRecurringTransactionsParams) ([]sqlc.RecurringTransaction, error) {
	if m.pullRecurrings != nil {
		return m.pullRecurrings(ctx, arg)
	}
	return nil, nil
}
func (m *mockSyncStore) SyncPullTrackingPeriods(ctx context.Context, arg sqlc.SyncPullTrackingPeriodsParams) ([]sqlc.TrackingPeriod, error) {
	if m.pullPeriods != nil {
		return m.pullPeriods(ctx, arg)
	}
	return nil, nil
}

// Push stubs — forwarded to the TransactionService sub-mock.
func (m *mockSyncStore) GetTransactionByClientID(ctx context.Context, arg sqlc.GetTransactionByClientIDParams) (sqlc.Transaction, error) {
	if m.getByClientID != nil {
		return m.getByClientID(ctx, arg)
	}
	return sqlc.Transaction{}, pgx.ErrNoRows
}
func (m *mockSyncStore) GetActiveTrackingPeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
	if m.getActivePeriod != nil {
		return m.getActivePeriod(ctx, userID)
	}
	return sqlc.TrackingPeriod{}, pgx.ErrNoRows
}
func (m *mockSyncStore) GetTrackingPeriodByID(ctx context.Context, id uuid.UUID) (sqlc.TrackingPeriod, error) {
	if m.getPeriodByID != nil {
		return m.getPeriodByID(ctx, id)
	}
	return sqlc.TrackingPeriod{}, pgx.ErrNoRows
}
func (m *mockSyncStore) GetAccount(ctx context.Context, arg sqlc.GetAccountParams) (sqlc.Account, error) {
	if m.getAccount != nil {
		return m.getAccount(ctx, arg)
	}
	return sqlc.Account{}, pgx.ErrNoRows
}
func (m *mockSyncStore) GetCategoryForUser(ctx context.Context, arg sqlc.GetCategoryForUserParams) (sqlc.Category, error) {
	if m.getCategory != nil {
		return m.getCategory(ctx, arg)
	}
	return sqlc.Category{}, pgx.ErrNoRows
}
func (m *mockSyncStore) CreateTransactionTx(ctx context.Context, arg sqlc.CreateTransactionParams) (sqlc.Transaction, error) {
	if m.createTxnTx != nil {
		return m.createTxnTx(ctx, arg)
	}
	return sqlc.Transaction{}, nil
}
func (m *mockSyncStore) GetTransaction(ctx context.Context, arg sqlc.GetTransactionParams) (sqlc.Transaction, error) {
	if m.getTransaction != nil {
		return m.getTransaction(ctx, arg)
	}
	return sqlc.Transaction{}, pgx.ErrNoRows
}
func (m *mockSyncStore) UpdateTransactionTx(ctx context.Context, arg sqlc.UpdateTransactionParams) (sqlc.Transaction, error) {
	if m.updateTxnTx != nil {
		return m.updateTxnTx(ctx, arg)
	}
	return sqlc.Transaction{}, nil
}
func (m *mockSyncStore) SoftDeleteTransactionTx(ctx context.Context, id, userID uuid.UUID) (sqlc.Transaction, error) {
	if m.softDeleteTxnTx != nil {
		return m.softDeleteTxnTx(ctx, id, userID)
	}
	return sqlc.Transaction{}, nil
}
func (m *mockSyncStore) ClosePeriodTx(ctx context.Context, periodID, userID uuid.UUID) (database.ClosePeriodResult, error) {
	if m.closePeriodTx != nil {
		return m.closePeriodTx(ctx, periodID, userID)
	}
	return database.ClosePeriodResult{}, nil
}

// RefreshImmediateDuringInsights is a no-op in sync tests.
func (m *mockSyncStore) RefreshImmediateDuringInsights(ctx context.Context, userID, periodID uuid.UUID) error {
	return nil
}

// --- helpers ----------------------------------------------------------------

func pgTs(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func newSyncSvc(store *mockSyncStore) *SyncService {
	txnSvc := &TransactionService{
		store: store,
		now:   func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) },
	}
	return NewSyncService(store, zerolog.Nop(), SyncServices{
		Transaction: txnSvc,
		Account:     NewAccountService(store),
		Category:    NewCategoryService(store),
		Budget:      NewBudgetService(store),
		SavingsGoal: NewSavingsGoalService(store),
		Recurring:   NewRecurringTransactionService(store),
	})
}

func activeSyncPeriod() sqlc.TrackingPeriod {
	return sqlc.TrackingPeriod{
		ID:        uuid.New(),
		StartDate: pgDateOf(time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)),
		EndDate:   pgDateOf(time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)),
		Status:    "active",
		UpdatedAt: pgTs(time.Now()),
	}
}

// --- Pull tests -------------------------------------------------------------

func TestPull_EmptyDatabase(t *testing.T) {
	store := &mockSyncStore{}
	svc := newSyncSvc(store)

	result, err := svc.Pull(context.Background(), uuid.New(), time.Time{}, 0)
	require.NoError(t, err)
	assert.False(t, result.HasMore)
	assert.Empty(t, result.Transactions)
	assert.Empty(t, result.Accounts)
	assert.Empty(t, result.Categories)
	assert.Empty(t, result.Budgets)
	assert.Empty(t, result.SavingsGoals)
	assert.Empty(t, result.GoalContributions)
	assert.Empty(t, result.RecurringTransactions)
	assert.Empty(t, result.TrackingPeriods)
	assert.True(t, result.ServerTime.After(time.Now().Add(-5*time.Second)),
		"server_time should be very recent")
}

func TestPull_ReturnsTransactionsSinceTimestamp(t *testing.T) {
	since := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	txnID := uuid.New()
	userID := uuid.New()

	store := &mockSyncStore{
		pullTransactions: func(_ context.Context, arg sqlc.SyncPullTransactionsParams) ([]sqlc.Transaction, error) {
			assert.Equal(t, userID, arg.UserID)
			assert.Equal(t, since, arg.Since.Time)
			return []sqlc.Transaction{
				{
					ID:              txnID,
					UserID:          userID,
					TransactionType: "expense",
					Amount:          decimal.RequireFromString("5000"),
					Currency:        "COP",
					UpdatedAt:       pgTs(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)),
				},
			}, nil
		},
	}
	svc := newSyncSvc(store)

	result, err := svc.Pull(context.Background(), userID, since, 0)
	require.NoError(t, err)
	assert.Len(t, result.Transactions, 1)
	assert.Equal(t, txnID, result.Transactions[0].ID)
}

func TestPull_HasMoreWhenResultsExceedPageSize(t *testing.T) {
	// Return pageSize+1 transactions to trigger HasMore.
	pageSize := 5
	txns := make([]sqlc.Transaction, pageSize+1)
	for i := range txns {
		txns[i] = sqlc.Transaction{ID: uuid.New(), Amount: decimal.NewFromInt(100)}
	}

	store := &mockSyncStore{
		pullTransactions: func(_ context.Context, _ sqlc.SyncPullTransactionsParams) ([]sqlc.Transaction, error) {
			return txns, nil
		},
	}
	svc := newSyncSvc(store)

	result, err := svc.Pull(context.Background(), uuid.New(), time.Time{}, pageSize)
	require.NoError(t, err)
	assert.True(t, result.HasMore)
	assert.Len(t, result.Transactions, pageSize, "should be trimmed to pageSize")
}

func TestPull_PageSizeCappedAtMax(t *testing.T) {
	store := &mockSyncStore{
		pullTransactions: func(_ context.Context, arg sqlc.SyncPullTransactionsParams) ([]sqlc.Transaction, error) {
			// page_size passed to query should be maxPageSize+1 (limit = ps+1).
			assert.LessOrEqual(t, int(arg.PageSize), maxPageSize+1)
			return nil, nil
		},
	}
	svc := newSyncSvc(store)

	// Request far above cap.
	_, err := svc.Pull(context.Background(), uuid.New(), time.Time{}, 99999)
	require.NoError(t, err)
}

func TestPull_SoftDeletedTransactionIncluded(t *testing.T) {
	deletedAt := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	txnID := uuid.New()

	store := &mockSyncStore{
		pullTransactions: func(_ context.Context, _ sqlc.SyncPullTransactionsParams) ([]sqlc.Transaction, error) {
			return []sqlc.Transaction{
				{
					ID:        txnID,
					Amount:    decimal.NewFromInt(100),
					DeletedAt: pgTs(deletedAt), // soft-deleted
					UpdatedAt: pgTs(deletedAt),
				},
			}, nil
		},
	}
	svc := newSyncSvc(store)

	result, err := svc.Pull(context.Background(), uuid.New(), time.Time{}, 0)
	require.NoError(t, err)
	// Soft-deleted transaction MUST be present so the client can remove it.
	require.Len(t, result.Transactions, 1)
	assert.Equal(t, txnID, result.Transactions[0].ID)
	assert.True(t, result.Transactions[0].DeletedAt.Valid,
		"deleted_at should be set on the returned transaction")
}

// --- Push tests -------------------------------------------------------------

func TestPush_CreateTransaction_HappyPath(t *testing.T) {
	userID := uuid.New()
	period := activeSyncPeriod()
	accountID := uuid.New()
	newTxnID := uuid.New()

	store := &mockSyncStore{
		getActivePeriod: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return period, nil
		},
		getAccount: func(_ context.Context, _ sqlc.GetAccountParams) (sqlc.Account, error) {
			return sqlc.Account{ID: accountID, Currency: "COP"}, nil
		},
		createTxnTx: func(_ context.Context, arg sqlc.CreateTransactionParams) (sqlc.Transaction, error) {
			return sqlc.Transaction{ID: newTxnID, AccountID: arg.AccountID}, nil
		},
	}
	svc := newSyncSvc(store)

	clientRef := "local-001"
	clientID := "mobile-uuid-abc123"
	amount := "15000.00"
	items := []PushItem{
		{
			ClientRef:  clientRef,
			EntityType: EntityTransaction,
			Operation:  OpCreate,
			TxnPayload: &PushTransactionPayload{
				AccountID:       accountID,
				TransactionType: "expense",
				Amount:          amount,
				ClientID:        &clientID,
			},
		},
	}

	result, err := svc.Push(context.Background(), userID, items)
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	r := result.Results[0]
	assert.Equal(t, clientRef, r.ClientRef)
	assert.Equal(t, StatusApplied, r.Status)
	assert.Nil(t, r.Error)
	assert.NotNil(t, r.ServerEntity)
}

func TestPush_CreateTransaction_Idempotent(t *testing.T) {
	// If a client_id already exists on the server, the item must be "skipped"
	// with the existing server row returned (not an error).
	userID := uuid.New()
	existingID := uuid.New()
	clientID := "duplicate-client-id"

	store := &mockSyncStore{
		getByClientID: func(_ context.Context, arg sqlc.GetTransactionByClientIDParams) (sqlc.Transaction, error) {
			return sqlc.Transaction{ID: existingID, Amount: decimal.NewFromInt(1000)}, nil
		},
	}
	svc := newSyncSvc(store)

	items := []PushItem{
		{
			ClientRef:  "ref-dup",
			EntityType: EntityTransaction,
			Operation:  OpCreate,
			TxnPayload: &PushTransactionPayload{
				AccountID:       uuid.New(),
				TransactionType: "expense",
				Amount:          "1000.00",
				ClientID:        &clientID,
			},
		},
	}

	result, err := svc.Push(context.Background(), userID, items)
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	r := result.Results[0]
	assert.Equal(t, StatusSkipped, r.Status)
	// Server entity should be the already-existing transaction.
	assert.NotNil(t, r.ServerEntity)
}

func TestPush_CreateTransaction_InvalidAmount(t *testing.T) {
	store := &mockSyncStore{}
	svc := newSyncSvc(store)

	items := []PushItem{
		{
			ClientRef:  "ref-bad",
			EntityType: EntityTransaction,
			Operation:  OpCreate,
			TxnPayload: &PushTransactionPayload{
				AccountID:       uuid.New(),
				TransactionType: "expense",
				Amount:          "-500.00", // negative
			},
		},
	}

	result, err := svc.Push(context.Background(), uuid.New(), items)
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	r := result.Results[0]
	assert.Equal(t, StatusRejected, r.Status)
	require.NotNil(t, r.Error)
	assert.Contains(t, *r.Error, domain.ErrInvalidAmount.Error())
}

func TestPush_UpdateTransaction_ConflictDetection(t *testing.T) {
	// Server has a newer updated_at than what the client sent → conflict.
	userID := uuid.New()
	txnID := uuid.New()
	periodID := uuid.New()

	serverUpdatedAt := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) // server is newer
	clientUpdatedAt := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC) // client is older

	existingTxn := sqlc.Transaction{
		ID:               txnID,
		TrackingPeriodID: periodID,
		Amount:           decimal.NewFromInt(2000),
		UpdatedAt:        pgTs(serverUpdatedAt),
	}

	store := &mockSyncStore{
		getTransaction: func(_ context.Context, arg sqlc.GetTransactionParams) (sqlc.Transaction, error) {
			return existingTxn, nil
		},
		getPeriodByID: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return sqlc.TrackingPeriod{ID: periodID, Status: "active"}, nil
		},
	}
	svc := newSyncSvc(store)

	items := []PushItem{
		{
			ClientRef:       "ref-conflict",
			EntityType:      EntityTransaction,
			Operation:       OpUpdate,
			EntityID:        &txnID,
			ClientUpdatedAt: &clientUpdatedAt,
			TxnPayload: &PushTransactionPayload{
				AccountID:       uuid.New(),
				TransactionType: "expense",
				Amount:          "999.00",
			},
		},
	}

	result, err := svc.Push(context.Background(), userID, items)
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	r := result.Results[0]
	assert.Equal(t, StatusConflict, r.Status)
	// Server entity returned so client can reconcile.
	assert.NotNil(t, r.ServerEntity)
}

func TestPush_UpdateTransaction_RejectedIfPeriodClosed(t *testing.T) {
	userID := uuid.New()
	txnID := uuid.New()
	periodID := uuid.New()

	existingTxn := sqlc.Transaction{
		ID:               txnID,
		TrackingPeriodID: periodID,
		Amount:           decimal.NewFromInt(500),
		UpdatedAt:        pgTs(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
	}

	store := &mockSyncStore{
		getTransaction: func(_ context.Context, _ sqlc.GetTransactionParams) (sqlc.Transaction, error) {
			return existingTxn, nil
		},
		getPeriodByID: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return sqlc.TrackingPeriod{ID: periodID, Status: "closed"}, nil
		},
	}
	svc := newSyncSvc(store)

	// Client claims its version is fresh.
	freshClient := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	items := []PushItem{
		{
			ClientRef:       "ref-closed",
			EntityType:      EntityTransaction,
			Operation:       OpUpdate,
			EntityID:        &txnID,
			ClientUpdatedAt: &freshClient,
			TxnPayload: &PushTransactionPayload{
				AccountID:       uuid.New(),
				TransactionType: "expense",
				Amount:          "500.00",
			},
		},
	}

	result, err := svc.Push(context.Background(), userID, items)
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	r := result.Results[0]
	assert.Equal(t, StatusRejected, r.Status)
	require.NotNil(t, r.Error)
	assert.Contains(t, *r.Error, domain.ErrPeriodClosed.Error())
}

func TestPush_DeleteTransaction_IdempotentIfAlreadyDeleted(t *testing.T) {
	userID := uuid.New()
	txnID := uuid.New()

	store := &mockSyncStore{
		getTransaction: func(_ context.Context, _ sqlc.GetTransactionParams) (sqlc.Transaction, error) {
			return sqlc.Transaction{}, pgx.ErrNoRows // already deleted
		},
	}
	svc := newSyncSvc(store)

	items := []PushItem{
		{
			ClientRef:  "ref-del-dup",
			EntityType: EntityTransaction,
			Operation:  OpDelete,
			EntityID:   &txnID,
		},
	}

	result, err := svc.Push(context.Background(), userID, items)
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	assert.Equal(t, StatusSkipped, result.Results[0].Status)
}

func TestPush_MixedBatch_IndependentOutcomes(t *testing.T) {
	// A batch with one valid create and one item with an unknown entity_type
	// should process the valid item and reject the unknown one independently.
	userID := uuid.New()
	period := activeSyncPeriod()
	accountID := uuid.New()

	store := &mockSyncStore{
		getActivePeriod: func(_ context.Context, _ uuid.UUID) (sqlc.TrackingPeriod, error) {
			return period, nil
		},
		getAccount: func(_ context.Context, _ sqlc.GetAccountParams) (sqlc.Account, error) {
			return sqlc.Account{ID: accountID, Currency: "COP"}, nil
		},
		createTxnTx: func(_ context.Context, _ sqlc.CreateTransactionParams) (sqlc.Transaction, error) {
			return sqlc.Transaction{ID: uuid.New()}, nil
		},
	}
	svc := newSyncSvc(store)

	amount := "3000.00"
	items := []PushItem{
		{
			ClientRef:  "valid-create",
			EntityType: EntityTransaction,
			Operation:  OpCreate,
			TxnPayload: &PushTransactionPayload{
				AccountID:       accountID,
				TransactionType: "expense",
				Amount:          amount,
			},
		},
		{
			ClientRef:  "unknown-entity",
			EntityType: "spaceship", // not supported
			Operation:  OpCreate,
		},
	}

	result, err := svc.Push(context.Background(), userID, items)
	require.NoError(t, err)
	require.Len(t, result.Results, 2)

	applied := result.Results[0]
	assert.Equal(t, "valid-create", applied.ClientRef)
	assert.Equal(t, StatusApplied, applied.Status)

	rejected := result.Results[1]
	assert.Equal(t, "unknown-entity", rejected.ClientRef)
	assert.Equal(t, StatusRejected, rejected.Status)
}

func TestPush_NoPayload_Rejected(t *testing.T) {
	store := &mockSyncStore{}
	svc := newSyncSvc(store)

	items := []PushItem{
		{
			ClientRef:  "no-payload",
			EntityType: EntityTransaction,
			Operation:  OpCreate,
			TxnPayload: nil, // missing
		},
	}

	result, err := svc.Push(context.Background(), uuid.New(), items)
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	assert.Equal(t, StatusRejected, result.Results[0].Status)
}

// --- Multi-entity push -------------------------------------------------------
//
// The generic pipeline in sync_push_entities.go is shared by five entities, so
// these exercise it through accounts (the entity with the smallest store
// surface) and then cover what is genuinely entity-specific elsewhere.

// accountSyncStore adds the account CRUD surface to the sync mock.
type accountSyncStore struct {
	mockSyncStore

	account       sqlc.Account
	getErr        error
	created       *sqlc.CreateAccountParams
	updated       *sqlc.UpdateAccountParams
	deletedID     *uuid.UUID
	txnsOnAccount int64
}

func (m *accountSyncStore) GetAccount(ctx context.Context, arg sqlc.GetAccountParams) (sqlc.Account, error) {
	if m.getErr != nil {
		return sqlc.Account{}, m.getErr
	}
	return m.account, nil
}

func (m *accountSyncStore) CreateAccount(ctx context.Context, arg sqlc.CreateAccountParams) (sqlc.Account, error) {
	m.created = &arg
	return m.account, nil
}

func (m *accountSyncStore) UpdateAccount(ctx context.Context, arg sqlc.UpdateAccountParams) (sqlc.Account, error) {
	m.updated = &arg
	return m.account, nil
}

func (m *accountSyncStore) SoftDeleteAccount(ctx context.Context, arg sqlc.SoftDeleteAccountParams) (uuid.UUID, error) {
	m.deletedID = &arg.ID
	return arg.ID, nil
}

func (m *accountSyncStore) CountTransactionsByAccount(ctx context.Context, arg sqlc.CountTransactionsByAccountParams) (int64, error) {
	return m.txnsOnAccount, nil
}

func newAccountSyncSvc(store *accountSyncStore) *SyncService {
	return NewSyncService(store, zerolog.Nop(), SyncServices{
		Transaction: &TransactionService{store: store, now: time.Now},
		Account:     NewAccountService(store),
		Category:    NewCategoryService(store),
		Budget:      NewBudgetService(store),
		SavingsGoal: NewSavingsGoalService(store),
		Recurring:   NewRecurringTransactionService(store),
	})
}

func serverAccount(updatedAt time.Time) sqlc.Account {
	return sqlc.Account{
		ID:          uuid.New(),
		Name:        "Efectivo",
		AccountType: "cash",
		Currency:    "COP",
		UpdatedAt:   pgTs(updatedAt),
	}
}

func TestPush_CreateAccount(t *testing.T) {
	store := &accountSyncStore{account: serverAccount(time.Now())}
	svc := newAccountSyncSvc(store)

	res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
		ClientRef:  "a1",
		EntityType: EntityAccount,
		Operation:  OpCreate,
		Payload: json.RawMessage(`{
			"name": "Bancolombia",
			"account_type": "checking",
			"currency": "COP",
			"initial_balance": "150000.50"
		}`),
	}})

	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	assert.Equal(t, StatusApplied, res.Results[0].Status)
	require.NotNil(t, store.created)
	assert.Equal(t, "Bancolombia", store.created.Name)
	// Money must survive the wire as an exact decimal, never a float.
	assert.Equal(t, "150000.5", store.created.InitialBalance.String())
}

func TestPush_CreateAccountDefaultsBalanceToZero(t *testing.T) {
	store := &accountSyncStore{account: serverAccount(time.Now())}
	svc := newAccountSyncSvc(store)

	_, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
		ClientRef:  "a1",
		EntityType: EntityAccount,
		Operation:  OpCreate,
		Payload:    json.RawMessage(`{"name": "Efectivo", "account_type": "cash"}`),
	}})

	require.NoError(t, err)
	require.NotNil(t, store.created)
	assert.True(t, store.created.InitialBalance.IsZero(), "an account has to start somewhere")
}

func TestPush_UpdateAccountConflictWhenServerIsNewer(t *testing.T) {
	serverAt := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	store := &accountSyncStore{account: serverAccount(serverAt)}
	svc := newAccountSyncSvc(store)

	// The client edited from a snapshot taken an hour before the server's.
	clientAt := serverAt.Add(-time.Hour)
	id := store.account.ID

	res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
		ClientRef:       "a1",
		EntityType:      EntityAccount,
		Operation:       OpUpdate,
		EntityID:        &id,
		ClientUpdatedAt: &clientAt,
		Payload:         json.RawMessage(`{"name": "Renombrada", "account_type": "cash"}`),
	}})

	require.NoError(t, err)
	assert.Equal(t, StatusConflict, res.Results[0].Status)
	assert.NotNil(t, res.Results[0].ServerEntity, "the client needs the winning version to reconcile")
	assert.Nil(t, store.updated, "a conflict must not write")
}

func TestPush_UpdateAccountAppliesWhenClientIsNewer(t *testing.T) {
	serverAt := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	store := &accountSyncStore{account: serverAccount(serverAt)}
	svc := newAccountSyncSvc(store)

	clientAt := serverAt.Add(time.Hour)
	id := store.account.ID

	res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
		ClientRef:       "a1",
		EntityType:      EntityAccount,
		Operation:       OpUpdate,
		EntityID:        &id,
		ClientUpdatedAt: &clientAt,
		Payload:         json.RawMessage(`{"name": "Renombrada", "account_type": "cash"}`),
	}})

	require.NoError(t, err)
	assert.Equal(t, StatusApplied, res.Results[0].Status)
	require.NotNil(t, store.updated)
	assert.Equal(t, "Renombrada", store.updated.Name)
}

// Sub-second differences come from serialisation, not from a user edit, so they
// must not be reported as conflicts.
func TestPush_UpdateAccountIgnoresSubSecondSkew(t *testing.T) {
	serverAt := time.Date(2026, 8, 8, 12, 0, 0, 900_000_000, time.UTC)
	store := &accountSyncStore{account: serverAccount(serverAt)}
	svc := newAccountSyncSvc(store)

	clientAt := serverAt.Truncate(time.Second)
	id := store.account.ID

	res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
		ClientRef:       "a1",
		EntityType:      EntityAccount,
		Operation:       OpUpdate,
		EntityID:        &id,
		ClientUpdatedAt: &clientAt,
		Payload:         json.RawMessage(`{"name": "Renombrada", "account_type": "cash"}`),
	}})

	require.NoError(t, err)
	assert.Equal(t, StatusApplied, res.Results[0].Status)
}

func TestPush_DeleteAccount(t *testing.T) {
	store := &accountSyncStore{account: serverAccount(time.Now())}
	svc := newAccountSyncSvc(store)
	id := store.account.ID

	res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
		ClientRef:  "a1",
		EntityType: EntityAccount,
		Operation:  OpDelete,
		EntityID:   &id,
	}})

	require.NoError(t, err)
	assert.Equal(t, StatusApplied, res.Results[0].Status)
	require.NotNil(t, store.deletedID)
	assert.Equal(t, id, *store.deletedID)
}

func TestPush_RejectsMissingEntityID(t *testing.T) {
	store := &accountSyncStore{account: serverAccount(time.Now())}
	svc := newAccountSyncSvc(store)

	for _, op := range []PushOperation{OpUpdate, OpDelete} {
		res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
			ClientRef:  "a1",
			EntityType: EntityAccount,
			Operation:  op,
			Payload:    json.RawMessage(`{"name": "X", "account_type": "cash"}`),
		}})

		require.NoError(t, err)
		assert.Equal(t, StatusRejected, res.Results[0].Status, "op %s", op)
		require.NotNil(t, res.Results[0].Error)
		assert.Contains(t, *res.Results[0].Error, "entity_id is required")
	}
}

func TestPush_RejectsMissingPayload(t *testing.T) {
	store := &accountSyncStore{account: serverAccount(time.Now())}
	svc := newAccountSyncSvc(store)

	res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
		ClientRef:  "a1",
		EntityType: EntityAccount,
		Operation:  OpCreate,
	}})

	require.NoError(t, err)
	assert.Equal(t, StatusRejected, res.Results[0].Status)
	assert.Nil(t, store.created, "a rejected item must not reach the database")
}

func TestPush_RejectsUnknownOperation(t *testing.T) {
	store := &accountSyncStore{account: serverAccount(time.Now())}
	svc := newAccountSyncSvc(store)

	res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
		ClientRef:  "a1",
		EntityType: EntityAccount,
		Operation:  PushOperation("upsert"),
		Payload:    json.RawMessage(`{"name": "X", "account_type": "cash"}`),
	}})

	require.NoError(t, err)
	assert.Equal(t, StatusRejected, res.Results[0].Status)
	require.NotNil(t, res.Results[0].Error)
	assert.Contains(t, *res.Results[0].Error, "unknown operation")
}

// The whole point of per-item results: one bad apple must not spoil the batch.
func TestPush_BadItemDoesNotAbortTheBatch(t *testing.T) {
	store := &accountSyncStore{account: serverAccount(time.Now())}
	svc := newAccountSyncSvc(store)

	res, err := svc.Push(context.Background(), uuid.New(), []PushItem{
		{
			ClientRef:  "bad",
			EntityType: EntityAccount,
			Operation:  OpCreate,
			Payload:    json.RawMessage(`{"name": "X", "account_type": "cash", "initial_balance": "no soy un número"}`),
		},
		{
			ClientRef:  "good",
			EntityType: EntityAccount,
			Operation:  OpCreate,
			Payload:    json.RawMessage(`{"name": "Buena", "account_type": "cash"}`),
		},
	})

	require.NoError(t, err)
	require.Len(t, res.Results, 2)
	assert.Equal(t, StatusRejected, res.Results[0].Status)
	assert.Equal(t, StatusApplied, res.Results[1].Status)
	assert.Equal(t, "Buena", store.created.Name)
}

// A malformed amount is the user's typo, not a server fault. It must name the
// field, because "internal server error" is all the client could otherwise show
// for something only the user can fix.
func TestPush_MalformedAmountNamesTheField(t *testing.T) {
	store := &accountSyncStore{account: serverAccount(time.Now())}
	svc := newAccountSyncSvc(store)

	res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
		ClientRef:  "a1",
		EntityType: EntityAccount,
		Operation:  OpCreate,
		Payload:    json.RawMessage(`{"name": "X", "account_type": "cash", "initial_balance": "abc"}`),
	}})

	require.NoError(t, err)
	assert.Equal(t, StatusRejected, res.Results[0].Status)
	require.NotNil(t, res.Results[0].Error)
	assert.Contains(t, *res.Results[0].Error, "initial_balance")
	assert.NotContains(t, *res.Results[0].Error, "internal server error")
}

func TestPush_StillRejectsAnUnknownEntityType(t *testing.T) {
	store := &accountSyncStore{account: serverAccount(time.Now())}
	svc := newAccountSyncSvc(store)

	res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
		ClientRef:  "x1",
		EntityType: PushEntityType("tracking_period"),
		Operation:  OpCreate,
		Payload:    json.RawMessage(`{}`),
	}})

	require.NoError(t, err)
	assert.Equal(t, StatusRejected, res.Results[0].Status)
	require.NotNil(t, res.Results[0].Error)
	assert.Contains(t, *res.Results[0].Error, "unsupported entity_type")
}

// Contributions move a goal's current_amount, and nothing in the domain undoes
// that, so editing or deleting one over sync is refused rather than half-done.
func TestPush_ContributionRejectsUpdateAndDelete(t *testing.T) {
	store := &accountSyncStore{account: serverAccount(time.Now())}
	svc := newAccountSyncSvc(store)
	id := uuid.New()

	for _, op := range []PushOperation{OpUpdate, OpDelete} {
		res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
			ClientRef:  "c1",
			EntityType: EntityContribution,
			Operation:  op,
			EntityID:   &id,
			Payload:    json.RawMessage(`{"savings_goal_id": "` + id.String() + `", "amount": "1000"}`),
		}})

		require.NoError(t, err)
		assert.Equal(t, StatusRejected, res.Results[0].Status, "op %s", op)
		require.NotNil(t, res.Results[0].Error)
		assert.Contains(t, *res.Results[0].Error, "only supports create")
	}
}

func TestPush_ContributionRequiresAGoal(t *testing.T) {
	store := &accountSyncStore{account: serverAccount(time.Now())}
	svc := newAccountSyncSvc(store)

	res, err := svc.Push(context.Background(), uuid.New(), []PushItem{{
		ClientRef:  "c1",
		EntityType: EntityContribution,
		Operation:  OpCreate,
		Payload:    json.RawMessage(`{"amount": "1000"}`),
	}})

	require.NoError(t, err)
	assert.Equal(t, StatusRejected, res.Results[0].Status)
	require.NotNil(t, res.Results[0].Error)
	assert.Contains(t, *res.Results[0].Error, "savings_goal_id is required")
}
