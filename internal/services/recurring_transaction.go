package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database"
	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// validFrequencies is the complete set of allowed frequency enum values.
var validFrequencies = map[string]bool{
	"daily":    true,
	"weekly":   true,
	"biweekly": true,
	"monthly":  true,
	"yearly":   true,
	"custom":   true,
}

// validRecurringTypes is the set of allowed transaction_type values for recurring
// templates (note: "transfer" is intentionally excluded — recurring transfers are
// not supported in the current domain model).
var validRecurringTypes = map[string]bool{
	"income":  true,
	"expense": true,
}

// RecurringTransactionInput is the validated business input for creating or
// updating a recurring transaction template.
type RecurringTransactionInput struct {
	AccountID          uuid.UUID
	CategoryID         *uuid.UUID
	Name               string
	TransactionType    string
	Amount             decimal.Decimal
	Currency           string
	Description        *string
	Frequency          string
	CustomIntervalDays *int
	DayOfMonth         *int
	DayOfWeek          *int
	StartDate          time.Time
	EndDate            *time.Time
	IsActive           bool
}

// RecurringTransactionService manages recurring transaction templates (CRUD only).
// The engine that materialises templates into real transactions is out of scope
// and implemented separately; next_due_date is computed on write but nothing
// "executes" it yet.
type RecurringTransactionService struct {
	store database.Store
}

// NewRecurringTransactionService builds the service.
func NewRecurringTransactionService(store database.Store) *RecurringTransactionService {
	return &RecurringTransactionService{store: store}
}

// Create adds a new recurring transaction template for the user.
func (s *RecurringTransactionService) Create(ctx context.Context, userID uuid.UUID, in RecurringTransactionInput) (sqlc.RecurringTransaction, error) {
	if err := validateRecurringInput(in); err != nil {
		return sqlc.RecurringTransaction{}, err
	}

	if _, err := s.store.GetAccount(ctx, sqlc.GetAccountParams{ID: in.AccountID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.RecurringTransaction{}, domain.ErrAccountNotFound
		}
		return sqlc.RecurringTransaction{}, err
	}

	categoryID, err := s.resolveCategory(ctx, userID, in.CategoryID)
	if err != nil {
		return sqlc.RecurringTransaction{}, err
	}

	currency := in.Currency
	if currency == "" {
		currency = defaultCurrency
	}

	params := s.buildParams(in, categoryID, currency)
	return s.store.CreateRecurringTransaction(ctx, sqlc.CreateRecurringTransactionParams{
		UserID:             userID,
		AccountID:          params.AccountID,
		CategoryID:         params.CategoryID,
		Name:               params.Name,
		TransactionType:    params.TransactionType,
		Amount:             params.Amount,
		Currency:           params.Currency,
		Description:        params.Description,
		Frequency:          params.Frequency,
		CustomIntervalDays: params.CustomIntervalDays,
		DayOfMonth:         params.DayOfMonth,
		DayOfWeek:          params.DayOfWeek,
		StartDate:          params.StartDate,
		EndDate:            params.EndDate,
		NextDueDate:        params.NextDueDate,
		IsActive:           params.IsActive,
	})
}

// List returns all non-deleted recurring transaction templates for the user,
// ordered by most recently created first.
func (s *RecurringTransactionService) List(ctx context.Context, userID uuid.UUID) ([]sqlc.RecurringTransaction, error) {
	return s.store.ListRecurringTransactionsForUser(ctx, userID)
}

// Get returns a single recurring transaction template owned by the user.
func (s *RecurringTransactionService) Get(ctx context.Context, userID, id uuid.UUID) (sqlc.RecurringTransaction, error) {
	rt, err := s.store.GetRecurringTransaction(ctx, sqlc.GetRecurringTransactionParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.RecurringTransaction{}, domain.ErrNotFound
	}
	return rt, err
}

