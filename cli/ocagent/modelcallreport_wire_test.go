package main

import (
	"bytes"
	"maps"
	"strings"
	"testing"
)

// TestModelCallReportUplinkBodies drives the real Stop and StopFailure hooks and
// compares every body they put on the wire against the frozen schema, nested
// model_call included: the server decodes with DisallowUnknownFields, so one
// undeclared key loses the report while the hook still exits 0.
func TestModelCallReportUplinkBodies(t *testing.T) {
	telemetryDeclared := frozenIngestProperties(t, "AgentTelemetryIngestDTO")
	home := writeClaudeJSON(t, `{"oauthAccount":{"accountUuid":"au-1","emailAddress":"kyle@x.io",`+
		`"organizationName":"OffiCraft","organizationUuid":"org-1"}}`)
	srv, posts := contextServer(t)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", MemberID: "kyle", AgentsRoot: t.TempDir()}
	writeModelCallTime(modelCallSuccessPath(cfg), 900)
	env := testEnv(map[string]string{"HOME": home, "OC_HOST": "lab-1"})

	now := 1000.0
	walked := map[string]int{}
	drive := func(name, input string) capturedPost {
		before := len(*posts)
		var errOut bytes.Buffer
		cmdModelCallReport(srv.Client(), cfg, env, now, strings.NewReader(input), &errOut)
		now += 10
		if len(*posts) != before+1 {
			t.Fatalf("%s sent %v, want exactly one report", name, (*posts)[before:])
		}
		tel := (*posts)[before]
		walked[name+" → "+tel.path]++
		if bad := schemaViolations(tel.body, telemetryDeclared); len(bad) > 0 {
			t.Errorf("%s: telemetry body has keys the frozen schema refuses %v; body=%s", name, bad, tel.body)
		}
		if bad := modelCallViolations(t, tel.body); len(bad) > 0 {
			t.Errorf("%s: model_call does not match the frozen schema %v; body=%s", name, bad, tel.body)
		}
		return tel
	}

	failure := drive("failure", stopFailureInput("rate_limit", fixturePath(t, "sample-rate-limit.jsonl")))
	cleared := drive("clearing-success", stopInput)

	wantFailure := `{"runtime":"claude","account":"au-1/org-1","account_label":"kyle@x.io(OffiCraft)",` +
		`"machine":"lab-1","model_call":{"last_failure":{"ts":1000,"kind":"rate_limit",` +
		`"code":"rate_limit","resets_at":1790854200},"last_success_ts":900}}`
	if failure.body != wantFailure {
		t.Errorf("failure body =\n  %s\nwant\n  %s", failure.body, wantFailure)
	}
	wantCleared := `{"runtime":"claude","account":"au-1/org-1","account_label":"kyle@x.io(OffiCraft)",` +
		`"machine":"lab-1","model_call":{"last_success_ts":1010}}`
	if cleared.body != wantCleared {
		t.Errorf("clearing success body =\n  %s\nwant\n  %s", cleared.body, wantCleared)
	}

	committed := manifestUplinkPaths(t, "cli/ocagent/modelcallreport_wire_test.go")
	if !maps.Equal(walked, committed) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but the hooks posted %v",
			committed, walked)
	}
}
