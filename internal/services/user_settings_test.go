package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// mockSettingsStore embeds database.Store so we only override what the service
// touches. periodCalled/updateCalled let a test assert that a rejected input
// never reached the database.
type mockSettingsStore struct {
	database.Store
	getFn         func(ctx context.Context, userID uuid.UUID) (sqlc.UserSetting, error)
	updateFn      func(ctx context.Context, arg sqlc.UpdateUserSettingsParams) (sqlc.UserSetting, error)
	activeFn      func(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error)
	txnCount      int64
	updateCalled  bool
	updateArg     sqlc.UpdateUserSettingsParams
	closeCalled   bool
	periodQueried bool
	reshapeCalled bool
	reshapeArg    sqlc.ReshapeTrackingPeriodParams
}

func (m *mockSettingsStore) GetUserSettingsByUserID(ctx context.Context, userID uuid.UUID) (sqlc.UserSetting, error) {
	return m.getFn(ctx, userID)
}

func (m *mockSettingsStore) UpdateUserSettings(ctx context.Context, arg sqlc.UpdateUserSettingsParams) (sqlc.UserSetting, error) {
	m.updateCalled = true
	m.updateArg = arg
	return m.updateFn(ctx, arg)
}

func (m *mockSettingsStore) GetActiveTrackingPeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
	m.periodQueried = true
	if m.activeFn != nil {
		return m.activeFn(ctx, userID)
	}
	return sqlc.TrackingPeriod{}, pgx.ErrNoRows
}

func (m *mockSettingsStore) ClosePeriodTx(ctx context.Context, periodID, userID uuid.UUID) (database.ClosePeriodResult, error) {
	m.closeCalled = true
	return database.ClosePeriodResult{}, nil
}

func (m *mockSettingsStore) CountTransactionsByPeriod(ctx context.Context, periodID uuid.UUID) (int64, error) {
	return m.txnCount, nil
}

func (m *mockSettingsStore) ReshapeTrackingPeriod(ctx context.Context, arg sqlc.ReshapeTrackingPeriodParams) (sqlc.TrackingPeriod, error) {
	m.reshapeCalled = true
	m.reshapeArg = arg
	return sqlc.TrackingPeriod{
		ID:               arg.ID,
		EndDate:          arg.EndDate,
		ConfigPeriodMode: arg.ConfigPeriodMode,
		IsTransition:     arg.IsTransition,
	}, nil
}

// fixedNow is the clock all these tests run at.
var fixedNow = time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)

func newSettingsSvc(store database.Store) *UserSettingsService {
	return &UserSettingsService{store: store, now: func() time.Time { return fixedNow }}
}

func defaultSettings() sqlc.UserSetting {
	return sqlc.UserSetting{
		TrackingStartDay:     15,
		TrackingDurationDays: 30,
		TrackingPeriodMode:   domain.PeriodModeRolling,
		DefaultCurrency:      "COP",
		CountryCode:          "CO",
		Locale:               "es-CO",
		Theme:                "system",
		DefaultPeriodView:    "full",
		SubscriptionTier:     "free",
	}
}

// echoUpdate applies the non-nil params on top of the defaults, the way the
// COALESCE in the SQL does.
func echoUpdate(_ context.Context, arg sqlc.UpdateUserSettingsParams) (sqlc.UserSetting, error) {
	row := defaultSettings()
	if arg.TrackingStartDay != nil {
		row.TrackingStartDay = *arg.TrackingStartDay
	}
	if arg.TrackingDurationDays != nil {
		row.TrackingDurationDays = *arg.TrackingDurationDays
	}
	if arg.TrackingPeriodMode != nil {
		row.TrackingPeriodMode = *arg.TrackingPeriodMode
	}
	if arg.DefaultCurrency != nil {
		row.DefaultCurrency = *arg.DefaultCurrency
	}
	if arg.Locale != nil {
		row.Locale = *arg.Locale
	}
	if arg.Theme != nil {
		row.Theme = *arg.Theme
	}
	if arg.DefaultPeriodView != nil {
		row.DefaultPeriodView = *arg.DefaultPeriodView
	}
	return row, nil
}

