-- TRACKING_PERIOD_SUMMARIES
CREATE TABLE tracking_period_summaries (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tracking_period_id          UUID NOT NULL UNIQUE REFERENCES tracking_periods(id) ON DELETE CASCADE,
    user_id                     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    total_income                NUMERIC(15,2) NOT NULL DEFAULT 0,
    total_expenses              NUMERIC(15,2) NOT NULL DEFAULT 0,
    total_transfers             NUMERIC(15,2) NOT NULL DEFAULT 0,
    net_savings                 NUMERIC(15,2) NOT NULL DEFAULT 0,
    savings_rate                NUMERIC(5,2),

    transaction_count           INTEGER NOT NULL DEFAULT 0,
    expense_transaction_count   INTEGER NOT NULL DEFAULT 0,
    income_transaction_count    INTEGER NOT NULL DEFAULT 0,

    top_expense_category_id     UUID REFERENCES categories(id) ON DELETE SET NULL,
    top_expense_category_amount NUMERIC(15,2),

    expense_by_category         JSONB NOT NULL DEFAULT '[]'::jsonb,
    income_by_category          JSONB NOT NULL DEFAULT '[]'::jsonb,
    expense_by_account          JSONB NOT NULL DEFAULT '[]'::jsonb,
    expense_by_day              JSONB NOT NULL DEFAULT '[]'::jsonb,

    budget_performance          JSONB NOT NULL DEFAULT '[]'::jsonb,
    goal_contributions_total    NUMERIC(15,2) NOT NULL DEFAULT 0,
    vs_previous_period          JSONB,

    calculated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_summaries_user ON tracking_period_summaries(user_id);
CREATE INDEX idx_summaries_period ON tracking_period_summaries(tracking_period_id);

-- TRACKING_PERIOD_INSIGHTS
CREATE TABLE tracking_period_insights (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tracking_period_id      UUID NOT NULL REFERENCES tracking_periods(id) ON DELETE CASCADE,
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    insight_type            VARCHAR(50) NOT NULL,
    calculation_phase       VARCHAR(20) NOT NULL,
    severity                VARCHAR(20) NOT NULL DEFAULT 'info',

    title                   VARCHAR(200) NOT NULL,
    message                 TEXT NOT NULL,
    action_label            VARCHAR(100),
    action_target           VARCHAR(255),

    data                    JSONB,

    related_category_id     UUID REFERENCES categories(id) ON DELETE SET NULL,
    related_account_id      UUID REFERENCES accounts(id) ON DELETE SET NULL,
    related_goal_id         UUID REFERENCES savings_goals(id) ON DELETE SET NULL,

    is_dismissed            BOOLEAN NOT NULL DEFAULT FALSE,
    dismissed_at            TIMESTAMPTZ,

    valid_from              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until             TIMESTAMPTZ,

    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_insight_phase    CHECK (calculation_phase IN ('during','final')),
    CONSTRAINT chk_insight_severity CHECK (severity IN ('info','success','warning','critical'))
);

CREATE INDEX idx_insights_period ON tracking_period_insights(tracking_period_id);
CREATE INDEX idx_insights_user_active ON tracking_period_insights(user_id, calculation_phase) WHERE is_dismissed = FALSE;
CREATE INDEX idx_insights_type ON tracking_period_insights(insight_type);
CREATE INDEX idx_insights_severity ON tracking_period_insights(severity, calculation_phase) WHERE is_dismissed = FALSE;
