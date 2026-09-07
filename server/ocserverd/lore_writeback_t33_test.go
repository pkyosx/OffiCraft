package main

// lore_writeback_t33_test.go — T-33: the 傳承 block must not be able to become
// document text.
//
// 🔴 THE BUG THIS GUARDS HAS ALREADY HAPPENED IN THIS TREE, in a smaller form:
// the 長期筆記 title self-heal in assets.go exists because a generation wrote its
// own boot-context fragment back through replace_lessons, and the drift was
// measured at +38 characters. The loop is: a reader is SERVED document + block,
// the documented procedure says read the latest and send back what changed, the
// block lands in the stored document, and the next read appends a fresh block
// after it. Nothing errors at any step.
//
// 🔴 THE REAL SYMPTOM IS GROWTH, so the load-bearing test here is the
// three-round-trip one: it asserts the stored document does not get longer, and
// it would go red for ANY implementation that lets one copy through — including
// one that strips only the first copy, or only on one of the write faces.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// readManualLearnings returns what GET /api/task-manuals/{type_key} SERVES as
// `learnings`, and readManualLore returns what it serves as `lore`.
//
// 🔴 THESE ARE TWO FIELDS SINCE 2026-09-07, and that is the whole point of the
// tests below. Lore used to be appended onto `learnings`, so the string an
// agent held after a read was document+block and writing it back stored the
// block — the read/write growth this file exists for. Now `learnings` is the
// stored document alone and the block rides `lore`, so the growth loop has no
// way to start. The write-face strip guards stay anyway: they cover a caller
// that concatenates the two itself, or a human who pastes a block in.
func readManualFields(t *testing.T, s *apiServer, typeKey string) (learnings, lore string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleGetTaskManualApiTaskManualsTypeKeyGet(rec,
		taskReq(t, "GET", "/api/task-manuals/"+typeKey, nil, "m-x", "agent"), typeKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("get manual: %d %s", rec.Code, rec.Body.String())
	}
	var dto struct {
		Learnings string `json:"learnings"`
		Lore      string `json:"lore"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return dto.Learnings, dto.Lore
}

func storedManualLearnings(t *testing.T, s *apiServer, typeKey string) string {
	t.Helper()
	m, err := s.dal.GetTaskManual(typeKey)
	if err != nil || m == nil {
		t.Fatalf("GetTaskManual: %v / %v", m, err)
	}
	return m.Learnings
}

// TestManualLearningsDoNotGrowAcrossReadWriteCycles is the symptom test.
//
// It plays the documented procedure three times: read the manual (which serves
// learnings WITH the appended 傳承 block), send the whole thing back through
// write_task_learnings. If any copy of the block survives into the column, the
// stored text is longer after round 2 than after round 1 and this fails.
//
// 🔴 THREE ROUNDS, NOT ONE. One round would pass against an implementation that
// strips exactly one trailing block; the growth bug is that each round adds
// another, so the assertion is on the whole series being equal.
func TestManualLearningsDoNotGrowAcrossReadWriteCycles(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-wb-1", "researcher")
	if err := s.dal.PutTaskManual(TaskManual{
		TypeKey: "tm-grow", DisplayName: "Grow", Learnings: "原本的學習內容",
		UpdatedTS: 1,
	}); err != nil {
		t.Fatalf("PutTaskManual: %v", err)
	}
	typed, err := s.dal.CreateTaskMintingID(Task{
		Title: "t", TypeKey: "tm-grow", ExecutorKind: TaskExecutorStaff,
		ExecutorID: me, CreatorID: me, Priority: TaskPriorityMid,
		CreatedTS: 1, UpdatedTS: 1,
	}, nil)
	if err != nil {
		t.Fatalf("CreateTaskMintingID: %v", err)
	}
	if rec := postLore(t, s, me, map[string]any{
		"title": "傳承標題", "body": "傳承內容", "task_id": typed.ID,
	}); rec.Code != http.StatusOK {
		t.Fatalf("seed lore: %d %s", rec.Code, rec.Body.String())
	}

	sizes := make([]int, 0, 3)
	for round := 0; round < 3; round++ {
		served, lore := readManualFields(t, s, "tm-grow")

		// FIXTURE GUARD, kept and re-aimed. There really is a lore block on
		// this manual to be confused about — if there were not, everything
		// below would pass without touching the thing it is about.
		if !strings.Contains(lore, loreBlockHeading) {
			t.Fatalf("round %d: the read face is not serving a 傳承 block on `lore` at "+
				"all, so this test is not exercising the loop it exists for: %q",
				round, lore)
		}

		// 🔴 THE ASSERTION THAT REPLACED THE OLD ONE, and it is stronger. The
		// old shape served document+block on one field and this test proved
		// the round trip did not GROW the stored document. Now the two are
		// separate fields, so the honest question is not "does it grow" but
		// "did they stay separate" — a regression that re-appended the block
		// would put it back here, and the growth check below would go red one
		// round later for a reason nothing named.
		if strings.Contains(served, loreBlockHeading) {
			t.Fatalf("round %d: `learnings` carries the 傳承 block again — the two "+
				"fields were split precisely so a reader holding `learnings` holds "+
				"only the stored document: %q", round, served)
		}

		rec := httptest.NewRecorder()
		s.HandleWriteTaskLearningsApiTaskManualsTypeKeyLearningsPost(rec,
			taskReq(t, "POST", "/api/task-manuals/tm-grow/learnings",
				map[string]any{"text": served}, me, "agent"), "tm-grow")
		if rec.Code != http.StatusOK {
			t.Fatalf("round %d write: %d %s", round, rec.Code, rec.Body.String())
		}
		sizes = append(sizes, utf8.RuneCountInString(storedManualLearnings(t, s, "tm-grow")))
	}
	if sizes[0] != sizes[1] || sizes[1] != sizes[2] {
		t.Fatalf("the stored learnings document GREW across read/write rounds: %v "+
			"characters. The 傳承 block is being written back into the document and "+
			"a fresh one appended on top of it every read — the same shape as the "+
			"+38-character boot-fragment drift the 長期筆記 title self-heal exists for.",
			sizes)
	}
	if got := storedManualLearnings(t, s, "tm-grow"); got != "原本的學習內容" {
		t.Fatalf("stored learnings = %q, want the original document verbatim", got)
	}
}

// TestEveryLearningsWriteFaceStripsTheLoreBlock walks the manual's write faces
// one by one. A single shared choke point is not enough on its own to make this
// pass — each face measures its own cap and answers its own receipt — and doing
// them separately is what makes a partial fix visible.
func TestEveryLearningsWriteFaceStripsTheLoreBlock(t *testing.T) {
	poisoned := "真正的學習內容\n\n" + loreBlockHeading + "\n\n## L-1 標題\n\n內容"

	t.Run("write_task_learnings", func(t *testing.T) {
		s := loreTestServer(t)
		if err := s.dal.PutTaskManual(TaskManual{
			TypeKey: "tm-a", DisplayName: "A", UpdatedTS: 1}); err != nil {
			t.Fatalf("PutTaskManual: %v", err)
		}
		rec := httptest.NewRecorder()
		s.HandleWriteTaskLearningsApiTaskManualsTypeKeyLearningsPost(rec,
			taskReq(t, "POST", "/api/task-manuals/tm-a/learnings",
				map[string]any{"text": poisoned}, "m-x", "agent"), "tm-a")
		if rec.Code != http.StatusOK {
			t.Fatalf("write: %d %s", rec.Code, rec.Body.String())
		}
		if got := storedManualLearnings(t, s, "tm-a"); got != "真正的學習內容" {
			t.Fatalf("stored = %q, want the document with the 傳承 block stripped", got)
		}
		// The receipt must describe what was STORED, not what was sent: an anchor
		// that measures text the server threw away cannot verify anything.
		var receipt struct {
			SizeChars int `json:"size_chars"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &receipt); err != nil {
			t.Fatalf("decode receipt: %v", err)
		}
		if want := utf8.RuneCountInString("真正的學習內容"); receipt.SizeChars != want {
			t.Fatalf("receipt size_chars = %d, want %d — the receipt must measure "+
				"the stored text", receipt.SizeChars, want)
		}
	})

	t.Run("update_task_manual", func(t *testing.T) {
		s := loreTestServer(t)
		if err := s.dal.PutTaskManual(TaskManual{
			TypeKey: "tm-b", DisplayName: "B", UpdatedTS: 1}); err != nil {
			t.Fatalf("PutTaskManual: %v", err)
		}
		rec := httptest.NewRecorder()
		s.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec,
			taskReq(t, "POST", "/api/task-manuals/tm-b",
				map[string]any{"learnings": poisoned}, "m-x", "agent"), "tm-b")
		if rec.Code != http.StatusOK {
			t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
		}
		if got := storedManualLearnings(t, s, "tm-b"); got != "真正的學習內容" {
			t.Fatalf("stored = %q, want the document with the 傳承 block stripped", got)
		}
	})

	t.Run("patch_task_learnings", func(t *testing.T) {
		s := loreTestServer(t)
		if err := s.dal.PutTaskManual(TaskManual{
			TypeKey: "tm-c", DisplayName: "C", Learnings: "真正的學習內容",
			UpdatedTS: 1}); err != nil {
			t.Fatalf("PutTaskManual: %v", err)
		}
		// An APPEND edit (empty `old`) carrying the block the caller pasted back.
		rec := httptest.NewRecorder()
		s.HandlePatchTaskLearningsApiTaskManualsTypeKeyLearningsPatchPost(rec,
			taskReq(t, "POST", "/api/task-manuals/tm-c/learnings/patch",
				map[string]any{"edits": []map[string]any{
					{"old": "", "new": "\n\n" + loreBlockHeading + "\n\n## L-1 標題\n\n內容"},
				}}, "m-x", "agent"), "tm-c")
		if rec.Code != http.StatusOK {
			t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
		}
		if got := storedManualLearnings(t, s, "tm-c"); got != "真正的學習內容" {
			t.Fatalf("stored = %q, want the document with the appended 傳承 block "+
				"stripped from the patch RESULT", got)
		}
	})
}

