package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubOps wraps the real filesystem ops (so rename/atomic-swap behaviour is
// exercised for real against a t.TempDir()) but makes the exec probe programmable:
// tests decide whether a "downloaded" binary passes the verify-before-swap gate.
type stubOps struct {
	osUpdaterOps
	probeErr error
	probed   *[]string
}

func (s stubOps) probe(bin string) error {
	if s.probed != nil {
		*s.probed = append(*s.probed, bin)
	}
	return s.probeErr
}

// getResult is one canned GET response keyed by path.
type getResult struct {
	status int
	body   []byte
	err    error
}

// recordingGetter serves canned responses and records which paths were fetched (so
// a test can assert the download GATE actually suppressed a binary download).
func recordingGetter(m map[string]getResult, calls *[]string) getter {
	return func(path string) (int, []byte, error) {
		if calls != nil {
			*calls = append(*calls, path)
		}
		r, ok := m[path]
		if !ok {
			return 404, nil, nil
		}
		return r.status, r.body, r.err
	}
}

func versionBody(sha string) []byte {
	return []byte(`{"version":"0.0.0","git_sha":"` + sha + `","update_available":false,"latest_version":null}`)
}

func newTestUpdater(ops updaterOps, get getter, selfPath, agentPath string) *updater {
	return &updater{
		get:          get,
		ops:          ops,
		selfPath:     selfPath,
		agentPath:    agentPath,
		interval:     time.Millisecond,
		backoffStart: time.Millisecond,
		backoffCap:   time.Millisecond,
		sleep:        sleepUntil,
		exit:         func(int) {},
		logf:         func(string, ...any) {},
	}
}

func writeLive(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("seed live binary: %v", err)
	}
}

func readFileStr(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// ── selfUpdateAgentPath — the SELF-UPDATE ocagent path resolver ──────────────

// UNCONDITIONAL HOME SIBLING: a home-installed warden whose ocagent sibling does NOT
// yet exist must still resolve to <dir(exe)>/ocagent — NOT the in-tree source dir —
// so the first tick can download+populate it there (the remote-install root cause).
func TestSelfUpdateAgentPath_HomeSiblingEvenWhenMissing(t *testing.T) {
	exe := func() (string, error) { return "/home/u/.officraft/warden/ocwarden", nil }
	got := selfUpdateAgentPath(exe)
	want := "/home/u/.officraft/warden/ocagent"
	if got != want {
		t.Fatalf("selfUpdateAgentPath = %q, want %q (must be the home sibling, never the source dir)", got, want)
	}
}

// UNRESOLVABLE EXE: if the OS cannot name our own binary, the path is "" so checkOnce
// skips ocagent reconcile rather than writing to a bogus location.
func TestSelfUpdateAgentPath_EmptyWhenExeUnresolvable(t *testing.T) {
	exe := func() (string, error) { return "", errors.New("cannot resolve executable") }
	if got := selfUpdateAgentPath(exe); got != "" {
		t.Fatalf("selfUpdateAgentPath = %q, want empty on unresolvable executable", got)
	}
}

// ① VERIFY-BEFORE-SWAP: a download that fails the health probe must NOT swap; the
// live binary is retained byte-for-byte and no .prev backup is left behind.
func TestReconcileBinary_VerifyFailsNoSwap(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "ocwarden")
	writeLive(t, live, "OLD-GOOD-BINARY")

	var probed []string
	ops := stubOps{probeErr: errors.New("exec format error"), probed: &probed}
	get := recordingGetter(map[string]getResult{
		wardenBinaryPath: {status: 200, body: []byte("NEW-CORRUPT-BINARY")},
	}, nil)
	u := newTestUpdater(ops, get, live, "")

	swapped, err := u.reconcileBinary(wardenBinaryPath, live, "ocwarden")
	if err == nil {
		t.Fatal("expected an error when the probe fails")
	}
	if swapped {
		t.Fatal("must NOT report a swap when verify fails")
	}
	if got := readFileStr(t, live); got != "OLD-GOOD-BINARY" {
		t.Fatalf("live binary must be untouched; got %q", got)
	}
	if _, statErr := os.Stat(live + ".prev"); !os.IsNotExist(statErr) {
		t.Fatal("no .prev backup should exist when the swap never happened")
	}
	// temp must have been cleaned up.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected only the live binary in dir, got %d entries", len(entries))
	}
	if len(probed) != 1 {
		t.Fatalf("probe should have run exactly once, ran %d times", len(probed))
	}
}

