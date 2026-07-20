package main

// worker_sharedcore_test.go — T-108b. Locks the three properties the ticket
// bought: (1) Global Context is the FIRST section on both sides, (2) the
// MEMBER fold is byte-for-byte unchanged, (3) the member-only sections really
// are ABSENT from the worker's copy (not merely "the good stuff is present").

import (
	"strings"
	"testing"
)

const globalContextH1 = "# Global Context"

// workerCtx builds a worker boot context over a minimal fixture.
func workerCtx(t *testing.T) string {
	t.Helper()
	return workerCtxOn(t, newWorkerTestServer(t))
}

func workerCtxOn(t *testing.T, s *apiServer) string {
	t.Helper()
	w := OutsourceWorker{ID: "ow-t108b", Codename: "O-9", Model: "sonnet", Effort: "medium"}
	task := Task{ID: "tk-t108b", Title: "T-108b fixture", TypeKey: "general",
		Priority: "mid", ExecutorKind: TaskExecutorOutsource, ExecutorID: w.ID}
	putWorkerFixture(t, s, w)
	putTaskFixture(t, s, task)
	ctx, err := s.buildWorkerBootContext(w, task, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}
	return ctx
}

// memberCtx builds the default member boot context.
func memberCtx(t *testing.T) (*apiServer, *bootContext) {
	t.Helper()
	s := newWorkerTestServer(t)
	bc, err := s.buildBootContext("", nil, "")
	if err != nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	if bc == nil {
		t.Fatal("buildBootContext returned nil for the default role")
	}
	return s, bc
}

// ── 1. owner's hard requirement: Global Context FIRST, both sides ────────────

func TestWorkerBootContextStartsWithGlobalContext(t *testing.T) {
	ctx := workerCtx(t)
	if !strings.HasPrefix(ctx, globalContextH1) {
		head := ctx
		if len(head) > 120 {
			head = head[:120]
		}
		t.Fatalf("worker boot context must OPEN with the Global Context section; got %q", head)
	}
	// ...and the worker overlay must come AFTER it, not before.
	iCore := strings.Index(ctx, globalContextH1)
	iOverlay := strings.Index(ctx, "# 外包工作者")
	if iOverlay < 0 {
		t.Fatal("worker overlay section missing from worker boot context")
	}
	if iCore > iOverlay {
		t.Fatalf("Global Context (at %d) must precede the worker overlay (at %d)", iCore, iOverlay)
	}
}

func TestMemberBootContextStartsWithGlobalContext(t *testing.T) {
	_, bc := memberCtx(t)
	if !strings.HasPrefix(bc.Context, globalContextH1) {
		head := bc.Context
		if len(head) > 120 {
			head = head[:120]
		}
		t.Fatalf("member boot context must OPEN with the Global Context section; got %q", head)
	}
}

// ── 2. the sentinel: the MEMBER fold must not move ───────────────────────────

// TestMemberBootContextByteIdenticalToSpecAssembly independently reassembles
// spec/lifecycle.md §2.2 from the seed assets and demands byte equality. T-108b
// must not touch the member path AT ALL; if this goes red, the worker-side
// change has overspilled into the member fold.
func TestMemberBootContextByteIdenticalToSpecAssembly(t *testing.T) {
	s, bc := memberCtx(t)

	sysSeed, err := s.root.readSeedFile("system_interaction.md")
	if err != nil {
		t.Fatalf("read system_interaction.md: %v", err)
	}
	bootSeed, err := s.root.readSeedFile("boot_sequence.md")
	if err != nil {
		t.Fatalf("read boot_sequence.md: %v", err)
	}
	roleDTO, err := s.foldRoleDefDTO(bc.RoleKey)
	if err != nil || roleDTO == nil {
		t.Fatalf("fold role: %v", err)
	}
	lessons, err := s.foldLessonsDTO(bc.RoleKey, bc.TaskType)
	if err != nil {
		t.Fatalf("fold lessons: %v", err)
	}
	userCtx, err := s.foldUserContextDTO()
	if err != nil {
		t.Fatalf("fold user context: %v", err)
	}

	roleTitle := roleDTO.Name
	if roleTitle == "" {
		roleTitle = roleDTO.Key
	}
	parts := []string{
		strings.TrimSpace(sysSeed),
		"# Role: " + roleTitle + "\n\n" + strings.TrimSpace(roleDTO.DefinitionMD),
		"# Lessons (" + bc.RoleKey + " / " + bc.TaskType + ")\n\n" + strings.TrimSpace(lessons.Text),
	}
	if strings.TrimSpace(userCtx.Text) != "" {
		parts = append(parts,
			"# 使用者自訂（Owner Additions）\n\n"+strings.TrimSpace(userCtx.Text))
	}
	parts = append(parts, strings.TrimSpace(bootSeed))
	want := strings.Join(parts, "\n\n") + "\n"

	if bc.Context != want {
		t.Fatalf("member boot context drifted from the §2.2 assembly "+
			"(got %d bytes, want %d) — T-108b must not touch the member path",
			len(bc.Context), len(want))
	}
}

