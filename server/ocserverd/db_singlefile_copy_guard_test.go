package main

// db_singlefile_copy_guard_test.go — T-dd7a.
//
// 🔴 WHY THIS GUARD EXISTS. With WAL turned on (migrate.go openSQLite), the
// database is no longer ONE file: the most recently committed data can still be
// sitting in `officraft.db-wal`, not yet checkpointed into the main file. So a
// construct that copies or moves `officraft.db` ALONE — a `cp` in a script, an
// os.Rename of the db path — now has a failure mode it did not have before, and
// it is the worst shape a backup bug can take: the copy SUCCEEDS, the file looks
// like a database, it opens, it passes integrity_check, and the only thing wrong
// with it is that the most recent work is missing. Nobody finds out until they
// restore it.
//
// 🔴 SHAPE: this is a QUERY THAT MUST RETURN ZERO ROWS, deliberately NOT a list
// of known copy sites. An enumerated list only knows about the copies someone
// already thought of, and the whole risk here is the one added later by someone
// who never read this file. A scan that returns findings cannot be satisfied by
// updating a list — it has to be satisfied by not writing the construct.
//
// 🔴 `VACUUM INTO` (backup.go) IS NOT THIS SHAPE AND MUST NOT BE FLAGGED. It is
// SQLite's own online backup: the engine reads its own pages, WAL included, and
// writes one already-consistent file. The guard therefore looks for FILESYSTEM
// copy/move verbs applied to the database PATH, which is exactly what VACUUM
// INTO is not — a false positive there would push the next person off the one
// mechanism that is actually correct.
//
// HOW THE TWO HALVES ARE SPLIT, and why it matters:
//
//   - Go code is scanned by AST. Comments and string literals are not expression
//     nodes, so the scanner cannot match prose — including the prose in THIS
//     file. That failure mode is not hypothetical in this repo (see CLAUDE.md on
//     authz_surface_gate_test.go: "grep matched its own explanation").
//   - Scripts (shell and friends) are scanned textually, because there is no
//     cheap AST for them. This guard is a .go file, so it is NEVER in the textual
//     corpus — the scanner needs no exemption for itself, by construction rather
//     than by a skip-list someone has to maintain.
//
// HONEST LIMITS (do not read this guard as more than it is): it catches the
// database path handed DIRECTLY to a copy/move verb. It does not do dataflow, so
// `f, _ := os.Open(dbPath); io.Copy(dst, f)` through several assignments can slip
// past, and it cannot see a copy performed by a program this repo merely calls.
// It is a tripwire on the shape people actually write, not a proof.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRootFromPackage is this package's path to the repo root (the same
// convention as doc_cap_mirror_test.go's "../../bin/tests/...").
const repoRootFromPackage = "../.."

// dbPathIdentRe matches an identifier that NAMES the database file. Deliberately
// narrow: it is the names our own code uses for it (dbPath, DB_PATH, dbFile,
// databasePath), not every variable that might transitively hold it.
var dbPathIdentRe = regexp.MustCompile(`(?i)^(db|database|sqlite)(_?(path|file|name))?$`)

// dbLiteralRe matches a string literal that names a database file by extension.
var dbLiteralRe = regexp.MustCompile(`\.db(-wal|-shm)?$|officraft\.db`)

// goCopyVerbs are the Go calls that copy or move ONE file. `os.Rename` and
// `os.Link` are the move/hardlink shapes; `io.Copy` is the stream shape;
// `os.WriteFile`/`os.ReadFile` are the read-it-all-then-write-it-all shape.
var goCopyVerbs = map[string]bool{
	"os.Rename":     true,
	"os.Link":       true,
	"os.Symlink":    true,
	"io.Copy":       true,
	"io.CopyBuffer": true,
	"os.WriteFile":  true,
	"os.ReadFile":   true,
}

// shellCopyVerbRe matches a shell copy/move invocation at a command position
// (start of line, or after a separator/pipe) so that the word "cp" inside an
// identifier or a path does not count.
var shellCopyVerbRe = regexp.MustCompile(`(^|[;&|(]|\s)(cp|mv|rsync|ditto|install)\s`)

// shellDBTokenRe matches a shell token that names the database FILE. A whole
// DIRECTORY move (what bin/install.sh's uninstall does: the entire
// ~/.officraft tree into a .bak-<ts> sibling) is intentionally NOT this shape —
// it carries `-wal` and `-shm` along with the main file, which is exactly the
// safe way to move a database at rest.
var shellDBTokenRe = regexp.MustCompile(`officraft\.db|\$\{?DB_PATH|\.db["'\s)]|\.db$`)

// scriptExts is the non-Go corpus: things that can actually execute a copy.
var scriptExts = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".py": true,
	".yml": true, ".yaml": true,
}

type copyFinding struct {
	file string
	line int
	what string
}

