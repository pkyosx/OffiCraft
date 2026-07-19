// T-661b regression: 跳到原訊息 for a card whose sender is an OUTSOURCE worker.
//
// The reply card / worker chat rides the SAME chatId hash slot as a member chat
// (#office/chat/<ow-id>/msg/<msgId>). An outsource worker is NEVER in the 正職
// roster, and once released (task closed) it also drops off the LIVE outsource
// list. When BOTH lookups miss, OfficePage used to fall the chat pane back to
// roster[0] — the first 正職 member (Mira) — so 跳到原訊息 on an outsource
// sender's card silently opened Mira's chat room instead of the origin
// conversation. The fix renders the ORIGINAL conversation read-only under an
// honest "已釋出" identity (chat history is keyed by peer id, so it is still
// reachable), never roster[0]; the same path covers a removed 正職 member.
//
// Rendered through OfficePage so the hash → peer-resolution → ChatArea chain is
// the REAL wiring (the post-jump state the RepliesPage jump button produces).

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import {
  __resetMock,
  __injectMockChat,
  __injectMockOutsourceWorker,
} from "../api/mock";

function renderOffice() {
  return render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>
  );
}

// Force useIsMobile's matchMedia probe to report a phone viewport. jsdom ships
// no matchMedia, so the hook otherwise defaults to desktop (both panes render).
function stubMobileViewport() {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: (query: string) => ({
      matches: /max-width/.test(query), // narrow → phone
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  });
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  // jsdom has no scrollIntoView; ChatArea's entry positioning calls it once a
  // thread renders — stub like the other office/chat suites.
  Element.prototype.scrollIntoView = vi.fn();
});

afterEach(() => {
  // Drop the mobile matchMedia stub so it never leaks into later specs.
  delete (window as unknown as { matchMedia?: unknown }).matchMedia;
});

describe("OfficePage — 跳到原訊息 to an outsource sender", () => {
  it("lands on the origin conversation (read-only), NOT the first 正職 member (Mira), for a released worker", async () => {
    const workerId = "ow-353820f2c636";
    // The origin message the card points at — a worker→owner report. The worker
    // is NOT injected as a live outsource worker (released / task closed), so
    // both the roster and the live-worker lookups miss.
    __injectMockChat({
      id: "m-orig",
      from: workerId,
      to: "owner",
      body: "外包回報:任務初稿完成,請確認。",
      ts: Date.now() / 1000 - 120,
      attachments: [],
      replyCardId: null,
    });

    window.location.hash = `#office/chat/${workerId}/msg/m-orig`;

    const { container, findByText, findByTestId } = renderOffice();

    // POSITIVE: the origin message renders and is the LOCATED (highlighted)
    // jump target — i.e. we reached the origin conversation, not a blank pane.
    const located = await findByText("外包回報:任務初稿完成,請確認。");
    await waitFor(() =>
      expect(located.closest(".chat__msg--located")).not.toBeNull()
    );
    // The header carries the honest 已釋出 subtitle, not a fabricated presence.
    await findByTestId("released-chat-sub");

    // NEGATIVE: never Mira's room.
    const headerName = container.querySelector(".chat__header-name");
    expect(headerName?.textContent ?? "").not.toContain("Mira");
  });

  it("shows the back nav + conversation on a phone (no dead-end)", async () => {
    stubMobileViewport();
    const workerId = "ow-deadbeef0001";
    __injectMockChat({
      id: "m-mob",
      from: workerId,
      to: "owner",
      body: "手機視窗的外包回報。",
      ts: Date.now() / 1000 - 90,
      attachments: [],
      replyCardId: null,
    });

    window.location.hash = `#office/chat/${workerId}/msg/m-mob`;

    const { container, findByText } = renderOffice();

    // The conversation renders (reachable via peer-id history)…
    await findByText("手機視窗的外包回報。");
    // …and the mobile back-to-roster control is present — the pre-fix blank
    // pane had no back button (it lives inside the chat section), a dead end.
    await waitFor(() =>
      expect(container.querySelector(".office__back")).not.toBeNull()
    );
  });

  it("opens the worker's own chat when the worker is still live", async () => {
    const workerId = "ow-live99";
    __injectMockOutsourceWorker({
      id: workerId,
      codename: "O-42",
      model: "Opus 4.6",
      effort: "high",
      taskId: "t-live",
      taskTitle: "整理報告",
      taskStatus: "in_progress",
      createdTs: Date.now() / 1000 - 600,
    });
    __injectMockChat({
      id: "m-live",
      from: workerId,
      to: "owner",
      body: "外包回報。",
      ts: Date.now() / 1000 - 60,
      attachments: [],
      replyCardId: null,
    });

    window.location.hash = `#office/chat/${workerId}/msg/m-live`;

    const { findByTestId, container } = renderOffice();

    // The LIVE worker chat renders its task-line subtitle (the rail row's
    // [T-xxxx chip → type], owner 2026-07-16), not the released placeholder
    // and not a member presence.
    await findByTestId("outsource-chat-sub");
    expect(
      container.querySelector('[data-testid="released-chat-sub"]')
    ).toBeNull();
    const headerName = container.querySelector(".chat__header-name");
    expect(headerName?.textContent ?? "").not.toContain("Mira");
  });

  it("a removed 正職 member's stale chatId renders its own read-only history, not Mira", async () => {
    // Finding #3: a stale chatId no longer self-heals to roster[0]. A non-`ow-`
    // id that resolves to no roster member is treated as a removed member — its
    // history still renders read-only under the neutral 不在名單 identity.
    const goneMemberId = "member-removed-xyz";
    __injectMockChat({
      id: "m-gone",
      from: goneMemberId,
      to: "owner",
      body: "已離開成員的舊訊息。",
      ts: Date.now() / 1000 - 300,
      attachments: [],
      replyCardId: null,
    });

    window.location.hash = `#office/chat/${goneMemberId}/msg/m-gone`;

    const { container, findByText } = renderOffice();

    await findByText("已離開成員的舊訊息。");
    const headerName = container.querySelector(".chat__header-name");
    expect(headerName?.textContent ?? "").not.toContain("Mira");
    // The 已釋出(outsource) subtitle must NOT show for a member id — the neutral
    // 不在名單 copy is used instead (both share the released-chat-sub testid).
    const sub = container.querySelector('[data-testid="released-chat-sub"]');
    expect(sub).not.toBeNull();
    expect(sub?.textContent ?? "").not.toContain("外包");
  });
});
