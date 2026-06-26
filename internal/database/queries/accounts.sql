-- name: CreateAccount :one
INSERT INTO accounts (
    user_id, name, account_type, currency,
    initial_balance, current_balance, icon, color, display_order
) VALUES (
    sqlc.arg(user_id),
    sqlc.arg(name),
    sqlc.arg(account_type),
    sqlc.arg(currency),
    sqlc.arg(initial_balance),
    sqlc.arg(initial_balance), -- current starts equal to initial
    sqlc.narg(icon),
    sqlc.narg(color),
    sqlc.arg(display_order)
)
RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: ListAccounts :many
SELECT * FROM accounts
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY display_order, created_at;

-- name: UpdateAccount :one
UPDATE accounts
SET name = sqlc.arg(name),
    account_type = sqlc.arg(account_type),
    icon = sqlc.narg(icon),
    color = sqlc.narg(color),
    display_order = sqlc.arg(display_order),
    is_archived = sqlc.arg(is_archived),
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteAccount :one
UPDATE accounts
SET deleted_at = NOW(), updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
RETURNING id;

-- name: AdjustAccountBalance :exec
UPDATE accounts
SET current_balance = current_balance + sqlc.arg(delta),
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND deleted_at IS NULL;
