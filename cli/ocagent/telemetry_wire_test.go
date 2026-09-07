package main

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// todayTranscript writes a one-row Claude Code transcript dated today, so the
// token source the reporter reads is live rather than filtered out by date.
func todayTranscript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	row := `{"type":"assistant","timestamp":"` + time.Now().UTC().Format("2006-01-02") +
		`T10:00:00Z","message":{"usage":{"input_tokens":7,"output_tokens":3,` +
		`"cache_creation_input_tokens":5,"cache_read_input_tokens":11}}}`
	if err := os.WriteFile(path, []byte(row+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestContextReportUplinkBodies drives the real reporter and compares every body
// it puts on the wire against the frozen request schemas the server decodes with
// DisallowUnknownFields — one undeclared key or one wrong type rejects the WHOLE
// report while the reporter still exits 0.
func TestContextReportUplinkBodies(t *testing.T) {
	contextDeclared := frozenIngestProperties(t, "AgentContextIngestDTO")
	telemetryDeclared := frozenIngestProperties(t, "AgentTelemetryIngestDTO")
	home := writeClaudeJSON(t, `{"userID":"acct-1","oauthAccount":{`+
		`"accountUuid":"au-1","emailAddress":"kyle@x.io",`+
		`"organizationName":"OffiCraft","organizationUuid":"org-1"}}`)
	transcript := todayTranscript(t)

	const identityOnly = `{"runtime":"claude","account":"au-1/org-1",` +
		`"account_label":"kyle@x.io(OffiCraft)","machine":"lab-1"}`

	cases := []struct {
		name          string
		payload       string
		wantPaths     []string
		wantContext   string
		wantTelemetry string
	}{
		{
			name: "every source measured",
			payload: `{"context_window":{"used_percentage":41.5},` +
				`"cost":{"total_cost_usd":1.25},` +
				`"rate_limits":{"five_hour":{"used_percentage":30,"resets_at":1720000000},` +
				`"seven_day":{"used_percentage":60,"resets_at":1720500000}},` +
				`"model":{"id":"claude-opus-5","display_name":"Opus"},` +
				`"effort":{"level":"high"},"transcript_path":"` + transcript + `"}`,
			wantPaths:   []string{"/api/agent/context", "/api/monitoring/telemetry"},
			wantContext: `{"context_pct":41.5}`,
			wantTelemetry: `{"runtime":"claude","rate_limits":{"five_hour":{"used_percentage":30,` +
				`"resets_at":1720000000},"seven_day":{"used_percentage":60,"resets_at":1720500000}},` +
				`"cost":1.25,"tokens":{"burned":12,"output":3,"cache_read":11},` +
				`"account":"au-1/org-1","account_label":"kyle@x.io(OffiCraft)",` +
				`"machine":"lab-1","effort":"high","model":"claude-opus-5"}`,
		},
		{
			name:          "nothing measured yet",
			payload:       `{}`,
			wantPaths:     []string{"/api/monitoring/telemetry"},
			wantTelemetry: identityOnly,
		},
		{
			name:          "unmeasurable values are omitted, never reported as zero",
			payload:       `{"context_window":{"used_percentage":"41.5"},"cost":{"total_cost_usd":"free"}}`,
			wantPaths:     []string{"/api/monitoring/telemetry"},
			wantTelemetry: identityOnly,
		},
	}

	walked := map[string]int{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, posts := contextServer(t)
			cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
			var out, errOut bytes.Buffer

			rc := cmdContextReport(srv.Client(), cfg,
				testEnv(map[string]string{"HOME": home, "OC_HOST": "lab-1"}),
				1000.0, strings.NewReader(tc.payload), &out, &errOut)

			if rc != 0 {
				t.Errorf("rc = %d, want 0", rc)
			}
			if errOut.String() != "" {
				t.Errorf("stderr = %q, want empty — the server accepted every report", errOut.String())
			}
			if got := postPaths(*posts); !reflect.DeepEqual(got, tc.wantPaths) {
				t.Fatalf("posted to %v, want %v", got, tc.wantPaths)
			}
			for _, one := range *posts {
				if one.auth != "Bearer t" {
					t.Errorf("POST %s Authorization = %q, want %q", one.path, one.auth, "Bearer t")
				}
			}

			tel := findPost(*posts, "/api/monitoring/telemetry")
			walked["/api/monitoring/telemetry"]++
			if bad := schemaViolations(tel.body, telemetryDeclared); len(bad) > 0 {
				t.Errorf("telemetry body has keys the frozen schema refuses %v — the whole "+
					"report would be rejected; body=%s", bad, tel.body)
			}
			if tel.body != tc.wantTelemetry {
				t.Errorf("telemetry body =\n  %s\nwant\n  %s", tel.body, tc.wantTelemetry)
			}

			if tc.wantContext == "" {
				return
			}
			ctx := findPost(*posts, "/api/agent/context")
			walked["/api/agent/context"]++
			if bad := schemaViolations(ctx.body, contextDeclared); len(bad) > 0 {
				t.Errorf("context body has keys the frozen schema refuses %v — the gauge "+
					"would be rejected; body=%s", bad, ctx.body)
			}
			if ctx.body != tc.wantContext {
				t.Errorf("context body = %s, want %s", ctx.body, tc.wantContext)
			}
		})
	}

	want := manifestUplinkPaths(t, "cli/ocagent/telemetry_wire_test.go")
	for route, rows := range want {
		if rows != 1 {
			t.Fatalf("cli/uplinks.json commits %d uplinks to %s through this wire test; the "+
				"join below compares route SETS and cannot tell them apart", rows, route)
		}
	}
	seen := map[string]int{}
	for route := range walked {
		seen[route] = 1
	}
	if !maps.Equal(seen, want) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but the reporter posted to %v",
			want, walked)
	}
}
