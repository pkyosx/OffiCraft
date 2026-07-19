package main

// api_members_restartself_test.go — restart_self (POST /api/self/refocus, the
// T-4c71 self-triggered recycle). The authz faces (machine floor, owner 404,
// offline 409) are pinned black-box in conformance/test_auth_matrix.py; the
// two behaviours that need a LIVE SSE session + a stamped boot_ts — the
// online-positive 200 stamp and the 429 minimum-liveness refusal — are pinned
// here where the harness can drive the hub + gauge directly.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// doRestartSelf drives POST /api/self/refocus as sub (agent scope), with an
// optional JSON body.
func doRestartSelf(api *apiServer, sub, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest("POST", "/api/self/refocus", nil)
	} else {
		r = httptest.NewRequest("POST", "/api/self/refocus", strings.NewReader(body))
	}
	claims := map[string]any{"sub": sub, "scope": "agent"}
	r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	api.HandleRestartSelfApiSelfRefocusPost(rec, r)
	return rec
}

// online marks a member online by registering an SSE listener (the sole
// authority for the online projection); returns a cleanup.
func online(t *testing.T, api *apiServer, id string) func() {
	t.Helper()
	l, err := api.hub.Connect(id, "")
	if err != nil {
		t.Fatalf("hub.Connect(%s): %v", id, err)
	}
	return func() { api.hub.Disconnect(l) }
}

func TestRestartSelfStampsRefocusWhenOnlineAndPastLivenessFloor(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "rs-ok", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	defer online(t, api, "rs-ok")()
	// A session that connected well before the liveness floor.
	api.gauge.Set("rs-ok", map[string]any{"boot_ts": nowSecs() - (minSelfRestartSecs + 100)})

	rec := doRestartSelf(api, "rs-ok", `{"reason":"context near the handover line"}`)
	if rec.Code != 200 {
		t.Fatalf("online + past-floor self-restart: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	m, err := dal.GetMember("rs-ok")
	if err != nil || m == nil {
		t.Fatalf("reload rs-ok: %v", err)
	}
	if m.RefocusSince <= 0.0 {
		t.Fatalf("restart_self must stamp refocus_since; got %v", m.RefocusSince)
	}
}

func TestRestartSelfRefusesWithinLivenessFloor(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "rs-fresh", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	defer online(t, api, "rs-fresh")()
	// A session that connected 1 minute ago — inside the 10-minute floor.
	api.gauge.Set("rs-fresh", map[string]any{"boot_ts": nowSecs() - 60})

	rec := doRestartSelf(api, "rs-fresh", "")
	if rec.Code != 429 {
		t.Fatalf("fresh session self-restart: want 429, got %d %s", rec.Code, rec.Body.String())
	}
	m, _ := dal.GetMember("rs-fresh")
	if m.RefocusSince != 0.0 {
		t.Fatalf("a refused self-restart must not stamp refocus_since; got %v", m.RefocusSince)
	}
}

func TestRestartSelfRefusesWhenOffline(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "rs-off", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	// No hub connection → not online. boot_ts old enough that the liveness
	// floor would pass, proving the 409 is the ONLINE gate, not the floor.
	api.gauge.Set("rs-off", map[string]any{"boot_ts": nowSecs() - (minSelfRestartSecs + 100)})

	rec := doRestartSelf(api, "rs-off", "")
	if rec.Code != 409 {
		t.Fatalf("offline self-restart: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	m, _ := dal.GetMember("rs-off")
	if m.RefocusSince != 0.0 {
		t.Fatalf("a refused self-restart must not stamp refocus_since; got %v", m.RefocusSince)
	}
}

func TestRestartSelfMissingBootTsFailsOpen(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "rs-noboot", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	defer online(t, api, "rs-noboot")()
	// No gauge entry → no boot_ts (server-restart amnesia): the liveness guard
	// must FAIL OPEN, never a false 429 on a long-lived session.

	rec := doRestartSelf(api, "rs-noboot", "")
	if rec.Code != 200 {
		t.Fatalf("missing boot_ts must fail open: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	m, _ := dal.GetMember("rs-noboot")
	if m.RefocusSince <= 0.0 {
		t.Fatalf("fail-open self-restart must stamp refocus_since; got %v", m.RefocusSince)
	}
}

func TestRestartSelfOwnerHasNoRosterRow404(t *testing.T) {
	api, _ := newGateTestAPI(t)
	// The owner's sub carries no roster row: self-op is agent-only by
	// construction → resolveSelf 404 before any gate.
	rec := doRestartSelf(api, "owner", "")
	if rec.Code != 404 {
		t.Fatalf("owner self-restart: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}
