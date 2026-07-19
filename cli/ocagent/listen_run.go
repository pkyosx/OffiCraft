package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// the listener — long-lived GET /api/events + reconnect/backoff + self-exit +
// topic dispatch. The struct-with-injectable-seams shape mirrors ocwarden's
// sseTransport so tests drive the WHOLE reconnect + dispatch + self-exit path
// against an httptest server with NO real sleep / network / tmux.
// ---------------------------------------------------------------------------

type listener struct {
	cfg          Config
	api          httpClient   // short-timeout client for chat/presence/member refetch
	streamClient *http.Client // long-lived (Timeout 0); the SSE downlink only

	sleep           func(time.Duration) // injectable; real is time.Sleep
	backoffStart    time.Duration
	backoffCap      time.Duration
	idleReadTimeout time.Duration // 0 disables the idle-read watchdog
	jitter          func() float64
	out             io.Writer

	probe func() probeVerdict // session-alive probe; nil ⇒ self-exit disabled
	miss  int                 // consecutive session-GONE probes (shared debounce)

	// FAIL-CLOSED bookkeeping (zombie defence line B; see the listen.go consts).
	clock            func() time.Time // injectable; real is time.Now
	unknowns         int              // consecutive cannot-probe verdicts
	firstUnknownAt   time.Time        // start of the current unknown run
	probeUnknownSpan time.Duration    // wall-clock bound for the unknown run
	refusals         int              // consecutive server 409 refusals
	firstRefusalAt   time.Time        // start of the current refusal run
	refusalGraceSpan time.Duration    // wall-clock bound for the refusal run
	selfTerminate    func()           // kill my own tmux session (default: `ocagent suicide`)

	cursorPath string
	winddown   *windDownHook
	recycle    *recycleHook
	seen       map[string]bool     // id-keyed unread cursor for drain_chat
	replySeen  *replyCardSeen      // persisted answered-card dedup (drain + live delta)
	taskSnaps  map[string]taskSnap // per-task last-seen state (the "what moved" diff)
	once       bool                // single-connect test hook (mirrors --once)
}

// newSSEStreamClient builds the long-lived HTTP client for the SSE downlink. Timeout
// is 0 (NO overall deadline) — an SSE connection stays open indefinitely; only
// connection SETUP (dial / TLS / response headers) is bounded, never the streaming
// body. Copy-twin of ocwarden newSSEClient.
func newSSEStreamClient() *http.Client {
	return &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: listenDialTimeout}).DialContext,
			TLSHandshakeTimeout:   listenDialTimeout,
			ResponseHeaderTimeout: listenHeaderTimeout,
		},
	}
}

func (l *listener) logf(format string, args ...any) {
	fmt.Fprintf(l.out, "[ocagent] "+format+"\n", args...)
}

// foldProbe runs ONE session-existence probe and folds its tri-state verdict
// into the self-exit debounce; returns true when the listener must self-exit.
// probe==nil ⇒ disabled ⇒ never. GONE trips fast (sessionMissLimit consecutive
// misses — the session provably no longer exists). UNKNOWN (cannot probe) is
// FAIL-CLOSED but wide: it never fast-kills (a flaky probe must not kill a
// healthy listener; unknown even resets the GONE debounce), yet once the
// session has been unverifiable for probeUnknownMin consecutive probes AND
// probeUnknownSpan of wall clock, the listener self-exits — the old fail-open
// "cannot probe ⇒ alive forever" is exactly how a zombie lived forever. A
// probe that PANICS folds as UNKNOWN. Mirrors _fold_session_probe +
// note_session_probe, hardened fail-closed.
func (l *listener) foldProbe() bool {
	if l.probe == nil {
		return false
	}
	verdict := probeUnknown
	func() {
		defer func() { _ = recover() }() // a panicking probe ⇒ UNKNOWN (never an instant verdict)
		verdict = l.probe()
	}()
	switch verdict {
	case probeAlive:
		l.miss = 0
		l.unknowns = 0
		l.firstUnknownAt = time.Time{}
		return false
	case probeGone:
		l.unknowns = 0
		l.firstUnknownAt = time.Time{}
		l.miss++
		if l.miss >= sessionMissLimit {
			l.logf("listen: tmux session gone (%d consecutive misses) — self-exiting so "+
				"no orphan holds the SSE.", l.miss)
			return true
		}
		return false
	default: // probeUnknown
		l.miss = 0 // an unverifiable probe is never evidence the session is GONE
		now := l.clock()
		if l.unknowns == 0 {
			l.firstUnknownAt = now
		}
		l.unknowns++
		if l.unknowns >= probeUnknownMin && now.Sub(l.firstUnknownAt) >= l.probeUnknownSpan {
			l.logf("listen: session unverifiable for %d consecutive probes over %s — "+
				"fail-closed self-exit (an unprobeable listener must not hold the SSE forever).",
				l.unknowns, now.Sub(l.firstUnknownAt).Round(time.Second))
			return true
		}
		return false
	}
}

