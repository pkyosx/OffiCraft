package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func taskManualSuggestedRepliesFixture(t *testing.T) (*apiServer, http.Handler, *DAL, string) {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	for _, key := range []string{"tm-a", "tm-b"} {
		dalPutManual(t, d, TaskManual{TypeKey: key, DisplayName: key, Fields: "[]", Assignee: "{}"})
	}
	return api, h, d, owner
}

func taskManualSuggestedRepliesPut(t *testing.T, h http.Handler, owner, typeKey string, replies ...string) {
	t.Helper()
	if status, data := apiJSON(t, h, "PUT", "/api/task-manual-suggested-replies/"+typeKey, owner,
		taskManualSuggestedRepliesBody(t, replies...)); status != 200 {
		t.Fatalf("PUT %s: want 200, got %d (%v)", typeKey, status, data)
	}
}

func taskManualSuggestedRepliesWantLists(t *testing.T, h http.Handler, owner string, lists ...any) {
	t.Helper()
	if lists == nil {
		lists = []any{}
	}
	status, data := apiJSON(t, h, "GET", "/api/task-manual-suggested-replies", owner, "")
	if status != 200 {
		t.Fatalf("GET: want 200, got %d (%v)", status, data)
	}
	apiWantBody(t, data, map[string]any{"lists": lists})
}

func taskManualSuggestedReplyFrame(seq int, trigger string) map[string]any {
	return map[string]any{
		"seq":   seq,
		"topic": "task_manual_suggested_reply",
		"op":    "patch",
		"data": map[string]any{
			"entity":  "task_manual_suggested_reply",
			"key":     "owner",
			"epoch":   seq,
			"deleted": false,
			"payload": nil,
		},
		"ts":      apiAnyNumber,
		"trigger": trigger,
	}
}

