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

// Claude Code runs `ocagent context-report` as each member's statusLine command
// on every render: stdin is the statusLine JSON and stdout is shown verbatim as
// the status line. So diagnostics go to stderr
// only, and it always prints the line and exits 0.

const reportThrottleSecs = 30.0

const reportBackoffCapSecs = 300.0

// No agent_id: the server refuses keys AgentContextIngestDTO does not declare
// (422); identity is the verified JWT sub.
type contextBody struct {
	ContextPct float64 `json:"context_pct"`
}

// Values pass through raw: the server parses resets_at as a unix-epoch number,
// so never convert it (e.g. to ISO).
type rlWindow struct {
	UsedPercentage any `json:"used_percentage"`
	ResetsAt       any `json:"resets_at"`
}

type rateLimits struct {
	FiveHour *rlWindow `json:"five_hour,omitempty"`
	SevenDay *rlWindow `json:"seven_day,omitempty"`
}

type tokensBody struct {
	Burned    int `json:"burned"`
	Output    int `json:"output"`
	CacheRead int `json:"cache_read"`
}

// 🔴 Every key must exist in the frozen AgentTelemetryIngestDTO
// (spec/openapi.json): the server decodes with DisallowUnknownFields, so ONE
// undeclared key 422s the whole report.
//
// Runtime (always "claude") satisfies the server's "at least one telemetry
// field" admission rule, so an identity-only report is still accepted.
type telemetryBody struct {
	Runtime    string      `json:"runtime"`
	RateLimits *rateLimits `json:"rate_limits,omitempty"`
	Cost       *float64    `json:"cost,omitempty"`
	Tokens     *tokensBody `json:"tokens,omitempty"`
	// Omitted when unreadable, never "unknown": a sentinel would show up as a
	// phantom account row in the owner's monitoring.
	Account      string `json:"account,omitempty"`
	AccountLabel string `json:"account_label,omitempty"`
	Machine      string `json:"machine,omitempty"`
	Effort       string `json:"effort,omitempty"`
	Model        string `json:"model,omitempty"`
}

