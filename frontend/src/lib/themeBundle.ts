// themeBundle.ts — the PUBLIC façade of the theme-bundle grammar. It is split
// across two files purely for load-cost reasons (see themeWording.ts); every
// existing importer keeps the same API, and the grammar still exists exactly
// once.
export * from "./themeBundleCore";
export { validateWording } from "./themeWording";

import { validateThemeBundleWith, validateThemeBundlesWith } from "./themeBundleCore";
import { validateWording } from "./themeWording";

/** Validate one bundle (colours + fonts + images + wording). */
export function validateThemeBundle(
  b: unknown,
  where = "theme",
  skipped?: string[]
): string | null {
  return validateThemeBundleWith(b, where, skipped, validateWording);
}

/** Validate the whole custom_themes array. */
export function validateThemeBundles(bundles: unknown): string | null {
  return validateThemeBundlesWith(bundles, (b, where) =>
    validateThemeBundleWith(b, where, undefined, validateWording)
  );
}
