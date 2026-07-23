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

	// A legal kind→image overlay round-trips.
	ok := map[string]string{
		"member":    dataURI("image/png", pngBytes),
		"outsource": dataURI("image/webp", webpBytes),
	}
	if err := validateAvatars(&ok, "t"); err != nil {
		t.Fatalf("legal avatars overlay must pass: %v", err)
	}

	// An unknown kind key is rejected.
	badKind := map[string]string{"boss": dataURI("image/png", pngBytes)}
	if err := validateAvatars(&badKind, "t"); err == nil ||
		!strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("unknown avatar kind must 422: %v", err)
	}

	// A bad value under a legal kind is rejected with the locator.
	badVal := map[string]string{"member": dataURI("image/svg+xml", []byte("<svg/>"))}
	if err := validateAvatars(&badVal, "cx[0]"); err == nil ||
		!strings.Contains(err.Error(), "cx[0]: avatars[member]") {
		t.Fatalf("bad avatar value must 422 with locator: %v", err)
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
	if err := validateThemeBundles(legal); err != nil {
		t.Fatalf("bundle with a legal avatars overlay must pass: %v", err)
	}

	bad := map[string]string{"member": dataURI("image/svg+xml", []byte("<svg onload=alert(1)>"))}
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
}
