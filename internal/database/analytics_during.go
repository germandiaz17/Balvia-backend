package database

// analytics_during.go — "during" insight generators for the active tracking period.
//
// These insights are ephemeral and recalculated. Two refresh strategies:
//
//  1. IMMEDIATE (called on every transaction mutation — create/update/delete):
//     spending_pace, budget_warning, budget_exceeded
//     → old immediate insights are deleted and regenerated atomically.
//
//  2. LAZY (called when GET /tracking-periods/:id/insights is requested for the
//     active period):
//     ant_expenses_early, unusual_expense, vs_previous_partial, goal_progress_alert
//     → the full "during" set is replaced (delete-all + regenerate) so the client
//     always gets a coherent view.
//
// At period close, ALL "during" insights for the closing period are deleted so
// they do not contaminate the final insights response for that period.
//
// Insight types (all calculation_phase = "during"):
//   spending_pace        — current burn rate vs. remaining days/budget
//   budget_warning       — category spend approaching the warning threshold
//   budget_exceeded      — category spend has exceeded the critical threshold
//   ant_expenses_early   — many small transactions detected early in the period
//   unusual_expense      — single unusually large expense detected
//   vs_previous_partial  — partial-period comparison with the equivalent slice of the previous period
//   goal_progress_alert  — a savings goal is falling behind its expected pace

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
)

// duringPeriodData bundles the data needed to compute "during" insights.
// It is collected once per refresh call to avoid multiple round-trips.
type duringPeriodData struct {
	Period      sqlc.TrackingPeriod
	Totals      sqlc.SummarizePeriodTotalsRow
	ExpByCategory []sqlc.ExpenseByCategoryRow
	Budgets     []sqlc.Budget
	Goals       []sqlc.SavingsGoal
	GoalContribs decimal.Decimal

	// PrevSummary is the snapshot of the preceding closed period (nil if none).
	PrevSummary *sqlc.TrackingPeriodSummary

	// Today is the reference date for elapsed/remaining calculations.
	Today time.Time
}

// generateImmediateDuringInsights returns drafts for the three insights that
// are recalculated on every transaction mutation (fast path):
//   - spending_pace
//   - budget_warning  (one per budget that crossed its warning threshold)
//   - budget_exceeded (one per budget that crossed its critical threshold)
func generateImmediateDuringInsights(d duringPeriodData) []insightDraft {
	var result []insightDraft

	if dr := genSpendingPace(d); dr != nil {
		result = append(result, *dr)
	}
	result = append(result, genBudgetAlerts(d)...)

	return result
}

// generateLazyDuringInsights returns ALL "during" insight drafts (immediate +
// lazy). The caller first deletes all existing "during" insights for the period
// and then persists these.
func generateLazyDuringInsights(d duringPeriodData) []insightDraft {
	var result []insightDraft

	// Immediate
	if dr := genSpendingPace(d); dr != nil {
		result = append(result, *dr)
	}
	result = append(result, genBudgetAlerts(d)...)

	// Lazy
	if dr := genAntExpensesEarly(d); dr != nil {
		result = append(result, *dr)
	}
	if dr := genUnusualExpense(d); dr != nil {
		result = append(result, *dr)
	}
	if dr := genVsPreviousPartial(d); dr != nil {
		result = append(result, *dr)
	}
	result = append(result, genGoalProgressAlerts(d)...)

	return result
}

// ─── Immediate generators ─────────────────────────────────────────────────────

