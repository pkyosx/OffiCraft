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
} from "../hooks/sharedServerSettings";
import {
  isValidDisplayTheme,
  type ThemeBundle,
  readThemeIcon,
  type ThemeIcon,
  type AvatarKind,
  type NavIconKey,
} from "../lib/themeBundle";
import {
  LS_THEME,
  LS_THEME_PAINT,
  paintRecordFor,
  applyThemeToRoot,
  readValidatedPaint,
} from "../lib/themePaint";
import { notifyThemeAvatarsChanged } from "../lib/themeAvatarsChanged";
import { applyWording } from "./wording";
import { makeMessages, type Messages } from "./compose";

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
  /** The PARAMETERISED messages (T-081b), composed from `t`'s overridable
   * static fragments — `m.taskTerminateConfirmBody(title)` rather than a
   * template leaf a wording overlay could never reach. Rebuilt with `t`, so an
   * active theme's re-worded fragment shows up here too. */
  msg: Messages;
  language: Language;
  setLanguage: (next: Language) => void;
  /** Active theme: the built-in name ("office") or a custom bundle id. */
  theme: string;
  setTheme: (next: string) => void;
  /** Whether the cockpit uses the WIDE layout (T-756f): the centred ~1040px
   * content column is lifted, the side gutters stay. false = the narrow
   * centred column (the default). */
  wide: boolean;
  setWide: (next: boolean) => void;
  /** The active custom theme's per-role avatar images (T-16a1 P5; T-ea81), or
   * undefined when the active theme carries none (the built-in office, or a
   * custom theme with no avatars overlay). The Avatar component reads this to
   * render a member/outsource/owner/assistant avatar image, falling back to the
   * built-in glyph when absent. */
  activeAvatars?: Partial<Record<AvatarKind, string>>;
  activeAvatarPools?: Partial<Record<"member" | "outsource", ThemeIcon[]>>;
  /** The active custom theme's studio logo image (T-ea81), or undefined when the
   * active theme carries none — the top bar then renders its built-in mark. */
  activeLogo?: string;
  /** The active custom theme's per-nav-tab icon images (T-ea81), or undefined
   * when the active theme carries none — each tab then keeps its built-in icon. */
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
   * display_theme and says so; this switches the cockpit back to the built-in
   * so the owner is never looking at a theme that no longer exists. */
  removeTheme: (id: string) => Promise<void>;
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
  const [themeList, setThemeList] = useState<ThemeListItem[]>([]);
  const [activeThemeBundle, setActiveThemeBundle] = useState<ThemeBundle | null>(
    null
  );
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

  const t = useMemo(() => {
    const base = DICTS[locale];
    const overlay = bundleForTheme?.wording?.[language];
    return applyWording(base, overlay);
  }, [locale, language, bundleForTheme]);

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
    () => bundleForTheme?.avatars,
    [bundleForTheme]
  );
  // A stored or imported bundle may still carry the legacy bare-string pool,
  // so it is lifted to the canonical {id, image} shape ONCE here. Every
  // consumer below — the Avatar, the chooser — then reads one shape only, and
  // an item with no id simply never matches a selection.
  const activeAvatarPools = useMemo(() => {
    // Same ONE authority as every other image on this theme (T-83ef): the
    // fetched bundle, and only while it is the ACTIVE theme's. Reading a
    // different source here is exactly how a theme switch rendered the next
    // theme's colours over the previous theme's images for one round trip.
    const pools = bundleForTheme?.avatarPools;
    if (!pools) return pools;
    const out: Partial<Record<"member" | "outsource", ThemeIcon[]>> = {};
    for (const kind of ["member", "outsource"] as const) {
      const items = pools[kind];
      if (!items) continue;
      out[kind] = items
        .map((item) => readThemeIcon(item))
        .filter((item): item is ThemeIcon => !!item);
    }
    return out;
  }, [bundleForTheme]);

  // The active custom theme's studio logo image + per-nav-tab icons (T-ea81).
  // Like avatars, these are IMAGES rendered as <img> (top-bar logo / nav-tab
  // icons), so they ride the context rather than the DOM. Absent → the built-in
  // logo mark / built-in nav icons (office never degrades).
  const activeLogo = useMemo(
    () => bundleForTheme?.logo,
    [bundleForTheme]
  );

  const activeNavIcons = useMemo(
    () => bundleForTheme?.navIcons,
    [bundleForTheme]
  );

  // The --color-* inline props applied for the current custom theme, remembered
  // so the NEXT apply can remove exactly this set before painting the next one.
  // [T-1500] seeded from the pre-React applier: ONE ledger, two writers.
  const appliedTokensRef = useRef<string[]>(window.__ocPaintTokens ?? []);

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
    root.dataset.theme = "office";
    if (bundleForTheme) {
      appliedTokensRef.current = applyThemeToRoot(root, bundleForTheme);
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
  }, [theme, bundleForTheme, themesLoaded]);

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
    writeStored(LS_THEME_PAINT, b ? JSON.stringify(paintRecordFor(b)) : null);
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
          .then(adoptServerSettings) // shared snapshot (T-8115)
          .catch((e) => console.warn("setLanguage: server sync failed", e));
      }
    },
    [cacheLanguage]
  );

  // [T-83ef] Switching theme now has to FETCH the chosen bundle: the provider no
  // longer holds every bundle, only the active one. The id is cached first and
  // synced first, so the choice is durable even if the fetch loses a race; the
  // paint cache is written only once the bundle is actually in hand, because a
  // paint record written from nothing would clear the cached picture and hand
  // the next pre-auth load a flash.
  const setTheme = useCallback(
    (next: string) => {
      cacheTheme(next);
      // The roster is holding faces resolved for the theme we are LEAVING; every
      // card has to be re-read against the new one or it silently shows that
      // pool's first image instead of this member's own choice there.
      //
      // ANNOUNCED HERE, before the server round-trip, because the switch has
      // ALREADY happened for this client the moment the cache moves — the paint
      // below does not wait either, and the pre-auth path never reaches the
      // server at all. Announcing from a .then() would leave the roster stale
      // in exactly the cases where the request is slow or absent.
      notifyThemeAvatarsChanged();
      if (next === "office") {
        setActiveThemeBundle(null);
        writePaint(null);
      }
      if (hasToken()) {
        api
          .patchServerSettings({ displayTheme: next })
          .then(adoptServerSettings) // shared snapshot (T-8115)
          .catch((e) => console.warn("setTheme: server sync failed", e));
        if (next !== "office") {
          api
            .getTheme(next)
            .then((b) => {
              // Ignore a fetch that lost a race to a later switch — otherwise a
              // slow response could repaint a theme the owner already left.
              if (themeRef.current !== next) return;
              setActiveThemeBundle(b);
              writePaint(b);
            })
            .catch((e) => console.warn("setTheme: bundle load failed", e));
        }
      }
    },
    [cacheTheme, writePaint]
  );

  const setWide = useCallback(
    (next: boolean) => {
      cacheWide(next);
      if (hasToken()) {
        api
          .patchServerSettings({ displayWide: next })
          .then(adoptServerSettings) // shared snapshot (T-8115)
          .catch((e) => console.warn("setWide: server sync failed", e));
      }
    },
    [cacheWide]
  );

  // [T-83ef] The whole-set write is GONE. It re-sent every theme, with every
  // embedded image, to change one colour — that is the cost this ticket exists
  // to remove, and there is no per-theme door it could keep using.

  /** Fetch ONE theme in full. Editing and exporting are the only things that
   * need a bundle, and they are always about one theme. */
  const loadTheme = useCallback((id: string) => api.getTheme(id), []);

  const saveTheme = useCallback(
    async (bundle: ThemeBundle) => {
      await api.putTheme(bundle);
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
      // A pool edit can remove the very image a member had chosen; the server
      // prunes that association on this write, so the roster's copy is stale
      // even though no member row was touched.
      notifyThemeAvatarsChanged();
    },
    [writePaint]
  );

  const removeTheme = useCallback(
    async (id: string) => {
      const result = await api.deleteTheme(id);
      setThemeList((prev) => prev.filter((x) => x.id !== id));
      // ⚠️ The server stores "" for the reset and the cockpit shows the built-in.
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
      // Deleting a theme drops every selection recorded against it, and may
      // reset the active theme as well — both change what the roster resolves.
      notifyThemeAvatarsChanged();
    },
    [cacheTheme, writePaint]
  );

  // Login reconcile (server = cross-device truth): pull /api/settings and adopt
  // a real, valid display pref that the server has stored, writing it back to
  // the localStorage cache so the NEXT pre-auth first paint is already correct.
  // "" (never set) or an unreachable/failing load keeps the cache showing — a
  // failed load must never masquerade as "reset to default". Applying an equal
  // value is a React state no-op, so the common (cache == server) case does not
  // repaint — no flash.
  const reconcileFromServer = useCallback(() => {
    // Both faces at once: settings still owns WHICH theme is active, the theme
    // resource now owns what themes there are.
    Promise.all([loadServerSettings(), api.listThemes()])
      .then(([s, list]) => {
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
        if (active === "office" || !ids.has(active)) {
          setActiveThemeBundle(null);
          writePaint(null);
          setThemesLoaded(true);
          return;
        }
        return api.getTheme(active).then((b) => {
          // Same guard `setTheme` carries, for the same reason and it was
          // missing here: reconcile fires on login/reload, so its fetch is in
          // flight while the owner is free to pick a different theme. Without
          // this, the slower of the two answers wins — and the damage outlives
          // the moment, because writePaint would cache a picture of a theme the
          // owner is not on and the NEXT cold load would paint it before React
          // mounts. Two paths fetch one bundle; one of them having the guard is
          // not the same as the guard existing.
          if (themeRef.current !== active) {
            setThemesLoaded(true);
            return;
          }
          setActiveThemeBundle(b);
          writePaint(b);
          setThemesLoaded(true);
        });
      })
      .catch((e) => console.warn("i18n reconcile: load failed", e));
  }, [cacheTheme, cacheLanguage, cacheWide, writePaint]);

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
    writeStored(LS_THEME_PAINT, null);
    cacheWide(false);
    // The themes are server-backed — clear only the LOCAL mirror so the next
    // owner's paint is not tinted; the server copy is untouched (re-adopted at
    // that owner's reconcile).
    setThemeList([]);
    setActiveThemeBundle(null);
  }, [cacheLanguage, cacheTheme, cacheWide]);

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
      activeAvatars,
      activeAvatarPools,
      activeLogo,
      activeNavIcons,
      themeList,
      activeThemeBundle,
      loadTheme,
      saveTheme,
      removeTheme,
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
      activeAvatars,
      activeAvatarPools,
      activeLogo,
      activeNavIcons,
      themeList,
      activeThemeBundle,
      loadTheme,
      saveTheme,
      removeTheme,
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

export function useActiveAvatarPools():
  | Partial<Record<"member" | "outsource", ThemeIcon[]>>
  | undefined {
  return useContext(I18nContext)?.activeAvatarPools;
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
