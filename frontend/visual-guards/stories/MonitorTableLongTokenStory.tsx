// Story — the monitor page's two wide tables in their PHONE card mode
// (T-d451). This is a SECOND, INDEPENDENT root cause: it is not a `.doc-md`
// surface, so the base wrap fix cannot reach it.
//
// `monitor.css`'s `@media (max-width: 720px)` block turns each row into a card
// and, to avoid a phantom scrollbar inside those cards, deliberately drops the
// desktop wrap's `overflow-x: auto`. That removes the only scroller that used
// to absorb a too-wide cell — so on a phone an unbreakable value (machine id,
// session id, model name) pushes the PAGE sideways instead. Measured before the
// fix: machines table +448px, sessions table +436px of page overflow at 375px.
//
// The markup below mirrors MonitorPage.tsx's table structure (`.mon-table-wrap`
// > `.mon-table` > `thead`/`tbody` > `tr` > `td[data-label]`, machine name cell
// = `.mon-machine-name` + `.mon-machine-id`). NOTE the honest caveat: this is a
// hand-mirrored chain, not a mounted <MonitorPage/> (that needs the API seam).
// If MonitorPage grows an ancestor this story lacks, the numbers can drift —
// the owner's phone acceptance is the backstop.
import "../../src/components/monitor.css";

/** A realistic warden machine id — no whitespace, no break opportunity. */
export const LONG_MACHINE_ID = "m-eva-m5-warden-c20ccd2eaed4f663f3c5de9a41625ab02770";
/** A model name of the kind the sessions table prints verbatim. */
export const LONG_MODEL = "claude-opus-4-8-20260715-preview-extended-thinking-256k";
/** A session/transcript id — printed as BARE TEXT in a cell, no wrapper span. */
export const LONG_SESSION =
  "session0122q5Em8AGqSCX2vn9xdgPD2caa350d12694a65bff7bc9dc2812597transcript";

export function MonitorTableLongTokenStory() {
  return (
    <div className="mon-page">
      {/* §2 機器 */}
      <div className="mon-table-wrap" data-surface="machines">
        <table className="mon-table">
          <thead>
            <tr>
              <th>機器</th>
              <th>狀態</th>
              <th>模型</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td className="mon-table__left" data-label="機器">
                <div className="mon-machine-name">
                  <span className="mon-table__strong">eva-m5</span>
                  <span className="mon-machine-id" title={LONG_MACHINE_ID}>
                    {LONG_MACHINE_ID}
                  </span>
                </div>
              </td>
              <td className="mon-table__left" data-label="狀態">
                <span className="mon-online mon-online--on">上線</span>
              </td>
              <td className="mon-table__left" data-label="模型">
                <span>{LONG_MODEL}</span>
              </td>
              {/* A BARE TEXT cell — no wrapper element. MonitorPage prints
               * several cells this way, and a `td > *` rule would miss them
               * entirely (it matches element children only). Keeping one here
               * is what forces the fix onto the cell itself. */}
              <td className="mon-table__left" data-label="工作階段">
                {LONG_SESSION}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      {/* §3 AI 會話 */}
      <div className="mon-table-wrap" data-surface="sessions">
        <table className="mon-table">
          <thead>
            <tr>
              <th>成員</th>
              <th>模型</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td className="mon-table__left" data-label="成員">
                <span className="mon-member">{LONG_MACHINE_ID}</span>
              </td>
              <td className="mon-table__left" data-label="模型">
                <span>{LONG_MODEL}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  );
}
