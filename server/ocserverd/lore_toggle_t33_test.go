package main

// lore_toggle_t33_test.go — T-33: the station-wide LORE feature switch
// (settings `lore.enabled`, default OFF).
//
// 🔴 THE OFF HALF IS THE HALF THAT NEEDS THE TESTS, AND IT IS THE HALF THAT
// DISAPPEARS QUIETLY. When the feature is off there is by definition nothing to
// see: no directory in a boot document, no subject list, no lore tab. So
// 「correctly nothing」 and 「the fold is broken, therefore nothing」 render as
// the SAME BYTES, and a test that only checks for an absence passes just as
// happily on a server whose lore code has been deleted outright.
//
// 🔴 EVERY ABSENCE ASSERTED BELOW THEREFORE CARRIES A POSITIVE CONTROL ON THE
// SAME SERVER WITH THE SAME FIXTURE: first prove the thing WOULD be there with
// the switch on, then flip the switch and prove it is gone. Without the control
// half, none of these tests distinguishes the feature being off from the
// feature being absent — which is the exact mistake that makes an off-state
// test worthless.
//
// The three faces the owner named, one section each below:
//   讀 — resume_summary carries no subject list; a boot context folds in no
//        對象目錄 section.
//   寫 — every lore route refuses, out loud, naming the switch.
//   UI — the cockpit's own half is asserted in frontend/src/App.lore-toggle
//        .test.tsx; what the SERVER owes the cockpit is `lore_enabled` on
//        GET /api/settings, pinned here.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// disableLoreForTest is the twin of enableLoreForTest: it puts the switch back
// to its SHIPPED value. It exists so an off-state test can seed a fixture
// through the ordinary helpers (which turn the feature on so the fixture is
// real) and then flip it off — the control and the case then run against one
// server and one fixture, which is what makes the comparison mean anything.
func disableLoreForTest(s *apiServer) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.loreEnabled = false
}

// loreFixtureSubjectLines are seedLoreDirectoryFixture's three subjects AS THE
// DIRECTORY RENDERS THEM — the `- <canonical>` line form of loreSubjectLine.
//
// 🔴 IT IS THE LINE AND NOT THE BARE NAME ON PURPOSE. `repo:officraft`,
// `agent:Kyle` and `human:Seth` all appear in the seeded write_lore_entry
// EXAMPLES that every boot document carries whether or not the feature is on,
// so asserting the bare name would report a directory that is not there. The
// leading "- " is produced by nothing but the directory itself.
var loreFixtureSubjectLines = []string{
	"- human:Seth", "- agent:Kyle", "- repo:officraft",
}

