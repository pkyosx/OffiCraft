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
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1001,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1001,"kind":"subagent"},` +
				`{"id":"b2","parent_id":null,"name":"general-purpose","description":"","started_ts":1002,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"b2","parent_id":null,"name":"general-purpose","description":"read cli/","started_ts":1002,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1005,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1006,"kind":"subagent"}],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		if got, want := readFileString(t, statePath(cfg)),
			`{"busy":false,"busy_since":null,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1006,"kind":"subagent"}],`+
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
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002,"kind":"subagent","state":"waiting_for_subagents"},{"id":"b2","parent_id":"a1","name":"general-purpose","description":"read cli/","started_ts":1005,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002,"kind":"subagent"},{"id":"b2","parent_id":"a1","name":"general-purpose","description":"read cli/ (renamed)","started_ts":1005,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002,"kind":"subagent","state":"waiting_for_subagents"},{"id":"b2","parent_id":"a1","name":"general-purpose","description":"read cli/ (renamed)","started_ts":1005,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1002,"kind":"subagent","state":"waiting_for_subagents"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1009,"kind":"subagent"}],"waits":[]}`),
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
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002,"kind":"subagent","state":"waiting_for_subagents"},{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1004,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002,"kind":"subagent","state":"waiting_for_subagents"},{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1004,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002,"kind":"subagent","state":"waiting_for_subagents"},{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1004,"kind":"subagent","state":"waiting_for_subagents"},{"id":"C","parent_id":"B","name":"general-purpose","description":"C task","started_ts":1007,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002,"kind":"subagent","state":"waiting_for_subagents"},{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1004,"kind":"subagent","state":"waiting_for_subagents"},{"id":"C","parent_id":"B","name":"general-purpose","description":"C task","started_ts":1007,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002,"kind":"subagent","state":"waiting_for_subagents"},{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1004,"kind":"subagent","state":"waiting_for_subagents"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002,"kind":"subagent","state":"waiting_for_subagents"},{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1010,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002,"kind":"subagent","state":"waiting_for_subagents"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1012,"kind":"subagent"}],"waits":[]}`),
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
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1001,"kind":"subagent"},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1002,"kind":"subagent"},` +
				`{"id":"C","parent_id":"B","name":"general-purpose","description":"C task","started_ts":1003,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1001,"kind":"subagent"},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1002,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1012,"subagents":[{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1001,"kind":"subagent"},` +
				`{"id":"B","parent_id":"A","name":"general-purpose","description":"B task","started_ts":1002,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
	})

	t.Run("a working teammate and the foreground children under it stay listed across the main Stop, the teammate reads as waiting while a foreground child runs, a standing-by teammate is listed as standing by until it starts again, and a teammate gone from the list goes with its children", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		tasks := func(extra ...string) string {
			list := append([]string{`{"id":"bzbqsru8v","type":"shell","status":"running","description":"sleep 400","command":"sleep 400"}`,
				`{"id":"thgpmlm3x","type":"teammate","status":"running","description":"implement phase 1"}`,
				`{"id":"t0","type":"teammate","status":"running","description":"spawned before this session"}`}, extra...)
			return `"background_tasks":[` + strings.Join(list, ",") + `],"session_crons":[]`
		}

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, agentPreToolUse("", "implement phase 1")},
			step{1002, activityHook("SubagentStart", `"agent_id":"aimpl-937342692ba02f94","agent_type":"impl"`)},
			step{1003, agentPostToolUse("", "implement phase 1", `{"status":"teammate_spawned","agentId":"aimpl-937342692ba02f94","name":"impl","team_name":"session-43b63d3c"}`)},
			step{1004, agentPreToolUse("aimpl-937342692ba02f94", "write the tests")},
			step{1005, activityHook("SubagentStart", `"agent_id":"w1","agent_type":"gf-builder:unit-tester"`)},
			step{1006, agentPreToolUse("aimpl-937342692ba02f94", "write the code")},
			step{1007, activityHook("SubagentStart", `"agent_id":"w2","agent_type":"gf-builder:implementer"`)},
			step{1010, activityHook("Stop", tasks())},
			step{1020, activityHook("SubagentStop", `"agent_id":"w1","agent_type":"gf-builder:unit-tester",`+tasks())},
			step{1021, activityHook("SubagentStop", `"agent_id":"w2","agent_type":"gf-builder:implementer",`+tasks())},
			step{1022, activityHook("SubagentStop", `"agent_id":"aimpl-937342692ba02f94","agent_type":"impl",`+tasks())},
			step{1030, activityHook("UserPromptSubmit", `"prompt":"next"`)},
			step{1031, activityHook("Stop", tasks())},
			step{1040, activityHook("SubagentStart", `"agent_id":"aimpl-937342692ba02f94","agent_type":"impl"`)},
			step{1041, activityHook("Stop", tasks())},
			step{1042, agentPreToolUse("aimpl-937342692ba02f94", "rerun the tests")},
			step{1043, activityHook("SubagentStart", `"agent_id":"w3","agent_type":"gf-builder:unit-tester"`)},
			step{1050, activityHook("Stop", `"background_tasks":[{"id":"bzbqsru8v","type":"shell","status":"running","description":"sleep 400"},`+
				`{"id":"t0","type":"teammate","status":"running","description":"spawned before this session"}],"session_crons":[]`)})

		impl := func(started, kind string, waiting bool) string {
			tail := ""
			if waiting {
				tail = `,"state":"waiting_for_subagents"`
			}
			return `{"id":"aimpl-937342692ba02f94","parent_id":null,"name":"impl","description":"implement phase 1","started_ts":` +
				started + `,"kind":"` + kind + `"` + tail + `}`
		}
		w1 := `{"id":"w1","parent_id":"aimpl-937342692ba02f94","name":"gf-builder:unit-tester","description":"write the tests","started_ts":1005,"kind":"subagent"}`
		w2 := `{"id":"w2","parent_id":"aimpl-937342692ba02f94","name":"gf-builder:implementer","description":"write the code","started_ts":1007,"kind":"subagent"}`
		shell := `{"kind":"shell","description":"sleep 400","next_ts":null,"recurring":false}`
		t0 := `{"id":"t0","parent_id":null,"name":"teammate","description":"spawned before this session","started_ts":1010,"kind":"teammate"}`
		standby := `{"id":"aimpl-937342692ba02f94","parent_id":null,"name":"impl","description":"implement phase 1","started_ts":1022,"kind":"teammate","state":"standing_by"}`
		want := []capturedPost{
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[` + impl("1002", "subagent", false) + `],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[` + impl("1002", "teammate", false) + `],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[` + impl("1002", "teammate", true) + `,` + w1 + `],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[` + impl("1002", "teammate", true) + `,` + w1 + `,` + w2 + `],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + impl("1002", "teammate", true) + `,` + w1 + `,` + w2 + `,` + t0 + `],"waits":[` + shell + `]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + impl("1002", "teammate", true) + `,` + w2 + `,` + t0 + `],"waits":[` + shell + `]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + impl("1002", "teammate", false) + `,` + t0 + `],"waits":[` + shell + `]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + t0 + `,` + standby + `],"waits":[` + shell + `]}`),
			sent(`{"busy":true,"busy_since":1030,"subagents":[` + t0 + `,` + standby + `],"waits":[` + shell + `]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + t0 + `,` + standby + `],"waits":[` + shell + `]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + t0 + `,` + impl("1040", "teammate", false) + `],"waits":[` + shell + `]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + t0 + `,` + impl("1040", "teammate", false) + `],"waits":[` + shell + `]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + t0 + `,` + impl("1040", "teammate", true) + `,` +
				`{"id":"w3","parent_id":"aimpl-937342692ba02f94","name":"gf-builder:unit-tester","description":"rerun the tests","started_ts":1043,"kind":"subagent"}],"waits":[` + shell + `]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + t0 + `],"waits":[` + shell + `]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
	})

	t.Run("a worker a teammate asks for across the main Stop still goes under the teammate, a new teammate with a gone teammate's description does not bring the gone one back, and a sub-agent first seen in the list takes its name when it starts", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		review := `"background_tasks":[{"id":"t1","type":"teammate","status":"running","description":"review"}],"session_crons":[]`

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, agentPreToolUse("", "review")},
			step{1002, activityHook("SubagentStart", `"agent_id":"old","agent_type":"reviewer"`)},
			step{1003, agentPostToolUse("", "review", `{"status":"teammate_spawned","agentId":"old"}`)},
			step{1004, activityHook("SubagentStop", `"agent_id":"old","agent_type":"reviewer",`+review)},
			step{1005, activityHook("Stop", `"background_tasks":[],"session_crons":[]`)},
			step{1010, activityHook("UserPromptSubmit", `"prompt":"again"`)},
			step{1011, agentPreToolUse("", "review")},
			step{1012, activityHook("SubagentStart", `"agent_id":"new","agent_type":"reviewer"`)},
			step{1013, agentPostToolUse("", "review", `{"status":"teammate_spawned","agentId":"new"}`)},
			step{1014, agentPreToolUse("new", "write tests")},
			step{1015, activityHook("Stop", review)},
			step{1016, activityHook("SubagentStart", `"agent_id":"w","agent_type":"gf-builder:unit-tester"`)},
			step{1020, activityHook("Stop", `"background_tasks":[{"id":"t1","type":"teammate","status":"running","description":"review"},`+
				`{"id":"x","type":"subagent","status":"running","description":"found in the list"}],"session_crons":[]`)},
			step{1021, activityHook("SubagentStart", `"agent_id":"x","agent_type":"Explore"`)})

		newRow := `{"id":"new","parent_id":null,"name":"reviewer","description":"review","started_ts":1012,"kind":"teammate","state":"waiting_for_subagents"}`
		w := `{"id":"w","parent_id":"new","name":"gf-builder:unit-tester","description":"write tests","started_ts":1016,"kind":"subagent"}`
		got := *posts
		want := []capturedPost{
			sent(`{"busy":false,"busy_since":null,"subagents":[` + newRow + `,` + w + `],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + newRow + `,` + w +
				`,{"id":"x","parent_id":null,"name":"unknown","description":"found in the list","started_ts":1020,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + newRow + `,` + w +
				`,{"id":"x","parent_id":null,"name":"Explore","description":"found in the list","started_ts":1021,"kind":"subagent"}],"waits":[]}`),
		}
		if len(got) < len(want) || !reflect.DeepEqual(got[len(got)-len(want):], want) {
			t.Errorf("sent\n  %+v\nwant the last to be\n  %+v", got, want)
		}
	})

	t.Run("two live teammates with one description keep both: the standing-by one stays listed while the new one works, is not forgotten by a hook without a background_tasks list, and is a teammate again when it starts", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		two := `"background_tasks":[{"id":"t1","type":"teammate","status":"running","description":"review"},` +
			`{"id":"t2","type":"teammate","status":"running","description":"review"}],"session_crons":[]`

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, agentPreToolUse("", "review")},
			step{1002, activityHook("SubagentStart", `"agent_id":"a","agent_type":"reviewer"`)},
			step{1003, agentPostToolUse("", "review", `{"status":"teammate_spawned","agentId":"a"}`)},
			step{1004, activityHook("SubagentStop", `"agent_id":"a","agent_type":"reviewer","background_tasks":[{"id":"t1","type":"teammate","status":"running","description":"review"}],"session_crons":[]`)},
			step{1004, activityHook("SubagentStop", `"agent_id":"z","agent_type":"Explore"`)},
			step{1005, agentPreToolUse("", "review")},
			step{1006, activityHook("SubagentStart", `"agent_id":"b","agent_type":"reviewer"`)},
			step{1007, agentPostToolUse("", "review", `{"status":"teammate_spawned","agentId":"b"}`)},
			step{1008, activityHook("Stop", two)},
			step{1010, activityHook("SubagentStart", `"agent_id":"a","agent_type":"reviewer"`)},
			step{1011, activityHook("Stop", two)})

		a := func(started, tail string) string {
			return `{"id":"a","parent_id":null,"name":"reviewer","description":"review","started_ts":` + started + `,"kind":"teammate"` + tail + `}`
		}
		b := `{"id":"b","parent_id":null,"name":"reviewer","description":"review","started_ts":1006,"kind":"teammate"}`
		got := *posts
		want := []capturedPost{
			sent(`{"busy":false,"busy_since":null,"subagents":[` + b + `,` + a("1008", `,"state":"standing_by"`) + `],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + b + `,` + a("1010", "") + `],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + b + `,` + a("1010", "") + `],"waits":[]}`),
		}
		if len(got) < len(want) || !reflect.DeepEqual(got[len(got)-len(want):], want) {
			t.Errorf("sent\n  %+v\nwant the last to be\n  %+v", got, want)
		}
	})

	t.Run("with one live teammate task for a description, a working teammate with that description takes it and an older standing-by one is forgotten", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		one := `"background_tasks":[{"id":"t1","type":"teammate","status":"running","description":"review"}],"session_crons":[]`

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, agentPreToolUse("", "review")},
			step{1002, activityHook("SubagentStart", `"agent_id":"a","agent_type":"reviewer"`)},
			step{1003, agentPostToolUse("", "review", `{"status":"teammate_spawned","agentId":"a"}`)},
			step{1004, activityHook("SubagentStop", `"agent_id":"a","agent_type":"reviewer",`+one)},
			step{1005, agentPreToolUse("", "review")},
			step{1006, activityHook("SubagentStart", `"agent_id":"b","agent_type":"reviewer"`)},
			step{1007, agentPostToolUse("", "review", `{"status":"teammate_spawned","agentId":"b"}`)},
			step{1008, activityHook("Stop", one)},
			step{1009, activityHook("SubagentStop", `"agent_id":"b","agent_type":"reviewer",`+one)},
			step{1010, activityHook("SubagentStart", `"agent_id":"a","agent_type":"reviewer"`)})

		got := *posts
		want := []capturedPost{
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"b","parent_id":null,"name":"reviewer","description":"review","started_ts":1006,"kind":"teammate"}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"b","parent_id":null,"name":"reviewer","description":"review","started_ts":1009,"kind":"teammate","state":"standing_by"}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"b","parent_id":null,"name":"reviewer","description":"review","started_ts":1009,"kind":"teammate","state":"standing_by"},` +
				`{"id":"a","parent_id":null,"name":"reviewer","description":"","started_ts":1010,"kind":"subagent"}],"waits":[]}`),
		}
		if len(got) < len(want) || !reflect.DeepEqual(got[len(got)-len(want):], want) {
			t.Errorf("sent\n  %+v\nwant the last to be\n  %+v", got, want)
		}
	})

	t.Run("with one live teammate task for a description and two standing-by teammates with it, the newer one is listed and the older one forgotten", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		one := `"background_tasks":[{"id":"t1","type":"teammate","status":"running","description":"review"}],"session_crons":[]`

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, agentPreToolUse("", "review")},
			step{1002, activityHook("SubagentStart", `"agent_id":"a","agent_type":"reviewer"`)},
			step{1003, agentPostToolUse("", "review", `{"status":"teammate_spawned","agentId":"a"}`)},
			step{1004, agentPreToolUse("", "review")},
			step{1005, activityHook("SubagentStart", `"agent_id":"b","agent_type":"reviewer"`)},
			step{1006, agentPostToolUse("", "review", `{"status":"teammate_spawned","agentId":"b"}`)},
			step{1007, activityHook("SubagentStop", `"agent_id":"b","agent_type":"reviewer","background_tasks":[{"id":"t1","type":"teammate","status":"running","description":"review"},`+
				`{"id":"t2","type":"teammate","status":"running","description":"review"}],"session_crons":[]`)},
			step{1008, activityHook("SubagentStop", `"agent_id":"a","agent_type":"reviewer",`+one)})

		got := *posts
		want := sent(`{"busy":true,"busy_since":1000,"subagents":[{"id":"b","parent_id":null,"name":"reviewer","description":"review","started_ts":1007,"kind":"teammate","state":"standing_by"}],"waits":[]}`)
		if len(got) == 0 || !reflect.DeepEqual(got[len(got)-1], want) {
			t.Errorf("sent\n  %+v\nwant the last to be\n  %+v", got, want)
		}
	})

	t.Run("a teammate whose PostToolUse comes before its start and before its task is listed is still a teammate when it starts", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, agentPreToolUse("", "review")},
			step{1002, agentPostToolUse("", "review", `{"status":"teammate_spawned","agentId":"a"}`)},
			step{1003, activityHook("Stop", `"background_tasks":[],"session_crons":[]`)},
			step{1004, activityHook("SubagentStart", `"agent_id":"a","agent_type":"reviewer"`)})

		got := *posts
		want := sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"a","parent_id":null,"name":"reviewer","description":"review","started_ts":1004,"kind":"teammate"}],"waits":[]}`)
		if len(got) == 0 || !reflect.DeepEqual(got[len(got)-1], want) {
			t.Errorf("sent\n  %+v\nwant the last to be\n  %+v", got, want)
		}
	})

	t.Run("a sub-agent that stops to wait on its child keeps the Agent call it just made, so the next child is paired with it", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		running := `"background_tasks":[{"id":"A","type":"subagent","status":"running","description":"A task"}],"session_crons":[]`

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, agentPreToolUse("", "A task")},
			step{1002, activityHook("SubagentStart", `"agent_id":"A","agent_type":"general-purpose"`)},
			step{1003, agentPostToolUse("", "A task", `{"status":"async_launched","agentId":"A"}`)},
			step{1004, agentPreToolUse("A", "C task")},
			step{1005, activityHook("SubagentStart", `"agent_id":"C","agent_type":"general-purpose"`)},
			step{1006, agentPostToolUse("A", "C task", `{"status":"async_launched","agentId":"C"}`)},
			step{1007, agentPreToolUse("A", "B task")},
			step{1008, activityHook("SubagentStop", `"agent_id":"A","agent_type":"general-purpose",`+running)},
			step{1009, activityHook("SubagentStart", `"agent_id":"B","agent_type":"Explore"`)})

		got := *posts
		want := sent(`{"busy":true,"busy_since":1000,"subagents":[` +
			`{"id":"A","parent_id":null,"name":"general-purpose","description":"A task","started_ts":1002,"kind":"subagent","state":"waiting_for_subagents"},` +
			`{"id":"C","parent_id":"A","name":"general-purpose","description":"C task","started_ts":1005,"kind":"subagent"},` +
			`{"id":"B","parent_id":"A","name":"Explore","description":"B task","started_ts":1009,"kind":"subagent"}],"waits":[]}`)
		if len(got) == 0 || !reflect.DeepEqual(got[len(got)-1], want) {
			t.Errorf("sent\n  %+v\nwant the last to be\n  %+v", got, want)
		}
	})

	t.Run("an Agent call a teammate made that never started is dropped when the teammate stops, so the main agent's next sub-agent is not paired with it", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		review := `"background_tasks":[{"id":"t1","type":"teammate","status":"running","description":"review"}],"session_crons":[]`

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, agentPreToolUse("", "review")},
			step{1002, activityHook("SubagentStart", `"agent_id":"tm","agent_type":"reviewer"`)},
			step{1003, agentPostToolUse("", "review", `{"status":"teammate_spawned","agentId":"tm"}`)},
			step{1004, agentPreToolUse("tm", "denied call")},
			step{1005, activityHook("Stop", review)},
			step{1006, activityHook("SubagentStop", `"agent_id":"tm","agent_type":"reviewer",`+review)},
			step{1010, activityHook("UserPromptSubmit", `"prompt":"next"`)},
			step{1011, agentPreToolUse("", "main task")},
			step{1012, activityHook("SubagentStart", `"agent_id":"m","agent_type":"Explore"`)})

		got := *posts
		want := sent(`{"busy":true,"busy_since":1010,"main_state":"waiting_for_subagents","subagents":[` +
			`{"id":"tm","parent_id":null,"name":"reviewer","description":"review","started_ts":1006,"kind":"teammate","state":"standing_by"},` +
			`{"id":"m","parent_id":null,"name":"Explore","description":"main task","started_ts":1012,"kind":"subagent"}],"waits":[]}`)
		if len(got) == 0 || !reflect.DeepEqual(got[len(got)-1], want) {
			t.Errorf("sent\n  %+v\nwant the last to be\n  %+v", got, want)
		}
	})

	t.Run("a foreground sub-agent takes the oldest pending Agent call, the main agent reads as waiting while it runs, and its late PostToolUse sends nothing", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)

		drive(t, cfg, srv.Client(),
			step{1000, activityHook("UserPromptSubmit", `"prompt":"hi"`)},
			step{1001, agentPreToolUse("", "first")},
			step{1002, agentPreToolUse("", "second")},
			step{1003, activityHook("SubagentStart", `"agent_id":"f1","agent_type":"Plan"`)},
			step{1004, activityHook("SubagentStop", `"agent_id":"f1","agent_type":"Plan","background_tasks":[],"session_crons":[]`)},
			step{1005, agentPostToolUse("", "first", `{"status":"completed","agentId":"f1","content":[{"type":"text","text":"done"}]}`)})

		want := []capturedPost{
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"main_state":"waiting_for_subagents","subagents":[{"id":"f1","parent_id":null,"name":"Plan","description":"first","started_ts":1003,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		if got, want := readFileString(t, statePath(cfg)), `{"busy":true,"busy_since":1000,"subagents":[],"waits":[],`+
			`"stopped":{"f1":true},"spawns":{"f1":{"parent_id":null,"description":"first","name":"Plan","spawned_ts":1005}},`+
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

		b2 := `{"id":"b2","parent_id":"a1","name":"Explore","description":"deep dive","started_ts":1004,"kind":"subagent"}`
		want := []capturedPost{
			sent(`{"busy":false,"busy_since":null,"subagents":[` + b2 + `],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[` + b2 + `,` +
				`{"id":"c3","parent_id":null,"name":"Plan","description":"other","started_ts":1005,"kind":"subagent"}],"waits":[]}`),
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
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey","started_ts":1010,"kind":"subagent"}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey","started_ts":1010,"kind":"subagent"},` +
				`{"id":"z9","parent_id":null,"name":"Plan","description":"","started_ts":1011,"kind":"subagent"}],"waits":[]}`),
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
			`"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1003,"kind":"subagent"}],"waits":[]}`); got != want {
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

	t.Run("Stop drops sub-agents no longer running, adds a running one it never saw start, lists a background task of any other type as working unless its type only waits, and lists crons as waits", func(t *testing.T) {
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
			`{"id":"typed-unseen","type":"subagent","status":"running","description":"unseen explore","agent_type":"Explore"},`+
			`{"id":"s1","type":"shell","status":"running","description":"npm test"},`+
			`{"id":"s2","type":"shell","status":"completed","description":"go vet"},`+
			`{"id":"m1","type":"monitor","status":"running","description":"watch the deploy log","command":"tail -f deploy.log"},`+
			`{"id":"m2","type":"monitor","status":"completed","description":"wait for the build","command":"make build"},`+
			`{"id":"m3","type":"monitor","status":"running","description":"watch the queue","server":"queue","tool":"subscribe"},`+
			`{"id":"m4","type":"monitor","status":"running","description":"artifact watch"},`+
			`{"id":"s3","type":"shell","status":"pending","description":"queued build"},`+
			`{"id":"t1","type":"teammate","status":"running","description":"review the PR"},`+
			`{"id":"w1","type":"workflow","status":"running","description":"release workflow","name":"release"},`+
			`{"id":"w2","type":"workflow","status":"running","description":"","name":"nightly"},`+
			`{"id":"w3","type":"workflow","status":"completed","description":"old workflow","name":"old"},`+
			`{"id":"p1","type":"MCP task","status":"running","description":"render the report","server":"docs","tool":"render"},`+
			`{"id":"p2","type":"MCP task","status":"running","description":"","server":"docs","tool":"export"},`+
			`{"id":"r1","type":"cloud session","status":"running","description":"ultrareview of #750"},`+
			`{"id":"d1","type":"dream","status":"running","description":"consolidating memory"},`+
			`{"id":"x1","type":"auto-mode scan","status":"running","description":"scanning the transcript"},`+
			`{"id":"k1","type":"code","status":"running","description":"local script"},`+
			`{"id":"n1","type":"hologram","status":"running","description":"a task type from a later Claude Code"},`+
			`{"id":"n2","type":"a task type whose name runs well past forty characters","status":"running","description":"long type"},`+
			`{"id":"e1","type":"","status":"running","description":"untyped"}],`+
			`"session_crons":[{"id":"c1","schedule":"*/5 * * * *","recurring":true,"prompt":"check CI"}]`)})

		a1 := `{"id":"a1","parent_id":null,"name":"Explore","description":"survey the repo","started_ts":1001`
		unseen := `{"id":"never-started","parent_id":null,"name":"unknown","description":"unseen","started_ts":1010,"kind":"subagent"},` +
			`{"id":"typed-unseen","parent_id":null,"name":"Explore","description":"unseen explore","started_ts":1010,"kind":"subagent"}`
		working := `{"id":"t1","parent_id":null,"name":"teammate","description":"review the PR","started_ts":1010,"kind":"teammate"},` +
			`{"id":"w1","parent_id":null,"name":"workflow","description":"release workflow","started_ts":1010,"kind":"workflow"},` +
			`{"id":"w2","parent_id":null,"name":"workflow","description":"nightly","started_ts":1010,"kind":"workflow"},` +
			`{"id":"r1","parent_id":null,"name":"cloud session","description":"ultrareview of #750","started_ts":1010,"kind":"cloud session"},` +
			`{"id":"d1","parent_id":null,"name":"dream","description":"consolidating memory","started_ts":1010,"kind":"dream"},` +
			`{"id":"x1","parent_id":null,"name":"auto-mode scan","description":"scanning the transcript","started_ts":1010,"kind":"auto-mode scan"},` +
			`{"id":"n1","parent_id":null,"name":"hologram","description":"a task type from a later Claude Code","started_ts":1010,"kind":"hologram"},` +
			`{"id":"n2","parent_id":null,"name":"a task type whose name runs well past forty characters","description":"long type","started_ts":1010,"kind":"a task type whose name runs well past forty characters"},` +
			`{"id":"e1","parent_id":null,"name":"unknown","description":"untyped","started_ts":1010,"kind":"unknown"}`
		waits := `"waits":[{"kind":"shell","description":"npm test","next_ts":null,"recurring":false},` +
			`{"kind":"monitor","description":"watch the deploy log","next_ts":null,"recurring":false},` +
			`{"kind":"monitor","description":"watch the queue","next_ts":null,"recurring":false},` +
			`{"kind":"monitor","description":"artifact watch","next_ts":null,"recurring":false},` +
			`{"kind":"shell","description":"queued build","next_ts":null,"recurring":false},` +
			`{"kind":"MCP task","description":"render the report","next_ts":null,"recurring":false},` +
			`{"kind":"MCP task","description":"docs·export","next_ts":null,"recurring":false},` +
			`{"kind":"code","description":"local script","next_ts":null,"recurring":false},` +
			`{"kind":"schedule","description":"check CI","next_ts":1200,"recurring":true}]`
		want := []capturedPost{sent(`{"busy":false,"busy_since":null,"subagents":[` + a1 + `,"kind":"subagent"},` + unseen + `,` + working + `],` + waits + `}`)}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		if got, want := readFileString(t, statePath(cfg)), `{"busy":false,"busy_since":null,"subagents":[`+a1+`},`+unseen+`],`+
			waits+`,"stopped":{"a1-child":true,"gone":true},"working":[`+working+`]}`+"\n"; got != want {
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
			`"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1010,"kind":"subagent"}],"waits":[],"unsent":true}`+"\n"; got != want {
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
			`"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"","started_ts":1010,"kind":"subagent"}],"waits":[]}`)}
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
				ID: strings.Repeat("i", 81), ParentID: ptrString(strings.Repeat("p", 81)), Name: strings.Repeat("名", 81), Description: strings.Repeat("述", 201), StartedTs: float64(100 - i), Kind: strings.Repeat("種", 81),
			})
			state.Waits = append(state.Waits, activityWait{Kind: strings.Repeat("k", 81), Description: strings.Repeat("w", 201), NextTs: ptrFloat(7), Recurring: true})
		}

		got := newActivityBody(state)

		var wantSubs []activitySubagentBody
		for i := 0; i < 50; i++ {
			wantSubs = append(wantSubs, activitySubagentBody{
				ID: strings.Repeat("i", 80), ParentID: ptrString(strings.Repeat("p", 80)), Name: strings.Repeat("名", 80), Description: strings.Repeat("述", 200), StartedTs: float64(49 + i), Kind: strings.Repeat("種", 80),
			})
		}
		var wantWaits []activityWait
		for i := 0; i < 50; i++ {
			wantWaits = append(wantWaits, activityWait{Kind: strings.Repeat("k", 80), Description: strings.Repeat("w", 200), NextTs: ptrFloat(7), Recurring: true})
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
			Waits:     []activityWait{{Kind: "shell", Description: "npm test"}},
		})

		reannounceActivity(srv.Client(), cfg, 2000)

		want := []capturedPost{{path: "/api/agent/activity", auth: "Bearer t", body: `{"busy":false,"busy_since":null,` +
			`"subagents":[{"id":"a1","parent_id":null,"name":"Explore","description":"survey","started_ts":1001,"kind":"subagent"}],` +
			`"waits":[{"kind":"shell","description":"npm test","next_ts":null,"recurring":false}]}`}}
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
