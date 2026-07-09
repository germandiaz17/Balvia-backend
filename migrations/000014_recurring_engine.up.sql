-- Migration 000014: add recurring transaction engine support.
--
-- Changes:
--   1. Add recurring_transaction_id FK on transactions so each generated row
--      is linked back to its template. Used for idempotency: the engine checks
--      whether an (template_id, transaction_date) pair already has a row before
--      inserting.
--   2. Add (recurring_transaction_id, transaction_date) unique partial index so
--      the database itself enforces idempotency even if two engine runs race.

ALTER TABLE transactions
    ADD COLUMN recurring_transaction_id UUID
        REFERENCES recurring_transactions(id) ON DELETE SET NULL;

-- Partial index: only rows that originated from a recurring template are
-- covered, so normal (non-recurring) transactions are unaffected.
CREATE UNIQUE INDEX uq_txn_recurring_date
    ON transactions(recurring_transaction_id, transaction_date)
    WHERE recurring_transaction_id IS NOT NULL AND deleted_at IS NULL;
