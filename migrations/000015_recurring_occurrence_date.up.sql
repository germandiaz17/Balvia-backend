-- Migration 000015: add occurrence_date to transactions.
--
-- When a recurring template generates a transaction, the transaction_date is
-- clamped to the active period's range (since historical occurrences must
-- land within a valid open period). This means two catch-up occurrences can
-- share the same transaction_date. To correctly identify which specific
-- recurrence iteration a transaction represents, we store the original
-- scheduled occurrence date separately.
--
-- occurrence_date is:
--   - NULL for regular (non-recurring) transactions.
--   - The original next_due_date value at the time of generation for
--     recurring-engine transactions.
--
-- The unique partial index ensures idempotency: a (template, occurrence_date)
-- pair can only ever produce one non-deleted transaction row, even if the
-- engine runs twice.

ALTER TABLE transactions
    ADD COLUMN occurrence_date DATE;

-- Replace the previous index that incorrectly used transaction_date as the
-- idempotency key. The new index uses occurrence_date which is the true
-- identity of a recurring occurrence.
DROP INDEX IF EXISTS uq_txn_recurring_date;

CREATE UNIQUE INDEX uq_txn_recurring_occurrence
    ON transactions(recurring_transaction_id, occurrence_date)
    WHERE recurring_transaction_id IS NOT NULL
      AND occurrence_date IS NOT NULL
      AND deleted_at IS NULL;
