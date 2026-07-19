// M2-5 multi-attachment chat: one message may carry MULTIPLE files/images.
// Covers the composer staging list (pick several / paste several / drop), the
// count cap notice, the send payload (ALL staged attachments ride the SAME
// message), the per-message multi-attachment rendering, and the M2-4 composer
// lock interaction (a locked composer stages nothing — not even via drop).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

let messages: ChatMessage[] = [];
const send = vi.fn(() => Promise.resolve());

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    messagesPeer: "m1",
    peerLastReadTs: 0,
    send,
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

function mkMember(lifecycle: Member["lifecycle"]): Member {
  return {
    id: "m1",
    memberId: "m1",
    name: "Mira",
    role: "assistant",
    status: lifecycle === "online" ? "online" : "offline",
    lifecycle,
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

function renderChat(member: Member = mkMember("online")) {
  return render(
    <I18nProvider>
      <ChatArea member={member} />
    </I18nProvider>,
  );
}

function pngFile(name: string): File {
  // A tiny fake png — content doesn't matter for staging, only type/size.
  return new File([new Uint8Array([137, 80, 78, 71])], name, {
    type: "image/png",
  });
}

function txtFile(name: string): File {
  return new File(["hello"], name, { type: "text/plain" });
}

/** Count staged preview tiles (image thumbs + file chips). */
function previewCount(container: HTMLElement): number {
  return container.querySelectorAll(
    ".chat__preview-thumb, .chat__preview-file",
  ).length;
}

describe("ChatArea multi-attachment composer", () => {
  beforeEach(() => {
    messages = [];
    send.mockClear();
    localStorage.clear();
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("stages SEVERAL picked files at once (multiple file input)", async () => {
    const { container } = renderChat();
    const input = container.querySelector(
      ".chat__file-input",
    ) as HTMLInputElement;
    expect(input).toBeTruthy();
    expect(input.multiple).toBe(true);

    fireEvent.change(input, {
      target: { files: [pngFile("a.png"), txtFile("b.txt")] },
    });
    await waitFor(() => expect(previewCount(container)).toBe(2));
    // Mixed kinds: one image thumb + one file chip.
    expect(container.querySelectorAll(".chat__preview-thumb").length).toBe(1);
    expect(container.querySelectorAll(".chat__preview-file").length).toBe(1);
  });

  it("stages EVERY pasted image on a multi-image clipboard", async () => {
    const { container } = renderChat();
    const input = container.querySelector(".chat__input") as HTMLTextAreaElement;
    const items = [pngFile("p1.png"), pngFile("p2.png")].map((f) => ({
      type: f.type,
      getAsFile: () => f,
    }));
    fireEvent.paste(input, { clipboardData: { items } });
    await waitFor(() => expect(previewCount(container)).toBe(2));
  });

  it("sends ALL staged attachments on ONE message and clears the stage", async () => {
    const { container } = renderChat();
    const fileInput = container.querySelector(
      ".chat__file-input",
    ) as HTMLInputElement;
    fireEvent.change(fileInput, {
      target: { files: [pngFile("a.png"), txtFile("b.txt")] },
    });
    await waitFor(() => expect(previewCount(container)).toBe(2));

    const draft = container.querySelector(".chat__input") as HTMLTextAreaElement;
    fireEvent.change(draft, { target: { value: "here you go" } });
    fireEvent.click(container.querySelector(".chat__send") as HTMLElement);

    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    const [body, attachments] = send.mock.calls[0] as unknown as [
      string,
      { dataB64: string; filename?: string; mime: string }[],
    ];
    expect(body).toBe("here you go");
    expect(attachments).toHaveLength(2);
    expect(attachments[0].filename).toBe("a.png");
    expect(attachments[0].mime).toBe("image/png");
    expect(attachments[1].filename).toBe("b.txt");
    expect(attachments[1].mime).toBe("text/plain");
    // The stage is cleared after the send.
    await waitFor(() => expect(previewCount(container)).toBe(0));
  });

  it("removes ONE staged attachment without touching its siblings", async () => {
    const { container } = renderChat();
    const fileInput = container.querySelector(
      ".chat__file-input",
    ) as HTMLInputElement;
    fireEvent.change(fileInput, {
      target: { files: [txtFile("keep.txt"), txtFile("drop.txt")] },
    });
    await waitFor(() => expect(previewCount(container)).toBe(2));

    const chips = container.querySelectorAll(".chat__preview-file");
    const dropChip = Array.from(chips).find((c) =>
      c.textContent?.includes("drop.txt"),
    ) as HTMLElement;
    fireEvent.click(
      dropChip.querySelector(".chat__preview-remove--inline") as HTMLElement,
    );
    await waitFor(() => expect(previewCount(container)).toBe(1));
    expect(container.textContent).toContain("keep.txt");
    expect(container.textContent).not.toContain("drop.txt");
  });

  it("caps the staged count at 10 and surfaces a notice for the overflow", async () => {
    const { container } = renderChat();
    const fileInput = container.querySelector(
      ".chat__file-input",
    ) as HTMLInputElement;
    const files = Array.from({ length: 11 }, (_, i) => txtFile(`f${i}.txt`));
    fireEvent.change(fileInput, { target: { files } });
    await waitFor(() => expect(previewCount(container)).toBe(10));
    // The 11th is refused with a visible too-many notice.
    const err = container.querySelector(".chat__preview-error");
    expect(err).toBeTruthy();
    expect(err?.textContent).toContain("10");
  });

  it("stages dropped files (drag-drop onto the chat window)", async () => {
    const { container } = renderChat();
    const chat = container.querySelector(".chat") as HTMLElement;
    fireEvent.drop(chat, {
      dataTransfer: { files: [pngFile("d.png"), txtFile("d.txt")] },
    });
    await waitFor(() => expect(previewCount(container)).toBe(2));
  });
});

describe("ChatArea multi-attachment vs composer lock (M2-4)", () => {
  beforeEach(() => {
    messages = [];
    send.mockClear();
    localStorage.clear();
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("locked composer offers NO attach affordances at all", () => {
    // T-94c1: offline/stopped are now UNLOCKED (typable + queue); only
    // waking/stopping stay locked, so this "locked" test uses `stopping`.
    const { container } = renderChat(mkMember("stopping"));
    expect(container.querySelector(".chat__composer-locked")).toBeTruthy();
    expect(container.querySelector(".chat__attach")).toBeNull();
    expect(container.querySelector(".chat__file-input")).toBeNull();
    expect(container.querySelector(".chat__input")).toBeNull();
  });

  it("a drop while locked stages NOTHING (still empty once unlocked)", async () => {
    // T-94c1: `stopping` is the locked state now (offline/stopped unlocked).
    const { container, rerender } = renderChat(mkMember("stopping"));
    const chat = container.querySelector(".chat") as HTMLElement;
    fireEvent.drop(chat, {
      dataTransfer: { files: [pngFile("sneak.png")] },
    });

    // Unlock (member back online) — the same component instance keeps its
    // state, so anything staged during the lock would surface now. It must not.
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember("online")} />
      </I18nProvider>,
    );
    // Give any (wrongly) started FileReader a beat to land, then assert empty.
    await new Promise((r) => setTimeout(r, 30));
    expect(previewCount(container)).toBe(0);
  });
});

describe("ChatArea multi-attachment message rendering", () => {
  beforeEach(() => {
    messages = [];
    send.mockClear();
    localStorage.clear();
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("renders EVERY attachment of one message (images + file chips)", () => {
    localStorage.setItem("oc_token", "jwt-1");
    messages = [
      {
        id: "msg1",
        from: "owner",
        to: "m1",
        body: "bundle",
        ts: 1000,
        replyCardId: null,
        attachments: [
          {
            id: "a1",
            url: "/api/chat/attachment/a1",
            filename: "",
            mime: "image/png",
            isImage: true,
          },
          {
            id: "a2",
            url: "/api/chat/attachment/a2",
            filename: "notes.pdf",
            mime: "application/pdf",
            isImage: false,
          },
          {
            id: "a3",
            url: "/api/chat/attachment/a3",
            filename: "",
            mime: "image/jpeg",
            isImage: true,
          },
        ],
      },
    ];
    const { container } = renderChat();
    const imgs = container.querySelectorAll("img.chat__msg-image");
    expect(imgs.length).toBe(2);
    // Each image src is individually token-authed (gated blob per attachment).
    expect(imgs[0].getAttribute("src")).toBe(
      "/api/chat/attachment/a1?token=jwt-1",
    );
    expect(imgs[1].getAttribute("src")).toBe(
      "/api/chat/attachment/a3?token=jwt-1",
    );
    const chip = container.querySelector(".chat__msg-file") as HTMLAnchorElement;
    expect(chip).toBeTruthy();
    expect(chip.getAttribute("href")).toBe(
      "/api/chat/attachment/a2?token=jwt-1",
    );
    expect(chip.textContent).toContain("notes.pdf");
    // The text body still renders alongside the whole bundle.
    expect(container.textContent).toContain("bundle");
  });
});
