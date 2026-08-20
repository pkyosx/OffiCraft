package main

// doc_capacity_t6bd2_test.go — the near-cap signal (T-6bd2).
//
// 🔴 WHAT THESE TESTS HAVE TO PROVE, AND WHAT THEY DELIBERATELY DO NOT.
// "A full document still answers 400" is the OLD behaviour and proves nothing
// about this ticket. Every assertion below is about a carrier that is CLOSE to
// its cap and NOT YET FULL — i.e. a write that still succeeds — and about
// whether anything says so before the writing starts.
//
// Every number here is written as a LITERAL. Reading the caps off the code
// under test would let a mutant move the threshold and the assertion together
// and stay green, which is the one failure mode a guard cannot have.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func t6bd2Server(t *testing.T) *apiServer {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "t6bd2.db"))
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
	return newAPIServer(dal, NewHub(), []byte("t6bd2-secret"), 3600, "../..")
}

func t6bd2Text(n int) string { return strings.Repeat("x", n) }

// t6bd2Capacity pulls the caller's wake snapshot through the REAL route and
// returns the doc_capacity block AS IT ARRIVES ON THE WIRE. It decodes the raw
// JSON rather than the Go struct on purpose: a row that is computed but never
// serialised is the third mutant this file has to catch, and a struct-level
// assertion cannot see the difference.
func t6bd2Capacity(t *testing.T, s *apiServer, actor string) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleResumeSummaryApiResumeSummaryGet(rec,
		taskReq(t, "GET", "/api/resume-summary", nil, actor, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("resume-summary: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		DocCapacity []map[string]any `json:"doc_capacity"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	return body.DocCapacity
}

// t6bd2Docs reduces the block to the `doc` labels it named.
func t6bd2Docs(rows []map[string]any) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r["doc"].(string))
	}
	return out
}

func t6bd2Row(t *testing.T, rows []map[string]any, substr string) map[string]any {
	t.Helper()
	for _, r := range rows {
		if strings.Contains(r["doc"].(string), substr) {
			return r
		}
	}
	t.Fatalf("no row naming %q; the block named %v", substr, t6bd2Docs(rows))
	return nil
}

// t6bd2FillAll stuffs EVERY one of the nine carriers to just under its own cap
// and returns the substrings each is expected to be named by. The sizes are
// chosen per band (see docCapacityNear) so each one is near but not full:
//
//	duty 1000              → 800 stored, 200 left (threshold 250)
//	insight/lessons 15000  → 12500 stored, 2500 left (threshold 3000)
//	manual sop/learn 15000 → 12500 stored, 2500 left
//	system interaction 60000 → 55000 stored, 5000 left (threshold 6000)
//	boot sequence 15000    → 12500 stored, 2500 left
//	offboard 15000         → 12500 stored, 2500 left
//	step note 4000         → 3200 stored, 800 left (absolute threshold 1000)
func t6bd2FillAll(t *testing.T, s *apiServer, actor string) []string {
	t.Helper()
	m, err := s.dal.GetMember(actor)
	if err != nil || m == nil {
		t.Fatalf("member %s: %v %v", actor, m, err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed carrier: %v", err)
		}
	}
	must(s.dal.PutRoleDef(RoleDef{RoleKey: m.RoleKey, Name: "Assistant",
		DefinitionMD: t6bd2Text(800)}))
	must(s.dal.PutInsight(Insight{RoleKey: m.RoleKey, Text: t6bd2Text(12500)}))
	must(s.dal.PutLessons(Lessons{RoleKey: m.RoleKey, TaskType: seedLessonsTaskType,
		Text: t6bd2Text(12500)}))
	must(s.dal.PutBootDocument(BootDocument{Kind: docKindSystemInteraction,
		Key: systemInteractionDocKey, Text: t6bd2Text(55000)}))
	must(s.dal.PutBootDocument(BootDocument{Kind: docKindOffboard,
		Key: offboardDocKey, Text: t6bd2Text(12500)}))
	must(s.dal.PutBootDocument(BootDocument{Kind: docKindBootSequence,
		Key: bootSequenceDocKey(m.Runtime), Text: t6bd2Text(12500)}))
	must(s.dal.PutTaskManual(TaskManual{TypeKey: "tm-t6bd2", DisplayName: "probe",
		Fields: "[]", Assignee: "{}", SopMD: t6bd2Text(12500),
		Learnings: t6bd2Text(12500)}))
	must(s.dal.PutTask(Task{ID: "t-t6bd2000001", TypeKey: "tm-t6bd2",
		Title: "probe", Status: TaskStatusInProgress, Priority: "mid", ExecutorID: actor,
		ExecutorKind: "member", CreatorID: wireOwnerID}))
	must(s.dal.PutTaskStep(TaskStep{ID: "ts-t6bd2000001", TaskID: "t-t6bd2000001",
		OrderIdx: 0, Name: "probe step", Status: StepStatusInProgress,
		Note: t6bd2Text(3200)}))
	return []string{
		"role definition", "insight", "role lessons",
		"system interaction", "offboard sequence", "boot sequence",
		"task manual SOP", "task manual learnings", "step note",
	}
}

