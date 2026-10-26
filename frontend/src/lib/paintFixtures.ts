// paintFixtures.ts — T-1500. The ONE source of truth for the paint-cache guard
// fixtures, shared by BOTH layers that guard the pre-React applier:
//
//   * src/lib/themePaint.test.ts        (jsdom, gate 4b) — asserts the VALIDATOR
//     rejects every malicious record and ACCEPTS + APPLIES the valid rich one.
//   * paint-guards/*.paint.spec.ts      (real Chromium on the real build, gate
//     4c) — asserts no frame of a real page load ever carries a rejected value,
//     and that the valid rich record's values DO land.
//
// Two layers, one fixture list: a payload added here is guarded in both, and the
// two can never silently disagree about what "malicious" means.
//
// NOTE ON THE POSITIVE CASE (`VALID_RICH_BUNDLE`): it deliberately carries
// colours AND fonts AND a canvas background AND a background mode. An
// absence-only suite (only "this forbidden string never appears") stays green
// when the applier silently STOPS applying fonts/backgrounds altogether — that
// exact mutant (delete the fonts loop + the canvas branch from applyThemeToRoot)
// passed tsc, the build, the artifact assertions, the decision tests in
// paintCache.test.tsx and a 6-case absence-only browser probe. `EXPECT_APPLIED`
// is what closes it. An independent review re-measured the narrow half of that
// mutant: deleting ONLY the fonts loop still passes tsc and leaves 1756 of 1757
// tests green — the single red is the positive assertion below. The fonts half
// really was at zero coverage; this fixture is the only thing holding it.

import type { ThemeBundle } from "./themeBundleCore";

/** A real 8-byte PNG signature as a base64 data URI — the smallest value that
 * clears the image gate (mime allowlist + magic bytes + the size cap — 64 KiB for
 * avatars / logo / navIcons, 512 KiB for backgrounds since T-72da; 8 bytes
 * clears either). */
export const TINY_PNG = "data:image/png;base64,iVBORw0KGgo=";

/** An SVG data URI carrying a <script> — must never reach the DOM. SVG is NOT
 * in the image mime allowlist, so a correct validator rejects the whole record. */
export const EVIL_SVG =
  "data:image/svg+xml;base64,PHN2Zz48c2NyaXB0PmFsZXJ0KDEpPC9zY3JpcHQ+PC9zdmc+";

/** The cached background colour every probe looks for. Kept deliberately far
 * from the office default (#191c24 → rgb(25, 28, 36)) so a wrong frame is
 * unambiguous. */
export const CACHED_BG_HEX = "#010203";
export const CACHED_BG_RGB = "rgb(1, 2, 3)";

/** A safe font stack — EXACT membership in SAFE_FONT_STACK_SET is the only way
 * a font value passes, so this string must stay byte-identical to the generated
 * `system` stack. */
export const SAFE_SANS_STACK =
  'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';

export const PAINT_THEME_ID = "midnight";

/** The valid record's bundle: colours + fonts + canvas image + canvas mode, so
 * every branch of applyThemeToRoot is exercised by a POSITIVE assertion. This is
 * ALSO exactly what the stub server hands back from GET /api/settings, which is
 * what lets the zero-flash guard assert "cache == server truth" (no repaint) and
 * "reconcile really ran" in the same load.
 *
 * The same bytes exist a second time as `paintFixtures.theme.json`, because
 * paint-guards/settingsStub.mjs is plain Node and can neither import TypeScript
 * nor (under Playwright's native ESM loader) import JSON without an import
 * attribute. The duplication is NOT trusted: themePaint.test.ts reads the JSON
 * off disk and deep-compares it to this object, so the two cannot drift. */
export const VALID_RICH_BUNDLE: ThemeBundle = {
  id: PAINT_THEME_ID,
  name: "Midnight",
  colors: { "--color-bg": CACHED_BG_HEX },
  fonts: { "--font-sans": SAFE_SANS_STACK },
  backgrounds: { canvas: TINY_PNG },
  backgroundModes: { canvas: "cover" },
};

/** Where the Node-side copy lives, relative to the frontend root. */
export const SERVER_THEME_JSON_PATH = "src/lib/paintFixtures.theme.json";

/** Substrings that MUST appear in <html>'s inline style once the valid record is
 * applied — one per applyThemeToRoot branch (colour loop / fonts loop / canvas
 * branch, including a mode-specific value so "cover" cannot silently become the
 * default "tile"). */
export const EXPECT_APPLIED: readonly string[] = [
  `--color-bg: ${CACHED_BG_HEX}`,
  "--font-sans: system-ui",
  `--canvas-bg-image: url("${TINY_PNG}")`,
  "--canvas-bg-repeat: no-repeat",
  "--canvas-bg-size: cover",
  "--canvas-bg-attachment: fixed",
];

