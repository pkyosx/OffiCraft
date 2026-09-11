package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// routedHTTP answers each request from a canned table keyed by "<path>?<query>",
// falling back to 404, and records every URL it was asked for in order.
type routedHTTP struct {
	routes map[string]string
	status map[string]int
	asked  []string
}

func newRoutedHTTP(routes map[string]string) *routedHTTP {
	return &routedHTTP{routes: routes, status: map[string]int{}}
}

func (r *routedHTTP) Do(req *http.Request) (*http.Response, error) {
	key := req.URL.Path
	if req.URL.RawQuery != "" {
		key += "?" + req.URL.RawQuery
	}
	r.asked = append(r.asked, key)
	body, ok := r.routes[key]
	code := 404
	if ok {
		code = 200
	}
	if s, set := r.status[key]; set {
		code = s
	}
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}, nil
}

// collectSSE runs one scan and returns everything the sink was told, in order.
type sseTrace struct {
	activity int
	data     []string
	ids      []string
	comments int
}

func collectSSE(t *testing.T, stream string, stopOnComment bool) (sseTrace, error) {
	t.Helper()
	var got sseTrace
	err := scanSSE(strings.NewReader(stream), sseSink{
		onActivity: func() { got.activity++ },
		onData:     func(b []byte) { got.data = append(got.data, string(b)) },
		onID:       func(s string) { got.ids = append(got.ids, s) },
		onComment:  func() bool { got.comments++; return stopOnComment },
	})
	return got, err
}

func TestScanSSE(t *testing.T) {
	t.Run("a blank line dispatches the accumulated data", func(t *testing.T) {
		got, err := collectSSE(t, "id: 7\ndata: {\"a\":1}\n\n", false)
		if err != nil {
			t.Errorf("scanSSE err = %v, want nil at EOF", err)
		}
		want := sseTrace{activity: 3, data: []string{`{"a":1}`}, ids: []string{"7"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("sink saw %+v, want %+v", got, want)
		}
	})

	t.Run("multiple data lines join with a newline", func(t *testing.T) {
		got, err := collectSSE(t, "data: one\ndata: two\n\n", false)
		if err != nil {
			t.Errorf("scanSSE err = %v, want nil", err)
		}
		want := sseTrace{activity: 3, data: []string{"one\ntwo"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("sink saw %+v, want %+v", got, want)
		}
	})

	t.Run("exactly one leading space after the colon is stripped", func(t *testing.T) {
		got, _ := collectSSE(t, "data:  padded\ndata:tight\n\n", false)
		want := []string{" padded\ntight"}
		if !reflect.DeepEqual(got.data, want) {
			t.Errorf("data = %q, want %q", got.data, want)
		}
	})

	t.Run("CRLF framing is tolerated", func(t *testing.T) {
		got, _ := collectSSE(t, "id: 9\r\ndata: hi\r\n\r\n", false)
		want := sseTrace{activity: 3, data: []string{"hi"}, ids: []string{"9"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("sink saw %+v, want %+v", got, want)
		}
	})

	t.Run("event, retry and valueless fields are ignored", func(t *testing.T) {
		got, _ := collectSSE(t, "event: delta\nretry: 3000\nnofieldvalue\ndata: body\n\n", false)
		want := sseTrace{activity: 5, data: []string{"body"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("sink saw %+v, want %+v", got, want)
		}
	})

	t.Run("an incomplete final event is discarded", func(t *testing.T) {
		got, err := collectSSE(t, "data: first\n\ndata: unterminated\n", false)
		if err != nil {
			t.Errorf("scanSSE err = %v, want nil", err)
		}
		want := []string{"first"}
		if !reflect.DeepEqual(got.data, want) {
			t.Errorf("data = %q, want %q — the spec discards an event with no blank boundary",
				got.data, want)
		}
	})

	t.Run("a keepalive comment reaches onComment and does not dispatch", func(t *testing.T) {
		got, err := collectSSE(t, ": keepalive\ndata: body\n\n", false)
		if err != nil {
			t.Errorf("scanSSE err = %v, want nil", err)
		}
		want := sseTrace{activity: 3, data: []string{"body"}, comments: 1}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("sink saw %+v, want %+v", got, want)
		}
	})

	t.Run("a comment whose probe says stop ends the scan with errSelfExit", func(t *testing.T) {
		got, err := collectSSE(t, ": keepalive\ndata: never seen\n\n", true)
		if !errors.Is(err, errSelfExit) {
			t.Errorf("scanSSE err = %v, want errSelfExit", err)
		}
		want := sseTrace{activity: 1, comments: 1}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("sink saw %+v, want %+v — nothing after the stop is read", got, want)
		}
	})

	t.Run("a nil sink drives nothing and does not panic", func(t *testing.T) {
		if err := scanSSE(strings.NewReader(": c\nid: 1\ndata: x\n\n"), sseSink{}); err != nil {
			t.Errorf("scanSSE err = %v, want nil", err)
		}
	})
}

func TestNextBackoff(t *testing.T) {
	const start = 1 * time.Second
	const capd = 15 * time.Second
	cases := []struct {
		name    string
		current time.Duration
		jf      float64
		want    time.Duration
	}{
		{"the first delay is floored at start and doubled", 0, 1.0, 2 * time.Second},
		{"a delay below start is floored at start", 200 * time.Millisecond, 1.0, 2 * time.Second},
		{"the delay doubles", 2 * time.Second, 1.0, 4 * time.Second},
		{"full jitter halves the doubled delay", 2 * time.Second, 0.5, 2 * time.Second},
		{"the cap bounds the doubling", 12 * time.Second, 1.0, 15 * time.Second},
		{"jitter applies after the cap", 12 * time.Second, 0.5, 7500 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextBackoff(tc.current, start, capd, tc.jf); got != tc.want {
				t.Errorf("nextBackoff(%v, %v, %v, %v) = %v, want %v",
					tc.current, start, capd, tc.jf, got, tc.want)
			}
		})
	}
}

func TestCursorPath(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"an id is lowercased", Config{Home: "/h", ID: "M-Kyle"}, "/h/m-kyle/sse-cursor"},
		{"no id falls back to anon", Config{Home: "/h"}, "/h/anon/sse-cursor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cursorPath(tc.cfg); got != tc.want {
				t.Errorf("cursorPath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteCursor(t *testing.T) {
	t.Run("the cursor lands under a directory it creates and reads back", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kyle", "sse-cursor")
		writeCursor(path, "seq-42")

		if got := readFileString(t, path); got != "seq-42" {
			t.Errorf("file = %q, want %q", got, "seq-42")
		}
		if got := readCursor(path); got != "seq-42" {
			t.Errorf("readCursor = %q, want %q", got, "seq-42")
		}
	})

	t.Run("an unwritable parent is swallowed", func(t *testing.T) {
		blocked := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeCursor(filepath.Join(blocked, "sse-cursor"), "seq-1")
		if got := readFileString(t, blocked); got != "x" {
			t.Errorf("the blocking file = %q, want %q", got, "x")
		}
	})
}

func TestReadCursor(t *testing.T) {
	t.Run("surrounding whitespace is trimmed off", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sse-cursor")
		if err := os.WriteFile(path, []byte("  seq-42\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readCursor(path); got != "seq-42" {
			t.Errorf("readCursor = %q, want %q", got, "seq-42")
		}
	})

	t.Run("a missing cursor is a full replay, not an error", func(t *testing.T) {
		if got := readCursor(filepath.Join(t.TempDir(), "nope")); got != "" {
			t.Errorf("readCursor = %q, want empty", got)
		}
	})
}

func TestIsExecutableFileListen(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tmux")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"an executable file", exe, true},
		{"a non-executable file", plain, false},
		{"a directory", dir, false},
		{"a missing path", filepath.Join(dir, "nope"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isExecutableFileListen(tc.path); got != tc.want {
				t.Errorf("isExecutableFileListen(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestMakeSessionProbe(t *testing.T) {
	t.Run("a headless run has no session to mirror and disables probing", func(t *testing.T) {
		if probe := makeSessionProbe(testEnv(nil)); probe != nil {
			t.Error("makeSessionProbe returned a probe with no OC_SESSION, want nil — a run " +
				"with no session must never be able to self-exit on a probe verdict")
		}
	})

	t.Run("a blank OC_SESSION also disables probing", func(t *testing.T) {
		if probe := makeSessionProbe(testEnv(map[string]string{"OC_SESSION": "   "})); probe != nil {
			t.Error("makeSessionProbe returned a probe for a blank session, want nil")
		}
	})

	t.Run("a named session that tmux says does not exist reads GONE", func(t *testing.T) {
		if resolveTmuxBin() == "" {
			t.Skip("no tmux on this host — the GONE verdict needs tmux to answer")
		}
		probe := makeSessionProbe(testEnv(map[string]string{
			"OC_SESSION":     "oc-test-session-that-does-not-exist",
			"OC_TMUX_SOCKET": "officraft-test-no-such-socket",
		}))
		if probe == nil {
			t.Fatal("makeSessionProbe = nil with OC_SESSION set, want a probe")
		}
		if got := probe(); got != probeGone {
			t.Errorf("probe() = %v, want probeGone (%v)", got, probeGone)
		}
	})
}

func TestShouldDispatch(t *testing.T) {
	cases := []struct {
		name  string
		frame map[string]any
		want  bool
	}{
		{"an action delta wakes", map[string]any{"topic": "action"}, true},
		{"a task delta wakes", map[string]any{"topic": "task"}, true},
		{"a chat delta does not (chat is a refetch, not a wake)", map[string]any{"topic": "chat"}, false},
		{"a member delta does not", map[string]any{"topic": "member"}, false},
		{"a non-string topic does not", map[string]any{"topic": 7.0}, false},
		{"no topic does not", map[string]any{}, false},
		{"a nil frame does not", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldDispatch(tc.frame); got != tc.want {
				t.Errorf("shouldDispatch(%v) = %v, want %v", tc.frame, got, tc.want)
			}
		})
	}
}

func TestShouldWindDown(t *testing.T) {
	member := func(key any) map[string]any {
		return map[string]any{"topic": "member", "data": map[string]any{"key": key}}
	}
	cases := []struct {
		name  string
		frame map[string]any
		myID  string
		want  bool
	}{
		{"a scoped key naming me", member("kyle::m-1"), "m-1", true},
		{"an unscoped key naming me", member("m-1"), "m-1", true},
		{"case and padding do not change the answer", member("  KYLE::M-1  "), "m-1", true},
		{"a key naming someone else", member("kyle::m-2"), "m-1", false},
		{"my id as the owner half of someone else's key", member("m-1::m-2"), "m-1", false},
		{"a non-member topic", map[string]any{"topic": "task", "data": map[string]any{"key": "m-1"}}, "m-1", false},
		{"a blank key", member("  "), "m-1", false},
		{"no data object", map[string]any{"topic": "member"}, "m-1", false},
		{"a blank id of my own", member("m-1"), "  ", false},
		{"a nil frame", nil, "m-1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldWindDown(tc.frame, tc.myID); got != tc.want {
				t.Errorf("shouldWindDown(%v, %q) = %v, want %v", tc.frame, tc.myID, got, tc.want)
			}
		})
	}
}

func TestFrameTrigger(t *testing.T) {
	cases := []struct {
		name  string
		frame map[string]any
		want  string
	}{
		{"a named actor", map[string]any{"trigger": " owner "}, "owner"},
		{"an older producer sends no trigger", map[string]any{"topic": "chat"}, ""},
		{"a null trigger", map[string]any{"trigger": nil}, ""},
		{"a nil frame", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := frameTrigger(tc.frame); got != tc.want {
				t.Errorf("frameTrigger(%v) = %q, want %q", tc.frame, got, tc.want)
			}
		})
	}
}

func TestIsSelfEcho(t *testing.T) {
	cases := []struct {
		name    string
		trigger string
		myID    string
		want    bool
	}{
		{"my own action pushed back at me", "m-1", "m-1", true},
		{"case does not change the answer", "M-1", " m-1 ", true},
		{"someone else's action", "owner", "m-1", false},
		{"unknown attribution is never an echo", "", "m-1", false},
		{"a blank id of my own is never an echo", "owner", "  ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSelfEcho(tc.trigger, tc.myID); got != tc.want {
				t.Errorf("isSelfEcho(%q, %q) = %v, want %v", tc.trigger, tc.myID, got, tc.want)
			}
		})
	}
}

func TestPreviewLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"whitespace collapses to single spaces", "  fix   the\n\tlistener  ", 48, "fix the listener"},
		{"a title at the cap is not truncated", "abcde", 5, "abcde"},
		{"one rune past the cap truncates with an ellipsis", "abcdef", 5, "abcde…"},
		{"truncation counts runes, not bytes", "壹貳參肆伍陸", 3, "壹貳參…"},
		{"an empty title stays empty", "   ", 5, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := previewLine(tc.in, tc.max); got != tc.want {
				t.Errorf("previewLine(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

func TestRenderMessageBody(t *testing.T) {
	t.Run("a one-line body prints verbatim", func(t *testing.T) {
		if got := renderMessageBody("這個再確認一下", chatBodyAuthority); got != "這個再確認一下" {
			t.Errorf("renderMessageBody = %q, want %q", got, "這個再確認一下")
		}
	})

	t.Run("continuation lines are indented so none can look like a new event", func(t *testing.T) {
		want := "first\n    second\n    third"
		if got := renderMessageBody("first\nsecond\nthird", chatBodyAuthority); got != want {
			t.Errorf("renderMessageBody = %q, want %q", got, want)
		}
	})

	t.Run("a dangling trailing newline leaves no empty indented tail", func(t *testing.T) {
		if got := renderMessageBody("body\n\n", chatBodyAuthority); got != "body" {
			t.Errorf("renderMessageBody = %q, want %q", got, "body")
		}
	})

	t.Run("a body at the safety valve is untouched", func(t *testing.T) {
		body := strings.Repeat("a", messageBodyValve)
		if got := renderMessageBody(body, chatBodyAuthority); got != body {
			t.Errorf("renderMessageBody truncated a body of exactly %d bytes", messageBodyValve)
		}
	})

	t.Run("a pathological body is cut with a pointer to the authority", func(t *testing.T) {
		body := strings.Repeat("a", messageBodyValve+100)
		want := strings.Repeat("a", messageBodyValve) +
			"… [+100 bytes past the 64 KiB safety valve — read the full message with get_chat]"
		if got := renderMessageBody(body, chatBodyAuthority); got != want {
			t.Errorf("renderMessageBody = %q…%q, want the valve notice %q",
				got[:20], got[len(got)-90:], want[len(want)-90:])
		}
	})

	t.Run("the cut never splits a multi-byte rune", func(t *testing.T) {
		body := strings.Repeat("a", messageBodyValve-1) + strings.Repeat("界", 40)
		want := strings.Repeat("a", messageBodyValve-1) +
			"… [+120 bytes past the 64 KiB safety valve — read the full message with get_reply_card]"
		if got := renderMessageBody(body, replyCardBodyAuthority); got != want {
			t.Errorf("renderMessageBody tail = %q, want %q", got[len(got)-90:], want[len(want)-90:])
		}
	})
}

func TestHandleEvent(t *testing.T) {
	cases := []struct {
		name    string
		frame   map[string]any
		trigger string
		want    string
	}{
		{
			name:    "an attributed wake carries who moved it",
			frame:   map[string]any{"seq": 42.0, "topic": "task"},
			trigger: "owner",
			want:    "[ocagent] wake seq=42 topic=task · by owner\n",
		},
		{
			name:  "an unattributed wake carries no suffix rather than lying",
			frame: map[string]any{"seq": 42.0, "topic": "action"},
			want:  "[ocagent] wake seq=42 topic=action\n",
		},
		{
			name:  "a junk frame still wakes",
			frame: map[string]any{},
			want:  "[ocagent] wake seq=None topic=None\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			handleEvent(tc.frame, tc.trigger, &out)
			if out.String() != tc.want {
				t.Errorf("printed %q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestHandleDirectedBand(t *testing.T) {
	cases := []struct {
		name  string
		frame map[string]any
		want  string
	}{
		{
			name: "the server-composed reason IS the message",
			frame: map[string]any{"topic": "context-high", "data": map[string]any{
				"reason": " 你的 context 已經到 85%,先收尾 "}},
			want: "[ocagent] signal context-high: 你的 context 已經到 85%,先收尾\n",
		},
		{
			name: "a context-high frame with no reason composes a terse fallback",
			frame: map[string]any{"topic": "context-high", "data": map[string]any{
				"level": "high", "pct": 85.0}},
			want: "[ocagent] signal context-high: context usage high (level=high pct=85) — " +
				"close out your in-flight state before the handover\n",
		},
		{
			name: "a token-expiry frame with no reason composes a terse fallback",
			frame: map[string]any{"topic": "token-expiry", "data": map[string]any{
				"expires_in": 600.0}},
			want: "[ocagent] signal token-expiry: agent token expires in 600s — " +
				"checkpoint this turn, then call restart_self\n",
		},
		{
			name: "a task-close frame with no reason composes a terse fallback",
			frame: map[string]any{"topic": "task-close", "data": map[string]any{
				"task_no": "T-be18", "type": "build", "status": "done"}},
			want: "[ocagent] signal task-close: task T-be18 (type=build) closed (done) — " +
				"walk your close-out: clean this run's scratch, then report_task_closeout\n",
		},
		{
			name:  "a frame with no data object still prints rather than being dropped",
			frame: map[string]any{"topic": "token-expiry"},
			want: "[ocagent] signal token-expiry: agent token expires in s — " +
				"checkpoint this turn, then call restart_self\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			handleDirectedBand(tc.frame, &out)
			if out.String() != tc.want {
				t.Errorf("printed %q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestHandleTaskEvent(t *testing.T) {
	frame := func(id string) map[string]any {
		return map[string]any{"topic": "task", "seq": 5.0,
			"data": map[string]any{"payload": map[string]any{"id": id}}}
	}
	cfg := Config{Base: "http://x", Token: "t", ID: "m-1"}

	t.Run("the first sight this session states the position, not a diff", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			"/api/tasks/T1": `{"task_no":"T-be18","title":"fix the listener",` +
				`"status":"in_progress","progress_done":2,"progress_total":5}`,
		})
		var out bytes.Buffer
		snaps := map[string]taskSnap{}

		handleTaskEvent(client, cfg, frame("T1"), snaps, "owner", &out)

		want := "[ocagent] task T-be18「fix the listener」status=in_progress (2/5) · by owner\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		wantSnap := taskSnap{status: "in_progress", done: 2, total: 5}
		if snaps["T1"] != wantSnap {
			t.Errorf("snapshot = %+v, want %+v", snaps["T1"], wantSnap)
		}
	})

	t.Run("a status flip reads as an arrow between the two", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			"/api/tasks/T1": `{"task_no":"T-be18","title":"fix the listener",` +
				`"status":"done","progress_done":5,"progress_total":5}`,
		})
		var out bytes.Buffer
		snaps := map[string]taskSnap{"T1": {status: "in_progress", done: 2, total: 5}}

		handleTaskEvent(client, cfg, frame("T1"), snaps, "", &out)

		want := "[ocagent] task T-be18「fix the listener」status in_progress → done (5/5)\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("step progress alone reads as step done", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			"/api/tasks/T1": `{"task_no":"T-be18","title":"fix the listener",` +
				`"status":"in_progress","progress_done":3,"progress_total":5}`,
		})
		var out bytes.Buffer
		snaps := map[string]taskSnap{"T1": {status: "in_progress", done: 2, total: 5}}

		handleTaskEvent(client, cfg, frame("T1"), snaps, "owner", &out)

		want := "[ocagent] task T-be18「fix the listener」step done (3/5) · by owner\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("anything else that moved reads as updated", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			"/api/tasks/T1": `{"task_no":"T-be18","title":"","status":"in_progress"}`,
		})
		var out bytes.Buffer
		snaps := map[string]taskSnap{"T1": {status: "in_progress"}}

		handleTaskEvent(client, cfg, frame("T1"), snaps, "", &out)

		want := "[ocagent] task T-be18 updated\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q — with no title there is a space before what moved",
				out.String(), want)
		}
	})

	t.Run("a refetch fault says the task DID change rather than going silent", func(t *testing.T) {
		client := newRoutedHTTP(nil)
		var out bytes.Buffer

		handleTaskEvent(client, cfg, frame("T9"), map[string]taskSnap{}, "owner", &out)

		want := "[ocagent] task T9 changed but refetch failed (HTTP 404) — " +
			"read it manually (get_task) · by owner\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a frame with no id degrades to the generic wake and asks nothing", func(t *testing.T) {
		client := newRoutedHTTP(nil)
		var out bytes.Buffer

		handleTaskEvent(client, cfg, map[string]any{"topic": "task", "seq": 5.0},
			map[string]taskSnap{}, "owner", &out)

		want := "[ocagent] wake seq=5 topic=task · by owner\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if client.asked != nil {
			t.Errorf("asked %v, want nothing — a junk hint routes no refetch", client.asked)
		}
	})
}

