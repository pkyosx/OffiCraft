package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-81's window, built rather than waited for.
//
// The defect only exists on a machine that was JUST installed: the warden is
// running and ocagent has not finished downloading next to it. On a machine that
// is already set up, that window does not exist — which is why "I ran it and it
// worked" proves nothing here. So this test constructs the window out of the same
// parts production uses.
//
// The construction: resolveRepoRoot walks three parents up from os.Executable(),
// so a warden living at <root>/.officraft/warden/ocwarden computes repoRoot =
// <root>, and the fallback lands on <root>/cli/ocagent/ocagent — a path that has
// never existed on any real machine. That is the window, exactly.
//
// MEASURED ON THE PRE-FIX TREE (33021c99, the commit this branch is based on),
// with the same layout: resolveOcAgentBin answered that nonexistent fallback,
// os.Symlink accepted it, and the workdir got a DANGLING link with nothing
// anywhere saying so. Those numbers are the negative control for this test — it
// is asserting the difference, not just that today is fine.
func TestT81Window_HomeInstalledWardenWithNoSiblingYet(t *testing.T) {
	root := t.TempDir()
	wardenDir := filepath.Join(root, ".officraft", "warden")
	if err := os.MkdirAll(wardenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(wardenDir, "ocwarden")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Deliberately NOT creating <wardenDir>/ocagent: the download has not landed.
	executable := func() (string, error) { return exe, nil }

	resolve := newOcAgentResolver(executable, pathStatable)
	path, present := resolve()
	if present {
		t.Fatalf("the window was not built: resolver vouched for %q, but nothing was put there", path)
	}
	if !strings.HasSuffix(path, filepath.Join("cli", "ocagent", "ocagent")) {
		t.Fatalf("resolver answered %q; the window this test means to build is the repoRoot "+
			"fallback, so if the shape changed this test is no longer measuring T-81", path)
	}

	// The spawn must REFUSE here. Pre-fix this was OK:true plus a dangling symlink.
	links := map[string]string{}
	run := &recRunner{err: map[string]error{"tmux -L officraft has-session -t member-alice": errAbsent()}}
	deps := newStartDepsLinks(t, run, map[string]string{}, links)
	deps.ResolveOcAgentBin = resolve

	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if out.OK {
		t.Fatalf("outcome = %+v — in T-81's window the spawn must refuse; OK here means a member "+
			"starts, never connects, and looks exactly like one that crashed", out)
	}
	if !strings.Contains(out.Reason, "ocagent_not_found") || !strings.Contains(out.Reason, path) {
		t.Errorf("Reason = %q, want ocagent_not_found naming %q", out.Reason, path)
	}
	if len(links) != 0 {
		t.Errorf("published %v — a dangling symlink IS the defect", links)
	}

	// And the window closes by itself: the download lands, the next spawn finds it.
	sibling := filepath.Join(wardenDir, "ocagent")
	if err := os.WriteFile(sibling, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run2 := &recRunner{err: map[string]error{"tmux -L officraft has-session -t member-bob": errAbsent()}}
	deps2 := newStartDepsLinks(t, run2, map[string]string{}, links)
	deps2.ResolveOcAgentBin = resolve // the SAME resolver, deliberately

	if out := deps2.start(StartParams{MemberID: "bob", MemberToken: fxToken, SessionName: "member-bob"}); !out.OK {
		t.Fatalf("after the download landed the next spawn must succeed with no warden restart; got %+v", out)
	}
	if got := links["/home/oc/.officraft/agents/bob/ocagent"]; got != sibling {
		t.Errorf("second spawn linked at %q, want the freshly downloaded %q", got, sibling)
	}
}
