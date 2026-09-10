package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

// stubTransport answers each dial from replies[n] / errs[n] in order (the last
// entry repeats) and keeps every request it was handed.
type stubTransport struct {
	reply func(attempt int, req *http.Request) (*http.Response, error)
	reqs  []*http.Request
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n := len(s.reqs)
	s.reqs = append(s.reqs, req)
	return s.reply(n, req)
}

func sseReply(status int, header http.Header, body string) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
}

// runCfg is the config every listener below is built from: a configured base,
// so the T-89 origin segment is absent unless a case asks for it.
func runCfg(t *testing.T) Config {
	t.Helper()
	return Config{
		Base: "http://station", BaseConfigured: true,
		Token: "tok", ID: "kyle", Home: t.TempDir(),
	}
}

// quietAPI answers the two drains a successful connect always runs, so a
// connect test's output holds only the lines it is about.
func quietAPI() *routedHTTP {
	return newRoutedHTTP(map[string]string{
		"/api/reply-cards?status=answered":              `[]`,
		"/api/reply-cards?status=expired":               `[]`,
		"/api/chat?recipient=kyle&unread=true&limit=50": `{"messages":[]}`,
	})
}

// fixedClock is a hand-wound clock: every reading is now, and advance moves it.
type fixedClock struct{ now time.Time }

func (c *fixedClock) read() time.Time         { return c.now }
func (c *fixedClock) advance(d time.Duration) { c.now = c.now.Add(d) }

func newRunListener(t *testing.T, cfg Config, out io.Writer, api httpClient, tr http.RoundTripper) *listener {
	t.Helper()
	return &listener{
		cfg:              cfg,
		api:              api,
		streamClient:     &http.Client{Transport: tr},
		sleep:            func(time.Duration) {},
		backoffStart:     time.Millisecond,
		backoffCap:       time.Millisecond,
		jitter:           func() float64 { return 1.0 },
		out:              out,
		stamp:            &eventStamper{clock: stampClock(1_787_148_244_692_000_000)},
		clock:            time.Now,
		probeUnknownSpan: probeUnknownGrace,
		refusalGraceSpan: sseRefusalGrace,
		cursorPath:       filepath.Join(cfg.Home, "sse-cursor"),
		drainWarn:        &drainWarner{},
		replySeen:        loadReplyCardSeen(filepath.Join(cfg.Home, "replycards-seen")),
		taskSnaps:        map[string]taskSnap{},
	}
}

func TestNewSSEStreamClient(t *testing.T) {
	c := newSSEStreamClient()

	if c.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0 — an overall deadline would cut a healthy SSE body", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", c.Transport)
	}
	if tr.ResponseHeaderTimeout != 30*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want 30s", tr.ResponseHeaderTimeout)
	}
	if tr.TLSHandshakeTimeout != 10*time.Second {
		t.Errorf("TLSHandshakeTimeout = %v, want 10s", tr.TLSHandshakeTimeout)
	}
	if tr.DialContext == nil {
		t.Error("DialContext = nil, want the bounded dialer — connection SETUP must stay bounded")
	}
	if tr.Proxy == nil {
		t.Error("Proxy = nil, want the environment's proxy")
	}
}

