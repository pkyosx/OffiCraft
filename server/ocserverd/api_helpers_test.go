package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// apiHelpersGated drives one request carrying token through the PRODUCTION auth
// middleware and hands the verified request to probe — the only way a claims
// context is ever built, so the identity accessors read what a real handler
// reads.
func apiHelpersGated(t *testing.T, api *apiServer, d *DAL, token string, probe func(*http.Request)) {
	t.Helper()
	reached := false
	gate := requireAuth(api.keys, api.authPasswordChangedAt, d.GetMember,
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			reached = true
			probe(r)
		}))
	req := httptest.NewRequest("GET", "/api/probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if !reached {
		t.Fatalf("the auth gate refused the credential: %d %s", rec.Code, rec.Body.String())
	}
}

func apiHelpersUngated() *http.Request {
	return httptest.NewRequest("GET", "/api/probe", nil)
}

func apiHelpersWardenToken(t *testing.T, api *apiServer, d *DAL, machineID string) string {
	t.Helper()
	m, err := d.GetMember(machineID)
	if err != nil || m == nil {
		t.Fatalf("GetMember(%q): %v %v", machineID, m, err)
	}
	token, err := api.mintWardenToken(*m)
	if err != nil {
		t.Fatalf("mintWardenToken: %v", err)
	}
	return token
}

func apiHelpersWire(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return out
}

func apiHelpersWritten(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("non-JSON body (%d): %s", rec.Code, rec.Body.Bytes())
	}
	return out
}

func apiHelpersMember(t *testing.T, d *DAL, id string) Member {
	t.Helper()
	m, err := d.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("GetMember(%q): %v %v", id, m, err)
	}
	return *m
}

func apiHelpersDecode(t *testing.T, body string, required ...string) (
	LessonsPatchDTO, map[string]bool, bool, *httptest.ResponseRecorder,
) {
	t.Helper()
	var dst LessonsPatchDTO
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/probe", strings.NewReader(body))
	keys, ok := decodeJSONBodyKeys(rec, req, &dst, required...)
	return dst, keys, ok, rec
}

func TestCurrentActor(t *testing.T) {
	t.Run("the owner credential minted at set-password names the owner literal as the caller", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)

		apiHelpersGated(t, api, d, owner, func(r *http.Request) {
			apiWantValue(t, "actor", any(currentActor(r)), any("owner"))
		})
	})

	t.Run("an agent credential minted for a member names that member, whether or not it carries a placement claim", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		apiHelpersGated(t, api, d, apiTestAgentToken(t, api, apiTestPlainAgentID, ""),
			func(r *http.Request) {
				apiWantValue(t, "claim-less actor", any(currentActor(r)), any("kip"))
			})
		apiHelpersGated(t, api, d, apiTestAgentToken(t, api, apiTestPlainAgentID, "m-lab"),
			func(r *http.Request) {
				apiWantValue(t, "claim-bearing actor", any(currentActor(r)), any("kip"))
			})
	})

	t.Run("a warden's permanent credential names the machine member's own id", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		apiHelpersGated(t, api, d, apiHelpersWardenToken(t, api, d, ServerSelfHost),
			func(r *http.Request) {
				apiWantValue(t, "actor", any(currentActor(r)), any("m-server-self"))
			})
	})

	t.Run("a request that never passed the auth gate carries no caller identity at all", func(t *testing.T) {
		apiWantValue(t, "actor", any(currentActor(apiHelpersUngated())), any(""))
	})
}

func TestCurrentScope(t *testing.T) {
	t.Run("the owner credential is owner-scoped while every minted member credential is agent-scoped", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)

		apiHelpersGated(t, api, d, owner, func(r *http.Request) {
			apiWantValue(t, "owner scope", any(currentScope(r)), any("owner"))
		})
		apiHelpersGated(t, api, d, apiTestAgentToken(t, api, apiTestPlainAgentID, "m-lab"),
			func(r *http.Request) {
				apiWantValue(t, "agent scope", any(currentScope(r)), any("agent"))
			})
	})

	t.Run("a warden's permanent credential is agent-scoped too, so scope alone never identifies a machine", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		apiHelpersGated(t, api, d, apiHelpersWardenToken(t, api, d, ServerSelfHost),
			func(r *http.Request) {
				apiWantValue(t, "warden scope", any(currentScope(r)), any("agent"))
			})
	})

	t.Run("a request that never passed the auth gate carries no scope at all", func(t *testing.T) {
		apiWantValue(t, "scope", any(currentScope(apiHelpersUngated())), any(""))
	})
}

func TestPrincipalOfRequest(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	for _, tc := range []struct {
		name   string
		claims map[string]any
		want   principalClass
	}{
		{name: "owner claim", claims: map[string]any{"scope": "owner", "sub": "owner"}, want: principalOwner},
		{name: "assistant member claim", claims: map[string]any{"scope": "agent", "sub": "mira"}, want: principalAdminAgent},
		{name: "ordinary member claim", claims: map[string]any{"scope": "agent", "sub": "kip"}, want: principalAgent},
		{name: "warden member claim", claims: map[string]any{"scope": "agent", "sub": "m-server-self"}, want: principalMachine},
		{name: "unknown member claim is denied by default", claims: map[string]any{"scope": "agent", "sub": "ghost"}, want: principalAgent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/probe", nil)
			req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, tc.claims))
			if got := api.principalOfRequest(req); got != tc.want {
				t.Fatalf("principalOfRequest(%v) = %v, want %v", tc.claims, got, tc.want)
			}
		})
	}
}

func TestUnreadCountsForRequest(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	dalPutChats(t, d,
		dalChat("c-000000000901", "mira", "owner", 100),
		dalChat("c-000000000902", "mira", "owner", 200),
		dalChat("c-000000000903", "kip", "owner", 300),
		dalChat("c-000000000904", "owner", "owner", 400),
	)
	if _, _, err := d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "mira", LastReadTS: 100}); err != nil {
		t.Fatalf("PutChatRead: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/probe", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey,
		map[string]any{"scope": "owner", "sub": "owner"}))
	got, err := api.unreadCountsForRequest(req)
	if err != nil {
		t.Fatalf("unreadCountsForRequest: %v", err)
	}
	want := map[string]int{"mira": 1, "kip": 1, "owner": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unreadCountsForRequest = %v, want %v", got, want)
	}
}

func TestRequestTrigger(t *testing.T) {
	t.Run("the owner credential attributes a durable write to the owner literal", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)

		apiHelpersGated(t, api, d, owner, func(r *http.Request) {
			apiWantValue(t, "trigger", any(requestTrigger(r)), any("owner"))
		})
	})

	t.Run("an agent and a warden credential each attribute the write to their own member id", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		apiHelpersGated(t, api, d, apiTestAgentToken(t, api, apiTestPlainAgentID, "m-lab"),
			func(r *http.Request) {
				apiWantValue(t, "agent trigger", any(requestTrigger(r)), any("kip"))
			})
		apiHelpersGated(t, api, d, apiHelpersWardenToken(t, api, d, ServerSelfHost),
			func(r *http.Request) {
				apiWantValue(t, "warden trigger", any(requestTrigger(r)), any("m-server-self"))
			})
	})

	t.Run("a request carrying no verified identity is attributed to the server rather than to nobody", func(t *testing.T) {
		apiWantValue(t, "trigger", any(requestTrigger(apiHelpersUngated())), any("server"))
	})
}

