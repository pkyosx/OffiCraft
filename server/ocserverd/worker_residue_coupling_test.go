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
	want := strings.TrimSpace(sys)
	if got := crossrefWorkerCtx(t); !strings.HasPrefix(got, want+"\n\n") {
		t.Fatal("worker boot context no longer starts with the unfiltered system-interaction seed")
	}
}

// TestWorkerLaunchGuidanceIsTheSharedOneNotAReplacement — T-4595.
//
// The overlay used to carry a full REPLACEMENT boot sequence of its own,
// introduced as authoritative ("你的開機序列以這一節為準"), which (a) claimed
// report_waking was not in a worker's boot sequence — false, the handler routes
// an outsource caller through workerReportWaking on the same endpoint — and (b)
// re-stated the shared three steps, so the two copies could drift apart with
// nothing to notice. The overlay is gone.
//
// What must remain true of the assembled document: the shared boot sequence is
// present, that shared text still covers the worker's one-bound-task case
// itself, and nothing claims the shared steps were removed or overridden.
//
// 🔴 THE "want" LIST USED TO NAME A TOOL, AND THAT WAS THE WRONG THING TO PIN.
// It required the literal string "get_my_task" — an assertion that survived the
// overlay's deletion only because the sentence this package added to the shared
// boot-sequence seed happened to name that tool too. It is being retired
// (T-4595's other half removes it outright and workers read their plan with
// get_task, the same tool staff use), so a guard spelled that way would have
// forced the shared seed to keep advertising a tool that no longer exists —
// exactly the "共用那份對每一個外包說謊" failure this whole ticket is about.
//
// What the assertion was really for is one level up: the ONE-TASK case is
// covered by the SHARED document, not by a replacement overlay. That is what is
// pinned now, by the sentence rather than by the tool name inside it.
func TestWorkerLaunchGuidanceIsTheSharedOneNotAReplacement(t *testing.T) {
	ctx := crossrefWorkerCtx(t)
	if strings.Contains(ctx, "已從你這份的啟動程序裡拿掉") {
		t.Error("worker boot context falsely claims shared boot steps were removed")
	}
	// The shared 啟動程序 block itself must be there. What it SAYS is not pinned
	// here: the owner authors that document by hand, and a test that spells out
	// his sentences turns every edit of his into a red build with nothing wrong
	// (owner 2026-08-15: 「context 寫守衛沒有什麼意義… 需要的是端到端的實測」).
	if !strings.Contains(ctx, bootSequenceH1) {
		t.Errorf("worker boot context is missing launch guidance %q", bootSequenceH1)
	}
	// And the shared seeds must not go back to naming the retired tool. This is
	// a wording tripwire over an ASSEMBLED document, so it cannot catch a
	// rephrasing nobody has written yet — it catches the realistic regression
	// (a revert, or a merge from a branch cut before this one).
	if strings.Contains(ctx, "get_my_task") {
		t.Error("the shared boot documents name get_my_task again — it is being " +
			"retired, and a worker obeying that sentence would call a tool that " +
			"does not exist; workers read their plan with get_task, same as staff")
	}
}
