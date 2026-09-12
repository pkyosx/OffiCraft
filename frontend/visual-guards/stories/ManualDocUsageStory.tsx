// CT story for the 「已用 / 上限」 readout on the task-manual SOP (T-100), at
// phone and desktop widths.
//
// The ancestor chain is reproduced BY CLASS, per frontend/CLAUDE.md 〈浮層寬度
// 不可用 vw 夾〉: a bare card mounted at x≈0 carries ~22px of slack it does not
// have in the app, and that slack is exactly what hides an overflow at 390px.
// Production is  .app > .app__main > .settings > (this page's own wrapper).
//
// The readout is added into `.manual-sec__head` — a flex ROW that already holds
// a number, a CJK title, a CJK hint and the block's 編輯 switch. That is the
// shape frontend/.claude/rules/css-layout-traps.md warns about: a shrinkable
// flex item whose CJK min-content is one character will fold to a second line
// and overflow a fixed-height row. jsdom applies no layout engine, so it cannot
// answer any of this — it says the span is present and stops there.

import { I18nProvider } from "../../src/i18n";
import { TaskManualDefinitionPage } from "../../src/components/TaskManualsPage";
import type { TaskManualView } from "../../src/api/adapter";

/** Realistic content: a long CJK SOP, with a size and cap in the range a real
 * station actually carries (measured 2026-09-06: 15796 / 18000). The five-digit
 * pair matters — a guard built on 「1 / 10」 would never reach the width where
 * the row runs out of room. */
const SOP = "先確認來源，再決定要不要動手。".repeat(40);

const MANUAL: TaskManualView = {
  typeKey: "tm-05f7c776d6ff",
  // A long display name, because it shares the page with the readout.
  displayName: "OffiCraft · PR 審查（含外部 PR 接待）",
  purpose: "審查一個 PR 並給出可執行的結論",
  fields: [],
  sopMd: SOP,
  assignee: null,
  updatedTs: 0,
  sopMdChars: 15796,
  sopMdCapChars: 18000,
};

export function ManualDocUsageStory({ widthPx }: { widthPx: number }) {
  return (
    <I18nProvider>
      <div className="app" style={{ width: widthPx }}>
        <div className="app__main">
          <TaskManualDefinitionPage
            manual={MANUAL}
            crumbs={[{ label: "設定" }, { label: "任務手冊" }]}
            onSave={async () => undefined}
          />
        </div>
      </div>
    </I18nProvider>
  );
}
