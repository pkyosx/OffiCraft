package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// codexPipe is the App Server's stdin as a test sees it: everything the sidecar
// wrote, verbatim.
type codexPipe struct{ *bytes.Buffer }

func (codexPipe) Close() error { return nil }

// codexTestSession is a sidecar wired to in-memory pipes: `in` collects the
// JSON-RPC bytes it sends the App Server, `pane` the tmux-visible activity log,
// and `acks` the listener verdicts.
type codexTestSession struct {
	*codexSession
	in   *bytes.Buffer
	pane *bytes.Buffer
	acks *bytes.Buffer
}

func newCodexTestSession() *codexTestSession {
	in, pane, acks := &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}
	return &codexTestSession{
		codexSession: &codexSession{
			in: codexPipe{in}, out: pane, ackTo: acks,
			threadID: "th_1", effort: "medium",
		},
		in: in, pane: pane, acks: acks,
	}
}

// sent decodes every JSON-RPC message the sidecar wrote to the App Server.
func (s *codexTestSession) sent(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s.in.String()), "\n") {
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("the sidecar wrote a line that is not JSON: %q (%v)", line, err)
		}
		out = append(out, msg)
	}
	return out
}

// paneLines strips the wall-clock stamp every activity line carries so the text
// itself can be compared literally.
func (s *codexTestSession) paneLines(t *testing.T) []string {
	t.Helper()
	stamp := regexp.MustCompile(`^\d{2}:\d{2}:\d{2} \[codex\] `)
	var out []string
	for _, line := range strings.Split(strings.TrimSuffix(s.pane.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		if !stamp.MatchString(line) {
			t.Fatalf("activity line %q is not stamped `HH:MM:SS [codex] `", line)
		}
		out = append(out, stamp.ReplaceAllString(line, ""))
	}
	return out
}

func TestBuildCodexLaunchCommand(t *testing.T) {
	got := buildCodexLaunchCommand(
		"/usr/local/bin/ocwarden", "/opt/homebrew/bin/codex", "/Users/seth/work/ow-1",
		"/Users/seth/work/ow-1/PERSONA.md", "/Users/seth/work/ow-1/.token",
		"ow-1", "https://officraft.example", "oc-ow-1", "/tmp/oc.sock",
		"gpt-5-codex", "extreme",
		[][2]string{{"OC_ROLE", "builder"}, {"OC_NOTE", "it's fine"}},
		"/Users/seth/work/ow-1/.env",
	)

	want := "cd /Users/seth/work/ow-1; " +
		"[ -f /Users/seth/work/ow-1/.env ] && . /Users/seth/work/ow-1/.env; " +
		`export OC_TOKEN="$(/bin/cat /Users/seth/work/ow-1/.token)" ` +
		"OC_BASE=https://officraft.example OC_ID=ow-1 OC_SESSION=oc-ow-1 " +
		"OC_TMUX_SOCKET=/tmp/oc.sock OC_ROLE=builder OC_NOTE='it'\"'\"'s fine'; " +
		`export PATH=/Users/seth/work/ow-1:"$PATH"; ` +
		"exec /usr/local/bin/ocwarden codex-session " +
		"--codex-bin /opt/homebrew/bin/codex " +
		"--workdir /Users/seth/work/ow-1 " +
		"--persona /Users/seth/work/ow-1/PERSONA.md " +
		"--agent-id ow-1 --model gpt-5-codex --effort medium"
	if got != want {
		t.Errorf("buildCodexLaunchCommand =\n%q\nwant\n%q", got, want)
	}
}

func TestBuildCodexLaunchCommandWithoutRenderedEnv(t *testing.T) {
	got := buildCodexLaunchCommand(
		"/usr/local/bin/ocwarden", "/opt/homebrew/bin/codex", "/w", "/w/P.md", "/w/.token",
		"ow-2", "https://x.test", "oc-ow-2", "/tmp/s.sock", "", "high", nil, "",
	)

	want := "cd /w; " +
		`export OC_TOKEN="$(/bin/cat /w/.token)" OC_BASE=https://x.test OC_ID=ow-2 ` +
		"OC_SESSION=oc-ow-2 OC_TMUX_SOCKET=/tmp/s.sock; " +
		`export PATH=/w:"$PATH"; ` +
		"exec /usr/local/bin/ocwarden codex-session --codex-bin /opt/homebrew/bin/codex " +
		"--workdir /w --persona /w/P.md --agent-id ow-2 --model '' --effort high"
	if got != want {
		t.Errorf("buildCodexLaunchCommand =\n%q\nwant\n%q", got, want)
	}
}

func TestNormalizeCodexEffort(t *testing.T) {
	cases := []struct{ in, want string }{
		{"low", "low"},
		{"high", "high"},
		{"max", "max"},
		{"medium", "medium"},
		{"  high  ", "high"},
		{"", "medium"},
		{"   ", "medium"},
		{"HIGH", "medium"},
		{"extreme", "medium"},
		{"minimal", "medium"},
	}
	for _, tc := range cases {
		if got := normalizeCodexEffort(tc.in); got != tc.want {
			t.Errorf("normalizeCodexEffort(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCodexPersonaInstruction(t *testing.T) {
	const head = "Read /w/PERSONA.md completely before acting. It is your OffiCraft identity and " +
		"operating context. Never use request_user_input for normal questions; create an OffiCraft " +
		"reply card instead. "

	if got, want := codexPersonaInstruction("/w/PERSONA.md", "gpt-5-codex"), head+
		"The explicit OffiCraft launch model is gpt-5-codex. If your role's boot sequence calls "+
		"report_waking, pass that exact value as its model argument. Follow your role-specific "+
		"boot sequence when it says not to call report_waking."; got != want {
		t.Errorf("codexPersonaInstruction with an explicit model =\n%q\nwant\n%q", got, want)
	}

	blank := head + "The OffiCraft launch model setting is blank, so the machine's Codex default " +
		"applies. If your role's boot sequence calls report_waking, omit its optional model " +
		"argument; never guess or persist a model name."
	if got := codexPersonaInstruction("/w/PERSONA.md", ""); got != blank {
		t.Errorf("codexPersonaInstruction with a blank model =\n%q\nwant\n%q", got, blank)
	}
	if got := codexPersonaInstruction("/w/PERSONA.md", "   "); got != blank {
		t.Errorf("codexPersonaInstruction with a whitespace model =\n%q\nwant\n%q", got, blank)
	}
}

// codexIDToken assembles a JWT-shaped id_token around a payload; the signature
// is never verified so any third segment does.
func codexIDToken(payload string) string {
	return "eyJhbGciOiJSUzI1NiJ9." +
		base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".sig"
}

// The digest of "officraft-codex-account-v2:user_abc123".
const codexKeyForUserABC = "codex:0b7f6a11d109c50695fe9b42805f49d0bbeede516fc7864f03554d271ebac76b"

func TestCodexUserIDFromIDToken(t *testing.T) {
	cases := []struct{ name, token, want string }{
		{name: "empty", token: ""},
		{name: "not three segments", token: "a.b"},
		{name: "four segments", token: "a.b.c.d"},
		{name: "payload is not base64url", token: "a.!!!.c"},
		{name: "payload is not json", token: "a." + base64.RawURLEncoding.EncodeToString([]byte("nope")) + ".c"},
		{name: "payload is not an object", token: codexIDToken(`["user_abc123"]`)},
		{name: "the auth claim is missing", token: codexIDToken(`{"sub":"google-oauth2|1","email":"seth@x.test"}`)},
		{name: "the auth claim carries no user id", token: codexIDToken(
			`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct_1"}}`)},
		{name: "a blank user id is no user id", token: codexIDToken(
			`{"https://api.openai.com/auth":{"chatgpt_user_id":"   "}}`)},
		{
			name: "the per-person claim",
			token: codexIDToken(`{"https://api.openai.com/auth":{"chatgpt_user_id":" user_abc123 ",` +
				`"chatgpt_account_id":"acct_shared","user_id":"user_abc123"},"sub":"google-oauth2|1"}`),
			want: "user_abc123",
		},
		{
			name:  "padded base64 is still decoded",
			token: "h." + base64.URLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_user_id":"user_abc123"}}`)) + ".s",
			want:  "user_abc123",
		},
		{
			name:  "surrounding whitespace on the token itself",
			token: "  " + codexIDToken(`{"https://api.openai.com/auth":{"chatgpt_user_id":"user_abc123"}}`) + "\n",
			want:  "user_abc123",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexUserIDFromIDToken(tc.token); got != tc.want {
				t.Errorf("codexUserIDFromIDToken = %q, want %q", got, tc.want)
			}
		})
	}
}

// stageCodexAuth writes home/.codex/auth.json and returns home.
func stageCodexAuth(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatalf("stage .codex: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("stage auth.json: %v", err)
	}
	return home
}

func TestCodexAccountKeyForHome(t *testing.T) {
	valid := `{"tokens":{"id_token":"` +
		codexIDToken(`{"https://api.openai.com/auth":{"chatgpt_user_id":"user_abc123"}}`) +
		`","account_id":"acct_shared"}}`

	t.Run("the same person hashes to the same key on any machine", func(t *testing.T) {
		one := codexAccountKeyForHome(stageCodexAuth(t, valid))
		two := codexAccountKeyForHome(stageCodexAuth(t, valid))
		if one != codexKeyForUserABC || two != codexKeyForUserABC {
			t.Errorf("codexAccountKeyForHome = %q and %q, want %q on both", one, two, codexKeyForUserABC)
		}
	})

	t.Run("a different person hashes to a different key", func(t *testing.T) {
		other := `{"tokens":{"id_token":"` +
			codexIDToken(`{"https://api.openai.com/auth":{"chatgpt_user_id":"user_zzz999"}}`) + `"}}`
		got := codexAccountKeyForHome(stageCodexAuth(t, other))
		want := "codex:d58f2de4c03c791a9ba9b513e84fd283afc887189fd752fdbbbc6c727cea0d33"
		if got != want {
			t.Errorf("codexAccountKeyForHome = %q, want %q", got, want)
		}
	})

	t.Run("every unreadable shape reports no account", func(t *testing.T) {
		cases := []struct{ name, body string }{
			{name: "auth.json is not json", body: "not json"},
			{name: "no tokens object", body: `{"other":1}`},
			{name: "no id_token", body: `{"tokens":{"account_id":"acct_shared"}}`},
			{name: "id_token is not a jwt", body: `{"tokens":{"id_token":"garbage"}}`},
			{name: "the workspace id is never a fallback", body: `{"tokens":{"id_token":"` +
				codexIDToken(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct_shared"}}`) + `"}}`},
		}
		for _, tc := range cases {
			if got := codexAccountKeyForHome(stageCodexAuth(t, tc.body)); got != "" {
				t.Errorf("%s: codexAccountKeyForHome = %q, want empty", tc.name, got)
			}
		}
	})

	t.Run("a home with no auth.json reports no account", func(t *testing.T) {
		if got := codexAccountKeyForHome(t.TempDir()); got != "" {
			t.Errorf("codexAccountKeyForHome = %q, want empty", got)
		}
	})
}

func TestCodexAccountKey(t *testing.T) {
	valid := `{"tokens":{"id_token":"` +
		codexIDToken(`{"https://api.openai.com/auth":{"chatgpt_user_id":"user_abc123"}}`) + `"}}`
	t.Setenv("HOME", stageCodexAuth(t, valid))
	if got := codexAccountKey(); got != codexKeyForUserABC {
		t.Errorf("codexAccountKey = %q, want %q", got, codexKeyForUserABC)
	}

	t.Setenv("HOME", t.TempDir())
	if got := codexAccountKey(); got != "" {
		t.Errorf("codexAccountKey on a machine with no Codex login = %q, want empty", got)
	}
}

func TestCodexSessionActivity(t *testing.T) {
	s := newCodexTestSession()

	s.activity("thread ready · booting agent")
	s.activity("context %.0f%% · compact %d", 41.4, 2)

	want := []string{"thread ready · booting agent", "context 41% · compact 2"}
	if got := s.paneLines(t); !reflect.DeepEqual(got, want) {
		t.Errorf("the pane shows %q, want %q", got, want)
	}

	quiet := &codexSession{}
	quiet.activity("nowhere to write this")
}

func TestCodexSessionSend(t *testing.T) {
	s := newCodexTestSession()

	first := s.send("initialize", map[string]any{"capabilities": map[string]any{"experimentalApi": true}})
	second := s.send("account/rateLimits/read", nil)

	if first != 1 || second != 2 {
		t.Errorf("send returned ids %d then %d, want 1 then 2", first, second)
	}
	want := []map[string]any{
		{"id": float64(1), "method": "initialize",
			"params": map[string]any{"capabilities": map[string]any{"experimentalApi": true}}},
		{"id": float64(2), "method": "account/rateLimits/read", "params": nil},
	}
	if got := s.sent(t); !reflect.DeepEqual(got, want) {
		t.Errorf("the sidecar sent %v, want %v", got, want)
	}
}

func TestCodexSessionNotify(t *testing.T) {
	s := newCodexTestSession()

	s.notify("initialized", map[string]any{})
	id := s.send("initialize", nil)

	if id != 1 {
		t.Errorf("a notification consumed request id %d; send returned %d, want 1", id-1, id)
	}
	want := []map[string]any{
		{"method": "initialized", "params": map[string]any{}},
		{"id": float64(1), "method": "initialize", "params": nil},
	}
	if got := s.sent(t); !reflect.DeepEqual(got, want) {
		t.Errorf("the sidecar sent %v, want %v", got, want)
	}
}

func TestMessageID(t *testing.T) {
	cases := []struct {
		name string
		msg  appServerMessage
		want int
	}{
		{name: "a decoded json number", msg: appServerMessage{"id": float64(7)}, want: 7},
		{name: "a native int", msg: appServerMessage{"id": 7}, want: 7},
		{name: "no id at all", msg: appServerMessage{"method": "turn/started"}},
		{name: "a string id is not a number we track", msg: appServerMessage{"id": "req-7"}},
		{name: "a null id", msg: appServerMessage{"id": nil}},
		{name: "a fractional id truncates", msg: appServerMessage{"id": 7.9}, want: 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := messageID(tc.msg); got != tc.want {
				t.Errorf("messageID(%v) = %d, want %d", tc.msg, got, tc.want)
			}
		})
	}
}

func TestNestedString(t *testing.T) {
	msg := map[string]any{
		"result": map[string]any{
			"thread": map[string]any{"id": "th_42", "count": float64(1)},
			"empty":  map[string]any{},
		},
	}
	cases := []struct {
		name string
		keys []string
		want string
	}{
		{name: "the whole path", keys: []string{"result", "thread", "id"}, want: "th_42"},
		{name: "no keys at all", keys: nil},
		{name: "an absent leaf", keys: []string{"result", "thread", "missing"}},
		{name: "an absent branch", keys: []string{"result", "nothing", "id"}},
		{name: "the leaf is not a string", keys: []string{"result", "thread", "count"}},
		{name: "the path stops at an object", keys: []string{"result", "empty"}},
		{name: "descending through a string", keys: []string{"result", "thread", "id", "more"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nestedString(msg, tc.keys...); got != tc.want {
				t.Errorf("nestedString(%v) = %q, want %q", tc.keys, got, tc.want)
			}
		})
	}
}

func TestCodexSessionWaitResponse(t *testing.T) {
	t.Run("the answer to our own id, past everything else", func(t *testing.T) {
		messages := make(chan appServerMessage, 4)
		messages <- appServerMessage{"method": "turn/started", "params": map[string]any{}}
		messages <- appServerMessage{"id": float64(1), "result": map[string]any{"other": true}}
		messages <- appServerMessage{"id": float64(2), "result": map[string]any{"thread": map[string]any{"id": "th_42"}}}
		s := &codexSession{messages: messages}

		got, err := s.waitResponse(2)

		if err != nil {
			t.Fatalf("waitResponse returned %v, want no error", err)
		}
		want := appServerMessage{"id": float64(2), "result": map[string]any{"thread": map[string]any{"id": "th_42"}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("waitResponse returned %v, want %v", got, want)
		}
		if len(messages) != 0 {
			t.Errorf("%d message(s) left unread, want the stream drained up to the answer", len(messages))
		}
	})

	t.Run("an error response is an error", func(t *testing.T) {
		messages := make(chan appServerMessage, 1)
		messages <- appServerMessage{"id": float64(3), "error": map[string]any{
			"code": float64(-32600), "message": "thread not found"}}
		s := &codexSession{messages: messages}

		got, err := s.waitResponse(3)

		if got != nil {
			t.Errorf("waitResponse returned %v, want nil alongside the error", got)
		}
		if err == nil || err.Error() != "app-server request failed: thread not found" {
			t.Errorf("waitResponse returned error %v, want app-server request failed: thread not found", err)
		}
	})

	t.Run("a closed stream is an error", func(t *testing.T) {
		messages := make(chan appServerMessage)
		close(messages)
		s := &codexSession{messages: messages}

		got, err := s.waitResponse(1)

		if got != nil {
			t.Errorf("waitResponse returned %v, want nil alongside the error", got)
		}
		if err == nil || err.Error() != "app-server exited before responding" {
			t.Errorf("waitResponse returned error %v, want app-server exited before responding", err)
		}
	})
}

func TestCodexAppReader(t *testing.T) {
	stream := strings.Join([]string{
		`{"id":1,"result":{"ok":true}}`,
		`not json at all`,
		`123`,
		`{"method":"turn/started","params":{"turn":{"id":"t_1"}}}`,
		``,
		`{"method":"turn/completed"}`,
	}, "\n") + "\n"

	messages := codexAppReader(strings.NewReader(stream))

	var got []appServerMessage
	for msg := range messages {
		got = append(got, msg)
	}
	want := []appServerMessage{
		{"id": float64(1), "result": map[string]any{"ok": true}},
		{"method": "turn/started", "params": map[string]any{"turn": map[string]any{"id": "t_1"}}},
		{"method": "turn/completed"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("codexAppReader produced %v, want %v", got, want)
	}
}

func TestCodexDeliveryLabel(t *testing.T) {
	long := strings.Repeat("あ", 100)
	cases := []struct{ name, in, want string }{
		{name: "a short line is passed through", in: "  hello  ", want: "hello"},
		{name: "only the first line", in: "first line\nsecond line\nthird", want: "first line"},
		{name: "empty stays empty", in: "   \n  ", want: ""},
		{name: "exactly 80 runes is not truncated", in: strings.Repeat("b", 80), want: strings.Repeat("b", 80)},
		{name: "81 runes is truncated with an ellipsis", in: strings.Repeat("b", 81), want: strings.Repeat("b", 80) + "…"},
		{name: "truncation counts runes, not bytes", in: long, want: strings.Repeat("あ", 80) + "…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexDeliveryLabel(tc.in); got != tc.want {
				t.Errorf("codexDeliveryLabel(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCodexBatchToken(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		token string
		ok    bool
	}{
		{name: "the marker", line: "[ocagent] listen: batch b-17", token: "b-17", ok: true},
		{name: "the listener's trailing stamp is not part of the token",
			line: "[ocagent] listen: batch b-17 [ts=2026-09-07T12:00:00 local]", token: "b-17", ok: true},
		{name: "leading whitespace", line: "   [ocagent] listen: batch b-17\t", token: "b-17", ok: true},
		{name: "a marker with no token", line: "[ocagent] listen: batch "},
		{name: "a marker with only whitespace after it", line: "[ocagent] listen: batch    "},
		{name: "another transport line", line: "[ocagent] listen: connected"},
		{name: "an ordinary chat line", line: "batch b-17"},
		{name: "empty", line: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, ok := codexBatchToken(tc.line)
			if token != tc.token || ok != tc.ok {
				t.Errorf("codexBatchToken(%q) = %q, %v, want %q, %v", tc.line, token, ok, tc.token, tc.ok)
			}
		})
	}
}

func TestActionableCodexListenerLine(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{name: "an OffiCraft chat event", line: "[chat] owner: 早安", want: true},
		{name: "an empty line", line: "", want: true},
		{name: "the first failure of an outage",
			line: "[ocagent] listen: disconnected — stream ended: EOF", want: true},
		{name: "back up again", line: "[ocagent] listen: connected (station oc-1)", want: true},
		{name: "the retry loop really stopped",
			line: "[ocagent] listen: giving up after 40 attempts", want: true},
		{name: "a notice with leading whitespace", line: "   [ocagent] listen: connected", want: true},
		{name: "mid-outage retry chatter", line: "[ocagent] listen: retry 3 in 8s"},
		{name: "the batch marker is protocol, not transcript", line: "[ocagent] listen: batch b-17"},
		{name: "any other transport diagnostic", line: "[ocagent] listen: stream ended: EOF"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := actionableCodexListenerLine(tc.line); got != tc.want {
				t.Errorf("actionableCodexListenerLine(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestCodexListenerActions(t *testing.T) {
	cases := []struct {
		name          string
		line          string
		alreadySent   bool
		wake, forward bool
	}{
		{name: "the boot connect wakes and is not forwarded",
			line: "[ocagent] listen: connected", wake: true},
		{name: "a later connect is forwarded and wakes nothing",
			line: "[ocagent] listen: connected", alreadySent: true, forward: true},
		{name: "a disconnect is forwarded", line: "[ocagent] listen: disconnected — EOF", forward: true},
		{name: "giving up is forwarded", line: "[ocagent] listen: giving up", forward: true},
		{name: "retry chatter does neither", line: "[ocagent] listen: retry 3 in 8s"},
		{name: "an OffiCraft event is forwarded", line: "[chat] owner: 早安", forward: true},
		{name: "an OffiCraft event never wakes, even before the boot connect",
			line: "[task] ow-1 assigned", forward: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wake, forward := codexListenerActions(tc.line, tc.alreadySent)
			if wake != tc.wake || forward != tc.forward {
				t.Errorf("codexListenerActions(%q, %v) = wake %v, forward %v, want wake %v, forward %v",
					tc.line, tc.alreadySent, wake, forward, tc.wake, tc.forward)
			}
		})
	}
}

func TestRateLimitSnapshot(t *testing.T) {
	nested := map[string]any{"primary": map[string]any{"usedPercent": float64(1)}}
	cases := []struct {
		name   string
		result map[string]any
		want   map[string]any
	}{
		{name: "the notification's nested form",
			result: map[string]any{"rateLimits": nested, "other": true}, want: nested},
		{name: "the flat form some versions answer with", result: nested, want: nested},
		{name: "a null rateLimits falls back to the result itself",
			result: map[string]any{"rateLimits": nil}, want: map[string]any{"rateLimits": nil}},
		{name: "an empty result", result: map[string]any{}, want: map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rateLimitSnapshot(tc.result); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rateLimitSnapshot(%v) = %v, want %v", tc.result, got, tc.want)
			}
		})
	}
}

func TestCodexOpenYourOwnCardMessage(t *testing.T) {
	const refusal = "OffiCraft does not open reply cards on your behalf. Open it yourself with the " +
		"create_reply_card tool, then end this turn and wait for its SSE answer event. " +
		"linked_task is required: send {\"task_id\": ..., \"step_id\": ...} for the step this " +
		"question is about, or null if it is not about a task."

	cases := []struct {
		name     string
		question map[string]any
		want     string
	}{
		{name: "an ordinary question", question: map[string]any{"id": "q1"}, want: refusal},
		{name: "isSecret false", question: map[string]any{"isSecret": false}, want: refusal},
		{name: "isSecret is not a bool", question: map[string]any{"isSecret": "yes"}, want: refusal},
		{
			name:     "a credential ask carries the do-not-paste warning",
			question: map[string]any{"id": "q1", "isSecret": true},
			want:     refusal + " 這是秘密資料請求；請只完成所需動作，不要把秘密貼進卡片。",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexOpenYourOwnCardMessage(tc.question); got != tc.want {
				t.Errorf("codexOpenYourOwnCardMessage(%v) =\n%q\nwant\n%q", tc.question, got, tc.want)
			}
		})
	}
}

func TestCodexSessionStartTurn(t *testing.T) {
	s := newCodexTestSession()
	s.effort = "high"
	batch := &codexBatch{token: "b-1"}

	s.startTurn("開始。", batch)

	want := []map[string]any{{
		"id": float64(1), "method": "turn/start",
		"params": map[string]any{
			"threadId": "th_1", "effort": "high",
			"input": []any{map[string]any{"type": "text", "text": "開始。"}},
		},
	}}
	if got := s.sent(t); !reflect.DeepEqual(got, want) {
		t.Errorf("startTurn sent %v, want %v", got, want)
	}
	if got, want := s.paneLines(t), []string{"turn started"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the pane shows %q, want %q", got, want)
	}
	if d := s.pending[1]; d == nil || d.method != "turn/start" || d.text != "開始。" || d.batch != batch {
		t.Errorf("startTurn registered %+v as request 1, want a turn/start carrying the text and batch", d)
	}
	if batch.outstanding != 1 {
		t.Errorf("the batch counts %d outstanding deliveries, want 1", batch.outstanding)
	}
	if s.acks.Len() != 0 {
		t.Errorf("startTurn wrote %q to the listener, want nothing yet", s.acks.String())
	}
}

func TestCodexSessionSteerOrStart(t *testing.T) {
	t.Run("a live turn is steered", func(t *testing.T) {
		s := newCodexTestSession()
		s.active, s.turnID = true, "t_9"
		batch := &codexBatch{}

		s.steerOrStart("  [chat] owner: 早安  ", batch)

		want := []map[string]any{{
			"id": float64(1), "method": "turn/steer",
			"params": map[string]any{
				"threadId": "th_1", "expectedTurnId": "t_9",
				"input": []any{map[string]any{"type": "text", "text": "[chat] owner: 早安"}},
			},
		}}
		if got := s.sent(t); !reflect.DeepEqual(got, want) {
			t.Errorf("steerOrStart sent %v, want %v", got, want)
		}
		if got, want := s.paneLines(t), []string{"turn steered by OffiCraft event"}; !reflect.DeepEqual(got, want) {
			t.Errorf("the pane shows %q, want %q", got, want)
		}
		if d := s.pending[1]; d == nil || d.method != "turn/steer" || d.text != "[chat] owner: 早安" {
			t.Errorf("steerOrStart registered %+v, want a turn/steer carrying the trimmed text", d)
		}
		if batch.outstanding != 1 {
			t.Errorf("the batch counts %d outstanding deliveries, want 1", batch.outstanding)
		}
	})

	t.Run("an idle session opens a fresh turn instead", func(t *testing.T) {
		s := newCodexTestSession()
		s.active, s.turnID = false, "t_9"

		s.steerOrStart("[chat] owner: 早安", nil)

		got := s.sent(t)
		if len(got) != 1 || got[0]["method"] != "turn/start" {
			t.Fatalf("steerOrStart sent %v, want a single turn/start", got)
		}
		if got, want := s.paneLines(t), []string{"turn started"}; !reflect.DeepEqual(got, want) {
			t.Errorf("the pane shows %q, want %q", got, want)
		}
	})

	t.Run("an active turn with no id opens a fresh turn", func(t *testing.T) {
		s := newCodexTestSession()
		s.active, s.turnID = true, ""

		s.steerOrStart("[chat] owner: 早安", nil)

		got := s.sent(t)
		if len(got) != 1 || got[0]["method"] != "turn/start" {
			t.Fatalf("steerOrStart sent %v, want a single turn/start", got)
		}
	})

	t.Run("blank text is not a delivery", func(t *testing.T) {
		s := newCodexTestSession()
		s.active, s.turnID = true, "t_9"
		batch := &codexBatch{}

		s.steerOrStart("   \n\t ", batch)

		if s.in.Len() != 0 || s.pane.Len() != 0 {
			t.Errorf("blank text sent %q and logged %q, want neither", s.in.String(), s.pane.String())
		}
		if len(s.pending) != 0 || batch.outstanding != 0 {
			t.Errorf("blank text registered %d pending deliveries (batch %d), want 0",
				len(s.pending), batch.outstanding)
		}
	})
}

func TestCodexSessionTrack(t *testing.T) {
	s := newCodexTestSession()
	batch := &codexBatch{}
	steer := &codexDelivery{method: "turn/steer", text: "hi", batch: batch}

	s.track(0, &codexDelivery{method: "turn/start", text: "dropped", batch: batch})
	s.track(4, steer)

	want := map[int]*codexDelivery{4: steer}
	if !reflect.DeepEqual(s.pending, want) {
		t.Errorf("pending = %v, want only request 4", s.pending)
	}
	if batch.outstanding != 1 {
		t.Errorf("the batch counts %d outstanding deliveries, want 1 (the unsent request must not count)",
			batch.outstanding)
	}
}

func TestCodexSessionResolveResponse(t *testing.T) {
	t.Run("a delivered turn acks its closed batch", func(t *testing.T) {
		s := newCodexTestSession()
		batch := s.currentBatch()
		s.startTurn("[chat] owner: 早安", batch)
		s.closeBatch("b-1")
		s.pane.Reset()

		s.resolveResponse(1, appServerMessage{"id": float64(1), "result": map[string]any{}})

		if got, want := s.paneLines(t), []string{"已送進對話：[chat] owner: 早安"}; !reflect.DeepEqual(got, want) {
			t.Errorf("the pane shows %q, want %q", got, want)
		}
		if got, want := s.acks.String(), "ack b-1\n"; got != want {
			t.Errorf("the listener was told %q, want %q", got, want)
		}
		if len(s.pending) != 0 {
			t.Errorf("%d delivery/deliveries still pending, want 0", len(s.pending))
		}
	})

	t.Run("a refused turn/steer is re-sent as a fresh turn", func(t *testing.T) {
		s := newCodexTestSession()
		s.active, s.turnID = true, "t_9"
		batch := s.currentBatch()
		s.steerOrStart("[chat] owner: 早安", batch)
		s.closeBatch("b-1")
		s.in.Reset()
		s.pane.Reset()

		s.resolveResponse(1, appServerMessage{"id": float64(1),
			"error": map[string]any{"message": "expectedTurnId is stale"}})

		want := []map[string]any{{
			"id": float64(2), "method": "turn/start",
			"params": map[string]any{
				"threadId": "th_1", "effort": "medium",
				"input": []any{map[string]any{"type": "text", "text": "[chat] owner: 早安"}},
			},
		}}
		if got := s.sent(t); !reflect.DeepEqual(got, want) {
			t.Errorf("the retry sent %v, want %v", got, want)
		}
		wantPane := []string{
			"turn/steer 被拒（expectedTurnId is stale）— 改開新的一輪重送同一段內容",
			"turn started",
		}
		if got := s.paneLines(t); !reflect.DeepEqual(got, wantPane) {
			t.Errorf("the pane shows %q, want %q", got, wantPane)
		}
		if s.acks.Len() != 0 {
			t.Errorf("the listener was told %q while the retry is still in flight, want nothing",
				s.acks.String())
		}
		if batch.outstanding != 1 || batch.failed {
			t.Errorf("the batch is %+v, want one outstanding delivery and no failure yet", *batch)
		}

		s.resolveResponse(2, appServerMessage{"id": float64(2), "result": map[string]any{}})
		if got, want := s.acks.String(), "ack b-1\n"; got != want {
			t.Errorf("the listener was told %q, want %q", got, want)
		}
	})

	t.Run("a refused turn/start nacks the whole batch", func(t *testing.T) {
		s := newCodexTestSession()
		batch := s.currentBatch()
		s.startTurn(strings.Repeat("あ", 100), batch)
		s.closeBatch("b-1")
		s.in.Reset()
		s.pane.Reset()

		s.resolveResponse(1, appServerMessage{"id": float64(1),
			"error": map[string]any{"message": "thread is busy"}})

		if s.in.Len() != 0 {
			t.Errorf("a refused turn/start re-sent %q, want no retry", s.in.String())
		}
		wantPane := []string{
			"⚠️ 送不進去（thread is busy）：" + strings.Repeat("あ", 80) + "… — 這段內容沒有進到 agent 的對話，" +
				"agent 不會知道有人說過這句話",
			"批次 b-1 沒能送進對話 — 已告訴 listener 不要標已讀，下一輪會重印",
		}
		if got := s.paneLines(t); !reflect.DeepEqual(got, wantPane) {
			t.Errorf("the pane shows %q, want %q", got, wantPane)
		}
		if got, want := s.acks.String(), "nack b-1\n"; got != want {
			t.Errorf("the listener was told %q, want %q", got, want)
		}
	})

	t.Run("one failure nacks a group that also delivered", func(t *testing.T) {
		s := newCodexTestSession()
		batch := s.currentBatch()
		s.startTurn("first", batch)
		s.startTurn("second", batch)
		s.closeBatch("b-2")

		s.resolveResponse(1, appServerMessage{"id": float64(1), "result": map[string]any{}})
		if s.acks.Len() != 0 {
			t.Errorf("the listener was told %q with a delivery still open, want nothing", s.acks.String())
		}
		s.resolveResponse(2, appServerMessage{"id": float64(2),
			"error": map[string]any{"message": "thread is busy"}})

		if got, want := s.acks.String(), "nack b-2\n"; got != want {
			t.Errorf("the listener was told %q, want %q", got, want)
		}
	})

	t.Run("a response to an id nobody tracks is skipped", func(t *testing.T) {
		s := newCodexTestSession()
		batch := s.currentBatch()
		s.startTurn("[chat] owner: 早安", batch)
		s.closeBatch("b-1")
		s.pane.Reset()

		s.resolveResponse(99, appServerMessage{"id": float64(99), "result": map[string]any{}})
		s.resolveResponse(99, appServerMessage{"id": float64(99),
			"error": map[string]any{"message": "boom"}})

		if s.pane.Len() != 0 || s.acks.Len() != 0 {
			t.Errorf("an untracked response logged %q and told the listener %q, want neither",
				s.pane.String(), s.acks.String())
		}
		if len(s.pending) != 1 || batch.outstanding != 1 {
			t.Errorf("pending=%d batch.outstanding=%d, want 1 and 1", len(s.pending), batch.outstanding)
		}
	})
}

func TestCodexSessionConfirmStartedTurn(t *testing.T) {
	t.Run("the oldest unanswered turn/start is the one that started", func(t *testing.T) {
		s := newCodexTestSession()
		batch := s.currentBatch()
		s.active, s.turnID = true, "t_9"
		s.steerOrStart("steered", batch)
		s.active = false
		s.startTurn("older", batch)
		s.startTurn("newer", batch)
		s.closeBatch("b-1")
		s.pane.Reset()

		s.confirmStartedTurn()

		if got, want := s.paneLines(t), []string{"已送進對話：older"}; !reflect.DeepEqual(got, want) {
			t.Errorf("the pane shows %q, want %q", got, want)
		}
		if _, still := s.pending[2]; still {
			t.Error("the confirmed turn/start is still pending")
		}
		if len(s.pending) != 2 || s.pending[1].method != "turn/steer" || s.pending[3].text != "newer" {
			t.Errorf("pending = %v, want the turn/steer and the newer turn/start left", s.pending)
		}
		if batch.outstanding != 2 {
			t.Errorf("the batch counts %d outstanding deliveries, want 2", batch.outstanding)
		}
		if s.acks.Len() != 0 {
			t.Errorf("the listener was told %q with deliveries still open, want nothing", s.acks.String())
		}
	})

	t.Run("nothing pending is a no-op", func(t *testing.T) {
		s := newCodexTestSession()

		s.confirmStartedTurn()

		if s.pane.Len() != 0 || s.acks.Len() != 0 {
			t.Errorf("confirmStartedTurn logged %q and told the listener %q, want neither",
				s.pane.String(), s.acks.String())
		}
	})

	t.Run("a turn/started never confirms a turn/steer", func(t *testing.T) {
		s := newCodexTestSession()
		s.active, s.turnID = true, "t_9"
		s.steerOrStart("steered", nil)
		s.pane.Reset()

		s.confirmStartedTurn()

		if s.pane.Len() != 0 {
			t.Errorf("confirmStartedTurn logged %q, want nothing", s.pane.String())
		}
		if len(s.pending) != 1 {
			t.Errorf("pending = %v, want the turn/steer left alone", s.pending)
		}
	})
}

func TestCodexSessionCloseBatch(t *testing.T) {
	t.Run("a marker that closed an empty window is acked at once", func(t *testing.T) {
		s := newCodexTestSession()

		s.closeBatch("b-1")

		if got, want := s.acks.String(), "ack b-1\n"; got != want {
			t.Errorf("the listener was told %q, want %q", got, want)
		}
		if s.batch != nil {
			t.Errorf("the closed batch is still current: %+v", *s.batch)
		}
	})

	t.Run("a marker with a delivery still in flight waits", func(t *testing.T) {
		s := newCodexTestSession()
		s.startTurn("[chat] owner: 早安", s.currentBatch())

		s.closeBatch("b-1")

		if s.acks.Len() != 0 {
			t.Errorf("the listener was told %q before the delivery landed, want nothing", s.acks.String())
		}
		if s.batch != nil {
			t.Errorf("the closed batch is still current: %+v", *s.batch)
		}

		s.resolveResponse(1, appServerMessage{"id": float64(1), "result": map[string]any{}})
		if got, want := s.acks.String(), "ack b-1\n"; got != want {
			t.Errorf("the listener was told %q, want %q", got, want)
		}
	})

	t.Run("the next window is a new group", func(t *testing.T) {
		s := newCodexTestSession()
		first := s.currentBatch()
		s.closeBatch("b-1")
		second := s.currentBatch()

		if first == second {
			t.Error("the second window joined the already-closed batch")
		}
		s.startTurn("later", second)
		s.closeBatch("b-2")
		s.resolveResponse(1, appServerMessage{"id": float64(1), "result": map[string]any{}})
		if got, want := s.acks.String(), "ack b-1\nack b-2\n"; got != want {
			t.Errorf("the listener was told %q, want %q", got, want)
		}
	})
}

func TestCodexSessionSettleBatch(t *testing.T) {
	t.Run("only a closed, quiet, unanswered group produces a verdict", func(t *testing.T) {
		cases := []struct {
			name  string
			group *codexBatch
			want  string
		}{
			{name: "no group at all"},
			{name: "still open", group: &codexBatch{token: "b-1"}},
			{name: "still waiting on a delivery",
				group: &codexBatch{token: "b-1", closed: true, outstanding: 1}},
			{name: "already answered",
				group: &codexBatch{token: "b-1", closed: true, answered: true}},
			{name: "closed and quiet", group: &codexBatch{token: "b-1", closed: true}, want: "ack b-1\n"},
			{name: "closed, quiet and failed",
				group: &codexBatch{token: "b-1", closed: true, failed: true}, want: "nack b-1\n"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				s := newCodexTestSession()

				s.settleBatch(tc.group)

				if got := s.acks.String(); got != tc.want {
					t.Errorf("the listener was told %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("the verdict is written once", func(t *testing.T) {
		s := newCodexTestSession()
		group := &codexBatch{token: "b-1", closed: true}

		s.settleBatch(group)
		s.settleBatch(group)

		if got, want := s.acks.String(), "ack b-1\n"; got != want {
			t.Errorf("the listener was told %q, want %q", got, want)
		}
	})

	t.Run("with no listener the verdict is dropped, and the group stays answered", func(t *testing.T) {
		s := newCodexTestSession()
		s.ackTo = nil
		group := &codexBatch{token: "b-1", closed: true, failed: true}

		s.settleBatch(group)

		if !group.answered {
			t.Error("the group is not marked answered, so a later settle would write a second verdict")
		}
		wantPane := []string{"批次 b-1 沒能送進對話 — 已告訴 listener 不要標已讀，下一輪會重印"}
		if got := s.paneLines(t); !reflect.DeepEqual(got, wantPane) {
			t.Errorf("the pane shows %q, want %q", got, wantPane)
		}
	})
}

func TestCodexSessionOpenListenerTurn(t *testing.T) {
	t.Run("the post-boot wake announces itself as the wake", func(t *testing.T) {
		s := newCodexTestSession()

		s.openListenerTurn(codexPostBootWake)

		want := []map[string]any{{
			"id": float64(1), "method": "turn/start",
			"params": map[string]any{
				"threadId": "th_1", "effort": "medium",
				"input": []any{map[string]any{"type": "text", "text": codexPostBootWake}},
			},
		}}
		if got := s.sent(t); !reflect.DeepEqual(got, want) {
			t.Errorf("openListenerTurn sent %v, want %v", got, want)
		}
		wantPane := []string{"waking the session now that SSE is up", "turn started"}
		if got := s.paneLines(t); !reflect.DeepEqual(got, wantPane) {
			t.Errorf("the pane shows %q, want %q", got, wantPane)
		}
	})

	t.Run("everything else is announced as an OffiCraft event", func(t *testing.T) {
		s := newCodexTestSession()

		s.openListenerTurn("[chat] owner: 早安")

		wantPane := []string{"OffiCraft event: [chat] owner: 早安", "turn started"}
		if got := s.paneLines(t); !reflect.DeepEqual(got, wantPane) {
			t.Errorf("the pane shows %q, want %q", got, wantPane)
		}
	})

	t.Run("every line since the last marker joins one group", func(t *testing.T) {
		s := newCodexTestSession()

		s.openListenerTurn("[chat] owner: 早安")
		s.openListenerTurn("[task] ow-1 assigned")
		group := s.batch
		s.closeBatch("b-3")

		if group == nil || group.outstanding != 2 {
			t.Fatalf("the group is %+v, want both deliveries counted into one batch", group)
		}
		if s.acks.Len() != 0 {
			t.Errorf("the listener was told %q before both landed, want nothing", s.acks.String())
		}
		s.resolveResponse(1, appServerMessage{"id": float64(1), "result": map[string]any{}})
		s.resolveResponse(2, appServerMessage{"id": float64(2), "result": map[string]any{}})
		if got, want := s.acks.String(), "ack b-3\n"; got != want {
			t.Errorf("the listener was told %q, want %q", got, want)
		}
	})
}

func TestCodexListenerStateHandleListenerLine(t *testing.T) {
	type effects struct {
		connects int
		turns    []string
		batches  []string
	}
	run := func(state *codexListenerState, lines ...string) effects {
		var got effects
		for _, line := range lines {
			state.handleListenerLine(line,
				func() { got.connects++ },
				func(text string) { got.turns = append(got.turns, text) },
				func(token string) { got.batches = append(got.batches, token) })
		}
		return got
	}

	t.Run("the boot connect wakes once and reports the connection", func(t *testing.T) {
		state := &codexListenerState{}

		got := run(state, "[ocagent] listen: connected (station oc-1)")

		want := effects{connects: 1, turns: []string{codexPostBootWake}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the line produced %+v, want %+v", got, want)
		}
		if !state.wakeSent {
			t.Error("the wake flag was not written, so the next reconnect would wake again")
		}
	})

	t.Run("a reconnect reports the connection and is forwarded as a notice", func(t *testing.T) {
		state := &codexListenerState{wakeSent: true}

		got := run(state, "[ocagent] listen: connected (station oc-2)")

		want := effects{connects: 1, turns: []string{"[ocagent] listen: connected (station oc-2)"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the line produced %+v, want %+v", got, want)
		}
	})

	t.Run("a whole session in order", func(t *testing.T) {
		state := &codexListenerState{}

		got := run(state,
			"[ocagent] listen: connected",
			"[ocagent] listen: batch b-1 [ts=2026-09-07T12:00:00 local]",
			"[chat] owner: 早安",
			"[ocagent] listen: retry 3 in 8s",
			"[ocagent] listen: disconnected — stream ended: EOF",
			"[ocagent] listen: connected",
			"[ocagent] listen: giving up after 40 attempts",
			"[ocagent] listen: batch b-2",
		)

		want := effects{
			connects: 2,
			turns: []string{
				codexPostBootWake,
				"[chat] owner: 早安",
				"[ocagent] listen: disconnected — stream ended: EOF",
				"[ocagent] listen: connected",
				"[ocagent] listen: giving up after 40 attempts",
			},
			batches: []string{"b-1", "b-2"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the session produced %+v, want %+v", got, want)
		}
	})

	t.Run("the batch marker never reaches the model", func(t *testing.T) {
		state := &codexListenerState{}

		got := run(state, "[ocagent] listen: batch b-1")

		want := effects{batches: []string{"b-1"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the marker produced %+v, want %+v", got, want)
		}
	})

	t.Run("a marker with no onBatch handler is still swallowed", func(t *testing.T) {
		state := &codexListenerState{}
		var turns []string

		state.handleListenerLine("[ocagent] listen: batch b-1",
			func() { t.Error("the marker reported a connection") },
			func(text string) { turns = append(turns, text) }, nil)

		if turns != nil {
			t.Errorf("the marker opened turn(s) %q, want none", turns)
		}
	})
}

// codexPost is one HTTP request the sidecar really made.
type codexPost struct {
	method string
	url    string
	auth   string
	ctype  string
	body   map[string]any
}

// codexFakeTransport answers every request with a canned status and records it.
// It replaces http.DefaultTransport for the duration of one test, which is the
// only seam post() leaves: its client is built inline with no Transport.
type codexFakeTransport struct {
	status int
	err    error
	posts  []codexPost
}

func (f *codexFakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	post := codexPost{
		method: req.Method, url: req.URL.String(),
		auth: req.Header.Get("Authorization"), ctype: req.Header.Get("Content-Type"),
	}
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(raw, &post.body); err != nil {
			return nil, err
		}
	}
	f.posts = append(f.posts, post)
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status, Status: http.StatusText(f.status),
		Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: http.Header{}, Request: req,
	}, nil
}

// interceptCodexPosts routes the sidecar's HTTP through a fake for one test.
func interceptCodexPosts(t *testing.T, status int, err error) *codexFakeTransport {
	t.Helper()
	fake := &codexFakeTransport{status: status, err: err}
	saved := http.DefaultTransport
	http.DefaultTransport = fake
	t.Cleanup(func() { http.DefaultTransport = saved })
	return fake
}

func TestCodexSessionPost(t *testing.T) {
	t.Run("the request the server sees", func(t *testing.T) {
		fake := interceptCodexPosts(t, http.StatusOK, nil)
		s := newCodexTestSession()
		s.base, s.token = "https://officraft.example/", "wire-token"

		s.post("/api/agent/context", map[string]any{"context_pct": 41.0, "compaction_count": 2})

		want := []codexPost{{
			method: http.MethodPost, url: "https://officraft.example/api/agent/context",
			auth: "Bearer wire-token", ctype: "application/json",
			body: map[string]any{"context_pct": float64(41), "compaction_count": float64(2)},
		}}
		if !reflect.DeepEqual(fake.posts, want) {
			t.Errorf("the sidecar sent %+v, want %+v", fake.posts, want)
		}
		if s.pane.Len() != 0 {
			t.Errorf("an accepted post logged %q, want nothing", s.pane.String())
		}
	})

	t.Run("a refused post is reported in the pane", func(t *testing.T) {
		interceptCodexPosts(t, http.StatusUnprocessableEntity, nil)
		s := newCodexTestSession()
		s.base, s.token = "https://officraft.example", "wire-token"

		s.post("/api/monitoring/telemetry", map[string]any{"runtime": "codex"})

		want := []string{"Codex POST /api/monitoring/telemetry rejected with HTTP 422"}
		if got := s.paneLines(t); !reflect.DeepEqual(got, want) {
			t.Errorf("the pane shows %q, want %q", got, want)
		}
	})

	t.Run("an unreachable server is silent", func(t *testing.T) {
		interceptCodexPosts(t, 0, errors.New("dial tcp: connection refused"))
		s := newCodexTestSession()
		s.base, s.token = "https://officraft.example", "wire-token"

		s.post("/api/monitoring/telemetry", map[string]any{"runtime": "codex"})

		if s.pane.Len() != 0 {
			t.Errorf("a failed post logged %q, want nothing", s.pane.String())
		}
	})

	t.Run("an unparsable base never reaches the transport", func(t *testing.T) {
		fake := interceptCodexPosts(t, http.StatusOK, nil)
		s := newCodexTestSession()
		s.base, s.token = "https://offi craft.example", "wire-token"

		s.post("/api/monitoring/telemetry", map[string]any{"runtime": "codex"})

		if len(fake.posts) != 0 {
			t.Errorf("the sidecar sent %+v, want nothing", fake.posts)
		}
	})
}

func TestCodexSessionReportRejectedCodexPost(t *testing.T) {
	cases := []struct {
		status int
		want   []string
	}{
		{status: http.StatusOK},
		{status: http.StatusNoContent},
		{status: http.StatusPermanentRedirect},
		{status: http.StatusBadRequest,
			want: []string{"Codex POST /api/monitoring/telemetry rejected with HTTP 400"}},
		{status: http.StatusUnauthorized,
			want: []string{"Codex POST /api/monitoring/telemetry rejected with HTTP 401"}},
		{status: http.StatusServiceUnavailable,
			want: []string{"Codex POST /api/monitoring/telemetry rejected with HTTP 503"}},
	}
	for _, tc := range cases {
		s := newCodexTestSession()

		s.reportRejectedCodexPost("/api/monitoring/telemetry", tc.status)

		if got := s.paneLines(t); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("HTTP %d put %q in the pane, want %q", tc.status, got, tc.want)
		}
	}
}

func TestCodexSessionAllowUsageReport(t *testing.T) {
	s := newCodexTestSession()

	if !s.allowUsageReport() {
		t.Error("the first report was throttled")
	}
	if s.allowUsageReport() {
		t.Error("a second report inside the throttle window was allowed")
	}

	s.forceUsageReport = true
	if !s.allowUsageReport() {
		t.Error("a forced report inside the throttle window was refused")
	}
	if s.forceUsageReport {
		t.Error("the force flag survived the report it forced")
	}
	if s.allowUsageReport() {
		t.Error("the report after a forced one was not throttled again")
	}

	s.lastUsageReport = time.Now().Add(-codexTelemetryThrottle)
	if !s.allowUsageReport() {
		t.Error("a report a full throttle window later was refused")
	}
}

func TestCodexSessionRequestRateLimits(t *testing.T) {
	s := newCodexTestSession()

	s.requestRateLimits()
	s.requestRateLimits()

	want := []map[string]any{
		{"id": float64(1), "method": "account/rateLimits/read", "params": nil},
	}
	if got := s.sent(t); !reflect.DeepEqual(got, want) {
		t.Errorf("the sidecar sent %v, want a single read while one is in flight", got)
	}
	if s.rateLimitReadID != 1 {
		t.Errorf("rateLimitReadID = %d, want 1 so the answer can be recognised", s.rateLimitReadID)
	}

	s.rateLimitReadID = 0
	s.requestRateLimits()
	if got := s.sent(t); len(got) != 2 || got[1]["id"] != float64(2) {
		t.Errorf("after the answer arrived the sidecar sent %v, want a second read with id 2", got)
	}
}

func TestCodexSessionReportTokenUsage(t *testing.T) {
	usage := func(window float64) map[string]any {
		return map[string]any{"tokenUsage": map[string]any{
			"modelContextWindow": window,
			"last":               map[string]any{"totalTokens": float64(41)},
			"total": map[string]any{
				"inputTokens": float64(1), "outputTokens": float64(2), "totalTokens": float64(10),
				"serverSideOnly": float64(9),
			},
		}}
	}

	t.Run("a blank launch model is omitted rather than reported as measured", func(t *testing.T) {
		fake := interceptCodexPosts(t, http.StatusOK, nil)
		s := newCodexTestSession()
		s.base, s.token, s.account, s.effort, s.model = "https://x.test", "tok", "codex:abc", "high", "  "
		s.compactions = 2

		s.reportTokenUsage(usage(100))

		want := []codexPost{
			{
				method: http.MethodPost, url: "https://x.test/api/agent/context",
				auth: "Bearer tok", ctype: "application/json",
				body: map[string]any{"context_pct": float64(41), "compaction_count": float64(2)},
			},
			{
				method: http.MethodPost, url: "https://x.test/api/monitoring/telemetry",
				auth: "Bearer tok", ctype: "application/json",
				body: map[string]any{
					"runtime": "codex", "account": "codex:abc", "account_label": "ChatGPT",
					"effort": "high",
					"tokens": map[string]any{
						"inputTokens": float64(1), "outputTokens": float64(2), "totalTokens": float64(10),
					},
				},
			},
		}
		if !reflect.DeepEqual(fake.posts, want) {
			t.Errorf("the sidecar sent %+v, want %+v", fake.posts, want)
		}
		if got, want := s.paneLines(t), []string{"context 41% · compact 2"}; !reflect.DeepEqual(got, want) {
			t.Errorf("the pane shows %q, want %q", got, want)
		}
	})

	t.Run("no context window means no context gauge", func(t *testing.T) {
		fake := interceptCodexPosts(t, http.StatusOK, nil)
		s := newCodexTestSession()
		s.base, s.token, s.account, s.effort = "https://x.test", "tok", "codex:abc", "medium"

		s.reportTokenUsage(usage(0))

		if len(fake.posts) != 1 || fake.posts[0].url != "https://x.test/api/monitoring/telemetry" {
			t.Fatalf("the sidecar sent %+v, want the telemetry body alone", fake.posts)
		}
		if s.pane.Len() != 0 {
			t.Errorf("the pane shows %q, want nothing without a window to measure against", s.pane.String())
		}
	})

	t.Run("a throttled report reaches nothing", func(t *testing.T) {
		fake := interceptCodexPosts(t, http.StatusOK, nil)
		s := newCodexTestSession()
		s.base, s.token = "https://x.test", "tok"

		s.reportTokenUsage(usage(100))
		before := len(fake.posts)
		s.reportTokenUsage(usage(100))

		if len(fake.posts) != before {
			t.Errorf("the throttled report sent %+v", fake.posts[before:])
		}
	})

	t.Run("an empty payload still reports the runtime and its empty token map", func(t *testing.T) {
		fake := interceptCodexPosts(t, http.StatusOK, nil)
		s := newCodexTestSession()
		s.base, s.token, s.account, s.effort = "https://x.test", "tok", "codex:abc", "medium"

		s.reportTokenUsage(map[string]any{})

		want := []codexPost{{
			method: http.MethodPost, url: "https://x.test/api/monitoring/telemetry",
			auth: "Bearer tok", ctype: "application/json",
			body: map[string]any{
				"runtime": "codex", "account": "codex:abc", "account_label": "ChatGPT",
				"effort": "medium", "tokens": map[string]any{},
			},
		}}
		if !reflect.DeepEqual(fake.posts, want) {
			t.Errorf("the sidecar sent %+v, want %+v", fake.posts, want)
		}
	})
}

func TestCodexSessionReportRateLimits(t *testing.T) {
	cases := []struct {
		name     string
		snapshot map[string]any
		want     map[string]any
	}{
		{
			name: "a five-hour window at the boundary",
			snapshot: map[string]any{"primary": map[string]any{
				"windowDurationMins": float64(360), "usedPercent": float64(12), "resetsAt": "2026-09-07T17:00:00Z"}},
			want: map[string]any{"five_hour": map[string]any{
				"used_percentage": float64(12), "resets_at": "2026-09-07T17:00:00Z"}},
		},
		{
			name: "one minute past the boundary is the weekly window",
			snapshot: map[string]any{"primary": map[string]any{
				"windowDurationMins": float64(361), "usedPercent": float64(12), "resetsAt": nil}},
			want: map[string]any{"seven_day": map[string]any{
				"used_percentage": float64(12), "resets_at": nil}},
		},
		{
			name: "weekly only leaves five_hour absent rather than fabricated",
			snapshot: map[string]any{
				"primary":   map[string]any{"windowDurationMins": float64(0), "usedPercent": float64(3)},
				"secondary": map[string]any{"windowDurationMins": float64(10080), "usedPercent": float64(40)},
			},
			want: map[string]any{"seven_day": map[string]any{
				"used_percentage": float64(40), "resets_at": nil}},
		},
		{
			name: "the later window wins when both map to the same name",
			snapshot: map[string]any{
				"primary":   map[string]any{"windowDurationMins": float64(300), "usedPercent": float64(1)},
				"secondary": map[string]any{"windowDurationMins": float64(60), "usedPercent": float64(2)},
			},
			want: map[string]any{"five_hour": map[string]any{
				"used_percentage": float64(2), "resets_at": nil}},
		},
		{name: "an empty snapshot reports nothing", snapshot: map[string]any{}},
		{
			name:     "a snapshot whose windows are absent reports nothing",
			snapshot: map[string]any{"primary": nil, "secondary": nil},
		},
		{
			name: "a window with no duration reports nothing",
			snapshot: map[string]any{"primary": map[string]any{
				"usedPercent": float64(90), "resetsAt": "2026-09-07T17:00:00Z"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := interceptCodexPosts(t, http.StatusOK, nil)
			s := newCodexTestSession()
			s.base, s.token, s.account = "https://x.test", "tok", "codex:abc"

			s.reportRateLimits(tc.snapshot)

			if tc.want == nil {
				if len(fake.posts) != 0 {
					t.Errorf("the sidecar sent %+v, want nothing", fake.posts)
				}
				return
			}
			want := []codexPost{{
				method: http.MethodPost, url: "https://x.test/api/monitoring/telemetry",
				auth: "Bearer tok", ctype: "application/json",
				body: map[string]any{
					"runtime": "codex", "account": "codex:abc", "account_label": "ChatGPT",
					"rate_limits": tc.want,
				},
			}}
			if !reflect.DeepEqual(fake.posts, want) {
				t.Errorf("the sidecar sent %+v, want %+v", fake.posts, want)
			}
		})
	}
}

func TestCodexSessionRecordCompaction(t *testing.T) {
	compaction := func(id string) map[string]any {
		return map[string]any{"item": map[string]any{"type": "contextCompaction", "id": id}}
	}

	t.Run("ignored signals", func(t *testing.T) {
		cases := []struct {
			name   string
			params map[string]any
		}{
			{name: "no item", params: map[string]any{}},
			{name: "a null item", params: map[string]any{"item": nil}},
			{name: "another kind of item", params: map[string]any{
				"item": map[string]any{"type": "assistantMessage", "id": "i_1"}}},
			{name: "an anonymous echo", params: compaction("")},
			{name: "an item with no id at all", params: map[string]any{
				"item": map[string]any{"type": "contextCompaction"}}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				s := newCodexTestSession()

				s.recordCompaction(tc.params)

				if s.compactions != 0 || s.forceUsageReport || s.pane.Len() != 0 {
					t.Errorf("count=%d force=%v pane=%q, want 0, false and nothing",
						s.compactions, s.forceUsageReport, s.pane.String())
				}
			})
		}
	})

	t.Run("each item is counted once, however often it is replayed", func(t *testing.T) {
		s := newCodexTestSession()

		s.recordCompaction(compaction("i_1"))
		if !s.forceUsageReport {
			t.Error("a compaction did not force the next usage report")
		}
		s.recordCompaction(compaction("i_1"))
		s.recordCompaction(compaction("i_2"))
		s.recordCompaction(compaction("i_1"))

		if s.compactions != 2 {
			t.Errorf("compactions = %d, want 2", s.compactions)
		}
		want := []string{"context compacted · count 1", "context compacted · count 2"}
		if got := s.paneLines(t); !reflect.DeepEqual(got, want) {
			t.Errorf("the pane shows %q, want %q", got, want)
		}
	})
}

func TestCodexSessionHandleServerRequest(t *testing.T) {
	cases := []struct {
		name string
		msg  appServerMessage
		want map[string]any
	}{
		{
			name: "a user-input request is refused with the open-it-yourself instruction",
			msg: appServerMessage{"id": "req-7", "method": "item/tool/requestUserInput",
				"params": map[string]any{"questions": []any{
					map[string]any{"id": "q1"},
					map[string]any{"id": "q2", "isSecret": true},
				}}},
			want: map[string]any{"id": "req-7", "result": map[string]any{"answers": map[string]any{
				"q1": map[string]any{"answers": []any{codexOpenYourOwnCardMessage(map[string]any{})}},
				"q2": map[string]any{"answers": []any{
					codexOpenYourOwnCardMessage(map[string]any{"isSecret": true})}},
			}}},
		},
		{
			name: "a request with no questions answers with an empty answer set",
			msg: appServerMessage{"id": float64(4), "method": "item/tool/requestUserInput",
				"params": map[string]any{}},
			want: map[string]any{"id": float64(4),
				"result": map[string]any{"answers": map[string]any{}}},
		},
		{
			name: "an elicitation is declined",
			msg:  appServerMessage{"id": float64(5), "method": "mcpServer/elicitation/request"},
			want: map[string]any{"id": float64(5), "result": map[string]any{"action": "decline"}},
		},
		{
			name: "anything else is answered as unsupported",
			msg:  appServerMessage{"id": float64(6), "method": "session/somethingNew"},
			want: map[string]any{"id": float64(6), "error": map[string]any{
				"code":    float64(-32601),
				"message": "OffiCraft sidecar does not support this server request"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newCodexTestSession()

			s.handleServerRequest(tc.msg)

			if got, want := s.sent(t), []map[string]any{tc.want}; !reflect.DeepEqual(got, want) {
				t.Errorf("the sidecar answered %v, want %v", got, want)
			}
			want := []string{"native user-input request → OffiCraft reply card"}
			if got := s.paneLines(t); !reflect.DeepEqual(got, want) {
				t.Errorf("the pane shows %q, want %q", got, want)
			}
		})
	}
}

func TestRunCodexSession(t *testing.T) {
	env := credEnvFunc(map[string]string{"OC_BASE": "https://x.test", "OC_TOKEN": "tok"})

	t.Run("incomplete launch parameters are refused before anything starts", func(t *testing.T) {
		cases := []struct {
			name string
			argv []string
		}{
			{name: "nothing at all", argv: nil},
			{name: "no codex binary", argv: []string{"--workdir", "/w", "--persona", "/w/P.md"}},
			{name: "no workdir", argv: []string{"--codex-bin", "/bin/true", "--persona", "/w/P.md"}},
			{name: "no persona", argv: []string{"--codex-bin", "/bin/true", "--workdir", "/w"}},
			{name: "an unknown flag", argv: []string{"--nope"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				out := &bytes.Buffer{}

				code := runCodexSession(tc.argv, env, out)

				if code != 2 {
					t.Errorf("runCodexSession returned %d, want 2", code)
				}
				if !strings.HasSuffix(out.String(), "codex-session: missing required launch parameters\n") {
					t.Errorf("runCodexSession printed %q, want it to end with the missing-parameters line",
						out.String())
				}
			})
		}
	})

	t.Run("an unlaunchable codex binary is reported", func(t *testing.T) {
		work := t.TempDir()
		out := &bytes.Buffer{}

		code := runCodexSession([]string{
			"--codex-bin", filepath.Join(work, "no-such-codex"),
			"--workdir", work, "--persona", filepath.Join(work, "P.md"),
		}, env, out)

		if code != 1 {
			t.Errorf("runCodexSession returned %d, want 1", code)
		}
		if !strings.HasPrefix(out.String(), "codex-session: start app-server: ") {
			t.Errorf("runCodexSession printed %q, want a start app-server failure", out.String())
		}
	})

	t.Run("an app server that never answers initialize ends the session", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		work := t.TempDir()
		codexBin := filepath.Join(work, "codex")
		script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >&2\nexit 0\n"
		if err := os.WriteFile(codexBin, []byte(script), 0o755); err != nil {
			t.Fatalf("stage codex stub: %v", err)
		}
		out := &bytes.Buffer{}

		code := runCodexSession([]string{
			"--codex-bin", codexBin, "--workdir", work,
			"--persona", filepath.Join(work, "P.md"), "--model", "gpt-5-codex",
		}, env, out)

		if code != 1 {
			t.Errorf("runCodexSession returned %d, want 1", code)
		}
		if !strings.Contains(out.String(),
			"codex-session: initialize: app-server exited before responding\n") {
			t.Errorf("runCodexSession printed %q, want the initialize failure", out.String())
		}
		if !strings.Contains(out.String(), "app-server\n") {
			t.Errorf("the stub was run as %q, want it invoked with the app-server subcommand", out.String())
		}
	})
}
