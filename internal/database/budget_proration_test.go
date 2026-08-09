package database

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
)

func period(start, end string, isTransition bool) sqlc.TrackingPeriod {
	parse := func(s string) pgtype.Date {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return pgtype.Date{Time: t, Valid: true}
	}
	return sqlc.TrackingPeriod{
		StartDate:    parse(start),
		EndDate:      parse(end),
		IsTransition: isTransition,
	}
}

func TestBudgetProrationFactor(t *testing.T) {
	tests := []struct {
		name string
		from sqlc.TrackingPeriod
		to   sqlc.TrackingPeriod
		want string
	}{
		{
			// The common case. A 30-to-31-day drift silently moving someone's
			// budget would be baffling, so regular periods copy verbatim.
			name: "two regular periods copy verbatim",
			from: period("2026-08-01", "2026-08-31", false),
			to:   period("2026-09-01", "2026-09-30", false),
			want: "1",
		},
		{
			name: "regular into a short bridge scales down",
			from: period("2026-08-05", "2026-09-03", false), // 30 days
			to:   period("2026-09-04", "2026-09-30", true),  // 27 days
			want: "0.9",
		},
		{
			name: "short bridge into a full month scales back up",
			from: period("2026-09-04", "2026-09-30", true),  // 27 days
			to:   period("2026-10-01", "2026-10-31", false), // 31 days
			want: "1.1481481481481481",
		},
		{
			name: "regular into a long bridge scales up",
			from: period("2026-08-27", "2026-09-25", false), // 30 days
			to:   period("2026-09-26", "2026-10-31", true),  // 36 days
			want: "1.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := budgetProrationFactor(tt.from, tt.to)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

func TestProrateAmount(t *testing.T) {
	amount := decimal.RequireFromString("300000.00")

	// A factor of exactly 1 must leave the amount untouched, not round-trip it.
	assert.Equal(t, amount, prorateAmount(amount, decimal.NewFromInt(1)))

	// 30-day period into a 27-day bridge.
	assert.Equal(t, "270000", prorateAmount(amount, decimal.RequireFromString("0.9")).String())

	// Money always lands on 2 decimals.
	third := prorateAmount(decimal.RequireFromString("100.00"), decimal.NewFromInt(1).Div(decimal.NewFromInt(3)))
	assert.Equal(t, "33.33", third.String())
}

// A zero-length period would be a data bug, but a division by zero in the close
// transaction would take down the rollover for that user entirely. Degrade to a
// verbatim copy instead.
func TestBudgetProrationFactorGuardsAgainstEmptyPeriods(t *testing.T) {
	empty := sqlc.TrackingPeriod{IsTransition: true}
	regular := period("2026-08-01", "2026-08-31", false)

	assert.Equal(t, "1", budgetProrationFactor(empty, regular).String())
	assert.Equal(t, "1", budgetProrationFactor(regular, empty).String())
}
