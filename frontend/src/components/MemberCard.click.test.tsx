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

  it("with drag wiring, Enter still opens the chat, Space does not, nothing opens while a drag is in progress, and right-click or the menu key opens the row's menu instead", () => {
    const onChat = vi.fn();
    const onContextMenu = vi.fn((e: { preventDefault: () => void }) => e.preventDefault());
    const menuKey = vi.fn((e: { key: string; shiftKey: boolean }) => e.key === "ContextMenu" || (e.shiftKey && e.key === "F10"));
    let dragging = false;
    const { container } = render(
      <I18nProvider>
        <MemberCard
          member={mkMember()}
          selected={false}
          onOpenDetail={() => {}}
          onChat={onChat}
          sortable={{
            rowId: "mira",
            ref: () => {},
            style: {},
            className: "chat-groups__row",
            attributes: {
              role: "button",
              tabIndex: 0,
              "aria-disabled": false,
              "aria-pressed": undefined,
              "aria-roledescription": "可拖曳的對話",
              "aria-describedby": "",
            },
            listeners: undefined,
            canActivate: () => !dragging,
            onContextMenu,
            menuKey,
          }}
        />
      </I18nProvider>,
    );
    const row = container.querySelector(".member-card")!;
    fireEvent.keyDown(row, { key: " " });
    expect(onChat).not.toHaveBeenCalled();
    fireEvent.keyDown(row, { key: "Enter" });
    expect(onChat).toHaveBeenCalledTimes(1);

    fireEvent.contextMenu(row);
    fireEvent.keyDown(row, { key: "ContextMenu" });
    fireEvent.keyDown(row, { key: "F10", shiftKey: true });
    expect(onContextMenu).toHaveBeenCalledTimes(1);
    expect(menuKey.mock.results.map((r) => r.value)).toEqual([false, false, true, true]);
    expect(onChat).toHaveBeenCalledTimes(1);
    expect(row.getAttribute("data-chat-row")).toBe("mira");

    dragging = true;
    fireEvent.keyDown(row, { key: "Enter" });
    fireEvent.click(row);
    expect(onChat).toHaveBeenCalledTimes(1);
  });
});
