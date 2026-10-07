package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

// A claude member's listener does not print for the member to read: it runs as
// a child of the member's own Claude Code, started by the notification mod
// (cli/ocwarden/mod), and writes each event into that session's messaging
// socket (--deliver-socket, listen_socket.go).

const listenCodexFlag = "deliver-codex"

// Owner ruling rc-62ede5d63772: anything bigger reaches the member as an id-only
// notice. It counts the bytes actually delivered, header included.
const (
	deliveryMaxBytes = 8 << 10

	oversizedNoticeHeaderRunes = 200
)

// Claude delivery must pass through the socket route; raw stdout never reaches its conversation.
func cmdListen(argv []string, cfg Config, env func(string) string, out, errOut io.Writer,
	start func(Config, func(string) string, bool, io.Writer) int) int {
	fs := flag.NewFlagSet("ocagent listen", flag.ContinueOnError)
	fs.SetOutput(out)
	once := fs.Bool("once", false, "do a single connect then return (test/diagnostic hook)")
	deliverSocket := fs.Bool("deliver-socket", false,
		"run under the member's own Claude Code: write each payload into its messaging socket "+
			"("+messagingSocketEnv+", "+messagingTokenEnv+"), diagnostics on stderr")
	deliverCodex := fs.Bool(listenCodexFlag, false, "print each complete notice as one JSON frame for the Codex sidecar")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *deliverCodex {
		if *deliverSocket || env(listenAckEnv) != "1" {
			fmt.Fprint(errOut, agentLinePrefix+"listen: --deliver-codex requires OC_LISTEN_ACK=1 and cannot be combined with other delivery routes\n")
			return 2
		}
		return start(cfg, env, *once, &codexFrameWriter{out: out})
	}
	if !*deliverSocket {
		return start(cfg, env, *once, out)
	}
	if _, _, ok := tmuxSessionFromEnv(env); !ok {
		// 🔴 Refuse, do not degrade: without OC_SESSION makeSessionProbe returns
		// nil, so this listener could never self-exit and would hold the SSE —
		// i.e. keep a vanished member "online" — forever, saying nothing.
		fmt.Fprint(errOut, agentLinePrefix+"listen: --deliver-socket needs OC_SESSION "+
			"(the session to deliver into, and the session this listener must die with); "+
			"refusing to start.\n")
		return 2
	}
	// Missing messaging variables are reported per payload, never refused:
	// an exited listener looks like a dead member.
	w := newSocketWriter(env, errOut)
	stop := w.startPump()
	defer stop()
	return start(cfg, env, *once, w)
}

// Packs whole events (a column-0 line plus its indented continuation lines) into
// payloads of at most deliveryMaxBytes; an event that alone exceeds it is replaced
// by its id-only notice, so no payload is ever split between lines (owner ruling).
func packDeliveries(lines []string) []string {
	var payloads []string
	var current string
	for _, event := range splitEvents(lines) {
		if len(event) > deliveryMaxBytes {
			event = oversizedEventNotice(event)
		}
		if current != "" && len(current)+1+len(event) > deliveryMaxBytes {
			payloads = append(payloads, current)
			current = ""
		}
		if current == "" {
			current = event
		} else {
			current += "\n" + event
		}
	}
	if current != "" {
		payloads = append(payloads, current)
	}
	return payloads
}

func splitEvents(lines []string) []string {
	var events []string
	for _, line := range lines {
		isContinuation := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		if isContinuation && len(events) > 0 {
			events[len(events)-1] += "\n" + line
			continue
		}
		events = append(events, line)
	}
	return events
}

func oversizedEventNotice(event string) string {
	header, _, _ := strings.Cut(event, "\n")
	return previewLine(header, oversizedNoticeHeaderRunes) + fmt.Sprintf(
		" [這則通知約 %d 行／%d 字，超過送進畫面的上限 %d KiB，正文沒有送進來 — 請用 %s 讀全文]",
		strings.Count(event, "\n")+1, utf8.RuneCountInString(event), deliveryMaxBytes>>10, fullReadToolFor(header))
}

func fullReadToolFor(header string) string {
	switch {
	case strings.HasPrefix(header, agentLinePrefix+"reply-card "):
		return replyCardFullReadTool
	case strings.HasPrefix(header, agentLinePrefix+"task "):
		return "get_task"
	default:
		return chatFullReadTool
	}
}

// One queued unit: a forwarded line, or a batch marker (batch != "").
type deliveryItem struct {
	line  string
	batch string
}

// deliveryQueue: lines are QUEUED for a pump, never delivered inline — Write
// runs inside the SSE scan loop, whose idle-read watchdog resets only on reads.
type deliveryQueue struct {
	diag io.Writer

	// diagMu guards diag, mu the queue; neither is held while taking the other.
	// ⚠️ Assumes Write has ONE calling goroutine (the SSE scan loop): a second
	// one could interleave a batch marker with lines it does not close.
	diagMu  sync.Mutex
	mu      sync.Mutex
	pending bytes.Buffer
	queued  []deliveryItem
	inHand  int // items the pump has taken but not yet attempted
	filter  bootConnectFilter

	wake chan struct{}
}

func newDeliveryQueue(diag io.Writer) *deliveryQueue {
	return &deliveryQueue{diag: diag, wake: make(chan struct{}, 1)}
}

