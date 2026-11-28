// test/tasksFilter.ts — drive the 任務頁 filter the way an owner does.
//
// WHY THIS IS SHARED (T-93 round 3). Six suites used to carry their own copy of
// a two-line `toggleFilter`. One hand-copied gesture per suite is six chances to
// drift, and the gesture has changed twice since.
//
// The fields live in the dialog the page's funnel opens (FilterPanel's
// collapsed shape), so every helper below opens it first.
//
// 🔴 THE ID FIELD IS THE EXCEPTION AND `applyIdFilter` STILL EARNS ITS NAME.
// Typing into 任務編號 does NOT filter. It commits on Enter or blur and on
// nothing else (「按enter或是點外面就視為apply了」), because an applied id is a
// server request. A test that types into that box and asserts the list changed
// is asserting the behaviour the owner rejected — the Enter is not decoration.

import { fireEvent, screen, waitFor } from "@testing-library/react";
import { expect } from "vitest";

/** Open the page's 篩選 dialog unless it is already open (any list page:
 * the funnel is `<page>-filter-toggle`). */
export function openFilters() {
  const toggle = document.querySelector('[data-testid$="-filter-toggle"]');
  if (toggle && toggle.getAttribute("aria-expanded") !== "true") {
    fireEvent.click(toggle);
  }
}

/** Tick/untick ONE option of one multi-select axis. It takes effect on the
 * click — there is no Apply. */
export function toggleFilter(testId: string, value: string) {
  openFilters();
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
  openFilters();
  const field = document.querySelector('[data-testid="filter-task-id"]')!;
  fireEvent.change(field, { target: { value } });
  fireEvent.keyDown(field, { key: "Enter" });
}

/** Type into 任務編號 and STOP — no Enter, no blur, so nothing is applied. This
 * is the half of the gesture the owner asked to be inert. */
export function typeIdFilter(value: string) {
  openFilters();
  fireEvent.change(document.querySelector('[data-testid="filter-task-id"]')!, {
    target: { value },
  });
}

/** Commit whatever is in 任務編號 by clicking away from it (「點外面」). */
export function blurIdFilter() {
  openFilters();
  fireEvent.blur(document.querySelector('[data-testid="filter-task-id"]')!);
}

/** Open the 顯示方式 panel unless it is already open; returns its 分組依據
 * select. */
export function openDisplayOptions(): HTMLSelectElement {
  const trigger = document.querySelector('[data-testid="display-options"]')!;
  if (trigger.getAttribute("aria-expanded") !== "true") {
    fireEvent.click(trigger);
  }
  return document.querySelector<HTMLSelectElement>('[data-testid="group-by"]')!;
}

/** Wait until the page has the server's answer to what it currently asks:
 * the filters, groupings and lanes are all asked of the server, so the screen
 * catches up a moment after each gesture. */
export async function waitForTasksSettled() {
  await waitFor(() => expect(document.querySelector('[aria-busy="true"]')).toBeNull());
}

/** Pick a 分組依據 mode in the 顯示方式 panel and wait for its lanes. */
export async function chooseGroupBy(
  mode: "due" | "priority" | "executor" | "type" | "status" | "none"
) {
  fireEvent.change(openDisplayOptions(), { target: { value: mode } });
  await waitForTasksSettled();
}

/** Wait until the 不分組 list holds exactly `count` cards. A 狀態 tick only
 * starts the fetch, so the closed rows it asked for land a moment later. */
export async function waitForListedCards(count: number) {
  await waitFor(() =>
    expect(
      screen
        .getByTestId("task-list")
        .querySelectorAll('[data-testid="task-card"]').length
    ).toBe(count)
  );
}

/** Expand one 分組 lane (e.g. `nodue` for 沒有到期日) unless it is already
 * open. Clicking blindly would COLLAPSE a lane that opened by default as the
 * first one. Waits for the lane to exist, since it renders only once a task is
 * in it — and, given `taskCount`, until it holds exactly that many: a closed
 * task shares its due lane with open ones, so the lane existing does not mean
 * the closed rows a status tick asked for have landed in it yet. */
export async function expandLane(key: string, taskCount?: number) {
  await screen.findByTestId(`lane-toggle-${key}`);
  if (taskCount !== undefined) {
    await waitFor(() =>
      expect(screen.getByTestId(`lane-count-${key}`).textContent).toBe(
        ` · ${taskCount}`
      )
    );
  }
  const toggle = screen.getByTestId(`lane-toggle-${key}`);
  if (toggle.getAttribute("aria-expanded") !== "true") fireEvent.click(toggle);
  await waitForTasksSettled();
}

/** Click a lane's header and wait for the rows it asked for. */
export async function toggleLane(key: string) {
  fireEvent.click(screen.getByTestId(`lane-toggle-${key}`));
  await waitForTasksSettled();
}

/** Every axis back to "no constraint".
 *
 * Empties each field one by one, unlike the product's single 清除篩選 button.
 * ⚠️ IT IS N GESTURES, NOT ONE, AND EACH COSTS A FETCH. Every 狀態 untick
 * rewrites the applied set, and that axis is asked of the SERVER — so this
 * helper makes several round trips where the product's own 清除篩選 button makes
 * one. Do not use it as the basis of a request-count assertion. */
export function clearAllFilters() {
  openFilters();
  for (const testId of ["filter-executor", "filter-type", "filter-status", "filter-due"]) {
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

/** Wait for the page's funnel, then open the 篩選 dialog. */
export async function openFiltersWhenReady() {
  await waitFor(() =>
    expect(document.querySelector('[data-testid$="-filter-toggle"]')).not.toBeNull()
  );
  openFilters();
}

/** Pick one of 請示頁's three list labels (等待回覆／稍後再看／近期已回覆). */
export async function selectRepliesTab(view: "waiting" | "snoozed" | "answered") {
  fireEvent.click(await screen.findByTestId(`replies-tab-${view}`));
}
