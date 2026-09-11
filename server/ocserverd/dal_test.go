package main

// dal_test.go — the behaviour of server/ocserverd/dal.go over a fresh migrated
// database per test: what a write stores, what a read answers, and what a
// failed write leaves behind.

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestScanMember(t *testing.T) {
	d := newAPITestDAL(t)
	linked := "T-1"
	full := dalTestMember("ann", "Ann")
	full.Kind = KindOutsource
	full.Codename = "O-7"
	full.LinkedTaskID = &linked
	dalPutMember(t, d, full)
	bare := dalPutMember(t, d, Member{ID: "bob", Name: "Bob", Kind: KindStaff})

	query := `SELECT ` + memberColumns + ` FROM member WHERE id = ?`
	got, err := scanMember(d.rdb.QueryRow(query, "ann"))
	if err != nil {
		t.Fatalf("scanMember(ann): %v", err)
	}
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("scanMember(ann):\n got %+v\nwant %+v", got, full)
	}

	got, err = scanMember(d.rdb.QueryRow(query, "bob"))
	if err != nil {
		t.Fatalf("scanMember(bob): %v", err)
	}
	if !reflect.DeepEqual(got, bare) {
		t.Fatalf("scanMember(bob):\n got %+v\nwant %+v", got, bare)
	}
	if got.LastOpOK != nil || got.LinkedTaskID != nil || got.Codename != "" {
		t.Fatalf("scanMember(bob): the three nullable columns must read back as nil/nil/%q, got %v/%v/%q",
			"", got.LastOpOK, got.LinkedTaskID, got.Codename)
	}

	if _, err := scanMember(d.rdb.QueryRow(query, "ghost")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("scanMember(ghost): want sql.ErrNoRows, got %v", err)
	}
}

func TestListMembers(t *testing.T) {
	d := newAPITestDAL(t)
	empty, err := d.ListMembers()
	if err != nil {
		t.Fatalf("ListMembers on an empty roster: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("ListMembers on an empty roster: want none, got %+v", empty)
	}

	bea := dalTestMember("bea", "bea")
	bea.Kind = KindWarden
	dalPutMember(t, d, bea)
	al := dalTestMember("al", "Al")
	dalPutMember(t, d, al)
	carl := dalTestMember("carl", "carl")
	carl.Kind = KindOutsource
	carl.Codename = "O-9"
	carl.RosterStatus = RosterStatusRemoved
	dalPutMember(t, d, carl)

	got, err := d.ListMembers()
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	want := []Member{al, bea, carl}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListMembers:\n got %+v\nwant %+v", got, want)
	}
}

func TestGetMember(t *testing.T) {
	d := newAPITestDAL(t)
	dalWantMember(t, d, dalPutMember(t, d, dalTestMember("ann", "Ann")))

	got, err := d.GetMember("ghost")
	if err != nil {
		t.Fatalf("GetMember(ghost): %v", err)
	}
	if got != nil {
		t.Fatalf("GetMember(ghost): want nil, got %+v", *got)
	}
}

func TestAddAccountSpend(t *testing.T) {
	d := newAPITestDAL(t)
	for _, delta := range []float64{1.5, 2.25} {
		if err := d.AddAccountSpend("acct-a", delta); err != nil {
			t.Fatalf("AddAccountSpend(acct-a, %v): %v", delta, err)
		}
	}
	if err := d.AddAccountSpend("acct-b", 4); err != nil {
		t.Fatalf("AddAccountSpend(acct-b): %v", err)
	}
	for _, noop := range []struct {
		account string
		delta   float64
	}{{"", 5}, {"acct-a", 0}, {"acct-a", -3}} {
		if err := d.AddAccountSpend(noop.account, noop.delta); err != nil {
			t.Fatalf("AddAccountSpend(%q, %v): %v", noop.account, noop.delta, err)
		}
	}

	got, err := d.ListAccountSpend()
	if err != nil {
		t.Fatalf("ListAccountSpend: %v", err)
	}
	want := map[string]float64{"acct-a": 3.75, "acct-b": 4}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListAccountSpend: want %v, got %v", want, got)
	}
}

func TestListAccountSpend(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.ListAccountSpend()
	if err != nil {
		t.Fatalf("ListAccountSpend before any report: %v", err)
	}
	if !reflect.DeepEqual(got, map[string]float64{}) {
		t.Fatalf("ListAccountSpend before any report: want an empty map, got %v", got)
	}

	for account, delta := range map[string]float64{"acct-a": 1, "acct-b": 2.5, "acct-c": 7} {
		if err := d.AddAccountSpend(account, delta); err != nil {
			t.Fatalf("AddAccountSpend(%q): %v", account, err)
		}
	}
	got, err = d.ListAccountSpend()
	if err != nil {
		t.Fatalf("ListAccountSpend: %v", err)
	}
	want := map[string]float64{"acct-a": 1, "acct-b": 2.5, "acct-c": 7}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListAccountSpend: want %v, got %v", want, got)
	}
}

func TestZeroAccountSpend(t *testing.T) {
	d := newAPITestDAL(t)
	if err := d.AddAccountSpend("acct-a", 9.5); err != nil {
		t.Fatalf("AddAccountSpend(acct-a): %v", err)
	}
	if err := d.AddAccountSpend("acct-b", 3); err != nil {
		t.Fatalf("AddAccountSpend(acct-b): %v", err)
	}

	had, err := d.ZeroAccountSpend("acct-a")
	if err != nil {
		t.Fatalf("ZeroAccountSpend(acct-a): %v", err)
	}
	if had != 9.5 {
		t.Fatalf("ZeroAccountSpend(acct-a): want the receipt 9.5, got %v", had)
	}
	had, err = d.ZeroAccountSpend("acct-a")
	if err != nil {
		t.Fatalf("ZeroAccountSpend(acct-a) again: %v", err)
	}
	if had != 0 {
		t.Fatalf("ZeroAccountSpend(acct-a) again: want 0, got %v", had)
	}
	had, err = d.ZeroAccountSpend("never-reported")
	if err != nil {
		t.Fatalf("ZeroAccountSpend(never-reported): %v", err)
	}
	if had != 0 {
		t.Fatalf("ZeroAccountSpend(never-reported): want 0, got %v", had)
	}

	got, err := d.ListAccountSpend()
	if err != nil {
		t.Fatalf("ListAccountSpend: %v", err)
	}
	want := map[string]float64{"acct-a": 0, "acct-b": 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListAccountSpend: want %v, got %v", want, got)
	}
}

func TestAddMemberBankedCost(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

	if err := d.AddMemberBankedCost("ann", 2.5); err != nil {
		t.Fatalf("AddMemberBankedCost: %v", err)
	}
	if err := d.AddMemberBankedCost("ann", 1.25); err != nil {
		t.Fatalf("AddMemberBankedCost: %v", err)
	}
	if err := d.AddMemberBankedCost("ghost", 100); err != nil {
		t.Fatalf("AddMemberBankedCost on a missing row: %v", err)
	}

	ann.BankedCost = 16.25
	dalWantMember(t, d, ann)
	dalWantMember(t, d, bob)
	if got, err := d.GetMember("ghost"); err != nil || got != nil {
		t.Fatalf("AddMemberBankedCost must not create a row: %+v, %v", got, err)
	}
}

func TestZeroMemberBankedCost(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

	had, err := d.ZeroMemberBankedCost("ann")
	if err != nil {
		t.Fatalf("ZeroMemberBankedCost: %v", err)
	}
	if had != 12.5 {
		t.Fatalf("ZeroMemberBankedCost: want the receipt 12.5, got %v", had)
	}
	had, err = d.ZeroMemberBankedCost("ann")
	if err != nil {
		t.Fatalf("ZeroMemberBankedCost again: %v", err)
	}
	if had != 0 {
		t.Fatalf("ZeroMemberBankedCost again: want 0, got %v", had)
	}
	had, err = d.ZeroMemberBankedCost("ghost")
	if err != nil {
		t.Fatalf("ZeroMemberBankedCost on a missing row: %v", err)
	}
	if had != 0 {
		t.Fatalf("ZeroMemberBankedCost on a missing row: want 0, got %v", had)
	}

	ann.BankedCost = 0
	dalWantMember(t, d, ann)
	dalWantMember(t, d, bob)
	if got, err := d.GetMember("ghost"); err != nil || got != nil {
		t.Fatalf("ZeroMemberBankedCost must not create a row: %+v, %v", got, err)
	}
}

func TestSetMemberHandoverNoticedTS(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

	if err := d.SetMemberHandoverNoticedTS("ann", 1800000000); err != nil {
		t.Fatalf("SetMemberHandoverNoticedTS: %v", err)
	}
	ann.HandoverNoticedTS = 1800000000
	dalWantMember(t, d, ann)

	if err := d.SetMemberHandoverNoticedTS("ann", 0); err != nil {
		t.Fatalf("SetMemberHandoverNoticedTS(0): %v", err)
	}
	ann.HandoverNoticedTS = 0
	dalWantMember(t, d, ann)
	dalWantMember(t, d, bob)

	if err := d.SetMemberHandoverNoticedTS("ghost", 5); err != nil {
		t.Fatalf("SetMemberHandoverNoticedTS on a missing row: %v", err)
	}
	if got, err := d.GetMember("ghost"); err != nil || got != nil {
		t.Fatalf("SetMemberHandoverNoticedTS must not create a row: %+v, %v", got, err)
	}
}

func TestSetMemberSessionBootTS(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

	if err := d.SetMemberSessionBootTS("ann", 1800000000); err != nil {
		t.Fatalf("SetMemberSessionBootTS: %v", err)
	}
	ann.SessionBootTS = 1800000000
	dalWantMember(t, d, ann)

	if err := d.SetMemberSessionBootTS("ann", 0); err != nil {
		t.Fatalf("SetMemberSessionBootTS(0): %v", err)
	}
	ann.SessionBootTS = 0
	dalWantMember(t, d, ann)
	dalWantMember(t, d, bob)

	if err := d.SetMemberSessionBootTS("ghost", 5); err != nil {
		t.Fatalf("SetMemberSessionBootTS on a missing row: %v", err)
	}
	if got, err := d.GetMember("ghost"); err != nil || got != nil {
		t.Fatalf("SetMemberSessionBootTS must not create a row: %+v, %v", got, err)
	}
}

func TestSetMemberWakingSince(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

	if err := d.SetMemberWakingSince("ann", 1800000000); err != nil {
		t.Fatalf("SetMemberWakingSince: %v", err)
	}
	ann.WakingSince = 1800000000
	dalWantMember(t, d, ann)

	if err := d.SetMemberWakingSince("ann", 0); err != nil {
		t.Fatalf("SetMemberWakingSince(0): %v", err)
	}
	ann.WakingSince = 0
	dalWantMember(t, d, ann)
	dalWantMember(t, d, bob)

	if err := d.SetMemberWakingSince("ghost", 5); err != nil {
		t.Fatalf("SetMemberWakingSince on a missing row: %v", err)
	}
	if got, err := d.GetMember("ghost"); err != nil || got != nil {
		t.Fatalf("SetMemberWakingSince must not create a row: %+v, %v", got, err)
	}
}

func TestSetMemberWindDownAnchors(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

	if err := d.SetMemberWindDownAnchors("ann", 1800000001, 1800000002, 1800000003, "accelerated_stop"); err != nil {
		t.Fatalf("SetMemberWindDownAnchors: %v", err)
	}
	ann.StoppingSince = 1800000001
	ann.StoppedSince = 1800000002
	ann.RefocusSince = 1800000003
	ann.RefocusOp = "accelerated_stop"
	dalWantMember(t, d, ann)

	if err := d.SetMemberWindDownAnchors("ann", 0, 0, 0, ""); err != nil {
		t.Fatalf("SetMemberWindDownAnchors(cleared): %v", err)
	}
	ann.StoppingSince = 0
	ann.StoppedSince = 0
	ann.RefocusSince = 0
	ann.RefocusOp = ""
	dalWantMember(t, d, ann)
	dalWantMember(t, d, bob)

	if err := d.SetMemberWindDownAnchors("ghost", 1, 2, 3, "refocus"); err != nil {
		t.Fatalf("SetMemberWindDownAnchors on a missing row: %v", err)
	}
	if got, err := d.GetMember("ghost"); err != nil || got != nil {
		t.Fatalf("SetMemberWindDownAnchors must not create a row: %+v, %v", got, err)
	}
}

func TestSetMemberDesiredMachineID(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

	if err := d.SetMemberDesiredMachineID("ann", "mac-9"); err != nil {
		t.Fatalf("SetMemberDesiredMachineID: %v", err)
	}
	ann.DesiredMachineID = "mac-9"
	dalWantMember(t, d, ann)

	if err := d.SetMemberDesiredMachineID("ann", ""); err != nil {
		t.Fatalf("SetMemberDesiredMachineID(unpinned): %v", err)
	}
	ann.DesiredMachineID = ""
	dalWantMember(t, d, ann)
	dalWantMember(t, d, bob)

	if err := d.SetMemberDesiredMachineID("ghost", "mac-9"); err != nil {
		t.Fatalf("SetMemberDesiredMachineID on a missing row: %v", err)
	}
	if got, err := d.GetMember("ghost"); err != nil || got != nil {
		t.Fatalf("SetMemberDesiredMachineID must not create a row: %+v, %v", got, err)
	}
}

func TestSetMemberModel(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

	if err := d.SetMemberModel("ann", "opus"); err != nil {
		t.Fatalf("SetMemberModel: %v", err)
	}
	ann.Model = "opus"
	dalWantMember(t, d, ann)

	if err := d.SetMemberModel("ann", ""); err != nil {
		t.Fatalf("SetMemberModel(unset): %v", err)
	}
	ann.Model = ""
	dalWantMember(t, d, ann)
	dalWantMember(t, d, bob)

	if err := d.SetMemberModel("ghost", "opus"); err != nil {
		t.Fatalf("SetMemberModel on a missing row: %v", err)
	}
	if got, err := d.GetMember("ghost"); err != nil || got != nil {
		t.Fatalf("SetMemberModel must not create a row: %+v, %v", got, err)
	}
}

func TestSetMemberRuntime(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

	if err := d.SetMemberRuntime("ann", "codex"); err != nil {
		t.Fatalf("SetMemberRuntime: %v", err)
	}
	ann.Runtime = "codex"
	dalWantMember(t, d, ann)

	if err := d.SetMemberRuntime("ann", ""); err != nil {
		t.Fatalf("SetMemberRuntime(nobody has picked yet): %v", err)
	}
	ann.Runtime = ""
	dalWantMember(t, d, ann)
	dalWantMember(t, d, bob)

	if err := d.SetMemberRuntime("ghost", "codex"); err != nil {
		t.Fatalf("SetMemberRuntime on a missing row: %v", err)
	}
	if got, err := d.GetMember("ghost"); err != nil || got != nil {
		t.Fatalf("SetMemberRuntime must not create a row: %+v, %v", got, err)
	}
}

func TestSetMemberEffort(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

	if err := d.SetMemberEffort("ann", "low"); err != nil {
		t.Fatalf("SetMemberEffort: %v", err)
	}
	ann.Effort = "low"
	dalWantMember(t, d, ann)

	if err := d.SetMemberEffort("ann", ""); err != nil {
		t.Fatalf("SetMemberEffort(unset): %v", err)
	}
	ann.Effort = ""
	dalWantMember(t, d, ann)
	dalWantMember(t, d, bob)

	if err := d.SetMemberEffort("ghost", "low"); err != nil {
		t.Fatalf("SetMemberEffort on a missing row: %v", err)
	}
	if got, err := d.GetMember("ghost"); err != nil || got != nil {
		t.Fatalf("SetMemberEffort must not create a row: %+v, %v", got, err)
	}
}

func TestSetMemberOpReceipt(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

	failed := false
	if err := d.SetMemberOpReceipt("ann", "relocate", &failed, "boom", "dispatch: refused", 1800000000); err != nil {
		t.Fatalf("SetMemberOpReceipt: %v", err)
	}
	ann.LastOp = "relocate"
	ann.LastOpOK = &failed
	ann.LastOpLog = "boom"
	ann.LastOpReason = "dispatch: refused"
	ann.LastOpAt = 1800000000
	dalWantMember(t, d, ann)

	if err := d.SetMemberOpReceipt("ann", "", nil, "", "", 0); err != nil {
		t.Fatalf("SetMemberOpReceipt(withdrawn): %v", err)
	}
	ann.LastOp = ""
	ann.LastOpOK = nil
	ann.LastOpLog = ""
	ann.LastOpReason = ""
	ann.LastOpAt = 0
	dalWantMember(t, d, ann)
	dalWantMember(t, d, bob)

	if err := d.SetMemberOpReceipt("ghost", "relocate", &failed, "", "", 1); err != nil {
		t.Fatalf("SetMemberOpReceipt on a missing row: %v", err)
	}
	if got, err := d.GetMember("ghost"); err != nil || got != nil {
		t.Fatalf("SetMemberOpReceipt must not create a row: %+v, %v", got, err)
	}
}

func TestHardDeleteMember(t *testing.T) {
	d := newAPITestDAL(t)
	ann := dalTestMember("ann", "Ann")
	dalPutMember(t, d, ann)
	bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))
	for _, id := range []string{"blob-ann", "blob-loose"} {
		if err := d.PutChatAttachment(ChatAttachment{ID: id, Mime: "image/png", Data: []byte(id)}); err != nil {
			t.Fatalf("PutChatAttachment(%q): %v", id, err)
		}
	}

	deleted, err := d.HardDeleteMember("ann")
	if err != nil {
		t.Fatalf("HardDeleteMember(ann): %v", err)
	}
	if !deleted {
		t.Fatalf("HardDeleteMember(ann): want true, got false")
	}
	if got, err := d.GetMember("ann"); err != nil || got != nil {
		t.Fatalf("HardDeleteMember must remove the row, got %+v, %v", got, err)
	}
	// A member row references no blob any more (the personal-avatar pointer is
	// retired), so a hard delete collects nothing from the attachment table.
	if got, err := d.GetChatAttachment("blob-ann"); err != nil || got == nil {
		t.Fatalf("HardDeleteMember must leave unrelated blobs alone, got %+v, %v", got, err)
	}
	loose, err := d.GetChatAttachment("blob-loose")
	if err != nil {
		t.Fatalf("GetChatAttachment(blob-loose): %v", err)
	}
	if !reflect.DeepEqual(loose, &ChatAttachment{ID: "blob-loose", Mime: "image/png", Data: []byte("blob-loose")}) {
		t.Fatalf("HardDeleteMember must leave another member's blobs alone, got %+v", loose)
	}
	dalWantMember(t, d, bob)

	deleted, err = d.HardDeleteMember("ann")
	if err != nil {
		t.Fatalf("HardDeleteMember(ann) again: %v", err)
	}
	if deleted {
		t.Fatalf("HardDeleteMember on an absent row: want false, got true")
	}

	deleted, err = d.HardDeleteMember("bob")
	if err != nil {
		t.Fatalf("HardDeleteMember(bob): %v", err)
	}
	if !deleted {
		t.Fatalf("HardDeleteMember(bob): want true, got false")
	}
	rest, err := d.ListMembers()
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("ListMembers after both deletes: want none, got %+v", rest)
	}
}

