package main

import (
	"testing"
	"time"
)

func TestMostRecentSlot(t *testing.T) {
	cases := []struct {
		name string
		s    ScheduledMessage
		now  time.Time
		want string
	}{
		{
			name: "daily wall clock",
			s:    ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 8, Minute: 0, Timezone: "Asia/Taipei"},
			now:  time.Date(2026, 9, 7, 0, 30, 0, 0, time.UTC),
			want: "2026-09-07T08:00+08:00",
		},
		{
			name: "weekly previous weekday",
			s:    ScheduledMessage{Cadence: ScheduledMessageCadenceWeekly, DayOfWeek: int(time.Monday), Hour: 9, Minute: 0, Timezone: "Asia/Taipei"},
			now:  time.Date(2026, 9, 9, 4, 0, 0, 0, time.UTC),
			want: "2026-09-07T09:00+08:00",
		},
		{
			name: "monthly skips a month without the day",
			s:    ScheduledMessage{Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 31, Hour: 9, Minute: 0, Timezone: "UTC"},
			now:  time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
			want: "2026-01-31T09:00+00:00",
		},
		{
			name: "custom latest selected reading",
			s: ScheduledMessage{
				Cadence: ScheduledMessageCadenceCustom, CustomMonths: []int{3, 1}, CustomDays: []int{15, 1},
				CustomHours: []int{17, 9}, CustomMinutes: []int{30, 0}, Timezone: "Asia/Taipei",
			},
			now:  time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
			want: "2026-03-15T17:30+08:00",
		},
		{
			name: "invalid timezone has no slot",
			s:    ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 9, Minute: 0, Timezone: "Mars/Olympus"},
			now:  time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC),
		},
		{
			name: "unknown cadence has no slot",
			s:    ScheduledMessage{Cadence: "fortnightly", Timezone: "UTC"},
			now:  time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := mostRecentSlot(tc.s, tc.now)
			assertSlotResult(t, got, ok, tc.want)
		})
	}
}

func TestMostRecentCustomSlot(t *testing.T) {
	locNewYork := mustLoadLocation(t, "America/New_York")
	cases := []struct {
		name  string
		s     ScheduledMessage
		loc   *time.Location
		local time.Time
		want  string
	}{
		{
			name: "latest same-day reading from unsorted sets",
			s: ScheduledMessage{
				CustomMonths: []int{3, 1}, CustomDays: []int{15, 1},
				CustomHours: []int{17, 9}, CustomMinutes: []int{30, 0},
			},
			loc:   locNewYork,
			local: time.Date(2026, 3, 15, 13, 0, 0, 0, locNewYork),
			want:  "2026-03-15T09:30-04:00",
		},
		{
			name:  "leap day reaches the preceding leap year",
			s:     ScheduledMessage{CustomMonths: []int{2}, CustomDays: []int{29}, CustomHours: []int{9}, CustomMinutes: []int{0}},
			loc:   time.UTC,
			local: time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC),
			want:  "2024-02-29T09:00+00:00",
		},
		{
			name:  "impossible month and day has no slot",
			s:     ScheduledMessage{CustomMonths: []int{2}, CustomDays: []int{30}, CustomHours: []int{9}, CustomMinutes: []int{0}},
			loc:   time.UTC,
			local: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name:  "empty reading set has no slot",
			s:     ScheduledMessage{CustomMonths: []int{1}, CustomDays: []int{1}, CustomHours: nil, CustomMinutes: []int{0}},
			loc:   time.UTC,
			local: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := mostRecentCustomSlot(tc.s, tc.loc, tc.local)
			assertSlotResult(t, got, ok, tc.want)
		})
	}
}