// ②③ SUCCESS PATH + RETREAT: a verified, different download swaps atomically and
// leaves the previous binary at <path>.prev so a rollback is possible.
func TestReconcileBinary_SuccessSwapAndBackup(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "ocwarden")
	writeLive(t, live, "OLD-BINARY")

	ops := stubOps{probeErr: nil}
	get := recordingGetter(map[string]getResult{
		wardenBinaryPath: {status: 200, body: []byte("NEW-BINARY")},
	}, nil)
	u := newTestUpdater(ops, get, live, "")

	swapped, err := u.reconcileBinary(wardenBinaryPath, live, "ocwarden")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !swapped {
		t.Fatal("expected a swap for a verified, differing binary")
	}
	if got := readFileStr(t, live); got != "NEW-BINARY" {
		t.Fatalf("live binary should now be the new bytes; got %q", got)
	}
	if got := readFileStr(t, live+".prev"); got != "OLD-BINARY" {
		t.Fatalf("retreat path .prev should hold the old bytes; got %q", got)
	}
	// no dangling temp file (dir should hold exactly: ocwarden + ocwarden.prev).
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("expected live + .prev only, got %d entries", len(entries))
	}
}

// POPULATE PATH: when the live binary does NOT exist yet but its dir does (a fresh
// remote/manual install whose ocagent sibling was never copied), a verified download
// must be written into place — with no .prev backup, since there was no prior binary.
func TestReconcileBinary_PopulatesWhenLiveMissing(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "ocagent") // deliberately NOT created

	ops := stubOps{probeErr: nil}
	get := recordingGetter(map[string]getResult{
		agentBinaryPath: {status: 200, body: []byte("FRESH-AGENT")},
	}, nil)
	u := newTestUpdater(ops, get, "", live)

	swapped, err := u.reconcileBinary(agentBinaryPath, live, "ocagent")
	if err != nil {
		t.Fatalf("unexpected error populating a missing live binary: %v", err)
	}
	if !swapped {
		t.Fatal("expected a swap that populates the absent live binary")
	}
	if got := readFileStr(t, live); got != "FRESH-AGENT" {
		t.Fatalf("live binary should now hold the downloaded bytes; got %q", got)
	}
	if _, statErr := os.Stat(live + ".prev"); !os.IsNotExist(statErr) {
		t.Fatal("no .prev backup should exist when there was no prior binary")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected only the populated binary in dir, got %d entries", len(entries))
	}
}

// IDEMPOTENT: identical content ⇒ no swap and the probe is never even run (content
// hash is the oracle; this is what makes the loop drift/restart-proof).
func TestReconcileBinary_IdenticalNoSwap(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "ocwarden")
	writeLive(t, live, "SAME-BINARY")

	var probed []string
	ops := stubOps{probed: &probed}
	get := recordingGetter(map[string]getResult{
		wardenBinaryPath: {status: 200, body: []byte("SAME-BINARY")},
	}, nil)
	u := newTestUpdater(ops, get, live, "")

	swapped, err := u.reconcileBinary(wardenBinaryPath, live, "ocwarden")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if swapped {
		t.Fatal("identical content must not swap")
	}
	if len(probed) != 0 {
		t.Fatal("probe must NOT run on the identical-content fast path")
	}
	if _, statErr := os.Stat(live + ".prev"); !os.IsNotExist(statErr) {
		t.Fatal("no .prev backup for a no-op reconcile")
	}
}

// ④ VERSION COMPARE — SAME: when the server git_sha matches our last reconcile, the
// cheap gate must short-circuit BEFORE any binary is downloaded.
func TestCheckOnce_SameSHANoDownload(t *testing.T) {
	var calls []string
	get := recordingGetter(map[string]getResult{
		versionPath: {status: 200, body: versionBody("abc123")},
	}, &calls)
	u := newTestUpdater(stubOps{}, get, "", "")
	u.lastSHA = "abc123"

	swapped, err := u.checkOnce()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if swapped {
		t.Fatal("no swap expected when the server has not moved")
	}
	for _, c := range calls {
		if c == wardenBinaryPath || c == agentBinaryPath {
			t.Fatalf("binary download %q must be gated out when sha is unchanged", c)
		}
	}
}

