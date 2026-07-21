// T-684c tests for the trash reaper. TWO DIRECTIONS, both mandatory:
//
//	A. the POSITIVE direction — things inside <workdir>/trash DO get removed.
//	B. the NEGATIVE direction — everything else DOES NOT get touched: siblings of
//	   trash inside the workdir, other agents' workdirs, the agents root itself,
//	   and anything outside the agents root reachable through a planted symlink.
//
// Direction B is the one that matters. A guard that never fires proves nothing, so
// every refusal case ALSO asserts the would-be victim still exists on disk afterwards
// (the negative control), not merely that purgeTrash returned false.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// trashFixture builds <root>/<agent>/{trash/{junk.txt,sub/deep.txt},keep.txt,tmp/x.txt}
// and a SIBLING agent dir with its own trash, then returns (root, workdir).
func trashFixture(t *testing.T) (root, workdir string) {
	t.Helper()
	// EvalSymlinks: t.TempDir() is under /var on macOS, itself a symlink to
	// /private/var. Resolving up front keeps the fixture's own paths honest so a
	// failing assertion means a real bug, not a platform quirk.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(TempDir): %v", err)
	}
	root = filepath.Join(base, "agents")
	workdir = filepath.Join(root, "m-1a2b")
	mk := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	mk(filepath.Join(workdir, "trash", "junk.txt"), "junk")
	mk(filepath.Join(workdir, "trash", "sub", "deep.txt"), "deep")
	mk(filepath.Join(workdir, "keep.txt"), "keep")
	mk(filepath.Join(workdir, "tmp", "x.txt"), "x")
	mk(filepath.Join(workdir, ".oc-token"), "tok")
	mk(filepath.Join(root, "m-other", "trash", "theirs.txt"), "theirs")
	mk(filepath.Join(root, "m-other", "keep.txt"), "keep")
	return root, workdir
}

func mustExist(t *testing.T, path, why string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("%s: %s should still exist, got %v", why, path, err)
	}
}

func mustNotExist(t *testing.T, path, why string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s: %s should be gone, got err=%v", why, path, err)
	}
}

// capturingLogf records the refusal lines so a test can assert the OBSERVABLE
// SIGNAL exists — a silent skip is a defect, not a pass.
func trashLogf(sink *[]string) func(string, ...any) {
	return func(format string, a ...any) {
		_ = a
		*sink = append(*sink, format)
	}
}

// ── direction A: the trash IS removed ────────────────────────────────────────

func TestPurgeTrash_RemovesTrashTree(t *testing.T) {
	root, workdir := trashFixture(t)
	if !purgeTrash(root, workdir, nil) {
		t.Fatal("purgeTrash returned false on a well-formed trash dir")
	}
	mustNotExist(t, filepath.Join(workdir, "trash"), "direction A")
	mustNotExist(t, filepath.Join(workdir, "trash", "sub", "deep.txt"), "direction A")
}

func TestPurgeTrash_AbsentTrashIsQuietNoop(t *testing.T) {
	root, workdir := trashFixture(t)
	if err := os.RemoveAll(filepath.Join(workdir, "trash")); err != nil {
		t.Fatal(err)
	}
	var logs []string
	if purgeTrash(root, workdir, trashLogf(&logs)) {
		t.Fatal("purgeTrash claimed a purge with no trash dir present")
	}
	// Absent trash is the NORMAL state, not an anomaly: no refusal noise.
	for _, l := range logs {
		if strings.Contains(l, "REFUSED") {
			t.Fatalf("absent trash logged a refusal: %q", l)
		}
	}
	mustExist(t, filepath.Join(workdir, "keep.txt"), "direction B")
}

// ── direction B: NOTHING outside <workdir>/trash is touched ──────────────────

func TestPurgeTrash_LeavesEverythingOutsideTrashAlone(t *testing.T) {
	root, workdir := trashFixture(t)
	if !purgeTrash(root, workdir, nil) {
		t.Fatal("purgeTrash returned false on a well-formed trash dir")
	}
	// (a) siblings of trash INSIDE the same workdir — the exact files the old
	// `rm -rf <workdir>/tmp/...` habit targeted.
	mustExist(t, filepath.Join(workdir, "keep.txt"), "direction B")
	mustExist(t, filepath.Join(workdir, "tmp", "x.txt"), "direction B")
	mustExist(t, filepath.Join(workdir, ".oc-token"), "direction B")
	mustExist(t, workdir, "direction B")
	// (b) ANOTHER agent's workdir, trash included — one agent's teardown must
	// never reap a neighbour.
	mustExist(t, filepath.Join(root, "m-other", "trash", "theirs.txt"), "direction B")
	mustExist(t, filepath.Join(root, "m-other", "keep.txt"), "direction B")
	// (c) the agents root itself.
	mustExist(t, root, "direction B")
}

