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

// A claude member's listener does not print for the member to read: its events
// are delivered into the member's conversation, by one of two routes the warden
// picks per spawn (cli/ocwarden/spawn.go):
//   - --deliver-mod: the listener is the child of the member's own Claude Code
//     notification mod (cli/ocwarden/mod), which submits each payload as a
//     prompt and answers the ack gate;
//   - --deliver-tmux: the listener runs beside the member in its own tmux
//     session and pastes into the member's pane. The fallback for a Claude Code
//     without mods, or one that did not load the mod.
//
// --deliver-socket (listen_socket.go) is a third route the warden does not pick yet.

const listenCodexFlag = "deliver-codex"

const (
	// Owner ruling rc-62ede5d63772: anything bigger reaches the member as an id-only
	// notice. It counts the bytes actually delivered, header included, and has to
	// stay well under set-buffer's argv ceiling (~16.3 KB measured; the exact figure
	// moves with the socket and buffer name lengths).
	deliveryMaxBytes = 8 << 10

	oversizedNoticeHeaderRunes = 200
)

type listenRoute struct {
	tmux, mod, socket bool
}

func (r listenRoute) flagNames() []string {
	var names []string
	for _, f := range []struct {
		on   bool
		name string
	}{{r.tmux, "--deliver-tmux"}, {r.mod, "--deliver-mod"}, {r.socket, "--deliver-socket"}} {
		if f.on {
			names = append(names, f.name)
		}
	}
	return names
}

func listenSink(out, errOut io.Writer, env func(string) string, route listenRoute, run tmuxRun) (io.Writer, func(), bool) {
	flags := route.flagNames()
	if len(flags) == 0 {
		return out, func() {}, true
	}
	if len(flags) > 1 {
		fmt.Fprint(errOut, agentLinePrefix+"listen: "+flags[0]+" and "+flags[1]+" are two routes "+
			"into the same member; pick one. Refusing to start.\n")
		return nil, func() {}, false
	}
	socket, session, ok := tmuxSessionFromEnv(env)
	if !ok {
		// 🔴 Refuse, do not degrade: without OC_SESSION makeSessionProbe returns
		// nil, so this listener could never self-exit and would hold the SSE —
		// i.e. keep a vanished member "online" — forever, saying nothing.
		w := errOut
		if route.tmux {
			w = out
		}
		fmt.Fprint(w, agentLinePrefix+"listen: "+flags[0]+" needs OC_SESSION "+
			"(the session to deliver into, and the session this listener must die with); "+
			"refusing to start.\n")
		return nil, func() {}, false
	}
	switch {
	case route.socket:
		// Missing messaging variables are reported per payload, never refused:
		// an exited listener looks like a dead member.
		w := newSocketWriter(env, errOut)
		return w, w.startPump(), true
	case route.mod:
		// Without the ack channel a submit the session refused would still be
		// marked read on the station.
		if env(listenAckEnv) != "1" || strings.TrimSpace(env(listenAckFileEnv)) == "" {
			fmt.Fprint(errOut, agentLinePrefix+"listen: --deliver-mod needs "+listenAckEnv+"=1 and "+
				listenAckFileEnv+" (the file the mod answers each batch in); refusing to start.\n")
			return nil, func() {}, false
		}
		w := newModWriter(out, errOut)
		return w, w.start(), true
	}
	w := newPaneWriter(out, socket, session, run, nil)
	return w, w.start(), true
}

// Claude delivery must pass through listenSink; raw stdout never reaches its conversation.
func cmdListen(argv []string, cfg Config, env func(string) string, out, errOut io.Writer,
	start func(Config, func(string) string, bool, io.Writer) int, run tmuxRun) int {
	fs := flag.NewFlagSet("ocagent listen", flag.ContinueOnError)
	fs.SetOutput(out)
	once := fs.Bool("once", false, "do a single connect then return (test/diagnostic hook)")
	deliverTmux := fs.Bool("deliver-tmux", false,
		"run beside the member: deliver each event into OC_SESSION's pane instead of expecting it to read this stdout")
	deliverMod := fs.Bool("deliver-mod", false,
		"run under the member's notification mod: print one JSON frame per line on stdout "+
			"(submit payloads and batch markers), diagnostics on stderr, acks read from OC_LISTEN_ACK_FILE")
	deliverSocket := fs.Bool("deliver-socket", false,
		"run under the member's own Claude Code: write each payload into its messaging socket "+
			"("+messagingSocketEnv+", "+messagingTokenEnv+"), diagnostics on stderr")
	deliverCodex := fs.Bool(listenCodexFlag, false, "print each complete notice as one JSON frame for the Codex sidecar")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *deliverCodex {
		if *deliverTmux || *deliverMod || *deliverSocket || env(listenAckEnv) != "1" {
			fmt.Fprint(errOut, agentLinePrefix+"listen: --deliver-codex requires OC_LISTEN_ACK=1 and cannot be combined with other delivery routes\n")
			return 2
		}
		return start(cfg, env, *once, &codexFrameWriter{out: out})
	}
	route := listenRoute{tmux: *deliverTmux, mod: *deliverMod, socket: *deliverSocket}
	sink, stop, ok := listenSink(out, errOut, env, route, run)
	if !ok {
		return 2
	}
	defer stop()
	return start(cfg, env, *once, sink)
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

// deliveryQueue is what the routes that answer the ack gate share: lines are
// QUEUED for a pump, never delivered inline — Write runs inside the SSE scan
// loop, whose idle-read watchdog resets only on reads.
type deliveryQueue struct {
	diag io.Writer

	// diagMu guards diag, mu the queue; neither is held while taking the other.
	// ⚠️ Assumes Write has ONE calling goroutine (the SSE scan loop): a second
	// one could interleave a batch marker with lines it does not close.
	diagMu  sync.Mutex
	mu      sync.Mutex
	pending bytes.Buffer
	queued  []deliveryItem
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

func (q *deliveryQueue) Write(p []byte) (int, error) {
	var unforwarded []string
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
		} else {
			unforwarded = append(unforwarded, line)
		}
	}
	queued := len(q.queued)
	q.mu.Unlock()

	if len(unforwarded) > 0 {
		q.diagMu.Lock()
		_, _ = io.WriteString(q.diag, strings.Join(unforwarded, "\n")+"\n")
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
		q.mu.Unlock()
		if len(items) == 0 {
			return
		}
		var lines []string
		flush := func() {
			for _, p := range packDeliveries(lines) {
				payload(p)
			}
			lines = nil
		}
		for _, item := range items {
			if item.batch == "" {
				lines = append(lines, item.line)
				continue
			}
			flush()
			batch(item.batch)
		}
		flush()
	}
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
// or queued (pasted by the warden, or submitted by the mod before it starts the
// listener) and reads the station itself. Later connects mean "the stream is
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
