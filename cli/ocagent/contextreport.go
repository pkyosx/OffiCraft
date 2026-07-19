package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// context-report: ocagent context-report  (stdin statusLine JSON, no flags)
// A faithful port of agent/oc_agent.py cmd_context_report (+ its helpers
// _statusline_pct / _report_stamp_path / _report_throttled / read_claude_user_id
// / _build_telemetry) and reconcile.local_host. Exception: the account key
// (readClaudeAccount) EXTENDS the Python read_claude_user_id with an
// organizationUuid dimension (T-713a, plan dimension later removed by T-f694)
// — see its doc.
// ---------------------------------------------------------------------------
//
// The Claude Code statusLine reporter (B2). The model can't read its own
// context-% at runtime, but Claude Code passes context_window.used_percentage to
// the statusLine command on stdin. This subcommand bridges that value (throttled,
// best-effort) onto the server gauge via POST /api/agent/context, and — pct-
// INDEPENDENTLY — pushes account-wide rate_limits / cumulative cost / this
// session's own token burn onto the monitoring surface via POST
// /api/monitoring/telemetry.
//
// Fail-safe throughout: no OC_TOKEN/OC_ID, a null pct, or a blocked POST all
// degrade to just printing the one-line status line (never crash, never
// fabricate). ALWAYS prints the status line + exits 0 (dual-use), so a mis-wire
// never breaks the TUI status line.

// reportThrottleSecs mirrors _REPORT_THROTTLE_SECS: at most one report burst per
// 30s window (both POSTs ride the SAME window).
const reportThrottleSecs = 30.0

// contextBody is the context POST wire body: {agent_id, context_pct}.
type contextBody struct {
	AgentID    string  `json:"agent_id"`
	ContextPct float64 `json:"context_pct"`
}

// rlWindow is one rate-limit window ({used_percentage, resets_at}) with values
// passed through RAW from the statusLine payload (a JSON number or null — resets_at
// is a unix-epoch number the server parses, never converted to ISO).
type rlWindow struct {
	UsedPercentage any `json:"used_percentage"`
	ResetsAt       any `json:"resets_at"`
}

// rateLimits carries the two account-wide windows. Each is a pointer so a window
// missing from the payload is OMITTED (matches Python only inserting present dicts),
// and the field order (five_hour, seven_day) matches Python's insertion order.
type rateLimits struct {
	FiveHour *rlWindow `json:"five_hour,omitempty"`
	SevenDay *rlWindow `json:"seven_day,omitempty"`
}

// tokensBody is this session's own token burn TODAY (order matches Python).
type tokensBody struct {
	Burned    int `json:"burned"`
	Output    int `json:"output"`
	CacheRead int `json:"cache_read"`
}

// telemetryBody is the monitoring telemetry POST wire body. Field order mirrors
// Python's {"agent_id", **telemetry, "account", "machine"} (agent_id, rate_limits,
// cost, tokens, account, machine). rate_limits/cost/tokens are pointers with
// omitempty so an absent source is dropped — and, crucially, cost is *float64 so a
// REAL 0.0 (a brand-new session) is KEPT while a missing cost is omitted (a plain
// float64 with omitempty would wrongly drop 0.0). machine is always present in
// practice (localHost defaults to the server-self id) but stays omitempty for the
// empty guard.
type telemetryBody struct {
	AgentID    string      `json:"agent_id"`
	RateLimits *rateLimits `json:"rate_limits,omitempty"`
	Cost       *float64    `json:"cost,omitempty"`
	Tokens     *tokensBody `json:"tokens,omitempty"`
	Account    string      `json:"account"`
	// AccountLabel is the human-readable owner-facing label for the account key
	// ("<emailAddress>(<organizationName>)" from oauthAccount, T-260e). Display
	// only — never part of the account KEY — and omitted when unreadable (the
	// server must see absent, not "").
	AccountLabel string `json:"account_label,omitempty"`
	Machine      string `json:"machine,omitempty"`
}

