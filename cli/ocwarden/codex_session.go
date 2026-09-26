package main

// codex_session.go is the Codex counterpart to Claude's direct TUI launch: an
// ocwarden sidecar owns one stdio Codex App Server and turns `ocagent listen`
// output into turn/start or turn/steer calls, keeping lifecycle and SSE
// ownership identical to the Claude design.

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func (s *codexSession) reportRejectedCodexPost(path string, status int) {
	if status >= http.StatusBadRequest {
		s.activity("Codex POST %s rejected with HTTP %d", path, status)
	}
}

func buildCodexLaunchCommand(wardenBin, codexBin, workdir, personaFile, tokenFile,
	agentID, base, session, socket, model, effort string, extraEnv [][2]string,
	envRendered string, logf func(string, ...any)) string {
	cd := "cd " + shellQuote(workdir) + "; "
	if envRendered != "" {
		cd += "[ -f " + shellQuote(envRendered) + " ] && . " + shellQuote(envRendered) + "; "
	}
	pairs := [][2]string{
		{"OC_BASE", base},
		{"OC_ID", agentID},
		{"OC_SESSION", session},
		{"OC_TMUX_SOCKET", socket},
	}
	pairs = append(pairs, extraEnv...)
	kvs := []string{`OC_TOKEN="$(/bin/cat ` + shellQuote(tokenFile) + `)"`}
	for _, pair := range pairs {
		kvs = append(kvs, pair[0]+"="+shellQuote(pair[1]))
	}
	exports := "export " + strings.Join(kvs, " ") + "; "
	exports += "export PATH=" + shellQuote(workdir) + `:"$PATH"; `
	launchEffort, recognised := normalizeCodexEffort(effort)
	if !recognised && logf != nil {
		logf("codex launch: effort %q is not a level this warden knows; launching at %q. "+
			"The cockpit will keep showing %q, so this line is the only place the "+
			"difference is visible — upgrade the warden if the server has grown a level.",
			effort, launchEffort, effort)
	}
	parts := []string{
		shellQuote(wardenBin), "codex-session",
		"--codex-bin", shellQuote(codexBin),
		"--workdir", shellQuote(workdir),
		"--persona", shellQuote(personaFile),
		"--agent-id", shellQuote(agentID),
		"--model", shellQuote(model),
		"--effort", shellQuote(launchEffort),
	}
	return cd + exports + "exec " + strings.Join(parts, " ")
}

// Coerces rather than refuses: a warden is upgraded separately from the
// server, so a server that has grown a level runs ahead of some warden for a
// while, and refusing would turn "wrong effort" into "cannot boot at all". The
// second return exists so the caller says the coercion out loud.
func normalizeCodexEffort(effort string) (string, bool) {
	switch strings.TrimSpace(effort) {
	case "low", "medium", "high", "xhigh", "max":
		return strings.TrimSpace(effort), true
	case "":
		// Not a coercion: an omitted effort has always meant medium, and an old
		// frame's launch line must stay byte-identical.
		return "medium", true
	default:
		return "medium", false
	}
}

func codexPersonaInstruction(personaFile, model string) string {
	instruction := "Read " + personaFile +
		" completely before acting, first line to LAST line, and read it with your shell — " +
		"that is the only tool you have that can open a local file. " +
		"Read it like this: (1) run `wc -l " + personaFile + "` first and write down the total " +
		"line count N; (2) then walk the file in order with `sed -n 'START,ENDp' " + personaFile +
		"`, about 200 lines per call, until a call has returned line N; (3) after EVERY call " +
		"check that the output really ends at the line you asked for — shell output is truncated " +
		"silently, with no error of any kind, so a short chunk means you must re-read that range " +
		"in smaller pieces, never skip ahead. " +
		"You have read the file ONLY when every line from 1 to N has come back untruncated: " +
		"reaching the LAST line is what finishes the read, not reaching a part that looks like an " +
		"ending, and the 開機程序 (boot sequence) section is at the very END of the file. " +
		"Do not tell anyone you have read it until then. " +
		"It is your OffiCraft identity and operating context. " +
		"Never use request_user_input for normal questions; create an OffiCraft reply card instead. "
	if strings.TrimSpace(model) == "" {
		return instruction +
			"The OffiCraft launch model setting is blank, so the machine's Codex default applies. " +
			"If your role's boot sequence calls report_waking, omit its optional model argument; " +
			"never guess or persist a model name."
	}
	return instruction + "The explicit OffiCraft launch model is " + model +
		". If your role's boot sequence calls report_waking, pass that exact value as its model argument. " +
		"Follow your role-specific boot sequence when it says not to call report_waking."
}

