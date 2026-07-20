package main

// worker_residue_coupling_test.go — T-108b, round-3 rework.
//
// WHY THIS FILE EXISTS AT ALL, AND WHY IT SHIPS IN THE *DOCS* COMMIT.
//
// Round 2 shipped a claim that its two commits were independently revertible.
// Round 3 disproved it, and I re-measured the same failure for MY OWN pair
// before writing this file:
//
//	git revert -n <code commit>   → `go test ./...` STAYS GREEN (28.9s, ok)
//	                                 …while all four member-only residues come
//	                                 back into the worker's boot context.
//
// The mechanism is structural and it will bite anyone who tries again: the
// assertions about a change live in the same commit as the change, so reverting
// the change deletes its own guards. A test cannot guard the commit it ships in.
//
// So this file ships in the DOCS commit, not the code commit. Reverting the code
// commit leaves this file in the tree, still compiling, and it goes RED on the
// assertion itself — which is the failure mode that actually tells you what is
// wrong, rather than a build error whose easiest "fix" is deleting the file.
//
// It depends ONLY on helpers that predate this rework (crossrefWorkerCtx /
// crossrefMemberCtx from worker_crossref_test.go, which is part of neither of
// this rework's two commits), for the same reason.
//
// COUPLING, both directions (each measured with `git revert -n`):
//
//	revert the code commit alone → RED here (the dangerous direction: the
//	                               residues return, i.e. a safety regression)
//	revert the docs commit alone → RED, but NOT here — this file is deleted by
//	                               that revert. It is caught by
//	                               TestWorkerOverlayDoesNotClaimBlanketRemoval,
//	                               which ships in the code commit precisely so
//	                               it survives. Each commit carries the guard
//	                               for the OTHER one's revert.
//
// Both directions were measured, before and after, with `git revert -n` + a full
// package run. Before this pairing existed, BOTH directions were silent-green.

import (
	"strings"
	"testing"
)

// residueCouplingCases are the four member-only residues found by the third
// independent review, all by reading the assembled document line by line.
//
// Deliberately duplicated from the regression list in worker_sharedcore_test.go
// — same discipline as crossrefWorkerCtx duplicating workerCtx. The copy in the
// code commit is the readable canonical list; THIS copy is the one that survives
// a revert of that commit. Neither is redundant with the other.
var residueCouplingCases = []struct{ name, text, why string }{
	{
		"§10.4 create_task_manual 建類型",
		"`create_task_manual` 建類型",
		"正面 ✅ 示範，叫 worker 去做 §10.1／§10.1b 被排除的理由明文說它不該做的事。",
	},
	{
		"§10.4 你是窗口：轉交／喚醒外包",
		"你是窗口：轉交負責成員，或建好交伺服器喚醒外包",
		"指派讀者身分為派工窗口並叫它轉包；worker 是被發包的那一方（§10.1c 已排除）。",
	},
	{
		"§10.5 你這個協調窗口不代勞",
		"你這個協調窗口不代勞",
		"用第二人稱送給 worker，讀成「§10.5 這三步不用我做」，與 overlay §6 相反。",
	},
	{
		"§5 presence report_waking 祈使開機指示",
		"boot 起手你主動用 MCP `report_waking()` 報一次",
		"帶順序的祈使開機指示，與 overlay §2「report_waking 不在你的開機序列」矛盾。",
	},
}

// TestWorkerResidueRemovalSurvivesPartialRevert is the cross-commit guard.
func TestWorkerResidueRemovalSurvivesPartialRevert(t *testing.T) {
	worker := crossrefWorkerCtx(t)
	member := crossrefMemberCtx(t)

	// Positive control on the ASSEMBLY, before any per-case assertion: a worker
	// context that failed to fold in the shared core would make every "text is
	// absent" assertion below trivially true.
	if len(worker) < 30000 {
		t.Fatalf("worker boot context is only %d bytes — the shared core is not folded "+
			"in, so every absence assertion below is vacuous", len(worker))
	}

	for _, tc := range residueCouplingCases {
		t.Run(tc.name, func(t *testing.T) {
			// Per-case positive control: the member must still get it. Without
			// this, deleting the shared core wholesale would pass.
			if !strings.Contains(member, tc.text) {
				t.Fatalf("positive control failed: %q is absent from the MEMBER fold too, "+
					"so its absence from the worker proves nothing (seed reworded?)", tc.text)
			}
			if strings.Contains(worker, tc.text) {
				t.Errorf("member-only residue is back in the worker's copy: %q\n%s\n"+
					"若你剛 revert 了 T-108b 的程式那一顆：這兩顆必須一起進退。"+
					"單獨退程式那顆會靜默地把這四處成員專屬指示送回外包手上。",
					tc.text, tc.why)
			}
		})
	}
}

// TestWorkerOverlayReportWakingClaimIsTrue guards a defect class one level
// nastier than residue: the document making a FALSE STATEMENT ABOUT ITSELF.
//
// The overlay tells the worker that report_waking "對你不適用". Before this
// rework it went on to claim the tool had been "已從你這份裡拿掉" — while
// report_waking still appeared TWICE in the shared core above it, one of them a
// sequenced imperative. A reader who checks a document's self-description and
// finds it false has no way to know which other self-descriptions to trust.
//
// The property pinned is the one the reworded overlay actually claims: every
// surviving report_waking mention in the shared core is a TOOL-DIRECTORY listing
// (附錄 A), never an instruction to call it. That keeps the claim true without
// deleting the tool directory, which is legitimately descriptive — the judgement
// the previous round got right for 附錄 A and wrong for §5.
func TestWorkerOverlayReportWakingClaimIsTrue(t *testing.T) {
	ctx := crossrefWorkerCtx(t)

	overlayAt := strings.Index(ctx, "# 外包工作者 —— 你與正職成員的差異")
	if overlayAt < 0 {
		t.Fatal("cannot locate the overlay boundary — the split below would be meaningless")
	}
	core := ctx[:overlayAt]

	if len(core) < 20000 {
		t.Fatalf("shared core slice is only %d bytes — assertions below would be "+
			"vacuously true", len(core))
	}
	// Prove the scan has teeth on this document before trusting a clean result.
	if !strings.Contains(core, "report_waking") {
		t.Fatal("no report_waking anywhere in the shared core — the tool directory in " +
			"附錄 A should still list it; a clean scan here means the scan is broken")
	}

	var offenders []string
	for _, line := range strings.Split(core, "\n") {
		if !strings.Contains(line, "report_waking") {
			continue
		}
		if strings.Contains(line, "CLI 指令以 `ocagent --help` 為準") {
			continue // 附錄 A tool directory — a listing, not an instruction
		}
		offenders = append(offenders, line)
	}
	if len(offenders) > 0 {
		t.Errorf("shared core still INSTRUCTS the worker about report_waking (%d line(s)):\n%v\n"+
			"overlay §2 說 report_waking 不在你的開機序列——核心裡任何祈使式的提及都與它矛盾，"+
			"而且出現在它前面。工具目錄列舉是唯一允許的形式。", len(offenders), offenders)
	}

	// The other revert direction: the overlay must not go back to claiming a
	// blanket removal, which is false while 附錄 A still lists the tool.
	if strings.Contains(ctx, "`resume_summary` 對你都不適用（已從你這份裡拿掉）") {
		t.Error("overlay is back to claiming report_waking was removed outright — it " +
			"still appears in the 附錄 A tool directory, so that statement is false")
	}
}
