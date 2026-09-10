package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// sseStream is a response body a test feeds line by line. A closed lines channel
// is a clean end of stream; a cancelled ctx is the socket going away under a
// blocked read, which is what the idle watchdog does.
type sseStream struct {
	ctxOf   func() context.Context
	lines   chan string
	pending string
	closed  bool
}

func (s *sseStream) Read(p []byte) (int, error) {
	for s.pending == "" {
		select {
		case line, ok := <-s.lines:
			if !ok {
				return 0, io.EOF
			}
			s.pending = line
		case <-s.ctxOf().Done():
			return 0, errors.New("read tcp: i/o timeout")
		}
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

func (s *sseStream) Close() error { s.closed = true; return nil }

// sseHarness is one scripted downlink: the requests it saw, the body it served,
// the frames the transport dispatched, and everything it logged.
type sseHarness struct {
	requests  []*http.Request
	reqCtx    context.Context
	stream    *sseStream
	log       []string
	stops     []string
	updates   int
	connects  int
	transport *sseTransport
}

func newSSEHarness(t *testing.T, status int, dialErr error) *sseHarness {
	t.Helper()
	h := &sseHarness{}
	h.stream = &sseStream{lines: make(chan string, 16), ctxOf: func() context.Context { return h.reqCtx }}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h.requests = append(h.requests, r)
		h.reqCtx = r.Context()
		if dialErr != nil {
			return nil, dialErr
		}
		return &http.Response{StatusCode: status, Body: h.stream, Header: http.Header{}}, nil
	})}
	h.transport = &sseTransport{
		base:   "https://station.example",
		token:  jwtWardenOne,
		client: client,
		logf:   func(format string, a ...any) { h.log = append(h.log, fmt.Sprintf(format, a...)) },
		deps: CommandDeps{
			Stop:   func(session string) (bool, bool) { h.stops = append(h.stops, session); return true, false },
			Update: func() { h.updates++ },
		},
		onConnect: func() { h.connects++ },
	}
	return h
}

func TestScanSSEWithActivity(t *testing.T) {
	cases := []struct {
		name         string
		stream       string
		wantPayloads []string
		wantActivity int
	}{
		{
			name:         "one complete event",
			stream:       "event: message\ndata: {\"topic\":\"warden-command\"}\n\n",
			wantPayloads: []string{`{"topic":"warden-command"}`},
			wantActivity: 3,
		},
		{
			name:         "multi-line data joins with newlines",
			stream:       "data: first\ndata: second\n\n",
			wantPayloads: []string{"first\nsecond"},
			wantActivity: 3,
		},
		{
			name:         "keepalive comments carry no payload but are activity",
			stream:       ": connected\n: heartbeat\n: heartbeat\n",
			wantPayloads: nil,
			wantActivity: 3,
		},
		{
			name:         "CRLF framing and ignored fields",
			stream:       "id: 42\r\nretry: 3000\r\ndata: hi\r\n\r\n",
			wantPayloads: []string{"hi"},
			wantActivity: 4,
		},
		{
			name:         "exactly one leading space is stripped",
			stream:       "data:  padded \n\ndata:tight\n\n",
			wantPayloads: []string{" padded ", "tight"},
			wantActivity: 4,
		},
		{
			name:         "an unterminated final event is discarded",
			stream:       "data: delivered\n\ndata: truncated\n",
			wantPayloads: []string{"delivered"},
			wantActivity: 3,
		},
		{
			name:         "a field with no colon is ignored",
			stream:       "notafield\ndata: hi\n\n",
			wantPayloads: []string{"hi"},
			wantActivity: 3,
		},
		{
			name:         "a blank line with nothing accumulated dispatches nothing",
			stream:       "\n\ndata: hi\n\n",
			wantPayloads: []string{"hi"},
			wantActivity: 4,
		},
	}
	for _, c := range cases {
		var payloads []string
		activity := 0
		err := scanSSEWithActivity(strings.NewReader(c.stream),
			func(p []byte) { payloads = append(payloads, string(p)) },
			func() { activity++ })
		if err != nil {
			t.Errorf("%s: err = %v, want nil", c.name, err)
		}
		if !reflect.DeepEqual(payloads, c.wantPayloads) {
			t.Errorf("%s: payloads = %#v, want %#v", c.name, payloads, c.wantPayloads)
		}
		if activity != c.wantActivity {
			t.Errorf("%s: watchdog reset %d times, want %d", c.name, activity, c.wantActivity)
		}
	}

	var payloads []string
	oversize := "data: delivered\n\ndata: " + strings.Repeat("x", maxSSELine) + "\n\n"
	err := scanSSEWithActivity(strings.NewReader(oversize), func(p []byte) { payloads = append(payloads, string(p)) }, nil)
	if err == nil {
		t.Error("an unbounded line must end the stream so the reader reconnects")
	}
	if !reflect.DeepEqual(payloads, []string{"delivered"}) {
		t.Errorf("payloads before the refusal = %#v, want [delivered]", payloads)
	}

	payloads = nil
	if err := scanSSE(strings.NewReader("data: hi\n\n"), func(p []byte) { payloads = append(payloads, string(p)) }); err != nil {
		t.Errorf("scanSSE err = %v, want nil", err)
	}
	if !reflect.DeepEqual(payloads, []string{"hi"}) {
		t.Errorf("scanSSE payloads = %#v, want [hi]", payloads)
	}
}