// foldRefusal folds ONE authoritative server refusal (pre-stream 409) into the
// fail-closed counter; returns true when BOTH bounds are crossed (see the
// sseRefusal* consts) and the listener must self-terminate. resetRefusals is
// called on EVERY other connect outcome, so only an uninterrupted run of
// refusals ever trips.
func (l *listener) foldRefusal() bool {
	now := l.clock()
	if l.refusals == 0 {
		l.firstRefusalAt = now
	}
	l.refusals++
	return l.refusals >= sseRefusalMin && now.Sub(l.firstRefusalAt) >= l.refusalGraceSpan
}

func (l *listener) resetRefusals() {
	l.refusals = 0
	l.firstRefusalAt = time.Time{}
}

// dispatch is the bridge from ONE completed SSE data payload to the agent's downlink
// behaviour: parse → echo gate → topic demux. A chat delta drives an R7 refetch (never
// reads the payload); a reply_card delta refetches the card and wakes the session when
// MY card got answered; a task delta refetches the task and prints ONE readable line
// (task_no + title + what moved + who moved it); a member delta nudges the graceful
// hooks; a DIRECTED band frame (context-high / task-close) prints its server-composed
// message. A non-dict frame / any other topic is silently ignored.
//
// ECHO SUPPRESSION (spec/sse.md §2.3, T-f39c 方案 A — the client half): a delta whose
// `trigger` equals MY OWN member id is my own action bounced back down my own stream
// (an agent connection only ever receives frames addressed to itself, so trigger==self
// ⟺ recipient==self ∧ actor==self). Printing it burns transcript tokens on something
// this session just DID — drop it silently. Owner-/server-/other-member-triggered
// frames always process; a blank/absent trigger processes too (fail-open — an older
// server or an unknown actor must not lose wakes). Directed band frames carry no
// trigger and are untouched by construction. EXEMPTION (spec §2.3): the `member`
// topic is never suppressed — a member delta naming self is a lifecycle NUDGE for the
// hooks below (prints nothing by itself), and the self-requested recycle
// (restart_self, T-4c71) rides a SELF-triggered member delta whose handover-SOP wake
// must still land.
func (l *listener) dispatch(payload []byte) {
	frame, _ := safeJSON(string(payload)).(map[string]any)
	topic, _ := frame["topic"].(string)
	trigger := frameTrigger(frame)
	if isSelfEcho(trigger, l.cfg.ID) && topic != memberTopic {
		return // my own echo — recipient==self ∧ actor==self (spec §2.3)
	}
	switch topic {
	case "chat":
		// R7 HARD CONSTRAINT: the chat delta payload is convenience — NEVER merged.
		// The delta is a pure NUDGE ⇒ REFETCH /api/chat and print the unread-for-me.
		drainChat(l.api, l.cfg, l.seen, l.out, false)
	case replyCardTopic:
		// R7 again: the payload ({id, from, status}) only routes — the printed
		// answer comes from a refetch of GET /api/reply-cards/{id}.
		handleReplyCard(l.api, l.cfg, frame, l.replySeen, trigger, l.out)
	case taskTopic:
		// R7: the payload ({id, status, priority}) only routes — the readable
		// line comes from a refetch of GET /api/tasks/{id} diffed against the
		// last snapshot this listener saw.
		if l.taskSnaps == nil {
			l.taskSnaps = map[string]taskSnap{}
		}
		handleTaskEvent(l.api, l.cfg, frame, l.taskSnaps, trigger, l.out)
	case memberTopic:
		// Graceful self-stop (desired_state=offline) + recycle (desired_state=online ∧ refocus) are
		// mutually exclusive, so both are safe to call on every member delta. Side-effect
		// only: the listener keeps HOLDING the stream (wind-down's `suicide` / the
		// server-dispatched warden kill is the real drop; recycle merely WAKES the
		// session with the handover SOP — see listen_hooks.go).
		l.winddown.maybeWindDown(frame)
		l.recycle.maybeRecycle(frame)
	default:
		// Directed band frames (context-high / task-close) carry a server-
		// composed message for THIS agent — print it so the Monitor brings it
		// into the transcript (spec/sse.md §6/§8).
		if directedBandTopics[topic] {
			handleDirectedBand(frame, l.out)
			return
		}
		if shouldDispatch(frame) {
			handleEvent(frame, trigger, l.out)
		}
	}
}