// loreToggleStack is loreGovStack's off-by-default twin: the same wired stack
// and the same two identities, with the feature switch left where the product
// ships it. It returns the apiServer so a test can flip the switch mid-run.
func loreToggleStack(t *testing.T) (srvURL string, dal *DAL, api *apiServer, agentTok, ownerTok string) {
	t.Helper()
	srv, dal, secret, api := newLessonsTestServerAPI(t)
	now := time.Now().Unix()
	if err := dal.PutMember(Member{
		ID: "m-lore-agent", Name: "lore-agent", Kind: KindAssistant, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put agent member: %v", err)
	}
	var err error
	if agentTok, err = mintJWT("m-lore-agent", "agent", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint agent token: %v", err)
	}
	if ownerTok, err = mintJWT("owner", "owner", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint owner token: %v", err)
	}
	return srv.URL, dal, api, agentTok, ownerTok
}

// ── 預設：關 ─────────────────────────────────────────────────────────────────

// TestLoreShipsOffAndTheDefaultIsNotAnAccident — the owner said 「預設是關閉起來
// 的」 in as many words, so the default is a requirement and not an
// implementation preference.
//
// 🔴 THE CONTROL IS THE SECOND HALF: the SAME loader, over the SAME database,
// reads `true` once the row says so. Without it, a loader that ignored the key
// entirely — or one that could not parse a bool — would leave the first
// assertion green and the switch permanently stuck off, which looks exactly
// like a correct default until the owner tries to turn the feature on.
func TestLoreShipsOffAndTheDefaultIsNotAnAccident(t *testing.T) {
	s := newWorkerTestServer(t)
	disableLoreForTest(s) // the harness turns it on; this is about the SHIPPED value

	auth, err := loadAuthSettings(s.dal, Config{}, func(string) {})
	if err != nil {
		t.Fatalf("loadAuthSettings on a fresh station: %v", err)
	}
	if auth.loreEnabled {
		t.Fatal("a fresh station loads lore.enabled = true — the owner asked for OFF by default")
	}
	if s.loreEnabledSnapshot() {
		t.Fatal("the live snapshot reads on with no row written")
	}

	// Control: the loader really does read the key.
	if err := s.dal.PutSetting(settingLoreEnabled, "true"); err != nil {
		t.Fatalf("put setting: %v", err)
	}
	auth, err = loadAuthSettings(s.dal, Config{}, func(string) {})
	if err != nil {
		t.Fatalf("loadAuthSettings after the row was written: %v", err)
	}
	if !auth.loreEnabled {
		t.Fatal("lore.enabled = true in the DB and the loader still reads false — " +
			"the default above would then be an accident, not a decision")
	}
}

// ── 寫：關著的時候，寫入工具不可用，而且說得出原因 ──────────────────────────

// loreGatedRoutes is every method+path the T-33 lore feature serves, spelled
// out BY HAND. It is deliberately not derived from the route table: a list
// computed from the thing under test would follow the table into whatever
// mistake the table made, and 「the gate covers everything the table flags」 is
// a tautology while 「the gate covers these eleven addresses」 is a claim.
// TestEveryLoreRouteCarriesTheFeatureFlag confronts this list with the table.
var loreGatedRoutes = []struct{ method, path string }{
	{"GET", "/api/lore/entities/pending"},
	{"POST", "/api/lore/entities/en-nope/approve"},
	{"POST", "/api/lore/entities/en-nope/merge"},
	{"POST", "/api/lore/entries/e-nope/retire"},
	{"POST", "/api/lore/entries/e-nope/revive"},
	{"POST", "/api/lore/entries"},
	{"POST", "/api/lore/search"},
	{"GET", "/api/lore/entries/e-nope"},
	{"GET", "/api/lore/entries/e-nope/revisions/1"},
	{"POST", "/api/lore/entries/e-nope/proposals"},
	{"GET", "/api/lore/entries/e-nope/proposals"},
}

// TestLoreOffRefusesEveryLoreRouteAndSaysWhy is the 寫 row of the behaviour
// table, and it covers the read routes with it: with the switch off the feature
// does not exist for an agent at all.
//
// 🔴 IT ASSERTS THE WORDING, NOT JUST THE 403, AND THAT IS THE POINT OF THE
// TEST. A bare "forbidden" is indistinguishable to an agent from 「you are not
// allowed」 or 「you sent that wrong」, and an agent that believes either of
// those RETRIES — forever, because while the switch is off every retry fails
// identically. The refusal must name the switch, say nothing was written, and
// point at the tools that still work.
//
// 🔴 THE POSITIVE CONTROL IS THE SECOND PASS: the same requests on the same
// server with the switch ON must stop being 403. Without it a station whose
// lore routes were never registered at all would pass the first pass perfectly.
func TestLoreOffRefusesEveryLoreRouteAndSaysWhy(t *testing.T) {
	url, _, api, _, ownerTok := loreToggleStack(t)

	// 🔴 THE SWEEP RUNS AS THE OWNER, AND THAT IS A STATEMENT ABOUT ORDER. The
	// feature gate sits INSIDE the auth + RBAC chokes (buildHandler wraps the
	// gated handler, so the floor answers first), which is the right way round:
	// an unauthenticated stranger must not be able to probe which features a
	// station has switched on. Three of these rows admit only an admin agent or
	// the owner, so an ordinary agent would be turned away by the floor with
	// 「principal not permitted」 and this test would be asserting the floor's
	// wording instead of the gate's. The owner passes every floor, so what
	// answers below is always the gate.
	for _, r := range loreGatedRoutes {
		st, body := rosterREST(t, url, ownerTok, r.method, r.path, "{}")
		if st != http.StatusForbidden {
			t.Errorf("%s %s with the feature OFF: want 403, got %d %s",
				r.method, r.path, st, body)
			continue
		}
		for _, want := range []string{
			"lore.enabled",  // the setting an owner has to change
			"lore_enabled",  // the exact PATCH body key
			"/api/settings", // where to change it
			"重試",            // do not loop on this
			"learning",      // what still works
			"patch_lessons", // ...named concretely
			"沒有被寫進去",        // the write did NOT happen
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s %s refusal does not mention %q — an agent cannot act on it: %s",
					r.method, r.path, want, body)
			}
		}
	}

	// Positive control: with the switch ON these addresses stop answering 403,
	// which is what proves the first pass was the GATE talking and not a
	// missing route, a bad token, or a typo in the path.
	enableLoreForTest(api)
	for _, r := range loreGatedRoutes {
		st, body := rosterREST(t, url, ownerTok, r.method, r.path, "{}")
		if st == http.StatusForbidden && strings.Contains(body, "lore.enabled") {
			t.Errorf("%s %s still refuses with the feature ON: %d %s",
				r.method, r.path, st, body)
		}
	}
}

