package main

import (
	"fmt"
	"regexp"
	"strings"
)

// ── shared core / Global Context (T-108b) ────────────────────────────────────
//
// "Global Context" is what the cockpit shows under 全域情境 — THREE blocks:
//
//	1. 系統互動   seeds/system_interaction.md   (read-only seed; its own H1 is
//	                                            literally "# Global Context")
//	2. 使用者自訂 /api/global-context           (owner-editable additive block)
//	3. 啟動程序   seeds/boot_sequence.md        (read-only studio SOP)
//
// Members receive all three via the member fold (assets.go buildBootContext),
// where they are DISPERSED through the assembly (parts 1, 4 and 5 — the boot
// sequence is deliberately the recency-authoritative tail per
// spec/lifecycle.md §2.2). That member path is NOT touched by T-108b and is
// pinned byte-for-byte by conformance/test_lifecycle.py plus
// TestMemberBootContextByteIdenticalToSpecAssembly.
//
// Workers previously received NONE of the three. They now receive all three,
// GROUPED AS THE OPENING SECTION of their boot context (owner requirement),
// minus the regions that do not hold for a worker. Grouping them at the front
// on the worker side does not disturb the member side: the worker assembly is
// its own code path.
//
// Why reference instead of copy: seeds/worker_context.md used to be a second
// full copy of the policy. Both copies landed in one commit (8e573a7); only the
// member copy was ever maintained (6c82e2a) → 29 one-way drifts, three
// statements that contradicted the code, and zero risk language reaching the
// one role authorised to do destructive work itself. Referencing makes that
// class of drift structurally impossible.

// sharedCoreExclusion names one region of a shared block that must NOT reach an
// outsource worker. Anchor is matched as a LINE PREFIX.
//
// Exactly one shape applies:
//   - default (Block and Line both false): Anchor is a markdown heading; the
//     heading AND its whole subtree go, up to the next same-or-higher heading.
//   - Block: Anchor leads a paragraph; that blank-line-delimited block goes.
//   - Line: Anchor leads a single line (e.g. one item of a numbered list);
//     only that line goes.
type sharedCoreExclusion struct {
	Anchor string
	Block  bool
	Line   bool
	Why    string
}

// workerSharedCoreExclusions filters block 1 (系統互動) for a worker.
var workerSharedCoreExclusions = []sharedCoreExclusion{
	{
		Anchor: "## 9. 保存跨 session 的學習",
		Why: "角色 lessons 掛在角色身上，worker 沒有角色。worker 的等價物是任務手冊的" +
			"學習經驗（write_task_learnings），由 overlay §3 說明。",
	},
	{
		Anchor: "### 10.1 接案",
		Why:    "接案／建任務類型是成員治理職責；worker 只做綁給它的那一張任務。",
	},
	{
		Anchor: "### 10.1b 手冊的內容也歸你維護",
		Why:    "手冊維護是成員治理職責。worker 只回寫學習經驗，不改手冊設定。",
	},
	{
		Anchor: "### 10.1c 把單一任務發包給外包",
		Why:    "發包是成員的動作。worker 就是被發包的那一方，不轉包。",
	},
	{
		Anchor: "### 5.1 開機程序",
		Why: "worker 的開機序列在 overlay §2。這一段只是個指標，且寫著「見本 context " +
			"文末的啟動程序段落」——對外包的組裝順序不成立（啟動程序在最前面那一組）。",
	},
	{
		Anchor: "**接班起手式**",
		Block:  true,
		Why: "成員接班先讀自己 chat 的交接 baton + lessons + 持球 tasks。worker 的開機" +
			"起手式是 get_my_task 領工（overlay §2），不是讀上一代交接。",
	},
}

// workerBootSequenceExclusions filters block 3 (啟動程序) for a worker.
//
// The member boot sequence is built on two tools a worker does not use:
// report_waking (a worker's online signal is get_my_task) and
// peek_resume_summary_size/resume_summary (a member identity-snapshot path; a
// worker re-derives its state from get_my_task + task plan/step notes + the
// handover baton). Shipping those steps to a worker would inject exactly the
// class of code-contradicting instruction this ticket exists to remove, so
// they are excluded rather than left to be overridden further down.
var workerBootSequenceExclusions = []sharedCoreExclusion{
	{
		Anchor: "剛醒過來、開機當下照序做這三步",
		Block:  true,
		Why:    "「三步」的步數與內容對 worker 不成立；worker 的開機序列在 overlay §2。",
	},
	{
		Anchor: "1. **報 waking",
		Line:   true,
		Why:    "report_waking 不在 worker 的開機序列——它的上線訊號是 get_my_task 領工。",
	},
	{
		Anchor: "2. **接回脈絡",
		Line:   true,
		Why: "resume_summary 是成員的身分快照接續路徑；worker 從 get_my_task + task " +
			"plan/step note + baton 接回。",
	},
}

