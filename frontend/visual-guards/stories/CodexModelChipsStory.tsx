// Story (T-129) — the Codex 模型 quick-pick chips, in BOTH places that render
// CODEX_MODEL_OPTIONS: the 轉派 dialog and the 任務手冊 負責成員 editor.
//
// The vocabulary went from 3 slugs to 4 when gpt-6-astra was added, which is a
// layout change: four `gpt-5.6-*` chips do not fit one row inside a 390px
// phone's ~300px content column. jsdom applies no layout engine, so the whole
// question — do the four chips land on two even rows, or does one of them end
// up alone on a full-width row / squeezed until its label breaks across three
// lines — is invisible to the vitest suite.
import { I18nProvider } from "../../src/i18n";
import { TaskReassignDialog } from "../../src/components/TaskReassignDialog";
import { TaskManualHub } from "../../src/components/TaskManualsPage";
import type { TaskManualSummaryView, TaskView } from "../../src/api/adapter";

const TASK: TaskView = {
  id: "task-1",
  taskNo: "t-10015291a015",
  title: "任務",
  typeKey: "",
  description: "",
  status: "in_progress",
  priority: "mid",
  executorKind: "staff",
  executorId: "",
  creatorId: "",
  dedupeKey: "",
  deps: [],
  waitingReason: "",
  duplicateOf: "",
  createdTs: 1_780_000_000,
  updatedTs: 1_780_000_100,
  closedTs: null,
  progressDone: 0,
  progressTotal: 0,
  steps: [],
};

const MANUAL: TaskManualSummaryView = {
  typeKey: "tm-05f7c776d6ff",
  displayName: "OffiCraft · PR 審查",
  purpose: "審查一個 PR 並給出可執行的結論",
  fields: [],
  assignee: {
    kind: "outsource",
    runtime: "codex",
    model: "",
    effort: "medium",
    copies: 1,
    machine: "",
  },
  updatedTs: 0,
  sopMdChars: 0,
  sopMdCapChars: 18000,
  learningsChars: 0,
  learningsCapChars: 17000,
};

/** 轉派 dialog. The spec picks 轉外包 + Codex itself — going through the real
 * controls is what proves the chips that ship are the ones measured. */
export function ReassignCodexChipsStory() {
  return (
    <I18nProvider>
      <TaskReassignDialog
        task={TASK}
        members={[]}
        onReassign={() => Promise.resolve()}
        onClose={() => {}}
      />
    </I18nProvider>
  );
}

/** 任務手冊 › 負責成員 editor. The ancestor chain is reproduced BY CLASS
 * (.app > .app__main) per frontend/CLAUDE.md — .app__main's side padding is
 * part of the width the chips actually get. */
export function ManualCodexChipsStory({ widthPx }: { widthPx: number }) {
  return (
    <I18nProvider>
      <div className="app" style={{ width: widthPx }}>
        <div className="app__main">
          <TaskManualHub
            manual={MANUAL}
            members={[]}
            crumbs={[{ label: "設定" }, { label: "任務手冊" }]}
            onSave={() => Promise.resolve()}
            onOpenDefinition={() => {}}
            onOpenLearnings={() => {}}
          />
        </div>
      </div>
    </I18nProvider>
  );
}
