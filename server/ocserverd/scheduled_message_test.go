package main

// scheduled_message_test.go — T-f059 定期訊息 sentinels.
//
// Every test here states the ONE bug it goes red on; a test that cannot name
// its bug is decoration. The load-bearing ones are the last three: the
// cross-month lookback (silent in February and invisible to any fixture built
// from days 1-28), the fire-once invariant (a resend looks EXACTLY like a
// correct delivery), and the outsource recipient (resolving it through the
// member door under staffOnly silently makes every `ow-` schedule
// undeliverable, and the only symptom is a schedule that never fires).

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// scheduledStack assembles the full wired stack and hands back the apiServer
// too, so a test can plant rows directly and drive ONE tick without waiting on
// the cadence goroutine.
func scheduledStack(t *testing.T) (*httptest.Server, []byte, *apiServer) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "scheduled-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	secret := []byte(interopSecret)
	api := newAPIServer(dal, NewHub(), singleKeyring(secret), 3600, "../..")
	h, err := buildHandler(specsFor(api), api.keys, dal.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, secret, api
}

// chatsFrom returns every stored message whose sender is exactly `sender`.
func chatsFrom(t *testing.T, api *apiServer, sender string) []ChatMessage {
	t.Helper()
	all, err := api.dal.ListChat()
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	var out []ChatMessage
	for _, m := range all {
		if m.Sender == sender {
			out = append(out, m)
		}
	}
	return out
}

// mustLoadZone loads an IANA zone or fails the test — a missing zone means the
// embedded tz database is gone, which every assertion below silently depends on.
func mustLoadZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("%s will not load — tz data is missing from this binary: %v", name, err)
	}
	return loc
}

func mustParseSlot(t *testing.T, key string) time.Time {
	t.Helper()
	parsed, err := time.Parse(slotKeyLayout, key)
	if err != nil {
		t.Fatalf("slot key %q does not parse with the canonical layout: %v", key, err)
	}
	return parsed
}

