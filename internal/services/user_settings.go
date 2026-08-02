package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// Bounds enforced by the chk_tracking_duration and chk_tracking_start_day CHECK
// constraints in migration 000003. Duplicated here so a bad value fails as a
// 422 domain error instead of surfacing as a 500 from Postgres.
const (
	minTrackingDurationDays = 28
	maxTrackingDurationDays = 31
	minTrackingStartDay     = 1
	maxTrackingStartDay     = 31
)

// ValidThemes mirrors the chk_theme constraint.
var ValidThemes = map[string]bool{
	"system": true,
	"light":  true,
	"dark":   true,
}

// ValidPeriodViews mirrors the chk_default_period_view constraint. The values
// match the ViewFull/ViewBiweekly/ViewWeekly constants used by the summary
// endpoint.
var ValidPeriodViews = map[string]bool{
	ViewFull:     true,
	ViewBiweekly: true,
	ViewWeekly:   true,
}

// SettingsUpdateInput is a partial update: a nil field leaves the column as is.
// country_code is not editable and subscription_tier is server-controlled, so
// neither is exposed here.
type SettingsUpdateInput struct {
	TrackingStartDay     *int16
	TrackingDurationDays *int16
	DefaultCurrency      *string
	Locale               *string
	Theme                *string
	DefaultPeriodView    *string
}

// IsEmpty reports whether the caller asked for no change at all.
func (in SettingsUpdateInput) IsEmpty() bool {
	return in.TrackingStartDay == nil &&
		in.TrackingDurationDays == nil &&
		in.DefaultCurrency == nil &&
		in.Locale == nil &&
		in.Theme == nil &&
		in.DefaultPeriodView == nil
}

// SettingsView bundles the user's settings with the end date of their active
// tracking period. The client needs that date to tell the user exactly when a
// change to the tracking configuration takes effect, instead of showing a vague
// "applies later" message.
type SettingsView struct {
	Settings sqlc.UserSetting
	// ActivePeriodEnd is nil when the user has no active period.
	ActivePeriodEnd *time.Time
}

// UserSettingsService reads and updates a user's preferences.
//
// Domain rule 8 — changes to the tracking configuration apply to the NEXT
// tracking period, never to the active one. This service upholds it by
// construction: it only ever writes to user_settings and never touches
// tracking_periods. The rollover reads the settings at close time
// (ClosePeriodTx re-reads GetUserSettingsByUserID), so a new duration is picked
// up by whichever period is generated next. Do NOT "optimise" this by reading
// the settings when a period is created — that would silently reshape the
// active period and break the rule.
type UserSettingsService struct {
	store database.Store
	now   func() time.Time
}

func NewUserSettingsService(store database.Store) *UserSettingsService {
	return &UserSettingsService{store: store, now: time.Now}
}

// Get returns the user's settings plus the end date of their active period.
func (s *UserSettingsService) Get(ctx context.Context, userID uuid.UUID) (SettingsView, error) {
	row, err := s.store.GetUserSettingsByUserID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SettingsView{}, domain.ErrNotFound
	} else if err != nil {
		return SettingsView{}, err
	}
	return s.withActivePeriod(ctx, userID, row)
}

// Update validates the input and applies it as a partial update. An empty input
// is a no-op that still returns the current settings.
func (s *UserSettingsService) Update(ctx context.Context, userID uuid.UUID, in SettingsUpdateInput) (SettingsView, error) {
	if err := validateSettingsInput(in); err != nil {
		return SettingsView{}, err
	}

	row, err := s.store.UpdateUserSettings(ctx, sqlc.UpdateUserSettingsParams{
		TrackingStartDay:     in.TrackingStartDay,
		TrackingDurationDays: in.TrackingDurationDays,
		DefaultCurrency:      in.DefaultCurrency,
		Locale:               in.Locale,
		Theme:                in.Theme,
		DefaultPeriodView:    in.DefaultPeriodView,
		UserID:               userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SettingsView{}, domain.ErrNotFound
	} else if err != nil {
		return SettingsView{}, err
	}
	return s.withActivePeriod(ctx, userID, row)
}

// validateSettingsInput rejects out-of-range and out-of-enum values before they
// reach Postgres, so the client gets a 422 with a domain error rather than a
// 500 from a CHECK violation.
func validateSettingsInput(in SettingsUpdateInput) error {
	if d := in.TrackingDurationDays; d != nil {
		if *d < minTrackingDurationDays || *d > maxTrackingDurationDays {
			return domain.ErrInvalidTrackingConfig
		}
	}
	if sd := in.TrackingStartDay; sd != nil {
		if *sd < minTrackingStartDay || *sd > maxTrackingStartDay {
			return domain.ErrInvalidTrackingConfig
		}
	}
	if t := in.Theme; t != nil && !ValidThemes[*t] {
		return domain.ErrInvalidSettings
	}
	if v := in.DefaultPeriodView; v != nil && !ValidPeriodViews[*v] {
		return domain.ErrInvalidSettings
	}
	if c := in.DefaultCurrency; c != nil {
		// CHAR(3), stored uppercase.
		if len(*c) != 3 || *c != strings.ToUpper(*c) {
			return domain.ErrInvalidSettings
		}
	}
	if l := in.Locale; l != nil && (*l == "" || len(*l) > 10) {
		return domain.ErrInvalidSettings
	}
	return nil
}

// withActivePeriod attaches the active period's end date, lazily closing any
// period that has already ended so the date we hand the client is the real one.
// A user with no active period is not an error here — settings are readable
// either way.
func (s *UserSettingsService) withActivePeriod(ctx context.Context, userID uuid.UUID, row sqlc.UserSetting) (SettingsView, error) {
	view := SettingsView{Settings: row}

	period, err := s.activePeriod(ctx, userID)
	if errors.Is(err, domain.ErrNoActivePeriod) {
		return view, nil
	} else if err != nil {
		return SettingsView{}, err
	}

	end := dateOnly(period.EndDate.Time)
	view.ActivePeriodEnd = &end
	return view, nil
}

// activePeriod returns the user's active period, lazily closing (and rolling
// over) any that have already ended. Mirrors BudgetService.activePeriod.
func (s *UserSettingsService) activePeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
	const maxRollovers = 120
	today := dateOnly(s.now())

	for i := 0; i < maxRollovers; i++ {
		period, err := s.store.GetActiveTrackingPeriod(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.TrackingPeriod{}, domain.ErrNoActivePeriod
		} else if err != nil {
			return sqlc.TrackingPeriod{}, err
		}

		if !dateOnly(period.EndDate.Time).Before(today) {
			return period, nil
		}

		if _, err := s.store.ClosePeriodTx(ctx, period.ID, userID); err != nil {
			return sqlc.TrackingPeriod{}, err
		}
	}

	return sqlc.TrackingPeriod{}, domain.ErrNoActivePeriod
}
