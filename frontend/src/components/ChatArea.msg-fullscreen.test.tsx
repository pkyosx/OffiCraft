// 放大閱讀 — the corner button on an INCOMING chat bubble that reopens that
// message's body in the shared markdown full-view overlay (the same surface a
// .md attachment opens into). Owner ask 2026-07-28: a long agent answer is a
// document, and the thread column is not where you read one.
//
// What this file pins (jsdom sees structure, not layout):
//   · WHICH messages carry the button — incoming with text yes; own message no;
//     incoming attachment-only no (the file chip owns its own 預覽 action).
//   · that the click opens the overlay on the MESSAGE BODY, rendered as
//     markdown, with NO download link (the text was never a file).
// What it cannot pin: that the reserved corner (.chat__msg-bubble--expandable
// padding) actually keeps the glyph off the last characters — that is CSS, and
// was verified in a real browser instead.

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

function msg(over: Partial<ChatMessage>): ChatMessage {
  return {
    id: "msg1",
    from: "m1",
    to: "owner",
    body: "hello",
    ts: 1000,
    replyCardId: null,
    attachments: [],
    ...over,
  };
}

function renderChat() {
  return render(
    <I18nProvider>
      <ChatArea member={mkMember()} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  localStorage.setItem("oc_token", "jwt-1");
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => vi.restoreAllMocks());

describe("chat message 放大閱讀 button", () => {
  it("puts the button on an incoming message that has text", () => {
    messages = [msg({ body: "# Plan\n\nthe long answer" })];
    const { container } = renderChat();
    expect(container.querySelectorAll("button.chat__msg-expand").length).toBe(1);
  });

  it("leaves the owner's OWN message without one", () => {
    messages = [msg({ id: "own", from: "owner", to: "m1", body: "my line" })];
    const { container } = renderChat();
    expect(container.querySelector("button.chat__msg-expand")).toBeNull();
  });

  it("leaves an attachment-only incoming message without one", () => {
    messages = [
      msg({
        body: "",
        attachments: [
          {
            id: "a1",
            url: "/api/chat/attachment/a1",
            filename: "design.md",
            mime: "text/markdown",
            isImage: false,
          },
        ],
      }),
    ];
    const { container } = renderChat();
    expect(container.querySelector("button.chat__msg-expand")).toBeNull();
  });

  it("opens the overlay on the message body — markdown rendered, no download link", async () => {
    // A fetch here would mean the overlay went looking for a blob that does not
    // exist: the body text is already in hand.
    globalThis.fetch = vi.fn(() =>
      Promise.reject(new Error("must not fetch")),
    ) as unknown as typeof fetch;
    messages = [msg({ body: "# Design\n\nthe **plan**" })];
    const { container } = renderChat();

    fireEvent.click(container.querySelector("button.chat__msg-expand")!);

    // Scoped to the overlay on purpose: the bubble renders the SAME markdown,
    // so an unscoped heading query matches the thread copy too.
    await waitFor(() =>
      expect(
        document.body.querySelector(".md-preview__md h1")?.textContent,
      ).toBe("Design"),
    );
    expect(globalThis.fetch).not.toHaveBeenCalled();
    // Nothing to download — the message body was never a stored file.
    expect(document.body.querySelector(".md-preview__download")).toBeNull();
    // The header names the sender, so a full-view read still says who wrote it.
    expect(document.body.querySelector(".md-preview__title")?.textContent).toContain(
      "Mira",
    );
  });

  // End-to-end version of the overlay's `breaks` guard: the bubble and its
  // full-view read must lay the SAME message out the same way. Opening a plain
  // multi-line message used to reflow it into one run-on line.
  it("reads a multi-line message with its newlines intact, like the bubble", async () => {
    globalThis.fetch = vi.fn(() =>
      Promise.reject(new Error("must not fetch")),
    ) as unknown as typeof fetch;
    messages = [msg({ body: "第一行\n第二行\n第三行" })];
    const { container } = renderChat();

    const bubble = container.querySelector(".chat__msg-text")!;
    fireEvent.click(container.querySelector("button.chat__msg-expand")!);
    await waitFor(() =>
      expect(document.body.querySelector(".md-preview__md")).toBeTruthy(),
    );
    const reader = document.body.querySelector(".md-preview__md")!;
    // Same shape on both surfaces — two hard breaks, one paragraph.
    expect(bubble.querySelectorAll("br").length).toBe(2);
    expect(reader.querySelectorAll("br").length).toBe(2);
    expect(reader.textContent).not.toContain("第一行 第二行");
  });
});
