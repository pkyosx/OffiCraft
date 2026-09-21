package main

// wording_bundle.go — T-16a1 P3: server-side validation of a theme bundle's
// optional `wording` overlay (per-language message-key text overrides). The
// overlay is `{ <lang>: { <code>: <replacement text> } }`:
//
//   - the LANGUAGE key is `zh` or `en`;
//   - the CODE key should be an i18n message code in the generated whitelist
//     (messageKeys, message_keys_gen.go — extracted from locales/en.ts, the
//     single source of truth shared with the client + mock). A code OUTSIDE the
//     whitelist is DROPPED rather than rejected (T-081b) — see validateWording;
//   - the VALUE is PLAIN TEXT: trimmed to 1..200 runes, no control characters
//     or newlines. The value reaches the UI as React children (escaped), so
//     there is no HTML/CSS injection surface — but we still cap length, bound
//     the per-language entry count, and reject control characters.
//
// This mirrors, rule for rule, the client validator in
// frontend/src/lib/themeBundle.ts (shared with the mock API), so a wording
// overlay rejected offline is rejected online for the identical reason.
// wording is OPTIONAL: an absent overlay is fine; a present one is validated
// in full. Any violation is a 422 — never silently dropped, never stored. The
// one exception is the unknown-code case above, which is dropped from the
// overlay so the REST of the bundle is accepted.

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// maxWordingValueLen caps one override string (runes). A UI label, not a
	// document — 200 runes is generous for any menu/button/heading copy.
	maxWordingValueLen = 200
	// maxWordingEntriesPerLang bounds one language's override map. validateWording
	// measures the RAW submitted map BEFORE unknown codes are pruned, so this is
	// not "how big may the whitelist get" — it is "how many raw entries may one
	// submission carry". The binding constraint is therefore that the cap must sit
	// ABOVE the whitelist length: a legitimate pack that re-words EVERY message key
	// submits exactly len(messageKeys) entries, and a cap at or below that number
	// makes that pack refuse itself.
	//
	// 2000 — owner ruling 2026-08-31 on card rc-e5780adf4f1d, replacing the 1200
	// of 2026-08-11.
	//
	// ⚠️ THE 1200 RESTED ON A GROWTH RATE THAT WAS MEASURED WRONG, and the wrong
	// sentence is kept here rather than quietly overwritten. It read: 「1200
	// against a whitelist of ~1009: ~190 spare entries is a year-odd of headroom
	// at the ~10-keys-a-month growth actually measured」. The measured rate is
	// about 7 keys A DAY, not 10 a month — the whitelist went 1,009 (2026-08-11)
	// → 1,149 (2026-08-31), +140 in 20 days, roughly 20x the estimate. So the
	// 「a year-odd of headroom」 was gone inside those same 20 days, and T-36 is
	// what ran it out.
	//
	// PRECISELY WHAT WENT RED, because "ran out" is easy to misread: the cap 1200
	// was never EXCEEDED (the whitelist is far below it). What failed is the
	// 50-entry spare the mirror test demands ABOVE the whitelist: it asserts
	// cap >= len(messageKeys) + 50. T-36's first two keys
	// (chat.mdPreview.openInNewTab, chat.mdPreview.newTabStaticNote) took the
	// whitelist 1,149 → 1,151, so 1200 − 1151 = 49 < 50 and that assertion is the
	// one that went red. T-36 ships THREE keys in total — the third,
	// chat.mdPreview.unavailableOpenInNewTab, landed after this cap was raised —
	// leaving the whitelist at 1,152. Whoever sizes this cap next needs to know the
	// old estimate was wrong, not merely what the number is today.
	//
	// At the measured rate 2000 buys about FOUR MONTHS: (2000 − 1152) = 848 spare
	// entries ÷ ~7/day ≈ 121 days. ASK THE OWNER AGAIN THEN — he chose that
	// cadence deliberately (「這次拉到 2000 就好，照實際速度大約再撐四個月，到時
	// 再問你一次」).
	//
	// 🔴 IT STAYS A HARD-CODED NUMBER. Making the cap track the whitelist length
	// automatically was on the same card as an option and the owner did NOT pick
	// it. Do not "improve" this into a computed value.
	//
	// Raise this together with its TypeScript twin (MAX_WORDING_ENTRIES_PER_LANG in
	// frontend/src/lib/themeBundleCore.ts); they are asserted equal, so moving one
	// alone is red.
	maxWordingEntriesPerLang = 2000
)

// wordingLangAllowed is the closed set of override languages (the two UI
// languages). Any other language key is a 422.
var wordingLangAllowed = map[string]bool{"zh": true, "en": true}

// validateWording validates one bundle's optional wording overlay. `where` is
// the caller's bundle locator (e.g. "custom_themes[2]") for a precise 422
// message. A nil overlay is admissible (wording is optional). Unknown codes are
// deleted from the overlay IN PLACE, so the caller stores only live codes; the
// per-language entry cap is still measured against the RAW submitted map, so a
// pack cannot smuggle thousands of junk keys past it.
func validateWording(wording *map[string]map[string]string, where string) error {
	if wording == nil {
		return nil
	}
	// Language set and the RAW entry cap first — both are measured before any
	// pruning, so a pack cannot smuggle thousands of junk keys past the cap by
	// having them dropped. This is also the rule order the TS twin applies.
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

// maxLoggedDroppedCodes caps how many dropped codes one log line names. The
// count carries the rest — a 1000-key junk pack must not write a 1000-key line.
const maxLoggedDroppedCodes = 10

// maxLoggedDroppedCodeLen caps the LENGTH of each code the line names. The
// entry-count cap bounds how many codes a pack can carry, but nothing bounds how
// long one is, so without this a single key could still flood the log.
const maxLoggedDroppedCodeLen = 80

// dropUnknownWordingCodes deletes every code outside the messageKeys whitelist
// from the overlay IN PLACE and reports what it removed.
//
// The DROP itself is the owner's ruling (2026-07-27, rc-1599a0026a80): T-081b
// removed the theme-identity keys from the whitelist, and a pack that overrode
// one must stay importable, so an unrecognised code is not a 422. Deleting from
// the decoded map keeps the entry out of the stored JSON and out of the applied
// overlay, so a re-export carries only live codes.
//
// The silence toward the THEME AUTHOR is the accepted cost of that ruling (the
// PATCH response shape is frozen wire, so there is no warning channel on it).
// The silence toward the OPERATOR is not: this repo's own decoder principle
// (api_helpers.go — "any key the DTO does not declare is a 422, not a silent
// drop") was written after a silent drop wiped a document, so every drop leaves
// a server-log trail naming the bundle and the codes.
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
	// A dropped code is ATTACKER-CONTROLLED text (it is precisely the input the
	// whitelist refused), so it is quoted, not interpolated raw: %q escapes the
	// newlines and control characters that would otherwise let an imported theme
	// forge whole extra lines in the server log. Each code is also truncated —
	// the cap on entries bounds their number, nothing bounds their length.
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
