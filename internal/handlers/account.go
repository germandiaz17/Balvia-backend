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

// AccountHandler exposes account CRUD endpoints.
type AccountHandler struct {
	svc      *services.AccountService
	validate *validator.Validate
}

func NewAccountHandler(svc *services.AccountService, v *validator.Validate) *AccountHandler {
	return &AccountHandler{svc: svc, validate: v}
}

func (h *AccountHandler) Register(r fiber.Router) {
	g := r.Group("/accounts")
	g.Post("/", h.Create)
	g.Get("/", h.List)
	g.Get("/:id", h.Get)
	g.Put("/:id", h.Update)
	g.Delete("/:id", h.Delete)
}

type createAccountRequest struct {
	Name           string  `json:"name" validate:"required,max=100"`
	AccountType    string  `json:"account_type" validate:"required,oneof=cash checking savings credit_card investment other"`
	Currency       string  `json:"currency" validate:"omitempty,len=3"`
	InitialBalance string  `json:"initial_balance" validate:"omitempty"`
	Icon           *string `json:"icon" validate:"omitempty,max=50"`
	Color          *string `json:"color" validate:"omitempty,len=7"`
	DisplayOrder   int32   `json:"display_order"`
}

type updateAccountRequest struct {
	Name         string  `json:"name" validate:"required,max=100"`
	AccountType  string  `json:"account_type" validate:"required,oneof=cash checking savings credit_card investment other"`
	Icon         *string `json:"icon" validate:"omitempty,max=50"`
	Color        *string `json:"color" validate:"omitempty,len=7"`
	DisplayOrder int32   `json:"display_order"`
	IsArchived   bool    `json:"is_archived"`
}

func (h *AccountHandler) Create(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	var req createAccountRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	initial := decimal.Zero
	if req.InitialBalance != "" {
		v, err := decimal.NewFromString(req.InitialBalance)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid initial_balance")
		}
		initial = v
	}

	acct, err := h.svc.Create(c.Context(), userID, services.CreateAccountInput{
		Name:           req.Name,
		AccountType:    req.AccountType,
		Currency:       req.Currency,
		InitialBalance: initial,
		Icon:           req.Icon,
		Color:          req.Color,
		DisplayOrder:   req.DisplayOrder,
	})
	if err != nil {
		return mapDomainError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(toAccountResponse(acct))
}

func (h *AccountHandler) List(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	accts, err := h.svc.List(c.Context(), userID)
	if err != nil {
		return mapDomainError(err)
	}
	out := make([]accountResponse, 0, len(accts))
	for _, a := range accts {
		out = append(out, toAccountResponse(a))
	}
	return c.JSON(fiber.Map{"accounts": out, "count": len(out)})
}

func (h *AccountHandler) Get(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	acct, err := h.svc.Get(c.Context(), userID, id)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toAccountResponse(acct))
}

func (h *AccountHandler) Update(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	var req updateAccountRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	acct, err := h.svc.Update(c.Context(), userID, id, services.UpdateAccountInput{
		Name:         req.Name,
		AccountType:  req.AccountType,
		Icon:         req.Icon,
		Color:        req.Color,
		DisplayOrder: req.DisplayOrder,
		IsArchived:   req.IsArchived,
	})
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toAccountResponse(acct))
}

func (h *AccountHandler) Delete(c *fiber.Ctx) error {
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

type accountResponse struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	AccountType    string    `json:"account_type"`
	Currency       string    `json:"currency"`
	InitialBalance string    `json:"initial_balance"`
	CurrentBalance string    `json:"current_balance"`
	Icon           *string   `json:"icon,omitempty"`
	Color          *string   `json:"color,omitempty"`
	IsArchived     bool      `json:"is_archived"`
	DisplayOrder   int32     `json:"display_order"`
}

func toAccountResponse(a sqlc.Account) accountResponse {
	return accountResponse{
		ID:             a.ID,
		Name:           a.Name,
		AccountType:    a.AccountType,
		Currency:       a.Currency,
		InitialBalance: a.InitialBalance.String(),
		CurrentBalance: a.CurrentBalance.String(),
		Icon:           a.Icon,
		Color:          a.Color,
		IsArchived:     a.IsArchived,
		DisplayOrder:   a.DisplayOrder,
	}
}
