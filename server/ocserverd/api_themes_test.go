package main

// api_themes_test.go — T-83ef. Handler-level guards for the per-theme endpoints.
//
// Route-table authz (401/403) is NOT asserted here; it is pinned by the
// conformance auth matrix, which drives the real mux. These tests invoke the
// handlers directly and are about the semantics above that gate.
//
// 🔴 THE DISCIPLINE THIS FILE IS WRITTEN UNDER, because this ticket has already
// shipped five decorative guards: for every rule under test the input must be
// legal in EVERY OTHER respect, so the assertion cannot be satisfied by an
// earlier check firing first. Where that is not achievable the test says what it
// actually pins.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// aTestBundle builds a minimally legal bundle: one real colour token, a
// one-character name, an id that matches the slug rule. Callers break exactly
// one thing.
func aTestBundle(id, name string) map[string]any {
	return map[string]any{
		"id": id, "name": name,
		"colors": map[string]any{"--color-bg": "#eef0dc"},
	}
}

func putTheme(t *testing.T, api *apiServer, id string, body any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandlePutThemeApiThemesThemeIdPut(rec,
		taskReq(t, http.MethodPut, "/api/themes/"+id, body, "owner", "owner"), id)
	return rec
}

func listThemes(t *testing.T, api *apiServer) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListThemesApiThemesGet(rec,
		taskReq(t, http.MethodGet, "/api/themes", nil, "owner", "owner"))
	return rec
}

func getTheme(t *testing.T, api *apiServer, id string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetThemeApiThemesThemeIdGet(rec,
		taskReq(t, http.MethodGet, "/api/themes/"+id, nil, "owner", "owner"), id)
	return rec
}

func deleteTheme(t *testing.T, api *apiServer, id string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleDeleteThemeApiThemesThemeIdDelete(rec,
		taskReq(t, http.MethodDelete, "/api/themes/"+id, nil, "owner", "owner"), id)
	return rec
}

