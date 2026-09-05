package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
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

// reportBackoffCapSecs is the CEILING on the retry spacing while the server keeps
// refusing — one attempt per 5 minutes, and never slower than that no matter how
// long the outage lasts. The cap is DELIBERATE and load-bearing, not an artefact:
// without it an hour-long outage would push the retry spacing past the outage
// itself, so a server that came back would go unnoticed for longer than it was
// ever down. 10x the healthy window is the trade: ~12 attempts/hour while broken
// (vs ~120/hour healthy), and recovery is still seen within 5 minutes.
const reportBackoffCapSecs = 300.0

// contextBody is the context POST wire body: {context_pct}.
//
// NO agent_id: the gauge key is the verified JWT sub (identity-from-token), the
// frozen AgentContextIngestDTO does not declare the field, and the server's
// mutable-write decoder REFUSES unknown keys — a self-reported agent_id makes
// the whole POST a 422. See the telemetryBody note below.
type contextBody struct {
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

// telemetryBody is the monitoring telemetry POST wire body.
//
// 🔴 EVERY KEY HERE MUST EXIST IN THE FROZEN AgentTelemetryIngestDTO
// (spec/openapi.json, additionalProperties:false). The server routes all mutable
// writes through a DisallowUnknownFields decoder, so ONE undeclared key 422s the
// WHOLE report — usage, cost and account together. That is exactly how this
// reporter went dark: it kept sending the retired self-reported `agent_id`
// (identity is the verified JWT sub, never a body field) and every POST was
// refused. `telemetry_wire_test.go` pins each key against the frozen schema.
//
// rate_limits/cost/tokens are pointers with omitempty so an absent source is
// dropped — and, crucially, cost is *float64 so a REAL 0.0 (a brand-new session)
// is KEPT while a missing cost is omitted (a plain float64 with omitempty would
// wrongly drop 0.0). machine is always present in practice (localHost defaults to
// the server-self id) but stays omitempty for the empty guard.
//
// Runtime is ALWAYS "claude" — this reporter only ever runs as a Claude Code
// statusLine command. It doubles as the report's identity payload: it satisfies
// the server's "at least one telemetry field" admission rule, so an identity-only
// report (no usage measured yet) is still accepted instead of being dropped.
type telemetryBody struct {
	Runtime    string      `json:"runtime"`
	RateLimits *rateLimits `json:"rate_limits,omitempty"`
	Cost       *float64    `json:"cost,omitempty"`
	Tokens     *tokensBody `json:"tokens,omitempty"`
	// Account is the stable monitoring attribution key, OMITTED when unreadable.
	// Never a literal "unknown": a sentinel would mint a phantom account row in
	// the owner's monitoring fold that looks like a real, separate account.
	Account string `json:"account,omitempty"`
	// AccountLabel is the human-readable owner-facing label for the account key
	// ("<emailAddress>(<organizationName>)" from oauthAccount, T-260e). Display
	// only — never part of the account KEY — and omitted when unreadable (the
	// server must see absent, not "").
	AccountLabel string `json:"account_label,omitempty"`
	Machine      string `json:"machine,omitempty"`
	// Effort is the session's LIVE reasoning effort, read verbatim from the
	// statusLine payload's effort.level (see effortValue: never OC_EFFORT, never
	// the status line's "med" abbreviation). The key is already declared on the
	// frozen AgentTelemetryIngestDTO, so this is not a wire change; the codex
	// sidecar has been sending it all along and the monitoring session DTO has
	// always served it. Omitted when the payload carries no effort block: an empty
	// string would turn "this model has no effort" into a reported blank, and that
	// blank is exactly what hid this bug for as long as it lasted.
	Effort string `json:"effort,omitempty"`
	// Model is the session's LIVE model id, read from the statusLine payload's
	// model.id (see modelValue). Same PRODUCER contract as Effort — reported
	// state, omitted when unmeasured — and it shares the bug that motivated it,
	// but the two are NOT twins downstream: the server persists model onto the
	// roster row's actual_model and serves it from there, while effort lives
	// only in the in-memory telemetry entry (there is no actual_effort column).
	// The bug: the model was on the status-line string from the
	// start and in no POST body ever, so the cockpit's 模型 column had to fall
	// back to the owner's configured launch value to show anything at all — and
	// for an outsource worker, which has no configured value on that read path,
	// it showed nothing at all. Omitted when the payload carries no model.
	Model string `json:"model,omitempty"`
}

// cmdContextReport implements `ocagent context-report`. `now` is the current unix
// time in fractional seconds (mirrors time.time()) — injected so the throttle is
// testable. ALWAYS returns 0 (best-effort + dual-use status line), identical to
// Python cmd_context_report.
func cmdContextReport(client httpClient, cfg Config, env func(string) string, now float64, stdin io.Reader, out, errw io.Writer) int {
	payload := ""
	if raw, err := io.ReadAll(stdin); err == nil { // a read fault degrades to ""
		payload = string(raw)
	}
	pct, havePct := statuslinePct(payload)

	// OC_BASE CLASSIFICATION: SIGNAL ONLY — stderr line, never a refusal.
	// The OC_BASE mis-wire signal, and DELIBERATELY the only half of the guard
	// this subcommand takes (Kyle, T-86: option 丙). The other three subcommands
	// refuse and exit non-zero; this one MUST NOT, because the fail-safe
	// documented at the top of this file — always print the status line, always
	// exit 0 — is what keeps a mis-wired agent's TUI working, and statusLine is
	// fed by this command on EVERY turn. Loud failure here would break the
	// status line on every turn of every conversation.
	//
	// So: stdout is untouched, the exit code is untouched, and the ONLY change
	// is one line on stderr, which statusLine does not read. requireBase's
	// return is ignored ON PURPOSE — the refusal is a signal here, not a
	// decision — and because it fires only when OC_BASE is genuinely absent, a
	// correctly wired agent prints nothing extra on any of those turns.
	_ = requireBase(cfg, "context-report", errw)

	if cfg.Token != "" && cfg.ID != "" {
		stamp := reportStampPath(cfg)
		// TWO independent gates, and they answer two different questions. The stamp
		// answers "did we DELIVER recently?" (the healthy 30s window). The backoff
		// record answers "how long has the server been REFUSING?" — the state the
		// stamp deliberately refuses to carry, because a stamp is evidence of
		// delivery and must never be advanced by a failure. Splitting them is what
		// lets a failing path slow down WITHOUT the stamp ever lying about health.
		backoffFile := reportBackoffPath(cfg)
		backoff := readReportBackoff(backoffFile)
		if !reportThrottled(stamp, now, reportThrottleSecs) && !reportBackedOff(backoff, now) {
			// Context POST needs a real pct: when pct is absent (fresh/compacted
			// session — used_percentage is null) we honestly SKIP it rather than
			// fabricate a fake 0. pct gates ONLY its own POST, never the telemetry.
			delivered := true
			if havePct {
				delivered = reportPost(client, cfg, "/api/agent/context", contextBody{ContextPct: pct}, errw) && delivered
			}
			// ADDITIVE telemetry POST, pct-INDEPENDENT — and IDENTITY-INDEPENDENT of
			// usage. A measured usage source (rate_limits / cost / tokens) is added
			// when present, but its ABSENCE must never suppress the report: runtime +
			// account + machine are facts we always know, and withholding them left
			// the owner's monitoring fold showing a stale runtime's account (or none)
			// for a live claude session. runtime is always set, so the body is never
			// empty and the server never has to reject it.
			rl, cost, tokens := buildTelemetry(payload)
			body := telemetryBody{
				Runtime:      "claude",
				RateLimits:   rl,
				Cost:         cost,
				Tokens:       tokens,
				Account:      readClaudeAccount(env),
				AccountLabel: readClaudeAccountLabel(env),
				Effort:       effortValue(payload),
				Model:        modelValue(payload),
			}
			if machine := localHost(env); machine != "" {
				body.Machine = machine
			}
			delivered = reportPost(client, cfg, "/api/monitoring/telemetry", body, errw) && delivered
			// Stamp the throttle window ONLY when every POST this burst attempted was
			// ACCEPTED by the server. The stamp is not a "we tried" marker: it is the
			// only externally-readable evidence that this reporter is delivering, and
			// advancing it on a refusal did two harms at once — it argued the path was
			// healthy while 100% of reports were being refused, and it SUPPRESSED the
			// retry for a further window. A refused burst therefore leaves the previous
			// stamp untouched, so the very next tick reports again (and keeps logging
			// the refusal) until the server accepts. Success keeps the throttle exactly
			// as before: one burst per window, so pct=None ticks never re-POST
			// telemetry every tick.
			//
			// The backoff record is the SECOND half of that: leaving the window open
			// meant the retry went out on the very next statusLine tick, and a
			// statusLine ticks several times a SECOND. So against a server that keeps
			// refusing, the throttle was effectively switched off — measured at ~0.4s
			// between bursts, unbounded, for as long as the outage lasted. A refused
			// burst therefore also records the consecutive-failure count, which spaces
			// the retries out (30s, 60s, 120s, 240s, then the 300s cap). Delivery
			// CLEARS the record, so one accepted burst restores the plain 30s cadence
			// immediately — the backoff must never outlive the outage that caused it.
			if delivered {
				writeStamp(stamp, now)
				clearReportBackoff(backoffFile)
			} else {
				writeReportBackoff(backoffFile, reportBackoffState{
					failures: backoff.failures + 1, lastAttempt: now,
				})
			}
		}
	}

	fmt.Fprintln(out, renderStatusline(payload, now))
	return 0
}

// reportPost POSTs one best-effort report and — unlike the bare postJSON it wraps
// — leaves an OBSERVABLE TRACE when the server refuses it. A rejected report used
// to be indistinguishable from a delivered one at every layer: postJSON dropped
// the status, cmdContextReport always exits 0, and the throttle stamp used to
// advance on attempt rather than on success, so a reporter that had 422'd every 30 seconds
// for hours still looked perfectly healthy from the outside. The line goes to
// STDERR (never `out` — Claude Code renders stdout verbatim as the status line)
// and includes the response body, because the interesting refusals are schema
// refusals that name the offending key. Delivery stays best-effort for the exit
// code (always 0), but the verdict is NOT thrown away: it is returned so the
// caller can bind the throttle stamp to delivery instead of to attempt.
//
// Returns true iff the server ACCEPTED the report (2xx). A transport fault
// (status 0) counts as not-delivered too — nothing was stored either way.
func reportPost(client httpClient, cfg Config, path string, body any, errw io.Writer) bool {
	status, detail := httpRequest(client, http.MethodPost, cfg.Base+path, cfg.Token, body)
	if status >= 200 && status < 300 {
		return true
	}
	if errw == nil {
		return false
	}
	fmt.Fprintf(errw, "[ocagent] context-report: POST %s FAILED status=%d: %s\n",
		path, status, truncateForLog(strings.TrimSpace(detail)))
	return false
}

// truncateForLog caps a diagnostic string so a large transcript-derived body
// cannot flood the warden log.
func truncateForLog(s string) string {
	const max = 400
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
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

// renderStatusline builds the full status line from a statusLine JSON payload and
// now (fractional unix seconds, for the rate-limit reset/elapsed maths — injected
// so tests are deterministic). Every segment now comes out of the payload. Returns
// the ready-to-print line WITHOUT a trailing newline. Never panics: a nil / junk
// payload yields an empty line.
func renderStatusline(payload string, now float64) string {
	obj, _ := safeJSON(payload).(map[string]any) // nil ⇒ every segment skips

	var segs []string
	if s := modelEffortSegment(obj); s != "" {
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
// display_name doesn't already say so. The effort (yellow) is present iff the
// payload carries effort.level; a bare effort with no model still renders
// "⚡<effort>". Both missing ⇒ "".
func modelEffortSegment(obj map[string]any) string {
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
	if effort := effortLabel(obj); effort != "" {
		e := ansiYellow + "⚡" + effort + ansiReset
		if out != "" {
			out += " " + e
		} else {
			out = e
		}
	}
	return out
}

// effortValue reads the LIVE reasoning effort out of the statusLine payload
// (`effort.level`), trimmed and VERBATIM. Claude Code carries the session's
// CURRENT level there — it tracks a mid-session /effort change, which the launch
// intent cannot — and OMITS the block entirely on models that have no effort
// parameter, so an absent level is an honest "this session has no effort", not a
// gap to paper over.
//
// 🔴 Deliberately NOT OC_EFFORT, and there is no fallback to it. OC_EFFORT is the
// owner's LAUNCH INTENT (what the session was started with); the monitoring
// surfaces must show what the session IS, never what it was configured to be
// (owner, 2026-07-31). A fallback would reintroduce exactly that: a session that
// dropped to low mid-run would keep displaying the "high" it was launched at, and
// nobody could tell the difference from a live report.
func effortValue(payload string) string {
	obj, _ := safeJSON(payload).(map[string]any)
	return effortLevel(obj)
}

// modelValue reads the LIVE model out of the statusLine payload (`model.id`),
// trimmed and VERBATIM. Same contract as effortValue: reported state, never the
// launch configuration, and no fallback to OC_MODEL / the roster's configured
// model — a session the owner started as "opus" but that is actually running
// something else must not keep displaying the intent.
//
// 🔴 `model.id` and NOT `model.display_name`, which is what the status-line
// SEGMENT renders. Two reasons, both load-bearing:
//
//   - The id is the identifier the boot seed already tells a member to report
//     ("填 Claude Code 提供的真實 model id,不要猜值"), so member self-reports and
//     this reporter land the same vocabulary in the same column.
//   - Only the id carries the "[1m]" 1M-context marker. display_name says
//     "Opus 4.5" for both the 1M and the standard tier, so sending it would
//     collapse two genuinely different sessions onto one string — and the
//     cockpit shows that distinction today.
//
// Absent or malformed ⇒ "" (honest blank, omitted by omitempty), never a
// fabricated default.
func modelValue(payload string) string {
	obj, _ := safeJSON(payload).(map[string]any)
	return modelID(obj)
}

// modelID pulls `model.id` out of an already-decoded payload.
func modelID(obj map[string]any) string {
	block, ok := obj["model"].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := block["id"].(string)
	return strings.TrimSpace(id)
}

// effortLevel pulls `effort.level` out of an already-decoded payload. Absent or
// malformed ⇒ "" (honest blank), never a fabricated default.
func effortLevel(obj map[string]any) string {
	block, ok := obj["effort"].(map[string]any)
	if !ok {
		return ""
	}
	level, _ := block["level"].(string)
	return strings.TrimSpace(level)
}

// effortLabel is the status-line rendering of the same live value. "medium"
// abbreviates to "med" to match the owner's target layout; other values pass
// through verbatim. Empty ⇒ "". The abbreviation is display-only and must never
// reach the POST body — the cockpit would then read a level no /effort command
// or launch flag ever names.
func effortLabel(obj map[string]any) string {
	e := effortLevel(obj)
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

// ---------------------------------------------------------------------------
// failure backoff (T-d11f)
//
// `ocagent context-report` is a ONE-SHOT process: Claude Code re-execs it on
// every statusLine render, so there is no in-process loop to hold a backoff in
// (unlike ocwarden's report loop, which keeps `backoff` in a local). All cadence
// state therefore has to live on disk beside the throttle stamp.
// ---------------------------------------------------------------------------

// reportBackoffState is the on-disk consecutive-failure record: how many bursts
// in a row were NOT delivered, and when the last one was attempted. The zero
// value means "healthy" (no backoff in effect).
//
// "Not delivered" deliberately includes TRANSPORT FAULTS (connection refused, a
// dead server, a timeout — reportPost's status-0 case), not just schema refusals.
// ⚠️ This DIVERGES from ocwarden's report loop, which treats status 0 as
// "not the server's fault" and RESETS its backoff (`result.Posted ||
// result.Status == 0`). The divergence is intentional and the shapes are not
// comparable: ocwarden holds its backoff in a live loop whose floor is already
// one attempt per second, whereas this reporter is re-exec'd on every statusLine
// render — several times a second — so "server is down" is precisely the case
// where the unbounded retry storm was worst, and a down server is the LEAST able
// to absorb it. Resetting on status 0 here would leave the original bug fully
// intact for the most common outage of all.
type reportBackoffState struct {
	failures    int
	lastAttempt float64
}

// reportBackoffPath is the failure record, a SIBLING of the throttle stamp (same
// per-agent dir). Kept in its own file on purpose: the stamp's only meaning is
// "when did we last DELIVER", and folding attempt state into it is exactly the
// bug that made a 100%-refused reporter look healthy from the outside.
func reportBackoffPath(cfg Config) string {
	return filepath.Join(filepath.Dir(reportStampPath(cfg)), "context_report.backoff")
}

// reportBackoffSecs is the MINIMUM spacing between attempts after `failures`
// consecutive refused bursts: the throttle window doubled once per extra failure,
// CAPPED at reportBackoffCapSecs. failures<=0 (healthy) ⇒ 0, i.e. the plain
// throttle governs and nothing about the healthy cadence changes.
//
// The first failure yields exactly reportThrottleSecs, so a REFUSING reporter is
// never denser than a healthy one — and from there it strictly thins out.
func reportBackoffSecs(failures int) float64 {
	if failures <= 0 {
		return 0
	}
	wait := reportThrottleSecs
	for i := 1; i < failures; i++ {
		wait *= 2
		if wait >= reportBackoffCapSecs {
			return reportBackoffCapSecs
		}
	}
	return wait
}

// reportBackedOff is true iff a refused burst was attempted within this state's
// backoff window (skip this tick). Mirrors reportThrottled's forgiving shape: a
// zero/absent record is never a reason to suppress.
func reportBackedOff(st reportBackoffState, now float64) bool {
	if st.failures <= 0 {
		return false
	}
	return (now - st.lastAttempt) < reportBackoffSecs(st.failures)
}

// readReportBackoff parses the failure record ("<failures> <lastAttempt>"). Any
// fault — missing, empty, truncated, unparseable, nonsensical count — reads as
// the healthy zero value, i.e. DO NOT suppress. Fail-open is the right direction
// here for the same reason writeStamp is best-effort: an unreadable scratch file
// must never be able to silence a working reporter. (The cost is symmetric: if
// the record can never be WRITTEN, the backoff cannot engage — that is the same
// pre-existing exposure the throttle stamp already has.)
func readReportBackoff(path string) reportBackoffState {
	raw, err := os.ReadFile(path)
	if err != nil {
		return reportBackoffState{}
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		return reportBackoffState{}
	}
	failures, errN := strconv.Atoi(fields[0])
	last, errT := strconv.ParseFloat(fields[1], 64)
	if errN != nil || errT != nil || failures < 1 {
		return reportBackoffState{}
	}
	return reportBackoffState{failures: failures, lastAttempt: last}
}

// writeReportBackoff records a refused burst best-effort (a write fault is
// swallowed, matching writeStamp).
func writeReportBackoff(path string, st reportBackoffState) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(strconv.Itoa(st.failures)+" "+
		strconv.FormatFloat(st.lastAttempt, 'f', -1, 64)), 0o644)
}

// clearReportBackoff drops the failure record after a delivered burst, so the very
// next tick is governed by the plain throttle again. Best-effort; an absent file
// is the normal case (a healthy reporter never writes one).
func clearReportBackoff(path string) {
	_ = os.Remove(path)
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
// Claude account: the OAuth accountUuid joined with oauthAccount's
// organizationUuid as "<accountUuid>/<organizationUuid>" — bare accountUuid
// when no org is present (never a dangling "<accountUuid>/"), "" when no
// account identity is found. Both dimensions come from .claude.json only,
// which the claude CLI writes regardless of where credentials live (file or
// macOS Keychain), so the key is
// stable across machines and credential storage forms. subscriptionType does
// not join the key. Legacy config without accountUuid falls back to userID so
// existing reports remain attributable. Both candidates
// (~/.claude/.claude.json then ~/.claude.json) are scanned and each dimension
// resolves independently — real installs split them across the two files.
func readClaudeAccount(env func(string) string) string {
	home := env("HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	accountUUID, userID, org := "", "", ""
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
		if accountUUID == "" {
			accountUUID = claudeAccountUUID(d)
		}
		if org == "" {
			org = claudeOrgUUID(d)
		}
	}
	identity := accountUUID
	if identity == "" {
		identity = userID
	}
	if identity == "" {
		return ""
	}
	if org != "" {
		return identity + "/" + org
	}
	return identity
}

// readClaudeAccountLabel returns the human-readable OWNER-FACING label for the
// logged-in Claude account — "<emailAddress>(<organizationName>)" from
// .claude.json's oauthAccount (T-260e). This is DISPLAY ONLY: the stable
// account key stays readClaudeAccount's accountUuid/org dimensions; the label
// never
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
// so readClaudeAccount omits the org suffix rather than appending an empty
// (or "None") suffix.
func claudeOrgUUID(d map[string]any) string {
	oauth, ok := d["oauthAccount"].(map[string]any)
	if !ok {
		return ""
	}
	org, _ := oauth["organizationUuid"].(string)
	return strings.TrimSpace(org)
}

func claudeAccountUUID(d map[string]any) string {
	oauth, ok := d["oauthAccount"].(map[string]any)
	if !ok {
		return ""
	}
	account, _ := oauth["accountUuid"].(string)
	return strings.TrimSpace(account)
}

// buildTelemetry parses a statusLine payload into the MEASURED telemetry pieces
// (rate_limits, cost, tokens). Every field is OMITTED when its source is missing
// (the panel shows 未量到, never a fabricated 0); a real 0.0 cost IS kept. Mirrors
// _build_telemetry. It deliberately reports no "anything present?" verdict: the
// caller must not gate the report — least of all the account identity — on
// whether usage happened to be measurable this tick.
func buildTelemetry(raw string) (rl *rateLimits, cost *float64, tokens *tokensBody) {
	obj, ok := safeJSON(raw).(map[string]any)
	if !ok {
		return nil, nil, nil
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

	return rl, cost, tokens
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
