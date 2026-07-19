package main

// sse_writedeadline_test.go — the T-7e07 write-deadline BACKSTOP (api_infra.go
// sseWriteTimeout). The PRIMARY half-open reaper is TCP keepalive (server.go,
// covered in keepalive_test.go); this deadline covers the OTHER stall a
// keepalive probe would not resolve quickly: a stuck / zero-window consumer
// whose kernel send buffer has FILLED, so the next write genuinely BLOCKS. The
// deadline must turn that blocked write into a prompt error the stream loop
// reaps into Disconnect, dropping the member's online projection — all without
// r.Context() ever cancelling. Driven against the real handler over a
// ResponseWriter whose writes block, the shape of a full send buffer.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// blockedWriteConn is a ResponseWriter+Flusher whose Write never completes on
// its own — the shape of a socket whose send buffer has filled and stopped
// draining. It implements SetWriteDeadline so http.ResponseController reaches
// it exactly as it would a net.Conn:
//
//   - deadline armed (backstop active): a Write blocks only until the deadline,
//     then fails with os.ErrDeadlineExceeded — the real netpoller behaviour.
//   - no deadline (sseWriteTimeout==0, backstop disabled): a Write blocks until
//     the test releases it, modelling the indefinite block without the deadline.
type blockedWriteConn struct {
	mu       sync.Mutex
	deadline time.Time
	hdr      http.Header
	release  chan struct{}
	relOnce  sync.Once
}

func newBlockedWriteConn() *blockedWriteConn {
	return &blockedWriteConn{hdr: http.Header{}, release: make(chan struct{})}
}

func (c *blockedWriteConn) Header() http.Header { return c.hdr }
func (c *blockedWriteConn) WriteHeader(int)     {}
func (c *blockedWriteConn) Flush()              {}

func (c *blockedWriteConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return nil
}

func (c *blockedWriteConn) unblock() { c.relOnce.Do(func() { close(c.release) }) }

func (c *blockedWriteConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	dl := c.deadline
	c.mu.Unlock()
	if !dl.IsZero() {
		d := time.Until(dl)
		if d < 0 {
			d = 0
		}
		select {
		case <-time.After(d):
			return 0, os.ErrDeadlineExceeded
		case <-c.release:
			return len(p), nil
		}
	}
	<-c.release
	return 0, errors.New("blocked-write connection released")
}

// TestEventsHandlerWriteDeadlineReapsStuckConsumer pins the write-deadline
// backstop: with the deadline armed, a blocked write (full send buffer) fails
// promptly, the handler returns, and the member's online projection clears —
// without r.Context() ever cancelling. With the deadline disabled
// (sseWriteTimeout==0) nothing unblocks the write and the listener is NOT
// reaped, proving the deadline is the load-bearing part.
func TestEventsHandlerWriteDeadlineReapsStuckConsumer(t *testing.T) {
	reapWithin := func(t *testing.T, writeTimeout time.Duration) bool {
		api, dal := newGateTestAPI(t)
		putGateMember(t, dal, Member{ID: "h-1", Kind: KindAssistant,
			DesiredState: DesiredStateOnline})

		prev := sseWriteTimeout
		sseWriteTimeout = writeTimeout

		w := newBlockedWriteConn()

		req := httptest.NewRequest("GET", "/api/events", nil)
		claims := map[string]any{"sub": "h-1", "scope": "agent"}
		// The context is deliberately NOT cancelled: only the write deadline can
		// reap a connection whose write is blocked on a full send buffer.
		req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))

		done := make(chan struct{})
		go func() {
			api.HandleEventsApiEventsGet(w, req)
			close(done)
		}()

		// Always fully drain the handler goroutine before returning — unblock any
		// stuck write, join it, THEN restore the global. Joining first keeps the
		// handler off the DB / global once the subtest's cleanup runs (no race).
		reaped := false
		defer func() {
			w.unblock()
			<-done
			sseWriteTimeout = prev
		}()

		// Wait until the handler has registered the listener, then force a loop
		// write by publishing an addressed frame.
		deadline := time.Now().Add(time.Second)
		for !api.hub.IsOnline("h-1") {
			if time.Now().After(deadline) {
				t.Fatal("handler never registered the listener online")
			}
			time.Sleep(2 * time.Millisecond)
		}
		api.hub.Publish("member", "patch", "member", "owner::h-1", nil,
			audienceMembers("h-1"), "")

		select {
		case <-done:
			reaped = true
			if api.hub.IsOnline("h-1") {
				t.Fatal("a reaped connection must no longer project online")
			}
		case <-time.After(3 * time.Second):
			reaped = false
		}
		return reaped
	}

	t.Run("armed deadline reaps the stuck consumer", func(t *testing.T) {
		if !reapWithin(t, 50*time.Millisecond) {
			t.Fatal("stuck consumer was never reaped: the write deadline did not trip")
		}
	})

	t.Run("disabled deadline never reaps (the deadline is load-bearing)", func(t *testing.T) {
		if reapWithin(t, 0) {
			t.Fatal("with the deadline disabled the blocked write must NOT be reaped")
		}
	})
}