// TestNothingCopiesTheDatabaseAsASingleFile is the zero-rows query.
func TestNothingCopiesTheDatabaseAsASingleFile(t *testing.T) {
	goFindings, goStats := scanGoForSingleFileDBCopies(t)
	scriptFindings, scriptStats := scanScriptsForSingleFileDBCopies(t)

	// ── anti-vacuity: a scanner that read nothing would pass silently ────────
	//
	// The bare file counts only prove the walk ran. The VERB counts prove
	// something stronger and specific to this guard: the detector really does
	// recognise the constructs it is hunting, and the zero findings above are
	// therefore "the verbs are here but never aimed at the database" — not "the
	// detector is blind". Both corpora contain such verbs today (backup.go
	// renames its .partial into place; the install script copies binaries).
	if goStats.files < 50 {
		t.Fatalf("go corpus is implausibly small (%d files) — the walk is broken, and a broken walk is GREEN", goStats.files)
	}
	if scriptStats.files < 5 {
		t.Fatalf("script corpus is implausibly small (%d files) — the walk is broken", scriptStats.files)
	}
	if goStats.verbs == 0 {
		t.Fatal("saw zero single-file copy/move calls anywhere in the Go tree — the verb detector is dead, so its silence means nothing")
	}
	if scriptStats.verbs == 0 {
		t.Fatal("saw zero copy/move commands anywhere in the scripts — the verb detector is dead, so its silence means nothing")
	}

	findings := append(goFindings, scriptFindings...)
	if len(findings) == 0 {
		return
	}
	for _, f := range findings {
		t.Errorf("%s:%d copies or moves the database as a SINGLE file (%s)", f.file, f.line, f.what)
	}
	t.Fatalf(`%d construct(s) copy the database as one file.

In WAL mode the newest committed data may still be in the "-wal" sidecar, so a
single-file copy produces a database that opens cleanly and is silently missing
the most recent work. Use one of these instead:

  - an ONLINE snapshot: "VACUUM INTO" (server/ocserverd/backup.go already does
    this, and it is WAL-safe by construction), or the "ocserverd backup" command;
  - AT REST (server stopped): take all THREE files together — officraft.db,
    officraft.db-wal, officraft.db-shm — or move the whole directory.

See docs/guide/troubleshooting.md ("想自己備份或搬移資料庫").`, len(findings))
}

type scanStats struct{ files, verbs int }

func scanGoForSingleFileDBCopies(t *testing.T) ([]copyFinding, scanStats) {
	t.Helper()
	var findings []copyFinding
	var stats scanStats
	fset := token.NewFileSet()

	walkRepo(t, func(path string) {
		if !strings.HasSuffix(path, ".go") {
			return
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			// A parse failure must not be silently skipped: it would shrink the
			// corpus without shrinking the file count.
			t.Fatalf("parse %s: %v", path, err)
		}
		stats.files++
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			verb := calleeName(call.Fun)
			if !goCopyVerbs[verb] {
				return true
			}
			stats.verbs++
			for _, arg := range call.Args {
				if namesTheDatabase(arg) {
					findings = append(findings, copyFinding{
						file: relPath(path), line: fset.Position(arg.Pos()).Line,
						what: verb + " on the database path",
					})
					break
				}
			}
			return true
		})
	})
	return findings, stats
}

func scanScriptsForSingleFileDBCopies(t *testing.T) ([]copyFinding, scanStats) {
	t.Helper()
	var findings []copyFinding
	var stats scanStats

	walkRepo(t, func(path string) {
		if !scriptExts[filepath.Ext(path)] {
			return
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		stats.files++
		for i, line := range strings.Split(string(body), "\n") {
			code := strings.TrimSpace(line)
			if code == "" || strings.HasPrefix(code, "#") {
				continue // a comment cannot copy a file
			}
			if !shellCopyVerbRe.MatchString(code) {
				continue
			}
			stats.verbs++
			if shellDBTokenRe.MatchString(code) {
				findings = append(findings, copyFinding{
					file: relPath(path), line: i + 1,
					what: "shell copy/move naming the database file",
				})
			}
		}
	})
	return findings, stats
}

// walkRepo visits every tracked-ish source file, skipping build output and
// vendored trees (they are not code this repo decides anything with).
func walkRepo(t *testing.T, visit func(path string)) {
	t.Helper()
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "dist": true, "webdist": true,
		"seedsdist": true, "docsdist": true, "bindist": true, "anchordist": true,
		"coverage": true, "playwright-report": true, "test-results": true,
	}
	root, err := filepath.Abs(repoRootFromPackage)
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		visit(path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func relPath(path string) string {
	root, err := filepath.Abs(repoRootFromPackage)
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

// calleeName renders `os.Rename` / `io.Copy` for a call's function expression.
func calleeName(fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return pkg.Name + "." + sel.Sel.Name
}

// namesTheDatabase reports whether an argument expression names the database
// file — either through an identifier our code uses for it, or a literal path.
func namesTheDatabase(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if dbPathIdentRe.MatchString(v.Name) {
				found = true
			}
		case *ast.SelectorExpr:
			if dbPathIdentRe.MatchString(v.Sel.Name) {
				found = true
			}
		case *ast.BasicLit:
			if v.Kind == token.STRING && dbLiteralRe.MatchString(strings.Trim(v.Value, "`\"")) {
				found = true
			}
		}
		return !found
	})
	return found
}
