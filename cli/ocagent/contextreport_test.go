package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeHTTP answers every request with one canned verdict and records what was
// asked of it as "<METHOD> <path> <body>".
type fakeHTTP struct {
	status int
	body   string
	err    error
	seen   []string
}

func (f *fakeHTTP) Do(r *http.Request) (*http.Response, error) {
	raw := ""
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
	}
	f.seen = append(f.seen, r.Method+" "+r.URL.Path+" "+raw)
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     http.Header{},
	}, nil
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func TestCmdContextReport(t *testing.T) {
	const payload = `{"context_window":{"used_percentage":40},"cost":{"total_cost_usd":2}}`
	const wantLine = "\x1b[90m████░░░░░░\x1b[0m \x1b[32m40%\x1b[0m\x1b[90m | \x1b[0m\x1b[33m$2.00\x1b[0m\n"

	t.Run("an accepted burst stamps the throttle window and clears the backoff", func(t *testing.T) {
		home := t.TempDir()
		cfg := Config{BaseConfigured: true, Base: "http://x", Token: "t", ID: "Kyle", Home: home}
		stale := filepath.Join(home, "kyle", "context_report.backoff")
		writeReportBackoff(stale, reportBackoffState{failures: 3, lastAttempt: 1})
		client := &fakeHTTP{status: 200, body: "{}"}
		var out, errOut bytes.Buffer

		rc := cmdContextReport(client, cfg, testEnv(map[string]string{"HOME": t.TempDir()}),
			1000.0, strings.NewReader(payload), &out, &errOut)

		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		if out.String() != wantLine {
			t.Errorf("stdout = %q, want %q", out.String(), wantLine)
		}
		if errOut.String() != "" {
			t.Errorf("stderr = %q, want empty", errOut.String())
		}
		if got := readFileString(t, filepath.Join(home, "kyle", "context_report.stamp")); got != "1000" {
			t.Errorf("throttle stamp = %q, want %q", got, "1000")
		}
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Errorf("the backoff record survived a delivered burst (err=%v), so the next "+
				"tick would still be spaced out by an outage that is over", err)
		}
	})

	t.Run("a refused burst records the failure and leaves the stamp untouched", func(t *testing.T) {
		home := t.TempDir()
		cfg := Config{BaseConfigured: true, Base: "http://x", Token: "t", ID: "kyle", Home: home}
		client := &fakeHTTP{status: 422, body: `{"detail":"context_pct is not declared"}`}
		var out, errOut bytes.Buffer

		rc := cmdContextReport(client, cfg, testEnv(map[string]string{"HOME": t.TempDir()}),
			1000.0, strings.NewReader(payload), &out, &errOut)

		if rc != 0 {
			t.Errorf("rc = %d, want 0 — a refused report must never break the status line", rc)
		}
		if out.String() != wantLine {
			t.Errorf("stdout = %q, want %q", out.String(), wantLine)
		}
		wantErr := "[ocagent] context-report: POST /api/agent/context FAILED status=422: " +
			`{"detail":"context_pct is not declared"}` + "\n" +
			"[ocagent] context-report: POST /api/monitoring/telemetry FAILED status=422: " +
			`{"detail":"context_pct is not declared"}` + "\n"
		if errOut.String() != wantErr {
			t.Errorf("stderr =\n%q\nwant\n%q", errOut.String(), wantErr)
		}
		if _, err := os.Stat(filepath.Join(home, "kyle", "context_report.stamp")); !os.IsNotExist(err) {
			t.Errorf("a refused burst wrote a throttle stamp (err=%v) — the stamp is the only "+
				"evidence of DELIVERY and would then argue the reporter is healthy", err)
		}
		got := readFileString(t, filepath.Join(home, "kyle", "context_report.backoff"))
		if got != "1 1000" {
			t.Errorf("backoff record = %q, want %q", got, "1 1000")
		}
	})

	t.Run("a burst inside the throttle window sends nothing", func(t *testing.T) {
		home := t.TempDir()
		cfg := Config{BaseConfigured: true, Base: "http://x", Token: "t", ID: "kyle", Home: home}
		writeStamp(filepath.Join(home, "kyle", "context_report.stamp"), 990.0)
		client := &fakeHTTP{status: 200, body: "{}"}
		var out, errOut bytes.Buffer

		rc := cmdContextReport(client, cfg, testEnv(map[string]string{"HOME": t.TempDir()}),
			1000.0, strings.NewReader(payload), &out, &errOut)

		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		if out.String() != wantLine {
			t.Errorf("stdout = %q, want %q", out.String(), wantLine)
		}
		if client.seen != nil {
			t.Errorf("sent %v, want nothing within the 30s window", client.seen)
		}
	})

	t.Run("a burst inside the failure backoff sends nothing", func(t *testing.T) {
		home := t.TempDir()
		cfg := Config{BaseConfigured: true, Base: "http://x", Token: "t", ID: "kyle", Home: home}
		writeReportBackoff(filepath.Join(home, "kyle", "context_report.backoff"),
			reportBackoffState{failures: 4, lastAttempt: 900})
		client := &fakeHTTP{status: 200, body: "{}"}
		var out, errOut bytes.Buffer

		rc := cmdContextReport(client, cfg, testEnv(map[string]string{"HOME": t.TempDir()}),
			1000.0, strings.NewReader(payload), &out, &errOut)

		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		if out.String() != wantLine {
			t.Errorf("stdout = %q, want %q", out.String(), wantLine)
		}
		if client.seen != nil {
			t.Errorf("sent %v, want nothing — 4 consecutive failures space attempts 240s apart "+
				"and only 100s have passed", client.seen)
		}
	})

	t.Run("an unconfigured agent reports nothing and still prints the status line", func(t *testing.T) {
		home := t.TempDir()
		cfg := Config{Base: "http://x", Home: home}
		client := &fakeHTTP{status: 200, body: "{}"}
		var out, errOut bytes.Buffer

		rc := cmdContextReport(client, cfg, testEnv(nil), 1000.0,
			strings.NewReader(payload), &out, &errOut)

		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		if out.String() != wantLine {
			t.Errorf("stdout = %q, want %q", out.String(), wantLine)
		}
		wantErr := "[ocagent] context-report: no OC_BASE configured — nothing here knows " +
			"which station to talk to, and the built-in default is this machine's loopback address.\n"
		if errOut.String() != wantErr {
			t.Errorf("stderr = %q, want %q", errOut.String(), wantErr)
		}
		if client.seen != nil {
			t.Errorf("sent %v, want nothing without OC_TOKEN/OC_ID", client.seen)
		}
	})

	t.Run("a null pct skips its own POST and never blocks the telemetry one", func(t *testing.T) {
		home := t.TempDir()
		cfg := Config{BaseConfigured: true, Base: "http://x", Token: "t", ID: "kyle", Home: home}
		client := &fakeHTTP{status: 200, body: "{}"}
		var out, errOut bytes.Buffer

		rc := cmdContextReport(client, cfg,
			testEnv(map[string]string{"HOME": t.TempDir(), "OC_HOST": "lab-1"}),
			1000.0, strings.NewReader(`{"context_window":{"used_percentage":null}}`), &out, &errOut)

		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		if out.String() != "\n" {
			t.Errorf("stdout = %q, want %q — nothing in this payload renders a segment",
				out.String(), "\n")
		}
		want := []string{`POST /api/monitoring/telemetry {"runtime":"claude","machine":"lab-1"}`}
		if !reflect.DeepEqual(client.seen, want) {
			t.Errorf("sent %q, want %q", client.seen, want)
		}
	})
}