// TestMemberBootContextKeepsMemberOnlySections is the other half of the
// sentinel: the member must still receive, UNFILTERED, every section the worker
// excludes. Guards against someone "simplifying" both paths onto the filtered
// core.
func TestMemberBootContextKeepsMemberOnlySections(t *testing.T) {
	_, bc := memberCtx(t)
	for _, ex := range workerSharedCoreExclusions {
		if !strings.Contains(bc.Context, ex.Anchor) {
			t.Errorf("member boot context lost %q — the worker exclusions must "+
				"NEVER apply to the member fold", ex.Anchor)
		}
	}
}

// ── 3. exclusions: prove the excluded things are ABSENT ──────────────────────

// TestWorkerSharedCoreExclusionAnchorsAllResolve is the anti-silent-drift
// guard. If upstream renames a heading, the exclusion would quietly stop
// firing and a worker would start reading member-only instructions. Fail loud.
func TestWorkerSharedCoreExclusionAnchorsAllResolve(t *testing.T) {
	s := newWorkerTestServer(t)
	for _, tc := range []struct {
		seed string
		list []sharedCoreExclusion
	}{
		{"system_interaction.md", workerSharedCoreExclusions},
		{"boot_sequence.md", workerBootSequenceExclusions},
	} {
		doc, err := s.root.readSeedFile(tc.seed)
		if err != nil {
			t.Fatalf("read %s: %v", tc.seed, err)
		}
		for _, ex := range tc.list {
			if !strings.Contains(doc, ex.Anchor) {
				t.Errorf("exclusion anchor %q no longer matches %s — the doc was "+
					"renamed; repoint the anchor (do NOT delete it): %s",
					ex.Anchor, tc.seed, ex.Why)
			}
		}
	}
	if _, err := s.workerGlobalContext(); err != nil {
		t.Fatalf("workerGlobalContext must resolve every anchor: %v", err)
	}
}

// TestWorkerSharedCoreRewriteAnchorsAllResolve is the same drift guard for the
// in-place rewrites. A rewrite that stops matching is worse than an exclusion
// that stops matching: the member-only clause silently stays in the worker's
// copy, and every test asserting "the corrected wording is present" can still
// be satisfied by some other line. filteredSeed fails closed on both halves
// (anchor missing, Find missing); this pins it as an explicit expectation.
func TestWorkerSharedCoreRewriteAnchorsAllResolve(t *testing.T) {
	s := newWorkerTestServer(t)
	for _, tc := range []struct {
		seed string
		ex   []sharedCoreExclusion
		rw   []sharedCoreRewrite
	}{
		{"system_interaction.md", workerSharedCoreExclusions, workerSharedCoreRewrites},
		{"boot_sequence.md", workerBootSequenceExclusions, workerBootSequenceRewrites},
	} {
		if _, err := s.filteredSeed(tc.seed, tc.ex, tc.rw); err != nil {
			t.Errorf("filteredSeed(%s) must resolve every exclusion AND rewrite: %v",
				tc.seed, err)
		}
	}
}

