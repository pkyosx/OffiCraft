import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { zh, type Dict } from "./locales/zh";
import { en } from "./locales/en";
import { api } from "../api";
import { hasToken, AUTH_LOGIN_EVENT } from "../api/auth";
import {
  isValidDisplayTheme,
  type ThemeBundle,
  type AvatarKind,
  type NavIconKey,
} from "../lib/themeBundle";
import { applyWording } from "./wording";

export type Locale = "zh" | "en";
/** User-selectable language (mockup 語言 toggle offers only 中文 / English). */
export type Language = "zh" | "en";
/** Built-in visual theme (辦公室). office is the ONLY built-in — every other
 * theme (e.g. 修仙) is now an importable custom bundle. The ACTIVE selector is
 * a plain string (T-16a1 P2): the built-in name here, or a custom bundle's id. */
export type Theme = "office";

/** Matches a custom bundle id (mirrors THEME_ID_RE in lib/themeBundle). */
const THEME_ID_RE = /^[a-z0-9][a-z0-9-]{1,63}$/;

function isSelectableTheme(v: string): boolean {
  return v === "office" || THEME_ID_RE.test(v);
}

const DICTS: Record<Locale, Dict> = { zh, en };

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

// The theme cache admits a built-in name OR a custom bundle id (the id's bundle
// arrives with the login reconcile; until then the apply effect falls back to
// the neutral office base — see the apply effect below).
function readStoredTheme(): string {
  try {
    const v = localStorage.getItem(LS_THEME);
    if (v && isSelectableTheme(v)) return v;
  } catch {
    // localStorage unavailable — fall through to default
  }
  return "office";
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
  /** Effective render locale — follows the language toggle (theme↔locale are
   * decoupled, T-16a1 P1: a visual theme never hijacks the UI language). */
  locale: Locale;
  t: Dict;
  language: Language;
  setLanguage: (next: Language) => void;
  /** Active theme: the built-in name ("office") or a custom bundle id. */
  theme: string;
  setTheme: (next: string) => void;
  /** The active custom theme's per-role avatar images (T-16a1 P5; T-ea81), or
   * undefined when the active theme carries none (the built-in office, or a
   * custom theme with no avatars overlay). The Avatar component reads this to
   * render a member/outsource/owner/assistant avatar image, falling back to the
   * built-in glyph when absent. */
  activeAvatars?: Partial<Record<AvatarKind, string>>;
  /** The active custom theme's studio logo image (T-ea81), or undefined when the
   * active theme carries none — the top bar then renders its built-in mark. */
  activeLogo?: string;
  /** The active custom theme's per-nav-tab icon images (T-ea81), or undefined
   * when the active theme carries none — each tab then keeps its built-in icon. */
  activeNavIcons?: Partial<Record<NavIconKey, string>>;
  /** The owner's saved custom theme bundles (server-backed, reconciled at
   * login). Empty until reconcile / when none are saved. */
  customThemes: ThemeBundle[];
  /** Replace the custom-theme set (import / edit / delete). Optionally switch
   * the active theme in the SAME server PATCH — required when deleting the
   * active theme so the server's dangling-reset stays in sync. */
  commitCustomThemes: (next: ThemeBundle[], nextTheme?: string) => void;
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
  const [theme, setThemeState] = useState<string>(() => readStoredTheme());
  const [customThemes, setCustomThemesState] = useState<ThemeBundle[]>([]);
  // Effective copy locale: the user's language toggle. Themes are copy-neutral
  // by default (theme↔locale decoupled, T-16a1 P1) — a visual theme never
  // hijacks the UI language; copy comes from `language`, with a custom theme's
  // optional 用詞 overlay laid on top (below).
  const locale: Locale = language;
  // The active custom theme's 用詞 (wording) overlay for the current language,
  // laid on top of the base dict (T-16a1 P3). The built-in office theme has no
  // user overlay; a custom theme keys its overlay on `language` (zh/en). FALLBACK
  // (owner decision b): codes without an override keep the base dict's text, so
  // the interface's original language is preserved for everything unwrapped.
  const t = useMemo(() => {
    const base = DICTS[locale];
    const overlay = (customThemes ?? []).find((b) => b.id === theme)?.wording?.[
      language
    ];
    return applyWording(base, overlay);
  }, [locale, theme, language, customThemes]);

  // The active custom theme's avatar images (T-16a1 P5). Unlike colours/fonts
  // (CSS vars applied to documentElement), avatars are IMAGES the Avatar
  // component renders as <img>, so they ride the context rather than the DOM.
  // The built-in office theme carries none; a dangling active id resolves to
  // undefined (office glyph fallback — office never degrades).
  const activeAvatars = useMemo(
    () => (customThemes ?? []).find((b) => b.id === theme)?.avatars,
    [customThemes, theme]
  );

  // The active custom theme's studio logo image + per-nav-tab icons (T-ea81).
  // Like avatars, these are IMAGES rendered as <img> (top-bar logo / nav-tab
  // icons), so they ride the context rather than the DOM. Absent → the built-in
  // logo mark / built-in nav icons (office never degrades).
  const activeLogo = useMemo(
    () => (customThemes ?? []).find((b) => b.id === theme)?.logo,
    [customThemes, theme]
  );

  const activeNavIcons = useMemo(
    () => (customThemes ?? []).find((b) => b.id === theme)?.navIcons,
    [customThemes, theme]
  );

  // The --color-* inline props applied for the current custom theme, remembered
  // so the NEXT apply can remove exactly this set before painting the next one.
  const appliedTokensRef = useRef<string[]>([]);

  // Apply the active theme. The office built-in rides <html data-theme> and any
  // leftover inline vars from a previous custom theme are cleared. A custom id
  // resolves to its bundle: take the neutral office base via data-theme, then
  // push each colour onto documentElement via setProperty (the value is NEVER
  // concatenated into a stylesheet — the security boundary). A dangling id
  // (bundle not yet reconciled / deleted) falls back to the office base.
  useEffect(() => {
    const root = document.documentElement;
    for (const tok of appliedTokensRef.current) root.style.removeProperty(tok);
    appliedTokensRef.current = [];

    if (theme === "office") {
      root.dataset.theme = theme;
      return;
    }
    const bundle = customThemes.find((b) => b.id === theme);
    root.dataset.theme = "office";
    if (bundle) {
      const applied: string[] = [];
      for (const [tok, val] of Object.entries(bundle.colors)) {
        root.style.setProperty(tok, val);
        applied.push(tok);
      }
      // The optional font overlay (T-16a1 P4): push each chosen --font-* stack
      // onto documentElement the SAME way (setProperty — the value is a member
      // of the closed safe-family allowlist, never concatenated into CSS). A
      // token the bundle omits keeps theme.css's default (office never
      // degrades), and the removeProperty pass above clears a previous theme's
      // font choice before this one paints.
      for (const [tok, val] of Object.entries(bundle.fonts ?? {})) {
        root.style.setProperty(tok, val);
        applied.push(tok);
      }
      appliedTokensRef.current = applied;
    }
  }, [theme, customThemes]);

  // Low-level cache writes: local state + the localStorage pre-auth cache, NO
  // server write. Shared by the public setters (which add the server PATCH), the
  // login reconcile (server → cache), and resetPreferences (local-only).
  const cacheLanguage = useCallback((next: Language) => {
    setLanguageState(next);
    writeStored(LS_LANGUAGE, next);
  }, []);

  const cacheTheme = useCallback((next: string) => {
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
    (next: string) => {
      cacheTheme(next);
      if (hasToken()) {
        api
          .patchServerSettings({ displayTheme: next })
          .catch((e) => console.warn("setTheme: server sync failed", e));
      }
    },
    [cacheTheme]
  );

  // Replace the custom-theme set (import / rename / re-colour / delete). The
  // server couples custom_themes + display_theme in one PATCH: deleting the
  // active theme resets display_theme to "". Callers that touch the active
  // theme pass `nextTheme` so both the local state and the single server PATCH
  // stay consistent with that reset. State updates apply optimistically; a
  // failed sync leaves them in place (next login reconcile settles divergence).
  const commitCustomThemes = useCallback(
    (next: ThemeBundle[], nextTheme?: string) => {
      setCustomThemesState(next);
      if (nextTheme !== undefined) cacheTheme(nextTheme);
      if (hasToken()) {
        const patch: { customThemes: ThemeBundle[]; displayTheme?: string } = {
          customThemes: next,
        };
        if (nextTheme !== undefined) patch.displayTheme = nextTheme;
        api
          .patchServerSettings(patch)
          .catch((e) =>
            console.warn("commitCustomThemes: server sync failed", e)
          );
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
        // The server owns the custom-theme set — adopt it wholesale (so the
        // apply effect can resolve a custom active id to its bundle). Coerce a
        // missing field to [] so downstream `.find` (apply effect + wording
        // memo) never sees undefined.
        setCustomThemesState(s.customThemes ?? []);
        // Adopt a stored active theme only when it is actually selectable given
        // that set (a built-in, or an id present in it). "" (never set) or a
        // dangling id keeps the local cache — a stale server value must not
        // override a live local choice.
        const ids = new Set(s.customThemes.map((b) => b.id));
        if (s.displayTheme !== "" && isValidDisplayTheme(s.displayTheme, ids)) {
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
    // The custom set is server-backed — clear only the LOCAL mirror so the next
    // owner's paint is not tinted; the server copy is untouched (re-adopted at
    // that owner's reconcile).
    setCustomThemesState([]);
  }, [cacheLanguage, cacheTheme]);

  const value = useMemo<I18nContextValue>(
    () => ({
      locale,
      t,
      language,
      setLanguage,
      theme,
      setTheme,
      activeAvatars,
      activeLogo,
      activeNavIcons,
      customThemes,
      commitCustomThemes,
      resetPreferences,
    }),
    [
      locale,
      t,
      language,
      setLanguage,
      theme,
      setTheme,
      activeAvatars,
      activeLogo,
      activeNavIcons,
      customThemes,
      commitCustomThemes,
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

/** The active theme's per-member-type avatar images (T-16a1 P5), or undefined.
 * DEFENSIVE variant of useI18n for the Avatar leaf: it reads the context
 * WITHOUT throwing when no provider is present (an Avatar rendered in an
 * isolated test/story with no I18nProvider just falls back to the built-in
 * glyph rather than crashing). */
export function useActiveAvatars(): Partial<Record<AvatarKind, string>> | undefined {
  return useContext(I18nContext)?.activeAvatars;
}

/** The active theme's studio logo image (T-ea81), or undefined. DEFENSIVE like
 * useActiveAvatars: reads the context WITHOUT throwing when no provider is
 * present, so a logo consumer in an isolated test/story falls back to the
 * built-in mark rather than crashing. */
export function useActiveLogo(): string | undefined {
  return useContext(I18nContext)?.activeLogo;
}

/** The active theme's per-nav-tab icon images (T-ea81), or undefined. DEFENSIVE
 * like useActiveAvatars: reads the context WITHOUT throwing when no provider is
 * present, so a nav-icon consumer in an isolated test/story falls back to the
 * built-in icons rather than crashing. */
export function useActiveNavIcons(): Partial<Record<NavIconKey, string>> | undefined {
  return useContext(I18nContext)?.activeNavIcons;
}
