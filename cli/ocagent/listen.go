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

// listen: the agent's long-lived GET /api/events SSE downlink.
//
// Holding this connection IS the agent's `online`: the server derives presence
// from the SSE connection, so listen reports no presence of its own. Hence the
// self-exit: a dead agent's orphaned listener would keep it online forever, so
// the listener probes its own tmux session (makeSessionProbe, folded in
// listen_run.go) and exits once it is gone, or unverifiable for too long.
//
// Every line printed on out reaches the agent: claude reads it via Monitor;
// codex via the ocwarden sidecar, which swallows "[ocagent] listen:" lines
// (except the transport notices; actionableCodexListenerLine) and turns every
// other line into a model turn.
//
// The SSE scan / reconnect / idle-watchdog mechanics are a copy-twin of
// cli/ocwarden/transport.go (ocwarden is package main and cannot be imported).

const (
	eventsPath = "/api/events"

	// stationSHAHeader rides the SSE response so learning the station's sha costs
	// no request: a version change restarts the station and reconnects the whole
	// fleet at once, so a separate version request would hit it N times at the
	// worst moment. Must equal sseStationSHAHeader in server/ocserverd/api_infra.go;
	// a typo fails silently (Header.Get returns "", same as "no sha sent").
	// bin/tests/station-sha-header-guard.sh compares the two.
	stationSHAHeader   = "X-Officraft-Station-Sha"
	membersPath        = "/api/members/"
	listenBackoffStart = 1 * time.Second
	listenBackoffCap   = 15 * time.Second

	sessionMissLimit = 2

	// probeUnknown*: a probe that cannot run (tmux unresolvable, spawn fault,
	// timeout) must eventually fail closed — fail-open let a zombie listener keep a
	// dead agent online indefinitely — but only past BOTH bounds, so a transient
	// exec/PATH hiccup never kills a healthy listener.
	probeUnknownMin   = 8
	probeUnknownGrace = 10 * time.Minute

	// sseRefusal*: only a standing run of authoritative refusals (see
	// errSSERefused) spanning BOTH bounds self-terminates; any other outcome
	// resets the run, so a flapping server cannot mass-kill agents. 120 s is a
	// tolerance, not a mirror of any server value. The ladder relies on the
	// server's stop gate letting a session still working its offboard sequence
	// reconnect (server/ocserverd/api_infra.go); without that it would kill agents
	// mid-hand-off on a station upgrade or network blip.
	sseRefusalMin   = 4
	sseRefusalGrace = 120 * time.Second

	// Setup-only bounds: never put a deadline on the body — it would cut the
	// always-open SSE every N seconds. Values match cli/ocwarden/transport.go.
	listenDialTimeout   = 10 * time.Second
	listenHeaderTimeout = 30 * time.Second
	// listenIdleReadTimeout ≈ 3× the server's ~15 s `: heartbeat`; silence past it
	// means a half-open link, so the connection is dropped and redialled.
	listenIdleReadTimeout = 45 * time.Second
	maxSSELine            = 8 << 20

	// defaultTmuxSocket must match cli/ocwarden/tmux.go tmuxSocket (the socket
	// agent sessions are spawned on); nothing checks the two agree.
	defaultTmuxSocket = "officraft"

	chatTopic      = "chat"
	memberTopic    = "member"
	desiredOffline = "offline"
	desiredOnline  = "online"

	replyCardTopic    = "reply_card"
	replyCardsPath    = "/api/reply-cards/"
	replyCardAnswered = "answered"
	replyCardExpired  = "expired"

	contextHighTopic = "context-high"
	tokenExpiryTopic = "token-expiry"
	taskCloseTopic   = "task-close"

	taskTopic = "task"
	tasksPath = "/api/tasks/"

	// messageBodyValve is an anti-blowup valve, NOT a preview cap. Chat bodies and
	// reply-card text are addressed to this agent, which reads them in full anyway,
	// so truncating only adds a get_chat / get_reply_card round trip whose JSON
	// envelope re-inflates the body 2–5×. 64 KiB prints every realistic
	// message whole (the owner's must-print size, 5,000 chars, is ~15 KiB; a
	// 20k-char SOP ~60 KiB) and trips only on a pathological paste.
	messageBodyValve = 64 << 10

	chatFullReadTool      = "get_chat"
	replyCardFullReadTool = "get_reply_card"
)