func (q *deliveryQueue) start(drain func()) func() {
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-q.wake:
				drain()
			case <-quit:
				drain()
				return
			}
		}
	}()
	return func() {
		close(quit)
		<-done
	}
}

// Every transport line also goes to diag, forwarded or not: the mod writes its
// load marker only on seeing one there (cli/ocwarden/mod), and a first dial that
// fails forwards the disconnect and spends the swallowed boot connect.
func (q *deliveryQueue) Write(p []byte) (int, error) {
	var diagLines []string
	q.mu.Lock()
	q.pending.Write(p)
	for {
		line, ok := takeLine(&q.pending)
		if !ok {
			break
		}
		if token, isBatch := batchMarkerToken(line); isBatch {
			q.queued = append(q.queued, deliveryItem{batch: token})
		} else if q.filter.shouldForward(line) {
			q.queued = append(q.queued, deliveryItem{line: line})
			if strings.HasPrefix(line, agentLinePrefix+"listen:") {
				diagLines = append(diagLines, line)
			}
		} else {
			diagLines = append(diagLines, line)
		}
	}
	queued := len(q.queued)
	q.mu.Unlock()

	if len(diagLines) > 0 {
		q.diagMu.Lock()
		_, _ = io.WriteString(q.diag, strings.Join(diagLines, "\n")+"\n")
		q.diagMu.Unlock()
	}
	if queued > 0 {
		select {
		case q.wake <- struct{}{}:
		default:
		}
	}
	return len(p), nil
}

// The listener stamps the line with a trailing `[ts=…]`, so the token is the
// first field after the head (same parse as codex_session.go's).
func batchMarkerToken(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, agentLinePrefix+noticeBatch+" ")
	if !ok {
		return "", false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", false
	}
	return fields[0], true
}

// drain hands every queued line on as packed payloads, each batch marker only
// after the payloads printed before it.
func (q *deliveryQueue) drain(payload func(string), batch func(string)) {
	for {
		q.mu.Lock()
		items := q.queued
		q.queued = nil
		q.inHand = len(items)
		q.mu.Unlock()
		if len(items) == 0 {
			return
		}
		var lines []string
		flush := func() {
			for _, p := range packDeliveries(lines) {
				payload(p)
			}
			q.settle(len(lines))
			lines = nil
		}
		for _, item := range items {
			if item.batch == "" {
				lines = append(lines, item.line)
				continue
			}
			flush()
			q.settle(1)
			batch(item.batch)
		}
		flush()
	}
}

func (q *deliveryQueue) settle(n int) {
	q.mu.Lock()
	q.inHand -= n
	q.mu.Unlock()
}

// idle: every line written so far has been attempted, so a batch answer
// already given covers all of it.
func (q *deliveryQueue) idle() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queued) == 0 && q.inHand == 0
}

func (q *deliveryQueue) note(format string, args ...any) {
	q.diagMu.Lock()
	defer q.diagMu.Unlock()
	fmt.Fprintf(q.diag, agentLinePrefix+format, args...)
}

func takeLine(buf *bytes.Buffer) (string, bool) {
	all := buf.Bytes()
	i := bytes.IndexByte(all, '\n')
	if i < 0 {
		return "", false
	}
	line := string(all[:i])
	rest := append([]byte(nil), all[i+1:]...)
	buf.Reset()
	buf.Write(rest)
	return line, true
}

type bootConnectFilter struct {
	firstConnectSettled bool
}

// The connect that opens a BOOT is swallowed: the boot turn is already running
// or queued (submitted by the mod before it starts the listener) and reads the
// station itself. Later connects mean "the stream is
// back" and are forwarded.
//
// 🔴 "Opens a boot" ≠ "first connect printed": if the first dial fails, the
// forwarded disconnect notice promised the member that the next
// transport line is the reconnect or a give-up — so forwarding a disconnect or
// give-up spends the boot swallow. The codex sidecar is no precedent for plain
// swallowing: it replaces that line with a post-boot wake (codex_session.go).
func (f *bootConnectFilter) shouldForward(line string) bool {
	if !forwardToMember(line) {
		return false
	}
	if strings.HasPrefix(line, agentLinePrefix+noticeConnected) && !f.firstConnectSettled {
		f.firstConnectSettled = true
		return false
	}
	if strings.HasPrefix(line, agentLinePrefix+noticeDisconnected) ||
		strings.HasPrefix(line, agentLinePrefix+noticeGivingUp) {
		f.firstConnectSettled = true
	}
	return true
}

// Owner's disconnect-notice ruling: event lines reach the member, transport
// chatter does not — except the three notices it names.
//
// 🔴 Match at column 0, never after a trim: chat bodies arrive INDENTED, and a
// message quoting a transport line would otherwise be swallowed as one of ours —
// silently and for good (the chat is already receipted as read).
func forwardToMember(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	rest, isOurs := strings.CutPrefix(line, agentLinePrefix)
	if !isOurs || !strings.HasPrefix(rest, "listen:") {
		return true
	}
	for _, notice := range []string{noticeDisconnected, noticeConnected, noticeGivingUp} {
		if strings.HasPrefix(rest, notice) {
			return true
		}
	}
	return false
}
