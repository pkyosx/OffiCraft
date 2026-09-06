// TaskCard — 任務編號 chip 點擊複製 (owner 2026-07-19 圈截圖). Locked:
//   1. Clicking the #<編號> chip writes the task number to the clipboard, and
//      that number IS the task id (T-5291) — so what lands on the clipboard is
//      exactly the string `#tasks/<id>` and MCP `get_task(task_id)` accept.
//   2. On a successful write the chip flashes a transient 「已複製」feedback.
//   3. The chip is a keyboard-operable button with a copy aria-label.
//   4. Clicking the chip must NOT toggle the card open (the copy is its own job).
//
// 🔴 WHAT THIS FILE USED TO SAY, and why that had to go (T-5291 round 3).
// Point 1 read "writes the DISPLAY task number (task.taskNo, NEVER the internal
// id)", and line 75 asserted it for real:
//
//     expect(writeText).not.toHaveBeenCalledWith(task.id);
//
// That is the OPPOSITE of what this ticket shipped. It stayed green only
// because the fixture forced `taskNo` ("T-1001") and `id` ("task-1") into a
// shape production can no longer produce — the server sends task_no = id. The
// first person to make the fixture realistic would have been told the copy
// feature was broken while having broken nothing. A live assertion of a retired
// rule is worse than a stale comment: it hands the next reader a false verdict.
//
// So the fixture is production-shaped (`taskNo === id`, a real `t-<hex12>`), the
// reverse assertion is deleted rather than loosened, and the assertion this
// ticket actually needed is added below: the clipboard string equals the task
// id, and it round-trips through the route parser that is supposed to accept it.
// Before this file, NOTHING in the repo pinned that — the one test touching the
// clipboard asserted the contrary.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { parseHash } from "../lib/hashRoute";
import { __resetMock, __injectMockTask } from "../api/mock";
import type { TaskView } from "../api/adapter";

let seq = 0;

// Production shape: `t-` + 12 hex, and `taskNo` IS that same string — the exact
// pair the server now sends (server/ocserverd/domain.go TaskNo). Deliberately
// NOT two different values: a fixture that keeps them apart is a fixture that
// can only test a rule this ticket removed.
function mkTask(over: Partial<TaskView> = {}): TaskView {
  seq += 1;
  const id = `t-c09d5291${String(seq).padStart(4, "0")}`;
  return {
    id,
    taskNo: id,
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

function stubClipboard() {
  // Typed parameter on purpose: an untyped `vi.fn(async () => {})` gives the
  // mock a zero-length call tuple, and reading `mock.calls[0][0]` — which the
  // round-trip assertion below has to do — is then a tsc error rather than a
  // value. The stub mirrors `navigator.clipboard.writeText(data: string)`.
  const writeText = vi.fn(async (_text: string) => {});
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
  });
  return writeText;
}

function renderPage() {
  return render(
    <I18nProvider>
      <TasksPage />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

describe("TaskCard 任務編號 chip 點擊複製 (owner 2026-07-19)", () => {
  it("copies the task number to the clipboard and flashes 已複製", async () => {
    const writeText = stubClipboard();
    const task = mkTask({ title: "複製編號" });
    __injectMockTask(task);

    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-no"));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith(task.taskNo));

    // transient success feedback appears after the write resolves.
    expect(await findByTestId("task-no-copied")).toBeTruthy();
  });

  // 🔴 THE TICKET'S OWN CLAIM, pinned for the first time.
  //
  // T-5291 exists so that a number read off the card can be pasted back and
  // used. The chip is the ONE surface that hands that string to a human, so
  // this is where the claim is testable. Everything else in the ticket (the
  // server projection, the spec prose, the mock) only makes it POSSIBLE.
  //
  // The expected value is a hard-coded literal, not `task.id` and not
  // `deriveTaskNo(...)`: an assertion that calls the same function the code
  // calls moves WITH a regression instead of catching it (the mistake T-5291
  // round 1 already made once, commit 1bf12db3).
  it("puts the task id itself on the clipboard — the 貼回去就能用 claim", async () => {
    const writeText = stubClipboard();
    const ID = "t-72dd79b666d0";
    __injectMockTask(mkTask({ id: ID, taskNo: ID, title: "貼回去" }));

    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-no"));
    await waitFor(() => expect(writeText).toHaveBeenCalledTimes(1));

    const copied = writeText.mock.calls[0][0];
    expect(
      copied,
      "the string a human copies off the card must BE the task id — anything " +
        "else and pasting it back 404s (lookup is WHERE id = ?, byte-exact)"
    ).toBe("t-72dd79b666d0");

    // And it must survive the trip it exists for: pasted after `#tasks/`, the
    // route parser has to resolve it to this same task. This is the whole
    // round trip, measured rather than asserted in prose.
    expect(parseHash(`#tasks/${copied}`)).toEqual({
      page: "tasks",
      taskId: "t-72dd79b666d0",
    });
  });

  it("exposes the chip as a button with a copy aria-label", async () => {
    const ID = "t-72dd79b666d0";
    __injectMockTask(mkTask({ id: ID, taskNo: ID, title: "無障礙" }));
    const { findByTestId } = renderPage();
    const chip = await findByTestId("task-no");
    expect(chip.tagName).toBe("BUTTON");
    expect(chip.getAttribute("aria-label")).toBe(
      "複製任務編號 t-72dd79b666d0"
    );
  });

  it("clicking the chip copies without toggling the card open", async () => {
    const writeText = stubClipboard();
    const task = mkTask({ title: "不展開" });
    __injectMockTask(task);

    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    expect(card.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(await findByTestId("task-no"));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(task.taskNo));
    // the copy click never flipped the whole-card toggle.
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });
});
