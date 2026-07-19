// Phase 3 "hands": the stop EXECUTION mechanism of the stateless warden — the last
// and most dangerous leg (an IRREVERSIBLE process-group kill).
//
// v2 is a server→warden PUSH model: the SERVER decides WHEN to stop a member (grace
// judgement, the 120s stopping_timed_out clock, rule-③ reconcile — all live
// server-side). This file is PURELY the executor: given a server-downpushed stop
// command it runs the host-local kill MECHANISM and re-asserts death. It reuses the
// Phase 1 CmdRunner seam, tmuxHasSession three-way probe and tmuxPanePID.
//
// SETH-RULED DESIGN — a SINGLE robust `stop` with a built-in escalation ladder
// (NOT two separate stop/force_kill RPCs). "Once the lightweight kill fails, we do
// the force kill." One command, escalating:
//
//	⓪ SNAPSHOT: capture the member's FULL process footprint BEFORE any kill —
//	   the pane pid + its descendant tree (links still intact) PLUS any lingering
//	   ocagent process anchored to the member's workdir (cwd match). The workdir
//	   leg is what catches an `ocagent listen` that claude started in its OWN
//	   process group (tmux kill-session's SIGHUP never reaches it) and even a
//	   zombie already reparented to init by an earlier failed stop.
//	① LIGHTWEIGHT: tmux kill-session (sends SIGHUP — gives claude a graceful
//	   shutdown + a chance to reap its OWN children) + re-assert.
//	② DETECT FAILURE: the re-assert still sees the session (or a broken probe).
//	③ ESCALATE (irreversible): killpg(pane_pid) SIGKILL the whole pane process
//	   GROUP + a process-tree walk to reap any descendant that DETACHED to a new
//	   pgroup (setsid / double-fork orphans killpg can't reach) + kill-session.
//	④ AUTHORITATIVE session re-assert: ¬has_session gates the verdict.
//	⑤ SWEEP (unconditional — runs even when ① took): every snapshot pid still
//	   alive gets an EXACT-PID SIGTERM → grace poll → SIGKILL → poll until
//	   signal-0 proves ALL gone. NEVER a pattern kill (no pkill/killall — the
//	   host runs unrelated services); every signal is re-verified per pid.
//	   `stopped` is true ONLY when ¬has_session AND the sweep is clean — a sweep
//	   timeout honestly reports false (partial) so the server re-issues stop,
//	   never a fake ok over a live zombie holding a fake-online SSE.
//
// WHY killpg not a single pid: a tmux pane process is a process-GROUP LEADER
// (pane_pid == pgid — verified empirically: `tmux new-session` forkpty+setsid gives
// each pane a fresh session, and a plain `&` child stays in the pane's pgroup). So
// killpg(pane_pid) reaps claude AND its non-detached children in one shot; the
// tree-walk is the belt-and-suspenders for anything that setsid'd away.
//
// DELIBERATELY NOT here (server owns these in v2 — porting them = stateless breach):
//   - stand-down notify                                       → server (B5)
//   - stopping_timed_out read / grace judgement / 120s clock  → server (T2.1)
//   - rule-③ reconcile / decision state / reconcile loop      → server (T2.1)
//
// Faithful to the Python origin's KILL leg (agent/reconcile.py tmux_kill_session +
// TmuxSpawnPort.retire ②KILL/③RE-ASSERT). Two DELIBERATE deltas: the ①STAND-DOWN
// leg is dropped (server owns it, B5); and the killpg/SIGKILL escalation is NEW vs
// python — the origin's "force-kill" (rule ③) is JUST tmux_kill_session + re-assert
// with NO os.kill on the pane pid (grep SIGKILL: no hit). v2 hardens it so a
// SIGHUP-ignoring pane (and its orphans) still die. Flagged.
//
// ASYNC contract (wade-ruled): stop does NOT synchronously return {stopped} to the
// server — under NAT push the server cannot inline-await. The kill result flows back
// ASYNC via the Phase 1 presence report: a killed session DROPS OUT of the next
// presence sessions[] (the server's confirmation of death); a kill that did not take
// keeps reporting online. The bool stop returns is the IMMEDIATE local signal for the
// local caller / tests — the authoritative death verdict is presence, not this value.
package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// killFunc is the injectable os.kill(pid, sig) seam. Real runs pass realKill
// (syscall.Kill); tests pass a fake so the IRREVERSIBLE SIGKILL is never sent at a
// real process. Signal 0 (no signal) is a pure existence/permission probe; SIGKILL
// is the irreversible hard kill. A NEGATIVE pid signals the whole process GROUP
// (killpg) — only ever passed after an explicit pgroup-leader verification.
type killFunc func(pid int, sig syscall.Signal) error

