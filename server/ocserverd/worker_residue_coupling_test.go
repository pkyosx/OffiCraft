package main

import (
	"strings"
	"testing"
)

// TestWorkerSharedCoreStartsWithTheUnfilteredSystemSeed guards the assembled
// path, independently from workerSharedHead's unit-level equality test. A
// future worker-only exclusion or rewrite changes this prefix and fails here.
//
// T-4595 narrowed WHAT this prefix is. It used to be 系統互動 + 啟動程序 glued
// together, because a worker got all three shared blocks grouped at the top.
// The boot sequence is now the recency-authoritative TAIL for workers too —
// same slot as staff — so only the system-interaction seed leads. The tail's
// placement is asserted separately (TestBothBootContextsUseTheSameFourSlots).
func TestWorkerSharedCoreStartsWithTheUnfilteredSystemSeed(t *testing.T) {
	s := newWorkerTestServer(t)
	sys, err := s.root.readSeedFile("system_interaction.md")
	if err != nil {
		t.Fatalf("read system_interaction.md: %v", err)
	}
	// DocRendered, not the raw file: since T-3201 these seeds carry a
	// read-only head above docBodyMarker, and what a READER gets is the two
	// halves joined — the marker line never reaches an agent. The join is
	// spelled here rather than read from the registry, so changing it there
	// comes back red.
	want := strings.TrimSpace(DocRendered(sys, "\n\n"))
	if got := crossrefWorkerCtx(t); !strings.HasPrefix(got, want+"\n\n") {
		t.Fatal("worker boot context no longer starts with the unfiltered system-interaction seed")
	}
}

// TestWorkerLaunchGuidanceIsTheSharedOneNotAReplacement — T-4595.
//
// The worker assembly must carry the shared boot-sequence block rather than a
// worker-only replacement. Runtime routing and the actual boot behavior are
// covered by the end-to-end lifecycle tests; this test only checks the
// assembly boundary.
func TestWorkerLaunchGuidanceIsTheSharedOneNotAReplacement(t *testing.T) {
	ctx := crossrefWorkerCtx(t)
	// The shared 啟動程序 block itself must be there.
	if !strings.Contains(ctx, bootSequenceH1) {
		t.Errorf("worker boot context is missing launch guidance %q", bootSequenceH1)
	}
}