// cmdContextReport implements `ocagent context-report`. `now` is the current unix
// time in fractional seconds (mirrors time.time()) — injected so the throttle is
// testable. ALWAYS returns 0 (best-effort + dual-use status line), identical to
// Python cmd_context_report.
func cmdContextReport(client httpClient, cfg Config, env func(string) string, now float64, stdin io.Reader, out io.Writer) int {
	payload := ""
	if raw, err := io.ReadAll(stdin); err == nil { // a read fault degrades to ""
		payload = string(raw)
	}
	pct, havePct := statuslinePct(payload)

	if cfg.Token != "" && cfg.ID != "" {
		stamp := reportStampPath(cfg)
		if !reportThrottled(stamp, now, reportThrottleSecs) {
			// Context POST needs a real pct: when pct is absent (fresh/compacted
			// session — used_percentage is null) we honestly SKIP it rather than
			// fabricate a fake 0. pct gates ONLY its own POST, never the telemetry.
			if havePct {
				postJSON(client, cfg, "/api/agent/context",
					contextBody{AgentID: cfg.ID, ContextPct: pct})
			}
			// ADDITIVE telemetry POST, pct-INDEPENDENT. Skipped entirely when there's
			// no real telemetry (an empty body would 400 at the server).
			if rl, cost, tokens, hasTel := buildTelemetry(payload); hasTel {
				body := telemetryBody{
					AgentID:    cfg.ID,
					RateLimits: rl,
					Cost:       cost,
					Tokens:     tokens,
				}
				account := readClaudeAccount(env)
				if account == "" {
					account = "unknown" // honest sentinel
				}
				body.Account = account
				body.AccountLabel = readClaudeAccountLabel(env)
				if machine := localHost(env); machine != "" {
					body.Machine = machine
				}
				postJSON(client, cfg, "/api/monitoring/telemetry", body)
			}
			// Stamp the throttle window (NOT bound to any POST's status): once we've
			// done our best-effort POSTs, advance the window so the next tick is
			// throttled (keeps pct=None ticks from re-POSTing telemetry every tick).
			writeStamp(stamp, now)
		}
	}

	fmt.Fprintln(out, renderStatusline(payload, env, now))
	return 0
}

// ---------------------------------------------------------------------------
// statusline rendering (B2 display upgrade, T-51a8)
//
// The one-line status line the owner sees in the cockpit. Upgraded from the old
// "🧠 N% context" to the owner's target layout:
//
//   ◆ <model> ⚡<effort> | <bar> N% | $X.XX | XmXXs | 5h:N%(rst:XhYm) 7d:N%(N%elapsed)
//
// Every segment is INDEPENDENTLY fail-safe: a missing / null / unparseable
// source drops just that segment (never a fabricated 0, never an empty shell,
// never a panic). This is display-only — the throttled telemetry / context POSTs
// above are untouched, and the same stdin payload feeds both. ANSI colours are
// honoured by Claude Code's statusLine surface (model blue, effort yellow,
// context% green, cost yellow, everything else grey).
// ---------------------------------------------------------------------------

const (
	ansiReset  = "\x1b[0m"
	ansiBlue   = "\x1b[34m"
	ansiYellow = "\x1b[33m"
	ansiGreen  = "\x1b[32m"
	ansiGray   = "\x1b[90m"

	statusBarWidth = 10 // context progress-bar cell count
)

// renderStatusline builds the full status line from a statusLine JSON payload,
// the process env (for OC_EFFORT), and now (fractional unix seconds, for the
// rate-limit reset/elapsed maths — injected so tests are deterministic). Returns
// the ready-to-print line WITHOUT a trailing newline. Never panics: a nil / junk
// payload yields an empty line.
func renderStatusline(payload string, env func(string) string, now float64) string {
	obj, _ := safeJSON(payload).(map[string]any) // nil ⇒ every segment skips

	var segs []string
	if s := modelEffortSegment(obj, env); s != "" {
		segs = append(segs, s)
	}
	if s := contextBarSegment(payload); s != "" {
		segs = append(segs, s)
	}
	if s := costSegment(obj); s != "" {
		segs = append(segs, ansiYellow+s+ansiReset)
	}
	if s := durationSegment(obj); s != "" {
		segs = append(segs, ansiGray+s+ansiReset)
	}
	if s := rateLimitSegment(obj, now); s != "" {
		segs = append(segs, ansiGray+s+ansiReset)
	}
	return strings.Join(segs, ansiGray+" | "+ansiReset)
}

