package main

// wal_pool_test.go — T-dd7a.
//
// 🔴 These tests exist because the defect they guard is INVISIBLE. If WAL fails
// to turn on, or the write pool stops beginning IMMEDIATE, or a write is wired to
// the read pool, the server still compiles and the data is still correct. The
// symptoms are (a) every request queueing at the database again — which is the
// thing the owner reported, a 0.1 kB endpoint taking 904 ms while a 407 kB one
// took 85 ms in the same session — or (b) an intermittent SQLITE_BUSY under
// concurrent writers. Neither has any other alarm, so each half needs a test that
// fails rather than a comment that hopes.

import (
	"context"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── the file's journal mode ───────────────────────────────────────────────────

// TestOpenSQLite_IsActuallyInWAL asks the DATABASE, not the DSN string.
// Asserting the DSN would be worthless: SQLite ignores a malformed pragma
// WITHOUT error, so a typo'd pragma satisfies a string check exactly as happily
// as a correct one.
func TestOpenSQLite_IsActuallyInWAL(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "wal.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode is %q, want wal — every request will serialise at the database", mode)
	}
}

// TestAssertJournalMode_NamesTheMismatch is the other half: the check must FAIL
// when the mode is wrong, and must say WHICH mode it found. A check that always
// returned nil would look identical to a working one, and "wrong mode" without
// the actual value sends the next reader off to guess.
func TestAssertJournalMode_NamesTheMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollback.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	var forced string
	if err := db.QueryRow(`PRAGMA journal_mode=delete`).Scan(&forced); err != nil {
		t.Fatalf("force rollback journal: %v", err)
	}

	got, err := assertJournalMode(db, sqliteJournalMode)
	if err == nil {
		t.Fatalf("a database in %q passed a check for %q", got, sqliteJournalMode)
	}
	if got == "" {
		t.Error("the failure did not report the mode it actually found")
	}
	if !strings.Contains(err.Error(), got) {
		t.Errorf("the message must carry the mode it found; got %q", err.Error())
	}
}

// ── the write pool's transaction mode ────────────────────────────────────────

