package main

// receipt_write_ordering_t55_test.go — T-55 batch B: the two invariants the
// batch's own comments carry and NOTHING ELSE DID.
//
// An independent review of this batch verified both by experiment and then
// showed that neither was pinned: moving the receipt write past the launch-intent
// setters, and dropping the heldDown gate in front of it, each left the whole
// suite green. A load-bearing rule that only a comment holds up is the shape this
// entire ticket exists to remove, so it may not be left standing in the patch
// that removes it elsewhere.
//
// FAULT INJECTION WITHOUT A SEAM: both tests reach s.dal.wdb directly and install
// a SQLite trigger. That is deliberate — the alternative is threading a
// failable/observable DAL through production code purely so a test can watch it,
// and a test seam in the write path is a thing the next writer can route around.
// A trigger observes the REAL writer.
//
// ⚠️ `UPDATE OF last_op` fires only when that column appears in the statement's
// SET clause, which after this batch it does in exactly one place:
// SetMemberOpReceipt. If someone puts the column back into PutMember's
// ON CONFLICT DO UPDATE SET, these triggers start firing from the whole-row write
// too and these tests fail confusingly — that is a acceptable second-order
// signal; the first-order one is
// TestPutMemberNeverOverwritesSingleColumnOwnedFields, which fails clearly.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUpdateMember_ReceiptLandsBeforeTheLaunchIntentSetters pins the ONE ordering
// on this handler that a retry cannot repair.
//
// HandleUpdateMember gates the whole held-down branch on launchIntentChanged,
// which compares the request against the STORED launch intent. So if the receipt
// write is placed AFTER SetMemberModel and then fails, the value is already on the
// row: the owner's retry finds nothing changed, takes neither branch, and never
// stamps the explanation. The member sits held down with the cockpit saying
// nothing about why, permanently — T-b6d9's bug through a third door.
//
// The test drives that exact sequence: block the receipt write, send the PATCH,
// and assert the MODEL DID NOT MOVE. Under the correct order the failing receipt
// write happens first and the setters never run; under the reversed order the
// model has already landed and this assertion fires. Then it unblocks and retries,
// which is the half that proves the residue is actually recoverable rather than
// merely small.
func TestUpdateMember_ReceiptLandsBeforeTheLaunchIntentSetters(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	heldDownMember(t, s, "m-order")

	before, err := s.dal.GetMember("m-order")
	if err != nil || before == nil {
		t.Fatalf("read fixture: %v", err)
	}

	if _, err := s.dal.wdb.Exec(`CREATE TRIGGER t55_block_receipt
		BEFORE UPDATE OF last_op ON member
		BEGIN SELECT RAISE(ABORT, 't55: receipt write blocked'); END;`); err != nil {
		t.Fatalf("install the blocking trigger: %v", err)
	}

	patch := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
			taskReq(t, "PATCH", "/api/members/m-order",
				map[string]any{"model": "claude-opus-4-9"}, wireOwnerID, "owner"), "m-order")
		return rec
	}

	if rec := patch(); rec.Code != http.StatusInternalServerError {
		t.Fatalf("a blocked receipt write must fail the request, got %d %s",
			rec.Code, rec.Body.String())
	}

	// 🔴 THE ASSERTION THIS FILE EXISTS FOR.
	if mid, _ := s.dal.GetMember("m-order"); mid == nil || mid.Model != before.Model {
		t.Fatalf("model = %q after the failed write, want it UNCHANGED (%q). "+
			"The receipt write must run BEFORE the launch-intent setters: once the "+
			"value is on the row, launchIntentChanged is false on the retry and the "+
			"held-down receipt can never be stamped at all.", mid.Model, before.Model)
	}

	if _, err := s.dal.wdb.Exec(`DROP TRIGGER t55_block_receipt`); err != nil {
		t.Fatalf("drop the blocking trigger: %v", err)
	}

	if rec := patch(); rec.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := s.dal.GetMember("m-order")
	if after == nil || after.Model != "claude-opus-4-9" {
		t.Fatalf("the retry must store the model, got %+v", after)
	}
	if !strings.HasPrefix(after.LastOpReason, spawnReasonHeldDown+":") {
		t.Fatalf("the retry must ALSO stamp the held-down receipt, got %q — this is "+
			"the half that makes the residue recoverable; without it the first "+
			"failure is permanent", after.LastOpReason)
	}
}

// TestOwnerVerbsWriteNoReceiptTheyDidNotStamp pins the heldDown gate in front of
// both receipt writes on the member faces.
//
// The gate is not tidiness. Persisting unconditionally would push the handler's
// snapshot of the five receipt columns — read at the top of the request, before
// anything else touched the row — back over whatever a reconcile tick stamped in
// the meantime. That is the exact clobber this ticket removes, re-introduced
// through the fix for it, and it is invisible: the write succeeds, the API
// answers 200, and the receipt the owner is shown is simply the older one.
//
// Counting the writes rather than comparing values is what makes this catch the
// mutant: with no concurrent tick in the test the clobbered value equals the
// value written back, so only the WRITE ITSELF distinguishes the two versions.
func TestOwnerVerbsWriteNoReceiptTheyDidNotStamp(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	putWarden(t, s, "mach-b")

	// A member the owner is NOT holding down: both faces take their non-held-down
	// path, stamp nothing, and must therefore write nothing to these columns.
	m := testAgent("m-live")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)

	if _, err := s.dal.wdb.Exec(`CREATE TABLE t55_receipt_writes (n INTEGER)`); err != nil {
		t.Fatalf("create the counter table: %v", err)
	}
	if _, err := s.dal.wdb.Exec(`CREATE TRIGGER t55_count_receipt
		AFTER UPDATE OF last_op ON member
		BEGIN INSERT INTO t55_receipt_writes VALUES (1); END;`); err != nil {
		t.Fatalf("install the counting trigger: %v", err)
	}
	receiptWrites := func() int {
		var n int
		if err := s.dal.wdb.QueryRow(`SELECT COUNT(*) FROM t55_receipt_writes`).Scan(&n); err != nil {
			t.Fatalf("count receipt writes: %v", err)
		}
		return n
	}
	// SENTINEL: the counter really does see a receipt write, so a zero below means
	// "nothing was written" and not "the trigger never fired".
	if err := s.dal.SetMemberOpReceipt("m-live", reconcileCmdStart, nil, "", "probe", 1); err != nil {
		t.Fatalf("sentinel write: %v", err)
	}
	if got := receiptWrites(); got != 1 {
		t.Fatalf("fixture is blind: the counting trigger saw %d writes, want 1", got)
	}
	if _, err := s.dal.wdb.Exec(`DELETE FROM t55_receipt_writes`); err != nil {
		t.Fatalf("reset the counter: %v", err)
	}

	rec := httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-live",
			map[string]any{"model": "claude-opus-4-9"}, wireOwnerID, "owner"), "m-live")
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if got := receiptWrites(); got != 0 {
		t.Errorf("the 換 model face wrote the receipt columns %d time(s) on a member it "+
			"stamped no receipt for. It must persist them ONLY when heldDown: the "+
			"values it would write are its own snapshot from the top of the request, "+
			"which is exactly the stale whole-row write this ticket exists to remove.",
			got)
	}

	rec = httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-live/relocate",
			map[string]any{"machine_id": "mach-b"}, wireOwnerID, "owner"), "m-live")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	if got := receiptWrites(); got != 0 {
		t.Errorf("the 改機器 face wrote the receipt columns %d time(s) on a member it "+
			"stamped no receipt for — same gate, same reason as the 換 model face above",
			got)
	}
}