func TestReportPost(t *testing.T) {
	cfg := Config{Base: "http://x", Token: "t"}

	t.Run("an accepted report says so and leaves no trace", func(t *testing.T) {
		client := &fakeHTTP{status: 201, body: "{}"}
		var errOut bytes.Buffer

		if !reportPost(client, cfg, "/api/agent/context", contextBody{ContextPct: 12.5}, &errOut) {
			t.Error("reportPost = false on 201, want true")
		}
		if errOut.String() != "" {
			t.Errorf("stderr = %q, want empty", errOut.String())
		}
		want := []string{`POST /api/agent/context {"context_pct":12.5}`}
		if !reflect.DeepEqual(client.seen, want) {
			t.Errorf("sent %q, want %q", client.seen, want)
		}
	})

	t.Run("a refusal is reported as not delivered and named on stderr", func(t *testing.T) {
		client := &fakeHTTP{status: 422, body: "  unknown field \"context_pct\"  "}
		var errOut bytes.Buffer

		if reportPost(client, cfg, "/api/agent/context", contextBody{}, &errOut) {
			t.Error("reportPost = true on 422, want false")
		}
		want := "[ocagent] context-report: POST /api/agent/context FAILED status=422: " +
			"unknown field \"context_pct\"\n"
		if errOut.String() != want {
			t.Errorf("stderr = %q, want %q", errOut.String(), want)
		}
	})

	t.Run("a transport fault counts as not delivered", func(t *testing.T) {
		client := &fakeHTTP{err: io.ErrUnexpectedEOF}
		var errOut bytes.Buffer

		if reportPost(client, cfg, "/api/monitoring/telemetry", telemetryBody{Runtime: "claude"}, &errOut) {
			t.Error("reportPost = true on a transport fault, want false — nothing was stored")
		}
		want := "[ocagent] context-report: POST /api/monitoring/telemetry FAILED status=0: " +
			"unexpected EOF\n"
		if errOut.String() != want {
			t.Errorf("stderr = %q, want %q", errOut.String(), want)
		}
	})
}

func TestTruncateForLog(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"a short body passes through verbatim", "boom", "boom"},
		{"exactly 400 bytes is not truncated", strings.Repeat("a", 400), strings.Repeat("a", 400)},
		{"401 bytes is cut to 400 plus an ellipsis", strings.Repeat("a", 401), strings.Repeat("a", 400) + "…"},
		{"an empty body stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncateForLog(tc.in); got != tc.want {
				t.Errorf("truncateForLog(%d bytes) = %q, want %q", len(tc.in), got, tc.want)
			}
		})
	}
}