// TestThemeCRUDRoundTrip is the positive control the refusal tests below lean
// on: without it, a handler that refused everything would satisfy every one of
// them.
func TestThemeCRUDRoundTrip(t *testing.T) {
	api := newTasksTestServer(t)

	rec := putTheme(t, api, "night-walk", aTestBundle("night-walk", "夜行"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create must 200, got %d %s", rec.Code, rec.Body.String())
	}
	receipt := decodeBody[map[string]any](t, rec)
	if receipt["created"] != true {
		t.Fatalf("a theme that did not exist must report created=true: %v", receipt)
	}

	got := decodeBody[map[string]any](t, getTheme(t, api, "night-walk"))
	if got["name"] != "夜行" {
		t.Fatalf("the theme must read back as written: %v", got)
	}

	if rec := deleteTheme(t, api, "night-walk"); rec.Code != http.StatusOK {
		t.Fatalf("delete must 200, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := getTheme(t, api, "night-walk"); rec.Code != http.StatusNotFound {
		t.Fatalf("a deleted theme must 404, got %d", rec.Code)
	}
}

// TestThemeListCarriesOnlyIdAndName is the guard on the owner's 2026-08-18
// ruling that the list returns the title and the little the UI shows. It is
// written as "no other field is present" rather than "id and name are present",
// because the second passes just as happily on a list of whole bundles — which
// is the exact regression worth catching, since serving the bundles is both the
// obvious implementation and the thing this endpoint exists not to do.
func TestThemeListCarriesOnlyIdAndName(t *testing.T) {
	api := newTasksTestServer(t)
	// A bundle carrying MORE than the minimum, so "no other field" is a claim
	// with something to be wrong about: if the handler served bundles, colors
	// would be here.
	b := aTestBundle("night-walk", "夜行")
	b["fonts"] = map[string]any{
		"--font-sans": `system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`,
	}
	if rec := putTheme(t, api, "night-walk", b); rec.Code != http.StatusOK {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}

	rows := decodeBody[[]map[string]any](t, listThemes(t, api))
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0]["id"] != "night-walk" || rows[0]["name"] != "夜行" {
		t.Fatalf("the row must carry id and name: %v", rows[0])
	}
	for _, banned := range []string{"colors", "fonts", "wording", "avatars", "logo", "navIcons", "backgrounds", "backgroundModes"} {
		if _, present := rows[0][banned]; present {
			t.Fatalf("the list must not carry %q — it exists to stop serving whole "+
				"bundles, and a bundle is hundreds of KB: %v", banned, rows[0])
		}
	}
}

// TestThemePutRefusesAnIdThatDisagreesWithThePath.
//
// The bundle is legal in EVERY other respect — real token, legal name, legal
// slug id — so the only thing that can produce the refusal is the mismatch. And
// the write must not have happened under EITHER id: "filed under the other one"
// is the silent outcome this rule exists to prevent, so asserting the 422 alone
// would not distinguish it from a refusal that still wrote.
//
// ⚠️ WHICH LAYER THIS ACTUALLY PINS, measured rather than assumed. When the
// handler carried its own body.Id != themeID check as well, this test passed
// with that check deleted and only went red once the DAL check went too — i.e.
// it never had the power to tell the two apart, and the handler copy was
// decorative. The handler copy is gone (see HandlePutThemeApiThemesThemeIdPut);
// what this test pins is the one check that a mutant can kill:
// checkCustomThemeIDMatchesBundle.
func TestThemePutRefusesAnIdThatDisagreesWithThePath(t *testing.T) {
	api := newTasksTestServer(t)
	rec := putTheme(t, api, "night-walk", aTestBundle("day-walk", "夜行"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a mismatched id must 422, got %d %s", rec.Code, rec.Body.String())
	}
	for _, id := range []string{"night-walk", "day-walk"} {
		if row, err := api.dal.GetCustomTheme(id); err != nil || row != nil {
			t.Fatalf("a refused write must store nothing, found a row under %q: %+v", id, row)
		}
	}
}

// TestThemePutRefusesAnIllegalBundleAndWritesNothing proves the shared validator
// is actually wired into this path.
//
// ⚠️ WHAT THIS PINS, stated honestly: that SOME rule of validateThemeBundle
// reaches this handler. It is not a re-test of the grammar — that lives in
// theme_bundle_test.go and is not duplicated here, because two copies of the
// same table drift. The colour value is the probe; the id and name are legal, so
// the earlier checks cannot be what fires.
func TestThemePutRefusesAnIllegalBundleAndWritesNothing(t *testing.T) {
	api := newTasksTestServer(t)
	b := aTestBundle("night-walk", "夜行")
	b["colors"] = map[string]any{"--color-bg": "url(javascript:alert(1))"}
	rec := putTheme(t, api, "night-walk", b)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an illegal colour value must 422, got %d %s", rec.Code, rec.Body.String())
	}
	if row, _ := api.dal.GetCustomTheme("night-walk"); row != nil {
		t.Fatalf("a refused write must store nothing: %+v", row)
	}
}

// TestThemeReplaceKeepsItsPlaceInTheList pins the claim PutCustomTheme's comment
// makes and that the receipt reports: re-colouring a theme does not move it to
// the bottom of the owner's list.
//
// 🔴 THE FIXTURE HAS THREE THEMES AND EDITS THE MIDDLE ONE. With two, "kept its
// place" and "moved to the end" can look the same at a glance; with the middle
// one, an append-on-replace implementation reorders the list visibly.
func TestThemeReplaceKeepsItsPlaceInTheList(t *testing.T) {
	api := newTasksTestServer(t)
	// 🔴 The receipt's own order_idx is asserted here, not just the list order
	// below. wire.go says every field on the receipt is "something the caller
	// cannot already know" and that OrderIdx is the place a replace KEEPS —
	// but only `created` had a test. Replacing the whole read-back with zeroes
	// left every case green, so those two fields could ship as constants and
	// nothing would say a word. The list order and the receipt are two separate
	// promises to the caller; a client that trusted the receipt (rather than
	// re-listing) is exactly who the untested one would have failed.
	seeded := map[string]float64{}
	for i, id := range []string{"aaa", "bbb", "ccc"} {
		rec := putTheme(t, api, id, aTestBundle(id, id))
		if rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", id, rec.Code, rec.Body.String())
		}
		r := decodeBody[map[string]any](t, rec)
		idx, ok := r["order_idx"].(float64)
		if !ok || int(idx) != i {
			t.Fatalf("a create must report the place it was appended to: want %d, receipt %v", i, r)
		}
		if ts, ok := r["updated_at"].(float64); !ok || ts <= 0 {
			t.Fatalf("the receipt must carry a real write time (the caller cannot know it): %v", r)
		}
		seeded[id] = idx
	}
	rec := putTheme(t, api, "bbb", aTestBundle("bbb", "renamed"))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace must 200, got %d %s", rec.Code, rec.Body.String())
	}
	receipt := decodeBody[map[string]any](t, rec)
	if receipt["created"] != false {
		t.Fatalf("replacing an existing theme must report created=false: %v", receipt)
	}
	if idx, ok := receipt["order_idx"].(float64); !ok || idx != seeded["bbb"] {
		t.Fatalf("a replace must keep its place — re-colouring a theme may not move it to the bottom.\n want order_idx %v, receipt %v", seeded["bbb"], receipt)
	}

	rows := decodeBody[[]map[string]any](t, listThemes(t, api))
	var order []string
	for _, r := range rows {
		order = append(order, fmt.Sprint(r["id"]))
	}
	want := []string{"aaa", "bbb", "ccc"}
	for i := range want {
		if i >= len(order) || order[i] != want[i] {
			t.Fatalf("a replace must not reorder the list: got %v, want %v", order, want)
		}
	}
	if rows[1]["name"] != "renamed" {
		t.Fatalf("the replace must have landed: %v", rows[1])
	}
}

