// T-dfae 聊天 header 的兩顆跳轉圖示 — 任務 / 角色設定 (owner 2026-07-17, 紅框
// 指在 header 最右角). This is the POSITIVE home of the role-settings jump:
// MemberCard.click.test.tsx §4 and OutsourcePanel.test.tsx both assert it is
// NOT on their surface and name THIS file as where it does live. Those two
// negatives are only meaningful while the label they key off is really rendered
// by a real control somewhere — that "somewhere" is here. If this file's
// positives ever go, those negatives quietly become unfalsifiable.
//
// Locked here:
//   1. Both buttons render for a roster member, each carrying the live label
//      the negatives key off, and each fires its own callback.
//   2. Neither click bubbles into the clickable header (open-detail) — the
//      gallery toggle's stopPropagation pattern, which is load-bearing because
//      the whole header is a click target.
//   3. NO prop = NO button, independently per axis: an outsource peer has no
//      role to define and no separable tasks, so the caller wires neither and
//      we must not advertise a jump that would lie.
//   4. The gallery toggle stays UNIQUELY addressable by .chat__gallery-toggle
//      — ChatArea.gallery.test.tsx reaches for it with querySelector, which
//      silently returns the FIRST match, so a new header button wearing that
//      class would hijack an unrelated suite rather than fail honestly.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

let messages: ChatMessage[] = [];

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    messagesPeer: "m1",
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

vi.mock("../api", () => ({
  api: {
    listChatAttachments: vi.fn(async () => []),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "m1",
    memberId: "m1",
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-m1",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

type Wiring = {
  onOpenDetail?: () => void;
  onOpenTasks?: () => void;
  onOpenRoleSettings?: () => void;
  member?: Member;
};

function renderChat(w: Wiring = {}) {
  return render(
    <I18nProvider>
      <ChatArea
        member={w.member ?? mkMember()}
        onOpenDetail={w.onOpenDetail}
        onOpenTasks={w.onOpenTasks}
        onOpenRoleSettings={w.onOpenRoleSettings}
      />
    </I18nProvider>
  );
}

describe("ChatArea header 任務/角色設定 圖示 (T-dfae)", () => {
  beforeEach(() => {
    messages = [];
    localStorage.clear();
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("a roster member's header grows BOTH buttons, each firing its own jump", () => {
    const onOpenTasks = vi.fn();
    const onOpenRoleSettings = vi.fn();
    const { getByLabelText } = renderChat({ onOpenTasks, onOpenRoleSettings });

    // Addressed by the LIVE aria-label — the exact string MemberCard.click and
    // OutsourcePanel assert the absence of. Same key, both directions.
    fireEvent.click(getByLabelText(zh.chat.tasksLink));
    expect(onOpenTasks).toHaveBeenCalledTimes(1);
    expect(onOpenRoleSettings).not.toHaveBeenCalled();

    fireEvent.click(getByLabelText(zh.chat.roleSettingsLink));
    expect(onOpenRoleSettings).toHaveBeenCalledTimes(1);
    expect(onOpenTasks).toHaveBeenCalledTimes(1); // still just the one
  });

  it("neither click bubbles into the clickable header (open detail)", () => {
    const onOpenDetail = vi.fn();
    const { getByLabelText } = renderChat({
      onOpenDetail,
      onOpenTasks: vi.fn(),
      onOpenRoleSettings: vi.fn(),
    });

    fireEvent.click(getByLabelText(zh.chat.tasksLink));
    expect(onOpenDetail).not.toHaveBeenCalled();
    fireEvent.click(getByLabelText(zh.chat.roleSettingsLink));
    expect(onOpenDetail).not.toHaveBeenCalled();

    // Positive control: the header IS live — without it the two negatives above
    // would pass on a header that simply never opens detail at all.
    fireEvent.click(document.querySelector(".chat__header")!);
    expect(onOpenDetail).toHaveBeenCalledTimes(1);
  });

  it("no prop = no button, per axis (the outsource peer's header)", () => {
    // The outsource case as ChatArea sees it: the caller (OfficePage) wires
    // NEITHER jump, because an outsource peer has no role to define and its
    // tasks all collapse to the single "outsource" executor key. Absent props
    // must render nothing rather than a dead control.
    const { queryByLabelText } = renderChat();
    expect(queryByLabelText(zh.chat.tasksLink)).toBeNull();
    expect(queryByLabelText(zh.chat.roleSettingsLink)).toBeNull();
    // Positive control: this render DOES produce a header with its gallery
    // toggle — so the two nulls above mean "these buttons are absent", not
    // "nothing rendered / the label is a typo nothing ever matches".
    expect(document.querySelector(".chat__gallery-toggle")).not.toBeNull();
    expect(queryByLabelText(zh.chat.galleryLabel)).not.toBeNull();
  });

  it("each axis is independent — one prop wires one button, not both", () => {
    // Guards the wiring from collapsing into a single "is this a member?" flag.
    // OfficePage really does pass them separately: a member with an empty role
    // gets 任務 but NOT 角色設定.
    const { queryByLabelText } = renderChat({ onOpenTasks: vi.fn() });
    expect(queryByLabelText(zh.chat.tasksLink)).not.toBeNull();
    expect(queryByLabelText(zh.chat.roleSettingsLink)).toBeNull();
  });

  it("the gallery toggle stays the ONLY .chat__gallery-toggle in the header", () => {
    // ChatArea.gallery.test.tsx does container.querySelector(".chat__gallery-
    // toggle") — first match wins, silently. If a T-dfae button ever wore that
    // class, that suite would drive THIS button while still passing green.
    const { container } = renderChat({
      onOpenTasks: vi.fn(),
      onOpenRoleSettings: vi.fn(),
    });
    expect(container.querySelectorAll(".chat__gallery-toggle")).toHaveLength(1);
    // …and it is the gallery's own, not one of ours.
    expect(
      container
        .querySelector(".chat__gallery-toggle")!
        .getAttribute("aria-label")
    ).toBe(zh.chat.galleryLabel);
    // Positive control on the counting itself: the two new buttons DO exist in
    // this render, under their own class — so the "1" above is a real
    // separation, not an empty header.
    expect(container.querySelectorAll(".chat__header-action")).toHaveLength(2);
  });

  it("a LONG member name never wraps the header into a second line", () => {
    // The 390px squeeze (see office.css .chat__header-name > span). The text
    // column drops to ~142px once the two buttons land; 「Eva Rhapsody Inbox」
    // overflows it. Ellipsis keeps that overflow HORIZONTAL — a wrapped header
    // would grow past .chat__gallery's hardcoded top:64px and tear the panel
    // off. jsdom computes no layout, so this asserts the RULE is applied to the
    // name span, which is the thing layout depends on.
    const { container } = renderChat({
      member: mkMember({ name: "Eva Rhapsody Inbox" }),
      onOpenTasks: vi.fn(),
      onOpenRoleSettings: vi.fn(),
    });
    const nameSpan = container.querySelector(".chat__header-name > span")!;
    expect(nameSpan.textContent).toBe("Eva Rhapsody Inbox");
    // The real geometric proof is the Playwright measurement (header height +
    // gallery top at 390px) — this only pins the DOM shape the CSS rule
    // selects, so a refactor that drops the <span> wrapper reddens here.
    expect(nameSpan.tagName).toBe("SPAN");
  });
});
