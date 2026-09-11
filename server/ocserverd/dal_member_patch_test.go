package main

// dal_member_patch_test.go — the member table's one write door: which columns a
// named patch puts in the statement, which ones a whole-row writer is allowed
// to carry onto an existing row, and what each reaches SQL as.

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestMfLinkedTaskID(t *testing.T) {
	t.Run("a bound task is carried as its own string", func(t *testing.T) {
		id := "T-7"
		got := mfLinkedTaskID(&id)
		want := memberField{col: "linked_task_id", val: "T-7"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mfLinkedTaskID(&%q): got %+v, want %+v", id, got, want)
		}
	})

	t.Run("an unbound member is carried as nil, not as the empty string", func(t *testing.T) {
		got := mfLinkedTaskID(nil)
		want := memberField{col: "linked_task_id", val: nil}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mfLinkedTaskID(nil): got %+v, want %+v", got, want)
		}
	})

	t.Run("nil reaches the column as SQL NULL and reads back as an unbound member", func(t *testing.T) {
		d := newAPITestDAL(t)
		bound := "T-7"
		m := dalTestMember("ann", "Ann")
		m.LinkedTaskID = &bound
		dalPutMember(t, d, m)

		if err := d.PatchMember("ann", mfLinkedTaskID(nil)); err != nil {
			t.Fatalf("PatchMember: %v", err)
		}
		m.LinkedTaskID = nil
		dalWantMember(t, d, m)
		if got := dalMemberColumnIsNull(t, d, "ann", "linked_task_id"); !got {
			t.Fatalf("linked_task_id after unbinding: want SQL NULL, got a non-NULL value")
		}

		if err := d.PatchMember("ann", mfLinkedTaskID(&bound)); err != nil {
			t.Fatalf("PatchMember: %v", err)
		}
		m.LinkedTaskID = &bound
		dalWantMember(t, d, m)
		if got := dalMemberColumnIsNull(t, d, "ann", "linked_task_id"); got {
			t.Fatalf("linked_task_id after rebinding: want a stored id, got SQL NULL")
		}
	})
}

func TestMfCodename(t *testing.T) {
	t.Run("a codename is carried as its own string", func(t *testing.T) {
		got := mfCodename("O-7")
		want := memberField{col: "codename", val: "O-7"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mfCodename(O-7): got %+v, want %+v", got, want)
		}
	})

	t.Run("the empty codename is carried as nil, not as the empty string", func(t *testing.T) {
		got := mfCodename("")
		want := memberField{col: "codename", val: nil}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mfCodename(\"\"): got %+v, want %+v", got, want)
		}
	})

	t.Run("many codename-less rows coexist while two rows cannot share one codename", func(t *testing.T) {
		d := newAPITestDAL(t)
		for _, id := range []string{"ann", "bob", "carl"} {
			m := dalTestMember(id, id)
			m.Codename = ""
			dalPutMember(t, d, m)
			if !dalMemberColumnIsNull(t, d, id, "codename") {
				t.Fatalf("codename of %q: want SQL NULL, got a stored value", id)
			}
		}
		named := dalTestMember("ow-1", "Wren")
		named.Codename = "O-7"
		dalPutMember(t, d, named)

		clash := dalTestMember("ow-2", "Rook")
		clash.Codename = "O-7"
		err := d.PutMember(clash)
		if err == nil {
			t.Fatalf("a second row under the same codename: want a refusal, got nil")
		}
		if err.Error() != "constraint failed: UNIQUE constraint failed: member.codename (2067)" {
			t.Fatalf("a second row under the same codename: got %q", err.Error())
		}
		if got := dalMemberIDs(t, d); !reflect.DeepEqual(got, []string{"ann", "bob", "carl", "ow-1"}) {
			t.Fatalf("the roster after the refusal: got %v", got)
		}
	})
}

