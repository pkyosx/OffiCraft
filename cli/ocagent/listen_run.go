package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type listener struct {
	cfg          Config
	api          httpClient
	streamClient *http.Client

	sleep           func(time.Duration)
	backoffStart    time.Duration
	backoffCap      time.Duration
	idleReadTimeout time.Duration
	jitter          func() float64
	out             io.Writer

	stamper *eventStamper

	probe func() probeVerdict
	miss  int

	clock             func() time.Time
	unknowns          int
	firstUnknownAt    time.Time
	probeUnknownGrace time.Duration
	refusals          int
	firstRefusalAt    time.Time
	refusalGrace      time.Duration
	selfTerminate     func()

	inOutage    bool
	sawConnect  bool
	lastStation string

	sseCursorPath string
	winddown      *windDownHook
	recycle       *recycleHook
	// Not a chat ledger (owner ruling rc-224dee5770dd): the server's unread set
	// is the only record of what this listener has surfaced.
	drainWarn *drainWarner
	// Non-nil only for the codex sidecar (OC_LISTEN_ACK). nil ⇒ a printed line
	// counts as delivered: the claude path, which must stay byte-for-byte as is.
	ack       *ackGate
	replySeen *replyCardSeen
	taskSnaps map[string]taskSnap
	once      bool
}

// Copy-twin of ocwarden newSSEClient.
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

const agentLinePrefix = "[ocagent] "

// Heads of the three transport notices the disconnect-notice ruling (below)
// sends to the agent. 🔴 They are the contract with the codex sidecar
// (cli/ocwarden/codex_session.go notice*Prefix, another Go module), which
// matches agentLinePrefix+head with HasPrefix at column 0 — review once shifted a
// head right and both modules stayed green (tests used strings.Contains). The only
// cross-check is bin/listen-notice-mirror-guard.py, which catches the two sides
// drifting apart, not both being wrong together.
const (
	noticeDisconnected = "listen: disconnected"
	noticeConnected    = "listen: connected"
	noticeGivingUp     = "listen: giving up"

	// End-of-batch marker of the ack protocol. Same `listen:` head for the
	// opposite reason: the sidecar DROPS any `[ocagent] listen:` line it does not
	// recognise, so this marker can never become a turn on the model.
	noticeBatch = "listen: batch"
)

func (l *listener) logf(format string, args ...any) {
	fmt.Fprintf(l.out, agentLinePrefix+format+"\n", args...)
}

// GONE trips fast (sessionMissLimit misses); UNKNOWN (cannot probe) never
// fast-kills — a flaky probe must not kill a healthy listener — yet is
// FAIL-CLOSED after probeUnknownMin probes AND probeUnknownGrace, because
// "cannot probe ⇒ alive" is how a zombie lived forever. Mirrors Python
// _fold_session_probe + note_session_probe.
func (l *listener) foldProbe() bool {
	if l.probe == nil {
		return false
	}
	verdict := probeUnknown
	func() {
		defer func() { _ = recover() }()
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
	default:
		l.miss = 0
		now := l.clock()
		if l.unknowns == 0 {
			l.firstUnknownAt = now
		}
		l.unknowns++
		if l.unknowns >= probeUnknownMin && now.Sub(l.firstUnknownAt) >= l.probeUnknownGrace {
			l.logf("listen: session unverifiable for %d consecutive probes over %s — "+
				"fail-closed self-exit (an unprobeable listener must not hold the SSE forever).",
				l.unknowns, now.Sub(l.firstUnknownAt).Round(time.Second))
			return true
		}
		return false
	}
}

func (l *listener) foldRefusal() bool {
	now := l.clock()
	if l.refusals == 0 {
		l.firstRefusalAt = now
	}
	l.refusals++
	return l.refusals >= sseRefusalMin && now.Sub(l.firstRefusalAt) >= l.refusalGrace
}

func (l *listener) resetRefusals() {
	l.refusals = 0
	l.firstRefusalAt = time.Time{}
}

// 🔴 Disconnect-notice ruling (owner, 2026-08-30): tell the agent at the first
// disconnect and at the reconnect, stay silent in between, and do NOT slow the
// retries — it is a transcript rule, never a backoff change. Every exit from the
// retry loop prints (stopRetrying), so the silence can only mean 「還在重試」.
func (l *listener) noteDisconnect(format string, args ...any) {
	if l.inOutage {
		return
	}
	l.inOutage = true
	// 🔴 The origin segment belongs here too: an unconfigured listener off the
	// station host dials the invented loopback, never connects, and this is the
	// only transport line it ever prints. ⚠️ It is an ARGUMENT, not format text:
	// a `%` in its wording would print `%!(NOVERB)` into agent transcripts.
	l.logf(noticeDisconnected+" — "+format+"%s"+
		" (retrying on the same schedule, quietly; the next transport line you see "+
		"is either the reconnect or a give-up)",
		append(append([]any{}, args...), baseAddressOrigin(l.cfg.BaseConfigured))...)
}