// ── zero residue: member-only instructions are GONE, not merely contradicted ─

// TestWorkerBootContextHasZeroMemberOnlyResidue pins the six residues the
// independent review found. Five were "overridden" by the overlay 47% of the
// document later; one — the 發包 item pointing at §10.1c — was not overridden
// by anything at all, and was a ✅ POSITIVE example encouraging the worker to
// do the exact thing the exclusion list removed §10.1c for ("worker 就是被發包
// 的那一方，不轉包").
//
// Each case carries a positive control: the same text MUST still be present in
// the member fold. Without that, deleting the shared core wholesale would make
// every assertion here pass.
func TestWorkerBootContextHasZeroMemberOnlyResidue(t *testing.T) {
	worker := workerCtx(t)
	_, bc := memberCtx(t)
	member := bc.Context

	for _, tc := range []struct{ name, text, why string }{
		{
			"§10.4 發包給外包（指向已排除的 §10.1c）",
			"想把**當下這一張**任務發包給外包",
			"worker 就是被發包的那一方，不轉包。這條先前完全沒有任何 overlay 覆寫。",
		},
		{
			"§10.4 照手冊的負責設定走 §10.1 三條路",
			"照手冊的負責設定走 §10.1 三條路",
			"§10.1 接案已排除；worker 只做綁給它的那一張任務。",
		},
		{
			"§10.4 你是 scrum master、不下場",
			"你是 scrum master",
			"對 worker 是反的——它正是被授權自己下場的角色。",
		},
		{
			"§10.4 等待不是停下——開下一張",
			"等待不是停下——開下一張",
			"worker 一綁一任務，沒有下一張可開。",
		},
		{
			"§8b 換手 SOP 第 3 步 lessons",
			"用 lessons 工具整併耐久教訓",
			"指向已排除的 §9，且在 ~120 秒寬限下會被照著執行。",
		},
		{
			"§1 世界觀 lessons 指標",
			"掛在你的角色身上，見 §9",
			"worker 沒有角色，§9 也不在它讀到的文件裡。",
		},
		{
			"§10.5 角色 lessons 那一軌",
			"照 §9 整併進你角色的學習筆記",
			"同上；worker 只有手冊學習經驗這一軌。",
		},

		// ── 以下三處不在 review 清單上，是修這包時從實際組出來的內容量到的 ──
		// review 只掃了 §10.4 的 DO/DON'T 一行版本，漏了 §10.3 的政策本體
		// （更長、更實質、語氣更權威），以及 §10.4 的 resume_summary 那段。
		{
			"§10.3 等待不是停下（多任務調度）政策本體",
			"等待不是停下（多任務調度）",
			"叫 worker「回頭掃自己手上的任務佇列，開下一張」——它一綁一任務，沒有佇列。",
		},
		{
			"§10.3 你當 scrum master 政策本體",
			"吃 context 的重活交給 sub-agent，你當 scrum master",
			"「不自己下場做重活」對 worker 完全相反——它正是被授權下場的角色（overlay §4）。",
		},
		{
			"§10.4 resume_summary 接手路徑",
			"先用 MCP `peek_resume_summary_size` 探快照多大",
			"這兩個工具正是因為對 worker 不成立而被排除；叫它去用一條它沒有的路。",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// positive control — the member still gets it.
			if !strings.Contains(member, tc.text) {
				t.Fatalf("positive control failed: %q is not in the MEMBER fold either, "+
					"so its absence from the worker proves nothing (seed reworded?)", tc.text)
			}
			if strings.Contains(worker, tc.text) {
				t.Errorf("member-only instruction survived in the worker's copy: %q\n%s\n"+
					"不要靠 overlay 覆寫——「後面覆寫前面」正是這份文件已經失敗過一次的機制。",
					tc.text, tc.why)
			}
		})
	}
}

