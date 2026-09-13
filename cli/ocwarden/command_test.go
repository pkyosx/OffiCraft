package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// dispatchSpy records every side effect the dispatcher is allowed to have, and
// answers each seam from the fields the test sets.
type dispatchSpy struct {
	spawns    []StartParams
	spawnOut  SpawnOutcome
	stops     []string
	stopOK    bool
	stopNoop  bool
	teardowns int
	teardown  func() (bool, string)
	exits     []int
	updates   int
	renews    int
	reports   []CommandResult
	reportErr error
}

func (s *dispatchSpy) deps() CommandDeps {
	return CommandDeps{
		Spawn: func(p StartParams) SpawnOutcome {
			s.spawns = append(s.spawns, p)
			return s.spawnOut
		},
		Stop: func(session string) (bool, bool) {
			s.stops = append(s.stops, session)
			return s.stopOK, s.stopNoop
		},
		Teardown: func() (bool, string) {
			s.teardowns++
			if s.teardown != nil {
				return s.teardown()
			}
			return true, "launchd bootout ok; tokfile removed; plist removed"
		},
		Exit:   func(code int) { s.exits = append(s.exits, code) },
		Update: func() { s.updates++ },
		Renew:  func() { s.renews++ },
		Report: func(cr CommandResult) error {
			s.reports = append(s.reports, cr)
			return s.reportErr
		},
	}
}

// receipt is the one reported CommandResult with the wall-clock stamp blanked,
// so a receipt can be compared as a whole literal.
func (s *dispatchSpy) receipt(t *testing.T) CommandResult {
	t.Helper()
	if len(s.reports) != 1 {
		t.Fatalf("reports = %+v, want exactly one", s.reports)
	}
	cr := s.reports[0]
	if _, err := time.Parse(time.RFC3339, cr.At); err != nil {
		t.Errorf("receipt At = %q, want an RFC3339 stamp: %v", cr.At, err)
	}
	cr.At = ""
	return cr
}

func TestParseCommandFrame(t *testing.T) {
	t.Run("a warden-command frame becomes a dispatchable command", func(t *testing.T) {
		cmd, err := parseCommandFrame([]byte(
			`{"topic":"warden-command","data":{"rpc":"start","args":{"member_id":"m1","effort":"high","n":7}}}`))
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		want := &Command{RPC: "start", Args: map[string]any{"member_id": "m1", "effort": "high", "n": float64(7)}}
		if !reflect.DeepEqual(cmd, want) {
			t.Errorf("cmd = %+v, want %+v", cmd, want)
		}
	})

	t.Run("every accepted verb parses", func(t *testing.T) {
		for _, rpc := range []string{"start", "stop", "uninstall", "update", "renew", "worker_stop"} {
			cmd, err := parseCommandFrame([]byte(`{"topic":"warden-command","data":{"rpc":"` + rpc + `","args":{}}}`))
			if err != nil {
				t.Errorf("%s: err = %v, want nil", rpc, err)
				continue
			}
			if want := (&Command{RPC: rpc, Args: map[string]any{}}); !reflect.DeepEqual(cmd, want) {
				t.Errorf("%s: cmd = %+v, want %+v", rpc, cmd, want)
			}
		}
	})

	t.Run("another topic is a silent skip", func(t *testing.T) {
		for _, payload := range []string{
			`{"topic":"context-high","data":{"rpc":"start","args":{}}}`,
			`{"topic":"chat","data":{"body":"hi"}}`,
			`{"data":{"rpc":"start","args":{}}}`,
			`{}`,
		} {
			cmd, err := parseCommandFrame([]byte(payload))
			if cmd != nil || err != nil {
				t.Errorf("%s: got (%v, %v), want (nil, nil)", payload, cmd, err)
			}
		}
	})

	t.Run("a malformed or unactionable frame is an error and never a command", func(t *testing.T) {
		cases := []struct {
			payload string
			wantErr string
		}{
			{``, "command: empty frame payload"},
			{`{"topic":"warden-command","data":{"rpc":"start","args":{}}`, "command: malformed envelope: unexpected end of JSON input"},
			{`[1,2,3]`, "command: malformed envelope: json: cannot unmarshal array into Go value of type struct { Topic string \"json:\\\"topic\\\"\"; Data json.RawMessage \"json:\\\"data\\\"\" }"},
			{`{"topic":"warden-command"}`, "command: warden-command frame missing data"},
			{`{"topic":"warden-command","data":"start"}`, "command: malformed data body: json: cannot unmarshal string into Go value of type struct { RPC string \"json:\\\"rpc\\\"\"; Args map[string]interface {} \"json:\\\"args\\\"\" }"},
			{`{"topic":"warden-command","data":{"args":{}}}`, `command: unknown or missing rpc ""`},
			{`{"topic":"warden-command","data":{"rpc":"worker_start","args":{}}}`, `command: unknown or missing rpc "worker_start"`},
			{`{"topic":"warden-command","data":{"rpc":"START","args":{}}}`, `command: unknown or missing rpc "START"`},
			{`{"topic":"warden-command","data":{"rpc":"stop"}}`, `command: rpc "stop" missing args object`},
			{`{"topic":"warden-command","data":{"rpc":"stop","args":null}}`, `command: rpc "stop" missing args object`},
		}
		for _, c := range cases {
			cmd, err := parseCommandFrame([]byte(c.payload))
			if cmd != nil {
				t.Errorf("%s: cmd = %+v, want nil", c.payload, cmd)
			}
			if err == nil || err.Error() != c.wantErr {
				t.Errorf("%s: err = %v, want %q", c.payload, err, c.wantErr)
			}
		}
	})
}

