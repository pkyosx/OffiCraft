// TaskCard — 進度條看得見 (T-ad21).
//
// WHY THIS FILE EXISTS
// --------------------
// owner reported 2026-07-17「進度 2/5 原本的進度條怎麼不見了？」. The recon
// (~/ai_workspace/dev/kyle-ad21-recon.md) REFUTED the r-55 regression
// hypothesis: the bar's JSX and its CSS have each been touched exactly once —
// at creation (5bad214) — and r-54/r-55 render it pixel-identically. There was
// no regression to fix.
//
// What the recon DID find is that the bar was completely BARE: across the whole
// suite, not one assertion touched `.task-card__progress-bar` or
// `.task-card__progress-fill`. Every progress assertion was on the TEXT
// (`data-testid="task-progress"`). So a real bar regression would have shipped
// green — the suite was never looking. This file closes that hole.
//
// The fixture space is deliberate (a fixture that misses a shape leaves the
// guard blind on exactly that shape — mutants vary the code, never the input):
//   · 0/0   — stepless; total 0 must NOT divide-by-zero into NaN width
//   · 0/1   — zero fill; the bar element must still be there (the shape in the
//             a20b 390px baseline where the bar reads as "missing" to the eye)
//   · 2/5   — owner's shape, AND re-planned (superseded nodes excluded from the
//             server counts, T-1aea) with a 2h+ elapsed text
//   · 3/3   — 100%
//   · 7/5   — done > total must clamp to 100%, never overflow
//
// The width assertions are the point: asserting the bar merely EXISTS would
// still pass if the fill collapsed to 0 on a 2/5 card, which is exactly the
// failure owner described.

import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import type { Member } from "../types";
import type { TaskView, TaskStepView, OutsourceWorkerView } from "../api/adapter";

