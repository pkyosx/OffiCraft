package main

// font_bundle.go — validation of a theme bundle's optional `fonts` overlay
// (T-16a1 P4). The KEY must be in the generated token whitelist
// (themeFontTokens, theme_fonts_gen.go, from themeFonts.source.json shared with
// the client + mock). The VALUE is a CLOSED ALLOWLIST (themeFontStacks): unlike
// a colour, a font-family stack has no safe closed grammar that also excludes
// url()/@font-face. Mirrors, rule for rule, frontend/src/lib/themeBundle.ts, so
// an overlay rejected offline is rejected online for the same reason.

import (
	"fmt"
	"strings"
)

// maxFontValueLen is a cheap pre-filter, the twin of MAX_FONT_VALUE_LEN on the
// client; membership in themeFontStacks is the real gate.
const maxFontValueLen = 128

// fontInjectionMarkers are defence-in-depth: membership in themeFontStacks
// already guarantees safety.
var fontInjectionMarkers = []string{
	"url(", "expression(", "var(", "javascript:", "/*", "*/",
	";", "{", "}", "<", ">", "@", "\\", "`", "\n", "\r", "(",
}

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
