// 請示 ↔ 任務: the 請示 → 任務 leg. Locked here:
//   1. A TASK-derived reply card (card.task non-null) shows the 精簡任務資訊
//      row: the TYPE + 查看任務詳情 — and NEVER the task number / 識別鍵
//      (adjudicated). A pure chat ask shows nothing.
//   2. Clicking 查看任務詳情 routes to #tasks/<taskId>.
//   3. Arriving at #tasks/<id> FILTERS the list down to that one task in the
//      normal layout (a closed target auto-expands 已結束); the same 清除篩選
//      returns to the full list; an unknown id self-heals.
//   4. The chat's inline card (ChatReplyCard) carries the same task row.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { RepliesPage } from "./RepliesPage";
import { ChatReplyCard } from "./ChatReplyCard";
import {
  __resetMock,
  __injectMockTask,
  __injectMockReplyCard,
} from "../api/mock";
import type { ReplyCard, TaskView } from "../api/adapter";

let seq = 0;

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

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  seq += 1;
  return {
    id: `rc-${seq}`,
    from: "mira",
    kind: "decision",
    summary: "要現在同步到 Jira 嗎？",
    body: "",
    options: ["核可，直接同步上去", "先不要"],
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: `msg-${seq}`,
    answer: null,
    task: null,
    ...over,
  };
}

// Toggle one option in a multi-select filter dropdown (T-be18) — same helper as
// TasksPage.test.tsx: open the pill if needed, then click the option's checkbox.
function toggleFilter(testId: string, value: string) {
  const trigger = document.querySelector(`[data-testid="${testId}"]`)!;
  if (trigger.getAttribute("aria-expanded") !== "true") {
    fireEvent.click(trigger);
  }
  const checkbox = document.querySelector(
    `[data-testid="${testId}-opt-${value}"] input`
  )!;
  fireEvent.click(checkbox);
}