func TestScanChat(t *testing.T) {
	d := newAPITestDAL(t)
	rich := ChatMessage{
		ID: "c1", Sender: "ann", Recipient: "bob", Body: "hi", TS: 1700000001.5,
		Meta: map[string]any{
			"attachments": []any{map[string]any{"id": "blob-1", "mime": "image/png"}},
			"count":       float64(2),
			"flag":        true,
		},
	}
	dalPutChats(t, d, rich, ChatMessage{ID: "c2", Sender: "bob", Recipient: "ann", Body: "yo", TS: 2})

	query := `SELECT id, sender, recipient, body, ts, meta FROM chat_message WHERE id = ?`
	got, err := scanChat(d.rdb.QueryRow(query, "c1"))
	if err != nil {
		t.Fatalf("scanChat(c1): %v", err)
	}
	if !reflect.DeepEqual(got, rich) {
		t.Fatalf("scanChat(c1):\n got %+v\nwant %+v", got, rich)
	}

	got, err = scanChat(d.rdb.QueryRow(query, "c2"))
	if err != nil {
		t.Fatalf("scanChat(c2): %v", err)
	}
	want := ChatMessage{ID: "c2", Sender: "bob", Recipient: "ann", Body: "yo", TS: 2, Meta: map[string]any{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanChat(c2): a message stored with no meta must read back as an empty object:\n got %+v\nwant %+v", got, want)
	}

	if _, err := d.wdb.Exec(
		`INSERT INTO chat_message (id, sender, recipient, body, ts, meta) VALUES (?, ?, ?, ?, ?, ?)`,
		"c3", "ann", "bob", "broken", 3, "not json",
	); err != nil {
		t.Fatalf("seed a corrupt meta row: %v", err)
	}
	_, err = scanChat(d.rdb.QueryRow(query, "c3"))
	if err == nil || !strings.Contains(err.Error(), "chat_message c3: bad meta JSON") {
		t.Fatalf("scanChat(c3): want a bad-meta-JSON error naming the row, got %v", err)
	}

	if _, err := scanChat(d.rdb.QueryRow(query, "ghost")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("scanChat(ghost): want sql.ErrNoRows, got %v", err)
	}
}

func TestListChat(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.ListChat()
	if err != nil {
		t.Fatalf("ListChat on an empty stream: %v", err)
	}
	dalWantChats(t, "ListChat on an empty stream", got, nil)

	late := dalChat("c9", "ann", "bob", 300)
	tieB := dalChat("c5", "bob", "ann", 200)
	tieA := dalChat("c4", "ann", "bob", 200)
	early := dalChat("c1", "carl", "ann", 100)
	dalPutChats(t, d, late, tieB, tieA, early)

	got, err = d.ListChat()
	if err != nil {
		t.Fatalf("ListChat: %v", err)
	}
	dalWantChats(t, "ListChat", got, []ChatMessage{early, tieA, tieB, late})
}

func TestAppendSQL(t *testing.T) {
	d := newAPITestDAL(t)
	dalPutChats(t, d,
		dalChat("m1", "ann", "bob", 1),
		dalChat("m2", "bob", "ann", 2),
		dalChat("m3", "ann", "carl", 3),
		dalChat("m4", "carl", "bob", 4),
	)
	selectIDs := func(t *testing.T, query string, args []any) []string {
		t.Helper()
		rows, err := d.rdb.Query(query, args...)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out = append(out, id)
		}
		return out
	}

	for _, tc := range []struct {
		name string
		f    chatListFilter
		want []string
	}{
		{"no filter at all", chatListFilter{}, []string{"m1", "m2", "m3", "m4"}},
		{"participant matches either side", chatListFilter{participant: "ann"}, []string{"m1", "m2", "m3"}},
		{"sender is one-sided", chatListFilter{sender: "ann"}, []string{"m1", "m3"}},
		{"recipient is one-sided", chatListFilter{recipient: "bob"}, []string{"m1", "m4"}},
		{"the three conjuncts AND", chatListFilter{participant: "ann", sender: "ann", recipient: "bob"}, []string{"m1"}},
		{"an unsatisfiable conjunction reads nothing", chatListFilter{sender: "ann", recipient: "ann"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := `SELECT id FROM chat_message WHERE 1=1`
			var args []any
			tc.f.appendSQL(&query, &args, "")
			if got := selectIDs(t, query+` ORDER BY ts, id`, args); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}

	t.Run("the column prefix qualifies a joined read", func(t *testing.T) {
		query := `
			SELECT m.id FROM chat_message m
			LEFT JOIN chat_read r ON r.reader_id = m.recipient AND r.peer_id = m.sender
			WHERE 1=1`
		var args []any
		chatListFilter{participant: "ann", sender: "ann"}.appendSQL(&query, &args, "m.")
		want := []string{"m1", "m3"}
		if got := selectIDs(t, query+` ORDER BY m.ts, m.id`, args); !reflect.DeepEqual(got, want) {
			t.Fatalf("want %v, got %v", want, got)
		}
	})
}

func TestListChatBefore(t *testing.T) {
	d := newAPITestDAL(t)
	m1 := dalChat("m1", "ann", "bob", 100)
	m2 := dalChat("m2", "ann", "bob", 200)
	m3 := dalChat("m3", "bob", "ann", 200)
	m4 := dalChat("m4", "carl", "dee", 300)
	m5 := dalChat("m5", "ann", "bob", 400)
	dalPutChats(t, d, m1, m2, m3, m4, m5)

	got, err := d.ListChatBefore("", 300, "m4", 2)
	if err != nil {
		t.Fatalf("ListChatBefore: %v", err)
	}
	dalWantChats(t, "the two messages before the cursor, oldest first", got, []ChatMessage{m2, m3})

	got, err = d.ListChatBefore("", 200, "m3", 10)
	if err != nil {
		t.Fatalf("ListChatBefore at an equal-ts cursor: %v", err)
	}
	dalWantChats(t, "an equal-ts cursor tie-breaks on id", got, []ChatMessage{m1, m2})

	got, err = d.ListChatBefore("ann", 400, "m5", -1)
	if err != nil {
		t.Fatalf("ListChatBefore uncapped: %v", err)
	}
	dalWantChats(t, "a negative limit disables the cap and the participant matches either side",
		got, []ChatMessage{m1, m2, m3})

	got, err = d.ListChatBefore("", 300, "m4", 0)
	if err != nil {
		t.Fatalf("ListChatBefore with limit 0: %v", err)
	}
	dalWantChats(t, "limit 0 reads nothing", got, nil)

	got, err = d.ListChatBefore("", 100, "m1", 10)
	if err != nil {
		t.Fatalf("ListChatBefore at the oldest message: %v", err)
	}
	dalWantChats(t, "nothing is older than the first message", got, []ChatMessage{})

	t.Run("equal timestamps exclude the cursor and a compound filter keeps both sides", func(t *testing.T) {
		got, err := d.listChatBefore(chatListFilter{sender: "ann", recipient: "bob"}, 200, "m2", -1)
		if err != nil {
			t.Fatalf("listChatBefore with a compound filter: %v", err)
		}
		dalWantChats(t, "the equal-ts cursor is exclusive", got, []ChatMessage{m1})
	})

	t.Run("a line with no messages answers an empty page", func(t *testing.T) {
		got, err := d.listChatBefore(chatListFilter{participant: "nobody"}, 999, "missing", 10)
		if err != nil {
			t.Fatalf("listChatBefore with no matching line: %v", err)
		}
		dalWantChats(t, "a missing line", got, []ChatMessage{})
	})
}

func TestNewerThan(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b chatAnchor
		want bool
	}{
		{"a later ts is newer", chatAnchor{TS: 2, ID: "a"}, chatAnchor{TS: 1, ID: "z"}, true},
		{"an earlier ts is older", chatAnchor{TS: 1, ID: "z"}, chatAnchor{TS: 2, ID: "a"}, false},
		{"an equal ts tie-breaks on the larger id", chatAnchor{TS: 1, ID: "b"}, chatAnchor{TS: 1, ID: "a"}, true},
		{"an equal ts tie-breaks against the smaller id", chatAnchor{TS: 1, ID: "a"}, chatAnchor{TS: 1, ID: "b"}, false},
		{"the same anchor is not strictly after itself", chatAnchor{TS: 1, ID: "a"}, chatAnchor{TS: 1, ID: "a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.newerThan(tc.b); got != tc.want {
				t.Fatalf("%+v.newerThan(%+v): want %v, got %v", tc.a, tc.b, tc.want, got)
			}
		})
	}
}

func TestListChatWindow(t *testing.T) {
	d := newAPITestDAL(t)
	m1 := dalChat("m1", "ann", "bob", 100)
	m2 := dalChat("m2", "carl", "dee", 200)
	m3 := dalChat("m3", "ann", "bob", 300)
	m4 := dalChat("m4", "bob", "ann", 400)
	m5 := dalChat("m5", "ann", "bob", 500)
	dalPutChats(t, d, m1, m2, m3, m4, m5)
	at := func(m ChatMessage) *chatAnchor { return &chatAnchor{TS: m.TS, ID: m.ID} }

	got, err := d.listChatWindow(chatListFilter{}, at(m2), at(m4), 200)
	if err != nil {
		t.Fatalf("listChatWindow: %v", err)
	}
	dalWantChats(t, "both anchors are inclusive", got, []ChatMessage{m2, m3, m4})

	got, err = d.listChatWindow(chatListFilter{}, at(m2), nil, 3)
	if err != nil {
		t.Fatalf("listChatWindow(start only): %v", err)
	}
	dalWantChats(t, "start only keeps the anchor and cuts the newest end", got, []ChatMessage{m2, m3, m4})

	got, err = d.listChatWindow(chatListFilter{}, nil, at(m4), 3)
	if err != nil {
		t.Fatalf("listChatWindow(end only): %v", err)
	}
	dalWantChats(t, "end only keeps the anchor and cuts the oldest end", got, []ChatMessage{m2, m3, m4})

	got, err = d.listChatWindow(chatListFilter{}, at(m1), at(m5), 2)
	if err != nil {
		t.Fatalf("listChatWindow(both, truncated): %v", err)
	}
	dalWantChats(t, "an over-wide window keeps end_id and loses the start_id side", got, []ChatMessage{m4, m5})

	got, err = d.listChatWindow(chatListFilter{participant: "ann"}, at(m1), at(m5), 200)
	if err != nil {
		t.Fatalf("listChatWindow(filtered): %v", err)
	}
	dalWantChats(t, "the participant filter narrows the window", got, []ChatMessage{m1, m3, m4, m5})

	got, err = d.listChatWindow(chatListFilter{}, at(m1), at(m5), 0)
	if err != nil {
		t.Fatalf("listChatWindow(limit 0): %v", err)
	}
	dalWantChats(t, "a non-positive limit reads nothing", got, nil)

	got, err = d.listChatWindow(chatListFilter{}, at(m4), at(m2), 200)
	if err != nil {
		t.Fatalf("listChatWindow(start past end): %v", err)
	}
	dalWantChats(t, "a start past its end selects nothing", got, []ChatMessage{})
}

func TestListChatLatest(t *testing.T) {
	d := newAPITestDAL(t)
	m1 := dalChat("m1", "ann", "bob", 100)
	m2 := dalChat("m2", "carl", "dee", 200)
	m3 := dalChat("m3", "bob", "ann", 300)
	m4 := dalChat("m4", "ann", "bob", 300)
	dalPutChats(t, d, m1, m2, m3, m4)

	got, err := d.ListChatLatest("", 2)
	if err != nil {
		t.Fatalf("ListChatLatest: %v", err)
	}
	dalWantChats(t, "the newest two, oldest first", got, []ChatMessage{m3, m4})

	got, err = d.ListChatLatest("ann", 10)
	if err != nil {
		t.Fatalf("ListChatLatest(ann): %v", err)
	}
	dalWantChats(t, "the participant matches either side", got, []ChatMessage{m1, m3, m4})

	got, err = d.ListChatLatest("", -1)
	if err != nil {
		t.Fatalf("ListChatLatest uncapped: %v", err)
	}
	dalWantChats(t, "a negative limit disables the cap", got, []ChatMessage{m1, m2, m3, m4})

	got, err = d.ListChatLatest("", 0)
	if err != nil {
		t.Fatalf("ListChatLatest(0): %v", err)
	}
	dalWantChats(t, "limit 0 reads nothing", got, nil)

	got, err = d.ListChatLatest("nobody", 10)
	if err != nil {
		t.Fatalf("ListChatLatest(nobody): %v", err)
	}
	dalWantChats(t, "a participant with no line reads nothing", got, []ChatMessage{})

	t.Run("a one-sided filter is applied before the newest row is selected", func(t *testing.T) {
		got, err := d.listChatLatest(chatListFilter{recipient: "bob"}, 1)
		if err != nil {
			t.Fatalf("listChatLatest with a recipient filter: %v", err)
		}
		dalWantChats(t, "the newest message received by bob", got, []ChatMessage{m4})
	})

	t.Run("a line with no messages answers an empty page", func(t *testing.T) {
		got, err := d.listChatLatest(chatListFilter{sender: "nobody"}, 10)
		if err != nil {
			t.Fatalf("listChatLatest with no matching line: %v", err)
		}
		dalWantChats(t, "a missing line", got, []ChatMessage{})
	})
}

func TestListChatUnread(t *testing.T) {
	d := newAPITestDAL(t)
	fromA1 := dalChat("a1", "ann", "owner", 100)
	fromA2 := dalChat("a2", "ann", "owner", 300)
	fromB1 := dalChat("b1", "bob", "owner", 50)
	fromB2 := dalChat("b2", "bob", "owner", 400)
	toSelf := dalChat("s1", "owner", "owner", 500)
	elsewhere := dalChat("x1", "ann", "carl", 600)
	dalPutChats(t, d, fromA1, fromA2, fromB1, fromB2, toSelf, elsewhere)
	if _, _, err := d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "ann", LastReadTS: 200}); err != nil {
		t.Fatalf("PutChatRead: %v", err)
	}

	got, err := d.listChatUnread("owner", chatListFilter{}, nil, -1)
	if err != nil {
		t.Fatalf("listChatUnread: %v", err)
	}
	dalWantChats(t, "each message is judged against its own sender's watermark, and a sender with no receipt has none",
		got, []ChatMessage{fromB1, fromA2, fromB2, toSelf})

	got, err = d.listChatUnread("owner", chatListFilter{}, &chatAnchor{TS: 300, ID: "a2"}, -1)
	if err != nil {
		t.Fatalf("listChatUnread(after): %v", err)
	}
	dalWantChats(t, "the after cursor is exclusive", got, []ChatMessage{fromB2, toSelf})

	got, err = d.listChatUnread("owner", chatListFilter{sender: "bob"}, nil, -1)
	if err != nil {
		t.Fatalf("listChatUnread(filtered): %v", err)
	}
	dalWantChats(t, "the filter narrows the unread page", got, []ChatMessage{fromB1, fromB2})

	got, err = d.listChatUnread("owner", chatListFilter{}, nil, 2)
	if err != nil {
		t.Fatalf("listChatUnread(limit 2): %v", err)
	}
	dalWantChats(t, "a positive limit caps the page at the oldest end", got, []ChatMessage{fromB1, fromA2})

	for _, tc := range []struct {
		name   string
		reader string
		limit  int
	}{
		{"a blank reader reads nothing", "", -1},
		{"limit 0 reads nothing", "owner", 0},
	} {
		got, err := d.listChatUnread(tc.reader, chatListFilter{}, nil, tc.limit)
		if err != nil {
			t.Fatalf("listChatUnread(%s): %v", tc.name, err)
		}
		dalWantChats(t, tc.name, got, nil)
	}

	after, err := d.ListChatReads("owner", "")
	if err != nil {
		t.Fatalf("ListChatReads: %v", err)
	}
	want := []ChatRead{{ReaderID: "owner", PeerID: "ann", LastReadTS: 200}}
	if !reflect.DeepEqual(after, want) {
		t.Fatalf("reading unread must write nothing: want %+v, got %+v", want, after)
	}
}

func TestListChatByIDs(t *testing.T) {
	d := newAPITestDAL(t)
	m1 := dalChat("m1", "ann", "bob", 100)
	m2 := dalChat("m2", "carl", "dee", 200)
	m3 := dalChat("m3", "bob", "ann", 300)
	dalPutChats(t, d, m1, m2, m3)

	got, err := d.ListChatByIDs([]string{"m3", "m1"})
	if err != nil {
		t.Fatalf("ListChatByIDs: %v", err)
	}
	dalWantChats(t, "the named messages come back in stream order, not argument order", got, []ChatMessage{m1, m3})

	got, err = d.ListChatByIDs([]string{"m2", "m2", "ghost"})
	if err != nil {
		t.Fatalf("ListChatByIDs(duplicated + unknown): %v", err)
	}
	dalWantChats(t, "a duplicated id cannot inflate the answer and an unknown id is simply absent",
		got, []ChatMessage{m2})

	got, err = d.ListChatByIDs(nil)
	if err != nil {
		t.Fatalf("ListChatByIDs(nil): %v", err)
	}
	dalWantChats(t, "a blank id list reads nothing", got, nil)
}

func TestListChatInvolving(t *testing.T) {
	d := newAPITestDAL(t)
	m1 := dalChat("m1", "ann", "bob", 100)
	m2 := dalChat("m2", "carl", "dee", 200)
	m3 := dalChat("m3", "bob", "ann", 300)
	m4 := dalChat("m4", "ann", "carl", 400)
	dalPutChats(t, d, m1, m2, m3, m4)

	got, err := d.ListChatInvolving("ann", 10)
	if err != nil {
		t.Fatalf("ListChatInvolving: %v", err)
	}
	dalWantChats(t, "sender OR recipient, oldest first", got, []ChatMessage{m1, m3, m4})

	got, err = d.ListChatInvolving("ann", 2)
	if err != nil {
		t.Fatalf("ListChatInvolving(limit 2): %v", err)
	}
	dalWantChats(t, "the newest two, still oldest first", got, []ChatMessage{m3, m4})

	for _, tc := range []struct {
		name        string
		participant string
		limit       int
		want        []ChatMessage
	}{
		{"a blank participant reads nothing", "", 10, nil},
		{"a non-positive limit reads nothing", "ann", 0, nil},
		{"a negative limit reads nothing", "ann", -1, nil},
		{"someone with no line reads nothing", "nobody", 10, []ChatMessage{}},
	} {
		got, err := d.ListChatInvolving(tc.participant, tc.limit)
		if err != nil {
			t.Fatalf("ListChatInvolving(%s): %v", tc.name, err)
		}
		dalWantChats(t, tc.name, got, tc.want)
	}
}

func TestDocumentHistoryKeepFor(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want int
	}{
		{docKindSystemInteraction, 10},
		{docKindBootSequence, 10},
		{docKindOffboard, 10},
		{"role_definition", 3},
		{"", 3},
	} {
		if got := documentHistoryKeepFor(tc.kind); got != tc.want {
			t.Fatalf("documentHistoryKeepFor(%q): want %d, got %d", tc.kind, tc.want, got)
		}
	}
}

func TestSaveWithDocumentHistory(t *testing.T) {
	d := newAPITestDAL(t)
	since := nowSecs()

	for _, snapshot := range []string{"", "{}"} {
		if err := d.SaveWithDocumentHistory("role_definition", "engineer", "owner",
			dalStaticSnapshot(snapshot), func(ex sqlExecer) error {
				_, err := ex.Exec(`INSERT INTO setting (key, value, updated_at) VALUES (?, ?, ?)
					ON CONFLICT (key) DO UPDATE SET value = excluded.value`, "probe", snapshot, 1)
				return err
			}); err != nil {
			t.Fatalf("SaveWithDocumentHistory(%q): %v", snapshot, err)
		}
	}
	empty, err := d.ListDocumentHistory("role_definition", "engineer")
	if err != nil {
		t.Fatalf("ListDocumentHistory: %v", err)
	}
	dalWantHistory(t, "an empty document is retained as nothing", empty, nil, since)

	for _, body := range []string{`{"v":1}`, `{"v":2}`, `{"v":3}`, `{"v":4}`} {
		if err := d.SaveWithDocumentHistory("role_definition", "engineer", "owner",
			dalStaticSnapshot(body), func(sqlExecer) error { return nil }); err != nil {
			t.Fatalf("SaveWithDocumentHistory(%s): %v", body, err)
		}
	}
	got, err := d.ListDocumentHistory("role_definition", "engineer")
	if err != nil {
		t.Fatalf("ListDocumentHistory: %v", err)
	}
	dalWantHistory(t, "only the newest three snapshots survive", got, []DocumentHistory{
		{DocumentKind: "role_definition", DocumentKey: "engineer", ContentJSON: `{"v":4}`, ActorID: "owner"},
		{DocumentKind: "role_definition", DocumentKey: "engineer", ContentJSON: `{"v":3}`, ActorID: "owner"},
		{DocumentKind: "role_definition", DocumentKey: "engineer", ContentJSON: `{"v":2}`, ActorID: "owner"},
	}, since)

	failed := errors.New("the write refused")
	if err := d.SaveWithDocumentHistory("role_definition", "engineer", "kip",
		dalStaticSnapshot(`{"v":5}`), func(sqlExecer) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("SaveWithDocumentHistory(failing write): want %v, got %v", failed, err)
	}
	after, err := d.ListDocumentHistory("role_definition", "engineer")
	if err != nil {
		t.Fatalf("ListDocumentHistory: %v", err)
	}
	if !reflect.DeepEqual(after, got) {
		t.Fatalf("a failed write must retain nothing:\n got %+v\nwant %+v", after, got)
	}

	t.Run("a real snapshot query retains the row it replaces before the write lands", func(t *testing.T) {
		d := newAPITestDAL(t)
		if err := d.PutSetting("snapshot", `{"v":1}`); err != nil {
			t.Fatalf("PutSetting(snapshot): %v", err)
		}
		since := nowSecs()
		if err := d.SaveWithDocumentHistory("role_definition", "engineer", "owner",
			func(q sqlQuerier) (string, error) {
				var value string
				if err := q.QueryRow(`SELECT value FROM setting WHERE key = ?`, "snapshot").Scan(&value); err != nil {
					return "", err
				}
				return value, nil
			}, func(ex sqlExecer) error {
				_, err := ex.Exec(`UPDATE setting SET value = ? WHERE key = ?`, `{"v":2}`, "snapshot")
				return err
			}); err != nil {
			t.Fatalf("SaveWithDocumentHistory with a real snapshot: %v", err)
		}

		history, err := d.ListDocumentHistory("role_definition", "engineer")
		if err != nil {
			t.Fatalf("ListDocumentHistory: %v", err)
		}
		dalWantHistory(t, "the value read through sqlQuerier", history, []DocumentHistory{
			{DocumentKind: "role_definition", DocumentKey: "engineer", ContentJSON: `{"v":1}`, ActorID: "owner"},
		}, since)

		current, err := d.GetSetting("snapshot")
		if err != nil {
			t.Fatalf("GetSetting(snapshot): %v", err)
		}
		if current == nil || *current != `{"v":2}` {
			t.Fatalf("the replacement write: want %q, got %v", `{"v":2}`, current)
		}
	})
}