func cmdContextReport(client httpClient, cfg Config, env func(string) string, now float64, stdin io.Reader, out, errOut io.Writer) int {
	payload := ""
	if raw, err := io.ReadAll(stdin); err == nil {
		payload = string(raw)
	}
	pct, havePct := statuslinePct(payload)

	// OC_BASE CLASSIFICATION: SIGNAL ONLY — stderr line, never a refusal.
	// Kyle, T-86 option 丙: statusLine runs this every turn, so a refusal would
	// break the status line on every turn.
	_ = warnMissingBase(cfg, "context-report", errOut)

	if cfg.Token != "" && cfg.MemberID != "" {
		stamp := reportStampPath(cfg)
		backoffFile := reportBackoffPath(cfg)
		backoff := readReportBackoff(backoffFile)
		if !reportThrottled(stamp, now, reportThrottleSecs) && !reportBackedOff(backoff, now) {
			delivered := true
			if havePct {
				delivered = reportPost(client, cfg, "/api/agent/context", contextBody{ContextPct: pct}, errOut) && delivered
			}
			// Sent even when no usage was measured: runtime/account/machine must still
			// reach monitoring, or it keeps showing a stale account for a live session.
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
			if machine := machineID(env); machine != "" {
				body.Machine = machine
			}
			delivered = reportPost(client, cfg, "/api/monitoring/telemetry", body, errOut) && delivered
			// The stamp advances only when every POST was accepted: it is the only
			// outside evidence of delivery, so a refusal must never advance it.
			// Refusals go to the separate backoff record instead — statusLine ticks
			// several times a second, and a refusing server was measured at ~0.4s
			// between bursts without it.
			if delivered {
				writeReportStamp(stamp, now)
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

func reportPost(client httpClient, cfg Config, path string, body any, errOut io.Writer) bool {
	status, detail := httpRequest(client, http.MethodPost, cfg.Base+path, cfg.Token, body)
	if status >= 200 && status < 300 {
		return true
	}
	if errOut == nil {
		return false
	}
	fmt.Fprintf(errOut, "[ocagent] context-report: POST %s FAILED status=%d: %s\n",
		path, status, truncateForLog(strings.TrimSpace(detail)))
	return false
}

func truncateForLog(s string) string {
	const max = 400
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

const (
	ansiReset  = "\x1b[0m"
	ansiBlue   = "\x1b[34m"
	ansiYellow = "\x1b[33m"
	ansiGreen  = "\x1b[32m"
	ansiGray   = "\x1b[90m"

	statusBarWidth = 10
)

func renderStatusline(payload string, now float64) string {
	obj, _ := safeJSON(payload).(map[string]any)

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

// 🔴 Never OC_EFFORT (the launch intent), and no fallback to it: monitoring
// shows what the session IS (owner, 2026-07-31).
func effortValue(payload string) string {
	obj, _ := safeJSON(payload).(map[string]any)
	return effortLevel(obj)
}

// model.id, not display_name: only the id carries the "[1m]" 1M-context marker,
// and it is what the boot seed tells members to self-report into the same
// column. No fallback to OC_MODEL / the roster's configured model.
func modelValue(payload string) string {
	obj, _ := safeJSON(payload).(map[string]any)
	return modelID(obj)
}

func modelID(obj map[string]any) string {
	block, ok := obj["model"].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := block["id"].(string)
	return strings.TrimSpace(id)
}

func effortLevel(obj map[string]any) string {
	block, ok := obj["effort"].(map[string]any)
	if !ok {
		return ""
	}
	level, _ := block["level"].(string)
	return strings.TrimSpace(level)
}

func effortLabel(obj map[string]any) string {
	e := effortLevel(obj)
	if e == "medium" {
		return "med"
	}
	return e
}

func contextBarSegment(payload string) string {
	pct, ok := statuslinePct(payload)
	if !ok {
		return ""
	}
	filled := int(math.Round(pct / 100 * statusBarWidth))
	if filled < 0 {
		filled = 0
	}
	if filled > statusBarWidth {
		filled = statusBarWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", statusBarWidth-filled)
	shown := int(math.RoundToEven(pct))
	return ansiGray + bar + ansiReset + " " + ansiGreen + strconv.Itoa(shown) + "%" + ansiReset
}

func costSegment(obj map[string]any) string {
	cm, ok := obj["cost"].(map[string]any)
	if !ok {
		return ""
	}
	tc, ok := cm["total_cost_usd"].(float64)
	if !ok {
		return ""
	}
	return fmt.Sprintf("$%.2f", tc)
}

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

func rlWindowFields(w map[string]any) (used, resetsAt float64, ok bool) {
	up, upOK := w["used_percentage"].(float64)
	ra, raOK := w["resets_at"].(float64)
	if !upOK || !raOK {
		return 0, 0, false
	}
	return up, ra, true
}

func compactDuration(seconds float64) string {
	total := int(seconds)
	h, m := total/3600, (total%3600)/60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func statuslinePct(payload string) (float64, bool) {
	obj, ok := safeJSON(payload).(map[string]any)
	if !ok {
		return 0, false
	}
	cw, ok := obj["context_window"].(map[string]any)
	if !ok {
		return 0, false
	}
	pct, ok := cw["used_percentage"].(float64)
	if !ok {
		return 0, false
	}
	return math.Max(0.0, math.Min(100.0, pct)), true
}

func reportStampPath(cfg Config) string {
	key := strings.ToLower(cfg.MemberID)
	if key == "" {
		key = "anon"
	}
	return filepath.Join(cfg.AgentsRoot, key, "context_report.stamp")
}

func reportThrottled(stampPath string, now, minInterval float64) bool {
	raw, err := os.ReadFile(stampPath)
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		s = "0"
	}
	last, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false
	}
	return (now - last) < minInterval
}

// Transport faults (status 0) count as failures too — deliberately unlike
// ocwarden's report loop, which resets its backoff on status 0: this process is
// re-exec'd several times a second, so a down server is where the retry storm
// is worst.
type reportBackoffState struct {
	failures    int
	lastAttempt float64
}

func reportBackoffPath(cfg Config) string {
	return filepath.Join(filepath.Dir(reportStampPath(cfg)), "context_report.backoff")
}

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

func reportBackedOff(st reportBackoffState, now float64) bool {
	if st.failures <= 0 {
		return false
	}
	return (now - st.lastAttempt) < reportBackoffSecs(st.failures)
}

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

func writeReportBackoff(path string, st reportBackoffState) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(strconv.Itoa(st.failures)+" "+
		strconv.FormatFloat(st.lastAttempt, 'f', -1, 64)), 0o644)
}

func clearReportBackoff(path string) {
	_ = os.Remove(path)
}

func writeReportStamp(stampPath string, now float64) {
	if err := os.MkdirAll(filepath.Dir(stampPath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(stampPath, []byte(strconv.FormatFloat(now, 'f', -1, 64)), 0o644)
}

// "m-server-self" must equal ServerSelfHost in server/ocserverd/domain.go.
func machineID(env func(string) string) string {
	if h := env("OC_HOST"); h != "" {
		return h
	}
	return "m-server-self"
}

// The monitoring attribution key must stay stable across machines and
// credential storage, so subscriptionType is deliberately not part of it. Real
// installs split fields across the two .claude.json files, so each resolves
// independently.
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

func buildTelemetry(payload string) (rl *rateLimits, cost *float64, tokens *tokensBody) {
	obj, ok := safeJSON(payload).(map[string]any)
	if !ok {
		return nil, nil, nil
	}

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

	if cm, ok := obj["cost"].(map[string]any); ok {
		if tc, ok := cm["total_cost_usd"].(float64); ok {
			c := tc
			cost = &c
		}
	}

	if tp, ok := obj["transcript_path"].(string); ok && tp != "" {
		if fi, err := os.Stat(tp); err == nil && !fi.IsDir() {
			tokens = parseTranscriptTokens(tp)
		}
	}

	return rl, cost, tokens
}

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
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var ev map[string]any
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
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
		if len(usage) == 0 {
			continue
		}
		seen = true
		burned += intOrZero(usage["input_tokens"]) + intOrZero(usage["cache_creation_input_tokens"])
		output += intOrZero(usage["output_tokens"])
		cacheRead += intOrZero(usage["cache_read_input_tokens"])
	}
	if sc.Err() != nil {
		return nil
	}
	if !seen {
		return nil
	}
	return &tokensBody{Burned: burned, Output: output, CacheRead: cacheRead}
}

func intOrZero(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}
