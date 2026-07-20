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

// sharedCoreRewrite edits ONE line of a shared block in place, for the worker
// rendering only. It exists because some member-only statements are a CLAUSE
// inside a line whose remainder is correct and worth keeping — deleting the
// whole line would throw away shared policy, but leaving it whole would leave a
// residue. Anchor locates the line (LINE PREFIX); Find/Replace edit within it.
//
// FAILS CLOSED exactly like the exclusions: anchor not found, or Find absent
// from the anchored line, is an error. Rewrites run AFTER exclusions, so a
// rewrite whose line was excised also fails loudly rather than silently
// no-op'ing.
type sharedCoreRewrite struct {
	Anchor  string
	Find    string
	Replace string
	Why     string
}

// workerSharedCoreExclusions filters block 1 (系統互動) for a worker.
//
// Everything here is REMOVED OUTRIGHT rather than left to be contradicted by
// the overlay further down. That is deliberate and is the core lesson of this
// ticket: "later text overrides earlier text" is the mechanism this very
// document has ALREADY failed under once (the old worker copy contained both
// 不像正職成員得守 context 預算 and 跟正職同一組門檻; the wrong one, being
// earlier, was the one that got obeyed). An outsource worker executes the
// handover SOP under a ~120s grace window and will follow the numbered list in
// front of it; a correction 47% of the document later is not a correction.
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

	// ── §8b 換手 SOP：拿掉 lessons 那一步 ────────────────────────────────
	//
	// 這一步指向 §9（已排除）——對 worker 它是一條「指向不存在章節」的壞指示，
	// 不只是「被後面覆寫」。連同下面兩條 renumber rewrite 與 五步→四步，一起
	// 讓外包讀到一份自洽的四步 SOP，不留孤兒編號。
	{
		Anchor: "3. **用 lessons 工具整併耐久教訓**",
		Line:   true,
		Why: "角色 lessons 對 worker 不成立，且這一步指向已排除的 §9。worker 的等價物" +
			"是 write_task_learnings（overlay §3），不放進換手 SOP 的編號步驟裡。",
	},

	// ── §10.4 DO/DON'T 決策清單：四條成員專屬 ────────────────────────────
	{
		Anchor: "- ✅ 想把**當下這一張**任務發包給外包",
		Line:   true,
		Why: "最危險的一條：它是一個**正面示範**，指向已排除的 §10.1c，鼓勵 worker 去做" +
			"排除清單明文說它不該做的事（worker 是被發包的那一方，不轉包）。" +
			"overlay 從頭到尾沒有任何一句推翻它——這不是覆寫不夠強，是完全沒有覆寫。",
	},
	{
		Anchor: "- ✅ `list_task_manuals` 判出 `review-pr` 類型",
		Line:   true,
		Why:    "指向已排除的 §10.1 三條路。接案是成員治理職責，worker 只做綁給它的那一張。",
	},
	{
		Anchor: "- ❌ 自己在 main session 下場寫一整批程式",
		Line:   true,
		Why: "「你是 scrum master、不下場」對 worker 是反的——worker 正是被授權自己下場的" +
			"角色（overlay §4）。留著它等 overlay 覆寫，等於把最關鍵的授權押在閱讀順序上。",
	},
	{
		Anchor: "- ❌ 開了 gate 卡等 owner 回覆",
		Line:   true,
		Why: "「等待不是停下——開下一張」預設常駐、多任務的成員工作型態。worker 一綁一任務，" +
			"沒有下一張可開（overlay §1）。",
	},
	{
		Anchor: "- **吃 context 的重活交給 sub-agent，你當 scrum master。**",
		Line:   true,
		Why: "§10.3 的政策本體，比 §10.4 那條 DO/DON'T 一行更實質：「你這個 main session 的" +
			"角色是 scrum master……**不自己下場做重活**」。對 worker 完全相反——它正是被授權" +
			"下場的角色。這條連自己的括號都寫著「外包 worker……可以自己下場做重活——你不行」，" +
			"整段是寫給成員讀的，用第二人稱送給外包只會讀成反的。overlay §4 已完整涵蓋" +
			"（可以下場 ＋ context 預算照守 ＋ 重活丟 sub-agent ＋ 開發/review 不同 actor）。" +
			"（第八處殘留，量自實際組出來的內容，不在 review 清單上。）",
	},
	{
		Anchor: "- **等待不是停下（多任務調度）。**",
		Line:   true,
		Why: "§10.3 的多任務調度政策本體——比上面那條 DO/DON'T 一行更實質，明確叫你「回頭掃" +
			"自己手上的任務佇列，開下一張繼續推進」。worker 一綁一任務，佇列裡永遠沒有下一張。" +
			"（這條是修這包時從實際組出來的內容裡量到的第七處殘留，不在 review 清單上。）",
	},
}

