CREATE TABLE recurring_transactions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id              UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    category_id             UUID REFERENCES categories(id) ON DELETE SET NULL,
    name                    VARCHAR(150) NOT NULL,
    transaction_type        VARCHAR(20) NOT NULL,
    amount                  NUMERIC(15,2) NOT NULL,
    currency                CHAR(3) NOT NULL DEFAULT 'COP',
    description             VARCHAR(255),
    frequency               VARCHAR(20) NOT NULL,
    custom_interval_days    INTEGER,
    day_of_month            SMALLINT,
    day_of_week             SMALLINT,
    start_date              DATE NOT NULL,
    end_date                DATE,
    last_generated_date     DATE,
    next_due_date           DATE,
    is_active               BOOLEAN NOT NULL DEFAULT TRUE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ,

    CONSTRAINT chk_rec_transaction_type CHECK (transaction_type IN ('income','expense')),
    CONSTRAINT chk_rec_amount_positive  CHECK (amount > 0),
    CONSTRAINT chk_rec_frequency        CHECK (frequency IN ('daily','weekly','biweekly','monthly','yearly','custom')),
    CONSTRAINT chk_rec_custom_interval  CHECK (
        (frequency = 'custom' AND custom_interval_days IS NOT NULL AND custom_interval_days > 0) OR
        (frequency <> 'custom')
    ),
    CONSTRAINT chk_rec_day_of_month CHECK (day_of_month IS NULL OR day_of_month BETWEEN 1 AND 31),
    CONSTRAINT chk_rec_day_of_week  CHECK (day_of_week IS NULL OR day_of_week BETWEEN 0 AND 6)
);

CREATE INDEX idx_recurring_user ON recurring_transactions(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_recurring_active ON recurring_transactions(user_id, next_due_date) WHERE is_active = TRUE AND deleted_at IS NULL;
CREATE INDEX idx_recurring_due ON recurring_transactions(next_due_date) WHERE is_active = TRUE AND deleted_at IS NULL;
