package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
)

// Store is the data-access boundary the services depend on. It embeds the
// sqlc-generated Querier (all single-statement queries) and adds higher-level
// methods that span several statements inside a transaction.
//
// Being an interface keeps services testable (mockable) without a real DB.
type Store interface {
	sqlc.Querier

	// OnboardUser atomically creates the user, their settings and the first
	// tracking period. The caller computes all values (dates, defaults); this
	// method only orchestrates the inserts inside a single transaction.
	OnboardUser(ctx context.Context, arg OnboardUserParams) (OnboardUserResult, error)

	// CreateTransactionTx inserts a transaction and adjusts the affected
	// account balance(s) in a single transaction.
	CreateTransactionTx(ctx context.Context, arg sqlc.CreateTransactionParams) (sqlc.Transaction, error)

	// SoftDeleteTransactionTx soft-deletes a transaction and reverses its
	// balance effect in a single transaction. Returns ErrNotFound if missing.
	SoftDeleteTransactionTx(ctx context.Context, id, userID uuid.UUID) (sqlc.Transaction, error)

	// UpdateTransactionTx reverses the old balance effect and applies the new
	// one in a single transaction. Returns ErrNotFound if missing.
	UpdateTransactionTx(ctx context.Context, arg sqlc.UpdateTransactionParams) (sqlc.Transaction, error)

	// ClosePeriodTx closes a tracking period, snapshots its summary, generates
	// the next (contiguous) period and copies its budgets — all atomically.
	ClosePeriodTx(ctx context.Context, periodID, userID uuid.UUID) (ClosePeriodResult, error)

	// CreateContributionTx atomically inserts a savings goal contribution and
	// updates the goal's current_amount. If the new total meets or exceeds the
	// target_amount the goal is marked achieved.
	CreateContributionTx(ctx context.Context, arg CreateContributionTxParams) (CreateContributionTxResult, error)

	// MaterialiseRecurringTx generates one transaction for a single occurrence
	// of a recurring template and advances the template's next_due_date —
	// all atomically. It is idempotent: if a transaction for (templateID, date)
	// already exists the call is a no-op (returns the existing transaction).
	MaterialiseRecurringTx(ctx context.Context, arg MaterialiseRecurringParams) (sqlc.Transaction, error)

	// GetFinalInsights returns the "final" (close-time) insights for a closed
	// period. Returns an empty slice (not an error) when none exist yet.
	GetFinalInsights(ctx context.Context, periodID, userID uuid.UUID) ([]sqlc.TrackingPeriodInsight, error)

	// RefreshImmediateDuringInsights recalculates the three "immediate" during
	// insights (spending_pace, budget_warning, budget_exceeded) for an active
	// period, replacing them atomically. Called after every transaction mutation.
	// No-op if the period is already closed.
	RefreshImmediateDuringInsights(ctx context.Context, userID uuid.UUID, periodID uuid.UUID) error

	// RefreshAllDuringInsights recalculates ALL seven "during" insights for an
	// active period, replacing them atomically. Called lazily when the client
	// requests GET /tracking-periods/:id/insights on the active period.
	// Returns the fresh insight rows (empty when the period is closed).
	RefreshAllDuringInsights(ctx context.Context, periodID, userID uuid.UUID) ([]sqlc.TrackingPeriodInsight, error)
}

// SQLStore is the pgx-backed implementation of Store.
type SQLStore struct {
	*sqlc.Queries
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by the given pgx pool.
func NewStore(pool *pgxpool.Pool) *SQLStore {
	return &SQLStore{
		Queries: sqlc.New(pool),
		pool:    pool,
	}
}

// execTx runs fn within a database transaction, committing on success and
// rolling back on error.
func (s *SQLStore) execTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if err := fn(sqlc.New(tx)); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("tx error: %v; rollback error: %w", err, rbErr)
		}
		return err
	}

	return tx.Commit(ctx)
}

// MaterialiseRecurringParams is the input for MaterialiseRecurringTx.
// The caller (RecurringEngineService) pre-computes all dates and the next
// iteration value; the store only does the DB work.
type MaterialiseRecurringParams struct {
	// Template is the recurring_transaction row being materialised.
	Template sqlc.RecurringTransaction

	// OccurrenceDate is the date of the specific occurrence being generated
	// (the current value of next_due_date at the time of generation).
	OccurrenceDate time.Time

	// TransactionDate is the date written to the generated transaction row.
	// For the current occurrence it is min(OccurrenceDate, today) clamped to
	// the active period's [start_date, end_date].
	TransactionDate time.Time

	// TrackingPeriodID is the user's currently active period.
	TrackingPeriodID uuid.UUID

	// NextDueDate is the next scheduled occurrence after this one. When nil
	// the template has been exhausted (end_date reached).
	NextDueDate *time.Time

	// StillActive signals whether the template should remain active after
	// this generation step.
	StillActive bool
}

// OnboardUserParams carries the fully-computed inputs for onboarding. The
// UserID fields of Settings and Period are filled in by OnboardUser once the
// user row is created, so callers can leave them zero.
type OnboardUserParams struct {
	User     sqlc.CreateUserParams
	Settings sqlc.CreateUserSettingsParams
	Period   sqlc.CreateTrackingPeriodParams
}

// OnboardUserResult is the set of rows created during onboarding.
type OnboardUserResult struct {
	User           sqlc.User
	Settings       sqlc.UserSetting
	TrackingPeriod sqlc.TrackingPeriod
}

// OnboardUser implements Store.
func (s *SQLStore) OnboardUser(ctx context.Context, arg OnboardUserParams) (OnboardUserResult, error) {
	var res OnboardUserResult

	err := s.execTx(ctx, func(q *sqlc.Queries) error {
		user, err := q.CreateUser(ctx, arg.User)
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		res.User = user

		settings := arg.Settings
		settings.UserID = user.ID
		us, err := q.CreateUserSettings(ctx, settings)
		if err != nil {
			return fmt.Errorf("create user settings: %w", err)
		}
		res.Settings = us

		period := arg.Period
		period.UserID = user.ID
		tp, err := q.CreateTrackingPeriod(ctx, period)
		if err != nil {
			return fmt.Errorf("create tracking period: %w", err)
		}
		res.TrackingPeriod = tp

		return nil
	})

	return res, err
}
