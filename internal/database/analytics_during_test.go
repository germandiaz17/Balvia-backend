package database

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// makePeriod builds a TrackingPeriod spanning the given number of days starting
// from start, with today set to startDay days into the period (1-indexed).
func makePeriod(start time.Time, durationDays, elapsedDays int) (sqlc.TrackingPeriod, time.Time) {
	end := start.AddDate(0, 0, durationDays-1)
	today := start.AddDate(0, 0, elapsedDays-1)
	return sqlc.TrackingPeriod{
		ID:        uuid.New(),
		StartDate: pgtype.Date{Time: start, Valid: true},
		EndDate:   pgtype.Date{Time: end, Valid: true},
		Status:    "active",
	}, today
}

// makeBudget builds a minimal budget row with the given category and thresholds.
func makeBudget(catID uuid.UUID, amount, warning, critical string) sqlc.Budget {
	return sqlc.Budget{
		ID:                     uuid.New(),
		CategoryID:             uuid.NullUUID{UUID: catID, Valid: true},
		Amount:                 decimal.RequireFromString(amount),
		AlertThresholdWarning:  decimal.RequireFromString(warning),
		AlertThresholdCritical: decimal.RequireFromString(critical),
	}
}

// makeGlobalBudget builds a budget without a category (global budget).
func makeGlobalBudget(amount, warning, critical string) sqlc.Budget {
	return sqlc.Budget{
		ID:                     uuid.New(),
		CategoryID:             uuid.NullUUID{Valid: false},
		Amount:                 decimal.RequireFromString(amount),
		AlertThresholdWarning:  decimal.RequireFromString(warning),
		AlertThresholdCritical: decimal.RequireFromString(critical),
	}
}

// makeDuringData builds a minimal duringPeriodData for testing. Period is
// 30 days long, elapsed is 10 days.
func makeDuringData() duringPeriodData {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	period, today := makePeriod(start, 30, 10)

	catID := uuid.New()
	catName := "Comida"

	totals := sqlc.SummarizePeriodTotalsRow{
		TotalIncome:             decimal.NewFromInt(3_000_000),
		TotalExpenses:           decimal.NewFromInt(500_000),
		TotalTransfers:          decimal.Zero,
		TransactionCount:        8,
		ExpenseTransactionCount: 5,
		IncomeTransactionCount:  3,
	}

	return duringPeriodData{
		Period: period,
		Today:  today,
		Totals: totals,
		ExpByCategory: []sqlc.ExpenseByCategoryRow{
			{CategoryID: uuid.NullUUID{UUID: catID, Valid: true}, CategoryName: &catName, Total: decimal.NewFromInt(300_000), TxnCount: 4},
			{CategoryID: uuid.NullUUID{Valid: false}, CategoryName: nil, Total: decimal.NewFromInt(200_000), TxnCount: 1},
		},
		Budgets: []sqlc.Budget{
			makeBudget(catID, "400000", "80", "100"),
		},
		Goals:        nil,
		GoalContribs: decimal.Zero,
		PrevSummary:  nil,
	}
}

// ─── spending_pace ────────────────────────────────────────────────────────────

func TestGenSpendingPace_InfoWhenOnTrack(t *testing.T) {
	d := makeDuringData()
	// 500k spent in 10 of 30 days → 50k/day → projected 1.5M vs income 3M → ok
	dr := genSpendingPace(d)
	require.NotNil(t, dr)
	assert.Equal(t, "spending_pace", dr.InsightType)
	assert.Equal(t, "during", dr.CalculationPhase)
	assert.Equal(t, "info", dr.Severity)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(dr.Data, &payload))
	assert.EqualValues(t, 10, payload["elapsed_days"])
	assert.EqualValues(t, 30, payload["total_days"])
}

func TestGenSpendingPace_WarningWhenProjectedExceedsIncome(t *testing.T) {
	d := makeDuringData()
	// Period: 30 days, elapsed: 10. Income: 3M.
	// Spend 700k in 10 days → 70k/day → projected 2.1M.
	// 2.1M > income (3M)? No. Need projected > income but < 120% income (3.6M).
	// Spend 1.1M → 110k/day → projected 3.3M. 3.3M > 3M and 3.3M < 3.6M → warning.
	d.Totals.TotalExpenses = decimal.NewFromInt(1_100_000)
	dr := genSpendingPace(d)
	require.NotNil(t, dr)
	assert.Equal(t, "warning", dr.Severity)
}

func TestGenSpendingPace_CriticalWhenProjectedExceeds120PctIncome(t *testing.T) {
	d := makeDuringData()
	// Spend 2.5M in 10 days → 250k/day → projected 7.5M > 120% of 3M income (3.6M)
	d.Totals.TotalExpenses = decimal.NewFromInt(2_500_000)
	dr := genSpendingPace(d)
	require.NotNil(t, dr)
	assert.Equal(t, "critical", dr.Severity)
}

