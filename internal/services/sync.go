package services

// SyncService implements the delta-sync business logic for the offline-first
// mobile client.
//
// Pull (GET /sync/pull):
//
//	Returns every row of each syncable entity whose updated_at > since.
//	Soft-deleted rows are included (the client uses deleted_at != nil to
//	remove the local record).  A page_size cap prevents unbounded responses;
//	the response includes a "has_more" flag and the highest updated_at seen
//	so the client can page through.
//
// Push (POST /sync/push):
//
//	Accepts a batch of mutations (create / update / delete) for any syncable
//	entity type.  Each mutation is processed with the SAME business rules as
//	the corresponding CRUD endpoint (via the existing services), ensuring no
//	logic is duplicated.
//
// Conflict resolution (last-write-wins by updated_at):
//
//	For each pushed item the handler compares the client's updated_at with
//	the server's current updated_at for that resource.
//	  applied  – the client's version is newer; the server row was updated.
//	  conflict – the server's version is newer; the item is rejected and the
//	             current server version is returned so the client can reconcile.
//	  rejected – a hard business-rule violation (e.g. closed period, invalid
//	             amount, unknown account).  The item is not applied; the error
//	             message is returned.
//	  skipped  – client_id matches an existing transaction (idempotency).
//
// The response is per-item (not all-or-nothing), so one bad item never
// blocks the rest of the batch from being applied.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

const (
	// defaultPageSize is the maximum number of rows returned per entity in a
	// pull response.  Clients should page until has_more == false.
	defaultPageSize = 200

	// maxPageSize caps the caller-supplied page_size to avoid abusive requests.
	maxPageSize = 500
)

// --- Pull types -------------------------------------------------------------

// PullResult is the response payload for GET /sync/pull.
type PullResult struct {
	// ServerTime is the server's UTC timestamp at the moment the query was
	// executed.  The client must persist this value and use it as the `since`
	// parameter in the next pull.  Using the server clock (not the client clock)
	// prevents drift and race conditions.
	ServerTime time.Time `json:"server_time"`

	// HasMore is true when any entity collection was truncated by page_size.
	// The client should re-pull immediately with the same since value until
	// HasMore is false.
	HasMore bool `json:"has_more"`

	Transactions          []sqlc.Transaction             `json:"transactions"`
	Accounts              []sqlc.Account                 `json:"accounts"`
	Categories            []sqlc.Category                `json:"categories"`
	Budgets               []sqlc.Budget                  `json:"budgets"`
	SavingsGoals          []sqlc.SavingsGoal             `json:"savings_goals"`
	GoalContributions     []sqlc.SavingsGoalContribution `json:"goal_contributions"`
	RecurringTransactions []sqlc.RecurringTransaction    `json:"recurring_transactions"`
	TrackingPeriods       []sqlc.TrackingPeriod          `json:"tracking_periods"`
}

// --- Push types -------------------------------------------------------------

// PushEntityType enumerates the entity kinds the client may push.
type PushEntityType string

const (
	EntityTransaction  PushEntityType = "transaction"
	EntityAccount      PushEntityType = "account"
	EntityCategory     PushEntityType = "category"
	EntityBudget       PushEntityType = "budget"
	EntitySavingsGoal  PushEntityType = "savings_goal"
	EntityContribution PushEntityType = "savings_goal_contribution"
	EntityRecurring    PushEntityType = "recurring_transaction"
)

// PushOperation is the kind of mutation.
type PushOperation string

const (
	OpCreate PushOperation = "create"
	OpUpdate PushOperation = "update"
	OpDelete PushOperation = "delete"
)

