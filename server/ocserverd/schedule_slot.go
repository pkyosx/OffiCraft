package main

// schedule_slot.go — T-f059 定期訊息: the slot arithmetic, kept as PURE
// functions with no DAL, no HTTP and no ambient clock so every rule below is
// testable by handing it a struct and a `now`.
//
// The whole feature rests on one question: "what is the most recent slot of
// this schedule that has already elapsed?" Answer it, name that slot with a
// stable string, and compare against the string already delivered — that is the
// entire fire/skip decision (see migrations/00050 for why the cursor is a slot
// identifier rather than a clock reading).

import (
	"fmt"
	"time"

	// 🔴 Embed the IANA tz database into the binary. Without this,
	// time.LoadLocation reads /usr/share/zoneinfo from the HOST: present on
	// macOS and on ordinary Linux, absent in a slim container — and the failure
	// mode there is not "the scheduler broke". It is "every timezone silently
	// resolves to something else and the messages keep arriving, at the wrong
	// hour". Nothing alarms on that. ~450 KB of binary buys host independence.
	// This is the first and only tz-database dependency in the repo.
	_ "time/tzdata"
)

// slotKeyLayout is the canonical rendering of a slot, e.g.
// `2026-08-10T09:00+08:00`. The zone OFFSET is part of the identity on purpose:
// the same wall-clock reading on the two sides of a DST transition is two
// different instants, and a cursor that could not tell them apart would either
// resend or skip one. `-07:00` (numeric, never `Z`) keeps the rendering uniform
// so a UTC schedule reads `+00:00` rather than switching shape.
const slotKeyLayout = "2006-01-02T15:04-07:00"

// monthlyLookbackMonths bounds how far back a monthly schedule searches for a
// month that actually CONTAINS its day_of_month.
//
// 🔴 Looking back only ONE step is the bug this constant exists to prevent, and
// it fails silently: with day_of_month=31 and now = 1 March, March's 31st has
// not arrived and February has no 31st at all, so the correct answer is 31
// JANUARY — two months back. A shorter search finds nothing, returns "no slot",
// and the schedule simply never fires, with nothing to observe. Two months back
// is the floor this constant has to clear; the rest is bounded headroom, not a
// claim that any real schedule needs twelve.
const monthlyLookbackMonths = 12

// dailyLookbackDays bounds how far back a daily schedule searches for a date its
// zone actually HAS. It counts STEPS BACK from today, so the search covers today
// plus this many earlier dates — four in all. Two steps is the worst case anyone
// has constructed (today's slot still ahead, and yesterday a date the zone
// deleted outright — Pacific/Apia skipped 30 December 2011 for the date-line
// move); that is a reasoned bound, NOT a measured maximum over every zone, so
// the rest is headroom that keeps the loop bounded and absorbs the difference.
const dailyLookbackDays = 3

// weeklyLookbackDays bounds the weekly search, again as steps back from today
// (fifteen dates in all). Seven days finds the previous occurrence of the
// weekday; fourteen is what it takes when THAT occurrence landed on a date its
// zone deleted, so the answer is a week earlier again.
const weeklyLookbackDays = 14

