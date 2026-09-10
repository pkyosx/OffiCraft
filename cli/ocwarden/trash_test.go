package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// treeOf lists every path under root, relative to it, so a refusal can be shown
// to have changed nothing at all.
func treeOf(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		if rel != "." {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(paths)
	return paths
}

// stageAgentTree builds <root>/agents/<id>/trash/{a,sub/b} and returns the
// agents root and the workdir.
func stageAgentTree(t *testing.T, base, id string) (string, string) {
	t.Helper()
	agents := filepath.Join(base, "agents")
	workdir := filepath.Join(agents, id)
	if err := os.MkdirAll(filepath.Join(workdir, "trash", "sub"), 0o755); err != nil {
		t.Fatalf("stage tree: %v", err)
	}
	for _, f := range []string{
		filepath.Join(workdir, "trash", "a"),
		filepath.Join(workdir, "trash", "sub", "b"),
		filepath.Join(workdir, "keep-me"),
	} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatalf("stage %s: %v", f, err)
		}
	}
	return agents, workdir
}

func TestPurgeTrash(t *testing.T) {
	t.Run("a well-formed workdir loses its trash dir and nothing else", func(t *testing.T) {
		base := t.TempDir()
		agents, workdir := stageAgentTree(t, base, "member-alice")
		var log []string
		logf := func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) }

		if !purgeTrash(agents, workdir, logf) {
			t.Fatal("purgeTrash = false, want true for a staged trash dir")
		}
		want := []string{"agents", "agents/member-alice", "agents/member-alice/keep-me"}
		if got := treeOf(t, base); !reflect.DeepEqual(got, want) {
			t.Errorf("tree after the purge = %v, want %v", got, want)
		}
		wantLog := []string{fmt.Sprintf("[ocwarden trash] purged %q", filepath.Join(workdir, "trash"))}
		if !reflect.DeepEqual(log, wantLog) {
			t.Errorf("log = %#v, want %#v", log, wantLog)
		}
	})

	t.Run("no trash dir is the ordinary state: false, silent, nothing removed", func(t *testing.T) {
		base := t.TempDir()
		agents := filepath.Join(base, "agents")
		workdir := filepath.Join(agents, "member-alice")
		if err := os.MkdirAll(workdir, 0o755); err != nil {
			t.Fatalf("stage: %v", err)
		}
		var log []string
		logf := func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) }

		if purgeTrash(agents, workdir, logf) {
			t.Error("purgeTrash = true, want false when there is nothing to remove")
		}
		if got, want := treeOf(t, base), []string{"agents", "agents/member-alice"}; !reflect.DeepEqual(got, want) {
			t.Errorf("tree = %v, want %v", got, want)
		}
		if len(log) != 0 {
			t.Errorf("log = %#v, want nothing said about a workdir that has no trash", log)
		}
	})

	t.Run("a nil log sink still purges", func(t *testing.T) {
		base := t.TempDir()
		agents, workdir := stageAgentTree(t, base, "member-alice")
		if !purgeTrash(agents, workdir, nil) {
			t.Fatal("purgeTrash = false, want true")
		}
		want := []string{"agents", "agents/member-alice", "agents/member-alice/keep-me"}
		if got := treeOf(t, base); !reflect.DeepEqual(got, want) {
			t.Errorf("tree after the purge = %v, want %v", got, want)
		}
	})

	t.Run("every malformed shape is refused with a reason and touches nothing", func(t *testing.T) {
		base := t.TempDir()
		agents, workdir := stageAgentTree(t, base, "member-alice")
		before := treeOf(t, base)

		cases := []struct {
			name    string
			root    string
			workdir string
			want    string
		}{
			{"an empty root", "", workdir,
				fmt.Sprintf("[ocwarden trash] REFUSED: empty root (%q) or workdir (%q)", "", workdir)},
			{"an empty workdir", agents, "",
				fmt.Sprintf("[ocwarden trash] REFUSED: empty root (%q) or workdir (%q)", agents, "")},
			{"a relative workdir", agents, "agents/member-alice",
				fmt.Sprintf("[ocwarden trash] REFUSED: non-absolute root (%q) or workdir (%q)", agents, "agents/member-alice")},
			{"a relative root", "agents", workdir,
				fmt.Sprintf("[ocwarden trash] REFUSED: non-absolute root (%q) or workdir (%q)", "agents", workdir)},
			{"a workdir carrying ..", agents, agents + "/../agents/member-alice",
				fmt.Sprintf("[ocwarden trash] REFUSED: unclean root (%q) or workdir (%q)", agents, agents+"/../agents/member-alice")},
			{"a root with a trailing slash", agents + "/", workdir,
				fmt.Sprintf("[ocwarden trash] REFUSED: unclean root (%q) or workdir (%q)", agents+"/", workdir)},
			{"a grandchild of the agents root", agents, filepath.Join(workdir, "nested"),
				fmt.Sprintf("[ocwarden trash] REFUSED: workdir %q is not a direct child of agents root %q",
					filepath.Join(workdir, "nested"), agents)},
			{"the agents root itself", agents, agents,
				fmt.Sprintf("[ocwarden trash] REFUSED: workdir %q is not a direct child of agents root %q", agents, agents)},
			{"a sibling tree that merely shares the root's prefix", agents, agents + "EVIL/member-alice",
				fmt.Sprintf("[ocwarden trash] REFUSED: workdir %q is not a direct child of agents root %q",
					agents+"EVIL/member-alice", agents)},
		}
		for _, c := range cases {
			var log []string
			logf := func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) }
			if purgeTrash(c.root, c.workdir, logf) {
				t.Errorf("%s: purgeTrash = true, want false", c.name)
			}
			if !reflect.DeepEqual(log, []string{c.want}) {
				t.Errorf("%s: log = %#v, want %#v", c.name, log, []string{c.want})
			}
		}
		if got := treeOf(t, base); !reflect.DeepEqual(got, before) {
			t.Errorf("a refusal changed the tree: %v, want the untouched %v", got, before)
		}
	})

	t.Run("a trash symlink is refused rather than followed", func(t *testing.T) {
		base := t.TempDir()
		agents := filepath.Join(base, "agents")
		workdir := filepath.Join(agents, "member-alice")
		elsewhere := filepath.Join(base, "precious")
		if err := os.MkdirAll(workdir, 0o755); err != nil {
			t.Fatalf("stage: %v", err)
		}
		if err := os.MkdirAll(elsewhere, 0o755); err != nil {
			t.Fatalf("stage: %v", err)
		}
		if err := os.WriteFile(filepath.Join(elsewhere, "data"), []byte("x"), 0o644); err != nil {
			t.Fatalf("stage: %v", err)
		}
		trash := filepath.Join(workdir, "trash")
		if err := os.Symlink(elsewhere, trash); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		before := treeOf(t, base)

		var log []string
		logf := func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) }
		if purgeTrash(agents, workdir, logf) {
			t.Error("purgeTrash = true, want false for a trash symlink")
		}
		want := []string{fmt.Sprintf("[ocwarden trash] REFUSED: %q is a symlink — refusing to follow it", trash)}
		if !reflect.DeepEqual(log, want) {
			t.Errorf("log = %#v, want %#v", log, want)
		}
		if got := treeOf(t, base); !reflect.DeepEqual(got, before) {
			t.Errorf("tree = %v, want the untouched %v", got, before)
		}
	})

	t.Run("a plain file named trash is not ours", func(t *testing.T) {
		base := t.TempDir()
		agents := filepath.Join(base, "agents")
		workdir := filepath.Join(agents, "member-alice")
		if err := os.MkdirAll(workdir, 0o755); err != nil {
			t.Fatalf("stage: %v", err)
		}
		trash := filepath.Join(workdir, "trash")
		if err := os.WriteFile(trash, []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("stage: %v", err)
		}
		before := treeOf(t, base)

		var log []string
		logf := func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) }
		if purgeTrash(agents, workdir, logf) {
			t.Error("purgeTrash = true, want false for a plain file named trash")
		}
		want := []string{fmt.Sprintf("[ocwarden trash] REFUSED: %q is not a directory (mode %v)", trash, os.FileMode(0o644))}
		if !reflect.DeepEqual(log, want) {
			t.Errorf("log = %#v, want %#v", log, want)
		}
		if got := treeOf(t, base); !reflect.DeepEqual(got, before) {
			t.Errorf("tree = %v, want the untouched %v", got, before)
		}
	})

	t.Run("a workdir symlinked at a neighbour agent does not reap the neighbour", func(t *testing.T) {
		base := t.TempDir()
		agents, victim := stageAgentTree(t, base, "member-victim")
		impostor := filepath.Join(agents, "member-impostor")
		if err := os.Symlink(victim, impostor); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		before := treeOf(t, base)

		var log []string
		logf := func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) }
		if purgeTrash(agents, impostor, logf) {
			t.Error("purgeTrash = true, want false for a workdir symlinked at a neighbour")
		}
		realAgents, err := filepath.EvalSymlinks(agents)
		if err != nil {
			t.Fatalf("resolve agents root: %v", err)
		}
		want := []string{fmt.Sprintf(
			"[ocwarden trash] REFUSED: workdir %q resolves to %q — not the %q child of agents root %q (resolved %q)",
			impostor, filepath.Join(realAgents, "member-victim"), "member-impostor", agents, realAgents)}
		if !reflect.DeepEqual(log, want) {
			t.Errorf("log = %#v, want %#v", log, want)
		}
		if got := treeOf(t, base); !reflect.DeepEqual(got, before) {
			t.Errorf("the neighbour's trash was touched: %v, want %v", got, before)
		}
	})

	t.Run("a workdir symlinked out of the agents root is refused", func(t *testing.T) {
		base := t.TempDir()
		agents := filepath.Join(base, "agents")
		if err := os.MkdirAll(agents, 0o755); err != nil {
			t.Fatalf("stage: %v", err)
		}
		outside := filepath.Join(base, "elsewhere", "member-alice")
		if err := os.MkdirAll(filepath.Join(outside, "trash"), 0o755); err != nil {
			t.Fatalf("stage: %v", err)
		}
		if err := os.WriteFile(filepath.Join(outside, "trash", "a"), []byte("x"), 0o644); err != nil {
			t.Fatalf("stage: %v", err)
		}
		workdir := filepath.Join(agents, "member-alice")
		if err := os.Symlink(outside, workdir); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		before := treeOf(t, base)

		var log []string
		logf := func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) }
		if purgeTrash(agents, workdir, logf) {
			t.Error("purgeTrash = true, want false for a workdir that resolves out of the agents root")
		}
		if len(log) != 1 || !strings.HasPrefix(log[0], "[ocwarden trash] REFUSED: workdir "+fmt.Sprintf("%q", workdir)+" resolves to ") {
			t.Errorf("log = %#v, want one refusal naming the resolved workdir", log)
		}
		if got := treeOf(t, base); !reflect.DeepEqual(got, before) {
			t.Errorf("tree = %v, want the untouched %v", got, before)
		}
	})

	t.Run("an agents root reached through a symlink still purges", func(t *testing.T) {
		base := t.TempDir()
		realBase := filepath.Join(base, "real")
		if err := os.MkdirAll(realBase, 0o755); err != nil {
			t.Fatalf("stage: %v", err)
		}
		stageAgentTree(t, realBase, "member-alice")
		linkedBase := filepath.Join(base, "linked")
		if err := os.Symlink(realBase, linkedBase); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		agents := filepath.Join(linkedBase, "agents")
		workdir := filepath.Join(agents, "member-alice")

		if !purgeTrash(agents, workdir, nil) {
			t.Fatal("purgeTrash = false, want true — an ancestor symlink cancels on both sides")
		}
		want := []string{"agents", "agents/member-alice", "agents/member-alice/keep-me"}
		if got := treeOf(t, realBase); !reflect.DeepEqual(got, want) {
			t.Errorf("tree = %v, want %v", got, want)
		}
	})

	t.Run("an absent workdir is silent and leaves the real agent alone", func(t *testing.T) {
		base := t.TempDir()
		_, workdir := stageAgentTree(t, base, "member-alice")
		before := treeOf(t, base)
		gone := filepath.Join(base, "no-such-root")
		var log []string
		logf := func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) }

		if purgeTrash(gone, filepath.Join(gone, "member-alice"), logf) {
			t.Error("purgeTrash = true, want false when the workdir does not exist")
		}
		if len(log) != 0 {
			t.Errorf("log = %#v, want silence: an absent workdir has no trash dir either", log)
		}
		if got := treeOf(t, base); !reflect.DeepEqual(got, before) {
			t.Errorf("tree = %v, want the untouched %v", got, before)
		}
		if _, err := os.Stat(filepath.Join(workdir, "trash")); err != nil {
			t.Errorf("the staged trash of the real agent was disturbed: %v", err)
		}
	})
}

func TestStderrLogf(t *testing.T) {
	t.Skip("a single fmt.Fprintf to os.Stderr with a newline appended — asserting it would restate the one line it is")
}
