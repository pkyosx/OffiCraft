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

// reportRejectedCodexPost makes a refused best-effort report visible to the
// sidecar operator without changing its deliberately non-blocking flow.
func (s *codexSession) reportRejectedCodexPost(path string, status int) {
	if status >= http.StatusBadRequest {
		s.activity("Codex POST %s rejected with HTTP %d", path, status)
	}
}

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
	case "low", "high", "max":
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
	body := map[string]any{
		"runtime": "codex", "tokens": tokens, "effort": s.effort,
		"account": s.account, "account_label": "ChatGPT",
	}
	// The codex twin of the claude reporter's model telemetry. This sidecar is
	// the only thing on the codex path that knows which model the session is
	// actually running, so without this the cockpit's 模型 column has no reported
	// value for ANY codex session and has to fall back to the launch setting.
	//
	// Blank is OMITTED, not sent as "": a blank s.model means the OffiCraft launch
	// model was unset and the machine's own Codex default is in force (see
	// codexPersonaInstruction), i.e. we genuinely do not know the name. Sending ""
	// would record that unknown as a reported blank, which is the exact
	// "measured" vs "never measured" collapse this field exists to end.
	if m := strings.TrimSpace(s.model); m != "" {
		body["model"] = m
	}
	s.post("/api/monitoring/telemetry", body)
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

// codexOpenYourOwnCardMessage is what Codex gets back instead of a card the
// warden minted for it: an instruction to open the card ITSELF, through the
// tool, where it can name the task and step the question is actually about.
//
// 🔴 THE SECRET WARNING RIDES HERE, AND IT HAS TO. While the warden opened the
// card it inspected question["isSecret"] and, for a credential ask, put "do not
// paste the secret into the card" into the card body itself. Nothing executes
// that path any more. If the sentence did not move into THIS text, Codex would
// go and open its own card for a password or an API key with nothing anywhere
// telling it not to type the secret into the body — the guard would be gone and
// its absence would be silent, which is the failure mode this whole ticket is
// about.
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
		// T-18: the warden no longer opens the card ON CODEX'S BEHALF. It could
		// not do the job honestly — it holds no task_id and no step_id, so every
		// card it minted here went out asking the server to GUESS the binding,
		// and a guess that missed produced a card with no 等我回覆 hold that the
		// owner's answer would later be refused for. create_reply_card now
		// requires an explicit linked_task, and the only party that knows what
		// work this question is about is Codex itself. So this arm REFUSES and
		// says so, the same shape mcpServer/elicitation/request has always used.
		//
		// The structure is unchanged: this always answered Codex with a line of
		// text rather than parking it — a sidecar that made the model wait on a
		// terminal round-trip loses the whole turn the moment the connection
		// drops. Only the sentence is different.
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

// codexListenerActions decides what ONE listener line does to the session:
// whether it wakes a session whose boot is still unfinished, and whether it is
// forwarded to the model as a turn. It is a pure function so the decision can be
// tested without an App Server — the loop below owns only the side effects.
// codexListenerState carries the once-only wake flag across listener lines.
type codexListenerState struct{ wakeSent bool }

