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
)
