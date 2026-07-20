package handlers

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

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

	// One malformed item must never poison the whole batch: an aborted batch
	// leaves every other (valid) outbox row on the client stuck in 'pending'
	// forever. Parse failures become per-item rejected results instead.
	items := make([]services.PushItem, 0, len(req.Items))
	rejected := make([]services.PushItemResult, 0)
	for _, ri := range req.Items {
		item, err := parsePushItemRequest(ri)
		if err != nil {
			msg := err.Error()
			rejected = append(rejected, services.PushItemResult{
				ClientRef: ri.ClientRef,
				Status:    services.StatusRejected,
				Error:     &msg,
			})
			continue
		}
		items = append(items, item)
	}

	result := services.PushResult{}
	if len(items) > 0 {
		var err error
		result, err = h.svc.Push(c.Context(), userID, items)
		if err != nil {
			return mapDomainError(err)
		}
	}
	result.Results = append(result.Results, rejected...)

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
//
// Unlike the public CRUD DTOs, every sync wire shape carries the full sync
// envelope — user_id, created_at, updated_at and (where the table soft-deletes)
// deleted_at. The client's pull protocol depends on these: updated_at drives
// cursor pagination, deleted_at propagates soft-deletes, and user_id fills the
// local DB's NOT NULL owner column.

type syncPullResponse struct {
	ServerTime            string                     `json:"server_time"`
	HasMore               bool                       `json:"has_more"`
	Transactions          []syncTransactionResponse  `json:"transactions"`
	Accounts              []syncAccountResponse      `json:"accounts"`
	Categories            []syncCategoryResponse     `json:"categories"`
	Budgets               []syncBudgetResponse       `json:"budgets"`
	SavingsGoals          []syncGoalResponse         `json:"savings_goals"`
	GoalContributions     []syncContributionResponse `json:"goal_contributions"`
	RecurringTransactions []syncRecurringResponse    `json:"recurring_transactions"`
	TrackingPeriods       []syncPeriodResponse       `json:"tracking_periods"`
}

