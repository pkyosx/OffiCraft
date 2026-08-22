package main

// api_bootdocs_event_procedures_t3201_test.go — the six event-procedure
// documents T-3201 adds, and the two gates they brought with them: the
// read-only pair no write face may touch, and the variable validation on the
// write face.
//
// The registry-driven cases below deliberately iterate bootDocRegistry rather
// than a second hand-written list. Adding a boot document used to mean editing
// eight scattered switches, four of which have no gate at all; a table that
// walks the registry is the gate those four never had.

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// ── fixture: an apiServer over a real DB. The cases here call the domain
// helpers (replaceBootDoc / resetBootDoc / foldBootDocDTO) DIRECTLY, one kind at
// a time; the route that carries them — the generic {kind}/{key} family — is
// addressed in api_bootdocs_generic_t3201_test.go. Keeping the split means a
// guard on the document's own rules cannot be satisfied by a routing change, or
// lost to one. ───────────────────────────────────────────────────────────────

func newEventProcServer(t *testing.T) *apiServer {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "event-proc.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return newAPIServer(dal, NewHub(), []byte(interopSecret), 3600, "../..")
}

func ownerPost(path string) *http.Request {
	return httptest.NewRequest(http.MethodPost, path, nil)
}

// eventProcKinds are the six kinds this ticket adds, spelled out rather than
// filtered out of the registry: a test that derived them from the registry
// would agree with a registry that lost one.
func eventProcKinds() []string {
	return []string{
		docKindAcceleratedStop, docKindTaskCloseout, docKindTaskReassignPredecessor,
		docKindTaskTakeoverWithPredecessor, docKindTaskTakeoverFresh, docKindTaskUnblocked,
	}
}

func readOnlyEventProcKinds() []string {
	return []string{docKindTaskTakeoverFresh, docKindTaskUnblocked}
}

// ── the registry answers for every kind on every face ────────────────────────

func TestBootDocRegistry_EveryKindResolvesOnEveryFace(t *testing.T) {
	s := newEventProcServer(t)
	seen := map[string]bool{}
	for _, reg := range bootDocRegistry {
		if seen[reg.Kind] {
			t.Fatalf("kind %q appears twice in bootDocRegistry", reg.Kind)
		}
		seen[reg.Kind] = true
		if len(reg.Keys) == 0 {
			t.Fatalf("kind %q registers no key, so nothing can address it", reg.Kind)
		}
		for _, key := range reg.Keys {
			spec, ok := s.bootDocSpecFor(reg.Kind, key)
			if !ok {
				t.Fatalf("bootDocSpecFor(%q, %q) = not found", reg.Kind, key)
			}
			if !bootDocHistoryKeyKnown(reg.Kind, key) {
				t.Errorf("bootDocHistoryKeyKnown(%q, %q) = false", reg.Kind, key)
			}
			if spec.Cap <= 0 {
				t.Errorf("%s/%s has cap %d — a document with no ceiling", reg.Kind, key, spec.Cap)
			}
			// has_seed=false is the failure mode with no gate anywhere else:
			// the document folds to "" and the reset face 404s, so it has no
			// way back at all.
			if _, hasSeed, err := s.root.seedBlockMD(spec.SeedFile); err != nil || !hasSeed {
				t.Errorf("%s/%s: seed %q missing (err=%v) — run bin/build-seedsdist",
					reg.Kind, key, spec.SeedFile, err)
			}
		}
	}
	for _, kind := range eventProcKinds() {
		if !seen[kind] {
			t.Errorf("kind %q is not in bootDocRegistry", kind)
		}
	}
}

// 🔴 THE VARIABLE GATE ON THE SEEDS THEMSELVES. A seed that names a variable
// its kind does not declare is a sentence that reaches an agent with the braces
// still in it, and before this test nothing in the tree would have noticed —
// there was no interpolation and no validation anywhere on the server.
func TestBootDocRegistry_EverySeedDeclaresExactlyTheVariablesItUses(t *testing.T) {
	s := newEventProcServer(t)
	for _, reg := range bootDocRegistry {
		if reg.Vars == nil {
			continue // opted out of validation — see doc_vars.go
		}
		for _, key := range reg.Keys {
			t.Run(reg.Kind+"/"+key, func(t *testing.T) {
				spec, ok := s.bootDocSpecFor(reg.Kind, key)
				if !ok {
					t.Fatalf("bootDocSpecFor(%q, %q) = not found", reg.Kind, key)
				}
				seed, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
				if err != nil || !hasSeed {
					t.Fatalf("read seed %q: hasSeed=%v err=%v", spec.SeedFile, hasSeed, err)
				}
				if bad := DocVarsUndeclared(seed, spec.Vars); len(bad) > 0 {
					t.Errorf("seed uses %v, which the kind does not declare (declares %v)", bad, spec.Vars)
				}
				used := map[string]bool{}
				for _, n := range DocVarsIn(seed) {
					used[n] = true
				}
				for _, declared := range spec.Vars {
					if !used[declared] {
						t.Errorf("kind declares %q but no slot in the seed uses it", declared)
					}
				}
			})
		}
	}
}

