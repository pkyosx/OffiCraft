package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRelocateMember_PlacementOnly is the CORE contract: relocate writes the
// owner-pinned desired_machine_id and NOTHING else. In particular desired_state
// is left exactly as it was — the sharp contrast with activate (which force-
// revives desired_state=online). An offline member relocated stays offline.
func TestRelocateMember_PlacementOnly(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-new")

	// An OFFLINE member pinned elsewhere — relocate must re-pin WITHOUT waking it.
	m := testAgent("m-off")
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = ServerSelfHost
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-off/relocate",
			map[string]any{"machine_id": "mach-new"}, wireOwnerID, "owner"), "m-off")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}

	got, err := s.dal.GetMember("m-off")
	if err != nil || got == nil {
		t.Fatalf("re-read member: %v", err)
	}
	if got.DesiredMachineID != "mach-new" {
		t.Errorf("desired_machine_id = %q, want mach-new", got.DesiredMachineID)
	}
	// The mutant that matters: if relocate ever borrowed activate's semantics it
	// would flip desired_state to online. It must stay OFFLINE.
	if got.DesiredState != DesiredStateOffline {
		t.Errorf("relocate must NOT touch desired_state: got %q, want offline (a relocate is not a wake)",
			got.DesiredState)
	}
}

// TestRelocateMember_MigratesLiveMember is the END-TO-END 改機器 of a LIVE
// member, and T-b6d9 changed its shape on purpose: it used to assert that the
// handler dispatched a robust STOP to the old warden ON THE SPOT (exactly one
// stop frame, same instant) — i.e. it pinned the hard kill as the contract, so
// making the move graceful HAD to redden it.
//
// The move is now a wind-down: the pin lands, a refocus epoch is stamped (which
// is the ONLY thing cli/ocagent's recycleHook gates the 下線程序 wake on), and
// NOTHING is dispatched until the agent answers report_stopped (or RecycleGrace
// expires). Both halves are asserted here, because either alone is satisfiable
// by a broken implementation: "no frame yet" alone is also true of a relocate
// that does nothing at all, and "refocus stamped" alone is also true of one that
// stamps AND kills.
func TestRelocateMember_MigratesLiveMember(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-old")
	putWarden(t, s, "mach-new")

	mover := testAgent("m-move")
	mover.DesiredState = DesiredStateOnline
	mover.DesiredMachineID = "mach-old"
	putTestMember(t, s, mover)
	connectOnline(t, s, "mach-old")                  // old warden reachable (holds the session)
	connectOnline(t, s, "mach-new")                  // new warden reachable
	connectOnlineMachine(t, s, "m-move", "mach-old") // the mover runs on the OLD machine

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-move/relocate",
			map[string]any{"machine_id": "mach-new"}, wireOwnerID, "owner"), "m-move")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}

	got, err := s.dal.GetMember("m-move")
	if err != nil || got == nil {
		t.Fatalf("re-read member: %v", err)
	}
	if got.DesiredMachineID != "mach-new" {
		t.Errorf("desired_machine_id = %q, want mach-new", got.DesiredMachineID)
	}
	if got.DesiredState != DesiredStateOnline {
		t.Errorf("a live member's desired_state must stay online across a relocate: got %q", got.DesiredState)
	}
	// (a) the agent was TOLD. refocus_since > 0 with desired_state=online is the
	// exact condition recycleHook.maybeRecycle refetches and prints the SOP on;
	// without it the agent never learns the move is coming.
	if got.RefocusSince <= 0.0 {
		t.Errorf("a live 改機器 must stamp a refocus epoch (the ONLY thing the agent's "+
			"recycle hook gates the wind-down SOP on): refocus_since = %v", got.RefocusSince)
	}
	// (b) and it was given TIME: no kill went out with the owner's click.
	if f := drainFrames(t, s, "mach-old"); len(f) != 0 {
		t.Fatalf("a graceful 改機器 dispatches NO kill on the click — the 收口 owns it: %+v", f)
	}
	if f := drainFrames(t, s, "mach-new"); len(f) != 0 {
		t.Fatalf("the target (new) machine's warden must get nothing on the click: %+v", f)
	}

	// 收口: the agent finishes its dump and reports stopped → the robust STOP is
	// dispatched NOW, and it must still land on the RUNNING (old) machine's
	// warden — the session to kill lives there, never on the target.
	rec = httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, "m-move", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	oldFrames := drainFrames(t, s, "mach-old")
	if len(oldFrames) != 1 || oldFrames[0].RPC != "stop" || oldFrames[0].Args["member_id"] != "m-move" {
		t.Fatalf("the 收口 must dispatch a STOP to the old machine's warden: %+v", oldFrames)
	}
	if newFrames := drainFrames(t, s, "mach-new"); len(newFrames) != 0 {
		t.Fatalf("the target (new) machine's warden must NOT get the relocation STOP: %+v", newFrames)
	}
	// The pin survives the wind-down, so the next tick's plain START lands on the
	// NEW machine — the move actually completes, it is not merely deferred.
	if after, _ := s.dal.GetMember("m-move"); after == nil || after.DesiredMachineID != "mach-new" {
		t.Fatalf("the new pin must survive the 收口: %+v", after)
	}
}

