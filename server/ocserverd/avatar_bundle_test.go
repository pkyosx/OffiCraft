package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// dataURI packs raw bytes into a `data:<mime>;base64,<...>` URI.
func dataURI(mime string, raw []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

// Minimal byte payloads that begin with each format's magic bytes (the
// validator checks magic bytes + size, not full image structure).
var (
	pngBytes  = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01, 0x02, 0x03}
	jpegBytes = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46}
	webpBytes = []byte{'R', 'I', 'F', 'F', 0x10, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 0x00}
)

func TestValidAvatarValue_Accepts(t *testing.T) {
	for _, tc := range []struct {
		name string
		mime string
		raw  []byte
	}{
		{"png", "image/png", pngBytes},
		{"jpeg", "image/jpeg", jpegBytes},
		{"webp", "image/webp", webpBytes},
	} {
		if err := validAvatarValue(dataURI(tc.mime, tc.raw)); err != nil {
			t.Fatalf("%s: legal avatar must pass: %v", tc.name, err)
		}
	}
}

func TestValidAvatarValue_Rejects(t *testing.T) {
	big := make([]byte, maxAvatarBytes+1)
	copy(big, pngBytes)

	cases := []struct {
		name   string
		value  string
		expect string // substring the error must contain
	}{
		{"empty", "", "empty"},
		{"not a data URI", "https://evil/x.png", "data: URI"},
		{"plain text data URI", "data:text/html,<script>alert(1)</script>", "base64"},
		{"svg is rejected", dataURI("image/svg+xml", []byte(`<svg onload="alert(1)"/>`)), "not an allowed image type"},
		{"text/html base64", dataURI("text/html", []byte("<script>alert(1)</script>")), "not an allowed image type"},
		{"gif not whitelisted", dataURI("image/gif", []byte{'G', 'I', 'F', '8'}), "not an allowed image type"},
		{"javascript scheme", "javascript:alert(1)", "data: URI"},
		{"bad base64", "data:image/png;base64,!!!!not-base64!!!!", "invalid base64"},
		{"png magic mismatch (declares png, carries jpeg bytes)", dataURI("image/png", jpegBytes), "magic-byte"},
		{"png declared, svg payload", dataURI("image/png", []byte(`<svg onload=alert(1)>`)), "magic-byte"},
		{"jpeg magic mismatch", dataURI("image/jpeg", pngBytes), "magic-byte"},
		{"webp missing WEBP tag", dataURI("image/webp", []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'X', 'X', 'X', 'X'}), "magic-byte"},
		{"oversize decoded", dataURI("image/png", big), "too large"},
		{"missing ;base64", "data:image/png,iVBOR", "base64-encoded"},
	}
	for _, tc := range cases {
		err := validAvatarValue(tc.value)
		if err == nil {
			t.Fatalf("%s: expected rejection, got nil", tc.name)
		}
		if !strings.Contains(err.Error(), tc.expect) {
			t.Fatalf("%s: error %q must contain %q", tc.name, err.Error(), tc.expect)
		}
	}
}

// TestValidAvatarValue_RejectsWhitespaceBase64 pins the double-ended-symmetry
// fix: Go's base64 decoder silently skips \n/\r/space, but the client's strict
// regex rejects them — so the server MUST also reject a payload carrying
// whitespace, or the same value would be accepted online and rejected offline.
func TestValidAvatarValue_RejectsWhitespaceBase64(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString(pngBytes)
	// Inject a newline mid-payload (what a PEM-style / wrapped base64 carries).
	mid := len(enc) / 2
	for _, ws := range []string{"\n", "\r", "\r\n", " ", "\t"} {
		wrapped := "data:image/png;base64," + enc[:mid] + ws + enc[mid:]
		if err := validAvatarValue(wrapped); err == nil ||
			!strings.Contains(err.Error(), "invalid base64") {
			t.Fatalf("base64 with %q must be rejected (client parity): %v", ws, err)
		}
	}
	// Sanity: the same payload WITHOUT the injected whitespace is accepted, so
	// the rejection above is due to the whitespace, not the image.
	if err := validAvatarValue("data:image/png;base64," + enc); err != nil {
		t.Fatalf("clean base64 must still pass: %v", err)
	}
}

func TestValidAvatarValue_OverlongString(t *testing.T) {
	// A string longer than the cap is rejected BEFORE decoding.
	v := "data:image/png;base64," + strings.Repeat("A", maxAvatarValueLen+4)
	if err := validAvatarValue(v); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("overlong data URI must be rejected pre-decode: %v", err)
	}
}

