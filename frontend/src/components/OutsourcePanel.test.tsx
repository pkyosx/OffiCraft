// 外包 panel (M3 SPEC §4). Locked here — the acceptance behaviors:
//   1. Rows render the two-line shape (owner 2026-07-16, folding the
//      2026-07-14 report's lines 2+3 into one; second ruling the same day
//      puts the dot at the line start): 代號 / [上線綠點 at 行首, THEN the
//      clickable T-xxxx chip, THEN task type — one line] — NO model name
//      (the codename implies it), NO task title, NO 識別鍵, NO status word.
//   2. Ordering 依任務建立時間新→舊 (the bound TASK's created_ts, not the
//      worker's own mint stamp).
//   3. T-66a8: the 外包 content lives behind the top 外包 tab (the sidebar
//      switches 正職/外包). The 「N 人 · 上限 M」 count moved to that tab's
//      sub-line; there is no more per-panel head/collapse toggle.
//   4. Clicking a row opens a CHAT CHANNEL with that worker: the worker id
//      rides the SAME chatId hash slot (#office/chat/<ow-id>) and the chat
//      header shows 「外包 · 代號」 (the member ChatArea, reused) over the
//      SAME task line as the rail row ([T-xxxx chip → type], shared
//      OutsourceTaskLine, no dot — owner 2026-07-16: 兩邊顯示一樣的東西);
//      a T-xxxx chip (row or header) instead jumps to the task card
//      (#tasks/<taskId>).
//   5. The 招攬新成員 button (sidebar bottom) opens the cap popover on the
//      外包 tab (PATCH /api/settings); 0 annotates 已暫停指派. (No gear on the
//      task line — the 任務類型 ⚙ was deleted 2026-07-17.)
//   6. A worker with unread owner-bound chat carries the member-card unread
//      badge (owner report 2026-07-14: 外包也要有未讀紅點).
//
// Rendered through OfficePage so the row-click → hash route → ChatArea chain
// is the REAL wiring, not a stub.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { OfficePage } from "./OfficePage";
import {
  __resetMock,
  __injectMockTask,
  __injectMockTaskType,
  __injectMockChat,
  __injectMockOutsourceWorker,
} from "../api/mock";
import { api } from "../api";
import type { TaskView, OutsourceWorkerView } from "../api/adapter";

