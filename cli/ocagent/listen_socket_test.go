package main

import (
	"bytes"
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
			conn.Close()
			s.mu.Lock()
			s.got = append(s.got, string(raw))
			s.mu.Unlock()
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
}
