package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
	"github.com/germandiaz17/Balvia-backend/internal/domain"
)

// GetFinalInsights implements Store. Returns the "final" (close-time) insights
// for a period owned by the given user. Returns an empty slice when none exist.
func (s *SQLStore) GetFinalInsights(ctx context.Context, periodID, userID uuid.UUID) ([]sqlc.TrackingPeriodInsight, error) {
	return s.Queries.ListFinalInsightsByPeriod(ctx, sqlc.ListFinalInsightsByPeriodParams{
		TrackingPeriodID: periodID,
		UserID:           userID,
	})
}

// RefreshImmediateDuringInsights recalculates the three "immediate" during
// insights (spending_pace, budget_warning, budget_exceeded) for the user's
// active period. It:
//  1. Loads all data required for the immediate generators.
//  2. Deletes the existing immediate during insights atomically.
//  3. Persists the new drafts.
//
// Called after every transaction mutation (create/update/delete).
// Returns without error if there is no active period.
func (s *SQLStore) RefreshImmediateDuringInsights(ctx context.Context, userID uuid.UUID, periodID uuid.UUID) error {
	return s.execTx(ctx, func(q *sqlc.Queries) error {
		// Load current period (must still be active).
		period, err := q.GetTrackingPeriodByID(ctx, periodID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // period gone — nothing to do
		}
		if err != nil {
			return fmt.Errorf("load period for during insights: %w", err)
		}
		if period.Status != "active" {
			return nil // do not touch closed periods
		}

		// Collect data for generators.
		data, err := collectImmediateDuringData(ctx, q, period, userID)
		if err != nil {
			return err
		}

		// Delete and regenerate only the immediate insight types.
		if err := q.DeleteImmediateDuringInsightsByPeriod(ctx, sqlc.DeleteImmediateDuringInsightsByPeriodParams{
			TrackingPeriodID: periodID,
			UserID:           userID,
		}); err != nil {
			return fmt.Errorf("delete immediate during insights: %w", err)
		}

		drafts := generateImmediateDuringInsights(data)
		return persistInsightDrafts(ctx, q, periodID, userID, drafts)
	})
}

// RefreshAllDuringInsights recalculates ALL seven "during" insights for the
// given active period (used by the lazy trigger on GET /insights for an active
// period). It deletes all existing "during" insights and regenerates them.
// Returns without error if the period is closed or does not exist.
func (s *SQLStore) RefreshAllDuringInsights(ctx context.Context, periodID, userID uuid.UUID) ([]sqlc.TrackingPeriodInsight, error) {
	var result []sqlc.TrackingPeriodInsight

	err := s.execTx(ctx, func(q *sqlc.Queries) error {
		// Load period.
		period, err := q.GetTrackingPeriodByID(ctx, periodID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load period for during insights: %w", err)
		}
		if period.Status != "active" {
			// Closed period: return existing final insights — caller handles routing.
			return nil
		}

		// Collect full dataset.
		data, err := collectFullDuringData(ctx, q, period, userID)
		if err != nil {
			return err
		}

		// Replace all during insights.
		if err := q.DeleteDuringInsightsByPeriod(ctx, sqlc.DeleteDuringInsightsByPeriodParams{
			TrackingPeriodID: periodID,
			UserID:           userID,
		}); err != nil {
			return fmt.Errorf("delete during insights: %w", err)
		}

		drafts := generateLazyDuringInsights(data)
		if err := persistInsightDrafts(ctx, q, periodID, userID, drafts); err != nil {
			return err
		}

		// Return the freshly persisted rows.
		rows, err := q.ListDuringInsightsByPeriod(ctx, sqlc.ListDuringInsightsByPeriodParams{
			TrackingPeriodID: periodID,
			UserID:           userID,
		})
		if err != nil {
			return fmt.Errorf("list during insights after refresh: %w", err)
		}
		result = rows
		return nil
	})

	return result, err
}

