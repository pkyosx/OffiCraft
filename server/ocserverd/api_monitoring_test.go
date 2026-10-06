// Skeleton generated from server/ocserverd/api_monitoring.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"net/http"
	"path/filepath"
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

	t.Run("under a warden's not-logged-in refusal, the reason names the reporting machine and the warden's own text stays as the log", func(t *testing.T) {
		claudeText := "claude_not_logged_in: `claude auth status` reports logged out on this host. " +
			"Fix any one: set this member's 執行環境 to Codex; log in with `claude` as this user; or " +
			"re-install the warden with OC_CLAUDE_CRED_CHECK=0 (shell exports do not reach it)."
		codexText := "codex_not_logged_in: `codex login status` failed on this host"
		for _, c := range []struct {
			name, text, want string
			withLog          bool
		}{
			{"claude with a log", claudeText, "claude_not_logged_in: machine 'm-studio' is not logged in to claude", true},
			{"codex with a log", codexText, "codex_not_logged_in: machine 'm-studio' is not logged in to codex", true},
			{"codex without a log", codexText, "codex_not_logged_in: machine 'm-studio' is not logged in to codex", false},
		} {
			t.Run(c.name, func(t *testing.T) {
				api, _, d, _ := newAPITestServer(t)
				before := apiTestMemberRow(t, d, "kip")
				receipt := map[string]any{
					"member_id": "kip", "rpc": "start", "ok": false,
					"reason": c.text, "at": "2024-07-03T09:46:40Z",
				}
				if c.withLog {
					receipt["log"] = c.text
				}

				received := nowSecs()
				api.foldCommandResult(receipt, "telemetry", "m-studio")
				got := apiTestMemberRow(t, d, "kip")

				if got.LastOpAt < received || got.LastOpAt > nowSecs() {
					t.Fatalf("last_op_at = %v, want the server's receipt time (>= %v), not the machine's stamp", got.LastOpAt, received)
				}
				want := before
				want.LastOp = "start"
				want.LastOpOK = boolPtr(false)
				want.LastOpReason = c.want
				want.LastOpLog = c.text
				want.LastOpAt = got.LastOpAt
				apiTestWantEqual(t, "stored member", got, want)
			})
		}
	})

	t.Run("under a not-logged-in refusal from a machine other than the member's pin, the reason names the reporting machine", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.SetMemberDesiredMachineID("kip", "m-other"); err != nil {
			t.Fatalf("SetMemberDesiredMachineID: %v", err)
		}
		text := "codex_not_logged_in: `codex login status` failed on this host"

		api.foldCommandResult(map[string]any{
			"member_id": "kip", "rpc": "start", "ok": false,
			"reason": text, "log": text, "at": float64(1720000000),
		}, "telemetry", "m-studio")

		apiWantValue(t, "reason", any(apiTestMemberRow(t, d, "kip").LastOpReason),
			any("codex_not_logged_in: machine 'm-studio' is not logged in to codex"))
	})

	t.Run("under a not-logged-in refusal with no reporting machine, the reason is kept as the warden wrote it", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		text := "codex_not_logged_in: `codex login status` failed on this host"

		api.foldCommandResult(map[string]any{
			"member_id": "kip", "rpc": "start", "ok": false,
			"reason": text, "log": text, "at": float64(1720000000),
		}, "telemetry", "")

		row := apiTestMemberRow(t, d, "kip")
		apiWantValue(t, "receipt", any([]string{row.LastOpReason, row.LastOpLog}), any([]string{text, text}))
	})

	t.Run("under a worker's not-logged-in refusal from a machine other than its pin, the worker row's reason names the reporting machine", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		if err := d.SetMemberDesiredMachineID("ow-abc123", "m-other"); err != nil {
			t.Fatalf("SetMemberDesiredMachineID: %v", err)
		}
		text := "codex_not_logged_in: `codex login status` failed on this host"

		api.foldCommandResult(map[string]any{
			"worker_id": "ow-abc123", "rpc": "start", "ok": false,
			"reason": text, "log": text, "at": float64(1720000000),
		}, "telemetry", "m-studio")

		after, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || after == nil {
			t.Fatalf("GetOutsourceWorker: %v %v", after, err)
		}
		apiWantValue(t, "receipt", any([]string{after.LastOpReason, after.LastOpLog}), any([]string{
			"codex_not_logged_in: machine 'm-studio' is not logged in to codex", text,
		}))
	})

	t.Run("a successful no-such-session stop leaves the existing receipt untouched and fans nothing", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		before := apiTestMemberRow(t, d, "kip")
		before.LastOp = "start"
		before.LastOpOK = boolPtr(true)
		before.LastOpLog = "session started"
		before.LastOpReason = "started"
		before.LastOpAt = 1720000000
		if err := d.SetMemberLastOp(before.ID, before.LastOp, before.LastOpOK,
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
		if err := d.putMemberWholeRowForTest(before); err != nil {
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
	t.Run("under a start-cleared anchor, a clobber refusal folds the receipt and restores the session's anchor, notice claim and readings", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		infraSeedAnchoredSession(t, api, d, "kip")
		api.clearSessionBootTSForStart("kip")

		api.foldCommandResult(map[string]any{
			"member_id": "kip", "rpc": "start", "ok": false,
			"reason": infraClobberReason, "at": float64(1720000000),
		}, "telemetry", "m-server-self")

		infraWantSession(t, api, d, "kip", 1700000000, 1700000000, infraSeededGauge())
		if m := apiTestMemberRow(t, d, "kip"); m.LastOp != "start" || m.LastOpReason != infraClobberReason {
			t.Fatalf("the refusal must still fold onto last_op: %q %q", m.LastOp, m.LastOpReason)
		}
	})

	t.Run("under a start-cleared anchor, a start refused for another reason leaves the session unanchored for good", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		infraSeedAnchoredSession(t, api, d, "kip")
		api.clearSessionBootTSForStart("kip")

		api.foldCommandResult(map[string]any{
			"member_id": "kip", "rpc": "start", "ok": false,
			"reason": "mkdir_failed: permission denied", "at": float64(1720000000),
		}, "telemetry", "m-server-self")
		api.restoreRefusedStartAnchor("kip", "start", boolPtr(false), infraClobberReason)

		infraWantSession(t, api, d, "kip", 0, 0, map[string]any{
			"rate_limits": map[string]any{}, "ts": 1700000100.0,
		})
	})

	t.Run("under a start-cleared anchor, an accepted start leaves it cleared and the new session's connect mints a fresh anchor", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		infraSeedAnchoredSession(t, api, d, "kip")
		api.clearSessionBootTSForStart("kip")

		api.foldCommandResult(map[string]any{
			"member_id": "kip", "rpc": "start", "ok": true,
			"reason": "started", "at": float64(1720000000),
		}, "telemetry", "m-server-self")

		infraWantSession(t, api, d, "kip", 0, 0, map[string]any{
			"rate_limits": map[string]any{}, "ts": 1700000100.0,
		})
		connectedAt := float64(time.Now().Unix())
		api.onFirstConnect("kip")
		if got := infraTestMember(t, d, "kip").SessionBootTS; got < connectedAt {
			t.Fatalf("the new session must anchor at its own connect (>= %v), got %v", connectedAt, got)
		}
	})
}

