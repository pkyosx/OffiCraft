package main

import (
	"testing"
	"time"
)

func TestCronNextRun(t *testing.T) {
	taipei := time.FixedZone("UTC+8", 8*3600)
	after := time.Date(2026, 10, 22, 14, 7, 30, 0, taipei) // a Thursday
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"every minute is the next whole minute", "* * * * *", "2026-10-22 14:08"},
		{"a fixed minute later this hour", "30 * * * *", "2026-10-22 14:30"},
		{"a fixed minute already past rolls to the next hour", "5 * * * *", "2026-10-22 15:05"},
		{"every five minutes", "*/5 * * * *", "2026-10-22 14:10"},
		{"a stepped range", "10-50/20 * * * *", "2026-10-22 14:10"},
		{"an hour range from its start", "0 9-17 * * *", "2026-10-22 15:00"},
		{"an hour range already over rolls to tomorrow", "0 2-4 * * *", "2026-10-23 02:00"},
		{"a list takes its next member", "0,15,45 * * * *", "2026-10-22 14:15"},
		{"a weekday", "0 9 * * 1", "2026-10-26 09:00"},
		{"Sunday spelled 7", "0 9 * * 7", "2026-10-25 09:00"},
		{"day of month and month", "0 0 1 1 *", "2027-01-01 00:00"},
		{"both day fields restricted fire on either", "0 0 1 * 5", "2026-10-23 00:00"},
		{"the 29th of February within a year", "0 0 29 2 *", ""},
		{"the 30th of February never", "0 0 30 2 *", ""},
		{"too few fields", "* * * *", ""},
		{"a word", "@hourly * * * *", ""},
		{"a list with a minute out of range", "0,60 * * * *", ""},
		{"a range running past the last hour", "0 2-30 * * *", ""},
		{"a backwards range", "0 5-2 * * *", ""},
		{"a zero step", "*/0 * * * *", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, found := cronNextRun(tc.expr, after)

			got := ""
			if found {
				got = next.Format("2006-01-02 15:04")
				if next.Location() != taipei {
					t.Errorf("location = %v, want the input's", next.Location())
				}
			}
			if got != tc.want {
				t.Errorf("cronNextRun(%q) = %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}
