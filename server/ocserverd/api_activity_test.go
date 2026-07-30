package main

// api_activity_test.go — T-a1d7. Two halves:
//
//   * deriveActivity, the ONE verdict function — the four states, the
//     max-turn boundary, and the online gate;
//   * the ingestion handler + the two SSE lifecycle edges — the ordering /
//     de-duplication rules, and the "a drop must not destroy a claim, but must
//     not display one either" behaviour.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── deriveActivity (pure) ────────────────────────────────────────────────────

func TestDeriveActivity_FourStates(t *testing.T) {
	const now = 10_000.0
	cases := []struct {
		name      string
		claim     activityClaim
		online    bool
		want      string
		wantSince *float64
		wantEnd   *float64
	}{
		{
			name:  "nothing ever reported is never, not idle",
			claim: activityClaim{},
			// A dash on screen. Saying "idle" here would assert something we
			// have no source for — an old runtime with no hooks looks exactly
			// like this.
			online: true, want: ActivityNever,
		},
		{
			name:   "a fresh claim on an online agent is active",
			claim:  activityClaim{Reported: true, Active: true, Since: now - 60},
			online: true, want: ActivityActive, wantSince: f(now - 60),
		},
		{
			name: "a claim past the window degrades to unknown, KEEPING the anchor",
			claim: activityClaim{Reported: true, Active: true,
				Since: now - activityMaxTurnSecs - 1},
			online: true, want: ActivityUnknown,
			wantSince: f(now - activityMaxTurnSecs - 1),
		},
		{
			name: "a closed claim is idle and serves the observed end",
			claim: activityClaim{Reported: true, Active: false,
				LastEnd: now - 180},
			online: true, want: ActivityIdle, wantEnd: f(now - 180),
		},
		{
			name:   "idle with nothing ever observed to end serves NO end stamp",
			claim:  activityClaim{Reported: true},
			online: true, want: ActivityIdle,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state, since, end := deriveActivity(c.claim, c.online, now)
			if state != c.want {
				t.Fatalf("state = %q, want %q", state, c.want)
			}
			if !ptrEq(since, c.wantSince) {
				t.Fatalf("working_since = %v, want %v", show(since), show(c.wantSince))
			}
			if !ptrEq(end, c.wantEnd) {
				t.Fatalf("last_turn_completed_at = %v, want %v", show(end), show(c.wantEnd))
			}
		})
	}
}

// The boundary is the WHOLE decision this constant makes, so pin both sides of
// it rather than a comfortable value in the middle.
func TestDeriveActivity_MaxTurnBoundary(t *testing.T) {
	const now = 10_000.0
	claim := func(age float64) activityClaim {
		return activityClaim{Reported: true, Active: true, Since: now - age}
	}
	if state, _, _ := deriveActivity(claim(activityMaxTurnSecs), true, now); state != ActivityActive {
		t.Fatalf("exactly at the window must still be active, got %q", state)
	}
	if state, _, _ := deriveActivity(claim(activityMaxTurnSecs+0.001), true, now); state != ActivityUnknown {
		t.Fatalf("one tick past the window must be unknown, got %q", state)
	}
}

// The online GATE is what satisfies "an offline agent must never render as
// working" — and it does so without destroying the claim, which is the whole
// point (a deleted claim cannot come back when the blip ends).
func TestDeriveActivity_OfflineNeverReadsAsWorking(t *testing.T) {
	const now = 10_000.0
	live := activityClaim{Reported: true, Active: true, Since: now - 10, LastEnd: now - 500}
	state, since, end := deriveActivity(live, false, now)
	if state != ActivityIdle {
		t.Fatalf("an offline actor must never read active/unknown, got %q", state)
	}
	if since != nil {
		t.Fatalf("an offline actor must serve no working_since, got %v", *since)
	}
	if end == nil || *end != now-500 {
		t.Fatalf("the observed end must survive the gate, got %v", show(end))
	}
	// …and the claim itself is untouched: reconnecting inside the grace shows
	// it again.
	if state, _, _ := deriveActivity(live, true, now); state != ActivityActive {
		t.Fatalf("the claim must be intact for a reconnect, got %q", state)
	}
}

func TestDeriveActivity_NeverInventsAnEndForAnUnreportedActor(t *testing.T) {
	// F1 in the failure table: a member that never ran a turn must NOT read
	// "last ended 0 minutes ago".
	_, since, end := deriveActivity(activityClaim{}, true, 10_000.0)
	if since != nil || end != nil {
		t.Fatalf("an unreported actor must carry no stamps, got since=%v end=%v",
			show(since), show(end))
	}
}

