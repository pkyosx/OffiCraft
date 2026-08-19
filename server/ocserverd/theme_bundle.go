package main

// theme_bundle.go — T-16a1 P2: server-side validation of owner-authored theme
// colour bundles. (The parenthetical here named `display.custom_themes` until
// T-83ef moved themes to their own table and endpoints; the validation is the
// same, only what it is called from changed.) The security boundary is the
// colour VALUE, not the token name.
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
	"unicode"
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
	// maxCustomThemes bounds how many themes the owner may keep.
	//
	// ⚠️ ITS ORIGINAL REASON IS GONE AND THE CAP IS NOT. It read "the setting is
	// one JSON row, so an unbounded array is the only way to bloat it" — true
	// until T-83ef gave themes their own table, where one more theme is one more
	// row rather than a longer array. The cap stays because the thing it really
	// bounds is what the OWNER accumulates: a theme carries its images embedded,
	// so a hundred of them is already a large database and an unbounded number is
	// an unbounded one. What changed is how it is asked — CountCustomThemes on
	// creates only, since a replace keeps the count the same and refusing one
	// would strand an owner at the limit with no way to edit.
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

// invisibleNameCategories are the Unicode general CATEGORIES a theme display
// name may not carry — categories, not a hand-listed set of codepoints (T-081b
// review round 4, SHOULD-C). Round 3 listed six zero-width codepoints and every
// unlisted member of the SAME categories walked straight through: U+00AD SOFT
// HYPHEN, U+180E and the U+E00xx TAG block (all Cf, like the ZWSP that WAS
// listed), U+2028/U+2029 (Zl/Zp), U+00A0 / U+1680 / U+3000 (Zs). Each of them is
// invisible in the rendered name, so two names that look identical on screen can
// differ in bytes — which is how bad data gets in.
//
//	Cc control · Cf format (bidi marks, ZWSP/ZWNJ/ZWJ, WORD JOINER, BOM,
//	SOFT HYPHEN, the TAG block) · Co private use · Cs surrogate ·
//	Zl line separator · Zp paragraph separator
//
// Zs (space separator) is NOT here — it is NORMALISED to U+0020 instead, see
// normalizeThemeSpaces (T-081b review round 4 recheck, SHOULD-3). Rejecting it
// blocked 「深海　之夜」, which is simply what a Chinese IME in full-width mode
// emits when the user presses the space bar, and told them so in a message
// written for implementers. Normalising loses nothing on the security side —
// 「　辦公室　」 still fails, now against the reserved-name rule that names the
// actual reason — and keeps the legitimate names.
//
// DELIBERATELY NOT REJECTED: the variation selectors (U+FE0F & co., category
// Mn). They are zero-width, but they are also how a legitimate emoji name is
// spelled (「Heart ❤️」), and Mn holds every combining accent besides — banning
// the category would reject ordinary Vietnamese, Hebrew-with-points and
// Devanagari names.
//
// These are the standard library's own tables, so this is not a third
// hand-kept list; the TS twin (INVISIBLE_NAME_CLASS_RE in
// frontend/src/lib/themeBundle.ts) reads the same categories through `u`-flag
// property escapes, and frontend/src/lib/themeName.parity.test.ts feeds both ends
// the same 61 names and fails on ANY divergence — including one caused by the
// two runtimes shipping different Unicode versions.
var invisibleNameCategories = []*unicode.RangeTable{
	unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs,
	unicode.Zl, unicode.Zp,
}

// hasInvisibleNameRune reports whether s carries a rune from one of the
// rejected categories. The twin of hasInvisibleNameRune in
// frontend/src/lib/themeBundle.ts.
func hasInvisibleNameRune(s string) bool {
	for _, r := range s {
		if unicode.In(r, invisibleNameCategories...) {
			return true
		}
	}
	return false
}

// normalizeThemeSpaces folds every space separator onto U+0020 — the FIRST
// thing done to a name, before it is trimmed, measured or compared against a
// built-in's.
//
// U+3000 IDEOGRAPHIC SPACE, U+00A0 NO-BREAK SPACE and the rest of Zs are
// ordinary spaces as far as a human reading the name is concerned, so treating
// them as one is what makes both halves come out right: 「深海　之夜」 is accepted
// and stored as an ordinary two-word name, while 「　辦公室　」 collapses onto
// 「辦公室」 and is refused BY THE RESERVED-NAME RULE, which can say so.
//
// The twin of normalizeThemeSpaces in frontend/src/lib/themeBundle.ts.
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

