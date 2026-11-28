// CT story: the asking outsource worker's CURRENT TASK under the 請示卡's
// 開卡人 chip.
//
// Same philosophy as ReplyCardOpenerStory: RepliesPage owns live hooks this
// story does not wire up, so it reproduces the REAL DOM skeleton
// (.card-meta / .reply-card__worker-task) against the
// REAL replies.css + office.css, and mounts the REAL components RepliesPage
// renders — `OutsourceTaskLine` and `CurrentTaskTitle`, not a hand-copied
// approximation, so a mutant in either can redden this guard.
//
// ⚠️ KNOWN BOUNDARY: the SKELETON is a copy. If RepliesPage moves the row out
// of `.card-meta`, this guard keeps measuring the old arrangement and
// stays green. What it does cover is the thing jsdom cannot see at all: with
// both stylesheets loaded and real layout in play, does the title actually
// clamp here the way it does in the rail, and does the card survive a long
// title at phone width without pushing the card into horizontal scroll.
import { I18nProvider } from "../../src/i18n";
import { CurrentTaskTitle } from "../../src/components/CurrentTaskTitle";
import { OutsourceTaskLine } from "../../src/components/OutsourcePanel";
import { JumpToMessageIcon } from "../../src/components/replyCardActionIcons";
import { mkWorker, VERY_LONG_TITLE } from "./currentTaskFixtures";
import "../../src/components/replies.css";
import "../../src/components/card-meta.css";
import "../../src/components/office.css";

export function ReplyCardWorkerTaskStory() {
  const worker = mkWorker({});
  return (
    <I18nProvider>
      <div className="replies" style={{ padding: 12 }}>
        <article className="reply-card" data-testid="card">
          <header className="reply-card__head">
            <div className="reply-card__head-end">
              <div className="reply-card__actions">
                <button
                  type="button"
                  className="reply-card__icon-btn"
                  aria-label="跳到原訊息"
                  title="跳到原訊息"
                >
                  <JumpToMessageIcon size={13} />
                </button>
              </div>
              <span className="reply-card__waited">已等你 25m</span>
            </div>
          </header>
          <div className="reply-card__summary">要照這個方向做嗎？</div>
          <div className="card-meta">
            <span className="card-meta__label">開卡人</span>
            <button type="button" className="card-meta__chip card-meta__chip--link">
              <span className="card-meta__chip-name">O-30</span>
            </button>
            <div className="reply-card__worker-task">
              <OutsourceTaskLine
                worker={worker}
                onOpenTask={() => {}}
                idPrefix="reply-card-rc-1"
              />
              <CurrentTaskTitle
                title={VERY_LONG_TITLE}
                clamp
                testid="reply-card-task-title-rc-1"
              />
            </div>
          </div>
        </article>
      </div>
    </I18nProvider>
  );
}