func TestIntField(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		{"a JSON number", 5.0, 5},
		{"a fraction truncates toward zero", 5.9, 5},
		{"a string", "5", 0},
		{"nil", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := intField(tc.in); got != tc.want {
				t.Errorf("intField(%v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestFetchChat(t *testing.T) {
	cfg := Config{Base: "http://x", Token: "t", ID: "kyle"}
	const page1 = "/api/chat?recipient=kyle&unread=true&limit=50"

	t.Run("a single page ends the walk when the server issues no cursor", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			page1: `{"messages":[{"id":"a1"},{"id":"a2"}]}`,
		})

		got := fetchChat(client, cfg, "kyle")

		wantRows := []map[string]any{{"id": "a1"}, {"id": "a2"}}
		if !reflect.DeepEqual(got.rows, wantRows) {
			t.Errorf("rows = %v, want %v", got.rows, wantRows)
		}
		if got.stop != "" {
			t.Errorf("stop = %q, want empty — the server said this is the end", got.stop)
		}
		if !reflect.DeepEqual(client.asked, []string{page1}) {
			t.Errorf("asked %v, want just the first page", client.asked)
		}
	})

	t.Run("the walk follows next_cursor oldest first across pages", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			page1:                `{"messages":[{"id":"a1"}],"next_cursor":"c2"}`,
			page1 + "&cursor=c2": `{"messages":[{"id":"a2"}],"next_cursor":"c3"}`,
			page1 + "&cursor=c3": `{"messages":[{"id":"a3"}]}`,
		})

		got := fetchChat(client, cfg, "kyle")

		wantRows := []map[string]any{{"id": "a1"}, {"id": "a2"}, {"id": "a3"}}
		if !reflect.DeepEqual(got.rows, wantRows) {
			t.Errorf("rows = %v, want %v", got.rows, wantRows)
		}
		if got.stop != "" {
			t.Errorf("stop = %q, want empty", got.stop)
		}
		wantAsked := []string{page1, page1 + "&cursor=c2", page1 + "&cursor=c3"}
		if !reflect.DeepEqual(client.asked, wantAsked) {
			t.Errorf("asked %v, want %v", client.asked, wantAsked)
		}
	})

	t.Run("a first-page fault answers nil rows so it is not read as an empty inbox", func(t *testing.T) {
		client := newRoutedHTTP(nil)
		client.status[page1] = 500

		got := fetchChat(client, cfg, "kyle")

		if got.rows != nil {
			t.Errorf("rows = %v, want nil — \"zero messages\" and \"I could not look\" must "+
				"not be the same answer", got.rows)
		}
		want := "[ocagent] chat: 補印一頁都沒撈到（HTTP 500）—— 這不是「沒有新訊息」，" +
			"是這次沒問到。未讀原封不動，下一次補印會再試；等不及就用 get_chat 自己撈。\n"
		if got.stop != want {
			t.Errorf("stop = %q, want %q", got.stop, want)
		}
	})

	t.Run("a later-page fault keeps what is already in hand and says so", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			page1: `{"messages":[{"id":"a1"}],"next_cursor":"c2"}`,
		})
		client.status[page1+"&cursor=c2"] = 503

		got := fetchChat(client, cfg, "kyle")

		wantRows := []map[string]any{{"id": "a1"}}
		if !reflect.DeepEqual(got.rows, wantRows) {
			t.Errorf("rows = %v, want %v", got.rows, wantRows)
		}
		want := "[ocagent] chat: 補印在第 2 頁斷掉了（已經撈到 1 則）—— 未讀沒撈完，" +
			"剩下的下一次補印會再試；等不及就用 get_chat 自己回頭撈。\n"
		if got.stop != want {
			t.Errorf("stop = %q, want %q", got.stop, want)
		}
	})

	t.Run("a cursor that does not advance stops the walk", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			page1:                `{"messages":[{"id":"a1"}],"next_cursor":"c2"}`,
			page1 + "&cursor=c2": `{"messages":[{"id":"a2"}],"next_cursor":"c2"}`,
		})

		got := fetchChat(client, cfg, "kyle")

		if len(got.rows) != 2 {
			t.Errorf("rows = %v, want the two pages it did read", got.rows)
		}
		want := "[ocagent] chat: 補印停在第 2 頁（已經撈到 2 則）—— server 的 next_cursor 沒有前進，" +
			"同一個游標又發了一次。未讀沒撈完，請用 get_chat 自己回頭撈。\n"
		if got.stop != want {
			t.Errorf("stop = %q, want %q", got.stop, want)
		}
	})

	t.Run("a server that never stops is bounded at ten pages", func(t *testing.T) {
		routes := map[string]string{page1: `{"messages":[{"id":"a"}],"next_cursor":"c1"}`}
		for i := 1; i <= 12; i++ {
			routes[page1+"&cursor=c"+strconv.Itoa(i)] =
				`{"messages":[{"id":"a"}],"next_cursor":"c` + strconv.Itoa(i+1) + `"}`
		}
		client := newRoutedHTTP(routes)

		got := fetchChat(client, cfg, "kyle")

		if len(got.rows) != 10 {
			t.Errorf("rows = %d, want 10 — the walk refuses more than ten pages", len(got.rows))
		}
		want := "[ocagent] chat: 補印撈到第 10 頁就停了（已經撈到 10 則），server 還在給 next_cursor —— " +
			"這是分頁上限，不是你的信箱真有這麼多。未讀沒撈完，請用 get_chat 自己回頭撈。\n"
		if got.stop != want {
			t.Errorf("stop = %q, want %q", got.stop, want)
		}
		if len(client.asked) != 10 {
			t.Errorf("asked %d pages, want 10 — the walk bounds a runaway server's request cost",
				len(client.asked))
		}
	})
}

