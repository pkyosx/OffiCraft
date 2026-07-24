// CT story for req4 — the 設定 › 主題管理 "新增" flow. Starts with NO custom
// themes (just the built-in office row); clicking 新增 must create an office-based
// custom theme and jump straight into the edit view. Rendered in its REAL app CSS
// (theme.css is loaded by playwright/index.ts) so exportOfficeBaseTheme reads the
// genuine office :root palette — the whole point is that the new theme is seeded
// with office's colours.
import { I18nProvider } from "../../src/i18n";
import { ThemeSettings } from "../../src/components/ThemeSettings";

export function ThemeSettingsAddStory() {
  return (
    <I18nProvider>
      <div className="app__main">
        <ThemeSettings crumbs={[{ label: "設定" }]} />
      </div>
    </I18nProvider>
  );
}
