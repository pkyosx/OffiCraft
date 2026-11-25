// themeWording.ts — the wording-overlay half of the theme grammar, split out of
// themeBundle.ts so the PRE-PAINT path can validate colours/fonts/images
// without pulling in messageKeys.generated.ts (876 lines / ~25 kB) that only the
// wording rules need. The grammar itself is UNCHANGED and still single-sourced:
// this file is the only implementation of the wording rules, and themeBundle.ts
// composes it back into validateThemeBundle for every non-paint caller.
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";
import {
  MAX_WORDING_VALUE_LEN,
  WORDING_LANGS,
  hasControlChar,
  runeCount,
} from "./themeBundleCore";

const WORDING_LANG_SET = new Set<string>(WORDING_LANGS);
const MESSAGE_KEY_SET = new Set<string>(MESSAGE_KEYS);

/** Validate a bundle's optional wording overlay (T-16a1 P3) — the twin of the
 * Go validateWording. Returns an error message, or null when admissible (an
 * absent overlay is admissible). Every rule but one is strict; an unknown
 * message code is dropped from `wording` IN PLACE (T-081b) so the overlay that
 * reaches the caller — and the store — holds only live codes.
 *
 * `skipped` is an OUT-parameter warning channel: every dropped code is appended
 * to it (once, even when several languages override the same code) so the
 * import UI can name what it silently threw away. It is deliberately separate
 * from the return value — a dropped code is a WARNING, never a rejection. */
export function validateWording(
  wording: unknown,
  where = "theme",
  skipped?: string[]
): string | null {
  if (wording === undefined || wording === null) return null;
  if (typeof wording !== "object" || Array.isArray(wording)) {
    return `${where}: wording must be an object`;
  }
  for (const [lang, entries] of Object.entries(
    wording as Record<string, unknown>
  )) {
    if (!WORDING_LANG_SET.has(lang)) {
      return `${where}: wording language "${lang}" is not allowed (only zh, en)`;
    }
    if (typeof entries !== "object" || entries === null || Array.isArray(entries)) {
      return `${where}: wording[${lang}] must be an object`;
    }
    const pairs = Object.entries(entries as Record<string, unknown>);
    for (const [code, value] of pairs) {
      if (!MESSAGE_KEY_SET.has(code)) {
        // An unrecognised code is DROPPED here, not rejected (owner ruling
        // 2026-07-27, rc-1599a0026a80): T-081b removed the theme-identity keys
        // from the whitelist, and a pack that overrode one must stay importable.
        // Dropping in place keeps the entry out of the stored bundle and out of
        // the applied overlay, so a re-export carries only live codes. The
        // accepted cost: a theme author's typo does nothing — but it is
        // REPORTED through `skipped`, so the import UI can say so.
        delete (entries as Record<string, unknown>)[code];
        if (skipped && !skipped.includes(code)) skipped.push(code);
        continue;
      }
      if (typeof value !== "string" || hasControlChar(value)) {
        return `${where}: wording[${lang}][${code}] must not contain control characters`;
      }
      const n = runeCount(value.trim());
      if (n < 1 || n > MAX_WORDING_VALUE_LEN) {
        return `${where}: wording[${lang}][${code}] must be 1..${MAX_WORDING_VALUE_LEN} characters after trimming`;
      }
    }
  }
  return null;
}

