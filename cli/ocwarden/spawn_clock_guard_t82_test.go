package main

import (
	"os"
	"reflect"
	"testing"
	"time"
)

// The two guards T-82's review round 3 found missing. Both exist because the
// thing they pin was, at the time, observable ONLY by watching a spawn take ~30
// real seconds — so every mutation of it stayed green and cost nothing but a
// slower suite.

// TestNudgeClock_NilMeansRealPacingNotNoWait pins the fail-safe itself (BLK-1).
// Reverting nudgeClock's nil arm to a no-op — the exact edit that was green
// before this test existed — fails here in microseconds.
func TestNudgeClock_NilMeansRealPacingNotNoWait(t *testing.T) {
	got := nudgeClock(nil)
	if got == nil {
		t.Fatal("nudgeClock(nil) returned nil: a nil clock must become a REAL clock, " +
			"never an absent one — an absent one turns 30 paced Enters into 30 in microseconds")
	}
	want := reflect.ValueOf(time.Sleep).Pointer()
	if reflect.ValueOf(got).Pointer() != want {
		t.Errorf("nudgeClock(nil) is not time.Sleep.\n"+
			"  A no-op (or any other clock) here means forgetting to wire the clock silently\n"+
			"  stops the boot nudge from pacing, which is the correctness bug this fallback\n"+
			"  exists to demote to a performance one.\n"+
			"  got pointer %v, want time.Sleep %v", reflect.ValueOf(got).Pointer(), want)
	}
}

// TestNudgeClock_NonNilIsHandedBackUnchanged is the other half: the fail-safe
// must not override a clock a caller DID supply, or every test that passes a
// no-op for speed would start pacing for real.
func TestNudgeClock_NonNilIsHandedBackUnchanged(t *testing.T) {
	calls := 0
	mine := func(time.Duration) { calls++ }
	got := nudgeClock(mine)
	got(time.Millisecond)
	if calls != 1 {
		t.Errorf("nudgeClock replaced a caller-supplied clock: called mine %d times, want 1", calls)
	}
}

// TestPerSpawnBinding_CarriesTheBaseClockThrough pins the seam that survived two
// review rounds (BLK-2). The transport binds two seams per spawn — Pretrust and
// PurgeTrash — and MUST carry everything else through from the base untouched.
//
// The mutant this catches, verbatim from the review: assigning a non-nil no-op
// clock at that seam. It is a real correctness regression (30 Enters in
// microseconds) that the nil fail-safe cannot see, because a no-op clock is
// indistinguishable from a real one by type.
//
// It also fails if someone adds a THIRD per-spawn knob without deciding to: any
// field other than the two named below that stops matching the base is reported.
func TestPerSpawnBinding_CarriesTheBaseClockThrough(t *testing.T) {
	ticks := 0
	// EVERY field is populated, and the FUNC ones are populated for a reason that
	// is easy to undo by accident: the structural half below compares func fields
	// by POINTER, and nil == nil. A fixture that leaves a seam nil therefore cannot
	// see `d.X = nil` — the whole "quietly clear a seam" family is invisible to a
	// guard whose base already has nothing there.
	//
	// That is not hypothetical. Review round 4 measured it: with only Sleep set,
	// `d.CaptureEnv = nil` (every agent silently loses the owner's shell env) and
	// `d.ResolveOcAgentBin = nil` (T-81's defect, whole) both ran 522/522 GREEN.
	//
	// ⇒ ADDING A FUNC FIELD TO SpawnDeps MEANS ADDING IT HERE. There is no
	// mechanical check that says you forgot; the symptom is silence.
	base := SpawnDeps{
		Runner:    fakeRunner{},
		Base:      "https://station.example",
		Socket:    "sock-fixture",
		Home:      "/fixture/home",
		Namespace: "ns-fixture",
		EnvFile:   "/fixture/env",
		ClaudeBin: "/fixture/claude",
		CodexBin:  "/fixture/codex",
		WardenBin: "/fixture/ocwarden",
		RepoRoot:  "/fixture/repo",
		Nudge:     "fixture-nudge",
		Sleep:     func(time.Duration) { ticks++ },

		CaptureEnv:        func() (string, error) { return "", nil },
		Logf:              func(string, ...any) {},
		ClaudeCreds:       func() claudeCredStatus { return claudeCredStatus{} },
		ResolveOcAgentBin: func() (string, bool) { return "", false },
		WriteFile:         func(string, string, os.FileMode) error { return nil },
		MkdirAll:          func(string, os.FileMode) error { return nil },
		Symlink:           func(string, string) error { return nil },
		Remove:            func(string) error { return nil },
		Pretrust:          func() error { return nil },
		PurgeTrash:        func() {},
	}

	got := base.withPerSpawn(func() error { return nil }, func() {})

	// (a) BEHAVIOURAL: the clock that comes out is the one that went in. A pointer
	// check alone would pass a copy that merely looks alike; calling it proves the
	// base's own counter moves.
	if got.Sleep == nil {
		t.Fatal("withPerSpawn dropped the nudge clock entirely")
	}
	got.Sleep(time.Millisecond)
	if ticks != 1 {
		t.Fatalf("withPerSpawn did not carry the BASE clock through: base clock ran %d times, want 1.\n"+
			"  This is the shape that stayed green through two review rounds: the per-spawn\n"+
			"  seam replacing Sleep with a non-nil no-op turns 30 paced Enters into 30 in\n"+
			"  microseconds, and nothing else in the package notices.", ticks)
	}

	// (b) STRUCTURAL: exactly two fields may differ. Anything else is a per-spawn
	// knob somebody added without a decision — the failure names it rather than
	// reporting a bare inequality, because the next reader needs to know WHICH.
	perSpawn := map[string]bool{"Pretrust": true, "PurgeTrash": true}
	rt := reflect.TypeOf(base)
	rb, rg := reflect.ValueOf(base), reflect.ValueOf(got)
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if perSpawn[name] {
			continue
		}
		bf, gf := rb.Field(i), rg.Field(i)
		if bf.Kind() == reflect.Func {
			if bf.Pointer() != gf.Pointer() {
				t.Errorf("withPerSpawn changed %q, which is NOT one of the two per-spawn seams.\n"+
					"  If that was deliberate, add it to this test's perSpawn set AND to\n"+
					"  withPerSpawn's parameter list, so widening what varies per spawn stays a\n"+
					"  visible edit instead of one more line inside a closure.", name)
			}
			continue
		}
		if !reflect.DeepEqual(bf.Interface(), gf.Interface()) {
			t.Errorf("withPerSpawn changed %q (base %v, got %v), which is NOT a per-spawn seam",
				name, bf.Interface(), gf.Interface())
		}
	}

	// (c) and the two that MAY differ actually were rebound — otherwise this whole
	// test would pass against a withPerSpawn that does nothing at all.
	if got.Pretrust == nil || got.PurgeTrash == nil {
		t.Error("withPerSpawn did not bind Pretrust/PurgeTrash: the test above would then be " +
			"asserting that a no-op function changes nothing, which is vacuous")
	}
}
