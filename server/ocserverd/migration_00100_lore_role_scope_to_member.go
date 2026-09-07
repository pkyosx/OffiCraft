package main

// migration_00100_lore_role_scope_to_member.go — T-33: the 傳承 scopes collapse
// from three to two. Every `lore_entry` row at scope_kind='role' is rekeyed onto
// the ONE member sitting under that role, so that after this runs the writable
// vocabulary is {agent, manual} and nothing addresses a role any more.
//
// Owner, 2026-09-07, card rc-a43100fd0486 圈 [0]:
//
//	「應該已經沒有角色傳承」「只有成員跟任務傳承兩種」
//	選項 [0]：拿掉角色傳承，現有九筆全部搬到「那個角色底下的那一位成員」底下
//
// ── THE NUMBER ───────────────────────────────────────────────────────────────
//
// 00100, not "main's highest + 1" (= 00089) and not "this branch's highest + 1"
// (= 00094). A migration number has to be unique across every branch that will
// EVER merge, not across the one in hand. Scanned at the time of writing, both
// sources that 00093's header names:
//
//	· all 458 refs/heads on origin, each confirmed present AND at the same sha
//	  as its refs/remotes/origin/* mirror (so `git log --all` really does reach
//	  every one of them — a stale tracking ref would have made this scan lie);
//	· every path ever added OR touched under server/ocserverd/migrations/, plus
//	  every `migration_NNNNN_*.go` — the Go family, which does not live under
//	  migrations/ because that directory is embedded as *.sql.
//
// Highest taken anywhere: 00099 (migrations/00099_placeholder_t80_member_key_id.sql,
// on a branch that has not landed). 00094–00098 are free and 00100 is free; this
// takes 00100 to leave the 0009x band alone for whatever 00099's branch is doing.
// As with 00088 and 00093 before it: that is only true AS OF THAT SCAN. Re-scan
// BOTH sources before this lands.
//
// ── WHY THIS IS A REKEY AND NOT A LOSS ───────────────────────────────────────
//
// Staff are one-to-one with their role in the roster as it stands (owner,
// c-712174eb0720), so a role_key and that member's id name the SAME reader and
// the SAME boot document. The fold that reads these rows is the same function
// (selectLoreForScope) called with a different scope, and the budget is the same
// knob (loreRoleCap). Nothing about what any live reader receives changes.
//
// ⚠️ WHAT IS BEING GIVEN UP, RECORDED BECAUSE IT IS NOT FREE. If two members are
// ever placed under one role they will no longer share a 傳承 — each learns its
// own. The owner was shown that sentence before he chose [0]. And it is worth
// being exact about the standing of "one member per role": it is a property of
// today's roster, NOT an invariant. `member.role_key` carries no UNIQUE index
// (the only unique index on that table is idx_member_codename), and the hire face
// does not check whether a role_key is already taken. This migration therefore
// cannot assume the one-to-one; it has to MEASURE it, row group by row group,
// which is what the rule below does.
//
// ── THE RULE, AND WHY IT REFUSES RATHER THAN GUESSES ─────────────────────────
//
// Per distinct role_key carried by role-scoped entries:
//
//	exactly one member with roster_status='active' ⇒ rekey onto that member's id.
//	anything else (zero, or two or more)           ⇒ LEAVE THE ROWS ALONE.
//
// The owner's words were 「那個角色底下的那一位成員」. That phrase has a referent
// when there is exactly one such member and has none otherwise, so the migration
// acts exactly where the instruction is defined and stops where it is not.
//
// 🔴 LEAVING A FINDABLE ORPHAN BEATS SILENTLY PICKING SOMEONE. Getting it wrong
// is irreversible in practice — a 傳承 entry has NO edit path, so an entry filed
// under the wrong member cannot be moved back by any face this station exposes,
// and it would begin riding a stranger's boot document at every boot with nothing
// anywhere reporting it. An orphan is the cheap failure: the row is still there,
// still readable, still curatable, and can be placed by hand once someone decides
// who it belongs to.
//
// 🔴 REMOVED MEMBERS DO NOT COUNT, AND THAT IS WHAT MAKES THE ZERO CASE REAL.
// Dismissal is a SOFT delete — HandleDismissMemberApiMembersMemberIdDelete only
// sets roster_status='removed' and the row stays forever — so a role whose member
// was replaced carries two or more member rows. Counting all of them would make
// the common "we swapped who does this job" history look like an ambiguity and
// strand entries that have an obvious owner. Counting only 'active' is what makes
// the ordinary case land. The cost is that a role whose member has LEFT and not
// been replaced counts zero, and that falls to the leave-alone arm rather than
// being filed under someone who is gone.
//
// 🔴 THE ORPHANS ARE PRINTED, NOT MERELY LEFT. A row this migration declined to
// move is invisible unless something says so: the cockpit's unfiltered page will
// show it, but nobody re-reads a page they have no reason to suspect. So every
// group left behind is written to stderr with its role_key, its row count and
// WHICH arm it fell down (zero active, or N active) — and a run that moved
// everything says so too, so "no orphan lines" can be told apart from "the report
// did not run". This is the whole reason this is a Go migration rather than the
// .sql file the change would otherwise fit in: goose executes .sql statements but
// discards what a SELECT returns, so a pure-SQL version could compute this number
// and would have nowhere to put it.
//
// 🔴 THE DB CHECK IS NOT NARROWED, ON PURPOSE. migrations/00093 admits
// ('role','agent','manual') and this migration leaves that CHECK exactly as it
// is. Tightening it to two values would require a table rebuild that any orphan
// row would then FAIL — turning "we could not tell whose this is" into "this row
// may not exist", which is the silent deletion the rule above was written to
// avoid. The Go vocabulary is the two live scopes (domain.go); the schema stays
// wide enough to hold what was preserved.
//
// 🔴 IDEMPOTENT BY SHAPE, NOT BY A FLAG. Up only ever reads rows at
// scope_kind='role' and only ever writes them to 'agent'. A second run finds the
// migrated rows are no longer 'role' and cannot touch them again; it re-examines
// only the orphans, which is the correct behaviour rather than a leak — if
// somebody has since hired the missing member, a replay places them. No sentinel
// table, no "already done" row: the predicate IS the guard, so there is nothing
// that can get out of step with the data it describes.
//
// 🔴 updated_ts IS NOT TOUCHED. This is a rekey by the system, not an edit by a
// person. Stamping it would make every migrated entry read as recently modified
// on a page whose whole job is showing when things were written, and there is no
// second column to recover the real value from. effective_ts is likewise left
// alone — moving it would silently reorder the fold.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"

	"github.com/pressly/goose/v3"
)

