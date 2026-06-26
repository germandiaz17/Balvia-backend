-- name: SummarizePeriodTotals :one
SELECT
    COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'income'), 0)::numeric  AS total_income,
    COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'expense'), 0)::numeric AS total_expenses,
    COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'transfer'), 0)::numeric AS total_transfers,
    COUNT(*)::int                                                AS transaction_count,
    COUNT(*) FILTER (WHERE transaction_type = 'expense')::int    AS expense_transaction_count,
    COUNT(*) FILTER (WHERE transaction_type = 'income')::int     AS income_transaction_count
FROM transactions
WHERE tracking_period_id = $1 AND deleted_at IS NULL;

-- name: GetTopExpenseCategory :one
SELECT category_id, SUM(amount)::numeric AS total
FROM transactions
WHERE tracking_period_id = $1
  AND transaction_type = 'expense'
  AND deleted_at IS NULL
  AND category_id IS NOT NULL
GROUP BY category_id
ORDER BY total DESC
LIMIT 1;

-- name: CreateTrackingPeriodSummary :one
INSERT INTO tracking_period_summaries (
    tracking_period_id, user_id,
    total_income, total_expenses, total_transfers, net_savings, savings_rate,
    transaction_count, expense_transaction_count, income_transaction_count,
    top_expense_category_id, top_expense_category_amount
) VALUES (
    sqlc.arg(tracking_period_id),
    sqlc.arg(user_id),
    sqlc.arg(total_income),
    sqlc.arg(total_expenses),
    sqlc.arg(total_transfers),
    sqlc.arg(net_savings),
    sqlc.narg(savings_rate),
    sqlc.arg(transaction_count),
    sqlc.arg(expense_transaction_count),
    sqlc.arg(income_transaction_count),
    sqlc.narg(top_expense_category_id),
    sqlc.narg(top_expense_category_amount)
)
RETURNING *;
