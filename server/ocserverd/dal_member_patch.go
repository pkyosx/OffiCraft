package main

// dal_member_patch.go — the member table's ONE write door, and the table of
// per-column properties that door reads (T-63, owner ruling rc-0b940a0e12ca).
//
// WHY THIS EXISTS. The member row used to be written one way only: hand
// PutMember a whole Member and it wrote a whole row. Two faces editing two
// different columns at the same time therefore fought over every OTHER column
// as well — the later writer landed the snapshot it had read before the earlier
// one committed, and the earlier edit vanished. Silently: nothing errors, no
// test goes red, the row simply disagrees with what the owner last pressed. The
// damage with a name is spend that was counted and then refunded by a stale
// figure (T-14 項目 6).
//
// The answer up to now was to carve columns OUT of PutMember's conflict clause
// one at a time and give each a single-column setter of its own — banked_cost,
// handover_noticed_ts, agent_iat_floor, the five last_op* columns, the four
// owner-intent columns. That works, and it does not scale: every carve-out is a
// new function, a new place to forget the SSE delta, and a new comment
// explaining the same mechanism again.
//
// PatchMember below is the general form of all of them: name the columns you
// are changing, and NOTHING ELSE is in the statement. A column you did not name
// cannot be clobbered by you, because your value for it never reaches SQL.
//
// PutMember keeps its whole-row shape (52 callers still hand it a whole Member)
// and becomes a SHELL over this door that names every column — so today's
// behaviour is unchanged to the byte, and a caller that wants patch semantics
// simply names fewer fields.

import "strings"

// memberField is one column-and-value pair on its way to the member table,
// carrying the column's own properties rather than leaving them to whoever
// writes the SQL.
//
// The two properties are ORTHOGONAL and both matter:
//
//   - insertOnly says a WHOLE-ROW writer must not carry this column. It is the
//     property every earlier carve-out established by hand: the column belongs
//     to a single-column setter, and a snapshot writer landing its stale value
//     here is the bug those carve-outs were for. It does NOT mean the column is
//     unwritable — a targeted patch that NAMES the column writes it, which is
//     exactly what SetMemberModel and its siblings do.
//
//   - forwardOnly says the column only ever MOVES FORWARD: the update becomes
//     max(col, ?), so a writer holding a stale (or zero) value cannot walk it
//     back. It is the owner's answer for 「只能往前的欄位」 (rc-78cb22a6de94),
//     and it is a property of the COLUMN, declared once beside it — adding
//     another such column is one more constructor with forwardOnly:true, not an
//     if-branch someone has to remember to extend.
//
//     🔴 ADDING forwardOnly TO A COLUMN IS NOT COMPLETE ON ITS OWN. Every
//     single-column setter that writes that column must ALSO go through
//     PatchMember, or it keeps its own `col = ?` and walks the value backwards
//     while the whole-row door holds it — one property, two representations,
//     and nothing goes red. That is precisely what SetMemberForcedStopAt did
//     before T-63. Converge the setters first, then declare the flag.
type memberField struct {
	col         string
	val         any
	insertOnly  bool
	forwardOnly bool
}

// The per-column constructors. This block IS the property table: everything the
// writer needs to know about a column is on its own line here, and nowhere else.
//
// ⚠️ The three NULL rules live here too, in the three constructors that need
// them, rather than at one caller's top. They were PutMember's prologue while
// PutMember was the only door; a second door would have had to remember to
// repeat them, and the codename one going missing is not a visible failure —
// it is the partial UNIQUE codename index colliding across the many
// codename-less staff rows.

func mfID(v string) memberField { return memberField{col: "id", val: v, insertOnly: true} }

func mfName(v string) memberField        { return memberField{col: "name", val: v} }
func mfKind(v string) memberField        { return memberField{col: "kind", val: v} }
func mfRoleKey(v string) memberField     { return memberField{col: "role_key", val: v} }
func mfActualModel(v string) memberField { return memberField{col: "actual_model", val: v} }

func mfActualRuntime(v string) memberField { return memberField{col: "actual_runtime", val: v} }
func mfActualEffort(v string) memberField  { return memberField{col: "actual_effort", val: v} }
func mfDesiredState(v string) memberField  { return memberField{col: "desired_state", val: v} }
func mfLastMachineID(v string) memberField { return memberField{col: "last_machine_id", val: v} }

