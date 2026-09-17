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
// id. The roleKey argument is still taken and still stored: since the scopes
// collapsed it is no longer what a write is FILED under, and that is exactly
// what several tests below now assert — a fixture that stopped setting a role
// could not tell "keyed by the member" from "there was no role to key by".
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

// TestWriteLoreWithNoTaskFilesUnderTheCallersOwnMemberId — the MEMBER arm, and
// the scope key comes from the ROSTER, never from the request.
//
// 🔴 THIS TEST USED TO WANT role/researcher. Owner collapsed the scopes on
// 2026-09-07 (card rc-a43100fd0486 [0]): staff no longer file under their role,
// they file under themselves, exactly as outsource members already did. The
// fixture still GIVES this member a role_key, and that is the discriminating
// part — an implementation that kept keying by role would have a non-empty role
// to key by and would fail here, whereas a fixture with no role would let both
// implementations pass.
func TestWriteLoreWithNoTaskFilesUnderTheCallersOwnMemberId(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-1", "researcher")

	rec := postLore(t, s, me, map[string]any{"title": "一件事", "body": "內容"})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var dto LoreEntryWriteReceiptDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.ScopeKind != LoreScopeAgent || dto.ScopeKey != me {
		t.Fatalf("scope = %s/%s, want agent/%s — a staff member's 傳承 hangs off "+
			"its own member id, not off %q, since the scopes collapsed to two",
			dto.ScopeKind, dto.ScopeKey, me, "researcher")
	}
	// 🔴 AND IT DID NOT LAND UNDER THE ROLE KEY UNDER SOME OTHER KIND EITHER. The
	// check above pins the pair; this one catches the implementation that swapped
	// only the kind and kept feeding it m.RoleKey, which would answer
	// agent/researcher and satisfy any assertion that looked at one field.
	if dto.ScopeKey == "researcher" {
		t.Fatalf("scope_key is the ROLE key %q — the kind was changed and the key "+
			"was not", dto.ScopeKey)
	}
	// 🔴 THE REST IS ASSERTED AGAINST THE STORED ROW, NOT THE RESPONSE. The write
	// answers a bounded receipt (T-33, owner 2026-09-07), so author_id, state and
	// effective_ts no longer ride home — but they are still properties of the
	// write, and a receipt that stopped carrying them must not also stop them
	// being checked. Reading the row is the stricter check anyway: it is what the
	// next reader will see.
	stored, err := s.dal.GetLoreEntry(dto.Id)
	if err != nil {
		t.Fatalf("GetLoreEntry: %v", err)
	}
	if stored == nil {
		t.Fatalf("the receipt named %q and no such row was stored", dto.Id)
	}
	if stored.AuthorID != me {
		t.Fatalf("author_id = %q, want %q — the writer is PINNED at write time",
			stored.AuthorID, me)
	}
	if stored.State != LoreStateActive {
		t.Fatalf("state = %q, want %q", stored.State, LoreStateActive)
	}
	if stored.CreatedTS != stored.EffectiveTS {
		t.Fatalf("created_ts %v != effective_ts %v — they start equal",
			stored.CreatedTS, stored.EffectiveTS)
	}
	// The one timestamp the receipt DOES carry must be the stored one, or the
	// receipt is reporting a write other than the one that landed.
	if dto.CreatedTs != stored.CreatedTS {
		t.Fatalf("receipt created_ts %v != stored %v", dto.CreatedTs, stored.CreatedTS)
	}
	// 🔴 AND THE ECHO IS GONE. The body is what the caller just sent; if it comes
	// home the whole change is undone, and nothing else here would notice.
	if strings.Contains(rec.Body.String(), "內容") {
		t.Fatalf("the write echoed the body back: %s", rec.Body.String())
	}
}

