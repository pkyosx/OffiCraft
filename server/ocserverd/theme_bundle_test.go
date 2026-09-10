// Skeleton generated from server/ocserverd/theme_bundle.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"fmt"
	"strings"
	"testing"
)

func themeBundleForTest(id, name, color string) ThemeBundleDTO {
	return ThemeBundleDTO{
		Id:     id,
		Name:   name,
		Colors: map[string]string{"--color-bg": color},
	}
}

func TestHasInvisibleNameRune(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{name: "ordinary Unicode text is visible", text: "精靈村", want: false},
		{name: "a Cc control is rejected", text: "Mid\x00night", want: true},
		{name: "a Cf format character is rejected", text: "Mid\u200bnight", want: true},
		{name: "a Co private-use character is rejected", text: "Mid\ue000night", want: true},
		{name: "a line separator is rejected", text: "Mid\u2028night", want: true},
		{name: "a paragraph separator is rejected", text: "Mid\u2029night", want: true},
		{name: "a variation selector remains allowed", text: "Heart ❤️", want: false},
		{name: "a space separator is normalized elsewhere rather than rejected", text: "深海\u3000之夜", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasInvisibleNameRune(tc.text); got != tc.want {
				t.Fatalf("hasInvisibleNameRune(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestNormalizeThemeSpaces(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{name: "ideographic and no-break spaces become ASCII spaces", text: "深海\u3000之夜\u00a0", want: "深海 之夜 "},
		{name: "the other space separators are also folded", text: "\u1680A\u2000B\u2001C\u202fD", want: " A B C D"},
		{name: "letters and control characters outside Zs remain unchanged", text: "A\nB\tC", want: "A\nB\tC"},
		{name: "an ordinary ASCII space remains an ASCII space", text: "left right", want: "left right"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeThemeSpaces(tc.text); got != tc.want {
				t.Fatalf("normalizeThemeSpaces(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestValidColorValue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "three-digit hex is accepted", value: "#fff", want: true},
		{name: "four-digit hex is accepted", value: "#1234", want: true},
		{name: "six-digit hex is accepted", value: "#12aBcD", want: true},
		{name: "eight-digit hex is accepted", value: "#12aBcD80", want: true},
		{name: "rgb with commas is accepted", value: "rgb(1, 20, 255)", want: true},
		{name: "modern rgba with percent and slash is accepted", value: "rgba(10% 20% 30% / 50%)", want: true},
		{name: "hsl with degree units is accepted", value: "hsl(120deg, 50%, 50%)", want: true},
		{name: "hsla with turn units is accepted", value: "hsla(1turn 50% 50% / 25%)", want: true},
		{name: "transparent is accepted", value: "transparent", want: true},
		{name: "an empty value is refused", value: "", want: false},
		{name: "a named color other than transparent is refused", value: "red", want: false},
		{name: "a short hex value is refused", value: "#12", want: false},
		{name: "a seven-digit hex value is refused", value: "#1234567", want: false},
		{name: "a variable expression is refused", value: "var(--color-bg)", want: false},
		{name: "a URL expression is refused", value: "url(https://evil.example/x)", want: false},
		{name: "a CSS declaration is refused", value: "#fff; color: red", want: false},
		{name: "a value beyond the length cap is refused", value: strings.Repeat("a", 65), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validColorValue(tc.value); got != tc.want {
				t.Fatalf("validColorValue(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestValidateThemeBundles(t *testing.T) {
	if err := validateThemeBundles([]ThemeBundleDTO{
		themeBundleForTest("midnight", "Midnight", "#101018"),
	}); err != nil {
		t.Fatalf("a valid theme bundle must be accepted: %v", err)
	}

	for _, tc := range []struct {
		name    string
		bundles []ThemeBundleDTO
		want    string
	}{
		{
			name: "duplicate IDs are refused at the second position",
			bundles: []ThemeBundleDTO{
				themeBundleForTest("midnight", "First", "#101018"),
				themeBundleForTest("midnight", "Second", "#202028"),
			},
			want: `custom_themes[1]: duplicate id "midnight"`,
		},
		{
			name: "an invalid ID is refused before the bundle name",
			bundles: []ThemeBundleDTO{
				themeBundleForTest("x", "Valid name", "#101018"),
			},
			want: `custom_themes[0]: id must match ^[a-z0-9][a-z0-9-]{1,63}$ (got "x")`,
		},
		{
			name: "the built-in ID is reserved",
			bundles: []ThemeBundleDTO{
				themeBundleForTest("office", "Custom copy", "#101018"),
			},
			want: `custom_themes[0]: id "office" is reserved for a built-in theme`,
		},
		{
			name: "an unknown color token is refused",
			bundles: []ThemeBundleDTO{{
				Id: "midnight", Name: "Midnight", Colors: map[string]string{"--not-a-color": "#101018"},
			}},
			want: `custom_themes[0]: "--not-a-color" is not a theme colour token (see theme.css)`,
		},
		{
			name: "a CSS expression is refused as a color value",
			bundles: []ThemeBundleDTO{{
				Id: "midnight", Name: "Midnight", Colors: map[string]string{"--color-bg": "var(--color-card)"},
			}},
			want: `custom_themes[0]: "--color-bg" has an invalid colour value "var(--color-card)" — only concrete hex / rgb() / rgba() / hsl() / hsla() / transparent are accepted`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateThemeBundles(tc.bundles); err == nil || err.Error() != tc.want {
				t.Fatalf("validateThemeBundles error = %v, want %q", err, tc.want)
			}
		})
	}

	withinCap := make([]ThemeBundleDTO, 100)
	for i := range withinCap {
		withinCap[i] = themeBundleForTest(fmt.Sprintf("theme-%03d", i), fmt.Sprintf("Theme %03d", i), "#101018")
	}
	if err := validateThemeBundles(withinCap); err != nil {
		t.Fatalf("100 valid themes must be accepted: %v", err)
	}
	tooMany := append(withinCap, themeBundleForTest("theme-100", "Theme 100", "#101018"))
	if err := validateThemeBundles(tooMany); err == nil || err.Error() != "custom_themes must hold at most 100 themes" {
		t.Fatalf("101 themes error = %v, want the 100-theme cap", err)
	}
}

func TestValidateThemeBundle(t *testing.T) {
	t.Run("a valid bundle is accepted and records its ID in the set", func(t *testing.T) {
		seen := map[string]bool{}
		if err := validateThemeBundle(themeBundleForTest("midnight", "Midnight", "#101018"), "theme", seen); err != nil {
			t.Fatalf("validateThemeBundle returned an error: %v", err)
		}
		if !seen["midnight"] {
			t.Fatalf("validateThemeBundle did not record the accepted ID: %#v", seen)
		}
	})

	for _, tc := range []struct {
		name string
		b    ThemeBundleDTO
		seen map[string]bool
		want string
	}{
		{
			name: "a duplicate is reported before a later invalid name",
			b:    themeBundleForTest("midnight", "", "#101018"),
			seen: map[string]bool{"midnight": true},
			want: `theme: duplicate id "midnight"`,
		},
		{
			name: "a duplicate is not checked when no set is supplied",
			b:    themeBundleForTest("midnight", "Another name", "#101018"),
			seen: nil,
		},
		{
			name: "an ID shorter than two characters is refused",
			b:    themeBundleForTest("x", "Midnight", "#101018"),
			want: `theme: id must match ^[a-z0-9][a-z0-9-]{1,63}$ (got "x")`,
		},
		{
			name: "the built-in ID is refused",
			b:    themeBundleForTest("office", "Office copy", "#101018"),
			want: `theme: id "office" is reserved for a built-in theme`,
		},
		{
			name: "a name that trims to empty is refused",
			b:    themeBundleForTest("midnight", " \u3000 ", "#101018"),
			want: `theme: name must be 1..80 characters after trimming`,
		},
		{
			name: "a name over eighty runes is refused",
			b:    themeBundleForTest("midnight", strings.Repeat("n", 81), "#101018"),
			want: `theme: name must be 1..80 characters after trimming`,
		},
		{
			name: "an invisible format character is refused",
			b:    themeBundleForTest("midnight", "Mid\u200bnight", "#101018"),
			want: `theme: name must not contain control, formatting, private-use, surrogate or line/paragraph separator characters`,
		},
		{
			name: "an empty colors map is refused",
			b:    ThemeBundleDTO{Id: "midnight", Name: "Midnight", Colors: map[string]string{}},
			want: `theme: colors must hold 1..200 entries (got 0)`,
		},
		{
			name: "a colors map over two hundred entries is refused before token checks",
			b: func() ThemeBundleDTO {
				colors := make(map[string]string, 201)
				for i := 0; i < 201; i++ {
					colors[fmt.Sprintf("--color-%03d", i)] = "#101018"
				}
				return ThemeBundleDTO{Id: "midnight", Name: "Midnight", Colors: colors}
			}(),
			want: `theme: colors must hold 1..200 entries (got 201)`,
		},
		{
			name: "an unknown color token is refused",
			b:    ThemeBundleDTO{Id: "midnight", Name: "Midnight", Colors: map[string]string{"--not-a-color": "#101018"}},
			want: `theme: "--not-a-color" is not a theme colour token (see theme.css)`,
		},
		{
			name: "a non-concrete color is refused",
			b:    ThemeBundleDTO{Id: "midnight", Name: "Midnight", Colors: map[string]string{"--color-bg": "var(--x)"}},
			want: `theme: "--color-bg" has an invalid colour value "var(--x)" — only concrete hex / rgb() / rgba() / hsl() / hsla() / transparent are accepted`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateThemeBundle(tc.b, "theme", tc.seen); err == nil {
				if tc.want != "" {
					t.Fatalf("validateThemeBundle returned nil, want %q", tc.want)
				}
			} else if err.Error() != tc.want {
				t.Fatalf("validateThemeBundle error = %q, want %q", err, tc.want)
			}
		})
	}
}
