package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestHistoryKeyParts(t *testing.T) {
	for name, tc := range map[string]struct {
		kind    string
		key     string
		primary string
		valid   bool
	}{
		"a key names the document it addresses": {kind: "role_definition", key: "engineer", primary: "engineer", valid: true},
		"a separator is ordinary in a key":      {kind: "role_definition", key: "engineer::build", primary: "engineer::build", valid: true},
		"an empty key names nothing":            {kind: "global_context", key: "", primary: "", valid: false},
	} {
		t.Run(name, func(t *testing.T) {
			primary, valid := historyKeyParts(tc.kind, tc.key)
			if primary != tc.primary || valid != tc.valid {
				t.Fatalf("historyKeyParts(%q, %q) = (%q, %v), want (%q, %v)",
					tc.kind, tc.key, primary, valid, tc.primary, tc.valid)
			}
		})
	}
}

func TestDocumentHistoryContent(t *testing.T) {
	t.Run("a retained JSON object becomes its complete string map", func(t *testing.T) {
		got, err := documentHistoryContent(DocumentHistory{
			ContentJSON: `{"text":"正文","tombstoned":"true","definition_md":"# Duty"}`,
		})
		if err != nil {
			t.Fatalf("documentHistoryContent: %v", err)
		}
		want := map[string]string{"definition_md": "# Duty", "text": "正文", "tombstoned": "true"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("content = %#v, want %#v", got, want)
		}
	})

	t.Run("malformed retained content returns its decoding error and no map", func(t *testing.T) {
		got, err := documentHistoryContent(DocumentHistory{ContentJSON: `{"text":`})
		if got != nil {
			t.Fatalf("content = %#v, want nil", got)
		}
		if err == nil || err.Error() != "unexpected end of JSON input" {
			t.Fatalf("err = %v, want unexpected end of JSON input", err)
		}
	})
}

func TestDocumentHistoryDTO(t *testing.T) {
	t.Run("a catalogue row preserves identity and measures every document field except tombstoned", func(t *testing.T) {
		got, err := documentHistoryDTO(DocumentHistory{
			ID:          17,
			CreatedTS:   1234.5,
			ActorID:     "agent-kip",
			ContentJSON: `{"text":"甲乙","definition_md":"é😊","tombstoned":"true"}`,
		})
		if err != nil {
			t.Fatalf("documentHistoryDTO: %v", err)
		}
		want := DocumentHistoryDTO{
			Id: 17, CreatedTs: 1234.5, ActorId: "agent-kip", Tombstoned: true,
			FieldChars: map[string]int{"definition_md": 2, "text": 2},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("dto = %#v, want %#v", got, want)
		}
	})

	t.Run("malformed retained content returns its decoding error and a zero catalogue row", func(t *testing.T) {
		got, err := documentHistoryDTO(DocumentHistory{ContentJSON: `{"text":`})
		if !reflect.DeepEqual(got, DocumentHistoryDTO{}) {
			t.Fatalf("dto = %#v, want zero value", got)
		}
		if err == nil || err.Error() != "unexpected end of JSON input" {
			t.Fatalf("err = %v, want unexpected end of JSON input", err)
		}
	})
}

