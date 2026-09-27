package main

// schedule_slot.go — scheduled-message slot arithmetic. The fire/skip decision is
// "name the most recent elapsed slot with a stable string and compare it to the
// string already delivered" (see migrations/00050 for why the cursor is a slot
// identifier rather than a clock reading).

import (
	"fmt"
	"time"

	// 🔴 Embed the IANA tz database: without it time.LoadLocation reads the HOST's
	// /usr/share/zoneinfo, absent in a slim container, and every timezone then
	// silently resolves to something else — messages keep arriving at the wrong hour.
	_ "time/tzdata"
)

// slotKeyLayout: the zone OFFSET is part of the identity on purpose — the same
// wall-clock reading on the two sides of a DST transition is two instants, and a
// cursor that could not tell them apart would resend or skip one. `-07:00`
// (never `Z`) keeps a UTC schedule rendering as `+00:00`.
const slotKeyLayout = "2006-01-02T15:04-07:00"

// monthlyLookbackMonths: worst case is day_of_month=31 with now = 1 March — the
// answer is 31 JANUARY, two months back. Looking back only one step finds nothing
// and the schedule silently never fires. Beyond two is bounded headroom.
const monthlyLookbackMonths = 12

// dailyLookbackDays counts steps back from today (four dates in all). Worst case
// constructed is two steps: today's slot still ahead and yesterday a date the zone
// deleted outright (Pacific/Apia skipped 30 December 2011). A reasoned bound, NOT
// a measured maximum over every zone; the rest is headroom.
const dailyLookbackDays = 3

// weeklyLookbackDays: steps back from today (fifteen dates). Seven finds the
// previous weekday; fourteen covers that occurrence landing on a date the zone
// deleted.
const weeklyLookbackDays = 14

// customYearsBack / customLeapDayYearsBack bound the calendar derivation `custom`
// uses instead of a scan window. A window cannot work: months {2} × days {29} can
// go EIGHT years between occurrences (2096 → 2104), and a 70-day window made
// mostRecentSlot non-monotonic (months {1} × days {1} reported its slot at day+70
// and nothing at day+71).
//
// 🔴 THE BOUND IS A PROOF, NOT HEADROOM. Any feasible pair other than (2, 29)
// occurs every year, so the current year plus one is exhaustive; the extra year
// absorbs a year whose declared date the zone deletes. (2, 29) recurs in four
// years, eight across a non-leap century, hence 9. These only bite if a zone
// deletes every declared date for customYearsBack CONSECUTIVE years — re-derive
// against that condition.
const (
	customYearsBack        = 2
	customLeapDayYearsBack = 9
)

// 🔴 It must be MONOTONIC in `now` (see dayAnchor for the trap that broke it).
// runScheduledMessageTick refuses to move the cursor backwards, so a future
// non-monotonic answer costs a delivery rather than duplicating one.
// ok=false means "do not deliver"; there is NO fallback zone (see
// ValidateScheduledMessageTimezone).
func mostRecentSlot(s ScheduledMessage, now time.Time) (time.Time, bool) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(loc)
	switch s.Cadence {
	case ScheduledMessageCadenceDaily:
		anchor := dayAnchor(local)
		for back := 0; back <= dailyLookbackDays; back++ {
			day := anchor.AddDate(0, 0, -back)
			if slot, exists := slotOn(day, s, loc); exists && !slot.After(local) {
				return slot, true
			}
		}
		return time.Time{}, false

	case ScheduledMessageCadenceWeekly:
		anchor := dayAnchor(local)
		for back := 0; back <= weeklyLookbackDays; back++ {
			day := anchor.AddDate(0, 0, -back)
			if int(day.Weekday()) != s.DayOfWeek {
				continue
			}
			if slot, exists := slotOn(day, s, loc); exists && !slot.After(local) {
				return slot, true
			}
		}
		return time.Time{}, false

	case ScheduledMessageCadenceMonthly:
		year, month := local.Year(), local.Month()
		for back := 0; back <= monthlyLookbackMonths; back++ {
			slot, exists := monthlySlot(year, month, s, loc)
			if exists && !slot.After(local) {
				return slot, true
			}
			// Step back on (year, month) directly, not via AddDate, which would
			// normalise 31 March minus one month into 3 March and skip months.
			month--
			if month < time.January {
				month = time.December
				year--
			}
		}
		return time.Time{}, false

	case ScheduledMessageCadenceCustom:
		return mostRecentCustomSlot(s, loc, local)
	}
	schedLog("skip %s: unknown cadence %q — this schedule can never fire", s.ID, s.Cadence)
	return time.Time{}, false
}