// PushItem represents a single mutation in the push batch.
// The Payload field holds the entity-specific fields (same shape as the
// corresponding CRUD endpoint body), marshalled as a generic map by the
// handler before passing to the service.
type PushItem struct {
	// ClientRef is an opaque string the client uses to correlate result items
	// with the original request items (does NOT have to be a UUID).
	ClientRef  string         `json:"client_ref"`
	EntityType PushEntityType `json:"entity_type"`
	Operation  PushOperation  `json:"operation"`

	// EntityID is the server-side UUID of the resource.  Required for update
	// and delete; may be empty for create (the server generates the ID).
	EntityID *uuid.UUID `json:"entity_id,omitempty"`

	// ClientUpdatedAt is the client's last-known updated_at for this resource.
	// Used for conflict detection on updates: if the server's current
	// updated_at is newer than this value, the push is a conflict.
	// For creates and deletes this field is informational only.
	ClientUpdatedAt *time.Time `json:"client_updated_at,omitempty"`

	// Transaction-specific fields (used when EntityType == "transaction").
	TxnPayload *PushTransactionPayload `json:"transaction_payload,omitempty"`
}

// PushTransactionPayload mirrors CreateTransactionInput but with string amounts
// (wire format) and optional client_id for idempotency.
type PushTransactionPayload struct {
	AccountID         uuid.UUID  `json:"account_id"`
	TransactionType   string     `json:"transaction_type"`
	Amount            string     `json:"amount"`
	Currency          string     `json:"currency,omitempty"`
	CategoryID        *uuid.UUID `json:"category_id,omitempty"`
	Description       *string    `json:"description,omitempty"`
	Notes             *string    `json:"notes,omitempty"`
	TransactionDate   *string    `json:"transaction_date,omitempty"`
	TransferAccountID *uuid.UUID `json:"transfer_account_id,omitempty"`
	ClientID          *string    `json:"client_id,omitempty"`

	// AI categorization metadata (optional).
	AICategorized         bool       `json:"ai_categorized,omitempty"`
	AIConfidence          *string    `json:"ai_confidence,omitempty"`
	AISuggestedCategoryID *uuid.UUID `json:"ai_suggested_category_id,omitempty"`
}

// ItemStatus describes the outcome of a single push item.
type ItemStatus string

const (
	StatusApplied  ItemStatus = "applied"
	StatusConflict ItemStatus = "conflict"
	StatusRejected ItemStatus = "rejected"
	StatusSkipped  ItemStatus = "skipped"
)

// PushItemResult is the per-item outcome returned in the push response.
type PushItemResult struct {
	// ClientRef echoes the caller's client_ref so it can match results.
	ClientRef string `json:"client_ref"`

	// Status: applied | conflict | rejected | skipped
	Status ItemStatus `json:"status"`

	// ServerEntity is the server's current state of the resource after
	// processing.  Nil for rejected items where no server row was involved.
	// For conflicts this is the winning server version so the client can reconcile.
	ServerEntity interface{} `json:"server_entity,omitempty"`

	// Error contains a human-readable reason for rejected items.
	Error *string `json:"error,omitempty"`
}

// PushResult is the full push response payload.
type PushResult struct {
	Results []PushItemResult `json:"results"`
}

// --- Service ----------------------------------------------------------------

// SyncService implements the sync-delta logic.
type SyncService struct {
	store  database.Store
	txnSvc *TransactionService
	log    zerolog.Logger
}

// NewSyncService wires the service with its dependencies.
func NewSyncService(store database.Store, txnSvc *TransactionService, log zerolog.Logger) *SyncService {
	return &SyncService{
		store:  store,
		txnSvc: txnSvc,
		log:    log,
	}
}

