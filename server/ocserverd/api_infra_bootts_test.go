package main

// api_infra_bootts_test.go — the boot_ts session-anchor fix (T-8fb2). boot_ts
// anchors the SESSION, not the individual SSE connection: a mid-session flap
// (drop → reconnect) must NOT reset it (else the min-liveness gate keeps seeing
// "just booted" and an edge-flapping agent can never self-rescue / be
// auto-handed-over), while a genuinely new session (spawn/stop boundary) MUST
// re-stamp. onFirstConnect stamps IFF absent; clearSessionBootTS drops it at the
// boundary so the next connect re-stamps.

import "testing"

// TestOnFirstConnectStampsBootTSWhenAbsent — the first connect of a session
// stamps a fresh boot_ts.
func TestOnFirstConnectStampsBootTSWhenAbsent(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "bt-new", Kind: KindAssistant})

	api.onFirstConnect("bt-new")

	got, ok := gaugeBootTS(api.gauge.Get("bt-new"))
	if !ok || got <= 0 {
		t.Fatalf("first connect must stamp a boot_ts; got %v ok=%t", got, ok)
	}
}

// TestOnFirstConnectDoesNotResetBootTSOnReconnect — the reported bug (Joey):
// a mid-session SSE flap re-fires onFirstConnect, but boot_ts must survive
// unchanged so the session age keeps growing.
func TestOnFirstConnectDoesNotResetBootTSOnReconnect(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "bt-flap", Kind: KindAssistant})

	// A session that connected well in the past.
	orig := nowSecs() - 500
	api.gauge.Set("bt-flap", map[string]any{"boot_ts": orig})

	// Reconnect (same session, no spawn/stop boundary crossed).
	api.onFirstConnect("bt-flap")

	got, ok := gaugeBootTS(api.gauge.Get("bt-flap"))
	if !ok {
		t.Fatal("reconnect must keep the existing boot_ts, not drop it")
	}
	if got != orig {
		t.Fatalf("reconnect must NOT reset boot_ts: want %v (unchanged), got %v", orig, got)
	}
}

// TestClearSessionBootTSThenReconnectReStamps — a spawn/stop boundary clears the
// anchor, so the NEXT connect (a genuinely new session) re-stamps fresh.
func TestClearSessionBootTSThenReconnectReStamps(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "bt-respawn", Kind: KindAssistant})

	orig := nowSecs() - 500
	api.gauge.Set("bt-respawn", map[string]any{"boot_ts": orig})

	// Session boundary (a kill/START dispatch would call this).
	api.clearSessionBootTS("bt-respawn")
	if _, ok := gaugeBootTS(api.gauge.Get("bt-respawn")); ok {
		t.Fatal("clearSessionBootTS must drop the boot_ts anchor")
	}

	// New session connects.
	api.onFirstConnect("bt-respawn")
	got, ok := gaugeBootTS(api.gauge.Get("bt-respawn"))
	if !ok {
		t.Fatal("a new session's first connect must re-stamp boot_ts")
	}
	if got == orig {
		t.Fatalf("new session must get a FRESH boot_ts, not the old %v", orig)
	}
}

// TestClearSessionBootTSPreservesOtherGaugeFields — clearing the anchor must not
// wipe the rest of the gauge entry (context_pct etc.).
func TestClearSessionBootTSPreservesOtherGaugeFields(t *testing.T) {
	api, _ := newGateTestAPI(t)
	api.gauge.Set("bt-keep", map[string]any{"boot_ts": 100.0, "context_pct": 42.0})

	api.clearSessionBootTS("bt-keep")

	entry := api.gauge.Get("bt-keep")
	if _, ok := entry["boot_ts"]; ok {
		t.Fatal("boot_ts must be gone")
	}
	if pct, ok := asNumber(entry["context_pct"]); !ok || pct != 42.0 {
		t.Fatalf("context_pct must survive the clear; got %v ok=%t", pct, ok)
	}
}

// TestDispatchRobustStopClearsBootTS — the robust kill (force-stop /
// report_stopped recycle / relocate) is a session boundary: it clears boot_ts so
// the respawn's first connect re-stamps.
func TestDispatchRobustStopClearsBootTS(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "bt-stop", Kind: KindAssistant,
		DesiredState: DesiredStateOnline, DesiredMachineID: "m1"})
	api.gauge.Set("bt-stop", map[string]any{"boot_ts": nowSecs() - 500})

	api.dispatchRobustStopNow("bt-stop")

	if _, ok := gaugeBootTS(api.gauge.Get("bt-stop")); ok {
		t.Fatal("robust STOP must clear the boot_ts session anchor")
	}
}
