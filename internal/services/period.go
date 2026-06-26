package services

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	"github.com/germandiaz17/Balvia-backend/internal/database"
)

// PeriodService closes tracking periods that have reached their end. It is
// driven by the nightly scheduler and as a lazy fallback by other services.
type PeriodService struct {
	store database.Store
	log   zerolog.Logger
	now   func() time.Time
}

func NewPeriodService(store database.Store, log zerolog.Logger) *PeriodService {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		loc = time.UTC
	}
	return &PeriodService{
		store: store,
		log:   log,
		now:   func() time.Time { return time.Now().In(loc) },
	}
}

// CloseDuePeriods closes every active period whose end_date is before today,
// generating the next period and snapshotting a summary for each. A failure on
// one period is logged and skipped so the rest still close. Returns how many
// periods were closed.
func (s *PeriodService) CloseDuePeriods(ctx context.Context) (int, error) {
	today := dateOnly(s.now())
	due, err := s.store.ListDueActivePeriods(ctx, pgtype.Date{Time: today, Valid: true})
	if err != nil {
		return 0, err
	}

	closed := 0
	for _, p := range due {
		if _, err := s.store.ClosePeriodTx(ctx, p.ID, p.UserID); err != nil {
			s.log.Error().Err(err).
				Str("period_id", p.ID.String()).
				Str("user_id", p.UserID.String()).
				Msg("failed to close due period")
			continue
		}
		closed++
	}

	return closed, nil
}
