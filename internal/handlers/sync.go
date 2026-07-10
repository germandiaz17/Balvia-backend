package handlers

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/middleware"
	"github.com/germandiaz17/Balvia-backend/internal/services"
)

// SyncHandler exposes GET /sync/pull and POST /sync/push.
type SyncHandler struct {
	svc *services.SyncService
}

// NewSyncHandler wires the handler with its service.
func NewSyncHandler(svc *services.SyncService) *SyncHandler {
	return &SyncHandler{svc: svc}
}

// Register mounts the routes under the provided router (the caller is
// responsible for applying the auth middleware).
func (h *SyncHandler) Register(r fiber.Router) {
	g := r.Group("/sync")
	g.Get("/pull", h.Pull)
	g.Post("/push", h.Push)
}

// Pull handles GET /sync/pull?since=<RFC3339>[&page_size=<int>].
//
// Query params:
//
//	since     – RFC3339 timestamp (required). Only rows with updated_at > since
//	            are returned.  Pass the server_time from the previous pull.
//	            Omit or pass "0001-01-01T00:00:00Z" for the initial full sync.
//	page_size – max rows per entity (default 200, max 500).
func (h *SyncHandler) Pull(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}

	sinceStr := c.Query("since")
	var since time.Time
	if sinceStr != "" {
		var err error
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "since must be an RFC3339 timestamp (e.g. 2026-07-09T00:00:00Z)")
		}
	}
	// Empty sinceStr → since = time.Time{} (zero value) → returns all rows.

	pageSize := c.QueryInt("page_size", 0) // 0 → service uses defaultPageSize

	result, err := h.svc.Pull(c.Context(), userID, since, pageSize)
	if err != nil {
		return mapDomainError(err)
	}

	return c.JSON(toSyncPullResponse(result))
}

// Push handles POST /sync/push.
//
// Body:  { "items": [ <PushItem>, ... ] }
// Response: 200 { "results": [ <PushItemResult>, ... ] }
//
// The response is always 200 regardless of per-item outcomes (applied /
// conflict / rejected / skipped).  A non-200 status signals a transport-level
// error (auth failure, malformed body, server crash).
func (h *SyncHandler) Push(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}

	var req syncPushRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if len(req.Items) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "items must not be empty")
	}
	const maxBatchSize = 100
	if len(req.Items) > maxBatchSize {
		return fiber.NewError(fiber.StatusBadRequest, "batch size exceeds maximum (100 items)")
	}

	items := make([]services.PushItem, 0, len(req.Items))
	for _, ri := range req.Items {
		item, err := parsePushItemRequest(ri)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "item "+ri.ClientRef+": "+err.Error())
		}
		items = append(items, item)
	}

	result, err := h.svc.Push(c.Context(), userID, items)
	if err != nil {
		return mapDomainError(err)
	}

	return c.JSON(result)
}

// --- Request wrappers -------------------------------------------------------

type syncPushRequest struct {
	Items []syncPushItemRequest `json:"items"`
}

type syncPushItemRequest struct {
	ClientRef       string                           `json:"client_ref"`
	EntityType      string                           `json:"entity_type"`
	Operation       string                           `json:"operation"`
	EntityID        *string                          `json:"entity_id,omitempty"`
	ClientUpdatedAt *string                          `json:"client_updated_at,omitempty"`
	TxnPayload      *services.PushTransactionPayload `json:"transaction_payload,omitempty"`
}

func parsePushItemRequest(r syncPushItemRequest) (services.PushItem, error) {
	item := services.PushItem{
		ClientRef:  r.ClientRef,
		EntityType: services.PushEntityType(r.EntityType),
		Operation:  services.PushOperation(r.Operation),
		TxnPayload: r.TxnPayload,
	}

	if r.EntityID != nil {
		id, err := uuid.Parse(*r.EntityID)
		if err != nil {
			return services.PushItem{}, fiber.NewError(fiber.StatusBadRequest, "invalid entity_id: "+err.Error())
		}
		item.EntityID = &id
	}

	if r.ClientUpdatedAt != nil {
		t, err := time.Parse(time.RFC3339, *r.ClientUpdatedAt)
		if err != nil {
			return services.PushItem{}, fiber.NewError(fiber.StatusBadRequest, "client_updated_at must be RFC3339")
		}
		item.ClientUpdatedAt = &t
	}

	return item, nil
}

// --- Pull response shaping --------------------------------------------------
// Sync-specific response types have a "sync" prefix to avoid conflicts with
// the per-entity handler response types defined in their own files.

type syncPullResponse struct {
	ServerTime            string                     `json:"server_time"`
	HasMore               bool                       `json:"has_more"`
	Transactions          []syncTransactionResponse  `json:"transactions"`
	Accounts              []accountResponse          `json:"accounts"`
	Categories            []categoryResponse         `json:"categories"`
	Budgets               []budgetResponse           `json:"budgets"`
	SavingsGoals          []goalResponse             `json:"savings_goals"`
	GoalContributions     []syncContributionResponse `json:"goal_contributions"`
	RecurringTransactions []recurringResponse        `json:"recurring_transactions"`
	TrackingPeriods       []syncPeriodResponse       `json:"tracking_periods"`
}