type appServerMessage map[string]any

type codexSession struct {
	in               io.WriteCloser
	messages         <-chan appServerMessage
	writeMu          sync.Mutex
	nextID           int
	threadID         string
	turnID           string
	active           bool
	base             string
	token            string
	workdir          string
	model            string
	effort           string
	account          string
	out              io.Writer
	compactions      int
	telemetryMu      sync.Mutex
	lastUsageReport  time.Time
	forceUsageReport bool
	rateLimitReadID  int
	// Replayed item/completed notifications must not look like fresh
	// compactions and recycle a just-booted agent.
	completedCompactions map[string]struct{}

	pending map[int]*codexDelivery
	batch   *codexBatch
	ackTo   io.Writer
}

type codexDelivery struct {
	method string
	text   string
	batch  *codexBatch
}

// The listener is BLOCKED on this batch's verdict: until it arrives it files no
// read receipt, so a batch that never lands is printed again on the next drain.
// One failed delivery nacks the whole batch — a re-print is the safe direction.
type codexBatch struct {
	token       string
	closed      bool
	outstanding int
	failed      bool
	answered    bool
}

const codexTelemetryThrottle = 30 * time.Second
const codexAppResponseTimeout = 30 * time.Second

func (s *codexSession) allowUsageReport() bool {
	s.telemetryMu.Lock()
	defer s.telemetryMu.Unlock()
	now := time.Now()
	if !s.lastUsageReport.IsZero() && !s.forceUsageReport && now.Sub(s.lastUsageReport) < codexTelemetryThrottle {
		return false
	}
	s.lastUsageReport = now
	s.forceUsageReport = false
	return true
}

// Versions the hash's input semantics (v1 hashed the workspace id, v2 hashes
// the person) so the two generations never merge into one row. A mixed fleet
// shows one person as TWO rows until the last warden is upgraded, and the
// owner's `account_alias` rows keyed on v1 become orphans that must be
// re-aliased once.
const codexAccountKeyVersion = "officraft-codex-account-v2:"

// Keyed on the id_token claim `https://api.openai.com/auth`.`chatgpt_user_id`:
// per person, identical on every machine, unchanged by token refresh. Rejected:
// account_id / chatgpt_account_id (workspace-scoped — everyone in a workspace
// collapsed into one row), email / name (mutable, PII), `sub` (changes with the
// login connection), sid / jti / at_hash (per token). The workspace id is
// deliberately NOT mixed in. The raw claim is never returned, logged, or posted.
func codexAccountKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return codexAccountKeyForHome(home)
}