// genSpendingPace computes the daily burn rate and projects it against the
// remaining period days. Severity:
//   - "critical"  → projected total > 120% of income (or > actual expenses if no income)
//   - "warning"   → projected total > 100% of income
//   - "info"      → on track
//
// Omitted if the period has just started (0 elapsed days).
func genSpendingPace(d duringPeriodData) *insightDraft {
	start := d.Period.StartDate.Time
	end := d.Period.EndDate.Time

	elapsedDays := int(d.Today.Sub(start).Hours()/24) + 1
	totalDays := int(end.Sub(start).Hours()/24) + 1
	remainingDays := totalDays - elapsedDays

	if elapsedDays <= 0 || totalDays <= 0 {
		return nil
	}

	// Clamp elapsed to total (should not happen but guard against it)
	if elapsedDays > totalDays {
		elapsedDays = totalDays
		remainingDays = 0
	}

	expenses := d.Totals.TotalExpenses
	if expenses.IsZero() {
		return nil // nothing spent yet — no pace to report
	}

	// Daily burn rate (avg over elapsed days)
	dailyBurn := expenses.Div(decimal.NewFromInt(int64(elapsedDays)))

	// Projected total at this rate
	projected := dailyBurn.Mul(decimal.NewFromInt(int64(totalDays))).Round(2)

	// Pct of period elapsed
	pctElapsed := decimal.NewFromInt(int64(elapsedDays)).
		Div(decimal.NewFromInt(int64(totalDays))).
		Mul(decimal.NewFromInt(100)).Round(1)

	// Pct of income spent (if income exists)
	pctOfIncome := decimal.Zero
	if d.Totals.TotalIncome.IsPositive() {
		pctOfIncome = expenses.Div(d.Totals.TotalIncome).Mul(decimal.NewFromInt(100)).Round(1)
	}

	severity := "info"
	var msg string

	if d.Totals.TotalIncome.IsPositive() {
		if projected.GreaterThan(d.Totals.TotalIncome.Mul(decimal.NewFromFloat(1.2))) {
			severity = "critical"
			msg = fmt.Sprintf(
				"Llevas %s%% del periodo con un ritmo de gasto de %s/día. De continuar así, gastarás %s (%.1f%% de tus ingresos).",
				pctElapsed.String(), dailyBurn.Round(0).String(), projected.String(),
				projected.Div(d.Totals.TotalIncome).Mul(decimal.NewFromInt(100)).InexactFloat64(),
			)
		} else if projected.GreaterThan(d.Totals.TotalIncome) {
			severity = "warning"
			msg = fmt.Sprintf(
				"Llevas %s%% del periodo gastando %s/día. Tu proyección (%s) supera tus ingresos (%s).",
				pctElapsed.String(), dailyBurn.Round(0).String(), projected.String(), d.Totals.TotalIncome.String(),
			)
		} else {
			msg = fmt.Sprintf(
				"Llevas %s%% del periodo. Ritmo de gasto: %s/día — vas bien (proyección %s vs ingresos %s).",
				pctElapsed.String(), dailyBurn.Round(0).String(), projected.String(), d.Totals.TotalIncome.String(),
			)
		}
	} else {
		// No income registered yet; just report pace.
		msg = fmt.Sprintf(
			"Llevas %s%% del periodo (%d días). Ritmo: %s/día, gastos acumulados %s.",
			pctElapsed.String(), elapsedDays, dailyBurn.Round(0).String(), expenses.String(),
		)
	}

	return &insightDraft{
		InsightType:      "spending_pace",
		CalculationPhase: "during",
		Severity:         severity,
		Title:            "Ritmo de gasto",
		Message:          msg,
		Data: mustJSON(map[string]any{
			"elapsed_days":       elapsedDays,
			"total_days":         totalDays,
			"remaining_days":     remainingDays,
			"pct_elapsed":        pctElapsed.String(),
			"daily_burn":         dailyBurn.Round(2).String(),
			"total_expenses":     expenses.String(),
			"projected_total":    projected.String(),
			"total_income":       d.Totals.TotalIncome.String(),
			"pct_of_income_spent": pctOfIncome.String(),
		}),
	}
}

