package main

import (
	"strings"
	"testing"
)

const globalContextH1 = "# Global Context"

// Slot markers used by the ordering guards below. Each is the literal heading
// the assembled document actually carries, so a reordering shows up as an index
// comparison rather than as a byte-diff nobody can read.
const (
	ownerAdditionsH1 = "# 使用者自訂（Owner Additions）"
	roleH1           = "# Role: "
	lessonsH1        = "# Lessons ("
	bootSequenceH1   = "# 啟動程序（Boot Sequence"
)

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

// mustIndex returns the offset of marker in doc, failing loudly when it is
// absent — an ordering assertion built on -1 offsets silently "passes".
func mustIndex(t *testing.T, doc, marker, what string) int {
	t.Helper()
	i := strings.Index(doc, marker)
	if i < 0 {
		t.Fatalf("assembled context has no %s block (looked for %q)", what, marker)
	}
	return i
}

// unfilteredWorkerSharedHeadWant rebuilds the expected first two shared blocks
// of a worker boot context from the seeds on disk.
func unfilteredWorkerSharedHeadWant(t *testing.T, s *apiServer, ownerText string) string {
	t.Helper()
	sys, err := s.root.readSeedFile("system_interaction.md")
	if err != nil {
		t.Fatalf("read system_interaction.md: %v", err)
	}
	parts := []string{strings.TrimSpace(sys)}
	if strings.TrimSpace(ownerText) != "" {
		parts = append(parts, ownerAdditionsH1+"\n\n"+strings.TrimSpace(ownerText))
	}
	return strings.Join(parts, "\n\n")
}

func TestWorkerBootContextStartsWithGlobalContext(t *testing.T) {
	ctx := workerCtx(t)
	if !strings.HasPrefix(ctx, globalContextH1) {
		t.Fatalf("worker boot context must open with Global Context; got %q", ctx[:min(len(ctx), 120)])
	}
}

func TestMemberBootContextStartsWithGlobalContext(t *testing.T) {
	_, bc := memberCtx(t)
	if !strings.HasPrefix(bc.Context, globalContextH1) {
		t.Fatalf("member boot context must open with Global Context; got %q", bc.Context[:min(len(bc.Context), 120)])
	}
}

// TestBothBootContextsUseTheSameFourSlots — T-4595, and the reason this whole
// package changed shape.
//
// Staff and outsource boot contexts are now the SAME FOUR SLOTS in the SAME
// ORDER, and slot 3 (the persona) is the only difference — a worker has no
// role, so it reads nothing there. An outsource boot context is a staff boot
// context minus slot 3; not one word in it is written for outsource readers.
//
//  1. 系統互動   — shared seed
//  2. 使用者自訂 — shared owner block (this is the MOVE: staff used to read it
//     fourth, wedged between the lessons and the boot sequence)
//  3. persona    — staff: 角色說明 → 判準（blank ⇒ skipped）→ 長期筆記;
//     outsource: NOTHING (no role)
//  4. 啟動程序   — shared seed, recency-authoritative tail
//
// Both halves are asserted here on purpose. The member fold is what every
// staff agent reads on every boot, so the reorder needs a guard of its own
// rather than riding on the worker assertion.
func TestBothBootContextsUseTheSameFourSlots(t *testing.T) {
	t.Run("staff", func(t *testing.T) {
		s := newWorkerTestServer(t)
		const ownerMark = "T4595-STAFF-OWNER-CUSTOM"
		if err := s.dal.PutUserContext(UserContext{Text: ownerMark}); err != nil {
			t.Fatalf("put user context: %v", err)
		}
		bc, err := s.buildBootContext("", nil, "")
		if err != nil || bc == nil {
			t.Fatalf("buildBootContext: %v", err)
		}
		ctx := bc.Context

		owner := mustIndex(t, ctx, ownerAdditionsH1, "使用者自訂")
		role := mustIndex(t, ctx, roleH1, "角色說明")
		lessons := mustIndex(t, ctx, lessonsH1, "長期筆記")
		boot := mustIndex(t, ctx, bootSequenceH1, "啟動程序")

		if !(owner < role && role < lessons && lessons < boot) {
			t.Fatalf("staff slots out of order: 使用者自訂=%d 角色說明=%d 長期筆記=%d 啟動程序=%d\n"+
				"要的順序是 系統互動 → 使用者自訂 → 角色說明 →（判準）→ 長期筆記 → 啟動程序",
				owner, role, lessons, boot)
		}
		// The owner block must sit AFTER the shared seed, not before it.
		if owner == 0 {
			t.Fatal("使用者自訂 must follow the 系統互動 seed, not lead the document")
		}
	})

	t.Run("outsource", func(t *testing.T) {
		s := newWorkerTestServer(t)
		const ownerMark = "T4595-WORKER-OWNER-CUSTOM"
		if err := s.dal.PutUserContext(UserContext{Text: ownerMark}); err != nil {
			t.Fatalf("put user context: %v", err)
		}
		ctx := workerCtxOn(t, s)

		owner := mustIndex(t, ctx, ownerAdditionsH1, "使用者自訂")
		boot := mustIndex(t, ctx, bootSequenceH1, "啟動程序")

		if owner >= boot {
			t.Fatalf("outsource slots out of order: 使用者自訂=%d 啟動程序=%d\n"+
				"要的順序是 系統互動 → 使用者自訂 → 啟動程序", owner, boot)
		}
		if owner == 0 {
			t.Fatal("使用者自訂 must follow the 系統互動 seed, not lead the document")
		}
		// Slot 3 is EMPTY for a worker: it has no role, so it reads nothing
		// where staff read 角色說明 →（判準）→ 長期筆記. The pre-T-4595 assembly put an
		// overlay, an identity block, the whole bound task and the whole type
		// manual in there — and put the shared boot sequence at the TOP rather
		// than at the tail.
		for _, absent := range []string{roleH1, lessonsH1, "# 任務手冊", "# 你的身分", "# 你的任務"} {
			if strings.Contains(ctx, absent) {
				t.Errorf("outsource boot context has something in slot 3 (%q); "+
					"a worker reads nothing there", absent)
			}
		}
	})
}