func TestFmtAgo(t *testing.T) {
	cases := []struct {
		secs float64
		want string
	}{
		{-5, "0s"},
		{0, "0s"},
		{10.9, "10s"},
		{59, "59s"},
		{60, "1m"},
		{3599, "59m"},
		{3600, "1h"},
		{86399, "23h"},
		{86400, "1d"},
		{259200, "3d"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := fmtAgo(tc.secs); got != tc.want {
				t.Errorf("fmtAgo(%v) = %q, want %q", tc.secs, got, tc.want)
			}
		})
	}
}

func TestAttachmentSummary(t *testing.T) {
	atts := func(kinds ...bool) map[string]any {
		refs := make([]any, 0, len(kinds))
		for _, isImage := range kinds {
			refs = append(refs, map[string]any{"is_image": isImage})
		}
		return map[string]any{"attachments": refs}
	}
	cases := []struct {
		name string
		m    map[string]any
		want string
	}{
		{"two images", atts(true, true), "📎2圖"},
		{"one file", atts(false), "📎1檔"},
		{"a mix reads images then files", atts(true, false, false), "📎1圖 2檔"},
		{"an empty attachments array carries no badge", map[string]any{"attachments": []any{}}, ""},
		{"no attachments field carries no badge", map[string]any{"body": "hi"}, ""},
		{"a non-array attachments field carries no badge", map[string]any{"attachments": "att-1"}, ""},
		{"non-map entries are skipped", map[string]any{"attachments": []any{"att-1", map[string]any{"is_image": true}}}, "📎1圖"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := attachmentSummary(tc.m); got != tc.want {
				t.Errorf("attachmentSummary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewAckGate(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		answers io.Reader
		wantNil bool
	}{
		{"the parent asked for acks", map[string]string{listenAckEnv: "1"}, strings.NewReader(""), false},
		{"the claude path asks for nothing", nil, strings.NewReader(""), true},
		{"a value other than 1 is not an ask", map[string]string{listenAckEnv: "true"}, strings.NewReader(""), true},
		{"no stdin to answer on", map[string]string{listenAckEnv: "1"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := newAckGate(testEnv(tc.env), tc.answers)
			if (got == nil) != tc.wantNil {
				t.Errorf("newAckGate = %v, want nil == %v", got, tc.wantNil)
			}
		})
	}

	t.Run("a nil env is the claude path", func(t *testing.T) {
		if got := newAckGate(nil, strings.NewReader("")); got != nil {
			t.Errorf("newAckGate = %v, want nil", got)
		}
	})
}

func TestConfirm(t *testing.T) {
	newGate := func(t *testing.T, answers string) *ackGate {
		t.Helper()
		g := newAckGate(testEnv(map[string]string{listenAckEnv: "1"}), strings.NewReader(answers))
		if g == nil {
			t.Fatal("newAckGate = nil with OC_LISTEN_ACK=1")
		}
		return g
	}

	t.Run("the claude path has no gate object and always says delivered", func(t *testing.T) {
		var out bytes.Buffer
		var g *ackGate
		if !g.confirm(&out) {
			t.Error("confirm = false on the claude path, want true")
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing — there is no marker on the claude path", out.String())
		}
	})

	t.Run("an ack on the open token confirms delivery", func(t *testing.T) {
		var out bytes.Buffer
		if !newGate(t, "ack 1\n").confirm(&out) {
			t.Error("confirm = false after `ack 1`, want true")
		}
		if out.String() != "[ocagent] listen: batch 1\n" {
			t.Errorf("printed %q, want %q", out.String(), "[ocagent] listen: batch 1\n")
		}
	})

	t.Run("a nack on the open token refuses delivery", func(t *testing.T) {
		var out bytes.Buffer
		if newGate(t, "nack 1\n").confirm(&out) {
			t.Error("confirm = true after `nack 1`, want false")
		}
		if out.String() != "[ocagent] listen: batch 1\n" {
			t.Errorf("printed %q, want %q", out.String(), "[ocagent] listen: batch 1\n")
		}
	})

	t.Run("an answer to a closed batch is passed over", func(t *testing.T) {
		var out bytes.Buffer
		if !newGate(t, "ack 7\nnack 99\nack 1\n").confirm(&out) {
			t.Error("confirm = false, want true — only the answer on the open token counts")
		}
	})

	t.Run("the token advances with each batch", func(t *testing.T) {
		var out bytes.Buffer
		g := newGate(t, "ack 1\nack 2\n")
		if !g.confirm(&out) || !g.confirm(&out) {
			t.Error("confirm = false, want two confirmed batches")
		}
		want := "[ocagent] listen: batch 1\n[ocagent] listen: batch 2\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a closed stdin means nobody can say it was delivered", func(t *testing.T) {
		var out bytes.Buffer
		if newGate(t, "").confirm(&out) {
			t.Error("confirm = true with a closed stdin, want false")
		}
	})

	t.Run("a batch nobody answers times out loudly and counts as undelivered", func(t *testing.T) {
		pr, pw := io.Pipe()
		t.Cleanup(func() { _ = pw.Close() })
		g := newAckGate(testEnv(map[string]string{listenAckEnv: "1"}), pr)
		g.wait = 20 * time.Millisecond
		var out bytes.Buffer

		if g.confirm(&out) {
			t.Error("confirm = true after the deadline, want false")
		}
		want := "[ocagent] listen: batch 1\n" +
			"[ocagent] 等不到「已送達」的回覆（batch 1，等了 20ms）—— " +
			"這一批訊息**沒有**被算成你看過了，下一次補印會再送一次。" +
			"如果這行一直出現，收訊這條路的另一端有問題。\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})
}

func TestNoteChatFetchFault(t *testing.T) {
	const line = "[ocagent] chat: 補印一頁都沒撈到（HTTP 500）\n"

	t.Run("the fault is announced once per episode", func(t *testing.T) {
		var out bytes.Buffer
		warn := &drainWarner{}
		noteChatFetchFault(warn, &out, line)
		noteChatFetchFault(warn, &out, line)

		if out.String() != line {
			t.Errorf("printed %q, want the line exactly once (%q)", out.String(), line)
		}
		if !warn.chatFaultOpen {
			t.Error("chatFaultOpen = false, want the episode left open")
		}
	})

	t.Run("a nil warner fails loud rather than swallowing the only signal", func(t *testing.T) {
		var out bytes.Buffer
		noteChatFetchFault(nil, &out, line)
		noteChatFetchFault(nil, &out, line)

		if out.String() != line+line {
			t.Errorf("printed %q, want the line both times", out.String())
		}
	})

	t.Run("an empty line opens no episode", func(t *testing.T) {
		var out bytes.Buffer
		warn := &drainWarner{}
		noteChatFetchFault(warn, &out, "")

		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
		if warn.chatFaultOpen {
			t.Error("chatFaultOpen = true with nothing said, want false")
		}
	})
}

func TestClearChatFetchFault(t *testing.T) {
	t.Run("closing an open episode says so", func(t *testing.T) {
		var out bytes.Buffer
		warn := &drainWarner{chatFaultOpen: true}
		clearChatFetchFault(warn, &out)

		want := "[ocagent] chat: 補印又問得到了 —— 上面那次「一頁都沒撈到」到此為止。" +
			"接下來印出來的就是這次真的撈到的東西；沒有東西就是真的沒有新訊息。\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if warn.chatFaultOpen {
			t.Error("chatFaultOpen = true after the recovery line, want false")
		}
	})

	t.Run("no open episode says nothing", func(t *testing.T) {
		var out bytes.Buffer
		clearChatFetchFault(&drainWarner{}, &out)
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
	})

	t.Run("a nil warner suppressed nothing so it announces nothing", func(t *testing.T) {
		var out bytes.Buffer
		clearChatFetchFault(nil, &out)
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
	})
}

func TestWarnMarkReadFailed(t *testing.T) {
	const want = "[ocagent] mark-read 沒送成功（peer=alice，HTTP 503）— 訊息已經印出來了，" +
		"但是回條沒送成功，server 那邊就還算未讀：這一批下一次補印會再印一次，" +
		"而且會一直重印到回條送成功為止，在那之前對方的已讀勾也不會亮。" +
		"這個行程只會講這一次 —— 之後再看到同一批訊息重複出現，原因就是這一行。\n"

	t.Run("the loss is said once per process, whatever peer flaps next", func(t *testing.T) {
		var out bytes.Buffer
		warn := &drainWarner{}
		warnMarkReadFailed(warn, &out, "alice", 503)
		warnMarkReadFailed(warn, &out, "bob", 500)

		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a nil warner fails loud", func(t *testing.T) {
		var out bytes.Buffer
		warnMarkReadFailed(nil, &out, "alice", 503)
		warnMarkReadFailed(nil, &out, "alice", 503)

		if out.String() != want+want {
			t.Errorf("printed %q, want the line both times", out.String())
		}
	})
}

func TestPrintChatLine(t *testing.T) {
	cases := []struct {
		name string
		m    map[string]any
		want string
	}{
		{
			name: "every tag slot filled",
			m: map[string]any{"from": "boss", "id": "c-reply", "reply_to": "c-target",
				"ts": 1000.0, "body": "這個再確認一下"},
			want: "[ocagent] chat from boss (#c-reply, ↩#c-target, 2m ago): 這個再確認一下\n",
		},
		{
			name: "a blank reply_to drops just that slot",
			m:    map[string]any{"from": "boss", "id": "c-1", "reply_to": "  ", "ts": 1000.0, "body": "hi"},
			want: "[ocagent] chat from boss (#c-1, 2m ago): hi\n",
		},
		{
			name: "no id, no reply_to and no ts prints without the parenthesised tag",
			m:    map[string]any{"from": "boss", "body": "hi"},
			want: "[ocagent] chat from boss: hi\n",
		},
		{
			name: "an attachment badge trails the body",
			m: map[string]any{"from": "boss", "id": "c-1", "body": "看一下",
				"attachments": []any{map[string]any{"is_image": true}, map[string]any{}}},
			want: "[ocagent] chat from boss (#c-1): 看一下 📎1圖 1檔\n",
		},
		{
			name: "an attachment-only message is the badge itself",
			m: map[string]any{"from": "boss", "id": "c-1", "body": "",
				"attachments": []any{map[string]any{"is_image": true}}},
			want: "[ocagent] chat from boss (#c-1): 📎1圖\n",
		},
		{
			name: "a multi-line body stays one event block",
			m:    map[string]any{"from": "boss", "id": "c-1", "body": "one\ntwo"},
			want: "[ocagent] chat from boss (#c-1): one\n    two\n",
		},
		{
			name: "a non-positive ts carries no age",
			m:    map[string]any{"from": "boss", "id": "c-1", "ts": 0.0, "body": "hi"},
			want: "[ocagent] chat from boss (#c-1): hi\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			printChatLine(&out, tc.m, 1120)
			if out.String() != tc.want {
				t.Errorf("printed %q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestHandleReplyCard(t *testing.T) {
	cfg := Config{Base: "http://x", Token: "t", ID: "m-1", Home: "/unused"}
	frame := func(id, from string) map[string]any {
		return map[string]any{"topic": "reply_card",
			"data": map[string]any{"payload": map[string]any{"id": id, "from": from}}}
	}
	newSeen := func(t *testing.T) *replyCardSeen {
		t.Helper()
		return loadReplyCardSeen(filepath.Join(t.TempDir(), "replycards-seen"))
	}

	t.Run("an answered card of mine prints the answer and is recorded", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			"/api/reply-cards/rc-1": `{"id":"rc-1","from":"m-1","status":"answered",` +
				`"answered_ts":1700,"summary":"要不要改 schema?",` +
				`"options":[{"text":"改"},{"text":"不改"}],"answer":{"option_idxs":[0]}}`,
		})
		var out bytes.Buffer
		seen := newSeen(t)

		handleReplyCard(client, cfg, frame("rc-1", "m-1"), seen, "owner", &out)

		want := "[ocagent] reply-card rc-1 answered: picked [0] \"改\" | asked: 要不要改 schema? · by owner\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if !seen.has("rc-1", 1700) {
			t.Error("the answer was printed but not recorded — the next drain would repeat it")
		}
	})

	t.Run("the same answer never prints twice", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			"/api/reply-cards/rc-1": `{"id":"rc-1","from":"m-1","status":"answered",` +
				`"answered_ts":1700,"summary":"要不要改 schema?","answer":{"text":"改"}}`,
		})
		var out bytes.Buffer
		seen := newSeen(t)

		handleReplyCard(client, cfg, frame("rc-1", "m-1"), seen, "owner", &out)
		first := out.String()
		handleReplyCard(client, cfg, frame("rc-1", "m-1"), seen, "owner", &out)

		if out.String() != first {
			t.Errorf("printed %q, want the answer exactly once (%q)", out.String(), first)
		}
	})

	t.Run("an expired card prints the self-carrying guidance", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			"/api/reply-cards/rc-2": `{"id":"rc-2","from":"m-1","status":"expired",` +
				`"expired_ts":1800,"summary":"要不要改 schema?"}`,
		})
		var out bytes.Buffer

		handleReplyCard(client, cfg, frame("rc-2", "m-1"), newSeen(t), "owner", &out)

		want := "[ocagent] reply-card rc-2 EXPIRED (no answer) | asked: 要不要改 schema? — " +
			"settled without an answer: if the question still matters, open a FRESH card " +
			"with current context; if not, proceed / close out. Any held step/task was " +
			"already restored to in_progress · by owner\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("another member's card costs no refetch and no line", func(t *testing.T) {
		client := newRoutedHTTP(nil)
		var out bytes.Buffer

		handleReplyCard(client, cfg, frame("rc-3", "m-2"), newSeen(t), "owner", &out)

		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
		if client.asked != nil {
			t.Errorf("asked %v, want nothing — the payload's `from` pre-filters the fan-out",
				client.asked)
		}
	})

	t.Run("the authority overrules a payload that claims the card is mine", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			"/api/reply-cards/rc-4": `{"id":"rc-4","from":"m-2","status":"answered",` +
				`"answered_ts":1700,"answer":{"text":"改"}}`,
		})
		var out bytes.Buffer

		handleReplyCard(client, cfg, frame("rc-4", "m-1"), newSeen(t), "owner", &out)

		if out.String() != "" {
			t.Errorf("printed %q, want nothing — a stale payload costs one wasted GET, "+
				"never a wrong line", out.String())
		}
	})

	t.Run("a card still open is my own create echo", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			"/api/reply-cards/rc-5": `{"id":"rc-5","from":"m-1","status":"open"}`,
		})
		var out bytes.Buffer

		handleReplyCard(client, cfg, frame("rc-5", "m-1"), newSeen(t), "", &out)

		if out.String() != "" {
			t.Errorf("printed %q, want nothing to wake on yet", out.String())
		}
	})

	t.Run("a refetch fault says the card DID change", func(t *testing.T) {
		client := newRoutedHTTP(nil)
		var out bytes.Buffer

		handleReplyCard(client, cfg, frame("rc-6", "m-1"), newSeen(t), "owner", &out)

		want := "[ocagent] reply-card rc-6 changed but refetch failed (HTTP 404) — " +
			"read it manually (get_reply_card).\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a frame with no id routes nothing", func(t *testing.T) {
		client := newRoutedHTTP(nil)
		var out bytes.Buffer

		handleReplyCard(client, cfg, map[string]any{"topic": "reply_card"}, newSeen(t), "", &out)
		handleReplyCard(client, cfg, frame("  ", "m-1"), newSeen(t), "", &out)

		if out.String() != "" || client.asked != nil {
			t.Errorf("printed %q and asked %v, want neither", out.String(), client.asked)
		}
	})
}

