package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

// killCall is one (pid, signal) the kill seam was asked to send.
type killCall struct {
	pid int
	sig syscall.Signal
}

// killSpy records every signal the code under test sends and answers from an
// injected oracle, so the IRREVERSIBLE path is asserted without a real kill.
type killSpy struct {
	answer func(pid int, sig syscall.Signal) error
	calls  []killCall
}

func (k *killSpy) kill(pid int, sig syscall.Signal) error {
	k.calls = append(k.calls, killCall{pid, sig})
	if k.answer == nil {
		return nil
	}
	return k.answer(pid, sig)
}

// aliveKill answers "alive and ours" for the listed pids and ESRCH for the rest.
func aliveKill(alive ...int) func(int, syscall.Signal) error {
	set := map[int]bool{}
	for _, p := range alive {
		set[p] = true
	}
	return func(pid int, _ syscall.Signal) error {
		if set[pid] {
			return nil
		}
		return syscall.ESRCH
	}
}

func TestParseKillablePID(t *testing.T) {
	cases := []struct {
		in     string
		wantN  int
		wantOK bool
	}{
		{"48213", 48213, true},
		{"1", 1, true},
		{"", 0, false},
		{"0", 0, false},
		{"-1", 0, false},
		{"-48213", 0, false},
		{"48213\n", 0, false},
		{" 48213", 0, false},
		{"48a13", 0, false},
		{"1e5", 0, false},
	}
	for _, c := range cases {
		n, ok := parseKillablePID(c.in)
		if n != c.wantN || ok != c.wantOK {
			t.Errorf("parseKillablePID(%q) = (%d, %v), want (%d, %v)", c.in, n, ok, c.wantN, c.wantOK)
		}
	}
}

func TestKillSession(t *testing.T) {
	const (
		killCmd  = "tmux -L officraft kill-session -t member-m1"
		probeCmd = "tmux -L officraft has-session -t member-m1"
	)
	cases := []struct {
		name   string
		script map[string]wardenRun
		want   bool
	}{
		{"session positively gone after the kill", map[string]wardenRun{
			probeCmd: {err: errors.New("can't find session: member-m1")},
		}, true},
		{"kill-session's own failure is ignored when the re-probe says gone", map[string]wardenRun{
			killCmd:  {err: errors.New("can't find session: member-m1")},
			probeCmd: {err: errors.New("can't find session: member-m1")},
		}, true},
		{"session still present", map[string]wardenRun{probeCmd: {}}, false},
		{"broken re-probe is never an assumed-dead", map[string]wardenRun{
			probeCmd: {err: errors.New("exec: \"tmux\": executable file not found in $PATH")},
		}, false},
	}
	for _, c := range cases {
		r := &wardenRunner{script: c.script}
		got := killSession(r, "officraft", "member-m1")
		if got != c.want {
			t.Errorf("%s: killSession = %v, want %v", c.name, got, c.want)
		}
		if want := []string{killCmd, probeCmd}; !reflect.DeepEqual(r.calls, want) {
			t.Errorf("%s: calls = %v, want %v", c.name, r.calls, want)
		}
	}
}

