// Skeleton generated from server/ocserverd/api_monitoring.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"reflect"
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
	cases := []struct {
		name    string
		input   any
		want    float64
		present bool
	}{
		{name: "positive", input: float64(12.5), want: 12.5, present: true},
		{name: "zero", input: float64(0), want: 0, present: true},
		{name: "negative sentinel", input: float64(-1)},
		{name: "bool", input: true},
		{name: "string", input: "12.5"},
		{name: "nil", input: nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := teleNum(tc.input)
			if !tc.present {
				if got != nil {
					t.Fatalf("teleNum(%#v) = %v, want nil", tc.input, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("teleNum(%#v) = nil, want %v", tc.input, tc.want)
			}
			if *got != tc.want {
				t.Fatalf("teleNum(%#v) = %v, want %v", tc.input, *got, tc.want)
			}
		})
	}
}

func TestTeleBool(t *testing.T) {
	cases := []struct {
		name    string
		input   any
		want    bool
		present bool
	}{
		{name: "true", input: true, want: true, present: true},
		{name: "false", input: false, want: false, present: true},
		{name: "string", input: "true"},
		{name: "number", input: float64(1)},
		{name: "nil", input: nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := teleBool(tc.input)
			if !tc.present {
				if got != nil {
					t.Fatalf("teleBool(%#v) = %v, want nil", tc.input, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("teleBool(%#v) = nil, want %v", tc.input, tc.want)
			}
			if *got != tc.want {
				t.Fatalf("teleBool(%#v) = %v, want %v", tc.input, *got, tc.want)
			}
		})
	}
}

func TestHardwareInvalidKeys(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  []string
	}{
		{name: "clean", input: map[string]any{
			"cpu_pct":     float64(12.5),
			"ram_pct":     float64(41),
			"battery_pct": float64(-1),
			"ac_power":    true,
		}, want: []string{}},
		{name: "wrong declared types with unknown and null values", input: map[string]any{
			"cpu_pct":     "12.5",
			"ram_pct":     nil,
			"battery_pct": float64(90),
			"ac_power":    "yes",
			"new_probe":   "ignored",
		}, want: []string{"ac_power", "cpu_pct"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := hardwareInvalidKeys(tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("hardwareInvalidKeys(%#v) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestCommandResultAtEpoch(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  float64
	}{
		{name: "number", input: float64(1720000000.5), want: 1720000000.5},
		{name: "RFC3339", input: "2026-01-01T00:00:00Z", want: 1767225600},
		{name: "blank", input: " ", want: 0},
		{name: "garbage", input: "not a timestamp", want: 0},
		{name: "other type", input: true, want: 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := commandResultAtEpoch(tc.input); got != tc.want {
				t.Fatalf("commandResultAtEpoch(%#v) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsStopNoopReceipt(t *testing.T) {
	cases := []struct {
		name   string
		rpc    string
		ok     *bool
		reason string
		want   bool
	}{
		{name: "member stop", rpc: "stop", ok: boolPtr(true), reason: "no_such_session: missing", want: true},
		{name: "worker stop", rpc: "worker_stop", ok: boolPtr(true), reason: "no_such_session: missing", want: true},
		{name: "failed stop", rpc: "stop", ok: boolPtr(false), reason: "no_such_session: missing", want: false},
		{name: "other rpc", rpc: "start", ok: boolPtr(true), reason: "no_such_session: missing", want: false},
		{name: "unknown result", rpc: "stop", ok: nil, reason: "no_such_session: missing", want: false},
		{name: "embedded code", rpc: "stop", ok: boolPtr(true), reason: "failed: no_such_session", want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := isStopNoopReceipt(tc.rpc, tc.ok, tc.reason); got != tc.want {
				t.Fatalf("isStopNoopReceipt(%q, %#v, %q) = %v, want %v",
					tc.rpc, tc.ok, tc.reason, got, tc.want)
			}
		})
	}
}

func TestSupersededDispatchClue(t *testing.T) {
	cases := []struct {
		name   string
		member Member
		want   string
	}{
		{name: "dispatch diagnosis", member: Member{LastOpAt: 1720000000, LastOpReason: "wake_timeout: machine never woke"}, want: "[superseded dispatch diagnosis @1720000000] wake_timeout: machine never woke"},
		{name: "different reason", member: Member{LastOpAt: 1720000000, LastOpReason: "stopped"}, want: ""},
		{name: "bare code", member: Member{LastOpAt: 1720000000, LastOpReason: "wake_timeout"}, want: ""},
		{name: "empty", member: Member{}, want: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := supersededDispatchClue(tc.member); got != tc.want {
				t.Fatalf("supersededDispatchClue(%#v) = %q, want %q", tc.member, got, tc.want)
			}
		})
	}
}

func TestStringOf(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{name: "string", input: "stop", want: "stop"},
		{name: "empty", input: "", want: ""},
		{name: "number", input: float64(1), want: ""},
		{name: "nil", input: nil, want: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := stringOf(tc.input); got != tc.want {
				t.Fatalf("stringOf(%#v) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestBoolPtrOf(t *testing.T) {
	cases := []struct {
		name    string
		input   any
		want    bool
		present bool
	}{
		{name: "true", input: true, want: true, present: true},
		{name: "false", input: false, want: false, present: true},
		{name: "string", input: "true"},
		{name: "number", input: float64(1)},
		{name: "nil", input: nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := boolPtrOf(tc.input)
			if !tc.present {
				if got != nil {
					t.Fatalf("boolPtrOf(%#v) = %v, want nil", tc.input, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("boolPtrOf(%#v) = nil, want %v", tc.input, tc.want)
			}
			if *got != tc.want {
				t.Fatalf("boolPtrOf(%#v) = %v, want %v", tc.input, *got, tc.want)
			}
		})
	}
}

func TestFoldCommandResult(t *testing.T) {
	t.Run("a receipt is trimmed to the addressed member, persisted with its full outcome, and fanned to the owner and member", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		api.foldCommandResult(map[string]any{
			"member_id": " kip ", "rpc": "start", "ok": true,
			"reason": "started", "log": "session started", "at": "2026-01-01T00:00:00Z",
		}, "telemetry", "m-server-self")

		want := before
		want.LastOp = "start"
		want.LastOpOK = boolPtr(true)
		want.LastOpReason = "started"
		want.LastOpLog = "session started"
		want.LastOpAt = 1767225600
		apiTestWantEqual(t, "stored member", apiTestMemberRow(t, d, "kip"), want)
		frame := apiTestMemberFrame(1, "patch", "kip",
			apiTestMemberPayload("kip", "Kip", "active", ""), "telemetry")
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a missing log falls back to reason and an untyped ok stays NULL", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")

		api.foldCommandResult(map[string]any{
			"member_id": "kip", "rpc": "stop", "ok": "unknown",
			"reason": "stop refused", "at": float64(1720000000),
		}, "telemetry", "m-server-self")

		want := before
		want.LastOp = "stop"
		want.LastOpOK = nil
		want.LastOpReason = "stop refused"
		want.LastOpLog = "stop refused"
		want.LastOpAt = 1720000000
		apiTestWantEqual(t, "stored member", apiTestMemberRow(t, d, "kip"), want)
		dashboard.wantFrames(apiTestMemberFrame(1, "patch", "kip",
			apiTestMemberPayload("kip", "Kip", "active", ""), "telemetry"))
	})

	t.Run("a successful no-such-session stop leaves the existing receipt untouched and fans nothing", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		before.LastOp = "start"
		before.LastOpOK = boolPtr(true)
		before.LastOpLog = "session started"
		before.LastOpReason = "started"
		before.LastOpAt = 1720000000
		if err := d.SetMemberOpReceipt(before.ID, before.LastOp, before.LastOpOK,
			before.LastOpLog, before.LastOpReason, before.LastOpAt); err != nil {
			t.Fatalf("SetMemberOpReceipt: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.foldCommandResult(map[string]any{
			"member_id": "kip", "rpc": "stop", "ok": true,
			"reason": "no_such_session: nothing to kill", "log": "no session",
			"at": float64(1720000100),
		}, "telemetry", "m-server-self")

		apiTestWantEqual(t, "stored member", apiTestMemberRow(t, d, "kip"), before)
		dashboard.wantFrames()
	})

	t.Run("an unknown or blank member id is ignored without creating a row or fan", func(t *testing.T) {
		for _, memberID := range []string{"", "ghost"} {
			t.Run(map[string]string{"": "blank", "ghost": "unknown"}[memberID], func(t *testing.T) {
				api, _, d, _ := newAPITestServer(t)
				dashboard := apiTestListen(t, api, "")

				api.foldCommandResult(map[string]any{
					"member_id": memberID, "rpc": "start", "ok": true,
					"reason": "started", "log": "started", "at": float64(1720000000),
				}, "telemetry", "m-server-self")

				if memberID != "" {
					got, err := d.GetMember(memberID)
					if err != nil {
						t.Fatalf("GetMember(%q): %v", memberID, err)
					}
					if got != nil {
						t.Fatalf("unknown member was created: %#v", got)
					}
				}
				dashboard.wantFrames()
			})
		}
	})

	t.Run("a successful uninstall converges desired state before publishing the receipt", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		before.DesiredState = DesiredStateOnline
		if err := d.PutMember(before); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.foldCommandResult(map[string]any{
			"member_id": "kip", "rpc": "uninstall", "ok": true,
			"reason": "uninstalled", "log": "teardown complete", "at": float64(1720000200),
		}, "telemetry", "m-server-self")

		want := before
		want.DesiredState = DesiredStateOffline
		want.LastOp = "uninstall"
		want.LastOpOK = boolPtr(true)
		want.LastOpReason = "uninstalled"
		want.LastOpLog = "teardown complete"
		want.LastOpAt = 1720000200
		apiTestWantEqual(t, "stored member", apiTestMemberRow(t, d, "kip"), want)
		payload := apiTestMemberPayload("kip", "Kip", "active", DesiredStateOffline)
		dashboard.wantFrames(
			apiTestMemberFrame(1, "patch", "kip", payload, "telemetry"),
			apiTestMemberFrame(2, "patch", "kip", payload, "telemetry"),
		)
	})
}

func TestFoldWorkerCommandResult(t *testing.T) {
	t.Run("a worker receipt persists the five outcome fields, leaves lifecycle alone, and reaches only the owner", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		before, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || before == nil {
			t.Fatalf("GetOutsourceWorker before: %v %v", before, err)
		}
		dashboard := apiTestListen(t, api, "")
		workerListener := apiTestListen(t, api, "ow-abc123")

		api.foldWorkerCommandResult("ow-abc123", map[string]any{
			"rpc": "worker_stop", "ok": false, "reason": "stop failed",
			"at": float64(1720000000),
		}, "warden-1")

		want := *before
		want.LastOp = "worker_stop"
		want.LastOpOK = boolPtr(false)
		want.LastOpLog = "stop failed"
		want.LastOpReason = "stop failed"
		want.LastOpAt = 1720000000
		after, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || after == nil {
			t.Fatalf("GetOutsourceWorker after: %v %v", after, err)
		}
		apiTestWantEqual(t, "stored worker", *after, want)
		dashboard.wantFrames(apiTestWorkerDelta(2, "assigned", "warden-1"))
		workerListener.wantFrames()
	})

	t.Run("a no-such-session worker stop leaves the existing receipt untouched and fans nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		before, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || before == nil {
			t.Fatalf("GetOutsourceWorker before: %v %v", before, err)
		}
		before.LastOp = "worker_start"
		before.LastOpOK = boolPtr(true)
		before.LastOpLog = "started"
		before.LastOpReason = "started"
		before.LastOpAt = 1720000000
		if err := d.SetMemberOpReceipt(before.ID, before.LastOp, before.LastOpOK,
			before.LastOpLog, before.LastOpReason, before.LastOpAt); err != nil {
			t.Fatalf("SetMemberOpReceipt: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.foldWorkerCommandResult("ow-abc123", map[string]any{
			"rpc": "worker_stop", "ok": true,
			"reason": "no_such_session: nothing to kill", "log": "no session",
			"at": float64(1720000100),
		}, "warden-1")

		after, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || after == nil {
			t.Fatalf("GetOutsourceWorker after: %v %v", after, err)
		}
		apiTestWantEqual(t, "stored worker", *after, *before)
		dashboard.wantFrames()
	})

	t.Run("a refused worker start records its failure and benches the attempted machine", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		api.workerSpawnTarget["ow-abc123"] = "m-server-self"
		dashboard := apiTestListen(t, api, "")

		api.foldWorkerCommandResult("ow-abc123", map[string]any{
			"rpc": "start", "ok": false, "reason": "machine refused",
			"log": "start refused", "at": float64(1720000200),
		}, "warden-1")

		worker, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || worker == nil {
			t.Fatalf("GetOutsourceWorker: %v %v", worker, err)
		}
		if worker.LastOp != "start" || worker.LastOpOK == nil || *worker.LastOpOK ||
			worker.LastOpLog != "start refused" || worker.LastOpReason != "machine refused" ||
			worker.LastOpAt != 1720000200 {
			t.Fatalf("worker receipt = %#v", worker)
		}
		if !api.workerMachineCoolingOn("ow-abc123", "m-server-self", nowSecs()) {
			t.Fatalf("the refused target was not benched")
		}
		dashboard.wantFrames(apiTestWorkerDelta(2, "assigned", "warden-1"))
	})
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
	t.Run("a report is trimmed, persisted on the named roster row, and fanned to that row and the owner", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		api.stampReportedLaunchFacts("kip", " opus ", " codex ", " high ", "telemetry")

		want := before
		want.ActualModel = "opus"
		want.ActualRuntime = "codex"
		want.ActualEffort = "high"
		apiTestWantEqual(t, "stored member", apiTestMemberRow(t, d, "kip"), want)
		frame := apiTestMemberFrame(1, "patch", "kip",
			apiTestMemberPayload("kip", "Kip", "active", ""), "telemetry")
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("blank reported facts do not erase existing values or publish a frame", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		before.ActualModel = "old-model"
		before.ActualRuntime = "old-runtime"
		before.ActualEffort = "old-effort"
		if err := d.PutMember(before); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.stampReportedLaunchFacts("kip", " ", "", "\t", "telemetry")

		apiTestWantEqual(t, "stored member", apiTestMemberRow(t, d, "kip"), before)
		dashboard.wantFrames()
	})

	t.Run("a partial report changes only its non-blank facts", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		before.ActualModel = "old-model"
		before.ActualRuntime = "old-runtime"
		before.ActualEffort = "old-effort"
		if err := d.PutMember(before); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.stampReportedLaunchFacts("kip", " new-model ", "", "new-effort", "telemetry")

		want := before
		want.ActualModel = "new-model"
		want.ActualEffort = "new-effort"
		apiTestWantEqual(t, "stored member", apiTestMemberRow(t, d, "kip"), want)
		dashboard.wantFrames(apiTestMemberFrame(1, "patch", "kip",
			apiTestMemberPayload("kip", "Kip", "active", ""), "telemetry"))
	})

	t.Run("an unchanged report publishes nothing", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		before.ActualModel = "opus"
		before.ActualRuntime = "codex"
		before.ActualEffort = "high"
		if err := d.PutMember(before); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.stampReportedLaunchFacts("kip", "opus", "codex", "high", "telemetry")

		apiTestWantEqual(t, "stored member", apiTestMemberRow(t, d, "kip"), before)
		dashboard.wantFrames()
	})

	t.Run("an unknown or dismissed roster row is unchanged and receives no frame", func(t *testing.T) {
		for _, dismissed := range []bool{false, true} {
			name := "unknown"
			id := "ghost"
			if dismissed {
				name = "dismissed"
				id = "kip"
			}
			t.Run(name, func(t *testing.T) {
				api, _, d, _ := newAPITestServer(t)
				var before Member
				if dismissed {
					before = apiTestMemberRow(t, d, id)
					before.RosterStatus = RosterStatusRemoved
					if err := d.PutMember(before); err != nil {
						t.Fatalf("PutMember: %v", err)
					}
				}
				dashboard := apiTestListen(t, api, "")

				api.stampReportedLaunchFacts(id, "opus", "codex", "high", "telemetry")

				if dismissed {
					apiTestWantEqual(t, "dismissed member", apiTestMemberRow(t, d, id), before)
				} else {
					got, err := d.GetMember(id)
					if err != nil {
						t.Fatalf("GetMember(%q): %v", id, err)
					}
					if got != nil {
						t.Fatalf("unknown member was created: %#v", got)
					}
				}
				dashboard.wantFrames()
			})
		}
	})
}