// loreScopeMigrationLog is where the two reports below are written.
//
// 🔴 IT IS A VARIABLE SO THE REPORT CAN BE ASSERTED ON, and that is not test
// convenience — it is the only way the report is protected. The orphan lines are
// the entire mechanism by which "this migration declined to move N entries"
// stops being invisible; a report nothing tests is a report the next person
// deletes as noise while every test stays green, and the orphans then go quiet
// on every station this ships to. Production behaviour is unchanged: it is
// os.Stderr, which is where goose's own output goes.
var loreScopeMigrationLog io.Writer = os.Stderr

func init() {
	goose.AddNamedMigrationContext("00100_lore_role_scope_to_member.go",
		upLoreRoleScopeToMember, downLoreRoleScopeToMember)
}

// loreLegacyRoleScope is the value being retired. It is a LITERAL here and
// deliberately not a reference to a Go constant: domain.go no longer has one
// (that is the point of this migration), and a migration must keep naming the
// world as it was when it ran even after the code has moved on.
const loreLegacyRoleScope = "role"

// upLoreRoleScopeToMember rekeys role-scoped 傳承 onto members, then reports
// whatever it declined to move.
func upLoreRoleScopeToMember(ctx context.Context, tx *sql.Tx) error {
	// The groups, BEFORE anything is written. Reading first means the report
	// below can say what each group's fate was; deciding and reporting from the
	// same snapshot is what stops the two disagreeing.
	type group struct {
		roleKey string
		rows    int
		active  int
		memberI string // the single active member's id, when active == 1
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT l.scope_key,
		       COUNT(*),
		       (SELECT COUNT(*) FROM member m
		         WHERE m.role_key = l.scope_key AND m.roster_status = 'active'),
		       COALESCE((SELECT m.id FROM member m
		                  WHERE m.role_key = l.scope_key AND m.roster_status = 'active'
		                  ORDER BY m.id LIMIT 1), '')
		  FROM lore_entry l
		 WHERE l.scope_kind = ?
		 GROUP BY l.scope_key
		 ORDER BY l.scope_key`, loreLegacyRoleScope)
	if err != nil {
		return fmt.Errorf("migration 00100: read role-scoped groups: %w", err)
	}
	var groups []group
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.roleKey, &g.rows, &g.active, &g.memberI); err != nil {
			rows.Close()
			return fmt.Errorf("migration 00100: scan group: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("migration 00100: iterate groups: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("migration 00100: close groups: %w", err)
	}

	moved, movedGroups, orphanRows, orphanGroups := 0, 0, 0, 0
	for _, g := range groups {
		// 🔴 THE TEST IS `== 1`, NOT `>= 1`. `>= 1` with the ORDER BY above
		// would quietly turn "two candidates" into "the alphabetically first
		// one", which is a guess wearing a deterministic tie-break. The id is
		// selected with LIMIT 1 only so the count and the id come from one
		// query; it is USED only on the branch where the count proves it unique.
		if g.active != 1 {
			orphanRows += g.rows
			orphanGroups++
			why := fmt.Sprintf("%d members are active under it", g.active)
			if g.active == 0 {
				why = "no member is active under it " +
					"(the role may have been deleted, or its member dismissed and not replaced)"
			}
			fmt.Fprintf(loreScopeMigrationLog,
				"[migration 00100] LEFT AS-IS: %d 傳承 entr%s at scope_kind='role' scope_key=%q — %s. "+
					"They are NOT deleted and NOT reassigned; they stay readable on the 傳承 page's "+
					"unfiltered view and can be placed by hand once their owner is decided.\n",
				g.rows, plural(g.rows, "y", "ies"), g.roleKey, why)
			continue
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE lore_entry
			   SET scope_kind = 'agent', scope_key = ?
			 WHERE scope_kind = ? AND scope_key = ?`,
			g.memberI, loreLegacyRoleScope, g.roleKey)
		if err != nil {
			return fmt.Errorf("migration 00100: rekey role %q onto member %q: %w",
				g.roleKey, g.memberI, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("migration 00100: rows affected for role %q: %w", g.roleKey, err)
		}
		moved += int(n)
		movedGroups++
	}

	// 🔴 THE SUMMARY IS UNCONDITIONAL. A run with nothing to do still prints,
	// so a silent log means "this migration did not run", not "there was
	// nothing wrong" — those two are indistinguishable if the report is
	// conditional, and the second is the one people assume.
	fmt.Fprintf(loreScopeMigrationLog,
		"[migration 00100] 傳承 scope collapse: moved %d entr%s across %d role(s) onto their one active member; "+
			"left %d entr%s across %d role(s) in place as orphans.\n",
		moved, plural(moved, "y", "ies"), movedGroups,
		orphanRows, plural(orphanRows, "y", "ies"), orphanGroups)
	return nil
}

