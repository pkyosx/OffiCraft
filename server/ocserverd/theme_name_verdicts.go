package main

// theme_name_verdicts.go — the Go HALF of the cross-language theme-name parity
// check. frontend/src/lib/themeName.parity.test.ts drives it and does the
// comparing; this side only reports what THIS validator says about each name.
//
// 🔴 WHY IT IS A SUBCOMMAND AND NOT A TEST. It asserts nothing — it reads a case
// file and writes a verdict file — and until T-125 it was a `go test` carrying a
// build tag purely so that an ordinary `go test ./...` would not see a thing
// that is not a test. A subcommand needs no tag to stay out of the suite, and
// `ocserverd theme-name-verdicts` says what it is in its own name.
//
// 🔴 WHY THE CROSS-CHECK EXISTS AT ALL (T-081b review round 4, SHOULD-C). The
// two ends read Unicode CATEGORIES out of two different runtimes' tables — Go's
// `unicode` package versus the JS engine's property escapes. That is the point:
// neither side hand-keeps a codepoint list. But it also means the two can drift
// apart on a Unicode version bump, silently, in exactly the direction that
// matters — a character one side calls Cf and the other does not is a
// server-ACCEPT / client-REJECT split, and the server is the authority. So the
// same corpus goes through BOTH validators in one run and any divergence in
// verdict OR in reason fails on the TS side.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// themeBundleNamed returns a minimal, otherwise-valid bundle carrying `name`, so
// each case isolates the NAME rule under test.
func themeBundleNamed(name string) []ThemeBundleDTO {
	return []ThemeBundleDTO{{
		Id:     "midnight",
		Name:   name,
		Colors: map[string]string{"--color-bg": "#101018"},
	}}
}

// cmdThemeNameVerdicts reads the shared case file and writes this side's verdict
// for every name in it.
//
// ⚠️ AN EMPTY CORPUS IS REFUSED rather than answered with an empty verdict map.
// A parity comparison over nothing agrees perfectly, and "the two ends never
// disagreed" is precisely the sentence a silently-emptied case file would buy.
// The TS side has its own floor on the case count; this is the other end of it.
func cmdThemeNameVerdicts(args []string, out io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(out, "usage: ocserverd theme-name-verdicts <cases.json> <verdicts.json>")
		fmt.Fprintln(out, "  cases.json    [{\"k\":\"<key>\",\"n\":\"<name>\"}, …] — the shared corpus")
		fmt.Fprintln(out, "  verdicts.json written: {\"<key>\": \"ACCEPT\"|\"REJECT: <reason>\", …}")
		fmt.Fprintln(out, "Driven by frontend/src/lib/themeName.parity.test.ts.")
		return 2
	}
	in, dst := args[0], args[1]
	raw, err := os.ReadFile(in)
	if err != nil {
		fmt.Fprintf(out, "[theme-name-verdicts] read cases %s: %v\n", in, err)
		return 1
	}
	var cases []struct {
		K string `json:"k"`
		N string `json:"n"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		fmt.Fprintf(out, "[theme-name-verdicts] parse cases %s: %v\n", in, err)
		return 1
	}
	if len(cases) == 0 {
		fmt.Fprintf(out, "[theme-name-verdicts] %s carries no cases — a parity comparison over an "+
			"empty corpus agrees perfectly and proves nothing, so this is a failure, not an "+
			"empty answer.\n", in)
		return 1
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
		fmt.Fprintf(out, "[theme-name-verdicts] marshal verdicts: %v\n", err)
		return 1
	}
	if err := os.WriteFile(dst, blob, 0o600); err != nil {
		fmt.Fprintf(out, "[theme-name-verdicts] write %s: %v\n", dst, err)
		return 1
	}
	fmt.Fprintf(out, "[theme-name-verdicts] wrote %s: %d verdict(s)\n", dst, len(verdicts))
	return 0
}
