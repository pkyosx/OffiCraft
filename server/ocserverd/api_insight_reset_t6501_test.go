package main

// api_insight_reset_t6501_test.go — the way back to the factory insight seed.
//
// 🔴 WHY THE OVERLAY IN EVERY FIXTURE MUST DIFFER FROM THE SEED. This ticket
// exists because on 2026-08-04 two people in a row misread the state of this
// document, and both misreads survived because the numbers they compared were
// EQUAL BY COINCIDENCE (a written overlay that happened to be byte-identical to
// the shipped seed — 317 == 317). Every assertion below that compares "what the
// reset produced" against "the factory seed" is TAUTOLOGICALLY TRUE whenever
// the overlay already equals the seed. So each fixture asserts the difference
// FIRST, and only then asserts the reset moved the document.
//
// 🔴 THE SECOND TRAP, WRITTEN DOWN BECAUSE IT COST THE SAME TWO PEOPLE A CALL:
// `IsSeed` (on RoleDefDTO) means "this role HAS a factory version available".
// It does NOT mean "what you are reading right now IS the factory version" —
// that is `IsDefault`. The reset handler asks the SEED FILE, never IsSeed.
//
// MUTANTS (each preceded by `go clean -testcache`; restore from a scratchpad
// backup, never `git checkout --`) — see the record in the task report:
//   ① swap SaveWithDocumentHistory for a bare putInsightOn →
//      TestResetInsight_RetainsTheOverlayItDiscarded goes red (and NOTHING
//      else: the response body is identical either way, which is the whole
//      reason that test exists).
//   ② drop the no-seed 404 → TestResetInsight_RoleWithNoSeedIs404 goes red.
//   ③ apply the doc cap on this path → TestResetInsight_IgnoresTheDocCap goes
//      red.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// resetInsight drives the handler as the owner and returns the recorder, so a
// caller can assert on a refusal as easily as on the happy path.
func resetInsight(t *testing.T, s *apiServer, roleKey string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleResetInsightApiInsightRoleKeyResetPost(rec,
		taskReq(t, http.MethodPost, "/api/insight/"+roleKey+"/reset", nil, "owner", "owner"), roleKey)
	return rec
}

// writeInsightOverlay puts a doc that is GUARANTEED to differ from the seed —
// the anti-tautology precondition this whole file rests on.
func writeInsightOverlay(t *testing.T, s *apiServer, roleKey, text, seed string) {
	t.Helper()
	if text == seed {
		t.Fatal("fixture bug: the overlay must DIFFER from the seed, or every assertion below is vacuously true (this is the 317 == 317 defect that opened T-6501)")
	}
	if strings.TrimSpace(seed) == "" {
		t.Fatal("the shipped assistant insight seed is empty — every comparison below would be vacuous")
	}
	if err := s.dal.PutInsight(Insight{RoleKey: roleKey, Text: text}); err != nil {
		t.Fatalf("PutInsight: %v", err)
	}
}

const overlayUnlikeTheSeed = "# Insight — 這一份是角色自己寫的，不是出廠版。\n\n刪除成本與判斷取捨的個人筆記。\n"

// Acceptance #1: a role that has written its own insight can get the factory
// wording back, and the answer says so with is_default.
func TestResetInsight_PutsTheFactorySeedBack(t *testing.T) {
	s := newTasksTestServer(t)
	seed := readShippedAssistantInsightSeed(t, s)
	writeInsightOverlay(t, s, seedRoleAssistant, overlayUnlikeTheSeed, seed)

	// POSITIVE CONTROL: prove the doc really is the custom one first, so the
	// assertions after the reset are measuring a MOVE and not a no-op.
	before := getInsightDTO(t, s, seedRoleAssistant)
	if before.Text != overlayUnlikeTheSeed || before.IsDefault {
		t.Fatalf("precondition: doc should be the written overlay with is_default=false; got is_default=%v text=%q",
			before.IsDefault, before.Text)
	}

	rec := resetInsight(t, s, seedRoleAssistant)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: status=%d body=%s", rec.Code, rec.Body.String())
	}
	// T-91: reset_insight answers a RECEIPT, not the document. The claim this
	// test makes is unchanged — the factory seed is what is now stored, verbatim
	// — but the receipt states it with a hash instead of 2,687 characters the
	// caller can already read through get_insight. The verbatim comparison did
	// not weaken: sha256 over the seed is a stronger equality than the eyeball
	// one, and size_chars still cross-checks the length independently.
	var dto insightReceiptDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode insightReceiptDTO: %v", err)
	}
	if !dto.IsDefault {
		t.Fatal("is_default must flip to TRUE — false says a person wrote what the reset just restored, and the cockpit renders it that way")
	}
	if dto.Sha256 != receiptSha256(seed) {
		t.Fatalf("reset receipt sha256 %q is not the factory seed's", dto.Sha256)
	}
	if dto.SizeChars != utf8.RuneCountInString(seed) {
		t.Fatalf("size_chars %d disagrees with the served seed (%d runes)", dto.SizeChars, utf8.RuneCountInString(seed))
	}
	// And the READ face agrees — the response is not a one-off projection.
	if after := getInsightDTO(t, s, seedRoleAssistant); after.Text != seed || !after.IsDefault {
		t.Fatalf("GET after reset: is_default=%v, %d chars; want the factory seed", after.IsDefault, len(after.Text))
	}
}

