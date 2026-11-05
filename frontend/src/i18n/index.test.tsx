// Dual-layer theme/language (T-0b41-p2): the localStorage pre-auth CACHE drives
// the first paint (zero-flash, server unreachable pre-auth), and the server is
// the cross-device TRUTH reconciled in at login — applied to state + written
// back to the cache. These jsdom tests cover the state/localStorage/<html
// data-theme> reconcile wiring; the geometry-free bits a real browser is not
// needed for (the visual pre-auth guard lives in the Playwright CT suite).

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, it, expect, beforeEach, afterEach, vi, type Mock } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { I18nProvider, useI18n } from "./index";
import { zh } from "./locales/zh";
import { readDictMessage } from "./wording";
import { mockApi, __resetMock, __setMockNoAppearance } from "../api/mock";
import { api } from "../api";
import { setToken, TOKEN_KEY } from "../api/auth";
import type { LoadableSeasonalWindow } from "../lib/seasonalTheme";
import type { ThemeBundle } from "../lib/themeBundleCore";
import { paintRecordFor, readValidatedSchedulePaint } from "../lib/themePaint";
import { scheduleFrom } from "../lib/seasonalSchedule";

const seasonalWindows = vi.hoisted(() => [] as LoadableSeasonalWindow[]);
vi.mock("../lib/seasonalTheme", () => ({ SEASONAL_WINDOWS: seasonalWindows }));
const builtinLoads = vi.hoisted(() => [] as string[]);
/** A built-in id whose load waits for `release` — to land it late on purpose. */
const builtinHold = vi.hoisted(() => ({ id: "", until: Promise.resolve() }));
vi.mock("../lib/bundledBuiltinThemes", async (importOriginal) => {
  const real = await importOriginal<typeof import("../lib/bundledBuiltinThemes")>();
  return {
    ...real,
    loadBundledBuiltinTheme: async (id: string) => {
      builtinLoads.push(id);
      if (id === builtinHold.id) await builtinHold.until;
      return real.loadBundledBuiltinTheme(id);
    },
  };
});

function Probe() {
  const { theme, language } = useI18n();
  return <div data-testid="probe" data-theme={theme} data-lang={language} />;
}

let ctx = null as unknown as ReturnType<typeof useI18n>;
function Capture() {
  ctx = useI18n();
  return null;
}

const MIDNIGHT = {
  id: "midnight",
  name: "Midnight",
  colors: { "--color-accent": "#010203", "--color-bg": "#040506" },
};
const SUNRISE = {
  id: "sunrise",
  name: "Sunrise",
  colors: { "--color-accent": "#ffaa00" },
};

describe("I18nProvider dual-layer theme/language", () => {
  beforeEach(() => {
    __resetMock();
    __setMockNoAppearance();
    localStorage.clear();
    delete document.documentElement.dataset.theme;
  });

  it("first paint uses the localStorage cache when pre-auth (no server call)", () => {
    // A cached custom-theme id. Pre-reconcile the bundle isn't loaded yet,
    // so the apply effect paints the neutral office base — but the theme STATE
    // (the cache) is what drives the value.
    localStorage.setItem("oc.theme", "midnight");
    localStorage.setItem("oc.language", "en");
    // No token → /api/settings is unreachable; the cache is the only truth.
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    expect(screen.getByTestId("probe").dataset.theme).toBe("midnight");
    expect(screen.getByTestId("probe").dataset.lang).toBe("en");
    // A dangling custom id (bundle not yet reconciled) paints the office base.
    expect(document.documentElement.dataset.theme).toBe("office");
  });

  it("first paint applies a cached built-in theme as <html data-theme> pre-auth", () => {
    localStorage.setItem("oc.theme", "office-light");
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    expect(screen.getByTestId("probe").dataset.theme).toBe("office-light");
    expect(document.documentElement.dataset.theme).toBe("office-light");
  });

  it("adopts a server-stored built-in office-light at login and caches it", async () => {
    await mockApi.patchServerSettings({ displayTheme: "office-light" });
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    await waitFor(() =>
      expect(document.documentElement.dataset.theme).toBe("office-light")
    );
    expect(localStorage.getItem("oc.theme")).toBe("office-light");
  });

  it("adopts the server value on mount when a token already exists, writing it back to the cache", async () => {
    // Cache is empty (defaults office/zh); the server holds the owner's choice —
    // a custom theme plus display.theme pointing at it (so the id is selectable).
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.patchServerSettings({
      displayTheme: "midnight",
      displayLanguage: "en",
    });
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    await waitFor(() =>
      expect(screen.getByTestId("probe").dataset.theme).toBe("midnight")
    );
    expect(screen.getByTestId("probe").dataset.lang).toBe("en");
    // Written back so the NEXT pre-auth first paint is already correct.
    expect(localStorage.getItem("oc.theme")).toBe("midnight");
    expect(localStorage.getItem("oc.language")).toBe("en");
  });

  it("reconciles when a login mints a token mid-session (oc-auth-login)", async () => {
    // Pre-auth first paint: default office/zh, server not yet reachable.
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.patchServerSettings({
      displayTheme: "midnight",
      displayLanguage: "en",
    });
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    expect(screen.getByTestId("probe").dataset.theme).toBe("office");
    // A login mints the token, which fires oc-auth-login from setToken → reconcile.
    await act(async () => {
      setToken("fresh-owner-token");
    });
    await waitFor(() =>
      expect(screen.getByTestId("probe").dataset.theme).toBe("midnight")
    );
    expect(screen.getByTestId("probe").dataset.lang).toBe("en");
  });

  it("keeps the cache when the server pref is unset (\"\")", async () => {
    localStorage.setItem("oc.theme", "midnight");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    // Server default is "" for both — an unset server value must NOT clobber the
    // cache back to a default.
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    // Give the reconcile a chance to (wrongly) run, then assert it left the cache.
    await Promise.resolve();
    await waitFor(() =>
      expect(screen.getByTestId("probe").dataset.theme).toBe("midnight")
    );
    expect(localStorage.getItem("oc.theme")).toBe("midnight");
  });
});

describe("I18nProvider layout width (T-756f)", () => {
  const root = document.documentElement;

  beforeEach(() => {
    __resetMock();
    __setMockNoAppearance();
    localStorage.clear();
    delete root.dataset.layout;
  });

  it("defaults to narrow and leaves the DOM exactly as it was", () => {
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    expect(ctx.wide).toBe(false);
    // The narrow default must be the ABSENCE of the attribute — that is what
    // guarantees the shipped chrome is untouched for anyone who never opted in.
    expect(root.hasAttribute("data-layout")).toBe(false);
  });

  it("applies <html data-layout=\"wide\"> when switched on, and removes it again", () => {
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    act(() => ctx.setWide(true));
    expect(root.dataset.layout).toBe("wide");
    act(() => ctx.setWide(false));
    expect(root.hasAttribute("data-layout")).toBe(false);
  });

  it("caches the choice to localStorage for the pre-auth first paint", () => {
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    act(() => ctx.setWide(true));
    expect(localStorage.getItem("oc.wide")).toBe("true");
  });

  it("first paint uses the localStorage cache when pre-auth", () => {
    localStorage.setItem("oc.wide", "true");
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    expect(ctx.wide).toBe(true);
    expect(root.dataset.layout).toBe("wide");
  });

  it("adopts the server value at login, writing it back to the cache", async () => {
    await mockApi.patchServerSettings({ displayWide: true });
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    await waitFor(() => expect(ctx.wide).toBe(true));
    expect(root.dataset.layout).toBe("wide");
    expect(localStorage.getItem("oc.wide")).toBe("true");
  });

  it("lets the server turn wide back OFF across devices (no \"\" third state)", async () => {
    // This device cached wide; the owner turned it off elsewhere, so the server
    // says false. Unlike theme/language there is no unset value meaning "keep
    // the cache" — the server bool is simply adopted.
    localStorage.setItem("oc.wide", "true");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    await waitFor(() => expect(ctx.wide).toBe(false));
    expect(localStorage.getItem("oc.wide")).toBe("false");
    expect(root.hasAttribute("data-layout")).toBe(false);
  });

  it("resetPreferences drops back to narrow (logout must not tint the next owner)", () => {
    localStorage.setItem("oc.wide", "true");
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    expect(ctx.wide).toBe(true);
    act(() => ctx.resetPreferences());
    expect(ctx.wide).toBe(false);
    expect(root.hasAttribute("data-layout")).toBe(false);
  });
});


// [T-83ef] The provider no longer holds every bundle, so the door these claims
// happen through changed: a theme is SAVED to its own resource, and switching to
// it FETCHES that one bundle (`api.getTheme`). Every claim below is the same one
// the whole-set `commitCustomThemes` used to assert — only the door moved.
// Fetching is gated on hasToken(), which is why these mount with a token.
const PNG =
  "data:image/png;base64," +
  btoa(
    String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01)
  );

/** Mount the provider and WAIT FOR THE LOGIN RECONCILE to land — it is what
 * fills `themeList`, and it also settles the active bundle (null here: the
 * server's display_theme is unset). Racing it would let a late reconcile clear
 * the bundle a switch had just fetched, i.e. the test would be measuring the
 * race rather than the switch. `saved` is how many themes were seeded. */
async function mountCapture(saved: number) {
  await act(async () => {
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
  });
  await waitFor(() => expect(ctx.themeList).toHaveLength(saved));
}

/** Switch the cockpit to a SAVED theme and wait for its one-bundle fetch to
 * land — the fetch is what the whole-set provider used to do without a round
 * trip, so it is the step the assertions now have to wait on. */
async function activate(id: string) {
  // SYNC act on purpose: in the browser the switch comes from a click, so React
  // has already committed the new active id by the time the bundle fetch
  // resolves. An async act would let the fetch land first and the provider's
  // "ignore a fetch that lost a race to a later switch" guard would (correctly)
  // drop it — the test would then be measuring an ordering the product does not
  // have.
  act(() => {
    ctx.setTheme(id);
  });
  await waitFor(() => expect(ctx.activeThemeBundle?.id).toBe(id));
}

/** Switch to a theme that resolves to NO bundle (the built-in, or an id the
 * server does not know) — there is nothing to wait for. */
async function activateBundleless(id: string) {
  act(() => {
    ctx.setTheme(id);
  });
  await waitFor(() => expect(ctx.theme).toBe(id));
}

