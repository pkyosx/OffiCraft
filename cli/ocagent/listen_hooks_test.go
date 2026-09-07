package main

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// hookHTTP answers one canned (status, body) and records the whole request line
// of every call it received.
type hookHTTP struct {
	status int
	body   string
	calls  []string
}

func (h *hookHTTP) Do(req *http.Request) (*http.Response, error) {
	h.calls = append(h.calls, req.Method+" "+req.URL.String()+" auth="+req.Header.Get("Authorization"))
	return &http.Response{
		StatusCode: h.status,
		Body:       io.NopCloser(strings.NewReader(h.body)),
		Header:     http.Header{},
	}, nil
}

func hookCfg() Config { return Config{Base: "http://station", Token: "tok", ID: "m-1"} }

// memberFrame is the member delta both hooks gate on, carrying an optional
// server-composed offboard notice.
func memberFrame(key, notice string) map[string]any {
	payload := map[string]any{"id": "m-1"}
	if notice != "" {
		payload["offboard_notice"] = notice
	}
	return map[string]any{
		"topic": memberTopic,
		"data":  map[string]any{"key": key, "payload": payload},
	}
}

func TestFetchMemberRow(t *testing.T) {
	t.Run("a 200 object is the authoritative row, fetched with the agent's token", func(t *testing.T) {
		client := &hookHTTP{status: 200, body: `{"id":"m-1","desired_state":"offline","refocus_since":0}`}

		row, ok := fetchMemberRow(client, hookCfg())

		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := map[string]any{"id": "m-1", "desired_state": "offline", "refocus_since": float64(0)}
		if !reflect.DeepEqual(row, want) {
			t.Errorf("row = %v, want %v", row, want)
		}
		wantCalls := []string{"GET http://station/api/members/m-1 auth=Bearer tok"}
		if !reflect.DeepEqual(client.calls, wantCalls) {
			t.Errorf("calls = %v, want %v", client.calls, wantCalls)
		}
	})

	t.Run("a non-200 is not a row", func(t *testing.T) {
		client := &hookHTTP{status: 503, body: `{"id":"m-1"}`}

		row, ok := fetchMemberRow(client, hookCfg())

		if row != nil || ok {
			t.Errorf("(%v, %v), want (nil, false) — a fault must not read as an intent", row, ok)
		}
	})

	t.Run("a 200 that is not an object is not a row", func(t *testing.T) {
		client := &hookHTTP{status: 200, body: `["m-1"]`}

		row, ok := fetchMemberRow(client, hookCfg())

		if row != nil || ok {
			t.Errorf("(%v, %v), want (nil, false)", row, ok)
		}
	})

	t.Run("a 200 carrying unparseable bytes is not a row", func(t *testing.T) {
		client := &hookHTTP{status: 200, body: `not json`}

		row, ok := fetchMemberRow(client, hookCfg())

		if row != nil || ok {
			t.Errorf("(%v, %v), want (nil, false)", row, ok)
		}
	})
}

func TestNewWindDownHook(t *testing.T) {
	t.Run("the wired refetch reads desired_state off my own member row", func(t *testing.T) {
		client := &hookHTTP{status: 200, body: `{"id":"m-1","desired_state":"offline"}`}
		var out strings.Builder

		h := newWindDownHook(client, hookCfg(), &out)
		state, ok := h.fetchDesired()

		if state != "offline" || !ok {
			t.Errorf("fetchDesired() = (%q, %v), want (\"offline\", true)", state, ok)
		}
		wantCalls := []string{"GET http://station/api/members/m-1 auth=Bearer tok"}
		if !reflect.DeepEqual(client.calls, wantCalls) {
			t.Errorf("calls = %v, want %v", client.calls, wantCalls)
		}
		if h.started || h.lastNotice != "" {
			t.Errorf("a fresh hook has already spoken (started=%v lastNotice=%q)", h.started, h.lastNotice)
		}
		h.say("hello")
		if out.String() != "[ocagent] hello\n" {
			t.Errorf("the hook wrote to %q, want the writer it was handed", out.String())
		}
	})

	t.Run("a row with no desired_state is not a positive read", func(t *testing.T) {
		client := &hookHTTP{status: 200, body: `{"id":"m-1"}`}

		state, ok := newWindDownHook(client, hookCfg(), io.Discard).fetchDesired()

		if state != "" || ok {
			t.Errorf("fetchDesired() = (%q, %v), want (\"\", false)", state, ok)
		}
	})

	t.Run("an unreadable row is not a positive read", func(t *testing.T) {
		client := &hookHTTP{status: 500, body: ``}

		state, ok := newWindDownHook(client, hookCfg(), io.Discard).fetchDesired()

		if state != "" || ok {
			t.Errorf("fetchDesired() = (%q, %v), want (\"\", false)", state, ok)
		}
	})
}