func TestValidateAvatars(t *testing.T) {
	// nil overlay is admissible (avatars is optional).
	if err := validateAvatars(nil, "t"); err != nil {
		t.Fatalf("nil avatars must be admissible: %v", err)
	}

	// Canonical single-image identities remain owner and assistant.
	ok := map[string]string{
		"owner":     dataURI("image/jpeg", jpegBytes),
		"assistant": dataURI("image/png", pngBytes),
	}
	if err := validateAvatars(&ok, "t"); err != nil {
		t.Fatalf("legal avatars overlay must pass: %v", err)
	}

	// An unknown kind key is rejected, and the message names the canonical set.
	badKind := map[string]string{"boss": dataURI("image/png", pngBytes)}
	if err := validateAvatars(&badKind, "t"); err == nil ||
		!strings.Contains(err.Error(), "only owner, assistant") {
		t.Fatalf("unknown avatar kind must 422 naming the kind set: %v", err)
	}

	// A bad value under a legal kind is rejected with the locator.
	badVal := map[string]string{"assistant": dataURI("image/svg+xml", []byte("<svg/>"))}
	if err := validateAvatars(&badVal, "cx[0]"); err == nil ||
		!strings.Contains(err.Error(), "cx[0]: avatars[assistant]") {
		t.Fatalf("bad avatar value must 422 with locator: %v", err)
	}
}

func TestNormalizeAndValidateAvatarPools(t *testing.T) {
	memberImage := dataURI("image/png", pngBytes)
	legacy := map[string]string{
		"member": memberImage,
		"owner":  dataURI("image/jpeg", jpegBytes),
	}
	bundle := ThemeBundleDTO{Avatars: &legacy}
	if err := normalizeThemeAvatarPools(&bundle, "theme"); err != nil {
		t.Fatal(err)
	}
	if bundle.AvatarPools == nil || len((*bundle.AvatarPools)["member"]) != 1 ||
		(*bundle.AvatarPools)["member"][0].Image != memberImage {
		t.Fatalf("legacy member image was not normalized: %+v", bundle.AvatarPools)
	}
	// The normalized item carries the derived identity a member's selection
	// points at, so a legacy bundle is selectable without a data migration.
	if id := (*bundle.AvatarPools)["member"][0].Id; id == nil || *id != themeIconID(memberImage) {
		t.Fatalf("normalized legacy image has no stable id: %+v", id)
	}
	if bundle.Avatars == nil || (*bundle.Avatars)["owner"] == "" {
		t.Fatalf("owner singleton must remain canonical: %+v", bundle.Avatars)
	}
	if _, exists := (*bundle.Avatars)["member"]; exists {
		t.Fatal("legacy member key must be omitted after normalization")
	}

	tooMany := map[string][]ThemeIconDTO{
		"member": make([]ThemeIconDTO, maxAvatarPoolItems+1),
	}
	if err := validateAvatarPools(&tooMany, "theme"); err == nil ||
		!strings.Contains(err.Error(), "at most 12") {
		t.Fatalf("oversized pool must be rejected: %v", err)
	}
	bothAvatar := map[string]string{"member": memberImage}
	bothPool := map[string][]ThemeIconDTO{"member": {{Image: memberImage}}}
	both := ThemeBundleDTO{Avatars: &bothAvatar, AvatarPools: &bothPool}
	if err := normalizeThemeAvatarPools(&both, "theme"); err == nil ||
		!strings.Contains(err.Error(), "cannot define both") {
		t.Fatalf("ambiguous legacy/canonical input must be rejected: %v", err)
	}
}

