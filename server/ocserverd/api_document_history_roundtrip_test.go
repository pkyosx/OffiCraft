package main

// Round trips through the REAL handlers for the document families the
// global-context test does not reach: a role definition, a lessons doc (whose
// history key is the role::task_type pair), and a task manual — whose SOP and
// learnings are two INDEPENDENT version series keyed by the same type_key
// (T-1f39), so every manual case here asserts on both by name.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type historyFixture struct {
	t   *testing.T
	api *apiServer
}

func newHistoryFixture(t *testing.T) historyFixture {
	t.Helper()
	return historyFixture{t: t, api: newTasksTestServer(t)}
}

func (f historyFixture) req(method, path string, body any) *http.Request {
	return taskReq(f.t, method, path, body, "owner", "owner")
}

func (f historyFixture) list(kind, key string) []historyRow {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
		f.req(http.MethodGet, "/api/document-history/"+kind+"/"+key, nil), kind, key)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("list %s/%s: status=%d body=%s", kind, key, rec.Code, rec.Body.String())
	}
	var rows []DocumentHistoryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		f.t.Fatal(err)
	}
	return hydrateHistory(f.t, f.api, kind, key, "owner", "owner", rows)
}

func (f historyFixture) restore(kind, key string, id int64) {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
		f.req(http.MethodPost, "/api/document-history/"+kind+"/"+key+"/"+"restore", nil), kind, key, id)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("restore %s/%s: status=%d body=%s", kind, key, rec.Code, rec.Body.String())
	}
}

func TestDocumentHistoryRoundTripsARoleDefinition(t *testing.T) {
	f := newHistoryFixture(t)
	role := seedRoleAssistant
	for _, definition := range []string{"first", "second", "third"} {
		rec := httptest.NewRecorder()
		f.api.HandleUpdateRoleApiRolesRolePost(rec,
			f.req(http.MethodPost, "/api/roles/"+role, map[string]any{"definition_md": definition}), role)
		if rec.Code != http.StatusOK {
			t.Fatalf("update role %q: status=%d body=%s", definition, rec.Code, rec.Body.String())
		}
	}

	// Two, not three: the first write customized a seed role that had no
	// overlay row yet, and "no overlay" is the default state rather than a
	// version of the document.
	history := f.list("role_definition", role)
	if len(history) != 2 {
		t.Fatalf("retained %d revisions, want 2: %+v", len(history), history)
	}
	if history[0].Content["definition_md"] != "second" {
		t.Fatalf("newest revision = %+v, want the replaced definition \"second\"", history[0].Content)
	}

	f.restore("role_definition", role, history[0].Id)
	current, err := f.api.foldRoleDefDTO(role)
	if err != nil || current == nil {
		t.Fatalf("fold role: %+v, %v", current, err)
	}
	if current.DefinitionMD != "second" {
		t.Fatalf("restored definition = %q, want second", current.DefinitionMD)
	}
	// The restore is itself a write, so the document it replaced is retained.
	after := f.list("role_definition", role)
	if after[0].Content["definition_md"] != "third" {
		t.Fatalf("restore did not retain the definition it replaced: %+v", after[0].Content)
	}
}

