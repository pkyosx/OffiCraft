// 任務 page — the ID 篩選 field (T-93, second pass).
//
// WHY THIS FILE EXISTS AT ALL. The first pass shipped the field on 請示卡頁 only.
// owner opened the trial station, answered rc-44347fc49338 in free text —
// 「任務沒有出現同樣的filter, 而且好像太寬了」 — and he was right: 任務頁 had the
// hash half (a URL could seed a filter) but no field to type one into, while his
// charter said 「任務列表跟請示卡列表，是不是都可以有一個ID的filter」. Nothing was
// broken; half the ticket was missing, and every test in the suite was green.
//
// 🔴 THE TWO PATHS THESE SPECS KEEP APART. 任務頁 already had a by-id mechanism
// before this field existed, and it is NOT the same one:
//   (1) the HASH anchor `#tasks/<id>` fetches that ONE task from its own
//       endpoint and OVERRIDES the status set, so a link to a 已完成 task lands
//       even though the default view hides terminals;
//   (2) the FIELD filters what is already loaded and asks the server nothing —
//       an independent review returned 「一個字元一個請求」 as a must-fix on
//       請示卡頁, and a half-typed id names no task anyway.
// A change that quietly merged the two would keep (1)'s tests green while
// turning every keystroke into a fetch, so (2) is pinned on its own below.

import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { __resetMock, __injectMockTask } from "../api/mock";
import { api } from "../api";
import type { TaskView } from "../api/adapter";

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

