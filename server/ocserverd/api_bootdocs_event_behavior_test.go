package main

// api_bootdocs_event_procedures_t3201_test.go — the six event-procedure
// documents T-3201 adds, and the two gates they brought with them: the
// read-only flag no write face may pass, and the variable validation on the
// write face.
//
// Which documents are read-only is pinned per entry by TestBootDocRegFor; what
// pins the door actually opening for the rest is
// TestReplaceBootDoc_EveryShippedKindAcceptsAnEditThroughTheRoute.
//
// The registry-driven cases below deliberately iterate bootDocRegistry rather
// than a second hand-written list. Adding a boot document used to mean editing
// eight scattered switches, four of which have no gate at all; a table that
// walks the registry is the gate those four never had.

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// ── fixture: an apiServer over a real DB. The cases here call the domain
// helpers (replaceBootDoc / resetBootDoc / foldBootDocDTO) DIRECTLY, one kind at
// a time; the route that carries them — the generic {kind}/{key} family — is
// addressed in api_bootdocs_generic_t3201_test.go. Keeping the split means a
// guard on the document's own rules cannot be satisfied by a routing change, or
// lost to one. ───────────────────────────────────────────────────────────────
//
// Developer mode is on: these cases are about the documents' own rules, not
// the lock in front of them.

