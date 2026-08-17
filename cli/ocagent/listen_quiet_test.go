package main

import (
	"context"
	"errors"
	"io"
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
// failFirst /api/events dials are answered 500 (a server outage) before the
// stream is served.
func closingEventsServer(failFirst int32, frames []string, chatList string) *httptest.Server {
	var chatCalls, dials int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, eventsPath) && atomic.AddInt32(&dials, 1) <= failFirst {
			w.WriteHeader(500)
			return
		}
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
	srv := closingEventsServer(0, frames, `[{"id":"c1","from":"boss","to":"kyle","body":"ping"}]`)
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle"}
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.verbose = verbose
	l.once = true
	l.winddown = newWindDownHook(srv.Client(), cfg, out)
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
	"chat from boss (#c1): ping",
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
// the outage summary: ONE line per outage, on recovery.
//
// Silencing the 367 per-retry lines of a 4143-second outage without this would
// have made that outage invisible — which is the failure mode this whole ticket
// is about, just relocated. So the summary carries what those lines carried:
// how long, how many retries, and the three-state drop cause.
// ---------------------------------------------------------------------------

// runOutageCycle scripts `failures` failed dials followed by a working stream,
// with a clock that makes the outage exactly `gap` long. Quiet mode throughout.
// Returns everything printed.
func runOutageCycle(t *testing.T, failures int32, gap time.Duration, verbose bool) string {
	t.Helper()
	cfgTempDir = t.TempDir()
	frames := []string{
		"data: {\"topic\":\"task\",\"seq\":3}\n\n",
		"data: {\"topic\":\"chat\"}\n\n",
		"data: {\"topic\":\"reply_card\",\"data\":{\"key\":\"owner::rc-9\"," +
			"\"payload\":{\"id\":\"rc-9\",\"from\":\"kyle\",\"status\":\"answered\"}}}\n\n",
		"data: {\"topic\":\"context-high\",\"data\":{\"reason\":\"context usage high — hand over now\"}}\n\n",
	}
	srv := closingEventsServer(failures, frames,
		`[{"id":"c1","from":"boss","to":"kyle","body":"ping"}]`)
	defer srv.Close()

	out := &syncBuf{}
	l := newTestListener(srv, Config{Base: srv.URL, Token: "t", ID: "kyle"}, out)
	l.verbose = verbose
	l.winddown = newWindDownHook(srv.Client(), Config{Base: srv.URL, ID: "kyle"}, out)
	l.recycle = newRecycleHook(srv.Client(), Config{Base: srv.URL, ID: "kyle"}, out)
	// Exact, not approximate: the FIRST clock read is when the outage starts,
	// every later read is `gap` after it, so the reported duration is a value
	// the test can name rather than bound.
	base := time.Unix(1785717600, 0)
	var reads int
	l.clock = func() time.Time {
		reads++
		if reads == 1 {
			return base
		}
		return base.Add(gap)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	// The events only arrive AFTER recovery, and the summary is printed before
	// them, so waiting on the events means the summary has had its chance.
	waitForCond(t, func() bool {
		got := out.String()
		for _, s := range wantEventLines {
			if !strings.Contains(got, s) {
				return false
			}
		}
		return true
	}, "the listener recovered and dispatched its events")
	cancel()
	<-done
	return out.String()
}

// 陽: an outage past the threshold prints exactly one summary, and the numbers
// in it are the real ones — asserted as an exact line, not as "contains a digit".
func TestListener_LongOutagePrintsOneSummaryOnRecovery(t *testing.T) {
	got := runOutageCycle(t, 3, 6*time.Minute, false)

	want := "listen: recovered after a 6m0s outage — 3 failed reconnect attempts; " +
		"dropped with: n/a (no prior stream); last failure: unexpected status 500"
	if !strings.Contains(got, want) {
		t.Fatalf("outage summary wrong.\nwant substring: %s\ngot:\n%s", want, got)
	}
	if n := strings.Count(got, "recovered after"); n != 1 {
		t.Fatalf("one outage must print exactly 1 summary, got %d:\n%s", n, got)
	}
	// Positive control: the run really did recover and deliver every event.
	assertContainsAll(t, got, wantEventLines, "the outage summary cost us an event")
	// And the per-retry chatter is still gone — 3 failures, 0 chatter lines.
	assertNoConnNoise(t, got)
}

// 陰: a SHORT outage is the routine drop this change exists to swallow — the
// same code path, one constant below the threshold, prints nothing at all.
func TestListener_ShortOutagePrintsNothing(t *testing.T) {
	got := runOutageCycle(t, 3, 1*time.Minute, false)

	if strings.Contains(got, "recovered after") {
		t.Fatalf("a 1m outage is under the %s threshold and must stay silent:\n%s",
			listenOutageReportMin, got)
	}
	// Positive control: the run DID happen and DID recover, so the silence
	// above is a decision, not an empty buffer.
	assertContainsAll(t, got, wantEventLines, "the run never got as far as recovering")
	assertNoConnNoise(t, got)
}

// Under -verbose the per-retry lines are back, so the summary could read as
// saying the same thing twice. It is kept anyway, and printed exactly once: it
// is the ONLY line carrying the total duration and the retry count, so dropping
// it would make -verbose strictly less informative than the quiet default —
// backwards. The per-retry lines are the trace; this is the verdict.
func TestListener_VerboseKeepsExactlyOneOutageSummary(t *testing.T) {
	got := runOutageCycle(t, 3, 6*time.Minute, true)

	if n := strings.Count(got, "recovered after"); n != 1 {
		t.Fatalf("-verbose must not duplicate the summary, got %d:\n%s", n, got)
	}
	// The trace is present too — 3 failures, 3 per-retry lines.
	if n := strings.Count(got, "listen: connect failed"); n != 3 {
		t.Fatalf("-verbose must show all 3 per-retry lines, got %d:\n%s", n, got)
	}
	assertContainsAll(t, got, []string{"6m0s outage — 3 failed reconnect attempts"},
		"-verbose lost the summary's numbers")
}

// The drop cause is the three-state that diagnosed the last incident (`<nil>`
// clean close vs `unexpected EOF` hard kill vs `context canceled` watchdog).
// Losing it would leave "why does it keep dropping?" unanswerable, so each
// state is pinned to the exact text it must produce.
func TestReportOutage_QuotesTheThreeStateDropCause(t *testing.T) {
	for _, tc := range []struct {
		name string
		drop error
		want string
	}{
		{"clean close", nil, "dropped with: <nil>"},
		{"killed mid-read", io.ErrUnexpectedEOF, "dropped with: unexpected EOF"},
		{"idle watchdog", context.Canceled, "dropped with: context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &syncBuf{}
			now := time.Unix(1785717600, 0)
			l := &listener{out: out, clock: func() time.Time { return now }}

			l.noteStreamDropped(tc.drop)
			l.noteConnectFailure(errors.New("dial tcp: connection refused"))
			now = now.Add(10 * time.Minute)
			l.reportOutage()

			got := out.String()
			if !strings.Contains(got, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, got)
			}
			// The rest of the line is exact too, so a reshuffle that drops a
			// field cannot pass on the drop cause alone.
			want := "recovered after a 10m0s outage — 1 failed reconnect attempts; " +
				tc.want + "; last failure: dial tcp: connection refused"
			if !strings.Contains(got, want) {
				t.Fatalf("want substring:\n%s\ngot:\n%s", want, got)
			}
		})
	}
}