func TestDocumentHistoryRestoreDTO(t *testing.T) {
	t.Run("a restore receipt preserves identity and the complete retained content", func(t *testing.T) {
		got, err := documentHistoryRestoreDTO(DocumentHistory{
			ID:          23,
			CreatedTS:   2345.5,
			ActorID:     "owner",
			ContentJSON: `{"text":"復原","tombstoned":"false"}`,
		})
		if err != nil {
			t.Fatalf("documentHistoryRestoreDTO: %v", err)
		}
		want := DocumentHistoryRestoreDTO{
			Id: 23, CreatedTs: 2345.5, ActorId: "owner",
			Content: map[string]string{"text": "復原", "tombstoned": "false"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("dto = %#v, want %#v", got, want)
		}
	})

	t.Run("malformed retained content returns its decoding error and a zero restore receipt", func(t *testing.T) {
		got, err := documentHistoryRestoreDTO(DocumentHistory{ContentJSON: `{"text":`})
		if !reflect.DeepEqual(got, DocumentHistoryRestoreDTO{}) {
			t.Fatalf("dto = %#v, want zero value", got)
		}
		if err == nil || err.Error() != "unexpected end of JSON input" {
			t.Fatalf("err = %v, want unexpected end of JSON input", err)
		}
	})
}

func TestHistoryTombstoned(t *testing.T) {
	for name, tc := range map[string]struct {
		content map[string]string
		want    bool
	}{
		"the true marker is true":    {content: map[string]string{"tombstoned": "true"}, want: true},
		"the false marker is false":  {content: map[string]string{"tombstoned": "false"}, want: false},
		"a missing marker is false":  {content: map[string]string{}, want: false},
		"an invalid marker is false": {content: map[string]string{"tombstoned": "maybe"}, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			got := historyTombstoned(tc.content)
			if got != tc.want {
				t.Fatalf("historyTombstoned(%#v) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func TestUserContextHistorySnapshot(t *testing.T) {
	for name, tc := range map[string]struct {
		current *UserContext
		want    string
	}{
		"no row":           {current: nil, want: `{}`},
		"a live empty row": {current: &UserContext{Text: "", Tombstoned: false}, want: `{"text":"","tombstoned":"false"}`},
		"a tombstoned row": {current: &UserContext{Text: "全域規則", Tombstoned: true}, want: `{"text":"全域規則","tombstoned":"true"}`},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := userContextHistorySnapshot(tc.current)
			if err != nil {
				t.Fatalf("userContextHistorySnapshot: %v", err)
			}
			if got != tc.want {
				t.Fatalf("snapshot = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRoleDefHistorySnapshot(t *testing.T) {
	for name, tc := range map[string]struct {
		current *RoleDef
		want    string
	}{
		"no row": {current: nil, want: `{}`},
		"a definition keeps text and tombstone but not the current display name": {
			current: &RoleDef{RoleKey: "r-design", Name: "目前名稱", DefinitionMD: "# 職責", Tombstoned: true},
			want:    `{"definition_md":"# 職責","tombstoned":"true"}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := roleDefHistorySnapshot(tc.current)
			if err != nil {
				t.Fatalf("roleDefHistorySnapshot: %v", err)
			}
			if got != tc.want {
				t.Fatalf("snapshot = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUserContextSnapshotIn(t *testing.T) {
	t.Run("the transaction reader returns the live text and tombstone", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"交易前"}`)
		got, err := userContextSnapshotIn(d.rdb)
		if err != nil {
			t.Fatalf("userContextSnapshotIn: %v", err)
		}
		if got != `{"text":"交易前","tombstoned":"false"}` {
			t.Fatalf("snapshot = %q, want %q", got, `{"text":"交易前","tombstoned":"false"}`)
		}
	})

	t.Run("the transaction reader represents a never-written block as the empty object", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)
		got, err := userContextSnapshotIn(d.rdb)
		if err != nil {
			t.Fatalf("userContextSnapshotIn: %v", err)
		}
		if got != `{}` {
			t.Fatalf("snapshot = %q, want %q", got, `{}`)
		}
	})
}

func TestRoleDefSnapshotIn(t *testing.T) {
	t.Run("the transaction reader returns the addressed role definition", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{
			RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty", Tombstoned: true,
		}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		got, err := roleDefSnapshotIn("r-design")(d.rdb)
		if err != nil {
			t.Fatalf("roleDefSnapshotIn: %v", err)
		}
		if got != `{"definition_md":"# Duty","tombstoned":"true"}` {
			t.Fatalf("snapshot = %q, want %q", got, `{"definition_md":"# Duty","tombstoned":"true"}`)
		}
	})

	t.Run("the transaction reader represents an absent role definition as the empty object", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)
		got, err := roleDefSnapshotIn("r-missing")(d.rdb)
		if err != nil {
			t.Fatalf("roleDefSnapshotIn: %v", err)
		}
		if got != `{}` {
			t.Fatalf("snapshot = %q, want %q", got, `{}`)
		}
	})
}

func TestManualSnapshotIn(t *testing.T) {
	t.Run("the transaction reader gives the current manual to the requested projection", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)
		if err := d.PutTaskManual(TaskManual{
			TypeKey: "tm-history", DisplayName: "歷史", Purpose: "目的",
			Fields: "[]", SopMD: "SOP 目前版",
			Assignee: "{}", UpdatedTS: 7,
		}); err != nil {
			t.Fatalf("PutTaskManual: %v", err)
		}
		got, err := manualSnapshotIn("tm-history", func(m TaskManual) (string, error) {
			return m.Purpose + " / " + m.SopMD, nil
		})(d.rdb)
		if err != nil {
			t.Fatalf("manualSnapshotIn: %v", err)
		}
		if got != "目的 / SOP 目前版" {
			t.Fatalf("snapshot = %q, want %q", got, "目的 / SOP 目前版")
		}
	})

	t.Run("an absent manual becomes the empty object without calling the projection", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)
		called := false
		got, err := manualSnapshotIn("tm-missing", func(TaskManual) (string, error) {
			called = true
			return "unexpected", nil
		})(d.rdb)
		if err != nil {
			t.Fatalf("manualSnapshotIn: %v", err)
		}
		if got != `{}` {
			t.Fatalf("snapshot = %q, want %q", got, `{}`)
		}
		if called {
			t.Fatal("the projection was called for an absent manual")
		}
	})

	t.Run("a projection error is returned to the transaction caller", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)
		if err := d.PutTaskManual(TaskManual{TypeKey: "tm-history", Fields: "[]", Assignee: "{}"}); err != nil {
			t.Fatalf("PutTaskManual: %v", err)
		}
		wantErr := errors.New("projection failed")
		got, err := manualSnapshotIn("tm-history", func(TaskManual) (string, error) {
			return "", wantErr
		})(d.rdb)
		if got != "" {
			t.Fatalf("snapshot = %q, want empty string", got)
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want %v", err, wantErr)
		}
	})
}

