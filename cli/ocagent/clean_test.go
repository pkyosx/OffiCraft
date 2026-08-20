package main

// clean's guards, negative-first.
//
// This command runs on the close-out path, under time pressure, and it is the
// thing agents are told to use INSTEAD of `rm -rf`. So the assertions that
// matter are not the happy path — they are:
//   • a path outside my workdir is REFUSED and the target is byte-identical after
//   • one bad argument among good ones moves NOTHING (no half-done state)
//   • nothing is ever deleted, only moved
// A green happy-path test would be satisfied by `os.RemoveAll`, which is exactly
// the implementation this command exists to replace.

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cleanFixture builds an isolated agents-home with ONE agent workdir and
// returns (cfg, root). Everything the tests create lives under t.TempDir(), so
// a guard that fails open would still not reach anything real.
func cleanFixture(t *testing.T) (Config, string) {
	t.Helper()
	home := t.TempDir()
	cfg := Config{Home: home, ID: "m-test01"}
	root := filepath.Join(home, "m-test01")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return cfg, root
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func run(cfg Config, args ...string) (int, string) {
	var out bytes.Buffer
	rc := cmdClean(cfg, args, &out)
	return rc, out.String()
}

// ── the happy paths, only enough to prove the move actually happens ──────────

func TestCleanQuarantinesAFileRatherThanDeletingIt(t *testing.T) {
	cfg, root := cleanFixture(t)
	target := filepath.Join(root, "tmp", "scratch.log")
	writeFile(t, target, "keep me readable")

	rc, out := run(cfg, target)
	if rc != 0 {
		t.Fatalf("rc = %d, out = %q", rc, out)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("the target should have moved away, err = %v", err)
	}
	// 🔴 THE assertion: it is still readable. A deletion would pass "the target
	// is gone" too — this is what tells the two apart.
	parked := filepath.Join(root, "trash", "tmp", "scratch.log")
	if got := mustReadFile(t, parked); got != "keep me readable" {
		t.Fatalf("quarantined bytes changed: %q", got)
	}
	if !strings.Contains(out, parked) {
		t.Fatalf("output must say where it went; got %q", out)
	}
}

func TestCleanQuarantinesAFolderWithItsContents(t *testing.T) {
	cfg, root := cleanFixture(t)
	dir := filepath.Join(root, "work", "wt-123")
	writeFile(t, filepath.Join(dir, "a", "b.txt"), "nested")

	if rc, out := run(cfg, dir); rc != 0 {
		t.Fatalf("rc = %d, out = %q", rc, out)
	}
	if got := mustReadFile(t, filepath.Join(root, "trash", "work", "wt-123", "a", "b.txt")); got != "nested" {
		t.Fatalf("nested bytes changed: %q", got)
	}
}

// ── the guards: every one of these must refuse AND leave the target alone ────

func TestCleanRefusesEverythingOutsideMyWorkdir(t *testing.T) {
	cfg, root := cleanFixture(t)
	outsideDir := t.TempDir()

	// A neighbour agent's workdir under the SAME agents home — the most likely
	// real mistake, and the one a naive prefix check gets wrong.
	neighbour := filepath.Join(cfg.Home, "m-other9", "notes.md")
	writeFile(t, neighbour, "not mine")

	// A file reachable only by climbing out with ..
	sibling := filepath.Join(cfg.Home, "sibling.txt")
	writeFile(t, sibling, "not mine either")

	// A symlink INSIDE my root pointing at a file outside it: the path looks
	// local, the bytes are not.
	victim := filepath.Join(outsideDir, "victim.txt")
	writeFile(t, victim, "outside")
	link := filepath.Join(root, "looks-local")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	cases := []struct {
		name, arg, untouched string
	}{
		{"neighbour agent workdir", neighbour, neighbour},
		{"dot-dot escape", filepath.Join(root, "..", "sibling.txt"), sibling},
		{"symlink pointing out", link, victim},
		{"filesystem root", string(filepath.Separator), ""},
		{"my workdir itself", root, ""},
		{"the quarantine dir itself", filepath.Join(root, "trash"), ""},
		{"inside the quarantine dir", filepath.Join(root, "trash", "anything"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rc, out := run(cfg, c.arg)
			if rc == 0 {
				t.Fatalf("must refuse %s; rc = 0, out = %q", c.arg, out)
			}
			if !strings.Contains(out, "NOTHING was moved") {
				t.Fatalf("the refusal must say nothing moved; got %q", out)
			}
			if c.untouched != "" {
				if _, err := os.Lstat(c.untouched); err != nil {
					t.Fatalf("a refused run must not touch the target: %v", err)
				}
			}
		})
	}
	// The root itself and the agents home are still whole.
	if _, err := os.Lstat(root); err != nil {
		t.Fatalf("root vanished: %v", err)
	}
}

// 🔴 The all-or-nothing assertion. One bad argument in a list of good ones must
// cost ZERO moves — a command that replaces `rm -rf` may not leave the caller
// guessing which half ran.
func TestCleanMovesNothingWhenAnyArgumentIsRefused(t *testing.T) {
	cfg, root := cleanFixture(t)
	good1 := filepath.Join(root, "tmp", "one.txt")
	good2 := filepath.Join(root, "tmp", "two.txt")
	writeFile(t, good1, "one")
	writeFile(t, good2, "two")
	bad := filepath.Join(cfg.Home, "elsewhere.txt")
	writeFile(t, bad, "elsewhere")

	rc, out := run(cfg, good1, bad, good2)
	if rc == 0 {
		t.Fatalf("must refuse the batch; out = %q", out)
	}
	for _, p := range []string{good1, good2, bad} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("%s must be untouched: %v", p, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "trash")); !os.IsNotExist(err) {
		t.Fatalf("no quarantine directory should have been created, err = %v", err)
	}
}

