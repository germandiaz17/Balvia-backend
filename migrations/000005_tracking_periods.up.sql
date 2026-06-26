CREATE TABLE tracking_periods (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    start_date                  DATE NOT NULL,
    end_date                    DATE NOT NULL,
    status                      VARCHAR(20) NOT NULL DEFAULT 'active',
    sequence_number             INTEGER NOT NULL,
    config_start_day            SMALLINT NOT NULL,
    config_duration_days        SMALLINT NOT NULL,
    closed_at                   TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_status            CHECK (status IN ('active','closed')),
    CONSTRAINT chk_date_order        CHECK (end_date >= start_date),
    CONSTRAINT chk_duration_range    CHECK ((end_date - start_date + 1) BETWEEN 28 AND 31),
    CONSTRAINT chk_closed_consistency CHECK (
        (status = 'closed' AND closed_at IS NOT NULL) OR
        (status = 'active' AND closed_at IS NULL)
    ),
    CONSTRAINT uq_user_sequence      UNIQUE (user_id, sequence_number)
);

CREATE UNIQUE INDEX uq_one_active_per_user ON tracking_periods(user_id) WHERE status = 'active';

ALTER TABLE tracking_periods
    ADD CONSTRAINT exclude_overlapping_periods
    EXCLUDE USING gist (
        user_id WITH =,
        daterange(start_date, end_date, '[]') WITH &&
    );

CREATE INDEX idx_tracking_periods_user_id ON tracking_periods(user_id);
CREATE INDEX idx_tracking_periods_user_dates ON tracking_periods(user_id, start_date DESC, end_date DESC);
CREATE INDEX idx_tracking_periods_status ON tracking_periods(status);