func TestSaveWithDocumentHistories(t *testing.T) {
	d := newAPITestDAL(t)
	since := nowSecs()
	landed := 0

	if err := d.SaveWithDocumentHistories(nil, func(sqlExecer) error {
		landed++
		return nil
	}); err != nil {
		t.Fatalf("SaveWithDocumentHistories(no stream): %v", err)
	}

	streams := func(sop, learnings string) []documentHistoryStream {
		return []documentHistoryStream{
			{Kind: "task_manual_sop", Key: "review", ActorID: "owner", Snapshot: dalStaticSnapshot(sop)},
			{Kind: "task_manual_learnings", Key: "review", ActorID: "owner", Snapshot: dalStaticSnapshot(learnings)},
		}
	}
	if err := d.SaveWithDocumentHistories(streams(`{"sop":1}`, ""), func(sqlExecer) error {
		landed++
		return nil
	}); err != nil {
		t.Fatalf("SaveWithDocumentHistories: %v", err)
	}
	if err := d.SaveWithDocumentHistories(streams(`{"sop":2}`, `{"learnings":1}`), func(sqlExecer) error {
		landed++
		return nil
	}); err != nil {
		t.Fatalf("SaveWithDocumentHistories: %v", err)
	}
	if landed != 3 {
		t.Fatalf("every write must land once, got %d", landed)
	}

	sop, err := d.ListDocumentHistory("task_manual_sop", "review")
	if err != nil {
		t.Fatalf("ListDocumentHistory(sop): %v", err)
	}
	dalWantHistory(t, "the SOP stream", sop, []DocumentHistory{
		{DocumentKind: "task_manual_sop", DocumentKey: "review", ContentJSON: `{"sop":2}`, ActorID: "owner"},
		{DocumentKind: "task_manual_sop", DocumentKey: "review", ContentJSON: `{"sop":1}`, ActorID: "owner"},
	}, since)

	learnings, err := d.ListDocumentHistory("task_manual_learnings", "review")
	if err != nil {
		t.Fatalf("ListDocumentHistory(learnings): %v", err)
	}
	dalWantHistory(t, "the learnings stream is retained independently", learnings, []DocumentHistory{
		{DocumentKind: "task_manual_learnings", DocumentKey: "review", ContentJSON: `{"learnings":1}`, ActorID: "owner"},
	}, since)

	failed := errors.New("the write refused")
	if err := d.SaveWithDocumentHistories(streams(`{"sop":3}`, `{"learnings":2}`),
		func(sqlExecer) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("SaveWithDocumentHistories(failing write): want %v, got %v", failed, err)
	}
	for _, stream := range []struct {
		kind string
		want int
	}{{"task_manual_sop", 2}, {"task_manual_learnings", 1}} {
		got, err := d.ListDocumentHistory(stream.kind, "review")
		if err != nil {
			t.Fatalf("ListDocumentHistory(%s): %v", stream.kind, err)
		}
		if len(got) != stream.want {
			t.Fatalf("a failed write must retain nothing in %s: want %d entries, got %+v",
				stream.kind, stream.want, got)
		}
	}
}

func TestRetainDocumentVersion(t *testing.T) {
	d := newAPITestDAL(t)
	since := nowSecs()

	for i := 1; i <= 12; i++ {
		body := fmt.Sprintf(`{"v":%d}`, i)
		if err := d.SaveWithDocumentHistory(docKindSystemInteraction, "global", "owner",
			dalStaticSnapshot(body), func(sqlExecer) error { return nil }); err != nil {
			t.Fatalf("SaveWithDocumentHistory(%s): %v", body, err)
		}
	}
	deep, err := d.ListDocumentHistory(docKindSystemInteraction, "global")
	if err != nil {
		t.Fatalf("ListDocumentHistory: %v", err)
	}
	want := []DocumentHistory{}
	for i := 12; i >= 3; i-- {
		want = append(want, DocumentHistory{
			DocumentKind: docKindSystemInteraction, DocumentKey: "global",
			ContentJSON: fmt.Sprintf(`{"v":%d}`, i), ActorID: "owner",
		})
	}
	dalWantHistory(t, "an argued-up kind keeps ten", deep, want, since)

	failing := errors.New("the snapshot refused")
	if err := d.SaveWithDocumentHistory(docKindSystemInteraction, "global", "owner",
		func(sqlQuerier) (string, error) { return "", failing },
		func(sqlExecer) error { return nil }); !errors.Is(err, failing) {
		t.Fatalf("a refusing snapshot must abort the save: want %v, got %v", failing, err)
	}
	after, err := d.ListDocumentHistory(docKindSystemInteraction, "global")
	if err != nil {
		t.Fatalf("ListDocumentHistory: %v", err)
	}
	if !reflect.DeepEqual(after, deep) {
		t.Fatalf("a refusing snapshot must retain nothing:\n got %+v\nwant %+v", after, deep)
	}

	other, err := d.ListDocumentHistory(docKindSystemInteraction, "another-key")
	if err != nil {
		t.Fatalf("ListDocumentHistory(another-key): %v", err)
	}
	dalWantHistory(t, "the trim never reaches another key", other, nil, since)
}

func TestListDocumentHistory(t *testing.T) {
	d := newAPITestDAL(t)
	since := nowSecs()

	empty, err := d.ListDocumentHistory("role_definition", "engineer")
	if err != nil {
		t.Fatalf("ListDocumentHistory before any save: %v", err)
	}
	dalWantHistory(t, "a document nobody has replaced has no history", empty, nil, since)

	for _, save := range []struct{ kind, key, body, actor string }{
		{"role_definition", "engineer", `{"v":1}`, "owner"},
		{"role_definition", "engineer", `{"v":2}`, "kip"},
		{"role_definition", "designer", `{"v":9}`, "owner"},
		{"task_manual_sop", "engineer", `{"v":7}`, "owner"},
	} {
		if err := d.SaveWithDocumentHistory(save.kind, save.key, save.actor,
			dalStaticSnapshot(save.body), func(sqlExecer) error { return nil }); err != nil {
			t.Fatalf("SaveWithDocumentHistory: %v", err)
		}
	}

	got, err := d.ListDocumentHistory("role_definition", "engineer")
	if err != nil {
		t.Fatalf("ListDocumentHistory: %v", err)
	}
	dalWantHistory(t, "one document's own snapshots, newest first", got, []DocumentHistory{
		{DocumentKind: "role_definition", DocumentKey: "engineer", ContentJSON: `{"v":2}`, ActorID: "kip"},
		{DocumentKind: "role_definition", DocumentKey: "engineer", ContentJSON: `{"v":1}`, ActorID: "owner"},
	}, since)
}

func TestGetDocumentHistory(t *testing.T) {
	d := newAPITestDAL(t)
	since := nowSecs()
	for _, save := range []struct{ kind, key, body string }{
		{"role_definition", "engineer", `{"v":1}`},
		{"role_definition", "designer", `{"v":9}`},
	} {
		if err := d.SaveWithDocumentHistory(save.kind, save.key, "owner",
			dalStaticSnapshot(save.body), func(sqlExecer) error { return nil }); err != nil {
			t.Fatalf("SaveWithDocumentHistory: %v", err)
		}
	}
	listed, err := d.ListDocumentHistory("role_definition", "engineer")
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListDocumentHistory: %+v, %v", listed, err)
	}
	id := listed[0].ID

	got, err := d.GetDocumentHistory("role_definition", "engineer", id)
	if err != nil {
		t.Fatalf("GetDocumentHistory: %v", err)
	}
	if got == nil {
		t.Fatalf("GetDocumentHistory(%d): no row", id)
	}
	dalWantHistory(t, "the addressed snapshot", []DocumentHistory{*got}, []DocumentHistory{
		{DocumentKind: "role_definition", DocumentKey: "engineer", ContentJSON: `{"v":1}`, ActorID: "owner"},
	}, since)

	for _, tc := range []struct {
		name      string
		kind, key string
		id        int64
	}{
		{"an id from another key is not addressable here", "role_definition", "designer", id},
		{"an id from another kind is not addressable here", "task_manual_sop", "engineer", id},
		{"an id nobody minted", "role_definition", "engineer", id + 1000},
	} {
		got, err := d.GetDocumentHistory(tc.kind, tc.key, tc.id)
		if err != nil {
			t.Fatalf("GetDocumentHistory(%s): %v", tc.name, err)
		}
		if got != nil {
			t.Fatalf("GetDocumentHistory(%s): want nil, got %+v", tc.name, *got)
		}
	}
}

func TestPutChatOn(t *testing.T) {
	d := newAPITestDAL(t)
	first := dalChatWithAtts("m1", "ann", "bob", 100, dalAttRef("att-1", "image/png", "a.png"))
	if err := putChatOn(d.wdb, first); err != nil {
		t.Fatalf("putChatOn: %v", err)
	}
	dalWantChats(t, "the message is readable straight after the write",
		dalMustListChat(t, d), []ChatMessage{first})

	rewritten := ChatMessage{ID: "m1", Sender: "bob", Recipient: "carl", Body: "rewritten", TS: 200}
	if err := putChatOn(d.wdb, rewritten); err != nil {
		t.Fatalf("putChatOn(rewrite): %v", err)
	}
	rewritten.Meta = map[string]any{}
	dalWantChats(t, "the same id replaces the row wholesale, meta included",
		dalMustListChat(t, d), []ChatMessage{rewritten})

	second := dalChat("m2", "carl", "ann", 300)
	if err := d.inTx(func(tx *sql.Tx) error { return putChatOn(tx, second) }); err != nil {
		t.Fatalf("putChatOn(tx): %v", err)
	}
	dalWantChats(t, "the transactional form writes the same row",
		dalMustListChat(t, d), []ChatMessage{rewritten, second})

	failed := errors.New("the rest of the transaction refused")
	if err := d.inTx(func(tx *sql.Tx) error {
		if err := putChatOn(tx, dalChat("m3", "ann", "bob", 400)); err != nil {
			return err
		}
		return failed
	}); !errors.Is(err, failed) {
		t.Fatalf("inTx: want %v, got %v", failed, err)
	}
	dalWantChats(t, "a rolled-back transaction leaves no message",
		dalMustListChat(t, d), []ChatMessage{rewritten, second})
}

// dalMustListChat reads the whole stream or fails the test.

func TestRefIDsFromJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		blob string
		want []string
	}{
		{"the ids of a conforming array", `[{"id":"att-1","mime":"image/png"},{"id":"att-2"}]`, []string{"att-1", "att-2"}},
		{"a blank id contributes nothing", `[{"id":""},{"id":"att-3"}]`, []string{"att-3"}},
		{"an empty array contributes nothing", `[]`, []string{}},
		{"an object where an array belongs contributes nothing", `{"id":"att-4"}`, []string{}},
		{"malformed JSON contributes nothing", `not json`, []string{}},
		{"an empty string contributes nothing", ``, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			into := map[string]bool{}
			refIDsFromJSON(tc.blob, into)
			if got := dalSortedKeys(into); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}

	into := map[string]bool{"already-there": true}
	refIDsFromJSON(`[{"id":"att-5"},{"id":"already-there"}]`, into)
	want := []string{"already-there", "att-5"}
	if got := dalSortedKeys(into); !reflect.DeepEqual(got, want) {
		t.Fatalf("the ids fold into what the caller already had: want %v, got %v", want, got)
	}
}

func TestDeleteChatInvolving(t *testing.T) {
	d := newAPITestDAL(t)
	dalPutBlobs(t, d, "att-orphan", "att-shared", "att-card", "att-artifact", "att-untouched")
	doomedSent := dalChatWithAtts("m1", "ann", "bob", 100,
		dalAttRef("att-orphan", "image/png", "a.png"),
		dalAttRef("att-shared", "image/png", "b.png"),
		dalAttRef("att-card", "image/png", "c.png"),
		dalAttRef("att-artifact", "image/png", "d.png"))
	doomedReceived := dalChat("m2", "carl", "ann", 200)
	survivor := dalChatWithAtts("m3", "carl", "dee", 300, dalAttRef("att-shared", "image/png", "b.png"))
	dalPutChats(t, d, doomedSent, doomedReceived, survivor)
	if err := d.PutReplyCard(ReplyCard{
		ID: "rc-1", FromMember: "dee", Kind: "decision", SelectMode: "single", Status: "answered",
		AnswerAttachments: []any{dalAttRef("att-card", "image/png", "c.png")},
	}); err != nil {
		t.Fatalf("PutReplyCard: %v", err)
	}
	if err := d.PutTaskArtifact(TaskArtifact{
		ID: "ta-1", TaskID: "T-1", Kind: "file", AttachmentID: "att-artifact",
	}); err != nil {
		t.Fatalf("PutTaskArtifact: %v", err)
	}

	msgs, atts, err := d.DeleteChatInvolving("ann")
	if err != nil {
		t.Fatalf("DeleteChatInvolving: %v", err)
	}
	if msgs != 2 || atts != 1 {
		t.Fatalf("DeleteChatInvolving: want (2 messages, 1 blob), got (%d, %d)", msgs, atts)
	}
	dalWantChats(t, "only the messages involving the member are gone",
		dalMustListChat(t, d), []ChatMessage{survivor})
	want := []string{"att-artifact", "att-card", "att-shared", "att-untouched"}
	if got := dalStoredBlobIDs(t, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("only the blob no surviving record references is collected: want %v, got %v", want, got)
	}

	msgs, atts, err = d.DeleteChatInvolving("ann")
	if err != nil {
		t.Fatalf("DeleteChatInvolving again: %v", err)
	}
	if msgs != 0 || atts != 0 {
		t.Fatalf("DeleteChatInvolving on a member with no messages: want (0, 0), got (%d, %d)", msgs, atts)
	}
}

