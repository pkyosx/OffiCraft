package main

import (
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// parseCommandFrame — happy paths
// ---------------------------------------------------------------------------

func TestParseCommandFrame_StartEnvelope(t *testing.T) {
	payload := []byte(`{"topic":"warden-command","data":{"rpc":"start","args":{
		"member_id":"m-1","persona_context":"you are x","member_token":"tok",
		"role":"agent","task_type":"onboard","model":"opus","effort":"high","session_name":"member-m-1"}}}`)
	cmd, err := parseCommandFrame(payload)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cmd == nil {
		t.Fatal("cmd is nil, want a start command")
	}
	if cmd.RPC != rpcStart {
		t.Errorf("rpc = %q, want start", cmd.RPC)
	}
	if got, _ := argString(cmd.Args, "member_id"); got != "m-1" {
		t.Errorf("args[member_id] = %q, want m-1", got)
	}
}

func TestParseCommandFrame_StopEnvelope(t *testing.T) {
	payload := []byte(`{"topic":"warden-command","data":{"rpc":"stop","args":{
		"member_id":"m-2","session_name":"member-m-2"}}}`)
	cmd, err := parseCommandFrame(payload)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cmd == nil || cmd.RPC != rpcStop {
		t.Fatalf("cmd = %+v, want stop command", cmd)
	}
}

// A non-warden-command topic is a benign SKIP: (nil, nil), NOT an error.
func TestParseCommandFrame_SkipOtherTopics(t *testing.T) {
	for _, topic := range []string{"context-high", "chat", "keepalive", "heartbeat", ""} {
		payload := []byte(`{"topic":"` + topic + `","data":{"anything":true}}`)
		cmd, err := parseCommandFrame(payload)
		if err != nil {
			t.Errorf("topic %q: unexpected err %v", topic, err)
		}
		if cmd != nil {
			t.Errorf("topic %q: cmd = %+v, want nil (skip)", topic, cmd)
		}
	}
}

// ---------------------------------------------------------------------------
// parseCommandFrame — ADVERSARIAL: every malformed shape returns err, NEVER panics.
// ---------------------------------------------------------------------------

func TestParseCommandFrame_MalformedNeverPanics(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"empty", []byte(``)},
		{"nil", nil},
		{"truncated_json", []byte(`{"topic":"warden-command","data":{"rpc":"sta`)},
		{"not_json_at_all", []byte(`not json <<>>`)},
		{"payload_is_array", []byte(`[1,2,3]`)},
		{"payload_is_string", []byte(`"just a string"`)},
		{"warden_missing_data", []byte(`{"topic":"warden-command"}`)},
		{"warden_data_null", []byte(`{"topic":"warden-command","data":null}`)},
		{"warden_data_is_string", []byte(`{"topic":"warden-command","data":"nope"}`)},
		{"warden_data_is_number", []byte(`{"topic":"warden-command","data":42}`)},
		{"missing_rpc", []byte(`{"topic":"warden-command","data":{"args":{}}}`)},
		{"rpc_wrong_type", []byte(`{"topic":"warden-command","data":{"rpc":123,"args":{}}}`)},
		{"unknown_rpc", []byte(`{"topic":"warden-command","data":{"rpc":"restart","args":{}}}`)},
		{"missing_args", []byte(`{"topic":"warden-command","data":{"rpc":"start"}}`)},
		{"args_null", []byte(`{"topic":"warden-command","data":{"rpc":"start","args":null}}`)},
		{"args_wrong_type", []byte(`{"topic":"warden-command","data":{"rpc":"start","args":"x"}}`)},
		{"args_is_array", []byte(`{"topic":"warden-command","data":{"rpc":"stop","args":[1]}}`)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("PANIC on malformed frame %q: %v", c.name, r)
				}
			}()
			cmd, err := parseCommandFrame(c.payload)
			if err == nil {
				t.Fatalf("want err for malformed %q, got cmd=%+v", c.name, cmd)
			}
			if cmd != nil {
				t.Fatalf("want nil cmd for malformed %q, got %+v", c.name, cmd)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// dispatchCommand — start
// ---------------------------------------------------------------------------

// fullStartArgs is a complete, valid full-field start args map.
func fullStartArgs() map[string]any {
	return map[string]any{
		"member_id":       "m-1",
		"persona_context": "you are x",
		"member_token":    "tok-abc",
		"role":            "agent",
		"task_type":       "onboard",
		"model":           "opus",
		"effort":          "high",
		"session_name":    "member-m-1",
	}
}

func TestDispatch_Start_CallsSpawnWithAll7Fields(t *testing.T) {
	var gotParams StartParams
	var spawnCalls int
	deps := CommandDeps{
		Spawn: func(p StartParams) SpawnOutcome {
			gotParams = p
			spawnCalls++
			return SpawnOutcome{OK: true, SessionID: "member-m-1", PID: "111"}
		},
		Stop: func(string) bool { t.Fatal("stop must not be called on start"); return false },
	}
	err := dispatchCommand(&Command{RPC: rpcStart, Args: fullStartArgs()}, deps)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if spawnCalls != 1 {
		t.Fatalf("spawn calls = %d, want 1", spawnCalls)
	}
	want := StartParams{
		MemberID: "m-1", PersonaContext: "you are x", MemberToken: "tok-abc",
		Role: "agent", TaskType: "onboard", Model: "opus", Effort: "high",
		SessionName: "member-m-1",
	}
	if gotParams != want {
		t.Fatalf("StartParams = %+v, want %+v", gotParams, want)
	}
}

func TestDispatch_Start_MissingRequiredField_NoSpawn(t *testing.T) {
	// The 3 executor-hard-required fields: missing any → refuse the spawn entirely.
	for _, missing := range []string{"member_id", "persona_context", "member_token"} {
		t.Run("missing_"+missing, func(t *testing.T) {
			args := fullStartArgs()
			delete(args, missing)
			var spawnCalls int
			deps := CommandDeps{
				Spawn: func(StartParams) SpawnOutcome { spawnCalls++; return SpawnOutcome{} },
			}
			err := dispatchCommand(&Command{RPC: rpcStart, Args: args}, deps)
			if err == nil {
				t.Fatalf("want err when required %q missing", missing)
			}
			if spawnCalls != 0 {
				t.Fatalf("spawn called %d times, want 0 (no half-formed spawn)", spawnCalls)
			}
		})
	}
}

func TestDispatch_Start_MissingOptionalField_StillSpawns(t *testing.T) {
	// The 4 optional fields: missing one must NOT refuse — the executor defaults it and
	// the dispatcher must not be stricter than the executor. It is passed through EMPTY.
	for _, opt := range []string{"role", "task_type", "model", "effort", "session_name"} {
		t.Run("missing_"+opt, func(t *testing.T) {
			args := fullStartArgs()
			delete(args, opt)
			var gotParams StartParams
			var spawnCalls int
			deps := CommandDeps{
				Spawn: func(p StartParams) SpawnOutcome { gotParams = p; spawnCalls++; return SpawnOutcome{OK: true} },
			}
			if err := dispatchCommand(&Command{RPC: rpcStart, Args: args}, deps); err != nil {
				t.Fatalf("optional %q missing must NOT refuse: %v", opt, err)
			}
			if spawnCalls != 1 {
				t.Fatalf("want spawn=1, got spawn=%d", spawnCalls)
			}
			if gotParams.MemberID != "m-1" || gotParams.PersonaContext != "you are x" || gotParams.MemberToken != "tok-abc" {
				t.Fatalf("required fields not populated: %+v", gotParams)
			}
		})
	}
}

func TestDispatch_Start_RequiredBlankOrWrongTyped_NoSpawn(t *testing.T) {
	// A present-but-blank or present-but-wrong-typed REQUIRED field refuses.
	for _, mut := range []struct {
		name string
		key  string
		val  any
	}{
		{"blank_token", "member_token", ""},
		{"whitespace_token", "member_token", " "},
		{"numeric_member_id", "member_id", 42},
		{"null_persona", "persona_context", nil},
	} {
		t.Run(mut.name, func(t *testing.T) {
			args := fullStartArgs()
			args[mut.key] = mut.val
			var spawnCalls int
			deps := CommandDeps{Spawn: func(StartParams) SpawnOutcome { spawnCalls++; return SpawnOutcome{} }}
			if err := dispatchCommand(&Command{RPC: rpcStart, Args: args}, deps); err == nil {
				t.Fatalf("want err for %s", mut.name)
			}
			if spawnCalls != 0 {
				t.Fatalf("spawn called %d, want 0 for %s", spawnCalls, mut.name)
			}
		})
	}
}

func TestDispatch_Start_OptionalWrongTyped_LenientSpawns(t *testing.T) {
	// A present-but-wrong-typed OPTIONAL field is lenient: read as empty, member still
	// spawns with the executor's default (dispatcher not stricter than the executor).
	for _, mut := range []struct {
		name string
		key  string
		val  any
	}{
		{"bool_role", "role", true},
		{"null_model", "model", nil},
		{"bool_effort", "effort", true},
		{"numeric_task_type", "task_type", 7},
	} {
		t.Run(mut.name, func(t *testing.T) {
			args := fullStartArgs()
			args[mut.key] = mut.val
			var spawnCalls int
			deps := CommandDeps{Spawn: func(StartParams) SpawnOutcome { spawnCalls++; return SpawnOutcome{OK: true} }}
			if err := dispatchCommand(&Command{RPC: rpcStart, Args: args}, deps); err != nil {
				t.Fatalf("optional wrong-type %s must NOT refuse: %v", mut.name, err)
			}
			if spawnCalls != 1 {
				t.Fatalf("spawn called %d, want 1 for %s", spawnCalls, mut.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// dispatchCommand — stop
// ---------------------------------------------------------------------------

func TestDispatch_Stop_CallsStopWithSession(t *testing.T) {
	var gotSession string
	var stopCalls int
	deps := CommandDeps{
		Spawn: func(StartParams) SpawnOutcome { t.Fatal("spawn must not be called on stop"); return SpawnOutcome{} },
		Stop: func(session string) bool {
			gotSession = session
			stopCalls++
			return true
		},
	}
	args := map[string]any{"member_id": "m-9", "session_name": "member-m-9"}
	if err := dispatchCommand(&Command{RPC: rpcStop, Args: args}, deps); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if stopCalls != 1 {
		t.Fatalf("stop=%d, want 1", stopCalls)
	}
	if gotSession != "member-m-9" {
		t.Fatalf("session = %q, want member-m-9", gotSession)
	}
}

func TestDispatch_Stop_Addressing(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"explicit_session_name", map[string]any{"member_id": "m-1", "session_name": "member-m-1"}, "member-m-1"},
		{"session_id_fallback", map[string]any{"member_id": "m-2", "session_id": "member-m-2"}, "member-m-2"},
		{"derive_from_member_id", map[string]any{"member_id": "M-3"}, "member-m-3"}, // lowercased
		{"session_name_wins_over_id", map[string]any{"member_id": "m-4", "session_name": "member-m-4", "session_id": "other"}, "member-m-4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got string
			deps := CommandDeps{Stop: func(s string) bool { got = s; return true }}
			if err := dispatchCommand(&Command{RPC: rpcStop, Args: c.args}, deps); err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != c.want {
				t.Fatalf("session = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDispatch_Stop_NoTarget_NoStop(t *testing.T) {
	var stopCalls int
	deps := CommandDeps{
		Stop: func(string) bool { stopCalls++; return false },
	}
	// args present but carry no addressing field at all.
	err := dispatchCommand(&Command{RPC: rpcStop, Args: map[string]any{"unrelated": "x"}}, deps)
	if err == nil {
		t.Fatal("want err when stop has no target")
	}
	if stopCalls != 0 {
		t.Fatalf("stop=%d, want 0", stopCalls)
	}
}

// ---------------------------------------------------------------------------
// dispatchCommand — misc guards
// ---------------------------------------------------------------------------

func TestDispatch_NilCommand_NoOp(t *testing.T) {
	// A skipped frame (parse returned nil,nil) dispatched as nil must be a no-op.
	var called int
	deps := CommandDeps{
		Spawn: func(StartParams) SpawnOutcome { called++; return SpawnOutcome{} },
		Stop:  func(string) bool { called++; return false },
	}
	if err := dispatchCommand(nil, deps); err != nil {
		t.Fatalf("nil command: unexpected err %v", err)
	}
	if called != 0 {
		t.Fatalf("nil command triggered %d seam calls, want 0", called)
	}
}

// The T-5f01 `update` verb: parse accepts it, dispatch calls ONLY the Update
// seam (no spawn/stop/report), and an unwired seam is a loud refusal — the
// exact degraded face an old-warden-shaped build presents, never a crash.
func TestParseThenDispatch_UpdateKicksSelfUpdateSeam(t *testing.T) {
	payload := []byte(`{"topic":"warden-command","data":{"rpc":"update","args":{"member_id":"m-w1"}}}`)
	cmd, err := parseCommandFrame(payload)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if cmd == nil || cmd.RPC != rpcUpdate {
		t.Fatalf("cmd = %+v, want update command", cmd)
	}
	kicked := 0
	deps := CommandDeps{
		Spawn:  func(StartParams) SpawnOutcome { t.Fatal("must not spawn"); return SpawnOutcome{} },
		Stop:   func(string) bool { t.Fatal("must not stop"); return false },
		Update: func() { kicked++ },
		Report: func(CommandResult) error { t.Fatal("update must not report a receipt"); return nil },
	}
	if err := dispatchCommand(cmd, deps); err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if kicked != 1 {
		t.Fatalf("kicked = %d, want 1", kicked)
	}
}

func TestDispatch_UpdateUnwiredSeamRefusedWithoutPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on update with nil seam: %v", r)
		}
	}()
	err := dispatchCommand(&Command{RPC: rpcUpdate, Args: map[string]any{}}, CommandDeps{})
	if err == nil {
		t.Fatal("want a loud refusal when the Update seam is unwired")
	}
}

func TestDispatch_UnknownRPC_Refused(t *testing.T) {
	// A hand-built Command with an unknown rpc (bypassing parse) is refused.
	deps := CommandDeps{
		Spawn: func(StartParams) SpawnOutcome { t.Fatal("must not spawn"); return SpawnOutcome{} },
		Stop:  func(string) bool { t.Fatal("must not stop"); return false },
	}
	if err := dispatchCommand(&Command{RPC: "bogus", Args: map[string]any{}}, deps); err == nil {
		t.Fatal("want err for unknown rpc")
	}
}

// End-to-end: a raw frame → parse → dispatch, with nil seams, must not panic.
func TestParseThenDispatch_NilSeamsNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on parse+dispatch with nil seams: %v", r)
		}
	}()
	payload := []byte(`{"topic":"warden-command","data":{"rpc":"stop","args":{"member_id":"m-1"}}}`)
	cmd, err := parseCommandFrame(payload)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	// nil seams — dispatch must nil-check them, not deref.
	if err := dispatchCommand(cmd, CommandDeps{}); err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
}

func TestParseThenDispatch_SkippedTopicIsNoOp(t *testing.T) {
	cmd, err := parseCommandFrame([]byte(`{"topic":"chat","data":{"body":"hi"}}`))
	if err != nil || cmd != nil {
		t.Fatalf("want (nil,nil) skip, got cmd=%+v err=%v", cmd, err)
	}
	var called int
	deps := CommandDeps{
		Spawn: func(StartParams) SpawnOutcome { called++; return SpawnOutcome{} },
		Stop:  func(string) bool { called++; return false },
	}
	if err := dispatchCommand(cmd, deps); err != nil {
		t.Fatalf("dispatch of skipped frame: %v", err)
	}
	if called != 0 {
		t.Fatalf("skipped frame triggered %d calls, want 0", called)
	}
}

// Sanity: derived stop session for a bad member id still routes through stop's own
// isMemberSession guard (documented backstop) — here we just assert the derivation
// contract holds and does not blow up on odd input.
func TestDispatch_Stop_DerivedSessionShape(t *testing.T) {
	var got []string
	deps := CommandDeps{Stop: func(s string) bool { got = append(got, s); return true }}
	_ = dispatchCommand(&Command{RPC: rpcStop, Args: map[string]any{"member_id": "AbC"}}, deps)
	if len(got) == 0 || !strings.HasPrefix(got[0], memberSessionPrefix) {
		t.Fatalf("primary derived session %v lacks member- prefix", got)
	}
	// Any further leg is the P5b legacy sweep — exact derived worker-<id> only.
	for _, s := range got[1:] {
		if !strings.HasPrefix(s, workerSessionPrefix) {
			t.Fatalf("legacy sweep leg %q outside the worker- namespace", s)
		}
	}
}

// TestDispatch_Start_SpawnRefused_ReturnsError locks the fail-loud contract: a
// well-formed START whose Spawn returns OK=false must surface as a dispatch ERROR
// (carrying the Reason) — NOT a silent success. This is the guard against the
// Phase-4 boot-death, where a not-OK spawn (claude unresolved) was dropped and
// logged as "dispatched OK".
func TestDispatch_Start_SpawnRefused_ReturnsError(t *testing.T) {
	deps := CommandDeps{
		Spawn: func(StartParams) SpawnOutcome {
			return SpawnOutcome{OK: false, Reason: "claude binary unresolved"}
		},
	}
	err := dispatchCommand(&Command{RPC: rpcStart, Args: fullStartArgs()}, deps)
	if err == nil {
		t.Fatal("a not-OK spawn must return a dispatch error, got nil (silent success)")
	}
	if !strings.Contains(err.Error(), "claude binary unresolved") {
		t.Fatalf("dispatch error must carry the Reason, got %v", err)
	}
	if !strings.Contains(err.Error(), "m-1") {
		t.Fatalf("dispatch error must name the member, got %v", err)
	}
}

// TestDispatch_Start_SpawnRefused_NoReason_StillErrors: a bare OK=false (no Reason)
// still surfaces as an error with a generic cause — never silently swallowed.
func TestDispatch_Start_SpawnRefused_NoReason_StillErrors(t *testing.T) {
	deps := CommandDeps{Spawn: func(StartParams) SpawnOutcome { return SpawnOutcome{OK: false} }}
	err := dispatchCommand(&Command{RPC: rpcStart, Args: fullStartArgs()}, deps)
	if err == nil {
		t.Fatal("a bare not-OK spawn must still return an error")
	}
}

// ---------------------------------------------------------------------------
// command_result reporting (fleet remote-ops stage 1)
// ---------------------------------------------------------------------------

// TestDispatch_Start_ReportsCommandResult: a successful start reports a receipt with
// the member_id / rpc=start / ok=true / reason & log carried from the SpawnOutcome.
func TestDispatch_Start_ReportsCommandResult(t *testing.T) {
	var got CommandResult
	var reports int
	deps := CommandDeps{
		Spawn: func(StartParams) SpawnOutcome { return SpawnOutcome{OK: true} },
		Report: func(cr CommandResult) error {
			got = cr
			reports++
			return nil
		},
	}
	if err := dispatchCommand(&Command{RPC: rpcStart, Args: fullStartArgs()}, deps); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if reports != 1 {
		t.Fatalf("reports = %d, want 1", reports)
	}
	if got.MemberID != "m-1" || got.RPC != rpcStart || !got.OK {
		t.Fatalf("report = %+v, want member m-1 rpc start ok true", got)
	}
	if got.At == "" {
		t.Fatalf("report.At must be stamped (RFC3339), got empty")
	}
}

// TestDispatch_Start_SpawnRefused_ReportsFailure: a refused spawn STILL reports a
// receipt (ok=false + the Reason as reason/log) even though dispatch returns an error.
func TestDispatch_Start_SpawnRefused_ReportsFailure(t *testing.T) {
	var got CommandResult
	var reports int
	deps := CommandDeps{
		Spawn: func(StartParams) SpawnOutcome {
			return SpawnOutcome{OK: false, Reason: "claude binary unresolved"}
		},
		Report: func(cr CommandResult) error { got = cr; reports++; return nil },
	}
	err := dispatchCommand(&Command{RPC: rpcStart, Args: fullStartArgs()}, deps)
	if err == nil {
		t.Fatal("a refused spawn must still return a dispatch error")
	}
	if reports != 1 {
		t.Fatalf("a refused spawn must still report exactly once, got %d", reports)
	}
	if got.OK {
		t.Fatalf("refused spawn report must be ok=false, got %+v", got)
	}
	if got.Reason != "claude binary unresolved" || got.Log != "claude binary unresolved" {
		t.Fatalf("refused report must carry the Reason as reason+log, got %+v", got)
	}
}

// TestDispatch_Stop_ReportsCommandResult: a stop reports member_id / rpc=stop / ok =
// the robust-stop verdict, with the target session in the log.
func TestDispatch_Stop_ReportsCommandResult(t *testing.T) {
	for _, c := range []struct {
		name   string
		stopOK bool
		wantOK bool
	}{
		{"stopped", true, true},
		{"did_not_take", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var got CommandResult
			var reports int
			deps := CommandDeps{
				Stop:   func(string) bool { return c.stopOK },
				Report: func(cr CommandResult) error { got = cr; reports++; return nil },
			}
			args := map[string]any{"member_id": "m-9", "session_name": "member-m-9"}
			if err := dispatchCommand(&Command{RPC: rpcStop, Args: args}, deps); err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if reports != 1 {
				t.Fatalf("reports = %d, want 1", reports)
			}
			if got.MemberID != "m-9" || got.RPC != rpcStop || got.OK != c.wantOK {
				t.Fatalf("report = %+v, want member m-9 rpc stop ok %v", got, c.wantOK)
			}
			if !strings.Contains(got.Log, "member-m-9") {
				t.Fatalf("stop report log must name the session, got %q", got.Log)
			}
		})
	}
}

// TestDispatch_NilReport_Toothless: a nil Report seam is a silent no-op — dispatch's
// return is UNCHANGED (start ok, start refused error, stop) whether or not a reporter
// is wired. Reporting can never gate the critical kill/spawn path.
func TestDispatch_NilReport_Toothless(t *testing.T) {
	okDeps := CommandDeps{Spawn: func(StartParams) SpawnOutcome { return SpawnOutcome{OK: true} }}
	if err := dispatchCommand(&Command{RPC: rpcStart, Args: fullStartArgs()}, okDeps); err != nil {
		t.Fatalf("nil Report must not change a successful start: %v", err)
	}
	refuseDeps := CommandDeps{Spawn: func(StartParams) SpawnOutcome { return SpawnOutcome{OK: false} }}
	if err := dispatchCommand(&Command{RPC: rpcStart, Args: fullStartArgs()}, refuseDeps); err == nil {
		t.Fatal("nil Report must not mask a refused start's error")
	}
	stopDeps := CommandDeps{Stop: func(string) bool { return true }}
	args := map[string]any{"member_id": "m-1", "session_name": "member-m-1"}
	if err := dispatchCommand(&Command{RPC: rpcStop, Args: args}, stopDeps); err != nil {
		t.Fatalf("nil Report must not change a stop: %v", err)
	}
}

// TestCommandDeps_report_TruncatesLog: the report seam clamps an over-cap log to the
// 4 KB ceiling and stamps a default At when empty.
func TestCommandDeps_report_TruncatesLog(t *testing.T) {
	var got CommandResult
	deps := CommandDeps{Report: func(cr CommandResult) error { got = cr; return nil }}
	big := strings.Repeat("x", commandResultLogMax+500)
	deps.report(CommandResult{MemberID: "m", RPC: rpcStop, Log: big})
	if len(got.Log) != commandResultLogMax {
		t.Fatalf("log len = %d, want clamped to %d", len(got.Log), commandResultLogMax)
	}
	if got.At == "" {
		t.Fatal("report must stamp a default At when empty")
	}
}

// ---------------------------------------------------------------------------
// dispatchCommand — uninstall (the warden dismantling itself)
// ---------------------------------------------------------------------------

// uninstallArgs is a valid uninstall args map: member_id addresses the receipt, the
// session names the agent to kill first.
func uninstallArgs() map[string]any {
	return map[string]any{"member_id": "m-5", "session_name": "member-m-5"}
}

// TestDispatch_Uninstall_HappyPath_StopTeardownReportThenExitZero: a clean uninstall
// (1) stops the addressed agent session, (2) tears down its own install, (3) reports a
// DELIVERED receipt, then (4) self-exits 0. The ORDER matters — stop before teardown,
// report before exit.
func TestDispatch_Uninstall_HappyPath_StopTeardownReportThenExitZero(t *testing.T) {
	var order []string
	var stoppedSession string
	var gotReport CommandResult
	var exitCode = -1
	deps := CommandDeps{
		Stop: func(s string) bool { order = append(order, "stop"); stoppedSession = s; return true },
		Teardown: func() (bool, string) {
			order = append(order, "teardown")
			return true, "[ocwarden teardown] teardown complete for com.officraft.ocwarden\n"
		},
		Report: func(cr CommandResult) error { order = append(order, "report"); gotReport = cr; return nil },
		Exit:   func(code int) { order = append(order, "exit"); exitCode = code },
	}
	err := dispatchCommand(&Command{RPC: rpcUninstall, Args: uninstallArgs()}, deps)
	if err != nil {
		t.Fatalf("clean uninstall must not return an error, got %v", err)
	}
	if strings.Join(order, ",") != "stop,teardown,report,exit" {
		t.Fatalf("uninstall order = %v, want stop,teardown,report,exit", order)
	}
	if stoppedSession != "member-m-5" {
		t.Fatalf("stop targeted %q, want member-m-5", stoppedSession)
	}
	if exitCode != 0 {
		t.Fatalf("clean uninstall must os.Exit(0), got %d", exitCode)
	}
	if gotReport.MemberID != "m-5" || gotReport.RPC != rpcUninstall || !gotReport.OK {
		t.Fatalf("report = %+v, want member m-5 rpc uninstall ok true", gotReport)
	}
	if !strings.Contains(gotReport.Log, "teardown complete") {
		t.Fatalf("uninstall report log must carry the teardown transcript, got %q", gotReport.Log)
	}
}

// TestDispatch_Uninstall_ReportUndelivered_DoesNotExit: if the SYNCHRONOUS receipt
// fails to land, the warden must NOT self-exit (so the server's reconcile can re-issue).
func TestDispatch_Uninstall_ReportUndelivered_DoesNotExit(t *testing.T) {
	var exited bool
	deps := CommandDeps{
		Stop:     func(string) bool { return true },
		Teardown: func() (bool, string) { return true, "torn down" },
		Report:   func(CommandResult) error { return errors.New("POST status 500") },
		Exit:     func(int) { exited = true },
	}
	err := dispatchCommand(&Command{RPC: rpcUninstall, Args: uninstallArgs()}, deps)
	if err == nil {
		t.Fatal("an undelivered uninstall receipt must surface as a dispatch error")
	}
	if exited {
		t.Fatal("the warden must NOT self-exit when its receipt did not land")
	}
}

// TestDispatch_Uninstall_TeardownIncomplete_ReportsButStaysAlive: a teardown that could
// not fully remove its artifacts (ok=false) STILL reports (so the server sees the fault),
// but does NOT self-exit — the warden stays up for a retry.
func TestDispatch_Uninstall_TeardownIncomplete_ReportsButStaysAlive(t *testing.T) {
	var gotReport CommandResult
	var reported, exited bool
	deps := CommandDeps{
		Stop:     func(string) bool { return true },
		Teardown: func() (bool, string) { return false, "could not remove plist" },
		Report:   func(cr CommandResult) error { reported = true; gotReport = cr; return nil },
		Exit:     func(int) { exited = true },
	}
	err := dispatchCommand(&Command{RPC: rpcUninstall, Args: uninstallArgs()}, deps)
	if err == nil {
		t.Fatal("an incomplete teardown must surface as a dispatch error (stay-alive signal)")
	}
	if !reported {
		t.Fatal("an incomplete teardown must STILL report the fault to the server")
	}
	if gotReport.OK {
		t.Fatalf("an incomplete teardown must report ok=false, got %+v", gotReport)
	}
	if exited {
		t.Fatal("an incomplete teardown must NOT self-exit — stay alive for retry")
	}
}

// TestDispatch_Uninstall_MissingSessionTarget_StillTearsDown: uninstall of a host whose
// agent is already gone (no session target) tolerates the missing stop and still tears
// down the warden itself, reports, and exits 0.
func TestDispatch_Uninstall_MissingSessionTarget_StillTearsDown(t *testing.T) {
	var stopCalls int
	var toreDown bool
	var exitCode = -1
	deps := CommandDeps{
		Stop:     func(string) bool { stopCalls++; return true },
		Teardown: func() (bool, string) { toreDown = true; return true, "ok" },
		Report:   func(CommandResult) error { return nil },
		Exit:     func(code int) { exitCode = code },
	}
	// No session_name/session_id/member_id → stopSessionFromArgs refuses → stop skipped.
	err := dispatchCommand(&Command{RPC: rpcUninstall, Args: map[string]any{}}, deps)
	if err != nil {
		t.Fatalf("a targetless uninstall must still tear down cleanly, got %v", err)
	}
	if stopCalls != 0 {
		t.Fatalf("no session target → stop must be skipped, got %d calls", stopCalls)
	}
	if !toreDown {
		t.Fatal("teardown is the load-bearing step and must run even with no stop target")
	}
	if exitCode != 0 {
		t.Fatalf("a clean targetless uninstall must exit 0, got %d", exitCode)
	}
}

// TestParseCommandFrame_UninstallEnvelope: the uninstall verb is accepted by the parser.
func TestParseCommandFrame_UninstallEnvelope(t *testing.T) {
	payload := []byte(`{"topic":"warden-command","data":{"rpc":"uninstall","args":{
		"member_id":"m-5","session_id":"member-m-5"}}}`)
	cmd, err := parseCommandFrame(payload)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cmd == nil || cmd.RPC != rpcUninstall {
		t.Fatalf("cmd = %+v, want uninstall command", cmd)
	}
}