// downLoreRoleScopeToMember is the retreat: member-keyed 傳承 whose member
// carries a role_key goes back to being role-keyed.
//
// 🔴 IT IS A GENUINE RETREAT, NOT AN EXACT INVERSE, AND THE DIFFERENCE IS
// STATED RATHER THAN GLOSSED. What it restores is the state the OLD BINARY
// expects, which is not quite the state that existed before Up:
//
//   - A member with a non-empty role_key goes back to that role. That covers
//     every row Up moved.
//   - It ALSO covers any agent-scoped entry a STAFF member wrote AFTER Up ran.
//     That is deliberate and is the correct target state: the old binary filed
//     staff writes under the role, so leaving those at 'agent' would hide them
//     from the very fold the rollback is restoring.
//   - Outsource members carry no role_key, so their entries — which were 'agent'
//     before Up and are untouched by it — stay 'agent'. They must: 'role' has
//     nothing to name for a member with no role.
//   - A row whose member id is no longer on the roster at all cannot be reversed
//     (there is nothing to read a role_key from) and stays 'agent'. The old
//     binary reads such a row as an outsource entry, which is wrong but is
//     inert: it rides no boot document, because no live member has that id.
//
// The orphans Up refused to move are already at 'role' and are matched by
// nothing here, so a down/up cycle leaves them exactly where they were.
func downLoreRoleScopeToMember(ctx context.Context, tx *sql.Tx) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE lore_entry
		   SET scope_kind = ?,
		       scope_key  = (SELECT m.role_key FROM member m WHERE m.id = lore_entry.scope_key)
		 WHERE scope_kind = 'agent'
		   AND EXISTS (SELECT 1 FROM member m
		                WHERE m.id = lore_entry.scope_key AND m.role_key <> '')`,
		loreLegacyRoleScope)
	if err != nil {
		return fmt.Errorf("migration 00100 down: rekey members back onto roles: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("migration 00100 down: rows affected: %w", err)
	}
	fmt.Fprintf(loreScopeMigrationLog,
		"[migration 00100] DOWN: returned %d 傳承 entr%s to scope_kind='role'. "+
			"Entries whose member carries no role_key (outsource) and entries whose member "+
			"is no longer on the roster were left at 'agent'.\n",
		n, plural(int(n), "y", "ies"))
	return nil
}

// plural picks a suffix. It exists so the two reports above read as sentences —
// a log line that says "1 entries" is a line people stop trusting the rest of.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
