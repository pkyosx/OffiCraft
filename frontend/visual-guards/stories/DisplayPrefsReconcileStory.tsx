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
//     /api/settings, adopts the custom bundle set AND the active id, and the
//     bundle's colours VISUALLY take effect. Same seam production uses; no
//     test-only backdoor.
import { I18nProvider, useI18n } from "../../src/i18n";
import { mockApi } from "../../src/api/mock";
import { setToken } from "../../src/api/auth";

/** The imported custom theme this guard reconciles in. Its --color-bg is a
 * distinctive value so the visual paint is unmistakable (rgb(1, 2, 3)). */
const MIDNIGHT = {
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
      // custom_themes + display_theme land in one PATCH (server parity), so the
      // reconcile can resolve the active id to its freshly-saved bundle.
      await mockApi.patchServerSettings({
        customThemes: [MIDNIGHT],
        displayTheme: "midnight",
      });
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
