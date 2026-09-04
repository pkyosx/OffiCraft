package main

// single_column_writes_t14_test.go — T-14 項目 6, ONE invariant, for a GROWING
// set of columns:
//
//	a column that has been migrated to a single-column writer must never
//	become writable again by a whole-row write of the member row.
//
// The MECHANISM behind that sentence changed in T-63; the invariant did not.
// There is no hand-written ON CONFLICT DO UPDATE SET list any more — which
// columns a whole-row write lands on an EXISTING row is derived from each
// column's insertOnly flag on its constructor in dal_member_patch.go. Every
// assertion below asks the database what happened rather than reading the text
// of the SQL, which is exactly why this file survived that refactor untouched.
//
// The migration is only half a fix. Writing SetMemberX / AddMemberX and leaving
// the column writable by a whole-row write changes nothing at all: every stale
// snapshot still lands on it, and the suite stays green either way — which is
// exactly how the handover claim nearly shipped broken (T-ffdf). Until this
// file existed, the ONLY thing holding the second half in place was a comment
// in dal.go, and comments do not fail builds.
//
// Data-driven ON PURPOSE, and the shape matters: the guard asserts the DATABASE
// BEHAVIOUR (stamp through the sole writer, run a stale whole-row upsert over
// it, read back), never the TEXT of the SQL. A test that string-matched the
// statement would go red on a reflow and stay green on a semantic change — the
// wrong way round. Migrating the next column is one entry in the table below.

import (
	"sort"
	"strings"
	"testing"
)

// The op receipt is FIVE columns behind ONE writer (T-55), so its five registry
// rows all stamp through this one call and then each check its own column. The
// values are arbitrary but must be non-zero in every column at once: a row that
// left one of them on its zero could not tell "the stale upsert clobbered it"
// from "the stamp never wrote it".
const (
	probeReceiptOp     = "start"
	probeReceiptLog    = "probe log"
	probeReceiptReason = "probe reason"
	probeReceiptAt     = float64(4243)
)

func stampProbeReceipt(d *DAL, id string) error {
	ok := true
	return d.SetMemberOpReceipt(id, probeReceiptOp, &ok, probeReceiptLog, probeReceiptReason,
		probeReceiptAt)
}

// The four wind-down anchors move together through one writer, so they share a
// stamp the way the receipt columns do.
//
// 🔴 EVERY VALUE HERE IS DIFFERENT, AND THAT IS THE POINT. These are anchors: a
// writer that drops one of them leaves it at 0/"" — which is exactly what "was
// never written" looks like — so a fixture that stamped the same number into two
// of them would read green through a setter that transposed its parameters or
// forgot a column. Distinct values make every one of those mistakes name itself.
const (
	probeStoppingSince = 7_001.0
	probeStoppedSince  = 7_002.0
	probeRefocusSince  = 7_003.0
	probeRefocusOp     = "probe-refocus-op"
)

func stampProbeAnchors(d *DAL, id string) error {
	return d.SetMemberWindDownAnchors(id, probeStoppingSince, probeStoppedSince,
		probeRefocusSince, probeRefocusOp)
}