func TestCollectOrphanBlobs(t *testing.T) {
	d := newAPITestDAL(t)
	dalPutBlobs(t, d, "att-1", "att-2", "att-3")
	dalPutChats(t, d, dalChatWithAtts("m1", "ann", "bob", 100, dalAttRef("att-2", "image/png", "b.png")))

	var deleted int
	if err := d.inTx(func(tx *sql.Tx) error {
		n, err := collectOrphanBlobs(tx, map[string]bool{})
		if err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("an empty candidate set collects nothing, got %d", n)
		}
		deleted, err = collectOrphanBlobs(tx, map[string]bool{
			"att-1": true, "att-2": true, "att-never-stored": true,
		})
		return err
	}); err != nil {
		t.Fatalf("collectOrphanBlobs: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("only the unreferenced, stored candidate is deleted: want 1, got %d", deleted)
	}
	want := []string{"att-2", "att-3"}
	if got := dalStoredBlobIDs(t, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestCollectSurvivingBlobRefs(t *testing.T) {
	d := newAPITestDAL(t)
	dalPutChats(t, d,
		dalChatWithAtts("m1", "ann", "bob", 100,
			dalAttRef("att-chat", "image/png", "a.png"), dalAttRef("", "image/png", "")),
		dalChat("m2", "bob", "ann", 200))
	if _, err := d.wdb.Exec(
		`INSERT INTO chat_message (id, sender, recipient, body, ts, meta) VALUES (?, ?, ?, ?, ?, ?)`,
		"m3", "ann", "bob", "free-form meta", 300, `{"attachments":"not an array"}`,
	); err != nil {
		t.Fatalf("seed a non-conforming meta row: %v", err)
	}
	if err := d.PutReplyCard(ReplyCard{
		ID: "rc-1", FromMember: "ann", Kind: "decision", SelectMode: "single", Status: "answered",
		AnswerAttachments: []any{dalAttRef("att-answer", "image/png", "b.png")},
		Attachments:       []any{dalAttRef("att-question", "image/png", "c.png")},
	}); err != nil {
		t.Fatalf("PutReplyCard: %v", err)
	}
	live := TaskArtifact{ID: "ta-1", TaskID: "T-1", Kind: "file", AttachmentID: "att-retired"}
	if err := d.PutTaskArtifact(live); err != nil {
		t.Fatalf("PutTaskArtifact: %v", err)
	}
	live.AttachmentID = "att-artifact"
	if replaced, err := d.ReplaceTaskArtifact(live); err != nil || !replaced {
		t.Fatalf("ReplaceTaskArtifact: %v, %v", replaced, err)
	}
	dalPutMember(t, d, dalTestMember("dee", "Dee"))
	dalPutMember(t, d, dalTestMember("eve", "Eve"))

	var got []string
	if err := d.inTx(func(tx *sql.Tx) error {
		into := map[string]bool{"already-there": true}
		if err := collectSurvivingBlobRefs(tx, into); err != nil {
			return err
		}
		got = dalSortedKeys(into)
		return nil
	}); err != nil {
		t.Fatalf("collectSurvivingBlobRefs: %v", err)
	}
	want := []string{
		"already-there", "att-answer", "att-artifact",
		"att-chat", "att-question", "att-retired",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the liveness verdict:\n got %v\nwant %v", got, want)
	}
}

func TestCollectChatMetaRefs(t *testing.T) {
	d := newAPITestDAL(t)
	dalPutChats(t, d,
		dalChatWithAtts("m1", "ann", "bob", 100,
			dalAttRef("att-1", "image/png", "a.png"), dalAttRef("", "image/png", "")),
		dalChatWithAtts("m2", "carl", "dee", 200, dalAttRef("att-2", "image/png", "b.png")),
		dalChat("m3", "ann", "bob", 300))
	if _, err := d.wdb.Exec(
		`INSERT INTO chat_message (id, sender, recipient, body, ts, meta) VALUES (?, ?, ?, ?, ?, ?)`,
		"m4", "ann", "bob", "free-form meta", 400, `{"attachments":{"id":"att-3"}}`,
	); err != nil {
		t.Fatalf("seed a non-conforming meta row: %v", err)
	}

	for _, tc := range []struct {
		name  string
		query string
		args  []any
		want  []string
	}{
		{"every message", `SELECT meta FROM chat_message`, nil, []string{"att-1", "att-2"}},
		{"one side of the stream", `SELECT meta FROM chat_message WHERE sender = ? OR recipient = ?`,
			[]any{"ann", "ann"}, []string{"att-1"}},
		{"a query that returns nothing", `SELECT meta FROM chat_message WHERE sender = ?`,
			[]any{"nobody"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			if err := d.inTx(func(tx *sql.Tx) error {
				into := map[string]bool{}
				if err := collectChatMetaRefs(tx, tc.query, into, tc.args...); err != nil {
					return err
				}
				got = dalSortedKeys(into)
				return nil
			}); err != nil {
				t.Fatalf("collectChatMetaRefs: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestChatAttachmentRefBefore(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b ChatAttachmentRef
		want bool
	}{
		{"a newer message sorts first",
			ChatAttachmentRef{TS: 200, MessageID: "z", Ord: 9},
			ChatAttachmentRef{TS: 100, MessageID: "a", Ord: 0}, true},
		{"an older message does not",
			ChatAttachmentRef{TS: 100, MessageID: "a", Ord: 0},
			ChatAttachmentRef{TS: 200, MessageID: "z", Ord: 9}, false},
		{"an equal ts tie-breaks on the SMALLER message id",
			ChatAttachmentRef{TS: 100, MessageID: "a", Ord: 9},
			ChatAttachmentRef{TS: 100, MessageID: "b", Ord: 0}, true},
		{"an equal ts does not favour the larger message id",
			ChatAttachmentRef{TS: 100, MessageID: "b", Ord: 0},
			ChatAttachmentRef{TS: 100, MessageID: "a", Ord: 9}, false},
		{"inside one message the earlier position sorts first",
			ChatAttachmentRef{TS: 100, MessageID: "a", Ord: 0},
			ChatAttachmentRef{TS: 100, MessageID: "a", Ord: 1}, true},
		{"the same ref does not sort before itself",
			ChatAttachmentRef{TS: 100, MessageID: "a", Ord: 1},
			ChatAttachmentRef{TS: 100, MessageID: "a", Ord: 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatAttachmentRefBefore(tc.a, tc.b); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestListChatAttachmentRefsOneSided(t *testing.T) {
	d := newAPITestDAL(t)
	dalPutChats(t, d,
		dalChatWithAtts("m1", "ann", "bob", 100,
			dalAttRef("att-1", "image/png", "a.png"), dalAttRef("att-2", "image/jpeg", "b.jpg")),
		dalChatWithAtts("m2", "bob", "ann", 200, dalAttRef("att-3", "image/png", "c.png")),
		dalChatWithAtts("m3", "ann", "ann", 300, dalAttRef("att-4", "image/png", "d.png")),
		dalChat("m4", "ann", "bob", 400))

	got, err := d.listChatAttachmentRefsOneSided("sender", "ann", "")
	if err != nil {
		t.Fatalf("listChatAttachmentRefsOneSided(sender): %v", err)
	}
	want := []ChatAttachmentRef{
		{MessageID: "m3", Ord: 0, AttachmentID: "att-4", Sender: "ann", Recipient: "ann", TS: 300, Mime: "image/png", Filename: "d.png"},
		{MessageID: "m1", Ord: 0, AttachmentID: "att-1", Sender: "ann", Recipient: "bob", TS: 100, Mime: "image/png", Filename: "a.png"},
		{MessageID: "m1", Ord: 1, AttachmentID: "att-2", Sender: "ann", Recipient: "bob", TS: 100, Mime: "image/jpeg", Filename: "b.jpg"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the sender side, newest message first then position:\n got %+v\nwant %+v", got, want)
	}

	got, err = d.listChatAttachmentRefsOneSided("recipient", "ann", " AND sender <> recipient")
	if err != nil {
		t.Fatalf("listChatAttachmentRefsOneSided(recipient): %v", err)
	}
	want = []ChatAttachmentRef{
		{MessageID: "m2", Ord: 0, AttachmentID: "att-3", Sender: "bob", Recipient: "ann", TS: 200, Mime: "image/png", Filename: "c.png"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the extra clause narrows the recipient side:\n got %+v\nwant %+v", got, want)
	}

	got, err = d.listChatAttachmentRefsOneSided("sender", "nobody", "")
	if err != nil {
		t.Fatalf("listChatAttachmentRefsOneSided(nobody): %v", err)
	}
	if got != nil {
		t.Fatalf("someone with no attachment reads nothing, got %+v", got)
	}
}

func TestListChatAttachmentRefsFor(t *testing.T) {
	d := newAPITestDAL(t)
	dalPutChats(t, d,
		dalChatWithAtts("m1", "ann", "bob", 100, dalAttRef("att-1", "image/png", "a.png")),
		dalChatWithAtts("m2", "bob", "ann", 200,
			dalAttRef("att-2", "image/png", "b.png"), dalAttRef("att-3", "image/png", "c.png")),
		dalChatWithAtts("m3", "ann", "ann", 200, dalAttRef("att-4", "image/png", "d.png")),
		dalChatWithAtts("m4", "carl", "dee", 300, dalAttRef("att-5", "image/png", "e.png")))

	got, err := d.ListChatAttachmentRefsFor("ann")
	if err != nil {
		t.Fatalf("ListChatAttachmentRefsFor: %v", err)
	}
	want := []ChatAttachmentRef{
		{MessageID: "m2", Ord: 0, AttachmentID: "att-2", Sender: "bob", Recipient: "ann", TS: 200, Mime: "image/png", Filename: "b.png"},
		{MessageID: "m2", Ord: 1, AttachmentID: "att-3", Sender: "bob", Recipient: "ann", TS: 200, Mime: "image/png", Filename: "c.png"},
		{MessageID: "m3", Ord: 0, AttachmentID: "att-4", Sender: "ann", Recipient: "ann", TS: 200, Mime: "image/png", Filename: "d.png"},
		{MessageID: "m1", Ord: 0, AttachmentID: "att-1", Sender: "ann", Recipient: "bob", TS: 100, Mime: "image/png", Filename: "a.png"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("both sides merged, newest first, a self-message counted once:\n got %+v\nwant %+v", got, want)
	}

	got, err = d.ListChatAttachmentRefsFor("nobody")
	if err != nil {
		t.Fatalf("ListChatAttachmentRefsFor(nobody): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("someone with no attachment reads nothing, got %+v", got)
	}
}

func TestPutChatAttachmentOn(t *testing.T) {
	d := newAPITestDAL(t)
	named := "poster.png"
	first := ChatAttachment{ID: "att-1", Mime: "image/png", Data: []byte("first bytes"), Filename: &named}
	if err := putChatAttachmentOn(d.wdb, first); err != nil {
		t.Fatalf("putChatAttachmentOn: %v", err)
	}
	got, err := d.GetChatAttachment("att-1")
	if err != nil {
		t.Fatalf("GetChatAttachment: %v", err)
	}
	if !reflect.DeepEqual(got, &first) {
		t.Fatalf("the blob reads back whole:\n got %+v\nwant %+v", got, first)
	}

	replacement := ChatAttachment{ID: "att-1", Mime: "image/jpeg", Data: []byte("second bytes")}
	if err := putChatAttachmentOn(d.wdb, replacement); err != nil {
		t.Fatalf("putChatAttachmentOn(replacement): %v", err)
	}
	got, err = d.GetChatAttachment("att-1")
	if err != nil {
		t.Fatalf("GetChatAttachment: %v", err)
	}
	if !reflect.DeepEqual(got, &replacement) {
		t.Fatalf("the same id replaces mime, bytes and filename:\n got %+v\nwant %+v", got, replacement)
	}

	failed := errors.New("the rest of the transaction refused")
	if err := d.inTx(func(tx *sql.Tx) error {
		if err := putChatAttachmentOn(tx, ChatAttachment{ID: "att-2", Mime: "image/png", Data: []byte("x")}); err != nil {
			return err
		}
		return failed
	}); !errors.Is(err, failed) {
		t.Fatalf("inTx: want %v, got %v", failed, err)
	}
	want := []string{"att-1"}
	if ids := dalStoredBlobIDs(t, d); !reflect.DeepEqual(ids, want) {
		t.Fatalf("a rolled-back transaction leaves no blob: want %v, got %v", want, ids)
	}
}

func TestGetChatAttachment(t *testing.T) {
	d := newAPITestDAL(t)
	named := "poster.png"
	stored := ChatAttachment{ID: "att-1", Mime: "image/png", Data: []byte("bytes"), Filename: &named}
	if err := d.PutChatAttachment(stored); err != nil {
		t.Fatalf("PutChatAttachment: %v", err)
	}
	pasted := ChatAttachment{ID: "att-2", Mime: "image/jpeg", Data: []byte("more bytes")}
	if err := d.PutChatAttachment(pasted); err != nil {
		t.Fatalf("PutChatAttachment: %v", err)
	}

	got, err := d.GetChatAttachment("att-1")
	if err != nil {
		t.Fatalf("GetChatAttachment(att-1): %v", err)
	}
	if !reflect.DeepEqual(got, &stored) {
		t.Fatalf("GetChatAttachment(att-1):\n got %+v\nwant %+v", got, stored)
	}

	got, err = d.GetChatAttachment("att-2")
	if err != nil {
		t.Fatalf("GetChatAttachment(att-2): %v", err)
	}
	if !reflect.DeepEqual(got, &pasted) {
		t.Fatalf("a blob stored with no filename reads back with none:\n got %+v\nwant %+v", got, pasted)
	}

	got, err = d.GetChatAttachment("att-ghost")
	if err != nil {
		t.Fatalf("GetChatAttachment(att-ghost): %v", err)
	}
	if got != nil {
		t.Fatalf("GetChatAttachment(att-ghost): want nil, got %+v", *got)
	}
}

func TestListChatReads(t *testing.T) {
	d := newAPITestDAL(t)
	empty, err := d.ListChatReads("", "")
	if err != nil {
		t.Fatalf("ListChatReads before any receipt: %v", err)
	}
	if empty != nil {
		t.Fatalf("ListChatReads before any receipt: want none, got %+v", empty)
	}

	for _, r := range []ChatRead{
		{ReaderID: "owner", PeerID: "ann", LastReadTS: 100},
		{ReaderID: "owner", PeerID: "bob", LastReadTS: 200},
		{ReaderID: "ann", PeerID: "owner", LastReadTS: 300},
	} {
		if _, _, err := d.PutChatRead(r); err != nil {
			t.Fatalf("PutChatRead: %v", err)
		}
	}

	for _, tc := range []struct {
		name         string
		reader, peer string
		want         []ChatRead
	}{
		{"no filter at all", "", "", []ChatRead{
			{ReaderID: "owner", PeerID: "ann", LastReadTS: 100},
			{ReaderID: "owner", PeerID: "bob", LastReadTS: 200},
			{ReaderID: "ann", PeerID: "owner", LastReadTS: 300},
		}},
		{"by reader", "owner", "", []ChatRead{
			{ReaderID: "owner", PeerID: "ann", LastReadTS: 100},
			{ReaderID: "owner", PeerID: "bob", LastReadTS: 200},
		}},
		{"by peer", "", "owner", []ChatRead{{ReaderID: "ann", PeerID: "owner", LastReadTS: 300}}},
		{"by both", "owner", "ann", []ChatRead{{ReaderID: "owner", PeerID: "ann", LastReadTS: 100}}},
		{"a pair with no receipt", "bob", "ann", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := d.ListChatReads(tc.reader, tc.peer)
			if err != nil {
				t.Fatalf("ListChatReads: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %+v, got %+v", tc.want, got)
			}
		})
	}
}

func TestUnreadCountsFor(t *testing.T) {
	d := newAPITestDAL(t)
	dalPutChats(t, d,
		dalChat("a1", "ann", "owner", 100),
		dalChat("a2", "ann", "owner", 300),
		dalChat("b1", "bob", "owner", 50),
		dalChat("b2", "bob", "owner", 400),
		dalChat("s1", "owner", "owner", 500),
		dalChat("x1", "ann", "carl", 600))
	if _, _, err := d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "ann", LastReadTS: 200}); err != nil {
		t.Fatalf("PutChatRead: %v", err)
	}

	got, err := d.UnreadCountsFor("owner")
	if err != nil {
		t.Fatalf("UnreadCountsFor: %v", err)
	}
	want := map[string]int{"ann": 1, "bob": 2, "owner": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("each sender is counted against its own watermark: want %v, got %v", want, got)
	}

	if _, _, err := d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "bob", LastReadTS: 400}); err != nil {
		t.Fatalf("PutChatRead: %v", err)
	}
	got, err = d.UnreadCountsFor("owner")
	if err != nil {
		t.Fatalf("UnreadCountsFor: %v", err)
	}
	want = map[string]int{"ann": 1, "owner": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a sender with nothing unread is absent: want %v, got %v", want, got)
	}

	got, err = d.UnreadCountsFor("nobody")
	if err != nil {
		t.Fatalf("UnreadCountsFor(nobody): %v", err)
	}
	if !reflect.DeepEqual(got, map[string]int{}) {
		t.Fatalf("a reader nothing is addressed to counts nothing, got %v", got)
	}
}

func TestPutChatRead(t *testing.T) {
	d := newAPITestDAL(t)

	got, advanced, err := d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "ann", LastReadTS: 100})
	if err != nil {
		t.Fatalf("PutChatRead: %v", err)
	}
	want := ChatRead{ReaderID: "owner", PeerID: "ann", LastReadTS: 100}
	if got != want || !advanced {
		t.Fatalf("a first receipt advances: want (%+v, true), got (%+v, %v)", want, got, advanced)
	}

	got, advanced, err = d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "ann", LastReadTS: 200})
	if err != nil {
		t.Fatalf("PutChatRead(advance): %v", err)
	}
	want = ChatRead{ReaderID: "owner", PeerID: "ann", LastReadTS: 200}
	if got != want || !advanced {
		t.Fatalf("a newer receipt advances: want (%+v, true), got (%+v, %v)", want, got, advanced)
	}

	for _, ts := range []float64{200, 150} {
		got, advanced, err = d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "ann", LastReadTS: ts})
		if err != nil {
			t.Fatalf("PutChatRead(%v): %v", ts, err)
		}
		if got != want || advanced {
			t.Fatalf("a stale or equal receipt never rewinds: want (%+v, false), got (%+v, %v)", want, got, advanced)
		}
	}

	if _, _, err := d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "bob", LastReadTS: 50}); err != nil {
		t.Fatalf("PutChatRead(bob): %v", err)
	}
	all, err := d.ListChatReads("", "")
	if err != nil {
		t.Fatalf("ListChatReads: %v", err)
	}
	wantAll := []ChatRead{
		{ReaderID: "owner", PeerID: "ann", LastReadTS: 200},
		{ReaderID: "owner", PeerID: "bob", LastReadTS: 50},
	}
	if !reflect.DeepEqual(all, wantAll) {
		t.Fatalf("the composite key keeps one row per pair:\n got %+v\nwant %+v", all, wantAll)
	}
}

func TestDeleteChatReadsInvolving(t *testing.T) {
	d := newAPITestDAL(t)
	for _, r := range []ChatRead{
		{ReaderID: "owner", PeerID: "ann", LastReadTS: 100},
		{ReaderID: "ann", PeerID: "owner", LastReadTS: 200},
		{ReaderID: "bob", PeerID: "ann", LastReadTS: 300},
		{ReaderID: "bob", PeerID: "carl", LastReadTS: 400},
	} {
		if _, _, err := d.PutChatRead(r); err != nil {
			t.Fatalf("PutChatRead: %v", err)
		}
	}

	deleted, err := d.DeleteChatReadsInvolving("ann")
	if err != nil {
		t.Fatalf("DeleteChatReadsInvolving: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("every receipt naming the member on either side goes: want 3, got %d", deleted)
	}
	got, err := d.ListChatReads("", "")
	if err != nil {
		t.Fatalf("ListChatReads: %v", err)
	}
	want := []ChatRead{{ReaderID: "bob", PeerID: "carl", LastReadTS: 400}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}

	deleted, err = d.DeleteChatReadsInvolving("ann")
	if err != nil {
		t.Fatalf("DeleteChatReadsInvolving again: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("a member with no receipt deletes nothing: want 0, got %d", deleted)
	}
}

func TestGetUserContextOn(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := getUserContextOn(d.rdb)
	if err != nil {
		t.Fatalf("getUserContextOn before any write: %v", err)
	}
	if got != nil {
		t.Fatalf("a block nobody has written reads back as nil, got %+v", *got)
	}

	written := UserContext{Text: "the owner's own block", Tombstoned: true}
	if err := d.PutUserContext(written); err != nil {
		t.Fatalf("PutUserContext: %v", err)
	}
	got, err = getUserContextOn(d.rdb)
	if err != nil {
		t.Fatalf("getUserContextOn: %v", err)
	}
	if !reflect.DeepEqual(got, &written) {
		t.Fatalf("getUserContextOn:\n got %+v\nwant %+v", got, written)
	}

	if err := d.inTx(func(tx *sql.Tx) error {
		inside, err := getUserContextOn(tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(inside, &written) {
			t.Fatalf("the transactional read sees the same block:\n got %+v\nwant %+v", inside, written)
		}
		return nil
	}); err != nil {
		t.Fatalf("inTx: %v", err)
	}
}

func TestPutUserContextOn(t *testing.T) {
	d := newAPITestDAL(t)
	first := UserContext{Text: "first draft", Tombstoned: false}
	if err := putUserContextOn(d.wdb, first); err != nil {
		t.Fatalf("putUserContextOn: %v", err)
	}
	dalWantUserContext(t, d, &first)

	second := UserContext{Text: "", Tombstoned: true}
	if err := putUserContextOn(d.wdb, second); err != nil {
		t.Fatalf("putUserContextOn(reset marker): %v", err)
	}
	dalWantUserContext(t, d, &second)

	failed := errors.New("the rest of the transaction refused")
	if err := d.inTx(func(tx *sql.Tx) error {
		if err := putUserContextOn(tx, UserContext{Text: "rolled back", Tombstoned: false}); err != nil {
			return err
		}
		return failed
	}); !errors.Is(err, failed) {
		t.Fatalf("inTx: want %v, got %v", failed, err)
	}
	dalWantUserContext(t, d, &second)
}

// dalWantUserContext asserts the single-row block reads back as want.

func TestListRoleDefs(t *testing.T) {
	d := newAPITestDAL(t)
	empty, err := d.ListRoleDefs()
	if err != nil {
		t.Fatalf("ListRoleDefs before any overlay: %v", err)
	}
	if empty != nil {
		t.Fatalf("ListRoleDefs before any overlay: want none, got %+v", empty)
	}

	live := RoleDef{RoleKey: "engineer", Name: "Engineer", DefinitionMD: "# duty", Tombstoned: false}
	reset := RoleDef{RoleKey: "designer", Name: "", DefinitionMD: "", Tombstoned: true}
	for _, rd := range []RoleDef{live, reset} {
		if err := d.PutRoleDef(rd); err != nil {
			t.Fatalf("PutRoleDef(%q): %v", rd.RoleKey, err)
		}
	}

	got, err := d.ListRoleDefs()
	if err != nil {
		t.Fatalf("ListRoleDefs: %v", err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].RoleKey < got[j].RoleKey })
	want := []RoleDef{reset, live}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a tombstoned overlay is still a row:\n got %+v\nwant %+v", got, want)
	}
}

func TestGetRoleDefOn(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := getRoleDefOn(d.rdb, "engineer")
	if err != nil {
		t.Fatalf("getRoleDefOn before any overlay: %v", err)
	}
	if got != nil {
		t.Fatalf("a role nobody has edited reads back as nil, got %+v", *got)
	}

	written := RoleDef{RoleKey: "engineer", Name: "Engineer", DefinitionMD: "# duty", Tombstoned: true}
	if err := d.PutRoleDef(written); err != nil {
		t.Fatalf("PutRoleDef: %v", err)
	}
	if err := d.PutRoleDef(RoleDef{RoleKey: "designer", Name: "Designer"}); err != nil {
		t.Fatalf("PutRoleDef(designer): %v", err)
	}

	got, err = getRoleDefOn(d.rdb, "engineer")
	if err != nil {
		t.Fatalf("getRoleDefOn: %v", err)
	}
	if !reflect.DeepEqual(got, &written) {
		t.Fatalf("getRoleDefOn:\n got %+v\nwant %+v", got, written)
	}

	if err := d.inTx(func(tx *sql.Tx) error {
		inside, err := getRoleDefOn(tx, "engineer")
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(inside, &written) {
			t.Fatalf("the transactional read sees the same overlay:\n got %+v\nwant %+v", inside, written)
		}
		return nil
	}); err != nil {
		t.Fatalf("inTx: %v", err)
	}
}

func TestPutRoleDefOn(t *testing.T) {
	d := newAPITestDAL(t)
	first := RoleDef{RoleKey: "engineer", Name: "Engineer", DefinitionMD: "# first", Tombstoned: false}
	if err := putRoleDefOn(d.wdb, first); err != nil {
		t.Fatalf("putRoleDefOn: %v", err)
	}
	dalWantRoleDef(t, d, "engineer", &first)

	second := RoleDef{RoleKey: "engineer", Name: "Engineer II", DefinitionMD: "", Tombstoned: true}
	if err := putRoleDefOn(d.wdb, second); err != nil {
		t.Fatalf("putRoleDefOn(rewrite): %v", err)
	}
	dalWantRoleDef(t, d, "engineer", &second)

	failed := errors.New("the rest of the transaction refused")
	if err := d.inTx(func(tx *sql.Tx) error {
		if err := putRoleDefOn(tx, RoleDef{RoleKey: "designer", Name: "Designer"}); err != nil {
			return err
		}
		return failed
	}); !errors.Is(err, failed) {
		t.Fatalf("inTx: want %v, got %v", failed, err)
	}
	dalWantRoleDef(t, d, "designer", nil)
}

// dalWantRoleDef asserts one role's overlay reads back as want.

func TestDeleteRoleDef(t *testing.T) {
	d := newAPITestDAL(t)
	since := nowSecs()
	for _, rd := range []RoleDef{
		{RoleKey: "r-abc", Name: "Custom", DefinitionMD: "# custom"},
		{RoleKey: "r-abcdef", Name: "Neighbour", DefinitionMD: "# neighbour"},
	} {
		if err := d.PutRoleDef(rd); err != nil {
			t.Fatalf("PutRoleDef(%q): %v", rd.RoleKey, err)
		}
		if err := d.SaveWithDocumentHistory("role_definition", rd.RoleKey, "owner",
			dalStaticSnapshot(`{"v":1}`), func(sqlExecer) error { return nil }); err != nil {
			t.Fatalf("SaveWithDocumentHistory(%q): %v", rd.RoleKey, err)
		}
	}

	deleted, err := d.DeleteRoleDef("r-abc")
	if err != nil {
		t.Fatalf("DeleteRoleDef: %v", err)
	}
	if !deleted {
		t.Fatalf("DeleteRoleDef: want true, got false")
	}
	dalWantRoleDef(t, d, "r-abc", nil)
	gone, err := d.ListDocumentHistory("role_definition", "r-abc")
	if err != nil {
		t.Fatalf("ListDocumentHistory: %v", err)
	}
	dalWantHistory(t, "the retained versions go with the document", gone, nil, since)

	dalWantRoleDef(t, d, "r-abcdef", &RoleDef{RoleKey: "r-abcdef", Name: "Neighbour", DefinitionMD: "# neighbour"})
	kept, err := d.ListDocumentHistory("role_definition", "r-abcdef")
	if err != nil {
		t.Fatalf("ListDocumentHistory(neighbour): %v", err)
	}
	dalWantHistory(t, "a role whose key merely starts with the deleted one keeps its history", kept,
		[]DocumentHistory{{DocumentKind: "role_definition", DocumentKey: "r-abcdef", ContentJSON: `{"v":1}`, ActorID: "owner"}}, since)

	deleted, err = d.DeleteRoleDef("r-abc")
	if err != nil {
		t.Fatalf("DeleteRoleDef again: %v", err)
	}
	if deleted {
		t.Fatalf("deleting an absent overlay: want false, got true")
	}
}

func TestGetLessonsOn(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := getLessonsOn(d.rdb, "engineer")
	if err != nil {
		t.Fatalf("getLessonsOn before any overlay: %v", err)
	}
	if got != nil {
		t.Fatalf("a role nobody has edited reads back as nil, got %+v", *got)
	}

	written := Lessons{RoleKey: "engineer", Text: "what we learned", Tombstoned: true}
	if err := d.PutLessons(written); err != nil {
		t.Fatalf("PutLessons: %v", err)
	}
	got, err = getLessonsOn(d.rdb, "engineer")
	if err != nil {
		t.Fatalf("getLessonsOn: %v", err)
	}
	if !reflect.DeepEqual(got, &written) {
		t.Fatalf("getLessonsOn:\n got %+v\nwant %+v", got, written)
	}

	if err := d.inTx(func(tx *sql.Tx) error {
		inside, err := getLessonsOn(tx, "engineer")
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(inside, &written) {
			t.Fatalf("the transactional read sees the same overlay:\n got %+v\nwant %+v", inside, written)
		}
		return nil
	}); err != nil {
		t.Fatalf("inTx: %v", err)
	}
}

func TestPutLessonsOn(t *testing.T) {
	d := newAPITestDAL(t)
	first := Lessons{RoleKey: "engineer", Text: "first draft"}
	if err := putLessonsOn(d.wdb, first); err != nil {
		t.Fatalf("putLessonsOn: %v", err)
	}
	dalWantLessons(t, d, "engineer", &first)

	second := Lessons{RoleKey: "engineer", Text: "", Tombstoned: true}
	if err := putLessonsOn(d.wdb, second); err != nil {
		t.Fatalf("putLessonsOn(reset marker): %v", err)
	}
	dalWantLessons(t, d, "engineer", &second)

	failed := errors.New("the rest of the transaction refused")
	if err := d.inTx(func(tx *sql.Tx) error {
		if err := putLessonsOn(tx, Lessons{RoleKey: "designer", Text: "rolled back"}); err != nil {
			return err
		}
		return failed
	}); !errors.Is(err, failed) {
		t.Fatalf("inTx: want %v, got %v", failed, err)
	}
	dalWantLessons(t, d, "designer", nil)
}

// dalWantLessons asserts one role's lessons overlay reads back as want.

func TestDeleteLessonsForRole(t *testing.T) {
	d := newAPITestDAL(t)
	since := nowSecs()
	for _, roleKey := range []string{"r-abc", "r-abcdef"} {
		if err := d.PutLessons(Lessons{RoleKey: roleKey, Text: "learned by " + roleKey}); err != nil {
			t.Fatalf("PutLessons(%q): %v", roleKey, err)
		}
		if err := d.SaveWithDocumentHistory("lessons", roleKey, "owner",
			dalStaticSnapshot(`{"v":1}`), func(sqlExecer) error { return nil }); err != nil {
			t.Fatalf("SaveWithDocumentHistory(%q): %v", roleKey, err)
		}
	}

	deleted, err := d.DeleteLessonsForRole("r-abc")
	if err != nil {
		t.Fatalf("DeleteLessonsForRole: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteLessonsForRole: want 1, got %d", deleted)
	}
	dalWantLessons(t, d, "r-abc", nil)
	gone, err := d.ListDocumentHistory("lessons", "r-abc")
	if err != nil {
		t.Fatalf("ListDocumentHistory: %v", err)
	}
	dalWantHistory(t, "the retained versions go with the document", gone, nil, since)

	dalWantLessons(t, d, "r-abcdef", &Lessons{RoleKey: "r-abcdef", Text: "learned by r-abcdef"})
	kept, err := d.ListDocumentHistory("lessons", "r-abcdef")
	if err != nil {
		t.Fatalf("ListDocumentHistory(neighbour): %v", err)
	}
	dalWantHistory(t, "a role whose key merely starts with the deleted one keeps its history", kept,
		[]DocumentHistory{{DocumentKind: "lessons", DocumentKey: "r-abcdef", ContentJSON: `{"v":1}`, ActorID: "owner"}}, since)

	deleted, err = d.DeleteLessonsForRole("r-abc")
	if err != nil {
		t.Fatalf("DeleteLessonsForRole again: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleting an absent overlay: want 0, got %d", deleted)
	}
}

func TestGetInsightOn(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := getInsightOn(d.rdb, "engineer")
	if err != nil {
		t.Fatalf("getInsightOn before any write: %v", err)
	}
	if got != nil {
		t.Fatalf("a role nobody has written reads back as nil, got %+v", *got)
	}

	written := Insight{RoleKey: "engineer", Text: "how this role weighs a call", Tombstoned: true}
	if err := d.PutInsight(written); err != nil {
		t.Fatalf("PutInsight: %v", err)
	}
	got, err = getInsightOn(d.rdb, "engineer")
	if err != nil {
		t.Fatalf("getInsightOn: %v", err)
	}
	if !reflect.DeepEqual(got, &written) {
		t.Fatalf("getInsightOn:\n got %+v\nwant %+v", got, written)
	}

	if err := d.inTx(func(tx *sql.Tx) error {
		inside, err := getInsightOn(tx, "engineer")
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(inside, &written) {
			t.Fatalf("the transactional read sees the same doc:\n got %+v\nwant %+v", inside, written)
		}
		return nil
	}); err != nil {
		t.Fatalf("inTx: %v", err)
	}
}

func TestPutInsightOn(t *testing.T) {
	d := newAPITestDAL(t)
	first := Insight{RoleKey: "engineer", Text: "first draft"}
	if err := putInsightOn(d.wdb, first); err != nil {
		t.Fatalf("putInsightOn: %v", err)
	}
	dalWantInsight(t, d, "engineer", &first)

	second := Insight{RoleKey: "engineer", Text: "", Tombstoned: true}
	if err := putInsightOn(d.wdb, second); err != nil {
		t.Fatalf("putInsightOn(reset marker): %v", err)
	}
	dalWantInsight(t, d, "engineer", &second)

	failed := errors.New("the rest of the transaction refused")
	if err := d.inTx(func(tx *sql.Tx) error {
		if err := putInsightOn(tx, Insight{RoleKey: "designer", Text: "rolled back"}); err != nil {
			return err
		}
		return failed
	}); !errors.Is(err, failed) {
		t.Fatalf("inTx: want %v, got %v", failed, err)
	}
	dalWantInsight(t, d, "designer", nil)
}

// dalWantInsight asserts one role's insight doc reads back as want.

func TestDeleteInsightForRole(t *testing.T) {
	d := newAPITestDAL(t)
	since := nowSecs()
	for _, roleKey := range []string{"r-abc", "r-abcdef"} {
		if err := d.PutInsight(Insight{RoleKey: roleKey, Text: "weighed by " + roleKey}); err != nil {
			t.Fatalf("PutInsight(%q): %v", roleKey, err)
		}
		if err := d.SaveWithDocumentHistory("insight", roleKey, "owner",
			dalStaticSnapshot(`{"v":1}`), func(sqlExecer) error { return nil }); err != nil {
			t.Fatalf("SaveWithDocumentHistory(%q): %v", roleKey, err)
		}
	}

	deleted, err := d.DeleteInsightForRole("r-abc")
	if err != nil {
		t.Fatalf("DeleteInsightForRole: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteInsightForRole: want 1, got %d", deleted)
	}
	dalWantInsight(t, d, "r-abc", nil)
	gone, err := d.ListDocumentHistory("insight", "r-abc")
	if err != nil {
		t.Fatalf("ListDocumentHistory: %v", err)
	}
	dalWantHistory(t, "the retained versions go with the document", gone, nil, since)

	dalWantInsight(t, d, "r-abcdef", &Insight{RoleKey: "r-abcdef", Text: "weighed by r-abcdef"})
	kept, err := d.ListDocumentHistory("insight", "r-abcdef")
	if err != nil {
		t.Fatalf("ListDocumentHistory(neighbour): %v", err)
	}
	dalWantHistory(t, "a role whose key merely starts with the deleted one keeps its history", kept,
		[]DocumentHistory{{DocumentKind: "insight", DocumentKey: "r-abcdef", ContentJSON: `{"v":1}`, ActorID: "owner"}}, since)

	deleted, err = d.DeleteInsightForRole("r-abc")
	if err != nil {
		t.Fatalf("DeleteInsightForRole again: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleting an absent doc: want 0, got %d", deleted)
	}
}

func TestGetBootDocumentOn(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := getBootDocumentOn(d.rdb, docKindBootSequence, "claude")
	if err != nil {
		t.Fatalf("getBootDocumentOn before any overlay: %v", err)
	}
	if got != nil {
		t.Fatalf("a block nobody has overlaid reads back as nil, got %+v", *got)
	}

	written := BootDocument{Kind: docKindBootSequence, Key: "claude", Text: "step one", Tombstoned: true}
	if err := d.PutBootDocument(written); err != nil {
		t.Fatalf("PutBootDocument: %v", err)
	}
	if err := d.PutBootDocument(BootDocument{Kind: docKindBootSequence, Key: "codex", Text: "another runtime"}); err != nil {
		t.Fatalf("PutBootDocument(codex): %v", err)
	}
	if err := d.PutBootDocument(BootDocument{Kind: docKindSystemInteraction, Key: "claude", Text: "another kind"}); err != nil {
		t.Fatalf("PutBootDocument(system_interaction): %v", err)
	}

	got, err = getBootDocumentOn(d.rdb, docKindBootSequence, "claude")
	if err != nil {
		t.Fatalf("getBootDocumentOn: %v", err)
	}
	if !reflect.DeepEqual(got, &written) {
		t.Fatalf("the pair addresses one row:\n got %+v\nwant %+v", got, written)
	}

	if err := d.inTx(func(tx *sql.Tx) error {
		inside, err := getBootDocumentOn(tx, docKindBootSequence, "claude")
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(inside, &written) {
			t.Fatalf("the transactional read sees the same overlay:\n got %+v\nwant %+v", inside, written)
		}
		return nil
	}); err != nil {
		t.Fatalf("inTx: %v", err)
	}
}

func TestPutBootDocumentOn(t *testing.T) {
	d := newAPITestDAL(t)
	first := BootDocument{Kind: docKindSystemInteraction, Key: "global", Text: "first draft"}
	if err := putBootDocumentOn(d.wdb, first); err != nil {
		t.Fatalf("putBootDocumentOn: %v", err)
	}
	dalWantBootDocument(t, d, docKindSystemInteraction, "global", &first)

	second := BootDocument{Kind: docKindSystemInteraction, Key: "global", Text: "", Tombstoned: true}
	if err := putBootDocumentOn(d.wdb, second); err != nil {
		t.Fatalf("putBootDocumentOn(reset marker): %v", err)
	}
	dalWantBootDocument(t, d, docKindSystemInteraction, "global", &second)

	failed := errors.New("the rest of the transaction refused")
	if err := d.inTx(func(tx *sql.Tx) error {
		if err := putBootDocumentOn(tx, BootDocument{Kind: docKindOffboard, Key: "global", Text: "rolled back"}); err != nil {
			return err
		}
		return failed
	}); !errors.Is(err, failed) {
		t.Fatalf("inTx: want %v, got %v", failed, err)
	}
	dalWantBootDocument(t, d, docKindOffboard, "global", nil)
}

// dalWantBootDocument asserts one boot-context overlay reads back as want.

func TestGetAccountAlias(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.GetAccountAlias("acct-a")
	if err != nil {
		t.Fatalf("GetAccountAlias before any overlay: %v", err)
	}
	if got != nil {
		t.Fatalf("an account nobody has renamed reads back as nil, got %+v", *got)
	}

	written := AccountAlias{Account: "acct-a", DisplayName: "Studio account"}
	if err := d.PutAccountAlias(written); err != nil {
		t.Fatalf("PutAccountAlias: %v", err)
	}
	if err := d.PutAccountAlias(AccountAlias{Account: "acct-b", DisplayName: "Spare"}); err != nil {
		t.Fatalf("PutAccountAlias(acct-b): %v", err)
	}

	got, err = d.GetAccountAlias("acct-a")
	if err != nil {
		t.Fatalf("GetAccountAlias: %v", err)
	}
	if !reflect.DeepEqual(got, &written) {
		t.Fatalf("GetAccountAlias:\n got %+v\nwant %+v", got, written)
	}
}

func TestAccountDisplayNames(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.AccountDisplayNames()
	if err != nil {
		t.Fatalf("AccountDisplayNames before any overlay: %v", err)
	}
	if !reflect.DeepEqual(got, map[string]string{}) {
		t.Fatalf("AccountDisplayNames before any overlay: want an empty map, got %v", got)
	}

	for _, a := range []AccountAlias{
		{Account: "acct-a", DisplayName: "Studio account"},
		{Account: "acct-b", DisplayName: "Spare"},
		{Account: "acct-c", DisplayName: ""},
	} {
		if err := d.PutAccountAlias(a); err != nil {
			t.Fatalf("PutAccountAlias(%q): %v", a.Account, err)
		}
	}

	got, err = d.AccountDisplayNames()
	if err != nil {
		t.Fatalf("AccountDisplayNames: %v", err)
	}
	want := map[string]string{"acct-a": "Studio account", "acct-b": "Spare"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a blank display name is skipped: want %v, got %v", want, got)
	}
}

func TestPutAccountAlias(t *testing.T) {
	d := newAPITestDAL(t)
	if err := d.PutAccountAlias(AccountAlias{Account: "acct-a", DisplayName: "First name"}); err != nil {
		t.Fatalf("PutAccountAlias: %v", err)
	}
	got, err := d.GetAccountAlias("acct-a")
	if err != nil {
		t.Fatalf("GetAccountAlias: %v", err)
	}
	want := &AccountAlias{Account: "acct-a", DisplayName: "First name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetAccountAlias:\n got %+v\nwant %+v", got, want)
	}

	if err := d.PutAccountAlias(AccountAlias{Account: "acct-a", DisplayName: "Second name"}); err != nil {
		t.Fatalf("PutAccountAlias(rename): %v", err)
	}
	got, err = d.GetAccountAlias("acct-a")
	if err != nil {
		t.Fatalf("GetAccountAlias: %v", err)
	}
	want = &AccountAlias{Account: "acct-a", DisplayName: "Second name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the same tag keeps one row:\n got %+v\nwant %+v", got, want)
	}
	names, err := d.AccountDisplayNames()
	if err != nil {
		t.Fatalf("AccountDisplayNames: %v", err)
	}
	if !reflect.DeepEqual(names, map[string]string{"acct-a": "Second name"}) {
		t.Fatalf("the upsert never duplicates a tag, got %v", names)
	}
}