func mfSessionBootTS(v float64) memberField { return memberField{col: "session_boot_ts", val: v} }
func mfWakingSince(v float64) memberField   { return memberField{col: "waking_since", val: v} }

// mfStoppingSince / mfStoppedSince / mfRefocusSince / mfRefocusOp — the four
// WIND-DOWN ANCHORS (T-55 batch C), which together date one rung of the
// 下線 → 加速 → 強制 ladder and the 換手 epoch riding on it. They are
// insert-only for the ordinary reason at unusual scale: THREE families of writer
// move them and none share a lock — the owner verbs (deactivate /
// accelerated-stop / force-stop / refocus), the agent reporting on ITSELF
// (report_stopping / report_stopped / report_waking), and the reconcile tick's
// recycle passes. Each reaches this door holding a snapshot read before the
// others landed. While the four rode a whole-row writer's update, an owner face
// that touched something else entirely put its pre-stop snapshot back over an
// anchor the agent had just written — stopped_since to 0 — and the collect that
// keys on it never fired, so the member sat in a wind-down that had in fact
// already finished. Nothing goes red; the ladder simply disagrees with what
// happened.
//
// 🔴 ONE WRITER FOR FOUR COLUMNS, for SetMemberOpReceipt's reason:
// armRefocusEpoch writes all four in one breath and the readers take them
// together (StopIntent is stopping_since > 0; the ladder gate compares the
// refocus epoch against the stop anchors), so any moment with some landed and
// some not is a rung nobody stood on. SetMemberWindDownAnchors is the only
// writer that moves them; the INSERT still carries all four so a new row is born
// on the rung it was created with.
// Guarded by TestPutMemberNeverOverwritesSingleColumnOwnedFields: clearing
// insertOnly on any of these four turns it red NAMING the column.
func mfStoppingSince(v float64) memberField {
	return memberField{col: "stopping_since", val: v, insertOnly: true}
}
func mfStoppedSince(v float64) memberField {
	return memberField{col: "stopped_since", val: v, insertOnly: true}
}
func mfRefocusSince(v float64) memberField {
	return memberField{col: "refocus_since", val: v, insertOnly: true}
}
func mfRefocusOp(v string) memberField {
	return memberField{col: "refocus_op", val: v, insertOnly: true}
}
func mfRosterStatus(v string) memberField { return memberField{col: "roster_status", val: v} }

func mfCreatedTS(v float64) memberField   { return memberField{col: "created_ts", val: v} }
func mfReleasedTS(v float64) memberField  { return memberField{col: "released_ts", val: v} }
func mfActivatedTS(v float64) memberField { return memberField{col: "activated_ts", val: v} }

// mfLinkedTaskID carries the nil-means-unbound rule: a nil pointer stores SQL
// NULL, not the empty string, because "" is a task id nothing can join on.
func mfLinkedTaskID(v *string) memberField {
	var stored any
	if v != nil {
		stored = *v
	}
	return memberField{col: "linked_task_id", val: stored}
}

// mfCodename stores "" as NULL so the partial UNIQUE codename index never trips
// on the many codename-less staff rows (NULLs are mutually distinct in SQLite).
func mfCodename(v string) memberField {
	var stored any
	if v != "" {
		stored = v
	}
	return memberField{col: "codename", val: stored}
}

// mfForcedStopAt — FORWARD-ONLY. It only ever moves forward: a caller holding
// the current row carries the stamp it just set and it lands here; a STALE
// snapshot carries an older value (or 0) and max() keeps what is already stored,
// so the record that a session was cut off survives every other writer — the
// property the avatar pointer and the session anchor each needed their own seam
// for. SetMemberForcedStopAt writes through this same constructor, so the two
// paths cannot say different things about the column.
func mfForcedStopAt(v float64) memberField {
	return memberField{col: "forced_stop_at", val: v, forwardOnly: true}
}

// ── The insert-only block ────────────────────────────────────────────────────
//
// Each column below has a single-column setter that is its only mover. A
// whole-row writer carries it on INSERT (so a new row is born with it) and never
// onto an existing row. These reasons used to live as one comment inside
// PutMember's conflict clause; they are per-column facts, so they live beside
// the column now, and the flag that enforces them is on the same line.