func TestOrUnknown(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  any
	}{
		{name: "nil", input: nil, want: "?"},
		{name: "string", input: "claude", want: "claude"},
		{name: "zero", input: float64(0), want: float64(0)},
		{name: "false", input: false, want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := orUnknown(tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("orUnknown(%#v) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestEntryStr(t *testing.T) {
	entry := map[string]any{"ok": "value", "wrong": float64(1)}
	cases := []struct {
		name    string
		key     string
		want    string
		present bool
	}{
		{name: "matching string", key: "ok", want: "value", present: true},
		{name: "wrong type", key: "wrong"},
		{name: "absent", key: "absent"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := entryStr(entry, tc.key)
			if !tc.present {
				if got != nil {
					t.Fatalf("entryStr(%q) = %q, want nil", tc.key, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("entryStr(%q) = nil, want %q", tc.key, tc.want)
			}
			if *got != tc.want {
				t.Fatalf("entryStr(%q) = %q, want %q", tc.key, *got, tc.want)
			}
		})
	}
}

func TestEntryObj(t *testing.T) {
	object := map[string]any{"nested": true}
	entry := map[string]any{"ok": object, "wrong": "object"}
	cases := []struct {
		name    string
		key     string
		want    map[string]any
		present bool
	}{
		{name: "matching object", key: "ok", want: object, present: true},
		{name: "wrong type", key: "wrong"},
		{name: "absent", key: "absent"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := entryObj(entry, tc.key)
			if !tc.present {
				if got != nil {
					t.Fatalf("entryObj(%q) = %#v, want nil", tc.key, got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("entryObj(%q) = %#v, want %#v", tc.key, got, tc.want)
			}
		})
	}
}

func TestEntryNum(t *testing.T) {
	entry := map[string]any{"ok": float64(12.5), "wrong": "12.5"}
	cases := []struct {
		name    string
		key     string
		want    float64
		present bool
	}{
		{name: "matching number", key: "ok", want: 12.5, present: true},
		{name: "wrong type", key: "wrong"},
		{name: "absent", key: "absent"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := entryNum(entry, tc.key)
			if !tc.present {
				if got != nil {
					t.Fatalf("entryNum(%q) = %v, want nil", tc.key, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("entryNum(%q) = nil, want %v", tc.key, tc.want)
			}
			if *got != tc.want {
				t.Fatalf("entryNum(%q) = %v, want %v", tc.key, *got, tc.want)
			}
		})
	}
}

func TestRuntimeCapabilitiesStampOf(t *testing.T) {
	cases := []struct {
		name  string
		entry map[string]any
		want  float64
	}{
		{name: "stamp", entry: map[string]any{"runtimes_ts": float64(1720000000)}, want: 1720000000},
		{name: "wrong type", entry: map[string]any{"runtimes_ts": "1720000000"}, want: 0},
		{name: "absent", entry: map[string]any{}, want: 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimeCapabilitiesStampOf(tc.entry); got != tc.want {
				t.Fatalf("runtimeCapabilitiesStampOf(%#v) = %v, want %v", tc.entry, got, tc.want)
			}
		})
	}
}

func TestRateLimitStampOf(t *testing.T) {
	cases := []struct {
		name  string
		entry map[string]any
		want  float64
	}{
		{name: "stamp", entry: map[string]any{"rate_limits_ts": float64(1720000000)}, want: 1720000000},
		{name: "wrong type", entry: map[string]any{"rate_limits_ts": "1720000000"}, want: 0},
		{name: "absent", entry: map[string]any{}, want: 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := rateLimitStampOf(tc.entry); got != tc.want {
				t.Fatalf("rateLimitStampOf(%#v) = %v, want %v", tc.entry, got, tc.want)
			}
		})
	}
}

func TestUsableRateLimitWindow(t *testing.T) {
	cases := []struct {
		name       string
		raw        any
		windowSec  float64
		now        float64
		wantWindow map[string]any
		wantReset  float64
		wantOK     bool
	}{
		{
			name:       "valid numeric reset",
			raw:        map[string]any{"used_percentage": float64(25), "resets_at": float64(1720003600)},
			windowSec:  3600,
			now:        1720001800,
			wantWindow: map[string]any{"used_percentage": float64(25), "resets_at": float64(1720003600)},
			wantReset:  1720003600,
			wantOK:     true,
		},
		{
			name:       "valid RFC3339 reset",
			raw:        map[string]any{"resets_at": "2026-01-01T01:00:00Z"},
			windowSec:  3600,
			now:        1767227400,
			wantWindow: map[string]any{"resets_at": "2026-01-01T01:00:00Z"},
			wantReset:  1767229200,
			wantOK:     true,
		},
		{name: "before window", raw: map[string]any{"resets_at": float64(1720003600)}, windowSec: 3600, now: 1719999999},
		{name: "at reset", raw: map[string]any{"resets_at": float64(1720003600)}, windowSec: 3600, now: 1720003600},
		{name: "missing reset", raw: map[string]any{"used_percentage": float64(25)}, windowSec: 3600, now: 1720001800},
		{name: "not an object", raw: "window", windowSec: 3600, now: 1720001800},
		{name: "non-positive window", raw: map[string]any{"resets_at": float64(1720003600)}, windowSec: 0, now: 1720001800},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			window, resetAt, ok := usableRateLimitWindow(tc.raw, tc.windowSec, tc.now)
			if !reflect.DeepEqual(window, tc.wantWindow) {
				t.Errorf("window = %#v, want %#v", window, tc.wantWindow)
			}
			if resetAt != tc.wantReset {
				t.Errorf("resetAt = %v, want %v", resetAt, tc.wantReset)
			}
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

func TestHardwareStampOf(t *testing.T) {
	cases := []struct {
		name  string
		entry map[string]any
		want  float64
	}{
		{name: "stamp", entry: map[string]any{"hardware_ts": float64(1720000000)}, want: 1720000000},
		{name: "wrong type", entry: map[string]any{"hardware_ts": "1720000000"}, want: 0},
		{name: "absent", entry: map[string]any{}, want: 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := hardwareStampOf(tc.entry); got != tc.want {
				t.Fatalf("hardwareStampOf(%#v) = %v, want %v", tc.entry, got, tc.want)
			}
		})
	}
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
	var nilMap map[string]any
	cases := []struct {
		name  string
		input map[string]any
		want  any
	}{
		{name: "nil", input: nilMap, want: nil},
		{name: "empty", input: map[string]any{}, want: map[string]any{}},
		{name: "value", input: map[string]any{
			"five_hour": map[string]any{"used_pct": float64(10)},
		}, want: map[string]any{
			"five_hour": map[string]any{"used_pct": float64(10)},
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := anyOrNil(tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("anyOrNil(%#v) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}