func TestGenSpendingPace_OmittedWhenNoExpenses(t *testing.T) {
	d := makeDuringData()
	d.Totals.TotalExpenses = decimal.Zero
	dr := genSpendingPace(d)
	assert.Nil(t, dr, "should be omitted when no expenses yet")
}

// ─── budget_warning / budget_exceeded ────────────────────────────────────────

func TestGenBudgetAlerts_WarningWhenApproachingThreshold(t *testing.T) {
	d := makeDuringData()
	// Category has 300k spent against budget 400k → 75% — below warning (80%)
	// Let's push it to 85%: spend = 340k
	catID := d.Budgets[0].CategoryID.UUID
	d.ExpByCategory[0] = sqlc.ExpenseByCategoryRow{
		CategoryID: uuid.NullUUID{UUID: catID, Valid: true},
		Total:      decimal.NewFromInt(340_000),
		TxnCount:   4,
	}
	d.Totals.TotalExpenses = decimal.NewFromInt(540_000)

	alerts := genBudgetAlerts(d)
	require.Len(t, alerts, 1)
	assert.Equal(t, "budget_warning", alerts[0].InsightType)
	assert.Equal(t, "warning", alerts[0].Severity)
}

func TestGenBudgetAlerts_ExceededWhenOverCriticalThreshold(t *testing.T) {
	d := makeDuringData()
	catID := d.Budgets[0].CategoryID.UUID
	// Spend 450k against budget 400k → 112.5% > critical (100%)
	d.ExpByCategory[0] = sqlc.ExpenseByCategoryRow{
		CategoryID: uuid.NullUUID{UUID: catID, Valid: true},
		Total:      decimal.NewFromInt(450_000),
		TxnCount:   5,
	}
	d.Totals.TotalExpenses = decimal.NewFromInt(650_000)

	alerts := genBudgetAlerts(d)
	require.Len(t, alerts, 1)
	assert.Equal(t, "budget_exceeded", alerts[0].InsightType)
	assert.Equal(t, "critical", alerts[0].Severity)
}

func TestGenBudgetAlerts_ExceededTakesPriorityOverWarning(t *testing.T) {
	// A budget that has crossed both warning and critical threshold should only
	// emit budget_exceeded (not both).
	d := makeDuringData()
	catID := d.Budgets[0].CategoryID.UUID
	d.ExpByCategory[0] = sqlc.ExpenseByCategoryRow{
		CategoryID: uuid.NullUUID{UUID: catID, Valid: true},
		Total:      decimal.NewFromInt(500_000), // 125% of 400k budget
		TxnCount:   5,
	}
	alerts := genBudgetAlerts(d)
	require.Len(t, alerts, 1)
	assert.Equal(t, "budget_exceeded", alerts[0].InsightType)
}

func TestGenBudgetAlerts_NoneWhenBelowWarningThreshold(t *testing.T) {
	d := makeDuringData()
	// 300k spent against 400k budget → 75%, below 80% warning threshold
	alerts := genBudgetAlerts(d)
	assert.Empty(t, alerts, "no alerts below warning threshold")
}

func TestGenBudgetAlerts_GlobalBudgetUsesTotalExpenses(t *testing.T) {
	d := makeDuringData()
	d.Budgets = []sqlc.Budget{makeGlobalBudget("600000", "80", "100")}
	// Total expenses 500k / 600k = 83.3% → warning
	alerts := genBudgetAlerts(d)
	require.Len(t, alerts, 1)
	assert.Equal(t, "budget_warning", alerts[0].InsightType)
}

func TestGenBudgetAlerts_OmittedWhenNoBudgets(t *testing.T) {
	d := makeDuringData()
	d.Budgets = nil
	alerts := genBudgetAlerts(d)
	assert.Nil(t, alerts)
}

// ─── ant_expenses_early ───────────────────────────────────────────────────────

func TestGenAntExpensesEarly_DetectedWhenPatternPresent(t *testing.T) {
	d := makeDuringData()
	// avg txn = 500k/7 ≈ 71.4k. threshold ≈ 7.14k.
	// Small cat: 18k in 3 txns → 6k/txn < 7.14k → ant. antCount = 3 ≥ 3 → generated.
	d.Totals.TotalExpenses = decimal.NewFromInt(500_000)
	d.Totals.ExpenseTransactionCount = 7

	smallCat := "Transporte"
	d.ExpByCategory = []sqlc.ExpenseByCategoryRow{
		{CategoryID: uuid.NullUUID{Valid: false}, CategoryName: nil, Total: decimal.NewFromInt(482_000), TxnCount: 4},
		{CategoryID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, CategoryName: &smallCat, Total: decimal.NewFromInt(18_000), TxnCount: 3},
	}

	dr := genAntExpensesEarly(d)
	require.NotNil(t, dr)
	assert.Equal(t, "ant_expenses_early", dr.InsightType)
	assert.Equal(t, "during", dr.CalculationPhase)
	assert.Equal(t, "warning", dr.Severity)
}