// A listed day the month lacks is never clamped onto the month's last date (the
// RFC 5545 rule): [1,15,31] in February fires on the 1st and 15th only.
//
// 🔴 The feasibility pre-check is what lets customYearsBack be a proof. It shares
// maxDaysInMonth with the write seam (ValidateScheduledMessageCustomSets): a second
// calendar copy that disagreed would make a schedule silently never fire.
func mostRecentCustomSlot(s ScheduledMessage, loc *time.Location, local time.Time) (time.Time, bool) {
	months, days := sortedIntSet(s.CustomMonths), sortedIntSet(s.CustomDays)
	if len(months) == 0 || len(days) == 0 || len(s.CustomHours) == 0 || len(s.CustomMinutes) == 0 {
		return time.Time{}, false
	}
	feasible, onlyLeapDay := false, true
	for _, m := range months {
		for _, d := range days {
			if d > maxDaysInMonth(m) {
				continue
			}
			feasible = true
			if m != 2 || d != 29 {
				onlyLeapDay = false
			}
		}
	}
	if !feasible {
		return time.Time{}, false
	}
	yearsBack := customYearsBack
	if onlyLeapDay {
		yearsBack = customLeapDayYearsBack
	}
	// ⚠️ The two "still ahead of now" skips below are COST, not correctness:
	// customSlotOn's notAfter already excludes future readings (deleting both
	// skips left every test green), but they cut 30.3ms to 96.6µs on a 24 × 60
	// schedule in mid-January. Neither a guard nor redundant.
	for year := local.Year(); year >= local.Year()-yearsBack; year-- {
		for mi := len(months) - 1; mi >= 0; mi-- {
			month := time.Month(months[mi])
			if year == local.Year() && month > local.Month() {
				continue
			}
			for di := len(days) - 1; di >= 0; di-- {
				day := days[di]
				if year == local.Year() && month == local.Month() && day > local.Day() {
					continue
				}
				candidate := time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
				if candidate.Year() != year || candidate.Month() != month || candidate.Day() != day {
					continue
				}
				if slot, ok := customSlotOn(candidate, s, loc, local); ok {
					return slot, true
				}
			}
		}
	}
	return time.Time{}, false
}

// 🔴 DELIBERATE DST ASYMMETRY (owner-ruled): slotAt moves a skipped (spring
// forward) reading FORWARD; `custom` SKIPS it, because with many readings a day a
// forward search tends to land on a reading already in the set — same slotKey,
// second delivery silently merged. Cost: a single-reading `custom` schedule loses
// the whole day where the equivalent `monthly` fires late.
//
// ⚠️ Autumn: a repeated wall reading resolves via time.Date to ONE instant, and it
// is NOT always the earlier offset — measured, America/New_York resolves
// 2024-11-03 01:30 to the earlier instant while Europe/London and Africa/Cairo
// resolve to the LATER one. We depend only on determinism (one reading → one
// slotKey → fires once); the offset direction is deliberately not pinned, and it
// lives in the readBack/time.Date path all four cadences share.
func customSlotOn(day time.Time, s ScheduledMessage, loc *time.Location, notAfter time.Time) (time.Time, bool) {
	year, month, dayNum := day.Year(), day.Month(), day.Day()
	if _, exists := firstReadingOn(year, month, dayNum, loc); !exists {
		return time.Time{}, false
	}
	hours, minutes := sortedIntSet(s.CustomHours), sortedIntSet(s.CustomMinutes)
	for hi := len(hours) - 1; hi >= 0; hi-- {
		for mi := len(minutes) - 1; mi >= 0; mi-- {
			slot, ok := readBack(time.Date(year, month, dayNum, hours[hi], minutes[mi], 0, 0, time.UTC), loc)
			if !ok {
				continue
			}
			if !slot.After(notAfter) {
				return slot, true
			}
		}
	}
	return time.Time{}, false
}

func intSetContains(vals []int, want int) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}

// dayAnchor names t's calendar DATE as a noon-UTC instant for day arithmetic.
//
// 🔴 Never do day arithmetic on a local time: when yesterday's midnight is an hour
// the zone skipped (America/Santiago 2026-09-06, America/Havana 2026-03-08),
// `local.AddDate(0,0,-1)` normalises BACKWARDS into the day before, the cursor
// walks backwards and the tick redelivers slots it already sent. Noon, not
// midnight, because midnight is the reading zones actually skip.
func dayAnchor(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, time.UTC)
}

func slotOn(day time.Time, s ScheduledMessage, loc *time.Location) (time.Time, bool) {
	return slotAt(day.Year(), day.Month(), day.Day(), s, loc)
}

func monthlySlot(year int, month time.Month, s ScheduledMessage, loc *time.Location) (time.Time, bool) {
	return slotAt(year, month, s.DayOfMonth, s, loc)
}

