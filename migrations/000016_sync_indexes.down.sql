DROP INDEX IF EXISTS idx_tracking_periods_sync;
DROP INDEX IF EXISTS idx_recurring_sync;
DROP INDEX IF EXISTS idx_goal_contributions_sync;
DROP INDEX IF EXISTS idx_savings_goals_sync;
DROP INDEX IF EXISTS idx_budgets_sync;
DROP INDEX IF EXISTS idx_categories_sync;
DROP INDEX IF EXISTS idx_accounts_sync;
DROP INDEX IF EXISTS idx_txn_sync;

ALTER TABLE savings_goal_contributions
    DROP COLUMN IF EXISTS updated_at;
