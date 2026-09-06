// FilterPanel — the 篩選 row both list pages wear (T-118).
//
// WHAT THE OWNER ASKED FOR, AND WHY THE SHAPE IS NOT NEGOTIABLE HERE
// ──────────────────────────────────────────────────────────────────────────
// 🔴 THIS FILE'S PREVIOUS HEADER SAID THE OPPOSITE, AND IT WAS RIGHT AT THE
// TIME. Round 3 (also 2026-09-06) put the fields behind a funnel button, made
// them a draft committed by 套用篩選, and carried a 「N 筆 · 已篩選：<chip ×> ·
// 清除全部」 strip so a collapsed panel could not hide a live filter. That
// header even warned that floating this panel would be "reversing a decision,
// not tidying CSS".
//
// owner 2026-09-06 20:07 (c-c3d681fe05da, with a screenshot of the block) then
// reversed it himself, in his own words:
//
//   「我想改一下,不要多filter那一層了,全部拉出來,而任務編號那邊就是按enter
//     或是點外面就視為apply了,然後也不用再顯示14筆已篩選跟那一行跟案件那個
//     子標了,案件跟請示卡都一樣」
//
// So the expand, both buttons, the summary strip and the 案件／請示卡 sub-title
// are gone. Four things survive from the earlier rounds and each is still
// load-bearing:
//
//   1. 🔴 IT IS STILL NOT A MODAL, and that decision was NOT reversed. owner
//      c-3b5a0aa66550:「按搜尋時不要再跳出新modal」. The fields are page content
//      sitting above the list. No scrim, no fixed positioning, no z-index.
//      「全部拉出來」 is the opposite of floating it — reaching for an overlay
//      here undoes two separate rulings at once.
//   2. 🔴 THE ID FIELD IS STILL NOT A PER-KEYSTROKE FILTER. It commits on Enter
//      or on blur, and on nothing else. That is the one timing the owner spelt
//      out this round, and it is what remains of 「每次都要全部都撈回來才濾不
//      合理」: the id changes a SERVER request (TasksPage passes it to
//      `useTasks`, RepliesPage to `api.getReplyCard`), so a fetch per keystroke
//      is exactly the shape he rejected. The dropdowns are a different case —
//      one click IS a complete condition, so they take effect at once.
//   3. THERE IS NO LONGER ANYTHING TO HIDE A FILTER. The summary strip existed
//      because a collapsed panel could narrow the list silently. With every
//      field permanently on screen, the fields themselves say what is applied —
//      that is why the strip could go without losing the property it protected.
//   4. THE FIELDS ARE THE HOST PAGE'S, passed as children, because 請示 and 任務
//      filter on different things and a shared field set would have to be
//      widened for every page that ever adopts this.
//
// What is left of this component is the ROW — its layout and its responsive
// behaviour. It deliberately holds no state at all now: with the draft/applied
// split gone from the shell, there is nothing for it to remember.

import type { ReactNode } from "react";
import "./filter-panel.css";

export function FilterPanel({
  testId = "filter-panel",
  children,
}: {
  testId?: string;
  /** The filter fields. Always visible — there is no collapsed state. */
  children: ReactNode;
}) {
  return (
    <div className="filter-panel" data-testid={testId}>
      <div className="filter-panel__fields" data-testid={`${testId}-fields`}>
        {children}
      </div>
    </div>
  );
}