// TestWorkerHandoverSOPIsSelfConsistent checks the OTHER half of "zero
// residue": removing step 3 must not leave a 1/2/4/5 list still calling itself
// 五步. Half a change reads as a document that lost a step, which invites the
// worker to go looking for it.
func TestWorkerHandoverSOPIsSelfConsistent(t *testing.T) {
	ctx := workerCtx(t)

	if strings.Contains(ctx, "五步") {
		t.Error("worker handover SOP still says 五步 but only four steps survive")
	}
	if !strings.Contains(ctx, "照四步走完") {
		t.Error("worker handover SOP should read 四步 after the lessons step is removed")
	}
	// The surviving steps must be numbered 1..4 with no gap.
	for _, want := range []string{
		"1. **MCP `report_stopping()`**",
		"2. **把還在飛的工作寫回 task step note**",
		"3. **post chat 給「自己」一則交接 baton**",
		"4. **MCP `report_stopped()`**",
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("renumbered handover step missing: %q", want)
		}
	}
	if strings.Contains(ctx, "5. **MCP `report_stopped()`**") {
		t.Error("handover step 5 was not renumbered — orphan numbering left behind")
	}
}

// TestWorkerBootSequenceHasNoOrphanNumbering covers the same defect in block 3.
// Steps 1 and 2 are excluded; shipping a lone "3." with no 1, no 2 and no intro
// tells the worker it is missing two steps.
func TestWorkerBootSequenceHasNoOrphanNumbering(t *testing.T) {
	ctx := workerCtx(t)
	if strings.Contains(ctx, "3. **全部就緒後，才掛") {
		t.Error("worker boot sequence still ships an orphan \"3.\" with no 1 and no 2")
	}
	if !strings.Contains(ctx, "**掛 `ocagent listen`") {
		t.Error("worker boot sequence lost the ocagent listen step entirely")
	}
}

// ── the three 全域情境 blocks all reach the worker, grouped, at the front ─────

func TestWorkerGlobalContextCarriesAllThreeBlocks(t *testing.T) {
	s := newWorkerTestServer(t)
	const ownerMark = "T108B-OWNER-CUSTOM-MARKER"
	if err := s.dal.PutUserContext(UserContext{Text: ownerMark}); err != nil {
		t.Fatalf("put user context: %v", err)
	}
	ctx := workerCtxOn(t, s)

	iSys := strings.Index(ctx, globalContextH1)          // 1. 系統互動
	iCustom := strings.Index(ctx, ownerMark)             // 2. 使用者自訂
	iBoot := strings.Index(ctx, "# 啟動程序（Boot Sequence）") // 3. 啟動程序
	iOverlay := strings.Index(ctx, "# 外包工作者")

	for name, idx := range map[string]int{
		"系統互動": iSys, "使用者自訂": iCustom, "啟動程序": iBoot, "外包差異覆寫": iOverlay,
	} {
		if idx < 0 {
			t.Fatalf("worker boot context is missing the %s block", name)
		}
	}
	// cockpit order, and the whole group ahead of the overlay.
	if !(iSys < iCustom && iCustom < iBoot && iBoot < iOverlay) {
		t.Fatalf("三塊 Global Context 必須依序 (系統互動=%d, 使用者自訂=%d, 啟動程序=%d) "+
			"且整組在外包差異覆寫 (=%d) 之前", iSys, iCustom, iBoot, iOverlay)
	}
}

// A blank owner block must not emit an empty header (member rule, §2.2 part 4).
func TestWorkerGlobalContextSkipsBlankOwnerBlock(t *testing.T) {
	ctx := workerCtx(t) // default DB: user context never written
	if strings.Contains(ctx, "# 使用者自訂（Owner Additions）") {
		t.Error("blank owner text must skip the 使用者自訂 header entirely")
	}
}