func TestGenAntExpensesEarly_OmittedWhenFewTransactions(t *testing.T) {
	d := makeDuringData()
	d.Totals.ExpenseTransactionCount = 2
	dr := genAntExpensesEarly(d)
	assert.Nil(t, dr)
}

// ─── unusual_expense ─────────────────────────────────────────────────────────

func TestGenUnusualExpense_FlaggedWhenDominatesWithFewTxns(t *testing.T) {
	d := makeDuringData()
	// Top category has 350k out of 500k total = 70%, with only 1 txn → unusual
	d.Totals.ExpenseTransactionCount = 3
	d.ExpByCategory[0] = sqlc.ExpenseByCategoryRow{
		CategoryID: d.ExpByCategory[0].CategoryID,
		Total:      decimal.NewFromInt(350_000),
		TxnCount:   1, // single large purchase
	}
	d.ExpByCategory[1].Total = decimal.NewFromInt(150_000)

	dr := genUnusualExpense(d)
	require.NotNil(t, dr)
	assert.Equal(t, "unusual_expense", dr.InsightType)
	assert.Equal(t, "info", dr.Severity)
}

func TestGenUnusualExpense_OmittedWhenNotDominating(t *testing.T) {
	d := makeDuringData()
	// Top category is 60% but not dominant enough to flag yet (we need 30%+ AND few txns)
	// 300k/500k = 60% but 4 txns → skip (TxnCount > 3)
	dr := genUnusualExpense(d)
	assert.Nil(t, dr, "should be omitted when category has many transactions")
}

// ─── vs_previous_partial ─────────────────────────────────────────────────────

func TestGenVsPreviousPartial_OmittedWithoutPrevSummary(t *testing.T) {
	d := makeDuringData()
	d.PrevSummary = nil
	dr := genVsPreviousPartial(d)
	assert.Nil(t, dr)
}

func TestGenVsPreviousPartial_InfoWhenSimilar(t *testing.T) {
	d := makeDuringData()
	// 10/30 days elapsed → 33% of period. Previous period expenses: 1.5M.
	// Estimate = 1.5M * 33% = 500k. Current = 500k → similar.
	prevPeriodID := uuid.New()
	d.PrevSummary = &sqlc.TrackingPeriodSummary{
		TrackingPeriodID: prevPeriodID,
		TotalExpenses:    decimal.NewFromInt(1_500_000),
	}

	dr := genVsPreviousPartial(d)
	require.NotNil(t, dr)
	assert.Equal(t, "vs_previous_partial", dr.InsightType)
	assert.Equal(t, "info", dr.Severity)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(dr.Data, &payload))
	assert.Equal(t, prevPeriodID.String(), payload["previous_period_id"])
}

func TestGenVsPreviousPartial_WarningWhenHigherThanPrevious(t *testing.T) {
	d := makeDuringData()
	// 10/30 days elapsed → 33%. Previous: 900k total → estimate 300k.
	// Current: 500k → 66% higher than 300k estimate → warning
	prevPeriodID := uuid.New()
	d.PrevSummary = &sqlc.TrackingPeriodSummary{
		TrackingPeriodID: prevPeriodID,
		TotalExpenses:    decimal.NewFromInt(900_000),
	}

	dr := genVsPreviousPartial(d)
	require.NotNil(t, dr)
	assert.Equal(t, "warning", dr.Severity)
}

// ─── goal_progress_alert ─────────────────────────────────────────────────────

func TestGenGoalProgressAlerts_OmittedWhenOnTrack(t *testing.T) {
	d := makeDuringData()
	today := d.Today
	goalStart := today.AddDate(0, 0, -30) // started 30 days ago
	goalEnd := today.AddDate(0, 0, 60)    // ends in 60 days — 90 total

	// Expected: 30/90 = 33% of 1M = 333k. Current = 400k → on track.
	d.Goals = []sqlc.SavingsGoal{
		{
			ID:            uuid.New(),
			Name:          "Vacaciones",
			TargetAmount:  decimal.NewFromInt(1_000_000),
			CurrentAmount: decimal.NewFromInt(400_000),
			Status:        "active",
			StartDate:     pgtype.Date{Time: goalStart, Valid: true},
			TargetDate:    pgtype.Date{Time: goalEnd, Valid: true},
		},
	}
	alerts := genGoalProgressAlerts(d)
	assert.Empty(t, alerts, "no alert when goal is on track")
}

