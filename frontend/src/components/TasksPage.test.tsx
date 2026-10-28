// 任務 page (M3 任務卡). Locked here — the SPEC §2/§3 acceptance behaviors:
//   1. Empty states ×2: no tasks at all → 目前沒有任務 (T-a3e4: judged from
//      GET /api/tasks/count's unfiltered `total`, since the list itself now
//      answers only the ticked statuses); filters matching nothing →
//      沒有符合篩選條件的任務 (+ 清除篩選 restores).
//   2. Every task is in ONE list: open tasks ordered 高→中→低→凍結 (凍結永遠
//      最後), createdTs newest-first within a level, then closed tasks
//      (已完成+終止+重複) newest close first. There is no 未結案 / 已結案
//      section; the default view is that one flat list (不分組), and under 到期
//      grouping a closed task goes into the lane of its due date like an open
//      one.
//   3. The 狀態 filter's 未結案 / 已結案 group rows tick either side at once.
//   4. Filters: 執行者 (外包/未指派/各成員) · 類型 (各手冊類型/自由代辦) ·
//      狀態 (六態全部).
//   5. Card rendering: six status badges (spec copy), executor display
//      (member name / 外包 代號·模型·投入度 / 未指派), SERVER progress
//      passthrough, transitional states (未指派→等待指派, 有執行者無節點→
//      規劃中), gate projection (announced dashed vs armed solid), parallel
//      stage label, deps chips, waiting_external reason row.
//   6. 終止 is double-confirmed (ConfirmModal), non-terminal only, and keeps
//      the task listed once 終止 is ticked.
//   7. The embedded reply card is the SHARED M2 interior: answering by option
//      flips it to answered in place.
//   8. The message box posts one ordinary chat message to the executor and is
//      DISABLED while unassigned.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import {
  mockApi,
  __resetMock,
  __injectMockTask,
  __injectMockMember,
  __injectMockOutsourceWorker,
  __injectMockTaskType,
  __injectMockReplyCard,
} from "../api/mock";
import { api } from "../api";
// The 篩選 fields moved INSIDE a FilterPanel and only bite on 套用篩選 (T-93
// round 3, owner 2026-09-06) — so every filter gesture in this file goes
// through the shared driver rather than clicking a bare dropdown.
import {
  toggleFilter,
  expandLane,
  waitForListedCards,
  applyIdFilter,
  chooseGroupBy,
  clearAllFilters,
  openDisplayOptions,
} from "../test/tasksFilter";
import { useEnglishLocale } from "../test/effortOptions";
import { ApiError } from "../api/errors";
import { codeForStatus } from "../api/errorCodes";
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

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要現在同步到 Jira 嗎？",
    body: "",
    options: [{ text: "核可，直接同步上去", aiPick: false }, { text: "先不要", aiPick: true }],
    selectMode: "single",
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


function seedDueTasks(now: number) {
  __injectMockTask(mkTask({ title: "逾期的自由代辦", dueTs: now - 3600 }));
  __injectMockTask(
    mkTask({ title: "逾期的手冊任務", typeKey: "deploy", dueTs: now - 7200 })
  );
  __injectMockTask(mkTask({ title: "剛好到期", dueTs: now }));
  __injectMockTask(mkTask({ title: "快到期的", dueTs: now + 3600 }));
  __injectMockTask(mkTask({ title: "整整一天後", dueTs: now + 86_400 }));
  __injectMockTask(mkTask({ title: "一天又一秒後", dueTs: now + 86_401 }));
  __injectMockTask(mkTask({ title: "還早的", dueTs: now + 5 * 86_400 }));
  __injectMockTask(mkTask({ title: "沒有到期日的" }));
  __injectMockTask(
    mkTask({
      title: "已完成的逾期",
      status: "done",
      closedTs: now - 60,
      dueTs: now - 3600,
    })
  );
}
const laneKeys = () =>
  [...document.querySelectorAll('[data-testid^="lane-toggle-"]')].map((el) =>
    el.getAttribute("data-testid")!.slice("lane-toggle-".length)
  );
const laneTitles = (key: string) =>
  [
    ...document.querySelectorAll(
      `[data-testid="lane-${key}"] .task-card__title`
    ),
  ].map((el) => el.textContent);
const laneCount = (key: string) =>
  document.querySelector(`[data-testid="lane-count-${key}"]`)?.textContent;
const laneToggle = (key: string) =>
  document.querySelector(`[data-testid="lane-toggle-${key}"]`)!;

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  localStorage.clear();
});

// This suite installs spies (api.getTask, and T-a3e4's mockApi.listTasks stub).
// There is no global restoreMocks in vite.config.ts, so an un-restored spy would
// leak into every later test in the file — a stubbed listTasks would quietly
// change the population they render. Restore after each, the same way
// TaskCard.deps.test.tsx does.
afterEach(() => vi.restoreAllMocks());