func TestPurgeTrash_RefusesMalformedShapes(t *testing.T) {
	// Each case names a shape someone could steer the reaper at. `victim` is the
	// path that MUST survive — the negative control that proves the guard is what
	// saved it, not luck.
	cases := []struct {
		name string
		// mutate returns (root, workdir, victim) from the fixture.
		mutate func(t *testing.T, root, workdir string) (string, string, string)
	}{
		{
			// agent id "../.." — Join(root, id) escapes the agents root entirely.
			name: "workdir escapes root via ..",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				return root, filepath.Join(root, "..", ".."), filepath.Dir(root)
			},
		},
		{
			// The unclean form of the same attack, before Clean() collapses it.
			name: "unclean workdir with embedded ..",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				return root, root + "/m-1a2b/../m-other", filepath.Join(root, "m-other", "trash", "theirs.txt")
			},
		},
		{
			// An empty workdir string: Join("") would yield the bare relative
			// "trash", i.e. whatever the daemon's CWD happens to be.
			name: "empty workdir",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				return root, "", filepath.Join(root, "m-1a2b", "trash", "junk.txt")
			},
		},
		{
			name: "empty root",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				return "", workdir, filepath.Join(workdir, "trash", "junk.txt")
			},
		},
		{
			name: "relative workdir",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				return root, "agents/m-1a2b", filepath.Join(workdir, "trash", "junk.txt")
			},
		},
		{
			// workdir == root would make <root>/trash the target — a grandchild of
			// the tree, not an agent's own dir.
			name: "workdir is the agents root itself",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				if err := os.MkdirAll(filepath.Join(root, "trash"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "trash", "rootlevel.txt"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return root, root, filepath.Join(root, "trash", "rootlevel.txt")
			},
		},
		{
			// A workdir NESTED deeper than one level under the root — not a shape
			// the spawn ever produces, so it is not ours to delete.
			name: "workdir is a grandchild of root",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				deep := filepath.Join(workdir, "nested")
				if err := os.MkdirAll(filepath.Join(deep, "trash"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(deep, "trash", "n.txt"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return root, deep, filepath.Join(deep, "trash", "n.txt")
			},
		},
		{
			// A sibling root with a shared PREFIX — the case a HasPrefix
			// containment check would wave through.
			name: "prefix-sibling root",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				evil := root + "EVIL"
				wd := filepath.Join(evil, "m-1a2b")
				if err := os.MkdirAll(filepath.Join(wd, "trash"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(wd, "trash", "e.txt"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return root, wd, filepath.Join(wd, "trash", "e.txt")
			},
		},
		{
			// R1 (found in review, NOT by the first cut of these tests): the
			// WORKDIR ITSELF is a symlink pointing out of the agents root. It
			// satisfies every STRING-level guard — absolute, clean, and
			// Dir(workdir)==root textually — so before G7 was fixed this deleted
			// <symlink target>/trash outside the tree and reported success. Same
			// pathology as the agentsEVIL prefix-sibling case, via the filesystem
			// instead of via the string.
			name: "workdir is a symlink out of the agents root",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				outside := filepath.Join(filepath.Dir(root), "precious-home")
				if err := os.MkdirAll(filepath.Join(outside, "trash", "ownerdata"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(outside, "trash", "ownerdata", "irreplaceable.txt"), []byte("owner data"), 0o600); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(root, "m-linked")
				if err := os.Symlink(outside, link); err != nil {
					t.Fatal(err)
				}
				return root, link, filepath.Join(outside, "trash", "ownerdata", "irreplaceable.txt")
			},
		},
		{
			// R2: the same trick aimed INSIDE the tree — one agent's workdir
			// symlinked at a NEIGHBOUR's dir. Textually impeccable; would have
			// reaped somebody else's trash.
			name: "workdir is a symlink to a neighbour agent",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				link := filepath.Join(root, "m-linked")
				if err := os.Symlink(filepath.Join(root, "m-other"), link); err != nil {
					t.Fatal(err)
				}
				return root, link, filepath.Join(root, "m-other", "trash", "theirs.txt")
			},
		},
		{
			// THE headline case: `trash` is a symlink pointing OUT of the workdir.
			name: "trash is a symlink out of the workdir",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				outside := filepath.Join(filepath.Dir(root), "precious")
				if err := os.MkdirAll(outside, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(outside, "data.txt"), []byte("owner data"), 0o600); err != nil {
					t.Fatal(err)
				}
				trash := filepath.Join(workdir, "trash")
				if err := os.RemoveAll(trash); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, trash); err != nil {
					t.Fatal(err)
				}
				return root, workdir, filepath.Join(outside, "data.txt")
			},
		},
		{
			// trash exists but is a plain FILE — not the dir shape we own.
			name: "trash is a regular file",
			mutate: func(t *testing.T, root, workdir string) (string, string, string) {
				trash := filepath.Join(workdir, "trash")
				if err := os.RemoveAll(trash); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(trash, []byte("not a dir"), 0o600); err != nil {
					t.Fatal(err)
				}
				return root, workdir, trash
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, workdir := trashFixture(t)
			r, wd, victim := c.mutate(t, root, workdir)
			var logs []string
			if purgeTrash(r, wd, trashLogf(&logs)) {
				t.Fatalf("purgeTrash claimed a purge for malformed shape root=%q workdir=%q", r, wd)
			}
			// (1) the would-be victim survived — the negative control.
			mustExist(t, victim, "direction B "+c.name)
			// (2) fail-closed means LOUD, not silent: a refusal must be observable
			// in the warden err.log.
			refused := false
			for _, l := range logs {
				if strings.Contains(l, "REFUSED") {
					refused = true
				}
			}
			if !refused {
				t.Fatalf("refusal was SILENT (no REFUSED line) for %s; logs=%v", c.name, logs)
			}
			// (3) the honest workdir's own contents are untouched either way.
			mustExist(t, filepath.Join(workdir, "keep.txt"), "direction B "+c.name)
		})
	}
}

