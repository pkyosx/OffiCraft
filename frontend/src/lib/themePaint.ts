// The paint cache: a *whole ThemeBundle* pruned to the fields that reach the
// DOM, so the reader can call the EXISTING validateThemeBundle instead of
// growing a second grammar / a second whitelist.
import {
  validateThemeBundleWith,
  DEFAULT_BACKGROUND_MODE,
  type BackgroundMode,
  type ThemeBundle,
} from "./themeBundleCore";

/** The paint path's entry into the SHARED grammar: the same
 * validateThemeBundleWith every other caller runs, with the wording validator
 * passed as null — the paint record structurally has no wording (paintRecordFor
 * prunes it) and the applier has no wording sink, so a record carrying wording
 * is rejected outright. Colour / font / image / mode rules are byte-identical to
 * the non-paint path because they are the same function. */
function validatePaintBundle(b: unknown): string | null {
  return validateThemeBundleWith(b, "themePaint", undefined, null);
}

/** The localStorage key holding the SELECTED theme id. It lives here, next to
 * the paint key, because BOTH the pre-React inline script and the React
 * provider read it — a literal in either place is a second source of truth that
 * nothing would catch when it drifts. */
export const LS_THEME = "oc.theme";
export const LS_THEME_PAINT = "oc.themePaint";
/** The display_seasonal_theme cache ("true"/"false", absent = the shipped true)
 * and the seasonal theme's own paint record, kept apart from LS_THEME_PAINT so
 * the display theme's cached picture survives the seasonal window. */
export const LS_SEASONAL_THEME = "oc.seasonalTheme";
export const LS_SEASONAL_PAINT = "oc.seasonalPaint";
export const PAINT_CACHE_VERSION = 1;

export interface PaintRecord {
  v: number;
  bundle: ThemeBundle;
}

/** Prune a bundle to exactly the fields the DOM applier reads. */
export function paintRecordFor(b: ThemeBundle): PaintRecord {
  const pruned: ThemeBundle = { id: b.id, name: b.name, colors: b.colors };
  if (b.fonts) pruned.fonts = b.fonts;
  if (b.backgrounds?.canvas) {
    pruned.backgrounds = { canvas: b.backgrounds.canvas };
    if (b.backgroundModes?.canvas) {
      pruned.backgroundModes = { canvas: b.backgroundModes.canvas };
    }
  }
  return { v: PAINT_CACHE_VERSION, bundle: pruned };
}

/** Read + fully validate the cached picture. Any failure → null (today's
 * behaviour). Never throws. */
export function readValidatedPaint(key: string = LS_THEME_PAINT): ThemeBundle | null {
  let raw: string | null = null;
  try {
    raw = localStorage.getItem(key);
  } catch {
    return null;
  }
  if (!raw) return null;
  let rec: unknown;
  try {
    rec = JSON.parse(raw);
  } catch {
    return null;
  }
  if (typeof rec !== "object" || rec === null) return null;
  const r = rec as Record<string, unknown>;
  if (r.v !== PAINT_CACHE_VERSION) return null;
  // ONE call, the same validator the import UI / picker / mock parity use.
  if (validatePaintBundle(r.bundle) !== null) return null;
  return r.bundle as ThemeBundle;
}

/** "dark" or "light" from a `#rrggbb` / `rgb()` page background; null when the
 * value cannot be read. */
export function colorSchemeForBackground(bg: string | undefined): "dark" | "light" | null {
  if (!bg) return null;
  const hex = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(bg.trim());
  const rgb = /^rgba?\(\s*(\d+)[\s,]+(\d+)[\s,]+(\d+)/i.exec(bg.trim());
  const parts = hex
    ? hex.slice(1, 4).map((h) => parseInt(h, 16))
    : rgb
      ? rgb.slice(1, 4).map(Number)
      : null;
  if (!parts) return null;
  const [r, g, b] = parts.map((c) => {
    const v = c / 255;
    return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b < 0.2 ? "dark" : "light";
}

/** The single DOM applier — used by BOTH the pre-React script and the React
 * apply effect. Returns the token names pushed (the ledger entry). */
export function applyThemeToRoot(root: HTMLElement, bundle: ThemeBundle): string[] {
  const applied: string[] = [];
  for (const [tok, val] of Object.entries(bundle.colors)) {
    root.style.setProperty(tok, val);
    applied.push(tok);
  }
  // Native controls (the date input's calendar icon, select menus, scrollbars)
  // follow color-scheme, not the colour tokens: without it a dark pack gets a
  // black calendar icon on its dark field. It rides the ledger so the next
  // theme's clear removes it with the tokens.
  const scheme = colorSchemeForBackground(bundle.colors["--color-bg"]);
  if (scheme) {
    root.style.setProperty("color-scheme", scheme);
    applied.push("color-scheme");
  }
  for (const [tok, val] of Object.entries(bundle.fonts ?? {})) {
    root.style.setProperty(tok, val);
    applied.push(tok);
  }
  const canvas = bundle.backgrounds?.canvas;
  if (canvas) {
    const url = `url("${canvas}")`;
    const mode: BackgroundMode =
      bundle.backgroundModes?.canvas ?? DEFAULT_BACKGROUND_MODE;
    const lay = {
      tile: { image: url, repeat: "repeat", position: "0 0", size: "auto", attachment: "scroll" },
      sides: {
        image: `${url}, ${url}`,
        repeat: "no-repeat, no-repeat",
        position: "left bottom, right bottom",
        size: "auto, auto",
        attachment: "fixed, fixed",
      },
      cover: {
        image: url,
        repeat: "no-repeat",
        position: "center center",
        size: "cover",
        attachment: "fixed",
      },
    }[mode];
    root.style.setProperty("--canvas-bg-image", lay.image);
    root.style.setProperty("--canvas-bg-repeat", lay.repeat);
    root.style.setProperty("--canvas-bg-position", lay.position);
    root.style.setProperty("--canvas-bg-size", lay.size);
    root.style.setProperty("--canvas-bg-attachment", lay.attachment);
    applied.push(
      "--canvas-bg-image",
      "--canvas-bg-repeat",
      "--canvas-bg-position",
      "--canvas-bg-size",
      "--canvas-bg-attachment"
    );
  }
  return applied;
}