describe("I18nProvider custom theme apply", () => {
  const root = document.documentElement;

  beforeEach(() => {
    __resetMock();
    __setMockNoAppearance();
    localStorage.clear();
    root.removeAttribute("style");
    delete root.dataset.theme;
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
  });

  it("applies a custom bundle as inline vars over the neutral office base", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mountCapture(1);
    await activate("midnight");

    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-accent")).toBe("#010203");
    expect(root.style.getPropertyValue("--color-bg")).toBe("#040506");
  });

  for (const builtin of ["office", "office-light"]) {
    it(`clears the previous custom vars and the paint cache when switching to the built-in ${builtin}`, async () => {
      await mockApi.putTheme(MIDNIGHT);
      await mountCapture(1);
      await activate("midnight");
      const getTheme = vi.spyOn(api, "getTheme");
      await activateBundleless(builtin);

      expect(root.dataset.theme).toBe(builtin);
      expect(root.style.getPropertyValue("--color-accent")).toBe("");
      expect(root.style.getPropertyValue("--color-bg")).toBe("");
      expect(localStorage.getItem("oc.themePaint")).toBeNull();
      expect(getTheme).not.toHaveBeenCalled();
      getTheme.mockRestore();
    });
  }

  it("drops vars not carried by the next custom theme when switching between them", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.putTheme(SUNRISE);
    await mountCapture(2);
    await activate("midnight");
    await activate("sunrise");

    expect(root.style.getPropertyValue("--color-accent")).toBe("#ffaa00");
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
  });

  it("falls back to the office base for a dangling custom id", async () => {
    await mountCapture(0);
    // Nothing is saved under this id — the fetch 404s and the picker keeps the
    // office base rather than painting something invented.
    await activateBundleless("ghost");

    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-accent")).toBe("");
  });

  it("applies the canvas background image alongside the canvas colour (T-081b)", async () => {
    await mockApi.putTheme({ ...MIDNIGHT, backgrounds: { canvas: PNG } });
    await mountCapture(1);
    await activate("midnight");

    expect(root.style.getPropertyValue("--canvas-bg-image")).toBe(`url("${PNG}")`);
    expect(root.style.getPropertyValue("--color-bg")).toBe("#040506");
    // no mode named ⇒ the tiling values theme.css already defaults to
    expect(root.style.getPropertyValue("--canvas-bg-repeat")).toBe("repeat");
    expect(root.style.getPropertyValue("--canvas-bg-position")).toBe("0 0");
    expect(root.style.getPropertyValue("--canvas-bg-size")).toBe("auto");
    expect(root.style.getPropertyValue("--canvas-bg-attachment")).toBe("scroll");

    // "sides" lays the same url twice — once against each viewport edge, ONE
    // copy each (a repeat would put a second tree down a long page). The editor's
    // save path is saveTheme: same id, new content, repaint because it is active.
    await act(async () => {
      await ctx.saveTheme({
        ...MIDNIGHT,
        backgrounds: { canvas: PNG },
        backgroundModes: { canvas: "sides" },
      });
    });
    expect(root.style.getPropertyValue("--canvas-bg-image")).toBe(
      `url("${PNG}"), url("${PNG}")`
    );
    expect(root.style.getPropertyValue("--canvas-bg-repeat")).toBe(
      "no-repeat, no-repeat"
    );
    expect(root.style.getPropertyValue("--canvas-bg-position")).toBe(
      "left bottom, right bottom"
    );
    // pinned to the viewport, so a long page cannot scroll a second copy in
    expect(root.style.getPropertyValue("--canvas-bg-attachment")).toBe(
      "fixed, fixed"
    );

    // "cover" scales ONE copy to the whole viewport
    await act(async () => {
      await ctx.saveTheme({
        ...MIDNIGHT,
        backgrounds: { canvas: PNG },
        backgroundModes: { canvas: "cover" },
      });
    });
    expect(root.style.getPropertyValue("--canvas-bg-image")).toBe(`url("${PNG}")`);
    expect(root.style.getPropertyValue("--canvas-bg-repeat")).toBe("no-repeat");
    expect(root.style.getPropertyValue("--canvas-bg-size")).toBe("cover");
    expect(root.style.getPropertyValue("--canvas-bg-attachment")).toBe("fixed");

    // and all five are cleared again when the next theme carries no image
    await activateBundleless("office");
    expect(root.style.getPropertyValue("--canvas-bg-image")).toBe("");
    expect(root.style.getPropertyValue("--canvas-bg-repeat")).toBe("");
    expect(root.style.getPropertyValue("--canvas-bg-position")).toBe("");
    expect(root.style.getPropertyValue("--canvas-bg-size")).toBe("");
    expect(root.style.getPropertyValue("--canvas-bg-attachment")).toBe("");
  });

  it("caches a custom active id to localStorage", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mountCapture(1);
    await activate("midnight");
    expect(localStorage.getItem("oc.theme")).toBe("midnight");
  });

  it("keeps a custom theme visual-only — copy follows the language toggle", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mountCapture(1);
    await act(async () => ctx.setLanguage("en"));
    await activate("midnight");
    expect(ctx.locale).toBe("en");
  });

  it("keeps the locale following the language toggle regardless of theme (theme↔locale decoupled)", async () => {
    await mountCapture(0);
    await act(async () => ctx.setLanguage("zh"));
    await activateBundleless("office");
    expect(ctx.locale).toBe("zh");
    await act(async () => ctx.setLanguage("en"));
    expect(ctx.locale).toBe("en");
  });
});

describe("I18nProvider custom theme wording overlay", () => {
  const WORDED = {
    id: "worded",
    name: "Worded",
    colors: { "--color-accent": "#123456" },
    wording: {
      zh: { "nav.tasks": "任務榜" },
      en: { "nav.tasks": "Quest Board" },
    },
  };

  beforeEach(() => {
    __resetMock();
    __setMockNoAppearance();
    localStorage.clear();
    document.documentElement.removeAttribute("style");
    delete document.documentElement.dataset.theme;
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
  });

  it("overrides an overridden code in the active language, leaving others intact", async () => {
    await mockApi.putTheme(WORDED);
    await mountCapture(1);
    await activate("worded");
    // zh is the default language → the zh override applies.
    expect(ctx.t.nav.tasks).toBe("任務榜");
    // A non-overridden code keeps its base value (fallback = original language).
    expect(ctx.t.nav.monitor).toBe(zh.nav.monitor);
  });

  it("follows the language toggle for the overlay language", async () => {
    await mockApi.putTheme(WORDED);
    await mountCapture(1);
    await act(async () => ctx.setLanguage("en"));
    await activate("worded");
    expect(ctx.t.nav.tasks).toBe("Quest Board");
  });

  it("restores the base wording when switching away from the theme", async () => {
    await mockApi.putTheme(WORDED);
    await mountCapture(1);
    await activate("worded");
    expect(ctx.t.nav.tasks).toBe("任務榜");
    await activateBundleless("office");
    expect(ctx.t.nav.tasks).toBe(zh.nav.tasks);
  });

  it("keeps applying an already-stored pack whose overlay holds an unrecognised code", async () => {
    // Owner requirement 2026-07-27: 已匯入的主題包還是要能夠運作,只是不認得的會失效.
    // A pack imported BEFORE T-081b de-whitelisted the theme-identity keys still
    // sits in the store with those codes in it — the recognised overrides must
    // all still land, and the unrecognised one must simply do nothing.
    await mockApi.putTheme({
      ...WORDED,
      id: "legacy",
      wording: {
        zh: {
          "nav.tasks": "任務榜",
          "nav.replies": "傳訊台",
          "profile.themeOffice": "精靈村",
          "typo.not.a.key": "x",
        },
      },
    });
    await mountCapture(1);
    await activate("legacy");
    expect(ctx.t.nav.tasks).toBe("任務榜");
    expect(ctx.t.nav.replies).toBe("傳訊台");
    // The unrecognised codes are inert: no leaf is invented for them, and the
    // built-in theme keeps its own name.
    expect(readDictMessage(ctx.t, "profile.themeOffice")).toBeNull();
    expect(readDictMessage(ctx.t, "typo.not.a.key")).toBeNull();
    expect(ctx.t.themeIdentity.office).toBe(zh.themeIdentity.office);
  });

  it("leaves a custom theme without wording on the base dict", async () => {
    await mockApi.putTheme(SUNRISE);
    await mountCapture(1);
    await activate("sunrise");
    expect(ctx.t.nav.tasks).toBe(zh.nav.tasks);
  });
});