func TestTruncLog(t *testing.T) {
	if got := truncLog("session=member-m1: stopped"); got != "session=member-m1: stopped" {
		t.Errorf("a short log is returned verbatim, got %q", got)
	}
	at := strings.Repeat("x", 4096)
	if got := truncLog(at); got != at {
		t.Errorf("a log exactly at the cap is returned verbatim, len = %d", len(got))
	}
	over := strings.Repeat("x", 4096) + "TAIL"
	got := truncLog(over)
	if len(got) != 4096 || got != at {
		t.Errorf("an oversized log = %d bytes, want the first 4096", len(got))
	}
}

func TestReport(t *testing.T) {
	t.Run("an unwired reporter is a silent skip", func(t *testing.T) {
		if err := (CommandDeps{}).report(CommandResult{MemberID: "m1"}); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("the emitted receipt is capped and stamped", func(t *testing.T) {
		var got CommandResult
		deps := CommandDeps{Report: func(cr CommandResult) error { got = cr; return nil }}
		before := time.Now().UTC().Add(-time.Second)
		if err := deps.report(CommandResult{
			MemberID: "m1", RPC: "stop", OK: true, Reason: "stopped",
			Log: strings.Repeat("y", 5000),
		}); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if len(got.Log) != 4096 {
			t.Errorf("Log = %d bytes, want 4096", len(got.Log))
		}
		stamp, err := time.Parse(time.RFC3339, got.At)
		if err != nil {
			t.Fatalf("At = %q, want an RFC3339 stamp: %v", got.At, err)
		}
		if stamp.Before(before) || stamp.After(time.Now().UTC().Add(time.Second)) {
			t.Errorf("At = %q, want a stamp from now", got.At)
		}
		got.Log, got.At = "", ""
		if want := (CommandResult{MemberID: "m1", RPC: "stop", OK: true, Reason: "stopped"}); got != want {
			t.Errorf("receipt = %+v, want %+v", got, want)
		}
	})

	t.Run("a caller-set timestamp is kept", func(t *testing.T) {
		var got CommandResult
		deps := CommandDeps{Report: func(cr CommandResult) error { got = cr; return nil }}
		_ = deps.report(CommandResult{MemberID: "m1", At: "2026-01-02T03:04:05Z"})
		if got.At != "2026-01-02T03:04:05Z" {
			t.Errorf("At = %q, want the caller's stamp", got.At)
		}
	})

	t.Run("the delivery verdict is returned", func(t *testing.T) {
		want := errors.New("post status 503")
		deps := CommandDeps{Report: func(CommandResult) error { return want }}
		if err := deps.report(CommandResult{MemberID: "m1"}); !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
	})
}

func TestDispatchCommand(t *testing.T) {
	startArgs := map[string]any{
		"member_id": "m1", "persona_context": "you are m1", "member_token": "jwt-m1",
		"role": "builder", "runtime": "claude", "model": "opus", "effort": "high",
		"session_name": "member-m1",
	}

	t.Run("a skipped frame does nothing", func(t *testing.T) {
		s := &dispatchSpy{}
		if err := dispatchCommand(nil, s.deps()); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
		if len(s.spawns)+len(s.stops)+len(s.reports)+s.updates+s.renews != 0 {
			t.Errorf("a nil command touched something: %+v", s)
		}
	})

	t.Run("start spawns the downpushed params and reports the receipt", func(t *testing.T) {
		s := &dispatchSpy{spawnOut: SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}}
		if err := dispatchCommand(&Command{RPC: "start", Args: startArgs}, s.deps()); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		want := []StartParams{{
			MemberID: "m1", PersonaContext: "you are m1", MemberToken: "jwt-m1",
			Role: "builder", Runtime: "claude", Model: "opus", Effort: "high",
			SessionName: "member-m1",
		}}
		if !reflect.DeepEqual(s.spawns, want) {
			t.Errorf("spawns = %+v, want %+v", s.spawns, want)
		}
		if got, want := s.receipt(t), (CommandResult{MemberID: "m1", RPC: "start", OK: true}); got != want {
			t.Errorf("receipt = %+v, want %+v", got, want)
		}
	})

	t.Run("a spawn that went ahead with something to say carries it on the receipt", func(t *testing.T) {
		s := &dispatchSpy{spawnOut: SpawnOutcome{
			OK: true, SessionID: "member-m1", PID: "500",
			Note: "pretrust_unverified: claude reads a different file",
		}}
		if err := dispatchCommand(&Command{RPC: "start", Args: startArgs}, s.deps()); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		want := CommandResult{
			MemberID: "m1", RPC: "start", OK: true,
			Reason: "pretrust_unverified: claude reads a different file",
			Log:    "pretrust_unverified: claude reads a different file",
		}
		if got := s.receipt(t); got != want {
			t.Errorf("receipt = %+v, want %+v", got, want)
		}
	})

	t.Run("a refused spawn is an error carrying the reason, and still reported", func(t *testing.T) {
		s := &dispatchSpy{spawnOut: SpawnOutcome{Reason: "ocagent_not_found: no ocagent binary at /x"}}
		err := dispatchCommand(&Command{RPC: "start", Args: startArgs}, s.deps())
		wantErr := `command: start for "m1" did not spawn: ocagent_not_found: no ocagent binary at /x`
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
		want := CommandResult{
			MemberID: "m1", RPC: "start", OK: false,
			Reason: "ocagent_not_found: no ocagent binary at /x",
			Log:    "ocagent_not_found: no ocagent binary at /x",
		}
		if got := s.receipt(t); got != want {
			t.Errorf("receipt = %+v, want %+v", got, want)
		}
	})

	t.Run("a reason-less refusal still names the refusal", func(t *testing.T) {
		s := &dispatchSpy{spawnOut: SpawnOutcome{}}
		err := dispatchCommand(&Command{RPC: "start", Args: startArgs}, s.deps())
		wantErr := `command: start for "m1" did not spawn: spawn refused (warden reported no reason)`
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
	})

	t.Run("a spawn that ran but whose receipt did not land is reported as undelivered", func(t *testing.T) {
		s := &dispatchSpy{spawnOut: SpawnOutcome{OK: true}, reportErr: errors.New("post status 503")}
		err := dispatchCommand(&Command{RPC: "start", Args: startArgs}, s.deps())
		if !errors.Is(err, errReceiptUndelivered) {
			t.Fatalf("err = %v, want an errReceiptUndelivered", err)
		}
		wantErr := `command: start for "m1" ran but command_result receipt undelivered: post status 503`
		if err.Error() != wantErr {
			t.Errorf("err = %q, want %q", err, wantErr)
		}
	})

	t.Run("a half-formed start is refused before anything runs", func(t *testing.T) {
		s := &dispatchSpy{}
		err := dispatchCommand(&Command{RPC: "start", Args: map[string]any{"member_id": "m1"}}, s.deps())
		wantErr := `command: start missing/blank required field "persona_context"`
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
		if len(s.spawns) != 0 || len(s.reports) != 0 {
			t.Errorf("spawns = %v, reports = %v, want neither", s.spawns, s.reports)
		}
	})

	t.Run("an unwired spawn seam neither spawns nor reports", func(t *testing.T) {
		s := &dispatchSpy{}
		deps := s.deps()
		deps.Spawn = nil
		if err := dispatchCommand(&Command{RPC: "start", Args: startArgs}, deps); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
		if len(s.reports) != 0 {
			t.Errorf("reports = %+v, want none", s.reports)
		}
	})

	t.Run("stop kills the addressed session and reports the ladder verdict", func(t *testing.T) {
		s := &dispatchSpy{stopOK: true}
		err := dispatchCommand(&Command{RPC: "stop", Args: map[string]any{
			"member_id": "m1", "session_name": "member-m1",
		}}, s.deps())
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if want := []string{"member-m1"}; !reflect.DeepEqual(s.stops, want) {
			t.Errorf("stops = %v, want %v", s.stops, want)
		}
		want := CommandResult{
			MemberID: "m1", RPC: "stop", OK: true, Reason: "stopped",
			Log: "session=member-m1: stopped",
		}
		if got := s.receipt(t); got != want {
			t.Errorf("receipt = %+v, want %+v", got, want)
		}
	})

	t.Run("an idempotent no-op stop carries the no_such_session reason", func(t *testing.T) {
		s := &dispatchSpy{stopOK: true, stopNoop: true}
		_ = dispatchCommand(&Command{RPC: "stop", Args: map[string]any{"member_id": "m1"}}, s.deps())
		want := CommandResult{
			MemberID: "m1", RPC: "stop", OK: true,
			Reason: "no_such_session: stop was a no-op (no session, no member process on this warden)",
			Log:    "session=member-m1: no_such_session: stop was a no-op (no session, no member process on this warden)",
		}
		if got := s.receipt(t); got != want {
			t.Errorf("receipt = %+v, want %+v", got, want)
		}
	})

	t.Run("an incomplete stop reports the partial verdict without failing the dispatch", func(t *testing.T) {
		s := &dispatchSpy{}
		if err := dispatchCommand(&Command{RPC: "stop", Args: map[string]any{"member_id": "m1"}}, s.deps()); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
		want := CommandResult{
			MemberID: "m1", RPC: "stop", OK: false,
			Reason: "stop incomplete (session still present / broken probe / member process survived the sweep)",
			Log:    "session=member-m1: stop incomplete (session still present / broken probe / member process survived the sweep)",
		}
		if got := s.receipt(t); got != want {
			t.Errorf("receipt = %+v, want %+v", got, want)
		}
	})

	t.Run("stopping an outsource member also reaps its retired worker session", func(t *testing.T) {
		s := &dispatchSpy{stopOK: true}
		_ = dispatchCommand(&Command{RPC: "stop", Args: map[string]any{"member_id": "ow-78173e"}}, s.deps())
		if want := []string{"member-ow-78173e", "worker-ow-78173e"}; !reflect.DeepEqual(s.stops, want) {
			t.Errorf("stops = %v, want %v", s.stops, want)
		}
		if got := s.receipt(t); got.Log != "session=member-ow-78173e: stopped" {
			t.Errorf("the receipt must stay the primary session's, got %+v", got)
		}
	})

	t.Run("a stop with no target is refused before the kill", func(t *testing.T) {
		s := &dispatchSpy{}
		err := dispatchCommand(&Command{RPC: "stop", Args: map[string]any{"role": "builder"}}, s.deps())
		wantErr := "command: stop missing target (need session_name/session_id/member_id)"
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
		if len(s.stops) != 0 {
			t.Errorf("stops = %v, want none", s.stops)
		}
	})

	t.Run("a stop whose receipt did not land is reported as undelivered", func(t *testing.T) {
		s := &dispatchSpy{stopOK: true, reportErr: errors.New("post status 503")}
		err := dispatchCommand(&Command{RPC: "stop", Args: map[string]any{"member_id": "m1"}}, s.deps())
		wantErr := `command: stop for session "member-m1" ran (stopped) but command_result receipt undelivered: post status 503`
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
	})

	t.Run("the legacy worker_stop alias kills the retired session and keys on worker_id", func(t *testing.T) {
		s := &dispatchSpy{stopOK: true}
		if err := dispatchCommand(&Command{RPC: "worker_stop",
			Args: map[string]any{"worker_id": "ow-78173e"}}, s.deps()); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if want := []string{"worker-ow-78173e"}; !reflect.DeepEqual(s.stops, want) {
			t.Errorf("stops = %v, want %v", s.stops, want)
		}
		want := CommandResult{
			WorkerID: "ow-78173e", RPC: "worker_stop", OK: true, Reason: "stopped",
			Log: "session=worker-ow-78173e: stopped",
		}
		if got := s.receipt(t); got != want {
			t.Errorf("receipt = %+v, want %+v", got, want)
		}
	})

	t.Run("an incomplete worker_stop is an error", func(t *testing.T) {
		s := &dispatchSpy{}
		err := dispatchCommand(&Command{RPC: "worker_stop",
			Args: map[string]any{"worker_id": "ow-78173e"}}, s.deps())
		wantErr := `command: worker_stop incomplete for "worker-ow-78173e" (session still present / sweep survivor)`
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
		if got := s.receipt(t); got.OK || got.Reason != "stop incomplete (session still present / sweep survivor)" {
			t.Errorf("receipt = %+v, want the partial verdict", got)
		}
	})

	t.Run("worker_stop refuses an unwired stop seam", func(t *testing.T) {
		s := &dispatchSpy{}
		deps := s.deps()
		deps.Stop = nil
		err := dispatchCommand(&Command{RPC: "worker_stop", Args: map[string]any{"worker_id": "ow-1"}}, deps)
		wantErr := `command: worker_stop for "worker-ow-1" refused: stop seam not wired`
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
	})

	t.Run("update kicks the self-updater and carries no receipt", func(t *testing.T) {
		s := &dispatchSpy{}
		if err := dispatchCommand(&Command{RPC: "update", Args: map[string]any{}}, s.deps()); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if s.updates != 1 || s.renews != 0 || len(s.reports) != 0 {
			t.Errorf("updates=%d renews=%d reports=%d, want 1/0/0", s.updates, s.renews, len(s.reports))
		}
	})

	t.Run("renew raises the credential demand and carries no receipt", func(t *testing.T) {
		s := &dispatchSpy{}
		if err := dispatchCommand(&Command{RPC: "renew", Args: map[string]any{}}, s.deps()); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if s.renews != 1 || s.updates != 0 || len(s.reports) != 0 {
			t.Errorf("renews=%d updates=%d reports=%d, want 1/0/0", s.renews, s.updates, len(s.reports))
		}
	})

	t.Run("an unwired update or renew seam is refused loudly", func(t *testing.T) {
		s := &dispatchSpy{}
		deps := s.deps()
		deps.Update, deps.Renew = nil, nil
		err := dispatchCommand(&Command{RPC: "update", Args: map[string]any{}}, deps)
		if want := "command: update refused: self-update kick seam not wired"; err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
		err = dispatchCommand(&Command{RPC: "renew", Args: map[string]any{}}, deps)
		if want := "command: renew refused: credential-renewal seam not wired"; err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})

	t.Run("uninstall kills the agent, tears itself down, reports, then exits", func(t *testing.T) {
		s := &dispatchSpy{stopOK: true}
		if err := dispatchCommand(&Command{RPC: "uninstall",
			Args: map[string]any{"member_id": "m1"}}, s.deps()); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if want := []string{"member-m1"}; !reflect.DeepEqual(s.stops, want) {
			t.Errorf("stops = %v, want %v", s.stops, want)
		}
		if s.teardowns != 1 {
			t.Errorf("teardowns = %d, want 1", s.teardowns)
		}
		want := CommandResult{
			MemberID: "m1", RPC: "uninstall", OK: true, Reason: "uninstalled",
			Log: "launchd bootout ok; tokfile removed; plist removed",
		}
		if got := s.receipt(t); got != want {
			t.Errorf("receipt = %+v, want %+v", got, want)
		}
		if !reflect.DeepEqual(s.exits, []int{0}) {
			t.Errorf("exits = %v, want [0]", s.exits)
		}
	})

	t.Run("an undelivered uninstall receipt keeps the warden alive", func(t *testing.T) {
		s := &dispatchSpy{stopOK: true, reportErr: errors.New("post status 503")}
		err := dispatchCommand(&Command{RPC: "uninstall", Args: map[string]any{"member_id": "m1"}}, s.deps())
		wantErr := "command: uninstall receipt undelivered, NOT self-exiting: post status 503"
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
		if len(s.exits) != 0 {
			t.Errorf("exits = %v, want none", s.exits)
		}
	})

	t.Run("an incomplete teardown reports, then stays alive for the retry", func(t *testing.T) {
		s := &dispatchSpy{stopOK: true, teardown: func() (bool, string) {
			return false, "could not remove /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist"
		}}
		err := dispatchCommand(&Command{RPC: "uninstall", Args: map[string]any{"member_id": "m1"}}, s.deps())
		wantErr := `command: uninstall teardown incomplete for "m1" (receipt delivered); staying alive for retry`
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
		want := CommandResult{
			MemberID: "m1", RPC: "uninstall", OK: false,
			Reason: "teardown incomplete (a required artifact could not be removed)",
			Log:    "could not remove /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist",
		}
		if got := s.receipt(t); got != want {
			t.Errorf("receipt = %+v, want %+v", got, want)
		}
		if len(s.exits) != 0 {
			t.Errorf("exits = %v, want none", s.exits)
		}
	})

	t.Run("an uninstall with no teardown seam still reports and exits", func(t *testing.T) {
		s := &dispatchSpy{stopOK: true}
		deps := s.deps()
		deps.Teardown = nil
		if err := dispatchCommand(&Command{RPC: "uninstall", Args: map[string]any{"member_id": "m1"}}, deps); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		want := CommandResult{
			MemberID: "m1", RPC: "uninstall", OK: true, Reason: "uninstalled",
			Log: "uninstall: teardown seam not wired",
		}
		if got := s.receipt(t); got != want {
			t.Errorf("receipt = %+v, want %+v", got, want)
		}
		if !reflect.DeepEqual(s.exits, []int{0}) {
			t.Errorf("exits = %v, want [0]", s.exits)
		}
	})

	t.Run("an uninstall with no killable target still tears down", func(t *testing.T) {
		s := &dispatchSpy{stopOK: true}
		if err := dispatchCommand(&Command{RPC: "uninstall", Args: map[string]any{}}, s.deps()); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if len(s.stops) != 0 {
			t.Errorf("stops = %v, want none", s.stops)
		}
		if s.teardowns != 1 || !reflect.DeepEqual(s.exits, []int{0}) {
			t.Errorf("teardowns = %d, exits = %v, want 1 and [0]", s.teardowns, s.exits)
		}
	})

	t.Run("a hand-built unknown rpc is refused", func(t *testing.T) {
		s := &dispatchSpy{}
		err := dispatchCommand(&Command{RPC: "worker_start", Args: map[string]any{}}, s.deps())
		if want := `command: unhandled rpc "worker_start"`; err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})
}

func TestArgString(t *testing.T) {
	args := map[string]any{"member_id": "m1", "count": float64(7), "on": true, "null": nil}
	cases := []struct {
		key     string
		wantVal string
		wantOK  bool
	}{
		{"member_id", "m1", true},
		{"count", "", false},
		{"on", "", false},
		{"null", "", false},
		{"absent", "", false},
	}
	for _, c := range cases {
		v, ok := argString(args, c.key)
		if v != c.wantVal || ok != c.wantOK {
			t.Errorf("argString(%q) = (%q, %v), want (%q, %v)", c.key, v, ok, c.wantVal, c.wantOK)
		}
	}
	if v, ok := argString(nil, "member_id"); v != "" || ok {
		t.Errorf("argString(nil) = (%q, %v), want (\"\", false)", v, ok)
	}
}

func TestStartParamsFromArgs(t *testing.T) {
	t.Run("every field is carried through", func(t *testing.T) {
		got, err := startParamsFromArgs(map[string]any{
			"member_id": "m1", "persona_context": "you are m1", "member_token": "jwt-m1",
			"role": "builder", "task_type": "build", "runtime": "codex",
			"model": "gpt-5", "effort": "max", "session_name": "member-custom",
		})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		want := StartParams{
			MemberID: "m1", PersonaContext: "you are m1", MemberToken: "jwt-m1",
			Role: "builder", TaskType: "build", Runtime: "codex",
			Model: "gpt-5", Effort: "max", SessionName: "member-custom",
		}
		if got != want {
			t.Errorf("params = %+v, want %+v", got, want)
		}
	})

	t.Run("the optional fields are left to the executor's defaults", func(t *testing.T) {
		got, err := startParamsFromArgs(map[string]any{
			"member_id": "m1", "persona_context": "you are m1", "member_token": "jwt-m1",
			"role": 42, "model": nil,
		})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		want := StartParams{MemberID: "m1", PersonaContext: "you are m1", MemberToken: "jwt-m1"}
		if got != want {
			t.Errorf("params = %+v, want %+v", got, want)
		}
	})

	t.Run("a missing or blank required field is refused", func(t *testing.T) {
		full := map[string]any{"member_id": "m1", "persona_context": "you are m1", "member_token": "jwt-m1"}
		for _, key := range []string{"member_id", "persona_context", "member_token"} {
			for _, bad := range []any{nil, "", "  ", 42} {
				args := map[string]any{}
				for k, v := range full {
					args[k] = v
				}
				args[key] = bad
				got, err := startParamsFromArgs(args)
				wantErr := `command: start missing/blank required field "` + key + `"`
				if err == nil || err.Error() != wantErr {
					t.Errorf("%s=%v: err = %v, want %q", key, bad, err, wantErr)
				}
				if got != (StartParams{}) {
					t.Errorf("%s=%v: params = %+v, want the zero value", key, bad, got)
				}
			}
		}
	})
}

func TestStopSessionFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"session_name wins", map[string]any{
			"session_name": "member-explicit", "session_id": "member-sid", "member_id": "m1",
		}, "member-explicit"},
		{"session_id is next", map[string]any{"session_id": "member-sid", "member_id": "m1"}, "member-sid"},
		{"member_id derives the session", map[string]any{"member_id": "M1"}, "member-m1"},
		{"an empty session_name falls through", map[string]any{"session_name": "", "member_id": "m1"}, "member-m1"},
		{"a non-string session_name falls through", map[string]any{"session_name": 42, "member_id": "m1"}, "member-m1"},
	}
	for _, c := range cases {
		got, err := stopSessionFromArgs(c.args)
		if err != nil || got != c.want {
			t.Errorf("%s: got (%q, %v), want (%q, nil)", c.name, got, err, c.want)
		}
	}

	for _, args := range []map[string]any{{}, {"member_id": ""}, {"member_id": 42}, {"role": "builder"}} {
		got, err := stopSessionFromArgs(args)
		wantErr := "command: stop missing target (need session_name/session_id/member_id)"
		if got != "" || err == nil || err.Error() != wantErr {
			t.Errorf("%v: got (%q, %v), want (\"\", %q)", args, got, err, wantErr)
		}
	}
}