// The member assembly remains an independent sentinel: this worker-only change
// must not alter the staff fold.
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
	insight, err := s.foldInsightDTO(bc.RoleKey)
	if err != nil {
		t.Fatalf("fold insight: %v", err)
	}
	roleTitle := roleDTO.Name
	if roleTitle == "" {
		roleTitle = roleDTO.Key
	}
	// §2.2 order: 系統互動 → 使用者自訂 → Role → Insight → Lessons → 啟動程序.
	// Insight, like the owner block, is skipped ENTIRELY when its folded text
	// is blank — the gate is the text, not is_default/has_seed.
	parts := []string{strings.TrimSpace(sysSeed)}
	if strings.TrimSpace(userCtx.Text) != "" {
		parts = append(parts, ownerAdditionsH1+"\n\n"+strings.TrimSpace(userCtx.Text))
	}
	parts = append(parts,
		"# Role: "+roleTitle+"\n\n"+strings.TrimSpace(roleDTO.DefinitionMD))
	if body := strings.TrimSpace(insight.Text); body != "" {
		parts = append(parts, "# Insight ("+bc.RoleKey+")\n\n"+body)
	}
	parts = append(parts,
		"# Lessons ("+bc.RoleKey+" / "+bc.TaskType+")\n\n"+strings.TrimSpace(lessons.Text),
		strings.TrimSpace(bootSeed))
	want := strings.Join(parts, "\n\n") + "\n"
	if bc.Context != want {
		t.Fatalf("member boot context drifted from the §2.2 assembly (got %d bytes, want %d)", len(bc.Context), len(want))
	}
}

// TestWorkerSharedHeadMatchesUnfilteredSeedAssembly is the discriminator for
// T-108b, kept through the T-4595 restructure. Reintroducing any worker-only
// exclusion or rewrite into the shared seed makes this equality assertion red.
func TestWorkerSharedHeadMatchesUnfilteredSeedAssembly(t *testing.T) {
	s := newWorkerTestServer(t)
	const ownerMark = "T108B-OWNER-CUSTOM-MARKER"
	if err := s.dal.PutUserContext(UserContext{Text: ownerMark}); err != nil {
		t.Fatalf("put user context: %v", err)
	}
	got, err := s.workerSharedHead()
	if err != nil {
		t.Fatalf("workerSharedHead: %v", err)
	}
	if want := unfilteredWorkerSharedHeadWant(t, s, ownerMark); got != want {
		t.Fatalf("worker shared head must equal the unfiltered seed assembly (got %d bytes, want %d)", len(got), len(want))
	}
}

func TestWorkerSharedHeadSkipsBlankOwnerBlock(t *testing.T) {
	s := newWorkerTestServer(t)
	got, err := s.workerSharedHead()
	if err != nil {
		t.Fatalf("workerSharedHead: %v", err)
	}
	if got != unfilteredWorkerSharedHeadWant(t, s, "") {
		t.Fatal("blank owner text must preserve the shared seed assembly without an empty header")
	}
}