func newStoreForUpdate() *mockSettingsStore {
	return &mockSettingsStore{
		getFn:    func(context.Context, uuid.UUID) (sqlc.UserSetting, error) { return defaultSettings(), nil },
		updateFn: echoUpdate,
	}
}

// assertRejected checks the input never made it to the database.
func assertRejected(t *testing.T, store *mockSettingsStore, err, want error) {
	t.Helper()
	require.ErrorIs(t, err, want)
	assert.False(t, store.updateCalled, "invalid input must not reach the database")
}

func TestUpdateSettings_PartialLeavesOtherFieldsNil(t *testing.T) {
	store := newStoreForUpdate()
	svc := newSettingsSvc(store)

	d := int16(31)
	view, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{TrackingDurationDays: &d})

	require.NoError(t, err)
	assert.Equal(t, int16(31), view.Settings.TrackingDurationDays)

	// Everything the caller did not send must travel as NULL so COALESCE keeps
	// the stored value.
	arg := store.updateArg
	require.NotNil(t, arg.TrackingDurationDays)
	assert.Equal(t, int16(31), *arg.TrackingDurationDays)
	assert.Nil(t, arg.TrackingStartDay)
	assert.Nil(t, arg.DefaultCurrency)
	assert.Nil(t, arg.Locale)
	assert.Nil(t, arg.Theme)
	assert.Nil(t, arg.DefaultPeriodView)
}

func TestUpdateSettings_DurationOutOfRange(t *testing.T) {
	for _, days := range []int16{27, 32, 0, -1} {
		store := newStoreForUpdate()
		svc := newSettingsSvc(store)

		d := days
		_, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{TrackingDurationDays: &d})

		assertRejected(t, store, err, domain.ErrInvalidTrackingConfig)
	}
}

func TestUpdateSettings_StartDayOutOfRange(t *testing.T) {
	for _, day := range []int16{0, 32} {
		store := newStoreForUpdate()
		svc := newSettingsSvc(store)

		sd := day
		_, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{TrackingStartDay: &sd})

		assertRejected(t, store, err, domain.ErrInvalidTrackingConfig)
	}
}

func TestUpdateSettings_InvalidTheme(t *testing.T) {
	store := newStoreForUpdate()
	svc := newSettingsSvc(store)

	theme := "neon"
	_, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{Theme: &theme})

	assertRejected(t, store, err, domain.ErrInvalidSettings)
}

func TestUpdateSettings_InvalidPeriodView(t *testing.T) {
	store := newStoreForUpdate()
	svc := newSettingsSvc(store)

	view := "monthly"
	_, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{DefaultPeriodView: &view})

	assertRejected(t, store, err, domain.ErrInvalidSettings)
}

func TestUpdateSettings_InvalidCurrency(t *testing.T) {
	// Wrong length, and right length but lowercase (the column is CHAR(3) stored
	// uppercase).
	for _, cur := range []string{"COLP", "CO", "cop"} {
		store := newStoreForUpdate()
		svc := newSettingsSvc(store)

		c := cur
		_, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{DefaultCurrency: &c})

		assertRejected(t, store, err, domain.ErrInvalidSettings)
	}
}

func TestUpdateSettings_EmptyInputIsANoOp(t *testing.T) {
	store := newStoreForUpdate()
	svc := newSettingsSvc(store)

	view, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{})

	require.NoError(t, err)
	assert.Equal(t, defaultSettings().TrackingDurationDays, view.Settings.TrackingDurationDays)
	assert.True(t, store.updateCalled, "an empty update still round-trips to read the current row")
	assert.Nil(t, store.updateArg.TrackingDurationDays)
}