var headingLevelRe = regexp.MustCompile(`^(#{1,6})\s`)

// headingLevel returns the ATX heading level of line, or 0 when not a heading.
func headingLevel(line string) int {
	m := headingLevelRe.FindStringSubmatch(line)
	if m == nil {
		return 0
	}
	return len(m[1])
}

// multiBlankRe collapses the blank-line runs that excision leaves behind.
var multiBlankRe = regexp.MustCompile(`\n{3,}`)

// applySharedCoreExclusions removes each exclusion's region from doc.
//
// FAILS CLOSED: an anchor that matches nothing is an error, not a no-op. That
// is the whole point — the failure mode this ticket exists to prevent is a
// SILENT one (upstream renames a heading, the exclusion quietly stops firing,
// and a worker boots with instructions that do not apply to it and does not
// crash). A loud assembly-time failure is strictly preferable.
func applySharedCoreExclusions(doc string, exclusions []sharedCoreExclusion) (string, error) {
	lines := strings.Split(doc, "\n")
	drop := make([]bool, len(lines))

	for _, ex := range exclusions {
		start := -1
		for i, line := range lines {
			if drop[i] {
				continue // already excised by an earlier (outer) exclusion
			}
			if strings.HasPrefix(line, ex.Anchor) {
				start = i
				break
			}
		}
		if start < 0 {
			return "", fmt.Errorf(
				"shared-core exclusion anchor %q matched nothing — the shared block "+
					"changed and the worker exclusion list is stale; repoint the "+
					"anchor rather than dropping it", ex.Anchor)
		}

		end := len(lines) // exclusive
		switch {
		case ex.Line:
			end = start + 1
		case ex.Block:
			for i := start; i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) == "" {
					end = i
					break
				}
			}
		default:
			lvl := headingLevel(lines[start])
			if lvl == 0 {
				return "", fmt.Errorf(
					"shared-core exclusion anchor %q is not a heading but was marked "+
						"neither Block nor Line", ex.Anchor)
			}
			for i := start + 1; i < len(lines); i++ {
				if l := headingLevel(lines[i]); l > 0 && l <= lvl {
					end = i
					break
				}
			}
		}
		for i := start; i < end; i++ {
			drop[i] = true
		}
	}

	kept := make([]string, 0, len(lines))
	for i, line := range lines {
		if !drop[i] {
			kept = append(kept, line)
		}
	}
	out := multiBlankRe.ReplaceAllString(strings.Join(kept, "\n"), "\n\n")
	return strings.TrimSpace(out), nil
}

// filteredSeed reads a seed and applies its worker exclusion list.
func (s *apiServer) filteredSeed(filename string, ex []sharedCoreExclusion) (string, error) {
	raw, err := s.root.readSeedFile(filename)
	if err != nil {
		return "", err
	}
	return applySharedCoreExclusions(raw, ex)
}

// workerGlobalContext returns the worker's view of all THREE 全域情境 blocks,
// grouped, in cockpit order (系統互動 → 使用者自訂 → 啟動程序). This is the
// FIRST section of every worker boot context.
//
// The 使用者自訂 block follows the member rule: skipped entirely when the owner
// text is blank, so a worker never sees an empty header.
func (s *apiServer) workerGlobalContext() (string, error) {
	sys, err := s.filteredSeed("system_interaction.md", workerSharedCoreExclusions)
	if err != nil {
		return "", err
	}
	parts := []string{sys}

	userCtx, err := s.foldUserContextDTO()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(userCtx.Text) != "" {
		parts = append(parts,
			"# 使用者自訂（Owner Additions）\n\n"+strings.TrimSpace(userCtx.Text))
	}

	boot, err := s.filteredSeed("boot_sequence.md", workerBootSequenceExclusions)
	if err != nil {
		return "", err
	}
	parts = append(parts, boot)

	return strings.Join(parts, "\n\n"), nil
}
