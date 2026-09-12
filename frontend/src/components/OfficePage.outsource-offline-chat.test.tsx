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
  __injectMockMember,
  __injectMockOutsourceWorker,
} from "../api/mock";
import { ApiError } from "../api/errors";
import type { MemberLifecycle } from "../types";

const WORKER_ID = "ow-t128aa01";
const CODENAME = "O-128";

function injectWorker(
  presence: MemberLifecycle | undefined,
  opts: { history?: boolean } = {},
) {
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
  // The CENTRAL offline card only renders on an EMPTY thread, so the parity
  // cases below opt out of the seeded history. Everything else keeps it.
  if (opts.history !== false) {
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
}

/** A LIVE 正職 in the SAME presence state — the comparison subject for the
 *  parity cases. It is a different projection (the wire → `toMember` mapper)
 *  reaching the same component, which is what makes the comparison say
 *  something: OfficePage builds the outsource peer by hand. Fresh id ⇒ no
 *  seeded chat history, so its thread is empty like the worker's. */
const STAFF_ID = "m-t128staff";
const STAFF_NAME = "對照正職";
function injectStaffPeer(presence: MemberLifecycle) {
  __injectMockMember({
    id: STAFF_ID,
    kind: "staff",
    name: STAFF_NAME,
    presence,
  });
}

/** Open a chat room and hand back the composer surfaces ChatArea draws —
 *  re-queried on every call so a presence flip is observable. */
function openChat(peerId: string) {
  window.location.hash = `#office/chat/${peerId}`;
  const utils = render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>,
  );
  const query = () => ({
    input: utils.container.querySelector<HTMLTextAreaElement>(
      "textarea.chat__input",
    ),
    send: utils.container.querySelector<HTMLButtonElement>("button.chat__send"),
    locked: utils.container.querySelector(".chat__composer-locked"),
    wakeRow: utils.container.querySelector(".chat__wake-row"),
    wakeBtn: utils.container.querySelector<HTMLButtonElement>(
      "button.chat__wake-btn",
    ),
  });
  return { ...utils, query };
}

async function openWorkerChat() {
  const utils = openChat(WORKER_ID);
  // The outsource header subtitle is the proof we are in the WORKER room (and
  // not the roster[0] member chat) before anything below is asserted.
  await utils.findByTestId("outsource-chat-sub");
  return utils;
}

async function openStaffChat() {
  const utils = openChat(STAFF_ID);
  // The header name is the proof the EXPLICIT id resolved to the injected
  // member and not to the roster[0] fallback.
  await waitFor(() =>
    expect(
      utils.container.querySelector(".chat__header-name")?.textContent,
    ).toBe(STAFF_NAME),
  );
  return utils;
}

/** What the room LOOKS LIKE, read back out of the DOM and normalised on the
 *  peer's own displayed name so an 外包 room and a 正職 room are comparable.
 *  Deliberately reads the RENDERED output, never the props that produced it —
 *  the two sides are built by two different projections and only the rendered
 *  result is a shared yardstick. */
function projection(container: HTMLElement) {
  const peerName =
    container.querySelector(".chat__header-name")?.textContent ?? "";
  const norm = (s: string | null | undefined) =>
    (s ?? "").split(peerName).join("«PEER»");
  const card = container.querySelector(".chat__offline");
  return {
    centralCard:
      card === null
        ? null
        : {
            title: norm(
              card.querySelector(".chat__offline-title")?.textContent,
            ),
            hint: norm(card.querySelector(".chat__offline-hint")?.textContent),
          },
    wakeRow:
      container.querySelector(".chat__wake-row") === null
        ? null
        : norm(container.querySelector(".chat__wake-row")?.textContent),
    hasInput: container.querySelector("textarea.chat__input") !== null,
    hasLock: container.querySelector(".chat__composer-locked") !== null,
  };
}

/** Press the in-place ⚡喚醒 on the open room and let the outcome settle, then
 *  read back the button's post-outcome state. The click is verified to have
 *  LATCHED (the optimistic disable) before the wait, so a handler that never
 *  ran cannot satisfy the "recovered" wait by simply never having started. */
async function wakeAndReadBack(utils: ReturnType<typeof openChat>) {
  const btn = utils.query().wakeBtn!;
  expect(btn.disabled).toBe(false);
  fireEvent.click(btn);
  expect(utils.query().wakeBtn?.disabled).toBe(true);
  await waitFor(() =>
    expect(
      utils.query().wakeBtn?.disabled,
      "a wake whose POST rejected must roll back the optimistic 「喚醒中…」 and re-enable the button",
    ).toBe(false),
  );
  const after = utils.query().wakeBtn;
  return {
    disabled: after?.disabled,
    label: after?.textContent?.trim(),
    wakeRowPresent: utils.query().wakeRow !== null,
    undispatchedNotice: utils.queryByTestId("chat-wake-undispatched") !== null,
    composerUsable:
      utils.query().input !== null && utils.query().locked === null,
  };
}

/** The thread panel has settled onto one of its three terminal renders (the
 *  central offline card, the empty-range notice, or a message list) — i.e. the
 *  loading placeholder is gone. Deliberately does NOT name which one: the
 *  parity assertion is what reads that. */
async function threadSettled(utils: ReturnType<typeof openChat>) {
  await waitFor(() =>
    expect(utils.container.querySelector(".chat__loading")).toBeNull(),
  );
}

