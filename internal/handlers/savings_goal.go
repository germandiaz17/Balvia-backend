package handlers

import (
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/middleware"
	"github.com/germandiaz17/Balvia-backend/internal/services"
)

// SavingsGoalHandler exposes the savings goal CRUD and contribution endpoints.
type SavingsGoalHandler struct {
	svc      *services.SavingsGoalService
	validate *validator.Validate
}

func NewSavingsGoalHandler(svc *services.SavingsGoalService, v *validator.Validate) *SavingsGoalHandler {
	return &SavingsGoalHandler{svc: svc, validate: v}
}

// Register mounts all routes. The caller must supply an authenticated router.
func (h *SavingsGoalHandler) Register(r fiber.Router) {
	g := r.Group("/savings-goals")
	g.Post("/", h.Create)
	g.Get("/", h.List)
	g.Get("/:id", h.Get)
	g.Put("/:id", h.Update)
	g.Delete("/:id", h.Delete)
	g.Post("/:id/contributions", h.AddContribution)
	g.Get("/:id/contributions", h.ListContributions)
}

// ─── Request / response types ───────────────────────────────────────────────

type goalCreateRequest struct {
	Name            string     `json:"name" validate:"required"`
	Description     *string    `json:"description"`
	Icon            *string    `json:"icon"`
	Color           *string    `json:"color"`
	TargetAmount    string     `json:"target_amount" validate:"required"`
	Currency        string     `json:"currency" validate:"omitempty,len=3"`
	StartDate       string     `json:"start_date" validate:"required"`
	TargetDate      string     `json:"target_date" validate:"required"`
	LinkedAccountID *uuid.UUID `json:"linked_account_id"`
}

type goalUpdateRequest struct {
	Name            string     `json:"name" validate:"required"`
	Description     *string    `json:"description"`
	Icon            *string    `json:"icon"`
	Color           *string    `json:"color"`
	TargetAmount    string     `json:"target_amount" validate:"required"`
	TargetDate      string     `json:"target_date" validate:"required"`
	Status          string     `json:"status" validate:"required"`
	LinkedAccountID *uuid.UUID `json:"linked_account_id"`
}

type contributionRequest struct {
	Amount           string  `json:"amount" validate:"required"`
	ContributionDate *string `json:"contribution_date"`
	Notes            *string `json:"notes"`
}

// goalResponse is the wire representation of a savings goal.
type goalResponse struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Description     *string    `json:"description"`
	Icon            *string    `json:"icon"`
	Color           *string    `json:"color"`
	TargetAmount    string     `json:"target_amount"`
	CurrentAmount   string     `json:"current_amount"`
	Currency        string     `json:"currency"`
	StartDate       string     `json:"start_date"`
	TargetDate      string     `json:"target_date"`
	Status          string     `json:"status"`
	LinkedAccountID *uuid.UUID `json:"linked_account_id"`
	AchievedAt      *string    `json:"achieved_at"`
}

// contributionResponse is the wire representation of a contribution.
type contributionResponse struct {
	ID               uuid.UUID `json:"id"`
	SavingsGoalID    uuid.UUID `json:"savings_goal_id"`
	TrackingPeriodID uuid.UUID `json:"tracking_period_id"`
	Amount           string    `json:"amount"`
	ContributionDate string    `json:"contribution_date"`
	Notes            *string   `json:"notes"`
	CreatedAt        string    `json:"created_at"`
}

// ─── Mapping helpers ─────────────────────────────────────────────────────────

func toGoalResponse(g sqlc.SavingsGoal) goalResponse {
	var linkedAccID *uuid.UUID
	if g.LinkedAccountID.Valid {
		linkedAccID = &g.LinkedAccountID.UUID
	}
	var achievedAt *string
	if g.AchievedAt.Valid {
		s := g.AchievedAt.Time.UTC().Format(time.RFC3339)
		achievedAt = &s
	}
	return goalResponse{
		ID:              g.ID,
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
		LinkedAccountID: linkedAccID,
		AchievedAt:      achievedAt,
	}
}

