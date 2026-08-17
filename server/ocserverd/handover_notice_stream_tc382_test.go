package main

// handover_notice_stream_tc382_test.go — T-c382 「只通知一次」at the STREAM seam.
//
// handover_notice_once_tc382_test.go pins the dedup HELPER. That is not enough:
// the helper can be perfect while the stream loop keeps its own per-connection
// map and calls it per connection, and every helper test still passes. Measured:
// swapping api_infra.go's session-keyed claim for a connection-local map leaves
// the whole package green. So this file drives the real handler and counts what
// a client would actually receive, across a RECONNECT — the one scope where the
// two implementations differ.
//
// The reconnect is modelled as the production one: a second connection for the
// same member takes the slot over (spec/sse.md §5.1) while the SESSION continues
// — member.session_boot_ts is untouched, so the anchor is restored, which is
// exactly the mid-session SSE flap the owner must not be re-nudged by.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// contextHighFrameMark counts FRAMES, so it anchors on the envelope prefix: the
// directed envelope duplicates the topic inside its payload (spec §6), so a bare
// `"topic":"context-high"` appears TWICE per frame and would count one notice as
// two.
const contextHighFrameMark = `data: {"topic":"context-high"`

// waitForBody polls a sinkWriter until want appears, and fails the test if it
// never does — so a notice that silently never fires cannot pass as "did not
// fire twice".
func waitForBody(t *testing.T, w *sinkWriter, want, what string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for !strings.Contains(w.bodyText(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never arrived: body=%q", what, w.bodyText())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHandoverNoticeStreamOncePerSessionAcrossReconnect(t *testing.T) {
	api, dal := newGateTestAPI(t)
	const id = "hn-stream"
	// A session anchored well before now, with a gauge parked ABOVE the derived
	// notice point (default handover 50 → notice at 40) and a pct reported after
	// the anchor, so the stale guard lets it drive the decision.
	anchor := nowSecs() - 600
	putGateMember(t, dal, Member{ID: id, Kind: KindAssistant,
		Runtime: RuntimeClaude, DesiredState: DesiredStateOnline,
		SessionBootTS: anchor})
	api.gauge.Set(id, map[string]any{
		"boot_ts": anchor, "context_pct": 55.0, "context_pct_ts": anchor + 10})

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	w1 := newSinkWriter()
	done1 := startEventsHandler(api, w1, agentEventsRequest(ctx1, id))
	waitOnline(t, api, id)
	waitForBody(t, w1, contextHighFrameMark, "the advance handover notice")

	// The gauge stays parked, so every later quiet tick re-decides the same
	// signal: only the claim keeps it to one.
	time.Sleep(6 * ssePoll)
	if n := strings.Count(w1.bodyText(), contextHighFrameMark); n != 1 {
		t.Fatalf("a parked gauge must yield exactly ONE notice on the wire, got %d "+
			"(the owner was nudged five times; that is the bug)", n)
	}

	// 🔴 THE RECONNECT. Same session (session_boot_ts untouched → the anchor is
	// restored), fresh connection. Per-connection dedup re-notifies here; the
	// requirement is silence.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	w2 := newSinkWriter()
	done2 := startEventsHandler(api, w2, agentEventsRequest(ctx2, id))
	select {
	case <-done1:
	case <-time.After(4 * time.Second):
		t.Fatal("the displaced connection was not released")
	}
	// Greet first: without it, "the loop never got going" would look like
	// "the loop correctly stayed quiet".
	waitForBody(t, w2, ": connected", "the reconnect greeting")
	time.Sleep(8 * ssePoll)
	if strings.Contains(w2.bodyText(), contextHighFrameMark) {
		t.Fatalf("a mid-session SSE reconnect must NOT re-notify — the dedup key has "+
			"to be the session anchor, not the connection: body=%q", w2.bodyText())
	}

	cancel2()
	select {
	case <-done2:
	case <-time.After(4 * time.Second):
		t.Fatal("the reconnect handler did not exit on context cancel")
	}
}

// TestHandoverNoticeClearedAtSessionBoundary pins the other half of "per
// SESSION": the claim is session-scoped state, so the session boundary that
// drops the anchor must drop it too. Without this the record outlives every
// session the process ever sees.
func TestHandoverNoticeClearedAtSessionBoundary(t *testing.T) {
	api, dal := newGateTestAPI(t)
	const id = "hn-boundary"
	anchor := nowSecs() - 600
	putGateMember(t, dal, Member{ID: id, Kind: KindAssistant,
		DesiredState: DesiredStateOnline, SessionBootTS: anchor})
	if !api.claimHandoverNotice(id, map[string]any{"boot_ts": anchor}) {
		t.Fatal("the first claim of a session must succeed")
	}

	api.clearSessionBootTS(id)

	api.settingsMu.RLock()
	_, still := api.handoverNoticed[id]
	api.settingsMu.RUnlock()
	if still {
		t.Fatal("a session boundary must drop the notice claim with the anchor it " +
			"is keyed on — session-scoped state must not outlive the session")
	}
}
