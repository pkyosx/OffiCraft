package main

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestGooseUpOnFreshSQLite proves the whole migration base end to end: the
// embedded migrations apply (goose up) onto a FRESH throwaway SQLite database
// via the cgo-free modernc driver, and the goose version bookkeeping lands.
func TestGooseUpOnFreshSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "ocserverd-test.db") // nested: exercises the mkdir
	db, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	// Every schema table + goose's own version table must exist.
	for _, table := range []string{
		"member", "chat_message", "chat_attachment", "chat_read",
		"user_context", "role_def", "lessons",
		"account_alias", "machine_alias",
		"setting", "reply_card",
		"goose_db_version",
	} {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing after goose up: %v", table, err)
		}
	}

	// goose up must be idempotent (a second run applies nothing, errors nothing).
	if err := runMigrations(db); err != nil {
		t.Fatalf("second goose up must be a no-op: %v", err)
	}
}

// TestStepSupersededMigrationRoundTrip pins the 00016 rebuild (T-1aea): Up
// drops the task_step status CHECK (the five-state closed set lives in code —
// domain.ValidStepStatus) while preserving every pre-existing row; Down
// restores the four-state CHECK and maps superseded rows to 'done' (the
// closest legacy terminal — the row is kept, never deleted audit trail).
func TestStepSupersededMigrationRoundTrip(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "superseded-mig.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	// The CHECK is gone: a superseded row inserts cleanly post-00016.
	for _, row := range []struct{ id, status string }{
		{"ts-pend", "pending"},
		{"ts-done", "done"},
		{"ts-sup", "superseded"},
	} {
		if _, err := db.Exec(`INSERT INTO task_step
			(id, task_id, order_idx, name, status, finished_ts)
			VALUES (?, 't-1', 0, 'n', ?, 9)`, row.id, row.status); err != nil {
			t.Fatalf("seed %s: %v", row.id, err)
		}
	}

	// Down one step (00016 → 00015): superseded squashes to done, the CHECK
	// returns, every other row survives untouched.
	if err := goose.DownTo(db, "migrations", 15); err != nil {
		t.Fatalf("goose down to 15: %v", err)
	}
	var status string
	if err := db.QueryRow(
		`SELECT status FROM task_step WHERE id='ts-sup'`).Scan(&status); err != nil {
		t.Fatalf("ts-sup after down: %v", err)
	}
	if status != "done" {
		t.Fatalf("down must squash superseded→done, got %q", status)
	}
	if err := db.QueryRow(
		`SELECT status FROM task_step WHERE id='ts-pend'`).Scan(&status); err != nil ||
		status != "pending" {
		t.Fatalf("ts-pend must survive the down rebuild: %q %v", status, err)
	}
	if _, err := db.Exec(`INSERT INTO task_step (id, task_id, order_idx, status)
		VALUES ('ts-bad', 't-1', 9, 'superseded')`); err == nil {
		t.Fatalf("down must restore the four-state CHECK (superseded rejected)")
	}

	// Up again: superseded is legal once more, the index survives, and the
	// run is idempotent thereafter.
	if err := runMigrations(db); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if _, err := db.Exec(`UPDATE task_step SET status='superseded'
		WHERE id='ts-pend'`); err != nil {
		t.Fatalf("post re-up superseded write must pass: %v", err)
	}
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM task_step`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("re-up must preserve all 3 rows, got %d %v", n, err)
	}
	if err := db.QueryRow(`SELECT name FROM sqlite_master
		WHERE type='index' AND name='idx_task_step_task'`).Scan(&status); err != nil {
		t.Fatalf("idx_task_step_task must survive the rebuild: %v", err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
}

// TestReplyCardExpiredMigrationRoundTrip pins the 00013 rebuild: Up preserves
// every pre-existing reply_card row (the CHECK-dropping create/copy/swap loses
// nothing) and adds expired_ts; Down rebuilds the two-state CHECK and squashes
// expired rows back to 'waiting' (never answered — the honest legacy reading).
func TestReplyCardExpiredMigrationRoundTrip(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "expired-mig.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	// Seed one card per status through the post-00013 schema.
	for _, row := range []struct {
		id, status        string
		answered, expired float64
	}{
		{"rc-wait", "waiting", 0, 0},
		{"rc-ans", "answered", 5, 0},
		{"rc-exp", "expired", 0, 7},
	} {
		if _, err := db.Exec(`INSERT INTO reply_card
			(id, kind, status, created_ts, answered_ts, expired_ts, options, summary)
			VALUES (?, 'decision', ?, 1, ?, ?, '["A"]', 's')`,
			row.id, row.status, row.answered, row.expired); err != nil {
			t.Fatalf("seed %s: %v", row.id, err)
		}
	}

	// Down one step (00013 → 00012): expired squashes to waiting, the column
	// goes away, everything else survives byte-for-byte.
	if err := goose.DownTo(db, "migrations", 12); err != nil {
		t.Fatalf("goose down to 12: %v", err)
	}
	var status string
	if err := db.QueryRow(
		`SELECT status FROM reply_card WHERE id='rc-exp'`).Scan(&status); err != nil {
		t.Fatalf("rc-exp after down: %v", err)
	}
	if status != "waiting" {
		t.Fatalf("down must squash expired→waiting, got %q", status)
	}
	if err := db.QueryRow(
		`SELECT status FROM reply_card WHERE id='rc-ans'`).Scan(&status); err != nil ||
		status != "answered" {
		t.Fatalf("rc-ans must survive the down rebuild: %q %v", status, err)
	}
	if _, err := db.Exec(`SELECT expired_ts FROM reply_card`); err == nil {
		t.Fatalf("down must drop the expired_ts column")
	}

	// Up again: the column returns (0.0 default), rows survive, and the run is
	// idempotent thereafter.
	if err := runMigrations(db); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM reply_card WHERE expired_ts = 0.0`).Scan(&n); err != nil {
		t.Fatalf("count after re-up: %v", err)
	}
	if n != 3 {
		t.Fatalf("re-up must preserve all 3 rows with expired_ts=0.0, got %d", n)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
}

