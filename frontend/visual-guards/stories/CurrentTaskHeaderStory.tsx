// CT story: the T-3451 current-task-title line in the chat HEADER — the peer
// bar shape ChatArea emits, with the REAL full-title line (clamp={false}). The
// full title must render un-truncated and wrap without forcing a page-level
// horizontal scrollbar at the owner's phone widths.
import { I18nProvider } from "../../src/i18n";
import { CurrentTaskTitle } from "../../src/components/CurrentTaskTitle";
import { LONG_TITLE } from "./currentTaskFixtures";

export function CurrentTaskHeaderStory() {
  return (
    <I18nProvider>
      <div className="office">
        <section className="office__chat">
          <div className="chat">
            <header className="chat__header">
              <div className="chat__header-text">
                <div className="chat__header-name">
                  <span>Kyle</span>
                </div>
                <div className="chat__header-sub">
                  <span className="presence-badge__sub">OC Developer</span>
                </div>
                <div className="chat__header-task">
                  <CurrentTaskTitle
                    title={LONG_TITLE}
                    clamp={false}
                    showEmpty={false}
                    testid="chat-header-task-title"
                  />
                </div>
              </div>
            </header>
          </div>
        </section>
      </div>
    </I18nProvider>
  );
}