// TestEveryLoreRouteCarriesTheFeatureFlag confronts the hand-written list above
// with the served table, in BOTH directions.
//
// 🔴 THIS IS THE GUARD FOR THE ROUTE ADDED NEXT YEAR. The gate is applied from
// one place (specsFor) over rows that declare LoreGated, so a new /api/lore/*
// row that forgets the flag is served UNGATED — a write that lands on a station
// whose owner believes the feature is off, with nothing anywhere to say so. The
// reverse direction matters too: a flag on a row that is not part of the
// feature would silently take an unrelated endpoint off the air.
func TestEveryLoreRouteCarriesTheFeatureFlag(t *testing.T) {
	s := newWorkerTestServer(t)
	specs := specsFor(s)

	flagged := map[string]bool{}
	for _, spec := range specs {
		isLorePath := strings.HasPrefix(spec.Path, "/api/lore/")
		if isLorePath != spec.LoreGated {
			t.Errorf("%s %s: LoreGated = %v but its path says %v — "+
				"a lore route without the flag is served with the feature switched off",
				spec.Method, spec.Path, spec.LoreGated, isLorePath)
		}
		if spec.LoreGated {
			flagged[spec.Method+" "+spec.Path] = true
		}
	}
	if len(flagged) != len(loreGatedRoutes) {
		t.Errorf("the table flags %d lore rows, loreGatedRoutes lists %d — "+
			"the exercised set and the served set have drifted apart",
			len(flagged), len(loreGatedRoutes))
	}
}

// ── 讀 ①：開機脈絡不折入 lore 那一段 ─────────────────────────────────────────

