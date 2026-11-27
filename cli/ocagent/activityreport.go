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
	"slices"
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
	Kind        string  `json:"kind,omitempty"`
	// Stopped its turn while a sub-agent it started runs on.
	WaitingOnChild bool `json:"waiting_on_child,omitempty"`
	// Its Agent call has not returned: a foreground child blocks its parent.
	Foreground bool `json:"foreground,omitempty"`
	StandingBy bool `json:"standing_by,omitempty"`
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
	// background_tasks lists a teammate under a task id of its own, never its
	// agent id, so it can only be matched there by this description.
	Teammate bool `json:"teammate,omitempty"`
	// Its agent_type from SubagentStart, the name a standing-by teammate is shown under.
	Name      string  `json:"name,omitempty"`
	SpawnedTs float64 `json:"spawned_ts,omitempty"`
}

type activityState struct {
	Busy      bool               `json:"busy"`
	BusySince *float64           `json:"busy_since"`
	Subagents []activitySubagent `json:"subagents"`
	Waits     []activityWait     `json:"waits"`
	// Sub-agents that stopped with no listed sub-agent under it, kept until the
	// session restarts: one can start again under the same id (SendMessage, or a
	// child whose parent was never known), and must then not take a pending
	// Agent call. Not pruned at the main Stop: a sub-agent waiting on its child
	// is not always listed as running in background_tasks.
	Stopped map[string]bool `json:"stopped,omitempty"`
	// The station has not accepted this snapshot; `listen` retries it.
	Unsent bool `json:"unsent,omitempty"`
	// Keyed by sub-agent id; PostToolUse's is authoritative.
	Spawns map[string]activitySpawn `json:"spawns,omitempty"`
	// Agent-tool calls whose sub-agent has not started yet, oldest first. A
	// foreground child's PostToolUse only arrives after it finished, so its
	// SubagentStart takes the oldest; concurrent spawns from different parents
	// can be paired wrongly.
	Pending []activitySpawn `json:"pending,omitempty"`
	// Background tasks of a type that is not known to only wait, shown as
	// working, and known teammates standing by; rebuilt from every
	// background_tasks list.
	Working []activitySubagent `json:"working,omitempty"`
}

