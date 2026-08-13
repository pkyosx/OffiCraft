package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// quiet-by-default connection status (logConnf).
//
// The runtime treats EVERY stdout line as an event notification, so each
// connection-status line wakes the agent and re-reads its whole context. These
// tests pin BOTH halves of the bargain: the four transport lines stop being
// printed, and NOTHING else does — every event body, every actionable alarm,
// stays on stdout at default verbosity.
//
// Every negative assertion below is paired with a positive control in the SAME
// run: "the bad line is absent" proves nothing on its own (a listener that
// printed nothing at all would pass it).
// ---------------------------------------------------------------------------

// The four lines this change suppresses by default.
var connNoiseSubstrings = []string{
	"listen: connected",
	"listen: stream ended",
	"listen: connect refused",
	"listen: connect failed",
}

func assertNoConnNoise(t *testing.T, got string) {
	t.Helper()
	for _, s := range connNoiseSubstrings {
		if strings.Contains(got, s) {
			t.Fatalf("connection noise %q survived the default quiet mode:\n%s", s, got)
		}
	}
}

func assertContainsAll(t *testing.T, got string, want []string, what string) {
	t.Helper()
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Fatalf("%s: missing %q from:\n%s", what, s, got)
		}
	}
}

// THE guard experiment, at the layer the guard lives on: one listener, two
// calls, one flag. A logConnf line must vanish and a logf line must survive —
// run twice with the flag flipped so the difference is attributable to the
// flag and to nothing else.
func TestLogConnf_SuppressesOnlyItselfAndOnlyWhenQuiet(t *testing.T) {
	for _, tc := range []struct {
		name        string
		verbose     bool
		wantConnLog bool
	}{
		{"quiet (default)", false, false},
		{"-verbose", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &syncBuf{}
			l := &listener{out: out, verbose: tc.verbose}

			l.logConnf("listen: connect failed: %v", "dial tcp: refused")
			l.logf("listen: tmux session gone (%d consecutive misses)", 2)

			got := out.String()
			// Positive control: the un-classified line is ALWAYS printed, so a
			// listener that simply wrote nothing cannot pass this test.
			if !strings.Contains(got, "tmux session gone") {
				t.Fatalf("logf must never be suppressed, got:\n%s", got)
			}
			if hasConn := strings.Contains(got, "connect failed"); hasConn != tc.wantConnLog {
				t.Fatalf("logConnf printed = %v want %v (verbose=%v):\n%s",
					hasConn, tc.wantConnLog, tc.verbose, got)
			}
		})
	}
}

