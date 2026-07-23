// themeBundle.ts — T-16a1 P2: the client-side theme-bundle validator, the
// character-for-character twin of the server grammar (server/ocserverd/
// theme_bundle.go). It is the SINGLE client implementation shared by the import
// UI (P2b), the theme picker (P2b), and the mock API's PATCH parity check — so
// a bundle rejected offline is rejected online for the identical reason.
//
// The security boundary is the colour VALUE. A bundle is applied via
// element.style.setProperty(name, value) — never concatenated into a stylesheet
// — but we still admit only CONCRETE colours through a strict allowlist grammar
// and constrain the token NAME to the generated theme.css whitelist.

import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";
import {
  THEME_FONT_TOKENS,
  SAFE_FONT_FAMILIES,
} from "../styles/themeFonts.generated";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";

/** One owner-authored theme colour bundle (mirrors ThemeBundleDTO). `wording`
 * is an OPTIONAL per-language message-key text-override overlay (T-16a1 P3).
 * `fonts` is an OPTIONAL --font-* → safe-family-stack overlay (T-16a1 P4). */
export interface ThemeBundle {
  id: string;
  name: string;
  colors: Record<string, string>;
  wording?: Record<string, Record<string, string>>;
  fonts?: Record<string, string>;
}

export const MAX_COLOR_VALUE_LEN = 64;
export const MIN_THEME_COLORS = 1;
export const MAX_THEME_COLORS = 200;
export const MAX_CUSTOM_THEMES = 100;
export const MAX_THEME_NAME_LEN = 80;

// Wording overlay bounds (T-16a1 P3) — the character-for-character twin of the
// Go constants in server/ocserverd/wording_bundle.go.
export const MAX_WORDING_VALUE_LEN = 200;
export const MAX_WORDING_ENTRIES_PER_LANG = 1000;

// Font overlay bound (T-16a1 P4) — the twin of maxFontValueLen in
// server/ocserverd/font_bundle.go. A font value is a whole family stack; the
// longest curated stack fits comfortably, so anything longer is only ever an
// injection attempt. Membership in the SAFE stack set is the real gate; this
// cap is a cheap pre-filter mirrored on the server.
export const MAX_FONT_VALUE_LEN = 128;

/** The built-in theme names a custom bundle must never claim. office is the
 * only built-in now (修仙 is an importable custom bundle, so "xian" is a
 * perfectly legal custom id). */
export const RESERVED_THEME_IDS = ["office"] as const;

/** The override languages a wording overlay may key on. */
export const WORDING_LANGS = ["zh", "en"] as const;

const THEME_TOKEN_SET = new Set<string>(THEME_COLOR_TOKENS);
const WORDING_LANG_SET = new Set<string>(WORDING_LANGS);
const MESSAGE_KEY_SET = new Set<string>(MESSAGE_KEYS);
// Font whitelists (T-16a1 P4): the --font-* token names and the CLOSED set of
// safe family-stack values — the character-for-character twin of the Go maps in
// theme_fonts_gen.go.
const THEME_FONT_TOKEN_SET = new Set<string>(THEME_FONT_TOKENS);
const SAFE_FONT_STACK_SET = new Set<string>(SAFE_FONT_FAMILIES.map((f) => f.stack));

// The colour-value allowlist grammar (anchored full-match) — identical to the
// Go regexps. Concrete colours only: hex / rgb(a) / hsl(a) / transparent.
const COLOR_HEX_RE = /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/;
const COLOR_RGB_RE = /^rgba?\(\s*[0-9.,%/\s]+\)$/;
// hsl()/hsla(): rgb() char set plus the angle-unit letters {d,e,g,r,a,t,u,n}
// (deg/grad/rad/turn) on the hue — a closed set that cannot form url/var/
// expression/color-mix/javascript (all need a paren or colon this class forbids).
const COLOR_HSL_RE = /^hsla?\(\s*[0-9.,%/\sdegratun]+\)$/;
const THEME_ID_RE = /^[a-z0-9][a-z0-9-]{1,63}$/;

