// T-a20b — the task card's waiting_external reason (TaskView.waitingReason) is
// agent-authored free text and must render through the shared, XSS-safe
// `Markdown` component. T-13af rendered the description + step DoD but never
// reached this block, so owner's screenshot showed `**fms #20054**` and
// `` `919fe961` `` as literal asterisks/backticks.
//
// The interesting constraint here (and why this can't copy the description's
// shape): the label is an i18n TEMPLATE. Feeding `等待中 · ${reason}` into
// <Markdown> whole would hand the prefix to the markdown parser, so the label
// must stay OUTSIDE the markdown container — asserted below.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import { xian } from "../i18n/locales/xian";
import type { Member } from "../types";
import type { TaskView, OutsourceWorkerView } from "../api/adapter";

function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "T-a20b", taskNo: "T-a20b", title: "waiting markdown 任務", typeKey: "",
    description: "", status: "waiting_external", priority: "high",
    executorKind: "member", executorId: "mira", creatorId: "", dedupeKey: "",
    deps: [], waitingReason: "", duplicateOf: "", createdTs: 1000, updatedTs: 2000,
    closedTs: null, progressDone: 0, progressTotal: 1, steps: [], ...over,
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

describe("TaskCard waiting_external reason markdown render (T-a20b)", () => {
  it("renders the reason's bold + inline code as elements — the exact shapes from owner's screenshot", async () => {
    const { findByTestId } = renderCard(
      mkTask({ waitingReason: "等 **fms #20054** 合併，commit `919fe961`" })
    );
    const waiting = await findByTestId("task-waiting-reason");

    // positive control: this scope always holds the clock icon + the label, so
    // a mis-scoped/empty selector fails HERE rather than passing the negative
    // assertions below by vacuity.
    expect(waiting.querySelector("svg")).not.toBeNull();
    expect(waiting.querySelector(".task-card__waiting-label")?.textContent)
      .toContain("等待中");

    const md = waiting.querySelector(".task-card__waiting-md")!;
    expect(md.querySelector("strong")?.textContent).toBe("fms #20054");
    expect(md.querySelector("code")?.textContent).toBe("919fe961");
    // the literal syntax owner saw on screen must be gone
    expect(md.textContent).not.toContain("**fms #20054**");
    expect(md.textContent).not.toContain("`919fe961`");
  });

  it("keeps the i18n label OUT of the markdown container (the template must not be parsed)", async () => {
    const { findByTestId } = renderCard(
      mkTask({ waitingReason: "**外部** 回覆" })
    );
    const waiting = await findByTestId("task-waiting-reason");

    // positive control — the label is rendered somewhere in this block...
    expect(waiting.textContent).toContain("等待中");
    // ...but NOT inside the markdown container: feeding the whole
    // `等待中 · ${reason}` template to <Markdown> is the trap this pins.
    const md = waiting.querySelector(".task-card__waiting-md")!;
    expect(md.querySelector("strong")?.textContent).toBe("外部");
    expect(md.textContent).not.toContain("等待中");
  });

  it("renders a fenced code block as <pre> inside the .doc-md container", async () => {
    const { findByTestId } = renderCard(
      mkTask({
        waitingReason:
          "等這個跑完：\n```\nkubectl --context arn:aws:eks:us-west-2 -n sit exec pod -- python manage.py cmd\n```",
      })
    );
    const waiting = await findByTestId("task-waiting-reason");

    // The mobile overflow fix (`.doc-md pre { max-width:100%; overflow-x:auto }`,
    // settings.css) only reaches this <pre> if it lands inside a .doc-md
    // container — jsdom can't measure layout, so structure is what we can pin.
    const md = waiting.querySelector(".task-card__waiting-md")!;
    expect(md.classList.contains("doc-md")).toBe(true);
    const pre = md.querySelector("pre");
    expect(pre).not.toBeNull();
    expect(pre?.querySelector("code")?.textContent).toContain("kubectl");
  });

  it("sanitizes a malicious reason — no raw HTML injected, script text stays literal", async () => {
    const { findByTestId } = renderCard(
      mkTask({ waitingReason: "<img src=x onerror=alert(1)> 等 [bad](javascript:alert(1))" })
    );
    const waiting = await findByTestId("task-waiting-reason");

    const md = waiting.querySelector(".task-card__waiting-md")!;
    expect(md.querySelector("img")).toBeNull();
    expect(md.querySelector("a")).toBeNull();
    expect(md.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(md.textContent).toContain("[bad](javascript:alert(1))");
  });
});

// ── T-a20b follow-up: the orphan "·" ──────────────────────────────────────
// The row (`.task-card__waiting-md.doc-md { flex: 1 1 100% }` + the row's
// flex-wrap) puts the markdown on its OWN line at EVERY width — always-stack,
// per owner's 2026-07-17 ruling on rc-91492e026e87. (It was ~415px-and-below
// only when these tests were written; going unconditional does not weaken the
// case for deleting the separator, it strengthens it.) The "·" was a JSX
// literal whose only job was to join "label · reason" on one shared line; once
// they stop sharing one it dangles at the end of the label's line with nothing
// after it. It is removed — these tests are what keep it removed.
//
// NOTE ON WHAT THESE CAN AND CANNOT DO: these pin the "·" is gone. They do NOT
// pin the always-stack layout itself — vitest runs in jsdom (vite.config.ts:8),
// which applies no stylesheet and computes no layout, so deleting the CSS rule
// outright leaves this suite fully green. The stacking is verified out-of-band
// by Playwright (see the measured y-coords in tasks.css). Do not add a
// layout-shaped assertion here; it would be decorative.
//
// Why these are equality assertions and not `not.toContain("·")`: a bare
// negation over a deleted thing is a dead assertion — it also passes when the
// label stops rendering entirely, when the selector rots, or when textContent
// is undefined. `toBe(<exact bare word>)` fails in ALL of those directions AND
// fails the moment someone re-adds the separator, because "等待中 ·" is simply
// not equal to "等待中". Each test additionally carries a positive control.
describe("TaskCard waiting label carries no orphan separator (T-a20b)", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  // The label is a bare word in every locale — the "·" never lived in the dict
  // (zh.ts:111-112 documents it as a JSX literal on purpose), so the fix is a
  // JSX-only deletion. This pins the dict side of that contract: if anyone ever
  // "fixes" the separator by pushing it INTO the i18n strings, this reddens.
  it.each([
    ["zh", zh, "等待中"],
    ["en", en, "Waiting"],
    ["xian", xian, "候緣"],
  ])("%s dict keeps waitingLabel a bare word", (_name, dict, expected) => {
    // positive control: the entry exists and is the word we think it is — a
    // renamed/removed key fails here instead of vacuously passing below.
    expect(dict.tasks.waitingLabel).toBe(expected);
    expect(dict.tasks.waitingLabel).not.toContain("·");
  });

  it.each([
    ["zh", { "oc.language": "zh" }, "等待中"],
    ["en", { "oc.language": "en" }, "Waiting"],
    ["xian", { "oc.theme": "xian" }, "候緣"],
  ])("renders the %s label with no trailing separator", async (_name, ls, expected) => {
    for (const [k, v] of Object.entries(ls)) localStorage.setItem(k, v);

    const { findByTestId } = renderCard(
      mkTask({ waitingReason: "等 **fms #20054** 合併" })
    );
    const waiting = await findByTestId("task-waiting-reason");
    const label = waiting.querySelector(".task-card__waiting-label");

    // positive control #1 — the label element actually exists and is THIS
    // locale's word, so a vanished label / wrong locale fails here.
    expect(label).not.toBeNull();
    // positive control #2 — the reason still renders as markdown beside it, so
    // a card that stopped rendering the waiting block entirely fails here.
    expect(waiting.querySelector(".task-card__waiting-md strong")?.textContent)
      .toBe("fms #20054");

    // the assertion that matters: the label is EXACTLY the bare word. Re-adding
    // the "·" (in JSX or via the dict) makes this "等待中 ·" and reddens.
    expect(label?.textContent?.trim()).toBe(expected);
  });
});