func TestCurrentMachineClaim(t *testing.T) {
	t.Run("an agent credential minted with a boot host carries that host as its placement claim", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		apiHelpersGated(t, api, d, apiTestAgentToken(t, api, apiTestPlainAgentID, "m-lab"),
			func(r *http.Request) {
				apiWantValue(t, "claim", any(currentMachineClaim(r)), any("m-lab"))
			})
	})

	t.Run("the owner credential, a claim-less agent credential and a warden credential are all three claim-less", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)

		apiHelpersGated(t, api, d, owner, func(r *http.Request) {
			apiWantValue(t, "owner claim", any(currentMachineClaim(r)), any(""))
		})
		apiHelpersGated(t, api, d, apiTestAgentToken(t, api, apiTestPlainAgentID, ""),
			func(r *http.Request) {
				apiWantValue(t, "claim-less agent claim", any(currentMachineClaim(r)), any(""))
			})
		apiHelpersGated(t, api, d, apiHelpersWardenToken(t, api, d, ServerSelfHost),
			func(r *http.Request) {
				apiWantValue(t, "warden claim", any(currentMachineClaim(r)), any(""))
			})
	})

	t.Run("a request that never passed the auth gate carries no placement claim", func(t *testing.T) {
		apiWantValue(t, "claim", any(currentMachineClaim(apiHelpersUngated())), any(""))
	})
}

func TestReceiptReporterMachine(t *testing.T) {
	t.Run("a warden credential names the machine it is, taken from its own sub", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		apiHelpersGated(t, api, d, apiHelpersWardenToken(t, api, d, ServerSelfHost),
			func(r *http.Request) {
				apiWantValue(t, "reporter", any(receiptReporterMachine(r)), any("m-server-self"))
			})
	})

	t.Run("a claim-bearing agent credential names no machine at all, so a member id can never be read as one", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		apiHelpersGated(t, api, d, apiTestAgentToken(t, api, apiTestPlainAgentID, "m-lab"),
			func(r *http.Request) {
				apiWantValue(t, "reporter", any(receiptReporterMachine(r)), any(""))
			})
	})

	t.Run("a claim-less non-warden credential is indistinguishable from a warden here and hands back its own sub — the known residue", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)

		apiHelpersGated(t, api, d, apiTestAgentToken(t, api, apiTestPlainAgentID, ""),
			func(r *http.Request) {
				apiWantValue(t, "claim-less agent reporter", any(receiptReporterMachine(r)), any("kip"))
			})
		apiHelpersGated(t, api, d, owner, func(r *http.Request) {
			apiWantValue(t, "owner reporter", any(receiptReporterMachine(r)), any("owner"))
		})
	})

	t.Run("a request that never passed the auth gate answers the unknown machine", func(t *testing.T) {
		apiWantValue(t, "reporter", any(receiptReporterMachine(apiHelpersUngated())), any(""))
	})
}

func TestDecodeJSONBodyStrict(t *testing.T) {
	t.Run("a well-formed body decodes and answers true with nothing written to the response", func(t *testing.T) {
		var dst LessonsPatchDTO
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/probe",
			strings.NewReader(`{"edits":[{"old":"a","new":"b"}]}`))

		ok := decodeJSONBodyStrict(rec, req, &dst, "edits")

		apiWantValue(t, "ok", any(ok), any(true))
		apiWantValue(t, "status", any(float64(rec.Code)), any(200))
		apiWantValue(t, "body", any(rec.Body.String()), any(""))
		apiWantValue(t, "decoded", any(apiHelpersWire(t, dst)), any(map[string]any{
			"edits": []any{map[string]any{"old": "a", "new": "b"}},
		}))
	})

	t.Run("an unknown key answers false and the same 422 envelope the keys-returning face writes", func(t *testing.T) {
		var dst LessonsPatchDTO
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/probe",
			strings.NewReader(`{"edits":[],"learnings":"wiped"}`))

		ok := decodeJSONBodyStrict(rec, req, &dst, "edits")

		apiWantValue(t, "ok", any(ok), any(false))
		apiWantValue(t, "status", any(float64(rec.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, rec), "validation_error",
			`invalid request body: json: unknown field "learnings"`)
	})

	t.Run("a required key the caller omitted answers false and names the field", func(t *testing.T) {
		var dst LessonsPatchDTO
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/probe", strings.NewReader(`{"allow_shrink":true}`))

		ok := decodeJSONBodyStrict(rec, req, &dst, "edits")

		apiWantValue(t, "ok", any(ok), any(false))
		apiWantValue(t, "status", any(float64(rec.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, rec), "validation_error", "field required: edits")
	})
}