func TestGenGoalProgressAlerts_AlertWhenBehind(t *testing.T) {
	d := makeDuringData()
	today := d.Today
	goalStart := today.AddDate(0, 0, -30)
	goalEnd := today.AddDate(0, 0, 60)

	// Expected: 30/90 = 33% of 1M = 333k. Current = 100k → behind.
	d.Goals = []sqlc.SavingsGoal{
		{
			ID:            uuid.New(),
			Name:          "Fondo de emergencia",
			TargetAmount:  decimal.NewFromInt(1_000_000),
			CurrentAmount: decimal.NewFromInt(100_000),
			Status:        "active",
			StartDate:     pgtype.Date{Time: goalStart, Valid: true},
			TargetDate:    pgtype.Date{Time: goalEnd, Valid: true},
		},
	}
	alerts := genGoalProgressAlerts(d)
	require.Len(t, alerts, 1)
	assert.Equal(t, "goal_progress_alert", alerts[0].InsightType)
	assert.Equal(t, "warning", alerts[0].Severity)
}

func TestGenGoalProgressAlerts_OmittedForNonActiveGoal(t *testing.T) {
	d := makeDuringData()
	today := d.Today
	d.Goals = []sqlc.SavingsGoal{
		{
			ID:            uuid.New(),
			Name:          "Laptop",
			TargetAmount:  decimal.NewFromInt(2_000_000),
			CurrentAmount: decimal.Zero,
			Status:        "paused",
			StartDate:     pgtype.Date{Time: today.AddDate(0, 0, -30), Valid: true},
			TargetDate:    pgtype.Date{Time: today.AddDate(0, 0, 60), Valid: true},
		},
	}
	alerts := genGoalProgressAlerts(d)
	assert.Empty(t, alerts, "paused goal should not generate alerts")
}

// ─── generateImmediateDuringInsights ─────────────────────────────────────────

func TestGenerateImmediateDuringInsights_ContainsExpectedTypes(t *testing.T) {
	d := makeDuringData()
	// Push budget to warning territory.
	catID := d.Budgets[0].CategoryID.UUID
	d.ExpByCategory[0] = sqlc.ExpenseByCategoryRow{
		CategoryID: uuid.NullUUID{UUID: catID, Valid: true},
		Total:      decimal.NewFromInt(340_000),
		TxnCount:   4,
	}
	d.Totals.TotalExpenses = decimal.NewFromInt(540_000)

	drafts := generateImmediateDuringInsights(d)
	types := make(map[string]bool)
	for _, dr := range drafts {
		types[dr.InsightType] = true
	}

	assert.True(t, types["spending_pace"], "spending_pace always included")
	assert.True(t, types["budget_warning"], "budget_warning when threshold crossed")
	assert.False(t, types["ant_expenses_early"], "lazy insight not in immediate set")
}

// ─── generateLazyDuringInsights ──────────────────────────────────────────────

func TestGenerateLazyDuringInsights_IncludesAllTypes(t *testing.T) {
	d := makeDuringData()

	// Setup for ant_expenses_early.
	// avg txn = 500k / 8 = 62.5k → threshold = 6.25k.
	// Small cat: 15k / 3 txns = 5k per txn < 6.25k → ant. antCount = 3 ≥ 3 → generated.
	d.Totals.ExpenseTransactionCount = 8
	smallCat := "Meriendas"
	d.ExpByCategory = []sqlc.ExpenseByCategoryRow{
		{CategoryID: uuid.NullUUID{Valid: false}, CategoryName: nil, Total: decimal.NewFromInt(485_000), TxnCount: 5},
		{CategoryID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, CategoryName: &smallCat, Total: decimal.NewFromInt(15_000), TxnCount: 3},
	}

	// Setup for vs_previous_partial.
	prevID := uuid.New()
	d.PrevSummary = &sqlc.TrackingPeriodSummary{
		TrackingPeriodID: prevID,
		TotalExpenses:    decimal.NewFromInt(900_000),
	}

	// Setup for goal_progress_alert.
	today := d.Today
	d.Goals = []sqlc.SavingsGoal{
		{
			ID:            uuid.New(),
			Name:          "Carro",
			TargetAmount:  decimal.NewFromInt(5_000_000),
			CurrentAmount: decimal.Zero,
			Status:        "active",
			StartDate:     pgtype.Date{Time: today.AddDate(0, 0, -30), Valid: true},
			TargetDate:    pgtype.Date{Time: today.AddDate(0, 0, 60), Valid: true},
		},
	}

	drafts := generateLazyDuringInsights(d)
	types := make(map[string]bool)
	for _, dr := range drafts {
		types[dr.InsightType] = true
		// All insights must be phase "during".
		assert.Equal(t, "during", dr.CalculationPhase, "all lazy insights should be 'during'")
	}

	assert.True(t, types["spending_pace"])
	assert.True(t, types["ant_expenses_early"])
	assert.True(t, types["vs_previous_partial"])
	assert.True(t, types["goal_progress_alert"])
}
