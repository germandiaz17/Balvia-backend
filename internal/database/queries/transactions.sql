-- name: CreateTransaction :one
INSERT INTO transactions (
    user_id,
    tracking_period_id,
    account_id,
    category_id,
    transaction_type,
    amount,
    currency,
    description,
    notes,
    transaction_date,
    transfer_account_id,
    client_id
) VALUES (
    sqlc.arg(user_id),
    sqlc.arg(tracking_period_id),
    sqlc.arg(account_id),
    sqlc.narg(category_id),
    sqlc.arg(transaction_type),
    sqlc.arg(amount),
    sqlc.arg(currency),
    sqlc.narg(description),
    sqlc.narg(notes),
    sqlc.arg(transaction_date),
    sqlc.narg(transfer_account_id),
    sqlc.narg(client_id)
)
RETURNING *;

-- name: GetTransaction :one
SELECT * FROM transactions
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: ListTransactionsByPeriod :many
SELECT * FROM transactions
WHERE user_id = $1 AND tracking_period_id = $2 AND deleted_at IS NULL
ORDER BY transaction_date DESC, created_at DESC;

-- name: SoftDeleteTransaction :one
UPDATE transactions
SET deleted_at = NOW(), updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: UpdateTransaction :one
-- tracking_period_id is intentionally NOT updatable (a transaction stays in its
-- period). The validate_transaction_period trigger re-checks date/period here.
UPDATE transactions
SET account_id = sqlc.arg(account_id),
    category_id = sqlc.narg(category_id),
    transaction_type = sqlc.arg(transaction_type),
    amount = sqlc.arg(amount),
    currency = sqlc.arg(currency),
    description = sqlc.narg(description),
    notes = sqlc.narg(notes),
    transaction_date = sqlc.arg(transaction_date),
    transfer_account_id = sqlc.narg(transfer_account_id),
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND deleted_at IS NULL
RETURNING *;
