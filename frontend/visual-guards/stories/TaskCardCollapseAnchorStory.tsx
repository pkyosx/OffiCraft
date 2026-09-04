// CT story (T-6630 ③): collapsing a WHOLE task card, inside the real scrolling
// chain and inside a LIST of cards.
//
// The sibling story (TaskCardNoteAnchorStory) mounts ONE card, which is enough
// for the note-disclosure question but cannot ask this one: the owner's report
// is「收和整個任務時,最後應該要定位到那則任務,現在好像會跑掉」— "跑掉" is a
// statement about where the card ends up RELATIVE TO THE REST OF THE COLUMN, so
// there has to be a column. Cards above give the card a non-zero offset; cards
// below give the scrollport range to move within (a card parked at the very end
// of the range keeps its position for the wrong reason).
//
//   .app (viewport-tall) > .app__main > .tasks > .tasks__section > .tasks__list
//
// Same chain, and the same reason, as the note story: `.tasks` is what scrolls
// in production, not the document.
import { useLayoutEffect } from "react";
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, mkNoteStep, MIRA, NOOP, WORKERS } from "./taskFixtures";
import { LIGHT_PACK } from "./ThemeContrastStory";

const LONG_NOTE = (n: number) =>
  [
    `第 ${n} 步做到哪:handler 已完成,conformance 三份重生一致。`,
    "",
    "下一步:補負面案例(400 / 403),再跑一次 `bin/ci.sh` 確認整輪是綠的。",
    "",
    "風險:seed 舊資料沒有 note 欄位,列表要能容忍空值;另外舊版 client 會把",
    "空字串當成「有備註但內容為空」,所以 wire 上要維持 optional。",
  ].join("\n");

// An expanded card must be TALLER THAN THE SCROLLPORT — that is the whole
// reported situation: you scrolled down inside one task, so its top edge is
// already off screen when you collapse it. Nine steps each carrying a note does
// that at both widths (~776/820px scrollports).
const makeTask = (i: number) =>
  mkTask({
    id: `t-c${i}ab5291a01`,
    taskNo: `t-c${i}ab5291a01`,
    title: `第 ${i} 張任務卡:收合後畫面要停在這張`,
    status: "in_progress",
    description: "九個步驟,每一步都有一則備註。",
    progressDone: 3,
    progressTotal: 9,
    steps: Array.from({ length: 9 }, (_, s) =>
      // T-66: size on the card, text behind the per-step read (see mkNoteStep).
      mkNoteStep(
        {
          id: `c${i}-s-${s + 1}`,
          name: `節點 ${s + 1}`,
          dod: `第 ${s + 1} 步的驗收標準。`,
          status: s < 3 ? "done" : s === 3 ? "in_progress" : "pending",
        },
        LONG_NOTE(s + 1)
      )
    ),
  });

export function TaskCardCollapseAnchorStory({
  theme = "dark",
  cards = 5,
}: {
  theme?: "dark" | "light";
  cards?: number;
}) {
  const TASKS = Array.from({ length: cards }, (_, i) => makeTask(i + 1));
  useLayoutEffect(() => {
    const root = document.documentElement;
    document.body.style.margin = "0";
    root.style.height = "100%";
    document.body.style.height = "100%";
    if (theme === "light") {
      for (const [k, v] of Object.entries(LIGHT_PACK))
        root.style.setProperty(k, v);
    } else {
      for (const k of Object.keys(LIGHT_PACK)) root.style.removeProperty(k);
    }
  }, [theme]);
  return (
    <I18nProvider>
      <div className="app" style={{ height: "100vh" }}>
        <main className="app__main">
          <div className="tasks">
            <section className="tasks__section">
              <div className="tasks__list">
                {TASKS.map((task) => (
                  <TaskCard
                    key={task.id}
                    task={task}
                    allTasks={TASKS}
                    members={[MIRA]}
                    workers={WORKERS}
                    nowTs={3000}
                    onTerminate={NOOP as never}
                    onMarkDuplicate={NOOP as never}
                    onSetPriority={NOOP as never}
                    onSendMessage={NOOP as never}
                    onReassign={NOOP as never}
                    onHydrate={(async () => task) as never}
                  />
                ))}
              </div>
            </section>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
