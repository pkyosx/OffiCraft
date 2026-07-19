package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// SSE line framing (scanSSE) — the pure wire parser, no network.
// ---------------------------------------------------------------------------

func collectPayloads(t *testing.T, raw string) []string {
	t.Helper()
	var got []string
	if err := scanSSE(strings.NewReader(raw), func(p []byte) {
		got = append(got, string(p))
	}); err != nil {
		t.Fatalf("scanSSE returned error: %v", err)
	}
	return got
}

func TestScanSSE_SingleDataFrame(t *testing.T) {
	got := collectPayloads(t, "data: {\"topic\":\"warden-command\"}\n\n")
	want := []string{`{"topic":"warden-command"}`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestScanSSE_MultiLineDataJoinedWithNewline(t *testing.T) {
	// Two data: lines within ONE event → joined by \n (SSE spec).
	got := collectPayloads(t, "data: line-one\ndata: line-two\n\n")
	want := []string{"line-one\nline-two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestScanSSE_IgnoresCommentsAndIdEventFields(t *testing.T) {
	// `: connected` / `: heartbeat` comments, and id:/event:/retry: fields are all
	// ignored; only data payloads survive, split at the blank-line boundaries.
	raw := ": connected\n\n" +
		"id: 7\n" +
		"event: delta\n" +
		"data: {\"topic\":\"member\"}\n\n" +
		": heartbeat\n\n" +
		"retry: 3000\n" +
		"data: {\"topic\":\"warden-command\"}\n\n"
	got := collectPayloads(t, raw)
	want := []string{`{"topic":"member"}`, `{"topic":"warden-command"}`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestScanSSE_CRLFTolerated(t *testing.T) {
	got := collectPayloads(t, "data: hello\r\n\r\n")
	want := []string{"hello"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestScanSSE_LeadingSpaceStrippedExactlyOnce(t *testing.T) {
	// "data:  x" → one leading space stripped, so payload is " x" (not "x").
	got := collectPayloads(t, "data:  x\n\n")
	want := []string{" x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestScanSSE_IncompleteFinalEventDiscarded(t *testing.T) {
	// No trailing blank line → the SSE spec discards the incomplete final event.
	got := collectPayloads(t, "data: no-terminator\n")
	if len(got) != 0 {
		t.Fatalf("expected no payloads for unterminated event, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// dispatch bridge (handlePayload) — parse → skip | log | dispatch, no crash.
// ---------------------------------------------------------------------------

// recordingDeps is a fake CommandDeps that records every call for assertions.
type recordingDeps struct {
	mu     sync.Mutex
	spawns []StartParams
	stops  []string
}

func (d *recordingDeps) deps() CommandDeps {
	return CommandDeps{
		Spawn: func(p StartParams) SpawnOutcome {
			d.mu.Lock()
			d.spawns = append(d.spawns, p)
			d.mu.Unlock()
			return SpawnOutcome{OK: true}
		},
		Stop: func(session string) bool {
			d.mu.Lock()
			d.stops = append(d.stops, session)
			d.mu.Unlock()
			return true
		},
	}
}

func (d *recordingDeps) snapshot() ([]StartParams, []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := append([]StartParams(nil), d.spawns...)
	st := append([]string(nil), d.stops...)
	return s, st
}

func newTestTransport(deps CommandDeps) (*sseTransport, *[]string) {
	var logs []string
	var mu sync.Mutex
	logf := func(format string, args ...any) {
		mu.Lock()
		logs = append(logs, fmt.Sprintf(format, args...))
		mu.Unlock()
	}
	return &sseTransport{deps: deps, logf: logf}, &logs
}

func TestHandlePayload_StartDispatches(t *testing.T) {
	rec := &recordingDeps{}
	tr, _ := newTestTransport(rec.deps())
	payload := `{"topic":"warden-command","data":{"rpc":"start","args":{` +
		`"member_id":"m-1","persona_context":"you are","member_token":"tok-1",` +
		`"role":"assistant","model":"opus","session_name":"member-m-1"}}}`
	tr.handlePayload([]byte(payload))

	spawns, stops := rec.snapshot()
	if len(spawns) != 1 {
		t.Fatalf("expected 1 spawn, got %d", len(spawns))
	}
	want := StartParams{
		MemberID: "m-1", PersonaContext: "you are", MemberToken: "tok-1",
		Role: "assistant", Model: "opus", SessionName: "member-m-1",
	}
	if spawns[0] != want {
		t.Fatalf("spawn params mismatch:\n got %+v\nwant %+v", spawns[0], want)
	}
	if len(stops) != 0 {
		t.Fatalf("expected 0 stops, got %d", len(stops))
	}
}

func TestHandlePayload_StopDispatches(t *testing.T) {
	rec := &recordingDeps{}
	tr, _ := newTestTransport(rec.deps())
	tr.handlePayload([]byte(`{"topic":"warden-command","data":{"rpc":"stop","args":{"session_name":"member-m-9"}}}`))

	spawns, stops := rec.snapshot()
	if len(spawns) != 0 {
		t.Fatalf("expected 0 spawns, got %d", len(spawns))
	}
	if len(stops) != 1 || stops[0] != "member-m-9" {
		t.Fatalf("expected stop of member-m-9, got %v", stops)
	}
}

func TestHandlePayload_NonWardenTopicSkipped(t *testing.T) {
	rec := &recordingDeps{}
	tr, logs := newTestTransport(rec.deps())
	tr.handlePayload([]byte(`{"topic":"member","data":{"entity":"member"}}`))

	spawns, stops := rec.snapshot()
	if len(spawns) != 0 || len(stops) != 0 {
		t.Fatalf("non-warden topic must not dispatch: spawns=%v stops=%v", spawns, stops)
	}
	if len(*logs) != 0 {
		t.Fatalf("a skipped (valid) non-command topic must not log: %v", *logs)
	}
}

// The old-warden face of a NEW server verb: a frame whose rpc this build does
// not know (exactly how a pre-update warden sees T-5f01's `update`) is logged
// + skipped — no dispatch, no crash, the reader loop keeps living. This is
// the safety contract that makes shipping new verbs fleet-wide non-breaking.
func TestHandlePayload_UnknownFutureVerbSkippedSafely(t *testing.T) {
	rec := &recordingDeps{}
	tr, logs := newTestTransport(rec.deps())
	tr.handlePayload([]byte(`{"topic":"warden-command","data":{"rpc":"verb-from-the-future","args":{"member_id":"m-1"}}}`))

	spawns, stops := rec.snapshot()
	if len(spawns) != 0 || len(stops) != 0 {
		t.Fatalf("unknown verb must not dispatch: spawns=%v stops=%v", spawns, stops)
	}
	if len(*logs) != 1 || !strings.Contains((*logs)[0], "skip malformed frame") ||
		!strings.Contains((*logs)[0], "unknown or missing rpc") {
		t.Fatalf("expected one unknown-rpc skip log line, got %v", *logs)
	}
}

func TestHandlePayload_MalformedLoggedNotCrashed(t *testing.T) {
	rec := &recordingDeps{}
	tr, logs := newTestTransport(rec.deps())
	// truncated JSON — parseCommandFrame returns (nil, err).
	tr.handlePayload([]byte(`{"topic":"warden-command","data":{"rpc":"star`))

	spawns, stops := rec.snapshot()
	if len(spawns) != 0 || len(stops) != 0 {
		t.Fatalf("malformed frame must not dispatch")
	}
	if len(*logs) != 1 || !strings.Contains((*logs)[0], "malformed") {
		t.Fatalf("expected one malformed log line, got %v", *logs)
	}
}

func TestHandlePayload_PanicInDepsRecovered(t *testing.T) {
	// A side-effecting dep that panics must NOT crash the reader: the defensive
	// recover in handlePayload catches it and logs.
	deps := CommandDeps{
		Spawn: func(StartParams) SpawnOutcome { panic("boom") },
	}
	tr, logs := newTestTransport(deps)
	tr.handlePayload([]byte(`{"topic":"warden-command","data":{"rpc":"start","args":{` +
		`"member_id":"m-1","persona_context":"p","member_token":"t"}}}`))
	// Two lines now: the pre-dispatch RECEIPT beacon (observability), then the
	// defensive recovered-panic line. The panic path must NOT emit a "dispatched OK"
	// line (dispatch panicked, never returned success).
	if len(*logs) != 2 {
		t.Fatalf("expected receipt + recovered-panic log lines, got %v", *logs)
	}
	if !strings.Contains((*logs)[0], "received start frame") {
		t.Fatalf("expected the receipt beacon first, got %v", *logs)
	}
	if !strings.Contains((*logs)[1], "recovered from panic") {
		t.Fatalf("expected a recovered-panic log line, got %v", *logs)
	}
	for _, l := range *logs {
		if strings.Contains(l, "dispatched start OK") {
			t.Fatalf("a panicking dispatch must not log success, got %v", *logs)
		}
	}
}

// ---------------------------------------------------------------------------
// end-to-end over an httptest mock SSE server — dispatch across the wire.
// ---------------------------------------------------------------------------

// mockSSEServer streams the given frames on the FIRST connection, then blocks
// holding the connection open until the request context is cancelled (so the
// client never reconnects/churns during the assertion window). Later connections
// (if any) just block. gotAuth captures the Authorization header of connection #1.
func mockSSEServer(frames []string, gotAuth *string, connectionsSeen *int32) *httptest.Server {
	var first sync.Once
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(connectionsSeen, 1)
		first.Do(func() {
			if gotAuth != nil {
				*gotAuth = r.Header.Get("Authorization")
			}
		})
		w.Header().Set("Content-Type", "text/event-stream")
		fl, ok := w.(http.Flusher)
		if !ok {
			return
		}
		fl.Flush()
		for _, f := range frames {
			_, _ = w.Write([]byte(f))
			fl.Flush()
		}
		<-r.Context().Done() // hold the stream open until the client cancels
	}))
}

func TestTransport_EndToEnd_DispatchOverWire(t *testing.T) {
	rec := &recordingDeps{}
	frames := []string{
		": connected\n\n",
		"data: {\"topic\":\"warden-command\",\"data\":{\"rpc\":\"start\",\"args\":{\"member_id\":\"m-1\",\"persona_context\":\"p\",\"member_token\":\"t\"}}}\n\n",
		": heartbeat\n\n",
		"data: {\"topic\":\"member\",\"data\":{}}\n\n", // non-command → skipped
		"data: {\"topic\":\"warden-command\",\"data\":{\"rpc\":\"stop\",\"args\":{\"session_name\":\"member-m-1\"}}}\n\n",
	}
	var auth string
	var conns int32
	srv := mockSSEServer(frames, &auth, &conns)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr, _ := newTestTransport(rec.deps())
	tr.base = srv.URL
	tr.token = "warden-tok"
	tr.client = srv.Client()
	tr.sleep = func(time.Duration) {}
	tr.backoffStart = time.Millisecond
	tr.backoffCap = time.Millisecond

	done := make(chan struct{})
	go func() { tr.run(ctx); close(done) }()

	waitFor(t, func() bool {
		s, st := rec.snapshot()
		return len(s) == 1 && len(st) == 1
	}, "1 spawn + 1 stop dispatched over the wire")

	cancel()
	<-done

	if auth != "Bearer warden-tok" {
		t.Fatalf("expected Bearer auth on the SSE GET, got %q", auth)
	}
	spawns, stops := rec.snapshot()
	if spawns[0].MemberID != "m-1" {
		t.Fatalf("wrong spawn member: %+v", spawns[0])
	}
	if stops[0] != "member-m-1" {
		t.Fatalf("wrong stop session: %q", stops[0])
	}
}

// ---------------------------------------------------------------------------
// reconnect + backoff — no real sleep; drive termination via a fake clock.
// ---------------------------------------------------------------------------

// dropServer closes the connection after emitting one frame every time (no hold),
// so the client is forced to reconnect. It counts connections.
func dropServer(frame string, conns *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(conns, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(frame))
		// handler returns → body EOF → client sees the stream end → reconnects.
	}))
}

func TestTransport_ReconnectsAfterDrop(t *testing.T) {
	var conns int32
	frame := "data: {\"topic\":\"warden-command\",\"data\":{\"rpc\":\"stop\",\"args\":{\"session_name\":\"member-x\"}}}\n\n"
	srv := dropServer(frame, &conns)
	defer srv.Close()

	rec := &recordingDeps{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fake sleep: records each backoff and cancels ctx once we have proven the
	// client reconnected (≥2 connections) — so the loop exits WITHOUT any real wait.
	var backoffs []time.Duration
	var bmu sync.Mutex
	tr, _ := newTestTransport(rec.deps())
	tr.base = srv.URL
	tr.token = "t"
	tr.client = srv.Client()
	tr.backoffStart = 5 * time.Millisecond
	tr.backoffCap = 40 * time.Millisecond
	tr.sleep = func(d time.Duration) {
		bmu.Lock()
		backoffs = append(backoffs, d)
		bmu.Unlock()
		if atomic.LoadInt32(&conns) >= 2 {
			cancel()
		}
	}

	done := make(chan struct{})
	go func() { tr.run(ctx); close(done) }()
	<-done

	if atomic.LoadInt32(&conns) < 2 {
		t.Fatalf("expected at least 2 connections (reconnect), got %d", conns)
	}
	bmu.Lock()
	defer bmu.Unlock()
	if len(backoffs) == 0 {
		t.Fatalf("expected at least one backoff sleep between reconnects")
	}
	// A healthy (opened=200) connection dropping resets backoff to start each time.
	if backoffs[0] != tr.backoffStart {
		t.Fatalf("expected first backoff to reset to start %s, got %s", tr.backoffStart, backoffs[0])
	}
}

func TestNextSSEBackoff_ExponentialCapped(t *testing.T) {
	cases := []struct{ in, want time.Duration }{
		{0, sseBackoffCap},                 // degenerate seed → jump to cap
		{sseBackoffStart, 2 * time.Second}, // 1s → 2s
		{30 * time.Second, sseBackoffCap},  // 30s → 60s (cap)
		{sseBackoffCap, sseBackoffCap},     // 60s → clamped at 60s
	}
	for _, c := range cases {
		if got := nextSSEBackoff(c.in, sseBackoffCap); got != c.want {
			t.Fatalf("nextSSEBackoff(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestTransport_Non200DoesNotResetBackoff(t *testing.T) {
	// A server that always 503s: connectOnce returns opened=false, so backoff must
	// GROW (never reset to start) across retries.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rec := &recordingDeps{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var backoffs []time.Duration
	var mu sync.Mutex
	tr, _ := newTestTransport(rec.deps())
	tr.base = srv.URL
	tr.client = srv.Client()
	tr.backoffStart = 1 * time.Millisecond
	tr.backoffCap = 100 * time.Millisecond
	tr.sleep = func(d time.Duration) {
		mu.Lock()
		backoffs = append(backoffs, d)
		n := len(backoffs)
		mu.Unlock()
		if n >= 3 {
			cancel()
		}
	}
	done := make(chan struct{})
	go func() { tr.run(ctx); close(done) }()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(backoffs) < 3 {
		t.Fatalf("expected ≥3 retries, got %d", len(backoffs))
	}
	// Strictly increasing (exponential) since it never opened → never reset.
	if !(backoffs[0] < backoffs[1] && backoffs[1] < backoffs[2]) {
		t.Fatalf("expected growing backoff on repeated non-200, got %v", backoffs)
	}
}

// ---------------------------------------------------------------------------
// --once — a single-cycle run never starts the command reader (iters==0 only).
// ---------------------------------------------------------------------------

// TestRealMain_OnceDoesNotConnect proves a --once cycle starts NO command reader —
// a mock SSE endpoint sees ZERO connections. We point OC_BASE at a server that would
// record any /api/events GET, and run a single --once cycle (which also disables the
// loop goroutines). The command reader only starts on the long-running iters==0 path.
func TestRealMain_OnceDoesNotConnect(t *testing.T) {
	var eventsHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, eventsPath) {
			atomic.AddInt32(&eventsHits, 1)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	// A valid-looking token/id so the reader's OTHER preconditions are satisfied —
	// isolating --once (iters==1) as the sole reason no connection is made.
	env := func(k string) string {
		switch k {
		case "OC_BASE":
			return srv.URL
		case "OC_TOKEN":
			return "tok"
		case "OC_ID":
			return "warden-1"
		default:
			return ""
		}
	}
	var out strings.Builder
	_ = realMain([]string{"run", "--once"}, env, &out)

	if got := atomic.LoadInt32(&eventsHits); got != 0 {
		t.Fatalf("--once must not open /api/events; saw %d connection(s)", got)
	}
	if strings.Contains(out.String(), "command reader: enabled") {
		t.Fatalf("--once must not log the reader as enabled:\n%s", out.String())
	}
}

// ---------------------------------------------------------------------------
// idle-read watchdog — silently-dead / half-open TCP liveness robustness.
// ---------------------------------------------------------------------------

// silentSSEServer accepts the SSE GET, flushes headers so the client sees an OPEN
// 200 stream, then sends NOTHING and holds the connection open until the client
// aborts it (r.Context().Done()). This is the "silently-dead / half-open" case: the
// socket looks alive but no frame — not even a heartbeat — ever arrives, so a
// deadline-less reader would block forever. It counts connections.
func silentSSEServer(conns *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(conns, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		<-r.Context().Done() // never emit a frame; wait for the client to give up
	}))
}

// TestTransport_WatchdogReconnectsOnSilentStream proves the watchdog: an open but
// silent stream (no frames at all) trips idleReadTimeout, which force-drops the
// connection into the EXISTING reconnect path — so the client re-dials (≥2 conns)
// instead of hanging deaf forever.
func TestTransport_WatchdogReconnectsOnSilentStream(t *testing.T) {
	var conns int32
	srv := silentSSEServer(&conns)
	defer srv.Close()

	rec := &recordingDeps{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tr, _ := newTestTransport(rec.deps())
	tr.base = srv.URL
	tr.client = srv.Client()
	tr.idleReadTimeout = 50 * time.Millisecond // fire fast; no real 45s wait
	tr.backoffStart = time.Millisecond
	tr.backoffCap = time.Millisecond
	// Stop the loop once we have PROVEN a reconnect (≥2 connections), so the test
	// exits without any real backoff wait.
	tr.sleep = func(time.Duration) {
		if atomic.LoadInt32(&conns) >= 2 {
			cancel()
		}
	}

	done := make(chan struct{})
	go func() { tr.run(ctx); close(done) }()

	// Bounded guard: on a watchdog regression the reader hangs deaf (conns stays 1)
	// — fail fast at 2s instead of dragging to the 10min `go test` timeout.
	waitFor(t, func() bool { return atomic.LoadInt32(&conns) >= 2 },
		"watchdog to force-drop the silent stream and reconnect (≥2 conns)")

	cancel() // reconnect proven; stop the loop and let run() exit cleanly
	<-done

	if got := atomic.LoadInt32(&conns); got < 2 {
		t.Fatalf("watchdog should have force-dropped the silent stream and reconnected; got %d connection(s)", got)
	}
}

// heartbeatSSEServer streams a `: heartbeat` comment frame every interval until the
// client disconnects — a HEALTHY keepalive-only link (officraft's real behaviour
// between commands). It counts connections.
func heartbeatSSEServer(conns *int32, interval time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(conns, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl, ok := w.(http.Flusher)
		if !ok {
			return
		}
		fl.Flush()
		tk := time.NewTicker(interval)
		defer tk.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-tk.C:
				if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
					return
				}
				fl.Flush()
			}
		}
	}))
}

// TestTransport_WatchdogNotTrippedByHeartbeats is the anti-false-positive guard: a
// steady heartbeat stream (interval << idleReadTimeout) must keep the SINGLE
// connection alive and NEVER trigger a reconnect. A heartbeat comment resets the
// watchdog just like a data frame, so a live-but-idle-of-commands link is not killed.
func TestTransport_WatchdogNotTrippedByHeartbeats(t *testing.T) {
	var conns int32
	srv := heartbeatSSEServer(&conns, 10*time.Millisecond)
	defer srv.Close()

	rec := &recordingDeps{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var backoffSleeps int32
	tr, _ := newTestTransport(rec.deps())
	tr.base = srv.URL
	tr.client = srv.Client()
	tr.idleReadTimeout = 100 * time.Millisecond // 10× the heartbeat interval
	tr.backoffStart = time.Millisecond
	tr.backoffCap = time.Millisecond
	tr.sleep = func(time.Duration) { atomic.AddInt32(&backoffSleeps, 1) }

	done := make(chan struct{})
	go func() { tr.run(ctx); close(done) }()

	// Let it run well past several idle windows while heartbeats flow, then stop.
	time.Sleep(400 * time.Millisecond)
	cancel()
	<-done

	if got := atomic.LoadInt32(&conns); got != 1 {
		t.Fatalf("heartbeats should keep exactly one connection alive; got %d connection(s)", got)
	}
	if got := atomic.LoadInt32(&backoffSleeps); got != 0 {
		t.Fatalf("no reconnect (backoff) expected while heartbeats flow; got %d", got)
	}
}

// rawSilentTCPListener is a REAL TCP server (not an httptest/pipe mock) that, on
// each accepted connection, writes a valid HTTP/1.1 200 + text/event-stream header
// block and one `: connected` comment, then goes SILENT forever — it never writes
// again and never closes the socket. This is the genuine "silent-but-open stream"
// shape: a live TCP fd with a blocked-forever downstream Read. It counts accepted
// connections and returns the base URL to point a transport at. Unlike a
// pipe/httptest mock, this exercises a real net.Conn, so ONLY a read-deadline armed
// on that net.Conn (SetReadDeadline) — not a context-cancel→Close — reliably unblocks
// the reader. The returned closer stops the accept loop and frees the listener.
func rawSilentTCPListener(t *testing.T, conns *int32) (baseURL string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	var (
		mu     sync.Mutex
		held   []net.Conn // keep accepted conns referenced so they are not GC/closed
		closed bool
	)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return // listener closed → stop accepting
			}
			atomic.AddInt32(conns, 1)
			mu.Lock()
			if closed {
				mu.Unlock()
				_ = c.Close()
				return
			}
			held = append(held, c)
			mu.Unlock()
			// Write the 200 + SSE header block and one comment, then go SILENT: never
			// write again, never close. The socket stays OPEN with nothing to read.
			_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\n" +
				"Content-Type: text/event-stream\r\n" +
				"Cache-Control: no-cache\r\n" +
				"Connection: keep-alive\r\n" +
				"\r\n" +
				": connected\n\n"))
			// intentionally no further writes, no Close
		}
	}()
	closeFn = func() {
		mu.Lock()
		closed = true
		hs := held
		held = nil
		mu.Unlock()
		_ = ln.Close()
		for _, c := range hs {
			_ = c.Close()
		}
	}
	return "http://" + ln.Addr().String(), closeFn
}

