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

	// ErrAccountHasTransactions is returned when the opening balance of an
	// account that already has movements is edited. Once money has flowed the
	// opening balance is history, not a setting.
	ErrAccountHasTransactions = errors.New("the opening balance cannot be changed once the account has transactions")

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

	// ErrInvalidTrackingConfig is returned when the tracking configuration is out
	// of the ranges the schema allows: start day 1-31, duration 28-31.
	ErrInvalidTrackingConfig = errors.New("invalid tracking configuration")

	// ErrInvalidSettings is returned when a user preference is not one of the
	// values the schema allows (theme, default_period_view, currency).
	ErrInvalidSettings = errors.New("invalid user settings value")
)

// businessRuleErrors lists every error above that means "the caller broke a
// rule", as opposed to "something failed on our side".
//
// It exists because /sync/push has to tell those two apart: a business-rule
// violation is permanent, so the item is reported as rejected and the client
// drops it from its outbox, while a genuine fault is worth retrying. Before this
// list the distinction was a switch in the sync service, and it silently fell
// out of date the moment a new entity started flowing through push — a duplicate
// budget came back as "internal server error", so the client had no way to tell
// the user what was wrong and would retry a request that could never succeed.
//
// When you add an error above, add it here too unless it really does mean the
// server failed.
var businessRuleErrors = []error{
	ErrEmailAlreadyExists,
	ErrNotFound,
	ErrNoActivePeriod,
	ErrAccountNotFound,
	ErrCategoryNotFound,
	ErrInvalidTransfer,
	ErrDateOutsidePeriod,
	ErrInvalidAmount,
	ErrInvalidCredentials,
	ErrInvalidToken,
	ErrBudgetExists,
	ErrPeriodClosed,
	ErrAccountHasTransactions,
	ErrInvalidThreshold,
	ErrGoalNotFound,
	ErrInvalidGoalDates,
	ErrInvalidFrequency,
	ErrInvalidRecurringConfig,
	ErrAINotConfigured,
	ErrInvalidAIProvider,
	ErrInvalidTrackingConfig,
	ErrInvalidSettings,
}

// IsBusinessRule reports whether err is a rule the caller violated.
//
// Deliberately excluded: ErrAIUnavailable (the server is missing its encryption
// key) and ErrAIUpstream (a third party failed) — neither is the caller's doing,
// and both can succeed on a later attempt.
func IsBusinessRule(err error) bool {
	for _, target := range businessRuleErrors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
