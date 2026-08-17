package main

// api_context_cap_t3aeb_test.go — T-3aeb: the document size cap became a
// SETTING, and the patch receipt's `size` started speaking the cap's unit
// instead of bytes.
//
// ⚠️ T-ae38 (2026-08-03) split that one setting into FOUR
// (`doc.cap_chars.duty` / `.insight` / `.learning` / `.manual`; the old
// `doc.cap_chars` was RENAMED to `.manual` by migration 00048). This file is
// the LESSONS half of the story and now names `doc.cap_chars.learning`
// throughout — the rulings below are unchanged, they just apply per segment.
// The per-segment independence itself is pinned in api_doc_caps_tae38_test.go.
//
// Owner rulings (2026-07-31, cards rc-286b34b60388 / rc-33b88ed80212):
//   - shipped default = contextDocMaxCharsDefault, adjustable up to
//     maxDocCapChars — the FLOOR IS THE DEFAULT, so
//     the cap can only ever be raised. Lowering it would turn documents that are
//     legal today into shrink-only ones;
//   - the receipt's `size` counts CHARACTERS, reversing the earlier "frozen wire
//     field, do not touch" ruling, because one subject may not have two units.
//
// ⚠️ WHY THE MULTI-BYTE FIXTURES ARE LOAD-BEARING. Before this file, EVERY
// assertion on `size` in the repo was ASCII-only (api_lessons_patch_test.go,
// api_taskmanuals_patch_test.go, conformance's `size > 0`) — and for ASCII the
// byte count and the rune count are the SAME NUMBER. The unit flip was
// therefore invisible to the entire suite: it could have shipped either way and
// nothing would have reddened. The CJK fixtures below are the only thing that
// makes the change falsifiable, so do not "simplify" them to ASCII.
//
// The T-3351 sentinel next door still pins the RULE (the three lines); this
// file pins only what T-3aeb changed: where the cap comes from, and what unit
// the receipt speaks.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// cjkDoc builds an n-rune Chinese document. Every rune is 3 bytes in UTF-8, so
// a byte count of the result is ~3n — far enough from n that any assertion
// mixing the two units cannot pass by coincidence.
func cjkDoc(t *testing.T, n int) string {
	t.Helper()
	doc := strings.Repeat("字", n)
	if utf8.RuneCountInString(doc) != n {
		t.Fatalf("cjkDoc built %d runes, want %d", utf8.RuneCountInString(doc), n)
	}
	if len(doc) == n {
		t.Fatalf("cjkDoc is not multi-byte — the fixture has lost its discriminating power")
	}
	return doc
}

// patchSettings PATCHes /api/settings with an owner token and returns the
// status plus the decoded body.
func patchSettings(t *testing.T, srv, tok, body string) (int, map[string]any) {
	t.Helper()
	return doJSON(t, "PATCH", srv+"/api/settings", tok, body)
}

// ── the cap follows the setting ──────────────────────────────────────────────