func TestActivityReconnectKeepsClaim(t *testing.T) {
	const grace = 180.0
	cases := []struct {
		name         string
		offlineSince float64
		now          float64
		want         bool
	}{
		{"a blip inside the grace keeps the turn", 1000, 1000 + grace - 1, true},
		{"a real absence at the grace drops it", 1000, 1000 + grace, false},
		{"never seen offline resurrects nothing", 0, 5000, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := activityReconnectKeepsClaim(c.offlineSince, c.now, grace); got != c.want {
				t.Fatalf("keepsClaim = %v, want %v", got, c.want)
			}
		})
	}
}

// ── ingestion handler ────────────────────────────────────────────────────────

func activityTestServer() *apiServer {
	return &apiServer{activity: newMemStore(), hub: NewHub(), telemetry: newMemStore()}
}

func doReportActivity(api *apiServer, sub, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/self/activity", strings.NewReader(body))
	claims := map[string]any{"sub": sub, "scope": "agent"}
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	api.HandleReportActivityApiSelfActivityPost(rec, req)
	return rec
}

func activityReceipt(t *testing.T, rec *httptest.ResponseRecorder) ActivityReportResultDTO {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("report: %d %s", rec.Code, rec.Body.String())
	}
	var out ActivityReportResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode receipt: %v (%s)", err, rec.Body.String())
	}
	return out
}