// closingEventsServer streams `frames` and then RETURNS from the handler, so the
// body closes and scanSSE reports the stream ended — the real "connected …
// stream ended" cycle, with no second long-lived connection and no real network
// beyond loopback.
func closingEventsServer(frames []string, chatList string) *httptest.Server {
	var chatCalls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/chat"):
			w.WriteHeader(200)
			if atomic.AddInt32(&chatCalls, 1) == 1 {
				_, _ = w.Write([]byte("[]")) // silent boot baseline
			} else {
				_, _ = w.Write([]byte(chatList))
			}
		case strings.HasPrefix(r.URL.Path, "/api/reply-cards/"):
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"rc-9","from":"kyle","status":"answered",
				"summary":"ship it?","options":["ship","hold"],
				"answer":{"option_idx":0,"text":"","attachments":[]}}`))
		case strings.HasPrefix(r.URL.Path, eventsPath):
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
		default:
			w.WriteHeader(404)
		}
	}))
}

// runOneConnectCycle drives ONE full connect → dispatch → stream-ended cycle
// (l.once) against the local mock and returns everything printed.
func runOneConnectCycle(t *testing.T, verbose bool) string {
	t.Helper()
	cfgTempDir = t.TempDir()
	frames := []string{
		": connected\n\n",
		"data: {\"topic\":\"task\",\"seq\":3}\n\n",
		"data: {\"topic\":\"chat\"}\n\n",
		"data: {\"topic\":\"reply_card\",\"data\":{\"key\":\"owner::rc-9\"," +
			"\"payload\":{\"id\":\"rc-9\",\"from\":\"kyle\",\"status\":\"answered\"}}}\n\n",
		"data: {\"topic\":\"context-high\",\"data\":{\"reason\":\"context usage high — hand over now\"}}\n\n",
	}
	srv := closingEventsServer(frames, `[{"id":"c1","from":"boss","to":"kyle","body":"ping"}]`)
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle"}
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.verbose = verbose
	l.once = true
	l.winddown = newWindDownHook(srv.Client(), cfg, noEnv, out)
	l.recycle = newRecycleHook(srv.Client(), cfg, out)

	if rc := l.run(context.Background()); rc != 0 {
		t.Fatalf("rc = %d want 0", rc)
	}
	return out.String()
}

// The event bodies that MUST survive the change — a work wake, a chat refetch,
// a reply-card wake and a DIRECTED band signal (the handover SOP class), i.e.
// one of each thing the agent is woken for.
var wantEventLines = []string{
	"wake seq=3 topic=task",
	"chat from boss (id): ping",
	`reply-card rc-9 answered: picked [0] "ship" | asked: ship it?`,
	"signal context-high: context usage high — hand over now",
}

// Default (quiet): the four transport lines are gone AND every event body is
// still there. The event assertions are the positive control — they are what
// distinguishes "the noise was filtered" from "the listener went silent".
func TestListener_QuietByDefault_DropsConnNoiseKeepsEveryEvent(t *testing.T) {
	got := runOneConnectCycle(t, false)
	assertContainsAll(t, got, wantEventLines, "quiet mode lost an event body")
	assertNoConnNoise(t, got)
}

// -verbose puts the transport lines back, on the same cycle that produced none
// of them above — so the difference is the flag and only the flag.
func TestListener_VerboseRestoresConnNoise(t *testing.T) {
	got := runOneConnectCycle(t, true)
	assertContainsAll(t, got, wantEventLines, "-verbose lost an event body")
	assertContainsAll(t, got,
		[]string{"listen: connected", "listen: stream ended"},
		"-verbose failed to restore the connection-status lines")
}

// A dial failure (server down — the outage that motivated all this) is quiet by
// default and printed under -verbose.
func TestListener_ConnectFailedIsQuietByDefault(t *testing.T) {
	for _, tc := range []struct {
		name          string
		verbose, want bool
	}{
		{"quiet (default)", false, false},
		{"-verbose", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfgTempDir = t.TempDir()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			dead := srv.URL
			srv.Close() // nothing listens on `dead` any more ⇒ every dial fails

			out := &syncBuf{}
			l := newTestListener(srv, Config{Base: dead, Token: "t", ID: "kyle"}, out)
			l.verbose = tc.verbose
			l.once = true

			if rc := l.run(context.Background()); rc != 0 {
				t.Fatalf("rc = %d want 0", rc)
			}
			got := out.String()
			if has := strings.Contains(got, "listen: connect failed"); has != tc.want {
				t.Fatalf("connect-failed printed = %v want %v:\n%s", has, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// what must NEVER be suppressed: the alarms a human has to act on.
// ---------------------------------------------------------------------------

// A 409 stop-gate refusal storm: the per-attempt line is noise and goes, but the
// fail-closed self-termination ALARM stays — and still quotes the server's
// reason verbatim, so quiet mode never masks WHY the listener killed itself.
func TestListener_FailClosedRefusalAlarmSurvivesQuietMode(t *testing.T) {
	cfgTempDir = t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("[]"))
			return
		}
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"member 'kyle' has a stop in effect"}}`))
	}))
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.verbose = false // the default — the whole point
	l.refusalGraceSpan = 0
	var terminated int32
	l.selfTerminate = func() { atomic.AddInt32(&terminated, 1) }

	done := make(chan int, 1)
	go func() { done <- l.run(context.Background()) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not fail-closed within 3s of persistent 409s")
	}

	got := out.String()
	if atomic.LoadInt32(&terminated) != 1 {
		t.Fatalf("the suicide seam must still fire in quiet mode:\n%s", got)
	}
	assertContainsAll(t, got, []string{
		"fail-closed",
		"self-terminating",
		"stop in effect", // the server's reason, not masked
	}, "the 409 fail-closed alarm was damaged by quiet mode")
	// Only the repeated per-attempt line is gone.
	if strings.Contains(got, "listen: connect refused") {
		t.Fatalf("the per-attempt refusal noise should be quiet:\n%s", got)
	}
}

// The self-exit alarms (tmux session gone / session unverifiable) go through
// logf, so quiet mode must leave them completely alone. Suppressing these would
// turn the zombie detector off silently.
func TestListener_SelfExitAlarmsSurviveQuietMode(t *testing.T) {
	t.Run("tmux session gone", func(t *testing.T) {
		out := &syncBuf{}
		l := &listener{out: out, verbose: false, clock: time.Now}
		l.probe = func() probeVerdict { return probeGone }
		l.miss = sessionMissLimit - 1
		if !l.foldProbe() {
			t.Fatal("probe must trip the self-exit")
		}
		if !strings.Contains(out.String(), "tmux session gone") {
			t.Fatalf("the self-exit alarm was suppressed:\n%s", out.String())
		}
	})
	t.Run("session unverifiable", func(t *testing.T) {
		out := &syncBuf{}
		now := time.Unix(1785717600, 0)
		l := &listener{out: out, verbose: false, probeUnknownSpan: 0}
		l.clock = func() time.Time { now = now.Add(time.Minute); return now }
		l.probe = func() probeVerdict { return probeUnknown }
		var tripped bool
		for i := 0; i < probeUnknownMin && !tripped; i++ {
			tripped = l.foldProbe()
		}
		if !tripped {
			t.Fatal("the unknown run must trip the fail-closed self-exit")
		}
		if !strings.Contains(out.String(), "session unverifiable") {
			t.Fatalf("the fail-closed alarm was suppressed:\n%s", out.String())
		}
	})
}

// The mis-wire notice is a CONFIGURATION error a human must fix; it is printed
// before any listener exists and must survive the default quiet path end to end
// (cmdListen is what main.go calls).
func TestCmdListen_MisWireNoticeSurvivesQuietMode(t *testing.T) {
	out := &syncBuf{}
	if rc := cmdListen(Config{ID: "kyle"}, noEnv, false, false, out); rc != 0 {
		t.Fatalf("rc = %d want 0", rc)
	}
	if !strings.Contains(out.String(), "no OC_ID/OC_TOKEN") {
		t.Fatalf("the config-error notice was suppressed:\n%s", out.String())
	}
}