// A drop that reconnects on the FIRST try is not an outage, however long the
// wall clock says the gap was. Without this, a listener idle across a laptop
// suspend would announce an "outage" it never had.
func TestReportOutage_ADropWithNoFailedRetryIsNotAnOutage(t *testing.T) {
	out := &syncBuf{}
	now := time.Unix(1785717600, 0)
	l := &listener{out: out, clock: func() time.Time { return now }}

	l.noteStreamDropped(io.ErrUnexpectedEOF)
	now = now.Add(3 * time.Hour) // a very long gap …
	l.reportOutage()             // … but zero failed retries in between

	if got := out.String(); got != "" {
		t.Fatalf("a clean reconnect must print nothing, got:\n%s", got)
	}
	// Positive control: the SAME listener over the SAME gap, with one failed
	// retry added, does speak — so the silence above is the retry count, not a
	// dead code path or a mis-wired test clock.
	l.noteStreamDropped(io.ErrUnexpectedEOF)
	l.noteConnectFailure(errors.New("boom"))
	now = now.Add(3 * time.Hour)
	l.reportOutage()
	if !strings.Contains(out.String(), "recovered after") {
		t.Fatalf("one failed retry over the threshold must report:\n%s", out.String())
	}
}

// ---------------------------------------------------------------------------
// 4xx is a CLIENT fault (expired/wrong OC_TOKEN, wrong OC_ID) — a human must
// act, so it is never suppressed; 5xx and dial errors are the transient server
// trouble this change exists to silence. Both live on the same code path, so
// each of these tests pins one against the other.
// ---------------------------------------------------------------------------

