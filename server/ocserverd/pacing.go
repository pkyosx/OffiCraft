package main

// pacing.go — rate-limit window pacing (the Go twin of
// the retired Python domain/token_pacing.py). Turns the Claude-Code statusLine
// rate_limits payload (the 5-hour and 7-day windows, each carrying
// used_percentage + resets_at) into a pacing view: how much of the QUOTA is
// used vs how much of the TIME has elapsed. used% running ahead of elapsed%
// = burning too fast; at or behind = has headroom.
//
// Honest-null discipline throughout: a value that cannot be measured is nil
// (未量到), NEVER a fabricated 0 — the panel must never render a fake 0%.
// Pure and framework-free; the raw payload is untrusted free-form JSON, so
// every accessor tolerates any shape and never panics.

import (
	"math"
	"strings"
	"time"
)

// WindowSeconds fixes the window lengths, aligned to Claude's rate-limit
// windows.
var WindowSeconds = map[string]float64{
	"five_hour": 5 * 3600,
	"seven_day": 7 * 24 * 3600,
}

// PaceMarginPct: used% must exceed elapsed% by MORE than this to count as
// "burning hot" — a small band so normal jitter around the pace line doesn't
// flip the verdict.
const PaceMarginPct = 5.0

// The pace verdict vocabulary (nil pace = can't judge).
const (
	PaceHot = "hot"
	PaceOK  = "ok"
)

// PaceWindow is one shaped rate-limit window. Nil fields are honest nulls
// (unmeasured); ResetsAt echoes the raw value AS GIVEN (epoch number or ISO
// string; nil when absent).
type PaceWindow struct {
	UsedPct    *float64 `json:"used_pct"`
	ElapsedPct *float64 `json:"elapsed_pct"`
	Pace       *string  `json:"pace"`
	ResetsAt   any      `json:"resets_at"`
}

// round2 mirrors Python round(x, 2) (banker's rounding).
func round2(x float64) float64 {
	return math.RoundToEven(x*100) / 100
}

// asFloat narrows an untrusted JSON value to a float64. JSON decoding yields
// float64 only; the integer cases cover literal Go call sites. A bool is not
// a number here (unlike Python, where bool ⊂ int needed an explicit guard).
func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// parseResetsAt turns a raw resets_at into epoch seconds, or nil if absent /
// unparseable. Claude's statusLine actually sends a unix-epoch NUMBER; an
// ISO-8601 string is accepted defensively (a naive timestamp reads in local
// time, matching the Python fromisoformat fallback). Garbage or a
// non-positive number → nil (NEVER 0).
func parseResetsAt(value any) *float64 {
	if n, ok := asFloat(value); ok {
		if n > 0 {
			return &n
		}
		return nil
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return nil
	}
	text = strings.TrimSpace(text)
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		epoch := float64(t.UnixNano()) / 1e9
		return &epoch
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", text, time.Local); err == nil {
		epoch := float64(t.UnixNano()) / 1e9
		return &epoch
	}
	return nil
}

// usedPctOrNone shapes a raw used_percentage: non-number / the -1 "not
// measured" sentinel (any negative) → nil.
func usedPctOrNone(value any) *float64 {
	n, ok := asFloat(value)
	if !ok || n < 0 {
		return nil
	}
	rounded := round2(n)
	return &rounded
}

// elapsedPct back-computes how much of the window has elapsed from its END
// time (resets_at): start = resets_at - windowSec; elapsed% clamped to
// [0,100]. resets_at missing / unparseable → nil (NEVER 0).
func elapsedPct(resetsAt any, windowSec, now float64) *float64 {
	resetEpoch := parseResetsAt(resetsAt)
	if resetEpoch == nil || windowSec <= 0 {
		return nil
	}
	start := *resetEpoch - windowSec
	elapsed := (now - start) / windowSec * 100.0
	rounded := round2(math.Max(0.0, math.Min(100.0, elapsed)))
	return &rounded
}

// paceVerdict: "hot" when used% runs MORE than PaceMarginPct ahead of
// elapsed% (strict >), else "ok"; either input missing → nil (can't judge).
func paceVerdict(usedPct, elapsedPct *float64) *string {
	if usedPct == nil || elapsedPct == nil {
		return nil
	}
	verdict := PaceOK
	if *usedPct > *elapsedPct+PaceMarginPct {
		verdict = PaceHot
	}
	return &verdict
}

// ShapeWindow shapes one raw rate-limit window. A non-object raw → nil;
// individually unmeasurable fields stay nil but the window object is still
// returned (partial is allowed, so the panel can show what it has).
func ShapeWindow(raw any, windowSec, now float64) *PaceWindow {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	resetsAt := obj["resets_at"]
	used := usedPctOrNone(obj["used_percentage"])
	elapsed := elapsedPct(resetsAt, windowSec, now)
	return &PaceWindow{
		UsedPct:    used,
		ElapsedPct: elapsed,
		Pace:       paceVerdict(used, elapsed),
		ResetsAt:   resetsAt,
	}
}

// ShapeWindows shapes the 5h + 7d windows from a raw rate_limits value.
// rate_limits missing / not an object → both windows nil (未量到). Never
// panics.
func ShapeWindows(rateLimits any, now float64) map[string]*PaceWindow {
	out := map[string]*PaceWindow{"five_hour": nil, "seven_day": nil}
	obj, ok := rateLimits.(map[string]any)
	if !ok {
		return out
	}
	for key, windowSec := range WindowSeconds {
		out[key] = ShapeWindow(obj[key], windowSec, now)
	}
	return out
}