// TestWriteLoreWithNoRoleFilesUnderTheWritersOwnId — the AGENT arm.
//
// 🔴 THIS TEST USED TO ASSERT THE OPPOSITE (TestWriteLoreWithNoRoleIs400: a
// caller with no role was refused). The owner overturned that on 2026-09-07,
// card rc-3c24fdc61ed3: an outsource member has no role by construction, so the
// old refusal meant it had nowhere at all to put anything it learned outside a
// typed task. It now files under its OWN member id, which rides its own boot
// document and nobody else's.
//
// The scope key comes from the VERIFIED token subject, never from the request.
// Since the 2026-09-07 collapse (card rc-a43100fd0486 [0]) this is no longer a
// second arm beside a role arm — it is the ONLY member arm, and staff enter it
// through the same door. This test is kept separate anyway because it is the
// kind of writer with NO role_key at all: it proves the door does not quietly
// depend on there being one.
func TestWriteLoreWithNoRoleFilesUnderTheWritersOwnId(t *testing.T) {
	s := loreTestServer(t)
	if err := s.dal.PutMember(Member{
		ID: "ow-lore-1", Name: "Contractor", Kind: KindOutsource, RoleKey: "",
		Runtime: RuntimeClaude, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}

	rec := postLore(t, s, "ow-lore-1", map[string]any{"title": "x", "body": "y"})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var dto LoreEntryWriteReceiptDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.ScopeKind != LoreScopeAgent || dto.ScopeKey != "ow-lore-1" {
		t.Fatalf("scope = %s/%s, want agent/ow-lore-1", dto.ScopeKind, dto.ScopeKey)
	}
	// 🔴 AND NOTHING LANDED UNDER AN EMPTY KEY. An implementation that filed under
	// "" (this writer's empty role_key) would answer 200 and set scope_kind
	// itself, so the DTO alone cannot tell the two apart. The scan is over the
	// WHOLE table rather than one scope: the old version of this check asked
	// ListLoreEntriesLive(LoreScopeRole, ""), which stopped being able to fail
	// the moment the role scope was removed — it would have gone on passing
	// while asserting nothing.
	all, err := s.dal.ListLoreEntriesPage(loreListFilter{}, 30, 0)
	if err != nil {
		t.Fatalf("ListLoreEntriesPage: %v", err)
	}
	for _, e := range all {
		if e.ScopeKey == "" {
			t.Fatalf("an entry landed under an EMPTY scope key: %+v", e)
		}
	}
	// No scope_note: the writer named no task, so nothing about where this went
	// was unpredictable from its own request.
	if dto.ScopeNote != "" {
		t.Fatalf("scope_note = %q, want empty — this write named no task", dto.ScopeNote)
	}
}

// TestWriteLoreWithNoRosterRowIs400 — the refusal that SURVIVED the ruling. The
// owner has no roster row at all, so there is no boot document of his own for an
// entry to ride, and inventing a scope for him would create one nobody reads.
//
// It matters that this is still here: the ruling widened who may write, and the
// cheap way to implement "let outsource through" is to stop checking at all.
func TestWriteLoreWithNoRosterRowIs400(t *testing.T) {
	s := loreTestServer(t)

	rec := postLore(t, s, "nobody-on-the-roster", map[string]any{"title": "x", "body": "y"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d %s", rec.Code, rec.Body.String())
	}
	page, err := s.dal.ListLoreEntriesPage(loreListFilter{}, 30, 0)
	if err != nil {
		t.Fatalf("ListLoreEntriesPage: %v", err)
	}
	if len(page) != 0 {
		t.Fatalf("a refused write stored %d entries: %+v", len(page), page)
	}
}

// TestWriteLoreAgainstAnUntypedTaskFilesUnderTheWritersOwnBootDocument.
//
// 🔴 THIS TEST USED TO ASSERT THE OPPOSITE — it was
// TestWriteLoreAgainstAnUntypedTaskIs400AndIsNotFiledUnderTheRole, and it
// called the refusal "the branch the spec calls out in red". The owner
// overturned it on 2026-09-07 in one sentence: 「臨時任務跟無關乎任何任務一樣都是
// 給 NULL」. Under that reading the old refusal was answering the wrong question.
// A task with no type is not a request that got re-routed to somewhere it did
// not ask for — it is not a place an entry can hang AT ALL, so it is the same
// input as naming no task, and the same answer follows: the writer's own boot
// document.
//
// 🔴 WHAT THE OLD TEST GUARDED IS STILL GUARDED, and it is the second half here:
// the entry must NOT reach any manual. That was always the real hazard — an
// entry charged to a task TYPE that will never work on it — and it is untouched
// by the ruling.
func TestWriteLoreAgainstAnUntypedTaskFilesUnderTheWritersOwnBootDocument(t *testing.T) {
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
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 for a task with no type_key, got %d %s",
			rec.Code, rec.Body.String())
	}
	var dto LoreEntryWriteReceiptDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The writer here is STAFF, and since the collapse its own boot document is
	// its OWN — not its role's.
	if dto.ScopeKind != LoreScopeAgent || dto.ScopeKey != me {
		t.Fatalf("scope = %s/%s, want agent/%s", dto.ScopeKind, dto.ScopeKey, me)
	}
	// 🔴 The half that outlived the ruling: no manual was charged for this.
	page, err := s.dal.ListLoreEntriesPage(loreListFilter{ScopeKinds: []string{LoreScopeManual}}, 30, 0)
	if err != nil {
		t.Fatalf("ListLoreEntriesPage: %v", err)
	}
	if len(page) != 0 {
		t.Fatalf("a 臨時任務 entry reached a task manual: %+v — that charges a task "+
			"TYPE for a lesson about a one-off piece of work", page)
	}
	// 🔴 AND THE RESPONSE SAYS SO. This is the one case the writer could not have
	// predicted: it named a task and the entry went somewhere else. Without this
	// sentence the two outcomes are indistinguishable on the wire — both are 200.
	if dto.ScopeNote == "" {
		t.Fatalf("scope_note is empty — a write that named a task and landed in the " +
			"writer's own boot document must SAY so; the writer has no other way to " +
			"learn whether the task it named carried a type")
	}
	if !strings.Contains(dto.ScopeNote, adhoc.ID) {
		t.Fatalf("scope_note %q must name the task that carried no type", dto.ScopeNote)
	}
}

