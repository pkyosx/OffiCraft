// CT story for the two contrast facts a THEME PACK can break and jsdom cannot
// see, because both are computed colours (color-mix / alpha compositing) read
// off real app CSS (T-081b round 6):
//
//   ① the 頂列 (topbar) inline-edit input. Its FILL is --color-bg while the bar
//      under it is --color-topbar-bg — a pairing that only exists since this
//      ticket split the shell into three zone tokens, and one a light theme
//      collapses to ~1.11:1. What keeps the field visible is its BORDER, so the
//      border is what the guard measures.
//   ② the 登入 / 首啟 hint line (.login__hint) on the login card.
//   ③ the 任務 nav tab's due warning triangle, red and amber, on an idle and an active tab.
//
// A theme is applied the way the product applies one: --color-* custom
// properties set on documentElement. `light` seeds LIGHT_PACK below, which is
// the palette of a REAL shipped theme pack — the one the T-081b round 6 audit
// measured — copied here verbatim, so the guard reports a shipped-theme fact,
// not a synthetic worst case. Do not "tidy" these values: the moment they are
// hand-tuned, the guard stops measuring anything a user can actually ship.
import { InlineEdit } from "../../src/components/InlineEdit";
import { I18nProvider } from "../../src/i18n";
import { AlertTriangleIcon } from "../../src/components/icons";
import "../../src/components/login.css";

export const LIGHT_PACK: Record<string, string> = {
  "--color-bg": "#c2d492",
  "--color-card": "#fdfbf1",
  "--color-text": "#33301f",
  "--color-text-strong": "#1e1c10",
  "--color-text-muted": "#403d2c",
  "--color-topbar-bg": "rgba(179, 200, 134, 0.8)",
  "--color-nav-bg": "rgba(215, 207, 164, 0.8)",
  "--color-main-bg": "rgba(241, 234, 209, 0.8)",
  "--color-border": "#b0ae83",
  "--color-accent": "#2b450b",
  "--color-overlay": "#241f0d",
};

export function ThemeContrastStory() {
  const applyLight = () => {
    for (const [k, v] of Object.entries(LIGHT_PACK))
      document.documentElement.style.setProperty(k, v);
  };
  return (
    <I18nProvider>
      <button data-testid="apply-light" onClick={applyLight}>
        light
      </button>
      <header className="topbar">
        <div className="topbar__brand">
          <InlineEdit
            value="辦公室"
            onCommit={() => {}}
            ariaLabel="改名"
            displayClassName="topbar__org"
          />
        </div>
      </header>
      <nav className="nav-tabs">
        <div className="nav-tabs__seg">
          {(["idle", "active"] as const).map((state) => (
            <button
              key={state}
              type="button"
              className={state === "active" ? "nav-tab nav-tab--active" : "nav-tab"}
            >
              {(["overdue", "soon"] as const).map((tone) => (
                <span
                  key={tone}
                  className={`nav-tab__due-warning nav-tab__due-warning--${tone}`}
                  data-testid={`due-warning-${tone}-${state}`}
                >
                  <AlertTriangleIcon size={14} />
                </span>
              ))}
            </button>
          ))}
        </div>
      </nav>
      <div className="login">
        <div className="login__card">
          <p className="login__hint">提示文字</p>
        </div>
      </div>
    </I18nProvider>
  );
}
