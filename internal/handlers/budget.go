package handlers

import (
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/middleware"
	"github.com/germandiaz17/Balvia-backend/internal/services"
)

// BudgetHandler exposes the budget CRUD endpoints. Budgets are anchored to the
// user's active tracking period.
type BudgetHandler struct {
	svc      *services.BudgetService
	validate *validator.Validate
}

func NewBudgetHandler(svc *services.BudgetService, v *validator.Validate) *BudgetHandler {
	return &BudgetHandler{svc: svc, validate: v}
}

// Register mounts the routes (the caller is responsible for auth middleware).
func (h *BudgetHandler) Register(r fiber.Router) {
	g := r.Group("/budgets")
	g.Post("/", h.Create)
	g.Get("/", h.List)
	g.Get("/:id", h.Get)
	g.Put("/:id", h.Update)
	g.Delete("/:id", h.Delete)
}

type budgetRequest struct {
	CategoryID        *uuid.UUID `json:"category_id"`
	Amount            string     `json:"amount" validate:"required"`
	Currency          string     `json:"currency" validate:"omitempty,len=3"`
	WarningThreshold  *string    `json:"alert_threshold_warning" validate:"omitempty"`
	CriticalThreshold *string    `json:"alert_threshold_critical" validate:"omitempty"`
	Notes             *string    `json:"notes"`
}

// toInput parses the wire request into the service input. It returns a *fiber.Error
// for malformed decimals.
func (r budgetRequest) toInput() (services.BudgetInput, error) {
	amount, err := decimal.NewFromString(r.Amount)
	if err != nil {
		return services.BudgetInput{}, fiber.NewError(fiber.StatusBadRequest, "invalid amount")
	}
	warning, err := parseOptionalDecimal(r.WarningThreshold, "alert_threshold_warning")
	if err != nil {
		return services.BudgetInput{}, err
	}
	critical, err := parseOptionalDecimal(r.CriticalThreshold, "alert_threshold_critical")
	if err != nil {
		return services.BudgetInput{}, err
	}
	return services.BudgetInput{
		CategoryID:        r.CategoryID,
		Amount:            amount,
		Currency:          r.Currency,
		WarningThreshold:  warning,
		CriticalThreshold: critical,
		Notes:             r.Notes,
	}, nil
}

func (h *BudgetHandler) Create(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	var req budgetRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	in, err := req.toInput()
	if err != nil {
		return err
	}
	budget, err := h.svc.Create(c.Context(), userID, in)
	if err != nil {
		return mapDomainError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(toBudgetResponse(budget))
}

func (h *BudgetHandler) List(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	var periodID *uuid.UUID
	if raw := c.Query("tracking_period_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid tracking_period_id")
		}
		periodID = &id
	}
	budgets, err := h.svc.List(c.Context(), userID, periodID)
	if err != nil {
		return mapDomainError(err)
	}
	out := make([]budgetResponse, 0, len(budgets))
	for _, b := range budgets {
		out = append(out, toBudgetResponse(b))
	}
	return c.JSON(fiber.Map{"budgets": out, "count": len(out)})
}

func (h *BudgetHandler) Get(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	budget, err := h.svc.Get(c.Context(), userID, id)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toBudgetResponse(budget))
}

func (h *BudgetHandler) Update(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	var req budgetRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	in, err := req.toInput()
	if err != nil {
		return err
	}
	budget, err := h.svc.Update(c.Context(), userID, id, in)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toBudgetResponse(budget))
}

func (h *BudgetHandler) Delete(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	if err := h.svc.Delete(c.Context(), userID, id); err != nil {
		return mapDomainError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// parseOptionalDecimal parses a pointer-to-string decimal, returning nil when
// the field is absent and a 400 error when it is malformed.
func parseOptionalDecimal(s *string, field string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	v, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid "+field)
	}
	return &v, nil
}

type budgetResponse struct {
	ID                     uuid.UUID  `json:"id"`
	TrackingPeriodID       uuid.UUID  `json:"tracking_period_id"`
	CategoryID             *uuid.UUID `json:"category_id"`
	Amount                 string     `json:"amount"`
	Currency               string     `json:"currency"`
	AlertThresholdWarning  string     `json:"alert_threshold_warning"`
	AlertThresholdCritical string     `json:"alert_threshold_critical"`
	Notes                  *string    `json:"notes"`
}

func toBudgetResponse(b sqlc.Budget) budgetResponse {
	var categoryID *uuid.UUID
	if b.CategoryID.Valid {
		categoryID = &b.CategoryID.UUID
	}
	return budgetResponse{
		ID:                     b.ID,
		TrackingPeriodID:       b.TrackingPeriodID,
		CategoryID:             categoryID,
		Amount:                 b.Amount.String(),
		Currency:               b.Currency,
		AlertThresholdWarning:  b.AlertThresholdWarning.String(),
		AlertThresholdCritical: b.AlertThresholdCritical.String(),
		Notes:                  b.Notes,
	}
}