// genBudgetAlerts returns budget_warning and/or budget_exceeded insights for
// every budget whose spend has crossed the corresponding threshold.
// A budget_exceeded takes priority: if a budget has both crossed the warning
// threshold AND the critical threshold, only budget_exceeded is emitted.
func genBudgetAlerts(d duringPeriodData) []insightDraft {
	if len(d.Budgets) == 0 {
		return nil
	}

	// Build category→spend lookup from the expense-by-category breakdown.
	catSpend := make(map[uuid.UUID]decimal.Decimal)
	var uncategorizedSpend decimal.Decimal
	for _, row := range d.ExpByCategory {
		if row.CategoryID.Valid {
			catSpend[row.CategoryID.UUID] = row.Total
		} else {
			uncategorizedSpend = uncategorizedSpend.Add(row.Total)
		}
	}

	var result []insightDraft

	for _, b := range d.Budgets {
		var spent decimal.Decimal
		if b.CategoryID.Valid {
			spent = catSpend[b.CategoryID.UUID]
		} else {
			// Global budget: compare against total expenses.
			spent = d.Totals.TotalExpenses
		}

		if spent.IsZero() {
			continue
		}

		// pct = spent / budget_amount * 100
		compliancePct := decimal.Zero
		if b.Amount.IsPositive() {
			compliancePct = spent.Div(b.Amount).Mul(decimal.NewFromInt(100)).Round(1)
		}

		var catIDStr *string
		if b.CategoryID.Valid {
			s := b.CategoryID.UUID.String()
			catIDStr = &s
		}

		catLabel := "tu presupuesto global"
		if catIDStr != nil {
			// Use category UUID as fallback label; the client resolves the name.
			catLabel = *catIDStr
		}

		payload := map[string]any{
			"budget_id":      b.ID.String(),
			"category_id":    catIDStr,
			"budget_amount":  b.Amount.String(),
			"spent_amount":   spent.String(),
			"compliance_pct": compliancePct.String(),
		}

		// Critical threshold (budget_exceeded) — higher priority.
		if compliancePct.GreaterThanOrEqual(b.AlertThresholdCritical) {
			result = append(result, insightDraft{
				InsightType:      "budget_exceeded",
				CalculationPhase: "during",
				Severity:         "critical",
				Title:            "Presupuesto superado",
				Message: fmt.Sprintf(
					"Has gastado %s en %s (%s%% de tu presupuesto de %s).",
					spent.String(), catLabel, compliancePct.String(), b.Amount.String(),
				),
				Data:              mustJSON(payload),
				RelatedCategoryID: b.CategoryID,
			})
			continue // skip warning for the same budget
		}

		// Warning threshold.
		if compliancePct.GreaterThanOrEqual(b.AlertThresholdWarning) {
			result = append(result, insightDraft{
				InsightType:      "budget_warning",
				CalculationPhase: "during",
				Severity:         "warning",
				Title:            "Presupuesto próximo al límite",
				Message: fmt.Sprintf(
					"Has usado el %s%% de tu presupuesto para %s (%s de %s).",
					compliancePct.String(), catLabel, spent.String(), b.Amount.String(),
				),
				Data:              mustJSON(payload),
				RelatedCategoryID: b.CategoryID,
			})
		}
	}

	return result
}

// ─── Lazy generators ──────────────────────────────────────────────────────────

// genAntExpensesEarly detects "ant expenses" early in the period: many small
// transactions that individually seem negligible but accumulate quickly.
// Logic mirrors genAntExpensesFinal but uses a lower txn threshold (3 instead
// of 5) to catch the pattern before the period ends.
// Omitted if fewer than 3 expense transactions exist.
func genAntExpensesEarly(d duringPeriodData) *insightDraft {
	if d.Totals.ExpenseTransactionCount < 3 {
		return nil
	}

	avgTxn := d.Totals.TotalExpenses.Div(decimal.NewFromInt(int64(d.Totals.ExpenseTransactionCount)))
	threshold := avgTxn.Mul(decimal.NewFromFloat(0.1))

	type small struct {
		CategoryID   *string `json:"category_id"`
		CategoryName *string `json:"category_name"`
		Total        string  `json:"total"`
		TxnCount     int32   `json:"txn_count"`
	}

	var antTotal decimal.Decimal
	var antCount int32
	var breakdown []small

	for _, row := range d.ExpByCategory {
		if row.TxnCount < 2 {
			continue
		}
		perTxn := row.Total.Div(decimal.NewFromInt(int64(row.TxnCount)))
		if perTxn.LessThan(threshold) {
			antTotal = antTotal.Add(row.Total)
			antCount += row.TxnCount
			var catIDStr *string
			if row.CategoryID.Valid {
				s := row.CategoryID.UUID.String()
				catIDStr = &s
			}
			breakdown = append(breakdown, small{
				CategoryID:   catIDStr,
				CategoryName: row.CategoryName,
				Total:        row.Total.String(),
				TxnCount:     row.TxnCount,
			})
		}
	}

	if antCount < 3 || antTotal.IsZero() {
		return nil
	}

	pct := decimal.Zero
	if d.Totals.TotalExpenses.IsPositive() {
		pct = antTotal.Div(d.Totals.TotalExpenses).Mul(decimal.NewFromInt(100)).Round(1)
	}

	return &insightDraft{
		InsightType:      "ant_expenses_early",
		CalculationPhase: "during",
		Severity:         "warning",
		Title:            "Gastos hormiga detectados",
		Message: fmt.Sprintf(
			"Detectamos %d pequeños gastos que ya suman %s (%s%% de tus gastos del periodo).",
			antCount, antTotal.String(), pct.String(),
		),
		Data: mustJSON(map[string]any{
			"ant_total":    antTotal.String(),
			"ant_count":    antCount,
			"pct_of_total": pct.String(),
			"breakdown":    breakdown,
		}),
	}
}

