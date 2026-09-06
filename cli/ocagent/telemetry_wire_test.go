package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// ── the frozen-schema coupling test (T-6b42) ────────────────────────────────
//
// This is the test whose ABSENCE let the Claude reporter go silently dark.
//
// The two ingest schemas in spec/openapi.json declare additionalProperties:false,
// and the server decodes every mutable write with DisallowUnknownFields. So a
// single key this reporter sends that the schema does not declare does not get
// dropped — it rejects the ENTIRE report. Usage, cost, and account identity all
// die together, the reporter still exits 0, and the throttle stamp still advances,
// so nothing anywhere looks wrong.
//
// The reporter and the schema live in different Go modules and cannot import each
// other, which is exactly why this drift was invisible to the compiler. This test
// re-couples them the only way available: it reads the frozen spec off disk and
// checks the real POST bodies against it. No server, no network.

// frozenIngestProperties loads one request schema's declared properties from the
// frozen spec as name → declared JSON type ("" when the property declares none),
// and asserts the schema really is closed (a schema that tolerated extra keys
// would make this whole test vacuous).
//
// The type comes along because a NAME-ONLY comparison answers half the question.
// A key can be declared and still be refused at runtime for having the wrong
// shape: the handler hand-validates the permissive scalars and returns a flat
// 400 for a non-string effort/model, and the codegen'd decoder 422s a non-object
// hardware/claude/runtimes. Either way the WHOLE report dies, which is the same
// silent-dark failure this file exists to prevent — and a name-only test stays
// green through it. (This is not hypothetical: the change that added `model`
// here shipped alongside a producer that could have sent the payload's nested
// model OBJECT instead of its id string.)
func frozenIngestProperties(t *testing.T, schemaName string) map[string]string {
	t.Helper()
	specPath := filepath.Join("..", "..", "spec", "openapi.json")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read frozen spec %s: %v", specPath, err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
				AdditionalProperties *bool `json:"additionalProperties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse frozen spec: %v", err)
	}
	schema, ok := spec.Components.Schemas[schemaName]
	if !ok {
		t.Fatalf("%s not in the frozen spec", schemaName)
	}
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Fatalf("%s is not a closed schema — this guard would be vacuous", schemaName)
	}
	declared := map[string]string{}
	for name, prop := range schema.Properties {
		declared[name] = prop.Type
	}
	return declared
}

// jsonKind reports the JSON type of a raw value, in OpenAPI's vocabulary.
func jsonKind(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "invalid"
	}
	switch trimmed[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

// schemaViolations reports every key in body the frozen schema would refuse:
// undeclared names, plus declared names carrying the wrong JSON type.
//
// A property that declares NO type is skipped on purpose — the ingest schemas
// leave the scalars deliberately permissive so a bad value is a flat 400 rather
// than a Pydantic-style 422, and inventing an expectation here would make this
// test assert something the contract does not say. `null` is likewise always
// allowed: it is how a producer says "not measured" for a declared-object block.
func schemaViolations(body string, declared map[string]string) []string {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &obj) != nil {
		return []string{"<body is not a JSON object>"}
	}
	var bad []string
	for key, raw := range obj {
		want, isDeclared := declared[key]
		if !isDeclared {
			bad = append(bad, key+" (undeclared)")
			continue
		}
		if want == "" {
			continue
		}
		if got := jsonKind(raw); got != "null" && got != want &&
			!(want == "number" && got == "number") &&
			!(want == "integer" && got == "number") {
			bad = append(bad, key+" (declared "+want+", sent "+got+")")
		}
	}
	sort.Strings(bad)
	return bad
}

// handlerValidatedKinds is the OTHER half of the type contract, and it exists
// because the frozen schema deliberately cannot express it.
//
// The ingest scalars are declared WITHOUT a type on purpose: the server
// hand-validates them so a bad value is a flat 400 instead of a codegen 422
// (see AgentTelemetryIngestDTO's description). The cost of that choice is that
// schemaViolations, which can only read what the schema declares, is blind to
// exactly these keys — it would stay green while the reporter sent an object
// where the handler demands a string and every report died with a 400.
//
// That blindness is not theoretical for `model`: the statusLine payload's
// `model` IS a nested object ({id, display_name}), and the reporter's job is to
// reduce it to one string. Sending the object through is a one-line slip that
// the schema, the compiler, and a name-only comparison all accept.
//
// So this table mirrors the handler's own validation (api_monitoring.go's
// HandleIngestTelemetry...): each entry is a key the handler 400s on when the
// JSON kind is wrong. Keep it in step with that handler — it is a copy of a
// contract, and the whole point of this file is that copies drift.
var handlerValidatedKinds = map[string]string{
	"runtime": "string",
	"effort":  "string",
	"model":   "string",
	"cost":    "number",
}

// handlerKindViolations reports keys whose JSON kind the SERVER would reject
// with a flat 400, independent of what the (deliberately permissive) schema says.
func handlerKindViolations(body string) []string {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &obj) != nil {
		return []string{"<body is not a JSON object>"}
	}
	var bad []string
	for key, want := range handlerValidatedKinds {
		raw, present := obj[key]
		if !present {
			continue
		}
		if got := jsonKind(raw); got != want {
			bad = append(bad, key+" (handler demands "+want+", sent "+got+")")
		}
	}
	sort.Strings(bad)
	return bad
}

// TestContextReportBodiesMatchFrozenIngestSchemas drives the real reporter over
// the payload shapes a live Claude Code session actually produces, and asserts
// every POST body is accepted by the frozen schema. The first case is the exact
// shape observed on the owner's machine (a null context percentage next to a
// genuinely-zero cost) — the shape under which the whole report was being thrown
// away.
func TestContextReportBodiesMatchFrozenIngestSchemas(t *testing.T) {
	contextDeclared := frozenIngestProperties(t, "AgentContextIngestDTO")
	telemetryDeclared := frozenIngestProperties(t, "AgentTelemetryIngestDTO")

	home := writeClaudeJSON(t,
		`{"userID":"acct-1","oauthAccount":{"emailAddress":"e@x.io","organizationName":"Org","organizationUuid":"org-1"}}`)
	today := transcriptToday(t)

	cases := []struct {
		name    string
		payload string
	}{
		{
			// The real observed shape: no usable context percentage, cost a true 0.
			name:    "null pct and zero cost",
			payload: `{"context_window":{"used_percentage":null},"cost":{"total_cost_usd":0}}`,
		},
		{
			name:    "pct only",
			payload: `{"context_window":{"used_percentage":28.93}}`,
		},
		{
			name: "everything measured",
			payload: `{"context_window":{"used_percentage":41.5},
				"cost":{"total_cost_usd":1.25,"total_duration_ms":90000},
				"rate_limits":{"five_hour":{"used_percentage":30,"resets_at":1720000000},
				               "seven_day":{"used_percentage":60,"resets_at":1720500000}},
				"model":{"display_name":"Opus","id":"claude-opus-5"},
				"transcript_path":"` + today + `"}`,
		},
		{
			name:    "nothing measurable at all",
			payload: `{}`,
		},
	}

	// Which uplinks this test actually walked against the frozen schema — not which
	// ones it meant to. Compared below against the manifest's own count, so a row
	// added here without an assertion to go with it is short by one and red.
	walked := map[string]int{}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, posts := contextServer(t)
			cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
			var out, errOut bytes.Buffer
			cmdContextReport(srv.Client(), cfg,
				testEnv(map[string]string{"HOME": home, "OC_HOST": "lab-1"}),
				1000.0, strings.NewReader(tc.payload), &out, &errOut)

			// Identity must be reported for EVERY shape, including the last one where
			// nothing at all was measurable.
			tel := findPost(*posts, "/api/monitoring/telemetry")
			if tel == nil {
				t.Fatalf("no telemetry POST; posts=%v", *posts)
			}
			walked["/api/monitoring/telemetry"]++
			if bad := schemaViolations(tel.body, telemetryDeclared); len(bad) > 0 {
				t.Errorf("telemetry body has keys the frozen schema refuses %v — the whole "+
					"report (usage AND account) would 400/422; body=%s", bad, tel.body)
			}
			if bad := handlerKindViolations(tel.body); len(bad) > 0 {
				t.Errorf("telemetry body has values the SERVER refuses %v — the whole "+
					"report would 400; body=%s", bad, tel.body)
			}
			if ctx := findPost(*posts, "/api/agent/context"); ctx != nil {
				walked["/api/agent/context"]++
				if bad := schemaViolations(ctx.body, contextDeclared); len(bad) > 0 {
					t.Errorf("context body has keys the frozen schema refuses %v — the gauge "+
						"would 400/422; body=%s", bad, ctx.body)
				}
			}
		})
	}

	// The join: the routes committed to this file, against the routes a real test
	// server observed the real producer post to. Compared as SETS because the loop
	// above drives the reporter once per payload shape, so the send counts are a
	// property of the case table, not of the manifest.
	//
	// That makes one thing the set form cannot see, so it is asserted directly: two
	// rows on the same route would be indistinguishable here. While every route
	// carries exactly one row, "committed" and "walked" are the same question.
	want := manifestUplinkPaths(t, "cli/ocagent/telemetry_wire_test.go")
	for route, rows := range want {
		if rows != 1 {
			t.Fatalf("cli/uplinks.json commits %d uplinks to %s through this wire test. "+
				"This join compares route SETS, so it cannot tell them apart — give the "+
				"second one its own assertion and count sends, or split the wire test.",
				rows, route)
		}
	}
	seen := map[string]int{}
	for route := range walked {
		seen[route] = 1
	}
	if !maps.Equal(seen, want) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but the producer posted to "+
			"%v. A committed uplink nobody compared is exactly the gap the shipped clients "+
			"went dark through.", want, walked)
	}
}

// TestContextReportSendsSessionEffort pins the session's LIVE reasoning effort
// onto the telemetry body. It was never sent at all: the reporter only ever put
// effort on the status-line string, so every Claude session reported a blank
// effort forever while the server, the frozen schema and the monitoring page were
// all already wired for it — and a blank is indistinguishable from a session that
// simply has not reported yet, which is why it survived unnoticed until the owner
// spotted it on screen. Two properties are pinned beyond presence: the value is
// the payload's LIVE effort.level (never the launch intent, which cannot follow a
// mid-session /effort change), and it is VERBATIM (the status line's "med"
// abbreviation must never leak onto the wire).
func TestContextReportSendsSessionEffort(t *testing.T) {
	home := writeClaudeJSON(t, `{"userID":"acct-1"}`)

	cases := []struct {
		name   string
		effort string
		want   any
	}{
		{name: "reported verbatim", effort: "medium", want: "medium"},
		{name: "non-default passes through", effort: "xhigh", want: "xhigh"},
		{name: "no effort block is omitted, never a blank", effort: "", want: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, posts := contextServer(t)
			cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
			payload := `{"context_window":{"used_percentage":41.5}}`
			if tc.effort != "" {
				payload = `{"context_window":{"used_percentage":41.5},` +
					`"effort":{"level":"` + tc.effort + `"}}`
			}
			var out, errOut bytes.Buffer
			cmdContextReport(srv.Client(), cfg,
				testEnv(map[string]string{"HOME": home, "OC_HOST": "lab-1"}), 1000.0,
				strings.NewReader(payload), &out, &errOut)

			tel := findPost(*posts, "/api/monitoring/telemetry")
			if tel == nil {
				t.Fatalf("no telemetry POST; posts=%v", *posts)
			}
			var body map[string]any
			if err := json.Unmarshal([]byte(tel.body), &body); err != nil {
				t.Fatalf("telemetry body is not JSON: %v", err)
			}
			if got := body["effort"]; got != tc.want {
				t.Errorf("telemetry effort = %v, want %v; body=%s", got, tc.want, tel.body)
			}
			// The context gauge DTO does not declare effort and refuses undeclared
			// keys, so one stray copy there would 422 the whole gauge POST.
			if ctx := findPost(*posts, "/api/agent/context"); ctx != nil {
				if strings.Contains(ctx.body, "effort") {
					t.Errorf("effort rode the context POST; it would 422; body=%s", ctx.body)
				}
			}
		})
	}
}

// TestContextReportSendsSessionModel is the model twin of the effort test above:
// the model was on the status-line string from day one and in no POST body ever,
// so the cockpit's 模型 column had nothing reported to serve and fell back to the
// owner's configured launch value — a fallback an outsource worker does not even
// have, which is why its column was blank forever.
//
// Beyond presence it pins WHICH of the payload's two model strings goes on the
// wire: `model.id`, never `model.display_name`. The id is the vocabulary the boot
// seed already tells members to report, and it is the only one carrying the
// "[1m]" 1M-context marker — display_name reads "Opus 4.5" for both tiers, so
// sending it would collapse two genuinely different sessions onto one string.
//
// ⚠️ COVERAGE NOTE — do not read the `want: nil` cases as more than they are.
// Only the two positive cases could have failed before this change; the two
// absent-cases were vacuously true then, because the reporter sent no `model`
// key under ANY input. What they guard is the omit-vs-blank distinction INSIDE
// the new design (an unmeasured model must be OMITTED, never sent as ""), which
// is the distinction the server's stamp guard depends on. They are not evidence
// that the field is being sent at all — the positive cases are.
func TestContextReportSendsSessionModel(t *testing.T) {
	home := writeClaudeJSON(t, `{"userID":"acct-1"}`)

	cases := []struct {
		name  string
		model string
		want  any
	}{
		{
			name:  "the id is sent verbatim",
			model: `"model":{"id":"claude-opus-4-5-20251101","display_name":"Opus 4.5"},`,
			want:  "claude-opus-4-5-20251101",
		},
		{
			name:  "the 1M marker survives",
			model: `"model":{"id":"claude-opus-4-5-20251101[1m]","display_name":"Opus 4.5"},`,
			want:  "claude-opus-4-5-20251101[1m]",
		},
		{
			name:  "no model block is omitted, never a blank",
			model: "",
			want:  nil,
		},
		{
			name:  "a display_name with no id is omitted, never guessed",
			model: `"model":{"display_name":"Opus 4.5"},`,
			want:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, posts := contextServer(t)
			cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
			payload := `{` + tc.model + `"context_window":{"used_percentage":41.5}}`
			var out, errOut bytes.Buffer
			cmdContextReport(srv.Client(), cfg,
				testEnv(map[string]string{"HOME": home, "OC_HOST": "lab-1"}), 1000.0,
				strings.NewReader(payload), &out, &errOut)

			tel := findPost(*posts, "/api/monitoring/telemetry")
			if tel == nil {
				t.Fatalf("no telemetry POST; posts=%v", *posts)
			}
			var body map[string]any
			if err := json.Unmarshal([]byte(tel.body), &body); err != nil {
				t.Fatalf("telemetry body is not JSON: %v", err)
			}
			if got := body["model"]; got != tc.want {
				t.Errorf("telemetry model = %v, want %v; body=%s", got, tc.want, tel.body)
			}
			// AgentContextIngestDTO declares no model and refuses undeclared keys,
			// so one stray copy there would 422 the whole gauge POST.
			if ctx := findPost(*posts, "/api/agent/context"); ctx != nil {
				if strings.Contains(ctx.body, "model") {
					t.Errorf("model rode the context POST; it would 422; body=%s", ctx.body)
				}
			}
		})
	}
}

// transcriptToday writes a one-row transcript dated today so the tokens source is
// live, and returns its path.
func transcriptToday(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	day := nowUTCDate()
	line := `{"type":"assistant","timestamp":"` + day +
		`T10:00:00Z","message":{"usage":{"input_tokens":7,"output_tokens":3}}}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestContextReportSurfacesRefusedPost: a refused POST must leave a trace on
// STDERR while the status line still goes to stdout untouched and the exit code
// stays 0. The old reporter discarded the status entirely, so hours of 422s were
// indistinguishable from hours of healthy reporting — the throttle stamp kept
// advancing and the process kept exiting 0, which is the failure mode that made
// this bug survive three separate investigations.
func TestContextReportSurfacesRefusedPost(t *testing.T) {
	var refused []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refused = append(refused, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"error":{"code":"validation_error",` +
			`"message":"invalid request body: json: unknown field \"agent_id\""}}`))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	var out, errOut bytes.Buffer
	rc := cmdContextReport(srv.Client(), cfg, testEnv(nil), 1000.0,
		strings.NewReader(`{"context_window":{"used_percentage":28.93}}`), &out, &errOut)

	if rc != 0 {
		t.Errorf("rc = %d, want 0 — a refused report must never break the status line", rc)
	}
	if len(refused) != 2 {
		t.Fatalf("expected both POSTs attempted, saw %v", refused)
	}
	diag := errOut.String()
	for _, want := range []string{"/api/agent/context", "/api/monitoring/telemetry", "422"} {
		if !strings.Contains(diag, want) {
			t.Errorf("stderr diagnostic missing %q; got %q", want, diag)
		}
	}
	if !strings.Contains(diag, "unknown field") {
		t.Errorf("stderr must carry the server's own explanation; got %q", diag)
	}
	// stdout stays the status line ALONE — Claude Code renders it verbatim.
	if strings.Contains(out.String(), "FAILED") {
		t.Errorf("diagnostics must not pollute the status line; stdout = %q", out.String())
	}
	if got := stripANSI(out.String()); !strings.Contains(got, "29%") {
		t.Errorf("status line = %q, want the rendered pct", got)
	}
}

// nowUTCDate is the UTC day parseTranscriptTokens filters on.
func nowUTCDate() string { return time.Now().UTC().Format("2006-01-02") }
