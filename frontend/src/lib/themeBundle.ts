// themeBundle.ts — the PUBLIC façade of the theme-bundle grammar. It is split
// across two files purely for load-cost reasons (see themeWording.ts); every
// existing importer keeps the same API, and the grammar still exists exactly
// once.
export * from "./themeBundleCore";
export { validateWording } from "./themeWording";

import {
  RESERVED_THEME_IDS,
  validateThemeBundleWith,
  validateThemeBundlesWith,
} from "./themeBundleCore";
import { validateWording } from "./themeWording";

/** Validate one custom bundle (colours + fonts + images + wording). Its id may
 * not be a built-in's or a seasonal theme's. The shared grammar only refuses
 * the built-in ids, because the pre-paint record of the seasonal theme itself
 * goes through it. */
export function validateThemeBundle(
  b: unknown,
  where = "theme",
  skipped?: string[]
): string | null {
  const err = validateThemeBundleWith(b, where, skipped, validateWording);
  if (err) return err;
  const id = (b as { id: string }).id;
  return RESERVED_THEME_IDS.includes(id)
    ? `${where}: id "${id}" is reserved for a built-in theme`
    : null;
}

/** Validate the whole custom_themes array. */
export function validateThemeBundles(bundles: unknown): string | null {
  return validateThemeBundlesWith(bundles, (b, where) => validateThemeBundle(b, where));
}