// mfRuntime / mfModel / mfEffort / mfDesiredMachineID are DELIBERATELY absent
// from a whole-row writer's update for the ordinary reason, not a special one
// (T-55): they are OWNER INTENT, edited from faces that hold no common lock, and
// every one of those faces reaches this door carrying a whole snapshot read
// before the others landed. An activate in flight put its own older pin back
// over a relocate that had just moved the member; a 成員設定 save that touched
// only effort restated the model beside it. Nothing goes red either time — the
// row simply disagrees with what the owner last pressed. The INSERT still
// carries all four so a new row is born with the intent it was created with;
// SetMemberDesiredMachineID / SetMemberModel / SetMemberRuntime /
// SetMemberEffort are the only writers that move them.
// Guarded by TestPutMemberNeverOverwritesSingleColumnOwnedFields: clearing
// insertOnly on any of these four turns it red NAMING the column.
func mfRuntime(v string) memberField {
	return memberField{col: "runtime", val: v, insertOnly: true}
}
func mfModel(v string) memberField {
	return memberField{col: "model", val: v, insertOnly: true}
}
func mfEffort(v string) memberField {
	return memberField{col: "effort", val: v, insertOnly: true}
}
func mfDesiredMachineID(v string) memberField {
	return memberField{col: "desired_machine_id", val: v, insertOnly: true}
}

// mfBankedCost — a running TOTAL, and a total is the one thing a whole-row
// writer can never carry safely (T-14 項目 6): every HTTP face that writes a
// member row holds a snapshot read before the banking edge, so landing its stale
// figure would silently REFUND spend the owner already saw — and the worker half
// is worse, because memberFromWorker rebuilds the row from an OutsourceWorker
// read at the same stale moment. The INSERT still carries it so a new row starts
// at whatever it was born with; after that it moves only through a single-column
// writer, and there are TWO of them by design — AddMemberBankedCost accumulates
// (`banked_cost + ?`, in SQL, so two concurrent banking edges cannot lose each
// other's contribution) and ZeroMemberBankedCost resets (`banked_cost = 0`, T-53).
//
// 🔴 THIS SENTENCE SAID "the only writer" AND THAT WAS FALSE — twice, in two
// files, from two different branches. It is the shape that keeps coming back:
// "sole writer" reads like a property of the column, but it is a claim about a
// POPULATION, and a population grows. The reset arrived after the accumulator and
// nothing anywhere went red. Whatever the count is when you read this, do not
// trust it — `grep 'banked_cost' *.go` is the answer, and it is cheap.
func mfBankedCost(v float64) memberField {
	return memberField{col: "banked_cost", val: v, insertOnly: true}
}

// The five last_op* columns are the operation receipt, moved out together so the
// write and its SSE delta cannot drift apart: SetMemberOpReceipt is their sole
// writer and persistMemberOpReceipt is the service-layer pairing.
func mfLastOp(v string) memberField {
	return memberField{col: "last_op", val: v, insertOnly: true}
}

// mfLastOpOK carries the three-valued rule: nil is "no op reported yet" and
// must reach SQL as NULL, distinct from both true and false.
func mfLastOpOK(v *bool) memberField {
	var stored any
	if v != nil {
		stored = *v
	}
	return memberField{col: "last_op_ok", val: stored, insertOnly: true}
}

func mfLastOpLog(v string) memberField {
	return memberField{col: "last_op_log", val: v, insertOnly: true}
}
func mfLastOpReason(v string) memberField {
	return memberField{col: "last_op_reason", val: v, insertOnly: true}
}
func mfLastOpAt(v float64) memberField {
	return memberField{col: "last_op_at", val: v, insertOnly: true}
}

// mfAvatarAttachmentID — insert-only, and its blast radius is WORSE than the
// usual clobber, which is why it gets its own paragraph rather than sharing the
// receipt's. ReplaceMemberAvatar / DeleteMemberAvatar are the only update seams
// for this pointer, and they DELETE the blob they replace in the same
// transaction. So a whole-row writer landing a stale pointer here does not just
// disagree with the cockpit — it restores a pointer to bytes that are already
// gone, orphaning the new blob and leaving a member whose avatar cannot load.
// The INSERT still accepts the field for migrations, tests and new rows.
func mfAvatarAttachmentID(v string) memberField {
	return memberField{col: "avatar_attachment_id", val: v, insertOnly: true}
}

