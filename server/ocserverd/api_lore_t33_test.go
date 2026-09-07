package main

// api_lore_t33_test.go — T-33: the write face's two refusals, and the TWO
// EXITS.
//
// 🔴 THE TWO EXITS ARE ASSERTED ON THE ASSEMBLED OUTPUT, not by calling
// selectLoreForScope again and comparing. A test that re-runs the production
// function and compares the result to itself is green for every possible
// implementation of that function, which is the shape
// TestMemberBootContextByteIdenticalToSpecAssembly fell into: it rebuilds its
// own expectation, so it asserts nothing about the two halves being equal.
// Here the expectation is a LITERAL string that has to appear (or not appear)
// in the document the server actually produced.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func loreTestServer(t *testing.T) *apiServer {
	t.Helper()
	return newTasksTestServer(t)
}

// hireLoreStaff puts a staff member with a role on the roster and returns its
// id — the identity a role-scoped write is filed under.
func hireLoreStaff(t *testing.T, s *apiServer, id, roleKey string) string {
	t.Helper()
	if err := s.dal.PutMember(Member{
		ID: id, Name: "Lore Writer", Kind: KindStaff, RoleKey: roleKey,
		Runtime: RuntimeClaude, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	return id
}

func postLore(t *testing.T, s *apiServer, sub string, body any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleWriteLoreEntryApiLorePost(rec,
		taskReq(t, "POST", "/api/lore", body, sub, "agent"))
	return rec
}

// TestWriteLoreWithNoTaskFilesUnderTheCallersOwnRole — the ROLE arm, and the
// scope key comes from the ROSTER, never from the request.
func TestWriteLoreWithNoTaskFilesUnderTheCallersOwnRole(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-1", "researcher")

	rec := postLore(t, s, me, map[string]any{"title": "一件事", "body": "內容"})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var dto LoreEntryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.ScopeKind != LoreScopeRole || dto.ScopeKey != "researcher" {
		t.Fatalf("scope = %s/%s, want role/researcher", dto.ScopeKind, dto.ScopeKey)
	}
	if dto.AuthorId != me {
		t.Fatalf("author_id = %q, want %q — the writer is PINNED at write time",
			dto.AuthorId, me)
	}
	if dto.State != LoreStateActive {
		t.Fatalf("state = %q, want %q", dto.State, LoreStateActive)
	}
	if dto.CreatedTs != dto.EffectiveTs {
		t.Fatalf("created_ts %v != effective_ts %v — they start equal",
			dto.CreatedTs, dto.EffectiveTs)
	}
}

// TestWriteLoreWithNoRoleIs400 — a caller with no role has no 角色傳承 to write
// into. The refusal must NAME that, not fall through to some default role.
func TestWriteLoreWithNoRoleIs400(t *testing.T) {
	s := loreTestServer(t)
	if err := s.dal.PutMember(Member{
		ID: "ow-lore-1", Name: "Contractor", Kind: KindOutsource, RoleKey: "",
		Runtime: RuntimeClaude, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}

	rec := postLore(t, s, "ow-lore-1", map[string]any{"title": "x", "body": "y"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d %s", rec.Code, rec.Body.String())
	}
	// And NOTHING was written anywhere — in particular, not under a default role.
	page, err := s.dal.ListLoreEntriesPage(loreListFilter{}, 30, 0)
	if err != nil {
		t.Fatalf("ListLoreEntriesPage: %v", err)
	}
	if len(page) != 0 {
		t.Fatalf("a refused write stored %d entries: %+v", len(page), page)
	}
}

// TestWriteLoreAgainstAnUntypedTaskIs400AndIsNotFiledUnderTheRole is the
// branch the spec calls out in red: a 臨時任務 has no type, and the lesson must
// NOT be filed under the caller's role instead.
//
// 🔴 THE SECOND HALF OF THIS TEST IS THE POINT. A 400 alone would still pass
// against an implementation that answered 400 and wrote a role entry anyway,
// and a 200-with-role-fallback is the exact bug: every boot of that role would
// pay for the entry, the task type that needed it would never see it, and
// nothing would report anything. So the assertion is BOTH the status AND that
// the role scope is still empty.
func TestWriteLoreAgainstAnUntypedTaskIs400AndIsNotFiledUnderTheRole(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-2", "researcher")

	adhoc, err := s.dal.CreateTaskMintingID(Task{
		Title: "一張臨時任務", TypeKey: "", ExecutorKind: TaskExecutorStaff,
		ExecutorID: me, CreatorID: me, Priority: TaskPriorityMid,
		CreatedTS: 1, UpdatedTS: 1,
	}, nil)
	if err != nil {
		t.Fatalf("CreateTaskMintingID: %v", err)
	}

	rec := postLore(t, s, me, map[string]any{
		"title": "一件事", "body": "內容", "task_id": adhoc.ID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for a task with no type_key, got %d %s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "沒有類型") {
		t.Fatalf("the refusal must say the task has no type; got %s", rec.Body.String())
	}

	role, err := s.dal.ListLoreEntriesLive(LoreScopeRole, "researcher")
	if err != nil {
		t.Fatalf("ListLoreEntriesLive: %v", err)
	}
	if len(role) != 0 {
		t.Fatalf("an untyped task's entry was SILENTLY filed under the caller's "+
			"role: %+v — that charges every boot of that role for a lesson the "+
			"task type never receives", role)
	}
}

// TestWriteLoreAgainstATypedTaskFilesUnderThatType — the MANUAL arm.
func TestWriteLoreAgainstATypedTaskFilesUnderThatType(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-3", "researcher")
	typed, err := s.dal.CreateTaskMintingID(Task{
		Title: "一張有類型的任務", TypeKey: "tm-review", ExecutorKind: TaskExecutorStaff,
		ExecutorID: me, CreatorID: me, Priority: TaskPriorityMid,
		CreatedTS: 1, UpdatedTS: 1,
	}, nil)
	if err != nil {
		t.Fatalf("CreateTaskMintingID: %v", err)
	}

	rec := postLore(t, s, me, map[string]any{
		"title": "一件事", "body": "內容", "task_id": typed.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var dto LoreEntryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.ScopeKind != LoreScopeManual || dto.ScopeKey != "tm-review" {
		t.Fatalf("scope = %s/%s, want manual/tm-review", dto.ScopeKind, dto.ScopeKey)
	}
	if dto.SourceTaskId != typed.ID {
		t.Fatalf("source_task_id = %q, want %q", dto.SourceTaskId, typed.ID)
	}
	// The role scope stays empty: the two are not interchangeable.
	role, err := s.dal.ListLoreEntriesLive(LoreScopeRole, "researcher")
	if err != nil {
		t.Fatalf("ListLoreEntriesLive: %v", err)
	}
	if len(role) != 0 {
		t.Fatalf("a manual entry leaked into the role scope: %+v", role)
	}
}

// TestWriteLoreOverCapWritesNothing — the cap refusal writes NOTHING and does
// not truncate. Both caps get their own arm.
func TestWriteLoreOverCapWritesNothing(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-4", "researcher")
	s.loreCapCharsTitle = 5
	s.loreCapCharsBody = 5

	for _, tc := range []struct{ name, title, body string }{
		{"title", "123456", "ok"},
		{"body", "ok", "123456"},
	} {
		rec := postLore(t, s, me, map[string]any{"title": tc.title, "body": tc.body})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}
	page, err := s.dal.ListLoreEntriesPage(loreListFilter{}, 30, 0)
	if err != nil {
		t.Fatalf("ListLoreEntriesPage: %v", err)
	}
	if len(page) != 0 {
		t.Fatalf("an over-cap write stored something (possibly truncated): %+v", page)
	}
}

// TestSetLoreStateClearsTheReasonOnTheWayBack — a reason belongs to `retired`
// alone. Bringing an entry back must not leave the explanation for a
// retirement that was undone attached to a live entry.
func TestSetLoreStateClearsTheReasonOnTheWayBack(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-5", "researcher")
	rec := postLore(t, s, me, map[string]any{"title": "一件事", "body": "內容"})
	if rec.Code != http.StatusOK {
		t.Fatalf("seed write: %d %s", rec.Code, rec.Body.String())
	}
	var seeded LoreEntryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &seeded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	rec = httptest.NewRecorder()
	s.HandleSetLoreEntryStateApiLoreEntryIdStatePost(rec,
		taskReq(t, "POST", "/api/lore/"+seeded.Id+"/state",
			map[string]any{"state": LoreStateRetired, "retire_reason": "已被取代"},
			me, "agent"), seeded.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("retire: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.HandleSetLoreEntryStateApiLoreEntryIdStatePost(rec,
		taskReq(t, "POST", "/api/lore/"+seeded.Id+"/state",
			map[string]any{"state": LoreStatePinned}, me, "agent"), seeded.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("pin: %d %s", rec.Code, rec.Body.String())
	}
	var after LoreEntryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if after.State != LoreStatePinned {
		t.Fatalf("state = %q, want %q", after.State, LoreStatePinned)
	}
	if after.RetireReason != "" {
		t.Fatalf("retire_reason = %q on a %s entry — a live entry must not carry "+
			"the explanation for a retirement that was undone",
			after.RetireReason, after.State)
	}
}

// TestSetLoreStateRejectsAnUnknownState — the closed set is enforced at the
// door, not only by the schema CHECK (which would surface as a 500).
func TestSetLoreStateRejectsAnUnknownState(t *testing.T) {
	s := loreTestServer(t)
	rec := httptest.NewRecorder()
	s.HandleSetLoreEntryStateApiLoreEntryIdStatePost(rec,
		taskReq(t, "POST", "/api/lore/L-1/state",
			map[string]any{"state": "archived"}, "m-x", "agent"), "L-1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for an unknown state, got %d %s", rec.Code, rec.Body.String())
	}
}

// ── the two exits ───────────────────────────────────────────────────────────

// TestRoleLoreLandsInTheStaffBootDocumentAfterTheLessons is EXIT 1, asserted on
// the assembled document: the literal entry text must be IN it, and it must sit
// AFTER the 長期筆記 heading and BEFORE the boot-sequence tail.
func TestRoleLoreLandsInTheStaffBootDocumentAfterTheLessons(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-6", defaultBootRole)
	rec := postLore(t, s, me, map[string]any{
		"title": "傳承標題甲", "body": "傳承內容甲"})
	if rec.Code != http.StatusOK {
		t.Fatalf("seed write: %d %s", rec.Code, rec.Body.String())
	}

	m, err := s.dal.GetMember(me)
	if err != nil || m == nil {
		t.Fatalf("GetMember: %v / %v", m, err)
	}
	ctx, err := s.buildBootContext("", m)
	if err != nil || ctx == nil {
		t.Fatalf("buildBootContext: %v / %v", ctx, err)
	}

	doc := ctx.Context
	lessonsAt := strings.Index(doc, "# Lessons ("+defaultBootRole+")")
	loreAt := strings.Index(doc, loreBlockHeading)
	titleAt := strings.Index(doc, "傳承標題甲")
	bodyAt := strings.Index(doc, "傳承內容甲")
	if lessonsAt < 0 {
		t.Fatalf("the boot document has no 長期筆記 block at all:\n%s", doc)
	}
	if loreAt < 0 || titleAt < 0 || bodyAt < 0 {
		t.Fatalf("the entry did not reach the boot document (lore=%d title=%d body=%d):\n%s",
			loreAt, titleAt, bodyAt, doc)
	}
	if loreAt < lessonsAt {
		t.Fatalf("傳承 was assembled BEFORE 長期筆記 (lore=%d lessons=%d) — the spec "+
			"puts it after", loreAt, lessonsAt)
	}
}

// TestRoleLoreDoesNotReachTheOutsourceBootContext is the other half of exit 1,
// and it is what makes 「不要動外包那條路」 checkable rather than a claim in a
// commit message. The worker fold is the staff fold MINUS the persona; a role's
// traditions are part of the persona, so they must not appear.
func TestRoleLoreDoesNotReachTheOutsourceBootContext(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-7", defaultBootRole)
	if rec := postLore(t, s, me, map[string]any{
		"title": "傳承標題乙", "body": "傳承內容乙"}); rec.Code != http.StatusOK {
		t.Fatalf("seed write: %d %s", rec.Code, rec.Body.String())
	}

	worker := OutsourceWorker{ID: "ow-lore-7", Runtime: RuntimeClaude}
	ctx, err := s.buildWorkerBootContext(worker, Task{}, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}
	for _, forbidden := range []string{loreBlockHeading, "傳承標題乙", "傳承內容乙"} {
		if strings.Contains(ctx, forbidden) {
			t.Fatalf("the outsource boot context now carries %q — a worker has no "+
				"role, so a role's 傳承 names nothing it can read, and this path was "+
				"not to be touched at all", forbidden)
		}
	}
}

// TestManualLoreLandsAfterTheLearningsInTheManualRead is EXIT 2, asserted on
// the wire body: the entry text appears, and it appears AFTER the learnings
// text that was already there.
func TestManualLoreLandsAfterTheLearningsInTheManualRead(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-8", "researcher")
	if err := s.dal.PutTaskManual(TaskManual{
		TypeKey: "tm-review", DisplayName: "Review", Learnings: "既有的學習內容",
		UpdatedTS: 1,
	}); err != nil {
		t.Fatalf("PutTaskManual: %v", err)
	}
	typed, err := s.dal.CreateTaskMintingID(Task{
		Title: "t", TypeKey: "tm-review", ExecutorKind: TaskExecutorStaff,
		ExecutorID: me, CreatorID: me, Priority: TaskPriorityMid,
		CreatedTS: 1, UpdatedTS: 1,
	}, nil)
	if err != nil {
		t.Fatalf("CreateTaskMintingID: %v", err)
	}
	if rec := postLore(t, s, me, map[string]any{
		"title": "傳承標題丙", "body": "傳承內容丙", "task_id": typed.ID,
	}); rec.Code != http.StatusOK {
		t.Fatalf("seed write: %d %s", rec.Code, rec.Body.String())
	}

	rec := httptest.NewRecorder()
	s.HandleGetTaskManualApiTaskManualsTypeKeyGet(rec,
		taskReq(t, "GET", "/api/task-manuals/tm-review", nil, me, "agent"), "tm-review")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var dto struct {
		Learnings         string `json:"learnings"`
		LearningsChars    int    `json:"learnings_chars"`
		LearningsCapChars int    `json:"learnings_cap_chars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	learnAt := strings.Index(dto.Learnings, "既有的學習內容")
	loreAt := strings.Index(dto.Learnings, "傳承標題丙")
	if learnAt < 0 {
		t.Fatalf("the manual's own learnings vanished: %q", dto.Learnings)
	}
	if loreAt < 0 {
		t.Fatalf("the entry did not reach the manual read: %q", dto.Learnings)
	}
	if loreAt < learnAt {
		t.Fatalf("傳承 was placed BEFORE the learnings: %q", dto.Learnings)
	}
	// 🔴 learnings_chars still measures the STORED document, because that is what
	// a WRITER sizes an edit against. If it grew to cover the appended block, an
	// edit that fits would start being refused.
	if want := len([]rune("既有的學習內容")); dto.LearningsChars != want {
		t.Fatalf("learnings_chars = %d, want %d — it must measure the STORED "+
			"document, not the rendering", dto.LearningsChars, want)
	}
}

// TestManualWithNoLoreIsUnchanged — the exits append NOTHING when there is
// nothing to append. An empty heading would be a claim the empty selection does
// not support.
func TestManualWithNoLoreIsUnchanged(t *testing.T) {
	s := loreTestServer(t)
	if err := s.dal.PutTaskManual(TaskManual{
		TypeKey: "tm-empty", DisplayName: "Empty", Learnings: "只有學習內容",
		UpdatedTS: 1,
	}); err != nil {
		t.Fatalf("PutTaskManual: %v", err)
	}
	rec := httptest.NewRecorder()
	s.HandleGetTaskManualApiTaskManualsTypeKeyGet(rec,
		taskReq(t, "GET", "/api/task-manuals/tm-empty", nil, "m-x", "agent"), "tm-empty")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var dto struct {
		Learnings string `json:"learnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.Learnings != "只有學習內容" {
		t.Fatalf("learnings = %q, want the stored text verbatim — with no 傳承 to "+
			"append, nothing at all is appended", dto.Learnings)
	}
}

// TestBothExitsGoThroughTheOneSelector is the structural guard behind the
// ticket's hard condition. It is a SOURCE-level check on purpose: no behavioural
// test can tell "two call sites of one function" from "two identical copies", and
// a copy is exactly what this rule forbids.
func TestBothExitsGoThroughTheOneSelector(t *testing.T) {
	callers := map[string]string{
		"assets.go":          "the staff boot document (角色傳承)",
		"api_taskmanuals.go": "GET /api/task-manuals/{type_key} (任務傳承)",
	}
	for file, what := range callers {
		src := readSourceForLoreGuard(t, file)
		if !strings.Contains(src, "selectLoreForScope(") {
			t.Fatalf("%s (%s) no longer calls selectLoreForScope — if the selection "+
				"was reimplemented there, that is the second copy T-33 forbids",
				file, what)
		}
	}
	// And the rule itself lives in exactly one file.
	for _, file := range []string{"assets.go", "api_taskmanuals.go", "api_lore.go"} {
		src := readSourceForLoreGuard(t, file)
		if strings.Contains(src, "func selectLoreForScope") {
			t.Fatalf("selectLoreForScope is DEFINED in %s as well as lore_select.go — "+
				"there must be exactly one definition", file)
		}
	}
}

// readSourceForLoreGuard reads one file of this package off disk. Tests run
// with the package directory as their working directory, so the relative name
// is the file beside this one.
func readSourceForLoreGuard(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
