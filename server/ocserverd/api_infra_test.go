package main

// api_infra_test.go — the zombie SSE gate (sseStopGateRefusal + its pre-stream
// wiring in HandleEventsApiEventsGet). The HTTP-integration face (status,
// envelope, presence interplay) is pinned black-box in
// conformance/test_sse.py; here the predicate's every arm is pinned directly.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// tokenExpiryRecorder closes the otherwise long-lived SSE handler exactly
// after the token-expiry frame reaches the response body. It drives the real
// wire path without sleeping for a heartbeat or relying on a synthetic band
// call as proof of delivery.
type tokenExpiryRecorder struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (r *tokenExpiryRecorder) Write(p []byte) (int, error) {
	n, err := r.ResponseRecorder.Write(p)
	if bytes.Contains(p, []byte(`"topic":"token-expiry"`)) {
		r.cancel()
	}
	return n, err
}

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
	api := newAPIServer(dal, NewHub(), singleKeyring([]byte(interopSecret)), 3600, "../..")
	return api, dal
}

// putGateMember seeds a member row so the ROW ENDS UP LOOKING LIKE `m` — the
// third helper of this shape, beside putTestMember and putWorkerFixture.
//
// 🔴 THE ANCHOR WRITE IS NOT REDUNDANT (T-55). The four wind-down anchors left
// PutMember's DO UPDATE SET, so on a row that ALREADY EXISTS the upsert above
// silently drops them. This helper's callers re-seed the same id to move a
// member between gate states — including the stop→start case that clears the
// anchors — and without this second write that re-seed plants nothing while
// still reading like it did. The test then passes on the half it can still
// satisfy and stops exercising the half it was written for.
func putGateMember(t *testing.T, dal *DAL, m Member) {
	t.Helper()
	if m.RosterStatus == "" {
		m.RosterStatus = RosterStatusActive
	}
	if err := dal.PutMember(m); err != nil {
		t.Fatalf("PutMember(%s): %v", m.ID, err)
	}
	if err := dal.SetMemberWindDownAnchors(m.ID, m.StoppingSince, m.StoppedSince,
		m.RefocusSince, m.RefocusOp); err != nil {
		t.Fatalf("seed wind-down anchors for %s: %v", m.ID, err)
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
		// 🔴 A plain deactivate is now ADMITTED while the close-out runs (T-a9d6):
		// 下線 collects on the agent's own stopped report, not a clock, so the
		// session legitimately holds this state for as long as the hand-off
		// takes — and a refusal here is not inert, the listener reads a run of
		// them as "I have been retired" and kills its own tmux session.
		{"deactivated ADMITTED while the close-out is still in flight",
			&Member{ID: "g-stop", Kind: KindAssistant, DesiredState: DesiredStateOffline,
				StoppingSince: 1.0}, false},
		// …and the two ways that stops being true: the agent says it is done,
		// or the owner cut it off. Both must still be refused, or the gate has
		// simply been removed.
		{"force-stopped refused even before it reports",
			&Member{ID: "g-forced", Kind: KindAssistant, DesiredState: DesiredStateOffline,
				StoppingSince: 1.0, ForcedStopAt: 2.0}, true},
		{"stopped refused (desired offline + stopped_since)",
			&Member{ID: "g-stopped", Kind: KindAssistant, DesiredState: DesiredStateOffline,
				StoppedSince: 1.0}, true},
		{"junk desired parses offline → still gated (admitted only because the " +
			"close-out is in flight, same as a real deactivate)",
			&Member{ID: "g-junk", Kind: KindAssistant, DesiredState: "bogus",
				StoppingSince: 1.0}, false},
		{"junk desired + reported stopped → refused",
			&Member{ID: "g-junk2", Kind: KindAssistant, DesiredState: "bogus",
				StoppingSince: 1.0, StoppedSince: 2.0}, true},
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
	// A member that has REPORTED STOPPED — the finished case. (A stop anchor
	// alone no longer refuses: that is a close-out in flight, see the predicate
	// table above.)
	putGateMember(t, dal, Member{ID: "z-1", Kind: KindAssistant,
		DesiredState: DesiredStateOffline, StoppingSince: 1.0, StoppedSince: 2.0})

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

func TestEventsHandlerDeliversTokenExpiryReminderOnTheRealSSEWire(t *testing.T) {
	api, dal := newGateTestAPI(t)
	now := int64(nowSecs())
	putGateMember(t, dal, Member{
		ID: "expiry-wire", Kind: KindAssistant, DesiredState: DesiredStateOnline,
		// The session is old enough that restart_self is currently permitted.
		SessionBootTS: float64(now) - minSelfRestartSecs - 1,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/api/events", nil).WithContext(
		context.WithValue(ctx, claimsContextKey, map[string]any{
			"sub": "expiry-wire", "scope": "agent",
			// Exactly at the owner-approved thirty-minute boundary.
			"exp": float64(now + tokenExpiryWarningWindow),
		}))
	rec := &tokenExpiryRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	api.HandleEventsApiEventsGet(rec, req)

	text := rec.Body.String()
	if !strings.Contains(text, `data: {"topic":"token-expiry"`) || strings.Contains(text, "id: ") {
		t.Fatalf("expiry reminder must be a bare directed SSE frame, got %q", text)
	}
	if !strings.Contains(text, `"to":"expiry-wire"`) ||
		!strings.Contains(text, `"expires_in":`) ||
		!strings.Contains(text, "restart_self") {
		t.Fatalf("expiry frame must target the live agent and instruct restart_self, got %q", text)
	}
}

// The sha on the SSE response is only useful if it is the SAME build the
// station names anywhere else — so it is pinned against /api/version's
// git_sha rather than against a literal, and read off rec.Result().Header,
// which is the snapshot httptest takes at WriteHeader. A header set after the
// status line never reaches a real client; asserting on the post-hoc
// rec.Header() map would not notice.
func TestEventsHandlerStampsTheBuildApiVersionReportsAsGitSHA(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "sha-1", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	api.processSHA = "deadbeefdead"

	rec := doEvents(api, "sha-1")
	if rec.Code != 200 {
		t.Fatalf("admitted stream: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	stamped := rec.Result().Header.Get(sseStationSHAHeader)
	if stamped != api.processSHA {
		t.Fatalf("SSE %s = %q, want the running build %q (set before WriteHeader?)",
			sseStationSHAHeader, stamped, api.processSHA)
	}

	vrec := httptest.NewRecorder()
	api.HandleVersionApiVersionGet(vrec, httptest.NewRequest("GET", "/api/version", nil))
	var version struct {
		GitSHA string `json:"git_sha"`
	}
	if err := json.Unmarshal(vrec.Body.Bytes(), &version); err != nil {
		t.Fatalf("decode /api/version: %v (%s)", err, vrec.Body.String())
	}
	if version.GitSHA != stamped {
		t.Fatalf("the two faces of one build disagree: /api/version git_sha=%q, "+
			"SSE %s=%q", version.GitSHA, sseStationSHAHeader, stamped)
	}
}

// TestHandoverNoticeTick_ClosureIsNotRunAfterTheClaim
//
// 🔴 WHAT WAS ACTUALLY WRONG. decideHandoverNotice has no memory: once an agent
// is past its notice point it returns a signal on EVERY quiet tick, and the
// once-per-session gate (claimHandoverNotice) is asked AFTER that signal has
// been composed. So the notice closure — a fold over a durable document, its
// variables rendered — ran every 250ms for the rest of the session, and every run after the first
// was thrown away. Two comments in sse_bands.go asserted the exact opposite; a
// comment cannot be run, so this counts instead.
//
// It fails if handoverNoticeTick stops asking handoverNoticeSettled first.
func TestHandoverNoticeTick_ClosureIsNotRunAfterTheClaim(t *testing.T) {
	s, dal := newGateTestAPI(t)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s.ctxhigh = SseContextHighConfig{HandoverPct: 65, NoticePct: 55}
	s.gauge.Set(seedMiraID, map[string]any{"context_pct": 56.0, "boot_ts": 1000.0})

	runs := 0
	notice := func() string {
		runs++
		return s.winddownNoticeText(offboardKindSoft, 0)
	}

	frame, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, notice)
	if !ok || len(frame) == 0 {
		t.Fatal("the first tick past the notice point must send the session's one notice")
	}
	if runs != 1 {
		t.Fatalf("the sending tick must compose the text exactly once: %d", runs)
	}

	// The rest of the session. Every one of these ticks is above the notice
	// point, so decideHandoverNotice would still say "send" — the claim is what
	// makes them silent, and the point of this test is that they must be silent
	// WITHOUT paying for the text first.
	for i := 0; i < 200; i++ {
		if _, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, notice); ok {
			t.Fatalf("tick %d re-sent the once-per-session notice", i)
		}
	}
	if runs != 1 {
		t.Fatalf("200 silent ticks composed text they threw away: %d (want 1) — the "+
			"notice fires once per session, so its cost must too", runs)
	}
}

// TestHandoverNoticeTick_ANewSessionStillPaysAndStillSends is the OTHER
// direction of the same guard, and it is not optional: "never runs the closure
// again" and "went permanently mute for this agent" are the same green
// otherwise. A new boot_ts is a new session and is entitled to its own notice.
func TestHandoverNoticeTick_ANewSessionStillPaysAndStillSends(t *testing.T) {
	s, dal := newGateTestAPI(t)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s.ctxhigh = SseContextHighConfig{HandoverPct: 65, NoticePct: 55}
	s.gauge.Set(seedMiraID, map[string]any{"context_pct": 56.0, "boot_ts": 1000.0})

	runs := 0
	// A non-empty answer, because an unrenderable notice now keeps the tick
	// SILENT — returning "" here would make this test measure that instead.
	notice := func() string { runs++; return "停止" }
	if _, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, notice); !ok {
		t.Fatal("first session must be told")
	}
	if _, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, notice); ok {
		t.Fatal("second tick of the SAME session must stay quiet")
	}

	// New session: the agent restarted, its gauge carries a new anchor.
	s.gauge.Set(seedMiraID, map[string]any{"context_pct": 56.0, "boot_ts": 2000.0})
	if _, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, notice); !ok {
		t.Fatal("a NEW session must still get its own notice — a cost guard that " +
			"silences the feature has removed the feature")
	}
	if runs != 2 {
		t.Fatalf("the closure must run exactly once per SENDING tick: got %d, want 2", runs)
	}
}
