// 步驟備註 (T-cc3e) — the cockpit face of the step working note.
//
// The owner picked a step-level note over folding progress into the task
// description for ONE stated reason: he wants the granularity of "第 4 步做到
// 哪" on the card. So "the server stores it" is not the deliverable — being
// able to READ it on the task card is. These tests assert the rendered text,
// per step, on the real card; they are what stops the field from shipping
// invisible.
//
// The note is agent-authored free text (the handover SOP asks for "做到哪、下
// 一步接什麼", routinely with markdown), so it takes the same treatment as the
// DoD and the waiting reason: rendered through the shared, XSS-safe `Markdown`
// component, with the i18n label kept OUTSIDE that container.
//
// T-e5b1 (owner 2026-08-15) put the note behind a per-step disclosure; T-6630 ④
// (owner 2026-08-16:「備註不是很常按,可以放在 step 的右下角,點開再跳出另一個
// Modal」) moved the READING of it into the cockpit's existing full-view
// overlay, leaving a small corner control in the step. That does NOT weaken the
// paragraph above — it makes one more thing load-bearing. While no note is
// open, that CONTROL is the only signal separating a step someone wrote a note
// on from a step nobody did, so its presence/absence is asserted here as a
// first-class contract, not as decoration.
//
// Where the note text is asserted, it is asserted INSIDE the opened overlay:
// that is now the only place a human can read it.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import type { Member } from "../types";
import type { TaskView, TaskStepView, OutsourceWorkerView } from "../api/adapter";