// Update replaces all editable fields of a recurring transaction template.
// next_due_date is recomputed from the new start_date and frequency.
func (s *RecurringTransactionService) Update(ctx context.Context, userID, id uuid.UUID, in RecurringTransactionInput) (sqlc.RecurringTransaction, error) {
	if err := validateRecurringInput(in); err != nil {
		return sqlc.RecurringTransaction{}, err
	}

	if _, err := s.store.GetAccount(ctx, sqlc.GetAccountParams{ID: in.AccountID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.RecurringTransaction{}, domain.ErrAccountNotFound
		}
		return sqlc.RecurringTransaction{}, err
	}

	categoryID, err := s.resolveCategory(ctx, userID, in.CategoryID)
	if err != nil {
		return sqlc.RecurringTransaction{}, err
	}

	currency := in.Currency
	if currency == "" {
		currency = defaultCurrency
	}

	params := s.buildParams(in, categoryID, currency)
	rt, err := s.store.UpdateRecurringTransaction(ctx, sqlc.UpdateRecurringTransactionParams{
		AccountID:          params.AccountID,
		CategoryID:         params.CategoryID,
		Name:               params.Name,
		TransactionType:    params.TransactionType,
		Amount:             params.Amount,
		Currency:           params.Currency,
		Description:        params.Description,
		Frequency:          params.Frequency,
		CustomIntervalDays: params.CustomIntervalDays,
		DayOfMonth:         params.DayOfMonth,
		DayOfWeek:          params.DayOfWeek,
		StartDate:          params.StartDate,
		EndDate:            params.EndDate,
		NextDueDate:        params.NextDueDate,
		IsActive:           params.IsActive,
		ID:                 id,
		UserID:             userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.RecurringTransaction{}, domain.ErrNotFound
	}
	return rt, err
}

// Delete soft-deletes a recurring transaction template owned by the user.
func (s *RecurringTransactionService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.store.SoftDeleteRecurringTransaction(ctx, sqlc.SoftDeleteRecurringTransactionParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

// ─── Internal helpers ────────────────────────────────────────────────────────

// sharedParams is an internal struct that groups the common computed fields used
// by both Create and Update to avoid duplicating conversion logic.
type recurringParams struct {
	AccountID          uuid.UUID
	CategoryID         uuid.NullUUID
	Name               string
	TransactionType    string
	Amount             decimal.Decimal
	Currency           string
	Description        *string
	Frequency          string
	CustomIntervalDays *int32
	DayOfMonth         *int16
	DayOfWeek          *int16
	StartDate          pgtype.Date
	EndDate            pgtype.Date
	NextDueDate        pgtype.Date
	IsActive           bool
}

// buildParams converts a RecurringTransactionInput into the shared param set
// used by both Create and Update.
func (s *RecurringTransactionService) buildParams(in RecurringTransactionInput, categoryID uuid.NullUUID, currency string) recurringParams {
	var endDate pgtype.Date
	if in.EndDate != nil {
		endDate = pgtype.Date{Time: dateOnly(*in.EndDate), Valid: true}
	}

	var customInterval *int32
	if in.CustomIntervalDays != nil {
		v := int32(*in.CustomIntervalDays)
		customInterval = &v
	}

	var dayOfMonth *int16
	if in.DayOfMonth != nil {
		v := int16(*in.DayOfMonth)
		dayOfMonth = &v
	}

	var dayOfWeek *int16
	if in.DayOfWeek != nil {
		v := int16(*in.DayOfWeek)
		dayOfWeek = &v
	}

	start := dateOnly(in.StartDate)
	nextDue := computeNextDueDate(start, in.Frequency, in.DayOfMonth, in.DayOfWeek, in.CustomIntervalDays)

	return recurringParams{
		AccountID:          in.AccountID,
		CategoryID:         categoryID,
		Name:               in.Name,
		TransactionType:    in.TransactionType,
		Amount:             in.Amount,
		Currency:           currency,
		Description:        in.Description,
		Frequency:          in.Frequency,
		CustomIntervalDays: customInterval,
		DayOfMonth:         dayOfMonth,
		DayOfWeek:          dayOfWeek,
		StartDate:          pgtype.Date{Time: start, Valid: true},
		EndDate:            endDate,
		NextDueDate:        pgtype.Date{Time: nextDue, Valid: true},
		IsActive:           in.IsActive,
	}
}

// validateRecurringInput enforces recurring-template business rules. It is
// called before any DB access so invalid requests never touch the database.
func validateRecurringInput(in RecurringTransactionInput) error {
	if !validRecurringTypes[in.TransactionType] {
		return domain.ErrInvalidRecurringConfig
	}
	if !in.Amount.IsPositive() {
		return domain.ErrInvalidAmount
	}
	if !validFrequencies[in.Frequency] {
		return domain.ErrInvalidFrequency
	}
	if in.Frequency == "custom" && (in.CustomIntervalDays == nil || *in.CustomIntervalDays <= 0) {
		return domain.ErrInvalidRecurringConfig
	}
	if in.DayOfMonth != nil && (*in.DayOfMonth < 1 || *in.DayOfMonth > 31) {
		return domain.ErrInvalidRecurringConfig
	}
	if in.DayOfWeek != nil && (*in.DayOfWeek < 0 || *in.DayOfWeek > 6) {
		return domain.ErrInvalidRecurringConfig
	}
	if in.EndDate != nil && !dateOnly(*in.EndDate).After(dateOnly(in.StartDate)) {
		return domain.ErrInvalidRecurringConfig
	}
	return nil
}

// resolveCategory validates an optional category against the user and returns a
// NullUUID. Returns ErrCategoryNotFound when the category cannot be used.
func (s *RecurringTransactionService) resolveCategory(ctx context.Context, userID uuid.UUID, categoryID *uuid.UUID) (uuid.NullUUID, error) {
	if categoryID == nil {
		return uuid.NullUUID{}, nil
	}
	if _, err := s.store.GetCategoryForUser(ctx, sqlc.GetCategoryForUserParams{
		ID:     *categoryID,
		UserID: uuid.NullUUID{UUID: userID, Valid: true},
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.NullUUID{}, domain.ErrCategoryNotFound
		}
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: *categoryID, Valid: true}, nil
}

// computeNextDueDate calculates the first scheduled occurrence on or after
// startDate according to the given frequency and optional day hints.
//
// Rules:
//   - daily, yearly, custom: returns startDate as-is.
//   - weekly / biweekly with dayOfWeek: returns the next occurrence of that
//     weekday (0=Sunday … 6=Saturday) that is >= startDate.
//   - monthly with dayOfMonth: returns that day-of-month in the same month if
//     >= startDate, otherwise the same day in the next calendar month. Go's
//     date normalization handles overflow (e.g. Apr 31 → May 1).
//   - any frequency without a relevant day hint: returns startDate.
func computeNextDueDate(start time.Time, freq string, dayOfMonth, dayOfWeek *int, _ *int) time.Time {
	switch freq {
	case "weekly", "biweekly":
		if dayOfWeek == nil {
			return start
		}
		// time.Weekday: Sunday=0, Monday=1, …, Saturday=6 — same mapping as
		// Postgres EXTRACT(DOW …).
		targetWD := time.Weekday(*dayOfWeek)
		diff := (int(targetWD) - int(start.Weekday()) + 7) % 7
		return start.AddDate(0, 0, diff)

	case "monthly":
		if dayOfMonth == nil {
			return start
		}
		dom := *dayOfMonth
		candidate := time.Date(start.Year(), start.Month(), dom, 0, 0, 0, 0, start.Location())
		if candidate.Before(start) {
			// The target day has already passed this month; advance to next month.
			// time.Month arithmetic handles year roll-over automatically.
			candidate = time.Date(start.Year(), start.Month()+1, dom, 0, 0, 0, 0, start.Location())
		}
		return candidate

	default: // daily, yearly, custom
		return start
	}
}