// ④ VERSION COMPARE — NEWER: a moved server sha triggers reconcile; a differing
// ocwarden download swaps and flips the self-exit bool, and lastSHA advances.
func TestCheckOnce_NewerTriggersWardenSwap(t *testing.T) {
	dir := t.TempDir()
	warden := filepath.Join(dir, "ocwarden")
	agent := filepath.Join(dir, "ocagent")
	writeLive(t, warden, "OLD-WARDEN")
	writeLive(t, agent, "SAME-AGENT") // agent unchanged → no agent swap

	get := recordingGetter(map[string]getResult{
		versionPath:      {status: 200, body: versionBody("newsha")},
		wardenBinaryPath: {status: 200, body: []byte("NEW-WARDEN")},
		agentBinaryPath:  {status: 200, body: []byte("SAME-AGENT")},
	}, nil)
	u := newTestUpdater(stubOps{}, get, warden, agent)
	u.lastSHA = "oldsha"

	swapped, err := u.checkOnce()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !swapped {
		t.Fatal("ocwarden differs ⇒ checkOnce must report a warden swap (self-exit trigger)")
	}
	if got := readFileStr(t, warden); got != "NEW-WARDEN" {
		t.Fatalf("ocwarden should be updated; got %q", got)
	}
	if got := readFileStr(t, agent); got != "SAME-AGENT" {
		t.Fatalf("ocagent was identical and must be untouched; got %q", got)
	}
	if u.lastSHA != "newsha" {
		t.Fatalf("lastSHA should advance to the reconciled server sha; got %q", u.lastSHA)
	}
}

// ocwarden vs ocagent DIFFERENCE: an ocagent-only change updates ocagent but must
// NOT flip the self-exit bool (only ocwarden replacement warrants a relaunch).
func TestCheckOnce_AgentOnlyNoSelfExit(t *testing.T) {
	dir := t.TempDir()
	warden := filepath.Join(dir, "ocwarden")
	agent := filepath.Join(dir, "ocagent")
	writeLive(t, warden, "SAME-WARDEN")
	writeLive(t, agent, "OLD-AGENT")

	get := recordingGetter(map[string]getResult{
		versionPath:      {status: 200, body: versionBody("newsha")},
		wardenBinaryPath: {status: 200, body: []byte("SAME-WARDEN")},
		agentBinaryPath:  {status: 200, body: []byte("NEW-AGENT")},
	}, nil)
	u := newTestUpdater(stubOps{}, get, warden, agent)

	swapped, err := u.checkOnce()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if swapped {
		t.Fatal("an ocagent-only swap must NOT trigger the ocwarden self-exit")
	}
	if got := readFileStr(t, agent); got != "NEW-AGENT" {
		t.Fatalf("ocagent should be updated; got %q", got)
	}
	if got := readFileStr(t, warden); got != "SAME-WARDEN" {
		t.Fatalf("ocwarden was identical and must be untouched; got %q", got)
	}
}

// MID-CYCLE FAILURE: if the ocagent download fails, checkOnce returns an error and
// must NOT advance lastSHA (so the next cycle retries rather than silently skips).
func TestCheckOnce_MidCycleFailureKeepsSHA(t *testing.T) {
	dir := t.TempDir()
	agent := filepath.Join(dir, "ocagent")
	writeLive(t, agent, "OLD-AGENT")

	get := recordingGetter(map[string]getResult{
		versionPath:     {status: 200, body: versionBody("newsha")},
		agentBinaryPath: {status: 500, body: nil},
	}, nil)
	u := newTestUpdater(stubOps{}, get, "", agent)
	u.lastSHA = "oldsha"

	if _, err := u.checkOnce(); err == nil {
		t.Fatal("expected an error when the ocagent download fails")
	}
	if u.lastSHA != "oldsha" {
		t.Fatalf("lastSHA must NOT advance on a mid-cycle failure; got %q", u.lastSHA)
	}
}

