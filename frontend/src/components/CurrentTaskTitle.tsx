// CurrentTaskTitle — the SINGLE place that renders a worker's "current task
// title" line (T-3451). One component so the surfaces that show it (the
// Outsource roster row, the Outsource chat header, the outsource detail card,
// the reply card's worker line) can never drift in truncation, hover, or
// empty-state behaviour.
//
// `clamp` gives the 2-line ellipsis (-webkit-line-clamp:2); every shape carries
// the full text on the native `title` tooltip (owner: 超出「…」截斷, hover 顯示
// 全文). Without `clamp` the title renders in full.
//
// Empty state (owner: 沒有的顯示適當空狀態): a member may hold no open task. The
// roster row shows a muted placeholder so the slot reads "no current task" (not
// blank chrome); the header passes showEmpty={false} to render nothing rather
// than clutter the peer bar with a "no task" line.

import { useI18n } from "../i18n";

export function CurrentTaskTitle({
  title,
  clamp,
  showEmpty = true,
  testid,
}: {
  /** The task's REAL title (never the type / manual display name); "" ⇒ no
   * current task. */
  title: string;
  /** 2-line clamp; false renders the full title. */
  clamp: boolean;
  /** Render the muted placeholder when there is no task (roster rows) vs render
   * nothing (chat header). Default true. */
  showEmpty?: boolean;
  /** data-testid for the CT guards. */
  testid?: string;
}) {
  const { t } = useI18n();

  if (!title) {
    if (!showEmpty) return null;
    return (
      <span
        className="current-task-title current-task-title--empty"
        data-testid={testid}
      >
        {t.office.noCurrentTask}
      </span>
    );
  }

  return (
    <span
      className={`current-task-title${clamp ? " current-task-title--clamp" : ""}`}
      title={title}
      data-testid={testid}
    >
      {title}
    </span>
  );
}