var dispatchTopics = map[string]bool{"action": true, "task": true}

var errSelfExit = errors.New("listen: tmux session gone — self-exit")

// Copies of server/ocserverd/authz.go's constants (separate module; the wire
// literal is the contract).
const (
	authRefusalHeader      = "X-OC-Auth-Refusal"
	refusalAgentSuperseded = "agent-superseded"
)

// errSSERefused marks a server refusal that is authoritative: a 409 (the
// server's zombie stop gate or dual-SSE guard) or a 401 marked agent-superseded
// (a newer generation of this member has reported waking).
var errSSERefused = errors.New("listen: server authoritatively refused the SSE connection")

type sseSink struct {
	// Fires on EVERY line, heartbeats included: the idle-read watchdog and
	// backoff reset depend on it.
	onActivity func()
	onData     func([]byte)
	onID       func(string)
	onComment  func() bool
}

// scanSSE: an incomplete final event (EOF before the blank line) is DISCARDED
// per the SSE spec — hence no flush after the loop.
func scanSSE(r io.Reader, sink sseSink) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxSSELine)
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
			sink.onActivity()
		}
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			if sink.onComment != nil && sink.onComment() {
				return errSelfExit
			}
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "data":
			data = append(data, value)
		case "id":
			if sink.onID != nil {
				sink.onID(strings.TrimSpace(value))
			}
		}
	}
	return sc.Err()
}

// nextBackoff: the jittered value also becomes the caller's next `current`, so
// the delay drifts — inherited from the Python listener, not a bug.
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

func defaultJitter() float64 { return 0.5 + rand.Float64()*0.5 }

func sseCursorPath(cfg Config) string {
	key := strings.ToLower(cfg.MemberID)
	if key == "" {
		key = "anon"
	}
	return filepath.Join(cfg.AgentsRoot, key, "sse-cursor")
}

func readSSECursor(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func writeSSECursor(path, seq string) {
	if parent := filepath.Dir(path); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return
		}
	}
	_ = os.WriteFile(path, []byte(seq), 0o644)
}

// resolveTmuxBin falls back to fixed install paths because launchd starts the
// process with a minimal PATH.
func resolveTmuxBin() string {
	if p, err := exec.LookPath("tmux"); err == nil && p != "" {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/tmux", "/usr/local/bin/tmux", "/usr/bin/tmux"} {
		if isExecutableFile(p) {
			return p
		}
	}
	return ""
}

func isExecutableFile(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}

type probeVerdict int

const (
	probeAlive probeVerdict = iota
	probeGone
	probeUnknown
)