// modelEffortSegment renders "◆ <display_name>[ (1M context)] ⚡<effort>". The
// model (blue) is present iff model.display_name is a non-empty string; the "1M
// context" hint is appended only when model.id signals the 1M tier ("[1m]") and
// display_name doesn't already say so. The effort (yellow) is present iff
// OC_EFFORT is set; a bare effort with no model still renders "⚡<effort>". Both
// missing ⇒ "".
func modelEffortSegment(obj map[string]any, env func(string) string) string {
	model := ""
	if m, ok := obj["model"].(map[string]any); ok {
		if name, ok := m["display_name"].(string); ok {
			model = strings.TrimSpace(name)
		}
		if model != "" {
			id, _ := m["id"].(string)
			lid, lname := strings.ToLower(id), strings.ToLower(model)
			if strings.Contains(lid, "[1m]") && !strings.Contains(lname, "1m") {
				model += " (1M context)"
			}
		}
	}

	out := ""
	if model != "" {
		out = ansiBlue + "◆ " + model + ansiReset
	}
	if effort := effortLabel(env); effort != "" {
		e := ansiYellow + "⚡" + effort + ansiReset
		if out != "" {
			out += " " + e
		} else {
			out = e
		}
	}
	return out
}

// effortLabel reads OC_EFFORT (the owner's launch intent, plumbed by ocwarden
// spawn as an extra env pair), trimmed. "medium" abbreviates to "med" to match
// the owner's target layout; other values pass through verbatim. Empty ⇒ "".
func effortLabel(env func(string) string) string {
	e := strings.TrimSpace(env("OC_EFFORT"))
	if e == "medium" {
		return "med"
	}
	return e
}

