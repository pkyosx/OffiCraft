package main

// font_bundle.go — T-16a1 P4: server-side validation of a theme bundle's
// optional `fonts` overlay (font-family choices). The overlay is
// `{ "--font-*": "<font-family stack>" }`:
//
//   - the KEY is a `--font-*` token in the generated whitelist (themeFontTokens,
//     theme_fonts_gen.go — extracted from themeFonts.source.json, the single
//     source of truth shared with the client + mock);
//   - the VALUE is NOT an arbitrary string. Unlike a colour (admitted by a
//     grammar), a font-family stack has no safe closed grammar that also
//     excludes url()/@font-face, so the value set is a CLOSED ALLOWLIST
//     (themeFontStacks) — the curated safe families the theme editor offers.
//     A value outside the set, or one carrying any CSS-structure character, is
//     a 422. There is no external-font / url() / @font-face path by construction.
//
// The value is applied on the client via element.style.setProperty("--font-x",
// value) — never concatenated into a stylesheet — but even so we admit only the
// closed allowlist and reject structure characters as defence-in-depth.
//
// This mirrors, rule for rule, the client validator in
// frontend/src/lib/themeBundle.ts (shared with the mock API), so a fonts overlay
// rejected offline is rejected online for the identical reason. fonts is
// OPTIONAL: an absent overlay is fine; a present one is validated in full. Any
// violation is a 422 — never silently dropped, never stored.

import (
	"fmt"
	"strings"
)

// maxFontValueLen caps one font value (bytes). The longest curated family stack
// fits comfortably; a value longer than this is only ever an injection attempt.
// Membership in themeFontStacks is the real gate — this cap is a cheap
// pre-filter, the twin of MAX_FONT_VALUE_LEN on the client.
const maxFontValueLen = 128

// fontInjectionMarkers are structure-breaking substrings a font value can never
// legitimately contain — the same defence-in-depth pass colours get. Membership
// in themeFontStacks already guarantees safety; this second pass exists so a
// smuggled value returns a SPECIFIC error before the membership check.
var fontInjectionMarkers = []string{
	"url(", "expression(", "var(", "javascript:", "/*", "*/",
	";", "{", "}", "<", ">", "@", "\\", "`", "\n", "\r", "(",
}

// validFontValue reports whether v is an admissible font value: a member of the
// closed safe-family stack allowlist, with no CSS-structure characters.
func validFontValue(v string) bool {
	if v == "" || len(v) > maxFontValueLen {
		return false
	}
	for _, m := range fontInjectionMarkers {
		if strings.Contains(v, m) {
			return false
		}
	}
	return themeFontStacks[v]
}

// validateFonts validates one bundle's optional fonts overlay. `where` is the
// caller's bundle locator (e.g. "custom_themes[2]") for a precise 422 message.
// A nil overlay is admissible (fonts is optional).
func validateFonts(fonts *map[string]string, where string) error {
	if fonts == nil {
		return nil
	}
	for token, value := range *fonts {
		if !themeFontTokens[token] {
			return fmt.Errorf(
				"%s: %q is not a theme font token (only --font-sans / --font-title)", where, token)
		}
		if !validFontValue(value) {
			return fmt.Errorf(
				"%s: %q has an invalid font value %q — only a safe built-in font family may be chosen",
				where, token, value)
		}
	}
	return nil
}