func TestConnectOnce(t *testing.T) {
	t.Run("an open stream is read to its end", func(t *testing.T) {
		h := newSSEHarness(t, http.StatusOK, nil)
		go func() {
			h.stream.lines <- ": heartbeat\n"
			h.stream.lines <- `data: {"topic":"chat","data":{}}` + "\n\n"
			h.stream.lines <- `data: {"topic":"warden-command","data":{"rpc":"stop","args":{"member_id":"m-5"}}}` + "\n\n"
			close(h.stream.lines)
		}()
		opened, err := h.transport.connectOnce(context.Background())
		if !opened || err != nil {
			t.Errorf("connectOnce = (%v, %v), want (true, nil)", opened, err)
		}
		if !reflect.DeepEqual(h.stops, []string{"member-m-5"}) {
			t.Errorf("stopped %v, want [member-m-5]", h.stops)
		}
		wantLog := []string{
			"[ocwarden] command reader: connected — streaming https://station.example/api/events",
			"[ocwarden] command reader: received stop frame (member_id=m-5)",
			"[ocwarden] command reader: dispatched stop OK (member_id=m-5)",
		}
		if !reflect.DeepEqual(h.log, wantLog) {
			t.Errorf("log =\n  %#v\nwant\n  %#v", h.log, wantLog)
		}
		if h.connects != 1 {
			t.Errorf("the self-update kick fired %d times, want 1 per open stream", h.connects)
		}
		if !h.stream.closed {
			t.Error("the response body was not closed")
		}

		req := h.requests[0]
		if req.Method != http.MethodGet || req.URL.String() != "https://station.example/api/events" {
			t.Errorf("dialled %s %s, want GET https://station.example/api/events", req.Method, req.URL)
		}
		wantHeaders := map[string]string{
			"User-Agent":    "ocwarden/0.1",
			"Accept":        "text/event-stream",
			"Cache-Control": "no-cache",
			"Authorization": "Bearer " + jwtWardenOne,
		}
		for name, want := range wantHeaders {
			if got := req.Header.Get(name); got != want {
				t.Errorf("%s = %q, want %q", name, got, want)
			}
		}
		if req.URL.RawQuery != "" {
			t.Errorf("the warden addresses itself by its credential alone, but sent ?%s", req.URL.RawQuery)
		}
	})

	t.Run("a rejected connection opens nothing", func(t *testing.T) {
		h := newSSEHarness(t, http.StatusUnauthorized, nil)
		close(h.stream.lines)
		opened, err := h.transport.connectOnce(context.Background())
		if opened || err == nil || err.Error() != "unexpected status 401" {
			t.Errorf("connectOnce = (%v, %v), want (false, unexpected status 401)", opened, err)
		}
		if h.connects != 0 || len(h.log) != 0 {
			t.Errorf("a refused connection kicked %d times and logged %#v, want 0 and none", h.connects, h.log)
		}
		if !h.stream.closed {
			t.Error("the refused response body was not closed")
		}
	})

	t.Run("an undialable station opens nothing", func(t *testing.T) {
		h := newSSEHarness(t, 0, errors.New("dial tcp: connection refused"))
		opened, err := h.transport.connectOnce(context.Background())
		if opened || err == nil || !strings.Contains(err.Error(), "connection refused") {
			t.Errorf("connectOnce = (%v, %v), want (false, connection refused)", opened, err)
		}
		if h.connects != 0 || len(h.log) != 0 {
			t.Errorf("a failed dial kicked %d times and logged %#v, want 0 and none", h.connects, h.log)
		}
	})

	t.Run("a credential-less warden sends no Authorization header", func(t *testing.T) {
		h := newSSEHarness(t, http.StatusOK, nil)
		h.transport.token = ""
		close(h.stream.lines)
		if _, err := h.transport.connectOnce(context.Background()); err != nil {
			t.Fatalf("connectOnce: %v", err)
		}
		if got := h.requests[0].Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want none", got)
		}
	})

	t.Run("a silent stream is dropped by the idle watchdog", func(t *testing.T) {
		h := newSSEHarness(t, http.StatusOK, nil)
		h.transport.idleReadTimeout = 60 * time.Millisecond
		start := time.Now()
		opened, err := h.transport.connectOnce(context.Background())
		if !opened || err == nil {
			t.Errorf("connectOnce = (%v, %v), want (true, a read timeout)", opened, err)
		}
		if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
			t.Errorf("the watchdog fired after %s, want at least its 60ms threshold", elapsed)
		}
	})

	t.Run("heartbeats keep the watchdog from firing", func(t *testing.T) {
		h := newSSEHarness(t, http.StatusOK, nil)
		h.transport.idleReadTimeout = 100 * time.Millisecond
		go func() {
			for i := 0; i < 5; i++ {
				time.Sleep(30 * time.Millisecond)
				h.stream.lines <- ": heartbeat\n"
			}
			close(h.stream.lines)
		}()
		opened, err := h.transport.connectOnce(context.Background())
		if !opened || err != nil {
			t.Errorf("connectOnce = (%v, %v), want (true, nil) — a heartbeat-only stream is alive", opened, err)
		}
	})
}

