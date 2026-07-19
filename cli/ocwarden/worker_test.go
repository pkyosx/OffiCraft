package main

// worker_test.go — the LEGACY worker-session transition guard (A案 P5b: workers
// now ride the member verbs + member-<ow-id> sessions; the worker-* namespace
// survives kill-side only): naming / guards / the legacy worker_stop alias /
// the member-stop legacy sweep. Mirrors command_test.go's fake-seam style —
// NO real tmux / spawn / kill / network anywhere here.

import (
	"fmt"
	"strings"
	"syscall"
	"testing"
)

// ---------------------------------------------------------------------------
// session naming + guards — the two namespaces stay disjoint.
// ---------------------------------------------------------------------------

func TestWorkerSessionName_LowercasesAndPrefixes(t *testing.T) {
	if got := workerSessionName("OW-AB12"); got != "worker-ow-ab12" {
		t.Errorf("workerSessionName = %q, want worker-ow-ab12", got)
	}
}

func TestIsWorkerSession_Guard(t *testing.T) {
	cases := map[string]bool{
		"worker-ow-1":    true,
		"worker-":        false, // bare prefix refused
		"member-m-1":     false, // member namespace is NOT a worker
		"officraft":      false,
		"":               false,
		"workerish-ow-1": false,
	}
	for session, want := range cases {
		if got := isWorkerSession(session); got != want {
			t.Errorf("isWorkerSession(%q) = %v, want %v", session, got, want)
		}
	}
}

// countingRunner marks whether the stop ladder ever touched tmux — every call
// answers "positively absent" so an admitted session completes the ladder
// cleanly with zero subprocess.
type countingRunner struct{ calls int }

func (c *countingRunner) Run(name string, args ...string) (string, error) {
	c.calls++
	return "", fmt.Errorf("can't find session")
}

// The stop ladder's outermost gate admits BOTH warden-spawned namespaces —
// member-<id> (members AND post-P5b workers) plus the LEGACY worker-<ow-id>
// residuals — and nothing else. A refused session must return false without
// touching tmux. This is the P5b transition guard's kill half: a pre-P5b
// leftover must stay killable forever.
func TestStop_GuardAdmitsWorkerRefusesForeign(t *testing.T) {
	for _, tc := range []struct {
		session string
		want    bool // whether the guard lets the ladder RUN (probe the runner)
	}{
		{"worker-ow-9", true},
		{"member-m-9", true},
		{"member-ow-9", true}, // the post-P5b worker session shape
		{"worker-", false},
		{"officraft", false},
	} {
		r := &countingRunner{}
		stopped, _ := stop(r, "sock", tc.session, func(int, syscall.Signal) error { return nil },
			func(int) (int, error) { return 0, nil }, sweepSeams{})
		entered := r.calls > 0
		if entered != tc.want {
			t.Errorf("stop(%q): ladder entered = %v, want %v", tc.session, entered, tc.want)
		}
		if !tc.want && stopped {
			t.Errorf("stop(%q) = true for a refused session", tc.session)
		}
	}
}

func TestWorkerWorkdirForSession(t *testing.T) {
	if got := workerWorkdirForSession("/w", "worker-ow-1"); got != "/w/ow-1" {
		t.Errorf("workdir = %q, want /w/ow-1", got)
	}
	if got := workerWorkdirForSession("/w", "member-m-1"); got != "" {
		t.Errorf("member session must yield %q, got %q", "", got)
	}
	if got := workerWorkdirForSession("", "worker-ow-1"); got != "" {
		t.Errorf("empty home must yield \"\", got %q", got)
	}
}

func TestDefaultWorkerHome_SiblingOfAgents(t *testing.T) {
	// OC_AGENT_HOME set (namespaced instance) → workers is its sibling.
	env := func(k string) string {
		if k == "OC_AGENT_HOME" {
			return "/root/.officraft-x/agents"
		}
		return ""
	}
	if got := defaultWorkerHome(env); got != "/root/.officraft-x/workers" {
		t.Errorf("defaultWorkerHome = %q, want /root/.officraft-x/workers", got)
	}
	// Unset → <instance root>/workers, disjoint from <instance root>/agents.
	empty := func(string) string { return "" }
	got := defaultWorkerHome(empty)
	if !strings.HasSuffix(got, "/.officraft/workers") {
		t.Errorf("defaultWorkerHome = %q, want …/.officraft/workers", got)
	}
}

// ---------------------------------------------------------------------------
// parse — worker_start is RETIRED (unknown rpc); worker_stop stays a legacy
// alias on the same envelope.
// ---------------------------------------------------------------------------

// A retired worker_start from an old server must be refused as unknown-rpc
// (logged + skipped by the reader loop) — never spawn through a side door.
func TestParseCommandFrame_WorkerStartRetired(t *testing.T) {
	payload := []byte(`{"topic":"warden-command","data":{"rpc":"worker_start","args":{
		"worker_id":"ow-1","persona_context":"you are O-7","worker_token":"tok",
		"model":"opus","effort":"high"}}}`)
	cmd, err := parseCommandFrame(payload)
	if err == nil || cmd != nil {
		t.Fatalf("worker_start must be refused as unknown rpc, got cmd=%+v err=%v", cmd, err)
	}
}

func TestParseCommandFrame_WorkerStopEnvelope(t *testing.T) {
	payload := []byte(`{"topic":"warden-command","data":{"rpc":"worker_stop","args":{"worker_id":"ow-2"}}}`)
	cmd, err := parseCommandFrame(payload)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cmd == nil || cmd.RPC != rpcWorkerStop {
		t.Fatalf("cmd = %+v, want worker_stop", cmd)
	}
}