// TestMostRecentSlot pins the arithmetic for all three cadences against known
// inputs, in a NON-UTC zone. Goes red if the wall clock is read in the wrong
// zone, if "today's slot has not arrived yet" fails to fall back a day/week, or
// if the weekday indexing drifts (0 must be Sunday).
func TestMostRecentSlot(t *testing.T) {
	taipei := mustLoadZone(t, "Asia/Taipei")
	// Zones whose DST transition happens AT MIDNIGHT, so the skipped hour is
	// 00:00-00:59 and the whole day's date arithmetic sits on top of a wall clock
	// that does not exist. Nothing in this feature was exercised against one
	// before; both production defects lived here.
	santiago := mustLoadZone(t, "America/Santiago")
	havana := mustLoadZone(t, "America/Havana")
	lordHowe := mustLoadZone(t, "Australia/Lord_Howe")
	apia := mustLoadZone(t, "Pacific/Apia")
	newYork := mustLoadZone(t, "America/New_York")
	// The other side of Apia: a zone that keeps the date but loses the END of it.
	// America/Nuuk springs forward at 23:00, so its March transition day has no
	// reading at all from 23:00 to midnight.
	nuuk := mustLoadZone(t, "America/Nuuk")
	// The two zones with gaps WIDER than an hour: Troll jumps two hours every
	// March (still happening), Casey has jumped three (2016-2022).
	troll := mustLoadZone(t, "Antarctica/Troll")
	casey := mustLoadZone(t, "Antarctica/Casey")
	cases := []struct {
		name string
		sm   ScheduledMessage
		now  time.Time
		want string
	}{{
		// 10:30 local, daily 09:00 → today's slot has already elapsed.
		name: "daily after the slot takes today",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 10, 30, 0, 0, taipei),
		want: "2026-08-10T09:00+08:00",
	}, {
		// 08:30 local, daily 09:00 → today's has not arrived; yesterday's is the
		// most recent one.
		name: "daily before the slot falls back to yesterday",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 8, 30, 0, 0, taipei),
		want: "2026-08-09T09:00+08:00",
	}, {
		// 2026-08-10 is a Monday; day_of_week=1 (Monday) at 09:00 has elapsed.
		name: "weekly on the day after the slot takes today",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceWeekly, DayOfWeek: 1,
			Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 10, 0, 0, 0, taipei),
		want: "2026-08-10T09:00+08:00",
	}, {
		// day_of_week=0 is SUNDAY. From Monday 2026-08-10 the most recent
		// Sunday slot is 2026-08-09.
		name: "weekly zero means sunday",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceWeekly, DayOfWeek: 0,
			Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 10, 0, 0, 0, taipei),
		want: "2026-08-09T09:00+08:00",
	}, {
		// Monday 09:00 has not arrived at 08:00 → the same weekday a week back.
		name: "weekly before the slot falls back a week",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceWeekly, DayOfWeek: 1,
			Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 8, 0, 0, 0, taipei),
		want: "2026-08-03T09:00+08:00",
	}, {
		name: "monthly on the day after the slot takes this month",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 10,
			Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 10, 0, 0, 0, taipei),
		want: "2026-08-10T09:00+08:00",
	}, {
		name: "monthly before the day falls back a month",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 20,
			Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 10, 0, 0, 0, taipei),
		want: "2026-07-20T09:00+08:00",
	}, {
		// 🔴 Santiago skips 2026-09-06 00:00-00:59. Stepping back a day by
		// subtracting from a LOCAL time lands on that skipped wall clock and Go
		// normalises it to 09-05 23:00 — so "yesterday" silently becomes the day
		// BEFORE yesterday, the cursor walks backwards, and the tick redelivers
		// two slots it had already sent.
		name: "daily across a midnight spring-forward takes yesterday, not the day before",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 9, Timezone: "America/Santiago"},
		now:  time.Date(2026, time.September, 7, 0, 30, 0, 0, santiago),
		want: "2026-09-06T09:00-03:00",
	}, {
		// 🔴 The schedule's own wall clock is the one the zone skipped: Havana
		// jumps 00:00 → 01:00 on 2026-03-08, so it has no 00:30 that day. The
		// occurrence MOVES FORWARD to the first reading the zone does have
		// (01:00). It is not dropped — a dropped occurrence is a day the owner
		// silently receives nothing — and it is not 03-07 23:30, which is merely
		// where time.Date's normalisation happened to land.
		name: "daily whose wall clock the zone skipped moves to the first existing one",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 0, Minute: 30, Timezone: "America/Havana"},
		now:  time.Date(2026, time.March, 8, 12, 0, 0, 0, havana),
		want: "2026-03-08T01:00-04:00",
	}, {
		// And the shift lasts exactly one day: the NEXT day's occurrence is back
		// at the wall clock the owner set. A schedule that drifted permanently
		// after a transition would be worse than one that skipped.
		name: "the shift does not persist into the following day",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 0, Minute: 30, Timezone: "America/Havana"},
		now:  time.Date(2026, time.March, 9, 12, 0, 0, 0, havana),
		want: "2026-03-09T00:30-04:00",
	}, {
		// The same rule on the zone the sibling service pinned it with:
		// America/New_York has no 02:30 on 2024-03-10, so that day's 02:30 lands
		// at 03:00 …
		name: "spring forward moves an 0230 daily slot to 0300",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 2, Minute: 30, Timezone: "America/New_York"},
		now:  time.Date(2024, time.March, 10, 12, 0, 0, 0, newYork),
		want: "2024-03-10T03:00-04:00",
	}, {
		// … and the next day is 02:30 again.
		name: "spring forward does not drift the following day",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 2, Minute: 30, Timezone: "America/New_York"},
		now:  time.Date(2024, time.March, 11, 12, 0, 0, 0, newYork),
		want: "2024-03-11T02:30-04:00",
	}, {
		// Pacific/Apia skipped 30 December 2011 ENTIRELY (the date-line move), so
		// the search has to step back two days, not one. This is what bounds the
		// daily lookback below 3.
		name: "daily over a calendar day the zone deleted steps back twice",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 9, Timezone: "Pacific/Apia"},
		now:  time.Date(2011, time.December, 31, 8, 0, 0, 0, apia),
		want: "2011-12-29T09:00-10:00",
	}, {
		// 🔴 The mirror image of Apia, and the one that reads identically from
		// inside slotAt while meaning the opposite. America/Nuuk springs forward
		// at 23:00 every March, so 2027-03-27 HAS most of its readings and then
		// simply stops: 23:00-23:59 are not in the zone. The date is there; only
		// its tail is gone, so a 23:30 schedule's occurrence for the 27th is one
		// that HAPPENS — at the first reading the zone does have, which is on the
		// next date. Stepping back a day instead answers with the 26th, the slot
		// the cursor is already parked on, and the delivery vanishes in silence.
		name: "daily whose wall clock falls in a gap that eats the rest of the day still happens",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 23, Minute: 30, Timezone: "America/Nuuk"},
		now:  time.Date(2027, time.March, 28, 12, 0, 0, 0, nuuk),
		want: "2027-03-28T00:00-01:00",
	}, {
		// And the day after is back at 23:30 — a shift over the day boundary is
		// still one occurrence moved, not a new alignment.
		name: "a gap that eats the rest of the day does not drift the following day",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 23, Minute: 30, Timezone: "America/Nuuk"},
		now:  time.Date(2027, time.March, 29, 23, 59, 0, 0, nuuk),
		want: "2027-03-29T23:30-01:00",
	}, {
		// Same rule on the weekly branch: 2026-03-08 is a Sunday, Havana has no
		// 00:30 on it, and that Sunday's occurrence still happens — at 01:00.
		name: "weekly whose wall clock the zone skipped moves to the first existing one",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceWeekly, DayOfWeek: 0,
			Hour: 0, Minute: 30, Timezone: "America/Havana"},
		now:  time.Date(2026, time.March, 8, 12, 0, 0, 0, havana),
		want: "2026-03-08T01:00-04:00",
	}, {
		// 🔴 And on the monthly branch — the case that made a whole month
		// disappear. Havana has no 00:30 on the 8th of March 2026, and the day
		// check read time.Date's backwards normalisation (03-07 23:30) as "March
		// has no 8th", which is plainly false. March's occurrence happens, at
		// 01:00.
		name: "monthly whose wall clock the zone skipped keeps its month",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 8,
			Hour: 0, Minute: 30, Timezone: "America/Havana"},
		now:  time.Date(2026, time.March, 8, 12, 0, 0, 0, havana),
		want: "2026-03-08T01:00-04:00",
	}, {
		// 🔴 Lord Howe's spring transition is a HALF hour at 02:00, so 02:15 does
		// not exist on 2026-10-04 and time.Date normalises it FORWARD to 02:45 —
		// same year, same month, same day, same hour, thirty minutes late. A day
		// check alone accepts that. The first reading the zone actually has is
		// 02:30, and that is where the occurrence goes: the shift is searched
		// for, never inherited from whatever time.Date happened to return.
		name: "monthly whose wall clock the zone skipped moves to the first existing one, not the normalised one",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 4,
			Hour: 2, Minute: 15, Timezone: "Australia/Lord_Howe"},
		now:  time.Date(2026, time.October, 4, 12, 0, 0, 0, lordHowe),
		want: "2026-10-04T02:30+11:00",
	}, {
		// 🔴 The autumn side, where the ordering test alone would NOT save us:
		// 01:30 happens twice in New York on 2026-11-01, at two different
		// instants, and the later one IS strictly later than the earlier. What
		// keeps it one occurrence is that a slot is CONSTRUCTED from its wall
		// clock, so both passes name the same instant. These two rows are the same
		// schedule observed on either side of the repeat.
		name: "autumn fall back names one slot on the first pass through the repeated hour",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 1, Minute: 30, Timezone: "America/New_York"},
		now:  time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC),
		want: "2026-11-01T01:30-04:00",
	}, {
		name: "autumn fall back names the same slot on the second pass",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 1, Minute: 30, Timezone: "America/New_York"},
		now:  time.Date(2026, time.November, 1, 6, 30, 0, 0, time.UTC),
		want: "2026-11-01T01:30-04:00",
	}, {
		// 🔴 Antarctica/Troll jumps 01:00 → 03:00 every March: a TWO-HOUR gap,
		// once a year, and fourteen of them are still ahead of us. A 01:00
		// schedule there needs the search to walk exactly 120 minutes. The bound
		// that used to sit at 120 fitted this with nothing to spare while its
		// comment called two hours "headroom" — the tz database has had 30, 60,
		// 120 and 180 minute gaps.
		name: "a two hour gap still finds the first existing reading",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 1, Timezone: "Antarctica/Troll"},
		now:  time.Date(2027, time.March, 28, 12, 0, 0, 0, troll),
		want: "2027-03-28T03:00+02:00",
	}, {
		// A reading INSIDE the same gap but nearer its end walks less far, and
		// lands on the same first existing reading — not on wherever time.Date's
		// normalisation put it (which for 02:30 here is 04:30).
		name: "a reading later in the same gap lands on the gap's end, not on the normalised time",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 2, Minute: 30, Timezone: "Antarctica/Troll"},
		now:  time.Date(2027, time.March, 28, 12, 0, 0, 0, troll),
		want: "2027-03-28T03:00+02:00",
	}, {
		// The day after is back at the wall clock the owner set — a two-hour shift
		// is still one occurrence, not a new alignment.
		name: "a two hour gap does not drift the following day",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 1, Timezone: "Antarctica/Troll"},
		now:  time.Date(2027, time.March, 29, 12, 0, 0, 0, troll),
		want: "2027-03-29T01:00+02:00",
	}, {
		// 🔴 And a THREE-hour gap, which the old 120-minute bound could not reach
		// at all: Antarctica/Casey has done this six times, and those occurrences
		// were dropped in silence. Note the answer is 03:01, not a round hour —
		// Casey's jump runs 00:01 → 03:01, so the first reading the zone actually
		// has is the one the search finds, never a tidied-up guess.
		name: "a three hour gap is delivered, not dropped",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Minute: 30, Timezone: "Antarctica/Casey"},
		now:  time.Date(2020, time.October, 4, 12, 0, 0, 0, casey),
		want: "2020-10-04T03:01+11:00",
	}, {
		// The same date in the same zone at an hour the transition does not touch
		// DOES exist — the rule is "this wall clock does not exist", never "this
		// month has no 4th".
		name: "monthly outside the skipped window still takes the current month",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 4,
			Hour: 9, Timezone: "Australia/Lord_Howe"},
		now:  time.Date(2026, time.October, 4, 12, 0, 0, 0, lordHowe),
		want: "2026-10-04T09:00+11:00",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slot, ok := mostRecentSlot(tc.sm, tc.now)
			if !ok {
				t.Fatalf("no slot found; want %s", tc.want)
			}
			if got := slotKey(slot); got != tc.want {
				t.Fatalf("slot: want %s, got %s", tc.want, got)
			}
		})
	}
}

