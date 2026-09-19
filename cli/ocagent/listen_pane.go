package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Delivery into a claude member's pane, for a listener the member does NOT own.
//
// A claude member used to mount `ocagent listen` itself, inside the harness that
// runs it; the harness drops that background job every 30 minutes, and a member
// stuck in one long tool call has no moment to re-mount it. Presence is the SSE
// connection, so the station read the gap as death and recycled a healthy
// session. The listener is now started BESIDE the member (cli/ocwarden/spawn.go)
// and this is how what it reads reaches the member: the same tmux buffer +
// Enter the boot nudge uses. The codex runtime has had the same shape since
// T-4595, with its sidecar forwarding into App Server instead of a pane.
//
// The forwarding POLICY is the owner's 2026-08-30 disconnect-notice ruling, and
// the constants it matches on are the ones this binary prints (listen_run.go),
// not a copy of them — the codex side has to keep a copy because it is a
// different Go module, and that copy has already drifted once.

const (
	// One buffer name for all deliveries: they are serialised (deliverMu), so a
	// second name would only make two half-pasted lines possible.
	paneBufferName = "oc-listen-deliver"

	// Enter is what SUBMITS the pasted line; the paste alone leaves it sitting
	// in the input box. A single Enter loses the race against a TUI that is
	// redrawing, and an Enter on an empty box costs nothing, so press a few.
	paneEnterAttempts = 3
	paneEnterSettle   = 700 * time.Millisecond

	paneCmdTimeout = 5 * time.Second
)

// listenSink answers what a `listen` run writes to, and is a named function
// rather than three lines inside the flag switch because the switch is
// unreachable from a test: a run that reaches cmdListen opens a connection.
// Returning ok=false means the caller must refuse — the message is already
// written.
func listenSink(out io.Writer, env func(string) string, deliver bool) (io.Writer, bool) {
	if !deliver {
		return out, true
	}
	socket, session, ok := suicideSession(env)
	if !ok {
		// 🔴 REFUSE, do not degrade. With no OC_SESSION this listener has nowhere
		// to deliver AND its self-exit probe is disabled (makeSessionProbe returns
		// nil), so it would hold the SSE open forever and the station would read a
		// member that no longer exists as permanently alive — never recycling it.
		// That is worse than the recycling this whole change exists to stop, and
		// it announces nothing on its own: a listener nobody can reach looks
		// exactly like a quiet one.
		fmt.Fprint(out, agentLinePrefix+"listen: --deliver-tmux needs OC_SESSION "+
			"(the session to deliver into, and the session this listener must die with); "+
			"refusing to start.\n")
		return nil, false
	}
	return newPaneWriter(out, socket, session, nil, nil), true
}

// tmuxRun runs one tmux argv. Injected so delivery can be asserted without a
// terminal.
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

// paneWriter is the listener's output sink when it runs beside its member: every
// line still goes to inner (the listener's own session is attachable, and that
// log is the only place a swallowed line can be read), and the lines the policy
// says the member must see are additionally delivered into the member's pane.
type paneWriter struct {
	inner   io.Writer
	socket  string
	session string
	run     tmuxRun
	sleep   func(time.Duration)

	mu      sync.Mutex
	pending bytes.Buffer
}

func newPaneWriter(inner io.Writer, socket, session string, run tmuxRun, sleep func(time.Duration)) *paneWriter {
	if run == nil {
		run = realTmuxRun
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	return &paneWriter{inner: inner, socket: socket, session: session, run: run, sleep: sleep}
}

func (w *paneWriter) Write(p []byte) (int, error) {
	n, err := w.inner.Write(p)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending.Write(p)
	for {
		line, ok := takePaneLine(&w.pending)
		if !ok {
			break
		}
		if forwardToPane(line) {
			w.deliver(line)
		}
	}
	return n, err
}

// takePaneLine pops one COMPLETE line. A trailing fragment stays buffered: half
// a line delivered now would arrive as a turn that says nothing and would leave
// its other half to arrive as a second one.
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

// forwardToPane applies the owner's notice policy: everything the listener has
// to say about EVENTS reaches the member, and its own transport chatter does not
// — except the three notices the ruling names, because 「斷線 → 沉默」 cannot
// tell 還在重試 from 已經放棄.
func forwardToPane(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	rest, isOurs := strings.CutPrefix(trimmed, agentLinePrefix)
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

func (w *paneWriter) deliver(line string) {
	_ = w.run("-L", w.socket, "set-buffer", "-b", paneBufferName, line)
	if err := w.run("-L", w.socket, "paste-buffer", "-t", w.session, "-b", paneBufferName, "-d", "-p"); err != nil {
		_ = w.run("-L", w.socket, "paste-buffer", "-t", w.session, "-b", paneBufferName)
	}
	for attempt := 0; attempt < paneEnterAttempts; attempt++ {
		_ = w.run("-L", w.socket, "send-keys", "-t", w.session, "Enter")
		w.sleep(paneEnterSettle)
	}
}
