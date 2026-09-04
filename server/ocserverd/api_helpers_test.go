package main

// api_helpers_test.go — the ONE member-API target resolver and its scope.
//
// resolveMember(id, scope) folds "no row" and "soft-removed" always, and
// kind='outsource' only when the caller passes staffOnly. The population is a
// REQUIRED PARAMETER, not a second function name (owner ruling 2026-08-28:
// 「只有某些行為如果真的需要只拿正職或外包，才下額外參數指定」): a name can be
// omitted by whoever writes the NEXT verb, a parameter cannot, and its zero
// value refuses instead of widening.
//
// 🔑 WHY OPENING THE ITEM DOOR GIVES NOTHING AWAY: GET /api/members already
// LISTS outsource rows to the same principal (ListMembers, whose
// `WHERE kind != 'outsource'` T-14 項目 6 removed; the P7 convergence
// rc-2786636f30e5). The item door refusing what the list
// door hands out was two doors onto one row disagreeing about who may open it —
// and the cockpit paid for that disagreement with one guaranteed 404 plus one
// whole-roster refetch on every contractor chat line.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestResolveMember_ReadsOutsource pins the widened READ door, and pins the
// write door NEXT TO IT so the pair can never drift into "everything opened".
// Mutant: putting the kind='outsource' arm back into resolveMember → the read
// half goes red; pointing PATCH at resolveMember → the write half goes red.
func TestResolveMember_ReadsOutsource(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	if _, err := api.resolveMember(workerID, anyMember); err != nil {
		t.Fatalf("resolveMember(%s) must RESOLVE an outsource row now, got %v",
			workerID, err)
	}
	if _, err := api.resolveMember(workerID, staffOnly); !errors.Is(err, errNotFound) {
		t.Fatalf("resolveMember(%s, staffOnly) must still refuse an outsource row, got %v",
			workerID, err)
	}

	// The read door answers — this is the 404 the cockpit used to eat on every
	// contractor chat line, together with a whole-roster refetch.
	rec := httptest.NewRecorder()
	api.HandleGetMemberApiMembersMemberIdGet(rec,
		taskReq(t, "GET", "/api/members/"+workerID, nil, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/members/{ow-}: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if dto := decodeBody[memberDTO](t, rec); dto.ID != workerID || dto.Kind != KindOutsource {
		t.Fatalf("read DTO = %+v, want the worker's own row", dto)
	}

	// ...and the write door does NOT.
	rec = httptest.NewRecorder()
	api.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/"+workerID,
			map[string]any{"name": "hijack"}, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH /api/members/{ow-}: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestResolveMember_UnsetScopeRefuses pins the reason this is a parameter and
// not two function names: the population a caller serves cannot be left
// unstated. A memberScope that arrives at its zero value is a call site that
// never chose, and the zero value refuses rather than falling back to the wider
// population — which is the exact failure this ticket exists to remove.
// Mutant: making memberScopeUnset behave as anyMember → red.
func TestResolveMember_UnsetScopeRefuses(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	var never memberScope // a caller that never said which population it serves
	if _, err := api.resolveMember(workerID, never); !errors.Is(err, errScopeUnset) {
		t.Fatalf("unset scope must refuse with errScopeUnset, got %v", err)
	}
	// Same for a staff row: the refusal is about the CALLER not choosing, not
	// about which row was asked for.
	if err := api.dal.PutMember(fullMember("mira")); err != nil {
		t.Fatalf("put member: %v", err)
	}
	if _, err := api.resolveMember("mira", never); !errors.Is(err, errScopeUnset) {
		t.Fatalf("unset scope on a staff row must refuse too, got %v", err)
	}
}

// TestStaffOnlyVerbsStillRefuseOutsource is the other half of the ticket, and
// the half nobody would notice breaking: each of these verbs passes staffOnly,
// and a future edit that "tidies" one to anyMember opens it silently — an agent
// that can be force-stopped through two different funnels, or handed a staff
// boot document, does not report it.
//
// 🔴 Deliberately a TABLE over the whole set rather than one test per verb: a
// new member verb that copies the wrong resolver is caught by adding one row.
// The set is every staffOnly call site — activate, deactivate, dismiss,
// bootstrap, force-stop, accelerated-stop, refocus, webhook create/update/
// revoke, and the public /in inlet. GET /api/members/{id}/webhooks (list) is
// NOT here because it passes anyMember by design.
func TestStaffOnlyVerbsStillRefuseOutsource(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	// The webhook rows exist so the endpoint doors can actually be reached:
	// resolveWebhook folds an ABSENT endpoint onto the same errNotFound as a
	// refused member, so without a real row those cases would answer 404 for the
	// wrong reason and pin nothing. One row per case — the revoke case deletes
	// its own if the scope is ever widened.
	seedHook := func(endpointID string) string {
		t.Helper()
		token := newWebhookToken()
		if err := api.dal.PutWebhookEndpoint(WebhookEndpoint{
			Token:      token,
			MemberID:   workerID,
			EndpointID: endpointID,
			Status:     WebhookStatusEnabled,
			CreatedTS:  nowSecs(),
			Platform:   WebhookPlatformGeneric,
		}); err != nil {
			t.Fatalf("seed webhook %s: %v", endpointID, err)
		}
		return token
	}
	seedHook("pr-events")
	seedHook("legacy-hook")
	inletToken := seedHook("inlet")

	cases := []struct {
		name string
		// wantCode is the refusal this door owes an ow- id. 404 everywhere the
		// caller is authenticated and addressed the member by id; see the /in
		// row for the one door whose refusal is shaped differently.
		wantCode int
		call     func(*httptest.ResponseRecorder)
		// also runs after the status assertion, for a door whose real refusal
		// is not visible in the status line.
		also func(*testing.T)
	}{
		{name: "activate", wantCode: http.StatusNotFound, call: func(rec *httptest.ResponseRecorder) {
			api.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
				taskReq(t, "POST", "/api/members/"+workerID+"/activate", nil, wireOwnerID, "owner"), workerID)
		}},
		{name: "deactivate", wantCode: http.StatusNotFound, call: func(rec *httptest.ResponseRecorder) {
			api.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
				taskReq(t, "POST", "/api/members/"+workerID+"/deactivate", nil, wireOwnerID, "owner"), workerID)
		}},
		{name: "dismiss", wantCode: http.StatusNotFound, call: func(rec *httptest.ResponseRecorder) {
			api.HandleDismissMemberApiMembersMemberIdDelete(rec,
				taskReq(t, "DELETE", "/api/members/"+workerID, nil, wireOwnerID, "owner"), workerID)
		}},
		// An explicit role is passed so the boot package would BUILD if the door
		// opened: without it a widened scope could still 404 on an unknown role
		// key and look like the refusal this row is asserting.
		{name: "bootstrap", wantCode: http.StatusNotFound, call: func(rec *httptest.ResponseRecorder) {
			api.HandleBootstrapApiBootstrapPost(rec,
				taskReq(t, "POST", "/api/bootstrap",
					map[string]any{"member_id": workerID, "role": seedRoleAssistant},
					wireOwnerID, "owner"))
		}},
		{name: "force-stop", wantCode: http.StatusNotFound, call: func(rec *httptest.ResponseRecorder) {
			api.HandleForceStopMemberApiMembersMemberIdForceStopPost(rec,
				taskReq(t, "POST", "/api/members/"+workerID+"/force-stop", nil, wireOwnerID, "owner"), workerID)
		}},
		{name: "accelerated-stop", wantCode: http.StatusNotFound, call: func(rec *httptest.ResponseRecorder) {
			api.HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost(rec,
				taskReq(t, "POST", "/api/members/"+workerID+"/accelerated-stop", nil, wireOwnerID, "owner"), workerID)
		}},
		{name: "refocus", wantCode: http.StatusNotFound, call: func(rec *httptest.ResponseRecorder) {
			api.HandleRefocusMemberApiMembersMemberIdRefocusPost(rec,
				taskReq(t, "POST", "/api/members/"+workerID+"/refocus", nil, wireOwnerID, "owner"), workerID)
		}},
		{name: "webhook-create", wantCode: http.StatusNotFound, call: func(rec *httptest.ResponseRecorder) {
			api.HandleCreateWebhookApiMembersMemberIdWebhooksPost(rec,
				taskReq(t, "POST", "/api/members/"+workerID+"/webhooks",
					map[string]any{"endpoint_id": "smuggled"}, wireOwnerID, "owner"), workerID)
		}},
		{name: "webhook-update", wantCode: http.StatusNotFound, call: func(rec *httptest.ResponseRecorder) {
			api.HandleUpdateWebhookApiMembersMemberIdWebhooksEndpointIdPatch(rec,
				taskReq(t, "PATCH", "/api/members/"+workerID+"/webhooks/pr-events",
					map[string]any{"status": WebhookStatusDisabled}, wireOwnerID, "owner"),
				workerID, "pr-events")
		}},
		{name: "webhook-revoke", wantCode: http.StatusNotFound, call: func(rec *httptest.ResponseRecorder) {
			api.HandleDeleteWebhookApiMembersMemberIdWebhooksEndpointIdDelete(rec,
				taskReq(t, "DELETE", "/api/members/"+workerID+"/webhooks/legacy-hook", nil, wireOwnerID, "owner"),
				workerID, "legacy-hook")
		}},
		// 🔴 The ONE row whose refusal is not a 404, and deliberately so: /in is
		// the unauthenticated public inlet, and its whole contract is that every
		// outcome answers the SAME silent 200 so a caller can never learn whether
		// an endpoint exists. The refusal is therefore only visible BEHIND the
		// response — no synthetic chat, and the endpoint stamped
		// dropped:member_gone — which is what `also` asserts.
		{name: "public-in", wantCode: http.StatusOK, call: func(rec *httptest.ResponseRecorder) {
			req := httptest.NewRequest("POST", "/in?t="+inletToken,
				strings.NewReader("PR #42 merged"))
			api.HandleReceiveWebhookInPost(rec, req,
				HandleReceiveWebhookInPostParams{T: &inletToken})
		}, also: func(t *testing.T) {
			msgs, err := api.dal.ListChatInvolving(workerID, 50)
			if err != nil {
				t.Fatalf("list chat: %v", err)
			}
			if len(msgs) != 0 {
				t.Fatalf("POST /in must synthesise NO chat for a contractor endpoint, got %d: %+v",
					len(msgs), msgs)
			}
			e, err := api.dal.GetWebhookByToken(inletToken)
			if err != nil || e == nil {
				t.Fatalf("read back inlet endpoint: %v %+v", err, e)
			}
			if e.DeliveredCount != 0 || e.LastDropReason != WebhookDropReasonMemberGone {
				t.Fatalf("inlet must record a member_gone drop, got delivered=%d drop=%q",
					e.DeliveredCount, e.LastDropReason)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c.call(rec)
			if rec.Code != c.wantCode {
				t.Fatalf("%s on an ow- id must stay %d, got %d %s",
					c.name, c.wantCode, rec.Code, rec.Body.String())
			}
			if c.also != nil {
				c.also(t)
			}
		})
	}
}

func TestUpdateMember_RuntimeRoundTripsAndValidates(t *testing.T) {
	api := newTasksTestServer(t)
	if err := api.dal.PutMember(fullMember("mira")); err != nil {
		t.Fatalf("put member: %v", err)
	}
	rec := httptest.NewRecorder()
	api.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/mira",
			map[string]any{"runtime": RuntimeCodex}, wireOwnerID, "owner"), "mira")
	if rec.Code != http.StatusOK {
		t.Fatalf("Codex PATCH: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody[memberDTO](t, rec).Runtime; got != RuntimeCodex {
		t.Fatalf("runtime = %q, want codex", got)
	}

	rec = httptest.NewRecorder()
	api.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/mira",
			map[string]any{"runtime": "unknown"}, wireOwnerID, "owner"), "mira")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown runtime: want 422, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestGetMember_WorkerSelfReadResolves (T-ea82): the ONE exception to the ow-
