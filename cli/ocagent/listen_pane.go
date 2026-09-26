package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// The listener runs BESIDE its claude member (started by cli/ocwarden/spawn.go,
// not inside the member's harness, which drops background jobs every 30 min) and
// reaches the member's pane with the same tmux buffer + Enter the boot nudge uses.

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
		w.deliver(strings.Join(batch, "\n"))
	}
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

// 🔴 The fallback re-sends ONE LINE AT A TIME, never the batch: on tmux 3.6b a
// bare paste turns each newline into Enter (a 17-line batch = 17 interleaved
// turns); on 3.7c it does not (measured in a real Claude Code pane). It is
// version-dependent, so assume the worst.
func (w *paneWriter) deliver(payload string) {
	_ = w.run("-L", w.socket, "set-buffer", "-b", w.buffer, payload)
	if err := w.run("-L", w.socket, "paste-buffer", "-t", w.session, "-b", w.buffer, "-d", "-p"); err == nil {
		w.submit()
		return
	}
	lines := strings.Split(payload, "\n")
	for i, line := range lines {
		_ = w.run("-L", w.socket, "set-buffer", "-b", w.buffer, line)
		if err := w.run("-L", w.socket, "paste-buffer", "-t", w.session, "-b", w.buffer); err != nil {
			// Bail on the first failure: a GONE pane fails every line, and stop()
			// drains synchronously (a 17-line batch would hold shutdown ~36s). The
			// remaining lines are LOST: the claude path has no ack gate (only the
			// codex sidecar sets OC_LISTEN_ACK) and mark-read follows what was
			// printed, so the listener's own log is the only record.
			w.note("listen: gave up on %d line(s) after tmux refused a paste into %s\n",
				len(lines)-i, w.session)
			return
		}
		w.submit()
	}
}

func (w *paneWriter) note(format string, args ...any) {
	w.logMu.Lock()
	defer w.logMu.Unlock()
	fmt.Fprintf(w.inner, agentLinePrefix+format, args...)
}

func (w *paneWriter) submit() {
	for attempt := 0; attempt < paneEnterAttempts; attempt++ {
		_ = w.run("-L", w.socket, "send-keys", "-t", w.session, "Enter")
		w.sleep(paneEnterSettle)
	}
}