// realKill is the production kill seam (syscall.Kill). Phase 4 wiring passes it.
func realKill(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }

// pgidFunc is the injectable getpgid seam. It guards the killpg step: we only signal
// a process GROUP when the captured pane pid IS that group's leader (pgid == pane_pid)
// — otherwise a negative-pid kill would hit a DIFFERENT, unrelated group.
type pgidFunc func(pid int) (int, error)

// realGetpgid is the production getpgid seam. Phase 4 wiring passes it.
func realGetpgid(pid int) (int, error) { return syscall.Getpgid(pid) }

// ---------------------------------------------------------------------------
// member-session guard — the kill mechanism NEVER touches anything that is not a
// member-* session. It refuses the telemetry warden's own session, a bare
// "member-" with no id, and any non-member name. This is the outermost gate (a
// decision-free structural refusal, not a server judgement).
// ---------------------------------------------------------------------------

// isMemberSession reports whether session is a real member-<id> tmux session
// (prefix "member-" AND a non-empty id after it). A bare "member-", "", the
// telemetry warden's session, or any other name is refused.
func isMemberSession(session string) bool {
	return strings.HasPrefix(session, memberSessionPrefix) && len(session) > len(memberSessionPrefix)
}

// parseKillablePID validates a pane-pid string for the IRREVERSIBLE kill path.
// Conservative by construction: empty / non-numeric / 0 / negative → (0, false) =
// DO NOT KILL. tmuxPanePID already returns only digit-strings or "", so this is a
// defense-in-depth re-check that also rejects "0" (which os.kill treats as the
// whole process group — a catastrophic over-kill) and any negative number (a
// negative pid signals a process GROUP — never a single pane).
func parseKillablePID(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// ---------------------------------------------------------------------------
// killSession — the GATED tmux kill helper (byte-for-byte port of the origin's
// tmux_kill_session). Run kill-session, then RE-PROBE: True ONLY when has_session
// is POSITIVELY false. A kill that did not take, or a BROKEN re-probe (nil), reads
// False — NEVER an assumed-dead (fail-safe: never falsely report a live member as
// killed, or the server places against a ghost).
// ---------------------------------------------------------------------------

func killSession(r CmdRunner, socket, session string) bool {
	// kill-session is best-effort; its own exit is not trusted — only the re-probe
	// verdict counts (mirrors the origin: it ignores the kill-session return).
	_, _ = r.Run("tmux", "-L", socket, "kill-session", "-t", session)
	has := tmuxHasSession(r, socket, session)
	return has != nil && !*has // POSITIVELY gone (probe ran) — the only killed verdict
}

// ---------------------------------------------------------------------------
// descendantPIDs walks the process tree via `ps -eo pid=,ppid=` and returns every
// descendant of root (children, grandchildren, …), NEVER root itself. The caller reaps
// them — the PAYOFF being any descendant that DETACHED to a new pgroup (setsid /
// double-fork) which killpg could not reach (the non-detached ones killpg already
// got; re-killing them is idempotent). It is captured BEFORE the killpg so the
// parent→child links are still intact (post-kill, orphans reparent to init and the
// links vanish). Best-effort: a broken ps returns nil. Cycle-guarded; rejects
// non-positive pids and negative ppids.
// ---------------------------------------------------------------------------

func descendantPIDs(r CmdRunner, root int) []int {
	out, err := r.Run("ps", "-eo", "pid=,ppid=")
	if err != nil {
		return nil
	}
	children := map[int][]int{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) != 2 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		if e1 != nil || e2 != nil || pid <= 0 || ppid < 0 {
			continue
		}
		children[ppid] = append(children[ppid], pid)
	}
	var desc []int
	seen := map[int]bool{root: true} // never re-visit / never emit root
	queue := append([]int{}, children[root]...)
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if pid <= 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		desc = append(desc, pid)
		queue = append(queue, children[pid]...)
	}
	return desc
}

