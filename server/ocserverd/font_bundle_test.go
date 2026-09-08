// Skeleton generated from server/ocserverd/font_bundle.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"strings"
	"testing"
)

func TestValidFontValue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "a curated sans stack is accepted",
			value: `"Noto Sans TC", "Noto Sans", system-ui, sans-serif`,
			want:  true,
		},
		{
			name:  "a curated monospace stack is accepted",
			value: `ui-monospace, "SF Mono", Menlo, Consolas, monospace`,
			want:  true,
		},
		{name: "an empty value is refused", value: "", want: false},
		{name: "an arbitrary family is refused", value: "Arial", want: false},
		{name: "a trailing space defeats exact allowlist membership", value: `"Noto Sans TC", "Noto Sans", system-ui, sans-serif `, want: false},
		{name: "a URL injection is refused", value: `url("https://evil.example/font.woff2")`, want: false},
		{name: "a CSS at-rule is refused", value: "@font-face{font-family:x}", want: false},
		{name: "a CSS declaration is refused", value: "system-ui;}", want: false},
		{name: "a variable expression is refused", value: "var(--font)", want: false},
		{name: "a value over the length cap is refused", value: strings.Repeat("f", 129), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validFontValue(tc.value); got != tc.want {
				t.Fatalf("validFontValue(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestValidateFonts(t *testing.T) {
	if err := validateFonts(nil, "theme[0]"); err != nil {
		t.Fatalf("nil fonts must be accepted: %v", err)
	}
	empty := map[string]string{}
	if err := validateFonts(&empty, "theme[0]"); err != nil {
		t.Fatalf("an empty fonts overlay must be accepted: %v", err)
	}

	valid := map[string]string{
		"--font-sans":  `"Noto Sans TC", "Noto Sans", system-ui, sans-serif`,
		"--font-title": `"Noto Serif TC", Georgia, "Times New Roman", serif`,
	}
	if err := validateFonts(&valid, "theme[0]"); err != nil {
		t.Fatalf("valid fonts must be accepted: %v", err)
	}

	unknownToken := map[string]string{"--color-bg": valid["--font-sans"]}
	if err := validateFonts(&unknownToken, "theme[0]"); err == nil || err.Error() != `theme[0]: "--color-bg" is not a theme font token (only --font-sans / --font-title)` {
		t.Fatalf("unknown font token error = %v", err)
	}

	invalidValue := map[string]string{"--font-sans": "Arial"}
	if err := validateFonts(&invalidValue, "theme[0]"); err == nil || err.Error() != `theme[0]: "--font-sans" has an invalid font value "Arial" — only a safe built-in font family may be chosen` {
		t.Fatalf("invalid font value error = %v", err)
	}
}
