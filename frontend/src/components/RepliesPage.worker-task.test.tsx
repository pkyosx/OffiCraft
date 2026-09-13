// 請示 page — the ASKER'S CURRENT TASK on an outsource worker's card (T-196).
//
// The ask (owner rc-dce285c5274c): a card opened by an outsource worker gave him
// no way to see which task that worker is on, so he could not judge the card's
// weight. His ruling names both the scope and the shape:「我覺得只在 UI 上補上顯示
// 就好 就像在 chat 那邊 使用者列表上 outsource worker 會顯示他在進行的工作是哪一
// 個」— display only, and the office rail is the reference.
//
// What is pinned here, and why each one is a way to get this wrong:
//   1. The line is the WORKER'S, not the CARD'S. A card bound to task B, opened
//      by a worker sitting on task A, shows A here and B in the card's own task
//      row. Reading `row.task` instead passes every other assertion in this file.
//   2. It is the rail's own two pieces (clickable 任務編號 chip → #tasks/<id>,
//      then the task type; the real title under it), NOT a second rendering.
//   3. A worker with NO task shows the shared placeholder, not blank chrome and
//      not a chip pointing nowhere.
//   4. A STAFF asker renders nothing — there is no such fact for a member
//      (MemberDTO carries no task), so asserting the absence is the only way to
//      keep a future "just render it for everyone" from inventing one.
//   5. Clicking the chip jumps WITHOUT toggling the card open, since the whole
//      article is the expand/collapse control.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RepliesPage } from "./RepliesPage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import {
  __resetMock,
  __injectMockMember,
  __injectMockReplyCard,
} from "../api/mock";
import type { OutsourceWorkerView, ReplyCard } from "../api/adapter";

// The worker rows the page resolves for its `ow-` askers. Per test, so a case
// can hand the SAME worker a different current task without touching the cards.
let workerRows: OutsourceWorkerView[] = [];

vi.mock("../hooks/useWorkerCodenames", () => ({
  useWorkerCodenames: (ids: readonly string[]) =>
    new Map(
      ids
        .filter((id) => id.startsWith("ow-"))
        .map((id) => [id, id === "ow-a" ? "O-9" : "O-8"]),
    ),
  useWorkerAvatarUrls: () => new Map(),
  useWorkerCurrentTasks: (ids: readonly string[]) =>
    new Map(
      workerRows.filter((w) => ids.includes(w.id)).map((w) => [w.id, w]),
    ),
}));

function mkWorker(over: Partial<OutsourceWorkerView>): OutsourceWorkerView {
  return {
    id: "ow-a",
    codename: "O-9",
    model: "opus",
    effort: "medium",
    status: "active",
    taskId: "T-42",
    taskNo: "T-42",
    taskTitle: "把請示卡列表補上外包正在做的任務",
    taskTypeName: "OffiCraft · 開發",
    ...over,
  };
}

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "ow-a",
    kind: "decision",
    summary: "要照這個方向做嗎？",
    body: "",
    options: [{ text: "好", aiPick: true }],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 10 * 60,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

/** The card article itself — the expand/collapse control. It carries no
 * data-testid (the seam is `[data-reply-card-id][aria-expanded]`, the same one
 * RepliesPage.test.tsx opens cards through); this fails loudly if that moves. */
async function findCard(id = "rc-1"): Promise<HTMLElement> {
  return await waitFor(() => {
    const el = document.querySelector<HTMLElement>(
      `[data-reply-card-id="${id}"][aria-expanded]`,
    );
    expect(el, "the reply card article seam moved").not.toBeNull();
    return el as HTMLElement;
  });
}

