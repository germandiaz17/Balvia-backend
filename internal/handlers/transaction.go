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

// TransactionHandler exposes the transaction CRUD endpoints. It assumes an auth
// middleware has set the user id on the context.
type TransactionHandler struct {
	svc      *services.TransactionService
	validate *validator.Validate
}

// NewTransactionHandler wires the handler with its service and validator.
func NewTransactionHandler(svc *services.TransactionService, v *validator.Validate) *TransactionHandler {
	return &TransactionHandler{svc: svc, validate: v}
}

// Register mounts the routes (the caller is responsible for auth middleware).
func (h *TransactionHandler) Register(r fiber.Router) {
	g := r.Group("/transactions")
	g.Post("/", h.Create)
	g.Get("/", h.List)
	g.Get("/:id", h.Get)
	g.Put("/:id", h.Update)
	g.Delete("/:id", h.Delete)
}

type createTxnRequest struct {
	AccountID         uuid.UUID  `json:"account_id" validate:"required"`
	TransactionType   string     `json:"transaction_type" validate:"required,oneof=income expense transfer"`
	Amount            string     `json:"amount" validate:"required"`
	Currency          string     `json:"currency" validate:"omitempty,len=3"`
	CategoryID        *uuid.UUID `json:"category_id"`
	Description       *string    `json:"description" validate:"omitempty,max=255"`
	Notes             *string    `json:"notes"`
	TransactionDate   *string    `json:"transaction_date" validate:"omitempty,datetime=2006-01-02"`
	TransferAccountID *uuid.UUID `json:"transfer_account_id"`
	ClientID          *string    `json:"client_id" validate:"omitempty,max=100"`
}

// Create handles POST /transactions.
func (h *TransactionHandler) Create(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}

	var req createTxnRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid amount")
	}

	var txnDate *time.Time
	if req.TransactionDate != nil {
		d, err := time.Parse(dateLayout, *req.TransactionDate)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid transaction_date")
		}
		txnDate = &d
	}

	txn, err := h.svc.Create(c.Context(), userID, services.CreateTransactionInput{
		AccountID:         req.AccountID,
		TransactionType:   req.TransactionType,
		Amount:            amount,
		Currency:          req.Currency,
		CategoryID:        req.CategoryID,
		Description:       req.Description,
		Notes:             req.Notes,
		TransactionDate:   txnDate,
		TransferAccountID: req.TransferAccountID,
		ClientID:          req.ClientID,
	})
	if err != nil {
		return mapDomainError(err)
	}

	return c.Status(fiber.StatusCreated).JSON(toTransactionResponse(txn))
}

// Update handles PUT /transactions/:id.
func (h *TransactionHandler) Update(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	var req createTxnRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid amount")
	}

	var txnDate *time.Time
	if req.TransactionDate != nil {
		d, err := time.Parse(dateLayout, *req.TransactionDate)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid transaction_date")
		}
		txnDate = &d
	}

	txn, err := h.svc.Update(c.Context(), userID, id, services.CreateTransactionInput{
		AccountID:         req.AccountID,
		TransactionType:   req.TransactionType,
		Amount:            amount,
		Currency:          req.Currency,
		CategoryID:        req.CategoryID,
		Description:       req.Description,
		Notes:             req.Notes,
		TransactionDate:   txnDate,
		TransferAccountID: req.TransferAccountID,
		ClientID:          req.ClientID,
	})
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toTransactionResponse(txn))
}

// Get handles GET /transactions/:id.
func (h *TransactionHandler) Get(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	txn, err := h.svc.Get(c.Context(), userID, id)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toTransactionResponse(txn))
}

// List handles GET /transactions[?tracking_period_id=...].
func (h *TransactionHandler) List(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}

	var periodID *uuid.UUID
	if raw := c.Query("tracking_period_id"); raw != "" {
		pid, err := uuid.Parse(raw)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid tracking_period_id")
		}
		periodID = &pid
	}

	txns, err := h.svc.List(c.Context(), userID, periodID)
	if err != nil {
		return mapDomainError(err)
	}

	out := make([]transactionResponse, 0, len(txns))
	for _, t := range txns {
		out = append(out, toTransactionResponse(t))
	}
	return c.JSON(fiber.Map{"transactions": out, "count": len(out)})
}

// Delete handles DELETE /transactions/:id.
func (h *TransactionHandler) Delete(c *fiber.Ctx) error {
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

type transactionResponse struct {
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
}

func toTransactionResponse(t sqlc.Transaction) transactionResponse {
	return transactionResponse{
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
		CreatedAt:         t.CreatedAt.Time.Format(time.RFC3339),
	}
}

func nullUUIDToPtr(n uuid.NullUUID) *uuid.UUID {
	if !n.Valid {
		return nil
	}
	id := n.UUID
	return &id
}
