// Chat image rendering — the gated attachment blob is fetched by a bare <img>,
// which cannot send an Authorization header. The src must therefore carry the
// owner JWT as a ?token= query param (mirroring the SSE downlink), else the
// server answers 401 and the image renders broken. Clicking an image — stored
// in the thread or still staged in the composer — opens the ONE shared preview
// overlay that Esc / backdrop dismisses.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
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

  it("opens the shared attachment popup on image click and closes it on Escape", () => {
    localStorage.setItem("oc_token", "jwt-123");
    const { container } = renderChat();
    const img = container.querySelector(
      "img.chat__msg-image"
    ) as HTMLImageElement;

    expect(document.body.querySelector(".md-preview")).toBeNull();

    fireEvent.click(img);
    const popup = document.body.querySelector(".md-preview");
    expect(popup).toBeTruthy();
    const full = popup?.querySelector(
      "img.md-preview__image"
    ) as HTMLImageElement;
    expect(full.getAttribute("src")).toBe(
      "/api/chat/attachment/abc?token=jwt-123"
    );

    fireEvent.keyDown(window, { key: "Escape" });
    expect(document.body.querySelector(".md-preview")).toBeNull();
  });

  // T-f014: the staged composer thumbnail was the LAST surface still opening
  // the separate `<Lightbox>` overlay (a bare backdrop + ×). It now opens the
  // same shell every other attachment does — but the bytes are still in the
  // composer, so there is no blob id and no share link may be offered.
  it("opens a staged composer image in the same shell, without a share link", async () => {
    const { container } = renderChat();
    const input = container.querySelector("input.chat__file-input") as HTMLInputElement;
    const file = new File(["png-bytes"], "pasted.png", { type: "image/png" });
    fireEvent.change(input, { target: { files: [file] } });

    const thumb = await waitFor(() => {
      const el = container.querySelector("img.chat__preview-image");
      expect(el).toBeTruthy();
      return el as HTMLImageElement;
    });
    const staged = thumb.getAttribute("src")!;
    expect(staged.startsWith("data:image/png;")).toBe(true);

    fireEvent.click(thumb);
    const popup = document.body.querySelector(".md-preview");
    expect(popup, "the staged thumbnail must open the SHARED overlay").not.toBeNull();
    const full = popup!.querySelector<HTMLImageElement>("img.md-preview__image");
    expect(full, "the shared overlay must render the staged bytes as an image").not.toBeNull();
    expect(full!.getAttribute("src")).toBe(staged);
    expect(popup!.querySelector(".md-preview__title")?.textContent).toContain("pasted.png");
    expect(popup!.querySelector("button.md-preview__share")).toBeNull();
    expect(container.querySelector(".chat__lightbox")).toBeNull();
  });
});