func taskManualSuggestedRepliesBody(t *testing.T, replies ...string) string {
	t.Helper()
	if replies == nil {
		replies = []string{}
	}
	raw, err := json.Marshal(map[string]any{"replies": replies})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func TestHandleGetTaskManualSuggestedRepliesApiTaskManualSuggestedRepliesGet(t *testing.T) {
	t.Run("on a fresh station there are no lists", func(t *testing.T) {
		_, h, _, owner := taskManualSuggestedRepliesFixture(t)

		taskManualSuggestedRepliesWantLists(t, h, owner)
	})

	t.Run("every type's list comes back in display order and a type with an emptied list is not listed", func(t *testing.T) {
		_, h, _, owner := taskManualSuggestedRepliesFixture(t)
		taskManualSuggestedRepliesPut(t, h, owner, "tm-b", "b", "a")
		taskManualSuggestedRepliesPut(t, h, owner, "builtin-role-design", "照手冊")
		taskManualSuggestedRepliesPut(t, h, owner, "tm-a", "gone")
		taskManualSuggestedRepliesPut(t, h, owner, "tm-a")

		taskManualSuggestedRepliesWantLists(t, h, owner,
			map[string]any{"type_key": "builtin-role-design", "replies": []any{"照手冊"}},
			map[string]any{"type_key": "tm-b", "replies": []any{"b", "a"}},
		)
	})

	t.Run("an agent token answers 403 and no token answers 401", func(t *testing.T) {
		api, h, _, _ := taskManualSuggestedRepliesFixture(t)

		status, data := apiJSON(t, h, "GET", "/api/task-manual-suggested-replies", apiTestAgentToken(t, api, "mira", ""), "")
		if status != 403 {
			t.Fatalf("admin agent: want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		status, data = apiJSON(t, h, "GET", "/api/task-manual-suggested-replies", "", "")
		if status != 401 {
			t.Fatalf("no token: want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUpdateTaskManualSuggestedRepliesApiTaskManualSuggestedRepliesTypeKeyPut(t *testing.T) {
	for _, tc := range []struct {
		typeKey string
		lists   []any
	}{
		{"tm-a", []any{
			map[string]any{"type_key": "tm-a", "replies": []any{"second", "first", "first"}},
			map[string]any{"type_key": "tm-b", "replies": []any{"keep me"}},
		}},
		{"builtin-role-design", []any{
			map[string]any{"type_key": "builtin-role-design", "replies": []any{"second", "first", "first"}},
			map[string]any{"type_key": "tm-b", "replies": []any{"keep me"}},
		}},
	} {
		typeKey := tc.typeKey
		t.Run("a list for "+typeKey+" is stored trimmed in the order sent, leaves other lists alone and fans one frame to the owner only", func(t *testing.T) {
			api, h, _, owner := taskManualSuggestedRepliesFixture(t)
			taskManualSuggestedRepliesPut(t, h, owner, "tm-b", "keep me")
			dashboard := apiTestListen(t, api, "")
			agent := apiTestListen(t, api, "kip")

			status, data := apiJSON(t, h, "PUT", "/api/task-manual-suggested-replies/"+typeKey, owner,
				taskManualSuggestedRepliesBody(t, "  second  ", "first", "first"))
			if status != 200 {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			apiWantBody(t, data, map[string]any{"type_key": typeKey, "replies": []any{"second", "first", "first"}})
			dashboard.wantFrames(taskManualSuggestedReplyFrame(2, "owner"))
			agent.wantFrames()
			taskManualSuggestedRepliesWantLists(t, h, owner, tc.lists...)
		})
	}

	t.Run("a shorter list replaces the whole of the old one and an empty list removes it", func(t *testing.T) {
		_, h, _, owner := taskManualSuggestedRepliesFixture(t)
		taskManualSuggestedRepliesPut(t, h, owner, "tm-a", "a", "b", "c")

		taskManualSuggestedRepliesPut(t, h, owner, "tm-a", "c")
		taskManualSuggestedRepliesWantLists(t, h, owner, map[string]any{"type_key": "tm-a", "replies": []any{"c"}})

		status, data := apiJSON(t, h, "PUT", "/api/task-manual-suggested-replies/tm-a", owner, taskManualSuggestedRepliesBody(t))
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"type_key": "tm-a", "replies": []any{}})
		taskManualSuggestedRepliesWantLists(t, h, owner)
	})

	t.Run("exactly 20 entries of exactly 120 characters are accepted", func(t *testing.T) {
		_, h, _, owner := taskManualSuggestedRepliesFixture(t)
		replies := make([]string, 20)
		want := make([]any, 20)
		for i := range replies {
			replies[i] = fmt.Sprintf("%02d", i) + strings.Repeat("句", 118)
			want[i] = replies[i]
		}

		taskManualSuggestedRepliesPut(t, h, owner, "tm-a", replies...)
		taskManualSuggestedRepliesWantLists(t, h, owner, map[string]any{"type_key": "tm-a", "replies": want})
	})

	twentyOne := make([]string, 21)
	for i := range twentyOne {
		twentyOne[i] = "s"
	}
	for _, tc := range []struct {
		name, typeKey, body, message string
	}{
		{"a body without replies", "tm-a", `{}`, "field required: replies"},
		{"a null list", "tm-a", `{"replies":null}`, "replies must be a list; an empty list removes the type's suggested replies"},
		{"a blank entry", "tm-a", taskManualSuggestedRepliesBody(t, "ok", "   "), "replies[1] is blank"},
		{"an entry of 121 characters", "tm-a", taskManualSuggestedRepliesBody(t, strings.Repeat("句", 121)), "replies must be at most 120 characters per entry"},
		{"21 entries", "tm-a", taskManualSuggestedRepliesBody(t, twentyOne...), "replies must be at most 20 entries"},
	} {
		t.Run(tc.name+" answers 422, writes nothing and fans nothing", func(t *testing.T) {
			api, h, _, owner := taskManualSuggestedRepliesFixture(t)
			taskManualSuggestedRepliesPut(t, h, owner, "tm-a", "existing")
			dashboard := apiTestListen(t, api, "")

			status, data := apiJSON(t, h, "PUT", "/api/task-manual-suggested-replies/"+tc.typeKey, owner, tc.body)
			if status != 422 {
				t.Fatalf("want 422, got %d (%v)", status, data)
			}
			apiWantError(t, data, "validation_error", tc.message)
			dashboard.wantFrames()
			taskManualSuggestedRepliesWantLists(t, h, owner, map[string]any{"type_key": "tm-a", "replies": []any{"existing"}})
		})
	}

	for _, typeKey := range []string{"tm-ghost", "tm-deleted"} {
		t.Run("a type key naming no manual ("+typeKey+") answers 404, writes nothing and fans nothing", func(t *testing.T) {
			api, h, d, owner := taskManualSuggestedRepliesFixture(t)
			dalPutManual(t, d, TaskManual{TypeKey: "tm-deleted", Fields: "[]", Assignee: "{}"})
			if _, _, err := d.DeleteTaskManual("tm-deleted"); err != nil {
				t.Fatalf("DeleteTaskManual: %v", err)
			}
			dashboard := apiTestListen(t, api, "")

			status, data := apiJSON(t, h, "PUT", "/api/task-manual-suggested-replies/"+typeKey, owner,
				taskManualSuggestedRepliesBody(t, "x"))
			if status != 404 {
				t.Fatalf("want 404, got %d (%v)", status, data)
			}
			apiWantError(t, data, "not_found", "task manual '"+typeKey+"' not found")
			dashboard.wantFrames()
			taskManualSuggestedRepliesWantLists(t, h, owner)
		})
	}

	t.Run("an agent token answers 403 and no token answers 401, and neither writes", func(t *testing.T) {
		api, h, _, owner := taskManualSuggestedRepliesFixture(t)
		body := taskManualSuggestedRepliesBody(t, "x")

		status, data := apiJSON(t, h, "PUT", "/api/task-manual-suggested-replies/tm-a", apiTestAgentToken(t, api, "mira", ""), body)
		if status != 403 {
			t.Fatalf("admin agent: want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		status, data = apiJSON(t, h, "PUT", "/api/task-manual-suggested-replies/tm-a", "", body)
		if status != 401 {
			t.Fatalf("no token: want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		taskManualSuggestedRepliesWantLists(t, h, owner)
	})
}
