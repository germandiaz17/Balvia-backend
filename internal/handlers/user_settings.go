package handlers

import (
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"

	"github.com/germandiaz17/Balvia-backend/internal/middleware"
	"github.com/germandiaz17/Balvia-backend/internal/services"
)

// UserSettingsHandler exposes the authenticated user's preferences as a
// singleton resource.
type UserSettingsHandler struct {
	svc      *services.UserSettingsService
	validate *validator.Validate
}

func NewUserSettingsHandler(svc *services.UserSettingsService, v *validator.Validate) *UserSettingsHandler {
	return &UserSettingsHandler{svc: svc, validate: v}
}

func (h *UserSettingsHandler) Register(r fiber.Router) {
	g := r.Group("/settings")
	g.Get("/", h.Get)
	g.Put("/", h.Update)
}

// settingsResponse never exposes anything the client may not change except for
// context: country_code and subscription_tier are read-only.
type settingsResponse struct {
	TrackingStartDay     int16  `json:"tracking_start_day"`
	TrackingDurationDays int16  `json:"tracking_duration_days"`
	DefaultCurrency      string `json:"default_currency"`
	CountryCode          string `json:"country_code"`
	Locale               string `json:"locale"`
	Theme                string `json:"theme"`
	DefaultPeriodView    string `json:"default_period_view"`
	SubscriptionTier     string `json:"subscription_tier"`
	UpdatedAt            string `json:"updated_at"`

	// AppliesToNextPeriod is always true: per domain rule 8 a change to the
	// tracking configuration never reshapes the active period. Sent explicitly so
	// the client does not have to hardcode the rule.
	AppliesToNextPeriod bool `json:"applies_to_next_period"`
	// ActivePeriodEndDate is the last day of the period currently running
	// (YYYY-MM-DD), or null when the user has no active period. The app uses it to
	// say exactly when a change takes effect.
	ActivePeriodEndDate *string `json:"active_period_end_date"`
}

// updateSettingsRequest is a partial update: omitted fields are left alone.
// The tags catch malformed input as a 400; the service re-checks the same rules
// and returns 422, so a CHECK violation never reaches the client as a 500.
type updateSettingsRequest struct {
	TrackingStartDay     *int16  `json:"tracking_start_day" validate:"omitempty,min=1,max=31"`
	TrackingDurationDays *int16  `json:"tracking_duration_days" validate:"omitempty,min=28,max=31"`
	DefaultCurrency      *string `json:"default_currency" validate:"omitempty,len=3,uppercase"`
	Locale               *string `json:"locale" validate:"omitempty,min=2,max=10"`
	Theme                *string `json:"theme" validate:"omitempty,oneof=system light dark"`
	DefaultPeriodView    *string `json:"default_period_view" validate:"omitempty,oneof=full biweekly weekly"`
}

func toSettingsResponse(v services.SettingsView) settingsResponse {
	s := v.Settings
	resp := settingsResponse{
		TrackingStartDay:     s.TrackingStartDay,
		TrackingDurationDays: s.TrackingDurationDays,
		DefaultCurrency:      s.DefaultCurrency,
		CountryCode:          s.CountryCode,
		Locale:               s.Locale,
		Theme:                s.Theme,
		DefaultPeriodView:    s.DefaultPeriodView,
		SubscriptionTier:     s.SubscriptionTier,
		AppliesToNextPeriod:  true,
	}
	if s.UpdatedAt.Valid {
		resp.UpdatedAt = s.UpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	if v.ActivePeriodEnd != nil {
		d := v.ActivePeriodEnd.Format(dateLayout)
		resp.ActivePeriodEndDate = &d
	}
	return resp
}

// Get handles GET /settings.
func (h *UserSettingsHandler) Get(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}
	view, err := h.svc.Get(c.Context(), userID)
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toSettingsResponse(view))
}

// Update handles PUT /settings. It is a partial update: sending only
// tracking_duration_days leaves every other preference untouched.
func (h *UserSettingsHandler) Update(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
	}

	var req updateSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	view, err := h.svc.Update(c.Context(), userID, services.SettingsUpdateInput{
		TrackingStartDay:     req.TrackingStartDay,
		TrackingDurationDays: req.TrackingDurationDays,
		DefaultCurrency:      req.DefaultCurrency,
		Locale:               req.Locale,
		Theme:                req.Theme,
		DefaultPeriodView:    req.DefaultPeriodView,
	})
	if err != nil {
		return mapDomainError(err)
	}
	return c.JSON(toSettingsResponse(view))
}