// Tests must never point this at a real home: ~/.codex/auth.json holds live
// credentials.
func codexAccountKeyForHome(home string) string {
	raw, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		return ""
	}
	var auth struct {
		Tokens struct {
			IDToken string `json:"id_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &auth) != nil {
		return ""
	}
	user := codexUserIDFromIDToken(auth.Tokens.IDToken)
	if user == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(codexAccountKeyVersion + user))
	return "codex:" + fmt.Sprintf("%x", sum[:])
}

// The signature is deliberately not verified: this is Codex's own local
// credential store, not a trust boundary.
//
// Every failure returns "" — never fall back to the workspace id, which would
// silently restore the v1 collision. Accepted trade-off: the server treats an
// empty account as a no-op, so a machine that stops reading the claim keeps its
// last key until the server restarts; if that needs solving, solve it
// server-side.
func codexUserIDFromIDToken(idToken string) string {
	parts := strings.Split(strings.TrimSpace(idToken), ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims struct {
		OpenAI struct {
			ChatGPTUserID string `json:"chatgpt_user_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return strings.TrimSpace(claims.OpenAI.ChatGPTUserID)
}

// Lifecycle only — never raw model prompts, tool arguments, or response
// bodies.
func (s *codexSession) activity(format string, args ...any) {
	if s.out == nil {
		return
	}
	fmt.Fprintf(s.out, "%s [codex] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

func (s *codexSession) send(method string, params map[string]any) int {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.nextID++
	id := s.nextID
	_ = json.NewEncoder(s.in).Encode(appServerMessage{
		"id": id, "method": method, "params": params,
	})
	return id
}

func (s *codexSession) notify(method string, params map[string]any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = json.NewEncoder(s.in).Encode(appServerMessage{"method": method, "params": params})
}

func messageID(msg appServerMessage) int {
	switch value := msg["id"].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
}

func nestedString(obj map[string]any, keys ...string) string {
	var current any = obj
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	text, _ := current.(string)
	return text
}

func (s *codexSession) waitResponse(id int) (appServerMessage, error) {
	timer := time.NewTimer(codexAppResponseTimeout)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return nil, fmt.Errorf("app-server request %d timed out after %s", id, codexAppResponseTimeout)
		case msg, ok := <-s.messages:
			if !ok {
				return nil, errors.New("app-server exited before responding")
			}
			if messageID(msg) != id {
				continue
			}
			if problem, ok := msg["error"].(map[string]any); ok {
				return nil, fmt.Errorf("app-server request failed: %v", problem["message"])
			}
			return msg, nil
		}
	}
}

func (s *codexSession) startTurn(text string, batch *codexBatch) {
	s.activity("turn started")
	params := map[string]any{
		"threadId": s.threadID,
		"input":    []any{map[string]any{"type": "text", "text": text}},
		"effort":   s.effort,
	}
	s.track(s.send("turn/start", params), &codexDelivery{
		method: "turn/start", text: text, batch: batch,
	})
}

func (s *codexSession) steerOrStart(text string, batch *codexBatch) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if s.active && s.turnID != "" {
		s.activity("turn steered by OffiCraft event")
		s.track(s.send("turn/steer", map[string]any{
			"threadId": s.threadID, "expectedTurnId": s.turnID,
			"input": []any{map[string]any{"type": "text", "text": text}},
		}), &codexDelivery{method: "turn/steer", text: text, batch: batch})
		return
	}
	s.startTurn(text, batch)
}

func (s *codexSession) track(id int, d *codexDelivery) {
	if id == 0 {
		return
	}
	if s.pending == nil {
		s.pending = map[int]*codexDelivery{}
	}
	s.pending[id] = d
	if d.batch != nil {
		d.batch.outstanding++
	}
}

// ⚠️ Three wirings in runCodexSession's select loop are pinned by no test: the
// call to this method, `s.ackTo = ackPipe`, and `listenerCmd.Env =
// codexListenerEnv(...)`. Delete any of them and every test stays green while
// acks stop (every drain blocks forever) or the listener never enters ack mode.
//
// A refused turn/steer gets ONE second chance as a fresh turn: the common
// refusal is a stale expectedTurnId (turn/completed is in flight and unread).
func (s *codexSession) resolveResponse(id int, msg appServerMessage) {
	d, ok := s.pending[id]
	if !ok {
		return
	}
	delete(s.pending, id)
	if d.batch != nil {
		d.batch.outstanding--
	}
	problem, failed := msg["error"].(map[string]any)
	if !failed {
		s.confirmDelivered(d)
		return
	}
	detail := strings.TrimSpace(fmt.Sprintf("%v", problem["message"]))
	if d.method == "turn/steer" {
		s.activity("turn/steer 被拒（%s）— 改開新的一輪重送同一段內容", detail)
		s.startTurn(d.text, d.batch)
		return
	}
	s.activity("⚠️ 送不進去（%s）：%s — 這段內容沒有進到 agent 的對話，"+
		"agent 不會知道有人說過這句話", detail, codexDeliveryLabel(d.text))
	if d.batch != nil {
		d.batch.failed = true
	}
	s.settleBatch(d.batch)
}