// contextBarSegment renders "<bar> N%" — a unicode progress bar (grey) plus the
// rounded percentage (green) — from context_window.used_percentage. Reuses
// statuslinePct so the clamp / bool-exclusion / null-skip semantics match the
// POST path exactly. Absent pct ⇒ "".
func contextBarSegment(payload string) string {
	pct, ok := statuslinePct(payload)
	if !ok {
		return ""
	}
	filled := int(math.Round(pct / 100 * statusBarWidth)) // half-up: a nonzero pct shows ≥1 cell
	if filled < 0 {
		filled = 0
	}
	if filled > statusBarWidth {
		filled = statusBarWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", statusBarWidth-filled)
	shown := int(math.RoundToEven(pct)) // Python int(round()): banker's
	return ansiGray + bar + ansiReset + " " + ansiGreen + strconv.Itoa(shown) + "%" + ansiReset
}

// costSegment renders "$X.XX" from cost.total_cost_usd (a real 0.0 is kept —
// only a missing / non-numeric value is dropped). Uncoloured here; the caller
// paints it yellow.
func costSegment(obj map[string]any) string {
	cm, ok := obj["cost"].(map[string]any)
	if !ok {
		return ""
	}
	tc, ok := cm["total_cost_usd"].(float64) // a JSON bool ⇒ bool, excluded
	if !ok {
		return ""
	}
	return fmt.Sprintf("$%.2f", tc)
}

// durationSegment renders this session's wall time from cost.total_duration_ms:
// "XmYYs" under an hour, "XhYYm" at/over an hour. Absent / non-numeric ⇒ "".
func durationSegment(obj map[string]any) string {
	cm, ok := obj["cost"].(map[string]any)
	if !ok {
		return ""
	}
	ms, ok := cm["total_duration_ms"].(float64)
	if !ok {
		return ""
	}
	total := int(ms / 1000)
	if total < 0 {
		total = 0
	}
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm%02ds", m, s)
}

// rateLimitSegment renders "5h:N%(rst:XhYm) 7d:N%(N%elapsed)" from
// rate_limits.{five_hour,seven_day}. The two windows are space-joined into ONE
// segment (matching the owner's layout). Each window needs BOTH used_percentage
// AND resets_at present (task rule: any null ⇒ skip that window). 5h shows the
// reset countdown; 7d shows how much of the 7-day window has elapsed.
func rateLimitSegment(obj map[string]any, now float64) string {
	rlm, ok := obj["rate_limits"].(map[string]any)
	if !ok {
		return ""
	}
	var parts []string
	if w, ok := rlm["five_hour"].(map[string]any); ok {
		if up, ra, ok := rlWindowFields(w); ok {
			label := fmt.Sprintf("5h:%d%%", int(math.RoundToEven(up)))
			if rem := ra - now; rem > 0 {
				label += "(rst:" + compactDuration(rem) + ")"
			}
			parts = append(parts, label)
		}
	}
	if w, ok := rlm["seven_day"].(map[string]any); ok {
		if up, ra, ok := rlWindowFields(w); ok {
			const window = 7 * 24 * 3600.0
			elapsed := (now - (ra - window)) / window * 100
			if elapsed < 0 {
				elapsed = 0
			}
			if elapsed > 100 {
				elapsed = 100
			}
			parts = append(parts, fmt.Sprintf("7d:%d%%(%d%%elapsed)",
				int(math.RoundToEven(up)), int(math.RoundToEven(elapsed))))
		}
	}
	return strings.Join(parts, " ")
}

// rlWindowFields pulls used_percentage + resets_at from one window, requiring
// BOTH to be JSON numbers (a null / missing / non-numeric either side ⇒ skip the
// whole window).
func rlWindowFields(w map[string]any) (used, resetsAt float64, ok bool) {
	up, upOK := w["used_percentage"].(float64)
	ra, raOK := w["resets_at"].(float64)
	if !upOK || !raOK {
		return 0, 0, false
	}
	return up, ra, true
}

// compactDuration formats a positive second-count as "XhYm" (hours present) or
// "Ym" (under an hour) — the owner's compact reset-countdown style (e.g. 3h7m,
// 45m). Minutes are NOT zero-padded here (distinct from durationSegment's XmYYs).
func compactDuration(seconds float64) string {
	total := int(seconds)
	h, m := total/3600, (total%3600)/60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// statuslinePct extracts context_window.used_percentage (clamped 0–100) from a
// statusLine JSON payload → (pct, true), or (0, false) when null / missing /
// unparseable / a bool (never a fabricated 0). Mirrors _statusline_pct.
func statuslinePct(payload string) (float64, bool) {
	obj, ok := safeJSON(payload).(map[string]any)
	if !ok {
		return 0, false
	}
	cw, ok := obj["context_window"].(map[string]any)
	if !ok {
		return 0, false
	}
	// JSON numbers decode to float64; a JSON bool decodes to bool (so the
	// isinstance(pct, bool) exclusion is automatic — only float64 is accepted).
	pct, ok := cw["used_percentage"].(float64)
	if !ok {
		return 0, false
	}
	return math.Max(0.0, math.Min(100.0, pct)), true
}

// reportStampPath is the throttle marker for this agent: <home>/<id-or-anon>/
// context_report.stamp (id lowercased). Mirrors _report_stamp_path.
func reportStampPath(cfg Config) string {
	key := strings.ToLower(cfg.ID)
	if key == "" {
		key = "anon"
	}
	return filepath.Join(cfg.Home, key, "context_report.stamp")
}

// reportThrottled is true iff a report was sent within `window` seconds (skip this
// one). A missing / empty / unreadable / unparseable stamp reads as NOT throttled
// (send). Never raises. Mirrors _report_throttled.
func reportThrottled(stampPath string, now, window float64) bool {
	raw, err := os.ReadFile(stampPath)
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		s = "0" // mirrors `float(... or "0")`
	}
	last, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false // unparseable ⇒ suppressed exception ⇒ NOT throttled
	}
	return (now - last) < window
}

