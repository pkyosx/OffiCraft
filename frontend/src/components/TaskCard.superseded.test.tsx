// TaskCard / stepBadge — superseded 已答卡凍結節點 (T-1aea). Locked here:
//   1. resolveStepBadge (the ONE badge decision source) resolves a superseded
//      step to the plain status badge — never the card-answered/expired badge
//      (those fire only for waiting_owner) and never the dashed announced-gate
//      preview (a superseded gate is frozen replan history: nothing will ever
//      arm it).
//   2. The rendered row carries the archive styling hooks: 已取代 badge text,
//      the task-step--superseded greyscale class, the muted timeline dot.
//   3. Edge ③ (design §3-6): a task whose steps are ALL superseded honestly
//      reports progress 0/0 — while its detail hydrates the card must show the
//      loading placeholder, never the zero-step 「等待建立 Steps」 transition
//      copy; once the detail lands, the frozen timeline renders.
//   4. All three locales carry the stepStatus.superseded key (raw-string
//      fallback must never fire for a server-minted status).

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import { resolveStepBadge } from "../lib/stepBadge";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import type { Member } from "../types";
import type { TaskView, TaskStepView } from "../api/adapter";

let seq = 0;
function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`, name: `節點-${seq}`, dod: "", status: "pending",
    isGate: false, replyCardId: "", parallelGroup: "", orderIdx: seq,
    startedTs: 0, finishedTs: 0, ...over,
  };
}
function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "T-1aea", taskNo: "T-1aea", title: "re-plan 保留已答卡節點", typeKey: "",
    description: "", status: "in_progress", priority: "mid",
    executorKind: "member", executorId: "mira", creatorId: "", dedupeKey: "",
    deps: [], waitingReason: "", duplicateOf: "", createdTs: 1000, updatedTs: 2000,
    closedTs: null, progressDone: 0, progressTotal: 0, steps: [], ...over,
  };
}

const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
const noop = async () => {};

function renderCard(task: TaskView, onHydrate: (id: string) => Promise<TaskView>) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task} allTasks={[task]} members={[MIRA]} workers={[]} nowTs={3000}
        onTerminate={noop as never} onMarkDuplicate={noop as never}
        onSetPriority={noop as never} onReassign={noop as never}
        onSendMessage={noop as never}
        onHydrate={onHydrate}
      />
    </I18nProvider>
  );
}

describe("resolveStepBadge — superseded (T-1aea)", () => {
  it("resolves a superseded step to the plain status badge", () => {
    expect(
      resolveStepBadge({
        status: "superseded", isGate: false,
        replyCardId: "rc-1", replyCardStatus: "answered",
      })
    ).toEqual({ kind: "status", status: "superseded" });
  });

  it("never shows the announced-gate preview on a superseded gate", () => {
    expect(
      resolveStepBadge({
        status: "superseded", isGate: true, replyCardId: "", replyCardStatus: null,
      })
    ).toEqual({ kind: "status", status: "superseded" });
    // Sanity: the preview still fires for a live announced gate.
    expect(
      resolveStepBadge({
        status: "pending", isGate: true, replyCardId: "", replyCardStatus: null,
      })
    ).toEqual({ kind: "gate-announced" });
  });

  it("keeps the card-answered/expired badges for waiting_owner only", () => {
    expect(
      resolveStepBadge({
        status: "waiting_owner", isGate: false,
        replyCardId: "rc-1", replyCardStatus: "expired",
      })
    ).toEqual({ kind: "card-expired" });
  });
});

describe("TaskCard renders superseded rows as greyed archive (T-1aea)", () => {
  it("shows the 已取代 badge, the greyscale row class and the muted dot", async () => {
    const steps = [
      mkStep({
        name: "問方向", status: "superseded",
        startedTs: 1000, finishedTs: 2000,
      }),
      mkStep({ name: "重新執行", status: "in_progress", startedTs: 2000 }),
    ];
    const detail = mkTask({ progressDone: 0, progressTotal: 1, steps });
    const { container, findByTestId, findByText } = renderCard(
      mkTask({ progressDone: 0, progressTotal: 1 }),
      vi.fn(async () => detail)
    );
    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    await findByText("已取代"); // zh badge from stepStatus.superseded
    expect(container.querySelector(".task-step--superseded")).not.toBeNull();
    expect(
      container.querySelector(".task-step-badge--superseded")!.textContent
    ).toBe("已取代");
    expect(
      container.querySelector(".task-timeline__dot--superseded")
    ).not.toBeNull();
  });
});

describe("TaskCard edge ③ — all-superseded task reports 0/0 (T-1aea)", () => {
  it("shows loading (never 「等待建立 Steps」) while the 0/0 detail hydrates, then the frozen timeline", async () => {
    const frozen = mkTask({
      progressDone: 0, progressTotal: 0,
      steps: [
        mkStep({ name: "史料一", status: "superseded", startedTs: 1, finishedTs: 2 }),
        mkStep({ name: "史料二", status: "superseded", startedTs: 2, finishedTs: 3 }),
      ],
    });
    let resolveHydrate!: (v: TaskView) => void;
    const onHydrate = vi.fn(
      () => new Promise<TaskView>((res) => { resolveHydrate = res; })
    );
    const { findByTestId, findAllByTestId, queryByTestId } = renderCard(
      mkTask({ progressDone: 0, progressTotal: 0 }),
      onHydrate
    );
    fireEvent.click(await findByTestId("task-card"));

    // In-flight: the loading placeholder — the transition copy would lie
    // (progress 0/0 says nothing when superseded history is excluded).
    expect(await findByTestId("task-steps-loading")).not.toBeNull();
    expect(queryByTestId("task-transition")).toBeNull();

    await act(async () => { resolveHydrate(frozen); });
    expect((await findAllByTestId("task-step")).length).toBe(2);
    expect(queryByTestId("task-transition")).toBeNull();
  });

  it("still reaches the genuine zero-step transition copy after a stepless hydrate", async () => {
    const onHydrate = vi.fn(async () => mkTask({ steps: [] }));
    const { findByTestId } = renderCard(
      mkTask({ progressDone: 0, progressTotal: 0 }),
      onHydrate
    );
    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});
    expect((await findByTestId("task-transition")).textContent).toBe(
      "等待 Mira 建立 Steps"
    );
  });
});

describe("i18n — stepStatus.superseded exists in every locale (T-1aea)", () => {
  it("zh / en all carry a non-empty label", () => {
    for (const locale of [zh, en]) {
      expect(locale.tasks.stepStatus.superseded).toBeTruthy();
    }
  });
});
