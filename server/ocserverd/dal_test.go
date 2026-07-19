package main

import (
	"path/filepath"
	"testing"
)

func newTestDAL(t *testing.T) *DAL {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "dal-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return NewDAL(db)
}

func fullMember(id string) Member {
	ok := true
	return Member{
		ID:               id,
		Name:             "Mira",
		Kind:             "assistant",
		RoleKey:          "assistant",
		Model:            "opus",
		Effort:           "high",
		DesiredState:     "online",
		DesiredMachineID: "m-abc123",
		WakingSince:      1.5,
		StoppingSince:    2.5,
		StoppedSince:     3.5,
		RefocusSince:     4.5,
		BankedCost:       6.25,
		LastOp:           "start",
		LastOpOK:         &ok,
		LastOpLog:        "spawned",
		LastOpReason:     "session_already_exists: tmux session \"member-m-1\" is already live",
		LastOpAt:         7.5,
		RosterStatus:     RosterStatusActive,
	}
}

func TestMemberCRUDRoundTrip(t *testing.T) {
	d := newTestDAL(t)

	if m, err := d.GetMember("m-1"); err != nil || m != nil {
		t.Fatalf("absent member must be (nil, nil), got (%v, %v)", m, err)
	}

	want := fullMember("m-1")
	if err := d.PutMember(want); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := d.GetMember("m-1")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.LastOpOK == nil || !*got.LastOpOK {
		t.Fatalf("last_op_ok must round-trip true, got %v", got.LastOpOK)
	}
	gotCopy := *got
	gotCopy.LastOpOK = want.LastOpOK // pointer compare aside, values checked above
	if gotCopy != want {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", gotCopy, want)
	}

	// Upsert: same id updates in place (no duplicate row).
	want.Name = "Renamed"
	want.LastOpOK = nil // and NULL round-trips as nil
	if err := d.PutMember(want); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	all, err := d.ListMembers()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 || all[0].Name != "Renamed" || all[0].LastOpOK != nil {
		t.Fatalf("upsert must keep one updated row, got %+v", all)
	}
}

func TestMemberKindCheckRejectsNonClosedSetValues(t *testing.T) {
	d := newTestDAL(t)
	for _, kind := range []string{"", "robot", "Assistant"} {
		m := fullMember("m-bad")
		m.Kind = kind
		if err := d.PutMember(m); err == nil {
			t.Fatalf("kind %q must be rejected by the CHECK constraint", kind)
		}
	}
	// The closed set passes; 'warden' covers the machine arm.
	m := fullMember("m-w")
	m.Kind = "warden"
	if err := d.PutMember(m); err != nil {
		t.Fatalf("kind 'warden' must pass: %v", err)
	}
}

// TestMemberOutsourceKindAndLinkedTask pins A案 P0 (migrations/00024): the
// widened kind CHECK admits 'outsource', and linked_task_id round-trips as a
// nullable *string. Pure schema prep — no existing path writes either yet, so
// this drives the DAL surface directly.
func TestMemberOutsourceKindAndLinkedTask(t *testing.T) {
	d := newTestDAL(t)

	// A kind='outsource' member with linked_task_id set stores + reads back.
	taskID := "t-42"
	m := fullMember("m-out")
	m.Kind = "outsource"
	m.LinkedTaskID = &taskID
	if err := d.PutMember(m); err != nil {
		t.Fatalf("outsource put must pass the widened CHECK: %v", err)
	}
	got, err := d.GetMember("m-out")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.Kind != "outsource" {
		t.Fatalf("kind must round-trip 'outsource', got %q", got.Kind)
	}
	if got.LinkedTaskID == nil || *got.LinkedTaskID != "t-42" {
		t.Fatalf("linked_task_id must round-trip 't-42', got %v", got.LinkedTaskID)
	}

	// NULL linked_task_id round-trips as nil (the default for every other row).
	plain := fullMember("m-plain")
	if err := d.PutMember(plain); err != nil {
		t.Fatalf("put plain: %v", err)
	}
	gotPlain, err := d.GetMember("m-plain")
	if err != nil || gotPlain == nil {
		t.Fatalf("get plain: %v %v", gotPlain, err)
	}
	if gotPlain.LinkedTaskID != nil {
		t.Fatalf("absent linked_task_id must round-trip nil, got %v", *gotPlain.LinkedTaskID)
	}

	// Upsert clears the binding back to NULL.
	m.LinkedTaskID = nil
	if err := d.PutMember(m); err != nil {
		t.Fatalf("upsert clear: %v", err)
	}
	if cleared, err := d.GetMember("m-out"); err != nil || cleared == nil || cleared.LinkedTaskID != nil {
		t.Fatalf("upsert must clear linked_task_id to nil, got %v %v", cleared, err)
	}
}

