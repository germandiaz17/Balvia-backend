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

-- name: ListDueRecurringTransactions :many
-- Returns all active, non-deleted recurring templates whose next_due_date is
-- on or before the given date. Used by the recurrence engine (scheduler +
-- lazy trigger) to find templates that need materialization.
SELECT * FROM recurring_transactions
WHERE is_active = TRUE
  AND deleted_at IS NULL
  AND next_due_date IS NOT NULL
  AND next_due_date <= sqlc.arg(due_before)::DATE
ORDER BY user_id, next_due_date;

-- name: AdvanceRecurringTransaction :one
-- After materialising one occurrence, update the template:
--   - last_generated_date ← the occurrence date just generated
--   - next_due_date       ← the next scheduled occurrence (computed by Go)
--   - is_active           ← caller sets to FALSE when end_date is exhausted
UPDATE recurring_transactions
SET last_generated_date = sqlc.arg(last_generated_date),
    next_due_date       = sqlc.narg(next_due_date),
    is_active           = sqlc.arg(is_active),
    updated_at          = NOW()
WHERE id = sqlc.arg(id) AND deleted_at IS NULL
RETURNING *;

-- name: HasRecurringTransactionForDate :one
-- Idempotency guard: returns TRUE if a non-deleted transaction already exists
-- for this (template, occurrence_date) pair, avoiding duplicate generation
-- on re-runs. We use occurrence_date (the original scheduled date) rather
-- than transaction_date (which may be clamped to the period bounds).
SELECT EXISTS (
    SELECT 1 FROM transactions
    WHERE recurring_transaction_id = sqlc.arg(recurring_transaction_id)
      AND occurrence_date = sqlc.arg(occurrence_date)::DATE
      AND deleted_at IS NULL
) AS exists;
