package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNextPeriodRange(t *testing.T) {
	tests := []struct {
		name         string
		prevEnd      string
		mode         string
		duration     int
		wantStart    string
		wantEnd      string
		wantTransit  bool
		wantDuration int
	}{
		{
			name:         "rolling chains a block of the configured length",
			prevEnd:      "2026-09-03",
			mode:         PeriodModeRolling,
			duration:     30,
			wantStart:    "2026-09-04",
			wantEnd:      "2026-10-03",
			wantDuration: 30,
		},
		{
			name:         "rolling honours a 28 day duration",
			prevEnd:      "2026-09-03",
			mode:         PeriodModeRolling,
			duration:     28,
			wantStart:    "2026-09-04",
			wantEnd:      "2026-10-01",
			wantDuration: 28,
		},
		{
			name:         "calendar already aligned yields a clean month",
			prevEnd:      "2026-08-31",
			mode:         PeriodModeCalendar,
			duration:     30,
			wantStart:    "2026-09-01",
			wantEnd:      "2026-09-30",
			wantDuration: 30,
		},
		{
			name:         "calendar mid month with room to spare bridges short",
			prevEnd:      "2026-09-03",
			mode:         PeriodModeCalendar,
			duration:     30,
			wantStart:    "2026-09-04",
			wantEnd:      "2026-09-30",
			wantTransit:  true,
			wantDuration: 27,
		},
		{
			// Exactly at the floor: 15 days left, so the bridge stays short.
			name:         "calendar with exactly the floor left bridges short",
			prevEnd:      "2026-09-15",
			mode:         PeriodModeCalendar,
			duration:     30,
			wantStart:    "2026-09-16",
			wantEnd:      "2026-09-30",
			wantTransit:  true,
			wantDuration: 15,
		},
		{
			// One day below the floor: absorbed into the following month.
			name:         "calendar one day under the floor bridges long",
			prevEnd:      "2026-09-16",
			mode:         PeriodModeCalendar,
			duration:     30,
			wantStart:    "2026-09-17",
			wantEnd:      "2026-10-31",
			wantTransit:  true,
			wantDuration: 45,
		},
		{
			name:         "calendar with too little left absorbs the next month",
			prevEnd:      "2026-09-25",
			mode:         PeriodModeCalendar,
			duration:     30,
			wantStart:    "2026-09-26",
			wantEnd:      "2026-10-31",
			wantTransit:  true,
			wantDuration: 36,
		},
		{
			name:         "calendar bridging out of January lands on a leap February",
			prevEnd:      "2028-01-20",
			mode:         PeriodModeCalendar,
			duration:     30,
			wantStart:    "2028-01-21",
			wantEnd:      "2028-02-29",
			wantTransit:  true,
			wantDuration: 40,
		},
		{
			name:         "calendar bridging out of January lands on a common February",
			prevEnd:      "2026-01-20",
			mode:         PeriodModeCalendar,
			duration:     30,
			wantStart:    "2026-01-21",
			wantEnd:      "2026-02-28",
			wantTransit:  true,
			wantDuration: 39,
		},
		{
			name:         "calendar steady state through February",
			prevEnd:      "2026-01-31",
			mode:         PeriodModeCalendar,
			duration:     30,
			wantStart:    "2026-02-01",
			wantEnd:      "2026-02-28",
			wantDuration: 28,
		},
		{
			name:         "calendar steady state across a year boundary",
			prevEnd:      "2026-12-31",
			mode:         PeriodModeCalendar,
			duration:     30,
			wantStart:    "2027-01-01",
			wantEnd:      "2027-01-31",
			wantDuration: 31,
		},
		{
			name:         "calendar bridging from the last day of a month",
			prevEnd:      "2026-09-29",
			mode:         PeriodModeCalendar,
			duration:     30,
			wantStart:    "2026-09-30",
			wantEnd:      "2026-10-31",
			wantTransit:  true,
			wantDuration: 32,
		},
		{
			name:         "switching back to rolling from a calendar month needs no bridge",
			prevEnd:      "2026-09-30",
			mode:         PeriodModeRolling,
			duration:     30,
			wantStart:    "2026-10-01",
			wantEnd:      "2026-10-30",
			wantDuration: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextPeriodRange(d(tt.prevEnd), tt.mode, tt.duration)

			assert.Equal(t, d(tt.wantStart), got.Start, "start")
			assert.Equal(t, d(tt.wantEnd), got.End, "end")
			assert.Equal(t, tt.wantTransit, got.IsTransition, "is transition")
			assert.Equal(t, tt.wantDuration, got.DurationDays(), "duration")
		})
	}
}

