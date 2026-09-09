// Skeleton generated from server/ocserverd/api_doc_sizes.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"net/http"
	"testing"
)

// apiDocSizes reads the size overview and fails the test unless it is served.
func apiDocSizes(t *testing.T, h http.Handler, credential string) map[string]any {
	t.Helper()
	status, data := apiJSON(t, h, "GET", "/api/doc-sizes", credential, "")
	if status != 200 {
		t.Fatalf("want 200, got %d (%v)", status, data)
	}
	return data
}

// apiSeededAssistantRow is the out-of-box roster's one role, as the overview
// reports it before anything has been written.
func apiSeededAssistantRow() map[string]any {
	return map[string]any{
		"role_key": "assistant",
		"duty":     map[string]any{"size_chars": 169, "cap_chars": 1000},
		"insight":  map[string]any{"size_chars": 1089, "cap_chars": 15000},
		"lessons":  map[string]any{"size_chars": 14, "cap_chars": 15000},
	}
}

func TestHandlePeekDocSizesApiDocSizesGet(t *testing.T) {
	t.Run("an out-of-box station reports its one seeded role's three documents and no task manual at all", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		apiWantBody(t, apiDocSizes(t, h, owner), map[string]any{
			"roles":        []any{apiSeededAssistantRow()},
			"task_manuals": []any{},
		})
		dashboard.wantFrames()
	})

	t.Run("a created role and a created task manual each add their own row", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		status, created := apiJSON(t, h, "POST", "/api/roles", owner, `{"name":"Design","member_name":"Zed"}`)
		if status != 200 {
			t.Fatalf("create role: %d %v", status, created)
		}
		roleKey, _ := created["role_key"].(string)
		if status, data := apiJSON(t, h, "POST", "/api/task-manuals", owner,
			`{"type_key":"crate","display_name":"裝箱"}`); status != 200 {
			t.Fatalf("create manual: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		apiWantBody(t, apiDocSizes(t, h, owner), map[string]any{
			"roles": []any{
				apiSeededAssistantRow(),
				map[string]any{
					"role_key": roleKey,
					"duty":     map[string]any{"size_chars": 155, "cap_chars": 1000},
					"insight":  map[string]any{"size_chars": 0, "cap_chars": 15000},
					"lessons":  map[string]any{"size_chars": 14, "cap_chars": 15000},
				},
			},
			"task_manuals": []any{
				map[string]any{
					"type_key":  "crate",
					"sop":       map[string]any{"size_chars": 0, "cap_chars": 15000},
					"learnings": map[string]any{"size_chars": 0, "cap_chars": 15000},
				},
			},
		})
		dashboard.wantFrames()
	})

	t.Run("writing a role's insight moves that role's insight size and leaves every other number where it was", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/insight/assistant", owner,
			`{"text":"weigh latency"}`); status != 200 {
			t.Fatalf("write insight: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		apiWantBody(t, apiDocSizes(t, h, owner), map[string]any{
			"roles": []any{map[string]any{
				"role_key": "assistant",
				"duty":     map[string]any{"size_chars": 169, "cap_chars": 1000},
				"insight":  map[string]any{"size_chars": 13, "cap_chars": 15000},
				"lessons":  map[string]any{"size_chars": 14, "cap_chars": 15000},
			}},
			"task_manuals": []any{},
		})
		dashboard.wantFrames()
	})

	t.Run("each of the five segments is quoted against its OWN cap, so raising one moves that one number and no other", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/task-manuals", owner,
			`{"type_key":"crate","display_name":"裝箱"}`); status != 200 {
			t.Fatalf("create manual: %d %v", status, data)
		}
		if status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"doc_cap_chars_insight":20000,"doc_cap_chars_manual_sop":30000}`); status != 200 {
			t.Fatalf("raise caps: %d %v", status, data)
		}

		apiWantBody(t, apiDocSizes(t, h, owner), map[string]any{
			"roles": []any{map[string]any{
				"role_key": "assistant",
				"duty":     map[string]any{"size_chars": 169, "cap_chars": 1000},
				"insight":  map[string]any{"size_chars": 1089, "cap_chars": 20000},
				"lessons":  map[string]any{"size_chars": 14, "cap_chars": 15000},
			}},
			"task_manuals": []any{map[string]any{
				"type_key":  "crate",
				"sop":       map[string]any{"size_chars": 0, "cap_chars": 30000},
				"learnings": map[string]any{"size_chars": 0, "cap_chars": 15000},
			}},
		})
	})

	t.Run("a plain agent identity reads it too, because this row sits at the machine floor", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		apiWantBody(t, apiDocSizes(t, h, housekeeper), map[string]any{
			"roles":        []any{apiSeededAssistantRow()},
			"task_manuals": []any{},
		})
	})

	t.Run("a request carrying no credentials answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/doc-sizes", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})

	t.Run("an ignored request body does not change the document size overview", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/doc-sizes", owner, "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"roles":        []any{apiSeededAssistantRow()},
			"task_manuals": []any{},
		})
	})
}
