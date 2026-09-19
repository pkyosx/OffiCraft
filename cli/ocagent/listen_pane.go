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
	// 🔴 ONE BUFFER PER SESSION, AND THE SUFFIX IS LOAD-BEARING. tmux buffer
	// names live on the SERVER (one per -L socket), not on a session, and every
	// member on this machine shares the `officraft` socket — so a single shared
	// name means N listeners writing one buffer. Measured with real tmux: two
	// set-buffers then two pastes put member B's line into member A's pane and
	// left B with nothing, because the first paste's `-d` deleted the buffer.
	// Both halves are silent: the misdelivered line was already receipted as
	// read, so it never comes back.
	paneBufferPrefix = "oc-listen-deliver-"

	// Enter is what SUBMITS the pasted text; the paste alone leaves it sitting in
	// the input box. A single Enter loses the race against a TUI that is
	// redrawing, and an Enter on an empty box costs nothing, so press a few.
	paneEnterAttempts = 3
	paneEnterSettle   = 700 * time.Millisecond

	paneCmdTimeout = 5 * time.Second
)

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

// listenSink answers what a `listen` run writes to, plus the shutdown call that
// flushes whatever is still queued. ok=false means the caller must refuse — the
// message is already written.
func listenSink(out io.Writer, env func(string) string, deliver bool, run tmuxRun) (io.Writer, func(), bool) {
	if !deliver {
		return out, func() {}, true
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
		return nil, func() {}, false
	}
	w := newPaneWriter(out, socket, session, run, nil)
	return w, w.start(), true
}

// runListen is the whole `listen` subcommand: flags, the output decision, then
// the run itself.
//
// 🔴 IT IS A FUNCTION SO THE WIRING CAN BE PINNED. Inside the flag switch, the
// lines that decide where events go were reachable by no test at all — a run
// that gets as far as cmdListen opens a connection. Independent review made the
// switch ignore BOTH of listenSink's answers and the whole package stayed green
// while --deliver-tmux became a no-op: the member got nothing, the listener
// still held the SSE, and the station read it as healthy.
//
// start is cmdListen in production; run is nil there (the real tmux).
func runListen(argv []string, cfg Config, env func(string) string, out io.Writer,
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

// paneWriter is the listener's output sink when it runs beside its member: every
// line still goes to inner (the listener's own session is attachable, and that
// log is the only place a swallowed line can be read), and the lines the policy
// says the member must see are queued for delivery into the member's pane.
//
// 🔴 QUEUED, NOT DELIVERED INLINE, AND THAT IS NOT AN OPTIMISATION. Write is
// called from inside the SSE scan loop, and the idle-read watchdog is reset only
// when that loop reads — so a handler that blocks the loop for longer than the
// watchdog kills its own connection. One delivery costs ~2s, the wind-down and
// recycle hooks print an owner-editable document ONE LINE PER WRITE, and the
// shipped 〈停止〉 text is already 17 lines. Inline, being told how to shut down
// was enough to sever the connection and have the member recycled.
//
// Queuing also fixes what the member SEES: everything that arrives while a batch
// is being delivered goes out as ONE paste, so a 17-line checklist is one turn
// rather than 17 one-line turns racing each other's Enter.
type paneWriter struct {
	inner   io.Writer
	socket  string
	session string
	buffer  string
	run     tmuxRun
	sleep   func(time.Duration)

	mu           sync.Mutex
	pending      bytes.Buffer // bytes not yet forming a complete line
	queued       []string     // complete lines awaiting delivery
	sawConnected bool         // the boot connect has already been swallowed

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

// start runs the delivery pump and returns the stop call. Stopping drains what
// is still queued: the last thing this listener says is usually the give-up
// line, and a member told nothing cannot tell 還在重試 from 已經放棄.
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
	n, err := w.inner.Write(p)
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
		default: // a wake is already pending; the pump drains everything anyway
		}
	}
	return n, err
}

// drain delivers everything queued, batch by batch, and returns once nothing is
// left. Lines that arrive DURING a delivery are picked up by the next round,
// which is what turns a burst into one paste instead of one paste per line.
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

// shouldForward is forwardToPane plus the one piece of state the policy needs:
// the FIRST connect is swallowed.
//
// The codex side swallows it for a reason that applies here too — that line
// arrives while the member is still running its boot turn, so forwarding it
// spends a turn on a transport notice nobody asked for, once per member per
// boot. Every LATER connect is forwarded: by then it means the stream came back,
// which is the half of the owner's notice ruling that has to reach the member.
//
// Caller holds w.mu.
func (w *paneWriter) shouldForward(line string) bool {
	if !forwardToPane(line) {
		return false
	}
	if strings.HasPrefix(line, agentLinePrefix+noticeConnected) && !w.sawConnected {
		w.sawConnected = true
		return false
	}
	return true
}

// forwardToPane applies the owner's notice policy: everything the listener has
// to say about EVENTS reaches the member, and its own transport chatter does not
// — except the three notices the ruling names, because 「斷線 → 沉默」 cannot
// tell 還在重試 from 已經放棄.
//
// 🔴 THE PREFIX IS MATCHED AT COLUMN 0, NOT AFTER A TRIM. Chat bodies reach this
// writer INDENTED, so trimming first made a message whose text happens to quote
// a transport line — which is exactly what members paste at each other while
// working on this feature — read as one of this binary's own notices and be
// swallowed, silently and permanently (the chat was already receipted as read).
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

func (w *paneWriter) deliver(payload string) {
	_ = w.run("-L", w.socket, "set-buffer", "-b", w.buffer, payload)
	if err := w.run("-L", w.socket, "paste-buffer", "-t", w.session, "-b", w.buffer, "-d", "-p"); err != nil {
		_ = w.run("-L", w.socket, "paste-buffer", "-t", w.session, "-b", w.buffer)
	}
	for attempt := 0; attempt < paneEnterAttempts; attempt++ {
		_ = w.run("-L", w.socket, "send-keys", "-t", w.session, "Enter")
		w.sleep(paneEnterSettle)
	}
}