func TestMaybeWindDown(t *testing.T) {
	newHook := func(out io.Writer, state string, ok bool, calls *int) *windDownHook {
		return &windDownHook{
			cfg: hookCfg(),
			out: out,
			fetchDesired: func() (string, bool) {
				*calls++
				return state, ok
			},
		}
	}

	t.Run("a delta naming me on an offline row wakes the session with the pushed notice", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, desiredOffline, true, &calls)

		woke := h.maybeWindDown(memberFrame("kyle::m-1", "第一步：交接。\n第二步：報停。"))

		if !woke {
			t.Error("maybeWindDown = false, want true")
		}
		want := "[ocagent] offboard: 第一步：交接。\n[ocagent] offboard: 第二步：報停。\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if calls != 1 {
			t.Errorf("refetched %d times, want exactly 1", calls)
		}
	})

	t.Run("a delta naming someone else never even refetches", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, desiredOffline, true, &calls)

		woke := h.maybeWindDown(memberFrame("kyle::m-2", "收尾清單"))

		if woke {
			t.Error("maybeWindDown = true, want false")
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
		if calls != 0 {
			t.Errorf("refetched %d times, want 0 — the nudge did not name me", calls)
		}
	})

	t.Run("a refetch that failed is never acted on", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, "", false, &calls)

		woke := h.maybeWindDown(memberFrame("kyle::m-1", "收尾清單"))

		if woke || out.String() != "" {
			t.Errorf("woke=%v printed %q, want no wake and no line", woke, out.String())
		}
		if h.started {
			t.Error("started = true — a failed read must not arm the de-dupe")
		}
	})

	t.Run("an online row is my row changing for some other reason", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, desiredOnline, true, &calls)

		woke := h.maybeWindDown(memberFrame("kyle::m-1", "收尾清單"))

		if woke || out.String() != "" {
			t.Errorf("woke=%v printed %q, want no wake and no line", woke, out.String())
		}
	})

	t.Run("the same sentence arriving again is not repeated", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, desiredOffline, true, &calls)
		frame := memberFrame("kyle::m-1", "收尾清單")

		first := h.maybeWindDown(frame)
		second := h.maybeWindDown(frame)
		third := h.maybeWindDown(frame)

		if !first || second || third {
			t.Errorf("wakes = (%v, %v, %v), want (true, false, false)", first, second, third)
		}
		if out.String() != "[ocagent] offboard: 收尾清單\n" {
			t.Errorf("printed %q, want the notice exactly once", out.String())
		}
	})

	t.Run("the final call is a new sentence and gets through", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, desiredOffline, true, &calls)

		soft := h.maybeWindDown(memberFrame("kyle::m-1", "收尾清單"))
		final := h.maybeWindDown(memberFrame("kyle::m-1", "收尾清單，期限 2026-09-08T10:00:00Z"))

		if !soft || !final {
			t.Errorf("wakes = (%v, %v), want (true, true)", soft, final)
		}
		want := "[ocagent] offboard: 收尾清單\n" +
			"[ocagent] offboard: 收尾清單，期限 2026-09-08T10:00:00Z\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a delta that lost the notice still tells the agent it is being collected", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, desiredOffline, true, &calls)

		woke := h.maybeWindDown(memberFrame("kyle::m-1", ""))

		if !woke {
			t.Error("maybeWindDown = false, want true")
		}
		want := "[ocagent] offboard: server 要收你了，但這則通知沒有帶到〈停止〉 —— " +
			"請立刻用 MCP get_offboard 拿完整收尾清單並照做，別空手停下。\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})
}