// persistInsightDrafts inserts a set of insightDraft values into the DB within
// the caller's transaction.
func persistInsightDrafts(ctx context.Context, q *sqlc.Queries, periodID, userID uuid.UUID, drafts []insightDraft) error {
	for _, draft := range drafts {
		_, err := q.CreateTrackingPeriodInsight(ctx, sqlc.CreateTrackingPeriodInsightParams{
			TrackingPeriodID:  periodID,
			UserID:            userID,
			InsightType:       draft.InsightType,
			CalculationPhase:  draft.CalculationPhase,
			Severity:          draft.Severity,
			Title:             draft.Title,
			Message:           draft.Message,
			ActionLabel:       draft.ActionLabel,
			ActionTarget:      draft.ActionTarget,
			Data:              draft.Data,
			RelatedCategoryID: draft.RelatedCategoryID,
			RelatedAccountID:  draft.RelatedAccountID,
			RelatedGoalID:     draft.RelatedGoalID,
			ValidUntil:        neverExpires(),
		})
		if err != nil {
			return fmt.Errorf("create during insight %s: %w", draft.InsightType, err)
		}
	}
	return nil
}

// collectImmediateDuringData gathers the minimal dataset required for the three
// immediate insight generators (spending_pace, budget_warning, budget_exceeded).
func collectImmediateDuringData(
	ctx context.Context,
	q *sqlc.Queries,
	period sqlc.TrackingPeriod,
	userID uuid.UUID,
) (duringPeriodData, error) {
	today := period.StartDate.Time // fallback; overridden below
	_ = today

	data := duringPeriodData{Period: period}

	// Use server-side now in America/Bogota to match the application's "today".
	data.Today = bogotaToday()

	totals, err := q.SummarizePeriodTotals(ctx, period.ID)
	if err != nil {
		return data, fmt.Errorf("summarize totals: %w", err)
	}
	data.Totals = totals

	expByCategory, err := q.ExpenseByCategory(ctx, period.ID)
	if err != nil {
		return data, fmt.Errorf("expense by category: %w", err)
	}
	data.ExpByCategory = expByCategory

	budgets, err := q.ListBudgetsByPeriod(ctx, period.ID)
	if err != nil {
		return data, fmt.Errorf("list budgets: %w", err)
	}
	data.Budgets = budgets

	return data, nil
}

// collectFullDuringData extends collectImmediateDuringData with the heavier
// queries needed by the lazy generators.
func collectFullDuringData(
	ctx context.Context,
	q *sqlc.Queries,
	period sqlc.TrackingPeriod,
	userID uuid.UUID,
) (duringPeriodData, error) {
	data, err := collectImmediateDuringData(ctx, q, period, userID)
	if err != nil {
		return data, err
	}

	// Goals (for goal_progress_alert).
	goals, err := q.ListSavingsGoalsForUser(ctx, userID)
	if err != nil {
		return data, fmt.Errorf("list savings goals: %w", err)
	}
	// Filter out soft-deleted goals.
	active := goals[:0]
	for _, g := range goals {
		if !g.DeletedAt.Valid {
			active = append(active, g)
		}
	}
	data.Goals = active

	// Goal contributions total for this period.
	contribs, err := q.GoalContributionsTotalForPeriod(ctx, period.ID)
	if err != nil {
		return data, fmt.Errorf("goal contributions: %w", err)
	}
	data.GoalContribs = contribs

	// Previous period summary (for vs_previous_partial).
	prevPeriod, err := q.GetPreviousTrackingPeriod(ctx, sqlc.GetPreviousTrackingPeriodParams{
		UserID:         userID,
		SequenceNumber: period.SequenceNumber,
	})
	switch {
	case err == nil:
		prevSummary, err := q.GetTrackingPeriodSummaryForPeriod(ctx, prevPeriod.ID)
		switch {
		case err == nil:
			data.PrevSummary = &prevSummary
		case !isNoRows(err):
			return data, fmt.Errorf("previous period summary: %w", err)
		}
	case !isNoRows(err):
		return data, fmt.Errorf("previous period: %w", err)
	}

	return data, nil
}

// isNoRows returns true for pgx.ErrNoRows — a helper to keep the callers tidy.
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
