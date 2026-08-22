package main

// api_chat_resume_wire_t3201_test.go — the wake snapshot no longer carries a
// near-cap document block (T-3201; owner ruling rc-5d06304ca54b, 2026-08-22).
//
// The removed block re-reported, on the READ side, what the five long-lived
// documents' own read faces already carry: every one of them reports its
// size_chars and cap_chars, and every write face repeats the pair on its
// receipt. So the reminder was a third copy of numbers the agent already had
// before it could touch any of those documents.
//
// 🔴 WHY THIS FILE EXISTS RATHER THAN NOTHING. "The key is gone" is exactly the
// property a deletion cannot leave to chance: `doc_capacity` was `omitempty`,
// so on an ordinary station it was absent BEFORE the removal too. A test that
// merely read an empty station would have passed against the old code and
// proved nothing. Every assertion below therefore runs against a station whose
// documents are stuffed to just under their caps — the fixture that USED to
// make the block appear — and is checked on the RAW JSON, because a Go struct
// with no such field cannot decode a key that is still on the wire.

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// t3201NearCap stuffs every document the removed block used to watch to just
// under its own cap: role definition, insight, role lessons, the three boot
// documents, an open task's manual (SOP + learnings) and an open step's note.
func t3201NearCap(t *testing.T, s *apiServer, actor string) {
	t.Helper()
	m, err := s.dal.GetMember(actor)
	if err != nil || m == nil {
		t.Fatalf("member %s: %v %v", actor, m, err)
	}
	fill := func(n int) string { return strings.Repeat("x", n) }
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed carrier: %v", err)
		}
	}
	must(s.dal.PutRoleDef(RoleDef{RoleKey: m.RoleKey, Name: "Assistant",
		DefinitionMD: fill(800)}))
	must(s.dal.PutInsight(Insight{RoleKey: m.RoleKey, Text: fill(12500)}))
	must(s.dal.PutLessons(Lessons{RoleKey: m.RoleKey, TaskType: seedLessonsTaskType,
		Text: fill(12500)}))
	must(s.dal.PutBootDocument(BootDocument{Kind: docKindSystemInteraction,
		Key: systemInteractionDocKey, Text: fill(55000)}))
	must(s.dal.PutBootDocument(BootDocument{Kind: docKindOffboard,
		Key: offboardDocKey, Text: fill(12500)}))
	must(s.dal.PutBootDocument(BootDocument{Kind: docKindBootSequence,
		Key: bootSequenceDocKey(m.Runtime), Text: fill(12500)}))
	must(s.dal.PutTaskManual(TaskManual{TypeKey: "tm-t3201", DisplayName: "probe",
		Fields: "[]", Assignee: "{}", SopMD: fill(12500), Learnings: fill(12500)}))
	must(s.dal.PutTask(Task{ID: "t-t3201000001", TypeKey: "tm-t3201",
		Title: "probe", Status: TaskStatusInProgress, Priority: "mid", ExecutorID: actor,
		ExecutorKind: "member", CreatorID: wireOwnerID}))
	must(s.dal.PutTaskStep(TaskStep{ID: "ts-t3201000001", TaskID: "t-t3201000001",
		OrderIdx: 0, Name: "probe step", Status: StepStatusInProgress, Note: fill(3200)}))
}

// t3201Keys decodes the body as a bare object and returns its top-level keys
// plus the overview's, so absence is asserted against the key set that actually
// arrived rather than against a substring of the whole document.
func t3201Keys(t *testing.T, body []byte) (map[string]json.RawMessage, map[string]json.RawMessage) {
	t.Helper()
	top := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("decode: %v", err)
	}
	overview := map[string]json.RawMessage{}
	raw, ok := top["overview"]
	if !ok {
		t.Fatal("every resume face carries an overview block; without it this " +
			"test is reading something other than the payload it names")
	}
	if err := json.Unmarshal(raw, &overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	return top, overview
}

