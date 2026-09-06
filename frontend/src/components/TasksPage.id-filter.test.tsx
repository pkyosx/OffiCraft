// 任務頁 — the ID 篩選 field, third pass (T-93).
//
// WHY THIS FILE EXISTS. Round 1 shipped the field on 請示卡頁 only; owner opened
// the trial station and answered 「任務沒有出現同樣的filter, 而且好像太寬了」, so
// round 2 added it here. Round 3 is the owner rejecting the SHAPE of both:
//
//   「很常我們要找一張任務或票而已，但每次都要全部都撈回來才濾不合理 你可以設計成
//     要給完搜尋條件要再按 search 的版本嗎」            (rejected the 篩選列)
//   「按搜尋時不要再跳出新modal」                        (rejected the overlay)
//   「一起搬」                    (ALL the dropdowns move into the panel too)
//   and, on the id itself, option ①: EVERY CONDITION ANDS, THE ID INCLUDED.
//
// 🔴 WHAT ROUND 2 GOT WRONG, AND WHAT THESE SPECS NOW EXIST TO PREVENT. The
// field filtered the ALREADY-LOADED rows by substring. So an id that named a
// real task the page had not downloaded (a 已完成 one, under the default status
// filter) produced the same screen as an id that names nothing at all —
// 沒有符合篩選條件的任務, both times. The owner read a live task as a deleted one
// in review. Under option ① the id is answered by `GET /api/tasks/{id}` and the
// page has THREE separate endings, pinned separately below:
//   · 404              → 「不存在」, and it says the server was asked.
//   · found, fails the other applied axes → says so, NAMES them, and offers
//     「只用編號再找一次」.
//   · found and passes → that one row.
// A fourth, non-404 failure, is NOT a miss: nothing was reached, so nothing may
// be claimed.
//
// ⚠️ TWO ASSERTIONS FROM ROUND 2 ARE DELIBERATELY GONE, because the owner
// overruled the behaviour they pinned:
//   · 「typing NEVER asks the server」 — typing still asks nothing (the fetch
//     rides the APPLIED id), but pressing 套用篩選 now DOES ask, exactly once.
//     The spec is rewritten to that rule rather than deleted; the must-fix it
//     came from (「一個字元一個請求」) is still what it guards.
//   · SUBSTRING matching — `GET /api/tasks/{id}` answers about one id, so the
//     field is exact-match now. A half-typed id names no task, which is the
//     same thing the substring rule was hand-waving at.

import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { __resetMock, __injectMockTask } from "../api/mock";
import { api } from "../api";
import type { TaskView } from "../api/adapter";
import {
  openFilterPanel,
  applyFilters,
  cancelFilters,
  applyIdFilter,
  toggleFilter,
  clearAllFilters,
} from "../test/tasksFilter";