// statusServer answers /api/events with a scripted sequence of statuses, one per
// connection, so a run loop can be walked through a status CHANGE.
func statusServer(codes []int, conns *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("[]"))
			return
		}
		i := int(atomic.AddInt32(conns, 1)) - 1
		if i >= len(codes) {
			i = len(codes) - 1
		}
		w.WriteHeader(codes[i])
	}))
}

// runStatusLoop drives the run loop until it has seen `want` /api/events dials,
// then cancels. Quiet mode throughout — the default is what is under test.
func runStatusLoop(t *testing.T, codes []int, want int32) string {
	t.Helper()
	cfgTempDir = t.TempDir()
	var conns int32
	srv := statusServer(codes, &conns)
	defer srv.Close()

	out := &syncBuf{}
	l := newTestListener(srv, Config{Base: srv.URL, Token: "t", ID: "kyle"}, out)
	l.verbose = false

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	waitForCond(t, func() bool { return atomic.LoadInt32(&conns) >= want },
		"the run loop kept dialing the scripted statuses")
	cancel()
	<-done
	return out.String()
}

// 陽: a 401 speaks. 陰: a 500 on the identical path stays quiet.
func TestListener_ClientFaultSpeaksServerFaultStaysQuiet(t *testing.T) {
	for _, tc := range []struct {
		name  string
		code  int
		voice bool
	}{
		{"401 expired token", 401, true},
		{"403 wrong member", 403, true},
		{"500 server having a bad day", 500, false},
		{"503 server restarting", 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runStatusLoop(t, []int{tc.code}, 3)
			has := strings.Contains(got, "server rejected the SSE")
			if has != tc.voice {
				t.Fatalf("status %d printed the actionable notice = %v want %v:\n%s",
					tc.code, has, tc.voice, got)
			}
			// Either way the transport chatter stays suppressed.
			if strings.Contains(got, "listen: connect failed") {
				t.Fatalf("connect-failed chatter must stay quiet:\n%s", got)
			}
		})
	}
}

// The dedup, both directions: an UNCHANGING 4xx says its piece exactly once no
// matter how many retries, and a CHANGE speaks again. Without the second half
// the dedup could be "print once ever", which would hide a real escalation.
func TestListener_ClientFaultIsDedupedOnStatusChangeOnly(t *testing.T) {
	t.Run("same status N times prints once", func(t *testing.T) {
		got := runStatusLoop(t, []int{401}, 5)
		if n := strings.Count(got, "server rejected the SSE"); n != 1 {
			t.Fatalf("a persistent 401 must print exactly 1 line, got %d:\n%s", n, got)
		}
		if !strings.Contains(got, "with 401") {
			t.Fatalf("the notice must name the status:\n%s", got)
		}
	})
	t.Run("a changed status speaks again", func(t *testing.T) {
		got := runStatusLoop(t, []int{401, 401, 403, 403}, 5)
		if n := strings.Count(got, "server rejected the SSE"); n != 2 {
			t.Fatalf("401→403 must print 2 lines (one per distinct status), got %d:\n%s", n, got)
		}
		assertContainsAll(t, got, []string{"with 401", "with 403"},
			"the dedup swallowed a status change")
	})
}

// 409 keeps its own louder path (foldRefusal → the fail-closed alarm) and must
// NOT also come out of the 4xx notice — that would be the same event twice.
func TestListener_409IsNotDoubleReportedAsAClientFault(t *testing.T) {
	if code, actionable := clientFault(&sseStatusError{code: 409}); actionable {
		t.Fatalf("409 must not route to the 4xx notice (got code=%d)", code)
	}
	// Positive control: the same helper DOES classify its neighbours, so the
	// assertion above is not passing because clientFault always says false.
	if _, actionable := clientFault(&sseStatusError{code: 401}); !actionable {
		t.Fatal("clientFault must classify 401 as actionable")
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
