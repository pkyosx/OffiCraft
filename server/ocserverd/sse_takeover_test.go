package main

// sse_takeover_test.go — the T-b315 dual-SSE takeover at the HANDLER seam
// (spec/sse.md §5.1). The hub-level atomic handover + throttle is pinned in
// hub_test.go; this file pins what the hub cannot see:
//
//   * the half-open zombie (the prod root cause): a connection whose peer —
//     the local cloudflared — stays healthy, so neither TCP keepalive nor the
//     write deadline nor r.Context() ever fires. A reconnect must take the
//     slot over and the zombie handler must return within the poll cadence
//     (the kicked channel), with the online projection never flickering;
//   * the §5.2 edge hooks staying OFF across a takeover (the kicked
//     listener's Disconnect reports last=false) and firing exactly once on
//     the real last disconnect;
//   * the throttle surfacing as the pre-stream 409 with the THROTTLED
//     message while the incumbent keeps its stream;
//   * the stop gate outranking takeover: a stop-in-effect member's reconnect
//     is refused BEFORE hub.Connect and kicks nobody.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// sinkWriter is a ResponseWriter+Flusher whose writes always SUCCEED and whose
// request context never cancels on its own — the exact shape of the prod
// zombie: the server-side socket peer (cloudflared) keeps reading heartbeats
// forever while the real client is long gone. No SetWriteDeadline method, so
// the backstop deadline is inert (ResponseController returns ErrNotSupported),
// just like a peer that never stops draining.
type sinkWriter struct {
	mu   sync.Mutex
	hdr  http.Header
	code int
	body strings.Builder
}

func newSinkWriter() *sinkWriter { return &sinkWriter{hdr: http.Header{}, code: 0} }

func (w *sinkWriter) Header() http.Header { return w.hdr }
func (w *sinkWriter) Flush()              {}

func (w *sinkWriter) WriteHeader(code int) {
	w.mu.Lock()
	if w.code == 0 {
		w.code = code
	}
	w.mu.Unlock()
}

func (w *sinkWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.code == 0 {
		w.code = http.StatusOK
	}
	w.body.Write(p)
	return len(p), nil
}

func (w *sinkWriter) status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.code
}

func (w *sinkWriter) bodyText() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

// agentEventsRequest builds a claims-injected GET /api/events for member sub.
func agentEventsRequest(ctx context.Context, sub string) *http.Request {
	req := httptest.NewRequest("GET", "/api/events", nil)
	claims := map[string]any{"sub": sub, "scope": "agent"}
	return req.WithContext(context.WithValue(ctx, claimsContextKey, claims))
}

// startEventsHandler runs the handler in a goroutine; the returned channel
// closes when the handler returns.
func startEventsHandler(api *apiServer, w http.ResponseWriter, r *http.Request) chan struct{} {
	done := make(chan struct{})
	go func() {
		api.HandleEventsApiEventsGet(w, r)
		close(done)
	}()
	return done
}

