// Skeleton generated from server/ocserverd/api_taskmanuals.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestTaskManualSopHistorySnapshot(t *testing.T) {
	t.Skip("TODO: The two split snapshots carry ONE field each, and answer \"{}\" — the sentinel SaveWithDocumentHistories reads as \"nothing worth retaining\" — when that field is empty.")
}

func TestTaskManualLearningsHistorySnapshot(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestResolveTaskManual(t *testing.T) {
	t.Skip("TODO: resolveTaskManual returns the manual for typeKey (errNotFound when absent).")
}

func TestWriteTaskManual(t *testing.T) {
	t.Skip("TODO: writeTaskManual is the common single-manual READ response tail.")
}

func TestWriteTaskManualReceipt(t *testing.T) {
	t.Skip("TODO: writeTaskManualReceipt answers create_task_manual and update_task_manual (T-91).")
}

func TestValidateManualAssignee(t *testing.T) {
	t.Skip("TODO: validateManualAssignee checks an incoming assignee object: {} unsets; a populated object must carry a legal kind — \"staff\" (with a non-blank member_id) or \"outsource\".")
}

func TestResolveManualAssigneeMachine(t *testing.T) {
	t.Skip("TODO: resolveManualAssigneeMachine confirms an outsource assignee's `machine` names a machine that actually exists, writing the resolve error and returning false when it does not.")
}

func apiTestCreateTaskManual(t *testing.T, h http.Handler, token, body string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/task-manuals", token, body)
	if status != 200 {
		t.Fatalf("create task manual: %d %v", status, data)
	}
	typeKey, _ := data["type_key"].(string)
	if typeKey == "" {
		t.Fatalf("create task manual must answer a type_key: %v", data)
	}
	return typeKey
}

func apiTestWantTaskManual(t *testing.T, h http.Handler, token, typeKey string, want map[string]any) {
	t.Helper()
	status, data := apiJSON(t, h, "GET", "/api/task-manuals/"+typeKey, token, "")
	if status != 200 {
		t.Fatalf("read back %s: %d %v", typeKey, status, data)
	}
	apiWantBody(t, data, want)
}

func apiTestTaskManualList(t *testing.T, h http.Handler, token string) []any {
	t.Helper()
	rec := apiRequest(t, h, "GET", "/api/task-manuals", token, "")
	if rec.Code != 200 {
		t.Fatalf("list task manuals: %d %s", rec.Code, rec.Body.String())
	}
	var got []any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("non-JSON body: %s", rec.Body.String())
	}
	return got
}

func apiTestTaskManualFrame(seq int, typeKey, trigger string) map[string]any {
	return map[string]any{
		"seq":   seq,
		"topic": "task_manual",
		"op":    "patch",
		"data": map[string]any{
			"entity":  "task_manual",
			"key":     "owner::" + typeKey,
			"epoch":   seq,
			"deleted": false,
			"payload": nil,
		},
		"ts":      apiAnyNumber,
		"trigger": trigger,
	}
}