func TestDecodeJSONBodyKeys(t *testing.T) {
	t.Run("a body naming both keys decodes them and reports both as actually sent", func(t *testing.T) {
		dst, keys, ok, rec := apiHelpersDecode(t,
			`{"edits":[{"old":"a"},{"new":"b"}],"allow_shrink":true}`, "edits")

		apiWantValue(t, "ok", any(ok), any(true))
		apiWantValue(t, "status", any(float64(rec.Code)), any(200))
		apiWantValue(t, "body", any(rec.Body.String()), any(""))
		apiWantValue(t, "sent", any(keys), any(map[string]bool{"edits": true, "allow_shrink": true}))
		apiWantValue(t, "decoded", any(apiHelpersWire(t, dst)), any(map[string]any{
			"allow_shrink": true,
			"edits":        []any{map[string]any{"old": "a"}, map[string]any{"new": "b"}},
		}))
	})

	t.Run("an omitted optional key is absent from the sent set while the key that was sent is in it", func(t *testing.T) {
		dst, keys, ok, _ := apiHelpersDecode(t, `{"edits":[]}`, "edits")

		apiWantValue(t, "ok", any(ok), any(true))
		apiWantValue(t, "sent", any(keys), any(map[string]bool{"edits": true}))
		apiWantValue(t, "allow_shrink stayed unset", any(dst.AllowShrink == nil), any(true))
	})

	t.Run("an empty body and an empty object both decode the zero value and report an empty sent set rather than a nil one", func(t *testing.T) {
		dstEmpty, keysEmpty, okEmpty, recEmpty := apiHelpersDecode(t, ``)
		dstObj, keysObj, okObj, recObj := apiHelpersDecode(t, `{}`)

		apiWantValue(t, "empty body ok", any(okEmpty), any(true))
		apiWantValue(t, "empty body status", any(float64(recEmpty.Code)), any(200))
		apiWantValue(t, "empty body sent is not nil", any(keysEmpty == nil), any(false))
		apiWantValue(t, "empty body sent", any(keysEmpty), any(map[string]bool{}))
		apiWantValue(t, "empty body left edits nil", any(dstEmpty.Edits == nil), any(true))
		apiWantValue(t, "empty object ok", any(okObj), any(true))
		apiWantValue(t, "empty object status", any(float64(recObj.Code)), any(200))
		apiWantValue(t, "empty object sent", any(keysObj), any(map[string]bool{}))
		apiWantValue(t, "empty object left edits nil", any(dstObj.Edits == nil), any(true))
	})

	t.Run("an unknown top-level key is refused 422 naming it, and the sent set comes back nil", func(t *testing.T) {
		_, keys, ok, rec := apiHelpersDecode(t, `{"edits":[],"learnings":"wiped"}`, "edits")

		apiWantValue(t, "ok", any(ok), any(false))
		apiWantValue(t, "sent is nil", any(keys == nil), any(true))
		apiWantValue(t, "status", any(float64(rec.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, rec), "validation_error",
			`invalid request body: json: unknown field "learnings"`)
	})

	t.Run("an unknown key nested inside an edit is refused the same way, not silently dropped", func(t *testing.T) {
		_, keys, ok, rec := apiHelpersDecode(t, `{"edits":[{"old_text":"x"}]}`, "edits")

		apiWantValue(t, "ok", any(ok), any(false))
		apiWantValue(t, "sent is nil", any(keys == nil), any(true))
		apiWantValue(t, "status", any(float64(rec.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, rec), "validation_error",
			`invalid request body: json: unknown field "old_text"`)
	})

	t.Run("a required key is refused 422 naming the field, and an empty body counts as omitting it", func(t *testing.T) {
		_, keysPresent, okPresent, recPresent := apiHelpersDecode(t, `{"allow_shrink":true}`, "edits")
		_, keysEmpty, okEmpty, recEmpty := apiHelpersDecode(t, ``, "edits")

		apiWantValue(t, "ok", any(okPresent), any(false))
		apiWantValue(t, "sent is nil", any(keysPresent == nil), any(true))
		apiWantValue(t, "status", any(float64(recPresent.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, recPresent), "validation_error", "field required: edits")
		apiWantValue(t, "empty body ok", any(okEmpty), any(false))
		apiWantValue(t, "empty body sent is nil", any(keysEmpty == nil), any(true))
		apiWantValue(t, "empty body status", any(float64(recEmpty.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, recEmpty), "validation_error", "field required: edits")
	})

	t.Run("a body that is not JSON is refused 422 carrying the parser's own complaint", func(t *testing.T) {
		_, keys, ok, rec := apiHelpersDecode(t, `{{{`, "edits")

		apiWantValue(t, "ok", any(ok), any(false))
		apiWantValue(t, "sent is nil", any(keys == nil), any(true))
		apiWantValue(t, "status", any(float64(rec.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, rec), "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")
	})

	t.Run("two JSON values back to back are refused rather than the first one being taken", func(t *testing.T) {
		_, keys, ok, rec := apiHelpersDecode(t, `{"edits":[]} {"edits":[]}`, "edits")

		apiWantValue(t, "ok", any(ok), any(false))
		apiWantValue(t, "sent is nil", any(keys == nil), any(true))
		apiWantValue(t, "status", any(float64(rec.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, rec), "validation_error",
			"invalid request body: invalid character '{' after top-level value")
	})
}

func TestInternalError(t *testing.T) {
	t.Run("a storage fault answers 500 with the internal_error code and the fault's own text", func(t *testing.T) {
		rec := httptest.NewRecorder()

		internalError(rec, errors.New("disk on fire"))

		apiWantValue(t, "status", any(float64(rec.Code)), any(500))
		apiWantValue(t, "content type", any(rec.Header().Get("Content-Type")), any("application/json"))
		apiWantError(t, apiHelpersWritten(t, rec), "internal_error", "internal error: disk on fire")
	})
}

func TestResolveMember(t *testing.T) {
	t.Run("a live staff row resolves under both scopes and comes back whole", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		anyScope, errAny := api.resolveMember(apiTestPlainAgentID, anyMember)
		staffScope, errStaff := api.resolveMember(apiTestPlainAgentID, staffOnly)

		if errAny != nil || errStaff != nil {
			t.Fatalf("want both to resolve, got %v / %v", errAny, errStaff)
		}
		apiWantValue(t, "any-scope row", any(*anyScope), any(apiHelpersMember(t, d, apiTestPlainAgentID)))
		apiWantValue(t, "staff-scope row", any(*staffScope), any(*anyScope))
		dashboard.wantFrames()
	})

	t.Run("an id nobody ever minted is not found under either scope", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		anyScope, errAny := api.resolveMember("ghost", anyMember)
		staffScope, errStaff := api.resolveMember("ghost", staffOnly)

		if !errors.Is(errAny, errNotFound) || !errors.Is(errStaff, errNotFound) {
			t.Fatalf("want errNotFound twice, got %v / %v", errAny, errStaff)
		}
		apiWantValue(t, "any-scope row is nil", any(anyScope == nil), any(true))
		apiWantValue(t, "staff-scope row is nil", any(staffScope == nil), any(true))
	})

	t.Run("a soft-removed member is not found even though its row is still on disk", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}
		apiWantValue(t, "the row survived the dismiss",
			any(apiHelpersMember(t, d, apiTestPlainAgentID).RosterStatus), any(RosterStatusRemoved))

		row, err := api.resolveMember(apiTestPlainAgentID, anyMember)

		if !errors.Is(err, errNotFound) {
			t.Fatalf("want errNotFound, got %v", err)
		}
		apiWantValue(t, "row is nil", any(row == nil), any(true))
	})

	t.Run("a contractor resolves for the whole-roster scope and is refused for the staff-only one", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship the crate","executor_member_id":"kip"}`); status != 200 {
			t.Fatalf("create task: %d %v", status, data)
		}
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-000000000001", Codename: "Contractor", TaskID: "T-1",
			Status: "assigned", Runtime: "claude", Model: "sonnet", Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		anyScope, errAny := api.resolveMember("ow-000000000001", anyMember)
		staffScope, errStaff := api.resolveMember("ow-000000000001", staffOnly)

		if errAny != nil {
			t.Fatalf("the whole-roster scope must serve a contractor: %v", errAny)
		}
		apiWantValue(t, "any-scope row", any(anyScope.ID), any("ow-000000000001"))
		apiWantValue(t, "any-scope kind", any(anyScope.Kind), any(KindOutsource))
		if !errors.Is(errStaff, errNotFound) {
			t.Fatalf("want errNotFound for the staff-only scope, got %v", errStaff)
		}
		apiWantValue(t, "staff-scope row is nil", any(staffScope == nil), any(true))
	})

	t.Run("a caller that never chose a scope is refused outright rather than served the wider population", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		row, err := api.resolveMember(apiTestPlainAgentID, memberScopeUnset)

		if !errors.Is(err, errScopeUnset) {
			t.Fatalf("want errScopeUnset, got %v", err)
		}
		apiWantValue(t, "message", any(err.Error()), any("member lookup called without a memberScope"))
		apiWantValue(t, "row is nil", any(row == nil), any(true))
	})
}