func TestDocumentHistoryRoundTripsLessonsUnderItsRoleKey(t *testing.T) {
	f := newHistoryFixture(t)
	role := seedRoleAssistant
	for _, text := range []string{"lesson one", "lesson two", "lesson three"} {
		rec := httptest.NewRecorder()
		f.api.HandleReplaceLessonsApiLessonsRoleKeyPost(rec,
			f.req(http.MethodPost, "/api/lessons/"+role, map[string]any{"text": text}),
			role)
		if rec.Code != http.StatusOK {
			t.Fatalf("replace lessons %q: status=%d body=%s", text, rec.Code, rec.Body.String())
		}
	}

	key := role
	history := f.list("lessons", key)
	if len(history) == 0 || history[0].Content["text"] != "lesson two" {
		t.Fatalf("retained lessons history = %+v, want the replaced \"lesson two\" newest", history)
	}

	f.restore("lessons", key, history[0].Id)
	current, err := f.api.foldLessonsDTO(role)
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "lesson two" {
		t.Fatalf("restored lessons = %q, want \"lesson two\"", current.Text)
	}

	// The BARE role_key is what addresses the document since T-2, and the old
	// composite is REFUSED rather than silently re-split back onto this role's
	// series. Two properties in one answer: nothing re-derives the role from
	// the composite (the axis is not back as a parsing rule), and the caller is
	// told so instead of being handed an empty list it would read as "this role
	// has no history". An earlier cut of T-2 answered 200-with-empty here; that
	// also let the RESTORE face through, which materialised a lessons row under
	// the composite key — see api_document_history_lessons_key_t2_test.go.
	rec := httptest.NewRecorder()
	legacy := role + "::general"
	f.api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
		f.req(http.MethodGet, "/api/document-history/lessons/"+legacy, nil), "lessons", legacy)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("list lessons history under the pre-T-2 composite key %q: status=%d body=%s, want 400",
			legacy, rec.Code, rec.Body.String())
	}
	if current, err := f.api.foldLessonsDTO(role); err != nil || current.Text != "lesson two" {
		t.Fatalf("the refused listing disturbed the role's live lessons: %+v, %v", current, err)
	}

	// A BLANK key still addresses nothing and is still a 400.
	rec = httptest.NewRecorder()
	f.api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
		f.req(http.MethodGet, "/api/document-history/lessons/", nil), "lessons", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("list lessons history with a blank key: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// A role's lessons and its definition are TWO documents, so each keeps its own
// three retained versions: editing one must not consume — or reorder — a slot
// belonging to the other. The manual used to fail exactly this way (one bundle,
// four fields), and the cockpit now offers history on both faces side by side,
// so a regression here would silently eat the lessons a role spent months
// accumulating. Writing PAST the retention limit on one side is what makes the
// case bite: three writes cannot show a shared series, a full turnover can.
func TestRoleDefinitionAndLessonsKeepSeparateVersionSeries(t *testing.T) {
	f := newHistoryFixture(t)
	role := seedRoleAssistant
	lessonsKey := role

	writeLessons := func(text string) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandleReplaceLessonsApiLessonsRoleKeyPost(rec,
			f.req(http.MethodPost, "/api/lessons/"+role, map[string]any{"text": text}),
			role)
		if rec.Code != http.StatusOK {
			t.Fatalf("replace lessons %q: status=%d body=%s", text, rec.Code, rec.Body.String())
		}
	}
	writeDefinition := func(definition string) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandleUpdateRoleApiRolesRolePost(rec,
			f.req(http.MethodPost, "/api/roles/"+role, map[string]any{"definition_md": definition}), role)
		if rec.Code != http.StatusOK {
			t.Fatalf("update role %q: status=%d body=%s", definition, rec.Code, rec.Body.String())
		}
	}
	snapshot := func(kind, key string) []string {
		t.Helper()
		var seen []string
		for _, version := range f.list(kind, key) {
			field := "text"
			if kind == "role_definition" {
				field = "definition_md"
			}
			seen = append(seen, version.Content[field])
		}
		return seen
	}
	assertSame := func(what string, got, want []string) {
		t.Helper()
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("%s history = %v, want it untouched at %v — the two documents "+
				"are sharing one version series", what, got, want)
		}
	}

	for _, text := range []string{"lesson one", "lesson two", "lesson three"} {
		writeLessons(text)
	}
	lessonsBefore := snapshot("lessons", lessonsKey)
	if len(lessonsBefore) != 2 || lessonsBefore[0] != "lesson two" {
		t.Fatalf("lessons history = %v, want the two replaced texts newest-first", lessonsBefore)
	}

	// Four definition writes: more than the three retained slots, so a shared
	// series would have flushed every lessons revision out of the window.
	for _, definition := range []string{"def one", "def two", "def three", "def four"} {
		writeDefinition(definition)
	}
	assertSame("lessons", snapshot("lessons", lessonsKey), lessonsBefore)

	definitionBefore := snapshot("role_definition", role)
	if len(definitionBefore) != 3 || definitionBefore[0] != "def three" {
		t.Fatalf("role definition history = %v, want three retained revisions newest-first", definitionBefore)
	}

	for _, text := range []string{"lesson four", "lesson five", "lesson six", "lesson seven"} {
		writeLessons(text)
	}
	assertSame("role definition", snapshot("role_definition", role), definitionBefore)

	// A restore is itself a write, so it retains a version too — on its OWN
	// series only.
	definitionHistory := f.list("role_definition", role)
	f.restore("role_definition", role, definitionHistory[0].Id)
	assertSame("lessons", snapshot("lessons", lessonsKey),
		[]string{"lesson six", "lesson five", "lesson four"})
}