// syncTransactionResponse extends the regular transactionResponse with
// deleted_at and updated_at so the client can handle soft-deletes.
type syncTransactionResponse struct {
	ID                uuid.UUID  `json:"id"`
	UserID            uuid.UUID  `json:"user_id"`
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

type syncAccountResponse struct {
	ID             uuid.UUID `json:"id"`
	UserID         uuid.UUID `json:"user_id"`
	Name           string    `json:"name"`
	AccountType    string    `json:"account_type"`
	Currency       string    `json:"currency"`
	InitialBalance string    `json:"initial_balance"`
	CurrentBalance string    `json:"current_balance"`
	Icon           *string   `json:"icon,omitempty"`
	Color          *string   `json:"color,omitempty"`
	IsArchived     bool      `json:"is_archived"`
	DisplayOrder   int32     `json:"display_order"`
	CreatedAt      string    `json:"created_at"`
	UpdatedAt      string    `json:"updated_at"`
	DeletedAt      *string   `json:"deleted_at,omitempty"`
}

// syncCategoryResponse: user_id is nullable on the wire — system categories
// belong to no user.
type syncCategoryResponse struct {
	ID           uuid.UUID  `json:"id"`
	UserID       *uuid.UUID `json:"user_id"`
	ParentID     *uuid.UUID `json:"parent_id,omitempty"`
	Name         string     `json:"name"`
	CategoryType string     `json:"category_type"`
	Icon         *string    `json:"icon,omitempty"`
	Color        *string    `json:"color,omitempty"`
	IsSystem     bool       `json:"is_system"`
	DisplayOrder int32      `json:"display_order"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
	DeletedAt    *string    `json:"deleted_at,omitempty"`
}

// syncBudgetResponse: budgets hard-delete, so there is no deleted_at.
type syncBudgetResponse struct {
	ID                     uuid.UUID  `json:"id"`
	UserID                 uuid.UUID  `json:"user_id"`
	TrackingPeriodID       uuid.UUID  `json:"tracking_period_id"`
	CategoryID             *uuid.UUID `json:"category_id"`
	Amount                 string     `json:"amount"`
	Currency               string     `json:"currency"`
	AlertThresholdWarning  string     `json:"alert_threshold_warning"`
	AlertThresholdCritical string     `json:"alert_threshold_critical"`
	Notes                  *string    `json:"notes,omitempty"`
	CreatedAt              string     `json:"created_at"`
	UpdatedAt              string     `json:"updated_at"`
}

type syncGoalResponse struct {
	ID              uuid.UUID  `json:"id"`
	UserID          uuid.UUID  `json:"user_id"`
	Name            string     `json:"name"`
	Description     *string    `json:"description,omitempty"`
	Icon            *string    `json:"icon,omitempty"`
	Color           *string    `json:"color,omitempty"`
	TargetAmount    string     `json:"target_amount"`
	CurrentAmount   string     `json:"current_amount"`
	Currency        string     `json:"currency"`
	StartDate       string     `json:"start_date"`
	TargetDate      string     `json:"target_date"`
	Status          string     `json:"status"`
	LinkedAccountID *uuid.UUID `json:"linked_account_id,omitempty"`
	AchievedAt      *string    `json:"achieved_at,omitempty"`
	CreatedAt       string     `json:"created_at"`
	UpdatedAt       string     `json:"updated_at"`
	DeletedAt       *string    `json:"deleted_at,omitempty"`
}

type syncRecurringResponse struct {
	ID                 uuid.UUID  `json:"id"`
	UserID             uuid.UUID  `json:"user_id"`
	AccountID          uuid.UUID  `json:"account_id"`
	CategoryID         *uuid.UUID `json:"category_id"`
	Name               string     `json:"name"`
	TransactionType    string     `json:"transaction_type"`
	Amount             string     `json:"amount"`
	Currency           string     `json:"currency"`
	Description        *string    `json:"description,omitempty"`
	Frequency          string     `json:"frequency"`
	CustomIntervalDays *int32     `json:"custom_interval_days,omitempty"`
	DayOfMonth         *int16     `json:"day_of_month,omitempty"`
	DayOfWeek          *int16     `json:"day_of_week,omitempty"`
	StartDate          string     `json:"start_date"`
	EndDate            *string    `json:"end_date,omitempty"`
	LastGeneratedDate  *string    `json:"last_generated_date,omitempty"`
	NextDueDate        *string    `json:"next_due_date,omitempty"`
	IsActive           bool       `json:"is_active"`
	CreatedAt          string     `json:"created_at"`
	UpdatedAt          string     `json:"updated_at"`
	DeletedAt          *string    `json:"deleted_at,omitempty"`
}

// syncContributionResponse adds the sync envelope to the contribution shape.
// Contributions hard-delete, so there is no deleted_at.
type syncContributionResponse struct {
	ID               uuid.UUID  `json:"id"`
	UserID           uuid.UUID  `json:"user_id"`
	SavingsGoalID    uuid.UUID  `json:"savings_goal_id"`
	TrackingPeriodID uuid.UUID  `json:"tracking_period_id"`
	TransactionID    *uuid.UUID `json:"transaction_id,omitempty"`
	Amount           string     `json:"amount"`
	ContributionDate string     `json:"contribution_date"`
	Notes            *string    `json:"notes,omitempty"`
	CreatedAt        string     `json:"created_at"`
	UpdatedAt        string     `json:"updated_at"`
}

// syncPeriodResponse adds the sync envelope to the tracking period wire shape.
// Periods are never deleted, so there is no deleted_at.
type syncPeriodResponse struct {
	ID                 uuid.UUID `json:"id"`
	UserID             uuid.UUID `json:"user_id"`
	SequenceNumber     int32     `json:"sequence_number"`
	StartDate          string    `json:"start_date"`
	EndDate            string    `json:"end_date"`
	Status             string    `json:"status"`
	ConfigStartDay     int16     `json:"config_start_day"`
	ConfigDurationDays int16     `json:"config_duration_days"`
	ClosedAt           *string   `json:"closed_at,omitempty"`
	CreatedAt          string    `json:"created_at"`
	UpdatedAt          string    `json:"updated_at"`
}

// tsPtr converts a nullable timestamp to an RFC3339 *string for the wire.
func tsPtr(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.UTC().Format(time.RFC3339)
	return &s
}

// datePtr converts a nullable date to a YYYY-MM-DD *string for the wire.
func datePtr(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format(dateLayout)
	return &s
}

func toSyncPullResponse(r services.PullResult) syncPullResponse {
	txns := make([]syncTransactionResponse, 0, len(r.Transactions))
	for _, t := range r.Transactions {
		txns = append(txns, toSyncTransactionResponse(t))
	}

	accts := make([]syncAccountResponse, 0, len(r.Accounts))
	for _, a := range r.Accounts {
		accts = append(accts, toSyncAccountResponse(a))
	}

	cats := make([]syncCategoryResponse, 0, len(r.Categories))
	for _, c := range r.Categories {
		cats = append(cats, toSyncCategoryResponse(c))
	}

	bgets := make([]syncBudgetResponse, 0, len(r.Budgets))
	for _, b := range r.Budgets {
		bgets = append(bgets, toSyncBudgetResponse(b))
	}

	goals := make([]syncGoalResponse, 0, len(r.SavingsGoals))
	for _, g := range r.SavingsGoals {
		goals = append(goals, toSyncGoalResponse(g))
	}

	contribs := make([]syncContributionResponse, 0, len(r.GoalContributions))
	for _, c := range r.GoalContributions {
		contribs = append(contribs, toSyncContributionResponse(c))
	}

	recurrings := make([]syncRecurringResponse, 0, len(r.RecurringTransactions))
	for _, rt := range r.RecurringTransactions {
		recurrings = append(recurrings, toSyncRecurringResponse(rt))
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
		UserID:            t.UserID,
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

func toSyncAccountResponse(a sqlc.Account) syncAccountResponse {
	return syncAccountResponse{
		ID:             a.ID,
		UserID:         a.UserID,
		Name:           a.Name,
		AccountType:    a.AccountType,
		Currency:       a.Currency,
		InitialBalance: a.InitialBalance.String(),
		CurrentBalance: a.CurrentBalance.String(),
		Icon:           a.Icon,
		Color:          a.Color,
		IsArchived:     a.IsArchived,
		DisplayOrder:   a.DisplayOrder,
		CreatedAt:      a.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:      a.UpdatedAt.Time.UTC().Format(time.RFC3339),
		DeletedAt:      tsPtr(a.DeletedAt),
	}
}

func toSyncCategoryResponse(c sqlc.Category) syncCategoryResponse {
	return syncCategoryResponse{
		ID:           c.ID,
		UserID:       nullUUIDToPtr(c.UserID),
		ParentID:     nullUUIDToPtr(c.ParentID),
		Name:         c.Name,
		CategoryType: c.CategoryType,
		Icon:         c.Icon,
		Color:        c.Color,
		IsSystem:     c.IsSystem,
		DisplayOrder: c.DisplayOrder,
		CreatedAt:    c.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:    c.UpdatedAt.Time.UTC().Format(time.RFC3339),
		DeletedAt:    tsPtr(c.DeletedAt),
	}
}

func toSyncBudgetResponse(b sqlc.Budget) syncBudgetResponse {
	return syncBudgetResponse{
		ID:                     b.ID,
		UserID:                 b.UserID,
		TrackingPeriodID:       b.TrackingPeriodID,
		CategoryID:             nullUUIDToPtr(b.CategoryID),
		Amount:                 b.Amount.String(),
		Currency:               b.Currency,
		AlertThresholdWarning:  b.AlertThresholdWarning.String(),
		AlertThresholdCritical: b.AlertThresholdCritical.String(),
		Notes:                  b.Notes,
		CreatedAt:              b.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:              b.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func toSyncGoalResponse(g sqlc.SavingsGoal) syncGoalResponse {
	return syncGoalResponse{
		ID:              g.ID,
		UserID:          g.UserID,
		Name:            g.Name,
		Description:     g.Description,
		Icon:            g.Icon,
		Color:           g.Color,
		TargetAmount:    g.TargetAmount.String(),
		CurrentAmount:   g.CurrentAmount.String(),
		Currency:        g.Currency,
		StartDate:       g.StartDate.Time.Format(dateLayout),
		TargetDate:      g.TargetDate.Time.Format(dateLayout),
		Status:          g.Status,
		LinkedAccountID: nullUUIDToPtr(g.LinkedAccountID),
		AchievedAt:      tsPtr(g.AchievedAt),
		CreatedAt:       g.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       g.UpdatedAt.Time.UTC().Format(time.RFC3339),
		DeletedAt:       tsPtr(g.DeletedAt),
	}
}

func toSyncRecurringResponse(rt sqlc.RecurringTransaction) syncRecurringResponse {
	return syncRecurringResponse{
		ID:                 rt.ID,
		UserID:             rt.UserID,
		AccountID:          rt.AccountID,
		CategoryID:         nullUUIDToPtr(rt.CategoryID),
		Name:               rt.Name,
		TransactionType:    rt.TransactionType,
		Amount:             rt.Amount.String(),
		Currency:           rt.Currency,
		Description:        rt.Description,
		Frequency:          rt.Frequency,
		CustomIntervalDays: rt.CustomIntervalDays,
		DayOfMonth:         rt.DayOfMonth,
		DayOfWeek:          rt.DayOfWeek,
		StartDate:          rt.StartDate.Time.Format(dateLayout),
		EndDate:            datePtr(rt.EndDate),
		LastGeneratedDate:  datePtr(rt.LastGeneratedDate),
		NextDueDate:        datePtr(rt.NextDueDate),
		IsActive:           rt.IsActive,
		CreatedAt:          rt.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:          rt.UpdatedAt.Time.UTC().Format(time.RFC3339),
		DeletedAt:          tsPtr(rt.DeletedAt),
	}
}

func toSyncContributionResponse(c sqlc.SavingsGoalContribution) syncContributionResponse {
	return syncContributionResponse{
		ID:               c.ID,
		UserID:           c.UserID,
		SavingsGoalID:    c.SavingsGoalID,
		TrackingPeriodID: c.TrackingPeriodID,
		TransactionID:    nullUUIDToPtr(c.TransactionID),
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
		UserID:             p.UserID,
		SequenceNumber:     p.SequenceNumber,
		StartDate:          p.StartDate.Time.Format(dateLayout),
		EndDate:            p.EndDate.Time.Format(dateLayout),
		Status:             p.Status,
		ConfigStartDay:     p.ConfigStartDay,
		ConfigDurationDays: p.ConfigDurationDays,
		CreatedAt:          p.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:          p.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if p.ClosedAt.Valid {
		s := p.ClosedAt.Time.UTC().Format(time.RFC3339)
		pr.ClosedAt = &s
	}
	return pr
}
