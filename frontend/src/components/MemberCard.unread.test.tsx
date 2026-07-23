// M2-1 roster unread COUNT badge (member → owner direction; the red dot,
// upgraded to a count).
//
// Locked here:
//   1. PRESENCE-INDEPENDENT: an OFFLINE member with unreadCount > 0 shows the
//      badge — unread and presence are separate signals that never merge (AC #4).
//   2. COUNT, NOT A DOT: the badge renders the actual number of waiting
//      messages; > 99 clamps to "99+".
//   3. SUPPRESSED IN THE OPEN CONVERSATION: while THIS member's chat is open
//      (`selected`), the badge never shows — in the open thread a new message is
//      read immediately (listChat auto-mark), so it must not accumulate (AC #3,
//      the UI-side guarantee).
//   4. count 0 → NOT RENDERED at all (no empty pill, no leftover dot).

import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberCard } from "./MemberCard";
import type { Member } from "../types";

function mkMember(over: Partial<Member>): Member {
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

function renderCard(member: Member, selected = false) {
  return render(
    <I18nProvider>
      <MemberCard
        member={member}
        selected={selected}
        currentTaskTitle=""
        onOpenDetail={vi.fn()}
        onChat={vi.fn()}
      />
    </I18nProvider>,
  );
}

describe("MemberCard unread count badge", () => {
  it("an OFFLINE member with unread shows the COUNT (presence-independent)", () => {
    const member = mkMember({
      unreadCount: 3,
      status: "offline",
      lifecycle: "offline",
    });
    const { getByTestId } = renderCard(member);
    const badge = getByTestId("unread-badge");
    expect(badge).toBeTruthy();
    // A COUNT badge, not a bare dot: the number is the badge's text.
    expect(badge.textContent).toBe("3");
  });

  it("clamps a count over 99 to 99+", () => {
    const { getByTestId } = renderCard(mkMember({ unreadCount: 120 }));
    expect(getByTestId("unread-badge").textContent).toBe("99+");
  });

  it("renders nothing at all when the count is 0", () => {
    const { queryByTestId } = renderCard(mkMember({ unreadCount: 0 }));
    expect(queryByTestId("unread-badge")).toBeNull();
  });

  it("suppresses the badge while this member's chat is open (selected) and WATCHED", () => {
    // In the open, actually-watched conversation a new message is read
    // immediately — the card must not flash a badge even if a stale refetch
    // still carries a count. "Watched" = the window is active (jsdom's
    // hasFocus() is false by default, so pin it true explicitly).
    const spy = vi.spyOn(document, "hasFocus").mockReturnValue(true);
    try {
      const { queryByTestId } = renderCard(mkMember({ unreadCount: 2 }), true);
      expect(queryByTestId("unread-badge")).toBeNull();
    } finally {
      spy.mockRestore();
    }
  });

  it("a SELECTED card still shows the badge while the window is BACKGROUNDED", () => {
    // Badge-flash fix: suppression assumes the open conversation is being
    // WATCHED (its auto-mark consumes new messages instantly). With the window
    // backgrounded the open thread stops consuming reads (useChat peeks
    // read-only), unread genuinely accumulates — the selected card must show
    // it, or the owner returns to a red dot that silently died.
    const spy = vi.spyOn(document, "hasFocus").mockReturnValue(false);
    try {
      const { getByTestId } = renderCard(mkMember({ unreadCount: 4 }), true);
      expect(getByTestId("unread-badge").textContent).toBe("4");
    } finally {
      spy.mockRestore();
    }
  });

  it("an UNSELECTED card shows the badge regardless of window focus", () => {
    const spy = vi.spyOn(document, "hasFocus").mockReturnValue(false);
    try {
      const { getByTestId } = renderCard(mkMember({ unreadCount: 1 }), false);
      expect(getByTestId("unread-badge").textContent).toBe("1");
    } finally {
      spy.mockRestore();
    }
  });
});