// A symlinked ANCESTOR (macOS /var -> /private/var is the real-world instance) is
// legitimate and must NOT be refused — otherwise the reaper never runs in
// production and the whole feature is theatre.
func TestPurgeTrash_SymlinkedAncestorStillPurges(t *testing.T) {
	root, workdir := trashFixture(t)
	linkRoot := filepath.Join(filepath.Dir(root), "agents-link")
	if err := os.Symlink(root, linkRoot); err != nil {
		t.Fatal(err)
	}
	linkedWorkdir := filepath.Join(linkRoot, "m-1a2b")
	if !purgeTrash(linkRoot, linkedWorkdir, nil) {
		t.Fatal("purgeTrash refused a legitimate symlinked-ancestor path")
	}
	mustNotExist(t, filepath.Join(workdir, "trash"), "symlinked ancestor")
	mustExist(t, filepath.Join(workdir, "keep.txt"), "symlinked ancestor")
}

// ── the two HOOKS: the reaper must actually be reachable from both timings ────
//
// purgeTrash being correct is worthless if nothing calls it. These pin the two
// call sites T-684c promises: spawn (a fresh generation starts on a clean dir) and
// teardown (the stop ladder's exit).

func TestStart_InvokesPurgeTrashHook(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	pidKey := "tmux -L officraft display-message -p -t member-alice #{pane_pid}"
	run := &recRunner{
		out: map[string]string{pidKey: "4242\n"},
		err: map[string]error{hasKey: errAbsent()},
	}
	deps := newStartDeps(t, run, map[string]string{})
	calls := 0
	deps.PurgeTrash = func() { calls++ }

	out := deps.start(StartParams{
		MemberID:       "alice",
		PersonaContext: "P",
		MemberToken:    fxToken,
		Role:           "assistant",
		Model:          fxModel,
		SessionName:    "member-alice",
	})
	if !out.OK {
		t.Fatalf("spawn failed: %s", out.Reason)
	}
	if calls != 1 {
		t.Fatalf("spawn-time trash purge ran %d times, want exactly 1", calls)
	}
}

func TestStop_InvokesPurgeTrashHook(t *testing.T) {
	sw := quietSweep()
	calls := 0
	sw.purgeTrash = func() { calls++ }
	rec := &killRecorder{probeErr: syscall.ESRCH}
	stop(absentAfterKill(), tmuxSocket, "member-x", rec.fn, leaderPgid, sw)
	if calls != 1 {
		t.Fatalf("teardown-time trash purge ran %d times, want exactly 1", calls)
	}
}

// The outer gate refuses foreign sessions BEFORE anything destructive — the trash
// reaper must sit behind that gate too, not in front of it.
func TestStop_ForeignSessionNeverPurgesTrash(t *testing.T) {
	sw := quietSweep()
	calls := 0
	sw.purgeTrash = func() { calls++ }
	rec := &killRecorder{probeErr: syscall.ESRCH}
	stop(absentAfterKill(), tmuxSocket, "some-unrelated-session", rec.fn, leaderPgid, sw)
	if calls != 0 {
		t.Fatalf("trash purge ran %d times for a foreign session, want 0", calls)
	}
}