func TestRenderStatusline(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name: "every source measured",
			payload: `{"model":{"id":"claude-opus-5[1m]","display_name":"Opus 5"},` +
				`"effort":{"level":"medium"},"context_window":{"used_percentage":41.5},` +
				`"cost":{"total_cost_usd":1.25,"total_duration_ms":3725000},` +
				`"rate_limits":{"five_hour":{"used_percentage":30,"resets_at":1720003600},` +
				`"seven_day":{"used_percentage":60,"resets_at":1720300000}}}`,
			want: "\x1b[34m◆ Opus 5 (1M context)\x1b[0m \x1b[33m⚡med\x1b[0m" +
				"\x1b[90m | \x1b[0m\x1b[90m████░░░░░░\x1b[0m \x1b[32m42%\x1b[0m" +
				"\x1b[90m | \x1b[0m\x1b[33m$1.25\x1b[0m" +
				"\x1b[90m | \x1b[0m\x1b[90m1h02m\x1b[0m" +
				"\x1b[90m | \x1b[0m\x1b[90m5h:30%(rst:1h0m) 7d:60%(50%elapsed)\x1b[0m",
		},
		{
			name:    "one measured source renders alone, with no separators",
			payload: `{"context_window":{"used_percentage":0}}`,
			want:    "\x1b[90m░░░░░░░░░░\x1b[0m \x1b[32m0%\x1b[0m",
		},
		{name: "an empty payload renders an empty line", payload: "", want: ""},
		{name: "a junk payload renders an empty line", payload: "not json", want: ""},
		{name: "a JSON array renders an empty line", payload: "[1,2]", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderStatusline(tc.payload, 1720000000); got != tc.want {
				t.Errorf("renderStatusline = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestModelEffortSegment(t *testing.T) {
	cases := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{
			name: "model and effort together",
			obj: map[string]any{
				"model":  map[string]any{"id": "claude-opus-5", "display_name": " Opus 5 "},
				"effort": map[string]any{"level": "high"},
			},
			want: "\x1b[34m◆ Opus 5\x1b[0m \x1b[33m⚡high\x1b[0m",
		},
		{
			name: "a 1M-tier id appends the context hint",
			obj:  map[string]any{"model": map[string]any{"id": "claude-opus-5[1m]", "display_name": "Opus 5"}},
			want: "\x1b[34m◆ Opus 5 (1M context)\x1b[0m",
		},
		{
			name: "a display name that already says 1M is left alone",
			obj:  map[string]any{"model": map[string]any{"id": "claude-opus-5[1m]", "display_name": "Opus 5 1M"}},
			want: "\x1b[34m◆ Opus 5 1M\x1b[0m",
		},
		{
			name: "a bare effort renders without a model",
			obj:  map[string]any{"effort": map[string]any{"level": "low"}},
			want: "\x1b[33m⚡low\x1b[0m",
		},
		{
			name: "medium abbreviates to med",
			obj:  map[string]any{"effort": map[string]any{"level": "medium"}},
			want: "\x1b[33m⚡med\x1b[0m",
		},
		{name: "nothing measured renders nothing", obj: map[string]any{}, want: ""},
		{
			name: "a blank display name drops the model half",
			obj:  map[string]any{"model": map[string]any{"id": "x[1m]", "display_name": "   "}},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelEffortSegment(tc.obj); got != tc.want {
				t.Errorf("modelEffortSegment = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEffortValue(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"the live level is read verbatim and trimmed", `{"effort":{"level":"  high  "}}`, "high"},
		{"medium is NOT abbreviated on the wire value", `{"effort":{"level":"medium"}}`, "medium"},
		{"a model with no effort block reports nothing", `{"model":{"id":"x"}}`, ""},
		{"a non-string level reports nothing", `{"effort":{"level":3}}`, ""},
		{"a junk payload reports nothing", "not json", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effortValue(tc.payload); got != tc.want {
				t.Errorf("effortValue(%q) = %q, want %q", tc.payload, got, tc.want)
			}
		})
	}
}

func TestModelValue(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"the id is read verbatim and trimmed", `{"model":{"id":" claude-opus-5[1m] "}}`, "claude-opus-5[1m]"},
		{"the display name is never substituted", `{"model":{"display_name":"Opus 5"}}`, ""},
		{"a missing model block reports nothing", `{"effort":{"level":"high"}}`, ""},
		{"a junk payload reports nothing", "[]", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelValue(tc.payload); got != tc.want {
				t.Errorf("modelValue(%q) = %q, want %q", tc.payload, got, tc.want)
			}
		})
	}
}

