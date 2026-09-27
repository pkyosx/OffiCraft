package main

// Server-side validation of a theme bundle's optional `wording` overlay
// { <lang>: { <code>: <text> } }. It mirrors, rule for rule, the client
// validator in frontend/src/lib/themeBundle.ts (shared with the mock API), so an
// overlay rejected offline is rejected online for the identical reason. The code
// whitelist is messageKeys (message_keys_gen.go, extracted from locales/en.ts).
// Values reach the UI as escaped React children — no injection surface.

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxWordingValueLen = 200
	// 🔴 The cap is measured on the RAW submitted map, BEFORE unknown codes are
	// pruned, so it must sit ABOVE len(messageKeys): a pack re-wording every key
	// must not refuse itself. 2000 is owner ruling rc-e5780adf4f1d; it STAYS a
	// hard-coded number (the owner did not pick a computed one) — ask the owner
	// again in about four months, his chosen cadence. The whitelist grows ~7 keys
	// A DAY (an earlier ~10-a-month estimate was measured wrong). Raise together with
	// MAX_WORDING_ENTRIES_PER_LANG in frontend/src/lib/themeBundleCore.ts; they
	// are asserted equal.
	maxWordingEntriesPerLang = 2000
)

var wordingLangAllowed = map[string]bool{"zh": true, "en": true}

func validateWording(wording *map[string]map[string]string, where string) error {
	if wording == nil {
		return nil
	}
	// Language set and the RAW entry cap first, before any pruning — junk keys
	// cannot slip past the cap by being dropped. Same rule order as the TS twin.
	for lang, entries := range *wording {
		if !wordingLangAllowed[lang] {
			return fmt.Errorf(
				"%s: wording language %q is not allowed (only zh, en)", where, lang)
		}
		if len(entries) > maxWordingEntriesPerLang {
			return fmt.Errorf(
				"%s: wording[%s] holds more than %d entries", where, lang, maxWordingEntriesPerLang)
		}
	}
	dropUnknownWordingCodes(*wording, where)
	for lang, entries := range *wording {
		for code, value := range entries {
			if err := validateWordingValue(value); err != nil {
				return fmt.Errorf("%s: wording[%s][%s] %v", where, lang, code, err)
			}
		}
	}
	return nil
}

const maxLoggedDroppedCodes = 10

const maxLoggedDroppedCodeLen = 80

// The DROP (rather than a 422) is owner ruling rc-1599a0026a80: a pack that
// overrode a since-removed key must stay importable. The author gets no warning
// (the PATCH response shape is frozen wire), but every drop leaves a server-log
// trail — this repo's decoder principle (api_helpers.go) forbids silent drops.
func dropUnknownWordingCodes(wording map[string]map[string]string, where string) {
	dropped := map[string]bool{}
	for _, entries := range wording {
		for code := range entries {
			if !messageKeys[code] {
				delete(entries, code)
				dropped[code] = true
			}
		}
	}
	if len(dropped) == 0 {
		return
	}
	codes := make([]string, 0, len(dropped))
	for code := range dropped {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	shown := codes
	more := ""
	if len(shown) > maxLoggedDroppedCodes {
		shown = shown[:maxLoggedDroppedCodes]
		more = ", …"
	}
	// A dropped code is ATTACKER-CONTROLLED text: %q stops an imported theme
	// forging extra server-log lines; the truncation bounds its length.
	quoted := make([]string, len(shown))
	for i, code := range shown {
		if len(code) > maxLoggedDroppedCodeLen {
			code = code[:maxLoggedDroppedCodeLen] + "…"
		}
		quoted[i] = fmt.Sprintf("%q", code)
	}
	log.Printf("[theme] %s: dropped %d unrecognised wording code(s): %s%s",
		where, len(codes), strings.Join(quoted, ", "), more)
}

func validateWordingValue(value string) error {
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("must not contain control characters")
		}
	}
	trimmed := strings.TrimSpace(value)
	if n := utf8.RuneCountInString(trimmed); n < 1 || n > maxWordingValueLen {
		return fmt.Errorf("must be 1..%d characters after trimming", maxWordingValueLen)
	}
	return nil
}