// Acceptance #2, and 🔴 THE ONE NOTHING ELSE CAN SEE. Wiring the reset to a
// bare putInsightOn answers the SAME 200 with the SAME body; the only
// difference is that the overlay it discarded becomes unrecoverable. So this
// asserts the retained revision exists AND that it carries the pre-reset
// overlay — not the seed, not an empty doc.
func TestResetInsight_RetainsTheOverlayItDiscarded(t *testing.T) {
	s := newTasksTestServer(t)
	seed := readShippedAssistantInsightSeed(t, s)
	writeInsightOverlay(t, s, seedRoleAssistant, overlayUnlikeTheSeed, seed)

	// The history is keyed by the BARE role_key for insight (historyKeyParts
	// only splits on "::" for lessons) — a "<role>::<something>" key here would
	// read an address the handler never writes and find nothing, which looks
	// exactly like a missing revision.
	const kind = "insight"
	if n := len(agentList(t, s, kind, seedRoleAssistant)); n != 0 {
		t.Fatalf("fixture retained %d revisions before the reset — the assertion below could pass on someone else's write", n)
	}

	if rec := resetInsight(t, s, seedRoleAssistant); rec.Code != http.StatusOK {
		t.Fatalf("reset: status=%d body=%s", rec.Code, rec.Body.String())
	}

	history := agentList(t, s, kind, seedRoleAssistant)
	if len(history) == 0 {
		t.Fatal("reset_insight retained nothing — the overlay it replaced is unrecoverable")
	}
	got := history[0].Content["text"]
	if got == seed {
		t.Fatal("the retained revision is the SEED — a reset must retain what it discarded, not what it restored")
	}
	if got != overlayUnlikeTheSeed {
		t.Fatalf("retained text = %q, want the pre-reset overlay %q — the newest revision is not the version this write replaced",
			got, overlayUnlikeTheSeed)
	}
	// The tombstone the reset wrote is not the retained state; the retained
	// state is the LIVE overlay it replaced.
	if history[0].Content["tombstoned"] != "false" {
		t.Fatalf("retained tombstoned = %q, want \"false\" — the discarded overlay was a live document",
			history[0].Content["tombstoned"])
	}
}

// has_seed is the cockpit's ONLY way to know whether to offer the reset at all
// (T-6501): DocumentHistoryEntry may not grow a 初始版本 row where the server
// would 404. So the field has to be true for the one role that ships a seed and
// FALSE for roles that do not — 🔴 and the false case is what makes this test
// worth having. Asserting only the assistant would pass just as happily against
// a server that hard-coded `true`, and the resulting cockpit would offer every
// custom role a reset that 404s.
func TestInsightHasSeed_TrueOnlyWhereAFactoryVersionExists(t *testing.T) {
	s := newTasksTestServer(t)

	if got := getInsightDTO(t, s, seedRoleAssistant); !got.HasSeed {
		t.Fatal("assistant must report has_seed=true — seeds/insight_assistant.md ships, so the reset is available")
	}
	for _, roleKey := range []string{"r-tester", "r-engineer", "reviewer"} {
		if got := getInsightDTO(t, s, roleKey); got.HasSeed {
			t.Fatalf("role %q reports has_seed=true but has no seed file — the cockpit would offer a reset the server 404s", roleKey)
		}
	}
}

// 🔴 has_seed and is_default answer DIFFERENT questions, and the state where
// they disagree is the one that matters: a seeded role that has written its own
// insight reads has_seed=true, is_default=false — precisely when the reset is
// most worth offering. A field derived from is_default (or from "the doc is
// factory text") would report false here and hide the affordance.
func TestInsightHasSeed_SurvivesTheRoleWritingItsOwn(t *testing.T) {
	s := newTasksTestServer(t)
	seed := readShippedAssistantInsightSeed(t, s)
	writeInsightOverlay(t, s, seedRoleAssistant, overlayUnlikeTheSeed, seed)

	got := getInsightDTO(t, s, seedRoleAssistant)
	if got.IsDefault {
		t.Fatal("precondition: the role has written, is_default must be false")
	}
	if !got.HasSeed {
		t.Fatal("has_seed flipped false because the role wrote its own doc — it answers what exists to fall back TO, not what is being read")
	}

	// The WRITE response itself must say the same thing: the cockpit adopts it
	// directly after a save, so a false here removes the row until a refetch.
	rec := httptest.NewRecorder()
	s.HandleReplaceInsightApiInsightRoleKeyPost(rec, taskReq(t, http.MethodPost,
		"/api/insight/"+seedRoleAssistant, map[string]any{"text": overlayUnlikeTheSeed + "\nmore"},
		"owner", "owner"), seedRoleAssistant)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var wrote insightDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &wrote); err != nil {
		t.Fatal(err)
	}
	if !wrote.HasSeed || wrote.IsDefault {
		t.Fatalf("replace answered has_seed=%v is_default=%v; want true/false",
			wrote.HasSeed, wrote.IsDefault)
	}
}