let seq = 0;
function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`,
    name: `節點-${seq}`,
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
  return {
    id: "t-ad21",
    taskNo: "T-ad21",
    title: "進度條任務",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "high",
    executorKind: "member",
    executorId: "mira",
    creatorId: "owner",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: 1000,
    updatedTs: 2000,
    closedTs: null,
    progressDone: 0,
    progressTotal: 1,
    steps: [],
    ...over,
  };
}

const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
const noop = async () => {};
const workers: OutsourceWorkerView[] = [];

function renderCard(task: TaskView, nowTs = 3000) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task}
        allTasks={[task]}
        members={[MIRA]}
        workers={workers}
        nowTs={nowTs}
        onTerminate={noop as never}
        onMarkDuplicate={noop as never}
        onSetPriority={noop as never}
        onReassign={noop as never}
        onSendMessage={noop as never}
        onHydrate={async () => task}
      />
    </I18nProvider>
  );
}

function bar(container: HTMLElement): HTMLElement | null {
  return container.querySelector(".task-card__progress-bar");
}
function fill(container: HTMLElement): HTMLElement | null {
  return container.querySelector(".task-card__progress-fill");
}

// owner's exact shape: re-planned to 5 nodes, reporting 2/5. The 3 superseded
// nodes are excluded from the server-computed counts (T-1aea) — the card must
// take the SERVER's 2/5 and never recount the steps array.
const REPLANNED_2_OF_5 = mkTask({
  progressDone: 2,
  progressTotal: 5,
  createdTs: 1000,
  steps: [
    mkStep({ status: "done" }),
    mkStep({ status: "done" }),
    mkStep({ status: "in_progress" }),
    mkStep({ status: "pending" }),
    mkStep({ status: "pending" }),
    mkStep({ status: "superseded" }),
    mkStep({ status: "superseded" }),
    mkStep({ status: "superseded" }),
  ],
});

describe("進度條看得見 — the bar element is always rendered", () => {
  const shapes: Array<[string, TaskView]> = [
    ["0/0 (stepless)", mkTask({ progressDone: 0, progressTotal: 0 })],
    ["0/1 (zero fill)", mkTask({ progressDone: 0, progressTotal: 1 })],
    ["2/5 (owner's, re-planned)", REPLANNED_2_OF_5],
    ["3/3 (100%)", mkTask({ progressDone: 3, progressTotal: 3 })],
    ["7/5 (over-count)", mkTask({ progressDone: 7, progressTotal: 5 })],
  ];

  for (const [name, task] of shapes) {
    it(`renders the bar and its fill for ${name}`, () => {
      const { container } = renderCard(task);
      expect(bar(container)).not.toBeNull();
      expect(fill(container)).not.toBeNull();
      // the fill lives INSIDE the track — a fill that escapes its track has no
      // track to be a proportion of.
      expect(bar(container)!.contains(fill(container)!)).toBe(true);
    });
  }
});

describe("進度條看得見 — the fill width tracks the SERVER's counts", () => {
  it("owner's 2/5 re-planned card fills 40% — NOT 0", () => {
    const { container } = renderCard(REPLANNED_2_OF_5);
    // The exact failure owner described: text says 2/5 while the bar shows
    // nothing. Pin the fill to a real, non-zero proportion.
    expect(fill(container)!.style.width).toBe("40%");
  });

  it("the 2/5 text and the 2/5 fill agree (one source: the server counts)", () => {
    const { container, getByTestId } = renderCard(REPLANNED_2_OF_5);
    // superseded nodes are NOT counted — 8 steps exist, the card still says 2/5.
    expect(getByTestId("task-progress").textContent).toContain("2/5");
    expect(fill(container)!.style.width).toBe("40%");
  });

  it("0/0 yields a 0% fill, never NaN (no divide-by-zero)", () => {
    const { container } = renderCard(mkTask({ progressDone: 0, progressTotal: 0 }));
    const w = fill(container)!.style.width;
    expect(w).toBe("0%");
    expect(w).not.toContain("NaN");
  });

  it("0/1 yields a 0% fill and still keeps the track element", () => {
    const { container } = renderCard(mkTask({ progressDone: 0, progressTotal: 1 }));
    expect(fill(container)!.style.width).toBe("0%");
    expect(bar(container)).not.toBeNull();
  });

  it("3/3 fills 100%", () => {
    const { container } = renderCard(mkTask({ progressDone: 3, progressTotal: 3 }));
    expect(fill(container)!.style.width).toBe("100%");
  });

  it("done > total clamps to 100% and never overflows the track", () => {
    const { container } = renderCard(mkTask({ progressDone: 7, progressTotal: 5 }));
    expect(fill(container)!.style.width).toBe("100%");
  });
});

describe("進度條看得見 — the bar is unconditional", () => {
  // The bar sits OUTSIDE the expanded-only blocks. A collapsed card is the
  // card's default state, which is precisely the state owner was looking at.
  it("a COLLAPSED card still renders the bar (not gated behind expand)", () => {
    const { container } = renderCard(REPLANNED_2_OF_5);
    expect(container.querySelector(".task-timeline")).toBeNull(); // collapsed
    expect(bar(container)).not.toBeNull();
    expect(fill(container)!.style.width).toBe("40%");
  });

  it("every status renders a bar — including the two with their own fill colour", () => {
    for (const status of [
      "not_started",
      "in_progress",
      "waiting_owner",
      "waiting_external",
      "done",
      "terminated",
    ]) {
      const { container, unmount } = renderCard(
        mkTask({ ...REPLANNED_2_OF_5, status })
      );
      expect(bar(container), `bar missing for ${status}`).not.toBeNull();
      expect(fill(container)!.style.width, `fill wrong for ${status}`).toBe("40%");
      // the status modifier must ride ALONG with the base class, never replace
      // it — the base class is what carries the fill's background.
      expect(fill(container)!.className).toContain("task-card__progress-fill");
      expect(fill(container)!.className).toContain(
        `task-card__progress-fill--${status}`
      );
      unmount();
    }
  });
});
