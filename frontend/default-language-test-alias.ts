import { fileURLToPath } from "node:url";

// Playwright CT only (vitest pins the same value with a vi.mock in
// src/test/setup.ts, which a suite about the default can undo): the guards
// assert Chinese text, so they run with Chinese as the default language.
export const ZH_DEFAULT_LANGUAGE_ALIAS = {
  find: /^\.\/defaultLanguage$/,
  replacement: fileURLToPath(new URL("./src/test/defaultLanguage.zh.ts", import.meta.url)),
};