// singleColumnOwnedFields is the registry the guard iterates. Add the entry in
// the SAME commit that removes a column from PutMember's SET list.
var singleColumnOwnedFields = []struct {
	// column names the database column, and is what the failure message must
	// print — a reader who breaks this needs to be told WHICH column, not that
	// "a member upsert regressed".
	column string
	// writer names the single-column writer this row stamps through, so the
	// message says where the fix lives. It is not necessarily the column's ONLY
	// writer (banked_cost has two: one accumulates, one resets); what the
	// invariant below forbids is the WHOLE-ROW upsert carrying the column.
	writer string
	// stamp moves the column off its zero through that writer.
	stamp func(*DAL, string) error
	// want is the value stamp must have left behind. `any` rather than a
	// number because the registry outgrew the numeric anchors it started on —
	// the owner-intent columns (T-55) are strings, and a string column is
	// clobbered by a stale snapshot exactly the same way. Compared with !=, so
	// every entry must carry a COMPARABLE dynamic type.
	want any
	// read pulls the column out of a round-tripped row.
	read func(Member) any
	// stale zeroes the column on a snapshot, imitating every whole-row writer
	// that read the row before the stamp landed.
	stale func(*Member)
}{
	{
		column: "banked_cost",
		writer: "AddMemberBankedCost",
		stamp:  func(d *DAL, id string) error { return d.AddMemberBankedCost(id, 42.5) },
		want:   42.5,
		read:   func(m Member) any { return m.BankedCost },
		stale:  func(m *Member) { m.BankedCost = 0 },
	},
	{
		column: "handover_noticed_ts",
		writer: "SetMemberHandoverNoticedTS",
		stamp:  func(d *DAL, id string) error { return d.SetMemberHandoverNoticedTS(id, 4242) },
		want:   float64(4242),
		read:   func(m Member) any { return m.HandoverNoticedTS },
		stale:  func(m *Member) { m.HandoverNoticedTS = 0 },
	},
	{
		column: "agent_iat_floor",
		writer: "SetMemberAgentIatFloor",
		stamp:  func(d *DAL, id string) error { return d.SetMemberAgentIatFloor(id, 1700) },
		want:   float64(1700),
		read:   func(m Member) any { return m.AgentIatFloor },
		stale:  func(m *Member) { m.AgentIatFloor = 0 },
	},
	{
		// T-80. "" is what "this station has never verified a credential of
		// this member's" looks like, so a stale whole-row write that blanks the
		// column turns a machine the owner has SEEN back into one he has not —
		// and the whole point of the column is to tell him whether it is safe to
		// remove the outgoing signing key. Under-counting the migrated fleet is
		// the direction that keeps him from ever pressing it.
		column: "token_key_id",
		writer: "SetMemberTokenKeyID",
		stamp: func(d *DAL, id string) error {
			return d.SetMemberTokenKeyID(id, "k-observed")
		},
		want:  "k-observed",
		read:  func(m Member) any { return m.TokenKeyID },
		stale: func(m *Member) { m.TokenKeyID = "" },
	},
	{
		column: "desired_machine_id",
		writer: "SetMemberDesiredMachineID",
		stamp: func(d *DAL, id string) error {
			return d.SetMemberDesiredMachineID(id, "m-relocated-here")
		},
		want:  "m-relocated-here",
		read:  func(m Member) any { return m.DesiredMachineID },
		stale: func(m *Member) { m.DesiredMachineID = "" },
	},
	{
		column: "model",
		writer: "SetMemberModel",
		stamp:  func(d *DAL, id string) error { return d.SetMemberModel(id, "opus") },
		want:   "opus",
		read:   func(m Member) any { return m.Model },
		stale:  func(m *Member) { m.Model = "" },
	},
	{
		column: "runtime",
		writer: "SetMemberRuntime",
		stamp:  func(d *DAL, id string) error { return d.SetMemberRuntime(id, RuntimeCodex) },
		want:   RuntimeCodex,
		read:   func(m Member) any { return m.Runtime },
		// "" is the durable "nobody has picked yet", which is exactly the value
		// a snapshot taken before the owner's save carries.
		stale: func(m *Member) { m.Runtime = "" },
	},
	{
		column: "effort",
		writer: "SetMemberEffort",
		stamp:  func(d *DAL, id string) error { return d.SetMemberEffort(id, "max") },
		want:   "max",
		read:   func(m Member) any { return m.Effort },
		stale:  func(m *Member) { m.Effort = "" },
	},
	{
		column: "last_op",
		writer: "SetMemberOpReceipt",
		stamp:  stampProbeReceipt,
		want:   probeReceiptOp,
		read:   func(m Member) any { return m.LastOp },
		stale:  func(m *Member) { m.LastOp = "" },
	},
	{
		// 🔴 read PROJECTS the *bool instead of returning it. want is compared
		// with !=, and two pointers are equal only when they are the SAME
		// pointer — a round-tripped row always carries a fresh one, so handing
		// the pointer straight back would make this row report a clobber on
		// every run, for a reason that has nothing to do with the column. The
		// projection also keeps the THIRD state distinguishable: nil comes back
		// as a string no bool can equal, so a stale upsert that blanks the
		// verdict reddens rather than reading as `false`.
		column: "last_op_ok",
		writer: "SetMemberOpReceipt",
		stamp:  stampProbeReceipt,
		want:   true,
		read: func(m Member) any {
			if m.LastOpOK == nil {
				return "nil"
			}
			return *m.LastOpOK
		},
		stale: func(m *Member) { m.LastOpOK = nil },
	},
	{
		column: "last_op_log",
		writer: "SetMemberOpReceipt",
		stamp:  stampProbeReceipt,
		want:   probeReceiptLog,
		read:   func(m Member) any { return m.LastOpLog },
		stale:  func(m *Member) { m.LastOpLog = "" },
	},
	{
		column: "last_op_reason",
		writer: "SetMemberOpReceipt",
		stamp:  stampProbeReceipt,
		want:   probeReceiptReason,
		read:   func(m Member) any { return m.LastOpReason },
		stale:  func(m *Member) { m.LastOpReason = "" },
	},
	{
		column: "last_op_at",
		writer: "SetMemberOpReceipt",
		stamp:  stampProbeReceipt,
		want:   probeReceiptAt,
		read:   func(m Member) any { return m.LastOpAt },
		stale:  func(m *Member) { m.LastOpAt = 0 },
	},
	{
		// The OLDEST member of this class and the last one to be registered:
		// avatar_attachment_id has been out of the SET list since T-c826, with
		// ReplaceMemberAvatar / DeleteMemberAvatar as its only update seams —
		// but nothing enforced that, so putting the column back would have gone
		// unnoticed here while every OTHER migrated column was guarded. The
		// consequence is worse than the usual clobber: the pointer's previous
		// blob is DELETEd on replace, so a stale snapshot restoring the old id
		// leaves the row pointing at a blob that no longer exists.
		column: "avatar_attachment_id",
		writer: "ReplaceMemberAvatar / DeleteMemberAvatar",
		stamp: func(d *DAL, id string) error {
			return d.ReplaceMemberAvatar(id, ChatAttachment{
				ID: "ava-" + id, Mime: "image/png", Data: avatarTestPNG,
			})
		},
		want:  "ava-m-avatar_attachment_id",
		read:  func(m Member) any { return m.AvatarAttachmentID },
		stale: func(m *Member) { m.AvatarAttachmentID = "" },
	},
	{
		column: "stopping_since",
		writer: "SetMemberWindDownAnchors",
		stamp:  stampProbeAnchors,
		want:   probeStoppingSince,
		read:   func(m Member) any { return m.StoppingSince },
		stale:  func(m *Member) { m.StoppingSince = 0 },
	},
	{
		column: "stopped_since",
		writer: "SetMemberWindDownAnchors",
		stamp:  stampProbeAnchors,
		want:   probeStoppedSince,
		read:   func(m Member) any { return m.StoppedSince },
		stale:  func(m *Member) { m.StoppedSince = 0 },
	},
	{
		column: "refocus_since",
		writer: "SetMemberWindDownAnchors",
		stamp:  stampProbeAnchors,
		want:   probeRefocusSince,
		read:   func(m Member) any { return m.RefocusSince },
		stale:  func(m *Member) { m.RefocusSince = 0 },
	},
	{
		column: "refocus_op",
		writer: "SetMemberWindDownAnchors",
		stamp:  stampProbeAnchors,
		want:   probeRefocusOp,
		read:   func(m Member) any { return m.RefocusOp },
		stale:  func(m *Member) { m.RefocusOp = "" },
	},
}

