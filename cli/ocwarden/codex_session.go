package main

// codex_session.go is the Codex counterpart to Claude's direct TUI launch.
// A small ocwarden sidecar owns one stdio Codex App Server, performs the
// initialize/thread/turn handshake, starts ocagent listen after the boot turn,
// and translates listener output into turn/start or turn/steer calls. This
// keeps lifecycle and SSE ownership identical to the existing Claude design
// without requiring a human to attach to a TUI.

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

func buildCodexLaunchCommand(wardenBin, codexBin, workdir, personaFile, tokenFile,
	agentID, base, session, socket, model, effort string, extraEnv [][2]string,
	envRendered string) string {
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
	parts := []string{
		shellQuote(wardenBin), "codex-session",
		"--codex-bin", shellQuote(codexBin),
		"--workdir", shellQuote(workdir),
		"--persona", shellQuote(personaFile),
		"--agent-id", shellQuote(agentID),
		"--model", shellQuote(model),
		"--effort", shellQuote(normalizeCodexEffort(effort)),
	}
	return cd + exports + "exec " + strings.Join(parts, " ")
}

func normalizeCodexEffort(effort string) string {
	switch strings.TrimSpace(effort) {
	case "low", "high":
		return strings.TrimSpace(effort)
	default:
		return "medium"
	}
}

func codexPersonaInstruction(personaFile, model string) string {
	instruction := "Read " + personaFile +
		" completely before acting. It is your OffiCraft identity and operating context. " +
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
	// rateLimitReadID is the one in-flight refresh requested after an OffiCraft
	// SSE reconnect. Responses arrive on the same App Server stream as events.
	rateLimitReadID int
	// completedCompactions makes the App Server's item/completed stream
	// idempotent. Replayed notifications must not look like fresh context
	// compactions and accidentally recycle a just-booted agent.
	completedCompactions map[string]struct{}
	// activitySeq orders the turn-boundary reports (T-a1d7) within this
	// sidecar's session. A plain in-process counter is enough and is stronger
	// than a clock: every report from this process goes through here, so the
	// order is the true emission order, and the server drops anything that
	// arrives out of it. Guarded by activityMu — reports can be emitted from
	// the App Server reader loop.
	activityMu  sync.Mutex
	activitySeq float64
	// activityState / activityTurn de-duplicate at the SOURCE. The App Server
	// emits BOTH turn/started|completed (boundaries) and thread/status/changed
	// (a re-statable status), so the same transition legitimately arrives
	// twice; sending both is what makes the pairing self-healing, but there is
	// no reason to spend an HTTP round-trip restating what we just said.
	activityState string
	activityTurn  string
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

// codexAccountKeyVersion versions the *input semantics* of the hash, not the
// hash algorithm. v1 hashed the workspace id; v2 hashes the person. Bumping it
// keeps the two generations of keys from silently merging into one row.
//
// What actually happens on upgrade (verified against the server fold, not
// assumed): the accounts overview groups by the key an actor is reporting
// RIGHT NOW, and `banked_cost` is a durable per-actor column that the fold adds
// under that current key. So the moment a warden is upgraded,
//
//   - the v1 row does NOT freeze — no actor reports it any more, so the row
//     disappears from /api/monitoring entirely;
//   - each actor's banked history immediately re-attaches to that machine's v2
//     personal key, i.e. money that used to sit in the shared workspace row is
//     re-credited to whoever is logged in on that machine now;
//   - what really is stranded is the owner's hand-set alias: `account_alias`
//     rows are keyed on the v1 string and become orphans, so the owner must
//     re-alias the new key once. Until they do, the cockpit shows a bare
//     `codex:…` digest in that row.
//
// Operational consequence #1 — a mixed fleet shows one person as TWO rows for
// as long as it stays mixed (old wardens still send v1, new ones send v2). It
// converges only when the last warden is upgraded, so upgrade the fleet in one
// pass rather than trickling it.
const codexAccountKeyVersion = "officraft-codex-account-v2:"

// codexAccountKey derives a stable opaque key for the *person* logged into
// Codex on this machine. Both directions matter and v1 only got one of them
// right:
//
//   - the same ChatGPT user on two machines must map to ONE monitoring
//     account (that part v1 did satisfy), and
//   - two different ChatGPT users must map to TWO monitoring accounts.
//
// v1 hashed `tokens.account_id`, which is the ChatGPT *workspace/organization*
// id (verified locally: it is byte-identical to the id_token's
// `chatgpt_account_id` claim). Everyone in one workspace therefore collapsed
// into a single monitoring row: their spend was summed with no way to tell who
// burned it, and the 5h/7d usage windows — which keep "whichever report
// arrived last" on the assumption that one key means one quota — showed people
// with separate quotas fighting over one row.
//
// The identifier chosen instead is the id_token claim
// `https://api.openai.com/auth`.`chatgpt_user_id`: it is ChatGPT's own opaque
// per-person id ("user_..."), identical on every machine that person logs in
// from, and unchanged by token refresh (a refresh mints a new id_token with the
// same claim). Rejected alternatives:
//
//   - `tokens.account_id` / `chatgpt_account_id` — workspace scoped: the bug.
//   - `email` / `name` — human-mutable and PII; a key must survive a rename and
//     must not carry identity in cleartext. They are fine as a *label*, which
//     is a separate field, never as the key input.
//   - `sub` — the IdP subject ("google-oauth2|..."), scoped to the login
//     connection. The same person switching from Google SSO to a password
//     login would look like a new account.
//   - `https://api.openai.com/auth`.`user_id` — observed equal to
//     chatgpt_user_id, so it adds no discrimination; accepting it as a silent
//     alias would only create two ways to spell the same key.
//   - `sid` / `jti` / `at_hash` — per-session or per-token; they change on
//     every refresh.
//   - `chatgpt_plan_type`, `organizations`, `groups` — attributes of the
//     account, not identities of the person.
//
// The workspace id is deliberately NOT mixed in: a person's Codex quota follows
// the person, so folding the workspace into the key would split one human's
// history the day their workspace membership changes. Caveat we did not verify:
// if a single ChatGPT user can hold two independently-metered quotas at once
// (say a personal plan plus a business seat), this key would still merge them.
//
// The key stays an irreversible sha256 with a versioned prefix, and the raw
// claim is never returned, logged, or posted anywhere.
func codexAccountKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return codexAccountKeyForHome(home)
}

