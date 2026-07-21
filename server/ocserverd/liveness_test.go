package main

import "testing"

// gaugeWith builds a gauge record carrying a last-report ts (and nothing else),
// the one input gaugeLastReportSecs / decideLiveness read for silence.
func gaugeWith(ts float64) map[string]any { return map[string]any{"ts": ts} }

func TestGaugeLastReportSecs(t *testing.T) {
	const now = 10_000.0
	cases := []struct {
		name   string
		record map[string]any
		want   *float64 // nil = fail-open
	}{
		{"nil record fails open", nil, nil},
		{"no ts key fails open", map[string]any{"context_pct": 42.0}, nil},
		{"zero ts fails open (never reported sentinel)", gaugeWith(0), nil},
		{"negative ts fails open", gaugeWith(-1), nil},
		{"non-numeric ts fails open", map[string]any{"ts": "soon"}, nil},
		{"fresh report → small delta", gaugeWith(now - 5), fptr(5)},
		{"old report → large delta", gaugeWith(now - 3600), fptr(3600)},
	}
	for _, c := range cases {
		got := gaugeLastReportSecs(c.record, now)
		switch {
		case c.want == nil && got != nil:
			t.Errorf("%s: got %v, want nil (fail-open)", c.name, *got)
		case c.want != nil && got == nil:
			t.Errorf("%s: got nil, want %v", c.name, *c.want)
		case c.want != nil && got != nil && *got != *c.want:
			t.Errorf("%s: got %v, want %v", c.name, *got, *c.want)
		}
	}
}

func TestTotalUnread(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]int
		want int
	}{
		{"nil map is zero", nil, 0},
		{"empty map is zero", map[string]int{}, 0},
		{"single sender", map[string]int{"owner": 3}, 3},
		{"sums across senders", map[string]int{"owner": 2, "m-abc": 5, "hook:ci": 1}, 8},
	}
	for _, c := range cases {
		if got := totalUnread(c.in); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}

// TestDecideLiveness is the core judgment. It covers BOTH directions the ticket
// weights — the real stuck agent IS flagged, and (the sentinel that matters
// more) an idle-but-normal agent is NOT — plus every fail-open guard.
func TestDecideLiveness(t *testing.T) {
	const now = 100_000.0
	const silence = stuckSilenceSecs // 900s
	stale := gaugeWith(now - silence - 1)
	fresh := gaugeWith(now - 10)

	cases := []struct {
		name      string
		record    map[string]any
		unread    int
		online    bool
		wantStuck bool
	}{
		// ── the real stuck agent: online, silent past the window, unread waits ──
		{"stuck: online + silent + unread waiting", stale, 1, true, true},

		// ── the sentinel (DoD#4, more important): a normal quiet agent, NOT stuck ─
		{"idle-normal: online + silent + NOTHING waiting → not stuck", stale, 0, true, false},

		// ── a working agent still reporting is never stuck, unread or not ─────────
		{"working: online + fresh report + unread → not stuck", fresh, 5, true, false},
		{"working: online + fresh report + no unread → not stuck", fresh, 0, true, false},

		// ── offline is not stuck (presence / zombie-takeover own that), even with
		//    a silent gauge and unread piled up ──────────────────────────────────
		{"offline is never stuck (silent + unread)", stale, 9, false, false},

		// ── fail-open: no usable report ts → never a fabricated alarm ────────────
		{"fail-open: online + no ts + unread → not stuck", map[string]any{}, 3, true, false},
		{"fail-open: online + nil record + unread → not stuck", nil, 3, true, false},

		// ── boundary: idle EXACTLY at the window is not yet stuck (uses <=) ───────
		{"boundary: idle == silence window is not stuck", gaugeWith(now - silence), 1, true, false},
		{"boundary: idle one past the window IS stuck", gaugeWith(now - silence - 1), 1, true, true},
	}
	for _, c := range cases {
		got := decideLiveness(c.record, c.unread, c.online, silence, now)
		if got.Stuck != c.wantStuck {
			t.Errorf("%s: Stuck=%v want %v (reason: %s)", c.name, got.Stuck, c.wantStuck, got.Reason)
		}
	}
}

// TestLivenessForMembers drives the FULL wiring — the real hub online gate, the
// gauge report ts, and the DAL unread scan — not just the pure decider. It
// carries the DoD#4 sentinel end-to-end: a normal quiet agent (online, silent,
// but nothing unread) must NOT be flagged even as a truly stuck peer beside it
// is. A green here after a mutant to the decider is the "test froze the bug as
// spec" trap the ticket warns about, so this is paired with the decider mutants.
func TestLivenessForMembers(t *testing.T) {
	dal := newTestDAL(t)
	api := &apiServer{dal: dal, hub: NewHub(), gauge: newMemStore(), stuckFlagged: map[string]bool{}}
	const now = 1_000_000.0

	const stuck, idleNormal, working = "mb-stuck", "mb-idle", "mb-working"
	// stuck + idleNormal: both silent past the window; working: fresh report.
	api.gauge.Set(stuck, map[string]any{"ts": now - stuckSilenceSecs - 60})
	api.gauge.Set(idleNormal, map[string]any{"ts": now - stuckSilenceSecs - 60})
	api.gauge.Set(working, map[string]any{"ts": now - 30})

	// All three hold a live SSE (the failure mode: body wedged, light still green).
	for _, id := range []string{stuck, idleNormal, working} {
		defer online(t, api, id)()
	}

	// Only the stuck member has an unread inbound message waiting on it.
	if err := dal.PutChat(ChatMessage{
		ID: "c-1", Sender: "owner", Recipient: stuck, Body: "you there?", TS: now - 500,
	}); err != nil {
		t.Fatalf("PutChat: %v", err)
	}
	// A message the working member has ALREADY read — proves "unread", not "any
	// message", is the trigger (working is also excluded by its fresh report, so
	// this is a belt-and-suspenders control).
	if err := dal.PutChat(ChatMessage{
		ID: "c-2", Sender: "owner", Recipient: working, Body: "hi", TS: now - 5000,
	}); err != nil {
		t.Fatalf("PutChat: %v", err)
	}
	if _, _, err := dal.PutChatRead(ChatRead{ReaderID: working, PeerID: "owner", LastReadTS: now - 100}); err != nil {
		t.Fatalf("PutChatRead: %v", err)
	}

	sigs := api.livenessForMembers([]string{stuck, idleNormal, working}, now)

	if !sigs[stuck].Stuck {
		t.Errorf("stuck member: got not-stuck, want stuck (reason: %s)", sigs[stuck].Reason)
	}
	if sigs[idleNormal].Stuck {
		t.Errorf("idle-normal SENTINEL misfired: online+silent but nothing waiting was flagged stuck (reason: %s)",
			sigs[idleNormal].Reason)
	}
	if sigs[working].Stuck {
		t.Errorf("working member (fresh report): got stuck, want not-stuck (reason: %s)", sigs[working].Reason)
	}

	// The reconcile-tick pass records the edge for the stuck member only, and
	// takes NO remedy (signal-only): stuckFlagged is the only state it writes.
	api.flagStuckMembers([]Member{{ID: stuck}, {ID: idleNormal}, {ID: working}}, now)
	if !api.stuckFlagged[stuck] {
		t.Errorf("flagStuckMembers: stuck member not recorded on the edge map")
	}
	if api.stuckFlagged[idleNormal] || api.stuckFlagged[working] {
		t.Errorf("flagStuckMembers: a non-stuck member was flagged (idle=%v working=%v)",
			api.stuckFlagged[idleNormal], api.stuckFlagged[working])
	}
}
