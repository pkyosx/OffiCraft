package main

// reported_launch_facts_t7f28_test.go — the guards for T-7f28: the owner must
// be able to tell "this is what the agent is running" from "you changed this
// and it has not taken effect yet".
//
// Every test here is written so that RESTORING the old behaviour turns it red.
// That is the whole point of the ticket: the two substitutions it removes had
// stood for months precisely because nothing failed when they were introduced,
// and a fix with no failing witness is one convenience away from coming back.
//
// Mutants this file kills (each verified by hand):
//   - observedHost() returning m.DesiredMachineID instead of ""      → red
//   - stampLandedMachine() re-scoped to kind==outsource               → red
//   - stampReportedLaunchFacts() dropping runtime or effort           → red
//   - monitoring's runtime/effort read off the configured member row  → red
//   - refocus_op stamped but not cleared (or vice versa)              → red

import (
	"net/http/httptest"
	"testing"
)

// TestMemberDAL_ReportedColumnsRoundTrip covers the plumbing the rest of this
// file assumes: four new columns, each scanned back into the field it was
// written from. fullMember() deliberately leaves the reported twins blank (it
// is the CONFIGURED fixture, and a dozen tests assert honest-empty off it), so
// the round trip is driven here with values that differ from the configured
// ones beside them — a fixture where they matched would stay green even if the
// DAL scanned the wrong column.
func TestMemberDAL_ReportedColumnsRoundTrip(t *testing.T) {
	d := newTestDAL(t)
	want := fullMember("m-rt")
	want.ActualModel = "claude-opus-5"
	want.ActualRuntime = RuntimeCodex
	want.ActualEffort = "medium"
	want.RefocusOp = memberOpRelocate
	if err := d.PutMember(want); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := d.SetMemberWindDownAnchors(want.ID, want.StoppingSince,
		want.StoppedSince, want.RefocusSince, want.RefocusOp); err != nil {
		t.Fatalf("seed wind-down anchors: %v", err)
	}
	got, err := d.GetMember("m-rt")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	for _, c := range []struct{ name, got, want string }{
		{"actual_model", got.ActualModel, want.ActualModel},
		{"actual_runtime", got.ActualRuntime, want.ActualRuntime},
		{"actual_effort", got.ActualEffort, want.ActualEffort},
		{"refocus_op", got.RefocusOp, want.RefocusOp},
		{"runtime (configured, untouched)", got.Runtime, want.Runtime},
		{"model (configured, untouched)", got.Model, want.Model},
		{"effort (configured, untouched)", got.Effort, want.Effort},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// TestOutsourceWorker_ReportedColumnsSurviveTheWorkerRoundTrip is the trap the
// projection sets: memberFromWorker rebuilds a Member from scratch, so any
// column it forgets is ZEROED on the next worker write. A reported value
// stamped by the telemetry path would be silently erased by the very next
// outsource tick — invisible in every unit test that does not round-trip.
func TestOutsourceWorker_ReportedColumnsSurviveTheWorkerRoundTrip(t *testing.T) {
	m := fullMember("ow-rt")
	m.Kind = KindOutsource
	m.Codename = "O-7"
	m.ActualModel = "claude-opus-5"
	m.ActualRuntime = RuntimeCodex
	m.ActualEffort = "medium"
	m.RefocusOp = memberOpRelocate
	m.LastMachineID = "m-old"

	back := memberFromWorker(workerFromMember(m))
	for _, c := range []struct{ name, got, want string }{
		{"actual_model", back.ActualModel, m.ActualModel},
		{"actual_runtime", back.ActualRuntime, m.ActualRuntime},
		{"actual_effort", back.ActualEffort, m.ActualEffort},
		{"refocus_op", back.RefocusOp, m.RefocusOp},
		{"last_machine_id", back.LastMachineID, m.LastMachineID},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q — memberFromWorker dropped it, so the "+
				"next outsource write erases what telemetry just stamped",
				c.name, c.got, c.want)
		}
	}
}

// TestObservedHost_NeverSubstitutesTheOwnersPin is the ticket's red line in one
// assertion: an unobserved member reads blank, not "wherever you pinned it".
//
// The substitution it forbids was not a rounding error — it made the observed
// cell and the intent cell of the detail panel show the same value, so a move
// that had NOT happened was byte-indistinguishable from one that had.
func TestObservedHost_NeverSubstitutesTheOwnersPin(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), telemetry: newMemStore()}
	m := fullMember("mira")
	m.DesiredMachineID = "m-pinned"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if got := s.observedHost(m); got != "" {
		t.Errorf("observedHost of an unobserved member = %q, want \"\" — "+
			"serving the pin here is what made a pending move look complete", got)
	}

	// A real observation still wins: the guard must not have blinded the fold.
	s.telemetry.Set(m.ID, map[string]any{"machine": "m-actual"})
	if got := s.observedHost(m); got != "m-actual" {
		t.Errorf("observedHost with telemetry = %q, want m-actual", got)
	}
}

