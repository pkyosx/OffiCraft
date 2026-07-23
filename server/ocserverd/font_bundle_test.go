package main

import (
	"strings"
	"testing"
)

// aFontStack returns one curated safe family stack for the happy-path cases.
func aFontStack(t *testing.T) string {
	t.Helper()
	for s := range themeFontStacks {
		return s
	}
	t.Fatal("themeFontStacks is empty — gen:fonts did not run")
	return ""
}

func TestValidFontValue(t *testing.T) {
	// Every curated stack is admissible.
	for s := range themeFontStacks {
		if !validFontValue(s) {
			t.Fatalf("curated stack %q must be valid", s)
		}
	}

	stack := aFontStack(t)
	// Arbitrary strings, off-allowlist families, and injection payloads are all
	// rejected — the value set is a CLOSED allowlist, not a grammar.
	for _, bad := range []string{
		"",
		"Arial",
		"Comic Sans MS, sans-serif",
		"sans-serif",
		`url("https://evil/x.woff2")`,
		"@font-face{font-family:x;src:url(y)}",
		"system-ui;}",
		"system-ui, <script>",
		"var(--x)",
		"javascript:alert(1)",
		stack + " ",              // trailing space defeats exact membership
		strings.Repeat("f", 200), // over the length cap
	} {
		if validFontValue(bad) {
			t.Fatalf("illegal font value %q must be rejected", bad)
		}
	}
}

func TestValidateFonts(t *testing.T) {
	stack := aFontStack(t)

	// nil overlay is admissible (fonts is optional).
	if err := validateFonts(nil, "t"); err != nil {
		t.Fatalf("nil fonts must be admissible: %v", err)
	}

	// A legal token→stack overlay round-trips.
	ok := map[string]string{"--font-sans": stack, "--font-title": stack}
	if err := validateFonts(&ok, "t"); err != nil {
		t.Fatalf("legal fonts overlay must pass: %v", err)
	}

	// An unknown token key is rejected.
	badTok := map[string]string{"--color-bg": stack}
	if err := validateFonts(&badTok, "t"); err == nil ||
		!strings.Contains(err.Error(), "not a theme font token") {
		t.Fatalf("unknown font token must 422: %v", err)
	}

	// An off-allowlist / injection value is rejected.
	for _, bad := range []string{
		"Times New Roman",
		`url(https://evil)`,
		"@font-face{}",
		"x;}",
	} {
		m := map[string]string{"--font-sans": bad}
		if err := validateFonts(&m, "t"); err == nil ||
			!strings.Contains(err.Error(), "invalid font value") {
			t.Fatalf("illegal font value %q must 422: %v", bad, err)
		}
	}
}

// TestValidateThemeBundlesFonts checks the fonts overlay flows through the
// top-level bundle validator (parity with the colours / wording overlays).
func TestValidateThemeBundlesFonts(t *testing.T) {
	stack := aFontStack(t)
	fonts := map[string]string{"--font-sans": stack}
	legal := []ThemeBundleDTO{{
		Id:     "midnight",
		Name:   "Midnight",
		Colors: map[string]string{"--color-bg": "#101018"},
		Fonts:  &fonts,
	}}
	if err := validateThemeBundles(legal); err != nil {
		t.Fatalf("bundle with a legal fonts overlay must pass: %v", err)
	}

	bad := map[string]string{"--font-sans": "url(https://evil)"}
	illegal := []ThemeBundleDTO{{
		Id:     "evil",
		Name:   "Evil",
		Colors: map[string]string{"--color-bg": "#101018"},
		Fonts:  &bad,
	}}
	if err := validateThemeBundles(illegal); err == nil ||
		!strings.Contains(err.Error(), "invalid font value") {
		t.Fatalf("bundle with an injection fonts value must 422: %v", err)
	}
}
