package handlers

import (
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
// Order matters: /active and /:id/summary must be registered before /:id to
// prevent Fiber's wildcard from swallowing those named paths.
func (h *TrackingPeriodHandler) Register(r fiber.Router) {
	g := r.Group("/tracking-periods")
	g.Get("/", h.List)
	g.Get("/active", h.GetActive)
	g.Get("/:id/summary", h.Summary)
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
	ClosedAt           *string   `json:"closed_at"`
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
type summaryResponse struct {
	PeriodID                string              `json:"period_id"`
	View                    string              `json:"view"`
	TotalIncome             string              `json:"total_income"`
	TotalExpenses           string              `json:"total_expenses"`
	TotalTransfers          string              `json:"total_transfers"`
	NetSavings              string              `json:"net_savings"`
	SavingsRate             string              `json:"savings_rate"`
	TransactionCount        int32               `json:"transaction_count"`
	ExpenseTransactionCount int32               `json:"expense_transaction_count"`
	IncomeTransactionCount  int32               `json:"income_transaction_count"`
	TopExpenseCategoryID    *uuid.UUID          `json:"top_expense_category_id"`
	TopExpenseCategoryTotal *string             `json:"top_expense_category_total"`
	SubPeriods              []subPeriodResponse `json:"sub_periods,omitempty"`
}

// toSummaryResponse maps a services.PeriodSummary to the wire response.
func toSummaryResponse(s services.PeriodSummary) summaryResponse {
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

	return summaryResponse{
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
	summary, err := h.svc.Summary(c.Context(), userID, id, view)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toSummaryResponse(summary))
}