// codexAccountKeyForHome is the injectable half of codexAccountKey. Tests must
// never be pointed at a real home directory: ~/.codex/auth.json holds live
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

// codexUserIDFromIDToken reads the per-person claim out of the locally stored
// id_token. The signature is deliberately NOT verified: this file is not a
// trust boundary, it is Codex's own credential store on this machine, and
// anyone who can rewrite it can already act as that account.
//
// Every failure mode — no file, unparsable JSON, a token that is not three
// dot-separated segments, undecodable base64url, payload that is not an
// object, claim absent or blank — returns the empty string, which OffiCraft
// reads as "this machine has no identifiable Codex account". Falling back to
// the workspace id would be worse than reporting nothing: it would silently
// restore the v1 collision on exactly the machines whose id_token we could not
// read, and nothing in the telemetry would show which ones those were.
//
// Operational consequence #2, and the likeliest thing here to bite later:
// fail-empty is SILENT on the wire. `applyAccountReport` treats "account empty
// + runtime present" as a no-op — it neither stores nor clears the pairing — so
// a machine that stops being able to read the claim keeps being served under
// its last successfully reported key until the server restarts, and no
// telemetry field distinguishes "this machine has no Codex account" from "this
// machine could not read one". That is an accepted trade-off, not an oversight:
// the obvious "fix" — falling back to the workspace id — is exactly the defect
// this function exists to remove. If it ever needs solving, solve it
// server-side with an explicit unknown-account signal; do not reintroduce a
// fallback here.
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

// activity is the human-readable, tmux-visible companion to the headless
// App Server protocol. It intentionally describes lifecycle only, never raw
// model prompts, tool arguments, or response bodies.
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

func (s *codexSession) startTurn(text string) {
	s.activity("turn started")
	params := map[string]any{
		"threadId": s.threadID,
		"input":    []any{map[string]any{"type": "text", "text": text}},
		"effort":   s.effort,
	}
	s.send("turn/start", params)
}