// TestValidateThemeBundlesAvatars checks the avatars overlay flows through the
// top-level bundle validator (parity with colours / wording / fonts).
func TestValidateThemeBundlesAvatars(t *testing.T) {
	avatars := map[string]string{"member": dataURI("image/png", pngBytes)}
	legal := []ThemeBundleDTO{{
		Id:      "midnight",
		Name:    "Midnight",
		Colors:  map[string]string{"--color-bg": "#101018"},
		Avatars: &avatars,
	}}
	// The write face normalizes BEFORE it validates, and a legacy
	// `avatars.member` singleton is exactly what normalization exists to move:
	// it becomes a one-image pool, so `member` never reaches the singleton
	// validator. Validating without that step asks a question the product
	// never asks.
	if err := normalizeThemeBundles(legal); err != nil {
		t.Fatalf("legacy singleton must normalize: %v", err)
	}
	if err := validateThemeBundles(legal); err != nil {
		t.Fatalf("bundle with a legal avatars overlay must pass: %v", err)
	}
	if pool := (*legal[0].AvatarPools)["member"]; len(pool) != 1 || pool[0].Id == nil {
		t.Fatalf("legacy singleton did not become an identified pool item: %+v", pool)
	}

	// The illegal half stays on a SINGLETON kind: the point is the image gate,
	// and routing it through normalization first would test the pool path.
	bad := map[string]string{"assistant": dataURI("image/svg+xml", []byte("<svg onload=alert(1)>"))}
	illegal := []ThemeBundleDTO{{
		Id:      "midnight",
		Name:    "Midnight",
		Colors:  map[string]string{"--color-bg": "#101018"},
		Avatars: &bad,
	}}
	if err := validateThemeBundles(illegal); err == nil ||
		!strings.Contains(err.Error(), "not an allowed image type") {
		t.Fatalf("bundle with an SVG avatar must 422: %v", err)
	}

	// Backward compatibility: a pre-T-ea81 bundle carrying only member/outsource
	// avatars and no logo/navIcons/backgrounds stays valid unchanged.
	legacy := map[string]string{
		"member":    dataURI("image/png", pngBytes),
		"outsource": dataURI("image/webp", webpBytes),
	}
	legacyBundles := []ThemeBundleDTO{{
		Id:      "midnight",
		Name:    "Midnight",
		Colors:  map[string]string{"--color-bg": "#101018"},
		Avatars: &legacy,
	}}
	// Same order the write face uses: normalize, then validate. Both singletons
	// become one-image pools and the bundle stays admissible unchanged.
	if err := normalizeThemeBundles(legacyBundles); err != nil {
		t.Fatalf("legacy avatars-only bundle must normalize: %v", err)
	}
	if err := validateThemeBundles(legacyBundles); err != nil {
		t.Fatalf("legacy avatars-only bundle must stay valid: %v", err)
	}
	pools := *legacyBundles[0].AvatarPools
	if len(pools["member"]) != 1 || len(pools["outsource"]) != 1 {
		t.Fatalf("both legacy singletons must become pools: %+v", pools)
	}
}

func TestValidateLogo(t *testing.T) {
	// nil logo is admissible (optional).
	if err := validateLogo(nil, "t"); err != nil {
		t.Fatalf("nil logo must be admissible: %v", err)
	}

	// A legal raster logo passes the shared avatar image gate.
	logo := dataURI("image/png", pngBytes)
	if err := validateLogo(&logo, "t"); err != nil {
		t.Fatalf("legal logo must pass: %v", err)
	}

	big := make([]byte, maxAvatarBytes+1)
	copy(big, pngBytes)
	for _, tc := range []struct {
		name   string
		value  string
		expect string
	}{
		{"svg rejected", dataURI("image/svg+xml", []byte(`<svg onload=alert(1)>`)), "not an allowed image type"},
		{"oversize rejected", dataURI("image/png", big), "too large"},
		{"bad magic rejected", dataURI("image/png", jpegBytes), "magic-byte"},
	} {
		err := validateLogo(&tc.value, "cx[0]")
		if err == nil || !strings.Contains(err.Error(), tc.expect) {
			t.Fatalf("%s: expected %q, got %v", tc.name, tc.expect, err)
		}
		if !strings.Contains(err.Error(), "cx[0]: logo") {
			t.Fatalf("%s: error must carry the logo locator: %v", tc.name, err)
		}
	}
}

func TestValidateNavIcons(t *testing.T) {
	// nil overlay is admissible (optional).
	if err := validateNavIcons(nil, "t"); err != nil {
		t.Fatalf("nil navIcons must be admissible: %v", err)
	}

	// All five nav-tab keys are legal and pass the shared image gate.
	ok := map[string]string{
		"office":  dataURI("image/png", pngBytes),
		"replies": dataURI("image/jpeg", jpegBytes),
		"tasks":   dataURI("image/webp", webpBytes),
		"monitor": dataURI("image/png", pngBytes),
		"guide":   dataURI("image/jpeg", jpegBytes),
	}
	if err := validateNavIcons(&ok, "t"); err != nil {
		t.Fatalf("legal navIcons overlay must pass: %v", err)
	}

	// An unknown tab key is rejected.
	badKey := map[string]string{"settings": dataURI("image/png", pngBytes)}
	if err := validateNavIcons(&badKey, "t"); err == nil ||
		!strings.Contains(err.Error(), "only office, replies, tasks, monitor, guide") {
		t.Fatalf("unknown nav icon key must 422 naming the key set: %v", err)
	}

	// A value under a legal key still runs the image gate — SVG is rejected.
	badVal := map[string]string{"office": dataURI("image/svg+xml", []byte("<svg onload=alert(1)>"))}
	if err := validateNavIcons(&badVal, "cx[0]"); err == nil ||
		!strings.Contains(err.Error(), "cx[0]: navIcons[office]") ||
		!strings.Contains(err.Error(), "not an allowed image type") {
		t.Fatalf("SVG nav icon value must 422 through the image gate with locator: %v", err)
	}
}