func TestFoldProbe(t *testing.T) {
	t.Run("no probe means the listener can never self-exit on one", func(t *testing.T) {
		var out strings.Builder
		l := &listener{out: &out, clock: time.Now}

		if l.foldProbe() {
			t.Error("foldProbe = true with no probe, want false")
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
	})

	t.Run("one missing session is not enough to leave", func(t *testing.T) {
		var out strings.Builder
		l := &listener{out: &out, clock: time.Now, probe: func() probeVerdict { return probeGone }}

		if l.foldProbe() {
			t.Error("foldProbe = true on the first miss, want false")
		}
		if l.miss != 1 {
			t.Errorf("miss = %d, want 1", l.miss)
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
	})

	t.Run("two consecutive missing sessions end the listener and say why", func(t *testing.T) {
		var out strings.Builder
		l := &listener{out: &out, clock: time.Now, probe: func() probeVerdict { return probeGone }}

		l.foldProbe()
		if !l.foldProbe() {
			t.Error("foldProbe = false on the second miss, want true")
		}
		want := "[ocagent] listen: tmux session gone (2 consecutive misses) — self-exiting so " +
			"no orphan holds the SSE.\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a live session clears a miss run before it can trip", func(t *testing.T) {
		var out strings.Builder
		verdict := probeGone
		l := &listener{out: &out, clock: time.Now, probe: func() probeVerdict { return verdict }}

		l.foldProbe()
		verdict = probeAlive
		l.foldProbe()
		verdict = probeGone
		tripped := l.foldProbe()

		if tripped {
			t.Error("foldProbe = true, want false — the run was broken by a live probe")
		}
		if l.miss != 1 {
			t.Errorf("miss = %d, want 1", l.miss)
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
	})

	t.Run("an unverifiable probe never kills fast and even clears a miss run", func(t *testing.T) {
		var out strings.Builder
		verdict := probeGone
		clock := &fixedClock{now: time.Unix(1787148244, 0)}
		l := &listener{out: &out, clock: clock.read, probeUnknownSpan: probeUnknownGrace,
			probe: func() probeVerdict { return verdict }}

		l.foldProbe()
		verdict = probeUnknown
		tripped := l.foldProbe()

		if tripped {
			t.Error("foldProbe = true on one unknown, want false")
		}
		if l.miss != 0 {
			t.Errorf("miss = %d, want 0 — an unverifiable probe is no evidence the session is gone", l.miss)
		}
		if l.unknowns != 1 {
			t.Errorf("unknowns = %d, want 1", l.unknowns)
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
	})

	t.Run("a probe that panics folds as unverifiable rather than as an instant verdict", func(t *testing.T) {
		var out strings.Builder
		clock := &fixedClock{now: time.Unix(1787148244, 0)}
		l := &listener{out: &out, clock: clock.read, probeUnknownSpan: probeUnknownGrace,
			probe: func() probeVerdict { panic("tmux exploded") }}

		if l.foldProbe() {
			t.Error("foldProbe = true, want false")
		}
		if l.unknowns != 1 || l.miss != 0 {
			t.Errorf("unknowns = %d miss = %d, want 1 and 0", l.unknowns, l.miss)
		}
	})

	t.Run("enough unverifiable probes over enough wall clock fail closed", func(t *testing.T) {
		var out strings.Builder
		clock := &fixedClock{now: time.Unix(1787148244, 0)}
		l := &listener{out: &out, clock: clock.read, probeUnknownSpan: probeUnknownGrace,
			probe: func() probeVerdict { return probeUnknown }}

		for i := 1; i < probeUnknownMin; i++ {
			if l.foldProbe() {
				t.Fatalf("foldProbe = true on unknown #%d, want false", i)
			}
			clock.advance(2 * time.Minute)
		}
		tripped := l.foldProbe()

		if !tripped {
			t.Error("foldProbe = false on the eighth unknown past the grace, want true")
		}
		want := "[ocagent] listen: session unverifiable for 8 consecutive probes over 14m0s — " +
			"fail-closed self-exit (an unprobeable listener must not hold the SSE forever).\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("the count alone never trips without the wall clock to back it", func(t *testing.T) {
		var out strings.Builder
		clock := &fixedClock{now: time.Unix(1787148244, 0)}
		l := &listener{out: &out, clock: clock.read, probeUnknownSpan: probeUnknownGrace,
			probe: func() probeVerdict { return probeUnknown }}

		for i := 0; i < 50; i++ {
			if l.foldProbe() {
				t.Fatalf("foldProbe = true after %d instantaneous unknowns, want false", i+1)
			}
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
	})
}

func TestFoldRefusal(t *testing.T) {
	t.Run("a run short of the minimum never trips", func(t *testing.T) {
		clock := &fixedClock{now: time.Unix(1787148244, 0)}
		l := &listener{clock: clock.read, refusalGraceSpan: sseRefusalGrace}

		for i := 1; i < sseRefusalMin; i++ {
			clock.advance(time.Minute)
			if l.foldRefusal() {
				t.Fatalf("foldRefusal = true on refusal #%d, want false", i)
			}
		}
		if l.refusals != 3 {
			t.Errorf("refusals = %d, want 3", l.refusals)
		}
	})

	t.Run("the minimum count alone never trips inside the grace", func(t *testing.T) {
		clock := &fixedClock{now: time.Unix(1787148244, 0)}
		l := &listener{clock: clock.read, refusalGraceSpan: sseRefusalGrace}

		for i := 0; i < 20; i++ {
			if l.foldRefusal() {
				t.Fatalf("foldRefusal = true after %d instantaneous refusals, want false", i+1)
			}
		}
	})

	t.Run("the minimum count spanning the whole grace trips", func(t *testing.T) {
		clock := &fixedClock{now: time.Unix(1787148244, 0)}
		l := &listener{clock: clock.read, refusalGraceSpan: sseRefusalGrace}

		l.foldRefusal()
		l.foldRefusal()
		l.foldRefusal()
		clock.advance(sseRefusalGrace)
		tripped := l.foldRefusal()

		if !tripped {
			t.Error("foldRefusal = false on the fourth refusal past the grace, want true")
		}
		if l.refusals != 4 {
			t.Errorf("refusals = %d, want 4", l.refusals)
		}
		if !l.firstRefusalAt.Equal(time.Unix(1787148244, 0)) {
			t.Errorf("firstRefusalAt = %v, want the first refusal's instant", l.firstRefusalAt)
		}
	})
}

func TestResetRefusals(t *testing.T) {
	t.Run("a broken run has to earn both bounds again from scratch", func(t *testing.T) {
		clock := &fixedClock{now: time.Unix(1787148244, 0)}
		l := &listener{clock: clock.read, refusalGraceSpan: sseRefusalGrace}
		for i := 0; i < 3; i++ {
			l.foldRefusal()
		}
		clock.advance(sseRefusalGrace)

		l.resetRefusals()

		if l.refusals != 0 || !l.firstRefusalAt.IsZero() {
			t.Errorf("refusals = %d firstRefusalAt = %v, want 0 and the zero time",
				l.refusals, l.firstRefusalAt)
		}
		for i := 1; i < sseRefusalMin; i++ {
			clock.advance(time.Hour)
			if l.foldRefusal() {
				t.Fatalf("foldRefusal = true on refusal #%d of the new run, want false", i)
			}
		}
		if !l.foldRefusal() {
			t.Error("foldRefusal = false on refusal #4 of the new run, want true")
		}
	})
}

func TestNoteDisconnect(t *testing.T) {
	const tail = " (retrying on the same schedule, quietly; the next transport line you see " +
		"is either the reconnect or a give-up)\n"

	t.Run("the first failure of an outage is announced", func(t *testing.T) {
		var out strings.Builder
		l := &listener{out: &out, cfg: Config{BaseConfigured: true}}

		l.noteDisconnect("stream ended: %v", io.EOF)

		want := "[ocagent] listen: disconnected — stream ended: EOF" + tail
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if !l.inOutage {
			t.Error("inOutage = false, want true — the outage is open until a connect closes it")
		}
	})

	t.Run("every later failure inside the same outage is folded away", func(t *testing.T) {
		var out strings.Builder
		l := &listener{out: &out, cfg: Config{BaseConfigured: true}}

		l.noteDisconnect("stream ended: %v", io.EOF)
		l.noteDisconnect("connect failed: %v", errors.New("unexpected status 502"))
		l.noteDisconnect("connect failed: %v", errors.New("connection refused"))

		want := "[ocagent] listen: disconnected — stream ended: EOF" + tail
		if out.String() != want {
			t.Errorf("printed %q, want only the first notice %q", out.String(), want)
		}
	})

	t.Run("a new outage after the last one closed is announced again", func(t *testing.T) {
		var out strings.Builder
		l := &listener{out: &out, cfg: Config{BaseConfigured: true}}

		l.noteDisconnect("stream ended: %v", io.EOF)
		l.inOutage = false // what a successful connect does
		l.noteDisconnect("stream ended: %v", io.ErrUnexpectedEOF)

		want := "[ocagent] listen: disconnected — stream ended: EOF" + tail +
			"[ocagent] listen: disconnected — stream ended: unexpected EOF" + tail
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a listener that was never told its station says so on this line too", func(t *testing.T) {
		var out strings.Builder
		l := &listener{out: &out, cfg: Config{BaseConfigured: false}}

		l.noteDisconnect("connect failed: %v", errors.New("connection refused"))

		want := "[ocagent] listen: disconnected — connect failed: connection refused" +
			" [⚠ address GUESSED — OC_BASE is not set, so nobody chose this station]" + tail
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a percent sign in the reason is text, not a verb", func(t *testing.T) {
		var out strings.Builder
		l := &listener{out: &out, cfg: Config{BaseConfigured: false}}

		l.noteDisconnect("connect failed: %v", errors.New("100% packet loss"))

		want := "[ocagent] listen: disconnected — connect failed: 100% packet loss" +
			" [⚠ address GUESSED — OC_BASE is not set, so nobody chose this station]" + tail
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})
}

func TestStopRetrying(t *testing.T) {
	t.Run("stopping mid-outage names the reason so the silence cannot read as a retry", func(t *testing.T) {
		var out strings.Builder
		l := &listener{out: &out, inOutage: true}

		rc := l.stopRetrying("this process is shutting down")

		if rc != 0 {
			t.Errorf("exit code = %d, want 0 — listen degrades gracefully", rc)
		}
		want := "[ocagent] listen: giving up — this process is shutting down. No further " +
			"reconnect attempts from THIS listener; I am NOT still retrying.\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if l.inOutage {
			t.Error("inOutage = true, want false — the outage was closed by the give-up")
		}
	})

	t.Run("stopping while connected was never ambiguous and prints nothing", func(t *testing.T) {
		var out strings.Builder
		l := &listener{out: &out}

		rc := l.stopRetrying("this process is shutting down")

		if rc != 0 {
			t.Errorf("exit code = %d, want 0", rc)
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
	})

	t.Run("the give-up line is said once, not once per exit path", func(t *testing.T) {
		var out strings.Builder
		l := &listener{out: &out, inOutage: true}

		l.stopRetrying("--once was set: this run makes a single attempt")
		l.stopRetrying("--once was set: this run makes a single attempt")

		want := "[ocagent] listen: giving up — --once was set: this run makes a single attempt. " +
			"No further reconnect attempts from THIS listener; I am NOT still retrying.\n"
		if out.String() != want {
			t.Errorf("printed %q, want the line exactly once", out.String())
		}
	})
}

func TestStationVerdict(t *testing.T) {
	cases := []struct {
		name         string
		prev, cur    string
		firstConnect bool
		want         string
	}{
		{"the first connect has no previous station to compare", "", "da11eae8", true, ""},
		{"a first connect never claims sameness even holding a remembered sha", "da11eae8", "da11eae8", true, ""},
		{"a reconnect to the same build says so", "da11eae8", "da11eae8", false, " [same station]"},
		{"a changeover names the build that was there before", "da11eae8", "bb22cc33", false,
			" [new station — was da11eae8]"},
		{"a station that never sent a sha cannot be compared", "", "bb22cc33", false, ""},
		{"a station that stopped sending its sha cannot be compared", "da11eae8", "", false, ""},
		{"neither side known", "", "", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stationVerdict(c.prev, c.cur, c.firstConnect)
			if got != c.want {
				t.Errorf("stationVerdict(%q, %q, %v) = %q, want %q",
					c.prev, c.cur, c.firstConnect, got, c.want)
			}
			if strings.Contains(got, "[station") {
				t.Errorf("verdict %q borrows the bytes the sha segment owns", got)
			}
		})
	}
}

func TestBaseAddressOrigin(t *testing.T) {
	t.Run("a member that was told its station adds nothing to the line", func(t *testing.T) {
		if got := baseAddressOrigin(true); got != "" {
			t.Errorf("baseAddressOrigin(true) = %q, want empty", got)
		}
	})

	t.Run("a member that invented its address says so", func(t *testing.T) {
		want := " [⚠ address GUESSED — OC_BASE is not set, so nobody chose this station]"
		if got := baseAddressOrigin(false); got != want {
			t.Errorf("baseAddressOrigin(false) = %q, want %q", got, want)
		}
	})
}

func TestDispatch(t *testing.T) {
	const chatPage = "/api/chat?recipient=kyle&unread=true&limit=50"

	newDispatcher := func(t *testing.T, out io.Writer, api *routedHTTP) *listener {
		t.Helper()
		cfg := runCfg(t)
		l := newRunListener(t, cfg, out, api, nil)
		l.winddown = &windDownHook{cfg: cfg, out: out,
			fetchDesired: func() (string, bool) { return desiredOffline, true }}
		l.recycle = &recycleHook{cfg: cfg, out: out,
			fetchMember: func() (map[string]any, bool) { return nil, false }}
		return l
	}

	t.Run("a payload that is not a JSON object is ignored in silence", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(nil)
		l := newDispatcher(t, &out, api)

		l.dispatch([]byte("not json at all"))
		l.dispatch([]byte(`["a","b"]`))

		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
		if len(api.asked) != 0 {
			t.Errorf("asked %v, want no request at all", api.asked)
		}
	})

	t.Run("a chat delta is a nudge that refetches and prints the unread set", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(map[string]string{
			chatPage: `{"messages":[{"id":"c1","from":"boss","to":"kyle","body":"ping"}]}`,
		})
		l := newDispatcher(t, &out, api)

		l.dispatch([]byte(`{"topic":"chat","trigger":"boss"}`))

		if out.String() != "[ocagent] chat from boss (#c1): ping\n" {
			t.Errorf("printed %q, want the refetched line", out.String())
		}
		if !reflect.DeepEqual(api.asked, []string{chatPage}) {
			t.Errorf("asked %v, want just the refetch — the delta payload is never merged", api.asked)
		}
	})

	t.Run("my own action bounced back down my own stream is dropped", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(nil)
		l := newDispatcher(t, &out, api)

		l.dispatch([]byte(`{"topic":"task","trigger":"kyle","data":{"payload":{"id":"t-1"}}}`))

		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
		if len(api.asked) != 0 {
			t.Errorf("asked %v, want no refetch of my own work", api.asked)
		}
	})

	t.Run("a self-triggered chat delta still buys the read receipt", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(map[string]string{
			chatPage: `{"messages":[{"id":"c1","from":"kyle","to":"kyle",` +
				`"ts":1787148244,"body":"note to self"}]}`,
			"/api/chat/mark-read": `{}`,
		})
		l := newDispatcher(t, &out, api)

		l.dispatch([]byte(`{"topic":"chat","trigger":"kyle"}`))

		if out.String() != "" {
			t.Errorf("printed %q, want nothing — a member is not read its own words back", out.String())
		}
		want := []string{chatPage, "/api/chat/mark-read"}
		if !reflect.DeepEqual(api.asked, want) {
			t.Errorf("asked %v, want %v — the receipt is what this exemption buys", api.asked, want)
		}
	})

	t.Run("a member delta nudges both lifecycle hooks and holds the stream", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(nil)
		l := newDispatcher(t, &out, api)

		l.dispatch([]byte(`{"topic":"member","trigger":"owner","data":{"key":"kyle::kyle",` +
			`"payload":{"offboard_notice":"〈停止〉"}}}`))

		if out.String() != "[ocagent] offboard: 〈停止〉\n" {
			t.Errorf("printed %q, want the wind-down wake", out.String())
		}
		if !l.winddown.started {
			t.Error("the wind-down hook was never nudged")
		}
	})

	t.Run("a self-triggered member delta is never suppressed", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(nil)
		l := newDispatcher(t, &out, api)

		l.dispatch([]byte(`{"topic":"member","trigger":"kyle","data":{"key":"kyle::kyle",` +
			`"payload":{"offboard_notice":"〈停止〉"}}}`))

		if out.String() != "[ocagent] offboard: 〈停止〉\n" {
			t.Errorf("printed %q, want the wake — restart_self rides a self-triggered delta",
				out.String())
		}
	})

	t.Run("a directed band frame prints the message the server composed", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(nil)
		l := newDispatcher(t, &out, api)

		l.dispatch([]byte(`{"topic":"context-high","data":{"reason":"context 92% — close out now"}}`))

		want := "[ocagent] signal context-high: context 92% — close out now\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a wake topic prints one wake line naming who moved it", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(nil)
		l := newDispatcher(t, &out, api)

		l.dispatch([]byte(`{"topic":"action","seq":42,"trigger":"owner"}`))

		if out.String() != "[ocagent] wake seq=42 topic=action · by owner\n" {
			t.Errorf("printed %q, want the wake line", out.String())
		}
	})

	t.Run("an unknown topic is ignored in silence", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(nil)
		l := newDispatcher(t, &out, api)

		l.dispatch([]byte(`{"topic":"weather","seq":7}`))

		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
	})

	t.Run("every line of a frame reports the server's own clock, and only for that frame", func(t *testing.T) {
		var inner strings.Builder
		api := newRoutedHTTP(nil)
		cfg := runCfg(t)
		stamper := &eventStamper{clock: stampClock(1_500_000_000_000_000_000)}
		out := &stampWriter{inner: &inner, stamp: stamper.suffix}
		l := newRunListener(t, cfg, out, api, nil)
		l.stamp = stamper

		l.dispatch([]byte(`{"topic":"action","seq":42,"ts":1787148244.692}`))
		l.dispatch([]byte(`{"topic":"action","seq":43}`))

		want := "[ocagent] wake seq=42 topic=action [ts=1787148244.692]\n" +
			"[ocagent] wake seq=43 topic=action [ts=1500000000.000 local]\n"
		if inner.String() != want {
			t.Errorf("printed %q, want %q", inner.String(), want)
		}
	})
}

