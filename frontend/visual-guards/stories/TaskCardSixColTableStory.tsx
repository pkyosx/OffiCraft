// T-e4ae recon fixture: a six-column GFM table shaped from the existing
// worker-panel-parity design data (docs/design/worker-panel-parity.md).
//
// This is intentionally table-only: no <pre> or blockquote is present, so the
// measurements isolate the table's min-content behaviour inside a real
// TaskCard ancestor chain. The rows keep the source document's mixed CJK,
// inline-code, and long explanatory-cell shapes rather than inventing a toy
// one-word table.
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

export const SIX_COL_TABLE_MD = [
  "## 六欄表格（取自 worker-panel-parity 設計表的欄位形狀）",
  "",
  "| # | 項目 | 正職有什麼 | 外包有什麼 | 差在哪 | 期望行為 |",
  "|---|------|-----------|-----------|--------|---------|",
  "| A1 | 返回鍵 | `mp__back`（共用面板畫） | 同 | 同 | 維持共用，不動 |",
  "| A6 | 任務 chip（`T-xxxx`）+ 任務類型 | 無 | 有，可點 → `#tasks/<id>` | 外包獨有 | 保留。外包的「角色」就是它綁的任務類型，這是 rail 列形的同一條裁定 |",
  "| B2 | 模型值的語意 | REPORTED（agent 開機回報的實際值） | CONFIGURED（`worker.model`，owner 意圖值） | 差 | 待裁定：外包 DTO 無 `actual_model`，模型格無法像正職那樣標「最近一次開機回報」 |",
].join("\n");

const SIX_COL_TASK = mkTask({
  id: "t-e4ae-six-col",
  taskNo: "T-e4ae",
  title: "手機寬表格六欄 recon",
  status: "in_progress",
  description: SIX_COL_TABLE_MD,
  progressDone: 1,
  progressTotal: 2,
  steps: [mkStep({ status: "pending" })],
});

export function TaskCardSixColTableStory() {
  return <TasksShell task={SIX_COL_TASK} />;
}