func TestValidateBackgrounds(t *testing.T) {
	// nil overlay is admissible (optional) — a bundle authored before T-081b.
	if err := validateBackgrounds(nil, "t"); err != nil {
		t.Fatalf("nil backgrounds must be admissible: %v", err)
	}

	// The canvas zone is legal and passes the shared avatar image gate.
	ok := map[string]string{"canvas": dataURI("image/png", pngBytes)}
	if err := validateBackgrounds(&ok, "t"); err != nil {
		t.Fatalf("legal canvas background must pass: %v", err)
	}

	// The zones that sit under text are NOT openable to an image.
	for _, zone := range []string{"topbar", "nav", "main"} {
		bad := map[string]string{zone: dataURI("image/png", pngBytes)}
		if err := validateBackgrounds(&bad, "t"); err == nil ||
			!strings.Contains(err.Error(), "only canvas") {
			t.Fatalf("zone %q must 422 naming the allowed zone set: %v", zone, err)
		}
	}

	// A background image that is one byte OVER the avatar cap is now ADMISSIBLE
	// (T-72da): the owner overturned T-081b's "the cap is never relaxed" on
	// 2026-08-03. Without this positive case the relaxation itself has no
	// witness — the reject cases below would still pass at 64 KiB.
	overAvatarCap := make([]byte, maxAvatarBytes+1)
	copy(overAvatarCap, pngBytes)
	pastAvatar := map[string]string{"canvas": dataURI("image/png", overAvatarCap)}
	if err := validateBackgrounds(&pastAvatar, "t"); err != nil {
		t.Fatalf("a background one byte past the AVATAR cap must now pass: %v", err)
	}
	// Exactly at the background cap is admissible; this also proves the raw
	// string-length pre-filter moved with it — 512 KiB decoded is ~683 KiB
	// encoded, so a stale maxBackgroundValueLen would reject this as "too long"
	// long before the decoded cap is ever consulted.
	atCap := make([]byte, maxBackgroundBytes)
	copy(atCap, pngBytes)
	exact := map[string]string{"canvas": dataURI("image/png", atCap)}
	if err := validateBackgrounds(&exact, "t"); err != nil {
		t.Fatalf("a background of exactly maxBackgroundBytes must pass: %v", err)
	}

	// A value under the legal zone still runs the same image gate as an avatar
	// for every SECURITY property — mime allowlist, magic bytes — and is capped,
	// just at the background cap rather than the avatar one.
	big := make([]byte, maxBackgroundBytes+1)
	copy(big, pngBytes)
	for _, tc := range []struct {
		name   string
		value  string
		expect string
	}{
		{"svg rejected", dataURI("image/svg+xml", []byte(`<svg onload=alert(1)>`)), "not an allowed image type"},
		{"oversize rejected", dataURI("image/png", big), "too large"},
		{"bad magic rejected", dataURI("image/png", jpegBytes), "magic-byte"},
	} {
		bad := map[string]string{"canvas": tc.value}
		err := validateBackgrounds(&bad, "cx[0]")
		if err == nil || !strings.Contains(err.Error(), tc.expect) {
			t.Fatalf("%s: expected %q, got %v", tc.name, tc.expect, err)
		}
		if !strings.Contains(err.Error(), "cx[0]: backgrounds[canvas]") {
			t.Fatalf("%s: error must carry the backgrounds locator: %v", tc.name, err)
		}
	}
}