// TestRelocateMember_OfflineMemberIsNotWoundDown is the誤擋 half: 改機器 on a
// member with NO live session must stay the instant re-pin it always was. There
// is nothing to hear a 預告 and nothing to flush, so stamping a refocus epoch
// would only park a marker no one will ever read (the agent's own gate re-checks
// desired_state=online) and make the owner wait for a 收口 that cannot come.
func TestRelocateMember_OfflineMemberIsNotWoundDown(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-new")

	m := testAgent("m-dark")
	m.DesiredState = DesiredStateOnline // wants to run, but holds no SSE connection
	m.DesiredMachineID = ServerSelfHost
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-dark/relocate",
			map[string]any{"machine_id": "mach-new"}, wireOwnerID, "owner"), "m-dark")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	got, err := s.dal.GetMember("m-dark")
	if err != nil || got == nil {
		t.Fatalf("re-read member: %v", err)
	}
	if got.DesiredMachineID != "mach-new" {
		t.Errorf("the pin must land immediately: %q", got.DesiredMachineID)
	}
	if got.RefocusSince != 0.0 {
		t.Errorf("an offline member has nothing to wind down: refocus_since = %v", got.RefocusSince)
	}
}

// TestRelocateMember_RestartIsUntouched is the SENTINEL for the one verb this
// ticket must not move a millimetre: 重新聚焦 (refocus_member) stamps a refocus
// epoch and dispatches NOTHING — the §4.5 recycle arm owns the kill. If the
// staff wind-down funnel ever grew a dispatch (or a deny-list arm that skipped
// the stamp), this is what would catch it.
func TestRelocateMember_RestartIsUntouched(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-old")

	m := testAgent("m-refocus")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-old"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-old")
	connectOnlineMachine(t, s, "m-refocus", "mach-old")

	rec := httptest.NewRecorder()
	s.HandleRefocusMemberApiMembersMemberIdRefocusPost(rec,
		taskReq(t, "POST", "/api/members/m-refocus/refocus", map[string]any{},
			wireOwnerID, "owner"), "m-refocus")
	if rec.Code != http.StatusOK {
		t.Fatalf("refocus: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := s.dal.GetMember("m-refocus")
	if got == nil || got.RefocusSince <= 0.0 {
		t.Fatalf("refocus must stamp the epoch: %+v", got)
	}
	if got.DesiredMachineID != "mach-old" {
		t.Errorf("refocus must not touch the pin: %q", got.DesiredMachineID)
	}
	if f := drainFrames(t, s, "mach-old"); len(f) != 0 {
		t.Fatalf("refocus dispatches nothing — the recycle arm owns the kill: %+v", f)
	}
}

// TestRelocateMember_Rejects: a concrete pin that names no real machine, and an
// unknown member, both 404 — a stale/typo'd id never pins a member to a
// placement that can never boot, and a missing member never silently succeeds.
func TestRelocateMember_Rejects(t *testing.T) {
	s := newReconcileTestServer(t)
	putTestMember(t, s, testAgent("m-real"))

	// Unknown machine → 404 (validation before the pin lands).
	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-real/relocate",
			map[string]any{"machine_id": "ghost"}, wireOwnerID, "owner"), "m-real")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown machine: want 404, got %d %s", rec.Code, rec.Body.String())
	}
	// The pin must NOT have landed.
	if got, _ := s.dal.GetMember("m-real"); got == nil || got.DesiredMachineID == "ghost" {
		t.Errorf("a rejected relocate must not pin the member: %+v", got)
	}

	// Unknown member → 404.
	rec = httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-nope/relocate",
			map[string]any{"machine_id": "auto"}, wireOwnerID, "owner"), "m-nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown member: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestRelocateMember_RejectsUnresolvableMachine: ANY non-"" machine_id must
