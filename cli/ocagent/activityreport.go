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

// Claude Code schedules session crons in the machine's local time.
var cronLocation = time.Local

const (
	activityNameMax        = 80
	activityDescriptionMax = 200
	activityListMax        = 50
)

type activitySubagent struct {
	ID          string  `json:"id"`
	ParentID    *string `json:"parent_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	StartedTs   float64 `json:"started_ts"`
}

type activityWait struct {
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	NextTs      *float64 `json:"next_ts"`
	Recurring   bool     `json:"recurring"`
}

// Who spawned a sub-agent and the description it was given: the Agent tool's
// caller and tool_input, which SubagentStart does not carry.
type activitySpawn struct {
	ParentID    *string `json:"parent_id"`
	Description string  `json:"description"`
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
	// Keyed by sub-agent id; PostToolUse's is authoritative.
	Spawns map[string]activitySpawn `json:"spawns,omitempty"`
	// Agent-tool calls whose sub-agent has not started yet, oldest first. A
	// foreground child's PostToolUse only arrives after it finished, so its
	// SubagentStart takes the oldest; concurrent spawns from different parents
	// can be paired wrongly.
	Pending []activitySpawn `json:"pending,omitempty"`
}

type activitySubagentBody struct {
	ID          string  `json:"id"`
	ParentID    *string `json:"parent_id"`
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
	HookEventName string `json:"hook_event_name"`
	Source        string `json:"source"`
	AgentID       string `json:"agent_id"`
	AgentType     string `json:"agent_type"`
	ToolName      string `json:"tool_name"`
	// Raw: a failed tool call's response need not be an object.
	ToolInput       json.RawMessage `json:"tool_input"`
	ToolResponse    json.RawMessage `json:"tool_response"`
	BackgroundTasks []struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		Status      string `json:"status"`
		Description string `json:"description"`
	} `json:"background_tasks"`
	SessionCrons []struct {
		Schedule  string `json:"schedule"`
		Recurring bool   `json:"recurring"`
		Prompt    string `json:"prompt"`
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
		changed, report := applyActivityHook(&state, in, now)
		if !changed {
			return
		}
		if !report {
			writeActivityState(activityStatePath(cfg), state)
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

// changed is false for an event that changes nothing; report is false for one
// that changes only bookkeeping the station does not see.
func applyActivityHook(s *activityState, in activityHookInput, now float64) (changed, report bool) {
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
		s.Pending = nil
		refreshActivityFromTasks(s, in, now)
	case "PreToolUse":
		if !isAgentTool(in.ToolName) {
			return false, false
		}
		s.Pending = append(s.Pending, spawnOf(in))
		return true, false
	case "PostToolUse":
		if !isAgentTool(in.ToolName) {
			return false, false
		}
		var response struct {
			AgentID string `json:"agentId"`
		}
		if json.Unmarshal(in.ToolResponse, &response) != nil || response.AgentID == "" {
			return false, false
		}
		return true, recordSpawn(s, response.AgentID, spawnOf(in))
	case "SubagentStart":
		for _, sub := range s.Subagents {
			if sub.ID == in.AgentID {
				return true, true
			}
		}
		started, paused := s.Paused[in.AgentID]
		if !paused {
			started = now
		}
		delete(s.Paused, in.AgentID)
		spawn, known := s.Spawns[in.AgentID]
		if !known && !paused && len(s.Pending) > 0 {
			spawn, s.Pending = s.Pending[0], s.Pending[1:]
			setSpawn(s, in.AgentID, spawn)
		}
		s.Subagents = append(s.Subagents, activitySubagent{ID: in.AgentID, ParentID: spawn.ParentID,
			Name: in.AgentType, Description: spawn.Description, StartedTs: started})
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
		refreshActivityFromTasks(s, in, now)
	default:
		return false, false
	}
	return true, true
}

func isAgentTool(name string) bool { return name == "Agent" || name == "Task" }

// The caller is the hook's agent_id; absent when the main agent called.
func spawnOf(in activityHookInput) activitySpawn {
	var input struct {
		Description string `json:"description"`
	}
	_ = json.Unmarshal(in.ToolInput, &input)
	spawn := activitySpawn{Description: input.Description}
	if in.AgentID != "" {
		caller := in.AgentID
		spawn.ParentID = &caller
	}
	return spawn
}

func setSpawn(s *activityState, id string, spawn activitySpawn) {
	if s.Spawns == nil {
		s.Spawns = map[string]activitySpawn{}
	}
	s.Spawns[id] = spawn
}

// Reports whether a running sub-agent changed. Before its SubagentStart the
// mapping waits in Spawns, and the PreToolUse entry for the same call is
// dropped so a later SubagentStart cannot take it.
func recordSpawn(s *activityState, id string, spawn activitySpawn) bool {
	_, known := s.Spawns[id]
	_, paused := s.Paused[id]
	setSpawn(s, id, spawn)
	for i := range s.Subagents {
		if s.Subagents[i].ID == id {
			s.Subagents[i].ParentID, s.Subagents[i].Description = spawn.ParentID, spawn.Description
			return true
		}
	}
	if !known && !paused {
		for i, pending := range s.Pending {
			if pending.Description == spawn.Description && sameParent(pending.ParentID, spawn.ParentID) {
				s.Pending = append(s.Pending[:i], s.Pending[i+1:]...)
				break
			}
		}
	}
	return false
}

func sameParent(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// Monitor tasks are listed as type "shell" too.
func refreshActivityFromTasks(s *activityState, in activityHookInput, now float64) {
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
		wait := activityWait{Kind: "schedule", Description: cron.Prompt, Recurring: cron.Recurring}
		if next, ok := cronNextRun(cron.Schedule, time.Unix(0, int64(now*1e9)).In(cronLocation)); ok {
			ts := float64(next.Unix())
			wait.NextTs = &ts
		}
		s.Waits = append(s.Waits, wait)
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
			ID:          truncateRunes(sub.ID, activityNameMax),
			ParentID:    truncateParent(sub.ParentID),
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
			NextTs: wait.NextTs, Recurring: wait.Recurring,
		})
	}
	return body
}

func truncateParent(id *string) *string {
	if id == nil {
		return nil
	}
	cut := truncateRunes(*id, activityNameMax)
	return &cut
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