// customYearsBack and customLeapDayYearsBack bound the CALENDAR DERIVATION the
// `custom` cadence uses instead of a scan window. There is no lookback window
// here any more, and the two numbers below are derived rather than chosen:
//
// 🔴 WHY A WINDOW COULD NOT BE MADE TO WORK, since a window is the obvious
// shape and round 1 shipped one (customLookbackDays = 70). A window has to be
// at least as wide as the longest gap between two occurrences, and the MONTH
// set (round 2) makes that gap unbounded in the only direction that matters:
// months {1} × days {1} occurs once a YEAR, and months {2} × days {29} can go
// EIGHT years between occurrences (2096 → 2104, because 2100 is not a leap
// year). A 70-day window therefore answered "no slot" for a slot that
// genuinely happened, which made mostRecentSlot NOT MONOTONIC in `now` — the
// property the header below says the whole feature rests on. That was not a
// theoretical hole: months {1} × days {1} reports its slot at day+70 and
// reports NOTHING at day+71.
//
// WHAT REPLACES IT. The candidate dates are enumerated straight out of the
// month × day sets, newest first, so only dates the schedule actually names
// are ever probed. The per-date resolver (customSlotOn) is untouched, which is
// what keeps every DST decision this feature argued about exactly where it was.
//
// 🔴 THE BOUND IS A PROOF, NOT HEADROOM. Any feasible (month, day) pair that
// is not (2, 29) exists in EVERY year — 31 January, 30 April and 28 February
// are all annual — so the previous occurrence is at most one year back and
// scanning the current year plus one is already exhaustive. customYearsBack is
// 2, and the extra year is the only part that is headroom: it absorbs a year
// whose one declared date the zone happens to delete outright.
//
// (2, 29) is the sole exception, and its gap is fixed by the Gregorian rule:
// four years normally, EIGHT across a century that is not a leap year (1900,
// 2100, 2200). customLeapDayYearsBack is 9 for the same reason, one year of
// headroom over a bound that is otherwise exact.
//
// ⚠️ THESE ARE STILL TRUNCATIONS, AND HERE IS WHAT WOULD MAKE THEM BITE: a
// zone that deletes every declared date of a schedule for customYearsBack
// CONSECUTIVE years. No tzdata release does anything of the kind — deletions
// are single dates around a date-line move — but that, and not "is the number
// big enough", is the condition to re-derive against.
const (
	customYearsBack        = 2
	customLeapDayYearsBack = 9
)

// mostRecentSlot returns the latest slot of s at or before now, computed as
// WALL-CLOCK TIME IN s.Timezone — never in the host's zone, never in UTC.
//
// 🔴 The whole feature depends on this function being MONOTONIC in `now`: a
// later `now` must never yield an earlier slot. Nothing about slot identity
// enforces that, so the arithmetic below has to earn it — see dayAnchor for the
// trap that broke it, and slotAt for what a slot is built out of.
// runScheduledMessageTick nonetheless refuses to move the cursor backwards, so a
// future non-monotonic answer costs a delivery rather than duplicating one.
//
// ⚠️ THE THREE CALENDAR CADENCES EARN IT THROUGH A WINDOW, WHICH IS ONLY SOUND
// WHILE THE WINDOW EXCEEDS THE LONGEST GAP BETWEEN OCCURRENCES. daily, weekly
// and monthly each have AT MOST ONE reading per day/week/month, so their gaps
// are bounded by the calendar and their constants are sized against a stated
// worst case. `custom` is the one whose gap is not bounded that way, and it
// therefore derives its candidates from the calendar instead — see
// customYearsBack.
//
// ok=false means "no slot exists", which happens in exactly these ways, all of
// which the caller must treat as "do not deliver":
//   - s.Timezone will not load (see the 🔴 in ValidateScheduledMessageTimezone:
//     there is NO fallback zone, here or anywhere else in this feature);
//   - for daily/weekly/monthly, no date within the cadence's lookback exists in
//     both the calendar and the zone (a 31st in February; a calendar day the
//     zone deleted outright);
//   - for `custom`, no date the sets name exists in both the calendar and the
//     zone within the derived bound — including the case where NO (month, day)
//     pair is possible at all (months {2} × days {30}), which is refused at
//     write time and answered here without one zone lookup;
//   - a cadence outside the closed set.
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
			// Step back one calendar month. Done on (year, month) directly
			// rather than via AddDate, which would normalise 31 March minus one
			// month into 3 March and start skipping months.
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
	// A cadence outside the closed set produces no slot — that half is
	// deliberate and unchanged. What is NOT acceptable is doing it in silence:
	// such a row simply never fires, with no error and no trace, which looks
	// exactly like a schedule working correctly. One log line is the whole
	// remedy; the trigger behaviour is untouched.
	schedLog("skip %s: unknown cadence %q — this schedule can never fire", s.ID, s.Cadence)
	return time.Time{}, false
}

