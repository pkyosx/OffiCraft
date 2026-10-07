package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const testMessagingToken = "tok-0123456789abcdef"

// inboxSocket stands in for a Claude Code session's messaging socket and keeps
// every connection's bytes exactly as written.
type inboxSocket struct {
	path string
	mu   sync.Mutex
	got  []string
}

// A short directory under /tmp: macOS refuses unix socket paths past ~104 bytes.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "oc-inbox-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// newInboxSocket reads each connection to EOF and closes it, as the session
// does; with holding set it accepts and then never reads or closes.
func newInboxSocket(t *testing.T, holding bool) *inboxSocket {
	t.Helper()
	s := &inboxSocket{path: filepath.Join(shortSocketDir(t), "s.sock")}
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var held []net.Conn
	t.Cleanup(func() {
		ln.Close()
		<-done
		for _, c := range held {
			c.Close()
		}
	})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if holding {
				held = append(held, conn)
				continue
			}
			raw, _ := io.ReadAll(conn)
			// Recorded before the close: the writer returns on that EOF.
			s.mu.Lock()
			s.got = append(s.got, string(raw))
			s.mu.Unlock()
			conn.Close()
		}
	}()
	return s
}

func (s *inboxSocket) written() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.got...)
}

// staleSocketPath is a socket file nobody listens on any more.
func staleSocketPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(shortSocketDir(t), "s.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	ln.Close()
	return path
}

func messagingEnv(socketPath string) func(string) string {
	return testEnv(map[string]string{messagingSocketEnv: socketPath, messagingTokenEnv: testMessagingToken})
}

func takeAnswer(t *testing.T, w *socketWriter) string {
	t.Helper()
	select {
	case a := <-w.answers:
		return a
	default:
		t.Fatal("no batch answer was given")
		return ""
	}
}