beforeEach(() => {
  // Spies here are per-test (some are mockRejectedValue) and nothing else
  // restores them, so a leaked one would silently re-answer the NEXT test.
  vi.restoreAllMocks();
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
    // with the `api.activateMember` line deleted. That is the T-7fa1 shape (a
    // real signal, produced correctly, dropped in the middle), so the wake is
    // measured at the SEAM, spying through to the real mock rather than
    // replacing it.
    const restart = vi.spyOn(api, "activateMember");
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
  // ────────────────────────────────────────────────────────────────────────
  // Review follow-up (Lumi, r1). Three behaviours this pack already HAS but
  // nothing pinned. None of them is new function; each is a judgement that
  // could vanish without a single existing case going red.
  // ────────────────────────────────────────────────────────────────────────

  it.each<MemberLifecycle>(["stopped", "offline"])(
    "%s worker: a typed message really POSTS — to THIS worker, carrying the typed body",
    async (presence) => {
      // 🔴 THE GAP THIS CLOSES: every case above asserts the composer is not
      // LOCKED. "Not locked" is not "sends", and it is certainly not "sends to
      // the right peer" — the chat identity OfficePage hands ChatArea is what
      // `useChat` posts to, and a wrong id there renders a completely normal
      // room while the message lands in someone else's conversation. So the
      // assertion is on the adapter seam's ARGUMENTS, spying THROUGH to the
      // real mock so the send also has to actually succeed.
      const postChat = vi.spyOn(api, "postChat");
      injectWorker(presence);
      const utils = await openWorkerChat();
      const body = `離線也送得出去 ${presence}`;

      fireEvent.change(utils.query().input!, { target: { value: body } });
      const send = utils.query().send!;
      await waitFor(() => expect(send.disabled).toBe(false));
      fireEvent.click(send);

      await waitFor(() => expect(postChat).toHaveBeenCalledTimes(1));
      expect(postChat.mock.calls[0][0]).toMatchObject({ to: WORKER_ID, body });
      // …and it went THROUGH: the real mock adapter stored it and the refetched
      // thread shows it. A spy-only assertion would still pass if the post were
      // swallowed downstream.
      await waitFor(() =>
        expect(utils.container.textContent ?? "").toContain(body),
      );
    },
  );

  it.each([404, 409])(
    "wake rejected with %i: the outsource button rolls back EXACTLY as a 正職's does",
    async (status) => {
      // 🔴 THE FAILURE PATH THE IMPLEMENTER SELF-REPORTED AS UNVERIFIED. The
      // acceptance condition for this pack is "the outsource wake joins the
      // 正職 path", so the assertion is not a hand-written expectation of what
      // failure SHOULD look like — it is the 正職 room's own behaviour under the
      // identical rejection, measured in the same run and compared.
      //
      // 404 = the worker is gone/released, 409 = a conflicting lifecycle. Both
      // reach the UI as one rejected promise and the UI owes the same rollback;
      // running both says that stays true if either ever grows its own arm.
      const reject = () =>
        new ApiError(
          `http ${status} for POST /restart`,
          status,
          "conflict",
          "nope",
        );
      const activate = vi
        .spyOn(api, "activateMember")
        .mockRejectedValue(reject());

      injectWorker("stopped");
      injectStaffPeer("stopped");

      const worker = await openWorkerChat();
      const afterWorker = await wakeAndReadBack(worker);
      expect(activate).toHaveBeenCalledWith(WORKER_ID);
      worker.unmount();

      const staff = await openStaffChat();
      const afterStaff = await wakeAndReadBack(staff);
      expect(activate).toHaveBeenCalledWith(STAFF_ID);
      staff.unmount();

      expect(afterWorker).toEqual(afterStaff);
      // Spelled out too: two rooms agreeing on a permanently stuck 「喚醒中…」
      // would satisfy the comparison above and satisfy nobody else.
      expect(afterWorker).toEqual({
        disabled: false,
        label: zh.chat.wakeButton,
        wakeRowPresent: true,
        undispatchedNotice: false,
        composerUsable: true,
      });
    },
  );

  it.each<[MemberLifecycle, boolean]>([
    ["stopped", true],
    ["stopping", false],
  ])(
    "%s: the central panel projects EXACTLY what a 正職 room in the same state projects",
    async (presence, centralCardExpected) => {
      // The central offline card reads `status` (the collapsed tri-state), not
      // `lifecycle` — a different field from the one every case above asserts,
      // fed by a different line of the projection. The 正職 side comes from the
      // wire mapper, the 外包 side from OfficePage's hand-built peer, so the
      // two are genuinely two computations of the same thing, compared on their
      // rendered output rather than on the props behind it.
      //
      // The card only renders on an EMPTY thread, hence no seeded history here.
      injectWorker(presence, { history: false });
      injectStaffPeer(presence);

      const worker = await openWorkerChat();
      await threadSettled(worker);
      const workerShape = projection(worker.container);
      worker.unmount();

      const staff = await openStaffChat();
      await threadSettled(staff);
      const staffShape = projection(staff.container);
      staff.unmount();

      expect(workerShape).toEqual(staffShape);
      // Anti-vacuity: the two states are NOT the same picture, so "both rooms
      // rendered nothing" can never pass as parity. `stopped` floors to status
      // offline (central card); `stopping` collapses to online (no card, the
      // wake row alone) — the mapper's tri-state, on both sides.
      expect(workerShape.centralCard !== null).toBe(centralCardExpected);
      expect(workerShape.wakeRow).not.toBeNull();
      expect(workerShape.hasInput).toBe(true);
      expect(workerShape.hasLock).toBe(false);
    },
  );
});