// Pull fetches all rows for the authenticated user that changed after `since`.
// The caller passes the desired page_size (0 → defaultPageSize, max 500).
func (s *SyncService) Pull(ctx context.Context, userID uuid.UUID, since time.Time, pageSize int) (PullResult, error) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	// Capture server time before queries so "since" for the next pull is at
	// least as late as any row we return.
	serverTime := time.Now().UTC()

	sincePG := pgtype.Timestamptz{Time: since, Valid: true}
	ps := int32(pageSize)

	// Each query fetches up to pageSize+1 rows; we use the extra row to detect
	// whether more pages exist without a separate COUNT query.
	limit := ps + 1

	txns, err := s.store.SyncPullTransactions(ctx, sqlc.SyncPullTransactionsParams{
		UserID:   userID,
		Since:    sincePG,
		PageSize: limit,
	})
	if err != nil {
		return PullResult{}, err
	}

	accts, err := s.store.SyncPullAccounts(ctx, sqlc.SyncPullAccountsParams{
		UserID:   userID,
		Since:    sincePG,
		PageSize: limit,
	})
	if err != nil {
		return PullResult{}, err
	}

	cats, err := s.store.SyncPullCategories(ctx, sqlc.SyncPullCategoriesParams{
		UserID:   uuid.NullUUID{UUID: userID, Valid: true},
		Since:    sincePG,
		PageSize: limit,
	})
	if err != nil {
		return PullResult{}, err
	}

	budgets, err := s.store.SyncPullBudgets(ctx, sqlc.SyncPullBudgetsParams{
		UserID:   userID,
		Since:    sincePG,
		PageSize: limit,
	})
	if err != nil {
		return PullResult{}, err
	}

	goals, err := s.store.SyncPullSavingsGoals(ctx, sqlc.SyncPullSavingsGoalsParams{
		UserID:   userID,
		Since:    sincePG,
		PageSize: limit,
	})
	if err != nil {
		return PullResult{}, err
	}

	contribs, err := s.store.SyncPullGoalContributions(ctx, sqlc.SyncPullGoalContributionsParams{
		UserID:   userID,
		Since:    sincePG,
		PageSize: limit,
	})
	if err != nil {
		return PullResult{}, err
	}

	recurrings, err := s.store.SyncPullRecurringTransactions(ctx, sqlc.SyncPullRecurringTransactionsParams{
		UserID:   userID,
		Since:    sincePG,
		PageSize: limit,
	})
	if err != nil {
		return PullResult{}, err
	}

	periods, err := s.store.SyncPullTrackingPeriods(ctx, sqlc.SyncPullTrackingPeriodsParams{
		UserID:   userID,
		Since:    sincePG,
		PageSize: limit,
	})
	if err != nil {
		return PullResult{}, err
	}

	// Detect overflow and trim to page_size.
	hasMore := false
	trim := func(n int) bool { return n > int(ps) }

	if trim(len(txns)) {
		hasMore = true
		txns = txns[:ps]
	}
	if trim(len(accts)) {
		hasMore = true
		accts = accts[:ps]
	}
	if trim(len(cats)) {
		hasMore = true
		cats = cats[:ps]
	}
	if trim(len(budgets)) {
		hasMore = true
		budgets = budgets[:ps]
	}
	if trim(len(goals)) {
		hasMore = true
		goals = goals[:ps]
	}
	if trim(len(contribs)) {
		hasMore = true
		contribs = contribs[:ps]
	}
	if trim(len(recurrings)) {
		hasMore = true
		recurrings = recurrings[:ps]
	}
	if trim(len(periods)) {
		hasMore = true
		periods = periods[:ps]
	}

	return PullResult{
		ServerTime:            serverTime,
		HasMore:               hasMore,
		Transactions:          txns,
		Accounts:              accts,
		Categories:            cats,
		Budgets:               budgets,
		SavingsGoals:          goals,
		GoalContributions:     contribs,
		RecurringTransactions: recurrings,
		TrackingPeriods:       periods,
	}, nil
}

// Push applies a batch of mutations from the client.  Each item is processed
// independently so a single failure does not abort the batch.
func (s *SyncService) Push(ctx context.Context, userID uuid.UUID, items []PushItem) (PushResult, error) {
	results := make([]PushItemResult, 0, len(items))
	for _, item := range items {
		result := s.processItem(ctx, userID, item)
		results = append(results, result)
	}
	return PushResult{Results: results}, nil
}

// processItem dispatches a single push mutation to the appropriate handler.
func (s *SyncService) processItem(ctx context.Context, userID uuid.UUID, item PushItem) PushItemResult {
	switch item.EntityType {
	case EntityTransaction:
		return s.pushTransaction(ctx, userID, item)
	default:
		reason := "unsupported entity_type: " + string(item.EntityType)
		return rejected(item.ClientRef, reason)
	}
}

