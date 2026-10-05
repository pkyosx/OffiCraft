// Built-in themes for the contrast guards, selected the way the product selects
// one: the stored theme id that I18nProvider reads, plus <html data-theme> that
// theme.css keys each built-in's block off (stories without a provider rely on
// the attribute alone).
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { LS_THEME } from "../src/lib/themePaint";
import { RESERVED_THEME_IDS, type BuiltinThemeId } from "../src/lib/themeBundleCore";

export const BUILTIN_THEME_IDS: readonly BuiltinThemeId[] = RESERVED_THEME_IDS;

const THEME_CSS = readFileSync(
  fileURLToPath(new URL("../src/styles/theme.css", import.meta.url)),
  "utf8"
).replace(/\/\*[\s\S]*?\*\//g, "");

function blockOf(theme: BuiltinThemeId): string {
  const re =
    theme === "office"
      ? /(?:^|\n):root\s*\{([^}]*)\}/
      : new RegExp(`:root\\[data-theme="${theme}"\\]\\s*\\{([^}]*)\\}`);
  const body = re.exec(THEME_CSS)?.[1];
  if (body === undefined) throw new Error(`theme.css has no block for built-in ${theme}`);
  return body;
}

/** `token`'s declared value in `theme`'s theme.css block; a non-office block
 * that leaves it out inherits the :root declaration. */
export function builtinToken(theme: BuiltinThemeId, token: string): string {
  const pick = (body: string) =>
    new RegExp(`${token}\\s*:\\s*([^;]+);`).exec(body)?.[1].trim();
  const value = pick(blockOf(theme)) ?? pick(blockOf("office"));
  if (value === undefined) throw new Error(`${token} is not declared for ${theme}`);
  return value;
}

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
  expect(bg, `--color-bg at capture time — is ${theme} really applied?`).toBe(
    builtinToken(theme, "--color-bg")
  );
}