// TestMostRecentSlotIsComputedInTheSchedulesOwnZone is the anti-tautology
// guard for the table above: every assertion there would still pass if the
// implementation ignored the Timezone field and computed in UTC, because the
// dates happen to agree. Here the SAME schedule in two zones must produce two
// DIFFERENT slots — which is only true if the zone is genuinely read.
//
// Red when: LoadLocation's result is dropped, or an unloadable zone falls back
// to UTC/Local instead of refusing.
func TestMostRecentSlotIsComputedInTheSchedulesOwnZone(t *testing.T) {
	// 2026-08-10 01:00 UTC is already 09:00 in Taipei: the Taipei schedule's
	// 09:00 slot is TODAY's, the UTC one's is YESTERDAY's.
	now := time.Date(2026, time.August, 10, 1, 0, 0, 0, time.UTC)
	base := ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 9}

	taipei := base
	taipei.Timezone = "Asia/Taipei"
	tpeSlot, ok := mostRecentSlot(taipei, now)
	if !ok {
		t.Fatal("Asia/Taipei schedule produced no slot")
	}
	utc := base
	utc.Timezone = "UTC"
	utcSlot, ok := mostRecentSlot(utc, now)
	if !ok {
		t.Fatal("UTC schedule produced no slot")
	}
	if slotKey(tpeSlot) == slotKey(utcSlot) {
		t.Fatalf("Asia/Taipei and UTC produced the SAME slot (%s) — the schedule's "+
			"timezone is not being read", slotKey(tpeSlot))
	}
	if got := slotKey(tpeSlot); got != "2026-08-10T09:00+08:00" {
		t.Fatalf("Asia/Taipei slot: want 2026-08-10T09:00+08:00, got %s", got)
	}
	if got := slotKey(utcSlot); got != "2026-08-09T09:00+00:00" {
		t.Fatalf("UTC slot: want 2026-08-09T09:00+00:00, got %s", got)
	}

	// An unloadable zone is refused, NOT silently computed somewhere else.
	broken := base
	broken.Timezone = "Mars/Olympus_Mons"
	if _, ok := mostRecentSlot(broken, now); ok {
		t.Fatal("an unloadable timezone produced a slot — a fallback zone was applied somewhere")
	}
}

// TestMostRecentSlotSkipsMonthsWithoutTheDay is the cross-month lookback pin
// (the design's 🔴). day_of_month=31 in mid-February: February has no 31st, so
// per RFC 5545 that occurrence is dropped from the recurrence set entirely and
// the most recent slot is 31 JANUARY.
//
// Red when: the search looks back only one month (returns "no slot" and the
// schedule silently never fires — nothing alarms), or when the invalid date is
// clamped to the end of February, or when time.Date's rollover to 3 March is
// accepted as a real slot.
func TestMostRecentSlotSkipsMonthsWithoutTheDay(t *testing.T) {
	taipei := mustLoadZone(t, "Asia/Taipei")
	sm := ScheduledMessage{Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 31,
		Hour: 9, Timezone: "Asia/Taipei"}
	now := time.Date(2026, time.February, 15, 12, 0, 0, 0, taipei)

	slot, ok := mostRecentSlot(sm, now)
	if !ok {
		t.Fatal("no slot found for day_of_month=31 in mid-February — the lookback " +
			"stopped at one month, so this schedule would never fire and nothing would say so")
	}
	if got := slotKey(slot); got != "2026-01-31T09:00+08:00" {
		t.Fatalf("want the 31 January slot (2026-01-31T09:00+08:00), got %s — "+
			"February was clamped or rolled over instead of skipped", got)
	}

	// The 29th in a non-leap February is the same shape one month narrower:
	// 2026 is not a leap year, so 29 January is the answer.
	sm.DayOfMonth = 29
	slot, ok = mostRecentSlot(sm, now)
	if !ok {
		t.Fatal("no slot found for day_of_month=29 in mid-February 2026 (not a leap year)")
	}
	if got := slotKey(slot); got != "2026-01-29T09:00+08:00" {
		t.Fatalf("want 2026-01-29T09:00+08:00, got %s", got)
	}

	// And the rollover really is refused at the source, not merely stepped over.
	if _, exists := monthlySlot(2026, time.February, sm, taipei); exists {
		t.Fatal("29 February 2026 was accepted as a real date — time.Date's " +
			"normalisation is not being checked")
	}

	// 🔴 The lookback must span more than ONE step back. day_of_month=31 on
	// 1 March: March's 31st has not arrived, February has no 31st at all, so the
	// answer is 31 JANUARY — two months back. A one-month lookback returns "no
	// slot" and the schedule never fires with nothing to observe, which is
	// exactly the failure monthlyLookbackMonths exists to prevent; before this
	// assertion the constant could be lowered from 12 to 1 with the whole suite
	// still green.
	sm.DayOfMonth = 31
	slot, ok = mostRecentSlot(sm, time.Date(2026, time.March, 1, 0, 30, 0, 0, taipei))
	if !ok {
		t.Fatal("no slot found for day_of_month=31 on 1 March — the lookback stopped " +
			"one month back, at a February that has no 31st")
	}
	if got := slotKey(slot); got != "2026-01-31T09:00+08:00" {
		t.Fatalf("want the 31 January slot (2026-01-31T09:00+08:00), got %s", got)
	}
}

