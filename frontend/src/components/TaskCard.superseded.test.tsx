// TaskCard / stepBadge — superseded 已答卡凍結節點 (T-1aea). Locked here:
//   1. resolveStepBadge (the ONE step-status decision source) resolves a
//      superseded step to superseded, whatever its card or gate state.
//   2. The rendered row carries the archive styling hooks: the 已取代 timeline
//      icon and the task-step--superseded greyscale class.
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
    executorKind: "staff", executorId: "mira", creatorId: "", dedupeKey: "",
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
        task={task} members={[MIRA]} workers={[]} nowTs={3000}
        onTerminate={noop as never} onMarkDuplicate={noop as never}
        onSetPriority={noop as never} onReassign={noop as never}
        onSendMessage={noop as never}
        onHydrate={onHydrate}
      />
    </I18nProvider>
  );
}

describe("resolveStepBadge — superseded (T-1aea)", () => {
  it("resolves a superseded step with an answered card to superseded", () => {
    expect(
      resolveStepBadge({
        status: "superseded",
        replyCardId: "rc-1", replyCardStatus: "answered",
      })
    ).toBe("superseded");
  });

  it("resolves a superseded card-less step to superseded", () => {
    expect(
      resolveStepBadge({
        status: "superseded", replyCardId: "", replyCardStatus: null,
      })
    ).toBe("superseded");
  });
});

describe("TaskCard renders superseded rows as greyed archive (T-1aea)", () => {
  it("shows the 已取代 timeline icon and the greyscale row class", async () => {
    const steps = [
      mkStep({
        name: "問方向", status: "superseded",
        startedTs: 1000, finishedTs: 2000,
      }),
      mkStep({ name: "重新執行", status: "in_progress", startedTs: 2000 }),
    ];
    const detail = mkTask({ progressDone: 0, progressTotal: 1, steps });
    const { container, findByTestId, findAllByTestId } = renderCard(
      mkTask({ progressDone: 0, progressTotal: 1 }),
      vi.fn(async () => detail)
    );
    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    const markers = await findAllByTestId("step-marker");
    expect(markers.map((m) => m.getAttribute("aria-label"))).toEqual(["已取代", "進行中"]);
    expect(container.querySelector(".task-step--superseded")).not.toBeNull();
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
