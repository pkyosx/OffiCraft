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

	// stamp decides which clock the next transcript line reports; dispatch
	// parks the current frame's server ts on it (T-7fb2, listen_stamp.go).
	stamp *eventStamper

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

	// DISCONNECT-NOTICE POLICY (owner, 2026-08-30): 「第一次斷線，跟連線回來的
	// 時候發訊息給 agent，中間的 retry 我們不需要降低頻率，但是不需要打攪 agent」.
	// These three fields carry the whole of it; the BACKOFF is deliberately not
	// among them, because the ruling is about the transcript and not about the
	// cadence — see noteDisconnect.
	inOutage    bool   // an outage has been announced and has not yet ended
	sawConnect  bool   // at least one connection has been established this process
	lastStation string // the station sha the previous connection reported ("" = never known)

	cursorPath string
	winddown   *windDownHook
	recycle    *recycleHook
	// drainWarn holds the drain's two DIFFERENT latches — a mark-read receipt
	// that did not land (once per PROCESS) and a total fetch fault (once per
	// OUTAGE, with a line when it clears). The field was called markWarn back
	// when there was only the first one; the second arrived and the name did
	// not follow, which is how a reader ends up believing there is one latch
	// with one lifetime. There are two, and their lifetimes differ on purpose —
	// see warnMarkReadFailed and noteChatFetchFault for which is which and why.
	//
	// It is NOT a chat ledger: what this listener has already surfaced is the
	// server's unread set and nothing else (T-48, rc-224dee5770dd).
	drainWarn *drainWarner
	// ack is the delivery gate for chat batches: non-nil ONLY when the parent
	// asked for acks (the codex sidecar). nil ⇒ a printed line counts as
	// delivered, which is the claude path and must stay byte-for-byte as it was.
	ack       *ackGate
	replySeen *replyCardSeen      // persisted answered-card dedup (drain + live delta)
	taskSnaps map[string]taskSnap // per-task last-seen state (the "what moved" diff)
	once      bool                // single-connect test hook (mirrors --once)
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

// agentLinePrefix opens EVERY line this binary writes into an agent's pane, and
// the sidecar's three prefix checks start their match at column 0 with it. It is
// a constant so the notice heads below can be spelled as whole prefixes.
const agentLinePrefix = "[ocagent] "

// The heads of the three transport notices the disconnect-notice policy (owner,
// 2026-08-30) says must reach the agent. THEY ARE THE CONTRACT WITH THE CODEX
// SIDECAR, which matches them with HasPrefix from column 0
// (cli/ocwarden/codex_session.go's notice*Prefix constants).
//
// 🔴 WHY THEY ARE CONSTANTS AND NOT INLINE STRINGS. Independent review shifted
// ONE of these heads rightward — `"listen: disconnected — "` →
// `"net listen: disconnected — "` — and both Go modules stayed green while every
// codex member went permanently silent about its transport. Nothing in the
// tests looked at column 0: they asked `strings.Contains`, which cannot see
// anything INSERTED in front. Naming the head once means the printf can no
// longer carry a head of its own, and listen_notice_contract_test.go requires
// the sidecar's copy of these same bytes to still exist on the other side.
const (
	noticeDisconnected = "listen: disconnected"
	noticeConnected    = "listen: connected"
	noticeGivingUp     = "listen: giving up"

	// noticeBatch is the END-OF-BATCH marker of the ack protocol (T-48), and it
	// is deliberately spelled with the same `listen:` head as the three notices
	// above — for the OPPOSITE reason. Those are carved OUT of the sidecar's
	// blanket transport filter so they reach the agent; this one must be
	// swallowed BY it, because it is a line the two processes say to each other
	// and not a line anybody should read. The head is what guarantees that: any
	// `[ocagent] listen: …` line the sidecar does not recognise is dropped, so a
	// marker can never become a turn on the model.
	noticeBatch = "listen: batch"
)

