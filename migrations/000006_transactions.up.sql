CREATE TABLE transactions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tracking_period_id      UUID NOT NULL REFERENCES tracking_periods(id) ON DELETE RESTRICT,
    account_id              UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    category_id             UUID REFERENCES categories(id) ON DELETE SET NULL,

    transaction_type        VARCHAR(20) NOT NULL,
    amount                  NUMERIC(15,2) NOT NULL,
    currency                CHAR(3) NOT NULL DEFAULT 'COP',
    description             VARCHAR(255),
    notes                   TEXT,
    transaction_date        DATE NOT NULL,

    transfer_account_id     UUID REFERENCES accounts(id),

    ai_categorized          BOOLEAN NOT NULL DEFAULT FALSE,
    ai_confidence           NUMERIC(3,2),
    ai_suggested_category_id UUID REFERENCES categories(id) ON DELETE SET NULL,

    voice_input             BOOLEAN NOT NULL DEFAULT FALSE,
    raw_voice_text          TEXT,

    location_lat            NUMERIC(9,6),
    location_lng            NUMERIC(9,6),

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

-- Trigger: validar que la transacción está dentro del tracking_period y que éste no esté cerrado
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