// ── idempotence: the property that lets an agent re-run this without thinking ─

func TestCleanIsIdempotentAndNeverOverwritesAnEarlierParkedCopy(t *testing.T) {
	cfg, root := cleanFixture(t)
	target := filepath.Join(root, "tmp", "note.txt")

	// (a) a path that is already gone is SUCCESS, not an error.
	rc, out := run(cfg, target)
	if rc != 0 {
		t.Fatalf("missing path must be rc 0; rc = %d, out = %q", rc, out)
	}
	if !strings.Contains(out, "already gone") {
		t.Fatalf("output should say it was already gone; got %q", out)
	}

	// (b) two rounds with the same name park BOTH copies — the first one is not
	// overwritten, because the parked copy is the evidence the agent may still
	// need.
	writeFile(t, target, "first")
	if rc, out := run(cfg, target); rc != 0 {
		t.Fatalf("rc = %d, out = %q", rc, out)
	}
	writeFile(t, target, "second")
	if rc, out := run(cfg, target); rc != 0 {
		t.Fatalf("rc = %d, out = %q", rc, out)
	}
	if got := mustReadFile(t, filepath.Join(root, "trash", "tmp", "note.txt")); got != "first" {
		t.Fatalf("the FIRST parked copy must survive; got %q", got)
	}
	if got := mustReadFile(t, filepath.Join(root, "trash", "tmp", "note.txt-2")); got != "second" {
		t.Fatalf("the second copy must be parked beside it; got %q", got)
	}
}

// The escape that arrives through the EXIT rather than the entrance: the path
// argument is impeccable, but the quarantine directory itself is a symlink, so
// the "move" lands outside the tree. ocwarden refuses to purge a symlinked
// trash for the same reason (cli/CLAUDE.md §5) — the writer has to agree.
func TestCleanRefusesWhenTheQuarantineDirIsASymlink(t *testing.T) {
	cfg, root := cleanFixture(t)
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(root, "trash")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	target := filepath.Join(root, "tmp", "x.txt")
	writeFile(t, target, "mine")

	rc, out := run(cfg, target)
	if rc == 0 {
		t.Fatalf("must not move into a symlinked quarantine; out = %q", out)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("the target must be untouched: %v", err)
	}
	if entries, err := os.ReadDir(elsewhere); err != nil || len(entries) != 0 {
		t.Fatalf("nothing may land outside the tree; entries = %v err = %v", entries, err)
	}
}