// TestWorkerBootSequenceFollowsTheWorkersRuntime pins BOTH directions of the
// runtime split at the shared-core seam: a codex worker must read
// boot_sequence_codex.md, a claude worker (and an unset runtime, which
// NormalizeRuntime folds to claude) boot_sequence.md.
//
// The expected seed file names are literals here on purpose: a want that called
// bootSequenceSeedName would move in lockstep with the code under test and
// could never disagree with it.
func TestWorkerBootSequenceFollowsTheWorkersRuntime(t *testing.T) {
	for _, tc := range []struct {
		name     string
		runtime  string
		bootSeed string
	}{
		{"codex", RuntimeCodex, "boot_sequence_codex.md"},
		{"claude", RuntimeClaude, "boot_sequence.md"},
		{"unset defaults to claude", "", "boot_sequence.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newWorkerTestServer(t)
			got, err := s.workerBootSequence(tc.runtime)
			if err != nil {
				t.Fatalf("workerBootSequence(%q): %v", tc.runtime, err)
			}
			seed, err := s.root.readSeedFile(tc.bootSeed)
			if err != nil {
				t.Fatalf("read %s: %v", tc.bootSeed, err)
			}
			if want := strings.TrimSpace(seed); got != want {
				t.Fatalf("runtime %q must be assembled from %s (got %d bytes, want %d)",
					tc.runtime, tc.bootSeed, len(got), len(want))
			}
		})
	}
}

// TestWorkerBootContextCarriesItsOwnRuntimeBootSequence is the END-TO-END half:
// the seam above proves workerBootSequence honours the argument it is handed,
// this proves the SPAWN path actually hands it the worker's own runtime. Without
// it, buildWorkerBootContext could pass a constant and the seam test would stay
// green.
//
// The discriminating evidence is content, not a file name: the Claude boot
// sequence tells the agent to run a bare `ocagent listen` under the built-in
// Monitor, while the codex runtime tail (worker_spawn.go) tells it NOT to start
// a listener because the App Server sidecar owns it. Those two must never be in
// one boot context — that pair is the reported bug.
func TestWorkerBootContextCarriesItsOwnRuntimeBootSequence(t *testing.T) {
	s := newWorkerTestServer(t)
	claudeBoot, err := s.root.readSeedFile("boot_sequence.md")
	if err != nil {
		t.Fatalf("read boot_sequence.md: %v", err)
	}
	codexBoot, err := s.root.readSeedFile("boot_sequence_codex.md")
	if err != nil {
		t.Fatalf("read boot_sequence_codex.md: %v", err)
	}
	claudeOnly, codexOnly := distinctiveLine(t, claudeBoot, codexBoot), distinctiveLine(t, codexBoot, claudeBoot)

	for _, tc := range []struct {
		name          string
		runtime       string
		wantSubstr    string
		notWantSubstr string
	}{
		{"codex worker reads the codex boot sequence", RuntimeCodex, codexOnly, claudeOnly},
		{"claude worker reads the claude boot sequence", RuntimeClaude, claudeOnly, codexOnly},
		{"unset runtime reads the claude boot sequence", "", claudeOnly, codexOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newWorkerTestServer(t)
			w := OutsourceWorker{ID: "ow-t4595", Codename: "O-7", Runtime: tc.runtime,
				Model: "sonnet", Effort: "medium"}
			task := Task{ID: "tk-t4595", Title: "T-4595 fixture", TypeKey: "general",
				Priority: "mid", ExecutorKind: TaskExecutorOutsource, ExecutorID: w.ID}
			putWorkerFixture(t, s, w)
			putTaskFixture(t, s, task)
			ctx, err := s.buildWorkerBootContext(w, task, nil)
			if err != nil {
				t.Fatalf("buildWorkerBootContext: %v", err)
			}
			if !strings.Contains(ctx, tc.wantSubstr) {
				t.Errorf("runtime %q: boot context is missing its own boot sequence (looked for %q)",
					tc.runtime, tc.wantSubstr)
			}
			if strings.Contains(ctx, tc.notWantSubstr) {
				t.Errorf("runtime %q: boot context carries the OTHER runtime's boot sequence (found %q)",
					tc.runtime, tc.notWantSubstr)
			}
		})
	}
}