// TestMigrateWorkerBankedCost proves 00021 (banked_cost, REAL — mirrors
// member.banked_cost) is a pure ADDITIVE, reversible column: up stamps it on a
// worker row; down-to-20 drops it (column gone, row + pre-existing columns
// survive); re-up restores it at its 0.0 default with the row intact and is
// idempotent. The 升降升無損 self-verify the column demands.
func TestMigrateWorkerBankedCost(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "wb.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	// 00025 folded outsource_worker away — step back into the 00021..00024
	// window where the table still exists to exercise the historical round trip.
	if err := goose.DownTo(db, "migrations", 21); err != nil {
		t.Fatalf("goose down to 21: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO outsource_worker
		(id, codename, model, effort, task_id, status, created_ts, banked_cost)
		VALUES ('ow-b', 'O-1', 'opus', 'high', 't-1', 'active', 5, 3.25)`); err != nil {
		t.Fatalf("seed worker: %v", err)
	}

	if err := goose.DownTo(db, "migrations", 20); err != nil {
		t.Fatalf("goose down to 20: %v", err)
	}
	if _, err := db.Exec(`SELECT banked_cost FROM outsource_worker`); err == nil {
		t.Fatalf("down must drop the banked_cost column")
	}
	var codename string
	if err := db.QueryRow(
		`SELECT codename FROM outsource_worker WHERE id='ow-b'`).Scan(&codename); err != nil ||
		codename != "O-1" {
		t.Fatalf("row must survive the down (codename=%q err=%v)", codename, err)
	}

	if err := goose.UpTo(db, "migrations", 21); err != nil {
		t.Fatalf("up to 21: %v", err)
	}
	var banked float64
	if err := db.QueryRow(
		`SELECT banked_cost FROM outsource_worker WHERE id='ow-b'`).Scan(&banked); err != nil {
		t.Fatalf("column after re-up: %v", err)
	}
	if banked != 0.0 {
		t.Fatalf("re-up must restore the 0.0 default, got %v", banked)
	}
	// Continue to head: 00025 folds the seeded worker into member.
	if err := runMigrations(db); err != nil {
		t.Fatalf("re-up to head: %v", err)
	}
	if err := db.QueryRow(`SELECT banked_cost FROM member
		WHERE id='ow-b' AND kind='outsource'`).Scan(&banked); err != nil || banked != 0.0 {
		t.Fatalf("00025 must carry the worker's banked_cost into member: %v %v", banked, err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
}

// TestMigrateTaskArtifactRoundTrip proves 00022 (task_artifact — the T-3dc5
// artifact set) is a pure ADDITIVE, reversible NEW TABLE: up creates the table +
// its task index and a row inserts; down-to-21 drops both (table gone, and the
// 00021 banked_cost column that precedes it survives untouched); re-up recreates
// the table empty and is idempotent. The 升降升無損 self-verify a new table
// demands — and, post-rebase, confirms 00022 stacks cleanly on 00021.
func TestMigrateTaskArtifactRoundTrip(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "ta.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	// The table + its index exist after up, and a row inserts through the CHECK.
	if _, err := db.Exec(`INSERT INTO task_artifact
		(id, task_id, kind, url, label, created_ts, created_by)
		VALUES ('ta-1', 't-1', 'link', 'https://x/pr/1', 'PR #1', 9, 'm-exec')`); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	if err := db.QueryRow(`SELECT name FROM sqlite_master
		WHERE type='index' AND name='idx_task_artifact_task'`).Scan(new(string)); err != nil {
		t.Fatalf("idx_task_artifact_task must exist after up: %v", err)
	}

	// Down to 21 (before 00022): the table is gone; the 00021 banked_cost column
	// that sits just below it in the stack must survive the revert untouched.
	if err := goose.DownTo(db, "migrations", 21); err != nil {
		t.Fatalf("goose down to 21: %v", err)
	}
	if _, err := db.Exec(`SELECT 1 FROM task_artifact`); err == nil {
		t.Fatalf("down must drop the task_artifact table")
	}
	if _, err := db.Exec(`SELECT banked_cost FROM outsource_worker`); err != nil {
		t.Fatalf("down to 21 must keep 00021 banked_cost intact: %v", err)
	}

	// Re-up: the table returns empty, the index is back, and the run is
	// idempotent thereafter.
	if err := runMigrations(db); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_artifact`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("re-up must recreate task_artifact empty, got %d %v", n, err)
	}
	if err := db.QueryRow(`SELECT name FROM sqlite_master
		WHERE type='index' AND name='idx_task_artifact_task'`).Scan(new(string)); err != nil {
		t.Fatalf("idx_task_artifact_task must survive the rebuild: %v", err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
}

// TestMigrateTaskReassignedFrom proves 00023 (reassigned_from +
// reassigned_from_kind, TEXT — the T-ba04 predecessor stamp) is a pair of pure
// ADDITIVE, reversible columns: up stamps them on a task row; down-to-22 drops
// BOTH (columns gone, the row + its pre-existing columns survive); re-up restores
// them at their ” defaults with the row intact and is idempotent. The 升降升無損
// self-verify the columns demand — run against an isolated on-disk SQLite, not
// inferred.
func TestMigrateTaskReassignedFrom(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "trf.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	// A task row carrying BOTH new columns set (+ a pre-existing column to prove
	// the down rebuild preserves the rest of the row).
	if _, err := db.Exec(`INSERT INTO task
		(id, title, status, priority, reassigned_from, reassigned_from_kind)
		VALUES ('t-r', 'handed over', 'reassigning', 'mid', 'ow-old', 'outsource')`); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	// Down to 22 (before 00023): both new columns must be gone.
	if err := goose.DownTo(db, "migrations", 22); err != nil {
		t.Fatalf("goose down to 22: %v", err)
	}
	for _, col := range []string{"reassigned_from", "reassigned_from_kind"} {
		if _, err := db.Exec(`SELECT ` + col + ` FROM task`); err == nil {
			t.Fatalf("down must drop the %s column", col)
		}
	}
	var title string
	if err := db.QueryRow(
		`SELECT title FROM task WHERE id='t-r'`).Scan(&title); err != nil || title != "handed over" {
		t.Fatalf("row must survive the down (title=%q err=%v)", title, err)
	}

	// Re-up: both columns return at their '' defaults, the row intact.
	if err := runMigrations(db); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	var from, kind string
	if err := db.QueryRow(
		`SELECT reassigned_from, reassigned_from_kind FROM task WHERE id='t-r'`).
		Scan(&from, &kind); err != nil {
		t.Fatalf("columns after re-up: %v", err)
	}
	if from != "" || kind != "" {
		t.Fatalf("re-up must restore the '' defaults, got from=%q kind=%q", from, kind)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
}

// TestMemberKindOutsourceLinkedTaskMigrationRoundTrip pins the 00024 rebuild
// (A案 P0): Up widens the member.kind CHECK to admit 'outsource' and adds the
// nullable linked_task_id, preserving every pre-existing row; Down restores the
// two-value CHECK, squashes 'outsource' rows back to 'assistant' (the honest
// legacy reading), and drops linked_task_id. 升降升無損, on an isolated on-disk
// SQLite.
func TestMemberKindOutsourceLinkedTaskMigrationRoundTrip(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "member-outsource-mig.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	// Step back into the 00024 window: 00025's Down would DELETE kind='outsource'
	// member rows (schema-only rollback, data restore = backup), so the 00024
	// squash semantics are exercised from version 24, before the fold.
	if err := goose.DownTo(db, "migrations", 24); err != nil {
		t.Fatalf("goose down to 24: %v", err)
	}

	// Post-00024 the widened CHECK admits 'outsource' + linked_task_id sets.
	if _, err := db.Exec(`INSERT INTO member (id, name, kind, linked_task_id)
		VALUES ('m-out', 'O-1', 'outsource', 't-42')`); err != nil {
		t.Fatalf("seed outsource member: %v", err)
	}
	// A plain assistant (pre-existing kind) with linked_task_id left NULL.
	if _, err := db.Exec(`INSERT INTO member (id, name, kind)
		VALUES ('m-asst', 'Mira', 'assistant')`); err != nil {
		t.Fatalf("seed assistant member: %v", err)
	}

	// Down one step (00024 → 00023): outsource squashes to assistant, the
	// linked_task_id column goes away, every row survives.
	if err := goose.DownTo(db, "migrations", 23); err != nil {
		t.Fatalf("goose down to 23: %v", err)
	}
	var kind string
	if err := db.QueryRow(
		`SELECT kind FROM member WHERE id='m-out'`).Scan(&kind); err != nil {
		t.Fatalf("m-out after down: %v", err)
	}
	if kind != "assistant" {
		t.Fatalf("down must squash outsource→assistant, got %q", kind)
	}
	if err := db.QueryRow(
		`SELECT kind FROM member WHERE id='m-asst'`).Scan(&kind); err != nil || kind != "assistant" {
		t.Fatalf("m-asst must survive the down rebuild: %q %v", kind, err)
	}
	if _, err := db.Exec(`SELECT linked_task_id FROM member`); err == nil {
		t.Fatalf("down must drop the linked_task_id column")
	}
	// The restored two-value CHECK rejects 'outsource' again.
	if _, err := db.Exec(`INSERT INTO member (id, name, kind)
		VALUES ('m-bad', 'x', 'outsource')`); err == nil {
		t.Fatalf("down must restore the two-value CHECK (outsource rejected)")
	}

	// Up again: 'outsource' + linked_task_id are legal once more, rows survive,
	// and the run is idempotent thereafter.
	if err := runMigrations(db); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if _, err := db.Exec(`UPDATE member SET kind='outsource', linked_task_id='t-99'
		WHERE id='m-out'`); err != nil {
		t.Fatalf("post re-up outsource write must pass: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM member WHERE id IN ('m-out','m-asst')`).
		Scan(&n); err != nil || n != 2 {
		t.Fatalf("re-up must preserve both seeded rows, got %d %v", n, err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
}

// TestMigrateWorkerLifecycleMarkers proves 00019 (refocus_since, REAL) + 00020
// (desired_state, TEXT — mirrors member.desired_state) are pure ADDITIVE,
// reversible columns: up stamps both on a worker row; down-to-18 drops them (the
// columns are gone, the row + every pre-existing column survive); re-up restores
// them at their defaults (refocus_since 0.0, desired_state 'online') with the row
// intact and is idempotent. The 升降升無損 self-verify the two columns demand.
func TestMigrateWorkerLifecycleMarkers(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "wm.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	// 00025 folded outsource_worker away — step back into the pre-fold window.
	if err := goose.DownTo(db, "migrations", 21); err != nil {
		t.Fatalf("goose down to 21: %v", err)
	}

	// A worker row carrying BOTH new columns set (and a pre-existing column to
	// prove the down rebuild preserves the rest of the row).
	if _, err := db.Exec(`INSERT INTO outsource_worker
		(id, codename, model, effort, task_id, status, created_ts,
		 refocus_since, desired_state)
		VALUES ('ow-m', 'O-1', 'opus', 'high', 't-1', 'active', 5,
		 111.0, 'offline')`); err != nil {
		t.Fatalf("seed worker: %v", err)
	}

	// Down to 18 (before 00019/00020): both new columns must be gone.
	if err := goose.DownTo(db, "migrations", 18); err != nil {
		t.Fatalf("goose down to 18: %v", err)
	}
	for _, col := range []string{"refocus_since", "desired_state"} {
		if _, err := db.Exec(`SELECT ` + col + ` FROM outsource_worker`); err == nil {
			t.Fatalf("down must drop the %s column", col)
		}
	}
	// The row + a pre-existing column survive the two column-drop rebuilds.
	var codename string
	if err := db.QueryRow(
		`SELECT codename FROM outsource_worker WHERE id='ow-m'`).Scan(&codename); err != nil ||
		codename != "O-1" {
		t.Fatalf("row must survive the down (codename=%q err=%v)", codename, err)
	}

	// Re-up (into the pre-fold window): both columns return at their defaults,
	// the row intact.
	if err := goose.UpTo(db, "migrations", 21); err != nil {
		t.Fatalf("up to 21: %v", err)
	}
	var refocus float64
	var desired string
	if err := db.QueryRow(
		`SELECT refocus_since, desired_state FROM outsource_worker WHERE id='ow-m'`).
		Scan(&refocus, &desired); err != nil {
		t.Fatalf("columns after re-up: %v", err)
	}
	if refocus != 0.0 || desired != "online" {
		t.Fatalf("re-up must restore defaults, got refocus=%v desired_state=%q", refocus, desired)
	}
	// Continue to head (00025 fold) + idempotence.
	if err := runMigrations(db); err != nil {
		t.Fatalf("re-up to head: %v", err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
}

// TestOutsourceWorkerFoldMigration pins 00025 (A案 P7d — outsource_worker
// folds INTO member, one shot): Up moves EVERY worker row (released included)
// onto kind='outsource' member rows with the ruled column map, asserts codename
// uniqueness, and drops the table; Down is an HONEST schema-only rollback
// (empty outsource_worker back, the four member columns gone, the outsource
// member rows deleted — data restore is the backup's job, owner
// rc-69dd122e9c73); re-Up then finds nothing to move and stays idempotent.
func TestOutsourceWorkerFoldMigration(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "fold.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	// Seed the pre-fold world (version 24) with the three lifecycle shapes —
	// including the RELEASED history row whose codename must stay burned.
	if err := goose.DownTo(db, "migrations", 24); err != nil {
		t.Fatalf("down to 24: %v", err)
	}
	seed := `INSERT INTO outsource_worker
		(id, codename, model, effort, task_id, status, created_ts, released_ts,
		 spawn_attempts, last_spawn_ts, last_spawn_target, last_op, last_op_ok,
		 last_op_log, last_op_reason, last_op_at, desired_machine_id,
		 refocus_since, desired_state, banked_cost) VALUES `
	if _, err := db.Exec(seed + `
		('ow-rel', 'S-1', 'claude-sonnet-4-5', 'high', 't-1', 'released', 100, 200,
		 3, 150, 'm-old', 'worker_stop', 1, 'bye', '', 201, '', 0, 'online', 1.25),
		('ow-act', 'S-2', 'claude-sonnet-4-5', 'medium', 't-2', 'active', 300, 0,
		 1, 310, 'm-a', 'worker_start', 1, 'ok', '', 311, 'auto', 42.0, 'offline', 2.5),
		('ow-asn', 'H-1', 'claude-haiku-4', 'low', 't-3', 'assigned', 400, 0,
		 0, 0, '', '', NULL, '', '', 0, '', 0, 'online', 0)`); err != nil {
		t.Fatalf("seed workers: %v", err)
	}

	// Up (the fold). Whole-fixture assertions on the member projection.
	if err := runMigrations(db); err != nil {
		t.Fatalf("fold up: %v", err)
	}
	if _, err := db.Exec(`SELECT 1 FROM outsource_worker`); err == nil {
		t.Fatalf("up must DROP outsource_worker")
	}
	type folded struct {
		name, kind, roster, linked, desired, desiredMachine string
		created, released, activated, refocus, banked       float64
	}
	read := func(id string) folded {
		var f folded
		if err := db.QueryRow(`SELECT name, kind, roster_status, linked_task_id,
			desired_state, desired_machine_id, created_ts, released_ts,
			activated_ts, refocus_since, banked_cost
			FROM member WHERE id = ?`, id).
			Scan(&f.name, &f.kind, &f.roster, &f.linked, &f.desired,
				&f.desiredMachine, &f.created, &f.released, &f.activated,
				&f.refocus, &f.banked); err != nil {
			t.Fatalf("member %s: %v", id, err)
		}
		return f
	}
	rel := read("ow-rel")
	if rel.kind != "outsource" || rel.roster != "removed" || rel.linked != "t-1" ||
		rel.name != "S-1" || rel.created != 100 || rel.released != 200 ||
		rel.banked != 1.25 {
		t.Fatalf("released fold wrong: %+v", rel)
	}
	act := read("ow-act")
	// 00035 normalizes the legacy "auto" placement to "" (owner ruling
	// 2026-07-25: "auto" never named a real machine).
	if act.roster != "active" || act.activated != 300 || act.desired != "offline" ||
		act.desiredMachine != "" || act.refocus != 42.0 {
		t.Fatalf("active fold wrong: %+v", act)
	}
	asn := read("ow-asn")
	if asn.roster != "active" || asn.activated != 0 || asn.linked != "t-3" {
		t.Fatalf("assigned fold wrong: %+v", asn)
	}
	// spawn_attempts/last_spawn_ts/last_spawn_target are deliberately NOT
	// member columns (in-memory since P7d).
	for _, col := range []string{"spawn_attempts", "last_spawn_ts", "last_spawn_target"} {
		if _, err := db.Exec(`SELECT ` + col + ` FROM member`); err == nil {
			t.Fatalf("member must not grow a %s column", col)
		}
	}
	// The codename uniqueness guarantee is a live UNIQUE index, not a one-shot.
	if _, err := db.Exec(`INSERT INTO member (id, name, kind, codename)
		VALUES ('ow-dup', 'S-1', 'outsource', 'S-1')`); err == nil {
		t.Fatalf("duplicate codename must be rejected by the UNIQUE index")
	}

	// Down: schema-only — empty table back, member columns + outsource rows gone.
	if err := goose.DownTo(db, "migrations", 24); err != nil {
		t.Fatalf("down to 24: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM outsource_worker`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("down must recreate outsource_worker EMPTY, got %d %v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM member WHERE kind='outsource'`).
		Scan(&n); err != nil || n != 0 {
		t.Fatalf("down must delete the outsource member rows, got %d %v", n, err)
	}
	for _, col := range []string{"codename", "created_ts", "released_ts", "activated_ts"} {
		if _, err := db.Exec(`SELECT ` + col + ` FROM member`); err == nil {
			t.Fatalf("down must drop member.%s", col)
		}
	}

	// Re-up over the emptied table + idempotence.
	if err := runMigrations(db); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
}

// TestOutsourceWorkerFoldMigration_DuplicateCodenameFails pins the 00025
// codename ASSERT: two worker rows sharing one codename (corruption — §A.4
// forbids reuse) must fail the migration outright, never fold silently.
func TestOutsourceWorkerFoldMigration_DuplicateCodenameFails(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "fold-dup.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	if err := goose.DownTo(db, "migrations", 24); err != nil {
		t.Fatalf("down to 24: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO outsource_worker
		(id, codename, model, task_id, status, created_ts) VALUES
		('ow-1', 'S-9', 'sonnet', 't-1', 'released', 1),
		('ow-2', 'S-9', 'sonnet', 't-2', 'active', 2)`); err != nil {
		t.Fatalf("seed duplicate codenames: %v", err)
	}
	if err := runMigrations(db); err == nil {
		t.Fatalf("00025 must FAIL on a duplicate codename, but migrated clean")
	}
}

// TestMigration00028DownPushesWaitingExternalToCurrentStep pins the T-9ca5 data
// move of 00028: a task held at status='waiting_external' (the retired
// task-level model) has its reason DOWN-PUSHED onto its CURRENT step (the first
// non-terminal by order_idx,id), which becomes waiting_external with the reason
// copied; task.status/waiting_reason are left untouched (the boot reconcile
// re-derives the identical display value). Graceful when the task has only
// terminal steps or no steps, and never touches a task that is not
// waiting_external. Exercises the migration's REAL Up SQL (gooseUpSQL) against a
// minimal schema seeded with fabricated pre-state.
func TestMigration00028DownPushesWaitingExternalToCurrentStep(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "mig28.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// The pre-00028 shape: task carries the (retired) task-level waiting_reason;
	// task_step has no waiting_reason yet — the migration's ALTER adds it.
	if _, err := db.Exec(`
		CREATE TABLE task (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			waiting_reason TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE task_step (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			order_idx INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL
		);`); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	tasks := []struct{ id, status, reason string }{
		{"we-cur", "waiting_external", "等外部開通"},    // (a) has a live current step
		{"we-alldone", "waiting_external", "全部完成"}, // (b) every step terminal
		{"we-nosteps", "waiting_external", "沒有步驟"}, // (c) no steps at all
		{"ip-reason", "in_progress", "殘留原因"},       // (d) not waiting_external
	}
	for _, tk := range tasks {
		if _, err := db.Exec(
			`INSERT INTO task (id, status, waiting_reason) VALUES (?, ?, ?)`,
			tk.id, tk.status, tk.reason); err != nil {
			t.Fatalf("seed task %s: %v", tk.id, err)
		}
	}
	steps := []struct {
		id, taskID string
		order      int
		status     string
	}{
		{"s-cur-done", "we-cur", 0, "done"},       // terminal prefix, skipped
		{"s-cur-run", "we-cur", 1, "in_progress"}, // the current step → moves
		{"s-alldone", "we-alldone", 0, "done"},    // only terminal → nothing moves
		{"s-ip", "ip-reason", 0, "in_progress"},   // task not waiting_external → untouched
	}
	for _, st := range steps {
		if _, err := db.Exec(
			`INSERT INTO task_step (id, task_id, order_idx, status) VALUES (?, ?, ?, ?)`,
			st.id, st.taskID, st.order, st.status); err != nil {
			t.Fatalf("seed step %s: %v", st.id, err)
		}
	}

	if _, err := db.Exec(gooseUpSQL(t, "migrations/00028_task_lock_and_step_waiting_reason.sql")); err != nil {
		t.Fatalf("run 00028 Up: %v", err)
	}

	readStep := func(id string) (status, reason string) {
		if err := db.QueryRow(
			`SELECT status, waiting_reason FROM task_step WHERE id = ?`, id).
			Scan(&status, &reason); err != nil {
			t.Fatalf("read step %s: %v", id, err)
		}
		return
	}
	readTaskReason := func(id string) string {
		var r string
		if err := db.QueryRow(
			`SELECT waiting_reason FROM task WHERE id = ?`, id).Scan(&r); err != nil {
			t.Fatalf("read task %s: %v", id, err)
		}
		return r
	}

	// (a) the first non-terminal step took the down-push; its done sibling is
	// untouched; the task-level reason is preserved.
	if s, r := readStep("s-cur-run"); s != "waiting_external" || r != "等外部開通" {
		t.Fatalf("(a) current step must become waiting_external + carry the reason, got %q/%q", s, r)
	}
	if s, r := readStep("s-cur-done"); s != "done" || r != "" {
		t.Fatalf("(a) the done prefix must stay put, got %q/%q", s, r)
	}
	if r := readTaskReason("we-cur"); r != "等外部開通" {
		t.Fatalf("(a) task-level waiting_reason must be left untouched, got %q", r)
	}

	// (b) all-terminal steps: nothing moves.
	if s, r := readStep("s-alldone"); s != "done" || r != "" {
		t.Fatalf("(b) an all-done task must move no step, got %q/%q", s, r)
	}

	// (c) no steps: graceful (nothing to assert beyond no error) — the task
	// reason is left intact.
	if r := readTaskReason("we-nosteps"); r != "沒有步驟" {
		t.Fatalf("(c) a stepless waiting_external task must keep its reason, got %q", r)
	}

	// (d) a task that is NOT waiting_external is never touched.
	if s, r := readStep("s-ip"); s != "in_progress" || r != "" {
		t.Fatalf("(d) a non-waiting_external task's step must not move, got %q/%q", s, r)
	}
}

// TestMigration00028DownRestoresStepStatusByStartedTS pins the REVERSIBILITY of
// the 00028 data move: Down must restore each down-pushed step to the EXACT
// status it held before Up, recovered from started_ts (a never-started step
// keeps started_ts=0 → pending; a step that entered in_progress carries
// started_ts>0 → in_progress). A blanket in_progress would silently promote a
// never-started current step — half the live prod population (Joey prod read:
// 2/4 waiting_external tasks hold a pending current step). Also exercises the
// reassigning-lock restore and the two no-op boundaries (all-done / stepless).
// Rules, not counts — the migration set drifts run to run.
func TestMigration00028DownRestoresStepStatusByStartedTS(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "mig28down.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// pre-00028, post-00004 shape: task_step already carries started_ts (00004);
	// lock / step.waiting_reason are exactly what 00028 Up adds and Down drops.
	if _, err := db.Exec(`
		CREATE TABLE task (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			waiting_reason TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE task_step (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			order_idx INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			started_ts REAL NOT NULL DEFAULT 0.0
		);`); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	tasks := []struct{ id, status, reason string }{
		{"we-pending", "waiting_external", "等審批"},  // current step never started
		{"we-inprog", "waiting_external", "等外部回覆"}, // current step in_progress
		{"reassign1", "reassigning", ""},           // lock migration + restore (boundary C)
		{"we-alldone", "waiting_external", "都做完了"}, // boundary A: nothing moves
		{"we-nosteps", "waiting_external", "沒步驟"},  // boundary B: no step at all
	}
	for _, tk := range tasks {
		if _, err := db.Exec(
			`INSERT INTO task (id, status, waiting_reason) VALUES (?, ?, ?)`,
			tk.id, tk.status, tk.reason); err != nil {
			t.Fatalf("seed task %s: %v", tk.id, err)
		}
	}
	steps := []struct {
		id, taskID string
		order      int
		status     string
		started    float64
	}{
		{"p-done", "we-pending", 0, "done", 100},      // terminal prefix, skipped by Up
		{"p-cur", "we-pending", 1, "pending", 0},      // pending current step: started_ts=0
		{"i-cur", "we-inprog", 0, "in_progress", 200}, // in_progress current step: started_ts>0
		{"r-step", "reassign1", 0, "in_progress", 300},
		{"ad-done", "we-alldone", 0, "done", 400}, // all terminal → Up moves nothing
	}
	for _, st := range steps {
		if _, err := db.Exec(
			`INSERT INTO task_step (id, task_id, order_idx, status, started_ts) VALUES (?, ?, ?, ?, ?)`,
			st.id, st.taskID, st.order, st.status, st.started); err != nil {
			t.Fatalf("seed step %s: %v", st.id, err)
		}
	}

	mig := "migrations/00028_task_lock_and_step_waiting_reason.sql"
	if _, err := db.Exec(gooseUpSQL(t, mig)); err != nil {
		t.Fatalf("run 00028 Up: %v", err)
	}

	stepStatus := func(id string) string {
		var s string
		if err := db.QueryRow(`SELECT status FROM task_step WHERE id = ?`, id).Scan(&s); err != nil {
			t.Fatalf("read step %s: %v", id, err)
		}
		return s
	}
	taskStatus := func(id string) string {
		var s string
		if err := db.QueryRow(`SELECT status FROM task WHERE id = ?`, id).Scan(&s); err != nil {
			t.Fatalf("read task %s: %v", id, err)
		}
		return s
	}

	// After Up: both live current steps became waiting_external; reassigning
	// moved onto the lock (status → in_progress placeholder).
	if s := stepStatus("p-cur"); s != "waiting_external" {
		t.Fatalf("Up: pending current step must down-push to waiting_external, got %q", s)
	}
	if s := stepStatus("i-cur"); s != "waiting_external" {
		t.Fatalf("Up: in_progress current step must down-push to waiting_external, got %q", s)
	}
	if s := taskStatus("reassign1"); s != "in_progress" {
		t.Fatalf("Up: reassigning task moves onto lock, status → in_progress, got %q", s)
	}

	// Down drops lock / waiting_reason columns, so read status only afterwards.
	if _, err := db.Exec(gooseDownSQL(t, mig)); err != nil {
		t.Fatalf("run 00028 Down: %v", err)
	}

	// THE reversibility assertion: the never-started current step round-trips
	// back to pending, NOT a lossy in_progress.
	if s := stepStatus("p-cur"); s != "pending" {
		t.Fatalf("Down MUST restore a never-started (started_ts=0) current step to pending, got %q", s)
	}
	if s := stepStatus("i-cur"); s != "in_progress" {
		t.Fatalf("Down must restore a started (started_ts>0) current step to in_progress, got %q", s)
	}
	if s := taskStatus("reassign1"); s != "reassigning" {
		t.Fatalf("Down must restore the reassigning status, got %q", s)
	}
	// Boundaries: Up moved no step for these, so Down leaves them exactly as seeded.
	if s := stepStatus("ad-done"); s != "done" {
		t.Fatalf("boundary A (all-done): step must stay done, got %q", s)
	}
	if s := stepStatus("p-done"); s != "done" {
		t.Fatalf("the done prefix must stay done through the round-trip, got %q", s)
	}
}

// TestMigration00035NormalizesAutoMachinePlacement pins the 00035 normalization:
// the legacy literal 'auto' — which never named a warden, so the placement was
// unreachable — becomes ” in ALL THREE stores that carried it, while every real
// machine id (and every already-empty value) is left untouched.
func TestMigration00035NormalizesAutoMachinePlacement(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "auto-norm.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	// Seed the pre-normalization world (the version just below this migration).
	if err := goose.DownTo(db, "migrations", 33); err != nil {
		t.Fatalf("down to 33: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO member (id, name, kind, desired_machine_id) VALUES
		('m-auto', 'Auto', 'assistant', 'auto'),
		('m-real', 'Real', 'assistant', 'm-box'),
		('m-none', 'None', 'assistant', '')`); err != nil {
		t.Fatalf("seed members: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO task (id, title, outsource_machine) VALUES
		('t-auto', 'auto placed', 'auto'),
		('t-real', 'box placed', 'm-box'),
		('t-none', 'unplaced', '')`); err != nil {
		t.Fatalf("seed tasks: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO task_manual (type_key, assignee) VALUES
		('tm-auto', '{"kind":"outsource","model":"opus","machine":"auto"}'),
		('tm-real', '{"kind":"outsource","model":"opus","machine":"m-box"}'),
		('tm-member', '{"kind":"member","member_id":"m-real"}')`); err != nil {
		t.Fatalf("seed manuals: %v", err)
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("normalize up: %v", err)
	}

	scalar := func(query, id string) string {
		t.Helper()
		var got string
		if err := db.QueryRow(query, id).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return got
	}
	const (
		memberQ = `SELECT desired_machine_id FROM member WHERE id = ?`
		taskQ   = `SELECT outsource_machine FROM task WHERE id = ?`
		manualQ = `SELECT COALESCE(json_extract(assignee, '$.machine'), '<absent>')
		             FROM task_manual WHERE type_key = ?`
	)
	for _, tc := range []struct{ what, query, id, want string }{
		{"member auto", memberQ, "m-auto", ""},
		{"member real", memberQ, "m-real", "m-box"},
		{"member empty", memberQ, "m-none", ""},
		{"task auto", taskQ, "t-auto", ""},
		{"task real", taskQ, "t-real", "m-box"},
		{"task empty", taskQ, "t-none", ""},
		{"manual auto", manualQ, "tm-auto", ""},
		{"manual real", manualQ, "tm-real", "m-box"},
		{"manual member assignee", manualQ, "tm-member", "<absent>"},
	} {
		if got := scalar(tc.query, tc.id); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.what, got, tc.want)
		}
	}
	// The member-kind assignee must survive byte-for-byte — json_set on a row
	// that never carried a machine would rewrite the whole blob.
	if got := scalar(`SELECT assignee FROM task_manual WHERE type_key = ?`, "tm-member"); got !=
		`{"kind":"member","member_id":"m-real"}` {
		t.Errorf("an untouched assignee must not be rewritten, got %q", got)
	}
}

// TestMemberAvatarMigrationRollback pins the destructive half of 00040: an
// avatar blob is owned only by member.avatar_attachment_id, so Down must remove
// that blob before dropping its sole pointer. Unrelated chat blobs survive.
func TestMemberAvatarMigrationRollback(t *testing.T) {
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}
	db, err := openSQLite(filepath.Join(t.TempDir(), "member-avatar-mig.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := goose.UpTo(db, "migrations", 40); err != nil {
		t.Fatalf("goose up to 40: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO chat_attachment (id, mime, data, filename) VALUES
		('ava-owned', 'image/png', X'89504E47', 'owned.png'),
		('att-chat', 'image/png', X'89504E47', 'chat.png')`); err != nil {
		t.Fatalf("seed blobs: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO member
		(id, name, kind, avatar_attachment_id)
		VALUES ('m-avatar-mig', 'Avatar', 'assistant', 'ava-owned')`); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	if err := goose.DownTo(db, "migrations", 37); err != nil {
		t.Fatalf("goose down to 37: %v", err)
	}

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM chat_attachment WHERE id = 'ava-owned'`,
	).Scan(&count); err != nil || count != 0 {
		t.Fatalf("owned avatar blob must be removed, count=%d err=%v", count, err)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM chat_attachment WHERE id = 'att-chat'`,
	).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unrelated chat blob must survive, count=%d err=%v", count, err)
	}
	if _, err := db.Exec(
		`SELECT avatar_attachment_id FROM member LIMIT 1`,
	); err == nil {
		t.Fatal("rollback must drop member.avatar_attachment_id")
	}
}

// TestMigration00045DeletesOnlyTheRetiredTaskManualHistory pins the surgical
// half of the irreversible delete (T-1f39, owner-approved): every
// 'task_manual' row goes, and every other document_kind — including the two
// replacement manual streams keyed by the SAME document_key — survives with
// its content byte-for-byte. The fixture deliberately puts rows a wider WHERE
// clause would take (same key, neighbouring kinds; same kind prefix) in the
// blast radius, so a mutant that drops or widens the predicate has somewhere
// to do damage.
func TestMigration00045DeletesOnlyTheRetiredTaskManualHistory(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "legacy-manual-history.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	// Seed the world as it stood the moment before the deletion.
	if err := goose.DownTo(db, "migrations", 43); err != nil {
		t.Fatalf("down to 43: %v", err)
	}

	type row struct{ kind, key, content string }
	seeded := []row{
		{"task_manual", "tm-alpha", `{"purpose":"p1","fields":"[]","sop_md":"sop v1","learnings":"l v1"}`},
		{"task_manual", "tm-alpha", `{"purpose":"p2","fields":"[]","sop_md":"sop v2","learnings":"l v2"}`},
		{"task_manual", "tm-beta", `{"purpose":"p1","fields":"[]","sop_md":"sop v1","learnings":"l v1"}`},
		{"task_manual_sop", "tm-alpha", `{"sop_md":"sop v1"}`},
		{"task_manual_sop", "tm-beta", `{"sop_md":"sop v1"}`},
		{"task_manual_learnings", "tm-alpha", `{"learnings":"l v1"}`},
		{"task_manual_learnings", "tm-beta", `{"learnings":"l v2"}`},
		{"lessons", "r-assistant::review-pr", `{"text":"lesson v1","tombstoned":"false"}`},
		{"global_context", "global", `{"text":"context v1","tombstoned":"false"}`},
		{"role_definition", "r-assistant", `{"name":"Assistant","definition_md":"def v1","tombstoned":"false"}`},
	}
	for i, r := range seeded {
		if _, err := db.Exec(`INSERT INTO document_history
			(document_kind, document_key, content_json, created_ts, actor_id)
			VALUES (?, ?, ?, ?, 'owner')`, r.kind, r.key, r.content, float64(1000+i)); err != nil {
			t.Fatalf("seed %s/%s: %v", r.kind, r.key, err)
		}
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("delete up: %v", err)
	}

	var legacy int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM document_history WHERE document_kind = 'task_manual'`,
	).Scan(&legacy); err != nil {
		t.Fatalf("count legacy: %v", err)
	}
	if legacy != 0 {
		t.Errorf("the retired bundle still has %d rows", legacy)
	}

	survivors := map[row]bool{}
	rows, err := db.Query(`SELECT document_kind, document_key, content_json FROM document_history`)
	if err != nil {
		t.Fatalf("read survivors: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var got row
		if err := rows.Scan(&got.kind, &got.key, &got.content); err != nil {
			t.Fatalf("scan: %v", err)
		}
		survivors[got] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	want := map[row]bool{}
	for _, r := range seeded {
		if r.kind != "task_manual" {
			want[r] = true
		}
	}
	for r := range want {
		if !survivors[r] {
			t.Errorf("the delete took a %s row it had no business touching: %s/%s %s",
				r.kind, r.kind, r.key, r.content)
		}
	}
	if len(survivors) != len(want) {
		t.Errorf("document_history holds %d rows after the delete, want the %d non-legacy rows",
			len(survivors), len(want))
	}

	// Down cannot restore what Up deleted, and must not pretend otherwise by
	// leaving anything behind: rolling back leaves the survivors exactly as the
	// Up left them and revives nothing.
	if err := goose.DownTo(db, "migrations", 43); err != nil {
		t.Fatalf("down to 43: %v", err)
	}
	var after int
	if err := db.QueryRow(`SELECT COUNT(*) FROM document_history`).Scan(&after); err != nil {
		t.Fatalf("count after down: %v", err)
	}
	if after != len(want) {
		t.Errorf("rollback changed the table to %d rows, want the same %d — the Down is a no-op "+
			"and the deletion is irreversible by design", after, len(want))
	}
}

func TestMemberThemeAvatarMigrationCleansPersonalBlobsAndRoundTrips(t *testing.T) {
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}
	db, err := openSQLite(filepath.Join(t.TempDir(), "member-theme-avatar-mig.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := goose.UpTo(db, "migrations", 59); err != nil {
		t.Fatalf("goose up to 59: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO chat_attachment (id, mime, data, filename) VALUES
		('ava-retired', 'image/png', X'89504E47', 'retired.png'),
		('att-survives', 'image/png', X'89504E47', 'chat.png')`); err != nil {
		t.Fatalf("seed blobs: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO member
		(id, name, kind, avatar_attachment_id)
		VALUES
		('m-index-mig', 'Index', 'assistant', 'ava-retired'),
		('m-index-corrupt', 'Corrupt legacy pointer', 'assistant', 'att-survives')`); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 60); err != nil {
		t.Fatalf("goose up to 60: %v", err)
	}

	var count int
	// The association starts EMPTY. Nothing back-fills a default, because a
	// member with no row is exactly what "has not chosen yet" means.
	if err := db.QueryRow(`SELECT COUNT(*) FROM member_theme_avatar`).Scan(&count); err != nil ||
		count != 0 {
		t.Fatalf("migration must not back-fill selections, count=%d err=%v", count, err)
	}
	if _, err := db.Exec(`INSERT INTO member_theme_avatar (member_id, theme_id, icon_id)
		VALUES ('m-index-mig', 'alpha', 'icn-a')`); err != nil {
		t.Fatalf("insert selection: %v", err)
	}
	// One choice per (member, theme) is a database-level fact, not a handler
	// convention: without it a member could hold two faces in one theme.
	if _, err := db.Exec(`INSERT INTO member_theme_avatar (member_id, theme_id, icon_id)
		VALUES ('m-index-mig', 'alpha', 'icn-b')`); err == nil {
		t.Fatal("a second row for the same (member, theme) must violate the primary key")
	}
	if _, err := db.Exec(`INSERT INTO member_theme_avatar (member_id, theme_id, icon_id)
		VALUES ('m-index-mig', 'beta', 'icn-b')`); err != nil {
		t.Fatalf("the same member must be able to choose in another theme: %v", err)
	}

	if err := db.QueryRow(
		`SELECT COUNT(*) FROM chat_attachment WHERE id = 'ava-retired'`,
	).Scan(&count); err != nil || count != 0 {
		t.Fatalf("retired personal blob must be deleted, count=%d err=%v", count, err)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM chat_attachment WHERE id = 'att-survives'`,
	).Scan(&count); err != nil || count != 1 {
		t.Fatalf("non-ava blob must survive even when a legacy pointer names it, count=%d err=%v", count, err)
	}
	if _, err := db.Exec(`SELECT avatar_attachment_id FROM member LIMIT 1`); err == nil {
		t.Fatal("up migration must drop avatar_attachment_id")
	}

	if err := goose.DownTo(db, "migrations", 59); err != nil {
		t.Fatalf("goose down to 59: %v", err)
	}
	var pointer string
	if err := db.QueryRow(
		`SELECT avatar_attachment_id FROM member WHERE id = 'm-index-mig'`,
	).Scan(&pointer); err != nil || pointer != "" {
		t.Fatalf("rollback pointer must be empty: got %q err=%v", pointer, err)
	}
	if _, err := db.Exec(`SELECT 1 FROM member_theme_avatar LIMIT 1`); err == nil {
		t.Fatal("rollback must drop member_theme_avatar")
	}
	if err := goose.UpTo(db, "migrations", 60); err != nil {
		t.Fatalf("second goose up to 60: %v", err)
	}
}
