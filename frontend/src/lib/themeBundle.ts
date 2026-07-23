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

/** One owner-authored theme colour bundle (mirrors ThemeBundleDTO). */
export interface ThemeBundle {
  id: string;
  name: string;
  colors: Record<string, string>;
}

export const MAX_COLOR_VALUE_LEN = 64;
export const MIN_THEME_COLORS = 1;
export const MAX_THEME_COLORS = 200;
export const MAX_CUSTOM_THEMES = 100;
export const MAX_THEME_NAME_LEN = 80;

/** The built-in theme names a custom bundle must never claim. */
export const RESERVED_THEME_IDS = ["office", "xian"] as const;

const THEME_TOKEN_SET = new Set<string>(THEME_COLOR_TOKENS);

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

function runeCount(s: string): number {
  return [...s].length;
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
  return null;
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
