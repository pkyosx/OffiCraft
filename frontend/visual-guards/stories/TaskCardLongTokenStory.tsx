// CT story (T-4974): an expanded TaskCard whose free-text surfaces each carry
// an unbreakable long token — the exact shape the owner's iPhone screenshot
// circled inside a task card's baton text:
//   `twin(desired_state/desired_machine_id/refocus_since/bank_balance/...)`.
//
// The three owner/agent free-text renders on a card all go through `.doc-md`:
//   • description        → `.task-card__desc.doc-md`      (expanded-only)
//   • step DoD           → `.task-step__dod .doc-md`      (expanded-only)
//   • waiting reason     → `.task-step__waiting-md.doc-md`(a flex row item)
// (T-c514: the waiting reason used to render at the TASK level too; that
// duplicate block was removed, so the step's row is the surface under test.
// The first step is waiting_external here so that row actually renders.)
// None of them declared overflow-wrap before T-4974, so a no-whitespace token
// set the card's min-content to the token's full width, pushed the card past a
// 390px viewport and the whole PAGE scrolled sideways. Real TaskCard + real
// tasks.css so the guard measures production layout (jsdom is blind to it).
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, mkStep, MIRA, NOOP, WORKERS } from "./taskFixtures";

const TOKEN =
  "twin(desired_state/desired_machine_id/refocus_since/bank_balance/created_ts/activated_ts)";

// T-c21e: the dep row now LEADS with the dep task's 狀態 badge, so that row got
// one nowrap pill wider on a card that 1ea673e had just finished stopping from
// bursting its container at 390px. The row is flex-nowrap and only
// `.task-card__dep-title` may shrink (min-width: 0) — so badge + ⏱ + 「等 T-」 +
// ↗ form a hard floor, and this fixture is what proves that floor still fits a
// phone. 等我回覆 is TIED for the widest of the seven status labels (measured
// 42.3px, level with 尚未執行 and 等待外部 — an earlier version of this comment
// called it "the widest", which was a superlative nobody had measured; any of
// the three stresses the floor equally). The title carries the unbreakable
// token so the shrinking half is stressed at the same time.
const DEP_TASK = mkTask({
  id: "t-dep1",
  taskNo: "T-dep1",
  title: `等 member ${TOKEN} 遷移完成才能動`,
  status: "waiting_owner",
});

const LONG_TASK = mkTask({
  id: "t-fbcf",
  taskNo: "T-fbcf",
  title: "外包/worker 子系統收斂",
  status: "waiting_external",
  deps: [DEP_TASK.id],
  waitingReason: `等待 member ${TOKEN} 遷移完成`,
  description: `接 A案:00017-21 已把 outsource worker 欄位對齊 member ${TOKEN} → 合表資料形狀收斂已半完成。`,
  progressDone: 1,
  progressTotal: 2,
  steps: [
    mkStep({
      status: "waiting_external",
      waitingReason: `等 member ${TOKEN} 遷移完成`,
      dod: `寫碼坐實溢出容器層級後,390px 下 member ${TOKEN} 折行、頁面無橫向捲軸。`,
    }),
    mkStep({ status: "pending" }),
  ],
});

export function TaskCardLongTokenStory() {
  return (
    <I18nProvider>
      <TaskCard
        task={LONG_TASK}
        allTasks={[LONG_TASK, DEP_TASK]}
        depsResolvable
        members={[MIRA]}
        workers={WORKERS}
        nowTs={3000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onSendMessage={NOOP as never}
        onReassign={NOOP as never}
        onHydrate={(async () => LONG_TASK) as never}
      />
    </I18nProvider>
  );
}
