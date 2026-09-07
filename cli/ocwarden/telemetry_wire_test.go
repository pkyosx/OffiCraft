package main

import (
	"maps"
	"net/http"
	"reflect"
	"sort"
	"testing"
	"time"
)

// realHeartbeat drives runOnce — the producer that owns the heartbeat callsite —
// over faked shell probes and returns the body it actually posted.
func realHeartbeat(t *testing.T) map[string]any {
	t.Helper()
	runner := fakeRunner{out: fakeProbes}
	bodies := wireBodies(t, func(base string) {
		result := runOnce(Config{Base: base, Token: "tok", ID: "m-1"},
			func() map[string]any { return collectHardware(runner, "darwin") },
			func() string { return readMachineName(runner) },
			httpPoster(&http.Client{Timeout: 5 * time.Second}, base, "tok"),
			func() map[string]string { return map[string]string{"ocwarden": "abc123abc123"} },
			func() map[string]any {
				return map[string]any{"version": "2.1.211", "cred_file": true,
					"sub_readable": true, "keychain": false}
			},
			func() string { return string(shapeAnchor) },
			func() string { return string(effectUnproven) },
			func() map[string]any {
				return map[string]any{
					"claude": map[string]any{"installed": true, "logged_in": true, "version": "2.1.211"},
					"codex":  map[string]any{"installed": true, "logged_in": true, "version": "0.52.0"},
				}
			})
		want := ReportResult{Posted: true, Status: 200, Reason: "posted"}
		if result != want {
			t.Errorf("runOnce = %+v, want %+v", result, want)
		}
	})
	if len(bodies) != 1 {
		t.Fatalf("the heartbeat producer put %d bodies on the wire, want exactly 1", len(bodies))
	}
	return bodies[0]
}

// realCommandReceipt drives newCommandReporter's closure — the one production
// wires onto CommandDeps.Report — and returns the body it posted.
func realCommandReceipt(t *testing.T) map[string]any {
	t.Helper()
	bodies := wireBodies(t, func(base string) {
		report := newCommandReporter(Config{Base: base, Token: "tok", ID: "m-1"})
		if err := report(CommandResult{MemberID: "m-1", RPC: rpcStop, OK: true,
			Reason: "stopped", Log: "session=member-m-1: stopped",
			At: "2026-01-01T00:00:00Z"}); err != nil {
			t.Fatalf("the command_result reporter did not deliver: %v", err)
		}
	})
	if len(bodies) != 1 {
		t.Fatalf("the reporter put %d bodies on the wire, want exactly 1", len(bodies))
	}
	return bodies[0]
}

// realCommandReceipts drives the four command_result producers in command.go
// through the real reporter and returns the bodies keyed by rpc verb. Each verb
// builds its own CommandResult, so each gets its own run: a shared one would let
// three of them be paid for by driving the fourth.
func realCommandReceipts(t *testing.T) map[string]map[string]any {
	t.Helper()
	receipts := map[string]map[string]any{}
	for _, one := range []struct {
		rpc  string
		args map[string]any
	}{
		{rpcStart, map[string]any{
			"member_id": "m-1", "persona_context": "you are x", "member_token": "tok-abc",
			"role": "agent", "task_type": "onboard", "model": "opus", "effort": "high",
			"session_name": "member-m-1"}},
		{rpcWorkerStop, map[string]any{"worker_id": "ow-9"}},
		{rpcStop, map[string]any{"member_id": "m-5", "session_name": "member-m-5"}},
		{rpcUninstall, map[string]any{"member_id": "m-5", "session_name": "member-m-5"}},
	} {
		bodies := wireBodies(t, func(base string) {
			deps := CommandDeps{
				Spawn:    func(StartParams) SpawnOutcome { return SpawnOutcome{OK: true} },
				Stop:     func(string) (ok, noop bool) { return true, false },
				Teardown: func() (bool, string) { return true, "teardown complete\n" },
				Exit:     func(int) {},
				Report:   newCommandReporter(Config{Base: base, Token: "tok", ID: "m-1"}),
			}
			if err := dispatchCommand(&Command{RPC: one.rpc, Args: one.args}, deps); err != nil {
				t.Fatalf("dispatch %s: %v", one.rpc, err)
			}
		})
		if len(bodies) != 1 {
			t.Fatalf("%s put %d bodies on the wire, want exactly 1", one.rpc, len(bodies))
		}
		receipts[one.rpc] = bodies[0]
	}
	return receipts
}

// realSelfUpdateAnnounce drives the real self-update announcement over the real
// poster and returns the body it posted.
func realSelfUpdateAnnounce(t *testing.T) map[string]any {
	t.Helper()
	bodies := wireBodies(t, func(base string) {
		up := &updater{
			post:    httpPoster(&http.Client{Timeout: 5 * time.Second}, base, "tok"),
			agentID: "m-1",
			logf:    func(string, ...any) {},
			lastSwap: &selfUpdateEvent{Binary: "ocwarden", OldHash: "aaa", NewHash: "bbb",
				At: "2026-01-01T00:00:00Z"},
		}
		up.announceSelfUpdate()
	})
	if len(bodies) != 1 {
		t.Fatalf("the self-update announce put %d bodies on the wire, want exactly 1", len(bodies))
	}
	return bodies[0]
}