// ── read-only: shown, folded, never written ──────────────────────────────────

func TestReplaceBootDoc_ReadOnlyKindRefusesAndWritesNothing(t *testing.T) {
	s := newEventProcServer(t)
	for _, kind := range readOnlyEventProcKinds() {
		t.Run(kind, func(t *testing.T) {
			spec := s.mustBootDocSpec(kind, bootDocSingletonKey)
			before, err := s.foldBootDocDTO(spec)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			s.replaceBootDoc(w, ownerPost("/x"), spec, "換掉的內容", false)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
			}
			after, err := s.foldBootDocDTO(spec)
			if err != nil {
				t.Fatal(err)
			}
			if after.Text != before.Text || !after.IsDefault {
				t.Errorf("the document moved: is_default %v→%v, %d→%d chars",
					before.IsDefault, after.IsDefault, before.SizeChars, after.SizeChars)
			}
		})
	}
}

// allow_shrink is the documented way past the wipe guard, and the MCP schema
// hands it to agents. It must not be a way past this one.
func TestReplaceBootDoc_ReadOnlyKindRefusesEvenWithAllowShrink(t *testing.T) {
	s := newEventProcServer(t)
	spec := s.mustBootDocSpec(docKindTaskUnblocked, bootDocSingletonKey)
	w := httptest.NewRecorder()
	s.replaceBootDoc(w, ownerPost("/x"), spec, "", true)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
	after, err := s.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !after.IsDefault || after.SizeChars == 0 {
		t.Errorf("the document was wiped: is_default=%v, %d chars", after.IsDefault, after.SizeChars)
	}
}

