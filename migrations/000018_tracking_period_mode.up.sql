-- Tracking period mode: rolling (the original behaviour) vs calendar month.
--
-- A calendar month is 28-31 days long, so chk_duration_range already accepts
-- one as-is. What does not fit is the *transition* period created when a user
-- switches from rolling to calendar mode: the stretch between the end of the
-- current period and the 1st of a month is 15-45 days. Those rows carry
-- is_transition = true and get a wider bound.

ALTER TABLE user_settings
    ADD COLUMN tracking_period_mode VARCHAR(20) NOT NULL DEFAULT 'rolling';

ALTER TABLE user_settings
    ADD CONSTRAINT chk_tracking_period_mode
    CHECK (tracking_period_mode IN ('rolling', 'calendar_month'));

-- Stamped on each period as metadata, like config_start_day/config_duration_days.
ALTER TABLE tracking_periods
    ADD COLUMN config_period_mode VARCHAR(20) NOT NULL DEFAULT 'rolling';

ALTER TABLE tracking_periods
    ADD CONSTRAINT chk_config_period_mode
    CHECK (config_period_mode IN ('rolling', 'calendar_month'));

ALTER TABLE tracking_periods
    ADD COLUMN is_transition BOOLEAN NOT NULL DEFAULT false;

-- Widen the duration rule for transition periods only. This never fails on
-- existing rows: every one of them has is_transition = false and already
-- satisfies the 28-31 branch.
ALTER TABLE tracking_periods DROP CONSTRAINT chk_duration_range;

ALTER TABLE tracking_periods
    ADD CONSTRAINT chk_duration_range CHECK (
        (NOT is_transition AND (end_date - start_date + 1) BETWEEN 28 AND 31)
        OR (is_transition AND (end_date - start_date + 1) BETWEEN 1 AND 62)
    );