func TestHandlePayload(t *testing.T) {
	stopFrame := `{"topic":"warden-command","data":{"rpc":"stop","args":{"member_id":"m-5"}}}`

	cases := []struct {
		name      string
		payload   string
		deps      func(h *sseHarness) CommandDeps
		wantStops []string
		wantLog   []string
	}{
		{
			name:    "a frame on another topic is silently skipped",
			payload: `{"topic":"context-high","data":{"member_id":"m-5"}}`,
		},
		{
			name:    "a keepalive-shaped payload is silently skipped",
			payload: `{"topic":"heartbeat","data":null}`,
		},
		{
			name:    "a truncated frame is logged and skipped",
			payload: `{"topic":"warden-comm`,
			wantLog: []string{"[ocwarden] command reader: skip malformed frame: command: malformed envelope: " +
				"unexpected end of JSON input"},
		},
		{
			name:    "an unknown verb is logged and skipped",
			payload: `{"topic":"warden-command","data":{"rpc":"rm -rf","args":{}}}`,
			wantLog: []string{"[ocwarden] command reader: skip malformed frame: " +
				"command: unknown or missing rpc \"rm -rf\""},
		},
		{
			name:      "an executed stop is receipted then reported OK",
			payload:   stopFrame,
			wantStops: []string{"member-m-5"},
			wantLog: []string{
				"[ocwarden] command reader: received stop frame (member_id=m-5)",
				"[ocwarden] command reader: dispatched stop OK (member_id=m-5)",
			},
		},
		{
			name:    "an unwired verb is refused without touching the host",
			payload: `{"topic":"warden-command","data":{"rpc":"update","args":{}}}`,
			deps:    func(h *sseHarness) CommandDeps { return CommandDeps{} },
			wantLog: []string{
				"[ocwarden] command reader: received update frame (target=?)",
				"[ocwarden] command reader: dispatch refused: command: update refused: self-update kick seam not wired",
			},
		},
		{
			name:    "an executed stop whose receipt was lost never reads as an all-clear",
			payload: stopFrame,
			deps: func(h *sseHarness) CommandDeps {
				return CommandDeps{
					Stop:   func(s string) (bool, bool) { h.stops = append(h.stops, s); return true, false },
					Report: func(CommandResult) error { return errors.New("status 502") },
				}
			},
			wantStops: []string{"member-m-5"},
			wantLog: []string{
				"[ocwarden] command reader: received stop frame (member_id=m-5)",
				"[ocwarden] command reader: stop EXECUTED but its receipt did not reach the server " +
					"(member_id=m-5): command: stop for session \"member-m-5\" ran (stopped) but " +
					"command_result receipt undelivered: status 502 — the server does not know this outcome",
			},
		},
		{
			name:    "a panicking side effect cannot take the reader down",
			payload: stopFrame,
			deps: func(h *sseHarness) CommandDeps {
				return CommandDeps{Stop: func(string) (bool, bool) { panic("tmux socket vanished") }}
			},
			wantLog: []string{
				"[ocwarden] command reader: received stop frame (member_id=m-5)",
				"[ocwarden] command reader: recovered from panic handling frame: tmux socket vanished",
			},
		},
	}

	for _, c := range cases {
		h := newSSEHarness(t, http.StatusOK, nil)
		if c.deps != nil {
			h.transport.deps = c.deps(h)
		}
		h.transport.handlePayload([]byte(c.payload))
		if !reflect.DeepEqual(h.stops, c.wantStops) {
			t.Errorf("%s: stopped %v, want %v", c.name, h.stops, c.wantStops)
		}
		if !reflect.DeepEqual(h.log, c.wantLog) {
			t.Errorf("%s: log =\n  %#v\nwant\n  %#v", c.name, h.log, c.wantLog)
		}
	}
}