// TestResumeFacesCarryNoDocumentCapacityBlock pins the removal on all three
// faces that used to serve it, on a station where the block WOULD have fired.
func TestResumeFacesCarryNoDocumentCapacityBlock(t *testing.T) {
	s := newTasksTestServer(t)
	seedMachine(t, s, "m-host-one")
	if err := s.dal.PutMember(Member{ID: "m-reader", Name: "Reader",
		Kind: KindAssistant, RoleKey: "r-t3201", Runtime: RuntimeClaude,
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive}); err != nil {
		t.Fatal(err)
	}
	t3201NearCap(t, s, "m-reader")

	for _, face := range []struct {
		name string
		call func() *httptest.ResponseRecorder
	}{
		{"GET /api/resume-summary", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			s.HandleResumeSummaryApiResumeSummaryGet(rec, perfReq("m-reader", "agent"))
			return rec
		}},
		{"GET /api/members/{id}/resume-summary", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			s.HandleGetMemberResumeSummaryApiMembersMemberIdResumeSummaryGet(
				rec, perfReq(wireOwnerID, "owner"), "m-reader")
			return rec
		}},
		{"GET /api/resume-summary-size", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			s.HandlePeekResumeSummarySizeApiResumeSummarySizeGet(rec, perfReq("m-reader", "agent"))
			return rec
		}},
	} {
		t.Run(face.name, func(t *testing.T) {
			rec := face.call()
			if rec.Code != 200 {
				t.Fatalf("%s → %d: %s", face.name, rec.Code, rec.Body.String())
			}
			top, overview := t3201Keys(t, rec.Body.Bytes())
			// The positive control: the blocks that DID survive are still here,
			// so an empty or errored payload cannot masquerade as a green.
			for _, k := range []string{"chat_chars", "tasks_detail_chars",
				"roster_chars", "machines_chars", "steps_on_answered_card_chars"} {
				if _, ok := overview[k]; !ok {
					t.Fatalf("overview lost %s — this test is no longer reading a "+
						"real wake snapshot, so its absence checks prove nothing", k)
				}
			}
			if _, ok := top["doc_capacity"]; ok {
				t.Error("the wake snapshot must no longer carry doc_capacity")
			}
			if _, ok := overview["doc_capacity_chars"]; ok {
				t.Error("the overview must no longer carry doc_capacity_chars")
			}
		})
	}
}

// TestPeekTotalIsTheFiveBlocksThePayloadCarries keeps the removal honest in the
// other direction: dropping a field from the wire while leaving its value in
// the derived total would make the boot threshold gate on a number nothing
// explains. The expectation is summed off the wire, never read off the server.
func TestPeekTotalIsTheFiveBlocksThePayloadCarries(t *testing.T) {
	s := newTasksTestServer(t)
	seedMachine(t, s, "m-host-one")
	if err := s.dal.PutMember(Member{ID: "m-reader", Name: "Reader",
		Kind: KindAssistant, RoleKey: "r-t3201", Runtime: RuntimeClaude,
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive}); err != nil {
		t.Fatal(err)
	}
	t3201NearCap(t, s, "m-reader")

	rec := httptest.NewRecorder()
	s.HandlePeekResumeSummarySizeApiResumeSummarySizeGet(rec, perfReq("m-reader", "agent"))
	if rec.Code != 200 {
		t.Fatalf("peek → %d: %s", rec.Code, rec.Body.String())
	}
	var peek struct {
		Overview            map[string]float64 `json:"overview"`
		EstimatedTotalChars int                `json:"estimated_total_chars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &peek); err != nil {
		t.Fatalf("decode peek: %v", err)
	}
	sum := 0
	for _, k := range []string{"chat_chars", "tasks_detail_chars", "roster_chars",
		"machines_chars", "steps_on_answered_card_chars"} {
		v, ok := peek.Overview[k]
		if !ok {
			t.Fatalf("overview is missing %s", k)
		}
		sum += int(v)
	}
	if peek.EstimatedTotalChars != sum {
		t.Fatalf("estimated_total_chars must be the sum of the five blocks the "+
			"payload carries or omits: got %d, the five add to %d",
			peek.EstimatedTotalChars, sum)
	}
	if sum == 0 {
		t.Fatal("the five addends sum to 0 on a fully-loaded fixture — the " +
			"equality above would hold for a server that computes nothing")
	}
}