func TestValidateBackgroundModes(t *testing.T) {
	images := map[string]string{"canvas": dataURI("image/png", pngBytes)}

	// nil map is admissible and means every zone tiles — a bundle authored
	// before the mode existed must render exactly as it did.
	if err := validateBackgroundModes(nil, &images, "t"); err != nil {
		t.Fatalf("nil backgroundModes must be admissible: %v", err)
	}
	for _, mode := range []string{"tile", "sides", "cover"} {
		ok := map[string]string{"canvas": mode}
		if err := validateBackgroundModes(&ok, &images, "t"); err != nil {
			t.Fatalf("mode %q must pass: %v", mode, err)
		}
	}

	// Same closed zone set as the images themselves.
	badZone := map[string]string{"topbar": "tile"}
	if err := validateBackgroundModes(&badZone, &images, "t"); err == nil ||
		!strings.Contains(err.Error(), "only canvas") {
		t.Fatalf("unknown zone must 422 naming the zone set: %v", err)
	}

	// The mode vocabulary is closed — case and whitespace included.
	for _, mode := range []string{"Tile", "contain", "", "sides "} {
		bad := map[string]string{"canvas": mode}
		err := validateBackgroundModes(&bad, &images, "cx[0]")
		if err == nil || !strings.Contains(err.Error(), "not a valid mode") {
			t.Fatalf("mode %q must 422: %v", mode, err)
		}
		if !strings.Contains(err.Error(), "cx[0]: backgroundModes[canvas]") {
			t.Fatalf("mode %q: error must carry the locator: %v", mode, err)
		}
	}

	// A mode with no image paints nothing — named as a mistake, not ignored.
	lone := map[string]string{"canvas": "sides"}
	empty := map[string]string{"canvas": ""}
	for _, bg := range []*map[string]string{nil, &empty} {
		if err := validateBackgroundModes(&lone, bg, "t"); err == nil ||
			!strings.Contains(err.Error(), "no image in backgrounds") {
			t.Fatalf("mode without an image must 422: %v", err)
		}
	}
}

// TestValidateThemeBundlesImages checks the logo + navIcons + backgrounds
// overlays flow through the top-level bundle validator (parity with avatars).
func TestValidateThemeBundlesImages(t *testing.T) {
	logo := dataURI("image/png", pngBytes)
	navIcons := map[string]string{"office": dataURI("image/webp", webpBytes)}
	backgrounds := map[string]string{"canvas": dataURI("image/jpeg", jpegBytes)}
	backgroundModes := map[string]string{"canvas": "sides"}
	legal := []ThemeBundleDTO{{
		Id:              "midnight",
		Name:            "Midnight",
		Colors:          map[string]string{"--color-bg": "#101018"},
		Logo:            &logo,
		NavIcons:        &navIcons,
		Backgrounds:     &backgrounds,
		BackgroundModes: &backgroundModes,
	}}
	if err := validateThemeBundles(legal); err != nil {
		t.Fatalf("bundle with legal logo + navIcons + backgrounds must pass: %v", err)
	}

	// …and an illegal mode is caught at the bundle level too.
	badMode := map[string]string{"canvas": "contain"}
	if err := validateThemeBundles([]ThemeBundleDTO{{
		Id:              "midnight",
		Name:            "Midnight",
		Colors:          map[string]string{"--color-bg": "#101018"},
		Backgrounds:     &backgrounds,
		BackgroundModes: &badMode,
	}}); err == nil || !strings.Contains(err.Error(), "not a valid mode") {
		t.Fatalf("illegal background mode must 422 through the bundle: %v", err)
	}

	badBg := map[string]string{"canvas": dataURI("image/svg+xml", []byte("<svg onload=alert(1)>"))}
	if err := validateThemeBundles([]ThemeBundleDTO{{
		Id:          "midnight",
		Name:        "Midnight",
		Colors:      map[string]string{"--color-bg": "#101018"},
		Backgrounds: &badBg,
	}}); err == nil || !strings.Contains(err.Error(), "not an allowed image type") {
		t.Fatalf("bundle with an SVG canvas background must 422: %v", err)
	}

	badLogo := dataURI("image/svg+xml", []byte("<svg onload=alert(1)>"))
	if err := validateThemeBundles([]ThemeBundleDTO{{
		Id:     "midnight",
		Name:   "Midnight",
		Colors: map[string]string{"--color-bg": "#101018"},
		Logo:   &badLogo,
	}}); err == nil || !strings.Contains(err.Error(), "not an allowed image type") {
		t.Fatalf("bundle with an SVG logo must 422: %v", err)
	}

	badNav := map[string]string{"office": dataURI("image/svg+xml", []byte("<svg onload=alert(1)>"))}
	if err := validateThemeBundles([]ThemeBundleDTO{{
		Id:       "midnight",
		Name:     "Midnight",
		Colors:   map[string]string{"--color-bg": "#101018"},
		NavIcons: &badNav,
	}}); err == nil || !strings.Contains(err.Error(), "not an allowed image type") {
		t.Fatalf("bundle with an SVG nav icon must 422: %v", err)
	}
}