// resolve to a real machine — the legacy "auto" spelling has no exemption (it
// named no machine, so the pin could never boot). "" is no longer a clear
// either (owner 2026-07-27) — it is a 400, and an absent key a 422.
func TestRelocateMember_RejectsUnresolvableMachine(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-real")
	m := testAgent("m-pin")
	m.DesiredMachineID = "mach-real"
	putTestMember(t, s, m)

	relocate := func(machineID string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
			taskReq(t, "POST", "/api/members/m-pin/relocate",
				map[string]any{"machine_id": machineID}, wireOwnerID, "owner"), "m-pin")
		return rec
	}

	// SENTINEL: a real concrete machine id still pins.
	if rec := relocate("mach-real"); rec.Code != http.StatusOK {
		t.Fatalf("concrete machine relocate must 200, got %d %s", rec.Code, rec.Body.String())
	}
	for _, machineID := range []string{"auto", "ghost"} {
		if rec := relocate(machineID); rec.Code != http.StatusNotFound {
			t.Fatalf("machine_id %q must 404, got %d %s", machineID, rec.Code, rec.Body.String())
		}
		got, _ := s.dal.GetMember("m-pin")
		if got == nil || got.DesiredMachineID != "mach-real" {
			t.Fatalf("a rejected %q relocate must leave the pin alone: %+v", machineID, got)
		}
	}
	// Owner 2026-07-27: 搬遷一定要帶機器. "" used to be the legal UNPIN, which made
	// the destroying form the one you got by forgetting a field — and it flatly
	// contradicted the sticky-placement rule that a hand-moved worker is pulled
	// back by no configuration. There is no unpin verb on this route any more.
	if rec := relocate(""); rec.Code != http.StatusBadRequest {
		t.Fatalf("a blank machine_id must 400, got %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.dal.GetMember("m-pin"); got == nil || got.DesiredMachineID != "mach-real" {
		t.Fatalf("a refused blank relocate must leave the pin alone: %+v", got)
	}
	// An ABSENT key is the missing-required-field face (422), not a semantic
	// refusal — the frozen decodeJSONBodyRequired contract.
	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-pin/relocate", map[string]any{}, wireOwnerID, "owner"),
		"m-pin")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an absent machine_id must 422, got %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.dal.GetMember("m-pin"); got == nil || got.DesiredMachineID != "mach-real" {
		t.Fatalf("a refused absent relocate must leave the pin alone: %+v", got)
	}
}