// connectOnce dials GET /api/events (replaying from the persisted cursor via
// Last-Event-ID), and — on a 200 — streams the body through scanSSE until it ends.
// Returns (opened, activity, selfExit, err): opened is true once the 200 body is being
// read; activity is true if ANY line arrived (a byte proves the link healthy → the
// caller resets backoff, mirroring Python's per-line reset); selfExit is true when the
// heartbeat-line session probe fired errSelfExit. It NEVER returns until the stream
// ends or ctx is cancelled.
//
// IDLE-READ WATCHDOG: on an open 200 body it arms a per-connection deadline that resets
// on every arriving line; a lapse cancels the child context, aborting the blocking body
// Read so scanSSE returns an error and the caller reconnects. Copy-twin of ocwarden
// connectOnce; the child context isolates a watchdog trip to THIS connection.
func (l *listener) connectOnce(ctx context.Context) (opened, activity, selfExit bool, err error) {
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(connCtx, http.MethodGet, l.cfg.Base+eventsPath, nil)
	if err != nil {
		return false, false, false, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if l.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+l.cfg.Token)
	}
	// Last-Event-ID requests replay after the persisted cursor (pure optimisation).
	if cursor := readCursor(l.cursorPath); cursor != "" {
		req.Header.Set("Last-Event-ID", cursor)
	}

	resp, err := l.streamClient.Do(req)
	if err != nil {
		return false, false, false, err // dial / TLS / header timeout / ctx cancel
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusConflict {
			// An AUTHORITATIVE pre-stream refusal (the server's zombie stop gate
			// or the dual-SSE guard) — surfaced as the errSSERefused sentinel so
			// the run loop can fold it fail-closed. The (bounded) body carries
			// the server's reason for the honest log line.
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			_, _ = io.Copy(io.Discard, resp.Body)
			return false, false, false, fmt.Errorf("%w: %s",
				errSSERefused, strings.TrimSpace(string(snippet)))
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, false, false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	l.logf("listen: connected — streaming %s%s (⇒ online while held)", l.cfg.Base, eventsPath)

	// Boot/reconnect drain: /api/events has no replay, so any reply_card delta
	// fanned while this listener held no stream is lost — catch up from the
	// answered-pane authority NOW, before the live stream takes over (the
	// shared seen state collapses a drain/delta race to one printed line).
	drainReplyCards(l.api, l.cfg, l.replySeen, l.out)

	onAct := func() { activity = true }
	if l.idleReadTimeout > 0 {
		watchdog := time.AfterFunc(l.idleReadTimeout, cancel)
		defer watchdog.Stop()
		onAct = func() { activity = true; watchdog.Reset(l.idleReadTimeout) }
	}
	sink := sseSink{
		onActivity: onAct,
		onData:     l.dispatch,
		onID:       func(id string) { writeCursor(l.cursorPath, id) },
		onComment:  l.foldProbe, // heartbeat-line self-exit probe point #2
	}
	err = scanSSE(resp.Body, sink)
	if errors.Is(err, errSelfExit) {
		return true, activity, true, err
	}
	return true, activity, false, err
}

// run is the always-online listen loop. It blocks until ctx is cancelled or a self-exit
// fires, re-dialing whenever the stream drops (exponential + jittered backoff, floor
// reset when a healthy connection dropped). Returns the process exit code (always 0 —
// listen degrades gracefully; a mis-wire / self-exit / signal is a clean 0). Mirrors
// cmd_listen.
func (l *listener) run(ctx context.Context) int {
	// Boot BASELINE: advance the unread cursor to 'now' (silent refetch) so connecting
	// does NOT re-print the whole chat history — only NEW messages print thereafter.
	drainChat(l.api, l.cfg, l.seen, l.out, true)

	backoff := l.backoffStart
	for {
		if ctx.Err() != nil {
			return 0
		}
		// Lifecycle tie, probe point #1: never (re)connect an orphan.
		if l.foldProbe() {
			return 0
		}

		opened, activity, selfExit, err := l.connectOnce(ctx)
		if selfExit {
			return 0 // the heartbeat-line probe self-exited (probe point #2)
		}
		if ctx.Err() != nil {
			return 0 // cancelled while connected/dialing → clean exit
		}
		if opened {
			if activity {
				backoff = l.backoffStart // a byte proved health → reconnect fast
			}
			l.resetRefusals() // an opened stream breaks any refusal run
			l.logf("listen: stream ended: %v", err)
		} else if errors.Is(err, errSSERefused) {
			l.logf("listen: connect refused: %v", err)
			if l.foldRefusal() {
				// FAIL-CLOSED (zombie defence line B): the server has refused this
				// listener authoritatively for the whole grace window — I am a
				// zombie (stop in effect) or a stale dual-SSE twin. Self-terminate:
				// kill my OWN tmux session so claude + this listener + every child
				// drop together; a headless run (no OC_SESSION) degrades to just
				// exiting this loop — either way the reconnect hammering stops.
				l.logf("listen: server refused the SSE %d consecutive times over %s — "+
					"fail-closed: self-terminating instead of retrying forever "+
					"(a refused listener is a zombie, not a client with bad luck).",
					l.refusals, l.clock().Sub(l.firstRefusalAt).Round(time.Second))
				if l.selfTerminate != nil {
					l.selfTerminate()
				}
				return 0
			}
		} else {
			// A network fault / non-409 status (server down, 5xx, …) is NOT an
			// authoritative refusal — reset the run so a briefly-unavailable
			// server can never accumulate toward the fail-closed kill.
			l.resetRefusals()
			l.logf("listen: connect failed: %v", err)
		}

		if l.once {
			return 0 // single-connect test hook
		}
		if !sleepCtx(ctx, l.sleep, backoff) {
			return 0
		}
		backoff = nextBackoff(backoff, l.backoffStart, l.backoffCap, l.jitter())
	}
}

// sleepCtx sleeps d via the injectable seam, treating a cancelled ctx as an immediate
// stop (checked before AND after). Returns false when ctx is cancelled → the run loop
// exits. Copy-twin of ocwarden sleepCtx.
func sleepCtx(ctx context.Context, sleep func(time.Duration), d time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	sleep(d)
	return ctx.Err() == nil
}

// ---------------------------------------------------------------------------
// production wiring — cmdListen: the realMain entrypoint for `ocagent listen`.
// ---------------------------------------------------------------------------

// cmdListen implements `ocagent listen`. A mis-wire (no OC_ID/OC_TOKEN) prints one
// line + exits 0 quietly (mirrors cmd_listen). Otherwise it builds the production
// listener (long-lived SSE client, short-timeout API client, real backoff/jitter, the
// tmux session probe from OC_SESSION, the graceful hooks) and runs it under a
// signal-cancellable context so SIGINT/SIGTERM stops the stream cleanly. `once` is the
// single-connect flag (mirrors argparse --once). Always returns 0.
func cmdListen(cfg Config, env func(string) string, once bool, out io.Writer) int {
	if cfg.ID == "" || cfg.Token == "" {
		fmt.Fprint(out, "[ocagent] listen: no OC_ID/OC_TOKEN — nothing to do; exiting.\n")
		return 0
	}
	api := defaultHTTPClient()
	l := &listener{
		cfg:              cfg,
		api:              api,
		streamClient:     newSSEStreamClient(),
		sleep:            time.Sleep,
		backoffStart:     listenBackoffStart,
		backoffCap:       listenBackoffCap,
		idleReadTimeout:  listenIdleReadTimeout,
		jitter:           defaultJitter,
		out:              out,
		probe:            makeSessionProbe(env),
		clock:            time.Now,
		probeUnknownSpan: probeUnknownGrace,
		refusalGraceSpan: sseRefusalGrace,
		selfTerminate:    func() { cmdSuicide(cfg, env, out) },
		cursorPath:       cursorPath(cfg),
		winddown:         newWindDownHook(api, cfg, env, out),
		recycle:          newRecycleHook(api, cfg, out),
		seen:             map[string]bool{},
		replySeen:        loadReplyCardSeen(replyCardSeenPath(cfg)),
		taskSnaps:        map[string]taskSnap{},
		once:             once,
	}
	// Signal-driven root context: SIGINT/SIGTERM cancels ctx, and run() observes it to
	// shut down GRACEFULLY — no hard kill of an in-flight SSE read. In practice the
	// warden's tmux kill is what stops a spawned listener (SIGHUP to the session), but a
	// clean signal path keeps a foreground/manual run tidy.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return l.run(ctx)
}
