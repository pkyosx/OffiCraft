// Skeleton generated from server/ocserverd/pacing.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"math"
	"testing"
	"time"
)

func TestAsFloat(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want float64
		ok   bool
	}{
		{name: "float64", in: float64(1.25), want: 1.25, ok: true},
		{name: "float32", in: float32(2.5), want: 2.5, ok: true},
		{name: "int", in: 3, want: 3, ok: true},
		{name: "int64", in: int64(4), want: 4, ok: true},
		{name: "bool", in: true},
		{name: "string", in: "5"},
		{name: "nil", in: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := asFloat(tt.in)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Fatalf("asFloat(%#v) = (%v, %v), want (%v, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestParseResetsAt(t *testing.T) {
	expectedNaive := float64(time.Date(2024, time.January, 2, 3, 4, 5, 0, time.Local).UnixNano()) / 1e9
	tests := []struct {
		name string
		in   any
		want *float64
	}{
		{name: "positive epoch", in: float64(1700000000), want: floatPtr(1700000000)},
		{name: "zero", in: 0, want: nil},
		{name: "negative", in: -1.0, want: nil},
		{name: "rfc3339", in: "2024-01-02T03:04:05Z", want: floatPtr(1704164645)},
		{name: "naive local timestamp", in: "2024-01-02T03:04:05", want: &expectedNaive},
		{name: "blank", in: "  ", want: nil},
		{name: "garbage", in: "not-a-time", want: nil},
		{name: "unsupported type", in: true, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResetsAt(tt.in)
			if !sameFloatPtr(got, tt.want) {
				t.Fatalf("parseResetsAt(%#v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestUsedPctOrNone(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want *float64
	}{
		{name: "rounds a measured percentage", in: 43.456, want: floatPtr(43.46)},
		{name: "accepts an integer", in: 7, want: floatPtr(7)},
		{name: "negative sentinel", in: -1.0, want: nil},
		{name: "any negative value", in: -0.01, want: nil},
		{name: "non-number", in: "43", want: nil},
		{name: "missing", in: nil, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := usedPctOrNone(tt.in); !sameFloatPtr(got, tt.want) {
				t.Fatalf("usedPctOrNone(%#v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestElapsedPct(t *testing.T) {
	tests := []struct {
		name     string
		resetsAt any
		window   float64
		now      float64
		want     *float64
	}{
		{name: "window start", resetsAt: 2000.0, window: 1000, now: 1000, want: floatPtr(0)},
		{name: "half elapsed", resetsAt: 2000.0, window: 1000, now: 1500, want: floatPtr(50)},
		{name: "window end", resetsAt: 2000.0, window: 1000, now: 2000, want: floatPtr(100)},
		{name: "before window clamps to zero", resetsAt: 2000.0, window: 1000, now: 500, want: floatPtr(0)},
		{name: "after window clamps to one hundred", resetsAt: 2000.0, window: 1000, now: 2500, want: floatPtr(100)},
		{name: "invalid reset", resetsAt: "later", window: 1000, now: 1500, want: nil},
		{name: "non-positive window", resetsAt: 2000.0, window: 0, now: 1500, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := elapsedPct(tt.resetsAt, tt.window, tt.now); !sameFloatPtr(got, tt.want) {
				t.Fatalf("elapsedPct(%#v, %v, %v) = %v, want %v", tt.resetsAt, tt.window, tt.now, got, tt.want)
			}
		})
	}
}

func TestPaceVerdict(t *testing.T) {
	used := func(v float64) *float64 { return &v }
	tests := []struct {
		name       string
		used       *float64
		elapsed    *float64
		measuredAt *float64
		now        float64
		freshSecs  float64
		want       *string
	}{
		{name: "ahead by more than margin", used: used(60), elapsed: used(50), measuredAt: used(900), now: 1000, freshSecs: 200, want: stringPtr(PaceHot)},
		{name: "exactly at margin is okay", used: used(55), elapsed: used(50), measuredAt: used(900), now: 1000, freshSecs: 200, want: stringPtr(PaceOK)},
		{name: "behind pace is okay", used: used(40), elapsed: used(50), measuredAt: used(900), now: 1000, freshSecs: 200, want: stringPtr(PaceOK)},
		{name: "missing used percentage", used: nil, elapsed: used(50), measuredAt: used(900), now: 1000, freshSecs: 200, want: nil},
		{name: "missing elapsed percentage", used: used(60), elapsed: nil, measuredAt: used(900), now: 1000, freshSecs: 200, want: nil},
		{name: "unknown age cannot be judged", used: used(60), elapsed: used(50), measuredAt: nil, now: 1000, freshSecs: 200, want: nil},
		{name: "stale snapshot cannot be judged", used: used(60), elapsed: used(50), measuredAt: used(700), now: 1000, freshSecs: 200, want: nil},
		{name: "freshness boundary remains valid", used: used(60), elapsed: used(50), measuredAt: used(800), now: 1000, freshSecs: 200, want: stringPtr(PaceHot)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := paceVerdict(tt.used, tt.elapsed, tt.measuredAt, tt.now, tt.freshSecs)
			if !sameStringPtr(got, tt.want) {
				t.Fatalf("paceVerdict(%v, %v, %v, %v, %v) = %v, want %v", tt.used, tt.elapsed, tt.measuredAt, tt.now, tt.freshSecs, got, tt.want)
			}
		})
	}
}

func TestShapeWindow(t *testing.T) {
	now := 1500.0
	measuredAt := 1400.0
	window := ShapeWindow(map[string]any{
		"used_percentage": 60.123,
		"resets_at":       2000.0,
	}, 1000, now, &measuredAt, 200)
	if window == nil {
		t.Fatal("ShapeWindow returned nil for an object")
	}
	if !sameFloatPtr(window.UsedPct, floatPtr(60.12)) {
		t.Fatalf("UsedPct = %v, want 60.12", window.UsedPct)
	}
	if !sameFloatPtr(window.ElapsedPct, floatPtr(50)) {
		t.Fatalf("ElapsedPct = %v, want 50", window.ElapsedPct)
	}
	if !sameStringPtr(window.Pace, stringPtr(PaceHot)) {
		t.Fatalf("Pace = %v, want hot", window.Pace)
	}
	if window.ResetsAt != 2000.0 {
		t.Fatalf("ResetsAt = %#v, want 2000", window.ResetsAt)
	}
	if !sameFloatPtr(window.MeasuredAt, &measuredAt) {
		t.Fatalf("MeasuredAt = %v, want %v", window.MeasuredAt, &measuredAt)
	}

	if got := ShapeWindow([]any{}, 1000, now, &measuredAt, 200); got != nil {
		t.Fatalf("ShapeWindow(non-object) = %#v, want nil", got)
	}
	partial := ShapeWindow(map[string]any{"used_percentage": -1.0, "resets_at": "unknown"}, 1000, now, nil, 200)
	if partial == nil || partial.UsedPct != nil || partial.ElapsedPct != nil || partial.Pace != nil {
		t.Fatalf("ShapeWindow(partial) = %#v, want an object with nil measurements", partial)
	}
}

func TestShapeWindows(t *testing.T) {
	now := 10_000_000.0
	fiveHour := WindowSeconds["five_hour"]
	sevenDay := WindowSeconds["seven_day"]
	fiveMeasuredAt := now - 1
	sevenMeasuredAt := now - 100
	got := ShapeWindows(map[string]any{
		"five_hour": map[string]any{
			"used_percentage": 70.0,
			"resets_at":       now + fiveHour/2,
		},
		"seven_day": map[string]any{
			"used_percentage": 40.0,
			"resets_at":       now + sevenDay/2,
		},
	}, now, map[string]float64{
		"five_hour": fiveMeasuredAt,
		"seven_day": sevenMeasuredAt,
	}, 50)
	five := got["five_hour"]
	if five == nil || !sameFloatPtr(five.UsedPct, floatPtr(70)) || !sameFloatPtr(five.ElapsedPct, floatPtr(50)) || !sameStringPtr(five.Pace, stringPtr(PaceHot)) {
		t.Fatalf("five_hour = %#v, want measured hot window", five)
	}
	seven := got["seven_day"]
	if seven == nil || !sameFloatPtr(seven.UsedPct, floatPtr(40)) || !sameFloatPtr(seven.ElapsedPct, floatPtr(50)) || seven.Pace != nil {
		t.Fatalf("seven_day = %#v, want measured but stale window with nil pace", seven)
	}

	missing := ShapeWindows(nil, now, nil, 50)
	if len(missing) != 2 || missing["five_hour"] != nil || missing["seven_day"] != nil {
		t.Fatalf("ShapeWindows(missing) = %#v, want both named windows nil", missing)
	}
}

func floatPtr(v float64) *float64 {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

func sameFloatPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return math.Abs(*a-*b) < 1e-9
}

func sameStringPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
