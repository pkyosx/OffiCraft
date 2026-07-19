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

// TestRelocateMember_MigratesLiveMember proves the event-driven reconcile is
// wired: an ONLINE member running on the old machine, re-pinned to a new one,
// gets a robust STOP dispatched to the OLD machine's warden RIGHT NOW (the
// fbc5280 relocation recycle) — and desired_state (online) is left untouched.
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
	// The relocation STOP must land on the RUNNING (old) machine's warden — the
	// session to kill lives there. Never on the new (target) machine.
	oldFrames := drainFrames(t, s, "mach-old")
	if len(oldFrames) != 1 || oldFrames[0].RPC != "stop" || oldFrames[0].Args["member_id"] != "m-move" {
		t.Fatalf("relocate must dispatch a STOP to the old machine's warden: %+v", oldFrames)
	}
	if newFrames := drainFrames(t, s, "mach-new"); len(newFrames) != 0 {
		t.Fatalf("the target (new) machine's warden must NOT get the relocation STOP: %+v", newFrames)
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
	workerID := assignOneWorker(t, api)

	rec := httptest.NewRecorder()
	api.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/"+workerID+"/relocate",
			map[string]any{"machine_id": "auto"}, wireOwnerID, "owner"), workerID)
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
	if w.DesiredMachineID != "auto" {
		t.Errorf("worker desired_machine_id = %q, want auto", w.DesiredMachineID)
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
			map[string]any{"machine_id": "auto"}, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "member") {
		t.Fatalf("released worker id: want the member 404, got %d %s", rec.Code, rec.Body.String())
	}
}