// TestThemeDeleteResetsTheActiveThemeAndSaysSo covers the coupling that used to
// live in the settings write, in BOTH directions — a reset that always fired
// would satisfy the first case alone.
func TestThemeDeleteResetsTheActiveThemeAndSaysSo(t *testing.T) {
	api := newTasksTestServer(t)
	for _, id := range []string{"active-one", "other-one"} {
		if rec := putTheme(t, api, id, aTestBundle(id, id)); rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", id, rec.Code, rec.Body.String())
		}
	}
	if err := api.dal.PutSetting(settingDisplayTheme, "active-one"); err != nil {
		t.Fatal(err)
	}
	api.settingsMu.Lock()
	api.displayTheme = "active-one"
	api.settingsMu.Unlock()

	// Deleting a theme that is NOT active must leave the active one alone.
	got := decodeBody[map[string]any](t, deleteTheme(t, api, "other-one"))
	if got["display_theme_reset"] != false {
		t.Fatalf("deleting a non-active theme must not reset display_theme: %v", got)
	}
	if v, _ := api.dal.GetSetting(settingDisplayTheme); v == nil || *v != "active-one" {
		t.Fatalf("display_theme must be untouched, got %v", v)
	}

	// Deleting the ACTIVE one resets it, says so, and the reset is durable.
	got = decodeBody[map[string]any](t, deleteTheme(t, api, "active-one"))
	if got["display_theme_reset"] != true {
		t.Fatalf("deleting the active theme must report the reset: %v", got)
	}
	if v, _ := api.dal.GetSetting(settingDisplayTheme); v == nil || *v != "" {
		t.Fatalf("display_theme must be reset in the database, got %v", v)
	}
}

