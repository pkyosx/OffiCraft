package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func activityHook(event string, fields string) string {
	body := `{"session_id":"11111111-1111-4111-8111-111111111111","transcript_path":"/nowhere.jsonl",` +
		`"cwd":"/Users/example","hook_event_name":"` + event + `"`
	if fields != "" {
		body += "," + fields
	}
	return body + "}"
}

func TestCmdActivityReport(t *testing.T) {
	stubCronLocation(t)
	newCfg := func(t *testing.T, base string) Config {
		return Config{BaseConfigured: true, Base: base, Token: "t", MemberID: "Kyle", AgentsRoot: t.TempDir()}
	}
	statePath := func(cfg Config) string { return filepath.Join(cfg.AgentsRoot, "kyle", "activity.json") }
	sent := func(body string) capturedPost {
		return capturedPost{path: "/api/agent/activity", auth: "Bearer t", body: body}
	}
	type step struct {
		now   float64
		input string
	}
	drive := func(t *testing.T, cfg Config, client httpClient, steps ...step) string {
		t.Helper()
		var errOut bytes.Buffer
		for _, s := range steps {
			if rc := cmdActivityReport(client, cfg, s.now, strings.NewReader(s.input), &errOut); rc != 0 {
				t.Errorf("rc = %d, want 0", rc)
			}
		}
		return errOut.String()
	}

	t.Run("a prompt and then Stop leave the session idle", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		errOut := drive(t, cfg, srv.Client(),
			step{1000.5, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1010, activityHook("Stop", `"background_tasks":[],"session_crons":[]`)})

		want := []capturedPost{
			sent(`{"busy":true,"busy_since":1000.5,"subagents":[],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		if got, want := readFileString(t, statePath(cfg)),
			`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`+"\n"; got != want {
			t.Errorf("state file = %q, want %q", got, want)
		}
		if errOut != "" {
			t.Errorf("stderr = %q, want empty", errOut)
		}
	})

	t.Run("SessionStart clears whatever an earlier session left behind", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		writeActivityState(statePath(cfg), activityState{
			Busy: true, BusySince: ptrFloat(900),
			Subagents: []activitySubagent{{ID: "a1", Name: "Explore", StartedTs: 901}},
			Waits:     []activityWait{{Kind: "schedule", Description: "check CI"}},
			Stopped:   map[string]bool{"b2": true},
			Spawns:    map[string]activitySpawn{"a1": {Description: "survey"}},
			Pending:   []activitySpawn{{Description: "never started"}},
		})

		drive(t, cfg, srv.Client(), step{1000, activityHook("SessionStart", `"source":"startup"`)})

		want := []capturedPost{sent(`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`)}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		if got, want := readFileString(t, statePath(cfg)),
			`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`+"\n"; got != want {
			t.Errorf("state file = %q, want %q", got, want)
		}
	})

	t.Run("SessionStart from auto-compaction mid-turn keeps the turn busy", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1050, activityHook("SessionStart", `"source":"compact"`)})

		want := []capturedPost{
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
	})

	t.Run("a sub-agent with no listed child under it is dropped when it stops, and its time restarts at every start, even across the main Stop", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		tasks := `"background_tasks":[` +
			`{"id":"a1","type":"subagent","status":"running","description":"survey the repo"},` +
			`{"id":"b2","type":"subagent","status":"running","description":"read cli/"}],"session_crons":[]`

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, activityHook("SubagentStart", `"agent_id":"a1","agent_type":"Explore"`)},
			step{1002, activityHook("SubagentStart", `"agent_id":"b2","agent_type":"general-purpose"`)},
			step{1003, activityHook("SubagentStop", `"agent_id":"a1","agent_type":"Explore",`+tasks)},
			step{1004, activityHook("SubagentStop", `"agent_id":"b2","agent_type":"general-purpose",`+tasks)},
			step{1004.5, activityHook("Stop", `"background_tasks":[],"session_crons":[]`)},
			step{1005, activityHook("SubagentStart", `"agent_id":"a1","agent_type":"Explore"`)},
			step{1006, activityHook("SubagentStart", `"agent_id":"a1","agent_type":"Explore"`)})

		want := []capturedPost{
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1001}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1001},` +
				`{"id":"b2","parent_id":null,"name":"general-purpose","description":"","started_ts":1002}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"b2","parent_id":null,"name":"general-purpose","description":"read cli/","started_ts":1002}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1005}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1006}],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		if got, want := readFileString(t, statePath(cfg)),
			`{"busy":false,"busy_since":null,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1006}],`+
				`"waits":[],"stopped":{"b2":true}}`+"\n"; got != want {
			t.Errorf("state file = %q, want %q", got, want)
		}
	})

	t.Run("a background sub-agent gets its parent and description from the Agent call that spawned it, and that parent stays listed while it stops to wait for it, its time restarting when it carries on", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, agentPreToolUse("", "survey the repo")},
			step{1002, activityHook("SubagentStart", `"agent_id":"a1","agent_type":"Explore"`)},
			step{1003, agentPostToolUse("", "survey the repo", `{"isAsync":true,"status":"async_launched","agentId":"a1"}`)},
			step{1004, agentPreToolUse("a1", "read cli/")},
			step{1005, activityHook("SubagentStart", `"agent_id":"b2","agent_type":"general-purpose"`)},
			step{1006, agentPostToolUse("a1", "read cli/ (renamed)", `{"isAsync":true,"status":"async_launched","agentId":"b2"}`)},
			step{1007, activityHook("SubagentStop", `"agent_id":"a1","agent_type":"Explore","background_tasks":[`+
				`{"id":"a1","type":"subagent","status":"running","description":"survey the repo"},`+
				`{"id":"b2","type":"subagent","status":"running","description":"read cli/ (renamed)"}],"session_crons":[]`)},
			step{1008, activityHook("SubagentStop", `"agent_id":"b2","agent_type":"general-purpose","background_tasks":[`+
				`{"id":"b2","type":"subagent","status":"running","description":"read cli/ (renamed)"}],"session_crons":[]`)},
			step{1009, activityHook("SubagentStart", `"agent_id":"a1","agent_type":"Explore"`)},
			step{1010, activityHook("SubagentStop", `"agent_id":"a1","agent_type":"Explore","background_tasks":[`+
				`{"id":"a1","type":"subagent","status":"running","description":"survey the repo"}],"session_crons":[]`)})

		want := []capturedPost{
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002},` +
				`{"id":"b2","parent_id":"a1","name":"general-purpose","description":"read cli/","started_ts":1005}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002},` +
				`{"id":"b2","parent_id":"a1","name":"general-purpose","description":"read cli/ (renamed)","started_ts":1005}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002},` +
				`{"id":"b2","parent_id":"a1","name":"general-purpose","description":"read cli/ (renamed)","started_ts":1005}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1009}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
	})

	t.Run("each sub-agent of a three-level chain stays listed while it waits for its child, restarts its time when it carries on, and goes at its own last stop", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		running := func(ids ...string) string {
			tasks := make([]string, len(ids))
			for i, id := range ids {
				tasks[i] = `{"id":"` + id + `","type":"subagent","status":"running","description":"` + id + ` task"}`
			}
			return `"background_tasks":[` + strings.Join(tasks, ",") + `],"session_crons":[]`
		}

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, agentPreToolUse("", "A task")},
			step{1002, activityHook("SubagentStart", `"agent_id":"A","agent_type":"general-purpose"`)},
			step{1003, agentPreToolUse("A", "B task")},
			step{1004, activityHook("SubagentStart", `"agent_id":"B","agent_type":"general-purpose"`)},
			step{1005, activityHook("SubagentStop", `"agent_id":"A","agent_type":"general-purpose",`+running("A", "B"))},
			step{1006, agentPreToolUse("B", "C task")},
			step{1007, activityHook("SubagentStart", `"agent_id":"C","agent_type":"general-purpose"`)},
			step{1008, activityHook("SubagentStop", `"agent_id":"B","agent_type":"general-purpose",`+running("A", "B", "C"))},
			step{1009, activityHook("SubagentStop", `"agent_id":"C","agent_type":"general-purpose",`+running("A", "B", "C"))},
			step{1010, activityHook("SubagentStart", `"agent_id":"B","agent_type":"general-purpose"`)},
			step{1011, activityHook("SubagentStop", `"agent_id":"B","agent_type":"general-purpose",`+running("A", "B"))},
			step{1012, activityHook("SubagentStart", `"agent_id":"A","agent_type":"general-purpose"`)},
			step{1013, activityHook("SubagentStop", `"agent_id":"A","agent_type":"general-purpose",`+running("A"))})

		want := []capturedPost{
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1004}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1004}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1004},` +
				`{"id":"C","parent_id":"B","name":"general-purpose","description":"C task","started_ts":1007}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1004},` +
				`{"id":"C","parent_id":"B","name":"general-purpose","description":"C task","started_ts":1007}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1004}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1010}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1012}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
	})

	t.Run("the main Stop keeps every ancestor of a running sub-agent, and the next one drops a waiting parent whose last stop never came", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		writeActivityState(statePath(cfg), activityState{
			Busy: true, BusySince: ptrFloat(1000),
			Subagents: []activitySubagent{
				{ID: "A", Name: "general-purpose", Description: "A task", StartedTs: 1001},
				{ID: "B", ParentID: ptrString("A"), Name: "general-purpose", Description: "B task", StartedTs: 1002},
				{ID: "C", ParentID: ptrString("B"), Name: "general-purpose", Description: "C task", StartedTs: 1003},
				{ID: "D", Name: "Plan", Description: "D task", StartedTs: 1004},
				{ID: "E", ParentID: ptrString("D"), Name: "Plan", Description: "E task", StartedTs: 1005},
			},
		})

		drive(t, cfg, srv.Client(),
			step{1010, activityHook("Stop", `"background_tasks":[`+
				`{"id":"C","type":"subagent","status":"running","description":"C task"},`+
				`{"id":"E","type":"subagent","status":"completed","description":"E task"}],"session_crons":[]`)},
			step{1011, activityHook("SubagentStop", `"agent_id":"C","agent_type":"general-purpose","background_tasks":[],"session_crons":[]`)},
			step{1012, activityHook("UserPromptSubmit", `"prompt":"next"`)},
			step{1013, activityHook("Stop", `"background_tasks":[],"session_crons":[]`)})

		want := []capturedPost{
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1001},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1002},` +
				`{"id":"C","parent_id":"B","name":"general-purpose","description":"C task","started_ts":1003}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1001},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1002}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1012,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1001},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1002}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
	})

	t.Run("a foreground sub-agent takes the oldest pending Agent call, and its late PostToolUse sends nothing", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		drive(t, cfg, srv.Client(),
			step{1001, agentPreToolUse("", "first")},
			step{1002, agentPreToolUse("", "second")},
			step{1003, activityHook("SubagentStart", `"agent_id":"f1","agent_type":"Plan"`)},
			step{1004, activityHook("SubagentStop", `"agent_id":"f1","agent_type":"Plan","background_tasks":[],"session_crons":[]`)},
			step{1005, agentPostToolUse("", "first", `{"status":"completed","agentId":"f1","content":[{"type":"text","text":"done"}]}`)})

		want := []capturedPost{
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"f1","parent_id":null,"name":"Plan","description":"first","started_ts":1003}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		if got, want := readFileString(t, statePath(cfg)), `{"busy":false,"busy_since":null,"subagents":[],"waits":[],`+
			`"stopped":{"f1":true},"spawns":{"f1":{"parent_id":null,"description":"first"}},`+
			`"pending":[{"parent_id":null,"description":"second"}]}`+"\n"; got != want {
			t.Errorf("state file = %q, want %q", got, want)
		}
	})

	t.Run("a PostToolUse ahead of its SubagentStart is merged into it and uses up its own pending call", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		drive(t, cfg, srv.Client(),
			step{1001, agentPreToolUse("a1", "deep dive")},
			step{1002, agentPreToolUse("", "other")},
			step{1003, agentPostToolUse("a1", "deep dive", `{"isAsync":true,"status":"async_launched","agentId":"b2"}`)},
			step{1004, activityHook("SubagentStart", `"agent_id":"b2","agent_type":"Explore"`)},
			step{1005, activityHook("SubagentStart", `"agent_id":"c3","agent_type":"Plan"`)})

		b2 := `{"id":"b2","parent_id":"a1","name":"Explore","description":"deep dive","started_ts":1004}`
		want := []capturedPost{
			sent(`{"busy":false,"busy_since":null,"subagents":[` + b2 + `],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + b2 + `,` +
				`{"id":"c3","parent_id":null,"name":"Plan","description":"other","started_ts":1005}],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
	})

	t.Run("a sub-agent starting again after it stopped keeps its spawn, leaves the pending calls alone and counts its time from the new start", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		writeActivityState(statePath(cfg), activityState{
			Stopped: map[string]bool{"a1": true, "z9": true},
			Spawns:  map[string]activitySpawn{"a1": {Description: "survey"}},
			Pending: []activitySpawn{{ParentID: ptrString("a1"), Description: "child"}},
		})

		drive(t, cfg, srv.Client(),
			step{1010, activityHook("SubagentStart", `"agent_id":"a1","agent_type":"Explore"`)},
			step{1011, activityHook("SubagentStart", `"agent_id":"z9","agent_type":"Plan"`)})

		want := []capturedPost{
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey","started_ts":1010}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey","started_ts":1010},` +
				`{"id":"z9","parent_id":null,"name":"Plan","description":"","started_ts":1011}],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		var state activityState
		_ = json.Unmarshal([]byte(readFileString(t, statePath(cfg))), &state)
		if want := []activitySpawn{{ParentID: ptrString("a1"), Description: "child"}}; !reflect.DeepEqual(state.Pending, want) {
			t.Errorf("pending = %+v, want %+v", state.Pending, want)
		}
	})

	t.Run("the main Stop drops Agent calls whose sub-agent never started", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		drive(t, cfg, srv.Client(),
			step{1001, agentPreToolUse("", "refused call")},
			step{1002, activityHook("Stop", `"background_tasks":[],"session_crons":[]`)},
			step{1003, activityHook("SubagentStart", `"agent_id":"a1","agent_type":"Explore"`)})

		if got, want := (*posts)[1], sent(`{"busy":false,"busy_since":null,`+
			`"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1003}],"waits":[]}`); got != want {
			t.Errorf("sent %+v, want %+v", got, want)
		}
	})

	t.Run("another tool's PreToolUse or PostToolUse changes nothing", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("PreToolUse", `"tool_name":"Bash","tool_input":{"command":"ls","description":"list"}`)},
			step{1001, activityHook("PostToolUse", `"tool_name":"Bash","tool_input":{"command":"ls"},"tool_response":{"agentId":"x"}`)})

		if len(*posts) != 0 {
			t.Errorf("sent %+v, want nothing", *posts)
		}
		if _, err := os.Stat(statePath(cfg)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("state file stat err = %v, want it absent", err)
		}
	})

	t.Run("Stop drops sub-agents no longer running, never adds one, and lists running shells and crons as waits", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		writeActivityState(statePath(cfg), activityState{
			Busy: true, BusySince: ptrFloat(1000),
			Subagents: []activitySubagent{
				{ID: "a1", Name: "Explore", StartedTs: 1001},
				{ID: "stale", Name: "Plan", Description: "plan it", StartedTs: 1002},
				{ID: "done", Name: "general-purpose", StartedTs: 1003},
			},
			Stopped: map[string]bool{"a1-child": true, "gone": true},
		})

		drive(t, cfg, srv.Client(), step{1010, activityHook("Stop", `"background_tasks":[`+
			`{"id":"a1","type":"subagent","status":"running","description":"survey the repo"},`+
			`{"id":"a1-child","type":"subagent","status":"running","description":"nested"},`+
			`{"id":"done","type":"subagent","status":"completed","description":"finished"},`+
			`{"id":"never-started","type":"subagent","status":"running","description":"unseen"},`+
			`{"id":"s1","type":"shell","status":"running","description":"npm test"},`+
			`{"id":"s2","type":"shell","status":"completed","description":"go vet"},`+
			`{"id":"m1","type":"shell","status":"running","description":"watch the deploy log"}],`+
			`"session_crons":[{"id":"c1","schedule":"*/5 * * * *","recurring":true,"prompt":"check CI"}]`)})

		want := []capturedPost{sent(`{"busy":false,"busy_since":null,` +
			`"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1001}],` +
			`"waits":[{"kind":"background_command","description":"npm test","next_ts":null,"recurring":false},` +
			`{"kind":"background_command","description":"watch the deploy log","next_ts":null,"recurring":false},` +
			`{"kind":"schedule","description":"check CI","next_ts":1200,"recurring":true}]}`)}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		if got, want := readFileString(t, statePath(cfg)), `{"busy":false,"busy_since":null,`+
			`"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1001}],`+
			`"waits":[{"kind":"background_command","description":"npm test","next_ts":null,"recurring":false},`+
			`{"kind":"background_command","description":"watch the deploy log","next_ts":null,"recurring":false},`+
			`{"kind":"schedule","description":"check CI","next_ts":1200,"recurring":true}],"stopped":{"a1-child":true,"gone":true}}`+"\n"; got != want {
			t.Errorf("state file = %q, want %q", got, want)
		}
	})

	t.Run("StopFailure ends the turn like Stop", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1010, activityHook("StopFailure", `"error":"rate_limit"`)})

		if got, want := (*posts)[1], sent(`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`); got != want {
			t.Errorf("sent %+v, want %+v", got, want)
		}
	})

	t.Run("an event this hook does not track writes and sends nothing", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		drive(t, cfg, srv.Client(), step{1000, activityHook("PreToolUse", `"tool_name":"Bash"`)})

		if len(*posts) != 0 {
			t.Errorf("sent %+v, want nothing", *posts)
		}
		if _, err := os.Stat(statePath(cfg)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("state file stat err = %v, want it absent", err)
		}
	})

	t.Run("without a token or member id the state is kept and nothing is sent", func(t *testing.T) {
		for name, mutate := range map[string]func(*Config){
			"no token":     func(c *Config) { c.Token = "" },
			"no member id": func(c *Config) { c.MemberID = "" },
		} {
			t.Run(name, func(t *testing.T) {
				srv, posts := contextServer(t)
				cfg := newCfg(t, srv.URL)
				mutate(&cfg)

				drive(t, cfg, srv.Client(), step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)})

				if len(*posts) != 0 {
					t.Errorf("sent %+v, want nothing", *posts)
				}
				if got, want := readFileString(t, filepath.Join(filepath.Dir(reportStampPath(cfg)), "activity.json")),
					`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`+"\n"; got != want {
					t.Errorf("state file = %q, want %q", got, want)
				}
			})
		}
	})

	t.Run("a report that does not get through is named on stderr and still exits 0", func(t *testing.T) {
		cases := []struct {
			name    string
			client  *fakeHTTP
			wantErr string
		}{
			{"refused", &fakeHTTP{status: 422, body: `{"detail":"bad"}`},
				"[ocagent] activity-report: POST /api/agent/activity FAILED status=422: {\"detail\":\"bad\"}\n"},
			{"unreachable", &fakeHTTP{err: errors.New("connection refused")},
				"[ocagent] activity-report: POST /api/agent/activity FAILED status=0: connection refused\n"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cfg := newCfg(t, "http://x")

				errOut := drive(t, cfg, tc.client, step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)})

				if errOut != tc.wantErr {
					t.Errorf("stderr = %q, want %q", errOut, tc.wantErr)
				}
				if got, want := readFileString(t, statePath(cfg)),
					`{"busy":true,"busy_since":1000,"subagents":[],"waits":[],"unsent":true}`+"\n"; got != want {
					t.Errorf("state file = %q, want %q", got, want)
				}
			})
		}
	})

	t.Run("after a failed report later hooks only update the file until the backoff window passes", func(t *testing.T) {
		cfg := newCfg(t, "http://x")
		client := &fakeHTTP{err: errors.New("connection refused")}

		errOut := drive(t, cfg, client,
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1010, activityHook("SubagentStart", `"agent_id":"a1","agent_type":"Explore"`)})
		if got, want := readFileString(t, statePath(cfg)), `{"busy":true,"busy_since":1000,`+
			`"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1010}],"waits":[],"unsent":true}`+"\n"; got != want {
			t.Errorf("state file after the skipped report = %q, want %q", got, want)
		}
		client.err, client.status, client.body = nil, 200, `{}`
		drive(t, cfg, client,
			step{1029, activityHook("SubagentStop", `"agent_id":"a1","agent_type":"Explore"`)},
			step{1030, activityHook("Stop", `"background_tasks":[],"session_crons":[]`)},
			step{1031, activityHook("UserPromptSubmit", `"prompt":"again"`)})

		want := []string{
			"POST /api/agent/activity " + `{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`,
			"POST /api/agent/activity " + `{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`,
			"POST /api/agent/activity " + `{"busy":true,"busy_since":1031,"subagents":[],"waits":[]}`,
		}
		if !reflect.DeepEqual(client.seen, want) {
			t.Errorf("sent\n  %q\nwant\n  %q", client.seen, want)
		}
		if want := "[ocagent] activity-report: POST /api/agent/activity FAILED status=0: connection refused\n"; errOut != want {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
		if _, err := os.Stat(filepath.Join(cfg.AgentsRoot, "kyle", "activity.backoff")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("backoff record stat err = %v, want it cleared by the accepted report", err)
		}
	})

	t.Run("hooks queued behind a station that accepts and never answers all finish within 4 seconds", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) })
		cfg := newCfg(t, srv.URL)
		const hooks = 6
		started := time.Now()
		done := make(chan struct{}, hooks)
		for i := 1; i <= hooks; i++ {
			go func(i int) {
				input := activityHook("SubagentStart", fmt.Sprintf(`"agent_id":"a%d","agent_type":"Explore"`, i))
				cmdActivityReport(newActivityClient(), cfg, float64(1000+i), strings.NewReader(input), io.Discard)
				done <- struct{}{}
			}(i)
		}
		deadline := time.After(4 * time.Second)
		for i := 0; i < hooks; i++ {
			select {
			case <-done:
			case <-deadline:
				t.Fatalf("%d of %d hooks still blocked after %v", hooks-i, hooks, time.Since(started))
			}
		}

		var state activityState
		if err := json.Unmarshal([]byte(readFileString(t, statePath(cfg))), &state); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, sub := range state.Subagents {
			ids = append(ids, sub.ID)
		}
		sort.Strings(ids)
		if want := []string{"a1", "a2", "a3", "a4", "a5", "a6"}; !reflect.DeepEqual(ids, want) {
			t.Errorf("the state file holds sub-agents %v, want every hook's %v", ids, want)
		}
	})

	t.Run("input that is not JSON is named on stderr, changes nothing and exits 0", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		errOut := drive(t, cfg, srv.Client(), step{1000, "not json"})

		if !strings.HasPrefix(errOut, "[ocagent] activity-report: hook input is not JSON: ") {
			t.Errorf("stderr = %q, want the not-JSON line", errOut)
		}
		if len(*posts) != 0 {
			t.Errorf("sent %+v, want nothing", *posts)
		}
	})

	t.Run("a hook waits for the one holding the session lock before it reads, writes or sends", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		held := make(chan struct{})
		release := make(chan struct{})
		holderDone := make(chan struct{})
		go func() {
			withActivityLock(cfg, func() {
				close(held)
				<-release
				writeActivityState(statePath(cfg), activityState{Busy: true, BusySince: ptrFloat(999)})
			})
			close(holderDone)
		}()
		<-held
		hookDone := make(chan struct{})
		go func() {
			drive(t, cfg, srv.Client(), step{1010, activityHook("SubagentStart", `"agent_id":"a1","agent_type":"Explore"`)})
			close(hookDone)
		}()

		select {
		case <-hookDone:
			t.Fatal("the hook finished while another process held the lock")
		case <-time.After(200 * time.Millisecond):
		}
		close(release)
		<-holderDone
		<-hookDone

		want := []capturedPost{sent(`{"busy":true,"busy_since":999,` +
			`"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1010}],"waits":[]}`)}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
	})
}

