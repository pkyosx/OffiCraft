// Chat image rendering — the gated attachment blob is fetched by a bare <img>,
// which cannot send an Authorization header. The src must therefore carry the
// owner JWT as a ?token= query param (mirroring the SSE downlink), else the
// server answers 401 and the image renders broken. Clicking an image opens a
// full-size lightbox that Esc / backdrop dismisses.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

const imageMessage: ChatMessage = {
  id: "msg1",
  from: "owner",
  to: "m1",
  body: "",
  replyCardId: null,
  ts: 1000,
  attachments: [
    {
      id: "abc",
      url: "/api/chat/attachment/abc",
      filename: "",
      mime: "image/png",
      isImage: true,
    },
  ],
};

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages: [imageMessage],
    messagesPeer: "m1",
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

const member: Member = {
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

function renderChat() {
  return render(
    <I18nProvider>
      <ChatArea member={member} />
    </I18nProvider>
  );
}

describe("ChatArea image rendering", () => {
  beforeEach(() => {
    localStorage.clear();
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("appends the owner token to a gated attachment img src", () => {
    localStorage.setItem("oc_token", "jwt-123");
    const { container } = renderChat();
    const img = container.querySelector(
      "img.chat__msg-image"
    ) as HTMLImageElement;
    expect(img).toBeTruthy();
    expect(img.getAttribute("src")).toBe(
      "/api/chat/attachment/abc?token=jwt-123"
    );
  });

  it("leaves the src untokenised when no owner token is stored", () => {
    const { container } = renderChat();
    const img = container.querySelector(
      "img.chat__msg-image"
    ) as HTMLImageElement;
    expect(img.getAttribute("src")).toBe("/api/chat/attachment/abc");
  });

  it("opens a lightbox on image click and closes it on Escape", () => {
    localStorage.setItem("oc_token", "jwt-123");
    const { container } = renderChat();
    const img = container.querySelector(
      "img.chat__msg-image"
    ) as HTMLImageElement;

    expect(container.querySelector(".chat__lightbox")).toBeNull();

    fireEvent.click(img);
    const lightbox = container.querySelector(".chat__lightbox");
    expect(lightbox).toBeTruthy();
    const full = lightbox?.querySelector(
      "img.chat__lightbox-image"
    ) as HTMLImageElement;
    expect(full.getAttribute("src")).toBe(
      "/api/chat/attachment/abc?token=jwt-123"
    );

    fireEvent.keyDown(window, { key: "Escape" });
    expect(container.querySelector(".chat__lightbox")).toBeNull();
  });
});
