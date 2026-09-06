// test/tasksFilter.ts — drive the 任務頁 filter the way an owner does.
//
// WHY THIS MOVED OUT OF THE TEST FILES (T-93 round 3). Six suites used to carry
// their own copy of a two-line `toggleFilter` that clicked a dropdown on an
// always-visible 篩選列. That 篩選列 is gone: owner 2026-09-06 rejected it twice
// (「每次都要全部都撈回來才濾不合理」, then「按搜尋時不要再跳出新modal」) and every
// field now lives inside a FilterPanel that expands in the page, with nothing
// taking effect until 套用篩選. So the gesture is now three steps, not one — and
// six hand-copied versions of a three-step gesture is six chances to drift.
//
// 🔴 `applyFilters` is not decoration. The panel is a DRAFT: a test that ticks a
// box and asserts the list changed, without applying, is asserting the exact
// behaviour the owner threw out. Keep the Apply.

import { fireEvent } from "@testing-library/react";

/** Expand the 篩選 panel if it is collapsed (the funnel button on the header
 * row). Opening reseeds the draft fields from what is applied. */
export function openFilterPanel() {
  const toggle = document.querySelector('[data-testid="tasks-filter-toggle"]')!;
  if (toggle.getAttribute("aria-expanded") !== "true") {
    fireEvent.click(toggle);
  }
}

/** 套用篩選 — commit the draft. The panel closes itself. */
export function applyFilters() {
  fireEvent.click(
    document.querySelector('[data-testid="tasks-filter-apply"]')!
  );
}

/** 取消 — discard the draft. The panel closes and nothing changed. */
export function cancelFilters() {
  fireEvent.click(
    document.querySelector('[data-testid="tasks-filter-cancel"]')!
  );
}

/** Tick/untick ONE option of one multi-select axis and apply it: open the
 * panel, open the dropdown if needed, click the checkbox, press 套用篩選. */
export function toggleFilter(testId: string, value: string) {
  openFilterPanel();
  const trigger = document.querySelector(`[data-testid="${testId}"]`)!;
  if (trigger.getAttribute("aria-expanded") !== "true") {
    fireEvent.click(trigger);
  }
  const checkbox = document.querySelector(
    `[data-testid="${testId}-opt-${value}"] input`
  )!;
  fireEvent.click(checkbox);
  applyFilters();
}

/** Type an id into the panel's 任務編號 field and apply it. */
export function applyIdFilter(value: string) {
  openFilterPanel();
  fireEvent.change(document.querySelector('[data-testid="filter-task-id"]')!, {
    target: { value },
  });
  applyFilters();
}

/** 清除全部 on the 已篩選 strip — every axis back to "no constraint". */
export function clearAllFilters() {
  fireEvent.click(
    document.querySelector('[data-testid="tasks-filter-clear"]')!
  );
}
