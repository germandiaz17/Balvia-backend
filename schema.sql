-- ============================================================================
-- BALVIA - Schema de Base de Datos (Data Operativa)
-- ============================================================================
-- PostgreSQL 15+ (Neon)
-- Módulo: Operativo (transacciones, seguimientos, presupuestos, metas)
-- Módulo de Auth: se diseñará por separado (placeholder de users incluido)
--
-- Convenciones:
--   - UUIDs como PK (mejor para sync offline)
--   - NUMERIC(15,2) para todo lo monetario
--   - TIMESTAMPTZ para fechas con timezone
--   - Soft deletes con deleted_at donde aplique
--   - snake_case para tablas y columnas
-- ============================================================================

-- ----------------------------------------------------------------------------
-- 0. Extensiones requeridas
-- ----------------------------------------------------------------------------
CREATE EXTENSION IF NOT EXISTS "pgcrypto";   -- para gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS "btree_gist"; -- para EXCLUDE constraints (validación de overlaps)


-- ============================================================================
-- 1. USERS (PLACEHOLDER - se expande en módulo de Auth)
-- ============================================================================
-- Tabla mínima para que las FKs funcionen. El módulo de Auth completará
-- email, password_hash, biometric_enabled, etc.

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           VARCHAR(255) UNIQUE NOT NULL,
    full_name       VARCHAR(255),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);


-- ============================================================================
-- 2. USER_SETTINGS — Configuración del usuario para seguimientos
-- ============================================================================
-- Define cómo se generan los seguimientos para este usuario.

CREATE TABLE user_settings (
    id                              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                         UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,

    -- Configuración del seguimiento
    tracking_start_day              SMALLINT NOT NULL DEFAULT 1,
    tracking_duration_days          SMALLINT NOT NULL DEFAULT 30,

    -- Preferencias generales
    default_currency                CHAR(3) NOT NULL DEFAULT 'COP',
    country_code                    CHAR(2) NOT NULL DEFAULT 'CO',
    locale                          VARCHAR(10) NOT NULL DEFAULT 'es-CO',
    theme                           VARCHAR(20) NOT NULL DEFAULT 'system',  -- system | light | dark

    -- Preferencias de visualización
    default_period_view             VARCHAR(20) NOT NULL DEFAULT 'full',    -- full | biweekly | weekly

    -- Subscripción (se manejará a profundidad en módulo de monetización)
    subscription_tier               VARCHAR(20) NOT NULL DEFAULT 'free',    -- free | pro | premium | business

    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_tracking_duration   CHECK (tracking_duration_days BETWEEN 28 AND 31),
    CONSTRAINT chk_tracking_start_day  CHECK (tracking_start_day BETWEEN 1 AND 31),
    CONSTRAINT chk_subscription_tier   CHECK (subscription_tier IN ('free','pro','premium','business')),
    CONSTRAINT chk_theme               CHECK (theme IN ('system','light','dark')),
    CONSTRAINT chk_default_period_view CHECK (default_period_view IN ('full','biweekly','weekly'))
);

CREATE INDEX idx_user_settings_user_id ON user_settings(user_id);


-- ============================================================================
-- 3. ACCOUNTS — Cuentas financieras del usuario
-- ============================================================================
-- NO atadas a seguimiento. Son globales y persisten entre seguimientos.

CREATE TABLE accounts (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    name                VARCHAR(100) NOT NULL,
    account_type        VARCHAR(30) NOT NULL,     -- cash | checking | savings | credit_card | investment | other
    currency            CHAR(3) NOT NULL DEFAULT 'COP',

    initial_balance     NUMERIC(15,2) NOT NULL DEFAULT 0,
    current_balance     NUMERIC(15,2) NOT NULL DEFAULT 0,

    icon                VARCHAR(50),
    color               CHAR(7),                  -- hex: #RRGGBB

    is_archived         BOOLEAN NOT NULL DEFAULT FALSE,
    display_order       INTEGER NOT NULL DEFAULT 0,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ,

    CONSTRAINT chk_account_type CHECK (account_type IN ('cash','checking','savings','credit_card','investment','other'))
);

