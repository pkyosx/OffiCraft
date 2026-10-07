package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// --deliver-socket: the listener is a child of the member's own Claude Code and
// writes each packed payload into that session's messaging socket. Claude Code
// puts it between tool calls, or starts a turn when idle. The socket never
// answers, so "delivered" only means the write completed and the session closed
// the connection: a rejected token, a full queue, a refused burst or a changed
// line format look the same, and the chat is still marked read.
//
// ⚠️ The session queues at most 50 accepted messages and refuses rapid bursts;
// packing is what keeps a backlog under that.
const (
	messagingSocketEnv = "CLAUDE_CODE_MESSAGING_SOCKET"
	// 🔴 A bearer credential for this user's sessions: never print it.
	messagingTokenEnv = "CLAUDE_CODE_MESSAGING_TOKEN"
	messagingSender   = "officraft"
	messagingTimeout  = 3 * time.Second
)

// deliveryReport hears the outcome of every payload written to the member's
// session; reason is empty when delivered.
type deliveryReport func(delivered bool, reason string)

type socketWriter struct {
	*deliveryQueue
	socketPath string
	token      string
	timeout    time.Duration
	report     deliveryReport
	answers    chan string
	// Pump-only; spans drains, since a marker can be queued after its payloads were taken.
	batchFailed bool
}

func newSocketWriter(env func(string) string, diag io.Writer) *socketWriter {
	w := &socketWriter{
		deliveryQueue: newDeliveryQueue(diag),
		socketPath:    strings.TrimSpace(env(messagingSocketEnv)),
		token:         strings.TrimSpace(env(messagingTokenEnv)),
		timeout:       messagingTimeout,
		answers:       make(chan string, 64),
	}
	w.report = w.logUndelivered
	return w
}

func (w *socketWriter) startPump() func() {
	return w.deliveryQueue.start(w.flushToSession)
}

// ackGate answers each batch marker in-process: ack only when every payload
// printed before it was written to the socket.
func (w *socketWriter) ackGate() *ackGate {
	return &ackGate{answers: w.answers, wait: ackWaitTimeout, timeoutNotice: w.diag}
}

func (w *socketWriter) flushToSession() {
	w.deliveryQueue.drain(
		func(payload string) {
			if !w.deliverPayload(payload) {
				w.batchFailed = true
			}
		},
		func(token string) {
			verb := "ack"
			if w.batchFailed {
				verb = "nack"
			}
			w.batchFailed = false
			select {
			case w.answers <- verb + " " + token:
			default:
			}
		},
	)
}

func (w *socketWriter) deliverPayload(payload string) bool {
	if err := w.writeToSession(payload); err != nil {
		w.report(false, err.Error())
		return false
	}
	w.report(true, "")
	return true
}

func (w *socketWriter) writeToSession(content string) error {
	if missing := w.missingEnv(); missing != "" {
		return fmt.Errorf("收件管道環境變數不存在（%s 沒有設定）", missing)
	}
	conn, err := net.DialTimeout("unix", w.socketPath, w.timeout)
	if err != nil {
		return fmt.Errorf("連不上收件 socket（%v）", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(w.timeout))
	lines, err := messagingLines(w.token, content)
	if err != nil {
		return fmt.Errorf("訊息編碼失敗（%v）", err)
	}
	if _, err := conn.Write(lines); err != nil {
		return fmt.Errorf("寫入收件 socket 失敗（%v）", err)
	}
	if unixConn, ok := conn.(*net.UnixConn); ok {
		if err := unixConn.CloseWrite(); err != nil {
			return fmt.Errorf("寫入收件 socket 失敗（%v）", err)
		}
	}
	if _, err := io.Copy(io.Discard, conn); err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return fmt.Errorf("收件 socket %s 內沒有關閉連線", w.timeout)
		}
		return fmt.Errorf("收件 socket 中途斷線（%v）", err)
	}
	return nil
}

func (w *socketWriter) missingEnv() string {
	var missing []string
	if w.socketPath == "" {
		missing = append(missing, messagingSocketEnv)
	}
	if w.token == "" {
		missing = append(missing, messagingTokenEnv)
	}
	return strings.Join(missing, "、")
}

type messagingAuth struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

type messagingUser struct {
	Type    string           `json:"type"`
	From    string           `json:"from"`
	Message messagingContent `json:"message"`
}

type messagingContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// The message line's shape is not in Claude Code's public docs: it was read
// off the binary and confirmed against its debug log.
func messagingLines(token, content string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(messagingAuth{Type: "auth", Token: token}); err != nil {
		return nil, err
	}
	err := enc.Encode(messagingUser{
		Type:    "user",
		From:    messagingSender,
		Message: messagingContent{Role: "user", Content: content},
	})
	return buf.Bytes(), err
}

func (w *socketWriter) logUndelivered(delivered bool, reason string) {
	if delivered {
		return
	}
	w.note("listen: 通知沒有送進成員的對話：%s —— 聊天與 reply-card 不算已讀，之後補送會再送一次\n", reason)
}

func socketSinkOf(out io.Writer) *socketWriter {
	if stamped, ok := out.(*stampWriter); ok {
		out = stamped.inner
	}
	w, _ := out.(*socketWriter)
	return w
}