let seq = 0;
// The SAME fixture shape TasksPage.test.tsx uses — copied rather than invented,
// because a hand-rolled TaskView that omits a field TaskCard reads crashes the
// render, and a crashed render fails for a reason that has nothing to do with
// what these specs are about.
function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${1000 + seq}`,
    title: `任務 ${seq}`,
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "member",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: Date.now() / 1000 - 3600,
    updatedTs: Date.now() / 1000 - 60,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
}

function renderPage() {
  return render(
    <I18nProvider>
      <TasksPage />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  seq = 0;
  window.location.hash = "";
});
afterEach(() => {
  vi.restoreAllMocks();
});

describe("任務頁 篩選面板 (T-93 round 3)", () => {
  it("the fields are BEHIND the funnel — nothing filter-shaped is on the page until it is opened", async () => {
    // 「一起搬」: the id field AND all three dropdowns live inside the panel, so
    // a collapsed page shows the funnel and the 已篩選 strip and nothing else.
    __injectMockTask(mkTask({ id: "t-aaa1" }));
    const { findByTestId, queryByTestId } = renderPage();
    await findByTestId("tasks-filter-toggle");
    expect(queryByTestId("tasks-filter-form")).toBeNull();
    expect(queryByTestId("filter-task-id")).toBeNull();
    expect(queryByTestId("filter-executor")).toBeNull();
    expect(queryByTestId("filter-type")).toBeNull();
    expect(queryByTestId("filter-status")).toBeNull();

    openFilterPanel();
    expect(await findByTestId("tasks-filter-form")).toBeTruthy();
    expect(await findByTestId("filter-task-id")).toBeTruthy();
    expect(await findByTestId("filter-executor")).toBeTruthy();
    expect(await findByTestId("filter-type")).toBeTruthy();
    expect(await findByTestId("filter-status")).toBeTruthy();
  });

  it("🔴 the panel is NOT a modal: it is page content, with no scrim over the list", async () => {
    // owner c-3b5a0aa66550:「按搜尋時不要再跳出新modal」. The list must still be
    // in the document while the panel is open, and nothing may cover it.
    __injectMockTask(mkTask({ id: "t-aaa1", title: "還看得見我" }));
    const { findByText, queryByRole } = renderPage();
    await findByText("還看得見我");
    openFilterPanel();
    expect(await findByText("還看得見我")).toBeTruthy();
    expect(queryByRole("dialog")).toBeNull();
    const form = document.querySelector<HTMLElement>(
      '[data-testid="tasks-filter-form"]'
    )!;
    // Asserted on the RULE, not on geometry (jsdom lays nothing out): the shell
    // must not have grown a fixed box or a stacking context.
    expect(getComputedStyle(form).position).not.toBe("fixed");
  });

  it("🔴 typing changes NOTHING until 套用篩選 — and 取消 throws the draft away", async () => {
    // The answer to 「每次都要全部都撈回來才濾不合理」. Round 2 filtered on every
    // keystroke; this asserts the opposite rule.
    __injectMockTask(mkTask({ id: "t-aaa1", title: "第一張" }));
    __injectMockTask(mkTask({ id: "t-bbb2", title: "第二張" }));
    const { findByTestId, queryByText } = renderPage();
    await waitFor(() => expect(queryByText("第一張")).toBeTruthy());

    openFilterPanel();
    const field = (await findByTestId("filter-task-id")) as HTMLInputElement;
    fireEvent.change(field, { target: { value: "t-aaa1" } });
    // Still BOTH on screen: a draft narrows nothing.
    expect(queryByText("第一張")).toBeTruthy();
    expect(queryByText("第二張")).toBeTruthy();

    cancelFilters();
    // Cancel committed nothing, and it closed the panel (「按了面板就又會再消失」).
    expect(queryByText("第二張")).toBeTruthy();
    await waitFor(() =>
      expect(document.querySelector('[data-testid="tasks-filter-form"]')).toBeNull()
    );
    // Reopening shows the APPLIED truth, not the cancelled draft.
    openFilterPanel();
    expect(
      ((await findByTestId("filter-task-id")) as HTMLInputElement).value
    ).toBe("");
  });

  it("套用篩選 narrows to the one row, and the panel closes itself", async () => {
    __injectMockTask(mkTask({ id: "t-aaa1", title: "第一張" }));
    __injectMockTask(mkTask({ id: "t-bbb2", title: "第二張" }));
    const { queryByText } = renderPage();
    await waitFor(() => expect(queryByText("第二張")).toBeTruthy());

    applyIdFilter("t-aaa1");
    await waitFor(() => expect(queryByText("第二張")).toBeNull());
    expect(queryByText("第一張")).toBeTruthy();
    expect(
      document.querySelector('[data-testid="tasks-filter-form"]')
    ).toBeNull();
  });

  it("the applied filters stay readable while the panel is shut (已篩選 strip)", async () => {
    // The strip is what stops a collapsed panel from hiding a live filter. The
    // DEFAULT view is already filtered (terminals excluded), so it is there from
    // the first render — and after an id is applied it names the id too.
    __injectMockTask(mkTask({ id: "t-aaa1" }));
    const { findByTestId } = renderPage();
    const summary = await findByTestId("tasks-filter-summary");
    expect(summary.textContent).toContain("狀態");

    applyIdFilter("t-aaa1");
    await waitFor(() =>
      expect(
        document.querySelector('[data-testid="tasks-filter-summary"]')
          ?.textContent
      ).toContain("編號：t-aaa1")
    );
  });

  it("a chip's × drops JUST that axis and re-runs at once", async () => {
    __injectMockTask(mkTask({ id: "t-aaa1", title: "第一張" }));
    __injectMockTask(mkTask({ id: "t-bbb2", title: "第二張" }));
    const { queryByText, findAllByTestId } = renderPage();
    await waitFor(() => expect(queryByText("第二張")).toBeTruthy());

    applyIdFilter("t-aaa1");
    await waitFor(() => expect(queryByText("第二張")).toBeNull());

    // Two chips now: 編號 and 狀態. Remove the id one; 狀態 must survive.
    const chips = await findAllByTestId("tasks-filter-chip");
    const idChip = chips.find((c) => c.textContent?.includes("編號"))!;
    fireEvent.click(idChip.querySelector("button")!);

    await waitFor(() => expect(queryByText("第二張")).toBeTruthy());
    const summary = document.querySelector(
      '[data-testid="tasks-filter-summary"]'
    );
    expect(summary?.textContent).not.toContain("編號");
    expect(summary?.textContent).toContain("狀態");
  });
});

describe("任務頁 ID 篩選 — 三種結局 (owner 2026-09-06 選項①)", () => {
  it("① 404 → 「不存在」, and it says the SERVER was asked", async () => {
    // 🔴 The distinction the whole ticket is about. This may not read as
    // 「還沒載進來」 and may not borrow the generic filtered-empty sentence.
    __injectMockTask(mkTask({ id: "t-real" }));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const { findByTestId, queryByTestId } = renderPage();
    await waitFor(() => expect(queryByTestId("tasks-empty")).toBeNull());

    applyIdFilter("t-nope");

    const missing = await findByTestId("task-id-missing");
    expect(missing.textContent).toContain("t-nope");
    expect(missing.textContent).toContain("跟伺服器要過");
    expect(missing.textContent).toContain("不存在");
    expect(queryByTestId("tasks-empty-filtered")).toBeNull();
    expect(queryByTestId("tasks-empty")).toBeNull();
    expect(queryByTestId("task-id-filtered")).toBeNull();
  });

  it("② found but blocked by ANOTHER axis → says so, NAMES the axis, and offers 只用編號再找一次", async () => {
    // 🔴 THE STATE THE TICKET EXISTS FOR. Round 2 rendered this identically to
    // ①. Here the task is real and the server returned it — it is the 狀態
    // condition that is hiding it, and the page has to say that and nothing
    // else.
    __injectMockTask(mkTask({ id: "t-live", title: "還在跑" }));
    __injectMockTask(
      mkTask({
        id: "t-closed",
        title: "收工了",
        status: "done",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    const { findByTestId, queryByTestId, queryByText } = renderPage();
    await waitFor(() => expect(queryByText("還在跑")).toBeTruthy());

    // The default 狀態 set excludes terminals, so this id is real but filtered.
    applyIdFilter("t-closed");

    const blocked = await findByTestId("task-id-filtered");
    expect(blocked.textContent).toContain("t-closed");
    expect(blocked.textContent).toContain("狀態"); // the axis, named
    // It must NOT collapse into either of the other two answers.
    expect(queryByTestId("task-id-missing")).toBeNull();
    expect(queryByTestId("tasks-empty-filtered")).toBeNull();
    // The card itself is not shown — every condition ANDs, so it really is out.
    expect(queryByText("收工了")).toBeNull();

    // …and the exit works: drop the other axes, keep the id, and there it is.
    fireEvent.click(await findByTestId("task-id-only"));
    await waitFor(() => expect(queryByText("收工了")).toBeTruthy());
    expect(queryByTestId("task-id-filtered")).toBeNull();
  });

  it("② names the 負責人 axis when THAT is the one blocking it", async () => {
    // Non-vacuity control for the 狀態 case: the notice reports which condition
    // actually blocked the row, not a fixed word.
    __injectMockTask(mkTask({ id: "t-kyle", title: "凱爾的", executorId: "kyle" }));
    __injectMockTask(mkTask({ id: "t-mira", title: "米菈的", executorId: "mira" }));
    const { findByTestId, queryByText } = renderPage();
    await waitFor(() => expect(queryByText("凱爾的")).toBeTruthy());

    toggleFilter("filter-executor", "mira");
    await waitFor(() => expect(queryByText("凱爾的")).toBeNull());

    applyIdFilter("t-kyle");
    const blocked = await findByTestId("task-id-filtered");
    expect(blocked.textContent).toContain("負責人");
    expect(blocked.textContent).not.toContain("狀態");
  });

  it("③ found and passing every other condition → that ONE row is the list", async () => {
    __injectMockTask(mkTask({ id: "t-aaa1", title: "第一張" }));
    __injectMockTask(mkTask({ id: "t-bbb2", title: "第二張" }));
    const { queryByText, queryByTestId } = renderPage();
    await waitFor(() => expect(queryByText("第二張")).toBeTruthy());

    applyIdFilter("t-aaa1");
    await waitFor(() => expect(queryByText("第二張")).toBeNull());
    expect(queryByText("第一張")).toBeTruthy();
    expect(queryByTestId("task-id-missing")).toBeNull();
    expect(queryByTestId("task-id-filtered")).toBeNull();
  });

  it("a NON-404 failure is not a miss: it says the server was never reached", async () => {
    // 500 / offline. 「找不到」 would be an answer to a question that never got
    // asked, and the owner would read a broken server as a deleted task.
    __injectMockTask(mkTask({ id: "t-real" }));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(api, "getTask").mockRejectedValue(new Error("boom"));
    const { findByTestId, queryByTestId } = renderPage();

    applyIdFilter("t-real");

    const err = await findByTestId("tasks-error");
    expect(err.textContent).toContain("沒有得到伺服器的回覆");
    // It may only mention 找不到 to DENY it — never as the verdict.
    expect(err.textContent).toContain("這不是「找不到」");
    // No answer of any other kind may be on screen.
    expect(queryByTestId("task-id-missing")).toBeNull();
    expect(queryByTestId("task-id-filtered")).toBeNull();
    expect(queryByTestId("tasks-empty-filtered")).toBeNull();
    expect(queryByTestId("tasks-empty")).toBeNull();
  });

  it("🔴 typing asks the server NOTHING; 套用篩選 asks exactly once", async () => {
    // The rewritten form of round 2's 「typing NEVER asks the server」. The
    // must-fix it came from (an independent review's 「一個字元一個請求」 on
    // 請示卡頁) is still the thing guarded — what changed is that a COMMITTED id
    // is now allowed to cost exactly one request, which is what makes ① and ②
    // distinguishable at all.
    const spy = vi.spyOn(api, "getTask");
    __injectMockTask(mkTask({ id: "t-abcdef" }));
    const { findByTestId } = renderPage();

    openFilterPanel();
    const field = await findByTestId("filter-task-id");
    for (const v of ["t", "t-", "t-a", "t-ab", "t-abc", "t-abcdef"]) {
      fireEvent.change(field, { target: { value: v } });
    }
    await waitFor(() =>
      expect((field as HTMLInputElement).value).toBe("t-abcdef")
    );
    expect(spy, "six keystrokes must cost zero requests").not.toHaveBeenCalled();

    applyFilters();
    await waitFor(() => expect(spy).toHaveBeenCalledWith("t-abcdef"));
    // ⚠️ NOT asserted as "exactly one call ever": the located card expands
    // itself (TaskCard's `located` effect) and hydrates its own detail through
    // the same `api.getTask`, so a second read arrives for a reason that has
    // nothing to do with filtering. What the filter must not do is ask PER
    // KEYSTROKE, and that is the assertion above this one.
  });
});

describe("任務頁 ID 篩選 — 清除與 hash", () => {
  it("清除全部 empties the field and every other axis", async () => {
    __injectMockTask(mkTask({ id: "t-real" }));
    const { findByTestId } = renderPage();

    applyIdFilter("t-real");
    await findByTestId("tasks-filter-clear");
    clearAllFilters();

    // The strip is gone entirely — nothing narrows any more.
    await waitFor(() =>
      expect(
        document.querySelector('[data-testid="tasks-filter-summary"]')
      ).toBeNull()
    );
    expect(window.location.hash).toBe("");
    openFilterPanel();
    expect(
      ((await findByTestId("filter-task-id")) as HTMLInputElement).value
    ).toBe("");
  });

  it("清除全部 also drops the id from the URL when the hash seeded it", async () => {
    // Without this the field clears, the list widens, and a reload seeds the
    // filter straight back — the clear looks broken to the owner.
    __injectMockTask(mkTask({ id: "t-seed" }));
    window.location.hash = "#tasks/t-seed";
    const { findByTestId } = renderPage();

    // The hash seeds the APPLIED id, so the strip names it without the panel
    // ever being opened.
    await waitFor(() =>
      expect(
        document.querySelector('[data-testid="tasks-filter-summary"]')
          ?.textContent
      ).toContain("編號：t-seed")
    );

    clearAllFilters();
    // `#tasks`, not "": clearing returns the route to the plain 任務頁 rather
    // than to the app's home. What matters is that the ID is GONE — leave it in
    // and a reload seeds the filter straight back.
    await waitFor(() => expect(window.location.hash).toBe("#tasks"));
    expect(window.location.hash).not.toContain("t-seed");
    openFilterPanel();
    expect(
      ((await findByTestId("filter-task-id")) as HTMLInputElement).value
    ).toBe("");
  });

  it("the field's width comes from the id's LENGTH, not from a literal", async () => {
    // owner 2026-09-06: the old field was a flat 200px chosen with no reference
    // to its content, which is why it read as too wide. This asserts the
    // MECHANISM (a ch-based width the caller supplies), not a pixel count —
    // jsdom computes no layout, so a pixel assertion here would be theatre.
    // The real geometry is measured by the CT guard in visual-guards/.
    const { findByTestId } = renderPage();
    openFilterPanel();
    const field = (await findByTestId("filter-task-id")) as HTMLInputElement;
    // A custom property, not a width: idFilter.css owns the box model, because
    // the field has to run `content-box` against the app's global `border-box`
    // for the count to mean the TEXT area rather than the text area minus the
    // padding. So what the component contributes is the NUMBER OF CHARACTERS.
    expect(field.style.getPropertyValue("--id-filter-ch")).toBe("10");
    expect(field.style.width, "the pixel width must NOT come from here").toBe("");
  });
});