func newEventProcServer(t *testing.T) *apiServer {
	t.Helper()
	db, err := openMigratedSQLite(t, filepath.Join(t.TempDir(), "event-proc.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := dal.PutSetting(settingDeveloperMode, "true"); err != nil {
		t.Fatalf("developer mode: %v", err)
	}
	auth, err := loadAuthSettings(dal, defaultConfig(), func(string) {})
	if err != nil {
		t.Fatalf("loadAuthSettings: %v", err)
	}
	s := newAPIServer(dal, NewHub(), singleKeyring([]byte(interopSecret)), 3600, "../..")
	s.adoptSettings(auth)
	return s
}

// seedOf reads a spec's shipped seed, failing the test if it is not there.
func seedOf(t *testing.T, s *apiServer, spec bootDocSpec) string {
	t.Helper()
	seed, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil || !hasSeed {
		t.Fatalf("read seed %q: hasSeed=%v err=%v", spec.SeedFile, hasSeed, err)
	}
	return seed
}

func ownerPost(path string) *http.Request {
	return httptest.NewRequest(http.MethodPost, path, nil)
}

// eventProcKinds are the event-procedure kinds, spelled out rather than filtered
// out of the registry: a test that derived them from the registry would agree
// with a registry that lost one.
func eventProcKinds() []string {
	return []string{
		docKindAcceleratedStop, docKindTaskCloseout, docKindTaskReassignPredecessor,
		docKindTaskTakeoverWithPredecessor, docKindTaskUnblocked,
		docKindTaskReadyForDone,
	}
}

// readOnlyProbeSpec is a spec the registry does not contain: one real document's
// spec with ReadOnly flipped on. Every handler-level test below drives this
// instead of a registry kind, so what is measured is the gate itself,
// independent of which shipped documents are read-only.
func readOnlyProbeSpec(t *testing.T, s *apiServer) bootDocSpec {
	t.Helper()
	spec := s.mustBootDocSpec(docKindTaskUnblocked, bootDocSingletonKey)
	spec.ReadOnly = true
	return spec
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
	spec := readOnlyProbeSpec(t, s)
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
}

// allow_shrink is the documented way past the wipe guard, and the MCP schema
// hands it to agents. It must not be a way past this one.
func TestReplaceBootDoc_ReadOnlyKindRefusesEvenWithAllowShrink(t *testing.T) {
	s := newEventProcServer(t)
	spec := readOnlyProbeSpec(t, s)
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
	w := httptest.NewRecorder()
	s.resetBootDoc(w, ownerPost("/x"), readOnlyProbeSpec(t, s))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestReplaceBootDoc_DeclaredVariableIsAccepted(t *testing.T) {
	s := newEventProcServer(t)
	spec := s.mustBootDocSpec(docKindTaskCloseout, bootDocSingletonKey)
	head, _, split := DocSplitHeadBody(seedOf(t, s, spec))
	if !split {
		t.Fatal("task_closeout's seed lost its read-only head")
	}
	// Since T-3201 the write face takes the BODY alone, and the declared names
	// ride in the head. The body is stored under the shipped head.
	const body = "收尾就照這裡寫的做。"
	want := DocJoinHeadBody(head, body)
	w := httptest.NewRecorder()
	s.replaceBootDoc(w, ownerPost("/x"), spec, body, false)
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
// owner-edited bodies may quote JSON the {name} syntax cannot tell from a
// variable, so validating them would refuse that text.
func TestReplaceBootDoc_PreT3201KindsAreNotVariableValidated(t *testing.T) {
	s := newEventProcServer(t)
	spec := s.mustBootDocSpec(docKindSystemInteraction, systemInteractionDocKey)
	if spec.Vars != nil {
		t.Fatalf("system_interaction must opt out of variable validation, declares %v", spec.Vars)
	}
	w := httptest.NewRecorder()
	// The BODY alone (T-3201): the head is the server's to join back on, and
	// this case is about the BRACES.
	seed, _, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		t.Fatal(err)
	}
	// 🔴 NO read-only head here since T-6f44: 系統互動's head was only its own
	// title line, and it went back into the body. What this case is about is the
	// BRACES, and the assertion that matters is that the whole seed — title line
	// included — is now the editable body.
	if _, _, split := DocSplitHeadBody(seed); split {
		t.Fatal("system_interaction is declared unsplit but its seed still carries the marker")
	}
	s.replaceBootDoc(w, ownerPost("/x"), spec,
		`回傳 {"id": "<attachment id>"} 這種東西`, false)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
}

// ── the read faces stay open ─────────────────────────────────────────────────

// A document with no overlay serves its SHIPPED text, marked is_default.
func TestFoldBootDocDTO_ReadOnlyKindServesItsSeed(t *testing.T) {
	s := newEventProcServer(t)
	for _, kind := range eventProcKinds() {
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

// 🔴 ALL SIX NOW, NOT FOUR (T-6f44). The two exceptions here were the two
// read-only kinds, and the reason recorded for their depth of three was 「nothing
// can write them, so a depth for them would be a number about a list that is
// always empty」. Decision 2 made them writable from the same text box as the
// other eight, so that reason expired with the lock — and a two-of-ten retention
// depth nobody chose is exactly the kind of leftover this comment exists to stop.
func TestDocumentHistoryKeepFor_EditableEventProceduresKeepTen(t *testing.T) {
	for _, kind := range eventProcKinds() {
		if got := documentHistoryKeepFor(kind); got != 10 {
			t.Errorf("documentHistoryKeepFor(%q) = %d, want 10", kind, got)
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

// A flag that says "editable" and a door that actually opens are two claims,
// so every editable document's write is driven THROUGH THE ROUTE and read back.
// Registry-wide on purpose: a test naming a few would go green the day another
// ships locked by accident.
func TestReplaceBootDoc_EveryEditableKindAcceptsAnEditThroughTheRoute(t *testing.T) {
	f := newBootDocFixture(t)
	for _, reg := range bootDocRegistry {
		if reg.ReadOnly {
			continue
		}
		for _, key := range reg.Keys {
			t.Run(reg.Kind+"/"+key, func(t *testing.T) {
				// The probe is deliberately plain text; split-head variables are
				// rendered only when a notice is sent, while the body is preserved.
				const probe = "probe: this document is editable."
				status, body := f.do(t, http.MethodPost,
					"/api/boot-docs/"+reg.Kind+"/"+key, f.admin,
					map[string]string{"body": probe})
				if status != http.StatusOK {
					t.Fatalf("replace answered %d (%s)", status, body)
				}
				// A 200 that stored nothing looks, from the cockpit, exactly like a
				// locked document.
				status, body = f.do(t, http.MethodGet,
					"/api/boot-docs/"+reg.Kind+"/"+key, f.admin, nil)
				if status != http.StatusOK {
					t.Fatalf("read back answered %d (%s)", status, body)
				}
				if !strings.Contains(body, probe) {
					t.Errorf("the edit was accepted but is not in the document: %s", body)
				}
			})
		}
	}
}

// Restore is the write face that reaches a document SIDEWAYS: not from an
// editor, but by putting an old version back — and it writes the overlay row
// itself. So the read-only refusal has to be spelled a SECOND time, on this
// path (documentHistoryAllowed); a copy of it living only in replaceBootDoc
// would be a gate this door never passes. That is still true after T-3201,
// which moved the JOIN into a function both faces call but deliberately left
// this one where it is — it has to answer before the
// capability check, which is a property of this door rather than of the
// shared rules.
//
// ⚠️ NO ORDINAL HERE ON PURPOSE. This comment used to open "Restore is the
// THIRD write face"; that number drifted twice while nothing turned red (see
// docs/design/boot-documents.md §七, which now forbids writing one). To learn
// how many write faces there are, count the callers of putBootDocumentOn.
func TestDocumentHistoryRestore_RefusesExactlyTheReadOnlyKindsAsReadOnly(t *testing.T) {
	f := newBootDocFixture(t)
	for _, reg := range bootDocRegistry {
		for _, key := range reg.Keys {
			t.Run(reg.Kind+"/"+key, func(t *testing.T) {
				status, body := f.do(t, http.MethodPost,
					"/api/document-history/"+reg.Kind+"/"+key+"/1/restore", f.admin, nil)
				if got := status == http.StatusMethodNotAllowed; got != reg.ReadOnly {
					t.Errorf("restore answered %d (%s), read-only %v", status, body, reg.ReadOnly)
				}
			})
		}
	}
}
