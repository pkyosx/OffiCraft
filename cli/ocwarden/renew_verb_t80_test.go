package main

// renew_verb_t80_test.go — the `renew` warden-command, and the production line
// that connects it to the thing it is supposed to drive (T-80).
//
// THE VERB IS THE CHEAP HALF. What is worth asserting is that the seam it calls
// is wired to RenewNow and not to Kick. Both are `func()`, both are non-nil, both
// wake the same loop — and one of them renews nothing, because a warden
// credential has no expiry and a bare wake finds nothing due. `!= nil` cannot
// tell them apart, so these tests CALL what production wired and look at what it
// did, the way renewwiring_reached_test.go settles the same class of question.

import (
	"strings"
	"testing"
)

// 🔴 THE FRAME HAS TO GET PAST THE READER BEFORE ANY OF THE DISPATCH TESTS BELOW
// MEAN ANYTHING. They call dispatchCommand directly, so every one of them stays
// green with `renew` missing from the accepted-verb set — and a verb the reader
// refuses as unknown-rpc never reaches dispatch at all, which is a fleet that
// silently ignores every demand while looking exactly like one that has not been
// asked yet. This is the only test that enters through the real parse.
func TestParseCommandFrame_RenewIsAnAcceptedVerb(t *testing.T) {
	cmd, err := parseCommandFrame([]byte(`{"topic":"warden-command","data":{"rpc":"renew","args":{}}}`))
	if err != nil {
		t.Fatalf("a renew frame must parse; got %v", err)
	}
	if cmd == nil {
		t.Fatal("a renew frame must parse into a command, not a skip")
	}
	if cmd.RPC != rpcRenew {
		t.Fatalf("rpc = %q; want %q", cmd.RPC, rpcRenew)
	}
}

func TestDispatchCommand_RenewCallsTheCredentialRenewalSeam(t *testing.T) {
	calls := 0
	deps := CommandDeps{Renew: func() { calls++ }}

	if err := dispatchCommand(&Command{RPC: rpcRenew, Args: map[string]any{}}, deps); err != nil {
		t.Fatalf("renew: %v", err)
	}
	if calls != 1 {
		t.Fatalf("the renewal seam was called %d times; want 1", calls)
	}
}

// An unwired seam must REFUSE rather than answer politely. A silent no-op here
// reads on the station exactly like a machine that renewed and has not been seen
// since, which is the one state that must not be forgeable.
func TestDispatchCommand_RenewIsRefusedWhenTheSeamIsNotWired(t *testing.T) {
	err := dispatchCommand(&Command{RPC: rpcRenew, Args: map[string]any{}}, CommandDeps{})
	if err == nil {
		t.Fatal("an unwired renewal seam must refuse the frame, not silently succeed")
	}
	if !strings.Contains(err.Error(), "renew") {
		t.Fatalf("the refusal must name the verb; got %q", err)
	}
}

// The frame carries no credential and no target, and nothing in the dispatch path
// may start reading one. This is the ruling that made option A option A: the
// station says GO and nothing else, so a forged or mis-routed frame can at worst
// make a machine ask for its own credential again.
func TestDispatchCommand_RenewIgnoresAnythingCarriedInTheFrame(t *testing.T) {
	var handed []string
	deps := CommandDeps{Renew: func() { handed = append(handed, "called") }}
	cmd := &Command{RPC: rpcRenew, Args: map[string]any{
		"token":      "a-credential-the-station-should-not-be-able-to-push",
		"machine_id": "m-somebody-else",
	}}

	if err := dispatchCommand(cmd, deps); err != nil {
		t.Fatalf("renew: %v", err)
	}
	if len(handed) != 1 {
		t.Fatalf("the renewal seam was called %d times; want 1", len(handed))
	}
}

// 🔴 THE WIRING. Delete `transport.deps.Renew = up.RenewNow` from
// wireUpdaterSeams and this goes red; change it to `up.Kick` — which compiles,
// runs and renews nothing — and it goes red too, because the demand is what is
// measured, not the non-nil-ness.
func TestWireUpdaterSeams_RenewRaisesTheDemandOnTheUpdaterItWasGiven(t *testing.T) {
	up := &updater{kick: make(chan struct{}, 1)}
	transport := &sseTransport{}

	wireUpdaterSeams(transport, up)

	if transport.deps.Renew == nil {
		t.Fatal("the renewal seam was not wired at all")
	}
	transport.deps.Renew()

	if !up.renewDemanded.Load() {
		t.Fatal("calling the wired renewal seam did not raise the demand on the " +
			"updater — a warden credential has no expiry, so a seam wired to a plain " +
			"wake would find nothing due and this machine would never renew")
	}
}

// The self-update seams must NOT raise the demand: an owner clicking upgrade, and
// every SSE reconnect, would otherwise replace the machine's credential.
func TestWireUpdaterSeams_TheSelfUpdateSeamsDoNotDemandACredentialReplacement(t *testing.T) {
	for _, c := range []struct {
		name string
		call func(*sseTransport) func()
	}{
		{"the update verb", func(tr *sseTransport) func() { return tr.deps.Update }},
		{"an SSE reconnect", func(tr *sseTransport) func() { return tr.onConnect }},
	} {
		t.Run(c.name, func(t *testing.T) {
			up := &updater{kick: make(chan struct{}, 1)}
			transport := &sseTransport{}
			wireUpdaterSeams(transport, up)

			seam := c.call(transport)
			if seam == nil {
				t.Fatal("the seam was not wired at all")
			}
			seam()

			if up.renewDemanded.Load() {
				t.Fatal("a self-update wake demanded a credential replacement")
			}
			select {
			case <-up.kick:
			default:
				t.Fatal("the seam did not wake the self-update loop")
			}
		})
	}
}
