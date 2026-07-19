package main

import (
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"
)

// noSleep is the injected sweep pacer for tests — the poll loops must never
// wall-clock-wait in unit tests.
func noSleep(time.Duration) {}

// quietSweep is the zero-extra-legs sweep seam: no workdir listener discovery,
// fake pacing. Tests that don't exercise the ⓪/⑤ legs pass this.
func quietSweep() sweepSeams { return sweepSeams{sleep: noSleep} }

// tmuxFakeRunner is a richer shell seam than the telemetry fakeRunner: it maps
// an argv key -> (stdout, error) so a test can drive tmux's THREE-WAY outcomes
// (present / positively-absent / broken) with zero subprocess.
type tmuxFakeRunner struct {
	out map[string]string
	err map[string]error
}

func (f tmuxFakeRunner) Run(name string, args ...string) (string, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	if e, ok := f.err[key]; ok {
		return "", e
	}
	if s, ok := f.out[key]; ok {
		return s, nil
	}
	return "", fmt.Errorf("unclassifiable tmux failure") // broken/unknown
}

// killRecorder records every (pid, sig) the injected kill seam is asked to send, so a
// test can assert EXACTLY which signals were dispatched — and, critically for the
// irreversible-kill guardrails, assert that NO SIGKILL was sent when the pid was empty
// / non-numeric / 0 / stale / not-ours. The seam NEVER touches a real process.
type killRecorder struct {
	calls []struct {
		pid int
		sig syscall.Signal
	}
	// probeErr is returned for signal-0 existence probes (nil = exists-and-ours,
	// ESRCH = gone, EPERM = exists but not ours). killErr is returned for SIGKILL.
	probeErr error
	killErr  error
}

func (k *killRecorder) fn(pid int, sig syscall.Signal) error {
	k.calls = append(k.calls, struct {
		pid int
		sig syscall.Signal
	}{pid, sig})
	if sig == syscall.Signal(0) {
		return k.probeErr
	}
	return k.killErr
}

// sigkills returns the pids that received an actual SIGKILL (the irreversible leg).
// A NEGATIVE pid is a killpg (whole process group); a positive one a single pid.
func (k *killRecorder) sigkills() []int {
	var out []int
	for _, c := range k.calls {
		if c.sig == syscall.SIGKILL {
			out = append(out, c.pid)
		}
	}
	return out
}

// getpgid fakes: leaderPgid models the tmux-pane invariant (pane_pid IS its pgroup
// leader → pgid==pid); nonLeaderPgid models the unexpected non-leader case (→ single-
// pid fallback, never a stray group kill); brokenPgid models a getpgid failure.
func leaderPgid(pid int) (int, error)    { return pid, nil }
func nonLeaderPgid(pid int) (int, error) { return pid + 1, nil }
func brokenPgid(int) (int, error)        { return 0, syscall.ESRCH }

// sameInts reports whether a contains exactly the multiset want (order-agnostic).
func sameInts(a []int, want ...int) bool {
	if len(a) != len(want) {
		return false
	}
	m := map[int]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range want {
		m[x]--
		if m[x] < 0 {
			return false
		}
	}
	return true
}

const (
	hasKeyX     = "tmux -L officraft has-session -t member-x"
	panePidKeyX = "tmux -L officraft display-message -p -t member-x #{pane_pid}"
	psKey       = "ps -eo pid=,ppid="
)

// absentAfterKill / presentAfterKill / brokenAfterKill build the fake runner for the
// re-assert probe outcome. kill-session itself is best-effort (its return is ignored),
// so only the has-session key drives the verdict.
func absentAfterKill() tmuxFakeRunner {
	return tmuxFakeRunner{err: map[string]error{hasKeyX: fmt.Errorf("can't find session: member-x")}}
}
func presentAfterKill() tmuxFakeRunner {
	return tmuxFakeRunner{out: map[string]string{hasKeyX: ""}}
}
func brokenAfterKill() tmuxFakeRunner {
	return tmuxFakeRunner{err: map[string]error{hasKeyX: fmt.Errorf("unclassifiable boom")}}
}

