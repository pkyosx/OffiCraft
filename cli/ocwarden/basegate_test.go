package main

// Guards for the station-address gate (T-88). Each test below names the ONE
// edit it is here to turn red, because a guard whose defeating edit nobody
// wrote down tends to be a guard that no longer defeats anything.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Unix(1700000000, 0).UTC() }

// TestBaseFromEnvReportsWhetherItWasSet pins the distinction the old loadConfig
// destroyed: not what the address is, but whether anybody chose one.
//
// DEFEATED BY: making baseFromEnv fall back to defaultBase (i.e. putting the
// guess back where it was).
func TestBaseFromEnvReportsWhetherItWasSet(t *testing.T) {
	if _, ok := baseFromEnv(envFn(map[string]string{})); ok {
		t.Error("an absent OC_BASE must report configured=false")
	}
	if _, ok := baseFromEnv(envFn(map[string]string{"OC_BASE": "   "})); ok {
		t.Error("a whitespace-only OC_BASE must report configured=false")
	}
	// A station host legitimately points at loopback. Judging by comparing the
	// value against defaultBase would refuse exactly that machine, so the check
	// must be on whether the env was set — not on what it says.
	got, ok := baseFromEnv(envFn(map[string]string{"OC_BASE": defaultBase}))
	if !ok {
		t.Fatal("an explicitly-set loopback OC_BASE is CONFIGURED, not a guess — refusing it would break every station host")
	}
	if got != defaultBase {
		t.Errorf("base = %q, want %q", got, defaultBase)
	}
}

// TestLoadConfigDoesNotGuessBase pins the first of the two independent
// assignment paths: the run-loop's.
//
// DEFEATED BY: re-adding `if base == "" { base = defaultBase }` to loadConfig.
func TestLoadConfigDoesNotGuessBase(t *testing.T) {
	cfg := loadConfig(envFn(map[string]string{"OC_TOKEN": "t", "OC_ID": "m-1"}))
	if cfg.Base != "" {
		t.Errorf("loadConfig invented a station address %q with OC_BASE unset — that is the whole defect T-88 removes", cfg.Base)
	}
}

// TestResolvePaths_RefusesWhenBaseUnset pins the SECOND, independent assignment
// path: the installer's, whose guess gets baked into the launchd plist and is
// therefore inherited by every future launch on that machine.
//
// DEFEATED BY: re-adding `if ocBase == "" { ocBase = defaultBase }` to
// resolvePaths. Changing only main.go leaves this one intact, which is exactly
// why it is asserted separately.
func TestResolvePaths_RefusesWhenBaseUnset(t *testing.T) {
	_, err := resolvePaths(envFn(map[string]string{
		"HOME":     "/Users/seth",
		"OC_TOKEN": "tok-abc",
	}), "/repo/bin/ocwarden", 501)
	if err == nil {
		t.Fatal("install must refuse when OC_BASE is unset; a guess here is written into the plist permanently")
	}
	if !strings.Contains(err.Error(), "OC_BASE") {
		t.Errorf("the refusal must name the variable the operator has to set; got %q", err)
	}
}

// TestStationAddressGateLetsAConfiguredWardenThrough is the POSITIVE control.
// Without it, a gate that refused unconditionally would pass every other test in
// this file while stopping the entire fleet.
func TestStationAddressGateLetsAConfiguredWardenThrough(t *testing.T) {
	var out bytes.Buffer
	blocked := false
	rc, stop := stationAddressGate(
		envFn(map[string]string{"OC_BASE": "https://station.example"}),
		&out, false, fixedNow, func() { blocked = true },
	)
	if stop {
		t.Fatalf("a configured warden must pass the gate; got stop=true rc=%d", rc)
	}
	if blocked {
		t.Error("a configured warden must not halt")
	}
	if out.Len() != 0 {
		t.Errorf("a configured warden must say nothing about OC_BASE; got %q", out.String())
	}
}

// TestStationAddressGateHaltsRatherThanExits pins the shape chosen in step ④:
// stay alive and do nothing, rather than exit.
//
// DEFEATED BY: replacing the halt with a plain `return 1` — which, under the
// plist's unconditional KeepAlive, is a relaunch every ThrottleInterval with the
// message going to a log file nobody reads.
func TestStationAddressGateHaltsRatherThanExits(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	blocked := 0
	rc, stop := stationAddressGate(
		envFn(map[string]string{"HOME": home}),
		&out, false, fixedNow, func() { blocked++ },
	)
	if !stop {
		t.Fatal("an unconfigured warden must not be allowed through the gate")
	}
	if blocked != 1 {
		t.Errorf("the gate must PARK (block exactly once), not exit; block called %d times", blocked)
	}
	if rc == 0 {
		t.Error("exit code must be non-zero: nothing this warden was installed to do ever happened")
	}
}

// TestNoBaseMessageIsReadableByAPerson. The failures on this path have
// historically all been silent, so the message is part of the fix, not
// decoration around it. In particular it must say that setting the variable is
// not by itself enough — a halted warden does not heal, and "I fixed it, it
// should come back" is the expensive wrong belief here.
//
// DEFEATED BY: trimming the message down to something like "OC_BASE unset".
func TestNoBaseMessageIsReadableByAPerson(t *testing.T) {
	msg := noBaseMessage(noBaseSentinelName)
	for _, want := range []string{
		"OC_BASE",                  // the name of the thing to set
		"ocwarden install",         // what to actually do
		"restart",                  // ...and that setting it alone is not enough
		"not appear on the roster", // where the absence will show up
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message a person finds must contain %q; got:\n%s", want, msg)
		}
	}
}

// TestHaltWritesSentinelSayingWhy. The sentinel is the on-box record: the log
// line scrolls, the file stays.
func TestHaltWritesSentinelSayingWhy(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	haltNoBase(envFn(map[string]string{"HOME": home}), &out, fixedNow, func() {})

	path := filepath.Join(home, ".officraft", "warden", noBaseSentinelName)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no sentinel at %s: %v", path, err)
	}
	if !strings.Contains(string(body), "OC_BASE") {
		t.Errorf("the sentinel must say why the warden stopped; got %q", body)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("the log must name where the sentinel went, so a reader who cannot find it learns that from the same place; got %q", out.String())
	}
}

// TestHaltStillHaltsWhenTheSentinelCannotBeWritten. The sentinel is a RECORD,
// never an input: an unwritable scratch path must not be able to turn the
// refusal back into a start. (Same fail-open direction contextreport.go states
// for its backoff record — stated here as a test rather than a comment.)
//
// DEFEATED BY: making haltNoBase return early, or fall through to a normal
// start, when writeNoBaseSentinel fails.
func TestHaltStillHaltsWhenTheSentinelCannotBeWritten(t *testing.T) {
	var out bytes.Buffer
	blocked := 0
	// No HOME ⇒ noBaseSentinelPath cannot derive a path at all.
	rc := haltNoBase(envFn(map[string]string{}), &out, fixedNow, func() { blocked++ })
	if blocked != 1 {
		t.Errorf("an unwritable sentinel must not un-halt the warden; block called %d times", blocked)
	}
	if rc == 0 {
		t.Error("exit code must stay non-zero")
	}
	if !strings.Contains(out.String(), "no sentinel written") {
		t.Errorf("the log must say the record could not be written rather than implying one exists; got %q", out.String())
	}
}