// ── the task manual's two independent streams (T-1f39) ───────────────────────
//
// A manual used to keep ONE four-field bundle, so editing the purpose burned a
// version slot for the SOP and the learnings alike. Owner ruling: purpose and
// the identifier fields are not versioned at all, and SOP and learnings are
// versioned SEPARATELY. Every case below therefore asserts on BOTH streams by
// name — a change that folds them back into one bundle shows up as the other
// stream moving, which counting one stream alone cannot see.

type manualFixture struct {
	historyFixture
	typeKey string
}

func newManualFixture(t *testing.T) manualFixture {
	t.Helper()
	f := newHistoryFixture(t)
	rec := httptest.NewRecorder()
	f.api.HandleCreateTaskManualApiTaskManualsPost(rec,
		f.req(http.MethodPost, "/api/task-manuals", map[string]any{"display_name": "History"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("create manual: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created taskManualDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	return manualFixture{historyFixture: f, typeKey: created.TypeKey}
}

func (m manualFixture) update(body map[string]any) {
	m.t.Helper()
	rec := httptest.NewRecorder()
	m.api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec,
		m.req(http.MethodPost, "/api/task-manuals/"+m.typeKey, body), m.typeKey)
	if rec.Code != http.StatusOK {
		m.t.Fatalf("update manual %+v: status=%d body=%s", body, rec.Code, rec.Body.String())
	}
}

func (m manualFixture) writeLearnings(text string) {
	m.t.Helper()
	rec := httptest.NewRecorder()
	m.api.HandleWriteTaskLearningsApiTaskManualsTypeKeyLearningsPost(rec,
		m.req(http.MethodPost, "/api/task-manuals/"+m.typeKey+"/learnings",
			map[string]any{"text": text}), m.typeKey)
	if rec.Code != http.StatusOK {
		m.t.Fatalf("write_task_learnings %q: status=%d body=%s", text, rec.Code, rec.Body.String())
	}
}

func (m manualFixture) patchLearnings(old, replacement string) {
	m.t.Helper()
	rec := httptest.NewRecorder()
	m.api.HandlePatchTaskLearningsApiTaskManualsTypeKeyLearningsPatchPost(rec,
		m.req(http.MethodPost, "/api/task-manuals/"+m.typeKey+"/learnings/patch",
			map[string]any{"edits": []map[string]any{{"old": old, "new": replacement}}}), m.typeKey)
	if rec.Code != http.StatusOK {
		m.t.Fatalf("patch_task_learnings %q: status=%d body=%s", old, rec.Code, rec.Body.String())
	}
}

func (m manualFixture) versions(kind string) []historyRow {
	m.t.Helper()
	return m.list(kind, m.typeKey)
}

// mark is the whole shape of a stream that must not move: how many versions it
// holds and which one is newest. Counting alone misses a stream that gained one
// version and lost another to the retention trim in the same write.
func (m manualFixture) mark(kind string) (int, int64) {
	m.t.Helper()
	versions := m.versions(kind)
	if len(versions) == 0 {
		return 0, 0
	}
	return len(versions), versions[0].Id
}

func (m manualFixture) assertUnmoved(kind string, count int, newest int64) {
	m.t.Helper()
	if gotCount, gotNewest := m.mark(kind); gotCount != count || gotNewest != newest {
		m.t.Fatalf("%s stream moved: %d versions newest id %d, want %d versions newest id %d",
			kind, gotCount, gotNewest, count, newest)
	}
}

func (m manualFixture) manual() TaskManual {
	m.t.Helper()
	current, err := m.api.dal.GetTaskManual(m.typeKey)
	if err != nil || current == nil {
		m.t.Fatalf("read manual: %+v, %v", current, err)
	}
	return *current
}

func TestUpdateTaskManualVersionsSopAndLearningsSeparately(t *testing.T) {
	t.Run("an edit to the unversioned fields alone retains nothing anywhere", func(t *testing.T) {
		m := newManualFixture(t)
		m.update(map[string]any{"purpose": "purpose v1", "sop_md": "sop v1", "learnings": "learnings v1"})
		sopCount, sopNewest := m.mark(docKindTaskManualSop)
		learningsCount, learningsNewest := m.mark(docKindTaskManualLearnings)

		m.update(map[string]any{"purpose": "purpose v2"})
		m.update(map[string]any{"display_name": "Renamed"})
		m.update(map[string]any{"fields": []map[string]any{{"name": "order_no", "required": true, "is_key": true}}})

		m.assertUnmoved(docKindTaskManualSop, sopCount, sopNewest)
		m.assertUnmoved(docKindTaskManualLearnings, learningsCount, learningsNewest)
	})

	t.Run("an edit to the SOP alone leaves the learnings stream where it was", func(t *testing.T) {
		m := newManualFixture(t)
		m.update(map[string]any{"sop_md": "sop v1", "learnings": "learnings v1"})
		m.update(map[string]any{"learnings": "learnings v2"})
		learningsCount, learningsNewest := m.mark(docKindTaskManualLearnings)

		m.update(map[string]any{"sop_md": "sop v2"})

		sop := m.versions(docKindTaskManualSop)
		if len(sop) != 1 || sop[0].Content["sop_md"] != "sop v1" {
			t.Fatalf("sop stream = %+v, want one version holding the replaced \"sop v1\"", sop)
		}
		if _, carried := sop[0].Content["learnings"]; carried {
			t.Fatalf("the sop revision carries a learnings field: %+v", sop[0].Content)
		}
		m.assertUnmoved(docKindTaskManualLearnings, learningsCount, learningsNewest)
	})

	t.Run("an edit to the learnings alone leaves the sop stream where it was", func(t *testing.T) {
		m := newManualFixture(t)
		m.update(map[string]any{"sop_md": "sop v1", "learnings": "learnings v1"})
		m.update(map[string]any{"sop_md": "sop v2"})
		sopCount, sopNewest := m.mark(docKindTaskManualSop)

		m.update(map[string]any{"learnings": "learnings v2"})

		learnings := m.versions(docKindTaskManualLearnings)
		if len(learnings) != 1 || learnings[0].Content["learnings"] != "learnings v1" {
			t.Fatalf("learnings stream = %+v, want one version holding the replaced \"learnings v1\"", learnings)
		}
		if _, carried := learnings[0].Content["sop_md"]; carried {
			t.Fatalf("the learnings revision carries a sop_md field: %+v", learnings[0].Content)
		}
		m.assertUnmoved(docKindTaskManualSop, sopCount, sopNewest)
	})

	t.Run("one call changing both retains one version in each stream", func(t *testing.T) {
		m := newManualFixture(t)
		m.update(map[string]any{"sop_md": "sop v1", "learnings": "learnings v1"})

		m.update(map[string]any{"purpose": "purpose v2", "sop_md": "sop v2", "learnings": "learnings v2"})

		sop, learnings := m.versions(docKindTaskManualSop), m.versions(docKindTaskManualLearnings)
		if len(sop) != 1 || len(learnings) != 1 {
			t.Fatalf("streams = %d sop / %d learnings versions, want one each", len(sop), len(learnings))
		}
		if got := sop[0].Content; got["sop_md"] != "sop v1" || len(got) != 1 {
			t.Fatalf("sop revision = %+v, want only sop_md = \"sop v1\"", got)
		}
		if got := learnings[0].Content; got["learnings"] != "learnings v1" || len(got) != 1 {
			t.Fatalf("learnings revision = %+v, want only learnings = \"learnings v1\"", got)
		}
	})
}

func TestTaskLearningsWriteFacesVersionOnlyTheLearningsStream(t *testing.T) {
	for _, face := range []struct {
		name string
		run  func(manualFixture)
	}{
		{"write_task_learnings", func(m manualFixture) { m.writeLearnings("learnings v2") }},
		{"patch_task_learnings", func(m manualFixture) { m.patchLearnings("learnings v1", "learnings v2") }},
	} {
		t.Run(face.name, func(t *testing.T) {
			m := newManualFixture(t)
			m.update(map[string]any{"sop_md": "sop v1", "learnings": "learnings v1"})
			sopCount, sopNewest := m.mark(docKindTaskManualSop)

			face.run(m)

			learnings := m.versions(docKindTaskManualLearnings)
			if len(learnings) != 1 || learnings[0].Content["learnings"] != "learnings v1" {
				t.Fatalf("%s retained %+v, want the replaced \"learnings v1\"", face.name, learnings)
			}
			m.assertUnmoved(docKindTaskManualSop, sopCount, sopNewest)
			if m.manual().Learnings != "learnings v2" {
				t.Fatalf("%s did not write: learnings = %q", face.name, m.manual().Learnings)
			}
		})
	}
}

// Retention is three PER STREAM. Before the split the two fields shared one
// budget, so a busy SOP evicted the learnings history of the same manual.
func TestTaskManualStreamsAreTrimmedToThreeIndependently(t *testing.T) {
	m := newManualFixture(t)
	m.update(map[string]any{"learnings": "learnings v1"})
	m.update(map[string]any{"learnings": "learnings v2"})
	m.update(map[string]any{"learnings": "learnings v3"})
	learningsBefore := m.versions(docKindTaskManualLearnings)
	if len(learningsBefore) != 2 {
		t.Fatalf("learnings stream = %+v, want the two replaced versions", learningsBefore)
	}

	for _, sop := range []string{"sop v1", "sop v2", "sop v3", "sop v4", "sop v5"} {
		m.update(map[string]any{"sop_md": sop})
	}

	sop := m.versions(docKindTaskManualSop)
	// The depth is per-kind since T-791e; the manual series is one of the kinds
	// that deliberately stayed at the default, so this asks the table rather
	// than a literal.
	if want := documentHistoryKeepFor(docKindTaskManualSop); len(sop) != want {
		t.Fatalf("sop stream kept %d versions, want %d", len(sop), want)
	}
	if sop[0].Content["sop_md"] != "sop v4" || sop[2].Content["sop_md"] != "sop v2" {
		t.Fatalf("sop stream = %+v, want sop v4 down to sop v2 — the trim dropped the wrong end", sop)
	}
	m.assertUnmoved(docKindTaskManualLearnings, len(learningsBefore), learningsBefore[0].Id)
	if got := m.versions(docKindTaskManualLearnings)[0].Content["learnings"]; got != "learnings v2" {
		t.Fatalf("learnings stream newest = %q, want \"learnings v2\" — the sop trim reached it", got)
	}
}

func TestDocumentHistoryRestoresOneTaskManualStreamWithoutTouchingTheOther(t *testing.T) {
	// The manual stands at v2 in both fields; restoring one stream's only
	// version rolls THAT field back to v1 and leaves the other at v2.
	for _, tc := range []struct{ kind, wantSop, wantLearnings string }{
		{docKindTaskManualSop, "sop v1", "learnings v2"},
		{docKindTaskManualLearnings, "sop v2", "learnings v1"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			m := newManualFixture(t)
			m.update(map[string]any{"purpose": "purpose v1", "sop_md": "sop v1", "learnings": "learnings v1"})
			m.update(map[string]any{"sop_md": "sop v2", "learnings": "learnings v2"})
			other := docKindTaskManualLearnings
			if tc.kind == docKindTaskManualLearnings {
				other = docKindTaskManualSop
			}
			otherCount, otherNewest := m.mark(other)

			versions := m.versions(tc.kind)
			if len(versions) != 1 {
				t.Fatalf("%s stream = %+v, want one restorable version", tc.kind, versions)
			}
			m.restore(tc.kind, m.typeKey, versions[0].Id)

			restored := m.manual()
			if restored.SopMD != tc.wantSop || restored.Learnings != tc.wantLearnings {
				t.Fatalf("restoring %s left sop_md=%q learnings=%q, want %q and %q",
					tc.kind, restored.SopMD, restored.Learnings, tc.wantSop, tc.wantLearnings)
			}
			if restored.Purpose != "purpose v1" {
				t.Fatalf("restoring %s changed the unversioned purpose to %q", tc.kind, restored.Purpose)
			}
			m.assertUnmoved(other, otherCount, otherNewest)
		})
	}
}

// The retired four-field bundle. Migration 00045 deleted its rows (owner
// ruling, T-1f39), so both faces must refuse the old name LOUDLY and name the
// two replacements: an empty list or a 404 would read as "this manual has no
// history" and hide the fact that the caller is asking under a dead kind.
func TestDocumentHistoryRefusesTheRetiredTaskManualKind(t *testing.T) {
	m := newManualFixture(t)
	m.update(map[string]any{"sop_md": "sop v1", "learnings": "learnings v1"})
	m.update(map[string]any{"sop_md": "sop v2", "learnings": "learnings v2"})

	for _, face := range []struct {
		name string
		call func(*httptest.ResponseRecorder)
	}{
		{
			name: "list",
			call: func(rec *httptest.ResponseRecorder) {
				m.api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
					m.req(http.MethodGet, "/api/document-history/"+docKindTaskManual+"/"+m.typeKey, nil),
					docKindTaskManual, m.typeKey)
			},
		},
		{
			name: "restore",
			call: func(rec *httptest.ResponseRecorder) {
				m.api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
					m.req(http.MethodPost, "/api/document-history/"+docKindTaskManual+"/"+m.typeKey+"/restore", nil),
					docKindTaskManual, m.typeKey, 1)
			},
		},
	} {
		t.Run(face.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			face.call(rec)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s under the retired kind = %d %s, want 400", face.name, rec.Code, rec.Body.String())
			}
			for _, replacement := range []string{docKindTaskManualSop, docKindTaskManualLearnings} {
				if !strings.Contains(rec.Body.String(), replacement) {
					t.Errorf("the %s refusal does not name %q, so a caller on the old name "+
						"cannot tell which series it wanted: %s", face.name, replacement, rec.Body.String())
				}
			}
		})
	}

	// The positive control: the two replacements answer normally on the very
	// manual whose old-kind address was just refused.
	for _, kind := range []string{docKindTaskManualSop, docKindTaskManualLearnings} {
		versions := m.versions(kind)
		if len(versions) != 1 {
			t.Fatalf("%s stream = %+v, want the one replaced version", kind, versions)
		}
		m.restore(kind, m.typeKey, versions[0].Id)
	}
	if restored := m.manual(); restored.SopMD != "sop v1" || restored.Learnings != "learnings v1" {
		t.Fatalf("restored manual = %+v, want both replacement streams back at v1", restored)
	}
}