// handleListenerLine runs the side effects ONE listener line is owed, with the
// effects injected so the branching can be driven without an App Server.
//
// 🔴 THE DECISION TABLE IS NOT THE BEHAVIOUR. An earlier version of this
// package pinned only codexListenerActions, and independent review deleted both
// the wake call and the flag write from the loop with the whole ocwarden suite
// still green — the ticket's entire reason for existing could be removed and
// nothing turned red. A pure function says what SHOULD happen; this seam is
// what lets a test see that it DID.
//
// ⚠️ ONE RESIDUE, AND WHAT HOLDS IT IS AN ACCIDENT. Deleting the CALL to this
// method from the listener loop still leaves the whole ocwarden suite green;
// what reddens is `uplink-guard`, because uplinks.json's codex-hop-4 anchors on
// the reportIdentity/requestRateLimits pair that happens to live in the closure
// passed here. That is INCIDENTAL COVERAGE, not design — move those two calls
// out of the closure and the residue reopens with nothing to announce it.
// Recorded rather than closed: closing it properly needs a test that drives the
// real loop, and the review (T-99a6) judged the residue non-blocking.
func (st *codexListenerState) handleListenerLine(
	line string, onConnect func(), openTurn func(string),
) {
	wake, forward := codexListenerActions(line, st.wakeSent)
	if strings.HasPrefix(strings.TrimSpace(line), noticeConnectedPrefix) {
		onConnect()
	}
	// ONCE per session, and deliberately not on reconnects: this wake exists to
	// continue a boot that has not finished, and by the second connect that boot
	// is long over. A reconnect is a network blip — every one of them opening a
	// fresh "go do your inventory" turn would spend tokens re-doing work and
	// would interrupt whatever the agent is actually in the middle of.
	//
	// ⚠️ THE RECONNECT IS STILL REPORTED, just not as THIS turn. Since the
	// disconnect-notice policy (owner, 2026-08-30) the connected line is itself a
	// forwardable notice, so the second connect reaches the agent as one short
	// line rather than as a second boot instruction. The two must never both fire
	// for one line — see codexListenerActions.
	if wake {
		st.wakeSent = true
		openTurn(codexPostBootWake)
	}
	if forward {
		openTurn(line)
	}
}

// openListenerTurn is the LAST STEP OF THE DELIVERY: the one place where a line
// the decision table said to forward actually becomes a turn on the model.
//
// 🔴 IT IS A NAMED METHOD BECAUSE AN ANONYMOUS CLOSURE INSIDE THE LOOP WAS
// UNREACHABLE FROM ANY TEST. Independent review replaced this body's
// `s.steerOrStart(text)` with `_ = text` inside runCodexSession's select loop:
// the whole ocwarden suite went green and so did uplink-guard, while EVERY
// forwarded notice AND every chat/task event silently stopped reaching the
// model. The decision table was fully pinned; the delivery was not pinned by
// anything at all. Pulling it out here is what gives a test something to call —
// see codex_notice_test.go, which drives this against a real codexSession and
// reads the App Server bytes it writes.
func (s *codexSession) openListenerTurn(text string) {
	if text == codexPostBootWake {
		s.activity("waking the session now that SSE is up")
	} else {
		s.activity("OffiCraft event: %s", text)
	}
	s.steerOrStart(text)
}

func codexListenerActions(line string, wakeAlreadySent bool) (wake, forward bool) {
	connected := strings.HasPrefix(strings.TrimSpace(line), noticeConnectedPrefix)
	wake = connected && !wakeAlreadySent
	// A line never does BOTH. The boot connect opens the post-boot wake and is
	// not also forwarded; every later connect is forwarded as the reconnect
	// notice and wakes nothing.
	return wake, actionableCodexListenerLine(line) && !wake
}