// TestLessonsWriteFacesStripTheLoreBlock — the role side. The boot document is
// what serves the block there, and replace_lessons / patch_lessons are the two
// doors an agent merges it back through.
func TestLessonsWriteFacesStripTheLoreBlock(t *testing.T) {
	poisoned := "真正的長期筆記\n\n" + loreBlockHeading + "\n\n## L-1 標題\n\n內容"

	t.Run("replace_lessons", func(t *testing.T) {
		s := loreTestServer(t)
		hireLoreStaff(t, s, "m-wb-2", defaultBootRole)
		rec := httptest.NewRecorder()
		s.HandleReplaceLessonsApiLessonsRoleKeyPost(rec,
			taskReq(t, "POST", "/api/lessons/"+defaultBootRole,
				map[string]any{"text": poisoned}, "owner", "owner"), defaultBootRole)
		if rec.Code != http.StatusOK {
			t.Fatalf("replace: %d %s", rec.Code, rec.Body.String())
		}
		got, err := s.dal.GetLessons(defaultBootRole)
		if err != nil || got == nil {
			t.Fatalf("GetLessons: %v / %v", got, err)
		}
		if got.Text != "真正的長期筆記" {
			t.Fatalf("stored = %q, want the document with the 傳承 block stripped",
				got.Text)
		}
	})

	t.Run("the DAL backstop catches a write face that forgets", func(t *testing.T) {
		// 🔴 This one goes through the DAL directly, which is what a NEW write
		// face added later would do without knowing about any of this. The
		// handler-side strip keeps the receipts honest; this layer is what keeps
		// the column clean when somebody forgets.
		s := loreTestServer(t)
		if err := s.dal.PutLessons(Lessons{
			RoleKey: defaultBootRole, Text: poisoned}); err != nil {
			t.Fatalf("PutLessons: %v", err)
		}
		got, err := s.dal.GetLessons(defaultBootRole)
		if err != nil || got == nil {
			t.Fatalf("GetLessons: %v / %v", got, err)
		}
		if got.Text != "真正的長期筆記" {
			t.Fatalf("the DAL stored the 傳承 block: %q", got.Text)
		}
	})
}

