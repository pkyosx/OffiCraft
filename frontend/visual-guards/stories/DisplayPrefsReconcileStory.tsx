// CT story: the dual-layer theme reconcile (T-0b41-p2) in a REAL browser, now
// driven by an IMPORTED CUSTOM BUNDLE (office is the only built-in — 修仙 and
// every other theme is a user-imported bundle, T-16a1 P35).
//
// A custom theme paints by pushing its `colors` onto document.documentElement
// via setProperty — a COMPUTED STYLE change jsdom's unit suite cannot resolve
// (jsdom sees the data-theme-name attribute but never repaints --color-bg). The
// swatch's background is `var(--color-bg)`, so its resolved rgb is the theme
// actually in effect:
//   office base #191c24 → rgb(25, 28, 36) ; the Midnight bundle #010203 → rgb(1, 2, 3).
//
// Two layers, two facts a real browser proves:
//   * pre-auth: the localStorage cache carries the ACTIVE id, but a custom
//     bundle only arrives with the server reconcile — so a cached custom id
//     that is not yet loaded safely paints the office BASE (the dangling-id
//     fallback). The cache drives the id (data-theme-name); the paint waits.
//   * login: setToken fires oc-auth-login → the provider's reconcile pulls
//     /api/settings AND /api/themes, adopts the active id, FETCHES that one
//     theme in full, and the bundle's colours VISUALLY take effect. Same seam
//     production uses; no test-only backdoor.
//
// T-83ef: a theme is its own resource now, so the seed below is two writes —
// the theme through PUT /api/themes/{id}, and "which theme is active" through
// /api/settings. They are separate on purpose: the reconcile has to find the
// id in the theme LIST before it will adopt it, so a story that only patched
// display_theme would be adopting a dangling id and would paint the office
// base for a reason that has nothing to do with the guard.
import { I18nProvider, useI18n } from "../../src/i18n";
import { mockApi } from "../../src/api/mock";
import { setToken } from "../../src/api/auth";
import type { ThemeBundle } from "../../src/lib/themeBundle";

/** The imported custom theme this guard reconciles in. Its --color-bg is a
 * distinctive value so the visual paint is unmistakable (rgb(1, 2, 3)). */
const MIDNIGHT: ThemeBundle = {
  id: "midnight",
  name: "Midnight",
  colors: { "--color-bg": "#010203" },
};

function Swatch() {
  const { theme } = useI18n();
  return (
    <div
      data-testid="swatch"
      data-theme-name={theme}
      style={{ background: "var(--color-bg)", width: 120, height: 120 }}
    />
  );
}

export function DisplayPrefsReconcileStory({
  serverBundle,
}: {
  /** When set, the mock server holds the Midnight custom bundle as the active
   * theme — adopted (set + active id) when login mints a token. */
  serverBundle?: boolean;
}) {
  const login = async () => {
    if (serverBundle) {
      // The theme first, THEN the pointer at it: the reconcile adopts a stored
      // display_theme only when that id is actually in the theme list, so a
      // pointer written before the theme exists would simply be ignored.
      await mockApi.putTheme(MIDNIGHT);
      await mockApi.patchServerSettings({ displayTheme: "midnight" });
    }
    // Minting a token is what a real login does; setToken fires oc-auth-login,
    // the exact trigger I18nProvider reconciles on.
    setToken("ct-owner-token");
  };
  return (
    <div>
      <button data-testid="login" onClick={login}>
        login
      </button>
      <I18nProvider>
        <Swatch />
      </I18nProvider>
    </div>
  );
}