CREATE INDEX idx_accounts_user_id ON accounts(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_accounts_user_active ON accounts(user_id) WHERE deleted_at IS NULL AND is_archived = FALSE;


-- ============================================================================
-- 4. CATEGORIES — Categorías de transacciones
-- ============================================================================
-- NO atadas a seguimiento. Globales. Pueden ser de sistema o personalizadas.

CREATE TABLE categories (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID REFERENCES users(id) ON DELETE CASCADE,  -- NULL = categoría del sistema
    parent_id       UUID REFERENCES categories(id) ON DELETE SET NULL,

    name            VARCHAR(100) NOT NULL,
    category_type   VARCHAR(20) NOT NULL,   -- income | expense | transfer
    icon            VARCHAR(50),
    color           CHAR(7),

    is_system       BOOLEAN NOT NULL DEFAULT FALSE,
    display_order   INTEGER NOT NULL DEFAULT 0,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,

    CONSTRAINT chk_category_type CHECK (category_type IN ('income','expense','transfer')),
    CONSTRAINT chk_system_no_user CHECK (
        (is_system = TRUE AND user_id IS NULL) OR
        (is_system = FALSE AND user_id IS NOT NULL)
    )
);

CREATE INDEX idx_categories_user_id ON categories(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_categories_parent_id ON categories(parent_id);
CREATE INDEX idx_categories_system ON categories(is_system) WHERE is_system = TRUE;


-- ============================================================================
-- 5. TRACKING_PERIODS — Seguimientos (raíz temporal del sistema)
-- ============================================================================
-- Toda la data operativa orbita alrededor de esta tabla.

CREATE TABLE tracking_periods (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    start_date                  DATE NOT NULL,
    end_date                    DATE NOT NULL,

    status                      VARCHAR(20) NOT NULL DEFAULT 'active',  -- active | closed
    sequence_number             INTEGER NOT NULL,                       -- 1, 2, 3... por usuario

    -- Snapshot de configuración usada para crear este seguimiento
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

-- Solo puede haber UN seguimiento activo por usuario
CREATE UNIQUE INDEX uq_one_active_per_user ON tracking_periods(user_id) WHERE status = 'active';

-- Los seguimientos del mismo usuario NO pueden traslaparse en fechas
ALTER TABLE tracking_periods
    ADD CONSTRAINT exclude_overlapping_periods
    EXCLUDE USING gist (
        user_id WITH =,
        daterange(start_date, end_date, '[]') WITH &&
    );

CREATE INDEX idx_tracking_periods_user_id ON tracking_periods(user_id);
CREATE INDEX idx_tracking_periods_user_dates ON tracking_periods(user_id, start_date DESC, end_date DESC);
CREATE INDEX idx_tracking_periods_status ON tracking_periods(status);


-- ============================================================================
-- 6. TRANSACTIONS — Movimientos financieros
-- ============================================================================
-- Cada transacción pertenece a EXACTAMENTE UN seguimiento.
-- Fecha de la transacción DEBE estar dentro del rango del seguimiento.

CREATE TABLE transactions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tracking_period_id      UUID NOT NULL REFERENCES tracking_periods(id) ON DELETE RESTRICT,
    account_id              UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    category_id             UUID REFERENCES categories(id) ON DELETE SET NULL,

    transaction_type        VARCHAR(20) NOT NULL,   -- income | expense | transfer
    amount                  NUMERIC(15,2) NOT NULL,
    currency                CHAR(3) NOT NULL DEFAULT 'COP',

    description             VARCHAR(255),
    notes                   TEXT,
    transaction_date        DATE NOT NULL,

    -- Para transferencias entre cuentas
    transfer_account_id     UUID REFERENCES accounts(id),

    -- Metadata de IA (categorización automática)
    ai_categorized          BOOLEAN NOT NULL DEFAULT FALSE,
    ai_confidence           NUMERIC(3,2),
    ai_suggested_category_id UUID REFERENCES categories(id) ON DELETE SET NULL,

    -- Input por voz
    voice_input             BOOLEAN NOT NULL DEFAULT FALSE,
    raw_voice_text          TEXT,

    -- Geolocalización opcional
    location_lat            NUMERIC(9,6),
    location_lng            NUMERIC(9,6),

    -- Sync offline
    client_id               VARCHAR(100),
    synced_at               TIMESTAMPTZ,

    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ,

    CONSTRAINT chk_transaction_type   CHECK (transaction_type IN ('income','expense','transfer')),
    CONSTRAINT chk_amount_positive    CHECK (amount > 0),
    CONSTRAINT chk_ai_confidence      CHECK (ai_confidence IS NULL OR (ai_confidence >= 0 AND ai_confidence <= 1)),
    CONSTRAINT chk_transfer_distinct  CHECK (
        transaction_type <> 'transfer' OR
        (transfer_account_id IS NOT NULL AND transfer_account_id <> account_id)
    )
);

CREATE INDEX idx_txn_user_date ON transactions(user_id, transaction_date DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_txn_tracking_period ON transactions(tracking_period_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_txn_account ON transactions(account_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_txn_category ON transactions(category_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_txn_type ON transactions(transaction_type) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_txn_client_id ON transactions(user_id, client_id) WHERE client_id IS NOT NULL;


-- ============================================================================
-- 7. BUDGETS — Presupuestos por seguimiento
-- ============================================================================
-- Cada presupuesto pertenece a un seguimiento específico.
-- Al cerrarse un seguimiento, los presupuestos se auto-copian al siguiente.

CREATE TABLE budgets (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tracking_period_id      UUID NOT NULL REFERENCES tracking_periods(id) ON DELETE CASCADE,
    category_id             UUID REFERENCES categories(id) ON DELETE CASCADE,  -- NULL = presupuesto global del periodo

    amount                  NUMERIC(15,2) NOT NULL,
    currency                CHAR(3) NOT NULL DEFAULT 'COP',

    -- Alertas (porcentajes en los que notificar)
    alert_threshold_warning NUMERIC(5,2) NOT NULL DEFAULT 80.00,   -- 80%
    alert_threshold_critical NUMERIC(5,2) NOT NULL DEFAULT 100.00, -- 100%

    notes                   TEXT,

    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_budget_amount_positive CHECK (amount > 0),
    CONSTRAINT uq_budget_per_period_category UNIQUE (tracking_period_id, category_id)
);

CREATE INDEX idx_budgets_user_id ON budgets(user_id);
CREATE INDEX idx_budgets_tracking_period ON budgets(tracking_period_id);
CREATE INDEX idx_budgets_category ON budgets(category_id);


-- ============================================================================
-- 8. SAVINGS_GOALS — Metas de ahorro
-- ============================================================================
-- INDEPENDIENTES del seguimiento. Tienen su propio rango temporal.
-- Las sugerencias se calculan en base a los seguimientos que intersecan.

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

    status                  VARCHAR(20) NOT NULL DEFAULT 'active',  -- active | achieved | abandoned | paused

    -- Cuenta asociada (opcional, para tracking del balance)
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


-- ============================================================================
-- 9. SAVINGS_GOAL_CONTRIBUTIONS — Aportes a metas de ahorro
-- ============================================================================
-- Cada aporte se vincula al seguimiento en el que se hizo, para poder
-- calcular "cuánto aporté en tal mes" durante reportes.

CREATE TABLE savings_goal_contributions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    savings_goal_id         UUID NOT NULL REFERENCES savings_goals(id) ON DELETE CASCADE,
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tracking_period_id      UUID NOT NULL REFERENCES tracking_periods(id) ON DELETE RESTRICT,

    -- Si el aporte vino de una transacción específica
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


-- ============================================================================
-- 10. RECURRING_TRANSACTIONS — Plantillas de transacciones recurrentes
-- ============================================================================
-- Generan transacciones reales automáticamente en el seguimiento activo.

CREATE TABLE recurring_transactions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id              UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    category_id             UUID REFERENCES categories(id) ON DELETE SET NULL,

    name                    VARCHAR(150) NOT NULL,    -- "Salario", "Arriendo", "Netflix"
    transaction_type        VARCHAR(20) NOT NULL,
    amount                  NUMERIC(15,2) NOT NULL,
    currency                CHAR(3) NOT NULL DEFAULT 'COP',
    description             VARCHAR(255),

    frequency               VARCHAR(20) NOT NULL,    -- daily | weekly | biweekly | monthly | yearly | custom
    custom_interval_days    INTEGER,                 -- si frequency='custom'

    day_of_month            SMALLINT,                -- para monthly (1-31)
    day_of_week             SMALLINT,                -- para weekly (0=domingo, 6=sábado)

    start_date              DATE NOT NULL,
    end_date                DATE,                    -- NULL = indefinido

    last_generated_date     DATE,                    -- última vez que se generó una transacción
    next_due_date           DATE,                    -- próxima fecha en que toca generar

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


-- ============================================================================
-- 11. TRACKING_PERIOD_SUMMARIES — Snapshot al cerrar el seguimiento
-- ============================================================================
-- Métricas precalculadas e inmutables. Se crean al cerrar el seguimiento.
-- 1:1 con tracking_periods (status = 'closed').

CREATE TABLE tracking_period_summaries (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tracking_period_id          UUID NOT NULL UNIQUE REFERENCES tracking_periods(id) ON DELETE CASCADE,
    user_id                     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Agregados globales
    total_income                NUMERIC(15,2) NOT NULL DEFAULT 0,
    total_expenses              NUMERIC(15,2) NOT NULL DEFAULT 0,
    total_transfers             NUMERIC(15,2) NOT NULL DEFAULT 0,
    net_savings                 NUMERIC(15,2) NOT NULL DEFAULT 0,
    savings_rate                NUMERIC(5,2),               -- % de ahorro respecto al ingreso

    transaction_count           INTEGER NOT NULL DEFAULT 0,
    expense_transaction_count   INTEGER NOT NULL DEFAULT 0,
    income_transaction_count    INTEGER NOT NULL DEFAULT 0,

    -- Top
    top_expense_category_id     UUID REFERENCES categories(id) ON DELETE SET NULL,
    top_expense_category_amount NUMERIC(15,2),

    -- Datos estructurados para visualización (evita queries pesados al UI)
    -- Ejemplo: { "by_category": [{"category_id":"...","name":"Alimentación","amount":450000,"percentage":35.2}, ...] }
    expense_by_category         JSONB NOT NULL DEFAULT '[]'::jsonb,
    income_by_category          JSONB NOT NULL DEFAULT '[]'::jsonb,
    expense_by_account          JSONB NOT NULL DEFAULT '[]'::jsonb,
    expense_by_day              JSONB NOT NULL DEFAULT '[]'::jsonb,    -- para gráficos de tendencia

    -- Cumplimiento de presupuestos
    -- Ejemplo: [{"budget_id":"...","category":"Comida","budgeted":500000,"spent":620000,"variance":-120000}]
    budget_performance          JSONB NOT NULL DEFAULT '[]'::jsonb,

    -- Aportes a metas durante este seguimiento
    goal_contributions_total    NUMERIC(15,2) NOT NULL DEFAULT 0,

    -- Comparación vs seguimiento anterior (puede ser NULL si es el primero)
    vs_previous_period          JSONB,    -- { "income_change":..., "expense_change":..., "savings_change":... }

    calculated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_summaries_user ON tracking_period_summaries(user_id);
CREATE INDEX idx_summaries_period ON tracking_period_summaries(tracking_period_id);


-- ============================================================================
-- 12. TRACKING_PERIOD_INSIGHTS — Análisis inteligentes
-- ============================================================================
-- Insights tanto "durante" (actualizables) como "final" (inmutables).

CREATE TABLE tracking_period_insights (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tracking_period_id      UUID NOT NULL REFERENCES tracking_periods(id) ON DELETE CASCADE,
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Clasificación
    insight_type            VARCHAR(50) NOT NULL,
    -- Tipos sugeridos:
    --   DURANTE:
    --     spending_pace, budget_warning, budget_exceeded, ant_expenses_early,
    --     unusual_expense, vs_previous_partial, goal_progress_alert
    --   FINAL:
    --     top_categories, top_merchants, ant_expenses_final, reduction_opportunity,
    --     savings_summary, budget_compliance, vs_previous_final, monthly_wrap_up,
    --     goal_achievement_summary

    calculation_phase       VARCHAR(20) NOT NULL,   -- during | final
    severity                VARCHAR(20) NOT NULL DEFAULT 'info',   -- info | success | warning | critical

    title                   VARCHAR(200) NOT NULL,        -- texto listo para UI
    message                 TEXT NOT NULL,                -- descripción + sugerencia
    action_label            VARCHAR(100),                 -- texto del botón de acción (opcional)
    action_target           VARCHAR(255),                 -- a dónde lleva el botón (ruta/deep link)

    -- Detalles estructurados para que el front pueda renderizar datos
    -- Ejemplo gastos hormiga:
    -- { "category_id":"...","count":42,"avg_amount":4500,"total":189000,"top_merchant":"OXXO" }
    data                    JSONB,

    -- Referencias a entidades (cuando aplica)
    related_category_id     UUID REFERENCES categories(id) ON DELETE SET NULL,
    related_account_id      UUID REFERENCES accounts(id) ON DELETE SET NULL,
    related_goal_id         UUID REFERENCES savings_goals(id) ON DELETE SET NULL,

    -- Estado del insight
    is_dismissed            BOOLEAN NOT NULL DEFAULT FALSE,
    dismissed_at            TIMESTAMPTZ,

    -- Vigencia (sobre todo para insights "durante")
    valid_from              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until             TIMESTAMPTZ,    -- NULL = indefinido dentro del seguimiento

    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_insight_phase    CHECK (calculation_phase IN ('during','final')),
    CONSTRAINT chk_insight_severity CHECK (severity IN ('info','success','warning','critical'))
);

CREATE INDEX idx_insights_period ON tracking_period_insights(tracking_period_id);
CREATE INDEX idx_insights_user_active ON tracking_period_insights(user_id, calculation_phase) WHERE is_dismissed = FALSE;
CREATE INDEX idx_insights_type ON tracking_period_insights(insight_type);
CREATE INDEX idx_insights_severity ON tracking_period_insights(severity, calculation_phase) WHERE is_dismissed = FALSE;


-- ============================================================================
-- 13. TRIGGERS — Mantener updated_at automáticamente
-- ============================================================================

CREATE OR REPLACE FUNCTION trg_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_updated_at_users
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TRIGGER set_updated_at_user_settings
    BEFORE UPDATE ON user_settings
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TRIGGER set_updated_at_accounts
    BEFORE UPDATE ON accounts
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TRIGGER set_updated_at_categories
    BEFORE UPDATE ON categories
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TRIGGER set_updated_at_tracking_periods
    BEFORE UPDATE ON tracking_periods
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TRIGGER set_updated_at_transactions
    BEFORE UPDATE ON transactions
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TRIGGER set_updated_at_budgets
    BEFORE UPDATE ON budgets
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TRIGGER set_updated_at_savings_goals
    BEFORE UPDATE ON savings_goals
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TRIGGER set_updated_at_recurring_transactions
    BEFORE UPDATE ON recurring_transactions
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TRIGGER set_updated_at_insights
    BEFORE UPDATE ON tracking_period_insights
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();


-- ============================================================================
-- 14. TRIGGER — Validar que transactions están dentro de su tracking_period
-- ============================================================================

CREATE OR REPLACE FUNCTION trg_validate_transaction_period()
RETURNS TRIGGER AS $$
DECLARE
    period_start DATE;
    period_end   DATE;
    period_status VARCHAR(20);
BEGIN
    SELECT start_date, end_date, status
      INTO period_start, period_end, period_status
      FROM tracking_periods
     WHERE id = NEW.tracking_period_id;

    IF period_status = 'closed' THEN
        RAISE EXCEPTION 'No se pueden crear/modificar transacciones en un seguimiento cerrado';
    END IF;

    IF NEW.transaction_date < period_start OR NEW.transaction_date > period_end THEN
        RAISE EXCEPTION 'La fecha de la transacción (%) está fuera del seguimiento (% a %)',
            NEW.transaction_date, period_start, period_end;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER validate_transaction_period
    BEFORE INSERT OR UPDATE ON transactions
    FOR EACH ROW EXECUTE FUNCTION trg_validate_transaction_period();


-- ============================================================================
-- 15. SEED DATA — Categorías del sistema (predefinidas)
-- ============================================================================

INSERT INTO categories (name, category_type, icon, color, is_system, display_order) VALUES
    -- Ingresos
    ('Salario',            'income',  'briefcase',     '#4CAF50', TRUE, 1),
    ('Freelance',          'income',  'laptop',        '#66BB6A', TRUE, 2),
    ('Inversiones',        'income',  'trending-up',   '#43A047', TRUE, 3),
    ('Regalos',            'income',  'gift',          '#81C784', TRUE, 4),
    ('Reembolsos',         'income',  'arrow-back',    '#A5D6A7', TRUE, 5),
    ('Otros ingresos',     'income',  'plus-circle',   '#C8E6C9', TRUE, 6),

    -- Gastos
    ('Alimentación',       'expense', 'restaurant',    '#FF7043', TRUE, 10),
    ('Transporte',         'expense', 'directions-car','#42A5F5', TRUE, 11),
    ('Vivienda',           'expense', 'home',          '#8D6E63', TRUE, 12),
    ('Servicios públicos', 'expense', 'flash',         '#FFA726', TRUE, 13),
    ('Salud',              'expense', 'medical-bag',   '#EF5350', TRUE, 14),
    ('Educación',          'expense', 'school',        '#5C6BC0', TRUE, 15),
    ('Entretenimiento',    'expense', 'movie',         '#AB47BC', TRUE, 16),
    ('Suscripciones',      'expense', 'card',          '#7E57C2', TRUE, 17),
    ('Ropa',               'expense', 'shirt',         '#EC407A', TRUE, 18),
    ('Mascotas',           'expense', 'paw',           '#26A69A', TRUE, 19),
    ('Café y snacks',      'expense', 'coffee',        '#A1887F', TRUE, 20),
    ('Tecnología',         'expense', 'laptop',        '#78909C', TRUE, 21),
    ('Impuestos',          'expense', 'document-text', '#546E7A', TRUE, 22),
    ('Otros gastos',       'expense', 'help-circle',   '#BDBDBD', TRUE, 23);


-- ============================================================================
-- FIN DEL SCHEMA
-- ============================================================================