// 🔴 "This date is not in this zone" is decided ONLY by the date having no reading
// at all (firstReadingOn), never by how far a forward walk got: a 120-minute walk
// bound dropped Antarctica/Casey's 180-minute gap, and a day-end bound misread
// America/Nuuk's 23:00 → 00:00 jump as a missing date and silently skipped.
// The read-back test is slightly stricter than "deleted" (Africa/Casablanca cases
// cost an hour's delay, not a delivery). The wall-clock question is the +1-minute
// walk, which matches svc-automation's _first_existing_instant and may cross
// midnight onto the next date.

func firstReadingOn(year int, month time.Month, day int, loc *time.Location) (time.Time, bool) {
	start := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	for want := start; want.Day() == day && want.Month() == month && want.Year() == year; want = want.Add(time.Minute) {
		if slot, ok := readBack(want, loc); ok {
			return slot, true
		}
	}
	return time.Time{}, false
}

// ⚠️ readBack compares every component because time.Date NORMALISES a reading it
// cannot honour, in both directions: 00:30 on 2026-03-08 in America/Havana becomes
// 03-07 23:30; 02:15 on 2026-10-04 in Australia/Lord_Howe becomes 02:45 on the
// same date, which a day-only check waves through.
func readBack(want time.Time, loc *time.Location) (time.Time, bool) {
	slot := time.Date(want.Year(), want.Month(), want.Day(),
		want.Hour(), want.Minute(), 0, 0, loc)
	ok := slot.Year() == want.Year() && slot.Month() == want.Month() && slot.Day() == want.Day() &&
		slot.Hour() == want.Hour() && slot.Minute() == want.Minute()
	return slot, ok
}

// slotAt (owner ruling rc-aeef15360ab5, RFC 5545): a date that does not exist
// (31 February, or deleted by the zone) is DROPPED; a wall clock that does not
// exist MOVES FORWARD to the next reading, even onto the next date.
//
// 🔴 The slot is always CONSTRUCTED from (date, hour, minute, zone), never derived
// from an offset of `now`, so a wall clock occurring twice constructs one instant
// and the second pass delivers nothing.
func slotAt(year int, month time.Month, day int, s ScheduledMessage, loc *time.Location) (time.Time, bool) {
	wall := time.Date(year, month, day, s.Hour, s.Minute, 0, 0, time.UTC)
	if wall.Year() != year || wall.Month() != month || wall.Day() != day {
		return time.Time{}, false
	}
	if _, exists := firstReadingOn(year, month, day, loc); !exists {
		return time.Time{}, false
	}
	for want := wall; want.Day() == day && want.Month() == month && want.Year() == year; want = want.Add(time.Minute) {
		if slot, ok := readBack(want, loc); ok {
			return slot, true
		}
	}
	next := time.Date(year, month, day+1, 12, 0, 0, 0, time.UTC)
	return firstReadingOn(next.Year(), next.Month(), next.Day(), loc)
}

// slotKey renders the identifier stored in last_fired_slot.
func slotKey(slot time.Time) string {
	return slot.Format(slotKeyLayout)
}

// 🔴 slotIsAfterCursor is an ordering test, not string inequality: a slot that
// ever moves BACKWARDS differs from the cursor and would redeliver. Comparing
// instants makes the cursor a ratchet (the worst case is a discoverable skip).
// An empty or unparseable cursor fires: rows written before the cursor existed
// would otherwise be stranded forever.
func slotIsAfterCursor(slot time.Time, cursor string) bool {
	previous, err := time.Parse(slotKeyLayout, cursor)
	if err != nil {
		return true
	}
	return slot.After(previous)
}

// currentSlotKey seeds last_fired_slot at creation and re-aims it on edit, so a
// schedule never fires the slot it was born after or crossed.
func currentSlotKey(s ScheduledMessage, now time.Time) string {
	slot, ok := mostRecentSlot(s, now)
	if !ok {
		return ""
	}
	return slotKey(slot)
}

// describeSchedule: `custom` prints its sets because Hour/Minute hold 0/0
// defaults on a custom row.
func describeSchedule(s ScheduledMessage) string {
	if s.Cadence == ScheduledMessageCadenceCustom {
		return fmt.Sprintf("%s (custom months=[%s] days=[%s] hours=[%s] minutes=[%s] %s)", s.ID,
			canonicalIntSet(s.CustomMonths), canonicalIntSet(s.CustomDays),
			canonicalIntSet(s.CustomHours), canonicalIntSet(s.CustomMinutes), s.Timezone)
	}
	return fmt.Sprintf("%s (%s %02d:%02d %s)", s.ID, s.Cadence, s.Hour, s.Minute, s.Timezone)
}
