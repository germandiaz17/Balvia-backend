package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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
	TrackingPeriodMode   *string
	DefaultCurrency      *string
	Locale               *string
	Theme                *string
	DefaultPeriodView    *string
}

// IsEmpty reports whether the caller asked for no change at all.
func (in SettingsUpdateInput) IsEmpty() bool {
	return in.TrackingStartDay == nil &&
		in.TrackingDurationDays == nil &&
		in.TrackingPeriodMode == nil &&
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
	// ReshapedActivePeriod reports that the change took effect immediately
	// because the onboarding carve-out applied, rather than being deferred to
	// the next period. See UserSettingsService.Update.
	ReshapedActivePeriod bool
}

// UserSettingsService reads and updates a user's preferences.
//
// Domain rule 8 — changes to the tracking configuration apply to the NEXT
// tracking period, never to the active one. This service upholds it by
// construction: it writes to user_settings and lets the rollover read them at
// close time (ClosePeriodTx re-reads GetUserSettingsByUserID), so a new duration
// or mode is picked up by whichever period is generated next. Do NOT "optimise"
// this by reading the settings when a period is created — that would silently
// reshape the active period and break the rule.
//
// There is exactly one exception, reshapePristineFirstPeriod, and its guard is
// deliberately narrow. Read its doc before touching it.
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
		TrackingPeriodMode:   in.TrackingPeriodMode,
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

	view, err := s.withActivePeriod(ctx, userID, row)
	if err != nil {
		return SettingsView{}, err
	}

	if in.TrackingPeriodMode != nil {
		reshaped, err := s.reshapePristineFirstPeriod(ctx, userID, row)
		if err != nil {
			return SettingsView{}, err
		}
		if reshaped != nil {
			end := dateOnly(reshaped.EndDate.Time)
			view.ActivePeriodEnd = &end
			view.ReshapedActivePeriod = true
		}
	}

	return view, nil
}

// reshapePristineFirstPeriod is the single, deliberate exception to domain rule
// 8, and it exists for the onboarding wizard.
//
// The wizard runs *after* registration, so by the time the user picks a period
// mode their first period already exists in the default one. Deferring the
// change would hand a brand-new user a transition bridge on day one, which is
// absurd. When the active period is their first AND carries no transactions,
// there is nothing a reshape can invalidate: no summary, no insights, no
// balances, nothing the user has seen add up. So we reshape it in place.
//
// Everything about the guard is load-bearing. Past the first period, or once a
// single transaction exists, the change defers like any other. It returns nil
// when the carve-out does not apply.
func (s *UserSettingsService) reshapePristineFirstPeriod(
	ctx context.Context,
	userID uuid.UUID,
	settings sqlc.UserSetting,
) (*sqlc.TrackingPeriod, error) {
	period, err := s.activePeriod(ctx, userID)
	if errors.Is(err, domain.ErrNoActivePeriod) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	if period.SequenceNumber != 1 || period.ConfigPeriodMode == settings.TrackingPeriodMode {
		return nil, nil
	}

	count, err := s.store.CountTransactionsByPeriod(ctx, period.ID)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, nil
	}

	// Keep the original start date — the period has already begun — and let the
	// new mode decide where it ends.
	rng := domain.FirstPeriodRange(
		period.StartDate.Time,
		settings.TrackingPeriodMode,
		int(settings.TrackingDurationDays),
	)
	updated, err := s.store.ReshapeTrackingPeriod(ctx, sqlc.ReshapeTrackingPeriodParams{
		ID:                 period.ID,
		EndDate:            pgtype.Date{Time: rng.End, Valid: true},
		ConfigPeriodMode:   settings.TrackingPeriodMode,
		ConfigDurationDays: settings.TrackingDurationDays,
		IsTransition:       rng.IsTransition,
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
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
	if m := in.TrackingPeriodMode; m != nil && !domain.ValidPeriodModes[*m] {
		return domain.ErrInvalidTrackingConfig
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
