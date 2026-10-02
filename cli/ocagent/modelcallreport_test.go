package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func stopFailureInput(code, transcript string) string {
	return `{"session_id":"11111111-1111-4111-8111-111111111111","transcript_path":"` + transcript +
		`","cwd":"/Users/example/.officraft/agents/ow-000000000000","prompt_id":"p-1",` +
		`"hook_event_name":"StopFailure","error":"` + code + `","last_assistant_message":"<redacted>"}`
}

// The rate-limit fixture's own row is stamped 2026-10-01T10:34:46.307Z; a hook
// that starts at fixtureHookStart is the failure that row belongs to.
const fixtureHookStart = 1790850886.3

// rateLimitRow is the real rate-limit sample restamped, as the harness would
// write it for a later failure.
func rateLimitRow(t *testing.T, timestamp, resetsAt string) []byte {
	t.Helper()
	raw, err := os.ReadFile(fixturePath(t, "sample-rate-limit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	row := strings.Replace(string(raw), `"timestamp": "2026-10-01T10:34:46.307Z"`, `"timestamp": "`+timestamp+`"`, 1)
	return []byte(strings.Replace(row, `"resetsAt": 1790854200`, `"resetsAt": `+resetsAt, 1))
}

// stubModelCallSleep replaces the transcript wait's sleep for one test: it
// records every wait and runs onSleep(n) on the n-th.
func stubModelCallSleep(t *testing.T, onSleep func(n int)) *[]time.Duration {
	t.Helper()
	var slept []time.Duration
	saved := modelCallSleep
	modelCallSleep = func(d time.Duration) {
		slept = append(slept, d)
		if onSleep != nil {
			onSleep(len(slept))
		}
	}
	t.Cleanup(func() { modelCallSleep = saved })
	return &slept
}

const stopInput = `{"session_id":"11111111-1111-4111-8111-111111111111","transcript_path":"/nowhere.jsonl",` +
	`"cwd":"/Users/example","prompt_id":"p-1","permission_mode":"default","hook_event_name":"Stop",` +
	`"stop_hook_active":false,"last_assistant_message":"OK","background_tasks":[],"session_crons":[]}`

func TestCmdModelCallReport(t *testing.T) {
	accountHome := writeClaudeJSON(t, `{"oauthAccount":{"accountUuid":"au-1","emailAddress":"kyle@x.io",`+
		`"organizationName":"OffiCraft","organizationUuid":"org-1"}}`)
	env := testEnv(map[string]string{"HOME": accountHome, "OC_HOST": "lab-1"})
	newCfg := func(t *testing.T, base string) Config {
		return Config{BaseConfigured: true, Base: base, Token: "t", MemberID: "Kyle", AgentsRoot: t.TempDir()}
	}
	memberFile := func(cfg Config, name string) string {
		return filepath.Join(cfg.AgentsRoot, "kyle", name)
	}

	t.Run("a Stop hook sends its success when the station has none from the last 30s or a newer failure", func(t *testing.T) {
		sentNow := []capturedPost{{
			path: "/api/monitoring/telemetry", auth: "Bearer t",
			body: `{"runtime":"claude","account":"au-1/org-1","account_label":"kyle@x.io(OffiCraft)",` +
				`"machine":"lab-1","model_call":{"last_success_ts":1000.5}}`,
		}}
		cases := []struct {
			name          string
			failure, sent float64
			wantPosts     []capturedPost
			wantSent      string
		}{
			{name: "never sent before", wantPosts: sentNow, wantSent: "1000.5"},
			{name: "the last accepted success is older than 30s", sent: 960, wantPosts: sentNow, wantSent: "1000.5"},
			{name: "the last accepted success is exactly 30s old", sent: 970.5, wantPosts: sentNow, wantSent: "1000.5"},
			{name: "a failure newer than the success accepted 10s ago", failure: 995, sent: 990,
				wantPosts: sentNow, wantSent: "1000.5"},
			{name: "a success accepted 10s ago and no failure", sent: 990, wantSent: "990"},
			{name: "a success accepted 10s ago, after the last failure", failure: 980, sent: 990, wantSent: "990"},
			{name: "a success accepted at the failure's own time, 20s ago", failure: 980, sent: 980, wantSent: "980"},
			{name: "an accepted success newer than this one is never moved back", failure: 3000, sent: 2000,
				wantPosts: sentNow, wantSent: "2000"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv, posts := contextServer(t)
				cfg := newCfg(t, srv.URL)
				if tc.failure > 0 {
					writeModelCallTime(memberFile(cfg, "model_call.failure"), tc.failure)
				}
				if tc.sent > 0 {
					writeModelCallTime(memberFile(cfg, "model_call.success_sent"), tc.sent)
				}
				var errOut bytes.Buffer

				rc := cmdModelCallReport(srv.Client(), cfg, env, 1000.5, strings.NewReader(stopInput), &errOut)

				if rc != 0 {
					t.Errorf("rc = %d, want 0", rc)
				}
				if !reflect.DeepEqual(*posts, tc.wantPosts) {
					t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, tc.wantPosts)
				}
				if got := readFileString(t, memberFile(cfg, "model_call.success")); got != "1000.5" {
					t.Errorf("success record = %q, want %q", got, "1000.5")
				}
				gotSent := ""
				if raw, err := os.ReadFile(memberFile(cfg, "model_call.success_sent")); err == nil {
					gotSent = string(raw)
				}
				if gotSent != tc.wantSent {
					t.Errorf("sent-success record = %q, want %q", gotSent, tc.wantSent)
				}
				if errOut.String() != "" {
					t.Errorf("stderr = %q, want empty", errOut.String())
				}
			})
		}
	})

	t.Run("a Stop success that does not get through stays owed and still exits 0", func(t *testing.T) {
		cases := []struct {
			name    string
			client  *fakeHTTP
			wantErr string
		}{
			{"refused", &fakeHTTP{status: 422, body: `{"detail":"bad"}`},
				"[ocagent] model-call-report: POST /api/monitoring/telemetry FAILED status=422: {\"detail\":\"bad\"}\n"},
			{"unreachable", &fakeHTTP{err: errors.New("connection refused")},
				"[ocagent] model-call-report: POST /api/monitoring/telemetry FAILED status=0: connection refused\n"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cfg := newCfg(t, "http://x")
				writeModelCallTime(memberFile(cfg, "model_call.failure"), 980)
				writeModelCallTime(memberFile(cfg, "model_call.success_sent"), 950)
				var errOut bytes.Buffer

				rc := cmdModelCallReport(tc.client, cfg, env, 1000.5, strings.NewReader(stopInput), &errOut)

				if rc != 0 {
					t.Errorf("rc = %d, want 0", rc)
				}
				if len(tc.client.seen) != 1 {
					t.Errorf("sent %v, want one attempt", tc.client.seen)
				}
				if errOut.String() != tc.wantErr {
					t.Errorf("stderr = %q, want %q", errOut.String(), tc.wantErr)
				}
				if got := readFileString(t, memberFile(cfg, "model_call.success_sent")); got != "950" {
					t.Errorf("sent-success record = %q, want it left at %q", got, "950")
				}
			})
		}
	})

	t.Run("a StopFailure rate limit is reported at once with the transcript's reset time", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		writeModelCallTime(memberFile(cfg, "model_call.success"), 900)
		slept := stubModelCallSleep(t, nil)
		var errOut bytes.Buffer

		rc := cmdModelCallReport(srv.Client(), cfg, env, fixtureHookStart,
			strings.NewReader(stopFailureInput("rate_limit", fixturePath(t, "sample-rate-limit.jsonl"))), &errOut)

		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		want := []capturedPost{{
			path: "/api/monitoring/telemetry",
			auth: "Bearer t",
			body: `{"runtime":"claude","account":"au-1/org-1","account_label":"kyle@x.io(OffiCraft)",` +
				`"machine":"lab-1","model_call":{"last_failure":{"ts":1790850886.3,"kind":"rate_limit",` +
				`"code":"rate_limit","resets_at":1790854200},"last_success_ts":900}}`,
		}}
		if !reflect.DeepEqual(*posts, want) {
			t.Errorf("sent\n  %+v\nwant\n  %+v", *posts, want)
		}
		if len(*slept) != 0 {
			t.Errorf("waited %v, want no wait for a row already written", *slept)
		}
		if got := readFileString(t, memberFile(cfg, "model_call.failure")); got != "1790850886.3" {
			t.Errorf("failure record = %q, want %q", got, "1790850886.3")
		}
		if got := readFileString(t, memberFile(cfg, "model_call.success")); got != "900" {
			t.Errorf("success record = %q, want it untouched at %q", got, "900")
		}
		if errOut.String() != "" {
			t.Errorf("stderr = %q, want empty", errOut.String())
		}
	})

	t.Run("each runtime error code is reported under its kind, verbatim", func(t *testing.T) {
		cases := []struct{ code, wantKind, wantCode string }{
			{"authentication_failed", "auth", "authentication_failed"},
			{"oauth_org_not_allowed", "auth", "oauth_org_not_allowed"},
			{"account_on_hold", "auth", "account_on_hold"},
			{"verification_required", "auth", "verification_required"},
			{"cloud_credential_error", "auth", "cloud_credential_error"},
			{"server_error", "server", "server_error"},
			{"overloaded", "server", "overloaded"},
			{"model_not_found", "other", "model_not_found"},
			{"", "other", "unknown"},
		}
		for _, tc := range cases {
			t.Run(tc.wantCode, func(t *testing.T) {
				srv, posts := contextServer(t)
				cfg := newCfg(t, srv.URL)
				slept := stubModelCallSleep(t, nil)
				var errOut bytes.Buffer

				cmdModelCallReport(srv.Client(), cfg, env, fixtureHookStart+60,
					strings.NewReader(stopFailureInput(tc.code, fixturePath(t, "sample-rate-limit.jsonl"))), &errOut)

				want := `{"runtime":"claude","account":"au-1/org-1","account_label":"kyle@x.io(OffiCraft)",` +
					`"machine":"lab-1","model_call":{"last_failure":{"ts":1790850946.3,"kind":"` + tc.wantKind +
					`","code":"` + tc.wantCode + `","resets_at":null}}}`
				if len(*posts) != 1 || (*posts)[0].body != want {
					t.Errorf("sent %+v, want one body %s", *posts, want)
				}
				if len(*slept) != 0 {
					t.Errorf("waited %v, want no wait: only a rate limit has a reset time to wait for", *slept)
				}
			})
		}
	})

	t.Run("a rate limit carries the reset time of its own error row only", func(t *testing.T) {
		const hookStart = fixtureHookStart + 60
		writeTranscript := func(t *testing.T, rows ...[]byte) string {
			path := filepath.Join(t.TempDir(), "transcript.jsonl")
			if err := os.WriteFile(path, bytes.Join(rows, nil), 0o644); err != nil {
				t.Fatal(err)
			}
			return path
		}
		padding := []byte(strings.Repeat(`{"type":"user","message":"`+strings.Repeat("x", 1000)+`"}`+"\n", 300))
		ownRow := rateLimitRow(t, "2026-10-01T10:35:46.350Z", "1790860000")
		ownEarlyRow := rateLimitRow(t, "2026-10-01T10:35:45.300Z", "1790860000")
		olderRow := rateLimitRow(t, "2026-10-01T10:34:46.307Z", "1790854200")
		otherRows, err := os.ReadFile(fixturePath(t, "sample-other-errors.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		ownServerRow := []byte(strings.Replace(strings.SplitAfter(string(otherRows), "\n")[0],
			`"timestamp": "2026-09-22T00:55:56.322Z"`, `"timestamp": "2026-10-01T10:35:46.350Z"`, 1))
		thirtyWaits := make([]time.Duration, 30)
		for i := range thirtyWaits {
			thirtyWaits[i] = 100 * time.Millisecond
		}

		cases := []struct {
			name       string
			transcript func(t *testing.T) string
			lateRow    []byte
			wantResets string
			wantSlept  []time.Duration
		}{
			{
				name:       "only an older failure's row: no reset time, never the old one",
				transcript: func(t *testing.T) string { return writeTranscript(t, olderRow) },
				wantResets: "null", wantSlept: thirtyWaits,
			},
			{
				name:       "its own row lands 50ms after the hook starts",
				transcript: func(t *testing.T) string { return writeTranscript(t, olderRow) },
				lateRow:    ownRow,
				wantResets: "1790860000", wantSlept: []time.Duration{100 * time.Millisecond},
			},
			{
				name:       "its own row never lands: no reset time after the wait",
				transcript: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.jsonl") },
				wantResets: "null", wantSlept: thirtyWaits,
			},
			{
				name:       "its own row stamped 1s before the hook starts, inside the slack",
				transcript: func(t *testing.T) string { return writeTranscript(t, olderRow, ownEarlyRow) },
				wantResets: "1790860000",
			},
			{
				name:       "its own row is already there behind an older one",
				transcript: func(t *testing.T) string { return writeTranscript(t, olderRow, ownRow) },
				wantResets: "1790860000",
			},
			{
				name:       "its own row carries no quota: no reset time, no wait",
				transcript: func(t *testing.T) string { return writeTranscript(t, olderRow, ownServerRow) },
				wantResets: "null",
			},
			{
				name:       "its own row at the end of a transcript longer than the tail",
				transcript: func(t *testing.T) string { return writeTranscript(t, padding, ownRow) },
				wantResets: "1790860000",
			},
			{
				name:       "its own row beyond the tail that is read",
				transcript: func(t *testing.T) string { return writeTranscript(t, ownRow, padding) },
				wantResets: "null", wantSlept: thirtyWaits,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				transcript := tc.transcript(t)
				slept := stubModelCallSleep(t, func(n int) {
					if n != 1 || tc.lateRow == nil {
						return
					}
					f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o644)
					if err != nil {
						t.Fatal(err)
					}
					defer f.Close()
					if _, err := f.Write(tc.lateRow); err != nil {
						t.Fatal(err)
					}
				})
				srv, posts := contextServer(t)
				cfg := newCfg(t, srv.URL)
				var errOut bytes.Buffer

				rc := cmdModelCallReport(srv.Client(), cfg, env, hookStart,
					strings.NewReader(stopFailureInput("rate_limit", transcript)), &errOut)

				want := `{"runtime":"claude","account":"au-1/org-1","account_label":"kyle@x.io(OffiCraft)",` +
					`"machine":"lab-1","model_call":{"last_failure":{"ts":1790850946.3,"kind":"rate_limit",` +
					`"code":"rate_limit","resets_at":` + tc.wantResets + `}}}`
				if rc != 0 || len(*posts) != 1 || (*posts)[0].body != want {
					t.Errorf("rc=%d sent %+v, want rc 0 and one body %s", rc, *posts, want)
				}
				if !reflect.DeepEqual(*slept, tc.wantSlept) {
					t.Errorf("waited %v, want %v", *slept, tc.wantSlept)
				}
				if errOut.String() != "" {
					t.Errorf("stderr = %q, want empty", errOut.String())
				}
			})
		}
	})

	t.Run("an unreadable account is omitted, never sent blank", func(t *testing.T) {
		srv, posts := contextServer(t)
		cfg := newCfg(t, srv.URL)
		var errOut bytes.Buffer

		cmdModelCallReport(srv.Client(), cfg, testEnv(map[string]string{"HOME": t.TempDir()}), 1000,
			strings.NewReader(stopFailureInput("server_error", fixturePath(t, "sample-other-errors.jsonl"))), &errOut)

		want := `{"runtime":"claude","machine":"m-server-self","model_call":{"last_failure":` +
			`{"ts":1000,"kind":"server","code":"server_error","resets_at":null}}}`
		if len(*posts) != 1 || (*posts)[0].body != want {
			t.Errorf("sent %+v, want one body %s", *posts, want)
		}
	})

	t.Run("a refused report is named on stderr and still exits 0 with the failure recorded", func(t *testing.T) {
		cfg := newCfg(t, "http://x")
		client := &fakeHTTP{status: 422, body: `{"detail":"bad"}`}
		var errOut bytes.Buffer

		rc := cmdModelCallReport(client, cfg, env, 1000,
			strings.NewReader(stopFailureInput("server_error", "")), &errOut)

		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		want := "[ocagent] model-call-report: POST /api/monitoring/telemetry FAILED status=422: {\"detail\":\"bad\"}\n"
		if errOut.String() != want {
			t.Errorf("stderr = %q, want %q", errOut.String(), want)
		}
		if len(client.seen) != 1 {
			t.Errorf("sent %v, want one attempt", client.seen)
		}
		if got := readFileString(t, memberFile(cfg, "model_call.failure")); got != "1000" {
			t.Errorf("failure record = %q, want %q", got, "1000")
		}
	})

	t.Run("an agent with no token records the failure and sends nothing", func(t *testing.T) {
		cfg := newCfg(t, "http://x")
		cfg.Token = ""
		client := &fakeHTTP{status: 200, body: "{}"}
		var errOut bytes.Buffer

		rc := cmdModelCallReport(client, cfg, env, 1000,
			strings.NewReader(stopFailureInput("server_error", "")), &errOut)

		if rc != 0 || client.seen != nil {
			t.Errorf("rc=%d sent %v, want rc 0 and nothing sent", rc, client.seen)
		}
		if got := readFileString(t, memberFile(cfg, "model_call.failure")); got != "1000" {
			t.Errorf("failure record = %q, want %q", got, "1000")
		}
	})

	t.Run("hook input that is not JSON is named on stderr and records nothing", func(t *testing.T) {
		cfg := newCfg(t, "http://x")
		client := &fakeHTTP{status: 200, body: "{}"}
		var errOut bytes.Buffer

		rc := cmdModelCallReport(client, cfg, env, 1000, strings.NewReader("not json"), &errOut)

		if rc != 0 || client.seen != nil {
			t.Errorf("rc=%d sent %v, want rc 0 and nothing sent", rc, client.seen)
		}
		if !strings.HasPrefix(errOut.String(), "[ocagent] model-call-report: hook input is not JSON: ") {
			t.Errorf("stderr = %q, want the not-JSON line", errOut.String())
		}
		if entries, _ := os.ReadDir(cfg.AgentsRoot); len(entries) != 0 {
			t.Errorf("wrote %v, want nothing", entries)
		}
	})
}