func TestAuthoritativeRefusal(t *testing.T) {
	resp := func(status int, refusal string) *http.Response {
		h := http.Header{}
		if refusal != "" {
			h.Set(authRefusalHeader, refusal)
		}
		return &http.Response{StatusCode: status, Header: h}
	}
	cases := []struct {
		name string
		resp *http.Response
		want string
	}{
		{"a 409 is the stop gate or the dual-SSE guard", resp(409, ""),
			"409 stop gate / dual-SSE guard"},
		{"a 409 needs no marker to be authoritative", resp(409, "something-else"),
			"409 stop gate / dual-SSE guard"},
		{"a 401 the server marked superseded can never resolve itself",
			resp(401, "agent-superseded"),
			"401 superseded — a newer generation of this member has reported waking"},
		{"the marker is read past its surrounding whitespace",
			resp(401, "  agent-superseded  "),
			"401 superseded — a newer generation of this member has reported waking"},
		{"a bare 401 might just be a server having a moment", resp(401, ""), ""},
		{"a 401 marked something else is not this refusal", resp(401, "token-expired"), ""},
		{"a 502 is a retryable fault", resp(502, ""), ""},
		{"a 500 carrying the superseded marker is still not this refusal",
			resp(500, "agent-superseded"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := authoritativeRefusal(c.resp); got != c.want {
				t.Errorf("authoritativeRefusal = %q, want %q", got, c.want)
			}
		})
	}
}

