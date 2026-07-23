// 等待外部 reason — markdown render + label contract.
//
// ORIGINALLY T-a20b, on the TASK-level waiting block: waitingReason is
// agent-authored free text and must render through the shared, XSS-safe
// `Markdown` component (owner's screenshot showed `**fms #20054**` and
// `` `919fe961` `` as literal asterisks/backticks). The interesting constraint:
// the label is an i18n TEMPLATE, so feeding `等待中 · ${reason}` into <Markdown>
// whole would hand the prefix to the parser — the label must stay OUTSIDE the
// markdown container.
//
// T-c514 (owner 2026-07-20) REMOVED the task-level block as a duplicate: the
// reason is reported per-STEP and the step already renders it inside the node.
// Every contract above still holds — it just has exactly one carrier now, the
// step row — so this suite was MOVED down a level rather than deleted. The
// shapes asserted (bold/code, label-outside-markdown, fenced code, sanitize,
// bare-word label in three locales) are the same shapes; only the surface
// changed: .task-card__waiting-* → .task-step__waiting-*.
//
// The final describe is the T-c514 removal guard itself: the task-level block
// must be GONE while the step-level one is PRESENT — the ticket's "never both
// missing" requirement, pinned in one place.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
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

// A task whose CURRENT step is waiting_external and carries `reason`. The task
// itself is waiting_external too — that is the real shape the server derives
// (task status is derived from its steps), and it is what makes the removal
// guard below meaningful: the task-level block would have rendered here.
function mkWaitingTask(reason: string): TaskView {
  return {
    id: "T-a20b", taskNo: "T-a20b", title: "waiting markdown 任務", typeKey: "",
    description: "", status: "waiting_external", priority: "high",
    executorKind: "member", executorId: "mira", creatorId: "", dedupeKey: "",
    deps: [], waitingReason: reason, duplicateOf: "", createdTs: 1000, updatedTs: 2000,
    closedTs: null, progressDone: 0, progressTotal: 1,
    steps: [mkStep({ status: "waiting_external", waitingReason: reason })],
  };
}

const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
const noop = async () => {};
const workers: OutsourceWorkerView[] = [];

function renderCard(task: TaskView) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task} allTasks={[task]} members={[MIRA]} workers={workers} nowTs={3000}
        onTerminate={noop as never} onMarkDuplicate={noop as never} onSetPriority={noop as never}
        onReassign={noop as never}
        onSendMessage={noop as never} onHydrate={vi.fn(async () => task)}
      />
    </I18nProvider>
  );
}

// The step timeline only renders while the card is expanded, so every
// step-level assertion has to open the card first.
async function renderExpanded(reason: string) {
  const utils = renderCard(mkWaitingTask(reason));
  fireEvent.click(await utils.findByTestId("task-card"));
  return utils;
}