// makeSessionProbe: the listener runs inside its agent's tmux session (the
// ocwarden spawner exports OC_SESSION / OC_TMUX_SOCKET), so "my session is gone"
// is the host-local death signal.
func makeSessionProbe(env func(string) string) func() probeVerdict {
	session := strings.TrimSpace(env("OC_SESSION"))
	if session == "" {
		return nil
	}
	socket := strings.TrimSpace(env("OC_TMUX_SOCKET"))
	if socket == "" {
		socket = defaultTmuxSocket
	}
	return func() probeVerdict {
		bin := resolveTmuxBin()
		if bin == "" {
			return probeUnknown
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "-L", socket, "has-session", "-t", session)
		err := cmd.Run()
		if err == nil {
			return probeAlive
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && ctx.Err() == nil {
			return probeGone
		}
		return probeUnknown
	}
}

func shouldDispatch(frame map[string]any) bool {
	if frame == nil {
		return false
	}
	topic, _ := frame["topic"].(string)
	return dispatchTopics[topic]
}

func isMemberFrameForSelf(frame map[string]any, selfID string) bool {
	if frame == nil {
		return false
	}
	if t, _ := frame["topic"].(string); t != memberTopic {
		return false
	}
	mid := strings.ToLower(strings.TrimSpace(selfID))
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
	// Member keys are server-scoped as <owner>::<id>.
	if i := strings.LastIndex(key, "::"); i >= 0 {
		key = key[i+2:]
	}
	return strings.ToLower(strings.TrimSpace(key)) == mid
}

// `trigger` is the server-verified actor ("owner" / "server" / a member id);
// an older producer sends none, so blank means unknown and is never suppressed.
func frameTrigger(frame map[string]any) string {
	if frame == nil {
		return ""
	}
	return strings.TrimSpace(strOrEmpty(frame["trigger"]))
}

// isSelfEcho: an agent connection only receives frames addressed to itself
// (spec/sse.md §4), so trigger == self is my own action pushed back at me.
func isSelfEcho(trigger, selfID string) bool {
	mid := strings.TrimSpace(selfID)
	return trigger != "" && mid != "" && strings.EqualFold(trigger, mid)
}

func byTrigger(trigger string) string {
	if trigger == "" {
		return ""
	}
	return " · by " + trigger
}

func previewLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// Continuation lines are indented so no body line can start at column 0 with
// "[ocagent] " and be read downstream as a separate event.
func renderMessageBody(s, authority string) string {
	if len(s) > messageBodyValve {
		cut := messageBodyValve
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + fmt.Sprintf("… [+%d bytes past the %d KiB safety valve — read the full message with %s]",
			len(s)-cut, messageBodyValve>>10, authority)
	}
	s = strings.TrimRight(s, "\n")
	return strings.ReplaceAll(s, "\n", "\n    ")
}

func printWake(frame map[string]any, trigger string, out io.Writer) {
	fmt.Fprintf(out, "[ocagent] wake seq=%s topic=%s%s\n",
		pyStr(frame["seq"]), pyStr(frame["topic"]), byTrigger(trigger))
}

var directedBandTopics = map[string]bool{
	contextHighTopic: true,
	tokenExpiryTopic: true,
	taskCloseTopic:   true,
}

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
			// Fallback only — the server always composes `reason`. Deliberately vaguer
			// than the server's text: a threshold invented here would be a second source
			// of truth.
			line = fmt.Sprintf("context usage high (level=%s pct=%s) — close out your "+
				"in-flight state before the handover", get("level"), get("pct"))
		case tokenExpiryTopic:
			line = fmt.Sprintf("agent token expires in %ss — checkpoint this turn, then call restart_self",
				get("expires_in"))
		case taskCloseTopic:
			line = fmt.Sprintf("task %s (type=%s) closed (%s) — walk your close-out: "+
				"clean this run's scratch, then report_task_closeout",
				get("task_no"), get("type"), get("status"))
		}
	}
	fmt.Fprintf(out, "[ocagent] signal %s: %s\n", topic, line)
}

type taskSnap struct {
	status string
	done   int
	total  int
}

func handleTaskEvent(client httpClient, cfg Config, frame map[string]any, snaps map[string]taskSnap, trigger string, out io.Writer) {
	id := ""
	if data, ok := frame["data"].(map[string]any); ok {
		if payload, ok := data["payload"].(map[string]any); ok {
			id = strings.TrimSpace(strOrEmpty(payload["id"]))
		}
	}
	if id == "" {
		printWake(frame, trigger, out)
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
		what = "status=" + now.status
	case now.status != prev.status:
		what = "status " + prev.status + " → " + now.status
	case now.done != prev.done || now.total != prev.total:
		what = "step done"
	default:
		what = "updated" // plan/deps/priority/notes
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
		sep = ""
	}
	fmt.Fprintf(out, "[ocagent] task %s%s%s%s%s\n", no, title, sep, what, byTrigger(trigger))
}

func intField(v any) int {
	f, _ := v.(float64)
	return int(f)
}

const chatUnreadPageLimit = 50

// chatUnreadMaxPages is a loop guard far above real traffic, not a documented
// limit — keep it out of the agent-facing seed (owner ruling rc-c31c54ca9b8b).
const chatUnreadMaxPages = 10

// A later-page fault keeps the rows in hand: the server serves unread
// oldest-first, so they are a contiguous run from the oldest and receipting them
// leaves the rest for the next drain.
type chatFetch struct {
	rows []map[string]any
	stop string
}

