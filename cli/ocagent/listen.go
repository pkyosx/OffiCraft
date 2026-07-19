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
// self-stop hook (reports the presence PHASE then `suicide`s) and the recycle hook
// (WAKE-ONLY: prints the handover SOP for the session — see listen_hooks.go).
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
	// refusal (zombie, stale dual-SSE twin) crosses both bounds. The grace
	// mirrors the server's 120 s stop_grace.
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

	// memberTopic / desiredOffline / desiredOnline mirror the wire literals.
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

	// contextHighTopic / taskCloseTopic mirror the server's directed band
	// topic constants (server/ocserverd/sse_bands.go; spec/sse.md §6/§8).
	contextHighTopic = "context-high"
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

// errSSERefused is the sentinel connectOnce wraps a pre-stream HTTP 409 in: the
// server AUTHORITATIVELY refused this listener (the zombie stop gate, or the
// dual-SSE single-session guard). The run loop folds consecutive refusals into
// the fail-closed self-terminate (see sseRefusalMin/sseRefusalGrace).
var errSSERefused = errors.New("listen: server refused the SSE connection (409)")

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
// Local state is pure optimisation/dedup (this cursor + the reply-card seen file
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
// sse_bands.go contextHighTopic / taskCloseTopic; spec/sse.md §6/§8). Unlike
// the entity-delta topics these carry a server-composed human message the
// agent must actually READ — so they print, they don't just wake.
var directedBandTopics = map[string]bool{
	contextHighTopic: true,
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
			line = fmt.Sprintf("context usage high (level=%s pct=%s) — start converging",
				get("level"), get("pct"))
		case taskCloseTopic:
			line = fmt.Sprintf("task %s (type=%s) closed (%s) — fold this run's "+
				"learnings back into the manual (write_task_learnings)",
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

// fetchChat refetches the AUTHORITATIVE chat list for selfID from
// GET /api/chat?with=<selfID> — owner-scoped + participant-filtered by the SERVER.
// Returns the wire ChatMessageDTOs (from/to/body/id/ts), oldest→newest, or nil on any
// fault. Mirrors fetch_chat.
func fetchChat(client httpClient, cfg Config, selfID string) []map[string]any {
	path := "/api/chat?with=" + url.QueryEscape(selfID)
	status, body := getJSON(client, cfg, path, true)
	if status != 200 {
		return nil
	}
	list, ok := body.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
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

// drainChat refetches chat and prints the unread-for-me — ONE LINE per message so the
// spawned session's Monitor reads exactly '誰、多久前、說了什麼':
//
//	[ocagent] chat from MB-ABC123 (id, 2m ago): ...
//
// The `(id, …)` tag spells out that `from` is the STABLE member id (server-stamped,
// never a display name) — reply straight to it with post_chat; the relative age is
// computed client-side from the message ts (dropped when the wire carries no ts).
// Advances the seen-id cursor and returns the unread count. `silent` (the boot
// baseline) advances the cursor WITHOUT printing so connecting does not re-print
// history. R7: reads ONLY the refetched authority, never a delta. Mirrors drain_chat.
// attachmentSummary renders a message's attachments as a terse badge appended
// after the body: "📎2圖" (2 images), "📎1檔" (1 non-image file), or the mixed
// "📎1圖 2檔". Images are counted by the server-computed is_image flag. Returns
// "" when the message carries no attachments, so a zero-attachment line stays
// byte-for-byte unchanged. Junk-safe: a non-array attachments field or non-map
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

func drainChat(client httpClient, cfg Config, seen map[string]bool, out io.Writer, silent bool) int {
	sid := strings.ToLower(strings.TrimSpace(cfg.ID))
	now := float64(time.Now().Unix())
	n := 0
	for _, m := range fetchChat(client, cfg, cfg.ID) {
		if strings.ToLower(strings.TrimSpace(strOrEmpty(m["to"]))) != sid {
			continue
		}
		mid := strOrEmpty(m["id"])
		if mid != "" && seen[mid] {
			continue
		}
		if !silent {
			tag := "id"
			if ts, ok := m["ts"].(float64); ok && ts > 0 {
				tag = "id, " + fmtAgo(now-ts) + " ago"
			}
			content := renderMessageBody(strOrEmpty(m["body"]), chatBodyAuthority)
			if badge := attachmentSummary(m); badge != "" {
				if content == "" {
					content = badge
				} else {
					content += " " + badge
				}
			}
			fmt.Fprintf(out, "[ocagent] chat from %s (%s): %s\n",
				pyStr(m["from"]), tag, content)
		}
		if mid != "" {
			seen[mid] = true
		}
		n++
	}
	return n
}

// ---------------------------------------------------------------------------
// reply-card downlink (R7: the delta payload is a hint — refetch the authority).
// ---------------------------------------------------------------------------

// handleReplyCard wakes the session when a reply card THIS agent opened got its
// answer — or was marked EXPIRED by the owner (標為過期: not an answer; the
// owner is saying the ask went stale, and this agent decides itself whether to
// reopen a fresh card or move on). The delta payload ({id, from, status}) is
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
// seeds predate the expired state still knows what to do: the owner declined
// to answer (NOT a decision); reopen a FRESH card with current context if the
// question still matters, otherwise close out / proceed. The task/step hold
// (if any) has already been released server-side.
func printReplyCardExpired(out io.Writer, id string, card map[string]any, trigger string) {
	fmt.Fprintf(out, "[ocagent] reply-card %s EXPIRED by owner (no answer) | asked: %s — "+
		"the question may be stale: if it still matters, open a FRESH card with "+
		"current context; if not, proceed / close out. Any held step/task was "+
		"already restored to in_progress%s\n",
		id, renderMessageBody(strOrEmpty(card["summary"]), replyCardBodyAuthority), byTrigger(trigger))
}

// renderReplyCardAnswer renders a card's answer as ONE terse fragment: the
// picked option's ORIGINAL wording (the option index alone is meaningless to
// a session), any typed text, and an attachment count. It accepts BOTH wire
// shapes the two callers feed it (T-3f31):
//   - the FULL card (the live delta path's per-id refetch): the wording sits
//     in the card's options array; answer.attachments is the refs ARRAY;
//   - the LIGHT list row (the boot/reconnect drain's answered pane): no
//     options ride the row — the digest carries the wording as answer.option
//     and the attachments as a COUNT (a JSON number).
func renderReplyCardAnswer(card map[string]any) string {
	answer, _ := card["answer"].(map[string]any)
	var parts []string
	if answer != nil {
		if idx, ok := answer["option_idx"].(float64); ok {
			i := int(idx)
			wording := strings.TrimSpace(strOrEmpty(answer["option"])) // light digest
			if wording == "" {
				if opts, _ := card["options"].([]any); i >= 0 && i < len(opts) {
					wording = strOrEmpty(opts[i]) // full card
				}
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
		return "(empty answer)"
	}
	return strings.Join(parts, " — ")
}

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
// authorities; LIGHT rows since T-3f31, whose decision digest — answer.option
// wording, text preview, attachment COUNT — is exactly what the printed line
// needs) and prints MY cards whose answer/expiry was not yet surfaced. Beyond
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
