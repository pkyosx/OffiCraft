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

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";
import { getChatDraft, resetChatDrafts } from "../lib/chatDraftStore";

let messages: ChatMessage[] = [];
const send = vi.fn(() => Promise.resolve());

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    peerLastReadTs: 0,
    send,
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

function mkMember(id: string, name: string): Member {
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

// 🔴 MOUNTED THE WAY `OfficePage` MOUNTS IT (T-48, R13-5): under
// `key={peerId}`, so switching room is an unmount + a mount, exactly as it is in
// the app. Rendering it without a key here would test a component lifetime the
// product does not have — and it is the lifetime every bug in this file came
// from.
function renderChat(member: Member, draftSeed?: string) {
  return render(
    <I18nProvider>
      <ChatArea key={member.id} member={member} draftSeed={draftSeed} />
    </I18nProvider>,
  );
}

/** Walk into another room: same render tree, different `key`. */
function switchTo(
  view: ReturnType<typeof renderChat>,
  member: Member,
): void {
  view.rerender(
    <I18nProvider>
      <ChatArea key={member.id} member={member} />
    </I18nProvider>,
  );
}

function pngFile(name: string): File {
  return new File([new Uint8Array([137, 80, 78, 71])], name, {
    type: "image/png",
  });
}

/** A FileReader whose completion the test decides. The real one lands on a
 * macrotask whose timing is the FILE'S SIZE — seconds for a 100 MB drop — which
 * is exactly the window R9-1 lives in; holding `onload` in hand is how a test
 * gets to stand inside it. */
class HeldFileReader {
  static held: HeldFileReader[] = [];
  onload: (() => void) | null = null;
  result: string | null = null;
  private file: File | null = null;
  readAsDataURL(file: File) {
    this.file = file;
    HeldFileReader.held.push(this);
  }
  /** Land this read now, carrying a data URI the size guard will accept —
   * or, given a byte count over the cap, one it will refuse. */
  land(bytes?: number) {
    const b64 =
      bytes === undefined ? "AAAA" : "A".repeat(Math.ceil((bytes * 4) / 3));
    this.result = `data:${this.file?.type ?? ""};base64,${b64}`;
    this.onload?.();
  }
}

const input = (c: HTMLElement) =>
  c.querySelector(".chat__input") as HTMLTextAreaElement;
const previewCount = (c: HTMLElement) =>
  c.querySelectorAll(".chat__preview-thumb, .chat__preview-file").length;
const sendDisabled = (c: HTMLElement) =>
  (c.querySelector(".chat__send") as HTMLButtonElement).disabled;
const pick = (c: HTMLElement, file: File) =>
  fireEvent.change(c.querySelector(".chat__file-input") as HTMLInputElement, {
    target: { files: [file] },
  });
const attachErrorText = (c: HTMLElement) =>
  c.querySelector(".chat__preview-error")?.textContent ?? null;
const draftNames = (peerId: string) =>
  (getChatDraft(peerId)?.attachments ?? []).map((a) => a.filename);

describe("ChatArea draft survival", () => {
  const m1 = mkMember("m1", "Mira");

  beforeEach(() => {
    messages = [];
    send.mockClear();
    resetChatDrafts();
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

  describe("a file whose read lands after the visit that picked it ended", () => {
    const m2 = mkMember("m2", "Kye");

    beforeEach(() => {
      resetChatDrafts();
      HeldFileReader.held = [];
      vi.stubGlobal("FileReader", HeldFileReader);
    });
    afterEach(() => {
      vi.unstubAllGlobals();
    });

    it("never reaches the other peer's composer, draft or send button", async () => {
      // 🔴 R9-1. The row is written into the slot of the peer it was PICKED for,
      // so there is no list Kye's composer could read it out of — it is not
      // filtered out of his room, it was never in it.
      const view = renderChat(m1);
      pick(view.container, pngFile("a-secret.png"));
      expect(HeldFileReader.held).toHaveLength(1);

      // The owner moves to Kye while the read is still in flight.
      switchTo(view, m2);
      expect(previewCount(view.container)).toBe(0);
      expect(sendDisabled(view.container)).toBe(true);

      act(() => HeldFileReader.held[0].land());

      // The leak first: Kye's room must be untouched by a file nobody staged
      // there — not on screen, not in his draft, and not lighting his send
      // button (one typed line + Enter would have sent it to him).
      expect(previewCount(view.container)).toBe(0);
      expect(draftNames("m2")).toEqual([]);
      expect(sendDisabled(view.container)).toBe(true);
      // And blocking the commit did not destroy the file: it went to the room
      // it was picked for.
      expect(draftNames("m1")).toEqual(["a-secret.png"]);

      // And it is waiting where the owner put it when they come back.
      switchTo(view, m1);
      await waitFor(() => expect(previewCount(view.container)).toBe(1));
    });

    it("reaches the composer that is showing its room again after a 跳頁", async () => {
      // 🔴 R11-2. R10-4 taught the unmount path to file the file in its room's
      // DRAFT, which is right — and enough only while nobody comes back. A
      // returning composer read the draft ONCE, on mount, and by then the read
      // had not landed: the file was in the draft, absent from the screen, and
      // then destroyed by the persist effect writing this composer's own
      // (file-less) list over the top on the very next keystroke.
      //
      // Both halves are structural now (R13-2): the composer SUBSCRIBES to its
      // peer's slice of the store, so a late write repaints it, and the persist
      // effect cannot write the files back because it never holds them.
      const first = renderChat(m1);
      pick(first.container, pngFile("late.png"));
      expect(HeldFileReader.held).toHaveLength(1);

      // 跳頁 and back to the SAME room, all before the read finishes.
      first.unmount();
      const back = renderChat(m1);
      act(() => HeldFileReader.held[0].land());

      await waitFor(() => expect(previewCount(back.container)).toBe(1));
      expect(draftNames("m1")).toEqual(["late.png"]);

      // The keystroke that used to be the file's last moment.
      fireEvent.change(input(back.container), { target: { value: "一" } });
      await waitFor(() => expect(draftNames("m1")).toEqual(["late.png"]));
      expect(previewCount(back.container)).toBe(1);
    });

    it("raises the too-large notice in the room that picked the file, and only once that room is back on screen", async () => {
      // 🔴 R11-4 / R12-1. A rejected read produces a NOTICE where a good one
      // produces a file, and it needs the same journey: invisible in the room
      // the owner walked into, readable in the room it is about. The first fix
      // stamped the notice with its room but left the switch block clearing it
      // during render — which only moved the deletion from the exit to the
      // entrance, so 「圖片太大」 was still never seen.
      const view = renderChat(m1);
      pick(view.container, pngFile("huge.png"));

      switchTo(view, m2);
      act(() => HeldFileReader.held[0].land(21 * 1024 * 1024));

      // Kye picked nothing; a rejection is a sentence about somebody else's
      // action here.
      expect(attachErrorText(view.container)).toBeNull();
      expect(previewCount(view.container)).toBe(0);

      // Nor may Kye's own send destroy it: `clearAttachments` clears THIS
      // room's staged files and THIS room's notice, and Mira's is neither.
      fireEvent.change(input(view.container), { target: { value: "給 Kye" } });
      fireEvent.click(view.container.querySelector(".chat__send") as HTMLElement);
      await waitFor(() => expect(input(view.container).value).toBe(""));

      switchTo(view, m1);
      await waitFor(() =>
        expect(attachErrorText(view.container)).toBe("圖片太大（上限 20 MB）"),
      );
      // Refused, not staged: nothing was written into the room's draft either.
      expect(draftNames("m1")).toEqual([]);
    });

    it("still stages into the composer when the later visit is the same peer", async () => {
      const view = renderChat(m1);
      pick(view.container, pngFile("for-mira.png"));

      // A→B→A: three mounts, and the room on screen IS the file's room.
      switchTo(view, m2);
      switchTo(view, m1);
      act(() => HeldFileReader.held[0].land());

      await waitFor(() => expect(previewCount(view.container)).toBe(1));
      expect(sendDisabled(view.container)).toBe(false);
      expect(draftNames("m2")).toEqual([]);
    });
  });

  it("puts a failed send's words back ON SCREEN, even when the room was left and re-entered mid-flight", async () => {
    // 🔴 R14-1.3. The failure arm writes the words into the room's DRAFT rather
    // than into state, precisely because the owner may have walked out. That is
    // right, and it was only half the journey: the composer read the text once,
    // on mount, so the words sat in the store behind an empty box — and the
    // next keystroke persisted the empty box over them. The whole message went,
    // silently, without the owner ever seeing it come back.
    let failSend: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () =>
        new Promise((_resolve, reject) => {
          failSend = reject;
        }),
    );
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});

    const first = renderChat(m1);
    fireEvent.change(input(first.container), { target: { value: "沒送出去的字" } });
    fireEvent.click(first.container.querySelector(".chat__send") as HTMLElement);
    // Optimistically cleared, and the draft with it.
    await waitFor(() => expect(input(first.container).value).toBe(""));

    // 跳頁 and back while the POST is still in flight: a NEW composer, empty.
    first.unmount();
    const back = renderChat(m1);
    expect(input(back.container).value).toBe("");

    await act(async () => {
      failSend(new Error("boom"));
      await Promise.resolve();
    });

    // The words are visible where they were typed, not just in the store.
    await waitFor(() =>
      expect(input(back.container).value).toBe("沒送出去的字"),
    );
    // And the keystroke that used to be their last moment now edits them.
    fireEvent.change(input(back.container), {
      target: { value: "沒送出去的字!" },
    });
    await waitFor(() => expect(getChatDraft("m1")?.text).toBe("沒送出去的字!"));
    warn.mockRestore();
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