// EXEC-IN-PLACE: after a cycle that replaced ocwarden, run() must invoke the
// execSelf seam exactly once, with the new bytes already on disk. (A real
// syscall.Exec never returns on success; the fake returns, so run() then walks
// the failure fallback — that path is asserted by the fallback test below.)
func TestRun_ExecsSelfAfterWardenSwap(t *testing.T) {
	dir := t.TempDir()
	warden := filepath.Join(dir, "ocwarden")
	writeLive(t, warden, "OLD-WARDEN")

	get := recordingGetter(map[string]getResult{
		versionPath:      {status: 200, body: versionBody("newsha")},
		wardenBinaryPath: {status: 200, body: []byte("NEW-WARDEN")},
	}, nil)
	u := newTestUpdater(stubOps{}, get, warden, "")

	execCalls := 0
	u.execSelf = func() error { execCalls++; return nil }
	// proceed immediately, every tick.
	u.sleep = func(context.Context, time.Duration) bool { return true }

	u.run(context.Background())

	if execCalls != 1 {
		t.Fatalf("execSelf seam should fire exactly once after a warden swap, fired %d times", execCalls)
	}
	if got := readFileStr(t, warden); got != "NEW-WARDEN" {
		t.Fatalf("the swap must be on disk before the exec; got %q", got)
	}
}

// EXEC FALLBACK: a failing execSelf (syscall.Exec only returns on failure) must fall
// back to exit(0) — the pre-fix behaviour — never leave the loop running on the old
// binary or crash.
func TestRun_ExecFailureFallsBackToExit(t *testing.T) {
	dir := t.TempDir()
	warden := filepath.Join(dir, "ocwarden")
	writeLive(t, warden, "OLD-WARDEN")

	get := recordingGetter(map[string]getResult{
		versionPath:      {status: 200, body: versionBody("newsha")},
		wardenBinaryPath: {status: 200, body: []byte("NEW-WARDEN")},
	}, nil)
	u := newTestUpdater(stubOps{}, get, warden, "")

	u.execSelf = func() error { return errors.New("execve: permission denied") }
	exitCode := -1
	exitCalls := 0
	u.exit = func(c int) { exitCode = c; exitCalls++ }
	u.sleep = func(context.Context, time.Duration) bool { return true }

	u.run(context.Background())

	if exitCalls != 1 {
		t.Fatalf("exit fallback should fire exactly once when exec fails, fired %d times", exitCalls)
	}
	if exitCode != 0 {
		t.Fatalf("expected fallback exit(0), got exit(%d)", exitCode)
	}
}

// ── self-update OBSERVABILITY announce (best-effort telemetry POST) ──────────

// recordingPoster captures each POST (path + payload) and returns a canned status.
type recordingPoster struct {
	status int
	calls  *[]struct {
		path    string
		payload map[string]any
	}
}

func (r recordingPoster) post(path string, payload map[string]any) (int, map[string]any) {
	if r.calls != nil {
		*r.calls = append(*r.calls, struct {
			path    string
			payload map[string]any
		}{path, payload})
	}
	return r.status, nil
}

// HASH CAPTURE: a successful swap records the old->new content-hash prefixes and the
// binary name on the updater, so run() can announce them.
func TestReconcileBinary_CapturesSelfUpdateEvent(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "ocwarden")
	writeLive(t, live, "OLD-BINARY")

	ops := stubOps{probeErr: nil}
	get := recordingGetter(map[string]getResult{
		wardenBinaryPath: {status: 200, body: []byte("NEW-BINARY")},
	}, nil)
	u := newTestUpdater(ops, get, live, "")
	u.now = func() time.Time { return time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC) }

	if _, err := u.reconcileBinary(wardenBinaryPath, live, "ocwarden"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.lastSwap == nil {
		t.Fatal("a successful swap must capture a selfUpdateEvent")
	}
	if u.lastSwap.Binary != "ocwarden" {
		t.Fatalf("binary = %q, want ocwarden", u.lastSwap.Binary)
	}
	if u.lastSwap.OldHash != hashPrefix([]byte("OLD-BINARY")) {
		t.Fatalf("old_hash = %q, want prefix of OLD-BINARY", u.lastSwap.OldHash)
	}
	if u.lastSwap.NewHash != hashPrefix([]byte("NEW-BINARY")) {
		t.Fatalf("new_hash = %q, want prefix of NEW-BINARY", u.lastSwap.NewHash)
	}
	if u.lastSwap.At != "2026-07-08T12:00:00Z" {
		t.Fatalf("at = %q, want the injected UTC RFC3339 stamp", u.lastSwap.At)
	}
	if len(u.lastSwap.OldHash) != selfUpdateHashPrefixLen {
		t.Fatalf("hash prefix len = %d, want %d", len(u.lastSwap.OldHash), selfUpdateHashPrefixLen)
	}
}