// The member-only boot steps must NOT reach the worker.
func TestWorkerBootSequenceDropsMemberOnlySteps(t *testing.T) {
	ctx := workerCtx(t)
	// NOTE: deliberately NOT asserting the bare tool names are absent —
	// report_waking / resume_summary legitimately appear in §5's presence
	// explanation and 附錄A's tool catalog, which ARE true for a worker (it has
	// the presence tools; they are just not its BOOT sequence). What must go is
	// the member boot SOP itself.
	for _, gone := range []string{
		"照序做這三步",           // 啟動程序 intro (step count wrong for a worker)
		"報 waking（不掛 SSE）", // 啟動程序 step 1
		"接回脈絡（兩步",          // 啟動程序 step 2
		"### 5.1 開機程序",     // stale pointer to "文末的啟動程序段落"
	} {
		if strings.Contains(ctx, gone) {
			t.Errorf("worker boot context still carries member-only boot step %q", gone)
		}
	}
	// positive control: the surviving step is still there.
	if !strings.Contains(ctx, "ocagent listen") {
		t.Error("啟動程序 block over-filtered — the listen step should survive")
	}
}

func TestWorkerBootContextExcludesMemberOnlySections(t *testing.T) {
	ctx := workerCtx(t)

	// The excluded HEADINGS themselves.
	for _, ex := range workerSharedCoreExclusions {
		if strings.Contains(ctx, ex.Anchor) {
			t.Errorf("worker boot context still contains excluded region %q (%s)",
				ex.Anchor, ex.Why)
		}
	}

	// Distinctive BODY text from inside each excluded region — proves the
	// subtree went, not just its heading line.
	for _, body := range []string{
		"per 角色 × 任務型", // §9 role lessons (NOT "掛在角色身上" — the overlay
		// legitimately uses that phrase to say the worker has no such thing)
		"三條路建立",       // §10.1 接案
		"負責設定歸 owner", // §10.1b manual governance
		"依上限自動排隊",     // §10.1c 發包
	} {
		if strings.Contains(ctx, body) {
			t.Errorf("worker boot context still contains excluded body text %q", body)
		}
	}
}

// TestWorkerSharedCoreKeepsSharedSections is the positive control for the
// exclusion test above: without it, a filter that deleted the ENTIRE core
// would pass "the excluded stuff is absent" trivially.
func TestWorkerSharedCoreKeepsSharedSections(t *testing.T) {
	ctx := workerCtx(t)
	for _, keep := range []string{
		"## 0. 你為什麼在這裡", // WHY spine
		"### 10.2",      // step planning
		"### 10.3",      // step execution
		"## 附錄 B",       // judgement principles
		"### 8b. 換手",    // handover mechanics DO apply to workers
	} {
		if !strings.Contains(ctx, keep) {
			t.Errorf("worker boot context lost shared section %q — the exclusion "+
				"filter is over-broad", keep)
		}
	}
}

// ── 4. the safety motive: risk language must REACH the worker ────────────────

func TestWorkerBootContextCarriesRiskLanguage(t *testing.T) {
	ctx := workerCtx(t)
	for _, want := range []string{
		"風險",
		"backup-before-destructive",
		"verify-before-assert",
		"安全邊界",
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("worker boot context is missing risk language %q — this is the "+
				"safety motive of T-108b (the worker is the only role authorised to "+
				"do destructive work itself)", want)
		}
	}
}

// TestWorkerOverlayDropsCodeContradictingClaims pins the three statements the
// old worker seed made that the code contradicts (kyle-108b-facts.md §1/§2/§3).
func TestWorkerOverlayDropsCodeContradictingClaims(t *testing.T) {
	ctx := workerCtx(t)
	for _, gone := range []string{
		"沒有喚醒／下線管理",           // false: worker runs on the shared reconcile FSM
		"沒有對應工具、不主動觸發",        // false: worker HAS restart_self
		"不像正職成員得守 context 預算", // false: identical handover thresholds
	} {
		if strings.Contains(ctx, gone) {
			t.Errorf("worker boot context still asserts %q, which the code contradicts", gone)
		}
	}
	// And the corrected replacements are present.
	if !strings.Contains(ctx, "restart_self") {
		t.Error("worker overlay must tell the worker it HAS restart_self")
	}
	if !strings.Contains(ctx, "與正職完全相同的") {
		t.Error("worker overlay must state the context thresholds are identical to a member's")
	}
}