func TestDescendantPIDs(t *testing.T) {
	const psCmd = "ps -eo pid=,ppid="
	tree := "  100     1\n  200   100\n  300   200\n  400     1\n  500   300\n  600   400\n"

	r := &wardenRunner{script: map[string]wardenRun{psCmd: {out: tree}}}
	got := descendantPIDs(r, 100)
	if want := []int{200, 300, 500}; !reflect.DeepEqual(got, want) {
		t.Errorf("descendants of 100 = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(r.calls, []string{psCmd}) {
		t.Errorf("calls = %v, want %v", r.calls, []string{psCmd})
	}

	if got := descendantPIDs(&wardenRunner{script: map[string]wardenRun{psCmd: {out: tree}}}, 600); got != nil {
		t.Errorf("a leaf has no descendants, got %v", got)
	}
	if got := descendantPIDs(&wardenRunner{script: map[string]wardenRun{psCmd: {out: tree}}}, 999); got != nil {
		t.Errorf("an unknown root has no descendants, got %v", got)
	}

	selfParent := &wardenRunner{script: map[string]wardenRun{psCmd: {out: "100 100\n200 100\n"}}}
	if got := descendantPIDs(selfParent, 100); !reflect.DeepEqual(got, []int{200}) {
		t.Errorf("root must never be emitted as its own descendant, got %v", got)
	}

	cycle := &wardenRunner{script: map[string]wardenRun{psCmd: {out: "200 100\n100 200\n"}}}
	if got := descendantPIDs(cycle, 100); !reflect.DeepEqual(got, []int{200}) {
		t.Errorf("a parent cycle must terminate with [200], got %v", got)
	}

	junk := &wardenRunner{script: map[string]wardenRun{psCmd: {
		out: "PID PPID\n\n  x  100\n  200 100\n  -3 100\n  300 -1\n  400 100 extra\n",
	}}}
	if got := descendantPIDs(junk, 100); !reflect.DeepEqual(got, []int{200}) {
		t.Errorf("malformed ps lines must be skipped, got %v", got)
	}

	broken := &wardenRunner{fallback: wardenRun{err: errors.New("exec: \"ps\": executable file not found")}}
	if got := descendantPIDs(broken, 100); got != nil {
		t.Errorf("a broken ps reads nil, got %v", got)
	}
}

func TestEscalateKill(t *testing.T) {
	const (
		paneCmd = "tmux -L officraft display-message -p -t member-m1 #{pane_pid}"
		psCmd   = "ps -eo pid=,ppid="
	)
	live := map[string]wardenRun{paneCmd: {out: "500"}, psCmd: {out: "600 500\n700 600\n800 1\n"}}

	t.Run("pgroup leader is killed as a group, then every descendant", func(t *testing.T) {
		r := &wardenRunner{script: live}
		k := &killSpy{}
		escalateKill(r, "officraft", "member-m1", k.kill, func(pid int) (int, error) { return pid, nil })
		want := []killCall{
			{500, syscall.Signal(0)},
			{500, syscall.Signal(0)},
			{-500, syscall.SIGKILL},
			{600, syscall.Signal(0)}, {600, syscall.SIGKILL},
			{700, syscall.Signal(0)}, {700, syscall.SIGKILL},
		}
		if !reflect.DeepEqual(k.calls, want) {
			t.Errorf("kills = %v, want %v", k.calls, want)
		}
		if wantCalls := []string{paneCmd, psCmd}; !reflect.DeepEqual(r.calls, wantCalls) {
			t.Errorf("calls = %v, want %v", r.calls, wantCalls)
		}
	})

	t.Run("a non-leader pane pid is killed alone, never as a group", func(t *testing.T) {
		k := &killSpy{}
		escalateKill(&wardenRunner{script: live}, "officraft", "member-m1", k.kill,
			func(int) (int, error) { return 400, nil })
		want := []killCall{
			{500, syscall.Signal(0)},
			{500, syscall.SIGKILL},
			{600, syscall.Signal(0)}, {600, syscall.SIGKILL},
			{700, syscall.Signal(0)}, {700, syscall.SIGKILL},
		}
		if !reflect.DeepEqual(k.calls, want) {
			t.Errorf("kills = %v, want %v", k.calls, want)
		}
	})

	t.Run("an unreadable pgid kills the single verified pid", func(t *testing.T) {
		k := &killSpy{}
		escalateKill(&wardenRunner{script: live}, "officraft", "member-m1", k.kill,
			func(int) (int, error) { return 0, syscall.ESRCH })
		want := []killCall{
			{500, syscall.Signal(0)},
			{500, syscall.SIGKILL},
			{600, syscall.Signal(0)}, {600, syscall.SIGKILL},
			{700, syscall.Signal(0)}, {700, syscall.SIGKILL},
		}
		if !reflect.DeepEqual(k.calls, want) {
			t.Errorf("kills = %v, want %v", k.calls, want)
		}
	})

	t.Run("a descendant that vanished between snapshot and kill is skipped", func(t *testing.T) {
		k := &killSpy{answer: aliveKill(500, 700)}
		escalateKill(&wardenRunner{script: live}, "officraft", "member-m1", k.kill,
			func(pid int) (int, error) { return pid, nil })
		want := []killCall{
			{500, syscall.Signal(0)},
			{500, syscall.Signal(0)},
			{-500, syscall.SIGKILL},
			{600, syscall.Signal(0)},
			{700, syscall.Signal(0)}, {700, syscall.SIGKILL},
		}
		if !reflect.DeepEqual(k.calls, want) {
			t.Errorf("kills = %v, want %v", k.calls, want)
		}
	})

	t.Run("a pid that is not ours or already gone is never killed", func(t *testing.T) {
		r := &wardenRunner{script: live}
		k := &killSpy{answer: func(int, syscall.Signal) error { return syscall.EPERM }}
		escalateKill(r, "officraft", "member-m1", k.kill, func(pid int) (int, error) { return pid, nil })
		if want := []killCall{{500, syscall.Signal(0)}}; !reflect.DeepEqual(k.calls, want) {
			t.Errorf("kills = %v, want %v", k.calls, want)
		}
		if !reflect.DeepEqual(r.calls, []string{paneCmd}) {
			t.Errorf("the process tree must not even be walked, calls = %v", r.calls)
		}
	})

	t.Run("no live pane pid escalates nothing", func(t *testing.T) {
		r := &wardenRunner{script: map[string]wardenRun{paneCmd: {out: ""}}}
		k := &killSpy{}
		escalateKill(r, "officraft", "member-m1", k.kill, func(pid int) (int, error) { return pid, nil })
		if len(k.calls) != 0 {
			t.Errorf("kills = %v, want none", k.calls)
		}
		if !reflect.DeepEqual(r.calls, []string{paneCmd}) {
			t.Errorf("calls = %v, want %v", r.calls, []string{paneCmd})
		}
	})
}

func TestSnapshotMemberPIDs(t *testing.T) {
	const (
		paneCmd = "tmux -L officraft display-message -p -t member-m1 #{pane_pid}"
		psCmd   = "ps -eo pid=,ppid="
	)
	self := os.Getpid()

	r := &wardenRunner{script: map[string]wardenRun{paneCmd: {out: "500"}, psCmd: {out: "600 500\n700 600\n"}}}
	got := snapshotMemberPIDs(r, "officraft", "member-m1", sweepSeams{
		workdir:    "/Users/eva/.officraft/agents/m1",
		listenPIDs: func(string) []int { return []int{900, 600, 1, self} },
	})
	if want := []int{500, 600, 700, 900}; !reflect.DeepEqual(got, want) {
		t.Errorf("snapshot = %v, want %v (deduped, no init, never the warden itself)", got, want)
	}

	workdirs := []string{}
	got = snapshotMemberPIDs(
		&wardenRunner{script: map[string]wardenRun{paneCmd: {out: ""}}},
		"officraft", "member-m1", sweepSeams{
			workdir:    "/Users/eva/.officraft/agents/m1",
			listenPIDs: func(w string) []int { workdirs = append(workdirs, w); return []int{900} },
		})
	if want := []int{900}; !reflect.DeepEqual(got, want) {
		t.Errorf("with no pane pid the snapshot is the listener leg only, got %v want %v", got, want)
	}
	if want := []string{"/Users/eva/.officraft/agents/m1"}; !reflect.DeepEqual(workdirs, want) {
		t.Errorf("listenPIDs asked for %v, want %v", workdirs, want)
	}

	paneOnly := &wardenRunner{script: map[string]wardenRun{paneCmd: {out: "500"}, psCmd: {out: "600 500\n"}}}
	got = snapshotMemberPIDs(paneOnly, "officraft", "member-m1", sweepSeams{
		workdir:    "",
		listenPIDs: func(string) []int { t.Error("an empty workdir must disable the listener leg"); return nil },
	})
	if want := []int{500, 600}; !reflect.DeepEqual(got, want) {
		t.Errorf("snapshot = %v, want %v", got, want)
	}

	got = snapshotMemberPIDs(paneOnly, "officraft", "member-m1", sweepSeams{workdir: "/w"})
	if want := []int{500, 600}; !reflect.DeepEqual(got, want) {
		t.Errorf("a nil listener seam still snapshots the pane tree, got %v want %v", got, want)
	}

	empty := &wardenRunner{script: map[string]wardenRun{paneCmd: {out: ""}}}
	if got := snapshotMemberPIDs(empty, "officraft", "member-m1", sweepSeams{}); got != nil {
		t.Errorf("nothing to snapshot must be nil, got %v", got)
	}
}

func TestLivePIDs(t *testing.T) {
	k := &killSpy{answer: func(pid int, _ syscall.Signal) error {
		switch pid {
		case 500:
			return nil
		case 600:
			return syscall.ESRCH
		default:
			return syscall.EPERM
		}
	}}
	got := livePIDs([]int{500, 600, 700}, k.kill)
	if want := []int{500}; !reflect.DeepEqual(got, want) {
		t.Errorf("livePIDs = %v, want %v (gone and not-ours are both dropped)", got, want)
	}
	want := []killCall{{500, syscall.Signal(0)}, {600, syscall.Signal(0)}, {700, syscall.Signal(0)}}
	if !reflect.DeepEqual(k.calls, want) {
		t.Errorf("probes = %v, want %v (signal 0 only — nothing is killed)", k.calls, want)
	}
	if got := livePIDs(nil, k.kill); got != nil {
		t.Errorf("livePIDs(nil) = %v, want nil", got)
	}
}

func TestSweepPIDs(t *testing.T) {
	t.Run("an empty snapshot is clean without waiting", func(t *testing.T) {
		k := &killSpy{}
		var slept []time.Duration
		if !sweepPIDs(nil, k.kill, func(d time.Duration) { slept = append(slept, d) }) {
			t.Error("sweepPIDs(nil) = false, want true")
		}
		if len(k.calls) != 0 || len(slept) != 0 {
			t.Errorf("kills = %v, sleeps = %v, want none of either", k.calls, slept)
		}
	})

	t.Run("everything already dead is clean without a signal", func(t *testing.T) {
		k := &killSpy{answer: aliveKill()}
		var slept []time.Duration
		if !sweepPIDs([]int{500, 600}, k.kill, func(d time.Duration) { slept = append(slept, d) }) {
			t.Error("sweepPIDs = false, want true")
		}
		want := []killCall{{500, syscall.Signal(0)}, {600, syscall.Signal(0)}}
		if !reflect.DeepEqual(k.calls, want) {
			t.Errorf("kills = %v, want %v", k.calls, want)
		}
		if len(slept) != 0 {
			t.Errorf("sleeps = %v, want none", slept)
		}
	})

	t.Run("a pid that dies on SIGTERM is clean after one grace poll", func(t *testing.T) {
		dead := false
		k := &killSpy{answer: func(pid int, sig syscall.Signal) error {
			if sig == syscall.SIGTERM {
				dead = true
				return nil
			}
			if dead {
				return syscall.ESRCH
			}
			return nil
		}}
		var slept []time.Duration
		if !sweepPIDs([]int{500}, k.kill, func(d time.Duration) { slept = append(slept, d) }) {
			t.Error("sweepPIDs = false, want true")
		}
		want := []killCall{{500, syscall.Signal(0)}, {500, syscall.SIGTERM}, {500, syscall.Signal(0)}}
		if !reflect.DeepEqual(k.calls, want) {
			t.Errorf("kills = %v, want %v", k.calls, want)
		}
		if want := []time.Duration{200 * time.Millisecond}; !reflect.DeepEqual(slept, want) {
			t.Errorf("sleeps = %v, want %v", slept, want)
		}
	})

	t.Run("a SIGTERM-deaf pid is SIGKILLed after the 3s grace window", func(t *testing.T) {
		killed := false
		k := &killSpy{answer: func(pid int, sig syscall.Signal) error {
			if sig == syscall.SIGKILL {
				killed = true
			}
			if killed && sig == syscall.Signal(0) {
				return syscall.ESRCH
			}
			return nil
		}}
		var slept []time.Duration
		if !sweepPIDs([]int{500}, k.kill, func(d time.Duration) { slept = append(slept, d) }) {
			t.Error("sweepPIDs = false, want true")
		}
		var want []killCall
		want = append(want, killCall{500, syscall.Signal(0)}, killCall{500, syscall.SIGTERM})
		for i := 0; i < 15; i++ {
			want = append(want, killCall{500, syscall.Signal(0)})
		}
		want = append(want, killCall{500, syscall.SIGKILL}, killCall{500, syscall.Signal(0)})
		if !reflect.DeepEqual(k.calls, want) {
			t.Errorf("kills = %v, want %v", k.calls, want)
		}
		if len(slept) != 16 {
			t.Errorf("slept %d times, want 16 (15 grace polls + 1 post-SIGKILL poll)", len(slept))
		}
		for i, d := range slept {
			if d != 200*time.Millisecond {
				t.Fatalf("sleep %d = %v, want 200ms", i, d)
			}
		}
	})

	t.Run("an unkillable survivor is an honest partial", func(t *testing.T) {
		k := &killSpy{answer: aliveKill(500, 600)}
		var slept []time.Duration
		if sweepPIDs([]int{500, 600}, k.kill, func(d time.Duration) { slept = append(slept, d) }) {
			t.Error("sweepPIDs = true, want false")
		}
		terms, kills, probes := 0, 0, 0
		for _, c := range k.calls {
			switch c.sig {
			case syscall.SIGTERM:
				terms++
			case syscall.SIGKILL:
				kills++
			default:
				probes++
			}
		}
		if terms != 2 || kills != 2 || probes != 2+2*25 {
			t.Errorf("terms=%d kills=%d probes=%d, want 2/2/52", terms, kills, probes)
		}
		if len(slept) != 25 {
			t.Errorf("slept %d times, want 25 (15 grace + 10 confirmation polls)", len(slept))
		}
	})

	t.Run("only the survivors are re-signalled", func(t *testing.T) {
		k := &killSpy{answer: aliveKill(500, 600)}
		alive := map[int]bool{500: true, 600: true}
		k.answer = func(pid int, sig syscall.Signal) error {
			if sig == syscall.SIGTERM && pid == 600 {
				alive[600] = false
			}
			if !alive[pid] {
				return syscall.ESRCH
			}
			return nil
		}
		sweepPIDs([]int{500, 600}, k.kill, func(time.Duration) {})
		var hard []killCall
		for _, c := range k.calls {
			if c.sig == syscall.SIGKILL {
				hard = append(hard, c)
			}
		}
		if want := []killCall{{500, syscall.SIGKILL}}; !reflect.DeepEqual(hard, want) {
			t.Errorf("SIGKILLs = %v, want %v (the pid the grace poll proved dead is never re-signalled)", hard, want)
		}
	})
}

func TestOcagentPIDsByCwd(t *testing.T) {
	const lsofCmd = "lsof -a -c ocagent -d cwd -F pn"
	out := "p101\nfcwd\nn/Users/eva/.officraft/agents/m1\np102\nn/Users/eva/.officraft/agents/m2\np103\nn/Users/eva/.officraft/agents/m1\n"

	r := &wardenRunner{script: map[string]wardenRun{lsofCmd: {out: out}}}
	got := ocagentPIDsByCwd(r, "/Users/eva/.officraft/agents/m1")
	if want := []int{101, 103}; !reflect.DeepEqual(got, want) {
		t.Errorf("pids = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(r.calls, []string{lsofCmd}) {
		t.Errorf("calls = %v, want %v", r.calls, []string{lsofCmd})
	}

	if got := ocagentPIDsByCwd(&wardenRunner{script: map[string]wardenRun{lsofCmd: {out: out}}},
		"/Users/eva/.officraft/agents/m1/"); !reflect.DeepEqual(got, []int{101, 103}) {
		t.Errorf("a trailing slash must still match, got %v", got)
	}

	if got := ocagentPIDsByCwd(&wardenRunner{script: map[string]wardenRun{lsofCmd: {out: out}}},
		"/Users/eva/.officraft/agents/m9"); got != nil {
		t.Errorf("another member's workdir matches nothing, got %v", got)
	}

	if got := ocagentPIDsByCwd(&wardenRunner{fallback: wardenRun{err: errors.New("exit status 1")}},
		"/Users/eva/.officraft/agents/m1"); got != nil {
		t.Errorf("a failed lsof reads nil, got %v", got)
	}

	orphan := &wardenRunner{script: map[string]wardenRun{lsofCmd: {
		out: "pX\nn/Users/eva/.officraft/agents/m1\np0\nn/Users/eva/.officraft/agents/m1\n",
	}}}
	if got := ocagentPIDsByCwd(orphan, "/Users/eva/.officraft/agents/m1"); got != nil {
		t.Errorf("a cwd line with no valid pid before it is dropped, got %v", got)
	}

	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("evalsymlinks: %v", err)
	}
	sym := &wardenRunner{script: map[string]wardenRun{lsofCmd: {out: "p104\nn" + resolved + "\n"}}}
	if got := ocagentPIDsByCwd(sym, link); !reflect.DeepEqual(got, []int{104}) {
		t.Errorf("the kernel-resolved cwd must match the symlinked workdir, got %v", got)
	}
}

func TestMemberWorkdirForSession(t *testing.T) {
	cases := []struct {
		home    string
		session string
		want    string
	}{
		{"/Users/eva/.officraft/agents", "member-m1", "/Users/eva/.officraft/agents/m1"},
		{"/Users/eva/.officraft/agents", "member-OW-78173E", "/Users/eva/.officraft/agents/ow-78173e"},
		{"/Users/eva/.officraft/agents", "worker-ow-78173e", ""},
		{"/Users/eva/.officraft/agents", "member-", ""},
		{"/Users/eva/.officraft/agents", "", ""},
		{"", "member-m1", ""},
	}
	for _, c := range cases {
		if got := memberWorkdirForSession(c.home, c.session); got != c.want {
			t.Errorf("memberWorkdirForSession(%q, %q) = %q, want %q", c.home, c.session, got, c.want)
		}
	}
}

func TestStop(t *testing.T) {
	const (
		paneCmd = "tmux -L officraft display-message -p -t member-m1 #{pane_pid}"
		psCmd   = "ps -eo pid=,ppid="
	)
	probeOf := func(session string) string { return "tmux -L officraft has-session -t " + session }
	killOf := func(session string) string { return "tmux -L officraft kill-session -t " + session }
	gone := wardenRun{err: errors.New("can't find session: member-m1")}

	t.Run("a session outside the two warden namespaces is refused untouched", func(t *testing.T) {
		for _, session := range []string{"com.officraft.ocwarden", "member-", "worker-", "", "scratch"} {
			r := &wardenRunner{}
			k := &killSpy{}
			purged := 0
			stopped, noop := stop(r, "officraft", session, k.kill,
				func(pid int) (int, error) { return pid, nil },
				sweepSeams{purgeTrash: func() { purged++ }, sleep: func(time.Duration) {}})
			if stopped || noop {
				t.Errorf("%q: got (stopped=%v, noop=%v), want (false, false)", session, stopped, noop)
			}
			if len(r.calls) != 0 || len(k.calls) != 0 || purged != 0 {
				t.Errorf("%q: refused stop still did something: calls=%v kills=%v purges=%d",
					session, r.calls, k.calls, purged)
			}
		}
	})

	t.Run("an absent session with no member process is an idempotent no-op", func(t *testing.T) {
		r := &wardenRunner{script: map[string]wardenRun{
			probeOf("member-m1"): gone,
			paneCmd:              gone,
		}}
		k := &killSpy{}
		purged := 0
		stopped, noop := stop(r, "officraft", "member-m1", k.kill,
			func(pid int) (int, error) { return pid, nil },
			sweepSeams{purgeTrash: func() { purged++ }, sleep: func(time.Duration) {}})
		if !stopped || !noop {
			t.Errorf("got (stopped=%v, noop=%v), want (true, true)", stopped, noop)
		}
		want := []string{probeOf("member-m1"), paneCmd, killOf("member-m1"), probeOf("member-m1")}
		if !reflect.DeepEqual(r.calls, want) {
			t.Errorf("calls = %v, want %v", r.calls, want)
		}
		if len(k.calls) != 0 {
			t.Errorf("kills = %v, want none", k.calls)
		}
		if purged != 1 {
			t.Errorf("purgeTrash ran %d times, want 1", purged)
		}
	})

	t.Run("a live session killed by SIGHUP is stopped but not a no-op", func(t *testing.T) {
		probe := 0
		r := &wardenRunner{script: map[string]wardenRun{paneCmd: {out: "500"}, psCmd: {out: "600 500\n"}}}
		seq := &sequencedRunner{inner: r, key: probeOf("member-m1"),
			answers: []wardenRun{{}, gone}, seen: &probe}

		k := &killSpy{answer: aliveKill()}
		purged := 0
		stopped, noop := stop(seq, "officraft", "member-m1", k.kill,
			func(pid int) (int, error) { return pid, nil },
			sweepSeams{purgeTrash: func() { purged++ }, sleep: func(time.Duration) {}})
		if !stopped || noop {
			t.Errorf("got (stopped=%v, noop=%v), want (true, false)", stopped, noop)
		}
		want := []string{probeOf("member-m1"), paneCmd, psCmd, killOf("member-m1"), probeOf("member-m1")}
		if !reflect.DeepEqual(r.calls, want) {
			t.Errorf("calls = %v, want %v", r.calls, want)
		}
		wantKills := []killCall{{500, syscall.Signal(0)}, {600, syscall.Signal(0)}}
		if !reflect.DeepEqual(k.calls, wantKills) {
			t.Errorf("kills = %v, want %v (sweep probes only — everything died with the session)", k.calls, wantKills)
		}
		if purged != 1 {
			t.Errorf("purgeTrash ran %d times, want 1", purged)
		}
	})

	t.Run("a SIGHUP-deaf session escalates to the pgroup kill and re-asserts", func(t *testing.T) {
		probe := 0
		inner := &wardenRunner{script: map[string]wardenRun{paneCmd: {out: "500"}, psCmd: {out: "600 500\n"}}}
		seq := &sequencedRunner{inner: inner, key: probeOf("member-m1"),
			answers: []wardenRun{{}, {}, gone}, seen: &probe}
		k := &killSpy{answer: aliveKill(500, 600)}
		var slept []time.Duration
		stopped, noop := stop(seq, "officraft", "member-m1", k.kill,
			func(pid int) (int, error) { return pid, nil },
			sweepSeams{sleep: func(d time.Duration) { slept = append(slept, d) }})
		if stopped || noop {
			t.Errorf("got (stopped=%v, noop=%v), want (false, false) — the sweep survivors are still alive",
				stopped, noop)
		}
		want := []string{
			probeOf("member-m1"), paneCmd, psCmd,
			killOf("member-m1"), probeOf("member-m1"),
			paneCmd, psCmd,
			killOf("member-m1"), probeOf("member-m1"),
		}
		if !reflect.DeepEqual(inner.calls, want) {
			t.Errorf("calls = %v, want %v", inner.calls, want)
		}
		escalation := []killCall{
			{500, syscall.Signal(0)}, {500, syscall.Signal(0)}, {-500, syscall.SIGKILL},
			{600, syscall.Signal(0)}, {600, syscall.SIGKILL},
		}
		if !reflect.DeepEqual(k.calls[:len(escalation)], escalation) {
			t.Errorf("escalation kills = %v, want %v", k.calls[:len(escalation)], escalation)
		}
		if len(slept) != 25 {
			t.Errorf("slept %d times, want 25", len(slept))
		}
	})

	t.Run("a legacy worker session is admitted by the guard", func(t *testing.T) {
		r := &wardenRunner{script: map[string]wardenRun{
			"tmux -L officraft has-session -t worker-ow-1":                    {err: errors.New("can't find session: worker-ow-1")},
			"tmux -L officraft display-message -p -t worker-ow-1 #{pane_pid}": {err: errors.New("can't find session")},
		}}
		stopped, noop := stop(r, "officraft", "worker-ow-1", (&killSpy{}).kill,
			func(pid int) (int, error) { return pid, nil }, sweepSeams{sleep: func(time.Duration) {}})
		if !stopped || !noop {
			t.Errorf("got (stopped=%v, noop=%v), want (true, true)", stopped, noop)
		}
	})

	t.Run("a broken probe is never a no-op", func(t *testing.T) {
		broken := wardenRun{err: errors.New("exec: \"tmux\": executable file not found in $PATH")}
		r := &wardenRunner{fallback: broken}
		stopped, noop := stop(r, "officraft", "member-m1", (&killSpy{}).kill,
			func(pid int) (int, error) { return pid, nil }, sweepSeams{sleep: func(time.Duration) {}})
		if stopped || noop {
			t.Errorf("got (stopped=%v, noop=%v), want (false, false)", stopped, noop)
		}
	})

	t.Run("a lingering workdir listener keeps the stop from claiming a no-op", func(t *testing.T) {
		r := &wardenRunner{script: map[string]wardenRun{probeOf("member-m1"): gone, paneCmd: gone}}
		k := &killSpy{answer: aliveKill()}
		stopped, noop := stop(r, "officraft", "member-m1", k.kill,
			func(pid int) (int, error) { return pid, nil },
			sweepSeams{
				workdir:    "/Users/eva/.officraft/agents/m1",
				listenPIDs: func(string) []int { return []int{900} },
				sleep:      func(time.Duration) {},
			})
		if !stopped || noop {
			t.Errorf("got (stopped=%v, noop=%v), want (true, false)", stopped, noop)
		}
		if want := []killCall{{900, syscall.Signal(0)}}; !reflect.DeepEqual(k.calls, want) {
			t.Errorf("kills = %v, want %v", k.calls, want)
		}
	})
}

// sequencedRunner answers one argv key from a fixed sequence (so a session can
// be present on the first probe and gone on the next) and delegates everything
// else — including the call recording — to inner.
type sequencedRunner struct {
	inner   *wardenRunner
	key     string
	answers []wardenRun
	seen    *int
}

func (s *sequencedRunner) Run(name string, args ...string) (string, error) {
	out, err := s.inner.Run(name, args...)
	if s.inner.calls[len(s.inner.calls)-1] != s.key {
		return out, err
	}
	i := *s.seen
	*s.seen++
	if i >= len(s.answers) {
		i = len(s.answers) - 1
	}
	return s.answers[i].out, s.answers[i].err
}