// TestRunScheduledMessageTickFiresEachSlotOnce is the restart-does-not-resend
// invariant. Two ticks over the same elapsed slot must produce exactly ONE
// message; a third, after the schedule is re-aimed at a slot that has not been
// delivered, produces one more.
//
// Red when: the cursor is not written, is written as a timestamp rather than a
// slot identifier, or is compared with anything other than string equality.
// This is the test that has to exist because a duplicate delivery is
// indistinguishable, in the chat log, from a correct one.
func TestRunScheduledMessageTickFiresEachSlotOnce(t *testing.T) {
	_, _, api := scheduledStack(t)
	// Daily 00:00 UTC: whatever "now" is, a slot has always already elapsed.
	sm := ScheduledMessage{
		ID: "sch-once", MemberID: "mira", Label: "daily standup",
		Body: "time for the standup", Cadence: ScheduledMessageCadenceDaily,
		DayOfMonth: 1, Timezone: "UTC", Status: ScheduledMessageStatusEnabled,
		CreatedTS: nowSecs(),
	}
	if err := api.dal.PutScheduledMessage(sm); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	api.runScheduledMessageTick(nowSecs())
	api.runScheduledMessageTick(nowSecs())

	msgs := chatsFrom(t, api, "sched:sch-once")
	if len(msgs) != 1 {
		t.Fatalf("two ticks over the SAME slot delivered %d messages; want exactly 1", len(msgs))
	}
	if msgs[0].Recipient != "mira" || msgs[0].Body != "time for the standup" {
		t.Fatalf("unexpected delivery: %+v", msgs[0])
	}
	meta, _ := msgs[0].Meta["scheduled"].(map[string]any)
	if meta == nil {
		t.Fatalf("delivered message carries no meta.scheduled: %+v", msgs[0].Meta)
	}
	if meta["schedule_id"] != "sch-once" || meta["label"] != "daily standup" {
		t.Fatalf("meta.scheduled does not identify the schedule: %+v", meta)
	}
	fired, _ := meta["slot"].(string)
	stored, err := api.dal.GetScheduledMessage("sch-once")
	if err != nil || stored == nil {
		t.Fatalf("reload schedule: %v %v", stored, err)
	}
	if stored.LastFiredSlot != fired || fired == "" {
		t.Fatalf("cursor %q does not match the delivered slot %q", stored.LastFiredSlot, fired)
	}
	if stored.LastFiredTS == 0 {
		t.Fatal("last_fired_ts stayed 0 after a real delivery")
	}
	// The cursor is a SLOT identifier, not a clock reading.
	if _, err := time.Parse(slotKeyLayout, stored.LastFiredSlot); err != nil {
		t.Fatalf("last_fired_slot %q is not a slot identifier: %v", stored.LastFiredSlot, err)
	}

	// Re-aim at a slot that has NOT been delivered → the next tick fires once.
	stored.LastFiredSlot = "1999-01-01T00:00+00:00"
	if err := api.dal.PutScheduledMessage(*stored); err != nil {
		t.Fatalf("re-aim: %v", err)
	}
	api.runScheduledMessageTick(nowSecs())
	if n := len(chatsFrom(t, api, "sched:sch-once")); n != 2 {
		t.Fatalf("an undelivered slot produced %d total messages; want 2", n)
	}
}

// seedScheduleWithCurrentCursor plants an armed schedule whose cursor is aimed
// at the slot current at `from` — the state a real schedule is in the moment
// after it is created.
func seedScheduleWithCurrentCursor(t *testing.T, api *apiServer, sm ScheduledMessage, from time.Time) {
	t.Helper()
	sm.Status = ScheduledMessageStatusEnabled
	sm.CreatedTS = float64(from.Unix())
	sm.LastFiredSlot = currentSlotKey(sm, from)
	if sm.LastFiredSlot == "" {
		t.Fatalf("seeding %s produced an empty cursor — the schedule would fire on the first tick", sm.ID)
	}
	if err := api.dal.PutScheduledMessage(sm); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
}

// tickEvery drives real ticks across [from, to] and returns the slot identifier
// of every message the schedule delivered, in order.
func tickEvery(t *testing.T, api *apiServer, scheduleID string, from, to time.Time, step time.Duration) []string {
	t.Helper()
	for at := from; !at.After(to); at = at.Add(step) {
		api.runScheduledMessageTick(float64(at.Unix()))
	}
	var slots []string
	for _, m := range chatsFrom(t, api, "sched:"+scheduleID) {
		meta, _ := m.Meta["scheduled"].(map[string]any)
		slot, _ := meta["slot"].(string)
		slots = append(slots, slot)
	}
	return slots
}

