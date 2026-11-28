// CT story: the current-task-title line in the chat HEADER — the peer bar shape
// ChatArea emits, with the same 2-line clamped title line. VERY_LONG_TITLE so
// the clamp truncates even at the widest tested width.
import { I18nProvider } from "../../src/i18n";
import { CurrentTaskTitle } from "../../src/components/CurrentTaskTitle";
import { VERY_LONG_TITLE } from "./currentTaskFixtures";

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
                    title={VERY_LONG_TITLE}
                    clamp
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
