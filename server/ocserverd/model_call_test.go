package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

const modelCallEpoch = 1_800_000_000.0

type modelCallTimer struct {
	wait time.Duration
	fire func()
}

type modelCallFixture struct {
	api    *apiServer
	h      http.Handler
	d      *DAL
	owner  string
	clock  float64
	timers []modelCallTimer
	diffs  int
}

func newModelCallFixture(t *testing.T) *modelCallFixture {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	f := &modelCallFixture{api: api, h: h, d: d, owner: owner, clock: modelCallEpoch}
	api.modelCallNow = func() float64 { return f.clock }
	api.modelCallAfterFunc = func(wait time.Duration, fire func()) {
		f.timers = append(f.timers, modelCallTimer{wait: wait, fire: fire})
	}
	api.modelCallDiffObserved = func() { f.diffs++ }
	return f
}

// staff hires a claude member and sends the report every shipped reporter
// sends first (runtime + account), so the account pairing exists before the
// model-call reports under test arrive.
func (f *modelCallFixture) staff(t *testing.T, m Member, machineClaim, account string) string {
	t.Helper()
	m.Name, m.Kind, m.RosterStatus = m.ID, KindStaff, RosterStatusActive
	if m.Runtime == "" {
		m.Runtime = RuntimeClaude
	}
	if m.DesiredState == "" {
		m.DesiredState = DesiredStateOffline
	}
	putTestMember(t, f.api, m)
	token := apiTestAgentToken(t, f.api, m.ID, machineClaim)
	f.report(t, token, `{"runtime":"claude","account":"`+account+`"}`)
	return token
}

func (f *modelCallFixture) report(t *testing.T, token, body string) {
	t.Helper()
	if status, data := apiJSON(t, f.h, "POST", "/api/monitoring/telemetry", token, body); status != 200 {
		t.Fatalf("telemetry %s: %d %v", body, status, data)
	}
}

func (f *modelCallFixture) fail(t *testing.T, token string, ts float64, kind, code string, resetsAt any) {
	t.Helper()
	reset := "null"
	if resetsAt != nil {
		reset = fmt.Sprint(resetsAt)
	}
	f.report(t, token, fmt.Sprintf(`{"model_call":{"last_failure":{"ts":%v,"kind":%q,"code":%q,"resets_at":%s}}}`,
		ts, kind, code, reset))
}

func (f *modelCallFixture) succeed(t *testing.T, token string, ts float64) {
	t.Helper()
	f.report(t, token, fmt.Sprintf(`{"model_call":{"last_success_ts":%v}}`, ts))
}

func (f *modelCallFixture) member(t *testing.T, id string) map[string]any {
	t.Helper()
	status, data := apiJSON(t, f.h, "GET", "/api/members/"+id, f.owner, "")
	if status != 200 {
		t.Fatalf("GET member %s: %d %v", id, status, data)
	}
	return data
}

// wantModelCall compares both model-call fields of a member row in full.
func (f *modelCallFixture) wantModelCall(t *testing.T, id string, lastSuccess float64, warnings ...map[string]any) {
	t.Helper()
	row := f.member(t, id)
	want := make([]any, len(warnings))
	for i := range warnings {
		want[i] = warnings[i]
	}
	apiWantValue(t, id+".model_call_warnings", row["model_call_warnings"], any(want))
	apiWantValue(t, id+".model_call_last_success_ts", row["model_call_last_success_ts"], any(lastSuccess))
}

func (f *modelCallFixture) accountLimit(t *testing.T, account string) any {
	t.Helper()
	status, data := apiJSON(t, f.h, "GET", "/api/monitoring", f.owner, "")
	if status != 200 {
		t.Fatalf("GET monitoring: %d %v", status, data)
	}
	accounts, _ := data["accounts"].([]any)
	for _, raw := range accounts {
		row, _ := raw.(map[string]any)
		if row["account"] == account {
			return row["limit_reached"]
		}
	}
	t.Fatalf("no account row %q in %v", account, data["accounts"])
	return nil
}

func modelCallWarning(kind, code string, since float64, resetsAt any, accountWide bool) map[string]any {
	return map[string]any{
		"account_wide": accountWide,
		"code":         code,
		"kind":         kind,
		"resets_at":    resetsAt,
		"runtime":      "claude",
		"since_ts":     since,
	}
}

