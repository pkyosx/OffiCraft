//go:build octool

// theme_name_parity_tool_test.go — NOT A TEST. This is the Go HALF of the
// cross-language theme-name parity check; frontend/src/lib/themeName.parity.test.ts
// drives it and does the comparing. It carries the octool build tag so an ordinary
// `go test ./...` never sees it. Extracted from theme_bundle_test.go (T-125).

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// themeBundleNamed returns a minimal, otherwise-valid bundle carrying `name`, so
// each case below isolates the NAME rule under test.
func themeBundleNamed(name string) []ThemeBundleDTO {
	return []ThemeBundleDTO{{
		Id:     "midnight",
		Name:   name,
		Colors: map[string]string{"--color-bg": "#101018"},
	}}
}

// TestThemeNameVerdictsEmit is the Go HALF of the cross-language name-parity
// safety net (T-081b review round 4, SHOULD-C). It is not an assertion: driven
// by the two env vars below it reads a shared case file and writes THIS side's
// verdict for every name, so frontend/src/lib/themeName.parity.test.ts can feed
// the SAME 61 names to both validators and fail on any divergence.
//
// A cross-check is needed because the two ends now read Unicode CATEGORIES out
// of two different runtimes' tables (Go's `unicode` package vs the JS engine's
// property escapes). That is the point — neither side hand-keeps a codepoint
// list — but it also means the two could drift apart on a Unicode version bump,
// silently, in exactly the direction that matters: a character one side calls
// Cf and the other does not. Without the env vars the test no-ops, so an
// ordinary `go test ./...` is unaffected.
func TestThemeNameVerdictsEmit(t *testing.T) {
	in, out := os.Getenv("OC_THEME_NAME_CASES"), os.Getenv("OC_THEME_NAME_VERDICTS")
	if in == "" || out == "" {
		t.Skip("OC_THEME_NAME_CASES / OC_THEME_NAME_VERDICTS unset — driven by themeName.parity.test.ts")
	}
	raw, err := os.ReadFile(in)
	if err != nil {
		t.Fatalf("read cases: %v", err)
	}
	var cases []struct {
		K string `json:"k"`
		N string `json:"n"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse cases: %v", err)
	}
	verdicts := make(map[string]string, len(cases))
	for _, c := range cases {
		if err := validateThemeBundles(themeBundleNamed(c.N)); err != nil {
			verdicts[c.K] = "REJECT: " + strings.TrimPrefix(err.Error(), "custom_themes[0]: ")
		} else {
			verdicts[c.K] = "ACCEPT"
		}
	}
	blob, err := json.Marshal(verdicts)
	if err != nil {
		t.Fatalf("marshal verdicts: %v", err)
	}
	if err := os.WriteFile(out, blob, 0o600); err != nil {
		t.Fatalf("write verdicts: %v", err)
	}
}