// The SAME escape one level deeper — and the one the first version missed.
// `<root>/trash` is a real directory, but `<root>/trash/tmp` is a symlink out;
// MkdirAll and Rename both follow it, so the file leaves the tree while the
// command prints an in-tree path and exits 0. Reachable in two ordinary steps:
// clean a directory that contains an outward symlink (parked under trash/
// intact), then clean a real path whose rel traverses that parked name.
//
// 🔴 The target is TWO levels under the symlinked name on purpose, and that is
// the whole point of this fixture. With `<root>/tmp/x.txt` the destination
// parent is `<root>/trash/tmp` — the symlink itself, which already exists — so
// MkdirAll sees a directory and creates nothing, and "the outside stayed empty"
// held even with the guard moved AFTER MkdirAll. The test could not tell the
// two orders apart. With `<root>/tmp/sub/x.txt` the parent is
// `<root>/trash/tmp/sub`, which does NOT exist, so a guard that runs late lets
// MkdirAll punch a real directory through the link and into the outside tree —
// and the emptiness assertion below finally reddens.
func TestCleanRefusesWhenSomethingUnderTheQuarantineDirIsASymlink(t *testing.T) {
	cfg, root := cleanFixture(t)
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "trash"), 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(root, "trash", "tmp")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	target := filepath.Join(root, "tmp", "sub", "x.txt")
	writeFile(t, target, "mine")

	rc, out := run(cfg, target)
	if rc == 0 {
		t.Fatalf("must not move through a nested symlink; out = %q", out)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("the target must be untouched: %v", err)
	}
	entries, err := os.ReadDir(elsewhere)
	if err != nil || len(entries) != 0 {
		t.Fatalf("nothing may land outside the tree; entries = %v err = %v", entries, err)
	}
}

// ── identity: without an id there is no "my workdir" to be inside of ─────────

func TestCleanRefusesWithoutAnAgentIdentity(t *testing.T) {
	home := t.TempDir()
	stray := filepath.Join(home, "anon", "x.txt")
	writeFile(t, stray, "x")

	for _, cfg := range []Config{
		{Home: home, ID: ""},
		{Home: "", ID: "m-test01"},
	} {
		rc, out := run(cfg, stray)
		if rc == 0 {
			t.Fatalf("must refuse without a resolvable workdir; out = %q", out)
		}
		if _, err := os.Lstat(stray); err != nil {
			t.Fatalf("nothing may move: %v", err)
		}
	}
}

func TestCleanNeedsAtLeastOnePath(t *testing.T) {
	cfg, _ := cleanFixture(t)
	rc, out := run(cfg)
	if rc == 0 {
		t.Fatalf("no arguments must not be a success; out = %q", out)
	}
	if !strings.Contains(out, "usage:") {
		t.Fatalf("it should print usage; got %q", out)
	}
}

// ── the dispatch seam: an agent that reads --help must be able to FIND it ────

func TestCleanIsDispatchedAndAdvertised(t *testing.T) {
	var out bytes.Buffer
	if rc := realMain([]string{"--help"}, func(string) string { return "" }, nil, &out); rc != 0 {
		t.Fatalf("help rc = %d", rc)
	}
	if !strings.Contains(out.String(), "clean") {
		t.Fatalf("`clean` must appear in --help, or nobody finds it: %q", out.String())
	}

	// And it reaches cmdClean rather than falling through to "unknown
	// subcommand" — proven by the argument-count refusal, which only clean says.
	out.Reset()
	rc := realMain([]string{"clean"}, func(string) string { return "" }, nil, &out)
	if rc != 2 || !strings.Contains(out.String(), "ocagent clean <path>") {
		t.Fatalf("clean must be dispatched; rc = %d out = %q", rc, out.String())
	}
	// Kept as-is, deliberately. This line reads like it pins the phrase 「unknown
	// subcommand」 that seeds/system_interaction.md 附錄 A promises, and it does not:
	// rename the phrase in main.go's default arm and this Contains goes vacuously
	// true. That is not fixable by rewriting it — EVERY "output must not look like
	// X" check goes vacuous when X moves, so spelling X as a literal here would buy
	// nothing and would only pull goldenUsage across files.
	//
	// The phrase is pinned POSITIVELY instead, in
	// TestUnknownSubcommandPrintsExactlyTheUnknownBlock (config_test.go), which is
	// where a rename now reddens. What this line still earns its place for is the
	// thing directly above it: `clean` really reached cmdClean. The rc/usage check
	// above is the load-bearing half of that; this is the cheap corroboration.
	if strings.Contains(out.String(), "unknown subcommand") {
		t.Fatalf("clean fell through to the default branch: %q", out.String())
	}
}

