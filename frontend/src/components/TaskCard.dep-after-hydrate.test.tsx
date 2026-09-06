// TaskCard — THE DEP JOIN MUST SURVIVE THE DETAIL HYDRATE (T-8115 follow-up).
//
// An expanded card holds TWO TaskViews at once (`TaskCard.tsx`: `const view =
// hasDetail ? detail : task`):
//   • `task`  — the LIST row (`GET /api/tasks`), which CARRIES `depTasks`
//               (the server-side dep join, TaskListItemDTO, T-a3e4).
//   • `view`  — the same row until the card is expanded, and from then on the
//               HYDRATED detail (`GET /api/tasks/{id}`), which carries NO
//               `dep_tasks` at all — the frozen spec puts that join on
//               TaskListItemDTO only (see `api/dtoParity.ts`,
//               PER_ITEM_DTO_GAPS.task).
//
// Everything around the dep block reads `view` (artifacts, steps, description).
// The dep block deliberately reads `task`. That is LOAD-BEARING and
// counter-intuitive, and this file is what keeps it that way: swap that one
// `task.depTasks` for `view.depTasks` and regression ② comes straight back —
// every dep on an EXPANDED card collapses to a bare short id, which is exactly
// what the user saw before T-a3e4 (「等 T-dep」 with no title, no status).
//
// 🔴 WHY THIS FILE EXISTS AT ALL: the whole dep suite
// (TaskCard.deps / .dep-status / .dep-taskno) passes `NOOP as never` for
// `onHydrate`, so `hasDetail` is never true and `view === task` throughout.
// Against those tests the `task` → `view` swap is INVISIBLE — measured
// 2026-08-01: the full 1675-test suite stayed green with the defect in place.
// A dep test that never expands the card cannot see this class of bug.
//
// The fake detail here is built with `projectSingleItem("task", row)` rather
// than hand-copied, so it drops exactly what the real single-item wire drops —
// a hand-built fixture that "forgot" depTasks would prove the same thing by
// accident, and one that kept it would prove nothing at all.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import { projectSingleItem } from "../api/dtoParity";
import type { Member } from "../types";
import type { TaskView, OutsourceWorkerView } from "../api/adapter";

const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
const noop = async () => {};
const workers: OutsourceWorkerView[] = [];

const DEP_ID = "t-1d8292a2f8db";
const DEP_TITLE = "先把資料庫遷移做完";

function mkTask(over: Partial<TaskView> = {}): TaskView {
  return {
    id: "t-35e06c8e63c8",
    taskNo: "T-35e0",
    title: "依賴別人的任務",
    typeKey: "",
    description: "詳細敘述",
    status: "in_progress",
    priority: "high",
    executorKind: "staff",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [DEP_ID],
    waitingReason: "",
    duplicateOf: "",
    createdTs: 1000,
    updatedTs: 2000,
    closedTs: null,
    progressDone: 0,
    progressTotal: 1,
    steps: [],
    // The server-side join the LIGHT list carries. A CLOSED dep on purpose:
    // 「已結案的 dep 仍講得出標題」 is the T-a3e4 property that regression ②
    // destroyed, and a closed task is also the one a re-pull of the open-only
    // list could never recover on its own.
    depTasks: [{ id: DEP_ID, taskNo: "T-1d82", title: DEP_TITLE, status: "done" }],
    ...over,
  } as TaskView;
}

function renderCard(
  task: TaskView,
  onHydrate: (id: string) => Promise<TaskView>
) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task}
        allTasks={[task]}
        members={[MIRA]}
        workers={workers}
        nowTs={3000}
        onTerminate={noop as never}
        onMarkDuplicate={noop as never}
        onSetPriority={noop as never}
        onReassign={noop as never}
        onSendMessage={noop as never}
        onHydrate={onHydrate}
      />
    </I18nProvider>
  );
}

/** What `GET /api/tasks/{id}` really answers with: the row minus the gapped
 *  fields. `projectSingleItem` reads PER_ITEM_DTO_GAPS, so this fake cannot be
 *  more generous than the wire even if the table changes. */
function singleItemOf(row: TaskView): TaskView {
  return projectSingleItem(
    "task",
    row as unknown as Record<string, unknown>
  ) as unknown as TaskView;
}

describe("the dep join survives the detail hydrate (T-8115 follow-up to T-a3e4)", () => {
  it("an EXPANDED, hydrated card still names its dep's title and status", async () => {
    const row = mkTask();
    // Sanity on the fixture itself: the wire really does drop the join, so the
    // assertions below are measuring the gap and not a typo. Without this, a
    // projectSingleItem that silently became the identity would make the whole
    // test vacuous.
    const detail = singleItemOf(mkTask({ description: "hydrated 詳細敘述" }));
    expect(detail.depTasks).toBeUndefined();

    const onHydrate = vi.fn(async () => detail);
    const { findByTestId, container } = renderCard(row, onHydrate);

    // Collapsed: the dep already resolves (this is the pre-existing T-a3e4
    // behaviour, and the control that proves the fixture is well-formed).
    expect(
      (await findByTestId("task-dep")).getAttribute("data-dep-state")
    ).toBe("closed");

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    // The hydrate really happened — otherwise `view === task` and this test
    // would pass against the defect for the wrong reason.
    expect(onHydrate).toHaveBeenCalledWith(row.id);
    expect(container.querySelector(".task-card__desc")?.textContent).toContain(
      "hydrated"
    );

    // 🔴 THE ASSERTION. After hydrate the dep must still be RESOLVED — not the
    // "unresolved" silence a detail-sourced read would produce.
    const dep = await findByTestId("task-dep");
    expect(dep.getAttribute("data-dep-state")).toBe("closed");
    expect(dep.querySelector(".task-card__dep-title")?.textContent).toBe(
      DEP_TITLE
    );
    expect(
      dep.querySelector('[data-testid="task-dep-status"]')
    ).not.toBeNull();
    // The short number stays the server's, not a bare long id.
    expect(dep.textContent).toContain("T-1d82");
    expect(dep.textContent).not.toContain(DEP_ID);
  });

  it("a dep the server says is GONE still reads as 查無此任務 after hydrate", async () => {
    // The other half of the three-state contract: an entry with an empty status
    // is "the task is gone", and hydrating must not downgrade that into the
    // weaker "nobody resolved this" silence — they render differently on
    // purpose (TaskCard.tsx), and only one of them earns 查無此任務.
    const row = mkTask({
      depTasks: [{ id: DEP_ID, taskNo: "T-1d82", title: "", status: "" }],
    } as Partial<TaskView>);
    const detail = singleItemOf(mkTask({ description: "hydrated 詳細敘述" }));

    const { findByTestId } = renderCard(row, vi.fn(async () => detail));
    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    expect((await findByTestId("task-dep")).getAttribute("data-dep-state")).toBe(
      "missing"
    );
  });
});
