// ChatArea draft survival (T-8aaa) — the座艙 chat composer's draft (typed text
// AND staged image attachments) must outlive a 跳頁 (component unmount) and be
// restored on return, per chat peer. Locked here:
//   • text + attachment restore after an unmount/remount of the SAME peer;
//   • sending clears the draft (a later remount is empty);
//   • manually emptying the composer clears the draft;
//   • the compose seed (T-e987) still only injects into a genuinely-empty
//     restored draft, and never clobbers a restored non-empty draft.
//
// The persistence layer is an in-memory module store (lib/chatDraftStore), so it
// survives an unmount/remount but NOT a full page reload — the owner's scenario
// is跳頁, not reload. These tests drive the unmount/remount path directly.

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

function mkMember(id: string, name: string): Member {
  return {
    id,
    memberId: id,
    name,
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
    tmuxSession: `member-${id}`,
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

function renderChat(member: Member, draftSeed?: string) {
  return render(
    <I18nProvider>
      <ChatArea member={member} draftSeed={draftSeed} />
    </I18nProvider>,
  );
}

function pngFile(name: string): File {
  return new File([new Uint8Array([137, 80, 78, 71])], name, {
    type: "image/png",
  });
}

const input = (c: HTMLElement) =>
  c.querySelector(".chat__input") as HTMLTextAreaElement;
const previewCount = (c: HTMLElement) =>
  c.querySelectorAll(".chat__preview-thumb, .chat__preview-file").length;

describe("ChatArea draft survival", () => {
  const m1 = mkMember("m1", "Mira");

  beforeEach(() => {
    messages = [];
    send.mockClear();
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("restores typed text and a staged image after unmount/remount", async () => {
    const first = renderChat(m1);
    fireEvent.change(input(first.container), { target: { value: "半途打的字" } });
    fireEvent.change(
      first.container.querySelector(".chat__file-input") as HTMLInputElement,
      { target: { files: [pngFile("shot.png")] } },
    );
    await waitFor(() => expect(previewCount(first.container)).toBe(1));

    // 跳頁: the whole ChatArea unmounts.
    first.unmount();

    // 回到聊天: a fresh mount of the SAME peer restores both.
    const back = renderChat(m1);
    expect(input(back.container).value).toBe("半途打的字");
    await waitFor(() => expect(previewCount(back.container)).toBe(1));
  });

  it("clears the draft after the message is sent", async () => {
    const first = renderChat(m1);
    fireEvent.change(input(first.container), { target: { value: "要送出的訊息" } });
    fireEvent.click(first.container.querySelector(".chat__send") as HTMLElement);
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(input(first.container).value).toBe(""));
    first.unmount();

    const back = renderChat(m1);
    expect(input(back.container).value).toBe("");
    expect(previewCount(back.container)).toBe(0);
  });

  it("clears the draft when the composer is manually emptied", () => {
    const first = renderChat(m1);
    fireEvent.change(input(first.container), { target: { value: "先打再刪" } });
    fireEvent.change(input(first.container), { target: { value: "" } });
    first.unmount();

    const back = renderChat(m1);
    expect(input(back.container).value).toBe("");
  });

  it("keeps drafts independent per peer", () => {
    const m2 = mkMember("m2", "Kye");
    const a = renderChat(m1);
    fireEvent.change(input(a.container), { target: { value: "給 Mira 的草稿" } });
    a.unmount();

    // A different peer starts empty and does not see m1's draft.
    const b = renderChat(m2);
    expect(input(b.container).value).toBe("");
    b.unmount();

    const backToM1 = renderChat(m1);
    expect(input(backToM1.container).value).toBe("給 Mira 的草稿");
  });

  it("does NOT let the compose seed clobber a restored non-empty draft", () => {
    const first = renderChat(m1);
    fireEvent.change(input(first.container), { target: { value: "已在打字" } });
    first.unmount();

    // Return routed with a compose seed — the restored draft wins.
    const back = renderChat(m1, "[T-7d40] ");
    expect(input(back.container).value).toBe("已在打字");
  });

  it("lets the compose seed inject into an empty restored draft", () => {
    // No prior draft for this peer → seed injects as usual.
    const back = renderChat(m1, "[T-7d40] ");
    expect(input(back.container).value).toBe("[T-7d40] ");
  });
});
