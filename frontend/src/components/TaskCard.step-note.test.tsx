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
//
// 🔴 T-66 (owner rc-4c8065fb30a5:「整個拿掉,做在組裝票那一層…座艙改成點開才
// 抓」) CHANGED WHERE THE TEXT COMES FROM, and that is why this file no longer
// hand-builds a note onto a step. The task read carries only `noteSizeChars`;
// the text arrives from a `getTaskStep` call made when the entry is pressed.
// The previous version of this file put the text straight on the fixture and
// therefore could not tell a card that FETCHES from a card that does not —
// it would have stayed green against a cockpit that silently rendered nothing.
// So every case here goes through a stubbed API seam, and the stub is asserted
// on: WHICH step was fetched, that nothing was fetched before the click, and
// what happens while the fetch is in flight and when it fails.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import { api } from "../api";
import type { Member } from "../types";
import type {
  TaskView,
  TaskStepView,
  TaskStepDetailView,
  OutsourceWorkerView,
} from "../api/adapter";

let seq = 0;
function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`, name: `節點-${seq}`, dod: "", status: "pending",
    isGate: false, replyCardId: "", parallelGroup: "", orderIdx: seq,
    startedTs: 0, finishedTs: 0, ...over,
  };
}

/** A step the SERVER is holding a note for. The size is what the card sees —
 * the text is only ever handed over by the stubbed fetch below, exactly as the
 * real cockpit only ever gets it from get_task_step. */
function mkNotedStep(note: string, over: Partial<TaskStepView> = {}): TaskStepView {
  return mkStep({ noteSizeChars: [...note].length, ...over });
}

/** Stub the single-step read with a stepId → note map. Returns the spy so a
 * case can assert WHAT was fetched (and that nothing was, before the click). */
function stubStepNotes(notes: Record<string, string>) {
  return vi.spyOn(api, "getTaskStep").mockImplementation(
    async (_taskId: string, stepId: string): Promise<TaskStepDetailView> => ({
      ...mkStep({ id: stepId }),
      detailLevel: "full",
      note: notes[stepId] ?? "",
      noteSizeChars: [...(notes[stepId] ?? "")].length,
      noteCapChars: 4000,
    }),
  );
}

afterEach(() => {
  vi.restoreAllMocks();
});

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

/** Open step `i`'s note in the overlay and hand back the overlay element, once
 * the fetched text has landed. The wait is not incidental: since T-66 the
 * overlay opens BEFORE the text exists, so a helper that read the body
 * synchronously would be reading the loading state. */
async function openNote(
  utils: {
    findAllByTestId: (id: string) => Promise<HTMLElement[]>;
    findByTestId: (id: string) => Promise<HTMLElement>;
  },
  i = 0,
  expected?: string
) {
  const entries = await utils.findAllByTestId("step-note-open");
  fireEvent.click(entries[i]);
  // The overlay portals to document.body and carries no testid of its own.
  const el = document.querySelector(".md-preview") as HTMLElement | null;
  if (!el) throw new Error("the note overlay did not open");
  if (expected !== undefined) {
    await waitFor(() => expect(el.textContent).toContain(expected));
  } else {
    await waitFor(() =>
      expect(el.textContent).not.toContain("讀取備註中"),
    );
  }
  return el;
}

describe("步驟備註 renders on the task card (T-cc3e)", () => {
  it("fetches the note ON OPEN, for that step, and shows what came back", async () => {
    // 🔴 THIS IS THE T-66 CASE. The card is handed sizes and no text at all, so
    // a cockpit that did not call get_task_step has nothing to render — and the
    // old fixture-carried-the-text version of this file could not have seen
    // that. The spy is asserted three ways: not called before the click, called
    // with THIS step's id, and the returned text on screen.
    const a = mkNotedStep("spec 三份重生一致", { name: "第一步", status: "done" });
    const b = mkNotedStep("handler 寫完，測試還差負面案例", {
      name: "第二步", status: "in_progress",
    });
    const spy = stubStepNotes({
      [a.id]: "spec 三份重生一致",
      [b.id]: "handler 寫完，測試還差負面案例",
    });
    const utils = await renderExpanded([a, b]);

    // Rendering the timeline must cost NOTHING: the whole point of the split is
    // that the notes are not fetched until someone asks for one.
    expect(spy).not.toHaveBeenCalled();

    const first = await openNote(utils, 0, "spec 三份重生一致");
    expect(spy).toHaveBeenCalledTimes(1);
    expect(spy).toHaveBeenCalledWith("T-cc3e", a.id);
    // Per step, not smeared across the timeline: the other step's note is not
    // in this reader.
    expect(first.textContent).not.toContain("handler 寫完");
  });

  it("shows a loading state while the note is in flight, never a blank body", async () => {
    // 🔴 The entry only exists because the server is HOLDING a note, so an
    // empty reader is a statement the card has already contradicted. Nothing
    // resolves this deferred, so the overlay stays in flight for the whole
    // assertion.
    const step = mkNotedStep("這一句永遠不會抵達");
    vi.spyOn(api, "getTaskStep").mockImplementation(
      () => new Promise<TaskStepDetailView>(() => {}),
    );
    const utils = await renderExpanded([step]);
    const entries = await utils.findAllByTestId("step-note-open");
    fireEvent.click(entries[0]);
    const el = document.querySelector(".md-preview") as HTMLElement;
    expect(el).not.toBeNull();
    expect(el.textContent).toContain("讀取備註中");
  });

  it("says so when the fetch fails, instead of an empty reader", async () => {
    // A failure that renders "" is indistinguishable from a step with no note —
    // and the reader is only reachable from an entry that says there IS one.
    const step = mkNotedStep("伺服器上有,但這次拿不到");
    vi.spyOn(api, "getTaskStep").mockRejectedValue(new Error("boom"));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const utils = await renderExpanded([step]);
    const entries = await utils.findAllByTestId("step-note-open");
    fireEvent.click(entries[0]);
    const el = document.querySelector(".md-preview") as HTMLElement;
    await waitFor(() => expect(el.textContent).toContain("讀取備註失敗"));
  });

  it("renders the note as markdown", async () => {
    const step = mkNotedStep("卡在 **conformance**，下一步 `bin/ci.sh`");
    stubStepNotes({ [step.id]: "卡在 **conformance**，下一步 `bin/ci.sh`" });
    const utils = await renderExpanded([step]);
    const overlay = await openNote(utils, 0, "conformance");
    expect(overlay.querySelector("strong")?.textContent).toBe("conformance");
    expect(overlay.querySelector("code")?.textContent).toBe("bin/ci.sh");
    expect(overlay.textContent).not.toContain("**conformance**");
  });

  it("offers no download and no share link — it is a reader, not a file view", async () => {
    // 🔴 owner 2026-08-16:「只是沒有下載或分享連結的功能」. This is a property of
    // feeding the overlay `source` instead of a blob url, so it is asserted
    // here: a later caller that switches to a url would restore both silently.
    const step = mkNotedStep("只有文字");
    stubStepNotes({ [step.id]: "只有文字" });
    const utils = await renderExpanded([step]);
    const overlay = await openNote(utils, 0, "只有文字");
    expect(overlay.querySelector(".md-preview__download")).toBeNull();
    expect(overlay.querySelector(".md-preview__share")).toBeNull();
  });

  it("renders no control at all when a step has no note — no empty shell", async () => {
    // Since T-66 "has a note" is `noteSizeChars > 0`, which is the server's
    // exact count. 0 means the step genuinely has none — never "withheld".
    const utils = await renderExpanded([
      mkStep({ name: "沒備註", status: "pending", noteSizeChars: 0 }),
      mkNotedStep("只有這一步有", { name: "有備註", status: "pending" }),
    ]);
    const steps = await utils.findAllByTestId("task-step");
    expect(steps[0].querySelectorAll("[data-testid='step-note-open']")).toHaveLength(0);
    expect(steps[1].querySelectorAll("[data-testid='step-note-open']")).toHaveLength(1);
  });

  it("carries the note whatever the step's status is — the point of the field", async () => {
    // waiting_reason is bound to waiting_external; the note is bound to
    // nothing. If a status condition ever creeps into the render, this reddens.
    const statuses = ["pending", "in_progress", "waiting_external", "done"];
    const steps = statuses.map((status) =>
      mkNotedStep(`note-in-${status}`, { status, waitingReason: "" }),
    );
    stubStepNotes(
      Object.fromEntries(
        steps.map((s, i) => [s.id, `note-in-${statuses[i]}`]),
      ),
    );
    const utils = await renderExpanded(steps);
    const entries = await utils.findAllByTestId("step-note-open");
    expect(entries).toHaveLength(statuses.length);
    for (let i = 0; i < statuses.length; i += 1) {
      const overlay = await openNote(utils, i, `note-in-${statuses[i]}`);
      fireEvent.click(overlay.querySelector(".md-preview__close")!);
    }
  });

  it("keeps every note closed until its own control is pressed, and one at a time", async () => {
    const a = mkNotedStep("第一步的備註", { name: "第一步", status: "done" });
    const b = mkNotedStep("第二步的備註", { name: "第二步", status: "in_progress" });
    stubStepNotes({ [a.id]: "第一步的備註", [b.id]: "第二步的備註" });
    const utils = await renderExpanded([a, b]);

    // ① default: both controls are on the card, no note text is.
    const entries = await utils.findAllByTestId("step-note-open");
    expect(entries).toHaveLength(2);
    expect(document.querySelector(".md-preview")).toBeNull();

    // ② opening ONE step shows only that step's note — per step, which is the
    // whole point of the owner's wording ("該 step 的備注").
    const first = await openNote(utils, 0, "第一步的備註");
    expect(first.textContent).not.toContain("第二步的備註");

    // ③ closing puts the reader away again.
    fireEvent.click(first.querySelector(".md-preview__close")!);
    expect(document.querySelector(".md-preview")).toBeNull();

    // ④ the other step opens its own.
    const second = await openNote(utils, 1, "第二步的備註");
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
      mkNotedStep("只有這一步有", { name: "第二步", status: "pending" }),
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
    const step = mkNotedStep("第一步的備註", { name: "第一步", status: "done" });
    stubStepNotes({ [step.id]: "第一步的備註" });
    const utils = await renderExpanded([step]);
    await openNote(utils, 0, "第一步的備註");
    expect((await utils.findByTestId("task-card")).getAttribute("aria-expanded")).toBe("true");
  });
});