// TestDocCapacityNear pins the threshold ON BOTH SIDES of every band boundary.
//
// 🔴 The pairs are one character apart. That is what kills the "wrong side of
// the comparison" mutant (`<` written as `<=`): a document sitting EXACTLY on
// its threshold still has the whole budgeted slack, so it must stay quiet, and
// only the character past it may speak.
func TestDocCapacityNear(t *testing.T) {
	cases := []struct {
		name          string
		capChars      int
		quietRemain   int // the largest remaining that must NOT fire
		speakingRemai int // one character less: must fire
	}{
		// 1000-char band: 25% left.
		{"duty", 1000, 250, 249},
		// <=15000 band: 20% left …
		{"insight", 15000, 3000, 2999},
		// … OR under 1000 outright, which is what makes the band right for the
		// 4000-char step note (20% of 4000 is 800 — barely one note).
		{"step note", 4000, 1000, 999},
		// >15000 band: 10% left.
		{"system interaction", 60000, 6000, 5999},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if docCapacityNear(c.capChars-c.quietRemain, c.capChars) {
				t.Fatalf("cap %d with %d chars left is ON the threshold, not past it — "+
					"it still has the full budgeted slack and must stay quiet",
					c.capChars, c.quietRemain)
			}
			if !docCapacityNear(c.capChars-c.speakingRemai, c.capChars) {
				t.Fatalf("cap %d with %d chars left is past the threshold and must fire",
					c.capChars, c.speakingRemai)
			}
		})
	}
	t.Run("a full document is still near", func(t *testing.T) {
		if !docCapacityNear(15000, 15000) {
			t.Fatal("a document at its cap must not fall out of the block")
		}
	})
	t.Run("no cap means no signal", func(t *testing.T) {
		if docCapacityNear(999999, 0) {
			t.Fatal("a document with no cap cannot be near one")
		}
	})
}

// TestResumeSummaryDocCapacityFiresForEveryCarrier — the POSITIVE direction, on
// all nine carriers at once.
//
// 🔴 It asserts EVERY carrier, not "at least one row". A mechanism wired to one
// document and silently blind to the other eight passes any weaker assertion,
// and blindness is exactly how this defect got here: the ticket was filed
// because one agent hit one cap, and the other eight fail identically and
// invisibly.
func TestResumeSummaryDocCapacityFiresForEveryCarrier(t *testing.T) {
	s := t6bd2Server(t)
	want := t6bd2FillAll(t, s, "mira")
	rows := t6bd2Capacity(t, s, "mira")
	for _, w := range want {
		t6bd2Row(t, rows, w)
	}
	if len(rows) != len(want) {
		t.Fatalf("want exactly %d rows (%v), got %d (%v)",
			len(want), want, len(rows), t6bd2Docs(rows))
	}
}

