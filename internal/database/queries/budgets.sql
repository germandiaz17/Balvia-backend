-- name: ListBudgetsByPeriod :many
-- Internal: used when copying budgets onto a freshly generated period.
SELECT * FROM budgets
WHERE tracking_period_id = $1
ORDER BY created_at;

-- name: ListBudgetsForUser :many
SELECT * FROM budgets
WHERE user_id = sqlc.arg(user_id) AND tracking_period_id = sqlc.arg(tracking_period_id)
ORDER BY created_at;

-- name: GetBudget :one
SELECT * FROM budgets
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

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

-- name: UpdateBudget :one
UPDATE budgets
SET category_id = sqlc.narg(category_id),
    amount = sqlc.arg(amount),
    currency = sqlc.arg(currency),
    alert_threshold_warning = sqlc.arg(alert_threshold_warning),
    alert_threshold_critical = sqlc.arg(alert_threshold_critical),
    notes = sqlc.narg(notes),
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: DeleteBudget :one
DELETE FROM budgets
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING id;
