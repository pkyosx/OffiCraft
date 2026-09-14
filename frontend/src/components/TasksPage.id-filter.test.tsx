// 任務頁 — the ID 篩選 field (T-93, T-118).
//
// Every applied condition ANDs, the id included (owner option ①). An applied id
// is answered by `GET /api/tasks/{id}`, never by filtering the rows the page has
// already loaded — filtering loaded rows made a real task the page had not
// downloaded (a 已完成 one, under the default status filter) look the same as an
// id that names nothing. Endings once an id is applied:
//   · 404, or found but excluded by another applied axis → the ordinary
//     `tasks-empty-filtered`, never `tasks-empty`.
//   · found and passes → that one row.
//   · any other failure (500 / offline) is NOT a miss: nothing was reached, so
//     nothing may be claimed.
// Typing asks the server nothing; the draft id commits (and asks exactly once)
// on Enter or blur. Matching is exact, not substring.

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
    executorKind: "staff",
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
    __injectMockTask(mkTask({ id: "t-aaa1" }));
    const { findByTestId } = renderPage();
    expect(await findByTestId("filter-task-id")).toBeTruthy();
    expect(await findByTestId("filter-executor")).toBeTruthy();
    expect(await findByTestId("filter-type")).toBeTruthy();
    expect(await findByTestId("filter-status")).toBeTruthy();
  });

  it("🔴 the filter row is NOT a modal: it is page content, with no scrim over the list", async () => {
    // owner c-3b5a0aa66550:「按搜尋時不要再跳出新modal」.
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
    // 「按enter或是點外面就視為apply了」. The dropdowns are immediate, so the id
    // field looks like the odd one out — but every commit is a server request.
    __injectMockTask(mkTask({ id: "t-aaa1", title: "第一張" }));
    __injectMockTask(mkTask({ id: "t-bbb2", title: "第二張" }));
    const { queryByText } = renderPage();
    await waitFor(() => expect(queryByText("第一張")).toBeTruthy());

    typeIdFilter("t-aaa1");
    // 🔴 WAIT ON A REAL CLOCK, NOT ONE MICROTASK: a microtask cannot observe a
    // `setTimeout`-debounced per-keystroke apply. Anything that eventually
    // applies without Enter or blur has to land inside 600ms.
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

  it("a dropdown takes effect on the click", async () => {
    __injectMockTask(
      mkTask({ id: "t-aaa1", title: "第一張", status: "in_progress" })
    );
    __injectMockTask(
      mkTask({ id: "t-bbb2", title: "第二張", status: "not_started" })
    );
    const { queryByText } = renderPage();
    await waitFor(() => expect(queryByText("第一張")).toBeTruthy());
    expect(queryByText("第二張")).toBeTruthy();

    // Untick 進行中; the click IS the condition.
    toggleFilter("filter-status", "in_progress");
    await waitFor(() => expect(queryByText("第一張")).toBeNull());
    expect(queryByText("第二張"), "only the unticked axis narrows").toBeTruthy();
  });

  it("清除篩選 is in the field row from the first render, and the id field shows the applied id", async () => {
    // owner 2026-09-06 c-2423dba8b65b:「清除篩選還是要留著」. The default status
    // set already narrows, so it is present from the first render.
    __injectMockTask(mkTask({ id: "t-aaa1" }));
    const { findByTestId, queryByTestId } = renderPage();
    await findByTestId("filter-task-id");
    expect(queryByTestId("tasks-filter-clear")).not.toBeNull();

    applyIdFilter("t-aaa1");
    await waitFor(() =>
      expect(
        (document.querySelector(
          '[data-testid="filter-task-id"]'
        ) as HTMLInputElement).value
      ).toBe("t-aaa1")
    );
  });
});

describe("任務頁 ID 篩選 — 三種結局 (owner 2026-09-06 選項①)", () => {
  // A 404 and a found-but-excluded task render the same ordinary filtered-empty
  // screen; the count shows the filtered number (owner rc-f603bbd447f4). What
  // these specs hold: the id is answered by the SERVER; an excluded row really is
  // excluded; the page still says something rather than going blank; 「no answer
  // yet」 (in flight / never returned) says nothing at all.
  it("① 404 → the ordinary empty result, not a blank page", async () => {
    __injectMockTask(mkTask({ id: "t-real" }));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const { findByTestId, queryByTestId } = renderPage();
    await waitFor(() => expect(queryByTestId("tasks-empty")).toBeNull());

    applyIdFilter("t-nope");

    // The ordinary filtered-empty message — the same one every other filter
    // gets.
    await findByTestId("tasks-empty-filtered");
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
    const { findByTestId, queryByText } = renderPage();
    await waitFor(() => expect(queryByText("還在跑")).toBeTruthy());

    // The default 狀態 set excludes terminals, so this id is real but filtered.
    applyIdFilter("t-closed");

    await findByTestId("tasks-empty-filtered");
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
    // 「一個字元一個請求」 is what this guards. A COMMITTED id costs exactly one
    // request; 「按enter或是點外面就視為apply了」 is a rule about when it may leave.
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
    __injectMockTask(mkTask({ id: "t-real" }));
    const { findByTestId } = renderPage();

    applyIdFilter("t-real");
    await findByTestId("filter-task-id");
    clearAllFilters();

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

    // The hash seeds the APPLIED id, and the always-visible field is where that
    // is readable.
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
    // This asserts the
    // MECHANISM (a ch-based count the caller supplies), not a pixel count —
    // jsdom computes no layout, so a pixel assertion here would be theatre.
    // The real geometry is measured by the CT guard in visual-guards/.
    //
    //   · the NUMBER: owner `rc-b2beb7b1fd3c` 「ID寬度要合理…任務可先假設到萬位數」
    //     ⇒ 「T-」 + 5 digits = 7.
    //   · the ELEMENT: owner `rc-e2edbb0fff01` 「寬度取編號跟標籤的較大者」. The
    //     count rides the WRAPPER, which hands it to a hidden copy of the label
    //     (the placeholder) as a `min-width`; the browser takes the max.
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
    // 🔴 AND THE INPUT MUST NOT BRING A WIDTH OF ITS OWN. An <input> with no
    // `size` carries a UA default of 20 characters, and that is an INTRINSIC
    // width — during the grid's intrinsic sizing pass a percentage width
    // behaves as `auto`, so the column would come out at max(sizer, 20 chars)
    // and the default would win nearly every time. The count above would go
    // dead while every assertion in this file stayed green: that is not a
    // hypothetical, it shipped in 9d2bc496 and the CT guard 「the field is
    // sized to the id it holds」 is what caught it.
    expect(input.getAttribute("size"), "the input must not size itself").toBe(
      "1"
    );
  });
});
