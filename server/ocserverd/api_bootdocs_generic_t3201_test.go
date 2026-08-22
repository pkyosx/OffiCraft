package main

// api_bootdocs_generic_t3201_test.go — the ONE route family that reaches every
// editable boot-context document (T-3201), instead of three named routes per
// document.
//
// WHAT IS WORTH TESTING HERE IS NOT "does it fold". The folding, the cap, the
// wipe guard, the read-only refusal and the variable gate all belong to
// replaceBootDoc / resetBootDoc / foldBootDocDTO, which the per-document faces
// have exercised since T-791e and which the event-procedure tests cover kind by
// kind. What is NEW is the ADDRESSING: a pair of path segments now chooses the
// document, so the failure mode this file exists for is a request that resolves
// to the WRONG document — or to none, silently.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func jsonPost(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func getBootDocHTTP(t *testing.T, s *apiServer, kind, key string) (int, bootDocDTO) {
	t.Helper()
	w := httptest.NewRecorder()
	s.HandleGetBootDocApiBootDocsKindKeyGet(w, httptest.NewRequest(http.MethodGet, "/x", nil), kind, key)
	var dto bootDocDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode %s/%s: %v", kind, key, err)
		}
	}
	return w.Code, dto
}

// ── the listing is the wire's only answer to "which documents exist" ─────────

