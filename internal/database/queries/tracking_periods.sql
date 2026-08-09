-- name: CreateTrackingPeriod :one
INSERT INTO tracking_periods (
    user_id,
    start_date,
    end_date,
    status,
    sequence_number,
    config_start_day,
    config_duration_days,
    config_period_mode,
    is_transition
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetActiveTrackingPeriod :one
SELECT * FROM tracking_periods
WHERE user_id = $1 AND status = 'active';

-- name: GetTrackingPeriodByID :one
SELECT * FROM tracking_periods
WHERE id = $1;

-- name: ListTrackingPeriodsByUser :many
SELECT * FROM tracking_periods
WHERE user_id = $1
ORDER BY sequence_number DESC;

-- name: ClosePeriod :one
UPDATE tracking_periods
SET status = 'closed', closed_at = NOW(), updated_at = NOW()
WHERE id = $1 AND status = 'active'
RETURNING *;

-- name: ListDueActivePeriods :many
-- Active periods whose end_date is strictly before the given date (i.e. over).
SELECT * FROM tracking_periods
WHERE status = 'active' AND end_date < $1
ORDER BY user_id, sequence_number;

-- name: ReshapeTrackingPeriod :one
-- Rewrites the end date and config metadata of an *active* period in place.
--
-- This deliberately breaks domain rule 8 (configuration changes never reshape
-- the active period), so it has exactly one caller: the onboarding carve-out in
-- UserSettingsService.Update, which only fires for a pristine first period with
-- no transactions. Do not reach for it anywhere else.
UPDATE tracking_periods
SET end_date             = sqlc.arg(end_date),
    config_period_mode   = sqlc.arg(config_period_mode),
    config_duration_days = sqlc.arg(config_duration_days),
    is_transition        = sqlc.arg(is_transition),
    updated_at           = NOW()
WHERE id = sqlc.arg(id) AND status = 'active'
RETURNING *;

-- name: GetTrackingPeriodForUser :one
-- Like GetTrackingPeriodByID but scoped to the owner — safe to expose directly.
SELECT * FROM tracking_periods
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);
