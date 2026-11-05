import { isBundledBuiltinTheme, type ThemeBundle } from "./themeBundleCore";

// Lazy: each file is its own chunk, fetched the first time its theme is shown
// or previewed. Not importable by the pre-paint script (import.meta.glob).
const FILES = import.meta.glob<ThemeBundle>("../../../themes/modern-office-desk-*.theme.json", {
  import: "default",
});

export function bundledBuiltinThemeLoaderFrom(
  files: Record<string, () => Promise<ThemeBundle>>
): (id: string) => Promise<ThemeBundle> {
  return (id) => {
    const file = isBundledBuiltinTheme(id) ? files[`../../../themes/${id}.theme.json`] : undefined;
    return file ? file() : Promise.reject(new Error(`built-in theme file not found: themes/${id}.theme.json`));
  };
}

export const loadBundledBuiltinTheme = bundledBuiltinThemeLoaderFrom(FILES);
