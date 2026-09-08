// Skeleton generated from server/ocserverd/api_monitoring.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHandleIngestAgentContextApiAgentContextPost(t *testing.T) {
	t.Run("a numeric context_pct answers a receipt naming the caller, and the gauge is readable afterwards", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":42.5}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"agent_id": "mira",
			"ts":       apiAnyNumber,
		})

		status, view := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("monitoring: want 200, got %d (%v)", status, view)
		}
		sessions := apiTestSessionsByID(t, view, "mira", "kip", "m-server-self")
		want := apiTestMonitoringSession("mira", "Mira", "Assistant")
		want["context_pct"] = 42.5
		apiWantBody(t, sessions["mira"], want)
	})

	t.Run("a report carrying a compaction count answers the same two-field receipt, and both numbers are readable afterwards", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/agent/context", agent,
			`{"context_pct":10,"compaction_count":3,"rate_limits":{"five_hour":{"used_pct":12}}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"agent_id": "mira",
			"ts":       apiAnyNumber,
		})

		status, view := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("monitoring: want 200, got %d (%v)", status, view)
		}
		sessions := apiTestSessionsByID(t, view, "mira", "kip", "m-server-self")
		want := apiTestMonitoringSession("mira", "Mira", "Assistant")
		want["context_pct"] = 10
		want["compaction_count"] = 3
		apiWantBody(t, sessions["mira"], want)
	})

	t.Run("a context_pct that is not a number answers 400", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":"half"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "context_pct must be a number")
	})

	t.Run("a negative compaction_count answers 400", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/agent/context", agent,
			`{"context_pct":10,"compaction_count":-1}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "compaction_count must be a non-negative integer")
	})

	t.Run("a fractional compaction_count answers 400", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/agent/context", agent,
			`{"context_pct":10,"compaction_count":1.5}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "compaction_count must be a non-negative integer")
	})

	t.Run("a body that is not JSON answers 422", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `not json`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character 'o' in literal null (expecting 'u')")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/agent/context", "", `{"context_pct":10}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an accepted report fans one context signal to the owner cockpit and to no agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")
		reporter := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":42.5}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}

		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "context",
			"op":    "signal",
			"data": map[string]any{
				"entity":  "context",
				"key":     "mira",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "mira",
		})
		reporter.wantFrames()
	})

	t.Run("a refused report fans nothing", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":"half"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "context_pct must be a number")
		dashboard.wantFrames()
	})
}

func TestTeleNum(t *testing.T) {
	t.Skip("TODO: teleNum shapes a telemetry numeric: bool / non-number / negative sentinel (-1 = 未量到) → nil, NEVER a fabricated 0 (handlers._tele_num).")
}

func TestTeleBool(t *testing.T) {
	t.Skip("TODO: teleBool shapes a telemetry boolean: absent / non-bool stays honest-nil.")
}

func TestHardwareInvalidKeys(t *testing.T) {
	t.Skip("TODO: hardwareInvalidKeys names the declared hardware keys that are PRESENT in this sample but carry a value the reader cannot use — sorted, empty when the sample is clean.")
}

func TestCommandResultAtEpoch(t *testing.T) {
	t.Skip("TODO: commandResultAtEpoch parses a command_result \"at\" (RFC3339 from the warden; a bare epoch number accepted for robustness; garbage → 0.0 so a bad timestamp can never shortcut presence).")
}

func TestIsStopNoopReceipt(t *testing.T) {
	t.Skip("TODO: isStopNoopReceipt reports whether a command_result receipt is a no-op stop: an OK stop whose reason carries the no_such_session code.")
}

func TestSupersededDispatchClue(t *testing.T) {
	t.Skip("TODO: supersededDispatchClue returns a one-line carry-forward of the member's CURRENT last_op_reason when that reason is a dispatch-level diagnosis (the \"nothing ever came back\" story) about to be replaced by an execution receipt (the \"the machine acted and here is what happened\" story).")
}

func TestStringOf(t *testing.T) {
	t.Skip("TODO: stringOf / boolPtrOf are the two type assertions the receipt reads use, named once so the pre-routing peek at rpc/ok/reason cannot drift from the per-fold reads further down (they must agree — the peek decides whether the folds' own isStopNoopReceipt verdict is about to fire).")
}