// TestWritePool_ReadThenWriteNeverHitsBusy pins the transaction mode. It models
// what our own transactions actually do — SaveWithDocumentHistories reads the live
// document inside the very transaction that overwrites it — from two INDEPENDENT
// handles on one file, which is a real production shape: `ocserverd backup`
// (cmdBackup) and a shell sqlite3 each open their own.
//
// 🔴 BE EXACT ABOUT WHAT THIS TEST DISCRIMINATES, because two wider claims are
// tempting and both are false:
//
//  1. It does NOT discriminate "after this change vs before". It is GREEN on the
//     parent commit too. What it discriminates is IMMEDIATE vs DEFERRED *under
//     WAL* — which is worth a test precisely because WAL is what this change
//     turns on.
//  2. It is NOT "the test that disproved the simpler version". The thing that
//     disproved `WAL + raise the cap` (branch t-dd7a-wal, commit 25bf66d) was the
//     full CI going red on TestSaveWithDocumentHistoryUnderConcurrentWriters-
//     KeepsTheChainContiguous. This test did not exist then.
//
// 🔴 AND THE HAZARD IS WAL-ONLY, not a general property of SQLite. Measured by an
// independent review on this tree: rollback journal + DEFERRED → 0/8 failed;
// WAL + DEFERRED → 2/8 failed with the SQLITE_BUSY arriving at 0ms/1ms against a
// 5000ms busy_timeout; WAL + IMMEDIATE → 0/8. The error code tells the same story
// — SQLITE_BUSY_SNAPSHOT (517) only exists in WAL mode. So turning WAL on is what
// INTRODUCES the upgrade-conflict hazard, and `_txlock=immediate` is the half of
// the change that pays for it: a DEFERRED transaction that reads first must
// UPGRADE its read lock, SQLite answers an upgrade conflict with an instant
// SQLITE_BUSY that busy_timeout does NOT wait out (waiting would deadlock two
// upgraders), and IMMEDIATE takes the write lock at BEGIN where busy_timeout does
// apply.
func TestWritePool_ReadThenWriteNeverHitsBusy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "writers.db")
	a, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	defer a.Close()
	b, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open b: %v", err)
	}
	defer b.Close()

	if _, err := a.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := a.Exec(`INSERT INTO t (v) VALUES ('seed')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var mu sync.Mutex
	var errs []error
	var wg sync.WaitGroup
	start := make(chan struct{})
	const perHandle = 4
	for _, db := range []*sql.DB{a, b} {
		for i := 0; i < perHandle; i++ {
			wg.Add(1)
			go func(db *sql.DB) {
				defer wg.Done()
				<-start
				note := func(err error) {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
				tx, err := db.Begin()
				if err != nil {
					note(err)
					return
				}
				// READ first, then WRITE, in one transaction — the shape that
				// forces a lock upgrade under BEGIN DEFERRED.
				var n int
				if err := tx.QueryRow(`SELECT count(*) FROM t`).Scan(&n); err != nil {
					_ = tx.Rollback()
					note(err)
					return
				}
				if _, err := tx.Exec(`INSERT INTO t (v) VALUES ('x')`); err != nil {
					_ = tx.Rollback()
					note(err)
					return
				}
				if err := tx.Commit(); err != nil {
					note(err)
				}
			}(db)
		}
	}
	close(start)
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("%d of %d read-then-write transactions failed (first: %v) — writers are not queueing, they are giving up",
			len(errs), 2*perHandle, errs[0])
	}
	// Every transaction must have LANDED, not merely not-errored: "no errors"
	// would also be satisfied by a loop that never ran.
	var rows int
	if err := a.QueryRow(`SELECT count(*) FROM t`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if want := 1 + 2*perHandle; rows != want {
		t.Fatalf("t has %d rows, want %d — some writes silently did not commit", rows, want)
	}
}

// ── the read pool ────────────────────────────────────────────────────────────

// TestReadPool_TwoReadersRunAtOnce is the behavioural claim the whole ticket is
// about: two reads must be able to be in flight simultaneously. With one pooled
// connection the second Begin blocks until the first finishes, so this test is
// what turns "reads queue behind each other" from an invisible property into a
// failing test.
//
// It is bounded by a context deadline on purpose: the regression manifests as
// BLOCKING, and a test that hangs forever reports nothing.
func TestReadPool_TwoReadersRunAtOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readers.db")
	w, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open write pool: %v", err)
	}
	defer w.Close()
	if err := runMigrations(w); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	r, err := openSQLiteReadPool(path)
	if err != nil {
		t.Fatalf("open read pool: %v", err)
	}
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	first, err := r.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("first reader: %v", err)
	}
	defer func() { _ = first.Rollback() }()
	var n int
	if err := first.QueryRowContext(ctx, `SELECT count(*) FROM member`).Scan(&n); err != nil {
		t.Fatalf("first read: %v", err)
	}

	// The second reader must get a connection WHILE the first transaction is
	// still open. Capped at one connection this Begin never returns and the
	// deadline is what reports it.
	second, err := r.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("second reader could not start while the first was open (reads are still queueing): %v", err)
	}
	defer func() { _ = second.Rollback() }()
	if err := second.QueryRowContext(ctx, `SELECT count(*) FROM member`).Scan(&n); err != nil {
		t.Fatalf("second read: %v", err)
	}
}

// TestReadPool_IsActuallyReadOnly gives every other read-pool assertion its
// teeth. `mode=ro` is a GUARD, not a micro-optimisation: it is what converts "a
// write got wired to the reader" from an intermittent, load-only SQLITE_BUSY into
// an immediate named error on the very first attempt, in every environment. If
// this test fails, TestWritesAllLandThroughTheRealSplitPools below proves nothing.
func TestReadPool_IsActuallyReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ro.db")
	w, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open write pool: %v", err)
	}
	defer w.Close()
	if err := runMigrations(w); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	r, err := openSQLiteReadPool(path)
	if err != nil {
		t.Fatalf("open read pool: %v", err)
	}
	defer r.Close()

	if _, err := r.Exec(`INSERT INTO setting (key, value, updated_at) VALUES ('x', 'y', 1)`); err == nil {
		t.Fatal("the read pool accepted a write — the mode=ro guard is not armed, so a misrouted write would fail only under concurrency")
	}
}

// newSplitPoolDAL builds a DAL wired exactly the way `serve` wires it: a write
// pool that has migrated the file, plus a genuinely read-only read pool. Unit
// tests normally use NewDAL (one handle for both roles), which CANNOT catch a
// write routed to the read pool — that handle is writable.
func newSplitPoolDAL(t *testing.T) *DAL {
	t.Helper()
	path := filepath.Join(t.TempDir(), "split.db")
	w, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open write pool: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if err := runMigrations(w); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	r, err := openSQLiteReadPool(path)
	if err != nil {
		t.Fatalf("open read pool: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return NewDALPools(w, r)
}

// TestWritesAllLandThroughTheRealSplitPools drives write paths against the REAL
// split wiring. A write misrouted to the read pool fails here with "attempt to
// write a readonly database" — loudly, deterministically, without needing
// concurrency to provoke it.
//
// ⚠️ HONEST SCOPE: this is a sample, not the census. The exhaustive part of the
// argument is the compiler (the DAL has no `db` field any more, so every call
// site had to name a pool) plus the two AST scans below, which are zero-rows
// queries over ALL of them. This test's job is to prove the runtime guard is
// really armed on the paths people touch most.
func TestWritesAllLandThroughTheRealSplitPools(t *testing.T) {
	d := newSplitPoolDAL(t)

	writes := []struct {
		name string
		run  func() error
	}{
		// direct d.wdb.Exec
		{"PutMember", func() error { return d.PutMember(fullMember("m-split")) }},
		{"PutSetting", func() error { return d.PutSetting("auth.token_ttl", "3600") }},
		{"DeleteSetting", func() error { return d.DeleteSetting("auth.token_ttl") }},
		{"PutAccountAlias", func() error { return d.PutAccountAlias(AccountAlias{Account: "a", DisplayName: "A"}) }},
		{"PutMachineAlias", func() error { return d.PutMachineAlias(MachineAlias{MachineID: "m", DisplayName: "M"}) }},
		{"PutWardenCommand", func() error {
			return d.PutWardenCommand(WardenCommand{WardenID: "w", Verb: "update", MemberID: "m", Frame: []byte("{}"), EnqueuedTS: 1})
		}},
		{"DeleteWardenCommand", func() error { return d.DeleteWardenCommand("w", "update", "m") }},
		{"PutPushSubscription", func() error {
			return d.PutPushSubscription(PushSubscription{Endpoint: "https://e", P256dh: "p", Auth: "a"})
		}},
		{"DeletePushSubscription", func() error { return d.DeletePushSubscription("https://e") }},
		// the sqlExecer seam (putXOn(d.wdb, …)) — the shape the compiler CANNOT
		// distinguish from putXOn(d.rdb, …), which is why it is sampled here.
		{"PutChat", func() error {
			return d.PutChat(ChatMessage{ID: "c-split", Sender: "m-split", Recipient: "owner", Body: "hi", TS: 1})
		}},
		{"PutChatAttachment", func() error {
			return d.PutChatAttachment(ChatAttachment{ID: "att-split", Mime: "text/plain", Data: []byte("x")})
		}},
		{"PutUserContext", func() error { return d.PutUserContext(UserContext{Text: "ctx"}) }},
		{"PutRoleDef", func() error {
			return d.PutRoleDef(RoleDef{RoleKey: "assistant", Name: "A", DefinitionMD: "d"})
		}},
		{"PutLessons", func() error {
			return d.PutLessons(Lessons{RoleKey: "assistant", TaskType: "t", Text: "l"})
		}},
		// read-modify-write pair: the Exec goes to the write pool, the read-back
		// to the read pool. It is here because a cross-pool read-your-own-write
		// is a real property of the split, not just a routing question.
		{"PutChatRead", func() error {
			_, _, err := d.PutChatRead(ChatRead{ReaderID: "m-split", PeerID: "owner", LastReadTS: 5})
			return err
		}},
		// d.wdb.Begin() paths (inTx and hand-rolled transactions)
		{"PutChatWithAttachments", func() error {
			return d.PutChatWithAttachments(
				ChatMessage{ID: "c-tx", Sender: "m-split", Recipient: "owner", Body: "b", TS: 2},
				[]ChatAttachment{{ID: "att-tx", Mime: "text/plain", Data: []byte("y")}})
		}},
		{"HardDeleteMember", func() error { _, err := d.HardDeleteMember("m-split"); return err }},
	}

	for _, w := range writes {
		if err := w.run(); err != nil {
			t.Errorf("%s failed against the real split pools: %v", w.name, err)
			if strings.Contains(err.Error(), "readonly") {
				t.Errorf("  ↳ %s is wired to the READ pool. Route it to d.wdb.", w.name)
			}
		}
	}

	// Anti-vacuity: an empty table would pass.
	if len(writes) < 15 {
		t.Fatalf("the write sample shrank to %d — that is not a sample of the write surface any more", len(writes))
	}
	// The writes must have LANDED, not merely not-errored — and reading them
	// back goes through the READ pool, so this also proves a write on one handle
	// is visible to the other (WAL's shared wal-index, not two private views).
	att, err := d.GetChatAttachment("att-tx")
	if err != nil || att == nil {
		t.Fatalf("a write that reported success is not readable back through the read pool: %+v (%v)", att, err)
	}
}

// ── the runtime self-check is actually WIRED into serve ──────────────────────

// TestServeAsksTheDatabaseWhichJournalModeItIsIn covers the CALL SITE, not the
// function.
//
// 🔴 WHY A SEPARATE TEST FOR ONE `if` BLOCK. assertJournalMode had unit coverage
// and openSQLite's WAL had coverage, but nothing observed that `cmdServe`
// actually CALLS it — an independent review deleted the whole block from
// server.go and the entire suite stayed green. That block is the ONLY online
// signal for a defect that is otherwise undetectable from outside: if WAL fails
// to turn on, the server serves, the data is correct, and the sole symptom is the
// per-request queueing this ticket removed. A self-check that can be silently
// removed is not a self-check.
//
// It drives the real boot by HOLDING THE PORT serve is about to want (the same
// seam server_test.go's orphan-card boot test uses): boot runs all the way
// through open → pre-migration backup → goose → THIS CHECK → read pool → seed,
// and then exits 1 on the bind. rc == 1 for that reason is itself the proof that
// the whole boot path executed rather than short-circuiting early.
func TestServeAsksTheDatabaseWhichJournalModeItIsIn(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "serve-journal.db")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	cfgPath := filepath.Join(dir, "oc.toml")
	if err := os.WriteFile(cfgPath,
		[]byte(fmt.Sprintf("[server]\nport = %d\n", port)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out strings.Builder
	rc := cmdServe(envOf(map[string]string{
		"OC_CONFIG":          cfgPath,
		"OC_DATABASE_URL":    "sqlite:///" + dbPath,
		"OC_NO_OPEN_BROWSER": "1",
	}), true, true, &out)
	if rc != 1 {
		t.Fatalf("the held port must make serve exit 1 (boot ran, bind failed), got %d\n%s", rc, out.String())
	}

	got := out.String()
	// 🔴 THE TWO FAILURE CAUSES MUST NOT SHARE ONE MESSAGE. If the check runs and
	// finds the wrong mode (a typo'd pragma, so WAL never turned on) the wiring is
	// FINE and the database is wrong; if nothing is printed at all, the call site
	// is gone. An earlier version reported both as "the self-check is not wired
	// in", which sent the reader to the wrong file — the exact class of defect
	// this whole ticket keeps correcting.
	switch {
	case strings.Contains(got, "WARNING: journal_mode"):
		t.Fatalf("the self-check IS wired in and it reports the wrong mode — WAL did not turn on (suspect openSQLite's pragma, not this call site):\n%s", got)
	case !strings.Contains(got, "journal_mode="):
		t.Fatalf("serve printed nothing about the journal mode — the self-check call site is missing from cmdServe:\n%s", got)
	case !strings.Contains(got, "journal_mode=wal"):
		t.Fatalf("serve reported a journal mode other than wal:\n%s", got)
	}
}

// ── zero-rows scans over EVERY pool reference in the DAL ─────────────────────

// poolScanCorpus is the corpus both zero-rows scans below run over: EVERY
// non-test .go file in this package.
//
// 🔴 IT DELIBERATELY DOES NOT FILTER BY FILENAME. The first version of this
// helper globbed `dal*.go` while its own comment claimed to be "derived, not
// enumerated: any file declaring methods on *DAL is in scope" — a claim that was
// false, and false in the worst direction: the comment told the next maintainer
// that a new file was automatically covered. It was not. An independent review
// dropped a `store_review_probe.go` holding two REAL defects (a `d.rdb.Exec` with
// an INSERT inside a `func (d *DAL)` method, and a `putChatOn(d.rdb, m)`) and ALL
// THREE guards stayed GREEN. The `len(out) < 3` floor could not help: there were
// exactly 3 matching files, so the floor was satisfied by the very set that was
// missing the defect.
//
// That is the exact shape this ticket exists to avoid — a list that omits things
// and stays green while omitting them — so the fix is to stop having a list. The
// `dal*.go` prefix never carried any meaning: nothing stops a DAL method, or an
// sqlExecer write seam, from being declared in a file called anything at all, and
// the seam scan's first pass has to see every such declaration or its second pass
// cannot recognise a misrouted call.
func poolScanCorpus(t *testing.T) []string {
	t.Helper()
	all, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package files: %v", err)
	}
	var out []string
	for _, f := range all {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		out = append(out, f)
	}
	// The floor is a liveness check on the glob, nothing more. It is set well
	// below the real count (this package is dozens of files) so that it fails
	// when the glob breaks and never when someone legitimately adds or removes
	// one — a floor tuned to the exact current count is a second list.
	if len(out) < 30 {
		t.Fatalf("found only %d non-test .go files — the glob is broken, and a broken glob is GREEN", len(out))
	}
	return out
}

// TestNoWriteStatementRunsOnTheReadPool is the first zero-rows query: every
// `d.rdb.<call>(sql)` in the DAL must carry a read-only statement.
//
// AST, not grep, on purpose: comments and string literals are not expression
// nodes, so this scan cannot match the prose that explains it — a failure mode
// this repo has recorded before.
func TestNoWriteStatementRunsOnTheReadPool(t *testing.T) {
	writeVerb := regexp.MustCompile(`(?i)\b(insert|update|delete|replace|create|drop|alter|vacuum|pragma|reindex|truncate)\b`)
	fset := token.NewFileSet()
	scanned := 0
	var findings []string

	for _, path := range poolScanCorpus(t) {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		// Walked per FUNCTION so a local alias of the read pool is in scope. An
		// independent review measured `q := d.rdb; q.Exec(INSERT…)` slipping past
		// the file-wide version of this scan; the alias rule is shared with the
		// transaction guard (readPoolAliases) so the two cannot drift into "one
		// knows about aliases, the other does not".
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			aliases := readPoolAliases(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !isReadPoolHandle(sel.X, aliases) {
					return true
				}
				scanned++
				sqlText := concatStringLiterals(call.Args)
				if writeVerb.MatchString(sqlText) {
					findings = append(findings, fmtPos(fset, call.Pos())+" "+sel.Sel.Name+": "+firstLine(sqlText))
				}
				return true
			})
			return true
		})
	}

	if scanned < 20 {
		t.Fatalf("only %d read-pool call sites found — the scan is broken, and a broken scan is GREEN", scanned)
	}
	for _, f := range findings {
		t.Errorf("write statement on the READ pool: %s", f)
	}
	if len(findings) > 0 {
		t.Fatalf("%d statement(s) write through d.rdb. The read pool is opened mode=ro, so in production these fail at runtime; route them to d.wdb.", len(findings))
	}
}

// TestReadPoolIsNeverHandedToAWriteSeam is the second zero-rows query, and it
// covers the ONE hole the compiler genuinely cannot see.
//
// 🔴 `sqlExecer` (write seam) and `sqlQuerier` (read seam) are both satisfied by
// *sql.DB, so `putChatOn(d.rdb, m)` compiles exactly as happily as
// `putChatOn(d.wdb, m)` — and the SQL lives in the seam function, not at the call
// site, so the scan above cannot see it either. That is the shape that would make
// this whole ticket "no worse than before, just wasted": correct in tests (where
// NewDAL makes both handles the same writable handle) and broken in production.
func TestReadPoolIsNeverHandedToAWriteSeam(t *testing.T) {
	fset := token.NewFileSet()

	// pass 1: which package-level funcs take a write seam?
	writeSeams := map[string]bool{}
	files := map[string]*ast.File{}
	for _, path := range poolScanCorpus(t) {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files[path] = file
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Type.Params == nil {
				continue
			}
			// METHODS COUNT TOO (no fn.Recv filter): a write seam declared as a
			// method is just as unsafe to hand the read pool to, and pass 2 can
			// now recognise the `h.f(…)` call form (calleeSimpleName).
			for _, p := range fn.Type.Params.List {
				if id, ok := p.Type.(*ast.Ident); ok && id.Name == "sqlExecer" {
					writeSeams[fn.Name.Name] = true
				}
			}
		}
	}
	if len(writeSeams) < 3 {
		t.Fatalf("found only %d sqlExecer seam functions — the scan cannot be trusted (a broken scan is GREEN)", len(writeSeams))
	}

	// pass 2: is the read pool ever an argument to one of them? Matches both the
	// bare `f(x)` and the method `h.f(x)` call forms, and resolves one-hop local
	// aliases of rdb (shared with the transaction guard).
	var findings []string
	handed := 0
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			aliases := readPoolAliases(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := calleeSimpleName(call.Fun)
				if !writeSeams[name] {
					return true
				}
				handed++
				for _, arg := range call.Args {
					if isReadPoolHandle(arg, aliases) {
						findings = append(findings, fmtPos(fset, arg.Pos())+" "+name+"(…the read pool…)")
					}
				}
				return true
			})
			return true
		})
	}
	if handed == 0 {
		t.Fatal("no call to any write seam was found — the scan is dead, so its silence means nothing")
	}
	for _, f := range findings {
		t.Errorf("the READ pool was handed to a write seam: %s", f)
	}
	if len(findings) > 0 {
		t.Fatalf("%d call(s) pass d.rdb to a function that WRITES. Both seams accept *sql.DB, so this compiles; pass d.wdb.", len(findings))
	}
}

// TestNoTransactionIsOpenedOnTheReadPool is the third zero-rows query, and it
// guards the one hazard WAL introduced that nothing else in this change covers.
//
// 🔴 THE FACT IT PROTECTS (measured on this tree, 2026-08-01). Nothing in this
// repo sets `wal_autocheckpoint` or `journal_size_limit`, so SQLite's defaults
// apply: auto-checkpoint at 1000 pages × 4096 = ~4 MB, and
// `journal_size_limit=-1` meaning the "-wal" file is NEVER truncated, only
// reused. In normal operation that is BOUNDED — writing ~80 MB of rows (main file
// grew to 91 MB) left "-wal" sitting at 4.1 MB the whole time. But auto-checkpoint
// can only run when no reader is pinning an older snapshot, so ONE open read
// transaction makes it UNBOUNDED: measured 4 MB → 67 MB → 196 MB and still
// climbing, and when the reader finally let go the file did NOT shrink back.
//
// 🔴 WHY A GUARD AND NOT A COMMENT. Before this test, production was safe only by
// COINCIDENCE: there happened to be no `rdb.Begin`/`BeginTx` anywhere in non-test
// code — every read is a single Query/QueryRow whose Rows are consumed
// immediately. Nothing enforced that. Adding one read transaction would grow the
// "-wal" file without limit, and the failure has NO signal: correct data, no
// error, nothing logged, just a disk filling up. That is the same shape as the
// single-file-copy guard next door — a hazard WAL introduced, silent when it
// fires, no violator today — and it gets the same treatment.
func TestNoTransactionIsOpenedOnTheReadPool(t *testing.T) {
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, path := range poolScanCorpus(t) {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files[path] = file
	}

	// pass 1: which package-level funcs open a transaction on a handle they were
	// GIVEN? Those are the ones it is unsafe to hand the read pool to.
	txOpeners := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Type.Params == nil {
				continue
			}
			params := map[string]bool{}
			for _, p := range fn.Type.Params.List {
				if !isSQLDBType(p.Type) {
					continue
				}
				for _, name := range p.Names {
					params[name.Name] = true
				}
			}
			if len(params) == 0 {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if recv, method, ok := methodCallOn(n); ok && isBeginMethod(method) && params[recv] {
					txOpeners[fn.Name.Name] = true
				}
				return true
			})
		}
	}

	var findings []string
	beginsSeen := 0
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			aliases := readPoolAliases(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				// (a) direct `<x>.rdb.Begin` and (b) one-hop alias `q.Begin`
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isBeginMethod(sel.Sel.Name) {
					beginsSeen++
					if mentionsReadPool(sel.X) {
						findings = append(findings, fmtPos(fset, call.Pos())+" "+sel.Sel.Name+" on d.rdb")
					} else if id, ok := sel.X.(*ast.Ident); ok && aliases[id.Name] {
						findings = append(findings, fmtPos(fset, call.Pos())+" "+sel.Sel.Name+" on "+id.Name+" (a local alias of the read pool)")
					}
				}
				// (c) the read pool handed to something that begins a tx on it,
				// as a bare function OR a method (see calleeSimpleName).
				if name := calleeSimpleName(call.Fun); txOpeners[name] {
					for _, arg := range call.Args {
						if isReadPoolHandle(arg, aliases) {
							findings = append(findings, fmtPos(fset, arg.Pos())+" "+name+"(…the read pool…) — that function opens a transaction on it")
						}
					}
				}
				return true
			})
			return true
		})
	}

	// Anti-vacuity: the detector must prove it can see the construct at all. The
	// write pool's transactions (inTx, HardDeleteMember, …) are that proof — if
	// this count is zero the scan is dead and its silence means nothing.
	if beginsSeen == 0 {
		t.Fatal("saw zero Begin/BeginTx calls anywhere in the package — the detector is dead, so finding nothing proves nothing")
	}

	for _, f := range findings {
		t.Errorf("a transaction is opened on the READ pool: %s", f)
	}
	if len(findings) > 0 {
		t.Fatalf(`%d transaction(s) opened on the read pool.

WHY THIS IS REFUSED — the consequence, not just the rule: an open read
transaction pins a WAL snapshot, and SQLite cannot auto-checkpoint while any
snapshot is pinned. The "officraft.db-wal" sidecar then grows for as long as the
transaction is open (measured: 4 MB → 67 MB → 196 MB and still climbing), and
because journal_size_limit is -1 it does NOT shrink back when the reader
finishes. Nothing reports this: the data stays correct, no error is raised, the
disk just fills.

WHAT TO DO INSTEAD, when you need several reads that agree with each other:
  - Prefer ONE statement. A join, or a single query with the aggregate you need,
    is consistent by construction and holds no snapshot open.
  - If it must be several reads, do them on the WRITE pool (d.wdb) inside inTx:
    that pool is capped at one connection, so the snapshot it pins is bounded by
    a transaction that is already short by design.
  - Reads that just need to be fast and independent stay as they are — single
    Query/QueryRow on d.rdb, Rows consumed immediately.`, len(findings))
	}
}

// readPoolAliases collects the local identifiers inside one function that hold
// the read pool — `q := d.rdb`, `q = d.rdb`, and `var q *sql.DB = d.rdb`.
//
// 🔴 ALL THREE DECLARATION FORMS, and that is not tidiness. The first version of
// the transaction guard collected only `*ast.AssignStmt`, so `var q *sql.DB =
// d.rdb; q.Begin()` walked straight past it (measured by an independent review)
// — while the prose on openSQLiteReadPool claimed the guard refuses "local
// aliases of rdb", which includes that form. "One hop is guarded, the other hop
// is not" is worse than either: it is a trap for whoever reads the claim.
//
// One hop only, on purpose: two hops (`a := d.rdb; b := a`) and stores into
// struct fields or closures are NOT covered, and that limit is written down
// rather than papered over (see server/CLAUDE.md).
func readPoolAliases(fn *ast.FuncDecl) map[string]bool {
	aliases := map[string]bool{}
	if fn.Body == nil {
		return aliases
	}
	note := func(lhs []ast.Expr, rhs []ast.Expr) {
		for i, r := range rhs {
			if i >= len(lhs) || !mentionsReadPool(r) {
				continue
			}
			if id, ok := lhs[i].(*ast.Ident); ok {
				aliases[id.Name] = true
			}
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt: // q := d.rdb   /   q = d.rdb
			note(v.Lhs, v.Rhs)
		case *ast.ValueSpec: // var q *sql.DB = d.rdb
			lhs := make([]ast.Expr, 0, len(v.Names))
			for _, name := range v.Names {
				lhs = append(lhs, name)
			}
			note(lhs, v.Values)
		}
		return true
	})
	return aliases
}

