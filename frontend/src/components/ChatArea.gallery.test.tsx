// M2-3: the chat header's gallery toggle + the member→owner attachment display.
//
// Covers: the header icon opens/closes the gallery panel, the toggle never
// bubbles into the clickable header (open-detail), and the INBOUND direction —
// a member-sent (agent → owner) message renders its image as a thumbnail and
// its file as a chip, with the owner JWT riding the gated blob URL as ?token=
// (authedAttachmentUrl applies to inbound attachments exactly as to outbound).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { api } from "../api";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import { TOKEN_KEY } from "../api/auth";
import type { Member } from "../types";
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

// The gallery panel (mounted on toggle) fetches the flattened member gallery
// through the api client (batch-16: listChatAttachments, not listChat).
vi.mock("../api", () => ({
  api: {
    listChatAttachments: vi.fn(async () => []),
    subscribeEvents: () => () => {},
    // A token is set below, so I18nProvider's dual-layer reconcile (T-0b41-p2)
    // fetches settings on mount — unset prefs keep the local cache.
    getServerSettings: vi.fn(async () => ({ displayTheme: "", displayLanguage: "" })),
    // [T-83ef] Themes left settings for their own resource, so the same
    // reconcile now makes a SECOND call. This stub is hand-written, so a call
    // it does not name is a TypeError rather than an empty answer — which is
    // how this one announced itself. No themes saved: this case is about chat
    // attachments, and the office base is what it was already rendering.
    listThemes: vi.fn(async () => []),
  },
}));

function mkMember(id = "m1", name = "Mira"): Member {
  return {
    id,
    name,
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
    tmuxSession: `member-${id}`,
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

  it("closes on a conversation switch, so no room shows another room's files", async () => {
    // §2.4 used to exempt this panel as an overlay whose backdrop blocks the
    // switch gesture. `.chat__gallery` has no backdrop: it is a 340px side
    // panel inside the chat column, and the roster beside it stays clickable.
    // Driven the way `OfficePage` drives it — `key={peerId}` (R13-5) — so the
    // switch is the unmount it is in the app.
    vi.mocked(api.listChatAttachments).mockResolvedValueOnce([
      {
        id: "att-1",
        url: "/api/chat/attachments/att-1",
        filename: "A-的機密.png",
        mime: "image/png",
        isImage: true,
        messageId: "c-1",
        from: "m1",
        fromName: "Mira",
        to: "owner",
        ts: 1,
      },
    ]);
    const mira = mkMember();
    const view = render(
      <I18nProvider>
        <ChatArea key={mira.id} member={mira} />
      </I18nProvider>,
    );
    fireEvent.click(view.container.querySelector(".chat__gallery-toggle")!);
    expect(await screen.findByText("A-的機密.png")).toBeTruthy();

    const bruno = mkMember("m2", "Bruno");
    view.rerender(
      <I18nProvider>
        <ChatArea key={bruno.id} member={bruno} />
      </I18nProvider>,
    );
    expect(view.container.querySelector(".chat__gallery")).toBeNull();
    expect(screen.queryByText("A-的機密.png")).toBeNull();
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
    // Stored files use the common preview popup; the popup owns download/share.
    const chip = container.querySelector<HTMLButtonElement>("button.chat__msg-file");
    expect(chip).toBeTruthy();
    expect(chip!.getAttribute("type")).toBe("button");
    expect(chip!.textContent).toContain("report.pdf");
  });
});
