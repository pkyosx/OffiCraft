// Roster row click semantics (owner feedback — the dedicated 聊聊 button is gone).
//
// Locked here:
//   1. WHOLE ROW = CHAT: clicking anywhere on the card (and Enter/Space on the
//      focused row) opens this member's chat — the old button-only entry.
//   2. AVATAR = DETAIL: clicking the avatar opens the member detail panel
//      (the old row-body behaviour) and does NOT also fire the row's chat
//      jump (stopPropagation).
//   3. NO 聊聊 BUTTON: the dedicated chat button no longer renders (Seth
//      2026-07-13, overrides the mockup's re-added button: its flex-end slot
//      now carries only the unread badge — see MemberCard.unread.test.tsx).
//   4. NO ROLE ⚙: the role settings control does NOT render on the row (owner
//      2026-07-17 sent it back twice — off the row, then out of the detail
//      panel too). Its live home is the chat window header
//      (ChatArea.header-actions.test.tsx).

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberCard } from "./MemberCard";
import type { Member } from "../types";

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    memberId: "MB-AST001",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

function renderCard() {
  const onChat = vi.fn();
  const onOpenDetail = vi.fn();
  const utils = render(
    <I18nProvider>
      <MemberCard
        member={mkMember()}
        selected={false}
        currentTaskTitle=""
        onOpenDetail={onOpenDetail}
        onChat={onChat}
      />
    </I18nProvider>,
  );
  return { ...utils, onChat, onOpenDetail };
}

describe("MemberCard click semantics", () => {
  it("clicking the row (e.g. the name) opens the chat, not the detail", () => {
    const { getByText, onChat, onOpenDetail } = renderCard();
    fireEvent.click(getByText("Mira"));
    expect(onChat).toHaveBeenCalledTimes(1);
    expect(onOpenDetail).not.toHaveBeenCalled();
  });

  it("Enter on the focused row opens the chat (keyboard parity)", () => {
    const { container, onChat, onOpenDetail } = renderCard();
    const row = container.querySelector(".member-card")!;
    fireEvent.keyDown(row, { key: "Enter" });
    expect(onChat).toHaveBeenCalledTimes(1);
    expect(onOpenDetail).not.toHaveBeenCalled();
  });

  it("clicking the avatar opens the detail and does NOT bubble into chat", () => {
    const { container, onChat, onOpenDetail } = renderCard();
    const avatarBtn = container.querySelector(".member-card__avatar")!;
    fireEvent.click(avatarBtn);
    expect(onOpenDetail).toHaveBeenCalledTimes(1);
    expect(onChat).not.toHaveBeenCalled();
  });

  it("grows NO role settings control — it lives on the chat header now", () => {
    // Owner 2026-07-17, twice: the ⚙ shipped on this row (2faa5ce), moved to
    // the member detail panel, then moved again onto the chat window header.
    // Its live home is locked in ChatArea.header-actions.test.tsx.
    //
    // 2026-07-17 review (kyle-dfae-review2.md §8): a label-only assertion here
    // is unfalsifiable against a control that comes back under a DIFFERENT
    // label — mutant-proven survived (R5). Restored to STRUCTURAL, matching
    // the pattern MemberDetailPanel.no-role-gear.test.tsx already uses: the
    // row grows exactly ONE button (the avatar), full stop. Any second control
    // — gear, link, icon, whatever it is eventually named — reddens this,
    // because the count moves off 1 rather than because a specific string is
    // absent.
    const { container, queryByLabelText } = renderCard();
    const buttons = container.querySelectorAll("button");
    expect(buttons).toHaveLength(1);
    expect(buttons[0].getAttribute("aria-label")).toBe(zh.office.viewProfile);
    // Same live-label check as before, kept as a second, independent signal —
    // belt and suspenders, not a replacement for the structural check above.
    expect(queryByLabelText(zh.chat.roleSettingsLink)).toBeNull();
  });

  it("renders no dedicated 聊聊 button", () => {
    const { container, queryByTestId } = renderCard();
    expect(queryByTestId("member-chat-btn")).toBeNull();
    expect(container.querySelector(".member-card__chat-btn")).toBeNull();
  });
});