func (l *listener) stopRetrying(reason string) int {
	if l.inOutage {
		l.inOutage = false
		l.logf(noticeGivingUp+" — %s. No further reconnect attempts from THIS "+
			"listener; I am NOT still retrying.", reason)
	}
	return 0
}

// Avoids the bytes "[station", which the sha segment owns and the station-sha
// tests match on.
func stationVerdict(prev, cur string, firstConnect bool) string {
	if firstConnect || prev == "" || cur == "" {
		return ""
	}
	if prev == cur {
		return " [same station]"
	}
	return " [new station — was " + prev + "]"
}

// 🔴 The predicate is BaseConfigured (was the fallback TAKEN), never
// `Base == defaultBase` (cli/CLAUDE.md): loadConfig fills defaultBase so Base is
// never empty, and loopback alone is no tell — cli/ocwarden/testdata/
// golden_launch.txt exports OC_BASE=http://127.0.0.1:7755 on purpose.
// 🔴 Text only, never a refusal or exit (owner ruling rc-55a969718c98, option [1]).
func baseAddressOrigin(configured bool) string {
	if configured {
		return ""
	}
	return " [⚠ address GUESSED — OC_BASE is not set, so nobody chose this station]"
}

// Echo suppression (spec/sse.md §2.3): an agent connection only receives frames
// addressed to itself, so trigger == my id is my own action bounced back —
// dropped, or the member gets its own work read back. Blank trigger processes
// (fail-open: older servers must not lose wakes). Exempt:
//   - member: restart_self's recycle rides a SELF-triggered member delta.
//   - chat (owner ruling c-75113935a255): a self-sent note prints nothing
//     (drainChat drops sender == self) but is receipted as read right away.
//
// R7 (spec): every delta payload only routes; what is printed comes from a
// refetch, never from merging the payload.
func (l *listener) dispatch(payload []byte) {
	frame, _ := safeJSON(string(payload)).(map[string]any)
	defer l.stamper.enter(frame)()
	topic, _ := frame["topic"].(string)
	trigger := frameTrigger(frame)
	if isSelfEcho(trigger, l.cfg.MemberID) && topic != memberTopic && topic != chatTopic {
		return
	}
	switch topic {
	case chatTopic:
		l.drainChatNow()
	case replyCardTopic:
		handleReplyCard(l.api, l.cfg, frame, l.replySeen, trigger, l.out)
	case taskTopic:
		if l.taskSnaps == nil {
			l.taskSnaps = map[string]taskSnap{}
		}
		handleTaskEvent(l.api, l.cfg, frame, l.taskSnaps, trigger, l.out)
	case memberTopic:
		// Offline vs online intent: the two hooks are mutually exclusive.
		l.winddown.maybeWindDown(frame)
		l.recycle.maybeRecycle(frame)
	default:
		if directedBandTopics[topic] {
			handleDirectedBand(frame, l.out)
			return
		}
		if shouldDispatch(frame) {
			printWake(frame, trigger, l.out)
		}
	}
}

// Only a STANDING refusal may count toward the fail-closed self-terminate; for a
// restart, deploy, 5xx or a 401 while the secret is not loaded the right answer
// is to retry forever, and killing the tmux session would turn a blip into
// fleet-wide data loss. Standing:
//   - 409: the zombie stop gate or the dual-SSE single-session guard.
//   - 401 with X-OC-Auth-Refusal: agent-superseded — the member's credential
//     floor only rises (server authz.go agentIatFloorRefusal), so this never
//     resolves; without it the superseded session re-dials forever as an orphan.
//
// 🔴 A BARE 401 IS DELIBERATELY NOT AUTHORITATIVE: status alone cannot tell
// "replaced" from "server blip" or "token expired"; only the server's marker can.
func authoritativeRefusal(resp *http.Response) string {
	switch {
	case resp.StatusCode == http.StatusConflict:
		return "409 stop gate / dual-SSE guard"
	case resp.StatusCode == http.StatusUnauthorized &&
		strings.TrimSpace(resp.Header.Get(authRefusalHeader)) == refusalAgentSuperseded:
		return "401 superseded — a newer generation of this member has reported waking"
	default:
		return ""
	}
}

