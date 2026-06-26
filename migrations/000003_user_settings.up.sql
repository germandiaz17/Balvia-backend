CREATE TABLE user_settings (
    id                              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                         UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    tracking_start_day              SMALLINT NOT NULL DEFAULT 1,
    tracking_duration_days          SMALLINT NOT NULL DEFAULT 30,
    default_currency                CHAR(3) NOT NULL DEFAULT 'COP',
    country_code                    CHAR(2) NOT NULL DEFAULT 'CO',
    locale                          VARCHAR(10) NOT NULL DEFAULT 'es-CO',
    theme                           VARCHAR(20) NOT NULL DEFAULT 'system',
    default_period_view             VARCHAR(20) NOT NULL DEFAULT 'full',
    subscription_tier               VARCHAR(20) NOT NULL DEFAULT 'free',
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_tracking_duration   CHECK (tracking_duration_days BETWEEN 28 AND 31),
    CONSTRAINT chk_tracking_start_day  CHECK (tracking_start_day BETWEEN 1 AND 31),
    CONSTRAINT chk_subscription_tier   CHECK (subscription_tier IN ('free','pro','premium','business')),
    CONSTRAINT chk_theme               CHECK (theme IN ('system','light','dark')),
    CONSTRAINT chk_default_period_view CHECK (default_period_view IN ('full','biweekly','weekly'))
);

CREATE INDEX idx_user_settings_user_id ON user_settings(user_id);