// Why a second piece of evidence: the listener blocks on this sidecar's
// verdict, and turn/start's response may only arrive when the turn ENDS, which
// would leave the member deaf for the whole turn. `turn/started` suffices;
// whichever evidence arrives first settles the delivery.
func (s *codexSession) confirmStartedTurn() {
	oldest := 0
	for id, d := range s.pending {
		if d.method != "turn/start" {
			continue
		}
		if oldest == 0 || id < oldest {
			oldest = id
		}
	}
	if oldest == 0 {
		return
	}
	d := s.pending[oldest]
	delete(s.pending, oldest)
	if d.batch != nil {
		d.batch.outstanding--
	}
	s.confirmDelivered(d)
}

func (s *codexSession) confirmDelivered(d *codexDelivery) {
	s.activity("已送進對話：%s", codexDeliveryLabel(d.text))
	s.settleBatch(d.batch)
}

func codexDeliveryLabel(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	if runes := []rune(text); len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return text
}

func (s *codexSession) closeBatch(token string) {
	group := s.batch
	s.batch = nil
	if group == nil {
		// An empty batch is confirmed, not left open: the listener is blocked on
		// this line.
		group = &codexBatch{}
	}
	group.token = token
	group.closed = true
	s.settleBatch(group)
}

func (s *codexSession) currentBatch() *codexBatch {
	if s.batch == nil {
		s.batch = &codexBatch{}
	}
	return s.batch
}

func (s *codexSession) settleBatch(group *codexBatch) {
	if group == nil || !group.closed || group.answered || group.outstanding > 0 {
		return
	}
	group.answered = true
	verb := "ack"
	if group.failed {
		verb = "nack"
	}
	if group.failed {
		s.activity("批次 %s 沒能送進對話 — 已告訴 listener 不要標已讀，下一輪會重印", group.token)
	}
	if s.ackTo == nil {
		return
	}
	_, _ = fmt.Fprintf(s.ackTo, "%s %s\n", verb, group.token)
}

func codexAppReader(r io.Reader) <-chan appServerMessage {
	out := make(chan appServerMessage, 64)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 8<<20)
		for scanner.Scan() {
			var msg appServerMessage
			if json.Unmarshal(scanner.Bytes(), &msg) == nil {
				out <- msg
			}
		}
	}()
	return out
}

func jsonNumber(value any) float64 {
	number, _ := value.(float64)
	return number
}

func (s *codexSession) post(path string, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(s.base, "/")+path,
		bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err == nil {
		s.reportRejectedCodexPost(path, resp.StatusCode)
		_ = resp.Body.Close()
	}
}

func (s *codexSession) reportIdentity() {
	s.post("/api/monitoring/telemetry", map[string]any{
		"runtime": "codex", "account": s.account, "account_label": "ChatGPT",
	})
}

func (s *codexSession) requestRateLimits() {
	if s.rateLimitReadID == 0 {
		s.rateLimitReadID = s.send("account/rateLimits/read", nil)
	}
}

// App Server versions have returned the snapshot both directly and nested in
// `rateLimits`; notifications always use the nested form.
func rateLimitSnapshot(result map[string]any) map[string]any {
	if nested, _ := result["rateLimits"].(map[string]any); nested != nil {
		return nested
	}
	return result
}

func (s *codexSession) reportTokenUsage(params map[string]any) {
	if !s.allowUsageReport() {
		return
	}
	usage, _ := params["tokenUsage"].(map[string]any)
	total, _ := usage["total"].(map[string]any)
	last, _ := usage["last"].(map[string]any)
	window := jsonNumber(usage["modelContextWindow"])
	// "total" is cumulative across the thread and can exceed one context
	// window; "last" is the current turn's context gauge.
	used := jsonNumber(last["totalTokens"])
	if window > 0 {
		s.activity("context %.0f%% · compact %d", used/window*100, s.compactions)
		s.post("/api/agent/context", map[string]any{
			"context_pct":      used / window * 100,
			"compaction_count": s.compactions,
		})
	}
	tokens := map[string]any{}
	for _, key := range []string{
		"inputTokens", "cachedInputTokens", "outputTokens",
		"reasoningOutputTokens", "totalTokens",
	} {
		if value, ok := total[key]; ok {
			tokens[key] = value
		}
	}
	body := map[string]any{
		"runtime": "codex", "tokens": tokens, "effort": s.effort,
		"account": s.account, "account_label": "ChatGPT",
	}
	// A blank model is OMITTED, not sent as "": it means the machine's Codex
	// default is in force and the name is unknown, and "" would record that
	// unknown as a reported blank.
	if m := strings.TrimSpace(s.model); m != "" {
		body["model"] = m
	}
	s.post("/api/monitoring/telemetry", body)
}