func TestTaskManualHistoryStreams(t *testing.T) {
	for name, tc := range map[string]struct {
		sopChanged bool
		wantKinds  []string
	}{
		"the versioned document did not change": {sopChanged: false, wantKinds: nil},
		"the SOP changed":                       {sopChanged: true, wantKinds: []string{docKindTaskManualSop}},
	} {
		t.Run(name, func(t *testing.T) {
			got := taskManualHistoryStreams("tm-history", "owner", tc.sopChanged)
			if len(got) != len(tc.wantKinds) || (got == nil) != (tc.wantKinds == nil) {
				t.Fatalf("stream count/nil = %d/%v, want %d/%v", len(got), got == nil, len(tc.wantKinds), tc.wantKinds == nil)
			}
			for i, stream := range got {
				if stream.Kind != tc.wantKinds[i] || stream.Key != "tm-history" || stream.ActorID != "owner" {
					t.Fatalf("stream[%d] = {%q, %q, %q}, want {%q, %q, %q}",
						i, stream.Kind, stream.Key, stream.ActorID, tc.wantKinds[i], "tm-history", "owner")
				}
			}
		})
	}

	t.Run("the selected streams read their own current fields", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)
		if err := d.PutTaskManual(TaskManual{
			TypeKey: "tm-history", Fields: "[]", SopMD: "SOP 目前版", Assignee: "{}",
		}); err != nil {
			t.Fatalf("PutTaskManual: %v", err)
		}
		gotStreams := taskManualHistoryStreams("tm-history", "owner", true)
		wantSnapshots := []string{`{"sop_md":"SOP 目前版"}`}
		if len(gotStreams) != 1 {
			t.Fatalf("stream count = %d, want 1", len(gotStreams))
		}
		for i, stream := range gotStreams {
			wantKind := []string{docKindTaskManualSop}[i]
			if stream.Kind != wantKind || stream.Key != "tm-history" || stream.ActorID != "owner" {
				t.Fatalf("stream[%d] identity = {%q, %q, %q}, want {%q, %q, %q}",
					i, stream.Kind, stream.Key, stream.ActorID, wantKind, "tm-history", "owner")
			}
			got, err := stream.Snapshot(d.rdb)
			if err != nil {
				t.Fatalf("stream[%d] snapshot: %v", i, err)
			}
			if got != wantSnapshots[i] {
				t.Fatalf("stream[%d] snapshot = %q, want %q", i, got, wantSnapshots[i])
			}
		}
	})
}

