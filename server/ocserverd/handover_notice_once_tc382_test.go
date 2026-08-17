package main

// handover_notice_once_tc382_test.go — T-c382 guard on 「只通知一次」.
//
// The owner's complaint was not that the reminder existed; it was that it
// arrived FIVE times (40/45/50/55/60 on the way to a 65% handover) and that an
// agent obeying the first one stops working with a quarter of its context
// unspent. So "exactly once" is the requirement, and this file measures it by
// counting, not by checking that a single call returns something.
//
// 🔴 The subtle half is the SCOPE of "once". Once per CONNECTION is easy and
// wrong: an SSE stream flaps, the agent reconnects mid-session, and the notice
// fires again — the same bombardment wearing a different hat, and invisible to
// any test that only ever opens one connection. Once per SESSION is the
// requirement, and the session anchor is the gauge's boot_ts (stamped once per
// session, restored from the durable member row across a reconnect). The
// reconnect case below is the one that separates the two implementations; the
// rest would pass either way.

import "testing"

func noticeGauge(bootTS float64) map[string]any {
	return map[string]any{"context_pct": 55.0, "context_pct_ts": bootTS + 10, "boot_ts": bootTS}
}

func TestHandoverNotice_OncePerSession(t *testing.T) {
	api := newTasksTestServer(t)
	rec := noticeGauge(1000)

	// Many quiet ticks on one connection: exactly one claim.
	claims := 0
	for i := 0; i < 50; i++ {
		if api.claimHandoverNotice("m-1", rec) {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("a parked gauge must be claimed exactly once, got %d "+
			"(the owner was nudged five times; that is the bug)", claims)
	}

	// 🔴 THE RECONNECT. Same session (boot_ts is restored, so it is the SAME
	// anchor), fresh stream. Per-connection state would hand out a second
	// notice here; per-session state must not.
	if api.claimHandoverNotice("m-1", noticeGauge(1000)) {
		t.Fatal("a mid-session SSE reconnect must NOT re-notify — the dedup key " +
			"has to be the session anchor, not the connection")
	}

	// A genuinely NEW session (spawn/stop boundary cleared boot_ts, so the next
	// first-connect stamps a new one) is entitled to its own notice. Without
	// this, "deduped correctly" and "notifies once ever and then never again"
	// are the same green.
	if !api.claimHandoverNotice("m-1", noticeGauge(2000)) {
		t.Fatal("a new session must get its own notice")
	}

	// Independent agents do not suppress each other.
	if !api.claimHandoverNotice("m-2", noticeGauge(1000)) {
		t.Fatal("one agent's notice must not consume another's")
	}
}

func TestHandoverNotice_FailsSafeWithoutASessionAnchor(t *testing.T) {
	api := newTasksTestServer(t)
	// No boot_ts (no gauge, or server-restart amnesia): refuse to claim rather
	// than fire off an anchor we could never recognise again — which would make
	// the notice repeat on every single tick, forever.
	for _, rec := range []map[string]any{
		nil,
		{"context_pct": 55.0},
		{"context_pct": 55.0, "boot_ts": 0.0},
	} {
		if api.claimHandoverNotice("m-1", rec) {
			t.Fatalf("no usable session anchor must not claim: %v", rec)
		}
	}
}