func TestGetMachineAlias(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.GetMachineAlias("mac-1")
	if err != nil {
		t.Fatalf("GetMachineAlias before any overlay: %v", err)
	}
	if got != nil {
		t.Fatalf("a machine nobody has renamed reads back as nil, got %+v", *got)
	}

	written := MachineAlias{MachineID: "mac-1", DisplayName: "Studio Mac"}
	if err := d.PutMachineAlias(written); err != nil {
		t.Fatalf("PutMachineAlias: %v", err)
	}
	if err := d.PutMachineAlias(MachineAlias{MachineID: "mac-2", DisplayName: "Spare Mac"}); err != nil {
		t.Fatalf("PutMachineAlias(mac-2): %v", err)
	}

	got, err = d.GetMachineAlias("mac-1")
	if err != nil {
		t.Fatalf("GetMachineAlias: %v", err)
	}
	if !reflect.DeepEqual(got, &written) {
		t.Fatalf("GetMachineAlias:\n got %+v\nwant %+v", got, written)
	}
}

func TestMachineDisplayNames(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.MachineDisplayNames()
	if err != nil {
		t.Fatalf("MachineDisplayNames before any overlay: %v", err)
	}
	if !reflect.DeepEqual(got, map[string]string{}) {
		t.Fatalf("MachineDisplayNames before any overlay: want an empty map, got %v", got)
	}

	for _, a := range []MachineAlias{
		{MachineID: "mac-1", DisplayName: "Studio Mac"},
		{MachineID: "mac-2", DisplayName: "Spare Mac"},
		{MachineID: "mac-3", DisplayName: ""},
	} {
		if err := d.PutMachineAlias(a); err != nil {
			t.Fatalf("PutMachineAlias(%q): %v", a.MachineID, err)
		}
	}

	got, err = d.MachineDisplayNames()
	if err != nil {
		t.Fatalf("MachineDisplayNames: %v", err)
	}
	want := map[string]string{"mac-1": "Studio Mac", "mac-2": "Spare Mac"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a blank display name is skipped: want %v, got %v", want, got)
	}
}

func TestPutMachineAlias(t *testing.T) {
	d := newAPITestDAL(t)
	if err := d.PutMachineAlias(MachineAlias{MachineID: "mac-1", DisplayName: "First name"}); err != nil {
		t.Fatalf("PutMachineAlias: %v", err)
	}
	got, err := d.GetMachineAlias("mac-1")
	if err != nil {
		t.Fatalf("GetMachineAlias: %v", err)
	}
	want := &MachineAlias{MachineID: "mac-1", DisplayName: "First name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetMachineAlias:\n got %+v\nwant %+v", got, want)
	}

	if err := d.PutMachineAlias(MachineAlias{MachineID: "mac-1", DisplayName: "Second name"}); err != nil {
		t.Fatalf("PutMachineAlias(rename): %v", err)
	}
	got, err = d.GetMachineAlias("mac-1")
	if err != nil {
		t.Fatalf("GetMachineAlias: %v", err)
	}
	want = &MachineAlias{MachineID: "mac-1", DisplayName: "Second name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the same machine keeps one row:\n got %+v\nwant %+v", got, want)
	}
	names, err := d.MachineDisplayNames()
	if err != nil {
		t.Fatalf("MachineDisplayNames: %v", err)
	}
	if !reflect.DeepEqual(names, map[string]string{"mac-1": "Second name"}) {
		t.Fatalf("the upsert never duplicates a machine, got %v", names)
	}
}

func TestScanReplyCard(t *testing.T) {
	d := newAPITestDAL(t)
	full := dalReplyCard("rc-1", 100)
	if err := d.PutReplyCard(full); err != nil {
		t.Fatalf("PutReplyCard: %v", err)
	}
	bare := ReplyCard{ID: "rc-2", FromMember: "bob", Kind: "action", Status: "waiting"}
	if err := d.PutReplyCard(bare); err != nil {
		t.Fatalf("PutReplyCard(bare): %v", err)
	}

	query := `SELECT ` + replyCardColumns + ` FROM reply_card WHERE id = ?`
	got, err := scanReplyCard(d.rdb.QueryRow(query, "rc-1"))
	if err != nil {
		t.Fatalf("scanReplyCard(rc-1): %v", err)
	}
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("scanReplyCard(rc-1):\n got %+v\nwant %+v", got, full)
	}

	got, err = scanReplyCard(d.rdb.QueryRow(query, "rc-2"))
	if err != nil {
		t.Fatalf("scanReplyCard(rc-2): %v", err)
	}
	want := ReplyCard{
		ID: "rc-2", FromMember: "bob", Kind: "action", SelectMode: replyCardSelectModeSingle,
		Status: "waiting", Options: []ReplyCardOption{},
		AnswerAttachments: []any{}, Attachments: []any{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a card with no options, no answer and no refs:\n got %+v\nwant %+v", got, want)
	}
	if got.AnswerOptionIdxs != nil {
		t.Fatalf("no option circled must read back as nil, got %v", got.AnswerOptionIdxs)
	}

	if _, err := d.wdb.Exec(
		`UPDATE reply_card SET options = ? WHERE id = ?`, "not json", "rc-2"); err != nil {
		t.Fatalf("seed a corrupt options blob: %v", err)
	}
	_, err = scanReplyCard(d.rdb.QueryRow(query, "rc-2"))
	if err == nil || !strings.Contains(err.Error(), "reply_card rc-2: bad options JSON") {
		t.Fatalf("scanReplyCard(rc-2): want a bad-options-JSON error naming the row, got %v", err)
	}

	if _, err := scanReplyCard(d.rdb.QueryRow(query, "rc-ghost")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("scanReplyCard(rc-ghost): want sql.ErrNoRows, got %v", err)
	}
}

func TestListReplyCards(t *testing.T) {
	d := newAPITestDAL(t)
	empty, err := d.ListReplyCards()
	if err != nil {
		t.Fatalf("ListReplyCards before any card: %v", err)
	}
	if empty != nil {
		t.Fatalf("ListReplyCards before any card: want none, got %+v", empty)
	}

	late := dalReplyCard("rc-late", 300)
	middle := dalReplyCard("rc-middle", 200)
	middle.Status = "waiting"
	early := dalReplyCard("rc-early", 100)
	early.Status = "expired"
	for _, c := range []ReplyCard{late, middle, early} {
		if err := d.PutReplyCard(c); err != nil {
			t.Fatalf("PutReplyCard(%q): %v", c.ID, err)
		}
	}

	got, err := d.ListReplyCards()
	if err != nil {
		t.Fatalf("ListReplyCards: %v", err)
	}
	want := []ReplyCard{early, middle, late}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("every card, whatever its status, oldest first:\n got %+v\nwant %+v", got, want)
	}
}

func TestGetReplyCard(t *testing.T) {
	d := newAPITestDAL(t)
	card := dalReplyCard("rc-1", 100)
	if err := d.PutReplyCard(card); err != nil {
		t.Fatalf("PutReplyCard: %v", err)
	}
	dalWantReplyCard(t, d, "rc-1", &card)
	dalWantReplyCard(t, d, "rc-ghost", nil)
}

func TestPutChatWithAttachments(t *testing.T) {
	d := newAPITestDAL(t)
	m := dalChatWithAtts("m1", "ann", "bob", 100,
		dalAttRef("att-1", "image/png", "a.png"), dalAttRef("att-2", "image/png", "b.png"))
	blobs := []ChatAttachment{
		{ID: "att-1", Mime: "image/png", Data: []byte("first")},
		{ID: "att-2", Mime: "image/png", Data: []byte("second")},
	}
	if err := d.PutChatWithAttachments(m, blobs); err != nil {
		t.Fatalf("PutChatWithAttachments: %v", err)
	}
	dalWantChats(t, "the message lands", dalMustListChat(t, d), []ChatMessage{m})
	want := []string{"att-1", "att-2"}
	if got := dalStoredBlobIDs(t, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("every blob it references lands: want %v, got %v", want, got)
	}

	refless := dalChat("m2", "bob", "ann", 200)
	if err := d.PutChatWithAttachments(refless, nil); err != nil {
		t.Fatalf("PutChatWithAttachments(no blob): %v", err)
	}
	dalWantChats(t, "a message with no fresh blob still lands",
		dalMustListChat(t, d), []ChatMessage{m, refless})

	third := dalChatWithAtts("m3", "ann", "bob", 300, dalAttRef("att-3", "image/png", "c.png"))
	if err := d.PutChatWithAttachments(third, []ChatAttachment{
		{ID: "att-3", Mime: "image/png", Data: []byte("third")},
		{ID: "att-4", Mime: "image/png", Data: []byte("fourth")},
	}); err != nil {
		t.Fatalf("PutChatWithAttachments(a blob the message does not name): %v", err)
	}
	dalWantChats(t, "the third message lands", dalMustListChat(t, d),
		[]ChatMessage{m, refless, third})
	want = []string{"att-1", "att-2", "att-3", "att-4"}
	if got := dalStoredBlobIDs(t, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("every blob handed in lands, named or not: want %v, got %v", want, got)
	}

	if _, err := d.wdb.Exec(`DROP TABLE chat_message`); err != nil {
		t.Fatalf("drop chat_message: %v", err)
	}
	if err := d.PutChatWithAttachments(
		dalChatWithAtts("m4", "ann", "bob", 400, dalAttRef("att-5", "image/png", "e.png")),
		[]ChatAttachment{{ID: "att-5", Mime: "image/png", Data: []byte("fifth")}},
	); err == nil {
		t.Fatalf("a message that cannot be written must fail")
	}
	want = []string{"att-1", "att-2", "att-3", "att-4"}
	if got := dalStoredBlobIDs(t, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("a failed write leaves no blob behind: want %v, got %v", want, got)
	}
}

