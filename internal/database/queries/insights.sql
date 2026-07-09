-- name: CreateTrackingPeriodInsight :one
INSERT INTO tracking_period_insights (
    tracking_period_id,
    user_id,
    insight_type,
    calculation_phase,
    severity,
    title,
    message,
    action_label,
    action_target,
    data,
    related_category_id,
    related_account_id,
    related_goal_id,
    valid_from,
    valid_until
) VALUES (
    sqlc.arg(tracking_period_id),
    sqlc.arg(user_id),
    sqlc.arg(insight_type),
    sqlc.arg(calculation_phase),
    sqlc.arg(severity),
    sqlc.arg(title),
    sqlc.arg(message),
    sqlc.narg(action_label),
    sqlc.narg(action_target),
    sqlc.arg(data),
    sqlc.narg(related_category_id),
    sqlc.narg(related_account_id),
    sqlc.narg(related_goal_id),
    NOW(),
    sqlc.narg(valid_until)
)
RETURNING *;

-- name: ListInsightsByPeriod :many
-- Returns all non-dismissed insights for a period, newest first.
SELECT * FROM tracking_period_insights
WHERE tracking_period_id = sqlc.arg(tracking_period_id)
  AND user_id = sqlc.arg(user_id)
  AND is_dismissed = FALSE
ORDER BY created_at DESC;

-- name: ListFinalInsightsByPeriod :many
-- Returns only the "final" (close-time) insights for a period.
SELECT * FROM tracking_period_insights
WHERE tracking_period_id = sqlc.arg(tracking_period_id)
  AND user_id = sqlc.arg(user_id)
  AND calculation_phase = 'final'
  AND is_dismissed = FALSE
ORDER BY created_at DESC;

-- name: CountFinalInsightsByPeriod :one
-- Used to detect whether final insights were already generated (idempotency).
SELECT COUNT(*)::int AS count
FROM tracking_period_insights
WHERE tracking_period_id = $1
  AND calculation_phase = 'final';

-- name: ListDuringInsightsByPeriod :many
-- Returns only the "during" (in-progress) insights for an active period, newest first.
SELECT * FROM tracking_period_insights
WHERE tracking_period_id = sqlc.arg(tracking_period_id)
  AND user_id = sqlc.arg(user_id)
  AND calculation_phase = 'during'
  AND is_dismissed = FALSE
ORDER BY created_at DESC;

-- name: DeleteDuringInsightsByPeriod :exec
-- Deletes all "during" insights for a period. Used before regenerating them
-- (replace strategy) and when closing the period (cleanup before final insights).
DELETE FROM tracking_period_insights
WHERE tracking_period_id = sqlc.arg(tracking_period_id)
  AND user_id = sqlc.arg(user_id)
  AND calculation_phase = 'during';

-- name: DeleteImmediateDuringInsightsByPeriod :exec
-- Deletes only the "immediate" during insights (those recalculated on every
-- transaction mutation: spending_pace, budget_warning, budget_exceeded).
-- The "lazy" during insights (ant_expenses_early, unusual_expense,
-- vs_previous_partial, goal_progress_alert) are preserved until the next
-- lazy refresh via GET /tracking-periods/:id/insights.
DELETE FROM tracking_period_insights
WHERE tracking_period_id = sqlc.arg(tracking_period_id)
  AND user_id = sqlc.arg(user_id)
  AND calculation_phase = 'during'
  AND insight_type IN ('spending_pace', 'budget_warning', 'budget_exceeded');