func TestMemberRosterStatusSoftDeleteKeepsRow(t *testing.T) {
	d := newTestDAL(t)
	m := fullMember("m-1")
	if err := d.PutMember(m); err != nil {
		t.Fatalf("put: %v", err)
	}

	// Dismiss = soft delete: the row survives with roster_status="removed"
	// (audit trail), and callers filter it out of active views.
	m.RosterStatus = RosterStatusRemoved
	if err := d.PutMember(m); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	all, err := d.ListMembers()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 || all[0].RosterStatus != RosterStatusRemoved {
		t.Fatalf("soft-deleted row must survive as removed, got %+v", all)
	}

	// Hard delete (custom-role cascade) physically drops the row.
	if deleted, err := d.HardDeleteMember("m-1"); err != nil || !deleted {
		t.Fatalf("hard delete: (%v, %v)", deleted, err)
	}
	if deleted, err := d.HardDeleteMember("m-1"); err != nil || deleted {
		t.Fatalf("second hard delete must report false, got (%v, %v)", deleted, err)
	}
	if got, err := d.GetMember("m-1"); err != nil || got != nil {
		t.Fatalf("hard-deleted member must be gone, got (%v, %v)", got, err)
	}
}

func TestChatPutListOrdersByTs(t *testing.T) {
	d := newTestDAL(t)
	msgs := []ChatMessage{
		{ID: "c-2", Sender: "owner", Recipient: "m-1", Body: "second", TS: 2.0},
		{ID: "c-1", Sender: "m-1", Recipient: "owner", Body: "first", TS: 1.0,
			Meta: map[string]any{"attachments": []any{map[string]any{"id": "a-1"}}}},
	}
	for _, m := range msgs {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put %s: %v", m.ID, err)
		}
	}
	got, err := d.ListChat()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0].ID != "c-1" || got[1].ID != "c-2" {
		t.Fatalf("list must be oldest→newest, got %+v", got)
	}
	atts, ok := got[0].Meta["attachments"].([]any)
	if !ok || len(atts) != 1 {
		t.Fatalf("meta JSON must round-trip, got %+v", got[0].Meta)
	}
	if got[1].Meta == nil || len(got[1].Meta) != 0 {
		t.Fatalf("nil meta must round-trip as empty map, got %+v", got[1].Meta)
	}
}