// [T-83ef] The window between "the owner picked a theme" and "that theme's
// bundle arrived". Before the split there was no window — the bundle was found
// synchronously in the array settings already carried — so nothing had to say
// what the cockpit shows in between. Now one round trip does, and these pin the
// answer: NOTHING of the previous theme survives into it.
describe("I18nProvider · the switch window", () => {
  const AURORA = {
    id: "aurora",
    name: "Aurora",
    colors: { "--color-accent": "#00ffcc" },
    avatars: { staff: PNG },
    logo: PNG,
    navIcons: { office: PNG },
    wording: { zh: { "nav.tasks": "極光榜" } },
  };
  const PLAIN = { id: "plain", name: "Plain", colors: { "--color-accent": "#111111" } };

  beforeEach(() => {
    __resetMock();
    __setMockNoAppearance();
    localStorage.clear();
    document.documentElement.removeAttribute("style");
    delete document.documentElement.dataset.theme;
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
  });

  it("shows NO part of the old theme while the next one's bundle is in flight", async () => {
    // The colour apply always refused a bundle whose id was not the active
    // theme; wording, avatars, logo and nav icons each read the fetched bundle
    // directly. So a switch used to render the next theme's (absent) colours
    // over the PREVIOUS theme's images and words — a blend of two themes that
    // never existed, on every switch, for as long as the fetch took.
    await mockApi.putTheme(AURORA);
    await mockApi.putTheme(PLAIN);
    await mountCapture(2);
    await activate("aurora");
    // Everything the bundle carries is live…
    expect(ctx.activeAvatars?.staff).toBe(PNG);
    expect(ctx.activeLogo).toBe(PNG);
    expect(ctx.activeNavIcons?.office).toBe(PNG);
    expect(ctx.t.nav.tasks).toBe("極光榜");

    // …now switch, and hold the next bundle in flight.
    let release!: (b: typeof PLAIN) => void;
    const pending = new Promise<typeof PLAIN>((res) => {
      release = res;
    });
    const spy = vi.spyOn(api, "getTheme").mockReturnValue(pending as never);
    act(() => {
      ctx.setTheme("plain");
    });

    // The active id has already moved…
    expect(ctx.theme).toBe("plain");
    // …so NOTHING of aurora may still be on screen. Each of these is a separate
    // consumer that used to read the raw bundle.
    expect(ctx.activeAvatars).toBeUndefined();
    expect(ctx.activeLogo).toBeUndefined();
    expect(ctx.activeNavIcons).toBeUndefined();
    expect(ctx.t.nav.tasks).toBe(zh.nav.tasks);

    release(PLAIN);
    await waitFor(() => expect(ctx.activeThemeBundle?.id).toBe("plain"));
    spy.mockRestore();
  });

  it("drops a reconcile bundle the owner has already switched away from, and does not cache its picture", async () => {
    // reconcile fires on login/reload, so its fetch is in flight exactly when
    // the owner is free to pick something else. `setTheme` carried this guard;
    // reconcile did not — and one of two paths having it is not the guard
    // existing. The cache is the half that outlives the moment: a picture of
    // the wrong theme is what the NEXT cold load paints before React mounts.
    await mockApi.putTheme(AURORA);
    await mockApi.patchServerSettings({ displayTheme: "aurora" });

    let release!: (b: typeof AURORA) => void;
    const spy = vi.spyOn(api, "getTheme").mockReturnValue(
      new Promise<typeof AURORA>((res) => {
        release = res;
      }) as never
    );
    await act(async () => {
      render(
        <I18nProvider>
          <Capture />
        </I18nProvider>
      );
    });
    await waitFor(() => expect(ctx.theme).toBe("aurora"));

    // The owner leaves for the built-in before aurora's bundle lands.
    act(() => {
      ctx.setTheme("office");
    });
    expect(ctx.theme).toBe("office");

    release(AURORA);
    await act(async () => {
      await Promise.resolve();
    });

    // The late answer is discarded on both faces.
    expect(ctx.activeThemeBundle).toBeNull();
    expect(localStorage.getItem("oc.themePaint")).toBeNull();
    spy.mockRestore();
  });

  it("adopts a bundle whose write settles BEFORE React re-renders with the new id", async () => {
    // 🔴 The guards above ask "is this still the active theme?" AFTER an await,
    // and the ref they ask used to be assigned during RENDER. So a promise that
    // settles in the same tick as the switch read the PREVIOUS theme and the
    // code threw away the bundle it had just asked for.
    //
    // This exact sequence — select, then save, in one tick — is what
    // visual-guards/stories/AvatarKindStory.tsx routes around in prose: "the
    // save's promise settles before React has re-rendered with the new id, the
    // provider sees a different active theme, and the avatars never arrive."
    // The diagnosis was right and the hole stayed open; the story split the two
    // steps across renders to avoid it. This pins the fix so the story's
    // workaround is a choice rather than a requirement.
    //
    // Production's three callers fire from discrete DOM events, where React
    // flushes synchronously — so the gap was closed by React's SCHEDULING, not
    // by this file. That is the kind of guarantee that stops holding quietly.
    await mountCapture(0);

    await act(async () => {
      ctx.setTheme("aurora");
      await ctx.saveTheme(AURORA);
    });

    expect(ctx.theme).toBe("aurora");
    expect(ctx.activeThemeBundle?.id).toBe("aurora");
    // …and everything the bundle carries actually arrived.
    expect(ctx.activeAvatars?.staff).toBe(PNG);
    expect(ctx.t.nav.tasks).toBe("極光榜");
  });

  it("refuses a stored display_theme that is not in the list, and keeps the local choice", async () => {
    // 🔴 This is the cockpit half of a race the SERVER cannot close cheaply:
    // PATCH /api/settings checks "does this theme exist?" outside the lock it
    // then writes display_theme under, so a DELETE landing in between leaves
    // settings naming a theme with no row. api_themes.go says in as many words
    // that the visible outcome is absorbed here — and that claim had no test:
    // every case fed display_theme "" (never set), so the branch that refuses a
    // NON-EMPTY unknown id had never once been taken.
    //
    // The window did not exist before T-83ef: the vocabulary and the selection
    // arrived in one request under one lock.
    await mockApi.putTheme(PLAIN);
    await mockApi.putTheme(AURORA);
    await mockApi.patchServerSettings({ displayTheme: "aurora" });
    localStorage.setItem("oc.theme", "plain");
    // …and now aurora is gone from the set while settings still names it.
    const spy = vi
      .spyOn(api, "listThemes")
      .mockResolvedValue([{ id: "plain", name: "Plain" }]);

    await mountCapture(1);

    // The dangling server value is NOT adopted — the live local choice stands…
    expect(ctx.theme).toBe("plain");
    // …and what is painted is that local choice, not a half-applied aurora.
    await waitFor(() => expect(ctx.activeThemeBundle?.id).toBe("plain"));
    expect(ctx.activeAvatars).toBeUndefined();
    spy.mockRestore();
  });
});

const THEMES_DIR = resolve(__dirname, "../../../themes");
const themeFile = (name: string) =>
  JSON.parse(readFileSync(resolve(THEMES_DIR, name), "utf8")) as ThemeBundle;
const TG_DAY = "thanksgiving-2026-day";
const TG_DUSK = "thanksgiving-2026-dusk";
const TG_NIGHT = "thanksgiving-2026-night";
const SAFARI_QUOTA = 2_500_000;
type Rec = { bundle: { id: string; backgrounds?: unknown } };
const records = (key: string) => JSON.parse(localStorage.getItem(key) ?? "[]") as Rec[];
const withImage = (key: string) =>
  Object.fromEntries(records(key).map((r) => [r.bundle.id, r.bundle.backgrounds !== undefined]));
const themeRecord = () => JSON.parse(localStorage.getItem("oc.themePaint") ?? "null") as Rec | null;
const storedChars = () => {
  let n = 0;
  for (let i = 0; i < localStorage.length; i++) {
    const k = localStorage.key(i)!;
    n += k.length + (localStorage.getItem(k) ?? "").length;
  }
  return n;
};
const tgLoad = vi.fn(async (file?: string) => themeFile(file!));
/** The real Thanksgiving window, optionally moved to `span`. */
function openThanksgiving(span?: { start: Date; end: Date }) {
  tgLoad.mockClear();
  const raw = JSON.parse(
    readFileSync(resolve(THEMES_DIR, "seasonal-schedule.json"), "utf8")
  ) as Parameters<typeof scheduleFrom>[0];
  const [w] = scheduleFrom(raw.filter((e) => e.id === "thanksgiving-2026"));
  seasonalWindows.push({ ...w, ...span, load: tgLoad });
}
function storeWholeRecords(key: string, ids: string[]) {
  localStorage.setItem(
    key,
    JSON.stringify(ids.map((id) => paintRecordFor(themeFile(`${id}.theme.json`))))
  );
}
/** Safari's origin quota: a write that would take the origin past it throws.
 * Returns a writer that bypasses it, for another app's data on the origin. */
function capStorageAtSafariQuota() {
  const setItem = localStorage.setItem.bind(localStorage);
  vi.spyOn(localStorage, "setItem").mockImplementation((k: string, v: string) => {
    const old = localStorage.getItem(k);
    const after = storedChars() - (old === null ? 0 : k.length + old.length) + k.length + v.length;
    if (after > SAFARI_QUOTA) throw new DOMException("quota", "QuotaExceededError");
    setItem(k, v);
  });
  return setItem;
}

