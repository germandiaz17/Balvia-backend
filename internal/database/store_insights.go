package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
)

// GetFinalInsights implements Store. Returns the "final" (close-time) insights
// for a period owned by the given user. Returns an empty slice when none exist.
func (s *SQLStore) GetFinalInsights(ctx context.Context, periodID, userID uuid.UUID) ([]sqlc.TrackingPeriodInsight, error) {
	return s.Queries.ListFinalInsightsByPeriod(ctx, sqlc.ListFinalInsightsByPeriodParams{
		TrackingPeriodID: periodID,
		UserID:           userID,
	})
}
