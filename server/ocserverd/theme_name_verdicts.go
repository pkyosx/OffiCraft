package main

// Go half of the cross-language theme-name parity check
// (frontend/src/lib/themeName.parity.test.ts compares). Go's `unicode` tables and
// the JS engine's property escapes can drift apart on a Unicode version bump —
// a server-ACCEPT / client-REJECT split, silently — so one corpus runs through
// both validators; the server is the authority.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func themeBundleNamed(name string) []ThemeBundleDTO {
	return []ThemeBundleDTO{{
		Id:     "midnight",
		Name:   name,
		Colors: map[string]string{"--color-bg": "#101018"},
	}}
}

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
