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

/** Export a built-in theme (office/xian) by momentarily applying its
 * data-theme layer with any inline overrides stripped, reading the resolved
 * colours, then restoring the previous state verbatim. Used by the 修仙 dogfood
 * ("匯入修仙範例") to prove the export→import loop against a shipped theme. */
export function exportBuiltinTheme(
  builtin: "office" | "xian",
  id: string,
  name: string,
  el: HTMLElement = document.documentElement
): ThemeBundle {
  const prevTheme = el.dataset.theme;
  const inline: [string, string][] = [];
  for (const tok of THEME_COLOR_TOKENS) {
    const v = el.style.getPropertyValue(tok);
    if (v) {
      inline.push([tok, v]);
      el.style.removeProperty(tok);
    }
  }
  el.dataset.theme = builtin;
  const bundle = exportComputedTheme(id, name, el);
  el.dataset.theme = prevTheme ?? "office";
  for (const [tok, v] of inline) el.style.setProperty(tok, v);
  return bundle;
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
  return { bundle: { id: b.id, name: b.name, colors: b.colors } };
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
