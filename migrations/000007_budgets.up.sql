CREATE TABLE budgets (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tracking_period_id      UUID NOT NULL REFERENCES tracking_periods(id) ON DELETE CASCADE,
    category_id             UUID REFERENCES categories(id) ON DELETE CASCADE,
    amount                  NUMERIC(15,2) NOT NULL,
    currency                CHAR(3) NOT NULL DEFAULT 'COP',
    alert_threshold_warning NUMERIC(5,2) NOT NULL DEFAULT 80.00,
    alert_threshold_critical NUMERIC(5,2) NOT NULL DEFAULT 100.00,
    notes                   TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_budget_amount_positive CHECK (amount > 0),
    CONSTRAINT uq_budget_per_period_category UNIQUE (tracking_period_id, category_id)
);

CREATE INDEX idx_budgets_user_id ON budgets(user_id);
CREATE INDEX idx_budgets_tracking_period ON budgets(tracking_period_id);
CREATE INDEX idx_budgets_category ON budgets(category_id);
