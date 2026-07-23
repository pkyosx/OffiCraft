// themeExport.ts — T-16a1 P2b: export/import glue between the running cockpit
// and the theme-bundle format. Export reads the RESOLVED colour of every
// --color-* token off getComputedStyle (so a built-in that leans on color-mix()
// still exports as concrete colours); import parses JSON and runs it through the
// same validator the server uses (lib/themeBundle.ts). Both directions go
// through the one grammar, so an exported bundle re-imports without loss.

import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";
import {
  isValidColorValue,
  validateThemeBundle,
  type ThemeBundle,
} from "./themeBundle";

/** Read the resolved value of each --color-* token off `el`'s computed style
 * and pack it into a bundle. Only tokens whose resolved value is a concrete
 * colour (per the shared grammar) are kept, so the result always re-imports
 * cleanly — a token that resolves to an unresolved color-mix()/var() is skipped
 * rather than poisoning the bundle. */
export function exportComputedTheme(
  id: string,
  name: string,
  el: Element = document.documentElement
): ThemeBundle {
  const cs = getComputedStyle(el);
  const colors: Record<string, string> = {};
  for (const tok of THEME_COLOR_TOKENS) {
    const v = cs.getPropertyValue(tok).trim();
    if (v && isValidColorValue(v)) colors[tok] = v;
  }
  return { id, name, colors };
}

/** Parse and validate imported bundle text. Returns the normalized bundle or a
 * human error. Never mutates anything — the caller decides whether to save. */
export function parseImportedBundle(
  text: string
): { bundle: ThemeBundle } | { error: string } {
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch {
    return { error: "不是有效的 JSON" };
  }
  const err = validateThemeBundle(data);
  if (err) return { error: err };
  const b = data as ThemeBundle;
  // Carry the optional wording overlay through (T-16a1 P3) — it has already
  // passed the shared validator; dropping it would silently lose an imported
  // theme's 用詞 pack. `colors`-only bundles keep `wording` absent.
  const bundle: ThemeBundle = { id: b.id, name: b.name, colors: b.colors };
  if (b.wording !== undefined) bundle.wording = b.wording;
  // Carry the optional font overlay through (T-16a1 P4) — already validated by
  // the shared validator; dropping it would silently lose an imported theme's
  // font choice.
  if (b.fonts !== undefined) bundle.fonts = b.fonts;
  return { bundle };
}

/** Serialize a bundle to pretty JSON (download / clipboard payload). */
export function serializeBundle(bundle: ThemeBundle): string {
  return JSON.stringify(bundle, null, 2);
}

/** Produce a stable, filesystem-safe filename for a downloaded bundle. */
export function bundleFilename(bundle: ThemeBundle): string {
  const slug = bundle.id.replace(/[^a-z0-9-]/g, "") || "theme";
  return `officraft-theme-${slug}.json`;
}