// The whole point of minBridgeDays is that a bridge is never absurd. Sweep every
// possible hand-off date for two years and assert the guarantee holds, so a
// future tweak to the threshold cannot quietly produce a two-day "seguimiento".
func TestNextPeriodRangeBridgeStaysWithinBounds(t *testing.T) {
	const maxBridgeDays = 62 // the widened CHECK in migration 000018

	for day := d("2026-01-01"); day.Before(d("2028-01-01")); day = day.AddDate(0, 0, 1) {
		got := NextPeriodRange(day, PeriodModeCalendar, 30)

		require.False(t, got.End.Before(got.Start), "end before start for prevEnd=%s", day.Format("2006-01-02"))

		if !got.IsTransition {
			// A clean calendar month always fits the original 28-31 window.
			assert.GreaterOrEqual(t, got.DurationDays(), 28, "clean month too short for prevEnd=%s", day.Format("2006-01-02"))
			assert.LessOrEqual(t, got.DurationDays(), 31, "clean month too long for prevEnd=%s", day.Format("2006-01-02"))
			continue
		}

		assert.GreaterOrEqual(t, got.DurationDays(), minBridgeDays, "bridge too short for prevEnd=%s", day.Format("2006-01-02"))
		assert.LessOrEqual(t, got.DurationDays(), maxBridgeDays, "bridge too long for prevEnd=%s", day.Format("2006-01-02"))

		// A bridge must always land on a month boundary, otherwise the period
		// after it would need bridging too and calendar mode would never settle.
		assert.Equal(t, got.End.AddDate(0, 0, 1).Day(), 1, "bridge does not end on a month boundary for prevEnd=%s", day.Format("2006-01-02"))
	}
}

func TestFirstPeriodRange(t *testing.T) {
	tests := []struct {
		name        string
		today       string
		mode        string
		duration    int
		wantStart   string
		wantEnd     string
		wantTransit bool
	}{
		{
			name:      "rolling starts today and runs the configured length",
			today:     "2026-08-08",
			mode:      PeriodModeRolling,
			duration:  30,
			wantStart: "2026-08-08",
			wantEnd:   "2026-09-06",
		},
		{
			name:        "calendar mid month runs to the end of the month",
			today:       "2026-08-08",
			mode:        PeriodModeCalendar,
			duration:    30,
			wantStart:   "2026-08-08",
			wantEnd:     "2026-08-31",
			wantTransit: true,
		},
		{
			name:      "calendar on the first is already a clean month",
			today:     "2026-08-01",
			mode:      PeriodModeCalendar,
			duration:  30,
			wantStart: "2026-08-01",
			wantEnd:   "2026-08-31",
		},
		{
			name:        "calendar late in the month absorbs the next one",
			today:       "2026-08-29",
			mode:        PeriodModeCalendar,
			duration:    30,
			wantStart:   "2026-08-29",
			wantEnd:     "2026-09-30",
			wantTransit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FirstPeriodRange(d(tt.today), tt.mode, tt.duration)

			assert.Equal(t, d(tt.wantStart), got.Start, "start")
			assert.Equal(t, d(tt.wantEnd), got.End, "end")
			assert.Equal(t, tt.wantTransit, got.IsTransition, "is transition")
		})
	}
}

// FirstPeriodRange is handed a wall clock, not a date, so it must floor the time
// component — otherwise the period would start at 14:32 and every inclusive day
// count downstream would be off by one.
func TestFirstPeriodRangeFloorsTheClock(t *testing.T) {
	noon := time.Date(2026, 8, 8, 14, 32, 7, 500, time.UTC)

	got := FirstPeriodRange(noon, PeriodModeRolling, 30)

	assert.Equal(t, d("2026-08-08"), got.Start)
	assert.Equal(t, 30, got.DurationDays())
}

func TestPeriodRangeDurationDays(t *testing.T) {
	// Inclusive on both ends: a single-day range is 1 day, not 0.
	assert.Equal(t, 1, PeriodRange{Start: d("2026-08-08"), End: d("2026-08-08")}.DurationDays())
	assert.Equal(t, 31, PeriodRange{Start: d("2026-08-01"), End: d("2026-08-31")}.DurationDays())
}

func TestValidPeriodModes(t *testing.T) {
	assert.True(t, ValidPeriodModes[PeriodModeRolling])
	assert.True(t, ValidPeriodModes[PeriodModeCalendar])
	assert.False(t, ValidPeriodModes["monthly"])
	assert.False(t, ValidPeriodModes[""])
}