// Acceptance #3: a role with no factory insight has nothing to reset TO, the
// same rule reset_role applies to a role with no seed definition.
func TestResetInsight_RoleWithNoSeedIs404(t *testing.T) {
	s := newTasksTestServer(t)

	// POSITIVE CONTROL: the seeded role must succeed in this same fixture, or
	// a build where seeds simply do not resolve would satisfy the 404 below.
	if rec := resetInsight(t, s, seedRoleAssistant); rec.Code != http.StatusOK {
		t.Fatalf("positive control: the seeded role must reset here; got %d %s", rec.Code, rec.Body.String())
	}

	for _, roleKey := range []string{"r-tester", "r-engineer", "reviewer"} {
		rec := resetInsight(t, s, roleKey)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("reset %q: status=%d body=%s; want 404 — there is no factory version to reset to",
				roleKey, rec.Code, rec.Body.String())
		}
	}
}

// Resetting an already-default doc is a no-op that still answers the seed —
// idempotent, exactly like reset_role's tombstone-over-tombstone.
func TestResetInsight_IsIdempotent(t *testing.T) {
	s := newTasksTestServer(t)
	seed := readShippedAssistantInsightSeed(t, s)
	writeInsightOverlay(t, s, seedRoleAssistant, overlayUnlikeTheSeed, seed)

	for i := 0; i < 3; i++ {
		rec := resetInsight(t, s, seedRoleAssistant)
		if rec.Code != http.StatusOK {
			t.Fatalf("reset #%d: status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	if dto := getInsightDTO(t, s, seedRoleAssistant); dto.Text != seed || !dto.IsDefault {
		t.Fatalf("after repeated resets: is_default=%v, %d chars; want the factory seed", dto.IsDefault, len(dto.Text))
	}
}

// 🔴 The doc cap must NOT be consulted on the way back to factory content —
// the same posture reset_role has. Behaviour on the two blocks has to match, or
// the office grows a state nobody predicts: the Duty resets fine while the very
// same gesture on Insight is refused by a ceiling the OWNER set afterwards.
//
// 🔴 THE FIXTURE HAS TO LOWER THE CAP, and the first version of this test did
// not — it ran on the SHIPPED cap, under which the seed fits comfortably, so a
// cap check added to this handler would have refused nothing and the test
// stayed green. Measured: a mutant that inserts a DocCapBlocked check here
// produced ZERO red tests. The cap is therefore set BELOW the seed's own
// length, with the overlay shorter still, so DocCapBlocked is provably true for
// the write the reset performs — the only arrangement in which "the reset
// succeeded" says anything at all.
func TestResetInsight_IgnoresTheDocCap(t *testing.T) {
	s := newTasksTestServer(t)
	seed := readShippedAssistantInsightSeed(t, s)
	writeInsightOverlay(t, s, seedRoleAssistant, overlayUnlikeTheSeed, seed)

	seedRunes := utf8.RuneCountInString(seed)
	if seedRunes <= utf8.RuneCountInString(overlayUnlikeTheSeed) {
		t.Fatalf("fixture bug: the seed (%d runes) must be LONGER than the overlay (%d) — otherwise restoring it is a shrink and no cap would refuse it either way",
			seedRunes, utf8.RuneCountInString(overlayUnlikeTheSeed))
	}
	// Reach past the settings whitelist on purpose: its floor is the shipped
	// default, and a cap at or above the default cannot express this question.
	s.settingsMu.Lock()
	s.docCapCharsInsight = seedRunes - 1
	s.settingsMu.Unlock()

	// ANTI-TAUTOLOGY: prove the cap this handler would consult really does
	// refuse the write the reset is about to perform.
	if !DocCapBlocked(s.insightCap(), overlayUnlikeTheSeed, seed) {
		t.Fatalf("fixture bug: cap=%d must refuse restoring the %d-rune seed over the %d-rune overlay, or this test proves nothing",
			s.insightCap(), seedRunes, utf8.RuneCountInString(overlayUnlikeTheSeed))
	}

	if rec := resetInsight(t, s, seedRoleAssistant); rec.Code != http.StatusOK {
		t.Fatalf("reset was refused (%d %s) — the way back to factory content must not be gated on a length limit the owner set for HAND-WRITTEN docs",
			rec.Code, rec.Body.String())
	}
	if dto := getInsightDTO(t, s, seedRoleAssistant); dto.Text != seed {
		t.Fatalf("after reset the doc is %d chars, want the %d-char factory seed", len(dto.Text), len(seed))
	}
}