func TestConnectOnce(t *testing.T) {
	const connected = "[ocagent] listen: connected — streaming http://station/api/events " +
		"(⇒ online while held)"

	t.Run("the dial announces itself as an SSE reader carrying the agent's token", func(t *testing.T) {
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, nil, ""), nil
		}}
		l := newRunListener(t, runCfg(t), io.Discard, quietAPI(), tr)

		opened, activity, selfExit, err := l.connectOnce(context.Background())

		if !opened || activity || selfExit || err != nil {
			t.Errorf("(%v, %v, %v, %v), want (true, false, false, nil)", opened, activity, selfExit, err)
		}
		if len(tr.reqs) != 1 {
			t.Fatalf("dialled %d times, want 1", len(tr.reqs))
		}
		req := tr.reqs[0]
		if req.Method != http.MethodGet || req.URL.String() != "http://station/api/events" {
			t.Errorf("%s %s, want GET http://station/api/events", req.Method, req.URL)
		}
		for header, want := range map[string]string{
			"User-Agent":    "ocagent/0.1",
			"Accept":        "text/event-stream",
			"Cache-Control": "no-cache",
			"Authorization": "Bearer tok",
			"Last-Event-ID": "",
		} {
			if got := req.Header.Get(header); got != want {
				t.Errorf("%s = %q, want %q", header, got, want)
			}
		}
	})

	t.Run("a persisted cursor asks the server to replay after it", func(t *testing.T) {
		cfg := runCfg(t)
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, nil, ""), nil
		}}
		l := newRunListener(t, cfg, io.Discard, quietAPI(), tr)
		writeCursor(l.cursorPath, "7")

		l.connectOnce(context.Background())

		if got := tr.reqs[0].Header.Get("Last-Event-ID"); got != "7" {
			t.Errorf("Last-Event-ID = %q, want %q", got, "7")
		}
	})

	t.Run("a plain non-200 is a retryable fault and nothing is announced", func(t *testing.T) {
		var out strings.Builder
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(502, nil, "bad gateway"), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)

		opened, activity, selfExit, err := l.connectOnce(context.Background())

		if opened || activity || selfExit {
			t.Errorf("(%v, %v, %v), want all false", opened, activity, selfExit)
		}
		if err == nil || err.Error() != "unexpected status 502" {
			t.Errorf("err = %v, want \"unexpected status 502\"", err)
		}
		if errors.Is(err, errSSERefused) {
			t.Error("a 502 folded as an authoritative refusal — a 5xx must never accumulate")
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing — the run loop owns the notice", out.String())
		}
	})

	t.Run("a 409 is an authoritative refusal carrying the server's own reason", func(t *testing.T) {
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(409, nil, "stop in effect\n"), nil
		}}
		l := newRunListener(t, runCfg(t), io.Discard, quietAPI(), tr)

		opened, _, _, err := l.connectOnce(context.Background())

		if opened {
			t.Error("opened = true, want false — the refusal is pre-stream")
		}
		if !errors.Is(err, errSSERefused) {
			t.Fatalf("err = %v, want it to wrap errSSERefused", err)
		}
		want := "listen: server authoritatively refused the SSE connection " +
			"[409 stop gate / dual-SSE guard]: stop in effect"
		if err.Error() != want {
			t.Errorf("err = %q, want %q", err.Error(), want)
		}
	})

	t.Run("a 401 the server marked superseded is an authoritative refusal", func(t *testing.T) {
		h := http.Header{}
		h.Set(authRefusalHeader, refusalAgentSuperseded)
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(401, h, "agent superseded"), nil
		}}
		l := newRunListener(t, runCfg(t), io.Discard, quietAPI(), tr)

		_, _, _, err := l.connectOnce(context.Background())

		if !errors.Is(err, errSSERefused) {
			t.Fatalf("err = %v, want it to wrap errSSERefused", err)
		}
		want := "listen: server authoritatively refused the SSE connection " +
			"[401 superseded — a newer generation of this member has reported waking]: agent superseded"
		if err.Error() != want {
			t.Errorf("err = %q, want %q", err.Error(), want)
		}
	})

	t.Run("a bare 401 never folds toward the fail-closed kill", func(t *testing.T) {
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(401, nil, "unauthorized"), nil
		}}
		l := newRunListener(t, runCfg(t), io.Discard, quietAPI(), tr)

		_, _, _, err := l.connectOnce(context.Background())

		if errors.Is(err, errSSERefused) {
			t.Fatal("a bare 401 folded as an authoritative refusal")
		}
		if err == nil || err.Error() != "unexpected status 401" {
			t.Errorf("err = %v, want \"unexpected status 401\"", err)
		}
	})

	t.Run("a dial that never reached a server is the transport's own error", func(t *testing.T) {
		boom := errors.New("connection refused")
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return nil, boom
		}}
		l := newRunListener(t, runCfg(t), io.Discard, quietAPI(), tr)

		opened, activity, selfExit, err := l.connectOnce(context.Background())

		if opened || activity || selfExit {
			t.Errorf("(%v, %v, %v), want all false", opened, activity, selfExit)
		}
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want it to wrap %v", err, boom)
		}
	})

	t.Run("an open stream announces the connection", func(t *testing.T) {
		var out strings.Builder
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, nil, ""), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)
		l.inOutage = true

		l.connectOnce(context.Background())

		if out.String() != connected+"\n" {
			t.Errorf("printed %q, want %q", out.String(), connected+"\n")
		}
		if l.inOutage {
			t.Error("inOutage = true, want false — the connect line IS the end of the outage")
		}
		if !l.sawConnect {
			t.Error("sawConnect = false, want true")
		}
	})

	t.Run("the connection line names the build the station self-reports", func(t *testing.T) {
		var out strings.Builder
		h := http.Header{}
		h.Set(stationSHAHeader, "da11eae8")
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, h, ""), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)

		l.connectOnce(context.Background())

		want := connected + " [station da11eae8]\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if l.lastStation != "da11eae8" {
			t.Errorf("lastStation = %q, want %q", l.lastStation, "da11eae8")
		}
	})

	t.Run("a reconnect to the same build says so before naming it", func(t *testing.T) {
		var out strings.Builder
		h := http.Header{}
		h.Set(stationSHAHeader, "da11eae8")
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, h, ""), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)

		l.connectOnce(context.Background())
		l.connectOnce(context.Background())

		want := connected + " [station da11eae8]\n" +
			connected + " [same station] [station da11eae8]\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a changeover names the build that was there before", func(t *testing.T) {
		var out strings.Builder
		shas := []string{"da11eae8", "bb22cc33"}
		tr := &stubTransport{reply: func(n int, _ *http.Request) (*http.Response, error) {
			h := http.Header{}
			h.Set(stationSHAHeader, shas[n])
			return sseReply(200, h, ""), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)

		l.connectOnce(context.Background())
		l.connectOnce(context.Background())

		want := connected + " [station da11eae8]\n" +
			connected + " [new station — was da11eae8] [station bb22cc33]\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a station that sends no sha leaves the line bare and never reuses the last one", func(t *testing.T) {
		var out strings.Builder
		tr := &stubTransport{reply: func(n int, _ *http.Request) (*http.Response, error) {
			h := http.Header{}
			if n == 0 {
				h.Set(stationSHAHeader, "da11eae8")
			}
			return sseReply(200, h, ""), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)

		l.connectOnce(context.Background())
		l.connectOnce(context.Background())

		want := connected + " [station da11eae8]\n" + connected + "\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if l.lastStation != "da11eae8" {
			t.Errorf("lastStation = %q, want the last sha actually seen", l.lastStation)
		}
	})

	t.Run("a listener that was never told its station says so beside the address", func(t *testing.T) {
		var out strings.Builder
		cfg := runCfg(t)
		cfg.BaseConfigured = false
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, nil, ""), nil
		}}
		l := newRunListener(t, cfg, &out, quietAPI(), tr)

		l.connectOnce(context.Background())

		want := "[ocagent] listen: connected — streaming http://station/api/events" +
			" [⚠ address GUESSED — OC_BASE is not set, so nobody chose this station]" +
			" (⇒ online while held)\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("the connect drains chat before the live stream takes over", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(map[string]string{
			"/api/reply-cards?status=answered": `[]`,
			"/api/reply-cards?status=expired":  `[]`,
			"/api/chat?recipient=kyle&unread=true&limit=50": `{"messages":` +
				`[{"id":"c1","from":"boss","to":"kyle","body":"你在嗎"}]}`,
		})
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, nil, "data: {\"topic\":\"action\",\"seq\":9}\n\n"), nil
		}}
		l := newRunListener(t, runCfg(t), &out, api, tr)
		l.winddown = &windDownHook{cfg: l.cfg, out: &out}
		l.recycle = &recycleHook{cfg: l.cfg, out: &out}

		l.connectOnce(context.Background())

		want := connected + "\n" +
			"[ocagent] chat from boss (#c1): 你在嗎\n" +
			"[ocagent] wake seq=9 topic=action\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		wantAsked := []string{
			"/api/reply-cards?status=answered",
			"/api/reply-cards?status=expired",
			"/api/chat?recipient=kyle&unread=true&limit=50",
		}
		if !reflect.DeepEqual(api.asked, wantAsked) {
			t.Errorf("asked %v, want %v", api.asked, wantAsked)
		}
	})

	t.Run("a byte on the wire proves the link healthy and every id persists the cursor", func(t *testing.T) {
		cfg := runCfg(t)
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, nil, "id: 9\ndata: {\"topic\":\"weather\"}\n\nid: 11\n\n"), nil
		}}
		l := newRunListener(t, cfg, io.Discard, quietAPI(), tr)

		opened, activity, selfExit, err := l.connectOnce(context.Background())

		if !opened || !activity || selfExit || err != nil {
			t.Errorf("(%v, %v, %v, %v), want (true, true, false, nil)", opened, activity, selfExit, err)
		}
		if got := readCursor(l.cursorPath); got != "11" {
			t.Errorf("persisted cursor = %q, want %q", got, "11")
		}
	})

	t.Run("a heartbeat line the session probe rejects ends the stream as a self-exit", func(t *testing.T) {
		var out strings.Builder
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, nil, ": heartbeat\n\n"), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)
		l.miss = sessionMissLimit - 1
		l.probe = func() probeVerdict { return probeGone }

		opened, _, selfExit, err := l.connectOnce(context.Background())

		if !opened || !selfExit {
			t.Errorf("(opened=%v, selfExit=%v), want (true, true)", opened, selfExit)
		}
		if !errors.Is(err, errSelfExit) {
			t.Errorf("err = %v, want errSelfExit", err)
		}
		want := connected + "\n" +
			"[ocagent] listen: tmux session gone (2 consecutive misses) — self-exiting so " +
			"no orphan holds the SSE.\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

}