// TestRunScheduledMessageTickDeliversOncePerOccurrenceAcrossDSTTransitions is the
// end-to-end DST sentinel — the coverage this feature shipped without, and the
// blind spot both production defects lived in.
//
// A daily schedule crossing a transition must deliver EXACTLY as many messages
// as there are days, no matter which way the clocks moved. Red when the slot
// arithmetic is not monotonic in `now` (spring forward at midnight: the cursor
// walks backwards and two already-delivered slots go out again) or when the same
// wall clock occurring twice is treated as two occurrences (autumn fall back).
func TestRunScheduledMessageTickDeliversOncePerOccurrenceAcrossDSTTransitions(t *testing.T) {
	santiago := mustLoadZone(t, "America/Santiago")
	newYork := mustLoadZone(t, "America/New_York")
	havana := mustLoadZone(t, "America/Havana")
	nuuk := mustLoadZone(t, "America/Nuuk")
	cases := []struct {
		name      string
		id        string
		sm        ScheduledMessage
		from, to  time.Time
		step      time.Duration
		wantSlots []string
	}{{
		// 🔴 America/Santiago springs forward AT MIDNIGHT on 2026-09-06: the
		// whole 00:00-00:59 hour does not exist. Four days, four deliveries.
		name: "midnight spring forward delivers once a day",
		id:   "sch-santiago",
		sm: ScheduledMessage{ID: "sch-santiago", MemberID: "mira", Body: "daily ping",
			Cadence: ScheduledMessageCadenceDaily, Hour: 9, DayOfMonth: 1,
			Timezone: "America/Santiago"},
		from: time.Date(2026, time.September, 4, 9, 0, 0, 0, santiago),
		to:   time.Date(2026, time.September, 8, 23, 59, 0, 0, santiago),
		step: time.Minute,
		wantSlots: []string{
			"2026-09-05T09:00-04:00",
			"2026-09-06T09:00-03:00",
			"2026-09-07T09:00-03:00",
			"2026-09-08T09:00-03:00",
		},
	}, {
		// The autumn side: 2026-11-01 01:30 happens TWICE in New York (once at
		// -04:00, once at -05:00). One day, one delivery.
		name: "repeated wall clock delivers once",
		id:   "sch-newyork",
		sm: ScheduledMessage{ID: "sch-newyork", MemberID: "mira", Body: "daily ping",
			Cadence: ScheduledMessageCadenceDaily, Hour: 1, Minute: 30, DayOfMonth: 1,
			Timezone: "America/New_York"},
		from:      time.Date(2026, time.October, 31, 2, 0, 0, 0, newYork),
		to:        time.Date(2026, time.November, 1, 23, 59, 0, 0, newYork),
		step:      time.Minute,
		wantSlots: []string{"2026-11-01T01:30-04:00"},
	}, {
		// 🔴 A whole MONTH used to vanish here. Havana springs forward at
		// midnight on 2026-03-08, so a monthly schedule on the 8th at 00:30 asks
		// for a wall clock that day does not have — and reading time.Date's
		// backwards normalisation as "March has no 8th" dropped March entirely,
		// with the card still showing a perfectly healthy last-delivered line.
		// March happens, an hour late; April is back at 00:30.
		name: "monthly over a midnight spring forward keeps its month",
		id:   "sch-havana",
		sm: ScheduledMessage{ID: "sch-havana", MemberID: "mira", Body: "monthly ping",
			Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 8, Minute: 30,
			Timezone: "America/Havana"},
		from: time.Date(2026, time.February, 8, 12, 0, 0, 0, havana),
		to:   time.Date(2026, time.April, 8, 23, 59, 0, 0, havana),
		step: 30 * time.Minute,
		wantSlots: []string{
			"2026-03-08T01:00-04:00",
			"2026-04-08T00:30-04:00",
		},
	}, {
		// 🔴 A gap that runs off the END of the day: America/Nuuk jumps 23:00 →
		// 00:00 every March, so 2027-03-27 stops having readings at 23:00. A
		// daily 23:30 must still deliver four times over these five days — the
		// 27th's occurrence lands half an hour late, at midnight, in the offset
		// the zone has by then.
		//
		// The shape this pins is a SILENT one and it does not go through the
		// "no slot" exit: answering the 27th with the 26th's slot leaves the
		// cursor exactly where it already was, so the tick skips without
		// delivering, without erroring, and without logging. On the card it
		// reads as a healthy schedule that simply had nothing to say that day.
		name: "a gap that eats the end of the day still delivers that day",
		id:   "sch-nuuk",
		sm: ScheduledMessage{ID: "sch-nuuk", MemberID: "mira", Body: "daily ping",
			Cadence: ScheduledMessageCadenceDaily, Hour: 23, Minute: 30, DayOfMonth: 1,
			Timezone: "America/Nuuk"},
		from: time.Date(2027, time.March, 25, 23, 30, 0, 0, nuuk),
		to:   time.Date(2027, time.March, 29, 23, 59, 0, 0, nuuk),
		step: time.Minute,
		wantSlots: []string{
			"2027-03-26T23:30-02:00",
			"2027-03-28T00:00-01:00",
			"2027-03-28T23:30-01:00",
			"2027-03-29T23:30-01:00",
		},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, api := scheduledStack(t)
			seedScheduleWithCurrentCursor(t, api, tc.sm, tc.from)
			got := tickEvery(t, api, tc.id, tc.from, tc.to, tc.step)
			if len(got) != len(tc.wantSlots) {
				t.Fatalf("delivered %d message(s) over the transition; want %d\n got: %v\nwant: %v",
					len(got), len(tc.wantSlots), got, tc.wantSlots)
			}
			for i, want := range tc.wantSlots {
				if got[i] != want {
					t.Fatalf("delivery %d was slot %s; want %s\n got: %v", i, got[i], want, got)
				}
			}
		})
	}
}

// TestRunScheduledMessageTickRefusesAHostRelativeTimezone is the tick-side half
// of "no schedule ever runs in the host's zone".
//
// `Local` is not an IANA name: time.LoadLocation resolves it to WHATEVER ZONE
// THE MACHINE IS IN, which is the one thing this feature's timezone rule exists
// to eliminate. The write seam refuses it, so a row can only carry it by
// predating that rule or by being written straight into the database — and the
// tick is the last place anything is looking.
//
// Red when the tick's timezone guard is removed: mostRecentSlot's own
// LoadLocation is happy to compute a slot for `Local`, so the message goes out,
// at an hour that depends on where the server is deployed, and nothing says so.
func TestRunScheduledMessageTickRefusesAHostRelativeTimezone(t *testing.T) {
	for _, zone := range []string{"Local", ""} {
		t.Run("timezone "+zone, func(t *testing.T) {
			_, _, api := scheduledStack(t)
			sm := ScheduledMessage{
				ID: "sch-host", MemberID: "mira", Body: "should never go out",
				Cadence: ScheduledMessageCadenceDaily, Hour: 0, DayOfMonth: 1,
				Timezone: zone, Status: ScheduledMessageStatusEnabled,
				LastFiredSlot: "1999-01-01T00:00+00:00", CreatedTS: nowSecs(),
			}
			if err := api.dal.PutScheduledMessage(sm); err != nil {
				t.Fatalf("seed schedule: %v", err)
			}
			api.runScheduledMessageTick(nowSecs())
			if n := len(chatsFrom(t, api, "sched:sch-host")); n != 0 {
				t.Fatalf("a schedule with timezone %q delivered %d message(s) — it ran in "+
					"the HOST's zone, which is the ambiguity this feature exists to remove", zone, n)
			}
		})
	}
}