func TestUpdateSettings_NeverTouchesTheActivePeriod(t *testing.T) {
	// Domain rule 8: changing the duration must not reshape the running period.
	// The period is only *read* (for the end date we report back), never closed.
	period := sqlc.TrackingPeriod{
		EndDate: pgtype.Date{Time: time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC), Valid: true},
	}
	store := newStoreForUpdate()
	store.activeFn = func(context.Context, uuid.UUID) (sqlc.TrackingPeriod, error) { return period, nil }
	svc := newSettingsSvc(store)

	d := int16(31)
	view, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{TrackingDurationDays: &d})

	require.NoError(t, err)
	assert.False(t, store.closeCalled, "updating settings must not close or roll over the active period")
	require.NotNil(t, view.ActivePeriodEnd)
	assert.Equal(t, "2026-08-13", view.ActivePeriodEnd.Format("2006-01-02"))
}

func TestGetSettings_NoActivePeriodIsNotAnError(t *testing.T) {
	store := &mockSettingsStore{
		getFn: func(context.Context, uuid.UUID) (sqlc.UserSetting, error) { return defaultSettings(), nil },
	}
	svc := newSettingsSvc(store)

	view, err := svc.Get(context.Background(), uuid.New())

	require.NoError(t, err)
	assert.Nil(t, view.ActivePeriodEnd)
	assert.Equal(t, "COP", view.Settings.DefaultCurrency)
}

func TestGetSettings_NotFound(t *testing.T) {
	store := &mockSettingsStore{
		getFn: func(context.Context, uuid.UUID) (sqlc.UserSetting, error) {
			return sqlc.UserSetting{}, pgx.ErrNoRows
		},
	}
	svc := newSettingsSvc(store)

	_, err := svc.Get(context.Background(), uuid.New())

	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestGetSettings_LazilyClosesAnExpiredPeriod(t *testing.T) {
	// The active period ended before "today", so resolving it rolls over once and
	// the date we report is the fresh period's, not the stale one's.
	stale := sqlc.TrackingPeriod{
		ID:      uuid.New(),
		EndDate: pgtype.Date{Time: time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC), Valid: true},
	}
	fresh := sqlc.TrackingPeriod{
		ID:      uuid.New(),
		EndDate: pgtype.Date{Time: time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC), Valid: true},
	}
	calls := 0
	store := &mockSettingsStore{
		getFn: func(context.Context, uuid.UUID) (sqlc.UserSetting, error) { return defaultSettings(), nil },
		activeFn: func(context.Context, uuid.UUID) (sqlc.TrackingPeriod, error) {
			calls++
			if calls == 1 {
				return stale, nil
			}
			return fresh, nil
		},
	}
	svc := newSettingsSvc(store)

	view, err := svc.Get(context.Background(), uuid.New())

	require.NoError(t, err)
	assert.True(t, store.closeCalled, "an already-ended period must be closed on read")
	require.NotNil(t, view.ActivePeriodEnd)
	assert.Equal(t, "2026-08-19", view.ActivePeriodEnd.Format("2006-01-02"))
}

func TestUpdateSettings_RejectsUnknownPeriodMode(t *testing.T) {
	store := newStoreForUpdate()
	svc := newSettingsSvc(store)

	mode := "monthly" // plausible, but not one of ours
	_, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{TrackingPeriodMode: &mode})

	assertRejected(t, store, err, domain.ErrInvalidTrackingConfig)
}

func TestUpdateSettings_AcceptsCalendarMode(t *testing.T) {
	store := newStoreForUpdate()
	svc := newSettingsSvc(store)

	mode := domain.PeriodModeCalendar
	view, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{TrackingPeriodMode: &mode})

	require.NoError(t, err)
	assert.Equal(t, domain.PeriodModeCalendar, view.Settings.TrackingPeriodMode)
	require.NotNil(t, store.updateArg.TrackingPeriodMode)
	assert.Equal(t, domain.PeriodModeCalendar, *store.updateArg.TrackingPeriodMode)
	// Nothing else was sent, so COALESCE must leave the rest alone.
	assert.Nil(t, store.updateArg.TrackingDurationDays)
}

