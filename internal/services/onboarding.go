// Package services holds business logic. Services depend on the data-access
// Store, never on HTTP or SQL details directly.
package services

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// Onboarding defaults (Colombia-focused MVP). These mirror the schema defaults
// but are set explicitly so the first tracking period can be derived from them.
const (
	defaultDurationDays = 30
	defaultCurrency     = "COP"
	defaultCountryCode  = "CO"
	defaultLocale       = "es-CO"
	defaultTheme        = "system"
	defaultPeriodView   = "full"
	defaultTier         = "free"

	// Everyone starts on rolling periods. The onboarding wizard offers calendar
	// months right after registration, and switching there reshapes this first
	// period in place — see UserSettingsService.reshapePristineFirstPeriod.
	defaultPeriodMode = domain.PeriodModeRolling

	// Default account provisioned for every new user. Transactions require an
	// account_id, so a user with none cannot register a single expense.
	defaultAccountName = "Efectivo"
	defaultAccountType = "cash"
	defaultAccountIcon = "wallet"

	// uniqueViolation is the Postgres SQLSTATE for a unique_violation.
	uniqueViolation = "23505"
)

// OnboardingInput is the validated business input for onboarding a new user.
type OnboardingInput struct {
	Email        string
	FullName     *string
	PasswordHash *string
}

// OnboardingService creates a new user with sensible defaults and their first
// active tracking period.
type OnboardingService struct {
	store database.Store
	// now allows tests to inject a fixed clock; defaults to time.Now in Bogota.
	now func() time.Time
}

// NewOnboardingService builds the service. Dates are computed in America/Bogota
// so "today" matches the Colombian user regardless of where the server runs.
func NewOnboardingService(store database.Store) *OnboardingService {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		loc = time.UTC
	}
	return &OnboardingService{
		store: store,
		now:   func() time.Time { return time.Now().In(loc) },
	}
}

// Onboard creates the user, settings, first tracking period and a default cash
// account atomically.
//
// First-period rule (per product decision): the period starts TODAY, shaped by
// defaultPeriodMode. The configured tracking_start_day is set to today's day of
// month, so future auto-generated periods follow the same monthly cadence.
//
// The default account exists so the <5s capture flow works on first launch; the
// client walks the user through renaming it and setting its opening balance
// during onboarding, but the invariant holds even if that is skipped.
func (s *OnboardingService) Onboard(ctx context.Context, in OnboardingInput) (database.OnboardUserResult, error) {
	// Fast pre-check for a friendly error; the DB unique constraint is the
	// real guarantee and is handled below in case of a race.
	if _, err := s.store.GetUserByEmail(ctx, in.Email); err == nil {
		return database.OnboardUserResult{}, domain.ErrEmailAlreadyExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return database.OnboardUserResult{}, err
	}

	rng := domain.FirstPeriodRange(s.now(), defaultPeriodMode, defaultDurationDays)
	startDay := int16(rng.Start.Day())
	accountIcon := defaultAccountIcon

	params := database.OnboardUserParams{
		User: sqlc.CreateUserParams{
			Email:        in.Email,
			FullName:     in.FullName,
			PasswordHash: in.PasswordHash,
		},
		Settings: sqlc.CreateUserSettingsParams{
			TrackingStartDay:     startDay,
			TrackingDurationDays: defaultDurationDays,
			TrackingPeriodMode:   defaultPeriodMode,
			DefaultCurrency:      defaultCurrency,
			CountryCode:          defaultCountryCode,
			Locale:               defaultLocale,
			Theme:                defaultTheme,
			DefaultPeriodView:    defaultPeriodView,
			SubscriptionTier:     defaultTier,
		},
		Period: sqlc.CreateTrackingPeriodParams{
			StartDate:          pgDate(rng.Start),
			EndDate:            pgDate(rng.End),
			Status:             "active",
			SequenceNumber:     1,
			ConfigStartDay:     startDay,
			ConfigDurationDays: defaultDurationDays,
			ConfigPeriodMode:   defaultPeriodMode,
			IsTransition:       rng.IsTransition,
		},
		Account: sqlc.CreateAccountParams{
			Name:           defaultAccountName,
			AccountType:    defaultAccountType,
			Currency:       defaultCurrency,
			InitialBalance: decimal.Zero,
			Icon:           &accountIcon,
			DisplayOrder:   0,
		},
	}

	res, err := s.store.OnboardUser(ctx, params)
	if err != nil {
		if isUniqueViolation(err) {
			return database.OnboardUserResult{}, domain.ErrEmailAlreadyExists
		}
		return database.OnboardUserResult{}, err
	}
	return res, nil
}

func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