func (l *listener) logf(format string, args ...any) {
	fmt.Fprintf(l.out, agentLinePrefix+format+"\n", args...)
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

// ---------------------------------------------------------------------------
// the disconnect-notice policy (owner ruling, 2026-08-30)
//
//	「應該是在第一次斷線，跟連線回來的時候發訊息給 agent，中間的 retry 我們不需要
//	 降低頻率，但是不需要打攪 agent。」
//
// 🔴 THIS IS NOT A BACKOFF CHANGE, AND MUST NEVER BECOME ONE. An earlier
// proposal answered the same complaint by widening the retry interval; the
// owner replaced it with this. The loop keeps re-dialling at exactly the rhythm
// listenBackoffStart/listenBackoffCap already give it — a member is trying just
// as hard to get back online as it ever was. What changes is only how much of
// that effort is narrated INTO THE AGENT'S TRANSCRIPT, where every line is an
// interruption that costs a turn.
//
// Measured before this existed: one station changeover produced THREE lines for
// ONE event on a claude member — `stream ended`, `connect failed: unexpected
// status 502`, `connected`. The middle one is the whole complaint.
//
// 🔴 AND SILENCE MAY MEAN ONLY ONE THING. Reporting just the two endpoints
// leaves the transcript reading `斷線 → 沉默 → 連上`, and in that silence
// 「還在重試」 and 「已經放棄」 are indistinguishable — the agent cannot tell
// whether waiting is a plan or a mistake. So every exit from the retry loop
// prints too (stopRetrying); the silence between the two notices then means
// exactly one thing, and it is the good one.
// ---------------------------------------------------------------------------

// noteDisconnect announces an outage ONCE. The first failure of a run prints;
// every later failure inside the SAME uninterrupted outage is folded away. The
// dial itself already happened before this is called and happens again after —
// this function has no say in the cadence at all.
func (l *listener) noteDisconnect(format string, args ...any) {
	if l.inOutage {
		return // the agent has already been told; retries are its own business
	}
	l.inOutage = true
	// 🔴 THE ORIGIN SEGMENT MUST BE ON THIS LINE TOO, and this is the half that is
	// easy to miss. On a machine that is NOT the station host, an unconfigured
	// listener never connects at all — it dials the invented loopback address, is
	// refused, and the connect line above is never reached. This disconnect notice is
	// then the ONLY transport line that member will ever print, and without the
	// segment it reads as "the station is down" when the truth is "nobody told me
	// which station". Same debounce as everything else here: once per outage.
	l.logf(noticeDisconnected+" — "+format+
		baseAddressOrigin(l.cfg.BaseConfigured)+
		" (retrying on the same schedule, quietly; the next transport line you see "+
		"is either the reconnect or a give-up)", args...)
}

// stopRetrying prints the give-up line when the retry loop terminates while an
// outage is still open, and returns the process exit code (always 0 — listen
// degrades gracefully). Called at EVERY exit of run(): a loop that stops during
// an outage and says nothing turns the disconnect notice into a lie by omission.
// Exiting while CONNECTED prints nothing — nothing was ambiguous there.
func (l *listener) stopRetrying(reason string) int {
	if l.inOutage {
		l.inOutage = false
		l.logf(noticeGivingUp+" — %s. No further reconnect attempts from THIS "+
			"listener; I am NOT still retrying.", reason)
	}
	return 0
}

// stationVerdict answers 「是不是換了一台」 on the reconnect line, so the reader
// is told rather than left to diff two shas by eye. It is a pure function of
// (previous sha, this sha, is-this-the-first-connect) and returns the segment to
// APPEND — never a claim it cannot support:
//   - first connect of the process: "" (there is no previous station; claiming
//     "same" would be a fabrication and claiming "new" would fire on every boot).
//   - either side unknown: "" (a station that sent no sha cannot be compared;
//     the same rule the sha segment itself follows — nothing is invented).
//
// The wording deliberately avoids the bytes "[station", which the sha segment
// owns, so the two can never be confused by a reader or by a test.
func stationVerdict(prev, cur string, firstConnect bool) string {
	if firstConnect || prev == "" || cur == "" {
		return ""
	}
	if prev == cur {
		return " [same station]"
	}
	return " [new station — was " + prev + "]"
}

// baseAddressOrigin answers 「這個位址是誰決定的」 on the two transport lines a
// misconfigured listener actually reaches, and it is the whole of T-89.
//
// 🔴 WHAT WAS WRONG. loadConfig falls back to defaultBase when OC_BASE is unset,
// so cfg.Base is NEVER empty and the connect line printed the invented address in
// exactly the bytes a correctly-configured member prints. Loopback is not itself the
// tell: cli/ocwarden/testdata/golden_launch.txt line 1 is a spawn that EXPORTS
// OC_BASE=http://127.0.0.1:7755 explicitly, i.e. a member that was TOLD to use the
// address an unconfigured one would have invented. The failure was never a missing
// line — it was a line that looks completely normal. Nothing on it
// said the address had been invented, so a member joining whatever happens to be
// listening on 127.0.0.1:7755 was indistinguishable, in the transcript, from a
// member joining the station somebody chose for it.
//
// 🔴 WHAT THIS DELIBERATELY DOES NOT DO: refuse, exit, or shorten anything. The
// two shapes the earlier deferral in cmdListen offered — folding this into the
// debounced refusal policy, or making it a launch-time refusal — are BOTH refusals,
// and the owner's ruling (rc-55a969718c98, option [1]) is that this must not become
// one: a listener that exits is a member that goes quiet, and from outside a quiet
// member is indistinguishable from a dead one. Worse, the debounced refusal policy
// ENDS at selfTerminate(), which kills the tmux session the member lives in. So this
// is a segment of text and nothing else. It reads cfg and returns a string; it holds
// no state, cannot fail, and no control flow branches on it.
//
// 🔴 THE PREDICATE IS BaseConfigured, NOT `Base == defaultBase`. cli/CLAUDE.md:11
// states the rule: BaseConfigured records whether the FALLBACK WAS TAKEN, which is
// not the same question as what the resulting value looks like. A member on the
// station host legitimately sets OC_BASE to loopback, and comparing against
// defaultBase would accuse that member of guessing when it was told.
//
// Configured ⇒ "" ⇒ both lines are emitted byte-identical to what they were before
// this existed, which is what keeps this change invisible on every healthy machine.
func baseAddressOrigin(configured bool) string {
	if configured {
		return ""
	}
	return " [⚠ address GUESSED — OC_BASE is not set, so nobody chose this station]"
}

// dispatch is the bridge from ONE completed SSE data payload to the agent's downlink
// behaviour: parse → echo gate → topic demux. A chat delta drives an R7 refetch (never
// reads the payload); a reply_card delta refetches the card and wakes the session when
// MY card got answered; a task delta refetches the task and prints ONE readable line
// (task_no + title + what moved + who moved it); a member delta nudges the graceful
// hooks; a DIRECTED band frame (context-high / token-expiry / task-close) prints its server-composed
// message. A non-dict frame / any other topic is silently ignored.
//
// ECHO SUPPRESSION (spec/sse.md §2.3, T-f39c 方案 A — the client half): a delta whose
// `trigger` equals MY OWN member id is my own action bounced back down my own stream
// (an agent connection only ever receives frames addressed to itself, so trigger==self
// ⟺ recipient==self ∧ actor==self). Printing it burns transcript tokens on something
// this session just DID — drop it silently. Owner-/server-/other-member-triggered
// frames always process; a blank/absent trigger processes too (fail-open — an older
// server or an unknown actor must not lose wakes). Directed band frames carry no
// trigger and are untouched by construction.
//
// EXEMPTION ① (spec §2.3): the `member` topic is never suppressed — a member delta
// naming self is a lifecycle NUDGE for the hooks below (prints nothing by itself),
// and the self-requested recycle (restart_self, T-4c71) rides a SELF-triggered
// member delta whose recycle wake must still land.
//
// EXEMPTION ② — `chat` (c-75113935a255, ruled by the owner). A self-triggered chat
// delta now DOES drive the refetch. It still prints nothing: drainChat drops
// `sender == self` rows, so what this buys is not a line but a RECEIPT — the
// self-addressed note (in practice a hand-off a member writes to itself) is marked
// read at the moment it is written, on the same path as everybody else's mail,
// instead of waiting for whatever reconnect happens next.
//
// 🔴 ITS PRICE, PAID KNOWINGLY: every message this member sends to ITSELF now costs
// one extra GET /api/chat, which usually comes back empty or holding only that one
// row. One post fans exactly one chat frame to this listener (the audience is a
// SET, and self-send collapses sender and recipient into one member), so it is one
// refetch per self-sent message and not a multiple; the read receipt this drain
// files fans a `chat_read` frame, which this switch has no case for, so nothing
// cascades. The owner chose consistency between the two paths over that request.
//
// Suppression is unchanged for every OTHER topic, and that matters: without it a
// member starts receiving a delta for every reply card it answers and every task it
// moves — its own work read back to it, one line at a time.
func (l *listener) dispatch(payload []byte) {
	frame, _ := safeJSON(string(payload)).(map[string]any)
	// Every line printed below belongs to THIS frame, so it reports the
	// server's own ts rather than the moment this session got round to it
	// (T-7fb2). Cleared on the way out so connection-level lines fall back to
	// the local clock and say so.
	defer l.stamp.enter(frame)()
	topic, _ := frame["topic"].(string)
	trigger := frameTrigger(frame)
	if isSelfEcho(trigger, l.cfg.ID) && topic != memberTopic && topic != chatTopic {
		return // my own echo — recipient==self ∧ actor==self (spec §2.3)
	}
	switch topic {
	case chatTopic:
		// R7 HARD CONSTRAINT: the chat delta payload is convenience — NEVER merged.
		// The delta is a pure NUDGE ⇒ REFETCH /api/chat and print the unread-for-me.
		//
		// Same entrance as the connect drain, and there is nothing left to
		// decide at either: the drain prints the server's unread set, so a delta
		// that arrives while the connect drain is still in flight can at worst
		// print the same window twice — and it cannot even do that, because the
		// first of them receipts what it printed and takes it out of the unread
		// set.
		l.drainChatNow()
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
		// only: the listener keeps HOLDING the stream — BOTH hooks merely WAKE the
		// session with the server-composed 〈停止〉 notice, and the server-
		// dispatched warden kill is the real drop (see listen_hooks.go).
		l.winddown.maybeWindDown(frame)
		l.recycle.maybeRecycle(frame)
	default:
		// Directed band frames (context-high / token-expiry / task-close) carry a server-
		// composed message for THIS agent — print it so the Monitor brings it
		// into the transcript (spec/sse.md §6/§6.1/§8).
		if directedBandTopics[topic] {
			handleDirectedBand(frame, l.out)
			return
		}
		if shouldDispatch(frame) {
			handleEvent(frame, trigger, l.out)
		}
	}
}

// authoritativeRefusal names WHY a non-200 on /api/events is a STANDING "you
// must not be online here" — the only kind of failure that may accumulate
// toward the fail-closed self-terminate — or returns "" when it is not one.
//
// 🔴 THIS FUNCTION IS THE WHOLE TRADE-OFF, so it is worth being explicit about
// what it must NOT do. The listener's default for a non-200 is to reconnect with
// backoff forever, and that default is RIGHT for a server that is restarting,
// mid-deploy, briefly 5xx, or answering 401 because its secret is not loaded yet
// — killing the agent's tmux session in any of those cases would turn a blip
// into fleet-wide data loss. It is WRONG for exactly the refusals the server
// will keep making until someone intervenes:
//
//   - 409 — the zombie stop gate, or the dual-SSE single-session guard.
//   - 401 CARRYING X-OC-Auth-Refusal: agent-superseded — this member's newer
//     generation has reported waking, so this session's token is below the
//     member's credential floor (server authz.go agentIatFloorRefusal). The
//     floor only ever rises, so this can never resolve itself. Without this
//     arm the superseded session re-dials every ≤15s forever, holding a tmux
//     session and the model session under it, none of which the cockpit can
//     show (the member's presence belongs to the successor now) — the exact
//     orphan the 409 ladder exists to prevent, reached through a different
//     status code.
//
// 🔴 A BARE 401 IS DELIBERATELY NOT AUTHORITATIVE. Status alone cannot separate
// "I have been replaced" from "the server is having a moment" or "my token just
// expired", and guessing wrong in that direction kills healthy agents. Only the
// server knows which refusal it made, so only the server's own marker counts.
// Pinned in both directions: TestListener_SelfTerminatesWhenSupersededByANewerGeneration
// and TestListener_APlain401NeverTripsFailClosed.
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
		if reason := authoritativeRefusal(resp); reason != "" {
			// An AUTHORITATIVE pre-stream refusal — surfaced as the errSSERefused
			// sentinel so the run loop can fold it fail-closed. The (bounded)
			// body carries the server's reason for the honest log line.
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			_, _ = io.Copy(io.Discard, resp.Body)
			return false, false, false, fmt.Errorf("%w [%s]: %s",
				errSSERefused, reason, strings.TrimSpace(string(snippet)))
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, false, false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	// T-5b83: name the build we just attached to. A station version change
	// necessarily restarts the station and therefore drops every stream, so
	// this line ALREADY marks the moment of every changeover — it just never
	// said which commit. The sha rides the SSE response headers, so there is
	// no second request: a changeover reconnects the whole fleet at once, and
	// that is precisely the station's most fragile moment to be asked N times.
	//
	// 🔴 SUFFIXES ONLY, AND THE PREFIX DOES NOT MOVE. The codex sidecar reads this
	// line through THREE prefix checks (cli/ocwarden/codex_session.go), and pushing
	// those bytes rightward breaks all three. They are named by function rather than
	// by line number so they cannot drift — an earlier version of this note claimed
	// they ALREADY had, which was wrong: the two it listed were still accurate. What
	// was actually wrong with it is below, and it is worse than a stale number: one
	// of the three was missing from the list entirely.
	//   - actionableCodexListenerLine keeps transport chatter out of the model
	//     transcript by matching the SHORT prefix "[ocagent] listen:". Miss it and
	//     every connect and reconnect turns the raw line into a turn — at a
	//     changeover, for the whole fleet at once.
	//   - codexListenerActions opens the ONE post-boot wake (T-51b0) on the long
	//     prefix "[ocagent] listen: connected". Miss it and that wake never fires,
	//     silently, and nothing reports it.
	//   - handleListenerLine fires onConnect() telemetry on that same long prefix,
	//     on EVERY connect. This one was missing from this list entirely.
	// All three TrimSpace and then HasPrefix, so they read the head of the line
	// only: anything appended is safe, anything prepended breaks three things at
	// once. The long prefix implies the short one, so a test that pins the REAL
	// printed line covers all three. Two do (the station-sha test and the agent-sha
	// test); the four sidecar tests do not — they feed hand-written constants and
	// cannot see what this function actually prints, so they stay green through a
	// moved prefix.
	//
	// ⚠️ "AT THE END" IS NOT WHERE THESE LAND. cmdListen wraps out in a
	// stampWriter before the first print, which inserts " [ts=<float> local]"
	// ahead of the first newline — so the real transcript line ends with the
	// timestamp and everything added here sits to its LEFT. Confirmed against a
	// live listener, not just read: `… (⇒ online while held) [station da11eae8]
	// [ts=1787148244.692 local]`. Tests that use newTestListener see no stamp
	// because that helper writes past the wrapper.
	//
	// Absent header ⇒ the line is emitted byte-identical to what it was before
	// this change. Nothing is fabricated and no earlier value is reused: each
	// connection reads only its own response.
	station, stationSHA := "", ""
	if sha := strings.TrimSpace(resp.Header.Get(stationSHAHeader)); sha != "" {
		stationSHA = sha
		station = " [station " + sha + "]"
	}
	// ── AND WHICH OCAGENT IS SAYING IT ──────────────────────────────────────
	// The station sha above names the peer; this names the SPEAKER. They are
	// different clocks: the station changes when it is deployed, this process
	// changes only when it restarts. Measured on this fleet: a listener held one
	// inode while the file on disk turned over four times, so its behaviour was
	// four changeovers old and the connection line said nothing about it.
	//
	// Empty ⇒ THE SEGMENT IS NOT PRINTED AT ALL. An unstamped build (any plain
	// `go build`, every test binary) must produce the line byte-identical to what
	// it was before this change: no empty brackets, no placeholder, and nothing
	// carried over from another build — same rule the station segment follows.
	// TrimSpace, exactly like the station segment above: `-X main.buildSHA=` with
	// nothing after it, or a build script whose sha lookup produced whitespace,
	// must take the same not-known path as a stamp that was never applied. A
	// blank one printed as " [agent \t]" looks like an answer, and the two
	// segments disagreeing about what counts as absent is the kind of difference
	// nobody finds until they are reading a transcript for another reason.
	agent := ""
	if sha := strings.TrimSpace(buildSHA); sha != "" {
		agent = " [agent " + sha + "]"
	}
	// ── AND WHETHER IT IS THE SAME ONE AS LAST TIME ─────────────────────────
	// The sha above NAMES the peer; this SAYS WHAT CHANGED. Both segments were
	// already on the line before this — a reader who wanted to know whether a
	// changeover had happened had to scroll back to the previous connection and
	// compare two hex strings by eye, which nobody does. This is the owner's
	// second ask on the reconnect notice (2026-08-30) and it costs no request:
	// the comparison is against what this same process saw last time.
	//
	// ⚠️ POSITION: this sits BEFORE the sha segments, not after. The two
	// existing station-sha tests assert the line ENDS with " [station <sha>]"
	// (the agent segment is empty in any unstamped build, tests included), and
	// appending past them would break both. Anywhere after the prefix is equally
	// safe for the three sidecar prefix consumers, which read only the head.
	verdict := stationVerdict(l.lastStation, stationSHA, !l.sawConnect)
	l.sawConnect = true
	if stationSHA != "" {
		l.lastStation = stationSHA // an unknown sha never overwrites a known one
	}
	// The stream is up: whatever outage was being announced is over, and the
	// line below IS the second of the owner's two notices.
	l.inOutage = false
	// ⚠️ POSITION: the origin segment goes HERE, not at the end. Four tests assert
	// this line ENDS with " [station <sha>]" or " [agent <sha>]"
	// (listen_agent_sha_test.go:62,106,138; listen_test.go:2726,2796,2828), so a
	// trailing segment would break them. Anywhere after the head is equally safe for
	// the three sidecar prefix consumers, which read column 0 only — and it belongs
	// beside the address it is talking about rather than after two shas.
	l.logf(noticeConnected+" — streaming %s%s%s (⇒ online while held)%s%s%s",
		l.cfg.Base, eventsPath, baseAddressOrigin(l.cfg.BaseConfigured), verdict, station, agent)

	// Connect drain: /api/events has no replay, so any reply_card delta
	// fanned while this listener held no stream is lost — catch up from the
	// answered-pane authority NOW, before the live stream takes over (the
	// shared seen state collapses a drain/delta race to one printed line).
	drainReplyCards(l.api, l.cfg, l.replySeen, l.out)
	// …and CHAT, for exactly the same reason. A chat message fanned during the
	// outage window is gone from the stream too, and the only thing that used to
	// surface it was the NEXT chat delta — so if nobody spoke again, the agent
	// was never told anyone had called. Draining here, before the live stream
	// takes over, is what makes "reconnected" mean "caught up".
	//
	// 🔴 THIS IS THE ONLY SCHEDULED CHAT DRAIN — there is none at process start
	// (owner, 2026-09-02). Including the FIRST one of the process: a virgin
	// machine's very first backfill happens right here, and it PRINTS (T-48 —
	// the old silent baseline is gone with the local ledger). Tying it to the
	// connect is what keeps an inbox from being printed into a session that is
	// about to receive no events at all.
	//
	// This cannot re-print history: whichever drain surfaced a line receipted it,
	// which took it out of the server's unread set, so a re-drain of the same
	// window fetches nothing.
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
		onID:       func(id string) { writeCursor(l.cursorPath, id) },
		onComment:  l.foldProbe, // heartbeat-line self-exit probe point #2
	}
	err = scanSSE(resp.Body, sink)
	if errors.Is(err, errSelfExit) {
		return true, activity, true, err
	}
	return true, activity, false, err
}

