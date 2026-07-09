package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// RecurringEngineService drives the materialisation of recurring transaction
// templates into real transactions. It mirrors the dual-trigger pattern used
// by PeriodService and PeriodQueryService:
//
//   - A background scheduler calls ProcessDueRecurring periodically to catch
//     all due templates across all users.
//   - Individual user-facing requests (e.g. GET /recurring-transactions) call
//     ProcessUserRecurring as a lazy fallback.
//
// Idempotency is guaranteed at two levels:
//  1. Per-occurrence check: HasRecurringTransactionForDate ensures no duplicate
//     for the same (template, date) pair.
//  2. DB unique index: uq_txn_recurring_date enforces this at the storage layer.
type RecurringEngineService struct {
	store database.Store
	log   zerolog.Logger
	now   func() time.Time
}

// NewRecurringEngineService constructs the engine. Dates are computed in
// America/Bogota so "today" matches the Colombian user regardless of server tz.
func NewRecurringEngineService(store database.Store, log zerolog.Logger) *RecurringEngineService {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		loc = time.UTC
	}
	return &RecurringEngineService{
		store: store,
		log:   log,
		now:   func() time.Time { return time.Now().In(loc) },
	}
}

// ProcessDueRecurring processes every active recurring template whose
// next_due_date is on or before today. It is called by the scheduler and on
// server start-up. Errors on individual templates are logged and skipped so
// that one bad template does not block the rest. Returns how many individual
// occurrences were generated across all users.
func (s *RecurringEngineService) ProcessDueRecurring(ctx context.Context) (int, error) {
	today := dateOnly(s.now())

	due, err := s.store.ListDueRecurringTransactions(ctx, pgtype.Date{Time: today, Valid: true})
	if err != nil {
		return 0, err
	}

	generated := 0
	for _, tmpl := range due {
		n, err := s.processTemplate(ctx, tmpl, today)
		if err != nil {
			s.log.Error().Err(err).
				Str("recurring_id", tmpl.ID.String()).
				Str("user_id", tmpl.UserID.String()).
				Msg("failed to process recurring template")
			continue
		}
		generated += n
	}
	return generated, nil
}

// ProcessUserRecurring is the lazy trigger variant: it processes only the
// templates for a specific user. Call this from endpoints that interact with
// recurring transactions or transactions so that templates are materialised
// even when the scheduler hasn't run yet. Returns the number of occurrences
// generated.
func (s *RecurringEngineService) ProcessUserRecurring(ctx context.Context, userID uuid.UUID) (int, error) {
	today := dateOnly(s.now())

	all, err := s.store.ListRecurringTransactionsForUser(ctx, userID)
	if err != nil {
		return 0, err
	}

	generated := 0
	for _, tmpl := range all {
		if !tmpl.IsActive || !tmpl.NextDueDate.Valid {
			continue
		}
		if dateOnly(tmpl.NextDueDate.Time).After(today) {
			continue // not yet due
		}
		n, err := s.processTemplate(ctx, tmpl, today)
		if err != nil {
			s.log.Error().Err(err).
				Str("recurring_id", tmpl.ID.String()).
				Msg("failed to process recurring template (lazy)")
			continue
		}
		generated += n
	}
	return generated, nil
}

// processTemplate materialises all pending occurrences for a single template up
// to (and including) today. It implements the catch-up loop: if next_due_date
// is weeks in the past, all missed occurrences are generated one by one.
// Returns the number of occurrences generated.
func (s *RecurringEngineService) processTemplate(
	ctx context.Context,
	tmpl sqlc.RecurringTransaction,
	today time.Time,
) (int, error) {
	if !tmpl.NextDueDate.Valid {
		return 0, nil
	}

	// Resolve the user's active period. This is where the lazy-close happens:
	// if the current period has ended the service closes it and returns the new
	// one — same pattern as BudgetService and TransactionService.
	period, err := s.resolveActivePeriod(ctx, tmpl.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrNoActivePeriod) {
			// No active period yet for this user; skip silently.
			return 0, nil
		}
		return 0, err
	}

	// dayOfMonth / dayOfWeek hints for the advance calculation.
	var dom, dow *int
	if tmpl.DayOfMonth != nil {
		v := int(*tmpl.DayOfMonth)
		dom = &v
	}
	if tmpl.DayOfWeek != nil {
		v := int(*tmpl.DayOfWeek)
		dow = &v
	}
	var customInterval *int
	if tmpl.CustomIntervalDays != nil {
		v := int(*tmpl.CustomIntervalDays)
		customInterval = &v
	}

	occurrenceDate := dateOnly(tmpl.NextDueDate.Time)
	generated := 0

	// Catch-up loop: generate every overdue occurrence until next_due_date is
	// in the future. The loop is bounded by a safety limit so a misconfigured
	// template can never spin indefinitely.
	const maxOccurrences = 1200 // ~100 years of monthly occurrences
	for i := 0; i < maxOccurrences; i++ {
		if occurrenceDate.After(today) {
			break // all caught up
		}

		// ── end_date check ─────────────────────────────────────────────────────
		if tmpl.EndDate.Valid && occurrenceDate.After(dateOnly(tmpl.EndDate.Time)) {
			// This occurrence is past the template's configured end_date. Deactivate
			// without generating a transaction.
			if err := s.deactivateTemplate(ctx, tmpl.ID, occurrenceDate); err != nil {
				return generated, err
			}
			return generated, nil
		}

		// ── Advance next_due_date ──────────────────────────────────────────────
		nextOccurrence := advanceByFrequency(occurrenceDate, tmpl.Frequency, dom, dow, customInterval)
		var nextDuePtr *time.Time

		stillActive := true

		if tmpl.EndDate.Valid && !nextOccurrence.After(dateOnly(tmpl.EndDate.Time)) {
			// next occurrence is within end_date → keep active
			nextDuePtr = &nextOccurrence
		} else if tmpl.EndDate.Valid && nextOccurrence.After(dateOnly(tmpl.EndDate.Time)) {
			// next would be past end_date → this is the last occurrence; deactivate
			stillActive = false
			nextDuePtr = nil
		} else {
			// no end_date: keep going
			nextDuePtr = &nextOccurrence
		}

		// ── transaction_date: clamped to the active period ────────────────────
		txDate := clampToActivePeriod(occurrenceDate, today, period)

		// ── Materialise ────────────────────────────────────────────────────────
		if _, err := s.store.MaterialiseRecurringTx(ctx, database.MaterialiseRecurringParams{
			Template:         tmpl,
			OccurrenceDate:   occurrenceDate,
			TransactionDate:  txDate,
			TrackingPeriodID: period.ID,
			NextDueDate:      nextDuePtr,
			StillActive:      stillActive,
		}); err != nil {
			return generated, err
		}
		generated++

		if !stillActive {
			return generated, nil
		}

		if nextDuePtr == nil {
			break
		}
		occurrenceDate = *nextDuePtr
	}

	return generated, nil
}

