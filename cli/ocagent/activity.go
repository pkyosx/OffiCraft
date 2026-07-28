package main

// activity.go — `ocagent report-activity`: the Claude-side reporter for the
// TURN dimension (T-a1d7). It is invoked from the Claude Code hooks the warden
// writes into each agent's settings.json (cli/ocwarden/spawn.go
// buildStatuslineSettings): UserPromptSubmit ⇒ active, Stop / StopFailure /
// SessionEnd ⇒ idle.
//
// THREE HARD RULES, each with a reason:
//
//  1. NEVER write to stdout. A UserPromptSubmit hook's stdout is fed to the
//     model as additional context — a stray diagnostic line would end up
//     injected into the agent's own prompt. Every message goes to stderr, which
//     lands in the warden log where an operator can actually find it.
//
//  2. ALWAYS exit 0. A hook is not a place to fail a turn from. The report is
//     best-effort by design: the server already degrades a claim it never hears
//     the end of into `unknown`, and the SSE drop covers a dead process, so a
//     lost report costs a display detail, never correctness.
//
//  3. NO throttle and NO failure backoff — the deliberate DIFFERENCE from
//     context-report, which needs both because Claude Code re-runs the
//     statusLine several times a second. Turn boundaries are sparse (a couple
//     per turn), so there is no storm to suppress; and every one of them is a
//     STATE TRANSITION. Suppressing one does not delay information, it deletes
//     it: skip a UserPromptSubmit and the session reads idle for the whole turn,
//     because Claude will not speak again until that turn ends. See the report
//     for this deviation from the written design, which listed context-report's
//     backoff among the pieces to reuse.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// activityPath is the ingestion route (spec/openapi.json, POST
// /api/self/activity — a self-op: identity rides the token, never a parameter).
const activityPath = "/api/self/activity"

// activityStdinMax bounds how much of the hook's JSON payload we read. We want
// exactly one small field out of it (session_id); a transcript-sized body must
// not be pulled into memory to find it.
const activityStdinMax = 64 << 10

// cmdReportActivity posts one turn boundary. `now` is injected so tests drive
// the sequence number deterministically. Returns the process exit code, which
// is ALWAYS 0 (rule 2 above) — the return type exists to match the other
// subcommands, not to carry failure.
func cmdReportActivity(
	client httpClient, cfg Config, args []string,
	now time.Time, in io.Reader, errw io.Writer,
) int {
	fs := flag.NewFlagSet("ocagent report-activity", flag.ContinueOnError)
	// Parse diagnostics go to stderr, NOT to the caller's stdout writer (rule 1).
	fs.SetOutput(errw)
	state := fs.String("state", "", "turn boundary: active (a turn began) or idle (it ended)")
	turnID := fs.String("turn-id", "", "the turn this report is about (optional; used to pair active↔idle)")
	sessionID := fs.String("session-id", "", "reporter session id (optional; defaults to the hook payload's session_id)")
	if err := fs.Parse(args); err != nil {
		return 0
	}
	if *state != "active" && *state != "idle" {
		fmt.Fprintf(errw, "[ocagent] report-activity: --state must be active or idle (got %q)\n", *state)
		return 0
	}
	session := *sessionID
	if session == "" {
		session = activitySessionFromHook(in)
	}
	payload := map[string]any{
		"state":   *state,
		"runtime": "claude",
		// seq orders reports WITHIN one session_id. Microseconds since the epoch
		// from the reporter's OWN clock: the server only ever compares it to the
		// previous value from the same reporter, so skew against the server does
		// not matter, and it never reaches a display. Microseconds (not nanos)
		// because float64 represents them exactly, and (not millis) so two hooks
		// firing back-to-back cannot collide on the same value.
		"seq": float64(now.UnixNano()) / 1e3,
	}
	if *turnID != "" {
		payload["turn_id"] = *turnID
	}
	if session != "" {
		payload["session_id"] = session
	}
	status, detail := httpRequest(client, http.MethodPost, cfg.Base+activityPath, cfg.Token, payload)
	if status < 200 || status >= 300 {
		// Loud in the log, silent to the model and to the exit code. A silent
		// failure here would make a column that quietly stops updating look like
		// an agent that quietly stopped working.
		fmt.Fprintf(errw, "[ocagent] report-activity: POST %s state=%s FAILED status=%d: %s\n",
			activityPath, *state, status, truncateForLog(strings.TrimSpace(detail)))
	}
	return 0
}

// activitySessionFromHook pulls `session_id` out of the Claude hook's stdin
// JSON. Entirely best-effort: no stdin, a non-JSON body, or a missing field all
// yield "" — the report is still sent, just without the ordering scope. The
// read is bounded (activityStdinMax) so a large payload cannot be slurped whole.
func activitySessionFromHook(in io.Reader) string {
	if in == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(in, activityStdinMax))
	if err != nil || len(raw) == 0 {
		return ""
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	id, _ := obj["session_id"].(string)
	return id
}
