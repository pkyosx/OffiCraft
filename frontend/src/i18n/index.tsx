import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { zh, type Dict } from "./locales/zh";
import { en } from "./locales/en";
import { xian } from "./locales/xian";

export type Locale = "zh" | "en" | "xian";
/** User-selectable language (mockup 語言 toggle offers only 中文 / English). */
export type Language = "zh" | "en";
/** Visual theme (mockup 主題 toggle: 辦公室 default / 修仙). */
export type Theme = "office" | "xian";

const DICTS: Record<Locale, Dict> = { zh, en, xian };

// localStorage keys (client-side persistence — these prefs have no backend
// store). NOTE: neither the studio/org name (T-d693) nor the owner nickname
// (T-0b41) is here — both moved server-side behind /api/settings (DB org.name /
// owner.name; see hooks/useOrgName + hooks/useOwnerName).
const LS_LANGUAGE = "oc.language";
const LS_THEME = "oc.theme";

function readStored<T extends string>(key: string, allowed: T[], fallback: T): T {
  try {
    const v = localStorage.getItem(key);
    if (v && (allowed as string[]).includes(v)) return v as T;
  } catch {
    // localStorage unavailable — fall through to default (no fake persistence)
  }
  return fallback;
}

function writeStored(key: string, value: string | null) {
  try {
    if (value == null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // ignore — best-effort persistence
  }
}

interface I18nContextValue {
  /** Effective render locale: 修仙 theme surfaces the immersive `xian` dict. */
  locale: Locale;
  t: Dict;
  language: Language;
  setLanguage: (next: Language) => void;
  theme: Theme;
  setTheme: (next: Theme) => void;
  /** Reset local preferences to initial (used by the honest M1 "logout").
   * Covers only the client-persisted prefs (theme/language); the owner
   * nickname is server-backed now (T-0b41) and is left untouched. */
  resetPreferences: () => void;
}

const I18nContext = createContext<I18nContextValue | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [language, setLanguageState] = useState<Language>(() =>
    readStored<Language>(LS_LANGUAGE, ["zh", "en"], "zh")
  );
  const [theme, setThemeState] = useState<Theme>(() =>
    readStored<Theme>(LS_THEME, ["office", "xian"], "office")
  );
  // 修仙 theme drives the immersive `xian` copy; 辦公室 uses the language toggle.
  const locale: Locale = theme === "xian" ? "xian" : language;
  const t = DICTS[locale];

  // Reflect the theme on <html data-theme> so CSS can restyle (see theme.css).
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  const setLanguage = useCallback((next: Language) => {
    setLanguageState(next);
    writeStored(LS_LANGUAGE, next);
  }, []);

  const setTheme = useCallback((next: Theme) => {
    setThemeState(next);
    writeStored(LS_THEME, next);
  }, []);

  const resetPreferences = useCallback(() => {
    setLanguage("zh");
    setTheme("office");
  }, [setLanguage, setTheme]);

  const value = useMemo<I18nContextValue>(
    () => ({
      locale,
      t,
      language,
      setLanguage,
      theme,
      setTheme,
      resetPreferences,
    }),
    [locale, t, language, setLanguage, theme, setTheme, resetPreferences]
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
  const ctx = useContext(I18nContext);
  if (!ctx) {
    throw new Error("useI18n must be used within an I18nProvider");
  }
  return ctx;
}
