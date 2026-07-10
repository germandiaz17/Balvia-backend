-- Migration 000016: indexes and column additions to support the sync-delta pull endpoint.
--
-- The pull endpoint (GET /sync/pull?since=<RFC3339>) returns every row whose
-- updated_at (or deleted_at for soft-deleted rows) is strictly after the
-- client-supplied timestamp.  Without indexes these queries do a full-table
-- sequential scan per entity type per user, which becomes expensive once a user
-- accumulates many records.
--
-- Entities covered:
--   transactions               (updated_at, deleted_at already exist)
--   accounts                   (updated_at, deleted_at already exist)
--   categories                 (updated_at, deleted_at already exist; user-owned only)
--   budgets                    (updated_at exists, no deleted_at - hard delete)
--   savings_goals              (updated_at, deleted_at already exist)
--   savings_goal_contributions (only created_at; we add updated_at here)
--   recurring_transactions     (updated_at, deleted_at already exist)
--   tracking_periods           (updated_at already exists; read-only for the client)
--
-- For savings_goal_contributions we add an updated_at column so that the pull
-- query can use a single consistent "updated_at > $since" predicate across all
-- entity types.  Contributions are immutable after creation, so updated_at will
-- always equal created_at; the column is here purely for query uniformity.

-- Add updated_at to savings_goal_contributions (immutable after insert).
ALTER TABLE savings_goal_contributions
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Indexes to speed up the delta-pull queries ("what changed for this user since X?")
-- Pattern: (user_id, updated_at) so Postgres can satisfy user_id equality +
-- updated_at range scan without a full table scan.

CREATE INDEX idx_txn_sync
    ON transactions(user_id, updated_at)
    WHERE deleted_at IS NULL OR updated_at >= deleted_at;

CREATE INDEX idx_accounts_sync
    ON accounts(user_id, updated_at);

CREATE INDEX idx_categories_sync
    ON categories(user_id, updated_at)
    WHERE user_id IS NOT NULL;     -- system categories are never user-specific

CREATE INDEX idx_budgets_sync
    ON budgets(user_id, updated_at);

CREATE INDEX idx_savings_goals_sync
    ON savings_goals(user_id, updated_at);

CREATE INDEX idx_goal_contributions_sync
    ON savings_goal_contributions(user_id, updated_at);

CREATE INDEX idx_recurring_sync
    ON recurring_transactions(user_id, updated_at);

CREATE INDEX idx_tracking_periods_sync
    ON tracking_periods(user_id, updated_at);