func TestWake(t *testing.T) {
	t.Run("every non-blank line of the notice is printed under the offboard badge", func(t *testing.T) {
		var out strings.Builder
		h := &windDownHook{cfg: hookCfg(), out: &out}

		h.wake("〈停止〉\n\n1. 交接\n   \n2. 報停")

		want := "[ocagent] offboard: 〈停止〉\n" +
			"[ocagent] offboard: 1. 交接\n" +
			"[ocagent] offboard: 2. 報停\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a blank notice is replaced by the shared fallback under this hook's own badge", func(t *testing.T) {
		var out strings.Builder
		h := &windDownHook{cfg: hookCfg(), out: &out}

		h.wake("   \n\t\n")

		want := "[ocagent] offboard: server 要收你了，但這則通知沒有帶到〈停止〉 —— " +
			"請立刻用 MCP get_offboard 拿完整收尾清單並照做，別空手停下。\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})
}

func TestNewRecycleHook(t *testing.T) {
	t.Run("the wired refetch hands back my whole member row", func(t *testing.T) {
		client := &hookHTTP{status: 200, body: `{"id":"m-1","desired_state":"online","refocus_since":1787148244}`}
		var out strings.Builder

		h := newRecycleHook(client, hookCfg(), &out)
		row, ok := h.fetchMember()

		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := map[string]any{"id": "m-1", "desired_state": "online", "refocus_since": float64(1787148244)}
		if !reflect.DeepEqual(row, want) {
			t.Errorf("row = %v, want %v", row, want)
		}
		wantCalls := []string{"GET http://station/api/members/m-1 auth=Bearer tok"}
		if !reflect.DeepEqual(client.calls, wantCalls) {
			t.Errorf("calls = %v, want %v", client.calls, wantCalls)
		}
		if h.handledRefocus != 0 || h.lastNotice != "" {
			t.Errorf("a fresh hook has already woken (epoch=%v lastNotice=%q)", h.handledRefocus, h.lastNotice)
		}
		h.say("hello")
		if out.String() != "[ocagent] hello\n" {
			t.Errorf("the hook wrote to %q, want the writer it was handed", out.String())
		}
	})

	t.Run("an unreadable row is not a positive read", func(t *testing.T) {
		client := &hookHTTP{status: 500, body: ``}

		row, ok := newRecycleHook(client, hookCfg(), io.Discard).fetchMember()

		if row != nil || ok {
			t.Errorf("(%v, %v), want (nil, false)", row, ok)
		}
	})
}

func TestOffboardNoticeIn(t *testing.T) {
	cases := []struct {
		name  string
		frame map[string]any
		want  string
	}{
		{
			"the notice the server pushed in this delta",
			map[string]any{"data": map[string]any{"payload": map[string]any{"offboard_notice": "〈停止〉"}}},
			"〈停止〉",
		},
		{
			"a payload that carries no notice",
			map[string]any{"data": map[string]any{"payload": map[string]any{"id": "m-1"}}},
			"",
		},
		{
			"a notice that is not a string",
			map[string]any{"data": map[string]any{"payload": map[string]any{"offboard_notice": 7.0}}},
			"",
		},
		{
			"no payload object",
			map[string]any{"data": map[string]any{"key": "kyle::m-1"}},
			"",
		},
		{"no data object", map[string]any{"topic": "member"}, ""},
		{"an empty frame", map[string]any{}, ""},
		{"a nil frame", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := offboardNoticeIn(c.frame); got != c.want {
				t.Errorf("offboardNoticeIn(%v) = %q, want %q", c.frame, got, c.want)
			}
		})
	}
}

func TestWakeForRecycle(t *testing.T) {
	t.Run("every non-blank line of the notice is printed under the recycle badge", func(t *testing.T) {
		var out strings.Builder
		h := &recycleHook{cfg: hookCfg(), out: &out}

		h.wakeForRecycle("〈停止〉\n\n1. 交接\n   \n2. 報停")

		want := "[ocagent] recycle: 〈停止〉\n" +
			"[ocagent] recycle: 1. 交接\n" +
			"[ocagent] recycle: 2. 報停\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a blank notice is replaced by the shared fallback under this hook's own badge", func(t *testing.T) {
		var out strings.Builder
		h := &recycleHook{cfg: hookCfg(), out: &out}

		h.wakeForRecycle("")

		want := "[ocagent] recycle: server 要收你了，但這則通知沒有帶到〈停止〉 —— " +
			"請立刻用 MCP get_offboard 拿完整收尾清單並照做，別空手停下。\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})
}

func TestMaybeRecycle(t *testing.T) {
	newHook := func(out io.Writer, row map[string]any, ok bool, calls *int) *recycleHook {
		return &recycleHook{
			cfg: hookCfg(),
			out: out,
			fetchMember: func() (map[string]any, bool) {
				*calls++
				return row, ok
			},
		}
	}
	online := func(refocus float64) map[string]any {
		return map[string]any{"desired_state": desiredOnline, "refocus_since": refocus}
	}

	t.Run("an online row carrying a refocus marker wakes the session with the pushed notice", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, online(1787148244), true, &calls)

		woke := h.maybeRecycle(memberFrame("kyle::m-1", "〈停止〉\n1. 交接"))

		if !woke {
			t.Error("maybeRecycle = false, want true")
		}
		want := "[ocagent] recycle: 〈停止〉\n[ocagent] recycle: 1. 交接\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if calls != 1 {
			t.Errorf("refetched %d times, want exactly 1", calls)
		}
	})

	t.Run("a delta naming someone else never even refetches", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, online(1787148244), true, &calls)

		woke := h.maybeRecycle(memberFrame("kyle::m-2", "〈停止〉"))

		if woke || out.String() != "" || calls != 0 {
			t.Errorf("woke=%v printed %q refetched %d, want no wake, no line, no fetch",
				woke, out.String(), calls)
		}
	})

	t.Run("a refetch that failed is never acted on", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, nil, false, &calls)

		woke := h.maybeRecycle(memberFrame("kyle::m-1", "〈停止〉"))

		if woke || out.String() != "" {
			t.Errorf("woke=%v printed %q, want no wake and no line", woke, out.String())
		}
		if h.handledRefocus != 0 {
			t.Errorf("handledRefocus = %v, want 0 — a failed read must not spend an epoch", h.handledRefocus)
		}
	})

	t.Run("an offline row belongs to wind-down, not to recycle", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, map[string]any{
			"desired_state": desiredOffline, "refocus_since": 1787148244.0,
		}, true, &calls)

		woke := h.maybeRecycle(memberFrame("kyle::m-1", "〈停止〉"))

		if woke || out.String() != "" {
			t.Errorf("woke=%v printed %q, want no wake and no line", woke, out.String())
		}
	})

	t.Run("an online row with no refocus marker has nothing to recycle", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, map[string]any{"desired_state": desiredOnline}, true, &calls)

		woke := h.maybeRecycle(memberFrame("kyle::m-1", "〈停止〉"))

		if woke || out.String() != "" {
			t.Errorf("woke=%v printed %q, want no wake and no line", woke, out.String())
		}
	})

	t.Run("a cleared refocus marker has nothing to recycle", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, online(0), true, &calls)

		woke := h.maybeRecycle(memberFrame("kyle::m-1", "〈停止〉"))

		if woke || out.String() != "" {
			t.Errorf("woke=%v printed %q, want no wake and no line", woke, out.String())
		}
	})

	t.Run("the follow-up deltas of the same epoch and sentence are silent", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, online(1787148244), true, &calls)
		frame := memberFrame("kyle::m-1", "〈停止〉")

		first := h.maybeRecycle(frame)
		second := h.maybeRecycle(frame)
		third := h.maybeRecycle(frame)

		if !first || second || third {
			t.Errorf("wakes = (%v, %v, %v), want (true, false, false)", first, second, third)
		}
		if out.String() != "[ocagent] recycle: 〈停止〉\n" {
			t.Errorf("printed %q, want the notice exactly once", out.String())
		}
	})

	t.Run("a new sentence on the same epoch gets through", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, online(1787148244), true, &calls)

		first := h.maybeRecycle(memberFrame("kyle::m-1", "〈停止〉"))
		second := h.maybeRecycle(memberFrame("kyle::m-1", "〈停止〉，最後通牒"))

		if !first || !second {
			t.Errorf("wakes = (%v, %v), want (true, true)", first, second)
		}
		want := "[ocagent] recycle: 〈停止〉\n[ocagent] recycle: 〈停止〉，最後通牒\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("a newer refocus epoch re-arms the wake for the same sentence", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		row := online(1787148244)
		h := newHook(&out, row, true, &calls)
		frame := memberFrame("kyle::m-1", "〈停止〉")

		first := h.maybeRecycle(frame)
		row["refocus_since"] = 1787148999.0
		second := h.maybeRecycle(frame)

		if !first || !second {
			t.Errorf("wakes = (%v, %v), want (true, true)", first, second)
		}
		want := "[ocagent] recycle: 〈停止〉\n[ocagent] recycle: 〈停止〉\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if h.handledRefocus != 1787148999 {
			t.Errorf("handledRefocus = %v, want 1787148999", h.handledRefocus)
		}
	})

	t.Run("a delta that lost the notice spends the epoch on the fallback", func(t *testing.T) {
		var out strings.Builder
		calls := 0
		h := newHook(&out, online(1787148244), true, &calls)
		frame := memberFrame("kyle::m-1", "")

		first := h.maybeRecycle(frame)
		second := h.maybeRecycle(frame)

		if !first || second {
			t.Errorf("wakes = (%v, %v), want (true, false)", first, second)
		}
		want := "[ocagent] recycle: server 要收你了，但這則通知沒有帶到〈停止〉 —— " +
			"請立刻用 MCP get_offboard 拿完整收尾清單並照做，別空手停下。\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})
}

// TestSay is deliberately not written. Both hooks' `say` is a single
// fmt.Fprintf that prefixes agentLinePrefix; the only assertion the rules
// permit restates that one line, and the prefix it guards is already pinned
// verbatim by every wake/maybeWindDown/maybeRecycle expectation above.
func TestSay(t *testing.T) {
	t.Skip("a one-line Fprintf wrapper: any assertion would transcribe it, and its output is pinned by the wake tests")
}
