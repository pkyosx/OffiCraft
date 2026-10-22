package main

// Claude Code runs `ocagent activity-report` as the member's SessionStart,
// UserPromptSubmit, Stop, StopFailure, SubagentStart and SubagentStop hook
// (cli/ocwarden/spawn.go). Each hook folds its event into one local state file
// and posts the whole snapshot; the station keeps only the latest one, in memory.
// Pressing Esc fires no hook, so an interrupted turn stays busy until the next
// turn ends (owner accepted).
//
// OC_BASE CLASSIFICATION: SIGNAL ONLY — stderr line, never a refusal: a hook's
// exit code is not read, and stdout from SessionStart / UserPromptSubmit would
// be injected into the model's context.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const activityHookInputCap = 1 << 20

const activityPath = "/api/agent/activity"

// The hooks are synchronous: a hung station holds up the member's turn.
const activityReportTimeout = 2 * time.Second

const activityFlushInterval = 30 * time.Second

const (
	activityNameMax        = 80
	activityDescriptionMax = 200
	activityListMax        = 50
)

type activitySubagent struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	StartedTs   float64 `json:"started_ts"`
}

type activityWait struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

type activityState struct {
	Busy      bool               `json:"busy"`
	BusySince *float64           `json:"busy_since"`
	Subagents []activitySubagent `json:"subagents"`
	Waits     []activityWait     `json:"waits"`
	// A sub-agent that ends a turn to wait for its own child stops and later
	// starts again under the same id; this keeps its original start time until
	// the session restarts. Not pruned at the main Stop: a sub-agent waiting on
	// its child is not always listed as running in background_tasks.
	Paused map[string]float64 `json:"paused,omitempty"`
	// The station has not accepted this snapshot; `listen` retries it.
	Unsent bool `json:"unsent,omitempty"`
}

type activitySubagentBody struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	StartedTs   float64 `json:"started_ts"`
}

type activityBody struct {
	Busy      bool                   `json:"busy"`
	BusySince *float64               `json:"busy_since"`
	Subagents []activitySubagentBody `json:"subagents"`
	Waits     []activityWait         `json:"waits"`
}

type activityHookInput struct {
	HookEventName   string `json:"hook_event_name"`
	Source          string `json:"source"`
	AgentID         string `json:"agent_id"`
	AgentType       string `json:"agent_type"`
	BackgroundTasks []struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		Status      string `json:"status"`
		Description string `json:"description"`
	} `json:"background_tasks"`
	SessionCrons []struct {
		Prompt string `json:"prompt"`
	} `json:"session_crons"`
}

func cmdActivityReport(client httpClient, cfg Config, now float64, stdin io.Reader, errOut io.Writer) int {
	raw, _ := io.ReadAll(io.LimitReader(stdin, activityHookInputCap))
	var in activityHookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		fmt.Fprintf(errOut, "[ocagent] activity-report: hook input is not JSON: %v\n", err)
		return 0
	}
	withActivityLock(cfg, func() {
		state, _ := loadActivityState(activityStatePath(cfg))
		if !applyActivityHook(&state, in, now) {
			return
		}
		_ = warnMissingBase(cfg, "activity-report", errOut)
		if cfg.Token == "" || cfg.MemberID == "" {
			writeActivityState(activityStatePath(cfg), state)
			return
		}
		// Every hook of a turn waits on this lock, so an unreachable station
		// costs one timeout per backoff window, not one per queued hook. A
		// skipped snapshot is marked unsent for the listener's flush: Claude has
		// no heartbeat, so a skipped Stop would otherwise leave the member busy
		// on the station until its next prompt.
		if reportBackedOff(readReportBackoff(activityBackoffPath(cfg)), now) {
			state.Unsent = true
			writeActivityState(activityStatePath(cfg), state)
			return
		}
		sendActivity(client, cfg, state, now, errOut)
	})
	return 0
}

func newActivityClient() *http.Client {
	return &http.Client{Timeout: activityReportTimeout}
}

// The station's activity is in memory only, so a listener that has just
// (re)connected — possibly to a restarted station — sends the last snapshot
// again, whatever the backoff says. The caller skips it for a Codex listener:
// the sidecar reports for itself, and a file left from when the member ran on
// Claude would be stale.
func reannounceActivity(client httpClient, cfg Config, now float64) {
	withActivityLock(cfg, func() {
		if state, ok := loadActivityState(activityStatePath(cfg)); ok {
			sendActivity(client, cfg, state, now, io.Discard)
		}
	})
}

// Run by the listener on a timer: retries a snapshot a hook could not send.
func flushActivity(client httpClient, cfg Config, now float64) {
	withActivityLock(cfg, func() {
		state, ok := loadActivityState(activityStatePath(cfg))
		if !ok || !state.Unsent || reportBackedOff(readReportBackoff(activityBackoffPath(cfg)), now) {
			return
		}
		sendActivity(client, cfg, state, now, io.Discard)
	})
}

// Callers hold the activity lock.
func sendActivity(client httpClient, cfg Config, state activityState, now float64, errOut io.Writer) {
	backoffPath := activityBackoffPath(cfg)
	backoff := readReportBackoff(backoffPath)
	if postActivity(client, cfg, state, errOut) {
		clearReportBackoff(backoffPath)
		state.Unsent = false
	} else {
		writeReportBackoff(backoffPath, reportBackoffState{failures: backoff.failures + 1, lastAttempt: now})
		state.Unsent = true
	}
	writeActivityState(activityStatePath(cfg), state)
}

