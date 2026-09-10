package main

// api_lore_settings_t33_test.go — T-33: the four 傳承 knobs, driven through the
// REAL settings faces.
//
// 🔴 The writes go through HandleUpdateSettingsApiSettingsPatch rather than by
// assigning the struct field, because the ticket's acceptance is "adjustable
// from the settings page" — and a test that sets the field proves nothing about
// the PATCH handler, the stored row, or the range check.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func patchLoreSetting(t *testing.T, api *apiServer, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateSettingsApiSettingsPatch(rec,
		taskReq(t, http.MethodPatch, "/api/settings", body, "owner", "owner"))
	return rec
}

func loreSettings(t *testing.T, api *apiServer) settingsDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetSettingsApiSettingsGet(rec,
		taskReq(t, http.MethodGet, "/api/settings", nil, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("get settings: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[settingsDTO](t, rec)
}

// TestLoreCapsShipTheOwnersNumbers pins the four defaults on all three faces:
// the accessor the folds read, the read face, and the shipped constant.
//
// The two entry numbers are the ones the owner set himself on 2026-09-07 (title
// 140→80, body 1000→500), so they are written out literally here rather than
// referred to by their constant — a test that says `loreTitleCapCharsDefault`
// stays green when somebody edits that constant, which is the one thing this
// test exists to notice.
func TestLoreCapsShipTheOwnersNumbers(t *testing.T) {
	api := newTasksTestServer(t)

	if got := api.loreRoleCap(); got != 10000 {
		t.Fatalf("loreRoleCap() = %d, want 10000", got)
	}
	if got := api.loreManualCap(); got != 10000 {
		t.Fatalf("loreManualCap() = %d, want 10000", got)
	}
	if got := api.loreTitleCap(); got != 80 {
		t.Fatalf("loreTitleCap() = %d, want 80", got)
	}
	if got := api.loreBodyCap(); got != 500 {
		t.Fatalf("loreBodyCap() = %d, want 500", got)
	}

	dto := loreSettings(t, api)
	if dto.LoreCapCharsRole != 10000 || dto.LoreCapCharsManual != 10000 ||
		dto.LoreCapCharsTitle != 80 || dto.LoreCapCharsBody != 500 {
		t.Fatalf("GET /api/settings reports %d/%d/%d/%d, want 10000/10000/80/500",
			dto.LoreCapCharsRole, dto.LoreCapCharsManual,
			dto.LoreCapCharsTitle, dto.LoreCapCharsBody)
	}
}

// TestLoreCapsMayBeLOWERED is the difference from every doc.cap_chars.* knob,
// and it is the assertion that would go red if these four were filed under that
// prefix and inherited its 只能調高 floor rule.
//
// The values are BELOW the shipped defaults on purpose — that is exactly what a
// doc-cap floor would refuse, and the owner lowered two of these himself the day
// they shipped.
func TestLoreCapsMayBeLOWERED(t *testing.T) {
	api := newTasksTestServer(t)

	rec := patchLoreSetting(t, api, map[string]any{
		"lore_cap_chars_role":   500,
		"lore_cap_chars_manual": 600,
		"lore_cap_chars_title":  40,
		"lore_cap_chars_body":   200,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("lowering the 傳承 caps must be allowed, got %d %s — these four do "+
			"NOT carry the doc-cap 只能調高 rule: an entry has no edit path, so a "+
			"lower cap cannot strand anything already stored",
			rec.Code, rec.Body.String())
	}
	if got := api.loreRoleCap(); got != 500 {
		t.Fatalf("loreRoleCap() = %d after the PATCH, want 500 — the accessor must "+
			"read the live value with no restart", got)
	}
	if got := api.loreTitleCap(); got != 40 {
		t.Fatalf("loreTitleCap() = %d after the PATCH, want 40", got)
	}
	dto := loreSettings(t, api)
	if dto.LoreCapCharsManual != 600 || dto.LoreCapCharsBody != 200 {
		t.Fatalf("read face reports %d/%d, want 600/200",
			dto.LoreCapCharsManual, dto.LoreCapCharsBody)
	}
}

// TestLoreCapsRefuseOutOfRange — "adjustable in both directions" is not "any
// number". Zero in particular is refused: selectLoreForScope reads a zero cap
// as NO ROOM, so a knob that could be set to zero would silently switch the
// whole feature off with no way for the settings page to explain it.
func TestLoreCapsRefuseOutOfRange(t *testing.T) {
	api := newTasksTestServer(t)
	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"role zero", map[string]any{"lore_cap_chars_role": 0}},
		{"manual negative", map[string]any{"lore_cap_chars_manual": -1}},
		{"title zero", map[string]any{"lore_cap_chars_title": 0}},
		{"body over ceiling", map[string]any{"lore_cap_chars_body": 10001}},
	} {
		rec := patchLoreSetting(t, api, tc.body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: want 422, got %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}
	// Nothing moved.
	dto := loreSettings(t, api)
	if dto.LoreCapCharsRole != 10000 || dto.LoreCapCharsManual != 10000 ||
		dto.LoreCapCharsTitle != 80 || dto.LoreCapCharsBody != 500 {
		t.Fatalf("a refused PATCH moved a value: %d/%d/%d/%d",
			dto.LoreCapCharsRole, dto.LoreCapCharsManual,
			dto.LoreCapCharsTitle, dto.LoreCapCharsBody)
	}
}

// TestLoreFoldCapsAreIndependent — the two fold budgets are never one number.
// Setting one must leave the other exactly where it was.
func TestLoreFoldCapsAreIndependent(t *testing.T) {
	api := newTasksTestServer(t)
	if rec := patchLoreSetting(t, api,
		map[string]any{"lore_cap_chars_role": 1234}); rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if got := api.loreRoleCap(); got != 1234 {
		t.Fatalf("loreRoleCap() = %d, want 1234", got)
	}
	if got := api.loreManualCap(); got != 10000 {
		t.Fatalf("loreManualCap() = %d, want its own untouched 10000 — the two "+
			"budgets are independent and are never summed or shared", got)
	}
}