// TestResumeSummaryDocCapacityQuietWhenNothingIsNear — the REVERSE direction.
//
// A station whose documents all have room must carry NO block at all: not an
// empty array, no key. A block on every wake is a block every agent learns to
// scroll past, and then the wake that mattered looks like all the others.
func TestResumeSummaryDocCapacityQuietWhenNothingIsNear(t *testing.T) {
	s := t6bd2Server(t)
	rec := httptest.NewRecorder()
	s.HandleResumeSummaryApiResumeSummaryGet(rec,
		taskReq(t, "GET", "/api/resume-summary", nil, "mira", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("resume-summary: %d %s", rec.Code, rec.Body.String())
	}
	// The BLOCK — the rows — must be absent: not an empty array, no key.
	//
	// ⚠️ Scoped to `"doc_capacity":` rather than the bare word since T-6bd2's
	// peek fix. The overview now carries `doc_capacity_chars`, the addend of
	// estimated_total_chars that sizes this block, and it is present-and-0 on
	// every wake exactly like roster_chars / steps_on_answered_card_chars. That
	// is a SIZE, not the block: it adds nothing for an agent to scroll past, and
	// the peek needs it to exist unconditionally or its total would be a
	// different arithmetic on stations with room than on stations without. The
	// property this test guards — no rows, no key — is unchanged and still
	// checked, on the exact key that carries them.
	if strings.Contains(rec.Body.String(), `"doc_capacity":`) {
		t.Fatalf("nothing is near a cap, so the payload must not carry the block at all: %s",
			rec.Body.String())
	}
	// And one carrier comfortably below its threshold stays out on its own:
	// 12000 of 15000 leaves 3000, which is the boundary, not past it.
	if err := s.dal.PutInsight(Insight{RoleKey: "assistant", Text: t6bd2Text(12000)}); err != nil {
		t.Fatal(err)
	}
	if rows := t6bd2Capacity(t, s, "mira"); len(rows) != 0 {
		t.Fatalf("a document ON its threshold must stay quiet, got %v", t6bd2Docs(rows))
	}
}

// TestResumeSummaryDocCapacitySplitsWhatTheReaderCanActOn — the SECOND half of
// the AC. It used to say "the block must not tell a reader to do something that
// could only answer 403", and it enforced that by asserting insight and role
// lessons are NOT writable.
//
// 🔴 THAT WAS FALSE, AND THE TEST WAS PINNING THE FALSEHOOD. Measured
// 2026-08-20 with zero-damage probes (patch_* with an anchor that cannot exist,
// so the permission gate answers before anything is written):
//
//	patch_insight  role_key=<OWN role>     → 400 validation_error  ⇒ WRITABLE
//	patch_lessons  role_key=<OWN role>     → 400 validation_error  ⇒ WRITABLE
//	patch_insight  role_key=<ANOTHER role> → 403 (role-scoped refusal)
//	update_role    role=<any>              → 403 "principal not permitted"
//
// An agent CAN write its own role's insight and lessons. The signal was telling
// it "you cannot write this one (it answers 403 to you)" — a claim the reader
// falsifies in one call, and a reminder caught lying is a reminder nobody reads
// again. It was found by an agent that did exactly that, within seconds of
// receiving the notice.
//
// ⇒ The rows split THREE ways, not two, because "technically writable" and
// "yours to do right now" are different questions:
//
//	SELF    the reader's own working documents → rewrite it yourself
//	MEMORY  the reader's own long-term memory  → yours to write, but compacting
//	        it is not a close-out job; schedule it or ask the compactor
//	ASK     documents the reader genuinely cannot write → name who can
//
// Only ASK makes a permission claim, and it is the only class measured to
// deserve one.
func TestResumeSummaryDocCapacitySplitsWhatTheReaderCanActOn(t *testing.T) {
	s := t6bd2Server(t)
	t6bd2FillAll(t, s, "mira")
	rows := t6bd2Capacity(t, s, "mira")

	const (
		classSelf   = "self"
		classMemory = "memory"
		classAsk    = "ask"
	)
	class := map[string]string{
		"task manual SOP": classSelf, "task manual learnings": classSelf,
		"step note": classSelf,
		// Writable — measured, see the header.
		"insight": classMemory, "role lessons": classMemory,
		// Not writable — also measured, and for two DIFFERENT reasons: the role
		// definition is refused by a principal gate (not even the reader's own),
		// the boot documents are owner-only.
		"role definition":    classAsk,
		"system interaction": classAsk, "offboard sequence": classAsk,
		"boot sequence": classAsk,
	}
	for name, want := range class {
		row := t6bd2Row(t, rows, name)
		gotWritable := row["writable"].(bool)
		wantWritable := want != classAsk
		if gotWritable != wantWritable {
			t.Fatalf("%q: writable is a FACT about this reader's permissions and "+
				"must be %v, got %v — re-measure with a zero-damage probe before "+
				"changing this line", name, wantWritable, gotWritable)
		}

		action := row["action"].(string)
		namesTheCompactor := strings.Contains(action, docCapacityCompactor)

		// 🔴 THE GUARD THE OLD SHAPE COULD NOT EXPRESS: a row the reader CAN
		// write must not claim it cannot. Permission words belong only on rows
		// where they are true.
		if gotWritable {
			for _, lie := range []string{"403", "cannot write", "not yours to write"} {
				if strings.Contains(action, lie) {
					t.Fatalf("%q IS writable by this reader, so the row must not "+
						"say %q — that is a claim the reader falsifies in one "+
						"call: %q", name, lie, action)
				}
			}
		}

		switch want {
		case classSelf:
			if namesTheCompactor {
				t.Fatalf("%q is the reader's OWN working document — sending it to "+
					"someone else buys a round trip for a rewrite it could have "+
					"done: %q", name, action)
			}
		case classMemory:
			// It must offer the route without pretending the reader is barred
			// from it, and it must say why not now — compacting long-term memory
			// under close-out pressure is the very failure this feature answers.
			if !namesTheCompactor {
				t.Fatalf("%q should still offer the compactor as a route: %q", name, action)
			}
			if !strings.Contains(action, "yourself") {
				t.Fatalf("%q IS the reader's to write and the row must say so: %q",
					name, action)
			}
		case classAsk:
			if !namesTheCompactor {
				t.Fatalf("%q is not this reader's to write, so the row MUST name "+
					"who can, instead of asking for a write that cannot land: %q",
					name, action)
			}
		}
	}
}

// TestResumeSummaryDocCapacityCarriesTheArithmetic — the row has to be enough
// on its own. A reader that has to go and fetch the document to find out how
// much room is left is back where it started.
func TestResumeSummaryDocCapacityCarriesTheArithmetic(t *testing.T) {
	s := t6bd2Server(t)
	t6bd2FillAll(t, s, "mira")
	row := t6bd2Row(t, t6bd2Capacity(t, s, "mira"), "step note")
	if got := row["size_chars"].(float64); got != 3200 {
		t.Fatalf("size_chars: want the 3200 characters stored, got %v", got)
	}
	if got := row["cap_chars"].(float64); got != 4000 {
		t.Fatalf("cap_chars: want the step-note ceiling 4000, got %v", got)
	}
	if got := row["remaining_chars"].(float64); got != 800 {
		t.Fatalf("remaining_chars: want 4000-3200=800, got %v", got)
	}
}

// TestOffboardNoticeCarriesDocCapacity — the SECOND timing, and the one the
// ticket cares about most: writing memory back is step 4 of the offboard
// sequence, so the numbers have to be in the agent's hands BEFORE it enters
// that sequence, not when the write is refused inside it.
//
// 🔴 It asserts on the bytes of the DIRECTED FRAME, not on the decision struct.
// "The signal was composed but never reached anyone" is a real mutant and a
// struct-level assertion cannot tell it from a delivered one.
func TestOffboardNoticeCarriesDocCapacity(t *testing.T) {
	s := t6bd2Server(t)
	t6bd2FillAll(t, s, "mira")
	cfg := SseContextHighConfig{HandoverPct: 65, NoticePct: 55}
	gauge := map[string]any{"context_pct": 56.0}

	capacity := func() string {
		return docCapacityLines(s.docCapacityFor("mira", s.stepNoteCapacityFor("mira")))
	}
	sig := decideHandoverNotice("mira", RuntimeClaude, gauge, cfg, 5, 6,
		s.offboardText, capacity)
	if sig == nil {
		t.Fatal("the soft notice must fire at the notice pct")
	}
	frame, err := directedFrameText(contextHighTopic, sig)
	if err != nil {
		t.Fatalf("frame: %v", err)
	}
	wire := string(frame)
	for _, want := range []string{"step note", "task manual learnings", "insight"} {
		if !strings.Contains(wire, want) {
			t.Fatalf("the frame that actually goes out must name %q: %s", want, wire)
		}
	}
	// 🔴 T-a9d6's approved closing sentence must survive verbatim: the owner
	// settled on "work the sequence below, then call <closer> yourself" and this
	// ticket adds a block beside it, never rewrites it.
	if !strings.Contains(wire, "work the sequence below, then call restart_self yourself.") {
		t.Fatalf("the owner's approved sentence must be untouched: %s", wire)
	}
}

// TestOffboardNoticeSilentWhenNothingIsNear — the REVERSE direction on the
// second timing. On a station whose documents have room the notice must be
// byte-identical to what it was before this ticket.
func TestOffboardNoticeSilentWhenNothingIsNear(t *testing.T) {
	s := t6bd2Server(t)
	cfg := SseContextHighConfig{HandoverPct: 65, NoticePct: 55}
	gauge := map[string]any{"context_pct": 56.0}
	capacity := func() string {
		return docCapacityLines(s.docCapacityFor("mira", s.stepNoteCapacityFor("mira")))
	}
	with := decideHandoverNotice("mira", RuntimeClaude, gauge, cfg, 5, 6,
		s.offboardText, capacity)
	without := decideHandoverNotice("mira", RuntimeClaude, gauge, cfg, 5, 6,
		s.offboardText, nil)
	if with == nil || without == nil {
		t.Fatal("the soft notice must fire either way")
	}
	if with.Reason != without.Reason {
		t.Fatalf("nothing is near a cap, so the notice must be unchanged.\nwith:    %q\nwithout: %q",
			with.Reason, without.Reason)
	}
}

// TestStepNoteWritesReportTheirOwnRoom — the附帶 half of the ticket. The step
// note was the ONE carrier whose remaining room could not be computed from any
// read: the wholesale receipt omitted the pair and so did the get_task step
// view, so the only place the number ever appeared was the 400 that refused the
// write. Both faces must now carry it.
//
// ⚠️ It asserts the ceiling is REPORTED as 4000 — the ticket's 界線 is that the
// number does not move, so a change to it should turn this red.
func TestStepNoteWritesReportTheirOwnRoom(t *testing.T) {
	s := t6bd2Server(t)
	t6bd2FillAll(t, s, "mira")

	rec := httptest.NewRecorder()
	s.HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost(rec,
		taskReq(t, "POST", "/api/tasks/t-t6bd2000001/steps/ts-t6bd2000001/note",
			map[string]any{"note": t6bd2Text(3300)}, "mira", "agent"),
		"t-t6bd2000001", "ts-t6bd2000001")
	if rec.Code != http.StatusOK {
		t.Fatalf("wholesale note write: %d %s", rec.Code, rec.Body.String())
	}
	var receipt struct {
		SizeChars *int `json:"size_chars"`
		CapChars  *int `json:"cap_chars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &receipt); err != nil {
		t.Fatalf("decode receipt: %v", err)
	}
	if receipt.SizeChars == nil || *receipt.SizeChars != 3300 {
		t.Fatalf("the wholesale receipt must report the stored size (3300), got %v",
			receipt.SizeChars)
	}
	if receipt.CapChars == nil || *receipt.CapChars != 4000 {
		t.Fatalf("the wholesale receipt must report the ceiling (4000), got %v",
			receipt.CapChars)
	}

	rec = httptest.NewRecorder()
	s.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/t-t6bd2000001", nil, "mira", "agent"),
		"t-t6bd2000001")
	if rec.Code != http.StatusOK {
		t.Fatalf("get_task: %d %s", rec.Code, rec.Body.String())
	}
	var task struct {
		Steps []struct {
			NoteSizeChars *int `json:"note_size_chars"`
			NoteCapChars  *int `json:"note_cap_chars"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if len(task.Steps) != 1 {
		t.Fatalf("want one step, got %d", len(task.Steps))
	}
	st := task.Steps[0]
	if st.NoteSizeChars == nil || *st.NoteSizeChars != 3300 {
		t.Fatalf("the step view must report the note's size (3300), got %v", st.NoteSizeChars)
	}
	if st.NoteCapChars == nil || *st.NoteCapChars != 4000 {
		t.Fatalf("the step view must report the note's ceiling (4000), got %v", st.NoteCapChars)
	}
}