// ---------------------------------------------------------------------------
// escalateKill — the IRREVERSIBLE hard-kill escalation, run ONLY after the
// lightweight kill-session failed to take. It reaps the whole pane process group
// plus any detached descendants. Every kill target is a freshly-captured, existence-
// verified pid — NEVER a passed-in / stale number, NEVER a pattern. Best-effort:
// the authoritative death verdict remains the caller's post-escalation re-assert.
// ---------------------------------------------------------------------------

func escalateKill(r CmdRunner, socket, session string, kill killFunc, getpgid pgidFunc) {
	// ① capture the pane pid FRESH — tmux only reports a pane_pid for a LIVE pane,
	// so a value here is the current pane process, not a stale record.
	n, ok := parseKillablePID(tmuxPanePID(r, socket, session))
	if !ok {
		return // no live pane pid to escalate against
	}
	// ② VERIFY existence + our-ownership with signal 0 (pure probe, kills nothing).
	// Only err==nil (exists AND we can signal it = our spawned member) proceeds.
	// ESRCH → pid already gone (do NOT kill a reused number); EPERM → exists but
	// owned by another uid → NOT our member → do NOT kill.
	if kill(n, syscall.Signal(0)) != nil {
		return
	}
	// ③ snapshot descendants BEFORE the kill (links intact); reap detached orphans after.
	descs := descendantPIDs(r, n)
	// ④ kill the process GROUP — but ONLY when the captured pid IS the group leader
	// (pgid == pane_pid, the tmux-pane invariant). A negative-pid kill hits the whole
	// group, so mis-firing at a non-leader would signal an UNRELATED group. If it is
	// not (unexpectedly) the leader, fall back to killing the single verified pid.
	if pgid, err := getpgid(n); err == nil && pgid == n {
		// RE-VERIFY existence immediately before the GROUP kill — narrows the
		// capture→getpgid→kill pid-reuse window (a pid that died and got reused by an
		// unrelated group leader would otherwise take a stray killpg). Not race-free
		// (POSIX pid kill is inherently racy without pidfd), but the smallest window
		// portable stdlib allows, and killpg is the widest-blast-radius leg.
		if kill(n, syscall.Signal(0)) == nil {
			_ = kill(-n, syscall.SIGKILL) // negative pid == killpg(pgid)
		}
	} else {
		_ = kill(n, syscall.SIGKILL) // not a leader → single pid only, never a stray group
	}
	// ⑤ reap ALL captured descendants — each re-verified (signal-0) before kill. killpg
	// already reaped the non-detached ones (this re-SIGKILL is idempotent/harmless); the
	// POINT of this leg is the DETACHED descendants (setsid / double-fork) that fled to a
	// new pgroup and killpg could not reach. A stale/reused descendant number is skipped.
	for _, d := range descs {
		if kill(d, syscall.Signal(0)) == nil {
			_ = kill(d, syscall.SIGKILL)
		}
	}
}

// ---------------------------------------------------------------------------
// snapshot + sweep — the ⓪/⑤ legs of the ladder. The snapshot is taken BEFORE any
// kill (parent→child links intact; post-kill, orphans reparent to init and the tree
// is unwalkable); the sweep runs UNCONDITIONALLY after the session kill, because a
// took-on-①  kill-session proves nothing about processes OUTSIDE the pane's SIGHUP
// reach (the escaped `ocagent listen` in claude's own pgroup is exactly that).
// ---------------------------------------------------------------------------

const (
	// sweepPollInterval × sweepTermPolls = the SIGTERM grace window (3 s); ×
	// sweepKillPolls = the post-SIGKILL confirmation window (2 s). The sweep only
	// ever sleeps while a target is STILL alive — the common path (everything died
	// with the session) exits on the first zero-survivor check with zero waiting.
	sweepPollInterval = 200 * time.Millisecond
	sweepTermPolls    = 15
	sweepKillPolls    = 10
)

