package domain

import "time"

// Tracking period modes. A user's periods are either rolling blocks of
// tracking_duration_days days chained end to end, or real calendar months.
const (
	PeriodModeRolling  = "rolling"
	PeriodModeCalendar = "calendar_month"
)

// ValidPeriodModes is the allow-list checked before the value reaches Postgres,
// so a bad mode surfaces as a 422 instead of a CHECK-violation 500.
var ValidPeriodModes = map[string]bool{
	PeriodModeRolling:  true,
	PeriodModeCalendar: true,
}

// minBridgeDays is the floor for a transition period.
//
// When a user switches to calendar mode their current period rarely ends on the
// last day of a month, so the stretch up to the next 1st has to be bridged. That
// leftover is whatever it happens to be — anything shorter than this gets
// absorbed into the following month instead of becoming a period of its own. A
// two-day "seguimiento" carrying its own summary, insights and budgets is noise,
// not information.
//
// With this floor a bridge is always between 15 and 45 days.
const minBridgeDays = 15

// PeriodRange is an inclusive [Start, End] date range, plus the flag marking it
// as a one-off bridge rather than a regular period. Transition periods are the
// only ones allowed to fall outside the usual 28-31 day window.
type PeriodRange struct {
	Start        time.Time
	End          time.Time
	IsTransition bool
}

// DurationDays returns the inclusive length of the range in days.
func (r PeriodRange) DurationDays() int {
	return inclusiveDays(r.Start, r.End)
}

// NextPeriodRange computes the period that follows one ending on prevEnd.
//
// In rolling mode it chains a block of durationDays days, which is what Balvia
// has always done. In calendar mode it returns a real calendar month — except on
// the first hop after a mode switch, where prevEnd rarely lands on a month
// boundary and the gap has to be bridged. See minBridgeDays.
//
// Pure date arithmetic: no clock, no database. Dates come back at midnight in
// prevEnd's location.
func NextPeriodRange(prevEnd time.Time, mode string, durationDays int) PeriodRange {
	start := dayStart(prevEnd).AddDate(0, 0, 1)

	if mode != PeriodModeCalendar {
		return PeriodRange{Start: start, End: start.AddDate(0, 0, durationDays-1)}
	}

	// Already on a month boundary: a clean calendar month, no bridging needed.
	// This is the steady state once a user has settled into calendar mode.
	if start.Day() == 1 {
		return PeriodRange{Start: start, End: endOfMonth(start)}
	}

	return PeriodRange{Start: start, End: bridgeEnd(start), IsTransition: true}
}

// FirstPeriodRange computes a user's very first period, which starts today
// rather than after a predecessor. Calendar mode runs to the end of the current
// month, applying the same floor so registering on the 29th does not produce a
// two-day first period.
func FirstPeriodRange(today time.Time, mode string, durationDays int) PeriodRange {
	start := dayStart(today)

	if mode != PeriodModeCalendar {
		return PeriodRange{Start: start, End: start.AddDate(0, 0, durationDays-1)}
	}

	if start.Day() == 1 {
		return PeriodRange{Start: start, End: endOfMonth(start)}
	}

	return PeriodRange{Start: start, End: bridgeEnd(start), IsTransition: true}
}

// bridgeEnd picks where a transition period should stop: the end of start's own
// month when that leaves a usable stretch, otherwise the end of the month after.
func bridgeEnd(start time.Time) time.Time {
	end := endOfMonth(start)
	if inclusiveDays(start, end) < minBridgeDays {
		end = endOfMonth(firstOfMonth(start).AddDate(0, 1, 0))
	}
	return end
}

// endOfMonth returns the last day of t's month. Going through the 1st keeps the
// arithmetic leap-safe: AddDate normalises overflow (Jan 31 + 1 month = Mar 3),
// which would silently skip February from any other day of the month.
func endOfMonth(t time.Time) time.Time {
	return firstOfMonth(t).AddDate(0, 1, 0).AddDate(0, 0, -1)
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// inclusiveDays counts both endpoints. Rounding to whole days absorbs the hour
// a DST boundary would otherwise shave off the subtraction.
func inclusiveDays(start, end time.Time) int {
	return int(end.Sub(start).Round(24*time.Hour).Hours()/24) + 1
}
