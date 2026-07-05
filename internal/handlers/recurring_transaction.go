package handlers

import (
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/middleware"
	"github.com/germandiaz17/Balvia-backend/internal/services"
)

// RecurringTransactionHandler exposes the recurring transaction template CRUD
// endpoints. These endpoints manage only the templates (schedule + amount); the
// engine that materialises templates into real transactions is not yet active.
type RecurringTransactionHandler struct {
	svc      *services.RecurringTransactionService
	validate *validator.Validate
}

func NewRecurringTransactionHandler(svc *services.RecurringTransactionService, v *validator.Validate) *RecurringTransactionHandler {
	return &RecurringTransactionHandler{svc: svc, validate: v}
}

// Register mounts all routes. The caller must supply an authenticated router.
func (h *RecurringTransactionHandler) Register(r fiber.Router) {
	g := r.Group("/recurring-transactions")
	g.Post("/", h.Create)
	g.Get("/", h.List)
	g.Get("/:id", h.Get)
	g.Put("/:id", h.Update)
	g.Delete("/:id", h.Delete)
}

// ─── Request / response types ────────────────────────────────────────────────

type recurringRequest struct {
	AccountID          uuid.UUID  `json:"account_id" validate:"required"`
	CategoryID         *uuid.UUID `json:"category_id"`
	Name               string     `json:"name" validate:"required,max=150"`
	TransactionType    string     `json:"transaction_type" validate:"required"`
	Amount             string     `json:"amount" validate:"required"`
	Currency           string     `json:"currency" validate:"omitempty,len=3"`
	Description        *string    `json:"description"`
	Frequency          string     `json:"frequency" validate:"required"`
	CustomIntervalDays *int       `json:"custom_interval_days"`
	DayOfMonth         *int       `json:"day_of_month"`
	DayOfWeek          *int       `json:"day_of_week"`
	StartDate          string     `json:"start_date" validate:"required"`
	EndDate            *string    `json:"end_date"`
	IsActive           *bool      `json:"is_active"`
}

type recurringResponse struct {
	ID                 uuid.UUID  `json:"id"`
	AccountID          uuid.UUID  `json:"account_id"`
	CategoryID         *uuid.UUID `json:"category_id"`
	Name               string     `json:"name"`
	TransactionType    string     `json:"transaction_type"`
	Amount             string     `json:"amount"`
	Currency           string     `json:"currency"`
	Description        *string    `json:"description"`
	Frequency          string     `json:"frequency"`
	CustomIntervalDays *int32     `json:"custom_interval_days"`
	DayOfMonth         *int16     `json:"day_of_month"`
	DayOfWeek          *int16     `json:"day_of_week"`
	StartDate          string     `json:"start_date"`
	EndDate            *string    `json:"end_date"`
	NextDueDate        *string    `json:"next_due_date"`
	IsActive           bool       `json:"is_active"`
}

func toRecurringResponse(rt sqlc.RecurringTransaction) recurringResponse {
	var categoryID *uuid.UUID
	if rt.CategoryID.Valid {
		categoryID = &rt.CategoryID.UUID
	}

	var endDate *string
	if rt.EndDate.Valid {
		s := rt.EndDate.Time.Format(dateLayout)
		endDate = &s
	}

	var nextDueDate *string
	if rt.NextDueDate.Valid {
		s := rt.NextDueDate.Time.Format(dateLayout)
		nextDueDate = &s
	}

	return recurringResponse{
		ID:                 rt.ID,
		AccountID:          rt.AccountID,
		CategoryID:         categoryID,
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
		EndDate:            endDate,
		NextDueDate:        nextDueDate,
		IsActive:           rt.IsActive,
	}
}

// toServiceInput converts a wire request into the service input. The caller is
// responsible for providing the parsed amount and dates.
func (req recurringRequest) toServiceInput(amount decimal.Decimal, startDate time.Time, endDate *time.Time) services.RecurringTransactionInput {
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	return services.RecurringTransactionInput{
		AccountID:          req.AccountID,
		CategoryID:         req.CategoryID,
		Name:               req.Name,
		TransactionType:    req.TransactionType,
		Amount:             amount,
		Currency:           req.Currency,
		Description:        req.Description,
		Frequency:          req.Frequency,
		CustomIntervalDays: req.CustomIntervalDays,
		DayOfMonth:         req.DayOfMonth,
		DayOfWeek:          req.DayOfWeek,
		StartDate:          startDate,
		EndDate:            endDate,
		IsActive:           isActive,
	}
}

// parseRequest parses and validates the common fields shared by Create and Update.
func (h *RecurringTransactionHandler) parseRequest(c *fiber.Ctx) (recurringRequest, decimal.Decimal, time.Time, *time.Time, error) {
	var req recurringRequest
	if err := c.BodyParser(&req); err != nil {
		return req, decimal.Zero, time.Time{}, nil, fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return req, decimal.Zero, time.Time{}, nil, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return req, decimal.Zero, time.Time{}, nil, fiber.NewError(fiber.StatusBadRequest, "invalid amount")
	}
	startDate, err := time.Parse(dateLayout, req.StartDate)
	if err != nil {
		return req, decimal.Zero, time.Time{}, nil, fiber.NewError(fiber.StatusBadRequest, "invalid start_date; expected YYYY-MM-DD")
	}
	var endDate *time.Time
	if req.EndDate != nil {
		d, err := time.Parse(dateLayout, *req.EndDate)
		if err != nil {
			return req, decimal.Zero, time.Time{}, nil, fiber.NewError(fiber.StatusBadRequest, "invalid end_date; expected YYYY-MM-DD")
		}
		endDate = &d
	}
	return req, amount, startDate, endDate, nil
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func (h *RecurringTransactionHandler) Create(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	req, amount, startDate, endDate, err := h.parseRequest(c)
	if err != nil {
		return err
	}
	rt, err := h.svc.Create(c.Context(), userID, req.toServiceInput(amount, startDate, endDate))
	if err != nil {
		return mapDomainError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(toRecurringResponse(rt))
}

func (h *RecurringTransactionHandler) List(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	rts, err := h.svc.List(c.Context(), userID)
	if err != nil {
		return mapDomainError(err)
	}
	out := make([]recurringResponse, 0, len(rts))
	for _, rt := range rts {
		out = append(out, toRecurringResponse(rt))
	}
	return c.JSON(fiber.Map{"recurring_transactions": out, "count": len(out)})
}

func (h *RecurringTransactionHandler) Get(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	rt, err := h.svc.Get(c.Context(), userID, id)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toRecurringResponse(rt))
}

func (h *RecurringTransactionHandler) Update(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	req, amount, startDate, endDate, err := h.parseRequest(c)
	if err != nil {
		return err
	}
	rt, err := h.svc.Update(c.Context(), userID, id, req.toServiceInput(amount, startDate, endDate))
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toRecurringResponse(rt))
}

func (h *RecurringTransactionHandler) Delete(c *fiber.Ctx) error {
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