describe("I18nProvider seasonal theme", () => {
  const root = document.documentElement;
  const HARVEST_START = new Date(2030, 9, 24, 0, 0);
  const HARVEST_END = new Date(2030, 10, 1, 0, 0);
  const HARVEST = {
    id: "harvest",
    name: "Harvest",
    colors: { "--color-bg": "#15101f", "--color-accent": "#ff8c2b" },
    avatars: { staff: PNG },
    logo: PNG,
    navIcons: { office: PNG },
    wording: { zh: { "nav.office": "鬼屋辦公室" }, en: { "nav.office": "Haunted Office" } },
  };
  let load: Mock<(typeof seasonalWindows)[number]["load"]>;

  function at(date: Date) {
    vi.useFakeTimers({
      shouldAdvanceTime: true,
      toFake: ["Date", "setTimeout", "clearTimeout"],
    });
    vi.setSystemTime(date);
  }

  async function mount() {
    await act(async () => {
      render(
        <I18nProvider>
          <Capture />
        </I18nProvider>
      );
    });
  }

  beforeEach(() => {
    __resetMock();
    __setMockNoAppearance();
    localStorage.clear();
    root.removeAttribute("style");
    delete root.dataset.theme;
    localStorage.setItem("oc.theme", "office-light");
    load = vi.fn(() => Promise.resolve(HARVEST));
    seasonalWindows.push({ id: "harvest", start: HARVEST_START, end: HARVEST_END, load });
  });
  afterEach(() => {
    seasonalWindows.length = 0;
    vi.useRealTimers();
  });

  it("before the window opens, shows the selected theme and never loads the seasonal theme file", async () => {
    at(new Date(2030, 9, 23, 12, 0));
    await mount();

    expect(load).not.toHaveBeenCalled();
    expect(ctx.theme).toBe("office-light");
    expect(root.dataset.theme).toBe("office-light");
    expect(ctx.t.nav.office).toBe(zh.nav.office);
    expect(ctx.activeLogo).toBeUndefined();
    expect(localStorage.getItem("oc.seasonalPaint")).toBeNull();
  });

  it("inside the window, shows the seasonal theme whole while the selected theme stays what it was", async () => {
    at(new Date(2030, 9, 28, 12, 0));
    await mount();

    await waitFor(() => expect(ctx.t.nav.office).toBe("鬼屋辦公室"));
    expect(load).toHaveBeenCalledTimes(1);
    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-bg")).toBe("#15101f");
    expect(root.style.getPropertyValue("--color-accent")).toBe("#ff8c2b");
    expect(ctx.activeAvatars).toEqual({ staff: PNG });
    expect(ctx.activeLogo).toBe(PNG);
    expect(ctx.activeNavIcons).toEqual({ office: PNG });
    expect(ctx.theme).toBe("office-light");
    expect(localStorage.getItem("oc.theme")).toBe("office-light");
    expect(JSON.parse(localStorage.getItem("oc.seasonalPaint") ?? "null")).toEqual([
      {
        v: 1,
        bundle: {
          id: "harvest",
          name: "Harvest",
          colors: { "--color-bg": "#15101f", "--color-accent": "#ff8c2b" },
        },
      },
    ]);
  });

  it("inside the window, never lists the seasonal theme among the owner's themes", async () => {
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    await mockApi.putTheme(SUNRISE);
    at(new Date(2030, 9, 28, 12, 0));
    await mount();

    await waitFor(() => expect(ctx.t.nav.office).toBe("鬼屋辦公室"));
    await waitFor(() => expect(ctx.themeList).toEqual([{ id: "sunrise", name: "Sunrise" }]));
  });

  it("switches to the seasonal theme by itself when the window opens on an open page", async () => {
    at(new Date(HARVEST_START.getTime() - 1000));
    await mount();
    expect(ctx.t.nav.office).toBe(zh.nav.office);
    expect(load).not.toHaveBeenCalled();

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    await waitFor(() => expect(ctx.t.nav.office).toBe("鬼屋辦公室"));
    expect(load).toHaveBeenCalledTimes(1);
    expect(root.style.getPropertyValue("--color-bg")).toBe("#15101f");
  });

  it("switches back to the selected theme by itself when the window ends, and drops the seasonal picture", async () => {
    at(new Date(HARVEST_END.getTime() - 1000));
    await mount();
    await waitFor(() => expect(ctx.t.nav.office).toBe("鬼屋辦公室"));

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    expect(ctx.t.nav.office).toBe(zh.nav.office);
    expect(root.dataset.theme).toBe("office-light");
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
    expect(ctx.activeLogo).toBeUndefined();
    expect(localStorage.getItem("oc.seasonalPaint")).toBeNull();
  });

  it("inside the window, turning it off shows the selected theme and saves the choice; turning it on brings it back", async () => {
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    at(new Date(2030, 9, 28, 12, 0));
    await mount();
    await waitFor(() => expect(ctx.t.nav.office).toBe("鬼屋辦公室"));
    const patch = vi.spyOn(api, "patchServerSettings");

    act(() => ctx.setSeasonalTheme(false));

    expect(ctx.seasonalTheme).toBe(false);
    expect(ctx.t.nav.office).toBe(zh.nav.office);
    expect(root.dataset.theme).toBe("office-light");
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
    expect(localStorage.getItem("oc.seasonalTheme")).toBe("false");
    expect(patch).toHaveBeenCalledWith({ displaySeasonalTheme: false });
    await waitFor(async () =>
      expect((await mockApi.getServerSettings()).displaySeasonalTheme).toBe(false)
    );

    act(() => ctx.setSeasonalTheme(true));

    await waitFor(() => expect(ctx.t.nav.office).toBe("鬼屋辦公室"));
    expect(root.style.getPropertyValue("--color-bg")).toBe("#15101f");
    expect(localStorage.getItem("oc.seasonalTheme")).toBe("true");
    expect(patch).toHaveBeenLastCalledWith({ displaySeasonalTheme: true });
    patch.mockRestore();
  });

  it("inside the window with the choice cached off, never loads the seasonal theme file", async () => {
    localStorage.setItem("oc.seasonalTheme", "false");
    at(new Date(2030, 9, 28, 12, 0));
    await mount();

    expect(ctx.seasonalTheme).toBe(false);
    expect(load).not.toHaveBeenCalled();
    expect(root.dataset.theme).toBe("office-light");
  });

  it("adopts the server's off at login over a cached on, writing it back to the cache", async () => {
    await mockApi.patchServerSettings({ displaySeasonalTheme: false });
    localStorage.setItem("oc.seasonalTheme", "true");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    at(new Date(2030, 9, 23, 12, 0));
    await mount();

    await waitFor(() => expect(ctx.seasonalTheme).toBe(false));
    expect(localStorage.getItem("oc.seasonalTheme")).toBe("false");
  });

  it("resetPreferences turns the seasonal theme back on (the shipped default)", async () => {
    localStorage.setItem("oc.seasonalTheme", "false");
    at(new Date(2030, 9, 23, 12, 0));
    await mount();
    expect(ctx.seasonalTheme).toBe(false);

    act(() => ctx.resetPreferences());

    expect(ctx.seasonalTheme).toBe(true);
    expect(localStorage.getItem("oc.seasonalTheme")).toBe("true");
  });

  it("inside the window, a seasonal theme file that fails to load shows the selected theme, and later clock readings never pass through the seasonal path or retry", async () => {
    load.mockReset();
    load.mockRejectedValue(new Error("chunk fetch failed"));
    at(new Date(2030, 9, 28, 12, 0));
    await mount();

    await waitFor(() => expect(root.dataset.theme).toBe("office-light"));
    expect(ctx.t.nav.office).toBe(zh.nav.office);
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
    expect(load).toHaveBeenCalledTimes(1);

    const painted: string[] = [];
    const observer = new MutationObserver(() =>
      painted.push(`${root.dataset.theme}|${root.style.getPropertyValue("--color-bg")}`)
    );
    observer.observe(root, { attributes: true, attributeFilter: ["data-theme", "style"] });
    const visible = vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
    for (const [minute, target, event] of [
      [5, window, "focus"],
      [6, document, "visibilitychange"],
      [7, window, "focus"],
    ] as const) {
      vi.setSystemTime(new Date(2030, 9, 28, 12, minute));
      await act(async () => {
        target.dispatchEvent(new Event(event));
      });
      painted.push(`${root.dataset.theme}|${root.style.getPropertyValue("--color-bg")}`);
    }
    observer.disconnect();
    visible.mockRestore();

    expect(painted.filter((p) => p !== "office-light|")).toEqual([]);
    expect(painted.slice(-3)).toEqual(["office-light|", "office-light|", "office-light|"]);
    expect(ctx.t.nav.office).toBe(zh.nav.office);
    expect(load).toHaveBeenCalledTimes(1);
  });

  for (const [event, target] of [
    ["visibilitychange", document],
    ["focus", window],
  ] as const) {
    it(`after sleeping past the window's start, ${event} switches to the seasonal theme without the boundary timer firing`, async () => {
      at(new Date(HARVEST_START.getTime() - 60 * 60 * 1000));
      await mount();
      expect(ctx.t.nav.office).toBe(zh.nav.office);

      vi.setSystemTime(new Date(HARVEST_START.getTime() + 60 * 1000));
      await act(async () => {
        target.dispatchEvent(new Event(event));
      });

      await waitFor(() => expect(ctx.t.nav.office).toBe("鬼屋辦公室"));
      expect(load).toHaveBeenCalledTimes(1);
    });
  }

  it("while two windows overlap shows the one listed first, and switches by itself to the other when the first ends", async () => {
    const FROST = {
      id: "frost",
      name: "Frost",
      colors: { "--color-bg": "#0d1a26" },
      wording: { zh: { "nav.office": "冰雪辦公室" } },
    };
    const loadFrost = vi.fn(() => Promise.resolve(FROST));
    seasonalWindows.length = 0;
    seasonalWindows.push(
      { id: "harvest", start: HARVEST_START, end: HARVEST_END, load },
      { id: "frost", start: new Date(2030, 9, 30, 0, 0), end: new Date(2030, 10, 28, 0, 0), load: loadFrost }
    );
    at(new Date(HARVEST_END.getTime() - 1000));
    await mount();

    await waitFor(() => expect(ctx.t.nav.office).toBe("鬼屋辦公室"));
    expect(loadFrost).not.toHaveBeenCalled();

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    await waitFor(() => expect(ctx.t.nav.office).toBe("冰雪辦公室"));
    expect(root.style.getPropertyValue("--color-bg")).toBe("#0d1a26");
    expect(loadFrost).toHaveBeenCalledTimes(1);
  });

  it("arms the boundary timer at most 2^31-1 ms ahead when the window opens later than that, and still switches when it opens", async () => {
    at(new Date(HARVEST_START.getTime() - 30 * 24 * 60 * 60 * 1000));
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    await mount();

    expect(setTimeoutSpy.mock.calls.map((c) => c[1])).toContain(2147483647);
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(load).not.toHaveBeenCalled();
    expect(ctx.t.nav.office).toBe(zh.nav.office);

    await act(async () => {
      vi.advanceTimersByTime(2147483647);
    });
    expect(load).not.toHaveBeenCalled();
    await act(async () => {
      vi.advanceTimersByTime(30 * 24 * 60 * 60 * 1000 - 2147483647);
    });

    await waitFor(() => expect(ctx.t.nav.office).toBe("鬼屋辦公室"));
    expect(load).toHaveBeenCalledTimes(1);
    setTimeoutSpy.mockRestore();
  });

  it("inside the window, while the seasonal theme is still loading, shows none of the selected custom theme and does not fetch it", async () => {
    const DUSK = {
      id: "dusk",
      name: "Dusk",
      colors: { "--color-bg": "#0a0b0c" },
      wording: { zh: { "nav.office": "黃昏辦公室" } },
    };
    await mockApi.putTheme(DUSK);
    await mockApi.patchServerSettings({ displayTheme: "dusk" });
    localStorage.setItem("oc.theme", "dusk");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    load.mockReset();
    load.mockReturnValue(new Promise(() => {}));
    const getTheme = vi.spyOn(api, "getTheme");
    at(new Date(2030, 9, 28, 12, 0));
    await mount();

    await waitFor(() => expect(ctx.themeList).toHaveLength(1));
    expect(getTheme).not.toHaveBeenCalled();
    expect(ctx.activeThemeBundle).toBeNull();
    expect(ctx.theme).toBe("dusk");
    expect(ctx.t.nav.office).toBe(zh.nav.office);
    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
  });
});

