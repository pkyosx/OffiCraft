package main

// pacing_test.go — case-for-case port of
// the retired Python tests/domain/test_token_pacing.py. Discipline under test: a value
// that cannot be measured is nil (未量到), NEVER a fabricated 0; elapsed% is a
// now-vs-resets_at back-computation; and used% running more than the margin
// ahead of elapsed% reads as hot.

import "testing"

func TestShapeWindowsMissingRateLimitsIsNilNotZero(t *testing.T) {
	got := ShapeWindows(nil, 1_000_000.0)
	if got["five_hour"] != nil || got["seven_day"] != nil {
		t.Fatalf("missing rate_limits must shape both windows nil, got %+v", got)
	}
	if ShapeWindows("nope", 1_000_000.0)["five_hour"] != nil {
		t.Fatal("non-object rate_limits must shape nil")
	}
	// A window whose raw value is not an object → nil.
	got = ShapeWindows(map[string]any{"five_hour": 5.0}, 1_000_000.0)
	if got["five_hour"] != nil {
		t.Fatalf("non-object window must shape nil, got %+v", got["five_hour"])
	}
}

func TestShapeWindowUsedPctUnmeasuredStaysNil(t *testing.T) {
	now := 1_000_000.0
	win := WindowSeconds["five_hour"]
	// used_percentage absent / sentinel -1 / non-number → used_pct nil, but
	// the window object still returns (partial is allowed).
	w := ShapeWindow(map[string]any{"resets_at": now + 9000}, win, now)
	if w == nil || w.UsedPct != nil {
		t.Fatalf("absent used_percentage must stay nil, got %+v", w)
	}
	if w := ShapeWindow(map[string]any{"used_percentage": -1.0}, win, now); w.UsedPct != nil {
		t.Fatalf("sentinel -1 must stay nil, got %v", *w.UsedPct)
	}
	if w := ShapeWindow(map[string]any{"used_percentage": "x"}, win, now); w.UsedPct != nil {
		t.Fatalf("non-number must stay nil, got %v", *w.UsedPct)
	}
	if w := ShapeWindow(map[string]any{"used_percentage": true}, win, now); w.UsedPct != nil {
		t.Fatalf("a bool is not a measurement, got %v", *w.UsedPct)
	}
}

func TestShapeWindowElapsedPctBackcomputedFromResetsAt(t *testing.T) {
	now := 1_000_000.0
	win := WindowSeconds["five_hour"]
	// Half the window remains → resets_at = now + win/2 → elapsed = 50%.
	w := ShapeWindow(map[string]any{"used_percentage": 10.0, "resets_at": now + win/2}, win, now)
	if w == nil || w.ElapsedPct == nil || *w.ElapsedPct != 50.0 {
		t.Fatalf("elapsed must back-compute to 50%%, got %+v", w)
	}
	// resets_at missing / unparseable → elapsed nil (never 0).
	if w := ShapeWindow(map[string]any{"used_percentage": 10.0}, win, now); w.ElapsedPct != nil {
		t.Fatalf("missing resets_at must leave elapsed nil, got %v", *w.ElapsedPct)
	}
	garbage := map[string]any{"used_percentage": 10.0, "resets_at": "garbage"}
	if w := ShapeWindow(garbage, win, now); w.ElapsedPct != nil {
		t.Fatalf("garbage resets_at must leave elapsed nil, got %v", *w.ElapsedPct)
	}
	// resets_at is echoed AS GIVEN (even when unparseable).
	if w := ShapeWindow(garbage, win, now); w.ResetsAt != "garbage" {
		t.Fatalf("resets_at must echo as given, got %v", w.ResetsAt)
	}
}

func TestShapeWindowPaceHotWhenUsedRunsAhead(t *testing.T) {
	now := 1_000_000.0
	win := WindowSeconds["five_hour"]
	resetsAt := now + win/2 // elapsed = 50%
	// used far ahead of elapsed (> margin) → hot.
	hot := ShapeWindow(map[string]any{"used_percentage": 80.0, "resets_at": resetsAt}, win, now)
	if hot == nil || hot.Pace == nil || *hot.Pace != PaceHot {
		t.Fatalf("80%% used at 50%% elapsed must be hot, got %+v", hot)
	}
	// used at/behind pace → ok.
	ok := ShapeWindow(map[string]any{"used_percentage": 50.0, "resets_at": resetsAt}, win, now)
	if ok == nil || ok.Pace == nil || *ok.Pace != PaceOK {
		t.Fatalf("on-pace usage must be ok, got %+v", ok)
	}
	// Exactly on the margin boundary is NOT hot (strict >).
	edge := ShapeWindow(
		map[string]any{"used_percentage": 50.0 + PaceMarginPct, "resets_at": resetsAt}, win, now)
	if edge == nil || edge.Pace == nil || *edge.Pace != PaceOK {
		t.Fatalf("margin boundary must be ok, got %+v", edge)
	}
}

func TestShapeWindowPaceNilWhenEitherInputMissing(t *testing.T) {
	now := 1_000_000.0
	win := WindowSeconds["five_hour"]
	// used present but elapsed unmeasurable → pace can't be judged → nil.
	w := ShapeWindow(map[string]any{"used_percentage": 90.0}, win, now)
	if w == nil || w.Pace != nil {
		t.Fatalf("unjudgeable pace must stay nil, got %+v", w)
	}
}
