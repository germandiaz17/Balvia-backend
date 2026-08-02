// Package domain holds cross-cutting domain types and errors.
package domain

import "errors"

// Domain errors. Services return these; handlers map them to HTTP status codes.
var (
	// ErrEmailAlreadyExists is returned when onboarding an email that is taken.
	ErrEmailAlreadyExists = errors.New("email already exists")

	// ErrNotFound is returned when a requested resource does not exist.
	ErrNotFound = errors.New("resource not found")

	// ErrNoActivePeriod is returned when a user has no active tracking period.
	ErrNoActivePeriod = errors.New("no active tracking period")

	// ErrAccountNotFound is returned when an account does not exist or is not the user's.
	ErrAccountNotFound = errors.New("account not found")

	// ErrCategoryNotFound is returned when a category is not usable by the user.
	ErrCategoryNotFound = errors.New("category not found")

	// ErrInvalidTransfer is returned when a transfer is missing/equal to its counter-account.
	ErrInvalidTransfer = errors.New("transfer requires a distinct destination account")

	// ErrDateOutsidePeriod is returned when the transaction date is outside the active period.
	ErrDateOutsidePeriod = errors.New("transaction date is outside the active tracking period")

	// ErrInvalidAmount is returned when a monetary amount is not strictly positive.
	ErrInvalidAmount = errors.New("amount must be greater than zero")

	// ErrInvalidCredentials is returned when login email/password do not match.
	ErrInvalidCredentials = errors.New("invalid email or password")

	// ErrInvalidToken is returned when a refresh token is missing/expired/revoked.
	ErrInvalidToken = errors.New("invalid or expired token")

	// ErrBudgetExists is returned when a budget already exists for the given
	// (tracking period, category) pair.
	ErrBudgetExists = errors.New("a budget already exists for this category in the period")

	// ErrPeriodClosed is returned when trying to mutate data anchored to a closed
	// (immutable) tracking period.
	ErrPeriodClosed = errors.New("the tracking period is closed and cannot be modified")

	// ErrInvalidThreshold is returned when an alert threshold is out of the 0-100 range.
	ErrInvalidThreshold = errors.New("alert thresholds must be between 0 and 100")

	// ErrGoalNotFound is returned when a savings goal does not exist or is not the user's.
	ErrGoalNotFound = errors.New("savings goal not found")

	// ErrInvalidGoalDates is returned when target_date is not strictly after start_date.
	ErrInvalidGoalDates = errors.New("target_date must be after start_date")

	// ErrInvalidFrequency is returned when the frequency value is not one of the
	// allowed enum values (daily, weekly, biweekly, monthly, yearly, custom).
	ErrInvalidFrequency = errors.New("frequency must be one of: daily, weekly, biweekly, monthly, yearly, custom")

	// ErrInvalidRecurringConfig is returned when a combination of recurring-template
	// fields violates a business rule (e.g. custom without custom_interval_days,
	// day ranges out of bounds, end_date not after start_date, invalid transaction type).
	ErrInvalidRecurringConfig = errors.New("invalid recurring transaction configuration")

	// ErrAIUnavailable is returned when AI features are requested but AI encryption
	// is not configured on the server (no AI_ENCRYPTION_KEY).
	ErrAIUnavailable = errors.New("ai service is unavailable")

	// ErrAINotConfigured is returned when the user has not set up their own AI
	// provider/key yet (BYOK).
	ErrAINotConfigured = errors.New("no ai provider configured for this user")

	// ErrInvalidAIProvider is returned when the requested AI provider or its
	// settings are invalid.
	ErrInvalidAIProvider = errors.New("invalid ai provider or api key")

	// ErrAIUpstream is returned when the user's AI provider rejected or failed the
	// request (e.g. an invalid API key, rate limit, or provider outage).
	ErrAIUpstream = errors.New("ai provider request failed")
)