func ptrFloat(v float64) *float64 { return &v }

func ptrString(v string) *string { return &v }

// The Agent tool's hooks as Claude Code 2.1.307 sends them; the caller's
// agent_id is absent when the main agent made the call.
func agentToolHook(event, caller, description, response string) string {
	fields := `"tool_name":"Agent","tool_input":{"description":"` + description +
		`","prompt":"…","subagent_type":"Explore"}`
	if caller != "" {
		fields = `"agent_id":"` + caller + `","agent_type":"general-purpose",` + fields
	}
	if response != "" {
		fields += `,"tool_response":` + response
	}
	return activityHook(event, fields)
}

func agentPreToolUse(caller, description string) string {
	return agentToolHook("PreToolUse", caller, description, "")
}

func agentPostToolUse(caller, description, response string) string {
	return agentToolHook("PostToolUse", caller, description, response)
}

// stubCronLocation pins the zone session crons are read in for one test.
func stubCronLocation(t *testing.T) {
	t.Helper()
	saved := cronLocation
	cronLocation = time.FixedZone("UTC+5:45", 5*3600+45*60)
	t.Cleanup(func() { cronLocation = saved })
}

func TestNewActivityBody(t *testing.T) {
	t.Run("busy_since is null whenever the main agent is not busy", func(t *testing.T) {
		got := newActivityBody(activityState{Busy: false, BusySince: ptrFloat(5)})

		want := activityBody{Subagents: []activitySubagentBody{}, Waits: []activityWait{}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("body = %+v, want %+v", got, want)
		}
	})

	t.Run("sub-agents go earliest first and every text and list is cut to the spec's limits", func(t *testing.T) {
		state := activityState{Busy: true, BusySince: ptrFloat(1)}
		for i := 0; i < 52; i++ {
			state.Subagents = append(state.Subagents, activitySubagent{
				ID: strings.Repeat("i", 81), ParentID: ptrString(strings.Repeat("p", 81)), Name: strings.Repeat("名", 81), Description: strings.Repeat("述", 201), StartedTs: float64(100 - i),
			})
			state.Waits = append(state.Waits, activityWait{Kind: "schedule", Description: strings.Repeat("w", 201), NextTs: ptrFloat(7), Recurring: true})
		}

		got := newActivityBody(state)

		var wantSubs []activitySubagentBody
		for i := 0; i < 50; i++ {
			wantSubs = append(wantSubs, activitySubagentBody{
				ID: strings.Repeat("i", 80), ParentID: ptrString(strings.Repeat("p", 80)), Name: strings.Repeat("名", 80), Description: strings.Repeat("述", 200), StartedTs: float64(49 + i),
			})
		}
		var wantWaits []activityWait
		for i := 0; i < 50; i++ {
			wantWaits = append(wantWaits, activityWait{Kind: "schedule", Description: strings.Repeat("w", 200), NextTs: ptrFloat(7), Recurring: true})
		}
		want := activityBody{Busy: true, BusySince: ptrFloat(1), Subagents: wantSubs, Waits: wantWaits}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("body = %+v, want %+v", got, want)
		}
	})
}