// TestDocCap_FollowsTheLiveSetting is the whole point of the change: a write
// that the shipped default refuses must LAND once the owner raises the cap, on
// the same running server, with no restart.
//
// The three phases are one story on purpose. Phase 1 proves the gate is real at
// the default (without it, phase 3 could pass on a server whose cap never
// applied at all — a broken gate would also "accept" the write); phase 2 raises
// it; phase 3 proves the raise reached the write path.
func TestDocCap_FollowsTheLiveSetting(t *testing.T) {
	srv, dal, tok := capLessonsServer(t)
	before := capDoc(t, contextDocMaxCharsDefault-10)
	seedLessonsOverlay(t, dal, "assistant", "general", before)

	// Over the DEFAULT cap, and not shorter than what is stored → refused.
	overDefault := capDoc(t, contextDocMaxCharsDefault+500)
	status, data := replaceLessons(t, srv, tok, overDefault, false)
	if status != http.StatusBadRequest {
		t.Fatalf("phase 1: a write over the default cap must be refused, got %d: %v", status, data)
	}
	defaultCap := strconv.Itoa(contextDocMaxCharsDefault)
	if msg := capErrMessage(data); !strings.Contains(msg, defaultCap) {
		t.Fatalf("phase 1: the refusal must name the cap in force (%s), got %q", defaultCap, msg)
	}

	// The owner raises the cap.
	if status, data := patchSettings(t, srv.URL, tok, `{"doc_cap_chars_learning":20000}`); status != http.StatusOK {
		t.Fatalf("phase 2: raising the cap must be accepted, got %d: %v", status, data)
	}

	// The SAME write now lands — no restart, no re-login.
	status, data = replaceLessons(t, srv, tok, overDefault, false)
	if status != http.StatusOK {
		t.Fatalf("phase 3: after raising the cap the same write must land, got %d: %v", status, data)
	}
	if got := getLessonsText(t, srv.URL, tok, "assistant", "general"); got != overDefault {
		t.Fatalf("phase 3: the raised cap must actually store the doc (%d runes stored)",
			utf8.RuneCountInString(got))
	}

	// And the NEW cap is the one now being enforced — not merely "no cap".
	status, data = replaceLessons(t, srv, tok, capDoc(t, 20001), false)
	if status != http.StatusBadRequest {
		t.Fatalf("phase 4: the raised cap must still be a cap, got %d: %v", status, data)
	}
	if msg := capErrMessage(data); !strings.Contains(msg, "20000") {
		t.Fatalf("phase 4: the refusal must name the NEW cap (20000), got %q", msg)
	}
}

// TestDocCap_RefusalQuotesTheLiveCapNotTheDefault pins the message itself. The
// refusal is the ONLY way an agent learns the cap — get_settings is admin-only,
// so a worker that hits the gate cannot look the number up. A message still
// still quoting the SHIPPED DEFAULT after the owner raised it would send every
// agent shrinking toward a limit that no longer exists.
func TestDocCap_RefusalQuotesTheLiveCapNotTheDefault(t *testing.T) {
	srv, dal, tok := capLessonsServer(t)
	seedLessonsOverlay(t, dal, "assistant", "general", capDoc(t, 30000))

	if status, data := patchSettings(t, srv.URL, tok, `{"doc_cap_chars_learning":25000}`); status != http.StatusOK {
		t.Fatalf("raise the cap: %d %v", status, data)
	}

	// Over 25000 and NOT shorter than the 30000 stored → refused.
	_, data := replaceLessons(t, srv, tok, capDoc(t, 30000), false)
	msg := capErrMessage(data)
	if !strings.Contains(msg, "25000") {
		t.Fatalf("the refusal must quote the live cap 25000, got %q", msg)
	}
	if strings.Contains(msg, strconv.Itoa(contextDocMaxCharsDefault)+"-char") {
		t.Fatalf("the refusal still quotes the shipped default, not the live cap: %q", msg)
	}
}

// ── the receipt speaks the cap's unit ────────────────────────────────────────