// TestPutMemberNeverOverwritesSingleColumnOwnedFields is the automatic guard.
//
// Mutant for any row: clear `insertOnly` on that column's constructor in
// dal_member_patch.go and this test goes red NAMING that column.
func TestPutMemberNeverOverwritesSingleColumnOwnedFields(t *testing.T) {
	// A deleted row is the one mutation the loop below cannot see: the guard
	// would pass by iterating less. Bump this deliberately when the registry
	// grows.
	if len(singleColumnOwnedFields) != 18 {
		t.Fatalf("singleColumnOwnedFields has %d entries, expected 18. Adding a "+
			"column? Bump this number. REMOVING one? That means a column became "+
			"writable by a whole-row write again — say why in the commit",
			len(singleColumnOwnedFields))
	}

	for _, f := range singleColumnOwnedFields {
		t.Run(f.column, func(t *testing.T) {
			d := newTestDAL(t)
			id := "m-" + f.column
			seed := fullMember(id)
			f.stale(&seed) // born at the zero the column's INSERT carries
			if err := d.PutMember(seed); err != nil {
				t.Fatalf("seed member: %v", err)
			}
			if err := f.stamp(d, id); err != nil {
				t.Fatalf("%s: %v", f.writer, err)
			}

			// The whole-row writer: a snapshot taken BEFORE the stamp, which is
			// every snapshot, since nothing but a single-column writer moves it.
			stale := seed
			f.stale(&stale)
			stale.Name = "renamed by an unrelated write"
			if err := d.PutMember(stale); err != nil {
				t.Fatalf("whole-row upsert: %v", err)
			}

			after, err := d.GetMember(id)
			if err != nil || after == nil {
				t.Fatalf("read back: %v %v", after, err)
			}
			if got := f.read(*after); got != f.want {
				// The TYPES are printed because `want`/`read` are `any` and a
				// mismatched pair compares unequal on type alone: float64(0)
				// against int64(0) prints "0 → 0" and reads as a database
				// regression when it is really a wrong registry entry. Comparing
				// as `any` is STRICTER than the float64 it replaced, never
				// looser — verified by seeding both of those pairs.
				t.Fatalf("member.%s was clobbered by a whole-row upsert: %#v (%T) → %#v (%T).\n"+
					"%s writes this column one column at a time; a whole-row "+
					"write must never land it on an existing row. If you just "+
					"cleared `insertOnly` on this column's constructor in "+
					"dal_member_patch.go, that is the line to restore.",
					f.column, f.want, f.want, got, got, f.writer)
			}
			// Positive control: the upsert really ran, so the assertion above
			// is not passing because nothing was written at all.
			if after.Name != "renamed by an unrelated write" {
				t.Fatalf("the upsert itself must have landed; got name %q", after.Name)
			}
		})
	}
}

