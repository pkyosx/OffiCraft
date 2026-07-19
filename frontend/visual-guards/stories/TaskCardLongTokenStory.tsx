// CT story (T-4974): an expanded TaskCard whose free-text surfaces each carry
// an unbreakable long token — the exact shape the owner's iPhone screenshot
// circled inside a task card's baton text:
//   `twin(desired_state/desired_machine_id/refocus_since/bank_balance/...)`.
//
// The three owner/agent free-text renders on a card all go through `.doc-md`:
//   • description        → `.task-card__desc.doc-md`      (expanded-only)
//   • step DoD           → `.task-step__dod .doc-md`      (expanded-only)
//   • waiting reason      → `.task-card__waiting-md.doc-md`(a flex row item)
// None of them declared overflow-wrap before T-4974, so a no-whitespace token
// set the card's min-content to the token's full width, pushed the card past a
// 390px viewport and the whole PAGE scrolled sideways. Real TaskCard + real
// tasks.css so the guard measures production layout (jsdom is blind to it).
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, mkStep, MIRA, NOOP, WORKERS } from "./taskFixtures";

const TOKEN =
  "twin(desired_state/desired_machine_id/refocus_since/bank_balance/created_ts/activated_ts)";

const LONG_TASK = mkTask({
  id: "t-fbcf",
  taskNo: "T-fbcf",
  title: "外包/worker 子系統收斂",
  status: "waiting_external",
  waitingReason: `等待 member ${TOKEN} 遷移完成`,
  description: `接 A案:00017-21 已把 outsource worker 欄位對齊 member ${TOKEN} → 合表資料形狀收斂已半完成。`,
  progressDone: 1,
  progressTotal: 2,
  steps: [
    mkStep({
      status: "in_progress",
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
        allTasks={[LONG_TASK]}
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