func TestPutReplyCardWithChat(t *testing.T) {
	d := newAPITestDAL(t)
	card := dalReplyCard("rc-1", 100)
	m := dalChatWithAtts("m1", "ann", "owner", 100, dalAttRef("att-question", "image/png", "q.png"))
	blobs := []ChatAttachment{{ID: "att-question", Mime: "image/png", Data: []byte("the question")}}

	if err := d.PutReplyCardWithChat(card, m, blobs); err != nil {
		t.Fatalf("PutReplyCardWithChat: %v", err)
	}
	dalWantReplyCard(t, d, "rc-1", &card)
	dalWantChats(t, "the companion message lands", dalMustListChat(t, d), []ChatMessage{m})
	want := []string{"att-question"}
	if got := dalStoredBlobIDs(t, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("the question-side blob lands: want %v, got %v", want, got)
	}

	if _, err := d.wdb.Exec(`DROP TABLE reply_card`); err != nil {
		t.Fatalf("drop reply_card: %v", err)
	}
	if err := d.PutReplyCardWithChat(
		dalReplyCard("rc-2", 200),
		dalChatWithAtts("m2", "ann", "owner", 200, dalAttRef("att-doomed", "image/png", "d.png")),
		[]ChatAttachment{{ID: "att-doomed", Mime: "image/png", Data: []byte("doomed")}},
	); err == nil {
		t.Fatalf("a card that cannot be written must fail")
	}
	dalWantChats(t, "a failed card write leaves no dangling message",
		dalMustListChat(t, d), []ChatMessage{m})
	if got := dalStoredBlobIDs(t, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("a failed card write leaves no blob behind: want %v, got %v", want, got)
	}
}

func TestPutReplyCardWithAttachments(t *testing.T) {
	d := newAPITestDAL(t)
	card := dalReplyCard("rc-1", 100)
	blobs := []ChatAttachment{{ID: "att-answer", Mime: "image/png", Data: []byte("the answer")}}

	if err := d.PutReplyCardWithAttachments(card, blobs); err != nil {
		t.Fatalf("PutReplyCardWithAttachments: %v", err)
	}
	dalWantReplyCard(t, d, "rc-1", &card)
	want := []string{"att-answer"}
	if got := dalStoredBlobIDs(t, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("the answer-side blob lands: want %v, got %v", want, got)
	}

	blobless := dalReplyCard("rc-2", 200)
	blobless.AnswerAttachments = []any{}
	if err := d.PutReplyCardWithAttachments(blobless, nil); err != nil {
		t.Fatalf("PutReplyCardWithAttachments(no blob): %v", err)
	}
	dalWantReplyCard(t, d, "rc-2", &blobless)

	if _, err := d.wdb.Exec(`DROP TABLE reply_card`); err != nil {
		t.Fatalf("drop reply_card: %v", err)
	}
	if err := d.PutReplyCardWithAttachments(dalReplyCard("rc-3", 300),
		[]ChatAttachment{{ID: "att-doomed", Mime: "image/png", Data: []byte("doomed")}},
	); err == nil {
		t.Fatalf("a card that cannot be written must fail")
	}
	if got := dalStoredBlobIDs(t, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("a failed write leaves no blob behind: want %v, got %v", want, got)
	}
}

func TestInTx(t *testing.T) {
	d := newAPITestDAL(t)

	if err := d.inTx(func(tx *sql.Tx) error {
		return putChatOn(tx, dalChat("m1", "ann", "bob", 100))
	}); err != nil {
		t.Fatalf("inTx: %v", err)
	}
	dalWantChats(t, "a body that returns nil commits",
		dalMustListChat(t, d), []ChatMessage{dalChat("m1", "ann", "bob", 100)})

	failed := errors.New("the body refused")
	if err := d.inTx(func(tx *sql.Tx) error {
		if err := putChatOn(tx, dalChat("m2", "ann", "bob", 200)); err != nil {
			return err
		}
		return failed
	}); !errors.Is(err, failed) {
		t.Fatalf("inTx must answer with the body's own error: want %v, got %v", failed, err)
	}
	dalWantChats(t, "a body that returns an error rolls back",
		dalMustListChat(t, d), []ChatMessage{dalChat("m1", "ann", "bob", 100)})

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatalf("the panic must reach the caller")
			}
		}()
		_ = d.inTx(func(tx *sql.Tx) error {
			if err := putChatOn(tx, dalChat("m3", "ann", "bob", 300)); err != nil {
				return err
			}
			panic("the body panicked")
		})
	}()
	dalWantChats(t, "a panicking body rolls back and leaves the write pool usable",
		dalMustListChat(t, d), []ChatMessage{dalChat("m1", "ann", "bob", 100)})

	if err := d.inTx(func(tx *sql.Tx) error {
		return putChatOn(tx, dalChat("m4", "ann", "bob", 400))
	}); err != nil {
		t.Fatalf("inTx after a panic: %v", err)
	}
	dalWantChats(t, "the pool still takes writes after a panic", dalMustListChat(t, d),
		[]ChatMessage{dalChat("m1", "ann", "bob", 100), dalChat("m4", "ann", "bob", 400)})
}

func TestPutReplyCardOn(t *testing.T) {
	d := newAPITestDAL(t)
	first := dalReplyCard("rc-1", 100)
	if err := putReplyCardOn(d.wdb, first); err != nil {
		t.Fatalf("putReplyCardOn: %v", err)
	}
	dalWantReplyCard(t, d, "rc-1", &first)

	answered := dalReplyCard("rc-1", 100)
	answered.Status = "expired"
	answered.AnswerText = "withdrawn"
	answered.AnswerOptionIdxs = []int{1}
	answered.Options = []ReplyCardOption{{Text: "only choice", AIPick: false}}
	answered.AnswerAttachments = []any{}
	answered.Attachments = []any{}
	if err := putReplyCardOn(d.wdb, answered); err != nil {
		t.Fatalf("putReplyCardOn(rewrite): %v", err)
	}
	dalWantReplyCard(t, d, "rc-1", &answered)

	defaulted := ReplyCard{ID: "rc-2", FromMember: "bob", Kind: "action", Status: "waiting",
		AnswerOptionIdxs: []int{}}
	if err := putReplyCardOn(d.wdb, defaulted); err != nil {
		t.Fatalf("putReplyCardOn(defaults): %v", err)
	}
	dalWantReplyCard(t, d, "rc-2", &ReplyCard{
		ID: "rc-2", FromMember: "bob", Kind: "action", Status: "waiting",
		SelectMode: replyCardSelectModeSingle, Options: []ReplyCardOption{},
		AnswerAttachments: []any{}, Attachments: []any{},
	})

	failed := errors.New("the rest of the transaction refused")
	if err := d.inTx(func(tx *sql.Tx) error {
		if err := putReplyCardOn(tx, dalReplyCard("rc-3", 300)); err != nil {
			return err
		}
		return failed
	}); !errors.Is(err, failed) {
		t.Fatalf("inTx: want %v, got %v", failed, err)
	}
	dalWantReplyCard(t, d, "rc-3", nil)
}

func TestScanWebhook(t *testing.T) {
	d := newAPITestDAL(t)
	full := dalWebhook(dalHookAlpha, "ann", "deploys", 100)
	if err := d.PutWebhookEndpoint(full); err != nil {
		t.Fatalf("PutWebhookEndpoint: %v", err)
	}
	bare := WebhookEndpoint{Token: dalHookBeta, MemberID: "bob", EndpointID: "alerts",
		Status: WebhookStatusDisabled, CreatedTS: 200}
	if err := d.PutWebhookEndpoint(bare); err != nil {
		t.Fatalf("PutWebhookEndpoint(bare): %v", err)
	}

	query := `SELECT ` + webhookColumns + ` FROM webhook_endpoint WHERE token = ?`
	got, err := scanWebhook(d.rdb.QueryRow(query, dalHookAlpha))
	if err != nil {
		t.Fatalf("scanWebhook(alpha): %v", err)
	}
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("scanWebhook(alpha):\n got %+v\nwant %+v", got, full)
	}

	got, err = scanWebhook(d.rdb.QueryRow(query, dalHookBeta))
	if err != nil {
		t.Fatalf("scanWebhook(beta): %v", err)
	}
	bare.Platform = WebhookPlatformGeneric
	if !reflect.DeepEqual(got, bare) {
		t.Fatalf("an inlet with no shared secret reads it back as the empty string:\n got %+v\nwant %+v", got, bare)
	}

	if _, err := scanWebhook(d.rdb.QueryRow(query, "wh-ghost")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("scanWebhook(ghost): want sql.ErrNoRows, got %v", err)
	}
}

func TestGetWebhookByToken(t *testing.T) {
	d := newAPITestDAL(t)
	alpha := dalWebhook(dalHookAlpha, "ann", "deploys", 100)
	if err := d.PutWebhookEndpoint(alpha); err != nil {
		t.Fatalf("PutWebhookEndpoint: %v", err)
	}
	if err := d.PutWebhookEndpoint(dalWebhook(dalHookBeta, "bob", "alerts", 200)); err != nil {
		t.Fatalf("PutWebhookEndpoint(beta): %v", err)
	}

	dalWantWebhook(t, d, dalHookAlpha, &alpha)
	dalWantWebhook(t, d, "wh-unknown", nil)
	dalWantWebhook(t, d, "", nil)
}

func TestGetWebhookByMemberEndpoint(t *testing.T) {
	d := newAPITestDAL(t)
	alpha := dalWebhook(dalHookAlpha, "ann", "deploys", 100)
	if err := d.PutWebhookEndpoint(alpha); err != nil {
		t.Fatalf("PutWebhookEndpoint: %v", err)
	}
	if err := d.PutWebhookEndpoint(dalWebhook(dalHookBeta, "bob", "deploys", 200)); err != nil {
		t.Fatalf("PutWebhookEndpoint(beta): %v", err)
	}

	got, err := d.GetWebhookByMemberEndpoint("ann", "deploys")
	if err != nil {
		t.Fatalf("GetWebhookByMemberEndpoint: %v", err)
	}
	if !reflect.DeepEqual(got, &alpha) {
		t.Fatalf("the pair addresses one inlet:\n got %+v\nwant %+v", got, alpha)
	}

	for _, tc := range []struct{ name, member, endpoint string }{
		{"another member's address key", "carl", "deploys"},
		{"an address key this member does not own", "ann", "alerts"},
	} {
		got, err := d.GetWebhookByMemberEndpoint(tc.member, tc.endpoint)
		if err != nil {
			t.Fatalf("GetWebhookByMemberEndpoint(%s): %v", tc.name, err)
		}
		if got != nil {
			t.Fatalf("GetWebhookByMemberEndpoint(%s): want nil, got %+v", tc.name, *got)
		}
	}
}

func TestListWebhooksByMember(t *testing.T) {
	d := newAPITestDAL(t)
	empty, err := d.ListWebhooksByMember("ann")
	if err != nil {
		t.Fatalf("ListWebhooksByMember before any inlet: %v", err)
	}
	if empty != nil {
		t.Fatalf("ListWebhooksByMember before any inlet: want none, got %+v", empty)
	}

	late := dalWebhook("wh-late", "ann", "late", 300)
	early := dalWebhook("wh-early", "ann", "early", 100)
	other := dalWebhook(dalHookBeta, "bob", "alerts", 200)
	for _, e := range []WebhookEndpoint{late, early, other} {
		if err := d.PutWebhookEndpoint(e); err != nil {
			t.Fatalf("PutWebhookEndpoint(%q): %v", e.EndpointID, err)
		}
	}

	got, err := d.ListWebhooksByMember("ann")
	if err != nil {
		t.Fatalf("ListWebhooksByMember: %v", err)
	}
	want := []WebhookEndpoint{early, late}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("one member's own inlets, oldest first:\n got %+v\nwant %+v", got, want)
	}
}

func TestPutWebhookEndpoint(t *testing.T) {
	d := newAPITestDAL(t)
	created := dalWebhook(dalHookAlpha, "ann", "deploys", 100)
	if err := d.PutWebhookEndpoint(created); err != nil {
		t.Fatalf("PutWebhookEndpoint: %v", err)
	}
	dalWantWebhook(t, d, dalHookAlpha, &created)

	edited := created
	edited.Purpose = "edited purpose"
	edited.Status = WebhookStatusDisabled
	edited.SigningSecret = ""
	edited.DeliveredCount = 9
	if err := d.PutWebhookEndpoint(edited); err != nil {
		t.Fatalf("PutWebhookEndpoint(edit): %v", err)
	}
	dalWantWebhook(t, d, dalHookAlpha, &edited)
	one, err := d.ListWebhooksByMember("ann")
	if err != nil {
		t.Fatalf("ListWebhooksByMember: %v", err)
	}
	if len(one) != 1 {
		t.Fatalf("the same key keeps one row, got %+v", one)
	}

	duplicate := dalWebhook(dalHookBeta, "ann", "deploys", 200)
	if err := d.PutWebhookEndpoint(duplicate); err == nil {
		t.Fatalf("a second inlet at the same member address must be refused")
	}
	dalWantWebhook(t, d, dalHookBeta, nil)
}

func TestTouchWebhookReceived(t *testing.T) {
	d := newAPITestDAL(t)
	alpha := dalWebhook(dalHookAlpha, "ann", "deploys", 100)
	beta := dalWebhook(dalHookBeta, "bob", "alerts", 200)
	for _, e := range []WebhookEndpoint{alpha, beta} {
		if err := d.PutWebhookEndpoint(e); err != nil {
			t.Fatalf("PutWebhookEndpoint(%q): %v", e.EndpointID, err)
		}
	}

	if err := d.TouchWebhookReceived(dalHookAlpha, 1800000000); err != nil {
		t.Fatalf("TouchWebhookReceived: %v", err)
	}
	alpha.LastReceivedTS = 1800000000
	dalWantWebhook(t, d, dalHookAlpha, &alpha)
	dalWantWebhook(t, d, dalHookBeta, &beta)

	if err := d.TouchWebhookReceived("wh-unknown", 1800000001); err != nil {
		t.Fatalf("TouchWebhookReceived on an unknown inlet: %v", err)
	}
	dalWantWebhook(t, d, "wh-unknown", nil)
}

func TestMarkWebhookDelivered(t *testing.T) {
	d := newAPITestDAL(t)
	alpha := dalWebhook(dalHookAlpha, "ann", "deploys", 100)
	beta := dalWebhook(dalHookBeta, "bob", "alerts", 200)
	for _, e := range []WebhookEndpoint{alpha, beta} {
		if err := d.PutWebhookEndpoint(e); err != nil {
			t.Fatalf("PutWebhookEndpoint(%q): %v", e.EndpointID, err)
		}
	}

	if err := d.MarkWebhookDelivered(dalHookAlpha, 1800000000); err != nil {
		t.Fatalf("MarkWebhookDelivered: %v", err)
	}
	if err := d.MarkWebhookDelivered(dalHookAlpha, 1800000001); err != nil {
		t.Fatalf("MarkWebhookDelivered again: %v", err)
	}
	alpha.DeliveredCount = 5
	alpha.LastReceivedTS = 1800000001
	dalWantWebhook(t, d, dalHookAlpha, &alpha)
	dalWantWebhook(t, d, dalHookBeta, &beta)

	if err := d.MarkWebhookDelivered("wh-unknown", 1800000002); err != nil {
		t.Fatalf("MarkWebhookDelivered on an unknown inlet: %v", err)
	}
	dalWantWebhook(t, d, "wh-unknown", nil)
}

func TestMarkWebhookDropped(t *testing.T) {
	d := newAPITestDAL(t)
	alpha := dalWebhook(dalHookAlpha, "ann", "deploys", 100)
	beta := dalWebhook(dalHookBeta, "bob", "alerts", 200)
	for _, e := range []WebhookEndpoint{alpha, beta} {
		if err := d.PutWebhookEndpoint(e); err != nil {
			t.Fatalf("PutWebhookEndpoint(%q): %v", e.EndpointID, err)
		}
	}

	if err := d.MarkWebhookDropped(dalHookAlpha, WebhookDropReasonDisabled, 1800000000); err != nil {
		t.Fatalf("MarkWebhookDropped: %v", err)
	}
	if err := d.MarkWebhookDropped(dalHookAlpha, WebhookDropReasonMemberGone, 1800000001); err != nil {
		t.Fatalf("MarkWebhookDropped again: %v", err)
	}
	alpha.DroppedCount = 4
	alpha.LastDropReason = WebhookDropReasonMemberGone
	alpha.LastReceivedTS = 1800000001
	dalWantWebhook(t, d, dalHookAlpha, &alpha)
	dalWantWebhook(t, d, dalHookBeta, &beta)

	if err := d.MarkWebhookDropped("wh-unknown", WebhookDropReasonSigFailed, 1800000002); err != nil {
		t.Fatalf("MarkWebhookDropped on an unknown inlet: %v", err)
	}
	dalWantWebhook(t, d, "wh-unknown", nil)
}

func TestSetWebhookStatus(t *testing.T) {
	d := newAPITestDAL(t)
	alpha := dalWebhook(dalHookAlpha, "ann", "deploys", 100)
	beta := dalWebhook(dalHookBeta, "bob", "alerts", 200)
	for _, e := range []WebhookEndpoint{alpha, beta} {
		if err := d.PutWebhookEndpoint(e); err != nil {
			t.Fatalf("PutWebhookEndpoint(%q): %v", e.EndpointID, err)
		}
	}

	if err := d.SetWebhookStatus(dalHookAlpha, WebhookStatusDisabled); err != nil {
		t.Fatalf("SetWebhookStatus: %v", err)
	}
	alpha.Status = WebhookStatusDisabled
	dalWantWebhook(t, d, dalHookAlpha, &alpha)

	if err := d.SetWebhookStatus(dalHookAlpha, WebhookStatusEnabled); err != nil {
		t.Fatalf("SetWebhookStatus(back): %v", err)
	}
	alpha.Status = WebhookStatusEnabled
	dalWantWebhook(t, d, dalHookAlpha, &alpha)
	dalWantWebhook(t, d, dalHookBeta, &beta)

	if err := d.SetWebhookStatus("wh-unknown", WebhookStatusDisabled); err != nil {
		t.Fatalf("SetWebhookStatus on an unknown inlet: %v", err)
	}
	dalWantWebhook(t, d, "wh-unknown", nil)
}

func TestDeleteWebhookEndpoint(t *testing.T) {
	d := newAPITestDAL(t)
	alpha := dalWebhook(dalHookAlpha, "ann", "deploys", 100)
	beta := dalWebhook(dalHookBeta, "bob", "alerts", 200)
	for _, e := range []WebhookEndpoint{alpha, beta} {
		if err := d.PutWebhookEndpoint(e); err != nil {
			t.Fatalf("PutWebhookEndpoint(%q): %v", e.EndpointID, err)
		}
		if err := d.InsertWebhookRequestLog(e.Token, WebhookRequestLog{
			TS: 300, Outcome: "delivered", Headers: `{"h":"v"}`, Body: "payload",
		}); err != nil {
			t.Fatalf("InsertWebhookRequestLog(%q): %v", e.EndpointID, err)
		}
	}

	if err := d.DeleteWebhookEndpoint(dalHookAlpha); err != nil {
		t.Fatalf("DeleteWebhookEndpoint: %v", err)
	}
	dalWantWebhook(t, d, dalHookAlpha, nil)
	logs, err := d.ListWebhookRequestLogs(dalHookAlpha)
	if err != nil {
		t.Fatalf("ListWebhookRequestLogs: %v", err)
	}
	if logs != nil {
		t.Fatalf("the ring buffer goes with the inlet, got %+v", logs)
	}

	dalWantWebhook(t, d, dalHookBeta, &beta)
	kept, err := d.ListWebhookRequestLogs(dalHookBeta)
	if err != nil {
		t.Fatalf("ListWebhookRequestLogs(beta): %v", err)
	}
	wantKept := []WebhookRequestLog{{TS: 300, Outcome: "delivered", Headers: `{"h":"v"}`, Body: "payload"}}
	if !reflect.DeepEqual(kept, wantKept) {
		t.Fatalf("another inlet's ring buffer is untouched:\n got %+v\nwant %+v", kept, wantKept)
	}

	if err := d.DeleteWebhookEndpoint(dalHookAlpha); err != nil {
		t.Fatalf("DeleteWebhookEndpoint again: %v", err)
	}
}