// pristineFirstPeriod is the state a user is in while the onboarding wizard
// runs: period #1, freshly created, no transactions yet.
func pristineFirstPeriod() sqlc.TrackingPeriod {
	start := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	return sqlc.TrackingPeriod{
		ID:               uuid.New(),
		SequenceNumber:   1,
		StartDate:        pgtype.Date{Time: start, Valid: true},
		EndDate:          pgtype.Date{Time: start.AddDate(0, 0, 29), Valid: true},
		Status:           "active",
		ConfigPeriodMode: domain.PeriodModeRolling,
	}
}

func newStoreWithPeriod(period sqlc.TrackingPeriod, txnCount int64) *mockSettingsStore {
	store := newStoreForUpdate()
	store.activeFn = func(context.Context, uuid.UUID) (sqlc.TrackingPeriod, error) { return period, nil }
	store.txnCount = txnCount
	return store
}

func TestUpdateSettings_ReshapesPristineFirstPeriod(t *testing.T) {
	store := newStoreWithPeriod(pristineFirstPeriod(), 0)
	svc := newSettingsSvc(store)

	mode := domain.PeriodModeCalendar
	view, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{TrackingPeriodMode: &mode})

	require.NoError(t, err)
	require.True(t, store.reshapeCalled, "a pristine first period should be reshaped in place")

	// Started on the 1st, so calendar mode makes it a clean August with no bridge.
	assert.Equal(t, time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC), store.reshapeArg.EndDate.Time)
	assert.Equal(t, domain.PeriodModeCalendar, store.reshapeArg.ConfigPeriodMode)
	assert.False(t, store.reshapeArg.IsTransition)

	// The change took effect now, so the client must not promise "next period".
	assert.True(t, view.ReshapedActivePeriod)
	assert.False(t, view.ActivePeriodEnd.Before(time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC)))
}

func TestUpdateSettings_DefersOncePeriodHasTransactions(t *testing.T) {
	store := newStoreWithPeriod(pristineFirstPeriod(), 1)
	svc := newSettingsSvc(store)

	mode := domain.PeriodModeCalendar
	view, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{TrackingPeriodMode: &mode})

	require.NoError(t, err)
	assert.False(t, store.reshapeCalled, "a single transaction is enough to make the period untouchable")
	assert.False(t, view.ReshapedActivePeriod)
}

func TestUpdateSettings_DefersPastTheFirstPeriod(t *testing.T) {
	period := pristineFirstPeriod()
	period.SequenceNumber = 2
	store := newStoreWithPeriod(period, 0)
	svc := newSettingsSvc(store)

	mode := domain.PeriodModeCalendar
	view, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{TrackingPeriodMode: &mode})

	require.NoError(t, err)
	assert.False(t, store.reshapeCalled, "rule 8 applies from the second period on, empty or not")
	assert.False(t, view.ReshapedActivePeriod)
}

func TestUpdateSettings_DoesNotReshapeWhenModeIsUnchanged(t *testing.T) {
	store := newStoreWithPeriod(pristineFirstPeriod(), 0)
	svc := newSettingsSvc(store)

	mode := domain.PeriodModeRolling // already the current mode
	_, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{TrackingPeriodMode: &mode})

	require.NoError(t, err)
	assert.False(t, store.reshapeCalled, "re-sending the current mode should be a no-op")
}

func TestUpdateSettings_DurationChangeNeverReshapes(t *testing.T) {
	store := newStoreWithPeriod(pristineFirstPeriod(), 0)
	svc := newSettingsSvc(store)

	d := int16(28)
	_, err := svc.Update(context.Background(), uuid.New(), SettingsUpdateInput{TrackingDurationDays: &d})

	require.NoError(t, err)
	assert.False(t, store.reshapeCalled, "the carve-out is scoped to mode changes only")
}