function renderPage() {
  return render(
    <I18nProvider>
      <ReplyCardsProvider>
        <RepliesPage />
      </ReplyCardsProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  __resetMock();
  workerRows = [];
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("RepliesPage — the asking outsource worker's current task", () => {
  it("shows the worker's own task: number, type and title", async () => {
    workerRows = [mkWorker({})];
    __injectMockReplyCard(mkCard({}));

    const { findByTestId } = renderPage();

    expect((await findByTestId("reply-card-rc-1-task-ow-a")).textContent).toBe(
      "T-42",
    );
    expect((await findByTestId("reply-card-rc-1-type-ow-a")).textContent).toBe(
      "OffiCraft · 開發",
    );
    expect((await findByTestId("reply-card-task-title-rc-1")).textContent).toBe(
      "把請示卡列表補上外包正在做的任務",
    );
  });

  it("carries the FULL title on the hover tooltip, like the rail row", async () => {
    workerRows = [mkWorker({})];
    __injectMockReplyCard(mkCard({}));

    const { findByTestId } = renderPage();

    expect(
      (await findByTestId("reply-card-task-title-rc-1")).getAttribute("title"),
    ).toBe("把請示卡列表補上外包正在做的任務");
  });

  it("reads the WORKER, not the card's binding: a card bound to another task still shows the worker's own", async () => {
    // The whole point of the ticket. A card bound to T-7 opened by a worker
    // sitting on T-42: the identity line is the WORKER's (T-42), the card's own
    // task row is the CARD's (T-7). Reading row.task here would show T-7 twice
    // and every other assertion in this file would still pass.
    workerRows = [mkWorker({})];
    __injectMockReplyCard(
      mkCard({
        task: { id: "T-7", typeKey: "tm-x", title: "另一張票的標題" },
      }),
    );

    const { findByTestId, findByText } = renderPage();

    expect((await findByTestId("reply-card-rc-1-task-ow-a")).textContent).toBe(
      "T-42",
    );
    expect((await findByTestId("reply-card-task-title-rc-1")).textContent).toBe(
      "把請示卡列表補上外包正在做的任務",
    );
    // …and the card's OWN task row still speaks for the binding, untouched.
    expect(await findByText("另一張票的標題")).toBeTruthy();
  });

  it("shows it on a card bound to NO task at all (a plain chat ask)", async () => {
    workerRows = [mkWorker({})];
    __injectMockReplyCard(mkCard({ task: null }));

    const { findByTestId } = renderPage();

    expect((await findByTestId("reply-card-rc-1-task-ow-a")).textContent).toBe(
      "T-42",
    );
  });

  it("the task number is a jump to that task, and does NOT open the card", async () => {
    workerRows = [mkWorker({})];
    __injectMockReplyCard(mkCard({}));

    const { findByTestId } = renderPage();
    const card = await findCard();
    expect(card.getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(await findByTestId("reply-card-rc-1-task-ow-a"));

    expect(window.location.hash).toBe("#tasks/T-42");
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });

  it("a worker with no current task shows ONLY the placeholder — no chip, and no 自由代辦 beside it", async () => {
    // 🔴 Every task field empty, which is what "no task" actually looks like on
    // the wire. An earlier fixture cleared taskId/taskNo/taskTitle but left
    // taskTypeName set — a state the server cannot produce — and it hid the
    // real bug: OutsourceTaskLine ALWAYS draws its type slot, falling back to
    // 自由代辦, so the row said 自由代辦 and 無當前任務 at the same time.
    workerRows = [
      mkWorker({ taskId: "", taskNo: "", taskTitle: "", taskTypeName: "" }),
    ];
    __injectMockReplyCard(mkCard({}));

    const { findByTestId, queryByTestId, queryByText } = renderPage();

    const title = await findByTestId("reply-card-task-title-rc-1");
    expect(title.textContent).toBe("無當前任務");
    expect(queryByTestId("reply-card-rc-1-task-ow-a")).toBeNull();
    expect(queryByTestId("reply-card-rc-1-type-ow-a")).toBeNull();
    expect(queryByText("自由代辦")).toBeNull();
  });

  it("a RELEASED worker shows nothing — its row still carries the task it finished", async () => {
    // Release flips the status and leaves task_id alone, and the per-id read
    // serves released rows in full — which is exactly why this page can resolve
    // them. So the row is a true record of FINISHED work and a false answer to
    // 「現在在做什麼」. The office rail cannot make this mistake: its list skips
    // released workers outright. This surface has to say so itself.
    workerRows = [mkWorker({ status: "released" })];
    __injectMockReplyCard(mkCard({}));

    const { queryByTestId } = renderPage();

    await findCard();
    expect(queryByTestId("reply-card-rc-1-task-ow-a")).toBeNull();
    expect(queryByTestId("reply-card-task-title-rc-1")).toBeNull();
  });

  it("a worker that is assigned but has not started yet still shows its task", async () => {
    // `assigned` is a real minted status, not a synonym for `active`: leaving it
    // out of the allowlist would hide the line for a worker that genuinely holds
    // the task it is about to start.
    workerRows = [mkWorker({ status: "assigned" })];
    __injectMockReplyCard(mkCard({}));

    const { findByTestId } = renderPage();

    expect((await findByTestId("reply-card-rc-1-task-ow-a")).textContent).toBe(
      "T-42",
    );
  });

  it("an UNKNOWN status shows nothing either — the check is an allowlist, not a list of the terminal ones", async () => {
    // Naming the terminal status instead would be a prediction: the next
    // terminal state added server-side would arrive here as a live-looking row.
    workerRows = [mkWorker({ status: "retired-someday" })];
    __injectMockReplyCard(mkCard({}));

    const { queryByTestId } = renderPage();

    await findCard();
    expect(queryByTestId("reply-card-task-title-rc-1")).toBeNull();
  });

  it("a task_id whose task no longer resolves shows the placeholder, not 自由代辦", async () => {
    // The server writes task_id unconditionally but fills task_no / task_title
    // / task_type_* only when it could resolve the task. Gating on task_id
    // would let this row draw the type slot's 自由代辦 fallback over 無當前任務
    // — the same contradiction, one field later.
    workerRows = [
      mkWorker({ taskId: "T-42", taskNo: "", taskTitle: "", taskTypeName: "" }),
    ];
    __injectMockReplyCard(mkCard({}));

    const { findByTestId, queryByTestId, queryByText } = renderPage();

    expect((await findByTestId("reply-card-task-title-rc-1")).textContent).toBe(
      "無當前任務",
    );
    expect(queryByTestId("reply-card-rc-1-type-ow-a")).toBeNull();
    expect(queryByText("自由代辦")).toBeNull();
  });

  it("a live worker with an ad-hoc task still shows 自由代辦 — the fallback is only wrong when there is NO task", async () => {
    workerRows = [mkWorker({ taskTypeName: "", taskTypeKey: "" })];
    __injectMockReplyCard(mkCard({}));

    const { findByTestId } = renderPage();

    expect((await findByTestId("reply-card-rc-1-type-ow-a")).textContent).toBe(
      "自由代辦",
    );
  });

  it("renders nothing for a STAFF asker — a member has no such fact", async () => {
    __injectMockMember({ id: "mira", name: "銀月", kind: "staff" });
    __injectMockReplyCard(mkCard({ from: "mira" }));

    const { queryByTestId } = renderPage();

    await findCard();
    expect(queryByTestId("reply-card-task-title-rc-1")).toBeNull();
  });

  it("renders nothing for a worker that never resolved — silence, not an asserted 'no task'", async () => {
    workerRows = []; // the per-id read has not landed, or answered 404
    __injectMockReplyCard(mkCard({}));

    const { queryByTestId } = renderPage();

    await findCard();
    expect(queryByTestId("reply-card-task-title-rc-1")).toBeNull();
  });
});