func TestInsertWebhookRequestLog(t *testing.T) {
	d := newAPITestDAL(t)
	for i := 1; i <= 7; i++ {
		if err := d.InsertWebhookRequestLog(dalHookAlpha, WebhookRequestLog{
			TS: float64(i), Outcome: fmt.Sprintf("outcome-%d", i),
			Headers: fmt.Sprintf(`{"n":%d}`, i), Body: fmt.Sprintf("payload %d", i),
			Truncated: i%2 == 0,
		}); err != nil {
			t.Fatalf("InsertWebhookRequestLog(%d): %v", i, err)
		}
	}
	if err := d.InsertWebhookRequestLog(dalHookBeta, WebhookRequestLog{
		TS: 100, Outcome: "delivered", Headers: `{}`, Body: "other inlet",
	}); err != nil {
		t.Fatalf("InsertWebhookRequestLog(beta): %v", err)
	}

	got, err := d.ListWebhookRequestLogs(dalHookAlpha)
	if err != nil {
		t.Fatalf("ListWebhookRequestLogs: %v", err)
	}
	want := []WebhookRequestLog{
		{TS: 7, Outcome: "outcome-7", Headers: `{"n":7}`, Body: "payload 7", Truncated: false},
		{TS: 6, Outcome: "outcome-6", Headers: `{"n":6}`, Body: "payload 6", Truncated: true},
		{TS: 5, Outcome: "outcome-5", Headers: `{"n":5}`, Body: "payload 5", Truncated: false},
		{TS: 4, Outcome: "outcome-4", Headers: `{"n":4}`, Body: "payload 4", Truncated: true},
		{TS: 3, Outcome: "outcome-3", Headers: `{"n":3}`, Body: "payload 3", Truncated: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("only the newest five survive:\n got %+v\nwant %+v", got, want)
	}

	other, err := d.ListWebhookRequestLogs(dalHookBeta)
	if err != nil {
		t.Fatalf("ListWebhookRequestLogs(beta): %v", err)
	}
	wantOther := []WebhookRequestLog{{TS: 100, Outcome: "delivered", Headers: `{}`, Body: "other inlet"}}
	if !reflect.DeepEqual(other, wantOther) {
		t.Fatalf("the trim never reaches another inlet:\n got %+v\nwant %+v", other, wantOther)
	}
}

func TestListWebhookRequestLogs(t *testing.T) {
	d := newAPITestDAL(t)
	empty, err := d.ListWebhookRequestLogs(dalHookAlpha)
	if err != nil {
		t.Fatalf("ListWebhookRequestLogs before any request: %v", err)
	}
	if empty != nil {
		t.Fatalf("ListWebhookRequestLogs before any request: want none, got %+v", empty)
	}

	for _, l := range []WebhookRequestLog{
		{TS: 100, Outcome: "delivered", Headers: `{"n":1}`, Body: "first", Truncated: false},
		{TS: 100, Outcome: "dropped:sig_failed", Headers: `{"n":2}`, Body: "second", Truncated: true},
		{TS: 90, Outcome: "challenge", Headers: `{"n":3}`, Body: "third", Truncated: false},
	} {
		if err := d.InsertWebhookRequestLog(dalHookAlpha, l); err != nil {
			t.Fatalf("InsertWebhookRequestLog: %v", err)
		}
	}

	got, err := d.ListWebhookRequestLogs(dalHookAlpha)
	if err != nil {
		t.Fatalf("ListWebhookRequestLogs: %v", err)
	}
	want := []WebhookRequestLog{
		{TS: 90, Outcome: "challenge", Headers: `{"n":3}`, Body: "third", Truncated: false},
		{TS: 100, Outcome: "dropped:sig_failed", Headers: `{"n":2}`, Body: "second", Truncated: true},
		{TS: 100, Outcome: "delivered", Headers: `{"n":1}`, Body: "first", Truncated: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the ring buffer comes back in insert order, newest first:\n got %+v\nwant %+v", got, want)
	}
}

func TestScanScheduledMessage(t *testing.T) {
	d := newAPITestDAL(t)
	full := dalSchedule("sm-1", "ann", 100)
	if err := d.PutScheduledMessage(full); err != nil {
		t.Fatalf("PutScheduledMessage: %v", err)
	}
	daily := ScheduledMessage{
		ID: "sm-2", MemberID: "bob", Label: "standup", Body: "morning",
		Cadence: ScheduledMessageCadenceDaily, Hour: 8, Timezone: "UTC",
		Status: ScheduledMessageStatusDisabled, CreatedTS: 200,
	}
	if err := d.PutScheduledMessage(daily); err != nil {
		t.Fatalf("PutScheduledMessage(daily): %v", err)
	}

	query := `SELECT ` + scheduledMessageColumns + ` FROM scheduled_message WHERE id = ?`
	got, err := scanScheduledMessage(d.rdb.QueryRow(query, "sm-1"))
	if err != nil {
		t.Fatalf("scanScheduledMessage(sm-1): %v", err)
	}
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("scanScheduledMessage(sm-1):\n got %+v\nwant %+v", got, full)
	}

	got, err = scanScheduledMessage(d.rdb.QueryRow(query, "sm-2"))
	if err != nil {
		t.Fatalf("scanScheduledMessage(sm-2): %v", err)
	}
	if !reflect.DeepEqual(got, daily) {
		t.Fatalf("a cadence with no explicit sets reads them back as nil:\n got %+v\nwant %+v", got, daily)
	}

	if _, err := d.wdb.Exec(
		`UPDATE scheduled_message SET custom_minutes = ? WHERE id = ?`, "0, x ,40", "sm-1"); err != nil {
		t.Fatalf("hand-edit a stored set: %v", err)
	}
	got, err = scanScheduledMessage(d.rdb.QueryRow(query, "sm-1"))
	if err != nil {
		t.Fatalf("scanScheduledMessage(hand-edited): %v", err)
	}
	if want := []int{0, 40}; !reflect.DeepEqual(got.CustomMinutes, want) {
		t.Fatalf("a hand-edited entry is dropped rather than failing the read: want %v, got %v",
			want, got.CustomMinutes)
	}

	if _, err := scanScheduledMessage(d.rdb.QueryRow(query, "sm-ghost")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("scanScheduledMessage(ghost): want sql.ErrNoRows, got %v", err)
	}
}

func TestCanonicalIntSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		vals []int
		want string
	}{
		{"sorted, deduplicated and comma-joined", []int{40, 0, 20, 0}, "0,20,40"},
		{"one value", []int{7}, "7"},
		{"negatives sort ascending too", []int{3, -1, 2}, "-1,2,3"},
		{"the empty set", []int{}, ""},
		{"no set at all", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalIntSet(tc.vals); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}

	vals := []int{40, 0, 20}
	canonicalIntSet(vals)
	if want := []int{40, 0, 20}; !reflect.DeepEqual(vals, want) {
		t.Fatalf("the input is not mutated: want %v, got %v", want, vals)
	}
}

func TestSortedIntSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		vals []int
		want []int
	}{
		{"sorted ascending with duplicates collapsed", []int{40, 0, 20, 0, 40}, []int{0, 20, 40}},
		{"already sorted", []int{1, 2, 3}, []int{1, 2, 3}},
		{"the empty set", []int{}, nil},
		{"no set at all", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sortedIntSet(tc.vals); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}

	vals := []int{40, 0, 20, 0}
	sortedIntSet(vals)
	if want := []int{40, 0, 20, 0}; !reflect.DeepEqual(vals, want) {
		t.Fatalf("the input is not mutated: want %v, got %v", want, vals)
	}
}

func TestParseIntSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    string
		want []int
	}{
		{"a canonical column value", "0,20,40", []int{0, 20, 40}},
		{"the order and duplicates a hand-edit left behind", "40,0,40", []int{40, 0, 40}},
		{"an entry that is not a decimal integer is dropped", "0, x ,40", []int{0, 40}},
		{"nothing but junk", "x,y", nil},
		{"the empty set", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseIntSet(tc.s); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestGetScheduledMessage(t *testing.T) {
	d := newAPITestDAL(t)
	schedule := dalSchedule("sm-1", "ann", 100)
	if err := d.PutScheduledMessage(schedule); err != nil {
		t.Fatalf("PutScheduledMessage: %v", err)
	}
	dalWantSchedule(t, d, "sm-1", &schedule)
	dalWantSchedule(t, d, "sm-ghost", nil)
}

func TestListScheduledMessagesByMember(t *testing.T) {
	d := newAPITestDAL(t)
	empty, err := d.ListScheduledMessagesByMember("ann")
	if err != nil {
		t.Fatalf("ListScheduledMessagesByMember before any schedule: %v", err)
	}
	if empty != nil {
		t.Fatalf("ListScheduledMessagesByMember before any schedule: want none, got %+v", empty)
	}

	late := dalSchedule("sm-late", "ann", 300)
	tieB := dalSchedule("sm-b", "ann", 200)
	tieA := dalSchedule("sm-a", "ann", 200)
	tieA.Status = ScheduledMessageStatusDisabled
	other := dalSchedule("sm-other", "bob", 100)
	for _, m := range []ScheduledMessage{late, tieB, tieA, other} {
		if err := d.PutScheduledMessage(m); err != nil {
			t.Fatalf("PutScheduledMessage(%q): %v", m.ID, err)
		}
	}

	got, err := d.ListScheduledMessagesByMember("ann")
	if err != nil {
		t.Fatalf("ListScheduledMessagesByMember: %v", err)
	}
	want := []ScheduledMessage{tieA, tieB, late}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("one member's own schedules, oldest first, disabled included:\n got %+v\nwant %+v", got, want)
	}
}

func TestListAllEnabledScheduledMessages(t *testing.T) {
	d := newAPITestDAL(t)
	empty, err := d.ListAllEnabledScheduledMessages()
	if err != nil {
		t.Fatalf("ListAllEnabledScheduledMessages before any schedule: %v", err)
	}
	if empty != nil {
		t.Fatalf("ListAllEnabledScheduledMessages before any schedule: want none, got %+v", empty)
	}

	late := dalSchedule("sm-late", "bob", 300)
	tieB := dalSchedule("sm-b", "ann", 200)
	tieA := dalSchedule("sm-a", "ann", 200)
	suspended := dalSchedule("sm-off", "ann", 100)
	suspended.Status = ScheduledMessageStatusDisabled
	for _, m := range []ScheduledMessage{late, tieB, tieA, suspended} {
		if err := d.PutScheduledMessage(m); err != nil {
			t.Fatalf("PutScheduledMessage(%q): %v", m.ID, err)
		}
	}

	got, err := d.ListAllEnabledScheduledMessages()
	if err != nil {
		t.Fatalf("ListAllEnabledScheduledMessages: %v", err)
	}
	want := []ScheduledMessage{tieA, tieB, late}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("every armed schedule across all members, suspended ones filtered out:\n got %+v\nwant %+v", got, want)
	}
}

func TestPutScheduledMessage(t *testing.T) {
	d := newAPITestDAL(t)
	created := dalSchedule("sm-1", "ann", 100)
	created.CustomMinutes = []int{40, 0, 20, 0}
	if err := d.PutScheduledMessage(created); err != nil {
		t.Fatalf("PutScheduledMessage: %v", err)
	}
	stored := created
	stored.CustomMinutes = []int{0, 20, 40}
	dalWantSchedule(t, d, "sm-1", &stored)

	edited := dalSchedule("sm-1", "bob", 150)
	edited.Label = "edited label"
	edited.Cadence = ScheduledMessageCadenceWeekly
	edited.CustomMonths, edited.CustomDays, edited.CustomHours, edited.CustomMinutes = nil, nil, nil, nil
	edited.Status = ScheduledMessageStatusDisabled
	edited.LastFiredSlot = "2026-09-01T09:00+08:00"
	edited.LastFiredTS = 900
	if err := d.PutScheduledMessage(edited); err != nil {
		t.Fatalf("PutScheduledMessage(edit): %v", err)
	}
	dalWantSchedule(t, d, "sm-1", &edited)

	one, err := d.ListScheduledMessagesByMember("bob")
	if err != nil {
		t.Fatalf("ListScheduledMessagesByMember: %v", err)
	}
	if len(one) != 1 {
		t.Fatalf("the same id keeps one row, got %+v", one)
	}
}

func TestUpdateScheduledMessageSettings(t *testing.T) {
	d := newAPITestDAL(t)
	stored := dalSchedule("sm-1", "ann", 100)
	untouched := dalSchedule("sm-2", "ann", 200)
	for _, m := range []ScheduledMessage{stored, untouched} {
		if err := d.PutScheduledMessage(m); err != nil {
			t.Fatalf("PutScheduledMessage(%q): %v", m.ID, err)
		}
	}

	patch := dalSchedule("sm-1", "carl", 999)
	patch.Label = "edited label"
	patch.Body = "edited body"
	patch.Cadence = ScheduledMessageCadenceMonthly
	patch.DayOfWeek = 0
	patch.DayOfMonth = 28
	patch.Hour = 18
	patch.Minute = 45
	patch.CustomMonths, patch.CustomDays, patch.CustomHours, patch.CustomMinutes = nil, nil, nil, nil
	patch.Timezone = "UTC"
	patch.Status = ScheduledMessageStatusDisabled
	patch.LastFiredSlot = "1999-01-01T00:00+00:00"
	patch.LastFiredTS = 1
	if err := d.UpdateScheduledMessageSettings(patch); err != nil {
		t.Fatalf("UpdateScheduledMessageSettings: %v", err)
	}

	want := patch
	want.MemberID = stored.MemberID
	want.CreatedTS = stored.CreatedTS
	want.LastFiredSlot = stored.LastFiredSlot
	want.LastFiredTS = stored.LastFiredTS
	dalWantSchedule(t, d, "sm-1", &want)
	dalWantSchedule(t, d, "sm-2", &untouched)

	ghost := dalSchedule("sm-ghost", "ann", 300)
	if err := d.UpdateScheduledMessageSettings(ghost); err != nil {
		t.Fatalf("UpdateScheduledMessageSettings on a missing row: %v", err)
	}
	dalWantSchedule(t, d, "sm-ghost", nil)
}

func TestAimScheduledMessageCursor(t *testing.T) {
	d := newAPITestDAL(t)
	aimed := dalSchedule("sm-1", "ann", 100)
	untouched := dalSchedule("sm-2", "ann", 200)
	for _, m := range []ScheduledMessage{aimed, untouched} {
		if err := d.PutScheduledMessage(m); err != nil {
			t.Fatalf("PutScheduledMessage(%q): %v", m.ID, err)
		}
	}

	if err := d.AimScheduledMessageCursor("sm-1", "2026-09-07T18:45+08:00"); err != nil {
		t.Fatalf("AimScheduledMessageCursor: %v", err)
	}
	aimed.LastFiredSlot = "2026-09-07T18:45+08:00"
	dalWantSchedule(t, d, "sm-1", &aimed)
	dalWantSchedule(t, d, "sm-2", &untouched)

	if err := d.AimScheduledMessageCursor("sm-ghost", "2026-09-07T18:45+08:00"); err != nil {
		t.Fatalf("AimScheduledMessageCursor on a missing row: %v", err)
	}
	dalWantSchedule(t, d, "sm-ghost", nil)
}

func TestMarkScheduledMessageFired(t *testing.T) {
	d := newAPITestDAL(t)
	fired := dalSchedule("sm-1", "ann", 100)
	untouched := dalSchedule("sm-2", "ann", 200)
	for _, m := range []ScheduledMessage{fired, untouched} {
		if err := d.PutScheduledMessage(m); err != nil {
			t.Fatalf("PutScheduledMessage(%q): %v", m.ID, err)
		}
	}

	if err := d.MarkScheduledMessageFired("sm-1", "2026-09-07T18:45+08:00", 1800000000); err != nil {
		t.Fatalf("MarkScheduledMessageFired: %v", err)
	}
	fired.LastFiredSlot = "2026-09-07T18:45+08:00"
	fired.LastFiredTS = 1800000000
	dalWantSchedule(t, d, "sm-1", &fired)
	dalWantSchedule(t, d, "sm-2", &untouched)

	if err := d.MarkScheduledMessageFired("sm-ghost", "2026-09-07T18:45+08:00", 1800000000); err != nil {
		t.Fatalf("MarkScheduledMessageFired on a missing row: %v", err)
	}
	dalWantSchedule(t, d, "sm-ghost", nil)
}

func TestDeleteScheduledMessage(t *testing.T) {
	d := newAPITestDAL(t)
	doomed := dalSchedule("sm-1", "ann", 100)
	kept := dalSchedule("sm-2", "ann", 200)
	for _, m := range []ScheduledMessage{doomed, kept} {
		if err := d.PutScheduledMessage(m); err != nil {
			t.Fatalf("PutScheduledMessage(%q): %v", m.ID, err)
		}
	}

	if err := d.DeleteScheduledMessage("sm-1"); err != nil {
		t.Fatalf("DeleteScheduledMessage: %v", err)
	}
	dalWantSchedule(t, d, "sm-1", nil)
	dalWantSchedule(t, d, "sm-2", &kept)

	if err := d.DeleteScheduledMessage("sm-1"); err != nil {
		t.Fatalf("DeleteScheduledMessage again: %v", err)
	}
	if err := d.DeleteScheduledMessage("sm-ghost"); err != nil {
		t.Fatalf("DeleteScheduledMessage on a schedule nobody created: %v", err)
	}
	dalWantSchedule(t, d, "sm-2", &kept)
}

func TestGetSetting(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.GetSetting("org_name")
	if err != nil {
		t.Fatalf("GetSetting before any write: %v", err)
	}
	if got != nil {
		t.Fatalf("a key nobody has written reads back as nil, got %q", *got)
	}

	for key, value := range map[string]string{"org_name": "OffiCraft", "owner_name": ""} {
		if err := d.PutSetting(key, value); err != nil {
			t.Fatalf("PutSetting(%q): %v", key, err)
		}
	}

	for _, tc := range []struct{ key, want string }{
		{"org_name", "OffiCraft"},
		{"owner_name", ""},
	} {
		got, err := d.GetSetting(tc.key)
		if err != nil {
			t.Fatalf("GetSetting(%q): %v", tc.key, err)
		}
		if got == nil || *got != tc.want {
			t.Fatalf("GetSetting(%q): want %q, got %v", tc.key, tc.want, got)
		}
	}
}

