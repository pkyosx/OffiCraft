// Story — Monitor §3 AI 會話 table, an outsource row whose sub-line carries a
// long free-text task TITLE (T-cf32; owner screenshot, DESKTOP: "版面太寬了 可
// 能要讓他的描述換行?"). `.mon-table td { white-space: nowrap }` (monitor.css)
// is table-wide; the outsource row's `.mon-member__sub` is the one cell fed
// unbounded owner text (a bound task's title) instead of a short role/status
// label, so it alone opts out and wraps — every OTHER column
// (machine/account/model) must stay nowrap on purpose.
//
// Real ancestor chain reproduced per frontend/CLAUDE.md's guard discipline
// (bare-mounting a card loses the `.app__main` max-width 1040 + 22px side
// padding, and an overflow can vanish under the extra room): `.app` >
// `.app__main` > `.mon-table-wrap[data-surface="sessions"]` > `.mon-table`,
// mirroring MonitorPage.tsx's real JSX for the member cell (`.mon-member` >
// `.mon-member__body` > `.mon-member__name` + `.mon-member__sub`).
//
// Two rows on purpose: a salaried MEMBER row (SENTINEL — short sub-line, its
// account column must stay nowrap) and the OUTSOURCE row under test.
//
// FIXTURE DESIGN — the title deliberately mixes two break behaviours so the CT
// spec's two CSS mutants each get pinned by a distinct part of it:
//   • a CJK sentence + spaced text  → pins `white-space: normal` (under the
//     inherited nowrap NOTHING wraps, so the whole cell overflows)
//   • an unbreakable long ascii token (no spaces) → pins
//     `overflow-wrap: anywhere` (with white-space:normal the CJK/spaces wrap
//     but this token still cannot break unless overflow-wrap allows it)
import { I18nProvider } from "../../src/i18n";
import { Avatar } from "../../src/components/Avatar";
import "../../src/components/chrome.css";
import "../../src/components/monitor.css";

/** A realistic long owner-authored task title: one unbroken line combining a
 * natural-language (CJK + spaced) part and an unbreakable ascii token — the
 * exact shape that stretched the outsource row in the owner's screenshot. */
export const LONG_TASK_TITLE =
  "重構帳務對帳流程,把批次排程改成事件驅動 " +
  "reconcileCutover2026Q3Phase2BillingImporterEventDrivenRetryAlertWebhookNotifyFullRegressionCoverageAndBackfillMigrationDoNotBreakThisSingleToken";

export function MonitorOutsourceSubWrapStory() {
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <div className="mon-table-wrap" data-surface="sessions">
            <table className="mon-table mon-table--sessions">
              <thead>
                <tr>
                  <th>成員</th>
                  <th>機器</th>
                  <th>帳號</th>
                  <th>模型</th>
                  <th>🧠</th>
                  <th>💲</th>
                </tr>
              </thead>
              <tbody>
                {/* member row — SENTINEL: short sub-line; its ordinary
                 * `.mon-table td` account column must stay nowrap, proving the
                 * fix did not blanket-loosen the table-wide rule. */}
                <tr className="mon-row--clickable" data-testid="mon-member-row">
                  <td className="mon-table__left" data-label="成員">
                    <div className="mon-member">
                      <Avatar size={34} />
                      <div className="mon-member__body">
                        <div className="mon-member__name">Eva</div>
                        <div className="mon-member__sub">
                          <span>engineer</span>
                        </div>
                      </div>
                    </div>
                  </td>
                  <td className="mon-table__left" data-label="機器">
                    mbp5
                  </td>
                  <td
                    className="mon-table__left"
                    data-label="帳號"
                    data-testid="mon-member-account"
                  >
                    eva@example.test
                  </td>
                  <td className="mon-table__left" data-label="模型">
                    <span className="mon-model">opus-4.8</span>
                  </td>
                  <td data-label="🧠">42%</td>
                  <td data-label="💲">$3.5</td>
                </tr>
                {/* outsource row — the fix under test */}
                <tr
                  className="mon-row--clickable"
                  data-testid="mon-outsource-row"
                >
                  <td className="mon-table__left" data-label="成員">
                    <div className="mon-member">
                      <Avatar size={34} />
                      <div className="mon-member__body">
                        <div className="mon-member__name">外包 · O-7</div>
                        <div
                          className="mon-member__sub"
                          data-testid="mon-outsource-sub"
                        >
                          <span>{LONG_TASK_TITLE}</span>
                        </div>
                      </div>
                    </div>
                  </td>
                  <td className="mon-table__left" data-label="機器">
                    mbp5
                  </td>
                  <td
                    className="mon-table__left"
                    data-label="帳號"
                    data-testid="mon-outsource-account"
                  >
                    pool@example.test
                  </td>
                  <td className="mon-table__left" data-label="模型">
                    <span className="mon-model">opus-4.8</span>
                  </td>
                  <td data-label="🧠">71%</td>
                  <td data-label="💲">$7</td>
                </tr>
              </tbody>
            </table>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