func (s *codexSession) reportRateLimits(snapshot map[string]any) {
	windows := map[string]any{}
	for _, key := range []string{"primary", "secondary"} {
		w, _ := snapshot[key].(map[string]any)
		mins := jsonNumber(w["windowDurationMins"])
		used := jsonNumber(w["usedPercent"])
		if w == nil || mins <= 0 {
			continue
		}
		name := "seven_day"
		if mins <= 360 {
			name = "five_hour"
		}
		windows[name] = map[string]any{"used_percentage": used, "resets_at": w["resetsAt"]}
	}
	if len(windows) == 0 {
		return
	}
	s.post("/api/monitoring/telemetry", map[string]any{
		"runtime": "codex", "account": s.account, "account_label": "ChatGPT", "rate_limits": windows,
	})
}

// Compaction is an item, not a turn; the deprecated thread/compacted echo is
// intentionally ignored.
func (s *codexSession) recordCompaction(params map[string]any) {
	item, _ := params["item"].(map[string]any)
	if item == nil || item["type"] != "contextCompaction" {
		return
	}
	id, _ := item["id"].(string)
	if id == "" {
		return
	}
	if s.completedCompactions == nil {
		s.completedCompactions = make(map[string]struct{})
	}
	if _, seen := s.completedCompactions[id]; seen {
		return
	}
	s.completedCompactions[id] = struct{}{}
	s.compactions++
	s.telemetryMu.Lock()
	s.forceUsageReport = true
	s.telemetryMu.Unlock()
	s.activity("context compacted · count %d", s.compactions)
}

// 🔴 The secret warning must ride in THIS text: the warden no longer opens the
// card, so nothing else tells Codex not to type a secret into the card body.
func codexOpenYourOwnCardMessage(question map[string]any) string {
	message := "OffiCraft does not open reply cards on your behalf. Open it yourself with the " +
		"create_reply_card tool, then end this turn and wait for its SSE answer event. " +
		"linked_task is required: send {\"task_id\": ..., \"step_id\": ...} for the step this " +
		"question is about, or null if it is not about a task."
	if secret, _ := question["isSecret"].(bool); secret {
		message += " 這是秘密資料請求；請只完成所需動作，不要把秘密貼進卡片。"
	}
	return message
}

func (s *codexSession) handleServerRequest(msg appServerMessage) {
	method, _ := msg["method"].(string)
	s.activity("native user-input request → OffiCraft reply card")
	// App Server RequestId is string | int64. Echo the exact JSON value back;
	// coercing a string id to zero would leave Codex waiting forever.
	id := msg["id"]
	params, _ := msg["params"].(map[string]any)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	enc := json.NewEncoder(s.in)
	switch method {
	case "item/tool/requestUserInput":
		// The warden does not open the card on Codex's behalf: it holds no
		// task_id / step_id, and create_reply_card requires linked_task. Answer
		// with text rather than parking the model on a terminal round-trip.
		answers := map[string]any{}
		questions, _ := params["questions"].([]any)
		for _, raw := range questions {
			question, _ := raw.(map[string]any)
			qid, _ := question["id"].(string)
			message := codexOpenYourOwnCardMessage(question)
			answers[qid] = map[string]any{"answers": []string{message}}
		}
		_ = enc.Encode(appServerMessage{"id": id, "result": map[string]any{"answers": answers}})
	case "mcpServer/elicitation/request":
		_ = enc.Encode(appServerMessage{"id": id, "result": map[string]any{"action": "decline"}})
	default:
		_ = enc.Encode(appServerMessage{"id": id, "error": map[string]any{
			"code": -32601, "message": "OffiCraft sidecar does not support this server request",
		}})
	}
}

