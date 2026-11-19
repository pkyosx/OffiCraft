import { isBundledBuiltinTheme, type ThemeBundle } from "./themeBundleCore";
import { themeFileLoaders } from "./themeFileFetch";

// Each file ships as its own JSON asset, fetched the first time its theme is
// shown or previewed. Not importable by the pre-paint script (import.meta.glob).
const FILE_URLS = import.meta.glob<string>("../../../themes/modern-office-desk-*.theme.json", {
  query: "?url&no-inline",
  import: "default",
  eager: true,
});

export function bundledBuiltinThemeLoaderFrom(
  files: Record<string, () => Promise<ThemeBundle>>
): (id: string) => Promise<ThemeBundle> {
  return (id) => {
    const file = isBundledBuiltinTheme(id) ? files[`../../../themes/${id}.theme.json`] : undefined;
    return file ? file() : Promise.reject(new Error(`built-in theme file not found: themes/${id}.theme.json`));
  };
}

export const loadBundledBuiltinTheme = bundledBuiltinThemeLoaderFrom(themeFileLoaders(FILE_URLS));
