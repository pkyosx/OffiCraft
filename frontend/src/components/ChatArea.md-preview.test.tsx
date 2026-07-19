// T-a1c4: a .md chat attachment gets an in-cockpit 預覽 action (distinct from
// download). The button shows ONLY on markdown attachments; clicking it opens
// the MarkdownPreviewOverlay, which fetches the blob text and renders it. A
// non-markdown attachment (pdf) gets no preview button — only the share button.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
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

function msgWith(attachments: ChatMessage["attachments"]): ChatMessage {
  return {
    id: "msg1",
    from: "m1",
    to: "owner",
    body: "here",
    ts: 1000,
    replyCardId: null,
    attachments,
  };
}

beforeEach(() => {
  localStorage.setItem("oc_token", "jwt-1");
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => vi.restoreAllMocks());

describe("chat .md preview action (T-a1c4)", () => {
  it("shows a 預覽 button on a .md attachment but not on a pdf", () => {
    messages = [
      msgWith([
        { id: "a-md", url: "/api/chat/attachment/a-md", filename: "design.md", mime: "text/markdown", isImage: false },
        { id: "a-pdf", url: "/api/chat/attachment/a-pdf", filename: "report.pdf", mime: "application/pdf", isImage: false },
      ]),
    ];
    const { container } = render(
      <I18nProvider>
        <ChatArea member={mkMember()} />
      </I18nProvider>,
    );
    const previews = container.querySelectorAll('[aria-label="預覽"]');
    expect(previews.length).toBe(1);
  });

  it("opens the preview overlay and renders the markdown on click", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# Design\n\nthe **plan**",
    })) as unknown as typeof fetch;
    messages = [
      msgWith([
        { id: "a-md", url: "/api/chat/attachment/a-md", filename: "design.md", mime: "text/markdown", isImage: false },
      ]),
    ];
    const { container, getByRole } = render(
      <I18nProvider>
        <ChatArea member={mkMember()} />
      </I18nProvider>,
    );
    fireEvent.click(container.querySelector('[aria-label="預覽"]')!);
    await waitFor(() => expect(getByRole("heading", { name: "Design" })).toBeTruthy());
    // Preview and download are separate: the overlay carries its own 下載 link.
    const dl = container.querySelector(".md-preview__download") as HTMLAnchorElement;
    expect(dl.getAttribute("download")).toBe("design.md");
  });
});