func TestResetBootDoc_ReadOnlyKindRefuses(t *testing.T) {
	s := newEventProcServer(t)
	for _, kind := range readOnlyEventProcKinds() {
		t.Run(kind, func(t *testing.T) {
			spec := s.mustBootDocSpec(kind, bootDocSingletonKey)
			w := httptest.NewRecorder()
			s.resetBootDoc(w, ownerPost("/x"), spec)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

// ── the write face's variable validation ─────────────────────────────────────

func TestReplaceBootDoc_UndeclaredVariableIsRefusedAndNothingIsWritten(t *testing.T) {
	s := newEventProcServer(t)
	spec := s.mustBootDocSpec(docKindTaskCloseout, bootDocSingletonKey)
	before, err := s.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.replaceBootDoc(w, ownerPost("/x"), spec, "任務 {task_nu} 已結束。", false)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	after, err := s.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	if after.Text != before.Text || !after.IsDefault {
		t.Errorf("a refused write moved the document: is_default %v→%v", before.IsDefault, after.IsDefault)
	}
}

// The positive control the refusal above needs: the SAME write with the name
// spelt right is accepted. Without it, a validator that refused everything
// would pass the test above.
func TestReplaceBootDoc_DeclaredVariableIsAccepted(t *testing.T) {
	s := newEventProcServer(t)
	spec := s.mustBootDocSpec(docKindTaskCloseout, bootDocSingletonKey)
	want := "任務 {task_no} 已結束。"
	w := httptest.NewRecorder()
	s.replaceBootDoc(w, ownerPost("/x"), spec, want, false)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	after, err := s.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	if after.Text != want {
		t.Errorf("read back %q, want %q", after.Text, want)
	}
	if after.IsDefault {
		t.Error("is_default stayed true after an accepted edit")
	}
}

// The three documents that shipped before this mechanism opt out of it: their
// seeds carry JSON examples the {name} syntax cannot tell from a variable, so
// validating them would refuse the factory text itself.
func TestReplaceBootDoc_PreT3201KindsAreNotVariableValidated(t *testing.T) {
	s := newEventProcServer(t)
	spec := s.mustBootDocSpec(docKindSystemInteraction, systemInteractionDocKey)
	if spec.Vars != nil {
		t.Fatalf("system_interaction must opt out of variable validation, declares %v", spec.Vars)
	}
	w := httptest.NewRecorder()
	// Under the document's own read-only head (T-3201): the head is required
	// back verbatim on every write, and this case is about the BRACES.
	seed, _, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		t.Fatal(err)
	}
	head, _, split := DocSplitHeadBody(seed)
	if !split {
		t.Fatal("system_interaction's seed lost its read-only head")
	}
	s.replaceBootDoc(w, ownerPost("/x"), spec,
		DocJoinHeadBody(head, `回傳 {"id": "<attachment id>"} 這種東西`), false)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
}

// ── the read faces stay open ─────────────────────────────────────────────────

func TestFoldBootDocDTO_ReadOnlyKindServesItsSeed(t *testing.T) {
	s := newEventProcServer(t)
	for _, kind := range readOnlyEventProcKinds() {
		t.Run(kind, func(t *testing.T) {
			spec := s.mustBootDocSpec(kind, bootDocSingletonKey)
			seed, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
			if err != nil || !hasSeed {
				t.Fatalf("seed %q: hasSeed=%v err=%v", spec.SeedFile, hasSeed, err)
			}
			dto, err := s.foldBootDocDTO(spec)
			if err != nil {
				t.Fatal(err)
			}
			if dto.Text != seed {
				t.Errorf("a read-only document must still SERVE its text; got %d chars, seed has %d",
					dto.SizeChars, len(seed))
			}
			if !dto.HasSeed || !dto.IsDefault {
				t.Errorf("has_seed=%v is_default=%v, want both true", dto.HasSeed, dto.IsDefault)
			}
		})
	}
}

// The capacity report this file used to pin these documents into is GONE — the
// whole 「哪些文件快滿了」 notice was removed in the same ticket (owner
// rc-5d06304ca54b: every capped document already reports size and cap on its own
// read face, so the notice was a second way to learn something the reader was
// already holding). The test that required every event procedure to appear in it
// went with it rather than being weakened: keeping an assertion whose subject no
// longer exists is how a suite starts describing a build nobody ships.

func TestDocumentHistoryKeepFor_EditableEventProceduresKeepTen(t *testing.T) {
	for _, kind := range eventProcKinds() {
		readOnly := kind == docKindTaskTakeoverFresh || kind == docKindTaskUnblocked
		want := 10
		if readOnly {
			want = documentHistoryKeepDefault
		}
		if got := documentHistoryKeepFor(kind); got != want {
			t.Errorf("documentHistoryKeepFor(%q) = %d, want %d", kind, got, want)
		}
	}
}

// ── the document-history faces, over the wired stack ─────────────────────────
//
// These run over HTTP rather than against the apiServer directly because the
// authz ladder lives in the ROUTE TABLE: a test that called the handler would
// stay green with the floor set to anything at all.

func TestDocumentHistorySeed_ServesEveryRegisteredBootDocKind(t *testing.T) {
	f := newBootDocFixture(t)
	for _, reg := range bootDocRegistry {
		for _, key := range reg.Keys {
			t.Run(reg.Kind+"/"+key, func(t *testing.T) {
				status, body := f.do(t, http.MethodGet,
					"/api/document-history/"+reg.Kind+"/"+key+"/seed", f.owner, nil)
				if status != http.StatusOK {
					t.Fatalf("status = %d, want 200 (%s) — without this the version list's "+
						"factory row 404s and the shipped text cannot be compared to", status, body)
				}
			})
		}
	}
}

func TestDocumentHistoryList_ServesEveryRegisteredBootDocKind(t *testing.T) {
	f := newBootDocFixture(t)
	for _, reg := range bootDocRegistry {
		for _, key := range reg.Keys {
			t.Run(reg.Kind+"/"+key, func(t *testing.T) {
				status, body := f.do(t, http.MethodGet,
					"/api/document-history/"+reg.Kind+"/"+key, f.owner, nil)
				if status != http.StatusOK {
					t.Fatalf("status = %d, want 200 (%s)", status, body)
				}
			})
		}
	}
}

// A key this server does not serve must be refused, not answered with an empty
// version list: "you used the wrong key" and "this document has no versions
// yet" must not look the same.
func TestDocumentHistoryList_UnknownKeyForANewKindIsRefused(t *testing.T) {
	f := newBootDocFixture(t)
	status, _ := f.do(t, http.MethodGet,
		"/api/document-history/"+docKindTaskCloseout+"/claude", f.owner, nil)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", status, http.StatusBadRequest)
	}
}

// Restore is the THIRD write face, and the one that reaches a document
// sideways — it writes the overlay row itself, so a read-only gate that lived
// only in replaceBootDoc would be a gate this path walks around.
func TestDocumentHistoryRestore_ReadOnlyKindIsRefused(t *testing.T) {
	f := newBootDocFixture(t)
	for _, kind := range readOnlyEventProcKinds() {
		t.Run(kind, func(t *testing.T) {
			status, body := f.do(t, http.MethodPost,
				"/api/document-history/"+kind+"/"+bootDocSingletonKey+"/1/restore", f.admin, nil)
			if status != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want %d (%s)", status, http.StatusMethodNotAllowed, body)
			}
		})
	}
}