func TestReportActivity_RejectsAnyStateOutsideTheClosedSet(t *testing.T) {
	api := activityTestServer()
	for _, body := range []string{
		`{"state":"working"}`, `{"state":""}`, `{"state":"ACTIVE"}`,
	} {
		if rec := doReportActivity(api, "m-1", body); rec.Code != 400 {
			t.Fatalf("%s must be a 400, got %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if api.activity.Get("m-1") != nil {
		t.Fatal("a refused report must not create a store entry")
	}
}

// An outsource worker has NO member row. It is also the session an owner most
// wants an activity reading on, so this is not an edge case — it is the reason
// the route sits at the agent floor and reads the sub directly.
func TestReportActivity_MemberlessWorkerIsAccepted(t *testing.T) {
	api := activityTestServer()
	got := activityReceipt(t, doReportActivity(api, "ow-42", `{"state":"active","turn_id":"t1"}`))
	if !got.Accepted {
		t.Fatal("an ow- caller's report must be accepted (no member row required)")
	}
	if api.activity.Get("ow-42") == nil {
		t.Fatal("the worker's claim must be stored")
	}
}

func TestReportActivity_IdleClosesTheClaimAndStampsTheObservedEnd(t *testing.T) {
	api := activityTestServer()
	activityReceipt(t, doReportActivity(api, "m-1", `{"state":"active","turn_id":"t1","seq":1}`))
	activityReceipt(t, doReportActivity(api, "m-1", `{"state":"idle","turn_id":"t1","seq":2}`))
	entry := api.activity.Get("m-1")
	if state, _ := entry[activityKeyState].(string); state != "" {
		t.Fatalf("the claim must be closed, got state %q", state)
	}
	if end, _ := entry[activityKeyLastEnd].(float64); end <= 0 {
		t.Fatal("closing a claim is the ONE place last_end is stamped")
	}
}

// A bare idle (no claim to close) is a real report but NOT an observed
// completion — stamping last_end there would put "last ended just now" on a
// session that never ran a turn.
func TestReportActivity_BareIdleObservesNoCompletion(t *testing.T) {
	api := activityTestServer()
	activityReceipt(t, doReportActivity(api, "m-1", `{"state":"idle"}`))
	entry := api.activity.Get("m-1")
	if end, ok := entry[activityKeyLastEnd]; ok {
		t.Fatalf("a bare idle must not fabricate a completion time, got %v", end)
	}
	state, _, lastEnd := activityOf(entry, true, nowSecs())
	if state != ActivityIdle {
		t.Fatalf("a reported idle is known-idle, got %q", state)
	}
	if lastEnd != nil {
		t.Fatalf("no anchor may be served, got %v", *lastEnd)
	}
}

// R1: out-of-order protection, scoped to one reporter session.
//
// ⚠️ The turn ids are DELIBERATELY THE SAME on both reports. An earlier draft
// used different ones, and R3 (the turn-pairing rule) then dropped the report
// too — so disabling R1 outright left this test green (measured). Two rules
// covering for each other is indistinguishable from one rule working. With a
// matching turn id, R1 is the ONLY thing that can refuse this report.
func TestReportActivity_StaleSeqIsDropped(t *testing.T) {
	api := activityTestServer()
	activityReceipt(t, doReportActivity(api, "m-1", `{"state":"active","turn_id":"t1","session_id":"s","seq":5}`))
	got := activityReceipt(t, doReportActivity(api, "m-1",
		`{"state":"idle","turn_id":"t1","session_id":"s","seq":4}`))
	if got.Accepted {
		t.Fatal("a report older than the stored seq must be dropped")
	}
	entry := api.activity.Get("m-1")
	if state, _ := entry[activityKeyState].(string); state != ActivityActive {
		t.Fatalf("the live claim must survive a stale report, got %q", state)
	}
	if _, stamped := entry[activityKeyLastEnd]; stamped {
		t.Fatal("a dropped report must not stamp a completion")
	}
	// The in-order twin MUST land, or "drops everything" would pass as well.
	if !activityReceipt(t, doReportActivity(api, "m-1",
		`{"state":"idle","turn_id":"t1","session_id":"s","seq":6}`)).Accepted {
		t.Fatal("control: an in-order report must still be accepted")
	}
}

// R2: a new reporter session cannot inherit the previous session's turn.
func TestReportActivity_NewSessionDiscardsTheOldClaim(t *testing.T) {
	api := activityTestServer()
	activityReceipt(t, doReportActivity(api, "m-1", `{"state":"active","turn_id":"t1","session_id":"s1","seq":9}`))
	// A LOWER seq under a new session id must NOT be treated as out-of-order.
	got := activityReceipt(t, doReportActivity(api, "m-1", `{"state":"idle","session_id":"s2","seq":1}`))
	if !got.Accepted {
		t.Fatal("a report from a NEW session must be accepted whatever its seq")
	}
	entry := api.activity.Get("m-1")
	if state, _ := entry[activityKeyState].(string); state != "" {
		t.Fatalf("the old session's claim must be gone, got %q", state)
	}
	if _, stamped := entry[activityKeyLastEnd]; stamped {
		t.Fatal("a new session's idle did not OBSERVE the old turn end — no stamp")
	}
}

// R3: a late idle from the PREVIOUS turn must not kill the current one.
func TestReportActivity_LateIdleFromAnotherTurnIsIgnored(t *testing.T) {
	api := activityTestServer()
	activityReceipt(t, doReportActivity(api, "m-1", `{"state":"active","turn_id":"t2"}`))
	got := activityReceipt(t, doReportActivity(api, "m-1", `{"state":"idle","turn_id":"t1"}`))
	if got.Accepted {
		t.Fatal("an idle naming a different turn must be dropped")
	}
	entry := api.activity.Get("m-1")
	if state, _ := entry[activityKeyState].(string); state != ActivityActive {
		t.Fatalf("turn t2 must still be claimed, got %q", state)
	}
}

// R4: a restated turn must not re-anchor when the turn started — otherwise a
// repeated report makes a long turn read as if it had just begun.
func TestReportActivity_RestatedTurnKeepsTheOriginalAnchor(t *testing.T) {
	api := activityTestServer()
	activityReceipt(t, doReportActivity(api, "m-1", `{"state":"active","turn_id":"t1"}`))
	first, _ := api.activity.Get("m-1")[activityKeySince].(float64)
	// Rewind the anchor so a re-anchor would be unmistakable.
	entry := api.activity.Get("m-1")
	entry[activityKeySince] = first - 600
	api.activity.Set("m-1", entry)

	activityReceipt(t, doReportActivity(api, "m-1", `{"state":"active","turn_id":"t1"}`))
	if got, _ := api.activity.Get("m-1")[activityKeySince].(float64); got != first-600 {
		t.Fatalf("restating the SAME turn must not move working_since (%v → %v)",
			first-600, got)
	}
}

// ── the two SSE lifecycle edges ──────────────────────────────────────────────

func TestActivityOnDisconnect_KeepsTheClaimAndInventsNoEnd(t *testing.T) {
	api := activityTestServer()
	activityReceipt(t, doReportActivity(api, "m-1", `{"state":"active","turn_id":"t1"}`))
	api.activityOnDisconnect("m-1")

	entry := api.activity.Get("m-1")
	if state, _ := entry[activityKeyState].(string); state != ActivityActive {
		t.Fatalf("a drop must not destroy the claim (a blip would lose the whole turn), got %q", state)
	}
	if _, stamped := entry[activityKeyLastEnd]; stamped {
		t.Fatal("a vanished session is not a finished one — last_end must stay unstamped")
	}
	if off, _ := entry[activityKeyOfflineSince].(float64); off <= 0 {
		t.Fatal("the drop time must be recorded for the reconnect decision")
	}
	// And it still cannot DISPLAY as working while offline.
	if state, _, _ := activityOf(entry, false, nowSecs()); state != ActivityIdle {
		t.Fatalf("an offline actor must read idle, got %q", state)
	}
}

// The disconnect edge fires for every member. It must not manufacture an entry
// for one that never reported, or `never` would stop meaning "never".
func TestActivityOnDisconnect_NeverCreatesAnEntry(t *testing.T) {
	api := activityTestServer()
	api.activityOnDisconnect("m-silent")
	if api.activity.Get("m-silent") != nil {
		t.Fatal("a disconnect must not invent an activity record")
	}
}

func TestActivityOnConnect_BlipKeepsTheTurnRealAbsenceDropsIt(t *testing.T) {
	now := nowSecs()
	t.Run("reconnect inside the grace resumes the same turn", func(t *testing.T) {
		api := activityTestServer()
		api.activity.Set("m-1", map[string]any{
			activityKeyState: ActivityActive, activityKeySince: now - 30,
			activityKeyTurnID:       "t1",
			activityKeyOfflineSince: now - 5,
		})
		api.activityOnConnect("m-1")
		entry := api.activity.Get("m-1")
		if state, _ := entry[activityKeyState].(string); state != ActivityActive {
			t.Fatalf("a 5s blip must not end the turn, got %q", state)
		}
		if _, ok := entry[activityKeyOfflineSince]; ok {
			t.Fatal("the drop stamp must be cleared once we are back")
		}
	})
	t.Run("reconnect after the grace drops the claim without inventing an end", func(t *testing.T) {
		api := activityTestServer()
		api.activity.Set("m-1", map[string]any{
			activityKeyState: ActivityActive, activityKeySince: now - 600,
			activityKeyTurnID:       "t1",
			activityKeyOfflineSince: now - defaultReconcileConfig().ZombieConfirmGrace - 1,
		})
		api.activityOnConnect("m-1")
		entry := api.activity.Get("m-1")
		if state, _ := entry[activityKeyState].(string); state != "" {
			t.Fatalf("a real absence must drop the claim, got %q", state)
		}
		if _, stamped := entry[activityKeyLastEnd]; stamped {
			t.Fatal("we never saw that turn end — no completion time may be written")
		}
	})
}

// THE BOOT TURN. An agent reports `active` from its UserPromptSubmit hook and
// only THEN connects its SSE stream — seeds/boot_sequence.md step 3 makes that
// ordering mandatory ("三步順序不可換，掛 SSE 永遠壓最後", so that a half-ready
// agent is never projected online). The first-ever connect therefore lands
// MID-TURN, with no drop ever observed and `offline_since` never stamped.
//
// The reconnect judgement is scoped to the RECONNECT edge by design
// (docs/design/activity-model.md: 「重連時才決定」). Applying it to the 0→1 edge
// discards a claim that is demonstrably live, and every spawn / recycle /
// refocus in the fleet goes through this path — the row would read 閒置 while
// the agent is provably working, which is the exact lie this feature exists to
// remove.
func TestActivityOnConnect_FirstEverConnectKeepsTheBootTurnClaim(t *testing.T) {
	api := activityTestServer()
	activityReceipt(t, doReportActivity(api, "m-boot", `{"state":"active","turn_id":"t1"}`))

	// No disconnect ever happened: this is the 0→1 edge, not a reconnect.
	api.activityOnConnect("m-boot")

	entry := api.activity.Get("m-boot")
	if state, _ := entry[activityKeyState].(string); state != ActivityActive {
		t.Fatalf("the first-ever connect must not discard a live boot-turn claim, got %q", state)
	}
	if state, since, _ := activityOf(entry, true, nowSecs()); state != ActivityActive || since == nil {
		t.Fatalf("the boot turn must read active with its anchor, got %q since=%v", state, since)
	}
}

// The second half of the same defect: with the claim destroyed, the boot turn's
// `Stop` finds nothing to close, so applyActivityReport never stamps an end.
// The cell then shows the bare word with NO 「上次結束 X 前」 until the agent's
// SECOND turn completes — a freshly recycled agent looks like it has never run.
func TestActivityBootTurn_EndIsObservedAfterTheFirstConnect(t *testing.T) {
	api := activityTestServer()
	activityReceipt(t, doReportActivity(api, "m-boot", `{"state":"active","turn_id":"t1"}`))
	api.activityOnConnect("m-boot")
	activityReceipt(t, doReportActivity(api, "m-boot", `{"state":"idle","turn_id":"t1"}`))

	entry := api.activity.Get("m-boot")
	if _, stamped := entry[activityKeyLastEnd]; !stamped {
		t.Fatal("the boot turn ended and we observed it — last_turn_completed_at must be stamped")
	}
	if state, _, end := activityOf(entry, true, nowSecs()); state != ActivityIdle || end == nil {
		t.Fatalf("after a normal boot turn the row must read idle WITH an end stamp, got %q end=%v", state, end)
	}
}

// The grace is the EXISTING ZombieConfirmGrace, not a second number invented
// for this feature. If someone forks a new constant, this fails.
func TestActivityGraceIsTheExistingZombieConfirmGrace(t *testing.T) {
	api := &apiServer{reconcileCfg: defaultReconcileConfig()}
	if got := api.activityGraceSecs(); got != defaultReconcileConfig().ZombieConfirmGrace {
		t.Fatalf("activity grace = %v, want the reconcile ZombieConfirmGrace %v",
			got, defaultReconcileConfig().ZombieConfirmGrace)
	}
	// …and it is NOT the max-turn threshold: two different quantities.
	if api.activityGraceSecs() == activityMaxTurnSecs {
		t.Fatal("the reconnect grace and the max-turn window must stay distinct")
	}
}

func TestActivityForget_DropsTheRecord(t *testing.T) {
	api := activityTestServer()
	activityReceipt(t, doReportActivity(api, "m-1", `{"state":"active"}`))
	api.activityForget("m-1")
	if api.activity.Get("m-1") != nil {
		t.Fatal("a dismissed member must leave no claim behind")
	}
}

// ── the read path (GET /api/monitoring sessions) ─────────────────────────────

// The store is only half the feature; if the fold does not carry it, the owner
// sees nothing. This drives the REAL handler end to end, with a REAL live SSE
// listener supplying the online fact the gate reads.
func TestGetMonitoring_SessionCarriesTheActivityVerdict(t *testing.T) {
	api := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore(), activity: newMemStore()}
	m := fullMember("mira")
	m.RoleKey = "builder"
	if err := api.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	sessionOf := func() map[string]any {
		t.Helper()
		body := monitoringOf(t, doGetMonitoring(api, map[string]any{
			"sub": wireOwnerID, "scope": "owner"}))
		rows, _ := body["sessions"].([]any)
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			if id, _ := row["id"].(string); id == "mira" {
				return row
			}
		}
		t.Fatal("no session row for mira")
		return nil
	}

	// 1. Nothing reported → the honest dash, with no fabricated stamps.
	row := sessionOf()
	if got := row["activity_state"]; got != ActivityNever {
		t.Fatalf("an unreported member must read never, got %v", got)
	}
	if row["working_since"] != nil || row["last_turn_completed_at"] != nil {
		t.Fatalf("no stamps may be served for an unreported member: %v / %v",
			row["working_since"], row["last_turn_completed_at"])
	}

	// 2. Reported active but OFFLINE → the gate refuses to show it as working.
	activityReceipt(t, doReportActivity(api, "mira", `{"state":"active","turn_id":"t1"}`))
	if got := sessionOf()["activity_state"]; got != ActivityIdle {
		t.Fatalf("an offline member must never read as working, got %v", got)
	}

	// 3. Same claim, now holding a live stream → active, with its anchor.
	listener, err := api.hub.Connect("mira", "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer api.hub.Disconnect(listener)
	row = sessionOf()
	if got := row["activity_state"]; got != ActivityActive {
		t.Fatalf("an online member mid-turn must read active, got %v", got)
	}
	since, ok := row["working_since"].(float64)
	if !ok || since <= 0 {
		t.Fatalf("active must carry its working_since anchor, got %v", row["working_since"])
	}

	// 4. Turn ends → idle with an observed completion, and the anchor is gone.
	activityReceipt(t, doReportActivity(api, "mira", `{"state":"idle","turn_id":"t1"}`))
	row = sessionOf()
	if got := row["activity_state"]; got != ActivityIdle {
		t.Fatalf("after the end report the session must read idle, got %v", got)
	}
	if row["working_since"] != nil {
		t.Fatalf("idle must carry no working_since, got %v", row["working_since"])
	}
	if end, ok := row["last_turn_completed_at"].(float64); !ok || end <= 0 {
		t.Fatalf("idle must serve the observed end, got %v", row["last_turn_completed_at"])
	}
}

// ── tiny helpers ─────────────────────────────────────────────────────────────

func f(v float64) *float64 { return &v }

func ptrEq(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func show(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