describe("TasksPage", () => {
  it("shows the 目前沒有任務 empty state when there is nothing", async () => {
    const { findByTestId } = renderPage();
    const empty = await findByTestId("tasks-empty");
    expect(empty.textContent).toBe("目前沒有任務");
  });

  it("does NOT say 目前沒有任務 when the archive is non-empty (T-a3e4)", async () => {
    // 目前沒有任務 is a claim about the WHOLE workshop, and the list fetch now
    // only asks for the ticked statuses — so an empty DEFAULT view over a
    // workshop that has finished tasks must read as the FILTERED empty state.
    // 🔴 Not true on e7120c5: there the same fixture fetched ?open=true, got
    // zero rows, and asserted 目前沒有任務 over five archived tasks.
    for (let i = 0; i < 5; i += 1) {
      __injectMockTask(
        mkTask({
          title: `早就做完的 ${i}`,
          status: "done",
          closedTs: Date.now() / 1000 - 100,
        })
      );
    }
    const { findByTestId } = renderPage();
    const empty = await findByTestId("tasks-empty-filtered");
    expect(empty.textContent).toBe("沒有符合篩選條件的任務");
    expect(document.querySelector('[data-testid="tasks-empty"]')).toBeNull();
  });

  // ── T-a3e4: a residual 轉派中 lock on a CLOSED task is not an intent ───────
  // Terminate never clears task.lock, so "reassign then terminate" leaves
  // status=terminated + lock=reassigning behind for good — and the 狀態 dropdown
  // ticks 轉派中 by default, so without a terminal guard a default view the owner
  // never touched surfaces a closed task. The rule has THREE identical copies
  // (server / this page's matchesStatus / the mock).
  // 🔴 The two tests below exist because ONE page-level test cannot see either
  // copy: the mock filters, then matchesStatus filters again, so breaking either
  // one alone leaves the other covering for it (measured — a single page test
  // stayed green under both mutants). Each test therefore isolates one copy.
  it("the MOCK's status set drops a terminated+locked row (server rule mirror)", async () => {
    const residue = mkTask({
      title: "反悔終止的",
      status: "terminated",
      lock: "reassigning",
      closedTs: Date.now() / 1000 - 50,
    });
    const live = mkTask({
      title: "真的在轉派",
      status: "in_progress",
      lock: "reassigning",
    });
    __injectMockTask(residue);
    __injectMockTask(live);
    // Straight at the adapter — no page, so matchesStatus cannot cover for it.
    const rows = await mockApi.listTasks({
      statuses: [
        "not_started",
        "in_progress",
        "waiting_owner",
        "waiting_external",
        "reassigning",
      ],
    });
    const ids = rows.map((r) => r.id);
    expect(ids).toContain(live.id);
    expect(ids).not.toContain(residue.id);
    // …and asking for 已終止 explicitly still returns it: not unreachable, just
    // not 轉派中.
    const closed = await mockApi.listTasks({ statuses: ["terminated"] });
    expect(closed.map((r) => r.id)).toContain(residue.id);
  });

  it("matchesStatus drops a terminated+locked row even if the server sends one", async () => {
    // The page's own copy, isolated by making the fetch return the residue row
    // regardless — which is also the real transient case: rows from an earlier,
    // wider ask are still in state while a narrower refetch is in flight.
    const residue = mkTask({
      title: "反悔終止的",
      status: "terminated",
      lock: "reassigning",
      closedTs: Date.now() / 1000 - 50,
    });
    const live = mkTask({
      title: "真的在轉派",
      status: "in_progress",
      lock: "reassigning",
    });
    vi.spyOn(mockApi, "listTasks").mockResolvedValue([residue, live]);
    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    const titles = cards.map((c) => c.textContent ?? "");
    expect(titles.some((t) => t.includes("真的在轉派"))).toBe(true);
    expect(titles.some((t) => t.includes("反悔終止的"))).toBe(false);
    expect(cards).toHaveLength(1);
  });

  it("lists open tasks by priority 高→中→低→凍結 (凍結墊底), createdTs newest-first within a level, and by default (不分組, nothing stored) the closed ones after them in one untitled list with no lanes", async () => {
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
    __injectMockTask(
      mkTask({
        title: "高的已完成",
        priority: "high",
        status: "done",
        closedTs: now - 300,
      })
    );
    __injectMockTask(
      mkTask({
        title: "低的剛終止",
        priority: "low",
        status: "terminated",
        closedTs: now - 30,
      })
    );

    const expected = ["高的晚建", "高的早建", "中的", "低的", "凍結的"];
    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    const titles = cards.map(
      (c) => c.querySelector(".task-card__title")?.textContent
    );
    expect(titles).toEqual(expected);
    expect(laneKeys()).toEqual([]);
    expect(openDisplayOptions().value).toBe("none");
    expect(
      document.querySelector('[data-testid="task-list"]')?.className
    ).toBe("tasks__list");

    toggleFilter("filter-status", "done");
    toggleFilter("filter-status", "terminated");
    fireEvent.click(document.querySelector('[data-testid="filter-status"]')!);
    const listTitles = () =>
      [
        ...document.querySelectorAll(
          '[data-testid="task-list"] .task-card__title'
        ),
      ].map((el) => el.textContent);
    await waitFor(() => expect(listTitles()).toHaveLength(7));
    expect(laneKeys()).toEqual([]);
    expect(listTitles()).toEqual([...expected, "低的剛終止", "高的已完成"]);
    expect(document.body.textContent).not.toContain("未結案");
    expect(document.body.textContent).not.toContain("已結案");
  });

  it("puts picked done+terminated tasks in the lane of their due date, newest close first and collapsed behind the open task's lane; ticking only the 已結案 group leaves only closed tasks", async () => {
    __injectMockTask(
      mkTask({ title: "還在跑", dueTs: Date.now() / 1000 + 10 * 86_400 })
    );
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

    const { findByTestId, queryByTestId, findAllByTestId, getByTestId, queryByText } =
      renderPage();
    chooseGroupBy("due");
    // Default view EXCLUDES the two terminal states (T-be18 #2): only the live
    // task shows and there is no 沒有到期日 lane for the closed ones yet.
    expect((await findAllByTestId("task-card")).length).toBe(1);
    expect(queryByTestId("lane-nodue")).toBeNull();

    toggleFilter("filter-status", "done");
    toggleFilter("filter-status", "terminated");

    const toggle = await findByTestId("lane-toggle-nodue");
    await waitFor(() => expect(toggle.textContent).toBe("沒有到期日 · 2"));
    expect(laneKeys()).toEqual(["later", "nodue"]);
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(laneTitles("nodue")).toEqual([]);
    expect((await findAllByTestId("task-card")).length).toBe(1);

    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(laneTitles("nodue")).toEqual(["被終止的", "做完的"]);

    fireEvent.click(toggle);
    expect(laneTitles("nodue")).toEqual([]);

    const groupBox = (g: string) =>
      document.querySelector<HTMLInputElement>(
        `[data-testid="filter-status-group-${g}"] input`
      )!;
    if (getByTestId("filter-status").getAttribute("aria-expanded") !== "true") {
      fireEvent.click(getByTestId("filter-status"));
    }
    fireEvent.click(groupBox("open"));
    fireEvent.click(groupBox("closed"));
    expect(groupBox("open").checked).toBe(false);
    expect(groupBox("closed").checked).toBe(true);
    expect(getByTestId("filter-status").textContent?.trim()).toBe("已結案");
    await waitFor(() => expect(laneKeys()).toEqual(["nodue"]));
    await expandLane("nodue", 2);
    expect(laneTitles("nodue")).toEqual(["被終止的", "做完的"]);
    expect(queryByText("還在跑")).toBeNull();
  });

  it("multi-selects executor / type / status; emptying every axis shows 顯示全部; no-match empty state", async () => {
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
    // The default view already narrows (terminals hidden) → 清除篩選 shows from
    // the start (T-50bb). ⚠️ This assertion was briefly REMOVED during T-118,
    // when I deleted the button along with the 已篩選 strip owner had named; he
    // asked for the button back (c-2423dba8b65b) and it is back in the field
    // row, so the original assertion stands unchanged.
    expect((await findAllByTestId("task-card")).length).toBe(3);
    expect(queryByTestId("tasks-filter-clear")).not.toBeNull();

    // 執行者 = 外包 (assigned outsource only — 未指派 is its own option).
    toggleFilter("filter-executor", "outsource");
    await waitFor(() => {
      const cards = document.querySelectorAll('[data-testid="task-card"]');
      expect(cards).toHaveLength(1);
      expect(cards[0].textContent).toContain("外包的");
    });
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
    fireEvent.click(getByTestId("tasks-filter-clear"));
    await waitFor(() =>
      expect(
        document.querySelectorAll('[data-testid="task-card"]')
      ).toHaveLength(3)
    );
    expect(queryByTestId("tasks-filter-clear")).toBeNull();

    // 類型 = 自由代辦 (no type key).
    toggleFilter("filter-type", "adhoc");
    await waitFor(() =>
      expect(
        document.querySelectorAll('[data-testid="task-card"]')
      ).toHaveLength(2)
    );
  });

  it("類型篩選 option shows the manual's DISPLAY name, a built-in one in the UI language; a deleted manual's key falls back to itself (T-fa76)", async () => {
    useEnglishLocale();
    __injectMockTaskType({
      typeKey: "tm-aaaabbbbcccc",
      displayName: "審查 PR",
      purpose: "",
    });
    __injectMockTask(
      mkTask({ title: "有顯示名", typeKey: "tm-aaaabbbbcccc" })
    );
    __injectMockTask(mkTask({ title: "手冊已刪", typeKey: "gone-type" }));
    __injectMockTask(mkTask({ title: "內建", typeKey: "builtin-role-design" }));

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
    expect(getByTestId("filter-type-opt-builtin-role-design").textContent).toContain(
      "Create / edit role"
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
    renderPage();
    // Terminals are hidden by default — tick 已完成 + 終止 so all six render.
    toggleFilter("filter-status", "done");
    toggleFilter("filter-status", "terminated");
    await waitForListedCards(6);
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
    // T-1d82: the row now carries 編號 + 標題 in two spans, so the assertion
    // targets the 編號 span rather than the row's whole textContent.
    expect(
      card.querySelector('[data-testid="task-dep"] .task-card__dep-no')
        ?.textContent
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

  it("終止 double-confirms; the terminated task leaves the default view, and 終止 reveals it in the list again (action hidden on closed tasks)", async () => {
    __injectMockTask(mkTask({ title: "要被終止的" }));
    const { findByTestId, queryByTestId } = renderPage();

    // v5: 終止 lives on the 狀態 badge dropdown.
    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-terminate"));
    // Second step: the ConfirmModal — cancelling does nothing yet.
    const modal = await findByTestId("terminate-confirm");
    expect(modal.textContent).toContain("要被終止的");
    fireEvent.click(await findByTestId("terminate-confirm-btn"));

    // Its new 終止 state is excluded by the default filter → the task drops out
    // of view entirely.
    await waitFor(() =>
      expect(document.querySelectorAll('[data-testid="task-card"]')).toHaveLength(
        0
      )
    );

    toggleFilter("filter-status", "terminated");
    await waitForListedCards(1);
    const card = (await findByTestId("task-list")).querySelector(
      '[data-testid="task-card"]'
    )!;
    expect(
      card.querySelector('[data-testid="task-status"]')?.textContent
    ).toBe("終止");
    // Terminal task: the badge STILL drops its menu (owner ruling 2026-07-17 —
    // 「照字面永遠出，已結案時兩項變灰不可點」), but 終止 there is now greyed and
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
    // The SHARED M2 interior: the ai_pick option carries the AI 建議 tag.
    // Each chip WHOLE — its ordinal, its wording and exactly the tags earned.
    // The tagged
    // option is deliberately NOT the first one: a chip that reads its own
    // position instead of its own ai_pick flag tags the wrong chip here.
    const options = embedded.querySelectorAll(".reply-option");
    expect([...options].map((e) => e.textContent)).toEqual([
      "1核可，直接同步上去",
      "2先不要AI 建議",
    ]);

    // A single-select card answers on the CLICK → it flips answered in place.
    fireEvent.click(options[1]);
    await waitFor(async () => {
      expect(
        (await findByTestId("task-reply-card")).querySelector(
          '[data-testid="final-answer"]'
        )?.textContent
      ).toBe("你選的AI 建議先不要");
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
      const thread = await api.listChat("mira");
      expect(thread.map((m) => m.body)).toContain(`[TaskID=${task.taskNo}] 先做 P0 的部分`);
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
  it("saves a due date picked on a card and shows it on the card", async () => {
    __injectMockTask(mkTask({ title: "要設到期日" }));
    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-due"));
    const input = await findByTestId("task-due-input");
    fireEvent.change(input, { target: { value: "2099-10-30" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(async () =>
      expect((await findByTestId("task-due")).textContent).toBe("2099/10/30（五）")
    );
  });

  it("groups tasks into 已逾期 / 即將到期 / 之後到期 / 沒有到期日 lanes by due date alone, closed ones included, most urgent first, each titled by its name and count", async () => {
    const now = 1_900_000_000;
    vi.useFakeTimers({ now: now * 1000, toFake: ["Date"] });
    try {
      seedDueTasks(now);
      __injectMockTask(
        mkTask({
          title: "已終止沒到期日",
          status: "terminated",
          closedTs: now - 30,
        })
      );
      const { findByText } = renderPage();
      await findByText("逾期的手冊任務");
      chooseGroupBy("due");
      toggleFilter("filter-status", "done");
      toggleFilter("filter-status", "terminated");
      await waitFor(() => expect(laneCount("overdue")).toBe(" · 4"));
      await waitFor(() => expect(laneCount("nodue")).toBe(" · 2"));

      expect(laneKeys()).toEqual(["overdue", "soon", "later", "nodue"]);
      expect(
        laneKeys().map((key) => laneToggle(key).getAttribute("aria-expanded"))
      ).toEqual(["true", "false", "false", "false"]);
      expect(laneTitles("soon")).toEqual([]);
      for (const key of ["soon", "later", "nodue"]) {
        fireEvent.click(laneToggle(key));
      }
      expect(laneTitles("overdue")).toEqual([
        "逾期的手冊任務",
        "逾期的自由代辦",
        "已完成的逾期",
        "剛好到期",
      ]);
      expect(laneTitles("soon")).toEqual(["快到期的", "整整一天後"]);
      expect(laneTitles("later")).toEqual(["一天又一秒後", "還早的"]);
      expect(laneTitles("nodue")).toEqual(["沒有到期日的", "已終止沒到期日"]);
      expect(laneToggle("overdue").textContent).toBe("已逾期 · 4");
      expect(laneToggle("soon").textContent).toBe("即將到期 · 2");
      expect(laneToggle("later").textContent).toBe("之後到期 · 2");
      expect(laneToggle("nodue").textContent).toBe("沒有到期日 · 2");

      const closedTitles = new Set(["已完成的逾期", "已終止沒到期日"]);
      for (const [lane, tone] of [
        ["overdue", "overdue"],
        ["soon", "soon"],
        ["later", "later"],
        ["nodue", "none"],
      ]) {
        if (lane === "nodue") continue;
        const cards = [
          ...document.querySelectorAll(`[data-testid="lane-${lane}"] .task-card`),
        ].filter(
          (card) =>
            !closedTitles.has(card.querySelector(".task-card__title")!.textContent!)
        );
        expect(cards.length).toBeGreaterThan(0);
        for (const card of cards) {
          const date = card.querySelector('[data-testid="task-due"]')!;
          if (tone === "later") expect(date.className).not.toContain("task-card__due-date--");
          else expect(date.classList.contains(`task-card__due-date--${tone}`)).toBe(true);
        }
      }

      const closedOverdue = [
        ...document.querySelectorAll('[data-testid="lane-overdue"] .task-card'),
      ].find(
        (card) =>
          card.querySelector(".task-card__title")!.textContent === "已完成的逾期"
      )!;
      expect(
        closedOverdue.querySelector('[data-testid="task-due"]')!.className
      ).not.toContain("task-card__due-date--");
    } finally {
      vi.useRealTimers();
    }
  });

  it("files a task under 即將到期 by its own lead, else by the workspace lead", async () => {
    const now = 1_900_000_000;
    vi.useFakeTimers({ now: now * 1000, toFake: ["Date"] });
    try {
      await api.patchServerSettings({ taskDueSoonLeadMinutes: 4320 });
      __injectMockTask(mkTask({ title: "全站三天內", dueTs: now + 2 * 86_400 }));
      __injectMockTask(
        mkTask({ title: "自己七天內", dueTs: now + 5 * 86_400, dueSoonLeadMinutes: 10080 })
      );
      __injectMockTask(
        mkTask({ title: "自己只提前一天", dueTs: now + 2 * 86_400, dueSoonLeadMinutes: 1440 })
      );
      __injectMockTask(mkTask({ title: "全站三天外", dueTs: now + 10 * 86_400 }));
      const { findByText } = renderPage();
      await findByText("全站三天內");
      chooseGroupBy("due");
      fireEvent.click(laneToggle("later"));
      expect(laneKeys()).toEqual(["soon", "later"]);
      expect(laneTitles("soon")).toEqual(["全站三天內", "自己七天內"]);
      expect(laneTitles("later")).toEqual(["自己只提前一天", "全站三天外"]);
    } finally {
      vi.useRealTimers();
    }
  });

  it("moves a task into 即將到期 and turns its card's date orange when another tab raises the workspace lead", async () => {
    const now = 1_900_000_000;
    vi.useFakeTimers({ now: now * 1000, toFake: ["Date"] });
    try {
      __injectMockTask(mkTask({ title: "兩天後到期", dueTs: now + 2 * 86_400 }));
      const { findByText } = renderPage();
      await findByText("兩天後到期");
      chooseGroupBy("due");
      expect(laneKeys()).toEqual(["later"]);
      expect(
        (await findByText("兩天後到期"))
          .closest(".task-card")!
          .querySelector('[data-testid="task-due"]')!.className
      ).not.toContain("task-card__due-date--");

      await act(async () => {
        await mockApi.patchServerSettings({ taskDueSoonLeadMinutes: 4320 });
      });

      await waitFor(() => expect(laneKeys()).toEqual(["soon"]));
      const date = (await findByText("兩天後到期"))
        .closest(".task-card")!
        .querySelector('[data-testid="task-due"]')!;
      expect(date.classList.contains("task-card__due-date--soon")).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("puts the filter row first on the page, with no count line above it, while overdue and due-soon tasks are listed", async () => {
    seedDueTasks(Date.now() / 1000);
    const { getByTestId, findByText } = renderPage();
    await findByText("逾期的手冊任務");
    await findByText("快到期的");
    expect(getByTestId("tasks-filter").previousElementSibling).toBeNull();
  });

  it("applies the filters before grouping and leaves out a lane the filters emptied", async () => {
    seedDueTasks(Date.now() / 1000);
    renderPage();
    chooseGroupBy("due");
    await waitFor(() =>
      expect(laneKeys()).toEqual(["overdue", "soon", "later", "nodue"])
    );
    toggleFilter("filter-type", "deploy");
    await waitFor(() => expect(laneKeys()).toEqual(["overdue"]));
    expect(laneTitles("overdue")).toEqual(["逾期的手冊任務"]);
    expect(laneCount("overdue")).toBe(" · 1");
  });

  it("filters by 到期 offering only 已逾期 / 快到期, OR'd, ANDed with the other axes, never counting a closed task as overdue, and 清除篩選 clears it", async () => {
    const now = 1_900_000_000;
    vi.useFakeTimers({ now: now * 1000, toFake: ["Date"] });
    try {
      seedDueTasks(now);
      __injectMockTask(
        mkTask({ title: "自己提前七天", dueTs: now + 5 * 86_400 + 60, dueSoonLeadMinutes: 10080 })
      );
      const { findByText, getByTestId } = renderPage();
      await findByText("逾期的手冊任務");
      toggleFilter("filter-status", "done");
      await findByText("已完成的逾期");
      const shown = () =>
        [...document.querySelectorAll(".task-card__title")].map((el) => el.textContent).sort();

      fireEvent.click(getByTestId("filter-due"));
      expect(
        [...document.querySelectorAll('[data-testid^="filter-due-opt-"]')].map(
          (el) => [el.getAttribute("data-testid"), el.textContent?.trim()]
        )
      ).toEqual([
        ["filter-due-opt-overdue", "已逾期"],
        ["filter-due-opt-soon", "快到期"],
      ]);

      toggleFilter("filter-due", "overdue");
      await waitFor(() =>
        expect(shown()).toEqual(["剛好到期", "逾期的手冊任務", "逾期的自由代辦"].sort())
      );
      expect(getByTestId("filter-due").textContent).toContain("已逾期");

      toggleFilter("filter-due", "soon");
      await waitFor(() =>
        expect(shown()).toEqual(
          ["剛好到期", "逾期的手冊任務", "逾期的自由代辦", "快到期的", "整整一天後", "自己提前七天"].sort()
        )
      );

      toggleFilter("filter-type", "deploy");
      await waitFor(() => expect(shown()).toEqual(["逾期的手冊任務"]));
      toggleFilter("filter-type", "deploy");

      fireEvent.click(getByTestId("tasks-filter-clear"));
      await waitFor(() => expect(getByTestId("filter-due").textContent).toBe("所有到期"));
    } finally {
      vi.useRealTimers();
    }
  });

  it("expands a collapsed lane from its header and collapses it again, and collapses the first lane, leaving the other lanes as they were", async () => {
    seedDueTasks(Date.now() / 1000);
    const { findByText, queryByText } = renderPage();
    await findByText("逾期的手冊任務");
    chooseGroupBy("due");
    const toggle = laneToggle("later");
    const region = document.getElementById(
      toggle.getAttribute("aria-controls")!
    )!;
    expect(region.getAttribute("role")).toBe("region");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(region.hidden).toBe(true);
    expect(queryByText("還早的")).toBeNull();
    expect(laneCount("later")).toBe(" · 2");

    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(region.hidden).toBe(false);
    expect(laneTitles("later")).toEqual(["一天又一秒後", "還早的"]);

    fireEvent.click(laneToggle("overdue"));
    expect(laneToggle("overdue").getAttribute("aria-expanded")).toBe("false");
    expect(queryByText("逾期的手冊任務")).toBeNull();
    expect(laneCount("overdue")).toBe(" · 3");
    expect(
      laneKeys().map((key) => laneToggle(key).getAttribute("aria-expanded"))
    ).toEqual(["false", "false", "true", "false"]);

    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(queryByText("還早的")).toBeNull();
  });

  it("moves tasks to 即將到期 and then 已逾期 as the clock passes each boundary, even before the counts refetch, keeping the open lane open until it empties and then opening the new first lane", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const now = Date.now() / 1000;
      __injectMockTask(mkTask({ title: "先到期的", dueTs: now + 86_400 + 10 }));
      __injectMockTask(mkTask({ title: "後到期的", dueTs: now + 86_400 + 40 }));
      __injectMockTask(mkTask({ title: "沒有期限的" }));
      vi.spyOn(api, "getTaskCount").mockResolvedValue({
        open: 3,
        total: 3,
        overdue: 0,
        dueSoon: 0,
        dueNextChangeTs: null,
      });
      const { findByText } = renderPage();
      await findByText("先到期的");
      chooseGroupBy("due");
      const expandedStates = () =>
        laneKeys().map((key) => laneToggle(key).getAttribute("aria-expanded"));
      expect(laneKeys()).toEqual(["later", "nodue"]);
      expect(expandedStates()).toEqual(["true", "false"]);

      await act(async () => {
        vi.advanceTimersByTime(20_000);
      });
      await waitFor(() =>
        expect(laneKeys()).toEqual(["soon", "later", "nodue"])
      );
      expect(expandedStates()).toEqual(["false", "true", "false"]);
      expect(laneTitles("later")).toEqual(["後到期的"]);

      await act(async () => {
        vi.advanceTimersByTime(30_000);
      });
      await waitFor(() => expect(laneKeys()).toEqual(["soon", "nodue"]));
      expect(expandedStates()).toEqual(["true", "false"]);
      expect(laneTitles("soon")).toEqual(["先到期的", "後到期的"]);

      await act(async () => {
        vi.advanceTimersByTime(86_400_000 - 30_000);
      });
      await waitFor(() =>
        expect(laneKeys()).toEqual(["overdue", "soon", "nodue"])
      );
      expect(expandedStates()).toEqual(["false", "true", "false"]);
      expect(laneTitles("soon")).toEqual(["後到期的"]);

      await act(async () => {
        vi.advanceTimersByTime(30_000);
      });
      await waitFor(() => expect(laneKeys()).toEqual(["overdue", "nodue"]));
      expect(expandedStates()).toEqual(["true", "false"]);
      expect(laneTitles("overdue")).toEqual(["先到期的", "後到期的"]);
    } finally {
      vi.useRealTimers();
    }
  });

  it("moves a closed task picked in 狀態 to 即將到期 as the clock passes its boundary, like an open one", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const now = Date.now() / 1000;
      __injectMockTask(
        mkTask({ title: "做完的", status: "done", closedTs: now - 60, dueTs: now + 86_400 + 10 })
      );
      vi.spyOn(api, "getTaskCount").mockResolvedValue({
        open: 0,
        total: 1,
        overdue: 0,
        dueSoon: 0,
        dueNextChangeTs: null,
      });
      renderPage();
      chooseGroupBy("due");
      toggleFilter("filter-status", "done");
      await waitFor(() => expect(laneKeys()).toEqual(["later"]));

      await act(async () => {
        vi.advanceTimersByTime(20_000);
      });
      await waitFor(() => expect(laneKeys()).toEqual(["soon"]));
    } finally {
      vi.useRealTimers();
    }
  });

  it("shows an id-filtered task that sits in a collapsed lane", async () => {
    const target = mkTask({
      title: "收起來的那張",
      dueTs: Date.now() / 1000 + 5 * 86_400,
    });
    __injectMockTask(target);
    __injectMockTask(mkTask({ title: "旁邊的" }));
    const { findByText, queryByText } = renderPage();
    await findByText("收起來的那張");
    chooseGroupBy("due");
    fireEvent.click(laneToggle("later"));
    expect(queryByText("收起來的那張")).toBeNull();

    applyIdFilter(target.id);
    await findByText("收起來的那張");
    expect(laneToggle("later").getAttribute("aria-expanded")).toBe("true");
    expect(queryByText("旁邊的")).toBeNull();
  });

  it("shows a #tasks/<id> target that sat in a collapsed lane and was hidden by 到期 = 已逾期, clearing the 到期 filter", async () => {
    const target = mkTask({
      title: "連結指向的",
      dueTs: Date.now() / 1000 + 3600,
    });
    __injectMockTask(target);
    __injectMockTask(mkTask({ title: "沒有到期日的" }));
    const { findByText, findByTestId, queryByText, getByTestId } = renderPage();
    await findByText("連結指向的");
    chooseGroupBy("due");
    fireEvent.click(laneToggle("soon"));
    expect(queryByText("連結指向的")).toBeNull();
    toggleFilter("filter-due", "overdue");
    await findByTestId("tasks-empty-filtered");

    act(() => {
      window.location.hash = `#tasks/${target.id}`;
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    await findByText("連結指向的");
    expect(laneToggle("soon").getAttribute("aria-expanded")).toBe("true");
    expect(getByTestId("filter-due").textContent).toBe("所有到期");
  });

  it("groups by 負責人 in the filter's option order: 外包, 未指派, then each member, closed tasks after the open ones in their owner's lane", async () => {
    __injectMockMember({ id: "kai", kind: "staff", name: "Kai" });
    __injectMockTask(mkTask({ title: "Kai 的", executorId: "kai" }));
    __injectMockTask(mkTask({ title: "Mira 的高", priority: "high" }));
    __injectMockTask(mkTask({ title: "Mira 的低", priority: "low" }));
    __injectMockTask(
      mkTask({ title: "沒人接", executorKind: "outsource", executorId: "" })
    );
    __injectMockTask(
      mkTask({ title: "外包的", executorKind: "outsource", executorId: "ow-x" })
    );
    __injectMockTask(
      mkTask({
        title: "Mira 的結案",
        priority: "high",
        status: "done",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    const { findByText } = renderPage();
    await findByText("Kai 的");
    toggleFilter("filter-status", "done");
    await waitForListedCards(6);
    chooseGroupBy("executor");
    await waitFor(() =>
      expect(laneKeys()).toEqual(["outsource", "unassigned", "mira", "kai"])
    );
    expect(laneToggle("outsource").textContent).toBe("負責人：外包 · 1");
    expect(laneToggle("unassigned").textContent).toBe("負責人：未指派 · 1");
    expect(laneToggle("mira").textContent).toBe("負責人：Mira · 3");
    expect(laneToggle("kai").textContent).toBe("負責人：Kai · 1");
    expect(
      laneKeys().map((key) => laneToggle(key).getAttribute("aria-expanded"))
    ).toEqual(["true", "false", "false", "false"]);
    expect(laneTitles("outsource")).toEqual(["外包的"]);
    fireEvent.click(laneToggle("mira"));
    expect(laneTitles("mira")).toEqual(["Mira 的高", "Mira 的低", "Mira 的結案"]);
  });

  it("groups by 類型 under each manual's display name, 自由代辦 last", async () => {
    __injectMockTaskType({ typeKey: "deploy", displayName: "部署", purpose: "" });
    __injectMockTaskType({ typeKey: "audit", displayName: "稽核", purpose: "" });
    __injectMockTask(mkTask({ title: "部署甲", typeKey: "deploy" }));
    __injectMockTask(mkTask({ title: "稽核甲", typeKey: "audit" }));
    __injectMockTask(mkTask({ title: "隨手的" }));
    const { findByText } = renderPage();
    await findByText("部署甲");
    chooseGroupBy("type");
    await waitFor(() =>
      expect(laneKeys()).toEqual(["audit", "deploy", "adhoc"])
    );
    expect(laneToggle("audit").textContent).toBe("類型：稽核 · 1");
    expect(laneToggle("deploy").textContent).toBe("類型：部署 · 1");
    expect(laneToggle("adhoc").textContent).toBe("類型：自由代辦 · 1");
  });

  it("groups by 優先權 高 → 中 → 低 → 凍結, titled 優先權：高 and so on", async () => {
    __injectMockTask(mkTask({ title: "凍住的", priority: "frozen" }));
    __injectMockTask(mkTask({ title: "低的", priority: "low" }));
    __injectMockTask(mkTask({ title: "高的", priority: "high" }));
    __injectMockTask(mkTask({ title: "中的", priority: "mid" }));
    const { findByText } = renderPage();
    await findByText("高的");
    chooseGroupBy("priority");
    await waitFor(() =>
      expect(laneKeys()).toEqual(["high", "mid", "low", "frozen"])
    );
    expect(laneToggle("high").textContent).toBe("優先權：高 · 1");
    expect(laneToggle("frozen").textContent).toBe("優先權：凍結 · 1");
    expect(laneTitles("high")).toEqual(["高的"]);
  });

  it("groups by 狀態 in the 狀態 filter's order, the 已完成 / 終止 lanes after the open ones, each titled 狀態：<name> with its count", async () => {
    __injectMockTask(mkTask({ title: "等我的", status: "waiting_owner" }));
    __injectMockTask(mkTask({ title: "還沒開始", status: "not_started" }));
    __injectMockTask(mkTask({ title: "跑著的" }));
    __injectMockTask(
      mkTask({ title: "結掉的", status: "done", closedTs: Date.now() / 1000 })
    );
    __injectMockTask(
      mkTask({
        title: "喊停的",
        status: "terminated",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    const { findByText } = renderPage();
    await findByText("跑著的");
    toggleFilter("filter-status", "terminated");
    toggleFilter("filter-status", "done");
    await waitForListedCards(5);
    chooseGroupBy("status");
    await waitFor(() =>
      expect(laneKeys()).toEqual([
        "not_started",
        "in_progress",
        "waiting_owner",
        "done",
        "terminated",
      ])
    );
    expect(laneToggle("waiting_owner").textContent).toBe("狀態：等我回覆 · 1");
    expect(laneToggle("done").textContent).toBe("狀態：已完成 · 1");
    expect(laneToggle("terminated").textContent).toBe("狀態：終止 · 1");
    fireEvent.click(laneToggle("done"));
    expect(laneTitles("done")).toEqual(["結掉的"]);
  });

  it("opens 顯示方式 at the end of the filter row, after 清除篩選, and closes it on an outside click or Esc", async () => {
    seedDueTasks(Date.now() / 1000);
    const { findByText, getByTestId, queryByTestId } = renderPage();
    await findByText("逾期的手冊任務");
    const fields = getByTestId("tasks-filter-fields");
    const trigger = getByTestId("display-options");
    expect(trigger.textContent).toBe("顯示方式");
    expect(fields.lastElementChild!.contains(trigger)).toBe(true);
    expect(
      getByTestId("tasks-filter-clear").compareDocumentPosition(trigger) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
    expect(queryByTestId("group-by")).toBeNull();
    expect(queryByTestId("display-options-panel")).toBeNull();

    fireEvent.click(trigger);
    const panel = getByTestId("display-options-panel");
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(trigger.getAttribute("aria-controls")).toBe(panel.id);
    expect(panel.textContent).toContain("顯示方式");
    expect(panel.textContent).toContain("分組依據");
    const select = getByTestId("group-by") as HTMLSelectElement;
    expect(Array.from(select.options).map((o) => o.textContent)).toEqual([
      "不分組",
      "到期狀態",
      "優先權",
      "負責人",
      "類型",
      "狀態",
    ]);
    expect(panel.contains(select)).toBe(true);
    expect(select.closest(".filter-panel__actions")).not.toBeNull();
    expect(document.activeElement).toBe(panel);

    chooseGroupBy("executor");
    await waitFor(() => expect(laneKeys()).toEqual(["mira"]));
    expect(queryByTestId("display-options-panel")).not.toBeNull();

    fireEvent.mouseDown(panel);
    expect(queryByTestId("display-options-panel")).not.toBeNull();
    fireEvent.mouseDown(document.body);
    expect(queryByTestId("display-options-panel")).toBeNull();
    expect(trigger.getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(trigger);
    expect(queryByTestId("display-options-panel")).not.toBeNull();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(queryByTestId("display-options-panel")).toBeNull();
    expect(document.activeElement).toBe(trigger);
    expect(laneKeys()).toEqual(["mira"]);
  });

  it("keeps the chosen 分組依據 when 清除篩選 is pressed", async () => {
    __injectMockTask(mkTask({ title: "跑著的" }));
    __injectMockTask(mkTask({ title: "等我的", status: "waiting_owner" }));
    const { findByText, getByTestId } = renderPage();
    await findByText("跑著的");
    chooseGroupBy("status");
    toggleFilter("filter-type", "adhoc");
    await waitFor(() =>
      expect(laneKeys()).toEqual(["in_progress", "waiting_owner"])
    );

    fireEvent.click(getByTestId("tasks-filter-clear"));
    await waitFor(() =>
      expect(getByTestId("filter-type").textContent).toBe("所有類型")
    );
    expect(laneKeys()).toEqual(["in_progress", "waiting_owner"]);
    expect(laneToggle("in_progress").textContent).toBe("狀態：進行中 · 1");
    expect(openDisplayOptions().value).toBe("status");
  });

  it("remembers the chosen 分組依據 in this browser, so a fresh mount comes back grouped the same way", async () => {
    __injectMockTask(mkTask({ title: "跑著的" }));
    __injectMockTask(mkTask({ title: "等我的", status: "waiting_owner" }));
    const first = renderPage();
    await first.findByText("跑著的");
    chooseGroupBy("status");
    expect(localStorage.getItem("oc_tasks_group_by")).toBe("status");
    first.unmount();

    const { findByText } = renderPage();
    await findByText("跑著的");
    expect(laneKeys()).toEqual(["in_progress", "waiting_owner"]);
    expect(openDisplayOptions().value).toBe("status");
  });

  it("opens ungrouped when the stored 分組依據 is not one it knows", async () => {
    localStorage.setItem("oc_tasks_group_by", "deadline");
    __injectMockTask(mkTask({ title: "跑著的" }));
    const { findByText } = renderPage();
    await findByText("跑著的");
    expect(laneKeys()).toEqual([]);
    expect(openDisplayOptions().value).toBe("none");
  });

  it("opens ungrouped and still switches 分組依據 for the mount when localStorage throws", async () => {
    vi.spyOn(localStorage, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(localStorage, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    __injectMockTask(mkTask({ title: "跑著的" }));
    __injectMockTask(mkTask({ title: "等我的", status: "waiting_owner" }));
    const { findByText } = renderPage();
    await findByText("跑著的");
    expect(laneKeys()).toEqual([]);

    const uncaught: unknown[] = [];
    const onError = (e: ErrorEvent) => uncaught.push(e.error);
    window.addEventListener("error", onError);
    try {
      chooseGroupBy("status");
    } finally {
      window.removeEventListener("error", onError);
    }
    expect(uncaught).toEqual([]);
    await waitFor(() =>
      expect(laneKeys()).toEqual(["in_progress", "waiting_owner"])
    );
  });

  it("expands only the first lane again after a switch of 分組依據", async () => {
    seedDueTasks(Date.now() / 1000);
    const { findByText } = renderPage();
    await findByText("逾期的手冊任務");
    chooseGroupBy("due");
    fireEvent.click(laneToggle("overdue"));
    fireEvent.click(laneToggle("nodue"));
    const expandedStates = () =>
      laneKeys().map((key) => laneToggle(key).getAttribute("aria-expanded"));
    expect(expandedStates()).toEqual(["false", "false", "false", "true"]);
    chooseGroupBy("executor");
    await waitFor(() => expect(laneKeys()).toEqual(["mira"]));
    expect(laneToggle("mira").getAttribute("aria-expanded")).toBe("true");
    chooseGroupBy("due");
    await waitFor(() =>
      expect(laneKeys()).toEqual(["overdue", "soon", "later", "nodue"])
    );
    expect(expandedStates()).toEqual(["true", "false", "false", "false"]);
    expect(laneTitles("overdue")).toEqual([
      "逾期的手冊任務",
      "逾期的自由代辦",
      "剛好到期",
    ]);
    expect(laneTitles("nodue")).toEqual([]);
  });

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

    it("manually expanding a listed DONE task hydrates its steps via getTask", async () => {
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
      const { getByTestId, findByText, queryByText } = renderPage();

      // Terminals are filtered out by default — tick 已完成 to list the done task.
      toggleFilter("filter-status", "done");
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
      // card in an open lane — but the card itself must ALSO auto-expand
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

    it("KEEPS the #tasks/<id> anchor and says the id DOES NOT EXIST when the server answers 404", async () => {
      // owner 2026-09-05 (rc-428906235337) / 2026-09-06 option ①. A broken link
      // must be told apart from a link that was never filtering, and 「不存在」
      // from 「在，只是沒被載進來」: the shared 沒有符合篩選條件的任務 empty
      // state is what the page shows for a real task outside the loaded page,
      // so a 404 gets its own words.
      //
      // ⚠️ Anchor handling is also pinned in TasksPage.jump.test.tsx and
      // TasksPage.anchor-fetch; the denominator for this behaviour is every
      // file that renders this page, not only this one.
      __injectMockTask(mkTask({ title: "工作室裡確實有一張票" }));
      // The real failure path: GET /api/tasks/{id} rejects (404 / deleted), and
      // useTasks resolves the anchor WITH the id and a null task, so
      // anchorPending goes false with nothing to show.
      const getTaskSpy = vi
        .spyOn(api, "getTask")
        .mockRejectedValue(
          new ApiError(
            "http 404 for GET /api/tasks/t-does-not-exist",
            404,
            codeForStatus(404),
            "task 't-does-not-exist' not found"
          )
        );
      const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
      window.location.hash = "#tasks/t-does-not-exist";

      const { findByTestId, queryByTestId } = renderPage();

      // Wait for the anchor fetch to have been attempted and REJECTED, so every
      // assertion below reads the SETTLED page rather than a frame on the way
      // there. 🔴 This ordering is load-bearing: asserted before the settle, the
      // empty-state half passes even against the self-heal mutant (it catches a
      // transient frame), and only the hash half would have teeth.
      await waitFor(() =>
        expect(getTaskSpy).toHaveBeenCalledWith("t-does-not-exist")
      );
      await findByTestId("tasks-empty-filtered");

      // The anchor survives: the hash still carries the id the owner clicked.
      expect(window.location.hash).toBe("#tasks/t-does-not-exist");
      // A 404 renders the ordinary 沒有符合篩選條件的任務, awaited above. 目前沒有
      // 任務 may NOT appear — that would be a claim about a workshop which in
      // fact holds a task.
      expect(queryByTestId("tasks-empty")).toBeNull();
      // …and the one real task is NOT on screen: the anchor is still narrowing.
      expect(document.querySelectorAll('[data-testid="task-card"]')).toHaveLength(
        0
      );

      // And there is a way out — which is why keeping the anchor is not a trap:
      // 清除全部 shows on the 已篩選 strip while the anchor is set and clears it
      // with every other axis, restoring both the list and the hash.
      clearAllFilters();
      await waitFor(() =>
        expect(
          document.querySelectorAll('[data-testid="task-card"]')
        ).toHaveLength(1)
      );
      expect(window.location.hash).toBe("#tasks");

      warnSpy.mockRestore();
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

    const { findByText, findByTestId, queryByText } = renderPage();
    await findByText("活的");
    expect(queryByText("完成的")).toBeNull();
    expect(queryByText("終止的")).toBeNull();
    await waitForListedCards(1);
    expect((await findByTestId("filter-status")).textContent?.trim()).toBe("未結案");

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
    // follows the status filter, T-be18 #3).
    // The dropdown stays open across the tick (a click on 負責人 would SHUT it),
    // and the count basis is the applied set — there is no draft — so the
    // number moves on the tick.
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

  it("never offers an outsource row as a member executor filter", async () => {
    // GET /api/members now includes this row.  Give it a matching task count so
    // the zero-count hiding rule cannot mask a missing kind allowlist.
    __injectMockMember({ id: "ow-filter", kind: "outsource", name: "X-9" });
    __injectMockTask(
      mkTask({ title: "正職任務", executorId: "mira", status: "in_progress" })
    );
    __injectMockTask(
      mkTask({ title: "外包不該成為選項", executorId: "ow-filter", status: "in_progress" })
    );

    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("filter-executor"));
    await waitFor(() =>
      expect(document.querySelector('[data-testid="filter-executor-opt-mira"]')).not.toBeNull()
    );
    expect(document.querySelector('[data-testid="filter-executor-opt-ow-filter"]')).toBeNull();
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
    // The popover stays open across toggles; clicking filter-executor again
    // would SHUT it and the reads below would find nothing.
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

    const { findByText, queryByText } = renderPage();
    await findByText("跑著的");
    expect(queryByText("收工的")).toBeNull();

    toggleFilter("filter-status", "done");
    await waitForListedCards(2);
    await findByText("收工的");
    // The live task is still there — the tick added a status, it didn't replace.
    expect((await findByText("跑著的")).textContent).toContain("跑著的");
  });
});

// ── T-50bb: 清空每一軸 = 顯示全部 (the default view counts as a filter)
// 🔁 THIS BLOCK WAS 「清除篩選 semantics」 and it was named after a BUTTON. owner
// 2026-09-06 (c-c3d681fe05da) removed that button along with the 已篩選 strip it
// stood on. T-50bb's actual RULE survives untouched — clearing every axis must
// go straight to 顯示全部 (terminals included), NOT back to the terminal-hiding
// default view — so the tests below keep it and only change the gesture.
describe("TasksPage 顯示全部 semantics (T-50bb)", () => {
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

  it("the DEFAULT view already narrows: the terminals it hides are absent from the list", async () => {
    // The default status set is a real constraint, not "no filter".
    injectMixedTasks();
    const { findByText, queryByText } = renderPage();
    await findByText("活的");
    expect(queryByText("完成的"), "已完成 is excluded by default").toBeNull();
    expect(queryByText("終止的"), "終止 is excluded by default").toBeNull();
  });

  it("emptying every axis from the default view lists EVERYTHING — 已完成/終止 included", async () => {
    injectMixedTasks();
    const { findByText, findByTestId, queryByTestId, queryByText } =
      renderPage();
    await findByText("活的");
    expect(queryByText("完成的")).toBeNull();

    clearAllFilters();
    await waitForListedCards(3);
    await findByText("完成的");
    await findByText("終止的");
    expect((await findByText("活的")).textContent).toContain("活的");
    // Nothing narrows the list, and the 狀態 dropdown reads as unconstrained
    // (all boxes unchecked) — there is no 已篩選 strip, so the dropdown's own
    // state is what says so.
    expect(queryByTestId("tasks-filter-clear")).toBeNull();
    fireEvent.click(await findByTestId("filter-status"));
    const checkOf = (v: string) =>
      document.querySelector<HTMLInputElement>(
        `[data-testid="filter-status-opt-${v}"] input`
      )!;
    expect(checkOf("in_progress").checked).toBe(false);
    expect(checkOf("done").checked).toBe(false);
  });

  it("emptying every axis after editing filters ALSO goes straight to 顯示全部 (not back to the default view)", async () => {
    injectMixedTasks();
    const { findByText, findByTestId, queryByText } = renderPage();
    await findByText("活的");

    // Edit two axes: executor = mira, and un-tick 進行中 → nothing matches.
    toggleFilter("filter-executor", "mira");
    toggleFilter("filter-status", "in_progress");
    await findByTestId("tasks-empty-filtered");

    clearAllFilters();
    // Straight to 顯示全部: the live task is back AND the terminals surface
    // beside it — proof it did not stop at the terminal-hiding default view.
    await findByText("活的");
    await waitForListedCards(3);
    await findByText("完成的");
    expect(queryByText("終止的")).not.toBeNull();
  });
});
