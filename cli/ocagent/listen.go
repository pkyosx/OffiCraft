package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// listen: ocagent listen  (the canonical SSE downlink — Plane A survival core)
// A faithful port of agent/oc_agent.py cmd_listen (+ its helpers parse_sse_line /
// should_dispatch / should_wind_down / handle_event / next_backoff / cursor_path /
// stream_events / make_session_probe / note_session_probe / drain_chat / fetch_chat)
// and the two graceful-lifecycle hooks WindDownHook / RecycleHook.
// ---------------------------------------------------------------------------
//
// WHAT listen IS: hold ONE long-lived GET /api/events SSE connection open. HOLDING
// that connection IS the agent's server-projected `online` (綠) — the server derives
// presence from the SSE-connection fact, so `listen` reports NO presence of its own
// (the phase=waking edge is reported via the MCP `set_member_presence` tool EARLY in
// the boot序 before listen attaches — killing the deaf-boot 假-online). Each
// downlink delta becomes a wake: a `chat` delta drives an R7 refetch of the
// authoritative /api/chat (the delta payload is NEVER trusted); a WORK delta
// (action/task) logs a liveness wake; a `member` delta naming me nudges the graceful
// self-stop hook (wakes the session with the offboard notice) and the recycle hook
// (WAKE-ONLY: prints the server's 〈停止〉 document for the session — see listen_hooks.go).
//
// SELF-EXIT (the lifecycle tie — the agent's OWN death signal): the listener IS the
// SSE holder, so a DEAD agent's orphaned listener would keep its SSE open forever,
// latching the agent falsely 'alive'. It polls whether its own tmux session
// (OC_SESSION) still exists and SELF-EXITS once it has been GONE for
// sessionMissLimit consecutive probes, folded at TWO points into ONE debounce: each
// (re)connect top + every heartbeat/comment line. No OC_SESSION ⇒ probing disabled;
// a probe fault reads as alive (a flaky probe must NEVER self-kill a healthy listener).
//
// SHARED-CODE NOTE (transport reuse): the SSE scanning + reconnect/backoff + idle-read
// watchdog mechanics are a COPY-TWIN of ocwarden/transport.go's proven primitives
// (scanSSE / connectOnce / newSSEClient / the AfterFunc watchdog / sleepCtx). We copy
// rather than import for the SAME reason config.go/http.go copy loadConfig/jwtSub:
// ocwarden is `package main`, its helpers are unexported and cannot be imported
// without first extracting ocwarden into a library (churn + risk on a landed,
// working binary). The twin diverges from warden's transport only where the AGENT's
// listen genuinely differs from the WARDEN's command reader: (1) a Last-Event-ID
// cursor (id: persistence + replay header) the warden has no use for; (2) topic
// dispatch is chat-refetch / member-hooks / wake-log, not command dispatch;
// (3) the session-probe SELF-EXIT the warden (a daemon) never does; (4) Python's
// jittered exponential backoff (start 1s, cap 15s) rather than warden's plain
// doubling. A future extraction into one shared `ocsse` module is a mechanical lift.
// Pure stdlib, zero third-party — matches both existing modules.

const (
	// eventsPath is the officraft SSE downlink (GET /api/events).
	eventsPath = "/api/events"

	// stationSHAHeader is the response header the station stamps its build sha
	// onto when the SSE stream opens (T-5b83). Reading it costs no request of
	// its own, which is the whole point: a station version change restarts the
	// station and reconnects the entire fleet at once, so a client that asked
	// for the version separately would ask N times at the worst possible
	// moment.
	//
	// 🔴 THIS STRING IS HALF OF A CROSS-MODULE CONTRACT and the modules cannot
	// import each other. The other half is sseStationSHAHeader in
	// server/ocserverd/api_infra.go. A typo does NOT fail loudly — Header.Get
	// simply returns "" and the connection line silently drops the sha, which
	// is byte-identical to the honest "this station did not send one". The two
	// halves drift apart, nothing turns red on its own — see the task note for
	// the guard this still owes.
	stationSHAHeader = "X-Officraft-Station-Sha"
	// chatPath / membersPath are the R7 refetch authorities.
	membersPath = "/api/members/"
	// Backoff mirrors agent/oc_agent.py _BACKOFF_START / _BACKOFF_CAP (1s / 15s) — the
	// self-heal cadence. Python jitters each delay by a factor in [0.5, 1.0] to de-sync
	// a fleet reconnecting in lockstep after a server restart (thundering-herd).
	listenBackoffStart = 1 * time.Second
	listenBackoffCap   = 15 * time.Second

	// sessionMissLimit mirrors SESSION_MISS_LIMIT: consecutive session-gone probes
	// before self-exit (a single blip must NEVER self-kill a healthy listener).
	sessionMissLimit = 2

	// FAIL-CLOSED bounds (zombie-agent defence line B). The old behaviour was
	// fail-open: a probe that COULD NOT run read as alive forever, and a server
	// that kept refusing the SSE was retried forever — either way a zombie
	// listener lived (and projected a dead agent online) indefinitely.
	//
	// probeUnknownMin/probeUnknownGrace bound the "cannot probe" state (tmux
	// unresolvable / probe fault): only when the session's existence has been
	// UNVERIFIABLE for at least probeUnknownMin consecutive probes AND
	// probeUnknownGrace of wall clock does the listener self-exit — a transient
	// exec/PATH hiccup can never kill a healthy listener, a permanently
	// unverifiable one eventually does die (fail-closed).
	probeUnknownMin   = 8
	probeUnknownGrace = 10 * time.Minute

	// sseRefusalMin/sseRefusalGrace bound the server-refusal state: the server
	// answering /api/events with a pre-stream 409 (the zombie stop gate or the
	// dual-SSE guard) is an AUTHORITATIVE "you must not be online here". Only
	// after sseRefusalMin CONSECUTIVE refusals spanning at least
	// sseRefusalGrace does the listener fail-closed (self-terminate, killing
	// its own tmux session). Any other outcome — a successful stream, a network
	// fault, a 5xx, a brief server outage — RESETS the run, so a flapping or
	// briefly-down server can never mass-kill healthy agents; only a standing
	// refusal (zombie, stale dual-SSE twin) crosses both bounds.
	//
	// The grace was sized to mirror the server's 120 s stop_grace. 🔴 That is no
	// longer what it mirrors: 下線 runs no clock at all since T-a9d6, and the
	// server's stop gate now lets a session that is still WORKING its offboard
	// sequence reconnect (api_infra.go) — precisely so this ladder cannot take
	// down an agent mid-hand-off, which is what it would otherwise do on a
	// station upgrade or a network blip. The number stays as a bound on how long
	// a genuine standing refusal is tolerated; it is no longer derived from
	// anything.
	sseRefusalMin   = 4
	sseRefusalGrace = 120 * time.Second

	// Connection-setup bounds (NEVER the long-lived body stream — a body deadline would
	// guillotine the always-open SSE every N seconds). Values match ocwarden/transport.
	listenDialTimeout   = 10 * time.Second
	listenHeaderTimeout = 30 * time.Second
	// listenIdleReadTimeout is the idle-read watchdog: officraft emits a `: heartbeat`
	// keepalive ~every 15s; if NOTHING arrives within this window the connection is
	// presumed silently-dead / half-open and force-dropped into the reconnect path
	// (~3× the heartbeat interval — ample slack for a healthy link, still catches a truly
	// deaf connection inside a minute). Tests inject a small value to fire fast.
	listenIdleReadTimeout = 45 * time.Second
	// maxSSELine caps a single SSE line so an adversarial unbounded line ends the stream
	// (→ reconnect) rather than growing memory without bound.
	maxSSELineListen = 8 << 20 // 8 MiB

	// defaultTmuxSocket mirrors agent/spawn.py DEFAULT_SOCKET.
	defaultTmuxSocket = "officraft"

	// chatTopic / memberTopic / desiredOffline / desiredOnline mirror the wire
	// literals.
	chatTopic      = "chat"
	memberTopic    = "member"
	desiredOffline = "offline"
	desiredOnline  = "online"

	// replyCardTopic / replyCardsPath: the reply-card (等我回覆卡) downlink. A
	// reply_card delta is a NUDGE; GET /api/reply-cards/{id} is the refetch
	// authority (spec/sse.md §2.2).
	replyCardTopic    = "reply_card"
	replyCardsPath    = "/api/reply-cards/"
	replyCardAnswered = "answered"
	replyCardExpired  = "expired"

	// contextHighTopic / tokenExpiryTopic / taskCloseTopic mirror the server's
	// directed band topic constants (server/ocserverd/sse_bands.go; spec/sse.md
	// §6/§6.1/§8).
	contextHighTopic = "context-high"
	tokenExpiryTopic = "token-expiry"
	taskCloseTopic   = "task-close"

	// taskTopic / tasksPath: the task delta downlink. A task delta is a NUDGE;
	// GET /api/tasks/{id} is the refetch authority (spec/sse.md §2.2).
	taskTopic = "task"
	tasksPath = "/api/tasks/"

	// messageBodyValve is the anti-blowup SAFETY VALVE — NOT a regular preview
	// cap — for a MESSAGE-type event's body (chat body + reply-card
	// summary/answer text). MESSAGE-type events are content addressed to THIS
	// agent: the agent will NECESSARILY read them. ocagent has already refetched
	// the whole message over REST before printing it, so a one-line preview does
	// not save context — the full body reaches the agent regardless (via
	// get_chat / get_reply_card). Truncating a must-read message is therefore
	// PURE LOSS: the same body lands in context anyway, PLUS the wasted preview
	// bytes, PLUS one MCP round-trip whose response re-inflates the body 2–5×
	// with a JSON envelope (T-4272). So message-type bodies print IN FULL with
	// NO regular threshold — verbatim, multi-line preserved.
	//
	// Truncation only ever pays off for an event the recipient will NOT read
	// (an FYI — someone else's task-status change, etc.); those keep their terse
	// preview (see previewLine callers, e.g. the task title) and are untouched
	// here. The valve below is the ONE guard T-f39c's anti-flood intent survives
	// as: it fires only at a PATHOLOGICAL size (a whole file / log pasted into
	// chat), never in normal use.
	//
	// 64 KiB (bytes, not runes — a hard cap independent of script), chosen as a
	// "tens of KB" valve:
	//   • prints EVERY realistic must-read message whole — an ordinary CJK chat
	//     of a few hundred chars is ~1–2 KiB; a 5,000-char message (the size the
	//     owner named as must-print) is ~15 KiB; even a 20k-char handover SOP is
	//     ~60 KiB — all under 64 KiB;
	//   • still 128× below the SSE single-line transport ceiling (8 MiB,
	//     maxSSELineListen), so a full-body print never nears the wire limit;
	//   • only a genuinely pathological paste (tens of thousands of chars) trips
	//     it, which is exactly the blowup the valve exists to stop.
	messageBodyValve = 64 << 10 // 64 KiB — anti-blowup safety valve, NOT a regular cap

	// chatBodyAuthority / replyCardBodyAuthority name the MCP read a truncated
	// message points the agent at for the full text (T-4272 截斷提示).
	chatBodyAuthority      = "get_chat"
	replyCardBodyAuthority = "get_reply_card"
)