// distinctiveLine returns a long line present in doc but absent from other — the
// probe the runtime assertions above use. It FAILS rather than returning "" when
// the two seeds have grown identical: an empty probe would make both a Contains
// and a NotContains assertion trivially satisfiable, i.e. a silently dead test.
func distinctiveLine(t *testing.T, doc, other string) string {
	t.Helper()
	best := ""
	for _, line := range strings.Split(doc, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 24 || strings.Contains(other, line) {
			continue
		}
		if len(line) > len(best) {
			best = line
		}
	}
	if best == "" {
		t.Fatal("the two boot-sequence seeds share every substantial line; there is nothing left to tell them apart")
	}
	return best
}

func TestWorkerBootContextCarriesRiskLanguage(t *testing.T) {
	ctx := workerCtx(t)
	for _, want := range []string{"風險", "backup-before-destructive", "verify-before-assert", "安全邊界"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("worker boot context is missing risk language %q", want)
		}
	}
}

// TestThereIsNoOutsourceOnlySeed — T-4595.
//
// The outsource-only seed (seeds/worker_context.md) is deleted. This asserts
// the deletion at the ASSET layer rather than by grepping the assembled text:
// a reader can smuggle the old wording back in under a different heading, but
// it cannot come back as a SEED without this failing, and re-adding the file is
// the realistic regression (a revert, or a merge from an older branch).
//
// 🔴 Scope, stated honestly: this proves the file is not shipped. It does not
// and cannot prove that no audience-specific paragraph was hidden somewhere
// else in the assembly.
func TestThereIsNoOutsourceOnlySeed(t *testing.T) {
	s := newWorkerTestServer(t)

	// Positive control: prove the seed reader can actually find a seed on this
	// server, so the negative below is not just a broken loader.
	if _, err := s.root.readSeedFile("system_interaction.md"); err != nil {
		t.Fatalf("seed loader is broken (%v) — the assertion below would be vacuous", err)
	}
	if _, err := s.root.readSeedFile("worker_context.md"); err == nil {
		t.Fatal("seeds/worker_context.md is back. There is no outsource-only seed: " +
			"every difference it carried was false, a restatement of the shared seed, " +
			"or unbacked by any harm (T-4595). The one survivor is a sentence in " +
			"system_interaction.md, written for both audiences.")
	}
}

// TestNoBootContextReinstatesTheRetiredOutsourceClaims — T-4595.
//
// The two retired claims with a NAMED harm:
//
//   - 「你沒有 roster 隊友關係」/「§11 的隊友模型對你而言就是你＋owner」. §11 of
//     the shared seed says staff AND outsource workers 都算 teammates; the
//     roster-read and post_chat tools are all available to a worker; its
//     resume_summary ships a roster block. THE HARM: an owner routinely puts
//     routing rules in the user-custom block ("403 分流請洽 Mira"), and a
//     worker that believes the world is only itself + the owner will not go
//     find her — hitting a governance 403 leaves it blindly retrying or
//     waiting for nothing.
//   - 「`report_waking` 不在你的開機序列」. False:
//     HandleReportWakingApiSelfWakingPost routes an outsource caller through
//     workerReportWaking on the very same /api/self/waking endpoint, and
//     resolveSelf's own comment says a worker walks the SAME 下線程序.
//     THE HARM: a worker that skips it never reports its live model, so the
//     cockpit's model column is structurally blank for every worker.
//
// 🔴 WHAT THIS ACTUALLY CHECKS, stated honestly: it is a WORDING tripwire over
// the assembled documents. It cannot detect the same claim rephrased in words
// nobody has written yet. It exists so that restoring the retired sentences —
// the realistic regression — turns red on its own assertion rather than sailing
// through.
func TestNoBootContextReinstatesTheRetiredOutsourceClaims(t *testing.T) {
	worker := workerCtx(t)
	_, bc := memberCtx(t)

	// Positive controls first: both premises must still hold in the shared
	// seed, otherwise the bans below guard nothing.
	if !strings.Contains(worker, "正職成員與外包工作者都算") {
		t.Fatal("shared §11 no longer says outsource workers count as teammates — " +
			"re-derive this guard before trusting it")
	}
	if !strings.Contains(worker, "report_waking") {
		t.Fatal("the assembled worker context never mentions report_waking at all — " +
			"the ban below would be measuring nothing")
	}

	for _, banned := range []string{
		"你沒有 roster 隊友關係",
		"隊友模型對你而言",
		"`report_waking` 不在你的開機序列",
		"這就是你的上線訊號",
		"你的開機序列以這一節為準",
	} {
		if strings.Contains(worker, banned) {
			t.Errorf("worker boot context reinstated the retired claim %q (T-4595)", banned)
		}
		if strings.Contains(bc.Context, banned) {
			t.Errorf("staff boot context picked up the retired outsource claim %q (T-4595)", banned)
		}
	}
}
