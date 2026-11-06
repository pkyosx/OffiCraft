import { I18nProvider } from "../../src/i18n";
import { ReplyCardTaskRef } from "../../src/components/ReplyCardBody";
import { TaskDueChip } from "../../src/components/TaskChips";

const DUE_SOON_TS = Math.floor(Date.now() / 1000) + 60 * 60;
const CARD_COUNT = 40;

export function ReplyTaskDueScrollStory() {
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <div className="replies" data-testid="reply-scroll">
            <div className="replies__list">
              {Array.from({ length: CARD_COUNT }, (_, index) => {
                const taskNo = index + 1;
                return (
                  <article className="reply-card" key={taskNo}>
                    <ReplyCardTaskRef
                      task={{
                        id: `T-${taskNo}`,
                        typeKey: "",
                        title: `期限測試任務 ${taskNo}`,
                      }}
                      onJump={() => {}}
                      jumpLabel="查看任務"
                      lead={
                        <span className="reply-card__task-chips">
                          <TaskDueChip
                            dueTs={DUE_SOON_TS}
                            leadMinutes={24 * 60}
                            testId={`reply-task-due-${taskNo}`}
                          />
                        </span>
                      }
                    />
                    <p className="reply-card__body">假資料回覆卡內容。</p>
                  </article>
                );
              })}
            </div>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
