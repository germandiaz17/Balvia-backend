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

-- name: SummarizePeriodTotalsInRange :one
-- Same aggregates as SummarizePeriodTotals but filtered to [from_date, to_date].
-- Used to compute biweekly / weekly sub-period breakdowns on the fly.
SELECT
    COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'income'), 0)::numeric  AS total_income,
    COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'expense'), 0)::numeric AS total_expenses,
    COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'transfer'), 0)::numeric AS total_transfers,
    COUNT(*)::int                                                AS transaction_count,
    COUNT(*) FILTER (WHERE transaction_type = 'expense')::int    AS expense_transaction_count,
    COUNT(*) FILTER (WHERE transaction_type = 'income')::int     AS income_transaction_count
FROM transactions
WHERE tracking_period_id = sqlc.arg(tracking_period_id)
  AND transaction_date BETWEEN sqlc.arg(from_date) AND sqlc.arg(to_date)
  AND deleted_at IS NULL;

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

-- name: UpdateTrackingPeriodSummaryBreakdowns :one
-- Populates the JSONB breakdown columns on an existing summary row. Called
-- after the row is created so we can compute breakdowns in Go and set them
-- in a second step without reopening the creation transaction.
UPDATE tracking_period_summaries
SET expense_by_category      = sqlc.arg(expense_by_category),
    income_by_category       = sqlc.arg(income_by_category),
    expense_by_account       = sqlc.arg(expense_by_account),
    expense_by_day           = sqlc.arg(expense_by_day),
    budget_performance       = sqlc.arg(budget_performance),
    goal_contributions_total = sqlc.arg(goal_contributions_total),
    vs_previous_period       = sqlc.arg(vs_previous_period)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: GetTrackingPeriodSummary :one
SELECT * FROM tracking_period_summaries
WHERE tracking_period_id = sqlc.arg(tracking_period_id);

-- name: ExpenseByCategory :many
-- Groups expense transactions in the period by category. Returns category_id
-- (nullable), category name (nullable — NULL when uncategorized), and total.
SELECT
    t.category_id,
    c.name  AS category_name,
    SUM(t.amount)::numeric AS total,
    COUNT(*)::int          AS txn_count
FROM transactions t
LEFT JOIN categories c ON c.id = t.category_id
WHERE t.tracking_period_id = $1
  AND t.transaction_type = 'expense'
  AND t.deleted_at IS NULL
GROUP BY t.category_id, c.name
ORDER BY total DESC;

-- name: IncomeByCategory :many
SELECT
    t.category_id,
    c.name  AS category_name,
    SUM(t.amount)::numeric AS total,
    COUNT(*)::int          AS txn_count
FROM transactions t
LEFT JOIN categories c ON c.id = t.category_id
WHERE t.tracking_period_id = $1
  AND t.transaction_type = 'income'
  AND t.deleted_at IS NULL
GROUP BY t.category_id, c.name
ORDER BY total DESC;

-- name: ExpenseByAccount :many
SELECT
    t.account_id,
    a.name  AS account_name,
    SUM(t.amount)::numeric AS total,
    COUNT(*)::int          AS txn_count
FROM transactions t
JOIN accounts a ON a.id = t.account_id
WHERE t.tracking_period_id = $1
  AND t.transaction_type = 'expense'
  AND t.deleted_at IS NULL
GROUP BY t.account_id, a.name
ORDER BY total DESC;

-- name: ExpenseByDay :many
-- Daily expense totals for the period, ordered chronologically.
SELECT
    transaction_date,
    SUM(amount)::numeric AS total,
    COUNT(*)::int        AS txn_count
FROM transactions
WHERE tracking_period_id = $1
  AND transaction_type = 'expense'
  AND deleted_at IS NULL
GROUP BY transaction_date
ORDER BY transaction_date;

-- name: TopMerchants :many
-- Returns the top description values by total expense amount.
-- Only rows with a non-empty description are included so purely note-based
-- transactions do not pollute the merchant list.
SELECT
    description,
    SUM(amount)::numeric AS total,
    COUNT(*)::int        AS txn_count
FROM transactions
WHERE tracking_period_id = $1
  AND transaction_type = 'expense'
  AND deleted_at IS NULL
  AND description IS NOT NULL
  AND description != ''
GROUP BY description
ORDER BY total DESC
LIMIT 10;

-- name: GoalContributionsTotalForPeriod :one
-- Total savings goal contributions that were recorded within a tracking period.
SELECT COALESCE(SUM(amount), 0)::numeric AS total
FROM savings_goal_contributions
WHERE tracking_period_id = $1;

-- name: GetPreviousTrackingPeriod :one
-- Returns the immediately preceding closed period for the same user.
SELECT * FROM tracking_periods
WHERE user_id = sqlc.arg(user_id)
  AND sequence_number = sqlc.arg(sequence_number) - 1
  AND status = 'closed';

-- name: GetTrackingPeriodSummaryForPeriod :one
-- Returns the snapshot summary for a closed period (used for vs_previous).
SELECT * FROM tracking_period_summaries
WHERE tracking_period_id = sqlc.arg(tracking_period_id);