// TestLoreOffKeepsTheSubjectDirectoryOutOfBothBootContexts is the 讀 row for
// the boot documents, and it covers BOTH assemblers because the switch is
// tested in the ONE shared fold they share (foldLoreSectionWithSurfacing).
//
// 🔴 正職 AND 外包 ARE BOTH ASSERTED EVEN THOUGH ONE CHECK GUARDS BOTH. That is
// not redundancy: the whole reason lore_fold.go exists is that these two
// documents must not drift, and a future edit that moves the switch into one
// caller would leave the other one folding the directory in while the owner
// believes the feature is off. Only asserting both catches that.
func TestLoreOffKeepsTheSubjectDirectoryOutOfBothBootContexts(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s) // also turns the feature ON

	staffOn, err := s.buildBootContext("", nil)
	if err != nil || staffOn == nil {
		t.Fatalf("buildBootContext (control): %v", err)
	}
	workerOn, err := s.buildWorkerBootContext(
		OutsourceWorker{ID: "ow-toggle", Codename: "O-9", Model: "opus", Effort: "high",
			Runtime: RuntimeClaude},
		Task{ID: "t-toggle", TypeKey: "review-pr", Title: "Review PR 42",
			Priority: TaskPriorityHigh}, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext (control): %v", err)
	}
	// The control. If either of these is missing, everything below is an
	// assertion about a directory that was never there in the first place.
	if !strings.Contains(staffAndWorker(staffOn.Context, workerOn), loreSectionH1) {
		t.Fatalf("control failed: with the feature ON neither boot context carries " +
			"the 對象目錄 — the absences below would be vacuous")
	}
	// 🔴 THE CONTROL CHECKS THE SUBJECT LINES, NOT ONLY THE HEADING, because the
	// off-case below checks the lines too — and a bare canonical like
	// `repo:officraft` also appears in the SEEDED write_lore_entry examples that
	// every boot document carries. The directory's own rendering is the line
	// form `- <canonical>`, which nothing else in the document produces, so that
	// is what both halves compare.
	for _, c := range []struct{ name, doc string }{
		{"正職", staffOn.Context}, {"外包", workerOn},
	} {
		if !strings.Contains(c.doc, loreSectionH1) {
			t.Fatalf("control failed: the %s boot context carries no 對象目錄", c.name)
		}
		for _, canonical := range loreFixtureSubjectLines {
			if !strings.Contains(c.doc, canonical) {
				t.Fatalf("control failed: the %s boot context does not print %q — "+
					"the absences below would be vacuous", c.name, canonical)
			}
		}
	}

	disableLoreForTest(s)

	staffOff, err := s.buildBootContext("", nil)
	if err != nil || staffOff == nil {
		t.Fatalf("buildBootContext (off): %v", err)
	}
	workerOff, err := s.buildWorkerBootContext(
		OutsourceWorker{ID: "ow-toggle", Codename: "O-9", Model: "opus", Effort: "high",
			Runtime: RuntimeClaude},
		Task{ID: "t-toggle", TypeKey: "review-pr", Title: "Review PR 42",
			Priority: TaskPriorityHigh}, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext (off): %v", err)
	}

	for _, c := range []struct{ name, doc string }{
		{"正職 buildBootContext", staffOff.Context},
		{"外包 buildWorkerBootContext", workerOff},
	} {
		if strings.Contains(c.doc, loreSectionH1) {
			t.Errorf("%s still folds in the 對象目錄 with the feature OFF", c.name)
		}
		// The heading is not the only thing that must be gone: a fold that kept
		// the subject LINES and dropped only the title would leave an agent
		// reading a directory it is not supposed to have.
		for _, canonical := range loreFixtureSubjectLines {
			if strings.Contains(c.doc, canonical) {
				t.Errorf("%s still prints the directory line %q with the feature OFF",
					c.name, canonical)
			}
		}
	}
}

// staffAndWorker joins the two documents so a single "neither of them has it"
// control reads as one sentence.
func staffAndWorker(staff, worker string) string { return staff + "\n" + worker }

// TestLoreOffFilesNoSurfacingReceipt — the journal half. A directory nobody was
// shown must not be recorded as shown: an entry that looks used can never be
// argued down later, which is the failure the design names by name.
func TestLoreOffFilesNoSurfacingReceipt(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s)

	// Control: with the feature on, the fold really does produce a receipt.
	_, surOn, err := s.foldLoreSectionWithSurfacing("m-anyone")
	if err != nil {
		t.Fatalf("fold (control): %v", err)
	}
	if !surOn.surfaced() {
		t.Fatal("control failed: the fold surfaced nothing with the feature ON")
	}

	disableLoreForTest(s)
	text, surOff, err := s.foldLoreSectionWithSurfacing("m-anyone")
	if err != nil {
		t.Fatalf("fold (off): %v", err)
	}
	if text != "" {
		t.Errorf("the fold produced %d bytes with the feature OFF", len(text))
	}
	if surOff.surfaced() || len(surOff.Subjects) != 0 || surOff.Omitted != 0 {
		t.Errorf("the fold filed a receipt for a directory nobody saw: %+v", surOff)
	}
}