func TestChatListBeforeKeysetPagination(t *testing.T) {
	d := newTestDAL(t)
	// c-a/c-b/c-c share ts=2.0 — the id tie-break is the whole point of the
	// composite cursor (ts REAL can collide; a pure-ts cursor would drop or
	// duplicate collided messages across a page boundary).
	for _, m := range []ChatMessage{
		{ID: "c-1", Sender: "m-1", Recipient: "owner", TS: 1.0},
		{ID: "c-a", Sender: "owner", Recipient: "m-1", TS: 2.0},
		{ID: "c-b", Sender: "m-1", Recipient: "owner", TS: 2.0},
		{ID: "c-c", Sender: "m-1", Recipient: "m-2", TS: 2.0},   // inter-agent, still m-1's thread
		{ID: "c-5", Sender: "owner", Recipient: "m-2", TS: 5.0}, // not involving m-1
		{ID: "c-6", Sender: "owner", Recipient: "m-1", TS: 6.0},
	} {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	// The full stream is totally ordered (ts, id) — equal-ts messages come back
	// in id order, matching what the cursor pages by.
	all, err := d.ListChat()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	gotAll := make([]string, len(all))
	for i, m := range all {
		gotAll[i] = m.ID
	}
	if len(gotAll) != 6 || gotAll[0] != "c-1" || gotAll[1] != "c-a" ||
		gotAll[2] != "c-b" || gotAll[3] != "c-c" || gotAll[4] != "c-5" || gotAll[5] != "c-6" {
		t.Fatalf("ListChat must order by (ts, id), got %v", gotAll)
	}

	// Page back from c-6 within m-1's thread: the 2 newest strictly-older
	// messages are c-b and c-c (equal ts, id order), oldest→newest.
	page, err := d.ListChatBefore("m-1", 6.0, "c-6", 2)
	if err != nil {
		t.Fatalf("before: %v", err)
	}
	if len(page) != 2 || page[0].ID != "c-b" || page[1].ID != "c-c" {
		t.Fatalf("want [c-b c-c], got %+v", page)
	}

	// Page back from (2.0, c-b): the id tie-break keeps c-a (same ts, smaller
	// id) and excludes c-b/c-c.
	page, err = d.ListChatBefore("m-1", 2.0, "c-b", 30)
	if err != nil {
		t.Fatalf("before tie: %v", err)
	}
	if len(page) != 2 || page[0].ID != "c-1" || page[1].ID != "c-a" {
		t.Fatalf("tie-break page: want [c-1 c-a], got %+v", page)
	}

	// Exhausted history answers an empty page (the has-more=false signal).
	page, err = d.ListChatBefore("m-1", 1.0, "c-1", 30)
	if err != nil || len(page) != 0 {
		t.Fatalf("exhausted history must be empty, got (%+v, %v)", page, err)
	}

	// No participant filter pages the whole stream; negative limit uncaps;
	// limit 0 reads nothing.
	page, err = d.ListChatBefore("", 6.0, "c-6", -1)
	if err != nil || len(page) != 5 {
		t.Fatalf("unfiltered uncapped: want 5, got (%d, %v)", len(page), err)
	}
	if page, err := d.ListChatBefore("m-1", 6.0, "c-6", 0); err != nil || page != nil {
		t.Fatalf("limit 0 must read nothing, got (%+v, %v)", page, err)
	}
}

func TestChatListInvolvingFiltersAndCapsAscending(t *testing.T) {
	d := newTestDAL(t)
	for _, m := range []ChatMessage{
		{ID: "c-1", Sender: "m-1", Recipient: "owner", TS: 1.0},
		{ID: "c-2", Sender: "owner", Recipient: "m-1", TS: 2.0},
		{ID: "c-3", Sender: "owner", Recipient: "m-2", TS: 3.0}, // not involving m-1
		{ID: "c-4", Sender: "m-1", Recipient: "m-2", TS: 4.0},
	} {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	got, err := d.ListChatInvolving("m-1", 2)
	if err != nil {
		t.Fatalf("list involving: %v", err)
	}
	// Newest 2 involving m-1 are c-2 and c-4, returned oldest→newest.
	if len(got) != 2 || got[0].ID != "c-2" || got[1].ID != "c-4" {
		t.Fatalf("want [c-2 c-4], got %+v", got)
	}

	if got, err := d.ListChatInvolving("", 5); err != nil || got != nil {
		t.Fatalf("blank participant must read nothing, got (%v, %v)", got, err)
	}
	if got, err := d.ListChatInvolving("m-1", 0); err != nil || got != nil {
		t.Fatalf("non-positive limit must read nothing, got (%v, %v)", got, err)
	}
}

func TestChatAttachmentRoundTrip(t *testing.T) {
	d := newTestDAL(t)

	if a, err := d.GetChatAttachment("a-x"); err != nil || a != nil {
		t.Fatalf("absent attachment must be (nil, nil), got (%v, %v)", a, err)
	}

	name := "report.pdf"
	blob := []byte{0x00, 0x01, 0xff}
	if err := d.PutChatAttachment(ChatAttachment{
		ID: "a-1", Mime: "application/pdf", Data: blob, Filename: &name,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := d.GetChatAttachment("a-1")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.Mime != "application/pdf" || string(got.Data) != string(blob) ||
		got.Filename == nil || *got.Filename != name {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// A pasted image with no name keeps filename NULL.
	if err := d.PutChatAttachment(ChatAttachment{
		ID: "a-2", Mime: "image/png", Data: []byte("png"),
	}); err != nil {
		t.Fatalf("put unnamed: %v", err)
	}
	got, err = d.GetChatAttachment("a-2")
	if err != nil || got == nil || got.Filename != nil {
		t.Fatalf("unnamed attachment must round-trip nil filename, got %+v (%v)", got, err)
	}
}

func TestDeleteChatInvolvingCascadesMetaReferencedAttachments(t *testing.T) {
	d := newTestDAL(t)
	for _, id := range []string{"a-1", "a-2", "a-other", "a-shared", "a-card"} {
		if err := d.PutChatAttachment(ChatAttachment{ID: id, Data: []byte(id)}); err != nil {
			t.Fatalf("put attachment: %v", err)
		}
	}
	for _, m := range []ChatMessage{
		{ID: "c-1", Sender: "m-1", Recipient: "owner", TS: 1.0,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "a-1"}, map[string]any{"id": "a-2"},
			}}},
		{ID: "c-2", Sender: "owner", Recipient: "m-1", TS: 2.0},
		{ID: "c-3", Sender: "owner", Recipient: "m-2", TS: 3.0,
			Meta: map[string]any{"attachments": []any{map[string]any{"id": "a-other"}}}},
		// a-shared rides BOTH a deleted (c-4) and a surviving (c-5) message
		// (ref-form post_chat allows multi-reference); a-card rides a deleted
		// message AND a reply-card answer. Neither may be cascaded.
		{ID: "c-4", Sender: "m-1", Recipient: "owner", TS: 4.0,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "a-shared"}, map[string]any{"id": "a-card"},
			}}},
		{ID: "c-5", Sender: "owner", Recipient: "m-2", TS: 5.0,
			Meta: map[string]any{"attachments": []any{map[string]any{"id": "a-shared"}}}},
	} {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put chat: %v", err)
		}
	}
	if err := d.PutReplyCard(ReplyCard{
		ID: "rc-1", FromMember: "m-2", Kind: replyCardKindDecision,
		Summary: "s", Options: []string{"A"}, Status: replyCardStatusAnswered,
		AnswerAttachments: []any{
			map[string]any{"id": "a-card", "mime": "", "filename": ""},
		},
	}); err != nil {
		t.Fatalf("put reply card: %v", err)
	}

	msgs, atts, err := d.DeleteChatInvolving("m-1")
	if err != nil {
		t.Fatalf("delete involving: %v", err)
	}
	if msgs != 3 || atts != 2 {
		t.Fatalf("want (3 msgs, 2 atts), got (%d, %d)", msgs, atts)
	}
	if a, err := d.GetChatAttachment("a-1"); err != nil || a != nil {
		t.Fatalf("referenced blob must be cascaded, got (%v, %v)", a, err)
	}
	if a, err := d.GetChatAttachment("a-other"); err != nil || a == nil {
		t.Fatalf("unrelated blob must survive, got (%v, %v)", a, err)
	}
	if a, err := d.GetChatAttachment("a-shared"); err != nil || a == nil {
		t.Fatalf("blob still referenced by a surviving message must survive, got (%v, %v)", a, err)
	}
	if a, err := d.GetChatAttachment("a-card"); err != nil || a == nil {
		t.Fatalf("blob referenced by a reply-card answer must survive, got (%v, %v)", a, err)
	}
	rest, err := d.ListChat()
	if err != nil || len(rest) != 2 || rest[0].ID != "c-3" || rest[1].ID != "c-5" {
		t.Fatalf("only c-3/c-5 must survive, got %+v (%v)", rest, err)
	}
}

