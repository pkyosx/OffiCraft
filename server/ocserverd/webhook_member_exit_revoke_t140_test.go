package main

// webhook_member_exit_revoke_t140_test.go — T-140's four pins.
//
// The ticket has two halves and they fail in DIFFERENT ways, so they are
// pinned separately here:
//
//   - the KIND half (a contractor may hold an inlet at all) fails LOUDLY —
//     a 404 where a 200 was wanted;
//   - the REVOCATION half fails SILENTLY — the endpoint row simply outlives
//     its member and nothing anywhere says so. That half is a database
//     TRIGGER (migrations/00094), which is invisible from the Go side, so one
//     of the tests below writes the member row in RAW SQL and never touches a
//     handler: if it passes, no Go code was involved.
//
// 🔴 "IT CANNOT GET IN" AND "THE ROW IS GONE" ARE TWO DIFFERENT READINGS AND
// THIS FILE REFUSES TO CONFLATE THEM. The inlet has TWO independent reasons to
// drop a call — an unknown token (no endpoint row) and a member that no longer
// resolves (member_gone) — and an assertion that only says "no chat arrived"
// cannot tell them apart, nor tell either from an inlet that never worked in
// the first place. Every exit test below therefore
//   ① delivers ONCE BEFORE the exit (so silence afterwards is a change, not a
//      standing condition),
//   ② asserts the ROW is absent by reading it back, and
//   ③ re-seeds a FRESH endpoint row on the departed member and shows the inlet
//      still refuses it with member_gone — which is the proof that ② and the
//      silence in ① are two mechanisms and not one fact read twice.

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// t140SeedStaff puts one live staff member and returns its id.
func t140SeedStaff(t *testing.T, api *apiServer, id string) string {
	t.Helper()
	m := fullMember(id)
	if err := api.dal.PutMember(m); err != nil {
		t.Fatalf("seed staff %s: %v", id, err)
	}
	return id
}

// t140CreateEndpoint creates one endpoint THROUGH THE HANDLER (the create door
// is itself under test) and returns the minted token.
func t140CreateEndpoint(t *testing.T, api *apiServer, memberID, endpointID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateWebhookApiMembersMemberIdWebhooksPost(rec,
		taskReq(t, "POST", "/api/members/"+memberID+"/webhooks",
			map[string]any{"endpoint_id": endpointID}, wireOwnerID, "owner"), memberID)
	if rec.Code != 200 {
		t.Fatalf("create webhook on %s: want 200, got %d %s", memberID, rec.Code, rec.Body.String())
	}
	token := decodeBody[webhookEndpointDTO](t, rec).Token
	if token == "" {
		t.Fatalf("create webhook on %s returned an empty token: %s", memberID, rec.Body.String())
	}
	return token
}

// t140PostIn drives the PUBLIC inlet with one token. It answers the status code
// only — every /in outcome is the same silent 200 by contract, so the caller
// has to look BEHIND the response for what happened.
func t140PostIn(t *testing.T, api *apiServer, token, body string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/in?t="+token, strings.NewReader(body))
	api.HandleReceiveWebhookInPost(rec, req, HandleReceiveWebhookInPostParams{T: &token})
	return rec.Code
}

// t140ChatCount is how many messages the member has received — the inlet's ONLY
// visible effect.
func t140ChatCount(t *testing.T, api *apiServer, memberID string) int {
	t.Helper()
	msgs, err := api.dal.ListChatInvolving(memberID, 50)
	if err != nil {
		t.Fatalf("list chat for %s: %v", memberID, err)
	}
	return len(msgs)
}

// t140EndpointGone reads the row back by token: nil is the revocation.
func t140EndpointGone(t *testing.T, api *apiServer, token string) bool {
	t.Helper()
	e, err := api.dal.GetWebhookByToken(token)
	if err != nil {
		t.Fatalf("read endpoint back: %v", err)
	}
	return e == nil
}

func t140LogCount(t *testing.T, api *apiServer, token string) int {
	t.Helper()
	rows, err := api.dal.ListWebhookRequestLogs(token)
	if err != nil {
		t.Fatalf("list request logs: %v", err)
	}
	return len(rows)
}