// syncTransactionResponse extends the regular transactionResponse with
// deleted_at and updated_at so the client can handle soft-deletes.
type syncTransactionResponse struct {
	ID                uuid.UUID  `json:"id"`
	TrackingPeriodID  uuid.UUID  `json:"tracking_period_id"`
	AccountID         uuid.UUID  `json:"account_id"`
	CategoryID        *uuid.UUID `json:"category_id"`
	TransactionType   string     `json:"transaction_type"`
	Amount            string     `json:"amount"`
	Currency          string     `json:"currency"`
	Description       *string    `json:"description,omitempty"`
	Notes             *string    `json:"notes,omitempty"`
	TransactionDate   string     `json:"transaction_date"`
	TransferAccountID *uuid.UUID `json:"transfer_account_id,omitempty"`
	ClientID          *string    `json:"client_id,omitempty"`
	CreatedAt         string     `json:"created_at"`
	UpdatedAt         string     `json:"updated_at"`
	DeletedAt         *string    `json:"deleted_at,omitempty"`
}

// syncContributionResponse adds updated_at to the existing contributionResponse.
type syncContributionResponse struct {
	ID               uuid.UUID `json:"id"`
	SavingsGoalID    uuid.UUID `json:"savings_goal_id"`
	TrackingPeriodID uuid.UUID `json:"tracking_period_id"`
	Amount           string    `json:"amount"`
	ContributionDate string    `json:"contribution_date"`
	Notes            *string   `json:"notes,omitempty"`
	CreatedAt        string    `json:"created_at"`
	UpdatedAt        string    `json:"updated_at"`
}

// syncPeriodResponse adds updated_at to the tracking period wire shape.
type syncPeriodResponse struct {
	ID                 uuid.UUID `json:"id"`
	SequenceNumber     int32     `json:"sequence_number"`
	StartDate          string    `json:"start_date"`
	EndDate            string    `json:"end_date"`
	Status             string    `json:"status"`
	ConfigStartDay     int16     `json:"config_start_day"`
	ConfigDurationDays int16     `json:"config_duration_days"`
	ClosedAt           *string   `json:"closed_at,omitempty"`
	UpdatedAt          string    `json:"updated_at"`
}

func toSyncPullResponse(r services.PullResult) syncPullResponse {
	txns := make([]syncTransactionResponse, 0, len(r.Transactions))
	for _, t := range r.Transactions {
		txns = append(txns, toSyncTransactionResponse(t))
	}

	accts := make([]accountResponse, 0, len(r.Accounts))
	for _, a := range r.Accounts {
		accts = append(accts, toAccountResponse(a))
	}

	cats := make([]categoryResponse, 0, len(r.Categories))
	for _, c := range r.Categories {
		cats = append(cats, toCategoryResponse(c))
	}

	bgets := make([]budgetResponse, 0, len(r.Budgets))
	for _, b := range r.Budgets {
		bgets = append(bgets, toBudgetResponse(b))
	}

	goals := make([]goalResponse, 0, len(r.SavingsGoals))
	for _, g := range r.SavingsGoals {
		goals = append(goals, toGoalResponse(g))
	}

	contribs := make([]syncContributionResponse, 0, len(r.GoalContributions))
	for _, c := range r.GoalContributions {
		contribs = append(contribs, toSyncContributionResponse(c))
	}

	recurrings := make([]recurringResponse, 0, len(r.RecurringTransactions))
	for _, rt := range r.RecurringTransactions {
		recurrings = append(recurrings, toRecurringResponse(rt))
	}

	periods := make([]syncPeriodResponse, 0, len(r.TrackingPeriods))
	for _, p := range r.TrackingPeriods {
		periods = append(periods, toSyncPeriodResponse(p))
	}

	return syncPullResponse{
		ServerTime:            r.ServerTime.UTC().Format(time.RFC3339),
		HasMore:               r.HasMore,
		Transactions:          txns,
		Accounts:              accts,
		Categories:            cats,
		Budgets:               bgets,
		SavingsGoals:          goals,
		GoalContributions:     contribs,
		RecurringTransactions: recurrings,
		TrackingPeriods:       periods,
	}
}

func toSyncTransactionResponse(t sqlc.Transaction) syncTransactionResponse {
	r := syncTransactionResponse{
		ID:                t.ID,
		TrackingPeriodID:  t.TrackingPeriodID,
		AccountID:         t.AccountID,
		CategoryID:        nullUUIDToPtr(t.CategoryID),
		TransactionType:   t.TransactionType,
		Amount:            t.Amount.String(),
		Currency:          t.Currency,
		Description:       t.Description,
		Notes:             t.Notes,
		TransactionDate:   t.TransactionDate.Time.Format(dateLayout),
		TransferAccountID: nullUUIDToPtr(t.TransferAccountID),
		ClientID:          t.ClientID,
		CreatedAt:         t.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:         t.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if t.DeletedAt.Valid {
		s := t.DeletedAt.Time.UTC().Format(time.RFC3339)
		r.DeletedAt = &s
	}
	return r
}

func toSyncContributionResponse(c sqlc.SavingsGoalContribution) syncContributionResponse {
	return syncContributionResponse{
		ID:               c.ID,
		SavingsGoalID:    c.SavingsGoalID,
		TrackingPeriodID: c.TrackingPeriodID,
		Amount:           c.Amount.String(),
		ContributionDate: c.ContributionDate.Time.Format(dateLayout),
		Notes:            c.Notes,
		CreatedAt:        c.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:        c.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func toSyncPeriodResponse(p sqlc.TrackingPeriod) syncPeriodResponse {
	pr := syncPeriodResponse{
		ID:                 p.ID,
		SequenceNumber:     p.SequenceNumber,
		StartDate:          p.StartDate.Time.Format(dateLayout),
		EndDate:            p.EndDate.Time.Format(dateLayout),
		Status:             p.Status,
		ConfigStartDay:     p.ConfigStartDay,
		ConfigDurationDays: p.ConfigDurationDays,
		UpdatedAt:          p.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if p.ClosedAt.Valid {
		s := p.ClosedAt.Time.UTC().Format(time.RFC3339)
		pr.ClosedAt = &s
	}
	return pr
}
