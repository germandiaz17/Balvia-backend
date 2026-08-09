-- NOTE: this rollback fails if any transition period outside 28-31 days already
-- exists, because restoring the old constraint would reject those rows. That is
-- deliberate — reshaping or deleting a user's real periods to make a rollback
-- succeed would lose data. Resolve them by hand first.

ALTER TABLE tracking_periods DROP CONSTRAINT chk_duration_range;

ALTER TABLE tracking_periods
    ADD CONSTRAINT chk_duration_range
    CHECK ((end_date - start_date + 1) BETWEEN 28 AND 31);

ALTER TABLE tracking_periods DROP COLUMN IF EXISTS is_transition;
ALTER TABLE tracking_periods DROP COLUMN IF EXISTS config_period_mode;
ALTER TABLE user_settings DROP COLUMN IF EXISTS tracking_period_mode;