// mfHandoverNoticedTS — session-scoped state cleared at a session boundary
// (T-6ebc), and the boundary runs next to HTTP faces that write member rows from
// snapshots taken before it. Carrying the column onto an existing row would let
// one of those revive a claim that was just cleared, silencing a genuinely new
// session's one notice. The INSERT carries it so a brand-new row starts at its
// zero value; SetMemberHandoverNoticedTS is the only writer that moves it.
// Guarded by TestHandoverNotice_ClaimSurvivesAWholeRowUpsert: clearing
// insertOnly here turns that test red.
func mfHandoverNoticedTS(v float64) memberField {
	return memberField{col: "handover_noticed_ts", val: v, insertOnly: true}
}

// mfAgentIatFloor — insert-only AND forward-only, and that pair is the whole
// reason the two properties are separate booleans (T-14 4B).
//
// It is a REVOCATION floor. report_waking raises it next to HTTP faces holding
// member snapshots taken BEFORE that wake, and any one of them landing here
// would put the superseded generation's credentials back in service — so a
// whole-row writer must not carry it at all (insertOnly). The INSERT carries it
// so a new row starts at 0, no floor, nothing refused; SetMemberAgentIatFloor is
// the only writer that moves it, and it moves it forward only, which is the
// second property (forwardOnly).
//
// Folding the two into one flag would force a choice between recording that the
// column is monotone and recording that snapshot writers must skip it. It is
// both, and the monotonicity has to be declared HERE rather than at its setter:
// the day this column is allowed onto an existing row, the max() must already be
// part of what the column IS, not something the person making that change has to
// remember to add.
func mfAgentIatFloor(v float64) memberField {
	return memberField{col: "agent_iat_floor", val: v, insertOnly: true, forwardOnly: true}
}

// mfRestartAfterStop — 「下線之後要不要起來」 (T-14 項目 7, migrations/00070),
// and the ONE owner-intent column on this table that is deliberately NOT
// insert-only: restart_after_stop IS carried by the whole-row upsert.
//
// It is written by the same HTTP faces that write desired_state, in the same
// putMember, and the two have to land together: a 下線 that cleared the flag and
// a 重啟 that set it must never be split across two writes, or the member comes
// back up after a 停止 (or stays down after a 重啟). Carving it out would buy
// snapshot safety at the price of that pairing, which is the more expensive of
// the two.
//
// 🔴 THAT MAKES ITS ABSENCE FROM THE INSERT-ONLY SET TEMPORARY, AND THE DEBT IS
// OWED TO T-55 批次D. desired_state is itself owner intent and is scheduled to
// leave the whole-row writer for a single-column setter. The moment it does, the
// pair this comment relies on is broken: desired_state lands through its own
// statement while restart_after_stop still rides the whole-row write — exactly
// the two-write split described above, and NOTHING GOES RED, because each half
// is individually correct. So 批次D must move this column out WITH desired_state
// and in the SAME write (one statement setting both, not one single-column
// writer each), or a 停止 whose flag-clear is carried by a stale snapshot brings
// the member back up, and a 重啟 whose flag-set loses the same race leaves it
// down.
func mfRestartAfterStop(v bool) memberField {
	return memberField{col: "restart_after_stop", val: v}
}

// memberWholeRow projects a Member onto the full column list.
//
// The order below follows memberColumns for readability only. It is NOT
// load-bearing: insertMemberRowIfAbsent emits each column NAME next to its own
// placeholder, so nothing binds positionally and reordering this slice changes
// no SQL. What IS load-bearing is the SET of columns, and
// TestMemberColumnPropertiesAreDeclaredInOnePlace compares it against
// memberColumns directly — a column added to one and not the other goes red.
func memberWholeRow(m Member) []memberField {
	return []memberField{
		mfID(m.ID), mfName(m.Name), mfKind(m.Kind), mfRoleKey(m.RoleKey),
		mfRuntime(m.Runtime), mfModel(m.Model), mfActualModel(m.ActualModel),
		mfEffort(m.Effort),
		mfActualRuntime(m.ActualRuntime), mfActualEffort(m.ActualEffort),
		mfDesiredState(m.DesiredState), mfDesiredMachineID(m.DesiredMachineID),
		mfLastMachineID(m.LastMachineID), mfSessionBootTS(m.SessionBootTS),
		mfWakingSince(m.WakingSince), mfStoppingSince(m.StoppingSince),
		mfStoppedSince(m.StoppedSince), mfRefocusSince(m.RefocusSince),
		mfRefocusOp(m.RefocusOp), mfBankedCost(m.BankedCost),
		mfLastOp(m.LastOp), mfLastOpOK(m.LastOpOK), mfLastOpLog(m.LastOpLog),
		mfLastOpReason(m.LastOpReason), mfLastOpAt(m.LastOpAt),
		mfRosterStatus(m.RosterStatus),
		mfLinkedTaskID(m.LinkedTaskID), mfCodename(m.Codename),
		mfCreatedTS(m.CreatedTS), mfReleasedTS(m.ReleasedTS),
		mfActivatedTS(m.ActivatedTS),
		mfAvatarAttachmentID(m.AvatarAttachmentID), mfForcedStopAt(m.ForcedStopAt),
		mfHandoverNoticedTS(m.HandoverNoticedTS), mfAgentIatFloor(m.AgentIatFloor),
		mfRestartAfterStop(m.RestartAfterStop),
	}
}

