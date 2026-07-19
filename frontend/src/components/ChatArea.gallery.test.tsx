// M2-3: the chat header's gallery toggle + the member→owner attachment display.
//
// Covers: the header icon opens/closes the gallery panel, the toggle never
// bubbles into the clickable header (open-detail), and the INBOUND direction —
// a member-sent (agent → owner) message renders its image as a thumbnail and
// its file as a chip, with the owner JWT riding the gated blob URL as ?token=
// (authedAttachmentUrl applies to inbound attachments exactly as to outbound).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import { TOKEN_KEY } from "../api/auth";
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

// The gallery panel (mounted on toggle) fetches the flattened member gallery
// through the api client (batch-16: listChatAttachments, not listChat).
vi.mock("../api", () => ({
  api: {
    listChatAttachments: vi.fn(async () => []),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(): Member {
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
  };
}

function renderChat(onOpenDetail?: () => void) {
  return render(
    <I18nProvider>
      <ChatArea member={mkMember()} onOpenDetail={onOpenDetail} />
    </I18nProvider>,
  );
}

describe("ChatArea gallery toggle (M2-3)", () => {
  beforeEach(() => {
    messages = [];
    localStorage.clear();
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("opens and closes the gallery panel from the header icon", async () => {
    const { container } = renderChat();
    // The toggle and the opened dialog share the same accessible label
    // (檔案與圖片) — address the toggle by its class.
    const toggle = container.querySelector(".chat__gallery-toggle")!;
    expect(toggle).toBeTruthy();
    expect(container.querySelector(".chat__gallery")).toBeNull();
    fireEvent.click(toggle);
    expect(await screen.findByRole("dialog")).toBeTruthy();
    fireEvent.click(toggle);
    expect(container.querySelector(".chat__gallery")).toBeNull();
  });

  it("does NOT bubble the toggle click into the clickable header (open detail)", () => {
    const onOpenDetail = vi.fn();
    const { container } = renderChat(onOpenDetail);
    fireEvent.click(container.querySelector(".chat__gallery-toggle")!);
    expect(onOpenDetail).not.toHaveBeenCalled();
  });
});

describe("ChatArea inbound (member→owner) attachment display", () => {
  beforeEach(() => {
    messages = [];
    localStorage.clear();
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("renders a member-sent image as a thumbnail and file as a chip, token-authed", async () => {
    localStorage.setItem(TOKEN_KEY, "tkn");
    messages = [
      {
        id: "c1",
        from: "m1", // the MEMBER sent this to the owner via POST /api/chat
        to: "owner",
        body: "deliverables",
        ts: 100,
        replyCardId: null,
        attachments: [
          {
            id: "a1",
            url: "/api/chat/attachment/a1",
            filename: "shot.png",
            mime: "image/png",
            isImage: true,
          },
          {
            id: "a2",
            url: "/api/chat/attachment/a2",
            filename: "report.pdf",
            mime: "application/pdf",
            isImage: false,
          },
        ],
      },
    ];
    const { container } = renderChat();
    // Incoming bubble (not mine) with the sender's name label.
    await waitFor(() =>
      expect(container.querySelector(".chat__msg:not(.chat__msg--me)")).toBeTruthy(),
    );
    // Image → inline thumbnail whose gated src carries the owner JWT.
    const img = container.querySelector<HTMLImageElement>(".chat__msg-image");
    expect(img).toBeTruthy();
    expect(img!.getAttribute("src")).toBe("/api/chat/attachment/a1?token=tkn");
    // File → a chip linking the gated blob, same ?token= auth.
    const chip = container.querySelector<HTMLAnchorElement>(".chat__msg-file");
    expect(chip).toBeTruthy();
    expect(chip!.getAttribute("href")).toBe("/api/chat/attachment/a2?token=tkn");
    expect(chip!.textContent).toContain("report.pdf");
  });
});
