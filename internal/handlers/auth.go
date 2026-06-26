package handlers

import (
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/germandiaz17/Balvia-backend/internal/middleware"
	"github.com/germandiaz17/Balvia-backend/internal/services"
)

// AuthHandler exposes registration, login, token refresh and logout.
type AuthHandler struct {
	svc      *services.AuthService
	validate *validator.Validate
}

func NewAuthHandler(svc *services.AuthService, v *validator.Validate) *AuthHandler {
	return &AuthHandler{svc: svc, validate: v}
}

// RegisterPublic mounts the unauthenticated auth routes.
func (h *AuthHandler) RegisterPublic(r fiber.Router) {
	g := r.Group("/auth")
	g.Post("/register", h.Register)
	g.Post("/login", h.Login)
	g.Post("/refresh", h.Refresh)
	g.Post("/logout", h.Logout)
}

// RegisterProtected mounts the authenticated auth routes (needs JWT).
func (h *AuthHandler) RegisterProtected(r fiber.Router) {
	r.Get("/auth/me", h.Me)
}

type registerRequest struct {
	Email    string  `json:"email" validate:"required,email"`
	Password string  `json:"password" validate:"required,min=8,max=72"`
	FullName *string `json:"full_name" validate:"omitempty,max=255"`
}

type loginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type authUserDTO struct {
	ID       uuid.UUID `json:"id"`
	Email    string    `json:"email"`
	FullName *string   `json:"full_name,omitempty"`
}

type authResponse struct {
	User         authUserDTO `json:"user"`
	AccessToken  string      `json:"access_token"`
	ExpiresAt    string      `json:"expires_at"`
	RefreshToken string      `json:"refresh_token"`
	TokenType    string      `json:"token_type"`
}

func (h *AuthHandler) Register(c *fiber.Ctx) error {
	var req registerRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	res, err := h.svc.Register(c.Context(), services.RegisterInput{
		Email:    req.Email,
		Password: req.Password,
		FullName: req.FullName,
	})
	if err != nil {
		return mapDomainError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(toAuthResponse(res))
}

func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req loginRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	res, err := h.svc.Login(c.Context(), req.Email, req.Password)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toAuthResponse(res))
}

func (h *AuthHandler) Refresh(c *fiber.Ctx) error {
	var req refreshRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	tokens, err := h.svc.Refresh(c.Context(), req.RefreshToken)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(fiber.Map{
		"access_token":  tokens.AccessToken,
		"expires_at":    tokens.AccessExpiresAt.Format(time.RFC3339),
		"refresh_token": tokens.RefreshToken,
		"token_type":    "Bearer",
	})
}

func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	var req refreshRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := h.svc.Logout(c.Context(), req.RefreshToken); err != nil {
		return mapDomainError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *AuthHandler) Me(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	user, err := h.svc.Me(c.Context(), userID)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(authUserDTO{ID: user.ID, Email: user.Email, FullName: user.FullName})
}

func toAuthResponse(r services.AuthResult) authResponse {
	return authResponse{
		User:         authUserDTO{ID: r.User.ID, Email: r.User.Email, FullName: r.User.FullName},
		AccessToken:  r.Tokens.AccessToken,
		ExpiresAt:    r.Tokens.AccessExpiresAt.Format(time.RFC3339),
		RefreshToken: r.Tokens.RefreshToken,
		TokenType:    "Bearer",
	}
}