func TestPrintReplyCardAnswered(t *testing.T) {
	card := map[string]any{"summary": "要不要改 schema?", "answer": map[string]any{"text": "改"}}

	t.Run("the live path carries who answered", func(t *testing.T) {
		var out bytes.Buffer
		printReplyCardAnswered(&out, "rc-1", card, "owner")
		want := "[ocagent] reply-card rc-1 answered: \"改\" | asked: 要不要改 schema? · by owner\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("the drain path has no frame and adds no suffix", func(t *testing.T) {
		var out bytes.Buffer
		printReplyCardAnswered(&out, "rc-1", card, "")
		want := "[ocagent] reply-card rc-1 answered: \"改\" | asked: 要不要改 schema?\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})
}

func TestPrintReplyCardExpired(t *testing.T) {
	t.Run("the body never names a presser", func(t *testing.T) {
		var out bytes.Buffer
		printReplyCardExpired(&out, "rc-2", map[string]any{"summary": "要不要改 schema?"}, "m-1")
		want := "[ocagent] reply-card rc-2 EXPIRED (no answer) | asked: 要不要改 schema? — " +
			"settled without an answer: if the question still matters, open a FRESH card " +
			"with current context; if not, proceed / close out. Any held step/task was " +
			"already restored to in_progress · by m-1\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("the drain path adds no attribution suffix", func(t *testing.T) {
		var out bytes.Buffer
		printReplyCardExpired(&out, "rc-2", map[string]any{"summary": "q"}, "")
		want := "[ocagent] reply-card rc-2 EXPIRED (no answer) | asked: q — " +
			"settled without an answer: if the question still matters, open a FRESH card " +
			"with current context; if not, proceed / close out. Any held step/task was " +
			"already restored to in_progress\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})
}

func TestRenderReplyCardAnswer(t *testing.T) {
	cases := []struct {
		name string
		card map[string]any
		want string
	}{
		{
			name: "a full card resolves each index to its own wording",
			card: map[string]any{
				"options": []any{map[string]any{"text": "改"}, map[string]any{"text": "不改"}},
				"answer":  map[string]any{"option_idxs": []any{1.0, 0.0}},
			},
			want: `picked [1] "不改" — picked [0] "改"`,
		},
		{
			name: "the light row's own wording list wins",
			card: map[string]any{"answer": map[string]any{
				"option_idxs": []any{2.0}, "options": []any{"照原案走"}}},
			want: `picked [2] "照原案走"`,
		},
		{
			name: "an index with no wording anywhere still names the choice",
			card: map[string]any{"answer": map[string]any{"option_idxs": []any{5.0}}},
			want: "picked [5]",
		},
		{
			name: "options, text and attachments join in that order",
			card: map[string]any{
				"options": []any{map[string]any{"text": "改"}},
				"answer": map[string]any{"option_idxs": []any{0.0}, "text": " 但先跑一輪 ",
					"attachments": []any{map[string]any{}, map[string]any{}}},
			},
			want: `picked [0] "改" — "但先跑一輪" — +2 attachment(s)`,
		},
		{
			name: "the light row's attachment COUNT is read the same way",
			card: map[string]any{"answer": map[string]any{"text": "改", "attachments": 3.0}},
			want: `"改" — +3 attachment(s)`,
		},
		{
			name: "no answer on the payload is not a failure",
			card: map[string]any{"summary": "q"},
			want: "(no answer carried on this payload)",
		},
		{
			name: "an answer this build cannot read is named as a read failure",
			card: map[string]any{"answer": map[string]any{"option_idx": 0.0}},
			want: unreadableAnswerNotice,
		},
		{
			name: "a non-numeric index is skipped rather than fabricated",
			card: map[string]any{"answer": map[string]any{"option_idxs": []any{"0"}, "text": "改"}},
			want: `"改"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderReplyCardAnswer(tc.card); got != tc.want {
				t.Errorf("renderReplyCardAnswer = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReplyCardSeenPath(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"an id is lowercased", Config{Home: "/h", ID: "M-Kyle"}, "/h/m-kyle/replycards-seen"},
		{"no id falls back to anon", Config{Home: "/h"}, "/h/anon/replycards-seen"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := replyCardSeenPath(tc.cfg); got != tc.want {
				t.Errorf("replyCardSeenPath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadReplyCardSeen(t *testing.T) {
	load := func(t *testing.T, body string) *replyCardSeen {
		t.Helper()
		path := filepath.Join(t.TempDir(), "replycards-seen")
		if body != "" {
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return loadReplyCardSeen(path)
	}

	t.Run("a persisted baseline comes back primed", func(t *testing.T) {
		s := load(t, `{"rc-1":1700}`)
		if !s.primed {
			t.Error("primed = false, want true — a loaded baseline is a baseline")
		}
		if !s.has("rc-1", 1700) {
			t.Error("has(rc-1, 1700) = false, want true")
		}
		if s.has("rc-1", 1800) {
			t.Error("has(rc-1, 1800) = true, want false — a revision bumps the ts and re-prints")
		}
	})

	t.Run("a missing file is an unprimed store", func(t *testing.T) {
		s := load(t, "")
		if s.primed {
			t.Error("primed = true on a brand-new agent home, want false — the first drain " +
				"baselines silently rather than flooding a fresh session")
		}
		if len(s.m) != 0 {
			t.Errorf("m = %v, want empty", s.m)
		}
	})

	t.Run("a corrupt file re-primes the same silent way", func(t *testing.T) {
		if s := load(t, "{{{ not json"); s.primed || len(s.m) != 0 {
			t.Errorf("primed = %v, m = %v, want an unprimed empty store", s.primed, s.m)
		}
	})

	t.Run("a JSON null is not a baseline", func(t *testing.T) {
		if s := load(t, "null"); s.primed || len(s.m) != 0 {
			t.Errorf("primed = %v, m = %v, want an unprimed empty store", s.primed, s.m)
		}
	})
}

func TestHas(t *testing.T) {
	s := &replyCardSeen{m: map[string]float64{"rc-1": 1700}}
	cases := []struct {
		name string
		id   string
		ts   float64
		want bool
	}{
		{"the exact answer already surfaced", "rc-1", 1700, true},
		{"a revision bumped the ts", "rc-1", 1800, false},
		{"a card never surfaced", "rc-9", 1700, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.has(tc.id, tc.ts); got != tc.want {
				t.Errorf("has(%q, %v) = %v, want %v", tc.id, tc.ts, got, tc.want)
			}
		})
	}
}

func TestRecord(t *testing.T) {
	t.Run("one answer lands on disk immediately and survives a reload", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kyle", "replycards-seen")
		s := loadReplyCardSeen(path)

		s.record("rc-1", 1700)

		if got := readFileString(t, path); got != `{"rc-1":1700}` {
			t.Errorf("file = %q, want %q", got, `{"rc-1":1700}`)
		}
		if !s.primed {
			t.Error("primed = false after a successful persist, want true")
		}
		reloaded := loadReplyCardSeen(path)
		if !reloaded.has("rc-1", 1700) {
			t.Error("the reloaded store does not carry the answer — a kill right after the " +
				"print would re-surface it")
		}
	})

	t.Run("a persist that cannot land leaves the store unprimed", func(t *testing.T) {
		blocked := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		s := loadReplyCardSeen(filepath.Join(blocked, "replycards-seen"))

		s.record("rc-1", 1700)

		if s.primed {
			t.Error("primed = true after a failed write, want false")
		}
		if !s.has("rc-1", 1700) {
			t.Error("the in-memory dedup lost the answer it just recorded")
		}
	})
}

func TestDrainReplyCards(t *testing.T) {
	cfg := Config{Base: "http://x", Token: "t", ID: "m-1"}
	const answeredPane = "/api/reply-cards?status=answered"
	const expiredPane = "/api/reply-cards?status=expired"

	t.Run("the first run ever primes the baseline and prints nothing", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			answeredPane: `[{"id":"rc-1","from":"m-1","answered_ts":1700,"summary":"q1",` +
				`"answer":{"text":"改"}}]`,
			expiredPane: `[]`,
		})
		path := filepath.Join(t.TempDir(), "replycards-seen")
		seen := loadReplyCardSeen(path)
		var out bytes.Buffer

		if n := drainReplyCards(client, cfg, seen, &out); n != 0 {
			t.Errorf("printed %d lines, want 0 on a brand-new agent home", n)
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
		if got := readFileString(t, path); got != `{"rc-1":1700}` {
			t.Errorf("baseline = %q, want %q", got, `{"rc-1":1700}`)
		}
	})

	t.Run("a primed store prints my not-yet-surfaced outcomes oldest first", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			answeredPane: `[{"id":"rc-2","from":"m-1","answered_ts":1800,"summary":"q2",` +
				`"answer":{"text":"新的"}},` +
				`{"id":"rc-1","from":"m-1","answered_ts":1700,"summary":"q1",` +
				`"answer":{"text":"舊的"}}]`,
			expiredPane: `[{"id":"rc-3","from":"m-1","expired_ts":1900,"summary":"q3"}]`,
		})
		path := filepath.Join(t.TempDir(), "replycards-seen")
		if err := os.WriteFile(path, []byte(`{"rc-0":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
		seen := loadReplyCardSeen(path)
		var out bytes.Buffer

		if n := drainReplyCards(client, cfg, seen, &out); n != 3 {
			t.Errorf("printed %d lines, want 3", n)
		}
		want := "[ocagent] reply-card rc-1 answered: \"舊的\" | asked: q1\n" +
			"[ocagent] reply-card rc-2 answered: \"新的\" | asked: q2\n" +
			"[ocagent] reply-card rc-3 EXPIRED (no answer) | asked: q3 — " +
			"settled without an answer: if the question still matters, open a FRESH card " +
			"with current context; if not, proceed / close out. Any held step/task was " +
			"already restored to in_progress\n"
		if out.String() != want {
			t.Errorf("printed\n%q\nwant\n%q", out.String(), want)
		}
		wantState := `{"rc-1":1700,"rc-2":1800,"rc-3":1900}`
		if got := readFileString(t, path); got != wantState {
			t.Errorf("state = %q, want %q — an entry absent from both panes has aged out "+
				"and is dropped so the file stays bounded", got, wantState)
		}
	})

	t.Run("an already-surfaced outcome stays quiet", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			answeredPane: `[{"id":"rc-1","from":"m-1","answered_ts":1700,"summary":"q1",` +
				`"answer":{"text":"改"}}]`,
			expiredPane: `[]`,
		})
		path := filepath.Join(t.TempDir(), "replycards-seen")
		if err := os.WriteFile(path, []byte(`{"rc-1":1700}`), 0o644); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer

		if n := drainReplyCards(client, cfg, loadReplyCardSeen(path), &out); n != 0 {
			t.Errorf("printed %d lines, want 0", n)
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
	})

	t.Run("another member's card in the owner-wide pane is not mine to print", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			answeredPane: `[{"id":"rc-9","from":"m-2","answered_ts":1700,"summary":"q",` +
				`"answer":{"text":"改"}}]`,
			expiredPane: `[]`,
		})
		path := filepath.Join(t.TempDir(), "replycards-seen")
		if err := os.WriteFile(path, []byte(`{"rc-0":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer

		if n := drainReplyCards(client, cfg, loadReplyCardSeen(path), &out); n != 0 {
			t.Errorf("printed %d lines, want 0", n)
		}
		if got := readFileString(t, path); got != `{}` {
			t.Errorf("state = %q, want %q", got, `{}`)
		}
	})

	t.Run("a fault on either pane leaves the state untouched", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			answeredPane: `[{"id":"rc-1","from":"m-1","answered_ts":1700,"summary":"q1",` +
				`"answer":{"text":"改"}}]`,
		})
		client.status[expiredPane] = 500
		path := filepath.Join(t.TempDir(), "replycards-seen")
		if err := os.WriteFile(path, []byte(`{"rc-0":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer

		if n := drainReplyCards(client, cfg, loadReplyCardSeen(path), &out); n != 0 {
			t.Errorf("printed %d lines, want 0", n)
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
		if got := readFileString(t, path); got != `{"rc-0":1}` {
			t.Errorf("state = %q, want it untouched (%q) — a partial rebuild would drop the "+
				"other pane's entries", got, `{"rc-0":1}`)
		}
	})

	t.Run("a pane body that is not a list is a fault too", func(t *testing.T) {
		client := newRoutedHTTP(map[string]string{
			answeredPane: `{"cards":[]}`,
			expiredPane:  `[]`,
		})
		var out bytes.Buffer

		if n := drainReplyCards(client, cfg,
			loadReplyCardSeen(filepath.Join(t.TempDir(), "replycards-seen")), &out); n != 0 {
			t.Errorf("printed %d lines, want 0", n)
		}
	})
}

func TestStrOrEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"a string is itself", "hi", "hi"},
		{"an empty string", "", ""},
		{"nil", nil, ""},
		{"false is Python-falsy", false, ""},
		{"true", true, "True"},
		{"zero is Python-falsy", 0.0, ""},
		{"a whole number", 42.0, "42"},
		{"a fraction", 1.5, "1.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strOrEmpty(tc.in); got != tc.want {
				t.Errorf("strOrEmpty(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