/** The EXACT value each custom property must hold after the valid record is
 * applied. The jsdom layer asserts these via getPropertyValue (no dependence on
 * how a CSS serializer quotes things); the browser layer additionally asserts
 * the serialized `style` attribute via EXPECT_APPLIED. */
export const EXPECT_APPLIED_VALUES: Readonly<Record<string, string>> = {
  "--color-bg": CACHED_BG_HEX,
  "--font-sans": SAFE_SANS_STACK,
  "--canvas-bg-image": `url("${TINY_PNG}")`,
  "--canvas-bg-repeat": "no-repeat",
  "--canvas-bg-position": "center center",
  "--canvas-bg-size": "cover",
  "--canvas-bg-attachment": "fixed",
  "color-scheme": "dark",
};

/** The CSS custom properties the valid record must put on the ledger. */
export const EXPECT_APPLIED_TOKENS: readonly string[] = [
  "--color-bg",
  "--font-sans",
  "--canvas-bg-image",
  "--canvas-bg-repeat",
  "--canvas-bg-position",
  "--canvas-bg-size",
  "--canvas-bg-attachment",
  "color-scheme",
];

export interface MaliciousPaintCase {
  /** Case name — also the test title. */
  readonly name: string;
  /** The `bundle` field of the stored record (attacker-controlled). */
  readonly bundle: unknown;
  /** Substrings that must NEVER appear in <html>'s inline style, on ANY frame. */
  readonly forbidden: readonly string[];
  /** When true this case is NOT evidence that the validator rejected anything —
   * the browser/CSSOM would drop the value on its own. Kept for regression
   * value, but it must never be counted as coverage of this gate. */
  readonly notGuardedByValidator?: boolean;
}

/** Every record the paint reader must reject WHOLE (never field-by-field). */
export const MALICIOUS_PAINT_CASES: readonly MaliciousPaintCase[] = [
  {
    name: "off-whitelist-token",
    bundle: {
      id: PAINT_THEME_ID,
      name: "M",
      colors: { "--color-bg": CACHED_BG_HEX, "--evil": "#ff0000" },
    },
    forbidden: ["--evil"],
  },
  {
    name: "font-outside-safe-set",
    bundle: {
      id: PAINT_THEME_ID,
      name: "M",
      colors: { "--color-bg": CACHED_BG_HEX },
      fonts: { "--font-sans": "evil" },
    },
    forbidden: ["--font-sans: evil"],
  },
  {
    name: "svg-canvas-bg",
    bundle: {
      id: PAINT_THEME_ID,
      name: "M",
      colors: { "--color-bg": CACHED_BG_HEX },
      backgrounds: { canvas: EVIL_SVG },
    },
    forbidden: ["svg+xml", "--canvas-bg-image"],
  },
  {
    name: "illegal-canvasMode",
    // `{tile,sides,cover}["__proto__"]` returns Object.prototype rather than
    // throwing, so an unvalidated applier writes `--canvas-bg-image: undefined`
    // onto the DOM — a SILENT failure, not a TypeError.
    bundle: {
      id: PAINT_THEME_ID,
      name: "M",
      colors: { "--color-bg": CACHED_BG_HEX },
      backgrounds: { canvas: TINY_PNG },
      backgroundModes: { canvas: "__proto__" },
    },
    forbidden: ["--canvas-bg-image"],
  },
  {
    name: "wording-on-the-paint-path",
    // The paint path passes wordingFn = null, which REJECTS a record carrying
    // wording rather than ignoring it — that is what makes "this path has no
    // wording sink" an enforced invariant instead of an assumption.
    bundle: {
      id: PAINT_THEME_ID,
      name: "M",
      colors: { "--color-bg": CACHED_BG_HEX },
      wording: { "zh-Hant": { "app.title": "x" } },
    },
    forbidden: [`--color-bg: ${CACHED_BG_HEX}`],
  },
  {
    name: "colour-css-injection",
    bundle: {
      id: PAINT_THEME_ID,
      name: "M",
      colors: { "--color-bg": "red;background:url(x)" },
    },
    forbidden: ["url(x)"],
    // CSSOM drops this on its own (setProperty does not parse declaration
    // lists), so it is clean even with the validator removed. NOT coverage.
    notGuardedByValidator: true,
  },
];

/** Wrap a bundle in the v1 paint record envelope. */
export function paintRecordJSON(bundle: unknown, v = 1): string {
  return JSON.stringify({ v, bundle });
}