// ── 讀 ②：resume_summary 不給對象清單 ────────────────────────────────────────

// TestLoreOffKeepsTheSubjectListOutOfResumeSummary is the owner's own sentence:
// 「我們 resume summary 也不會給他 對象清單」.
//
// ⚠️ WHAT THIS TEST ACTUALLY FOUND, STATED RATHER THAN HIDDEN: as of this
// change the wake snapshot carries NO subject list on any of its three faces
// even with the feature ON — the 對象目錄 reaches an agent through the BOOT
// CONTEXT, not through resume_summary. So the requirement is satisfied by the
// snapshot's current shape and not by a gate this ticket added. That makes this
// test a REGRESSION GUARD rather than a proof of new behaviour, and it is
// written so it stays honest: it seeds a real directory and asserts the
// snapshot names none of it while the switch is off.
//
// 🔴 THE CONTROL IS ON A DIFFERENT SURFACE ON PURPOSE. There is no on-state of
// resume_summary that carries the list, so the control cannot be 「turn it on
// and see it appear」. It is instead 「the same fixture, on the same server, IS
// findable — the boot context prints it」: that is what separates 「the snapshot
// withholds the list」 from 「the fixture never existed」, which is precisely the
// vacuous green this file is about.
func TestLoreOffKeepsTheSubjectListOutOfResumeSummary(t *testing.T) {
	s := newTasksTestServer(t)
	seedMachine(t, s, "m-host-one")
	if err := s.dal.PutMember(Member{ID: "m-reader", Name: "Reader",
		Kind: KindAssistant, Runtime: RuntimeClaude,
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive}); err != nil {
		t.Fatalf("put member: %v", err)
	}
	seedLoreDirectoryFixture(t, s) // turns the feature ON and files a real directory

	// The control: this fixture IS reachable on this server. If the boot context
	// does not print it, every absence below proves nothing at all.
	boot, err := s.buildBootContext("", nil)
	if err != nil || boot == nil {
		t.Fatalf("buildBootContext (control): %v", err)
	}
	for _, canonical := range loreFixtureSubjectLines {
		if !strings.Contains(boot.Context, canonical) {
			t.Fatalf("control failed: the seeded subject %q does not reach even the "+
				"boot context — the resume-summary absences below would be vacuous",
				canonical)
		}
	}

	disableLoreForTest(s)

	faces := []struct {
		name string
		call func() *httptest.ResponseRecorder
	}{
		{"GET /api/resume-summary", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			s.HandleResumeSummaryApiResumeSummaryGet(rec, perfReq("m-reader", "agent"))
			return rec
		}},
		{"GET /api/members/{id}/resume-summary", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			s.HandleGetMemberResumeSummaryApiMembersMemberIdResumeSummaryGet(
				rec, perfReq(wireOwnerID, "owner"), "m-reader")
			return rec
		}},
		{"GET /api/resume-summary-size", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			s.HandlePeekResumeSummarySizeApiResumeSummarySizeGet(rec, perfReq("m-reader", "agent"))
			return rec
		}},
	}
	for _, face := range faces {
		t.Run(face.name, func(t *testing.T) {
			rec := face.call()
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", face.name, rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, loreSectionH1) {
				t.Errorf("%s carries the 對象目錄 heading with the feature OFF", face.name)
			}
			for _, canonical := range loreFixtureSubjectLines {
				if strings.Contains(body, canonical) {
					t.Errorf("%s names the lore subject %q with the feature OFF: %s",
						face.name, canonical, body)
				}
			}
		})
	}
}

// ── 即時生效 ─────────────────────────────────────────────────────────────────