func TestCustomSlotOn(t *testing.T) {
	locNewYork := mustLoadLocation(t, "America/New_York")
	locApia := mustLoadLocation(t, "Pacific/Apia")
	locTaipei := mustLoadLocation(t, "Asia/Taipei")
	cases := []struct {
		name     string
		day      time.Time
		s        ScheduledMessage
		loc      *time.Location
		notAfter time.Time
		want     string
	}{
		{
			name:     "latest reading at or before the cutoff",
			day:      time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
			s:        ScheduledMessage{CustomHours: []int{17, 9}, CustomMinutes: []int{30, 0}},
			loc:      locTaipei,
			notAfter: time.Date(2026, 9, 7, 13, 0, 0, 0, locTaipei),
			want:     "2026-09-07T09:30+08:00",
		},
		{
			name:     "cutoff before every reading has no slot",
			day:      time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
			s:        ScheduledMessage{CustomHours: []int{9}, CustomMinutes: []int{0}},
			loc:      locTaipei,
			notAfter: time.Date(2026, 9, 7, 8, 59, 0, 0, locTaipei),
		},
		{
			name:     "a skipped daylight reading is not moved forward",
			day:      time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC),
			s:        ScheduledMessage{CustomHours: []int{2}, CustomMinutes: []int{30}},
			loc:      locNewYork,
			notAfter: time.Date(2026, 3, 8, 4, 0, 0, 0, locNewYork),
		},
		{
			name:     "a zone-deleted date contributes no reading",
			day:      time.Date(2011, 12, 30, 0, 0, 0, 0, time.UTC),
			s:        ScheduledMessage{CustomHours: []int{9}, CustomMinutes: []int{0}},
			loc:      locApia,
			notAfter: time.Date(2011, 12, 31, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := customSlotOn(tc.day, tc.s, tc.loc, tc.notAfter)
			assertSlotResult(t, got, ok, tc.want)
		})
	}
}

func TestIntSetContains(t *testing.T) {
	for _, tc := range []struct {
		name string
		vals []int
		want int
		ok   bool
	}{
		{name: "unsorted hit", vals: []int{9, 1, 4, 1}, want: 1, ok: true},
		{name: "miss", vals: []int{9, 1, 4}, want: 3},
		{name: "empty set", vals: nil, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := intSetContains(tc.vals, tc.want); got != tc.ok {
				t.Fatalf("intSetContains(%v, %d) = %v, want %v", tc.vals, tc.want, got, tc.ok)
			}
		})
	}
}

func TestFirstReadingOn(t *testing.T) {
	locHavana := mustLoadLocation(t, "America/Havana")
	locApia := mustLoadLocation(t, "Pacific/Apia")
	cases := []struct {
		name  string
		year  int
		month time.Month
		day   int
		loc   *time.Location
		want  string
	}{
		{name: "ordinary midnight", year: 2026, month: time.September, day: 7, loc: time.UTC, want: "2026-09-07T00:00+00:00"},
		{name: "midnight daylight gap", year: 2026, month: time.March, day: 8, loc: locHavana, want: "2026-03-08T01:00-04:00"},
		{name: "deleted date", year: 2011, month: time.December, day: 30, loc: locApia},
		{name: "invalid calendar date", year: 2026, month: time.February, day: 31, loc: time.UTC},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := firstReadingOn(tc.year, tc.month, tc.day, tc.loc)
			assertSlotResult(t, got, ok, tc.want)
		})
	}
}

func TestReadBack(t *testing.T) {
	locNewYork := mustLoadLocation(t, "America/New_York")
	locTaipei := mustLoadLocation(t, "Asia/Taipei")
	t.Run("an existing wall reading is returned in the requested zone", func(t *testing.T) {
		got, ok := readBack(time.Date(2026, 9, 7, 9, 30, 0, 0, time.UTC), locTaipei)
		assertSlotResult(t, got, ok, "2026-09-07T09:30+08:00")
	})
	t.Run("a skipped wall reading is rejected", func(t *testing.T) {
		got, ok := readBack(time.Date(2026, 3, 8, 2, 30, 0, 0, time.UTC), locNewYork)
		assertSlotResult(t, got, ok, "")
	})
	t.Run("a repeated wall reading resolves deterministically", func(t *testing.T) {
		want := time.Date(2026, 11, 1, 1, 30, 0, 0, time.UTC)
		first, ok := readBack(want, locNewYork)
		if !ok {
			t.Fatal("repeated wall reading was rejected")
		}
		second, ok := readBack(want, locNewYork)
		if !ok {
			t.Fatal("repeated wall reading was rejected on the second read")
		}
		if !first.Equal(second) {
			t.Fatalf("repeated wall reading changed from %s to %s", first.Format(time.RFC3339), second.Format(time.RFC3339))
		}
		if first.Year() != 2026 || first.Month() != time.November || first.Day() != 1 || first.Hour() != 1 || first.Minute() != 30 {
			t.Fatalf("repeated wall reading = %s, want the requested wall components", first.Format(time.RFC3339))
		}
	})
}