// ── killSession: gated re-assert (mirrors tmux_kill_session) ──────────────────

func TestKillSession_GatedReassert(t *testing.T) {
	cases := []struct {
		name string
		run  tmuxFakeRunner
		want bool
	}{
		{"positively_gone_true", absentAfterKill(), true},
		{"still_present_false", presentAfterKill(), false},
		{"broken_reprobe_conservative_false", brokenAfterKill(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := killSession(tc.run, tmuxSocket, "member-x"); got != tc.want {
				t.Fatalf("killSession = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── stop: the SINGLE robust stop with escalation ladder (Seth-ruled) ──────────

func TestStop_FailSafeInvariants(t *testing.T) {
	// The bool verdict is gated on the FINAL ¬has_session. present/broken drive the
	// escalation path too, but with no pane pid set the escalation is a no-op — the
	// verdict is still fail-safe false (keep reporting online).
	cases := []struct {
		name string
		run  tmuxFakeRunner
		want bool
	}{
		{"killed_confirmed_gone", absentAfterKill(), true},
		{"not_killed_still_online", presentAfterKill(), false},
		{"broken_probe_conservative_false", brokenAfterKill(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &killRecorder{}
			if got, _ := stop(tc.run, tmuxSocket, "member-x", rec.fn, leaderPgid, quietSweep()); got != tc.want {
				t.Fatalf("stop = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStop_LightweightSuccess_NoEscalation(t *testing.T) {
	// The common graceful path: kill-session takes on the first (lightweight) leg →
	// stop returns true WITHOUT ever touching the irreversible kill seam.
	rec := &killRecorder{}
	if got, _ := stop(absentAfterKill(), tmuxSocket, "member-x", rec.fn, leaderPgid, quietSweep()); got != true {
		t.Fatalf("stop = %v, want true", got)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("lightweight success with an empty snapshot MUST NOT touch the kill seam, got %+v", rec.calls)
	}
}

func TestStop_EscalatesWhenLightweightFails(t *testing.T) {
	// Session stays present after the lightweight kill-session → stop escalates:
	// captures the fresh pane pid and killpg's the whole group; re-assert still sees
	// the session → fail-safe false (server re-issues next tick). The unconditional
	// sweep then TERM→KILLs the snapshotted pane pid too (the fake probe keeps it
	// "alive" forever), so a single-pid SIGKILL joins the killpg.
	run := tmuxFakeRunner{out: map[string]string{panePidKeyX: "4242\n", hasKeyX: ""}}
	rec := &killRecorder{}
	if got, _ := stop(run, tmuxSocket, "member-x", rec.fn, leaderPgid, quietSweep()); got != false {
		t.Fatalf("stop = %v, want false (session never went away)", got)
	}
	if kills := rec.sigkills(); !sameInts(kills, -4242, 4242) {
		t.Fatalf("expected killpg(-4242) on escalation + sweep SIGKILL(4242), got sigkills %v", kills)
	}
}

func TestStop_RefusesNonMemberSessions(t *testing.T) {
	// The member-guard short-circuits BEFORE any tmux kill or SIGKILL on a non-member.
	for _, sess := range []string{"", "member-", "telemetry", "warden-1", "random"} {
		var killHit bool
		run := recordingKillRunner(&killHit)
		rec := &killRecorder{}
		if got, _ := stop(run, tmuxSocket, sess, rec.fn, leaderPgid, quietSweep()); got != false {
			t.Errorf("stop(%q) = %v, want false (refused)", sess, got)
		}
		if killHit {
			t.Errorf("stop(%q) issued a tmux kill-session on a NON-member session", sess)
		}
		if len(rec.sigkills()) != 0 {
			t.Errorf("stop(%q) sent SIGKILL on a NON-member session", sess)
		}
	}
}

// lifecycleKill is a STATEFUL kill seam for the sweep tests: a pid probes alive
// (signal-0 nil) until it receives its dieOn signal, then probes ESRCH — modelling
// "dies on SIGTERM" (graceful listener) vs "dies only on SIGKILL" vs immortal
// (dieOn absent). eperm pids probe EPERM (exist, not ours).
type lifecycleKill struct {
	dead  map[int]bool
	eperm map[int]bool
	dieOn map[int]syscall.Signal
	terms []int
	kills []int
}

func (l *lifecycleKill) fn(pid int, sig syscall.Signal) error {
	if l.dead == nil {
		l.dead = map[int]bool{}
	}
	if sig == syscall.Signal(0) {
		if l.dead[pid] {
			return syscall.ESRCH
		}
		if l.eperm[pid] {
			return syscall.EPERM
		}
		return nil
	}
	if sig == syscall.SIGTERM {
		l.terms = append(l.terms, pid)
	}
	if sig == syscall.SIGKILL {
		l.kills = append(l.kills, pid)
	}
	if want, ok := l.dieOn[pid]; ok && sig == want {
		l.dead[pid] = true
	}
	return nil
}

func TestStop_SweepsDetachedListenerAfterLightweightKill(t *testing.T) {
	// THE zombie-ocagent regression case: kill-session TAKES on ① (session gone),
	// but a snapshotted descendant (the detached `ocagent listen` in claude's own
	// pgroup) survived the SIGHUP. The unconditional sweep must SIGTERM it by EXACT
	// pid and, once it dies in the grace window, report a clean stopped=true with
	// NO SIGKILL needed.
	run := tmuxFakeRunner{
		out: map[string]string{panePidKeyX: "4242\n", psKey: "4242 1\n5100 4242\n"},
		err: map[string]error{hasKeyX: fmt.Errorf("can't find session: member-x")},
	}
	lk := &lifecycleKill{
		dead:  map[int]bool{4242: true}, // the pane died with the session
		dieOn: map[int]syscall.Signal{5100: syscall.SIGTERM},
	}
	if got, _ := stop(run, tmuxSocket, "member-x", lk.fn, leaderPgid, quietSweep()); got != true {
		t.Fatalf("stop = %v, want true (listener swept clean)", got)
	}
	if !sameInts(lk.terms, 5100) {
		t.Fatalf("expected exactly SIGTERM(5100), got %v", lk.terms)
	}
	if len(lk.kills) != 0 {
		t.Fatalf("listener died in grace — no SIGKILL expected, got %v", lk.kills)
	}
}

func TestStop_SweepsWorkdirListenerWhenSessionAlreadyGone(t *testing.T) {
	// The suicide-zombie case: the tmux session is ALREADY gone (no pane pid to
	// walk), but a reparented `ocagent listen` still holds the SSE. The workdir
	// (cwd-match) snapshot leg finds it; a TERM-ignoring listener is escalated to
	// SIGKILL and stop still ends true once signal-0 proves it gone.
	run := tmuxFakeRunner{err: map[string]error{hasKeyX: fmt.Errorf("can't find session: member-x")}}
	var askedWorkdir string
	sw := sweepSeams{
		listenPIDs: func(wd string) []int { askedWorkdir = wd; return []int{9100} },
		workdir:    "/agents/x",
		sleep:      noSleep,
	}
	lk := &lifecycleKill{dieOn: map[int]syscall.Signal{9100: syscall.SIGKILL}}
	if got, _ := stop(run, tmuxSocket, "member-x", lk.fn, leaderPgid, sw); got != true {
		t.Fatalf("stop = %v, want true (zombie listener reaped)", got)
	}
	if askedWorkdir != "/agents/x" {
		t.Fatalf("listenPIDs asked for workdir %q, want /agents/x", askedWorkdir)
	}
	if !sameInts(lk.terms, 9100) || !sameInts(lk.kills, 9100) {
		t.Fatalf("expected SIGTERM then SIGKILL on 9100, got terms=%v kills=%v", lk.terms, lk.kills)
	}
}

func TestStop_HonestPartialWhenSurvivorOutlivesSweep(t *testing.T) {
	// A snapshot pid that survives SIGTERM AND SIGKILL through both poll windows
	// (e.g. an unkillable D-state process) must yield stopped=false — the honest
	// partial — even though ¬has_session confirmed, so the server re-issues stop
	// instead of trusting a fake ok over a live zombie.
	run := tmuxFakeRunner{err: map[string]error{hasKeyX: fmt.Errorf("can't find session: member-x")}}
	sw := sweepSeams{
		listenPIDs: func(string) []int { return []int{9100} },
		workdir:    "/agents/x",
		sleep:      noSleep,
	}
	lk := &lifecycleKill{} // 9100 immortal: probes alive forever
	if got, _ := stop(run, tmuxSocket, "member-x", lk.fn, leaderPgid, sw); got != false {
		t.Fatalf("stop = %v, want false (survivor outlived the sweep)", got)
	}
	if !sameInts(lk.terms, 9100) || !sameInts(lk.kills, 9100) {
		t.Fatalf("expected the full TERM→KILL escalation on 9100, got terms=%v kills=%v", lk.terms, lk.kills)
	}
}

// ── sweepPIDs / snapshotMemberPIDs: the ⓪/⑤ ladder legs ──────────────────────

func TestSweepPIDs_EPERMNotOursNeverSignalled(t *testing.T) {
	// EPERM = the pid number was reused by ANOTHER uid's process → not our member:
	// never a kill target and never a sweep failure.
	lk := &lifecycleKill{eperm: map[int]bool{7777: true}}
	if got := sweepPIDs([]int{7777}, lk.fn, noSleep); got != true {
		t.Fatalf("sweepPIDs = %v, want true (EPERM pid is not ours, not a survivor)", got)
	}
	if len(lk.terms) != 0 || len(lk.kills) != 0 {
		t.Fatalf("EPERM pid must never be signalled, got terms=%v kills=%v", lk.terms, lk.kills)
	}
}

func TestSweepPIDs_AlreadyDeadIsCleanWithoutWaiting(t *testing.T) {
	// The common path: everything died with the session → zero signals, zero sleeps.
	slept := 0
	lk := &lifecycleKill{dead: map[int]bool{4242: true, 5100: true}}
	if got := sweepPIDs([]int{4242, 5100}, lk.fn, func(time.Duration) { slept++ }); got != true {
		t.Fatalf("sweepPIDs = %v, want true", got)
	}
	if len(lk.terms) != 0 || len(lk.kills) != 0 || slept != 0 {
		t.Fatalf("dead pids must cost no signals and no waiting, got terms=%v kills=%v sleeps=%d", lk.terms, lk.kills, slept)
	}
}

func TestSnapshotMemberPIDs_TreePlusWorkdirDeduped(t *testing.T) {
	// Snapshot = pane pid + descendants + workdir listeners, deduped, never pid ≤ 1.
	run := tmuxFakeRunner{out: map[string]string{
		panePidKeyX: "4242\n",
		psKey:       "4242 1\n5000 4242\n",
	}}
	sw := sweepSeams{
		listenPIDs: func(string) []int { return []int{5000, 9100, 0, 1, -3} },
		workdir:    "/agents/x",
	}
	if got := snapshotMemberPIDs(run, tmuxSocket, "member-x", sw); !sameInts(got, 4242, 5000, 9100) {
		t.Fatalf("snapshot = %v, want {4242,5000,9100}", got)
	}
}

func TestSnapshotMemberPIDs_NoPaneNoWorkdirIsEmpty(t *testing.T) {
	// No live pane and no workdir resolution → an empty kill list (never a guess).
	if got := snapshotMemberPIDs(tmuxFakeRunner{}, tmuxSocket, "member-x", sweepSeams{}); len(got) != 0 {
		t.Fatalf("snapshot = %v, want empty", got)
	}
}

// ── ocagentPIDsByCwd / memberWorkdirForSession: workdir-anchored discovery ────

const lsofKey = "lsof -a -c ocagent -d cwd -F pn"

func TestOcagentPIDsByCwd_ExactCwdMatchOnly(t *testing.T) {
	run := tmuxFakeRunner{out: map[string]string{
		lsofKey: "p9100\nfcwd\nn/home/agents/x\np9200\nfcwd\nn/home/agents/y\np9300\nfcwd\nn/home/agents/x/sub\n",
	}}
	if got := ocagentPIDsByCwd(run, "/home/agents/x"); !sameInts(got, 9100) {
		t.Fatalf("ocagentPIDsByCwd = %v, want {9100} (exact match only, never prefix)", got)
	}
}

func TestOcagentPIDsByCwd_BrokenLsofYieldsNil(t *testing.T) {
	// lsof missing / zero matches (it exits non-zero) → nil, a benign no-discovery.
	if got := ocagentPIDsByCwd(tmuxFakeRunner{}, "/home/agents/x"); got != nil {
		t.Fatalf("broken lsof must yield nil, got %v", got)
	}
}

func TestMemberWorkdirForSession(t *testing.T) {
	cases := []struct {
		home, session, want string
	}{
		{"/home/agents", "member-X", "/home/agents/x"}, // id lowercased like agentWorkdir
		{"/home/agents", "member-kyle", "/home/agents/kyle"},
		{"", "member-kyle", ""},         // unknown home → no guessing
		{"/home/agents", "member-", ""}, // bare prefix refused
		{"/home/agents", "telemetry", ""},
	}
	for _, tc := range cases {
		if got := memberWorkdirForSession(tc.home, tc.session); got != tc.want {
			t.Errorf("memberWorkdirForSession(%q,%q) = %q, want %q", tc.home, tc.session, got, tc.want)
		}
	}
}

// ── escalateKill: the irreversible pgroup hard-kill + orphan reap ─────────────

func TestEscalateKill_KillpgWholeGroup(t *testing.T) {
	// pane_pid IS the pgroup leader → killpg the whole group (negative pid), reaping
	// claude + all its non-detached children in one shot.
	run := tmuxFakeRunner{out: map[string]string{panePidKeyX: "4242\n"}}
	rec := &killRecorder{}
	escalateKill(run, tmuxSocket, "member-x", rec.fn, leaderPgid)
	if kills := rec.sigkills(); !sameInts(kills, -4242) {
		t.Fatalf("expected killpg(-4242), got %v", kills)
	}
}

func TestEscalateKill_NonLeaderFallbackSinglePid(t *testing.T) {
	// getpgid says the captured pid is NOT the group leader → fall back to a single-pid
	// SIGKILL. NEVER a negative-pid kill at a group we don't lead (would hit strangers).
	run := tmuxFakeRunner{out: map[string]string{panePidKeyX: "4242\n"}}
	rec := &killRecorder{}
	escalateKill(run, tmuxSocket, "member-x", rec.fn, nonLeaderPgid)
	if kills := rec.sigkills(); !sameInts(kills, 4242) {
		t.Fatalf("expected single-pid SIGKILL 4242 (no group kill), got %v", kills)
	}
}

func TestEscalateKill_BrokenGetpgidFallbackSinglePid(t *testing.T) {
	// getpgid errors → cannot prove leadership → single-pid fallback, never a group.
	run := tmuxFakeRunner{out: map[string]string{panePidKeyX: "4242\n"}}
	rec := &killRecorder{}
	escalateKill(run, tmuxSocket, "member-x", rec.fn, brokenPgid)
	if kills := rec.sigkills(); !sameInts(kills, 4242) {
		t.Fatalf("expected single-pid SIGKILL 4242 on broken getpgid, got %v", kills)
	}
}

func TestEscalateKill_TreeWalkReapsDetachedOrphans(t *testing.T) {
	// killpg can't reach a descendant that setsid'd to a new pgroup → the tree-walk
	// reaps them. 5000←4242, 6000←5000 are descendants; 7000←1 is unrelated.
	run := tmuxFakeRunner{out: map[string]string{
		panePidKeyX: "4242\n",
		psKey:       "4242 1\n5000 4242\n6000 5000\n7000 1\n",
	}}
	rec := &killRecorder{}
	escalateKill(run, tmuxSocket, "member-x", rec.fn, leaderPgid)
	// killpg(-4242) + the two detached descendants 5000, 6000; NOT the unrelated 7000.
	if kills := rec.sigkills(); !sameInts(kills, -4242, 5000, 6000) {
		t.Fatalf("expected killpg(-4242)+reap 5000,6000, got %v", kills)
	}
}

func TestEscalateKill_NeverKillsBadPID(t *testing.T) {
	// empty / non-numeric (scrubbed to "") / 0 pane pid → the escalation must issue NO
	// kill of any kind (not even the signal-0 probe).
	for _, pp := range []string{"", "not-a-pid", "0"} {
		run := tmuxFakeRunner{out: map[string]string{panePidKeyX: pp}}
		rec := &killRecorder{}
		escalateKill(run, tmuxSocket, "member-x", rec.fn, leaderPgid)
		if len(rec.calls) != 0 {
			t.Errorf("pane pid %q: escalation touched the kill seam: %+v", pp, rec.calls)
		}
	}
}

func TestEscalateKill_StalePID_NoKill(t *testing.T) {
	// signal-0 → ESRCH (gone / number reused) → MUST NOT SIGKILL a possibly-unrelated
	// process; and the verify-before-kill probe MUST have run.
	run := tmuxFakeRunner{out: map[string]string{panePidKeyX: "9999\n"}}
	rec := &killRecorder{probeErr: syscall.ESRCH}
	escalateKill(run, tmuxSocket, "member-x", rec.fn, leaderPgid)
	if kills := rec.sigkills(); len(kills) != 0 {
		t.Fatalf("SIGKILL fired on a stale/ESRCH pid: %v", kills)
	}
	if len(rec.calls) != 1 || rec.calls[0].sig != syscall.Signal(0) {
		t.Fatalf("expected exactly one signal-0 existence probe, got %+v", rec.calls)
	}
}

func TestEscalateKill_EPERM_NotOurs_NoKill(t *testing.T) {
	// signal-0 → EPERM: exists but owned by another uid ⇒ pid-number reuse, NOT our
	// member ⇒ do NOT SIGKILL.
	run := tmuxFakeRunner{out: map[string]string{panePidKeyX: "7777\n"}}
	rec := &killRecorder{probeErr: syscall.EPERM}
	escalateKill(run, tmuxSocket, "member-x", rec.fn, leaderPgid)
	if kills := rec.sigkills(); len(kills) != 0 {
		t.Fatalf("SIGKILL fired on an EPERM (not-ours) pid: %v", kills)
	}
}

// perPidKill is a kill seam whose signal-0 probe answer varies BY pid (unlike
// killRecorder's single global probeErr), so a test can model the MIXED scenario
// "pane pid live but a specific descendant is stale" — the case that exercises the
// per-descendant verify-before-kill gate (escalateKill ⑤). killRecorder can't express
// it (its probe is global, and once the pane probe returns non-nil escalate early-
// returns before the descendant loop even runs).
type perPidKill struct {
	probe map[int]error // signal-0 answer per pid (absent key = nil = live-and-ours)
	kills []int         // pids that received an actual SIGKILL
}

func (p *perPidKill) fn(pid int, sig syscall.Signal) error {
	if sig == syscall.Signal(0) {
		return p.probe[pid]
	}
	p.kills = append(p.kills, pid)
	return nil
}

func TestEscalateKill_StaleDescendant_NotReaped(t *testing.T) {
	// pane pid 4242 is a live leader → killpg(-4242). Of its descendants, 5000 is
	// stale (signal-0 ESRCH) and 6000 is live. The per-descendant gate MUST skip the
	// stale 5000 (never SIGKILL a reused number) and reap only the live 6000. This
	// guards the per-descendant verify-before-kill gate against a regression.
	run := tmuxFakeRunner{out: map[string]string{
		panePidKeyX: "4242\n",
		psKey:       "4242 1\n5000 4242\n6000 4242\n",
	}}
	pk := &perPidKill{probe: map[int]error{5000: syscall.ESRCH}}
	escalateKill(run, tmuxSocket, "member-x", pk.fn, leaderPgid)
	if !sameInts(pk.kills, -4242, 6000) {
		t.Fatalf("expected killpg(-4242)+reap live 6000, SKIP stale 5000; got %v", pk.kills)
	}
}

// ── descendantPIDs: process-tree walk ─────────────────────────────────────────

func TestDescendantPIDs(t *testing.T) {
	run := tmuxFakeRunner{out: map[string]string{
		psKey: "100 1\n200 100\n300 200\n400 100\n500 1\n",
	}}
	// descendants of 100: 200,300,400 (500 unrelated); root 100 itself excluded.
	if got := descendantPIDs(run, 100); !sameInts(got, 200, 300, 400) {
		t.Fatalf("descendantPIDs(100) = %v, want {200,300,400}", got)
	}
	// broken ps → nil (best-effort, no walk).
	if got := descendantPIDs(tmuxFakeRunner{}, 100); got != nil {
		t.Fatalf("broken ps must yield nil, got %v", got)
	}
}

func TestDescendantPIDs_CycleGuard(t *testing.T) {
	// A self-referential / cyclic ppid table must not loop forever.
	run := tmuxFakeRunner{out: map[string]string{psKey: "100 100\n200 100\n100 200\n"}}
	got := descendantPIDs(run, 100) // must terminate
	// 200 is a child of 100; the 100→200→100 cycle must not re-emit root.
	if !sameInts(got, 200) {
		t.Fatalf("cycle walk = %v, want {200}", got)
	}
}

// ── guard-unit tests ──────────────────────────────────────────────────────────

func TestIsMemberSession(t *testing.T) {
	cases := map[string]bool{
		"member-alice": true,
		"member-x":     true,
		"member-":      false, // bare prefix, no id
		"":             false,
		"telemetry":    false,
		"warden-1":     false,
		"xmember-a":    false,
	}
	for sess, want := range cases {
		if got := isMemberSession(sess); got != want {
			t.Errorf("isMemberSession(%q) = %v, want %v", sess, got, want)
		}
	}
}

func TestParseKillablePID(t *testing.T) {
	cases := []struct {
		in string
		n  int
		ok bool
	}{
		{"4242", 4242, true},
		{"1", 1, true},
		{"", 0, false},
		{"0", 0, false},   // whole-process-group — refused
		{"-1", 0, false},  // negative = process GROUP — refused
		{"abc", 0, false}, // non-numeric
		{"12x", 0, false}, // partial-numeric
	}
	for _, tc := range cases {
		n, ok := parseKillablePID(tc.in)
		if n != tc.n || ok != tc.ok {
			t.Errorf("parseKillablePID(%q) = (%d,%v), want (%d,%v)", tc.in, n, ok, tc.n, tc.ok)
		}
	}
}

// recordingKillRunner returns a runner that flips *hit true iff a tmux kill-session
// argv is ever executed — used to prove the member-guard short-circuits before tmux.
func recordingKillRunner(hit *bool) CmdRunner {
	return killHitRunner{hit: hit}
}

type killHitRunner struct{ hit *bool }

func (r killHitRunner) Run(name string, args ...string) (string, error) {
	if len(args) >= 3 && args[2] == "kill-session" {
		*r.hit = true
	}
	return "", fmt.Errorf("unclassifiable")
}

// ── T-9adc: the noop verdict — "there was nothing here to kill" ──────────────

// statefulTmuxRunner flips has-session from PRESENT to ABSENT once kill-session
// runs — the minimal stateful fake for "a real session actually died here".
type statefulTmuxRunner struct{ killed *bool }

func (f statefulTmuxRunner) Run(name string, args ...string) (string, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	switch {
	case strings.Contains(key, "kill-session"):
		*f.killed = true
		return "", nil
	case key == hasKeyX:
		if *f.killed {
			return "", fmt.Errorf("can't find session: member-x")
		}
		return "", nil // present
	case key == panePidKeyX:
		return "", fmt.Errorf("no pane") // keep the snapshot empty (⑤ trivially clean)
	}
	return "", fmt.Errorf("unclassifiable tmux failure")
}

// TestStop_NoopWhenNothingWasEverThere: session positively absent BEFORE any
// kill + empty snapshot → (stopped=true, noop=true). This is the identity-sweep
// / mis-routed stop shape whose receipt must carry no_such_session so the
// server never folds it as a real kill (T-9adc).
func TestStop_NoopWhenNothingWasEverThere(t *testing.T) {
	rec := &killRecorder{}
	stopped, noop := stop(absentAfterKill(), tmuxSocket, "member-x", rec.fn, leaderPgid, quietSweep())
	if !stopped {
		t.Fatalf("idempotent stop over an absent session must stay ok, got stopped=%v", stopped)
	}
	if !noop {
		t.Fatal("no session + no member process must report noop=true")
	}
}

// TestStop_RealKillIsNotNoop: a session that was PRESENT before the kill (and
// died on the lightweight leg) is a genuine kill — noop must be false.
func TestStop_RealKillIsNotNoop(t *testing.T) {
	killed := false
	rec := &killRecorder{}
	stopped, noop := stop(statefulTmuxRunner{killed: &killed}, tmuxSocket, "member-x",
		rec.fn, leaderPgid, quietSweep())
	if !stopped {
		t.Fatalf("the kill took — want stopped=true, got %v", stopped)
	}
	if noop {
		t.Fatal("a REAL kill must never report noop=true (the receipt would lie the other way)")
	}
}

// TestStop_SessionGoneButListenerAliveIsNotNoop: the suicide-zombie shape — no
// tmux session but a workdir-anchored ocagent still alive. Something real was
// reaped, so noop must be false even though the session was absent.
func TestStop_SessionGoneButListenerAliveIsNotNoop(t *testing.T) {
	run := tmuxFakeRunner{err: map[string]error{hasKeyX: fmt.Errorf("can't find session: member-x")}}
	sw := sweepSeams{
		listenPIDs: func(string) []int { return []int{9100} },
		workdir:    "/agents/x",
		sleep:      noSleep,
	}
	lk := &lifecycleKill{dieOn: map[int]syscall.Signal{9100: syscall.SIGTERM}}
	stopped, noop := stop(run, tmuxSocket, "member-x", lk.fn, leaderPgid, sw)
	if !stopped {
		t.Fatalf("listener reaped clean — want stopped=true, got %v", stopped)
	}
	if noop {
		t.Fatal("a swept listener is a real kill — noop must be false")
	}
}

// TestStop_BrokenProbeIsNotNoop: a broken has-session probe can never claim
// no-op (conservative: only a POSITIVE absence reads noop).
func TestStop_BrokenProbeIsNotNoop(t *testing.T) {
	rec := &killRecorder{}
	stopped, noop := stop(brokenAfterKill(), tmuxSocket, "member-x", rec.fn, leaderPgid, quietSweep())
	if stopped {
		t.Fatalf("broken probe must stay honest-false, got stopped=%v", stopped)
	}
	if noop {
		t.Fatal("a broken probe must never read as a no-op")
	}
}