func TestCommandTargetLabel(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"a member verb", map[string]any{"member_id": "m-5"}, "member_id=m-5"},
		{"a worker verb", map[string]any{"worker_id": "ow-9"}, "worker_id=ow-9"},
		{"member_id wins when both are present", map[string]any{"member_id": "m-5", "worker_id": "ow-9"}, "member_id=m-5"},
		{"a blank member_id falls through to the worker", map[string]any{"member_id": "", "worker_id": "ow-9"}, "worker_id=ow-9"},
		{"a non-string member_id falls through", map[string]any{"member_id": 42, "worker_id": "ow-9"}, "worker_id=ow-9"},
		{"an unaddressed verb", map[string]any{}, "target=?"},
		{"a blank worker_id", map[string]any{"worker_id": "   "}, "worker_id=   "},
	}
	for _, c := range cases {
		if got := commandTargetLabel(&Command{RPC: rpcStop, Args: c.args}); got != c.want {
			t.Errorf("%s: commandTargetLabel = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestNextSSEBackoff(t *testing.T) {
	cases := []struct {
		cur, capd, want time.Duration
	}{
		{time.Second, time.Minute, 2 * time.Second},
		{16 * time.Second, time.Minute, 32 * time.Second},
		{32 * time.Second, time.Minute, time.Minute},
		{time.Minute, time.Minute, time.Minute},
		{0, time.Minute, time.Minute},
		{-time.Second, time.Minute, time.Minute},
	}
	for _, c := range cases {
		if got := nextSSEBackoff(c.cur, c.capd); got != c.want {
			t.Errorf("nextSSEBackoff(%s, %s) = %s, want %s", c.cur, c.capd, got, c.want)
		}
	}
}

func TestSleepCtx(t *testing.T) {
	var slept []time.Duration
	sleep := func(d time.Duration) { slept = append(slept, d) }

	if !sleepCtx(context.Background(), sleep, 5*time.Second) {
		t.Error("a live context must keep the reconnect loop going")
	}
	if !reflect.DeepEqual(slept, []time.Duration{5 * time.Second}) {
		t.Errorf("slept %v, want [5s]", slept)
	}

	slept = nil
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepCtx(cancelled, sleep, 5*time.Second) {
		t.Error("an already-cancelled context must stop the loop")
	}
	if slept != nil {
		t.Errorf("a cancelled context must not wait out the backoff, slept %v", slept)
	}

	slept = nil
	midway, cancelMidway := context.WithCancel(context.Background())
	stopping := func(d time.Duration) { slept = append(slept, d); cancelMidway() }
	if sleepCtx(midway, stopping, 5*time.Second) {
		t.Error("a context cancelled during the backoff must stop the loop")
	}
	if !reflect.DeepEqual(slept, []time.Duration{5 * time.Second}) {
		t.Errorf("slept %v, want [5s]", slept)
	}
	cancelMidway()
}

func TestResolveClaudeBin(t *testing.T) {
	root := t.TempDir()
	onPath := stageBinary(t, filepath.Join(root, "path", "claude"), "#!/bin/sh\n")
	stamped := stageBinary(t, filepath.Join(root, "stamped", "claude"), "#!/bin/sh\n")
	local := stageBinary(t, filepath.Join(root, "home", ".local", "bin", "claude"), "#!/bin/sh\n")
	notExec := filepath.Join(root, "plain", "claude")
	if err := os.MkdirAll(filepath.Dir(notExec), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(notExec, []byte("text"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	empty := filepath.Join(root, "nothing-here")

	cases := []struct {
		name string
		path string
		env  map[string]string
		want string
	}{
		{"the plist stamp wins", filepath.Dir(onPath),
			map[string]string{"OC_CLAUDE_BIN": stamped, "HOME": filepath.Join(root, "home")}, stamped},
		{"a non-executable stamp falls through to PATH", filepath.Dir(onPath),
			map[string]string{"OC_CLAUDE_BIN": notExec, "HOME": filepath.Join(root, "home")}, onPath},
		{"a stamped directory falls through to PATH", filepath.Dir(onPath),
			map[string]string{"OC_CLAUDE_BIN": root, "HOME": filepath.Join(root, "home")}, onPath},
		{"an enriched PATH is honoured", filepath.Dir(onPath),
			map[string]string{"HOME": filepath.Join(root, "home")}, onPath},
		{"a launchd PATH falls back to the home install", empty,
			map[string]string{"HOME": filepath.Join(root, "home")}, local},
		{"nothing anywhere resolves to nothing", empty,
			map[string]string{"HOME": filepath.Join(root, "bare")}, ""},
	}
	for _, c := range cases {
		t.Setenv("PATH", c.path)
		if got := resolveClaudeBin(envMap(c.env)); got != c.want {
			t.Errorf("%s: resolveClaudeBin = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestResolveCodexBin(t *testing.T) {
	root := t.TempDir()
	onPath := stageBinary(t, filepath.Join(root, "path", "codex"), "#!/bin/sh\n")
	stamped := stageBinary(t, filepath.Join(root, "stamped", "codex"), "#!/bin/sh\n")
	npmGlobal := stageBinary(t, filepath.Join(root, "home", ".npm-global", "bin", "codex"), "#!/bin/sh\n")
	local := stageBinary(t, filepath.Join(root, "home2", ".local", "bin", "codex"), "#!/bin/sh\n")
	empty := filepath.Join(root, "nothing-here")

	cases := []struct {
		name string
		path string
		env  map[string]string
		want string
	}{
		{"the env override wins", filepath.Dir(onPath),
			map[string]string{"OC_CODEX_BIN": stamped, "HOME": filepath.Join(root, "home")}, stamped},
		{"an enriched PATH is honoured", filepath.Dir(onPath),
			map[string]string{"HOME": filepath.Join(root, "home")}, onPath},
		{"a launchd PATH falls back to the home install", empty,
			map[string]string{"HOME": filepath.Join(root, "home2")}, local},
		{"the npm-global install is found too", empty,
			map[string]string{"HOME": filepath.Join(root, "home")}, npmGlobal},
		{"nothing anywhere resolves to nothing", empty,
			map[string]string{"HOME": filepath.Join(root, "bare")}, ""},
	}
	for _, c := range cases {
		t.Setenv("PATH", c.path)
		if got := resolveCodexBin(envMap(c.env)); got != c.want {
			t.Errorf("%s: resolveCodexBin = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIsExecutableFile(t *testing.T) {
	root := t.TempDir()
	exec := stageBinary(t, filepath.Join(root, "claude"), "#!/bin/sh\n")
	plain := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(plain, []byte("text"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	groupOnly := filepath.Join(root, "group-exec")
	if err := os.WriteFile(groupOnly, []byte("x"), 0o010); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(root, "claude-link")
	if err := os.Symlink(exec, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"an executable file", exec, true},
		{"a symlink to one", link, true},
		{"a readable but non-executable file", plain, false},
		{"any executable bit is enough", groupOnly, true},
		{"a directory", root, false},
		{"a path that is not there", filepath.Join(root, "absent"), false},
		{"the empty path", "", false},
	}
	for _, c := range cases {
		if got := isExecutableFile(c.path); got != c.want {
			t.Errorf("%s: isExecutableFile(%q) = %v, want %v", c.name, c.path, got, c.want)
		}
	}
}

func TestResolveRepoRoot(t *testing.T) {
	if got := resolveRepoRoot(func() (string, error) {
		return "/Users/eva/open-company/cli/ocwarden/ocwarden", nil
	}); got != "/Users/eva/open-company" {
		t.Errorf("resolveRepoRoot = %q, want %q", got, "/Users/eva/open-company")
	}
	if got := resolveRepoRoot(func() (string, error) {
		return "/Users/eva/.officraft/warden/ocwarden", nil
	}); got != "/Users/eva" {
		t.Errorf("a home-installed warden walks the same three hops: %q, want %q", got, "/Users/eva")
	}
	if got := resolveRepoRoot(func() (string, error) { return "", errors.New("no /proc/self/exe") }); got != "" {
		t.Errorf("resolveRepoRoot = %q, want \"\"", got)
	}
}

func TestPathStatable(t *testing.T) {
	root := t.TempDir()
	file := stageBinary(t, filepath.Join(root, "ocagent"), "agent-bytes-v1")
	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(root, "absent"), dangling); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"a downloaded binary", file, true},
		{"a directory also stats", root, true},
		{"a path that is not there", filepath.Join(root, "absent"), false},
		{"a dangling symlink", dangling, false},
		{"the empty path", "", false},
	}
	for _, c := range cases {
		if got := pathStatable(c.path); got != c.want {
			t.Errorf("%s: pathStatable(%q) = %v, want %v", c.name, c.path, got, c.want)
		}
	}
}

func TestResolveOcAgentBin(t *testing.T) {
	exe := func(p string) func() (string, error) { return func() (string, error) { return p, nil } }
	present := func(paths ...string) func(string) bool {
		set := map[string]bool{}
		for _, p := range paths {
			set[p] = true
		}
		return func(p string) bool { return set[p] }
	}

	got, ok := resolveOcAgentBin(exe("/Users/eva/.officraft/warden/ocwarden"),
		present("/Users/eva/.officraft/warden/ocagent"), "/Users/eva/open-company")
	if got != "/Users/eva/.officraft/warden/ocagent" || !ok {
		t.Errorf("resolveOcAgentBin = (%q, %v), want the sibling and true", got, ok)
	}

	got, ok = resolveOcAgentBin(exe("/Users/eva/open-company/cli/ocwarden/ocwarden"),
		present("/Users/eva/open-company/cli/ocagent/ocagent"), "/Users/eva/open-company")
	if got != "/Users/eva/open-company/cli/ocagent/ocagent" || !ok {
		t.Errorf("resolveOcAgentBin = (%q, %v), want the in-tree fallback and true", got, ok)
	}

	got, ok = resolveOcAgentBin(exe("/Users/eva/.officraft/warden/ocwarden"),
		present(), "/Users/eva")
	if got != "/Users/eva/cli/ocagent/ocagent" || ok {
		t.Errorf("resolveOcAgentBin = (%q, %v), want the guessed path and FALSE", got, ok)
	}

	got, ok = resolveOcAgentBin(func() (string, error) { return "", errors.New("no /proc/self/exe") },
		present("/repo/cli/ocagent/ocagent"), "/repo")
	if got != "/repo/cli/ocagent/ocagent" || !ok {
		t.Errorf("resolveOcAgentBin = (%q, %v), want the fallback and true", got, ok)
	}
}

func TestNewOcAgentResolver(t *testing.T) {
	root := t.TempDir()
	exe := stageBinary(t, filepath.Join(root, "cli", "ocwarden", "ocwarden"), "warden-bytes-v1")
	resolve := newOcAgentResolver(func() (string, error) { return exe, nil }, pathStatable)

	want := filepath.Join(root, "cli", "ocagent", "ocagent")
	if got, ok := resolve(); got != want || ok {
		t.Errorf("resolve() = (%q, %v), want (%q, false) before the sibling is downloaded", got, ok, want)
	}

	stageBinary(t, want, "agent-bytes-v1")
	if got, ok := resolve(); got != want || !ok {
		t.Errorf("resolve() = (%q, %v), want (%q, true) once it lands — the answer must not be baked in", got, ok, want)
	}

	sibling := filepath.Join(root, "cli", "ocwarden", "ocagent")
	stageBinary(t, sibling, "agent-bytes-v1")
	if got, ok := resolve(); got != sibling || !ok {
		t.Errorf("resolve() = (%q, %v), want the sibling %q", got, ok, sibling)
	}
}

func TestBuildClaudeCredProbe(t *testing.T) {
	home := t.TempDir()

	if probe := buildClaudeCredProbe(envMap(map[string]string{"OC_CLAUDE_CRED_CHECK": "0"}), &wardenRunner{}); probe != nil {
		t.Error("OC_CLAUDE_CRED_CHECK=0 must leave the gate off (a nil probe), not a fabricated verdict")
	}

	keychainArgv := "security find-generic-password -s Claude Code-credentials"
	signedOut := &wardenRunner{fallback: wardenRun{err: errors.New("SecKeychainSearchCopyNext: not found")}}
	probe := buildClaudeCredProbe(envMap(map[string]string{"HOME": home}), signedOut)
	if probe == nil {
		t.Fatal("the gate must be wired when nobody disabled it")
	}
	got := probe()
	want := claudeCredStatus{Present: false,
		Summary: "cred_file=unset keychain=unset ANTHROPIC_API_KEY=unset ANTHROPIC_AUTH_TOKEN=unset " +
			"CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"}
	if got != want {
		t.Errorf("probe() = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(signedOut.calls, []string{keychainArgv}) {
		t.Errorf("ran %v, want the metadata-only keychain lookup %q", signedOut.calls, keychainArgv)
	}

	stageBinary(t, filepath.Join(home, ".claude", ".credentials.json"), "{}")
	if got := probe(); !got.Present || !strings.HasPrefix(got.Summary, "cred_file=SET ") {
		t.Errorf("probe() = %+v, want a present verdict led by cred_file=SET", got)
	}
}

func TestBuildSpawnDeps(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	claudeBin := stageBinary(t, filepath.Join(root, "bin", "claude"), "#!/bin/sh\n")
	codexBin := stageBinary(t, filepath.Join(root, "bin", "codex"), "#!/bin/sh\n")
	t.Setenv("PATH", filepath.Join(root, "nothing-here"))
	t.Setenv("HOME", home)

	env := envMap(map[string]string{
		"HOME": home, "OC_CLAUDE_BIN": claudeBin, "OC_CODEX_BIN": codexBin,
		"OC_AGENT_HOME": filepath.Join(home, "agents"), "OC_AGENT_ENV_FILE": filepath.Join(home, "env"),
		"OC_AGENT_ENV_INHERIT": "0", "OC_CLAUDE_CRED_CHECK": "0",
	})
	runner := &wardenRunner{}
	deps := buildSpawnDeps(Config{Base: "https://station.example"}, env, runner, "officraft-lab", "lab")

	if deps.Base != "https://station.example" || deps.Socket != "officraft-lab" || deps.Namespace != "lab" {
		t.Errorf("addressing = (%q, %q, %q), want the station, socket and namespace it was handed",
			deps.Base, deps.Socket, deps.Namespace)
	}
	if deps.Home != filepath.Join(home, "agents") || deps.EnvFile != filepath.Join(home, "env") {
		t.Errorf("paths = (%q, %q), want the owner's agents root and env file", deps.Home, deps.EnvFile)
	}
	if deps.ClaudeBin != claudeBin || deps.CodexBin != codexBin {
		t.Errorf("bins = (%q, %q), want (%q, %q)", deps.ClaudeBin, deps.CodexBin, claudeBin, codexBin)
	}
	if deps.Runner != CmdRunner(runner) {
		t.Error("the shell seam was not the one passed in")
	}
	if deps.CaptureEnv != nil {
		t.Error("OC_AGENT_ENV_INHERIT=0 must leave the interactive-env capture off")
	}
	if deps.ClaudeCreds != nil {
		t.Error("OC_CLAUDE_CRED_CHECK=0 must leave the login gate off")
	}
	if deps.Pretrust != nil {
		t.Error("Pretrust is bound per spawn, so the literal must leave it nil")
	}
	if deps.ResolveOcAgentBin == nil {
		t.Fatal("ResolveOcAgentBin unwired — every spawn would publish a dangling ocagent symlink (T-81)")
	}
	if got, _ := deps.ResolveOcAgentBin(); !strings.HasSuffix(got, "ocagent") {
		t.Errorf("ResolveOcAgentBin() = %q, want a path ending in ocagent", got)
	}
	if deps.RepoRoot != resolveRepoRoot(os.Executable) {
		t.Errorf("RepoRoot = %q, want the three-hop walk from the running executable", deps.RepoRoot)
	}
	for name, wired := range map[string]bool{
		"Logf": deps.Logf != nil, "WriteFile": deps.WriteFile != nil, "MkdirAll": deps.MkdirAll != nil,
		"Symlink": deps.Symlink != nil, "Remove": deps.Remove != nil, "Sleep": deps.Sleep != nil,
	} {
		if !wired {
			t.Errorf("buildSpawnDeps left %s unwired", name)
		}
	}

	inherit := buildSpawnDeps(Config{}, envMap(map[string]string{"HOME": home}), runner, "officraft", "")
	if inherit.CaptureEnv == nil {
		t.Error("the interactive-env capture must be on unless the owner turned it off")
	}
	if inherit.ClaudeCreds == nil {
		t.Error("the claude-login gate must be on unless the owner turned it off")
	}
}

func TestBuildCommandDeps(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", filepath.Join(root, "nothing-here"))
	t.Setenv("HOME", root)
	env := envMap(map[string]string{"HOME": root, "OC_AGENT_ENV_INHERIT": "0", "OC_CLAUDE_CRED_CHECK": "0"})

	deps := buildCommandDeps(Config{Base: "https://station.example", Token: jwtWardenOne, ID: "warden-1"},
		env, &wardenRunner{})

	for name, wired := range map[string]bool{
		"Spawn": deps.Spawn != nil, "Stop": deps.Stop != nil, "Teardown": deps.Teardown != nil,
		"Exit": deps.Exit != nil, "Report": deps.Report != nil,
	} {
		if !wired {
			t.Errorf("buildCommandDeps left %s unwired", name)
		}
	}
	if deps.Update != nil || deps.Renew != nil {
		t.Error("the update/renew kicks belong to the updater, so this constructor must leave them nil")
	}

	homeless := buildCommandDeps(Config{}, envMap(map[string]string{}), &wardenRunner{})
	ok, log := homeless.Teardown()
	if ok || log != "[ocwarden teardown] cannot resolve paths: HOME must be set\n" {
		t.Errorf("Teardown = (%v, %q), want a reported path failure that leaves the warden alive", ok, log)
	}
}

func TestNewCommandReporter(t *testing.T) {
	var posted []*http.Request
	restore := http.DefaultTransport
	status := 0
	var dialErr error
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		posted = append(posted, r)
		if dialErr != nil {
			return nil, dialErr
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Header: http.Header{}}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = restore })

	cfg := Config{Base: "https://station.example", Token: jwtWardenOne, ID: "warden-1"}
	receipt := CommandResult{MemberID: "m-5", RPC: rpcStop, OK: true, Reason: "stopped",
		Log: "session=member-m-5: stopped", At: "2026-09-08T10:30:00Z"}

	skipped := []struct {
		name    string
		cfg     Config
		receipt CommandResult
	}{
		{"a warden with no credential cannot report", Config{Base: cfg.Base, ID: "warden-1"}, receipt},
		{"a warden with no identity cannot report", Config{Base: cfg.Base, Token: jwtWardenOne}, receipt},
		{"an unaddressed receipt is noise", cfg, CommandResult{RPC: rpcStop, OK: true}},
		{"a blank member and worker id is unaddressed", cfg, CommandResult{MemberID: " ", WorkerID: " ", RPC: rpcStop}},
		{"a verb-less receipt is noise", cfg, CommandResult{MemberID: "m-5", RPC: "  "}},
	}
	for _, c := range skipped {
		posted = nil
		status = 200
		if err := newCommandReporter(c.cfg)(c.receipt); err != nil {
			t.Errorf("%s: err = %v, want nil", c.name, err)
		}
		if len(posted) != 0 {
			t.Errorf("%s: sent %d receipts, want none", c.name, len(posted))
		}
	}

	posted, status = nil, 200
	if err := newCommandReporter(cfg)(receipt); err != nil {
		t.Errorf("a 2xx is a delivered receipt, got err = %v", err)
	}
	if len(posted) != 1 || posted[0].URL.String() != "https://station.example"+commandResultPath {
		t.Fatalf("posted %d receipts to %v, want one to %s", len(posted), posted, commandResultPath)
	}
	if got := posted[0].Header.Get("Authorization"); got != "Bearer "+jwtWardenOne {
		t.Errorf("Authorization = %q, want the warden's bearer line", got)
	}

	posted, status = nil, 422
	err := newCommandReporter(cfg)(receipt)
	if err == nil || err.Error() != "command_result POST returned status 422" {
		t.Errorf("err = %v, want %q", err, "command_result POST returned status 422")
	}

	posted, status = nil, 0
	dialErr = errors.New("dial tcp: connection refused")
	err = newCommandReporter(cfg)(CommandResult{WorkerID: "ow-9", RPC: rpcWorkerStop, OK: true})
	if err == nil || err.Error() != "command_result POST returned status 0" {
		t.Errorf("err = %v, want %q", err, "command_result POST returned status 0")
	}
	if len(posted) != 1 {
		t.Errorf("a worker-addressed receipt was not attempted: %d posts", len(posted))
	}
	dialErr = nil
}

func TestNewCommandTransport(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", filepath.Join(root, "nothing-here"))
	t.Setenv("HOME", root)
	env := envMap(map[string]string{"HOME": root, "OC_AGENT_ENV_INHERIT": "0", "OC_CLAUDE_CRED_CHECK": "0"})

	var log []string
	logf := func(format string, a ...any) { log = append(log, fmt.Sprintf(format, a...)) }
	tr := newCommandTransport(Config{Base: "https://station.example", Token: jwtWardenOne, ID: "warden-1"},
		env, &wardenRunner{}, logf)

	if tr.base != "https://station.example" || tr.token != jwtWardenOne {
		t.Errorf("addressing = (%q, ...), want the configured station and its credential", tr.base)
	}
	if tr.backoffStart != time.Second || tr.backoffCap != time.Minute || tr.idleReadTimeout != 45*time.Second {
		t.Errorf("pacing = (%s, %s, %s), want (1s, 1m0s, 45s)", tr.backoffStart, tr.backoffCap, tr.idleReadTimeout)
	}
	if tr.client == nil || tr.client.Timeout != 0 {
		t.Errorf("an SSE downlink must carry no overall deadline, got %v", tr.client)
	}
	if tr.sleep == nil || tr.deps.Spawn == nil || tr.deps.Stop == nil || tr.deps.Report == nil {
		t.Error("newCommandTransport left the dispatch or backoff seams unwired")
	}
	if tr.onConnect != nil {
		t.Error("the self-update kick is wired by main.go, so this constructor must leave it nil")
	}
	tr.logf("hello %s", "there")
	if !reflect.DeepEqual(log, []string{"hello there"}) {
		t.Errorf("log = %#v, want [hello there]", log)
	}
}