func TestPutSetting(t *testing.T) {
	d := newAPITestDAL(t)
	if err := d.PutSetting("org_name", "First name"); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	if err := d.PutSetting("owner_name", "Eva"); err != nil {
		t.Fatalf("PutSetting(owner_name): %v", err)
	}
	if err := d.PutSetting("org_name", "Second name"); err != nil {
		t.Fatalf("PutSetting(rewrite): %v", err)
	}

	got, err := d.GetSetting("org_name")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if got == nil || *got != "Second name" {
		t.Fatalf("the same key keeps one value: want %q, got %v", "Second name", got)
	}
	other, err := d.GetSetting("owner_name")
	if err != nil {
		t.Fatalf("GetSetting(owner_name): %v", err)
	}
	if other == nil || *other != "Eva" {
		t.Fatalf("another key is untouched: want %q, got %v", "Eva", other)
	}

	var rows int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM setting WHERE key = ?`, "org_name").Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("the upsert never duplicates a key: want 1 row, got %d", rows)
	}
	var updatedAt float64
	if err := d.rdb.QueryRow(`SELECT updated_at FROM setting WHERE key = ?`, "org_name").Scan(&updatedAt); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	if updatedAt <= 0 {
		t.Fatalf("the write is stamped, got %v", updatedAt)
	}
}

func TestPutPushSubscription(t *testing.T) {
	d := newAPITestDAL(t)
	expires := 1800000000.0
	first := PushSubscription{
		Endpoint: "https://push.example/one", P256dh: dalPushP256dh,
		Auth: dalPushEnvelope, ExpirationTime: &expires,
	}
	if err := d.PutPushSubscription(first); err != nil {
		t.Fatalf("PutPushSubscription: %v", err)
	}
	second := PushSubscription{
		Endpoint: "https://push.example/two", P256dh: dalPushP256dh, Auth: dalPushEnvelope,
	}
	if err := d.PutPushSubscription(second); err != nil {
		t.Fatalf("PutPushSubscription(second): %v", err)
	}
	dalWantPushSubscriptions(t, d, "both browsers are delivery targets",
		[]PushSubscription{first, second})

	refreshed := PushSubscription{
		Endpoint: "https://push.example/one", P256dh: "renewed-public-material",
		Auth: "renewed-envelope-material",
	}
	if err := d.PutPushSubscription(refreshed); err != nil {
		t.Fatalf("PutPushSubscription(refresh): %v", err)
	}
	dalWantPushSubscriptions(t, d, "re-subscribing replaces the material rather than adding a row",
		[]PushSubscription{refreshed, second})
}

func TestListPushSubscriptions(t *testing.T) {
	d := newAPITestDAL(t)
	dalWantPushSubscriptions(t, d, "no browser has subscribed", nil)

	expires := 1800000000.0
	withExpiry := PushSubscription{
		Endpoint: "https://push.example/one", P256dh: dalPushP256dh,
		Auth: dalPushEnvelope, ExpirationTime: &expires,
	}
	without := PushSubscription{
		Endpoint: "https://push.example/two", P256dh: dalPushP256dh, Auth: dalPushEnvelope,
	}
	for _, s := range []PushSubscription{withExpiry, without} {
		if err := d.PutPushSubscription(s); err != nil {
			t.Fatalf("PutPushSubscription(%q): %v", s.Endpoint, err)
		}
	}
	dalWantPushSubscriptions(t, d, "a subscription with no expiry reads it back as nil",
		[]PushSubscription{withExpiry, without})
}

func TestDeletePushSubscription(t *testing.T) {
	d := newAPITestDAL(t)
	doomed := PushSubscription{
		Endpoint: "https://push.example/one", P256dh: dalPushP256dh, Auth: dalPushEnvelope,
	}
	kept := PushSubscription{
		Endpoint: "https://push.example/two", P256dh: dalPushP256dh, Auth: dalPushEnvelope,
	}
	for _, s := range []PushSubscription{doomed, kept} {
		if err := d.PutPushSubscription(s); err != nil {
			t.Fatalf("PutPushSubscription(%q): %v", s.Endpoint, err)
		}
	}

	if err := d.DeletePushSubscription("https://push.example/one"); err != nil {
		t.Fatalf("DeletePushSubscription: %v", err)
	}
	dalWantPushSubscriptions(t, d, "only the named target goes", []PushSubscription{kept})

	if err := d.DeletePushSubscription("https://push.example/one"); err != nil {
		t.Fatalf("DeletePushSubscription again: %v", err)
	}
	if err := d.DeletePushSubscription("https://push.example/never"); err != nil {
		t.Fatalf("DeletePushSubscription on an endpoint nobody registered: %v", err)
	}
	dalWantPushSubscriptions(t, d, "the retries change nothing", []PushSubscription{kept})
}

func TestPutWardenCommand(t *testing.T) {
	d := newAPITestDAL(t)
	first := WardenCommand{
		WardenID: "mac-1", Verb: "update", MemberID: "ann",
		Frame: []byte(`{"verb":"update"}`), EnqueuedTS: 100,
	}
	if err := d.PutWardenCommand(first); err != nil {
		t.Fatalf("PutWardenCommand: %v", err)
	}

	requeued := first
	requeued.Frame = []byte(`{"verb":"update","again":true}`)
	requeued.EnqueuedTS = 900
	if err := d.PutWardenCommand(requeued); err != nil {
		t.Fatalf("PutWardenCommand(re-enqueue): %v", err)
	}

	otherTarget := WardenCommand{
		WardenID: "mac-1", Verb: "update", MemberID: "bob",
		Frame: []byte(`{"verb":"update"}`), EnqueuedTS: 200,
	}
	if err := d.PutWardenCommand(otherTarget); err != nil {
		t.Fatalf("PutWardenCommand(other target): %v", err)
	}

	got, err := d.ListWardenCommands()
	if err != nil {
		t.Fatalf("ListWardenCommands: %v", err)
	}
	want := []WardenCommand{first, otherTarget}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the same order neither duplicates nor refreshes its stamp:\n got %+v\nwant %+v", got, want)
	}
}

func TestDeleteWardenCommand(t *testing.T) {
	d := newAPITestDAL(t)
	doomed := WardenCommand{WardenID: "mac-1", Verb: "update", MemberID: "ann",
		Frame: []byte(`{"verb":"update"}`), EnqueuedTS: 100}
	sameWarden := WardenCommand{WardenID: "mac-1", Verb: "update", MemberID: "bob",
		Frame: []byte(`{"verb":"update"}`), EnqueuedTS: 200}
	otherWarden := WardenCommand{WardenID: "mac-2", Verb: "update", MemberID: "ann",
		Frame: []byte(`{"verb":"update"}`), EnqueuedTS: 300}
	for _, c := range []WardenCommand{doomed, sameWarden, otherWarden} {
		if err := d.PutWardenCommand(c); err != nil {
			t.Fatalf("PutWardenCommand: %v", err)
		}
	}

	if err := d.DeleteWardenCommand("mac-1", "update", "ann"); err != nil {
		t.Fatalf("DeleteWardenCommand: %v", err)
	}
	got, err := d.ListWardenCommands()
	if err != nil {
		t.Fatalf("ListWardenCommands: %v", err)
	}
	want := []WardenCommand{sameWarden, otherWarden}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("only the addressed order is forgotten:\n got %+v\nwant %+v", got, want)
	}

	if err := d.DeleteWardenCommand("mac-1", "update", "ann"); err != nil {
		t.Fatalf("DeleteWardenCommand again: %v", err)
	}
	got, err = d.ListWardenCommands()
	if err != nil {
		t.Fatalf("ListWardenCommands: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("forgetting an order nobody enqueued changes nothing:\n got %+v\nwant %+v", got, want)
	}
}

func TestListWardenCommands(t *testing.T) {
	d := newAPITestDAL(t)
	empty, err := d.ListWardenCommands()
	if err != nil {
		t.Fatalf("ListWardenCommands on an empty queue: %v", err)
	}
	if empty != nil {
		t.Fatalf("ListWardenCommands on an empty queue: want none, got %+v", empty)
	}

	late := WardenCommand{WardenID: "mac-2", Verb: "update", MemberID: "carl",
		Frame: []byte(`{"n":3}`), EnqueuedTS: 300}
	tieFirst := WardenCommand{WardenID: "mac-1", Verb: "update", MemberID: "ann",
		Frame: []byte(`{"n":1}`), EnqueuedTS: 100}
	tieSecond := WardenCommand{WardenID: "mac-1", Verb: "update", MemberID: "bob",
		Frame: []byte(`{"n":2}`), EnqueuedTS: 100}
	for _, c := range []WardenCommand{late, tieFirst, tieSecond} {
		if err := d.PutWardenCommand(c); err != nil {
			t.Fatalf("PutWardenCommand: %v", err)
		}
	}

	got, err := d.ListWardenCommands()
	if err != nil {
		t.Fatalf("ListWardenCommands: %v", err)
	}
	want := []WardenCommand{tieFirst, tieSecond, late}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("enqueue order, equal stamps in insert order:\n got %+v\nwant %+v", got, want)
	}
}

func TestDeleteWardenCommandsBefore(t *testing.T) {
	d := newAPITestDAL(t)
	old := WardenCommand{WardenID: "mac-1", Verb: "update", MemberID: "ann",
		Frame: []byte(`{"n":1}`), EnqueuedTS: 100}
	atCutoff := WardenCommand{WardenID: "mac-1", Verb: "update", MemberID: "bob",
		Frame: []byte(`{"n":2}`), EnqueuedTS: 200}
	fresh := WardenCommand{WardenID: "mac-2", Verb: "update", MemberID: "carl",
		Frame: []byte(`{"n":3}`), EnqueuedTS: 300}
	for _, c := range []WardenCommand{old, atCutoff, fresh} {
		if err := d.PutWardenCommand(c); err != nil {
			t.Fatalf("PutWardenCommand: %v", err)
		}
	}

	n, err := d.DeleteWardenCommandsBefore(200)
	if err != nil {
		t.Fatalf("DeleteWardenCommandsBefore: %v", err)
	}
	if n != 1 {
		t.Fatalf("strictly before the cutoff: want 1 removed, got %d", n)
	}
	got, err := d.ListWardenCommands()
	if err != nil {
		t.Fatalf("ListWardenCommands: %v", err)
	}
	want := []WardenCommand{atCutoff, fresh}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the order stamped exactly at the cutoff survives:\n got %+v\nwant %+v", got, want)
	}

	n, err = d.DeleteWardenCommandsBefore(200)
	if err != nil {
		t.Fatalf("DeleteWardenCommandsBefore again: %v", err)
	}
	if n != 0 {
		t.Fatalf("a sweep with nothing to collect: want 0, got %d", n)
	}

	n, err = d.DeleteWardenCommandsBefore(1000)
	if err != nil {
		t.Fatalf("DeleteWardenCommandsBefore(everything): %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 removed, got %d", n)
	}
	got, err = d.ListWardenCommands()
	if err != nil {
		t.Fatalf("ListWardenCommands: %v", err)
	}
	if got != nil {
		t.Fatalf("the queue is empty, got %+v", got)
	}
}

func TestDeleteSetting(t *testing.T) {
	d := newAPITestDAL(t)
	for key, value := range map[string]string{"org_name": "OffiCraft", "owner_name": "Eva"} {
		if err := d.PutSetting(key, value); err != nil {
			t.Fatalf("PutSetting(%q): %v", key, err)
		}
	}

	if err := d.DeleteSetting("org_name"); err != nil {
		t.Fatalf("DeleteSetting: %v", err)
	}
	gone, err := d.GetSetting("org_name")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if gone != nil {
		t.Fatalf("the deleted key reads back as nil, got %q", *gone)
	}
	kept, err := d.GetSetting("owner_name")
	if err != nil {
		t.Fatalf("GetSetting(owner_name): %v", err)
	}
	if kept == nil || *kept != "Eva" {
		t.Fatalf("another key is untouched: want %q, got %v", "Eva", kept)
	}

	if err := d.DeleteSetting("org_name"); err != nil {
		t.Fatalf("DeleteSetting again: %v", err)
	}
	if err := d.DeleteSetting("never_written"); err != nil {
		t.Fatalf("DeleteSetting on a key nobody wrote: %v", err)
	}
}

func TestDisplayNames(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.displayNames(`SELECT account, display_name FROM account_alias`)
	if err != nil {
		t.Fatalf("displayNames over an empty table: %v", err)
	}
	if !reflect.DeepEqual(got, map[string]string{}) {
		t.Fatalf("displayNames over an empty table: want an empty map, got %v", got)
	}

	if err := d.PutAccountAlias(AccountAlias{Account: "acct-a", DisplayName: "Studio account"}); err != nil {
		t.Fatalf("PutAccountAlias: %v", err)
	}
	if err := d.PutMachineAlias(MachineAlias{MachineID: "mac-1", DisplayName: "Studio Mac"}); err != nil {
		t.Fatalf("PutMachineAlias: %v", err)
	}

	got, err = d.displayNames(`
		SELECT account, display_name FROM account_alias
		UNION ALL
		SELECT machine_id, display_name FROM machine_alias`)
	if err != nil {
		t.Fatalf("displayNames: %v", err)
	}
	want := map[string]string{"acct-a": "Studio account", "mac-1": "Studio Mac"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("every row of the query folds into the map: want %v, got %v", want, got)
	}

	if _, err := d.displayNames(`SELECT nope FROM nowhere`); err == nil {
		t.Fatalf("displayNames over an impossible query: want an error, got none")
	}
}

// dalTestMember is a roster row with every column carrying a distinct
// non-zero value, so a write that touches a column it should not shows up.

func dalTestMember(id, name string) Member {
	ok := true
	return Member{
		ID:                id,
		Name:              name,
		Kind:              KindStaff,
		RoleKey:           "engineer",
		Runtime:           "claude",
		Model:             "sonnet",
		ActualModel:       "sonnet-4",
		Effort:            "medium",
		ActualRuntime:     "codex",
		ActualEffort:      "high",
		DesiredState:      "online",
		DesiredMachineID:  "mac-1",
		LastMachineID:     "mac-0",
		SessionBootTS:     1700000001,
		WakingSince:       1700000002,
		StoppingSince:     1700000003,
		StoppedSince:      1700000004,
		RefocusSince:      1700000005,
		RefocusOp:         "refocus",
		ForcedStopAt:      1700000006,
		RestartAfterStop:  true,
		HandoverNoticedTS: 1700000007,
		AgentIatFloor:     1700000008,
		TokenKeyID:        "ring-1",
		BankedCost:        12.5,
		LastOp:            "stop",
		LastOpOK:          &ok,
		LastOpLog:         "log line",
		LastOpReason:      "code: detail",
		LastOpAt:          1700000009,
		RosterStatus:      RosterStatusActive,
		CreatedTS:         1700000000,
		ReleasedTS:        1700000010,
		ActivatedTS:       1700000011,
	}
}

// dalPutMember stores m and answers with it, so a test can name the row it
// seeded and the row it expects to read back in one literal.

func dalPutMember(t *testing.T, d *DAL, m Member) Member {
	t.Helper()
	if err := d.PutMember(m); err != nil {
		t.Fatalf("PutMember(%q): %v", m.ID, err)
	}
	return m
}

// dalWantMember asserts the stored roster row reads back as want, whole.

func dalWantMember(t *testing.T, d *DAL, want Member) {
	t.Helper()
	got, err := d.GetMember(want.ID)
	if err != nil {
		t.Fatalf("GetMember(%q): %v", want.ID, err)
	}
	if got == nil {
		t.Fatalf("GetMember(%q): no row", want.ID)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("GetMember(%q):\n got %+v\nwant %+v", want.ID, *got, want)
	}
}

// dalChat is one stream message with an empty meta object — the shape the
// store reads back for a message that carries no meta.

func dalChat(id, sender, recipient string, ts float64) ChatMessage {
	return ChatMessage{
		ID: id, Sender: sender, Recipient: recipient,
		Body: "body of " + id, TS: ts, Meta: map[string]any{},
	}
}

func dalPutChats(t *testing.T, d *DAL, msgs ...ChatMessage) {
	t.Helper()
	for _, m := range msgs {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("PutChat(%q): %v", m.ID, err)
		}
	}
}

// dalWantChats asserts a listing answered with exactly these messages, in
// this order.

func dalWantChats(t *testing.T, what string, got, want []ChatMessage) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s:\n got %+v\nwant %+v", what, got, want)
	}
}

// dalChatIDs names a listing's messages in the order they came back.

func dalChatIDs(msgs []ChatMessage) []string {
	out := []string{}
	for _, m := range msgs {
		out = append(out, m.ID)
	}
	return out
}

// dalStaticSnapshot is a documentHistoryStream snapshot that always serializes
// the same live state, whatever the transaction reads.

func dalStaticSnapshot(current string) func(sqlQuerier) (string, error) {
	return func(sqlQuerier) (string, error) { return current, nil }
}

// dalWantHistory asserts one history listing entry by entry, with each row's
// minted id and ingest stamp checked for plausibility rather than written down.

func dalWantHistory(t *testing.T, what string, got []DocumentHistory, want []DocumentHistory, since float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: want %d entries, got %+v", what, len(want), got)
	}
	for i := range got {
		if got[i].ID <= 0 {
			t.Fatalf("%s[%d]: want a minted id, got %d", what, i, got[i].ID)
		}
		if got[i].CreatedTS < since {
			t.Fatalf("%s[%d]: want a stamp no older than %v, got %v", what, i, since, got[i].CreatedTS)
		}
		if i > 0 && got[i].ID >= got[i-1].ID {
			t.Fatalf("%s: want newest first, got ids %d then %d", what, got[i-1].ID, got[i].ID)
		}
		entry := got[i]
		entry.ID = 0
		entry.CreatedTS = 0
		if !reflect.DeepEqual(entry, want[i]) {
			t.Fatalf("%s[%d]:\n got %+v\nwant %+v", what, i, entry, want[i])
		}
	}
}

// dalAttRef is one meta["attachments"] entry, in the shape the store reads it
// back as.

func dalAttRef(id, mime, filename string) map[string]any {
	return map[string]any{"id": id, "mime": mime, "filename": filename}
}

// dalChatWithAtts is a stream message whose meta names these blobs.

func dalChatWithAtts(id, sender, recipient string, ts float64, refs ...any) ChatMessage {
	m := dalChat(id, sender, recipient, ts)
	m.Meta = map[string]any{"attachments": refs}
	return m
}

// dalPutBlobs stores one blob per id, each carrying its own bytes.

func dalPutBlobs(t *testing.T, d *DAL, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := d.PutChatAttachment(ChatAttachment{
			ID: id, Mime: "image/png", Data: []byte("bytes of " + id),
		}); err != nil {
			t.Fatalf("PutChatAttachment(%q): %v", id, err)
		}
	}
}

// dalStoredBlobIDs names every blob still in the store, sorted.

func dalStoredBlobIDs(t *testing.T, d *DAL) []string {
	t.Helper()
	rows, err := d.rdb.Query(`SELECT id FROM chat_attachment ORDER BY id`)
	if err != nil {
		t.Fatalf("list blobs: %v", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan blob id: %v", err)
		}
		out = append(out, id)
	}
	return out
}

// dalSortedKeys names a ref set's members in a comparable order.

func dalSortedKeys(set map[string]bool) []string {
	out := []string{}
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func dalMustListChat(t *testing.T, d *DAL) []ChatMessage {
	t.Helper()
	got, err := d.ListChat()
	if err != nil {
		t.Fatalf("ListChat: %v", err)
	}
	return got
}

func dalWantUserContext(t *testing.T, d *DAL, want *UserContext) {
	t.Helper()
	got, err := d.GetUserContext()
	if err != nil {
		t.Fatalf("GetUserContext: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetUserContext:\n got %+v\nwant %+v", got, want)
	}
}

func dalWantRoleDef(t *testing.T, d *DAL, roleKey string, want *RoleDef) {
	t.Helper()
	got, err := d.GetRoleDef(roleKey)
	if err != nil {
		t.Fatalf("GetRoleDef(%q): %v", roleKey, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetRoleDef(%q):\n got %+v\nwant %+v", roleKey, got, want)
	}
}

func dalWantLessons(t *testing.T, d *DAL, roleKey string, want *Lessons) {
	t.Helper()
	got, err := d.GetLessons(roleKey)
	if err != nil {
		t.Fatalf("GetLessons(%q): %v", roleKey, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetLessons(%q):\n got %+v\nwant %+v", roleKey, got, want)
	}
}

func dalWantInsight(t *testing.T, d *DAL, roleKey string, want *Insight) {
	t.Helper()
	got, err := d.GetInsight(roleKey)
	if err != nil {
		t.Fatalf("GetInsight(%q): %v", roleKey, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetInsight(%q):\n got %+v\nwant %+v", roleKey, got, want)
	}
}

func dalWantBootDocument(t *testing.T, d *DAL, kind, key string, want *BootDocument) {
	t.Helper()
	got, err := d.GetBootDocument(kind, key)
	if err != nil {
		t.Fatalf("GetBootDocument(%q, %q): %v", kind, key, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetBootDocument(%q, %q):\n got %+v\nwant %+v", kind, key, got, want)
	}
}

// dalReplyCard is one answered card with every column carrying a distinct
// value, in the shape the store reads it back.

func dalReplyCard(id string, createdTS float64) ReplyCard {
	return ReplyCard{
		ID: id, FromMember: "ann", Kind: "decision",
		Summary: "summary of " + id, Body: "body of " + id,
		Options: []ReplyCardOption{
			{Text: "ship it", AIPick: true},
			{Text: "hold", AIPick: false},
		},
		SelectMode: "multi", Status: "answered",
		CreatedTS: createdTS, AnsweredTS: createdTS + 10, ExpiredTS: createdTS + 20,
		ChatMessageID:     "m-" + id,
		AnswerOptionIdxs:  []int{0, 1},
		AnswerText:        "ship it",
		AnswerAttachments: []any{dalAttRef("att-answer", "image/png", "a.png")},
		Attachments:       []any{dalAttRef("att-question", "image/png", "q.png")},
		TaskID:            "T-1", TaskStepID: "s-1",
	}
}

// dalWantReplyCard asserts one card reads back as want.

func dalWantReplyCard(t *testing.T, d *DAL, id string, want *ReplyCard) {
	t.Helper()
	got, err := d.GetReplyCard(id)
	if err != nil {
		t.Fatalf("GetReplyCard(%q): %v", id, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetReplyCard(%q):\n got %+v\nwant %+v", id, got, want)
	}
}

// The opaque inlet keys and the shared HMAC value the webhook cases address
// their endpoints by.
const (
	dalHookAlpha   = "wh-alpha"
	dalHookBeta    = "wh-beta"
	dalHookSigning = "shared-hmac-value"
)

// dalWebhook is one inlet with every column carrying a distinct value.

func dalWebhook(key, memberID, endpointID string, createdTS float64) WebhookEndpoint {
	return WebhookEndpoint{
		Token: key, MemberID: memberID, EndpointID: endpointID,
		Purpose: "purpose of " + endpointID, Status: WebhookStatusEnabled,
		CreatedTS: createdTS, Platform: WebhookPlatformSlack,
		SigningSecret:  dalHookSigning,
		LastReceivedTS: createdTS + 5, DeliveredCount: 3, DroppedCount: 2,
		LastDropReason: WebhookDropReasonSigFailed,
	}
}

// dalWantWebhook asserts one inlet reads back as want.

func dalWantWebhook(t *testing.T, d *DAL, key string, want *WebhookEndpoint) {
	t.Helper()
	got, err := d.GetWebhookByToken(key)
	if err != nil {
		t.Fatalf("GetWebhookByToken(%q): %v", key, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetWebhookByToken(%q):\n got %+v\nwant %+v", key, got, want)
	}
}

// dalSchedule is one custom-cadence schedule with every column carrying a
// distinct value, in the shape the store reads it back.

func dalSchedule(id, memberID string, createdTS float64) ScheduledMessage {
	return ScheduledMessage{
		ID: id, MemberID: memberID, Label: "label of " + id, Body: "body of " + id,
		Cadence: ScheduledMessageCadenceCustom, DayOfWeek: 3, DayOfMonth: 15,
		Hour: 9, Minute: 30,
		CustomMonths: []int{1, 6}, CustomDays: []int{1, 15},
		CustomHours: []int{9, 17}, CustomMinutes: []int{0, 20, 40},
		Timezone: "Asia/Taipei", Status: ScheduledMessageStatusEnabled,
		LastFiredSlot: "2026-08-10T09:00+08:00", LastFiredTS: createdTS + 5,
		CreatedTS: createdTS,
	}
}

// dalWantSchedule asserts one schedule reads back as want.

func dalWantSchedule(t *testing.T, d *DAL, id string, want *ScheduledMessage) {
	t.Helper()
	got, err := d.GetScheduledMessage(id)
	if err != nil {
		t.Fatalf("GetScheduledMessage(%q): %v", id, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetScheduledMessage(%q):\n got %+v\nwant %+v", id, got, want)
	}
}

// The browser-supplied encryption material the push cases subscribe with.
const (
	dalPushP256dh   = "browser-public-material"
	dalPushEnvelope = "browser-envelope-material"
)

// dalWantPushSubscriptions asserts the delivery targets, sorted by endpoint.

func dalWantPushSubscriptions(t *testing.T, d *DAL, what string, want []PushSubscription) {
	t.Helper()
	got, err := d.ListPushSubscriptions()
	if err != nil {
		t.Fatalf("ListPushSubscriptions: %v", err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Endpoint < got[j].Endpoint })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s:\n got %+v\nwant %+v", what, got, want)
	}
}