// TestPatchLessonsReceiptSizeIsCharsNotBytes — the T-3aeb unit fix, on the
// lessons face. A member hit the live defect on 2026-07-31: the refusal said
// "10184 chars, over the 10000-char cap", and the very next successful write
// answered `size: 22856` for a document nowhere near that many characters.
func TestPatchLessonsReceiptSizeIsCharsNotBytes(t *testing.T) {
	srv, dal, tok := capLessonsServer(t)
	seedLessonsOverlay(t, dal, "assistant", "general", cjkDoc(t, 200))

	status, data := patchLessons(t, srv.URL, tok, "assistant", "general",
		`{"edits":[{"old":"","new":"`+cjkDoc(t, 50)+`"}]}`)
	if status != http.StatusOK {
		t.Fatalf("append patch must land, got %d: %v", status, data)
	}

	stored := getLessonsText(t, srv.URL, tok, "assistant", "general")
	wantRunes := utf8.RuneCountInString(stored)
	if len(stored) == wantRunes {
		t.Fatalf("fixture is not multi-byte — this test cannot tell the units apart")
	}
	got, _ := data["size_chars"].(float64)
	if int(got) != wantRunes {
		t.Fatalf("receipt size must be CHARACTERS: got %v, runes=%d (bytes=%d)",
			data["size_chars"], wantRunes, len(stored))
	}
	if int(got) == len(stored) {
		t.Fatalf("receipt size_chars is still the BYTE count (%d)", len(stored))
	}
	// The receipt also states the cap it was judged against, so the caller can
	// compute its remaining budget without a second (admin-only) request.
	if capGot, _ := data["cap_chars"].(float64); int(capGot) != contextDocMaxCharsDefault {
		t.Fatalf("receipt must report the live cap, got %v", data["cap_chars"])
	}
}

// TestPatchTaskLearningsReceiptSizeIsCharsNotBytes — the same contract on the
// task-manual face. Both faces are pinned because they are two independent
// literal expressions; fixing one and missing the other is the obvious way to
// half-land this change, and nothing else in the suite would notice.
func TestPatchTaskLearningsReceiptSizeIsCharsNotBytes(t *testing.T) {
	api := newTasksTestServer(t)
	key := seedManualWithLearnings(t, api, cjkDoc(t, 200))

	status, data := patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit("", cjkDoc(t, 50))},
	})
	if status != http.StatusOK {
		t.Fatalf("append patch must land, got %d: %v", status, data)
	}

	stored := storedLearnings(t, api, key)
	wantRunes := utf8.RuneCountInString(stored)
	if len(stored) == wantRunes {
		t.Fatalf("fixture is not multi-byte — this test cannot tell the units apart")
	}
	got, _ := data["size_chars"].(float64)
	if int(got) != wantRunes {
		t.Fatalf("receipt size must be CHARACTERS: got %v, runes=%d (bytes=%d)",
			data["size_chars"], wantRunes, len(stored))
	}
	if int(got) == len(stored) {
		t.Fatalf("receipt size_chars is still the BYTE count (%d)", len(stored))
	}
	// The receipt also states the cap it was judged against, so the caller can
	// compute its remaining budget without a second (admin-only) request.
	if capGot, _ := data["cap_chars"].(float64); int(capGot) != contextDocMaxCharsDefault {
		t.Fatalf("receipt must report the live cap, got %v", data["cap_chars"])
	}
}

// TestReplaceLessonsReceiptReportsSizeAndCap — the whole-document write face
// answers with the same two numbers, from a THIRD literal expression. It needs
// its own pin: before this test, a mutant that reported the wrong cap here, and
// one that counted its size in bytes, each reddened NOTHING in the suite.
func TestReplaceLessonsReceiptReportsSizeAndCap(t *testing.T) {
	srv, _, tok := capLessonsServer(t)
	doc := cjkDoc(t, 500)

	status, data := replaceLessons(t, srv, tok, doc, false)
	if status != http.StatusOK {
		t.Fatalf("replace must land, got %d: %v", status, data)
	}
	if got, _ := data["size_chars"].(float64); int(got) != utf8.RuneCountInString(doc) {
		t.Fatalf("size_chars must be CHARACTERS: got %v want %d (bytes=%d)",
			data["size_chars"], utf8.RuneCountInString(doc), len(doc))
	}
	if got, _ := data["cap_chars"].(float64); int(got) != contextDocMaxCharsDefault {
		t.Fatalf("cap_chars must be the live cap: got %v", data["cap_chars"])
	}

	// And it FOLLOWS the setting instead of quoting the shipped default.
	if status, data := patchSettings(t, srv.URL, tok, `{"doc_cap_chars_learning":41000}`); status != http.StatusOK {
		t.Fatalf("raise the cap: %d %v", status, data)
	}
	_, data = replaceLessons(t, srv, tok, doc+cjkDoc(t, 10), false)
	if got, _ := data["cap_chars"].(float64); int(got) != 41000 {
		t.Fatalf("cap_chars must track the setting, got %v", data["cap_chars"])
	}
}

