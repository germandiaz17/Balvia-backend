package handlers

import (
	"encoding/json"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/middleware"
	"github.com/germandiaz17/Balvia-backend/internal/services"
)

// TrackingPeriodHandler exposes read-only endpoints for tracking periods.
type TrackingPeriodHandler struct {
	svc      *services.PeriodQueryService
	validate *validator.Validate
}

// NewTrackingPeriodHandler builds the handler.
func NewTrackingPeriodHandler(svc *services.PeriodQueryService, v *validator.Validate) *TrackingPeriodHandler {
	return &TrackingPeriodHandler{svc: svc, validate: v}
}

// Register mounts the routes (the caller is responsible for auth middleware).
// Order matters: /active, /:id/summary and /:id/insights must be registered
// before /:id to prevent Fiber's wildcard from swallowing those named paths.
func (h *TrackingPeriodHandler) Register(r fiber.Router) {
	g := r.Group("/tracking-periods")
	g.Get("/", h.List)
	g.Get("/active", h.GetActive)
	g.Get("/:id/summary", h.Summary)
	g.Get("/:id/insights", h.Insights)
	g.Get("/:id", h.Get)
}

// periodResponse is the JSON shape of a tracking_period.
type periodResponse struct {
	ID                 uuid.UUID `json:"id"`
	SequenceNumber     int32     `json:"sequence_number"`
	StartDate          string    `json:"start_date"`
	EndDate            string    `json:"end_date"`
	Status             string    `json:"status"`
	ConfigStartDay     int16     `json:"config_start_day"`
	ConfigDurationDays int16     `json:"config_duration_days"`
	ConfigPeriodMode   string    `json:"config_period_mode"`
	// IsTransition marks a one-off bridge created when the user switched period
	// mode. Its length is deliberately outside the usual 28-31 days, so the
	// client should present it as a transition rather than a normal period.
	IsTransition bool    `json:"is_transition"`
	ClosedAt     *string `json:"closed_at"`
}

// toPeriodResponse maps a sqlc.TrackingPeriod to the wire response.
func toPeriodResponse(p sqlc.TrackingPeriod) periodResponse {
	var closedAt *string
	if p.ClosedAt.Valid {
		s := p.ClosedAt.Time.UTC().Format(time.RFC3339)
		closedAt = &s
	}
	return periodResponse{
		ID:                 p.ID,
		SequenceNumber:     p.SequenceNumber,
		StartDate:          p.StartDate.Time.Format(dateLayout),
		EndDate:            p.EndDate.Time.Format(dateLayout),
		Status:             p.Status,
		ConfigStartDay:     p.ConfigStartDay,
		ConfigDurationDays: p.ConfigDurationDays,
		ConfigPeriodMode:   p.ConfigPeriodMode,
		IsTransition:       p.IsTransition,
		ClosedAt:           closedAt,
	}
}

// subPeriodResponse is the JSON shape of a single sub-block in a biweekly or
// weekly summary breakdown.
type subPeriodResponse struct {
	Index                   int    `json:"index"`
	From                    string `json:"from"`
	To                      string `json:"to"`
	TotalIncome             string `json:"total_income"`
	TotalExpenses           string `json:"total_expenses"`
	TotalTransfers          string `json:"total_transfers"`
	NetSavings              string `json:"net_savings"`
	SavingsRate             string `json:"savings_rate"`
	TransactionCount        int32  `json:"transaction_count"`
	ExpenseTransactionCount int32  `json:"expense_transaction_count"`
	IncomeTransactionCount  int32  `json:"income_transaction_count"`
}