// ---------------------------------------------------------------------------
// dispatch — the legacy worker_stop alias rides the ONE Stop closure.
// ---------------------------------------------------------------------------

func TestDispatch_WorkerStop_DerivesExactLegacySession(t *testing.T) {
	var got []string
	deps := CommandDeps{
		Stop: func(session string) (bool, bool) { got = append(got, session); return true, false },
	}
	cmd := &Command{RPC: rpcWorkerStop, Args: map[string]any{"worker_id": "OW-9"}}
	if err := dispatchCommand(cmd, deps); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 || got[0] != "worker-ow-9" {
		t.Errorf("legacy worker_stop sessions = %v, want exactly [worker-ow-9]", got)
	}
}

func TestDispatch_WorkerStop_MissingID_Refused(t *testing.T) {
	called := false
	deps := CommandDeps{Stop: func(string) (bool, bool) { called = true; return true, false }}
	cmd := &Command{RPC: rpcWorkerStop, Args: map[string]any{}}
	if err := dispatchCommand(cmd, deps); err == nil {
		t.Fatal("worker_stop without worker_id must refuse")
	}
	if called {
		t.Error("Stop must not be called without a target")
	}
}

func TestDispatch_WorkerStop_IncompleteStop_ReturnsError(t *testing.T) {
	deps := CommandDeps{Stop: func(string) (bool, bool) { return false, false }}
	cmd := &Command{RPC: rpcWorkerStop, Args: map[string]any{"worker_id": "ow-1"}}
	if err := dispatchCommand(cmd, deps); err == nil {
		t.Fatal("an incomplete worker stop must surface as an error")
	}
}

// ---------------------------------------------------------------------------
// the member-stop LEGACY SWEEP (P5b transition guard, dispatch half): a plain
// member `stop` addressed by member_id must ALSO reap the derived legacy
// worker-<id> session, so a residual spawned by a pre-P5b build can never
// outlive the verb convergence.
// ---------------------------------------------------------------------------

func TestDispatch_Stop_SweepsLegacyWorkerSession(t *testing.T) {
	var got []string
	deps := CommandDeps{
		Stop: func(session string) (bool, bool) { got = append(got, session); return true, false },
	}
	cmd := &Command{RPC: rpcStop, Args: map[string]any{"member_id": "ow-7"}}
	if err := dispatchCommand(cmd, deps); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 || got[0] != "member-ow-7" || got[1] != "worker-ow-7" {
		t.Fatalf("stop sessions = %v, want [member-ow-7 worker-ow-7]", got)
	}
}

// The sweep is scoped to the ow- namespace (the only ids the retired worker-*
// sessions were minted for): a staff member stop, and a purely
// session-addressed stop, must never grow a second leg.
func TestDispatch_Stop_NoLegacySweepOutsideOwNamespace(t *testing.T) {
	for _, c := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"staff member id", map[string]any{"member_id": "m-1"}, "member-m-1"},
		{"session-addressed", map[string]any{"session_name": "member-m-1"}, "member-m-1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			deps := CommandDeps{
				Stop: func(session string) (bool, bool) { got = append(got, session); return true, false },
			}
			if err := dispatchCommand(&Command{RPC: rpcStop, Args: c.args}, deps); err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if len(got) != 1 || got[0] != c.want {
				t.Fatalf("stop sessions = %v, want exactly [%s]", got, c.want)
			}
		})
	}
}

// The receipt stays the PRIMARY member session's verdict — the legacy sweep is
// best-effort hygiene and must not flip a clean stop's receipt.
func TestDispatch_Stop_LegacySweepDoesNotAffectReceipt(t *testing.T) {
	var got CommandResult
	deps := CommandDeps{
		Stop: func(session string) (bool, bool) {
			return session == "member-ow-7", false // legacy leg reports failure
		},
		Report: func(cr CommandResult) error { got = cr; return nil },
	}
	cmd := &Command{RPC: rpcStop, Args: map[string]any{"member_id": "ow-7"}}
	if err := dispatchCommand(cmd, deps); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.MemberID != "ow-7" || got.RPC != rpcStop || !got.OK {
		t.Fatalf("receipt = %+v, want the primary session's ok=true", got)
	}
}

// ---------------------------------------------------------------------------
// legacy worker_stop command_result reporting (old server + new warden skew):
// the receipt stays keyed on worker_id so the old server's fold keeps working.
// ---------------------------------------------------------------------------

func TestDispatch_WorkerStop_ReportsCommandResult(t *testing.T) {
	for _, c := range []struct {
		name   string
		stopOK bool
	}{
		{"stopped", true},
		{"incomplete", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var got CommandResult
			var reports int
			deps := CommandDeps{
				Stop:   func(string) (bool, bool) { return c.stopOK, false },
				Report: func(cr CommandResult) error { got = cr; reports++; return nil },
			}
			cmd := &Command{RPC: rpcWorkerStop, Args: map[string]any{"worker_id": "ow-9"}}
			err := dispatchCommand(cmd, deps)
			if c.stopOK && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !c.stopOK && err == nil {
				t.Fatal("an incomplete worker stop must surface as an error")
			}
			if reports != 1 {
				t.Fatalf("reports = %d, want 1", reports)
			}
			if got.WorkerID != "ow-9" || got.RPC != rpcWorkerStop || got.OK != c.stopOK {
				t.Fatalf("report = %+v, want worker ow-9 rpc worker_stop ok %v", got, c.stopOK)
			}
			if !strings.Contains(got.Log, "worker-ow-9") {
				t.Fatalf("stop report log must name the session, got %q", got.Log)
			}
		})
	}
}