func TestDrainChatNow(t *testing.T) {
	t.Run("it counts the unread it printed", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(map[string]string{
			"/api/chat?recipient=kyle&unread=true&limit=50": `{"messages":` +
				`[{"id":"c1","from":"boss","to":"kyle","body":"one"},` +
				`{"id":"c2","from":"boss","to":"kyle","body":"two"}]}`,
			"/api/chat/mark-read": `{}`,
		})
		l := newRunListener(t, runCfg(t), &out, api, nil)

		n := l.drainChatNow()

		if n != 2 {
			t.Errorf("drainChatNow = %d, want 2", n)
		}
		want := "[ocagent] chat from boss (#c1): one\n[ocagent] chat from boss (#c2): two\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a listener with no warner yet gets one, so the fault latch survives the next drain", func(t *testing.T) {
		var out strings.Builder
		api := newRoutedHTTP(nil)
		api.status["/api/chat?recipient=kyle&unread=true&limit=50"] = 500
		l := newRunListener(t, runCfg(t), &out, api, nil)
		l.drainWarn = nil

		first := l.drainChatNow()
		second := l.drainChatNow()

		if first != 0 || second != 0 {
			t.Errorf("drains = (%d, %d), want (0, 0) — nothing was fetched", first, second)
		}
		if l.drainWarn == nil {
			t.Fatal("drainWarn is still nil — the latch would be recreated on every drain")
		}
		want := "[ocagent] chat: 補印一頁都沒撈到（HTTP 500）—— 這不是「沒有新訊息」，" +
			"是這次沒問到。未讀原封不動，下一次補印會再試；等不及就用 get_chat 自己撈。\n"
		if out.String() != want {
			t.Errorf("printed %q, want the fault announced exactly once", out.String())
		}
	})
}

