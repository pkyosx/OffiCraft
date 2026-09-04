package main

// api_auth_machine_revoke_test.go — the sentinels for T-9cf8 (a deleted
// machine's credentials stop working).
//
// This is a DENY gate, so the tests are deliberately shaped as a matched pair
// and BOTH have to earn their keep:
//
//	① the deleted machine really is refused — measured on the wire, through the
//	   whole mux (requireAuth → RBAC choke → handler), on the request shapes the
//	   live warden/agent actually send;
//	② nothing else is. This one is the load-bearing half. Its discriminating
//	   power is proved by a mutant that WIDENS the check (drop the kind==warden
//	   restriction) — ② goes red because a RELEASED outsource worker carries the
//	   very same RosterStatusRemoved, and the close-out contract keeps that
//	   session working on purpose.
//
// Everything runs against a temp sqlite + httptest server. Nothing here
// touches a real machine, a real warden, or a real agent.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// revokeStack assembles the FULL wired stack and hands back the apiServer too,
// so a test can plant roster rows directly and then talk HTTP.
func revokeStack(t *testing.T) (*httptest.Server, []byte, *apiServer) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "revoke-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	secret := []byte(interopSecret)
	api := newAPIServer(dal, NewHub(), singleKeyring(secret), 3600, "../..")
	h, err := buildHandler(specsFor(api), api.keys, dal.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, secret, api
}

func revokeCall(t *testing.T, method, url, token, body string) (int, string) {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 2048)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp.StatusCode, sb.String()
}

// liveCall names one REAL request shape a live process sends, so the guards
// below assert on traffic that exists rather than on invented endpoints.
type liveCall struct {
	who    string
	method string
	path   string
	body   string
}

// wardenHeartbeat is byte-for-byte the shape cli/ocwarden buildTelemetryPayload
// produces on its 30s cadence (machine/hardware/binaries; NO agent_id — the
// reporting identity is the verified JWT sub).
const wardenHeartbeat = `{"machine":"box-1","hardware":{"cpu_pct":12.5,"ram_pct":48.0,` +
	`"battery_pct":91,"ac_power":true},"binaries":{"ocwarden":"a1b2c3d4e5f6",` +
	`"ocagent":"0f1e2d3c4b5a"},"runtimes":{"claude":{"installed":true,"logged_in":true}}}`