// isReadPoolHandle reports whether an expression denotes the read pool, either
// directly (`d.rdb`) or through one of this function's local aliases.
func isReadPoolHandle(e ast.Expr, aliases map[string]bool) bool {
	if mentionsReadPool(e) {
		return true
	}
	id, ok := e.(*ast.Ident)
	return ok && aliases[id.Name]
}

// calleeSimpleName renders the name a call is dispatched on, for BOTH a bare
// function (`f(x)` → "f") and a method or package-qualified form (`h.f(x)` →
// "f").
//
// 🔴 The method form is why this exists. Both handoff scans below match a callee
// name against a set of functions that are unsafe to hand the read pool to, and
// the first version compared only `*ast.Ident` — so `h.f(d.rdb)` matched nothing
// (measured), in BOTH this file's transaction guard and the older write-seam
// guard, while CLAUDE.md described the latter as covering "any function". Name
// matching across methods and plain functions is COARSE (a method and a function
// sharing a name are conflated), and that is the safe direction: it can only
// produce more findings to look at, never fewer.
func calleeSimpleName(fun ast.Expr) string {
	switch v := fun.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

// isSQLDBType reports whether an AST type expression is *sql.DB.
func isSQLDBType(e ast.Expr) bool {
	star, ok := e.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "sql" && sel.Sel.Name == "DB"
}

// methodCallOn renders the receiver IDENT and method name of `x.M(...)`.
func methodCallOn(n ast.Node) (recv, method string, ok bool) {
	call, isCall := n.(*ast.CallExpr)
	if !isCall {
		return "", "", false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return "", "", false
	}
	id, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return "", "", false
	}
	return id.Name, sel.Sel.Name, true
}

func isBeginMethod(name string) bool { return name == "Begin" || name == "BeginTx" }

// mentionsReadPool reports whether an expression names the read pool field.
func mentionsReadPool(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "rdb" {
			found = true
		}
		return !found
	})
	return found
}

func concatStringLiterals(args []ast.Expr) string {
	var b strings.Builder
	for _, a := range args {
		ast.Inspect(a, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				b.WriteString(strings.Trim(lit.Value, "`\""))
				b.WriteString(" ")
			}
			return true
		})
	}
	return b.String()
}

func fmtPos(fset *token.FileSet, p token.Pos) string {
	pos := fset.Position(p)
	return filepath.Base(pos.Filename) + ":" + strconv.Itoa(pos.Line)
}

// firstLine returns the first NON-EMPTY line: our SQL is written as raw strings
// that start with a newline, so "the first line" is blank and an error message
// carrying it would name a problem without showing it.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return "(no SQL literal at this call site)"
}
