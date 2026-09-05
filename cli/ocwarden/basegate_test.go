package main

// Guards for the station-address gate (T-88). Each test below names the ONE
// edit it is here to turn red, because a guard whose defeating edit nobody
// wrote down tends to be a guard that no longer defeats anything.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
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

// TestRealMainHaltsRatherThanExitsOnTheLaunchdPath (T-88) is the guard for the
// CALL SITE'S ARGUMENT, which is a different thing from the call site existing.
//
// basegate_reached_test.go proves realMain CALLS the gate. It drives `run --once`,
// so it only ever exercises the returning branch — and independent review turned
// that gap into a live defeat: change the ONE token `*once` to `true` at the call
// site and an unconfigured warden EXITS instead of halting. It compiles, and the
// entire package stayed green pass-for-pass, because no test could reach the
// branch that changed.
//
// This drives the real forever path (`run`, no --once) with the block seam
// swapped, so the halt is observable without parking the test.
//
// DEFEATED BY: passing anything but *once at the call site, or replacing the halt
// with a return.
func TestRealMainHaltsRatherThanExitsOnTheLaunchdPath(t *testing.T) {
	blocked := 0
	orig := gateBlock
	gateBlock = func() { blocked++ }
	t.Cleanup(func() { gateBlock = orig })

	var out bytes.Buffer
	env := envFn(map[string]string{"HOME": t.TempDir()})

	rc := realMain([]string{"run"}, env, &out)

	if blocked != 1 {
		t.Fatalf("the launchd path must HALT (block exactly once), not exit; block called %d times.\n"+
			"An exiting warden under the plist's unconditional KeepAlive is a silent relaunch loop —\n"+
			"the outcome this file's header calls strictly worse than the bug being fixed.\n"+
			"output was:\n%s", blocked, out.String())
	}
	if rc == 0 {
		t.Errorf("exit code must be non-zero after the halt is signalled away; got %d", rc)
	}
	if strings.Contains(out.String(), "--once") {
		t.Error("the launchd path must not print the --once branch's wording")
	}
}

// TestGateBlockIsWiredToTheRealBlocker (T-88, B-3) asserts the seam's IDENTITY.
//
// TestRealMainHaltsRatherThanExitsOnTheLaunchdPath swaps gateBlock for a counting
// closure, so it can only ever observe its own closure — it is blind to what
// gateBlock is bound to in production. Independent review defeated it with one
// identifier: `var gateBlock = blockUntilSignal` → `func() {}`. That compiles, it
// makes an unconfigured warden exit instead of halting, and the package stayed
// green pass-for-pass.
//
// This is the same failure the repo already documented for the updater seams —
// a seam wired to the wrong producer is still non-nil, so "it is set" proves
// nothing. Assert WHICH function it is set to.
//
// DEFEATED BY: rebinding gateBlock to anything that is not blockUntilSignal.
func TestGateBlockIsWiredToTheRealBlocker(t *testing.T) {
	got := reflect.ValueOf(gateBlock).Pointer()
	want := reflect.ValueOf(blockUntilSignal).Pointer()
	if got != want {
		t.Fatalf("gateBlock is not bound to blockUntilSignal.\n" +
			"A seam pointed at a no-op is still non-nil, and every other guard in this file\n" +
			"swaps it out — so nothing else can notice. An unconfigured warden would then EXIT\n" +
			"instead of halting, which under the plist's KeepAlive is the silent relaunch loop\n" +
			"this package exists to avoid.")
	}
}

// TestBlockUntilSignalActuallyBlocks (T-88, B-3) asserts the real function's BODY.
//
// Identity is not enough: gateBlock can point at the right function while that
// function returns immediately. Review defeated the first guard that way too —
// `<-ctx.Done()` → `_ = ctx`, one line, package still green, because no test ever
// entered the production blocker.
//
// The notifyContext seam exists solely so this test can supply a context it
// controls instead of a real signal — sending this process a real SIGTERM to test
// a signal handler would be a test that can kill the run it belongs to.
//
// DEFEATED BY: dropping the receive on ctx.Done() (returns early), or making the
// function ignore the cancellation (never returns → the second half times out).
func TestBlockUntilSignalActuallyBlocks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	orig := notifyContext
	notifyContext = func(context.Context, ...os.Signal) (context.Context, context.CancelFunc) {
		return ctx, func() {}
	}
	t.Cleanup(func() { notifyContext = orig; cancel() })

	returned := make(chan struct{})
	go func() { blockUntilSignal(); close(returned) }()

	// HALF ONE: it must still be parked while nothing has been signalled. This is
	// the half that catches a body which does not wait.
	select {
	case <-returned:
		t.Fatal("blockUntilSignal returned without being signalled — the halt does not hold, " +
			"so an unconfigured warden exits and launchd sees the job end")
	case <-time.After(150 * time.Millisecond):
	}

	// HALF TWO is the POSITIVE CONTROL. Without it, a function that blocks forever
	// on the wrong thing — or never returns at all — would satisfy half one, and a
	// warden that cannot be shut down the ordinary way is its own defect.
	cancel()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("blockUntilSignal did not return after cancellation — a halted warden must " +
			"still end on SIGINT/SIGTERM (teardown, reboot)")
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
		"OC_BASE",          // the name of the thing to set
		"ocwarden install", // what to actually do
		"restart",          // ...and that setting it alone is not enough
		// 🔴 THIS ONE REPLACES A PIN ON A FALSE FACT. The first draft required
		// "not appear on the roster", which is backwards — the roster row exists
		// before the install runs, so the machine IS listed and simply stays
		// offline. Pinning that sentence meant CORRECTING it would have turned
		// this test red, i.e. the guard was holding the wrong answer in place.
		// Independent review caught it. Pin what a reader must not conclude
		// instead.
		"never come online",
		"Do not read its presence in the list",
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