// fetchChat asks unread=true, not a ?with= window: a fixed newest-N window lets
// a long absence push the oldest unread out of reach. with= is omitted on purpose
// (recipient already pins the caller, and the server's unread index leads with
// recipient). The unread route marks nothing read.
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
		status, body := getJSON(client, cfg, path, true)
		env, _ := body.(map[string]any)
		list, listOK := env["messages"].([]any)
		if status != 200 || !listOK {
			if page == 1 {
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
			return chatFetch{rows: rows}
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

// listenAckEnv="1" is set by the parent (cli/ocwarden/codex_session.go) when a
// codex sidecar consumes stdout: each line must become an App Server turn, which
// can be refused, so printing proves nothing. Only the parent knows — never infer
// it from a tty, the parent process or the member id: a wrong guess (ack mode with
// nobody answering) hangs the drain. bin/listen-notice-mirror-guard.py holds the
// name equal to ocwarden's copy.
const listenAckEnv = "OC_LISTEN_ACK"

type ackGate struct {
	answers   <-chan string
	lastToken int
	wait      time.Duration
}

// ackWaitTimeout exists because confirm blocks the listener's only thread: an
// unanswered batch would otherwise leave the member deaf with nothing said.
// Timing out counts as a nack (the batch reprints next drain).
const ackWaitTimeout = 30 * time.Second

func newAckGate(env func(string) string, answers io.Reader) *ackGate {
	if env == nil || env(listenAckEnv) != "1" || answers == nil {
		return nil
	}
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

func (g *ackGate) confirm(out io.Writer) bool {
	if g == nil {
		return true
	}
	g.lastToken++
	token := strconv.Itoa(g.lastToken)
	fmt.Fprintf(out, "%s%s %s\n", agentLinePrefix, noticeBatch, token)
	deadline := time.After(g.wait)
	for {
		select {
		case line, open := <-g.answers:
			if !open {
				return false
			}
			verb, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
			if strings.TrimSpace(arg) != token {
				continue
			}
			return verb == "ack"
		case <-deadline:
			fmt.Fprintf(out, "%s等不到「已送達」的回覆（batch %s，等了 %s）—— "+
				"這一批訊息**沒有**被算成你看過了，下一次補印會再送一次。"+
				"如果這行一直出現，收訊這條路的另一端有問題。\n",
				agentLinePrefix, token, g.wait)
			return false
		}
	}
}

// drainWarner latches its lines because they lack the `listen:` head and a
// drain reruns every ≤ 15 s: unlatched, a standing fault is a codex model turn
// every 15 s (owner's disconnect-notice ruling, 2026-08-30).
type drainWarner struct {
	markReadWarned bool
	chatFaultOpen  bool
}

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

func clearChatFetchFault(warn *drainWarner, out io.Writer) {
	if warn == nil || out == nil || !warn.chatFaultOpen {
		return
	}
	warn.chatFaultOpen = false
	fmt.Fprint(out, "[ocagent] chat: 補印又問得到了 —— 上面那次「一頁都沒撈到」到此為止。"+
		"接下來印出來的就是這次真的撈到的東西；沒有東西就是真的沒有新訊息。\n")
}

// No local "already seen" ledger: the server's unread set is the only state and
// the read receipt the only thing that moves it (owner ruling rc-224dee5770dd).
func drainChat(client httpClient, cfg Config, out io.Writer, warn *drainWarner, gate *ackGate, clock func() time.Time) int {
	selfID := strings.ToLower(strings.TrimSpace(cfg.MemberID))
	now := float64(clock().Unix())
	msgs := fetchChat(client, cfg, cfg.MemberID)
	if msgs.rows == nil {
		noteChatFetchFault(warn, out, msgs.stop)
		return 0
	}
	clearChatFetchFault(warn, out)
	if msgs.stop != "" {
		fmt.Fprint(out, msgs.stop)
	}
	delivered := true
	unread := make([]map[string]any, 0, len(msgs.rows))
	var selfSent []map[string]any
	for _, m := range msgs.rows {
		if strings.ToLower(strings.TrimSpace(strOrEmpty(m["to"]))) != selfID {
			continue
		}
		// Self-sent rows are dropped by this client, not the API — the unread set no
		// longer excludes sender == caller (owner ruling rc-dccab860be32).
		if isSelfEcho(strings.TrimSpace(strOrEmpty(m["from"])), cfg.MemberID) {
			selfSent = append(selfSent, m)
			continue
		}
		unread = append(unread, m)
	}
	for _, m := range unread {
		printChatLine(out, m, now)
	}
	if len(unread) > 0 {
		delivered = gate.confirm(out)
	}
	// Self-sent rows are receipted unprinted (else they return on every walk), and
	// ONLY sender == this member: never widen it to other unprinted rows (owner
	// ruling rc-dccab860be32).
	toMarkRead := make([]map[string]any, 0, len(unread)+len(selfSent))
	if delivered {
		toMarkRead = append(toMarkRead, unread...)
	}
	toMarkRead = append(toMarkRead, selfSent...)
	reportChatRead(client, cfg, toMarkRead, warn, out)
	return len(unread)
}

// markReadPath (POST {peer, last_read_ts}): the server takes the reader from
// the verified JWT sub; the watermark is monotonic and a stale report is a
// no-op 200.
const markReadPath = "/api/chat/mark-read"

// reportChatRead files one receipt per SENDER. The listener has to: GET
// /api/chat no longer advances the watermark as a side effect. The watermark is
// per (reader, peer) and means "everything at or below this ts is read", so one
// batch-wide ts would mark A's conversation read up to B's message — and any
// skip in drainChat (a cap, a filter, an early break) marks unshown lines read;
// a former print cap did exactly that.
func reportChatRead(client httpClient, cfg Config, msgs []map[string]any, warn *drainWarner, out io.Writer) {
	high := map[string]float64{}
	for _, m := range msgs {
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
	sort.Strings(peers)
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

// Once per PROCESS, not per outage — deliberately: after a recovery a second
// failure reprints with nothing said. The per-outage latch (like chatFaultOpen)
// was rejected for the codex turn budget (see drainWarner).
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

// `from` is the server-stamped stable member id (never a display name) — the
// post_chat target; `#…` is the message id for get_chat. A reply shows only
// ↩#<id>: the quoted text is deliberately not printed although the wire carries
// it (reply_to_chat) — a per-message token cost; get_chat returns it.
func printChatLine(out io.Writer, m map[string]any, now float64) {
	msgID := strOrEmpty(m["id"])
	tag := make([]string, 0, 3)
	if msgID != "" {
		tag = append(tag, "#"+msgID)
	}
	if rt := strings.TrimSpace(strOrEmpty(m["reply_to"])); rt != "" {
		tag = append(tag, "↩#"+rt)
	}
	if ts, ok := m["ts"].(float64); ok && ts > 0 {
		tag = append(tag, fmtAgo(now-ts)+" ago")
	}
	content := renderMessageBody(strOrEmpty(m["body"]), chatFullReadTool)
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

// handleReplyCard: the reply_card delta fans out to every listener of the
// owner, but an answer is for the card's initiator alone — payload.from
// pre-filters before the refetch. A 重新決定 revision bumps answered_ts, so the
// seen dedup never swallows it.
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
	selfID := strings.ToLower(strings.TrimSpace(cfg.MemberID))
	from := strings.ToLower(strings.TrimSpace(strOrEmpty(payload["from"])))
	if from != "" && from != selfID {
		return
	}
	status, body := getJSON(client, cfg, replyCardsPath+url.PathEscape(id), true)
	card, ok := body.(map[string]any)
	if status != 200 || !ok {
		fmt.Fprintf(out, "[ocagent] reply-card %s changed but refetch failed (HTTP %d) — "+
			"read it manually (get_reply_card).\n", id, status)
		return
	}
	if strings.ToLower(strings.TrimSpace(strOrEmpty(card["from"]))) != selfID {
		return
	}
	switch strOrEmpty(card["status"]) {
	case replyCardAnswered:
		ts, _ := card["answered_ts"].(float64)
		if seen.has(id, ts) {
			return
		}
		printReplyCardAnswered(out, id, card, trigger)
		seen.record(id, ts)
	case replyCardExpired:
		// expired_ts never collides with an answered_ts for the same card: a card
		// expires only while waiting, so it never printed an answer.
		ts, _ := card["expired_ts"].(float64)
		if seen.has(id, ts) {
			return
		}
		printReplyCardExpired(out, id, card, trigger)
		seen.record(id, ts)
	default:
		return
	}
}

func printReplyCardAnswered(out io.Writer, id string, card map[string]any, trigger string) {
	fmt.Fprintf(out, "[ocagent] reply-card %s answered: %s | asked: %s%s\n",
		id, renderReplyCardAnswer(card),
		renderMessageBody(strOrEmpty(card["summary"]), replyCardFullReadTool), byTrigger(trigger))
}

// printReplyCardExpired carries its own guidance for agents whose seeds
// predate the expired state, and names no presser — the card's own author may
// expire it too (owner ruling rc-3ff94b116970); who pressed belongs to byTrigger.
func printReplyCardExpired(out io.Writer, id string, card map[string]any, trigger string) {
	fmt.Fprintf(out, "[ocagent] reply-card %s EXPIRED (no answer) | asked: %s — "+
		"settled without an answer: if the question still matters, open a FRESH "+
		"card with current context; if not, proceed / close out. Any held "+
		"step/task was already restored to in_progress%s\n",
		id, renderMessageBody(strOrEmpty(card["summary"]), replyCardFullReadTool), byTrigger(trigger))
}

// renderReplyCardAnswer takes both wire shapes: the FULL card (per-id refetch:
// answer.option_idxs index card.options objects {text, ai_pick}; attachments is
// the refs array) and the LIGHT list row (drain: answer.options holds the wording
// per circled index; attachments is a count).
//
// A renamed wire field fails silently (a missed map lookup returns zero) — it
// happened: the owner circled [0] and the line printed "(empty answer)". The
// server refuses an empty answer (applyReplyCardAnswer; option_idxs: [] counts
// as empty), so an answer that yields no parts is THIS build failing to read it.
func renderReplyCardAnswer(card map[string]any) string {
	answer, _ := card["answer"].(map[string]any)
	var parts []string
	if answer != nil {
		idxs, _ := answer["option_idxs"].([]any)
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
			parts = append(parts, fmt.Sprintf("%q", renderMessageBody(text, replyCardFullReadTool)))
		}
		nAtts := 0
		switch atts := answer["attachments"].(type) {
		case []any:
			nAtts = len(atts)
		case float64:
			nAtts = int(atts)
		}
		if nAtts > 0 {
			parts = append(parts, fmt.Sprintf("+%d attachment(s)", nAtts))
		}
	}
	if len(parts) == 0 {
		if answer == nil {
			return "(no answer carried on this payload)"
		}
		return unreadableAnswerNotice
	}
	return strings.Join(parts, " — ")
}

// unreadableAnswerNotice says restart_self, not "restart the listener": both
// seeds forbid a member from running `ocagent listen` (it belongs to the
// sidecar/ocwarden), and restart_self is a tool both runtimes hold. It must not
// tell anyone to touch the updater or upgrade another agent.
const unreadableAnswerNotice = "(UNREADABLE ANSWER — an answer IS recorded on this " +
	"card but this ocagent could not read it; do NOT treat this as \"no answer\". " +
	"Read the card itself with get_reply_card. This process may be older than the " +
	"station's answer shape; if it keeps happening, checkpoint and call restart_self.)"

// replyCardSeen: no lock — both callers run on the listen loop.
type replyCardSeen struct {
	path   string
	m      map[string]float64
	primed bool
}

func replyCardSeenPath(cfg Config) string {
	key := strings.ToLower(cfg.MemberID)
	if key == "" {
		key = "anon"
	}
	return filepath.Join(cfg.AgentsRoot, key, "replycards-seen")
}

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

func (s *replyCardSeen) has(id string, settledTS float64) bool {
	ts, ok := s.m[id]
	return ok && ts == settledTS
}

func (s *replyCardSeen) record(id string, settledTS float64) {
	s.m[id] = settledTS
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

// drainReplyCards exists because /api/events has no replay: the server
// buffers per live connection only and never reads Last-Event-ID, so a
// reply_card delta fanned while no stream was held is gone. The panes are the
// server's 24h views (older outcomes: get_reply_card); rebuilding seen from them
// prunes cards past that window. The first run (no state) prints nothing:
// flooding a fresh session with stale history is worse than the lost window.
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
	selfID := strings.ToLower(strings.TrimSpace(cfg.MemberID))
	silent := !seen.primed
	fresh := map[string]float64{}
	n := 0
	for i, p := range panes {
		list := lists[i]
		for j := len(list) - 1; j >= 0; j-- { // the server lists newest-first
			card, ok := list[j].(map[string]any)
			if !ok {
				continue
			}
			id := strings.TrimSpace(strOrEmpty(card["id"]))
			if id == "" || strings.ToLower(strings.TrimSpace(strOrEmpty(card["from"]))) != selfID {
				continue // the pane is owner-wide
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

// strOrEmpty keeps Python's str(x or "") semantics — 0 and false become "" —
// inherited from the Python listener.
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
