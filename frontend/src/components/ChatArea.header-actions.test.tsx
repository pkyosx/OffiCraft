// T-dfae 聊天 header 的兩顆跳轉圖示 — 任務 / 角色設定 (owner 2026-07-17, 紅框
// 指在 header 最右角). This is the home of the role-settings jump.
//
// Locked here:
//   1. Both buttons render for a roster member, each carrying its label, and
//      each fires its own callback.
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

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, screen, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ChatArea } from "./ChatArea";
import type { Member, MemberActivity } from "../types";
import type { ChatMessage } from "../api/adapter";

let messages: ChatMessage[] = [];

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
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
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    terminalAttachCommand: "tmux -L officraft attach -t member-m1",
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
  headerSub?: React.ReactNode;
};

function renderChat(w: Wiring = {}) {
  return render(
    <I18nProvider>
      <ChatArea
        member={w.member ?? mkMember()}
        onOpenDetail={w.onOpenDetail}
        onOpenTasks={w.onOpenTasks}
        onOpenRoleSettings={w.onOpenRoleSettings}
        headerSub={w.headerSub}
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

  afterEach(() => {
    vi.useRealTimers();
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

  it("under a runtime login warning, the header's presence line carries the exclamation whose hint shows on keyboard focus, and without one it does not", () => {
    const warned = renderChat({
      member: mkMember({
        runtimeLoginWarnings: [
          { machineId: "mac-1", machineName: "mac-1", runtime: "claude", pending: false },
        ],
      }),
    });
    const mark = warned.getByTestId("runtime-login-warning");
    expect(mark.hasAttribute("title")).toBe(false);
    fireEvent.focus(mark);
    expect(Array.from(screen.getByRole("tooltip").children).map((l) => l.textContent)).toEqual([
      "mac-1 未登入 Claude",
      "可到「監控」頁的機器資訊，在 Claude 欄按版本號 →「登入」",
    ]);
    fireEvent.blur(mark);
    expect(mark.closest(".chat__header-sub")).not.toBeNull();
    warned.unmount();

    const clean = renderChat({ member: mkMember({ runtimeLoginWarnings: [] }) });
    expect(clean.queryByTestId("runtime-login-warning")).toBeNull();
  });

  it("under only a model-call warning, the header's presence line carries the exclamation naming that reason", () => {
    const warned = renderChat({
      member: mkMember({
        runtimeLoginWarnings: [],
        modelCallWarnings: [
          {
            runtime: "claude",
            kind: "auth",
            code: "authentication_failed",
            resetsAt: null,
            sinceTs: 1_790_000_000,
            accountWide: false,
          },
        ],
      }),
    });
    const mark = warned.getByTestId("runtime-login-warning");
    fireEvent.focus(mark);
    expect(Array.from(screen.getByRole("tooltip").children).map((l) => l.textContent)).toEqual([
      "Claude 登入失效",
      "可到「監控」頁的機器資訊，在 Claude 欄按版本號 →「登入」",
    ]);
    expect(mark.closest(".chat__header-sub")).not.toBeNull();
  });

  const NOW = 1_800_000_000;
  const busy = (over: Partial<MemberActivity> = {}): MemberActivity => ({
    state: "busy",
    busySince: NOW - 180,
    subagents: [],
    waits: [],
    ...over,
  });
  const hintText = () => screen.getByRole("tooltip").textContent;
  const icon = () => screen.getByTestId("chat-header-activity");
  const glyphState = (el: HTMLElement) =>
    el.querySelector(".activity-icon__glyph")!.className.replace("activity-icon__glyph activity-icon__glyph--", "");

  it("shows the status icon right before the name, named by its state, and hovering it shows the one-sentence summary", () => {
    vi.useFakeTimers({ now: NOW * 1000 });
    renderChat({
      member: mkMember({
        activity: busy({
          subagents: [{ id: "s1", parentId: null, name: "Explore", description: "", startedTs: NOW - 60 }],
          waits: [{ kind: "schedule", description: "看 CI", nextTs: null, recurring: false }],
        }),
      }),
    });
    expect(icon().parentElement!.className).toBe("chat__header-name");
    expect(icon().nextElementSibling!.textContent).toBe("Mira");
    expect(icon().getAttribute("aria-label")).toBe("工作中，查看運行狀況");
    expect(glyphState(icon())).toBe("working");
    fireEvent.mouseEnter(icon());
    expect(hintText()).toBe(
      "主 agent 工作中（已 3 分鐘），另有 1 個 sub-agent 工作中，正在等 1 個背景檢查或排程",
    );
  });

  it("draws the waiting and idle states with their own icon and name", () => {
    const waiting = renderChat({
      member: mkMember({
        activity: busy({
          state: "waiting",
          busySince: null,
          waits: [{ kind: "background_command", description: "", nextTs: null, recurring: false }],
        }),
      }),
    });
    expect(icon().getAttribute("aria-label")).toBe("等待中，查看運行狀況");
    expect(glyphState(icon())).toBe("waiting");
    fireEvent.mouseEnter(icon());
    expect(hintText()).toBe("主 agent 與 sub-agent 都沒有在工作，正在等 1 個背景檢查或排程");
    waiting.unmount();

    renderChat({ member: mkMember({ activity: busy({ state: "idle", busySince: null }) }) });
    expect(icon().getAttribute("aria-label")).toBe("閒置，查看運行狀況");
    expect(glyphState(icon())).toBe("idle");
    fireEvent.mouseEnter(icon());
    expect(hintText()).toBe("主 agent 與 sub-agent 都沒有在工作");
  });

  it("renders a still unknown mark hinting 狀態未知 for an online member without activity, a still moon hinting 已停止 for a stopped one, neither opening dialog nor detail, and the role line carries no presence dot", () => {
    const onOpenDetail = vi.fn();
    const online = renderChat({ member: mkMember({ activity: null }), onOpenDetail });
    expect(screen.queryByTestId("chat-header-activity")).toBeNull();
    expect(screen.queryByTestId("chat-header-activity-offline")).toBeNull();
    const unknown = screen.getByTestId("chat-header-activity-unknown");
    expect(unknown.parentElement!.className).toBe("chat__header-name");
    expect(unknown.nextElementSibling!.textContent).toBe("Mira");
    expect(unknown.tagName).toBe("SPAN");
    expect(unknown.hasAttribute("tabindex")).toBe(false);
    expect(unknown.getAttribute("aria-hidden")).toBe("true");
    expect(unknown.querySelector(".activity-icon__glyph--unknown svg")).not.toBeNull();
    fireEvent.mouseEnter(unknown);
    expect(hintText()).toBe("狀態未知，成員重新啟動後就會顯示");
    fireEvent.click(unknown);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(onOpenDetail).not.toHaveBeenCalled();
    expect(document.querySelector(".chat__header-sub .lifecycle-dot")).toBeNull();
    expect(document.querySelector(".chat__header-sub")!.textContent).toBe("特助");
    expect(screen.getByTestId("chat-header-presence").closest(".presence-avatar")).not.toBeNull();
    online.unmount();

    renderChat({ member: mkMember({ lifecycle: "stopped", activity: null }) });
    const moon = screen.getByTestId("chat-header-activity-offline");
    expect(moon.parentElement!.className).toBe("chat__header-name");
    expect(moon.nextElementSibling!.textContent).toBe("Mira");
    expect(moon.tagName).toBe("SPAN");
    expect(moon.hasAttribute("tabindex")).toBe(false);
    expect(moon.querySelector(".activity-icon__glyph--offline svg")).not.toBeNull();
    fireEvent.mouseEnter(moon);
    expect(hintText()).toBe("已停止");
    fireEvent.click(moon);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("puts the presence dot on the avatar corner, not on the role line, and shows the icon in an outsource chat too", () => {
    renderChat({
      member: mkMember({ activity: busy({ state: "idle", busySince: null }) }),
      headerSub: <span>T-12 · 修理電梯</span>,
    });
    const dot = screen.getByTestId("chat-header-presence");
    expect(dot.closest(".presence-avatar")!.querySelector(".avatar")).not.toBeNull();
    expect(dot.getAttribute("aria-label")).toBe("線上");
    expect(document.querySelector(".chat__header-sub .lifecycle-dot")).toBeNull();
    expect(document.querySelector(".chat__header-sub")!.textContent).toBe("T-12 · 修理電梯");
    expect(icon().getAttribute("aria-label")).toBe("閒置，查看運行狀況");
  });

  it("tapping the status icon opens the 運行狀況 dialog without opening the member detail, and Esc closes it", () => {
    vi.useFakeTimers({ now: NOW * 1000 });
    const onOpenDetail = vi.fn();
    renderChat({
      member: mkMember({
        activity: busy({
          subagents: [{ id: "s1", parentId: null, name: "Explore", description: "查 CI 結果", startedTs: NOW - 45 }],
        }),
      }),
      onOpenDetail,
    });
    const word = icon();
    fireEvent.keyDown(word, { key: "Enter" });
    fireEvent.click(word);
    expect(onOpenDetail).not.toHaveBeenCalled();
    const dialog = screen.getByRole("dialog", { name: "Mira · 運行狀況" });
    expect(dialog.querySelector(".mp-activity__summary")!.textContent).toBe(
      "主 agent 工作中（已 3 分鐘），另有 1 個 sub-agent 工作中",
    );
    expect(Array.from(dialog.querySelectorAll(".mp-activity__row")).map((r) => r.textContent)).toEqual([
      "主 agent已 3 分鐘",
      "查 CI 結果Explore45 秒",
    ]);
    fireEvent.click(screen.getByRole("button", { name: "關閉" }));
    expect(onOpenDetail).not.toHaveBeenCalled();
    fireEvent.click(word);
    act(() => {
      fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(word);
    expect(onOpenDetail).not.toHaveBeenCalled();
  });

  it("moves the elapsed time on while the chat stays open, from seconds into minutes", () => {
    vi.useFakeTimers({ now: NOW * 1000 });
    renderChat({ member: mkMember({ activity: busy({ busySince: NOW - 50 }) }) });
    const word = screen.getByTestId("chat-header-activity");
    fireEvent.mouseEnter(word);
    expect(hintText()).toBe("主 agent 工作中（已 50 秒）");
    act(() => {
      vi.advanceTimersByTime(5_000);
    });
    expect(hintText()).toBe("主 agent 工作中（已 55 秒）");
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(hintText()).toBe("主 agent 工作中（已 1 分鐘）");
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(hintText()).toBe("主 agent 工作中（已 2 分鐘）");
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
    const nameSpan = container.querySelector(".chat__header-name > span:last-child")!;
    expect(nameSpan.textContent).toBe("Eva Rhapsody Inbox");
    // The real geometric proof is the Playwright measurement (header height +
    // gallery top at 390px) — this only pins the DOM shape the CSS rule
    // selects, so a refactor that drops the <span> wrapper reddens here.
    expect(nameSpan.tagName).toBe("SPAN");
  });
});