// ANNOUNCE PAYLOAD: after a warden swap, run() POSTs exactly one telemetry report to
// the telemetry endpoint carrying the self_update event, then exits.
func TestRun_AnnouncesSelfUpdateBeforeExit(t *testing.T) {
	dir := t.TempDir()
	warden := filepath.Join(dir, "ocwarden")
	writeLive(t, warden, "OLD-WARDEN")

	get := recordingGetter(map[string]getResult{
		versionPath:      {status: 200, body: versionBody("newsha")},
		wardenBinaryPath: {status: 200, body: []byte("NEW-WARDEN")},
	}, nil)
	u := newTestUpdater(stubOps{}, get, warden, "")
	u.now = func() time.Time { return time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC) }
	u.agentID = "mira-1"

	var calls []struct {
		path    string
		payload map[string]any
	}
	u.post = recordingPoster{status: 200, calls: &calls}.post

	exitCalls := 0
	u.exit = func(int) { exitCalls++ }
	u.sleep = func(context.Context, time.Duration) bool { return true }

	u.run(context.Background())

	if exitCalls != 1 {
		t.Fatalf("exit should fire once after a warden swap, fired %d", exitCalls)
	}
	if len(calls) != 1 {
		t.Fatalf("expected exactly one announce POST, got %d", len(calls))
	}
	if calls[0].path != selfUpdateReportPath {
		t.Fatalf("announce path = %q, want %q", calls[0].path, selfUpdateReportPath)
	}
	if calls[0].payload["agent_id"] != "mira-1" {
		t.Fatalf("announce agent_id = %v, want mira-1", calls[0].payload["agent_id"])
	}
	su, ok := calls[0].payload["self_update"].(map[string]any)
	if !ok {
		t.Fatalf("announce payload missing self_update object: %#v", calls[0].payload)
	}
	if su["binary"] != "ocwarden" {
		t.Fatalf("self_update.binary = %v, want ocwarden", su["binary"])
	}
	if su["old_hash"] != hashPrefix([]byte("OLD-WARDEN")) {
		t.Fatalf("self_update.old_hash = %v", su["old_hash"])
	}
	if su["new_hash"] != hashPrefix([]byte("NEW-WARDEN")) {
		t.Fatalf("self_update.new_hash = %v", su["new_hash"])
	}
	if su["at"] != "2026-07-08T12:00:00Z" {
		t.Fatalf("self_update.at = %v", su["at"])
	}
}

// BEST-EFFORT: a failing announce (non-2xx status here; a transport error status 0 is
// the same code path) must NEVER block the swap's self-exit — the swap already applied.
func TestRun_AnnounceFailureDoesNotBlockExit(t *testing.T) {
	dir := t.TempDir()
	warden := filepath.Join(dir, "ocwarden")
	writeLive(t, warden, "OLD-WARDEN")

	get := recordingGetter(map[string]getResult{
		versionPath:      {status: 200, body: versionBody("newsha")},
		wardenBinaryPath: {status: 200, body: []byte("NEW-WARDEN")},
	}, nil)
	u := newTestUpdater(stubOps{}, get, warden, "")
	u.agentID = "mira-1"
	u.post = func(string, map[string]any) (int, map[string]any) { return 500, nil }

	exitCode := -1
	exitCalls := 0
	u.exit = func(c int) { exitCode = c; exitCalls++ }
	u.sleep = func(context.Context, time.Duration) bool { return true }

	u.run(context.Background())

	if exitCalls != 1 || exitCode != 0 {
		t.Fatalf("swap must still exit(0) despite a failed announce; calls=%d code=%d", exitCalls, exitCode)
	}
	if got := readFileStr(t, warden); got != "NEW-WARDEN" {
		t.Fatalf("swap must have applied regardless of announce outcome; got %q", got)
	}
}

