// T-2654 — the 移除 affordance disappears once a task closes.
//
// The server freezes a closed task's deliverable set in EVERY direction (add,
// remove AND replace 409). Before this, the card kept offering 移除 on closed cards, so
// the only thing standing between owner and a dead-end click was the API. The
// mock made it worse: it had no terminal guard at all, so the fake cockpit
// DELETED where production refuses — a UI change checked against the mock would
// look correct and be wrong.
//
// The closed case is the load-bearing assertion; the open case is its positive
// control, so a mutant that drops the affordance unconditionally reddens too.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import type { TaskView } from "../api/adapter";

// T-66: the artifact rows come from the SERVER when the panel opens, not from
// the card's `onHydrate` (the task read carries an id+label index now — owner
// c-cd063427fb2f), so the fixture has to be handed in through this stub.
const { listTaskArtifacts } = vi.hoisted(() => ({ listTaskArtifacts: vi.fn() }));
vi.mock("../api", () => ({
  api: {
    subscribeEvents: () => () => {},
    getChatAttachmentShareLink: vi.fn(),
    getTaskStep: vi.fn(),
    listTaskArtifacts,
  },
}));

let seq = 0;

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${4000 + seq}`,
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
    artifactCount: 1,
    ...over,
  };
}

const NOOP = async () => {};

const ARTIFACT = {
  id: "ta-1",
  kind: "link",
  url: "https://example.com/pr/1",
  label: "PR #1",
  filename: "",
  mime: "",
  isImage: false,
  attachmentId: "",
  createdTs: 0,
  createdBy: "mira",
};

function renderCard(task: TaskView) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task}
        allTasks={[task]}
        members={[]}
        workers={[]}
        nowTs={Date.now() / 1000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onReassign={NOOP as never}
        onSendMessage={NOOP as never}
        onHydrate={(async () => ({ artifacts: [{ id: ARTIFACT.id, label: ARTIFACT.label }] })) as never}
        onRemoveArtifact={NOOP as never}
      />
    </I18nProvider>
  );
}

async function openArtifactPopover() {
  fireEvent.click(screen.getByTestId("task-artifacts-badge"));
  await waitFor(() => expect(screen.getByText("PR #1")).toBeTruthy());
}

beforeEach(() => {
  listTaskArtifacts.mockReset();
  listTaskArtifacts.mockResolvedValue([ARTIFACT]);
  seq = 0;
  window.location.hash = "";
});
afterEach(() => vi.restoreAllMocks());

describe("T-2654 已結案任務的產物集凍結", () => {
  it("結案的卡不再提供移除（done / terminated / duplicated 三種終態都一樣）", async () => {
    for (const status of ["done", "terminated", "duplicated"] as const) {
      const view = renderCard(
        mkTask({ status, closedTs: Date.now() / 1000 - 30 })
      );
      await openArtifactPopover();
      expect(
        view.container.querySelectorAll('[aria-label="移除產物"]').length
      ).toBe(0);
      view.unmount();
    }
  });

  it("正向對照：任務還開著時移除照常出現", async () => {
    const view = renderCard(mkTask({ status: "in_progress" }));
    await openArtifactPopover();
    expect(
      view.container.querySelectorAll('[aria-label="移除產物"]').length
    ).toBeGreaterThan(0);
  });
});
