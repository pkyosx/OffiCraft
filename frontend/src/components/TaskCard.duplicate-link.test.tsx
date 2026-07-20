// T-c21e S1 — the duplicated-original row, the THIRD mouth of the same
// regression owner reported on the T-1d82 驗收 card:
//   「那些 ID 應該要跟任務卡上面顯示的一樣,任務卡上的 ID 似乎沒這麼長」
//
// The ticket named two fallback branches (the dep rows). Review found this one
// carrying the identical `?? rawId` shape ~60 lines below them: when the
// ORIGINAL task a duplicate points at is not in the loaded population, the row
// printed `t-1d8292a2f8db` where every other surface says `T-1d82`.
//
// It is fixed here rather than deferred because "the ticket only named two"
// is a reason about the ticket, not about what owner sees. A surface read with
// the same eyes, on the same card, disagreeing with the rows right above it,
// would have reproduced the exact complaint this ticket exists to close — and
// the next person would reasonably read the two fixed branches as evidence
// that this one was considered and deliberately left alone.
//
// ── HONEST LIMIT ──────────────────────────────────────────────────────────
// Review could not construct a REAL cockpit state that reaches this fallback:
// a duplicated card is terminal, so `needClosed` has usually widened the
// population by the time it renders, and then the `??` left side wins. So this
// may well be unreachable in production today. It is pinned anyway — the row
// is one `needClosed` refactor away from being reachable, and the cost of the
// pin is one file. What is NOT claimed: that this was visibly broken for
// owner. The two dep branches were; this one is the same defect found by
// inspection.

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

const NOOP = async () => {};

function renderCard(task: TaskView, allTasks: TaskView[]) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task}
        allTasks={allTasks}
        depsResolvable
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
  it("原票不在母體時,顯示派生的短編號而非原始長 id", () => {
    const dupe = mkTask({
      title: "重複票",
      status: "duplicated",
      duplicateOf: LONG_ID,
    });
    const { container } = renderCard(dupe, [dupe]);

    const text = container.textContent ?? "";
    expect(text).toContain("T-1d82");
    expect(text).not.toContain(LONG_ID);
  });

  it("原票在母體時,仍以 server 的 task_no 為準", () => {
    // taskNo and id are deliberately INCONSISTENT so the two sources are
    // distinguishable: derivation would say T-9999, the server says T-abcd.
    const original = mkTask({
      id: "t-9999ffffffff",
      taskNo: "T-abcd",
      title: "原票",
    });
    const dupe = mkTask({
      title: "重複票",
      status: "duplicated",
      duplicateOf: original.id,
    });
    const { container } = renderCard(dupe, [dupe, original]);

    const text = container.textContent ?? "";
    expect(text).toContain("T-abcd");
    expect(text).not.toContain("T-9999");
  });
});