// NO-OP GUARD: no captured swap ⇒ no announce POST (an ordinary no-swap cycle stays
// silent; only a real swap is announced).
func TestAnnounceSelfUpdate_NoopWhenNothingSwapped(t *testing.T) {
	u := newTestUpdater(stubOps{}, recordingGetter(nil, nil), "", "")
	u.agentID = "mira-1"
	posted := 0
	u.post = func(string, map[string]any) (int, map[string]any) { posted++; return 200, nil }

	u.announceSelfUpdate() // lastSwap is nil

	if posted != 0 {
		t.Fatalf("announce must not POST when nothing swapped; posted %d", posted)
	}
}

// CLEAN SHUTDOWN: a cancelled ctx ends run() without calling exit.
func TestRun_CleanShutdownOnCancel(t *testing.T) {
	get := recordingGetter(map[string]getResult{}, nil)
	u := newTestUpdater(stubOps{}, get, "", "")
	exitCalls := 0
	u.exit = func(int) { exitCalls++ }

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled → sleepUntil returns false immediately
	u.run(ctx)

	if exitCalls != 0 {
		t.Fatalf("exit must not fire on a clean cancel; fired %d", exitCalls)
	}
}

// ── 方案A event-driven kick seam (T-c93d) ───────────────────────────────────
// The self-update loop's poll wait can be cut short by a kick (SSE reconnect
// today, T-5f01's `update` rpc tomorrow). The 15m timer remains the backstop.

// COALESCE: many kicks between cycles must collapse to a SINGLE pending wake and
// never block the caller (a reconnect storm must not stack N immediate cycles).
func TestKick_CoalescesAndNeverBlocks(t *testing.T) {
	u := &updater{kick: make(chan struct{}, 1)}
	for i := 0; i < 100; i++ {
		u.Kick() // must never block, regardless of how many pile up
	}
	if got := len(u.kick); got != 1 {
		t.Fatalf("kick must coalesce to one pending wake (de-bounce, no stack); len=%d", got)
	}
}

// NIL-SAFE: an updater with no kick channel (unwired / --once) must ignore Kick.
func TestKick_NilChannelIsNoOp(t *testing.T) {
	u := &updater{} // kick == nil
	u.Kick()        // must not panic
}

// TIMER BACKSTOP: with no kick, waitNext proceeds when the (seam) timer elapses.
func TestWaitNext_TimerElapsesProceeds(t *testing.T) {
	u := &updater{sleep: func(context.Context, time.Duration) bool { return true }}
	if !u.waitNext(context.Background(), time.Millisecond) {
		t.Fatal("timer elapse should proceed (true)")
	}
}

// CTX CANCEL: a cancelled ctx must stop the loop (false), never proceed.
func TestWaitNext_CtxCancelledStops(t *testing.T) {
	u := &updater{sleep: sleepUntil}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if u.waitNext(ctx, time.Hour) {
		t.Fatal("cancelled ctx must stop the loop (false)")
	}
}