// ── the setting's own surface ────────────────────────────────────────────────

// TestUpdateSettingsDocCapCharsRange pins the range the owner set, both ends,
// and the floor's REASON: the floor is the shipped default, so there is no such
// thing as lowering this cap.
//
// 🔴 EVERY knob is exercised, not just one. Until T-30f1 this test drove only
// `doc_cap_chars_learning`, so a knob whose floor was written as the wrong
// constant — say the manual halves given Duty's much smaller minimum, which
// would let the owner lower them — went green here. A per-knob table is the
// only shape that fails when a row is missing or holds the wrong floor.
func TestUpdateSettingsDocCapCharsRange(t *testing.T) {
	knobs := []struct {
		field string
		key   string
		floor int
		live  func(*apiServer) int
	}{
		{"doc_cap_chars_duty", settingDocCapCharsDuty, dutyCapCharsDefault,
			func(s *apiServer) int { return s.dutyCap() }},
		{"doc_cap_chars_insight", settingDocCapCharsInsight, contextDocMaxCharsDefault,
			func(s *apiServer) int { return s.insightCap() }},
		{"doc_cap_chars_learning", settingDocCapCharsLearning, contextDocMaxCharsDefault,
			func(s *apiServer) int { return s.learningCap() }},
		{"doc_cap_chars_manual_sop", settingDocCapCharsManualSop, contextDocMaxCharsDefault,
			func(s *apiServer) int { return s.manualSopCap() }},
		{"doc_cap_chars_manual_learnings", settingDocCapCharsManualLearnings, contextDocMaxCharsDefault,
			func(s *apiServer) int { return s.manualLearningsCap() }},
	}
	for _, k := range knobs {
		t.Run(k.field, func(t *testing.T) {
			api, srv, d, _ := newSettingsTestServer(t, "settings-pass")
			status, data := doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"settings-pass"}`)
			if status != 200 {
				t.Fatalf("login: %d", status)
			}
			owner := data["token"].(string)

			// The default is served, so a cockpit that never PATCHes still sees
			// a cap. This is also the assertion that catches a knob the server
			// never puts on the wire: absent reads as the Go zero value, and the
			// frontend's `?? DEFAULT` fallback would otherwise disguise it.
			if status, data := doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != 200 ||
				data[k.field] != float64(k.floor) {
				t.Fatalf("GET must serve %s's default: %d %v", k.field, status, data[k.field])
			}

			// Below the floor is refused — including the value one under the
			// default, which is the shape a "let me lower it a little" attempt
			// actually takes.
			below := strconv.Itoa(k.floor - 1)
			for _, body := range []string{
				`{"` + k.field + `":` + below + `}`,
				`{"` + k.field + `":0}`,
				`{"` + k.field + `":-1}`,
				`{"` + k.field + `":100001}`,
				`{"handover_pct":60,"` + k.field + `":` + below + `}`, // one bad field poisons the patch
			} {
				if status, _ := patchSettings(t, srv.URL, owner, body); status != 422 {
					t.Fatalf("PATCH %s: want 422, got %d", body, status)
				}
			}
			if v, err := d.GetSetting(k.key); err != nil || v != nil {
				t.Fatalf("a rejected patch must write nothing to %s: %v %v", k.key, v, err)
			}
			if got := k.live(api); got != k.floor {
				t.Fatalf("a rejected patch must not move %s's live cap: %d", k.field, got)
			}

			// Both ends of the range are accepted, durable, and live.
			for _, n := range []string{strconv.Itoa(k.floor), "100000", "42000"} {
				status, data := patchSettings(t, srv.URL, owner, `{"`+k.field+`":`+n+`}`)
				if status != 200 {
					t.Fatalf("PATCH %s=%s: want 200, got %d: %v", k.field, n, status, data)
				}
				if v, err := d.GetSetting(k.key); err != nil || v == nil || *v != n {
					t.Fatalf("%s=%s must be durable: %v %v", k.field, n, v, err)
				}
				if got := k.live(api); got != atoiOrFail(t, n) {
					t.Fatalf("%s=%s must be live immediately, got %d", k.field, n, got)
				}
			}
		})
	}
}

func atoiOrFail(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			t.Fatalf("atoiOrFail: %q", s)
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// TestLoadAuthSettingsRejectsAnOutOfRangeDocCap — a hand-edited DB row must not
// be able to install a cap the PATCH face would have refused. Loud at boot
// beats a server quietly running a cap nobody chose (the posture
// TestLoadAuthSettingsFailsLoudOnCorruptValues already pins for its neighbours).
func TestLoadAuthSettingsRejectsAnOutOfRangeDocCap(t *testing.T) {
	// Every key, for the reason the PATCH-face table above gives: a key missing
	// from the load face is never range-checked at all, and the way that shows
	// up is a server that boots happily on a cap the PATCH face would refuse.
	keys := map[string]int{
		settingDocCapCharsDuty:            dutyCapCharsDefault,
		settingDocCapCharsInsight:         contextDocMaxCharsDefault,
		settingDocCapCharsLearning:        contextDocMaxCharsDefault,
		settingDocCapCharsManualSop:       contextDocMaxCharsDefault,
		settingDocCapCharsManualLearnings: contextDocMaxCharsDefault,
	}
	for key, floor := range keys {
		for _, bad := range []string{strconv.Itoa(floor - 1), "100001", "0", "-5", "lots"} {
			d := newTestDAL(t)
			if err := d.PutSetting(key, bad); err != nil {
				t.Fatalf("PutSetting: %v", err)
			}
			// loadAuthSettings directly, NOT loadForTest: that helper t.Fatal's
			// on a load error, so it can never express "this load must fail".
			if _, err := loadAuthSettings(d, defaultConfig(), func(string) {}); err == nil {
				t.Fatalf("%s=%q must fail the boot load, not be silently accepted", key, bad)
			}
		}
		// Positive control: at the floor the same key loads. Without it, a load
		// face that refused everything would satisfy the loop above.
		d := newTestDAL(t)
		if err := d.PutSetting(key, strconv.Itoa(floor)); err != nil {
			t.Fatalf("PutSetting: %v", err)
		}
		if _, err := loadAuthSettings(d, defaultConfig(), func(string) {}); err != nil {
			t.Fatalf("%s at its floor must load: %v", key, err)
		}
	}
}

// ── the read faces report size and cap (owner 2026-07-31, card rc-3800e090f5e1) ──

// TestLessonsReadReportsSizeAndCap — the gap the owner asked to close: before
// this, an agent learned the limit ONLY by being refused, because the settings
// surface that holds it is admin-only. Reading the doc now answers both "how
// big is it" and "how big may it get".
func TestLessonsReadReportsSizeAndCap(t *testing.T) {
	srv, dal, tok := capLessonsServer(t)
	doc := cjkDoc(t, 300)
	seedLessonsOverlay(t, dal, "assistant", "general", doc)

	status, data := doJSON(t, "GET", srv.URL+"/api/lessons/assistant/general", tok, "")
	if status != http.StatusOK {
		t.Fatalf("GET lessons: %d %v", status, data)
	}
	// CJK on purpose: with ASCII the two units are the same number, so a byte
	// count would satisfy this assertion by coincidence.
	if got, _ := data["size_chars"].(float64); int(got) != utf8.RuneCountInString(doc) {
		t.Fatalf("size_chars must be the doc's CHARACTER count: got %v want %d (bytes=%d)",
			data["size_chars"], utf8.RuneCountInString(doc), len(doc))
	}
	if got, _ := data["cap_chars"].(float64); int(got) != contextDocMaxCharsDefault {
		t.Fatalf("cap_chars must be the live cap: got %v", data["cap_chars"])
	}

	// And it FOLLOWS the setting, like every other reader of the cap.
	if status, data := patchSettings(t, srv.URL, tok, `{"doc_cap_chars_learning":33000}`); status != 200 {
		t.Fatalf("raise the cap: %d %v", status, data)
	}
	_, data = doJSON(t, "GET", srv.URL+"/api/lessons/assistant/general", tok, "")
	if got, _ := data["cap_chars"].(float64); int(got) != 33000 {
		t.Fatalf("cap_chars must track the setting, got %v", data["cap_chars"])
	}
}

// TestTaskManualReadReportsPerDocumentSizes — the manual carries TWO capped
// documents, so it reports TWO sizes. One combined total would answer neither
// question, since the cap applies to each separately.
func TestTaskManualReadReportsPerDocumentSizes(t *testing.T) {
	api := newTasksTestServer(t)
	learnings := cjkDoc(t, 400)
	key := seedManualWithLearnings(t, api, learnings)

	sop := cjkDoc(t, 120)
	if rec := updateManual(t, api, key, map[string]any{"sop_md": sop}); rec.Code != http.StatusOK {
		t.Fatalf("seed sop_md: %d %s", rec.Code, rec.Body.String())
	}

	rec := httptest.NewRecorder()
	api.HandleGetTaskManualApiTaskManualsTypeKeyGet(rec, taskReq(t, "GET",
		"/api/task-manuals/"+key, nil, "m-exec", "agent"), key)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET manual: %d %s", rec.Code, rec.Body.String())
	}
	var data map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("decode manual: %v", err)
	}
	if got, _ := data["learnings_chars"].(float64); int(got) != utf8.RuneCountInString(learnings) {
		t.Fatalf("learnings_chars: got %v want %d (bytes=%d)",
			data["learnings_chars"], utf8.RuneCountInString(learnings), len(learnings))
	}
	if got, _ := data["sop_md_chars"].(float64); int(got) != utf8.RuneCountInString(sop) {
		t.Fatalf("sop_md_chars: got %v want %d", data["sop_md_chars"], utf8.RuneCountInString(sop))
	}
	// The two are DIFFERENT numbers here, so a swap or a shared total cannot
	// pass: that is the whole reason the fixture uses two distinct lengths.
	if data["learnings_chars"] == data["sop_md_chars"] {
		t.Fatalf("fixture lost its discriminating power: both docs measure the same")
	}
	if got, _ := data["cap_chars"].(float64); int(got) != contextDocMaxCharsDefault {
		t.Fatalf("cap_chars must be the live cap: got %v", data["cap_chars"])
	}
}

// TestListViewOmitsTheTextButNotItsSize — the listing drops the bulky sop_md /
// learnings, and it would have been easy to let their sizes fall out as 0 along
// with them. A 0 that looks like a measurement is worse than the omission it
// describes: the list is exactly where "which manual is close to the cap" gets
// asked.
func TestListViewOmitsTheTextButNotItsSize(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		docCapCharsManualSop:       contextDocMaxCharsDefault,
		docCapCharsManualLearnings: contextDocMaxCharsDefault}
	learnings := cjkDoc(t, 260)
	sop := cjkDoc(t, 90)
	if err := s.dal.PutTaskManual(TaskManual{
		TypeKey: "tm-sized", DisplayName: "sized", Purpose: "p",
		Fields: `[]`, SopMD: sop, Learnings: learnings, Assignee: `{}`, UpdatedTS: 1,
	}); err != nil {
		t.Fatal(err)
	}

	list := listManuals(t, s)
	if len(list) != 1 {
		t.Fatalf("want 1 manual, got %d", len(list))
	}
	got := list[0]
	// The narrowing still happened — otherwise this test would pass on the
	// full projection and prove nothing about the listing.
	for _, absent := range []string{"sop_md", "learnings"} {
		if _, present := listManualRows(t, s)[0][absent]; present {
			t.Fatalf("listing must still omit %q", absent)
		}
	}
	if got.LearningsChars != utf8.RuneCountInString(learnings) {
		t.Fatalf("learnings_chars must survive the narrowing: got %d want %d",
			got.LearningsChars, utf8.RuneCountInString(learnings))
	}
	if got.SopMDChars != utf8.RuneCountInString(sop) {
		t.Fatalf("sop_md_chars must survive the narrowing: got %d want %d",
			got.SopMDChars, utf8.RuneCountInString(sop))
	}
	if got.CapChars != contextDocMaxCharsDefault {
		t.Fatalf("cap_chars on the list view: got %d", got.CapChars)
	}
}

func ptrTo[T any](v T) *T { return &v }

// TestRestoreFollowsTheLiveCap — the restore path calls DocCapBlocked too, so
// raising the cap must make a previously un-restorable revision restorable.
// This is the one cap consumer the round-1 tests missed: the independent review
// pointed out that mutant M1 (docCap() pinned to the default) reddened four
// tests, NONE of them a restore test — so a regression pinning restore to the
// shipped default would have shipped silently, while the cockpit's marking
// (which is tested) said the opposite.
func TestRestoreFollowsTheLiveCap(t *testing.T) {
	api := newTasksTestServer(t)
	const role, taskType = seedRoleAssistant, "tm-cap-live"
	oversized := strings.Repeat("x", contextDocMaxCharsDefault+50)

	// An over-cap revision is retained (the cap never truncates what is stored),
	// then the live doc shrinks — so restoring would push it back over.
	if err := api.dal.PutLessons(Lessons{RoleKey: role, TaskType: taskType, Text: oversized}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	api.HandleReplaceLessonsApiLessonsRoleKeyTaskTypePost(rec, taskReq(t, http.MethodPost,
		"/api/lessons/"+role+"/"+taskType, map[string]any{"text": "short again"}, "owner", "owner"),
		role, taskType)
	if rec.Code != http.StatusOK {
		t.Fatalf("shrinking write: %d %s", rec.Code, rec.Body.String())
	}
	stored, err := api.dal.ListDocumentHistory("lessons", role+"::"+taskType)
	if err != nil || len(stored) == 0 {
		t.Fatalf("history = %+v, %v", stored, err)
	}

	restore := func() int {
		rec := httptest.NewRecorder()
		api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
			taskReq(t, http.MethodPost, "/api/document-history/lessons/"+role+"::"+taskType+"/"+
				strconv.FormatInt(stored[0].ID, 10)+"/restore", nil, "owner", "owner"),
			"lessons", role+"::"+taskType, stored[0].ID)
		return rec.Code
	}

	// At the shipped cap the restore is refused — without this the pass below
	// could equally mean "the restore gate never applied at all".
	if code := restore(); code != http.StatusBadRequest {
		t.Fatalf("at the default cap the over-cap restore must be refused, got %d", code)
	}

	// Raise the cap above the revision's size; the same restore now lands.
	api.settingsMu.Lock()
	api.docCapCharsLearning = contextDocMaxCharsDefault + 1000
	api.settingsMu.Unlock()

	if code := restore(); code != http.StatusOK {
		t.Fatalf("after raising the cap the restore must land, got %d", code)
	}
	current, err := api.foldLessonsDTO(role, taskType)
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != oversized {
		t.Fatalf("the restore must actually put the revision back (%d chars live)",
			utf8.RuneCountInString(current.Text))
	}
}
