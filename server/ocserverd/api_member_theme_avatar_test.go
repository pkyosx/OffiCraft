package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The avatar contract this file guards is an ASSOCIATION, not a slot:
// member_theme_avatar(member_id, theme_id, icon_id) keyed on (member, theme).
// Every test below exists because the earlier single-index model could not
// express the case it names — one index resolved against whatever pool the
// active theme happened to have, so switching themes silently changed faces and
// collided members onto one image.

// icon builds a distinct, valid pool image. The byte differs per name, so each
// image gets its own derived id, which is what makes "removing one icon must
// not rebind a member to another" testable at all.
func icon(name string) string {
	raw := append([]byte{}, pngBytes...)
	raw = append(raw, name...)
	return dataURI("image/png", raw)
}

func pool(images ...string) []ThemeIconDTO {
	out := make([]ThemeIconDTO, 0, len(images))
	for _, img := range images {
		id := themeIconID(img)
		out = append(out, ThemeIconDTO{Id: &id, Image: img})
	}
	return out
}

// installThemes stores bundles in the theme table and points display.theme at
// one of them. Tests that need the theme WRITE path (and therefore the prune)
// go through the handler instead.
func installThemes(t *testing.T, s *apiServer, active string, bundles ...ThemeBundleDTO) {
	t.Helper()
	for _, bundle := range bundles {
		raw, err := json.Marshal(bundle)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.dal.PutCustomTheme(bundle.Id, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	s.settingsMu.Lock()
	s.displayTheme = active
	s.settingsMu.Unlock()
	s.invalidateAvatarSelections()
}

func themeWithPools(id string, member, outsource []ThemeIconDTO) ThemeBundleDTO {
	pools := map[string][]ThemeIconDTO{}
	if member != nil {
		pools["member"] = member
	}
	if outsource != nil {
		pools["outsource"] = outsource
	}
	return ThemeBundleDTO{
		Id: id, Name: id,
		Colors:      map[string]string{"--color-bg": "#000000"},
		AvatarPools: &pools,
	}
}

func putThemeAvatar(t *testing.T, s *apiServer, memberID, themeID, iconID string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleSetMemberThemeAvatarApiMembersMemberIdThemeAvatarPut(rec, taskReq(
		t, http.MethodPut, "/api/members/"+memberID+"/theme-avatar",
		map[string]any{"theme_id": themeID, "icon_id": iconID},
		wireOwnerID, "owner",
	), memberID)
	return rec
}

func activeIconID(t *testing.T, s *apiServer, memberID string) *string {
	t.Helper()
	m, err := s.dal.GetMember(memberID)
	if err != nil || m == nil {
		t.Fatalf("read member %s: member=%+v err=%v", memberID, m, err)
	}
	return s.memberAvatarIconID(m.ID, m.Kind)
}

func staffMember(t *testing.T, s *apiServer, id string) Member {
	t.Helper()
	m := Member{
		ID: id, Name: id, Kind: KindAssistant,
		Runtime: RuntimeClaude, RosterStatus: RosterStatusActive,
	}
	if err := s.dal.PutMember(m); err != nil {
		t.Fatal(err)
	}
	return m
}

// A choice in theme A and a choice in theme B are separate rows. This is the
// owner's headline requirement: switching to B and back must restore A's image,
// and must not overwrite B's.
func TestThemeAvatarChoicesArePerThemeAndSurviveSwitchingBack(t *testing.T) {
	s := newTasksTestServer(t)
	alpha, beta := icon("alpha-1"), icon("alpha-2")
	gammaIcon, deltaIcon := icon("beta-1"), icon("beta-2")
	installThemes(t, s, "alpha",
		themeWithPools("alpha", pool(alpha, beta), nil),
		themeWithPools("beta", pool(gammaIcon, deltaIcon), nil))
	m := staffMember(t, s, "m-per-theme")

	if rec := putThemeAvatar(t, s, m.ID, "alpha", themeIconID(beta)); rec.Code != http.StatusOK {
		t.Fatalf("choose in alpha: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := putThemeAvatar(t, s, m.ID, "beta", themeIconID(gammaIcon)); rec.Code != http.StatusOK {
		t.Fatalf("choose in beta: status=%d body=%s", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct{ theme, want string }{
		{"alpha", themeIconID(beta)},
		{"beta", themeIconID(gammaIcon)},
		{"alpha", themeIconID(beta)}, // switching back restores, never re-resolves
	} {
		s.settingsMu.Lock()
		s.displayTheme = tc.theme
		s.settingsMu.Unlock()
		s.invalidateAvatarSelections()
		got := activeIconID(t, s, m.ID)
		if got == nil || *got != tc.want {
			t.Fatalf("theme %s resolved %v, want %s", tc.theme, got, tc.want)
		}
	}
}

// First entry renders the pool's first image WITHOUT writing a row. The wire
// therefore sends null, and the absence of a row is what keeps "never chose"
// distinguishable from "chose the first image".
func TestThemeAvatarFirstVisitIsNotPersisted(t *testing.T) {
	s := newTasksTestServer(t)
	first, second := icon("first"), icon("second")
	installThemes(t, s, "alpha", themeWithPools("alpha", pool(first, second), nil))
	m := staffMember(t, s, "m-first-visit")

	if got := activeIconID(t, s, m.ID); got != nil {
		t.Fatalf("first visit put %v on the wire, want null", *got)
	}
	rows, err := s.dal.MemberThemeAvatars("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("first visit persisted a default: %+v", rows)
	}

	// An explicit pick of that SAME first image is a different fact, and it is
	// the one that gets stored.
	if rec := putThemeAvatar(t, s, m.ID, "alpha", themeIconID(first)); rec.Code != http.StatusOK {
		t.Fatalf("explicit pick: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := activeIconID(t, s, m.ID); got == nil || *got != themeIconID(first) {
		t.Fatalf("explicit pick did not persist: %v", got)
	}
}

// Removing one image must not move anybody else, and must not collide two
// members onto one face. Under the old index model both happened silently.
func TestRemovingAPoolIconOnlyClearsItsOwnSelections(t *testing.T) {
	s := newTasksTestServer(t)
	keep, drop := icon("keep"), icon("drop")
	installThemes(t, s, "alpha", themeWithPools("alpha", pool(keep, drop), nil))
	keeper := staffMember(t, s, "m-keeper")
	loser := staffMember(t, s, "m-loser")
	putThemeAvatar(t, s, keeper.ID, "alpha", themeIconID(keep))
	putThemeAvatar(t, s, loser.ID, "alpha", themeIconID(drop))

	// The theme write face is where the prune runs: it is the only path that
	// can remove a pool image.
	if rec := putTheme(t, s, "alpha", themeWithPools("alpha", pool(keep), nil)); rec.Code != http.StatusOK {
		t.Fatalf("theme write: status=%d body=%s", rec.Code, rec.Body.String())
	}

	if got := activeIconID(t, s, keeper.ID); got == nil || *got != themeIconID(keep) {
		t.Fatalf("untouched member moved: %v", got)
	}
	// The member whose image is gone falls back to the pool's first image,
	// which the wire expresses as null — NOT as somebody else's icon id.
	if got := activeIconID(t, s, loser.ID); got != nil {
		t.Fatalf("removed icon rebound the member to %s", *got)
	}
	rows, err := s.dal.MemberThemeAvatars("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, dangling := rows[loser.ID]; dangling {
		t.Fatalf("stale association survived the pool edit: %+v", rows)
	}
}

// Deleting a theme drops its rows and leaves every other theme's choices alone.
func TestDeletingAThemeDropsOnlyItsOwnSelections(t *testing.T) {
	s := newTasksTestServer(t)
	a1, b1 := icon("a1"), icon("b1")
	installThemes(t, s, "alpha",
		themeWithPools("alpha", pool(a1), nil),
		themeWithPools("beta", pool(b1), nil))
	m := staffMember(t, s, "m-theme-delete")
	putThemeAvatar(t, s, m.ID, "alpha", themeIconID(a1))
	putThemeAvatar(t, s, m.ID, "beta", themeIconID(b1))

	if rec := deleteTheme(t, s, "alpha"); rec.Code != http.StatusOK {
		t.Fatalf("theme delete: status=%d body=%s", rec.Code, rec.Body.String())
	}

	gone, err := s.dal.MemberThemeAvatars("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 0 {
		t.Fatalf("deleted theme left dangling selections: %+v", gone)
	}
	kept, err := s.dal.MemberThemeAvatars("beta")
	if err != nil {
		t.Fatal(err)
	}
	if kept[m.ID] != themeIconID(b1) {
		t.Fatalf("surviving theme lost its selection: %+v", kept)
	}
}

// An empty pool has nothing to point at, and a kind with no pool never had one.
// Both render the built-in glyph, which the wire expresses as null.
func TestEmptyAndMissingPoolsResolveToNull(t *testing.T) {
	s := newTasksTestServer(t)
	installThemes(t, s, "alpha", themeWithPools("alpha", pool(), nil))
	m := staffMember(t, s, "m-empty-pool")
	if got := activeIconID(t, s, m.ID); got != nil {
		t.Fatalf("empty pool resolved to %s, want null", *got)
	}

	warden := Member{
		ID: "mac-no-pool", Name: "Mac", Kind: KindWarden,
		Runtime: RuntimeClaude, RosterStatus: RosterStatusActive,
	}
	if err := s.dal.PutMember(warden); err != nil {
		t.Fatal(err)
	}
	if got := activeIconID(t, s, warden.ID); got != nil {
		t.Fatalf("a machine resolved an avatar: %s", *got)
	}
}

func TestThemeAvatarRejectsUnresolvableSelectionsAndMachines(t *testing.T) {
	s := newTasksTestServer(t)
	only := icon("only")
	installThemes(t, s, "alpha", themeWithPools("alpha", pool(only), nil))
	m := staffMember(t, s, "m-reject")
	warden := Member{
		ID: "mac-reject", Name: "Mac", Kind: KindWarden,
		Runtime: RuntimeClaude, RosterStatus: RosterStatusActive,
	}
	if err := s.dal.PutMember(warden); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, member, theme, iconID string
		want                        int
	}{
		{"unknown theme", m.ID, "nope", themeIconID(only), http.StatusUnprocessableEntity},
		{"icon not in pool", m.ID, "alpha", "icn-deadbeef", http.StatusUnprocessableEntity},
		{"blank icon", m.ID, "alpha", "", http.StatusUnprocessableEntity},
		{"machine target", warden.ID, "alpha", themeIconID(only), http.StatusUnprocessableEntity},
		{"unknown member", "m-missing", "alpha", themeIconID(only), http.StatusNotFound},
		{"valid", m.ID, "alpha", themeIconID(only), http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := putThemeAvatar(t, s, tc.member, tc.theme, tc.iconID)
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s, want %d", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}

func TestThemeAvatarAcceptsOutsourceWorkers(t *testing.T) {
	s := newTasksTestServer(t)
	face := icon("worker")
	installThemes(t, s, "alpha", themeWithPools("alpha", nil, pool(face)))
	worker := OutsourceWorker{
		ID: "ow-theme-avatar", Codename: "O-1", Runtime: RuntimeClaude,
		Status: WorkerStatusAssigned, DesiredState: DesiredStateOnline,
	}
	if err := s.dal.PutOutsourceWorker(worker); err != nil {
		t.Fatal(err)
	}
	if rec := putThemeAvatar(t, s, worker.ID, "alpha", themeIconID(face)); rec.Code != http.StatusOK {
		t.Fatalf("worker pick: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := activeIconID(t, s, worker.ID); got == nil || *got != themeIconID(face) {
		t.Fatalf("worker choice did not resolve: %v", got)
	}
}

// 🔴 The owner gate and the MCP exclusion are a ruling, not a default. See the
// note on HandleSetMemberThemeAvatar... in api_members.go before widening
// either: a member's face is how the owner tells the fleet apart, so an agent
// must not be able to change it.
func TestThemeAvatarRouteIsOwnerOnlyAndOffMCP(t *testing.T) {
	s := newTasksTestServer(t)
	for _, route := range specsFor(s) {
		if route.Path != "/api/members/{member_id}/theme-avatar" {
			continue
		}
		if route.Method != http.MethodPut || route.Requires != principalOwner || !route.MCPExclude {
			t.Fatalf("unexpected route: %+v", route)
		}
		return
	}
	t.Fatal("theme-avatar route missing")
}

// A dismissed member leaves no association behind.
func TestDismissedMemberLeavesNoSelection(t *testing.T) {
	s := newTasksTestServer(t)
	face := icon("dismissed")
	installThemes(t, s, "alpha", themeWithPools("alpha", pool(face), nil))
	m := staffMember(t, s, "m-dismissed")
	putThemeAvatar(t, s, m.ID, "alpha", themeIconID(face))

	rec := httptest.NewRecorder()
	s.HandleDismissMemberApiMembersMemberIdDelete(rec, taskReq(
		t, http.MethodDelete, "/api/members/"+m.ID, nil, wireOwnerID, "owner"), m.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rows, err := s.dal.MemberThemeAvatars("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, left := rows[m.ID]; left {
		t.Fatalf("dismissed member kept a selection: %+v", rows)
	}
}

// The identity is derived from the image bytes, so it is the SAME id after a
// theme is exported and imported elsewhere. That is what lets a recipient's own
// picks keep working, and what makes ids stable enough for a later storage
// migration to reuse.
func TestThemeIconIDIsDerivedFromTheImageAndSurvivesRoundTrip(t *testing.T) {
	one, two := icon("one"), icon("two")
	if themeIconID(one) == themeIconID(two) {
		t.Fatal("two different images share an id")
	}
	if themeIconID(one) != themeIconID(one) {
		t.Fatal("the same image produced two ids")
	}
	// A caller-supplied id is overwritten, never trusted: otherwise one image
	// could claim another image's selections.
	claimed := themeIconID(two)
	pools := map[string][]ThemeIconDTO{"member": {{Id: &claimed, Image: one}}}
	assignThemeIconIDs(&pools)
	if got := *pools["member"][0].Id; got != themeIconID(one) {
		t.Fatalf("caller id survived: %s", got)
	}
}

// A legacy singleton `avatars.member` still normalizes into a one-image pool,
// and that image gets a derived id like any other.
func TestLegacySingletonNormalizesIntoAPoolWithAnID(t *testing.T) {
	legacy := icon("legacy")
	avatars := map[string]string{"member": legacy}
	bundles := []ThemeBundleDTO{{
		Id: "alpha", Name: "alpha",
		Colors:  map[string]string{"--color-bg": "#000000"},
		Avatars: &avatars,
	}}
	if err := normalizeThemeBundles(bundles); err != nil {
		t.Fatal(err)
	}
	got := (*bundles[0].AvatarPools)["member"]
	if len(got) != 1 || got[0].Image != legacy || got[0].Id == nil ||
		*got[0].Id != themeIconID(legacy) {
		t.Fatalf("legacy singleton did not normalize with an id: %+v", got)
	}
}
