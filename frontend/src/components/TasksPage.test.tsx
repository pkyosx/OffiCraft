// 任務 page (M3 任務卡). Locked here — the SPEC §2/§3 acceptance behaviors:
//   1. Empty states ×2: no tasks at all → 目前沒有任務; filters matching
//      nothing → 沒有符合篩選條件的任務 (+ 清除篩選 restores).
//   2. 未結束 is ONE list ordered 高→中→低→凍結 (凍結永遠最後), createdTs
//      newest-first within a level — never grouped by status.
//   3. 已結束 (已完成+終止) is collapsible and COLLAPSED BY DEFAULT; the
//      title row toggles it (RepliesPage answered-toggle pattern).
//   4. Filters: 執行者 (外包/未指派/各成員) · 類型 (各手冊類型/自由代辦) ·
//      狀態 (六態全部).
//   5. Card rendering: six status badges (spec copy), executor display
//      (member name / 外包 代號·模型·投入度 / 未指派), SERVER progress
//      passthrough, transitional states (未指派→等待指派, 有執行者無節點→
//      規劃中), gate projection (announced dashed vs armed solid), parallel
//      stage label, deps chips, waiting_external reason row.
//   6. 終止 is double-confirmed (ConfirmModal), non-terminal only, and moves
//      the task into 已結束.
//   7. The embedded reply card is the SHARED M2 interior: answering by option
//      flips it to answered in place.
//   8. The message box posts one ordinary chat message to the executor and is
//      DISABLED while unassigned.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import {
  __resetMock,
  __injectMockTask,
  __injectMockOutsourceWorker,
  __injectMockTaskType,
  __injectMockReplyCard,
} from "../api/mock";
import { api } from "../api";
import type { TaskView, TaskStepView, ReplyCard } from "../api/adapter";

let seq = 0;

