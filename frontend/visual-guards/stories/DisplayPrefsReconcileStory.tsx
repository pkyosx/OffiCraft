// CT story: the dual-layer theme reconcile (T-0b41-p2) in a REAL browser.
//
// The pre-auth CACHE → visual paint and the login RECONCILE → visual replace
// are properties of the REAL theme.css `:root[data-theme="xian"]` variable swap
// applied to document.documentElement by I18nProvider's effect — a COMPUTED
// STYLE change jsdom's unit suite cannot resolve (jsdom sees the data-theme
// attribute but never repaints --color-bg). The swatch's background is
// `var(--color-bg)`, so its resolved rgb is the theme actually in effect:
//   office #191c24 → rgb(25, 28, 36) ; xian #14100a → rgb(20, 16, 10).
//
// Seeding: the test sets the localStorage cache (page.evaluate) BEFORE mount so
// I18nProvider's synchronous initializer reads it — the pre-auth first paint.
// The login button seeds the mock server value then mints a token via setToken,
// which fires oc-auth-login → the provider's reconcile pulls /api/settings and
// applies the server value. Same seam production uses; no test-only backdoor.
import { I18nProvider, useI18n } from "../../src/i18n";
import { mockApi } from "../../src/api/mock";
import { setToken } from "../../src/api/auth";

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
  serverTheme,
}: {
  /** The theme the mock server holds — applied on login click. */
  serverTheme?: "office" | "xian";
}) {
  const login = async () => {
    if (serverTheme) {
      await mockApi.patchServerSettings({ displayTheme: serverTheme });
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
