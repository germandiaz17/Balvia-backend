package database

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
)

// makeTestData returns a minimal closePeriodData with sensible defaults.
func makeTestData() closePeriodData {
	catID := uuid.New()
	catName := "Comida"
	catIDNull := uuid.NullUUID{UUID: catID, Valid: true}

	return closePeriodData{
		Totals: sqlc.SummarizePeriodTotalsRow{
			TotalIncome:             decimal.NewFromInt(2_000_000),
			TotalExpenses:           decimal.NewFromInt(1_500_000),
			TotalTransfers:          decimal.Zero,
			TransactionCount:        10,
			ExpenseTransactionCount: 7,
			IncomeTransactionCount:  3,
		},
		ExpByCategory: []sqlc.ExpenseByCategoryRow{
			{CategoryID: catIDNull, CategoryName: &catName, Total: decimal.NewFromInt(900_000), TxnCount: 4},
			{CategoryID: uuid.NullUUID{Valid: false}, CategoryName: nil, Total: decimal.NewFromInt(600_000), TxnCount: 3},
		},
		IncByCategory: []sqlc.IncomeByCategoryRow{},
		ExpByAccount:  []sqlc.ExpenseByAccountRow{},
		ExpByDay:      []sqlc.ExpenseByDayRow{},
		TopMerchants: []sqlc.TopMerchantsRow{
			{Description: strPtr("Supermercado ABC"), Total: decimal.NewFromInt(500_000), TxnCount: 3},
		},
		Budgets:           nil,
		GoalContribsTotal: decimal.NewFromInt(200_000),
	}
}

func strPtr(s string) *string { return &s }

// ─── generateFinalInsights ────────────────────────────────────────────────────

func TestGenerateFinalInsights_AlwaysIncludesSavingsSummaryAndWrapUp(t *testing.T) {
	d := makeTestData()
	drafts := generateFinalInsights(d)

	types := make(map[string]bool)
	for _, dr := range drafts {
		types[dr.InsightType] = true
	}

	assert.True(t, types["savings_summary"], "savings_summary always generated")
	assert.True(t, types["monthly_wrap_up"], "monthly_wrap_up always generated")
}

func TestGenerateFinalInsights_TopCategories(t *testing.T) {
	d := makeTestData()
	drafts := generateFinalInsights(d)

	var tc *insightDraft
	for i := range drafts {
		if drafts[i].InsightType == "top_categories" {
			tc = &drafts[i]
			break
		}
	}
	require.NotNil(t, tc, "top_categories insight should be generated when expenses exist")
	assert.Equal(t, "final", tc.CalculationPhase)
	assert.Equal(t, "info", tc.Severity)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(tc.Data, &payload))
	categories, ok := payload["categories"].([]any)
	require.True(t, ok)
	// Only 2 categories in test data (both should appear; limit is 3)
	assert.Equal(t, 2, len(categories))
}

func TestGenerateFinalInsights_TopMerchants(t *testing.T) {
	d := makeTestData()
	drafts := generateFinalInsights(d)

	var tm *insightDraft
	for i := range drafts {
		if drafts[i].InsightType == "top_merchants" {
			tm = &drafts[i]
			break
		}
	}
	require.NotNil(t, tm)
	assert.Contains(t, tm.Message, "Supermercado ABC")
}

func TestGenerateFinalInsights_NoTopMerchantsWhenNoDescriptions(t *testing.T) {
	d := makeTestData()
	d.TopMerchants = nil
	drafts := generateFinalInsights(d)

	for _, dr := range drafts {
		assert.NotEqual(t, "top_merchants", dr.InsightType, "should not generate top_merchants when no descriptions")
	}
}

func TestGenerateFinalInsights_SavingsSummaryNegative(t *testing.T) {
	d := makeTestData()
	d.Totals.TotalExpenses = decimal.NewFromInt(3_000_000) // spend > income
	drafts := generateFinalInsights(d)

	var ss *insightDraft
	for i := range drafts {
		if drafts[i].InsightType == "savings_summary" {
			ss = &drafts[i]
			break
		}
	}
	require.NotNil(t, ss)
	assert.Equal(t, "warning", ss.Severity)
}

func TestGenerateFinalInsights_SavingsSummaryExcellent(t *testing.T) {
	d := makeTestData()
	d.Totals.TotalExpenses = decimal.NewFromInt(500_000) // 75% savings rate
	drafts := generateFinalInsights(d)

	var ss *insightDraft
	for i := range drafts {
		if drafts[i].InsightType == "savings_summary" {
			ss = &drafts[i]
			break
		}
	}
	require.NotNil(t, ss)
	assert.Equal(t, "success", ss.Severity)
}