func TestRoleDefHistoryStreams(t *testing.T) {
	t.Run("a rename with unchanged definition retains no stream", func(t *testing.T) {
		got := roleDefHistoryStreams("r-design", "owner", false)
		if got != nil {
			t.Fatalf("streams = %#v, want nil", got)
		}
	})

	t.Run("a definition change retains one addressed role stream", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		got := roleDefHistoryStreams("r-design", "agent-kip", true)
		if len(got) != 1 {
			t.Fatalf("stream count = %d, want 1", len(got))
		}
		stream := got[0]
		if stream.Kind != "role_definition" || stream.Key != "r-design" || stream.ActorID != "agent-kip" {
			t.Fatalf("stream identity = {%q, %q, %q}, want {%q, %q, %q}",
				stream.Kind, stream.Key, stream.ActorID, "role_definition", "r-design", "agent-kip")
		}
		snapshot, err := stream.Snapshot(d.rdb)
		if err != nil {
			t.Fatalf("stream snapshot: %v", err)
		}
		if snapshot != `{"definition_md":"# Duty","tombstoned":"false"}` {
			t.Fatalf("stream snapshot = %q, want %q", snapshot, `{"definition_md":"# Duty","tombstoned":"false"}`)
		}
	})
}

func TestDocumentHistoryAllowed(t *testing.T) {
	t.Run("a supported read is allowed without writing a response", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		req := httptest.NewRequest("GET", "/api/document-history/global_context/global", nil)
		rec := httptest.NewRecorder()

		if !api.documentHistoryAllowed(rec, req, "global_context", "global", false) {
			t.Fatal("documentHistoryAllowed refused a supported read")
		}
		if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
			t.Fatalf("allowed read response = %d %q, want empty 200 response", rec.Code, rec.Body.String())
		}
	})

	for name, tc := range map[string]struct {
		kind    string
		key     string
		message string
	}{
		"unknown kind": {
			kind: "bogus", key: "global", message: "unknown document history kind",
		},
		"retired task manual kind": {
			kind: docKindTaskManual, key: "tm-history", message: legacyTaskManualKindMsg,
		},
		"retired lessons kind": {
			kind: "lessons", key: "r-design", message: legacyMemoryKindsMsg,
		},
		"retired task manual learnings kind": {
			kind: "task_manual_learnings", key: "tm-history", message: legacyMemoryKindsMsg,
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Both faces, because they fail differently when a kind is put
			// back: list would answer an empty 200 and restore would write.
			for _, write := range []bool{false, true} {
				api, _, _, _ := newAPITestServer(t)
				req := httptest.NewRequest("GET", "/api/document-history/"+tc.kind+"/"+tc.key, nil)
				rec := httptest.NewRecorder()

				if api.documentHistoryAllowed(rec, req, tc.kind, tc.key, write) {
					t.Fatalf("documentHistoryAllowed allowed an invalid document address (write=%v)", write)
				}
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400 (write=%v, %s)", rec.Code, write, rec.Body.String())
				}
				var body map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("non-JSON error: %s", rec.Body.String())
				}
				apiWantError(t, body, "validation_error", tc.message)
			}
		})
	}

	t.Run("a plain agent cannot restore a governance document", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		taskTestUnderCaller(t, api, d, agent, func(r *http.Request) {
			rec := httptest.NewRecorder()
			if api.documentHistoryAllowed(rec, r, "global_context", "global", true) {
				t.Fatal("documentHistoryAllowed allowed a non-admin restore")
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("non-JSON error: %s", rec.Body.String())
			}
			apiWantError(t, body, "forbidden", "restoring this document requires admin capability")
		})
	})
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
			`document history kind "task_manual" was retired: use "task_manual_sop"`)
	})

	for name, kind := range map[string]string{
		"lessons":               "lessons",
		"task manual learnings": "task_manual_learnings",
	} {
		t.Run("the retired "+name+" kind answers 400 saying the documents were dropped", func(t *testing.T) {
			_, h, _, owner := newAPITestServer(t)

			status, data := apiJSON(t, h, "GET", "/api/document-history/"+kind+"/r-design", owner, "")
			if status != 400 {
				t.Fatalf("want 400, got %d (%v)", status, data)
			}
			apiWantError(t, data, "validation_error",
				`document history kinds "lessons" and "task_manual_learnings" were retired: `+
					`the legacy memory documents and their retained revisions were dropped `+
					`from the database, so there is nothing left to list or restore`)
		})
	}

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
	t.Run("the user-custom document has an empty tombstone seed", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		got, hasSeed, err := api.documentSeedContent("global_context", "global")
		if err != nil {
			t.Fatalf("documentSeedContent: %v", err)
		}
		want := map[string]string{"text": "", "tombstoned": "true"}
		if !reflect.DeepEqual(got, want) || !hasSeed {
			t.Fatalf("documentSeedContent = %#v, %v; want %#v, true", got, hasSeed, want)
		}
	})

	t.Run("the shipped assistant role returns its definition field and tombstone", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		got, hasSeed, err := api.documentSeedContent("role_definition", "assistant")
		if err != nil {
			t.Fatalf("documentSeedContent: %v", err)
		}
		want := map[string]string{"definition_md": apiTestAssistantSeedDefinitionMD, "tombstoned": "true"}
		if !reflect.DeepEqual(got, want) || !hasSeed {
			t.Fatalf("documentSeedContent = %#v, %v; want %#v, true", got, hasSeed, want)
		}
	})

	t.Run("a custom role has no shipped seed", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		got, hasSeed, err := api.documentSeedContent("role_definition", "r-design")
		if err != nil {
			t.Fatalf("documentSeedContent: %v", err)
		}
		if got != nil || hasSeed {
			t.Fatalf("documentSeedContent = %#v, %v; want nil, false", got, hasSeed)
		}
	})

	t.Run("a boot document returns its text field and tombstone", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		got, hasSeed, err := api.documentSeedContent("offboard", "global")
		if err != nil {
			t.Fatalf("documentSeedContent: %v", err)
		}
		want := map[string]string{"text": apiTestOffboardNotice, "tombstoned": "true"}
		if !reflect.DeepEqual(got, want) || !hasSeed {
			t.Fatalf("documentSeedContent = %#v, %v; want %#v, true", got, hasSeed, want)
		}
	})

	t.Run("a seeded insight returns its text field and tombstone", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		got, hasSeed, err := api.documentSeedContent("insight", "assistant")
		if err != nil {
			t.Fatalf("documentSeedContent: %v", err)
		}
		wantText := "# 接案窗口\n\n接到請求時，先釐清處理方式、任務安排與任務類型。需要 Owner 裁定時，開卡向 Owner 確認以下事項，並在同一張卡一次帶齊：\n\n1. **處理方式與執行者**\n\n   **由特助維持統一窗口**\n   若選擇由特助維持統一窗口，特助作為單一對口，負責接收與整理需求、將需求交給執行者、追蹤處理進度並回傳結果。特助向執行者說明，本案的對接窗口是特助，不是 Owner（負責人）；執行者將進度與問題回報特助，需要 Owner 裁定時由特助整理後開卡確認，執行者不直接向 Owner 溝通；請求方持續向特助對接。\n\n   **直接轉交給執行者**\n   若選擇直接轉交，窗口向指定的執行者明確交代需求範圍與下一步後退出處理鏈；後續由執行者與請求方直接對接並處理。\n\n2. **建立任務與任務類型**\n   若要建立任務，卡上至少提供：\n   - **問題與預期結果：** 要解決的問題、影響程度與發生可能性，以及問題解決後預期的情境。\n   - **預計解法與範圍：** 預計如何解決、這次要處理的工作範圍，以及可能遺留的問題或新產生的風險。\n   - **任務類型：** 符合的候選任務類型及其適用範圍，供請求方選擇；若沒有合適類型，標明差距並交由 Owner 確認，不自行套用相近類型。\n   - **任務優先權：** 根據問題的影響程度、發生可能性與其他時程因素，提出建議的處理優先權。\n   - **執行安排：** 預期執行者、執行機器、runtime／model、effort。\n\n待 Owner 裁定的項目直接標明，交由 Owner 確認。\n\n# 操作導覽窗口\n\n## 取得並回答使用說明\n\n- 先按問題找來源：功能、規則與設定查控制台說明文件；MCP 操作看當前工具的說明與 schema；CLI 指令先看 `ocagent <子命令> --help`；任務流程查任務手冊。\n- 讀完後再回答，必要時對照目前系統的實際資料；說清楚適用條件、操作路徑與目前有效性。\n- 找不到說明，或說明與實況不一致時，明確說出不確定與差異，不自行補出規則、欄位或權限。\n\n## 代為執行受限操作\n\n- 先確認操作目標、對象、範圍、理由與完成條件。\n- 特助的權限比一般成員大；Owner 交辦的 OffiCraft 操作也包含在代為執行範圍內。請求明確、責任清楚且在權限內時，代為執行並回報實際結果。\n- 需要 Owner 決定、核可或授權時，整理必要資訊後開一張卡交 Owner 裁定，不代替 Owner 做決定；內容不清楚或超出權限範圍時，先補齊資訊或確認。\n"
		want := map[string]string{"text": wantText, "tombstoned": "true"}
		if !reflect.DeepEqual(got, want) || !hasSeed {
			t.Fatalf("documentSeedContent = %#v, %v; want %#v, true", got, hasSeed, want)
		}
	})

	t.Run("an unknown kind has no seed and no error", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		got, hasSeed, err := api.documentSeedContent("bogus", "global")
		if got != nil || hasSeed || err != nil {
			t.Fatalf("documentSeedContent = %#v, %v, %v; want nil, false, nil", got, hasSeed, err)
		}
	})
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

	// EVERY event procedure, not one representative: the restore's write arm and
	// its fan arm are two case lists of literal kinds, and a kind missing from
	// either is silent — the restore answers 200 and the DB is changed, while a
	// missing WRITE arm stores nothing and a missing FAN arm leaves every open
	// screen showing the old text.
	for _, kind := range eventProcKinds() {
		t.Run("restoring "+kind+" writes the revision back and fans the boot-doc delta", func(t *testing.T) {
			api, h, _, owner := newAPITestServer(t)
			path := "/api/boot-docs/" + kind + "/" + bootDocSingletonKey
			apiJSON(t, h, "POST", path, owner, `{"body":"v1 body"}`)
			apiJSON(t, h, "POST", path, owner, `{"body":"v2 body"}`)
			dashboard := apiTestListen(t, api, "")

			status, data := apiJSON(t, h, "POST",
				"/api/document-history/"+kind+"/"+bootDocSingletonKey+"/1/restore", owner, "")
			if status != 200 {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}

			read, doc := apiJSON(t, h, "GET", path, owner, "")
			if read != 200 {
				t.Fatalf("read back: want 200, got %d (%v)", read, doc)
			}
			if body, _ := doc["body"].(string); !strings.Contains(body, "v1 body") {
				t.Fatalf("the restore was accepted but the document still reads %q", body)
			}
			dashboard.wantFrames(map[string]any{
				"seq":   apiAnyNumber,
				"topic": "global_context",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "global_context",
					"key":     "owner",
					"epoch":   apiAnyNumber,
					"deleted": false,
					"payload": nil,
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			})
		})
	}

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
			`document history kind "task_manual" was retired: use "task_manual_sop"`)
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
	for name, tc := range map[string]struct {
		kind             string
		key              string
		task             bool
		seq              int
		topic            string
		entity           string
		wireKey          string
		payload          any
		executorReceives bool
	}{
		"global context":   {kind: "global_context", key: "global", seq: 1, topic: "global_context", entity: "global_context", wireKey: "owner", payload: nil},
		"role definition":  {kind: "role_definition", key: "r-design", seq: 1, topic: "role_def", entity: "role_def", wireKey: "owner::r-design", payload: nil},
		"insight":          {kind: "insight", key: "assistant", seq: 1, topic: "insight", entity: "insight", wireKey: "owner::assistant", payload: nil},
		"boot document":    {kind: "offboard", key: "global", seq: 1, topic: "global_context", entity: "global_context", wireKey: "owner", payload: nil},
		"task manual SOP":  {kind: docKindTaskManualSop, key: "tm-history", seq: 1, topic: "task_manual", entity: "task_manual", wireKey: "owner::tm-history", payload: nil},
		"task description": {kind: docKindTaskDescription, key: "T-1", task: true, seq: 2, topic: "task", entity: "task", wireKey: "owner::T-1", payload: map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"}, executorReceives: true},
		"task title":       {kind: docKindTaskTitle, key: "T-1", task: true, seq: 2, topic: "task", entity: "task", wireKey: "owner::T-1", payload: map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"}, executorReceives: true},
	} {
		t.Run(name, func(t *testing.T) {
			api, h, d, owner := newAPITestServer(t)
			if tc.task {
				status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
					`{"title":"Ship it","executor_member_id":"kip","description":"desc"}`)
				if status != 200 {
					t.Fatalf("create task: %d %v", status, data)
				}
			}
			var req *http.Request
			taskTestUnderCaller(t, api, d, owner, func(r *http.Request) { req = r })
			dashboard := apiTestListen(t, api, "")
			executor := apiTestListen(t, api, "kip")

			api.publishDocumentHistoryRestore(req, tc.kind, tc.key)
			want := map[string]any{
				"seq": tc.seq, "topic": tc.topic, "op": "patch",
				"data": map[string]any{
					"entity": tc.entity, "key": tc.wireKey, "epoch": tc.seq,
					"deleted": false, "payload": tc.payload,
				},
				"ts": apiAnyNumber, "trigger": "owner",
			}
			dashboard.wantFrames(want)
			if tc.executorReceives {
				executor.wantFrames(want)
			} else {
				executor.wantFrames()
			}
		})
	}
}

