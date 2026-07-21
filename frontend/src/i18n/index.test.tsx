// Dual-layer theme/language (T-0b41-p2): the localStorage pre-auth CACHE drives
// the first paint (zero-flash, server unreachable pre-auth), and the server is
// the cross-device TRUTH reconciled in at login — applied to state + written
// back to the cache. These jsdom tests cover the state/localStorage/<html
// data-theme> reconcile wiring; the geometry-free bits a real browser is not
// needed for (the visual pre-auth guard lives in the Playwright CT suite).

import { describe, it, expect, beforeEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { I18nProvider, useI18n } from "./index";
import { mockApi, __resetMock } from "../api/mock";
import { setToken, TOKEN_KEY } from "../api/auth";

function Probe() {
  const { theme, language } = useI18n();
  return <div data-testid="probe" data-theme={theme} data-lang={language} />;
}

describe("I18nProvider dual-layer theme/language", () => {
  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    delete document.documentElement.dataset.theme;
  });

  it("first paint uses the localStorage cache when pre-auth (no server call)", () => {
    localStorage.setItem("oc.theme", "xian");
    localStorage.setItem("oc.language", "en");
    // No token → /api/settings is unreachable; the cache is the only truth.
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    expect(screen.getByTestId("probe").dataset.theme).toBe("xian");
    expect(screen.getByTestId("probe").dataset.lang).toBe("en");
    expect(document.documentElement.dataset.theme).toBe("xian");
  });

  it("adopts the server value on mount when a token already exists, writing it back to the cache", async () => {
    // Cache is empty (defaults office/zh); the server holds the owner's choice.
    await mockApi.patchServerSettings({ displayTheme: "xian", displayLanguage: "en" });
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    await waitFor(() =>
      expect(screen.getByTestId("probe").dataset.theme).toBe("xian")
    );
    expect(screen.getByTestId("probe").dataset.lang).toBe("en");
    expect(document.documentElement.dataset.theme).toBe("xian");
    // Written back so the NEXT pre-auth first paint is already correct.
    expect(localStorage.getItem("oc.theme")).toBe("xian");
    expect(localStorage.getItem("oc.language")).toBe("en");
  });

  it("reconciles when a login mints a token mid-session (oc-auth-login)", async () => {
    // Pre-auth first paint: default office/zh, server not yet reachable.
    await mockApi.patchServerSettings({ displayTheme: "xian", displayLanguage: "en" });
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
      expect(screen.getByTestId("probe").dataset.theme).toBe("xian")
    );
    expect(screen.getByTestId("probe").dataset.lang).toBe("en");
  });

  it("keeps the cache when the server pref is unset (\"\")", async () => {
    localStorage.setItem("oc.theme", "xian");
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
      expect(screen.getByTestId("probe").dataset.theme).toBe("xian")
    );
    expect(localStorage.getItem("oc.theme")).toBe("xian");
  });
});