func TestRun(t *testing.T) {
	t.Run("a context already cancelled stops before dialling and says nothing", func(t *testing.T) {
		var out strings.Builder
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, nil, ""), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		rc := l.run(ctx)

		if rc != 0 {
			t.Errorf("exit code = %d, want 0", rc)
		}
		if len(tr.reqs) != 0 {
			t.Errorf("dialled %d times, want 0", len(tr.reqs))
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing — no outage was ever open", out.String())
		}
	})

	t.Run("an orphan never reconnects and never reports an outage it did not have", func(t *testing.T) {
		var out strings.Builder
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, nil, ""), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)
		l.miss = sessionMissLimit - 1
		l.probe = func() probeVerdict { return probeGone }

		rc := l.run(context.Background())

		if rc != 0 {
			t.Errorf("exit code = %d, want 0", rc)
		}
		if len(tr.reqs) != 0 {
			t.Errorf("dialled %d times, want 0 — an orphan must not reopen the SSE", len(tr.reqs))
		}
		want := "[ocagent] listen: tmux session gone (2 consecutive misses) — self-exiting so " +
			"no orphan holds the SSE.\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a single-attempt run announces the outage and then says it stopped", func(t *testing.T) {
		var out strings.Builder
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(502, nil, ""), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)
		l.once = true

		rc := l.run(context.Background())

		if rc != 0 {
			t.Errorf("exit code = %d, want 0", rc)
		}
		if len(tr.reqs) != 1 {
			t.Errorf("dialled %d times, want exactly 1", len(tr.reqs))
		}
		want := "[ocagent] listen: disconnected — connect failed: unexpected status 502" +
			" (retrying on the same schedule, quietly; the next transport line you see " +
			"is either the reconnect or a give-up)\n" +
			"[ocagent] listen: giving up — --once was set: this run makes a single attempt. " +
			"No further reconnect attempts from THIS listener; I am NOT still retrying.\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a stream that opened and then ended is reported as a stream end", func(t *testing.T) {
		var out strings.Builder
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, nil, "data: {\"topic\":\"weather\"}\n\n"), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)
		l.once = true

		l.run(context.Background())

		want := "[ocagent] listen: connected — streaming http://station/api/events " +
			"(⇒ online while held)\n" +
			"[ocagent] listen: disconnected — stream ended: <nil>" +
			" (retrying on the same schedule, quietly; the next transport line you see " +
			"is either the reconnect or a give-up)\n" +
			"[ocagent] listen: giving up — --once was set: this run makes a single attempt. " +
			"No further reconnect attempts from THIS listener; I am NOT still retrying.\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a stand-down on the heartbeat line ends the loop without a give-up line", func(t *testing.T) {
		var out strings.Builder
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(200, nil, ": heartbeat\n\n: heartbeat\n\n"), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)
		// Alive at the loop's own probe point, so the stand-down can only come
		// from a heartbeat line inside the stream.
		l.probe = func() probeVerdict {
			if len(tr.reqs) == 0 {
				return probeAlive
			}
			return probeGone
		}

		rc := l.run(context.Background())

		if rc != 0 {
			t.Errorf("exit code = %d, want 0", rc)
		}
		if len(tr.reqs) != 1 {
			t.Errorf("dialled %d times, want 1 — a stood-down listener never redials", len(tr.reqs))
		}
		want := "[ocagent] listen: connected — streaming http://station/api/events " +
			"(⇒ online while held)\n" +
			"[ocagent] listen: tmux session gone (2 consecutive misses) — self-exiting so " +
			"no orphan holds the SSE.\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a standing refusal for the whole grace kills this listener's own session", func(t *testing.T) {
		var out strings.Builder
		clock := &fixedClock{now: time.Unix(1787148244, 0)}
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(409, nil, "stop in effect"), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)
		l.clock = clock.read
		l.refusalGraceSpan = 0
		killed := 0
		l.selfTerminate = func() { killed++ }

		rc := l.run(context.Background())

		if rc != 0 {
			t.Errorf("exit code = %d, want 0", rc)
		}
		if len(tr.reqs) != sseRefusalMin {
			t.Errorf("dialled %d times, want %d", len(tr.reqs), sseRefusalMin)
		}
		if killed != 1 {
			t.Errorf("selfTerminate called %d times, want 1", killed)
		}
		want := "[ocagent] listen: disconnected — connect refused: listen: server authoritatively " +
			"refused the SSE connection [409 stop gate / dual-SSE guard]: stop in effect" +
			" (retrying on the same schedule, quietly; the next transport line you see " +
			"is either the reconnect or a give-up)\n" +
			"[ocagent] listen: server refused the SSE 4 consecutive times over 0s — " +
			"fail-closed: self-terminating instead of retrying forever " +
			"(a refused listener is a zombie, not a client with bad luck).\n" +
			"[ocagent] listen: giving up — the server refused this listener authoritatively " +
			"for the whole grace window. No further reconnect attempts from THIS listener; " +
			"I am NOT still retrying.\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a briefly unavailable server can never accumulate toward the kill", func(t *testing.T) {
		var out strings.Builder
		clock := &fixedClock{now: time.Unix(1787148244, 0)}
		attempts := 0
		tr := &stubTransport{reply: func(n int, _ *http.Request) (*http.Response, error) {
			attempts++
			if n%2 == 0 {
				return sseReply(409, nil, "stop in effect"), nil
			}
			return sseReply(503, nil, "unavailable"), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)
		l.clock = clock.read
		l.refusalGraceSpan = 0
		killed := 0
		l.selfTerminate = func() { killed++ }
		ctx, cancel := context.WithCancel(context.Background())
		l.sleep = func(time.Duration) {
			if attempts >= 20 {
				cancel()
			}
		}

		l.run(ctx)

		if killed != 0 {
			t.Errorf("selfTerminate called %d times over %d alternating failures, want 0",
				killed, attempts)
		}
	})

	t.Run("a cancellation during the backoff sleep stops the loop", func(t *testing.T) {
		var out strings.Builder
		tr := &stubTransport{reply: func(int, *http.Request) (*http.Response, error) {
			return sseReply(502, nil, ""), nil
		}}
		l := newRunListener(t, runCfg(t), &out, quietAPI(), tr)
		ctx, cancel := context.WithCancel(context.Background())
		l.sleep = func(time.Duration) { cancel() }

		rc := l.run(ctx)

		if rc != 0 {
			t.Errorf("exit code = %d, want 0", rc)
		}
		if len(tr.reqs) != 1 {
			t.Errorf("dialled %d times, want 1", len(tr.reqs))
		}
		want := "[ocagent] listen: disconnected — connect failed: unexpected status 502" +
			" (retrying on the same schedule, quietly; the next transport line you see " +
			"is either the reconnect or a give-up)\n" +
			"[ocagent] listen: giving up — this process is shutting down. No further " +
			"reconnect attempts from THIS listener; I am NOT still retrying.\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("the retry cadence widens toward the cap and a healthy byte resets it", func(t *testing.T) {
		var slept []time.Duration
		tr := &stubTransport{reply: func(n int, _ *http.Request) (*http.Response, error) {
			if n == 3 {
				return sseReply(200, nil, "data: {\"topic\":\"weather\"}\n\n"), nil
			}
			return sseReply(502, nil, ""), nil
		}}
		l := newRunListener(t, runCfg(t), io.Discard, quietAPI(), tr)
		l.backoffStart = time.Second
		l.backoffCap = 15 * time.Second
		ctx, cancel := context.WithCancel(context.Background())
		l.sleep = func(d time.Duration) {
			slept = append(slept, d)
			if len(slept) == 5 {
				cancel()
			}
		}

		l.run(ctx)

		want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, time.Second, 2 * time.Second}
		if !reflect.DeepEqual(slept, want) {
			t.Errorf("slept %v, want %v", slept, want)
		}
	})
}

