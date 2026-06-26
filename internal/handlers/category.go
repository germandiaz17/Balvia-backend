package handlers

import (
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/middleware"
	"github.com/germandiaz17/Balvia-backend/internal/services"
)

// CategoryHandler exposes category endpoints (system categories are read-only).
type CategoryHandler struct {
	svc      *services.CategoryService
	validate *validator.Validate
}

func NewCategoryHandler(svc *services.CategoryService, v *validator.Validate) *CategoryHandler {
	return &CategoryHandler{svc: svc, validate: v}
}

func (h *CategoryHandler) Register(r fiber.Router) {
	g := r.Group("/categories")
	g.Post("/", h.Create)
	g.Get("/", h.List)
	g.Get("/:id", h.Get)
	g.Put("/:id", h.Update)
	g.Delete("/:id", h.Delete)
}

type createCategoryRequest struct {
	Name         string     `json:"name" validate:"required,max=100"`
	CategoryType string     `json:"category_type" validate:"required,oneof=income expense transfer"`
	ParentID     *uuid.UUID `json:"parent_id"`
	Icon         *string    `json:"icon" validate:"omitempty,max=50"`
	Color        *string    `json:"color" validate:"omitempty,len=7"`
	DisplayOrder int32      `json:"display_order"`
}

type updateCategoryRequest struct {
	Name         string     `json:"name" validate:"required,max=100"`
	ParentID     *uuid.UUID `json:"parent_id"`
	Icon         *string    `json:"icon" validate:"omitempty,max=50"`
	Color        *string    `json:"color" validate:"omitempty,len=7"`
	DisplayOrder int32      `json:"display_order"`
}

func (h *CategoryHandler) Create(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	var req createCategoryRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	cat, err := h.svc.Create(c.Context(), userID, services.CreateCategoryInput{
		Name:         req.Name,
		CategoryType: req.CategoryType,
		ParentID:     req.ParentID,
		Icon:         req.Icon,
		Color:        req.Color,
		DisplayOrder: req.DisplayOrder,
	})
	if err != nil {
		return mapDomainError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(toCategoryResponse(cat))
}

func (h *CategoryHandler) List(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	cats, err := h.svc.List(c.Context(), userID)
	if err != nil {
		return mapDomainError(err)
	}
	out := make([]categoryResponse, 0, len(cats))
	for _, cat := range cats {
		out = append(out, toCategoryResponse(cat))
	}
	return c.JSON(fiber.Map{"categories": out, "count": len(out)})
}

func (h *CategoryHandler) Get(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	cat, err := h.svc.Get(c.Context(), userID, id)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toCategoryResponse(cat))
}

func (h *CategoryHandler) Update(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	var req updateCategoryRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	cat, err := h.svc.Update(c.Context(), userID, id, services.UpdateCategoryInput{
		Name:         req.Name,
		ParentID:     req.ParentID,
		Icon:         req.Icon,
		Color:        req.Color,
		DisplayOrder: req.DisplayOrder,
	})
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toCategoryResponse(cat))
}

func (h *CategoryHandler) Delete(c *fiber.Ctx) error {
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

type categoryResponse struct {
	ID           uuid.UUID  `json:"id"`
	ParentID     *uuid.UUID `json:"parent_id,omitempty"`
	Name         string     `json:"name"`
	CategoryType string     `json:"category_type"`
	Icon         *string    `json:"icon,omitempty"`
	Color        *string    `json:"color,omitempty"`
	IsSystem     bool       `json:"is_system"`
	DisplayOrder int32      `json:"display_order"`
}

func toCategoryResponse(cat sqlc.Category) categoryResponse {
	return categoryResponse{
		ID:           cat.ID,
		ParentID:     nullUUIDToPtr(cat.ParentID),
		Name:         cat.Name,
		CategoryType: cat.CategoryType,
		Icon:         cat.Icon,
		Color:        cat.Color,
		IsSystem:     cat.IsSystem,
		DisplayOrder: cat.DisplayOrder,
	}
}