func TestChatReadCompositeKeyUpsertsOneWatermarkPerPair(t *testing.T) {
	d := newTestDAL(t)
	if _, _, err := d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "m-1", LastReadTS: 1.0}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, _, err := d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "m-1", LastReadTS: 2.0}); err != nil {
		t.Fatalf("put again: %v", err)
	}
	// The reversed pair is a DIFFERENT conversation direction: its own row.
	if _, _, err := d.PutChatRead(ChatRead{ReaderID: "m-1", PeerID: "owner", LastReadTS: 5.0}); err != nil {
		t.Fatalf("put reversed: %v", err)
	}

	all, err := d.ListChatReads("", "")
	if err != nil || len(all) != 2 {
		t.Fatalf("composite PK must keep one row per (reader, peer), got %+v (%v)", all, err)
	}
	byReader, err := d.ListChatReads("owner", "")
	if err != nil || len(byReader) != 1 || byReader[0].LastReadTS != 2.0 {
		t.Fatalf("reader filter: got %+v (%v)", byReader, err)
	}
	byPeer, err := d.ListChatReads("", "owner")
	if err != nil || len(byPeer) != 1 || byPeer[0].ReaderID != "m-1" {
		t.Fatalf("peer filter: got %+v (%v)", byPeer, err)
	}
}