func TestSleepCtx(t *testing.T) {
	t.Run("a live context sleeps for exactly what it was asked", func(t *testing.T) {
		var slept []time.Duration

		ok := sleepCtx(context.Background(), func(d time.Duration) { slept = append(slept, d) },
			3*time.Second)

		if !ok {
			t.Error("sleepCtx = false, want true")
		}
		if !reflect.DeepEqual(slept, []time.Duration{3 * time.Second}) {
			t.Errorf("slept %v, want [3s]", slept)
		}
	})

	t.Run("a context already cancelled never sleeps at all", func(t *testing.T) {
		var slept []time.Duration
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		ok := sleepCtx(ctx, func(d time.Duration) { slept = append(slept, d) }, 3*time.Second)

		if ok {
			t.Error("sleepCtx = true, want false")
		}
		if len(slept) != 0 {
			t.Errorf("slept %v, want nothing", slept)
		}
	})

	t.Run("a cancellation that lands during the sleep still stops the loop", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		ok := sleepCtx(ctx, func(time.Duration) { cancel() }, 3*time.Second)

		if ok {
			t.Error("sleepCtx = true, want false — the wait is checked after it ends too")
		}
	})
}

func TestCmdListen(t *testing.T) {
	t.Run("a mis-wired agent says so once, on a stamped line, and exits cleanly", func(t *testing.T) {
		cases := []struct {
			name string
			cfg  Config
		}{
			{"no OC_ID", Config{Base: "http://station", Token: "tok"}},
			{"no OC_TOKEN", Config{Base: "http://station", ID: "kyle"}},
			{"neither", Config{Base: "http://station"}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				var out strings.Builder

				rc := cmdListen(c.cfg, testEnv(nil), false, &out)

				if rc != 0 {
					t.Errorf("exit code = %d, want 0", rc)
				}
				want := regexp.MustCompile(
					`^\[ocagent\] listen: no OC_ID/OC_TOKEN — nothing to do; exiting\.` +
						` \[ts=\d+\.\d{3} local\]\n$`)
				if !want.MatchString(out.String()) {
					t.Errorf("printed %q, want the stamped mis-wire notice", out.String())
				}
			})
		}
	})
}