func TestReannounceActivity(t *testing.T) {
	t.Run("the last snapshot is sent again as it stands", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", MemberID: "kyle", AgentsRoot: t.TempDir()}
		writeActivityState(activityStatePath(cfg), activityState{
			Subagents: []activitySubagent{{ID: "a1", Name: "Explore", Description: "survey", StartedTs: 1001}},
			Waits:     []activityWait{{Kind: "background_command", Description: "npm test"}},
		})

		reannounceActivity(srv.Client(), cfg, 2000)

		want := []capturedPost{{path: "/api/agent/activity", auth: "Bearer t", body: `{"busy":false,"busy_since":null,` +
			`"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey","started_ts":1001}],` +
			`"waits":[{"kind":"background_command","description":"npm test","next_ts":null,"recurring":false}]}`}}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
	})

	t.Run("an accepted re-announce clears the backoff and a refused one sets it", func(t *testing.T) {
		cfg := Config{BaseConfigured: true, Base: "http://x", Token: "t", MemberID: "kyle", AgentsRoot: t.TempDir()}
		backoffPath := filepath.Join(cfg.AgentsRoot, "kyle", "activity.backoff")
		writeActivityState(activityStatePath(cfg), activityState{Unsent: true})
		writeReportBackoff(backoffPath, reportBackoffState{failures: 2, lastAttempt: 1990})

		reannounceActivity(&fakeHTTP{status: 200, body: `{}`}, cfg, 2000)

		if _, err := os.Stat(backoffPath); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("backoff record stat err = %v, want it cleared", err)
		}
		if got, want := readFileString(t, activityStatePath(cfg)),
			`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`+"\n"; got != want {
			t.Errorf("state file = %q, want %q", got, want)
		}

		reannounceActivity(&fakeHTTP{err: errors.New("connection refused")}, cfg, 2010)

		if got, want := readFileString(t, backoffPath), "1 2010"; got != want {
			t.Errorf("backoff record = %q, want %q", got, want)
		}
		if got, want := readFileString(t, activityStatePath(cfg)),
			`{"busy":false,"busy_since":null,"subagents":[],"waits":[],"unsent":true}`+"\n"; got != want {
			t.Errorf("state file = %q, want %q", got, want)
		}
	})

	t.Run("a session that never ran a hook sends nothing", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", MemberID: "kyle", AgentsRoot: t.TempDir()}

		reannounceActivity(srv.Client(), cfg, 2000)

		if len(*posts) != 0 {
			t.Errorf("sent %+v, want nothing", *posts)
		}
	})
}

