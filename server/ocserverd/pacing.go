package main

// pacing.go — shapes the Claude Code statusLine rate_limits payload
// (five_hour / seven_day, each used_percentage + resets_at) into used% vs
// elapsed%. Honest-null: an unmeasurable value is nil, NEVER 0 — the panel
// must never render a fake 0%. The payload is untrusted free-form JSON.

import (
	"math"
	"strings"
	"time"
)

var WindowSeconds = map[string]float64{
	"five_hour": 5 * 3600,
	"seven_day": 7 * 24 * 3600,
}

const PaceMarginPct = 5.0

const (
	PaceHot = "hot"
	PaceOK  = "ok"
)

// MeasuredAt is the AGE of UsedPct: UsedPct is a frozen snapshot while
// ElapsedPct is recomputed from now, so without it a live 43% and a three-day
// old one render identically. nil = nobody stamped it, not "just now".
type PaceWindow struct {
	UsedPct    *float64 `json:"used_pct"`
	ElapsedPct *float64 `json:"elapsed_pct"`
	Pace       *string  `json:"pace"`
	ResetsAt   any      `json:"resets_at"`
	MeasuredAt *float64 `json:"measured_at"`
}

func round2(x float64) float64 {
	return math.RoundToEven(x*100) / 100
}

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

// Claude's statusLine sends a unix-epoch NUMBER; an ISO string is accepted
// defensively (a naive one reads as local time).
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

// Any negative is the statusLine's -1 "not measured" sentinel.
func usedPctOrNone(value any) *float64 {
	n, ok := asFloat(value)
	if !ok || n < 0 {
		return nil
	}
	rounded := round2(n)
	return &rounded
}

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

// A snapshot older than freshSecs (or of unknown age) gets a nil verdict: a
// frozen used% against an advancing elapsed% flips "hot" by time alone. The
// number itself is still served.
func paceVerdict(usedPct, elapsedPct, measuredAt *float64, now, freshSecs float64) *string {
	if usedPct == nil || elapsedPct == nil {
		return nil
	}
	if measuredAt == nil || now-*measuredAt > freshSecs {
		return nil
	}
	verdict := PaceOK
	if *usedPct > *elapsedPct+PaceMarginPct {
		verdict = PaceHot
	}
	return &verdict
}

func ShapeWindow(raw any, windowSec, now float64, measuredAt *float64, freshSecs float64) *PaceWindow {
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
		Pace:       paceVerdict(used, elapsed, measuredAt, now, freshSecs),
		ResetsAt:   resetsAt,
		MeasuredAt: measuredAt,
	}
}

// measuredAt is PER WINDOW: the fold picks each window independently (later
// resets_at wins), so one account-wide stamp would let a fresh 5h window
// vouch for a frozen 7d one.
func ShapeWindows(rateLimits any, now float64, measuredAt map[string]float64, freshSecs float64) map[string]*PaceWindow {
	out := map[string]*PaceWindow{"five_hour": nil, "seven_day": nil}
	obj, ok := rateLimits.(map[string]any)
	if !ok {
		return out
	}
	for key, windowSec := range WindowSeconds {
		var stamp *float64
		if ts, ok := measuredAt[key]; ok && ts > 0 {
			stamp = &ts
		}
		out[key] = ShapeWindow(obj[key], windowSec, now, stamp, freshSecs)
	}
	return out
}
