-- SAVINGS_GOALS
CREATE TABLE savings_goals (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name                    VARCHAR(150) NOT NULL,
    description             TEXT,
    icon                    VARCHAR(50),
    color                   CHAR(7),
    target_amount           NUMERIC(15,2) NOT NULL,
    current_amount          NUMERIC(15,2) NOT NULL DEFAULT 0,
    currency                CHAR(3) NOT NULL DEFAULT 'COP',
    start_date              DATE NOT NULL,
    target_date             DATE NOT NULL,
    status                  VARCHAR(20) NOT NULL DEFAULT 'active',
    linked_account_id       UUID REFERENCES accounts(id) ON DELETE SET NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    achieved_at             TIMESTAMPTZ,
    deleted_at              TIMESTAMPTZ,

    CONSTRAINT chk_goal_status       CHECK (status IN ('active','achieved','abandoned','paused')),
    CONSTRAINT chk_goal_target_positive CHECK (target_amount > 0),
    CONSTRAINT chk_goal_date_order   CHECK (target_date > start_date)
);

CREATE INDEX idx_savings_goals_user ON savings_goals(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_savings_goals_active ON savings_goals(user_id, status) WHERE status = 'active' AND deleted_at IS NULL;
CREATE INDEX idx_savings_goals_dates ON savings_goals(user_id, start_date, target_date) WHERE deleted_at IS NULL;

-- SAVINGS_GOAL_CONTRIBUTIONS
CREATE TABLE savings_goal_contributions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    savings_goal_id         UUID NOT NULL REFERENCES savings_goals(id) ON DELETE CASCADE,
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tracking_period_id      UUID NOT NULL REFERENCES tracking_periods(id) ON DELETE RESTRICT,
    transaction_id          UUID REFERENCES transactions(id) ON DELETE SET NULL,
    amount                  NUMERIC(15,2) NOT NULL,
    contribution_date       DATE NOT NULL,
    notes                   TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_contribution_amount_positive CHECK (amount > 0)
);

CREATE INDEX idx_goal_contrib_goal ON savings_goal_contributions(savings_goal_id);
CREATE INDEX idx_goal_contrib_period ON savings_goal_contributions(tracking_period_id);
CREATE INDEX idx_goal_contrib_user ON savings_goal_contributions(user_id);