// A missing or unreadable file is a session with nothing running.
func loadActivityState(path string) (activityState, bool) {
	var state activityState
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &state) != nil {
		return activityState{}, false
	}
	return state, true
}

// Reports false for an event that changes nothing.
func applyActivityHook(s *activityState, in activityHookInput, now float64) bool {
	switch in.HookEventName {
	case "SessionStart":
		// Auto-compaction fires SessionStart in the middle of a turn.
		if in.Source != "compact" {
			*s = activityState{}
		}
	case "UserPromptSubmit":
		s.Busy, s.BusySince = true, &now
	case "Stop", "StopFailure":
		s.Busy, s.BusySince = false, nil
		running := map[string]bool{}
		for _, task := range in.BackgroundTasks {
			if task.Type == "subagent" && task.Status == "running" {
				running[task.ID] = true
			}
		}
		kept := s.Subagents[:0]
		for _, sub := range s.Subagents {
			if running[sub.ID] {
				kept = append(kept, sub)
			}
		}
		s.Subagents = kept
		refreshActivityFromTasks(s, in)
	case "SubagentStart":
		for _, sub := range s.Subagents {
			if sub.ID == in.AgentID {
				return true
			}
		}
		started, paused := s.Paused[in.AgentID]
		if !paused {
			started = now
		}
		delete(s.Paused, in.AgentID)
		s.Subagents = append(s.Subagents, activitySubagent{ID: in.AgentID, Name: in.AgentType, StartedTs: started})
	case "SubagentStop":
		kept := s.Subagents[:0]
		for _, sub := range s.Subagents {
			if sub.ID != in.AgentID {
				kept = append(kept, sub)
				continue
			}
			if s.Paused == nil {
				s.Paused = map[string]float64{}
			}
			s.Paused[sub.ID] = sub.StartedTs
		}
		s.Subagents = kept
		refreshActivityFromTasks(s, in)
	default:
		return false
	}
	return true
}

// Monitor tasks are listed as type "shell" too.
func refreshActivityFromTasks(s *activityState, in activityHookInput) {
	s.Waits = nil
	for _, task := range in.BackgroundTasks {
		switch {
		case task.Type == "subagent":
			for i := range s.Subagents {
				if s.Subagents[i].ID == task.ID {
					s.Subagents[i].Description = task.Description
				}
			}
		case task.Type == "shell" && task.Status == "running":
			s.Waits = append(s.Waits, activityWait{Kind: "background_command", Description: task.Description})
		}
	}
	for _, cron := range in.SessionCrons {
		s.Waits = append(s.Waits, activityWait{Kind: "schedule", Description: cron.Prompt})
	}
}

func newActivityBody(s activityState) activityBody {
	body := activityBody{
		Busy:      s.Busy,
		Subagents: []activitySubagentBody{},
		Waits:     []activityWait{},
	}
	if s.Busy {
		body.BusySince = s.BusySince
	}
	subs := append([]activitySubagent(nil), s.Subagents...)
	sort.SliceStable(subs, func(i, j int) bool { return subs[i].StartedTs < subs[j].StartedTs })
	for _, sub := range subs {
		if len(body.Subagents) == activityListMax {
			break
		}
		body.Subagents = append(body.Subagents, activitySubagentBody{
			Name:        truncateRunes(sub.Name, activityNameMax),
			Description: truncateRunes(sub.Description, activityDescriptionMax),
			StartedTs:   sub.StartedTs,
		})
	}
	for _, wait := range s.Waits {
		if len(body.Waits) == activityListMax {
			break
		}
		body.Waits = append(body.Waits, activityWait{
			Kind: wait.Kind, Description: truncateRunes(wait.Description, activityDescriptionMax),
		})
	}
	return body
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func postActivity(client httpClient, cfg Config, s activityState, errOut io.Writer) bool {
	status, detail := httpRequest(client, http.MethodPost, cfg.Base+activityPath, cfg.Token, newActivityBody(s))
	if status >= 200 && status < 300 {
		return true
	}
	fmt.Fprintf(errOut, "[ocagent] activity-report: POST %s FAILED status=%d: %s\n",
		activityPath, status, truncateForLog(strings.TrimSpace(detail)))
	return false
}

func activityBackoffPath(cfg Config) string {
	return filepath.Join(filepath.Dir(reportStampPath(cfg)), "activity.backoff")
}

func activityStatePath(cfg Config) string {
	return filepath.Join(filepath.Dir(reportStampPath(cfg)), "activity.json")
}

// Held across read, write AND post: hooks of one session run as separate
// processes, and without it two of them can apply, or reach the station, out
// of order.
func withActivityLock(cfg Config, fn func()) {
	path := filepath.Join(filepath.Dir(reportStampPath(cfg)), "activity.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		if lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644); err == nil {
			defer lock.Close()
			_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
		}
	}
	fn()
}

func writeActivityState(path string, s activityState) {
	if s.Subagents == nil {
		s.Subagents = []activitySubagent{}
	}
	if s.Waits == nil {
		s.Waits = []activityWait{}
	}
	raw, _ := json.Marshal(s)
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		_ = os.WriteFile(path, append(raw, '\n'), 0o644)
	}
}