func TestPutChatReadMonotonicNeverRewinds(t *testing.T) {
	d := newTestDAL(t)
	if _, _, err := d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "m-1", LastReadTS: 10.0}); err != nil {
		t.Fatalf("put: %v", err)
	}
	// A stale (lower) report is a no-op; the EFFECTIVE watermark comes back
	// and the report is flagged NOT advanced (the caller's no-fan signal).
	eff, advanced, err := d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "m-1", LastReadTS: 3.0})
	if err != nil {
		t.Fatalf("stale put: %v", err)
	}
	if eff.LastReadTS != 10.0 || advanced {
		t.Fatalf("stale report must keep the higher watermark and not advance, got ts=%v advanced=%v", eff.LastReadTS, advanced)
	}
	// An equal report is a no-op too (Python: existing >= report → no write).
	eff, advanced, err = d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "m-1", LastReadTS: 10.0})
	if err != nil || eff.LastReadTS != 10.0 || advanced {
		t.Fatalf("equal report must not advance, got %+v advanced=%v (%v)", eff, advanced, err)
	}
	// A newer report advances.
	eff, advanced, err = d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "m-1", LastReadTS: 12.0})
	if err != nil || eff.LastReadTS != 12.0 || !advanced {
		t.Fatalf("newer report must advance, got %+v advanced=%v (%v)", eff, advanced, err)
	}
}

