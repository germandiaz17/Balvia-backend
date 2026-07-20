-- Queries used exclusively by the sync-delta endpoints (GET /sync/pull, POST /sync/push).
-- All pull queries share a common pattern:
--   WHERE user_id = $user AND updated_at > $since
-- The caller passes a UTC timestamp; rows modified (created, updated, or
-- soft-deleted) after that timestamp are returned so the client can apply
-- the delta to its local Drift database.

-- name: SyncPullTransactions :many
-- Includes soft-deleted rows (deleted_at IS NOT NULL) because the client must
-- know about deletions to remove them from its local copy.
SELECT * FROM transactions
WHERE user_id = sqlc.arg(user_id)
  AND updated_at > sqlc.arg(since)
ORDER BY updated_at ASC, id ASC
LIMIT sqlc.arg(page_size);

-- name: SyncPullAccounts :many
SELECT * FROM accounts
WHERE user_id = sqlc.arg(user_id)
  AND updated_at > sqlc.arg(since)
ORDER BY updated_at ASC, id ASC
LIMIT sqlc.arg(page_size);

-- name: SyncPullCategories :many
-- User-owned categories plus the shared system set. System categories are NOT
-- embedded in the mobile app — the local DB (which the overlay bubble reads)
-- only knows what sync delivers, so they must come down the wire too.
SELECT * FROM categories
WHERE (user_id = sqlc.arg(user_id) OR is_system = TRUE)
  AND updated_at > sqlc.arg(since)
ORDER BY updated_at ASC, id ASC
LIMIT sqlc.arg(page_size);

-- name: SyncPullBudgets :many
-- Budgets have no deleted_at (hard delete). When a budget is deleted the row
-- disappears; the client must request a full re-sync if it detects a gap.
-- For now we return all budgets updated since $since.
SELECT * FROM budgets
WHERE user_id = sqlc.arg(user_id)
  AND updated_at > sqlc.arg(since)
ORDER BY updated_at ASC, id ASC
LIMIT sqlc.arg(page_size);

-- name: SyncPullSavingsGoals :many
SELECT * FROM savings_goals
WHERE user_id = sqlc.arg(user_id)
  AND updated_at > sqlc.arg(since)
ORDER BY updated_at ASC, id ASC
LIMIT sqlc.arg(page_size);

-- name: SyncPullGoalContributions :many
SELECT * FROM savings_goal_contributions
WHERE user_id = sqlc.arg(user_id)
  AND updated_at > sqlc.arg(since)
ORDER BY updated_at ASC, id ASC
LIMIT sqlc.arg(page_size);

-- name: SyncPullRecurringTransactions :many
SELECT * FROM recurring_transactions
WHERE user_id = sqlc.arg(user_id)
  AND updated_at > sqlc.arg(since)
ORDER BY updated_at ASC, id ASC
LIMIT sqlc.arg(page_size);

-- name: SyncPullTrackingPeriods :many
SELECT * FROM tracking_periods
WHERE user_id = sqlc.arg(user_id)
  AND updated_at > sqlc.arg(since)
ORDER BY updated_at ASC, id ASC
LIMIT sqlc.arg(page_size);

-- name: GetTransactionByClientID :one
-- Used by push to detect duplicates already committed by a previous push.
SELECT * FROM transactions
WHERE user_id = sqlc.arg(user_id)
  AND client_id = sqlc.arg(client_id)
  AND deleted_at IS NULL
LIMIT 1;