// TestWriteLoreAgainstAnUntypedTaskByAnOutsourceMemberFilesUnderItsOwnId — the
// same door, entered by the member kind the ruling was actually about.
func TestWriteLoreAgainstAnUntypedTaskByAnOutsourceMemberFilesUnderItsOwnId(t *testing.T) {
	s := loreTestServer(t)
	if err := s.dal.PutMember(Member{
		ID: "ow-lore-2", Name: "Contractor", Kind: KindOutsource, RoleKey: "",
		Runtime: RuntimeClaude, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	adhoc, err := s.dal.CreateTaskMintingID(Task{
		Title: "一張臨時任務", TypeKey: "", ExecutorKind: TaskExecutorOutsource,
		ExecutorID: "ow-lore-2", CreatorID: "ow-lore-2", Priority: TaskPriorityMid,
		CreatedTS: 1, UpdatedTS: 1,
	}, nil)
	if err != nil {
		t.Fatalf("CreateTaskMintingID: %v", err)
	}

	rec := postLore(t, s, "ow-lore-2", map[string]any{
		"title": "一件事", "body": "內容", "task_id": adhoc.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var dto LoreEntryWriteReceiptDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.ScopeKind != LoreScopeAgent || dto.ScopeKey != "ow-lore-2" {
		t.Fatalf("scope = %s/%s, want agent/ow-lore-2", dto.ScopeKind, dto.ScopeKey)
	}
	if dto.ScopeNote == "" {
		t.Fatalf("scope_note is empty — this write named a task and landed elsewhere")
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
	var dto LoreEntryWriteReceiptDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.ScopeKind != LoreScopeManual || dto.ScopeKey != "tm-review" {
		t.Fatalf("scope = %s/%s, want manual/tm-review", dto.ScopeKind, dto.ScopeKey)
	}
	// source_task_id is the task the caller just named, so it does not ride home
	// on the receipt (T-33). It is still RECORDED, and that is checked where it
	// now lives — on the stored row.
	stored, err := s.dal.GetLoreEntry(dto.Id)
	if err != nil {
		t.Fatalf("GetLoreEntry: %v", err)
	}
	if stored == nil {
		t.Fatalf("the receipt named %q and no such row was stored", dto.Id)
	}
	if stored.SourceTaskID != typed.ID {
		t.Fatalf("source_task_id = %q, want %q", stored.SourceTaskID, typed.ID)
	}
	// The WRITER's own scope stays empty: the two are not interchangeable. Keyed
	// by the member id, because that is what the writer's own scope is now — the
	// old version of this check looked in role/researcher, which after the
	// collapse is a scope nothing can write to, so it could no longer fail.
	mine, err := s.dal.ListLoreEntriesLive(LoreScopeAgent, me)
	if err != nil {
		t.Fatalf("ListLoreEntriesLive: %v", err)
	}
	if len(mine) != 0 {
		t.Fatalf("a manual entry leaked into the writer's own scope: %+v", mine)
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
	var seeded LoreEntryWriteReceiptDTO
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

	// Back to active — NOT to pinned: 置頂 is admin-only (see TestPinningIsAdminOnly),
	// and the author's own way back is 生效. That the reason is cleared is a
	// property of the transition, not of which non-retired state it lands in.
	rec = httptest.NewRecorder()
	s.HandleSetLoreEntryStateApiLoreEntryIdStatePost(rec,
		taskReq(t, "POST", "/api/lore/"+seeded.Id+"/state",
			map[string]any{"state": LoreStateActive}, me, "agent"), seeded.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("revive: %d %s", rec.Code, rec.Body.String())
	}
	var after LoreEntryStateReceiptDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if after.State != LoreStateActive {
		t.Fatalf("state = %q, want %q", after.State, LoreStateActive)
	}
	// 🔴 retire_reason IS NOT ON THE RECEIPT (T-33) — the governance receipt
	// carries id/state/effective_ts/updated_ts. The clearing is a property of the
	// STORED row, which is what the next reader and both folds see, so that is
	// where it is asserted. Reading it off the response only ever proved the
	// response.
	stored, err := s.dal.GetLoreEntry(seeded.Id)
	if err != nil {
		t.Fatalf("GetLoreEntry: %v", err)
	}
	if stored == nil {
		t.Fatalf("no such row after the revive: %q", seeded.Id)
	}
	if stored.RetireReason != "" {
		t.Fatalf("retire_reason = %q on a %s entry — a live entry must not carry "+
			"the explanation for a retirement that was undone",
			stored.RetireReason, stored.State)
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

// TestRoleLoreLandsInTheStaffBootDocumentAfterThePersona is EXIT 1, asserted on
// the assembled document: the literal entry text must be IN it, and it must sit
// AFTER the persona and BEFORE the boot-sequence tail.
// loreBlockAt reports where the APPENDED 傳承 block starts in an assembled
// document, or -1 when the document carries none.
//
// 🔴 IT ANCHORS THE HEADING TO THE START OF A LINE, and that is not
// fastidiousness — a plain strings.Index(doc, loreBlockHeading) is WRONG here
// and was wrong silently. The handbook these documents are assembled around now
// has a section headed 「### 傳承寫入位置」, and "### 傳承…" CONTAINS "# 傳承",
// so an unanchored search finds the handbook's prose and reports the block as
// present in a document that has none — and, in the document that does have
// one, reports it thousands of characters too early. Both readings look exactly
// like a real answer.
//
// Anchoring to the start of a line is what makes the answer trustworthy; the
// loose form was only ever asked here, never in production.
func loreBlockAt(doc string) int {
	if strings.HasPrefix(doc, loreBlockHeading+"\n") || doc == loreBlockHeading {
		return 0
	}
	if i := strings.Index(doc, "\n"+loreBlockHeading+"\n"); i >= 0 {
		return i + 1
	}
	if strings.HasSuffix(doc, "\n"+loreBlockHeading) {
		return len(doc) - len(loreBlockHeading)
	}
	return -1
}

func TestRoleLoreLandsInTheStaffBootDocumentAfterThePersona(t *testing.T) {
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
	personaAt := strings.Index(doc, "# Role: ")
	loreAt := loreBlockAt(doc)
	titleAt := strings.Index(doc, "傳承標題甲")
	bodyAt := strings.Index(doc, "傳承內容甲")
	if personaAt < 0 {
		t.Fatalf("the boot document has no 角色說明 block at all:\n%s", doc)
	}
	if loreAt < 0 || titleAt < 0 || bodyAt < 0 {
		t.Fatalf("the entry did not reach the boot document (lore=%d title=%d body=%d):\n%s",
			loreAt, titleAt, bodyAt, doc)
	}
	if loreAt < personaAt {
		t.Fatalf("傳承 was assembled BEFORE the persona (lore=%d persona=%d) — the "+
			"spec puts it after", loreAt, personaAt)
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
	if loreBlockAt(ctx) >= 0 {
		t.Fatalf("the outsource boot context now carries an appended %q block — a "+
			"worker has no role, so a role's 傳承 names nothing it can read, and "+
			"this path was not to be touched at all", loreBlockHeading)
	}
	// The entry's own text, separately: a block could in principle be absent
	// while the text leaked in some other way, and these two literals say so
	// without depending on the heading at all.
	for _, forbidden := range []string{"傳承標題乙", "傳承內容乙"} {
		if strings.Contains(ctx, forbidden) {
			t.Fatalf("the outsource boot context now carries %q — a role's 傳承 "+
				"must not reach the worker path", forbidden)
		}
	}
}

// TestManualLoreRidesItsOwnFieldInTheManualRead is EXIT 2, asserted on the wire
// body. It used to be called …LandsAfterTheLearnings… and asserted the ORDER of
// two things inside one field; the owner overturned that shape on 2026-09-07
// (「get_task_manual 應該 learning 跟 lore 還是分開的欄位」), so the question is no
// longer "which came first" but "did they stay apart".
//
// 🔴 IT PINS ALL FOUR CORNERS, and it has to. Serving the block on `lore` while
// ALSO folding it into the manual's stored document would satisfy any single
// one of these, and that half-migrated state is the likely regression: it is
// what every caller that "just adds the new field" produces.
func TestManualLoreRidesItsOwnFieldInTheManualRead(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-8", "researcher")
	if err := s.dal.PutTaskManual(TaskManual{
		TypeKey: "tm-review", DisplayName: "Review", SopMD: "既有的 SOP 內容",
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
		SopMD         string `json:"sop_md"`
		SopMDChars    int    `json:"sop_md_chars"`
		SopMDCapChars int    `json:"sop_md_cap_chars"`
		Lore          string `json:"lore"`
		LoreChars     int    `json:"lore_chars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// CORNER 1 — the entry reached the read AT ALL, on its own field. Without
	// this the three below would pass on a response that simply serves nothing.
	if !strings.Contains(dto.Lore, "傳承標題丙") {
		t.Fatalf("the entry did not reach `lore`: %q", dto.Lore)
	}
	if !strings.Contains(dto.Lore, loreBlockHeading) {
		t.Fatalf("`lore` is not a rendered 傳承 block: %q", dto.Lore)
	}

	// CORNER 2 — `sop_md` is the STORED DOCUMENT AND NOTHING ELSE. Equality,
	// not "contains": a half-migration that appends as well as splits still
	// contains the stored text, and would slip past a containment check.
	if dto.SopMD != "既有的 SOP 內容" {
		t.Fatalf("sop_md = %q, want the stored document verbatim and alone — "+
			"the 傳承 block rides `lore`", dto.SopMD)
	}

	// CORNER 3 — sop_md_chars still measures the STORED document, because that
	// is what a WRITER sizes an edit against. If it grew to cover the
	// rendering, an edit that fits would start being refused.
	if want := len([]rune("既有的 SOP 內容")); dto.SopMDChars != want {
		t.Fatalf("sop_md_chars = %d, want %d — it must measure the STORED "+
			"document", dto.SopMDChars, want)
	}

	// CORNER 4 — lore_chars measures `lore`. This is the number whose ABSENCE
	// produced the owner's report: a field full of text beside a count of 0,
	// with nothing saying the count was about the other half.
	if want := len([]rune(dto.Lore)); dto.LoreChars != want {
		t.Fatalf("lore_chars = %d, want %d — it must measure the `lore` field it "+
			"is named after", dto.LoreChars, want)
	}
	if dto.LoreChars == 0 {
		t.Fatalf("lore_chars = 0 while `lore` carries %q — this is exactly the "+
			"shape the owner reported: a field full of text beside a count of 0", dto.Lore)
	}
}

// TestManualWithNoLoreIsUnchanged — the exits append NOTHING when there is
// nothing to append. An empty heading would be a claim the empty selection does
// not support.
func TestManualWithNoLoreIsUnchanged(t *testing.T) {
	s := loreTestServer(t)
	if err := s.dal.PutTaskManual(TaskManual{
		TypeKey: "tm-empty", DisplayName: "Empty", SopMD: "只有 SOP 內容",
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
		SopMD string `json:"sop_md"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.SopMD != "只有 SOP 內容" {
		t.Fatalf("sop_md = %q, want the stored text verbatim — with no 傳承 to "+
			"append, nothing at all is appended", dto.SopMD)
	}
}

// TestBothExitsGoThroughTheOneSelector is the structural guard behind the
// ticket's hard condition. It is a SOURCE-level check on purpose: no behavioural
// test can tell "two call sites of one function" from "two identical copies", and
// a copy is exactly what this rule forbids.
func TestBothExitsGoThroughTheOneSelector(t *testing.T) {
	callers := []struct{ file, selector, what string }{
		{"assets.go", "selectMemberLore(", "the staff boot document"},
		{"worker_spawn.go", "selectMemberLore(", "the outsource boot document"},
		{"api_taskmanuals.go", "selectLoreForScope(", "GET /api/task-manuals/{type_key} (任務傳承)"},
	}
	for _, c := range callers {
		src := readSourceForLoreGuard(t, c.file)
		if !strings.Contains(src, c.selector) {
			t.Fatalf("%s (%s) no longer calls %s — if the selection was "+
				"reimplemented there, that is the second copy T-33 forbids",
				c.file, c.what, c.selector)
		}
	}
	// And the rule itself lives in exactly one file.
	for _, file := range []string{"assets.go", "worker_spawn.go", "api_taskmanuals.go", "api_lore.go"} {
		src := readSourceForLoreGuard(t, file)
		for _, def := range []string{"func selectLoreForScope", "func selectMemberLore", "func selectLoreEntries"} {
			if strings.Contains(src, def) {
				t.Fatalf("%s is DEFINED in %s as well as lore_select.go — there must "+
					"be exactly one definition", def, file)
			}
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

// ── governance floors (owner ruling, 2026-09-07) ────────────────────────────

// seedLoreEntryBy writes one entry as `author` and returns its id.
func seedLoreEntryBy(t *testing.T, s *apiServer, author, title string) string {
	t.Helper()
	rec := postLore(t, s, author, map[string]any{"title": title, "body": "內容"})
	if rec.Code != http.StatusOK {
		t.Fatalf("seed write as %s: %d %s", author, rec.Code, rec.Body.String())
	}
	var dto LoreEntryWriteReceiptDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return dto.Id
}

func postLoreState(t *testing.T, s *apiServer, sub, entryID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	scope := "agent"
	if sub == wireOwnerID {
		scope = "owner"
	}
	s.HandleSetLoreEntryStateApiLoreEntryIdStatePost(rec,
		taskReq(t, "POST", "/api/lore/"+entryID+"/state", body, sub, scope), entryID)
	return rec
}

func postLoreBump(t *testing.T, s *apiServer, sub, entryID string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	scope := "agent"
	if sub == wireOwnerID {
		scope = "owner"
	}
	s.HandleBumpLoreEntryApiLoreEntryIdBumpPost(rec,
		taskReq(t, "POST", "/api/lore/"+entryID+"/bump", nil, sub, scope), entryID)
	return rec
}

// TestPinningIsAdminOnly — 「置頂只有你跟 admin」. A plain agent cannot pin, not
// even its OWN entry: a pinned entry sorts ahead of everybody else's and so
// survives the cap at their expense, which is not the writer's call.
func TestPinningIsAdminOnly(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-gov-1", "researcher")
	mine := seedLoreEntryBy(t, s, me, "我自己寫的")

	rec := postLoreState(t, s, me, mine, map[string]any{"state": LoreStatePinned})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a plain agent pinning its OWN entry: want 403, got %d %s",
			rec.Code, rec.Body.String())
	}
	// ...and nothing moved.
	row, err := s.dal.GetLoreEntry(mine)
	if err != nil || row == nil {
		t.Fatalf("GetLoreEntry: %v / %v", row, err)
	}
	if row.State != LoreStateActive {
		t.Fatalf("state = %q after a refused pin, want %q", row.State, LoreStateActive)
	}

	// The owner may.
	if rec := postLoreState(t, s, wireOwnerID, mine,
		map[string]any{"state": LoreStatePinned}); rec.Code != http.StatusOK {
		t.Fatalf("owner pinning: want 200, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestUnpinningIsAdminOnlyToo — both directions, and this is the half that is
// easy to leave open. If the AUTHOR could un-pin, any writer could demote an
// entry the owner pinned and the admin-only floor would buy nothing.
func TestUnpinningIsAdminOnlyToo(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-gov-2", "researcher")
	mine := seedLoreEntryBy(t, s, me, "我自己寫的")
	if rec := postLoreState(t, s, wireOwnerID, mine,
		map[string]any{"state": LoreStatePinned}); rec.Code != http.StatusOK {
		t.Fatalf("owner pin: %d %s", rec.Code, rec.Body.String())
	}

	rec := postLoreState(t, s, me, mine, map[string]any{"state": LoreStateActive})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("the author un-pinning its own pinned entry: want 403, got %d %s",
			rec.Code, rec.Body.String())
	}
	row, err := s.dal.GetLoreEntry(mine)
	if err != nil || row == nil {
		t.Fatalf("GetLoreEntry: %v / %v", row, err)
	}
	if row.State != LoreStatePinned {
		t.Fatalf("state = %q after a refused un-pin, want it still pinned", row.State)
	}
}

// TestRetireAndBumpAreAuthorOnly — 失效／提到最新 只作用在「呼叫者自己寫的」那一筆
// （author_id 於寫入當下釘住），admin 不受限。來源是 owner 2026-09-07 的裁定；這裡
// 刻意把規則本身寫出來，不引用 Global Context 的句子 —— 那句話已被 owner 從文件裡
// 刪掉（同日），引用一句不存在的話會把讀的人送去找一個找不到的東西。
func TestRetireAndBumpAreAuthorOnly(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-gov-3", "researcher")
	other := hireLoreStaff(t, s, "m-gov-4", "researcher")
	theirs := seedLoreEntryBy(t, s, other, "別人寫的")

	rec := postLoreState(t, s, me, theirs, map[string]any{"state": LoreStateRetired})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("retiring somebody else's entry: want 403, got %d %s",
			rec.Code, rec.Body.String())
	}
	rec = postLoreBump(t, s, me, theirs)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bumping somebody else's entry: want 403, got %d %s",
			rec.Code, rec.Body.String())
	}
	row, err := s.dal.GetLoreEntry(theirs)
	if err != nil || row == nil {
		t.Fatalf("GetLoreEntry: %v / %v", row, err)
	}
	if row.State != LoreStateActive {
		t.Fatalf("state = %q after two refused writes, want %q untouched",
			row.State, LoreStateActive)
	}

	// The AUTHOR may do both.
	if rec := postLoreBump(t, s, other, theirs); rec.Code != http.StatusOK {
		t.Fatalf("the author bumping its own entry: want 200, got %d %s",
			rec.Code, rec.Body.String())
	}
	if rec := postLoreState(t, s, other, theirs,
		map[string]any{"state": LoreStateRetired, "retire_reason": "過時了"}); rec.Code != http.StatusOK {
		t.Fatalf("the author retiring its own entry: want 200, got %d %s",
			rec.Code, rec.Body.String())
	}
	// ...and so may an admin, on somebody else's.
	if rec := postLoreState(t, s, wireOwnerID, theirs,
		map[string]any{"state": LoreStateActive}); rec.Code != http.StatusOK {
		t.Fatalf("owner reviving somebody else's entry: want 200, got %d %s",
			rec.Code, rec.Body.String())
	}
}

// TestGovernanceRefusalsAreDistinguishable — the pin refusal and the not-yours
// refusal must not be the same sentence, or a caller cannot tell "ask an admin"
// from "that is not your entry".
func TestGovernanceRefusalsAreDistinguishable(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-gov-5", "researcher")
	other := hireLoreStaff(t, s, "m-gov-6", "researcher")
	mine := seedLoreEntryBy(t, s, me, "我的")
	theirs := seedLoreEntryBy(t, s, other, "別人的")

	pinRec := postLoreState(t, s, me, mine, map[string]any{"state": LoreStatePinned})
	ownRec := postLoreState(t, s, me, theirs, map[string]any{"state": LoreStateRetired})
	if pinRec.Body.String() == ownRec.Body.String() {
		t.Fatalf("both refusals read identically: %s", pinRec.Body.String())
	}
	if !strings.Contains(pinRec.Body.String(), "置頂") {
		t.Fatalf("the pin refusal does not name 置頂: %s", pinRec.Body.String())
	}
}

// TestGovernanceOnAnUnknownEntryIs404NotAForbidden — the row is read before the
// floors are applied, so a typo answers "no such entry" rather than a 403 that
// would tell the caller an entry exists.
func TestGovernanceOnAnUnknownEntryIs404NotAForbidden(t *testing.T) {
	s := loreTestServer(t)
	hireLoreStaff(t, s, "m-gov-7", "researcher")
	if rec := postLoreBump(t, s, "m-gov-7", "L-999"); rec.Code != http.StatusNotFound {
		t.Fatalf("bump on an unknown id: want 404, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := postLoreState(t, s, "m-gov-7", "L-999",
		map[string]any{"state": LoreStateRetired}); rec.Code != http.StatusNotFound {
		t.Fatalf("state on an unknown id: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}

// ── set_lore_entry_scope (T-236) ────────────────────────────────────────────

// loreScopeStack is a server reached through the real route table and auth
// middleware, with an owner credential from set-password and an admin agent
// and a plain agent whose credentials come from the production mint.
type loreScopeStack struct {
	api                *apiServer
	h                  http.Handler
	owner, admin, user string
}

const loreScopeAdminID = "m-scope-admin"

func newLoreScopeStack(t *testing.T) loreScopeStack {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	return loreScopeStack{
		api:   api,
		h:     h,
		owner: owner,
		admin: apiTestPrincipalToken(t, api, d, principalAdminAgent, loreScopeAdminID),
		user:  apiTestPrincipalToken(t, api, d, principalAgent, "m-scope-user"),
	}
}

// seedScopedLore stores one agent-scoped entry by author, stamped at ts 100, so
// a receipt's effective_ts and an untouched updated_ts are known literals.
func seedScopedLore(t *testing.T, api *apiServer, author, sourceTaskID, title string) string {
	t.Helper()
	e, err := api.dal.CreateLoreEntryMintingID(LoreEntry{
		ScopeKind: LoreScopeAgent, ScopeKey: author, Title: title, Body: "內容",
		AuthorID: author, SourceTaskID: sourceTaskID, State: LoreStateActive,
		EffectiveTS: 100, CreatedTS: 100, UpdatedTS: 100,
	})
	if err != nil {
		t.Fatalf("CreateLoreEntryMintingID: %v", err)
	}
	return e.ID
}

func seedScopeTask(t *testing.T, api *apiServer, typeKey string) string {
	t.Helper()
	task, err := api.dal.CreateTaskMintingID(Task{
		Title: "t", TypeKey: typeKey, ExecutorKind: TaskExecutorStaff,
		Priority: TaskPriorityMid, CreatedTS: 1, UpdatedTS: 1,
	}, nil)
	if err != nil {
		t.Fatalf("CreateTaskMintingID: %v", err)
	}
	return task.ID
}

func (st loreScopeStack) setScope(t *testing.T, token, entryID, kind string) (int, map[string]any) {
	t.Helper()
	return apiJSON(t, st.h, "POST", "/api/lore/"+entryID+"/scope", token,
		`{"scope_kind":"`+kind+`"}`)
}

func storedScope(t *testing.T, api *apiServer, id string) LoreEntry {
	t.Helper()
	e, err := api.dal.GetLoreEntry(id)
	if err != nil || e == nil {
		t.Fatalf("GetLoreEntry(%s): %v / %v", id, e, err)
	}
	return *e
}

func TestSetLoreEntryScopeMovesTheEntryToEachKindWithTheServerDerivedKey(t *testing.T) {
	st := newLoreScopeStack(t)
	author := hireLoreStaff(t, st.api, "m-scope-author", "researcher")
	typed := seedScopeTask(t, st.api, "tm-scope")
	id := seedScopedLore(t, st.api, author, typed, "會搬家的傳承")

	for _, step := range []struct {
		token, kind, key string
	}{
		{st.admin, LoreScopeManual, "tm-scope"},
		{st.owner, LoreScopeEveryone, ""},
		{st.admin, LoreScopeAgent, author},
	} {
		status, data := st.setScope(t, step.token, id, step.kind)
		if status != http.StatusOK {
			t.Fatalf("move to %s: %d %v", step.kind, status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": id, "scope_kind": step.kind, "scope_key": step.key,
			"state": LoreStateActive, "effective_ts": 100, "updated_ts": apiAnyNumber,
		})
		row := storedScope(t, st.api, id)
		if row.ScopeKind != step.kind || row.ScopeKey != step.key {
			t.Fatalf("stored scope after moving to %s = %s/%q, want %s/%q",
				step.kind, row.ScopeKind, row.ScopeKey, step.kind, step.key)
		}
		if row.Title != "會搬家的傳承" || row.Body != "內容" || row.AuthorID != author ||
			row.EffectiveTS != 100 || row.CreatedTS != 100 {
			t.Fatalf("a scope move touched something besides the scope: %+v", row)
		}
	}
}

func TestSetLoreEntryScopeDerivesTheManualKeyFromTheSourceTaskThenTheOutsourceBinding(t *testing.T) {
	st := newLoreScopeStack(t)
	staff := hireLoreStaff(t, st.api, "m-scope-staff", "researcher")
	typed := seedScopeTask(t, st.api, "tm-source")
	adhoc := seedScopeTask(t, st.api, "")
	bound := seedScopeTask(t, st.api, "tm-bound")
	for _, w := range []OutsourceWorker{
		{ID: "ow-scope-live", Codename: "O-81", TaskID: bound, Status: WorkerStatusActive, Runtime: RuntimeClaude},
		{ID: "ow-scope-gone", Codename: "O-82", TaskID: bound, Status: WorkerStatusActive, Runtime: RuntimeClaude},
		{ID: "ow-scope-adhoc", Codename: "O-83", TaskID: adhoc, Status: WorkerStatusActive, Runtime: RuntimeClaude},
	} {
		if err := st.api.dal.PutOutsourceWorker(w); err != nil {
			t.Fatalf("PutOutsourceWorker(%s): %v", w.ID, err)
		}
	}
	if w, err := st.api.dal.ReleaseWorkerByID("ow-scope-gone", 200); err != nil || w == nil {
		t.Fatalf("ReleaseWorkerByID: %v / %v", w, err)
	}
	if err := st.api.dal.PutMember(Member{
		ID: "m-scope-linked", Name: "Linked Staff", Kind: KindStaff, RoleKey: "researcher",
		Runtime: RuntimeClaude, RosterStatus: RosterStatusActive, LinkedTaskID: &bound,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}

	for _, tc := range []struct {
		name, author, source, wantKey string
	}{
		{"typed source task, staff author", staff, typed, "tm-source"},
		{"typed source task wins over the outsource binding", "ow-scope-live", typed, "tm-source"},
		{"ad-hoc source task, outsource author bound to a typed task", "ow-scope-live", adhoc, "tm-bound"},
		{"no source task, outsource author bound to a typed task", "ow-scope-live", "", "tm-bound"},
		{"released outsource author keeps its binding", "ow-scope-gone", "", "tm-bound"},
	} {
		id := seedScopedLore(t, st.api, tc.author, tc.source, tc.name)
		status, data := st.setScope(t, st.admin, id, LoreScopeManual)
		if status != http.StatusOK {
			t.Fatalf("%s: %d %v", tc.name, status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": id, "scope_kind": LoreScopeManual, "scope_key": tc.wantKey,
			"state": LoreStateActive, "effective_ts": 100, "updated_ts": apiAnyNumber,
		})
	}

	for _, tc := range []struct {
		name, author, source string
	}{
		{"ad-hoc source task, staff author", staff, adhoc},
		{"no source task, staff author", staff, ""},
		{"outsource author bound to an ad-hoc task", "ow-scope-adhoc", ""},
		{"staff author whose row carries a typed task binding", "m-scope-linked", ""},
	} {
		id := seedScopedLore(t, st.api, tc.author, tc.source, tc.name)
		status, data := st.setScope(t, st.admin, id, LoreScopeManual)
		if status != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d %v", tc.name, status, data)
		}
		apiWantError(t, data, "validation_error", "lore entry "+id+" has no task type "+
			"to key a manual scope to — its source task (if any) carries no type, and "+
			"its author is not an outsource member bound to a typed task; choose agent or everyone")
		if row := storedScope(t, st.api, id); row.ScopeKind != LoreScopeAgent ||
			row.ScopeKey != tc.author || row.UpdatedTS != 100 {
			t.Fatalf("%s: a refused move changed the row: %+v", tc.name, row)
		}
	}
}

func TestSetLoreEntryScopeRefusesAKindOutsideTheThreeTargets(t *testing.T) {
	st := newLoreScopeStack(t)
	author := hireLoreStaff(t, st.api, "m-scope-kind", "researcher")
	id := seedScopedLore(t, st.api, author, "", "不動的傳承")

	for _, kind := range []string{"role", "roles", ""} {
		status, data := st.setScope(t, st.admin, id, kind)
		if status != http.StatusBadRequest {
			t.Fatalf("scope_kind %q: want 400, got %d %v", kind, status, data)
		}
		apiWantError(t, data, "validation_error",
			`scope_kind must be agent, manual or everyone — got "`+kind+`"`)
	}
	if row := storedScope(t, st.api, id); row.ScopeKind != LoreScopeAgent ||
		row.ScopeKey != author || row.UpdatedTS != 100 {
		t.Fatalf("a refused move changed the row: %+v", row)
	}
}

func TestSetLoreEntryScopeIsRefusedToEveryoneButOwnerAndAdmin(t *testing.T) {
	st := newLoreScopeStack(t)
	author := hireLoreStaff(t, st.api, "m-scope-writer", "researcher")
	authorToken := apiTestAgentToken(t, st.api, author, "")
	id := seedScopedLore(t, st.api, author, "", "作者自己的")

	for _, tc := range []struct {
		name, token string
		status      int
		code, msg   string
	}{
		{"the entry's own author", authorToken, http.StatusForbidden, "forbidden", "principal not permitted"},
		{"another plain agent", st.user, http.StatusForbidden, "forbidden", "principal not permitted"},
		{"no credential", "", http.StatusUnauthorized, "unauthorized", "missing credentials"},
	} {
		status, data := st.setScope(t, tc.token, id, LoreScopeEveryone)
		if status != tc.status {
			t.Fatalf("%s: want %d, got %d %v", tc.name, tc.status, status, data)
		}
		apiWantError(t, data, tc.code, tc.msg)
	}
	if row := storedScope(t, st.api, id); row.ScopeKind != LoreScopeAgent || row.UpdatedTS != 100 {
		t.Fatalf("a refused move changed the row: %+v", row)
	}

	status, data := st.setScope(t, st.admin, "L-9999", LoreScopeEveryone)
	if status != http.StatusNotFound {
		t.Fatalf("unknown entry: want 404, got %d %v", status, data)
	}
	apiWantError(t, data, "not_found", "no such lore entry: L-9999")
}

func TestSetLoreEntryScopeToTheCurrentScopeChangesNothing(t *testing.T) {
	st := newLoreScopeStack(t)
	author := hireLoreStaff(t, st.api, "m-scope-noop", "researcher")
	id := seedScopedLore(t, st.api, author, "", "原地不動")

	status, data := st.setScope(t, st.admin, id, LoreScopeAgent)
	if status != http.StatusOK {
		t.Fatalf("no-op move: %d %v", status, data)
	}
	apiWantBody(t, data, map[string]any{
		"id": id, "scope_kind": LoreScopeAgent, "scope_key": author,
		"state": LoreStateActive, "effective_ts": 100, "updated_ts": 100,
	})
	if row := storedScope(t, st.api, id); row.UpdatedTS != 100 {
		t.Fatalf("a no-op move stamped updated_ts: %v", row.UpdatedTS)
	}

	status, data = st.setScope(t, st.admin, id, LoreScopeEveryone)
	if status != http.StatusOK {
		t.Fatalf("real move: %d %v", status, data)
	}
	if row := storedScope(t, st.api, id); row.UpdatedTS == 100 {
		t.Fatalf("a real move left updated_ts at 100 — the no-op check above proves nothing")
	}
}

func TestSetLoreEntryScopeMovesALegacyRoleEntryOutOfRole(t *testing.T) {
	st := newLoreScopeStack(t)
	author := hireLoreStaff(t, st.api, "m-scope-legacy", "researcher")
	e, err := st.api.dal.CreateLoreEntryMintingID(LoreEntry{
		ScopeKind: "role", ScopeKey: "researcher", Title: "孤兒", Body: "內容",
		AuthorID: author, State: LoreStatePinned,
		EffectiveTS: 100, CreatedTS: 100, UpdatedTS: 100,
	})
	if err != nil {
		t.Fatalf("seed role orphan: %v", err)
	}
	status, data := st.setScope(t, st.admin, e.ID, LoreScopeAgent)
	if status != http.StatusOK {
		t.Fatalf("move orphan: %d %v", status, data)
	}
	apiWantBody(t, data, map[string]any{
		"id": e.ID, "scope_kind": LoreScopeAgent, "scope_key": author,
		"state": LoreStatePinned, "effective_ts": 100, "updated_ts": apiAnyNumber,
	})
}

func TestListCarriesEachEntrysTaskTypeAndScopeOptions(t *testing.T) {
	st := newLoreScopeStack(t)
	author := hireLoreStaff(t, st.api, "m-scope-list", "researcher")
	typedID := seedScopedLore(t, st.api, author, seedScopeTask(t, st.api, "tm-listed"), "有類型")
	adhocID := seedScopedLore(t, st.api, author, seedScopeTask(t, st.api, ""), "臨時任務")

	status, data := apiJSON(t, st.h, "GET",
		"/api/lore?scope_kind=agent&scope_key="+author, st.user, "")
	if status != http.StatusOK {
		t.Fatalf("list: %d %v", status, data)
	}
	entries, _ := data["entries"].([]any)
	got := map[string][2]any{}
	for _, raw := range entries {
		e, _ := raw.(map[string]any)
		got[e["id"].(string)] = [2]any{e["task_type_key"], e["scope_options"]}
	}
	apiWantValue(t, typedID, got[typedID][0], "tm-listed")
	apiWantValue(t, typedID, got[typedID][1], []any{"manual", "agent", "everyone"})
	apiWantValue(t, adhocID, got[adhocID][0], "")
	apiWantValue(t, adhocID, got[adhocID][1], []any{"agent", "everyone"})
	if len(got) != 2 {
		t.Fatalf("the page carries %d entries, want the 2 seeded: %v", len(got), got)
	}
}

func TestListFiltersTheEveryoneScopeAndReportsTheMemberCapForIt(t *testing.T) {
	st := newLoreScopeStack(t)
	author := hireLoreStaff(t, st.api, "m-scope-filter", "researcher")
	st.api.loreCapCharsRole = 5
	st.api.loreCapCharsManual = 9000
	first := seedScopedLore(t, st.api, author, "", "甲甲甲")
	second := seedScopedLore(t, st.api, author, "", "乙乙乙")
	mine := seedScopedLore(t, st.api, author, "", "丙丙丙")
	for _, id := range []string{first, second} {
		if status, data := st.setScope(t, st.admin, id, LoreScopeEveryone); status != http.StatusOK {
			t.Fatalf("move %s: %d %v", id, status, data)
		}
	}
	// Equal effective_ts, so the fold orders by seq descending.
	status, data := apiJSON(t, st.h, "GET", "/api/lore?scope_kinds=everyone", st.user, "")
	if status != http.StatusOK {
		t.Fatalf("list everyone: %d %v", status, data)
	}
	var ids []any
	for _, raw := range data["entries"].([]any) {
		ids = append(ids, raw.(map[string]any)["id"])
	}
	apiWantValue(t, "everyone ids", ids, []any{second, first})
	apiWantValue(t, "cap_chars", data["cap_chars"], 5)
	apiWantValue(t, "first_dropped_id", data["first_dropped_id"], first)

	status, data = apiJSON(t, st.h, "GET",
		"/api/lore?scope_kind=agent&scope_key="+author, st.user, "")
	if status != http.StatusOK {
		t.Fatalf("list agent: %d %v", status, data)
	}
	apiWantValue(t, "agent cap_chars", data["cap_chars"], 5)
	apiWantValue(t, "agent first_dropped_id", data["first_dropped_id"], mine)
}
