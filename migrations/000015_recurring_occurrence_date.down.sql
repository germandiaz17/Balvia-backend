DROP INDEX IF EXISTS uq_txn_recurring_occurrence;

-- Restore the original (incorrect) index if rolling back.
CREATE UNIQUE INDEX uq_txn_recurring_date
    ON transactions(recurring_transaction_id, transaction_date)
    WHERE recurring_transaction_id IS NOT NULL AND deleted_at IS NULL;

ALTER TABLE transactions DROP COLUMN IF EXISTS occurrence_date;