// TestTransport_ReadDeadlineReconnectsOnRealHalfOpenTCP is the REGRESSION GUARD the
// pipe/httptest-based TestTransport_WatchdogReconnectsOnSilentStream cannot provide:
// it drives the transport against a REAL net.Conn (rawSilentTCPListener) that goes
// silent-but-open forever. The idle-read watchdog must give up on the silent stream
// and RECONNECT (the listener accepts a SECOND connection) within a few ×
// idleReadTimeout. This proves the read deadline fires against a real socket, which
// the context-cancel→Close path does not reliably do on a genuine half-open TCP.
func TestTransport_ReadDeadlineReconnectsOnRealHalfOpenTCP(t *testing.T) {
	var conns int32
	baseURL, closeFn := rawSilentTCPListener(t, &conns)
	defer closeFn()

	rec := &recordingDeps{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tr, _ := newTestTransport(rec.deps())
	tr.base = baseURL
	tr.client = newSSEClient() // a REAL http.Client over a REAL TCP conn (no mock/pipe)
	tr.idleReadTimeout = 150 * time.Millisecond
	tr.backoffStart = time.Millisecond
	tr.backoffCap = time.Millisecond
	// Instant fake sleep; cancel the loop once a reconnect (≥2 conns) is proven so the
	// test never waits real backoff.
	tr.sleep = func(time.Duration) {
		if atomic.LoadInt32(&conns) >= 2 {
			cancel()
		}
	}

	done := make(chan struct{})
	go func() { tr.run(ctx); close(done) }()

	// Bounded overall guard: a few × idleReadTimeout is ample for the deadline to fire
	// and the client to re-dial. On a regression (deadline never fires against the real
	// half-open conn) conns stays 1 and this fails fast rather than hanging.
	waitFor(t, func() bool { return atomic.LoadInt32(&conns) >= 2 },
		"read-deadline to give up on the real half-open TCP and reconnect (≥2 conns)")

	cancel()
	<-done

	if got := atomic.LoadInt32(&conns); got < 2 {
		t.Fatalf("read deadline should have dropped the silent real-TCP stream and reconnected; got %d connection(s)", got)
	}
}

// waitFor polls cond up to ~2s, failing the test if it never holds.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

// ---------------------------------------------------------------------------
// resolveClaudeBin — robust claude resolution under a minimal launchd PATH
// ---------------------------------------------------------------------------

func TestResolveClaudeBin_OverrideHonored(t *testing.T) {
	// OC_CLAUDE_BIN pointing at a real executable wins over PATH/heuristics.
	env := func(k string) string {
		if k == "OC_CLAUDE_BIN" {
			return "/bin/sh"
		}
		return ""
	}
	if got := resolveClaudeBin(env); got != "/bin/sh" {
		t.Fatalf("OC_CLAUDE_BIN override must win, got %q", got)
	}
}

func TestResolveClaudeBin_OverrideIgnoredWhenNotExecutable(t *testing.T) {
	// A non-existent override must NOT be returned — it falls through to LookPath /
	// heuristics (which, in this test env with no claude, yields "").
	env := func(k string) string {
		switch k {
		case "OC_CLAUDE_BIN":
			return "/nonexistent/definitely/not/claude"
		case "HOME":
			return "/nonexistent-home"
		default:
			return ""
		}
	}
	// Only assert the bogus override is not echoed back; LookPath may or may not find
	// a system claude, so we don't assert the final value beyond "not the bogus path".
	if got := resolveClaudeBin(env); got == "/nonexistent/definitely/not/claude" {
		t.Fatalf("a non-executable override must not be returned, got %q", got)
	}
}

func TestIsExecutableFile(t *testing.T) {
	if !isExecutableFile("/bin/sh") {
		t.Fatal("/bin/sh must be seen as an executable file")
	}
	if isExecutableFile("/nonexistent/path/xyz") {
		t.Fatal("a missing path must not be executable")
	}
	if isExecutableFile("/tmp") {
		t.Fatal("a directory must not be reported as an executable file")
	}
}

// ---------------------------------------------------------------------------
// newCommandReporter — the best-effort command_result POST (fleet stage 1)
// ---------------------------------------------------------------------------

// TestCommandReporter_PostsPayload: a wired reporter POSTs {agent_id, command_result:
// {member_id, rpc, ok, reason, log, at}} to the telemetry ingest path, Bearer-authed.
func TestCommandReporter_PostsPayload(t *testing.T) {
	var gotBody map[string]any
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	report := newCommandReporter(Config{Base: srv.URL, Token: "tok-1", ID: "agent-9"})
	report(CommandResult{
		MemberID: "m-7", RPC: "stop", OK: true,
		Reason: "stopped", Log: "session=member-m-7: stopped", At: "2026-07-08T00:00:00Z",
	})

	if gotPath != commandResultPath {
		t.Fatalf("path = %q, want %q", gotPath, commandResultPath)
	}
	if gotAuth != "Bearer tok-1" {
		t.Fatalf("auth = %q, want Bearer tok-1", gotAuth)
	}
	if gotBody["agent_id"] != "agent-9" {
		t.Fatalf("agent_id = %v, want agent-9", gotBody["agent_id"])
	}
	cr, ok := gotBody["command_result"].(map[string]any)
	if !ok {
		t.Fatalf("command_result missing/not an object: %v", gotBody["command_result"])
	}
	if cr["member_id"] != "m-7" || cr["rpc"] != "stop" || cr["ok"] != true {
		t.Fatalf("command_result fields wrong: %+v", cr)
	}
	if cr["at"] != "2026-07-08T00:00:00Z" || cr["log"] != "session=member-m-7: stopped" {
		t.Fatalf("command_result at/log wrong: %+v", cr)
	}
}

// TestCommandReporter_PostsWorkerReceipt (T-9ccf): a worker receipt carries a
// worker_id and NO member_id — the reporter must POST it (not skip it as an
// unaddressed receipt) with worker_id in the command_result body, so the server
// can fold the last-op onto the durable worker row.
func TestCommandReporter_PostsWorkerReceipt(t *testing.T) {
	var gotBody map[string]any
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	report := newCommandReporter(Config{Base: srv.URL, Token: "tok-1", ID: "agent-9"})
	if err := report(CommandResult{
		WorkerID: "ow-1", RPC: "worker_start", OK: false,
		Reason: "session_already_exists: ...", Log: "session_already_exists: ...",
		At: "2026-07-08T00:00:00Z",
	}); err != nil {
		t.Fatalf("a worker receipt must be delivered (2xx), got %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("a worker receipt must POST exactly once, got %d", n)
	}
	cr, ok := gotBody["command_result"].(map[string]any)
	if !ok {
		t.Fatalf("command_result missing/not an object: %v", gotBody["command_result"])
	}
	if cr["worker_id"] != "ow-1" || cr["rpc"] != "worker_start" || cr["ok"] != false {
		t.Fatalf("worker command_result fields wrong: %+v", cr)
	}
}

// TestCommandReporter_SyncErrorSemantics: the SYNCHRONOUS reporter now RETURNS its
// delivery verdict (the uninstall self-exit gates on it) — a non-2xx and a dead server
// are ERRORS (undelivered), while an unaddressed receipt (no token/id, blank member_id/
// rpc) is a benign nil skip with no POST, and a 2xx is a delivered nil. Never panics/hangs.
func TestCommandReporter_SyncErrorSemantics(t *testing.T) {
	// ① non-2xx status → error (the server did NOT record the receipt).
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv500.Close()
	if err := newCommandReporter(Config{Base: srv500.URL, Token: "t", ID: "i"})(
		CommandResult{MemberID: "m", RPC: "stop", At: "2026-07-08T00:00:00Z"}); err == nil {
		t.Fatal("non-2xx must return an error (undelivered receipt)")
	}

	// ② dead server (connection refused) → transport error → error.
	if err := newCommandReporter(Config{Base: "http://127.0.0.1:1", Token: "t", ID: "i"})(
		CommandResult{MemberID: "m", RPC: "stop", At: "2026-07-08T00:00:00Z"}); err == nil {
		t.Fatal("a dead server must return an error (undelivered receipt)")
	}

	// ③ no token/id → skip (no POST attempted) → nil, not an error.
	var hits int32
	srvCount := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srvCount.Close()
	if err := newCommandReporter(Config{Base: srvCount.URL, Token: "", ID: ""})(
		CommandResult{MemberID: "m", RPC: "stop"}); err != nil {
		t.Fatalf("no token/id must be a benign nil skip, got %v", err)
	}
	// ④ blank member_id/rpc → skip (unaddressed receipt is noise) → nil.
	if err := newCommandReporter(Config{Base: srvCount.URL, Token: "t", ID: "i"})(
		CommandResult{MemberID: "  ", RPC: "stop"}); err != nil {
		t.Fatalf("blank member_id must be a benign nil skip, got %v", err)
	}
	if err := newCommandReporter(Config{Base: srvCount.URL, Token: "t", ID: "i"})(
		CommandResult{MemberID: "m", RPC: ""}); err != nil {
		t.Fatalf("blank rpc must be a benign nil skip, got %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("skip cases must POST nothing, got %d hits", n)
	}

	// ⑤ a 2xx → nil (delivered) — the uninstall self-exit precondition.
	srvOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srvOK.Close()
	if err := newCommandReporter(Config{Base: srvOK.URL, Token: "t", ID: "i"})(
		CommandResult{MemberID: "m", RPC: "uninstall"}); err != nil {
		t.Fatalf("a 2xx must be a delivered nil, got %v", err)
	}
}

// 方案A (T-c93d): the transport must fire onConnect on a successful (re)connect —
// main.go wires this to the self-updater's Kick so a reconnect forces an immediate
// self-update check. mockSSEServer holds the stream open, so exactly one connect
// happens in the window and the hook fires once.
func TestTransport_OnConnectFiresOnConnect(t *testing.T) {
	rec := &recordingDeps{}
	var conns int32
	srv := mockSSEServer([]string{": connected\n\n"}, nil, &conns)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr, _ := newTestTransport(rec.deps())
	tr.base = srv.URL
	tr.client = srv.Client()
	tr.sleep = func(time.Duration) {}
	tr.backoffStart = time.Millisecond
	tr.backoffCap = time.Millisecond

	kicked := make(chan struct{}, 8)
	tr.onConnect = func() { kicked <- struct{}{} }

	done := make(chan struct{})
	go func() { tr.run(ctx); close(done) }()

	select {
	case <-kicked:
	case <-time.After(2 * time.Second):
		t.Fatal("onConnect did not fire on a successful SSE connect within 2s")
	}
	cancel()
	<-done
}
