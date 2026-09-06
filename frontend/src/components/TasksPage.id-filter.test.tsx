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
  applyIdFilter,
  typeIdFilter,
  blurIdFilter,
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

describe("任務頁 篩選列 (T-118)", () => {
  it("all four fields are on the page from the first render — there is nothing to open", async () => {
    // 🔁 REPLACES 「the fields are BEHIND the funnel」. That test pinned T-93
    // round 3, where 「一起搬」 meant every axis hid behind a funnel button.
    // OVERTURNED BY owner 2026-09-06 20:07 (c-c3d681fe05da):「不要多filter那一層
    // 了,全部拉出來」. The funnel, the panel and the toggle no longer exist.
    __injectMockTask(mkTask({ id: "t-aaa1" }));
    const { findByTestId, queryByTestId } = renderPage();
    expect(await findByTestId("filter-task-id")).toBeTruthy();
    expect(await findByTestId("filter-executor")).toBeTruthy();
    expect(await findByTestId("filter-type")).toBeTruthy();
    expect(await findByTestId("filter-status")).toBeTruthy();
    // The affordances that used to gate them are gone, not merely hidden.
    expect(queryByTestId("tasks-filter-toggle")).toBeNull();
    expect(queryByTestId("tasks-filter-form")).toBeNull();
    expect(queryByTestId("tasks-filter-apply")).toBeNull();
    expect(queryByTestId("tasks-filter-cancel")).toBeNull();
  });

  it("🔴 the filter row is NOT a modal: it is page content, with no scrim over the list", async () => {
    // owner c-3b5a0aa66550:「按搜尋時不要再跳出新modal」— NOT overturned by
    // T-118; 「全部拉出來」 is the opposite of floating it. Same rule, new node.
    __injectMockTask(mkTask({ id: "t-aaa1", title: "還看得見我" }));
    const { findByText, findByTestId, queryByRole } = renderPage();
    await findByText("還看得見我");
    expect(queryByRole("dialog")).toBeNull();
    const row = await findByTestId("tasks-filter");
    // Asserted on the RULE, not on geometry (jsdom lays nothing out): the shell
    // must not have grown a fixed box or a stacking context.
    expect(getComputedStyle(row).position).not.toBe("fixed");
  });

  it("🔴 typing in 任務編號 without Enter or blur applies NOTHING — the list does not move", async () => {
    // 🆕 T-118's own guard, and the reason it exists: owner rejected a version
    // where every keystroke took effect, and 「按enter或是點外面就視為apply了」 is
    // the only timing he named this round. The dropdowns are immediate now, so
    // without this assertion the id field would look like the odd one out and
    // the next person would "fix" it back onto onChange.
    __injectMockTask(mkTask({ id: "t-aaa1", title: "第一張" }));
    __injectMockTask(mkTask({ id: "t-bbb2", title: "第二張" }));
    const { queryByText } = renderPage();
    await waitFor(() => expect(queryByText("第一張")).toBeTruthy());

    typeIdFilter("t-aaa1");
    // 🔴 WAIT ON A REAL CLOCK, NOT ONE MICROTASK. This assertion used to be
    // `await Promise.resolve()`, and an independent review broke it: a commit
    // wrapped in `setTimeout(…, 300)` — a debounced per-keystroke apply, which
    // is the very shape owner rejected, just slower — passed every guard in
    // this file. A microtask cannot observe a timer, so "nothing happened yet"
    // was being read as "nothing will happen". 600ms is twice the debounce that
    // defeated it; anything that eventually applies without Enter or blur has
    // to land inside it.
    await new Promise((r) => setTimeout(r, 600));
    expect(queryByText("第一張")).toBeTruthy();
    expect(
      queryByText("第二張"),
      "typing alone must not narrow the list — not now, not after a delay"
    ).toBeTruthy();
  });

  it("Enter applies the typed id", async () => {
    __injectMockTask(mkTask({ id: "t-aaa1", title: "第一張" }));
    __injectMockTask(mkTask({ id: "t-bbb2", title: "第二張" }));
    const { queryByText } = renderPage();
    await waitFor(() => expect(queryByText("第二張")).toBeTruthy());

    applyIdFilter("t-aaa1");
    await waitFor(() => expect(queryByText("第二張")).toBeNull());
    expect(queryByText("第一張")).toBeTruthy();
  });

  it("blur applies the typed id too (「點外面」)", async () => {
    // The second half of the owner's sentence. Enter and blur are two doors to
    // one behaviour; a version that only wired Enter passes the test above and
    // still fails him.
    __injectMockTask(mkTask({ id: "t-aaa1", title: "第一張" }));
    __injectMockTask(mkTask({ id: "t-bbb2", title: "第二張" }));
    const { queryByText } = renderPage();
    await waitFor(() => expect(queryByText("第二張")).toBeTruthy());

    typeIdFilter("t-aaa1");
    expect(queryByText("第二張")).toBeTruthy();
    blurIdFilter();
    await waitFor(() => expect(queryByText("第二張")).toBeNull());
    expect(queryByText("第一張")).toBeTruthy();
  });

  it("a dropdown takes effect on the click — there is no 套用篩選", async () => {
    // 🔁 REPLACES 「typing changes NOTHING until 套用篩選」 for the DROPDOWN half.
    // That test pinned round 3's draft/apply split; owner 2026-09-06 removed
    // both buttons, so a tick IS the condition. The id half of the old test
    // survives, strengthened, as the three tests above.
    __injectMockTask(
      mkTask({ id: "t-aaa1", title: "第一張", status: "in_progress" })
    );
    __injectMockTask(
      mkTask({ id: "t-bbb2", title: "第二張", status: "not_started" })
    );
    const { queryByText } = renderPage();
    await waitFor(() => expect(queryByText("第一張")).toBeTruthy());
    expect(queryByText("第二張")).toBeTruthy();

    // Untick 進行中. Under round 3 this changed nothing until 套用篩選 was
    // pressed; now the click IS the condition.
    toggleFilter("filter-status", "in_progress");
    await waitFor(() => expect(queryByText("第一張")).toBeNull());
    expect(queryByText("第二張"), "only the unticked axis narrows").toBeTruthy();
  });

  it("the 已篩選 strip and its chips are gone, but 清除篩選 stays", async () => {
    // 🔁 REPLACES 「the applied filters stay readable while the panel is shut」
    // and 「a chip's × drops JUST that axis」. Both pinned the summary strip,
    // whose whole job was to keep a COLLAPSED panel honest. OVERTURNED BY owner
    // 2026-09-06:「也不用再顯示14筆已篩選跟那一行」— with every field permanently
    // visible there is no collapsed state left for it to protect against.
    __injectMockTask(mkTask({ id: "t-aaa1" }));
    const { findByTestId, queryByTestId } = renderPage();
    await findByTestId("filter-task-id");
    expect(queryByTestId("tasks-filter-summary")).toBeNull();
    expect(queryByTestId("tasks-filter-chip")).toBeNull();
    // 🔴 清除篩選 IS NOT PART OF THE STRIP AND MUST STAY. I removed it with the
    // strip and owner asked for it back by name (2026-09-06 c-2423dba8b65b:
    //「清除篩選還是要留著」). It is in the FIELD ROW now. The default status set
    // already narrows, so it is present from the first render.
    expect(queryByTestId("tasks-filter-clear")).not.toBeNull();

    applyIdFilter("t-aaa1");
    await waitFor(() =>
      expect(
        (document.querySelector(
          '[data-testid="filter-task-id"]'
        ) as HTMLInputElement).value
      ).toBe("t-aaa1")
    );
    // Still no strip — and the field itself is now what says an id is applied.
    expect(queryByTestId("tasks-filter-summary")).toBeNull();
    expect(queryByTestId("tasks-filter-chip")).toBeNull();
  });

  it("the 「案件」 sub-title above the list is gone", async () => {
    // owner 2026-09-06:「也不用再顯示…跟案件那個子標了」, restated at 20:19
    // (c-38c7759e6377) for 請示卡. The nav still names the page; this row was a
    // second, redundant title sitting directly above the list.
    __injectMockTask(mkTask({ id: "t-aaa1" }));
    const { findByTestId } = renderPage();
    await findByTestId("filter-task-id");
    expect(
      document.querySelector(".filter-panel__header"),
      "the header row that carried 案件 + the funnel must not exist"
    ).toBeNull();
    expect(document.querySelector(".filter-panel__title")).toBeNull();
  });
});