// workerSharedCoreRewrites edits block 1 (系統互動) in place for a worker.
//
// Ordering note: these run after workerSharedCoreExclusions, and several of
// them exist ONLY to clean up after an exclusion (the §8b renumber). Keeping
// them as data next to the exclusions is what makes "delete the step AND fix
// the numbering" a single reviewable unit instead of half a change.
var workerSharedCoreRewrites = []sharedCoreRewrite{
	{
		Anchor:  "# Global Context（AI 工作室 · 成員 boot context）",
		Find:    "（AI 工作室 · 成員 boot context）",
		Replace: "（AI 工作室 · 共用全域情境）",
		Why: "外包讀到的第一行不該自稱是「成員 boot context」——那是這份文件對外包" +
			"說的第一句話，而它是錯的。內容確實是共用的全域情境。",
	},
	{
		Anchor:  "- **Agent（你，與未來的 AI 隊友）**",
		Find:    "（記憶落在學習筆記，掛在你的角色身上，見 §9）",
		Replace: "（記憶落在任務手冊的學習經驗；你沒有角色，也沒有掛在角色身上的學習筆記）",
		Why: "§1 世界觀這一句是 worker 讀到的第一個 lessons 指標，且指向已排除的 §9。" +
			"整行刪掉會連「可丟棄的 edge session」這個共用世界觀一起丟掉，所以就地改寫。" +
			"改寫後不留任何 §N 指標，避免製造新的懸空引用。",
	},

	// §8b 換手 SOP：步驟 3 已被排除，把「五步」與其後的編號補乾淨。
	{
		Anchor:  "**你會收到 server 的下線／回收通知**",
		Find:    "看到就立刻照五步走完",
		Replace: "看到就立刻照四步走完",
		Why:     "步驟 3（lessons）已排除，worker 讀到的是四步。",
	},
	{
		Anchor:  "4. **post chat 給「自己」一則交接 baton**",
		Find:    "4. ",
		Replace: "3. ",
		Why:     "步驟 3 排除後的 renumber——不留 1、2、4、5 的孤兒編號。",
	},
	{
		Anchor:  "5. **MCP `report_stopped()`**",
		Find:    "5. ",
		Replace: "4. ",
		Why:     "同上 renumber。",
	},
	{
		Anchor:  "**你也可以主動要求換手（自我重啟）。**",
		Find:    "照**同一個五步**走完",
		Replace: "照**同一個四步**走完",
		Why:     "同一份 SOP 的第二個提及，一併改；漏掉這個就等於留半套。",
	},

	// §10.4 換手：把成員的 resume_summary 快照路徑換成 worker 的 get_my_task 路徑。
	//
	// 第九處殘留（量自實際組出來的內容，不在 review 清單上），而且是本票最該防的
	// 那一類：它明確叫 worker 去用 peek_resume_summary_size / resume_summary，而
	// 這兩個工具正是 workerBootSequenceExclusions 因為「對 worker 不成立」而排除的。
	// 整行刪掉會連「你沒報回 server 的，對下一個你就是不存在」這條對 worker 同樣
	// 成立、而且很重要的紀律一起丟掉，所以就地改寫中間那段機制描述。
	{
		Anchor:  "換手／換機（§8b）時，你手上任務的完整狀態",
		Find:    "接手的新 session **先用 MCP `peek_resume_summary_size` 探快照多大**（只回大小／counts ＋ `estimated_total_chars`、不含任何內容），再決定怎麼接回:小（經驗門檻 < 20000 字元、約 5k tokens）就直接用 `resume_summary` 拿回、大就派便宜 sub-agent（如 haiku）去 `resume_summary` 拉回並回壓縮摘要——然後**接著跑完**。`resume_summary` 快照是**輕量列**（省你的開機 context）：每張任務只有編號／標題／狀態／優先權／當前節點名稱＋進度，**不含 steps／DoD 全文**；`overview` 欄帶大小概要（開放任務總數、省略掉的計畫文字字數 `detail_chars`、快照 chat 字數 `chat_chars`、你的等回覆卡數，peek 讀的就是這塊）——**先看大小再決定**：細節按需 `get_task` 逐張拉，`detail_chars` 很大就丟給 sub-agent 消化、別整包塞進自己的 context；卡片列表用 `list_reply_cards`（有 `limit` 可截量，列表只給標題＋決策要點，全文 `get_reply_card`）。",
		Replace: "接手的新你用 MCP `get_my_task` 領回這張任務的全文＋手冊快照，再照 task plan／step note ＋ 你上一代留給自己的交接 baton 接回進度——然後**接著跑完**。細節按需 `get_task` 拉；很大就丟給 sub-agent 消化、別整包塞進自己的 context。",
		Why: "peek_resume_summary_size / resume_summary 是成員的身分快照路徑，已從 worker 的" +
			"啟動程序排除；留著這句等於叫 worker 去用一條它沒有的路。",
	},

	// §10.5 結案後續：拿掉「角色 lessons」那一軌。
	{
		Anchor:  "1. **經驗回寫**",
		Find:    "（有值得沉澱的就分兩軌）",
		Replace: "（有值得沉澱的就寫回手冊）",
		Why:     "worker 只有手冊這一軌，沒有角色那一軌。",
	},
	{
		Anchor: "1. **經驗回寫**",
		Find: "；**屬你這個角色的**（對你之後做任何事都成立的通則）→ 照 §9 整併進你角色的" +
			"學習筆記（lessons）。兩軌同一個整併紀律：整理不是往後貼。ad-hoc 任務無手冊，只有角色這一軌。",
		Replace: "。整併紀律：整理不是往後貼。對你之後做任何事都成立的通則，一樣寫進手冊的" +
			"學習經驗；ad-hoc 任務沒有手冊，就寫進 step note 的交接說明。",
		Why: "第二個指向已排除 §9 的 lessons 指標。整行刪掉會連 write_task_learnings 這條" +
			"（worker 真正該做的事）一起丟掉，所以就地改寫成單軌。",
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

// workerBootSequenceRewrites cleans up after workerBootSequenceExclusions.
//
// Steps 1 and 2 are excluded, which used to leave the worker reading a lone
// orphan "3." with no 1, no 2 and no intro. Same rule as §8b: if removing an
// item breaks the numbering, fix the numbering — do not ship half a change.
var workerBootSequenceRewrites = []sharedCoreRewrite{
	{
		Anchor:  "3. **全部就緒後，才掛 `ocagent listen`。**",
		Find:    "3. **全部就緒後，才掛 `ocagent listen`。**",
		Replace: "**掛 `ocagent listen`（在你 overlay §2 的開機序列就緒之後）。**",
		Why: "步驟 1、2 已排除，只剩這一步——留一個沒有 1、2 的孤兒「3.」會讓 worker " +
			"以為自己漏讀了兩步。改成單則敘述，並把「全部就緒」接回 worker 真正的開機序列。",
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

// applySharedCoreRewrites edits each rewrite's line in place.
//
// FAILS CLOSED for the same reason applySharedCoreExclusions does, and with one
// extra failure mode worth naming: a rewrite that silently did nothing would
// leave a member-only clause in the worker's copy while every test that asserts
// "the rewritten text is present" could still pass off some other line. Both
// "anchor not found" and "Find not in the anchored line" are hard errors.
func applySharedCoreRewrites(doc string, rewrites []sharedCoreRewrite) (string, error) {
	lines := strings.Split(doc, "\n")

	for _, rw := range rewrites {
		found := false
		for i, line := range lines {
			if !strings.HasPrefix(line, rw.Anchor) {
				continue
			}
			if !strings.Contains(line, rw.Find) {
				return "", fmt.Errorf(
					"shared-core rewrite anchor %q found, but its Find text %q is not on "+
						"that line — the shared block was reworded and the worker rewrite "+
						"is stale; re-derive the rewrite rather than dropping it",
					rw.Anchor, rw.Find)
			}
			lines[i] = strings.Replace(line, rw.Find, rw.Replace, 1)
			found = true
			break
		}
		if !found {
			return "", fmt.Errorf(
				"shared-core rewrite anchor %q matched nothing — either the shared block "+
					"changed, or an exclusion already removed this line; repoint or drop "+
					"the rewrite deliberately", rw.Anchor)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// filteredSeed reads a seed, applies its worker exclusion list, then its worker
// rewrite list. Exclusions run first so that a rewrite targeting an already
// excised line fails loudly (see applySharedCoreRewrites).
func (s *apiServer) filteredSeed(
	filename string, ex []sharedCoreExclusion, rw []sharedCoreRewrite,
) (string, error) {
	raw, err := s.root.readSeedFile(filename)
	if err != nil {
		return "", err
	}
	cut, err := applySharedCoreExclusions(raw, ex)
	if err != nil {
		return "", err
	}
	return applySharedCoreRewrites(cut, rw)
}

// workerGlobalContext returns the worker's view of all THREE 全域情境 blocks,
// grouped, in cockpit order (系統互動 → 使用者自訂 → 啟動程序). This is the
// FIRST section of every worker boot context.
//
// The 使用者自訂 block follows the member rule: skipped entirely when the owner
// text is blank, so a worker never sees an empty header.
func (s *apiServer) workerGlobalContext() (string, error) {
	sys, err := s.filteredSeed(
		"system_interaction.md", workerSharedCoreExclusions, workerSharedCoreRewrites)
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

	boot, err := s.filteredSeed(
		"boot_sequence.md", workerBootSequenceExclusions, workerBootSequenceRewrites)
	if err != nil {
		return "", err
	}
	parts = append(parts, boot)

	return strings.Join(parts, "\n\n"), nil
}