func TestFlushActivity(t *testing.T) {
	newCfg := func(t *testing.T) Config {
		return Config{BaseConfigured: true, Base: "http://x", Token: "t", MemberID: "kyle", AgentsRoot: t.TempDir()}
	}
	hook := func(cfg Config, client httpClient, now float64, input string) {
		cmdActivityReport(client, cfg, now, strings.NewReader(input), io.Discard)
	}

	t.Run("a Stop skipped during the backoff reaches the station once the window passes", func(t *testing.T) {
		cfg := newCfg(t)
		client := &fakeHTTP{err: errors.New("context deadline exceeded")}
		hook(cfg, client, 1000, activityHook("UserPromptSubmit", `"prompt":"hi"`))
		client.err, client.status, client.body = nil, 200, `{}`
		hook(cfg, client, 1020, activityHook("Stop", `"background_tasks":[],"session_crons":[]`))

		flushActivity(client, cfg, 1025)
		flushActivity(client, cfg, 1031)
		flushActivity(client, cfg, 1061)

		idle := "POST /api/agent/activity " + `{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`
		want := []string{"POST /api/agent/activity " + `{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`, idle}
		if !reflect.DeepEqual(client.seen, want) {
			t.Errorf("sent\n  %q\nwant\n  %q", client.seen, want)
		}
		if got, want := readFileString(t, activityStatePath(cfg)),
			`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`+"\n"; got != want {
			t.Errorf("state file = %q, want %q", got, want)
		}
	})

	t.Run("a hook skipped in the backoff window marks its snapshot unsent even when the state file was lost", func(t *testing.T) {
		cfg := newCfg(t)
		writeReportBackoff(filepath.Join(cfg.AgentsRoot, "kyle", "activity.backoff"), reportBackoffState{failures: 1, lastAttempt: 1000})
		client := &fakeHTTP{status: 200, body: `{}`}
		hook(cfg, client, 1010, activityHook("Stop", `"background_tasks":[],"session_crons":[]`))

		flushActivity(client, cfg, 1030)

		want := []string{"POST /api/agent/activity " + `{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`}
		if !reflect.DeepEqual(client.seen, want) {
			t.Errorf("sent %q, want %q", client.seen, want)
		}
	})

	t.Run("a snapshot the station already accepted is not sent again", func(t *testing.T) {
		cfg := newCfg(t)
		client := &fakeHTTP{status: 200, body: `{}`}
		hook(cfg, client, 1000, activityHook("UserPromptSubmit", `"prompt":"hi"`))

		flushActivity(client, cfg, 2000)

		if len(client.seen) != 1 {
			t.Errorf("sent %q, want only the hook's own report", client.seen)
		}
	})

	t.Run("a flush the station refuses extends the backoff and stays unsent", func(t *testing.T) {
		cfg := newCfg(t)
		client := &fakeHTTP{err: errors.New("connection refused")}
		hook(cfg, client, 1000, activityHook("UserPromptSubmit", `"prompt":"hi"`))

		flushActivity(client, cfg, 1030)
		flushActivity(client, cfg, 1060)

		if len(client.seen) != 2 {
			t.Errorf("sent %q, want the hook's report and one flush inside the doubled window", client.seen)
		}
		if got, want := readFileString(t, filepath.Join(cfg.AgentsRoot, "kyle", "activity.backoff")), "2 1030"; got != want {
			t.Errorf("backoff record = %q, want %q", got, want)
		}
		if got, want := readFileString(t, activityStatePath(cfg)),
			`{"busy":true,"busy_since":1000,"subagents":[],"waits":[],"unsent":true}`+"\n"; got != want {
			t.Errorf("state file = %q, want %q", got, want)
		}
	})
}