func TestDeleteChatReadsInvolvingRemovesReaderAndPeerRows(t *testing.T) {
	d := newTestDAL(t)
	for _, r := range []ChatRead{
		{ReaderID: "m-1", PeerID: "owner", LastReadTS: 1.0},
		{ReaderID: "owner", PeerID: "m-1", LastReadTS: 2.0},
		{ReaderID: "owner", PeerID: "m-2", LastReadTS: 3.0},
	} {
		if _, _, err := d.PutChatRead(r); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	n, err := d.DeleteChatReadsInvolving("m-1")
	if err != nil || n != 2 {
		t.Fatalf("want 2 deleted, got %d (%v)", n, err)
	}
	rest, err := d.ListChatReads("", "")
	if err != nil || len(rest) != 1 || rest[0].PeerID != "m-2" {
		t.Fatalf("only the m-2 watermark must survive, got %+v (%v)", rest, err)
	}
}

func TestUserContextSingleRowUpsert(t *testing.T) {
	d := newTestDAL(t)

	if uc, err := d.GetUserContext(); err != nil || uc != nil {
		t.Fatalf("never-written block must be (nil, nil), got (%v, %v)", uc, err)
	}

	if err := d.PutUserContext(UserContext{Text: "custom boot context"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	uc, err := d.GetUserContext()
	if err != nil || uc == nil || uc.Text != "custom boot context" || uc.Tombstoned {
		t.Fatalf("round-trip: got %+v (%v)", uc, err)
	}

	// Reset (tombstone) rides the same single row.
	if err := d.PutUserContext(UserContext{Text: "", Tombstoned: true}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	uc, err = d.GetUserContext()
	if err != nil || uc == nil || !uc.Tombstoned {
		t.Fatalf("tombstone round-trip: got %+v (%v)", uc, err)
	}
	var count int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM user_context`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("table must stay single-row, got %d (%v)", count, err)
	}

	// The schema CHECK pins the row id: a second row is unrepresentable.
	if _, err := d.db.Exec(
		`INSERT INTO user_context (id, text, tombstoned) VALUES (2, 'x', 0)`,
	); err == nil {
		t.Fatal("id != 1 must be rejected by the single-row CHECK")
	}
}

func TestRoleDefCRUDRoundTrip(t *testing.T) {
	d := newTestDAL(t)

	if rd, err := d.GetRoleDef("assistant"); err != nil || rd != nil {
		t.Fatalf("never-edited overlay must be (nil, nil), got (%v, %v)", rd, err)
	}

	want := RoleDef{RoleKey: "assistant", Name: "Assistant", DefinitionMD: "# role"}
	if err := d.PutRoleDef(want); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := d.GetRoleDef("assistant")
	if err != nil || got == nil || *got != want {
		t.Fatalf("round-trip: got %+v (%v)", got, err)
	}

	// Upsert with tombstone (seed-role reset) keeps one row.
	want.Tombstoned = true
	if err := d.PutRoleDef(want); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	all, err := d.ListRoleDefs()
	if err != nil || len(all) != 1 || !all[0].Tombstoned {
		t.Fatalf("list after tombstone: got %+v (%v)", all, err)
	}

	// Hard delete (custom role) drops the row; absent key reports false.
	if deleted, err := d.DeleteRoleDef("assistant"); err != nil || !deleted {
		t.Fatalf("delete: (%v, %v)", deleted, err)
	}
	if deleted, err := d.DeleteRoleDef("assistant"); err != nil || deleted {
		t.Fatalf("second delete must report false, got (%v, %v)", deleted, err)
	}
}

func TestLessonsCompositeKeyAndRoleCascade(t *testing.T) {
	d := newTestDAL(t)

	if l, err := d.GetLessons("assistant", "default"); err != nil || l != nil {
		t.Fatalf("never-edited lessons must be (nil, nil), got (%v, %v)", l, err)
	}

	if err := d.PutLessons(Lessons{RoleKey: "assistant", TaskType: "default", Text: "v1"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Same (role, task) upserts in place — composite-PK uniqueness.
	if err := d.PutLessons(Lessons{RoleKey: "assistant", TaskType: "default", Text: "v2"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := d.PutLessons(Lessons{RoleKey: "researcher", TaskType: "default", Text: "other"}); err != nil {
		t.Fatalf("put other role: %v", err)
	}
	got, err := d.GetLessons("assistant", "default")
	if err != nil || got == nil || got.Text != "v2" {
		t.Fatalf("upsert must replace text, got %+v (%v)", got, err)
	}

	n, err := d.DeleteLessonsForRole("assistant")
	if err != nil || n != 1 {
		t.Fatalf("role cascade: want 1 deleted, got %d (%v)", n, err)
	}
	if l, err := d.GetLessons("researcher", "default"); err != nil || l == nil {
		t.Fatalf("other role's lessons must survive, got (%v, %v)", l, err)
	}
}

func TestAliasOverlaysRoundTripAndFoldMapSkipsEmpty(t *testing.T) {
	d := newTestDAL(t)

	if a, err := d.GetAccountAlias("acct-1"); err != nil || a != nil {
		t.Fatalf("absent account alias must be (nil, nil), got (%v, %v)", a, err)
	}
	if a, err := d.GetMachineAlias("m-1"); err != nil || a != nil {
		t.Fatalf("absent machine alias must be (nil, nil), got (%v, %v)", a, err)
	}

	if err := d.PutAccountAlias(AccountAlias{Account: "acct-1", DisplayName: "Work"}); err != nil {
		t.Fatalf("put account: %v", err)
	}
	if err := d.PutAccountAlias(AccountAlias{Account: "acct-2", DisplayName: ""}); err != nil {
		t.Fatalf("put empty account: %v", err)
	}
	if err := d.PutMachineAlias(MachineAlias{MachineID: "m-1", DisplayName: "Studio"}); err != nil {
		t.Fatalf("put machine: %v", err)
	}
	// Rename upserts on the same key.
	if err := d.PutMachineAlias(MachineAlias{MachineID: "m-1", DisplayName: "Studio Mac"}); err != nil {
		t.Fatalf("rename machine: %v", err)
	}

	a, err := d.GetAccountAlias("acct-1")
	if err != nil || a == nil || a.DisplayName != "Work" {
		t.Fatalf("account round-trip: got %+v (%v)", a, err)
	}
	m, err := d.GetMachineAlias("m-1")
	if err != nil || m == nil || m.DisplayName != "Studio Mac" {
		t.Fatalf("machine rename round-trip: got %+v (%v)", m, err)
	}

	// The fold maps skip empty display names (absence folds to the id itself).
	accounts, err := d.AccountDisplayNames()
	if err != nil || len(accounts) != 1 || accounts["acct-1"] != "Work" {
		t.Fatalf("account fold map: got %+v (%v)", accounts, err)
	}
	machines, err := d.MachineDisplayNames()
	if err != nil || len(machines) != 1 || machines["m-1"] != "Studio Mac" {
		t.Fatalf("machine fold map: got %+v (%v)", machines, err)
	}
}

func TestSettingGetPutRoundTripAndUpsert(t *testing.T) {
	d := newTestDAL(t)

	if v, err := d.GetSetting("auth.token_ttl"); err != nil || v != nil {
		t.Fatalf("a never-written key must be (nil, nil), got (%v, %v)", v, err)
	}

	if err := d.PutSetting("auth.token_ttl", "3600"); err != nil {
		t.Fatalf("put: %v", err)
	}
	v, err := d.GetSetting("auth.token_ttl")
	if err != nil || v == nil || *v != "3600" {
		t.Fatalf("get after put: got (%v, %v)", v, err)
	}

	// Upsert overwrites in place and advances updated_at.
	var firstAt float64
	if err := d.db.QueryRow(`SELECT updated_at FROM setting WHERE key = 'auth.token_ttl'`).Scan(&firstAt); err != nil {
		t.Fatal(err)
	}
	if err := d.PutSetting("auth.token_ttl", "7200"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	v, err = d.GetSetting("auth.token_ttl")
	if err != nil || v == nil || *v != "7200" {
		t.Fatalf("get after upsert: got (%v, %v)", v, err)
	}
	var secondAt float64
	if err := d.db.QueryRow(`SELECT updated_at FROM setting WHERE key = 'auth.token_ttl'`).Scan(&secondAt); err != nil {
		t.Fatal(err)
	}
	if firstAt <= 0 || secondAt < firstAt {
		t.Fatalf("updated_at must be stamped and monotonic: %v -> %v", firstAt, secondAt)
	}
}

// TestDeleteChatInvolvingSparesQuestionSideCardRefs pins the T-5e8a GC seam:
// a card's QUESTION-side attachments are stamped into its companion message's
// meta, so removing the initiating member puts those blob ids on the delete
// candidate list — the surviving card row (cards live forever) must veto the
// drop, exactly as answer_attachments already does. A blob referenced by
// nothing but the deleted messages still cascades.
func TestDeleteChatInvolvingSparesQuestionSideCardRefs(t *testing.T) {
	d := newTestDAL(t)
	for _, id := range []string{"a-question", "a-loose"} {
		if err := d.PutChatAttachment(ChatAttachment{ID: id, Data: []byte(id)}); err != nil {
			t.Fatalf("put attachment: %v", err)
		}
	}
	// The companion message of the card (meta stamps the question refs) plus
	// an ordinary message referencing a blob nothing else holds.
	for _, m := range []ChatMessage{
		{ID: "c-card", Sender: "m-1", Recipient: "owner", TS: 1.0,
			Meta: map[string]any{
				"reply_card_id": "rc-q",
				"attachments":   []any{map[string]any{"id": "a-question"}},
			}},
		{ID: "c-loose", Sender: "m-1", Recipient: "owner", TS: 2.0,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "a-loose"},
			}}},
	} {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put chat: %v", err)
		}
	}
	if err := d.PutReplyCard(ReplyCard{
		ID: "rc-q", FromMember: "m-1", Kind: replyCardKindDecision,
		Summary: "s", Options: []string{"A"}, Status: replyCardStatusWaiting,
		ChatMessageID: "c-card",
		Attachments: []any{
			map[string]any{"id": "a-question", "mime": "", "filename": ""},
		},
	}); err != nil {
		t.Fatalf("put reply card: %v", err)
	}

	msgs, atts, err := d.DeleteChatInvolving("m-1")
	if err != nil {
		t.Fatalf("delete involving: %v", err)
	}
	if msgs != 2 || atts != 1 {
		t.Fatalf("want (2 msgs, 1 att), got (%d, %d)", msgs, atts)
	}
	if a, err := d.GetChatAttachment("a-question"); err != nil || a == nil {
		t.Fatalf("blob referenced by a surviving card's question attachments must survive, got (%v, %v)", a, err)
	}
	if a, err := d.GetChatAttachment("a-loose"); err != nil || a != nil {
		t.Fatalf("blob referenced only by deleted messages must cascade, got (%v, %v)", a, err)
	}
}