// selfExit comes from the heartbeat-line session probe (onComment → foldProbe),
// not from the server. Copy-twin of ocwarden connectOnce; resetting backoff on
// activity mirrors Python's per-line reset.
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
	// The server never reads Last-Event-ID (no replay).
	if cursor := readSSECursor(l.sseCursorPath); cursor != "" {
		req.Header.Set("Last-Event-ID", cursor)
	}

	resp, err := l.streamClient.Do(req)
	if err != nil {
		return false, false, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if reason := authoritativeRefusal(resp); reason != "" {
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			_, _ = io.Copy(io.Discard, resp.Body)
			return false, false, false, fmt.Errorf("%w [%s]: %s",
				errSSERefused, reason, strings.TrimSpace(string(snippet)))
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, false, false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	// The station sha rides the SSE response headers, not a second request: a
	// changeover reconnects the whole fleet at once, the station's most fragile
	// moment.
	//
	// 🔴 SUFFIXES ONLY; THE PREFIX DOES NOT MOVE. Three sidecar functions in
	// cli/ocwarden/codex_session.go TrimSpace+HasPrefix this line:
	// actionableCodexListenerLine ("[ocagent] listen:", keeps transport chatter
	// out of the model), codexListenerActions and handleListenerLine
	// ("[ocagent] listen: connected": the one post-boot wake, and onConnect
	// telemetry). The sidecar tests feed hand-written constants and stay green
	// through a moved prefix; only the station-sha and agent-sha tests pin the
	// real printed line.
	//
	// ⚠️ These segments are not the very end of the line: runListen's stampWriter
	// inserts " [ts=… local]" before the newline, to their right.
	station, stationSHA := "", ""
	if sha := strings.TrimSpace(resp.Header.Get(stationSHAHeader)); sha != "" {
		stationSHA = sha
		station = " [station " + sha + "]"
	}
	agent := ""
	if sha := strings.TrimSpace(buildSHA); sha != "" {
		agent = " [agent " + sha + "]"
	}
	// ⚠️ The verdict goes BEFORE the sha segments: the station-sha tests compare
	// the whole line and expect the sha segments last.
	verdict := stationVerdict(l.lastStation, stationSHA, !l.sawConnect)
	l.sawConnect = true
	if stationSHA != "" {
		l.lastStation = stationSHA
	}
	l.inOutage = false
	l.logf(noticeConnected+" — streaming %s%s%s (⇒ online while held)%s%s%s",
		l.cfg.Base, eventsPath, baseAddressOrigin(l.cfg.BaseConfigured), verdict, station, agent)

	drainReplyCards(l.api, l.cfg, l.replySeen, l.out)
	l.drainChatNow()

	onAct := func() { activity = true }
	if l.idleReadTimeout > 0 {
		watchdog := time.AfterFunc(l.idleReadTimeout, cancel)
		defer watchdog.Stop()
		onAct = func() { activity = true; watchdog.Reset(l.idleReadTimeout) }
	}
	sink := sseSink{
		onActivity: onAct,
		onData:     l.dispatch,
		onID:       func(id string) { writeSSECursor(l.sseCursorPath, id) },
		onComment:  l.foldProbe, // SSE comment lines are the server heartbeats
	}
	err = scanSSE(resp.Body, sink)
	if errors.Is(err, errSelfExit) {
		return true, activity, true, err
	}
	return true, activity, false, err
}

func (l *listener) drainChatNow() int {
	if l.drainWarn == nil {
		l.drainWarn = &drainWarner{}
	}
	return drainChat(l.api, l.cfg, l.out, l.drainWarn, l.ack, l.clock)
}

// Mirrors Python cmd_listen.
func (l *listener) run(ctx context.Context) int {
	// 🔴 No chat drain at process start — only on connect (owner ruling
	// 2026-09-02): a boot drain makes a machine whose stream never opens look healthy.
	backoff := l.backoffStart
	for {
		if ctx.Err() != nil {
			return l.stopRetrying("this process is shutting down")
		}
		if l.foldProbe() {
			return l.stopRetrying("the session probe says I should no longer be here")
		}

		opened, activity, selfExit, err := l.connectOnce(ctx)
		if selfExit {
			// the heartbeat-line probe self-exited
			return l.stopRetrying("the server told this listener to stand down")
		}
		if ctx.Err() != nil {
			return l.stopRetrying("this process is shutting down")
		}
		if opened {
			if activity {
				backoff = l.backoffStart
			}
			l.resetRefusals()
			l.noteDisconnect("stream ended: %v", err)
		} else if errors.Is(err, errSSERefused) {
			l.noteDisconnect("connect refused: %v", err)
			if l.foldRefusal() {
				l.logf("listen: server refused the SSE %d consecutive times over %s — "+
					"fail-closed: self-terminating instead of retrying forever "+
					"(a refused listener is a zombie, not a client with bad luck).",
					l.refusals, l.clock().Sub(l.firstRefusalAt).Round(time.Second))
				// 🔴 Give-up line BEFORE the kill: a successful selfTerminate SIGHUPs
				// this process and never returns (suicide.go), and
				// seeds/boot_sequence.md tells the fleet that no give-up line means
				// 「還在試」.
				rc := l.stopRetrying("the server refused this listener authoritatively " +
					"for the whole grace window")
				if l.selfTerminate != nil {
					l.selfTerminate()
				}
				return rc
			}
		} else {
			l.resetRefusals()
			l.noteDisconnect("connect failed: %v", err)
		}

		if l.once {
			return l.stopRetrying("--once was set: this run makes a single attempt")
		}
		if !sleepCtx(ctx, l.sleep, backoff) {
			return l.stopRetrying("this process is shutting down")
		}
		backoff = nextBackoff(backoff, l.backoffStart, l.backoffCap, l.jitter())
	}
}

// Copy-twin of ocwarden sleepCtx.
func sleepCtx(ctx context.Context, sleep func(time.Duration), d time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	sleep(d)
	return ctx.Err() == nil
}

// A mis-wire (no OC_ID/OC_TOKEN) exits 0 quietly, mirroring Python cmd_listen.
func runListen(cfg Config, env func(string) string, once bool, out io.Writer) int {
	// Wrap before the first print: the mis-wire line below must be stamped too.
	stamper := &eventStamper{clock: time.Now}
	out = &stampWriter{inner: out, stamp: stamper.suffix}

	// OC_BASE CLASSIFICATION: ANNOUNCED, NEVER REFUSED — the only subcommand in
	// that category: an unset OC_BASE is named on the connect and disconnect lines
	// (baseAddressOrigin), never refused.
	//
	// 🔴 Not refusing IS the owner ruling (rc-55a969718c98, option [1]): here a
	// refusal is an exit, and an exited member looks dead. The tests will not stop
	// a reversal — each checks one printed line, so a mutant that prints it and
	// then leaves stays green. Hence also not a warnMissingBase caller, and never will
	// be: that caller list is not the list of subcommands that care about OC_BASE.
	if cfg.MemberID == "" || cfg.Token == "" {
		fmt.Fprint(out, "[ocagent] listen: no OC_ID/OC_TOKEN — nothing to do; exiting.\n")
		return 0
	}
	return newListener(cfg, env, out, once, stamper).run(rootCtx())
}

// 🔴 A separate function so the wiring can be pinned: review set
// cfg.BaseConfigured to a constant inside runListen and every test that builds
// its listener directly stayed green while the feature was gone.
func newListener(cfg Config, env func(string) string, out io.Writer, once bool, stamper *eventStamper) *listener {
	api := defaultHTTPClient()
	return &listener{
		stamper:           stamper,
		cfg:               cfg,
		api:               api,
		streamClient:      newSSEStreamClient(),
		sleep:             time.Sleep,
		backoffStart:      listenBackoffStart,
		backoffCap:        listenBackoffCap,
		idleReadTimeout:   listenIdleReadTimeout,
		jitter:            defaultJitter,
		out:               out,
		probe:             makeSessionProbe(env),
		clock:             time.Now,
		probeUnknownGrace: probeUnknownGrace,
		refusalGrace:      sseRefusalGrace,
		selfTerminate:     func() { cmdSuicide(cfg, env, out) },
		sseCursorPath:     sseCursorPath(cfg),
		winddown:          newWindDownHook(api, cfg, out),
		recycle:           newRecycleHook(api, cfg, out),
		drainWarn:         &drainWarner{},
		ack:               newAckGate(env, os.Stdin),
		replySeen:         loadReplyCardSeen(replyCardSeenPath(cfg)),
		taskSnaps:         map[string]taskSnap{},
		once:              once,
	}
}

func rootCtx() context.Context {
	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	return ctx
}
