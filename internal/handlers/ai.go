package handlers

import (
	"errors"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/germandiaz17/Balvia-backend/internal/ai"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
	"github.com/germandiaz17/Balvia-backend/internal/middleware"
)

// AIHandler exposes AI-backed endpoints: per-user BYOK provider settings plus
// expense auto-categorization.
type AIHandler struct {
	svc         *ai.Service
	settingsSvc *ai.SettingsService
	validate    *validator.Validate
}

func NewAIHandler(svc *ai.Service, settingsSvc *ai.SettingsService, v *validator.Validate) *AIHandler {
	return &AIHandler{svc: svc, settingsSvc: settingsSvc, validate: v}
}

func (h *AIHandler) Register(r fiber.Router) {
	g := r.Group("/ai")
	g.Put("/settings", h.SetSettings)
	g.Get("/settings", h.GetSettings)
	g.Delete("/settings", h.DeleteSettings)
	g.Post("/categorize", h.Categorize)
}

// --- Settings ---------------------------------------------------------------

type setAISettingsRequest struct {
	Provider string  `json:"provider" validate:"required,oneof=anthropic openai_compatible"`
	APIKey   string  `json:"api_key" validate:"required,max=400"`
	BaseURL  *string `json:"base_url" validate:"omitempty,url,max=300"`
	Model    *string `json:"model" validate:"omitempty,max=100"`
}

// aiSettingsResponse never includes the API key (write-only).
type aiSettingsResponse struct {
	Configured bool    `json:"configured"`
	Provider   string  `json:"provider,omitempty"`
	BaseURL    *string `json:"base_url,omitempty"`
	Model      *string `json:"model,omitempty"`
	Enabled    bool    `json:"enabled,omitempty"`
	HasKey     bool    `json:"has_key,omitempty"`
}

func toAISettingsResponse(s sqlc.UserAiSetting) aiSettingsResponse {
	return aiSettingsResponse{
		Configured: true,
		Provider:   s.Provider,
		BaseURL:    s.BaseUrl,
		Model:      s.Model,
		Enabled:    s.Enabled,
		HasKey:     len(s.ApiKeyEncrypted) > 0,
	}
}

func (h *AIHandler) SetSettings(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	var req setAISettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	row, err := h.settingsSvc.Set(c.Context(), userID, ai.SetInput{
		Provider: req.Provider,
		APIKey:   req.APIKey,
		BaseURL:  req.BaseURL,
		Model:    req.Model,
	})
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toAISettingsResponse(row))
}

func (h *AIHandler) GetSettings(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	row, err := h.settingsSvc.Get(c.Context(), userID)
	if errors.Is(err, domain.ErrNotFound) {
		return c.JSON(aiSettingsResponse{Configured: false})
	}
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toAISettingsResponse(row))
}

func (h *AIHandler) DeleteSettings(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	if err := h.settingsSvc.Delete(c.Context(), userID); err != nil {
		return mapDomainError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// --- Categorize -------------------------------------------------------------

type categorizeRequest struct {
	Description     string `json:"description" validate:"required,max=255"`
	Amount          string `json:"amount" validate:"omitempty"`
	Merchant        string `json:"merchant" validate:"omitempty,max=255"`
	TransactionType string `json:"transaction_type" validate:"omitempty,oneof=income expense transfer"`
}

// categorizeResponse carries the suggested category. CategoryID is null when the
// model couldn't confidently match one of the user's categories.
type categorizeResponse struct {
	CategoryID *uuid.UUID `json:"category_id"`
	Confidence float64    `json:"confidence"`
}

func (h *AIHandler) Categorize(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	var req categorizeRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	sug, err := h.svc.Categorize(c.Context(), userID, ai.CategorizeInput{
		Description:     req.Description,
		Amount:          req.Amount,
		Merchant:        req.Merchant,
		TransactionType: req.TransactionType,
	})
	if err != nil {
		return mapDomainError(err)
	}

	resp := categorizeResponse{Confidence: sug.Confidence}
	if sug.CategoryID != uuid.Nil {
		id := sug.CategoryID
		resp.CategoryID = &id
	}
	return c.JSON(resp)
}