// ── the leaf itself is a symlink: all three shapes, defined ─────────────────
//
// 🔴 The regression this pins: `clean <a symlink>` used to resolve the leaf and
// move WHAT IT POINTED AT. Naming `inlink` deleted `d.txt` — a file the caller
// never named — left `inlink` in place (now dangling), and exited 0. For the
// command that replaces `rm -rf` on the close-out path, "reported success while
// moving the wrong file" is the worst failure available.
//
// The contract, one line per shape:
//   • points OUT of the tree  → refuse, touch nothing
//   • points INSIDE the tree  → move the LINK, leave the pointee alone
//   • dangling                → move the LINK (harmless: no bytes are behind it)

func TestCleanMovesTheLinkItselfWhenTheTargetPointsInsideTheTree(t *testing.T) {
	cfg, root := cleanFixture(t)
	pointee := filepath.Join(root, "d.txt")
	writeFile(t, pointee, "do not touch me")
	link := filepath.Join(root, "inlink")
	if err := os.Symlink(pointee, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rc, out := run(cfg, link)
	if rc != 0 {
		t.Fatalf("an in-tree symlink is cleanable; rc = %d out = %q", rc, out)
	}
	// 🔴 THE assertion: the file the caller did NOT name is still there.
	if got := mustReadFile(t, pointee); got != "do not touch me" {
		t.Fatalf("the pointee must be untouched; got %q", got)
	}
	// The thing the caller DID name is gone from where it was...
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("the link the caller named must have moved away; err = %v", err)
	}
	// ...and parked under its OWN name, still a symlink.
	parked := filepath.Join(root, "trash", "inlink")
	fi, err := os.Lstat(parked)
	if err != nil {
		t.Fatalf("the link must be parked as %s: %v", parked, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("what was parked must still be the symlink, not its target: mode = %v", fi.Mode())
	}
	if !strings.Contains(out, parked) {
		t.Fatalf("the output must name where the LINK went, not the pointee; got %q", out)
	}
	// Nothing was parked under the pointee's name — that would mean the pointee moved.
	if _, err := os.Lstat(filepath.Join(root, "trash", "d.txt")); !os.IsNotExist(err) {
		t.Fatalf("the pointee must never be quarantined; err = %v", err)
	}
}

func TestCleanMovesADanglingSymlink(t *testing.T) {
	cfg, root := cleanFixture(t)
	link := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(root, "gone-already.txt"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rc, out := run(cfg, link)
	if rc != 0 {
		t.Fatalf("a dangling link is cleanable; rc = %d out = %q", rc, out)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("the dangling link must have moved; err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "trash", "dangling")); err != nil {
		t.Fatalf("the dangling link must be parked: %v", err)
	}
}

// The third exit shape: a component of the quarantine path is a DANGLING
// symlink pointing outside. The resolved-ancestor pre-check cannot see where it
// aims (EvalSymlinks has nothing to resolve), so this pins that the refusal
// still happens — MkdirAll will not create a directory through a dangling link,
// and phase 2 turns that into a non-zero exit rather than a silent escape.
//
// This is also the shape the deleted post-MkdirAll re-check was supposed to
// catch. It refuses without it, which is half of why that check was dead.
func TestCleanRefusesWhenTheQuarantinePathRunsThroughADanglingSymlink(t *testing.T) {
	cfg, root := cleanFixture(t)
	elsewhere := t.TempDir()
	neverCreated := filepath.Join(elsewhere, "not-there-yet")
	if err := os.MkdirAll(filepath.Join(root, "trash"), 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.Symlink(neverCreated, filepath.Join(root, "trash", "tmp")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	target := filepath.Join(root, "tmp", "sub", "x.txt")
	writeFile(t, target, "mine")

	rc, out := run(cfg, target)
	if rc == 0 {
		t.Fatalf("must not move through a dangling quarantine link; out = %q", out)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("the target must be untouched: %v", err)
	}
	if _, err := os.Lstat(neverCreated); !os.IsNotExist(err) {
		t.Fatalf("nothing may be created outside the tree; err = %v", err)
	}
	if entries, err := os.ReadDir(elsewhere); err != nil || len(entries) != 0 {
		t.Fatalf("nothing may land outside the tree; entries = %v err = %v", entries, err)
	}
}

// ── B-1: the sentence in clean.go's header, made falsifiable ─────────────────
//
// clean.go's header says "Nothing in here may grow an os.RemoveAll of a
// caller-named path". That sentence used to be prose with nothing behind it —
// the exact failure mode this whole command was written to end (a procedure
// written in prose is a second source of truth that nothing keeps in step). So
// it is asserted here, BY NAME, against the file's own syntax tree.
//
// The AST is read rather than the text on purpose: the header comment itself
// contains the string "os.RemoveAll", so a grep would either match its own
// documentation or need a comment-stripper that is itself untested. go/parser
// discards comments, so what is left is only what the compiler will run.
//
// Scope: os.Remove and os.RemoveAll both DELETE, and this command's whole
// contract is that a wrong path costs a move and never the file — so both are
// refused, not just the one the sentence happens to name.
func TestCleanNeverDeletesAnything(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "clean.go", nil, 0)
	if err != nil {
		t.Fatalf("parse clean.go: %v", err)
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
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "os" {
			return true
		}
		if sel.Sel.Name == "Remove" || sel.Sel.Name == "RemoveAll" {
			t.Errorf("clean.go:%d calls os.%s — this command quarantines, it never deletes; "+
				"a wrong path must cost a move, not the file",
				fset.Position(call.Pos()).Line, sel.Sel.Name)
		}
		return true
	})
}

// ── N-1: the exit guard proved "in the tree", not "in the quarantine" ────────
//
// The escape it missed is INSIDE the tree, which is why "under root" waved it
// through: `<root>/trash/tmp -> <root>/live`. Cleaning `<root>/tmp/sub/x.txt`
// exited 0 and printed `<root>/trash/tmp/sub/x.txt` while the file actually
// landed in `<root>/live/sub/x.txt` — `trash/` held nothing.
//
// 🔴 Reporting success while naming a path the file is not at is the exact
// class this command calls its worst failure. The file being "still in the
// tree" does not soften it: the agent is told where its parked copy is, and it
// is not there. The destination of a quarantine move has to be inside the
// QUARANTINE, and "under root" is a strictly weaker claim.
func TestCleanRefusesWhenTheQuarantinePathLeavesTrashWithoutLeavingTheTree(t *testing.T) {
	cfg, root := cleanFixture(t)
	inTreeButNotTrash := filepath.Join(root, "live")
	if err := os.MkdirAll(inTreeButNotTrash, 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "trash"), 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.Symlink(inTreeButNotTrash, filepath.Join(root, "trash", "tmp")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// Two levels under the symlinked name, for the same reason the sibling
	// test spells out: at one level MkdirAll creates nothing and the fixture
	// cannot tell a late guard from an early one.
	target := filepath.Join(root, "tmp", "sub", "x.txt")
	writeFile(t, target, "mine")

	rc, out := run(cfg, target)
	if rc == 0 {
		t.Fatalf("a destination outside trash/ must be refused; out = %q", out)
	}
	if got := mustReadFile(t, target); got != "mine" {
		t.Fatalf("the target must be untouched; got %q", got)
	}
	// 🔴 THE assertion: nothing was written into the in-tree-but-not-quarantine
	// directory the link aimed at.
	entries, err := os.ReadDir(inTreeButNotTrash)
	if err != nil || len(entries) != 0 {
		t.Fatalf("nothing may land outside %s/; entries = %v err = %v", quarantineDirName, entries, err)
	}
}

// ── N-2: the "trash is not cleanable" guard fell to a single capital letter ──
//
// insideRoot compared the first path segment against "trash" as a
// case-SENSITIVE string, and macOS APFS is case-INSENSITIVE by default — the
// filesystem this repo is developed on. So `clean <root>/TRASH/tmp/parked.txt`
// named the very same directory the guard exists to protect, missed the
// comparison, and re-quarantined an already-quarantined file into
// `trash/TRASH/...` — "moving trash into trash nests forever", which is the
// comment's own words for what it was preventing.
//
// The assertion is filesystem-independent: on a case-sensitive host `TRASH/`
// is a different directory and refusing it costs one false refusal (the agent
// renames it and retries); on a case-insensitive host it is the quarantine
// itself and allowing it costs a spurious move of parked evidence. A command
// that moves things takes the refusal.
func TestCleanRefusesTheQuarantineDirWhateverItsCase(t *testing.T) {
	cfg, root := cleanFixture(t)
	parked := filepath.Join(root, "TRASH", "tmp", "parked.txt")
	writeFile(t, parked, "already quarantined")

	rc, out := run(cfg, parked)
	if rc == 0 {
		t.Fatalf("a path through the quarantine dir must be refused whatever its case; out = %q", out)
	}
	if !strings.Contains(out, "already in") {
		t.Fatalf("the refusal must say it is already quarantined; got %q", out)
	}
	if got := mustReadFile(t, parked); got != "already quarantined" {
		t.Fatalf("parked evidence must be untouched; got %q", got)
	}
	// Nothing nested a second time.
	if _, err := os.Lstat(filepath.Join(root, "trash", "TRASH")); !os.IsNotExist(err) {
		t.Fatalf("quarantine must not nest inside itself; err = %v", err)
	}
}

// ── N-3: the collision loop called every error "it already exists" ───────────
//
// The loop broke only on ENOENT and treated EVERY other Lstat error as "that
// slot is taken", so an ENOTDIR (a plain file sitting where `trash/tmp` should
// be a directory), an ELOOP, or an EACCES span 1000 candidate names and then
// answered `too many quarantined copies of tmp/x.txt` — when there are none.
// Nothing unsafe happens, but the message sends a human hunting for 1000
// copies that do not exist, and the real cause (a file in the way) is never
// named. A command whose whole job is "tell the agent where its file went" may
// not answer with a fabricated reason.
func TestCleanReportsTheRealReasonWhenTheQuarantineSlotCannotBeInspected(t *testing.T) {
	cfg, root := cleanFixture(t)
	// A PLAIN FILE where the quarantine subdirectory would have to be.
	writeFile(t, filepath.Join(root, "trash", "tmp"), "not a directory")
	target := filepath.Join(root, "tmp", "x.txt")
	writeFile(t, target, "mine")

	rc, out := run(cfg, target)
	if rc == 0 {
		t.Fatalf("must not report success; out = %q", out)
	}
	if strings.Contains(out, "too many quarantined copies") {
		t.Fatalf("must not invent 1000 copies that do not exist; got %q", out)
	}
	if got := mustReadFile(t, target); got != "mine" {
		t.Fatalf("the target must be untouched; got %q", got)
	}
}

// ── N-4: the fourth leaf shape, which the contract listed only three of ──────
//
// A leaf symlink that is BOTH dangling AND aimed outside the tree
// (`<root>/dangleout -> <elsewhere>/never.txt`). It sits between two documented
// rules — "points OUT of the tree → refused" and "dangling → the link moves" —
// and it takes the second one. That is correct and it is now stated: nothing
// exists at the far end to reach, so the only byte involved is the link itself,
// which is in the tree and is precisely what the caller named. Refusing it
// would strand exactly the debris this command exists to clear (a stale
// node_modules/.bin entry is this shape).
//
// It is asserted here because it was reachable and undocumented, which is the
// same defect as an undocumented refusal: the next reader cannot tell whether
// the behaviour was decided or fell out.
func TestCleanMovesADanglingLinkThatAimsOutsideTheTree(t *testing.T) {
	cfg, root := cleanFixture(t)
	elsewhere := t.TempDir()
	neverCreated := filepath.Join(elsewhere, "never.txt")
	link := filepath.Join(root, "dangleout")
	if err := os.Symlink(neverCreated, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rc, out := run(cfg, link)
	if rc != 0 {
		t.Fatalf("a dangling link is cleanable wherever it aims; rc = %d out = %q", rc, out)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("the link the caller named must have moved; err = %v", err)
	}
	parked := filepath.Join(root, "trash", "dangleout")
	fi, err := os.Lstat(parked)
	if err != nil {
		t.Fatalf("the link must be parked as %s: %v", parked, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("what was parked must still be the symlink: mode = %v", fi.Mode())
	}
	// 🔴 Nothing was created at the far end — "the link moves" must not have
	// meant "the pointee was materialised".
	if _, err := os.Lstat(neverCreated); !os.IsNotExist(err) {
		t.Fatalf("nothing may be created outside the tree; err = %v", err)
	}
	if entries, err := os.ReadDir(elsewhere); err != nil || len(entries) != 0 {
		t.Fatalf("nothing may land outside the tree; entries = %v err = %v", entries, err)
	}
}