describe("任務頁 ID 篩選 — 三種結局 (owner 2026-09-06 選項①)", () => {
  // 🔴 OWNER REMOVED THE TWO BY-ID NOTICES — 2026-09-06, rc-f603bbd447f4 and
  // c-2580b547d1a1 / c-a497d775aa4b / c-86c129855835. Round 3 gave this view its
  // own sentences (「找不到「X」。這是跟伺服器要過的結果…」 and 「找到了，但不符合
  // 你目前的狀態條件…只用編號再找一次」). He saw the first on the trial station:
  //   「為什麼要顯示這種東西 拿掉!」 →「UI不是本來就秀0筆了嗎」
  //   →「任務那邊也可以用同樣的方式就好,不用特別再多個顯示框」
  // The second was put to him explicitly — a task that EXISTS but is excluded by
  // another axis would read as deleted, the confusion this ticket was opened to
  // end — and he overruled it: 「不用 比數本來就是要顯示篩選過的數量」. The
  // conditions in force are on screen in the 已篩選 strip beside the count.
  //
  // SO THE TWO CASES NOW RENDER THE SAME SCREEN, AND THAT IS THE DECISION, NOT A
  // BUG. What these specs still hold is everything that decision did NOT touch:
  //   · the id is answered by the SERVER, not by filtering the loaded rows —
  //     that is what made round 2 dishonest, and it is untouched;
  //   · an excluded row really is excluded (every condition ANDs);
  //   · the page still says SOMETHING rather than going blank;
  //   · 「no answer yet」 (in flight / never returned) still says nothing at all.
  it("① 404 → the ordinary empty result, not a bespoke notice and not a blank page", async () => {
    __injectMockTask(mkTask({ id: "t-real" }));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const { findByTestId, queryByTestId } = renderPage();
    await waitFor(() => expect(queryByTestId("tasks-empty")).toBeNull());

    applyIdFilter("t-nope");

    // The ordinary filtered-empty message — the same one every other filter
    // gets. Round 3's dedicated box is gone.
    await findByTestId("tasks-empty-filtered");
    expect(queryByTestId("task-id-missing")).toBeNull();
    expect(queryByTestId("task-id-filtered")).toBeNull();
  });

  it("② a row that EXISTS but fails another axis is excluded — and says the same thing a 404 does", async () => {
    // 🔴 The non-vacuity is the second half: the task is REAL. If this spec only
    // asserted "empty", it would pass on a page that never fetched anything.
    // Dropping the blocking axis brings the row back, which is the proof.
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

    await findByTestId("tasks-empty-filtered");
    expect(queryByTestId("task-id-filtered")).toBeNull();
    expect(queryByTestId("task-id-missing")).toBeNull();
    // Every condition ANDs, so the row really is out — not merely unannounced.
    expect(queryByText("收工了")).toBeNull();

    // …and it was there all along: clear the 狀態 axis and the server's row shows.
    clearAllFilters();
    applyIdFilter("t-closed");
    await waitFor(() => expect(queryByText("收工了")).toBeTruthy());
  });

  it("② the 負責人 axis ANDs with the id the same way 狀態 does", async () => {
    // The axis-naming assertion this replaced is gone with the notice, but the
    // AND itself is not: a different axis must still be able to exclude the row.
    __injectMockTask(mkTask({ id: "t-kyle", title: "凱爾的", executorId: "kyle" }));
    __injectMockTask(mkTask({ id: "t-mira", title: "米菈的", executorId: "mira" }));
    const { findByTestId, queryByText } = renderPage();
    await waitFor(() => expect(queryByText("凱爾的")).toBeTruthy());

    toggleFilter("filter-executor", "mira");
    await waitFor(() => expect(queryByText("凱爾的")).toBeNull());

    applyIdFilter("t-kyle");
    await findByTestId("tasks-empty-filtered");
    expect(queryByText("凱爾的")).toBeNull();

    // Non-vacuity: drop the 負責人 axis and the same id resolves to the row.
    clearAllFilters();
    applyIdFilter("t-kyle");
    await waitFor(() => expect(queryByText("凱爾的")).toBeTruthy());
  });

  it("③ found and passing every other condition → that ONE row is the list", async () => {
    __injectMockTask(mkTask({ id: "t-aaa1", title: "第一張" }));
    __injectMockTask(mkTask({ id: "t-bbb2", title: "第二張" }));
    const { queryByText, queryByTestId } = renderPage();
    await waitFor(() => expect(queryByText("第二張")).toBeTruthy());

    applyIdFilter("t-aaa1");
    await waitFor(() => expect(queryByText("第二張")).toBeNull());
    expect(queryByText("第一張")).toBeTruthy();
    expect(queryByTestId("tasks-empty-filtered")).toBeNull();
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
    // No answer of any other kind may be on screen. 🔴 tasks-empty-filtered is
    // the one that matters now that a 404 renders it: an unreached server must
    // NOT borrow the sentence a real 「0 筆」 uses, or a broken server reads as a
    // deleted task — the exact confusion this ticket exists to end.
    expect(queryByTestId("tasks-empty-filtered")).toBeNull();
    expect(queryByTestId("tasks-empty")).toBeNull();
  });

  it("🔴 typing asks the server NOTHING; Enter asks exactly once", async () => {
    // The rewritten form of round 2's 「typing NEVER asks the server」. The
    // must-fix it came from (an independent review's 「一個字元一個請求」 on
    // 請示卡頁) is still the thing guarded — what changed is that a COMMITTED id
    // is now allowed to cost exactly one request, which is what makes ① and ②
    // distinguishable at all.
    // T-118: the COMMIT is now Enter rather than 套用篩選. The keystroke half of
    // this assertion is untouched, and it is the half that matters — owner's
    // 「按enter或是點外面就視為apply了」 is exactly a rule about when the request
    // is allowed to leave.
    const spy = vi.spyOn(api, "getTask");
    __injectMockTask(mkTask({ id: "t-abcdef" }));
    const { findByTestId } = renderPage();

    const field = await findByTestId("filter-task-id");
    for (const v of ["t", "t-", "t-a", "t-ab", "t-abc", "t-abcdef"]) {
      fireEvent.change(field, { target: { value: v } });
    }
    await waitFor(() =>
      expect((field as HTMLInputElement).value).toBe("t-abcdef")
    );
    // Same real-clock wait as the guard above, and for the same reason: a
    // debounced commit would otherwise sit in a pending timer and this spy
    // would report zero calls for a version that does ask per keystroke.
    await new Promise((r) => setTimeout(r, 600));
    expect(
      spy,
      "six keystrokes must cost zero requests — not now, not after a delay"
    ).not.toHaveBeenCalled();

    fireEvent.keyDown(field, { key: "Enter" });
    await waitFor(() => expect(spy).toHaveBeenCalledWith("t-abcdef"));
    // ⚠️ NOT asserted as "exactly one call ever": the located card expands
    // itself (TaskCard's `located` effect) and hydrates its own detail through
    // the same `api.getTask`, so a second read arrives for a reason that has
    // nothing to do with filtering. What the filter must not do is ask PER
    // KEYSTROKE, and that is the assertion above this one.
  });
});