func TestThemeDeleteOfAnUnknownIdIs404(t *testing.T) {
	api := newTasksTestServer(t)
	if rec := deleteTheme(t, api, "never-existed"); rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestStoredThemeReadPrunesRetiredWordingCodes is TestLoadAuthSettingsCustomThemes
// MOVED (T-83ef), not a new claim: a bundle stored before the message-key
// whitelist shrank must not serve its retired codes back. The behaviour left the
// settings loader with the themes, so its guard follows it to the read path that
// serves them now.
//
// It seeds the ROW DIRECTLY rather than writing through PUT, on purpose: the
// write path prunes too, so a seed through the endpoint could not tell "the read
// prunes" from "the write already had".
func TestStoredThemeReadPrunesRetiredWordingCodes(t *testing.T) {
	api := newTasksTestServer(t)
	code := aMessageKey(t)
	stored := `{"id":"smurf-village","name":"精靈村",` +
		`"colors":{"--color-bg":"#eef0dc"},` +
		`"wording":{"zh":{"` + code + `":"文字","profile.themeOffice":"精靈村"}}}`
	if err := api.dal.PutCustomTheme("smurf-village", stored); err != nil {
		t.Fatal(err)
	}

	got := decodeBody[map[string]any](t, getTheme(t, api, "smurf-village"))
	wording, _ := got["wording"].(map[string]any)
	zh, _ := wording["zh"].(map[string]any)
	if _, present := zh["profile.themeOffice"]; present {
		t.Fatalf("a retired wording code must be pruned on read: %v", zh)
	}
	if zh[code] != "文字" {
		t.Fatalf("the live wording codes must survive the read prune: %v", zh)
	}
	colors, _ := got["colors"].(map[string]any)
	if colors["--color-bg"] != "#eef0dc" {
		t.Fatalf("the rest of the bundle must read back untouched: %v", got)
	}
}

// TestDisplayThemeIsValidatedAgainstTheTable pins the seam that replaced
// themeBundleIDSet: settings no longer carries the bundles, so "is this a real
// theme id" is now a question for the custom_theme table.
//
// Both directions, and the accepted case uses a theme that exists ONLY in the
// table — never in any settings payload — which is what makes this a test of the
// table lookup rather than of anything left over.
func TestDisplayThemeIsValidatedAgainstTheTable(t *testing.T) {
	api := newTasksTestServer(t)
	if rec := putTheme(t, api, "night-walk", aTestBundle("night-walk", "夜行")); rec.Code != http.StatusOK {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}

	rec := httptest.NewRecorder()
	api.HandleUpdateSettingsApiSettingsPatch(rec, taskReq(t, http.MethodPatch,
		"/api/settings", map[string]any{"display_theme": "night-walk"}, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("an existing custom theme must be selectable: %d %s", rec.Code, rec.Body.String())
	}
	if v, _ := api.dal.GetSetting(settingDisplayTheme); v == nil || *v != "night-walk" {
		t.Fatalf("display_theme must have been written, got %v", v)
	}

	rec = httptest.NewRecorder()
	api.HandleUpdateSettingsApiSettingsPatch(rec, taskReq(t, http.MethodPatch,
		"/api/settings", map[string]any{"display_theme": "no-such-theme"}, "owner", "owner"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an id with no row must 422, got %d %s", rec.Code, rec.Body.String())
	}
	if v, _ := api.dal.GetSetting(settingDisplayTheme); v == nil || *v != "night-walk" {
		t.Fatalf("a refused patch must not move display_theme, got %v", v)
	}
}

// TestSettingsNoLongerCarriesCustomThemes is the guard on the breaking half of
// this ticket: the field is gone from BOTH faces. Without it, a later change
// that re-added the field to the response would silently undo the reason the
// split was done — the settings payload being unreadable.
func TestSettingsNoLongerCarriesCustomThemes(t *testing.T) {
	api := newTasksTestServer(t)

	read := httptest.NewRecorder()
	api.HandleGetSettingsApiSettingsGet(read,
		taskReq(t, http.MethodGet, "/api/settings", nil, "owner", "owner"))
	if read.Code != http.StatusOK {
		t.Fatalf("read: %d %s", read.Code, read.Body.String())
	}
	var view map[string]any
	if err := json.Unmarshal(read.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if _, present := view["custom_themes"]; present {
		t.Fatalf("GET /api/settings must not carry custom_themes any more: %v", view)
	}

	// The write face refuses it as an unknown field rather than ignoring it: a
	// client still sending the old shape has to find out, not have its themes
	// silently dropped.
	write := httptest.NewRecorder()
	api.HandleUpdateSettingsApiSettingsPatch(write, taskReq(t, http.MethodPatch,
		"/api/settings", map[string]any{"custom_themes": []any{}}, "owner", "owner"))
	if write.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH with custom_themes must 422, got %d %s", write.Code, write.Body.String())
	}
}

// TestThemeCapBoundsCreatesButNotReplaces covers the rule whose MEANING changed
// in this ticket. maxCustomThemes used to bound the LENGTH OF AN ARRAY arriving
// in one request; there is no array any more, so it bounds the number of ROWS
// and is asked of the table.
//
// 🔴 THE SECOND HALF IS THE POINT AND IT IS THE HALF AN OBVIOUS IMPLEMENTATION
// GETS WRONG. Checking the count on every write — the one-line version — would
// refuse to re-save a theme once the owner is at the limit, i.e. an owner with a
// full set could no longer EDIT anything, only delete. The cap is about how many
// are kept, so it constrains creates alone.
func TestThemeCapBoundsCreatesButNotReplaces(t *testing.T) {
	api := newTasksTestServer(t)
	for i := 0; i < maxCustomThemes; i++ {
		id := fmt.Sprintf("theme-%03d", i)
		if rec := putTheme(t, api, id, aTestBundle(id, id)); rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", id, rec.Code, rec.Body.String())
		}
	}
	if n, _ := api.dal.CountCustomThemes(); n != maxCustomThemes {
		t.Fatalf("seeded %d rows, want %d", n, maxCustomThemes)
	}

	// One more NEW theme is refused — and the bundle is legal in every other
	// respect, so nothing but the cap can be what refuses it.
	rec := putTheme(t, api, "one-too-many", aTestBundle("one-too-many", "one too many"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("creating past the cap must 422, got %d %s", rec.Code, rec.Body.String())
	}
	if row, _ := api.dal.GetCustomTheme("one-too-many"); row != nil {
		t.Fatalf("a capped-out create must store nothing: %+v", row)
	}

	// …while REPLACING one of the existing themes still works at the cap.
	if rec := putTheme(t, api, "theme-000", aTestBundle("theme-000", "renamed at the cap")); rec.Code != http.StatusOK {
		t.Fatalf("replacing at the cap must still 200, got %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[map[string]any](t, getTheme(t, api, "theme-000"))
	if got["name"] != "renamed at the cap" {
		t.Fatalf("the replace must have landed: %v", got)
	}
}

// TestThemeWriteRoundTripsAWordingOverlay and the two tests after it are the
// wording assertions MOVED from TestDisplayPrefsSettingRoundTrips (T-83ef).
// Their subject did not change — only the door it is reached through.
func TestThemeWriteRoundTripsAWordingOverlay(t *testing.T) {
	api := newTasksTestServer(t)
	b := aTestBundle("worded", "Worded")
	b["wording"] = map[string]any{
		"zh": map[string]any{"nav.tasks": "待辦"},
		"en": map[string]any{"nav.office": "Office Mode"},
	}
	if rec := putTheme(t, api, "worded", b); rec.Code != http.StatusOK {
		t.Fatalf("a legal wording overlay must 200: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[map[string]any](t, getTheme(t, api, "worded"))
	wording, _ := got["wording"].(map[string]any)
	zh, _ := wording["zh"].(map[string]any)
	en, _ := wording["en"].(map[string]any)
	if zh["nav.tasks"] != "待辦" || en["nav.office"] != "Office Mode" {
		t.Fatalf("the overlay must round-trip in both languages: %v", wording)
	}
}

// TestThemeWritePrunesUnknownWordingCodes is the WRITE half of the prune, and it
// is a different claim from the read half next door: this one says the request
// SUCCEEDS (owner ruling 2026-07-27 — a theme exported from a newer build must
// still import) while the unknown code is dropped, and that the dropped code
// never reaches storage.
//
// 🔴 THE KNOWN CODE BESIDE IT IS LOAD-BEARING. Without a live code in the same
// language map, "the overlay was pruned correctly" and "the whole overlay was
// thrown away" produce the same observation.
func TestThemeWritePrunesUnknownWordingCodes(t *testing.T) {
	api := newTasksTestServer(t)
	b := aTestBundle("worded", "Worded")
	b["wording"] = map[string]any{"zh": map[string]any{
		"nav.tasks":           "待辦",
		"profile.themeOffice": "精靈村",
		"not.a.real.key":      "x",
	}}
	if rec := putTheme(t, api, "worded", b); rec.Code != http.StatusOK {
		t.Fatalf("an unknown wording code must not fail the bundle: %d %s",
			rec.Code, rec.Body.String())
	}

	row, err := api.dal.GetCustomTheme("worded")
	if err != nil || row == nil {
		t.Fatalf("the theme must be stored: %v %v", row, err)
	}
	for _, dropped := range []string{"profile.themeOffice", "not.a.real.key"} {
		if strings.Contains(row.Bundle, dropped) {
			t.Fatalf("the unknown wording code %q must not be STORED: %s", dropped, row.Bundle)
		}
	}
	if !strings.Contains(row.Bundle, "待辦") {
		t.Fatalf("the known wording code must survive the prune: %s", row.Bundle)
	}
}

// TestThemeWriteRefusesEveryIllegalWordingOverlay is the wording refusal matrix,
// moved verbatim in substance. Each case breaks exactly ONE rule and is legal in
// every other respect, and none of them may leave anything behind.
func TestThemeWriteRefusesEveryIllegalWordingOverlay(t *testing.T) {
	api := newTasksTestServer(t)

	// POSITIVE CONTROL: the same fixture with a LEGAL overlay lands. Without it a
	// handler that refused every write would satisfy the whole table below.
	control := aTestBundle("w2", "W2")
	control["wording"] = map[string]any{"zh": map[string]any{"nav.tasks": "待辦"}}
	if rec := putTheme(t, api, "w2", control); rec.Code != http.StatusOK {
		t.Fatalf("control: a legal overlay must land: %d %s", rec.Code, rec.Body.String())
	}
	if rec := deleteTheme(t, api, "w2"); rec.Code != http.StatusOK {
		t.Fatalf("control cleanup: %d %s", rec.Code, rec.Body.String())
	}

	junk := map[string]any{}
	for i := 0; i <= maxWordingEntriesPerLang; i++ {
		junk[fmt.Sprintf("junk.key.%d", i)] = "x"
	}
	for name, wording := range map[string]any{
		"a language outside {zh,en}":  map[string]any{"xian": map[string]any{"nav.tasks": "仙"}},
		"a value over the rune cap":   map[string]any{"zh": map[string]any{"nav.tasks": strings.Repeat("字", 201)}},
		"a value with a control char": map[string]any{"zh": map[string]any{"nav.tasks": "a\nb"}},
		"a value empty after trim":    map[string]any{"zh": map[string]any{"nav.tasks": "   "}},
		"over the per-language cap":   map[string]any{"zh": junk},
	} {
		b := aTestBundle("w2", "W2")
		b["wording"] = wording
		rec := putTheme(t, api, "w2", b)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s must 422: got %d %s", name, rec.Code, rec.Body.String())
		}
		if row, _ := api.dal.GetCustomTheme("w2"); row != nil {
			t.Fatalf("%s must write nothing, found: %+v", name, row)
		}
	}
}
