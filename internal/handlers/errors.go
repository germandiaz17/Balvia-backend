package handlers

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// dateLayout is the wire format for date-only fields (YYYY-MM-DD).
const dateLayout = "2006-01-02"

// mapDomainError translates a domain error into an HTTP error. Unknown errors
// are returned as-is so the global ErrorHandler turns them into a 500.
func mapDomainError(err error) error {
	switch {
	case errors.Is(err, domain.ErrEmailAlreadyExists),
		errors.Is(err, domain.ErrBudgetExists):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrNotFound),
		errors.Is(err, domain.ErrAccountNotFound),
		errors.Is(err, domain.ErrCategoryNotFound),
		errors.Is(err, domain.ErrGoalNotFound):
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrInvalidToken):
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	case errors.Is(err, domain.ErrNoActivePeriod),
		errors.Is(err, domain.ErrInvalidTransfer),
		errors.Is(err, domain.ErrDateOutsidePeriod),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrPeriodClosed),
		errors.Is(err, domain.ErrInvalidThreshold),
		errors.Is(err, domain.ErrInvalidGoalDates),
		errors.Is(err, domain.ErrInvalidFrequency),
		errors.Is(err, domain.ErrInvalidRecurringConfig),
		errors.Is(err, domain.ErrAINotConfigured),
		errors.Is(err, domain.ErrInvalidAIProvider),
		errors.Is(err, domain.ErrInvalidTrackingConfig),
		errors.Is(err, domain.ErrInvalidSettings):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrAIUnavailable):
		return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
	case errors.Is(err, domain.ErrAIUpstream):
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	default:
		return err
	}
}
