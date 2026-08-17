// CT story (T-e5b1): an expanded TaskCard carrying BOTH halves of the ticket.
//
// It replaces TaskCardDescEditorStory, which existed only to hold the
// description editor open — that editor is gone, and with it the layout
// problem that guard measured.
//
// What this story is built to make measurable:
//   * the card renders a description and a title with NO edit affordance,
//     because the props that used to create them no longer exist on TaskCard;
//   * two steps whose names are the SAME LENGTH, one WITH a note and one
//     WITHOUT — so a height difference between the two collapsed rows can only
//     come from the disclosure control, not from the names;
//   * the note itself is long enough that opening it is unmistakable in
//     geometry (it is what the owner called 太長了).
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, mkStep, MIRA, NOOP, WORKERS } from "./taskFixtures";

const LONG_NOTE = [
  "第 4 步做到哪:handler 已完成,conformance 三份重生一致。",
  "",
  "下一步:補負面案例(400 / 403),再跑一次 `bin/ci.sh`。",
  "",
  "風險:seed 舊資料沒有 note 欄位,列表要能容忍空值。",
].join("\n");

const TASK = mkTask({
  id: "t-e5b1",
  taskNo: "T-e5b1",
  title: "任務卡標題不可就地編輯",
  status: "in_progress",
  description:
    "這張任務要把標題／敘述的編輯入口從任務 UI 拿掉,並把步驟備註改成預設收起。",
  progressDone: 1,
  progressTotal: 2,
  steps: [
    mkStep({ id: "s-nonote", name: "無備註節點", status: "done" }),
    mkStep({
      id: "s-note",
      name: "有備註節點",
      status: "in_progress",
      note: LONG_NOTE,
    }),
  ],
});

export function TaskCardNoteDisclosureStory() {
  return (
    <I18nProvider>
      <TaskCard
        task={TASK}
        allTasks={[TASK]}
        members={[MIRA]}
        workers={WORKERS}
        nowTs={3000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onSendMessage={NOOP as never}
        onReassign={NOOP as never}
        onHydrate={(async () => TASK) as never}
      />
    </I18nProvider>
  );
}