// resolveActivePeriod returns the user's current active period, lazily closing
// any that have already ended (same pattern as TransactionService).
func (s *RecurringEngineService) resolveActivePeriod(ctx context.Context, userID uuid.UUID) (sqlc.TrackingPeriod, error) {
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

// deactivateTemplate marks a template inactive when it has been exhausted
// (all occurrences past end_date have been consumed). It reuses the
// AdvanceRecurringTransaction query with is_active = false.
func (s *RecurringEngineService) deactivateTemplate(ctx context.Context, id uuid.UUID, lastDate time.Time) error {
	_, err := s.store.AdvanceRecurringTransaction(ctx, sqlc.AdvanceRecurringTransactionParams{
		LastGeneratedDate: pgtype.Date{Time: dateOnly(lastDate), Valid: true},
		NextDueDate:       pgtype.Date{}, // null
		IsActive:          false,
		ID:                id,
	})
	return err
}

// ─── Date arithmetic helpers ─────────────────────────────────────────────────

// advanceByFrequency computes the next occurrence date after `current` given
// the template's frequency settings. It mirrors the computeNextDueDate logic
// but operates on a date that is already a scheduled occurrence — so it
// advances by exactly one interval rather than searching for the first due date.
func advanceByFrequency(current time.Time, freq string, dayOfMonth, dayOfWeek, customInterval *int) time.Time {
	switch freq {
	case "daily":
		return current.AddDate(0, 0, 1)

	case "weekly":
		return current.AddDate(0, 0, 7)

	case "biweekly":
		return current.AddDate(0, 0, 14)

	case "monthly":
		// Land on the target day-of-month, clamped to the month's length.
		// Go's date normalization must NOT be used here: Jan 31 + 1 month would
		// become "Feb 31" = Mar 3, and from there the schedule drifts forever,
		// skipping months. Clamping (Jan 31 → Feb 28 → Mar 31) keeps the anchor.
		day := current.Day()
		if dayOfMonth != nil {
			day = *dayOfMonth
		}
		return addMonthClamped(current, day)

	case "yearly":
		// Same clamping concern for Feb 29 on non-leap years (→ Feb 28, not Mar 1).
		y, m, d := current.Date()
		if last := lastDayOfMonth(y+1, m, current.Location()); d > last {
			d = last
		}
		return time.Date(y+1, m, d, 0, 0, 0, 0, current.Location())

	case "custom":
		if customInterval == nil || *customInterval <= 0 {
			return current.AddDate(0, 0, 1) // fallback: +1 day
		}
		return current.AddDate(0, 0, *customInterval)

	default:
		return current.AddDate(0, 0, 1)
	}
}

// addMonthClamped advances `current` by one calendar month, landing on `day`
// clamped to the target month's last day (e.g. day 31 in February → Feb 28).
func addMonthClamped(current time.Time, day int) time.Time {
	y, m, _ := current.Date()
	if last := lastDayOfMonth(y, m+1, current.Location()); day > last {
		day = last
	}
	return time.Date(y, m+1, day, 0, 0, 0, 0, current.Location())
}

// lastDayOfMonth returns the number of days in the given month (month may be
// out of the 1-12 range; time.Date normalizes it).
func lastDayOfMonth(year int, month time.Month, loc *time.Location) int {
	// Day 0 of the following month == last day of the requested month.
	return time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
}

// clampToActivePeriod returns the date to write into the generated transaction.
// Rules:
//   - If occurrenceDate is within [period.start, period.end], use it directly.
//   - If it is before the period start (catch-up into a new period), use period start.
//   - If it is after today, use today (should not happen — the caller guarantees
//     occurrenceDate <= today, but we clamp defensively).
func clampToActivePeriod(occurrenceDate, today time.Time, period sqlc.TrackingPeriod) time.Time {
	pStart := dateOnly(period.StartDate.Time)
	pEnd := dateOnly(period.EndDate.Time)
	d := dateOnly(occurrenceDate)

	if d.Before(pStart) {
		return pStart
	}
	if d.After(pEnd) {
		return pEnd
	}
	if d.After(today) {
		return today
	}
	return d
}