func TestNewListener(t *testing.T) {
	t.Run("the production wiring carries the config through and takes the real bounds", func(t *testing.T) {
		cfg := Config{
			Base: "http://station", BaseConfigured: true,
			Token: "tok", ID: "Kyle", Home: t.TempDir(),
		}
		stamper := &eventStamper{clock: time.Now}
		var out strings.Builder

		l := newListener(cfg, testEnv(nil), &out, true, stamper)

		if l.cfg != cfg {
			t.Errorf("cfg = %+v, want %+v — the T-89 origin rides BaseConfigured", l.cfg, cfg)
		}
		if !l.once {
			t.Error("once = false, want true")
		}
		if l.stamp != stamper {
			t.Error("stamp is not the stamper cmdListen already wrapped out with")
		}
		if l.out != io.Writer(&out) {
			t.Error("out is not the writer it was handed")
		}
		if l.backoffStart != time.Second || l.backoffCap != 15*time.Second {
			t.Errorf("backoff = %v..%v, want 1s..15s", l.backoffStart, l.backoffCap)
		}
		if l.idleReadTimeout != 45*time.Second {
			t.Errorf("idleReadTimeout = %v, want 45s", l.idleReadTimeout)
		}
		if l.probeUnknownSpan != 10*time.Minute {
			t.Errorf("probeUnknownSpan = %v, want 10m", l.probeUnknownSpan)
		}
		if l.refusalGraceSpan != 120*time.Second {
			t.Errorf("refusalGraceSpan = %v, want 2m", l.refusalGraceSpan)
		}
		if want := filepath.Join(cfg.Home, "kyle", "sse-cursor"); l.cursorPath != want {
			t.Errorf("cursorPath = %q, want %q", l.cursorPath, want)
		}
		if l.streamClient.Timeout != 0 {
			t.Errorf("streamClient.Timeout = %v, want 0 — the SSE downlink has no deadline",
				l.streamClient.Timeout)
		}
		if l.api == nil || l.sleep == nil || l.jitter == nil || l.clock == nil ||
			l.selfTerminate == nil || l.winddown == nil || l.recycle == nil ||
			l.drainWarn == nil || l.replySeen == nil || l.taskSnaps == nil {
			t.Errorf("a seam was left nil: %+v", l)
		}
	})

	t.Run("a headless run has no session to probe and no ack gate to wait on", func(t *testing.T) {
		l := newListener(Config{ID: "kyle", Home: t.TempDir()}, testEnv(nil), io.Discard, false, nil)

		if l.probe != nil {
			t.Error("probe is wired with no OC_SESSION — a headless run could self-exit on a verdict")
		}
		if l.ack != nil {
			t.Error("ack is wired without the sidecar asking for it")
		}
		if l.once {
			t.Error("once = true, want false")
		}
	})

	t.Run("a spawned session is probed and an ack-gated sidecar is waited on", func(t *testing.T) {
		env := testEnv(map[string]string{"OC_SESSION": "oc-kyle", "OC_LISTEN_ACK": "1"})

		l := newListener(Config{ID: "kyle", Home: t.TempDir()}, env, io.Discard, false, nil)

		if l.probe == nil {
			t.Error("probe = nil with OC_SESSION set")
		}
		if l.ack == nil {
			t.Error("ack = nil with OC_LISTEN_ACK=1")
		}
	})
}

func TestRootCtx(t *testing.T) {
	t.Run("a termination signal cancels the root so the read can shut down gracefully", func(t *testing.T) {
		ctx := rootCtx()

		if ctx.Err() != nil {
			t.Fatalf("ctx.Err() = %v before any signal, want nil", ctx.Err())
		}
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatalf("could not raise SIGTERM: %v", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("the root context was never cancelled by SIGTERM")
		}
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Errorf("ctx.Err() = %v, want context.Canceled", ctx.Err())
		}
	})
}

// TestLogf is deliberately not written. logf is a single fmt.Fprintf that
// prefixes agentLinePrefix; the only assertion the rules permit restates that
// line, and the prefix it guards is pinned verbatim by every expectation in
// TestFoldProbe, TestNoteDisconnect, TestStopRetrying and TestConnectOnce.
func TestLogf(t *testing.T) {
	t.Skip("a one-line Fprintf wrapper: any assertion would transcribe it, and its output is pinned above")
}
