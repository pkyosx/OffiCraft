package main

// migration_lock_shared_test.go — T-75 teardown: the helpers that outlived the
// two guards that used to own them.
//
// registrarLocations came from migration_version_scan_t49e7_test.go; gitOut and
// inCI came from migration_upgrade_path_t64_test.go. The first three functions
// below are the parts the lock checks still call, moved here BYTE-FOR-BYTE — a
// teardown that also rewrites what it keeps cannot say afterwards which of the
// two changes moved a result.
//
// ⚠️ WHAT THE REMOVAL IS AND IS NOT CLAIMED TO REST ON. An earlier draft of this
// header said the two files were removed once the lock "was measured to cover
// every mutation they caught". That sentence was FALSE and an independent review
// showed it with one mutant: a third Go migration registered under a COMPUTED
// name — goose.AddNamedMigrationContext(fmt.Sprintf(...), …) — is invisible to
// registrarLocations' AST walk, so the lock never lists it, and because
// migrationLockMinGo equals today's count the anti-vacuity floor passed too. The
// assertion that HAD covered it was gooseAgreesWithTheParse, which died with
// migration_version_scan_t49e7_test.go and had no successor.
//
// gooseGoVersionCount (at the bottom of this file) is that successor, and it was
// verified as a DIFFERENCE, not as "today is green": with the mutant present,
// this tree is red and 86610b31 — the same branch before the successor existed —
// is `ok`.
//
// What is claimed now is narrower and is the honest version: every mutation
// EITHER side has actually been made to produce is caught by what remains. That
// is a statement about a measured set, not about all possible mutations, and it
// is the reason this note names the escape hatch instead of asserting there is
// none.

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// registrarLocations parses every non-test .go file in this package and returns
// version -> "file:line" for each goose registration it can read literally.
func registrarLocations(t *testing.T) map[int64]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	found := map[int64]string{}
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v — a file this scan cannot read is a file it cannot "+
				"clear, so this is a failure and not a skip", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !strings.HasPrefix(sel.Sel.Name, "AddNamedMigration") || len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true // a computed name: the registry still sees it, this locator does not
			}
			nameArg, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			v, err := goose.NumericComponent(nameArg)
			if err != nil {
				return true
			}
			found[v] = fmt.Sprintf("%s:%d", name, fset.Position(call.Lparen).Line)
			return true
		})
	}
	// Anti-vacuity: a scan over an empty corpus finds nothing and looks exactly
	// like a clean tree.
	if files < 20 {
		t.Fatalf("the AST scan read %d files in this package — that corpus is too small to "+
			"be the real one, so a finding of zero registrations would mean nothing", files)
	}
	return found
}

// gitOut keeps stderr. `.Output()` discards it, which turns every git failure
// into the bare string "exit status 128" — and this file's skip message is the
// one place a reader has to be told WHY the baseline could not be read.
func gitOut(args ...string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// inCI reports whether this run is the one that gates a merge. GITHUB_ACTIONS is
// checked first because it is what this repo's workflow actually sets and cannot
// be set by accident; CI is honoured too but is a crowded name (GitLab, Circle,
// Netlify, and plenty of dotfiles export it), which is why it is not alone.
func inCI() bool {
	return os.Getenv("GITHUB_ACTIONS") != "" || os.Getenv("CI") != ""
}

// gooseGoVersionCount asks goose itself how many GO-sourced migrations it holds,
// so the AST walk above has a second, differently-derived number to be checked
// against. registrarLocations reads the SOURCE and can only see a registration
// whose name is a literal; goose reads its own REGISTRY and sees every one. The
// gap between those two is a real escape hatch, and it was measured, not
// imagined: registering a third Go migration as
// `goose.AddNamedMigrationContext(fmt.Sprintf(...), up, down)` is invisible to
// the parse, and because migrationLockMinGo happens to equal today's count the
// anti-vacuity floor waves it through too. Two agreeing numbers derived the same
// way are one number; this is the other way.
//
// The recover is load-bearing. goose answers a duplicate version by PANICKING
// out of a sort comparator (`goose: duplicate version N detected`), and that
// panic would take the whole test binary down — every other test in the package
// disappears with it. Caught here it becomes a string the caller can report.
// sqlSources are the embedded .sql paths already enumerated as source ①; goose
// reports them under the same names, so subtracting them leaves the Go set.
func gooseGoVersionCount(t *testing.T, sqlSources []string) (count int, refusal string) {
	t.Helper()
	goose.SetBaseFS(embeddedMigrations) // the same base runMigrations sets
	isSQL := map[string]bool{}
	for _, p := range sqlSources {
		isSQL[p] = true
	}
	defer func() {
		if r := recover(); r != nil {
			refusal = fmt.Sprint(r)
		}
	}()
	collected, err := goose.CollectMigrations("migrations", 0, math.MaxInt64)
	if err != nil {
		t.Fatalf("collect migrations: %v", err)
	}
	seen := map[int64]bool{}
	for _, m := range collected {
		if isSQL[m.Source] {
			continue
		}
		seen[m.Version] = true
	}
	return len(seen), ""
}