// t140AssertExitRevoked is the shared body of the two exit tests: the member has
// already left, and this asserts BOTH readings plus the discriminator.
//
// reseed installs a fresh endpoint row on the departed member DIRECTLY through
// the DAL (no handler, so the create door's own refusal is not what is being
// measured) — the inlet must still refuse it, and that refusal is member_gone,
// a different mechanism from the missing row above.
func t140AssertExitRevoked(t *testing.T, api *apiServer, memberID, token string, chatBefore int) {
	t.Helper()
	// READING ONE — the row is gone. This is the half the old behaviour failed:
	// 00007 kept the row and merely dropped the delivery.
	if !t140EndpointGone(t, api, token) {
		t.Errorf("%s left the roster and its endpoint row survived", memberID)
	}
	if n := t140LogCount(t, api, token); n != 0 {
		t.Errorf("%s left the roster and %d request-log row(s) survived", memberID, n)
	}
	// READING TWO — the inlet does not deliver. Distinct from reading one: this
	// is about the token, not about the row.
	if code := t140PostIn(t, api, token, "after the exit"); code != 200 {
		t.Errorf("/in must stay silently 200 after the exit, got %d", code)
	}
	if got := t140ChatCount(t, api, memberID); got != chatBefore {
		t.Errorf("/in delivered %d message(s) after the exit, want the %d from before",
			got-chatBefore, chatBefore)
	}
	// THE DISCRIMINATOR — a FRESH row on the departed member. Reading one cannot
	// explain this one: the row exists. It is refused because the MEMBER is
	// gone, which is the second, independent guard.
	reseeded := newWebhookToken()
	if err := api.dal.PutWebhookEndpoint(WebhookEndpoint{
		Token: reseeded, MemberID: memberID, EndpointID: "reseeded",
		Status: WebhookStatusEnabled, CreatedTS: nowSecs(), Platform: WebhookPlatformGeneric,
	}); err != nil {
		t.Fatalf("reseed endpoint on the departed member: %v", err)
	}
	if code := t140PostIn(t, api, reseeded, "to a fresh row on a gone member"); code != 200 {
		t.Errorf("/in on the reseeded row must stay silently 200, got %d", code)
	}
	if got := t140ChatCount(t, api, memberID); got != chatBefore {
		t.Errorf("the reseeded row delivered to a departed member (%d messages, want %d)",
			got, chatBefore)
	}
	e, err := api.dal.GetWebhookByToken(reseeded)
	if err != nil || e == nil {
		t.Fatalf("read the reseeded row back: %v %+v", err, e)
	}
	if e.LastDropReason != WebhookDropReasonMemberGone {
		t.Errorf("the reseeded row's drop reason = %q, want %q — without this the "+
			"silence above could be any of the inlet's other refusals",
			e.LastDropReason, WebhookDropReasonMemberGone)
	}
}

// TestCreateWebhook_AContractorGetsAnInletThatDelivers is 必做四格 ①, and it is
// also the POSITIVE CONTROL every other test in this file leans on: if the
// contractor inlet never worked, "no message arrived after the release" would
// be true for the wrong reason.
func TestCreateWebhook_AContractorGetsAnInletThatDelivers(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	token := t140CreateEndpoint(t, api, workerID, "pr-events")

	if code := t140PostIn(t, api, token, "PR #42 merged"); code != 200 {
		t.Fatalf("/in on a contractor endpoint: want silent 200, got %d", code)
	}
	if got := t140ChatCount(t, api, workerID); got != 1 {
		t.Fatalf("/in must synthesise exactly ONE chat to the contractor, got %d", got)
	}
	e, err := api.dal.GetWebhookByToken(token)
	if err != nil || e == nil {
		t.Fatalf("read endpoint back: %v %+v", err, e)
	}
	// The counters are read as well as the chat: a 200 with no chat and no
	// delivered stamp is exactly what the pre-T-140 inlet answered.
	if e.DeliveredCount != 1 || e.LastDropReason != "" {
		t.Fatalf("endpoint must record ONE clean delivery, got delivered=%d drop=%q",
			e.DeliveredCount, e.LastDropReason)
	}
}

// TestReleaseWorkerByID_RevokesTheContractorsEndpoint is 必做四格 ②. The release
// is driven through the REAL retirement path (the DAL call every worker exit
// funnels into), not by hand-writing roster_status.
func TestReleaseWorkerByID_RevokesTheContractorsEndpoint(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	token := t140CreateEndpoint(t, api, workerID, "pr-events")

	if code := t140PostIn(t, api, token, "before the release"); code != 200 {
		t.Fatalf("/in before the release: want 200, got %d", code)
	}
	if got := t140ChatCount(t, api, workerID); got != 1 {
		t.Fatalf("the inlet must be PROVEN working before the release; delivered %d", got)
	}
	if t140LogCount(t, api, token) == 0 {
		t.Fatal("no request-log row before the release — the log assertion after it would be vacuous")
	}

	if w, err := api.dal.ReleaseWorkerByID(workerID, nowSecs()); err != nil || w == nil {
		t.Fatalf("release worker: %v %+v", err, w)
	}
	t140AssertExitRevoked(t, api, workerID, token, 1)
}