type activitySubagentBody struct {
	ID          string  `json:"id"`
	ParentID    *string `json:"parent_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	StartedTs   float64 `json:"started_ts"`
	Kind        string  `json:"kind"`
	// Omitted while working, which the station reads an empty state as.
	State string `json:"state,omitempty"`
}

type activityBody struct {
	Busy      bool                   `json:"busy"`
	BusySince *float64               `json:"busy_since"`
	MainState string                 `json:"main_state,omitempty"`
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
		AgentType   string `json:"agent_type"`
		Name        string `json:"name"`
		Server      string `json:"server"`
		Tool        string `json:"tool"`
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
		teammates := map[string]bool{}
		for _, task := range in.BackgroundTasks {
			switch {
			case !isLiveTask(task.Status):
			case task.Type == "subagent":
				running[task.ID] = true
			case task.Type == "teammate":
				teammates[task.Description] = true
			}
		}
		// A foreground child of a background sub-agent or a teammate is never
		// in background_tasks; its own SubagentStop removes it.
		s.Subagents = keepLineage(s.Subagents, func(sub activitySubagent) bool {
			return running[sub.ID] || (s.isTeammate(sub.ID) && teammates[sub.Description])
		})
		// A sub-agent's Agent calls outlive the main turn (a teammate spawns
		// workers while the main agent is idle); they go at its own stop.
		s.Pending = slices.DeleteFunc(s.Pending, func(p activitySpawn) bool {
			return p.ParentID == nil || !listsSubagent(s.Subagents, *p.ParentID)
		})
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
			Status  string `json:"status"`
		}
		if json.Unmarshal(in.ToolResponse, &response) != nil || response.AgentID == "" {
			return false, false
		}
		spawn := spawnOf(in)
		spawn.Teammate = response.Status == "teammate_spawned"
		spawn.SpawnedTs = now
		return true, recordSpawn(s, response.AgentID, spawn)
	case "SubagentStart":
		// The shown time is the current run's: every start restarts it,
		// including a parent carrying on after it stopped to wait for its child
		// (it stayed listed meanwhile).
		for i := range s.Subagents {
			if s.Subagents[i].ID == in.AgentID {
				s.Subagents[i].StartedTs, s.Subagents[i].WaitingOnChild = now, false
				if s.Subagents[i].Name == "unknown" && in.AgentType != "" {
					s.Subagents[i].Name = in.AgentType
				}
				return true, true
			}
		}
		stopped := s.Stopped[in.AgentID]
		delete(s.Stopped, in.AgentID)
		s.Working = slices.DeleteFunc(s.Working, func(w activitySubagent) bool { return w.ID == in.AgentID })
		spawn, known := s.Spawns[in.AgentID]
		fromPending := !known && !stopped && len(s.Pending) > 0
		if fromPending {
			spawn, s.Pending = s.Pending[0], s.Pending[1:]
		}
		if known || fromPending {
			spawn.Name = in.AgentType
			setSpawn(s, in.AgentID, spawn)
		}
		s.Subagents = append(s.Subagents, activitySubagent{ID: in.AgentID, ParentID: spawn.ParentID,
			Name: in.AgentType, Description: spawn.Description, StartedTs: now,
			Kind: agentKind(spawn), Foreground: fromPending})
	case "SubagentStop":
		// Claude Code (seen in 2.1.308) also fires SubagentStop when a sub-agent
		// ends its turn to wait for its own child, and SubagentStart when it
		// resumes: a stop does not mean finished while its child is listed.
		kept := keepWithAncestors(s.Subagents, func(sub activitySubagent) bool { return sub.ID != in.AgentID })
		for _, sub := range s.Subagents {
			if sub.ID == in.AgentID && !listsSubagent(kept, sub.ID) {
				if s.Stopped == nil {
					s.Stopped = map[string]bool{}
				}
				s.Stopped[sub.ID] = true
			}
		}
		for i := range kept {
			if kept[i].ID == in.AgentID {
				kept[i].WaitingOnChild = true
			}
		}
		if !listsSubagent(kept, in.AgentID) {
			s.Pending = slices.DeleteFunc(s.Pending, func(p activitySpawn) bool {
				return p.ParentID != nil && *p.ParentID == in.AgentID
			})
		}
		s.Subagents = kept
		refreshActivityFromTasks(s, in, now)
	default:
		return false, false
	}
	return true, true
}

// Keeps the sub-agents keep accepts, every listed ancestor of one, and every
// listed descendant of one.
func keepLineage(subs []activitySubagent, keep func(activitySubagent) bool) []activitySubagent {
	kept := keepWithAncestors(subs, keep)
	for grew := true; grew; {
		grew = false
		for _, sub := range subs {
			if sub.ParentID != nil && listsSubagent(kept, *sub.ParentID) && !listsSubagent(kept, sub.ID) {
				kept, grew = append(kept, sub), true
			}
		}
	}
	return kept
}

// Keeps the sub-agents keep accepts plus every listed ancestor of one, so no
// kept sub-agent is left without its parent.
func keepWithAncestors(subs []activitySubagent, keep func(activitySubagent) bool) []activitySubagent {
	parents := make(map[string]*string, len(subs))
	for _, sub := range subs {
		parents[sub.ID] = sub.ParentID
	}
	keepIDs := map[string]bool{}
	for _, sub := range subs {
		if !keep(sub) {
			continue
		}
		for id := sub.ID; !keepIDs[id]; {
			keepIDs[id] = true
			parent := parents[id]
			if parent == nil {
				break
			}
			if _, listed := parents[*parent]; !listed {
				break
			}
			id = *parent
		}
	}
	kept := make([]activitySubagent, 0, len(subs))
	for _, sub := range subs {
		if keepIDs[sub.ID] {
			kept = append(kept, sub)
		}
	}
	return kept
}

func listsSubagent(subs []activitySubagent, id string) bool {
	for _, sub := range subs {
		if sub.ID == id {
			return true
		}
	}
	return false
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
	before, known := s.Spawns[id]
	stopped := s.Stopped[id]
	spawn.Name = before.Name
	setSpawn(s, id, spawn)
	for i := range s.Subagents {
		if s.Subagents[i].ID == id {
			s.Subagents[i].ParentID, s.Subagents[i].Description = spawn.ParentID, spawn.Description
			s.Subagents[i].Kind, s.Subagents[i].Foreground = agentKind(spawn), false
			return true
		}
	}
	if !known && !stopped {
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

func (s *activityState) isTeammate(id string) bool { return s.Spawns[id].Teammate }

// Claude Code only lists tasks in one of these two states today.
func isLiveTask(status string) bool { return status == "running" || status == "pending" }

// Claude Code task types that run a program without the model. Any other type,
// including one a later Claude Code adds, is shown as working so that it is
// never missed (owner c-1dcafca28f9d).
var waitingTaskTypes = map[string]bool{"shell": true, "monitor": true, "MCP task": true, "code": true}

func refreshActivityFromTasks(s *activityState, in activityHookInput, now float64) {
	s.Waits = nil
	working := []activitySubagent{}
	for _, task := range in.BackgroundTasks {
		if !isLiveTask(task.Status) {
			continue
		}
		if task.Type == "subagent" {
			if !s.updateSubagentDescription(task.ID, task.Description) && !s.Stopped[task.ID] {
				s.Subagents = append(s.Subagents, activitySubagent{ID: task.ID,
					Name: orUnknown(task.AgentType), Description: task.Description, StartedTs: now, Kind: "subagent"})
			}
			continue
		}
		if task.Type == "teammate" && s.knowsTeammate(task.Description) {
			continue
		}
		description := task.Description
		if description == "" {
			description = task.Name
		}
		if description == "" && task.Server != "" {
			description = task.Server + "·" + task.Tool
		}
		if !waitingTaskTypes[task.Type] {
			working = append(working, activitySubagent{ID: task.ID, Name: orUnknown(task.Type),
				Description: description, StartedTs: s.workingSince(task.ID, now), Kind: orUnknown(task.Type)})
			continue
		}
		s.Waits = append(s.Waits, activityWait{Kind: task.Type, Description: description})
	}
	s.Working = append(working, s.standingBy(in, now)...)
	for _, cron := range in.SessionCrons {
		wait := activityWait{Kind: "schedule", Description: cron.Prompt, Recurring: cron.Recurring}
		if next, ok := cronNextRun(cron.Schedule, time.Unix(0, int64(now*1e9)).In(cronLocation)); ok {
			ts := float64(next.Unix())
			wait.NextTs = &ts
		}
		s.Waits = append(s.Waits, wait)
	}
}

// A known teammate not listed as a sub-agent while its task is live has
// finished its work and waits to be messaged again. Teammates are matched to
// their tasks by description only, so per description the newest spawns fill
// the live tasks the working ones leave, and the older ones are gone and
// forgotten. A hook without a background_tasks list forgets nothing.
func (s *activityState) standingBy(in activityHookInput, now float64) []activitySubagent {
	live := map[string]int{}
	for _, task := range in.BackgroundTasks {
		if task.Type == "teammate" && isLiveTask(task.Status) {
			live[task.Description]++
		}
	}
	idle := []string{}
	for id, spawn := range s.Spawns {
		// Never seen to start: its task may not be listed yet.
		if !spawn.Teammate || spawn.Name == "" {
			continue
		}
		if listsSubagent(s.Subagents, id) {
			live[spawn.Description]--
			continue
		}
		idle = append(idle, id)
	}
	sort.Slice(idle, func(i, j int) bool {
		a, b := s.Spawns[idle[i]], s.Spawns[idle[j]]
		if a.SpawnedTs != b.SpawnedTs {
			return a.SpawnedTs > b.SpawnedTs
		}
		return idle[i] < idle[j]
	})
	standing := []activitySubagent{}
	for _, id := range idle {
		spawn := s.Spawns[id]
		if live[spawn.Description] <= 0 {
			if in.BackgroundTasks != nil {
				delete(s.Spawns, id)
			}
			continue
		}
		live[spawn.Description]--
		standing = append(standing, activitySubagent{ID: id, ParentID: spawn.ParentID, Name: orUnknown(spawn.Name),
			Description: spawn.Description, StartedTs: s.workingSince(id, now), Kind: "teammate", StandingBy: true})
	}
	sort.Slice(standing, func(i, j int) bool { return standing[i].ID < standing[j].ID })
	return standing
}

func (s *activityState) workingSince(id string, now float64) float64 {
	for _, task := range s.Working {
		if task.ID == id {
			return task.StartedTs
		}
	}
	return now
}

func agentKind(spawn activitySpawn) string {
	if spawn.Teammate {
		return "teammate"
	}
	return "subagent"
}

const (
	stateWaitingForSubagents = "waiting_for_subagents"
	stateStandingBy          = "standing_by"
)

func subagentState(subs []activitySubagent, sub activitySubagent) string {
	switch {
	case sub.StandingBy:
		return stateStandingBy
	case waitsForSubagents(subs, sub):
		return stateWaitingForSubagents
	}
	return ""
}

func waitsForSubagents(subs []activitySubagent, sub activitySubagent) bool {
	if sub.WaitingOnChild {
		return true
	}
	for _, child := range subs {
		if child.Foreground && child.ParentID != nil && *child.ParentID == sub.ID {
			return true
		}
	}
	return false
}

func mainWaitsForSubagents(subs []activitySubagent) bool {
	for _, child := range subs {
		if child.Foreground && child.ParentID == nil {
			return true
		}
	}
	return false
}

func orUnknown(name string) string {
	if name == "" {
		return "unknown"
	}
	return name
}

func (s *activityState) updateSubagentDescription(id, description string) bool {
	for i := range s.Subagents {
		if s.Subagents[i].ID == id {
			s.Subagents[i].Description = description
			return true
		}
	}
	return false
}

// A known teammate is either working, so listed as a sub-agent, or standing by.
func (s *activityState) knowsTeammate(description string) bool {
	for _, spawn := range s.Spawns {
		if spawn.Teammate && spawn.Description == description {
			return true
		}
	}
	return false
}

func newActivityBody(s activityState) activityBody {
	body := activityBody{
		Busy:      s.Busy,
		Subagents: []activitySubagentBody{},
		Waits:     []activityWait{},
	}
	if s.Busy {
		body.BusySince = s.BusySince
		if mainWaitsForSubagents(s.Subagents) {
			body.MainState = stateWaitingForSubagents
		}
	}
	subs := append(append([]activitySubagent(nil), s.Subagents...), s.Working...)
	sort.SliceStable(subs, func(i, j int) bool { return subs[i].StartedTs < subs[j].StartedTs })
	for _, sub := range subs {
		if len(body.Subagents) == activityListMax {
			break
		}
		kind := sub.Kind
		if kind == "" {
			kind = "subagent"
		}
		body.Subagents = append(body.Subagents, activitySubagentBody{
			ID:          truncateRunes(sub.ID, activityNameMax),
			ParentID:    truncateParent(sub.ParentID),
			Name:        truncateRunes(sub.Name, activityNameMax),
			Description: truncateRunes(sub.Description, activityDescriptionMax),
			StartedTs:   sub.StartedTs,
			Kind:        truncateRunes(kind, activityNameMax),
			State:       subagentState(subs, sub),
		})
	}
	for _, wait := range s.Waits {
		if len(body.Waits) == activityListMax {
			break
		}
		body.Waits = append(body.Waits, activityWait{
			Kind: truncateRunes(wait.Kind, activityNameMax), Description: truncateRunes(wait.Description, activityDescriptionMax),
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
