package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureSSEStderr makes the operator-facing detach line an assertion surface
// without changing production logging. SSE tests in this file deliberately do
// not run in parallel because os.Stderr is process-global.
func captureSSEStderr(t *testing.T, fn func()) (out string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	var buf bytes.Buffer
	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(copyDone)
	}()
	defer func() {
		os.Stderr = old
		_ = w.Close()
		<-copyDone
		_ = r.Close()
		out = buf.String()
	}()
	fn()
	return
}

// assertDetachLine checks the complete field vector, not a substring. The
// member/gen/last fields are the pre-existing evidence and reason is the one
// new exact field; a near miss such as "not-peer-closed" must fail.
func assertDetachLine(t *testing.T, logText, member string, gen int64, last bool, reason string) {
	t.Helper()
	wantLast := "last=false"
	if last {
		wantLast = "last=true"
	}
	want := []string{
		"[sse]",
		"detach",
		"member=" + member,
		"gen=" + strconv.FormatInt(gen, 10),
		wantLast,
		"reason=" + reason,
	}
	for _, line := range strings.Split(logText, "\n") {
		fields := strings.Fields(line)
		if len(fields) == len(want) && len(fields) >= 3 && fields[0] == "[sse]" &&
			fields[1] == "detach" && fields[2] == "member="+member {
			if !sameStrings(fields, want) {
				t.Fatalf("detach fields = %v, want exact %v\nfull log:\n%s", fields, want, logText)
			}
			return
		}
	}
	t.Fatalf("missing exact detach line %v\nfull log:\n%s", want, logText)
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// failAfterGreetingWriter lets the greeting through and fails the first
// stream frame. That reaches the real write closure, not a synthetic helper.
type failAfterGreetingWriter struct {
	mu     sync.Mutex
	hdr    http.Header
	code   int
	writes int
}

func newFailAfterGreetingWriter() *failAfterGreetingWriter {
	return &failAfterGreetingWriter{hdr: http.Header{}}
}

func (w *failAfterGreetingWriter) Header() http.Header { return w.hdr }
func (w *failAfterGreetingWriter) Flush()              {}

func (w *failAfterGreetingWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.code == 0 {
		w.code = code
	}
}

func (w *failAfterGreetingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	if w.writes > 1 {
		return 0, errors.New("test write failure")
	}
	return len(p), nil
}

func waitDone(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not finish", what)
	}
}

// Mutant proof: changing sseContextDetachReason's non-shutdown return to
// station-shutdown makes this exact-field assertion red.
func TestSSEDetachReasonPeerClosed(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "detach-peer", Kind: KindStaff,
		DesiredState: DesiredStateOnline})

	logText := captureSSEStderr(t, func() {
		rec := doEvents(api, "detach-peer")
		if rec.Code != http.StatusOK {
			t.Fatalf("admitted stream: want 200, got %d", rec.Code)
		}
	})
	assertDetachLine(t, logText, "detach-peer", 1, true, sseDetachReasonPeerClosed)
}

// Mutant proof: changing every kicked branch's takeover assignment to
// peer-closed makes this exact-field assertion red.
func TestSSEDetachReasonTakeover(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "detach-takeover", Kind: KindStaff,
		DesiredState: DesiredStateOnline})

	logText := captureSSEStderr(t, func() {
		oldCtx, cancelOld := context.WithCancel(context.Background())
		defer cancelOld()
		oldDone := startEventsHandler(api, newSinkWriter(),
			agentEventsRequest(oldCtx, "detach-takeover"))
		waitOnline(t, api, "detach-takeover")

		newCtx, cancelNew := context.WithCancel(context.Background())
		newDone := startEventsHandler(api, newSinkWriter(),
			agentEventsRequest(newCtx, "detach-takeover"))
		waitDone(t, oldDone, "takeover incumbent")
		cancelNew()
		waitDone(t, newDone, "takeover replacement")
	})
	assertDetachLine(t, logText, "detach-takeover", 1, false, sseDetachReasonTakeover)
}

// Mutant proof: changing the stream write-error assignment to peer-closed
// makes this exact-field assertion red.
func TestSSEDetachReasonWriteFailed(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "detach-write", Kind: KindStaff,
		DesiredState: DesiredStateOnline})

	logText := captureSSEStderr(t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := startEventsHandler(api, newFailAfterGreetingWriter(),
			agentEventsRequest(ctx, "detach-write"))
		waitOnline(t, api, "detach-write")
		api.hub.Publish("member", "patch", "member", "owner::detach-write", nil,
			audienceMembers("detach-write"), "")
		waitDone(t, done, "write-failure stream")
	})
	assertDetachLine(t, logText, "detach-write", 1, true, sseDetachReasonWriteFailed)
}

// Mutant proof: making markStationShutdown store false makes this exact-field
// assertion red.
func TestSSEDetachReasonStationShutdown(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "detach-station", Kind: KindStaff,
		DesiredState: DesiredStateOnline})

	logText := captureSSEStderr(t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := startEventsHandler(api, newSinkWriter(),
			agentEventsRequest(ctx, "detach-station"))
		waitOnline(t, api, "detach-station")
		api.markStationShutdown()
		cancel()
		waitDone(t, done, "station-shutdown stream")
	})
	assertDetachLine(t, logText, "detach-station", 1, true, sseDetachReasonStationShutdown)
}

// The upgrade path marks the station and then re-execs; it never cancels the
// request context, because syscall.Exec replaces the process image outright.
// So the loop-top stationShuttingDown check is not decoration. What THIS test
// proves is narrower than the motivation, and the two must not be conflated
// (T-3b4e review): delete the early exit and this test goes red because the
// HANDLER NEVER FINISHES — the detach line still gets printed, because the
// harness tears the stream down afterwards. That a real upgrade would leave the
// operator with NO detach line at all is an INFERENCE from syscall.Exec
// replacing the process image before any defer can run; nothing here measures
// it. Deleting that early exit leaves the rest of this package green — this
// test is the only thing that turns red.
func TestSSEDetachReasonStationShutdownWithoutContextCancel(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "detach-upgrade", Kind: KindStaff,
		DesiredState: DesiredStateOnline})

	logText := captureSSEStderr(t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := startEventsHandler(api, newSinkWriter(),
			agentEventsRequest(ctx, "detach-upgrade"))
		waitOnline(t, api, "detach-upgrade")
		// No cancel(): the request context stays live, exactly as it does
		// between scheduleUpgradeRestart's mark and the re-exec.
		api.markStationShutdown()
		waitDone(t, done, "station-shutdown stream with a live request context")
	})
	assertDetachLine(t, logText, "detach-upgrade", 1, true, sseDetachReasonStationShutdown)
}
