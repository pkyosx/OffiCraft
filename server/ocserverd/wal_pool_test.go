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
	"go/ast"
	"go/parser"
	"go/token"
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

// TestWritePool_ReadThenWriteNeverHitsBusy is the mechanism test, and it is the
// one that disproved the simpler version of this change (branch t-dd7a-wal:
// journal_mode(WAL) plus raising the connection cap, with transactions left at
// the driver default). It models what our own transactions actually do —
// SaveWithDocumentHistories reads the live document inside the very transaction
// that overwrites it — from two INDEPENDENT handles on one file, which is a real
// production shape: `ocserverd backup` and a shell sqlite3 both open their own.
//
// 🔴 With BEGIN DEFERRED this test fails, and it fails for a reason busy_timeout
// cannot fix: a DEFERRED transaction that reads first has to UPGRADE its read
// lock to a write lock, and SQLite answers an upgrade conflict with an INSTANT
// SQLITE_BUSY (waiting would deadlock two upgraders, so the engine does not
// wait). `_txlock=immediate` takes the write lock at BEGIN, where busy_timeout
// does apply. Measured on this tree: DEFERRED → 4 errors incl. SQLITE_BUSY and
// SQLITE_BUSY_SNAPSHOT (517), IMMEDIATE → 0.
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

// ── zero-rows scans over EVERY pool reference in the DAL ─────────────────────

// dalFiles is the DAL surface. It is derived, not enumerated: any file declaring
// methods on *DAL is in scope.
func dalFiles(t *testing.T) []string {
	t.Helper()
	all, err := filepath.Glob("dal*.go")
	if err != nil {
		t.Fatalf("glob dal files: %v", err)
	}
	var out []string
	for _, f := range all {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		out = append(out, f)
	}
	if len(out) < 3 {
		t.Fatalf("found only %d DAL files — the glob is broken, and a broken glob is GREEN", len(out))
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

	for _, path := range dalFiles(t) {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// the receiver must be `<something>.rdb`
			recv, ok := sel.X.(*ast.SelectorExpr)
			if !ok || recv.Sel.Name != "rdb" {
				return true
			}
			scanned++
			sqlText := concatStringLiterals(call.Args)
			if writeVerb.MatchString(sqlText) {
				findings = append(findings, fmtPos(fset, call.Pos())+" "+sel.Sel.Name+": "+firstLine(sqlText))
			}
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
	for _, path := range dalFiles(t) {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files[path] = file
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Type.Params == nil {
				continue
			}
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

	// pass 2: is `.rdb` ever an argument to one of them?
	var findings []string
	handed := 0
	for path, file := range files {
		_ = path
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := call.Fun.(*ast.Ident)
			if !ok || !writeSeams[name.Name] {
				return true
			}
			handed++
			for _, arg := range call.Args {
				sel, ok := arg.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				if sel.Sel.Name == "rdb" {
					findings = append(findings, fmtPos(fset, arg.Pos())+" "+name.Name+"(…d.rdb…)")
				}
			}
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
