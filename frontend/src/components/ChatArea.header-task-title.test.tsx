// T-3451: the chat header's CURRENT task title line (owner 圖2 — the selected
// member's header shows the COMPLETE task title, un-truncated). Locked here:
//   1. headerTaskTitle present → the full title renders in the header as its
//      own line (data-testid chat-header-task-title), un-clamped, with the full
//      text on the `title` tooltip.
//   2. headerTaskTitle absent / "" → NO empty line grows in the header (a
//      released / taskless peer never gets a "no task" line there).
import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

const messages: ChatMessage[] = [];

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

const LONG = "把整包報告重寫成第一原理版本並補齊三語 i18n 與窄版 CT 護欄";

describe("ChatArea header current-task title (T-3451)", () => {
  it("renders the FULL task title in the header when headerTaskTitle is set", () => {
    const { getByTestId } = render(
      <I18nProvider>
        <ChatArea member={mkMember()} headerTaskTitle={LONG} />
      </I18nProvider>,
    );
    const el = getByTestId("chat-header-task-title");
    expect(el.textContent).toBe(LONG);
    // full text on hover, and NOT the 2-line clamp variant (header = un-truncated).
    expect(el.getAttribute("title")).toBe(LONG);
    expect(el.className).not.toContain("current-task-title--clamp");
  });

  it("renders NO task-title line when headerTaskTitle is absent", () => {
    const { queryByTestId } = render(
      <I18nProvider>
        <ChatArea member={mkMember()} />
      </I18nProvider>,
    );
    expect(queryByTestId("chat-header-task-title")).toBeNull();
  });
});