// TestMemberDTO_PendingRelocationStaysLegibleWhileOffline is the reason the
// blank above is not enough on its own. Blanking the observed cell without a
// durable last-landing would leave an offline member with NOTHING to compare
// the new pin against — honest, but useless. actual_machine is what keeps the
// pending move visible, so the two changes have to be pinned together.
func TestMemberDTO_PendingRelocationStaysLegibleWhileOffline(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), telemetry: newMemStore()}
	m := fullMember("mira")
	m.LastMachineID = "m-old" // where it was last actually seen
	m.DesiredMachineID = "m-new"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dto := s.newMemberDTO(m, "Assistant", s.observedHost(m), 0)
	if dto.Machine != "" {
		t.Errorf("machine = %q, want \"\" — it is not running anywhere", dto.Machine)
	}
	if dto.ActualMachine != "m-old" {
		t.Errorf("actual_machine = %q, want m-old — without it an offline "+
			"member has nothing to compare the new pin against", dto.ActualMachine)
	}
	if dto.DesiredMachineID != "m-new" {
		t.Errorf("desired_machine_id = %q, want m-new", dto.DesiredMachineID)
	}
}

// TestStampReportedLaunchFacts_PersistsAllThreeIndependently pins that a report
// carrying one field does not disturb the other two, that a blank is "not
// measured" rather than an erasure, and — the part that matters for the panel —
// that the three columns never inherit the configured values beside them.
func TestStampReportedLaunchFacts_PersistsAllThreeIndependently(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), telemetry: newMemStore()}
	seed := fullMember("mira")
	seed.ActualModel, seed.ActualRuntime, seed.ActualEffort = "", "", ""
	if err := s.dal.PutMember(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	read := func() Member {
		t.Helper()
		got, err := s.dal.GetMember("mira")
		if err != nil || got == nil {
			t.Fatalf("read back: %v", err)
		}
		return *got
	}

	// Nothing reported yet ⇒ blank, NOT the configured runtime/model/effort.
	if m := read(); m.ActualRuntime != "" || m.ActualEffort != "" || m.ActualModel != "" {
		t.Fatalf("unreported member already carries reported values: %+v", m)
	}

	s.stampReportedLaunchFacts("mira", "claude-opus-5", RuntimeCodex, "xhigh", "test")
	if m := read(); m.ActualModel != "claude-opus-5" ||
		m.ActualRuntime != RuntimeCodex || m.ActualEffort != "xhigh" {
		t.Fatalf("first report did not persist all three: %+v", m)
	}

	// A partial report touches only what it carries.
	s.stampReportedLaunchFacts("mira", "", RuntimeClaude, "", "test")
	m := read()
	if m.ActualRuntime != RuntimeClaude {
		t.Errorf("actual_runtime = %q, want claude", m.ActualRuntime)
	}
	if m.ActualModel != "claude-opus-5" || m.ActualEffort != "xhigh" {
		t.Errorf("a partial report erased a neighbour: %+v", m)
	}

	// The configured values are untouched throughout — they are the owner's
	// intent, and the reported path must never write onto them.
	if m.Runtime != seed.Runtime || m.Model != seed.Model || m.Effort != seed.Effort {
		t.Errorf("reported stamp overwrote the owner's configuration: %+v", m)
	}
}