// summaryResponse is the JSON shape returned by GET /tracking-periods/:id/summary.
// For closed periods the JSONB breakdown fields (expense_by_category, etc.) are
// populated from the stored snapshot; for active periods they are omitted (null).
type summaryResponse struct {
	PeriodID                string     `json:"period_id"`
	View                    string     `json:"view"`
	TotalIncome             string     `json:"total_income"`
	TotalExpenses           string     `json:"total_expenses"`
	TotalTransfers          string     `json:"total_transfers"`
	NetSavings              string     `json:"net_savings"`
	SavingsRate             string     `json:"savings_rate"`
	TransactionCount        int32      `json:"transaction_count"`
	ExpenseTransactionCount int32      `json:"expense_transaction_count"`
	IncomeTransactionCount  int32      `json:"income_transaction_count"`
	TopExpenseCategoryID    *uuid.UUID `json:"top_expense_category_id"`
	TopExpenseCategoryTotal *string    `json:"top_expense_category_total"`
	// JSONB breakdowns — only present for closed periods (stored snapshots).
	ExpenseByCategory json.RawMessage     `json:"expense_by_category,omitempty"`
	IncomeByCategory  json.RawMessage     `json:"income_by_category,omitempty"`
	ExpenseByAccount  json.RawMessage     `json:"expense_by_account,omitempty"`
	ExpenseByDay      json.RawMessage     `json:"expense_by_day,omitempty"`
	BudgetPerformance json.RawMessage     `json:"budget_performance,omitempty"`
	VsPreviousPeriod  json.RawMessage     `json:"vs_previous_period,omitempty"`
	SubPeriods        []subPeriodResponse `json:"sub_periods,omitempty"`
}

// insightResponse is the JSON shape of a single tracking_period_insights row.
type insightResponse struct {
	ID                uuid.UUID       `json:"id"`
	InsightType       string          `json:"insight_type"`
	CalculationPhase  string          `json:"calculation_phase"`
	Severity          string          `json:"severity"`
	Title             string          `json:"title"`
	Message           string          `json:"message"`
	ActionLabel       *string         `json:"action_label"`
	ActionTarget      *string         `json:"action_target"`
	Data              json.RawMessage `json:"data"`
	RelatedCategoryID *uuid.UUID      `json:"related_category_id"`
	RelatedAccountID  *uuid.UUID      `json:"related_account_id"`
	RelatedGoalID     *uuid.UUID      `json:"related_goal_id"`
	CreatedAt         string          `json:"created_at"`
}

