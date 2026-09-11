// Skeleton generated from server/ocserverd/avatar_bundle.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"bytes"
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

func TestValidImageValue(t *testing.T) {
	valid := []struct {
		name string
		mime string
		raw  []byte
	}{
		{name: "png", mime: "image/png", raw: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}},
		{name: "jpeg", mime: "image/jpeg", raw: []byte{0xff, 0xd8, 0xff}},
		{name: "webp", mime: "image/webp", raw: []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			if err := validImageValue(imageDataURI(tt.mime, tt.raw), len(tt.raw), maxAvatarValueLen); err != nil {
				t.Fatalf("validImageValue: %v", err)
			}
		})
	}

	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	tests := []struct {
		name  string
		value string
		max   int
		want  string
	}{
		{name: "empty", value: "", max: maxAvatarBytes, want: "must not be empty"},
		{name: "not a data URI", value: "https://example.test/image.png", max: maxAvatarBytes, want: "base64 data"},
		{name: "missing payload separator", value: "data:image/png;base64", max: maxAvatarBytes, want: "base64 data"},
		{name: "missing base64 marker", value: "data:image/png," + base64.StdEncoding.EncodeToString(png), max: maxAvatarBytes, want: "base64-encoded"},
		{name: "svg is not an allowed mime", value: imageDataURI("image/svg+xml", png), max: maxAvatarBytes, want: "not an allowed image type"},
		{name: "invalid base64 alphabet", value: "data:image/png;base64,!!!!", max: maxAvatarBytes, want: "invalid base64"},
		{name: "invalid base64 length", value: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png) + "A", max: maxAvatarBytes, want: "invalid base64"},
		{name: "empty decoded payload", value: "data:image/png;base64,====", max: maxAvatarBytes, want: "invalid base64"},
		{name: "declared mime does not match magic", value: imageDataURI("image/jpeg", png), max: maxAvatarBytes, want: "magic-byte check failed"},
		{name: "raw value cap", value: strings.Repeat("x", maxAvatarValueLen+1), max: maxAvatarBytes, want: "data URI is too long"},
		{name: "decoded byte cap", value: imageDataURI("image/png", append(append([]byte{}, png...), bytes.Repeat([]byte{'x'}, maxAvatarBytes-len(png)+1)...)), max: maxAvatarBytes, want: "decoded image is too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validImageValue(tt.value, tt.max, maxAvatarValueLen)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validImageValue() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateAvatars(t *testing.T) {
	if err := validateAvatars(nil, "theme[0]"); err != nil {
		t.Fatalf("validateAvatars(nil): %v", err)
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
	valid := imageDataURI("image/png", []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	if err := validateLogo(nil, "theme[0]"); err != nil {
		t.Fatalf("validateLogo(nil): %v", err)
	}
	if err := validateLogo(&valid, "theme[0]"); err != nil {
		t.Fatalf("validateLogo(valid): %v", err)
	}
	invalid := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>"))
	if err := validateLogo(&invalid, "theme[0]"); err == nil || !strings.Contains(err.Error(), "logo") {
		t.Fatalf("validateLogo(invalid) = %v, want logo error", err)
	}
}

func TestValidateBackgrounds(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	large := append(append([]byte{}, png...), bytes.Repeat([]byte{'x'}, maxAvatarBytes-len(png)+1)...)
	valid := imageDataURI("image/png", large)
	if err := validateBackgrounds(nil, "theme[0]"); err != nil {
		t.Fatalf("validateBackgrounds(nil): %v", err)
	}
	backgrounds := map[string]string{"canvas": valid}
	if err := validateBackgrounds(&backgrounds, "theme[0]"); err != nil {
		t.Fatalf("validateBackgrounds(valid large image): %v", err)
	}
	for _, tt := range []struct {
		name string
		data map[string]string
		want string
	}{
		{name: "unknown zone", data: map[string]string{"topbar": valid}, want: "not allowed"},
		{name: "invalid image", data: map[string]string{"canvas": "not-an-image"}, want: "backgrounds[canvas]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateBackgrounds(&tt.data, "theme[0]"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateBackgrounds() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateBackgroundModes(t *testing.T) {
	valid := imageDataURI("image/png", []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	backgrounds := map[string]string{"canvas": valid}
	if err := validateBackgroundModes(nil, &backgrounds, "theme[0]"); err != nil {
		t.Fatalf("validateBackgroundModes(nil): %v", err)
	}
	for _, mode := range []string{"tile", "sides", "cover"} {
		modes := map[string]string{"canvas": mode}
		if err := validateBackgroundModes(&modes, &backgrounds, "theme[0]"); err != nil {
			t.Fatalf("validateBackgroundModes(%q): %v", mode, err)
		}
	}
	for _, tt := range []struct {
		name        string
		modes       map[string]string
		backgrounds *map[string]string
		want        string
	}{
		{name: "unknown zone", modes: map[string]string{"nav": "tile"}, backgrounds: &backgrounds, want: "not allowed"},
		{name: "missing image", modes: map[string]string{"canvas": "tile"}, backgrounds: nil, want: "has no image"},
		{name: "empty image", modes: map[string]string{"canvas": "tile"}, backgrounds: func() *map[string]string { v := map[string]string{"canvas": ""}; return &v }(), want: "has no image"},
		{name: "unknown mode", modes: map[string]string{"canvas": "stretch"}, backgrounds: &backgrounds, want: "not a valid mode"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateBackgroundModes(&tt.modes, tt.backgrounds, "theme[0]"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateBackgroundModes() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateNavIcons(t *testing.T) {
	valid := imageDataURI("image/png", []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	if err := validateNavIcons(nil, "theme[0]"); err != nil {
		t.Fatalf("validateNavIcons(nil): %v", err)
	}
	icons := map[string]string{"office": valid, "replies": valid, "tasks": valid, "monitor": valid, "guide": valid}
	if err := validateNavIcons(&icons, "theme[0]"); err != nil {
		t.Fatalf("validateNavIcons(valid): %v", err)
	}
	for _, tt := range []struct {
		name string
		data map[string]string
		want string
	}{
		{name: "unknown tab", data: map[string]string{"settings": valid}, want: "not allowed"},
		{name: "invalid image", data: map[string]string{"office": "not-an-image"}, want: "navIcons[office]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateNavIcons(&tt.data, "theme[0]"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateNavIcons() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func imageDataURI(mime string, raw []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}
