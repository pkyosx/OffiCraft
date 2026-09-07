package main

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestCodexUplinkBodies drives the codex sidecar's real producers against a test
// server and pins every body they put on the wire, both against a hand-written
// expectation and against the schema the frozen spec declares for the ROUTE it
// was sent to. The server decodes these routes with DisallowUnknownFields, so one
// undeclared key rejects the whole body while the sidecar reports nothing wrong.
func TestCodexUplinkBodies(t *testing.T) {
	type capture struct {
		run   string
		route string
		body  map[string]any
	}
	var sent []capture
	run := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("%s body is not a JSON object: %v", r.URL.Path, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		sent = append(sent, capture{run: run, route: r.URL.Path, body: body})
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	session := &codexSession{base: srv.URL, token: "wire-token", account: "codex:wire",
		model: "gpt-5-codex", effort: "high"}

	driven := map[string]int{}
	drive := func(name string, produce func()) {
		before := len(sent)
		run = name
		produce()
		if len(sent) == before {
			t.Fatalf("%s put no body on the wire, so nothing about it was compared", name)
		}
		for _, one := range sent[before:] {
			driven[name+" → "+one.route]++
		}
	}

	drive("identity", session.reportIdentity)
	drive("context-and-tokens", func() {
		session.reportTokenUsage(map[string]any{"tokenUsage": map[string]any{
			"modelContextWindow": float64(100),
			"last":               map[string]any{"totalTokens": float64(41)},
			"total": map[string]any{
				"inputTokens": float64(1), "outputTokens": float64(2),
				"cachedInputTokens": float64(3), "reasoningOutputTokens": float64(4),
				"totalTokens": float64(10),
			},
		}})
	})
	drive("rate-limits", func() {
		session.reportRateLimits(map[string]any{
			"primary": map[string]any{"windowDurationMins": float64(300),
				"usedPercent": float64(1), "resetsAt": float64(1720000000)},
			"secondary": map[string]any{"windowDurationMins": float64(10080),
				"usedPercent": float64(0), "resetsAt": float64(1720500000)},
		})
	})

	want := []capture{
		{
			run:   "identity",
			route: "/api/monitoring/telemetry",
			body: map[string]any{
				"runtime": "codex", "account": "codex:wire", "account_label": "ChatGPT",
			},
		},
		{
			run:   "context-and-tokens",
			route: "/api/agent/context",
			body:  map[string]any{"context_pct": float64(41), "compaction_count": float64(0)},
		},
		{
			run:   "context-and-tokens",
			route: "/api/monitoring/telemetry",
			body: map[string]any{
				"runtime": "codex", "account": "codex:wire", "account_label": "ChatGPT",
				"effort": "high", "model": "gpt-5-codex",
				"tokens": map[string]any{
					"inputTokens": float64(1), "cachedInputTokens": float64(3),
					"outputTokens": float64(2), "reasoningOutputTokens": float64(4),
					"totalTokens": float64(10),
				},
			},
		},
		{
			run:   "rate-limits",
			route: "/api/monitoring/telemetry",
			body: map[string]any{
				"runtime": "codex", "account": "codex:wire", "account_label": "ChatGPT",
				"rate_limits": map[string]any{
					"five_hour": map[string]any{
						"used_percentage": float64(1), "resets_at": float64(1720000000)},
					"seven_day": map[string]any{
						"used_percentage": float64(0), "resets_at": float64(1720500000)},
				},
			},
		},
	}
	if !reflect.DeepEqual(sent, want) {
		t.Errorf("the sidecar put\n  %#v\non the wire, want\n  %#v", sent, want)
	}

	for _, one := range sent {
		declared := frozenRequestSchema(t, "post", one.route)
		if extra := undeclaredPayloadKeys(one.body, declared); len(extra) > 0 {
			t.Errorf("POST %s (%s) carries key(s) the frozen spec does not declare: %v; body=%v",
				one.route, one.run, extra, one.body)
		}
		if missing := missingRequiredKeys(one.body, declared); len(missing) > 0 {
			t.Errorf("POST %s (%s) omits key(s) the frozen spec requires: %v; body=%v",
				one.route, one.run, missing, one.body)
		}
		if bad := mistypedPayloadValues(one.body, declared); len(bad) > 0 {
			t.Errorf("POST %s (%s) sends declared key(s) with the wrong wire type: %v; body=%v",
				one.route, one.run, bad, one.body)
		}
	}

	if committed := manifestUplinkPaths(t, "cli/ocwarden/codex_uplink_wire_test.go"); !maps.Equal(driven, committed) {
		t.Errorf("cli/uplinks.json commits %v to this wire test, but the producers put %v "+
			"on the wire", committed, driven)
	}
}