func waitOnline(t *testing.T, api *apiServer, member string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !api.hub.IsOnline(member) {
		if time.Now().After(deadline) {
			t.Fatalf("member %q never came online", member)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestEventsTakeoverReleasesZombieHandler pins the core prod scenario at the
// handler seam: a zombie connection (writes succeed, context never cancels —
// nothing in T-7e07's two layers can reap it) holds the slot; a reconnect for
// the same member must be ADMITTED (not 409), the zombie handler must return
// within the ssePoll cadence via the kicked channel, and the member must stay
// online throughout (no presence flicker for reconcile to trip on: reconcile's
// only input here is hub.IsOnline, asserted continuously true).
func TestEventsTakeoverReleasesZombieHandler(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "tk-1", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})

	wOld := newSinkWriter()
	ctxOld, cancelOld := context.WithCancel(context.Background())
	defer cancelOld()
	doneOld := startEventsHandler(api, wOld, agentEventsRequest(ctxOld, "tk-1"))
	waitOnline(t, api, "tk-1")

	wNew := newSinkWriter()
	ctxNew, cancelNew := context.WithCancel(context.Background())
	doneNew := startEventsHandler(api, wNew, agentEventsRequest(ctxNew, "tk-1"))

	// The zombie handler must be released promptly (≤ssePoll + slack) —
	// WITHOUT its context ever cancelling.
	select {
	case <-doneOld:
	case <-time.After(2 * time.Second):
		t.Fatal("the displaced zombie handler was not released within the deadline")
	}
	if !api.hub.IsOnline("tk-1") {
		t.Fatal("the member must stay online across the takeover (the new connection holds)")
	}
	// The new connection is admitted with 200 + greeting (poll: the hub-side
	// handover is atomic, but the new handler writes its headers a beat later).
	greetDeadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(wNew.bodyText(), ": connected") {
		if time.Now().After(greetDeadline) {
			t.Fatalf("the takeover connection never greeted: status=%d body=%q",
				wNew.status(), wNew.bodyText())
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := wNew.status(); got != http.StatusOK {
		t.Fatalf("the takeover connection must be admitted with 200, got %d", got)
	}
	// Deltas now land on the NEW connection.
	api.hub.Publish("member", "patch", "member", "owner::tk-1", nil,
		audienceMembers("tk-1"), "")
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(wNew.bodyText(), `"topic":"member"`) {
		if time.Now().After(deadline) {
			t.Fatal("the post-takeover delta never reached the new connection")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancelNew()
	select {
	case <-doneNew:
	case <-time.After(2 * time.Second):
		t.Fatal("new handler did not exit on context cancel")
	}
	if api.hub.IsOnline("tk-1") {
		t.Fatal("closing the LAST connection must project offline")
	}
}

// TestEventsTakeoverEdgeHooksGated pins §5.2 across a takeover: the kicked
// listener's teardown must NOT bank the live telemetry cost (the member is
// still online under the new connection); the REAL last disconnect banks it
// exactly once.
func TestEventsTakeoverEdgeHooksGated(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "tk-2", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})

	doneOld := startEventsHandler(api, newSinkWriter(),
		agentEventsRequest(context.Background(), "tk-2"))
	waitOnline(t, api, "tk-2")
	// Live telemetry cost arrives while the first connection holds the slot.
	api.telemetry.Set("tk-2", map[string]any{"cost": 1.25})

	ctxNew, cancelNew := context.WithCancel(context.Background())
	doneNew := startEventsHandler(api, newSinkWriter(),
		agentEventsRequest(ctxNew, "tk-2"))
	select {
	case <-doneOld:
	case <-time.After(2 * time.Second):
		t.Fatal("displaced handler not released")
	}
	// The kicked listener's teardown ran (handler returned) — the cost must
	// NOT have been banked: the member is still online.
	if m, err := dal.GetMember("tk-2"); err != nil || m == nil || m.BankedCost != 0 {
		t.Fatalf("takeover must not bank the live cost (member still online): %+v %v", m, err)
	}
	if entry := api.telemetry.Get("tk-2"); entry == nil || entry["cost"] != 1.25 {
		t.Fatalf("the live cost entry must survive the takeover: %v", entry)
	}
	// Real last disconnect: banked exactly once.
	cancelNew()
	<-doneNew
	deadline := time.Now().Add(2 * time.Second)
	for {
		m, err := dal.GetMember("tk-2")
		if err == nil && m != nil && m.BankedCost == 1.25 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("last disconnect must bank the cost exactly once: %+v %v", m, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if entry := api.telemetry.Get("tk-2"); entry["cost"] != nil {
		t.Fatalf("the live cost must be popped after banking: %v", entry)
	}
}

// TestEventsTakeoverThrottled409 pins the anti-flap fallback at the handler
// seam: after the burst is spent, the next connect gets the pre-stream 409
// with the THROTTLED message (distinct from the old blanket refusal) and the
// incumbent keeps its stream.
func TestEventsTakeoverThrottled409(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "tk-3", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})

	var cancels []context.CancelFunc
	var dones []chan struct{}
	defer func() {
		for _, c := range cancels {
			c()
		}
		for _, d := range dones {
			<-d
		}
	}()
	// waitGen sequences the concurrent handlers deterministically: the i-th
	// admit bumps the hub's connection generation to i.
	waitGen := func(want int64) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			api.hub.mu.Lock()
			g := api.hub.connGen
			api.hub.mu.Unlock()
			if g >= want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("connection generation never reached %d (at %d)", want, g)
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	// First connect + takeoverBurst takeovers, all admitted.
	for i := 0; i <= takeoverBurst; i++ {
		w := newSinkWriter()
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		dones = append(dones, startEventsHandler(api, w, agentEventsRequest(ctx, "tk-3")))
		waitGen(int64(i + 1))
	}
	// Over budget: pre-stream 409, throttled wording, incumbent untouched.
	w := newSinkWriter()
	api.HandleEventsApiEventsGet(w, agentEventsRequest(context.Background(), "tk-3"))
	if w.status() != http.StatusConflict {
		t.Fatalf("over-budget connect must answer 409, got %d", w.status())
	}
	if !strings.Contains(w.bodyText(), "takeover throttled") {
		t.Fatalf("the refusal must carry the THROTTLED wording: %s", w.bodyText())
	}
	if !api.hub.IsOnline("tk-3") {
		t.Fatal("the incumbent connection must survive the throttled refusal")
	}
}

// TestEventsStopGateOutranksTakeover pins the gate ordering: a stop-in-effect
// member's reconnect answers the stop-gate 409 BEFORE hub.Connect runs — it
// never takes over (and so never kicks) an existing listener.
func TestEventsStopGateOutranksTakeover(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "tk-4", Kind: KindAssistant,
		DesiredState: DesiredStateOffline, StoppingSince: 1.0})

	incumbent, err := api.hub.Connect("tk-4", "")
	if err != nil {
		t.Fatalf("seed listener: %v", err)
	}
	w := newSinkWriter()
	api.HandleEventsApiEventsGet(w, agentEventsRequest(context.Background(), "tk-4"))
	if w.status() != http.StatusConflict {
		t.Fatalf("stop-in-effect reconnect must answer 409, got %d", w.status())
	}
	if !strings.Contains(w.bodyText(), "stop in effect") {
		t.Fatalf("the refusal must be the STOP-GATE message, not the throttle: %s", w.bodyText())
	}
	select {
	case <-incumbent.kicked:
		t.Fatal("a stop-gated connect must never kick the existing listener")
	default:
	}
	if !api.hub.IsOnline("tk-4") {
		t.Fatal("the existing listener must be untouched")
	}
	api.hub.Disconnect(incumbent)
}

// TestEventsTakeoverHalfOpenRealTCP is the end-to-end shape of the prod
// incident over a REAL loopback socket: client A opens the stream, reads the
// `: connected` greeting, then goes silent WITHOUT closing its socket (the
// kernel keeps ACKing — a cloudflared-shaped peer). Client B reconnects for
// the same member: B must get 200 + `: connected` (not 409), A's handler must
// return promptly, and A's response body must be TERMINATED by the server
// (EOF / terminal chunk on the wire) — the origin-side stream teardown that
// also reclaims the tunnel stream in prod.
func TestEventsTakeoverHalfOpenRealTCP(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "tk-5", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})

	var calls int32
	firstDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		claims := map[string]any{"sub": "tk-5", "scope": "agent"}
		r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
		api.HandleEventsApiEventsGet(w, r)
		if n == 1 {
			close(firstDone)
		}
	}))
	defer ts.Close()

	// Client A: raw TCP, hand-rolled GET, read up to the greeting, then stop
	// reading but keep the socket open (half-open zombie shape).
	connA, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	defer connA.Close()
	fmt.Fprintf(connA, "GET /api/events HTTP/1.1\r\nHost: t\r\n\r\n")
	readerA := bufio.NewReader(connA)
	_ = connA.SetReadDeadline(time.Now().Add(3 * time.Second))
	greeted := false
	for !greeted {
		line, err := readerA.ReadString('\n')
		if err != nil {
			t.Fatalf("A never saw the greeting: %v", err)
		}
		greeted = strings.Contains(line, ": connected")
	}
	waitOnline(t, api, "tk-5")

	// Client B: a real streaming GET for the same member — must be admitted.
	req, _ := http.NewRequest("GET", ts.URL+"/api/events", nil)
	respB, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("B connect: %v", err)
	}
	defer respB.Body.Close()
	if respB.StatusCode != http.StatusOK {
		t.Fatalf("takeover must admit B with 200, got %d", respB.StatusCode)
	}
	readerB := bufio.NewReader(respB.Body)
	line, err := readerB.ReadString('\n')
	if err != nil || !strings.Contains(line, ": connected") {
		t.Fatalf("B must receive the greeting: %q %v", line, err)
	}

	// A's handler returns promptly (kicked), without A's socket ever closing.
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the half-open zombie handler was not released after the takeover")
	}
	if !api.hub.IsOnline("tk-5") {
		t.Fatal("the member must stay online under B")
	}
	// A's response is terminated by the server: draining A's socket reaches
	// the terminal chunk / EOF within the deadline (never blocks forever).
	_ = connA.SetReadDeadline(time.Now().Add(3 * time.Second))
	terminated := false
	for !terminated {
		line, err := readerA.ReadString('\n')
		if err == io.EOF || strings.TrimSpace(line) == "0" && err == nil {
			terminated = true
			break
		}
		if err != nil {
			t.Fatalf("A's stream neither terminated nor kept draining: %v", err)
		}
	}
}
