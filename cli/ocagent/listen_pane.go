package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// --deliver-tmux: the listener runs BESIDE its claude member in a tmux session of
// its own (cli/ocwarden/spawn.go) and reaches the member's pane with a tmux
// buffer + Enter, as the boot nudge does. Text pasted while someone has the pane
// on a sub-agent view goes to that sub-agent, which is why the mod route exists.

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
	logMu   sync.Mutex
	mu      sync.Mutex
	pending bytes.Buffer
	queued  []string
	filter  bootConnectFilter

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
		line, ok := takeLine(&w.pending)
		if !ok {
			break
		}
		if w.filter.shouldForward(line) {
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
		for _, paste := range packDeliveries(batch) {
			w.deliver(paste)
		}
	}
}

var paneEventRefRe = regexp.MustCompile(`#c-[0-9A-Za-z]+|\brc-[0-9A-Za-z]+|\bT-[0-9A-Za-z]+`)

// ASCII only, because it is typed with send-keys -l, which can drop multibyte
// characters into a busy TUI.
func undeliveredNotice(paste string) string {
	events := splitEvents(strings.Split(paste, "\n"))
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
		// The events are LOST: the paste route has no ack gate (only the codex
		// sidecar and the notification mod set OC_LISTEN_ACK) and mark-read
		// follows what was printed.
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