// drainChatNow is THE way this listener drains chat. Both entrances go through
// it: every connect/reconnect (connectOnce, before the live stream takes over)
// and every inbound chat delta (dispatch). There is deliberately no third one at
// process start — see run().
//
// 🔴 NOBODY DECIDES SILENCE ANY MORE, BECAUSE THERE IS NOTHING TO DECIDE. The
// old drain took a `silent` flag fed from a persisted local ledger, so that a
// machine's first listen swallowed the whole inbox without printing it. That
// ledger is gone (owner, rc-224dee5770dd): the server's unread set is the only
// answer to "have I seen this", the drain prints everything in it, and the read
// receipt is the only thing that takes a row out of it. A caller therefore has
// no answer left to pass in and no way to get it wrong.
func (l *listener) drainChatNow() int {
	if l.drainWarn == nil {
		l.drainWarn = &drainWarner{}
	}
	return drainChat(l.api, l.cfg, l.out, l.drainWarn, l.ack)
}

// run is the always-online listen loop. It blocks until ctx is cancelled or a self-exit
// fires, re-dialing whenever the stream drops (exponential + jittered backoff, floor
// reset when a healthy connection dropped). Returns the process exit code (always 0 —
// listen degrades gracefully; a mis-wire / self-exit / signal is a clean 0). Mirrors
// cmd_listen.
func (l *listener) run(ctx context.Context) int {
	// 🔴 NOTHING IS DRAINED HERE. Chat catch-up hangs off the CONNECT, not off
	// process start (owner, 2026-09-02:「啟動的時候好像不用做，就連上 SSE 的
	// 時候統一做就好，包含 codex 應該也是類似的機制」).
	//
	// A boot drain runs in exactly one state the connect drain does not cover:
	// the API answers but the stream will not open. That state is BROKEN, and
	// printing an inbox into a session that is about to receive no events is not
	// a rescue — it makes a machine that cannot hear anything look like one that
	// is working, which is the failure this codebase keeps paying for. The one
	// window a boot drain legitimately covered (messages fanned while this agent
	// held no stream) is covered by the connect drain anyway, moments later and
	// only once the session can actually act on what follows.
	//
	// Everything the boot drain used to be responsible for now happens in
	// connectOnce: the connect drain fetches the server's unread set and prints
	// all of it, so a listener that never got a stream also never reads — and
	// never receipts — a single line.
	backoff := l.backoffStart
	for {
		if ctx.Err() != nil {
			return l.stopRetrying("this process is shutting down")
		}
		// Lifecycle tie, probe point #1: never (re)connect an orphan.
		if l.foldProbe() {
			return l.stopRetrying("the session probe says I should no longer be here")
		}

		opened, activity, selfExit, err := l.connectOnce(ctx)
		if selfExit {
			// the heartbeat-line probe self-exited (probe point #2)
			return l.stopRetrying("the server told this listener to stand down")
		}
		if ctx.Err() != nil {
			// cancelled while connected/dialing → clean exit
			return l.stopRetrying("this process is shutting down")
		}
		if opened {
			if activity {
				backoff = l.backoffStart // a byte proved health → reconnect fast
			}
			l.resetRefusals() // an opened stream breaks any refusal run
			l.noteDisconnect("stream ended: %v", err)
		} else if errors.Is(err, errSSERefused) {
			l.noteDisconnect("connect refused: %v", err)
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
				// 🔴 SAY IT BEFORE THE KILL, NOT AFTER. selfTerminate kills my own
				// tmux session, and suicide.go states outright that a successful
				// kill SIGHUPs this process and never returns — so a give-up line
				// printed after it is dead code on the path that matters most.
				// seeds/boot_sequence.md promises the whole fleet that the absence
				// of that line means「還在試」; on this path that promise was false,
				// and the member left behind reads an unfinished outage as a retry
				// still in flight. Printing first costs nothing and the ordering is
				// the entire content of the fix.
				rc := l.stopRetrying("the server refused this listener authoritatively " +
					"for the whole grace window")
				if l.selfTerminate != nil {
					l.selfTerminate()
				}
				return rc
			}
		} else {
			// A network fault / non-409 status (server down, 5xx, …) is NOT an
			// authoritative refusal — reset the run so a briefly-unavailable
			// server can never accumulate toward the fail-closed kill.
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
	// Wrap BEFORE the first print: the mis-wire notice below is a transcript
	// line like any other and must carry a time too (T-7fb2). Everything the
	// listener and its hooks print goes through this one writer, so a stamp can
	// never be forgotten at an individual call site.
	stamper := &eventStamper{clock: time.Now}
	out = &stampWriter{inner: out, stamp: stamper.suffix}

	// OC_BASE CLASSIFICATION: NOT GUARDED IN THIS PACKAGE, AND THIS IS THE
	// ONE CASE WHERE THAT IS A DEFERRAL RATHER THAN AN EXEMPTION. Say so plainly,
	// because listen is the subcommand with the MOST at stake here: cfg.Base is
	// what the SSE connection below is opened against, holding it is what makes
	// the server call this agent online, and nothing in this file asks whether
	// that address was ever configured. suicide and clean are exempt because they
	// reach no station at all; listen reaches one for as long as it runs.
	//
	// It was left alone for two reasons, neither of which is "it does not
	// matter":
	//   1. The refusal T-86 adds elsewhere is an exit. Here an exit is the
	//      failure mode itself — a member that stops holding its downlink is
	//      indistinguishable from a dead one — and this file's refusal policy is
	//      already a deliberate, debounced one (cli/CLAUDE.md §5: tmux gone twice,
	//      unknown eight times across ten minutes, 409 four times across two
	//      minutes). A new immediate refusal would sit outside that policy.
	//   2. T-86's own scope forbids changing what config_test.go pins about the
	//      mis-wire arm directly below.
	//
	// What it would take: a ruling on whether an unconfigured OC_BASE belongs in
	// the debounced refusal policy or is a launch-time refusal, which is a
	// question about this file's contract rather than about the guard T-86 added.
	if cfg.ID == "" || cfg.Token == "" {
		fmt.Fprint(out, "[ocagent] listen: no OC_ID/OC_TOKEN — nothing to do; exiting.\n")
		return 0
	}
	api := defaultHTTPClient()
	l := &listener{
		stamp:            stamper,
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
		winddown:         newWindDownHook(api, cfg, out),
		recycle:          newRecycleHook(api, cfg, out),
		drainWarn:        &drainWarner{},
		ack:              newAckGate(env, os.Stdin),
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