// writeStamp records the throttle window best-effort (mkdir -p + write str(now)).
// A write fault is swallowed (mirrors the Python contextlib.suppress).
func writeStamp(stampPath string, now float64) {
	if err := os.MkdirAll(filepath.Dir(stampPath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(stampPath, []byte(strconv.FormatFloat(now, 'f', -1, 64)), 0o644)
}

// localHost is this host's reconcile identity — OC_HOST or the server-self default
// "m-server-self" (the machine id of the box running the server; a remote warden
// sets OC_HOST to its own machine id). Mirrors reconcile.local_host; the default
// MUST equal dal.seed.SEED_SERVER_SELF_ID / domain.member.SERVER_SELF_HOST.
func localHost(env func(string) string) string {
	if h := env("OC_HOST"); h != "" {
		return h
	}
	return "m-server-self"
}

// readClaudeAccount returns the monitoring attribution key for the logged-in
// Claude account: the stable OAuth userID joined with oauthAccount's
// organizationUuid as "<userID>/<organizationUuid>" — bare userID when no org
// is present (never a dangling "<userID>/"), "" when no userID is found. Both
// dimensions come from .claude.json only, which the claude CLI writes
// regardless of where credentials live (file or macOS Keychain), so the key is
// stable across machines and credential storage forms. The plan's
// subscriptionType deliberately does NOT join the key (T-f694): keying on it
// made the same account split into two monitoring rows depending on whether
// ~/.claude/.credentials.json was readable on a given machine (Keychain-only
// installs fell back to the org dimension). Same userID under different orgs
// still yields distinct keys. userID scans BOTH candidates
// (~/.claude/.claude.json then ~/.claude.json) and each dimension resolves
// INDEPENDENTLY — real installs split them across the two files.
func readClaudeAccount(env func(string) string) string {
	home := env("HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	userID, org := "", ""
	for _, path := range []string{
		filepath.Join(home, ".claude", ".claude.json"),
		filepath.Join(home, ".claude.json"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var d map[string]any
		if json.Unmarshal(raw, &d) != nil {
			continue
		}
		if userID == "" {
			if uid, ok := d["userID"]; ok && uid != nil {
				userID = strings.TrimSpace(pyStr(uid))
			}
		}
		if org == "" {
			org = claudeOrgUUID(d)
		}
	}
	if userID == "" {
		return ""
	}
	if org != "" {
		return userID + "/" + org
	}
	return userID
}

// readClaudeAccountLabel returns the human-readable OWNER-FACING label for the
// logged-in Claude account — "<emailAddress>(<organizationName>)" from
// .claude.json's oauthAccount (T-260e). This is DISPLAY ONLY: the stable
// account key stays readClaudeAccount's userID/org dimensions; the label never
// joins the key. Same two-file discipline as readClaudeAccount (the T-713a
// lesson): BOTH candidates (~/.claude/.claude.json then ~/.claude.json) are
// scanned and each field resolves INDEPENDENTLY, because real installs split
// fields across the two files. Missing-field degradation: no emailAddress ⇒
// displayName carries the label; no organizationName ⇒ no "()" suffix; nothing
// readable ⇒ "" (the caller then OMITS account_label from the wire body —
// absent, never a fabricated ""). Only string-typed, non-blank fields count
// (never "null"/stringified junk in an owner-facing label).
func readClaudeAccountLabel(env func(string) string) string {
	home := env("HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	email, displayName, orgName := "", "", ""
	for _, path := range []string{
		filepath.Join(home, ".claude", ".claude.json"),
		filepath.Join(home, ".claude.json"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var d map[string]any
		if json.Unmarshal(raw, &d) != nil {
			continue
		}
		oauth, ok := d["oauthAccount"].(map[string]any)
		if !ok {
			continue
		}
		strField := func(key string) string {
			s, _ := oauth[key].(string)
			return strings.TrimSpace(s)
		}
		if email == "" {
			email = strField("emailAddress")
		}
		if displayName == "" {
			displayName = strField("displayName")
		}
		if orgName == "" {
			orgName = strField("organizationName")
		}
	}
	base := email
	if base == "" {
		base = displayName
	}
	if base == "" {
		return ""
	}
	if orgName != "" {
		return base + "(" + orgName + ")"
	}
	return base
}

// claudeOrgUUID pulls oauthAccount.organizationUuid — the org dimension of the
// account key — out of a decoded .claude.json. Strict: a missing oauthAccount
// object, a missing / null / non-string / blank organizationUuid all yield "",
// so readClaudeAccount degrades to a bare-userID key rather than appending an
// empty (or "None") suffix.
func claudeOrgUUID(d map[string]any) string {
	oauth, ok := d["oauthAccount"].(map[string]any)
	if !ok {
		return ""
	}
	org, _ := oauth["organizationUuid"].(string)
	return strings.TrimSpace(org)
}

// buildTelemetry parses a statusLine payload into the telemetry pieces
// (rate_limits, cost, tokens) and reports whether ANY is present. Every field is
// OMITTED when its source is missing (the panel shows 未量到, never a fabricated 0);
// a real 0.0 cost IS kept. Mirrors _build_telemetry.
func buildTelemetry(raw string) (rl *rateLimits, cost *float64, tokens *tokensBody, has bool) {
	obj, ok := safeJSON(raw).(map[string]any)
	if !ok {
		return nil, nil, nil, false
	}

	// rate_limits: pass each present window's used_percentage + resets_at through raw.
	if rlm, ok := obj["rate_limits"].(map[string]any); ok {
		var acc rateLimits
		set := false
		if w, ok := rlm["five_hour"].(map[string]any); ok {
			acc.FiveHour = &rlWindow{UsedPercentage: w["used_percentage"], ResetsAt: w["resets_at"]}
			set = true
		}
		if w, ok := rlm["seven_day"].(map[string]any); ok {
			acc.SevenDay = &rlWindow{UsedPercentage: w["used_percentage"], ResetsAt: w["resets_at"]}
			set = true
		}
		if set {
			rl = &acc
		}
	}

	// cost: keep a real numeric total_cost_usd (including 0.0); a JSON bool decodes
	// to bool (not float64) so the isinstance-bool exclusion is automatic.
	if cm, ok := obj["cost"].(map[string]any); ok {
		if tc, ok := cm["total_cost_usd"].(float64); ok {
			c := tc
			cost = &c
		}
	}

	// tokens: THIS session's burn TODAY, summed from the transcript. An absent /
	// unreadable transcript ⇒ tokens omitted.
	if tp, ok := obj["transcript_path"].(string); ok && tp != "" {
		if fi, err := os.Stat(tp); err == nil && !fi.IsDir() {
			tokens = parseTranscriptTokens(tp)
		}
	}

	has = rl != nil || cost != nil || tokens != nil
	return rl, cost, tokens, has
}

// parseTranscriptTokens sums today's assistant-message token usage from a Claude
// Code transcript (JSONL): burned = input + cache_creation; output; cache_read =
// cheap cache hits. Returns nil when no matching row is seen or on any read fault
// (mirrors the Python seen=False path). "Today" is the current UTC date, matching
// Python's time.strftime("%Y-%m-%d", time.gmtime()).
func parseTranscriptTokens(path string) *tokensBody {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	today := time.Now().UTC().Format("2006-01-02")
	var burned, output, cacheRead int
	seen := false

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024) // transcript lines can be large
	for sc.Scan() {
		var ev map[string]any
		if json.Unmarshal(sc.Bytes(), &ev) != nil { // a bad line is skipped
			continue
		}
		if t, _ := ev["type"].(string); t != "assistant" {
			continue
		}
		ts := ""
		if v := ev["timestamp"]; v != nil {
			ts = pyStr(v)
		}
		if !strings.HasPrefix(ts, today) {
			continue
		}
		msg, _ := ev["message"].(map[string]any)
		usage, _ := msg["usage"].(map[string]any)
		if len(usage) == 0 { // `if not u: continue`
			continue
		}
		seen = true
		burned += intOrZero(usage["input_tokens"]) + intOrZero(usage["cache_creation_input_tokens"])
		output += intOrZero(usage["output_tokens"])
		cacheRead += intOrZero(usage["cache_read_input_tokens"])
	}
	if sc.Err() != nil { // a read fault ⇒ discard (Python outer except ⇒ seen=False)
		return nil
	}
	if !seen {
		return nil
	}
	return &tokensBody{Burned: burned, Output: output, CacheRead: cacheRead}
}

// intOrZero mirrors Python's int(value or 0) for a JSON-decoded value: a JSON
// number (float64) truncates toward zero; anything else (nil / non-number) ⇒ 0.
func intOrZero(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}
