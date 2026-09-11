// CT story for T-5e79 — the 「預設」 pill (`.set-badge`) and the 「編輯」 button
// (`.doc-btn--edit`) that sit in the SAME header row of the Insight card.
//
// Why the real card and not a hand-built row: the defect is a flex-shrink
// story. `.mp-lessons__head` (shared by the journal cards) is
// `display:flex; justify-content:space-between`,
// its title span and its action button are both shrinkable flex items, and the
// badge inside the title is a THIRD shrinkable item. A hand-rolled row would
// have to re-declare that chain by hand and could drift from it; mounting
// <InsightCard> binds the guard to the production markup and the production
// sheets (`member-detail.css` comes in through the component's own import,
// `settings.css` through playwright/index.ts).
//
// The ancestor chain is reproduced BY CLASS, per frontend/CLAUDE.md 〈浮層寬度
// 不可用 vw 夾〉: a bare card mounted at x≈0 carries ~22px of slack it does not
// have in the app, which is exactly how earlier 390px guards stayed green while
// the owner's phone was broken. Production is
//   .app > .app__main (max-width 1040 + 22px side padding) > .settings > card.
//
// roleKey="assistant" is the ONE role the mock (and the server) carries an
// insight file seed for, so it is the only role whose card satisfies the
// badge's render gate `isDefault && text.trim() !== ""` out of the box — the
// same state the owner screenshotted on 2026-08-04 after restoring the
// assistant's insight to the factory version.
import { I18nProvider } from "../../src/i18n";
import { InsightCard } from "../../src/components/InsightCard";

export function InsightBadgeNarrowStory() {
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <div className="settings">
            <InsightCard roleKey="assistant" />
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