describe("I18nProvider seasonal theme with day parts", () => {
  const root = document.documentElement;
  const NIGHT = {
    id: "harvest-night",
    name: "豐收之夜",
    colors: { "--color-bg": "#101014", "--color-hot": "#ff5a36" },
    backgrounds: { canvas: PNG },
  };
  const DAY = { id: "harvest-day", name: "豐收白天", colors: { "--color-bg": "#f5f0e6" } };
  let load: Mock<(file?: string) => Promise<ThemeBundle>>;

  function at(date: Date) {
    vi.useFakeTimers({
      shouldAdvanceTime: true,
      toFake: ["Date", "setTimeout", "clearTimeout"],
    });
    vi.setSystemTime(date);
  }

  async function mount() {
    await act(async () => {
      render(
        <I18nProvider>
          <Capture />
        </I18nProvider>
      );
    });
  }

  beforeEach(() => {
    __resetMock();
    __setMockNoAppearance();
    localStorage.clear();
    root.removeAttribute("style");
    delete root.dataset.theme;
    localStorage.setItem("oc.theme", "office-light");
    localStorage.setItem(
      "oc.themeSchedule",
      JSON.stringify([
        { start: "00:00", theme: "office" },
        { start: "12:00", theme: "office-light" },
      ])
    );
    load = vi.fn((file?: string) =>
      Promise.resolve(file === "harvest-day.theme.json" ? DAY : NIGHT)
    );
    seasonalWindows.push({
      id: "harvest",
      start: new Date(2030, 9, 24, 0, 0),
      end: new Date(2030, 10, 1, 0, 0),
      schedule: [
        { start: "06:00", theme: "harvest-day.theme.json" },
        { start: "18:00", theme: "harvest-night.theme.json" },
      ],
      load,
    });
  });
  afterEach(() => {
    seasonalWindows.length = 0;
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("shows the day part for the local time over the user's schedule, loading only its file", async () => {
    at(new Date(2030, 9, 25, 21, 0));
    await mount();

    await waitFor(() => expect(ctx.seasonalThemeName).toBe("豐收之夜"));
    expect(load.mock.calls).toEqual([["harvest-night.theme.json"]]);
    expect(ctx.scheduledTheme).toBeNull();
    expect(ctx.shownTheme).toBe("harvest-night");
    expect(root.style.getPropertyValue("--color-bg")).toBe("#101014");
  });

  it("switches from the dark day part to the light one at its start with no reload: light color-scheme, nothing of the dark one left inline", async () => {
    at(new Date(2030, 9, 25, 5, 59, 59));
    await mount();
    await waitFor(() => expect(root.style.getPropertyValue("--color-bg")).toBe("#101014"));
    expect(root.style.getPropertyValue("color-scheme")).toBe("dark");
    expect(root.style.getPropertyValue("--canvas-bg-image")).not.toBe("");

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    await waitFor(() => expect(root.style.getPropertyValue("--color-bg")).toBe("#f5f0e6"));
    expect(root.style.getPropertyValue("color-scheme")).toBe("light");
    expect(root.style.getPropertyValue("--color-hot")).toBe("");
    expect(root.style.getPropertyValue("--canvas-bg-image")).toBe("");
    expect(ctx.seasonalThemeName).toBe("豐收白天");
    expect(load.mock.calls).toEqual([["harvest-night.theme.json"], ["harvest-day.theme.json"]]);
    expect(
      (JSON.parse(localStorage.getItem("oc.seasonalPaint") ?? "[]") as { bundle: { id: string } }[]).map(
        (r) => r.bundle.id
      )
    ).toEqual(["harvest-day", "harvest-night"]);
  });

  it("prefetches the next day part ten minutes before its start and switches to it with no further load, keeping the picture only in the shown part's record", async () => {
    const LIT_DAY = { ...DAY, backgrounds: { canvas: PNG } };
    load.mockImplementation((file?: string) =>
      Promise.resolve(file === "harvest-day.theme.json" ? LIT_DAY : NIGHT)
    );
    at(new Date(2030, 9, 25, 5, 40));
    await mount();
    await waitFor(() => expect(ctx.seasonalThemeName).toBe("豐收之夜"));
    expect(load.mock.calls).toEqual([["harvest-night.theme.json"]]);

    await act(async () => {
      vi.advanceTimersByTime(10 * 60 * 1000);
    });
    await waitFor(() =>
      expect(load.mock.calls).toEqual([["harvest-night.theme.json"], ["harvest-day.theme.json"]])
    );
    expect(ctx.seasonalThemeName).toBe("豐收之夜");
    await waitFor(() => expect(withImage("oc.seasonalPaint")).toEqual({ "harvest-day": false, "harvest-night": true }));

    await act(async () => {
      vi.advanceTimersByTime(10 * 60 * 1000);
    });
    expect(ctx.seasonalThemeName).toBe("豐收白天");
    expect(root.style.getPropertyValue("--color-bg")).toBe("#f5f0e6");
    expect(load).toHaveBeenCalledTimes(2);
    await waitFor(() => expect(withImage("oc.seasonalPaint")).toEqual({ "harvest-day": true, "harvest-night": false }));
  });

  it("when the next day part's prefetch fails, keeps showing this part until its start, then the user's schedule for the rest of the page, without retrying, and its records go back to the schedule's tiering", async () => {
    const DUSK = { id: "dusk", name: "Dusk", colors: { "--color-bg": "#0a0b0c" }, backgrounds: { canvas: PNG } };
    const SCHEDULE = [
      { start: "00:00", theme: "dusk" },
      { start: "12:00", theme: "office-light" },
    ];
    await mockApi.putTheme(DUSK);
    await mockApi.patchServerSettings({ displayTheme: "office-light", displayThemeSchedule: SCHEDULE });
    localStorage.setItem("oc.themeSchedule", JSON.stringify(SCHEDULE));
    localStorage.setItem("oc.schedulePaint", JSON.stringify([paintRecordFor(DUSK)]));
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    load.mockImplementation((file?: string) =>
      file === "harvest-day.theme.json" ? Promise.reject(new Error("chunk gone")) : Promise.resolve(NIGHT)
    );
    vi.spyOn(console, "warn").mockImplementation(() => {});
    at(new Date(2030, 9, 25, 5, 40));
    await mount();
    await waitFor(() => expect(ctx.seasonalThemeName).toBe("豐收之夜"));

    await act(async () => {
      vi.advanceTimersByTime(10 * 60 * 1000);
    });
    await waitFor(() => expect(load).toHaveBeenCalledTimes(2));
    expect(ctx.seasonalThemeName).toBe("豐收之夜");
    expect(root.style.getPropertyValue("--color-bg")).toBe("#101014");
    expect(withImage("oc.schedulePaint")).toEqual({ dusk: false });

    await act(async () => {
      vi.advanceTimersByTime(10 * 60 * 1000);
    });
    expect(ctx.seasonalThemeName).toBeNull();
    expect(ctx.scheduledTheme).toBe("dusk");
    await waitFor(() => expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c"));
    await waitFor(() => expect(withImage("oc.schedulePaint")).toEqual({ dusk: true }));
    expect(withImage("oc.seasonalPaint")).toEqual({ "harvest-night": false });

    await act(async () => {
      vi.advanceTimersByTime(60 * 60 * 1000);
      window.dispatchEvent(new Event("focus"));
    });
    expect(ctx.scheduledTheme).toBe("dusk");
    expect(load).toHaveBeenCalledTimes(2);
  });

  it("keeps records only for this window's day parts, dropping any other window's", async () => {
    localStorage.setItem(
      "oc.seasonalPaint",
      JSON.stringify([{ v: 1, bundle: { id: "frost-night", name: "Frost", colors: { "--color-bg": "#000010" } } }])
    );
    at(new Date(2030, 9, 25, 21, 0));
    await mount();

    await waitFor(() => expect(ctx.seasonalThemeName).toBe("豐收之夜"));
    expect(
      (JSON.parse(localStorage.getItem("oc.seasonalPaint") ?? "[]") as { bundle: { id: string } }[]).map(
        (r) => r.bundle.id
      )
    ).toEqual(["harvest-night"]);
  });

  it("prefetches the window's first day part ten minutes before the window opens, and opens on it with no further load", async () => {
    at(new Date(2030, 9, 23, 23, 55));
    await mount();

    await waitFor(() => expect(load.mock.calls).toEqual([["harvest-night.theme.json"]]));
    expect(ctx.seasonalThemeName).toBeNull();

    await act(async () => {
      vi.advanceTimersByTime(5 * 60 * 1000);
    });
    await waitFor(() => expect(ctx.seasonalThemeName).toBe("豐收之夜"));
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("prefetches the user's slot ten minutes before the window ends", async () => {
    await mockApi.putTheme({ id: "dusk", name: "Dusk", colors: { "--color-bg": "#0a0b0c" } });
    await mockApi.patchServerSettings({
      displayTheme: "office",
      displayThemeSchedule: [
        { start: "00:00", theme: "dusk" },
        { start: "12:00", theme: "office-light" },
      ],
    });
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    const getTheme = vi.spyOn(api, "getTheme");
    at(new Date(2030, 9, 31, 23, 55));
    await mount();

    await waitFor(() => expect(getTheme.mock.calls.map((c) => c[0])).toEqual(["dusk"]));
    expect(ctx.seasonalThemeName).toBe("豐收之夜");
    getTheme.mockRestore();
  });

  it("with the seasonal theme turned off, shows the user's own schedule and loads no day part", async () => {
    localStorage.setItem("oc.seasonalTheme", "false");
    at(new Date(2030, 9, 25, 9, 0));
    await mount();

    expect(ctx.scheduledTheme).toBe("office");
    expect(root.dataset.theme).toBe("office");
    expect(load).not.toHaveBeenCalled();
  });
});

describe("I18nProvider theme schedule", () => {
  const root = document.documentElement;
  const DUSK = {
    id: "dusk",
    name: "Dusk",
    colors: { "--color-bg": "#0a0b0c" },
    wording: { zh: { "nav.office": "黃昏辦公室" } },
  };
  const SCHEDULE = [
    { start: "07:00", theme: "office-light" },
    { start: "19:00", theme: "dusk" },
  ];

  function at(date: Date) {
    vi.useFakeTimers({
      shouldAdvanceTime: true,
      toFake: ["Date", "setTimeout", "clearTimeout"],
    });
    vi.setSystemTime(date);
  }

  async function mount() {
    await act(async () => {
      render(
        <I18nProvider>
          <Capture />
        </I18nProvider>
      );
    });
  }

  async function signedInWithSchedule() {
    await mockApi.putTheme(DUSK);
    await mockApi.patchServerSettings({ displayTheme: "office", displayThemeSchedule: SCHEDULE });
    localStorage.setItem("oc.theme", "office");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
  }

  beforeEach(() => {
    __resetMock();
    __setMockNoAppearance();
    localStorage.clear();
    root.removeAttribute("style");
    delete root.dataset.theme;
  });
  afterEach(() => {
    seasonalWindows.length = 0;
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("at login, adopts the server's schedule, shows its current entry, and caches the schedule and every scheduled custom theme's picture", async () => {
    await signedInWithSchedule();
    at(new Date(2030, 0, 15, 20, 0));
    await mount();

    await waitFor(() => expect(ctx.t.nav.office).toBe("黃昏辦公室"));
    expect(ctx.themeSchedule).toEqual(SCHEDULE);
    expect(ctx.scheduledTheme).toBe("dusk");
    expect(ctx.theme).toBe("office");
    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c");
    expect(JSON.parse(localStorage.getItem("oc.themeSchedule") ?? "null")).toEqual(SCHEDULE);
    expect(JSON.parse(localStorage.getItem("oc.schedulePaint") ?? "null")).toEqual([
      { v: 1, bundle: { id: "dusk", name: "Dusk", colors: { "--color-bg": "#0a0b0c" } } },
    ]);
    expect(localStorage.getItem("oc.theme")).toBe("office");
  });

  it("before the day's first start, shows the previous day's last entry", async () => {
    await signedInWithSchedule();
    at(new Date(2030, 0, 15, 6, 0));
    await mount();

    await waitFor(() => expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c"));
    expect(ctx.t.nav.office).toBe("黃昏辦公室");
  });

  it("switches by itself at the next start without fetching the theme again", async () => {
    await signedInWithSchedule();
    at(new Date(2030, 0, 15, 18, 59, 59));
    await mount();
    await waitFor(() => expect(JSON.parse(localStorage.getItem("oc.schedulePaint") ?? "[]")).toHaveLength(1));
    expect(root.dataset.theme).toBe("office-light");
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
    const getTheme = vi.spyOn(api, "getTheme");

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c");
    expect(ctx.t.nav.office).toBe("黃昏辦公室");
    expect(getTheme).not.toHaveBeenCalled();
  });

  for (const [event, target] of [
    ["visibilitychange", document],
    ["focus", window],
  ] as const) {
    it(`after sleeping past a start, ${event} switches to that entry without the boundary timer firing`, async () => {
      await signedInWithSchedule();
      at(new Date(2030, 0, 15, 12, 0));
      await mount();
      await waitFor(() => expect(ctx.themeSchedule).toEqual(SCHEDULE));
      expect(root.dataset.theme).toBe("office-light");

      vi.setSystemTime(new Date(2030, 0, 15, 21, 0));
      await act(async () => {
        target.dispatchEvent(new Event(event));
      });

      await waitFor(() => expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c"));
      expect(root.dataset.theme).toBe("office");
    });
  }

  it("while a scheduled theme's bundle is still in flight, paints its cached picture rather than a built-in", async () => {
    localStorage.setItem("oc.theme", "office");
    localStorage.setItem("oc.themeSchedule", JSON.stringify(SCHEDULE));
    localStorage.setItem(
      "oc.schedulePaint",
      JSON.stringify([{ v: 1, bundle: { id: "dusk", name: "Dusk", colors: { "--color-bg": "#0a0b0c" } } }])
    );
    at(new Date(2030, 0, 15, 20, 0));
    await mount();

    expect(ctx.themeSchedule).toEqual(SCHEDULE);
    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c");
  });

  it("inside a seasonal window with the seasonal theme on, shows the seasonal theme over the schedule and names it, and the schedule once it is turned off", async () => {
    const harvest = { id: "harvest", name: "Harvest", colors: { "--color-bg": "#15101f" } };
    seasonalWindows.push({
      id: "harvest",
      start: new Date(2030, 0, 1),
      end: new Date(2030, 1, 1),
      load: () => Promise.resolve(harvest),
    });
    await signedInWithSchedule();
    at(new Date(2030, 0, 15, 20, 0));
    await mount();
    await waitFor(() => expect(ctx.themeSchedule).toEqual(SCHEDULE));
    await waitFor(() => expect(root.style.getPropertyValue("--color-bg")).toBe("#15101f"));
    expect(ctx.seasonalThemeName).toBe("Harvest");
    expect(ctx.scheduledTheme).toBeNull();

    act(() => ctx.setSeasonalTheme(false));

    await waitFor(() => expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c"));
    expect(ctx.theme).toBe("office");
    expect(ctx.seasonalThemeName).toBeNull();
    expect(ctx.scheduledTheme).toBe("dusk");
  });

  it("with the schedule empty, shows the selected theme", async () => {
    localStorage.setItem("oc.theme", "office-light");
    at(new Date(2030, 0, 15, 20, 0));
    await mount();

    expect(ctx.themeSchedule).toEqual([]);
    expect(root.dataset.theme).toBe("office-light");
  });

  it("saving a schedule stores the server's sorted copy and shows it at once; turning it off shows the selected theme", async () => {
    await mockApi.putTheme(DUSK);
    localStorage.setItem("oc.theme", "office");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    at(new Date(2030, 0, 15, 20, 0));
    await mount();
    await waitFor(() => expect(ctx.themeList).toEqual([{ id: "dusk", name: "Dusk" }]));

    await act(async () => {
      await ctx.setThemeSchedule([SCHEDULE[1], SCHEDULE[0]]);
    });

    expect(ctx.themeSchedule).toEqual(SCHEDULE);
    expect((await mockApi.getServerSettings()).displayThemeSchedule).toEqual(SCHEDULE);
    expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c");
    expect(JSON.parse(localStorage.getItem("oc.themeSchedule") ?? "null")).toEqual(SCHEDULE);

    await act(async () => {
      await ctx.setThemeSchedule([]);
    });

    expect(ctx.themeSchedule).toEqual([]);
    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
    expect(localStorage.getItem("oc.themeSchedule")).toBeNull();
    expect(localStorage.getItem("oc.schedulePaint")).toBeNull();
  });

  describe("loading only the slot it needs", () => {
    const DAWN = { id: "dawn", name: "Dawn", colors: { "--color-bg": "#fafaf0" } };
    const TWO = [
      { start: "07:00", theme: "dawn" },
      { start: "19:00", theme: "dusk" },
    ];
    const recordIds = () =>
      (JSON.parse(localStorage.getItem("oc.schedulePaint") ?? "[]") as { bundle: { id: string } }[])
        .map((r) => r.bundle.id)
        .sort();

    async function signedInWithTwo() {
      await mockApi.putTheme(DUSK);
      await mockApi.putTheme(DAWN);
      await mockApi.patchServerSettings({ displayTheme: "office", displayThemeSchedule: TWO });
      localStorage.setItem("oc.theme", "office");
      localStorage.setItem(TOKEN_KEY, "live-owner-token");
    }

    it("opening in a slot fetches only that slot's theme, keeps the records of slots seen before and drops a removed slot's", async () => {
      await signedInWithTwo();
      localStorage.setItem(
        "oc.schedulePaint",
        JSON.stringify([
          { v: 1, bundle: { id: "dusk", name: "Dusk", colors: { "--color-bg": "#0a0b0c" } } },
          { v: 1, bundle: { id: "gone", name: "Gone", colors: { "--color-bg": "#123456" } } },
        ])
      );
      const getTheme = vi.spyOn(api, "getTheme");
      at(new Date(2030, 0, 15, 12, 0));
      await mount();

      await waitFor(() => expect(root.style.getPropertyValue("--color-bg")).toBe("#fafaf0"));
      expect(getTheme.mock.calls.map((c) => c[0])).toEqual(["dawn"]);
      await waitFor(() => expect(recordIds()).toEqual(["dawn", "dusk"]));
    });

    it("prefetches the next slot ten minutes before its start, and switches at the start with that bundle and no fetch", async () => {
      await signedInWithTwo();
      at(new Date(2030, 0, 15, 18, 0));
      await mount();
      await waitFor(() => expect(root.style.getPropertyValue("--color-bg")).toBe("#fafaf0"));
      const getTheme = vi.spyOn(api, "getTheme");

      await act(async () => {
        vi.advanceTimersByTime(49 * 60 * 1000);
      });
      expect(getTheme).not.toHaveBeenCalled();

      await act(async () => {
        vi.advanceTimersByTime(60 * 1000);
      });
      await waitFor(() => expect(recordIds()).toEqual(["dawn", "dusk"]));
      expect(getTheme.mock.calls.map((c) => c[0])).toEqual(["dusk"]);
      expect(root.style.getPropertyValue("--color-bg")).toBe("#fafaf0");

      await act(async () => {
        vi.advanceTimersByTime(10 * 60 * 1000);
      });
      expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c");
      expect(ctx.t.nav.office).toBe("黃昏辦公室");
      expect(getTheme).toHaveBeenCalledTimes(1);
    });

    it("opening within ten minutes of the next start prefetches it at once", async () => {
      await signedInWithTwo();
      const getTheme = vi.spyOn(api, "getTheme");
      at(new Date(2030, 0, 15, 18, 55));
      await mount();

      await waitFor(() => expect(getTheme.mock.calls.map((c) => c[0]).sort()).toEqual(["dawn", "dusk"]));
    });

    it("saving a schedule fetches the theme of the slot it lands in before switching, and not the others", async () => {
      await mockApi.putTheme(DUSK);
      await mockApi.putTheme(DAWN);
      localStorage.setItem("oc.theme", "office");
      localStorage.setItem(TOKEN_KEY, "live-owner-token");
      at(new Date(2030, 0, 15, 12, 0));
      await mount();
      await waitFor(() => expect(ctx.themeList).toHaveLength(2));
      const getTheme = vi.spyOn(api, "getTheme");

      await act(async () => {
        await ctx.setThemeSchedule(TWO);
      });

      expect(root.style.getPropertyValue("--color-bg")).toBe("#fafaf0");
      expect(getTheme.mock.calls.map((c) => c[0])).toEqual(["dawn"]);
    });
  });

  it("saving on a page left open since the morning shows the entry for now, not for when the page last read the clock", async () => {
    await mockApi.putTheme(DUSK);
    localStorage.setItem("oc.theme", "office");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    at(new Date(2030, 0, 15, 8, 0));
    await mount();
    await waitFor(() => expect(ctx.themeList).toEqual([{ id: "dusk", name: "Dusk" }]));
    vi.setSystemTime(new Date(2030, 0, 15, 20, 0));

    await act(async () => {
      await ctx.setThemeSchedule(SCHEDULE);
    });

    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c");
  });

  it("a scheduled theme whose fetch fails keeps its cached picture, and is shown from it", async () => {
    await signedInWithSchedule();
    const cached = { v: 1, bundle: { id: "dusk", name: "Dusk", colors: { "--color-bg": "#0a0b0c" } } };
    localStorage.setItem("oc.themeSchedule", JSON.stringify(SCHEDULE));
    localStorage.setItem("oc.schedulePaint", JSON.stringify([cached]));
    vi.spyOn(api, "getTheme").mockRejectedValue(new Error("offline"));
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    at(new Date(2030, 0, 15, 20, 0));
    await mount();

    await waitFor(() =>
      expect(warn).toHaveBeenCalledWith("theme schedule: bundle load failed", expect.any(Error))
    );
    await act(async () => {
      await Promise.resolve();
    });

    expect(JSON.parse(localStorage.getItem("oc.schedulePaint") ?? "null")).toEqual([cached]);
    expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c");
  });

  it("a login reconcile that lands after a save does not bring back the replaced schedule's themes", async () => {
    const DAWN = { id: "dawn", name: "Dawn", colors: { "--color-bg": "#fafaf0" } };
    await signedInWithSchedule();
    await mockApi.putTheme(DAWN);
    const realGetTheme = api.getTheme.bind(api);
    let releaseDusk: (() => void) | null = null;
    vi.spyOn(api, "getTheme").mockImplementation((id: string) =>
      id === "dusk"
        ? new Promise((resolve) => {
            releaseDusk = () => resolve(realGetTheme(id));
          })
        : realGetTheme(id)
    );
    at(new Date(2030, 0, 15, 20, 0));
    await mount();
    await waitFor(() => expect(releaseDusk).not.toBeNull());
    const next = [
      { start: "07:00", theme: "office-light" },
      { start: "19:00", theme: "dawn" },
    ];

    await act(async () => {
      await ctx.setThemeSchedule(next);
    });
    expect(root.style.getPropertyValue("--color-bg")).toBe("#fafaf0");
    await act(async () => {
      releaseDusk?.();
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(ctx.themeSchedule).toEqual(next);
    expect(root.style.getPropertyValue("--color-bg")).toBe("#fafaf0");
    expect(JSON.parse(localStorage.getItem("oc.schedulePaint") ?? "null")).toEqual([
      { v: 1, bundle: DAWN },
    ]);
  });

  it("when storage refuses the scheduled themes' pictures, the older pictures are removed rather than left for the next page load", async () => {
    await signedInWithSchedule();
    localStorage.setItem("oc.themeSchedule", JSON.stringify(SCHEDULE));
    localStorage.setItem(
      "oc.schedulePaint",
      JSON.stringify([{ v: 1, bundle: { id: "dusk", name: "Dusk", colors: { "--color-bg": "#111111" } } }])
    );
    const setItem = localStorage.setItem.bind(localStorage);
    vi.spyOn(localStorage, "setItem").mockImplementation((key: string, value: string) => {
      if (key === "oc.schedulePaint") throw new DOMException("full", "QuotaExceededError");
      setItem(key, value);
    });
    at(new Date(2030, 0, 15, 20, 0));
    await mount();

    await waitFor(() => expect(localStorage.getItem("oc.schedulePaint")).toBeNull());
    expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c");
  });

  it("a refused save rejects and leaves the schedule as it was", async () => {
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    at(new Date(2030, 0, 15, 20, 0));
    await mount();

    await expect(
      ctx.setThemeSchedule([
        { start: "07:00", theme: "office-light" },
        { start: "19:00", theme: "gone" },
      ])
    ).rejects.toMatchObject({ status: 422 });
    expect(ctx.themeSchedule).toEqual([]);
    expect(localStorage.getItem("oc.themeSchedule")).toBeNull();
  });

  it("deleting a scheduled theme adopts the schedule the server left, dropping its picture", async () => {
    await signedInWithSchedule();
    at(new Date(2030, 0, 15, 20, 0));
    await mount();
    await waitFor(() => expect(root.style.getPropertyValue("--color-bg")).toBe("#0a0b0c"));

    await act(async () => {
      await ctx.removeTheme("dusk");
    });

    expect(ctx.themeSchedule).toEqual([]);
    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
    expect(localStorage.getItem("oc.themeSchedule")).toBeNull();
    expect(localStorage.getItem("oc.schedulePaint")).toBeNull();
  });

  it("resetPreferences clears the schedule and its pictures", async () => {
    localStorage.setItem("oc.themeSchedule", JSON.stringify(SCHEDULE));
    localStorage.setItem(
      "oc.schedulePaint",
      JSON.stringify([{ v: 1, bundle: { id: "dusk", name: "Dusk", colors: { "--color-bg": "#0a0b0c" } } }])
    );
    at(new Date(2030, 0, 15, 20, 0));
    await mount();

    act(() => ctx.resetPreferences());

    expect(ctx.themeSchedule).toEqual([]);
    expect(localStorage.getItem("oc.themeSchedule")).toBeNull();
    expect(localStorage.getItem("oc.schedulePaint")).toBeNull();
  });
});

describe("I18nProvider under the desk default", () => {
  const root = document.documentElement;
  const DAY = "modern-office-desk-day";
  const DUSK = "modern-office-desk-dusk";
  const NIGHT = "modern-office-desk-night";

  function at(date: Date) {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["Date", "setTimeout", "clearTimeout"] });
    vi.setSystemTime(date);
  }
  async function mount() {
    await act(async () => {
      render(
        <I18nProvider>
          <Capture />
        </I18nProvider>
      );
    });
  }

  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    root.removeAttribute("style");
    delete root.dataset.theme;
    builtinLoads.length = 0;
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
  });
  afterEach(() => {
    seasonalWindows.length = 0;
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("opening at night loads only the night slot, not display_theme's desk-day, and keeps no display picture", async () => {
    at(new Date(2030, 0, 15, 20, 0));
    await mount();

    await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe(NIGHT));
    expect(ctx.theme).toBe(DAY);
    expect(builtinLoads).toEqual([NIGHT]);
    expect(localStorage.getItem("oc.themePaint")).toBeNull();
  });

  it("with a schedule on, drops the display theme's picture a device cached before the schedule, without fetching that theme", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.patchServerSettings({ displayTheme: "midnight" });
    localStorage.setItem("oc.theme", "midnight");
    localStorage.setItem("oc.themePaint", JSON.stringify(paintRecordFor(MIDNIGHT)));
    const getTheme = vi.spyOn(api, "getTheme");
    at(new Date(2030, 0, 15, 20, 0));
    await mount();

    await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe(NIGHT));
    expect(ctx.theme).toBe("midnight");
    expect(localStorage.getItem("oc.themePaint")).toBeNull();
    expect(getTheme).not.toHaveBeenCalled();
  });

  it("a display bundle that lands after reconcile turned a schedule on does not write the display picture back", async () => {
    await mockApi.patchServerSettings({ displayTheme: NIGHT });
    localStorage.setItem("oc.theme", NIGHT);
    let release!: () => void;
    builtinHold.id = NIGHT;
    builtinHold.until = new Promise<void>((r) => {
      release = r;
    });
    at(new Date(2030, 0, 15, 12, 0));
    await mount();
    await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe(DAY));
    expect(builtinLoads).toContain(NIGHT);

    await act(async () => {
      release();
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => expect(ctx.activeThemeBundle?.id).toBe(NIGHT));
    expect(localStorage.getItem("oc.themePaint")).toBeNull();
    builtinHold.id = "";
  });

  it("恢復預設 at night loads only the night slot", async () => {
    __setMockNoAppearance();
    at(new Date(2030, 0, 15, 20, 0));
    await mount();
    await waitFor(() => expect(ctx.themeSchedule).toEqual([]));
    builtinLoads.length = 0;

    await act(async () => {
      await ctx.restoreDefaultAppearance();
    });

    await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe(NIGHT));
    expect(builtinLoads).toEqual([NIGHT]);
    expect(localStorage.getItem("oc.themePaint")).toBeNull();
  });

  it("with a custom display theme and a schedule showing another one, fetches only the slot's", async () => {
    await mockApi.putTheme({ id: "dawn", name: "Dawn", colors: { "--color-bg": "#fafaf0" } });
    await mockApi.putTheme({ id: "dusk", name: "Dusk", colors: { "--color-bg": "#0a0b0c" } });
    await mockApi.patchServerSettings({
      displayTheme: "dusk",
      displayThemeSchedule: [
        { start: "07:00", theme: "dawn" },
        { start: "19:00", theme: "dusk" },
      ],
    });
    const getTheme = vi.spyOn(api, "getTheme");
    at(new Date(2030, 0, 15, 12, 0));
    await mount();

    await waitFor(() => expect(root.style.getPropertyValue("--color-bg")).toBe("#fafaf0"));
    expect(getTheme.mock.calls.map((c) => c[0])).toEqual(["dawn"]);
  });

  it("keeps the picture only for the slot on screen and the next one, colours for the rest, well under Safari's 2.5M-character storage", async () => {
    at(new Date(2030, 0, 15, 15, 55));
    await mount();
    await waitFor(() => expect(builtinLoads.sort()).toEqual([DAY, DUSK]));
    await waitFor(() => expect(withImage("oc.schedulePaint")).toEqual({ [DAY]: true, [DUSK]: true }));
    const sizes = [storedChars()];

    // One boundary per step: the next timer is armed only once React has
    // rendered the boundary before it.
    for (const minutes of [5, 140]) {
      await act(async () => {
        vi.advanceTimersByTime(minutes * 60 * 1000);
      });
    }
    await waitFor(() =>
      expect(withImage("oc.schedulePaint")).toEqual({ [DAY]: false, [DUSK]: true, [NIGHT]: true })
    );
    sizes.push(storedChars());

    await act(async () => {
      vi.advanceTimersByTime(10 * 60 * 1000);
    });
    await waitFor(() =>
      expect(withImage("oc.schedulePaint")).toEqual({ [DAY]: true, [DUSK]: false, [NIGHT]: true })
    );
    sizes.push(storedChars());
    expect(localStorage.getItem("oc.themePaint")).toBeNull();
    expect(Math.min(...sizes)).toBeGreaterThan(1_000_000);
    expect(Math.max(...sizes)).toBeLessThan(1_600_000);
  });

  describe("inside the Thanksgiving window", () => {
    beforeEach(() => {
      openThanksgiving();
      storeWholeRecords("oc.seasonalPaint", [TG_DAY, TG_DUSK, TG_NIGHT]);
    });

    for (const [hour, shown] of [
      [10, TG_DAY],
      [17, TG_DUSK],
    ] as const) {
      it(`at ${hour}:00 with the desk schedule, only the shown day part keeps a picture and everything stored fits Safari's 2,500,000 characters`, async () => {
        storeWholeRecords("oc.schedulePaint", [DAY, DUSK, NIGHT]);
        at(new Date(2026, 10, 18, hour, 0));
        await mount();

        await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe(shown));
        await waitFor(() =>
          expect(withImage("oc.seasonalPaint")).toEqual({
            [TG_DAY]: shown === TG_DAY,
            [TG_DUSK]: shown === TG_DUSK,
            [TG_NIGHT]: false,
          })
        );
        expect(withImage("oc.schedulePaint")).toEqual({ [DAY]: false, [DUSK]: false, [NIGHT]: false });
        expect(localStorage.getItem("oc.themePaint")).toBeNull();
        expect(storedChars()).toBeLessThanOrEqual(SAFARI_QUOTA);
      });

    }

    it("ten minutes before the window opens, the prefetched first day part keeps only colours and everything stored fits Safari's 2,500,000 characters", async () => {
      localStorage.removeItem("oc.seasonalPaint");
      storeWholeRecords("oc.schedulePaint", [DAY, DUSK, NIGHT]);
      at(new Date(2026, 10, 15, 23, 55));
      await mount();

      await waitFor(() => expect(withImage("oc.seasonalPaint")).toEqual({ [TG_NIGHT]: false }));
      await waitFor(() => expect(withImage("oc.schedulePaint")).toEqual({ [DAY]: true, [DUSK]: false, [NIGHT]: true }));
      expect(ctx.shownThemeBundle?.id).toBe(NIGHT);
      expect(storedChars()).toBeLessThanOrEqual(SAFARI_QUOTA);
    });

    it("at the window's start on an open page, switches to the prefetched first day part with no load, then gives it the picture and the schedule only colours", async () => {
      localStorage.removeItem("oc.seasonalPaint");
      storeWholeRecords("oc.schedulePaint", [DAY, DUSK, NIGHT]);
      at(new Date(2026, 10, 15, 23, 55));
      await mount();
      await waitFor(() => expect(tgLoad.mock.calls).toEqual([["thanksgiving-2026-night.theme.json"]]));
      await waitFor(() => expect(withImage("oc.seasonalPaint")).toEqual({ [TG_NIGHT]: false }));

      await act(async () => {
        vi.advanceTimersByTime(5 * 60 * 1000);
      });
      expect(ctx.shownThemeBundle?.id).toBe(TG_NIGHT);
      expect(tgLoad).toHaveBeenCalledTimes(1);
      await waitFor(() => expect(withImage("oc.seasonalPaint")).toEqual({ [TG_NIGHT]: true }));
      expect(withImage("oc.schedulePaint")).toEqual({ [DAY]: false, [DUSK]: false, [NIGHT]: false });
      expect(storedChars()).toBeLessThanOrEqual(SAFARI_QUOTA);
    });

    it("when the window ends, the user's slot gets its picture back for the next page load, and the next slot its own before it starts", async () => {
      storeWholeRecords("oc.schedulePaint", [DAY, DUSK, NIGHT]);
      at(new Date(2026, 10, 26, 23, 55));
      await mount();
      await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe(TG_NIGHT));
      await waitFor(() => expect(builtinLoads).toEqual([NIGHT]));
      expect(withImage("oc.schedulePaint")).toEqual({ [DAY]: false, [DUSK]: false, [NIGHT]: false });

      await act(async () => {
        vi.advanceTimersByTime(5 * 60 * 1000);
      });
      await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe(NIGHT));
      await waitFor(() => expect(withImage("oc.schedulePaint")[NIGHT]).toBe(true));
      expect(localStorage.getItem("oc.seasonalPaint")).toBeNull();
      expect(readValidatedSchedulePaint(NIGHT)?.backgrounds?.canvas).toBeTruthy();

      await act(async () => {
        vi.advanceTimersByTime((5 * 60 + 50) * 60 * 1000);
      });
      await waitFor(() =>
        expect(withImage("oc.schedulePaint")).toEqual({ [DAY]: true, [DUSK]: false, [NIGHT]: true })
      );
    });

    it("turning the seasonal theme off brings the schedule back to pictures for the slot on screen and the next one, and leaves the seasonal records colours only", async () => {
      storeWholeRecords("oc.schedulePaint", [DAY, DUSK, NIGHT]);
      at(new Date(2026, 10, 18, 15, 55));
      await mount();
      await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe(TG_DAY));
      await waitFor(() => expect(withImage("oc.schedulePaint")[DAY]).toBe(false));

      await act(async () => {
        ctx.setSeasonalTheme(false);
      });

      await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe(DAY));
      await waitFor(() =>
        expect(withImage("oc.schedulePaint")).toEqual({ [DAY]: true, [DUSK]: true, [NIGHT]: false })
      );
      expect(withImage("oc.seasonalPaint")).toEqual({ [TG_DAY]: false, [TG_DUSK]: false, [TG_NIGHT]: false });
      expect(storedChars()).toBeLessThanOrEqual(SAFARI_QUOTA);
    });
  });
});

describe("I18nProvider seasonal theme over a custom display theme", () => {
  const root = document.documentElement;
  const DAWN = { id: "dawn", name: "Dawn", colors: { "--color-bg": "#fafaf0" }, backgrounds: { canvas: PNG } };
  const copyOf = (id: string, as: string) => ({ ...paintRecordFor(themeFile(`${id}.theme.json`)).bundle, id: as, name: as });

  function at(date: Date) {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["Date", "setTimeout", "clearTimeout"] });
    vi.setSystemTime(date);
  }
  async function mount() {
    await act(async () => {
      render(
        <I18nProvider>
          <Capture />
        </I18nProvider>
      );
    });
  }
  async function showingDisplay(b: ThemeBundle, schedule: { start: string; theme: string }[] = []) {
    await mockApi.putTheme(b);
    await mockApi.patchServerSettings({ displayTheme: b.id, displayThemeSchedule: schedule });
    localStorage.setItem("oc.theme", b.id);
  }
  async function advanceMinutes(m: number) {
    await act(async () => {
      vi.advanceTimersByTime(m * 60 * 1000);
    });
  }

  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    root.removeAttribute("style");
    delete root.dataset.theme;
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
  });
  afterEach(() => {
    seasonalWindows.length = 0;
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  for (const [hour, shown] of [
    [10, TG_DAY],
    [17, TG_DUSK],
  ] as const) {
    it(`at ${hour}:00 with no schedule and a custom display theme copied from the Thanksgiving day bundle, its record keeps only colours and everything stored fits Safari's 2,500,000 characters`, async () => {
      openThanksgiving();
      storeWholeRecords("oc.seasonalPaint", [TG_DAY, TG_DUSK, TG_NIGHT]);
      const custom = copyOf(TG_DAY, "harvest-desk");
      await showingDisplay(custom);
      localStorage.setItem("oc.themePaint", JSON.stringify(paintRecordFor(custom)));
      at(new Date(2026, 10, 18, hour, 0));
      await mount();

      await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe(shown));
      await waitFor(() => expect(withImage("oc.seasonalPaint")[shown]).toBe(true));
      expect(themeRecord()?.bundle.id).toBe("harvest-desk");
      expect(themeRecord()?.bundle.backgrounds).toBeUndefined();
      expect(storedChars()).toBeLessThanOrEqual(SAFARI_QUOTA);
    });
  }

  it("inside the window on a cold load, turning the seasonal theme off brings back the custom display theme's picture", async () => {
    openThanksgiving();
    await showingDisplay(DAWN);
    localStorage.setItem("oc.themePaint", JSON.stringify(paintRecordFor(DAWN)));
    at(new Date(2026, 10, 18, 10, 0));
    await mount();
    await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe(TG_DAY));
    await waitFor(() => expect(themeRecord()?.bundle.backgrounds).toBeUndefined());

    await act(async () => {
      ctx.setSeasonalTheme(false);
    });

    await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe("dawn"));
    await waitFor(() => expect(themeRecord()?.bundle.backgrounds).toEqual(DAWN.backgrounds));
    });

  for (const [how, leave] of [
    ["turning the seasonal theme off", async () => {
      await act(async () => {
        ctx.setSeasonalTheme(false);
      });
    }],
    ["the window ending", () => advanceMinutes(30)],
  ] as const) {
    it(`with the display bundle in hand since before the window, ${how} gives its record the picture back without fetching it again`, async () => {
      openThanksgiving({ start: new Date(2026, 10, 16, 0, 0), end: new Date(2026, 10, 16, 0, 30) });
      await showingDisplay(DAWN);
      const getTheme = vi.spyOn(api, "getTheme");
      at(new Date(2026, 10, 15, 23, 55));
      await mount();
      await waitFor(() => expect(themeRecord()?.bundle.backgrounds).toEqual(DAWN.backgrounds));

      await advanceMinutes(5);
      expect(ctx.shownThemeBundle?.id).toBe(TG_NIGHT);
      await waitFor(() => expect(themeRecord()?.bundle.backgrounds).toBeUndefined());
      expect(themeRecord()?.bundle.id).toBe("dawn");

      await leave();
      await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe("dawn"));
      await waitFor(() => expect(themeRecord()?.bundle.backgrounds).toEqual(DAWN.backgrounds));
      expect(getTheme.mock.calls.map((c) => c[0])).toEqual(["dawn"]);
    });
  }

  it("under Safari's quota, the window opening on an open page takes the scheduled slot's picture away before writing the seasonal one, so the seasonal picture persists", async () => {
    openThanksgiving();
    await showingDisplay(copyOf(TG_DUSK, "big-dusk"), [
      { start: "00:00", theme: "office-light" },
      { start: "12:00", theme: "big-dusk" },
    ]);
    at(new Date(2026, 10, 15, 23, 55));
    await mount();
    await waitFor(() => expect(withImage("oc.schedulePaint")).toEqual({ "big-dusk": true }));
    await waitFor(() => expect(withImage("oc.seasonalPaint")).toEqual({ [TG_NIGHT]: false }));
    capStorageAtSafariQuota();

    await advanceMinutes(5);
    expect(ctx.shownThemeBundle?.id).toBe(TG_NIGHT);
    await waitFor(() => expect(withImage("oc.seasonalPaint")).toEqual({ [TG_NIGHT]: true }));
    expect(withImage("oc.schedulePaint")).toEqual({ "big-dusk": false });
  });

  it("under Safari's quota, a display picture that no longer fits clears its record rather than leaving the colours-only one", async () => {
    openThanksgiving();
    const big = copyOf(TG_DAY, "big-day");
    await showingDisplay(big);
    at(new Date(2026, 10, 15, 23, 55));
    await mount();
    await waitFor(() => expect(themeRecord()?.bundle.backgrounds).toBeDefined());
    await waitFor(() => expect(withImage("oc.seasonalPaint")).toEqual({ [TG_NIGHT]: false }));
    const writeUncapped = capStorageAtSafariQuota();

    await advanceMinutes(5);
    await waitFor(() => expect(withImage("oc.seasonalPaint")).toEqual({ [TG_NIGHT]: true }));
    expect(themeRecord()?.bundle.backgrounds).toBeUndefined();
    writeUncapped("other-app", "x".repeat(1_200_000));

    await act(async () => {
      ctx.setSeasonalTheme(false);
    });
    await waitFor(() => expect(ctx.shownThemeBundle?.id).toBe("big-day"));
    await waitFor(() => expect(withImage("oc.seasonalPaint")).toEqual({ [TG_NIGHT]: false }));
    expect(localStorage.getItem("oc.themePaint")).toBeNull();
  });
});