// 404 — a worker reading its OWN row (the ocagent recycle/wind-down hooks'
// refetch) gets the member DTO, desired_state + refocus_since included; the
// same worker targeting ANOTHER ow- id stays 404. Mutant: dropping the
// self-read fallback in HandleGetMember → the self case 404s (red).
func TestGetMember_WorkerSelfReadResolves(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.RefocusSince = 1234.5
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("stamp refocus: %v", err)
	}
	seedWorkerAnchors(t, api, *w)

	rec := httptest.NewRecorder()
	api.HandleGetMemberApiMembersMemberIdGet(rec,
		taskReq(t, "GET", "/api/members/"+workerID, nil, workerID, "agent"), workerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("self-read GET /api/members/{ow-}: want 200, got %d %s",
			rec.Code, rec.Body.String())
	}
	dto := decodeBody[memberDTO](t, rec)
	if dto.ID != workerID || dto.Kind != KindOutsource {
		t.Fatalf("self-read DTO = %+v, want the worker's own row", dto)
	}
	if dto.RefocusSince != 1234.5 || dto.DesiredState != DesiredStateOnline {
		t.Fatalf("self-read must expose refocus_since/desired_state (the recycle-hook "+
			"fields), got refocus=%v desired=%q", dto.RefocusSince, dto.DesiredState)
	}

	// Another worker's id from the same token stays the pre-fold 404.
	otherID := "ow-" + newHexID(6)
	if err := api.dal.PutOutsourceWorker(OutsourceWorker{
		ID: otherID, Codename: "S-" + otherID, TaskID: "t-x",
		Status: WorkerStatusAssigned, DesiredState: DesiredStateOnline,
	}); err != nil {
		t.Fatalf("seed other worker: %v", err)
	}
	// 🔴 This used to assert 404 and now asserts 200 — a DELIBERATE change, not
	// a relaxed test. Reading another agent's roster row was never contained by
	// this door: GET /api/members lists every row, outsource included, to the
	// same principal. The item door refusing it only cost the cockpit a wasted
	// request; it withheld nothing.
	rec = httptest.NewRecorder()
	api.HandleGetMemberApiMembersMemberIdGet(rec,
		taskReq(t, "GET", "/api/members/"+otherID, nil, workerID, "agent"), otherID)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-worker GET is a read and now resolves, got %d %s",
			rec.Code, rec.Body.String())
	}
}