function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`,
    name: `step-${seq}`,
    dod: "",
    status: "pending",
    isGate: false,
    replyCardId: "",
    parallelGroup: "",
    orderIdx: seq,
    startedTs: 0,
    finishedTs: 0,
    ...over,
  };
}

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
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要現在同步到 Jira 嗎？",
    body: "",
    options: ["核可，直接同步上去", "先不要"],
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
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

// Toggle one option in a multi-select filter dropdown (T-be18): open the pill if
// needed, then click the option's checkbox. Leaves the popover open so several
// options can be toggled in a row.
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

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

describe("TasksPage", () => {
  it("shows the 目前沒有任務 empty state when there is nothing", async () => {
    const { findByTestId } = renderPage();
    const empty = await findByTestId("tasks-empty");
    expect(empty.textContent).toBe("目前沒有任務");
  });

  it("orders 未結束 by priority 高→中→低→凍結 (凍結墊底), createdTs newest-first within a level", async () => {
    const now = Date.now() / 1000;
    __injectMockTask(
      mkTask({ title: "凍結的", priority: "frozen", createdTs: now - 9000 })
    );
    __injectMockTask(
      mkTask({ title: "低的", priority: "low", createdTs: now - 8000 })
    );
    __injectMockTask(
      mkTask({ title: "高的晚建", priority: "high", createdTs: now - 100 })
    );
    __injectMockTask(
      mkTask({ title: "高的早建", priority: "high", createdTs: now - 5000 })
    );
    __injectMockTask(
      mkTask({ title: "中的", priority: "mid", createdTs: now - 7000 })
    );

    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    const titles = cards.map(
      (c) => c.querySelector(".task-card__title")?.textContent
    );
    expect(titles).toEqual([
      "高的晚建",
      "高的早建",
      "中的",
      "低的",
      "凍結的",
    ]);
  });

  it("已結束 collects done+terminated (once their statuses are picked), collapsed by default, toggling open/shut", async () => {
    __injectMockTask(mkTask({ title: "還在跑" }));
    __injectMockTask(
      mkTask({
        title: "做完的",
        status: "done",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    __injectMockTask(
      mkTask({
        title: "被終止的",
        status: "terminated",
        closedTs: Date.now() / 1000 - 30,
      })
    );

    const { findByTestId, queryByTestId, findAllByTestId } = renderPage();
    // Default view EXCLUDES the two terminal states (T-be18 #2): only the live
    // task shows and there is no 已結束 section yet.
    expect((await findAllByTestId("task-card")).length).toBe(1);
    expect(queryByTestId("closed-toggle")).toBeNull();

    // Tick 已完成 + 終止 in the 狀態 filter → the closed section appears.
    toggleFilter("filter-status", "done");
    toggleFilter("filter-status", "terminated");

    // Section titles carry counts; closed cards hidden while collapsed.
    const toggle = await findByTestId("closed-toggle");
    expect(toggle.textContent).toContain("已結束 · 2");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(queryByTestId("closed-list")).toBeNull();
    expect((await findAllByTestId("task-card")).length).toBe(1);

    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    const closedList = await findByTestId("closed-list");
    expect(closedList.querySelectorAll('[data-testid="task-card"]')).toHaveLength(2);

    fireEvent.click(toggle);
    expect(queryByTestId("closed-list")).toBeNull();
  });

  it("multi-selects executor / type / status; 清除篩選 clears to 顯示全部; no-match empty state", async () => {
    __injectMockTaskType({ typeKey: "review-pr", displayName: "", purpose: "" });
    __injectMockTask(
      mkTask({ title: "Mira 的", typeKey: "review-pr", status: "in_progress" })
    );
    __injectMockTask(
      mkTask({
        title: "外包的",
        typeKey: "",
        status: "waiting_external",
        waitingReason: "等對方開通 sandbox",
        executorKind: "outsource",
        executorId: "w-1",
      })
    );
    __injectMockTask(
      mkTask({
        title: "未指派的",
        executorKind: "outsource",
        executorId: "",
        status: "not_started",
      })
    );

    const { findAllByTestId, findByTestId, getByTestId, queryByTestId } =
      renderPage();
    // All three are non-terminal → all show under the default status filter.
    // The default view already narrows (terminals hidden) → 清除篩選 shows
    // from the start (T-50bb).
    expect((await findAllByTestId("task-card")).length).toBe(3);
    expect(queryByTestId("clear-filters")).not.toBeNull();

    // 執行者 = 外包 (assigned outsource only — 未指派 is its own option).
    toggleFilter("filter-executor", "outsource");
    await waitFor(() => {
      const cards = document.querySelectorAll('[data-testid="task-card"]');
      expect(cards).toHaveLength(1);
      expect(cards[0].textContent).toContain("外包的");
    });
    expect(getByTestId("clear-filters")).toBeTruthy();

    // MULTI-SELECT: add 未指派 alongside 外包 → both assigned-outsource and
    // unassigned tasks show (2), proving the axis is a union, not a swap.
    toggleFilter("filter-executor", "unassigned");
    await waitFor(() =>
      expect(
        document.querySelectorAll('[data-testid="task-card"]')
      ).toHaveLength(2)
    );

    // Narrow to 成員 mira only: drop the two outsource picks, add mira, and add
    // 類型 = review-pr (手冊類型 option).
    toggleFilter("filter-executor", "outsource");
    toggleFilter("filter-executor", "unassigned");
    toggleFilter("filter-executor", "mira");
    toggleFilter("filter-type", "review-pr");
    await waitFor(() => {
      const cards = document.querySelectorAll('[data-testid="task-card"]');
      expect(cards).toHaveLength(1);
      expect(cards[0].textContent).toContain("Mira 的");
    });

    // Un-ticking 進行中 from the 狀態 set drops Mira's (in_progress) task →
    // nothing matches → the filtered empty state.
    toggleFilter("filter-status", "in_progress");
    const empty = await findByTestId("tasks-empty-filtered");
    expect(empty.textContent).toBe("沒有符合篩選條件的任務");

    // 清除篩選 = 顯示全部 (T-50bb): every axis empties (status too) → all three
    // tasks again, and with nothing narrowing the list the button goes away.
    fireEvent.click(getByTestId("clear-filters"));
    await waitFor(() =>
      expect(
        document.querySelectorAll('[data-testid="task-card"]')
      ).toHaveLength(3)
    );
    expect(queryByTestId("clear-filters")).toBeNull();

    // 類型 = 自由代辦 (no type key).
    toggleFilter("filter-type", "adhoc");
    await waitFor(() =>
      expect(
        document.querySelectorAll('[data-testid="task-card"]')
      ).toHaveLength(2)
    );
  });

  it("類型篩選 option shows the manual's DISPLAY name; a deleted manual's key falls back to itself (T-fa76)", async () => {
    __injectMockTaskType({
      typeKey: "tm-aaaabbbbcccc",
      displayName: "審查 PR",
      purpose: "",
    });
    __injectMockTask(
      mkTask({ title: "有顯示名", typeKey: "tm-aaaabbbbcccc" })
    );
    __injectMockTask(mkTask({ title: "手冊已刪", typeKey: "gone-type" }));

    const { findAllByTestId, getByTestId } = renderPage();
    await findAllByTestId("task-card");
    fireEvent.click(getByTestId("filter-type"));
    // The option row shows the human label, never the tm- key…
    const named = getByTestId("filter-type-opt-tm-aaaabbbbcccc");
    expect(named.textContent).toContain("審查 PR");
    expect(named.textContent).not.toContain("tm-aaaabbbbcccc");
    // …and a type whose manual is gone honestly shows its raw key.
    expect(getByTestId("filter-type-opt-gone-type").textContent).toContain(
      "gone-type"
    );
    // Picking by display label still filters by the underlying key.
    toggleFilter("filter-type", "tm-aaaabbbbcccc");
    await waitFor(() => {
      const cards = document.querySelectorAll('[data-testid="task-card"]');
      expect(cards).toHaveLength(1);
      expect(cards[0].textContent).toContain("有顯示名");
    });
  });

  it("renders the six status badges with the SPEC copy (not the mockup variants)", async () => {
    const now = Date.now() / 1000;
    const specs: Array<[string, string]> = [
      ["not_started", "尚未執行"],
      ["in_progress", "進行中"],
      ["waiting_owner", "等我回覆"],
      ["waiting_external", "等待外部"],
      ["done", "已完成"],
      ["terminated", "終止"],
    ];
    for (const [status] of specs) {
      __injectMockTask(
        mkTask({
          title: `s-${status}`,
          status,
          waitingReason: status === "waiting_external" ? "等外部" : "",
          closedTs:
            status === "done" || status === "terminated" ? now - 10 : null,
        })
      );
    }
    const { findByTestId } = renderPage();
    // Terminals are hidden by default — tick 已完成 + 終止 so all six render,
    // then reveal the 已結束 section that now holds the two terminal cards.
    toggleFilter("filter-status", "done");
    toggleFilter("filter-status", "terminated");
    fireEvent.click(await findByTestId("closed-toggle"));
    await waitFor(() => {
      const badges = [
        ...document.querySelectorAll('[data-testid="task-status"]'),
      ].map((b) => b.textContent);
      for (const [, label] of specs) {
        expect(badges).toContain(label);
      }
    });
  });

  it("shows executor identity: member name, 外包 代號·模型·投入度, and 未指派", async () => {
    __injectMockTask(mkTask({ title: "成員任務", executorId: "mira" }));
    const outsourced = mkTask({
      title: "外包任務",
      executorKind: "outsource",
      executorId: "w-7",
    });
    __injectMockTask(outsourced);
    __injectMockOutsourceWorker({
      id: "w-7",
      codename: "O-7",
      model: "Opus 4.6",
      effort: "high",
      taskId: outsourced.id,
    });
    __injectMockTask(
      mkTask({ title: "未指派任務", executorKind: "outsource", executorId: "" })
    );

    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    const byTitle = (title: string) =>
      cards.find((c) =>
        c.querySelector(".task-card__title")?.textContent?.includes(title)
      )!;
    expect(
      byTitle("成員任務").querySelector('[data-testid="task-executor"]')
        ?.textContent
    ).toBe("Mira");
    expect(
      byTitle("外包任務").querySelector('[data-testid="task-executor"]')
        ?.textContent
    ).toBe("外包 · O-7 · Opus 4.6 · 高投入");
    expect(
      byTitle("未指派任務").querySelector('[data-testid="task-executor"]')
        ?.textContent
    ).toBe("未指派");
  });

  it("passes the SERVER progress through, shows the key badge (URL → target=_blank) and dep chips", async () => {
    const blocker = mkTask({ title: "擋路的", taskNo: "T-7d40" });
    __injectMockTask(blocker);
    __injectMockTask(
      mkTask({
        title: "被擋的",
        dedupeKey: "https://github.com/x/y/pull/482",
        deps: [blocker.id],
        progressDone: 2,
        progressTotal: 4,
      })
    );

    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    const card = cards.find((c) =>
      c.querySelector(".task-card__title")?.textContent?.includes("被擋的")
    )!;
    expect(
      card.querySelector('[data-testid="task-progress"]')?.textContent
    ).toContain("步驟 2/4");
    const link = card.querySelector('[data-testid="task-key-link"]');
    expect(link?.getAttribute("target")).toBe("_blank");
    expect(link?.getAttribute("href")).toBe("https://github.com/x/y/pull/482");
    expect(
      card.querySelector('[data-testid="task-dep"]')?.textContent
    ).toBe("等 T-7d40");
  });

  it("shows the transitional states: 未指派 → 等待指派, 有執行者但零節點 → 等待 ○○ 建立 Steps", async () => {
    __injectMockTask(
      mkTask({
        title: "還沒指派",
        executorKind: "outsource",
        executorId: "",
        status: "not_started",
        steps: [],
      })
    );
    __injectMockTask(mkTask({ title: "有人沒計劃", steps: [] }));

    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    // Cards are COLLAPSED by default (mockup) — the workflow block (and its
    // transitional line) shows after clicking the card expands it.
    for (const c of cards) {
      fireEvent.click(c);
    }
    const byTitle = (title: string) =>
      cards.find((c) =>
        c.querySelector(".task-card__title")?.textContent?.includes(title)
      )!;
    expect(
      byTitle("還沒指派").querySelector('[data-testid="task-transition"]')
        ?.textContent
    ).toBe("等待指派");
    // 有執行者(成員 Mira)但零節點且 progressTotal 0 → 「等待 ○○ 建立 Steps」,
    // ○○ = 負責人顯示名(owner 核定 2026-07,T-71e8 同批)。名字來自 useMembers
    // (非同步載入),故用 waitFor 重試到 roster 到位、名稱由 id 定為顯示名。
    await waitFor(() =>
      expect(
        byTitle("有人沒計劃").querySelector('[data-testid="task-transition"]')
          ?.textContent
      ).toBe("等待 Mira 建立 Steps")
    );
  });

  it("cards are COLLAPSED by default; clicking the card reveals the workflow", async () => {
    __injectMockTask(
      mkTask({
        title: "預設摺疊的",
        progressDone: 0,
        progressTotal: 1,
        steps: [mkStep({ name: "step-1", status: "in_progress", orderIdx: 0 })],
      })
    );
    const { findByTestId, queryByTestId, getByTestId } = renderPage();
    const card = await findByTestId("task-card");
    // Collapsed: head/keys/progress/message box show, the workflow does not.
    expect(card.querySelector('[data-testid="task-progress"]')).not.toBeNull();
    expect(card.querySelector('[data-testid="task-msg-input"]')).not.toBeNull();
    expect(queryByTestId("task-step")).toBeNull();
    // Chevron → the workflow hydrates (light list carries no steps) and the
    // timeline appears; again → hides.
    fireEvent.click(getByTestId("task-card"));
    expect(await findByTestId("task-step")).not.toBeNull();
    fireEvent.click(getByTestId("task-card"));
    expect(queryByTestId("task-step")).toBeNull();
  });

  it("renders the timeline: step DoD + 耗時, parallel stage label, gate announced (dashed) vs armed (solid)", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "rc-armed" }));
    __injectMockTask(
      mkTask({
        title: "有流程的",
        status: "waiting_owner",
        steps: [
          mkStep({
            name: "build",
            dod: "產出可部署的 build artifact",
            status: "done",
            orderIdx: 0,
            startedTs: now - 600,
            finishedTs: now - 240,
          }),
          mkStep({
            name: "unit-test",
            status: "in_progress",
            parallelGroup: "verify",
            orderIdx: 1,
            startedTs: now - 200,
          }),
          mkStep({
            name: "e2e-test",
            status: "in_progress",
            parallelGroup: "verify",
            orderIdx: 2,
            startedTs: now - 200,
          }),
          mkStep({
            name: "approve",
            status: "waiting_owner",
            isGate: true,
            replyCardId: "rc-armed",
            orderIdx: 3,
            startedTs: now - 100,
          }),
          mkStep({
            name: "publish",
            status: "pending",
            isGate: true,
            replyCardId: "",
            orderIdx: 4,
          }),
        ],
      })
    );

    const { findAllByTestId, findByTestId } = renderPage();
    // Expand the (collapsed-by-default) card to reveal the workflow timeline.
    fireEvent.click(await findByTestId("task-card"));
    const steps = await findAllByTestId("task-step");
    expect(steps).toHaveLength(5);
    // DoD renders under the node name; the finished step carries its 耗時 (6m).
    expect(steps[0].textContent).toContain("產出可部署的 build artifact");
    expect(steps[0].textContent).toContain("6m");
    expect(steps[0].textContent).toContain("完成");
    // Parallel stage: one block labelled 同時進行 · 2 項並行.
    const parallel = await findByTestId("task-parallel");
    expect(parallel.textContent).toContain("同時進行 · 2 項並行");
    expect(
      parallel.querySelectorAll('[data-testid="task-step"]')
    ).toHaveLength(2);
    // ARMED gate: solid 等我回覆 badge (waiting_owner class, no dashed one).
    expect(
      steps[3].querySelector(".task-step-badge--waiting_owner")?.textContent
    ).toBe("等我回覆");
    expect(steps[3].querySelector('[data-testid="gate-announced"]')).toBeNull();
    // ANNOUNCED gate: dashed preview badge even while the step is pending.
    expect(
      steps[4].querySelector('[data-testid="gate-announced"]')?.textContent
    ).toBe("等我回覆");
  });

  it("aggregates the parallel stage dot: any in_progress → 藍, all done → 綠, else pending", async () => {
    const now = Date.now() / 1000;
    __injectMockTask(
      mkTask({
        title: "三段並行",
        steps: [
          // Stage 1: every lane done → the stage dot is done.
          mkStep({ name: "a1", status: "done", parallelGroup: "pg-done", orderIdx: 0, startedTs: now - 900, finishedTs: now - 600 }),
          mkStep({ name: "a2", status: "done", parallelGroup: "pg-done", orderIdx: 1, startedTs: now - 900, finishedTs: now - 700 }),
          // Stage 2: one lane running → in_progress wins the dot.
          mkStep({ name: "b1", status: "done", parallelGroup: "pg-run", orderIdx: 2, startedTs: now - 500, finishedTs: now - 400 }),
          mkStep({ name: "b2", status: "in_progress", parallelGroup: "pg-run", orderIdx: 3, startedTs: now - 500 }),
          // Stage 3: some done but none running → still pending as a stage.
          mkStep({ name: "c1", status: "done", parallelGroup: "pg-wait", orderIdx: 4, startedTs: now - 300, finishedTs: now - 200 }),
          mkStep({ name: "c2", status: "pending", parallelGroup: "pg-wait", orderIdx: 5 }),
        ],
      })
    );
    const { findByTestId, findAllByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-card"));
    const stages = await findAllByTestId("task-parallel");
    expect(stages).toHaveLength(3);
    const dotOf = (stage: Element) =>
      stage.parentElement!.querySelector('[class*="task-timeline__dot--"]')!
        .className;
    expect(dotOf(stages[0])).toContain("task-timeline__dot--done");
    expect(dotOf(stages[1])).toContain("task-timeline__dot--in_progress");
    expect(dotOf(stages[2])).toContain("task-timeline__dot--pending");
  });

  it("renders legacy dirty parallel data defensively: gate-in-group keeps its badge, a split group folds into two separate stages", async () => {
    // submit_plan now refuses these shapes (400), but rows written before the
    // gate landed are never rewritten — the timeline must stay honest, not crash.
    __injectMockTask(
      mkTask({
        title: "髒資料",
        steps: [
          // A gate INSIDE a parallel group (pre-gate legacy): the announced
          // dashed badge still renders inside the stage.
          mkStep({ name: "lane", status: "in_progress", parallelGroup: "pg-x", orderIdx: 0 }),
          mkStep({ name: "approve", status: "pending", parallelGroup: "pg-x", isGate: true, replyCardId: "", orderIdx: 1 }),
          // The SAME group key split by a plain step: two stages, each with
          // its own honest count — never merged across the gap.
          mkStep({ name: "solo", status: "pending", orderIdx: 2 }),
          mkStep({ name: "stray", status: "pending", parallelGroup: "pg-x", orderIdx: 3 }),
        ],
      })
    );
    const { findByTestId, findAllByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-card"));
    const stages = await findAllByTestId("task-parallel");
    expect(stages).toHaveLength(2);
    expect(stages[0].textContent).toContain("同時進行 · 2 項並行");
    expect(stages[1].textContent).toContain("同時進行 · 1 項並行");
    // The in-group announced gate still shows its dashed 等我回覆 preview.
    expect(
      stages[0].querySelector('[data-testid="gate-announced"]')?.textContent
    ).toBe("等我回覆");
  });

  it("終止 double-confirms; the terminated task leaves the default view, and 終止 reveals it in 已結束 (action hidden on closed tasks)", async () => {
    __injectMockTask(mkTask({ title: "要被終止的" }));
    const { findByTestId, queryByTestId } = renderPage();

    // v5: 終止 lives on the 狀態 badge dropdown, no longer under the ⋮.
    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-terminate"));
    // Second step: the ConfirmModal — cancelling does nothing yet.
    const modal = await findByTestId("terminate-confirm");
    expect(modal.textContent).toContain("要被終止的");
    fireEvent.click(await findByTestId("terminate-confirm-btn"));

    // Its new 終止 state is excluded by the default filter → the task drops out
    // of view entirely (no 未結束 card, no 已結束 section yet).
    await waitFor(() =>
      expect(document.querySelectorAll('[data-testid="task-card"]')).toHaveLength(
        0
      )
    );
    expect(queryByTestId("closed-toggle")).toBeNull();

    // Tick 終止 in the 狀態 filter → it surfaces in the (collapsed) 已結束 section.
    toggleFilter("filter-status", "terminated");
    const toggle = await findByTestId("closed-toggle");
    await waitFor(() => expect(toggle.textContent).toContain("已結束 · 1"));
    fireEvent.click(toggle);
    const closedList = await findByTestId("closed-list");
    const card = closedList.querySelector('[data-testid="task-card"]')!;
    expect(
      card.querySelector('[data-testid="task-status"]')?.textContent
    ).toBe("終止");
    // Terminal task: the badge STILL drops its menu (owner ruling 2026-07-17 —
    // 「照字面永遠出，已結束時兩項變灰不可點」), but 終止 there is now greyed and
    // dead: clicking it opens no confirm modal, so no 409-bound terminate can
    // leave the UI. (Full three-shape coverage lives in
    // TaskCard.status-menu.test.tsx; this pins the end of THIS flow.)
    const closedBadge = card.querySelector('[data-testid="task-status"]')!;
    fireEvent.click(closedBadge);
    expect(
      card.querySelector('[data-testid="task-status-options"]')
    ).toBeTruthy();
    const closedTerm = card.querySelector<HTMLButtonElement>(
      '[data-testid="task-terminate"]'
    )!;
    expect(closedTerm.disabled).toBe(true);
    fireEvent.click(closedTerm);
    expect(queryByTestId("terminate-confirm")).toBeNull();
  });

  it("changes priority (freeze) through the in-place chip picker — the frozen task drops to the tail", async () => {
    const now = Date.now() / 1000;
    __injectMockTask(
      mkTask({ title: "先高的", priority: "high", createdTs: now - 100 })
    );
    __injectMockTask(
      mkTask({ title: "低的", priority: "low", createdTs: now - 50 })
    );

    const { findAllByTestId, findByTestId } = renderPage();
    let cards = await findAllByTestId("task-card");
    expect(
      cards[0].querySelector(".task-card__title")?.textContent
    ).toContain("先高的");

    fireEvent.click(
      cards[0].querySelector('[data-testid="task-priority"]')!
    );
    fireEvent.click(await findByTestId("priority-frozen"));

    await waitFor(async () => {
      cards = await findAllByTestId("task-card");
      expect(
        cards[0].querySelector(".task-card__title")?.textContent
      ).toContain("低的");
      expect(
        cards[1].querySelector('[data-testid="task-priority"]')?.textContent
      ).toContain("凍結");
    });
  });

  it("embeds the armed gate's reply card; answering flips it to answered in place", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    __injectMockTask(
      mkTask({
        title: "等我核可的",
        status: "waiting_owner",
        steps: [
          mkStep({
            name: "push-status",
            status: "waiting_owner",
            isGate: true,
            replyCardId: "rc-1",
            orderIdx: 0,
            startedTs: Date.now() / 1000 - 300,
          }),
        ],
      })
    );

    const { findByTestId } = renderPage();
    // The embedded gate cards live in the EXPANDED card body (mockup default
    // collapsed) — expand first.
    fireEvent.click(await findByTestId("task-card"));
    const embedded = await findByTestId("task-reply-card");
    // The SHARED M2 interior: options[0] carries the AI 建議 tag.
    const options = embedded.querySelectorAll(".reply-option");
    expect(options).toHaveLength(2);
    expect(options[0].textContent).toContain("AI 建議");

    // Answer by option → the card flips answered in place.
    fireEvent.click(options[0]);
    await waitFor(async () => {
      expect(
        (await findByTestId("task-reply-card")).querySelector(
          '[data-testid="final-answer"]'
        )?.textContent
      ).toContain("你選的");
    });
    // The 等我回覆 page shares the same store: the card is answered there too.
    const answered = await api.listReplyCards("answered");
    expect(answered.map((c) => c.id)).toContain("rc-1");
  });

  it("the message box posts one ordinary chat message to the executor, prefixed with the task number", async () => {
    const task = mkTask({ title: "可傳話的", executorId: "mira" });
    __injectMockTask(task);
    const { findByTestId } = renderPage();

    const input = await findByTestId("task-msg-input");
    expect(input.getAttribute("placeholder")).toBe("傳訊息給 Mira…");
    fireEvent.change(input, { target: { value: "先做 P0 的部分" } });
    fireEvent.click(await findByTestId("task-msg-send"));

    // The executor's chat message carries the task's display number so it is
    // self-identifying (owner 2026-07-14).
    await waitFor(async () => {
      const thread = await api.peekChat("mira");
      expect(thread.map((m) => m.body)).toContain(`[${task.taskNo}] 先做 P0 的部分`);
    });
    // Sent → the box clears (retry keeps content only on failure).
    await waitFor(() =>
      expect((input as HTMLInputElement).value).toBe("")
    );
  });

  it("the message box is disabled while the task is unassigned (server would 409)", async () => {
    __injectMockTask(
      mkTask({
        title: "沒人接的",
        executorKind: "outsource",
        executorId: "",
        status: "not_started",
      })
    );
    const { findByTestId } = renderPage();
    const input = (await findByTestId("task-msg-input")) as HTMLInputElement;
    expect(input.disabled).toBe(true);
    const send = (await findByTestId("task-msg-send")) as HTMLButtonElement;
    expect(send.disabled).toBe(true);
  });

  // ── light list + expand hydration (T-d70f) ────────────────────────────────
  // The list serves the LIGHT projection (no steps/description); a card fetches
  // its full detail via getTask only when expanded.
  describe("light list + lazy detail on expand", () => {
    it("keeps steps/description off the collapsed card and hydrates them via getTask on expand", async () => {
      __injectMockTask(
        mkTask({
          title: "懶載入詳情",
          description: "只有展開才看得到的描述",
          progressDone: 1,
          progressTotal: 2,
          steps: [
            mkStep({ name: "節點甲", status: "done", orderIdx: 0 }),
            mkStep({ name: "節點乙", status: "in_progress", orderIdx: 1 }),
          ],
        })
      );

      const getTaskSpy = vi.spyOn(api, "getTask");
      const { getByTestId, findByText, queryByText } = renderPage();

      // Collapsed: the light card is on screen with the SERVER progress (1/2),
      // but the heavy detail is absent and getTask has NOT been called.
      await findByText("懶載入詳情");
      expect(getByTestId("task-progress").textContent).toContain("1");
      expect(queryByText("節點甲")).toBeNull();
      expect(queryByText("只有展開才看得到的描述")).toBeNull();
      expect(getTaskSpy).not.toHaveBeenCalled();

      // Expand → getTask fires once and the steps + description hydrate in.
      fireEvent.click(getByTestId("task-card"));
      await findByText("節點甲");
      await findByText("節點乙");
      await findByText("只有展開才看得到的描述");
      expect(getTaskSpy).toHaveBeenCalledTimes(1);

      getTaskSpy.mockRestore();
    });

    it("manually expanding a DONE task in 已結束 hydrates its steps via getTask", async () => {
      __injectMockTask(
        mkTask({
          title: "已完成有步驟",
          status: "done",
          closedTs: Date.now() / 1000 - 100,
          progressDone: 2,
          progressTotal: 2,
          steps: [
            mkStep({ name: "完成節點甲", status: "done", orderIdx: 0 }),
            mkStep({ name: "完成節點乙", status: "done", orderIdx: 1 }),
          ],
        })
      );
      const getTaskSpy = vi.spyOn(api, "getTask");
      const { getByTestId, findByTestId, findByText, queryByText } = renderPage();

      // Terminals are filtered out by default — tick 已完成 so the done task's
      // 已結束 section exists, then open the (collapsed) section.
      toggleFilter("filter-status", "done");
      fireEvent.click(await findByTestId("closed-toggle"));
      await findByText("已完成有步驟");
      // Light list → no steps in the DOM yet.
      expect(queryByText("完成節點甲")).toBeNull();

      // Expand the done card → getTask must fire and the steps must render.
      fireEvent.click(getByTestId("task-card"));
      await findByText("完成節點甲");
      await findByText("完成節點乙");
      expect(getTaskSpy).toHaveBeenCalled();

      getTaskSpy.mockRestore();
    });

    it("auto-expands and hydrates a DONE task jumped to via #tasks/<id> (T-4108 regression)", async () => {
      // The real report: a reply card's 查看任務詳情 (or the 外包 panel task chip)
      // routes to #tasks/<id>. For a CLOSED task that filters the list to one
      // card and auto-opens 已結束 — but the card itself must ALSO auto-expand
      // (located → setExpanded), otherwise the owner lands on the task they
      // asked to see and its steps/details are hidden behind a collapsed card.
      __injectMockTask(
        mkTask({
          id: "t-jump-done",
          title: "跳轉過來的完成任務",
          status: "done",
          closedTs: Date.now() / 1000 - 100,
          progressDone: 2,
          progressTotal: 2,
          steps: [
            mkStep({ name: "歷史節點甲", status: "done", orderIdx: 0 }),
            mkStep({ name: "歷史節點乙", status: "done", orderIdx: 1 }),
          ],
        })
      );
      const getTaskSpy = vi.spyOn(api, "getTask");
      window.location.hash = "#tasks/t-jump-done";
      const { findByText, queryByTestId } = renderPage();

      // No manual expand click: arriving via the jump must reveal the steps.
      await findByText("歷史節點甲");
      await findByText("歷史節點乙");
      expect(getTaskSpy).toHaveBeenCalled();
      // The 規劃中 empty state must never stand in for a hydrated done task.
      expect(queryByTestId("task-transition")).toBeNull();

      getTaskSpy.mockRestore();
    });

    it("shows a loading placeholder (not the 規劃中 empty state) while a planned task's steps are in flight", async () => {
      __injectMockTask(
        mkTask({
          title: "載入中不要閃空狀態",
          progressDone: 0,
          progressTotal: 1,
          steps: [mkStep({ name: "唯一節點", status: "pending", orderIdx: 0 })],
        })
      );

      // Hold getTask open so the placeholder is observable before detail lands.
      let release!: (v: TaskView) => void;
      const gate = new Promise<TaskView>((res) => {
        release = res;
      });
      const full = await api.getTask(
        (await api.listTasks()).find((t) => t.title === "載入中不要閃空狀態")!.id
      );
      const getTaskSpy = vi
        .spyOn(api, "getTask")
        .mockReturnValueOnce(gate);

      const { findByTestId, queryByTestId } = renderPage();
      fireEvent.click(await findByTestId("task-card"));

      // While hydrating: the loading placeholder shows, NOT the 規劃中 transition.
      await findByTestId("task-steps-loading");
      expect(queryByTestId("task-transition")).toBeNull();

      // Detail lands → the real timeline replaces the placeholder.
      release(full);
      await findByTestId("task-step");
      expect(queryByTestId("task-steps-loading")).toBeNull();

      getTaskSpy.mockRestore();
    });

    it("surfaces an error + retry (never the fake 規劃中) when the detail hydrate fails, and recovers on retry", async () => {
      __injectMockTask(
        mkTask({
          title: "拉不到詳情",
          progressDone: 0,
          progressTotal: 1,
          steps: [mkStep({ name: "待載節點", status: "pending", orderIdx: 0 })],
        })
      );
      // Capture the full task for the successful retry, then fail the FIRST
      // hydrate and let the second (retry) succeed.
      const injected = (await api.listTasks()).find(
        (t) => t.title === "拉不到詳情"
      )!;
      const full = await api.getTask(injected.id);
      const getTaskSpy = vi
        .spyOn(api, "getTask")
        .mockRejectedValueOnce(new Error("boom"))
        .mockResolvedValue(full);

      const { findByTestId, queryByTestId } = renderPage();
      fireEvent.click(await findByTestId("task-card"));

      // Failed hydrate → error + retry surface, NOT the fake 規劃中/no-step state.
      await findByTestId("task-steps-error");
      expect(queryByTestId("task-transition")).toBeNull();
      expect(queryByTestId("task-step")).toBeNull();

      // Retry re-runs the fetch → steps hydrate, error clears.
      fireEvent.click(await findByTestId("task-steps-retry"));
      await findByTestId("task-step");
      expect(queryByTestId("task-steps-error")).toBeNull();

      getTaskSpy.mockRestore();
    });
  });
});

// ── T-be18: filter 強化 (multi-select · default-exclude terminals · owner counts)
describe("TasksPage filter enhancements (T-be18)", () => {
  it("opens with the two terminal states excluded — only live tasks show, and the 狀態 dropdown shows them unchecked", async () => {
    const now = Date.now() / 1000;
    __injectMockTask(mkTask({ title: "活的", status: "in_progress" }));
    __injectMockTask(
      mkTask({ title: "完成的", status: "done", closedTs: now - 10 })
    );
    __injectMockTask(
      mkTask({ title: "終止的", status: "terminated", closedTs: now - 5 })
    );

    const { findByText, findByTestId, queryByText, queryByTestId } = renderPage();
    // Default view: the live task only; neither terminal task is anywhere, and
    // there is no 已結束 section standing in for them.
    await findByText("活的");
    expect(queryByText("完成的")).toBeNull();
    expect(queryByText("終止的")).toBeNull();
    expect(queryByTestId("closed-toggle")).toBeNull();

    // The exclusion is expressed IN the filter UI: 已完成/終止 start unchecked
    // while the live statuses are checked (the owner can see and undo it).
    fireEvent.click(await findByTestId("filter-status"));
    const checkOf = (v: string) =>
      document.querySelector<HTMLInputElement>(
        `[data-testid="filter-status-opt-${v}"] input`
      )!;
    expect(checkOf("done").checked).toBe(false);
    expect(checkOf("terminated").checked).toBe(false);
    expect(checkOf("in_progress").checked).toBe(true);
    expect(checkOf("waiting_owner").checked).toBe(true);
  });

  it("lists each owner's task count in the 所有人 dropdown, and the count tracks the status filter", async () => {
    const now = Date.now() / 1000;
    // Mira: two live + one done. Plus one assigned-outsource and one unassigned.
    __injectMockTask(mkTask({ title: "m1", executorId: "mira", status: "in_progress" }));
    __injectMockTask(mkTask({ title: "m2", executorId: "mira", status: "waiting_owner" }));
    __injectMockTask(
      mkTask({ title: "m3", executorId: "mira", status: "done", closedTs: now - 10 })
    );
    __injectMockTask(
      mkTask({
        title: "o1",
        executorKind: "outsource",
        executorId: "w-9",
        status: "in_progress",
      })
    );
    __injectMockTask(
      mkTask({
        title: "u1",
        executorKind: "outsource",
        executorId: "",
        status: "not_started",
      })
    );

    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("filter-executor"));
    const countOf = (v: string) =>
      document.querySelector(`[data-testid="filter-executor-count-${v}"]`)
        ?.textContent;

    // Default status excludes the done task → Mira shows 2 (her live tasks),
    // 外包 1, 未指派 1.
    await waitFor(() => expect(countOf("mira")).toBe("2"));
    expect(countOf("outsource")).toBe("1");
    expect(countOf("unassigned")).toBe("1");

    // Tick 已完成 in the 狀態 filter → Mira's count grows to 3 (the count basis
    // follows the live status filter, T-be18 #3).
    toggleFilter("filter-status", "done");
    await waitFor(() => expect(countOf("mira")).toBe("3"));
  });

  it("hides executors with zero tasks in the current result set (計數 0 隱藏; 外包/未指派同規則)", async () => {
    // Only a live Mira task exists → 外包 and 未指派 have count 0.
    __injectMockTask(
      mkTask({ title: "只有 Mira", executorId: "mira", status: "in_progress" })
    );

    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("filter-executor"));
    const optOf = (v: string) =>
      document.querySelector(`[data-testid="filter-executor-opt-${v}"]`);

    // Mira (count 1) is listed; the two zero-count pseudo-owners are dropped.
    await waitFor(() => expect(optOf("mira")).not.toBeNull());
    expect(optOf("outsource")).toBeNull();
    expect(optOf("unassigned")).toBeNull();
  });

  it("keeps a checked executor visible when its count drops to 0, and drops it once unchecked", async () => {
    // Mira (in_progress) + one assigned-outsource (not_started) — both live, so
    // both start with count 1 and both are listed.
    __injectMockTask(
      mkTask({ title: "Mira 的", executorId: "mira", status: "in_progress" })
    );
    __injectMockTask(
      mkTask({
        title: "外包的",
        executorKind: "outsource",
        executorId: "w-9",
        status: "not_started",
      })
    );

    const { findByText, findByTestId } = renderPage();
    const optOf = (v: string) =>
      document.querySelector(`[data-testid="filter-executor-opt-${v}"]`);
    const checkOf = (v: string) =>
      document.querySelector<HTMLInputElement>(
        `[data-testid="filter-executor-opt-${v}"] input`
      );

    // Wait for the async task load before opening the dropdown, else 外包 has a
    // stale count of 0 and is hidden before its task arrives.
    await findByText("外包的");
    fireEvent.click(await findByTestId("filter-executor"));
    await waitFor(() => expect(optOf("outsource")).not.toBeNull());

    // 勾選 外包 while it still has a task.
    toggleFilter("filter-executor", "outsource");
    await waitFor(() => expect(checkOf("outsource")?.checked).toBe(true));

    // Un-tick 尚未執行 in 狀態 → the only outsource task leaves the count scope,
    // so 外包's count is now 0 — but it is still CHECKED, so it must stay in the
    // dropdown (邊界:勾選態不從下拉消失,否則無法取消勾選).
    toggleFilter("filter-status", "not_started");
    await waitFor(() =>
      expect(
        document.querySelector(
          `[data-testid="filter-executor-count-outsource"]`
        )?.textContent
      ).toBe("0")
    );
    expect(optOf("outsource")).not.toBeNull();
    expect(checkOf("outsource")?.checked).toBe(true);

    // Un-tick 外包 → now zero-count AND unchecked → it disappears from the list.
    toggleFilter("filter-executor", "outsource");
    await waitFor(() => expect(optOf("outsource")).toBeNull());
  });

  it("unions statuses: ticking 已完成 alongside the live defaults shows BOTH live and done tasks", async () => {
    const now = Date.now() / 1000;
    __injectMockTask(mkTask({ title: "跑著的", status: "in_progress" }));
    __injectMockTask(
      mkTask({ title: "收工的", status: "done", closedTs: now - 10 })
    );

    const { findByText, findByTestId, queryByText } = renderPage();
    await findByText("跑著的");
    expect(queryByText("收工的")).toBeNull();

    toggleFilter("filter-status", "done");
    // 已結束 now carries the done task — expand it to see the card.
    fireEvent.click(await findByTestId("closed-toggle"));
    await findByText("收工的");
    // The live task is still there — the tick added a status, it didn't replace.
    expect((await findByText("跑著的")).textContent).toContain("跑著的");
  });
});

// ── T-50bb: 清除篩選 = 顯示全部 (the default view counts as a filter)
describe("TasksPage 清除篩選 semantics (T-50bb)", () => {
  const injectMixedTasks = () => {
    const now = Date.now() / 1000;
    __injectMockTask(mkTask({ title: "活的", status: "in_progress" }));
    __injectMockTask(
      mkTask({ title: "完成的", status: "done", closedTs: now - 10 })
    );
    __injectMockTask(
      mkTask({ title: "終止的", status: "terminated", closedTs: now - 5 })
    );
  };

  it("shows 清除篩選 already in the DEFAULT view (its status set hides terminals — that IS a filter)", async () => {
    injectMixedTasks();
    const { findByText, getByTestId } = renderPage();
    await findByText("活的");
    expect(getByTestId("clear-filters")).toBeTruthy();
  });

  it("clicking 清除篩選 from the default view lists EVERYTHING — 已完成/終止 included — and the button goes away", async () => {
    injectMixedTasks();
    const { findByText, findByTestId, getByTestId, queryByTestId, queryByText } =
      renderPage();
    await findByText("活的");
    expect(queryByText("完成的")).toBeNull();

    fireEvent.click(getByTestId("clear-filters"));
    // Terminals now pass the (emptied) status filter → the 已結束 section
    // appears with both closed tasks; expand it to see the cards.
    const toggle = await findByTestId("closed-toggle");
    expect(toggle.textContent).toContain("已結束 · 2");
    fireEvent.click(toggle);
    await findByText("完成的");
    await findByText("終止的");
    expect((await findByText("活的")).textContent).toContain("活的");
    // Nothing narrows the list anymore → no 清除篩選, and the 狀態 dropdown
    // reads as unconstrained (all boxes unchecked).
    expect(queryByTestId("clear-filters")).toBeNull();
    fireEvent.click(await findByTestId("filter-status"));
    const checkOf = (v: string) =>
      document.querySelector<HTMLInputElement>(
        `[data-testid="filter-status-opt-${v}"] input`
      )!;
    expect(checkOf("in_progress").checked).toBe(false);
    expect(checkOf("done").checked).toBe(false);
  });

  it("clicking 清除篩選 after editing filters ALSO goes straight to 顯示全部 (not back to the default view)", async () => {
    injectMixedTasks();
    const { findByText, findByTestId, getByTestId, queryByText } = renderPage();
    await findByText("活的");

    // Edit two axes: executor = mira, and un-tick 進行中 → nothing matches.
    toggleFilter("filter-executor", "mira");
    toggleFilter("filter-status", "in_progress");
    await findByTestId("tasks-empty-filtered");

    fireEvent.click(getByTestId("clear-filters"));
    // Straight to 顯示全部: the live task is back AND the terminals surface in
    // 已結束 — proof it did not stop at the terminal-hiding default view.
    await findByText("活的");
    fireEvent.click(await findByTestId("closed-toggle"));
    await findByText("完成的");
    expect(queryByText("終止的")).not.toBeNull();
  });
});