// listenerNoticePrefixes are the transport lines the owner's disconnect-notice
// policy (2026-08-30) says MUST reach the agent:
//
//	「應該是在第一次斷線，跟連線回來的時候發訊息給 agent，中間的 retry 我們不需要
//	 降低頻率，但是不需要打攪 agent。」
//
// 🔴 A CODEX MEMBER USED TO BE TOLD ABOUT ITS TRANSPORT EXACTLY ONCE, AT BOOT.
// The blanket "[ocagent] listen:" filter below is older than the ruling and was
// stricter than it: it dropped every disconnect and every reconnect for the
// whole life of the session, so a codex agent could sit through a station
// changeover with nothing in its transcript to say its stream had been down.
// The claude runtime sat at the opposite extreme — it printed EVERY retry
// straight into the transcript — and neither end was what the owner asked for.
//
// The give-up line is on this list for the reason the owner approved alongside
// the ruling: with only the two endpoints, `斷線 → 沉默` cannot distinguish
// 「還在重試」 from 「已經放棄」, and an agent cannot tell whether waiting is a
// plan. ocagent prints it at every exit of its retry loop; dropping it here
// would put the ambiguity straight back.
//
// These are LONG prefixes of the same "[ocagent] listen:" head, so they are
// exceptions carved out of the filter and not a second parser: the head itself
// still does not move (cli/ocagent/listen_run.go's prefix note).
//
// 🔴 THEY ARE CONSTANTS, AND THE OTHER HALF OF THE CONTRACT IS TESTED. These
// bytes are printed by a DIFFERENT Go module (cli/ocagent/listen_run.go's
// notice* constants) that this one cannot import, so the contract is physically
// two copies. Independent review moved one head rightward on the producing side
// — `"listen: disconnected — "` → `"net listen: disconnected — "` — and both
// suites stayed green while every codex member lost its transport notices for
// the rest of its session. cli/ocagent/listen_notice_contract_test.go now reads
// THIS file and requires these literals to still be here.
const (
	noticeDisconnectedPrefix = "[ocagent] listen: disconnected"
	noticeConnectedPrefix    = "[ocagent] listen: connected"
	noticeGivingUpPrefix     = "[ocagent] listen: giving up"

	// 🔴 THE FOURTH COPY, AND FOR A LONG TIME THE ONLY UNPINNED ONE. This is the
	// head of the blanket filter below — the bytes that decide whether a line is
	// transport chatter at all — and until T-4 it was a bare literal inside
	// actionableCodexListenerLine: not a constant, not in the contract test's
	// list, held up only INDIRECTLY by one behavioural case
	// ("stream ended: EOF" ⇒ false). Behaviour coverage is not contract
	// coverage: move this head rightward while the producer keeps printing
	// "[ocagent] listen: …" and the filter stops recognising ANY transport line,
	// so every retry diagnostic starts becoming a turn on the model — the exact
	// noise the owner's ruling exists to swallow. It is spelled once here and
	// cli/ocagent/listen_notice_contract_test.go now requires this declaration,
	// with this value, to still exist on this side of the module gap.
	noticeTransportHead = "[ocagent] listen:"
)

var listenerNoticePrefixes = []string{
	noticeDisconnectedPrefix, // the first failure of an outage
	noticeConnectedPrefix,    // back up (and whether the station changed)
	noticeGivingUpPrefix,     // the retry loop really stopped
}

func actionableCodexListenerLine(line string) bool {
	// Transport diagnostics belong in the pane, not in the model transcript.
	// Sending the whole retry chatter creates empty, token-heavy turns — which
	// is exactly the mid-outage traffic the ruling above says to swallow.
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

// codexPostBootWake is the turn this sidecar opens ONCE, the first time the
// listener's stream is up (T-51b0).
//
// 🔴 WITHOUT IT, THE BOOT SEQUENCE ENDS IN A DEAD STOP. The order the owner
// asked for is wake → resume → SSE → continue work, and only a sidecar can
// deliver the third arrow: this runtime's agent must NOT mount its own
// listener, so it ends its boot turn and hands control back here. But a codex
// agent only ever runs when a listener line is turned into a turn, and the
// connected line above is deliberately filtered out — so the agent that just
// handed control back would sit there until some unrelated event happened to
// arrive. Its boot document's post-SSE steps (the task inventory) would never
// run, and nothing anywhere would report it: an agent that never starts looks
// exactly like an agent with nothing to do.
//
// The text names the STEP rather than restating it. The boot document is the
// owner's and it moves; a copy of its wording here would be a second source of
// truth that goes stale silently — the failure this whole ticket is made of.
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
	s := &codexSession{
		in: stdin, messages: codexAppReader(stdout), nextID: 0,
		base: normalizeBase(env("OC_BASE")), token: env("OC_TOKEN"), workdir: *workdir,
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
	listenerState := &codexListenerState{}
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
			listenerState.handleListenerLine(line,
				func() {
					s.reportIdentity()
					s.requestRateLimits()
					identityHeartbeat.Reset(codexTelemetryThrottle)
				},
				s.openListenerTurn)
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
			case "turn/completed":
				s.active = false
				s.turnID = ""
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
