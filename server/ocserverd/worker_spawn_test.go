package main

// worker_spawn_test.go — the Phase 6 wake/reclaim lifecycle: boot-context
// assembly, warden targeting, fail-closed dispatch, pacing, and the reclaim
// hook + backstop. Everything runs against the real DAL/hub with zero network.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newWorkerTestServer builds an apiServer with the out-of-box roster seeded
// (Mira + the server-self warden). The seeds are read embed-only (from the
// staged seedsdist baked into the test binary), so no on-disk seeds/ is staged
// — a disk copy would be ignored anyway (T-e731).
func newWorkerTestServer(t *testing.T) *apiServer {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "worker-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return newAPIServer(dal, NewHub(), []byte("worker-test-secret"), 3600,
		assetRoot(t.TempDir()))
}

func putWorkerFixture(t *testing.T, s *apiServer, w OutsourceWorker) OutsourceWorker {
	t.Helper()
	if err := s.dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	return w
}

func putTaskFixture(t *testing.T, s *apiServer, task Task) Task {
	t.Helper()
	if task.Inputs == nil {
		task.Inputs = map[string]any{}
	}
	if err := s.dal.PutTask(task); err != nil {
		t.Fatalf("put task: %v", err)
	}
	return task
}

// connectWarden puts wardenID online on the hub (a live SSE downstream).
func connectWarden(t *testing.T, s *apiServer, wardenID string) {
	t.Helper()
	l, err := s.hub.Connect(wardenID, "")
	if err != nil {
		t.Fatalf("hub connect %s: %v", wardenID, err)
	}
	t.Cleanup(func() { s.hub.Disconnect(l) })
}

