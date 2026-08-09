package services

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// mockStore embeds database.Store so we only override the methods the service
// uses; any other Querier method is present (nil) but never called here.
type mockStore struct {
	database.Store
	getUserByEmailFn func(ctx context.Context, email string) (sqlc.User, error)
	onboardUserFn    func(ctx context.Context, arg database.OnboardUserParams) (database.OnboardUserResult, error)
	onboardCalled    bool
}

func (m *mockStore) GetUserByEmail(ctx context.Context, email string) (sqlc.User, error) {
	return m.getUserByEmailFn(ctx, email)
}

func (m *mockStore) OnboardUser(ctx context.Context, arg database.OnboardUserParams) (database.OnboardUserResult, error) {
	m.onboardCalled = true
	return m.onboardUserFn(ctx, arg)
}

func newSvcWithClock(store database.Store, fixed time.Time) *OnboardingService {
	return &OnboardingService{store: store, now: func() time.Time { return fixed }}
}

func TestOnboard_HappyPath(t *testing.T) {
	fixed := time.Date(2026, time.June, 25, 10, 30, 0, 0, time.UTC)

	var captured database.OnboardUserParams
	store := &mockStore{
		getUserByEmailFn: func(_ context.Context, _ string) (sqlc.User, error) {
			return sqlc.User{}, pgx.ErrNoRows // email is free
		},
		onboardUserFn: func(_ context.Context, arg database.OnboardUserParams) (database.OnboardUserResult, error) {
			captured = arg
			return database.OnboardUserResult{User: sqlc.User{Email: arg.User.Email}}, nil
		},
	}

	svc := newSvcWithClock(store, fixed)
	res, err := svc.Onboard(context.Background(), OnboardingInput{Email: "german@balvia.co"})

	require.NoError(t, err)
	assert.Equal(t, "german@balvia.co", res.User.Email)

	// First period: starts today, lasts 30 days, sequence 1, active.
	assert.Equal(t, int32(1), captured.Period.SequenceNumber)
	assert.Equal(t, "active", captured.Period.Status)
	assert.Equal(t, "2026-06-25", captured.Period.StartDate.Time.Format("2006-01-02"))
	assert.Equal(t, "2026-07-24", captured.Period.EndDate.Time.Format("2006-01-02"))
	assert.Equal(t, int16(25), captured.Period.ConfigStartDay)
	assert.Equal(t, int16(30), captured.Period.ConfigDurationDays)

	// Settings defaults (Colombia).
	assert.Equal(t, int16(25), captured.Settings.TrackingStartDay)
	assert.Equal(t, int16(30), captured.Settings.TrackingDurationDays)
	assert.Equal(t, "COP", captured.Settings.DefaultCurrency)
	assert.Equal(t, "CO", captured.Settings.CountryCode)
	assert.Equal(t, "es-CO", captured.Settings.Locale)
	assert.Equal(t, "free", captured.Settings.SubscriptionTier)

	// A default cash account: without one the user cannot create a single
	// transaction, since account_id is required.
	assert.Equal(t, "Efectivo", captured.Account.Name)
	assert.Equal(t, "cash", captured.Account.AccountType)
	assert.Equal(t, "COP", captured.Account.Currency)
	assert.True(t, captured.Account.InitialBalance.IsZero())
	require.NotNil(t, captured.Account.Icon)
	assert.Equal(t, "wallet", *captured.Account.Icon)
}

func TestOnboard_DuplicateEmail(t *testing.T) {
	store := &mockStore{
		getUserByEmailFn: func(_ context.Context, _ string) (sqlc.User, error) {
			return sqlc.User{Email: "german@balvia.co"}, nil // email already taken
		},
		onboardUserFn: func(_ context.Context, _ database.OnboardUserParams) (database.OnboardUserResult, error) {
			return database.OnboardUserResult{}, nil
		},
	}

	svc := newSvcWithClock(store, time.Now())
	_, err := svc.Onboard(context.Background(), OnboardingInput{Email: "german@balvia.co"})

	require.ErrorIs(t, err, domain.ErrEmailAlreadyExists)
	assert.False(t, store.onboardCalled, "OnboardUser must not run when email exists")
}
