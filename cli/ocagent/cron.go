package main

import (
	"strconv"
	"strings"
	"time"
)

// cronNextRun returns the first minute strictly after `after`, in after's
// location, that a 5-field cron expression (minute hour day-of-month month
// day-of-week) fires at. Fields take numbers, *, */n, a-b, a-b/n and comma
// lists; day-of-week 7 is Sunday. As in cron, when both day fields are
// restricted a day matching either one fires. found is false for an expression
// it cannot parse or one that does not fire within a year.
func cronNextRun(expr string, after time.Time) (next time.Time, found bool) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, false
	}
	minutes, ok1 := cronField(fields[0], 0, 59)
	hours, ok2 := cronField(fields[1], 0, 23)
	days, ok3 := cronField(fields[2], 1, 31)
	months, ok4 := cronField(fields[3], 1, 12)
	weekdays, ok5 := cronField(fields[4], 0, 7)
	if !(ok1 && ok2 && ok3 && ok4 && ok5) {
		return time.Time{}, false
	}
	if weekdays[7] {
		weekdays[0] = true
	}
	daysRestricted := !strings.HasPrefix(fields[2], "*")
	weekdaysRestricted := !strings.HasPrefix(fields[4], "*")
	dayMatches := func(t time.Time) bool {
		dom, dow := days[t.Day()], weekdays[int(t.Weekday())]
		if daysRestricted && weekdaysRestricted {
			return dom || dow
		}
		return dom && dow
	}

	loc := after.Location()
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := after.AddDate(1, 0, 1)
	for t.Before(limit) {
		y, mo, d := t.Date()
		switch {
		case !months[int(mo)]:
			t = time.Date(y, mo+1, 1, 0, 0, 0, 0, loc)
		case !dayMatches(t):
			t = time.Date(y, mo, d+1, 0, 0, 0, 0, loc)
		case !hours[t.Hour()]:
			t = time.Date(y, mo, d, t.Hour()+1, 0, 0, 0, loc)
		case !minutes[t.Minute()]:
			t = t.Add(time.Minute)
		default:
			return t, true
		}
	}
	return time.Time{}, false
}

func cronField(field string, low, high int) ([]bool, bool) {
	set := make([]bool, high+1)
	for _, part := range strings.Split(field, ",") {
		rangePart, stepPart, stepped := strings.Cut(part, "/")
		step := 1
		if stepped {
			n, err := strconv.Atoi(stepPart)
			if err != nil || n <= 0 {
				return nil, false
			}
			step = n
		}
		from, to := low, high
		if rangePart != "*" {
			a, b, isRange := strings.Cut(rangePart, "-")
			first, err := strconv.Atoi(a)
			if err != nil {
				return nil, false
			}
			from, to = first, first
			if isRange {
				last, err := strconv.Atoi(b)
				if err != nil {
					return nil, false
				}
				to = last
			} else if stepped {
				to = high
			}
		}
		if from < low || to > high || from > to {
			return nil, false
		}
		for v := from; v <= to; v += step {
			set[v] = true
		}
	}
	return set, true
}