// decodeWardenFrame unwraps one FIFO frame ("data: {...}\n\n") into rpc + args.
func decodeWardenFrame(t *testing.T, frame []byte) (string, map[string]any) {
	t.Helper()
	raw := strings.TrimSuffix(strings.TrimPrefix(string(frame), "data: "), "\n\n")
	var env struct {
		Topic string `json:"topic"`
		Data  struct {
			RPC  string         `json:"rpc"`
			Args map[string]any `json:"args"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("frame decode: %v (%s)", err, raw)
	}
	if env.Topic != wardenCommandTopic {
		t.Fatalf("frame topic = %q, want %q", env.Topic, wardenCommandTopic)
	}
	return env.Data.RPC, env.Data.Args
}

// ── boot context ─────────────────────────────────────────────────────────────

func TestBuildWorkerBootContext_FullAssembly(t *testing.T) {
	s := newWorkerTestServer(t)
	w := OutsourceWorker{ID: "ow-abc", Codename: "O-7", Model: "opus", Effort: "high"}
	task := Task{
		ID: "t-1234567890ab", TypeKey: "review-pr", Title: "Review PR 42",
		DedupeKey: "https://pr/42", Description: "把 42 號 PR 看完",
		Priority:     TaskPriorityHigh,
		Inputs:       map[string]any{"pr_url": "https://pr/42", "repo": "x/y"},
		HandoverNote: "先跑既有測試", HandoverNoteTS: 1, HandoverNoteBy: "m-kyle",
	}
	manual := &TaskManual{
		TypeKey: "review-pr", DisplayName: "審查 PR",
		Purpose:   "review 一個 PR",
		Fields:    `[{"name":"pr_url","required":true,"is_key":true}]`,
		SopMD:     "先看 diff 再留結論",
		Learnings: "大 PR 先分檔看",
	}
	got, err := s.buildWorkerBootContext(w, task, manual)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if strings.Contains(got, ownerPlaceholder) {
		t.Errorf("the shared seed must have its %s placeholder substituted", ownerPlaceholder)
	}
	// Positive control FIRST: the two shared slots this assembly is now made of
	// must really be here. Without it every absence assertion below is satisfied
	// by an empty string.
	for _, want := range []string{
		"# Global Context",     // slot 1 — the 系統互動 seed's own H1
		"# 啟動程序（Boot Sequence", // slot 4 — the shared boot sequence
		"# Claude Code 執行環境",   // that runtime's 執行環境 section, leading slot 4
	} {
		if !strings.Contains(got, want) {
			t.Errorf("boot context missing the shared block %q", want)
		}
	}

	// T-4595 — WHAT THIS ASSEMBLY NO LONGER CONTAINS. Three groups, each with
	// its own reason; every literal is a field of the fixture above (or the
	// heading the old assembly emitted for it), so this stays a real assertion
	// rather than a spelling check.
	//
	//  1. 你的身分 — identity arrives the way it always has for staff, through
	//     the launcher's --append-system-prompt.
	//  2. the BOUND TASK — the boot sequence has the worker pick it up with
	//     the boot sequence's 領工 step, which serves the LIVE row; this copy was a spawn-time
	//     snapshot, stale by construction. Staff boot contexts never carried one.
	//  3. the TYPE MANUAL — staff pull a manual with get_task_manual at the
	//     moment they plan a task's steps; outsource now does the same.
	for _, gone := range []string{
		"# 你的身分", w.ID, w.Codename, // 1
		TaskNo(task.ID), "Review PR 42", "https://pr/42", // 2
		"把 42 號 PR 看完", "pr_url", "x/y",
		"交接備註", "m-kyle", "先跑既有測試",
		"# 任務手冊", "review 一個 PR", "先看 diff 再留結論", // 3
		"大 PR 先分檔看", "必填、識別鍵",
	} {
		if strings.Contains(got, gone) {
			t.Errorf("worker boot context still carries %q — a worker reads the staff "+
				"assembly minus slot 3, and nothing is written for it (T-4595)", gone)
		}
	}
}

// TestBuildWorkerBootContext_RuntimeGuidanceIsTheSeedsOwnAndItIsLast — T-4595,
// the replacement for the two _RuntimeTailHasFinalPrecedence tests.
//
// The listener-ownership instruction is what those two guarded, and it still
// has to be right for both runtimes and last in the document — a worker that
// reads Claude's "hold `ocagent listen` under Monitor" while running under a
// codex sidecar is the T-4595-era regression this repo already paid for once.
// What changed is WHERE it comes from: it used to be a hand-written outsource
// tail appended after the seed, and it now arrives inside the runtime's own
// boot-sequence seed — the same bytes staff read.
//
// The seed's shape changed again when the owner rewrote both files (2026-08-15):
// 執行環境 is now a top-level section that LEADS the boot-sequence block instead
// of a subsection inside it. So "the boot sequence is the tail" is asserted on
// the 啟動程序 heading, and 執行環境 is required to be the only heading between
// the rest of the document and it.
func TestBuildWorkerBootContext_RuntimeGuidanceIsTheSeedsOwnAndItIsLast(t *testing.T) {
	for _, tc := range []struct {
		name           string
		runtime        string
		wantEnvH1      string
		wantOwnership  string
		otherRuntimeH1 string
	}{
		{"codex", RuntimeCodex, "# Codex App Server 執行環境",
			"不要自己啟動 `ocagent listen`", "# Claude Code 執行環境"},
		{"claude", RuntimeClaude, "# Claude Code 執行環境",
			"用內建 Monitor 工具在背景掛住", "# Codex App Server 執行環境"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newWorkerTestServer(t)
			got, err := s.buildWorkerBootContext(
				OutsourceWorker{ID: "ow-" + tc.name, Codename: "C-1", Runtime: tc.runtime},
				Task{ID: "t-aabbccddeeff", Title: "x", Priority: TaskPriorityMid}, nil)
			if err != nil {
				t.Fatalf("fold: %v", err)
			}
			if !strings.Contains(got, tc.wantOwnership) {
				t.Errorf("%s worker is not told who owns the listener", tc.name)
			}
			if strings.Contains(got, tc.otherRuntimeH1) {
				t.Errorf("%s worker received the OTHER runtime's 執行環境 section", tc.name)
			}
			// Recency-authoritative: the boot-sequence block is the TAIL, so
			// nothing follows it, and the runtime's 執行環境 section is the only
			// heading between the rest of the document and it. Before T-4595 the
			// three shared blocks were grouped at the TOP for workers and the
			// persona came after them — the one asymmetry with nothing behind it.
			env := strings.LastIndex(got, tc.wantEnvH1)
			if env < 0 {
				t.Fatalf("%s worker is missing %q", tc.name, tc.wantEnvH1)
			}
			rest := got[env+len(tc.wantEnvH1):]
			next := strings.Index(rest, "\n# ")
			if next < 0 || !strings.HasPrefix(rest[next+1:], bootSequenceH1) {
				t.Fatalf("%s: 執行環境 is not immediately followed by the 啟動程序 heading — "+
					"the runtime note must lead the tail block, not sit loose in the "+
					"document", tc.name)
			}
			if strings.Contains(rest[next+1:], "\n# ") {
				t.Errorf("%s: something follows the 啟動程序 block; it must be last", tc.name)
			}
		})
	}
}

// TestWorkerBootContextIsTheStaffFoldMinusThePersona — T-4595, the whole ruling
// in one equality.
//
// 「外包的 boot context ＝ 正職的 boot context 扣掉第 3 格（角色說明→判準→長期筆記）。
// 一個字都不為外包另寫。」
//
// So the want is BUILT FROM THE STAFF FOLD: take the document a staff member
// actually receives, cut the persona slot out of it, and require the worker's
// document to equal what is left, byte for byte. That is deliberately not a
// "contains" assertion — every weaker form was satisfied by the assembly this
// change replaced, which carried an overlay, an identity block, the whole bound
// task, the whole type manual, and a second copy of the runtime guidance.
//
// Both folds run on ONE server with a non-blank owner block, so the shared
// slots are the same bytes on both sides by construction rather than by a
// second re-derivation of them here.
//
// 🔴 SCOPE, MEASURED — IT GUARDS THE ASSEMBLY, NOT THE SEED TEXT. Because the
// want is built FROM the staff fold, any edit to a shared seed moves BOTH sides
// of the equality and this stays green. An independent review confirmed it:
// changing a sentence of prose in system_interaction.md (verified to reach the
// embed) left the ENTIRE ocserverd suite passing, this test included; inserting
// a single "\n" on the worker side alone turned exactly this test red. So it
// answers "are the two documents still the same shape?" and says NOTHING about
// whether the shared documents still say the right thing. Guards for the seed
// WORDING are separate and must be written separately — see
// worker_handover_lessons_t4595_test.go and
// TestNoBootContextReinstatesTheRetiredOutsourceClaims. Do not read a green
// here as cover for a seed edit.
func TestWorkerBootContextIsTheStaffFoldMinusThePersona(t *testing.T) {
	s := newWorkerTestServer(t)
	const ownerMark = "T4595-OWNER-CUSTOM-MARKER"
	if err := s.dal.PutUserContext(UserContext{Text: ownerMark}); err != nil {
		t.Fatalf("put user context: %v", err)
	}

	staff, err := s.buildBootContext("", nil, "")
	if err != nil || staff == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	worker, err := s.buildWorkerBootContext(
		OutsourceWorker{ID: "ow-eq", Codename: "O-9", Model: "opus", Effort: "high",
			Runtime: RuntimeClaude},
		Task{ID: "t-aabbccddeeff", TypeKey: "review-pr", Title: "Review PR 42",
			Priority: TaskPriorityHigh},
		&TaskManual{TypeKey: "review-pr", DisplayName: "審查 PR",
			Purpose: "review 一個 PR", SopMD: "先看 diff 再留結論"})
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}

	// Cut slot 3 out of the staff document: everything from the 角色說明 header
	// up to (but not including) the START of slot 4, plus the "\n\n" that joined
	// it to the block before.
	//
	// Slot 4 no longer BEGINS at the 啟動程序 heading: the owner's 2026-08-15
	// rewrite hoisted the runtime 執行環境 note into a top-level section that
	// leads the block. Cutting at 啟動程序 would leave that note on one side of
	// the equality only — which is how this anchor announced the change.
	role := strings.Index(staff.Context, "# Role: ")
	boot := strings.Index(staff.Context, "# Claude Code 執行環境")
	if role < 0 || boot < 0 || role >= boot {
		t.Fatalf("cannot locate slot 3 in the staff fold (角色說明=%d 啟動程序=%d) — "+
			"the staff assembly moved and this equality must be re-derived", role, boot)
	}
	// Positive control: the persona really is a substantial block, so "minus
	// slot 3" is a real subtraction and not a no-op that makes this vacuous.
	if boot-role < 200 {
		t.Fatalf("staff slot 3 is only %d bytes — too small to be the persona; "+
			"the subtraction below would prove nothing", boot-role)
	}
	want := staff.Context[:role] + staff.Context[boot:]

	// And prove the owner block is on BOTH sides, above the cut: if it were
	// still fourth (the pre-T-4595 staff order) it would sit inside the excised
	// span and this equality could hold while the two documents disagreed about
	// where the owner's additions live.
	if o := strings.Index(want, ownerMark); o < 0 || o > role {
		t.Fatalf("使用者自訂 must sit in slot 2, above the persona (found at %d, cut at %d)", o, role)
	}

	if worker != want {
		t.Errorf("outsource boot context is not the staff fold minus slot 3\n"+
			"got  %d bytes\nwant %d bytes\n"+
			"外包的 boot context ＝ 正職的扣掉第 3 格（角色說明→判準→長期筆記），"+
			"一個字都不為外包另寫（T-4595）\n"+
			"⚠️ 這顆守的是【組裝結構】，不是 seed 的文字內容：want 由正職那份實際產出"+
			"切出來，所以改 seed 會讓兩邊一起移動、這顆不會紅。要守 seed 措辭請看"+
			" worker_handover_lessons_t4595_test.go 與"+
			" TestNoBootContextReinstatesTheRetiredOutsourceClaims。",
			len(worker), len(want))
	}
}

// T-ba04: a worker minted onto a task that is in `reassigning` gets a TAKEOVER
// section in its boot context — who its predecessor is (id) + the "hand over
// first, THEN flip the status yourself" protocol. A non-reassigning task must
// NOT carry that section (a fresh assignment has no predecessor). RED/GREEN pin
// for the boot-context handover fold.
// TestWorkerBootContextIsInvariantToTheTaskAndItsManual — T-4595 replaces two
// tests that pinned blocks the assembly no longer emits
// (_ReassigningTakeoverSection and _MissingManualIsHonest).
//
// Neither the bound task nor its type manual is pasted into a worker's boot
// context any more: the boot sequence has the worker pick the task up with
// its own read (which serves the LIVE row, lock and handover note included), and
// a manual is pulled with get_task_manual at the moment the task is planned —
// exactly what staff do, in exactly the same places. So the STRONGEST statement
// available is INVARIANCE: the assembled document does not vary with either
// input at all.
//
// That is deliberately stronger than a list of absent substrings. A future
// reinstatement of any per-task or per-manual text — the takeover protocol, the
// honest "手冊目前不存在" placeholder, a description, a single field name —
// makes two of these three documents differ and turns this red, without anyone
// having to predict the wording.
func TestWorkerBootContextIsInvariantToTheTaskAndItsManual(t *testing.T) {
	s := newWorkerTestServer(t)
	if err := s.dal.PutMember(Member{
		ID: "m-pred", Name: "Ken", Kind: KindAssistant, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put predecessor: %v", err)
	}
	w := OutsourceWorker{ID: "ow-new", Codename: "O-2", Model: "opus", Effort: "high"}

	plain := Task{ID: "t-aabbccddeeff", TypeKey: "x", Title: "接手任務",
		Priority: TaskPriorityMid}

	// A reassignment takeover, with a predecessor and a handover note — the
	// richest task shape the old assembly rendered.
	takeover := plain
	takeover.Lock = TaskLockReassigning
	takeover.ReassignedFrom = "m-pred"
	takeover.ReassignedFromKind = TaskExecutorMember
	takeover.Description = "把 42 號 PR 看完"
	takeover.HandoverNote = "先跑既有測試"
	takeover.HandoverNoteTS = 1
	takeover.HandoverNoteBy = "m-kyle"

	manual := &TaskManual{
		TypeKey: "x", DisplayName: "審查 PR",
		Purpose:   "review 一個 PR",
		Fields:    `[{"name":"pr_url","required":true,"is_key":true}]`,
		SopMD:     "先看 diff 再留結論",
		Learnings: "大 PR 先分檔看",
	}

	base, err := s.buildWorkerBootContext(w, plain, nil)
	if err != nil {
		t.Fatalf("fold plain: %v", err)
	}
	// Positive control: the fold really did produce a document. Comparing two
	// empty strings would satisfy every assertion below.
	if len(base) < 10000 {
		t.Fatalf("worker boot context is only %d bytes — the shared core is missing "+
			"and the equalities below would be vacuous", len(base))
	}

	withTakeover, err := s.buildWorkerBootContext(w, takeover, nil)
	if err != nil {
		t.Fatalf("fold takeover: %v", err)
	}
	if withTakeover != base {
		t.Error("worker boot context varies with the bound task — the worker reads " +
			"the live task; a spawn-time copy can only be stale (T-4595)")
	}

	withManual, err := s.buildWorkerBootContext(w, takeover, manual)
	if err != nil {
		t.Fatalf("fold with manual: %v", err)
	}
	if withManual != base {
		t.Error("worker boot context varies with the type manual — a manual is pulled " +
			"with get_task_manual when the task is planned, as staff do (T-4595)")
	}
}

// ── warden targeting ─────────────────────────────────────────────────────────

// putWardenFixture registers one more active warden (= machine) on the roster.
func putWardenFixture(t *testing.T, s *apiServer, id string) {
	t.Helper()
	if err := s.dal.PutMember(Member{
		ID: id, Name: id + " box", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put warden %s: %v", id, err)
	}
}

// connectAgentOn projects an agent session onto a machine (a live SSE whose
// token carries machineID as its claim) — the agentLoadOn input.
func connectAgentOn(t *testing.T, s *apiServer, agentID, machineID string) {
	t.Helper()
	l, err := s.hub.Connect(agentID, machineID)
	if err != nil {
		t.Fatalf("hub connect %s@%s: %v", agentID, machineID, err)
	}
	t.Cleanup(func() { s.hub.Disconnect(l) })
}

// pickWarden calls the placement picker with a throwaway worker + wall clock —
// the existing placement tests below predate the T-9ccf cooldown/health params
// and only exercise the online/idlest/preference logic.
func pickWarden(s *apiServer, pref string) string {
	return s.pickWorkerWarden(OutsourceWorker{ID: "ow-pick"}, pref, nowSecs())
}

// TestPickWorkerWarden_UnnamedMachineNeverPlaces: placement is an explicit owner
// decision — an empty preference (and the legacy "auto" spelling, which names no
// machine either) resolves to NOTHING, no matter how many healthy idle wardens
// are online. This is the whole point of the change: a worker nobody placed is
// not quietly placed somewhere.
func TestPickWorkerWarden_UnnamedMachineNeverPlaces(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")

	for _, pref := range []string{"", "auto"} {
		if got := pickWarden(s, pref); got != "" {
			t.Fatalf("preference %q with healthy wardens online: got %q, want \"\" (no placement)",
				pref, got)
		}
	}
	// SENTINEL: the same fixture places fine once a machine is actually named,
	// so the emptiness above is the preference, not a broken fixture.
	if got := pickWarden(s, "m-other"); got != "m-other" {
		t.Fatalf("named machine: got %q, want m-other", got)
	}
}

func TestPickWorkerWarden_FiltersBySelectedRuntime(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-codex")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-codex")
	s.telemetry.Set(ServerSelfHost, map[string]any{"runtimes": map[string]any{
		RuntimeClaude: map[string]any{"installed": true, "logged_in": true},
	}})
	s.telemetry.Set("m-codex", map[string]any{"runtimes": map[string]any{
		RuntimeCodex: map[string]any{"installed": true, "logged_in": true},
	}})
	w := OutsourceWorker{ID: "ow-codex", Runtime: RuntimeCodex}
	if got := s.pickWorkerWarden(w, "m-codex", nowSecs()); got != "m-codex" {
		t.Fatalf("Codex worker on the Codex-capable machine: got %q, want m-codex", got)
	}
	// A machine that cannot run the worker's runtime is REFUSED outright — no
	// substitution onto the runtime-capable host that is sitting right there.
	if got := s.pickWorkerWarden(w, ServerSelfHost, nowSecs()); got != "" {
		t.Fatalf("Codex worker pinned to a claude-only machine: got %q, want \"\"", got)
	}
}

func TestPickWorkerWarden_SpecifiedMachineOnline_Honoured(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	// m-other is BUSIER than server-self — an explicit machine id is honoured
	// regardless: load is no longer an input to placement.
	connectAgentOn(t, s, "mira", "m-other")
	if got := pickWarden(s, "m-other"); got != "m-other" {
		t.Fatalf("specified online machine: got %q, want m-other", got)
	}
}

// TestPickWorkerWarden_SpecifiedMachineOffline_FailsClosed: the old contract
// substituted another host when the named machine was offline, which booted the
// worker somewhere the owner never chose. It now dispatches NOTHING.
func TestPickWorkerWarden_SpecifiedMachineOffline_FailsClosed(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	putWardenFixture(t, s, "m-third")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-third")
	// m-other is on the roster but OFFLINE, while two healthy wardens are online
	// and idle — exactly the situation the fallback used to exploit.
	if got := pickWarden(s, "m-other"); got != "" {
		t.Fatalf("offline specified machine: got %q, want \"\" (no substitution)", got)
	}
	// SENTINEL: bring it online and the same pin places immediately.
	connectWarden(t, s, "m-other")
	if got := pickWarden(s, "m-other"); got != "m-other" {
		t.Fatalf("machine back online: got %q, want m-other", got)
	}
	// An id naming no machine at all is likewise nothing.
	if got := pickWarden(s, "m-nonexistent"); got != "" {
		t.Fatalf("unknown machine id: got %q, want \"\"", got)
	}
}

// TestPickWorkerWarden_CooledMachineSkipped (T-9ccf DoD② 換機): a machine benched
// for a worker (a boot failure within the cooldown window) is refused while the
// bench holds — but the worker now WAITS for its own machine instead of rotating
// onto one the owner did not choose.
func TestPickWorkerWarden_CooledMachineSkipped(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	now := 1_000_000.0
	w := OutsourceWorker{ID: "ow-cool"}

	s.outsourceMu.Lock()
	s.benchWorkerMachine(w.ID, "m-other", now)
	got := s.pickWorkerWarden(w, "m-other", now)
	s.outsourceMu.Unlock()
	if got != "" {
		t.Fatalf("benched machine must not place (and must not rotate), got %q", got)
	}

	// After the cooldown window elapses the pinned machine is eligible again.
	s.outsourceMu.Lock()
	got = s.pickWorkerWarden(w, "m-other", now+workerSpawnCooldownSecs+1)
	s.outsourceMu.Unlock()
	if got != "m-other" {
		t.Fatalf("expired cooldown must re-admit the pinned machine, got %q", got)
	}
}

// TestFoldWorkerCommandResult_RefusedStartBenchesTarget (T-9ccf DoD② 換機): a
// REFUSED start receipt (the converged member verb — P5b) benches the worker's
// last spawn target so the next pick rotates off it. A legacy worker_start
// receipt from an old warden benches too; a SUCCESSFUL receipt benches nothing.
func TestFoldWorkerCommandResult_RefusedStartBenchesTarget(t *testing.T) {
	s := newWorkerTestServer(t)
	now := nowSecs()
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-rf", Codename: "O-1", TaskID: "t-rf", Status: WorkerStatusAssigned,
		CreatedTS: now,
	})
	s.workerSpawnTarget["ow-rf"] = "m-bad" // in-memory spawn observation (P7d)
	s.foldWorkerCommandResult("ow-rf", map[string]any{
		"rpc": reconcileCmdStart, "ok": false, "reason": "session_already_exists",
	}, triggerServer)

	s.outsourceMu.Lock()
	cooling := s.workerMachineCoolingOn("ow-rf", "m-bad", nowSecs())
	s.outsourceMu.Unlock()
	if !cooling {
		t.Fatal("a refused start must bench its last spawn target")
	}

	// A refused LEGACY worker_start receipt (old warden, transition window)
	// still benches.
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-lg", Codename: "O-3", TaskID: "t-lg", Status: WorkerStatusAssigned,
		CreatedTS: now,
	})
	s.workerSpawnTarget["ow-lg"] = "m-old"
	s.foldWorkerCommandResult("ow-lg", map[string]any{
		"rpc": legacyWardenCmdWorkerStart, "ok": false, "reason": "session_already_exists",
	}, triggerServer)
	s.outsourceMu.Lock()
	cooling = s.workerMachineCoolingOn("ow-lg", "m-old", nowSecs())
	s.outsourceMu.Unlock()
	if !cooling {
		t.Fatal("a refused legacy worker_start must bench its last spawn target")
	}

	// A successful start receipt benches nothing.
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-ok", Codename: "O-2", TaskID: "t-ok", Status: WorkerStatusAssigned,
		CreatedTS: now,
	})
	s.workerSpawnTarget["ow-ok"] = "m-good"
	s.foldWorkerCommandResult("ow-ok", map[string]any{
		"rpc": reconcileCmdStart, "ok": true,
	}, triggerServer)
	s.outsourceMu.Lock()
	cooling = s.workerMachineCoolingOn("ow-ok", "m-good", nowSecs())
	s.outsourceMu.Unlock()
	if cooling {
		t.Fatal("a successful start must NOT bench its target")
	}
}

// ── wake dispatch ────────────────────────────────────────────────────────────

func TestNotifyWorkerSpawn_NoOnlineWarden_FailClosed(t *testing.T) {
	s := newWorkerTestServer(t)
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000001", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-1",
	})
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-1", Codename: "O-1", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
	})
	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	_, stamped := s.workerSpawnAt[w.ID]
	s.outsourceMu.Unlock()
	if stamped {
		t.Error("fail-closed dispatch must NOT stamp pacing (next tick must retry)")
	}
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Errorf("nothing may be enqueued with no online warden, got %d frames", len(got))
	}
}

func TestNotifyWorkerSpawn_DispatchesMemberStart_AndPaces(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000002", TypeKey: "review-pr", Title: "Review PR 42",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-2",
	})
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "review-pr", Purpose: "p",
		Fields: "[]", Assignee: `{"kind":"outsource","model":"opus"}`}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-2", Codename: "O-2", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
		DesiredMachineID: ServerSelfHost, // explicit placement (owner ruling 2026-07-25)
	})

	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	s.notifyWorkerSpawn(w, nowSecs()) // paced: within the retry window → NOT re-enqueued
	s.outsourceMu.Unlock()

	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want exactly 1 start (pacing), got %d", len(frames))
	}
	rpc, args := decodeWardenFrame(t, frames[0].Frame)
	if rpc != reconcileCmdStart {
		t.Fatalf("rpc = %q, want start (P5b: the member verb)", rpc)
	}
	if args["member_id"] != "ow-2" || args["model"] != "opus" || args["effort"] != "high" {
		t.Errorf("args = %v", args)
	}
	if args["role"] != workerBootRoleLabel {
		t.Errorf("role = %v, want %q", args["role"], workerBootRoleLabel)
	}
	if args["session_name"] != "" {
		t.Errorf("session_name = %v, want \"\" (warden derives member-<ow-id>)", args["session_name"])
	}
	token, _ := args["member_token"].(string)
	if token == "" {
		t.Fatal("member_token missing from the start frame")
	}
	// The minted token's sub must be the worker id (server-mint, agent floor).
	if sub := jwtSubOf(t, token); sub != "ow-2" {
		t.Errorf("token sub = %q, want ow-2", sub)
	}
	// A案 P1: the token now burns the ACTUAL dispatch host as machine_id — here
	// server-self was the "auto" pick, so the resolved warden id (never a literal
	// "auto"/"") rides the claim, mirroring the member token.
	if mid := jwtMachineOf(t, token); mid != ServerSelfHost {
		t.Errorf("token machine_id = %q, want %s (the resolved auto pick)", mid, ServerSelfHost)
	}
	persona, _ := args["persona_context"].(string)
	// T-4595: this used to require the codename and the bound task title in the
	// frame. Neither is in a worker's boot context any more — it is the staff
	// fold minus the persona slot. What this frame assertion is actually FOR is
	// that the fold really rode the wire (an empty or truncated persona_context
	// boots a worker with no instructions at all), so it now checks the shared
	// blocks, plus the absences so the removal cannot quietly come back through
	// the spawn path.
	for _, want := range []string{"# Global Context", "# 啟動程序（Boot Sequence"} {
		if !strings.Contains(persona, want) {
			t.Errorf("persona_context is missing the shared block %q", want)
		}
	}
	for _, gone := range []string{"O-2", "Review PR 42", "# 你的身分"} {
		if strings.Contains(persona, gone) {
			t.Errorf("persona_context still carries %q (T-4595)", gone)
		}
	}
	// The token must never leak into the persona text (file/env only).
	if strings.Contains(persona, token) {
		t.Error("worker token leaked into the persona context")
	}
	if s.workerSpawnTarget["ow-2"] != ServerSelfHost {
		t.Errorf("spawn target = %q, want %s", s.workerSpawnTarget["ow-2"], ServerSelfHost)
	}
}

// TestNotifyWorkerSpawn_StampsSpawnObservation: each dispatch must stamp the
// in-memory spawn observation (workerSpawnAttempts++/workerSpawnAt/
// workerSpawnTarget) the cockpit projection folds from. In-memory by design
// since the P7d fold (the former durable spawn columns were retired with the
// outsource_worker table) — a restart forgetting them is the accepted trade.
func TestNotifyWorkerSpawn_StampsSpawnObservation(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000009", TypeKey: "review-pr", Title: "Review",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-9",
	})
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "review-pr", Purpose: "p",
		Fields: "[]", Assignee: `{"kind":"outsource","model":"opus"}`}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-9", Codename: "O-9", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
		DesiredMachineID: ServerSelfHost, // explicit placement (owner ruling 2026-07-25)
	})

	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()

	if got := s.workerSpawnAttempts["ow-9"]; got != 1 {
		t.Fatalf("workerSpawnAttempts = %d, want 1", got)
	}
	if got, _ := s.workerSpawnObs("ow-9"); got != ServerSelfHost {
		t.Fatalf("workerSpawnTarget = %q, want %s", got, ServerSelfHost)
	}
	if s.workerSpawnAt["ow-9"] == 0 {
		t.Fatalf("workerSpawnAt must be stamped, got 0")
	}
	// A案 P6: a successful dispatch stamps the shared-FSM in-flight state so
	// reconcileWorkerLiveness never doubles (and never zombie-misreads) a start
	// it did not decide itself.
	st := s.workerReconcileStates["ow-9"]
	if st.LastCommand != reconcileCmdStart || st.Phase != reconcilePhaseStarting {
		t.Fatalf("FSM state after dispatch = %+v, want start/starting", st)
	}
}

func TestNotifyWorkerSpawn_HonoursManualMachinePreference(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000000e", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-e",
	})
	// The manual pins the spawn to m-other — the frame must land on ITS FIFO
	// even though the tie-break order would otherwise pick server-self.
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "review-pr", Purpose: "p",
		Fields:   "[]",
		Assignee: `{"kind":"outsource","model":"opus","machine":"m-other"}`}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-e", Codename: "O-14", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
	})

	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()

	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("server-self must receive nothing, got %d frames", got)
	}
	frames := s.hub.DrainWardenCommands("m-other")
	if len(frames) != 1 {
		t.Fatalf("want 1 start on the preferred machine, got %d", len(frames))
	}
	rpc, args := decodeWardenFrame(t, frames[0].Frame)
	if rpc != reconcileCmdStart || args["member_id"] != "ow-e" {
		t.Errorf("frame = %s %v", rpc, args)
	}
	if s.workerSpawnTarget["ow-e"] != "m-other" {
		t.Errorf("spawn target = %q, want m-other", s.workerSpawnTarget["ow-e"])
	}
	// A案 P1: the machine_id claim must equal the pinned host it actually landed
	// on, not the literal preference string.
	token, _ := args["member_token"].(string)
	if mid := jwtMachineOf(t, token); mid != "m-other" {
		t.Errorf("token machine_id = %q, want m-other (the pinned dispatch host)", mid)
	}
}

// readWorker re-reads a worker row from the DAL — the blocked-placement stamp
// lands on the ROW, so every assertion below reads it back rather than trusting
// the in-memory copy the caller passed in.
func readWorker(t *testing.T, s *apiServer, id string) OutsourceWorker {
	t.Helper()
	w, err := s.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("re-read worker %s: %+v %v", id, w, err)
	}
	return *w
}

// blockedSpawnFixture seeds one assigned worker on a live outsource task, ready
// for a spawn attempt that is expected to be refused at placement time.
func blockedSpawnFixture(t *testing.T, s *apiServer, taskID, workerID, machine string) OutsourceWorker {
	t.Helper()
	task := putTaskFixture(t, s, Task{
		ID: taskID, Title: "placement stall", Status: TaskStatusNotStarted,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorOutsource,
		ExecutorID: workerID,
	})
	return putWorkerFixture(t, s, OutsourceWorker{
		ID: workerID, Codename: "O-" + workerID, Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned, DesiredMachineID: machine,
	})
}

// TestNotifyWorkerSpawn_StampsNoMachineSelectedReason: a spawn nobody placed
// dispatches NOTHING and says so on the worker row (last_op_reason
// no_machine_selected) — and because the 30s cadence retries the same blocked
// spawn forever, an identical retry must NOT re-stamp the row.
func TestNotifyWorkerSpawn_StampsNoMachineSelectedReason(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	w := blockedSpawnFixture(t, s, "t-0000000000b1", "ow-nm", "")

	now := 1_000_000.0
	s.outsourceMu.Lock()
	dispatched := s.notifyWorkerSpawn(w, now)
	s.outsourceMu.Unlock()
	if dispatched {
		t.Fatal("a worker no one placed must not be dispatched")
	}
	// Two healthy idle wardens are online: neither may be substituted.
	for _, host := range []string{ServerSelfHost, "m-other"} {
		if got := s.hub.DrainWardenCommands(host); len(got) != 0 {
			t.Fatalf("%s must receive nothing, got %d frames", host, len(got))
		}
	}
	blocked := readWorker(t, s, "ow-nm")
	if !strings.HasPrefix(blocked.LastOpReason, placementReasonNoMachine+":") {
		t.Fatalf("last_op_reason = %q, want a %s reason", blocked.LastOpReason,
			placementReasonNoMachine)
	}
	if blocked.LastOp != reconcileCmdStart || blocked.LastOpOK == nil || *blocked.LastOpOK {
		t.Fatalf("a blocked spawn must stamp a FAILED start op: %+v", blocked)
	}
	if blocked.LastOpAt != now {
		t.Fatalf("last_op_at = %v, want %v", blocked.LastOpAt, now)
	}

	// The anti-churn guarantee: same cause, same row, no second write.
	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(blocked, now+workerSpawnRetrySecs+1)
	s.outsourceMu.Unlock()
	if again := readWorker(t, s, "ow-nm"); again.LastOpAt != blocked.LastOpAt {
		t.Fatalf("an identical blocked retry must NOT re-stamp: last_op_at %v → %v",
			blocked.LastOpAt, again.LastOpAt)
	}

	// SENTINEL: name an online machine and the very same worker dispatches, so
	// the refusals above are the missing placement, not a broken fixture.
	blocked.DesiredMachineID = "m-other"
	if err := s.dal.PutOutsourceWorker(blocked); err != nil {
		t.Fatalf("pin worker: %v", err)
	}
	s.outsourceMu.Lock()
	ok := s.notifyWorkerSpawn(readWorker(t, s, "ow-nm"), now+2*workerSpawnRetrySecs)
	s.outsourceMu.Unlock()
	if !ok || len(s.hub.DrainWardenCommands("m-other")) != 1 {
		t.Fatal("a named online machine must take the worker")
	}
}

// TestNotifyWorkerSpawn_StampsMachineUnavailableReason: a worker pinned to an
// OFFLINE machine stalls with machine_unavailable — it is never re-placed onto
// the healthy warden sitting right there.
func TestNotifyWorkerSpawn_StampsMachineUnavailableReason(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-pinned")
	connectWarden(t, s, ServerSelfHost) // online, idle, and NOT the pin
	w := blockedSpawnFixture(t, s, "t-0000000000b2", "ow-off", "m-pinned")

	now := 2_000_000.0
	s.outsourceMu.Lock()
	dispatched := s.notifyWorkerSpawn(w, now)
	s.outsourceMu.Unlock()
	if dispatched {
		t.Fatal("an offline pin must not be dispatched anywhere")
	}
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Fatalf("no other machine may be substituted for the offline pin, got %d frames",
			len(got))
	}
	blocked := readWorker(t, s, "ow-off")
	if !strings.HasPrefix(blocked.LastOpReason, placementReasonUnavailable+":") {
		t.Fatalf("last_op_reason = %q, want a %s reason", blocked.LastOpReason,
			placementReasonUnavailable)
	}
	if !strings.Contains(blocked.LastOpReason, "m-pinned") {
		t.Fatalf("the reason must name the machine the owner chose: %q", blocked.LastOpReason)
	}
	if blocked.LastOpOK == nil || *blocked.LastOpOK {
		t.Fatalf("a blocked spawn must stamp last_op_ok=false: %+v", blocked)
	}

	// SENTINEL: the pin comes online and the identical worker boots THERE.
	connectWarden(t, s, "m-pinned")
	s.outsourceMu.Lock()
	ok := s.notifyWorkerSpawn(blocked, now+workerSpawnRetrySecs+1)
	s.outsourceMu.Unlock()
	if !ok {
		t.Fatal("the pinned machine coming online must dispatch the worker")
	}
	if got := len(s.hub.DrainWardenCommands("m-pinned")); got != 1 {
		t.Fatalf("want 1 start on the pinned machine, got %d", got)
	}
}

// TestNotifyWorkerSpawn_TaskRowMachineSurvivesRestart: the placement source
// chain reads the DURABLE 發包 target on the task row. workerMachinePref is
// in-memory by design, so a restart forgets it — and an ad-hoc 發包 worker has no
// type manual to fall back to. The task row is what keeps it from stalling
// permanently, so this test deliberately leaves the in-memory map EMPTY.
func TestNotifyWorkerSpawn_TaskRowMachineSurvivesRestart(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-dispatch")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-dispatch")

	// No TypeKey → no type manual, exactly like an ad-hoc 發包.
	task := putTaskFixture(t, s, Task{
		ID: "t-0000000000c1", Title: "ad-hoc 發包", Status: TaskStatusNotStarted,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorOutsource,
		ExecutorID: "ow-restart", OutsourceMachine: "m-dispatch",
	})
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-restart", Codename: "O-restart", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
	})
	if _, ok := s.workerMachinePref[w.ID]; ok {
		t.Fatal("fixture must leave workerMachinePref empty — that is what a restart forgets")
	}

	s.outsourceMu.Lock()
	dispatched := s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()
	if !dispatched {
		t.Fatal("the task row's durable 發包 target must place the worker after a restart")
	}
	frames := s.hub.DrainWardenCommands("m-dispatch")
	if len(frames) != 1 {
		t.Fatalf("want 1 start on the task row's machine, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != "ow-restart" {
		t.Fatalf("frame = %s %v", rpc, args)
	}
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("no other machine may be substituted, got %d frames", got)
	}
}

// TestNotifyWorkerSpawn_BlockedReasonNamesTheCause: the refusal text is the
// ACTUAL cause, not a three-way disjunction the owner has to guess between —
// each way a named machine can refuse a worker writes its own distinguishable
// machine_unavailable reason onto the worker row.
func TestNotifyWorkerSpawn_BlockedReasonNamesTheCause(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost) // online, idle, and never the pin
	putWardenFixture(t, s, "m-offline") // on the roster, never connects
	putWardenFixture(t, s, "m-benched")
	connectWarden(t, s, "m-benched")
	putWardenFixture(t, s, "m-claudeonly")
	connectWarden(t, s, "m-claudeonly")
	s.telemetry.Set("m-claudeonly", map[string]any{"runtimes": map[string]any{
		RuntimeClaude: map[string]any{"installed": true, "logged_in": true},
	}})
	// An ACTIVE roster member that is not a machine at all.
	putTestMember(t, s, testAgent("m-person"))

	now := 4_000_000.0
	cases := []struct {
		name, workerID, taskID, machine, runtime, phrase string
		bench                                            bool
	}{
		{name: "offline", workerID: "ow-c1", taskID: "t-0000000000d1",
			machine: "m-offline", phrase: "is offline"},
		{name: "benched after a failed boot", workerID: "ow-c2", taskID: "t-0000000000d2",
			machine: "m-benched", bench: true, phrase: "benched after a failed boot"},
		{name: "wrong runtime", workerID: "ow-c3", taskID: "t-0000000000d3",
			machine: "m-claudeonly", runtime: RuntimeCodex,
			phrase: "does not provide the '" + RuntimeCodex + "' runtime"},
		{name: "not an active machine", workerID: "ow-c4", taskID: "t-0000000000d4",
			machine: "m-person", phrase: "is not an active machine"},
		{name: "does not exist", workerID: "ow-c5", taskID: "t-0000000000d5",
			machine: "m-ghost", phrase: "does not exist"},
	}
	seen := map[string]string{}
	for _, c := range cases {
		w := blockedSpawnFixture(t, s, c.taskID, c.workerID, c.machine)
		if c.runtime != "" {
			w.Runtime = c.runtime
			putWorkerFixture(t, s, w)
		}
		s.outsourceMu.Lock()
		if c.bench {
			s.benchWorkerMachine(w.ID, c.machine, now)
		}
		dispatched := s.notifyWorkerSpawn(w, now)
		s.outsourceMu.Unlock()
		if dispatched {
			t.Fatalf("%s: a refused placement must not dispatch", c.name)
		}
		blocked := readWorker(t, s, c.workerID)
		if !strings.HasPrefix(blocked.LastOpReason, placementReasonUnavailable+":") {
			t.Fatalf("%s: last_op_reason = %q, want a %s reason", c.name,
				blocked.LastOpReason, placementReasonUnavailable)
		}
		if !strings.Contains(blocked.LastOpReason, c.phrase) {
			t.Fatalf("%s: last_op_reason must name the cause %q, got %q", c.name,
				c.phrase, blocked.LastOpReason)
		}
		if prior, dup := seen[blocked.LastOpReason]; dup {
			t.Fatalf("%s and %s share one reason %q — the causes are not distinguishable",
				c.name, prior, blocked.LastOpReason)
		}
		seen[blocked.LastOpReason] = c.name
	}
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("no refusal may be substituted onto the idle warden, got %d frames", got)
	}

	// SENTINEL: an online machine that can take the worker still dispatches, so
	// the refusals above are the causes, not a broken fixture.
	ok := blockedSpawnFixture(t, s, "t-0000000000d6", "ow-c6", ServerSelfHost)
	s.outsourceMu.Lock()
	dispatched := s.notifyWorkerSpawn(ok, now)
	s.outsourceMu.Unlock()
	if !dispatched || len(s.hub.DrainWardenCommands(ServerSelfHost)) != 1 {
		t.Fatal("a healthy named machine must take the worker")
	}
}

// TestStampWorkerPlacementBlocked_ReReadsTheRowBeforeWriting: the stamp is a
// whole-row write on a snapshot the tick loaded earlier, and the HTTP faces write
// worker rows without holding outsourceMu — so a change that landed meanwhile
// must survive the stamp, and a worker released meanwhile must not be written at
// all.
func TestStampWorkerPlacementBlocked_ReReadsTheRowBeforeWriting(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-gone")
	stale := blockedSpawnFixture(t, s, "t-0000000000e2", "ow-stale", "m-gone")

	// A relocate lands after the tick took its snapshot.
	moved := readWorker(t, s, "ow-stale")
	moved.DesiredMachineID = "m-moved"
	putWorkerFixture(t, s, moved)

	now := 4_000_000.0
	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(stale, now)
	s.outsourceMu.Unlock()

	got := readWorker(t, s, "ow-stale")
	// SENTINEL: the block really is stamped — the fixture reaches the write.
	if !strings.HasPrefix(got.LastOpReason, placementReasonUnavailable+":") ||
		got.LastOpAt != now {
		t.Fatalf("an offline pin must stamp a block at %v: %+v", now, got)
	}
	if got.DesiredMachineID != "m-moved" {
		t.Fatalf("the stamp must not clobber a pin that landed after the snapshot, got %q",
			got.DesiredMachineID)
	}

	// A worker RELEASED after the snapshot is left alone: there is nothing left
	// to explain, and writing the snapshot back would resurrect it as assigned.
	released := readWorker(t, s, "ow-stale")
	released.Status = WorkerStatusReleased
	released.LastOp, released.LastOpReason, released.LastOpAt = "", "", 0
	putWorkerFixture(t, s, released)

	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(stale, now+workerSpawnRetrySecs+1)
	s.outsourceMu.Unlock()

	after := readWorker(t, s, "ow-stale")
	if after.Status != WorkerStatusReleased || after.LastOpReason != "" || after.LastOpAt != 0 {
		t.Fatalf("a released worker must not be written back by the stamp: %+v", after)
	}
}

// TestNotifyWorkerSpawn_BlockRestampedAfterDispatch: the anti-churn guard
// suppresses REPETITION, never news. A block, a real dispatch, then the same
// block again must carry a FRESH last_op_at — without the clear on the dispatch
// path the second block matches the first and is silently swallowed, leaving the
// cockpit showing "stalled an hour ago" for a stall happening right now.
func TestNotifyWorkerSpawn_BlockRestampedAfterDispatch(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-flip")
	w := blockedSpawnFixture(t, s, "t-0000000000e1", "ow-flip", "m-flip")

	first := 3_000_000.0
	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, first)
	s.outsourceMu.Unlock()
	blocked := readWorker(t, s, "ow-flip")
	if !strings.HasPrefix(blocked.LastOpReason, placementReasonUnavailable+":") ||
		blocked.LastOpAt != first {
		t.Fatalf("an offline pin must stamp a block at %v: %+v", first, blocked)
	}

	// The machine comes online and the start actually lands.
	conn, err := s.hub.Connect("m-flip", "")
	if err != nil {
		t.Fatalf("hub connect m-flip: %v", err)
	}
	s.outsourceMu.Lock()
	dispatched := s.notifyWorkerSpawn(readWorker(t, s, "ow-flip"), first+workerSpawnRetrySecs+1)
	s.outsourceMu.Unlock()
	if !dispatched || len(s.hub.DrainWardenCommands("m-flip")) != 1 {
		t.Fatal("the pin coming online must dispatch the worker")
	}

	// It goes offline again: the SAME cause, but after a dispatch this is news.
	s.hub.Disconnect(conn)
	third := first + 10*workerSpawnRetrySecs
	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(readWorker(t, s, "ow-flip"), third)
	s.outsourceMu.Unlock()
	again := readWorker(t, s, "ow-flip")
	if again.LastOpReason != blocked.LastOpReason {
		t.Fatalf("the second block must carry the same cause: %q vs %q",
			again.LastOpReason, blocked.LastOpReason)
	}
	if again.LastOpAt != third {
		t.Fatalf("a block AFTER a dispatch must re-stamp: last_op_at = %v, want %v "+
			"(keeping the first block's %v means the dispatch never cleared it)",
			again.LastOpAt, third, blocked.LastOpAt)
	}
}

// A案 P5a: worker_stop rides the same fail-closed reachability gate as member
// dispatch — an offline target warden gets nothing in its FIFO.
func TestEnqueueWorkerStop_OfflineTarget_FailClosed(t *testing.T) {
	s := newWorkerTestServer(t)
	s.outsourceMu.Lock()
	accepted := s.enqueueWorkerStop(ServerSelfHost, "ow-1")
	s.outsourceMu.Unlock()
	if accepted {
		t.Error("worker_stop toward an offline warden must report not enqueued")
	}
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Errorf("nothing may land in a dead warden's FIFO, got %d frames", len(got))
	}

	connectWarden(t, s, ServerSelfHost)
	s.outsourceMu.Lock()
	accepted = s.enqueueWorkerStop(ServerSelfHost, "ow-1")
	s.outsourceMu.Unlock()
	if !accepted {
		t.Fatal("worker_stop toward an online warden must enqueue")
	}
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 worker_stop frame, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStop ||
		args["member_id"] != "ow-1" {
		t.Errorf("rpc = %q args = %v", rpc, args)
	}
}

// A案 P5a rework: an owner stop whose kill the gate refused (target warden
// unreachable) is PARKED and re-fired by the tick once the target reconnects —
// never silently lost (殘活 session 零容忍).
func TestStopWorkerNow_OfflineTarget_ParksKillAndTickRefires(t *testing.T) {
	s := newWorkerTestServer(t)
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-s", Codename: "O-1", Model: "opus", Effort: "high",
		TaskID: "t-s", Status: WorkerStatusAssigned,
		DesiredState: DesiredStateOffline, // owner-explicit stop intent
	})
	s.outsourceMu.Lock()
	s.workerSpawnTarget[w.ID] = ServerSelfHost // last session host, now offline
	s.stopWorkerNow(w)
	s.outsourceMu.Unlock()
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Fatalf("offline target must get nothing yet, got %d frames", len(got))
	}

	s.runOutsourceTick(nowSecs()) // target still offline — parked kill held
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Fatalf("tick with the target still offline must enqueue nothing, got %d", len(got))
	}

	connectWarden(t, s, ServerSelfHost)
	s.runOutsourceTick(nowSecs())
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want the parked worker_stop re-fired on reconnect, got %d frames", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStop ||
		args["member_id"] != "ow-s" {
		t.Errorf("rpc = %q args = %v", rpc, args)
	}
	s.outsourceMu.Lock()
	pending := s.workerStopPending[w.ID]
	s.outsourceMu.Unlock()
	if pending != "" {
		t.Errorf("parking must clear once the kill went out, still %q", pending)
	}
	// Once drained, later ticks owe nothing (one kill, no stop-spam).
	s.runOutsourceTick(nowSecs())
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Errorf("no further stops owed after the parked kill drained, got %d", len(got))
	}
}

// captureStderr runs fn with os.Stderr swapped onto a pipe and returns what it
// wrote — the outsourceLog sentinel assertions read it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(out)
}

// TestRespawnWorkerNow_SpawnMemoryEmpty_KillsViaSseMachineClaim: server-restart
// amnesia (workerSpawnTarget empty) must NOT skip the kill when the worker's
// live SSE machine claim still names the host — the stop dispatches there (the
// member relocation-STOP ground truth). Mutant: dropping the hub.MachineOf
// fallback in resolveWorkerKillTarget → no stop frame ever leaves and the old
// session survives the respawn (the O-28 double-active).
func TestRespawnWorkerNow_SpawnMemoryEmpty_KillsViaSseMachineClaim(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false)
	// The worker's live SSE carries the machine claim of the host it runs on.
	if _, err := api.hub.Connect(workerID, ServerSelfHost); err != nil {
		t.Fatalf("connect worker SSE: %v", err)
	}
	api.hub.DrainWardenCommands(ServerSelfHost)
	api.outsourceMu.Lock()
	delete(api.workerSpawnTarget, workerID) // server re-exec forgot the dispatch
	w, _ := api.dal.GetOutsourceWorker(workerID)
	done := api.respawnWorkerNow(*w, "auto-handover")
	api.outsourceMu.Unlock()
	if !done {
		t.Fatal("a resolvable SSE machine claim must let the respawn proceed")
	}
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 2 {
		t.Fatalf("want stop+start on the claimed machine, got %d frames", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStop ||
		args["member_id"] != workerID {
		t.Fatalf("first frame = %s %v, want stop %s", rpc, args, workerID)
	}
	if rpc, _ := decodeWardenFrame(t, frames[1].Frame); rpc != reconcileCmdStart {
		t.Fatalf("second frame = %s, want the respawn start", rpc)
	}
}

// TestRespawnWorkerNow_NoKillTarget_DefersWholeCycle: spawn memory empty AND no
// live SSE claim ⇒ the respawn must defer wholesale — no stop, no respawn, a
// greppable sentinel — and report false so the caller rolls its stamp back.
// Mutant: restoring the old `if old != "" { kill }` skip-and-respawn shape →
// a start frame appears with no preceding stop (red on the frame count).
func TestRespawnWorkerNow_NoKillTarget_DefersWholeCycle(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // no worker SSE at all
	api.hub.DrainWardenCommands(ServerSelfHost)
	var done bool
	logged := captureStderr(t, func() {
		api.outsourceMu.Lock()
		delete(api.workerSpawnTarget, workerID)
		w, _ := api.dal.GetOutsourceWorker(workerID)
		done = api.respawnWorkerNow(*w, "auto-handover")
		api.outsourceMu.Unlock()
	})
	if done {
		t.Fatal("no kill target must report the respawn as deferred")
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a deferred handover must dispatch nothing, got %d frames", got)
	}
	if !strings.Contains(logged, "auto-handover deferred "+workerID) ||
		!strings.Contains(logged, "no kill target (spawn memory empty, sse offline)") {
		t.Fatalf("sentinel log missing, got: %q", logged)
	}
	// …and the deferral is DIAGNOSABLE on the row, not log-only: a refused start
	// owes the cockpit a receipt, because the owner-explicit callers of this
	// primitive (relocate / restart / model change) discard the bool above and used
	// to leave the worker showing 尚未分配機器 with a blank last_op forever.
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if w.LastOp != reconcileCmdStart ||
		!strings.HasPrefix(w.LastOpReason, spawnReasonRespawnDeferred+":") {
		t.Fatalf("a deferred respawn must leave a receipt, got last_op=%q reason=%q",
			w.LastOp, w.LastOpReason)
	}
}

// ── the shared-FSM spawn/rescue path (A案 P6 — retired recoverStuckWorker) ──

// fsmWorkerFixture seeds a worker + a live bound task + a manual so the FSM's
// START decisions can actually dispatch (notifyWorkerSpawn refuses a worker
// with no live task).
func fsmWorkerFixture(t *testing.T, s *apiServer, id string, status string, createdTS float64) OutsourceWorker {
	t.Helper()
	putTaskFixture(t, s, Task{
		ID: "t-" + id, TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: id,
	})
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "review-pr", Purpose: "p",
		Fields: "[]", Assignee: `{"kind":"outsource","model":"opus"}`}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	return putWorkerFixture(t, s, OutsourceWorker{
		ID: id, Codename: "O-1", Model: "opus", Effort: "high",
		TaskID: "t-" + id, Status: status, CreatedTS: createdTS,
		DesiredMachineID: ServerSelfHost, // explicit placement (owner ruling 2026-07-25)
	})
}

// TestReconcileWorkerLiveness_ClobberedStartZombieTakeover: a START that
// bounced off the warden clobber-guard (receipt reason session_already_exists —
// the O-19 ghost wedge) makes the FSM dispatch a robust member `stop` to the
// last spawn target (reaping the ghost), bench that host (換機), clear the
// pacing, and NOT mark the worker reclaimed. A second tick inside stop_retry
// must not stop-spam.
func TestReconcileWorkerLiveness_ClobberedStartZombieTakeover(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 1_000_000.0
	w := fsmWorkerFixture(t, s, "ow-g", WorkerStatusAssigned, now-500)
	w.LastOp = reconcileCmdStart
	w.LastOpReason = "session_already_exists: live session refused clobber"
	putWorkerFixture(t, s, w)

	s.outsourceMu.Lock()
	s.workerSpawnAt["ow-g"] = now - 5 // recently paced (must not block the reap path)
	s.workerSpawnTarget["ow-g"] = ServerSelfHost
	s.workerReconcileStates["ow-g"] = reconcileState{
		Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: now - 10,
		// T-9adc: the takeover STOP now requires a SUSTAINED offline record —
		// this fixture is a long-confirmed ghost, past the second-confirm grace.
		OfflineSince: now - s.reconcileCfg.ZombieConfirmGrace - 1,
	}
	s.reconcileWorkerLiveness(w, now)
	_, stillPaced := s.workerSpawnAt["ow-g"]
	cooling := s.workerMachineCoolingOn("ow-g", ServerSelfHost, now)
	s.outsourceMu.Unlock()

	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("zombie takeover must enqueue exactly 1 stop, got %d", len(frames))
	}
	rpc, args := decodeWardenFrame(t, frames[0].Frame)
	if rpc != reconcileCmdStop || args["member_id"] != "ow-g" {
		t.Fatalf("takeover frame = %s %v, want stop ow-g", rpc, args)
	}
	if stillPaced {
		t.Fatal("the takeover must CLEAR the pacing stamp so the respawn is not throttled")
	}
	if !cooling {
		t.Fatal("the ghost's host must be benched for this worker (換機)")
	}
	if s.workerReclaimed["ow-g"] {
		t.Fatal("a rescue must NOT set workerReclaimed (that flag is for released-worker reclaim only)")
	}

	// The next tick moves to the RESPAWN leg (never a second stop toward the
	// same ghost), and with the only online warden benched the respawn honestly
	// waits — nothing is dispatched at the wedged host.
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, now+1)
	s.outsourceMu.Unlock()
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("the benched host must receive nothing after the takeover, got %d", got)
	}
}

// TestReconcileWorkerLiveness_InFlightStartAwaitsPresence (positive control):
// a start dispatched within start_timeout is a boot in flight — the FSM must
// leave it strictly alone (no re-start, no stop). Killing a healthy slow-booter
// would be the exact failure the old one-shot guard avoided; the FSM must too.
func TestReconcileWorkerLiveness_InFlightStartAwaitsPresence(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 1_000_000.0
	w := fsmWorkerFixture(t, s, "ow-f", WorkerStatusAssigned, now-10)
	s.outsourceMu.Lock()
	s.workerSpawnAt["ow-f"] = now - 5
	s.workerSpawnTarget["ow-f"] = ServerSelfHost
	s.workerReconcileStates["ow-f"] = reconcileState{
		Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: now - 5,
	}
	s.reconcileWorkerLiveness(w, now)
	_, stillPaced := s.workerSpawnAt["ow-f"]
	s.outsourceMu.Unlock()
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Fatalf("an in-flight boot must be left alone, got %d frames", len(got))
	}
	if !stillPaced {
		t.Fatal("an in-flight boot's pacing must be left intact")
	}
}

// TestReconcileWorkerLiveness_SilentTimeoutBacksOffThenRespawns: a start that
// times out silently (lost frame / dead boot, no receipt) folds into the FSM's
// backoff — no immediate re-dispatch — and once the backoff window lapses the
// next tick re-spawns fresh.
func TestReconcileWorkerLiveness_SilentTimeoutBacksOffThenRespawns(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 1_000_000.0
	w := fsmWorkerFixture(t, s, "ow-b", WorkerStatusAssigned, now-500)
	s.outsourceMu.Lock()
	s.workerReconcileStates["ow-b"] = reconcileState{
		Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
		LastCommandAt: now - (s.reconcileCfg.StartTimeout + 1),
	}
	s.reconcileWorkerLiveness(w, now) // timeout folds → backoff armed, nothing sent
	st := s.workerReconcileStates["ow-b"]
	s.outsourceMu.Unlock()
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a just-timed-out start must back off, not instantly re-dispatch (got %d)", got)
	}
	if st.Phase != reconcilePhaseBackoff || st.Attempts != 1 {
		t.Fatalf("state after silent timeout = %+v, want backoff/attempts=1", st)
	}

	// Past the backoff window the FSM re-spawns.
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, st.BackoffUntil+1)
	s.outsourceMu.Unlock()
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 start after the backoff lapsed, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != "ow-b" {
		t.Errorf("frame = %s %v", rpc, args)
	}
}

// TestReconcileWorkerLiveness_LegacyWorkerStartReceiptStillDetectsZombie: a
// clobber receipt folded by an OLD warden build still carries the retired
// worker_start verb — the transition fold (canonicalWorkerLastOp) must keep the
// zombie takeover working across the version skew.
func TestReconcileWorkerLiveness_LegacyWorkerStartReceiptStillDetectsZombie(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 1_000_000.0
	w := fsmWorkerFixture(t, s, "ow-l", WorkerStatusAssigned, now-500)
	w.LastOp = legacyWardenCmdWorkerStart
	w.LastOpReason = "session_already_exists: live session refused clobber"
	putWorkerFixture(t, s, w)
	s.outsourceMu.Lock()
	s.workerSpawnTarget["ow-l"] = ServerSelfHost
	s.workerReconcileStates["ow-l"] = reconcileState{
		Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: now - 10,
		// T-9adc: past the zombie second-confirm grace (sustained offline).
		OfflineSince: now - s.reconcileCfg.ZombieConfirmGrace - 1,
	}
	s.reconcileWorkerLiveness(w, now)
	s.outsourceMu.Unlock()
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("legacy-receipt zombie takeover must enqueue 1 stop, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStop ||
		args["member_id"] != "ow-l" {
		t.Errorf("frame = %s %v", rpc, args)
	}
}

func TestNotifyWorkerSpawn_TerminalTask_NoDispatch(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000003", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusTerminated, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-3", ClosedTS: 1,
	})
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-3", Codename: "O-3", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
	})
	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Errorf("a terminal task must never boot a worker, got %d frames", len(got))
	}
	// This arm repeats on every tick forever, so it must be diagnosable rather
	// than log-only — otherwise a permanently unbootable worker and a booting one
	// render identically on the cockpit.
	got, err := s.dal.GetOutsourceWorker("ow-3")
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if !strings.HasPrefix(got.LastOpReason, spawnReasonNoLiveTask+":") {
		t.Errorf("want a %s receipt, got last_op=%q reason=%q",
			spawnReasonNoLiveTask, got.LastOp, got.LastOpReason)
	}
}

// TestReconcileWorkerLiveness_WakeTimeoutLeavesDurableReceipt (T-e0e3 — the ACTUAL
// X-46 root cause): a START that is DISPATCHED and never produces a session was
// the one failure a worker row had NO durable record of. The member producer has
// stamped this same FSM signal since T-ba62 (stampWakeObservability arm (b));
// reconcileWorkerLiveness never read decision.StartTimedOut, and because worker
// spawn observability is in-memory by contract, a re-exec then erased the machine
// cell too — leaving a worker that had been dispatched to repeatedly showing
// 尚未分配機器 with every last_op field blank.
//
// Also pins the 31751ae lesson: the retry that FOLLOWS a wake timeout must not
// erase the explanation for the one before it.
func TestReconcileWorkerLiveness_WakeTimeoutLeavesDurableReceipt(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	w := fsmWorkerFixture(t, s, "ow-wt", WorkerStatusAssigned, 0)

	base := nowSecs()
	s.outsourceMu.Lock()
	// Tick 1 dispatches the START; the worker never connects.
	s.reconcileWorkerLiveness(w, base)
	s.outsourceMu.Unlock()
	if len(s.hub.DrainWardenCommands(ServerSelfHost)) != 1 {
		t.Fatal("precondition: the first tick must dispatch a START")
	}
	if got, _ := s.dal.GetOutsourceWorker("ow-wt"); got.LastOpReason != "" {
		t.Fatalf("a start in flight is not yet a failure: %q", got.LastOpReason)
	}

	// Tick 2, past the start window: the FSM calls it a silent timeout.
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base+WakingTTLSecs+1)
	s.outsourceMu.Unlock()

	got, err := s.dal.GetOutsourceWorker("ow-wt")
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if got.LastOp != reconcileCmdStart || got.LastOpOK == nil || *got.LastOpOK ||
		got.LastOpAt == 0 {
		t.Fatalf("a lapsed start must be a durable FAILED receipt, got last_op=%q ok=%v at=%v",
			got.LastOp, got.LastOpOK, got.LastOpAt)
	}
	if !strings.HasPrefix(got.LastOpReason, spawnReasonWakeTimeout+":") {
		t.Fatalf("want a %s receipt, got %q", spawnReasonWakeTimeout, got.LastOpReason)
	}
	// The receipt names the machine it was dispatched to and the runtime to check —
	// a bare "it failed" is not diagnosable.
	if !strings.Contains(got.LastOpReason, ServerSelfHost) {
		t.Errorf("the receipt must name the dispatch target: %q", got.LastOpReason)
	}

	// 31751ae: the NEXT re-dispatch must not blank the reason that explains the
	// previous failure — wake_timeout is not a placement block.
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(*got, base+WakingTTLSecs*10)
	s.outsourceMu.Unlock()
	after, _ := s.dal.GetOutsourceWorker("ow-wt")
	if !strings.HasPrefix(after.LastOpReason, spawnReasonWakeTimeout+":") {
		t.Fatalf("a retry must not erase the wake_timeout receipt, got %q", after.LastOpReason)
	}
}

// TestReconcileWorkerLiveness_NeverCollectedIsNotAFailedBoot (T-e0e3 round 3):
// "the frame was never picked up" and "it was picked up and the boot failed" have
// different culprits on different machines, so one receipt for both sends the
// owner to the wrong place. Enqueueing proves nothing about a reader —
// EnqueueWardenCommand is IsOnline + a map append, and notifyWorkerSpawn returns
// true on exactly that — so neither its bool nor a frame count can tell these
// apart. The hub's own FIFO depth can.
//
// The two subtests are a matched pair, the shape of the eva-m5 investigation that
// found this: the SAME worker, the SAME dispatch, differing ONLY in whether
// anything ever drained the queue.
func TestReconcileWorkerLiveness_NeverCollectedIsNotAFailedBoot(t *testing.T) {
	// NOBODY COLLECTS: the warden holds a stream (so placement resolves and the
	// enqueue succeeds) but never drains its FIFO — the X-46 shape.
	t.Run("uncollected frame is not blamed on the runtime", func(t *testing.T) {
		s := newWorkerTestServer(t)
		connectWarden(t, s, ServerSelfHost)
		w := fsmWorkerFixture(t, s, "ow-nc", WorkerStatusAssigned, 0)

		base := nowSecs()
		s.outsourceMu.Lock()
		s.reconcileWorkerLiveness(w, base) // dispatch — into the FIFO
		s.outsourceMu.Unlock()
		// Deliberately do NOT drain: no warden ever reads it.
		if got := s.hub.PendingWardenCommands(ServerSelfHost); got != 1 {
			t.Fatalf("precondition: the frame must be sitting in the FIFO, got %d", got)
		}

		s.outsourceMu.Lock()
		s.reconcileWorkerLiveness(w, base+WakingTTLSecs+1)
		s.outsourceMu.Unlock()

		got, err := s.dal.GetOutsourceWorker("ow-nc")
		if err != nil || got == nil {
			t.Fatalf("re-read worker: %v", err)
		}
		if !strings.HasPrefix(got.LastOpReason, spawnReasonNeverCollected+":") {
			t.Fatalf("an uncollected frame must say so, got %q", got.LastOpReason)
		}
		// The whole point: it must NOT send the owner to inspect the runtime on a
		// machine that was never even asked to start anything.
		if strings.Contains(got.LastOpReason, "runtime actually runs") {
			t.Errorf("must not blame the far machine's runtime: %q", got.LastOpReason)
		}
	})

	// SOMETHING COLLECTED IT: same dispatch, the queue is drained (a warden read
	// it), and the worker still never appears ⇒ a genuine boot failure.
	t.Run("collected frame that never boots is a wake timeout", func(t *testing.T) {
		s := newWorkerTestServer(t)
		connectWarden(t, s, ServerSelfHost)
		w := fsmWorkerFixture(t, s, "ow-wc", WorkerStatusAssigned, 0)

		base := nowSecs()
		s.outsourceMu.Lock()
		s.reconcileWorkerLiveness(w, base)
		s.outsourceMu.Unlock()
		if len(s.hub.DrainWardenCommands(ServerSelfHost)) != 1 { // a warden collects it
			t.Fatal("precondition: one frame must be collected")
		}
		if got := s.hub.PendingWardenCommands(ServerSelfHost); got != 0 {
			t.Fatalf("precondition: the FIFO must be empty after the drain, got %d", got)
		}

		s.outsourceMu.Lock()
		s.reconcileWorkerLiveness(w, base+WakingTTLSecs+1)
		s.outsourceMu.Unlock()

		got, err := s.dal.GetOutsourceWorker("ow-wc")
		if err != nil || got == nil {
			t.Fatalf("re-read worker: %v", err)
		}
		if !strings.HasPrefix(got.LastOpReason, spawnReasonWakeTimeout+":") {
			t.Fatalf("a collected-but-dead boot must be a wake timeout, got %q", got.LastOpReason)
		}
		if strings.HasPrefix(got.LastOpReason, spawnReasonNeverCollected+":") {
			t.Errorf("a collected frame must not claim it was never picked up")
		}
	})
}

// TestReconcileWorkerLiveness_DroppedStartIsNotBlamedOnTheRuntime (T-66a2 ×
// T-e0e3): the THIRD outcome, and the one the two arms above cannot see between
// them. A START that was drained off the FIFO and then lost — the warden's stream
// died mid-delivery — leaves the backlog EMPTY, exactly like a frame that was
// collected and booted badly. The at-most-once contract drops it (only `update`
// is ever requeued), so the only surviving evidence is the hub's loss note.
//
// Without an arm reading that note the receipt falls through to `default` and
// tells the owner, confidently, to go check the runtime on a machine that was
// never asked to start anything — the "points at a healthy machine" failure that
// c441f1a calls this change's only value. Before the rebase onto T-66a2 the blunt
// requeue put the frame back and the never_collected arm caught this case; the
// switch to at-most-once silently took that away.
//
// The discriminator is the RECEIPT, not the note: asserting the hub recorded a
// note would only re-test T-66a2's own plumbing and would stay green with the arm
// deleted.
func TestReconcileWorkerLiveness_DroppedStartIsNotBlamedOnTheRuntime(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	w := fsmWorkerFixture(t, s, "ow-drop", WorkerStatusAssigned, 0)

	base := nowSecs()
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base) // dispatch — into the FIFO
	s.outsourceMu.Unlock()

	// The warden's stream dies mid-drain: the frame is popped and never written,
	// which is precisely what the events handler hands back.
	captureStderr(t, func() {
		pending := s.hub.DrainWardenCommands(ServerSelfHost)
		if len(pending) != 1 {
			t.Fatalf("precondition: one frame must be on the FIFO, got %d", len(pending))
		}
		s.hub.ReturnUndeliveredCommands(ServerSelfHost, pending)
	})
	// The fixture's whole point: the backlog now looks IDENTICAL to a healthy
	// delivery, so nothing but the loss note can tell them apart.
	if got := s.hub.PendingWardenCommandsFor(ServerSelfHost, "ow-drop"); got != 0 {
		t.Fatalf("precondition: a dropped START must leave NO backlog, got %d", got)
	}

	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base+WakingTTLSecs+1)
	s.outsourceMu.Unlock()

	got, err := s.dal.GetOutsourceWorker("ow-drop")
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if strings.Contains(got.LastOpReason, "runtime actually runs") {
		t.Fatalf("a START that never reached the machine must NOT send the owner to "+
			"inspect the runtime there: %q", got.LastOpReason)
	}
	if !strings.HasPrefix(got.LastOpReason, spawnReasonNeverCollected+":") {
		t.Fatalf("a dropped START must read as never collected, got %q", got.LastOpReason)
	}
	// It must name the connection as the suspect, and the right machine.
	for _, want := range []string{"never reached machine", ServerSelfHost, "connection is the suspect"} {
		if !strings.Contains(got.LastOpReason, want) {
			t.Errorf("the receipt must contain %q so the owner knows where to look; got %q",
				want, got.LastOpReason)
		}
	}
}

// TestReconcileWorkerLiveness_OtherMembersBacklogIsNotOurs (T-e0e3 review C.1):
// one machine's command FIFO is shared by EVERY member and worker placed there,
// so its depth answers "does that machine owe anybody a frame" — never "is THIS
// worker's start frame still waiting". Reading the first as the second produced a
// confident, wrong accusation: a warden that had just demonstrably drained our
// frame got reported as not running, and the message explicitly steered the owner
// AWAY from the runtime, which is where the fault actually was.
//
// The fixture is exactly that ambiguity and nothing else: our frame IS collected,
// and a different member's frame is left queued on the SAME machine.
func TestReconcileWorkerLiveness_OtherMembersBacklogIsNotOurs(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	w := fsmWorkerFixture(t, s, "ow-shared", WorkerStatusAssigned, 0)

	base := nowSecs()
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base)
	s.outsourceMu.Unlock()
	if len(s.hub.DrainWardenCommands(ServerSelfHost)) != 1 {
		t.Fatal("precondition: our start frame must be collected")
	}
	// A COMPLETELY DIFFERENT member's frame arrives on the same machine and is
	// still waiting. That says nothing whatsoever about our worker.
	s.hub.EnqueueWardenCommandFor(ServerSelfHost, "m-somebody-else",
		[]byte("data: {\"topic\":\"warden-command\"}\n\n"))
	if got := s.hub.PendingWardenCommands(ServerSelfHost); got != 1 {
		t.Fatalf("precondition: the machine's queue must be non-empty, got %d", got)
	}
	if got := s.hub.PendingWardenCommandsFor(ServerSelfHost, "ow-shared"); got != 0 {
		t.Fatalf("precondition: none of that backlog is OURS, got %d", got)
	}

	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base+WakingTTLSecs+1)
	s.outsourceMu.Unlock()

	got, err := s.dal.GetOutsourceWorker("ow-shared")
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if strings.HasPrefix(got.LastOpReason, spawnReasonNeverCollected+":") {
		t.Fatalf("somebody else's queued frame must not be read as ours — this "+
			"receipt accuses a warden that collected our frame: %q", got.LastOpReason)
	}
	if !strings.HasPrefix(got.LastOpReason, spawnReasonWakeTimeout+":") {
		t.Fatalf("our frame WAS collected and no session appeared — that is a wake "+
			"timeout, got %q", got.LastOpReason)
	}
}

// TestReconcileWorkerLiveness_UnknownTargetNamesNoMachine (T-e0e3 review C.2):
// both timeout arms name a machine and then assert something about it. With no
// recorded target that becomes "collected by machine ”" — a confident claim made
// from zero evidence, sending the owner to a log on a host with no name. The
// guard says only what is known.
//
// Honest scope: the state is forced here (the spawn ledger is cleared directly).
// No production path is known that empties workerSpawnTarget while
// workerReconcileStates still holds a dispatched START — a re-exec clears both.
// The guard is kept because the COST of the wrong message is a wild goose chase
// and the cost of the guard is one branch, not because reachability is proven.
func TestReconcileWorkerLiveness_UnknownTargetNamesNoMachine(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	w := fsmWorkerFixture(t, s, "ow-notarget", WorkerStatusAssigned, 0)

	base := nowSecs()
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base)
	delete(s.workerSpawnTarget, w.ID) // forced: no record of where it went
	s.outsourceMu.Unlock()
	s.hub.DrainWardenCommands(ServerSelfHost)

	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base+WakingTTLSecs+1)
	s.outsourceMu.Unlock()

	got, err := s.dal.GetOutsourceWorker("ow-notarget")
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if got.LastOpReason == "" {
		t.Fatal("a timed-out start must still leave a receipt")
	}
	if strings.Contains(got.LastOpReason, "machine ''") ||
		strings.Contains(got.LastOpReason, "machine ‘’") {
		t.Fatalf("a receipt must not name — or make claims about — a machine it "+
			"cannot identify: %q", got.LastOpReason)
	}
	if strings.Contains(got.LastOpReason, "collected by") {
		t.Fatalf("with no known target nothing is known about collection: %q",
			got.LastOpReason)
	}
	if strings.HasPrefix(got.LastOpReason, spawnReasonNeverCollected+":") {
		t.Fatalf("PendingWardenCommandsFor('' , id) is 0 by construction — it must "+
			"not be read as evidence of collection either way: %q", got.LastOpReason)
	}
}

// TestNotifyWorkerSpawn_NoSigningSecret_LeavesReceipt: the fail-closed mint gate
// is one of the arms that used to abandon the spawn LOG-ONLY. Placement resolves
// fine here, so without a receipt the worker sits on a perfectly good machine pin
// with nothing at all explaining why it never booted.
func TestNotifyWorkerSpawn_NoSigningSecret_LeavesReceipt(t *testing.T) {
	s := newWorkerTestServer(t)
	s.secret = nil
	connectWarden(t, s, ServerSelfHost)
	seedMachine(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000009", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-9",
	})
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-9", Codename: "O-9", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned, DesiredMachineID: ServerSelfHost,
	})
	s.outsourceMu.Lock()
	dispatched := s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()
	if dispatched || len(s.hub.DrainWardenCommands(ServerSelfHost)) != 0 {
		t.Fatal("a server with no signing secret must never dispatch a worker start")
	}
	got, err := s.dal.GetOutsourceWorker("ow-9")
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if !strings.HasPrefix(got.LastOpReason, spawnReasonNoSecret+":") {
		t.Errorf("want a %s receipt, got last_op=%q reason=%q",
			spawnReasonNoSecret, got.LastOp, got.LastOpReason)
	}
}

// jwtSubOf verifies the minted token against the test secret and returns sub.
func jwtSubOf(t *testing.T, token string) string {
	t.Helper()
	claims, err := verifyJWT(token, []byte("worker-test-secret"), nowSecsInt())
	if err != nil {
		t.Fatalf("verify minted token: %v", err)
	}
	sub, _ := claims["sub"].(string)
	return sub
}

// jwtMachineOf verifies the minted token and returns its machine_id claim (""
// when the claim is absent) — the A案 P1 assertion that a worker token now
// carries its dispatch host, mirroring the member token.
func jwtMachineOf(t *testing.T, token string) string {
	t.Helper()
	claims, err := verifyJWT(token, []byte("worker-test-secret"), nowSecsInt())
	if err != nil {
		t.Fatalf("verify minted token: %v", err)
	}
	machineID, _ := claims["machine_id"].(string)
	return machineID
}

func nowSecsInt() int64 { return int64(nowSecs()) }

// ── reclaim ──────────────────────────────────────────────────────────────────

func TestReclaimWorkerSession_RecordedTarget(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-4", Codename: "O-4", Model: "opus", Effort: "high",
		TaskID: "t-x", Status: WorkerStatusReleased, ReleasedTS: 1,
	})
	s.outsourceMu.Lock()
	s.workerSpawnTarget["ow-4"] = ServerSelfHost
	s.reclaimWorkerSession(w)
	reclaimed := s.workerReclaimed["ow-4"]
	s.outsourceMu.Unlock()
	if !reclaimed {
		t.Error("reclaim must mark the worker once a frame went out")
	}
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 worker_stop, got %d", len(frames))
	}
	rpc, args := decodeWardenFrame(t, frames[0].Frame)
	if rpc != reconcileCmdStop || args["member_id"] != "ow-4" {
		t.Errorf("frame = %s %v, want worker_stop ow-4", rpc, args)
	}
}

func TestReclaimWorkerSession_NoTargetBroadcastsToOnlineWardens(t *testing.T) {
	s := newWorkerTestServer(t)
	if err := s.dal.PutMember(Member{
		ID: "m-other", Name: "other box", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put member: %v", err)
	}
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-5", Codename: "O-5", Model: "opus", Effort: "high",
		TaskID: "t-x", Status: WorkerStatusReleased, ReleasedTS: 1,
	})
	s.outsourceMu.Lock()
	s.reclaimWorkerSession(w) // no recorded target (restart amnesia)
	s.outsourceMu.Unlock()
	for _, warden := range []string{ServerSelfHost, "m-other"} {
		if got := len(s.hub.DrainWardenCommands(warden)); got != 1 {
			t.Errorf("warden %s: want 1 broadcast worker_stop, got %d", warden, got)
		}
	}
}

func TestReclaimWorkerSession_NoOnlineWarden_RetriesLater(t *testing.T) {
	s := newWorkerTestServer(t)
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-6", Codename: "O-6", Model: "opus", Effort: "high",
		TaskID: "t-x", Status: WorkerStatusReleased, ReleasedTS: 1,
	})
	s.outsourceMu.Lock()
	s.reclaimWorkerSession(w)
	reclaimed := s.workerReclaimed["ow-6"]
	s.outsourceMu.Unlock()
	if reclaimed {
		t.Error("an undeliverable reclaim must NOT be marked done (backstop retries)")
	}
}

func TestDismissOutsourceWorkersForTask_ReleasesAndReclaims(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000007", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusDone, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-7", ClosedTS: 1,
	})
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-7", Codename: "O-7", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusActive,
	})

	s.dismissOutsourceWorkersForTask(task.ID, 42.0, triggerServer)

	after, err := s.dal.GetOutsourceWorker("ow-7")
	if err != nil || after == nil {
		t.Fatalf("read back worker: %v", err)
	}
	if after.Status != WorkerStatusReleased || after.ReleasedTS != 42.0 {
		t.Errorf("worker after dismiss = %+v, want released@42", after)
	}
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 worker_stop, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStop ||
		args["member_id"] != "ow-7" {
		t.Errorf("frame = %s %v", rpc, args)
	}

	// Idempotent: a second dismissal (double报) enqueues nothing further.
	s.dismissOutsourceWorkersForTask(task.ID, 43.0, triggerServer)
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("second dismissal must be a no-op, got %d frames", got)
	}
}

// ── close-out report → immediate dismissal (the wired §6.3 step-2 hook) ─────

// postCloseout drives the real close-out handler as caller sub (agent scope).
func postCloseout(t *testing.T, s *apiServer, taskID, sub string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := taskReq(t, http.MethodPost, "/api/tasks/"+taskID+"/closeout", nil,
		sub, "agent")
	s.HandleReportTaskCloseoutApiTasksTaskIdCloseoutPost(rec, req, taskID)
	return rec
}

func TestCloseoutReport_DismissesWorkerImmediately(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000000b", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusDone, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-b", ClosedTS: 1,
	})
	// The worker is still ACTIVE here (closeTask normally releases it, but the
	// hook must be robust to a lingering row) — the close-out must both
	// release it AND reclaim its session at once, NOT after the grace.
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-b", Codename: "O-11", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusActive,
	})

	if rec := postCloseout(t, s, task.ID, "ow-b"); rec.Code != http.StatusOK {
		t.Fatalf("closeout report: %d %s", rec.Code, rec.Body.String())
	}

	after, err := s.dal.GetOutsourceWorker("ow-b")
	if err != nil || after == nil {
		t.Fatalf("read back worker: %v", err)
	}
	if after.Status != WorkerStatusReleased {
		t.Errorf("worker after close-out = %q, want released", after.Status)
	}
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 immediate worker_stop, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStop ||
		args["member_id"] != "ow-b" {
		t.Errorf("frame = %s %v, want worker_stop ow-b", rpc, args)
	}
}

func TestCloseoutReport_RepeatIsNoOp_NoSecondDispatch(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000000c", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusDone, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-c", ClosedTS: 1,
	})
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-c", Codename: "O-12", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusReleased, ReleasedTS: 1,
	})

	if rec := postCloseout(t, s, task.ID, "ow-c"); rec.Code != http.StatusOK {
		t.Fatalf("first closeout: %d %s", rec.Code, rec.Body.String())
	}
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 1 {
		t.Fatalf("first closeout: want 1 worker_stop, got %d", got)
	}
	if rec := postCloseout(t, s, task.ID, "ow-c"); rec.Code != http.StatusOK {
		t.Fatalf("repeat closeout: %d %s", rec.Code, rec.Body.String())
	}
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("repeat closeout must dispatch nothing, got %d frames", got)
	}
}

func TestCloseoutReport_MemberTask_NoDismissal(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000000d", Title: "ad-hoc thing",
		Status: TaskStatusDone, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorMember, ExecutorID: "mira", ClosedTS: 1,
	})
	// An UNRELATED live worker on another task must be untouched by this
	// member close-out (the dismissal is task-scoped).
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-d", Codename: "O-13", Model: "opus", Effort: "high",
		TaskID: "t-something-else", Status: WorkerStatusActive,
	})

	if rec := postCloseout(t, s, task.ID, "mira"); rec.Code != http.StatusOK {
		t.Fatalf("member closeout: %d %s", rec.Code, rec.Body.String())
	}
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("member-task closeout must dispatch no worker_stop, got %d", got)
	}
	after, err := s.dal.GetOutsourceWorker("ow-d")
	if err != nil || after == nil || after.Status != WorkerStatusActive {
		t.Errorf("unrelated worker must stay active, got %+v (err %v)", after, err)
	}
}

// ── the scheduler tick's lifecycle passes ────────────────────────────────────

func TestTick_ReclaimBackstop_GraceRespected(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := nowSecs()
	putTaskFixture(t, s, Task{
		ID: "t-000000000008", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusDone, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-8", ClosedTS: now,
	})
	// Released WITHIN the grace → left alone; released BEYOND it → reclaimed.
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-8", Codename: "O-8", Model: "opus", Effort: "high",
		TaskID: "t-000000000008", Status: WorkerStatusReleased, ReleasedTS: now - 5,
	})
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-9", Codename: "O-9", Model: "opus", Effort: "high",
		TaskID: "t-000000000008", Status: WorkerStatusReleased,
		ReleasedTS: now - workerReclaimGraceSecs - 5,
	})

	s.runOutsourceTick(now)

	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want exactly 1 backstop worker_stop, got %d", len(frames))
	}
	if _, args := decodeWardenFrame(t, frames[0].Frame); args["member_id"] != "ow-9" {
		t.Errorf("backstop reclaimed %v, want ow-9", args["member_id"])
	}
}

func TestTick_AssignedWorker_RedispatchesSpawn(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000000a", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-a",
	})
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-a", Codename: "O-10", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
		DesiredMachineID: ServerSelfHost, // explicit placement (owner ruling 2026-07-25)
	})

	s.runOutsourceTick(nowSecs())

	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 worker_start from the tick pass, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != "ow-a" {
		t.Errorf("frame = %s %v", rpc, args)
	}
}