func toContributionResponse(c sqlc.SavingsGoalContribution) contributionResponse {
	return contributionResponse{
		ID:               c.ID,
		SavingsGoalID:    c.SavingsGoalID,
		TrackingPeriodID: c.TrackingPeriodID,
		Amount:           c.Amount.String(),
		ContributionDate: c.ContributionDate.Time.Format(dateLayout),
		Notes:            c.Notes,
		CreatedAt:        c.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func (h *SavingsGoalHandler) Create(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	var req goalCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	targetAmount, err := decimal.NewFromString(req.TargetAmount)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid target_amount")
	}
	startDate, err := time.Parse(dateLayout, req.StartDate)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid start_date; expected YYYY-MM-DD")
	}
	targetDate, err := time.Parse(dateLayout, req.TargetDate)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid target_date; expected YYYY-MM-DD")
	}

	goal, err := h.svc.Create(c.Context(), userID, services.GoalInput{
		Name:            req.Name,
		Description:     req.Description,
		Icon:            req.Icon,
		Color:           req.Color,
		TargetAmount:    targetAmount,
		Currency:        req.Currency,
		StartDate:       startDate,
		TargetDate:      targetDate,
		LinkedAccountID: req.LinkedAccountID,
	})
	if err != nil {
		return mapDomainError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(toGoalResponse(goal))
}

func (h *SavingsGoalHandler) List(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	goals, err := h.svc.List(c.Context(), userID)
	if err != nil {
		return mapDomainError(err)
	}
	out := make([]goalResponse, 0, len(goals))
	for _, g := range goals {
		out = append(out, toGoalResponse(g))
	}
	return c.JSON(fiber.Map{"savings_goals": out, "count": len(out)})
}

func (h *SavingsGoalHandler) Get(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	goal, err := h.svc.Get(c.Context(), userID, id)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toGoalResponse(goal))
}

func (h *SavingsGoalHandler) Update(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	var req goalUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	// Validate status enum at the handler layer (format concern, not business rule).
	if !services.ValidGoalStatuses[req.Status] {
		return fiber.NewError(fiber.StatusBadRequest,
			fmt.Sprintf("status must be one of: active, achieved, abandoned, paused"))
	}

	targetAmount, err := decimal.NewFromString(req.TargetAmount)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid target_amount")
	}
	targetDate, err := time.Parse(dateLayout, req.TargetDate)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid target_date; expected YYYY-MM-DD")
	}

	goal, err := h.svc.Update(c.Context(), userID, id, services.GoalUpdateInput{
		Name:            req.Name,
		Description:     req.Description,
		Icon:            req.Icon,
		Color:           req.Color,
		TargetAmount:    targetAmount,
		TargetDate:      targetDate,
		Status:          req.Status,
		LinkedAccountID: req.LinkedAccountID,
	})
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toGoalResponse(goal))
}

func (h *SavingsGoalHandler) Delete(c *fiber.Ctx) error {
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

func (h *SavingsGoalHandler) AddContribution(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	var req contributionRequest
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

	var contribDate *time.Time
	if req.ContributionDate != nil {
		d, err := time.Parse(dateLayout, *req.ContributionDate)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid contribution_date; expected YYYY-MM-DD")
		}
		contribDate = &d
	}

	result, err := h.svc.AddContribution(c.Context(), userID, id, services.ContributionInput{
		Amount:           amount,
		ContributionDate: contribDate,
		Notes:            req.Notes,
	})
	if err != nil {
		return mapDomainError(err)
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"goal":         toGoalResponse(result.Goal),
		"contribution": toContributionResponse(result.Contribution),
	})
}

func (h *SavingsGoalHandler) ListContributions(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	contribs, err := h.svc.ListContributions(c.Context(), userID, id)
	if err != nil {
		return mapDomainError(err)
	}

	out := make([]contributionResponse, 0, len(contribs))
	for _, con := range contribs {
		out = append(out, toContributionResponse(con))
	}
	return c.JSON(fiber.Map{"contributions": out, "count": len(out)})
}
