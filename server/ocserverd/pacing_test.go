// Skeleton generated from server/ocserverd/pacing.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestAsFloat(t *testing.T) {
	t.Skip("TODO: asFloat narrows an untrusted JSON value to a float64.")
}

func TestParseResetsAt(t *testing.T) {
	t.Skip("TODO: parseResetsAt turns a raw resets_at into epoch seconds, or nil if absent / unparseable.")
}

func TestUsedPctOrNone(t *testing.T) {
	t.Skip("TODO: usedPctOrNone shapes a raw used_percentage: non-number / the -1 \"not measured\" sentinel (any negative) → nil.")
}

func TestElapsedPct(t *testing.T) {
	t.Skip("TODO: elapsedPct back-computes how much of the window has elapsed from its END time (resets_at): start = resets_at - windowSec; elapsed% clamped to [0,100].")
}

func TestPaceVerdict(t *testing.T) {
	t.Skip("TODO: paceVerdict: \"hot\" when used% runs MORE than PaceMarginPct ahead of elapsed% (strict >), else \"ok\"; either input missing → nil (can't judge).")
}

func TestShapeWindow(t *testing.T) {
	t.Skip("TODO: ShapeWindow shapes one raw rate-limit window.")
}

func TestShapeWindows(t *testing.T) {
	t.Skip("TODO: ShapeWindows shapes the 5h + 7d windows from a raw rate_limits value.")
}
