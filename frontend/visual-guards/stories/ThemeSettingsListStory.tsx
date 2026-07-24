// CT story: the 設定 › 主題管理 list view in its REAL layout + REAL app CSS,
// used by guards jsdom cannot reach:
//   * the row TAGS (內建 / 自訂) must clear WCAG AA (≥4.5:1) — a computed-colour
//     fact (the fills resolve through color-mix()) invisible to jsdom.
//   * the built-in office row and a custom row must line up their trailing
//     action column at every width (the built-in row's download is active while
//     its edit/delete icons are inert placeholders) — a layout fact jsdom's
//     zero-geometry engine cannot assert.
//
// The custom theme is seeded the SAME way production does it: patch the mock
// server's settings, then mint a token so I18nProvider's reconcile adopts the
// bundle (no test-only backdoor). The bundle carries a `wording` overlay ON
// PURPOSE — the wording MECHANISM stays, but the row must NO LONGER render a 用詞
// badge (only the always-on 自訂 badge). `displayTheme` stays "office" so the
// ACTIVE theme is the built-in and every --color-* token resolves to its office
// default — the contrast we ship by default.
//
// Wrapped in `.app__main` to reproduce the real ancestor chain (the 22px
// gutters that a bare mount would omit — see frontend/CLAUDE.md).
import { I18nProvider } from "../../src/i18n";
import { mockApi } from "../../src/api/mock";
import { setToken } from "../../src/api/auth";
import { ThemeSettings } from "../../src/components/ThemeSettings";
import type { ThemeBundle } from "../../src/lib/themeBundle";

const AURORA: ThemeBundle = {
  id: "aurora",
  name: "Aurora",
  colors: { "--color-accent": "#123456" },
  wording: { zh: { "chat.copyShareLink": "複製連結" } },
};

export function ThemeSettingsListStory() {
  const seed = async () => {
    await mockApi.patchServerSettings({
      customThemes: [AURORA],
      displayTheme: "office",
    });
    setToken("ct-owner-token");
  };
  return (
    <div>
      <button data-testid="seed" onClick={seed}>
        seed
      </button>
      <I18nProvider>
        <div className="app__main">
          <ThemeSettings crumbs={[{ label: "設定" }]} />
        </div>
      </I18nProvider>
    </div>
  );
}