// mostRecentCustomSlot answers mostRecentSlot's question for the `custom`
// cadence by DERIVING the candidate dates from the calendar, newest first,
// instead of scanning backwards a day at a time hoping to meet one.
//
// 🔴 WHAT DID NOT CHANGE, because it is the part every DST ruling lives in:
// each candidate date is still resolved by customSlotOn, with the same
// arguments and the same notAfter. A wall reading the zone skipped is still
// SKIPPED rather than searched forward; a repeated reading still reconstructs
// to one instant and fires once; a date the zone deleted outright still costs
// that date and nothing else. The only thing this function decides is WHICH
// DATE IS ASKED NEXT — and the answer is no longer "yesterday", it is "the next
// date this schedule actually names".
//
// The enumeration is descending year, then descending month, then descending
// day, which for real calendar dates IS descending date order — so the first
// candidate that yields a slot is the most recent one, the same reason the
// day-at-a-time scan could stop at its first hit.
//
// Membership is still decided DATE BY DATE: a listed day the month does not
// contain simply never becomes a candidate, and the month's OTHER listed days
// are unaffected — [1,15,31] in February fires on the 1st and the 15th and has
// no 31st. The day is never clamped onto the month's last date (the RFC 5545
// rule `monthly` follows; `monthly` names a single day, so for it that rule
// reads as "the whole month is skipped", and that phrasing does not carry over
// to a set). months {2} × days {29} is that same rule read across years: 29
// February is a date three Februaries in four do not have, so the schedule
// fires in leap years only.
//
// 🔴 THE FEASIBILITY PRE-CHECK IS A GUARD, NOT AN OPTIMISATION. A set pair no
// calendar can satisfy (months {2} × days {30}) has ZERO candidate dates, so
// without it the loop would run its full year bound producing nothing — but
// worse, the check is what lets the bound below be stated as a proof: the
// no-pair case is answered separately, so every remaining schedule provably has
// an occurrence within customYearsBack. It shares maxDaysInMonth with the write
// seam (ValidateScheduledMessageCustomSets) rather than carrying a second
// hand-written copy of the calendar: a second copy that disagreed would refuse
// a pair the calendar allows, and a schedule refused here does not error — it
// just never fires.
//
// An empty set is refused at write time; a row that reached the table with one
// (written straight into the database) has no readings at all, so it is
// answered here without a zone lookup and — as before — without a log line.
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
	// ⚠️ THE TWO "still ahead of now" SKIPS BELOW ARE COST, NOT CORRECTNESS, AND
	// SAYING SO IS THE POINT. What actually keeps a future reading out of the
	// answer is customSlotOn's notAfter, which returns nothing on a date whose
	// every reading is later than `now`; delete both skips and the answers are
	// IDENTICAL (measured: whole package green, 0 tests red). They are not
	// decoration either — they are what stops the enumeration from probing every
	// reading of eleven future months first: measured on a 24 × 60 schedule with
	// `now` in mid-January, America/Havana, 96.6µs with them and 30.3ms without,
	// same answer. A reader who takes them for a guard would be wrong, and a
	// reader who deletes them as redundant would be wrong differently.
	for year := local.Year(); year >= local.Year()-yearsBack; year-- {
		for mi := len(months) - 1; mi >= 0; mi-- {
			month := time.Month(months[mi])
			if year == local.Year() && month > local.Month() {
				continue // ahead of `now`: no reading on it can answer
			}
			for di := len(days) - 1; di >= 0; di-- {
				day := days[di]
				if year == local.Year() && month == local.Month() && day > local.Day() {
					continue // same
				}
				// Does the MONTH have this day? Asked exactly as slotAt asks
				// it — construct and compare every component, because
				// time.Date NORMALISES 31 February into 3 March rather than
				// refusing it.
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

// customSlotOn returns the latest reading `custom` has on this calendar date at
// or before notAfter, and false when the date contributes none.
//
// 🔴 THE DST ASYMMETRY AGAINST THE CALENDAR CADENCES, AND WHY IT IS DELIBERATE.
// slotAt moves a wall-clock reading the zone SKIPPED (spring forward) FORWARD to
// the next reading the zone does have. `custom` does the opposite: it SKIPS that
// reading and carries on with the next one in its own enumeration.
//
// The calendar cadences have exactly ONE reading a day, so searching forward is
// what stops the owner from getting nothing at all that day — "half an hour
// late" beats silence. `custom` typically names many readings a day, and
// searching forward there tends to land on a reading that is ALREADY in the
// set: two occurrences collapse onto the same instant, hence the same slotKey,
// and the second delivery merges into the first without a word.
//
// ⚠️ THIS IS A TRADE-OFF APPLIED UNCONDITIONALLY, NOT A TWO-WAY CHOICE, AND THE
// COST IS REAL AT THE DEGENERATE END. `days={15} × hours={2} × minutes={30}` is
// a perfectly legal `custom` schedule with exactly ONE reading a day, and for it
// the merge argument does not apply at all: skipping simply loses the whole day,
// silently, while a `monthly` schedule with the same meaning fires at 03:00.
// Two schedules that say the same thing behave oppositely, and this code chooses
// the silent-loss side for both. Skipping was chosen because its loss is
// predictable, bounded and testable, whereas a merge is invisible — but the
// choice is paid for by the single-reading case.
//
// A THIRD OPTION EXISTS and is not taken here: search forward, and discard the
// result only when the landing reading is itself in the declared sets. That
// keeps "never merge silently" AND stops a single-reading `custom` from
// vanishing. It is a real candidate for a future ticket — tzsweep's no-merge
// invariant (a reported slot must be a declared reading) already has the shape
// to check it. The owner ruled the current behaviour in; this note exists so the
// next person weighs three options rather than the two the first draft named.
//
// The forward-search behaviour of daily/weekly/monthly is untouched.
//
// ⚠️ THE AUTUMN SIDE IS NOT SYMMETRIC WITH THAT, AND IS LEFT AS IT IS. When the
// clocks go back, a wall-clock reading occurs TWICE, and time.Date picks ONE of
// the two instants. WHICH one is not ours to state: Go documents the result as
// implementation-defined for an ambiguous reading, and it is NOT always the
// earlier offset — measured, America/New_York resolves 2024-11-03 01:30 to the
// earlier instant while Europe/London and Africa/Cairo resolve their repeated
// readings to the LATER one. Roughly half the zones go each way, so "always the
// earlier offset" would be a false claim about half the world.
//
// The invariant we actually depend on is weaker and true everywhere: time.Date
// is DETERMINISTIC, so one wall-clock reading reconstructs one instant, produces
// one slotKey, and the cursor refuses the second pass — the reading fires ONCE,
// not twice. That is what the tests pin (and what tzsweep's no-duplicate arm
// checks in every shipped zone); the offset DIRECTION is deliberately not
// pinned, because it is not a promise this code can keep. Nothing is fixed here
// because the resolution lives in the shared readBack/time.Date path every
// cadence uses, and changing it would move all four.
func customSlotOn(day time.Time, s ScheduledMessage, loc *time.Location, notAfter time.Time) (time.Time, bool) {
	year, month, dayNum := day.Year(), day.Month(), day.Day()
	// Does the ZONE have this date at all? Asked with the SAME judgement the
	// calendar cadences use (firstReadingOn), so a date the zone deleted
	// outright — Pacific/Apia, 30 December 2011 — drops the whole day here too.
	if _, exists := firstReadingOn(year, month, dayNum, loc); !exists {
		return time.Time{}, false
	}
	hours, minutes := sortedIntSet(s.CustomHours), sortedIntSet(s.CustomMinutes)
	// Hours descending, minutes descending: the first readable reading at or
	// before notAfter is therefore the LATEST one, so no comparison beyond the
	// first hit is needed.
	for hi := len(hours) - 1; hi >= 0; hi-- {
		for mi := len(minutes) - 1; mi >= 0; mi-- {
			slot, ok := readBack(time.Date(year, month, dayNum, hours[hi], minutes[mi], 0, 0, time.UTC), loc)
			if !ok {
				continue // the zone skipped this reading — dropped, never searched forward
			}
			if !slot.After(notAfter) {
				return slot, true
			}
		}
	}
	return time.Time{}, false
}

// intSetContains reports membership without assuming the slice is sorted.
func intSetContains(vals []int, want int) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}

// dayAnchor names t's calendar DATE — year, month, day, with the zone dropped —
// as a noon-UTC instant, so day arithmetic can be done on it.
//
// 🔴 Day arithmetic is done here and never on a local time. `local.AddDate(0,0,-1)`
// reads like "yesterday" but is not: when yesterday's midnight is an hour the
// zone skipped (America/Santiago 2026-09-06, America/Havana 2026-03-08), the
// result normalises BACKWARDS into the day before, so "yesterday" quietly
// becomes the day before yesterday — the cursor walks backwards and the tick
// redelivers slots it already sent. UTC has no transitions, so the same
// arithmetic on this anchor is pure calendar counting. Noon, not midnight,
// because midnight is the reading zones actually skip.
func dayAnchor(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, time.UTC)
}

// slotOn stamps s's hour:minute onto the calendar date of day, in loc.
func slotOn(day time.Time, s ScheduledMessage, loc *time.Location) (time.Time, bool) {
	return slotAt(day.Year(), day.Month(), day.Day(), s, loc)
}

// monthlySlot builds the slot for s.DayOfMonth within (year, month), reporting
// exists=false when that month has no such day.
func monthlySlot(year int, month time.Month, s ScheduledMessage, loc *time.Location) (time.Time, bool) {
	return slotAt(year, month, s.DayOfMonth, s, loc)
}

// 🔴 What decides "this date is not in this zone" is THE DATE HAVING NO READING
// AT ALL — never how far a forward walk got, and never a guess at how large a
// DST gap can be. Both earlier versions got this from the wrong place:
//
//	v1 bounded the walk at 120 MINUTES and called that headroom on the grounds
//	that gaps are an hour at most. Antarctica/Troll needs exactly 120 every
//	March — the bound fitted with nothing to spare — and Antarctica/Casey's 180
//	did not fit at all, so those occurrences were dropped in silence.
//	v2 bounded the walk at THE END OF THE CALENDAR DAY and read "the walk ran
//	out" as "the zone does not have this date". That inference is false. A
//	spring-forward can land ON midnight and take the whole tail of the day with
//	it: America/Nuuk, Scoresbysund and their aliases jump 23:00 → 00:00 every
//	March, so those dates have no reading from 23:00 onward while plainly
//	existing. Reading that as a missing date sent the cadence loop back a day, to
//	the slot the cursor was already parked on — so the tick skipped without
//	delivering, without erroring and without logging. Same silence, one day a
//	year, in a zone nobody tests.
//
// So the two absences are asked as two SEPARATE questions, in this order:
//
//	Does the zone have this DATE at all?  → firstReadingOn. No reading anywhere
//	  in the 1440 wall readings is treated as "the zone deleted the date outright"
//	  (Pacific/Apia, 30 December 2011, the date-line move). The occurrence is
//	  dropped and the cadence loop steps back a day. THIS is what decides the day
//	  boundary now — nothing else does.
//	  ⚠️ The test is the read-back, not the zone's own history, so it is very
//	  slightly stricter than "deleted": a date whose every wall reading normalises
//	  to some other date reads as absent even though the zone technically has it.
//	  Measured cases exist (Africa/Casablanca and Africa/El_Aaiun, in the far
//	  future of the release measured in the design doc) and cost an hour's delay,
//	  not a lost delivery. Widening it would mean trusting time.Date's
//	  normalisation, which is what the two earlier versions did wrong.
//	Does the zone have this WALL CLOCK?  → the +1-minute walk, which matches
//	  svc-automation's _first_existing_instant. It is NOT bounded by the day: it
//	  may cross midnight onto the next date, because the reading it is looking
//	  for is simply the next one the zone has.
//
// Neither question is answerable from the other's result, which is exactly what
// both earlier versions assumed. The design doc carries the measured counts
// behind each reversal, stamped with the tzdata release and the date they were
// taken — they are a snapshot of one tzdata release, not a property of zones,
// so they are deliberately NOT restated here where nothing would ever re-check
// them.

// firstReadingOn returns the earliest wall reading loc actually HAS on this
// calendar date, and false when it has none — the test for "this zone does not
// have this date".
//
// Cost is one probe whenever 00:00 exists, which is the ordinary case; a date
// whose gap covers midnight walks until the first reading it does have (Havana's
// spring transition takes 61 probes), and only a date that reads as absent walks
// the full 1440. Measured cost is in the design doc, stamped with its tzdata
// release — it is a property of one release, not of zones.
func firstReadingOn(year int, month time.Month, day int, loc *time.Location) (time.Time, bool) {
	start := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	for want := start; want.Day() == day && want.Month() == month && want.Year() == year; want = want.Add(time.Minute) {
		if slot, ok := readBack(want, loc); ok {
			return slot, true
		}
	}
	return time.Time{}, false
}

// readBack constructs want's wall reading in loc and returns it only if loc
// genuinely has that reading.
//
// ⚠️ The read-back is not an optional check: time.Date NORMALISES a reading it
// cannot honour rather than refusing it, in both directions and by an amount of
// its own choosing. 31 February 2026 becomes 3 March; 00:30 on 2026-03-08 in
// America/Havana becomes 03-07 23:30 (backwards, into the previous day); 02:15
// on 2026-10-04 in Australia/Lord_Howe becomes 02:45 (forwards, thirty minutes,
// same date — which a day-only check waves straight through). So every component
// is compared, and only an exact match counts as the reading that was asked for.
func readBack(want time.Time, loc *time.Location) (time.Time, bool) {
	slot := time.Date(want.Year(), want.Month(), want.Day(),
		want.Hour(), want.Minute(), 0, 0, loc)
	ok := slot.Year() == want.Year() && slot.Month() == want.Month() && slot.Day() == want.Day() &&
		slot.Hour() == want.Hour() && slot.Minute() == want.Minute()
	return slot, ok
}

// slotAt builds the slot for (year, month, day) at s's hour:minute in loc.
//
// Two different absences are handled two DIFFERENT ways, and the asymmetry is
// deliberate — see the design doc, which records the reasoning for each:
//
//	THE DATE DOES NOT EXIST — either the month has no such day (31 February) or
//	  the zone deleted the date outright (Pacific/Apia, 30 December 2011) → the
//	  occurrence is DROPPED.
//	  Owner ruling rc-aeef15360ab5, RFC 5545: the day the owner asked for is not
//	  there at all, so there is nothing to move it to that would not be a
//	  different day of the owner's month.
//	THE WALL CLOCK DOES NOT EXIST (the reading a spring-forward skips) → the
//	  occurrence MOVES FORWARD to the first reading the zone does have, EVEN IF
//	  THAT READING IS ON THE NEXT DATE.
//	  The date IS there; it is merely short of some readings, and when the gap
//	  lands on midnight the ones it is short of are the last of the day. Dropping
//	  it means the owner gets nothing that day and nothing says so, which is the
//	  exact failure this feature exists to prevent. Half an hour late is late; it
//	  is not silence.
//
// 🔴 The slot is always CONSTRUCTED from (year, month, day, hour, minute, zone)
// and never derived from an offset of `now`. That is what makes it deterministic
// across the autumn side too: when a wall clock occurs TWICE, both passes
// construct the same instant, so the cursor sees the same slot and the second
// pass delivers nothing. An "elapsed since" style computation would produce two
// instants there and an ordering test alone would not catch it.
func slotAt(year int, month time.Month, day int, s ScheduledMessage, loc *time.Location) (time.Time, bool) {
	// Does the MONTH have this day? A calendar question, asked zone-free — UTC
	// has no transitions, so nothing here can be perturbed by one.
	wall := time.Date(year, month, day, s.Hour, s.Minute, 0, 0, time.UTC)
	if wall.Year() != year || wall.Month() != month || wall.Day() != day {
		return time.Time{}, false
	}
	// Does the ZONE have this date? Asked BEFORE the walk and independently of
	// it, so that "the walk found nothing on this date" can no longer be
	// mistaken for "the zone has no such date".
	if _, exists := firstReadingOn(year, month, day, loc); !exists {
		return time.Time{}, false
	}
	// Does the zone have this WALL CLOCK? If not, the occurrence takes the next
	// reading the zone does have.
	for want := wall; want.Day() == day && want.Month() == month && want.Year() == year; want = want.Add(time.Minute) {
		if slot, ok := readBack(want, loc); ok {
			return slot, true
		}
	}
	// The gap ran off the end of the date (America/Nuuk, 23:00 → 00:00): the
	// next reading is the first one of the following date. Should the zone not
	// have that date either — which no tzdata release has ever done back to back
	// — the occurrence is dropped rather than wandering further.
	next := time.Date(year, month, day+1, 12, 0, 0, 0, time.UTC)
	return firstReadingOn(next.Year(), next.Month(), next.Day(), loc)
}

// slotKey renders a slot as the identifier stored in last_fired_slot. The same
// slot must always render to the same string — that equality IS the
// "already delivered" test.
func slotKey(slot time.Time) string {
	return slot.Format(slotKeyLayout)
}

// slotIsAfterCursor reports whether slot is STRICTLY LATER than the slot the
// cursor names — the fire/skip test.
//
// 🔴 This is deliberately an ordering test and not string inequality. The cursor
// is a rendered instant (`2026-09-06T09:00-03:00`), and inequality answers
// "different from last time", which is not the question: a slot computation that
// ever moves BACKWARDS produces a string that differs from the cursor and
// therefore fires, redelivering something already sent — and a duplicate
// delivery is indistinguishable, in the chat log, from a correct one. Comparing
// instants makes the cursor a ratchet: the worst a future non-monotonic answer
// can do is skip a delivery, which is at least discoverable.
//
// An empty or unparseable cursor means "no slot has been delivered", so the
// schedule fires: that is the state of a row written before the cursor existed,
// and refusing to fire on it would strand the schedule forever.
func slotIsAfterCursor(slot time.Time, cursor string) bool {
	previous, err := time.Parse(slotKeyLayout, cursor)
	if err != nil {
		return true
	}
	return slot.After(previous)
}

// currentSlotKey is the cursor value for "everything up to and including now has
// already been dealt with" — what creation seeds last_fired_slot with so a new
// schedule does not fire the slot it was born after, and what an edit re-aims
// the cursor to so a re-aimed schedule does not fire the slot it crossed. An
// empty string means no slot exists yet, which fires at the next real one.
func currentSlotKey(s ScheduledMessage, now time.Time) string {
	slot, ok := mostRecentSlot(s, now)
	if !ok {
		return ""
	}
	return slotKey(slot)
}

// describeSchedule is the log identity of one schedule — id plus the aimed slot
// in words, so a skipped-delivery line says which schedule and which aim.
//
// `custom` prints its four sets instead of Hour/Minute: those two columns hold
// their 0/0 defaults on a custom row, so printing them would name a time nobody
// chose — a log line that reads like a fact and is not one.
func describeSchedule(s ScheduledMessage) string {
	if s.Cadence == ScheduledMessageCadenceCustom {
		return fmt.Sprintf("%s (custom months=[%s] days=[%s] hours=[%s] minutes=[%s] %s)", s.ID,
			canonicalIntSet(s.CustomMonths), canonicalIntSet(s.CustomDays),
			canonicalIntSet(s.CustomHours), canonicalIntSet(s.CustomMinutes), s.Timezone)
	}
	return fmt.Sprintf("%s (%s %02d:%02d %s)", s.ID, s.Cadence, s.Hour, s.Minute, s.Timezone)
}