func modelCallPatch(id, trigger string) map[string]any {
	return map[string]any{
		"seq": apiAnyNumber, "topic": "member", "op": "patch",
		"data": map[string]any{
			"entity": "member", "key": "owner::" + id, "epoch": apiAnyNumber, "deleted": false,
			"payload": map[string]any{
				"id": id, "name": id, "status": RosterStatusActive,
				"desired_state": DesiredStateOffline, "owner_id": "owner",
			},
		},
		"ts": apiAnyNumber, "trigger": trigger,
	}
}

func modelCallMonitoringSignal(key, trigger string) map[string]any {
	return map[string]any{
		"seq": apiAnyNumber, "topic": "monitoring", "op": "signal",
		"data": map[string]any{
			"entity": "monitoring", "key": key, "epoch": apiAnyNumber, "deleted": false, "payload": nil,
		},
		"ts": apiAnyNumber, "trigger": trigger,
	}
}

func TestModelCallWarnings(t *testing.T) {
	t0 := modelCallEpoch

	t.Run("a failure shows until a newer success arrives, then the row lists nothing", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")

		f.fail(t, ann, t0+10, "server", "server_error", nil)
		f.wantModelCall(t, "m-ann", 0, modelCallWarning("server", "server_error", t0+10, nil, false))

		f.succeed(t, ann, t0+20)
		f.wantModelCall(t, "m-ann", t0+20)
	})

	t.Run("the same failure sent twice is listed once", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")

		f.fail(t, ann, t0+10, "other", "max_output_tokens", nil)
		f.fail(t, ann, t0+10, "other", "max_output_tokens", nil)
		f.wantModelCall(t, "m-ann", 0, modelCallWarning("other", "max_output_tokens", t0+10, nil, false))
	})

	t.Run("an older failure arriving after a newer success shows nothing", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")

		f.succeed(t, ann, t0+20)
		f.fail(t, ann, t0+10, "server", "overloaded", nil)
		f.wantModelCall(t, "m-ann", t0+20)
	})

	t.Run("an older success arriving after a newer failure leaves the failure showing", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")

		f.fail(t, ann, t0+20, "server", "overloaded", nil)
		f.succeed(t, ann, t0+10)
		f.wantModelCall(t, "m-ann", t0+10, modelCallWarning("server", "overloaded", t0+20, nil, false))
	})

	t.Run("an older failure arriving after a newer one does not replace it", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")

		f.fail(t, ann, t0+20, "server", "overloaded", nil)
		f.fail(t, ann, t0+10, "other", "unknown", nil)
		f.wantModelCall(t, "m-ann", 0, modelCallWarning("server", "overloaded", t0+20, nil, false))
	})

	t.Run("a lost success is made up by the next, newer one, and a late older one changes nothing", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")

		f.fail(t, ann, t0+10, "server", "server_error", nil)
		// The success at t0+15 does not arrive in time.
		f.succeed(t, ann, t0+30)
		f.wantModelCall(t, "m-ann", t0+30)

		f.succeed(t, ann, t0+15)
		f.wantModelCall(t, "m-ann", t0+30)
	})

	t.Run("a usage limit one member hits shows on every member of the account until another member succeeds after it", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")
		bob := f.staff(t, Member{ID: "m-bob"}, "", "acct-1")
		f.staff(t, Member{ID: "m-cat"}, "", "acct-2")

		f.fail(t, ann, t0+10, "rate_limit", "rate_limit", t0+3600)
		f.wantModelCall(t, "m-ann", 0, modelCallWarning("rate_limit", "rate_limit", t0+10, t0+3600, false))
		f.wantModelCall(t, "m-bob", 0, modelCallWarning("rate_limit", "rate_limit", t0+10, t0+3600, true))
		f.wantModelCall(t, "m-cat", 0)
		apiWantValue(t, "acct-1.limit_reached", f.accountLimit(t, "acct-1"), any(map[string]any{
			"code": "rate_limit", "kind": "rate_limit", "resets_at": t0 + 3600, "ts": t0 + 10,
		}))
		apiWantValue(t, "acct-2.limit_reached", f.accountLimit(t, "acct-2"), nil)

		f.succeed(t, bob, t0+20)
		f.wantModelCall(t, "m-ann", 0)
		f.wantModelCall(t, "m-bob", t0+20)
		apiWantValue(t, "acct-1.limit_reached", f.accountLimit(t, "acct-1"), nil)
	})

	t.Run("a usage limit without a reset time stays on the member that hit it", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")
		f.staff(t, Member{ID: "m-bob"}, "", "acct-1")

		f.fail(t, ann, t0+10, "rate_limit", "usageLimitExceeded", nil)
		f.wantModelCall(t, "m-ann", 0, modelCallWarning("rate_limit", "usageLimitExceeded", t0+10, nil, false))
		f.wantModelCall(t, "m-bob", 0)
		apiWantValue(t, "acct-1.limit_reached", f.accountLimit(t, "acct-1"), nil)
	})

	t.Run("a usage limit is lifted for the whole account once its reset time passes", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")
		f.staff(t, Member{ID: "m-bob"}, "", "acct-1")

		f.fail(t, ann, t0+10, "rate_limit", "rate_limit", t0+100)
		f.clock = t0 + 99
		f.wantModelCall(t, "m-ann", 0, modelCallWarning("rate_limit", "rate_limit", t0+10, t0+100, false))
		f.wantModelCall(t, "m-bob", 0, modelCallWarning("rate_limit", "rate_limit", t0+10, t0+100, true))

		f.clock = t0 + 100
		f.wantModelCall(t, "m-ann", 0)
		f.wantModelCall(t, "m-bob", 0)
		apiWantValue(t, "acct-1.limit_reached", f.accountLimit(t, "acct-1"), nil)
	})

	t.Run("an auth failure is left out while the same runtime already shows as logged out, and listed otherwise", func(t *testing.T) {
		f := newModelCallFixture(t)
		putWarden(t, f.api, "m-out")
		putWarden(t, f.api, "m-in")
		for machine, runtimes := range map[string]string{
			"m-out": `{"runtimes":{"claude":{"installed":true,"logged_in":false}}}`,
			"m-in":  `{"runtimes":{"claude":{"installed":true,"logged_in":true}}}`,
		} {
			f.report(t, apiTestAgentToken(t, f.api, machine, machine), runtimes)
		}
		ann := f.staff(t, Member{ID: "m-ann", DesiredState: DesiredStateOnline, DesiredMachineID: "m-out"}, "m-out", "acct-1")
		bob := f.staff(t, Member{ID: "m-bob", DesiredState: DesiredStateOnline, DesiredMachineID: "m-in"}, "m-in", "acct-2")
		connectOnlineMachine(t, f.api, "m-ann", "m-out")
		connectOnlineMachine(t, f.api, "m-bob", "m-in")

		f.fail(t, ann, t0+10, "auth", "authentication_failed", nil)
		f.fail(t, bob, t0+10, "auth", "authentication_failed", nil)

		apiWantValue(t, "m-ann.runtime_login_warnings", f.member(t, "m-ann")["runtime_login_warnings"], any([]any{
			map[string]any{"machine_id": "m-out", "machine_name": "m-out", "pending": false, "runtime": "claude"},
		}))
		f.wantModelCall(t, "m-ann", 0)
		apiWantValue(t, "m-bob.runtime_login_warnings", f.member(t, "m-bob")["runtime_login_warnings"], any([]any{}))
		f.wantModelCall(t, "m-bob", 0, modelCallWarning("auth", "authentication_failed", t0+10, nil, false))
	})

	t.Run("an auth failure is listed when the logged-out runtime is another one, or the only logged-out entry is pending", func(t *testing.T) {
		f := newModelCallFixture(t)
		putWarden(t, f.api, "m-mix")
		putWarden(t, f.api, "m-out")
		for machine, runtimes := range map[string]string{
			"m-mix": `{"runtimes":{"claude":{"installed":true,"logged_in":true},"codex":{"installed":true,"logged_in":false}}}`,
			"m-out": `{"runtimes":{"claude":{"installed":true,"logged_in":false}}}`,
		} {
			f.report(t, apiTestAgentToken(t, f.api, machine, machine), runtimes)
		}
		// Configured for codex, still running claude: the codex logged-out mark
		// is the pending pair.
		ann := f.staff(t, Member{ID: "m-ann", Runtime: RuntimeCodex, DesiredState: DesiredStateOnline, DesiredMachineID: "m-mix"}, "m-mix", "acct-1")
		// Running on m-mix, moving to m-out: the claude logged-out mark is pending.
		bob := f.staff(t, Member{ID: "m-bob", DesiredState: DesiredStateOnline, DesiredMachineID: "m-out"}, "m-mix", "acct-2")
		// Running on m-out: the claude logged-out mark is current, so it dedups.
		cat := f.staff(t, Member{ID: "m-cat", DesiredState: DesiredStateOnline, DesiredMachineID: "m-out"}, "m-out", "acct-3")
		connectOnlineMachine(t, f.api, "m-ann", "m-mix")
		connectOnlineMachine(t, f.api, "m-bob", "m-mix")
		connectOnlineMachine(t, f.api, "m-cat", "m-out")
		for _, token := range []string{ann, bob, cat} {
			f.fail(t, token, t0+10, "auth", "authentication_failed", nil)
		}

		apiWantValue(t, "m-ann.runtime_login_warnings", f.member(t, "m-ann")["runtime_login_warnings"], any([]any{
			map[string]any{"machine_id": "m-mix", "machine_name": "m-mix", "pending": true, "runtime": "codex"},
		}))
		f.wantModelCall(t, "m-ann", 0, modelCallWarning("auth", "authentication_failed", t0+10, nil, false))
		apiWantValue(t, "m-bob.runtime_login_warnings", f.member(t, "m-bob")["runtime_login_warnings"], any([]any{
			map[string]any{"machine_id": "m-out", "machine_name": "m-out", "pending": true, "runtime": "claude"},
		}))
		f.wantModelCall(t, "m-bob", 0, modelCallWarning("auth", "authentication_failed", t0+10, nil, false))
		apiWantValue(t, "m-cat.runtime_login_warnings", f.member(t, "m-cat")["runtime_login_warnings"], any([]any{
			map[string]any{"machine_id": "m-out", "machine_name": "m-out", "pending": false, "runtime": "claude"},
		}))
		f.wantModelCall(t, "m-cat", 0)
	})

	t.Run("a member's own usage limit without a reset time also lists the account's limit that has one, and one with a reset time does not", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")
		bob := f.staff(t, Member{ID: "m-bob"}, "", "acct-1")
		cat := f.staff(t, Member{ID: "m-cat"}, "", "acct-1")

		f.fail(t, bob, t0+10, "rate_limit", "rate_limit", t0+3600)
		f.fail(t, ann, t0+20, "rate_limit", "usageLimitExceeded", nil)
		f.fail(t, cat, t0+5, "rate_limit", "rate_limit", t0+1800)
		f.wantModelCall(t, "m-ann", 0,
			modelCallWarning("rate_limit", "usageLimitExceeded", t0+20, nil, false),
			modelCallWarning("rate_limit", "rate_limit", t0+10, t0+3600, true))
		f.wantModelCall(t, "m-bob", 0, modelCallWarning("rate_limit", "rate_limit", t0+10, t0+3600, false))
		f.wantModelCall(t, "m-cat", 0, modelCallWarning("rate_limit", "rate_limit", t0+5, t0+1800, false))
	})

	t.Run("an outsource worker's failure shows on its row", func(t *testing.T) {
		f := newModelCallFixture(t)
		putTestMember(t, f.api, Member{
			ID: "ow-1", Name: "O-1", Codename: "O-1", Kind: KindOutsource, Runtime: RuntimeClaude,
			RosterStatus: RosterStatusActive, Status: WorkerStatusActive, ActivatedTS: 1,
		})
		worker := apiTestAgentToken(t, f.api, "ow-1", "")
		f.report(t, worker, `{"runtime":"claude","account":"acct-1"}`)

		f.fail(t, worker, t0+10, "server", "overloaded", nil)
		f.wantModelCall(t, "ow-1", 0, modelCallWarning("server", "overloaded", t0+10, nil, false))
		f.succeed(t, worker, t0+20)
		f.wantModelCall(t, "ow-1", t0+20)
	})

	t.Run("a light roster row carries an empty list and 0 where the full row has values", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")
		f.succeed(t, ann, t0+5)
		f.fail(t, ann, t0+10, "server", "server_error", nil)
		f.wantModelCall(t, "m-ann", t0+5, modelCallWarning("server", "server_error", t0+10, nil, false))

		rec := apiRequest(t, f.h, "GET", "/api/members?fields=light", f.owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var rows []any
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			if row["id"] != "m-ann" {
				continue
			}
			apiWantValue(t, "light.model_call_warnings", row["model_call_warnings"], any([]any{}))
			apiWantValue(t, "light.model_call_last_success_ts", row["model_call_last_success_ts"], any(0.0))
			return
		}
		t.Fatalf("no m-ann row in the light roster: %v", rows)
	})

	t.Run("a failure kind outside the vocabulary answers 400 and stores nothing", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")

		status, data := apiJSON(t, f.h, "POST", "/api/monitoring/telemetry", ann,
			`{"model_call":{"last_failure":{"ts":1800000010,"kind":"quota","code":"x"}}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"model_call.last_failure.kind must be 'auth', 'rate_limit', 'server' or 'other'")
		f.wantModelCall(t, "m-ann", 0)
	})
}

func TestModelCallWarningPushes(t *testing.T) {
	t0 := modelCallEpoch

	t.Run("a member patch goes out when a warning appears and when it clears, and none when a report changes nothing", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")
		dashboard := apiTestListen(t, f.api, "")

		f.fail(t, ann, t0+10, "server", "server_error", nil)
		dashboard.wantFrames(modelCallMonitoringSignal("m-ann", "m-ann"), modelCallPatch("m-ann", "m-ann"))

		f.fail(t, ann, t0+10, "server", "server_error", nil)
		dashboard.wantFrames(modelCallMonitoringSignal("m-ann", "m-ann"))

		f.succeed(t, ann, t0+5)
		dashboard.wantFrames(modelCallMonitoringSignal("m-ann", "m-ann"))

		f.succeed(t, ann, t0+20)
		dashboard.wantFrames(modelCallMonitoringSignal("m-ann", "m-ann"), modelCallPatch("m-ann", "m-ann"))
	})

	t.Run("success reports with no failure newer than the old success push no patch and run no roster diff", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")
		bob := f.staff(t, Member{ID: "m-bob"}, "", "acct-1")
		f.fail(t, bob, t0+5, "rate_limit", "rate_limit", t0+3600)
		f.succeed(t, bob, t0+6)
		f.succeed(t, ann, t0+7)
		f.diffs = 0
		dashboard := apiTestListen(t, f.api, "")

		f.succeed(t, ann, t0+10)
		f.succeed(t, ann, t0+20)
		f.succeed(t, bob, t0+30)
		dashboard.wantFrames(modelCallMonitoringSignal("m-ann", "m-ann"), modelCallMonitoringSignal("m-ann", "m-ann"),
			modelCallMonitoringSignal("m-bob", "m-bob"))
		if f.diffs != 0 {
			t.Fatalf("want no roster diff, got %d", f.diffs)
		}
		f.wantModelCall(t, "m-ann", t0+20)
	})

	t.Run("a success that clears a failure, the member's own or the account's limit, still runs the diff and pushes", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")
		bob := f.staff(t, Member{ID: "m-bob"}, "", "acct-1")
		f.fail(t, ann, t0+10, "server", "server_error", nil)
		dashboard := apiTestListen(t, f.api, "")
		f.diffs = 0

		f.succeed(t, ann, t0+15)
		dashboard.wantFrames(modelCallMonitoringSignal("m-ann", "m-ann"), modelCallPatch("m-ann", "m-ann"))
		f.fail(t, bob, t0+20, "rate_limit", "rate_limit", t0+3600)
		dashboard.wantFrames(modelCallMonitoringSignal("m-bob", "m-bob"),
			modelCallPatch("m-ann", "m-bob"), modelCallPatch("m-bob", "m-bob"))
		f.diffs = 0
		f.succeed(t, ann, t0+30)
		dashboard.wantFrames(modelCallMonitoringSignal("m-ann", "m-ann"),
			modelCallPatch("m-ann", "m-ann"), modelCallPatch("m-bob", "m-ann"))
		if f.diffs != 1 {
			t.Fatalf("want 1 roster diff, got %d", f.diffs)
		}
	})

	t.Run("an account-wide limit patches every member of the account and nobody else", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")
		bob := f.staff(t, Member{ID: "m-bob"}, "", "acct-1")
		f.staff(t, Member{ID: "m-cat"}, "", "acct-2")
		dashboard := apiTestListen(t, f.api, "")

		f.fail(t, ann, t0+10, "rate_limit", "rate_limit", t0+3600)
		dashboard.wantFrames(modelCallMonitoringSignal("m-ann", "m-ann"),
			modelCallPatch("m-ann", "m-ann"), modelCallPatch("m-bob", "m-ann"))

		f.succeed(t, bob, t0+20)
		dashboard.wantFrames(modelCallMonitoringSignal("m-bob", "m-bob"),
			modelCallPatch("m-ann", "m-bob"), modelCallPatch("m-bob", "m-bob"))
	})

	t.Run("the reset time of a usage limit patches the account's members and signals monitoring once, past the reset", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")
		f.staff(t, Member{ID: "m-bob"}, "", "acct-1")
		dashboard := apiTestListen(t, f.api, "")

		f.fail(t, ann, t0+10, "rate_limit", "rate_limit", t0+100)
		dashboard.wantFrames(modelCallMonitoringSignal("m-ann", "m-ann"),
			modelCallPatch("m-ann", "m-ann"), modelCallPatch("m-bob", "m-ann"))
		if len(f.timers) != 1 || f.timers[0].wait != 101*time.Second {
			t.Fatalf("want one timer 101s out (1s past the reset), got %v", f.timers)
		}

		f.clock = t0 + 101
		f.timers[0].fire()
		dashboard.wantFrames(modelCallPatch("m-ann", triggerServer), modelCallPatch("m-bob", triggerServer),
			modelCallMonitoringSignal("", triggerServer))
		f.wantModelCall(t, "m-bob", 0)
	})

	t.Run("a repeated usage-limit report arms no second timer, and a failure with no reset time arms none", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")

		f.fail(t, ann, t0+10, "rate_limit", "rate_limit", t0+100)
		f.fail(t, ann, t0+10, "rate_limit", "rate_limit", t0+100)
		f.fail(t, ann, t0+20, "rate_limit", "usageLimitExceeded", nil)
		if len(f.timers) != 1 {
			t.Fatalf("want exactly one timer, got %d", len(f.timers))
		}
	})

	t.Run("many failures sharing a reset time arm one timer, and each new reset time arms its own, also after one has fired", func(t *testing.T) {
		f := newModelCallFixture(t)
		ann := f.staff(t, Member{ID: "m-ann"}, "", "acct-1")
		bob := f.staff(t, Member{ID: "m-bob"}, "", "acct-1")

		f.fail(t, ann, t0+10, "rate_limit", "rate_limit", t0+100)
		f.fail(t, ann, t0+11, "rate_limit", "rate_limit", t0+100)
		f.fail(t, bob, t0+12, "rate_limit", "rate_limit", t0+100)
		f.fail(t, ann, t0+13, "rate_limit", "rate_limit", t0+200)
		waits := []time.Duration{}
		for _, timer := range f.timers {
			waits = append(waits, timer.wait)
		}
		apiWantValue(t, "timer waits", any(waits), any([]time.Duration{101 * time.Second, 201 * time.Second}))

		f.clock = t0 + 101
		f.timers[0].fire()
		f.clock = t0 + 150
		f.fail(t, bob, t0+150, "rate_limit", "rate_limit", t0+300)
		f.fail(t, ann, t0+151, "rate_limit", "rate_limit", t0+300)
		if len(f.timers) != 3 || f.timers[2].wait != 151*time.Second {
			t.Fatalf("want a third timer 151s out, got %v", f.timers)
		}
	})

	t.Run("a member moving between accounts is patched when it leaves a limit and when it joins one, with or without a model_call", func(t *testing.T) {
		for _, c := range []struct {
			name   string
			from   string
			report string
			want   []map[string]any
		}{
			{"leaving the limited account with a success clears it", "acct-1",
				`{"runtime":"claude","account":"acct-2","model_call":{"last_success_ts":1800000020}}`, nil},
			{"joining the limited account with an unchanged success time shows it", "acct-2",
				`{"runtime":"claude","account":"acct-1","model_call":{"last_success_ts":1800000005}}`,
				[]map[string]any{modelCallWarning("rate_limit", "rate_limit", t0+10, t0+3600, true)}},
			{"joining the limited account with no model_call shows it", "acct-2",
				`{"runtime":"claude","account":"acct-1"}`,
				[]map[string]any{modelCallWarning("rate_limit", "rate_limit", t0+10, t0+3600, true)}},
		} {
			t.Run(c.name, func(t *testing.T) {
				f := newModelCallFixture(t)
				ann := f.staff(t, Member{ID: "m-ann"}, "", c.from)
				bob := f.staff(t, Member{ID: "m-bob"}, "", "acct-1")
				f.succeed(t, ann, t0+5)
				f.fail(t, bob, t0+10, "rate_limit", "rate_limit", t0+3600)
				dashboard := apiTestListen(t, f.api, "")

				f.report(t, ann, c.report)
				dashboard.wantFrames(modelCallMonitoringSignal("m-ann", "m-ann"), modelCallPatch("m-ann", "m-ann"))
				lastSuccess := t0 + 5
				if c.want == nil {
					lastSuccess = t0 + 20
				}
				f.wantModelCall(t, "m-ann", lastSuccess, c.want...)
				f.wantModelCall(t, "m-bob", 0, modelCallWarning("rate_limit", "rate_limit", t0+10, t0+3600, false))
			})
		}
	})
}