let seq = 0;

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${1000 + seq}`,
    title: `任務 ${seq}`,
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "outsource",
    executorId: `ow-${seq}`,
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

function mkWorker(over: Partial<OutsourceWorkerView>): OutsourceWorkerView {
  seq += 1;
  return {
    id: `ow-${seq}`,
    codename: `O-${seq}`,
    model: "Opus 4.6",
    effort: "high",
    taskId: `task-${seq}`,
    taskTitle: "",
    taskStatus: "in_progress",
    createdTs: Date.now() / 1000 - 600,
    ...over,
  };
}

function renderOffice() {
  return render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>
  );
}

// T-66a8: the 外包 worker list now lives behind the 外包 tab (the sidebar
// switches 正職/外包 by a top tab). Every worker-list assertion first switches
// to that tab. The tab button renders synchronously (not gated on the fetch),
// so this is safe before the rows load in.
function renderOutsource() {
  const utils = renderOffice();
  fireEvent.click(utils.getByTestId("office-tab-outsource"));
  return utils;
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  // jsdom has no scrollIntoView; ChatArea's entry positioning calls it once a
  // thread renders (the worker-chat tests) — stub like the ChatArea suites.
  Element.prototype.scrollIntoView = vi.fn();
});

describe("OutsourcePanel", () => {
  it("renders 代號 / [T-xxxx chip → task type + 上線綠點] one line — NO model·title·識別鍵·狀態字", async () => {
    const now = Date.now() / 1000;
    const task = mkTask({
      id: "t-a",
      taskNo: "T-9c21",
      title: "修 PR 回饋",
      typeKey: "review-pr",
      dedupeKey: "https://github.com/x/y/pull/482",
      createdTs: now,
    });
    __injectMockTask(task);
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-a",
        codename: "O-7",
        model: "Opus 4.6",
        taskId: "t-a",
        taskTitle: "修 PR 回饋",
        taskStatus: "in_progress",
      })
    );

    const { findByTestId } = renderOutsource();
    const row = await findByTestId("outsource-row-ow-a");
    // line 1 — the worker's name is the outsource identity label 「外包 · 代號」
    // (T-3ed8, owner 2026-07-20: consistent with the chat header / task chips).
    expect(row.textContent).toContain("外包 · O-7");
    // line 2 — ONE line (owner 2026-07-16, second ruling): the online green
    // dot at the LINE START (member-row parity; a LIVE worker is by
    // definition online — owner report 2026-07-14), THEN the T-xxxx chip,
    // THEN the bound task's TYPE.
    const taskLine = await findByTestId("outsource-task-line-ow-a");
    const chip = within(taskLine).getByTestId("outsource-task-ow-a");
    const typeLine = within(taskLine).getByTestId("outsource-type-ow-a");
    // Dot, chip and type are SIBLINGS in the same one-line container, in
    // that order — the dot is the container's FIRST element (行首).
    const dot = taskLine.querySelector(".outsource-row__online-dot");
    expect(dot).not.toBeNull();
    expect(dot!.parentElement).toBe(taskLine);
    expect(taskLine.firstElementChild).toBe(dot);
    expect(chip.parentElement).toBe(taskLine);
    expect(typeLine.parentElement).toBe(taskLine);
    expect(
      dot!.compareDocumentPosition(chip) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
    expect(
      chip.compareDocumentPosition(typeLine) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
    expect(chip.textContent).toBe("T-9c21");
    expect(typeLine.textContent).toBe("review-pr");
    expect(typeLine.querySelector(".outsource-row__online-dot")).toBeNull();
    // …and never the model / task title / 識別鍵 / status word.
    expect(row.textContent).not.toContain("Opus");
    expect(row.textContent).not.toContain("修 PR 回饋");
    expect(row.textContent).not.toContain("github.com");
    expect(row.textContent).not.toContain("進行中");
  });

  it("the type line shows the manual's DISPLAY name — the raw key stays out of the UI (T-fa76)", async () => {
    __injectMockTaskType({
      typeKey: "tm-aaaabbbbcccc",
      displayName: "審查 PR",
      purpose: "",
    });
    __injectMockTask(
      mkTask({ id: "t-named", typeKey: "tm-aaaabbbbcccc", createdTs: 60 })
    );
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-named", taskId: "t-named" })
    );

    const { findByTestId } = renderOutsource();
    const typeLine = await findByTestId("outsource-type-ow-named");
    expect(typeLine.textContent).toBe("審查 PR");
    expect(typeLine.getAttribute("title")).toBeNull();
  });

  it("an ad-hoc task (typeKey '') reads 自由代辦 on the type line", async () => {
    __injectMockTask(mkTask({ id: "t-adhoc", typeKey: "", createdTs: 50 }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-adhoc", taskId: "t-adhoc" }));

    const { findByTestId } = renderOutsource();
    const typeLine = await findByTestId("outsource-type-ow-adhoc");
    expect(typeLine.textContent).toBe("自由代辦");
  });

  it("the rail's task type line grows NO settings gear — the outsource ⚙ is gone", async () => {
    // Owner 2026-07-17: the roster gears go back; the outsource one is DELETED
    // outright (the outsource panel has no 任務類型 field to host it, so unlike
    // the member gear it has nowhere to move to). A worker with a REAL typeKey
    // — the exact case that used to grow the gear — must show none.
    __injectMockTask(
      mkTask({ id: "t-geared", typeKey: "review-pr", createdTs: 65 })
    );
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-geared", taskId: "t-geared" })
    );

    const { findByTestId, queryByTestId } = renderOutsource();
    const typeLine = await findByTestId("outsource-type-ow-geared");
    expect(typeLine.textContent).toBe("review-pr");
    expect(queryByTestId("outsource-type-settings-ow-geared")).toBeNull();
    // Testid-independent, via a LIVE label: 任務類型設定 no longer renders
    // anywhere, so its old class/label cannot be asserted against without the
    // negative going unfalsifiable. What IS still live is the type text — this
    // row shows the type but offers no jump off it.
    const taskLine = await findByTestId("outsource-task-line-ow-geared");
    expect(taskLine.querySelector("button[title*='設定']")).toBeNull();
  });

  it("clicking the T-xxxx chip jumps to the task page — not the chat", async () => {
    __injectMockTask(
      mkTask({ id: "t-jump", taskNo: "T-950f", typeKey: "review-pr", createdTs: 70 })
    );
    __injectMockOutsourceWorker(mkWorker({ id: "ow-jump", taskId: "t-jump" }));

    const { findByTestId } = renderOutsource();
    const chip = await findByTestId("outsource-task-ow-jump");
    expect(chip.textContent).toBe("T-950f");
    fireEvent.click(chip);
    // The chip routes to the task card's locate anchor (owner 2026-07-14:
    // 點 task id 連到該任務頁) and must NOT also open the worker chat.
    expect(window.location.hash).toBe("#tasks/t-jump");
  });

  it("unread worker→owner chat shows the member-card unread badge", async () => {
    __injectMockTask(mkTask({ id: "t-u", createdTs: 90 }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-u", codename: "O-5", taskId: "t-u" })
    );
    // Two INBOUND worker→owner messages past the (absent) watermark — the
    // same unread rule the member roster badge counts (mock unreadCountOf).
    for (const [id, body] of [
      ["m-1", "回報 1"],
      ["m-2", "回報 2"],
    ]) {
      __injectMockChat({
        id,
        from: "ow-u",
        to: "owner",
        body,
        ts: Date.now() / 1000 - 30,
        attachments: [],
        replyCardId: null,
      });
    }

    // "Watched" = window focused (jsdom's hasFocus() is false by default) —
    // pin it true so the SELECTED row's suppression is what's under test.
    const spy = vi.spyOn(document, "hasFocus").mockReturnValue(true);
    try {
      const { findByTestId, queryByTestId } = renderOutsource();
      const badge = await findByTestId("outsource-unread-ow-u");
      expect(badge.textContent).toBe("2");

      // Opening the worker's chat suppresses the badge on the open, watched
      // row (member-card parity — the open thread's auto-mark consumes reads,
      // so the badge must never accumulate there).
      fireEvent.click(await findByTestId("outsource-row-ow-u"));
      await waitFor(() =>
        expect(queryByTestId("outsource-unread-ow-u")).toBeNull()
      );
    } finally {
      spy.mockRestore();
    }
  });

  it("orders rows by the bound TASK's created_ts, newest first", async () => {
    const now = Date.now() / 1000;
    // Worker mint order is the REVERSE of the task creation order on purpose:
    // the sort key must be the task's stamp, not the worker's.
    __injectMockTask(mkTask({ id: "t-old", title: "舊任務", createdTs: now - 9000 }));
    __injectMockTask(mkTask({ id: "t-new", title: "新任務", createdTs: now - 10 }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-old", codename: "S-1", taskId: "t-old", createdTs: now })
    );
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-new", codename: "O-2", taskId: "t-new", createdTs: now - 5000 })
    );

    const { findByTestId } = renderOutsource();
    const list = await findByTestId("outsource-list");
    await waitFor(() => {
      const rows = within(list).getAllByTestId(/^outsource-row-/);
      expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual([
        "outsource-row-ow-new",
        "outsource-row-ow-old",
      ]);
    });
  });

  it("the 外包 tab carries the count sub-line 「N 人 · 上限 M」; the tab switch shows/hides the list", async () => {
    // T-66a8: the old head 「N / 上限」 + collapse toggle are gone. The count
    // moved to the tab sub-line, and the 正職/外包 tab IS the switcher.
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));

    const { findByTestId, getByTestId, queryByTestId } = renderOutsource();
    // The mock cap default mirrors the server default (3).
    await waitFor(() =>
      expect(getByTestId("outsource-tab-sub").textContent).toBe("1 人 · 上限 3")
    );

    // On the 外包 tab the worker list shows; switching to 正職 hides it (and
    // shows the roster instead).
    await findByTestId("outsource-row-ow-1");
    fireEvent.click(getByTestId("office-tab-staff"));
    expect(queryByTestId("outsource-list")).toBeNull();
    fireEvent.click(getByTestId("office-tab-outsource"));
    expect(queryByTestId("outsource-list")).not.toBeNull();
  });

  it("clicking a row opens the worker chat channel (hash chatId + 外包 header)", async () => {
    __injectMockTask(
      mkTask({
        id: "t-1",
        taskNo: "T-50c6",
        title: "查帳單",
        typeKey: "review-pr",
        createdTs: 100,
      })
    );
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        codename: "H-3",
        taskId: "t-1",
        taskTitle: "查帳單",
        taskStatus: "waiting_external",
      })
    );

    const { findByTestId, findByText } = renderOutsource();
    fireEvent.click(await findByTestId("outsource-row-ow-1"));

    // The worker id rides the SAME chatId hash slot as a member chat.
    expect(window.location.hash).toBe("#office/chat/ow-1");
    // ChatArea header: 「外包 · 代號」 + the SAME task line the rail row shows
    // (owner 2026-07-16: 兩邊顯示一樣的東西 — [T-xxxx chip → type]), NOT the
    // old 狀態 · 標題 pair, and NO dot (presence lives only in the rail row).
    await findByText("外包 · H-3");
    const sub = await findByTestId("outsource-chat-sub");
    const chip = within(sub).getByTestId("outsource-chat-task-ow-1");
    const typeLine = within(sub).getByTestId("outsource-chat-type-ow-1");
    expect(chip.textContent).toBe("T-50c6");
    expect(typeLine.textContent).toBe("review-pr");
    expect(
      chip.compareDocumentPosition(typeLine) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
    expect(sub.querySelector(".outsource-row__online-dot")).toBeNull();
    // …and no settings gear either (owner 2026-07-17: the outsource ⚙ is gone
    // from BOTH surfaces). ow-1 carries a real "review-pr" typeKey, so this is
    // the case that would grow one if the gear ever came back.
    expect(
      within(sub).queryByTestId("outsource-chat-type-settings-ow-1")
    ).toBeNull();
    // T-dfae: the chat header's 任務/角色設定 buttons are wired ONLY for roster
    // members. An outsource peer has no role to define, and its tasks are not
    // separable from every other worker's (all collapse to the single
    // "outsource" executor key) — so BOTH jumps must stay absent here rather
    // than lie. Keyed off the live labels the member header does render.
    expect(within(sub).queryByLabelText(zh.chat.roleSettingsLink)).toBeNull();
    expect(within(sub).queryByLabelText(zh.chat.tasksLink)).toBeNull();
    // The old subtitle's status word / task title are GONE from the header.
    expect(sub.textContent).not.toContain("等待外部");
    expect(sub.textContent).not.toContain("查帳單");
    // The row carries the open-chat highlight.
    const row = await findByTestId("outsource-row-ow-1");
    expect(row.className).toContain("outsource-row--selected");
  });

  it("the header's T-xxxx chip jumps to the task page — not the worker detail", async () => {
    __injectMockTask(
      mkTask({ id: "t-hdr", taskNo: "T-77aa", typeKey: "review-pr", createdTs: 80 })
    );
    __injectMockOutsourceWorker(mkWorker({ id: "ow-hdr", taskId: "t-hdr" }));

    const { findByTestId } = renderOutsource();
    fireEvent.click(await findByTestId("outsource-row-ow-hdr"));
    const chip = await findByTestId("outsource-chat-task-ow-hdr");
    fireEvent.click(chip);
    // Same locate-anchor route as the rail chip; stopPropagation keeps the
    // header's open-detail click from also firing.
    expect(window.location.hash).toBe("#tasks/t-hdr");
  });

  it("clicking the avatar opens the worker detail panel (not the chat)", async () => {
    __injectMockTask(mkTask({ id: "t-1", createdTs: 100 }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", codename: "O-4", taskId: "t-1" })
    );

    const { findByTestId, queryByTestId } = renderOutsource();
    fireEvent.click(await findByTestId("outsource-detail-ow-1"));

    // The avatar routes to the worker detail hash, NOT the chat slot.
    expect(window.location.hash).toBe("#office/worker/ow-1");
    await findByTestId("worker-detail-task");
    // The chat pane is not what opened.
    expect(queryByTestId("outsource-chat-sub")).toBeNull();
  });

  it("owner messages a worker through the reused chat composer", async () => {
    __injectMockTask(mkTask({ id: "t-1", createdTs: 100 }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", codename: "O-9", taskId: "t-1" }));

    const { findByTestId, findByPlaceholderText } = renderOutsource();
    fireEvent.click(await findByTestId("outsource-row-ow-1"));

    const input = await findByPlaceholderText("回覆 外包 · O-9…");
    fireEvent.change(input, { target: { value: "進度如何？" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(async () => {
      const thread = await api.listChat("ow-1");
      expect(thread.map((m) => [m.to, m.body])).toEqual([
        ["ow-1", "進度如何？"],
      ]);
    });
  });

  it("招攬新成員 popover steppers the cap down to 0 → 已暫停指派", async () => {
    // T-66a8: on the 外包 tab the 招攬新成員 button (sidebar bottom) opens the
    // 外包上限設定 popover — the same stepper the head gear used to host.
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));

    const { findByTestId, getByTestId, queryByTestId } = renderOutsource();
    await waitFor(() =>
      expect(getByTestId("outsource-tab-sub").textContent).toBe("1 人 · 上限 3")
    );
    expect(queryByTestId("outsource-paused")).toBeNull();

    // 外包上限設定 popover (mockup): −/＋ stepper + 無限 + 完成.
    fireEvent.click(await findByTestId("office-recruit"));
    expect(getByTestId("outsource-cap-pop").textContent).toContain(
      "外包上限設定"
    );
    expect(getByTestId("outsource-cap-value").textContent).toBe("3");
    fireEvent.click(getByTestId("outsource-cap-dec"));
    fireEvent.click(getByTestId("outsource-cap-dec"));
    fireEvent.click(getByTestId("outsource-cap-dec"));
    expect(getByTestId("outsource-cap-value").textContent).toBe("0");
    // − bottoms out at 0 (disabled — no negative finite caps).
    expect(
      (getByTestId("outsource-cap-dec") as HTMLButtonElement).disabled
    ).toBe(true);
    fireEvent.click(getByTestId("outsource-cap-save"));

    await waitFor(() =>
      expect(getByTestId("outsource-tab-sub").textContent).toBe("1 人 · 上限 0")
    );
    // 0 = assignment paused — annotated explicitly (Seth ruling).
    expect(getByTestId("outsource-paused").textContent).toBe("已暫停指派");
    // The PATCH really landed server-side (mock parity with /api/settings).
    expect((await api.getServerSettings()).outsourceMaxParallel).toBe(0);
  });

  it("無限 sets the cap to -1 (wire) and the tab sub-line shows 上限 ∞", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));

    const { findByTestId, getByTestId, queryByTestId } = renderOutsource();
    fireEvent.click(await findByTestId("office-recruit"));
    fireEvent.click(getByTestId("outsource-cap-unlimited"));
    expect(getByTestId("outsource-cap-value").textContent).toBe("∞");
    fireEvent.click(getByTestId("outsource-cap-save"));

    await waitFor(() =>
      expect(getByTestId("outsource-tab-sub").textContent).toBe("1 人 · 上限 ∞")
    );
    // Unlimited is NOT paused.
    expect(queryByTestId("outsource-paused")).toBeNull();
    // The wire value is -1 (spec SettingsDTO: -1 = 無限).
    expect((await api.getServerSettings()).outsourceMaxParallel).toBe(-1);
  });
});
