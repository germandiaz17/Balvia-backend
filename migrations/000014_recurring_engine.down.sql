DROP INDEX IF EXISTS uq_txn_recurring_date;
ALTER TABLE transactions DROP COLUMN IF EXISTS recurring_transaction_id;
