package main

// The member table's ONE write door (owner ruling rc-0b940a0e12ca). A whole-row
// write lands the snapshot its caller read earlier over every column it did not
// mean to change, so two faces editing different columns silently undo each
// other (e.g. banked spend refunded by a stale figure, or a dismissed member put
// back on the roster). An existing row is only ever PATCHED: PatchMember names
// its columns, writeMemberChangesOn derives them from what the caller changed.
// CreateMember only creates rows.

import (
	"fmt"
	"strings"
)

// Two orthogonal properties:
//   - insertOnly: only row creation and a patch that NAMES the column write it
//     (that is how the single-column setters work). writeMemberChangesOn never
//     derives it from a diff, even when the caller's copy differs: these columns
//     have writers that accumulate in SQL or move as a group, and a diffed
//     absolute value would bypass both.
//   - forwardOnly: the update becomes max(col, ?), so a stale or zero value
//     cannot walk it back (owner ruling rc-78cb22a6de94). Declaring it is not
//     enough on its own: every single-column setter of that column must also go
//     through PatchMember, or its own `col = ?` still walks it back.
type memberField struct {
	col         string
	val         any
	insertOnly  bool
	forwardOnly bool
}

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

// Wind-down anchors: insert-only because three lock-free writer families move
// them (owner verbs, the agent's own report_*, the reconcile recycle passes);
// a whole-row writer once put stopped_since back to 0 and the member sat in a
// wind-down that had already finished. SetMemberWindDownAnchors moves all four in
// one write — readers take them together, so a half-landed set is a rung nobody
// stood on.
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

func mfLinkedTaskID(v *string) memberField {
	var stored any
	if v != nil {
		stored = *v
	}
	return memberField{col: "linked_task_id", val: stored}
}

// "" is stored as NULL so the partial UNIQUE codename index never trips on the
// many codename-less staff rows (NULLs are mutually distinct in SQLite).
func mfCodename(v string) memberField {
	var stored any
	if v != "" {
		stored = v
	}
	return memberField{col: "codename", val: stored}
}

func mfForcedStopAt(v float64) memberField {
	return memberField{col: "forced_stop_at", val: v, forwardOnly: true}
}

// Owner intent, edited from faces that share no lock: a whole-row writer once
// put an older machine pin back over a just-landed relocate, and a save that
// touched only effort restated the model beside it.
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

// A running TOTAL: a whole-row writer's stale figure would silently REFUND spend
// the owner already saw. Accumulation happens in SQL (`banked_cost + ?`) so two
// concurrent banking edges cannot lose each other's contribution. "Sole writer"
// claims about this column have been false twice — grep 'banked_cost' instead.
func mfBankedCost(v float64) memberField {
	return memberField{col: "banked_cost", val: v, insertOnly: true}
}

// The five last_op* columns move together so the write and its SSE delta cannot
// drift apart.
func mfLastOp(v string) memberField {
	return memberField{col: "last_op", val: v, insertOnly: true}
}

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

// The avatar seams DELETE the blob they replace in the same transaction, so a
// stale pointer landed here points at bytes already gone — the new blob is
// orphaned and the avatar cannot load.
func mfAvatarAttachmentID(v string) memberField {
	return memberField{col: "avatar_attachment_id", val: v, insertOnly: true}
}

// Cleared at a session boundary next to HTTP faces writing older snapshots;
// carrying it onto an existing row would revive a just-cleared claim and
// silence a genuinely new session's one notice.
func mfHandoverNoticedTS(v float64) memberField {
	return memberField{col: "handover_noticed_ts", val: v, insertOnly: true}
}

func mfAgentIatFloor(v float64) memberField {
	return memberField{col: "agent_iat_floor", val: v, insertOnly: true, forwardOnly: true}
}

// migrations/00080. Insert-only because memberFromWorker rebuilds a Member from
// zero and would send "", and "" means "never observed" — a fact the owner reads
// to decide on key removal. NOT forwardOnly: key ids have no order.
func mfTokenKeyID(v string) memberField {
	return memberField{col: "token_key_id", val: v, insertOnly: true}
}

// migrations/00070. Deliberately NOT insert-only: it must land in the same write
// as desired_state, or a stop comes back up (or a restart stays down). If
// desired_state ever becomes insert-only, this column must move WITH it, in one
// statement setting both — each half alone looks correct and nothing goes red.
func mfRestartAfterStop(v bool) memberField {
	return memberField{col: "restart_after_stop", val: v}
}

// Order is not load-bearing (each column name sits beside its placeholder). The
// SET is: it must match memberColumns (the SELECT list), or a value is written
// and never read back, or read off a row never told to carry it — silently.
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
		mfRestartAfterStop(m.RestartAfterStop), mfTokenKeyID(m.TokenKeyID),
	}
}

// changedMemberFields compares PROJECTED values, not struct fields: the
// constructors map "" and nil to NULL, so a raw comparison would see edits that
// store nothing new.
func changedMemberFields(before, after Member) []memberField {
	b, a := memberWholeRow(before), memberWholeRow(after)
	out := make([]memberField, 0, len(a))
	for i := range a {
		if a[i].insertOnly || a[i].val == b[i].val {
			continue
		}
		out = append(out, a[i])
	}
	return out
}

// writeMemberChangesOn lands on an existing row exactly the columns that differ
// between before (the row as the caller read it) and after (the row it wants).
// A column the caller did not touch is not written, so a snapshot read earlier
// cannot restore it over another writer's newer value.
func writeMemberChangesOn(ex sqlExecer, before, after Member) error {
	if before.ID != after.ID {
		return fmt.Errorf("member write: before %q and after %q are different rows", before.ID, after.ID)
	}
	return patchMemberOn(ex, after.ID, changedMemberFields(before, after)...)
}

// An UPDATE, deliberately not an upsert: row creation stays with CreateMember. It
// fans NO SSE delta — the service layer decides when to publish. That is
// structural (the DAL holds no hub), so there is deliberately no test; if the DAL
// ever gets a hub, it needs one.
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

func insertMemberRow(ex sqlExecer, fields []memberField) error {
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
		 VALUES (`+strings.Join(holes, ", ")+`)`, args...)
	return err
}