describe("step 等待外部 reason markdown render (T-a20b shapes, T-c514 surface)", () => {
  it("renders the reason's bold + inline code as elements — the exact shapes from owner's screenshot", async () => {
    const { findByTestId } = await renderExpanded(
      "等 **fms #20054** 合併，commit `919fe961`"
    );
    const waiting = await findByTestId("step-waiting-reason");

    // positive control: this scope always holds the clock icon + the label, so
    // a mis-scoped/empty selector fails HERE rather than passing the negative
    // assertions below by vacuity.
    expect(waiting.querySelector("svg")).not.toBeNull();
    expect(waiting.querySelector(".task-step__waiting-label")?.textContent)
      .toContain("等待中");

    const md = waiting.querySelector(".task-step__waiting-md")!;
    expect(md.querySelector("strong")?.textContent).toBe("fms #20054");
    expect(md.querySelector("code")?.textContent).toBe("919fe961");
    // the literal syntax owner saw on screen must be gone
    expect(md.textContent).not.toContain("**fms #20054**");
    expect(md.textContent).not.toContain("`919fe961`");
  });

  it("keeps the i18n label OUT of the markdown container (the template must not be parsed)", async () => {
    const { findByTestId } = await renderExpanded("**外部** 回覆");
    const waiting = await findByTestId("step-waiting-reason");

    // positive control — the label is rendered somewhere in this block...
    expect(waiting.textContent).toContain("等待中");
    // ...but NOT inside the markdown container: feeding the whole
    // `等待中 · ${reason}` template to <Markdown> is the trap this pins.
    const md = waiting.querySelector(".task-step__waiting-md")!;
    expect(md.querySelector("strong")?.textContent).toBe("外部");
    expect(md.textContent).not.toContain("等待中");
  });

  it("renders a fenced code block as <pre> inside the .doc-md container", async () => {
    const { findByTestId } = await renderExpanded(
      "等這個跑完：\n```\nkubectl --context arn:aws:eks:us-west-2 -n sit exec pod -- python manage.py cmd\n```"
    );
    const waiting = await findByTestId("step-waiting-reason");

    // The mobile overflow fix (`.doc-md pre { max-width:100%; overflow-x:auto }`,
    // settings.css) only reaches this <pre> if it lands inside a .doc-md
    // container — jsdom can't measure layout, so structure is what we can pin.
    const md = waiting.querySelector(".task-step__waiting-md")!;
    expect(md.classList.contains("doc-md")).toBe(true);
    const pre = md.querySelector("pre");
    expect(pre).not.toBeNull();
    expect(pre?.querySelector("code")?.textContent).toContain("kubectl");
  });

  it("sanitizes a malicious reason — no raw HTML injected, script text stays literal", async () => {
    const { findByTestId } = await renderExpanded(
      "<img src=x onerror=alert(1)> 等 [bad](javascript:alert(1))"
    );
    const waiting = await findByTestId("step-waiting-reason");

    const md = waiting.querySelector(".task-step__waiting-md")!;
    expect(md.querySelector("img")).toBeNull();
    expect(md.querySelector("a")).toBeNull();
    expect(md.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(md.textContent).toContain("[bad](javascript:alert(1))");
  });
});

// ── T-a20b follow-up: the orphan "·" ──────────────────────────────────────
// The row (`flex-basis: 100%` on the markdown column + the row's flex-wrap)
// puts the markdown on its OWN line at EVERY width — always-stack, per owner's
// 2026-07-17 ruling on rc-91492e026e87. The "·" was a JSX literal whose only
// job was to join "label · reason" on one shared line; once they stop sharing
// one it dangles at the end of the label's line with nothing after it. It is
// removed — these tests are what keep it removed.
//
// NOTE ON WHAT THESE CAN AND CANNOT DO: these pin the "·" is gone. They do NOT
// pin the always-stack layout itself — vitest runs in jsdom (vite.config.ts:8),
// which applies no stylesheet and computes no layout, so deleting the CSS rule
// outright leaves this suite fully green. The stacking is verified out-of-band
// by Playwright (visual-guards/step-waiting-external.ct.spec.tsx). Do not add a
// layout-shaped assertion here; it would be decorative.
//
// Why these are equality assertions and not `not.toContain("·")`: a bare
// negation over a deleted thing is a dead assertion — it also passes when the
// label stops rendering entirely, when the selector rots, or when textContent
// is undefined. `toBe(<exact bare word>)` fails in ALL of those directions AND
// fails the moment someone re-adds the separator, because "等待中 ·" is simply
// not equal to "等待中". Each test additionally carries a positive control.
describe("waiting label carries no orphan separator (T-a20b)", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  // The label is a bare word in every locale — the "·" never lived in the dict
  // (zh.ts documents it as a JSX literal on purpose), so the fix is a JSX-only
  // deletion. This pins the dict side of that contract: if anyone ever "fixes"
  // the separator by pushing it INTO the i18n strings, this reddens. The key is
  // shared by the step row, so it outlived the task-level block (T-c514).
  it.each([
    ["zh", zh, "等待中"],
    ["en", en, "Waiting"],
  ])("%s dict keeps waitingLabel a bare word", (_name, dict, expected) => {
    // positive control: the entry exists and is the word we think it is — a
    // renamed/removed key fails here instead of vacuously passing below.
    expect(dict.tasks.waitingLabel).toBe(expected);
    expect(dict.tasks.waitingLabel).not.toContain("·");
  });

  it.each([
    ["zh", { "oc.language": "zh" }, "等待中"],
    ["en", { "oc.language": "en" }, "Waiting"],
  ])("renders the %s label with no trailing separator", async (_name, ls, expected) => {
    for (const [k, v] of Object.entries(ls)) localStorage.setItem(k, v);

    const { findByTestId } = await renderExpanded("等 **fms #20054** 合併");
    const waiting = await findByTestId("step-waiting-reason");
    const label = waiting.querySelector(".task-step__waiting-label");

    // positive control #1 — the label element actually exists and is THIS
    // locale's word, so a vanished label / wrong locale fails here.
    expect(label).not.toBeNull();
    // positive control #2 — the reason still renders as markdown beside it, so
    // a step that stopped rendering the waiting row entirely fails here.
    expect(waiting.querySelector(".task-step__waiting-md strong")?.textContent)
      .toBe("fms #20054");

    // the assertion that matters: the label is EXACTLY the bare word. Re-adding
    // the "·" (in JSX or via the dict) makes this "等待中 ·" and reddens.
    expect(label?.textContent?.trim()).toBe(expected);
  });
});