// genUnusualExpense flags the largest single expense in the period if it
// accounts for more than 30% of total expenses (indicating an unusually large
// transaction).
// Omitted if total expenses < 2 transactions or if the top category accounts
// for < 30% of total expenses.
func genUnusualExpense(d duringPeriodData) *insightDraft {
	if d.Totals.ExpenseTransactionCount < 2 || len(d.ExpByCategory) == 0 {
		return nil
	}

	top := d.ExpByCategory[0]
	pct := decimal.Zero
	if d.Totals.TotalExpenses.IsPositive() {
		pct = top.Total.Div(d.Totals.TotalExpenses).Mul(decimal.NewFromInt(100)).Round(1)
	}

	// Only flag if the category dominates (> 30%) and has few transactions
	// (suggesting a single large purchase rather than normal category usage).
	if pct.LessThan(decimal.NewFromInt(30)) || top.TxnCount > 3 {
		return nil
	}

	catName := "Sin categoría"
	if top.CategoryName != nil {
		catName = *top.CategoryName
	}

	var relCatID uuid.NullUUID
	if top.CategoryID.Valid {
		relCatID = top.CategoryID
	}

	return &insightDraft{
		InsightType:      "unusual_expense",
		CalculationPhase: "during",
		Severity:         "info",
		Title:            "Gasto inusualmente alto",
		Message: fmt.Sprintf(
			"%s representa el %s%% de tus gastos de este periodo (%s). ¿Es esperado?",
			catName, pct.String(), top.Total.String(),
		),
		Data: mustJSON(map[string]any{
			"category_id":   func() *string {
				if top.CategoryID.Valid {
					s := top.CategoryID.UUID.String()
					return &s
				}
				return nil
			}(),
			"category_name": top.CategoryName,
			"total":         top.Total.String(),
			"txn_count":     top.TxnCount,
			"pct_of_total":  pct.String(),
		}),
		RelatedCategoryID: relCatID,
	}
}

// genVsPreviousPartial compares the current partial spend (elapsed days) against
// the same number of elapsed days in the previous period.
// Omitted if there is no previous period summary.
func genVsPreviousPartial(d duringPeriodData) *insightDraft {
	if d.PrevSummary == nil {
		return nil
	}

	prev := d.PrevSummary
	start := d.Period.StartDate.Time
	elapsedDays := int(d.Today.Sub(start).Hours()/24) + 1
	totalDays := int(d.Period.EndDate.Time.Sub(start).Hours()/24) + 1
	if totalDays <= 0 {
		return nil
	}

	// Estimate the equivalent spend at the same fraction of the previous period.
	// We use TotalExpenses from the summary (full period) and scale by elapsed fraction.
	elapsedFraction := decimal.NewFromInt(int64(elapsedDays)).
		Div(decimal.NewFromInt(int64(totalDays)))

	prevEstimate := prev.TotalExpenses.Mul(elapsedFraction).Round(2)
	currentSpend := d.Totals.TotalExpenses

	delta := currentSpend.Sub(prevEstimate)
	changePct := decimal.Zero
	if prevEstimate.IsPositive() {
		changePct = delta.Div(prevEstimate).Mul(decimal.NewFromInt(100)).Round(1)
	}

	severity := "info"
	direction := "similar"
	if delta.IsPositive() && changePct.GreaterThan(decimal.NewFromInt(15)) {
		severity = "warning"
		direction = "mayor"
	} else if delta.IsNegative() && changePct.Abs().GreaterThan(decimal.NewFromInt(10)) {
		severity = "success"
		direction = "menor"
	}

	pctElapsed := elapsedFraction.Mul(decimal.NewFromInt(100)).Round(0)

	return &insightDraft{
		InsightType:      "vs_previous_partial",
		CalculationPhase: "during",
		Severity:         severity,
		Title:            "Comparativa parcial con periodo anterior",
		Message: fmt.Sprintf(
			"Con el %s%% del periodo transcurrido, tu gasto (%s) es %s al del mismo punto del periodo anterior (%s estimado).",
			pctElapsed.String(), currentSpend.String(), direction, prevEstimate.String(),
		),
		Data: mustJSON(map[string]any{
			"elapsed_days":        elapsedDays,
			"total_days":          totalDays,
			"pct_elapsed":         pctElapsed.String(),
			"current_expenses":    currentSpend.String(),
			"previous_estimate":   prevEstimate.String(),
			"previous_total":      prev.TotalExpenses.String(),
			"delta":               delta.String(),
			"expense_change_pct":  changePct.String(),
			"previous_period_id":  prev.TrackingPeriodID.String(),
		}),
	}
}

