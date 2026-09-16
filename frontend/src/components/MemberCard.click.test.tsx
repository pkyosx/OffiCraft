// Roster row click semantics.
//
// Locked here:
//   1. WHOLE ROW = CHAT: clicking anywhere on the card (and Enter/Space on the
//      focused row) opens this member's chat — the old button-only entry.
//   2. AVATAR = DETAIL: clicking the avatar opens the member detail panel
//      (the old row-body behaviour) and does NOT also fire the row's chat
//      jump (stopPropagation).

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberCard } from "./MemberCard";
import type { Member } from "../types";

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    terminalAttachCommand: "tmux -L officraft attach -t member-mira",
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
});
