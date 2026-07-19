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
// store). NOTE: the studio/org name is NOT here — it moved server-side in
// T-d693 (DB org.name behind /api/settings; see hooks/useOrgName).
const LS_LANGUAGE = "oc.language";
const LS_THEME = "oc.theme";
const LS_OWNER_NAME = "oc.ownerName";

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
  /** Custom owner display name (client-persisted); null → fall back to t.user. */
  ownerName: string | null;
  setOwnerName: (next: string | null) => void;
  /** Resolved owner display name for the topbar pill / profile header. */
  userName: string;
  /** Reset local preferences to initial (used by the honest M1 "logout"). */
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
  const [ownerName, setOwnerNameState] = useState<string | null>(() => {
    try {
      return localStorage.getItem(LS_OWNER_NAME);
    } catch {
      return null;
    }
  });
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

  const setOwnerName = useCallback((next: string | null) => {
    const trimmed = next?.trim() ? next.trim() : null;
    setOwnerNameState(trimmed);
    writeStored(LS_OWNER_NAME, trimmed);
  }, []);

  const resetPreferences = useCallback(() => {
    setLanguage("zh");
    setTheme("office");
    setOwnerName(null);
  }, [setLanguage, setTheme, setOwnerName]);

  const userName = ownerName ?? t.user;

  const value = useMemo<I18nContextValue>(
    () => ({
      locale,
      t,
      language,
      setLanguage,
      theme,
      setTheme,
      ownerName,
      setOwnerName,
      userName,
      resetPreferences,
    }),
    [
      locale,
      t,
      language,
      setLanguage,
      theme,
      setTheme,
      ownerName,
      setOwnerName,
      userName,
      resetPreferences,
    ]
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
