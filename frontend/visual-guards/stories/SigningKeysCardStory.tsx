// CT story for 設定 › 系統更新與備份 · 簽章金鑰 (T-62).
//
// jsdom cannot see any of what this measures: it applies no layout engine, so a
// remove button pushed off the card by a long key row is in the DOM exactly as
// if it were on screen. The card's own vitest file mocks the hook and asserts
// WORDING and BEHAVIOUR; the geometry has to be measured in a browser.
//
// The ancestor chain is reproduced BY CLASS (frontend/CLAUDE.md 〈浮層寬度不可用
// vw 夾〉): a bare card mounted at x≈0 carries ~22px of slack it does not have
// in the app, which is how a narrow-width guard can stay green on a phone that
// is actually broken. Production is
//   .app > .app__main (max-width 1040 + side padding) > .settings > card.
//
// THE FIXTURE IS THE HOSTILE CASE, not the pretty one: the row that has to
// survive is a retired key carrying BOTH the "in use since before this was
// recorded" sentence AND a remove button, at the same time, on a phone. That is
// the widest a row ever gets, and it is the default state of every install that
// has never rotated.
import { I18nProvider } from "../../src/i18n";
import { SigningKeysCard } from "../../src/components/SigningKeysCard";

export function SigningKeysCardStory() {
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <div className="settings">
            <SigningKeysCard />
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
