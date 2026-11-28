import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, mkStep, MIRA, NOOP, WORKERS } from "./taskFixtures";

const LONG_DESC = [
  "## 背景",
  "Owner 在試站看過第一輪後回饋三件事：請示頁標籤旁的排序文字太突兀、桌面篩選跟「顯示方式」開法不一致、展開任務卡後描述直接貼成一段文字，跟周圍的字混在一起。這張卡把三件事收斂成可以實作的範圍。",
  "",
  "## 範圍",
  "- **請示頁標題**：拿掉「開卡時間（新到舊）」文字，排序只在一個地方表達。",
  "- **篩選**：桌面改成錨定在漏斗下方的下拉面板；手機維持底部抽屜。",
  "- **任務卡**：任務描述與工作流程各自成一個可收合的區塊，和步驟卡、請示卡分得開。",
  "",
  "## 驗收條件（AC）",
  "1. 390px 寬度下沒有水平捲動（`scrollWidth === 390`）。",
  "2. 每一個事實在畫面上只出現一次（排序不重複顯示）。",
  "3. 篩選面板與「顯示方式」用同一組圓角、陰影、邊框 token。",
  "4. 描述預設只顯示前幾行，可展開全文；兩個區塊都能從標題列收合。",
  "",
  "## 不做",
  "- 不改篩選條件本身，也不動 API。",
  "- 描述維持唯讀。",
].join("\n");

const DESCRIPTIONS = {
  long: LONG_DESC,
  short: "只有一行的描述。",
  paragraph: "這張卡要把週報的彙整流程整理成可以重複執行的步驟。".repeat(24),
  joined: "第一行\n第二行\n第三行",
};

export type StoryDescription = keyof typeof DESCRIPTIONS;

const STEPS = [
  mkStep({ name: "收集本週各任務進度", status: "done", dod: "每位成員的進度都有一句話摘要", startedTs: 1000, finishedTs: 1060 }),
  mkStep({ name: "草擬週報", status: "in_progress", dod: "週報草稿放進共享文件", startedTs: 1060 }),
  mkStep({ name: "請 Owner 過目", status: "pending", dod: "Owner 看過且沒有修改意見" }),
  mkStep({ name: "發佈週報", status: "pending", dod: "週報發佈到共享文件區" }),
];

export function TaskCardSectionsStory({ description }: { description: StoryDescription }) {
  const task = mkTask({
    id: "t-0048a0000002",
    taskNo: "t-0048a0000002",
    title: "第 48 週週報彙整與發佈",
    description: DESCRIPTIONS[description],
    progressDone: 1,
    progressTotal: 4,
    steps: STEPS,
  });
  return (
    <I18nProvider>
      <TaskCard
        task={task}
        members={[MIRA]}
        workers={WORKERS}
        nowTs={8000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onSendMessage={NOOP as never}
        onReassign={NOOP as never}
        onHydrate={(async () => task) as never}
      />
    </I18nProvider>
  );
}