type codexListenerState struct{ wakeSent bool }

// ⚠️ Deleting the CALL to this method leaves the whole ocwarden suite green;
// only uplink-guard reddens, incidentally, because uplinks.json's codex-hop-4
// anchors on the reportIdentity/requestRateLimits pair in the closure passed
// here. Move those calls out and nothing announces the loss.
func (st *codexListenerState) handleListenerLine(
	line string, onConnect func(), openTurn func(string), onBatch func(string),
) {
	if token, ok := codexBatchToken(line); ok {
		if onBatch != nil {
			onBatch(token)
		}
		return
	}
	wake, forward := codexListenerActions(line, st.wakeSent)
	if strings.HasPrefix(strings.TrimSpace(line), noticeConnectedPrefix) {
		onConnect()
	}
	// ONCE per session, not on reconnects: the wake continues an unfinished
	// boot, and re-opening it on every blip would re-do work and interrupt
	// the agent. A later connect is forwarded as the reconnect notice
	// instead (owner's disconnect-notice policy, 2026-08-30).
	if wake {
		st.wakeSent = true
		openTurn(codexPostBootWake)
	}
	if forward {
		openTurn(line)
	}
}

// A named method, not a closure inside the loop, so a test can drive the
// delivery: replacing it with a no-op inside the loop left every suite green.
func (s *codexSession) openListenerTurn(text string) {
	if text == codexPostBootWake {
		s.activity("waking the session now that SSE is up")
	} else {
		s.activity("OffiCraft event: %s", text)
	}
	s.steerOrStart(text, s.currentBatch())
}

// The ack-mode flag: a listener child started without it looks healthy and
// loses mail silently.
func codexListenerEnv(base []string) []string {
	return append(append([]string{}, base...), listenAckEnv+"=1")
}

func codexBatchToken(line string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), noticeBatchPrefix)
	if !ok {
		return "", false
	}
	// The listener stamps every transcript line with a trailing `[ts=… local]`,
	// so the token is the FIRST field of the remainder.
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", false
	}
	return fields[0], true
}

func codexListenerActions(line string, wakeAlreadySent bool) (wake, forward bool) {
	connected := strings.HasPrefix(strings.TrimSpace(line), noticeConnectedPrefix)
	wake = connected && !wakeAlreadySent
	return wake, actionableCodexListenerLine(line) && !wake
}

// Owner's disconnect-notice policy (2026-08-30): tell the agent at the first
// disconnect and when the stream is back, not on every retry. The give-up line
// is included so the agent can tell 還在重試 from 已經放棄.
//
// 🔴 These bytes are printed by a DIFFERENT Go module (cli/ocagent's notice*
// constants) that this one cannot import; bin/listen-notice-mirror-guard.py
// holds the two copies together. It catches the sides walking apart, not a
// wrong spelling renamed consistently on both.
const (
	noticeDisconnectedPrefix = "[ocagent] listen: disconnected"
	noticeConnectedPrefix    = "[ocagent] listen: connected"
	noticeGivingUpPrefix     = "[ocagent] listen: giving up"

	// Protocol between the two processes — NOT on listenerNoticePrefixes and
	// never may be; it wears the transport head so the blanket filter swallows it.
	noticeBatchPrefix = "[ocagent] listen: batch "

	// Read by the listener from its environment (cli/ocagent/listen.go's
	// listenAckEnv). Rename it on one side only and the listener silently goes
	// back to "printed means delivered"; only the mirror guard pins it.
	listenAckEnv = "OC_LISTEN_ACK"

	// The blanket filter's head: move it rightward while the producer keeps
	// printing "[ocagent] listen: …" and every retry diagnostic becomes a turn.
	// The mirror guard holds it to being a prefix of every producer notice.
	noticeTransportHead = "[ocagent] listen:"
)

var listenerNoticePrefixes = []string{
	noticeDisconnectedPrefix,
	noticeConnectedPrefix,
	noticeGivingUpPrefix,
}

func actionableCodexListenerLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, noticeTransportHead) {
		return true
	}
	for _, prefix := range listenerNoticePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// Without this wake the boot ends in a dead stop: the codex agent must not
// mount its own listener, it runs only when a listener line becomes a turn,
// and the first connected line opens this wake instead of being forwarded —
// so the post-SSE boot steps would never run. The text names the STEP rather
// than copying the owner's boot document, which moves.
const codexPostBootWake = "[OffiCraft sidecar] 你的事件流（SSE）已經接上了。" +
	"請接著做開機說明裡「接上 SSE 之後」的那些步驟（盤點你手上還沒結束的任務並開始推進）。"

func runCodexSession(argv []string, env func(string) string, out io.Writer) int {
	fs := flag.NewFlagSet("ocwarden codex-session", flag.ContinueOnError)
	fs.SetOutput(out)
	codexBin := fs.String("codex-bin", "", "")
	workdir := fs.String("workdir", "", "")
	persona := fs.String("persona", "", "")
	agentID := fs.String("agent-id", "", "")
	model := fs.String("model", "", "")
	effort := fs.String("effort", "medium", "")
	if fs.Parse(argv) != nil || *codexBin == "" || *workdir == "" || *persona == "" {
		fmt.Fprintln(out, "codex-session: missing required launch parameters")
		return 2
	}
	cmd := exec.Command(*codexBin, "app-server")
	cmd.Dir = *workdir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintf(out, "codex-session: stdin: %v\n", err)
		return 1
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(out, "codex-session: stdout: %v\n", err)
		return 1
	}
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(out, "codex-session: start app-server: %v\n", err)
		return 1
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	// Normalised again on purpose: --effort can come from anywhere, not only
	// buildCodexLaunchCommand.
	sessionEffort, recognisedEffort := normalizeCodexEffort(*effort)
	s := &codexSession{
		in: stdin, messages: codexAppReader(stdout), nextID: 0,
		base: normalizeBase(env("OC_BASE")), token: env("OC_TOKEN"), workdir: *workdir,
		model: *model, effort: sessionEffort, account: codexAccountKey(), out: out,
	}
	if !recognisedEffort {
		s.activity("--effort %q is not a level this warden knows; running at %q",
			*effort, sessionEffort)
	}
	s.activity("App Server started · model %s", func() string {
		if *model == "" {
			return "machine default"
		}
		return *model
	}())
	initializeID := s.send("initialize", map[string]any{
		"clientInfo": map[string]any{
			"name": "officraft", "title": "OffiCraft", "version": "0.1.0",
		},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	if _, err := s.waitResponse(initializeID); err != nil {
		fmt.Fprintf(out, "codex-session: initialize: %v\n", err)
		return 1
	}
	s.notify("initialized", map[string]any{})
	s.reportIdentity()
	rateID := s.send("account/rateLimits/read", nil)
	if response, rateErr := s.waitResponse(rateID); rateErr == nil {
		if snapshot, _ := response["result"].(map[string]any); snapshot != nil {
			s.reportRateLimits(rateLimitSnapshot(snapshot))
		}
	}
	threadParams := map[string]any{
		"cwd": *workdir, "approvalPolicy": "never", "sandbox": "danger-full-access",
		"developerInstructions": codexPersonaInstruction(*persona, *model),
		"config": map[string]any{
			"features": map[string]any{"default_mode_request_user_input": false},
			"mcp_servers": map[string]any{"officraft": map[string]any{
				"url":          strings.TrimRight(s.base, "/") + "/api/mcp",
				"http_headers": map[string]any{"Authorization": "Bearer " + s.token},
			}},
		},
	}
	if *model != "" {
		threadParams["model"] = *model
	}
	threadStartID := s.send("thread/start", threadParams)
	threadResp, err := s.waitResponse(threadStartID)
	if err != nil {
		fmt.Fprintf(out, "codex-session: thread/start: %v\n", err)
		return 1
	}
	s.threadID = nestedString(threadResp, "result", "thread", "id")
	if s.threadID == "" {
		fmt.Fprintln(out, "codex-session: thread/start returned no thread id")
		return 1
	}
	s.activity("thread ready · booting agent")
	s.startTurn("開始。", nil)

	listenerLines := make(chan string, 32)
	listenerStarted := false
	listenerState := &codexListenerState{}
	// Server telemetry is in-memory, so a quiet thread must re-announce its
	// identity after a server restart.
	identityHeartbeat := time.NewTicker(codexTelemetryThrottle)
	defer identityHeartbeat.Stop()
	var listenerCmd *exec.Cmd
	defer func() {
		if listenerCmd != nil && listenerCmd.Process != nil {
			_ = listenerCmd.Process.Kill()
		}
	}()
	for s.messages != nil {
		select {
		case <-identityHeartbeat.C:
			s.reportIdentity()
		case line, ok := <-listenerLines:
			if !ok {
				fmt.Fprintln(out, "codex-session: ocagent listen exited; ending session for reconciliation")
				return 1
			}
			listenerState.handleListenerLine(line,
				func() {
					s.reportIdentity()
					s.requestRateLimits()
					identityHeartbeat.Reset(codexTelemetryThrottle)
				},
				s.openListenerTurn, s.closeBatch)
		case msg, ok := <-s.messages:
			if !ok {
				s.messages = nil
				continue
			}
			if id := messageID(msg); id != 0 && id == s.rateLimitReadID {
				s.rateLimitReadID = 0
				if result, _ := msg["result"].(map[string]any); result != nil {
					s.reportRateLimits(rateLimitSnapshot(result))
				}
				continue
			}
			if _, hasID := msg["id"]; hasID {
				if _, hasMethod := msg["method"]; hasMethod {
					s.handleServerRequest(msg)
					continue
				}
				s.resolveResponse(messageID(msg), msg)
				continue
			}
			method, _ := msg["method"].(string)
			params, _ := msg["params"].(map[string]any)
			switch method {
			case "turn/started":
				s.active = true
				s.turnID = nestedString(params, "turn", "id")
				s.confirmStartedTurn()
			case "turn/completed":
				s.active = false
				s.turnID = ""
				s.activity("turn completed")
				if !listenerStarted {
					listenerStarted = true
					listenerCmd = exec.Command(filepath.Join(*workdir, "ocagent"), "listen")
					listenerCmd.Dir = *workdir
					listenerCmd.Stderr = out
					// The one runtime signal that this listener's stdout is not an
					// agent transcript: every guess the listener could make is wrong
					// in some configuration, and a wrong guess is a drain that waits
					// forever for an ack.
					listenerCmd.Env = codexListenerEnv(os.Environ())
					pipe, pipeErr := listenerCmd.StdoutPipe()
					if pipeErr != nil {
						fmt.Fprintf(out, "codex-session: ocagent listen stdout: %v\n", pipeErr)
						return 1
					}
					ackPipe, ackErr := listenerCmd.StdinPipe()
					if ackErr != nil {
						fmt.Fprintf(out, "codex-session: ocagent listen stdin: %v\n", ackErr)
						return 1
					}
					s.ackTo = ackPipe
					if startErr := listenerCmd.Start(); startErr != nil {
						fmt.Fprintf(out, "codex-session: start ocagent listen: %v\n", startErr)
						return 1
					}
					s.activity("listening for OffiCraft events")
					go func(listener *exec.Cmd) {
						scanner := bufio.NewScanner(pipe)
						scanner.Buffer(make([]byte, 64*1024), 8<<20)
						for scanner.Scan() {
							listenerLines <- scanner.Text()
						}
						_ = listener.Wait()
						close(listenerLines)
					}(listenerCmd)
				}
			case "thread/tokenUsage/updated":
				s.reportTokenUsage(params)
			case "account/rateLimits/updated":
				if snapshot, _ := params["rateLimits"].(map[string]any); snapshot != nil {
					s.reportRateLimits(snapshot)
				}
			case "item/completed":
				s.recordCompaction(params)
			case "item/tool/requestUserInput", "mcpServer/elicitation/request":
				s.handleServerRequest(msg)
			}
		}
	}
	_ = agentID // retained in argv for diagnostics and future thread metadata
	return 1
}
