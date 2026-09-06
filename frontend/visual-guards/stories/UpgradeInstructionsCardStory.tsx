// CT story for 設定 › 系統更新與備份 · 換版交代單 (T-79).
//
// jsdom cannot see any of what this measures: it applies no layout engine, so
// an instruction long enough to push the 收回 button off the card sits in the
// DOM exactly as if it were on screen. The card's own vitest file mocks the
// hook and asserts WORDING and BEHAVIOUR; the geometry has to be measured in a
// browser.
//
// The ancestor chain is reproduced BY CLASS (frontend/CLAUDE.md 〈浮層寬度不可
// 用 vw 夾〉): a bare card mounted at x≈0 carries ~22px of slack it does not
// have in the app, which is how a narrow-width guard stays green on a phone
// that is actually broken. Production is
//   .app > .app__main (max-width 1040 + side padding) > .settings > card.
//
// THE FIXTURE IS THE HOSTILE CASE. The mock's second open instruction names a
// migration path and a full 40-character sha — neither can break at a space,
// and that row carries BOTH buttons (標記完成 and 收回) because it is open.
// That is the widest this card ever gets, and it is an ordinary instruction,
// not a contrived one: telling the assistant which file to check is the whole
// point of the feature.
import { I18nProvider } from "../../src/i18n";
import { UpgradeInstructionsCard } from "../../src/components/UpgradeInstructionsCard";

export function UpgradeInstructionsCardStory() {
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <div className="settings">
            <UpgradeInstructionsCard />
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