// TestStripTrailingLoreBlockIsIdempotentAndHealsAccumulatedCopies covers the
// dirty data that already exists by the time the fix lands: a document that has
// been through the loop several times carries several copies, and one write
// must clean all of them rather than one per write.
func TestStripTrailingLoreBlockIsIdempotentAndHealsAccumulatedCopies(t *testing.T) {
	block := "\n\n" + loreBlockHeading + "\n\n## L-1 標題\n\n內容"
	for _, tc := range []struct{ name, in, want string }{
		{"clean text is untouched", "真正的內容", "真正的內容"},
		{"one copy", "真正的內容" + block, "真正的內容"},
		{"three accumulated copies", "真正的內容" + block + block + block, "真正的內容"},
		{"the whole document is the block", strings.TrimLeft(block, "\n"), ""},
		{"empty stays empty", "", ""},
		{
			// A heading that merely STARTS with the same runes is a different
			// heading and must survive — otherwise the stripper eats real content
			// whose section happens to be named 傳承的由來.
			"a longer heading is not the block",
			"真正的內容\n\n" + loreBlockHeading + "的由來\n\n這段要留著",
			"真正的內容\n\n" + loreBlockHeading + "的由來\n\n這段要留著",
		},
	} {
		if got := stripTrailingLoreBlock(tc.in); got != tc.want {
			t.Fatalf("%s: stripTrailingLoreBlock(%q) = %q, want %q",
				tc.name, tc.in, got, tc.want)
		}
		// Idempotent: a second pass changes nothing.
		if got := stripTrailingLoreBlock(stripTrailingLoreBlock(tc.in)); got != tc.want {
			t.Fatalf("%s: not idempotent", tc.name)
		}
	}
}