// TestAddMemberBankedCostAccumulatesAndIsRowScoped covers what the registry
// entry above cannot: the writer's own semantics.
//
// It ADDS rather than sets, in SQL, because the banking edges overlap — an SSE
// last-disconnect can race a kill funnel on the same actor. A Go-side
// read-modify-write would let one edge's spend vanish, and vanishing spend is
// the failure nobody reports (the number just looks low).
func TestAddMemberBankedCostAccumulatesAndIsRowScoped(t *testing.T) {
	d := newTestDAL(t)
	seed := fullMember("m-1")
	seed.BankedCost = 0
	if err := d.PutMember(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	neighbour := fullMember("m-2")
	neighbour.BankedCost = 9
	if err := d.PutMember(neighbour); err != nil {
		t.Fatalf("seed neighbour: %v", err)
	}

	for _, delta := range []float64{1.25, 2.5} {
		if err := d.AddMemberBankedCost("m-1", delta); err != nil {
			t.Fatalf("add %v: %v", delta, err)
		}
	}
	got, err := d.GetMember("m-1")
	if err != nil || got == nil {
		t.Fatalf("read back: %v %v", got, err)
	}
	if got.BankedCost != 3.75 {
		t.Fatalf("banked_cost = %v, want 3.75 — the writer must ACCUMULATE, "+
			"not overwrite", got.BankedCost)
	}
	if other, _ := d.GetMember("m-2"); other == nil || other.BankedCost != 9 {
		t.Fatalf("a bank must touch ONLY its own row, m-2 = %+v", other)
	}

	// A missing row is a clean no-op, like every other single-column setter.
	if err := d.AddMemberBankedCost("m-nope", 1); err != nil {
		t.Fatalf("banking an unknown id must be a silent no-op, got %v", err)
	}
}

// notInTheSetListExceptions names the columns a whole-row write leaves alone —
// or handles specially — for reasons that are NOT "a single-column writer owns
// this", so the completeness guard below must not demand a registry entry for
// them. Each one is a claim about the write, so each one says why.
var notInTheSetListExceptions = map[string]string{
	// The conflict key. It is what the upsert matches ON; a SET list carrying
	// it would be meaningless rather than dangerous.
	"id": "the ON CONFLICT key",
	// 🔴 A NAMED BLIND SPOT, not a column that is safe.
	//
	// forced_stop_at IS in the SET list, but as
	// max(forced_stop_at, excluded.forced_stop_at) — the one entry that is not a
	// plain overwrite. What protects it is that direction, NOT absence from the
	// list, so a registry entry would assert the wrong invariant.
	//
	// ① THIS GUARD DOES NOT COVER IT. The probe cannot even tell which side it
	// is on: it writes a TEXT sentinel, SQLite orders TEXT above every number,
	// so max() keeps the sentinel and the column reads as "absent". The blind
	// spot comes from a collation accident, not from the column's own design.
	//
	// ② SO NOTHING HERE WOULD STOP SOMEONE MIGRATING IT — but the REASON changed
	// in T-63, and the reason this note used to give is now the opposite of the
	// truth. It warned that the single-column writer already on the tree,
	// SetMemberForcedStopAt, was a plain assignment with NO max(), so migrating
	// the column to it would silently lose the forward-only property. That setter
	// now goes through PatchMember and picks up the column's own forwardOnly
	// declaration, so BOTH paths write max() and the property survives such a
	// migration. What is still uncovered is only ①: this guard cannot see which
	// side of the line the column is on. Whoever proposes that migration has to
	// argue it on the code; this guard will not argue back.
	"forced_stop_at": "carried under max(), forward-only — UNCOVERED by this guard, see above",
}

// TestEveryColumnOutOfTheSetListIsRegistered is the guard on the REGISTRY
// ITSELF, and it exists because the registry above is hand-maintained.
//
// The per-column guard proves that a REGISTERED column stays out of the SET
// list. It says nothing about the other direction — a column pulled out of the
// SET list with no entry added here is unguarded, and nothing goes red. That is
// not hypothetical: avatar_attachment_id sat outside the SET list since T-c826
// and outside this registry until T-55, so putting it back would have silently
// re-armed a bug whose blast radius is worse than the usual clobber (the
// replaced blob is DELETEd, so restoring the old pointer orphans it).
//
// It derives the answer rather than restating it. memberColumns is the column
// list the statement itself binds, and the probe asks the DATABASE which of
// them a stale whole-row upsert can still move — the same behavioural question
// the per-column guard asks, and for the same reason: a test that parsed the
// SQL text would go red on a reflow and stay green on a semantic change.
//
// Mutant: delete any entry from singleColumnOwnedFields, or take a column out
// of PutMember's SET list without adding one, and this test names the column.
func TestEveryColumnOutOfTheSetListIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, f := range singleColumnOwnedFields {
		registered[f.column] = true
	}

	var unguarded, stale []string
	for _, col := range strings.Split(memberColumns, ",") {
		col = strings.TrimSpace(strings.ReplaceAll(col, "\n", ""))
		if col == "" {
			continue
		}
		if _, exempt := notInTheSetListExceptions[col]; exempt {
			if registered[col] {
				stale = append(stale, col+" (exempt, yet registered)")
			}
			continue
		}
		if survivesAStaleWholeRowUpsert(t, col) == registered[col] {
			continue
		}
		if registered[col] {
			// Registered, but the upsert still moves it. The per-column guard
			// above should already be red; this catches the case where its
			// entry is the one that is wrong (a stale closure that reads or
			// stamps the wrong thing).
			stale = append(stale, col+" (registered, but a stale upsert still overwrites it)")
		} else {
			unguarded = append(unguarded, col)
		}
	}
	sort.Strings(unguarded)
	sort.Strings(stale)

	if len(unguarded) > 0 {
		t.Fatalf("these columns are NOT landed by a whole-row write, so a stale "+
			"whole-row upsert can no longer move them — but singleColumnOwnedFields "+
			"has no entry for them, so nothing would notice if they went back in: %v.\n"+
			"Add an entry (and bump the count in the guard above), or, if the column "+
			"is out of the list for some reason OTHER than a single-column writer "+
			"owning it, name it in notInTheSetListExceptions with the reason.",
			unguarded)
	}
	if len(stale) > 0 {
		t.Fatalf("singleColumnOwnedFields disagrees with what the database does: %v", stale)
	}
}

