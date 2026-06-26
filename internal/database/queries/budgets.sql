-- name: ListBudgetsByPeriod :many
SELECT * FROM budgets
WHERE tracking_period_id = $1
ORDER BY created_at;

-- name: CreateBudget :one
INSERT INTO budgets (
    user_id, tracking_period_id, category_id, amount, currency,
    alert_threshold_warning, alert_threshold_critical, notes
) VALUES (
    sqlc.arg(user_id),
    sqlc.arg(tracking_period_id),
    sqlc.narg(category_id),
    sqlc.arg(amount),
    sqlc.arg(currency),
    sqlc.arg(alert_threshold_warning),
    sqlc.arg(alert_threshold_critical),
    sqlc.narg(notes)
)
RETURNING *;
