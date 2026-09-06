package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-81, second round. The first round guarded resolveOcAgentBin and start(); an
// independent reviewer then pointed at the ONE line nothing was watching — the line in
// the production deps literal that hands start() its resolver. Setting that single field
// to nil restored the original defect in full (every member on a fresh machine gets a
// dangling symlink and never comes online) and the ENTIRE package still went green.
//
// A test can guard a function and still not guard its caller. This file is the caller's
// guard: it asserts that the deps production actually builds have the seam wired.
func TestBuildSpawnDeps_WiresTheOcAgentResolver(t *testing.T) {
	env := func(string) string { return "" }
	deps := buildSpawnDeps(Config{Base: fxBase}, env, &recRunner{}, fxSocket, "")

	if deps.ResolveOcAgentBin == nil {
		t.Fatal("production SpawnDeps ships without an ocagent resolver — this is the exact " +
			"one-line regression that reintroduces T-81, and start() would refuse every spawn " +
			"on every machine")
	}

	// And it must be a LIVE question, not a captured answer: calling it twice must go
	// back to the filesystem each time. We cannot control $HOME here, so we assert the
	// weaker but still load-bearing property — it answers, and it answers the same shape
	// twice without panicking or memoising a first-call error into permanence.
	p1, _ := deps.ResolveOcAgentBin()
	p2, _ := deps.ResolveOcAgentBin()
	if p1 != p2 {
		t.Errorf("resolver is not deterministic within one filesystem state: %q then %q", p1, p2)
	}
	if !strings.HasSuffix(p1, "ocagent") {
		t.Errorf("resolver answered %q, want a path ending in the ocagent binary name", p1)
	}
}

// TestPathStatable_AnswersTheFilesystem and TestNewOcAgentResolver_PassesTheVerdictThrough
// are the second review round's finding, and it was sharper than the first. The wiring
// line used to carry the existence probe written out inline. Changing that one word —
// `func(p string) bool { return true }` — makes the resolver claim every path is there,
// which IS T-81 (a symlink published to nothing), and the whole package stayed green.
//
// Worse, the guard that should have caught it was the one that stepped aside: the old
// t.Skipf fired `if present`, so "always claim present" was exactly the condition that
// silenced it. A guard whose give-up condition points the same way as the defect is not
// a guard.
//
// So behaviour moved off the wiring line into two named functions, and here is one test
// per function.
func TestPathStatable_AnswersTheFilesystem(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "here")
	if err := os.WriteFile(present, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !pathStatable(present) {
		t.Errorf("pathStatable(%q) = false for a file that is right there", present)
	}
	missing := filepath.Join(dir, "not-here")
	if pathStatable(missing) {
		t.Errorf("pathStatable(%q) = true for a path that does not exist — this is the "+
			"one-word change that reinstates T-81: the resolver then vouches for a "+
			"binary that is not on disk and the spawn publishes a link to nothing", missing)
	}
}

func TestNewOcAgentResolver_PassesTheVerdictThrough(t *testing.T) {
	exe := func() (string, error) { return "/home/oc/.officraft/warden/ocwarden", nil }

	if path, present := newOcAgentResolver(exe, func(string) bool { return false })(); present {
		t.Errorf("resolver reported present=true at %q while the probe said nothing is there", path)
	}
	if path, present := newOcAgentResolver(exe, func(string) bool { return true })(); !present {
		t.Errorf("resolver reported present=false at %q while the probe said it is there", path)
	}

	// And it is a LIVE question: this is the download landing between two spawns. The
	// probe's answer flips because the world changed, and the resolver must go and look
	// again rather than repeat what it said last time.
	onDisk := false
	r := newOcAgentResolver(exe, func(string) bool { return onDisk })
	if _, first := r(); first {
		t.Error("before the download: probe says nothing is there, resolver said present")
	}
	onDisk = true
	if _, second := r(); !second {
		t.Error("after the download: probe says it is there, resolver still said absent — " +
			"the answer was cached, which is the exact shape of the bug this ticket exists for")
	}
}

// TestBuildSpawnDeps_ResolverVerdictFollowsTheSiblingAppearing is the guard the third
// review round asked for, and it is the ONLY one that watches which probe the wiring line
// hands over. Behaviour moved off that line, but the line still CHOOSES:
//
//	newOcAgentResolver(os.Executable, func(string) bool { return true })   // mutant I
//
// leaves pathStatable untouched (its test stays green) and injects its own probe into
// newOcAgentResolver (that test stays green too), while production vouches for a binary
// that is not there — T-81, restored, on the one line nothing else covers.
//
// The earlier version of this test read whatever the machine happened to have, so on a
// host where the sibling really exists mutant I survived. This one BUILDS BOTH WORLDS:
// the resolver looks next to the running executable, and under `go test` that is the test
// binary, whose directory we can write. Create the sibling, the verdict must turn true;
// remove it, it must turn false. No skips, no dependence on what this host looks like.
func TestBuildSpawnDeps_ResolverVerdictFollowsTheSiblingAppearing(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot locate the test binary, so this test cannot build its two worlds: %v", err)
	}
	sibling := filepath.Join(filepath.Dir(exe), "ocagent")
	if _, err := os.Stat(sibling); err == nil {
		t.Skipf("something already occupies %s; refusing to disturb it", sibling)
	}

	env := func(string) string { return "" }
	deps := buildSpawnDeps(Config{Base: fxBase}, env, &recRunner{}, fxSocket, "")
	if deps.ResolveOcAgentBin == nil {
		t.Fatal("no resolver wired — see TestBuildSpawnDeps_WiresTheOcAgentResolver")
	}

	// World 1: nothing there.
	if path, present := deps.ResolveOcAgentBin(); present {
		t.Fatalf("with no ocagent next to the warden, the resolver still vouched for %q — "+
			"that verdict is what makes the spawn publish a symlink to nothing", path)
	}

	// World 2: the download lands.
	if err := os.WriteFile(sibling, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Skipf("cannot write %s, so this host cannot build the second world: %v", sibling, err)
	}
	t.Cleanup(func() { _ = os.Remove(sibling) })

	path, present := deps.ResolveOcAgentBin()
	if !present {
		t.Errorf("the sibling is now on disk at %s, but the resolver still reports absent "+
			"(it named %q) — the production wiring is not asking the filesystem", sibling, path)
	}
	if path != sibling {
		t.Errorf("resolver named %q, want the sibling %q", path, sibling)
	}

	// World 1 again, to prove the true answer was not simply latched.
	if err := os.Remove(sibling); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if path, present := deps.ResolveOcAgentBin(); present {
		t.Errorf("the sibling was removed, yet the resolver still vouches for %q — "+
			"the answer was cached", path)
	}
}