// TestRefocusOp_IsStampedAndClearedWithTheWindow pins the cause marker to the
// window it describes. A cause that outlives its window would be worse than no
// cause at all: the panel would keep announcing a wind-down that had already
// finished.
func TestRefocusOp_IsStampedAndClearedWithTheWindow(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	m := testAgent("m-op")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	m.Model = "old-model"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-op", "mach-a")

	rec := httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-op",
			map[string]any{"model": "new-model"}, wireOwnerID, "owner"), "m-op")
	if rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	dto := decodeBody[memberDTO](t, rec)
	if dto.RefocusOp != memberOpModel {
		t.Errorf("refocus_op = %q, want %q — without the cause the panel can "+
			"only say 'last refocus', which reads as history",
			dto.RefocusOp, memberOpModel)
	}
	if dto.RefocusSince <= 0 {
		t.Fatalf("refocus_since must be stamped alongside the cause")
	}
	// 換 model is a 停止 (T-ed79): nothing collects it on a clock, so the panel
	// must render NO deadline. 0 is how the wire says that — a positive number
	// here would put a countdown on screen that the reconcile tick has no
	// intention of honouring.
	if dto.RefocusDeadline != 0 {
		t.Errorf("refocus_deadline = %v, want 0 — 換 model runs no clock",
			dto.RefocusDeadline)
	}

	// The cause must not outlive its window: report_waking closes both.
	rec = httptest.NewRecorder()
	s.HandleReportWakingApiSelfWakingPost(rec,
		taskReq(t, "POST", "/api/self/waking", map[string]any{}, "m-op", "agent"))
	if rec.Code != 200 {
		t.Fatalf("report_waking: %d %s", rec.Code, rec.Body.String())
	}
	woken := decodeBody[memberDTO](t, rec)
	if woken.RefocusOp != "" || woken.RefocusDeadline != 0 {
		t.Errorf("a closed window still advertises op=%q deadline=%v — a cause "+
			"that outlives its window makes the panel announce a wind-down that "+
			"already finished", woken.RefocusOp, woken.RefocusDeadline)
	}
}

// TestRefocusDeadline_IsZeroWithoutAWindow guards the derivation itself: no
// window, no deadline. Without this a client would render an epoch computed
// from 0 — 1970 — as "takes effect by".
func TestRefocusDeadline_IsZeroWithoutAWindow(t *testing.T) {
	if got := refocusDeadline(0, 120); got != 0 {
		t.Errorf("refocusDeadline(0, 120) = %v, want 0", got)
	}
	if got := refocusDeadline(1600, 120); got != 1720 {
		t.Errorf("refocusDeadline(1600, 120) = %v, want 1720", got)
	}
}

// TestOutsourceWorkerDTO_CarriesTheReportedTwins pins the outsource half: the
// worker panel had no reported model/runtime/effort on its own DTO at all, so
// it could not compare anything without joining the monitoring fold.
func TestOutsourceWorkerDTO_CarriesTheReportedTwins(t *testing.T) {
	w := OutsourceWorker{
		ID: "ow-1", Codename: "O-7",
		Runtime: RuntimeClaude, Model: "claude-sonnet-4-5", Effort: "high",
		ActualRuntime: RuntimeCodex, ActualModel: "claude-opus-5", ActualEffort: "low",
		LastMachineID: "m-old", DesiredMachineID: "m-new",
		Status: WorkerStatusActive,
	}
	dto := newOutsourceWorkerDTO(w, nil, outsourceWorkerProjection{now: 1})

	for _, c := range []struct{ name, got, want string }{
		{"runtime (configured)", dto.Runtime, RuntimeClaude},
		{"actual_runtime (reported)", dto.ActualRuntime, RuntimeCodex},
		{"model (configured)", dto.Model, "claude-sonnet-4-5"},
		{"actual_model (reported)", dto.ActualModel, "claude-opus-5"},
		{"effort (configured)", dto.Effort, "high"},
		{"actual_effort (reported)", dto.ActualEffort, "low"},
		{"actual_machine (last observed)", dto.ActualMachine, "m-old"},
		{"desired_machine_id (pin)", dto.DesiredMachineID, "m-new"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// TestIngestTelemetry_ReportedRuntimeReachesTheWire closes the loop end to end:
// the runtime an agent reports was ingested and then thrown away on every read
// path, so no surface could see it. This drives the real handler rather than
// the stamp helper, because the discarding happened in the READ fold, not the
// write — a test that only called the helper would have passed all along.
func TestIngestTelemetry_ReportedRuntimeReachesTheWire(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("mira")
	m.Runtime = RuntimeClaude // configured
	m.ActualRuntime = ""
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"runtime":"codex","effort":"low"}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}

	rec := httptest.NewRecorder()
	s.HandleGetMemberApiMembersMemberIdGet(rec,
		taskReq(t, "GET", "/api/members/mira", nil, wireOwnerID, "owner"), "mira")
	if rec.Code != 200 {
		t.Fatalf("GET member: %d %s", rec.Code, rec.Body.String())
	}
	dto := decodeBody[memberDTO](t, rec)
	if dto.ActualRuntime != RuntimeCodex {
		t.Errorf("actual_runtime = %q, want codex — the reported runtime is "+
			"ingested but never surfaced", dto.ActualRuntime)
	}
	if dto.ActualEffort != "low" {
		t.Errorf("actual_effort = %q, want low", dto.ActualEffort)
	}
	if dto.Runtime != RuntimeClaude {
		t.Errorf("runtime = %q, want the CONFIGURED claude — the two must stay "+
			"separate cells or there is nothing to compare", dto.Runtime)
	}
}