// TestDismissMember_RevokesTheStaffMembersEndpoint is 必做四格 ③ — the same two
// readings, through the staff door (the handler, so the whole dismiss path runs).
func TestDismissMember_RevokesTheStaffMembersEndpoint(t *testing.T) {
	api := newTasksTestServer(t)
	memberID := t140SeedStaff(t, api, "m-leaver")
	token := t140CreateEndpoint(t, api, memberID, "pr-events")

	if code := t140PostIn(t, api, token, "before the dismissal"); code != 200 {
		t.Fatalf("/in before the dismissal: want 200, got %d", code)
	}
	if got := t140ChatCount(t, api, memberID); got != 1 {
		t.Fatalf("the inlet must be PROVEN working before the dismissal; delivered %d", got)
	}
	if t140LogCount(t, api, token) == 0 {
		t.Fatal("no request-log row before the dismissal — the log assertion after it would be vacuous")
	}

	rec := httptest.NewRecorder()
	api.HandleDismissMemberApiMembersMemberIdDelete(rec,
		taskReq(t, "DELETE", "/api/members/"+memberID, nil, wireOwnerID, "owner"), memberID)
	if rec.Code != 200 {
		t.Fatalf("dismiss: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	t140AssertExitRevoked(t, api, memberID, token, 1)
}

// TestMemberExitRevoke_LeavesALiveMembersEndpointAlone is 必做四格 ④ — 正常情況
// 不觸發, and the ONLY test here that can catch a revocation written too wide.
// Both kinds are present because the trigger asks about roster_status and not
// about kind: a rule that revoked on any member write at all would still pass
// every test above.
func TestMemberExitRevoke_LeavesALiveMembersEndpointAlone(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	liveWorker := assignOneWorker(t, api)
	liveStaff := t140SeedStaff(t, api, "m-stays")
	leaver := t140SeedStaff(t, api, "m-goes")

	workerToken := t140CreateEndpoint(t, api, liveWorker, "worker-hook")
	staffToken := t140CreateEndpoint(t, api, liveStaff, "staff-hook")
	leaverToken := t140CreateEndpoint(t, api, leaver, "leaver-hook")
	for _, tok := range []string{workerToken, staffToken, leaverToken} {
		if code := t140PostIn(t, api, tok, "seed a log row"); code != 200 {
			t.Fatalf("/in seeding: got %d", code)
		}
	}

	// Ordinary writes on the two survivors, of the shapes the roster does all
	// day: a whole-row put, and a wind-down stamp. Neither is an exit.
	staff, err := api.dal.GetMember(liveStaff)
	if err != nil || staff == nil {
		t.Fatalf("read live staff: %v", err)
	}
	staff.DesiredState = DesiredStateOffline
	if err := api.dal.PutMember(*staff); err != nil {
		t.Fatalf("put live staff: %v", err)
	}
	worker, err := api.dal.GetMember(liveWorker)
	if err != nil || worker == nil {
		t.Fatalf("read live worker: %v", err)
	}
	worker.StoppingSince = nowSecs()
	if err := api.dal.PutMember(*worker); err != nil {
		t.Fatalf("put live worker: %v", err)
	}

	// And a REAL exit next to them, so a run where nothing was revoked at all
	// cannot pass this test by doing nothing.
	rec := httptest.NewRecorder()
	api.HandleDismissMemberApiMembersMemberIdDelete(rec,
		taskReq(t, "DELETE", "/api/members/"+leaver, nil, wireOwnerID, "owner"), leaver)
	if rec.Code != 200 {
		t.Fatalf("dismiss the leaver: %d %s", rec.Code, rec.Body.String())
	}
	if !t140EndpointGone(t, api, leaverToken) {
		t.Fatal("the leaver's endpoint survived — this test's own control failed")
	}

	for _, c := range []struct{ who, token string }{
		{liveWorker, workerToken}, {liveStaff, staffToken},
	} {
		if t140EndpointGone(t, api, c.token) {
			t.Errorf("%s is still on the roster and its endpoint was deleted", c.who)
			continue
		}
		if n := t140LogCount(t, api, c.token); n != 1 {
			t.Errorf("%s: request-log rows = %d, want the 1 seeded", c.who, n)
		}
		if code := t140PostIn(t, api, c.token, "still working"); code != 200 {
			t.Errorf("%s: /in got %d", c.who, code)
		}
		if got := t140ChatCount(t, api, c.who); got != 2 {
			t.Errorf("%s: chat count = %d, want 2 (the seed + this one)", c.who, got)
		}
	}
}

// TestMemberExitRevoke_NeedsNoGoCode is the one test that can see WHERE the
// revocation lives. Everything else in this file goes through a handler or the
// DAL, so all of it would still pass if the rule were Go code in one of them —
// and Go code in a handler is exactly the shape this ticket refused, because a
// member leaves through twelve measured doors and a hand-maintained list of
// them is one new door away from being wrong.
//
// So this one writes SQL. No handler, no DAL method, no apiServer: rows in,
// UPDATE / DELETE, rows read back. If it is green, the revocation is in the
// database and a thirteenth door inherits it by construction.
func TestMemberExitRevoke_NeedsNoGoCode(t *testing.T) {
	api := newTasksTestServer(t)
	db := api.dal.wdb

	seed := func(memberID, kind string) string {
		t.Helper()
		if _, err := db.Exec(
			`INSERT INTO member (id, name, kind, roster_status) VALUES (?, ?, ?, 'active')`,
			memberID, memberID, kind); err != nil {
			t.Fatalf("insert member %s: %v", memberID, err)
		}
		token := "raw-" + memberID
		if _, err := db.Exec(
			`INSERT INTO webhook_endpoint (token, member_id, endpoint_id) VALUES (?, ?, 'raw')`,
			token, memberID); err != nil {
			t.Fatalf("insert endpoint for %s: %v", memberID, err)
		}
		if _, err := db.Exec(
			`INSERT INTO webhook_request_log (token, ts, outcome) VALUES (?, 1.0, 'delivered')`,
			token); err != nil {
			t.Fatalf("insert log for %s: %v", memberID, err)
		}
		return token
	}
	count := func(query, arg string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(query, arg).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	endpoints := func(memberID string) int {
		return count(`SELECT count(*) FROM webhook_endpoint WHERE member_id = ?`, memberID)
	}
	logs := func(token string) int {
		return count(`SELECT count(*) FROM webhook_request_log WHERE token = ?`, token)
	}

	softStaff := seed("m-raw-staff", KindStaff)
	softWorker := seed("ow-raw-worker", KindOutsource)
	hard := seed("m-raw-hard", KindStaff)
	live := seed("m-raw-live", KindStaff)
	for _, c := range []struct{ who, token string }{
		{"m-raw-staff", softStaff}, {"ow-raw-worker", softWorker},
		{"m-raw-hard", hard}, {"m-raw-live", live},
	} {
		if endpoints(c.who) != 1 || logs(c.token) != 1 {
			t.Fatalf("%s did not seed (endpoints=%d logs=%d) — the deletions below would be vacuous",
				c.who, endpoints(c.who), logs(c.token))
		}
	}

	// The three exit shapes, written EXACTLY as the production writers write
	// them: the staff soft delete (api_members dismiss), the worker release
	// (dal_tasks ReleaseWorker*), and the role-cascade hard delete (dal
	// HardDeleteMember).
	if _, err := db.Exec(
		`UPDATE member SET roster_status = 'removed', desired_state = 'offline' WHERE id = 'm-raw-staff'`); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE member SET roster_status = ?, released_ts = ? WHERE id = ? AND kind = 'outsource'`,
		RosterStatusRemoved, nowSecs(), "ow-raw-worker"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM member WHERE id = 'm-raw-hard'`); err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	// An ordinary UPDATE on the survivor, in the same batch: a trigger written
	// without its WHEN would take this one too.
	if _, err := db.Exec(`UPDATE member SET name = 'renamed' WHERE id = 'm-raw-live'`); err != nil {
		t.Fatalf("rename: %v", err)
	}

	for _, c := range []struct{ who, token string }{
		{"m-raw-staff", softStaff}, {"ow-raw-worker", softWorker}, {"m-raw-hard", hard},
	} {
		if n := endpoints(c.who); n != 0 {
			t.Errorf("%s: %d endpoint row(s) survived a pure-SQL exit", c.who, n)
		}
		if n := logs(c.token); n != 0 {
			t.Errorf("%s: %d request-log row(s) survived a pure-SQL exit", c.who, n)
		}
	}
	if n := endpoints("m-raw-live"); n != 1 {
		t.Errorf("the untouched member lost its endpoint (%d rows left)", n)
	}
	if n := logs(live); n != 1 {
		t.Errorf("the untouched member lost its request logs (%d rows left)", n)
	}
}

// TestWebhookRequestLogsNeverOutliveTheirEndpoint pins the ledger the revoke
// verb and the exit trigger now BOTH owe.
//
// 🔴 IT EXISTS BECAUSE ONE OF THE TWO CLEANERS IS ABOUT TO LOOK REDUNDANT.
// DeleteWebhookEndpoint deletes the log rows by hand and 00094's
// webhook_endpoint_bd_logs trigger deletes them again underneath it; T-140
// deliberately left the hand-written half in place rather than converge them in
// the same step. This test is what tells whoever removes it that the trigger
// catches the fall — and, read the other way, what fails if the trigger is
// dropped and some future deleter is not the revoke verb.
func TestWebhookRequestLogsNeverOutliveTheirEndpoint(t *testing.T) {
	api := newTasksTestServer(t)
	memberID := t140SeedStaff(t, api, "m-logs")

	viaVerb := t140CreateEndpoint(t, api, memberID, "via-verb")
	viaTrigger := t140CreateEndpoint(t, api, memberID, "via-trigger")
	for _, tok := range []string{viaVerb, viaTrigger} {
		if code := t140PostIn(t, api, tok, "seed a log row"); code != 200 {
			t.Fatalf("/in seeding: got %d", code)
		}
		if t140LogCount(t, api, tok) != 1 {
			t.Fatalf("seeding a log row for %s failed — both halves below would be vacuous", tok)
		}
	}

	// Path one: the revoke verb (DeleteWebhookEndpoint, which sweeps by hand).
	rec := httptest.NewRecorder()
	api.HandleDeleteWebhookApiMembersMemberIdWebhooksEndpointIdDelete(rec,
		taskReq(t, "DELETE", "/api/members/"+memberID+"/webhooks/via-verb", nil, wireOwnerID, "owner"),
		memberID, "via-verb")
	if rec.Code != 200 {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if n := t140LogCount(t, api, viaVerb); n != 0 {
		t.Errorf("revoke verb left %d request-log row(s) behind", n)
	}

	// Path two: a bare DELETE of the endpoint row, which is what the member-exit
	// triggers do and what any future deleter would do.
	if _, err := api.dal.wdb.Exec(
		`DELETE FROM webhook_endpoint WHERE token = ?`, viaTrigger); err != nil {
		t.Fatalf("bare endpoint delete: %v", err)
	}
	if n := t140LogCount(t, api, viaTrigger); n != 0 {
		t.Errorf("a bare endpoint DELETE left %d request-log row(s) behind", n)
	}
}

// TestReceiveWebhookIn_RefusesAWardenEndpoint is the other side of 「不要順手放
// 寬到不該放的東西」: widening the inlet to contractors must not widen it to
// wardens. The inlet's whole effect is a chat, and a warden is not a chat
// recipient (resolveChatRecipient refuses it by name) — a delivery here would
// be a message nobody ever reads.
//
// 🔴 The endpoint row is seeded through the DAL, not the create verb: create
// asks anyMember and DOES admit a warden today (unchanged by T-140), so
// building the row through the door would measure the door, not the inlet.
func TestReceiveWebhookIn_RefusesAWardenEndpoint(t *testing.T) {
	api := newTasksTestServer(t)
	warden := fullMember("m-warden")
	warden.Kind = KindWarden
	if err := api.dal.PutMember(warden); err != nil {
		t.Fatalf("seed warden: %v", err)
	}
	token := newWebhookToken()
	if err := api.dal.PutWebhookEndpoint(WebhookEndpoint{
		Token: token, MemberID: warden.ID, EndpointID: "warden-hook",
		Status: WebhookStatusEnabled, CreatedTS: nowSecs(), Platform: WebhookPlatformGeneric,
	}); err != nil {
		t.Fatalf("seed warden endpoint: %v", err)
	}

	if code := t140PostIn(t, api, token, "to a machine"); code != 200 {
		t.Fatalf("/in must stay silently 200 for a warden, got %d", code)
	}
	if got := t140ChatCount(t, api, warden.ID); got != 0 {
		t.Fatalf("/in delivered %d message(s) to a WARDEN — the inlet is wider than the chat door", got)
	}
	e, err := api.dal.GetWebhookByToken(token)
	if err != nil || e == nil {
		t.Fatalf("read warden endpoint back: %v %+v", err, e)
	}
	if e.DeliveredCount != 0 || e.LastDropReason != WebhookDropReasonMemberGone {
		t.Fatalf("warden inlet must record a member_gone drop, got delivered=%d drop=%q",
			e.DeliveredCount, e.LastDropReason)
	}
}
