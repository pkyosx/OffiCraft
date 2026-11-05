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
import type { ThemeListItem } from "../api/adapter";
import { hasToken, AUTH_LOGIN_EVENT } from "../api/auth";
import {
  adoptServerSettings,
  loadServerSettings,
  refreshServerSettings,
} from "../hooks/sharedServerSettings";
import {
  isBuiltinTheme,
  isBundledBuiltinTheme,
  isValidDisplayTheme,
  type BuiltinThemeId,
  type ThemeBundle,
  type AvatarKind,
  type NavIconKey,
} from "../lib/themeBundle";
import {
  LS_THEME,
  LS_THEME_PAINT,
  LS_SEASONAL_THEME,
  LS_SEASONAL_PAINT,
  LS_THEME_SCHEDULE,
  LS_SCHEDULE_PAINT,
  paintRecordFor,
  colourPaintRecordFor,
  applyThemeToRoot,
  readValidatedPaint,
  readValidatedSchedulePaint,
  readValidatedSeasonalPaint,
  readThemeSchedule,
  tieredPaintRecords,
} from "../lib/themePaint";
import {
  activeSeasonalWindow,
  nextSeasonalBoundary,
  seasonalPickAt,
  seasonalThemeIds,
  themeIdOfFile,
} from "../lib/seasonalSchedule";
import { nextDayPart } from "../lib/dayParts";
import {
  DEFAULT_DISPLAY_THEME,
  DEFAULT_THEME_SCHEDULE,
  activeScheduleEntry,
  nextScheduleBoundary,
  type ThemeScheduleEntry,
} from "../lib/themeSchedule";
import { SEASONAL_WINDOWS, type LoadableSeasonalWindow } from "../lib/seasonalTheme";
import { loadBundledBuiltinTheme } from "../lib/bundledBuiltinThemes";
import { applyWording } from "./wording";
import { makeMessages, type Messages } from "./compose";

export type Locale = "zh" | "en";
/** User-selectable language (mockup 語言 toggle offers only 中文 / English). */
export type Language = "zh" | "en";
/** A built-in visual theme id. Every other theme (e.g. 修仙) is an importable
 * custom bundle; the ACTIVE selector is a plain string (T-16a1 P2): a built-in
 * id, or a custom bundle's id. */
export type Theme = BuiltinThemeId;

/** Matches a custom bundle id (mirrors THEME_ID_RE in lib/themeBundle). */
const THEME_ID_RE = /^[a-z0-9][a-z0-9-]{1,63}$/;