// genGoalProgressAlerts checks each active savings goal whose date range
// intersects the current period and emits an alert if the goal is behind
// its expected pace (contributed less than the pro-rated share of target so far).
// Omitted when there are no active goals or no contributions in the period.
func genGoalProgressAlerts(d duringPeriodData) []insightDraft {
	if len(d.Goals) == 0 {
		return nil
	}

	today := d.Today

	var result []insightDraft

	for _, g := range d.Goals {
		if g.Status != "active" {
			continue
		}

		goalStart := g.StartDate.Time
		goalEnd := g.TargetDate.Time

		// Skip goals that haven't started or have already ended.
		if today.Before(goalStart) || today.After(goalEnd) {
			continue
		}

		totalGoalDays := int(goalEnd.Sub(goalStart).Hours()/24) + 1
		elapsedGoalDays := int(today.Sub(goalStart).Hours()/24) + 1
		if totalGoalDays <= 0 || elapsedGoalDays <= 0 {
			continue
		}

		remaining := g.TargetAmount.Sub(g.CurrentAmount)
		if remaining.IsNegative() || remaining.IsZero() {
			continue // already achieved
		}

		// Expected progress: fraction of goal elapsed * target amount.
		expectedAmount := g.TargetAmount.
			Mul(decimal.NewFromInt(int64(elapsedGoalDays))).
			Div(decimal.NewFromInt(int64(totalGoalDays))).Round(2)

		if g.CurrentAmount.GreaterThanOrEqual(expectedAmount) {
			continue // on track — no alert needed
		}

		gap := expectedAmount.Sub(g.CurrentAmount)

		pctComplete := decimal.Zero
		if g.TargetAmount.IsPositive() {
			pctComplete = g.CurrentAmount.Div(g.TargetAmount).Mul(decimal.NewFromInt(100)).Round(1)
		}

		relGoalID := uuid.NullUUID{UUID: g.ID, Valid: true}

		result = append(result, insightDraft{
			InsightType:      "goal_progress_alert",
			CalculationPhase: "during",
			Severity:         "warning",
			Title:            "Meta de ahorro por detrás",
			Message: fmt.Sprintf(
				"La meta '%s' lleva %s%% completada pero debería estar en %s. Tienes un déficit de %s.",
				g.Name, pctComplete.String(), expectedAmount.String(), gap.String(),
			),
			Data: mustJSON(map[string]any{
				"goal_id":          g.ID.String(),
				"goal_name":        g.Name,
				"target_amount":    g.TargetAmount.String(),
				"current_amount":   g.CurrentAmount.String(),
				"expected_amount":  expectedAmount.String(),
				"gap":              gap.String(),
				"pct_complete":     pctComplete.String(),
				"elapsed_goal_days": elapsedGoalDays,
				"total_goal_days":  totalGoalDays,
			}),
			RelatedGoalID: relGoalID,
		})
	}

	return result
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// neverExpires returns a zero-value pgtype.Timestamptz (valid_until = NULL),
// used for "during" insights that do not have a hard expiry time.
func neverExpires() pgtype.Timestamptz {
	return pgtype.Timestamptz{}
}

// bogotaToday returns the current date in the America/Bogota timezone,
// normalized to UTC midnight (matching the dateOnly convention used across
// the codebase). Falls back to UTC if the tz database is unavailable.
func bogotaToday() time.Time {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