// ── T-c514 removal guard ──────────────────────────────────────────────────
// Owner 2026-07-20: the task card's progress bar used to be followed by a
// task-level 「⏳ 等待中 + waitingReason」 block. waiting_external is reported
// per-step and the step renders its own reason, so that block was the same
// sentence twice, one level further from the work it describes. Removed.
//
// This is deliberately ONE test asserting BOTH halves together, because the
// requirement owner wrote is a conjunction — "移除任務層" is only safe while
// "step 層有顯示" holds, and the failure mode the ticket explicitly forbids is
// 「兩邊都沒有」. Split across two tests, a regression that kills the step row
// would leave THIS file's removal half green and the loss would read as a pass
// in the removal's own guard. Asserted on a task that is itself
// waiting_external WITH a non-empty waitingReason — i.e. the exact input the
// deleted block used to render on, so the absence is a real removal and not an
// unmet precondition.
describe("task-level waiting block is gone, step-level survives (T-c514)", () => {
  // The locale suite above parks `oc.language` in localStorage, which the
  // I18nProvider picks up. Clear it so this test asserts the zh wording it
  // names, independent of run order.
  beforeEach(() => {
    localStorage.clear();
  });

  it("waiting_external task: no block at the card top, reason present inside the step", async () => {
    const { findByTestId, container } = await renderExpanded(
      "等 **fms #20054** 合併"
    );

    // ① the removed task-level block is absent...
    expect(
      container.querySelector('[data-testid="task-waiting-reason"]')
    ).toBeNull();
    // ...and so is its markdown surface, so a rename can't smuggle it back.
    expect(container.querySelector(".task-card__waiting-md")).toBeNull();
    expect(container.querySelector(".task-card__waiting-label")).toBeNull();

    // ② the information did NOT go missing with it — the step still carries the
    // reason, rendered as markdown. This half is what forbids 「兩邊都沒有」.
    const stepRow = await findByTestId("step-waiting-reason");
    expect(stepRow.querySelector(".task-step__waiting-md strong")?.textContent)
      .toBe("fms #20054");

    // ③ the task-level STATUS pill stays — owner kept the status on the card,
    // only the duplicated reason text left. Guards against over-deletion.
    expect(
      container.querySelector('[data-testid="task-status"]')?.textContent
    ).toBe("等待外部");
  });
});
