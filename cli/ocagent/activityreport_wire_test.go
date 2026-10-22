package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"strings"
	"testing"
)

// TestActivityReportUplinkBodies drives the real hook, whose send the reconnect
// re-announce and the listener's flush share, and compares its body, nested
// sub-agent and wait items included, against the frozen schema: the server
// decodes with DisallowUnknownFields, so one undeclared key loses the report
// while the hook still exits 0.
func TestActivityReportUplinkBodies(t *testing.T) {
	declared := frozenIngestProperties(t, "AgentActivityIngestDTO")
	subagentDeclared := frozenIngestProperties(t, "AgentActivitySubagentDTO")
	waitDeclared := frozenIngestProperties(t, "AgentActivityWaitDTO")
	srv, posts := contextServer(t)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", MemberID: "kyle", AgentsRoot: t.TempDir()}

	walked := map[string]int{}
	check := func(name string, before int) capturedPost {
		if len(*posts) != before+1 {
			t.Fatalf("%s sent %v, want exactly one report", name, (*posts)[before:])
		}
		sent := (*posts)[before]
		walked[name+" → "+sent.path]++
		if bad := schemaViolations(sent.body, declared); len(bad) > 0 {
			t.Errorf("%s: body has keys the frozen schema refuses %v; body=%s", name, bad, sent.body)
		}
		var items struct {
			Subagents []json.RawMessage `json:"subagents"`
			Waits     []json.RawMessage `json:"waits"`
		}
		if err := json.Unmarshal([]byte(sent.body), &items); err != nil {
			t.Fatalf("%s: body is not JSON: %v", name, err)
		}
		for _, one := range items.Subagents {
			if bad := schemaViolations(string(one), subagentDeclared); len(bad) > 0 {
				t.Errorf("%s: sub-agent item has keys the frozen schema refuses %v; item=%s", name, bad, one)
			}
		}
		for _, one := range items.Waits {
			if bad := schemaViolations(string(one), waitDeclared); len(bad) > 0 {
				t.Errorf("%s: wait item has keys the frozen schema refuses %v; item=%s", name, bad, one)
			}
		}
		return sent
	}

	writeActivityState(activityStatePath(cfg), activityState{
		Busy: true, BusySince: ptrFloat(1000),
		Subagents: []activitySubagent{{ID: "a1", Name: "Explore", StartedTs: 1001}},
	})
	before := len(*posts)
	var errOut bytes.Buffer
	cmdActivityReport(srv.Client(), cfg, 1010, strings.NewReader(activityHook("SubagentStop",
		`"agent_id":"b2","agent_type":"Plan","background_tasks":[`+
			`{"id":"a1","type":"subagent","status":"running","description":"survey the repo"},`+
			`{"id":"s1","type":"shell","status":"running","description":"npm test"}],`+
			`"session_crons":[{"id":"c1","schedule":"0 * * * *","recurring":true,"prompt":"check CI"}]`)), &errOut)
	hook := check("hook", before)

	want := `{"busy":true,"busy_since":1000,` +
		`"subagents":[{"name":"Explore","description":"survey the repo","started_ts":1001}],` +
		`"waits":[{"kind":"background_command","description":"npm test"},{"kind":"schedule","description":"check CI"}]}`
	if hook.body != want {
		t.Errorf("hook body =\n  %s\nwant\n  %s", hook.body, want)
	}

	committed := manifestUplinkPaths(t, "cli/ocagent/activityreport_wire_test.go")
	if !maps.Equal(walked, committed) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but the producers posted %v",
			committed, walked)
	}
}