func TestEffortLabel(t *testing.T) {
	cases := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{"medium abbreviates", map[string]any{"effort": map[string]any{"level": "medium"}}, "med"},
		{"high passes through", map[string]any{"effort": map[string]any{"level": "high"}}, "high"},
		{"absent stays blank", map[string]any{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effortLabel(tc.obj); got != tc.want {
				t.Errorf("effortLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestContextBarSegment(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"41.5% fills four cells and rounds to 42", `{"context_window":{"used_percentage":41.5}}`,
			"\x1b[90m████░░░░░░\x1b[0m \x1b[32m42%\x1b[0m"},
		{"0% draws an empty bar rather than nothing", `{"context_window":{"used_percentage":0}}`,
			"\x1b[90m░░░░░░░░░░\x1b[0m \x1b[32m0%\x1b[0m"},
		{"an over-range value clamps to a full bar", `{"context_window":{"used_percentage":150}}`,
			"\x1b[90m██████████\x1b[0m \x1b[32m100%\x1b[0m"},
		{"a null pct renders no segment", `{"context_window":{"used_percentage":null}}`, ""},
		{"a missing context_window renders no segment", `{}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextBarSegment(tc.payload); got != tc.want {
				t.Errorf("contextBarSegment(%q) = %q, want %q", tc.payload, got, tc.want)
			}
		})
	}
}

func TestCostSegment(t *testing.T) {
	cases := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{"a real total renders two decimals", map[string]any{"cost": map[string]any{"total_cost_usd": 1.256}}, "$1.26"},
		{"a real zero is kept", map[string]any{"cost": map[string]any{"total_cost_usd": 0.0}}, "$0.00"},
		{"a string total is dropped", map[string]any{"cost": map[string]any{"total_cost_usd": "free"}}, ""},
		{"a bool total is dropped", map[string]any{"cost": map[string]any{"total_cost_usd": true}}, ""},
		{"a missing cost block is dropped", map[string]any{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := costSegment(tc.obj); got != tc.want {
				t.Errorf("costSegment = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDurationSegment(t *testing.T) {
	cases := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{"under an hour reads XmYYs", map[string]any{"cost": map[string]any{"total_duration_ms": 65000.0}}, "1m05s"},
		{"an hour and over reads XhYYm", map[string]any{"cost": map[string]any{"total_duration_ms": 3725000.0}}, "1h02m"},
		{"a negative duration clamps to zero", map[string]any{"cost": map[string]any{"total_duration_ms": -5000.0}}, "0m00s"},
		{"a non-numeric duration renders nothing", map[string]any{"cost": map[string]any{"total_duration_ms": "5s"}}, ""},
		{"a missing cost block renders nothing", map[string]any{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := durationSegment(tc.obj); got != tc.want {
				t.Errorf("durationSegment = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRateLimitSegment(t *testing.T) {
	both := map[string]any{"rate_limits": map[string]any{
		"five_hour": map[string]any{"used_percentage": 30.0, "resets_at": 1720003600.0},
		"seven_day": map[string]any{"used_percentage": 60.0, "resets_at": 1720300000.0},
	}}
	cases := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{"both windows join into one segment", both, "5h:30%(rst:1h0m) 7d:60%(50%elapsed)"},
		{
			name: "a window missing resets_at is skipped whole",
			obj: map[string]any{"rate_limits": map[string]any{
				"five_hour": map[string]any{"used_percentage": 30.0},
				"seven_day": map[string]any{"used_percentage": 60.0, "resets_at": 1720300000.0},
			}},
			want: "7d:60%(50%elapsed)",
		},
		{
			name: "a 5h window already past its reset drops the countdown",
			obj: map[string]any{"rate_limits": map[string]any{
				"five_hour": map[string]any{"used_percentage": 90.0, "resets_at": 1719999000.0},
			}},
			want: "5h:90%",
		},
		{name: "no rate_limits renders nothing", obj: map[string]any{}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rateLimitSegment(tc.obj, 1720000000); got != tc.want {
				t.Errorf("rateLimitSegment = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRlWindowFields(t *testing.T) {
	cases := []struct {
		name       string
		w          map[string]any
		wantUsed   float64
		wantResets float64
		wantOK     bool
	}{
		{"both numbers", map[string]any{"used_percentage": 30.0, "resets_at": 1720003600.0}, 30, 1720003600, true},
		{"a null resets_at", map[string]any{"used_percentage": 30.0, "resets_at": nil}, 0, 0, false},
		{"a string used_percentage", map[string]any{"used_percentage": "30", "resets_at": 1.0}, 0, 0, false},
		{"an empty window", map[string]any{}, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			used, resets, ok := rlWindowFields(tc.w)
			if used != tc.wantUsed || resets != tc.wantResets || ok != tc.wantOK {
				t.Errorf("rlWindowFields = (%v, %v, %v), want (%v, %v, %v)",
					used, resets, ok, tc.wantUsed, tc.wantResets, tc.wantOK)
			}
		})
	}
}

func TestCompactDuration(t *testing.T) {
	cases := []struct {
		seconds float64
		want    string
	}{
		{0, "0m"},
		{59, "0m"},
		{60, "1m"},
		{3599, "59m"},
		{3600, "1h0m"},
		{11220, "3h7m"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := compactDuration(tc.seconds); got != tc.want {
				t.Errorf("compactDuration(%v) = %q, want %q", tc.seconds, got, tc.want)
			}
		})
	}
}

func TestStatuslinePct(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		wantPct  float64
		wantHave bool
	}{
		{"a measured pct", `{"context_window":{"used_percentage":41.5}}`, 41.5, true},
		{"a negative pct clamps to 0", `{"context_window":{"used_percentage":-4}}`, 0, true},
		{"an over-range pct clamps to 100", `{"context_window":{"used_percentage":140}}`, 100, true},
		{"a null pct is not measured", `{"context_window":{"used_percentage":null}}`, 0, false},
		{"a bool pct is not measured", `{"context_window":{"used_percentage":true}}`, 0, false},
		{"a missing context_window is not measured", `{}`, 0, false},
		{"an unparseable payload is not measured", "not json", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pct, have := statuslinePct(tc.payload)
			if pct != tc.wantPct || have != tc.wantHave {
				t.Errorf("statuslinePct(%q) = (%v, %v), want (%v, %v)",
					tc.payload, pct, have, tc.wantPct, tc.wantHave)
			}
		})
	}
}

func TestReportStampPath(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"an id is lowercased", Config{Home: "/h", ID: "M-Kyle"}, "/h/m-kyle/context_report.stamp"},
		{"no id falls back to anon", Config{Home: "/h"}, "/h/anon/context_report.stamp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reportStampPath(tc.cfg); got != tc.want {
				t.Errorf("reportStampPath = %q, want %q", got, tc.want)
			}
			wantBackoff := strings.TrimSuffix(tc.want, "stamp") + "backoff"
			if got := reportBackoffPath(tc.cfg); got != wantBackoff {
				t.Errorf("reportBackoffPath = %q, want %q — the failure record is a sibling "+
					"of the stamp, never folded into it", got, wantBackoff)
			}
		})
	}
}

func TestReportThrottled(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "context_report.stamp")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Run("a stamp inside the window throttles", func(t *testing.T) {
		if !reportThrottled(write(t, "980"), 1000, 30) {
			t.Error("reportThrottled = false, want true — 20s < the 30s window")
		}
	})
	t.Run("a stamp exactly one window old does not throttle", func(t *testing.T) {
		if reportThrottled(write(t, "970"), 1000, 30) {
			t.Error("reportThrottled = true, want false — the window has closed")
		}
	})
	t.Run("a missing stamp does not throttle", func(t *testing.T) {
		if reportThrottled(filepath.Join(t.TempDir(), "nope"), 1000, 30) {
			t.Error("reportThrottled = true, want false — nothing was ever delivered")
		}
	})
	t.Run("an empty stamp reads as epoch and does not throttle", func(t *testing.T) {
		if reportThrottled(write(t, "  \n"), 1000, 30) {
			t.Error("reportThrottled = true, want false")
		}
	})
	t.Run("an unparseable stamp does not throttle", func(t *testing.T) {
		if reportThrottled(write(t, "yesterday"), 1000, 30) {
			t.Error("reportThrottled = true, want false — a junk scratch file must never " +
				"be able to silence a working reporter")
		}
	})
}

func TestReportBackoffSecs(t *testing.T) {
	cases := []struct {
		failures int
		want     float64
	}{
		{-1, 0},
		{0, 0},
		{1, 30},
		{2, 60},
		{3, 120},
		{4, 240},
		{5, 300},
		{6, 300},
		{40, 300},
	}
	for _, tc := range cases {
		if got := reportBackoffSecs(tc.failures); got != tc.want {
			t.Errorf("reportBackoffSecs(%d) = %v, want %v", tc.failures, got, tc.want)
		}
	}
}

func TestReportBackedOff(t *testing.T) {
	cases := []struct {
		name string
		st   reportBackoffState
		now  float64
		want bool
	}{
		{"a healthy record never suppresses", reportBackoffState{}, 1000, false},
		{"one failure suppresses for 30s", reportBackoffState{failures: 1, lastAttempt: 980}, 1000, true},
		{"one failure lets the 31st second through", reportBackoffState{failures: 1, lastAttempt: 969}, 1000, false},
		{"four failures suppress for 240s", reportBackoffState{failures: 4, lastAttempt: 900}, 1000, true},
		{"the cap lets an attempt through after 300s", reportBackoffState{failures: 40, lastAttempt: 600}, 1000, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reportBackedOff(tc.st, tc.now); got != tc.want {
				t.Errorf("reportBackedOff(%+v, %v) = %v, want %v", tc.st, tc.now, got, tc.want)
			}
		})
	}
}

func TestReadReportBackoff(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "context_report.backoff")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cases := []struct {
		name string
		body string
		want reportBackoffState
	}{
		{"a well-formed record", "3 1000.5", reportBackoffState{failures: 3, lastAttempt: 1000.5}},
		{"a truncated record reads healthy", "3", reportBackoffState{}},
		{"a non-numeric count reads healthy", "many 1000", reportBackoffState{}},
		{"a zero count reads healthy", "0 1000", reportBackoffState{}},
		{"an empty file reads healthy", "", reportBackoffState{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := readReportBackoff(write(t, tc.body)); got != tc.want {
				t.Errorf("readReportBackoff(%q) = %+v, want %+v", tc.body, got, tc.want)
			}
		})
	}
	t.Run("a missing file reads healthy", func(t *testing.T) {
		got := readReportBackoff(filepath.Join(t.TempDir(), "nope"))
		if got != (reportBackoffState{}) {
			t.Errorf("readReportBackoff = %+v, want the healthy zero value", got)
		}
	})
}

func TestWriteReportBackoff(t *testing.T) {
	t.Run("the record lands and reads back as it was written", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "deep", "context_report.backoff")
		writeReportBackoff(path, reportBackoffState{failures: 2, lastAttempt: 1000.25})

		if got := readFileString(t, path); got != "2 1000.25" {
			t.Errorf("file = %q, want %q", got, "2 1000.25")
		}
		want := reportBackoffState{failures: 2, lastAttempt: 1000.25}
		if got := readReportBackoff(path); got != want {
			t.Errorf("readReportBackoff = %+v, want %+v", got, want)
		}
	})

	t.Run("an unwritable parent is swallowed", func(t *testing.T) {
		blocked := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeReportBackoff(filepath.Join(blocked, "context_report.backoff"),
			reportBackoffState{failures: 1, lastAttempt: 1})
		if got := readFileString(t, blocked); got != "x" {
			t.Errorf("the blocking file = %q, want %q — nothing was written and nothing panicked",
				got, "x")
		}
	})
}

func TestClearReportBackoff(t *testing.T) {
	t.Run("a delivered burst drops the record", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "context_report.backoff")
		writeReportBackoff(path, reportBackoffState{failures: 5, lastAttempt: 1000})

		clearReportBackoff(path)

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("stat after clear = %v, want the file to be gone", err)
		}
		if got := readReportBackoff(path); got != (reportBackoffState{}) {
			t.Errorf("readReportBackoff = %+v, want the healthy zero value", got)
		}
	})

	t.Run("an absent record is the normal case", func(t *testing.T) {
		clearReportBackoff(filepath.Join(t.TempDir(), "nope"))
	})
}

func TestWriteStamp(t *testing.T) {
	t.Run("the stamp lands under a directory it creates", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kyle", "context_report.stamp")
		writeStamp(path, 1720000000.5)

		if got := readFileString(t, path); got != "1720000000.5" {
			t.Errorf("stamp = %q, want %q", got, "1720000000.5")
		}
		if reportThrottled(path, 1720000010, 30) != true {
			t.Error("the stamp it just wrote does not throttle the next tick")
		}
	})

	t.Run("an unwritable parent is swallowed", func(t *testing.T) {
		blocked := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeStamp(filepath.Join(blocked, "context_report.stamp"), 1000)
		if got := readFileString(t, blocked); got != "x" {
			t.Errorf("the blocking file = %q, want %q", got, "x")
		}
	})
}

func TestLocalHost(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"a remote warden names itself", map[string]string{"OC_HOST": "m-lab-1"}, "m-lab-1"},
		{"an unset OC_HOST is the server-self box", nil, "m-server-self"},
		{"an empty OC_HOST is the server-self box", map[string]string{"OC_HOST": ""}, "m-server-self"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := localHost(testEnv(tc.env)); got != tc.want {
				t.Errorf("localHost = %q, want %q", got, tc.want)
			}
		})
	}
}

// writeHomeClaudeJSON writes the ~/.claude.json candidate (the sibling of the
// ~/.claude/.claude.json one writeClaudeJSON writes) into an existing home.
func writeHomeClaudeJSON(t *testing.T, home, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadClaudeAccount(t *testing.T) {
	t.Run("the account uuid joins the org uuid", func(t *testing.T) {
		home := writeClaudeJSON(t, `{"oauthAccount":{"accountUuid":" au-1 ",`+
			`"organizationUuid":"org-1"}}`)
		if got := readClaudeAccount(testEnv(map[string]string{"HOME": home})); got != "au-1/org-1" {
			t.Errorf("readClaudeAccount = %q, want %q", got, "au-1/org-1")
		}
	})

	t.Run("no org yields a bare account uuid, never a dangling slash", func(t *testing.T) {
		home := writeClaudeJSON(t, `{"oauthAccount":{"accountUuid":"au-1"}}`)
		if got := readClaudeAccount(testEnv(map[string]string{"HOME": home})); got != "au-1" {
			t.Errorf("readClaudeAccount = %q, want %q", got, "au-1")
		}
	})

	t.Run("legacy config without an accountUuid falls back to userID", func(t *testing.T) {
		home := writeClaudeJSON(t, `{"userID":"legacy-1"}`)
		if got := readClaudeAccount(testEnv(map[string]string{"HOME": home})); got != "legacy-1" {
			t.Errorf("readClaudeAccount = %q, want %q", got, "legacy-1")
		}
	})

	t.Run("each dimension resolves independently across the two candidates", func(t *testing.T) {
		home := writeClaudeJSON(t, `{"oauthAccount":{"accountUuid":"au-1"}}`)
		writeHomeClaudeJSON(t, home, `{"oauthAccount":{"organizationUuid":"org-2"}}`)
		if got := readClaudeAccount(testEnv(map[string]string{"HOME": home})); got != "au-1/org-2" {
			t.Errorf("readClaudeAccount = %q, want %q — real installs split the two fields "+
				"across the two files", got, "au-1/org-2")
		}
	})

	t.Run("no identity anywhere reports nothing", func(t *testing.T) {
		if got := readClaudeAccount(testEnv(map[string]string{"HOME": t.TempDir()})); got != "" {
			t.Errorf("readClaudeAccount = %q, want empty", got)
		}
	})

	t.Run("an unparseable candidate is skipped, not fatal", func(t *testing.T) {
		home := writeClaudeJSON(t, `{{{ not json`)
		writeHomeClaudeJSON(t, home, `{"oauthAccount":{"accountUuid":"au-9","organizationUuid":"org-9"}}`)
		if got := readClaudeAccount(testEnv(map[string]string{"HOME": home})); got != "au-9/org-9" {
			t.Errorf("readClaudeAccount = %q, want %q", got, "au-9/org-9")
		}
	})
}

func TestReadClaudeAccountLabel(t *testing.T) {
	t.Run("the email carries the label and the org is parenthesised", func(t *testing.T) {
		home := writeClaudeJSON(t, `{"oauthAccount":{"emailAddress":"kyle@x.io",`+
			`"displayName":"Kyle","organizationName":"OffiCraft"}}`)
		want := "kyle@x.io(OffiCraft)"
		if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": home})); got != want {
			t.Errorf("readClaudeAccountLabel = %q, want %q", got, want)
		}
	})

	t.Run("no email falls back to the display name", func(t *testing.T) {
		home := writeClaudeJSON(t, `{"oauthAccount":{"displayName":"Kyle","organizationName":"OffiCraft"}}`)
		want := "Kyle(OffiCraft)"
		if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": home})); got != want {
			t.Errorf("readClaudeAccountLabel = %q, want %q", got, want)
		}
	})

	t.Run("no org drops the parenthesised suffix", func(t *testing.T) {
		home := writeClaudeJSON(t, `{"oauthAccount":{"emailAddress":"kyle@x.io"}}`)
		if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": home})); got != "kyle@x.io" {
			t.Errorf("readClaudeAccountLabel = %q, want %q", got, "kyle@x.io")
		}
	})

	t.Run("a non-string field never reaches an owner-facing label", func(t *testing.T) {
		home := writeClaudeJSON(t, `{"oauthAccount":{"emailAddress":null,"organizationName":7,`+
			`"displayName":"Kyle"}}`)
		if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": home})); got != "Kyle" {
			t.Errorf("readClaudeAccountLabel = %q, want %q", got, "Kyle")
		}
	})

	t.Run("each field resolves independently across the two candidates", func(t *testing.T) {
		home := writeClaudeJSON(t, `{"oauthAccount":{"emailAddress":"kyle@x.io"}}`)
		writeHomeClaudeJSON(t, home, `{"oauthAccount":{"organizationName":"OffiCraft"}}`)
		want := "kyle@x.io(OffiCraft)"
		if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": home})); got != want {
			t.Errorf("readClaudeAccountLabel = %q, want %q", got, want)
		}
	})

	t.Run("nothing readable reports nothing", func(t *testing.T) {
		home := writeClaudeJSON(t, `{"userID":"legacy-1"}`)
		if got := readClaudeAccountLabel(testEnv(map[string]string{"HOME": home})); got != "" {
			t.Errorf("readClaudeAccountLabel = %q, want empty — the caller then OMITS the "+
				"field rather than sending a fabricated blank", got)
		}
	})
}

func TestClaudeOrgUUID(t *testing.T) {
	cases := []struct {
		name string
		d    map[string]any
		want string
	}{
		{"a trimmed org uuid", map[string]any{"oauthAccount": map[string]any{"organizationUuid": " org-1 "}}, "org-1"},
		{"a blank org uuid", map[string]any{"oauthAccount": map[string]any{"organizationUuid": "  "}}, ""},
		{"a null org uuid", map[string]any{"oauthAccount": map[string]any{"organizationUuid": nil}}, ""},
		{"a non-string org uuid", map[string]any{"oauthAccount": map[string]any{"organizationUuid": 7.0}}, ""},
		{"no oauthAccount object", map[string]any{"userID": "u"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claudeOrgUUID(tc.d); got != tc.want {
				t.Errorf("claudeOrgUUID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClaudeAccountUUID(t *testing.T) {
	cases := []struct {
		name string
		d    map[string]any
		want string
	}{
		{"a trimmed account uuid", map[string]any{"oauthAccount": map[string]any{"accountUuid": " au-1 "}}, "au-1"},
		{"a non-string account uuid", map[string]any{"oauthAccount": map[string]any{"accountUuid": 7.0}}, ""},
		{"no oauthAccount object", map[string]any{"userID": "u"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claudeAccountUUID(tc.d); got != tc.want {
				t.Errorf("claudeAccountUUID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildTelemetry(t *testing.T) {
	t.Run("every measured piece comes back", func(t *testing.T) {
		transcript := todayTranscript(t)
		rl, cost, tokens := buildTelemetry(
			`{"rate_limits":{"five_hour":{"used_percentage":30,"resets_at":1720000000},` +
				`"seven_day":{"used_percentage":60,"resets_at":1720500000}},` +
				`"cost":{"total_cost_usd":1.25},"transcript_path":"` + transcript + `"}`)

		wantRL := &rateLimits{
			FiveHour: &rlWindow{UsedPercentage: 30.0, ResetsAt: 1720000000.0},
			SevenDay: &rlWindow{UsedPercentage: 60.0, ResetsAt: 1720500000.0},
		}
		if !reflect.DeepEqual(rl, wantRL) {
			t.Errorf("rate_limits = %+v, want %+v", rl, wantRL)
		}
		if cost == nil || *cost != 1.25 {
			t.Errorf("cost = %v, want 1.25", cost)
		}
		wantTokens := &tokensBody{Burned: 12, Output: 3, CacheRead: 11}
		if !reflect.DeepEqual(tokens, wantTokens) {
			t.Errorf("tokens = %+v, want %+v", tokens, wantTokens)
		}
	})

	t.Run("a real zero cost is kept while an unmeasured source is omitted", func(t *testing.T) {
		rl, cost, tokens := buildTelemetry(`{"cost":{"total_cost_usd":0}}`)
		if rl != nil {
			t.Errorf("rate_limits = %+v, want nil", rl)
		}
		if cost == nil || *cost != 0 {
			t.Errorf("cost = %v, want a kept 0", cost)
		}
		if tokens != nil {
			t.Errorf("tokens = %+v, want nil", tokens)
		}
	})

	t.Run("one present window is enough to report rate_limits", func(t *testing.T) {
		rl, _, _ := buildTelemetry(`{"rate_limits":{"seven_day":{"used_percentage":null,"resets_at":null}}}`)
		wantRL := &rateLimits{SevenDay: &rlWindow{UsedPercentage: nil, ResetsAt: nil}}
		if !reflect.DeepEqual(rl, wantRL) {
			t.Errorf("rate_limits = %+v, want %+v — a null is passed through as "+
				"\"not measured\", never dropped into a fabricated 0", rl, wantRL)
		}
	})

	t.Run("a transcript path that does not exist omits tokens", func(t *testing.T) {
		_, _, tokens := buildTelemetry(`{"transcript_path":"` +
			filepath.Join(t.TempDir(), "nope.jsonl") + `"}`)
		if tokens != nil {
			t.Errorf("tokens = %+v, want nil", tokens)
		}
	})

	t.Run("a junk payload measures nothing", func(t *testing.T) {
		rl, cost, tokens := buildTelemetry("not json")
		if rl != nil || cost != nil || tokens != nil {
			t.Errorf("buildTelemetry = (%v, %v, %v), want all nil", rl, cost, tokens)
		}
	})
}

func TestParseTranscriptTokens(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	row := func(ts string, in, out, create, read int) string {
		return `{"type":"assistant","timestamp":"` + ts + `","message":{"usage":{` +
			`"input_tokens":` + strconv.Itoa(in) + `,"output_tokens":` + strconv.Itoa(out) +
			`,"cache_creation_input_tokens":` + strconv.Itoa(create) +
			`,"cache_read_input_tokens":` + strconv.Itoa(read) + `}}}`
	}
	write := func(t *testing.T, lines ...string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "transcript.jsonl")
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("today's assistant rows are summed and burned folds in cache creation", func(t *testing.T) {
		path := write(t,
			row(today+"T10:00:00Z", 7, 3, 5, 11),
			row(today+"T11:00:00Z", 1, 2, 3, 4))
		want := &tokensBody{Burned: 16, Output: 5, CacheRead: 15}
		if got := parseTranscriptTokens(path); !reflect.DeepEqual(got, want) {
			t.Errorf("parseTranscriptTokens = %+v, want %+v", got, want)
		}
	})

	t.Run("rows from another day, other roles and junk lines are all skipped", func(t *testing.T) {
		path := write(t,
			row("2020-01-01T10:00:00Z", 100, 100, 100, 100),
			`{"type":"user","timestamp":"`+today+`T10:00:00Z","message":{"usage":{"input_tokens":50}}}`,
			`not json at all`,
			`{"type":"assistant","timestamp":"`+today+`T10:00:00Z","message":{"usage":{}}}`,
			row(today+"T12:00:00Z", 2, 1, 0, 0))
		want := &tokensBody{Burned: 2, Output: 1, CacheRead: 0}
		if got := parseTranscriptTokens(path); !reflect.DeepEqual(got, want) {
			t.Errorf("parseTranscriptTokens = %+v, want %+v", got, want)
		}
	})

	t.Run("no row from today measures nothing", func(t *testing.T) {
		path := write(t, row("2020-01-01T10:00:00Z", 9, 9, 9, 9))
		if got := parseTranscriptTokens(path); got != nil {
			t.Errorf("parseTranscriptTokens = %+v, want nil — an unmeasured session must "+
				"not report a fabricated 0", got)
		}
	})

	t.Run("an unreadable transcript measures nothing", func(t *testing.T) {
		if got := parseTranscriptTokens(filepath.Join(t.TempDir(), "nope.jsonl")); got != nil {
			t.Errorf("parseTranscriptTokens = %+v, want nil", got)
		}
	})
}

func TestIntOrZero(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		{"a whole number", 7.0, 7},
		{"a fraction truncates toward zero", 7.9, 7},
		{"a negative fraction truncates toward zero", -7.9, -7},
		{"nil", nil, 0},
		{"a string", "7", 0},
		{"a bool", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := intOrZero(tc.in); got != tc.want {
				t.Errorf("intOrZero(%v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