// TestCreateScheduledMessageDoesNotFireOnTheFirstTick pins the third acceptance
// condition, on the REAL create path: the cursor is seeded at creation, so a
// schedule created at 10:00 for daily 09:00 does not deliver today's 09:00.
//
// Red when: last_fired_slot is left empty at creation — the very next tick then
// sees a slot that differs from "" and delivers immediately.
func TestCreateScheduledMessageDoesNotFireOnTheFirstTick(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	status, created := doJSON(t, "POST", srv.URL+"/api/members/mira/scheduled-messages",
		ownerTok, `{"label":"morning ping","body":"good morning","cadence":"daily",`+
			`"hour":9,"minute":0,"timezone":"Asia/Taipei"}`)
	if status != 200 {
		t.Fatalf("create: want 200, got %d %v", status, created)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %v", created)
	}
	if cursor, _ := created["last_fired_slot"].(string); cursor == "" {
		t.Fatalf("a fresh schedule has an EMPTY delivery cursor (%v) — the next "+
			"tick will deliver a slot that elapsed before the schedule existed", created)
	}

	api.runScheduledMessageTick(nowSecs())
	if msgs := chatsFrom(t, api, "sched:"+id); len(msgs) != 0 {
		t.Fatalf("the first tick after creation delivered %d message(s); want 0", len(msgs))
	}
}

// TestScheduledMessageDeliversToAnOutsourceWorker is the recipient-rule guard.
// The design requires scheduled messages to use CHAT's recipient rule, not the
// member door: chat's rule says what a recipient IS, while the member door's
// answer depends on the memberScope its caller happened to pass.
//
// Red when: delivery (or the CRUD) resolves the recipient through the member
// door under staffOnly — the worker becomes a 404 on create and an
// undeliverable row in the tick, and the only symptom is a schedule that never
// fires.
func TestScheduledMessageDeliversToAnOutsourceWorker(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	worker := Member{ID: "ow-contractor", Name: "O-77", Kind: KindOutsource,
		RosterStatus: RosterStatusActive}
	if err := api.dal.PutMember(worker); err != nil {
		t.Fatalf("seed worker: %v", err)
	}

	// The CRUD accepts an ow- target (resolveMember would 404 here).
	status, created := doJSON(t, "POST",
		srv.URL+"/api/members/ow-contractor/scheduled-messages", ownerTok,
		`{"label":"worker check","body":"status check","cadence":"daily",`+
			`"hour":0,"minute":0,"timezone":"UTC"}`)
	if status != 200 {
		t.Fatalf("create on an outsource worker: want 200, got %d %v — "+
			"the recipient is being resolved through the member door under staffOnly",
			status, created)
	}
	id, _ := created["id"].(string)

	// And the tick really delivers to it: re-aim the cursor at a slot that has
	// not been delivered, then run one tick.
	stored, err := api.dal.GetScheduledMessage(id)
	if err != nil || stored == nil {
		t.Fatalf("reload schedule: %v %v", stored, err)
	}
	stored.LastFiredSlot = "1999-01-01T00:00+00:00"
	if err := api.dal.PutScheduledMessage(*stored); err != nil {
		t.Fatalf("re-aim: %v", err)
	}
	api.runScheduledMessageTick(nowSecs())

	msgs := chatsFrom(t, api, "sched:"+id)
	if len(msgs) != 1 {
		t.Fatalf("delivery to an outsource worker produced %d message(s); want 1", len(msgs))
	}
	if msgs[0].Recipient != "ow-contractor" {
		t.Fatalf("delivered to %q, want ow-contractor", msgs[0].Recipient)
	}
	if msgs[0].Sender != "sched:"+id {
		t.Fatalf("synthetic sender is %q, want sched:%s", msgs[0].Sender, id)
	}
}