func TestTaskDescriptionRestoreAuthz(t *testing.T) {
	t.Run("the task executor may restore its task description and receives the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/description", agent, `{"description":"new scope"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/document-history/task_description/T-1/1/restore", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": 1, "created_ts": apiAnyNumber, "actor_id": "kip",
			"content": map[string]any{"description": "old scope"},
		})
		taskFrame := map[string]any{
			"seq": 3, "topic": "task", "op": "patch",
			"data": map[string]any{
				"entity": "task", "key": "owner::T-1", "epoch": 3,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts": apiAnyNumber, "trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "old scope")
		rec := apiRequest(t, h, "GET", "/api/document-history/task_description/T-1", owner, "")
		var history []any
		if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
			t.Fatalf("non-JSON history: %s", rec.Body.String())
		}
		apiWantValue(t, "history", any(history), []any{
			map[string]any{
				"id": 2, "created_ts": apiAnyNumber, "actor_id": "kip",
				"tombstoned": false, "field_chars": map[string]any{"description": 9},
			},
			map[string]any{
				"id": 1, "created_ts": apiAnyNumber, "actor_id": "kip",
				"tombstoned": false, "field_chars": map[string]any{"description": 9},
			},
		})
	})

	t.Run("a plain agent who is not the task executor cannot restore its description", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"mira","description":"old scope"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/description", owner, `{"description":"new scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/document-history/task_description/T-1/1/restore", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", executorGuardRefusal)
		dashboard.wantFrames()
		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "new scope")
		rec := apiRequest(t, h, "GET", "/api/document-history/task_description/T-1", owner, "")
		var history []any
		if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
			t.Fatalf("non-JSON history: %s", rec.Body.String())
		}
		apiWantValue(t, "history", any(history), []any{map[string]any{
			"id": 1, "created_ts": apiAnyNumber, "actor_id": "owner",
			"tombstoned": false, "field_chars": map[string]any{"description": 9},
		}})
	})
}