function isSelectableTheme(v: string): boolean {
  return isBuiltinTheme(v) || THEME_ID_RE.test(v);
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

// The layout-width pref (T-756f) rides the SAME dual-layer contract. It differs
// from theme/language in one way only: it is a plain bool, so there is no ""
// third state — false IS the shipped narrow column, and the server's value is
// therefore always adopted at reconcile (that is what makes turning wide OFF on
// one device propagate to the others).
const LS_WIDE = "oc.wide";

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

// The wide cache holds the same "true"/"false" text the server stores, so the
// two layers are literally the same vocabulary. Anything else (absent, garbage,
// localStorage unavailable) reads as false — the shipped narrow column.
function readStoredWide(): boolean {
  try {
    return localStorage.getItem(LS_WIDE) === "true";
  } catch {
    // localStorage unavailable — fall through to the narrow default
  }
  return false;
}

// Same vocabulary as oc.wide, but true is the shipped default: only an explicit
// "false" turns the seasonal theme off.
function readStoredSeasonalTheme(): boolean {
  try {
    return localStorage.getItem(LS_SEASONAL_THEME) !== "false";
  } catch {
    // localStorage unavailable — fall through to the shipped default
  }
  return true;
}

// setTimeout fires at once for any delay above 2^31-1 ms (~24.8 days), so a
// boundary further out is reached in steps.
const MAX_TIMER_DELAY_MS = 2 ** 31 - 1;

/** How long before a schedule slot's (or seasonal day part's) start its bundle
 * is fetched. */
const SCHEDULE_PREFETCH_LEAD_MS = 10 * 60 * 1000;

/** A theme's bundle: a bundled built-in from its own chunk, else the server's. */
function fetchThemeBundle(id: string): Promise<ThemeBundle> {
  return isBundledBuiltinTheme(id) ? loadBundledBuiltinTheme(id) : api.getTheme(id);
}

function scheduledCustomThemes(schedule: readonly ThemeScheduleEntry[]): string[] {
  return [...new Set(schedule.map((e) => e.theme).filter((id) => !isBuiltinTheme(id)))];
}

function writeStored(key: string, value: string | null): boolean {
  try {
    if (value == null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
    return true;
  } catch {
    // ignore — best-effort persistence
    return false;
  }
}

interface I18nContextValue {
  /** Effective render locale — follows the language toggle (theme↔locale are
   * decoupled, T-16a1 P1: a visual theme never hijacks the UI language). */
  locale: Locale;
  t: Dict;
  /** The PARAMETERISED messages (T-081b), composed from `t`'s overridable
   * static fragments — `m.taskTerminateConfirmBody(title)` rather than a
   * template leaf a wording overlay could never reach. Rebuilt with `t`, so an
   * active theme's re-worded fragment shows up here too. */
  msg: Messages;
  language: Language;
  setLanguage: (next: Language) => void;
  /** Active theme: a built-in id ("office" / "office-light") or a custom bundle id. */
  theme: string;
  setTheme: (next: string) => void;
  /** Whether the cockpit uses the WIDE layout (T-756f): the centred ~1040px
   * content column is lifted, the side gutters stay. false = the narrow
   * centred column (the default). */
  wide: boolean;
  setWide: (next: boolean) => void;
  /** display_seasonal_theme: whether the built-in seasonal theme replaces the
   * active theme while its window is open. `theme` itself is never changed by
   * it. */
  seasonalTheme: boolean;
  setSeasonalTheme: (next: boolean) => void;
  /** display_theme_schedule, sorted by start; [] = off. Below the seasonal
   * theme and above `theme`, which it never rewrites. */
  themeSchedule: ThemeScheduleEntry[];
  /** Save the schedule ([] turns it off). Not optimistic: rejects with the
   * server's refusal, and takes effect only once saved and its custom themes
   * are in hand. */
  setThemeSchedule: (next: ThemeScheduleEntry[]) => Promise<ThemeScheduleEntry[]>;
  /** 恢復預設: the default schedule, the seasonal theme on, and display_theme
   * DEFAULT_DISPLAY_THEME, in one settings write. Custom themes are untouched.
   * Not optimistic, like setThemeSchedule. */
  restoreDefaultAppearance: () => Promise<ThemeScheduleEntry[]>;
  /** The schedule row's theme on screen now; null without a schedule or while
   * the seasonal theme is shown. */
  scheduledTheme: string | null;
  /** The seasonal theme's name while it is on screen and loaded, else null. */
  seasonalThemeName: string | null;
  /** The id of the theme on screen: seasonal, else the schedule's row, else `theme`. */
  shownTheme: string;
  /** The bundle painted on screen; null for a CSS built-in or while it loads. */
  shownThemeBundle: ThemeBundle | null;
  /** The shown theme's per-role avatar images (T-16a1 P5; T-ea81) — the
   * seasonal theme's inside its window, else the active custom theme's — or
   * undefined when the shown theme carries none (the built-in office, or a
   * custom theme with no avatars overlay). The Avatar component reads this to
   * render a staff/outsource/owner/assistant avatar image, falling back to the
   * built-in glyph when absent. */
  activeAvatars?: Partial<Record<AvatarKind, string>>;
  /** The shown theme's studio logo image (T-ea81), or undefined when the
   * shown theme carries none — the top bar then renders its built-in mark. */
  activeLogo?: string;
  /** The shown theme's per-nav-tab icon images (T-ea81), or undefined
   * when the shown theme carries none — each tab then keeps its built-in icon. */
  activeNavIcons?: Partial<Record<NavIconKey, string>>;
  /** The owner's saved themes as ONE LINE EACH — id and name, no colours and
   * no images (T-83ef). This is what the pickers render, and it is deliberately
   * NOT the bundles: a bundle carries its images embedded, so the whole set runs
   * to megabytes and used to be fetched on every login. Empty until reconcile /
   * when none are saved. */
  themeList: ThemeListItem[];
  /** The ACTIVE custom theme in full, or null when the active theme is a
   * built-in or its bundle has not been fetched yet.
   *
   * Every consumer inside this provider — the wording overlay, the avatars, the
   * logo, the nav icons, the colour apply — only ever wanted the active one;
   * they used to reach it by scanning the whole set. Naming it directly is what
   * lets the set shrink to `themeList`. */
  activeThemeBundle: ThemeBundle | null;
  /** Fetch ONE theme in full — what editing and exporting need, and the only
   * thing that needs a bundle at all. Rejects when the theme is gone. */
  loadTheme: (id: string) => Promise<ThemeBundle>;
  /** Create or replace ONE theme (import / add / edit-save). The whole-set write
   * this replaced re-sent every theme with every embedded image to change one
   * colour. Rejects on a refusal so the caller can say what went wrong. */
  saveTheme: (bundle: ThemeBundle) => Promise<void>;
  /** Delete ONE theme. When it is the ACTIVE one the server resets its stored
   * display_theme and says so; this switches the cockpit back to the default built-in (office)
   * so the owner is never looking at a theme that no longer exists. */
  removeTheme: (id: string) => Promise<void>;
  /** Re-read the theme list. A theme added or deleted elsewhere is not pushed;
   * the theme settings call this when they open. */
  refreshThemeList: () => void;
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
  const [wide, setWideState] = useState<boolean>(() => readStoredWide());
  const [seasonalTheme, setSeasonalThemeState] = useState<boolean>(() =>
    readStoredSeasonalTheme()
  );
  // Re-read at each window boundary (the timer below) and whenever the page is
  // shown or focused again: a timer does not count time the machine slept.
  const [clock, setClock] = useState(() => Date.now());
  const [themeSchedule, setThemeScheduleState] = useState<ThemeScheduleEntry[]>(() =>
    readThemeSchedule()
  );
  const themeScheduleRef = useRef(themeSchedule);
  // The scheduled themes fetched so far: the current slot's, and the next one's
  // from SCHEDULE_PREFETCH_LEAD_MS before its start, so a boundary does not wait
  // on the network.
  const [scheduleBundles, setScheduleBundles] = useState<Record<string, ThemeBundle>>({});
  const scheduleBundlesRef = useRef(scheduleBundles);
  // The open window's day parts seen so far, by bundle id.
  const [seasonalBundles, setSeasonalBundles] = useState<Record<string, ThemeBundle>>({});
  // ⚠️ No retry: browsers cache a failed dynamic import per URL, so importing
  // the same chunk again rejects at once; only a reload can recover it.
  const [seasonalLoadFailedId, setSeasonalLoadFailedId] = useState<string | null>(null);
  const [themeList, setThemeList] = useState<ThemeListItem[]>([]);
  // A settings or theme-list read that overlaps any part of one of this tab's
  // own writes may carry the value from before it. Such a read is not adopted; the reread
  // waits until every write has settled.
  const writesInFlightRef = useRef(0);
  const writeEpochRef = useRef(0);
  const rereadAfterWritesRef = useRef(false);
  const rereadRef = useRef<() => void>(() => {});
  const trackWrite = useCallback(function trackWrite<T>(write: () => Promise<T>): Promise<T> {
    writeEpochRef.current += 1;
    writesInFlightRef.current += 1;
    return write().finally(() => {
      writeEpochRef.current += 1;
      writesInFlightRef.current -= 1;
      if (writesInFlightRef.current === 0 && rereadAfterWritesRef.current) {
        rereadAfterWritesRef.current = false;
        rereadRef.current();
      }
    });
  }, []);
  /** True when a read issued at `epoch` must not be adopted; schedules the reread. */
  const readCrossedWrite = useCallback((epoch: number) => {
    if (writesInFlightRef.current === 0 && epoch === writeEpochRef.current) return false;
    if (writesInFlightRef.current > 0) rereadAfterWritesRef.current = true;
    else rereadRef.current();
    return true;
  }, []);
  const [activeThemeBundle, setActiveThemeBundle] = useState<ThemeBundle | null>(
    null
  );
  const activeThemeBundleRef = useRef(activeThemeBundle);
  activeThemeBundleRef.current = activeThemeBundle;
  // [T-1500] "the server has spoken". Flipped in reconcile's .then only.
  const [themesLoaded, setThemesLoaded] = useState(false);
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
  // 🔴 [T-83ef] ONE authority for "the bundle we are allowed to render": the
  // fetched bundle ONLY while it is the active theme's.
  //
  // `theme` moves the instant the owner picks one; `activeThemeBundle` cannot
  // move until its fetch lands a round trip later. Before the split there was
  // no gap — the bundle was `customThemes.find(b => b.id === theme)`, resolved
  // synchronously — so nothing had to say what happens in between. Now
  // something does, and the answer must be the SAME for every consumer: the
  // colour apply already refused a mismatched bundle, but wording, avatars,
  // logo and nav icons each read the raw state, so for one round trip a switch
  // rendered the NEXT theme's colours over the PREVIOUS theme's images and
  // words. Not a rare race — every single theme switch, for as long as a
  // several-hundred-KB bundle takes to arrive.
  //
  // Falling back to the built-in for that moment is the same answer the colour
  // path already gave; a consistent built-in beats a blend of two themes that
  // never existed.
  const bundleForTheme = useMemo(
    () =>
      activeThemeBundle && activeThemeBundle.id === theme
        ? activeThemeBundle
        : null,
    [activeThemeBundle, theme]
  );

  const openSeasonalWindow = useMemo(
    () => activeSeasonalWindow(new Date(clock), SEASONAL_WINDOWS),
    [clock]
  );
  const openSeasonalPick = openSeasonalWindow
    ? seasonalPickAt(new Date(clock), openSeasonalWindow)
    : null;
  // The seasonal bundle id on screen (this day part's), or null.
  const seasonalId =
    seasonalTheme && openSeasonalPick && openSeasonalPick.id !== seasonalLoadFailedId
      ? openSeasonalPick.id
      : null;
  // The theme on screen: the seasonal one while its window is open and the
  // owner has not turned it off, otherwise the schedule's current entry, else
  // the selected theme. A seasonal or scheduled bundle still in flight resolves
  // to null, never to the selected theme's.
  const scheduledTheme = seasonalId
    ? null
    : (activeScheduleEntry(new Date(clock), themeSchedule)?.theme ?? null);
  const shownTheme = seasonalId ?? scheduledTheme ?? theme;
  const shownBundle = useMemo(() => {
    if (seasonalId) {
      return seasonalBundles[seasonalId] ?? null;
    }
    if (scheduledTheme) {
      return scheduleBundles[scheduledTheme] ?? (scheduledTheme === theme ? bundleForTheme : null);
    }
    return bundleForTheme;
  }, [seasonalId, seasonalBundles, scheduledTheme, theme, scheduleBundles, bundleForTheme]);

  const seasonalThemeName =
    (seasonalId && seasonalBundles[seasonalId]?.name) || null;
  // While the seasonal theme is shown, its current day part is the only record
  // with a picture: two seasonal pictures plus the schedule's and the display
  // theme's exceed Safari's ~2.5M-character origin quota.
  const seasonalShown = seasonalId !== null;
  const seasonalShownRef = useRef(seasonalShown);
  seasonalShownRef.current = seasonalShown;

  const t = useMemo(() => {
    const base = DICTS[locale];
    const overlay = shownBundle?.wording?.[language];
    return applyWording(base, overlay);
  }, [locale, language, shownBundle]);

  // The composed parameterised messages ride the SAME memo inputs as `t` — a
  // wording overlay change re-composes them, so no message can serve stale
  // vocabulary after a theme switch.
  const msg = useMemo(() => makeMessages(t, language), [t, language]);

  // The active custom theme's avatar images (T-16a1 P5). Unlike colours/fonts
  // (CSS vars applied to documentElement), avatars are IMAGES the Avatar
  // component renders as <img>, so they ride the context rather than the DOM.
  // The built-in office theme carries none; a dangling active id resolves to
  // undefined (office glyph fallback — office never degrades).
  const activeAvatars = useMemo(
    () => shownBundle?.avatars,
    [shownBundle]
  );

  // The active custom theme's studio logo image + per-nav-tab icons (T-ea81).
  // Like avatars, these are IMAGES rendered as <img> (top-bar logo / nav-tab
  // icons), so they ride the context rather than the DOM. Absent → the built-in
  // logo mark / built-in nav icons (office never degrades).
  const activeLogo = useMemo(
    () => shownBundle?.logo,
    [shownBundle]
  );

  const activeNavIcons = useMemo(
    () => shownBundle?.navIcons,
    [shownBundle]
  );

  // The --color-* inline props applied for the current custom theme, remembered
  // so the NEXT apply can remove exactly this set before painting the next one.
  // [T-1500] seeded from the pre-React applier: ONE ledger, two writers.
  const appliedTokensRef = useRef<string[]>(window.__ocPaintTokens ?? []);

  // Apply the active theme. A built-in rides <html data-theme> (theme.css keys
  // each built-in's block off it) and any leftover inline vars from a previous
  // custom theme are cleared. A custom id resolves to its bundle: take the
  // neutral office base via data-theme, then push each colour onto
  // documentElement via setProperty (the value is NEVER concatenated into a
  // stylesheet — the security boundary). A dangling id (bundle not yet
  // reconciled / deleted) falls back to the office base.
  useEffect(() => {
    const root = document.documentElement;
    for (const tok of appliedTokensRef.current) root.style.removeProperty(tok);
    appliedTokensRef.current = [];

    if (isBuiltinTheme(shownTheme)) {
      root.dataset.theme = shownTheme;
      return;
    }
    root.dataset.theme = "office";
    if (shownBundle) {
      appliedTokensRef.current = applyThemeToRoot(root, shownBundle);
      return;
    }
    if (seasonalId) {
      const cached = readValidatedSeasonalPaint(seasonalId);
      if (cached) {
        appliedTokensRef.current = applyThemeToRoot(root, cached);
      }
      return;
    }
    if (scheduledTheme) {
      const cached = readValidatedSchedulePaint(scheduledTheme);
      if (cached) appliedTokensRef.current = applyThemeToRoot(root, cached);
      return;
    }
    // [T-1500] the bundle is unresolved AND reconcile has not spoken: keep
    // the cached picture standing (it is already on the DOM when the pre-React
    // applier ran; re-applying the same values is a visual no-op and covers the
    // paths where it did not run — dev, CT, a cold main.tsx).
    if (!themesLoaded) {
      const cached = readValidatedPaint();
      if (cached && cached.id === theme) {
        appliedTokensRef.current = applyThemeToRoot(root, cached);
      }
    }
  }, [theme, shownTheme, shownBundle, seasonalId, scheduledTheme, themesLoaded]);

  useEffect(() => {
    const now = new Date(clock);
    const next = [
      nextSeasonalBoundary(now, SEASONAL_WINDOWS),
      nextScheduleBoundary(now, themeSchedule),
    ].reduce<Date | null>((a, b) => (b && (!a || b < a) ? b : a), null);
    if (!next) return;
    const id = window.setTimeout(
      () => setClock(Date.now()),
      Math.min(next.getTime() - clock, MAX_TIMER_DELAY_MS)
    );
    return () => window.clearTimeout(id);
  }, [clock, themeSchedule]);

  useEffect(() => {
    const reread = () => {
      if (document.visibilityState === "visible") setClock(Date.now());
    };
    document.addEventListener("visibilitychange", reread);
    window.addEventListener("focus", reread);
    return () => {
      document.removeEventListener("visibilitychange", reread);
      window.removeEventListener("focus", reread);
    };
  }, []);

  const seasonalBundlesRef = useRef(seasonalBundles);
  seasonalBundlesRef.current = seasonalBundles;
  const seasonalInFlight = useRef(new Set<string>());

  // One record per day part seen in this window, so a later page load in any
  // of them paints. Whole for the day part on screen, colours-only for the others seen (the
  // next one's bundle is still prefetched into memory), none for other windows;
  // all colours-only while the seasonal theme is not on screen, the window's
  // first part prefetched before it opens included.
  const writeSeasonalRecords = useCallback((w: LoadableSeasonalWindow) => {
    const at = new Date(Math.max(Date.now(), w.start.getTime()));
    const full = new Set(seasonalShownRef.current ? [seasonalPickAt(at, w).id] : []);
    const records = tieredPaintRecords(
      seasonalThemeIds(w),
      full,
      (id) => seasonalBundlesRef.current[id] ?? readValidatedSeasonalPaint(id)
    );
    if (!writeStored(LS_SEASONAL_PAINT, records.length ? JSON.stringify(records) : null)) {
      writeStored(LS_SEASONAL_PAINT, null);
    }
  }, []);

  // Loads the day part `pick` of window `w` once.
  const loadSeasonalPick = useCallback(
    (w: LoadableSeasonalWindow, pick: { id: string; file: string | undefined }) => {
      if (seasonalBundlesRef.current[pick.id] || seasonalInFlight.current.has(pick.id)) return;
      seasonalInFlight.current.add(pick.id);
      w.load(pick.file)
        .then((b) => {
          if (b.id !== pick.id) throw new Error(`seasonal theme file carries id ${b.id}, not ${pick.id}`);
          seasonalBundlesRef.current = { ...seasonalBundlesRef.current, [b.id]: b };
          setSeasonalBundles(seasonalBundlesRef.current);
          writeSeasonalRecords(w);
        })
        .catch((e) => {
          console.warn("seasonal theme: load failed", e);
          setSeasonalLoadFailedId(pick.id);
        })
        .finally(() => seasonalInFlight.current.delete(pick.id));
    },
    [writeSeasonalRecords]
  );
  useEffect(() => {
    if (seasonalId && openSeasonalWindow && openSeasonalPick) {
      loadSeasonalPick(openSeasonalWindow, openSeasonalPick);
      if (seasonalBundlesRef.current[seasonalId]) writeSeasonalRecords(openSeasonalWindow);
    }
  }, [seasonalId]);

  // The next day part is prefetched like the next schedule slot, and so is a
  // window's first day part before the window opens.
  useEffect(() => {
    if (!seasonalTheme) return;
    let w: LoadableSeasonalWindow | null;
    let next: { at: Date; row: { start: string; theme: string } } | null;
    if (openSeasonalWindow) {
      w = openSeasonalWindow;
      if (!seasonalId || !w.schedule?.length) return;
      next = nextDayPart(new Date(clock), w.schedule);
      if (!next || next.at.getTime() >= w.end.getTime()) return;
    } else {
      const at = nextSeasonalBoundary(new Date(clock), SEASONAL_WINDOWS);
      w = at ? activeSeasonalWindow(at, SEASONAL_WINDOWS) : null;
      const first = w && at ? seasonalPickAt(at, w) : null;
      if (!w || !at || !first?.file) return;
      next = { at, row: { start: "", theme: first.file } };
    }
    const pick = {
      id: w.schedule?.length ? themeIdOfFile(next.row.theme) : w.id,
      file: next.row.theme,
    };
    if (seasonalBundles[pick.id] || pick.id === seasonalLoadFailedId) return;
    const lead = next.at.getTime() - SCHEDULE_PREFETCH_LEAD_MS - Date.now();
    if (lead <= 0) {
      loadSeasonalPick(w, pick);
      return;
    }
    const timer = window.setTimeout(() => loadSeasonalPick(w, pick), Math.min(lead, MAX_TIMER_DELAY_MS));
    return () => window.clearTimeout(timer);
  }, [clock, seasonalTheme, seasonalId, openSeasonalWindow, seasonalBundles, seasonalLoadFailedId, loadSeasonalPick]);

  useEffect(() => {
    if (!openSeasonalWindow) {
      seasonalBundlesRef.current = {};
      setSeasonalBundles({});
      writeStored(LS_SEASONAL_PAINT, null);
    }
  }, [openSeasonalWindow]);

  // Apply the layout width (T-756f). Narrow REMOVES the attribute rather than
  // writing data-layout="narrow": the default DOM then looks exactly as it did
  // before this pref existed, so the shipped narrow chrome cannot regress. The
  // CSS override keys off :root[data-layout="wide"] (components/chrome.css).
  useEffect(() => {
    const root = document.documentElement;
    if (wide) root.dataset.layout = "wide";
    else delete root.dataset.layout;
  }, [wide]);

  // Low-level cache writes: local state + the localStorage pre-auth cache, NO
  // server write. Shared by the public setters (which add the server PATCH), the
  // login reconcile (server → cache), and resetPreferences (local-only).
  const cacheLanguage = useCallback((next: Language) => {
    setLanguageState(next);
    writeStored(LS_LANGUAGE, next);
  }, []);

  // [T-1500] M3: the paint write must be driven by the BUNDLE SET, not by
  // the id — editing a theme's colours does not change its id.
  // 🔴 ONE writer, and it is NOT this line's old job. `cacheTheme` is the only
  // caller of setThemeState, and it moves this ref with the decision (see
  // there). The `themeRef.current = theme` that used to sit here was therefore
  // writing a value the ref already held — except in the one case that matters,
  // where it wrote the value the ref had just been moved AWAY from: a render
  // that happens before the state flush would push it BACKWARDS and undo the
  // fix. It was also a mutation during render, which StrictMode and concurrent
  // rendering are entitled to run more than once.
  //
  // It survived because it looked exactly like the writer that carries the
  // weight — the same shape this ticket has been paying for all along, this
  // time introduced BY the fix for it.
  //
  // ⚠️ BE HONEST ABOUT THE EVIDENCE: this removal is UNTESTABLE either way.
  // Taking the line out leaves all 2204 green, and putting it back leaves all
  // 2204 green — measured, both directions. So nothing here is a measurement;
  // the reasons are structural, and they are the whole case:
  //   - NOBODY READS THIS REF DURING RENDER. All eight touch sites sit inside
  //     useCallback bodies, every one of them after an await. The only thing
  //     the deleted line offered was synchrony *at render time*, which no
  //     reader ever needed — so removing it cannot hand anyone a stale value.
  //     This is the load-bearing reason.
  //   - it mutates a ref during render, and main.tsx really does wrap the app
  //     in <StrictMode>, which is entitled to run that body twice.
  //   - two writers that look identical, one load-bearing and one not, is the
  //     exact confusion this file has already been bitten by.
  //
  // One claim was WALKED BACK rather than left standing: an earlier draft of
  // this note led with "a render before the state flush would push the ref
  // BACKWARDS". That shape is real but its REACHABILITY IS UNPROVEN — cacheTheme
  // calls setThemeState synchronously before returning, so every later render
  // carries the update, and this repo has no useTransition / startTransition /
  // Suspense to defer one. Kept here demoted rather than deleted, because
  // "sounds right, nobody checked" is precisely what this ticket kept paying
  // for; a reason that cannot be reached should not be the first one a reader
  // sees.
  // If a later reader wants it back, the bar is a test that goes red without
  // it — not a green suite, which both versions already have.
  const themeRef = useRef(theme);

  // [T-83ef] It takes the BUNDLE, not an id plus a set to search. The set is no
  // longer held in full — and the thing being cached was always the active
  // bundle, so passing it directly removes the lookup rather than hiding it.
  const writePaint = useCallback((b: ThemeBundle | null) => {
    const rec = b && (seasonalShownRef.current ? colourPaintRecordFor(b) : paintRecordFor(b));
    if (!writeStored(LS_THEME_PAINT, rec ? JSON.stringify(rec) : null)) {
      writeStored(LS_THEME_PAINT, null);
    }
  }, []);

  const cacheTheme = useCallback((next: string) => {
    setThemeState(next);
    // 🔴 [T-83ef] The ref moves HERE, with the decision — not later, with the
    // render. `themeRef.current = theme` at the top of the body only runs when
    // React re-renders, and four separate places read this ref AFTER an await
    // (setTheme's fetch, saveTheme's write, removeTheme's delete, reconcile's
    // fetch). Any promise that settles before that render sees the PREVIOUS
    // theme and concludes the owner switched away — so the code throws away the
    // very bundle it just asked for.
    //
    // This is not hypothetical and it was already costing someone: the CT story
    // in visual-guards/stories/AvatarKindStory.tsx routes around it in prose
    // ("the save's promise settles before React has re-rendered with the new
    // id … and the avatars never arrive"). The diagnosis there was right; the
    // hole just never got closed. In production the three user-facing callers
    // happen to fire from discrete DOM events, where React flushes
    // synchronously — the gap is closed by React's scheduling, not by anything
    // this file does, which is the kind of guarantee that quietly stops holding.
    //
    // Setting it where the decision is made makes all four readers correct at
    // once, and keeps ONE answer to "which theme is active right now" instead of
    // one per call site.
    themeRef.current = next;
    writeStored(LS_THEME, next);
  }, []);

  const cacheWide = useCallback((next: boolean) => {
    setWideState(next);
    writeStored(LS_WIDE, next ? "true" : "false");
  }, []);

  const cacheSeasonalTheme = useCallback((next: boolean) => {
    setSeasonalThemeState(next);
    writeStored(LS_SEASONAL_THEME, next ? "true" : "false");
  }, []);

  const cacheThemeSchedule = useCallback((next: ThemeScheduleEntry[]) => {
    setThemeScheduleState(next);
    themeScheduleRef.current = next;
    // `clock` may be hours old on a page with no pending boundary.
    setClock(Date.now());
    writeStored(LS_THEME_SCHEDULE, next.length ? JSON.stringify(next) : null);
  }, []);

  // A theme whose fetch failed keeps the record already cached for it: writing
  // without it would hand the next page load a flash.
  const adoptScheduleBundles = useCallback(
    (schedule: ThemeScheduleEntry[], bundles: Record<string, ThemeBundle>) => {
      setScheduleBundles(bundles);
      scheduleBundlesRef.current = bundles;
      const now = new Date();
      const full = new Set(
        seasonalShownRef.current
          ? []
          : [activeScheduleEntry(now, schedule)?.theme, nextDayPart(now, schedule)?.row.theme].filter(
              (id): id is string => !!id
            )
      );
      const records = tieredPaintRecords(
        scheduledCustomThemes(schedule),
        full,
        (id) => bundles[id] ?? readValidatedSchedulePaint(id)
      );
      // The display theme is not on screen while a schedule is on, so its
      // picture would only be a second copy of a slot's.
      if (schedule.length) writeStored(LS_THEME_PAINT, null);
      // A refused write (storage full) leaves the previous records standing,
      // which may predate the last edit: the next pre-paint would draw the old
      // colours. Drawing nothing is the lesser flash.
      if (!writeStored(LS_SCHEDULE_PAINT, records.length ? JSON.stringify(records) : null)) {
        writeStored(LS_SCHEDULE_PAINT, null);
      }
    },
    []
  );

  /** The bundles already in hand that `schedule` still names, plus the one its
   * slot shows now when that needs fetching. Only the current slot is fetched
   * here; the next one is prefetched shortly before its start (below). */
  const loadScheduleBundles = useCallback(async (schedule: ThemeScheduleEntry[]) => {
    const ids = scheduledCustomThemes(schedule);
    const bundles: Record<string, ThemeBundle> = {};
    for (const id of ids) {
      const held = scheduleBundlesRef.current[id];
      if (held) bundles[id] = held;
    }
    const now = activeScheduleEntry(new Date(), schedule)?.theme;
    if (now && ids.includes(now) && !bundles[now]) {
      try {
        const b = await fetchThemeBundle(now);
        if (b.id === now) bundles[now] = b;
      } catch (e) {
        console.warn("theme schedule: bundle load failed", e);
      }
    }
    return bundles;
  }, []);

  const scheduleInFlight = useRef(new Set<string>());
  /** Fetch one scheduled theme into the held set, keeping its paint record. */
  const loadScheduleBundle = useCallback(
    (id: string) => {
      if (scheduleBundlesRef.current[id] || scheduleInFlight.current.has(id)) return;
      const schedule = themeScheduleRef.current;
      if (!scheduledCustomThemes(schedule).includes(id)) return;
      const active = activeThemeBundleRef.current;
      if (active && active.id === id) {
        adoptScheduleBundles(schedule, { ...scheduleBundlesRef.current, [id]: active });
        return;
      }
      scheduleInFlight.current.add(id);
      fetchThemeBundle(id)
        .then((b) => {
          if (b.id !== id || themeScheduleRef.current !== schedule) return;
          adoptScheduleBundles(schedule, { ...scheduleBundlesRef.current, [id]: b });
        })
        .catch((e) => console.warn("theme schedule: bundle load failed", e))
        .finally(() => scheduleInFlight.current.delete(id));
    },
    [adoptScheduleBundles]
  );

  // The slot on screen loads when it is reached (open, reconcile, wake,
  // boundary); until it lands the apply effect paints its cached record.
  useEffect(() => {
    if (scheduledTheme) loadScheduleBundle(scheduledTheme);
  }, [scheduledTheme, activeThemeBundle, themeSchedule, loadScheduleBundle]);

  // As the slot moves, the slot left behind keeps only its colours.
  useEffect(() => {
    if (scheduledTheme && scheduleBundlesRef.current[scheduledTheme]) {
      adoptScheduleBundles(themeScheduleRef.current, scheduleBundlesRef.current);
    }
  }, [scheduledTheme, adoptScheduleBundles]);

  // The next slot is fetched SCHEDULE_PREFETCH_LEAD_MS before its start (at
  // once when the page opens inside that lead), so the switch needs no fetch.
  useEffect(() => {
    const now = new Date(clock);
    // While the seasonal theme shows, the next thing the schedule shows is the
    // slot current when the window ends.
    const next = seasonalId
      ? openSeasonalWindow && {
          at: openSeasonalWindow.end,
          row: activeScheduleEntry(openSeasonalWindow.end, themeSchedule),
        }
      : nextDayPart(now, themeSchedule);
    if (!next?.row) return;
    const id = next.row.theme;
    if (!scheduledCustomThemes(themeSchedule).includes(id) || scheduleBundles[id]) return;
    const lead = next.at.getTime() - SCHEDULE_PREFETCH_LEAD_MS - Date.now();
    if (lead <= 0) {
      loadScheduleBundle(id);
      return;
    }
    const timer = window.setTimeout(() => loadScheduleBundle(id), Math.min(lead, MAX_TIMER_DELAY_MS));
    return () => window.clearTimeout(timer);
  }, [clock, themeSchedule, scheduleBundles, seasonalId, openSeasonalWindow, loadScheduleBundle]);

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
        trackWrite(() => api.patchServerSettings({ displayLanguage: next }))
          .then(adoptServerSettings) // shared snapshot (T-8115)
          .catch((e) => console.warn("setLanguage: server sync failed", e));
      }
    },
    [cacheLanguage]
  );

  const displayShown = !seasonalId && !scheduledTheme;
  const displayShownRef = useRef(displayShown);
  displayShownRef.current = displayShown;
  const displayInFlight = useRef<string | null>(null);

  /** Fetch display_theme's bundle — only ever while it is the theme on screen,
   * so a schedule slot or seasonal row never pays for it too. Resolves once
   * settled, failure included. */
  const loadDisplayBundle = useCallback(
    async (id: string) => {
      if (displayInFlight.current === id) return;
      displayInFlight.current = id;
      try {
        const b = await fetchThemeBundle(id);
        // Ignore a fetch that lost a race to a later switch: writePaint would
        // cache a picture of a theme the owner is not on, and the next cold
        // load would paint it before React mounts.
        if (themeRef.current !== id) return;
        setActiveThemeBundle(b);
        // A schedule may have come on while it loaded (reconcile); then this
        // picture would only duplicate a slot's.
        if (displayShownRef.current) writePaint(b);
      } catch (e) {
        console.warn("display theme: bundle load failed", e);
      } finally {
        if (displayInFlight.current === id) displayInFlight.current = null;
      }
    },
    [writePaint]
  );

  // Re-tier every paint record when the seasonal theme comes on screen (window
  // open, page load mid-window with older whole records stored, turned on) or
  // leaves it (window end, turned off, its day part failed to load). The side
  // losing pictures is written first, so the other side's write has the room;
  // the seasonal records are rewritten after, as an earlier write of them may
  // have been refused for want of it.
  useEffect(() => {
    const held = activeThemeBundleRef.current;
    const display = held && held.id === themeRef.current ? held : null;
    const schedule = themeScheduleRef.current;
    // An empty schedule may only be the cache before reconcile: adopting it
    // would drop records reconcile is about to keep.
    const retierSchedule = () => {
      if (schedule.length) adoptScheduleBundles(schedule, scheduleBundlesRef.current);
    };
    if (seasonalShown) {
      retierSchedule();
      const cached = schedule.length ? null : (display ?? readValidatedPaint());
      if (cached) writePaint(cached);
      if (openSeasonalWindow) writeSeasonalRecords(openSeasonalWindow);
      return;
    }
    if (openSeasonalWindow) writeSeasonalRecords(openSeasonalWindow);
    retierSchedule();
    if (display && displayShownRef.current) writePaint(display);
  }, [seasonalShown, openSeasonalWindow, adoptScheduleBundles, writePaint, writeSeasonalRecords]);

  // display_theme coming on screen with its bundle not in hand (the schedule
  // turned off, a seasonal window ending): fetch it now.
  useEffect(() => {
    if (!displayShown || isBuiltinTheme(theme) || bundleForTheme) return;
    if (!themesLoaded && !isBundledBuiltinTheme(theme)) return;
    if (!isBundledBuiltinTheme(theme) && !themeList.some((x) => x.id === theme)) return;
    void loadDisplayBundle(theme);
  }, [displayShown, theme, bundleForTheme, themesLoaded, themeList, loadDisplayBundle]);

  // [T-83ef] Switching theme now has to FETCH the chosen bundle: the provider no
  // longer holds every bundle, only the active one. The id is cached first and
  // synced first, so the choice is durable even if the fetch loses a race; the
  // paint cache is written only once the bundle is actually in hand, because a
  // paint record written from nothing would clear the cached picture and hand
  // the next pre-auth load a flash.
  const setTheme = useCallback(
    (next: string) => {
      cacheTheme(next);
      if (isBuiltinTheme(next)) {
        setActiveThemeBundle(null);
        writePaint(null);
      }
      if (hasToken()) {
        trackWrite(() => api.patchServerSettings({ displayTheme: next }))
          .then(adoptServerSettings) // shared snapshot (T-8115)
          .catch((e) => console.warn("setTheme: server sync failed", e));
      }
      if (
        !isBuiltinTheme(next) &&
        displayShownRef.current &&
        (hasToken() || isBundledBuiltinTheme(next))
      ) {
        void loadDisplayBundle(next);
      }
    },
    [cacheTheme, writePaint, loadDisplayBundle]
  );

  const setWide = useCallback(
    (next: boolean) => {
      cacheWide(next);
      if (hasToken()) {
        trackWrite(() => api.patchServerSettings({ displayWide: next }))
          .then(adoptServerSettings) // shared snapshot (T-8115)
          .catch((e) => console.warn("setWide: server sync failed", e));
      }
    },
    [cacheWide]
  );

  const setSeasonalTheme = useCallback(
    (next: boolean) => {
      cacheSeasonalTheme(next);
      if (hasToken()) {
        trackWrite(() => api.patchServerSettings({ displaySeasonalTheme: next }))
          .then(adoptServerSettings)
          .catch((e) => console.warn("setSeasonalTheme: server sync failed", e));
      }
    },
    [cacheSeasonalTheme]
  );

  const setThemeSchedule = useCallback(
    (next: ThemeScheduleEntry[]) =>
      trackWrite(async () => {
        const s = await api.patchServerSettings({ displayThemeSchedule: next });
        adoptServerSettings(s);
        const bundles = await loadScheduleBundles(s.displayThemeSchedule);
        cacheThemeSchedule(s.displayThemeSchedule);
        adoptScheduleBundles(s.displayThemeSchedule, bundles);
        return s.displayThemeSchedule;
      }),
    [trackWrite, loadScheduleBundles, cacheThemeSchedule, adoptScheduleBundles]
  );

  const restoreDefaultAppearance = useCallback(() => trackWrite(async () => {
    const s = await api.patchServerSettings({
      displayTheme: DEFAULT_DISPLAY_THEME,
      displayThemeSchedule: [...DEFAULT_THEME_SCHEDULE],
      displaySeasonalTheme: true,
    });
    adoptServerSettings(s);
    const bundles = await loadScheduleBundles(s.displayThemeSchedule);
    cacheSeasonalTheme(s.displaySeasonalTheme);
    cacheThemeSchedule(s.displayThemeSchedule);
    adoptScheduleBundles(s.displayThemeSchedule, bundles);
    cacheTheme(s.displayTheme);
    // The schedule is on screen, not display_theme: its bundle is fetched when
    // it is shown (the display-bundle effect), not now.
    setActiveThemeBundle(bundles[s.displayTheme] ?? null);
    return s.displayThemeSchedule;
  }), [trackWrite, loadScheduleBundles, cacheSeasonalTheme, cacheThemeSchedule, adoptScheduleBundles, cacheTheme]);

  // [T-83ef] The whole-set write is GONE. It re-sent every theme, with every
  // embedded image, to change one colour — that is the cost this ticket exists
  // to remove, and there is no per-theme door it could keep using.

  /** Fetch ONE theme in full. Editing and exporting are the only things that
   * need a bundle, and they are always about one theme. */
  const loadTheme = useCallback((id: string) => fetchThemeBundle(id), []);

  const saveTheme = useCallback(
    async (bundle: ThemeBundle) => {
      await trackWrite(() => api.putTheme(bundle));
      // The list carries the NAME, so a rename has to land here too. A replace
      // keeps its position (the server does not move an edited theme to the
      // bottom); a create appends, which is the position the server gives it.
      setThemeList((prev) =>
        prev.some((x) => x.id === bundle.id)
          ? prev.map((x) =>
              x.id === bundle.id ? { id: bundle.id, name: bundle.name } : x
            )
          : [...prev, { id: bundle.id, name: bundle.name }]
      );
      // Editing the theme you are LOOKING AT must repaint it; editing another
      // one must not touch the picture.
      if (themeRef.current === bundle.id) {
        setActiveThemeBundle(bundle);
        writePaint(bundle);
      }
      if (scheduledCustomThemes(themeScheduleRef.current).includes(bundle.id)) {
        adoptScheduleBundles(themeScheduleRef.current, {
          ...scheduleBundlesRef.current,
          [bundle.id]: bundle,
        });
      }
    },
    [trackWrite, writePaint, adoptScheduleBundles]
  );

  const removeTheme = useCallback(
    (id: string) => trackWrite(async () => {
      const result = await api.deleteTheme(id);
      setThemeList((prev) => prev.filter((x) => x.id !== id));
      // ⚠️ The server stores "" for the reset and the cockpit shows the default built-in (office).
      // Those are not the same string, and the difference is pre-existing: ""
      // means "never set", which tells ANOTHER device to keep its own cached
      // choice — and that cache may still name the theme just deleted. Not
      // silently papered over here; switching to the built-in is what this
      // device must do either way, and the cross-device half is a separate
      // question about what "" should mean.
      if (result.displayThemeReset || themeRef.current === id) {
        cacheTheme("office");
        setActiveThemeBundle(null);
        writePaint(null);
      }
      if (result.displayThemeSchedule) {
        const kept = { ...scheduleBundlesRef.current };
        delete kept[id];
        cacheThemeSchedule(result.displayThemeSchedule);
        adoptScheduleBundles(result.displayThemeSchedule, kept);
      }
    }),
    [trackWrite, cacheTheme, writePaint, cacheThemeSchedule, adoptScheduleBundles]
  );

  // Login and `settings`-delta reconcile (server = cross-device truth;
  // `fresh` bypasses the shared settings cache): pull /api/settings and adopt
  // a real, valid display pref that the server has stored, writing it back to
  // the localStorage cache so the NEXT pre-auth first paint is already correct.
  // "" (never set) or an unreachable/failing load keeps the cache showing — a
  // failed load must never masquerade as "reset to default". Applying an equal
  // value is a React state no-op, so the common (cache == server) case does not
  // repaint — no flash.
  const reconcileFromServer = useCallback((fresh = false) => {
    const epoch = writeEpochRef.current;
    // Both faces at once: settings still owns WHICH theme is active, the theme
    // resource now owns what themes there are.
    Promise.all([fresh ? refreshServerSettings() : loadServerSettings(), api.listThemes()])
      .then(([s, list]) => {
        if (readCrossedWrite(epoch)) return;
        // [T-83ef] The set arrives as ONE LINE EACH; the bundles are fetched
        // per theme. Only ONE bundle is ever needed to paint — the active one —
        // so this is two small requests instead of one that carried every image
        // the owner had ever saved.
        setThemeList(list);
        // Adopt a stored active theme only when it is actually selectable given
        // that set (a built-in, or an id present in it). "" (never set) or a
        // dangling id keeps the local cache — a stale server value must not
        // override a live local choice.
        const ids = new Set(list.map((b) => b.id));
        if (s.displayTheme !== "" && isValidDisplayTheme(s.displayTheme, ids)) {
          cacheTheme(s.displayTheme);
        }
        if (s.displayLanguage === "zh" || s.displayLanguage === "en") {
          cacheLanguage(s.displayLanguage);
        }
        // Layout width (T-756f) is adopted UNCONDITIONALLY — unlike theme and
        // language it has no "" third state to mean "keep the cache", so the
        // server's bool is simply the truth. That is what lets the owner turn
        // wide OFF on one device and have the others follow.
        cacheWide(s.displayWide);
        cacheSeasonalTheme(s.displaySeasonalTheme);
        const schedule = s.displayThemeSchedule;
        cacheThemeSchedule(schedule);
        const held = scheduledCustomThemes(schedule).filter((id) => scheduleBundlesRef.current[id]);
        adoptScheduleBundles(
          schedule,
          Object.fromEntries(held.map((id) => [id, scheduleBundlesRef.current[id]]))
        );
        // [T-1500] never leave a picture the server no longer recognises.
        // The active id AFTER this reconcile: the server's when selectable,
        // else the local one that survived above.
        const active =
          s.displayTheme !== "" && isValidDisplayTheme(s.displayTheme, ids)
            ? s.displayTheme
            : themeRef.current;
        // A built-in, or an id the server does not know: there is no bundle to
        // hold and no picture to cache. Clearing the paint record is the T-1500
        // rule ("never leave a picture the server no longer recognises") and it
        // now has to be spelled out, because there is no set to fail a lookup in.
        // No ref fix-up needed here: whichever branch above settled `active`
        // either called cacheTheme (which moves the ref with the decision) or
        // kept themeRef.current itself.
        if (isBuiltinTheme(active) || (!ids.has(active) && !isBundledBuiltinTheme(active))) {
          setActiveThemeBundle(null);
          writePaint(null);
          setThemesLoaded(true);
          return;
        }
        const now = new Date();
        const shownNow =
          !(s.displaySeasonalTheme && activeSeasonalWindow(now, SEASONAL_WINDOWS)) &&
          !activeScheduleEntry(now, schedule);
        if (!shownNow) {
          // Fetched once it is on screen (the display-bundle effect).
          if (activeThemeBundleRef.current?.id !== active) setActiveThemeBundle(null);
          setThemesLoaded(true);
          return;
        }
        // A delta re-read keeps the bundle in hand: no delta announces an edit
        // to a theme's content, and this tab's own echo must not re-download it.
        if (fresh && activeThemeBundleRef.current?.id === active) {
          setThemesLoaded(true);
          return;
        }
        return loadDisplayBundle(active).then(() => setThemesLoaded(true));
      })
      .catch((e) => console.warn("i18n reconcile: load failed", e));
  }, [
    readCrossedWrite,
    cacheTheme,
    cacheLanguage,
    cacheWide,
    cacheSeasonalTheme,
    cacheThemeSchedule,
    adoptScheduleBundles,
    writePaint,
    loadDisplayBundle,
  ]);
  rereadRef.current = () => reconcileFromServer(true);

  const refreshThemeList = useCallback(() => {
    if (!hasToken()) return;
    const epoch = writeEpochRef.current;
    api
      .listThemes()
      .then((list) => {
        if (readCrossedWrite(epoch)) return;
        setThemeList(list);
      })
      .catch((e) => console.warn("theme list: reload failed", e));
  }, [readCrossedWrite]);

  useEffect(() => {
    // Reconcile now if a token already exists (a returning session / reload
    // lands straight on the app wall), and again the instant one is minted (a
    // fresh login fires oc-auth-login from setToken). /api/settings is
    // owner-gated, so this is the earliest the server value is reachable. A
    // `settings` delta (another tab or device saved, or the resync after a
    // connection gap) reads it again. The subscription is taken at the same two
    // moments because without a token it is a no-op.
    let unsubscribe = () => {};
    const follow = () => {
      reconcileFromServer();
      unsubscribe();
      unsubscribe = api.subscribeEvents((topic) => {
        if (topic === "settings" && hasToken()) reconcileFromServer(true);
      });
    };
    if (hasToken()) follow();
    window.addEventListener(AUTH_LOGIN_EVENT, follow);
    return () => {
      window.removeEventListener(AUTH_LOGIN_EVENT, follow);
      unsubscribe();
    };
  }, [reconcileFromServer]);

  const resetPreferences = useCallback(() => {
    // The honest M1 "logout": drop back to the local defaults so the next
    // owner's first paint is not tinted by this session. Local-only — logout
    // must NOT rewrite the server pref (that would clear the owner's stored
    // choice for every other device).
    cacheLanguage("zh");
    cacheTheme("office");
    writeStored(LS_THEME_PAINT, null);
    cacheWide(false);
    cacheSeasonalTheme(true);
    cacheThemeSchedule([]);
    adoptScheduleBundles([], {});
    // The themes are server-backed — clear only the LOCAL mirror so the next
    // owner's paint is not tinted; the server copy is untouched (re-adopted at
    // that owner's reconcile).
    setThemeList([]);
    setActiveThemeBundle(null);
  }, [cacheLanguage, cacheTheme, cacheWide, cacheSeasonalTheme, cacheThemeSchedule, adoptScheduleBundles]);

  const value = useMemo<I18nContextValue>(
    () => ({
      locale,
      t,
      msg,
      language,
      setLanguage,
      theme,
      setTheme,
      wide,
      setWide,
      seasonalTheme,
      setSeasonalTheme,
      themeSchedule,
      setThemeSchedule,
      restoreDefaultAppearance,
      scheduledTheme,
      seasonalThemeName,
      shownTheme,
      shownThemeBundle: shownBundle,
      activeAvatars,
      activeLogo,
      activeNavIcons,
      themeList,
      activeThemeBundle,
      loadTheme,
      saveTheme,
      removeTheme,
      refreshThemeList,
      resetPreferences,
    }),
    [
      locale,
      t,
      msg,
      language,
      setLanguage,
      theme,
      setTheme,
      wide,
      setWide,
      seasonalTheme,
      setSeasonalTheme,
      themeSchedule,
      setThemeSchedule,
      restoreDefaultAppearance,
      scheduledTheme,
      seasonalThemeName,
      shownTheme,
      shownBundle,
      activeAvatars,
      activeLogo,
      activeNavIcons,
      themeList,
      activeThemeBundle,
      loadTheme,
      saveTheme,
      removeTheme,
      refreshThemeList,
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
