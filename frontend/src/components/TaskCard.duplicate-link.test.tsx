// The duplicated-original row names the original by its id (the number IS the
// id), so it never needs the original's row to be loaded.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import type { TaskView } from "../api/adapter";

const LONG_ID = "t-1d8292a2f8db";

let seq = 0;

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${3000 + seq}`,
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

const NOOP = async () => {};

function renderCard(task: TaskView) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task}
        members={[]}
        workers={[]}
        nowTs={Date.now() / 1000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onReassign={NOOP as never}
        onSendMessage={NOOP as never}
        onHydrate={NOOP as never}
        onRemoveArtifact={NOOP as never}
      />
    </I18nProvider>
  );
}

beforeEach(() => {
  window.location.hash = "";
});
afterEach(() => vi.restoreAllMocks());

describe("T-c21e S1 重複原票列的編號", () => {
  it("names the original by its id, led by the jump icon", () => {
    const dupe = mkTask({
      title: "重複票",
      status: "duplicated",
      duplicateOf: LONG_ID,
    });
    const { container } = renderCard(dupe);

    const text = container.textContent ?? "";
    // Was `toContain("T-1d82")` + `not.toContain(LONG_ID)`. The second half is
    // deleted rather than relaxed: with the number equal to the id it directly
    // contradicts the first half and could only pass on an empty row.
    expect(text).toContain(LONG_ID);
    const link = container.querySelector('[data-testid="task-duplicate-link"]')!;
    expect(
      Array.from(link.children).map((el) =>
        el.tagName === "svg" ? el.querySelector("polyline")?.getAttribute("points") : el.textContent,
      ),
    ).toEqual(["15 3 21 3 21 9", `重複於 #${LONG_ID}`]);
  });
});