func TestRestoreDocumentHistory(t *testing.T) {
	t.Run("restoring an offboard revision stores the old body under the shipped head and fans a boot delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"old notice"}`)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"new notice"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/document-history/offboard/global/1/restore", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": 1, "created_ts": apiAnyNumber, "actor_id": "owner",
			"content": map[string]any{"text": "old notice", "tombstoned": "false"},
		})
		dashboard.wantFrames(map[string]any{
			"seq": 3, "topic": "global_context", "op": "patch",
			"data": map[string]any{
				"entity": "global_context", "key": "owner", "epoch": 3,
				"deleted": false, "payload": nil,
			},
			"ts": apiAnyNumber, "trigger": "owner",
		})
		bystander.wantFrames()
		status, data = apiJSON(t, h, "GET", "/api/offboard", owner, "")
		if status != 200 {
			t.Fatalf("read offboard: %d %v", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind": "offboard", "key": "global", "owner_id": "owner",
			"text": "old notice", "body": "old notice", "size_chars": 10,
			"cap_chars": 15000, "has_seed": true, "is_default": false,
			"read_only": false, "read_only_head": "", "schema_version": 3,
		})
	})
}

func TestRestoreTaskManualField(t *testing.T) {
	t.Run("restoring the SOP revision changes only the SOP and fans the manual delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiTestCreateTaskManual(t, h, agent, `{"type_key":"tm-history","display_name":"歷史"}`)
		apiJSON(t, h, "POST", "/api/task-manuals/tm-history", agent,
			`{"purpose":"原目的","fields":[{"name":"客戶","required":true,"is_key":true}],"sop_md":"SOP 舊版"}`)
		apiJSON(t, h, "POST", "/api/task-manuals/tm-history", agent,
			`{"purpose":"新目的","sop_md":"SOP 新版"}`)
		dashboard := apiTestListen(t, api, "")
		agentListener := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/document-history/task_manual_sop/tm-history/1/restore", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": 1, "created_ts": apiAnyNumber, "actor_id": "kip",
			"content": map[string]any{"sop_md": "SOP 舊版"},
		})
		dashboard.wantFrames(apiTestTaskManualFrame(4, "tm-history", "kip"))
		agentListener.wantFrames()
		status, data = apiJSON(t, h, "GET", "/api/task-manuals/tm-history", owner, "")
		if status != 200 {
			t.Fatalf("read task manual: %d %v", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key": "tm-history", "display_name": "歷史", "purpose": "新目的",
			"fields": []any{map[string]any{"name": "客戶", "required": true, "is_key": true}},
			"sop_md": "SOP 舊版", "assignee": map[string]any{},
			"lore": "", "lore_chars": 0,
			"sop_md_chars": 6, "sop_md_cap_chars": 15000,
			"updated_ts": apiAnyNumber,
		})
	})
}