func (s *codexSession) steerOrStart(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if s.active && s.turnID != "" {
		s.activity("turn steered by OffiCraft event")
		s.send("turn/steer", map[string]any{
			"threadId": s.threadID, "expectedTurnId": s.turnID,
			"input": []any{map[string]any{"type": "text", "text": text}},
		})
		return
	}
	s.startTurn(text)
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

func (s *codexSession) openReplyCard(question map[string]any, bind string) string {
	header, _ := question["header"].(string)
	body, _ := question["question"].(string)
	secret, _ := question["isSecret"].(bool)
	kind := "decision"
	options := []string{}
	if secret {
		kind = "action"
		options = []string{"已完成（不要在卡片中貼秘密）"}
		body += "\n\n這是秘密資料請求；請只完成所需動作，不要把秘密貼進卡片。"
	} else if raw, ok := question["options"].([]any); ok {
		for _, item := range raw {
			if option, ok := item.(map[string]any); ok {
				if label, ok := option["label"].(string); ok && strings.TrimSpace(label) != "" {
					options = append(options, label)
				}
			}
		}
	}
	if len(options) == 0 {
		options = []string{"請在文字回覆中回答"}
	}
	if len(options) > 4 {
		options = options[:4]
	}
	if strings.TrimSpace(header) == "" {
		header = body
	}
	if strings.TrimSpace(header) == "" {
		header = "Codex 需要你的回覆"
	}
	payload := map[string]any{
		"kind": kind, "summary": header, "body": body,
		"options": options, "bind": bind,
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(s.base, "/")+
		"/api/reply-cards", bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var result map[string]any
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result)
	id, _ := result["id"].(string)
	return id
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
		_ = resp.Body.Close()
	}
}