// The role's NAME is not part of the versioned document (owner ruling
// 2026-07-31 「名稱不用留版本」, after he opened a role-definition revision and
// asked why a 名稱 field was in it: 「角色名稱跟角色誌本身應該要是無關的，角色誌
// 本身不知道說明他自己的名字，只是說明他是做什麼的」).
//
// Two halves, and BOTH have to hold or the ruling is only half-applied:
//
//	① a pure RENAME retains nothing — otherwise renaming a role three times
//	   would push every real revision of the TEXT out of the three slots
//	   without a word of it having changed;
//	② a RESTORE leaves the current name standing — the reader came to put the
//	   text back, and a silent rename underneath that is exactly the surprise
//	   the de-versioning removed.
func TestRoleNameIsNotVersionedAndRestoreLeavesItAlone(t *testing.T) {
	f := newHistoryFixture(t)

	// A CUSTOM role: seed roles are name-locked, so a rename cannot even be
	// attempted on one and the fixture would prove nothing.
	rec := httptest.NewRecorder()
	f.api.HandleCreateRoleApiRolesPost(rec,
		f.req(http.MethodPost, "/api/roles", map[string]any{"name": "研究員"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("create role: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created roleCreateResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	role := created.RoleKey

	update := func(body map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandleUpdateRoleApiRolesRolePost(rec,
			f.req(http.MethodPost, "/api/roles/"+role, body), role)
		if rec.Code != http.StatusOK {
			t.Fatalf("update role %v: status=%d body=%s", body, rec.Code, rec.Body.String())
		}
	}

	update(map[string]any{"definition_md": "第一版"})
	update(map[string]any{"definition_md": "第二版"})
	before := f.list("role_definition", role)
	if len(before) == 0 || before[0].Content["definition_md"] != "第一版" {
		t.Fatalf("retained history = %+v, want the replaced 第一版 newest", before)
	}
	// ① The name is not IN a revision — not as a listed field and not as an
	// unknown one riding along in the same JSON.
	for _, v := range before {
		if _, ok := v.Content["name"]; ok {
			t.Fatalf("revision %d carries a name field %+v — the role's name is "+
				"not versioned, so a revision must not hold one", v.Id, v.Content)
		}
	}

	// ① Renaming three times is a full turnover of the retention window: if a
	// rename retained anything, nothing of 第一版/第二版 could survive it.
	for _, name := range []string{"研究員 A", "研究員 B", "研究員 C"} {
		update(map[string]any{"name": name})
	}
	afterRenames := f.list("role_definition", role)
	if len(afterRenames) != len(before) || afterRenames[0].Content["definition_md"] != "第一版" {
		t.Fatalf("history after three renames = %+v, want it untouched at %+v — "+
			"a rename must retain nothing", afterRenames, before)
	}

	// ② Restoring the text does not rename the role back.
	f.restore("role_definition", role, afterRenames[0].Id)
	current, err := f.api.foldRoleDefDTO(role)
	if err != nil || current == nil {
		t.Fatalf("fold role: %+v, %v", current, err)
	}
	if current.DefinitionMD != "第一版" {
		t.Fatalf("restored definition = %q, want 第一版", current.DefinitionMD)
	}
	if current.Name != "研究員 C" {
		t.Fatalf("restore changed the role name to %q, want the CURRENT 研究員 C — "+
			"restoring a revision must not rename the role", current.Name)
	}
}
