// Skeleton generated from server/ocserverd/api_document_history.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHistoryKeyParts(t *testing.T) {
	t.Skip("TODO: historyKeyParts reports a document-history key's PRIMARY identity and whether the key names a document at all.")
}

func TestDocumentHistoryContent(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestDocumentHistoryDTO(t *testing.T) {
	t.Skip("TODO: documentHistoryDTO is the CATALOGUE row: identity, provenance, the tombstone flag, and the SIZE of every field the revision holds — never the text.")
}

func TestDocumentHistoryRestoreDTO(t *testing.T) {
	t.Skip("TODO: documentHistoryRestoreDTO is the RESTORE receipt, and it deliberately still carries `content` — the shape that route has always answered with.")
}

func TestHistoryTombstoned(t *testing.T) {
	t.Skip("TODO: Overlay documents must retain their persisted tombstone state, not only the folded text exposed to readers.")
}

func TestUserContextHistorySnapshot(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestRoleDefHistorySnapshot(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestLessonsHistorySnapshot(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestUserContextSnapshotIn(t *testing.T) {
	t.Skip("TODO: The four readers below are what SaveWithDocumentHistory calls from inside the write transaction.")
}

func TestRoleDefSnapshotIn(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestLessonsSnapshotIn(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestManualSnapshotIn(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestTaskManualHistoryStreams(t *testing.T) {
	t.Skip("TODO: taskManualHistoryStreams names the series a manual write must retain.")
}

func TestRoleDefHistoryStreams(t *testing.T) {
	t.Skip("TODO: roleDefHistoryStreams is the role's counterpart of taskManualHistoryStreams: the ONE series a role write may retain, and only when the definition text itself changed.")
}

func TestDocumentHistoryAllowed(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestHandleListDocumentHistoryApiDocumentHistoryKindKeyGet(t *testing.T) {
	t.Run("a document written twice answers one catalogue row sized by field, newest first", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v2 is longer"}`)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/document-history/global_context/global", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{map[string]any{
			"id":          1,
			"created_ts":  apiAnyNumber,
			"actor_id":    "owner",
			"tombstoned":  false,
			"field_chars": map[string]any{"text": 2},
		}})
		dashboard.wantFrames()
	})

	t.Run("a document nobody has written twice answers an empty catalogue", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		rec := apiRequest(t, h, "GET", "/api/document-history/global_context/global", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{})
	})

	t.Run("a kind this server does not serve answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/document-history/bogus/global", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "unknown document history kind")
	})

	t.Run("the retired task_manual kind answers 400 naming both series that replaced it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/document-history/task_manual/build", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`document history kind "task_manual" was retired: use "task_manual_sop" or "task_manual_learnings"`)
	})

	t.Run("a lessons key carrying the retired separator answers 400 naming the removed axis", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/document-history/lessons/engineer::build", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`invalid lessons document history key: T-2 removed the task_type axis, so a lessons key is the bare role_key and one carrying "::" names nothing`)
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/document-history/global_context/global", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetDocumentVersionApiDocumentHistoryKindKeyIdGet(t *testing.T) {
	t.Run("a named revision answers the content map it was stored with beside the address asked for", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v2"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/document-history/global_context/global/1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":    "global_context",
			"key":     "global",
			"id":      1,
			"content": map[string]any{"text": "v1", "tombstoned": "false"},
		})
		dashboard.wantFrames()
	})

	t.Run("a role definition revision answers under the field name that kind stores", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		apiJSON(t, h, "POST", "/api/roles/r-design", owner, `{"definition_md":"# Duty rewritten"}`)

		status, data := apiJSON(t, h, "GET", "/api/document-history/role_definition/r-design/1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":    "role_definition",
			"key":     "r-design",
			"id":      1,
			"content": map[string]any{"definition_md": "# Duty", "tombstoned": "false"},
		})
	})

	t.Run("an id that is not a retained revision of this document answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v2"}`)

		status, data := apiJSON(t, h, "GET", "/api/document-history/global_context/global/99", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "document history version not found")
	})

	t.Run("a kind this server does not serve answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/document-history/bogus/global/1", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "unknown document history kind")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/document-history/global_context/global/1", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestDocumentSeedContent(t *testing.T) {
	t.Skip("TODO: documentSeedContent answers \"what would a reset of this document write back\", in the SAME field names a retained revision carries — which is what lets the cockpit hand it to the very same reader/diff the retained versions use.")
}

func TestHandleGetDocumentSeedApiDocumentHistoryKindKeySeedGet(t *testing.T) {
	t.Run("the user-custom block answers the empty document its reset would put back", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/document-history/global_context/global/seed", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":    "global_context",
			"key":     "global",
			"content": map[string]any{"text": "", "tombstoned": "true"},
		})
		dashboard.wantFrames()
	})

	t.Run("a seed role answers the shipped definition under that kind's field name", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/document-history/role_definition/assistant/seed", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind": "role_definition",
			"key":  "assistant",
			"content": map[string]any{
				"definition_md": apiTestAssistantSeedDefinitionMD,
				"tombstoned":    "true",
			},
		})
	})

	t.Run("a custom role has no shipped default and answers 404 naming the document", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}

		status, data := apiJSON(t, h, "GET", "/api/document-history/role_definition/r-design/seed", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found",
			"document 'role_definition/r-design' has no shipped default to compare against")
	})

	t.Run("a lessons doc has no shipped default and answers 404 naming the document", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/document-history/lessons/engineer/seed", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found",
			"document 'lessons/engineer' has no shipped default to compare against")
	})

	t.Run("a kind this server does not serve answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/document-history/bogus/global/seed", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "unknown document history kind")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/document-history/global_context/global/seed", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(t *testing.T) {
	t.Run("restoring a user-custom block revision answers the revision and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v2"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/document-history/global_context/global/1/restore", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":         1,
			"created_ts": apiAnyNumber,
			"actor_id":   "owner",
			"content":    map[string]any{"text": "v1", "tombstoned": "false"},
		})
		dashboard.wantFrames(map[string]any{
			"seq":   3,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   3,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()

		read, block := apiJSON(t, h, "GET", "/api/global-context", owner, "")
		if read != 200 {
			t.Fatalf("want 200, got %d (%v)", read, block)
		}
		apiWantBody(t, block, map[string]any{
			"text":           "v1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"org_name":       "",
		})
	})

	t.Run("restoring a role definition revision fans the owner-only role_def delta", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		apiJSON(t, h, "POST", "/api/roles/r-design", owner, `{"definition_md":"# Duty rewritten"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/document-history/role_definition/r-design/1/restore", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":         1,
			"created_ts": apiAnyNumber,
			"actor_id":   "owner",
			"content":    map[string]any{"definition_md": "# Duty", "tombstoned": "false"},
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "role_def",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "role_def",
				"key":     "owner::r-design",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("restoring a lessons revision fans the owner-only lessons delta", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		apiJSON(t, h, "POST", "/api/lessons/r-design", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/lessons/r-design", owner, `{"text":"v2"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/document-history/lessons/r-design/1/restore", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":         1,
			"created_ts": apiAnyNumber,
			"actor_id":   "owner",
			"content":    map[string]any{"text": "v1", "tombstoned": "false"},
		})
		dashboard.wantFrames(map[string]any{
			"seq":   3,
			"topic": "lessons",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "lessons",
				"key":     "owner::r-design",
				"epoch":   3,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("restoring an insight revision fans the owner-only insight delta", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		apiJSON(t, h, "POST", "/api/insight/r-design", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/insight/r-design", owner, `{"text":"v2"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/document-history/insight/r-design/1/restore", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":         1,
			"created_ts": apiAnyNumber,
			"actor_id":   "owner",
			"content":    map[string]any{"text": "v1", "tombstoned": "false"},
		})
		dashboard.wantFrames(map[string]any{
			"seq":   3,
			"topic": "insight",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "insight",
				"key":     "owner::r-design",
				"epoch":   3,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("restoring a role definition revision that is over the duty cap answers 400 and fans nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{
			RoleKey: "r-design", Name: "Design",
			DefinitionMD: strings.Repeat("x", 1500),
		}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		apiJSON(t, h, "POST", "/api/roles/r-design", owner, `{"definition_md":"y"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/document-history/role_definition/r-design/1/restore", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"restoring this version would violate the existing document size limit")
		dashboard.wantFrames()
	})

	t.Run("an id that is not a retained revision of this document answers 404 and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v2"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/document-history/global_context/global/99/restore", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "document history version not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent restoring the user-custom block answers 403 and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v2"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/document-history/global_context/global/1/restore", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "restoring this document requires admin capability")
		dashboard.wantFrames()
	})

	t.Run("an agent restoring another role's lessons answers 403 and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/lessons/assistant", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/lessons/assistant", owner, `{"text":"v2"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/document-history/lessons/assistant/1/restore", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "an agent may only write its own role's lessons")
		dashboard.wantFrames()
	})

	t.Run("an agent restoring another role's insight answers 403 and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/insight/assistant", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/insight/assistant", owner, `{"text":"v2"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/document-history/insight/assistant/1/restore", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "an agent may only write its own role's insight")
		dashboard.wantFrames()
	})

	t.Run("the retired task_manual kind answers 400 and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/document-history/task_manual/build/1/restore", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`document history kind "task_manual" was retired: use "task_manual_sop" or "task_manual_learnings"`)
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v2"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/document-history/global_context/global/1/restore", machine, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401 and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v2"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/document-history/global_context/global/1/restore", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})
}

func TestPublishDocumentHistoryRestore(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestTaskDescriptionRestoreAuthz(t *testing.T) {
	t.Skip("TODO: taskDescriptionRestoreAuthz answers whether this caller may put an earlier description back, and writes the refusal when not (T-e271).")
}

func TestRestoreDocumentHistory(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestRestoreTaskManualField(t *testing.T) {
	t.Skip("TODO: restoreTaskManualField writes back exactly the one field its stream versions and leaves every other field of the manual as it stands.")
}
