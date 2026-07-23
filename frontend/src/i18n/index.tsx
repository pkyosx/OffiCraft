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
import { api } from "../api";
import { hasToken, AUTH_LOGIN_EVENT } from "../api/auth";

export type Locale = "zh" | "en" | "xian";
/** User-selectable language (mockup 語言 toggle offers only 中文 / English). */
export type Language = "zh" | "en";
/** Visual theme (mockup 主題 toggle: 辦公室 default / 修仙). */
export type Theme = "office" | "xian";

const DICTS: Record<Locale, Dict> = { zh, en, xian };

// Theme→copy decoupling (T-16a1 P1). A visual theme is copy-neutral by default:
// it restyles (data-theme → CSS tokens) but leaves wording to the language
// toggle. A theme only overrides copy if it EXPLICITLY opts in here — so adding
// a new visual theme (or a user-defined one) can never hijack the UI language.
// Only 修仙 opts in today, mapping to its immersive `xian` dict; that legacy
// coupling is preserved verbatim (no regression) until P3 replaces this map
// with per-theme 用詞包 (wording-override) bundles.
const THEME_COPY_LOCALE: Partial<Record<Theme, Locale>> = { xian: "xian" };

// localStorage keys for theme/language. DUAL-LAYER (T-0b41-p2): these prefs are
// now server-backed (DB display.theme / display.language behind the owner-gated
// /api/settings) so they follow the owner across devices — BUT they must apply
// BEFORE login, and /api/settings is unreachable pre-auth (401). So localStorage
// is the pre-auth CACHE (zero-flash first paint) and the server is the
// cross-device TRUTH, reconciled in at login and written back to the cache.
// NOTE: neither the studio/org name (T-d693) nor the owner nickname (T-0b41) is
// here — those are server-only (see hooks/useOrgName + hooks/useOwnerName).
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
  // Effective copy locale: a theme's explicit copy-override (only 修仙 today),
  // else the user's language toggle. Themes without an override are visual-only.
  const locale: Locale = THEME_COPY_LOCALE[theme] ?? language;
  const t = DICTS[locale];

  // Reflect the theme on <html data-theme> so CSS can restyle (see theme.css).
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  // Low-level cache writes: local state + the localStorage pre-auth cache, NO
  // server write. Shared by the public setters (which add the server PATCH), the
  // login reconcile (server → cache), and resetPreferences (local-only).
  const cacheLanguage = useCallback((next: Language) => {
    setLanguageState(next);
    writeStored(LS_LANGUAGE, next);
  }, []);

  const cacheTheme = useCallback((next: Theme) => {
    setThemeState(next);
    writeStored(LS_THEME, next);
  }, []);

  // Public setters (owner edits from ProfileDropdown): apply locally at once
  // (instant, and the pre-auth cache for next load) AND push to the server so
  // the choice syncs to the owner's other devices. Server sync is best-effort:
  // a failed PATCH leaves the local value in place (local is this session's
  // truth; the next login reconcile settles any divergence) rather than snapping
  // the visible theme back on a network blip. Gated on hasToken() — pre-auth
  // there is no server to write, and an unguarded PATCH would 401 → auth-expired.
  const setLanguage = useCallback(
    (next: Language) => {
      cacheLanguage(next);
      if (hasToken()) {
        api
          .patchServerSettings({ displayLanguage: next })
          .catch((e) => console.warn("setLanguage: server sync failed", e));
      }
    },
    [cacheLanguage]
  );

  const setTheme = useCallback(
    (next: Theme) => {
      cacheTheme(next);
      if (hasToken()) {
        api
          .patchServerSettings({ displayTheme: next })
          .catch((e) => console.warn("setTheme: server sync failed", e));
      }
    },
    [cacheTheme]
  );

  // Login reconcile (server = cross-device truth): pull /api/settings and adopt
  // a real, valid display pref that the server has stored, writing it back to
  // the localStorage cache so the NEXT pre-auth first paint is already correct.
  // "" (never set) or an unreachable/failing load keeps the cache showing — a
  // failed load must never masquerade as "reset to default". Applying an equal
  // value is a React state no-op, so the common (cache == server) case does not
  // repaint — no flash.
  const reconcileFromServer = useCallback(() => {
    api
      .getServerSettings()
      .then((s) => {
        if (s.displayTheme === "office" || s.displayTheme === "xian") {
          cacheTheme(s.displayTheme);
        }
        if (s.displayLanguage === "zh" || s.displayLanguage === "en") {
          cacheLanguage(s.displayLanguage);
        }
      })
      .catch((e) => console.warn("i18n reconcile: load failed", e));
  }, [cacheTheme, cacheLanguage]);

  useEffect(() => {
    // Reconcile now if a token already exists (a returning session / reload
    // lands straight on the app wall), and again the instant one is minted (a
    // fresh login fires oc-auth-login from setToken). /api/settings is
    // owner-gated, so this is the earliest the server value is reachable.
    if (hasToken()) reconcileFromServer();
    const onLogin = () => reconcileFromServer();
    window.addEventListener(AUTH_LOGIN_EVENT, onLogin);
    return () => window.removeEventListener(AUTH_LOGIN_EVENT, onLogin);
  }, [reconcileFromServer]);

  const resetPreferences = useCallback(() => {
    // The honest M1 "logout": drop back to the local defaults so the next
    // owner's first paint is not tinted by this session. Local-only — logout
    // must NOT rewrite the server pref (that would clear the owner's stored
    // choice for every other device).
    cacheLanguage("zh");
    cacheTheme("office");
  }, [cacheLanguage, cacheTheme]);

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
