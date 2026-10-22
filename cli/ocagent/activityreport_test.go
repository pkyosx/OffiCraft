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
			Paused:    map[string]float64{"b2": 902},
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

	t.Run("a nested sub-agent that stops to wait for its child keeps its start time when it resumes, even across the main Stop", func(t *testing.T) {
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
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"name":"Explore","description":"","started_ts":1001}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"name":"Explore","description":"","started_ts":1001},` +
				`{"name":"general-purpose","description":"","started_ts":1002}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[{"name":"general-purpose","description":"read cli/","started_ts":1002}],"waits":[]}`),
			sent(`{"busy":true,"busy_since":1000,"subagents":[],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"name":"Explore","description":"","started_ts":1001}],"waits":[]}`),
			sent(`{"busy":false,"busy_since":null,"subagents":[{"name":"Explore","description":"","started_ts":1001}],"waits":[]}`),
		}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		if got, want := readFileString(t, statePath(cfg)),
			`{"busy":false,"busy_since":null,"subagents":[{"id":"a1","name":"Explore","description":"","started_ts":1001}],`+
				`"waits":[],"paused":{"b2":1002}}`+"\n"; got != want {
			t.Errorf("state file = %q, want %q", got, want)
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
			Paused: map[string]float64{"a1-child": 1004, "gone": 1005},
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
			`"subagents":[{"name":"Explore","description":"survey the repo","started_ts":1001}],` +
			`"waits":[{"kind":"background_command","description":"npm test"},` +
			`{"kind":"background_command","description":"watch the deploy log"},` +
			`{"kind":"schedule","description":"check CI"}]}`)}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		if got, want := readFileString(t, statePath(cfg)), `{"busy":false,"busy_since":null,`+
			`"subagents":[{"id":"a1","name":"Explore","description":"survey the repo","started_ts":1001}],`+
			`"waits":[{"kind":"background_command","description":"npm test"},`+
			`{"kind":"background_command","description":"watch the deploy log"},`+
			`{"kind":"schedule","description":"check CI"}],"paused":{"a1-child":1004,"gone":1005}}`+"\n"; got != want {
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
			`"subagents":[{"id":"a1","name":"Explore","description":"","started_ts":1010}],"waits":[],"unsent":true}`+"\n"; got != want {
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
			`"subagents":[{"name":"Explore","description":"","started_ts":1010}],"waits":[]}`)}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
	})
}

func ptrFloat(v float64) *float64 { return &v }

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
				ID: "s", Name: strings.Repeat("名", 81), Description: strings.Repeat("述", 201), StartedTs: float64(100 - i),
			})
			state.Waits = append(state.Waits, activityWait{Kind: "schedule", Description: strings.Repeat("w", 201)})
		}

		got := newActivityBody(state)

		var wantSubs []activitySubagentBody
		for i := 0; i < 50; i++ {
			wantSubs = append(wantSubs, activitySubagentBody{
				Name: strings.Repeat("名", 80), Description: strings.Repeat("述", 200), StartedTs: float64(49 + i),
			})
		}
		var wantWaits []activityWait
		for i := 0; i < 50; i++ {
			wantWaits = append(wantWaits, activityWait{Kind: "schedule", Description: strings.Repeat("w", 200)})
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
			`"subagents":[{"name":"Explore","description":"survey","started_ts":1001}],` +
			`"waits":[{"kind":"background_command","description":"npm test"}]}`}}
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