// KICK CUTS A LONG WAIT: a kick makes waitNext return true well before the (here
// 1h) timer — proving the fast path is the kick, not the backstop timer.
func TestWaitNext_KickCutsLongWait(t *testing.T) {
	u := &updater{sleep: sleepUntil, kick: make(chan struct{}, 1)}
	u.Kick()
	done := make(chan bool, 1)
	go func() { done <- u.waitNext(context.Background(), time.Hour) }()
	select {
	case got := <-done:
		if !got {
			t.Fatal("kick should make waitNext proceed (true)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("kick did not cut the 1h backstop wait within 2s")
	}
}

// END-TO-END: with the timer backstop parked far away (1h), a Kick must drive
// run() to an immediate reconcile — the /api/version gate GET fires within
// seconds and, since the server moved + serves a new binary, the swap→exec path
// runs. Proves reconnect-kick → immediate checkOnce end to end.
func TestRun_KickTriggersImmediateCheck(t *testing.T) {
	dir := t.TempDir()
	warden := filepath.Join(dir, "ocwarden")
	writeLive(t, warden, "OLD-WARDEN")

	versioned := make(chan struct{}, 1)
	get := func(path string) (int, []byte, error) {
		switch path {
		case versionPath:
			select {
			case versioned <- struct{}{}:
			default:
			}
			return 200, versionBody("newsha"), nil
		case wardenBinaryPath:
			return 200, []byte("NEW-WARDEN"), nil
		}
		return 404, nil, nil
	}
	u := newTestUpdater(stubOps{}, get, warden, "")
	u.interval = time.Hour          // backstop timer parked far away
	u.kick = make(chan struct{}, 1) // enable the event-driven seam
	u.execSelf = func() error { return nil }
	exited := make(chan struct{})
	u.exit = func(int) { close(exited) } // run() reaches this after the kicked swap

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go u.run(ctx)

	u.Kick() // the fast path: must cut the 1h wait

	select {
	case <-versioned:
	case <-time.After(2 * time.Second):
		t.Fatal("kick did not trigger an immediate /api/version check within 2s (backstop is 1h)")
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not reach the exit seam after the kicked warden swap")
	}
}

// SIGNATURE OBSERVABILITY (T-33d5): a successful swap logs the new binary's
// signing identity — informational only, resolved AFTER the swap on the live
// path, and its answer never gates anything.
func TestReconcileBinary_LogsSigningIdentityAfterSwap(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "ocwarden")
	writeLive(t, live, "OLD-BINARY")

	get := recordingGetter(map[string]getResult{
		wardenBinaryPath: {status: 200, body: []byte("NEW-BINARY")},
	}, nil)
	u := newTestUpdater(stubOps{}, get, live, "")
	var askedPath string
	var logged []string
	u.signatureOf = func(path string) string {
		askedPath = path
		return "OffiCraft Code Signing"
	}
	u.logf = func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}

	swapped, err := u.reconcileBinary(wardenBinaryPath, live, "ocwarden")
	if err != nil || !swapped {
		t.Fatalf("expected a clean swap, got swapped=%v err=%v", swapped, err)
	}
	if askedPath != live {
		t.Fatalf("signatureOf should be asked about the LIVE (swapped-in) path %s, got %s", live, askedPath)
	}
	want := "[ocwarden] self-update: ocwarden signing identity: OffiCraft Code Signing"
	found := false
	for _, line := range logged {
		if line == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the signing-identity log line %q in %q", want, logged)
	}
}

func TestReconcileBinary_EmptySigningIdentitySkipsLog(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "ocwarden")
	writeLive(t, live, "OLD-BINARY")

	get := recordingGetter(map[string]getResult{
		wardenBinaryPath: {status: 200, body: []byte("NEW-BINARY")},
	}, nil)
	u := newTestUpdater(stubOps{}, get, live, "")
	u.signatureOf = func(string) string { return "" } // codesign unavailable
	var logged []string
	u.logf = func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}

	if swapped, err := u.reconcileBinary(wardenBinaryPath, live, "ocwarden"); err != nil || !swapped {
		t.Fatalf("expected a clean swap, got swapped=%v err=%v", swapped, err)
	}
	for _, line := range logged {
		if strings.Contains(line, "signing identity") {
			t.Fatalf("no signing-identity line expected when the identity is unknown, got %q", line)
		}
	}
}

func TestParseCodesignIdentity(t *testing.T) {
	cases := []struct {
		name, out, want string
	}{
		{"adhoc", "Executable=/x/ocwarden\nIdentifier=ocwarden\nSignature=adhoc\nInfo.plist=not bound", "adhoc"},
		{"release identity takes the leaf Authority", "Executable=/x/ocwarden\nIdentifier=com.officraft.ocwarden\nAuthority=OffiCraft Code Signing\nTimestamp=none", "OffiCraft Code Signing"},
		{"first Authority wins over the chain", "Authority=Leaf CN\nAuthority=Intermediate CN\nAuthority=Root CN", "Leaf CN"},
		{"no signature info", "Executable=/x/ocwarden\nIdentifier=ocwarden", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		if got := parseCodesignIdentity(c.out); got != c.want {
			t.Errorf("%s: parseCodesignIdentity => %q, want %q", c.name, got, c.want)
		}
	}
}