// Structure-breaking substrings a concrete colour can never legitimately hold.
// The grammar already rejects them; this yields a SPECIFIC "you pasted CSS" error.
const COLOR_INJECTION_MARKERS = [
  "url(", "expression(", "var(", "color-mix(", "image(", "element(",
  "javascript:", "/*", "*/", ";", "{", "}", "<", ">", "@", "\\", "`", "\n", "\r",
];

/** Whether v is an admissible concrete colour value. */
export function isValidColorValue(v: string): boolean {
  if (v === "" || v.length > MAX_COLOR_VALUE_LEN) return false;
  for (const m of COLOR_INJECTION_MARKERS) {
    if (v.includes(m)) return false;
  }
  if (v === "transparent") return true;
  return COLOR_HEX_RE.test(v) || COLOR_RGB_RE.test(v) || COLOR_HSL_RE.test(v);
}

// Structure-breaking substrings a font-family stack can never legitimately
// hold — the same defence-in-depth pass colours get. A font value is admitted
// only by EXACT membership in SAFE_FONT_STACK_SET (a closed allowlist), so a
// value that reaches setProperty is already safe; this second pass exists so a
// smuggled value (`url(`, `@font-face`, `;}`, `javascript:`, …) is rejected with
// a SPECIFIC error before the membership check, mirroring colorInjectionMarkers.
const FONT_INJECTION_MARKERS = [
  "url(", "expression(", "var(", "javascript:", "/*", "*/",
  ";", "{", "}", "<", ">", "@", "\\", "`", "\n", "\r", "(",
];

/** Whether v is an admissible font value: a member of the CLOSED safe-family
 * stack allowlist, with no CSS-structure characters (defence in depth). */
export function isValidFontValue(v: string): boolean {
  if (v === "" || v.length > MAX_FONT_VALUE_LEN) return false;
  for (const m of FONT_INJECTION_MARKERS) {
    if (v.includes(m)) return false;
  }
  return SAFE_FONT_STACK_SET.has(v);
}

/** Validate a bundle's optional `fonts` overlay (T-16a1 P4) — the twin of the
 * Go validateFonts. Key ∈ {--font-* token whitelist}, value ∈ {safe family
 * stack allowlist}. Returns an error message, or null when admissible (an
 * absent overlay is admissible). */
export function validateFonts(fonts: unknown, where = "theme"): string | null {
  if (fonts === undefined || fonts === null) return null;
  if (typeof fonts !== "object" || Array.isArray(fonts)) {
    return `${where}: fonts must be an object`;
  }
  for (const [token, value] of Object.entries(fonts as Record<string, unknown>)) {
    if (!THEME_FONT_TOKEN_SET.has(token)) {
      return `${where}: "${token}" is not a theme font token (only --font-sans / --font-title)`;
    }
    if (typeof value !== "string" || !isValidFontValue(value)) {
      return `${where}: "${token}" has an invalid font value — only a safe built-in font family may be chosen`;
    }
  }
  return null;
}

function runeCount(s: string): number {
  return [...s].length;
}

/** Whether a rune is a control character (Unicode Cc: 0x00-0x1F, 0x7F-0x9F) —
 * mirrors Go's unicode.IsControl, used to reject newlines / control chars in a
 * wording value. */
function hasControlChar(s: string): boolean {
  for (const ch of s) {
    const cp = ch.codePointAt(0) ?? 0;
    if (cp <= 0x1f || (cp >= 0x7f && cp <= 0x9f)) return true;
  }
  return false;
}

/** Validate a bundle's optional wording overlay (T-16a1 P3) — the twin of the
 * Go validateWording. Returns an error message, or null when admissible (an
 * absent overlay is admissible). */