func TestMfLastOpOK(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *bool
		want any
	}{
		{"a reported success is carried as true", boolPtr(true), true},
		{"a reported failure is carried as false", boolPtr(false), false},
		{"no op reported yet is carried as nil, distinct from both", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mfLastOpOK(tc.in)
			want := memberField{col: "last_op_ok", val: tc.want, insertOnly: true}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("mfLastOpOK: got %+v, want %+v", got, want)
			}
		})
	}

	t.Run("the three values round-trip through the column as themselves", func(t *testing.T) {
		d := newAPITestDAL(t)
		m := dalTestMember("ann", "Ann")
		m.LastOpOK = boolPtr(true)
		dalPutMember(t, d, m)
		dalWantMember(t, d, m)

		for _, tc := range []struct {
			name     string
			in       *bool
			wantNull bool
		}{
			{"false", boolPtr(false), false},
			{"nil", nil, true},
			{"true", boolPtr(true), false},
		} {
			if err := d.PatchMember("ann", mfLastOpOK(tc.in)); err != nil {
				t.Fatalf("PatchMember(%s): %v", tc.name, err)
			}
			m.LastOpOK = tc.in
			dalWantMember(t, d, m)
			if got := dalMemberColumnIsNull(t, d, "ann", "last_op_ok"); got != tc.wantNull {
				t.Fatalf("last_op_ok is NULL after writing %s: want %v, got %v", tc.name, tc.wantNull, got)
			}
		}
	})
}

func TestMfAgentIatFloor(t *testing.T) {
	got := mfAgentIatFloor(1700000000)
	want := memberField{
		col: "agent_iat_floor", val: float64(1700000000), insertOnly: true, forwardOnly: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mfAgentIatFloor = %+v, want %+v", got, want)
	}
}

func TestMfTokenKeyID(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
	}{
		{name: "new signing key", key: "ring-new"},
		{name: "older signing key remains writable", key: "ring-old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mfTokenKeyID(tc.key)
			want := memberField{col: "token_key_id", val: tc.key, insertOnly: true}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("mfTokenKeyID(%q) = %+v, want %+v", tc.key, got, want)
			}
		})
	}
}