func TestFoldWorkerCommandResult(t *testing.T) {
	t.Run("a worker receipt persists the five outcome fields, leaves lifecycle alone, and reaches the owner and the worker's own session", func(t *testing.T) {
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
		// The receipt now folds onto the SHARED member row, so its delta is a
		// member delta — and a member delta is addressed to the member itself as
		// well as the owner cockpit (publishMemberPatch). The worker's own
		// session seeing its fresh last_op is the unified behaviour, not a leak:
		// this is exactly what a staff member's receipt does.
		dashboard.wantFrames(apiTestWorkerDelta(2, "assigned", "warden-1"))
		workerListener.wantFrames(apiTestWorkerDelta(2, "assigned", "warden-1"))
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
		if err := d.SetMemberLastOp(before.ID, before.LastOp, before.LastOpOK,
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
		if !api.workerMachineBenched("ow-abc123", "m-server-self", nowSecs()) {
			t.Fatalf("the refused target was not benched")
		}
		dashboard.wantFrames(apiTestWorkerDelta(2, "assigned", "warden-1"))
	})
	t.Run("a worker start refused because a live session holds the slot records its failure and benches nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		api.workerSpawnTarget["ow-abc123"] = "m-server-self"

		api.foldWorkerCommandResult("ow-abc123", map[string]any{
			"rpc": "start", "ok": false, "reason": infraClobberReason,
			"log": "start refused", "at": float64(1720000200),
		}, "warden-1")

		worker, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || worker == nil {
			t.Fatalf("GetOutsourceWorker: %v %v", worker, err)
		}
		apiWantValue(t, "receipt", any(map[string]any{
			"op": worker.LastOp, "log": worker.LastOpLog, "reason": worker.LastOpReason,
			"at": worker.LastOpAt, "ok": worker.LastOpOK != nil && !*worker.LastOpOK,
		}), any(map[string]any{
			"op": "start", "log": "start refused", "reason": infraClobberReason,
			"at": 1720000200.0, "ok": true,
		}))
		apiWantValue(t, "bench book", any(wsBenchBook(api)), any(map[string]any{}))
	})

	t.Run("under a start-cleared anchor, a clobber refusal restores the worker's anchor, notice claim and readings", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		infraSeedAnchoredSession(t, api, d, "ow-abc123")
		api.clearSessionBootTSForStart("ow-abc123")

		api.foldWorkerCommandResult("ow-abc123", map[string]any{
			"rpc": "start", "ok": false, "reason": infraClobberReason,
			"at": float64(1720000200),
		}, "warden-1")

		infraWantSession(t, api, d, "ow-abc123", 1700000000, 1700000000, infraSeededGauge())
	})

	t.Run("under a start-cleared anchor, an accepted worker start leaves the worker unanchored", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		infraSeedAnchoredSession(t, api, d, "ow-abc123")
		api.clearSessionBootTSForStart("ow-abc123")

		api.foldWorkerCommandResult("ow-abc123", map[string]any{
			"rpc": "start", "ok": true, "reason": "started",
			"at": float64(1720000200),
		}, "warden-1")
		api.restoreRefusedStartAnchor("ow-abc123", "start", boolPtr(false), infraClobberReason)

		infraWantSession(t, api, d, "ow-abc123", 0, 0, map[string]any{
			"rate_limits": map[string]any{}, "ts": 1700000100.0,
		})
	})
}

