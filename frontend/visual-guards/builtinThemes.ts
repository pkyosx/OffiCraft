// Built-in themes for the contrast guards, selected the way the product selects
// one: the stored theme id that I18nProvider reads, plus <html data-theme> that
// theme.css keys each built-in's block off (stories without a provider rely on
// the attribute alone).
import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { LS_THEME } from "../src/lib/themePaint";
import { RESERVED_THEME_IDS, type BuiltinThemeId } from "../src/lib/themeBundleCore";

export const BUILTIN_THEME_IDS: readonly BuiltinThemeId[] = RESERVED_THEME_IDS;

/** Each built-in's --color-bg, written out by hand so a block that loses its
 * own declaration cannot pass by inheriting office's. */
const BUILTIN_BG: Record<BuiltinThemeId, string> = {
  office: "#191c24",
  "office-light": "#b8d8e3",
};

/** Select `theme` before mounting. Call after the CT page has loaded. */
export async function selectBuiltinTheme(page: Page, theme: BuiltinThemeId): Promise<void> {
  await page.evaluate(
    ([key, id]) => {
      localStorage.setItem(key, id);
      document.documentElement.dataset.theme = id;
    },
    [LS_THEME, theme] as const
  );
}

/** Proof the capture ran under `theme`: a guard that silently measured another
 * theme reports that theme's numbers under this one's name. */
export async function expectBuiltinApplied(page: Page, theme: BuiltinThemeId): Promise<void> {
  const bg = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue("--color-bg").trim()
  );
  expect(bg, `--color-bg at capture time — is ${theme} really applied?`).toBe(BUILTIN_BG[theme]);
}
