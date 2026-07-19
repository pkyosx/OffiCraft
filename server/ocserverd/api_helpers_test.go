package main

// api_helpers_test.go — resolveMember, the single member-API target resolver.
// The P7d table fold puts outsource workers INTO the member table, so this
// resolver is the ONE guard keeping the member surface's pre-fold semantics:
// an ow- id on any member endpoint stays an honest 404 (worker lifecycle rides
// the outsource routes / the relocate fallback). Mutant: dropping the
// KindOutsource arm admits ow- ids to the whole member API → red here.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveMember_OutsourceIDIsNotFound(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	if _, err := api.resolveMember(workerID); !errors.Is(err, errNotFound) {
		t.Fatalf("resolveMember(%s) must be errNotFound for an outsource row, got %v",
			workerID, err)
	}

	// REST face: the worker id on member endpoints answers the member 404.
	rec := httptest.NewRecorder()
	api.HandleGetMemberApiMembersMemberIdGet(rec,
		taskReq(t, "GET", "/api/members/"+workerID, nil, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/members/{ow-}: want 404, got %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/"+workerID,
			map[string]any{"name": "hijack"}, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH /api/members/{ow-}: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestGetMember_WorkerSelfReadResolves (T-ea82): the ONE exception to the ow-
// 404 — a worker reading its OWN row (the ocagent recycle/wind-down hooks'
// refetch) gets the member DTO, desired_state + refocus_since included; the
// same worker targeting ANOTHER ow- id stays 404. Mutant: dropping the
// self-read fallback in HandleGetMember → the self case 404s (red).
func TestGetMember_WorkerSelfReadResolves(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.RefocusSince = 1234.5
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("stamp refocus: %v", err)
	}

	rec := httptest.NewRecorder()
	api.HandleGetMemberApiMembersMemberIdGet(rec,
		taskReq(t, "GET", "/api/members/"+workerID, nil, workerID, "agent"), workerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("self-read GET /api/members/{ow-}: want 200, got %d %s",
			rec.Code, rec.Body.String())
	}
	dto := decodeBody[memberDTO](t, rec)
	if dto.ID != workerID || dto.Kind != KindOutsource {
		t.Fatalf("self-read DTO = %+v, want the worker's own row", dto)
	}
	if dto.RefocusSince != 1234.5 || dto.DesiredState != DesiredStateOnline {
		t.Fatalf("self-read must expose refocus_since/desired_state (the recycle-hook "+
			"fields), got refocus=%v desired=%q", dto.RefocusSince, dto.DesiredState)
	}

	// Another worker's id from the same token stays the pre-fold 404.
	otherID := "ow-" + newHexID(6)
	if err := api.dal.PutOutsourceWorker(OutsourceWorker{
		ID: otherID, Codename: "S-" + otherID, TaskID: "t-x",
		Status: WorkerStatusAssigned, DesiredState: DesiredStateOnline,
	}); err != nil {
		t.Fatalf("seed other worker: %v", err)
	}
	rec = httptest.NewRecorder()
	api.HandleGetMemberApiMembersMemberIdGet(rec,
		taskReq(t, "GET", "/api/members/"+otherID, nil, workerID, "agent"), otherID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-worker GET must stay 404, got %d", rec.Code)
	}
}
