package main

// The security boundary is the colour VALUE: only concrete colours pass, and
// anything outside the allowlist is a 422 — never silently dropped or stored.
// This grammar is mirrored character for character by
// frontend/src/lib/themeBundle.ts (shared with the mock API); change both.

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxColorValueLen = 64

	minThemeColors = 1
	maxThemeColors = 200
	// Its original reason (one JSON settings row) is gone; the cap stays because
	// themes embed their images, so it bounds the owner's database. Checked on
	// creates only — refusing a replace would strand an owner at the limit.
	maxCustomThemes = 100
	maxThemeNameLen = 80
)

var (
	colorHexRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	colorRgbRe = regexp.MustCompile(`^rgba?\(\s*[0-9.,%/\s]+\)$`)
	// The angle-unit letters (deg/grad/rad/turn) cannot spell url/var/expression/
	// color-mix/javascript — all need a paren or colon this class forbids.
	colorHslRe = regexp.MustCompile(`^hsla?\(\s*[0-9.,%/\sdegratun]+\)$`)

	themeBundleIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
)

var reservedThemeIDs = map[string]bool{"office": true}

// Unicode CATEGORIES, not hand-listed codepoints: a listed set let every
// unlisted member of the same categories through (SOFT HYPHEN, the TAG block,
// U+2028/9), and invisible runes make identical-looking names differ in bytes.
// Zs is NOT here — it is normalised to U+0020 (a full-width IME space in
// 「深海　之夜」 is legitimate). Mn is deliberately not rejected: variation
// selectors spell emoji names and Mn holds every combining accent.
// The TS twin (INVISIBLE_NAME_CLASS_RE) is held equal by
// frontend/src/lib/themeName.parity.test.ts, including across Unicode versions.
var invisibleNameCategories = []*unicode.RangeTable{
	unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs,
	unicode.Zl, unicode.Zp,
}

func hasInvisibleNameRune(s string) bool {
	for _, r := range s {
		if unicode.In(r, invisibleNameCategories...) {
			return true
		}
	}
	return false
}

// Runs FIRST, before the name is trimmed, measured or compared against a
// built-in's: 「　辦公室　」 then collapses onto 「辦公室」 and is refused by the
// reserved-name rule.
func normalizeThemeSpaces(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Zs, r) {
			r = ' '
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Deliberately not strings.TrimSpace: unicode.IsSpace differs from JS
// String.prototype.trim() (U+FEFF, U+0085), so the Go and TS validators would
// measure different strings. ASCII-only is exhaustive because
// normalizeThemeSpaces ran first and the other whitespace is rejected outright.
func trimThemeName(s string) string {
	return strings.Trim(normalizeThemeSpaces(s), "\t\n\v\f\r ")
}

var colorInjectionMarkers = []string{
	"url(", "expression(", "var(", "color-mix(", "image(", "element(",
	"javascript:", "/*", "*/", ";", "{", "}", "<", ">", "@", "\\", "`",
	"\n", "\r",
}

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

func validateThemeBundles(bundles []ThemeBundleDTO) error {
	if len(bundles) > maxCustomThemes {
		return fmt.Errorf("custom_themes must hold at most %d themes", maxCustomThemes)
	}
	seen := make(map[string]bool, len(bundles))
	for i, b := range bundles {
		where := fmt.Sprintf("custom_themes[%d]", i)
		if err := validateThemeBundle(b, where, seen); err != nil {
			return err
		}
	}
	return nil
}

// Excludes the two set-level rules (theme count, cross-bundle id uniqueness):
// the single-theme endpoints answer those against the table; pass a nil `seen`
// there. ⚠️ Check order is load-bearing: a bundle that is both a duplicate and
// badly named must report the duplicate — tests and the mirrored client
// validator pin that message.
func validateThemeBundle(b ThemeBundleDTO, where string, seen map[string]bool) error {
	if !themeBundleIDRe.MatchString(b.Id) {
		return fmt.Errorf(
			"%s: id must match ^[a-z0-9][a-z0-9-]{1,63}$ (got %q)", where, b.Id)
	}
	if reservedThemeIDs[b.Id] {
		return fmt.Errorf("%s: id %q is reserved for a built-in theme", where, b.Id)
	}
	if seen != nil {
		if seen[b.Id] {
			return fmt.Errorf("%s: duplicate id %q", where, b.Id)
		}
		seen[b.Id] = true
	}

	name := trimThemeName(b.Name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxThemeNameLen {
		return fmt.Errorf(
			"%s: name must be 1..%d characters after trimming", where, maxThemeNameLen)
	}
	if hasInvisibleNameRune(b.Name) {
		return fmt.Errorf(
			"%s: name must not contain control, formatting, private-use, surrogate or line/paragraph separator characters",
			where)
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
	if err := validateWording(b.Wording, where); err != nil {
		return err
	}
	if err := validateFonts(b.Fonts, where); err != nil {
		return err
	}
	if err := validateAvatars(b.Avatars, where); err != nil {
		return err
	}
	if err := validateLogo(b.Logo, where); err != nil {
		return err
	}
	if err := validateNavIcons(b.NavIcons, where); err != nil {
		return err
	}
	if err := validateBackgrounds(b.Backgrounds, where); err != nil {
		return err
	}
	if err := validateBackgroundModes(
		b.BackgroundModes, b.Backgrounds, where,
	); err != nil {
		return err
	}
	return nil
}
