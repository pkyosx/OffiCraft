// themeExport.ts — T-16a1 P2b: export/import glue between the running cockpit
// and the theme-bundle format. Export reads the RESOLVED colour of every
// --color-* token off getComputedStyle (so a built-in that leans on color-mix()
// still exports as concrete colours); import parses JSON and runs it through the
// same validator the server uses (lib/themeBundle.ts). Both directions go
// through the one grammar, so an exported bundle re-imports without loss.

import {
  THEME_ALIAS_DEFAULT_TOKENS,
  THEME_COLOR_TOKENS,
} from "../styles/themeTokens.generated";
import {
  isValidColorValue,
  validateThemeBundle,
  RESERVED_THEME_IDS,
  type ThemeBundle,
} from "./themeBundle";

/** True when `tok` is an alias-default token (theme.css defines it as a bare
 * `var(--other)`) that nobody has moved off its alias — i.e. it resolves to
 * exactly what the token it defers to resolves to.
 *
 * WHY IT MATTERS (T-081b): the zone tokens --color-topbar-bg / --color-nav-bg /
 * --color-main-bg default to `var(--color-bg)` so an untouched theme keeps one
 * flat backdrop and starts layering only once someone picks a zone colour.
 * getComputedStyle RESOLVES that var(), so a naive export bakes the built-in
 * #191c24 into all three — and the seeded-from-office theme every new custom
 * theme starts as would have its zones pinned forever: editing --color-bg would
 * move the body and nothing else (in the wide layout, where the gutter is 0, it
 * would move nothing visible at all). Omitting an unset alias keeps the
 * deferral, while a zone the owner actually chose still differs from --color-bg
 * and is exported normally. */
function isUnsetAliasDefault(
  tok: string,
  resolved: string,
  read: (t: string) => string
): boolean {
  const target = THEME_ALIAS_DEFAULT_TOKENS[tok];
  return target !== undefined && read(target) === resolved;
}

/** Read the resolved value of each --color-* token off `el`'s computed style
 * and pack it into a bundle. Only tokens whose resolved value is a concrete
 * colour (per the shared grammar) are kept, so the result always re-imports
 * cleanly — a token that resolves to an unresolved color-mix()/var() is skipped
 * rather than poisoning the bundle. Alias-default tokens still sitting on their
 * alias are skipped too (see isUnsetAliasDefault). */
export function exportComputedTheme(
  id: string,
  name: string,
  el: Element = document.documentElement
): ThemeBundle {
  const cs = getComputedStyle(el);
  const read = (t: string) => cs.getPropertyValue(t).trim();
  const colors: Record<string, string> = {};
  for (const tok of THEME_COLOR_TOKENS) {
    const v = read(tok);
    if (!v || !isValidColorValue(v)) continue;
    if (isUnsetAliasDefault(tok, v, read)) continue;
    colors[tok] = v;
  }
  return { id, name, colors };
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

/** Parse and validate imported bundle text. Returns the normalized bundle plus
 * `skippedWording` — the unrecognised message codes the validator dropped — or a
 * human error. A dropped code NEVER fails the import (owner ruling 2026-07-27);
 * it is handed back so the import UI can warn about what went inert. */
export function parseImportedBundle(
  text: string
): { bundle: ThemeBundle; skippedWording: string[] } | { error: string } {
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch {
    return { error: "不是有效的 JSON" };
  }
  const skippedWording: string[] = [];
  const err = validateThemeBundle(data, "theme", skippedWording);
  if (err) return { error: err };
  const b = data as ThemeBundle;
  // Carry the optional wording overlay through (T-16a1 P3) — it has already
  // passed the shared validator; dropping it would silently lose an imported
  // theme's 用詞 pack. `colors`-only bundles keep `wording` absent. The
  // validator has already removed any code outside the whitelist (T-081b), so
  // what is carried here is exactly what will be stored and re-exported.
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
  if (b.avatarPools !== undefined) bundle.avatarPools = b.avatarPools;
  // Carry the optional studio logo + per-nav-tab icons through (T-ea81) —
  // already validated by the shared validator; dropping them would silently
  // lose an imported theme's logo / nav icons (the images travel INSIDE the
  // bundle by design, like avatars).
  if (b.logo !== undefined) bundle.logo = b.logo;
  if (b.navIcons !== undefined) bundle.navIcons = b.navIcons;
  // Carry the optional outer-canvas background image through (T-081b). It rides
  // its OWN field rather than a --color-* token deliberately: its value is
  // url("data:...") — not a colour — so a token would be dropped on the floor by
  // exportComputedTheme's isValidColorValue filter and rejected by the bundle's
  // colour grammar. Living beside logo/avatars, it round-trips like they do.
  if (b.backgrounds !== undefined) bundle.backgrounds = b.backgrounds;
  // …and its display mode beside it (T-081b) — dropping it would round-trip a
  // "sides" theme back as a tiled one, i.e. silently change how it looks.
  if (b.backgroundModes !== undefined) bundle.backgroundModes = b.backgroundModes;
  return { bundle, skippedWording };
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