function renderTasks() {
  return render(
    <I18nProvider>
      <TasksPage />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

describe("請示卡的任務資訊 (RepliesPage)", () => {
  it("task-derived ask: TYPE + 查看任務詳情, never the task number", async () => {
    __injectMockReplyCard(
      mkCard({
        task: { id: "t-77", typeKey: "sync-jira", title: "同步 PROJ-1421" },
      })
    );
    const { findByTestId } = render(
      <I18nProvider>
        <RepliesPage />
      </I18nProvider>
    );
    const ref = await findByTestId("reply-task-ref");
    expect(ref.textContent).toContain("sync-jira");
    expect(ref.textContent).toContain("查看任務詳情");
    // Adjudicated: no task number / raw id leaks onto the card.
    expect(ref.textContent).not.toContain("t-77");
    expect(ref.textContent).not.toContain("T-");

    fireEvent.click(await findByTestId("reply-task-jump"));
    expect(window.location.hash).toBe("#tasks/t-77");
  });

  it("ad-hoc task ref labels as 自由代辦; a pure chat ask shows NO task row", async () => {
    __injectMockReplyCard(
      mkCard({ task: { id: "t-88", typeKey: "", title: "散事" } })
    );
    __injectMockReplyCard(mkCard({ task: null }));
    const { findAllByTestId } = render(
      <I18nProvider>
        <RepliesPage />
      </I18nProvider>
    );
    await findAllByTestId("waiting-card");
    const refs = await findAllByTestId("reply-task-ref");
    expect(refs).toHaveLength(1); // only the task-derived one
    expect(refs[0].textContent).toContain("自由代辦");
  });
});

describe("請示卡的任務資訊 (ChatReplyCard)", () => {
  it("the chat inline card carries the same type + jump", async () => {
    __injectMockReplyCard(
      mkCard({
        id: "rc-chat",
        task: { id: "t-99", typeKey: "review-pr", title: "修 PR" },
      })
    );
    const { findByTestId } = render(
      <I18nProvider>
        <ChatReplyCard replyCardId="rc-chat" fallbackSummary="…" />
      </I18nProvider>
    );
    const ref = await findByTestId("reply-task-ref");
    expect(ref.textContent).toContain("review-pr");
    fireEvent.click(await findByTestId("reply-task-jump"));
    expect(window.location.hash).toBe("#tasks/t-99");
  });
});

describe("TasksPage 單一任務 filter (#tasks/<id>)", () => {
  it("arriving at #tasks/<id> filters the list down to that one task", async () => {
    __injectMockTask(mkTask({ id: "t-open" }));
    __injectMockTask(mkTask({ id: "t-other" }));
    window.location.hash = "#tasks/t-open";

    const { findByTestId } = renderTasks();
    const openList = await findByTestId("open-list");
    expect(openList.querySelector('[data-task-id="t-open"]')).not.toBeNull();
    // The other task is filtered out; a filter being active shows 清除篩選.
    expect(document.querySelector('[data-task-id="t-other"]')).toBeNull();
    await findByTestId("clear-filters");
  });

  it("a CLOSED target shows up with 已結束 auto-expanded", async () => {
    __injectMockTask(mkTask({ id: "t-open" }));
    __injectMockTask(
      mkTask({ id: "t-closed", status: "done", closedTs: Date.now() / 1000 - 60 })
    );
    window.location.hash = "#tasks/t-closed";

    const { findByTestId } = renderTasks();
    const closedList = await findByTestId("closed-list");
    expect(closedList.querySelector('[data-task-id="t-closed"]')).not.toBeNull();
    expect(document.querySelector('[data-task-id="t-open"]')).toBeNull();
  });

  it("清除篩選 lifts the anchor AND every other axis → 顯示全部 (terminals included, T-50bb)", async () => {
    __injectMockTask(mkTask({ id: "t-open" }));
    __injectMockTask(mkTask({ id: "t-other" }));
    __injectMockTask(
      mkTask({ id: "t-done", status: "done", closedTs: Date.now() / 1000 - 60 })
    );
    window.location.hash = "#tasks/t-open";

    const { findByTestId } = renderTasks();
    fireEvent.click(await findByTestId("clear-filters"));

    await waitFor(() => expect(window.location.hash).toBe("#tasks"));
    await waitFor(() =>
      expect(document.querySelector('[data-task-id="t-other"]')).not.toBeNull()
    );
    expect(document.querySelector('[data-task-id="t-open"]')).not.toBeNull();
    // The anchor is a filter axis: clearing it goes to 顯示全部, so the done
    // task's 已結束 section is there too (the anchor visit already auto-
    // expanded it — open it only if still collapsed).
    const toggle = await findByTestId("closed-toggle");
    if (toggle.getAttribute("aria-expanded") !== "true") {
      fireEvent.click(toggle);
    }
    await waitFor(() =>
      expect(document.querySelector('[data-task-id="t-done"]')).not.toBeNull()
    );
  });

  it("an unknown/stale target self-heals (anchor stripped, full list)", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    window.location.hash = "#tasks/t-gone";
    const { findByTestId } = renderTasks();
    await waitFor(() => expect(window.location.hash).toBe("#tasks"));
    await findByTestId("open-list");
  });
});

describe("TasksPage executor seed (#tasks/executor/<id>, T-dfae)", () => {
  // 聊天 header 的任務圖示 lands here. Owner's words: 「連到任務頁面,並且直接
  // filter 該負責的任務還沒完成的任務」— so BOTH halves of that promise get a
  // test: the executor half narrows to that member, the 還沒完成 half keeps
  // terminals out. The 還沒完成 half is the fragile one — it rides on
  // DEFAULT_STATUS, which is also the mount-time default, so it would pass on a
  // seed that never touched the status axis at all. The stale-filter test below
  // is the one that can actually tell those two apart.

  it("arriving at #tasks/executor/<id> narrows to that member's UNFINISHED tasks", async () => {
    __injectMockTask(mkTask({ id: "t-mira-open", executorId: "mira" }));
    __injectMockTask(
      mkTask({
        id: "t-mira-done",
        executorId: "mira",
        status: "done",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    __injectMockTask(mkTask({ id: "t-kyle-open", executorId: "kyle" }));
    window.location.hash = "#tasks/executor/mira";

    const { findByTestId, queryByTestId } = renderTasks();
    const openList = await findByTestId("open-list");
    // executor half: mira's task is here, kyle's is not.
    expect(openList.querySelector('[data-task-id="t-mira-open"]')).not.toBeNull();
    expect(document.querySelector('[data-task-id="t-kyle-open"]')).toBeNull();
    // 還沒完成 half: mira's DONE task is nowhere on the page. Stronger than
    // "已結束 stays collapsed" — the status filter removes terminals from the
    // filtered set, so the 已結束 section does not render at all (it is gated
    // on closed.length > 0 over the FILTERED list). Contrast the #tasks/<id>
    // anchor, which deliberately auto-expands 已結束 to guarantee visibility;
    // this seed promises the opposite.
    expect(queryByTestId("closed-toggle")).toBeNull();
    expect(document.querySelector('[data-task-id="t-mira-done"]')).toBeNull();
  });

  it("the seed is ONE-SHOT: the hash normalises back to #tasks and the filters stay editable", async () => {
    __injectMockTask(mkTask({ id: "t-mira-open", executorId: "mira" }));
    __injectMockTask(mkTask({ id: "t-kyle-open", executorId: "kyle" }));
    window.location.hash = "#tasks/executor/mira";

    const { findByTestId } = renderTasks();
    // composeTaskNo precedent: consumed, then normalised away — so the route
    // never re-imposes itself over the owner's own filter edits.
    await waitFor(() => expect(window.location.hash).toBe("#tasks"));
    // The seeded filter is ordinary filter state: 清除篩選 lifts it like any
    // other axis, and kyle's task comes back.
    fireEvent.click(await findByTestId("clear-filters"));
    await waitFor(() =>
      expect(document.querySelector('[data-task-id="t-kyle-open"]')).not.toBeNull()
    );
  });

  it("seeds the status axis EXPLICITLY — a stale 所有狀態 cannot leak DONE tasks through", async () => {
    // The test that makes the 還沒完成 assertion above non-vacuous. The tasks
    // page is not always a fresh mount: the owner may have widened 狀態 to
    // include 已完成 on an earlier visit, then hit the header icon. If the seed
    // only set the executor axis and trusted the mount-time default, that stale
    // status set survives and the jump shows DONE tasks — silently breaking
    // half of what the icon promises.
    //
    // Mutant-proven (M13 "seed drops the status axis"): asserting the done task
    // is absent from OPEN-LIST is dead — a done task lands in the CLOSED list,
    // so it is absent from the open list no matter what the filter does. The
    // assertion has to be document-wide.
    __injectMockTask(mkTask({ id: "t-mira-open", executorId: "mira" }));
    __injectMockTask(
      mkTask({
        id: "t-mira-done",
        executorId: "mira",
        status: "done",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    const { findByTestId, queryByTestId } = renderTasks();

    // Widen 狀態 to 所有狀態 the way the owner would: 清除篩選 = 顯示全部
    // (T-50bb), which EMPTIES the status set — terminals included.
    fireEvent.click(await findByTestId("clear-filters"));
    fireEvent.click(await findByTestId("closed-toggle"));
    await waitFor(() =>
      expect(
        document.querySelector('[data-task-id="t-mira-done"]')
      ).not.toBeNull()
    );

    // Now the header icon fires on the SAME live page — no remount.
    window.location.hash = "#tasks/executor/mira";
    await waitFor(() => expect(window.location.hash).toBe("#tasks"));

    // The stale 所有狀態 must have been overwritten by the seed. Document-wide:
    // the done task is nowhere, and 已結束 is gone entirely (it renders only
    // when the FILTERED closed set is non-empty).
    await waitFor(() =>
      expect(document.querySelector('[data-task-id="t-mira-done"]')).toBeNull()
    );
    expect(queryByTestId("closed-toggle")).toBeNull();
    // Positive control: the seed really did run and the page is not just empty.
    expect(document.querySelector('[data-task-id="t-mira-open"]')).not.toBeNull();
  });

  it("seeds the type axis EXPLICITLY — a stale 類型 filter cannot hide the member's rows", async () => {
    // The third axis the seed promises. Same shape of risk as the status one,
    // opposite direction: a live 類型 filter from an earlier visit would HIDE
    // rows the icon promised to show, so the jump lands on a list that looks
    // like "this member owes nothing".
    //
    // Mutant-proven (M14 "seed drops the type axis"): a fresh mount has an
    // empty typeFilter, which matches everything — so nothing short of a
    // deliberately dirtied type filter on a LIVE page can tell the two apart.
    __injectMockTask(
      mkTask({ id: "t-mira-adhoc", executorId: "mira", typeKey: "" })
    );
    __injectMockTask(
      mkTask({ id: "t-other-review", executorId: "kyle", typeKey: "review-pr" })
    );
    const { findByTestId } = renderTasks();
    await findByTestId("open-list");

    // Owner narrows 類型 to review-pr — mira's ad-hoc task is now hidden.
    toggleFilter("filter-type", "review-pr");
    await waitFor(() =>
      expect(document.querySelector('[data-task-id="t-mira-adhoc"]')).toBeNull()
    );

    // Header icon fires on the same live page.
    window.location.hash = "#tasks/executor/mira";
    await waitFor(() => expect(window.location.hash).toBe("#tasks"));

    // The stale 類型 must have been cleared: mira's ad-hoc task is back.
    await waitFor(() =>
      expect(
        document.querySelector('[data-task-id="t-mira-adhoc"]')
      ).not.toBeNull()
    );
    // …and the executor axis still bit (this is a filter seed, not a reset).
    expect(document.querySelector('[data-task-id="t-other-review"]')).toBeNull();
  });
});
