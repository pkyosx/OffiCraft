// Stop executor. The server decides WHEN to stop (grace, the stopping timeout,
// reconcile); do not port those judgements here. Owner ruling (Seth): one `stop`
// with a built-in escalation ladder, not separate stop/force_kill RPCs.
//
// Async contract (wade-ruled): the bool stop returns is only a local signal; the
// server's authoritative death verdict is the session dropping out of the next
// presence report. stopped=false makes the server re-issue stop.
//
// A tmux pane pid is its process-group leader (forkpty+setsid, verified
// empirically), so killpg reaps it and its non-detached children; the tree walk
// catches descendants that setsid/double-forked into another group.
//
// Never a pattern kill (pkill/killall): the host runs unrelated services.
package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type killFunc func(pid int, sig syscall.Signal) error

func realKill(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }

type pgidFunc func(pid int) (int, error)

func realGetpgid(pid int) (int, error) { return syscall.Getpgid(pid) }

func isMemberSession(session string) bool {
	return strings.HasPrefix(session, memberSessionPrefix) && len(session) > len(memberSessionPrefix)
}

// pid 0 or a negative pid would signal a whole process group.
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

func killSession(r CmdRunner, socket, session string) bool {
	_, _ = r.Run("tmux", "-L", socket, "kill-session", "-t", session)
	has := tmuxHasSession(r, socket, session)
	return has != nil && !*has
}

// descendantPIDs must run BEFORE any kill: afterwards orphans reparent to init
// and the parent→child links are gone.
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
	seen := map[int]bool{root: true}
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

func escalateKill(r CmdRunner, socket, session string, kill killFunc, getpgid pgidFunc) {
	n, ok := parseKillablePID(tmuxPanePID(r, socket, session))
	if !ok {
		return
	}
	// Signal-0 probe before every SIGKILL: ESRCH means the pid may since have been
	// reused, EPERM means another uid's process — neither is our member.
	if kill(n, syscall.Signal(0)) != nil {
		return
	}
	descs := descendantPIDs(r, n)
	if pgid, err := getpgid(n); err == nil && pgid == n {
		// Re-verify right before the group kill to narrow the pid-reuse window
		// (not race-free without pidfd; killpg has the widest blast radius).
		if kill(n, syscall.Signal(0)) == nil {
			_ = kill(-n, syscall.SIGKILL)
		}
	} else {
		_ = kill(n, syscall.SIGKILL)
	}
	for _, d := range descs {
		if kill(d, syscall.Signal(0)) == nil {
			_ = kill(d, syscall.SIGKILL)
		}
	}
}

const (
	sweepPollInterval = 200 * time.Millisecond
	sweepTermPolls    = 15
	sweepKillPolls    = 10
)

type sweepSeams struct {
	// production: ocagentPIDsByCwd
	listenPIDs func(workdir string) []int
	workdir    string
	sleep      func(time.Duration)
	// purgeTrash reaps <workdir>/trash; bound by the transport wiring because only
	// it knows the root (agents/ vs legacy workers/). It runs even on a partial
	// stop, so it CAN race a still-alive process — ordering is not a mutual-
	// exclusion guarantee (the spawn-side hook has the same hole). Accepted: the
	// target is only what the agent already disowned, and a collision surfaces as
	// a loud RemoveAll error.
	purgeTrash func()
}

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

func livePIDs(pids []int, kill killFunc) []int {
	var alive []int
	for _, p := range pids {
		if kill(p, syscall.Signal(0)) == nil {
			alive = append(alive, p)
		}
	}
	return alive
}

// SIGTERM first: the ocagent listener traps it.
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

// Matching by cwd works because the spawn shim and the listener both cd into
// the member's own workdir before exec; other members' listeners never match.
func ocagentPIDsByCwd(r CmdRunner, workdir string) []int {
	out, err := r.Run("lsof", "-a", "-c", "ocagent", "-d", "cwd", "-F", "pn")
	if err != nil {
		return nil
	}
	// lsof reports the kernel-resolved cwd (macOS /var → /private/var).
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

func memberWorkdirForSession(home, session string) string {
	if home == "" || !isMemberSession(session) {
		return ""
	}
	return agentWorkdir(home, strings.TrimPrefix(session, memberSessionPrefix))
}

// noop: the session was positively absent before the kill and the snapshot was
// empty. The receipt then carries no_such_session so the server's last_op fold
// never records a kill story for a mis-routed stop whose live session (on
// another warden) was never touched.
func stop(r CmdRunner, socket, session string, kill killFunc, getpgid pgidFunc, sw sweepSeams) (stopped, noop bool) {
	if !isMemberSession(session) && !isWorkerSession(session) {
		// worker-<id> is a retired namespace, admitted only so legacy leftovers stay
		// killable; outsource workers now use member-<id>.
		return false, false
	}
	preHas := tmuxHasSession(r, socket, session)
	positivelyAbsent := preHas != nil && !*preHas
	// The workdir leg of the snapshot catches a paste-route listener (its own tmux
	// session, out of kill-session's reach) and zombies orphaned by an earlier failed stop,
	// which is why the sweep runs even when kill-session took.
	snap := snapshotMemberPIDs(r, socket, session, sw)
	killed := killSession(r, socket, session)
	if !killed {
		escalateKill(r, socket, session, kill, getpgid)
		killed = killSession(r, socket, session)
	}
	swept := sweepPIDs(snap, kill, sw.sleep)
	stopped = killed && swept
	noop = stopped && positivelyAbsent && len(snap) == 0
	if sw.purgeTrash != nil {
		sw.purgeTrash()
	}
	return stopped, noop
}