// sweepSeams carries the injectable seams of the ⓪ snapshot + ⑤ sweep legs.
// listenPIDs discovers lingering ocagent processes anchored to workdir (production:
// ocagentPIDsByCwd over lsof); nil or an empty workdir disables that leg (the pane
// tree snapshot still runs). sleep nil falls back to time.Sleep (fail-safe: a
// mis-wired production caller still paces, only tests inject a fake).
type sweepSeams struct {
	listenPIDs func(workdir string) []int
	workdir    string
	sleep      func(time.Duration)
}

// snapshotMemberPIDs captures the member's full process footprint: the live pane
// pid + every descendant (tree walked while links are intact) + any ocagent
// process whose cwd is the member's workdir. Deduped; never pid ≤ 1 (init /
// invalid) and never the warden's own pid — the snapshot is a KILL LIST, so its
// membership is conservative by construction.
func snapshotMemberPIDs(r CmdRunner, socket, session string, sw sweepSeams) []int {
	var pids []int
	if n, ok := parseKillablePID(tmuxPanePID(r, socket, session)); ok {
		pids = append(pids, n)
		pids = append(pids, descendantPIDs(r, n)...)
	}
	if sw.listenPIDs != nil && sw.workdir != "" {
		pids = append(pids, sw.listenPIDs(sw.workdir)...)
	}
	self := os.Getpid()
	seen := map[int]bool{}
	var out []int
	for _, p := range pids {
		if p <= 1 || p == self || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// livePIDs filters pids down to the ones signal-0 proves alive AND ours (err==nil).
// ESRCH → gone (or the number was reused and freed again — either way not a kill
// target); EPERM → exists but another uid's → pid-number reuse, NOT our member →
// never a target and never a sweep failure.
func livePIDs(pids []int, kill killFunc) []int {
	var alive []int
	for _, p := range pids {
		if kill(p, syscall.Signal(0)) == nil {
			alive = append(alive, p)
		}
	}
	return alive
}

// sweepPIDs reaps every snapshot pid still alive: EXACT-PID SIGTERM (graceful — the
// ocagent listener traps SIGTERM) → grace poll → SIGKILL the survivors → poll until
// signal-0 proves all gone. Returns true ONLY when every target is verified dead;
// a timeout returns false — the honest "partial" verdict (never fake ok while a
// zombie holds a fake-online SSE). Every signal is per-pid and re-verified; NEVER
// a pattern kill.
func sweepPIDs(pids []int, kill killFunc, sleep func(time.Duration)) bool {
	if sleep == nil {
		sleep = time.Sleep
	}
	alive := livePIDs(pids, kill)
	if len(alive) == 0 {
		return true
	}
	for _, p := range alive {
		_ = kill(p, syscall.SIGTERM)
	}
	for i := 0; i < sweepTermPolls; i++ {
		sleep(sweepPollInterval)
		if alive = livePIDs(alive, kill); len(alive) == 0 {
			return true
		}
	}
	for _, p := range alive {
		_ = kill(p, syscall.SIGKILL)
	}
	for i := 0; i < sweepKillPolls; i++ {
		sleep(sweepPollInterval)
		if alive = livePIDs(alive, kill); len(alive) == 0 {
			return true
		}
	}
	return false
}

// ocagentPIDsByCwd is the production listenPIDs seam: lsof-discover every ocagent
// process whose cwd is EXACTLY workdir (the member's durable per-agent dir — the
// spawn shim cd's there before exec, so a member's `ocagent listen` inherits it;
// other members' listeners live in OTHER workdirs and never match). Discovery may
// match by name+cwd; the KILL side stays exact-pid + signal-0 verified (sweepPIDs).
// Best-effort: a missing/failed lsof (it exits non-zero on zero matches) reads nil.
func ocagentPIDsByCwd(r CmdRunner, workdir string) []int {
	out, err := r.Run("lsof", "-a", "-c", "ocagent", "-d", "cwd", "-F", "pn")
	if err != nil {
		return nil
	}
	// lsof reports the KERNEL-resolved cwd (symlinks expanded, e.g. /var → /private/var
	// on macOS), so match against both the given path and its symlink resolution.
	want := map[string]bool{filepath.Clean(workdir): true}
	if resolved, e := filepath.EvalSymlinks(workdir); e == nil {
		want[filepath.Clean(resolved)] = true
	}
	var pids []int
	cur := 0
	for _, ln := range strings.Split(out, "\n") {
		if len(ln) < 2 {
			continue
		}
		switch ln[0] {
		case 'p':
			n, e := strconv.Atoi(ln[1:])
			if e != nil || n <= 0 {
				cur = 0
			} else {
				cur = n
			}
		case 'n':
			if cur > 0 && want[filepath.Clean(ln[1:])] {
				pids = append(pids, cur)
			}
		}
	}
	return pids
}

// memberWorkdirForSession derives the member's durable workdir from its session
// name (member-<id> → <home>/<id>, the same agentWorkdir the spawn used). "" when
// the name is not a member session or home is unknown — the sweep's workdir leg
// then no-ops rather than guessing a path.
func memberWorkdirForSession(home, session string) string {
	if home == "" || !isMemberSession(session) {
		return ""
	}
	return agentWorkdir(home, strings.TrimPrefix(session, memberSessionPrefix))
}

// ---------------------------------------------------------------------------
// stop — the SINGLE robust stop RPC surface (Seth-ruled), with a built-in
// escalation ladder. The server has already decided to stop this member (it emitted
// its own stand-down and owns the grace clock); the warden just runs the mechanism
// and re-asserts. It does NOT re-judge grace.
//
// Returns stopped: True ONLY on a positive ¬has_session AND a clean sweep (every
// snapshot pid verified dead). Fail-safe: a kill that did not take, a broken
// re-probe, or a sweep survivor returns False → the server's next tick re-issues
// stop rather than trusting a fake ok.
//
// noop (T-9adc): True ONLY when the stop was an idempotent NO-OP — the initial
// probe POSITIVELY found no session (probe ran, has_session false; a broken
// probe is never a no-op) AND the snapshot found no member process to sweep.
// stopped stays true (idempotent success, retry semantics unchanged), but the
// receipt carries the distinct no_such_session reason so the server's last_op
// fold can tell "I killed it" from "there was nothing here to kill" — an
// identity-sweep / mis-routed stop's polite OK must never forge a kill story
// onto a member whose live session (on ANOTHER warden) was never touched.
// ---------------------------------------------------------------------------

func stop(r CmdRunner, socket, session string, kill killFunc, getpgid pgidFunc, sw sweepSeams) (stopped, noop bool) {
	if !isMemberSession(session) && !isWorkerSession(session) {
		// NEVER kill a session outside the two warden-spawned namespaces —
		// member-<id> (members AND, since P5b, outsource workers) and the
		// LEGACY worker-<ow-id> residuals (retired namespace, admitted ONLY so
		// pre-P5b leftovers stay killable — the transition guard, worker.go).
		// The telemetry warden's own session, a bare prefix, or any foreign
		// tmux session is refused outright.
		return false, false
	}
	// no-op probe (T-9adc): POSITIVELY-absent BEFORE any kill. Conservative by
	// construction — nil (broken probe) or true both read "not a no-op".
	preHas := tmuxHasSession(r, socket, session)
	positivelyAbsent := preHas != nil && !*preHas
	// ⓪ SNAPSHOT the full footprint BEFORE any kill (links intact) — pane tree +
	// workdir-anchored ocagent listeners the pane's SIGHUP can never reach.
	snap := snapshotMemberPIDs(r, socket, session, sw)
	// ① LIGHTWEIGHT: kill-session (SIGHUP) + re-assert.
	killed := killSession(r, socket, session)
	if !killed {
		// ② FAILURE detected (session still present, or broken probe). ③ ESCALATE to
		// the irreversible pgroup hard-kill + orphan reap. ④ AUTHORITATIVE re-assert.
		escalateKill(r, socket, session, kill, getpgid)
		killed = killSession(r, socket, session)
	}
	// ⑤ SWEEP unconditionally — a took-on-① kill-session says nothing about the
	// detached `ocagent listen` (claude's own pgroup) or a previously-orphaned
	// zombie; both are in the snapshot and must be VERIFIED dead before we report
	// stopped. A sweep timeout is an honest partial → false.
	swept := sweepPIDs(snap, kill, sw.sleep)
	stopped = killed && swept
	// noop ONLY when nothing was ever here: no session before the kill, no
	// member process in the snapshot, and the ladder (vacuously) succeeded.
	noop = stopped && positivelyAbsent && len(snap) == 0
	return stopped, noop
}
