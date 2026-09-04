package main

// worker_handover_lessons_t4595_test.go — T-4595.
//
// WHY THIS FILE EXISTS. The handover guidance in seeds/system_interaction.md
// every agent — staff AND outsource worker — is told to run inside the ~120s
// grace window. Step 3 used to spell one literal tool pair: `get_lessons` →
// `replace_lessons`. A worker that obeys it LITERALLY cannot succeed:
//
//   - `fillLessonsIdentityArgs` (api_roles.go) folds a blank `role_key` to the
//     caller's own roster role, and a worker's roster row carries role_key ""
//     (dal_tasks.go memberFromWorker: "role_key stays \"\""), so the fold lands
//     on defaultBootRole == "assistant";
//   - `lessonsWriteAuthz` (api_roles.go) then compares the caller's member
//     RoleKey ("") against that path role ("assistant") and answers 403.
//
// So the worker spends its handover budget on a call that cannot land, and that
// round's learnings are simply gone. Worse, the READ half is ungated, so before
// failing it reads a long-term memory that is NOT its own.
//
// These tests pin the MECHANISM, not the prose: they are what makes the
// rewrite ("staff consolidate their role's lessons, outsource workers
// consolidate their task manual's learnings") a statement about this server
// rather than an opinion. If the authz chain is ever changed so a worker CAN
// write lessons, these go red and the seed sentence should be revisited.

import (
	"strings"
	"testing"
	"time"
)

// t4595WorkerIdentity stands up a wired lessons server plus one outsource
// worker roster row (the exact shape memberFromWorker writes) and returns the
// server URL and that worker's token.
func t4595WorkerIdentity(t *testing.T) (string, string) {
	t.Helper()
	srv, dal, secret := newLessonsTestServer(t)
	const workerID = "ow-t4595"
	if err := dal.PutMember(Member{
		ID: workerID, Kind: KindOutsource, RoleKey: "",
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember(worker): %v", err)
	}
	// Read the row back: the premise of this whole file is that a worker's
	// roster role_key is empty. Assert it from the store, do not assume it.
	got, err := dal.GetMember(workerID)
	if err != nil || got == nil {
		t.Fatalf("GetMember(worker): %v", err)
	}
	if got.RoleKey != "" {
		t.Fatalf("premise broken: an outsource roster row now carries role_key %q; "+
			"re-derive the handover step-3 argument before trusting it", got.RoleKey)
	}
	tok, err := mintJWT(workerID, "agent", 300, secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatalf("mint worker token: %v", err)
	}
	return srv.URL, tok
}

// TestWorkerCannotWriteLessonsTheHandoverSOPUsedToPrescribe drives the literal
// handover step-3 pair as a worker. The read must succeed (that is the trap: it
// hands back somebody else's doc), the write must 403.
func TestWorkerCannotWriteLessonsTheHandoverSOPUsedToPrescribe(t *testing.T) {
	url, workerTok := t4595WorkerIdentity(t)

	// Step 3, first half — exactly what the SOP said: `get_lessons`, no
	// arguments (identity comes from the token). It SERVES.
	isErr, code, text := lessonsCall(t, url, workerTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_lessons","arguments":{}}}`)
	if isErr {
		t.Fatalf("get_lessons as a worker must still serve (that is the trap); got code=%q", code)
	}
	// And prove it served a doc that is NOT the worker's: the identity fold
	// landed on the default boot role.
	if !strings.Contains(text, `"role_key":"`+defaultBootRole+`"`) {
		t.Fatalf("expected the blank role_key to fold to %q; body=%s", defaultBootRole, text)
	}

	// Step 3, second half — `replace_lessons`. It CANNOT land.
	isErr, code, text = lessonsCall(t, url, workerTok,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"replace_lessons","arguments":{"text":"handover learnings from this round"}}}`)
	if !isErr {
		t.Fatalf("replace_lessons as a worker must be refused — if this now lands, "+
			"the handover guidance can go back to naming one tool pair; body=%s", text)
	}
	if code != "forbidden" {
		t.Fatalf("expected a forbidden refusal, got code=%q body=%s", code, text)
	}
}

// TestStaffCanStillWriteLessonsTheHandoverSOPPrescribes is the positive
// control. Without it, the 403 above could come from a broken fixture (a
// mis-minted token, an unwired route) rather than from the worker's missing
// role, and the seed rewrite would be justified by nothing.
func TestStaffCanStillWriteLessonsTheHandoverSOPPrescribes(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	const staffID = "m-t4595staff"
	const staffRole = "r-t4595staff"
	seedLessonsOverlay(t, dal, staffRole, "staff baseline\n")
	if err := dal.PutMember(Member{
		ID: staffID, Kind: KindStaff, RoleKey: staffRole,
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember(staff): %v", err)
	}
	tok, err := mintJWT(staffID, "agent", 300, secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatalf("mint staff token: %v", err)
	}
	if isErr, code, text := lessonsCall(t, srv.URL, tok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"replace_lessons","arguments":{"text":"staff handover learnings"}}}`); isErr {
		t.Fatalf("a staff member must still be able to run the handover step 3; code=%q body=%s", code, text)
	}
}
