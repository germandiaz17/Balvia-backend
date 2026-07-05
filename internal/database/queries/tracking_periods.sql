-- name: CreateTrackingPeriod :one
INSERT INTO tracking_periods (
    user_id,
    start_date,
    end_date,
    status,
    sequence_number,
    config_start_day,
    config_duration_days
) VALUES ($1, $2, $3, $4, $5, $6, $7)
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

-- name: GetTrackingPeriodForUser :one
-- Like GetTrackingPeriodByID but scoped to the owner — safe to expose directly.
SELECT * FROM tracking_periods
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);