describe("任務頁 ID 篩選", () => {
  it("the field EXISTS — this is the half owner found missing", async () => {
    // The plainest possible assertion, and the one that would have caught the
    // gap: the field is on the page at all. Everything below assumes it.
    const { findByTestId } = renderPage();
    expect(await findByTestId("filter-task-id")).toBeTruthy();
  });

  it("typing an id narrows the list, and it is a SUBSTRING match", async () => {
    __injectMockTask(mkTask({ id: "t-aaa1", taskNo: "T-aaa1", title: "第一張" }));
    __injectMockTask(mkTask({ id: "t-bbb2", taskNo: "T-bbb2", title: "第二張" }));
    const { findByTestId, queryByText } = renderPage();

    // Both are on screen before the filter — without this the narrowing below
    // could pass on a page that never rendered the second task at all.
    await waitFor(() => expect(queryByText("第一張")).toBeTruthy());
    expect(queryByText("第二張")).toBeTruthy();

    const field = await findByTestId("filter-task-id");
    fireEvent.change(field, { target: { value: "aaa" } });

    await waitFor(() => expect(queryByText("第二張")).toBeNull());
    expect(queryByText("第一張")).toBeTruthy();
  });

  it("matching is case-insensitive", async () => {
    __injectMockTask(mkTask({ id: "t-AbCd", taskNo: "T-AbCd", title: "混大小寫" }));
    const { findByTestId, queryByText } = renderPage();
    await waitFor(() => expect(queryByText("混大小寫")).toBeTruthy());

    fireEvent.change(await findByTestId("filter-task-id"), {
      target: { value: "ABCD" },
    });
    // Still there: a case-sensitive matcher would have hidden it.
    await waitFor(() => expect(queryByText("混大小寫")).toBeTruthy());
  });

  it("an id that matches nothing shows 沒有符合篩選條件的任務, NOT 目前沒有任務", async () => {
    // 🔴 The distinction owner complained about in the first place: a filtered
    // empty must not read as "you have no tasks". The two empty states have
    // different testids for exactly this reason.
    __injectMockTask(mkTask({ id: "t-real", taskNo: "T-real" }));
    const { findByTestId, queryByTestId } = renderPage();
    await waitFor(() => expect(queryByTestId("tasks-empty")).toBeNull());

    fireEvent.change(await findByTestId("filter-task-id"), {
      target: { value: "t-nope" },
    });

    expect(await findByTestId("tasks-empty-filtered")).toBeTruthy();
    expect(queryByTestId("tasks-empty")).toBeNull();
  });

  it("清除篩選 appears for a TYPED id with no hash, and empties the field", async () => {
    // Gate the button on the hash anchor alone and this case loses it: the
    // owner types an id, the list narrows, and nothing on screen clears it.
    __injectMockTask(mkTask({ id: "t-real", taskNo: "T-real" }));
    const { findByTestId, queryByTestId } = renderPage();

    const field = (await findByTestId("filter-task-id")) as HTMLInputElement;
    fireEvent.change(field, { target: { value: "t-re" } });

    const clear = await findByTestId("clear-filters");
    fireEvent.click(clear);
    await waitFor(() => expect(field.value).toBe(""));
    expect(window.location.hash).toBe("");
    expect(queryByTestId("tasks-empty-filtered")).toBeNull();
  });

  it("🔴 after 清除篩選 empties every OTHER axis, typing an id brings the button BACK", async () => {
    // The discriminating case, and the only one that is: 狀態 opens at
    // DEFAULT_STATUS, so `statusFilter.size > 0` is true from mount and
    // `anyFilter` is true no matter what the id clause says. Deleting the
    // `idQuery !== ""` clause therefore leaves EVERY other spec in this file
    // green (measured: 8/8 pass with it removed).
    //
    // 清除篩選 empties the status set too (所有狀態), so AFTER pressing it every
    // other axis is unconstrained — and that is the one state where the id
    // clause is load-bearing. Without it the owner types an id, the list
    // narrows, and the only control that could widen it again is gone from the
    // screen.
    __injectMockTask(mkTask({ id: "t-aaa1", taskNo: "T-aaa1", title: "第一張" }));
    __injectMockTask(mkTask({ id: "t-bbb2", taskNo: "T-bbb2", title: "第二張" }));
    const { findByTestId, queryByTestId, queryByText } = renderPage();

    // 1. Wipe every axis, so nothing but the id can make anyFilter true.
    fireEvent.click(await findByTestId("clear-filters"));
    await waitFor(() => expect(queryByTestId("clear-filters")).toBeNull());

    // 2. Type an id. The list narrows…
    const field = (await findByTestId("filter-task-id")) as HTMLInputElement;
    fireEvent.change(field, { target: { value: "aaa" } });
    await waitFor(() => expect(queryByText("第二張")).toBeNull());

    // 3. …so the way out must be back on screen.
    expect(await findByTestId("clear-filters")).toBeTruthy();
  });

  it("清除篩選 also drops the id from the URL when the hash seeded it", async () => {
    // Without this the field clears, the list widens, and a reload seeds the
    // filter straight back — the clear looks broken to the owner.
    __injectMockTask(mkTask({ id: "t-seed", taskNo: "T-seed" }));
    window.location.hash = "#tasks/t-seed";
    const { findByTestId } = renderPage();

    const field = (await findByTestId("filter-task-id")) as HTMLInputElement;
    await waitFor(() => expect(field.value).toBe("t-seed"));

    fireEvent.click(await findByTestId("clear-filters"));
    // `#tasks`, not "": clearing returns the route to the plain 任務頁 rather
    // than to the app's home. What matters is that the ID is GONE — leave it in
    // and a reload seeds the filter straight back.
    await waitFor(() => expect(window.location.hash).toBe("#tasks"));
    expect(window.location.hash).not.toContain("t-seed");
    expect(field.value).toBe("");
  });

  it("🔴 typing NEVER asks the server for a task", async () => {
    // The must-fix an independent review returned on 請示卡頁, pinned here
    // BEFORE 任務頁 can grow it. The hash path legitimately calls getTask; this
    // spec types into the field with NO hash, so any call at all is the defect.
    const spy = vi.spyOn(api, "getTask");
    __injectMockTask(mkTask({ id: "t-abcdef", taskNo: "T-abcdef" }));
    const { findByTestId } = renderPage();
    const field = await findByTestId("filter-task-id");

    for (const v of ["t", "t-", "t-a", "t-ab", "t-abc", "t-abcdef"]) {
      fireEvent.change(field, { target: { value: v } });
    }
    await waitFor(() => expect((field as HTMLInputElement).value).toBe("t-abcdef"));
    expect(spy).not.toHaveBeenCalled();
  });

  it("the field's width comes from the id's LENGTH, not from a literal", async () => {
    // owner 2026-09-06: the old field was a flat 200px chosen with no reference
    // to its content, which is why it read as too wide. This asserts the
    // MECHANISM (a ch-based width the caller supplies), not a pixel count —
    // jsdom computes no layout, so a pixel assertion here would be theatre.
    // The real geometry is measured by the CT guard in visual-guards/.
    const { findByTestId } = renderPage();
    const field = (await findByTestId("filter-task-id")) as HTMLInputElement;
    // A custom property, not a width: idFilter.css owns the box model, because
    // the field has to run `content-box` against the app's global `border-box`
    // for the count to mean the TEXT area rather than the text area minus the
    // padding. So what the component contributes is the NUMBER OF CHARACTERS.
    expect(field.style.getPropertyValue("--id-filter-ch")).toBe("10");
    expect(field.style.width, "the pixel width must NOT come from here").toBe("");
  });
});