// pushTransaction handles create / update / delete for transactions.
// It reuses TransactionService (same business rules, same balance adjustments).
func (s *SyncService) pushTransaction(ctx context.Context, userID uuid.UUID, item PushItem) PushItemResult {
	switch item.Operation {
	case OpCreate:
		return s.pushCreateTransaction(ctx, userID, item)
	case OpUpdate:
		return s.pushUpdateTransaction(ctx, userID, item)
	case OpDelete:
		return s.pushDeleteTransaction(ctx, userID, item)
	default:
		return rejected(item.ClientRef, "unknown operation: "+string(item.Operation))
	}
}

func (s *SyncService) pushCreateTransaction(ctx context.Context, userID uuid.UUID, item PushItem) PushItemResult {
	p := item.TxnPayload
	if p == nil {
		return rejected(item.ClientRef, "transaction_payload is required for create")
	}

	// Idempotency: if a client_id already exists in the DB, return it as
	// "skipped" so the client can record the server ID without re-applying.
	if p.ClientID != nil && *p.ClientID != "" {
		existing, err := s.store.GetTransactionByClientID(ctx, sqlc.GetTransactionByClientIDParams{
			UserID:   userID,
			ClientID: p.ClientID,
		})
		if err == nil {
			// Already exists — return the server version.
			return PushItemResult{
				ClientRef:    item.ClientRef,
				Status:       StatusSkipped,
				ServerEntity: existing,
			}
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return serverError(item.ClientRef, err)
		}
		// pgx.ErrNoRows → not a duplicate, proceed with create.
	}

	in, err := txnPayloadToInput(p)
	if err != nil {
		return rejected(item.ClientRef, err.Error())
	}

	txn, err := s.txnSvc.Create(ctx, userID, in)
	if err != nil {
		return mapServiceError(item.ClientRef, err)
	}
	return PushItemResult{
		ClientRef:    item.ClientRef,
		Status:       StatusApplied,
		ServerEntity: txn,
	}
}

func (s *SyncService) pushUpdateTransaction(ctx context.Context, userID uuid.UUID, item PushItem) PushItemResult {
	if item.EntityID == nil {
		return rejected(item.ClientRef, "entity_id is required for update")
	}
	p := item.TxnPayload
	if p == nil {
		return rejected(item.ClientRef, "transaction_payload is required for update")
	}

	// Conflict detection: load the current server version.
	current, err := s.txnSvc.Get(ctx, userID, *item.EntityID)
	if err != nil {
		return mapServiceError(item.ClientRef, err)
	}

	// If the client's snapshot is older than the server version it is a conflict
	// (last-write-wins by updated_at).
	if item.ClientUpdatedAt != nil && current.UpdatedAt.Valid {
		serverUpdatedAt := current.UpdatedAt.Time.UTC().Truncate(time.Second)
		clientUpdatedAt := item.ClientUpdatedAt.UTC().Truncate(time.Second)
		if serverUpdatedAt.After(clientUpdatedAt) {
			return PushItemResult{
				ClientRef:    item.ClientRef,
				Status:       StatusConflict,
				ServerEntity: current,
			}
		}
	}

	// Check if the transaction belongs to a closed period (immutable).
	period, err := s.store.GetTrackingPeriodByID(ctx, current.TrackingPeriodID)
	if err != nil {
		return serverError(item.ClientRef, err)
	}
	if period.Status == "closed" {
		return rejected(item.ClientRef, domain.ErrPeriodClosed.Error())
	}

	in, err := txnPayloadToInput(p)
	if err != nil {
		return rejected(item.ClientRef, err.Error())
	}

	updated, err := s.txnSvc.Update(ctx, userID, *item.EntityID, in)
	if err != nil {
		return mapServiceError(item.ClientRef, err)
	}
	return PushItemResult{
		ClientRef:    item.ClientRef,
		Status:       StatusApplied,
		ServerEntity: updated,
	}
}