// TestFlippingTheSwitchAppliesToTheVeryNextCall is the promise made to the
// owner in as many words: 「你一開，他們當下就寫得進去」.
//
// 🔴 IT GOES THROUGH THE REAL PATCH, NOT THROUGH THE FIELD. Setting the field
// in a test would prove the gate reads a field; what was promised is that
// SAVING THE SETTING is enough — no restart, no cache to wait out. The failure
// this catches is the tempting cheaper implementation: capture the flag once
// when the route table is built. That version passes every other test in this
// file and breaks exactly this one.
func TestFlippingTheSwitchAppliesToTheVeryNextCall(t *testing.T) {
	url, _, _, agentTok, ownerTok := loreToggleStack(t)

	if st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/search", "{}"); st != http.StatusForbidden {
		t.Fatalf("before the save: want 403, got %d %s", st, body)
	}

	st, body := rosterREST(t, url, ownerTok, "PATCH", "/api/settings",
		`{"lore_enabled": true}`)
	if st != http.StatusOK {
		t.Fatalf("PATCH lore_enabled=true: %d %s", st, body)
	}
	var view settingsDTO
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decode settings echo: %v", err)
	}
	if !view.LoreEnabled {
		t.Fatalf("the settings echo still reads lore_enabled=false: %s", body)
	}

	// THE assertion: the next call, with no restart in between.
	if st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/search", "{}"); st != http.StatusOK {
		t.Fatalf("immediately after the save: want 200, got %d %s — "+
			"the switch was captured somewhere instead of read per call", st, body)
	}

	// And back off again, because a switch that only ever turns on is half a
	// switch: an owner who tried the feature and wants it off again must be able
	// to stop it, in the same one call.
	if st, body := rosterREST(t, url, ownerTok, "PATCH", "/api/settings",
		`{"lore_enabled": false}`); st != http.StatusOK {
		t.Fatalf("PATCH lore_enabled=false: %d %s", st, body)
	}
	if st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/search", "{}"); st != http.StatusForbidden {
		t.Fatalf("after switching back off: want 403, got %d %s", st, body)
	}
}

// TestTheSwitchIsDurableAndOwnerGated pins the two things 「誰能切」 was answered
// with: it rides the station's EXISTING settings surface (so the existing
// owner/admin floor applies — no new permission was invented for it), and the
// value survives a restart because it is written to the DB, not just to memory.
func TestTheSwitchIsDurableAndOwnerGated(t *testing.T) {
	url, dal, _, agentTok, ownerTok := loreToggleStack(t)

	// An ordinary agent cannot flip it — the same floor every other knob sits
	// behind. (This is a claim about the settings route, which is why it is
	// asserted through it rather than restated in the lore gate.)
	if st, _ := rosterREST(t, url, agentTok, "PATCH", "/api/settings",
		`{"lore_enabled": true}`); st != http.StatusForbidden {
		t.Errorf("an ordinary agent flipped the station's lore switch: %d", st)
	}

	if st, body := rosterREST(t, url, ownerTok, "PATCH", "/api/settings",
		`{"lore_enabled": true}`); st != http.StatusOK {
		t.Fatalf("owner PATCH: %d %s", st, body)
	}
	v, err := dal.GetSetting(settingLoreEnabled)
	if err != nil {
		t.Fatalf("read back the setting row: %v", err)
	}
	if v == nil || *v != "true" {
		t.Fatalf("the switch did not reach the DB (%v) — it would be forgotten on restart", v)
	}
	// The restart, simulated the only way that is honest: re-run the boot-time
	// loader over the same database.
	auth, err := loadAuthSettings(dal, Config{}, func(string) {})
	if err != nil {
		t.Fatalf("loadAuthSettings after the save: %v", err)
	}
	if !auth.loreEnabled {
		t.Fatal("the saved switch does not survive a restart")
	}
}