// lockedBuffer lets a test read what the pump goroutine writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// sessionWrite is what one delivered payload puts on the socket; the first
// TestSocketWriter case spells the same bytes out by hand.
func sessionWrite(t *testing.T, content string) string {
	t.Helper()
	b, err := messagingLines(testMessagingToken, content)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sessionWrites(t *testing.T, contents ...string) []string {
	t.Helper()
	var out []string
	for _, c := range contents {
		out = append(out, sessionWrite(t, c))
	}
	return out
}

func chatEvent(t *testing.T, id, body string) string {
	t.Helper()
	var out bytes.Buffer
	printChatLine(&out, map[string]any{"id": id, "from": "owner", "body": body}, 0)
	return strings.TrimSuffix(out.String(), "\n")
}

// quotedInBody is the CONTINUATION line of a chat body that quotes a transport
// line, produced by renderMessageBody — the renderer the real path uses. The
// indentation is what these cases turn on, and typed as a string literal it was
// four spaces nobody could see: anything that tidied the literal would have
// retired the case into a duplicate of "other transport chatter" and left the
// package green.
func quotedInBody(t *testing.T, transportLine string) string {
	t.Helper()
	lines := strings.Split(renderMessageBody("一位成員貼給另一位：\n"+transportLine, "get_chat"), "\n")
	if len(lines) != 2 {
		t.Fatalf("renderMessageBody produced %d lines, want 2: %q", len(lines), lines)
	}
	if !strings.HasPrefix(lines[1], " ") {
		t.Fatalf("the continuation line is not indented (%q) — this case would test nothing", lines[1])
	}
	return lines[1]
}

func newTestSocketWriter(t *testing.T) (*socketWriter, *inboxSocket, *bytes.Buffer) {
	t.Helper()
	inbox := newInboxSocket(t, false)
	var diag bytes.Buffer
	return newSocketWriter(messagingEnv(inbox.path), &diag), inbox, &diag
}

func TestSocketWriter(t *testing.T) {
	t.Run("an event is written as the auth line then one message line, and nothing is logged", func(t *testing.T) {
		inbox := newInboxSocket(t, false)
		var diag bytes.Buffer
		w := newSocketWriter(messagingEnv(inbox.path), &diag)

		w.Write([]byte("[ocagent] chat #c-1 from Owner: 看 \"這個\" <b>&\n    第二行\n"))
		w.flushToSession()

		want := []string{`{"type":"auth","token":"tok-0123456789abcdef"}` + "\n" +
			`{"type":"user","from":"officraft","message":{"role":"user","content":"[ocagent] chat #c-1 from Owner: 看 \"這個\" <b>&\n    第二行"}}` + "\n"}
		if got := inbox.written(); !reflect.DeepEqual(got, want) {
			t.Errorf("written =\n%q\nwant\n%q", got, want)
		}
		if diag.String() != "" {
			t.Errorf("diag = %q, want nothing", diag.String())
		}
	})

	t.Run("a blank line opens no connection", func(t *testing.T) {
		inbox := newInboxSocket(t, false)
		w := newSocketWriter(messagingEnv(inbox.path), io.Discard)

		w.Write([]byte("   \n\n"))
		w.flushToSession()

		if got := inbox.written(); len(got) != 0 {
			t.Errorf("written = %q, want no connection at all", got)
		}
	})

	t.Run("a batch is acked once every payload before its marker was written, and a failure spoils only its own batch", func(t *testing.T) {
		inbox := newInboxSocket(t, false)
		w := newSocketWriter(messagingEnv(inbox.path), io.Discard)

		w.Write([]byte("[ocagent] chat from owner (#c-1): 甲\n[ocagent] listen: batch 1 [ts=1759381200.000 local]\n"))
		w.flushToSession()
		if got := takeAnswer(t, w); got != "ack 1" {
			t.Errorf("answer = %q, want ack 1", got)
		}

		w.socketPath = filepath.Join(filepath.Dir(inbox.path), "gone.sock")
		w.Write([]byte("[ocagent] chat from owner (#c-2): 乙\n[ocagent] listen: batch 2\n"))
		w.flushToSession()
		if got := takeAnswer(t, w); got != "nack 2" {
			t.Errorf("answer = %q, want nack 2", got)
		}

		w.socketPath = inbox.path
		w.Write([]byte("[ocagent] listen: batch 3\n"))
		w.flushToSession()
		if got := takeAnswer(t, w); got != "ack 3" {
			t.Errorf("answer = %q, want ack 3", got)
		}
	})

	t.Run("a payload taken before its marker was queued still decides that batch", func(t *testing.T) {
		w := newSocketWriter(messagingEnv(filepath.Join(shortSocketDir(t), "gone.sock")), io.Discard)

		w.Write([]byte("[ocagent] chat from owner (#c-1): 甲\n"))
		w.flushToSession()
		w.Write([]byte("[ocagent] listen: batch 1\n"))
		w.flushToSession()

		if got := takeAnswer(t, w); got != "nack 1" {
			t.Errorf("answer = %q, want nack 1", got)
		}
	})

	t.Run("an undeliverable payload is logged with its reason and never with the token", func(t *testing.T) {
		missing := filepath.Join(shortSocketDir(t), "gone.sock")
		stale := staleSocketPath(t)
		for _, tc := range []struct {
			name   string
			env    func(string) string
			reason string
		}{
			{"no socket file", messagingEnv(missing),
				"連不上收件 socket（dial unix " + missing + ": connect: no such file or directory）"},
			{"a socket nobody listens on", messagingEnv(stale),
				"連不上收件 socket（dial unix " + stale + ": connect: connection refused）"},
			{"no messaging variables", testEnv(nil),
				"收件管道環境變數不存在（CLAUDE_CODE_MESSAGING_SOCKET、CLAUDE_CODE_MESSAGING_TOKEN 沒有設定）"},
			{"no token", testEnv(map[string]string{messagingSocketEnv: missing}),
				"收件管道環境變數不存在（CLAUDE_CODE_MESSAGING_TOKEN 沒有設定）"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var diag bytes.Buffer
				w := newSocketWriter(tc.env, &diag)

				w.Write([]byte("[ocagent] chat from owner (#c-1): 甲\n[ocagent] listen: batch 1\n"))
				w.flushToSession()

				want := "[ocagent] listen: 通知沒有送進成員的對話：" + tc.reason +
					" —— 聊天與 reply-card 不算已讀，之後補送會再送一次\n"
				if diag.String() != want {
					t.Errorf("diag = %q, want %q", diag.String(), want)
				}
				if strings.Contains(diag.String(), testMessagingToken) {
					t.Error("the messaging token was logged")
				}
				if got := takeAnswer(t, w); got != "nack 1" {
					t.Errorf("answer = %q, want nack 1", got)
				}
			})
		}
	})

	t.Run("a session that never finishes reading fails the payload at the timeout", func(t *testing.T) {
		inbox := newInboxSocket(t, true)
		var diag bytes.Buffer
		w := newSocketWriter(messagingEnv(inbox.path), &diag)
		w.timeout = 100 * time.Millisecond

		w.Write([]byte("[ocagent] chat from owner (#c-1): 甲\n[ocagent] listen: batch 1\n"))
		w.flushToSession()

		want := "[ocagent] listen: 通知沒有送進成員的對話：收件 socket 100ms 內沒有收完這則訊息" +
			" —— 聊天與 reply-card 不算已讀，之後補送會再送一次\n"
		if diag.String() != want {
			t.Errorf("diag = %q, want %q", diag.String(), want)
		}
		if got := takeAnswer(t, w); got != "nack 1" {
			t.Errorf("answer = %q, want nack 1", got)
		}
	})

	t.Run("the report seam hears every payload's outcome", func(t *testing.T) {
		inbox := newInboxSocket(t, false)
		w := newSocketWriter(messagingEnv(inbox.path), io.Discard)
		type outcome struct {
			delivered bool
			reason    string
		}
		var heard []outcome
		w.report = func(delivered bool, reason string) { heard = append(heard, outcome{delivered, reason}) }

		w.Write([]byte("[ocagent] chat from owner (#c-1): 甲\n"))
		w.flushToSession()
		w.token = ""
		w.Write([]byte("[ocagent] chat from owner (#c-2): 乙\n"))
		w.flushToSession()

		want := []outcome{{true, ""}, {false, "收件管道環境變數不存在（CLAUDE_CODE_MESSAGING_TOKEN 沒有設定）"}}
		if !reflect.DeepEqual(heard, want) {
			t.Errorf("heard %+v, want %+v", heard, want)
		}
	})

	t.Run("under transport chatter, it goes to stderr while the notices the ruling names are delivered and echoed there", func(t *testing.T) {
		for _, tc := range []struct {
			name, line string
			written    []string
			diag       string
		}{
			{"a line this binary did not write", "something else entirely",
				[]string{"something else entirely"}, ""},
			{"the first disconnect", "[ocagent] listen: disconnected — dial tcp: connection refused",
				[]string{"[ocagent] listen: disconnected — dial tcp: connection refused"},
				"[ocagent] listen: disconnected — dial tcp: connection refused\n"},
			{"giving up", "[ocagent] listen: giving up — 30 attempts",
				[]string{"[ocagent] listen: giving up — 30 attempts"},
				"[ocagent] listen: giving up — 30 attempts\n"},
			{"the boot connect", "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events",
				nil, "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events\n"},
			{"other transport chatter", "[ocagent] listen: retrying in 4s",
				nil, "[ocagent] listen: retrying in 4s\n"},
			{"a blank line", "   ", nil, "   \n"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				w, inbox, diag := newTestSocketWriter(t)
				w.Write([]byte(tc.line + "\n"))
				w.flushToSession()

				if got, want := inbox.written(), sessionWrites(t, tc.written...); !reflect.DeepEqual(got, want) {
					t.Errorf("written = %q, want %q", got, want)
				}
				if diag.String() != tc.diag {
					t.Errorf("diag = %q, want %q", diag.String(), tc.diag)
				}
			})
		}
	})

	t.Run("under a boot connect, it is swallowed and every later one is delivered", func(t *testing.T) {
		// It is also the half that stops an OVERSHOOT ("never swallow at all"): the
		// forwarded-disconnect case below passes under that mutant, so the two are
		// only a two-way guard while BOTH are alive.
		w, inbox, diag := newTestSocketWriter(t)

		boot := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events"
		back := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events [same station]"
		again := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events [new station — was c67268a4]"
		w.Write([]byte(boot + "\n" + back + "\n" + again + "\n"))
		w.flushToSession()

		if got, want := inbox.written(), sessionWrites(t, back+"\n"+again); !reflect.DeepEqual(got, want) {
			t.Errorf("written = %q, want %q", got, want)
		}
		if want := boot + "\n" + back + "\n" + again + "\n"; diag.String() != want {
			t.Errorf("diag = %q, want %q", diag.String(), want)
		}
	})

	t.Run("under a connect that answers a forwarded disconnect, it is not swallowed", func(t *testing.T) {
		// The first failed dial promises the member that the next transport line is
		// the reconnect or a give-up (listen_run.go); swallowing that reconnect
		// leaves the member waiting. 🔴 Half a guard: "never swallow anything"
		// passes it too, the boot-connect case above refuses that.
		w, inbox, _ := newTestSocketWriter(t)

		down := agentLinePrefix + noticeDisconnected + " — dial tcp: connection refused" +
			" (retrying on the same schedule, quietly; the next transport line you see" +
			" is either the reconnect or a give-up)"
		up := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events"
		w.Write([]byte(down + "\n"))
		w.flushToSession()
		w.Write([]byte(up + "\n"))
		w.flushToSession()

		if got, want := inbox.written(), sessionWrites(t, down, up); !reflect.DeepEqual(got, want) {
			t.Errorf("written = %q, want %q", got, want)
		}
	})

	t.Run("under a quoted transport line, the boot swallow is not spent", func(t *testing.T) {
		// The quoted copy reaches this writer INDENTED (renderMessageBody), so a
		// comparison that trims first mistakes it for this binary's own notice and
		// forwards the real boot connect into a member that is still booting.
		boot := "[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events"
		for _, quotedNotice := range []string{
			boot,
			"[ocagent] listen: disconnected — dial tcp: connection refused",
		} {
			t.Run(quotedNotice, func(t *testing.T) {
				w, inbox, _ := newTestSocketWriter(t)

				quoted := quotedInBody(t, quotedNotice)
				w.Write([]byte(quoted + "\n"))
				w.flushToSession()
				w.Write([]byte(boot + "\n"))
				w.flushToSession()

				if got, want := inbox.written(), sessionWrites(t, quoted); !reflect.DeepEqual(got, want) {
					t.Errorf("written = %q, want only the quoted line", got)
				}
			})
		}
	})

	t.Run("under several batches, each marker is answered only after the payloads printed before it", func(t *testing.T) {
		w, inbox, _ := newTestSocketWriter(t)
		var order []string
		w.report = func(delivered bool, _ string) { order = append(order, fmt.Sprintf("payload %v", delivered)) }

		w.Write([]byte("[ocagent] chat from owner (#c-1): 甲\n"))
		w.Write([]byte("[ocagent] chat from owner (#c-2): 乙\n"))
		w.Write([]byte("[ocagent] listen: batch 1 [ts=1759381200.000 local]\n"))
		w.Write([]byte("[ocagent] chat from owner (#c-3): 丙\n"))
		w.Write([]byte("[ocagent] listen: batch 2 [ts=1759381201.000 local]\n"))
		w.flushToSession()

		want := sessionWrites(t,
			"[ocagent] chat from owner (#c-1): 甲\n[ocagent] chat from owner (#c-2): 乙",
			"[ocagent] chat from owner (#c-3): 丙")
		if got := inbox.written(); !reflect.DeepEqual(got, want) {
			t.Errorf("written =\n%q\nwant\n%q", got, want)
		}
		if got := []string{takeAnswer(t, w), takeAnswer(t, w)}; !reflect.DeepEqual(got, []string{"ack 1", "ack 2"}) {
			t.Errorf("answers = %q, want ack 1, ack 2", got)
		}
		if want := []string{"payload true", "payload true"}; !reflect.DeepEqual(order, want) {
			t.Errorf("reports = %q, want %q", order, want)
		}
	})

	t.Run("under a chat body quoting a batch marker, it is chat, not a marker", func(t *testing.T) {
		w, inbox, _ := newTestSocketWriter(t)
		quoted := quotedInBody(t, "[ocagent] listen: batch 9")
		w.Write([]byte("[ocagent] chat from owner (#c-5): 一位成員貼給另一位：\n" + quoted + "\n"))
		w.flushToSession()

		want := sessionWrites(t, "[ocagent] chat from owner (#c-5): 一位成員貼給另一位：\n    [ocagent] listen: batch 9")
		if got := inbox.written(); !reflect.DeepEqual(got, want) {
			t.Errorf("written = %q, want %q", got, want)
		}
		select {
		case a := <-w.answers:
			t.Errorf("answer = %q, want none", a)
		default:
		}
	})

	t.Run("under half a line, nothing is written until the rest of it", func(t *testing.T) {
		w, inbox, _ := newTestSocketWriter(t)

		w.Write([]byte("[ocagent] chat #c-1 from "))
		w.flushToSession()
		if got := inbox.written(); len(got) != 0 {
			t.Fatalf("half a line was written: %q", got)
		}

		w.Write([]byte("Owner: 看一下這個\n"))
		w.flushToSession()
		if got, want := inbox.written(), sessionWrites(t, "[ocagent] chat #c-1 from Owner: 看一下這個"); !reflect.DeepEqual(got, want) {
			t.Errorf("written = %q, want %q", got, want)
		}
	})

	t.Run("under the ceiling, an event goes whole and one byte more goes as its id-only notice", func(t *testing.T) {
		// Owner ruling rc-62ede5d63772: the ceiling counts every byte delivered,
		// the header included.
		header := "[ocagent] chat from owner (#c-1): "
		atCeiling := header + strings.Repeat("a", 8192-len(header))
		bigChat := chatEvent(t, "c-2", strings.Repeat("界", 4000))
		var long strings.Builder
		for i := 0; i < 1194; i++ {
			fmt.Fprintf(&long, "line %04d xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n", i)
		}
		big := strings.Repeat("b", 9000)
		for _, tc := range []struct{ name, event, written string }{
			{"an event at the ceiling", atCeiling, atCeiling},
			{"one byte over", atCeiling + "a", header + strings.Repeat("a", 166) +
				"… [這則通知約 1 行／8193 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_chat 讀全文]"},
			{"a thousand-line message", chatEvent(t, "c-6feb08ebdbb6", long.String()),
				"[ocagent] chat from owner (#c-6feb08ebdbb6): line 0000 xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" +
					" [這則通知約 1194 行／53770 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_chat 讀全文]"},
			{"a first line cut at 200 characters, between characters", bigChat,
				"[ocagent] chat from owner (#c-2): " + strings.Repeat("界", 166) +
					"… [這則通知約 1 行／4034 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_chat 讀全文]"},
			{"a reply card", "[ocagent] reply-card rc-1 answered: " + big,
				"[ocagent] reply-card rc-1 answered: " + strings.Repeat("b", 164) +
					"… [這則通知約 1 行／9036 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_reply_card 讀全文]"},
			{"a task", "[ocagent] task T-1 " + big,
				"[ocagent] task T-1 " + strings.Repeat("b", 181) +
					"… [這則通知約 1 行／9019 字，超過送進畫面的上限 8 KiB，正文沒有送進來 — 請用 get_task 讀全文]"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				w, inbox, _ := newTestSocketWriter(t)
				w.Write([]byte(tc.event + "\n"))
				w.flushToSession()

				if got, want := inbox.written(), sessionWrites(t, tc.written); !reflect.DeepEqual(got, want) {
					t.Errorf("written =\n%q\nwant\n%q", got, want)
				}
			})
		}
	})

	t.Run("under queued events that together pass the ceiling, writes split between events, never inside one", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			events  []string
			written []string
		}{
			{"two that fit only without their joining newline",
				[]string{"[ocagent] chat from owner (#c-1): " + strings.Repeat("a", 4096-34),
					"[ocagent] chat from owner (#c-2): " + strings.Repeat("b", 4096-34)},
				[]string{"[ocagent] chat from owner (#c-1): " + strings.Repeat("a", 4096-34),
					"[ocagent] chat from owner (#c-2): " + strings.Repeat("b", 4096-34)}},
			{"multi-line events",
				[]string{chatEvent(t, "c-1", "甲\n"+strings.Repeat("a", 5000)),
					chatEvent(t, "c-2", "乙\n"+strings.Repeat("b", 5000)),
					"[ocagent] chat from owner (#c-3): 短的"},
				[]string{chatEvent(t, "c-1", "甲\n"+strings.Repeat("a", 5000)),
					chatEvent(t, "c-2", "乙\n"+strings.Repeat("b", 5000)) + "\n[ocagent] chat from owner (#c-3): 短的"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				w, inbox, _ := newTestSocketWriter(t)
				w.Write([]byte(strings.Join(tc.events, "\n") + "\n"))
				w.flushToSession()

				if got, want := inbox.written(), sessionWrites(t, tc.written...); !reflect.DeepEqual(got, want) {
					t.Errorf("got %d write(s), want %d", len(got), len(want))
				}
			})
		}
	})

	t.Run("under a running pump, a write is delivered with nobody stopping or draining the writer", func(t *testing.T) {
		// Observed BEFORE stop(): stopping flushes, so looking only afterwards is
		// green even when Write never wakes the pump.
		w, inbox, _ := newTestSocketWriter(t)
		stop := w.startPump()
		defer stop()

		w.Write([]byte("[ocagent] chat #c-1 from Owner: 看一下這個\n"))

		want := sessionWrites(t, "[ocagent] chat #c-1 from Owner: 看一下這個")
		deadline := time.Now().Add(2 * time.Second)
		for !reflect.DeepEqual(inbox.written(), want) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if got := inbox.written(); !reflect.DeepEqual(got, want) {
			t.Errorf("written = %q, want %q", got, want)
		}
	})
}
