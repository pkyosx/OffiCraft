package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
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
		"a tombstoned row": {current: &UserContext{Text: "全域規則", Tombstoned: true}, want: `{}`},
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
		"a definition keeps text but not the current display name": {
			current: &RoleDef{RoleKey: "r-design", Name: "目前名稱", DefinitionMD: "# 職責"},
			want:    `{"definition_md":"# 職責","tombstoned":"false"}`,
		},
		"a tombstoned row": {
			current: &RoleDef{RoleKey: "r-design", Name: "目前名稱", DefinitionMD: "# 職責", Tombstoned: true},
			want:    `{}`,
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
			RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty",
		}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		got, err := roleDefSnapshotIn("r-design")(d.rdb)
		if err != nil {
			t.Fatalf("roleDefSnapshotIn: %v", err)
		}
		if got != `{"definition_md":"# Duty","tombstoned":"false"}` {
			t.Fatalf("snapshot = %q, want %q", got, `{"definition_md":"# Duty","tombstoned":"false"}`)
		}
	})

	t.Run("the transaction reader represents a reset role definition as the empty object", func(t *testing.T) {
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
		if got != `{}` {
			t.Fatalf("snapshot = %q, want %q", got, `{}`)
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
	t.Run("a write, a reset and another write list only the revision that carried text", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/global-context/reset", owner, "")
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v2"}`)

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
	})

	t.Run("a tombstone revision retained by an older server is not listed", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		seedLegacyTombstoneRevision(t, d)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v2"}`)

		rec := apiRequest(t, h, "GET", "/api/document-history/global_context/global", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{map[string]any{
			"id":          2,
			"created_ts":  apiAnyNumber,
			"actor_id":    "owner",
			"tombstoned":  false,
			"field_chars": map[string]any{"text": 2},
		}})
	})

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
	t.Run("a tombstone revision retained by an older server answers 404 like a pruned one", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		seedLegacyTombstoneRevision(t, d)

		status, data := apiJSON(t, h, "GET", "/api/document-history/global_context/global/1", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "document history version not found")
	})

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
		wantText := "# Intake Point of Contact\n\nWhen you receive a request, first clarify how it will be handled, how the task will be arranged, and the task type. When an Owner ruling is needed, open a card to confirm the following with the Owner, and bring all of it on the same card at once:\n\n1. **Handling approach and executor**\n\n   **The Assistant stays the single point of contact**\n   If the Assistant is chosen to stay the single point of contact, the Assistant acts as the sole contact: it receives and organizes the request, hands it to the executor, tracks progress, and relays the results back. The Assistant tells the executor that the point of contact for this matter is the Assistant, not the Owner (the person in charge); the executor reports progress and problems to the Assistant, and when an Owner ruling is needed the Assistant organizes it and opens a card to confirm — the executor does not communicate with the Owner directly; the requester keeps dealing with the Assistant.\n\n   **Hand off directly to the executor**\n   If a direct handoff is chosen, the point of contact clearly explains the scope of the request and the next step to the designated executor, then leaves the handling chain; from then on the executor and the requester deal with each other directly and handle it.\n\n2. **Creating the task and the task type**\n   If a task is to be created, the card must provide at least:\n   - **Problem and expected outcome:** the problem to solve, its impact and likelihood, and the expected situation once it is solved.\n   - **Planned solution and scope:** how it is expected to be solved, the scope of work this time, and any problems that may remain or new risks that may arise.\n   - **Task type:** the candidate task types that fit and their scope of application, for the requester to choose from; if no type fits, state the gap and leave it to the Owner to confirm — do not apply a similar type on your own.\n   - **Task priority:** a suggested priority based on the problem's impact, likelihood, and other scheduling factors.\n   - **Execution arrangement:** expected executor, execution machine, runtime / model, effort.\n\nMark the items awaiting an Owner ruling explicitly and leave them to the Owner to confirm.\n\nWhen remote access needs to be opened (the user wants to reach the console from somewhere other than this computer), create a task with the built-in task manual \"Open remote access\" (`builtin-remote-access`) and execute it following the manual's steps.\n\n# Operations Guide Point of Contact\n\n## Finding and answering usage questions\n\n- First find the source that matches the question: for features, rules, and settings, check the console's user guide; for MCP operations, read the current tool's description and schema; for CLI commands, start with `ocagent <subcommand> --help`; for task workflows, check the task manual.\n- Answer only after reading, and check against the system's actual current data when needed; state clearly the conditions that apply, the path to perform the operation, and whether it is currently in effect.\n- When you cannot find documentation, or the documentation does not match reality, say plainly what is uncertain and what differs; do not make up rules, fields, or permissions.\n\n## Performing restricted operations on someone's behalf\n\n- First confirm the operation's goal, target, scope, reason, and completion condition.\n- The Assistant's permissions are broader than a regular member's; OffiCraft operations the Owner delegates are also within the scope of what it performs on others' behalf. When the request is clear, the responsibility is clear, and it is within permissions, perform it on their behalf and report the actual result.\n- When an Owner decision, approval, or authorization is needed, organize the necessary information and open a card for the Owner to rule on — do not make the decision in the Owner's place; when the content is unclear or beyond your permissions, first fill in the information or confirm.\n"
		want := map[string]string{"text": wantText, "tombstoned": "true"}
		if !reflect.DeepEqual(got, want) || !hasSeed {
			t.Fatalf("documentSeedContent = %#v, %v; want %#v, true", got, hasSeed, want)
		}
	})

	t.Run("a built-in task manual returns its shipped SOP under the SOP field and tombstone", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		for _, tc := range []struct {
			typeKey string
			runes   int
			sha256  string
		}{
			{"builtin-role-design", 9219, "58d517c3d59ff7fb36c91d5b0b09c70a2be2257055fe65cd2c5479583d84595e"},
			{"builtin-task-manual-design", 12335, "12a3bbc83db1c7b2171b27a91dfd9b51dd60fdbce89b963bf5caefa3274328e9"},
			{"builtin-remote-access", 14974, "515b1a9d02e4c90282e973e525b27870456dd62d46c968c03cb0c953d1353e4d"},
		} {
			got, hasSeed, err := api.documentSeedContent(docKindTaskManualSop, tc.typeKey)
			if err != nil || !hasSeed {
				t.Fatalf("%s: documentSeedContent = %v, %v", tc.typeKey, hasSeed, err)
			}
			if len(got) != 2 || got["tombstoned"] != "true" {
				t.Fatalf("%s: seed content keys = %#v, want exactly sop_md and tombstoned=true", tc.typeKey, got)
			}
			if n := utf8.RuneCountInString(got["sop_md"]); n != tc.runes {
				t.Fatalf("%s: seed sop_md runes = %d, want %d", tc.typeKey, n, tc.runes)
			}
			if sum := receiptSha256(got["sop_md"]); sum != tc.sha256 {
				t.Fatalf("%s: seed sop_md sha256 = %s, want %s", tc.typeKey, sum, tc.sha256)
			}
		}
	})

	t.Run("a task manual created on the station has no shipped seed", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.PutTaskManual(TaskManual{TypeKey: "tm-quote", Fields: "[]", Assignee: "{}", SopMD: "# SOP"}); err != nil {
			t.Fatalf("PutTaskManual: %v", err)
		}
		got, hasSeed, err := api.documentSeedContent(docKindTaskManualSop, "tm-quote")
		if got != nil || hasSeed || err != nil {
			t.Fatalf("documentSeedContent = %#v, %v, %v; want nil, false, nil", got, hasSeed, err)
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
	t.Run("restoring the revision a reset retained writes its text back as an edit, not as the default", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"v1"}`)
		apiJSON(t, h, "POST", "/api/global-context/reset", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/document-history/global_context/global/1/restore", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		status, data = apiJSON(t, h, "GET", "/api/global-context", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"text":           "v1",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"org_name":       "",
		})
	})

	t.Run("restoring a tombstone revision retained by an older server answers 404 and leaves the document alone", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		seedLegacyTombstoneRevision(t, d)
		apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"live"}`)

		status, data := apiJSON(t, h, "POST", "/api/document-history/global_context/global/1/restore", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "document history version not found")
		status, data = apiJSON(t, h, "GET", "/api/global-context", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"text":           "live",
			"owner_id":       "owner",
			"schema_version": 3,
			"is_default":     false,
			"org_name":       "",
		})
	})

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
		apiWantError(t, data, "forbidden", taskActorRefusal)
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
			"updated_ts": apiAnyNumber, "is_seed": false, "is_default": false,
		})
	})

	t.Run("restoring a built-in manual's SOP revision after a reset puts that SOP over the shipped manual", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/task-manuals/builtin-role-design", owner, `{"sop_md":"第一次改寫"}`)
		apiJSON(t, h, "POST", "/api/task-manuals/builtin-role-design", owner, `{"sop_md":"第二次改寫"}`)
		apiJSON(t, h, "POST", "/api/task-manuals/builtin-role-design/reset", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/document-history/task_manual_sop/builtin-role-design/1/restore", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": 1, "created_ts": apiAnyNumber, "actor_id": "owner",
			"content": map[string]any{"sop_md": "第一次改寫"},
		})
		status, data = apiJSON(t, h, "GET", "/api/task-manuals/builtin-role-design", owner, "")
		if status != 200 {
			t.Fatalf("read task manual: %d %v", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"type_key": "builtin-role-design", "display_name": "建立／修改角色",
			"purpose": "建立新的角色，或調整既有角色的角色定義與判準（Insight）。",
			"fields":  []any{},
			"sop_md":  "第一次改寫", "assignee": map[string]any{"kind": "staff", "member_id": "mira"},
			"lore": "", "lore_chars": 0,
			"sop_md_chars": 5, "sop_md_cap_chars": 15000,
			"updated_ts": apiAnyNumber, "is_seed": true, "is_default": false,
		})
	})
}

// seedLegacyTombstoneRevision writes a tombstone revision row, which every
// history read must hide while it still counts toward the retention slots.
func seedLegacyTombstoneRevision(t *testing.T, d *DAL) {
	t.Helper()
	if _, err := d.wdb.Exec(`INSERT INTO document_history
		(document_kind, document_key, content_json, created_ts, actor_id)
		VALUES (?, ?, ?, ?, ?)`,
		"global_context", "global", `{"text":"","tombstoned":"true"}`, 1700000001.0, "owner"); err != nil {
		t.Fatalf("seed a legacy tombstone revision: %v", err)
	}
}