// probeOverrides gives a LEGAL replacement value for columns whose CHECK
// constraint refuses the generic sentinel. The probe does not guess: it tries
// the sentinel, and a constraint failure with no override here fails the test
// asking for one — so a newly constrained column cannot slip past the guard by
// being unprobeable. The value only has to DIFFER from what fullMember seeds.
var probeOverrides = map[string]string{
	"kind": KindWarden, // CHECK kind IN ('staff','warden','outsource'); fullMember seeds "staff"
}

// survivesAStaleWholeRowUpsert writes a probe value straight into one column,
// runs a whole-row upsert carrying the row as it was BEFORE that write, and
// reports whether the probe value is still there.
//
// The default probe is TEXT and goes into every column regardless of its
// declared type: SQLite is dynamically typed, and a non-numeric string keeps its
// TEXT storage class even under REAL affinity. That is what lets one probe cover
// the timestamps, the flag and the strings alike — verified in both directions
// before this guard was written (a column known to be out of the list keeps the
// probe value; one known to be in it does not).
func survivesAStaleWholeRowUpsert(t *testing.T, column string) bool {
	t.Helper()
	d := newTestDAL(t)
	const id = "m-setlist-probe"
	seed := fullMember(id)
	if err := d.PutMember(seed); err != nil {
		t.Fatalf("%s: seed: %v", column, err)
	}
	probe := "oc-t55-sentinel"
	// Not parameterised because a column name cannot be: it is taken from
	// memberColumns, a const in this package, never from anything a caller
	// supplies.
	_, err := d.wdb.Exec(`UPDATE member SET `+column+` = ? WHERE id = ?`, probe, id)
	if err != nil {
		override, ok := probeOverrides[column]
		if !ok {
			t.Fatalf("%s: the probe value was refused (%v), and there is no entry in "+
				"probeOverrides for this column. Add one naming a LEGAL value that "+
				"differs from what fullMember seeds — without it this column cannot "+
				"be probed, and an unprobeable column is an unguarded one.", column, err)
		}
		probe = override
		if _, err := d.wdb.Exec(
			`UPDATE member SET `+column+` = ? WHERE id = ?`, probe, id); err != nil {
			t.Fatalf("%s: probe override %q also refused: %v", column, probe, err)
		}
	}
	if err := d.PutMember(seed); err != nil { // the stale whole-row writer
		t.Fatalf("%s: stale upsert: %v", column, err)
	}
	var got any
	if err := d.rdb.QueryRow(
		`SELECT `+column+` FROM member WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatalf("%s: read back: %v", column, err)
	}
	s, _ := got.(string)
	return s == probe
}