// updatableMemberFields drops the insert-only columns — the set a whole-row
// writer is allowed to carry onto an EXISTING row. This is the one place the
// conflict clause's membership is decided, so a column joins or leaves it by
// its constructor's insertOnly flag and by nothing else.
func updatableMemberFields(fields []memberField) []memberField {
	out := make([]memberField, 0, len(fields))
	for _, f := range fields {
		if !f.insertOnly {
			out = append(out, f)
		}
	}
	return out
}

// PatchMember writes EXACTLY the named columns of one member row and leaves
// every other column alone — the door T-55 asked for. A column you did not name
// does not appear in the statement at all, so no snapshot you happen to be
// holding can land on it.
//
// It is an UPDATE, deliberately NOT an upsert. A patch that could create a row
// would let a caller naming two columns mint a member whose other 33 columns are
// whatever the schema defaults to; row CREATION stays with PutMember, which
// carries the whole row. An id that names no row is a clean no-op (0 rows
// affected, no error) — the same answer every single-column setter beside it
// gives, and the same answer callers already rely on for a member dismissed
// between their read and their write.
//
// It fans NO SSE delta. That is the split publishMemberPatch documents and it
// is load-bearing here: PutOutsourceWorker reaches this door too, and an
// outsource row's changes travel on the outsource_worker projection, so a delta
// bound to the write itself would push a member frame that path has never sent.
// The service layer decides who publishes; the write layer only writes.
//
// 🔑 THAT GUARANTEE IS STRUCTURAL, AND THERE IS DELIBERATELY NO TEST FOR IT.
// The DAL holds no reference to the hub — it cannot publish, so nothing has to
// remember not to. A test asserting "the DAL published nothing" would be
// asserting something the type system already makes unsayable, and a test that
// cannot fail is worse than no test: it reads as a guard while guarding nothing.
// If a future change gives the DAL a hub, THAT is when this property needs a
// test, and this paragraph is the notice that it stopped being free.
func (d *DAL) PatchMember(id string, fields ...memberField) error {
	return patchMemberOn(d.wdb, id, fields...)
}

func patchMemberOn(ex sqlExecer, id string, fields ...memberField) error {
	if len(fields) == 0 {
		return nil
	}
	clauses := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)+1)
	for _, f := range fields {
		if f.forwardOnly {
			// max(col, ?) — the column only ever moves forward, so a stale or
			// zero value from a snapshot writer is dropped rather than landed.
			clauses = append(clauses, f.col+" = max("+f.col+", ?)")
		} else {
			clauses = append(clauses, f.col+" = ?")
		}
		args = append(args, f.val)
	}
	args = append(args, id)
	_, err := ex.Exec(
		`UPDATE member SET `+strings.Join(clauses, ", ")+` WHERE id = ?`, args...)
	return err
}

// insertMemberRowIfAbsent is PutMember's creation half: it lands the WHOLE row
// the caller handed over when no row with that id exists yet, and does nothing
// at all when one does. Every column is carried here — including the insert-only
// ones — because a member is born with the model, runtime, effort and machine
// the owner hired it with, and those columns' setters only move them afterwards.
func insertMemberRowIfAbsent(ex sqlExecer, fields []memberField) error {
	cols := make([]string, 0, len(fields))
	holes := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields))
	for _, f := range fields {
		cols = append(cols, f.col)
		holes = append(holes, "?")
		args = append(args, f.val)
	}
	_, err := ex.Exec(
		`INSERT INTO member (`+strings.Join(cols, ", ")+`)
		 VALUES (`+strings.Join(holes, ", ")+`)
		 ON CONFLICT (id) DO NOTHING`, args...)
	return err
}
