package main

// wording_bundle.go — T-16a1 P3: server-side validation of a theme bundle's
// optional `wording` overlay (per-language message-key text overrides). The
// overlay is `{ <lang>: { <code>: <replacement text> } }`:
//
//   - the LANGUAGE key is `zh` or `en` (the built-in `xian` theme carries its
//     own copy and is NOT part of the user override layer — P3 decisions);
//   - the CODE key must be an i18n message code in the generated whitelist
//     (messageKeys, message_keys_gen.go — extracted from locales/en.ts, the
//     single source of truth shared with the client + mock);
//   - the VALUE is PLAIN TEXT: trimmed to 1..200 runes, no control characters
//     or newlines. The value reaches the UI as React children (escaped), so
//     there is no HTML/CSS injection surface — but we still cap length, bound
//     the per-language entry count, and reject control characters.
//
// This mirrors, rule for rule, the client validator in
// frontend/src/lib/themeBundle.ts (shared with the mock API), so a wording
// overlay rejected offline is rejected online for the identical reason.
// wording is OPTIONAL: an absent overlay is fine; a present one is validated
// in full. Any violation is a 422 — never silently dropped, never stored.

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// maxWordingValueLen caps one override string (runes). A UI label, not a
	// document — 200 runes is generous for any menu/button/heading copy.
	maxWordingValueLen = 200
	// maxWordingEntriesPerLang bounds one language's override map. The message-key
	// whitelist is the real ceiling (a JSON object cannot repeat a key, so a
	// language can never carry more distinct valid codes than the whitelist holds);
	// this explicit cap bounds the stored JSON row regardless of whitelist growth.
	maxWordingEntriesPerLang = 1000
)

// wordingLangAllowed is the closed set of override languages. `xian` is a
// built-in theme's own copy, not a user override layer (P3 decisions), so it is
// intentionally excluded.
var wordingLangAllowed = map[string]bool{"zh": true, "en": true}

// validateWording validates one bundle's optional wording overlay. `where` is
// the caller's bundle locator (e.g. "custom_themes[2]") for a precise 422
// message. A nil overlay is admissible (wording is optional).
func validateWording(wording *map[string]map[string]string, where string) error {
	if wording == nil {
		return nil
	}
	for lang, entries := range *wording {
		if !wordingLangAllowed[lang] {
			return fmt.Errorf(
				"%s: wording language %q is not allowed (only zh, en)", where, lang)
		}
		if len(entries) > maxWordingEntriesPerLang {
			return fmt.Errorf(
				"%s: wording[%s] holds more than %d entries", where, lang, maxWordingEntriesPerLang)
		}
		for code, value := range entries {
			if !messageKeys[code] {
				return fmt.Errorf(
					"%s: wording[%s] key %q is not a known message code", where, lang, code)
			}
			if err := validateWordingValue(value); err != nil {
				return fmt.Errorf("%s: wording[%s][%s] %v", where, lang, code, err)
			}
		}
	}
	return nil
}

// validateWordingValue enforces the plain-text value rules: 1..200 runes after
// trimming, and no control characters (newlines included) anywhere in the raw
// value.
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