// reportActivity posts ONE turn boundary to the activity face (T-a1d7).
//
// ⚠️ It deliberately does NOT go through s.post. That helper is completely
// silent — no status check, no log, no retry — so a report that never lands
// leaves no trace at all. Changing s.post's three EXISTING call sites is a
// separate concern and is NOT done here; this is the minimum needed so the NEW
// path's failures are visible in the tmux pane an operator actually reads.
//
// Self-de-duplicating: the same (state, turn) restated is dropped locally.
// turn/started|completed and thread/status/changed both describe the same
// transitions on purpose — the boundary events carry the turn id and the exact
// moment, the status event is the self-healing restatement that recovers a
// missed boundary (it is what fires after an interrupt, where turn/completed
// never comes). Sending both is the point; sending each twice is not.
func (s *codexSession) reportActivity(state, turnID string) {
	s.activityMu.Lock()
	if s.activityState == state && s.activityTurn == turnID {
		s.activityMu.Unlock()
		return
	}
	s.activityState, s.activityTurn = state, turnID
	s.activitySeq++
	seq := s.activitySeq
	s.activityMu.Unlock()

	payload := map[string]any{
		"state":   state,
		"runtime": "codex",
		"seq":     seq,
	}
	if turnID != "" {
		payload["turn_id"] = turnID
	}
	// The App Server thread IS this reporter's session identity: a new thread
	// is a new session, and the server discards any turn claim held by the old
	// one when the id changes.
	if s.threadID != "" {
		payload["session_id"] = s.threadID
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		s.activity("activity report (%s) not sent: %v", state, err)
		return
	}
	req, err := http.NewRequest(http.MethodPost,
		strings.TrimRight(s.base, "/")+"/api/self/activity", bytes.NewReader(raw))
	if err != nil {
		s.activity("activity report (%s) not sent: %v", state, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		s.activity("activity report (%s) failed: %v", state, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.activity("activity report (%s) refused: status %d", state, resp.StatusCode)
	}
}

// reportIdentity is deliberately tiny: it is safe to send at session start,
// after an SSE reconnect, and as the throttled heartbeat without waiting for a
// token event.
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
// `rateLimits`; notifications always use the nested form. Normalize at the
// boundary so reconnect recovery works across both shapes.
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
	// window after a few turns. "last" is the current turn's context gauge.
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
	s.post("/api/monitoring/telemetry", map[string]any{
		"runtime": "codex", "tokens": tokens, "effort": s.effort,
		"account": s.account, "account_label": "ChatGPT",
	})
}

// reportRateLimits maps the App Server's primary/secondary rolling windows to
// OffiCraft's existing five_hour/seven_day monitoring shape. When Codex only
// provides the weekly window, five_hour stays absent rather than fabricated.
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

// recordCompaction consumes the current App Server signal. Context compaction is
// an item, not a turn: counting the completed item avoids guessing from token
// percentages and intentionally ignores the deprecated thread/compacted echo.
func (s *codexSession) recordCompaction(params map[string]any) {
	item, _ := params["item"].(map[string]any)
	if item == nil || item["type"] != "contextCompaction" {
		return
	}
	id, _ := item["id"].(string)
	if id == "" {
		return // completion events are item-addressed; never count an anonymous echo
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
		answers := map[string]any{}
		questions, _ := params["questions"].([]any)
		for index, raw := range questions {
			question, _ := raw.(map[string]any)
			bind := "none"
			if index == 0 {
				bind = ""
			}
			cardID := s.openReplyCard(question, bind)
			qid, _ := question["id"].(string)
			message := "Deferred to OffiCraft; end this turn and wait for the reply-card event."
			if cardID != "" {
				message = "Deferred to OffiCraft reply card " + cardID +
					"; end this turn and wait for its SSE answer event."
			} else {
				message = "OffiCraft reply-card creation failed; do not wait for terminal input. " +
					"Continue if safe or report the failure through OffiCraft chat."
			}
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

func actionableCodexListenerLine(line string) bool {
	// Transport diagnostics belong in the pane, not in the model transcript.
	// Sending the connected/reconnect chatter creates empty, token-heavy turns.
	return !strings.HasPrefix(strings.TrimSpace(line), "[ocagent] listen:")
}

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
	s := &codexSession{
		in: stdin, messages: codexAppReader(stdout), nextID: 0,
		base: env("OC_BASE"), token: env("OC_TOKEN"), workdir: *workdir,
		model: *model, effort: normalizeCodexEffort(*effort), account: codexAccountKey(), out: out,
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
	// Identity is independent of token/rate-limit events. Report it immediately
	// so a quiet Codex thread still shows its ChatGPT account in OffiCraft.
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
	threadID := s.send("thread/start", threadParams)
	threadResp, err := s.waitResponse(threadID)
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
	s.startTurn("開始。")

	listenerLines := make(chan string, 32)
	listenerStarted := false
	// Telemetry is intentionally in-memory on the server.  A quiet App Server
	// thread must therefore re-announce its lightweight identity after a server
	// restart; use the same 30-second cadence as token telemetry, not a noisy
	// per-event loop.
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
			// ocagent emits this exact lifecycle line each time its SSE stream
			// opens. A server restart therefore restores account telemetry
			// immediately, then restarts the normal 30-second cadence.
			if strings.HasPrefix(strings.TrimSpace(line), "[ocagent] listen: connected") {
				s.reportIdentity()
				s.requestRateLimits()
				identityHeartbeat.Reset(codexTelemetryThrottle)
			}
			if actionableCodexListenerLine(line) {
				s.activity("OffiCraft event: %s", line)
				s.steerOrStart(line)
			}
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
				}
				continue
			}
			method, _ := msg["method"].(string)
			params, _ := msg["params"].(map[string]any)
			switch method {
			case "turn/started":
				s.active = true
				s.turnID = nestedString(params, "turn", "id")
				// T-a1d7: the local s.active flag has always existed but never
				// left this process, so the cockpit could not tell a session
				// mid-turn from an idle one. Now it does.
				s.reportActivity("active", s.turnID)
			case "turn/completed":
				completedTurn := nestedString(params, "turn", "id")
				if completedTurn == "" {
					completedTurn = s.turnID
				}
				s.active = false
				s.turnID = ""
				s.reportActivity("idle", completedTurn)
				s.activity("turn completed")
				if !listenerStarted {
					listenerStarted = true
					listenerCmd = exec.Command(filepath.Join(*workdir, "ocagent"), "listen")
					listenerCmd.Dir = *workdir
					listenerCmd.Stderr = out
					pipe, pipeErr := listenerCmd.StdoutPipe()
					if pipeErr != nil {
						fmt.Fprintf(out, "codex-session: ocagent listen stdout: %v\n", pipeErr)
						return 1
					}
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
			case "thread/status/changed":
				// T-a1d7. A STATUS notification, not a boundary one: it is
				// re-statable and idempotent, which makes it the self-healing
				// half of the pair. It is the ONLY signal after a user
				// interrupt — turn/completed never arrives there, so without
				// this case an interrupted turn would stay claimed until the
				// server's max-turn window expired.
				//
				// It carries no turn id, so it reuses the one the boundary
				// event established: an "active" restated for the SAME turn is
				// then a local no-op rather than a spurious re-anchoring of
				// when the turn started.
				switch nestedString(params, "status", "type") {
				case "idle":
					s.reportActivity("idle", s.turnID)
				case "active":
					s.reportActivity("active", s.turnID)
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