func TestResolveMachine(t *testing.T) {
	t.Run("the live warden row whose id is the machine id resolves whole", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		machine, err := api.resolveMachine(ServerSelfHost)

		if err != nil {
			t.Fatalf("resolveMachine: %v", err)
		}
		apiWantValue(t, "row", any(*machine), any(apiHelpersMember(t, d, ServerSelfHost)))
		apiWantValue(t, "kind", any(machine.Kind), any(KindWarden))
		dashboard.wantFrames()
	})

	t.Run("a staff member, a contractor and an id nobody minted are each not found", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship the crate","executor_member_id":"kip"}`); status != 200 {
			t.Fatalf("create task: %d %v", status, data)
		}
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-000000000001", Codename: "Contractor", TaskID: "T-1",
			Status: "assigned", Runtime: "claude", Model: "sonnet", Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		for _, id := range []string{apiTestPlainAgentID, "ow-000000000001", "ghost"} {
			machine, err := api.resolveMachine(id)
			if !errors.Is(err, errNotFound) {
				t.Fatalf("resolveMachine(%q): want errNotFound, got %v", id, err)
			}
			apiWantValue(t, "row is nil for "+id, any(machine == nil), any(true))
		}
	})

	t.Run("a warden that is no longer active is not found, so a decommissioned machine cannot answer", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		retired := apiHelpersMember(t, d, ServerSelfHost)
		retired.RosterStatus = RosterStatusRemoved
		if err := d.PutMember(retired); err != nil {
			t.Fatalf("PutMember: %v", err)
		}

		machine, err := api.resolveMachine(ServerSelfHost)

		if !errors.Is(err, errNotFound) {
			t.Fatalf("want errNotFound, got %v", err)
		}
		apiWantValue(t, "row is nil", any(machine == nil), any(true))
	})
}

func TestWriteResolveError(t *testing.T) {
	t.Run("a not-found resolve answers 404 naming what was looked for and the id that was asked for", func(t *testing.T) {
		rec := httptest.NewRecorder()

		writeResolveError(rec, errNotFound, "member", "ghost")

		apiWantValue(t, "status", any(float64(rec.Code)), any(404))
		apiWantError(t, apiHelpersWritten(t, rec), "not_found", "member 'ghost' not found")
	})

	t.Run("the scope-unset programming error is not a 404 — it folds to the honest 500", func(t *testing.T) {
		rec := httptest.NewRecorder()

		writeResolveError(rec, errScopeUnset, "member", "kip")

		apiWantValue(t, "status", any(float64(rec.Code)), any(500))
		apiWantError(t, apiHelpersWritten(t, rec), "internal_error",
			"internal error: member lookup called without a memberScope")
	})

	t.Run("a storage fault folds to the same 500 envelope internalError writes", func(t *testing.T) {
		rec := httptest.NewRecorder()

		writeResolveError(rec, errors.New("db gone"), "machine", "m-lab")

		apiWantValue(t, "status", any(float64(rec.Code)), any(500))
		apiWantError(t, apiHelpersWritten(t, rec), "internal_error", "internal error: db gone")
	})
}

func TestNormalizeRuntime(t *testing.T) {
	t.Run("an unset runtime reads as claude while a named one is passed through trimmed, valid or not", func(t *testing.T) {
		apiWantValue(t, "empty", any(NormalizeRuntime("")), any("claude"))
		apiWantValue(t, "blank", any(NormalizeRuntime("   ")), any("claude"))
		apiWantValue(t, "claude", any(NormalizeRuntime("claude")), any("claude"))
		apiWantValue(t, "padded codex", any(NormalizeRuntime(" codex ")), any("codex"))
		apiWantValue(t, "unknown runtime is not coerced", any(NormalizeRuntime("gpt")), any("gpt"))
	})
}

func TestMemberRoleName(t *testing.T) {
	t.Run("a seed role shows its stable seed title", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		name, err := api.memberRoleName(apiHelpersMember(t, d, seedMiraID))

		if err != nil {
			t.Fatalf("memberRoleName: %v", err)
		}
		apiWantValue(t, "role name", any(name), any("Assistant"))
	})

	t.Run("a custom role shows the overlay name its founding create wrote", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "POST", "/api/roles", owner,
			`{"name":"Quartermaster","member_name":"Rill"}`)
		if status != 200 {
			t.Fatalf("create role: %d %v", status, data)
		}
		founder, _ := data["member_id"].(string)

		name, err := api.memberRoleName(apiHelpersMember(t, d, founder))

		if err != nil {
			t.Fatalf("memberRoleName: %v", err)
		}
		apiWantValue(t, "role name", any(name), any("Quartermaster"))
	})

	t.Run("a tombstoned overlay, an unknown role key and an unbound member all read as an honest blank", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "POST", "/api/roles", owner,
			`{"name":"Quartermaster","member_name":"Rill"}`)
		if status != 200 {
			t.Fatalf("create role: %d %v", status, data)
		}
		roleKey, _ := data["role_key"].(string)
		founder, _ := data["member_id"].(string)
		if err := d.PutRoleDef(RoleDef{
			RoleKey: roleKey, Name: "Quartermaster", DefinitionMD: "x", Tombstoned: true,
		}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}

		tombstoned, errTomb := api.memberRoleName(apiHelpersMember(t, d, founder))
		unknown, errUnknown := api.memberRoleName(Member{ID: "m-x", RoleKey: "r-nosuch"})
		unbound, errUnbound := api.memberRoleName(Member{ID: "m-x", RoleKey: ""})

		if errTomb != nil || errUnknown != nil || errUnbound != nil {
			t.Fatalf("want no error, got %v / %v / %v", errTomb, errUnknown, errUnbound)
		}
		apiWantValue(t, "tombstoned", any(tombstoned), any(""))
		apiWantValue(t, "unknown role key", any(unknown), any(""))
		apiWantValue(t, "unbound member", any(unbound), any(""))
	})
}

func TestRefocusDeadline(t *testing.T) {
	t.Run("an epoch plus its grace is the ceiling, and no epoch is no deadline", func(t *testing.T) {
		apiWantValue(t, "epoch + grace", any(refocusDeadline(100, 120)), any(float64(220)))
		apiWantValue(t, "zero grace still has a ceiling", any(refocusDeadline(100, 0)), any(float64(100)))
		apiWantValue(t, "no epoch", any(refocusDeadline(0, 120)), any(float64(0)))
		apiWantValue(t, "a negative epoch is no epoch", any(refocusDeadline(-5, 120)), any(float64(0)))
	})
}

