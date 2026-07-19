package main

// api_infra_test.go — the zombie SSE gate (sseStopGateRefusal + its pre-stream
// wiring in HandleEventsApiEventsGet). The HTTP-integration face (status,
// envelope, presence interplay) is pinned black-box in
// conformance/test_sse.py; here the predicate's every arm is pinned directly.

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// newGateTestAPI assembles a real apiServer over a temp sqlite DB (no HTTP
// mux — the gate tests drive the handler/predicate directly).
func newGateTestAPI(t *testing.T) (*apiServer, *DAL) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "gate-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	api := newAPIServer(dal, NewHub(), []byte(interopSecret), 3600, "../..")
	return api, dal
}

func putGateMember(t *testing.T, dal *DAL, m Member) {
	t.Helper()
	if m.RosterStatus == "" {
		m.RosterStatus = RosterStatusActive
	}
	if err := dal.PutMember(m); err != nil {
		t.Fatalf("PutMember(%s): %v", m.ID, err)
	}
}

func TestSSEStopGateRefusalPredicate(t *testing.T) {
	api, dal := newGateTestAPI(t)

	cases := []struct {
		name    string
		member  *Member // nil = no roster row
		refused bool
	}{
		{"unknown sub admitted (no roster row)", nil, false},
		{"fresh hire admitted (desired offline, no stop anchor)",
			&Member{ID: "g-hire", Kind: KindAssistant, DesiredState: DesiredStateOffline}, false},
		{"desired online admitted",
			&Member{ID: "g-up", Kind: KindAssistant, DesiredState: DesiredStateOnline}, false},
		{"recycle admitted (desired online, stop anchors set)",
			&Member{ID: "g-recycle", Kind: KindAssistant, DesiredState: DesiredStateOnline,
				StoppingSince: 1.0, StoppedSince: 2.0, RefocusSince: 3.0}, false},
		{"deactivated refused (desired offline + stopping_since)",
			&Member{ID: "g-stop", Kind: KindAssistant, DesiredState: DesiredStateOffline,
				StoppingSince: 1.0}, true},
		{"stopped refused (desired offline + stopped_since)",
			&Member{ID: "g-stopped", Kind: KindAssistant, DesiredState: DesiredStateOffline,
				StoppedSince: 1.0}, true},
		{"junk desired parses offline → refused with anchor",
			&Member{ID: "g-junk", Kind: KindAssistant, DesiredState: "bogus",
				StoppingSince: 1.0}, true},
		{"warden exempt from the desired-offline arm",
			&Member{ID: "g-warden", Kind: KindWarden, DesiredState: DesiredStateOffline,
				StoppingSince: 1.0}, false},
		{"removed member refused (any kind)",
			&Member{ID: "g-removed", Kind: KindAssistant, DesiredState: DesiredStateOnline,
				RosterStatus: RosterStatusRemoved}, true},
		{"removed warden refused",
			&Member{ID: "g-removed-warden", Kind: KindWarden, DesiredState: DesiredStateOffline,
				RosterStatus: RosterStatusRemoved}, true},
		// P7d fold: outsource rows keep the pre-fold worker admission. A RELEASED
		// worker is roster-removed + desired-offline, yet its session must stay
		// admitted for the close-out window (worker_spawn.go reclaim grace).
		{"released worker admitted (outsource close-out window)",
			&Member{ID: "g-ow-released", Kind: KindOutsource, DesiredState: DesiredStateOffline,
				StoppedSince: 1.0, RosterStatus: RosterStatusRemoved}, false},
		{"stopped worker admitted (scheduler hold-down, not this gate)",
			&Member{ID: "g-ow-stopped", Kind: KindOutsource, DesiredState: DesiredStateOffline,
				StoppingSince: 1.0}, false},
	}
	for _, tc := range cases {
		id := "g-ghost"
		if tc.member != nil {
			id = tc.member.ID
			putGateMember(t, dal, *tc.member)
		}
		msg := api.sseStopGateRefusal(id)
		if tc.refused && msg == "" {
			t.Errorf("%s: want refusal, got admitted", tc.name)
		}
		if !tc.refused && msg != "" {
			t.Errorf("%s: want admitted, got refusal %q", tc.name, msg)
		}
	}
}

// doEvents drives GET /api/events with agent-scope claims for sub, over a
// PRE-CANCELLED context so an ADMITTED stream returns immediately after the
// 200 header + preamble instead of looping.
func doEvents(api *apiServer, sub string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/events", nil)
	claims := map[string]any{"sub": sub, "scope": "agent"}
	ctx, cancel := context.WithCancel(
		context.WithValue(req.Context(), claimsContextKey, claims))
	cancel() // admitted streams exit on the first loop check
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	api.HandleEventsApiEventsGet(rec, req)
	return rec
}

func TestEventsHandlerAppliesStopGatePreStream(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "z-1", Kind: KindAssistant,
		DesiredState: DesiredStateOffline, StoppingSince: 1.0})

	rec := doEvents(api, "z-1")
	if rec.Code != 409 {
		t.Fatalf("zombie reconnect: want pre-stream 409, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"conflict"`) {
		t.Fatalf("want the conflict envelope, got %s", rec.Body.String())
	}
	if api.hub.IsOnline("z-1") {
		t.Fatal("a refused connection must never project online")
	}

	// The stop→start transition lifts the gate in the same write activate does:
	// desired online + anchors cleared → admitted (200 + SSE headers).
	putGateMember(t, dal, Member{ID: "z-1", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	rec = doEvents(api, "z-1")
	if rec.Code != 200 {
		t.Fatalf("post-activate reconnect: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("admitted connection must stream, got Content-Type %q", ct)
	}
	if !strings.Contains(rec.Body.String(), ": connected") {
		t.Fatalf("admitted stream must open with the connected preamble, got %q", rec.Body.String())
	}
	if api.hub.IsOnline("z-1") {
		t.Fatal("the pre-cancelled test stream must have disconnected (projection cleared)")
	}
}