func revokeMachine(t *testing.T, api *apiServer, machineID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/machines/"+machineID, nil)
	api.HandleDeleteMachineApiMachinesMemberIdDelete(rec, req, machineID)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/machines/%s: want 200, got %d %s",
			machineID, rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ① the deleted machine is refused — and was NOT refused before the delete
// ---------------------------------------------------------------------------

// TestDeletedMachineCredentialsAreRefused is sentinel ①. The positive control
// is inside the test on purpose: every assertion is a BEFORE/AFTER pair on the
// same token and the same request, so a version of this test that always
// refused (or a probe that never worked at all) cannot pass.
//
// Mutant: delete the revocationRefusal call in requireAuth → every AFTER arm
// here goes back to 200 and the test is red.
func TestDeletedMachineCredentialsAreRefused(t *testing.T) {
	srv, secret, api := revokeStack(t)
	putTestMember(t, api, Member{
		ID: "m-box", Name: "box-1", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	// The agent living ON that machine: boot token carries machine_id=m-box
	// (mintAgentToken) and the roster row still says that is where it lives.
	onBox := testAgent("m-onbox")
	onBox.DesiredMachineID = "m-box"
	putTestMember(t, api, onBox)

	now := time.Now().Unix()
	// A warden token carries machine_id "" by design (api_machines.go onboard:
	// "a warden carries NO self-binding") — the reason the gate cannot key on
	// the machine_id claim alone.
	wardenTok, err := mintJWT("m-box", "agent", 3600, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	agentTok, err := mintJWT("m-onbox", "agent", 3600, secret, now, "m-box")
	if err != nil {
		t.Fatal(err)
	}

	calls := []liveCall{
		{"the warden's heartbeat", "POST", "/api/monitoring/telemetry", wardenHeartbeat},
		{"the warden reading the roster", "GET", "/api/members", ""},
		{"the warden's MCP loopback", "POST", "/api/mcp",
			`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`},
	}
	// BEFORE: the positive control. If these are not 200 the probe is broken
	// and nothing below means anything.
	for _, c := range calls {
		if st, body := revokeCall(t, c.method, srv.URL+c.path, wardenTok, c.body); st != 200 {
			t.Fatalf("POSITIVE CONTROL FAILED — %s on a LIVE machine must be 200, got %d %s",
				c.who, st, body)
		}
	}
	if st, body := revokeCall(t, "POST", srv.URL+"/api/monitoring/telemetry",
		agentTok, `{"machine":"box-1","hardware":{"cpu_pct":3}}`); st != 200 {
		t.Fatalf("POSITIVE CONTROL FAILED — the agent on a LIVE machine must be 200, got %d %s",
			st, body)
	}

	revokeMachine(t, api, "m-box")

	// AFTER: the same tokens, the same requests.
	for _, c := range calls {
		st, body := revokeCall(t, c.method, srv.URL+c.path, wardenTok, c.body)
		assertMachineRevoked(t, "m-box", c.who, st, body)
	}
	st, body := revokeCall(t, "POST", srv.URL+"/api/monitoring/telemetry",
		agentTok, `{"machine":"box-1","hardware":{"cpu_pct":3}}`)
	assertMachineRevoked(t, "m-box",
		"an agent still running on the deleted machine", st, body)
}

// assertMachineRevoked is sentinel ③: the refusal has ONE observable shape —
// 401 + the unauthorized envelope code + a message that names the machine and
// says why. It also pins what the message must NOT do: a refusal that hands
// the caller a way around itself is not a refusal.
func assertMachineRevoked(t *testing.T, machineID, who string, status int, body string) {
	t.Helper()
	if status != http.StatusUnauthorized {
		t.Fatalf("%s after the delete: want 401, got %d %s", who, status, body)
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("%s: refusal body is not the error envelope: %v (%s)", who, err, body)
	}
	if env.Error.Code != "unauthorized" {
		t.Fatalf("%s: refusal code = %q, want %q", who, env.Error.Code, "unauthorized")
	}
	want := machineRevokedMsg(machineID)
	if env.Error.Message != want {
		t.Fatalf("%s: refusal message = %q, want %q", who, env.Error.Message, want)
	}
	// The refusal must not coach the caller around the gate. These are the
	// phrasings that would: naming a still-open door, or telling the revoked
	// process to go re-credential itself.
	for _, forbidden := range []string{
		"retry", "instead", "still works", "still available", "bypass",
		"re-install", "reinstall", "claim code", "/api/machines/claim",
		"boot-command", "mint", "another token", "without the",
	} {
		if strings.Contains(strings.ToLower(env.Error.Message), forbidden) {
			t.Fatalf("%s: refusal message must not teach a way around the gate, "+
				"but it contains %q: %q", who, forbidden, env.Error.Message)
		}
	}
}

// TestPermanentCredentialsAreLimitedToActiveWardens is the T-356a admission
// guard. A missing exp is only meaningful for the server-issued warden
// credential: even a correctly signed token must be rejected when its subject
// is an ordinary member (or absent from the roster), and immediately stops
// working once its warden row is removed.
//
// Mutant: remove permanentCredentialRefusal from requireAuth → every negative
// arm below turns 200, including the removed warden after its roster change.
func TestPermanentCredentialsAreLimitedToActiveWardens(t *testing.T) {
	srv, secret, api := revokeStack(t)
	putTestMember(t, api, Member{ID: "m-permanent-warden", Name: "permanent-box",
		Kind: KindWarden, DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive})
	agent := testAgent("m-finite-agent")
	putTestMember(t, api, agent)
	worker := testAgent("ow-finite-worker")
	worker.Kind = KindOutsource
	putTestMember(t, api, worker)

	now := time.Now().Unix()
	wardenTok, err := mintJWTWithoutExpiry("m-permanent-warden", "agent", secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	agentTok, err := mintJWTWithoutExpiry(agent.ID, "agent", secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	workerTok, err := mintJWTWithoutExpiry(worker.ID, "agent", secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	ownerTok, err := mintJWTWithoutExpiry(wireOwnerID, "owner", secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	unknownTok, err := mintJWTWithoutExpiry("m-not-on-roster", "agent", secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	boundWardenTok, err := mintJWTWithoutExpiry("m-permanent-warden", "agent", secret, now, "m-other")
	if err != nil {
		t.Fatal(err)
	}

	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", wardenTok, ""); st != http.StatusOK {
		t.Fatalf("positive control: active warden permanent credential must work, got %d %s", st, body)
	}
	for who, token := range map[string]string{
		"agent": agentTok, "outsource worker": workerTok, "owner": ownerTok,
		"unknown subject": unknownTok, "warden with machine binding": boundWardenTok,
	} {
		if st, body := revokeCall(t, "GET", srv.URL+"/api/members", token, ""); st != http.StatusUnauthorized {
			t.Errorf("permanent %s token must be 401, got %d %s", who, st, body)
		}
	}

	revokeMachine(t, api, "m-permanent-warden")
	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", wardenTok, ""); st != http.StatusUnauthorized {
		t.Fatalf("removed warden permanent credential must be 401, got %d %s", st, body)
	}
}

// ---------------------------------------------------------------------------
// ② the collateral-damage guard — the load-bearing half
// ---------------------------------------------------------------------------

// TestMachineRevocationSparesEveryLegitimateCaller is sentinel ②. One server,
// one deleted machine, and every OTHER live caller shape in the product exercised
// on a request it really makes. This test must be green BOTH before and after
// the T-9cf8 change (nothing here ever depended on the gate) — its job is to go
// red the moment the gate over-reaches. That discriminating power is proved by
// the widening mutant: drop `m.Kind == machineKind` from isRemovedMachine and
// the released-outsource-worker arm goes red, because a released worker carries
// exactly the same RosterStatusRemoved and is contractually still working.
//
// It deliberately asserts NOTHING about the gate firing — that is sentinel ①'s
// job, and mixing the two would make this guard fail for the one reason it must
// never fail for (the gate being absent), which is exactly the state it has to
// be green in to prove it is not the gate keeping these callers alive.
func TestMachineRevocationSparesEveryLegitimateCaller(t *testing.T) {
	srv, secret, api := revokeStack(t)
	now := time.Now().Unix()

	// The machine that gets deleted, and a healthy one that does not.
	putTestMember(t, api, Member{
		ID: "m-dead", Name: "dead", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	putTestMember(t, api, Member{
		ID: "m-live", Name: "live", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})

	// A normal agent on the healthy machine.
	worker := testAgent("m-kyle")
	worker.DesiredMachineID = "m-live"
	putTestMember(t, api, worker)

	// An agent that USED to live on the dead machine and has since been moved:
	// the roster says m-live, only the stale boot token still names m-dead.
	moved := testAgent("m-moved")
	moved.DesiredMachineID = "m-live"
	putTestMember(t, api, moved)

	// An admin agent (mira is seeded) and the owner.
	ownerTok, err := mintJWT(wireOwnerID, "owner", 3600, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	miraTok, err := mintJWT("mira", "agent", 3600, secret, now, "m-live")
	if err != nil {
		t.Fatal(err)
	}
	liveWardenTok, err := mintJWT("m-live", "agent", 3600, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	kyleTok, err := mintJWT("m-kyle", "agent", 3600, secret, now, "m-live")
	if err != nil {
		t.Fatal(err)
	}
	movedTok, err := mintJWT("m-moved", "agent", 3600, secret, now, "m-dead")
	if err != nil {
		t.Fatal(err)
	}

	// A RELEASED outsource worker mid-close-out. This is the arm that a
	// roster-status-only gate would kill fleet-wide: release stamps
	// RosterStatusRemoved (dal_tasks.go ReleaseWorkersForTask) while the
	// close-out contract deliberately keeps the session alive to write
	// learnings and report_task_closeout.
	if err := api.dal.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-closeout", Codename: "O-1", TaskID: "t-closeout",
		Status: WorkerStatusReleased, DesiredState: DesiredStateOnline,
		DesiredMachineID: "m-live", ReleasedTS: float64(now),
	}); err != nil {
		t.Fatalf("seed released worker: %v", err)
	}
	if err := api.dal.PutTask(Task{
		ID: "t-closeout", Title: "shipped", Status: TaskStatusDone,
		Priority: "mid", ExecutorKind: "outsource", ExecutorID: "ow-closeout",
		CreatedTS: float64(now), UpdatedTS: float64(now), ClosedTS: float64(now),
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	closeoutTok, err := mintJWT("ow-closeout", "agent", 3600, secret, now, "m-live")
	if err != nil {
		t.Fatal(err)
	}

	guards := []struct {
		liveCall
		token string
	}{
		{liveCall{"the healthy machine's warden heartbeat", "POST",
			"/api/monitoring/telemetry", wardenHeartbeat}, liveWardenTok},
		{liveCall{"the healthy machine's warden roster read", "GET",
			"/api/members", ""}, liveWardenTok},
		{liveCall{"a normal agent's telemetry", "POST",
			"/api/monitoring/telemetry", `{"machine":"live","hardware":{"cpu_pct":7}}`}, kyleTok},
		{liveCall{"a normal agent reading its tasks", "GET",
			"/api/tasks", ""}, kyleTok},
		{liveCall{"a normal agent posting chat", "POST",
			"/api/chat", `{"to":"owner","body":"working"}`}, kyleTok},
		{liveCall{"the admin agent reading settings", "GET",
			"/api/settings", ""}, miraTok},
		{liveCall{"the owner reading machines", "GET",
			"/api/machines", ""}, ownerTok},
		{liveCall{"an agent RELOCATED off the deleted machine (stale token still names it)",
			"POST", "/api/monitoring/telemetry",
			`{"machine":"live","hardware":{"cpu_pct":9}}`}, movedTok},
		{liveCall{"a RELEASED outsource worker reporting close-out", "POST",
			"/api/tasks/t-closeout/closeout", `{}`}, closeoutTok},
		{liveCall{"a RELEASED outsource worker writing chat", "POST",
			"/api/chat", `{"to":"owner","body":"learnings written"}`}, closeoutTok},
	}

	// Before the delete — the positive control for every arm.
	for _, g := range guards {
		if st, body := revokeCall(t, g.method, srv.URL+g.path, g.token, g.body); st != 200 {
			t.Fatalf("POSITIVE CONTROL FAILED — %s must be 200 before any delete, got %d %s",
				g.who, st, body)
		}
	}

	revokeMachine(t, api, "m-dead")

	// After the delete — every one of them must STILL work.
	for _, g := range guards {
		st, body := revokeCall(t, g.method, srv.URL+g.path, g.token, g.body)
		if st != 200 {
			t.Fatalf("COLLATERAL DAMAGE — %s must be unaffected by deleting an "+
				"UNRELATED machine, got %d %s", g.who, st, body)
		}
	}
}

// TestRevocationRefusalUnitTable pins the predicate itself, including the two
// fail-OPEN decisions that a future tightening would otherwise quietly reverse.
func TestRevocationRefusalUnitTable(t *testing.T) {
	rows := map[string]Member{
		"m-dead": {ID: "m-dead", Kind: KindWarden, RosterStatus: RosterStatusRemoved},
		"m-live": {ID: "m-live", Kind: KindWarden, RosterStatus: RosterStatusActive},
		"ow-released": {ID: "ow-released", Kind: KindOutsource,
			RosterStatus: RosterStatusRemoved, DesiredMachineID: "m-live"},
		"m-dismissed": {ID: "m-dismissed", Kind: KindStaff,
			RosterStatus: RosterStatusRemoved, DesiredMachineID: "m-live"},
		"m-onbox": {ID: "m-onbox", Kind: KindStaff,
			RosterStatus: RosterStatusActive, DesiredMachineID: "m-dead"},
		"m-relocated": {ID: "m-relocated", Kind: KindStaff,
			RosterStatus: RosterStatusActive, DesiredMachineID: "m-live"},
		"m-unpinned": {ID: "m-unpinned", Kind: KindStaff,
			RosterStatus: RosterStatusActive},
	}
	lookup := func(id string) (*Member, error) {
		if m, ok := rows[id]; ok {
			return &m, nil
		}
		return nil, nil // absent, not an error — the DAL's own "no such member"
	}
	boom := func(string) (*Member, error) { return nil, fmt.Errorf("db is down") }

	cases := []struct {
		name      string
		claims    map[string]any
		lookup    func(string) (*Member, error)
		wantRevo  bool
		wantNamed string
	}{
		{"the deleted machine itself (machine_id claim is empty by design)",
			map[string]any{"scope": "agent", "sub": "m-dead", "machine_id": ""},
			lookup, true, "m-dead"},
		{"an agent still pinned to the deleted machine",
			map[string]any{"scope": "agent", "sub": "m-onbox", "machine_id": "m-dead"},
			lookup, true, "m-dead"},
		{"an agent the roster has already relocated off it",
			map[string]any{"scope": "agent", "sub": "m-relocated", "machine_id": "m-dead"},
			lookup, false, ""},
		{"an unpinned agent whose token still names the dead machine",
			map[string]any{"scope": "agent", "sub": "m-unpinned", "machine_id": "m-dead"},
			lookup, true, "m-dead"},
		{"a live machine",
			map[string]any{"scope": "agent", "sub": "m-live", "machine_id": ""},
			lookup, false, ""},
		{"a RELEASED outsource worker (same roster status, different kind)",
			map[string]any{"scope": "agent", "sub": "ow-released", "machine_id": "m-live"},
			lookup, false, ""},
		{"a DISMISSED member (out of scope for this ticket)",
			map[string]any{"scope": "agent", "sub": "m-dismissed", "machine_id": "m-live"},
			lookup, false, ""},
		{"the owner (no roster row, iat floor is its seam)",
			map[string]any{"scope": "owner", "sub": wireOwnerID},
			lookup, false, ""},
		{"an unknown sub",
			map[string]any{"scope": "agent", "sub": "nobody", "machine_id": ""},
			lookup, false, ""},
		{"a DB failure must never revoke (fail-open, deliberately)",
			map[string]any{"scope": "agent", "sub": "m-dead", "machine_id": "m-dead"},
			boom, false, ""},
		{"no lookup wired at all must never revoke",
			map[string]any{"scope": "agent", "sub": "m-dead", "machine_id": "m-dead"},
			nil, false, ""},
	}
	for _, c := range cases {
		got := revocationRefusal(c.claims, c.lookup)
		if c.wantRevo {
			if got != machineRevokedMsg(c.wantNamed) {
				t.Errorf("%s: refusal = %q, want the message naming %q",
					c.name, got, c.wantNamed)
			}
			continue
		}
		if got != "" {
			t.Errorf("%s: must NOT be revoked, got refusal %q", c.name, got)
		}
	}
}

// ---------------------------------------------------------------------------
// server-self teardown guard (T-9cf8 follow-up)
// ---------------------------------------------------------------------------
//
// WHY IT LIVES IN THIS FILE: it is not an independent hardening. T-9cf8 turned
// "the server host's warden row got soft-deleted" from a machine going offline
// into a credential revocation that also takes out every agent placed on
// ServerSelfHost — which dbseed.go makes the DEFAULT placement. DELETE
// /api/machines already refused server-self; teardown-here reached the same
// soft-delete without the same guard. Raising the price of an action is what
// obliges you to close its guard.
//
// The whole thing runs through the ocwardenFS / runOcwarden seams, so no real
// warden is ever torn down — running the real binary here would be the incident
// the guard exists to prevent.

func newSelfTeardownServer(t *testing.T) (*apiServer, *[]recordedOcwardenRun) {
	t.Helper()
	s := newMachinesTestServer(t)
	s.binCacheDir = filepath.Join(t.TempDir(), "cache-bin")
	s.ocwardenFS = fstest.MapFS{"ocwarden": {Data: []byte("fake warden — never exec'd")}}
	runs := withRecordedOcwarden(t, 0) // exit 0 = a teardown that WOULD soft-delete
	// The server-local machine, exactly as dbseed.go seeds it.
	putTestMember(t, s, Member{
		ID: ServerSelfHost, Name: "this server", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	// A perfectly ordinary remote machine, for the collateral guard.
	putTestMember(t, s, Member{
		ID: "m-remote", Name: "remote", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	return s, runs
}

func postTeardownHereFor(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/machines/"+id+"/teardown-here", nil)
	s.HandleTeardownHereApiMachinesMachineIdTeardownHerePost(rec, req, id)
	return rec
}

// TestTeardownHereRefusesTheServerLocalMachine is the guard sentinel. It asserts
// the refusal AND that nothing was executed — a 409 written after the daemon was
// already booted out would be a worse bug than no guard at all.
//
// Mutant: delete the ServerSelfHost check in the teardown-here handler → this
// test goes red on the 409, on the "no ocwarden run" claim, and on the roster
// row surviving.
func TestTeardownHereRefusesTheServerLocalMachine(t *testing.T) {
	s, runs := newSelfTeardownServer(t)

	rec := postTeardownHereFor(t, s, ServerSelfHost)
	if rec.Code != http.StatusConflict {
		t.Fatalf("teardown-here on the server-local machine: want 409, got %d %s",
			rec.Code, rec.Body.String())
	}
	// Mirrored refusal — the SAME sentence DELETE /api/machines already speaks.
	if !strings.Contains(rec.Body.String(), serverSelfUndeletableMsg) {
		t.Fatalf("the refusal must mirror the delete verb's message %q, got %s",
			serverSelfUndeletableMsg, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"conflict"`) {
		t.Fatalf("refusal must ride the conflict envelope, got %s", rec.Body.String())
	}
	// The guard has to come BEFORE the subprocess, not after it.
	if len(*runs) != 0 {
		t.Fatalf("a refused teardown must never exec ocwarden, got %d run(s): %+v",
			len(*runs), *runs)
	}
	m, err := s.dal.GetMember(ServerSelfHost)
	if err != nil || m == nil {
		t.Fatalf("get server-self: %v", err)
	}
	if m.RosterStatus != RosterStatusActive {
		t.Fatalf("the server-local machine must stay on the roster, got roster=%q "+
			"— off the roster its credentials AND every agent placed on it die (T-9cf8)",
			m.RosterStatus)
	}

	// Same server, same refusal shape as the delete verb: the two must agree.
	del := httptest.NewRecorder()
	s.HandleDeleteMachineApiMachinesMemberIdDelete(del,
		httptest.NewRequest("DELETE", "/api/machines/"+ServerSelfHost, nil), ServerSelfHost)
	if del.Code != rec.Code || del.Body.String() != rec.Body.String() {
		t.Fatalf("teardown-here and delete must refuse server-self identically:\n"+
			"  teardown-here: %d %s\n  delete:        %d %s",
			rec.Code, rec.Body.String(), del.Code, del.Body.String())
	}
}

// TestTeardownHereRefusesAnOrdinaryMachineToo — this was
// TestTeardownHereStillWorksForAnOrdinaryMachine, the collateral guard that
// pinned the OPPOSITE claim: that an ordinary machine must still get a 200, a
// soft-delete and one ocwarden run, on the stated grounds that this endpoint is
// "the one working way to take a machine off this host".
//
// THAT PREMISE WAS FALSE, and it is the whole of T-42a0. The subprocess is
// addressed by HOME / uid / OC_NAMESPACE; nothing on that path can reach
// another host. So the green 200 this test used to demand meant: the SERVER
// HOST's warden was booted out of launchd, and m-remote — a machine that was
// never contacted — was written off the roster (and since T-9cf8, had its
// credentials revoked). The collateral guard was guarding the defect.
//
// It is kept, inverted, rather than deleted: the pairing with the test above
// still matters. The two refusals have different reasons and must not merge.
// The full T-42a0 sentinel set lives in
// api_machines_teardown_target_t42a0_test.go.
func TestTeardownHereRefusesAnOrdinaryMachineToo(t *testing.T) {
	s, runs := newSelfTeardownServer(t)

	rec := postTeardownHereFor(t, s, "m-remote")
	if rec.Code != http.StatusConflict {
		t.Fatalf("teardown-here aimed at an ordinary machine must be refused, got "+
			"%d %s — a 200 here means this host's own daemon was destroyed and "+
			"m-remote was falsely marked removed", rec.Code, rec.Body.String())
	}
	if len(*runs) != 0 {
		t.Fatalf("a refused teardown must never exec ocwarden, got %d run(s): %+v",
			len(*runs), *runs)
	}
	m, err := s.dal.GetMember("m-remote")
	if err != nil || m == nil {
		t.Fatalf("get m-remote: %v", err)
	}
	if m.RosterStatus != RosterStatusActive {
		t.Fatalf("a machine that was never contacted must stay on the roster, "+
			"roster=%q", m.RosterStatus)
	}
	// The two refusals must stay distinguishable — see the server-self test above.
	if strings.Contains(rec.Body.String(), serverSelfUndeletableMsg) {
		t.Fatalf("a foreign target must not be told the server-self sentence: %s",
			rec.Body.String())
	}
}

// TestSSERefusalPrecedenceIsUnchangedForNonWardens pins the ONE cross-gate
// interaction this cut has: `GET /api/events` now sits behind the auth gate as
// well as the pre-existing zombie stop gate, and the auth gate runs FIRST.
//
// WHY IT IS PINNED HERE: conformance's `test_dismissed_member_reconnect_refused`
// asserts a roster-removed member's reconnect is a **409** with code "conflict".
// That test hires a plain member (kind=staff), so the kind restriction keeps
// it on the old path and it still passes — checked, not assumed. But the moment
// someone widens this gate to every removed row, that conformance test flips to
// 401 and fails in a suite this package does not run. This test makes the
// contract fail HERE, in the package that owns the change, instead of there.
func TestSSERefusalPrecedenceIsUnchangedForNonWardens(t *testing.T) {
	srv, secret, api := revokeStack(t)
	now := time.Now().Unix()

	putTestMember(t, api, Member{
		ID: "m-gone", Name: "gone", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	dismissed := testAgent("m-fired")
	dismissed.DesiredMachineID = ""
	putTestMember(t, api, dismissed)

	wardenTok, err := mintJWT("m-gone", "agent", 3600, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	firedTok, err := mintJWT("m-fired", "agent", 3600, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}

	revokeMachine(t, api, "m-gone")
	dismissed.RosterStatus = RosterStatusRemoved
	putTestMember(t, api, dismissed)

	// The removed WARDEN: the new credential cut answers first.
	st, body := revokeCall(t, "GET", srv.URL+"/api/events", wardenTok, "")
	if st != http.StatusUnauthorized {
		t.Fatalf("a removed machine's SSE reconnect: want 401 from the credential "+
			"cut, got %d %s", st, body)
	}

	// The dismissed MEMBER: untouched, still the pre-existing zombie-gate 409
	// with the conflict envelope — the exact pair conformance asserts.
	st, body = revokeCall(t, "GET", srv.URL+"/api/events", firedTok, "")
	if st != http.StatusConflict {
		t.Fatalf("a dismissed member's SSE reconnect must stay the zombie gate's "+
			"409 (conformance test_sse.py test_dismissed_member_reconnect_refused "+
			"pins it), got %d %s", st, body)
	}
	if !strings.Contains(body, `"code":"conflict"`) {
		t.Fatalf("the dismissed member's refusal must keep the conflict envelope, got %s", body)
	}
}
