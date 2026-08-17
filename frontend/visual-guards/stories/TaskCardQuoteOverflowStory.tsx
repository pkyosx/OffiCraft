// CT story (T-4aa0): an expanded TaskCard whose description carries the shape
// the owner photographed on his phone — a markdown BLOCKQUOTE of quoted CJK
// prose with inline code, plus the neighbouring surfaces (table, fenced code,
// long URL) that the same fix has to leave alone.
//
// The screenshot's own text is reproduced rather than invented: the quote he
// circled is T-9b5d's 由來 block, whose lines are 「…」-wrapped sentences mixing
// CJK with latin words and `code` spans. Real TaskCard + real tasks.css, so the
// guard measures production layout (jsdom applies no layout at all).
//
// 🔴 THE ANCESTOR CHAIN IS PART OF THE FIXTURE. Both stories mount inside
// `.app > .app__main > .tasks > .tasks__section > .tasks__list`, the chain
// TasksPage really builds, because two levels of it change what is being
// measured (T-9b37 review):
//   · `.tasks` is the element that scrolls sideways in production — the
//     document never does, so a guard watching only the page watches a surface
//     the owner's symptom does not live on.
//   · `.app__main` contributes 22px of side padding. Without it the card is
//     388px wide at a 390px viewport where production gives it 344 — a guard
//     44px LOOSER than the real screen, which is how a regression slips past a
//     green run. `css-layout-traps.md`已經寫著這一條; this story broke it twice
//     before the review made it stick.
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, mkStep, MIRA, NOOP, WORKERS } from "./taskFixtures";

function TasksShell({ task }: { task: ReturnType<typeof mkTask> }) {
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <div className="tasks">
            <section className="tasks__section">
              <div className="tasks__list">
                <TaskCard
                  task={task}
                  allTasks={[task]}
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
              </div>
            </section>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}

const DESC = [
  "## 由來（owner 2026-08-14 聊天，原話逐字）",
  "",
  "> 「你可以看出自己開機中呼叫了多少次 mcp 載入多少 context 嗎」 「我發現你一起來就吃到 20% 以上」 「我需要你處理前端規範 他就算有規範應該也要有結構 不是全部放進去一個 claude.md 應該是隨需載入」 「而且你自己可以從程式碼去看規範嗎」 「前端規範重整後 可以也順道把它結構化嗎？ 我記得 claude code 是可以有類似 path map」 「你真的改到某一塊 才需要看那一個區塊的」",
  "",
  "## 我量到的（2026-08-14，親自量）",
  "",
  "| 文件 | 字數 |",
  "| --- | --- |",
  "| `frontend/CLAUDE.md` | 90,367 |",
  "| 根 `CLAUDE.md` | 30,084 |",
  "",
  "```bash",
  "wc -m frontend/CLAUDE.md CLAUDE.md docs/dev/*.md | sort -rn | head",
  "```",
  "",
  "參考：https://example.com/a/very/long/path/that/never/breaks/anywhere/at/all/because-it-has-no-spaces-in-it-at-all",
].join("\n");

const QUOTE_TASK = mkTask({
  id: "t-4aa0f",
  taskNo: "T-4aa0",
  title: "前端開發規範要拆成有結構、隨需載入",
  status: "waiting_owner",
  description: DESC,
  progressDone: 6,
  progressTotal: 7,
  steps: [
    mkStep({
      status: "done",
      dod: "> 引用區塊也會出現在步驟的 DoD 裡，所以同一個容器要一起量。",
    }),
    mkStep({ status: "pending" }),
  ],
});

export function TaskCardQuoteOverflowStory() {
  return <TasksShell task={QUOTE_TASK} />;
}

// The same card with NO code fence at all, so only the wide table is left.
// T-9b37 measured that a wide table overflows on its own — worse than the fence
// — so "the <pre> is the culprit" was too narrow a sentence. The fix is at the
// container, which is why one change kills both; this story is what keeps that
// true, since a future fix aimed only at `pre` would leave this one overflowing
// and nothing else would notice.
const TABLE_ONLY = [
  "## 只有表格，沒有程式碼區塊",
  "",
  "> 引用區塊也在，證明它不是元凶。",
  "",
  "| 檔案 | 行數 | 位元組 | 何時載入 | 誰維護 |",
  "| --- | --- | --- | --- | --- |",
  "| `server/ocserverd/CLAUDE.md` | 559 | 233,639 | 碰 server 時 | 後端 |",
  "| `e2e_test/seven_gate/CLAUDE.md` | 865 | 75,332 | 碰該目錄時 | 驗證 |",
].join("\n");

const TABLE_TASK = mkTask({
  id: "t-4aa0t",
  taskNo: "T-4aa1",
  title: "只有寬表格、沒有程式碼區塊",
  status: "in_progress",
  description: TABLE_ONLY,
  progressDone: 1,
  progressTotal: 2,
  steps: [mkStep({ status: "pending" })],
});

export function TaskCardWideTableOverflowStory() {
  return <TasksShell task={TABLE_TASK} />;
}