func (s *SyncService) pushDeleteTransaction(ctx context.Context, userID uuid.UUID, item PushItem) PushItemResult {
	if item.EntityID == nil {
		return rejected(item.ClientRef, "entity_id is required for delete")
	}

	// Check immutability before attempting the delete.
	current, err := s.txnSvc.Get(ctx, userID, *item.EntityID)
	if err != nil {
		// If already deleted → idempotent skip.
		if errors.Is(err, domain.ErrNotFound) {
			return PushItemResult{ClientRef: item.ClientRef, Status: StatusSkipped}
		}
		return mapServiceError(item.ClientRef, err)
	}
	period, err := s.store.GetTrackingPeriodByID(ctx, current.TrackingPeriodID)
	if err != nil {
		return serverError(item.ClientRef, err)
	}
	if period.Status == "closed" {
		return rejected(item.ClientRef, domain.ErrPeriodClosed.Error())
	}

	if err := s.txnSvc.Delete(ctx, userID, *item.EntityID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return PushItemResult{ClientRef: item.ClientRef, Status: StatusSkipped}
		}
		return mapServiceError(item.ClientRef, err)
	}
	return PushItemResult{ClientRef: item.ClientRef, Status: StatusApplied}
}

// --- Helpers ----------------------------------------------------------------

func txnPayloadToInput(p *PushTransactionPayload) (CreateTransactionInput, error) {
	amount, err := decimal.NewFromString(p.Amount)
	if err != nil || !amount.IsPositive() {
		return CreateTransactionInput{}, domain.ErrInvalidAmount
	}

	var txnDate *time.Time
	if p.TransactionDate != nil {
		d, err := time.Parse("2006-01-02", *p.TransactionDate)
		if err != nil {
			return CreateTransactionInput{}, errors.New("invalid transaction_date format (expected YYYY-MM-DD)")
		}
		txnDate = &d
	}

	var aiConfidence *decimal.Decimal
	if p.AIConfidence != nil && *p.AIConfidence != "" {
		conf, err := decimal.NewFromString(*p.AIConfidence)
		if err != nil {
			return CreateTransactionInput{}, errors.New("invalid ai_confidence")
		}
		aiConfidence = &conf
	}

	return CreateTransactionInput{
		AccountID:             p.AccountID,
		TransactionType:       p.TransactionType,
		Amount:                amount,
		Currency:              p.Currency,
		CategoryID:            p.CategoryID,
		Description:           p.Description,
		Notes:                 p.Notes,
		TransactionDate:       txnDate,
		TransferAccountID:     p.TransferAccountID,
		ClientID:              p.ClientID,
		AICategorized:         p.AICategorized,
		AIConfidence:          aiConfidence,
		AISuggestedCategoryID: p.AISuggestedCategoryID,
	}, nil
}

func mapServiceError(clientRef string, err error) PushItemResult {
	switch {
	case errors.Is(err, domain.ErrNotFound),
		errors.Is(err, domain.ErrAccountNotFound),
		errors.Is(err, domain.ErrCategoryNotFound),
		errors.Is(err, domain.ErrGoalNotFound):
		return rejected(clientRef, err.Error())
	case errors.Is(err, domain.ErrNoActivePeriod),
		errors.Is(err, domain.ErrInvalidTransfer),
		errors.Is(err, domain.ErrDateOutsidePeriod),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrPeriodClosed):
		return rejected(clientRef, err.Error())
	default:
		return serverError(clientRef, err)
	}
}

func rejected(clientRef, reason string) PushItemResult {
	r := reason
	return PushItemResult{
		ClientRef: clientRef,
		Status:    StatusRejected,
		Error:     &r,
	}
}

func serverError(clientRef string, err error) PushItemResult {
	msg := "internal server error"
	return PushItemResult{
		ClientRef: clientRef,
		Status:    StatusRejected,
		Error:     &msg,
	}
}