// takeStampedAt pulls the receipt's server-set timestamp out of the payload,
// asserts it is a real RFC3339 instant, and returns what is left — the part that
// can be compared against a written-out expectation.
func takeStampedAt(t *testing.T, name string, payload map[string]any) map[string]any {
	t.Helper()
	receipt, ok := payload["command_result"].(map[string]any)
	if !ok {
		t.Fatalf("the %s receipt carries no command_result object; payload = %#v", name, payload)
	}
	at, ok := receipt["at"].(string)
	if !ok {
		t.Fatalf("the %s receipt carries no string at; receipt = %#v", name, receipt)
	}
	if _, err := time.Parse(time.RFC3339, at); err != nil {
		t.Errorf("the %s receipt stamps at=%q, which is not RFC3339: %v", name, at, err)
	}
	rest := map[string]any{}
	for key, value := range receipt {
		if key != "at" {
			rest[key] = value
		}
	}
	return rest
}

// TestWardenTelemetryUplinkBodies confronts every body this module posts to the
// telemetry endpoint with the schema the frozen spec declares for that route, and
// with a written-out expectation of the body itself. The server decodes the route
// with DisallowUnknownFields, so a top-level undeclared key rejects the whole
// report; a NESTED one is answered 200, stored, and then never read again.
func TestWardenTelemetryUplinkBodies(t *testing.T) {
	declared := frozenRequestSchema(t, "post", telemetryPath)
	heartbeat := realHeartbeat(t)
	receipts := realCommandReceipts(t)
	cases := map[string]map[string]any{
		"heartbeat":                  heartbeat,
		"command_result":             realCommandReceipt(t),
		"command_result-start":       receipts[rpcStart],
		"command_result-worker_stop": receipts[rpcWorkerStop],
		"command_result-stop":        receipts[rpcStop],
		"command_result-uninstall":   receipts[rpcUninstall],
		"self_update":                realSelfUpdateAnnounce(t),
	}

	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)

	walked := map[string]int{}
	for _, name := range names {
		payload := cases[name]
		walked[name+" → "+telemetryPath]++
		if extra := undeclaredPayloadKeys(payload, declared); len(extra) > 0 {
			t.Errorf("%s carries key(s) the frozen spec does not declare: %v; payload = %#v",
				name, extra, payload)
		}
		if missing := missingRequiredKeys(payload, declared); len(missing) > 0 {
			t.Errorf("%s omits key(s) the frozen spec requires: %v; payload = %#v",
				name, missing, payload)
		}
		if bad := mistypedPayloadValues(payload, declared); len(bad) > 0 {
			t.Errorf("%s sends declared key(s) with the wrong wire type: %v; payload = %#v",
				name, bad, payload)
		}
	}

	if committed := manifestUplinkPaths(t, "cli/ocwarden/telemetry_wire_test.go"); !maps.Equal(walked, committed) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but %v was walked against "+
			"the frozen schema", committed, walked)
	}

	wantHeartbeat := map[string]any{
		"machine":  "Seth's MacBook Pro",
		"binaries": map[string]any{"ocwarden": "abc123abc123"},
		"hardware": map[string]any{"battery_pct": float64(87), "ac_power": true, "cpu_pct": float64(20), "ram_pct": 63.6},
		"claude":   map[string]any{"version": "2.1.211", "cred_file": true, "sub_readable": true, "keychain": false},
		"runtimes": map[string]any{
			"claude": map[string]any{"installed": true, "logged_in": true, "version": "2.1.211"},
			"codex":  map[string]any{"installed": true, "logged_in": true, "version": "0.52.0"},
		},
		"warden_shape":   "anchor",
		"cutover_effect": "unproven",
	}
	if !reflect.DeepEqual(heartbeat, wantHeartbeat) {
		t.Errorf("heartbeat =\n  %#v\nwant\n  %#v", heartbeat, wantHeartbeat)
	}

	wantDirect := map[string]any{"command_result": map[string]any{
		"member_id": "m-1", "worker_id": "", "rpc": "stop", "ok": true,
		"reason": "stopped", "log": "session=member-m-1: stopped", "at": "2026-01-01T00:00:00Z",
	}}
	if !reflect.DeepEqual(cases["command_result"], wantDirect) {
		t.Errorf("command_result =\n  %#v\nwant\n  %#v", cases["command_result"], wantDirect)
	}

	wantReceipts := map[string]map[string]any{
		"command_result-start": {"member_id": "m-1", "worker_id": "", "rpc": "start",
			"ok": true, "reason": "", "log": ""},
		"command_result-worker_stop": {"member_id": "", "worker_id": "ow-9", "rpc": "worker_stop",
			"ok": true, "reason": "stopped", "log": "session=worker-ow-9: stopped"},
		"command_result-stop": {"member_id": "m-5", "worker_id": "", "rpc": "stop",
			"ok": true, "reason": "stopped", "log": "session=member-m-5: stopped"},
		"command_result-uninstall": {"member_id": "m-5", "worker_id": "", "rpc": "uninstall",
			"ok": true, "reason": "uninstalled", "log": "teardown complete\n"},
	}
	for _, name := range []string{"command_result-start", "command_result-worker_stop",
		"command_result-stop", "command_result-uninstall"} {
		got := takeStampedAt(t, name, cases[name])
		if !reflect.DeepEqual(got, wantReceipts[name]) {
			t.Errorf("%s receipt =\n  %#v\nwant (besides at)\n  %#v", name, got, wantReceipts[name])
		}
	}

	wantSelfUpdate := map[string]any{"self_update": map[string]any{
		"binary": "ocwarden", "old_hash": "aaa", "new_hash": "bbb", "at": "2026-01-01T00:00:00Z",
	}}
	if !reflect.DeepEqual(cases["self_update"], wantSelfUpdate) {
		t.Errorf("self_update =\n  %#v\nwant\n  %#v", cases["self_update"], wantSelfUpdate)
	}
}