func TestMemberWholeRow(t *testing.T) {
	m := dalTestMember("ow-1", "Wren")
	m.Kind = KindOutsource
	m.Codename = "O-7"
	linked := "T-1"
	m.LinkedTaskID = &linked

	got := memberWholeRow(m)
	want := []memberField{
		{col: "id", val: "ow-1", insertOnly: true},
		{col: "name", val: "Wren"},
		{col: "kind", val: "outsource"},
		{col: "role_key", val: "engineer"},
		{col: "runtime", val: "claude", insertOnly: true},
		{col: "model", val: "sonnet", insertOnly: true},
		{col: "actual_model", val: "sonnet-4"},
		{col: "effort", val: "medium", insertOnly: true},
		{col: "actual_runtime", val: "codex"},
		{col: "actual_effort", val: "high"},
		{col: "desired_state", val: "online"},
		{col: "desired_machine_id", val: "mac-1", insertOnly: true},
		{col: "last_machine_id", val: "mac-0"},
		{col: "session_boot_ts", val: 1700000001.0},
		{col: "waking_since", val: 1700000002.0},
		{col: "stopping_since", val: 1700000003.0, insertOnly: true},
		{col: "stopped_since", val: 1700000004.0, insertOnly: true},
		{col: "refocus_since", val: 1700000005.0, insertOnly: true},
		{col: "refocus_op", val: "refocus", insertOnly: true},
		{col: "banked_cost", val: 12.5, insertOnly: true},
		{col: "last_op", val: "stop", insertOnly: true},
		{col: "last_op_ok", val: true, insertOnly: true},
		{col: "last_op_log", val: "log line", insertOnly: true},
		{col: "last_op_reason", val: "code: detail", insertOnly: true},
		{col: "last_op_at", val: 1700000009.0, insertOnly: true},
		{col: "roster_status", val: "active"},
		{col: "linked_task_id", val: "T-1"},
		{col: "codename", val: "O-7"},
		{col: "created_ts", val: 1700000000.0},
		{col: "released_ts", val: 1700000010.0},
		{col: "activated_ts", val: 1700000011.0},
		{col: "forced_stop_at", val: 1700000006.0, forwardOnly: true},
		{col: "handover_noticed_ts", val: 1700000007.0, insertOnly: true},
		{col: "agent_iat_floor", val: 1700000008.0, insertOnly: true, forwardOnly: true},
		{col: "restart_after_stop", val: true},
		{col: "token_key_id", val: "ring-1", insertOnly: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("memberWholeRow:\n got %+v\nwant %+v", got, want)
	}

	t.Run("the projected columns are exactly the ones the row is read back through", func(t *testing.T) {
		projected := []string{}
		for _, f := range got {
			projected = append(projected, f.col)
		}
		sort.Strings(projected)
		read := dalSplitColumns(memberColumns)
		sort.Strings(read)
		if !reflect.DeepEqual(projected, read) {
			t.Fatalf("the write projection and the read column list disagree:\n write %v\n read  %v", projected, read)
		}
	})
}

func TestUpdatableMemberFields(t *testing.T) {
	t.Run("the insert-only columns are dropped and the rest keep their order and values", func(t *testing.T) {
		m := dalTestMember("ow-1", "Wren")
		m.Kind = KindOutsource
		m.Codename = "O-7"
		linked := "T-1"
		m.LinkedTaskID = &linked

		got := updatableMemberFields(memberWholeRow(m))
		want := []memberField{
			{col: "name", val: "Wren"},
			{col: "kind", val: "outsource"},
			{col: "role_key", val: "engineer"},
			{col: "actual_model", val: "sonnet-4"},
			{col: "actual_runtime", val: "codex"},
			{col: "actual_effort", val: "high"},
			{col: "desired_state", val: "online"},
			{col: "last_machine_id", val: "mac-0"},
			{col: "session_boot_ts", val: 1700000001.0},
			{col: "waking_since", val: 1700000002.0},
			{col: "roster_status", val: "active"},
			{col: "linked_task_id", val: "T-1"},
			{col: "codename", val: "O-7"},
			{col: "created_ts", val: 1700000000.0},
			{col: "released_ts", val: 1700000010.0},
			{col: "activated_ts", val: 1700000011.0},
			{col: "forced_stop_at", val: 1700000006.0, forwardOnly: true},
			{col: "restart_after_stop", val: true},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("updatableMemberFields:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("an all-insert-only input keeps nothing, an all-updatable one keeps everything", func(t *testing.T) {
		got := updatableMemberFields([]memberField{mfID("ann"), mfBankedCost(1), mfLastOp("stop")})
		if !reflect.DeepEqual(got, []memberField{}) {
			t.Fatalf("updatableMemberFields over insert-only columns: want an empty slice, got %+v", got)
		}
		in := []memberField{mfName("Ann"), mfKind(KindStaff)}
		if got := updatableMemberFields(in); !reflect.DeepEqual(got, in) {
			t.Fatalf("updatableMemberFields over updatable columns:\n got %+v\nwant %+v", got, in)
		}
	})

	t.Run("nothing at all in is an empty slice out, not nil", func(t *testing.T) {
		got := updatableMemberFields(nil)
		if got == nil || len(got) != 0 {
			t.Fatalf("updatableMemberFields(nil): want an empty slice, got %#v", got)
		}
	})
}

func TestPatchMemberOn(t *testing.T) {
	t.Run("only the named columns move, and the neighbouring row is untouched", func(t *testing.T) {
		d := newAPITestDAL(t)
		ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
		bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

		if err := patchMemberOn(d.wdb, "ann", mfName("Annabel"), mfBankedCost(99)); err != nil {
			t.Fatalf("patchMemberOn: %v", err)
		}
		ann.Name = "Annabel"
		ann.BankedCost = 99
		dalWantMember(t, d, ann)
		dalWantMember(t, d, bob)
	})

	t.Run("a forward-only column drops a value older than the one stored and takes a newer one", func(t *testing.T) {
		d := newAPITestDAL(t)
		ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))

		if err := patchMemberOn(d.wdb, "ann", mfForcedStopAt(1)); err != nil {
			t.Fatalf("patchMemberOn with a stale stamp: %v", err)
		}
		dalWantMember(t, d, ann)
		if err := patchMemberOn(d.wdb, "ann", mfForcedStopAt(0)); err != nil {
			t.Fatalf("patchMemberOn with a zero stamp: %v", err)
		}
		dalWantMember(t, d, ann)

		if err := patchMemberOn(d.wdb, "ann", mfForcedStopAt(1800000000)); err != nil {
			t.Fatalf("patchMemberOn with a newer stamp: %v", err)
		}
		ann.ForcedStopAt = 1800000000
		dalWantMember(t, d, ann)
	})

	t.Run("an ordinary column takes whatever it is handed, backwards included", func(t *testing.T) {
		d := newAPITestDAL(t)
		ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
		if err := patchMemberOn(d.wdb, "ann", mfSessionBootTS(1)); err != nil {
			t.Fatalf("patchMemberOn: %v", err)
		}
		ann.SessionBootTS = 1
		dalWantMember(t, d, ann)
	})

	t.Run("an id naming no row is a clean no-op that creates nothing", func(t *testing.T) {
		d := newAPITestDAL(t)
		ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
		if err := patchMemberOn(d.wdb, "ghost", mfName("Ghost")); err != nil {
			t.Fatalf("patchMemberOn(ghost): %v", err)
		}
		if got := dalMemberIDs(t, d); !reflect.DeepEqual(got, []string{"ann"}) {
			t.Fatalf("the roster after patching an unknown id: want [ann], got %v", got)
		}
		dalWantMember(t, d, ann)
	})

	t.Run("naming no column at all writes nothing", func(t *testing.T) {
		d := newAPITestDAL(t)
		ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))
		if err := patchMemberOn(d.wdb, "ann"); err != nil {
			t.Fatalf("patchMemberOn with no fields: %v", err)
		}
		dalWantMember(t, d, ann)
	})

	t.Run("a patch inside a rolled-back transaction reaches the row not at all", func(t *testing.T) {
		d := newAPITestDAL(t)
		ann := dalPutMember(t, d, dalTestMember("ann", "Ann"))

		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if err := patchMemberOn(tx, "ann", mfName("Annabel")); err != nil {
			t.Fatalf("patchMemberOn on a transaction: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		dalWantMember(t, d, ann)

		tx, err = d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin again — the write pool is wedged: %v", err)
		}
		if err := patchMemberOn(tx, "ann", mfName("Annabel")); err != nil {
			t.Fatalf("patchMemberOn on the second transaction: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		ann.Name = "Annabel"
		dalWantMember(t, d, ann)
	})
}

func TestInsertMemberRowIfAbsent(t *testing.T) {
	t.Run("an absent id lands the whole row, insert-only columns included", func(t *testing.T) {
		d := newAPITestDAL(t)
		m := dalTestMember("ann", "Ann")
		m.Codename = "O-7"
		linked := "T-1"
		m.LinkedTaskID = &linked
		if err := insertMemberRowIfAbsent(d.wdb, memberWholeRow(m)); err != nil {
			t.Fatalf("insertMemberRowIfAbsent: %v", err)
		}
		dalWantMember(t, d, m)
	})

	t.Run("an id that already exists is left exactly as it stands", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutMember(t, d, dalTestMember("ann", "Ann"))
		bob := dalPutMember(t, d, dalTestMember("bob", "Bob"))

		incoming := dalTestMember("ann", "Someone Else")
		incoming.RoleKey = "auditor"
		incoming.BankedCost = 0
		incoming.RosterStatus = RosterStatusRemoved
		if err := insertMemberRowIfAbsent(d.wdb, memberWholeRow(incoming)); err != nil {
			t.Fatalf("insertMemberRowIfAbsent onto an existing id: %v", err)
		}
		dalWantMember(t, d, stored)
		dalWantMember(t, d, bob)
		if got := dalMemberIDs(t, d); !reflect.DeepEqual(got, []string{"ann", "bob"}) {
			t.Fatalf("the roster after the no-op insert: want [ann bob], got %v", got)
		}
	})
}

func TestSetMemberAgentIatFloor(t *testing.T) {
	d := newAPITestDAL(t)
	m := dalPutMember(t, d, dalTestMember("ann", "Ann"))

	if err := d.SetMemberAgentIatFloor("ann", 1800000000); err != nil {
		t.Fatalf("SetMemberAgentIatFloor(newer): %v", err)
	}
	m.AgentIatFloor = 1800000000
	dalWantMember(t, d, m)

	if err := d.SetMemberAgentIatFloor("ann", 1700000000); err != nil {
		t.Fatalf("SetMemberAgentIatFloor(older): %v", err)
	}
	dalWantMember(t, d, m)

	if err := d.SetMemberAgentIatFloor("ghost", 1900000000); err != nil {
		t.Fatalf("SetMemberAgentIatFloor(unknown): %v", err)
	}
	if got := dalMemberIDs(t, d); !reflect.DeepEqual(got, []string{"ann"}) {
		t.Fatalf("roster after unknown floor update = %v, want [ann]", got)
	}
}

func TestSetMemberForcedStopAt(t *testing.T) {
	d := newAPITestDAL(t)
	m := dalPutMember(t, d, dalTestMember("ann", "Ann"))

	if err := d.SetMemberForcedStopAt("ann", 1800000000); err != nil {
		t.Fatalf("SetMemberForcedStopAt(newer): %v", err)
	}
	m.ForcedStopAt = 1800000000
	dalWantMember(t, d, m)

	if err := d.SetMemberForcedStopAt("ann", 0); err != nil {
		t.Fatalf("SetMemberForcedStopAt(zero): %v", err)
	}
	dalWantMember(t, d, m)
}

func TestSetMemberTokenKeyID(t *testing.T) {
	d := newAPITestDAL(t)
	m := dalPutMember(t, d, dalTestMember("ann", "Ann"))

	if err := d.SetMemberTokenKeyID("ann", "ring-new"); err != nil {
		t.Fatalf("SetMemberTokenKeyID(new): %v", err)
	}
	m.TokenKeyID = "ring-new"
	dalWantMember(t, d, m)

	if err := d.SetMemberTokenKeyID("ann", "ring-old"); err != nil {
		t.Fatalf("SetMemberTokenKeyID(observation can move backwards): %v", err)
	}
	m.TokenKeyID = "ring-old"
	dalWantMember(t, d, m)

	if err := d.SetMemberTokenKeyID("ghost", "ring-ghost"); err != nil {
		t.Fatalf("SetMemberTokenKeyID(unknown): %v", err)
	}
	if got := dalMemberIDs(t, d); !reflect.DeepEqual(got, []string{"ann"}) {
		t.Fatalf("roster after unknown key update = %v, want [ann]", got)
	}
}

func boolPtr(v bool) *bool { return &v }

// dalMemberColumnIsNull answers whether one member column holds SQL NULL, which
// no Go-side projection can tell apart from a zero value.

func dalMemberColumnIsNull(t *testing.T, d *DAL, id, column string) bool {
	t.Helper()
	var isNull bool
	if err := d.rdb.QueryRow(
		`SELECT `+column+` IS NULL FROM member WHERE id = ?`, id).Scan(&isNull); err != nil {
		t.Fatalf("read %s of %q: %v", column, id, err)
	}
	return isNull
}

// dalMemberIDs names every roster row, so a write that created one nobody asked
// for shows up.

func dalMemberIDs(t *testing.T, d *DAL) []string {
	t.Helper()
	rows, err := d.rdb.Query(`SELECT id FROM member ORDER BY id`)
	if err != nil {
		t.Fatalf("list member ids: %v", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan member id: %v", err)
		}
		out = append(out, id)
	}
	return out
}

// dalSplitColumns turns a SELECT column list into its column names.

func dalSplitColumns(list string) []string {
	out := []string{}
	for _, raw := range strings.Split(list, ",") {
		if name := strings.TrimSpace(raw); name != "" {
			out = append(out, name)
		}
	}
	return out
}