// dispatchTopics mirrors DISPATCH_TOPICS: a delta on these WORK topics is a liveness
// WAKE only (chat is deliberately NOT here — a chat delta drives an R7 refetch).
var dispatchTopics = map[string]bool{"action": true, "task": true}

// errSelfExit is the sentinel scanSSE returns when a comment/heartbeat line's session
// probe reports the tmux session GONE — the run loop treats it as "self-exit, do NOT
// reconnect", distinct from an ordinary stream drop (which reconnects).
var errSelfExit = errors.New("listen: tmux session gone — self-exit")

// authRefusalHeader / refusalAgentSuperseded mirror the server's marker for the
// ONE 401 that is a standing refusal rather than transient bad luck: the agent
// credential floor (server authz.go). Copies of the server's constants — this is
// a separate module, and the wire literal is the contract between them.
const (
	authRefusalHeader      = "X-OC-Auth-Refusal"
	refusalAgentSuperseded = "agent-superseded"
)

// errSSERefused is the sentinel connectOnce wraps an AUTHORITATIVE pre-stream
// refusal in — a 409 (the zombie stop gate, or the dual-SSE single-session
// guard) or a 401 marked agent-superseded (this member's newer generation has
// reported waking). See authoritativeRefusal for why status alone is not enough
// for the 401 half. The run loop folds consecutive refusals into the
// fail-closed self-terminate (see sseRefusalMin/sseRefusalGrace).
var errSSERefused = errors.New("listen: server authoritatively refused the SSE connection")

// ---------------------------------------------------------------------------
// SSE line framing — copy-twin of ocwarden scanSSE, extended with id + comment
// hooks the agent needs (cursor persistence + heartbeat-line self-exit probe).
// ---------------------------------------------------------------------------

// sseSink is the per-event callback set scanSSE drives.
type sseSink struct {
	// onActivity fires ONCE per successfully-read line (data / id / blank boundary /
	// heartbeat comment) — the "a frame arrived → the link is alive" signal the idle
	// watchdog resets its deadline on, and the byte-proves-healthy backoff reset.
	onActivity func()
	// onData fires once per completed event with the event's concatenated `data`
	// payload (multiple data: lines within one event joined by \n, SSE spec).
	onData func([]byte)
	// onID fires per `id:` line with the trimmed value — the Last-Event-ID cursor
	// persistence point (officraft frames `id: <seq>` then `data: <json>`).
	onID func(string)
	// onComment fires per `:`-prefixed comment/heartbeat line; returning true STOPS
	// the scan with errSelfExit (the heartbeat-line self-exit probe point #2).
	onComment func() bool
}

// scanSSE reads Server-Sent-Events from r, driving sink per the parts of the SSE line
// protocol officraft emits: `\n`-separated lines (CRLF tolerated); a BLANK line is
// the event boundary → accumulated data dispatched; a `:` line is a comment/keepalive;
// `field: value` strips ONE leading space after the colon; `data:` lines join with \n;
// `id:` feeds the cursor; every other field (event/retry/…) is ignored. An incomplete
// final event (EOF before the blank boundary) is DISCARDED per spec. Returns on EOF /
// read error (that error) or errSelfExit (onComment stop), never panicking.
func scanSSE(r io.Reader, sink sseSink) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxSSELineListen)
	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		payload := strings.Join(data, "\n")
		data = data[:0]
		if sink.onData != nil {
			sink.onData([]byte(payload))
		}
	}
	for sc.Scan() {
		if sink.onActivity != nil {
			sink.onActivity() // a line arrived → the link is alive
		}
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" { // event boundary
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") { // comment / keepalive
			if sink.onComment != nil && sink.onComment() {
				return errSelfExit
			}
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue // a valueless field name → ignored (none of ours are valueless)
		}
		value = strings.TrimPrefix(value, " ") // strip ONE leading space (SSE spec)
		switch field {
		case "data":
			data = append(data, value)
		case "id":
			if sink.onID != nil {
				sink.onID(strings.TrimSpace(value))
			}
		}
		// event / retry / anything else → ignored.
	}
	return sc.Err()
}

// ---------------------------------------------------------------------------
// backoff — Python next_backoff: exponential, capped, with full jitter.
// ---------------------------------------------------------------------------

// nextBackoff mirrors agent/oc_agent.py next_backoff: double `current` (floored at
// start), clamp to cap, then multiply by a jitter factor `jf` in [0.5, 1.0]. The
// jittered result is BOTH the next sleep AND the next `current` (so the delay drifts,
// matching Python). jf is injected so the cadence is unit-testable without randomness.
func nextBackoff(current, start, capd time.Duration, jf float64) time.Duration {
	base := current
	if base < start {
		base = start
	}
	doubled := base * 2
	if doubled > capd {
		doubled = capd
	}
	return time.Duration(float64(doubled) * jf)
}

// defaultJitter draws a factor in [0.5, 1.0) — Python random.uniform(0.5, 1.0).
func defaultJitter() float64 { return 0.5 + rand.Float64()*0.5 }

// ---------------------------------------------------------------------------
// per-agent SSE cursor (Last-Event-ID persistence — pure replay optimisation).
// ---------------------------------------------------------------------------

// cursorPath is the agent's SSE cursor file: <home>/<id-lower-or-anon>/sse-cursor.
// Local state is pure optimisation/dedup (this cursor plus replycards-seen
// beside it) — losing it costs a full refetch or one silent re-baseline, never
// truth. Mirrors cursor_path.
func cursorPath(cfg Config) string {
	key := strings.ToLower(cfg.ID)
	if key == "" {
		key = "anon"
	}
	return filepath.Join(cfg.Home, key, "sse-cursor")
}

func readCursor(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func writeCursor(path, seq string) {
	if parent := filepath.Dir(path); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return
		}
	}
	_ = os.WriteFile(path, []byte(seq), 0o644)
}

// ---------------------------------------------------------------------------
// session-liveness probe (the listener's self-exit lifecycle tie).
// ---------------------------------------------------------------------------

// resolveTmuxBin resolves an executable tmux path, surviving launchd's MINIMAL PATH:
// PATH first, then the known install locations. "" when truly unresolvable — the
// caller treats that as 'cannot probe' (⇒ assume alive), never 'session dead'.
// Mirrors resolve_tmux_bin.
func resolveTmuxBin() string {
	if p, err := exec.LookPath("tmux"); err == nil && p != "" {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/tmux", "/usr/local/bin/tmux", "/usr/bin/tmux"} {
		if isExecutableFileListen(p) {
			return p
		}
	}
	return ""
}

func isExecutableFileListen(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}

// probeVerdict is the session probe's tri-state answer. The split between GONE
// (a definite "no such session" from tmux) and UNKNOWN (could not run the
// probe at all) is load-bearing: GONE debounces fast (sessionMissLimit), while
// UNKNOWN fails closed only after the much wider probeUnknownMin +
// probeUnknownGrace bounds — a flaky probe must never fast-kill a healthy
// listener, but an eternally unverifiable one must not live forever either.
type probeVerdict int

const (
	probeAlive probeVerdict = iota
	probeGone
	probeUnknown
)