func TestHandleListTaskManualsApiTaskManualsGet(t *testing.T) {
	t.Run("a station that has minted no type answers an empty list", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		got := apiTestTaskManualList(t, h, owner)
		apiWantValue(t, "body", any(got), any([]any{}))
		dashboard.wantFrames()
	})

	t.Run("every type is listed by display name carrying its documents' sizes and caps but neither document", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-ship"}`)
		apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", agent,
			`{"purpose":"對客戶報價","sop_md":"# SOP\n步驟一","learnings":"第一課"}`)
		dashboard := apiTestListen(t, api, "")

		got := apiTestTaskManualList(t, h, owner)
		apiWantValue(t, "body", any(got), any([]any{
			map[string]any{
				"type_key":            "tm-ship",
				"display_name":        "tm-ship",
				"purpose":             "",
				"fields":              []any{},
				"assignee":            map[string]any{},
				"learnings_chars":     0,
				"sop_md_chars":        0,
				"learnings_cap_chars": 15000,
				"sop_md_cap_chars":    15000,
				"cap_chars":           15000,
				"updated_ts":          apiAnyNumber,
			},
			map[string]any{
				"type_key":            "tm-quote",
				"display_name":        "報價",
				"purpose":             "對客戶報價",
				"fields":              []any{},
				"assignee":            map[string]any{},
				"learnings_chars":     3,
				"sop_md_chars":        9,
				"learnings_cap_chars": 15000,
				"sop_md_cap_chars":    15000,
				"cap_chars":           15000,
				"updated_ts":          apiAnyNumber,
			},
		}))
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/task-manuals", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleCreateTaskManualApiTaskManualsPost(t *testing.T) {
	t.Run("a create carrying only a display name mints the type key and fans the owner's manual delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")
		asker := apiTestListen(t, api, apiTestPlainAgentID)

		status, data := apiJSON(t, h, "POST", "/api/task-manuals", agent, `{"display_name":"報價"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":   apiAnyString,
			"updated_ts": apiAnyNumber,
		})
		typeKey, _ := data["type_key"].(string)
		dashboard.wantFrames(apiTestTaskManualFrame(1, typeKey, apiTestPlainAgentID))
		asker.wantFrames()
		apiTestWantTaskManual(t, h, owner, typeKey, map[string]any{
			"type_key":            typeKey,
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("a legacy explicit type key is taken verbatim and doubles as the display name", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals", agent, `{"type_key":"tm-quote"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":   "tm-quote",
			"updated_ts": apiAnyNumber,
		})
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "tm-quote",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
		dashboard.wantFrames(apiTestTaskManualFrame(1, "tm-quote", apiTestPlainAgentID))
	})

	t.Run("a second create of the same type key answers 409 and leaves the first one standing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals", agent, `{"type_key":"tm-quote","display_name":"改名"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task manual 'tm-quote' already exists")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("a create naming neither a type key nor a display name answers 400 and mints nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals", agent, `{}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_name must not be blank")
		dashboard.wantFrames()
		apiWantValue(t, "body", any(apiTestTaskManualList(t, h, owner)), any([]any{}))
	})

	t.Run("an admin agent's assignee is stored on the new type", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")

		typeKey := apiTestCreateTaskManual(t, h, admin,
			`{"display_name":"派工","assignee":{"kind":"staff","member_id":"kip"}}`)
		apiTestWantTaskManual(t, h, owner, typeKey, map[string]any{
			"type_key":            typeKey,
			"display_name":        "派工",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{"kind": "staff", "member_id": "kip"},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("a plain agent supplying an assignee answers 403 and mints nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals", agent,
			`{"display_name":"派工","assignee":{"kind":"staff","member_id":"kip"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden",
			"assignee is owner/admin-agent governance — a plain agent may not set who executes a task type")
		dashboard.wantFrames()
		apiWantValue(t, "body", any(apiTestTaskManualList(t, h, owner)), any([]any{}))
	})

	t.Run("an assignee spelled with the retired member kind answers 400 naming the rename", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals", admin,
			`{"display_name":"派工","assignee":{"kind":"member","member_id":"kip"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"assignee kind: task executor kind \"member\" was renamed to \"staff\" (T-101); the closed set is {\"staff\", \"outsource\"}")
		dashboard.wantFrames()
		apiWantValue(t, "body", any(apiTestTaskManualList(t, h, owner)), any([]any{}))
	})

	t.Run("an outsource assignee pinned to a machine nobody onboarded answers 404 and mints nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals", admin,
			`{"display_name":"派工","assignee":{"kind":"outsource","machine":"m-ghost"}}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'm-ghost' not found")
		dashboard.wantFrames()
		apiWantValue(t, "body", any(apiTestTaskManualList(t, h, owner)), any([]any{}))
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machine := apiTestAgentToken(t, api, ServerSelfHost, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals", machine, `{"display_name":"報價"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		apiWantValue(t, "body", any(apiTestTaskManualList(t, h, owner)), any([]any{}))
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals", "", `{"display_name":"報價"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		apiWantValue(t, "body", any(apiTestTaskManualList(t, h, owner)), any([]any{}))
	})
}

func TestHandleGetTaskManualApiTaskManualsTypeKeyGet(t *testing.T) {
	t.Run("a type is served in full with both documents and their separate caps", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", agent,
			`{"purpose":"對客戶報價","sop_md":"# SOP\n步驟一","learnings":"第一課","fields":[{"name":" 客戶 ","required":true,"is_key":true}]}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/task-manuals/tm-quote", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":     "tm-quote",
			"display_name": "報價",
			"purpose":      "對客戶報價",
			"fields": []any{
				map[string]any{"name": "客戶", "required": true, "is_key": true},
			},
			"sop_md":              "# SOP\n步驟一",
			"learnings":           "第一課",
			"assignee":            map[string]any{},
			"learnings_chars":     3,
			"sop_md_chars":        9,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
		dashboard.wantFrames()
	})

	t.Run("an unknown type key answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/task-manuals/tm-ghost", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task manual 'tm-ghost' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/task-manuals/tm-quote", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUpdateTaskManualApiTaskManualsTypeKeyPost(t *testing.T) {
	t.Run("a partial edit changes only the fields it names and reports only the documents it wrote", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")
		asker := apiTestListen(t, api, apiTestPlainAgentID)

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", agent,
			`{"purpose":"對客戶報價","sop_md":"# SOP\n步驟一","fields":[{"name":" 客戶 ","required":true,"is_key":true}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":         "tm-quote",
			"updated_ts":       apiAnyNumber,
			"sop_md_chars":     9,
			"sop_md_cap_chars": 15000,
			"sop_md_sha256":    "4463e39266c9a31c7dea3dc806c1567c97c34ed1d69af09aaedf4a302bc78aad",
		})
		dashboard.wantFrames(apiTestTaskManualFrame(2, "tm-quote", apiTestPlainAgentID))
		asker.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":     "tm-quote",
			"display_name": "報價",
			"purpose":      "對客戶報價",
			"fields": []any{
				map[string]any{"name": "客戶", "required": true, "is_key": true},
			},
			"sop_md":              "# SOP\n步驟一",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        9,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("an edit that resends both documents reports a triple for each of them", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", agent,
			`{"sop_md":"# SOP\n步驟一","learnings":"第一課"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":            "tm-quote",
			"updated_ts":          apiAnyNumber,
			"sop_md_chars":        9,
			"sop_md_cap_chars":    15000,
			"sop_md_sha256":       "4463e39266c9a31c7dea3dc806c1567c97c34ed1d69af09aaedf4a302bc78aad",
			"learnings_chars":     3,
			"learnings_cap_chars": 15000,
			"learnings_sha256":    "7c50dedd8cc42f2f1e96dc6a013e49de801c5a445f9569e9045c6ec63409e3f9",
		})
	})

	t.Run("the write_task_learnings spelling of the learnings key answers 422 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", agent, `{"learnings":"第一課"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", agent, `{"text":"覆蓋"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: json: unknown field \"text\"")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "第一課",
			"assignee":            map[string]any{},
			"learnings_chars":     3,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("a blank field name answers 400 and leaves the whole partial edit unwritten", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", agent,
			`{"purpose":"對客戶報價","fields":[{"name":"  "}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field name must not be blank")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("an identity-key field that is not required answers 400 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", agent,
			`{"fields":[{"name":"客戶","is_key":true}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "identity-key field '客戶' must be required")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("a plain agent supplying an assignee answers 403 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", agent,
			`{"purpose":"對客戶報價","assignee":{"kind":"staff","member_id":"kip"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden",
			"assignee is owner/admin-agent governance — a plain agent may not set who executes a task type")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("an unknown type key answers 404", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-ghost", agent, `{"purpose":"對客戶報價"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task manual 'tm-ghost' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		machine := apiTestAgentToken(t, api, ServerSelfHost, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", machine, `{"purpose":"對客戶報價"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", "", `{"purpose":"對客戶報價"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})
}

func TestHandleDeleteTaskManualApiTaskManualsTypeKeyDelete(t *testing.T) {
	t.Run("a type nobody is running answers the delete receipt and fans the owner's manual delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		admin := apiTestAgentToken(t, api, "mira", "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")
		asker := apiTestListen(t, api, apiTestPlainAgentID)

		status, data := apiJSON(t, h, "DELETE", "/api/task-manuals/tm-quote", admin, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"type_key": "tm-quote", "deleted": true})
		dashboard.wantFrames(apiTestTaskManualFrame(2, "tm-quote", "mira"))
		asker.wantFrames()
		apiWantValue(t, "body", any(apiTestTaskManualList(t, h, owner)), any([]any{}))
	})

	t.Run("a type that still has an open task answers 409 and keeps the manual", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		admin := apiTestAgentToken(t, api, "mira", "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		if status, data := apiJSON(t, h, "POST", "/api/tasks", agent,
			`{"title":"報一張價","type_key":"tm-quote","executor_member_id":"kip"}`); status != 200 {
			t.Fatalf("create task: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/task-manuals/tm-quote", admin, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task manual 'tm-quote' still has open tasks — close them first")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("an unknown type key answers 404", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/task-manuals/tm-ghost", admin, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task manual 'tm-ghost' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/task-manuals/tm-quote", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/task-manuals/tm-quote", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		apiWantValue(t, "body", any(apiTestTaskManualList(t, h, owner)), any([]any{
			map[string]any{
				"type_key":            "tm-quote",
				"display_name":        "報價",
				"purpose":             "",
				"fields":              []any{},
				"assignee":            map[string]any{},
				"learnings_chars":     0,
				"sop_md_chars":        0,
				"learnings_cap_chars": 15000,
				"sop_md_cap_chars":    15000,
				"cap_chars":           15000,
				"updated_ts":          apiAnyNumber,
			},
		}))
	})
}

func TestHandleWriteTaskLearningsApiTaskManualsTypeKeyLearningsPost(t *testing.T) {
	t.Run("a whole-doc write replaces the learnings and answers its size, cap and hash", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")
		asker := apiTestListen(t, api, apiTestPlainAgentID)

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings", agent, `{"text":"第一課"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":   "tm-quote",
			"size_chars": 3,
			"cap_chars":  15000,
			"sha256":     "7c50dedd8cc42f2f1e96dc6a013e49de801c5a445f9569e9045c6ec63409e3f9",
		})
		dashboard.wantFrames(apiTestTaskManualFrame(2, "tm-quote", apiTestPlainAgentID))
		asker.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "第一課",
			"assignee":            map[string]any{},
			"learnings_chars":     3,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("the update_task_manual spelling of the document key answers 422 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings", agent, `{"text":"第一課"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings", agent, `{"learnings":"覆蓋"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: json: unknown field \"learnings\"")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "第一課",
			"assignee":            map[string]any{},
			"learnings_chars":     3,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("an empty text over an accumulated doc answers 400 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings", agent, `{"text":"第一課"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings", agent, `{"text":""}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"this would replace the existing learnings with an empty doc — pass allow_shrink=true "+
				"if that is intended; nothing was written")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "第一課",
			"assignee":            map[string]any{},
			"learnings_chars":     3,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("allow_shrink clears the doc and answers the empty document's hash", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings", agent, `{"text":"第一課"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings", agent,
			`{"text":"","allow_shrink":true}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":   "tm-quote",
			"size_chars": 0,
			"cap_chars":  15000,
			"sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})
		dashboard.wantFrames(apiTestTaskManualFrame(3, "tm-quote", apiTestPlainAgentID))
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("an unknown type key answers 404", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-ghost/learnings", agent, `{"text":"第一課"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task manual 'tm-ghost' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		machine := apiTestAgentToken(t, api, ServerSelfHost, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings", machine, `{"text":"第一課"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings", "", `{"text":"第一課"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	})
}

func TestHandlePatchTaskLearningsApiTaskManualsTypeKeyLearningsPatchPost(t *testing.T) {
	setup := func(t *testing.T, seeded string) (*apiServer, http.Handler, string, string) {
		t.Helper()
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		if seeded != "" {
			if status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings", agent,
				`{"text":"`+seeded+`"}`); status != 200 {
				t.Fatalf("seed learnings: %d %v", status, data)
			}
		}
		return api, h, owner, agent
	}
	wantLearnings := func(t *testing.T, h http.Handler, owner, text string, chars int) {
		t.Helper()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              "",
			"learnings":           text,
			"assignee":            map[string]any{},
			"learnings_chars":     chars,
			"sop_md_chars":        0,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	}

	t.Run("an empty anchor appends to the doc and fans the owner's manual delta", func(t *testing.T) {
		api, h, owner, agent := setup(t, "")
		dashboard := apiTestListen(t, api, "")
		asker := apiTestListen(t, api, apiTestPlainAgentID)

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings/patch", agent,
			`{"edits":[{"new":"甲\n"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":      "tm-quote",
			"applied_edits": 1,
			"size_chars":    2,
			"cap_chars":     15000,
			"sha256":        "7589c50dcc90a9be456502ec1ebf077ed6c882f7ebe475a0fd6cb3a07f6b066c",
		})
		dashboard.wantFrames(apiTestTaskManualFrame(2, "tm-quote", apiTestPlainAgentID))
		asker.wantFrames()
		wantLearnings(t, h, owner, "甲\n", 2)
	})

	t.Run("a unique anchor is spliced in place", func(t *testing.T) {
		api, h, owner, agent := setup(t, "甲\\n")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings/patch", agent,
			`{"edits":[{"old":"甲","new":"乙"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":      "tm-quote",
			"applied_edits": 1,
			"size_chars":    2,
			"cap_chars":     15000,
			"sha256":        "d7d75b8c747529fa466c1edd6209ce3ecba924d70b1883525ddde333e065fd38",
		})
		dashboard.wantFrames(apiTestTaskManualFrame(3, "tm-quote", apiTestPlainAgentID))
		wantLearnings(t, h, owner, "乙\n", 2)
	})

	t.Run("an anchor that matches nothing answers 400 and writes nothing", func(t *testing.T) {
		api, h, owner, agent := setup(t, "甲\\n")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings/patch", agent,
			`{"edits":[{"old":"丙","new":"丁"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"edits[0]: old not found in the current doc — re-read (get_task_manual) and re-anchor; nothing was written")
		dashboard.wantFrames()
		wantLearnings(t, h, owner, "甲\n", 2)
	})

	t.Run("an empty edit list answers 422 and writes nothing", func(t *testing.T) {
		api, h, owner, agent := setup(t, "甲\\n")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings/patch", agent, `{"edits":[]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "edits requires at least one {old, new} entry")
		dashboard.wantFrames()
		wantLearnings(t, h, owner, "甲\n", 2)
	})

	t.Run("an edit carrying neither old nor new answers 422 and writes nothing", func(t *testing.T) {
		api, h, owner, agent := setup(t, "甲\\n")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings/patch", agent, `{"edits":[{}]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"edits[0]: neither old nor new was given — an edit needs at least one of them "+
				"(empty old appends new); nothing was written")
		dashboard.wantFrames()
		wantLearnings(t, h, owner, "甲\n", 2)
	})

	t.Run("a batch that undoes itself counts its edits but writes nothing and fans nothing", func(t *testing.T) {
		api, h, owner, agent := setup(t, "甲\\n")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings/patch", agent,
			`{"edits":[{"old":"甲","new":"乙"},{"old":"乙","new":"甲"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":      "tm-quote",
			"applied_edits": 2,
			"size_chars":    2,
			"cap_chars":     15000,
			"sha256":        "7589c50dcc90a9be456502ec1ebf077ed6c882f7ebe475a0fd6cb3a07f6b066c",
		})
		dashboard.wantFrames()
		wantLearnings(t, h, owner, "甲\n", 2)
	})

	t.Run("a patch that would empty the doc answers 400 without allow_shrink", func(t *testing.T) {
		api, h, owner, agent := setup(t, "甲\\n")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings/patch", agent,
			`{"edits":[{"old":"甲\n","new":""}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"patch would empty (or shrink to under a tenth of) the learnings doc — pass allow_shrink=true "+
				"if this is intended, or use write_task_learnings; nothing was written")
		dashboard.wantFrames()
		wantLearnings(t, h, owner, "甲\n", 2)
	})

	t.Run("an unknown type key answers 404", func(t *testing.T) {
		api, h, _, agent := setup(t, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-ghost/learnings/patch", agent,
			`{"edits":[{"new":"甲"}]}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task manual 'tm-ghost' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, owner, _ := setup(t, "甲\\n")
		machine := apiTestAgentToken(t, api, ServerSelfHost, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings/patch", machine,
			`{"edits":[{"new":"乙"}]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		wantLearnings(t, h, owner, "甲\n", 2)
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, owner, _ := setup(t, "甲\\n")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/learnings/patch", "",
			`{"edits":[{"new":"乙"}]}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		wantLearnings(t, h, owner, "甲\n", 2)
	})
}

func TestHandlePatchTaskSopApiTaskManualsTypeKeySopPatchPost(t *testing.T) {
	setup := func(t *testing.T) (*apiServer, http.Handler, string, string) {
		t.Helper()
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-quote","display_name":"報價"}`)
		if status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote", agent,
			`{"sop_md":"# SOP\n步驟一"}`); status != 200 {
			t.Fatalf("seed sop: %d %v", status, data)
		}
		return api, h, owner, agent
	}
	wantSop := func(t *testing.T, h http.Handler, owner, text string, chars int) {
		t.Helper()
		apiTestWantTaskManual(t, h, owner, "tm-quote", map[string]any{
			"type_key":            "tm-quote",
			"display_name":        "報價",
			"purpose":             "",
			"fields":              []any{},
			"sop_md":              text,
			"learnings":           "",
			"assignee":            map[string]any{},
			"learnings_chars":     0,
			"sop_md_chars":        chars,
			"learnings_cap_chars": 15000,
			"sop_md_cap_chars":    15000,
			"cap_chars":           15000,
			"updated_ts":          apiAnyNumber,
		})
	}

	t.Run("a unique anchor is spliced in place and fans the owner's manual delta", func(t *testing.T) {
		api, h, owner, agent := setup(t)
		dashboard := apiTestListen(t, api, "")
		asker := apiTestListen(t, api, apiTestPlainAgentID)

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/sop/patch", agent,
			`{"edits":[{"old":"步驟一","new":"步驟一\n步驟二"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":      "tm-quote",
			"applied_edits": 1,
			"size_chars":    13,
			"cap_chars":     15000,
			"sha256":        "7ef2731f09d79ff3529d190b13e2853e39d01ce10beab4ab44ff4f22856f8172",
		})
		dashboard.wantFrames(apiTestTaskManualFrame(3, "tm-quote", apiTestPlainAgentID))
		asker.wantFrames()
		wantSop(t, h, owner, "# SOP\n步驟一\n步驟二", 13)
	})

	t.Run("an empty anchor appends to the doc", func(t *testing.T) {
		api, h, owner, agent := setup(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/sop/patch", agent,
			`{"edits":[{"new":"步驟二"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":      "tm-quote",
			"applied_edits": 1,
			"size_chars":    13,
			"cap_chars":     15000,
			"sha256":        "7ef2731f09d79ff3529d190b13e2853e39d01ce10beab4ab44ff4f22856f8172",
		})
		dashboard.wantFrames(apiTestTaskManualFrame(3, "tm-quote", apiTestPlainAgentID))
		wantSop(t, h, owner, "# SOP\n步驟一\n步驟二", 13)
	})

	t.Run("an anchor that matches nothing answers 400 and writes nothing", func(t *testing.T) {
		api, h, owner, agent := setup(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/sop/patch", agent,
			`{"edits":[{"old":"步驟九","new":"步驟十"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"edits[0]: old not found in the current doc — re-read (get_task_manual) and re-anchor; nothing was written")
		dashboard.wantFrames()
		wantSop(t, h, owner, "# SOP\n步驟一", 9)
	})

	t.Run("an empty edit list answers 422 and writes nothing", func(t *testing.T) {
		api, h, owner, agent := setup(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/sop/patch", agent, `{"edits":[]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "edits requires at least one {old, new} entry")
		dashboard.wantFrames()
		wantSop(t, h, owner, "# SOP\n步驟一", 9)
	})

	t.Run("an edit carrying neither old nor new answers 422 and writes nothing", func(t *testing.T) {
		api, h, owner, agent := setup(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/sop/patch", agent, `{"edits":[{}]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"edits[0]: neither old nor new was given — an edit needs at least one of them "+
				"(empty old appends new); nothing was written")
		dashboard.wantFrames()
		wantSop(t, h, owner, "# SOP\n步驟一", 9)
	})

	t.Run("a batch that undoes itself counts its edits but writes nothing and fans nothing", func(t *testing.T) {
		api, h, owner, agent := setup(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/sop/patch", agent,
			`{"edits":[{"old":"步驟一","new":"步驟二"},{"old":"步驟二","new":"步驟一"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key":      "tm-quote",
			"applied_edits": 2,
			"size_chars":    9,
			"cap_chars":     15000,
			"sha256":        "4463e39266c9a31c7dea3dc806c1567c97c34ed1d69af09aaedf4a302bc78aad",
		})
		dashboard.wantFrames()
		wantSop(t, h, owner, "# SOP\n步驟一", 9)
	})

	t.Run("a patch that would empty the doc answers 400 without allow_shrink", func(t *testing.T) {
		api, h, owner, agent := setup(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/sop/patch", agent,
			`{"edits":[{"old":"# SOP\n步驟一","new":""}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"patch would empty (or shrink to under a tenth of) the sop_md doc — pass allow_shrink=true "+
				"if this is intended, or use update_task_manual; nothing was written")
		dashboard.wantFrames()
		wantSop(t, h, owner, "# SOP\n步驟一", 9)
	})

	t.Run("an unknown type key answers 404", func(t *testing.T) {
		api, h, _, agent := setup(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-ghost/sop/patch", agent,
			`{"edits":[{"new":"步驟二"}]}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task manual 'tm-ghost' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, owner, _ := setup(t)
		machine := apiTestAgentToken(t, api, ServerSelfHost, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/sop/patch", machine,
			`{"edits":[{"new":"步驟二"}]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		wantSop(t, h, owner, "# SOP\n步驟一", 9)
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, owner, _ := setup(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/task-manuals/tm-quote/sop/patch", "",
			`{"edits":[{"new":"步驟二"}]}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		wantSop(t, h, owner, "# SOP\n步驟一", 9)
	})
}