func TestBoolPtrOf(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestFoldCommandResult(t *testing.T) {
	t.Skip("TODO: foldCommandResult folds ONE warden command_result receipt onto the addressed member's last_op* fields (handlers._fold_command_result).")
}

func TestFoldWorkerCommandResult(t *testing.T) {
	t.Skip("TODO: foldWorkerCommandResult folds ONE warden worker command_result receipt (worker_start / worker_stop, T-9ccf) onto the addressed outsource_worker row's last_op* fields — the worker twin of foldCommandResult's member fold, reusing the SAME clamps and three-valued ok.")
}

func TestHandleIngestTelemetryApiMonitoringTelemetryPost(t *testing.T) {
	t.Run("a full warden report answers a three-field receipt, and the blocks it carried show up on the monitoring view", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden, `{
			"rate_limits":{"five_hour":{"used_pct":5}},
			"tokens":{"input":10},
			"hardware":{"cpu_pct":12.5,"ram_pct":40,"ac_power":true},
			"binaries":{"ocagent":"1.0.0"},
			"claude":{"version":"2.0.0"},
			"runtimes":{"claude":{"installed":true,"logged_in":true,"version":"2.0.0"}},
			"runtime":"claude",
			"cost":1.25,
			"effort":"high",
			"model":"opus",
			"self_update":{"binary":"ocwarden"},
			"warden_shape":"anchor",
			"cutover_effect":"effective"
		}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"agent_id": "m-server-self",
			"machine":  "m-server-self",
			"ts":       apiAnyNumber,
		})

		status, view := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("monitoring: want 200, got %d (%v)", status, view)
		}
		sessions := apiTestSessionsByID(t, view, "mira", "kip", "m-server-self")
		wantSession := apiTestMonitoringSession("m-server-self", "伺服器這一台", "")
		wantSession["machine"] = "m-server-self"
		wantSession["runtime"] = "claude"
		wantSession["model"] = "opus"
		wantSession["effort"] = "high"
		wantSession["cost"] = 1.25
		wantSession["tokens"] = map[string]any{"input": 10}
		apiWantBody(t, sessions["m-server-self"], wantSession)
		wantMachine := apiTestMonitoringMachine()
		wantMachine["cpu_pct"] = 12.5
		wantMachine["ram_pct"] = 40
		wantMachine["ac_power"] = true
		wantMachine["bin_status"] = "stale"
		wantMachine["claude_version"] = "2.0.0"
		wantMachine["runtime_capabilities"] = map[string]any{
			"claude": map[string]any{"installed": true, "logged_in": true, "version": "2.0.0"},
		}
		wantMachine["hardware_ts"] = apiAnyNumber
		wantMachine["hardware_stale"] = false
		wantMachine["runtime_capabilities_ts"] = apiAnyNumber
		wantMachine["runtime_capabilities_stale"] = false
		wantMachine["warden_shape"] = "anchor"
		wantMachine["cutover_effect"] = "effective"
		apiWantBody(t, view, map[string]any{
			"sessions": view["sessions"],
			"machines": []any{wantMachine},
			"accounts": view["accounts"],
		})
	})

	t.Run("a report naming its own machine while the token carries no claim answers 200 attributing that machine", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"tokens":{"input":1},"machine":"box-nine"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"agent_id": "mira",
			"machine":  "box-nine",
			"ts":       apiAnyNumber,
		})
	})

	t.Run("a partial report leaves the blocks it does not carry alone, which the monitoring view is the only place to see", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"effort":"high"}`)

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"agent_id": "mira",
			"machine":  nil,
			"ts":       apiAnyNumber,
		})

		status, view := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("monitoring: want 200, got %d (%v)", status, view)
		}
		sessions := apiTestSessionsByID(t, view, "mira", "kip", "m-server-self")
		want := apiTestMonitoringSession("mira", "Mira", "Assistant")
		want["effort"] = "high"
		want["cost"] = 2
		apiWantBody(t, sessions["mira"], want)
	})

	t.Run("a command_result receipt answers the bounded receipt and folds onto the addressed member's last_op fields", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"command_result":{"rpc":"start","member_id":"mira","ok":true,"reason":"started","at":"2026-01-01T00:00:00Z"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"agent_id": "m-server-self",
			"machine":  "m-server-self",
			"ts":       apiAnyNumber,
		})

		status, member := apiJSON(t, h, "GET", "/api/members/mira", owner, "")
		if status != 200 {
			t.Fatalf("member read back: want 200, got %d (%v)", status, member)
		}
		if member["last_op"] != "start" || member["last_op_ok"] != true ||
			member["last_op_reason"] != "started" || member["last_op_at"] != float64(1767225600) {
			t.Fatalf("the command_result did not fold onto the member: %v", member)
		}
	})

	t.Run("a body carrying none of the declared blocks answers 400 naming them all", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "rate_limits, tokens, hardware, binaries, claude, cost, effort, runtime, runtimes, "+
			"self_update, command_result, warden_shape or cutover_effect is required")
	})

	t.Run("a block that is not an object answers 400 naming the block", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		for _, c := range []struct{ body, message string }{
			{`{"rate_limits":1}`, "rate_limits must be an object"},
			{`{"tokens":1}`, "tokens must be an object"},
			{`{"binaries":1}`, "binaries must be an object"},
			{`{"self_update":1}`, "self_update must be an object"},
			{`{"command_result":1}`, "command_result must be an object"},
		} {
			status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, c.body)
			if status != 400 {
				t.Fatalf("%s: want 400, got %d (%v)", c.body, status, data)
			}
			apiWantError(t, data, "validation_error", c.message)
		}
	})

	t.Run("a runtimes block outside the closed vocabulary answers 400", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		for _, c := range []struct{ body, message string }{
			{`{"runtimes":{"gemini":{}}}`, "runtimes keys must be 'claude' or 'codex'"},
			{`{"runtimes":{"claude":1}}`, "runtimes.claude must be an object"},
			{`{"runtimes":{"claude":{"installed":"yes"}}}`, "runtimes.claude.installed must be a boolean"},
			{`{"runtimes":{"claude":{"logged_in":"yes"}}}`, "runtimes.claude.logged_in must be a boolean or null"},
			{`{"runtimes":{"claude":{"version":1}}}`, "runtimes.claude.version must be a string or null"},
		} {
			status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, c.body)
			if status != 400 {
				t.Fatalf("%s: want 400, got %d (%v)", c.body, status, data)
			}
			apiWantError(t, data, "validation_error", c.message)
		}
	})

	t.Run("a runtimes block carrying explicit nulls is accepted, and the monitoring view reports only the fields that were not null", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"runtimes":{"codex":{"installed":false,"logged_in":null,"version":null}}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"agent_id": "m-server-self",
			"machine":  "m-server-self",
			"ts":       apiAnyNumber,
		})

		status, view := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("monitoring: want 200, got %d (%v)", status, view)
		}
		wantMachine := apiTestMonitoringMachine()
		wantMachine["runtime_capabilities"] = map[string]any{
			"codex": map[string]any{"installed": false},
		}
		wantMachine["runtime_capabilities_ts"] = apiAnyNumber
		wantMachine["runtime_capabilities_stale"] = false
		apiWantBody(t, view, map[string]any{
			"sessions": view["sessions"],
			"machines": []any{wantMachine},
			"accounts": view["accounts"],
		})
	})

	t.Run("a scalar outside its closed vocabulary answers 400 naming the field", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		for _, c := range []struct{ body, message string }{
			{`{"runtime":"gemini"}`, "runtime must be 'claude' or 'codex'"},
			{`{"runtime":1}`, "runtime must be 'claude' or 'codex'"},
			{`{"warden_shape":"nope"}`, "warden_shape must be 'anchor', 'legacy' or 'unknown'"},
			{`{"cutover_effect":"nope"}`, "cutover_effect must be 'effective', 'not_effective' or 'unproven'"},
			{`{"cost":"free"}`, "cost must be a number"},
			{`{"effort":1}`, "effort must be a string"},
			{`{"tokens":{},"model":1}`, "model must be a string"},
		} {
			status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, c.body)
			if status != 400 {
				t.Fatalf("%s: want 400, got %d (%v)", c.body, status, data)
			}
			apiWantError(t, data, "validation_error", c.message)
		}
	})

	t.Run("a body that is not JSON answers 422", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `not json`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character 'o' in literal null (expecting 'u')")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", "", `{"tokens":{}}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("a report carrying no launch fact fans one monitoring signal to the owner cockpit and to no agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")
		reporter := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"cost":2}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}

		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "monitoring",
			"op":    "signal",
			"data": map[string]any{
				"entity":  "monitoring",
				"key":     "mira",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "mira",
		})
		reporter.wantFrames()
	})

	t.Run("a report whose launch facts move the roster row fans the member patch before the monitoring signal", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		dashboard := apiTestListen(t, api, "")
		reporter := apiTestListen(t, api, "m-server-self")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"tokens":{"input":10},"runtime":"claude","effort":"high","model":"opus"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}

		memberFrame := map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::m-server-self",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":            "m-server-self",
					"name":          "伺服器這一台",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "m-server-self",
		}
		dashboard.wantFrames(memberFrame, map[string]any{
			"seq":   2,
			"topic": "monitoring",
			"op":    "signal",
			"data": map[string]any{
				"entity":  "monitoring",
				"key":     "m-server-self",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "m-server-self",
		})
		reporter.wantFrames(memberFrame)
	})

	t.Run("a refused report fans nothing", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{"runtime":"gemini"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "runtime must be 'claude' or 'codex'")
		dashboard.wantFrames()
	})
}

func TestStampReportedLaunchFacts(t *testing.T) {
	t.Skip("TODO: stampReportedLaunchFacts persists a session's live self-reported model, runtime and effort onto the caller's OWN roster row (identity-from-token: agentID is the verified sub).")
}

func TestOrUnknown(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestEntryStr(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestEntryObj(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestEntryNum(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestRuntimeCapabilitiesStampOf(t *testing.T) {
	t.Skip("TODO: runtimeCapabilitiesStampOf reads WHEN the entry's capability probe was taken.")
}

func TestRateLimitStampOf(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestUsableRateLimitWindow(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestHardwareStampOf(t *testing.T) {
	t.Skip("TODO: hardwareStampOf reads WHEN the entry's hardware sample was taken.")
}

// apiTestMonitoringSession is a session row with nothing observed on it yet —
// every field of monitoringSessionDTO written out, so a scenario only names
// what its own reports moved.
func apiTestMonitoringSession(id, name, role string) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        name,
		"role":        role,
		"runtime":     "",
		"model":       "",
		"effort":      "",
		"machine":     "",
		"account":     "",
		"presence":    "offline",
		"context_pct": nil,
		"cost":        nil,
		"banked_cost": nil,
		"tokens":      nil,
	}
}

// apiTestMonitoringMachine is the server-self warden's row with nothing
// reported — every field of monitoringMachineDTO written out.
func apiTestMonitoringMachine() map[string]any {
	return map[string]any{
		"machine":                    "m-server-self",
		"display_name":               "m-server-self",
		"agents":                     1,
		"cpu_pct":                    nil,
		"ram_pct":                    nil,
		"battery_pct":                nil,
		"ac_power":                   nil,
		"accounts":                   []any{},
		"bin_status":                 nil,
		"claude_version":             nil,
		"claude_cred_source":         nil,
		"claude_sub_readable":        nil,
		"runtime_capabilities":       map[string]any{},
		"hardware_ts":                nil,
		"hardware_stale":             nil,
		"hardware_invalid":           []any{},
		"runtime_capabilities_ts":    nil,
		"runtime_capabilities_stale": nil,
		"warden_shape":               nil,
		"cutover_effect":             nil,
	}
}

// apiTestSessionsByID indexes the session list by id and asserts the id SET is
// exactly what the caller names, so a row that appears or vanishes is caught
// before any field is read.
func apiTestSessionsByID(t *testing.T, data map[string]any, ids ...string) map[string]map[string]any {
	t.Helper()
	list, ok := data["sessions"].([]any)
	if !ok {
		t.Fatalf("sessions: want an array, got %#v", data["sessions"])
	}
	byID := map[string]map[string]any{}
	got := []string{}
	for _, raw := range list {
		row, isObj := raw.(map[string]any)
		if !isObj {
			t.Fatalf("sessions: want objects, got %#v", raw)
		}
		id, _ := row["id"].(string)
		byID[id] = row
		got = append(got, id)
	}
	sort.Strings(got)
	want := append([]string{}, ids...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sessions: want ids %q, got %q", strings.Join(want, ","), strings.Join(got, ","))
	}
	return byID
}

func TestHandleGetMonitoringApiMonitoringGet(t *testing.T) {
	t.Run("a seeded roster answers 200 with a session row per member and a machine row per warden", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		sessions := apiTestSessionsByID(t, data, "mira", "kip", "m-server-self")
		apiWantBody(t, sessions["mira"], apiTestMonitoringSession("mira", "Mira", "Assistant"))
		apiWantBody(t, sessions["kip"], apiTestMonitoringSession("kip", "Kip", ""))
		wardenSession := apiTestMonitoringSession("m-server-self", "伺服器這一台", "")
		wardenSession["machine"] = "m-server-self"
		apiWantBody(t, sessions["m-server-self"], wardenSession)
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": []any{apiTestMonitoringMachine()},
			"accounts": []any{},
		})
	})

	t.Run("a reported context gauge answers 200 carrying it on that member's session row", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		apiJSON(t, h, "POST", "/api/agent/context", agent, `{"context_pct":37,"compaction_count":2}`)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		sessions := apiTestSessionsByID(t, data, "mira", "kip", "m-server-self")
		want := apiTestMonitoringSession("mira", "Mira", "Assistant")
		want["context_pct"] = 37
		want["compaction_count"] = 2
		apiWantBody(t, sessions["mira"], want)
		apiWantBody(t, sessions["kip"], apiTestMonitoringSession("kip", "Kip", ""))
	})

	t.Run("reported launch facts answer 200 on that member's session row", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"model":"opus","runtime":"codex","effort":"high","tokens":{"input":1}}`)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		sessions := apiTestSessionsByID(t, data, "mira", "kip", "m-server-self")
		want := apiTestMonitoringSession("mira", "Mira", "Assistant")
		want["model"] = "opus"
		want["runtime"] = "codex"
		want["effort"] = "high"
		want["tokens"] = map[string]any{"input": 1}
		apiWantBody(t, sessions["mira"], want)
	})

	t.Run("a fresh hardware sample answers 200 on that machine's row", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"hardware":{"cpu_pct":12.5,"ram_pct":41,"battery_pct":"most of it","ac_power":true}}`)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		want := apiTestMonitoringMachine()
		want["cpu_pct"] = 12.5
		want["ram_pct"] = 41
		want["ac_power"] = true
		want["hardware_ts"] = apiAnyNumber
		want["hardware_stale"] = false
		want["hardware_invalid"] = []any{"battery_pct"}
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": []any{want},
			"accounts": []any{},
		})
	})

	t.Run("a reported account answers 200 with an account row naming the machine it burns on", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"runtime":"claude","account":"eva-m5-claude","account_label":"eva@example.com","cost":3.5,`+
				`"rate_limits":{"five_hour":{"used_pct":10,"resets_at":`+strconv.FormatInt(time.Now().Unix()+3600, 10)+`}}}`)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		accounts, _ := data["accounts"].([]any)
		if len(accounts) != 1 {
			t.Fatalf("accounts: want 1 row, got %v", data["accounts"])
		}
		account, _ := accounts[0].(map[string]any)
		window, _ := account["five_hour"].(map[string]any)
		if window == nil {
			t.Fatalf("accounts[0].five_hour: want the reported window, got %v", account["five_hour"])
		}
		apiWantBody(t, account, map[string]any{
			"account":       "eva-m5-claude",
			"account_label": "eva@example.com",
			"display_name":  "eva@example.com",
			"machine":       "m-server-self",
			"cost":          3.5,
			"five_hour":     account["five_hour"],
			"seven_day":     nil,
		})
		sessions := apiTestSessionsByID(t, data, "mira", "kip", "m-server-self")
		wardenSession := apiTestMonitoringSession("m-server-self", "伺服器這一台", "")
		wardenSession["machine"] = "m-server-self"
		wardenSession["runtime"] = "claude"
		wardenSession["account"] = "eva@example.com"
		wardenSession["cost"] = 3.5
		apiWantBody(t, sessions["m-server-self"], wardenSession)
	})

	t.Run("an account with no reported label answers 200 with the raw key as its display name", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"runtime":"claude","account":"eva-m5-claude","tokens":{"input":1}}`)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": data["machines"],
			"accounts": []any{map[string]any{
				"account":      "eva-m5-claude",
				"display_name": "eva-m5-claude",
				"machine":      "m-server-self",
				"cost":         nil,
				"five_hour":    nil,
				"seven_day":    nil,
			}},
		})
	})

	t.Run("a live contractor answers 200 with one session row and one more agent on its machine", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutMember(Member{
			ID:               "ow-1",
			Name:             "Contractor One",
			Codename:         "Contractor One",
			Kind:             KindOutsource,
			RoleKey:          "engineer",
			RosterStatus:     RosterStatusActive,
			ActivatedTS:      1,
			DesiredMachineID: "m-server-self",
		}); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		worker := apiTestAgentToken(t, api, "ow-1", "m-server-self")
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", worker, `{"tokens":{"input":4}}`)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		sessions := apiTestSessionsByID(t, data, "mira", "kip", "m-server-self", "ow-1")
		want := apiTestMonitoringSession("ow-1", "Contractor One", "")
		want["machine"] = "m-server-self"
		want["runtime"] = ""
		want["tokens"] = map[string]any{"input": 4}
		apiWantBody(t, sessions["ow-1"], want)

		machine := apiTestMonitoringMachine()
		machine["agents"] = 2
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": []any{machine},
			"accounts": []any{},
		})
	})

	t.Run("a released contractor answers 200 with the session list still naming only the live roster", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		if err := d.PutMember(Member{
			ID:           "ow-2",
			Name:         "Contractor Two",
			Codename:     "Contractor Two",
			Kind:         KindOutsource,
			RoleKey:      "engineer",
			RosterStatus: RosterStatusRemoved,
			ActivatedTS:  1,
		}); err != nil {
			t.Fatalf("PutMember: %v", err)
		}

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		sessions := apiTestSessionsByID(t, data, "mira", "kip", "m-server-self")
		apiWantBody(t, sessions["mira"], apiTestMonitoringSession("mira", "Mira", "Assistant"))
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": []any{apiTestMonitoringMachine()},
			"accounts": []any{},
		})
	})

	t.Run("a machine alias answers 200 with the alias on the machine and account rows", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutMachineAlias(MachineAlias{MachineID: "m-server-self", DisplayName: "伺服器這一台"}); err != nil {
			t.Fatalf("PutMachineAlias: %v", err)
		}
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"runtimes":{"claude":{"installed":true,"logged_in":true,"version":"2.0.0"}}}`)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		want := apiTestMonitoringMachine()
		want["display_name"] = "伺服器這一台"
		want["runtime_capabilities"] = map[string]any{
			"claude": map[string]any{"installed": true, "logged_in": true, "version": "2.0.0"},
		}
		want["runtime_capabilities_ts"] = apiAnyNumber
		want["runtime_capabilities_stale"] = false
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": []any{want},
			"accounts": []any{},
		})
		sessions := apiTestSessionsByID(t, data, "mira", "kip", "m-server-self")
		wardenSession := apiTestMonitoringSession("m-server-self", "伺服器這一台", "")
		wardenSession["machine"] = "伺服器這一台"
		apiWantBody(t, sessions["m-server-self"], wardenSession)
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestAnyOrNil(t *testing.T) {
	t.Skip("TODO: anyOrNil widens a possibly-nil typed map to `any` so ShapeWindows sees a true nil (a typed nil inside any is not nil to a type switch on map).")
}
