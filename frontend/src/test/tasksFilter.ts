// test/tasksFilter.ts — drive the 任務頁 filter the way an owner does.
//
// WHY THIS IS SHARED (T-93 round 3). Six suites used to carry their own copy of
// a two-line `toggleFilter`. One hand-copied gesture per suite is six chances to
// drift, and the gesture has changed twice since.
//
// WHAT CHANGED IN T-118. owner 2026-09-06 (c-c3d681fe05da) removed the funnel,
// the panel, and the 取消／套用篩選 pair:「不要多filter那一層了,全部拉出來」. So
// the gesture is one step again — click the box, it applies. `openFilterPanel`
// and `applyFilters` are gone; there is nothing to open and nothing to commit.
//
// 🔴 THE ID FIELD IS THE EXCEPTION AND `applyIdFilter` STILL EARNS ITS NAME.
// Typing into 任務編號 does NOT filter. It commits on Enter or blur and on
// nothing else (「按enter或是點外面就視為apply了」), because an applied id is a
// server request. A test that types into that box and asserts the list changed
// is asserting the behaviour the owner rejected — the Enter is not decoration.

import { fireEvent } from "@testing-library/react";

/** Tick/untick ONE option of one multi-select axis. It takes effect on the
 * click — there is no Apply. */
export function toggleFilter(testId: string, value: string) {
  const trigger = document.querySelector(`[data-testid="${testId}"]`)!;
  if (trigger.getAttribute("aria-expanded") !== "true") {
    fireEvent.click(trigger);
  }
  const checkbox = document.querySelector(
    `[data-testid="${testId}-opt-${value}"] input`
  )!;
  fireEvent.click(checkbox);
}

/** Type an id into 任務編號 and COMMIT it with Enter — the gesture that turns
 * typed text into a filter. Use `typeIdFilter` when you mean to type WITHOUT
 * committing (that distinction is the point of T-118's guard). */
export function applyIdFilter(value: string) {
  const field = document.querySelector('[data-testid="filter-task-id"]')!;
  fireEvent.change(field, { target: { value } });
  fireEvent.keyDown(field, { key: "Enter" });
}

/** Type into 任務編號 and STOP — no Enter, no blur, so nothing is applied. This
 * is the half of the gesture the owner asked to be inert. */
export function typeIdFilter(value: string) {
  fireEvent.change(document.querySelector('[data-testid="filter-task-id"]')!, {
    target: { value },
  });
}

/** Commit whatever is in 任務編號 by clicking away from it (「點外面」). */
export function blurIdFilter() {
  fireEvent.blur(document.querySelector('[data-testid="filter-task-id"]')!);
}

/** Every axis back to "no constraint".
 *
 * 🔴 THERE IS NO 清除全部 BUTTON ANY MORE. It lived on the 已篩選 strip and went
 * with it (T-118), so "clear everything" is no longer one control — it is the
 * ordinary gesture of emptying each field, which is what this helper performs.
 * That is deliberate rather than a gap: with every field permanently on screen,
 * each one shows its own value and clears where the reader is already looking.
 * ⚠️ IT IS N GESTURES, NOT ONE, AND EACH COSTS A FETCH. Every 狀態 untick
 * rewrites the applied set, and that axis is asked of the SERVER — so this
 * helper makes several round trips where the product's own 清除篩選 button makes
 * one. Do not use it as the basis of a request-count assertion. */
export function clearAllFilters() {
  for (const testId of ["filter-executor", "filter-type", "filter-status"]) {
    const trigger = document.querySelector(`[data-testid="${testId}"]`);
    if (!trigger) continue;
    if (trigger.getAttribute("aria-expanded") !== "true") {
      fireEvent.click(trigger);
    }
    // Untick only what is ticked — clicking a clear box would SET it.
    const boxes = document.querySelectorAll<HTMLInputElement>(
      `[data-testid^="${testId}-opt-"] input`
    );
    for (const box of boxes) {
      if (box.checked) fireEvent.click(box);
    }
    // Close it again so the next axis's popover is not rendered underneath a
    // still-open one (the page allows it, but a test reading option rows by
    // prefix would then see two axes' worth).
    if (trigger.getAttribute("aria-expanded") === "true") {
      fireEvent.click(trigger);
    }
  }
  const field = document.querySelector('[data-testid="filter-task-id"]');
  if (field && (field as HTMLInputElement).value !== "") {
    fireEvent.change(field, { target: { value: "" } });
    fireEvent.keyDown(field, { key: "Enter" });
  }
}