func TestRefocusDeadlineOf(t *testing.T) {
	t.Run("only the two accelerated causes are collected on a clock; every other cause reports no deadline", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cfg := api.reconcileConfigLive()
		apiWantValue(t, "the fixture's grace", any(cfg.RecycleGrace), any(float64(120)))

		apiWantValue(t, "owner-pressed accelerated stop",
			any(refocusDeadlineOf(100, cfg, refocusOpAcceleratedStop)), any(float64(220)))
		apiWantValue(t, "the second context threshold",
			any(refocusDeadlineOf(100, cfg, refocusOpContextHigh)), any(float64(220)))
		apiWantValue(t, "a plain refocus", any(refocusDeadlineOf(100, cfg, "refocus")), any(float64(0)))
		apiWantValue(t, "a relocate", any(refocusDeadlineOf(100, cfg, "relocate")), any(float64(0)))
		apiWantValue(t, "no op at all", any(refocusDeadlineOf(100, cfg, "")), any(float64(0)))
	})

	t.Run("a clocked cause with no epoch to count from still reports no deadline", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cfg := api.reconcileConfigLive()

		apiWantValue(t, "deadline",
			any(refocusDeadlineOf(0, cfg, refocusOpAcceleratedStop)), any(float64(0)))
	})
}

func TestWinddownDeadlineOf(t *testing.T) {
	t.Run("a handover anchors on refocus_since and is clocked only for an accelerated cause", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cfg := api.reconcileConfigLive()

		accelerated := Member{ID: "m-a", DesiredState: DesiredStateOnline,
			RefocusSince: 100, RefocusOp: refocusOpAcceleratedStop}
		soft := Member{ID: "m-a", DesiredState: DesiredStateOnline,
			RefocusSince: 100, RefocusOp: "refocus"}

		apiWantValue(t, "accelerated handover", any(winddownDeadlineOf(accelerated, cfg)), any(float64(220)))
		apiWantValue(t, "soft handover", any(winddownDeadlineOf(soft, cfg)), any(float64(0)))
	})

	t.Run("a stop anchors on stopping_since instead, and only the accelerated rung carries a clock", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cfg := api.reconcileConfigLive()

		accelerated := Member{ID: "m-a", DesiredState: DesiredStateOffline,
			StoppingSince: 500, RefocusOp: refocusOpAcceleratedStop}
		soft := Member{ID: "m-a", DesiredState: DesiredStateOffline,
			StoppingSince: 500, RefocusOp: ""}

		apiWantValue(t, "accelerated stop", any(winddownDeadlineOf(accelerated, cfg)), any(float64(620)))
		apiWantValue(t, "plain stop", any(winddownDeadlineOf(soft, cfg)), any(float64(0)))
	})

	t.Run("a stop with no epoch open reports no deadline, whether it was never stamped or was cut off by a force-stop", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cfg := api.reconcileConfigLive()

		unstamped := Member{ID: "m-a", DesiredState: DesiredStateOffline,
			StoppingSince: 0, RefocusOp: refocusOpAcceleratedStop}
		forced := Member{ID: "m-a", DesiredState: DesiredStateOffline,
			StoppingSince: 500, ForcedStopAt: 600, RefocusOp: refocusOpAcceleratedStop}

		apiWantValue(t, "never stamped", any(winddownDeadlineOf(unstamped, cfg)), any(float64(0)))
		apiWantValue(t, "cut off", any(winddownDeadlineOf(forced, cfg)), any(float64(0)))
	})

	t.Run("a member with no wind-down in flight at all reports no deadline", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		cfg := api.reconcileConfigLive()

		apiWantValue(t, "deadline",
			any(winddownDeadlineOf(apiHelpersMember(t, d, seedMiraID), cfg)), any(float64(0)))
	})
}

