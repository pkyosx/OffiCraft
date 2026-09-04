package main

// dal_member_patch_t63_test.go — the guards for the member table's PATCH door.
//
// Three properties are load-bearing here and each one has a mutant that turns a
// named assertion red:
//
//  1. A column you did not name is not written. Delete a field from the caller
//     and the column must keep its value; add one to the statement and the
//     round-trip below stops matching.
//  2. forced_stop_at only moves forward, on BOTH paths that write it. Clear
//     forwardOnly on its constructor and the table test names the column.
//  3. The property table is the only place a column is classified. Clear
//     insertOnly on any column and the classification test names it.
//
// Property 3 is the one that decides whether "adding a monotone column is one
// line" is true or merely intended, so it asserts the whole classification, not
// a spot check.

import (
	"reflect"
	"strings"
	"testing"
)

// TestPatchMemberWritesOnlyTheNamedColumns is the reason this door exists: a
// writer that names one column must not carry anything else, whatever it happens
// to be holding.
//
// The assertion compares the WHOLE row before and after, with the one named
// column patched into the expectation. A column that leaks into the statement
// fails here no matter which column it is — the alternative, listing the columns
// we thought to check, is exactly the fixture shape that stops noticing.
func TestPatchMemberWritesOnlyTheNamedColumns(t *testing.T) {
	d := newTestDAL(t)
	seed := fullMember("m-1")
	seed.ForcedStopAt = 100
	seed.AgentIatFloor = 50
	seed.HandoverNoticedTS = 25
	// 🔴 The three NULL-ruled columns are seeded with NON-NULL values ON PURPOSE.
	// fullMember leaves codename "" and linked_task_id nil, and the round-trip
	// below compares "nil" against "nil" for both — so a constructor that stored
	// "" where it should store SQL NULL would pass unnoticed. Seeding a real
	// codename and a real binding is what makes the comparison able to see them.
	bound := "t-63"
	seed.Codename = "O-214"
	seed.LinkedTaskID = &bound
	no := false
	seed.LastOpOK = &no
	if err := d.PutMember(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := d.GetMember("m-1")
	if err != nil || before == nil {
		t.Fatalf("read seed back: %v %v", before, err)
	}
	// The seed really landed — without this, every assertion below would also
	// pass on a row that was never written the way we think it was.
	if before.ForcedStopAt != 100 || before.AgentIatFloor != 50 || before.BankedCost != seed.BankedCost {
		t.Fatalf("seed did not land: forced_stop_at=%v agent_iat_floor=%v banked_cost=%v",
			before.ForcedStopAt, before.AgentIatFloor, before.BankedCost)
	}
	if before.Codename != "O-214" || before.LinkedTaskID == nil || *before.LinkedTaskID != bound ||
		before.LastOpOK == nil || *before.LastOpOK {
		t.Fatalf("the NULL-ruled seed did not land, so this test cannot see those "+
			"columns: codename=%q linked_task_id=%v last_op_ok=%v",
			before.Codename, before.LinkedTaskID, before.LastOpOK)
	}

	if err := d.PatchMember("m-1", mfName("renamed")); err != nil {
		t.Fatalf("patch: %v", err)
	}
	after, err := d.GetMember("m-1")
	if err != nil || after == nil {
		t.Fatalf("read back: %v %v", after, err)
	}

	want := *before
	want.Name = "renamed"
	if !reflect.DeepEqual(memberComparable(want), memberComparable(*after)) {
		t.Fatalf("a one-column patch changed more than the column it named.\n"+
			"before: %#v\nafter:  %#v\n"+
			"PatchMember must put ONLY the named fields into the UPDATE statement.",
			memberComparable(want), memberComparable(*after))
	}
}

// TestPatchMemberKeepsTwoConcurrentSingleColumnWritersApart is the failure the
// whole-row door produced and this one cannot: two writers holding snapshots
// taken at the same moment, each changing a different column.
func TestPatchMemberKeepsTwoConcurrentSingleColumnWritersApart(t *testing.T) {
	d := newTestDAL(t)
	if err := d.PutMember(fullMember("m-1")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.PatchMember("m-1", mfName("writer A")); err != nil {
		t.Fatalf("writer A: %v", err)
	}
	if err := d.PatchMember("m-1", mfDesiredState("offline")); err != nil {
		t.Fatalf("writer B: %v", err)
	}
	got, err := d.GetMember("m-1")
	if err != nil || got == nil {
		t.Fatalf("read back: %v %v", got, err)
	}
	if got.Name != "writer A" {
		t.Fatalf("writer B clobbered writer A's column: name = %q, want %q",
			got.Name, "writer A")
	}
	if got.DesiredState != "offline" {
		t.Fatalf("writer B's own column did not land: desired_state = %q", got.DesiredState)
	}
}

// TestPatchMemberUnknownIDIsACleanNoOp pins the answer for an id that names no
// row, because the second half of this migration turns 52 whole-row callers into
// patch callers and one of them will eventually run against a member dismissed
// between its read and its write.
//
// The answer is: no error, no row created. It matches every single-column setter
// beside it, and it is the reason PatchMember is an UPDATE rather than an upsert
// — an upsert would answer by MINTING a member whose other columns are whatever
// the schema defaults to.
func TestPatchMemberUnknownIDIsACleanNoOp(t *testing.T) {
	d := newTestDAL(t)
	if err := d.PatchMember("m-never-existed", mfName("ghost")); err != nil {
		t.Fatalf("patching an unknown id must not error, got %v", err)
	}
	got, err := d.GetMember("m-never-existed")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != nil {
		t.Fatalf("patching an unknown id must NOT create a row, got %#v.\n"+
			"PatchMember is an UPDATE on purpose: a patch that can create rows "+
			"lets a caller naming two columns mint a member whose other columns "+
			"are schema defaults.", *got)
	}
	// Positive control: the same call against a row that DOES exist lands, so
	// the assertion above is not passing because PatchMember writes nothing.
	if err := d.PutMember(fullMember("m-1")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.PatchMember("m-1", mfName("ghost")); err != nil {
		t.Fatalf("patch: %v", err)
	}
	if m, err := d.GetMember("m-1"); err != nil || m == nil || m.Name != "ghost" {
		t.Fatalf("positive control: the same patch must land on an existing row, got %v %v", m, err)
	}
}

// TestTheThreeNULLRulesStoreRealSQLNULL asks the DATABASE what was stored,
// because a Member-level comparison is the wrong instrument here — for ONE of
// the three columns it is the wrong instrument in principle, and for the other
// two it was merely the wrong fixture.
//
// 🔴 THAT DISTINCTION IS WORTH GETTING RIGHT, because the first version of this
// comment collapsed the two cases and taught something false. scanMember reads
// linked_task_id and last_op_ok through sql.NullString / sql.NullBool and only
// assigns the pointer when Valid, so NULL and the zero value ARE distinguishable
// in a Member — seeding a non-zero value first would have caught those two.
// codename is the one that is structurally invisible: scanMember assigns
// codename.String unconditionally, so SQL NULL and "" both arrive as "", and no
// comparison of Members can ever tell them apart.
//
// Asking the database covers all three the same way and does not depend on which
// case each column happens to be in.
//
// The rules matter for three different reasons. codename "" → NULL keeps the
// PARTIAL UNIQUE codename index from colliding across the many codename-less
// staff rows (NULLs are mutually distinct in SQLite), and that failure is a
// write that starts being refused, not a wrong value. linked_task_id nil → NULL
// keeps "unbound" out of every join. last_op_ok is THREE-VALUED — "no op
// reported yet" is not "the op failed", and the cockpit renders those
// differently.
//
// Both halves are checked, because the patch door and the insert door build
// their values from the SAME constructors and a rule can only be lost in one
// place: the constructors.
//
// MUTANTS: make any of mfCodename / mfLinkedTaskID / mfLastOpOK store the zero
// value instead of nil ⇒ this test names the column.
func TestTheThreeNULLRulesStoreRealSQLNULL(t *testing.T) {
	isNull := func(t *testing.T, d *DAL, id, col string) bool {
		t.Helper()
		var n int
		if err := d.rdb.QueryRow(
			`SELECT `+col+` IS NULL FROM member WHERE id = ?`, id).Scan(&n); err != nil {
			t.Fatalf("read %s: %v", col, err)
		}
		return n == 1
	}
	cols := []string{"codename", "linked_task_id", "last_op_ok"}
	// Each column's OWN consequence. Printing one column's rationale for all
	// three is how a failure message starts teaching the wrong thing.
	whyNULL := map[string]string{
		"codename": "the partial UNIQUE codename index collides across every " +
			"codename-less staff row once they stop being mutually-distinct NULLs",
		"linked_task_id": `"" is a task id nothing can join on, so an unbound ` +
			"member starts matching rows it has no binding to",
		"last_op_ok": `false means "the op failed"; NULL means "no op reported ` +
			`yet", and the cockpit renders those differently`,
	}

	t.Run("through the whole-row INSERT", func(t *testing.T) {
		d := newTestDAL(t)
		seed := fullMember("m-1")
		seed.Codename = ""      // no codename — the many staff rows
		seed.LinkedTaskID = nil // unbound
		seed.LastOpOK = nil     // no op reported yet
		if err := d.PutMember(seed); err != nil {
			t.Fatalf("seed: %v", err)
		}
		for _, c := range cols {
			if !isNull(t, d, "m-1", c) {
				t.Errorf("member.%s stored a zero VALUE where the rule says SQL NULL: %s. "+
					"The rule lives on that column's constructor in dal_member_patch.go.",
					c, whyNULL[c])
			}
		}
	})

	t.Run("through the patch door", func(t *testing.T) {
		d := newTestDAL(t)
		seed := fullMember("m-1")
		bound := "t-63"
		yes := true
		seed.Codename = "O-214"
		seed.LinkedTaskID = &bound
		seed.LastOpOK = &yes
		if err := d.PutMember(seed); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// Positive control: the seed really landed non-NULL, so the assertions
		// below are not passing on a row that was NULL all along.
		for _, c := range cols {
			if isNull(t, d, "m-1", c) {
				t.Fatalf("seed did not land: member.%s is already NULL", c)
			}
		}
		if err := d.PatchMember("m-1",
			mfCodename(""), mfLinkedTaskID(nil), mfLastOpOK(nil)); err != nil {
			t.Fatalf("patch: %v", err)
		}
		for _, c := range cols {
			if !isNull(t, d, "m-1", c) {
				t.Errorf("member.%s did not go back to SQL NULL through the patch "+
					"door — the constructor's NULL rule is not being applied on this "+
					"path", c)
			}
		}
	})
}

// TestInsertOnlyDoesNotSuppressAColumnTheCallerNAMES pins the half of insertOnly
// that is easy to get backwards. The flag means "a WHOLE-ROW writer must not
// carry this column", not "this column is read-only": SetMemberModel and its
// siblings exist precisely to write insert-only columns, and after T-63 they can
// do it through PatchMember.
//
// MUTANT: make patchMemberOn skip fields flagged insertOnly. Without this test
// the new file stays green (the behavioural fallout lands in OTHER FILES of this
// same package — the agent-iat-floor and owner-intent guards), and the doc comment on
// memberField would be describing a property nothing checked.
func TestInsertOnlyDoesNotSuppressAColumnTheCallerNAMES(t *testing.T) {
	d := newTestDAL(t)
	if err := d.PutMember(fullMember("m-1")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// model is insert-only; naming it in a targeted patch must still write it.
	if err := d.PatchMember("m-1", mfModel("sonnet")); err != nil {
		t.Fatalf("patch: %v", err)
	}
	got, err := d.GetMember("m-1")
	if err != nil || got == nil {
		t.Fatalf("read back: %v %v", got, err)
	}
	if got.Model != "sonnet" {
		t.Fatalf("a targeted patch NAMING an insert-only column wrote nothing: "+
			"model = %q, want %q.\ninsertOnly restricts WHOLE-ROW writers; it must "+
			"not make the column unwritable, or every single-column setter that "+
			"routes through PatchMember becomes a silent no-op.", got.Model, "sonnet")
	}
	// The forward-only twin of the same property: agent_iat_floor is BOTH
	// insert-only and forward-only, and SetMemberAgentIatFloor writes it through
	// this door.
	if err := d.SetMemberAgentIatFloor("m-1", 700); err != nil {
		t.Fatalf("raise floor: %v", err)
	}
	if m, err := d.GetMember("m-1"); err != nil || m == nil || m.AgentIatFloor != 700 {
		t.Fatalf("SetMemberAgentIatFloor must still raise the floor through the "+
			"patch door; got %v %v", m, err)
	}
}

// TestForcedStopAtOnlyMovesForwardOnEveryPath covers the owner's rule for a
// 「只能往前」 column (rc-78cb22a6de94) on BOTH doors that write it.
//
// SetMemberForcedStopAt used to be a plain `forced_stop_at = ?` while the
// whole-row door used max(), so the two disagreed about the same column: a
// backdated stamp through the setter erased a force-stop the gate had already
// recorded. Both paths now go through the column's own forwardOnly declaration.
//
// MUTANT: drop `forwardOnly: true` from mfForcedStopAt and both sub-tests go red
// naming forced_stop_at.
func TestForcedStopAtOnlyMovesForwardOnEveryPath(t *testing.T) {
	paths := []struct {
		name  string
		write func(d *DAL, id string, ts float64) error
	}{
		{"whole-row PutMember", func(d *DAL, id string, ts float64) error {
			m := fullMember(id)
			m.ForcedStopAt = ts
			return d.PutMember(m)
		}},
		{"SetMemberForcedStopAt", func(d *DAL, id string, ts float64) error {
			return d.SetMemberForcedStopAt(id, ts)
		}},
	}
	for _, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			d := newTestDAL(t)
			seed := fullMember("m-1")
			seed.ForcedStopAt = 500
			if err := d.PutMember(seed); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if m, err := d.GetMember("m-1"); err != nil || m == nil || m.ForcedStopAt != 500 {
				t.Fatalf("seed did not land: %v %v", m, err)
			}

			// A stale writer carrying an older stamp — or none at all.
			if err := p.write(d, "m-1", 100); err != nil {
				t.Fatalf("stale write: %v", err)
			}
			m, err := d.GetMember("m-1")
			if err != nil || m == nil {
				t.Fatalf("read back: %v %v", m, err)
			}
			if m.ForcedStopAt != 500 {
				t.Fatalf("member.forced_stop_at walked BACKWARDS through %s: 500 → %v.\n"+
					"forced_stop_at is declared forwardOnly (mfForcedStopAt in "+
					"dal_member_patch.go); every writer must go through that "+
					"declaration so a stale snapshot cannot erase a force-stop.",
					p.name, m.ForcedStopAt)
			}

			// Forward still moves, so the guard above is not passing because the
			// column became unwritable.
			if err := p.write(d, "m-1", 900); err != nil {
				t.Fatalf("forward write: %v", err)
			}
			m, err = d.GetMember("m-1")
			if err != nil || m == nil {
				t.Fatalf("read back: %v %v", m, err)
			}
			if m.ForcedStopAt != 900 {
				t.Fatalf("member.forced_stop_at must still move forward through %s: got %v, want 900",
					p.name, m.ForcedStopAt)
			}
		})
	}
}

// TestMemberColumnPropertiesAreDeclaredInOnePlace asserts the classification
// itself, so "adding a monotone column is ONE LINE" is a fact rather than an
// intention: the only way to change what a column is, is to change its
// constructor, and doing so lands here NAMING the column.
//
// It asserts the WHOLE classification rather than spot-checking, because the
// failure this replaces is a column silently joining or leaving the whole-row
// writer's update — which nothing else notices.
//
// MUTANT (insert-only): clear insertOnly on any column ⇒ red naming it, and the
// behavioural twin TestPutMemberNeverOverwritesSingleColumnOwnedFields goes red
// too.
// MUTANT (forward-only): clear forwardOnly on forced_stop_at or agent_iat_floor
// ⇒ red naming it.
func TestMemberColumnPropertiesAreDeclaredInOnePlace(t *testing.T) {
	// A whole-row writer carries every column on INSERT and only the
	// non-insertOnly ones onto an existing row.
	wantInsertOnly := map[string]bool{
		"id": true, "runtime": true, "model": true, "effort": true,
		"desired_machine_id": true, "banked_cost": true,
		"last_op": true, "last_op_ok": true, "last_op_log": true,
		"last_op_reason": true, "last_op_at": true,
		"avatar_attachment_id": true, "handover_noticed_ts": true,
		"agent_iat_floor": true,
		// The four wind-down anchors, moved out by T-55 batch C (#391) and
		// carried through this refactor as flags rather than as an edit to a SET
		// list — which is the whole point: a column joins or leaves the whole-row
		// update by its own constructor and by nothing else.
		"stopping_since": true, "stopped_since": true,
		"refocus_since": true, "refocus_op": true,
	}
	// Columns that only ever move forward. agent_iat_floor is BOTH: it is
	// insert-only today AND monotone, and declaring the monotonicity now is what
	// makes the max() already true on the day it is allowed onto an existing row.
	wantForwardOnly := map[string]bool{
		"forced_stop_at": true, "agent_iat_floor": true,
	}

	fields := memberWholeRow(fullMember("m-1"))
	gotInsertOnly := map[string]bool{}
	gotForwardOnly := map[string]bool{}
	seen := map[string]bool{}
	for _, f := range fields {
		if seen[f.col] {
			t.Fatalf("column %q is declared twice in memberWholeRow", f.col)
		}
		seen[f.col] = true
		if f.insertOnly {
			gotInsertOnly[f.col] = true
		}
		if f.forwardOnly {
			gotForwardOnly[f.col] = true
		}
	}

	for col := range wantInsertOnly {
		if !gotInsertOnly[col] {
			t.Errorf("member.%s lost its insertOnly declaration ⇒ a whole-row "+
				"writer will now carry a STALE value of it onto an existing row. "+
				"Its single-column setter is meant to be the only writer that "+
				"moves it; its constructor is in dal_member_patch.go.", col)
		}
	}
	for col := range gotInsertOnly {
		if !wantInsertOnly[col] {
			t.Errorf("member.%s became insertOnly ⇒ it LEFT the whole-row "+
				"writer's update. That is a real behaviour change (52 callers "+
				"stop writing it); say why, then bump this list.", col)
		}
	}
	for col := range wantForwardOnly {
		if !gotForwardOnly[col] {
			t.Errorf("member.%s lost its forwardOnly declaration ⇒ a stale or "+
				"zero value can now walk it BACKWARDS. It is a 「只能往前」 "+
				"column (owner rc-78cb22a6de94); the declaration is on its "+
				"constructor in dal_member_patch.go.", col)
		}
	}
	for col := range gotForwardOnly {
		if !wantForwardOnly[col] {
			t.Errorf("member.%s became forwardOnly.\n"+
				"🔴 BEFORE you bump this list: find every single-column setter that "+
				"writes this column and check it goes through PatchMember. A setter "+
				"writing its own `col = ?` beside a forwardOnly declaration walks the "+
				"value BACKWARDS while the whole-row door holds — one property, two "+
				"representations, and nothing red. That is the exact bug "+
				"SetMemberForcedStopAt had. Converge the setter FIRST, then bump this "+
				"list.", col)
		}
	}

	// The whole-row door's update set is derived from the flags, not from a
	// hand-kept SET list. 35 columns minus the 14 insert-only ones is the 21 the
	// old ON CONFLICT DO UPDATE SET wrote.
	// The SET of columns must equal memberColumns exactly. This is the assertion
	// the comment on memberWholeRow points at: order is free (each column name is
	// emitted beside its own placeholder), membership is not. A column added to
	// the schema and to memberColumns but not here would be silently absent from
	// every whole-row INSERT.
	wantCols := map[string]bool{}
	for _, c := range strings.Split(memberColumns, ",") {
		if c = strings.TrimSpace(c); c != "" {
			wantCols[c] = true
		}
	}
	for c := range wantCols {
		if !seen[c] {
			t.Errorf("member.%s is in memberColumns but memberWholeRow does not "+
				"project it — a whole-row INSERT would leave it at its schema "+
				"default", c)
		}
	}
	for c := range seen {
		if !wantCols[c] {
			t.Errorf("memberWholeRow projects member.%s, which is not in "+
				"memberColumns — the INSERT would name a column the read side "+
				"never scans", c)
		}
	}
	if got, want := len(fields), len(wantCols); got != want {
		t.Errorf("memberWholeRow projects %d columns, memberColumns has %d", got, want)
	}
	// 36 columns minus the 18 insert-only ones. The number moves whenever a
	// column is migrated out (T-55 has been doing that in batches), and it is
	// asserted rather than derived ON PURPOSE: a column silently joining or
	// leaving the whole-row update is the failure this whole line of work exists
	// to stop, so it has to be a deliberate edit here, in the same commit.
	if got, want := len(updatableMemberFields(fields)), 18; got != want {
		t.Errorf("a whole-row write now updates %d columns, want %d. If you "+
			"migrated a column out, say so by bumping this number; if you did "+
			"not, a column changed sides on its own.", got, want)
	}
}

// memberComparable flattens the two pointer fields into values so a whole row
// can be compared with DeepEqual. It FLATTENS rather than drops them so the
// comparison can see last_op_ok's three-valued state and linked_task_id's
// nil-means-unbound.
//
// ⚠️ Flattening CANNOT guard the NULL rules and must not be read as doing so:
// "" and SQL NULL both scan back as the zero value, so no comparison of Members
// can tell them apart. What flattening buys is that a LEAK into either column is
// visible — which is why the caller above seeds a real codename, a real task
// binding and an explicit false. The NULL rules themselves are asked of the
// database directly, in TestTheThreeNULLRulesStoreRealSQLNULL.
type comparableMember struct {
	Row          Member
	LastOpOK     string // "nil" | "true" | "false" — the three-valued state
	LinkedTaskID string // "nil" | the id
}

func memberComparable(m Member) comparableMember {
	c := comparableMember{LastOpOK: "nil", LinkedTaskID: "nil"}
	if m.LastOpOK != nil {
		if *m.LastOpOK {
			c.LastOpOK = "true"
		} else {
			c.LastOpOK = "false"
		}
	}
	if m.LinkedTaskID != nil {
		c.LinkedTaskID = *m.LinkedTaskID
	}
	m.LastOpOK = nil
	m.LinkedTaskID = nil
	c.Row = m
	return c
}