// 🔴 THE COUNT IS DERIVED FROM THE REGISTRY, NOT WRITTEN DOWN. A number here
// would have to be edited by the same commit that adds a document, and a test
// that is edited alongside the thing it guards is not a guard. What this pins
// is that the listing walks EVERY key of EVERY row — the defect it replaces is
// a listing built from a second hand-maintained list, which is how a document
// ships and never appears in the cockpit.
func TestListBootDocs_CoversEveryRegistryKeyAndCarriesNoText(t *testing.T) {
	s := newEventProcServer(t)
	w := httptest.NewRecorder()
	s.HandleListBootDocsApiBootDocsGet(w, httptest.NewRequest(http.MethodGet, "/api/boot-docs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	want := map[string]bool{}
	for _, reg := range bootDocRegistry {
		for _, key := range reg.Keys {
			want[reg.Kind+"/"+key] = true
		}
	}
	var body bootDocListDTO
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, row := range body.Documents {
		got[row.Kind+"/"+row.Key] = true
		if row.DocName == "" {
			t.Errorf("%s/%s has no doc_name — a refusal will name it and this row will not",
				row.Kind, row.Key)
		}
		if row.CapChars <= 0 {
			t.Errorf("%s/%s reports cap %d", row.Kind, row.Key, row.CapChars)
		}
	}
	for addr := range want {
		if !got[addr] {
			t.Errorf("%s is in bootDocRegistry but not in the listing", addr)
		}
	}
	for addr := range got {
		if !want[addr] {
			t.Errorf("the listing serves %s, which is in no registry row", addr)
		}
	}

	// No text, and the assertion is on the RAW BYTES rather than the decoded
	// struct: a `text` field added to the row type would decode into a field
	// this test never reads, and the listing would silently start carrying
	// every document on a route the cockpit calls to draw a menu.
	if strings.Contains(w.Body.String(), `"text"`) {
		t.Errorf("the listing carries document text:\n%s", w.Body.String())
	}
}

// read_only is what tells a cockpit whether to render an editor at all. It has
// to be true for exactly the documents whose write faces refuse — a listing
// that got this wrong would offer an editor whose save is a 405, after the
// owner has already typed the edit.
func TestListBootDocs_ReadOnlyMatchesTheWriteFacesRefusal(t *testing.T) {
	s := newEventProcServer(t)
	w := httptest.NewRecorder()
	s.HandleListBootDocsApiBootDocsGet(w, httptest.NewRequest(http.MethodGet, "/api/boot-docs", nil))
	var body bootDocListDTO
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Documents) == 0 {
		t.Fatal("empty listing")
	}
	sawReadOnly := false
	for _, row := range body.Documents {
		rec := httptest.NewRecorder()
		s.HandleResetBootDocApiBootDocsKindKeyResetPost(rec, ownerPost("/x"), row.Kind, row.Key)
		refused := rec.Code == http.StatusMethodNotAllowed
		if refused != row.ReadOnly {
			t.Errorf("%s/%s: listing says read_only=%v, the reset face answers %d",
				row.Kind, row.Key, row.ReadOnly, rec.Code)
		}
		sawReadOnly = sawReadOnly || row.ReadOnly
	}
	// Positive control: without it every row answering read_only=false would
	// pass this test on a build where the refusal had been removed entirely.
	if !sawReadOnly {
		t.Error("no row reports read_only — the refusal this test measures is not reachable")
	}
}

// ── addressing: the pair chooses the document, or nothing at all ─────────────

func TestGetBootDoc_UnknownKindAndKeyAre404ThatNameTheKeys(t *testing.T) {
	s := newEventProcServer(t)

	code, _ := getBootDocHTTP(t, s, "no_such_kind", bootDocSingletonKey)
	if code != http.StatusNotFound {
		t.Errorf("unknown kind: status = %d, want 404", code)
	}

	// A REAL kind with a key it does not serve. This is the case a bare "not
	// found" leaves unresolvable: boot_sequence serves two keys and neither is
	// guessable from the kind, so the refusal has to name them.
	w := httptest.NewRecorder()
	s.HandleGetBootDocApiBootDocsKindKeyGet(w, httptest.NewRequest(http.MethodGet, "/x", nil),
		docKindBootSequence, "opus")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
	for _, key := range []string{bootSequenceKeyClaude, bootSequenceKeyCodex} {
		if !strings.Contains(w.Body.String(), key) {
			t.Errorf("the refusal does not name %q: %s", key, w.Body.String())
		}
	}
}

// The generic face and the named face are two doors onto ONE document. If they
// ever fold differently, the cockpit and an agent are reading different text
// while both believe they hold 系統互動.
func TestGetBootDoc_AgreesWithTheNamedRouteForTheSameDocument(t *testing.T) {
	s := newEventProcServer(t)

	named := httptest.NewRecorder()
	s.HandleGetSystemInteractionApiSystemInteractionGet(named,
		httptest.NewRequest(http.MethodGet, "/api/system-interaction", nil))
	if named.Code != http.StatusOK {
		t.Fatalf("named route: %d", named.Code)
	}
	code, generic := getBootDocHTTP(t, s, docKindSystemInteraction, systemInteractionDocKey)
	if code != http.StatusOK {
		t.Fatalf("generic route: %d", code)
	}
	var namedDTO bootDocDTO
	if err := json.Unmarshal(named.Body.Bytes(), &namedDTO); err != nil {
		t.Fatal(err)
	}
	if generic != namedDTO {
		t.Errorf("the two faces of one document disagree:\ngeneric=%+v\nnamed  =%+v", generic, namedDTO)
	}
}

// Every key of every kind must resolve to ITS OWN document. The failure this
// pins is the one a generic route makes possible and named routes cannot: a
// resolver that ignores the key and answers the kind's first document for all
// of them — which for boot_sequence means serving codex agents the claude
// checklist, and nothing that never boots reports it.
func TestGetBootDoc_EveryAddressResolvesToItsOwnDocument(t *testing.T) {
	s := newEventProcServer(t)
	for _, reg := range bootDocRegistry {
		for _, key := range reg.Keys {
			code, dto := getBootDocHTTP(t, s, reg.Kind, key)
			if code != http.StatusOK {
				t.Errorf("%s/%s: status = %d", reg.Kind, key, code)
				continue
			}
			if dto.Kind != reg.Kind || dto.Key != key {
				t.Errorf("asked for %s/%s, got %s/%s", reg.Kind, key, dto.Kind, dto.Key)
			}
			if dto.Text == "" {
				t.Errorf("%s/%s folded to empty text", reg.Kind, key)
			}
		}
	}
}

// ── the write faces reach the same guards through the new address ───────────

func TestReplaceBootDocRoute_WritesThroughTheAddressAndReadsBack(t *testing.T) {
	s := newEventProcServer(t)
	const kind = docKindAcceleratedStop

	_, before := getBootDocHTTP(t, s, kind, bootDocSingletonKey)
	head, _, found := strings.Cut(before.Text, docBodyMarker)
	if !found {
		t.Fatalf("%s has no body marker — this test's fixture assumption is stale", kind)
	}
	next := head + docBodyMarker + "\n\n照這份走。"

	w := httptest.NewRecorder()
	s.HandleReplaceBootDocApiBootDocsKindKeyPost(w,
		jsonPost(`{"text":`+mustJSONString(next)+`}`), kind, bootDocSingletonKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	code, after := getBootDocHTTP(t, s, kind, bootDocSingletonKey)
	if code != http.StatusOK {
		t.Fatalf("read back: %d", code)
	}
	if after.Text != next {
		t.Errorf("read back %q, wrote %q", after.Text, next)
	}
	if after.IsDefault {
		t.Error("is_default is still true after a write")
	}

	// And the OTHER documents did not move — the address wrote one of them.
	for _, kindOther := range eventProcKinds() {
		if kindOther == kind {
			continue
		}
		_, dto := getBootDocHTTP(t, s, kindOther, bootDocSingletonKey)
		if !dto.IsDefault {
			t.Errorf("writing %s also moved %s", kind, kindOther)
		}
	}

	// reset through the same address puts the shipped text back.
	rec := httptest.NewRecorder()
	s.HandleResetBootDocApiBootDocsKindKeyResetPost(rec, ownerPost("/x"), kind, bootDocSingletonKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	_, restored := getBootDocHTTP(t, s, kind, bootDocSingletonKey)
	if restored.Text != before.Text || !restored.IsDefault {
		t.Errorf("reset did not restore the shipped text (is_default=%v)", restored.IsDefault)
	}
}

func TestReplaceBootDocRoute_ReadOnlyKindRefusesThroughTheGenericAddress(t *testing.T) {
	s := newEventProcServer(t)
	for _, kind := range readOnlyEventProcKinds() {
		t.Run(kind, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.HandleReplaceBootDocApiBootDocsKindKeyPost(w,
				jsonPost(`{"text":"換掉"}`), kind, bootDocSingletonKey)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("replace: status = %d, want 405 (%s)", w.Code, w.Body.String())
			}
			rec := httptest.NewRecorder()
			s.HandleResetBootDocApiBootDocsKindKeyResetPost(rec, ownerPost("/x"), kind, bootDocSingletonKey)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("reset: status = %d, want 405 (%s)", rec.Code, rec.Body.String())
			}
			_, dto := getBootDocHTTP(t, s, kind, bootDocSingletonKey)
			if !dto.IsDefault || !dto.ReadOnly {
				t.Errorf("after two refusals: is_default=%v read_only=%v", dto.IsDefault, dto.ReadOnly)
			}
		})
	}
}