func TestObservedHost(t *testing.T) {
	t.Run("a warden attributes to its own id with nothing observed anywhere", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		apiWantValue(t, "observed host",
			any(api.observedHost(apiHelpersMember(t, d, ServerSelfHost))), any("m-server-self"))
	})

	t.Run("a member nothing has ever observed reads blank, and a pin it has not landed on is not substituted", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)

		apiWantValue(t, "unobserved member",
			any(api.observedHost(apiHelpersMember(t, d, apiTestPlainAgentID))), any(""))
		apiWantValue(t, "an intent and a last landing are neither of them an observation",
			any(api.observedHost(Member{ID: "m-x", DesiredMachineID: "m-pin", LastMachineID: "m-last"})),
			any(""))
	})

	t.Run("a self-reported telemetry machine is the observation when no live connection claims one", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"machine":"m-tele","tokens":{}}`); status != 200 {
			t.Fatalf("ingest telemetry: %d %v", status, data)
		}

		apiWantValue(t, "observed host",
			any(api.observedHost(apiHelpersMember(t, d, apiTestPlainAgentID))), any("m-tele"))
	})

	t.Run("a live connection's machine claim outranks the self-report, and a claim-less connection falls back to it", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		if status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", agent,
			`{"machine":"m-tele","tokens":{}}`); status != 200 {
			t.Fatalf("ingest telemetry: %d %v", status, data)
		}
		kip := apiHelpersMember(t, d, apiTestPlainAgentID)

		claimed, err := api.hub.Connect(apiTestPlainAgentID, "m-hub")
		if err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		apiWantValue(t, "the claim wins", any(api.observedHost(kip)), any("m-hub"))
		api.hub.Disconnect(claimed)

		claimless, err := api.hub.Connect(apiTestPlainAgentID, "")
		if err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		apiWantValue(t, "a claim-less connection observes nothing of its own",
			any(api.observedHost(kip)), any("m-tele"))
		api.hub.Disconnect(claimless)
	})
}

func TestNewMemberDTO(t *testing.T) {
	t.Run("the full projection carries every declared field, with the injected role, machine and unread count", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		member := Member{
			ID: "m-rich", Name: "Rill", Kind: KindStaff, RoleKey: "r-quarter",
			Runtime: "codex", Model: "opus", ActualModel: "sonnet",
			ActualRuntime: "claude", ActualEffort: "high", Effort: "max",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-pin",
			LastMachineID: "m-last", RefocusSince: 100, RefocusOp: refocusOpAcceleratedStop,
			LastOp: "START", LastOpOK: apiHelpersBool(true), LastOpLog: "started",
			LastOpReason: "ok: fine", LastOpAt: 42, ForcedStopAt: 7,
			RosterStatus: RosterStatusActive,
		}
		dashboard := apiTestListen(t, api, "")

		dto := api.newMemberDTO(member, "Quartermaster", "m-obs", 5)

		apiWantValue(t, "dto", any(apiHelpersWire(t, dto)), any(map[string]any{
			"id":                      "m-rich",
			"avatar_icon_id":          nil,
			"name":                    "Rill",
			"kind":                    "staff",
			"role_key":                "r-quarter",
			"role_name":               "Quartermaster",
			"runtime":                 "codex",
			"model":                   "opus",
			"actual_model":            "sonnet",
			"actual_runtime":          "claude",
			"actual_effort":           "high",
			"actual_machine":          "m-last",
			"effort":                  "max",
			"desired_state":           "online",
			"desired_machine_id":      "m-pin",
			"machine":                 "m-obs",
			"presence":                "offline",
			"refocus_since":           100,
			"refocus_op":              "accelerated_stop",
			"refocus_deadline":        220,
			"last_op":                 "START",
			"last_op_ok":              true,
			"last_op_log":             "started",
			"last_op_reason":          "ok: fine",
			"last_op_at":              42,
			"forced_stop_at":          7,
			"unread_count":            5,
			"roster_status":           "active",
			"owner_id":                "owner",
			"schema_version":          3,
			"terminal_attach_command": "tmux -L officraft attach -t member-m-rich",
		}))
		dashboard.wantFrames()
	})

	t.Run("presence is the live connection fact rather than the stored intent, and an unset runtime is normalised on the wire", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		member := apiHelpersMember(t, d, seedMiraID)

		offline := api.newMemberDTO(member, "Assistant", "", 0)
		listener, err := api.hub.Connect(seedMiraID, "")
		if err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		online := api.newMemberDTO(member, "Assistant", "", 0)
		api.hub.Disconnect(listener)

		apiWantValue(t, "presence with no connection", any(offline.Presence), any("offline"))
		apiWantValue(t, "presence with a live connection", any(online.Presence), any("online"))
		apiWantValue(t, "the row's own runtime", any(member.Runtime), any(""))
		apiWantValue(t, "runtime on the wire", any(offline.Runtime), any("claude"))
		apiWantValue(t, "no recorded choice is a null icon id, not a dangling one",
			any(offline.AvatarIconID), any((*string)(nil)))
	})
}

func TestNewMemberLightDTO(t *testing.T) {
	t.Run("the light projection keeps identity and role and leaves every derived field honest-empty in the SAME wire shape", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		member := Member{
			ID: "m-rich", Name: "Rill", Kind: KindStaff, RoleKey: "r-quarter",
			Runtime: "codex", Model: "opus", ActualModel: "sonnet",
			ActualRuntime: "claude", ActualEffort: "high", Effort: "max",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-pin",
			LastMachineID: "m-last", RefocusSince: 100, RefocusOp: refocusOpAcceleratedStop,
			LastOp: "START", LastOpOK: apiHelpersBool(true), LastOpLog: "started",
			LastOpReason: "ok: fine", LastOpAt: 42, ForcedStopAt: 7,
			RosterStatus: RosterStatusActive,
		}
		dashboard := apiTestListen(t, api, "")

		dto := api.newMemberLightDTO(member, "Quartermaster")

		apiWantValue(t, "dto", any(apiHelpersWire(t, dto)), any(map[string]any{
			"id":                      "m-rich",
			"avatar_icon_id":          nil,
			"name":                    "Rill",
			"kind":                    "staff",
			"role_key":                "r-quarter",
			"role_name":               "Quartermaster",
			"runtime":                 "codex",
			"roster_status":           "active",
			"owner_id":                "owner",
			"schema_version":          3,
			"terminal_attach_command": "tmux -L officraft attach -t member-m-rich",
			"model":                   "",
			"actual_model":            "",
			"actual_runtime":          "",
			"actual_effort":           "",
			"actual_machine":          "",
			"effort":                  "",
			"desired_state":           "",
			"desired_machine_id":      "",
			"machine":                 "",
			"presence":                "",
			"refocus_since":           0,
			"refocus_op":              "",
			"refocus_deadline":        0,
			"last_op":                 "",
			"last_op_ok":              nil,
			"last_op_log":             "",
			"last_op_reason":          "",
			"last_op_at":              0,
			"forced_stop_at":          0,
			"unread_count":            0,
		}))
		dashboard.wantFrames()
	})

	t.Run("the light projection never derives presence, so a live connection changes nothing about it", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		member := apiHelpersMember(t, d, seedMiraID)

		offline := api.newMemberLightDTO(member, "Assistant")
		listener, err := api.hub.Connect(seedMiraID, "m-hub")
		if err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		online := api.newMemberLightDTO(member, "Assistant")
		api.hub.Disconnect(listener)

		apiWantValue(t, "presence stays blank", any(online.Presence), any(""))
		apiWantValue(t, "the whole projection is unchanged", any(online), any(offline))
		apiWantValue(t, "the full projection would have said otherwise",
			any(api.newMemberDTO(member, "Assistant", "", 0).Presence), any("offline"))
	})
}

func TestWriteSelfReportReceipt(t *testing.T) {
	t.Run("the shared tail answers the four self-report fields and names no stop effect", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		member := Member{ID: "m-rich", DesiredState: DesiredStateOnline,
			RefocusSince: 100, RefocusOp: refocusOpAcceleratedStop, Name: "Rill"}
		rec := httptest.NewRecorder()

		api.writeSelfReportReceipt(rec, member)

		apiWantValue(t, "status", any(float64(rec.Code)), any(200))
		apiWantValue(t, "content type", any(rec.Header().Get("Content-Type")), any("application/json"))
		apiWantBody(t, apiHelpersWritten(t, rec), map[string]any{
			"id":               "m-rich",
			"desired_state":    "online",
			"refocus_op":       "accelerated_stop",
			"refocus_deadline": 220,
		})
	})

	t.Run("a member with no wind-down in flight is quoted no deadline at all", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		rec := httptest.NewRecorder()

		api.writeSelfReportReceipt(rec, apiHelpersMember(t, d, seedMiraID))

		apiWantValue(t, "status", any(float64(rec.Code)), any(200))
		apiWantBody(t, apiHelpersWritten(t, rec), map[string]any{
			"id":               "mira",
			"desired_state":    "offline",
			"refocus_op":       "",
			"refocus_deadline": 0,
		})
	})
}

func TestWriteSelfReportStopReceipt(t *testing.T) {
	t.Run("the stopped face is the same receipt with the effect named", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		member := Member{ID: "m-rich", DesiredState: DesiredStateOffline,
			StoppingSince: 500, RefocusOp: refocusOpAcceleratedStop}
		rec := httptest.NewRecorder()

		api.writeSelfReportStopReceipt(rec, member, "collected")

		apiWantValue(t, "status", any(float64(rec.Code)), any(200))
		apiWantBody(t, apiHelpersWritten(t, rec), map[string]any{
			"id":               "m-rich",
			"desired_state":    "offline",
			"refocus_op":       "accelerated_stop",
			"refocus_deadline": 620,
			"stop_effect":      "collected",
		})
	})

	t.Run("an unnamed effect is dropped from the wire, leaving the receipt the other three faces answer", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		member := Member{ID: "m-rich", DesiredState: DesiredStateOffline}
		withEffect := httptest.NewRecorder()
		withoutEffect := httptest.NewRecorder()

		api.writeSelfReportStopReceipt(withEffect, member, "collected")
		api.writeSelfReportStopReceipt(withoutEffect, member, "")

		apiWantBody(t, apiHelpersWritten(t, withEffect), map[string]any{
			"id":               "m-rich",
			"desired_state":    "offline",
			"refocus_op":       "",
			"refocus_deadline": 0,
			"stop_effect":      "collected",
		})
		apiWantBody(t, apiHelpersWritten(t, withoutEffect), map[string]any{
			"id":               "m-rich",
			"desired_state":    "offline",
			"refocus_op":       "",
			"refocus_deadline": 0,
		})
	})
}

func TestNewHexID(t *testing.T) {
	t.Run("a minted id is exactly n lowercase hex characters", func(t *testing.T) {
		for _, n := range []int{1, 2, 11, 12, 32} {
			id := newHexID(n)
			apiWantValue(t, "length", any(float64(len(id))), any(n))
			for _, c := range id {
				if !strings.ContainsRune("0123456789abcdef", c) {
					t.Fatalf("newHexID(%d) = %q carries %q, which is not lowercase hex", n, id, c)
				}
			}
		}
	})

	t.Run("asking for no characters mints the empty id rather than panicking", func(t *testing.T) {
		apiWantValue(t, "id", any(newHexID(0)), any(""))
	})

	t.Run("two mints of the same width do not collide", func(t *testing.T) {
		seen := map[string]bool{}
		for i := 0; i < 64; i++ {
			id := newHexID(12)
			if seen[id] {
				t.Fatalf("newHexID(12) minted %q twice in 64 draws", id)
			}
			seen[id] = true
		}
		apiWantValue(t, "distinct ids", any(float64(len(seen))), any(64))
	})
}

func TestStrOrEmpty(t *testing.T) {
	t.Run("an omitted optional string reads blank and a sent one reads through, empty string included", func(t *testing.T) {
		sent := "learnings"
		blank := ""

		apiWantValue(t, "omitted", any(strOrEmpty(nil)), any(""))
		apiWantValue(t, "sent", any(strOrEmpty(&sent)), any("learnings"))
		apiWantValue(t, "sent empty", any(strOrEmpty(&blank)), any(""))
	})
}

func TestIntOr(t *testing.T) {
	t.Run("an omitted optional int falls back to the field's declared default, never to zero", func(t *testing.T) {
		apiWantValue(t, "omitted with a day-of-month default", any(float64(intOr(nil, 1))), any(1))
		apiWantValue(t, "omitted with a zero default", any(float64(intOr(nil, 0))), any(0))
	})

	t.Run("a value the caller sent wins over the default, zero included", func(t *testing.T) {
		sent := 28
		zero := 0

		apiWantValue(t, "sent", any(float64(intOr(&sent, 1))), any(28))
		apiWantValue(t, "sent zero", any(float64(intOr(&zero, 1))), any(0))
	})
}

func TestIntSliceOrNil(t *testing.T) {
	t.Run("an omitted array reads as nil while a sent one reads through", func(t *testing.T) {
		sent := []int{1, 15, 28}

		apiWantValue(t, "omitted", any(intSliceOrNil(nil)), any([]int(nil)))
		apiWantValue(t, "sent", any(intSliceOrNil(&sent)), any([]int{1, 15, 28}))
	})

	t.Run("an array the caller sent EMPTY comes back as the empty slice, not as nil", func(t *testing.T) {
		empty := []int{}

		got := intSliceOrNil(&empty)

		apiWantValue(t, "is nil", any(got == nil), any(false))
		apiWantValue(t, "value", any(got), any([]int{}))
	})
}

func TestRequireNonEmptyEdits(t *testing.T) {
	t.Run("a batch with at least one edit passes with nothing written to the response", func(t *testing.T) {
		rec := httptest.NewRecorder()

		ok := requireNonEmptyEdits(rec, []LessonsEditDTO{{}})

		apiWantValue(t, "ok", any(ok), any(true))
		apiWantValue(t, "status", any(float64(rec.Code)), any(200))
		apiWantValue(t, "body", any(rec.Body.String()), any(""))
	})

	t.Run("an omitted and an explicitly empty edits list are both refused 422 with the same sentence", func(t *testing.T) {
		omitted := httptest.NewRecorder()
		explicit := httptest.NewRecorder()

		okOmitted := requireNonEmptyEdits(omitted, nil)
		okExplicit := requireNonEmptyEdits(explicit, []LessonsEditDTO{})

		apiWantValue(t, "omitted ok", any(okOmitted), any(false))
		apiWantValue(t, "omitted status", any(float64(omitted.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, omitted), "validation_error",
			"edits requires at least one {old, new} entry")
		apiWantValue(t, "explicit ok", any(okExplicit), any(false))
		apiWantValue(t, "explicit status", any(float64(explicit.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, explicit), "validation_error",
			"edits requires at least one {old, new} entry")
	})
}

func TestDecodePatchEdits(t *testing.T) {
	t.Run("each wire edit folds to the engine edit, an absent half reading as the empty string", func(t *testing.T) {
		old := "anchor"
		replacement := "replacement"
		rec := httptest.NewRecorder()

		edits, ok := decodePatchEdits(rec, []LessonsEditDTO{
			{Old: &old, New: &replacement}, {Old: &old}, {New: &replacement},
		})

		apiWantValue(t, "ok", any(ok), any(true))
		apiWantValue(t, "status", any(float64(rec.Code)), any(200))
		apiWantValue(t, "body", any(rec.Body.String()), any(""))
		apiWantValue(t, "edits", any(edits), any([]LessonsEdit{
			{Old: "anchor", New: "replacement"},
			{Old: "anchor", New: ""},
			{Old: "", New: "replacement"},
		}))
	})

	t.Run("an edit carrying neither half refuses the WHOLE batch 422, naming which entry it was", func(t *testing.T) {
		old := "anchor"
		rec := httptest.NewRecorder()

		edits, ok := decodePatchEdits(rec, []LessonsEditDTO{{Old: &old}, {}, {Old: &old}})

		apiWantValue(t, "ok", any(ok), any(false))
		apiWantValue(t, "edits are nil", any(edits == nil), any(true))
		apiWantValue(t, "status", any(float64(rec.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, rec), "validation_error",
			"edits[1]: neither old nor new was given — an edit needs at least one of "+
				"them (empty old appends new); nothing was written")
	})

	t.Run("an explicitly empty half is a real edit, not a missing one", func(t *testing.T) {
		blank := ""
		rec := httptest.NewRecorder()

		edits, ok := decodePatchEdits(rec, []LessonsEditDTO{{Old: &blank}, {New: &blank}})

		apiWantValue(t, "ok", any(ok), any(true))
		apiWantValue(t, "body", any(rec.Body.String()), any(""))
		apiWantValue(t, "edits", any(edits), any([]LessonsEdit{{Old: "", New: ""}, {Old: "", New: ""}}))
	})

	t.Run("an empty batch folds to the empty slice rather than nil, and writes nothing — the emptiness refusal is requireNonEmptyEdits' job", func(t *testing.T) {
		omitted := httptest.NewRecorder()
		explicit := httptest.NewRecorder()

		fromNil, okNil := decodePatchEdits(omitted, nil)
		fromEmpty, okEmpty := decodePatchEdits(explicit, []LessonsEditDTO{})

		apiWantValue(t, "ok from nil", any(okNil), any(true))
		apiWantValue(t, "from nil is not nil", any(fromNil == nil), any(false))
		apiWantValue(t, "from nil", any(fromNil), any([]LessonsEdit{}))
		apiWantValue(t, "nothing written", any(omitted.Body.String()), any(""))
		apiWantValue(t, "ok from empty", any(okEmpty), any(true))
		apiWantValue(t, "from empty is not nil", any(fromEmpty == nil), any(false))
		apiWantValue(t, "from empty", any(fromEmpty), any([]LessonsEdit{}))
		apiWantValue(t, "nothing written", any(explicit.Body.String()), any(""))
	})
}

func TestDecodeJSONBody(t *testing.T) {
	t.Run("an all-optional body accepts an empty object and leaves the destination at its zero value", func(t *testing.T) {
		var dst LessonsPatchDTO
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/probe", strings.NewReader(`{}`))

		if ok := decodeJSONBody(rec, req, &dst); !ok {
			t.Fatal("decodeJSONBody({}) = false, want true")
		}
		apiWantValue(t, "status", any(float64(rec.Code)), any(200))
		apiWantValue(t, "body", any(rec.Body.String()), any(""))
		apiWantValue(t, "destination", any(apiHelpersWire(t, dst)), any(map[string]any{"edits": nil}))
	})

	t.Run("an unknown field is rejected at the public decoder face", func(t *testing.T) {
		var dst LessonsPatchDTO
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/probe", strings.NewReader(`{"learnings":"not a patch"}`))

		if ok := decodeJSONBody(rec, req, &dst); ok {
			t.Fatal("decodeJSONBody with an unknown field = true, want false")
		}
		apiWantValue(t, "status", any(float64(rec.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, rec), "validation_error",
			`invalid request body: json: unknown field "learnings"`)
	})
}

func TestDecodeJSONBodyRequired(t *testing.T) {
	t.Run("a named key is decoded and accepted when the caller sends it", func(t *testing.T) {
		var dst LessonsPatchDTO
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/probe",
			strings.NewReader(`{"edits":[]}`))

		if ok := decodeJSONBodyRequired(rec, req, &dst, "edits"); !ok {
			t.Fatal("decodeJSONBodyRequired with edits = false, want true")
		}
		apiWantValue(t, "status", any(float64(rec.Code)), any(200))
		apiWantValue(t, "decoded edits", any(dst.Edits), any([]LessonsEditDTO{}))
	})

	t.Run("an omitted named key is rejected before a caller can use the zero value as a write", func(t *testing.T) {
		var dst LessonsPatchDTO
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/probe",
			strings.NewReader(`{"allow_shrink":true}`))

		if ok := decodeJSONBodyRequired(rec, req, &dst, "edits"); ok {
			t.Fatal("decodeJSONBodyRequired without edits = true, want false")
		}
		apiWantValue(t, "status", any(float64(rec.Code)), any(422))
		apiWantError(t, apiHelpersWritten(t, rec), "validation_error", "field required: edits")
	})
}

func TestDecodeJSONBodyPresent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		want  map[string]bool
		allow *bool
	}{
		{name: "omitted optional key", body: `{"edits":[]}`, want: map[string]bool{"edits": true}},
		{name: "explicit null optional key", body: `{"edits":[],"allow_shrink":null}`, want: map[string]bool{"edits": true, "allow_shrink": true}},
		{name: "explicit false optional key", body: `{"edits":[],"allow_shrink":false}`, want: map[string]bool{"edits": true, "allow_shrink": true}, allow: apiHelpersBool(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dst LessonsPatchDTO
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/probe", strings.NewReader(tc.body))

			got, ok := decodeJSONBodyPresent(rec, req, &dst, "edits")
			if !ok {
				t.Fatalf("decodeJSONBodyPresent(%s) = false, want true", tc.body)
			}
			apiWantValue(t, "status", any(float64(rec.Code)), any(200))
			apiWantValue(t, "sent keys", any(got), any(tc.want))
			apiWantValue(t, "allow_shrink pointer", any(dst.AllowShrink), any(tc.allow))
		})
	}
}

func TestValidEffort(t *testing.T) {
	for _, tc := range []struct {
		name   string
		effort string
		want   bool
	}{
		{name: "low is accepted", effort: "low", want: true},
		{name: "medium is accepted", effort: "medium", want: true},
		{name: "high is accepted", effort: "high", want: true},
		{name: "xhigh is accepted", effort: "xhigh", want: true},
		{name: "max is accepted", effort: "max", want: true},
		{name: "empty effort is refused", effort: "", want: false},
		{name: "wrong case is refused", effort: "LOW", want: false},
		{name: "padded effort is refused", effort: "medium ", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validEffort(tc.effort); got != tc.want {
				t.Fatalf("validEffort(%q) = %v, want %v", tc.effort, got, tc.want)
			}
		})
	}
}

func TestValidRuntime(t *testing.T) {
	for _, tc := range []struct {
		name    string
		runtime string
		want    bool
	}{
		{name: "claude is accepted", runtime: RuntimeClaude, want: true},
		{name: "codex is accepted", runtime: RuntimeCodex, want: true},
		{name: "empty runtime is refused", runtime: "", want: false},
		{name: "padded runtime is refused", runtime: " claude ", want: false},
		{name: "unknown runtime is refused", runtime: "opus", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidRuntime(tc.runtime); got != tc.want {
				t.Fatalf("ValidRuntime(%q) = %v, want %v", tc.runtime, got, tc.want)
			}
		})
	}
}

func TestTrimString(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "ascii surrounding whitespace is removed", input: "  note  ", want: "note"},
		{name: "unicode surrounding whitespace is removed", input: "\u2003note\n", want: "note"},
		{name: "all whitespace becomes empty", input: "\t \n", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := trimString(tc.input); got != tc.want {
				t.Fatalf("trimString(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestTrimmedOrEmpty(t *testing.T) {
	value := "  note  "
	blank := " \t\n"
	for _, tc := range []struct {
		name  string
		input *string
		want  string
	}{
		{name: "nil pointer", want: ""},
		{name: "whitespace pointer", input: &blank, want: ""},
		{name: "trimmed value", input: &value, want: "note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := trimmedOrEmpty(tc.input); got != tc.want {
				t.Fatalf("trimmedOrEmpty(%v) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func apiHelpersBool(b bool) *bool { return &b }
