package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The listener runs BESIDE its claude member (started by cli/ocwarden/spawn.go,
// not inside the member's harness, which drops background jobs every 30 min) and
// reaches the member's pane with a tmux buffer + Enter, as the boot nudge does.

const (
	// 🔴 The session suffix is load-bearing: tmux buffers are per SERVER (one per
	// -L socket) and every member shares the `officraft` socket. Measured: with
	// one shared name, member B's line was pasted into A's pane and B got nothing
	// (the first paste's -d deleted the buffer) — silently, and already receipted.
	paneBufferPrefix = "oc-listen-deliver-"

	// One Enter can lose the race against a redrawing TUI.
	paneEnterAttempts = 3
	paneEnterSettle   = 700 * time.Millisecond

	paneCmdTimeout = 5 * time.Second

	// Owner ruling rc-62ede5d63772: anything bigger reaches the member as an id-only
	// notice. It counts the bytes actually pasted, header included, and has to stay
	// well under set-buffer's argv ceiling (~16.3 KB measured; the exact figure moves
	// with the socket and buffer name lengths).
	panePasteMaxBytes = 8 << 10

	oversizedNoticeHeaderRunes = 200
)

type tmuxRun func(args ...string) error

func realTmuxRun(args ...string) error {
	bin := resolveTmuxBin()
	if bin == "" {
		return fmt.Errorf("tmux not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), paneCmdTimeout)
	defer cancel()
	return exec.CommandContext(ctx, bin, args...).Run()
}

func listenSink(out io.Writer, env func(string) string, deliver bool, run tmuxRun) (io.Writer, func(), bool) {
	if !deliver {
		return out, func() {}, true
	}
	socket, session, ok := tmuxSessionFromEnv(env)
	if !ok {
		// 🔴 Refuse, do not degrade: without OC_SESSION makeSessionProbe returns
		// nil, so this listener could never self-exit and would hold the SSE —
		// i.e. keep a vanished member "online" — forever, saying nothing.
		fmt.Fprint(out, agentLinePrefix+"listen: --deliver-tmux needs OC_SESSION "+
			"(the session to deliver into, and the session this listener must die with); "+
			"refusing to start.\n")
		return nil, func() {}, false
	}
	w := newPaneWriter(out, socket, session, run, nil)
	return w, w.start(), true
}

// 🔴 A function so tests can pin the wiring: independent review made the flag
// switch ignore listenSink's answers and the package stayed green while
// --deliver-tmux silently became a no-op.
func cmdListen(argv []string, cfg Config, env func(string) string, out io.Writer,
	start func(Config, func(string) string, bool, io.Writer) int, run tmuxRun) int {
	fs := flag.NewFlagSet("ocagent listen", flag.ContinueOnError)
	fs.SetOutput(out)
	once := fs.Bool("once", false, "do a single connect then return (test/diagnostic hook)")
	deliver := fs.Bool("deliver-tmux", false,
		"run beside the member: deliver each event into OC_SESSION's pane instead of expecting it to read this stdout")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	sink, stop, ok := listenSink(out, env, *deliver, run)
	if !ok {
		return 2
	}
	defer stop()
	return start(cfg, env, *once, sink)
}

// 🔴 Lines are QUEUED for a pump, never delivered inline: Write runs inside the
// SSE scan loop, whose idle-read watchdog resets only on reads.
// One delivery costs ~2s and the hooks print the 〈停止〉 text one line per Write,
// so inline delivery was enough to sever the connection and get the member
// recycled.
type paneWriter struct {
	inner   io.Writer
	socket  string
	session string
	buffer  string
	run     tmuxRun
	sleep   func(time.Duration)

	// logMu guards inner ALONE (written by the SSE loop and by the pump's note);
	// mu guards the queue; neither is held while taking the other. Folding the log
	// write into mu would let a blocked stdout stall the pump. ⚠️ Assumes Write
	// has ONE calling goroutine (the SSE scan loop): a second one could order the
	// log and the queue differently, with nothing failing.
	logMu               sync.Mutex
	mu                  sync.Mutex
	pending             bytes.Buffer
	queued              []string
	firstConnectSettled bool

	wake chan struct{}
}

func newPaneWriter(inner io.Writer, socket, session string, run tmuxRun, sleep func(time.Duration)) *paneWriter {
	if run == nil {
		run = realTmuxRun
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	return &paneWriter{
		inner:   inner,
		socket:  socket,
		session: session,
		buffer:  paneBufferPrefix + session,
		run:     run,
		sleep:   sleep,
		wake:    make(chan struct{}, 1),
	}
}

func (w *paneWriter) start() func() {
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-w.wake:
				w.drain()
			case <-quit:
				w.drain()
				return
			}
		}
	}()
	return func() {
		close(quit)
		<-done
	}
}

func (w *paneWriter) Write(p []byte) (int, error) {
	w.logMu.Lock()
	n, err := w.inner.Write(p)
	w.logMu.Unlock()

	w.mu.Lock()
	w.pending.Write(p)
	for {
		line, ok := takePaneLine(&w.pending)
		if !ok {
			break
		}
		if w.shouldForward(line) {
			w.queued = append(w.queued, line)
		}
	}
	queued := len(w.queued)
	w.mu.Unlock()
	if queued > 0 {
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
	return n, err
}

func (w *paneWriter) drain() {
	for {
		w.mu.Lock()
		batch := w.queued
		w.queued = nil
		w.mu.Unlock()
		if len(batch) == 0 {
			return
		}
		for _, paste := range packPanePastes(batch) {
			w.deliver(paste)
		}
	}
}

// Packs whole events (a column-0 line plus its indented continuation lines) into
// pastes of at most panePasteMaxBytes; an event that alone exceeds it is replaced by
// its id-only notice, so no paste is ever split between lines.
func packPanePastes(lines []string) []string {
	var pastes []string
	var current string
	for _, event := range splitPaneEvents(lines) {
		if len(event) > panePasteMaxBytes {
			event = oversizedEventNotice(event)
		}
		if current != "" && len(current)+1+len(event) > panePasteMaxBytes {
			pastes = append(pastes, current)
			current = ""
		}
		if current == "" {
			current = event
		} else {
			current += "\n" + event
		}
	}
	if current != "" {
		pastes = append(pastes, current)
	}
	return pastes
}

func splitPaneEvents(lines []string) []string {
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
		strings.Count(event, "\n")+1, utf8.RuneCountInString(event), panePasteMaxBytes>>10, fullReadToolFor(header))
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

var paneEventRefRe = regexp.MustCompile(`#c-[0-9A-Za-z]+|\brc-[0-9A-Za-z]+|\bT-[0-9A-Za-z]+`)

// ASCII only, because it is typed with send-keys -l, which can drop multibyte
// characters into a busy TUI.
func undeliveredNotice(paste string) string {
	events := splitPaneEvents(strings.Split(paste, "\n"))
	refs := make([]string, 0, len(events))
	for _, event := range events {
		header, _, _ := strings.Cut(event, "\n")
		if ref := paneEventRefRe.FindString(header); ref != "" {
			refs = append(refs, ref)
		} else {
			refs = append(refs, "no id")
		}
	}
	return fmt.Sprintf("%slisten: %d event(s) could not be pasted into this pane (%s) - none of their text "+
		"was typed, read them with get_chat / get_reply_card / get_task",
		agentLinePrefix, len(events), strings.Join(refs, ", "))
}

func takePaneLine(buf *bytes.Buffer) (string, bool) {
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

// The connect that opens a BOOT is swallowed (the member is mid boot turn);
// later connects mean "the stream is back" and are forwarded.
//
// 🔴 "Opens a boot" ≠ "first connect printed": if the first dial fails, the
// forwarded disconnect notice promised the member that the next
// transport line is the reconnect or a give-up — so forwarding a disconnect or
// give-up spends the boot swallow. The codex sidecar is no precedent for plain
// swallowing: it replaces that line with a post-boot wake (codex_session.go).
func (w *paneWriter) shouldForward(line string) bool {
	if !forwardToPane(line) {
		return false
	}
	if strings.HasPrefix(line, agentLinePrefix+noticeConnected) && !w.firstConnectSettled {
		w.firstConnectSettled = true
		return false
	}
	if strings.HasPrefix(line, agentLinePrefix+noticeDisconnected) ||
		strings.HasPrefix(line, agentLinePrefix+noticeGivingUp) {
		w.firstConnectSettled = true
	}
	return true
}

// Owner's disconnect-notice ruling: event lines reach the member, transport
// chatter does not — except the three notices it names.
//
// 🔴 Match at column 0, never after a trim: chat bodies arrive INDENTED, and a
// message quoting a transport line would otherwise be swallowed as one of ours —
// silently and for good (the chat is already receipted as read).
func forwardToPane(line string) bool {
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

// 🔴 Never fall back to pasting line by line (owner ruling): every line would become
// its own turn, and on tmux 3.6b so would every newline of a bare paste. A failed
// paste gets one id-only line typed instead.
func (w *paneWriter) deliver(payload string) {
	err := w.run("-L", w.socket, "set-buffer", "-b", w.buffer, payload)
	if err == nil {
		err = w.run("-L", w.socket, "paste-buffer", "-t", w.session, "-b", w.buffer, "-d", "-p")
	}
	if err == nil {
		w.submit()
		return
	}
	notice := undeliveredNotice(payload)
	w.note("listen: tmux refused a paste into %s (%v); typing an id-only notice instead\n", w.session, err)
	_ = w.run("-L", w.socket, "copy-mode", "-q", "-t", w.session)
	if err := w.run("-L", w.socket, "send-keys", "-t", w.session, "-l", notice); err != nil {
		// The events are LOST: the claude path has no ack gate (only the codex
		// sidecar sets OC_LISTEN_ACK) and mark-read follows what was printed.
		w.note("listen: the id-only notice did not reach %s either (%v): %s\n", w.session, err, notice)
		return
	}
	w.submit()
}

func (w *paneWriter) note(format string, args ...any) {
	w.logMu.Lock()
	defer w.logMu.Unlock()
	fmt.Fprintf(w.inner, agentLinePrefix+format, args...)
}

// A pane left in copy-mode (someone attached and scrolled) swallows every Enter
// under emacs mode-keys while the paste still lands, so the member sits on
// unsent input for as long as nobody leaves the mode. `copy-mode -q` leaves
// every mode, not only copy-mode. Visible cost: whoever is reading the pane's
// scrollback is sent back to the bottom when an event arrives.
func (w *paneWriter) submit() {
	for attempt := 0; attempt < paneEnterAttempts; attempt++ {
		_ = w.run("-L", w.socket, "copy-mode", "-q", "-t", w.session)
		_ = w.run("-L", w.socket, "send-keys", "-t", w.session, "Enter")
		w.sleep(paneEnterSettle)
	}
}
