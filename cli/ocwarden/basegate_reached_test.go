package main

// THE CALL SITE, NOT THE FUNCTION (T-88).
//
// basegate_test.go asserts that stationAddressGate refuses correctly. Every one
// of those assertions stays green if somebody deletes the single line in
// realMain that calls it — the gate would be perfect, well-tested, and never
// reached, and the warden would go straight back to talking to whatever answers
// on the guessed address. That failure is silent from every angle: it compiles,
// it starts, the screen looks normal.
//
// This file exists only to make that deletion red, so it asserts the one thing
// no unit test of the gate can: that realMain ACTUALLY CONSULTS IT.
//
// WHY IT DRIVES THE `--once` PATH. realMain's forever path parks by design, and
// a test that parks is not a test. `run --once` reaches the same single call
// site (the branch lives INSIDE the gate, deliberately, so there is only one
// line to delete) and returns.
//
// WHY THERE IS NO TOKEN IN THE ENV. Without one, a realMain that slipped past
// the gate takes run()'s "no OC_TOKEN/OC_ID" path and returns 0 rather than
// making a single HTTP request. So the assertions below distinguish "refused"
// from "carried on" without this test ever being able to reach the network — a
// test of a mis-wire must not itself be able to mis-wire.

import (
	"bytes"
	"strings"
	"testing"
)

func TestRealMainRefusesToRunWithoutAStationAddress(t *testing.T) {
	var out bytes.Buffer
	env := envFn(map[string]string{"HOME": t.TempDir()})

	rc := realMain([]string{"run", "--once"}, env, &out)

	if rc == 0 {
		t.Fatalf("realMain returned 0 with OC_BASE unset — the gate is not reached.\n"+
			"That is the failure this file exists for: stationAddressGate can be fully correct and simply never called.\n"+
			"output was:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "OC_BASE") {
		t.Errorf("realMain must say WHICH setting is missing before it stops; got:\n%s", out.String())
	}
	// The exact wrong outcome, named: carrying on to the reporter's own mis-wire
	// message means the gate was skipped and the guessed address had already been
	// resolved by the time anybody complained.
	if strings.Contains(out.String(), "no OC_TOKEN/OC_ID") {
		t.Error("realMain fell through to run()'s mis-wire path — the station-address gate was bypassed")
	}
}

func TestRealMainRunsOnWhenTheStationAddressIsConfigured(t *testing.T) {
	// POSITIVE CONTROL for the test above. Without it, a gate wired to refuse
	// unconditionally — or a realMain that returned non-zero for some entirely
	// unrelated reason — would satisfy TestRealMainRefusesToRunWithoutAStationAddress
	// while stopping every warden in the fleet.
	var out bytes.Buffer
	env := envFn(map[string]string{
		"HOME":    t.TempDir(),
		"OC_BASE": "https://station.example",
	})

	rc := realMain([]string{"run", "--once"}, env, &out)

	if rc != 0 {
		t.Fatalf("a configured warden must get past the gate; rc=%d output:\n%s", rc, out.String())
	}
	if strings.Contains(out.String(), "OC_BASE is not set") {
		t.Errorf("a configured warden must not be told OC_BASE is unset; got:\n%s", out.String())
	}
	// It stops for the RIGHT reason instead: no credentials. This is what proves
	// control reached run() rather than being turned back at the gate.
	if !strings.Contains(out.String(), "no OC_TOKEN/OC_ID") {
		t.Errorf("expected the reporter's own mis-wire path to be reached (proving the gate let it through); got:\n%s", out.String())
	}
}