// TestRelocateMember_AdminGated pins the route's authz floor through the FULL
// wired stack (the route-table Requires=admin_agent): a plain agent is a flat
// 403 envelope, denied before the handler resolves anything.
func TestRelocateMember_AdminGated(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")

	req, _ := http.NewRequest("POST", srv.URL+"/api/members/mira/relocate",
		strings.NewReader(`{"machine_id":"auto"}`))
	req.Header.Set("Authorization", "Bearer "+agentTok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 403 || !strings.Contains(string(body), `"code":"forbidden"`) {
		t.Fatalf("agent on the admin-gated relocate row: want 403 envelope, got %d %s", resp.StatusCode, body)
	}
}

// TestRelocateMember_WorkerIdFallback (P7c, gate rc-2786636f30e5 外包對齊正職):
// the relocate verb means "move one agent" — an id naming no roster member
// falls through to the outsource-worker table, so the SAME handler (and thus
// the MCP relocate_member tool) relocates a worker. The pin lands on the
// worker row and the response is the worker projection, not a member DTO.
func TestRelocateMember_WorkerIdFallback(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	seedMachine(t, api, ServerSelfHost)
	workerID := assignOneWorker(t, api)

	rec := httptest.NewRecorder()
	api.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/"+workerID+"/relocate",
			map[string]any{"machine_id": ServerSelfHost}, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate(worker id): %d %s", rec.Code, rec.Body.String())
	}
	dto := decodeBody[outsourceWorkerDTO](t, rec)
	if dto.ID != workerID {
		t.Errorf("response must be the worker projection: got id %q, want %q", dto.ID, workerID)
	}
	// Bind the ROUTING, not just the row write: since the P7d fold both paths
	// write the SAME member row, so only the response shape tells the worker
	// relocate core apart from the member path. Worker-only keys must be
	// present and the member DTO's "role" must not — if resolveMember ever
	// admits ow- ids, this turns red.
	body := decodeBody[map[string]any](t, rec)
	if _, ok := body["presence"]; !ok {
		t.Errorf("response lacks presence — worker projection not served: %s", rec.Body.String())
	}
	if _, ok := body["codename"]; !ok {
		t.Errorf("response lacks codename — worker projection not served: %s", rec.Body.String())
	}
	if _, ok := body["role"]; ok {
		t.Errorf("response carries the member DTO's role — the relocate rode the member path: %s",
			rec.Body.String())
	}
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if w.DesiredMachineID != ServerSelfHost {
		t.Errorf("worker desired_machine_id = %q, want %s", w.DesiredMachineID, ServerSelfHost)
	}

	// A RELEASED worker no longer resolves — the fallback answers the honest
	// member 404 (same as an id in neither table), never a zombie move.
	w.Status = WorkerStatusReleased
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("release worker: %v", err)
	}
	rec = httptest.NewRecorder()
	api.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/"+workerID+"/relocate",
			map[string]any{"machine_id": ServerSelfHost}, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "member") {
		t.Fatalf("released worker id: want the member 404, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestRelocateMember_MachineIsMandatory (owner 2026-07-27: 搬遷一定要帶機器) is
// the POSITIVE-and-NEGATIVE pair for the ruling on the member face.
//
// Before it, all three shapes — key absent, explicit null, "" — collapsed to ""
// and CLEARED the pin. That made the destroying form the one you got by
// forgetting a field, and it contradicted the sticky-placement rule that a
// hand-moved agent is pulled back by no configuration: the same verb both set
// and destroyed the placement. There is no unpin verb on this route any more.
func TestRelocateMember_MachineIsMandatory(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-new")
	m := testAgent("m-mand")
	m.DesiredMachineID = "mach-new"
	putTestMember(t, s, m)

	post := func(body any) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
			taskReq(t, "POST", "/api/members/m-mand/relocate", body, wireOwnerID, "owner"),
			"m-mand")
		return rec
	}

	// The refusals. Each one must ALSO leave the pin exactly as it was — a 400
	// that had already half-executed would be the worse bug.
	for _, tc := range []struct {
		name string
		body any
		want int
	}{
		{"blank", map[string]any{"machine_id": ""}, http.StatusBadRequest},
		{"null", map[string]any{"machine_id": nil}, http.StatusBadRequest},
		{"absent", map[string]any{}, http.StatusUnprocessableEntity},
	} {
		rec := post(tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s machine_id: got %d %s, want %d",
				tc.name, rec.Code, rec.Body.String(), tc.want)
		}
		got, _ := s.dal.GetMember("m-mand")
		if got == nil || got.DesiredMachineID != "mach-new" {
			t.Fatalf("%s machine_id must leave the pin alone: %+v", tc.name, got)
		}
	}

	// SENTINEL: a real machine still relocates, so the refusals above are the
	// rule and not a handler that stopped working.
	putWarden(t, s, "mach-far")
	if rec := post(map[string]any{"machine_id": "mach-far"}); rec.Code != http.StatusOK {
		t.Fatalf("a named machine must still relocate: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.dal.GetMember("m-mand"); got == nil || got.DesiredMachineID != "mach-far" {
		t.Fatalf("the relocate must land: %+v", got)
	}
}
