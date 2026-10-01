package main

import (
	"maps"
	"reflect"
	"sort"
	"testing"
)

// realLoginReports drives the reporter production wires onto the login relay
// once per state the relay reports, and returns each body it posted.
func realLoginReports(t *testing.T) map[string]map[string]any {
	t.Helper()
	reports := map[string]loginReport{
		"awaiting_code": {LoginID: "rl-1", State: "awaiting_code", AuthURL: "https://claude.ai/oauth/authorize?x=1"},
		"verifying":     {LoginID: "rl-1", State: "verifying"},
		"succeeded": {LoginID: "rl-1", State: "succeeded",
			Account: &loginAccount{Email: "owner@example.test", OrgName: "Example Org"}},
		"failed":  {LoginID: "rl-1", State: "failed", Reason: "Login failed: Request failed with status code 400"},
		"expired": {LoginID: "rl-1", State: "expired", Reason: "no code arrived within 10m0s"},
	}
	bodies := map[string]map[string]any{}
	for name, rep := range reports {
		posted := wireBodies(t, func(base string) {
			newLoginReporter(Config{Base: base, Token: "tok", ID: "m-1"})(rep)
		})
		if len(posted) != 1 {
			t.Fatalf("%s put %d bodies on the wire, want exactly 1", name, len(posted))
		}
		bodies[name] = posted[0]
	}
	return bodies
}

// TestWardenRuntimeLoginUplinkBodies confronts every body the login reporter
// posts with the schema the frozen spec declares for the route, and with a
// written-out expectation of the body itself.
func TestWardenRuntimeLoginUplinkBodies(t *testing.T) {
	declared := frozenRequestSchema(t, "post", runtimeLoginPath)
	cases := realLoginReports(t)

	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)
	walked := map[string]int{}
	for _, name := range names {
		payload := cases[name]
		if extra := undeclaredPayloadKeys(payload, declared); len(extra) > 0 {
			t.Errorf("%s carries key(s) the frozen spec does not declare: %v; payload = %#v", name, extra, payload)
		}
		if missing := missingRequiredKeys(payload, declared); len(missing) > 0 {
			t.Errorf("%s omits key(s) the frozen spec requires: %v; payload = %#v", name, missing, payload)
		}
		if bad := mistypedPayloadValues(payload, declared); len(bad) > 0 {
			t.Errorf("%s sends declared key(s) with the wrong wire type: %v; payload = %#v", name, bad, payload)
		}
	}
	walked["runtime-login → "+runtimeLoginPath] = 1
	if committed := manifestUplinkPaths(t, "cli/ocwarden/runtimelogin_wire_test.go"); !maps.Equal(walked, committed) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but %v was walked", committed, walked)
	}

	want := map[string]map[string]any{
		"awaiting_code": {"login_id": "rl-1", "state": "awaiting_code", "auth_url": "https://claude.ai/oauth/authorize?x=1"},
		"verifying":     {"login_id": "rl-1", "state": "verifying"},
		"succeeded": {"login_id": "rl-1", "state": "succeeded",
			"account": map[string]any{"email": "owner@example.test", "org_name": "Example Org"}},
		"failed":  {"login_id": "rl-1", "state": "failed", "reason": "Login failed: Request failed with status code 400"},
		"expired": {"login_id": "rl-1", "state": "expired", "reason": "no code arrived within 10m0s"},
	}
	if !reflect.DeepEqual(cases, want) {
		t.Errorf("login report bodies =\n  %#v\nwant\n  %#v", cases, want)
	}
}