func toInsightResponse(i sqlc.TrackingPeriodInsight) insightResponse {
	r := insightResponse{
		ID:               i.ID,
		InsightType:      i.InsightType,
		CalculationPhase: i.CalculationPhase,
		Severity:         i.Severity,
		Title:            i.Title,
		Message:          i.Message,
		ActionLabel:      i.ActionLabel,
		ActionTarget:     i.ActionTarget,
		Data:             json.RawMessage(i.Data),
		CreatedAt:        i.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
	if i.RelatedCategoryID.Valid {
		uid := i.RelatedCategoryID.UUID
		r.RelatedCategoryID = &uid
	}
	if i.RelatedAccountID.Valid {
		uid := i.RelatedAccountID.UUID
		r.RelatedAccountID = &uid
	}
	if i.RelatedGoalID.Valid {
		uid := i.RelatedGoalID.UUID
		r.RelatedGoalID = &uid
	}
	return r
}

// toSummaryResponse maps a services.PeriodSummary to the wire response.
// snapshot, if non-nil, provides the JSONB breakdown fields from the stored
// summary row (only available for closed periods).
func toSummaryResponse(s services.PeriodSummary, snapshot *sqlc.TrackingPeriodSummary) summaryResponse {
	var topTotal *string
	if s.TopExpenseCategoryTotal != nil {
		t := s.TopExpenseCategoryTotal.String()
		topTotal = &t
	}

	var subs []subPeriodResponse
	for _, sp := range s.SubPeriods {
		subs = append(subs, subPeriodResponse{
			Index:                   sp.Index,
			From:                    sp.From.Format(dateLayout),
			To:                      sp.To.Format(dateLayout),
			TotalIncome:             sp.TotalIncome.String(),
			TotalExpenses:           sp.TotalExpenses.String(),
			TotalTransfers:          sp.TotalTransfers.String(),
			NetSavings:              sp.NetSavings.String(),
			SavingsRate:             sp.SavingsRate.String(),
			TransactionCount:        sp.TransactionCount,
			ExpenseTransactionCount: sp.ExpenseTransactionCount,
			IncomeTransactionCount:  sp.IncomeTransactionCount,
		})
	}

	resp := summaryResponse{
		PeriodID:                s.PeriodID.String(),
		View:                    s.View,
		TotalIncome:             s.TotalIncome.String(),
		TotalExpenses:           s.TotalExpenses.String(),
		TotalTransfers:          s.TotalTransfers.String(),
		NetSavings:              s.NetSavings.String(),
		SavingsRate:             s.SavingsRate.String(),
		TransactionCount:        s.TransactionCount,
		ExpenseTransactionCount: s.ExpenseTransactionCount,
		IncomeTransactionCount:  s.IncomeTransactionCount,
		TopExpenseCategoryID:    s.TopExpenseCategoryID,
		TopExpenseCategoryTotal: topTotal,
		SubPeriods:              subs,
	}

	// Attach JSONB breakdowns from snapshot for closed periods.
	if snapshot != nil {
		if isNonEmptyJSON(snapshot.ExpenseByCategory) {
			resp.ExpenseByCategory = json.RawMessage(snapshot.ExpenseByCategory)
		}
		if isNonEmptyJSON(snapshot.IncomeByCategory) {
			resp.IncomeByCategory = json.RawMessage(snapshot.IncomeByCategory)
		}
		if isNonEmptyJSON(snapshot.ExpenseByAccount) {
			resp.ExpenseByAccount = json.RawMessage(snapshot.ExpenseByAccount)
		}
		if isNonEmptyJSON(snapshot.ExpenseByDay) {
			resp.ExpenseByDay = json.RawMessage(snapshot.ExpenseByDay)
		}
		if isNonEmptyJSON(snapshot.BudgetPerformance) {
			resp.BudgetPerformance = json.RawMessage(snapshot.BudgetPerformance)
		}
		if isNonEmptyJSON(snapshot.VsPreviousPeriod) {
			resp.VsPreviousPeriod = json.RawMessage(snapshot.VsPreviousPeriod)
		}
	}

	return resp
}

// isNonEmptyJSON returns true if b is non-nil, non-empty and not a bare null/[].
func isNonEmptyJSON(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	s := string(b)
	return s != "null" && s != "[]" && s != "{}"
}

// List handles GET /tracking-periods.
func (h *TrackingPeriodHandler) List(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	periods, err := h.svc.List(c.Context(), userID)
	if err != nil {
		return mapDomainError(err)
	}
	out := make([]periodResponse, 0, len(periods))
	for _, p := range periods {
		out = append(out, toPeriodResponse(p))
	}
	return c.JSON(fiber.Map{"tracking_periods": out, "count": len(out)})
}

// GetActive handles GET /tracking-periods/active.
func (h *TrackingPeriodHandler) GetActive(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	period, err := h.svc.GetActive(c.Context(), userID)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toPeriodResponse(period))
}

// Get handles GET /tracking-periods/:id.
func (h *TrackingPeriodHandler) Get(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	period, err := h.svc.Get(c.Context(), userID, id)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toPeriodResponse(period))
}

// Summary handles GET /tracking-periods/:id/summary?view=full|biweekly|weekly.
// For closed periods, the response includes the stored JSONB breakdown fields.
func (h *TrackingPeriodHandler) Summary(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	view := c.Query("view", services.ViewFull)
	summary, snapshot, err := h.svc.SummaryWithSnapshot(c.Context(), userID, id, view)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toSummaryResponse(summary, snapshot))
}

// Insights handles GET /tracking-periods/:id/insights.
// The response is polymorphic by period status (decided in the service):
//   - active period → lazily recomputes and returns the "during" insights
//     (spending_pace, budget alerts, etc.).
//   - closed period → returns the immutable "final" insights generated at close.
//
// Returns an empty array when no insights have been generated yet.
func (h *TrackingPeriodHandler) Insights(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	insights, err := h.svc.GetInsights(c.Context(), userID, id)
	if err != nil {
		return mapDomainError(err)
	}
	out := make([]insightResponse, 0, len(insights))
	for _, ins := range insights {
		out = append(out, toInsightResponse(ins))
	}
	return c.JSON(fiber.Map{"insights": out, "count": len(out)})
}
