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
  RESERVED_THEME_IDS,
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

/** Pick the bundle a "匯出當前主題" (toolbar 匯出) click should download. A custom
 * theme exports its STORED bundle — the FULL overlay: colours + wording + fonts +
 * avatars — so nothing is silently dropped. The built-in office carries no
 * overlay, so its computed colours ARE the complete, lossless export. This keeps
 * the toolbar 匯出 in step with a row's download icon (which already serialises
 * the stored bundle); before this, the toolbar path exported colours only. */
export function exportCurrentBundle(
  theme: string,
  customThemes: ThemeBundle[],
  officeName: string,
  el: Element = document.documentElement
): ThemeBundle {
  const stored = customThemes.find((b) => b.id === theme);
  if (stored) return stored;
  return exportComputedTheme("office-copy", officeName, el);
}

/** Read office's BASE palette — the theme.css :root defaults — off `el`,
 * transparently neutralising any active custom theme's inline overrides so the
 * result is always the built-in office colours no matter which theme is currently
 * applied. Used to seed a "以辦公室為底" new custom theme. The strip→read→restore
 * runs synchronously (getComputedStyle forces a style flush, never a paint), so
 * nothing flashes on screen. */
export function exportOfficeBaseTheme(
  id: string,
  name: string,
  el: Element = document.documentElement
): ThemeBundle {
  const root = el as HTMLElement;
  const saved: [string, string][] = [];
  for (const tok of THEME_COLOR_TOKENS) {
    const inline = root.style.getPropertyValue(tok);
    if (inline) {
      saved.push([tok, inline]);
      root.style.removeProperty(tok);
    }
  }
  try {
    return exportComputedTheme(id, name, el);
  } finally {
    for (const [tok, val] of saved) root.style.setProperty(tok, val);
  }
}

/** The first `custom-N` id (N ≥ 1) that no existing custom theme holds and that
 * is not a reserved built-in id. Always matches THEME_ID_RE, so a freshly added
 * theme is a valid, collision-free bundle. */
export function nextCustomThemeId(existing: Iterable<string>): string {
  const taken = new Set<string>(existing);
  const reserved = new Set<string>(RESERVED_THEME_IDS);
  for (let n = 1; ; n++) {
    const id = `custom-${n}`;
    if (!taken.has(id) && !reserved.has(id)) return id;
  }
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
  // Carry the optional avatar images through (T-16a1 P5) — already validated by
  // the shared validator; dropping them would silently lose an imported theme's
  // per-member-type avatars (the images travel INSIDE the bundle by design).
  if (b.avatars !== undefined) bundle.avatars = b.avatars;
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