// TestSettingsSurfaceCarriesTheSwitchForTheCockpit — the UI row's server half.
// The cockpit decides whether to render the 傳承 tab from `lore_enabled` on
// GET /api/settings, so a settings payload that omits the field would leave the
// frontend guessing, and the safest guess (show it) is the wrong one.
func TestSettingsSurfaceCarriesTheSwitchForTheCockpit(t *testing.T) {
	url, _, _, _, ownerTok := loreToggleStack(t)

	st, body := rosterREST(t, url, ownerTok, "GET", "/api/settings", "")
	if st != http.StatusOK {
		t.Fatalf("GET /api/settings: %d %s", st, body)
	}
	if !strings.Contains(body, `"lore_enabled"`) {
		t.Fatalf("GET /api/settings does not carry lore_enabled: %s", body)
	}
	var view settingsDTO
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if view.LoreEnabled {
		t.Fatal("a fresh station reports lore_enabled=true — the cockpit would show the tab")
	}

	// Control: the field tracks the switch rather than being a hardcoded false.
	if st, body := rosterREST(t, url, ownerTok, "PATCH", "/api/settings",
		`{"lore_enabled": true}`); st != http.StatusOK {
		t.Fatalf("PATCH: %d %s", st, body)
	}
	st, body = rosterREST(t, url, ownerTok, "GET", "/api/settings", "")
	if st != http.StatusOK {
		t.Fatalf("GET /api/settings after the save: %d %s", st, body)
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if !view.LoreEnabled {
		t.Fatal("lore_enabled is hardcoded false — the cockpit could never show the tab")
	}
}

// TestTheSwitchMovesNoMemoryAnywhere is the guard for the misreading the ticket
// was most likely to be built on: 「fallback 到原本的 learning / lesson」 read as
// 「the station moves the memory across stores」.
//
// 🔴 IT ASSERTS AN ABSENCE OF BEHAVIOUR, WHICH IS WHY IT NEEDS A CONTROL. The
// controls here are both counts: the lore entries are all still there after the
// switch goes off (nothing was migrated away or deleted), and the lessons
// document is byte-identical (nothing was migrated INTO it). A transfer in
// either direction moves one of those two numbers.
func TestTheSwitchMovesNoMemoryAnywhere(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s)

	before, err := s.dal.ListLoreSubjectRoster("")
	if err != nil {
		t.Fatalf("roster before: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("control failed: the fixture filed no subjects, so the counts below say nothing")
	}
	// The lessons overlay is the store the owner's sentence names as the place an
	// agent falls back to. Seeded with a known text so a transfer INTO it would
	// have something to change.
	const lessonsKey = "r-lore-toggle"
	if err := s.dal.PutLessons(Lessons{RoleKey: lessonsKey,
		Text: "T-33 toggle fixture — untouched"}); err != nil {
		t.Fatalf("seed lessons: %v", err)
	}
	lessonsBefore, err := s.dal.GetLessons(lessonsKey)
	if err != nil || lessonsBefore == nil {
		t.Fatalf("lessons before: %v", err)
	}

	disableLoreForTest(s)
	// Exercise the paths a boot takes, so a transfer that hid inside one of them
	// would have run by now.
	if _, err := s.buildBootContext("", nil); err != nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	if _, _, err := s.foldLoreSectionWithSurfacing("m-anyone"); err != nil {
		t.Fatalf("fold: %v", err)
	}

	// Turning the switch back on must return the SAME directory, untouched.
	enableLoreForTest(s)
	after, err := s.dal.ListLoreSubjectRoster("")
	if err != nil {
		t.Fatalf("roster after: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("the directory changed size across an off/on cycle: %d → %d — "+
			"something moved entries, and nothing in this feature is allowed to",
			len(before), len(after))
	}
	lessonsAfter, err := s.dal.GetLessons(lessonsKey)
	if err != nil || lessonsAfter == nil {
		t.Fatalf("lessons after: %v", err)
	}
	if lessonsAfter.Text != lessonsBefore.Text {
		t.Error("the lessons document changed while the lore switch was off — " +
			"the station carried a memory across stores, which it must never do")
	}
}