func TestGenerateFinalInsights_GoalAchievementSummary(t *testing.T) {
	d := makeTestData()
	// Already has 200_000 in contributions
	drafts := generateFinalInsights(d)

	var ga *insightDraft
	for i := range drafts {
		if drafts[i].InsightType == "goal_achievement_summary" {
			ga = &drafts[i]
			break
		}
	}
	require.NotNil(t, ga, "goal_achievement_summary when contributions > 0")
	assert.Equal(t, "success", ga.Severity)
}

func TestGenerateFinalInsights_NoGoalAchievementSummaryWhenZeroContributions(t *testing.T) {
	d := makeTestData()
	d.GoalContribsTotal = decimal.Zero
	drafts := generateFinalInsights(d)

	for _, dr := range drafts {
		assert.NotEqual(t, "goal_achievement_summary", dr.InsightType)
	}
}

func TestGenerateFinalInsights_VsPreviousOmittedWhenNoPrev(t *testing.T) {
	d := makeTestData()
	d.PrevSummary = nil
	drafts := generateFinalInsights(d)

	for _, dr := range drafts {
		assert.NotEqual(t, "vs_previous_final", dr.InsightType, "should be omitted when no prev summary")
	}
}

func TestGenerateFinalInsights_VsPreviousIncludedWhenPrevExists(t *testing.T) {
	d := makeTestData()
	prevID := uuid.New()
	d.PrevSummary = &sqlc.TrackingPeriodSummary{
		TrackingPeriodID: prevID,
		TotalIncome:      decimal.NewFromInt(1_800_000),
		TotalExpenses:    decimal.NewFromInt(1_600_000),
	}
	drafts := generateFinalInsights(d)

	var vp *insightDraft
	for i := range drafts {
		if drafts[i].InsightType == "vs_previous_final" {
			vp = &drafts[i]
			break
		}
	}
	require.NotNil(t, vp, "vs_previous_final must be generated when prev summary exists")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(vp.Data, &payload))
	assert.Equal(t, prevID.String(), payload["previous_period_id"])
}

func TestGenerateFinalInsights_BudgetComplianceOmittedWhenNoBudgets(t *testing.T) {
	d := makeTestData()
	d.Budgets = nil
	drafts := generateFinalInsights(d)

	for _, dr := range drafts {
		assert.NotEqual(t, "budget_compliance", dr.InsightType, "budget_compliance omitted when no budgets")
	}
}

func TestGenerateFinalInsights_BudgetComplianceExceeded(t *testing.T) {
	catID := uuid.New()
	d := makeTestData()
	d.Budgets = []sqlc.Budget{
		{
			ID:         uuid.New(),
			CategoryID: uuid.NullUUID{UUID: catID, Valid: true},
			Amount:     decimal.NewFromInt(100_000), // budget of 100k
		},
	}
	// ExpByCategory has "Comida" at 900k — far exceeds the budget
	d.ExpByCategory[0] = sqlc.ExpenseByCategoryRow{
		CategoryID:   uuid.NullUUID{UUID: catID, Valid: true},
		CategoryName: strPtr("Comida"),
		Total:        decimal.NewFromInt(900_000),
		TxnCount:     4,
	}

	drafts := generateFinalInsights(d)

	var bc *insightDraft
	for i := range drafts {
		if drafts[i].InsightType == "budget_compliance" {
			bc = &drafts[i]
			break
		}
	}
	require.NotNil(t, bc)
	assert.Equal(t, "critical", bc.Severity)
}

// ─── Reduction opportunity ────────────────────────────────────────────────────

func TestGenerateFinalInsights_ReductionOpportunityPresent(t *testing.T) {
	d := makeTestData()
	// Existing data has category with 4 txns — should trigger reduction_opportunity
	drafts := generateFinalInsights(d)

	var ro *insightDraft
	for i := range drafts {
		if drafts[i].InsightType == "reduction_opportunity" {
			ro = &drafts[i]
			break
		}
	}
	require.NotNil(t, ro, "reduction_opportunity should be generated")
	assert.Equal(t, "info", ro.Severity)
}

func TestGenerateFinalInsights_AntExpensesOmittedWhenFewTransactions(t *testing.T) {
	d := makeTestData()
	d.Totals.ExpenseTransactionCount = 3 // below threshold of 5
	drafts := generateFinalInsights(d)

	for _, dr := range drafts {
		assert.NotEqual(t, "ant_expenses_final", dr.InsightType, "ant_expenses omitted when < 5 transactions")
	}
}

// ─── Idempotency guard in closePeriodData (data collection) ─────────────────

func TestMustJSON_ProducesValidJSON(t *testing.T) {
	data := map[string]any{"key": "value", "num": 42}
	b := mustJSON(data)
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, "value", out["key"])
}