// TestScheduledMessageValidationRefusesUnusableSchedules pins the write-seam
// refusals, one per invariant. The timezone leg is the one that matters most:
// a name the tz database cannot resolve MUST fail here, while somebody is
// looking, because a schedule that silently runs in the wrong zone delivers on
// time-looking messages at the wrong hour and nothing ever alarms.
func TestScheduledMessageValidationRefusesUnusableSchedules(t *testing.T) {
	srv, secret, _ := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	base := `"body":"x","cadence":"daily","hour":9,"minute":0,"timezone":"Asia/Taipei"`
	cases := []struct{ name, body string }{
		{"unknown cadence", `{"body":"x","cadence":"hourly","hour":9,"minute":0,"timezone":"UTC"}`},
		{"blank body", `{"body":"   ","cadence":"daily","hour":9,"minute":0,"timezone":"UTC"}`},
		{"hour out of range", `{"body":"x","cadence":"daily","hour":24,"minute":0,"timezone":"UTC"}`},
		{"minute out of range", `{"body":"x","cadence":"daily","hour":9,"minute":60,"timezone":"UTC"}`},
		{"day_of_week out of range", `{` + base + `,"day_of_week":7}`},
		{"day_of_month zero", `{` + base + `,"day_of_month":0}`},
		{"day_of_month past 31", `{` + base + `,"day_of_month":32}`},
		{"unknown timezone", `{"body":"x","cadence":"daily","hour":9,"minute":0,"timezone":"Mars/Olympus_Mons"}`},
		// 🔴 The dangerous timezone is not the one that fails to load — it is the
		// one that loads and means "wherever this server happens to be running".
		// time.LoadLocation("Local") returns the HOST's zone and ("") returns UTC,
		// so both would be accepted by a load-succeeds check and would bind the
		// schedule to a deployment detail nobody stated.
		{"host-relative timezone Local", `{"body":"x","cadence":"daily","hour":9,"minute":0,"timezone":"Local"}`},
		{"blank timezone", `{"body":"x","cadence":"daily","hour":9,"minute":0,"timezone":"   "}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, resp := doJSON(t, "POST",
				srv.URL+"/api/members/mira/scheduled-messages", ownerTok, tc.body)
			if status != 422 {
				t.Fatalf("want 422, got %d %v", status, resp)
			}
		})
	}

	// day_of_month 31 is ACCEPTED (owner ruling rc-aeef15360ab5: RFC 5545, the
	// range is not capped at 28) — the sentinel against a well-meaning tighten.
	status, resp := doJSON(t, "POST", srv.URL+"/api/members/mira/scheduled-messages",
		ownerTok, `{"body":"x","cadence":"monthly","day_of_month":31,"hour":9,"minute":0,"timezone":"UTC"}`)
	if status != 200 {
		t.Fatalf("day_of_month=31 must be accepted (RFC 5545 ruling), got %d %v", status, resp)
	}
}

// TestUpdateScheduledMessageReAimsTheCursorOnlyWhenReAimed pins the PATCH
// contract the frozen spec states: editing a cadence/slot field moves the
// cursor to the slot current now (so the edit never fires the slot it crossed),
// while editing label/body/status leaves the cursor exactly where it was.
//
// Red when: the cursor is reset on EVERY patch (a disable/enable round-trip
// would then swallow the next delivery), or never (re-aiming a schedule
// backwards fires retroactively on the next tick).
func TestUpdateScheduledMessageReAimsTheCursorOnlyWhenReAimed(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	status, created := doJSON(t, "POST", srv.URL+"/api/members/mira/scheduled-messages",
		ownerTok, `{"body":"ping","cadence":"daily","hour":9,"minute":0,"timezone":"Asia/Taipei"}`)
	if status != 200 {
		t.Fatalf("create: %d %v", status, created)
	}
	id, _ := created["id"].(string)
	path := srv.URL + "/api/members/mira/scheduled-messages/" + id

	// Plant a distinctive stale cursor so a move is measurable either way.
	stored, _ := api.dal.GetScheduledMessage(id)
	stored.LastFiredSlot = "1999-01-01T00:00+08:00"
	if err := api.dal.PutScheduledMessage(*stored); err != nil {
		t.Fatalf("plant cursor: %v", err)
	}

	status, patched := doJSON(t, "PATCH", path, ownerTok, `{"status":"disabled","label":"renamed"}`)
	if status != 200 {
		t.Fatalf("status/label patch: %d %v", status, patched)
	}
	if got, _ := patched["last_fired_slot"].(string); got != "1999-01-01T00:00+08:00" {
		t.Fatalf("a status/label edit moved the cursor to %q — disabling and "+
			"re-enabling would silently swallow a delivery", got)
	}

	status, patched = doJSON(t, "PATCH", path, ownerTok, `{"hour":8}`)
	if status != 200 {
		t.Fatalf("hour patch: %d %v", status, patched)
	}
	got, _ := patched["last_fired_slot"].(string)
	if got == "1999-01-01T00:00+08:00" || got == "" {
		t.Fatalf("re-aiming the slot left the cursor at %q — the next tick would "+
			"deliver the slot the edit just crossed", got)
	}
	if slot := mustParseSlot(t, got); slot.After(time.Now()) {
		t.Fatalf("the re-aimed cursor %q is in the FUTURE — it must be the slot "+
			"most recently elapsed", got)
	}

	// 🔴 Re-aiming is about the VALUE changing, not about the field appearing.
	// Plant the stale cursor again and send the hour the schedule already has:
	// nothing about when it fires has changed, so nothing about the cursor may
	// change either. A caller that PATCHes the whole form back — every editor
	// eventually does — otherwise swallows one delivery per save, in the window
	// between a slot elapsing and the next tick, leaving no trace.
	stored, _ = api.dal.GetScheduledMessage(id)
	stored.LastFiredSlot = "1999-01-01T00:00+08:00"
	if err := api.dal.PutScheduledMessage(*stored); err != nil {
		t.Fatalf("replant cursor: %v", err)
	}
	status, patched = doJSON(t, "PATCH", path, ownerTok, `{"hour":8,"minute":0,"cadence":"daily","timezone":"Asia/Taipei"}`)
	if status != 200 {
		t.Fatalf("no-op patch: %d %v", status, patched)
	}
	if got, _ := patched["last_fired_slot"].(string); got != "1999-01-01T00:00+08:00" {
		t.Fatalf("a patch that changed NO slot field moved the cursor to %q — the "+
			"slot it just crossed will never be delivered and nothing will say so", got)
	}

	// An unloadable timezone is refused on PATCH too, not just on create — and so
	// is the host-relative one, which is the one that would otherwise succeed.
	for _, tz := range []string{"Mars/Olympus_Mons", "Local"} {
		if status, resp := doJSON(t, "PATCH", path, ownerTok, `{"timezone":"`+tz+`"}`); status != 422 {
			t.Fatalf("patching to timezone %q: want 422, got %d %v", tz, status, resp)
		}
	}
}

// TestUpdateScheduledMessageSettingsLeavesAConcurrentCursorAdvanceAlone pins the
// edit/tick race, which is the LAST way this feature could deliver twice.
//
// A PATCH is a read-modify-write: the handler reads the whole row, applies the
// patch to that snapshot, and persists it. The tick runs every 60 seconds and
// advances the same row's cursor. Persisting the snapshot WHOLE — including the
// cursor as it stood before the request — rolls a delivery that already happened
// back to "not delivered", and the very next tick sends it again. The monotonic
// fire test is no defence: the cursor itself moved backwards, so the slot really
// is strictly later than what the row now claims.
//
// The sequence below is that interleaving, made deterministic: the snapshot is
// taken BEFORE the tick and persisted AFTER it, which is exactly what a PATCH
// landing in the gap does.
//
// Red when the edit persists the cursor columns at all — the second tick then
// delivers the same slot a second time, and in the chat log a duplicate is
// indistinguishable from a correct delivery, so nothing anywhere says so.
func TestUpdateScheduledMessageSettingsLeavesAConcurrentCursorAdvanceAlone(t *testing.T) {
	_, _, api := scheduledStack(t)
	// Daily 00:00 UTC: a slot has always already elapsed, and the planted cursor
	// is old enough that the first tick must fire.
	sm := ScheduledMessage{
		ID: "sch-race", MemberID: "mira", Label: "before", Body: "the one delivery",
		Cadence: ScheduledMessageCadenceDaily, DayOfMonth: 1, Timezone: "UTC",
		Status: ScheduledMessageStatusEnabled, LastFiredSlot: "1999-01-01T00:00+00:00",
		CreatedTS: nowSecs(),
	}
	if err := api.dal.PutScheduledMessage(sm); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	// The row as the PATCH handler holds it once its read has returned.
	snapshot, err := api.dal.GetScheduledMessage("sch-race")
	if err != nil || snapshot == nil {
		t.Fatalf("read the row the way the handler does: %v %v", snapshot, err)
	}

	// …and the tick lands in the gap: one delivery, cursor advanced.
	api.runScheduledMessageTick(nowSecs())
	if n := len(chatsFrom(t, api, "sched:sch-race")); n != 1 {
		t.Fatalf("the tick delivered %d message(s); want 1 — the race cannot be set up", n)
	}
	fired, err := api.dal.GetScheduledMessage("sch-race")
	if err != nil || fired == nil {
		t.Fatalf("reload after the tick: %v %v", fired, err)
	}

	// Now the PATCH completes, carrying the pre-tick snapshot. Only the label
	// changed, so nothing about when this schedule fires was edited.
	snapshot.Label = "renamed"
	if err := api.dal.UpdateScheduledMessageSettings(*snapshot); err != nil {
		t.Fatalf("persist the edit: %v", err)
	}

	api.runScheduledMessageTick(nowSecs())

	if n := len(chatsFrom(t, api, "sched:sch-race")); n != 1 {
		t.Fatalf("slot %s went out %d times — the edit persisted the cursor it read "+
			"BEFORE the delivery, rolling the advance back, and the next tick resent it",
			fired.LastFiredSlot, n)
	}
	after, err := api.dal.GetScheduledMessage("sch-race")
	if err != nil || after == nil {
		t.Fatalf("reload after the edit: %v %v", after, err)
	}
	if after.LastFiredSlot != fired.LastFiredSlot {
		t.Fatalf("the edit moved the cursor from %q to %q", fired.LastFiredSlot, after.LastFiredSlot)
	}
	if after.LastFiredTS != fired.LastFiredTS {
		t.Fatalf("the edit rewrote last_fired_ts from %v to %v — it records when a "+
			"delivery happened, and this edit was not one", fired.LastFiredTS, after.LastFiredTS)
	}
	// And the edit itself really landed — the test must not pass by writing nothing.
	if after.Label != "renamed" {
		t.Fatalf("the edit did not apply: label is %q, want \"renamed\"", after.Label)
	}
}

// armScheduledRow plants an armed schedule whose cursor is old enough that the
// very next tick must deliver.
func armScheduledRow(t *testing.T, api *apiServer, id string) {
	t.Helper()
	// Daily 00:00 UTC: a slot has always already elapsed.
	if err := api.dal.PutScheduledMessage(ScheduledMessage{
		ID: id, MemberID: "mira", Label: "before", Body: "the one delivery",
		Cadence: ScheduledMessageCadenceDaily, DayOfMonth: 1, Timezone: "UTC",
		Status: ScheduledMessageStatusEnabled, LastFiredSlot: "1999-01-01T00:00+00:00",
		CreatedTS: nowSecs(),
	}); err != nil {
		t.Fatalf("arm %s: %v", id, err)
	}
}

// request performs one HTTP call and reports the status, distinguishing a
// TRANSPORT failure (status 0) from any answer the server actually sent. The
// difference is the whole point: a handler that panics closes the connection, so
// the caller sees EOF rather than a status.
func request(method, url, token, body string) (int, error) {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// TestUpdateScheduledMessageDoesNotRollBackACursorAdvancedByAConcurrentTick is
// the B2 guard, at the seam the invariant actually lives at: the HTTP handler.
//
// The edit is a read-modify-write over a snapshot, and the tick advances the
// same row's cursor every 60 seconds. If the edit persists that snapshot WHOLE,
// a tick landing between the handler's read and its write has its advance
// overwritten — the delivery is laundered back into "not delivered" and the next
// tick sends it again. The monotonic fire test cannot help: the cursor itself
// went backwards.
//
// 🔴 This drives the REAL route (`PATCH .../scheduled-messages/{id}`) against a
// REAL tick, concurrently, many rounds. Pinning the DAL function instead is not
// enough and was measured not to be: reverting only the handler's call to
// PutScheduledMessage — leaving the DAL correct — left the whole suite green
// while reproducing duplicates in one round in five.
//
// Red when the handler persists cursor columns from the row it read.
func TestUpdateScheduledMessageDoesNotRollBackACursorAdvancedByAConcurrentTick(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	const rounds = 300
	duplicated, patchFailures := 0, 0
	for i := 0; i < rounds; i++ {
		id := fmt.Sprintf("sch-race-%03d", i)
		armScheduledRow(t, api, id)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			// A label-only edit: nothing about WHEN this schedule fires changed,
			// so it has no business touching the cursor.
			if status, err := request("PATCH",
				srv.URL+"/api/members/mira/scheduled-messages/"+id, ownerTok,
				`{"label":"renamed"}`); err != nil || status != 200 {
				patchFailures++
			}
		}()
		go func() {
			defer wg.Done()
			api.runScheduledMessageTick(nowSecs())
		}()
		wg.Wait()

		// Whatever the interleaving was, a second tick must find nothing to do.
		api.runScheduledMessageTick(nowSecs())
		if len(chatsFrom(t, api, "sched:"+id)) > 1 {
			duplicated++
		}
		if err := api.dal.DeleteScheduledMessage(id); err != nil {
			t.Fatalf("clear %s: %v", id, err)
		}
	}
	if patchFailures != 0 {
		t.Fatalf("%d/%d PATCHes did not return 200 — the race could not be set up",
			patchFailures, rounds)
	}
	if duplicated != 0 {
		t.Fatalf("%d/%d rounds delivered the SAME slot twice — a PATCH landing between "+
			"the tick's delivery and its cursor write rolled the cursor back, and in the "+
			"chat log the resend is indistinguishable from a correct delivery",
			duplicated, rounds)
	}
}

// TestUpdateScheduledMessageAnswersWhenTheRowIsDeletedMidRequest pins that a row
// vanishing between the handler's read and its re-read is an ANSWER, not a
// dropped connection.
//
// The UPDATE matches zero rows without erroring, so a concurrent DELETE leaves
// the re-read returning (nil, nil). Folding "gone" together with "storage broke"
// handed internalError a nil error, and its first act is err.Error() — the
// handler panicked, net/http killed that connection, and the caller got EOF with
// no status at all.
//
// Red when the two are folded back together: rounds report a transport error
// rather than a status.
func TestUpdateScheduledMessageAnswersWhenTheRowIsDeletedMidRequest(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	const rounds = 300
	var dropped, unexpected int
	for i := 0; i < rounds; i++ {
		id := fmt.Sprintf("sch-gone-%03d", i)
		armScheduledRow(t, api, id)
		path := srv.URL + "/api/members/mira/scheduled-messages/" + id

		var wg sync.WaitGroup
		var patchStatus int
		var patchErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			patchStatus, patchErr = request("PATCH", path, ownerTok, `{"label":"renamed"}`)
		}()
		go func() {
			defer wg.Done()
			request("DELETE", path, ownerTok, "")
		}()
		wg.Wait()

		switch {
		case patchErr != nil:
			dropped++
		case patchStatus != 200 && patchStatus != 404:
			unexpected++
		}
		api.dal.DeleteScheduledMessage(id)
	}
	if dropped != 0 {
		t.Fatalf("%d/%d PATCHes got no response at all (transport error) — the handler "+
			"panicked on a row that was deleted mid-request instead of answering", dropped, rounds)
	}
	if unexpected != 0 {
		t.Fatalf("%d/%d PATCHes answered with something other than 200 or 404 — a row "+
			"deleted mid-request is a 404, the same answer a schedule that never existed gets",
			unexpected, rounds)
	}
}