// TestWipeGuardJudgesTheStrippedTextNotWhatWasSent pins the ORDER of the two
// guards on write_task_learnings, which is the only thing standing between an
// agent's honest mistake and a real document being erased.
//
// 🔴 THE FAILURE THIS CATCHES IS SILENT AND IT ANSWERS 200. An agent reads
// get_task_manual, sees the new `lore` field, and writes that block back as
// `learnings` — the exact loop stripTrailingLoreBlock exists to close. If the
// wipe guard is measured on the UNSTRIPPED body it sees a non-empty string and
// lets it through; the strip then reduces it to "" and the manual's accumulated
// learnings are gone. Nothing is refused, nothing is logged, and the only trace
// is `size_chars: 0` in a receipt nobody reads.
//
// The sibling face gets this right (api_roles.go strips BEFORE the wipe guard
// and says so in a comment). This test is here because the two faces disagreed
// and only one of them was guarded.
//
// ⚠️ The fixture MUST start with a NON-EMPTY learnings document. An empty one
// never trips the wipe guard at all, so the same test over an empty manual
// passes whichever order the code uses — that is why the existing writeback
// tests did not catch this.
func TestWipeGuardJudgesTheStrippedTextNotWhatWasSent(t *testing.T) {
	s := loreTestServer(t)
	const original = "既有的重要 learnings，不該被一次寫入抹掉。"
	if err := s.dal.PutTaskManual(TaskManual{
		TypeKey: "tm-wipe", DisplayName: "W", Learnings: original, UpdatedTS: 1}); err != nil {
		t.Fatalf("PutTaskManual: %v", err)
	}
	// Nothing but a 傳承 block: strips down to the empty string.
	onlyLore := loreBlockHeading + "\n\n## L-1 標題\n\n內容"

	rec := httptest.NewRecorder()
	s.HandleWriteTaskLearningsApiTaskManualsTypeKeyLearningsPost(rec,
		taskReq(t, "POST", "/api/task-manuals/tm-wipe/learnings",
			map[string]any{"text": onlyLore}, "m-x", "agent"), "tm-wipe")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("write that strips to empty answered %d %s — want 400: the wipe guard must "+
			"judge the text that will be STORED, not the text that was sent",
			rec.Code, rec.Body.String())
	}
	if got := storedManualLearnings(t, s, "tm-wipe"); got != original {
		t.Fatalf("stored = %q, want the original document untouched (%q)", got, original)
	}
}