let seq = 0;
function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`, name: `節點-${seq}`, dod: "", status: "pending",
    isGate: false, replyCardId: "", parallelGroup: "", orderIdx: seq,
    startedTs: 0, finishedTs: 0, ...over,
  };
}

function mkTask(steps: TaskStepView[]): TaskView {
  return {
    id: "T-cc3e", taskNo: "T-cc3e", title: "步驟備註任務", typeKey: "",
    description: "", status: "in_progress", priority: "mid",
    executorKind: "member", executorId: "mira", creatorId: "", dedupeKey: "",
    deps: [], waitingReason: "", duplicateOf: "", createdTs: 1000, updatedTs: 2000,
    closedTs: null, progressDone: 0, progressTotal: steps.length, steps,
  };
}

const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
const noop = async () => {};
const workers: OutsourceWorkerView[] = [];

// The step timeline only renders while the card is expanded.
async function renderExpanded(steps: TaskStepView[]) {
  const task = mkTask(steps);
  const utils = render(
    <I18nProvider>
      <TaskCard
        task={task} allTasks={[task]} members={[MIRA]} workers={workers} nowTs={3000}
        onTerminate={noop as never} onMarkDuplicate={noop as never} onSetPriority={noop as never}
        onReassign={noop as never}
        onSendMessage={noop as never} onHydrate={vi.fn(async () => task)}
      />
    </I18nProvider>
  );
  fireEvent.click(await utils.findByTestId("task-card"));
  return utils;
}

/** Open step `i`'s note in the overlay and hand back the overlay element. */
async function openNote(
  utils: {
    findAllByTestId: (id: string) => Promise<HTMLElement[]>;
    findByTestId: (id: string) => Promise<HTMLElement>;
  },
  i = 0
) {
  const entries = await utils.findAllByTestId("step-note-open");
  fireEvent.click(entries[i]);
  // The overlay portals to document.body and carries no testid of its own.
  const el = document.querySelector(".md-preview") as HTMLElement | null;
  if (!el) throw new Error("the note overlay did not open");
  return el;
}

describe("步驟備註 renders on the task card (T-cc3e)", () => {
  it("shows the note text of the step whose control was pressed", async () => {
    const utils = await renderExpanded([
      mkStep({ name: "第一步", status: "done", note: "spec 三份重生一致" }),
      mkStep({ name: "第二步", status: "in_progress", note: "handler 寫完，測試還差負面案例" }),
    ]);
    const first = await openNote(utils, 0);
    expect(first.textContent).toContain("spec 三份重生一致");
    // Per step, not smeared across the timeline: the other step's note is not
    // in this reader.
    expect(first.textContent).not.toContain("handler 寫完");
  });

  it("renders the note as markdown", async () => {
    const utils = await renderExpanded([
      mkStep({ note: "卡在 **conformance**，下一步 `bin/ci.sh`" }),
    ]);
    const overlay = await openNote(utils);
    expect(overlay.querySelector("strong")?.textContent).toBe("conformance");
    expect(overlay.querySelector("code")?.textContent).toBe("bin/ci.sh");
    expect(overlay.textContent).not.toContain("**conformance**");
  });

  it("offers no download and no share link — it is a reader, not a file view", async () => {
    // 🔴 owner 2026-08-16:「只是沒有下載或分享連結的功能」. This is a property of
    // feeding the overlay `source` instead of a blob url, so it is asserted
    // here: a later caller that switches to a url would restore both silently.
    const utils = await renderExpanded([mkStep({ note: "只有文字" })]);
    const overlay = await openNote(utils);
    expect(overlay.querySelector(".md-preview__download")).toBeNull();
    expect(overlay.querySelector(".md-preview__share")).toBeNull();
  });

  it("renders no control at all when a step has no note — no empty shell", async () => {
    const utils = await renderExpanded([
      mkStep({ name: "沒備註", status: "pending" }),
      mkStep({ name: "有備註", status: "pending", note: "只有這一步有" }),
    ]);
    const steps = await utils.findAllByTestId("task-step");
    expect(steps[0].querySelectorAll("[data-testid='step-note-open']")).toHaveLength(0);
    expect(steps[1].querySelectorAll("[data-testid='step-note-open']")).toHaveLength(1);
  });

  it("carries the note whatever the step's status is — the point of the field", async () => {
    // waiting_reason is bound to waiting_external; the note is bound to
    // nothing. If a status condition ever creeps into the render, this reddens.
    const statuses = ["pending", "in_progress", "waiting_external", "done"];
    const utils = await renderExpanded(
      statuses.map((status) =>
        mkStep({ status, note: `note-in-${status}`, waitingReason: "" }),
      ),
    );
    const entries = await utils.findAllByTestId("step-note-open");
    expect(entries).toHaveLength(statuses.length);
    for (let i = 0; i < statuses.length; i += 1) {
      const overlay = await openNote(utils, i);
      expect(overlay.textContent).toContain(`note-in-${statuses[i]}`);
      fireEvent.click(overlay.querySelector(".md-preview__close")!);
    }
  });

  it("keeps every note closed until its own control is pressed, and one at a time", async () => {
    const utils = await renderExpanded([
      mkStep({ name: "第一步", status: "done", note: "第一步的備註" }),
      mkStep({ name: "第二步", status: "in_progress", note: "第二步的備註" }),
    ]);

    // ① default: both controls are on the card, no note text is.
    const entries = await utils.findAllByTestId("step-note-open");
    expect(entries).toHaveLength(2);
    expect(document.querySelector(".md-preview")).toBeNull();

    // ② opening ONE step shows only that step's note — per step, which is the
    // whole point of the owner's wording ("該 step 的備注").
    const first = await openNote(utils, 0);
    expect(first.textContent).toContain("第一步的備註");
    expect(first.textContent).not.toContain("第二步的備註");

    // ③ closing puts the reader away again.
    fireEvent.click(first.querySelector(".md-preview__close")!);
    expect(document.querySelector(".md-preview")).toBeNull();

    // ④ the other step opens its own.
    const second = await openNote(utils, 1);
    expect(second.textContent).toContain("第二步的備註");
    expect(second.textContent).not.toContain("第一步的備註");
  });

  it("gives a step WITH a note a control a step without one does not have", async () => {
    // 🔴 The owner reads this timeline to find out where a step got to. With the
    // note behind a reader, "nobody wrote anything" and "someone wrote something
    // you have not opened" must not look the same. This control is that
    // difference, and it is asserted per step (not just counted) so a future
    // change that renders it on EVERY step reddens here.
    // The step NAMES deliberately avoid the word 備註 — a fixture name carrying
    // the same word would make the text assertion pass for the wrong reason.
    const { findAllByTestId } = await renderExpanded([
      mkStep({ name: "第一步", status: "pending" }),
      mkStep({ name: "第二步", status: "pending", note: "只有這一步有" }),
    ]);
    const steps = await findAllByTestId("task-step");
    expect(steps).toHaveLength(2);
    expect(steps[0].querySelectorAll("[data-testid='step-note-open']")).toHaveLength(0);
    expect(steps[1].querySelectorAll("[data-testid='step-note-open']")).toHaveLength(1);
    // and the control says the word 備註, so what it opens is not a guess.
    expect(steps[1].textContent).toContain("備註");
    expect(steps[0].textContent).not.toContain("備註");
  });

  it("opening a note does not collapse the card it lives in", async () => {
    // The whole card is a toggle surface; a click that lands on a <button> is
    // exempted by the card's closest() filter. If that exemption ever stops
    // covering this control, the note would open and the card would shut in the
    // same click — and the card behind the reader would be gone when it closes.
    const utils = await renderExpanded([
      mkStep({ name: "第一步", status: "done", note: "第一步的備註" }),
    ]);
    await openNote(utils);
    expect((await utils.findByTestId("task-card")).getAttribute("aria-expanded")).toBe("true");
  });
});
