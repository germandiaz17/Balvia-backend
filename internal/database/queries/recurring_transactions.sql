-- name: CreateRecurringTransaction :one
INSERT INTO recurring_transactions (
    user_id, account_id, category_id, name, transaction_type, amount, currency,
    description, frequency, custom_interval_days, day_of_month, day_of_week,
    start_date, end_date, next_due_date, is_active
) VALUES (
    sqlc.arg(user_id),
    sqlc.arg(account_id),
    sqlc.narg(category_id),
    sqlc.arg(name),
    sqlc.arg(transaction_type),
    sqlc.arg(amount),
    sqlc.arg(currency),
    sqlc.narg(description),
    sqlc.arg(frequency),
    sqlc.narg(custom_interval_days),
    sqlc.narg(day_of_month),
    sqlc.narg(day_of_week),
    sqlc.arg(start_date),
    sqlc.narg(end_date),
    sqlc.narg(next_due_date),
    sqlc.arg(is_active)
)
RETURNING *;

-- name: ListRecurringTransactionsForUser :many
SELECT * FROM recurring_transactions
WHERE user_id = sqlc.arg(user_id) AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: GetRecurringTransaction :one
SELECT * FROM recurring_transactions
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND deleted_at IS NULL;

-- name: UpdateRecurringTransaction :one
UPDATE recurring_transactions
SET account_id          = sqlc.arg(account_id),
    category_id         = sqlc.narg(category_id),
    name                = sqlc.arg(name),
    transaction_type    = sqlc.arg(transaction_type),
    amount              = sqlc.arg(amount),
    currency            = sqlc.arg(currency),
    description         = sqlc.narg(description),
    frequency           = sqlc.arg(frequency),
    custom_interval_days = sqlc.narg(custom_interval_days),
    day_of_month        = sqlc.narg(day_of_month),
    day_of_week         = sqlc.narg(day_of_week),
    start_date          = sqlc.arg(start_date),
    end_date            = sqlc.narg(end_date),
    next_due_date       = sqlc.narg(next_due_date),
    is_active           = sqlc.arg(is_active),
    updated_at          = NOW()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteRecurringTransaction :one
UPDATE recurring_transactions
SET deleted_at = NOW(), updated_at = NOW()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND deleted_at IS NULL
RETURNING id;