func TestHandleIngestTelemetryApiMonitoringTelemetryPost(t *testing.T) {
	t.Run("a full warden report answers a receipt carrying the login check, login recheck and disk-usage intervals, and the blocks it carried show up on the monitoring view", func(t *testing.T) {
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
			"agent_id":                    "m-server-self",
			"machine":                     "m-server-self",
			"ts":                          apiAnyNumber,
			"login_check_interval_secs":   300,
			"login_recheck_interval_secs": 30,
			"disk_usage_interval_secs":    3600,
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

	t.Run("under owner-set login check, login recheck and disk-usage intervals, a warden's receipt carries those values and an agent's receipt on the same machine carries none of them", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		mira := apiTestAgentToken(t, api, "mira", "m-server-self")
		apiJSON(t, h, "PATCH", "/api/settings", owner, `{"runtime_login_check_interval_secs":45,"runtime_login_recheck_interval_secs":60,"disk_usage_interval_secs":1200}`)

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden, `{"tokens":{"input":1}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"agent_id":                    "m-server-self",
			"machine":                     "m-server-self",
			"ts":                          apiAnyNumber,
			"login_check_interval_secs":   45,
			"login_recheck_interval_secs": 60,
			"disk_usage_interval_secs":    1200,
		})

		status, data = apiJSON(t, h, "POST", "/api/monitoring/telemetry", mira, `{"tokens":{"input":1}}`)
		if status != 200 {
			t.Fatalf("agent: want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"agent_id": "mira",
			"machine":  "m-server-self",
			"ts":       apiAnyNumber,
		})
	})

	t.Run("under a machine's claude login flipping, including from never-reported to false, every member running there is re-announced to the owner, a repeat of the same state announces nobody, and a logged-out machine returning from stale telemetry is re-announced", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		kip := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", kip, `{"runtime":"claude"}`)
		kipLink, err := api.hub.Connect("kip", "m-server-self")
		if err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		t.Cleanup(func() { api.hub.Disconnect(kipLink) })
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"runtimes":{"claude":{"installed":true}}}`)
		dashboard := apiTestListen(t, api, "")
		report := func(body string) {
			t.Helper()
			if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden, body); status != 200 {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
		}

		monitoringSignal := map[string]any{
			"seq":   apiAnyNumber,
			"topic": "monitoring",
			"op":    "signal",
			"data": map[string]any{
				"entity":  "monitoring",
				"key":     "m-server-self",
				"epoch":   apiAnyNumber,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "m-server-self",
		}
		kipPatch := map[string]any{
			"seq":   apiAnyNumber,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::kip",
				"epoch":   apiAnyNumber,
				"deleted": false,
				"payload": map[string]any{
					"id":            "kip",
					"name":          "Kip",
					"owner_id":      "owner",
					"status":        "active",
					"desired_state": "",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "m-server-self",
		}

		report(`{"runtimes":{"claude":{"installed":true,"logged_in":false}}}`)
		dashboard.wantFrames(monitoringSignal, kipPatch)

		report(`{"runtimes":{"claude":{"installed":true,"logged_in":true}}}`)
		report(`{"runtimes":{"claude":{"installed":true,"logged_in":false}}}`)
		report(`{"runtimes":{"claude":{"installed":true,"logged_in":false}}}`)
		dashboard.wantFrames(monitoringSignal, kipPatch, monitoringSignal, kipPatch, monitoringSignal)

		aged := api.telemetry.Get("m-server-self")
		aged["runtimes_ts"] = nowSecs() - telemetryFreshSecs - 1
		api.telemetry.Set("m-server-self", aged)
		report(`{"runtimes":{"claude":{"installed":true,"logged_in":false}}}`)
		dashboard.wantFrames(monitoringSignal, kipPatch)
	})

	t.Run("under a machine's claude login flipping, an outsource worker whose pair names that machine is re-announced to the owner", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-abc123", Codename: "Contractor", Runtime: "claude", ActualRuntime: "claude",
			TaskID: "T-1", Status: WorkerStatusActive, CreatedTS: 12,
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		api.workerSpawnTarget["ow-abc123"] = "m-server-self"
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"runtimes":{"claude":{"installed":true,"logged_in":true}}}`)
		dashboard := apiTestListen(t, api, "")

		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"runtimes":{"claude":{"installed":true,"logged_in":false}}}`); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}

		dashboard.wantFrames(
			map[string]any{
				"seq":   apiAnyNumber,
				"topic": "monitoring",
				"op":    "signal",
				"data": map[string]any{
					"entity":  "monitoring",
					"key":     "m-server-self",
					"epoch":   apiAnyNumber,
					"deleted": false,
					"payload": nil,
				},
				"ts":      apiAnyNumber,
				"trigger": "m-server-self",
			},
			map[string]any{
				"seq":   apiAnyNumber,
				"topic": "member",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "member",
					"key":     "owner::ow-abc123",
					"epoch":   apiAnyNumber,
					"deleted": false,
					"payload": map[string]any{
						"id":            "ow-abc123",
						"name":          "Contractor",
						"owner_id":      "owner",
						"status":        "active",
						"desired_state": "",
					},
				},
				"ts":      apiAnyNumber,
				"trigger": "m-server-self",
			},
		)
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

	t.Run("a warden report carrying only disk_usage answers 200, a later report without it keeps the stored measurement, and a newer measurement replaces it", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		box := apiTestDiskBox(t, api, d)
		diskOnMonitoring := func() any {
			t.Helper()
			status, view := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
			if status != 200 {
				t.Fatalf("monitoring: want 200, got %d (%v)", status, view)
			}
			machines, _ := view["machines"].([]any)
			row, _ := machines[0].(map[string]any)
			if row["machine"] != "m-box" {
				t.Fatalf("first machine row: want m-box, got %v", row["machine"])
			}
			return row["disk_usage"]
		}
		wantDisk := func(measuredAt float64, root int) map[string]any {
			return diskWant(measuredAt, root, nil, []any{diskCat("workspaces", 0, true), diskCat("other", root, true)}, []any{}, nil, nil)
		}

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", box,
			`{"disk_usage":{"measured_at":1759500000,"root_bytes":4000,"members":[]}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"agent_id":                    "m-box",
			"machine":                     "m-box",
			"ts":                          apiAnyNumber,
			"login_check_interval_secs":   300,
			"login_recheck_interval_secs": 30,
			"disk_usage_interval_secs":    3600,
		})
		apiWantValue(t, "after the measurement", diskOnMonitoring(), wantDisk(1759500000, 4000))

		apiJSON(t, h, "POST", "/api/monitoring/telemetry", box, `{"tokens":{"input":1}}`)
		apiWantValue(t, "after a report without disk_usage", diskOnMonitoring(), wantDisk(1759500000, 4000))

		apiJSON(t, h, "POST", "/api/monitoring/telemetry", box,
			`{"disk_usage":{"measured_at":1759503600,"root_bytes":6000,"members":[]}}`)
		apiWantValue(t, "after a newer measurement", diskOnMonitoring(), wantDisk(1759503600, 6000))
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
			"agent_id":                    "m-server-self",
			"machine":                     "m-server-self",
			"ts":                          apiAnyNumber,
			"login_check_interval_secs":   300,
			"login_recheck_interval_secs": 30,
			"disk_usage_interval_secs":    3600,
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

	t.Run("under a start-cleared anchor, a posted clobber-refusal receipt answers 200 and puts the member's anchor back", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		infraSeedAnchoredSession(t, api, d, "mira")
		api.clearSessionBootTSForStart("mira")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"command_result":{"rpc":"start","member_id":"mira","ok":false,`+
				`"reason":"session_already_exists: tmux session \"member-mira\" is already live (clobber-guard refused to stomp it)",`+
				`"at":"2026-01-01T00:00:00Z"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}

		infraWantSession(t, api, d, "mira", 1700000000, 1700000000, infraSeededGauge())
	})

	t.Run("a body carrying none of the declared blocks answers 400 naming them all", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, `{}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "rate_limits, tokens, hardware, binaries, claude, cost, effort, runtime, runtimes, "+
			"self_update, command_result, warden_shape, cutover_effect, model_call or disk_usage is required")
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
			{`{"runtimes":{"claude":{"below_notify_minimum":"yes"}}}`, "runtimes.claude.below_notify_minimum must be a boolean or null"},
		} {
			status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent, c.body)
			if status != 400 {
				t.Fatalf("%s: want 400, got %d (%v)", c.body, status, data)
			}
			apiWantError(t, data, "validation_error", c.message)
		}
	})

	t.Run("under a claude below_notify_minimum, the monitoring view carries it on claude only and drops it from codex", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"runtimes":{"claude":{"installed":true,"version":"2.1.200","below_notify_minimum":true},`+
				`"codex":{"installed":true,"below_notify_minimum":false}}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}

		status, view := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("monitoring: want 200, got %d (%v)", status, view)
		}
		wantMachine := apiTestMonitoringMachine()
		wantMachine["runtime_capabilities"] = map[string]any{
			"claude": map[string]any{"installed": true, "version": "2.1.200", "below_notify_minimum": true},
			"codex":  map[string]any{"installed": true},
		}
		wantMachine["runtime_capabilities_ts"] = apiAnyNumber
		wantMachine["runtime_capabilities_stale"] = false
		apiWantBody(t, view, map[string]any{
			"sessions": view["sessions"],
			"machines": []any{wantMachine},
			"accounts": view["accounts"],
		})
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
			"agent_id":                    "m-server-self",
			"machine":                     "m-server-self",
			"ts":                          apiAnyNumber,
			"login_check_interval_secs":   300,
			"login_recheck_interval_secs": 30,
			"disk_usage_interval_secs":    3600,
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

	for _, shape := range windowDALShapes {
		t.Run(shape+": a rename that landed after the fold read the row survives the uninstall convergence", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, _ := newAPITestServerOn(t, d)
			warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
			if status, data := windowJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
				`{"binaries":{"ocagent":"1.0.0"}}`); status != http.StatusOK {
				t.Fatalf("warm-up report: %d %v", status, data)
			}
			if _, err := d.wdb.Exec(`UPDATE member SET desired_state = 'online' WHERE id = 'kip'`); err != nil {
				t.Fatalf("prepare: %v", err)
			}
			hook.execAfterRead(t, path, "FROM member WHERE id",
				`UPDATE member SET name = 'Kip the Second' WHERE id = 'kip'`)

			status, data := windowJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
				`{"command_result":{"rpc":"uninstall","member_id":"kip","ok":true,"reason":"uninstalled","at":"2026-01-01T00:00:00Z"}}`)

			hook.wantFiredOnce(t)
			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			got := apiTestMemberRow(t, d, "kip")
			if got.Name != "Kip the Second" || got.DesiredState != DesiredStateOffline || got.LastOp != "uninstall" {
				t.Fatalf("name=%q desired_state=%q last_op=%q, want \"Kip the Second\" / offline / uninstall",
					got.Name, got.DesiredState, got.LastOp)
			}
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": "+"an uninstall receipt that fails to land does not converge the intent either", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, _ := newAPITestServerOn(t, d)
			warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
			if _, err := d.wdb.Exec(`UPDATE member SET desired_state = 'online' WHERE id = 'kip'`); err != nil {
				t.Fatalf("prepare: %v", err)
			}
			before := apiTestMemberRow(t, d, "kip")
			windowRefuse(t, d, "refuse_receipt", "BEFORE UPDATE OF last_op ON member", "the receipt write fails")
			self := apiTestListen(t, api, "kip")

			status, data := windowJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
				`{"command_result":{"rpc":"uninstall","member_id":"kip","ok":true,"reason":"uninstalled","at":"2026-01-01T00:00:00Z"}}`)

			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			apiTestWantEqual(t, "the row after the failed receipt", apiTestMemberRow(t, d, "kip"), before)
			self.wantFrames()
		})
	}

	for _, shape := range windowDALShapes {
		t.Run(shape+": a rename that landed after the launch-fact stamp read the row survives the stamp", func(t *testing.T) {
			d, hook, path := windowDAL(t, shape)
			api, h, _, _ := newAPITestServerOn(t, d)
			kip := apiTestAgentToken(t, api, "kip", "")
			hook.execAfterRead(t, path, "FROM member WHERE id",
				`UPDATE member SET name = 'Kip the Second' WHERE id = 'kip'`)

			status, data := windowJSON(t, h, "POST", "/api/monitoring/telemetry", kip,
				`{"effort":"high","model":"claude-opus-5"}`)

			hook.wantFiredOnce(t)
			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			got := apiTestMemberRow(t, d, "kip")
			if got.Name != "Kip the Second" || got.ActualModel != "claude-opus-5" || got.ActualEffort != "high" {
				t.Fatalf("name=%q actual_model=%q actual_effort=%q, want \"Kip the Second\" / claude-opus-5 / high",
					got.Name, got.ActualModel, got.ActualEffort)
			}
		})
	}
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
		if err := d.putMemberWholeRowForTest(before); err != nil {
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
		if err := d.putMemberWholeRowForTest(before); err != nil {
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
		if err := d.putMemberWholeRowForTest(before); err != nil {
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
					if err := d.putMemberWholeRowForTest(before); err != nil {
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
		"disk_usage":                 nil,
	}
}

// apiTestDiskBox onboards a second warden machine, m-box, and answers its
// machine credential.
func apiTestDiskBox(t *testing.T, api *apiServer, d *DAL) string {
	t.Helper()
	box := Member{ID: "m-box", Name: "m-box", Codename: "m-box", Kind: KindWarden,
		RosterStatus: RosterStatusActive, ActivatedTS: 1}
	if err := d.putMemberWholeRowForTest(box); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	return apiTestAgentToken(t, api, "m-box", "m-box")
}

// diskCat is one top-level row of a disk_usage breakdown as served.
func diskCat(key string, bytes any, inRoot bool) map[string]any {
	return map[string]any{"key": key, "parent_key": nil, "bytes": bytes, "in_root": inRoot}
}

func diskMember(id string, name any, status string, workspace any) map[string]any {
	return map[string]any{"member_id": id, "name": name, "roster_status": status, "workspace_bytes": workspace}
}

// diskWant is a served disk_usage object.
func diskWant(measuredAt, total, dbAt any, categories, members []any, free, diskTotal any) map[string]any {
	return map[string]any{
		"measured_at":          measuredAt,
		"total_bytes":          total,
		"database_measured_at": dbAt,
		"categories":           categories,
		"members":              members,
		"disk_free_bytes":      free,
		"disk_total_bytes":     diskTotal,
	}
}

// apiTestDiskBoxMachine is m-box's row with nothing but its disk usage set by
// the caller.
func apiTestDiskBoxMachine() map[string]any {
	row := apiTestMonitoringMachine()
	row["machine"] = "m-box"
	row["display_name"] = "m-box"
	return row
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
			"limit_reached": nil,
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
				"account":       "eva-m5-claude",
				"display_name":  "eva-m5-claude",
				"machine":       "m-server-self",
				"cost":          nil,
				"five_hour":     nil,
				"seven_day":     nil,
				"limit_reached": nil,
			}},
		})
	})

	t.Run("a live contractor answers 200 with one session row and one more agent on its machine", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.putMemberWholeRowForTest(Member{
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
		if err := d.putMemberWholeRowForTest(Member{
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

	t.Run("a departed staff member answers 200 with its account still naming the machine and usage it reported", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		kip := apiTestAgentToken(t, api, "kip", "m-server-self")
		resetsAt := time.Now().Unix() + 3600
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", kip,
			`{"runtime":"claude","account":"kip-claude","account_label":"kip@example.com","cost":2,`+
				`"rate_limits":{"five_hour":{"used_percentage":10,"resets_at":`+strconv.FormatInt(resetsAt, 10)+`}}}`)
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss kip: want 200, got %d (%v)", status, data)
		}

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiTestSessionsByID(t, data, "mira", "m-server-self")
		machine := apiTestMonitoringMachine()
		machine["accounts"] = []any{"kip-claude"}
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": []any{machine},
			"accounts": []any{map[string]any{
				"account":       "kip-claude",
				"account_label": "kip@example.com",
				"display_name":  "kip@example.com",
				"machine":       "m-server-self",
				"cost":          2,
				"five_hour": map[string]any{
					"used_pct":    10,
					"resets_at":   float64(resetsAt),
					"elapsed_pct": apiAnyNumber,
					"measured_at": apiAnyNumber,
					"pace":        "ok",
				},
				"seven_day":     nil,
				"limit_reached": nil,
			}},
		})
	})

	t.Run("a released contractor answers 200 with its account still naming the machine and usage it reported", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		worker := apiTestAgentToken(t, api, "ow-abc123", "m-server-self")
		resetsAt := time.Now().Unix() + 3600
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", worker,
			`{"runtime":"claude","account":"ow-claude","account_label":"ow@example.com","cost":3,`+
				`"rate_limits":{"five_hour":{"used_percentage":10,"resets_at":`+strconv.FormatInt(resetsAt, 10)+`}}}`)
		released, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || released == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", released, err)
		}
		released.Status = WorkerStatusReleased
		if err := d.PutOutsourceWorker(*released); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiTestSessionsByID(t, data, "mira", "kip", "m-server-self")
		machine := apiTestMonitoringMachine()
		machine["accounts"] = []any{"ow-claude"}
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": []any{machine},
			"accounts": []any{map[string]any{
				"account":       "ow-claude",
				"account_label": "ow@example.com",
				"display_name":  "ow@example.com",
				"machine":       "m-server-self",
				"cost":          3,
				"five_hour": map[string]any{
					"used_pct":    10,
					"resets_at":   float64(resetsAt),
					"elapsed_pct": apiAnyNumber,
					"measured_at": apiAnyNumber,
					"pace":        "ok",
				},
				"seven_day":     nil,
				"limit_reached": nil,
			}},
		})
	})

	reportFiveHour := func(t *testing.T, api *apiServer, h http.Handler, id string, usedPct int, resetsAt int64) {
		t.Helper()
		token := apiTestAgentToken(t, api, id, "m-server-self")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", token,
			`{"runtime":"claude","account":"shared-claude",`+
				`"rate_limits":{"five_hour":{"used_percentage":`+strconv.Itoa(usedPct)+
				`,"resets_at":`+strconv.FormatInt(resetsAt, 10)+`}}}`); status != 200 {
			t.Fatalf("%s telemetry: want 200, got %d (%v)", id, status, data)
		}
	}
	ageRateLimits := func(api *apiServer, id string, stamp float64) {
		entry := api.telemetry.Get(id)
		entry["rate_limits_ts"] = stamp
		api.telemetry.Set(id, entry)
	}
	sharedAccountRow := func(fiveHour map[string]any) map[string]any {
		return map[string]any{
			"account":       "shared-claude",
			"display_name":  "shared-claude",
			"machine":       "m-server-self",
			"cost":          nil,
			"five_hour":     fiveHour,
			"seven_day":     nil,
			"limit_reached": nil,
		}
	}

	t.Run("a fresh report and an older one with a later reset answer 200 with the fresh report's window", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		now := time.Now().Unix()
		reportFiveHour(t, api, h, "mira", 10, now+4000)
		ageRateLimits(api, "mira", float64(now)-telemetryFreshSecs-60)
		reportFiveHour(t, api, h, "kip", 60, now+3000)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": data["machines"],
			"accounts": []any{sharedAccountRow(map[string]any{
				"used_pct":    60,
				"resets_at":   float64(now + 3000),
				"elapsed_pct": apiAnyNumber,
				"measured_at": apiAnyNumber,
				"pace":        "ok",
			})},
		})
	})

	t.Run("fresh reports from the previous and the new window answer 200 with the new window", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		now := time.Now().Unix()
		reportFiveHour(t, api, h, "mira", 90, now+100)
		reportFiveHour(t, api, h, "kip", 5, now+3600)
		ageRateLimits(api, "kip", float64(now)-30)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": data["machines"],
			"accounts": []any{sharedAccountRow(map[string]any{
				"used_pct":    5,
				"resets_at":   float64(now + 3600),
				"elapsed_pct": apiAnyNumber,
				"measured_at": apiAnyNumber,
				"pace":        "ok",
			})},
		})
	})

	t.Run("fresh reports of the same window answer 200 with the newer sample", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		now := time.Now().Unix()
		olderStamp := float64(now) - 30
		reportFiveHour(t, api, h, "kip", 60, now+3000)
		reportFiveHour(t, api, h, "mira", 20, now+3000)
		ageRateLimits(api, "kip", olderStamp)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": data["machines"],
			"accounts": []any{sharedAccountRow(map[string]any{
				"used_pct":    20,
				"resets_at":   float64(now + 3000),
				"elapsed_pct": apiAnyNumber,
				"measured_at": apiAnyNumber,
				"pace":        "ok",
			})},
		})
	})

	t.Run("a fresh report without a seven-day window answers 200 with the seven-day window from a stale report", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		now := time.Now().Unix()
		staleStamp := float64(now) - telemetryFreshSecs - 60
		kip := apiTestAgentToken(t, api, "kip", "m-server-self")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", kip,
			`{"runtime":"claude","account":"shared-claude",`+
				`"rate_limits":{"seven_day":{"used_percentage":40,"resets_at":`+strconv.FormatInt(now+86400, 10)+`}}}`); status != 200 {
			t.Fatalf("kip telemetry: want 200, got %d (%v)", status, data)
		}
		ageRateLimits(api, "kip", staleStamp)
		reportFiveHour(t, api, h, "mira", 60, now+3000)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		row := sharedAccountRow(map[string]any{
			"used_pct":    60,
			"resets_at":   float64(now + 3000),
			"elapsed_pct": apiAnyNumber,
			"measured_at": apiAnyNumber,
			"pace":        "ok",
		})
		row["seven_day"] = map[string]any{
			"used_pct":    40,
			"resets_at":   float64(now + 86400),
			"elapsed_pct": apiAnyNumber,
			"measured_at": staleStamp,
			"pace":        nil,
		}
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": data["machines"],
			"accounts": []any{row},
		})
	})

	t.Run("only stale reports answer 200 with the later reset and no pace verdict", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		now := time.Now().Unix()
		staleStamp := float64(now) - telemetryFreshSecs - 60
		reportFiveHour(t, api, h, "mira", 10, now+4000)
		reportFiveHour(t, api, h, "kip", 60, now+3000)
		ageRateLimits(api, "mira", staleStamp)
		ageRateLimits(api, "kip", staleStamp+30)

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": data["machines"],
			"accounts": []any{sharedAccountRow(map[string]any{
				"used_pct":    10,
				"resets_at":   float64(now + 4000),
				"elapsed_pct": apiAnyNumber,
				"measured_at": staleStamp,
				"pace":        nil,
			})},
		})
	})

	t.Run("a removed machine answers 200 with its account naming no machine", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		gone := Member{ID: "m-gone", Name: "gone-host", Codename: "gone-host", Kind: KindWarden,
			RosterStatus: RosterStatusActive, ActivatedTS: 1}
		if err := d.putMemberWholeRowForTest(gone); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		warden := apiTestAgentToken(t, api, "m-gone", "m-gone")
		apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"runtime":"claude","account":"gone-claude","tokens":{"input":1}}`)
		gone.RosterStatus = RosterStatusRemoved
		if err := d.putMemberWholeRowForTest(gone); err != nil {
			t.Fatalf("PutMember: %v", err)
		}

		status, data := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiTestSessionsByID(t, data, "mira", "kip", "m-server-self")
		apiWantBody(t, data, map[string]any{
			"sessions": data["sessions"],
			"machines": []any{apiTestMonitoringMachine()},
			"accounts": []any{map[string]any{
				"account":       "gone-claude",
				"display_name":  "gone-claude",
				"machine":       "",
				"cost":          nil,
				"five_hour":     nil,
				"seven_day":     nil,
				"limit_reached": nil,
			}},
		})
	})

	// diskView ingests one warden report as tok (when body is not empty) and
	// answers the disk_usage of the monitoring row for host.
	diskView := func(t *testing.T, h http.Handler, owner, tok, host, body string) any {
		t.Helper()
		if body != "" {
			if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", tok, `{"disk_usage":`+body+`}`); status != 200 {
				t.Fatalf("ingest: want 200, got %d (%v)", status, data)
			}
		}
		status, view := apiJSON(t, h, "GET", "/api/monitoring", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, view)
		}
		for _, raw := range view["machines"].([]any) {
			if row := raw.(map[string]any); row["machine"] == host {
				return row["disk_usage"]
			}
		}
		t.Fatalf("no machine row %s", host)
		return nil
	}

	t.Run("under a warden's disk measurement on another machine, its row lists workspaces, the warden's categories and other, and the total is the root", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		box := apiTestDiskBox(t, api, d)
		got := diskView(t, h, owner, box, "m-box", `{
			"measured_at":1759500000.5,"took_secs":12.3,"root_bytes":10000,
			"disk_free_bytes":500000,"disk_total_bytes":900000,
			"categories":[
				{"key":"old_version_backups","bytes":2000,"in_root":true},
				{"key":"logs","bytes":1000,"in_root":true}
			],
			"members":[
				{"member_id":"kip","workspace_bytes":2000},
				{"member_id":"mira","workspace_bytes":3000}
			]}`)
		apiWantValue(t, "disk_usage", got, diskWant(1759500000.5, 10000, nil, []any{
			diskCat("workspaces", 5000, true),
			diskCat("logs", 1000, true),
			diskCat("old_version_backups", 2000, true),
			diskCat("other", 2000, true),
		}, []any{
			diskMember("mira", "Mira", "active", 3000),
			diskMember("kip", "Kip", "active", 2000),
		}, 500000, 900000))
		apiWantValue(t, "the server row, not measured", diskView(t, h, owner, "", "m-server-self", ""), nil)
	})

	t.Run("under the server's database inside the station root, its database, backups and old copies are rows inside the root, the copies added to old_version_backups", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		root, dbPath := diskTestStation(t)
		diskTestOldCopies(t, dbPath)
		api.recordServerDisk(dbPath, root, time.Unix(1759500100, 0))
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		got := diskView(t, h, owner, warden, "m-server-self", `{
			"measured_at":1759500000,"root_bytes":100000,
			"categories":[{"key":"logs","bytes":1000,"in_root":true},{"key":"old_version_backups","bytes":2000,"in_root":true}],
			"members":[{"member_id":"mira","workspace_bytes":30000}]}`)
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, 100000, 1759500100, []any{
			diskCat("database", 16384, true),
			diskCat("backups", 20480, true),
			diskCat("workspaces", 30000, true),
			diskCat("logs", 1000, true),
			diskCat("old_version_backups", 2000+diskTestOldCopiesBytes, true),
			diskCat("other", 100000-16384-20480-30000-1000-2000-diskTestOldCopiesBytes, true),
		}, []any{diskMember("mira", "Mira", "active", 30000)}, nil, nil))
	})

	t.Run("under a warden that sends no old_version_backups, the server's old copies inside the root are that row on their own and logs stay in other", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		root, dbPath := diskTestStation(t)
		diskTestOldCopies(t, dbPath)
		api.recordServerDisk(dbPath, root, time.Unix(1759500100, 0))
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		got := diskView(t, h, owner, warden, "m-server-self", `{"measured_at":1759500000,"root_bytes":100000,"members":[]}`)
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, 100000, 1759500100, []any{
			diskCat("database", 16384, true),
			diskCat("backups", 20480, true),
			diskCat("workspaces", 0, true),
			diskCat("old_version_backups", diskTestOldCopiesBytes, true),
			diskCat("other", 100000-16384-20480-diskTestOldCopiesBytes, true),
		}, []any{}, nil, nil))
	})

	t.Run("under the server's database outside the station root, database, backups and old_database_copies are rows outside it, added to the total and not taken from other", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, dbPath := diskTestStation(t)
		diskTestOldCopies(t, dbPath)
		api.recordServerDisk(dbPath, t.TempDir(), time.Unix(1759500100, 0))
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		got := diskView(t, h, owner, warden, "m-server-self", `{
			"measured_at":1759500000,"root_bytes":100000,
			"categories":[{"key":"logs","bytes":1000,"in_root":true},{"key":"old_version_backups","bytes":2000,"in_root":true}],
			"members":[{"member_id":"mira","workspace_bytes":30000}]}`)
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, 100000+16384+20480+diskTestOldCopiesBytes, 1759500100, []any{
			diskCat("database", 16384, false),
			diskCat("backups", 20480, false),
			diskCat("workspaces", 30000, true),
			diskCat("logs", 1000, true),
			diskCat("old_version_backups", 2000, true),
			diskCat("old_database_copies", diskTestOldCopiesBytes, false),
			diskCat("other", 100000-30000-1000-2000, true),
		}, []any{diskMember("mira", "Mira", "active", 30000)}, nil, nil))
	})

	t.Run("under the server's database outside the station root with its backups unmeasured, the total is null and other is not", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.serverDisk.Store(&serverDiskSample{DatabaseBytes: diskTestInt(16384), OldCopiesBytes: diskTestInt(0), MeasuredAt: 1759500100})
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		got := diskView(t, h, owner, warden, "m-server-self", `{"measured_at":1759500000,"root_bytes":100000,"members":[{"member_id":"mira","workspace_bytes":30000}]}`)
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, nil, 1759500100, []any{
			diskCat("database", 16384, false),
			diskCat("backups", nil, false),
			diskCat("workspaces", 30000, true),
			diskCat("old_database_copies", 0, false),
			diskCat("other", 70000, true),
		}, []any{diskMember("mira", "Mira", "active", 30000)}, nil, nil))
	})

	t.Run("under only the server's own measurement, the server row lists its rows with no other and no total", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		root, dbPath := diskTestStation(t)
		api.recordServerDisk(dbPath, root, time.Unix(1759500100, 0))
		apiWantValue(t, "disk_usage", diskView(t, h, owner, "", "m-server-self", ""), diskWant(nil, nil, 1759500100, []any{
			diskCat("database", 16384, true),
			diskCat("backups", 20480, true),
			diskCat("old_version_backups", 0, true),
		}, []any{}, nil, nil))
	})

	t.Run("under the server row's warden measurement arriving before the server measured its database, other is null because the root may still hold the database", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		got := diskView(t, h, owner, warden, "m-server-self", `{"measured_at":1759500000,"root_bytes":100000,"members":[],
			"categories":[{"key":"logs","bytes":10,"in_root":true}]}`)
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, 100000, nil, []any{
			diskCat("workspaces", 0, true),
			diskCat("logs", 10, true),
			diskCat("other", nil, true),
		}, []any{}, nil, nil))
	})

	t.Run("under workspaces of an active, a removed and an unknown member and one not sized, members sort by workspace with the unsized last, and workspaces and other are null", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.putMemberWholeRowForTest(Member{ID: "dev-gone", Name: "Gone Dev", Codename: "gone-dev",
			Kind: KindStaff, RosterStatus: RosterStatusRemoved}); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		box := apiTestDiskBox(t, api, d)
		got := diskView(t, h, owner, box, "m-box", `{"measured_at":1759500000,"root_bytes":1000,
			"members":[
				{"member_id":"kip"},
				{"member_id":"mira","workspace_bytes":100},
				{"member_id":"ow-ghost","workspace_bytes":0},
				{"member_id":"dev-gone","workspace_bytes":100}
			]}`)
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, 1000, nil, []any{
			diskCat("workspaces", nil, true),
			diskCat("other", nil, true),
		}, []any{
			diskMember("dev-gone", "Gone Dev", "removed", 100),
			diskMember("mira", "Mira", "active", 100),
			diskMember("ow-ghost", nil, "unknown", 0),
			diskMember("kip", "Kip", "active", nil),
		}, nil, nil))
	})

	t.Run("under fields and categories of the wrong shape, the heartbeat is accepted, a bad size reads as a failed probe, an entry without a key or in_root, a duplicate and a server-owned key are dropped", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		box := apiTestDiskBox(t, api, d)
		got := diskView(t, h, owner, box, "m-box", `{
			"measured_at":"yesterday","root_bytes":"lots","disk_free_bytes":true,"disk_total_bytes":null,
			"categories":[
				{"key":"logs","bytes":-5,"in_root":true},
				{"key":"old_version_backups","bytes":12.5,"in_root":true},
				{"key":7,"bytes":1,"in_root":true},
				{"key":"cache","bytes":1},
				{"key":"database","bytes":1,"in_root":true},
				{"key":"other","bytes":1,"in_root":true},
				{"key":"logs","bytes":3,"in_root":true},
				"x"
			],
			"members":[7,{"workspace_bytes":5},{"member_id":"kip","workspace_bytes":"x"}]}`)
		apiWantValue(t, "disk_usage", got, diskWant(nil, nil, nil, []any{
			diskCat("workspaces", nil, true),
			diskCat("logs", nil, true),
			diskCat("old_version_backups", nil, true),
			diskCat("other", nil, true),
		}, []any{diskMember("kip", "Kip", "active", nil)}, nil, nil))
	})

	t.Run("under a members field that is not a list, workspaces and other are null, members is empty and the total is still the root", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		box := apiTestDiskBox(t, api, d)
		got := diskView(t, h, owner, box, "m-box", `{"measured_at":1759500000,"root_bytes":500,"members":"many"}`)
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, 500, nil, []any{
			diskCat("workspaces", nil, true),
			diskCat("other", nil, true),
		}, []any{}, nil, nil))
	})

	t.Run("under rows inside the root that add up past it, other is 0 rather than negative", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		box := apiTestDiskBox(t, api, d)
		got := diskView(t, h, owner, box, "m-box", `{"measured_at":1759500000,"root_bytes":100,
			"categories":[{"key":"logs","bytes":50,"in_root":true}],
			"members":[{"member_id":"kip","workspace_bytes":300}]}`)
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, 100, nil, []any{
			diskCat("workspaces", 300, true),
			diskCat("logs", 50, true),
			diskCat("other", 0, true),
		}, []any{diskMember("kip", "Kip", "active", 300)}, nil, nil))
	})

	t.Run("a category the warden does not report stays in other; one it reports as failed makes other null but not the total", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		box := apiTestDiskBox(t, api, d)
		absent := diskView(t, h, owner, box, "m-box", `{"measured_at":1759500000,"root_bytes":1000,"members":[],
			"categories":[{"key":"logs","bytes":100,"in_root":true}]}`)
		apiWantValue(t, "old_version_backups absent", absent, diskWant(1759500000, 1000, nil, []any{
			diskCat("workspaces", 0, true),
			diskCat("logs", 100, true),
			diskCat("other", 900, true),
		}, []any{}, nil, nil))
		failed := diskView(t, h, owner, box, "m-box", `{"measured_at":1759500001,"root_bytes":1000,"members":[],
			"categories":[{"key":"logs","bytes":100,"in_root":true},{"key":"old_version_backups","bytes":null,"in_root":true}]}`)
		apiWantValue(t, "old_version_backups failed", failed, diskWant(1759500001, 1000, nil, []any{
			diskCat("workspaces", 0, true),
			diskCat("logs", 100, true),
			diskCat("old_version_backups", nil, true),
			diskCat("other", nil, true),
		}, []any{}, nil, nil))
	})

	t.Run("an unknown key is passed on after the known ones; parts follow a parent the server adds as their sum, outside the root counted in the total, and a failed part makes the parent and the total null", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		box := apiTestDiskBox(t, api, d)
		got := diskView(t, h, owner, box, "m-box", `{"measured_at":1759500000,"root_bytes":1000,"members":[],
			"categories":[
				{"key":"zeta-b","parent_key":"zeta","bytes":5,"in_root":false},
				{"key":"cache","bytes":50,"in_root":true},
				{"key":"zeta-a","parent_key":"zeta","bytes":10,"in_root":false},
				{"key":"logs","bytes":100,"in_root":true}
			]}`)
		part := func(key string, bytes any) map[string]any {
			c := diskCat(key, bytes, false)
			c["parent_key"] = "zeta"
			return c
		}
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, 1015, nil, []any{
			diskCat("workspaces", 0, true),
			diskCat("logs", 100, true),
			diskCat("cache", 50, true),
			diskCat("zeta", 15, false),
			part("zeta-a", 10),
			part("zeta-b", 5),
			diskCat("other", 850, true),
		}, []any{}, nil, nil))
		failed := diskView(t, h, owner, box, "m-box", `{"measured_at":1759500001,"root_bytes":1000,"members":[],
			"categories":[{"key":"zeta-a","parent_key":"zeta","bytes":null,"in_root":false},{"key":"zeta-b","parent_key":"zeta","bytes":5,"in_root":false}]}`)
		apiWantValue(t, "a failed part", failed, diskWant(1759500001, nil, nil, []any{
			diskCat("workspaces", 0, true),
			diskCat("zeta", nil, false),
			part("zeta-a", nil),
			part("zeta-b", 5),
			diskCat("other", 1000, true),
		}, []any{}, nil, nil))
	})

	t.Run("a warden part naming a server-owned row as its parent is dropped: the server's database, backups and workspaces keep their numbers and there is one other", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		root, dbPath := diskTestStation(t)
		api.recordServerDisk(dbPath, root, time.Unix(1759500100, 0))
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		got := diskView(t, h, owner, warden, "m-server-self", `{"measured_at":1759500000,"root_bytes":100000,
			"members":[{"member_id":"mira","workspace_bytes":30000}],
			"categories":[
				{"key":"a","parent_key":"database","bytes":1,"in_root":true},
				{"key":"b","parent_key":"backups","bytes":1,"in_root":true},
				{"key":"c","parent_key":"workspaces","bytes":1,"in_root":true},
				{"key":"d","parent_key":"other","bytes":1,"in_root":true},
				{"key":"e","parent_key":"old_database_copies","bytes":1,"in_root":false},
				{"key":"logs","bytes":1000,"in_root":true}
			]}`)
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, 100000, 1759500100, []any{
			diskCat("database", 16384, true),
			diskCat("backups", 20480, true),
			diskCat("workspaces", 30000, true),
			diskCat("logs", 1000, true),
			diskCat("old_version_backups", 0, true),
			diskCat("other", 100000-16384-20480-30000-1000, true),
		}, []any{diskMember("mira", "Mira", "active", 30000)}, nil, nil))
	})

	t.Run("under a warden that sends old_version_backups as parts, the server's old copies inside the root are one more part and the row is their sum", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		root, dbPath := diskTestStation(t)
		diskTestOldCopies(t, dbPath)
		api.recordServerDisk(dbPath, root, time.Unix(1759500100, 0))
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		got := diskView(t, h, owner, warden, "m-server-self", `{"measured_at":1759500000,"root_bytes":100000,"members":[],
			"categories":[
				{"key":"releases","parent_key":"old_version_backups","bytes":1000,"in_root":true},
				{"key":"binaries","parent_key":"old_version_backups","bytes":500,"in_root":true}
			]}`)
		part := func(key string, bytes any) map[string]any {
			c := diskCat(key, bytes, true)
			c["parent_key"] = "old_version_backups"
			return c
		}
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, 100000, 1759500100, []any{
			diskCat("database", 16384, true),
			diskCat("backups", 20480, true),
			diskCat("workspaces", 0, true),
			diskCat("old_version_backups", 1500+diskTestOldCopiesBytes, true),
			part("binaries", 500),
			part("old_database_copies", diskTestOldCopiesBytes),
			part("releases", 1000),
			diskCat("other", 100000-16384-20480-1500-diskTestOldCopiesBytes, true),
		}, []any{}, nil, nil))
	})

	t.Run("under an orphaned half-written snapshot, its journal and a file put in backups/ by hand, their bytes are in old_version_backups, not in backups or other", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		root, dbPath := diskTestStation(t)
		dir := backupDirFor(dbPath)
		diskTestWrite(t, filepath.Join(dir, "officraft-20260811-012749-premigration.db.partial"), 8000)
		diskTestWrite(t, filepath.Join(dir, "officraft-20260811-012749-premigration.db.partial-journal"), 100)
		diskTestWrite(t, filepath.Join(dir, "notes.txt"), 1)
		const strays = 8192 + 4096 + 4096
		api.recordServerDisk(dbPath, root, time.Unix(1759500100, 0))
		warden := apiTestAgentToken(t, api, "m-server-self", "m-server-self")
		got := diskView(t, h, owner, warden, "m-server-self", `{"measured_at":1759500000,"root_bytes":100000,"members":[],
			"categories":[{"key":"old_version_backups","bytes":2000,"in_root":true}]}`)
		apiWantValue(t, "disk_usage", got, diskWant(1759500000, 100000, 1759500100, []any{
			diskCat("database", 16384, true),
			diskCat("backups", 20480, true),
			diskCat("workspaces", 0, true),
			diskCat("old_version_backups", 2000+strays, true),
			diskCat("other", 100000-16384-20480-2000-strays, true),
		}, []any{}, nil, nil))
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