// makeSessionProbe builds the "is my tmux session still alive?" probe from the launch
// env, or nil when probing is DISABLED (no OC_SESSION — a headless run has no session
// to mirror). The SSE-holding listener is spawned INSIDE its agent's tmux session
// (spawn exports OC_SESSION / OC_TMUX_SOCKET), so 'my session no longer exists' is the
// robust host-local death signal. Verdicts: has-session exit 0 ⇒ ALIVE; a clean
// non-zero tmux exit ⇒ GONE (tmux answered: no such session); tmux unresolvable, a
// spawn fault or a probe timeout ⇒ UNKNOWN (cannot verify — folded fail-closed by
// the caller under the wide probeUnknown* bounds, never as an instant death
// verdict). The argv mirrors the PINNED reconcile.tmux_has_session_argv builder.
// Mirrors make_session_probe.
func makeSessionProbe(env func(string) string) func() probeVerdict {
	session := strings.TrimSpace(env("OC_SESSION"))
	if session == "" {
		return nil // probing disabled — nothing to mirror
	}
	socket := strings.TrimSpace(env("OC_TMUX_SOCKET"))
	if socket == "" {
		socket = defaultTmuxSocket
	}
	return func() probeVerdict {
		bin := resolveTmuxBin()
		if bin == "" {
			return probeUnknown // cannot probe ⇒ never an instant death verdict
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// tmux -L <socket> has-session -t <session>
		cmd := exec.CommandContext(ctx, bin, "-L", socket, "has-session", "-t", session)
		err := cmd.Run()
		if err == nil {
			return probeAlive
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && ctx.Err() == nil {
			return probeGone // tmux ran and answered: the session does not exist
		}
		return probeUnknown // spawn fault / timeout — the probe itself failed
	}
}

// ---------------------------------------------------------------------------
// SSE wake gates + wake handler (WORK signals; chat is refetch, not payload).
// ---------------------------------------------------------------------------

// shouldDispatch mirrors should_dispatch: True for the WORK topics (action/task) — a
// liveness wake only (the payload is never trusted). Junk-safe (nil / other topic →
// False).
func shouldDispatch(frame map[string]any) bool {
	if frame == nil {
		return false
	}
	topic, _ := frame["topic"].(string)
	return dispatchTopics[topic]
}

// shouldWindDown mirrors should_wind_down: True ONLY for a `member` delta whose scoped
// key (<owner>::<id>) names THIS agent (suffix == my id). A pure NUDGE gate — the
// caller still REFETCHES the authoritative desired_state. Junk-safe.
func shouldWindDown(frame map[string]any, myID string) bool {
	if frame == nil {
		return false
	}
	if t, _ := frame["topic"].(string); t != memberTopic {
		return false
	}
	mid := strings.ToLower(strings.TrimSpace(myID))
	if mid == "" {
		return false
	}
	data, ok := frame["data"].(map[string]any)
	if !ok {
		return false
	}
	key := strings.TrimSpace(strOrEmpty(data["key"]))
	if key == "" {
		return false
	}
	// Strip the <owner>:: storage-scope prefix; the suffix is the member id.
	if i := strings.LastIndex(key, "::"); i >= 0 {
		key = key[i+2:]
	}
	return strings.ToLower(strings.TrimSpace(key)) == mid
}

// ---------------------------------------------------------------------------
// frame attribution + echo suppression (spec/sse.md §2.3, T-f39c 方案 A).
// ---------------------------------------------------------------------------

// frameTrigger reads the envelope's `trigger` — the verified actor of the
// write ("owner" / "server" / a member id). "" for an older producer or a
// junk frame (the caller MUST treat blank as unknown: process, never
// suppress).
func frameTrigger(frame map[string]any) string {
	if frame == nil {
		return ""
	}
	return strings.TrimSpace(strOrEmpty(frame["trigger"]))
}

// isSelfEcho is the ONE echo-suppression predicate: true iff the frame was
// triggered by THIS agent itself. An agent connection only receives frames
// addressed to itself (spec §4), so trigger==self means "my own action pushed
// back at me" — pure transcript-token burn. Blank trigger is NEVER an echo
// (fail-open on unknown attribution).
func isSelfEcho(trigger, myID string) bool {
	mid := strings.TrimSpace(myID)
	return trigger != "" && mid != "" && strings.EqualFold(trigger, mid)
}

// byTrigger renders the " · by <actor>" attribution suffix every event line
// carries (an agent reading "by 自己" on its own stream = the suppression is
// broken); blank attribution renders nothing rather than lying.
func byTrigger(trigger string) string {
	if trigger == "" {
		return ""
	}
	return " · by " + trigger
}

// previewLine collapses all whitespace (newlines included — event lines are
// ONE line) and truncates to max runes with an ellipsis. Used for NON-message
// fields (e.g. a task title) that must stay a terse one-line preview.
func previewLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// renderMessageBody prepares a MESSAGE event's body (chat body, reply-card
// summary/answer text) for the transcript. Unlike previewLine it does NOT
// collapse the body to one line and applies NO regular length cap: the body
// prints VERBATIM — newlines and all — because it is addressed to THIS agent,
// which will read it regardless, so a preview saves nothing and only forces a
// redundant get_chat / get_reply_card (T-4272). Only the anti-blowup
// messageBodyValve (a pathological-size guard, not a normal threshold) ever
// trims it — cut on a rune boundary with a pointer to the full-text authority.
//
// A multi-line body stays ONE readable event block: every continuation line is
// indented, so no inner line can begin at column 0 with the "[ocagent] " event
// prefix and be mistaken for a separate event. (previewLine's whitespace
// collapse used to guarantee this implicitly by never emitting a newline; the
// indent is the explicit replacement now that bodies may span lines.)
func renderMessageBody(s, authority string) string {
	if len(s) > messageBodyValve {
		cut := messageBodyValve
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut-- // back up to a rune start so a multi-byte char is never split
		}
		s = s[:cut] + fmt.Sprintf("… [+%d bytes past the %d KiB safety valve — read the full message with %s]",
			len(s)-cut, messageBodyValve>>10, authority)
	}
	s = strings.TrimRight(s, "\n") // drop a dangling trailing newline (no empty indented tail)
	return strings.ReplaceAll(s, "\n", "\n    ")
}

// handleEvent is the wake handler for a WORK delta: log the wake. The delta is treated
// PURELY as a liveness wake — its payload is NOT trusted to describe work. Mirrors
// handle_event.
func handleEvent(frame map[string]any, trigger string, out io.Writer) {
	fmt.Fprintf(out, "[ocagent] wake seq=%s topic=%s%s\n",
		pyStr(frame["seq"]), pyStr(frame["topic"]), byTrigger(trigger))
}

// directedBandTopics is the closed set of DIRECTED band topics the server
// pushes down THIS agent's own connection (mirrors server/ocserverd/
// sse_bands.go contextHighTopic / tokenExpiryTopic / taskCloseTopic; spec/sse.md
// §6/§6.1/§8). Unlike
// the entity-delta topics these carry a server-composed human message the
// agent must actually READ — so they print, they don't just wake.
var directedBandTopics = map[string]bool{
	contextHighTopic: true,
	tokenExpiryTopic: true,
	taskCloseTopic:   true,
}

// handleDirectedBand surfaces one directed band frame as a single human-
// readable line on out — the spawned session's Monitor carries out into the
// agent's transcript, so this print IS the agent "receiving" the signal
// (before this handler existed the frames arrived and were silently dropped).
// The frame is {"topic": ..., "data": {...,"reason": ...}}; the server-
// composed `reason` sentence is the message. Junk-safe: a missing data object
// or reason key degrades to a terse composed line from whatever fields exist
// (a bare wake still beats silence), never a panic.
func handleDirectedBand(frame map[string]any, out io.Writer) {
	topic, _ := frame["topic"].(string)
	data, _ := frame["data"].(map[string]any)
	get := func(key string) string {
		if data == nil {
			return ""
		}
		return strings.TrimSpace(strOrEmpty(data[key]))
	}
	line := get("reason")
	if line == "" {
		switch topic {
		case contextHighTopic:
			// Fallback only — the server always composes a reason, and that reason
			// is the real message (it names the ceiling and the close-out steps,
			// which this side cannot know). Kept deliberately vaguer than the
			// server's: inventing a threshold here would be a second source of
			// truth, which is the exact bug T-c382 removed.
			line = fmt.Sprintf("context usage high (level=%s pct=%s) — close out your "+
				"in-flight state before the handover", get("level"), get("pct"))
		case tokenExpiryTopic:
			line = fmt.Sprintf("agent token expires in %ss — checkpoint this turn, then call restart_self",
				get("expires_in"))
		case taskCloseTopic:
			line = fmt.Sprintf("task %s (type=%s) closed (%s) — fold this run's "+
				"learnings into the current manual as an anchor-addressed patch (patch_task_learnings)",
				get("task_no"), get("type"), get("status"))
		}
	}
	fmt.Fprintf(out, "[ocagent] signal %s: %s\n", topic, line)
}

// ---------------------------------------------------------------------------
// task downlink (R7: the delta payload only routes — refetch the authority).
// ---------------------------------------------------------------------------

// taskSnap is the last task state this listener surfaced — the diff base for
// the "what moved" fragment of the task event line. In-memory only (a
// reconnect re-baselines; the first line after a restart says the current
// state instead of a delta, which is honest).
type taskSnap struct {
	status string
	done   int
	total  int
}

// handleTaskEvent turns ONE task delta into ONE readable line: which task
// (task_no + title), what moved (status flip / step progress vs the last
// snapshot), and who moved it (the frame trigger). The delta payload
// ({id, status, priority}) is used ONLY to route the refetch — everything
// printed comes from the authoritative GET /api/tasks/{id} (R7). A refetch
// fault prints one honest line (the task DID change — silence would re-break
// the wake); a junk frame without an id degrades to the generic wake line.
//
//	[ocagent] task T-be18「fix the listener」step done (3/5) · by owner
func handleTaskEvent(client httpClient, cfg Config, frame map[string]any, snaps map[string]taskSnap, trigger string, out io.Writer) {
	id := ""
	if data, ok := frame["data"].(map[string]any); ok {
		if payload, ok := data["payload"].(map[string]any); ok {
			id = strings.TrimSpace(strOrEmpty(payload["id"]))
		}
	}
	if id == "" {
		handleEvent(frame, trigger, out) // junk hint — a bare wake still beats silence
		return
	}
	status, body := getJSON(client, cfg, tasksPath+url.PathEscape(id), true)
	t, ok := body.(map[string]any)
	if status != 200 || !ok {
		fmt.Fprintf(out, "[ocagent] task %s changed but refetch failed (HTTP %d) — "+
			"read it manually (get_task)%s\n", id, status, byTrigger(trigger))
		return
	}
	now := taskSnap{
		status: strOrEmpty(t["status"]),
		done:   intField(t["progress_done"]),
		total:  intField(t["progress_total"]),
	}
	prev, seen := snaps[id]
	snaps[id] = now
	what := ""
	switch {
	case !seen:
		// First sight this session — state the current position, not a diff.
		what = "status=" + now.status
	case now.status != prev.status:
		what = "status " + prev.status + " → " + now.status
	case now.done != prev.done || now.total != prev.total:
		what = "step done"
	default:
		what = "updated" // plan/deps/priority/notes — refetch if it matters
	}
	if now.total > 0 {
		what += fmt.Sprintf(" (%d/%d)", now.done, now.total)
	}
	no := strOrEmpty(t["task_no"])
	if no == "" {
		no = id
	}
	title := previewLine(strOrEmpty(t["title"]), 48)
	sep := " "
	if title != "" {
		title = "「" + title + "」"
		sep = "" // the closing 」 already separates
	}
	fmt.Fprintf(out, "[ocagent] task %s%s%s%s%s\n", no, title, sep, what, byTrigger(trigger))
}

// intField reads a JSON-decoded number as int (0 on anything else).
func intField(v any) int {
	f, _ := v.(float64)
	return int(f)
}

// ---------------------------------------------------------------------------
// chat downlink (R7: the delta payload is NEVER read — refetch the authority).
// ---------------------------------------------------------------------------

// chatUnreadPageLimit is the row budget of ONE unread page. The walk below pages
// until the server stops handing back a cursor, so this number does not bound
// what a drain prints — it only decides how many round trips a given backlog
// costs. 50 keeps a normal catch-up to a single round trip.
const chatUnreadPageLimit = 50

// chatUnreadMaxPages is the SECOND net under "page until the cursor is empty".
//
// The first net is the cursor-repeat check in fetchChat: a server that keeps
// handing back the SAME token would otherwise spin forever. It does not catch a
// server that mints a FRESH token every time (or one that answers rows without
// ever ending), and that shape is just as fatal — the listener's only thread
// would never come back and this member would go deaf with nothing said. So the
// walk also refuses to take more than this many pages.
//
// 10 pages × 50 rows is 500 messages, well above any real backlog, which is the
// point: hitting this is a server bug, not a busy inbox, and the line it prints
// says so. It is also what bounds the REQUEST cost of a misbehaving server: at
// worst this walk costs 10 GETs, once per drain.
//
// 🔴 IT IS A LOOP GUARD, NOT A DOCUMENTED LIMIT — owner, 2026-09-03
// (rc-c31c54ca9b8b): 「5000則不是我們spec這樣訂，我們預期是未讀不會超過5000則，用這個
// 數字來防止無限迴圈」, and then 「你也可以先把5000先改為500」. So the number is chosen
// to be comfortably out of reach of real traffic while keeping a runaway server
// cheap, and it does NOT belong in the agent-facing seed as a caveat: the seed
// describes the normal case. Lower it further if 500 is still generous; the only
// thing that must stay true is that reaching it is impossible in normal use.
//
// Reaching it is NOT silent and never has been: the walk prints the "came up
// short" line and leaves the remainder unread, so the next drain picks it up.
const chatUnreadMaxPages = 10

// chatFetch is what one full unread walk answers.
//
// `rows` is nil ONLY on a first-page fault — the fail-closed shape fetchChat has
// always had, kept because "zero messages" and "I could not look" must not be
// the same answer. A fault on a LATER page keeps the pages already in hand:
// unread is served oldest-first, so what a partial walk holds is a contiguous
// run from the oldest, and printing it (then marking it read) leaves the rest
// exactly where the next drain will find it.
//
// `stop` is non-empty when the walk ended for any reason OTHER than the server
// saying "no more" — a mid-walk fault, a cursor that did not advance, or the
// page ceiling. It is a line to print: a backfill that quietly stopped short is
// indistinguishable from a backfill that finished, and this drain's whole job is
// to notice what arrived.
type chatFetch struct {
	rows []map[string]any
	stop string
}

// fetchChat walks the caller's OWN UNREAD from
// GET /api/chat?recipient=<selfID>&unread=true&limit=… , following `next_cursor`
// until the server stops issuing one. Returns the wire ChatMessageDTOs
// (from/to/body/id/ts), OLDEST→NEWEST across every page. Mirrors fetch_chat.
//
// WHY UNREAD AND NOT ?with=<self> (T-48). The old call took the server's newest
// window of the whole conversation and let the client work out what was new.
// That window is a fixed size, so a long enough absence pushed the oldest unread
// out of it — messages nobody could print because nobody fetched them. The
// unread query is defined by the caller's own per-sender watermarks instead, so
// the set is exactly "what I have not been shown", and paging it to exhaustion
// is what makes the backfill COMPLETE rather than merely recent.
//
// `with=` IS DELIBERATELY ABSENT: recipient=<self> already pins this listener as
// a participant, so `with=` would only re-state it — and the unread index leads
// with `recipient`, so the narrower filter is also the one the server can serve
// straight off the index.
//
// 🔴 NOTHING HERE MARKS ANYTHING READ. The unread route is a pure read; the
// receipt is filed by drainChat AFTER the lines are printed.
func fetchChat(client httpClient, cfg Config, selfID string) chatFetch {
	base := "/api/chat?recipient=" + url.QueryEscape(selfID) +
		"&unread=true&limit=" + strconv.Itoa(chatUnreadPageLimit)
	rows := make([]map[string]any, 0, chatUnreadPageLimit)
	issued := map[string]bool{}
	cursor := ""
	for page := 1; ; page++ {
		path := base
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		// ONE page: the T-48 envelope {"messages": [...], "next_cursor": "…"}. A
		// non-200, a body that is not an object, or one whose `messages` is not an
		// array is a FAULT — it must not degrade to "zero messages", which is
		// indistinguishable from a quiet conversation.
		//
		// A `next_cursor` that is absent, null, empty or not a string reads as "no
		// more pages". That is the safe direction: an under-printed drain is
		// retried by the next one, whereas treating junk as a token would put an
		// unusable value back on the wire.
		status, body := getJSON(client, cfg, path, true)
		env, _ := body.(map[string]any)
		list, listOK := env["messages"].([]any)
		if status != 200 || !listOK {
			if page == 1 {
				// TOTAL FAULT — and it says so. Nothing was fetched, so nothing
				// prints and nothing is receipted; the window stays unread and the
				// next drain tries again. That is the safe direction, but SILENCE
				// IS NOT: a drain that fetched nothing looks exactly like a drain
				// that found nothing, and the reader would conclude there was no
				// new chat.
				//
				// This used to cite a sentence in seeds/boot_sequence.md as its
				// reason. That sentence was rewritten out of the seed in this same
				// branch (d0306390) while this comment kept pointing at it — the
				// citation outlived what it cited, silently, which is the exact
				// failure this line exists to prevent, one level up. The reason is
				// therefore stated here instead of borrowed: whatever the seed
				// happens to say today, a fetch that answered nothing must not be
				// indistinguishable from a conversation that held nothing.
				return chatFetch{stop: fmt.Sprintf(
					"[ocagent] chat: 補印一頁都沒撈到（HTTP %d）—— 這不是「沒有新訊息」，"+
						"是這次沒問到。未讀原封不動，下一次補印會再試；等不及就用 get_chat 自己撈。\n",
					status)}
			}
			return chatFetch{rows: rows, stop: fmt.Sprintf(
				"[ocagent] chat: 補印在第 %d 頁斷掉了（已經撈到 %d 則）—— 未讀沒撈完，"+
					"剩下的下一次補印會再試；等不及就用 get_chat 自己回頭撈。\n", page, len(rows))}
		}
		for _, it := range list {
			if m, ok := it.(map[string]any); ok {
				rows = append(rows, m)
			}
		}
		next := strings.TrimSpace(strOrEmpty(env["next_cursor"]))
		if next == "" {
			return chatFetch{rows: rows} // the server says this is the end
		}
		if issued[next] {
			return chatFetch{rows: rows, stop: fmt.Sprintf(
				"[ocagent] chat: 補印停在第 %d 頁（已經撈到 %d 則）—— server 的 next_cursor 沒有前進，"+
					"同一個游標又發了一次。未讀沒撈完，請用 get_chat 自己回頭撈。\n", page, len(rows))}
		}
		if page >= chatUnreadMaxPages {
			return chatFetch{rows: rows, stop: fmt.Sprintf(
				"[ocagent] chat: 補印撈到第 %d 頁就停了（已經撈到 %d 則），server 還在給 next_cursor —— "+
					"這是分頁上限，不是你的信箱真有這麼多。未讀沒撈完，請用 get_chat 自己回頭撈。\n",
				page, len(rows))}
		}
		issued[next] = true
		cursor = next
	}
}

// fmtAgo renders an age in seconds as the terse single-unit form the chat line uses:
// 10s / 2m / 1h / 3d (truncating). A negative age (clock skew) clamps to 0s.
func fmtAgo(secs float64) string {
	s := int64(secs)
	switch {
	case s < 0:
		return "0s"
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh", s/3600)
	default:
		return fmt.Sprintf("%dd", s/86400)
	}
}

// attachmentSummary renders a message's attachments as a terse badge appended
// after the body: "📎2圖" (2 images), "📎1檔" (1 non-image file), or the mixed
// "📎1圖 2檔". Images are counted by the server-computed is_image flag. Returns
// "" when the message carries no attachments, so a zero-attachment line carries
// no badge at all — not a badge that says zero. (This says nothing about the
// rest of the line: it also carries the `#<message id>` tag, which is not this
// function's business.) Junk-safe: a non-array attachments field or non-map
// elements degrade to "" / are skipped, never a panic.
func attachmentSummary(m map[string]any) string {
	refs, ok := m["attachments"].([]any)
	if !ok {
		return ""
	}
	imgs, files := 0, 0
	for _, r := range refs {
		am, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if b, _ := am["is_image"].(bool); b {
			imgs++
		} else {
			files++
		}
	}
	switch {
	case imgs == 0 && files == 0:
		return ""
	case files == 0:
		return fmt.Sprintf("📎%d圖", imgs)
	case imgs == 0:
		return fmt.Sprintf("📎%d檔", files)
	default:
		return fmt.Sprintf("📎%d圖 %d檔", imgs, files)
	}
}

// ---------------------------------------------------------------------------
// batch acknowledgement — the codex sidecar's delivery receipt (T-48).
// ---------------------------------------------------------------------------

// listenAckEnv is the ONE runtime signal that this listener's stdout is not the
// agent's own transcript. A claude member reads these lines directly, so a
// printed line IS a delivered line; a codex member reads them through the
// ocwarden sidecar, which has to turn each line into an App Server turn — and
// that turn can be REFUSED. Printing is then no evidence of anything.
//
// 🔴 IT IS AN ENV VAR THE PARENT SETS, AND NOTHING ELSE. Not a tty check, not a
// parent-process sniff, not "OC_ID looks like a codex member": every one of
// those guesses answers wrongly in some real configuration, and the direction it
// is wrong in (ack mode with nobody to answer) HANGS the drain. The only party
// that knows this listener is being consumed is the process that started it, so
// it says so out loud (cli/ocwarden/codex_session.go sets it on the child).
const listenAckEnv = "OC_LISTEN_ACK"

// ackGate is the listener half of that protocol: it prints the end-of-batch
// marker and blocks until the consumer answers `ack <token>` / `nack <token>` on
// stdin. A nil gate is the claude path — no marker, no wait, always yes — so the
// non-ack behaviour is not a branch that can rot but the absence of an object.
type ackGate struct {
	answers <-chan string
	next    int // monotonic batch token
	wait    time.Duration
}

// ackWaitTimeout bounds how long one batch may hold the listener.
//
// 🔴 WHY THERE IS A DEADLINE AT ALL. Waiting for the verdict is the whole point
// of this gate, but the wait happens on the listener's ONLY thread: while it
// blocks, this member receives nothing — no chat, no cards, no tasks. An
// unbounded wait therefore turns one unanswerable batch into a member that is
// deaf for the rest of its life, and NOTHING says so. That is worse than the
// false tick this gate exists to remove: a tick is wrong about the past, a deaf
// listener is wrong about everything after it.
//
// Timing out is SAFE in the direction that matters. It is treated exactly like
// a nack — no receipt filed, the ids not recorded — so the same lines are
// printed again on the next drain. The cost of being wrong here is a repeat;
// the cost of waiting forever is silence.
const ackWaitTimeout = 30 * time.Second

// newAckGate returns nil (⇒ the claude path) unless the parent asked for acks.
func newAckGate(env func(string) string, answers io.Reader) *ackGate {
	if env == nil || env(listenAckEnv) != "1" || answers == nil {
		return nil
	}
	// The scanner runs on its own goroutine so `confirm` can select against a
	// deadline: a bufio.Scanner read cannot be cancelled once it has begun.
	lines := make(chan string, 8)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(answers)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	return &ackGate{answers: lines, wait: ackWaitTimeout}
}

// confirm prints `[ocagent] listen: batch <token>` and waits for the verdict on
// that exact token. True ⇒ the batch reached the agent's conversation and the
// caller may file the read receipt and record the ids; false ⇒ it did not, and
// the caller must do NEITHER, so the next drain prints the same lines again.
//
// The marker deliberately wears the `listen:` head: it is protocol, not content,
// and that head is what the sidecar's blanket filter swallows — so it can never
// become a turn on the model.
//
// A closed stdin (the consumer died) answers false for the same reason a nack
// does: nobody can say the message was delivered.
func (g *ackGate) confirm(out io.Writer) bool {
	if g == nil {
		return true
	}
	g.next++
	token := strconv.Itoa(g.next)
	fmt.Fprintf(out, "%s%s %s\n", agentLinePrefix, noticeBatch, token)
	deadline := time.After(g.wait)
	for {
		select {
		case line, open := <-g.answers:
			if !open {
				return false // stdin closed: nobody can say it was delivered
			}
			verb, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
			if strings.TrimSpace(arg) != token {
				continue // an answer to a batch that is no longer open
			}
			return verb == "ack"
		case <-deadline:
			// Said out loud, and deliberately WITHOUT the `listen:` head, so it
			// is one of the few lines that reaches the agent itself: from here
			// on this member is running again but those messages were not
			// delivered, and it is the only party that can go and look.
			fmt.Fprintf(out, "%s等不到「已送達」的回覆（batch %s，等了 %s）—— "+
				"這一批訊息**沒有**被算成你看過了，下一次補印會再送一次。"+
				"如果這行一直出現，收訊這條路的另一端有問題。\n",
				agentLinePrefix, token, g.wait)
			return false
		}
	}
}

// ---------------------------------------------------------------------------
// the drain's two latches — all that outlived the local ledger.
// ---------------------------------------------------------------------------

// drainWarner holds the once-per-episode latches for the two lines a drain can
// emit about ITSELF rather than about a message. It is ALL that remains of the
// old chatSeen store: that store was a second, local answer to "have I already
// surfaced this?", and the server's unread set is now the only one (T-48).
// Neither latch touches that question — both only stop one diagnostic line from
// repeating on every drain — so they stay, owned by the listener.
//
// 🔴 WHY THESE LINES NEED LATCHES AT ALL AND THE CHAT LINES DO NOT. Neither of
// them wears the "[ocagent] listen:" head, so cli/ocwarden's
// actionableCodexListenerLine does not swallow them: on the codex path every
// copy becomes a model turn. A drain runs on every reconnect, and a reconnect
// loop is bounded only by listenBackoffCap — so an unlatched diagnostic emitted
// by a drain is a turn every ≤15s, for as long as the fault lasts, without
// limit. That is the noise the owner's disconnect-notice ruling (2026-08-30)
// exists to remove, arriving through a door the ruling's own latch does not
// cover.
//
// A nil warner FAILS LOUD: both lines print every time rather than not at all.
// A latch that silently swallows the only signal a fault ever produces would be
// the same class of bug the lines exist to expose.
type drainWarner struct {
	// markReadWarned: a receipt that did not land is announced once per PROCESS,
	// not once per episode. See warnMarkReadFailed for why that one is different.
	markReadWarned bool
	// chatFaultOpen: a total fetch fault has been announced and has not yet been
	// cleared by a fetch that reached the server. Modelled on listener.inOutage —
	// one episode, one announcement, and a line when it ends.
	chatFaultOpen bool
}

// noteChatFetchFault announces a TOTAL fetch fault once per episode. The line
// itself is built by fetchChat (it is the only party that knows the status); the
// only decision here is whether this drain is the one that says it.
//
// The fault repeats on every drain for as long as /api/chat is refusing, and the
// drains keep coming as long as the SSE stream keeps reconnecting — a stream
// that accepts and immediately closes reconnects at listenBackoffCap forever. So
// without this latch the line is unbounded, and on the codex path each copy is a
// model turn.
func noteChatFetchFault(warn *drainWarner, out io.Writer, line string) {
	if out == nil || line == "" {
		return
	}
	if warn != nil {
		if warn.chatFaultOpen {
			return
		}
		warn.chatFaultOpen = true
	}
	fmt.Fprint(out, line)
}

// clearChatFetchFault closes the episode noteChatFetchFault opened, and SAYS SO.
//
// The silence after the fault line is ambiguous in exactly the way the fault
// line was written to remove: "未讀原封不動，下一次補印會再試" leaves the reader
// unable to tell a drain that is still failing quietly from one that recovered
// and found nothing. Announcing the recovery is what makes an empty drain
// readable again — the same reason connectOnce prints a reconnect notice rather
// than just dropping the outage flag.
//
// A nil warner has no episode to close (it printed the fault every time, so
// nothing was ever suppressed), and prints nothing here.
func clearChatFetchFault(warn *drainWarner, out io.Writer) {
	if warn == nil || out == nil || !warn.chatFaultOpen {
		return
	}
	warn.chatFaultOpen = false
	fmt.Fprint(out, "[ocagent] chat: 補印又問得到了 —— 上面那次「一頁都沒撈到」到此為止。"+
		"接下來印出來的就是這次真的撈到的東西；沒有東西就是真的沒有新訊息。\n")
}

// drainChat refetches chat and prints the unread-for-me — ONE LINE per message so the
// spawned session's Monitor reads exactly '誰、多久前、說了什麼':
//
//	[ocagent] chat from m-3417933c8632 (#c-ceb835093301, 2m ago): ...
//
// `from` is the STABLE member id (server-stamped, never a display name) — reply
// straight to it with post_chat. The `#…` tag is the MESSAGE id: the handle to
// name this exact message when calling get_chat for the full body/attachments.
// Only the id goes in — filenames, attachment ids and mimes stay OUT, because
// this line is a token cost every agent pays on every message; get_chat is where
// that detail belongs. The relative age is computed client-side from the message
// ts. Any tag slot is dropped when the wire carries no id / no reply_to / no ts,
// and a message with none of them prints without the parenthesised tag at all.
//
// The middle slot is the REPLY marker (T-4e95): `↩#<id>` naming the message this
// one is replying to, present only when the wire's `reply_to` is non-empty:
//
//	[ocagent] chat from boss (#c-reply, ↩#c-target, 2m ago): 這個再確認一下
//
// It is an EXISTENCE marker, exactly like the attachment badge: the quoted
// sender and body are NOT printed. Since 2026-08-21 the wire DOES carry them
// (`reply_to_chat`, built on every read), so this is a deliberate choice by this
// line rather than a limit of the payload — one console line per inbound message
// is a token cost every agent pays on every message, and a second sentence
// inside it doubles that for a relation most messages do not have. The id is
// enough to tell the woken agent a reply target EXISTS; get_chat is where the
// text belongs, and it now comes back with the quote already attached.
//
// Returns the unread count, and PRINTS EVERY LINE IT COUNTS.
//
// 🔴 THERE IS NO LOCAL "already seen" LEDGER ANY MORE (owner, rc-224dee5770dd:
// 「拔掉 —— 一件事只留一個說法（server 的未讀）」). fetchChat asks for
// `unread=true`, so the server's unread set IS the question's only answer, and
// the read receipt filed below is the only thing that advances it. There is
// therefore no "first run on this machine" concept left and no `silent` mode:
// unread is unread, and unread prints.
//
// WHAT THAT BUYS, structurally: a row can leave the unread set ONLY by way of a
// receipt, and a receipt is filed ONLY for lines this drain actually printed
// (plus the self-sent exception below, which is printed to nobody by design and
// belongs to the reader anyway). So "marked read but never shown" is not a case
// that can arise — not because a check forbids it, but because the only writer
// of the watermark is the code path that just printed.
//
// A fetch fault prints nothing and files nothing, so the same window is still
// unread on the server and the next drain retries it.
// R7: reads ONLY the refetched authority, never a delta. Mirrors drain_chat.
//
// `gate` (nil on the claude path) makes "printed" stop meaning "delivered": with
// a gate the receipt waits for the consumer's ack, so a batch the codex sidecar
// could not get into the model's conversation is never receipted — and, since
// the receipt is the only thing that would have moved the server's watermark, it
// comes back on the next drain by itself. See ackGate.
func drainChat(client httpClient, cfg Config, out io.Writer, warn *drainWarner, gate *ackGate) int {
	sid := strings.ToLower(strings.TrimSpace(cfg.ID))
	now := float64(time.Now().Unix())
	msgs := fetchChat(client, cfg, cfg.ID)
	if msgs.rows == nil {
		// TOTAL FAULT: nothing was fetched, so nothing prints and nothing is
		// receipted — the window is still unread and the next drain retries it.
		// It is announced (silence here reads as "no new chat"), but LATCHED: the
		// next drain is at most listenBackoffCap away and would say the same
		// thing, forever. See noteChatFetchFault.
		noteChatFetchFault(warn, out, msgs.stop)
		return 0
	}
	// The fetch reached the server, so whatever fault was open is over — said
	// before anything else this drain prints, because it is the caveat on the
	// PREVIOUS drains and not on this batch.
	clearChatFetchFault(warn, out)
	// A walk that ended short says so BEFORE the lines it did manage to fetch,
	// so the reader meets the caveat before the batch it applies to. This one is
	// NOT latched: it is a statement about the batch printed underneath it, so a
	// drain that omitted it would be mis-describing its own output.
	if msgs.stop != "" {
		fmt.Fprint(out, msgs.stop)
	}
	// delivered stays true on the claude path: nothing there can fail to deliver.
	delivered := true
	unread := make([]map[string]any, 0, len(msgs.rows))
	// selfSent collects the messages this member sent to ITSELF — see the block
	// that receipts them below, which is the one place in this drain where a line
	// is marked read without ever being printed.
	var selfSent []map[string]any
	for _, m := range msgs.rows {
		if strings.ToLower(strings.TrimSpace(strOrEmpty(m["to"]))) != sid {
			continue
		}
		// ECHO SUPPRESSION, message level (T-2c6d). The dispatch gate applies
		// isSelfEcho to a FRAME's trigger and drops the whole frame — which means a
		// self-triggered chat delta never reaches this drain at all, so the message
		// it announced is never marked seen. It then sits in the unread window until
		// some OTHER actor's chat delta passes the gate and runs this drain, which
		// flushes the whole self-sent backlog alongside the message that actually
		// arrived. Frame-level suppression therefore only
		// DELAYS a self echo; it does not remove it. Applying the same predicate to
		// the refetched sender closes that half.
		//
		// 🔴 THE RULE LIVES HERE, NOT IN THE API (rc-dccab860be32). GET /api/chat's
		// unread set no longer excludes `sender == caller`, so these rows DO come
		// back and this is the only party that drops them. That is deliberate:
		// "don't read me my own words back" is a presentation decision, and the
		// server should not be the one holding it.
		//
		// SKIPPED HERE, NOT AT PRINT TIME, and that placement is the point: this
		// loop builds `unread`, which is BOTH what prints below and this function's
		// return value. Filtering later would report unread lines that never print.
		//
		// Reusing isSelfEcho keeps the fail-open rule (spec/sse.md §2.3): a blank or
		// missing sender is never an echo, so it stays visible. TrimSpace mirrors
		// the `to` filter above, so both sides of the message are matched the same
		// way, and isSelfEcho's EqualFold makes the comparison case-insensitive.
		if isSelfEcho(strings.TrimSpace(strOrEmpty(m["from"])), cfg.ID) {
			selfSent = append(selfSent, m)
			continue
		}
		unread = append(unread, m)
	}
	// EVERY UNREAD LINE PRINTS. There used to be a per-drain print cap here
	// that kept the newest N and announced the rest as 略過 N 則 — and that
	// notice was not merely a smaller backfill, it was a WRONG one: the
	// receipt filed below is a per-sender WATERMARK, so a skipped older line
	// of a sender who still had a surviving newer line was swept to read by
	// that sender's own receipt. It was announced as fetchable and then
	// marked as read, and nothing would ever offer it again.
	//
	// Nothing replaces the cap because nothing needs to: fetchChat now walks
	// the caller's unread to exhaustion, so `unread` IS the whole backlog
	// rather than a window of it, and a backlog is only as long as the time
	// this member spent away. The cost this cap was protecting — the agent's
	// context window — is real, but it is paid for by messages that were
	// genuinely sent TO this member and never shown; dropping them is not a
	// saving, it is a loss with a receipt filed against it.
	for _, m := range unread {
		printChatLine(out, m, now)
	}
	// AFTER the lines are out, never before: a read receipt claims a HUMAN-
	// or agent-visible event, so it must trail the print it is claiming. A
	// crash between the fetch and the loop above therefore leaves no receipt
	// and the next drain re-prints — the safe direction.
	//
	// UNDER AN ACK GATE the print is not the delivery (see ackGate): the
	// codex sidecar still has to get these lines into the model's
	// conversation, and that can fail. So the receipt waits for its verdict.
	// An empty batch is not gated: there is nothing to confirm and nothing that
	// could be lost, and blocking on a round trip for zero lines would only add
	// a way to hang.
	if len(unread) > 0 {
		delivered = gate.confirm(out)
	}
	// 🔴 THE ONE EXCEPTION TO "printed, therefore read" (rc-dccab860be32,
	// ruled by the owner). Everywhere else in this drain a receipt trails a
	// line that actually reached the session; these rows are receipted having
	// been printed NOWHERE.
	//
	// WHY: since the unread set stopped excluding `sender == caller`, a
	// message this member sent to itself comes back on every unread walk. It
	// is dropped above, so no receipt would ever be filed for it, so the
	// server would keep returning it — forever, and one more of them with
	// every self-addressed note, until the backfill is mostly the member's own
	// voice. Marking them is what keeps the unread set finite.
	//
	// SCOPE — `sender == this member`, AND NOTHING ELSE, AND IT MUST NOT GROW.
	// The exception is safe only because the reader and the writer are the
	// same party: nobody is being told they were shown something they were
	// not. Any other unprinted row belongs to someone who is entitled to a ✓
	// that means what it says, so widening this to "skipped", "too long",
	// "uninteresting" or "already in the transcript" would put this system
	// straight back into the bug the whole ticket exists to remove.
	//
	// Its receipt is INDEPENDENT of `delivered`: the ack gate answers for what
	// the sidecar had to deliver, and nothing here was ever delivered to
	// anybody. The senders cannot collide either — a printed line's sender is
	// never this member, so the two groups never share a watermark.
	//
	// `show` is therefore WHAT GETS A RECEIPT, which is no longer the same
	// list as what was printed: the printed lines only when the consumer
	// confirmed them, plus the self-sent rows unconditionally.
	show := make([]map[string]any, 0, len(unread)+len(selfSent))
	if delivered {
		show = append(show, unread...)
	}
	show = append(show, selfSent...)
	reportChatRead(client, cfg, show, warn, out)
	// NOTHING IS RECORDED LOCALLY. What used to stand here was a rebuilt id set
	// written to disk, plus a hand-rolled "drop the ids nobody confirmed" pass to
	// simulate the redelivery the server would do anyway. Both are gone: the only
	// state is the server's unread set, and the ONLY thing that moves it is the
	// receipt filed above. An undelivered batch files no receipt, so those rows
	// are still unread and the next drain fetches and prints them again — not by
	// arrangement, but because nothing ever told the server otherwise.
	return len(unread)
}

// reportChatRead files the read receipts for the lines drainChat JUST printed,
// one POST /api/chat/mark-read per SENDER.
//
// WHY THE LISTENER HAS TO DO THIS AT ALL: GET /api/chat used to advance the
// watermark as a side effect of being read, which made the ✓ mean "something
// fetched this conversation" rather than "someone read it" — a listener merely
// being alive lit it. That side effect is gone (8cd4fff9), so the receipt now
// has to be filed by whoever actually surfaced the message, which is here.
//
// PER SENDER, NOT PER BATCH: the watermark is scoped to a conversation
// (reader, peer). One drain can carry messages from several senders, and a
// single "newest ts in the batch" reported against each of them would mark A's
// conversation read up to a message B sent — a receipt for something nobody
// showed. So each sender gets its own high-water mark, computed from that
// sender's own lines only.
//
// 🔴 THE WATERMARK ONLY TELLS THE TRUTH BECAUSE NOTHING IS SKIPPED ANY MORE.
// A receipt is a WATERMARK, not a per-message acknowledgement: it says
// "everything at or below this ts is read". While drainChat capped its print at
// the newest N lines, a sender with one surviving printed line had that receipt
// sweep in every OLDER line of theirs the cap had dropped — announced as
// fetchable, then marked read, and never offered again. The cap is gone and the
// unread walk is exhaustive, so every line covered by a watermark is a line this
// drain actually printed.
// Pinned by TestDrainChat_EveryWatermarkEqualsWhatThatSenderActuallyPrinted.
//
// A sender files NO receipt when none of their lines printed — an undelivered
// batch, or a message the wire sent without a usable ts, which has no watermark
// to report.
// markReadPath is the read-receipt endpoint (POST): body {peer, last_read_ts},
// reader = the verified JWT sub, watermark monotonic (a stale report is a
// no-op 200).
const markReadPath = "/api/chat/mark-read"

func reportChatRead(client httpClient, cfg Config, printed []map[string]any, warn *drainWarner, out io.Writer) {
	high := map[string]float64{}
	for _, m := range printed {
		peer := strings.TrimSpace(strOrEmpty(m["from"]))
		if peer == "" {
			continue
		}
		ts, ok := m["ts"].(float64)
		if !ok || ts <= 0 {
			continue
		}
		if ts > high[peer] {
			high[peer] = ts
		}
	}
	peers := make([]string, 0, len(high))
	for peer := range high {
		peers = append(peers, peer)
	}
	sort.Strings(peers) // deterministic order, so one drain reports the same way every run
	for _, peer := range peers {
		status, _ := postJSON(client, cfg, markReadPath, map[string]any{
			"peer":         peer,
			"last_read_ts": high[peer],
		})
		if status != 200 {
			warnMarkReadFailed(warn, out, peer, status)
		}
	}
}

// warnMarkReadFailed says ONCE per process that the read receipt did not land.
//
// 🔴 THE LOSS IS NOT COSMETIC, AND THIS LINE USED TO SAY IT WAS. While a local
// ledger existed, a refused receipt cost only the sender's ✓: the batch had been
// recorded as surfaced on this side, so it never came back. That ledger is gone
// (T-48, rc-224dee5770dd) and the receipt is now the ONLY thing that moves the
// server's unread watermark — so a receipt that does not land leaves the whole
// batch unread, and every drain from here on fetches and prints it again, for
// as long as the endpoint keeps refusing. Pinned by
// TestDrainChat_MarkReadRefused_TheSameBatchPrintsAgainNextDrain.
//
// WHY THE LATCH STAYS ONCE-PER-PROCESS ANYWAY. The reprints are unbounded, so a
// warning that tracked them would be unbounded too — and this line deliberately
// does NOT wear the "[ocagent] listen:" head, which means the codex sidecar
// forwards it and every copy becomes a model turn (cli/ocwarden's
// actionableCodexListenerLine). One line per flap of a flapping endpoint is the
// same unbounded-turn failure this ticket exists to remove. So the line is said
// ONCE PER PROCESS and made to carry the explanation forward: it states up front
// that the batch WILL keep reprinting until a receipt lands, which is the
// explanation every later reprint needs and the reason none of them needs its own.
//
// 🔴 SAY THE COST OUT LOUD, because "once per process" is not "once per outage"
// and the two were conflated here until the seventeenth review separated them.
// The chat-fault line one screen up IS per-outage (drainWarner.chatFaultOpen,
// plus a line when it clears). This one is not, and the difference is deliberate
// but it is NOT free: recover, then fail a SECOND time, and the reprints come
// back with nothing said at all. What makes that survivable is only that the
// first line already told the reader this warning does not repeat — a reader who
// missed that line gets no second chance. If you ever find yourself wanting the
// per-outage shape here too, the machinery is already next door; what stopped it
// was the codex turn budget, not the difficulty.
func warnMarkReadFailed(warn *drainWarner, out io.Writer, peer string, status int) {
	if out == nil {
		return
	}
	if warn != nil {
		if warn.markReadWarned {
			return
		}
		warn.markReadWarned = true
	}
	fmt.Fprintf(out, "[ocagent] mark-read 沒送成功（peer=%s，HTTP %d）— 訊息已經印出來了，"+
		"但是回條沒送成功，server 那邊就還算未讀：這一批下一次補印會再印一次，"+
		"而且會一直重印到回條送成功為止，在那之前對方的已讀勾也不會亮。"+
		"這個行程只會講這一次 —— 之後再看到同一批訊息重複出現，原因就是這一行。\n", peer, status)
}

// printChatLine emits the one-line-per-message form documented on drainChat.
func printChatLine(out io.Writer, m map[string]any, now float64) {
	mid := strOrEmpty(m["id"])
	tag := make([]string, 0, 3)
	if mid != "" {
		tag = append(tag, "#"+mid)
	}
	if rt := strings.TrimSpace(strOrEmpty(m["reply_to"])); rt != "" {
		tag = append(tag, "↩#"+rt)
	}
	if ts, ok := m["ts"].(float64); ok && ts > 0 {
		tag = append(tag, fmtAgo(now-ts)+" ago")
	}
	content := renderMessageBody(strOrEmpty(m["body"]), chatBodyAuthority)
	if badge := attachmentSummary(m); badge != "" {
		if content == "" {
			content = badge
		} else {
			content += " " + badge
		}
	}
	if len(tag) == 0 {
		fmt.Fprintf(out, "[ocagent] chat from %s: %s\n", pyStr(m["from"]), content)
		return
	}
	fmt.Fprintf(out, "[ocagent] chat from %s (%s): %s\n",
		pyStr(m["from"]), strings.Join(tag, ", "), content)
}

// ---------------------------------------------------------------------------
// reply-card downlink (R7: the delta payload is a hint — refetch the authority).
// ---------------------------------------------------------------------------

// handleReplyCard wakes the session when a reply card THIS agent opened got its
// answer — or was marked EXPIRED (標為過期: not an answer; whoever pressed it —
// the owner, an admin agent, or since T-1b88 the card's own author, possibly
// this very agent — is saying the ask went stale, and this agent decides itself
// whether to reopen a fresh card or move on). The delta payload ({id, from,
// status}) is
// used ONLY as a routing hint:
// `id` points the refetch, `from` pre-filters the owner-wide fan-out so an
// answer to some OTHER member's card never triggers a refetch (nor a print) —
// the fan reaches every listener of the owner, but a card's answer is addressed
// to its initiator alone. Everything PRINTED comes from the refetched authority
// (GET /api/reply-cards/{id}), whose from/status also re-gate the decision, so
// a stale or junk payload can at worst cost one wasted GET, never a wrong line.
// Both the first answer (POST) and a 重新決定 revision (PUT) ride the same
// delta shape and print the same way (a revision bumps answered_ts, so the
// seen dedup below never swallows it). A refetch fault prints one honest line
// (the card DID change — silence would re-break the wake this handler exists
// to deliver).
//
// seen is the shared (id → answered_ts) dedup state with drainReplyCards: an
// answer this listener already surfaced — by an earlier delta OR by the
// boot/reconnect drain racing this delta — prints exactly once, and what the
// live path prints is recorded so the next drain stays quiet about it.
func handleReplyCard(client httpClient, cfg Config, frame map[string]any, seen *replyCardSeen, trigger string, out io.Writer) {
	data, _ := frame["data"].(map[string]any)
	if data == nil {
		return
	}
	payload, _ := data["payload"].(map[string]any)
	if payload == nil {
		return
	}
	id := strings.TrimSpace(strOrEmpty(payload["id"]))
	if id == "" {
		return
	}
	mid := strings.ToLower(strings.TrimSpace(cfg.ID))
	from := strings.ToLower(strings.TrimSpace(strOrEmpty(payload["from"])))
	if from != "" && from != mid {
		return // someone else's card — not my wake
	}
	status, body := getJSON(client, cfg, replyCardsPath+url.PathEscape(id), true)
	card, ok := body.(map[string]any)
	if status != 200 || !ok {
		fmt.Fprintf(out, "[ocagent] reply-card %s changed but refetch failed (HTTP %d) — "+
			"read it manually (get_reply_card).\n", id, status)
		return
	}
	if strings.ToLower(strings.TrimSpace(strOrEmpty(card["from"]))) != mid {
		return // the authority says the card is not mine
	}
	switch strOrEmpty(card["status"]) {
	case replyCardAnswered:
		ts, _ := card["answered_ts"].(float64)
		if seen.has(id, ts) {
			return // this exact answer was already surfaced (drain or an earlier delta)
		}
		printReplyCardAnswered(out, id, card, trigger)
		seen.record(id, ts)
	case replyCardExpired:
		// The shared seen state keys (id → ts): expired_ts never collides with
		// an answered_ts entry for the same terminal card — a card expires only
		// from waiting, so it can never have printed an answer before.
		ts, _ := card["expired_ts"].(float64)
		if seen.has(id, ts) {
			return // this expiry was already surfaced (drain or an earlier delta)
		}
		printReplyCardExpired(out, id, card, trigger)
		seen.record(id, ts)
	default:
		return // my own create echo — nothing to wake on yet
	}
}

// printReplyCardAnswered is the ONE answer line both the live delta path and
// the boot/reconnect drain emit — same wake, same shape, whichever path wins.
// The live path carries the frame's trigger attribution (who answered — the
// owner, normally); the drain path has no frame and passes "" (no suffix).
func printReplyCardAnswered(out io.Writer, id string, card map[string]any, trigger string) {
	fmt.Fprintf(out, "[ocagent] reply-card %s answered: %s | asked: %s%s\n",
		id, renderReplyCardAnswer(card),
		renderMessageBody(strOrEmpty(card["summary"]), replyCardBodyAuthority), byTrigger(trigger))
}

// printReplyCardExpired is the ONE expiry line both the live delta path and
// the boot/reconnect drain emit — self-carrying guidance so an agent whose
// seeds predate the expired state still knows what to do: nobody answered (NOT
// a decision); reopen a FRESH card with current context if the question still
// matters, otherwise close out / proceed. The task/step hold (if any) has
// already been released server-side.
//
// The body deliberately does NOT name a presser. It said "EXPIRED by owner"
// until T-1b88 (owner 2026-08-07, card rc-3ff94b116970) widened the verb to the
// card's own AUTHOR as well as the owner / an admin agent, so that wording
// would report the WRONG presser for every card its own author retires. WHO
// pressed it belongs to the byTrigger(trigger) suffix alone; the body only
// reports that no answer is coming.
//
// ⚠️ FOLLOW THE PATH BEFORE REASONING ABOUT WHAT AN AUTHOR SEES — an earlier
// version of this comment claimed the author would read "EXPIRED by owner … ·
// by <its own id>" and contradict itself, and BOTH halves of that are wrong:
//   - the LIVE delta never reaches this function for the author. dispatch()
//     drops self-triggered frames for every topic except member, and
//     replyCardTopic is not that exception, so handleReplyCard is not called.
//   - the BOOT/RECONNECT drain is therefore the only path that shows an author
//     its own expiry — and drainReplyCards passes trigger "", so byTrigger
//     renders nothing. There is no "· by <who>" suffix to contradict.
//
// PREMISE: both bullets rest on the client-side self-echo suppression. That
// premise IS guarded as of the third merge of main into this branch (T-f39c /
// T-2c6d): isSelfEcho has a table test, and drainChat's self-sent suppression
// has its own. The clause that used to stand here — "which no test currently
// guards" — was true when it was written and became false the moment those
// tests landed; a merge that reports zero conflicts does not tell you that a
// sentence in the file it merged has stopped being true.
//
// The correction stands on the first bullet alone: on the one path that does
// reach the author, the old body would silently credit the owner with a button
// the author itself pressed, and nothing on that line would say otherwise.
//
// The guidance half was reworded for the same reason: "the question may be
// stale" was written for a card SOMEONE ELSE retired for you.
//
// ⚠️ Whether being woken by your own withdrawal causes anything worse than a
// mis-attributed line is NOT settled and was deliberately left out of T-1b88.
// Note the suppression above makes the wake itself doubtful, so the first step
// is to establish whether that path exists at all rather than to fix it.
func printReplyCardExpired(out io.Writer, id string, card map[string]any, trigger string) {
	fmt.Fprintf(out, "[ocagent] reply-card %s EXPIRED (no answer) | asked: %s — "+
		"settled without an answer: if the question still matters, open a FRESH "+
		"card with current context; if not, proceed / close out. Any held "+
		"step/task was already restored to in_progress%s\n",
		id, renderMessageBody(strOrEmpty(card["summary"]), replyCardBodyAuthority), byTrigger(trigger))
}

// renderReplyCardAnswer renders a card's answer as ONE terse fragment: EVERY
// circled option's ORIGINAL wording (an index alone is meaningless to a
// session), any typed text, and an attachment count. This is the only place the
// owner's decision becomes words for the session that asked, so an option the
// owner circled and this line drops is a choice the agent never learns about.
//
// It accepts BOTH wire shapes the two callers feed it (T-3f31), and T-40
// changed BOTH of them:
//   - the FULL card (the live delta path's per-id refetch): answer.option_idxs
//     is a LIST of indices into the card's options, whose entries are now
//     OBJECTS ({text, ai_pick}) rather than strings; answer.attachments is the
//     refs ARRAY;
//   - the LIGHT list row (the boot/reconnect drain's answered pane): no options
//     ride the row — the digest carries the wording as answer.options (a list,
//     one entry per circled index) and the attachments as a COUNT (a number).
//
// ⚠️ A field RENAME is invisible to the READS here: a map lookup that misses
// returns the zero value and no error, so a pre-T-40 `answer["option_idx"]`
// would simply stop matching and the whole picked-options clause would vanish
// — no panic, no log. That silence used to reach the reader as a bare
// "(empty answer)", which is the owner's decision rendered as its opposite:
// he circled an option, and the line said he had not answered. Writing the
// hazard down in this comment did not stop it — it then happened (owner
// circled option [0]; the listener line printed "(empty answer)").
//
// So the no-parts path no longer claims emptiness. The server REFUSES an empty
// answer — applyReplyCardAnswer 400s when there is no option, no text and no
// attachment, and `option_idxs: []` counts as empty (see the len() guard there,
// and the same promise in the OpenAPI text mirrored at
// frontend/src/api/generated/schema.ts) — so an ANSWERED card cannot carry an
// empty answer. Reaching len(parts) == 0 with an answer present therefore means
// THIS BUILD COULD NOT READ IT, not that the owner said nothing, and the line
// says exactly that and points at get_reply_card.
//
// The two are NOT the same fact and must not print the same words:
//   - answer absent entirely (nil) — no answer rides this payload. Not an
//     error; nothing to parse.
//   - answer present but nothing parsed out of it — a parse failure. Loud.
func renderReplyCardAnswer(card map[string]any) string {
	answer, _ := card["answer"].(map[string]any)
	var parts []string
	if answer != nil {
		idxs, _ := answer["option_idxs"].([]any)
		// The light digest's own wording list wins when present; otherwise the
		// wording is resolved out of the full card's options array.
		digest, _ := answer["options"].([]any)
		fullOptions, _ := card["options"].([]any)
		for n, raw := range idxs {
			idx, ok := raw.(float64)
			if !ok {
				continue
			}
			i := int(idx)
			wording := ""
			if n < len(digest) {
				wording = strings.TrimSpace(strOrEmpty(digest[n]))
			}
			if wording == "" && i >= 0 && i < len(fullOptions) {
				opt, _ := fullOptions[i].(map[string]any)
				wording = strings.TrimSpace(strOrEmpty(opt["text"]))
			}
			if wording != "" {
				parts = append(parts, fmt.Sprintf("picked [%d] %q", i, wording))
			} else {
				parts = append(parts, fmt.Sprintf("picked [%d]", i))
			}
		}
		if text := strings.TrimSpace(strOrEmpty(answer["text"])); text != "" {
			parts = append(parts, fmt.Sprintf("%q", renderMessageBody(text, replyCardBodyAuthority)))
		}
		nAtts := 0
		switch atts := answer["attachments"].(type) {
		case []any: // full card: the refs array
			nAtts = len(atts)
		case float64: // light row: the digest COUNT
			nAtts = int(atts)
		}
		if nAtts > 0 {
			parts = append(parts, fmt.Sprintf("+%d attachment(s)", nAtts))
		}
	}
	if len(parts) == 0 {
		if answer == nil {
			// No answer on this payload at all — nothing was parsed because
			// there was nothing to parse. Not a failure.
			return "(no answer carried on this payload)"
		}
		// An answer IS present and nothing came out of it. The server refuses an
		// empty answer, so this is a READ failure in this build — say so, and
		// never let it read as "the owner did not answer".
		return unreadableAnswerNotice
	}
	return strings.Join(parts, " — ")
}

// unreadableAnswerNotice is what the answer line says when this build cannot
// turn a PRESENT answer into words. It must never be mistakable for "no answer":
// it names the failure as this reader's, and sends the reader to the authority
// (get_reply_card), which is a tool every member already holds.
//
// The recovery verb is restart_self, and that choice is not cosmetic. The
// obvious sentence — "restart the listener" — was WRONG for half the fleet and
// unverified for the other half (caught in review, 2026-08-31):
//
//   - A codex member must not do it. seeds/boot_sequence_codex.md step 3 says
//     verbatim 「不要自己啟動 `ocagent listen`、Monitor 或前景迴圈」; the sidecar
//     owns that process. Such a member reads this line and has no hand on it.
//   - Nobody had measured that restarting picks up the on-disk build. It is
//     plausible, and it was still an unverified claim printed as instruction.
//
// restart_self is an MCP tool BOTH runtimes hold, and it is already this
// listener's recovery verb elsewhere (the token-expiry line above says
// "checkpoint this turn, then call restart_self"). One verb, one meaning.
//
// It deliberately does NOT tell anyone to touch the updater or to upgrade
// another agent — neither is authorised here.
const unreadableAnswerNotice = "(UNREADABLE ANSWER — an answer IS recorded on this " +
	"card but this ocagent could not read it; do NOT treat this as \"no answer\". " +
	"Read the card itself with get_reply_card. This process may be older than the " +
	"station's answer shape; if it keeps happening, checkpoint and call restart_self.)"

// ---------------------------------------------------------------------------
// reply-card boot/reconnect drain (the offline-answer catch-up).
// ---------------------------------------------------------------------------
//
// /api/events has NO replay: the server hub buffers per LIVE connection only
// (the Last-Event-ID header the client sends is never read server-side), so a
// reply_card delta fanned while this agent held no stream — a killed listener,
// a handover window, a machine reboot — is gone for good and the live dispatch
// above never fires. The drain closes that hole: on EVERY successful stream
// open (boot and each reconnect) it refetches the answered AND expired panes
// (GET /api/reply-cards?status=answered / ?status=expired — the server's 24h
// authorities; LIGHT rows since T-3f31, whose decision digest — the circled
// options' wording in answer.options, text preview, attachment COUNT — is
// exactly what the printed line needs) and prints MY cards whose answer/expiry was not yet surfaced. Beyond
// the panes' 24h window the server keeps no listable answered/expired view, so
// an agent offline longer than a day reads older outcomes via get_reply_card,
// not the drain.
//
// The dedup state is (card id → answered_ts-or-expired_ts), persisted BESIDE
// the SSE cursor (<home>/<id-lower-or-anon>/replycards-seen) so it survives
// the exact process death the drain exists for. The ts is the key on purpose:
// a 重新決定 revision bumps answered_ts and re-prints (mirroring the live
// path), while an unchanged re-listed row stays quiet; the two ts kinds never
// collide on one card (expiry happens only from waiting — an expired card
// never printed an answer). The live handler records what it prints into the
// SAME state, so drain-after-delta and delta-after-drain both collapse to one
// line.
//
// FIRST RUN (no persisted state — a brand-new agent home): the drain only
// PRIMES the baseline, printing nothing. There is no "last seen" to diff
// against, and flooding a fresh session with stale already-answered history
// would be worse than the one lost print window (itself bounded by the pane's
// 24h retention). A corrupt state file re-primes the same silent way.

// replyCardSeen is the persisted answered-card dedup state shared by the
// drain and the live delta handler. Single-goroutine by construction (both
// callers run on the listen loop), so no lock.
type replyCardSeen struct {
	path   string
	m      map[string]float64 // card id → answered_ts as last surfaced
	primed bool               // a baseline exists (loaded from disk or persisted once)
}

// replyCardSeenPath is the state file, sibling of cursorPath.
func replyCardSeenPath(cfg Config) string {
	key := strings.ToLower(cfg.ID)
	if key == "" {
		key = "anon"
	}
	return filepath.Join(cfg.Home, key, "replycards-seen")
}

// loadReplyCardSeen reads the persisted state; a missing or corrupt file
// yields an UNPRIMED store (the first drain baselines silently).
func loadReplyCardSeen(path string) *replyCardSeen {
	s := &replyCardSeen{path: path, m: map[string]float64{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var m map[string]float64
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return s
	}
	s.m = m
	s.primed = true
	return s
}

func (s *replyCardSeen) has(id string, answeredTS float64) bool {
	ts, ok := s.m[id]
	return ok && ts == answeredTS
}

// record marks one answer surfaced and persists immediately — the state must
// survive a kill that lands right after the print.
func (s *replyCardSeen) record(id string, answeredTS float64) {
	s.m[id] = answeredTS
	s.persist()
}

func (s *replyCardSeen) persist() {
	raw, err := json.Marshal(s.m)
	if err != nil {
		return
	}
	if parent := filepath.Dir(s.path); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return
		}
	}
	if os.WriteFile(s.path, raw, 0o644) == nil {
		s.primed = true
	}
}