describe("任務頁 ID 篩選 — 清除與 hash", () => {
  it("emptying every field leaves nothing narrowing the list", async () => {
    // 🔁 WAS 「清除全部 empties the field and every other axis」. That button
    // lived on the 已篩選 strip and was REMOVED WITH IT by owner 2026-09-06
    // (「也不用再顯示14筆已篩選跟那一行」). The BEHAVIOUR it guarded is still
    // required — it just has no single control any more, so the gesture is
    // clearing each field, which is what `clearAllFilters` now performs.
    __injectMockTask(mkTask({ id: "t-real" }));
    const { findByTestId } = renderPage();

    applyIdFilter("t-real");
    await findByTestId("filter-task-id");
    clearAllFilters();

    expect(
      document.querySelector('[data-testid="tasks-filter-summary"]'),
      "there is no strip to reappear"
    ).toBeNull();
    await waitFor(() => expect(window.location.hash).toBe(""));
    expect(
      ((await findByTestId("filter-task-id")) as HTMLInputElement).value
    ).toBe("");
  });

  it("clearing the 編號 field also drops the id from the URL when the hash seeded it", async () => {
    // Without this the field clears, the list widens, and a reload seeds the
    // filter straight back — the clear looks broken to the owner.
    __injectMockTask(mkTask({ id: "t-seed" }));
    window.location.hash = "#tasks/t-seed";
    const { findByTestId } = renderPage();

    // 🔁 WAS asserted through the 已篩選 strip, which T-118 removed. The hash
    // seeds the APPLIED id, and the permanently-visible field is now where that
    // is readable — which is the property that made removing the strip safe.
    await waitFor(() =>
      expect(
        (document.querySelector(
          '[data-testid="filter-task-id"]'
        ) as HTMLInputElement).value
      ).toBe("t-seed")
    );

    clearAllFilters();
    // `#tasks`, not "": clearing returns the route to the plain 任務頁 rather
    // than to the app's home. What matters is that the ID is GONE — leave it in
    // and a reload seeds the filter straight back.
    await waitFor(() => expect(window.location.hash).toBe("#tasks"));
    expect(window.location.hash).not.toContain("t-seed");
    expect(
      ((await findByTestId("filter-task-id")) as HTMLInputElement).value
    ).toBe("");
  });

  it("the field's width comes from the id's LENGTH, not from a literal", async () => {
    // owner 2026-09-06: the old field was a flat 200px chosen with no reference
    // to its content, which is why it read as too wide. This asserts the
    // MECHANISM (a ch-based count the caller supplies), not a pixel count —
    // jsdom computes no layout, so a pixel assertion here would be theatre.
    // The real geometry is measured by the CT guard in visual-guards/.
    //
    // 🔁 THIS TEST USED TO READ THE COUNT OFF THE INPUT, AND USED TO EXPECT 10.
    // Both halves were overturned on 2026-09-07 and by the same person:
    //   · the NUMBER, by owner `rc-b2beb7b1fd3c` 「ID寬度要合理…任務可先假設到
    //     萬位數」 ⇒ 「T-」 + 5 digits = 7. The 10 it replaced was owner's own
    //     hand-set figure from the day before, not a measurement.
    //   · the ELEMENT, by owner `rc-e2edbb0fff01` 「寬度取編號跟標籤的較大者」.
    //     The label is this field's only label (it is the placeholder), and on
    //     this page it is the wider of the two, so the box can no longer be
    //     sized off the id alone. The count now rides the WRAPPER, which hands
    //     it to a hidden copy of the label as a `min-width`; the browser takes
    //     the max. Reading it off the input would assert a spec nobody holds.
    const { findByTestId } = renderPage();
    const input = (await findByTestId("filter-task-id")) as HTMLInputElement;
    const field = input.parentElement as HTMLElement;
    expect(field.style.getPropertyValue("--id-filter-ch")).toBe("7");
    expect(field.style.width, "the pixel width must NOT come from here").toBe("");
    // 🔴 The label has to be IN THE BOX for the browser to take a max at all.
    // Delete the sizer and this file still passes on the count above while the
    // placeholder silently clips — so assert the sizer carries the label, and
    // that it is hidden from anyone reading the page aloud (the input already
    // carries the same string as its aria-label; two would be read twice).
    const sizer = field.firstElementChild as HTMLElement;
    expect(sizer.textContent).toBe(input.placeholder);
    expect(sizer.getAttribute("aria-hidden")).toBe("true");
  });
});
