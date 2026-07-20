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
