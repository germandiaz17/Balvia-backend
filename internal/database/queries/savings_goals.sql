-- name: CreateSavingsGoal :one
INSERT INTO savings_goals (
    user_id, name, description, icon, color,
    target_amount, currency, start_date, target_date, linked_account_id
) VALUES (
    sqlc.arg(user_id),
    sqlc.arg(name),
    sqlc.narg(description),
    sqlc.narg(icon),
    sqlc.narg(color),
    sqlc.arg(target_amount),
    sqlc.arg(currency),
    sqlc.arg(start_date),
    sqlc.arg(target_date),
    sqlc.narg(linked_account_id)
)
RETURNING *;

-- name: GetSavingsGoal :one
SELECT * FROM savings_goals
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND deleted_at IS NULL;

-- name: ListSavingsGoalsForUser :many
SELECT * FROM savings_goals
WHERE user_id = sqlc.arg(user_id) AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: UpdateSavingsGoal :one
UPDATE savings_goals
SET name             = sqlc.arg(name),
    description      = sqlc.narg(description),
    icon             = sqlc.narg(icon),
    color            = sqlc.narg(color),
    target_amount    = sqlc.arg(target_amount),
    target_date      = sqlc.arg(target_date),
    status           = sqlc.arg(status),
    linked_account_id = sqlc.narg(linked_account_id),
    updated_at       = NOW()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteSavingsGoal :one
UPDATE savings_goals
SET deleted_at = NOW(), updated_at = NOW()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND deleted_at IS NULL
RETURNING id;

-- name: CreateSavingsGoalContribution :one
INSERT INTO savings_goal_contributions (
    savings_goal_id, user_id, tracking_period_id, amount, contribution_date, notes
) VALUES (
    sqlc.arg(savings_goal_id),
    sqlc.arg(user_id),
    sqlc.arg(tracking_period_id),
    sqlc.arg(amount),
    sqlc.arg(contribution_date),
    sqlc.narg(notes)
)
RETURNING *;

-- name: ListContributionsForGoal :many
SELECT * FROM savings_goal_contributions
WHERE savings_goal_id = sqlc.arg(savings_goal_id) AND user_id = sqlc.arg(user_id)
ORDER BY contribution_date DESC, created_at DESC;

-- name: ApplyGoalContribution :one
-- Atomically adds the contribution amount to current_amount. If the resulting
-- current_amount meets or exceeds target_amount and the goal is not already
-- achieved, marks it achieved and sets achieved_at = NOW().
-- NOTE: CASE expressions reference OLD column values (pre-update), so
-- current_amount + $amount correctly computes the post-update total.
UPDATE savings_goals
SET current_amount = current_amount + sqlc.arg(amount),
    status = CASE
        WHEN current_amount + sqlc.arg(amount) >= target_amount AND status != 'achieved'
        THEN 'achieved'
        ELSE status
    END,
    achieved_at = CASE
        WHEN current_amount + sqlc.arg(amount) >= target_amount AND status != 'achieved'
        THEN NOW()
        ELSE achieved_at
    END,
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND deleted_at IS NULL
RETURNING *;
