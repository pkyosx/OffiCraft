// CT story (T-196): the asking outsource worker's CURRENT TASK line inside the
// 請示卡 head.
//
// Same philosophy as ReplyCardAvatarStory: RepliesPage owns live hooks this
// story does not wire up, so it reproduces the REAL DOM skeleton
// (.reply-card__head / .reply-card__who / .reply-card__worker-task) against the
// REAL replies.css + office.css, and mounts the REAL components RepliesPage
// renders — `OutsourceTaskLine` and `CurrentTaskTitle`, not a hand-copied
// approximation, so a mutant in either can redden this guard.
//
// ⚠️ KNOWN BOUNDARY: the SKELETON is a copy. If RepliesPage moves the line out
// of `.reply-card__who`, this guard keeps measuring the old arrangement and
// stays green. What it does cover is the thing jsdom cannot see at all: with
// both stylesheets loaded and real layout in play, does the title actually
// clamp here the way it does in the rail, and does the head survive a long
// title at phone width without pushing the card into horizontal scroll.
import { I18nProvider } from "../../src/i18n";
import { CurrentTaskTitle } from "../../src/components/CurrentTaskTitle";
import { OutsourceTaskLine } from "../../src/components/OutsourcePanel";
import { mkWorker, VERY_LONG_TITLE } from "./currentTaskFixtures";
import "../../src/components/replies.css";
import "../../src/components/office.css";

export function ReplyCardWorkerTaskStory() {
  const worker = mkWorker({});
  return (
    <I18nProvider>
      <div className="replies" style={{ padding: 12 }}>
        <article className="reply-card" data-testid="card">
          <header className="reply-card__head">
            <div className="reply-card__avatar" />
            <div className="reply-card__who">
              <span className="reply-card__name">外包 · O-30</span>
              <span className="reply-card__worker-task">
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
              </span>
            </div>
            <button type="button" className="reply-card__jump">
              跳到原訊息
            </button>
            <span className="reply-card__waited">已等你 25m</span>
          </header>
          <div className="reply-card__summary">要照這個方向做嗎？</div>
        </article>
      </div>
    </I18nProvider>
  );
}