export function validateWording(
  wording: unknown,
  where = "theme"
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
    if (pairs.length > MAX_WORDING_ENTRIES_PER_LANG) {
      return `${where}: wording[${lang}] holds more than ${MAX_WORDING_ENTRIES_PER_LANG} entries`;
    }
    for (const [code, value] of pairs) {
      if (!MESSAGE_KEY_SET.has(code)) {
        return `${where}: wording[${lang}] key "${code}" is not a known message code`;
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

/** Validate one bundle. Returns an error message, or null when admissible. */
export function validateThemeBundle(b: unknown, where = "theme"): string | null {
  if (typeof b !== "object" || b === null) {
    return `${where}: must be an object`;
  }
  const bundle = b as Record<string, unknown>;
  if (typeof bundle.id !== "string" || !THEME_ID_RE.test(bundle.id)) {
    return `${where}: id must match ^[a-z0-9][a-z0-9-]{1,63}$`;
  }
  if ((RESERVED_THEME_IDS as readonly string[]).includes(bundle.id)) {
    return `${where}: id "${bundle.id}" is reserved for a built-in theme`;
  }
  if (typeof bundle.name !== "string") {
    return `${where}: name must be a string`;
  }
  const n = runeCount(bundle.name.trim());
  if (n < 1 || n > MAX_THEME_NAME_LEN) {
    return `${where}: name must be 1..${MAX_THEME_NAME_LEN} characters after trimming`;
  }
  if (
    typeof bundle.colors !== "object" ||
    bundle.colors === null ||
    Array.isArray(bundle.colors)
  ) {
    return `${where}: colors must be an object`;
  }
  const colors = bundle.colors as Record<string, unknown>;
  const entries = Object.entries(colors);
  if (entries.length < MIN_THEME_COLORS || entries.length > MAX_THEME_COLORS) {
    return `${where}: colors must hold ${MIN_THEME_COLORS}..${MAX_THEME_COLORS} entries`;
  }
  for (const [token, value] of entries) {
    if (!THEME_TOKEN_SET.has(token)) {
      return `${where}: "${token}" is not a theme colour token (see theme.css)`;
    }
    if (typeof value !== "string" || !isValidColorValue(value)) {
      return `${where}: "${token}" has an invalid colour value — only concrete hex / rgb() / rgba() / hsl() / hsla() / transparent are accepted`;
    }
  }
  // wording (T-16a1 P3) is an optional per-language text-override overlay.
  const wErr = validateWording((bundle as { wording?: unknown }).wording, where);
  if (wErr) return wErr;
  // fonts (T-16a1 P4) is an optional --font-* → safe-family overlay.
  return validateFonts((bundle as { fonts?: unknown }).fonts, where);
}

/** Validate the whole custom_themes array (shape + token whitelist + colour
 * grammar + unique ids). Returns an error message, or null when admissible. */
export function validateThemeBundles(bundles: unknown): string | null {
  if (!Array.isArray(bundles)) {
    return "custom_themes must be an array";
  }
  if (bundles.length > MAX_CUSTOM_THEMES) {
    return `custom_themes must hold at most ${MAX_CUSTOM_THEMES} themes`;
  }
  const seen = new Set<string>();
  for (let i = 0; i < bundles.length; i++) {
    const err = validateThemeBundle(bundles[i], `custom_themes[${i}]`);
    if (err) return err;
    const id = (bundles[i] as ThemeBundle).id;
    if (seen.has(id)) return `custom_themes[${i}]: duplicate id "${id}"`;
    seen.add(id);
  }
  return null;
}

/** Whether a display.theme value is admissible given the custom id set:
 * "" (unset) | a built-in | an existing custom id. */
export function isValidDisplayTheme(theme: string, customIds: Set<string>): boolean {
  return (
    theme === "" ||
    (RESERVED_THEME_IDS as readonly string[]).includes(theme) ||
    customIds.has(theme)
  );
}
