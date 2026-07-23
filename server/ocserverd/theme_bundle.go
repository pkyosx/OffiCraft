package main

// theme_bundle.go — T-16a1 P2: server-side validation of owner-authored theme
// colour bundles (display.custom_themes). The security boundary is the colour
// VALUE, not the token name.
//
// A bundle is `{ id, name, colors: { "--color-x": "<value>" } }`. It is stored
// as JSON and, on the client, applied via element.style.setProperty(name,
// value) — the value is NEVER concatenated into a stylesheet string. Even so we
// admit only CONCRETE colours through a strict allowlist grammar (hex / rgb(a) /
// hsl(a) / transparent, anchored full-match, length-capped), reject any value
// carrying CSS structure characters as defence-in-depth, and constrain the
// token NAME to the generated theme.css whitelist (themeColorTokens,
// theme_tokens_gen.go). Anything outside the allowlist is a 422 — never
// silently dropped, never stored.
//
// This grammar is mirrored, character for character, by the client validator
// in frontend/src/lib/themeBundle.ts (shared with the mock API), so import
// rejection is identical offline and online.

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// maxColorValueLen caps a single colour value. The longest legitimate
	// concrete colour (a spaced-out modern rgba/hsla) fits comfortably; a value
	// longer than this is only ever an injection attempt.
	maxColorValueLen = 64
	// maxThemeColors / minThemeColors bound the colours map in one bundle.
	minThemeColors = 1
	maxThemeColors = 200
	// maxCustomThemes bounds how many bundles the owner may keep — the setting
	// is one JSON row, so an unbounded array is the only way to bloat it.
	maxCustomThemes = 100
	// maxThemeNameLen caps a bundle's display name (runes), matching the
	// existing 80-rune name convention (org.name / owner.name).
	maxThemeNameLen = 80
)

// The colour-value allowlist grammar (anchored full-match). Concrete colours
// only: no var(), no color-mix(), no CSS named colours except `transparent`
// (the one keyword worth keeping — it carries no injection surface).
var (
	colorHexRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	// rgb()/rgba(): digits, dot, comma, percent, slash (modern alpha), space —
	// NO letters at all, so `url(`/`var(`/`expression(` can never appear inside.
	colorRgbRe = regexp.MustCompile(`^rgba?\(\s*[0-9.,%/\s]+\)$`)
	// hsl()/hsla(): as rgb(), plus the angle-unit letters {d,e,g,r,a,t,u,n}
	// (deg/grad/rad/turn) on the hue. That letter set cannot form url/var/
	// expression/color-mix/javascript (all of which need a paren or colon this
	// class forbids), so the surface stays closed.
	colorHslRe = regexp.MustCompile(`^hsla?\(\s*[0-9.,%/\sdegratun]+\)$`)
	// themeBundleIDRe: a client-generated stable slug.
	themeBundleIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
)

// reservedThemeIDs are the built-in theme names — a custom bundle must never
// claim one (the built-in is applied via data-theme, not setProperty). office
// is the only built-in now (修仙 is an importable custom bundle), so "xian" is a
// perfectly legal custom id.
var reservedThemeIDs = map[string]bool{"office": true}

// colorInjectionMarkers are structure-breaking substrings a concrete colour can
// never legitimately contain. The allowlist grammar already rejects every one
// of them; this second pass exists only to return a SPECIFIC error (so the
// owner learns "you pasted CSS", not a generic "bad colour").
var colorInjectionMarkers = []string{
	"url(", "expression(", "var(", "color-mix(", "image(", "element(",
	"javascript:", "/*", "*/", ";", "{", "}", "<", ">", "@", "\\", "`",
	"\n", "\r",
}

// validColorValue reports whether v is an admissible concrete colour value.
func validColorValue(v string) bool {
	if v == "" || len(v) > maxColorValueLen {
		return false
	}
	for _, m := range colorInjectionMarkers {
		if strings.Contains(v, m) {
			return false
		}
	}
	if v == "transparent" {
		return true
	}
	return colorHexRe.MatchString(v) ||
		colorRgbRe.MatchString(v) ||
		colorHslRe.MatchString(v)
}

// validateThemeBundles validates the whole custom_themes array against the
// bundle shape (§1), the token-name whitelist (§3), and the colour-value
// grammar (§2). It returns a human-readable message on the first violation
// (surfaced verbatim as the 422 body) and nil when every bundle is admissible.
// ids must be unique across the array.
func validateThemeBundles(bundles []ThemeBundleDTO) error {
	if len(bundles) > maxCustomThemes {
		return fmt.Errorf("custom_themes must hold at most %d themes", maxCustomThemes)
	}
	seen := make(map[string]bool, len(bundles))
	for i, b := range bundles {
		where := fmt.Sprintf("custom_themes[%d]", i)
		if !themeBundleIDRe.MatchString(b.Id) {
			return fmt.Errorf(
				"%s: id must match ^[a-z0-9][a-z0-9-]{1,63}$ (got %q)", where, b.Id)
		}
		if reservedThemeIDs[b.Id] {
			return fmt.Errorf("%s: id %q is reserved for a built-in theme", where, b.Id)
		}
		if seen[b.Id] {
			return fmt.Errorf("%s: duplicate id %q", where, b.Id)
		}
		seen[b.Id] = true

		name := strings.TrimSpace(b.Name)
		if n := utf8.RuneCountInString(name); n < 1 || n > maxThemeNameLen {
			return fmt.Errorf(
				"%s: name must be 1..%d characters after trimming", where, maxThemeNameLen)
		}
		if n := len(b.Colors); n < minThemeColors || n > maxThemeColors {
			return fmt.Errorf(
				"%s: colors must hold %d..%d entries (got %d)",
				where, minThemeColors, maxThemeColors, n)
		}
		for token, value := range b.Colors {
			if !themeColorTokens[token] {
				return fmt.Errorf(
					"%s: %q is not a theme colour token (see theme.css)", where, token)
			}
			if !validColorValue(value) {
				return fmt.Errorf(
					"%s: %q has an invalid colour value %q — only concrete "+
						"hex / rgb() / rgba() / hsl() / hsla() / transparent are accepted",
					where, token, value)
			}
		}
		// wording (T-16a1 P3) is an OPTIONAL per-language text-override overlay —
		// validated in full when present (language set + message-key whitelist +
		// plain-text value rules), a no-op when absent.
		if err := validateWording(b.Wording, where); err != nil {
			return err
		}
	}
	return nil
}

// themeBundleIDSet returns the id set of a bundle array — the vocabulary
// display.theme may point at (on top of the built-ins).
func themeBundleIDSet(bundles []ThemeBundleDTO) map[string]bool {
	ids := make(map[string]bool, len(bundles))
	for _, b := range bundles {
		ids[b.Id] = true
	}
	return ids
}

// isValidDisplayTheme reports whether theme is an admissible display.theme
// value given the effective custom-theme id set: "" (unset) | a built-in |
// an existing custom id.
func isValidDisplayTheme(theme string, customIDs map[string]bool) bool {
	return theme == "" || displayThemeAllowed[theme] || customIDs[theme]
}
