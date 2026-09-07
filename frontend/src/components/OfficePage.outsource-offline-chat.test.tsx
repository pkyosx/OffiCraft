// T-128 · 外包成員離線時的聊天室畫面,跟正職一樣.
//
// 🔴 WHY THIS IS AN OfficePage TEST AND NOT A ChatArea ONE. ChatArea already
// gets this right and has always got it right: it locks only a peer with NO
// queue path and otherwise draws the 「訊息會排隊」 notice + ⚡喚醒 row off
// `lifecycle` and `onWake` (ChatArea.offline-lock.test.tsx pins that matrix).
// The outsource chat never reached any of it because OfficePage projected the
// worker onto a Member with `lifecycle: "online"` HARDWIRED and passed no
// `onWake`. Both facts live here, in the wiring, so only a test that renders
// the real OfficePage → mock adapter → ChatArea chain can see them.
//
// ⚠️ THE TWO HALVES ARE ONE CHANGE, and each of them alone is worse than the
// bug. Dropping the hardwire without wiring `onWake` locks the only outsource
// composer with no way out; wiring `onWake` while the lifecycle still says
// online hides the row it feeds. The first two cases below are what say so.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { api } from "../api";
import { zh } from "../i18n/locales/zh";
import {
  __resetMock,
  __injectMockChat,
  __injectMockOutsourceWorker,
} from "../api/mock";
import type { MemberLifecycle } from "../types";

const WORKER_ID = "ow-t128aa01";
const CODENAME = "O-128";

function injectWorker(presence: MemberLifecycle | undefined) {
  __injectMockOutsourceWorker({
    id: WORKER_ID,
    codename: CODENAME,
    model: "Opus 4.6",
    effort: "high",
    status: "active",
    taskId: "t-128",
    taskTitle: "整理報告",
    taskStatus: "in_progress",
    presence,
  });
  __injectMockChat({
    id: "m-t128",
    from: WORKER_ID,
    to: "owner",
    body: "外包回報。",
    ts: Date.now() / 1000 - 60,
    attachments: [],
    replyCardId: null,
  });
}

/** Open the worker's chat room and hand back the composer surfaces ChatArea
 *  draws — re-queried on every call so a presence flip is observable. */
async function openWorkerChat() {
  window.location.hash = `#office/chat/${WORKER_ID}`;
  const utils = render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>,
  );
  // The outsource header subtitle is the proof we are in the WORKER room (and
  // not the roster[0] member chat) before anything below is asserted.
  await utils.findByTestId("outsource-chat-sub");
  const query = () => ({
    input: utils.container.querySelector("textarea.chat__input"),
    locked: utils.container.querySelector(".chat__composer-locked"),
    wakeRow: utils.container.querySelector(".chat__wake-row"),
    wakeBtn: utils.container.querySelector<HTMLButtonElement>(
      "button.chat__wake-btn",
    ),
  });
  return { ...utils, query };
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
});

describe("OfficePage — an outsource worker's chat room", () => {
  it.each<MemberLifecycle>(["offline", "stopped", "waking", "stopping"])(
    "%s worker → the SAME unlocked composer + queue notice + ⚡喚醒 a 正職 gets",
    async (presence) => {
      injectWorker(presence);
      const { query } = await openWorkerChat();
      const { input, locked, wakeRow, wakeBtn } = query();
      expect(locked).toBeNull();
      expect(input).not.toBeNull();
      expect(wakeRow).not.toBeNull();
      // The button is the half `onWake` decides; the notice alone would leave
      // the owner with an honest message and no way to act on it.
      expect(wakeBtn).not.toBeNull();
      expect(wakeRow?.textContent ?? "").toContain(CODENAME);
    },
  );

  it("online worker → the plain composer, unchanged: no wake row, no lock", async () => {
    injectWorker("online");
    const { query } = await openWorkerChat();
    const { input, locked, wakeRow } = query();
    expect(input).not.toBeNull();
    expect(locked).toBeNull();
    expect(wakeRow).toBeNull();
  });

  it("a worker with NO presence on the wire reads offline, never a fabricated online", async () => {
    // Released / never dispatched / an older server that dropped the field.
    // The pre-T-128 projection answered "online" for every one of these.
    injectWorker(undefined);
    const { query } = await openWorkerChat();
    expect(query().wakeRow).not.toBeNull();
  });

  it("⚡喚醒 re-dispatches THIS worker through the real adapter", async () => {
    // 🔴 THE ASSERTION THAT CANNOT BE FAKED BY OPTIMISM. The button's own
    // 「喚醒中…」 is set by ChatArea BEFORE the promise settles, so an
    // `onWake={async () => {}}` that never touches the adapter renders exactly
    // the same thing — measured: the button-text assertion alone stays GREEN
    // with the `api.restartWorker` line deleted. That is the T-7fa1 shape (a
    // real signal, produced correctly, dropped in the middle), so the wake is
    // measured at the SEAM, spying through to the real mock rather than
    // replacing it.
    const restart = vi.spyOn(api, "restartWorker");
    injectWorker("stopped");
    const { query } = await openWorkerChat();
    const btn = query().wakeBtn!;
    expect(btn.textContent).toContain(zh.chat.wakeButton);

    fireEvent.click(btn);

    await waitFor(() => expect(restart).toHaveBeenCalledTimes(1));
    // …and on THIS worker, not whichever peer the roster happened to select.
    expect(restart).toHaveBeenCalledWith(WORKER_ID);
    // The call went THROUGH: the mock flipped the worker to `waking` and emitted
    // its SSE topic, useOutsourceWorkers refetched, and the room still offers
    // the queue path (waking is not online).
    await waitFor(() =>
      expect(query().wakeBtn?.textContent).toContain(zh.chat.wakePending),
    );
    expect(query().wakeRow).not.toBeNull();
  });
});