func TestSlotAt(t *testing.T) {
	locNewYork := mustLoadLocation(t, "America/New_York")
	locApia := mustLoadLocation(t, "Pacific/Apia")
	locTaipei := mustLoadLocation(t, "Asia/Taipei")
	cases := []struct {
		name  string
		year  int
		month time.Month
		day   int
		s     ScheduledMessage
		loc   *time.Location
		want  string
	}{
		{name: "ordinary wall reading", year: 2026, month: time.September, day: 7, s: ScheduledMessage{Hour: 9, Minute: 30}, loc: locTaipei, want: "2026-09-07T09:30+08:00"},
		{name: "missing calendar day", year: 2026, month: time.February, day: 31, s: ScheduledMessage{Hour: 9, Minute: 30}, loc: time.UTC},
		{name: "skipped daylight reading moves forward", year: 2026, month: time.March, day: 8, s: ScheduledMessage{Hour: 2, Minute: 30}, loc: locNewYork, want: "2026-03-08T03:00-04:00"},
		{name: "zone-deleted date is dropped", year: 2011, month: time.December, day: 30, s: ScheduledMessage{Hour: 9, Minute: 0}, loc: locApia},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := slotAt(tc.year, tc.month, tc.day, tc.s, tc.loc)
			assertSlotResult(t, got, ok, tc.want)
		})
	}
}

func TestSlotIsAfterCursor(t *testing.T) {
	slot := time.Date(2026, 9, 7, 10, 0, 0, 0, time.FixedZone("Taipei", 8*60*60))
	for _, tc := range []struct {
		name   string
		cursor string
		want   bool
	}{
		{name: "no previous cursor", cursor: "", want: true},
		{name: "malformed cursor", cursor: "not-a-slot", want: true},
		{name: "same instant in the same rendering", cursor: "2026-09-07T10:00+08:00", want: false},
		{name: "same instant with another offset", cursor: "2026-09-07T02:00+00:00", want: false},
		{name: "earlier cursor", cursor: "2026-09-07T09:59+08:00", want: true},
		{name: "later cursor", cursor: "2026-09-07T10:01+08:00", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := slotIsAfterCursor(slot, tc.cursor); got != tc.want {
				t.Fatalf("slotIsAfterCursor(%s, %q) = %v, want %v", slot.Format(slotKeyLayout), tc.cursor, got, tc.want)
			}
		})
	}
}

func TestCurrentSlotKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    ScheduledMessage
		now  time.Time
		want string
	}{
		{name: "daily slot key", s: ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 8, Minute: 0, Timezone: "Asia/Taipei"}, now: time.Date(2026, 9, 7, 0, 30, 0, 0, time.UTC), want: "2026-09-07T08:00+08:00"},
		{name: "monthly slot key skips February", s: ScheduledMessage{Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 31, Hour: 9, Minute: 0, Timezone: "UTC"}, now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), want: "2026-01-31T09:00+00:00"},
		{name: "invalid timezone has empty key", s: ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 9, Minute: 0, Timezone: "Mars/Olympus"}, now: time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := currentSlotKey(tc.s, tc.now)
			if got != tc.want {
				t.Fatalf("currentSlotKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDescribeSchedule(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    ScheduledMessage
		want string
	}{
		{name: "ordinary cadence includes its wall time", s: ScheduledMessage{ID: "sch-1", Cadence: ScheduledMessageCadenceDaily, Hour: 9, Minute: 5, Timezone: "Asia/Taipei"}, want: "sch-1 (daily 09:05 Asia/Taipei)"},
		{name: "custom cadence includes canonical sets", s: ScheduledMessage{ID: "sch-2", Cadence: ScheduledMessageCadenceCustom, Hour: 23, Minute: 59, CustomMonths: []int{12, 1, 12}, CustomDays: []int{15, 1}, CustomHours: []int{17, 9, 17}, CustomMinutes: []int{30, 0}, Timezone: "America/New_York"}, want: "sch-2 (custom months=[1,12] days=[1,15] hours=[9,17] minutes=[0,30] America/New_York)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeSchedule(tc.s); got != tc.want {
				t.Fatalf("describeSchedule() = %q, want %q", got, tc.want)
			}
		})
	}
}

func assertSlotResult(t *testing.T, got time.Time, ok bool, want string) {
	t.Helper()
	if want == "" {
		if ok {
			t.Fatalf("got slot %s, want no slot", got.Format(time.RFC3339))
		}
		return
	}
	if !ok {
		t.Fatalf("got no slot, want %s", want)
	}
	if rendered := got.Format("2006-01-02T15:04-07:00"); rendered != want {
		t.Fatalf("got slot %s, want %s", rendered, want)
	}
}

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %q: %v", name, err)
	}
	return loc
}
