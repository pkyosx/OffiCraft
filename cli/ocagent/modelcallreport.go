package main

// Claude Code runs `ocagent model-call-report` as the member's Stop and
// StopFailure hook (cli/ocwarden/spawn.go). A normal turn fires only Stop, an
// API error only StopFailure. Both are fire-and-forget: the exit code and
// stdout are ignored, so it always exits 0 and diagnostics go to stderr.
//
// Stop stays off the network except for the first success after a failure:
// that one is sent at once, because context-report only runs when the status
// line re-renders, which an idle member may never do again.
//
// OC_BASE CLASSIFICATION: SIGNAL ONLY — stderr line, never a refusal: a refusal
// would drop the one failure report this turn produces.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const modelCallHookInputCap = 1 << 20

const modelCallTelemetryPath = "/api/monitoring/telemetry"

// Shorter than httpTimeout: nothing reads the answer, so a hung station should
// not keep a hook process alive for long.
const modelCallReportTimeout = 5 * time.Second

// Only the tail is read: transcripts grow without bound, and the error row the
// harness just wrote is the last thing in the file.
const transcriptTailBytes = 256 << 10

type modelCallFailure struct {
	Ts       float64  `json:"ts"`
	Kind     string   `json:"kind"`
	Code     string   `json:"code"`
	ResetsAt *float64 `json:"resets_at"`
}

type modelCallReport struct {
	LastFailure   *modelCallFailure `json:"last_failure,omitempty"`
	LastSuccessTs *float64          `json:"last_success_ts,omitempty"`
}

type modelCallBody struct {
	Runtime      string          `json:"runtime"`
	Account      string          `json:"account,omitempty"`
	AccountLabel string          `json:"account_label,omitempty"`
	Machine      string          `json:"machine,omitempty"`
	ModelCall    modelCallReport `json:"model_call"`
}

type stopHookInput struct {
	HookEventName  string `json:"hook_event_name"`
	Error          string `json:"error"`
	TranscriptPath string `json:"transcript_path"`
}

var claudeModelCallKinds = map[string]string{
	"authentication_failed":  "auth",
	"oauth_org_not_allowed":  "auth",
	"account_on_hold":        "auth",
	"verification_required":  "auth",
	"cloud_credential_error": "auth",
	"rate_limit":             "rate_limit",
	"server_error":           "server",
	"overloaded":             "server",
}

func claudeModelCallKind(code string) string {
	if kind, ok := claudeModelCallKinds[code]; ok {
		return kind
	}
	return "other"
}

func cmdModelCallReport(client httpClient, cfg Config, env func(string) string, now float64, stdin io.Reader, errOut io.Writer) int {
	raw, _ := io.ReadAll(io.LimitReader(stdin, modelCallHookInputCap))
	var in stopHookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		fmt.Fprintf(errOut, "[ocagent] model-call-report: hook input is not JSON: %v\n", err)
		return 0
	}
	switch in.HookEventName {
	case "Stop":
		writeModelCallTime(modelCallSuccessPath(cfg), now)
		reportClearingSuccess(client, cfg, env, now, errOut)
	case "StopFailure":
		reportModelCallFailure(client, cfg, env, now, in, errOut)
	}
	return 0
}

func reportModelCallFailure(client httpClient, cfg Config, env func(string) string, now float64, in stopHookInput, errOut io.Writer) {
	code := strings.TrimSpace(in.Error)
	if code == "" {
		code = "unknown"
	}
	failure := modelCallFailure{Ts: now, Kind: claudeModelCallKind(code), Code: code}
	if failure.Kind == "rate_limit" {
		failure.ResetsAt = transcriptQuotaResetsAt(in.TranscriptPath)
	}
	writeModelCallTime(modelCallFailurePath(cfg), now)

	_ = warnMissingBase(cfg, "model-call-report", errOut)
	if cfg.Token == "" || cfg.MemberID == "" {
		return
	}
	report := modelCallReport{LastFailure: &failure}
	if success, ok := readModelCallTime(modelCallSuccessPath(cfg)); ok {
		report.LastSuccessTs = &success
	}
	postModelCall(client, cfg, newModelCallBody(env, report), errOut)
}

func reportClearingSuccess(client httpClient, cfg Config, env func(string) string, success float64, errOut io.Writer) {
	failure, failed := readModelCallTime(modelCallFailurePath(cfg))
	sent, _ := readModelCallTime(modelCallSuccessSentPath(cfg))
	if !failed || failure <= sent {
		return
	}
	_ = warnMissingBase(cfg, "model-call-report", errOut)
	if cfg.Token == "" || cfg.MemberID == "" {
		return
	}
	if postModelCall(client, cfg, newModelCallBody(env, modelCallReport{LastSuccessTs: &success}), errOut) {
		writeModelCallTime(modelCallSuccessSentPath(cfg), success)
	}
}

func newModelCallBody(env func(string) string, report modelCallReport) modelCallBody {
	return modelCallBody{
		Runtime:      "claude",
		Account:      readClaudeAccount(env),
		AccountLabel: readClaudeAccountLabel(env),
		Machine:      machineID(env),
		ModelCall:    report,
	}
}

func postModelCall(client httpClient, cfg Config, body modelCallBody, errOut io.Writer) bool {
	status, detail := httpRequest(client, http.MethodPost, cfg.Base+modelCallTelemetryPath, cfg.Token, body)
	if status >= 200 && status < 300 {
		return true
	}
	fmt.Fprintf(errOut, "[ocagent] model-call-report: POST /api/monitoring/telemetry FAILED status=%d: %s\n",
		status, truncateForLog(strings.TrimSpace(detail)))
	return false
}

// The reset time is only in the transcript: the harness's last
// isApiErrorMessage row carries quotaLimits.resetsAt (epoch seconds). Anything
// unreadable is "the runtime did not say", never an error.
func transcriptQuotaResetsAt(path string) *float64 {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return nil
	}
	start := info.Size() - transcriptTailBytes
	if start < 0 {
		start = 0
	}
	tail := make([]byte, info.Size()-start)
	if _, err := f.ReadAt(tail, start); err != nil && err != io.EOF {
		return nil
	}
	lines := bytes.Split(tail, []byte("\n"))
	if start > 0 {
		lines = lines[1:]
	}
	for i := len(lines) - 1; i >= 0; i-- {
		var row struct {
			IsAPIErrorMessage bool `json:"isApiErrorMessage"`
			QuotaLimits       struct {
				ResetsAt *float64 `json:"resetsAt"`
			} `json:"quotaLimits"`
		}
		if json.Unmarshal(lines[i], &row) != nil || !row.IsAPIErrorMessage {
			continue
		}
		return row.QuotaLimits.ResetsAt
	}
	return nil
}

func modelCallSuccessPath(cfg Config) string {
	return filepath.Join(filepath.Dir(reportStampPath(cfg)), "model_call.success")
}

func modelCallFailurePath(cfg Config) string {
	return filepath.Join(filepath.Dir(reportStampPath(cfg)), "model_call.failure")
}

// The newest success time the Stop hook got accepted: once it is newer than the
// last failure, Stop goes back to staying off the network.
func modelCallSuccessSentPath(cfg Config) string {
	return filepath.Join(filepath.Dir(reportStampPath(cfg)), "model_call.success_sent")
}

func readModelCallTime(path string) (float64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	ts, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil || ts <= 0 {
		return 0, false
	}
	return ts, true
}

func writeModelCallTime(path string, ts float64) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(strconv.FormatFloat(ts, 'f', -1, 64)), 0o644)
}
