// Dual-layer theme/language (T-0b41-p2): the localStorage pre-auth CACHE drives
// the first paint (zero-flash, server unreachable pre-auth), and the server is
// the cross-device TRUTH reconciled in at login — applied to state + written
// back to the cache. These jsdom tests cover the state/localStorage/<html
// data-theme> reconcile wiring; the geometry-free bits a real browser is not
// needed for (the visual pre-auth guard lives in the Playwright CT suite).

import { describe, it, expect, beforeEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { I18nProvider, useI18n } from "./index";
import { zh } from "./locales/zh";
import { mockApi, __resetMock } from "../api/mock";
import { setToken, TOKEN_KEY } from "../api/auth";

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
    localStorage.clear();
    delete document.documentElement.dataset.theme;
  });

  it("first paint uses the localStorage cache when pre-auth (no server call)", () => {
    // A cached custom-theme id (office is the only built-in now; a non-default
    // theme is a custom bundle id). Pre-reconcile the bundle isn't loaded yet,
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

  it("adopts the server value on mount when a token already exists, writing it back to the cache", async () => {
    // Cache is empty (defaults office/zh); the server holds the owner's choice —
    // a custom theme plus display.theme pointing at it (so the id is selectable).
    await mockApi.patchServerSettings({
      customThemes: [MIDNIGHT],
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
    await mockApi.patchServerSettings({
      customThemes: [MIDNIGHT],
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

describe("I18nProvider custom theme apply", () => {
  const root = document.documentElement;

  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    root.removeAttribute("style");
    delete root.dataset.theme;
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
  });

  it("applies a custom bundle as inline vars over the neutral office base", () => {
    act(() => ctx.commitCustomThemes([MIDNIGHT]));
    act(() => ctx.setTheme("midnight"));

    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-accent")).toBe("#010203");
    expect(root.style.getPropertyValue("--color-bg")).toBe("#040506");
  });

  it("clears the previous custom vars when switching to the built-in office", () => {
    act(() => ctx.commitCustomThemes([MIDNIGHT]));
    act(() => ctx.setTheme("midnight"));
    act(() => ctx.setTheme("office"));

    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-accent")).toBe("");
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
  });

  it("drops vars not carried by the next custom theme when switching between them", () => {
    act(() => ctx.commitCustomThemes([MIDNIGHT, SUNRISE]));
    act(() => ctx.setTheme("midnight"));
    act(() => ctx.setTheme("sunrise"));

    expect(root.style.getPropertyValue("--color-accent")).toBe("#ffaa00");
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
  });

  it("falls back to the office base for a dangling custom id", () => {
    act(() => ctx.setTheme("ghost"));

    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-accent")).toBe("");
  });

  it("caches a custom active id to localStorage", () => {
    act(() => ctx.commitCustomThemes([MIDNIGHT]));
    act(() => ctx.setTheme("midnight"));
    expect(localStorage.getItem("oc.theme")).toBe("midnight");
  });

  it("keeps a custom theme visual-only — copy follows the language toggle", () => {
    act(() => ctx.setLanguage("en"));
    act(() => ctx.commitCustomThemes([MIDNIGHT]));
    act(() => ctx.setTheme("midnight"));
    expect(ctx.locale).toBe("en");
  });

  it("keeps the locale following the language toggle regardless of theme (theme↔locale decoupled)", () => {
    act(() => ctx.setLanguage("zh"));
    act(() => ctx.setTheme("office"));
    expect(ctx.locale).toBe("zh");
    act(() => ctx.setLanguage("en"));
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
    localStorage.clear();
    document.documentElement.removeAttribute("style");
    delete document.documentElement.dataset.theme;
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
  });

  it("overrides an overridden code in the active language, leaving others intact", () => {
    act(() => ctx.commitCustomThemes([WORDED]));
    act(() => ctx.setTheme("worded"));
    // zh is the default language → the zh override applies.
    expect(ctx.t.nav.tasks).toBe("任務榜");
    // A non-overridden code keeps its base value (fallback = original language).
    expect(ctx.t.nav.monitor).toBe(zh.nav.monitor);
  });

  it("follows the language toggle for the overlay language", () => {
    act(() => ctx.setLanguage("en"));
    act(() => ctx.commitCustomThemes([WORDED]));
    act(() => ctx.setTheme("worded"));
    expect(ctx.t.nav.tasks).toBe("Quest Board");
  });

  it("restores the base wording when switching away from the theme", () => {
    act(() => ctx.commitCustomThemes([WORDED]));
    act(() => ctx.setTheme("worded"));
    expect(ctx.t.nav.tasks).toBe("任務榜");
    act(() => ctx.setTheme("office"));
    expect(ctx.t.nav.tasks).toBe(zh.nav.tasks);
  });

  it("leaves a custom theme without wording on the base dict", () => {
    act(() => ctx.commitCustomThemes([SUNRISE]));
    act(() => ctx.setTheme("sunrise"));
    expect(ctx.t.nav.tasks).toBe(zh.nav.tasks);
  });
});
