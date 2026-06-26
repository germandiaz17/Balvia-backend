DROP TRIGGER IF EXISTS validate_transaction_period ON transactions;
DROP FUNCTION IF EXISTS trg_validate_transaction_period();
DROP TABLE IF EXISTS transactions CASCADE;