// trimThemeName normalises spaces, then trims ASCII whitespace ONLY.
// Deliberately not strings.TrimSpace: that walks unicode.IsSpace, whose
// membership differs from JS String.prototype.trim()'s ECMAScript WhiteSpace
// set (U+FEFF, U+0085), so the two validators would decide name length — and
// name identity — on different strings. An explicit ASCII set is identical BY
// CONSTRUCTION on both sides, and it stays EXHAUSTIVE because
// normalizeThemeSpaces runs first: every non-ASCII space has already become
// U+0020, and every non-ASCII whitespace that is NOT a space (Zl/Zp, U+0085,
// U+FEFF) is rejected outright. Nothing is left for the two languages to
// disagree about.
// The twin of trimThemeName in frontend/src/lib/themeBundle.ts.
func trimThemeName(s string) string {
	return strings.Trim(normalizeThemeSpaces(s), "\t\n\v\f\r ")
}

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
		if err := validateThemeBundle(b, where, seen); err != nil {
			return err
		}
	}
	return nil
}

// validateThemeBundle validates ONE bundle — every rule above except the two
// that are properties of a SET rather than of a bundle: the cap on how many
// themes may be kept, and cross-bundle id uniqueness.
//
// 🔴 IT EXISTS BECAUSE THE PER-THEME ENDPOINTS CANNOT USE THE ARRAY FORM
// (T-83ef). `PUT /api/themes/{id}` has exactly one bundle in hand and no array
// to count or scan, while the two set-level rules still have to hold — they
// just have to be answered against the TABLE (CountCustomThemes, and the row
// that already carries this id) rather than against a slice. Splitting them out
// is what lets both callers share ONE copy of the bundle rules instead of the
// endpoints growing a second, drifting opinion about what a legal theme is.
//
// ⚠️ THE CHECK ORDER IS LOAD-BEARING AND IS PRESERVED EXACTLY. `seen` is
// consulted between the id checks and the name checks, where the array version
// consulted it — a bundle that is BOTH a duplicate and badly named must still
// report the duplicate, because that is the message the existing tests and the
// mirrored client validator pin. Pass a nil `seen` when there is no set to be
// duplicate WITHIN (the single-theme write, whose uniqueness question is
// "does this id already have a row", answered by the caller).
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
	// wording (T-16a1 P3) is an OPTIONAL per-language text-override overlay —
	// validated in full when present (language set + message-key whitelist +
	// plain-text value rules), a no-op when absent.
	if err := validateWording(b.Wording, where); err != nil {
		return err
	}
	// fonts (T-16a1 P4) is an OPTIONAL --font-* → safe-family overlay —
	// validated in full when present (font-token whitelist + closed
	// safe-family stack allowlist), a no-op when absent.
	if err := validateFonts(b.Fonts, where); err != nil {
		return err
	}
	// avatars (T-16a1 P5) is an OPTIONAL per-member-type embedded-image
	// overlay — validated in full when present (kind whitelist + data-URI /
	// raster-mime / size / magic-byte gate), a no-op when absent.
	if err := validateAvatars(b.Avatars, where); err != nil {
		return err
	}
	// logo (T-ea81) is an OPTIONAL single studio-logo image and navIcons an
	// OPTIONAL per-tab icon overlay — both reuse the same avatar image gate,
	// validated in full when present, a no-op when absent.
	if err := validateLogo(b.Logo, where); err != nil {
		return err
	}
	if err := validateNavIcons(b.NavIcons, where); err != nil {
		return err
	}
	// backgrounds (T-081b) is an OPTIONAL outer-canvas tiled-image overlay —
	// same avatar image gate again, validated in full when present, a no-op
	// when absent.
	if err := validateBackgrounds(b.Backgrounds, where); err != nil {
		return err
	}
	// backgroundModes (T-081b) says HOW each of those images is laid down
	// (tile / sides). Absent = every zone tiles, i.e. the behaviour that
	// predates the field, so an older bundle is unaffected.
	if err := validateBackgroundModes(
		b.BackgroundModes, b.Backgrounds, where,
	); err != nil {
		return err
	}
	return nil
}

// [T-83ef] `themeBundleIDSet` and `isValidDisplayTheme` lived here and are gone.
// They answered "is this a real theme id" by building a set out of the bundle
// ARRAY that settings used to carry — a question that cannot be asked that way
// any more, because settings no longer carries the bundles. The replacement is
// `(*apiServer).displayThemeExists` in api_themes.go, which asks the
// custom_theme table, and it is pinned by TestDisplayThemeIsValidatedAgainstThe
// Table using an id that exists ONLY in the table.
//
// Worth the note rather than a silent delete: Go does not fail a build over an
// unused unexported func, so a helper stranded by a refactor sits here looking
// current. These two survived the whole of this ticket and were found by asking
// the compiler (delete it and see), not by reading.