// drainReplyCards refetches the answered AND expired panes and prints MY
// not-yet-surfaced answers/expiries — the same lines the live handler emits,
// oldest first (per pane) so the session reads a chronology. It then REBUILDS
// the seen state from the panes (an entry absent from both has aged past the
// 24h window and can never drain again — dropping it keeps the file bounded; a
// later revision re-enters with a NEW answered_ts and prints as it should) and
// persists. A fault on EITHER pane (non-200 / junk body) prints nothing and
// leaves the state untouched — a partial rebuild would drop the other pane's
// entries and re-print them on the next drain; the next reconnect retries.
// Returns the printed count.
func drainReplyCards(client httpClient, cfg Config, seen *replyCardSeen, out io.Writer) int {
	panes := []struct {
		status string
		tsKey  string
		print  func(io.Writer, string, map[string]any, string)
	}{
		{replyCardAnswered, "answered_ts", printReplyCardAnswered},
		{replyCardExpired, "expired_ts", printReplyCardExpired},
	}
	lists := make([][]any, len(panes))
	for i, p := range panes {
		status, body := getJSON(client, cfg, "/api/reply-cards?status="+p.status, true)
		if status != 200 {
			return 0
		}
		list, ok := body.([]any)
		if !ok {
			return 0
		}
		lists[i] = list
	}
	mid := strings.ToLower(strings.TrimSpace(cfg.ID))
	silent := !seen.primed // first run ever: prime the baseline, print nothing
	fresh := map[string]float64{}
	n := 0
	for i, p := range panes {
		list := lists[i]
		for j := len(list) - 1; j >= 0; j-- { // pane is newest-first; print oldest-first
			card, ok := list[j].(map[string]any)
			if !ok {
				continue
			}
			id := strings.TrimSpace(strOrEmpty(card["id"]))
			if id == "" || strings.ToLower(strings.TrimSpace(strOrEmpty(card["from"]))) != mid {
				continue // not a card of mine — the pane is owner-wide
			}
			ts, _ := card[p.tsKey].(float64)
			if !silent && !seen.has(id, ts) {
				p.print(out, id, card, "")
				n++
			}
			fresh[id] = ts
		}
	}
	seen.m = fresh
	seen.persist()
	return n
}

// strOrEmpty mirrors Python's str(x or "") idiom for a JSON-decoded value: nil / empty
// string / 0 / false → "" (Python-falsy); a string → itself; any other scalar → its
// natural text. Used for the id/to/body fields drain_chat reads with `... or ""`.
func strOrEmpty(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if !t {
			return ""
		}
		return "True"
	case float64:
		if t == 0 {
			return ""
		}
		return pyStr(t)
	default:
		return pyStr(v)
	}
}
