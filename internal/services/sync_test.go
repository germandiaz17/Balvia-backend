package services

import (
	"context"
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
	return NewSyncService(store, txnSvc, zerolog.Nop())
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
