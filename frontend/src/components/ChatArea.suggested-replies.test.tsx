import { useState } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import { resetChatDrafts } from "../lib/chatDraftStore";

const send = vi.fn(() => Promise.resolve());

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages: [],
    peerLastReadTs: 0,
    send,
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

function member(over: Partial<Member> = {}): Member {
  return {
    id: "rd",
    name: "OffiCraft RD",
    role: "dev",
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
    terminalAttachCommand: "",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

type Props = Partial<React.ComponentProps<typeof ChatArea>>;
function renderChat(props: Props = {}) {
  const utils = render(
    <I18nProvider>
      <ChatArea member={member()} onWake={() => Promise.resolve({} as never)} {...props} />
    </I18nProvider>,
  );
  const input = utils.container.querySelector(".chat__input") as HTMLTextAreaElement;
  return { ...utils, input };
}
/** The room with a parent that keeps what is saved, as OfficePage does. */
function SavingChat({ initial }: { initial: string[] }) {
  const [replies, setReplies] = useState(initial);
  return (
    <I18nProvider>
      <ChatArea
        member={member()}
        onWake={() => Promise.resolve({} as never)}
        suggestedReplies={replies}
        onSaveSuggestedReplies={setReplies}
      />
    </I18nProvider>
  );
}
const SAVE_ERROR = "建議回覆沒有存成功，畫面已換回伺服器上的內容。";
const LOAD_ERROR = "建議回覆載入失敗，再按一次標頭的建議回覆按鈕會重新載入";
const chip = (text: string) => screen.getByRole("button", { name: text });
const openDialog = () => fireEvent.click(screen.getByTestId("chat-header-suggested-replies"));

beforeEach(() => {
  send.mockClear();
  resetChatDrafts();
});

describe("ChatArea 建議回覆", () => {
  it("without sentences the composer has no chip row", () => {
    const { container } = renderChat({ suggestedReplies: [], onSaveSuggestedReplies: vi.fn() });
    expect(screen.queryByTestId("chat-suggested-replies")).toBeNull();
    expect(container.querySelector(".chat__composer-row")).not.toBeNull();
  });

  it("the chips hang under the composer row, in the order given", () => {
    const { container } = renderChat({ suggestedReplies: ["繼續", "先放著"] });
    const row = screen.getByTestId("chat-suggested-replies");
    expect(row.previousElementSibling).toBe(container.querySelector(".chat__composer-row"));
    expect(Array.from(row.querySelectorAll("button")).map((b) => b.textContent)).toEqual(["繼續", "先放著"]);
  });

  it("a click fills the box, focuses it and sends nothing", async () => {
    const { input } = renderChat({ suggestedReplies: ["繼續"] });
    fireEvent.click(chip("繼續"));
    expect(input.value).toBe("繼續");
    expect(document.activeElement).toBe(input);
    await new Promise((r) => setTimeout(r, 50));
    expect(send).not.toHaveBeenCalled();
  });

  it("a click keeps what is typed and puts the sentence on a new line", () => {
    const { input } = renderChat({ suggestedReplies: ["繼續"] });
    fireEvent.change(input, { target: { value: "看過了" } });
    fireEvent.click(chip("繼續"));
    expect(input.value).toBe("看過了\n繼續");
  });

  it("a picked sentence goes out through the ordinary send, exactly as typed text does", async () => {
    const { input, container } = renderChat({ suggestedReplies: ["繼續"] });
    fireEvent.click(chip("繼續"));
    fireEvent.click(container.querySelector(".chat__send") as HTMLButtonElement);
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    expect((send.mock.calls[0] as unknown[])[0]).toBe("繼續");
    expect(input.value).toBe("");
  });

  it("shows the member's own sentences first, then the all-member ones, with identical text once", () => {
    renderChat({ suggestedReplies: ["專屬", "繼續"], allMemberSuggestedReplies: ["繼續", "共用"] });
    const row = screen.getByTestId("chat-suggested-replies");
    expect(Array.from(row.querySelectorAll("button")).map((b) => b.textContent)).toEqual(["專屬", "繼續", "共用"]);
  });

  it("the all-member sentences alone draw the row, and the dialog still edits only the member's own", () => {
    renderChat({ suggestedReplies: [], allMemberSuggestedReplies: ["共用"], onSaveSuggestedReplies: vi.fn() });
    expect(screen.getByTestId("chat-suggested-replies").textContent).toBe("共用");
    openDialog();
    const dialog = screen.getByTestId("chat-suggested-replies-dialog");
    expect(dialog.querySelectorAll("input").length).toBe(0);
    expect(dialog.textContent).toContain("這位成員還沒有專屬的句子。");
  });

  it("offers no editor without a save handler", () => {
    renderChat({ suggestedReplies: ["繼續"] });
    expect(screen.queryByTestId("chat-header-suggested-replies")).toBeNull();
  });

  it("a locked composer offers neither the editor nor the chips", () => {
    renderChat({ member: member({ lifecycle: "offline" }), onWake: undefined, suggestedReplies: ["繼續"], onSaveSuggestedReplies: vi.fn() });
    expect(screen.queryByTestId("chat-header-suggested-replies")).toBeNull();
    expect(screen.queryByTestId("chat-suggested-replies")).toBeNull();
  });

  it("a locked room still tells a failed save", () => {
    renderChat({ member: member({ lifecycle: "stopping" }), suggestedRepliesError: "save", onSaveSuggestedReplies: vi.fn() });
    expect(screen.getByTestId("chat-suggested-replies-error").textContent).toBe(SAVE_ERROR);
  });

  it("after a failed first read the header button stays and reads again instead of opening an empty dialog", () => {
    const onRetry = vi.fn();
    renderChat({ suggestedRepliesError: "load", onRetrySuggestedReplies: onRetry });
    expect(screen.getByRole("alert").textContent).toBe(LOAD_ERROR);
    openDialog();
    expect(onRetry).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("chat-suggested-replies-dialog")).toBeNull();
  });

  it("an outsource room gets the same chips and editor as a staff room", () => {
    renderChat({ member: member({ kind: "outsource" }), suggestedReplies: ["繼續"], onSaveSuggestedReplies: vi.fn() });
    expect(screen.getByTestId("chat-suggested-replies")).toBeTruthy();
    expect(screen.getByTestId("chat-header-suggested-replies")).toBeTruthy();
  });

  describe("the dialog", () => {
    it("names the member and lists the sentences", () => {
      renderChat({ suggestedReplies: ["繼續", "先放著"], onSaveSuggestedReplies: vi.fn() });
      openDialog();
      const dialog = screen.getByTestId("chat-suggested-replies-dialog");
      expect(dialog.querySelector("h2")?.textContent).toBe("建議回覆 · OffiCraft RD");
      expect(Array.from(dialog.querySelectorAll("input")).map((i) => i.value)).toEqual(["繼續", "先放著"]);
      expect(document.activeElement).toBe(dialog.querySelector("input"));
    });

    it("says so when there are no sentences, and adding one saves on blur", () => {
      const onSave = vi.fn();
      renderChat({ suggestedReplies: [], onSaveSuggestedReplies: onSave });
      openDialog();
      const dialog = screen.getByTestId("chat-suggested-replies-dialog");
      expect(dialog.textContent).toContain("這位成員還沒有專屬的句子。");
      fireEvent.click(screen.getByRole("button", { name: "新增一句" }));
      const input = dialog.querySelector("input") as HTMLInputElement;
      fireEvent.change(input, { target: { value: " 繼續 " } });
      fireEvent.blur(input);
      expect(onSave.mock.calls).toEqual([[["繼續"]]]);
    });

    it("moving and deleting save at once", () => {
      const onSave = vi.fn();
      renderChat({ suggestedReplies: ["a", "b"], onSaveSuggestedReplies: onSave });
      openDialog();
      fireEvent.click(screen.getByRole("button", { name: "建議回覆 往下移 1" }));
      fireEvent.click(screen.getByRole("button", { name: "建議回覆 刪除這一句 1" }));
      expect(onSave.mock.calls).toEqual([[["b", "a"]], [["b"]]]);
    });

    it("an over-long sentence is refused with the reason and not saved", () => {
      const onSave = vi.fn();
      renderChat({ suggestedReplies: ["a"], onSaveSuggestedReplies: onSave });
      openDialog();
      const input = screen.getByTestId("chat-suggested-replies-dialog").querySelector("input") as HTMLInputElement;
      fireEvent.change(input, { target: { value: "句".repeat(121) } });
      fireEvent.blur(input);
      expect(onSave).not.toHaveBeenCalled();
      expect(screen.getByRole("alert").textContent).toBe(
        "這一句 121 字,超過上限 120 字。改短一點才會存檔——超過的部分不會被自動截掉,也不會存進去。",
      );
      expect(input.value).toBe("句".repeat(121));
    });

    it("closing with Esc saves the sentence still being typed and returns focus to the header button", () => {
      const onSave = vi.fn();
      renderChat({ suggestedReplies: ["a"], onSaveSuggestedReplies: onSave });
      openDialog();
      const input = screen.getByTestId("chat-suggested-replies-dialog").querySelector("input") as HTMLInputElement;
      input.focus();
      fireEvent.change(input, { target: { value: "ab" } });
      fireEvent.keyDown(window, { key: "Escape" });
      expect(onSave.mock.calls).toEqual([[["ab"]]]);
      expect(screen.queryByTestId("chat-suggested-replies-dialog")).toBeNull();
      expect(document.activeElement).toBe(screen.getByTestId("chat-header-suggested-replies"));
    });

    it("a press on the backdrop saves and closes; a press inside the box does not close", () => {
      const onSave = vi.fn();
      renderChat({ suggestedReplies: ["a"], onSaveSuggestedReplies: onSave });
      openDialog();
      const dialog = screen.getByTestId("chat-suggested-replies-dialog");
      fireEvent.pointerDown(dialog);
      expect(screen.queryByTestId("chat-suggested-replies-dialog")).not.toBeNull();
      const input = dialog.querySelector("input") as HTMLInputElement;
      input.focus();
      fireEvent.change(input, { target: { value: "ab" } });
      fireEvent.pointerDown(dialog.parentElement as HTMLElement);
      expect(onSave.mock.calls).toEqual([[["ab"]]]);
      expect(screen.queryByTestId("chat-suggested-replies-dialog")).toBeNull();
    });

    it("a save failure is told once: in the room, in the dialog while it is open, and in the room again after", () => {
      renderChat({ suggestedReplies: ["a"], suggestedRepliesError: "save", onSaveSuggestedReplies: vi.fn() });
      expect(screen.getAllByRole("alert").map((a) => a.textContent)).toEqual([SAVE_ERROR]);
      expect(screen.getByRole("alert").closest(".chat__composer")).not.toBeNull();

      openDialog();
      const dialog = screen.getByTestId("chat-suggested-replies-dialog");
      expect(screen.getAllByRole("alert").map((a) => a.textContent)).toEqual([SAVE_ERROR]);
      expect(dialog.contains(screen.getByRole("alert"))).toBe(true);

      fireEvent.click(screen.getByRole("button", { name: "關閉" }));
      expect(screen.queryByTestId("chat-suggested-replies-dialog")).toBeNull();
      expect(screen.getByRole("alert").closest(".chat__composer")).not.toBeNull();
    });

    it("the empty list puts focus on the add button", () => {
      renderChat({ suggestedReplies: [], onSaveSuggestedReplies: vi.fn() });
      openDialog();
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "新增一句" }));
    });

    const removeRow = (n: number) => {
      const button = screen.getByRole("button", { name: `建議回覆 刪除這一句 ${n}` });
      button.focus();
      fireEvent.click(button);
    };

    it("deleting the only row hands focus to the add button, and Tab still circles inside", () => {
      render(<SavingChat initial={["a"]} />);
      openDialog();
      const dialog = screen.getByTestId("chat-suggested-replies-dialog");
      removeRow(1);
      expect(dialog.querySelectorAll("input").length).toBe(0);
      const add = screen.getByRole("button", { name: "新增一句" });
      expect(document.activeElement).toBe(add);
      fireEvent.keyDown(add, { key: "Tab" });
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "關閉" }));
    });

    it("deleting the bottom row of several hands focus to the add button", () => {
      render(<SavingChat initial={["a", "b"]} />);
      openDialog();
      removeRow(2);
      expect(Array.from(screen.getByTestId("chat-suggested-replies-dialog").querySelectorAll("input")).map((i) => i.value)).toEqual(["a"]);
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "新增一句" }));
    });

    it("deleting a row with one below it leaves focus on the next row's delete button", () => {
      render(<SavingChat initial={["a", "b"]} />);
      openDialog();
      removeRow(1);
      expect(Array.from(screen.getByTestId("chat-suggested-replies-dialog").querySelectorAll("input")).map((i) => i.value)).toEqual(["b"]);
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "建議回覆 刪除這一句 1" }));
    });

    for (const [how, composing] of [
      ["isComposing", { isComposing: true }],
      ["keyCode 229", { keyCode: 229 }],
    ] as const) {
      it(`an Esc spent on the IME (${how}) keeps the dialog and the text; the next Esc closes and saves`, () => {
        const onSave = vi.fn();
        renderChat({ suggestedReplies: ["a"], onSaveSuggestedReplies: onSave });
        openDialog();
        const input = screen.getByTestId("chat-suggested-replies-dialog").querySelector("input") as HTMLInputElement;
        input.focus();
        fireEvent.change(input, { target: { value: "aㄅ" } });
        fireEvent.keyDown(input, { key: "Escape", ...composing });
        expect(input.value).toBe("aㄅ");
        expect(input.isConnected).toBe(true);
        expect(onSave).not.toHaveBeenCalled();

        fireEvent.keyDown(input, { key: "Escape" });
        expect(onSave.mock.calls).toEqual([[["aㄅ"]]]);
        expect(screen.queryByTestId("chat-suggested-replies-dialog")).toBeNull();
      });
    }

    it("a file dropped on the dialog stays out of the composer, and the browser does not open it", async () => {
      const { container } = renderChat({ suggestedReplies: ["a"], onSaveSuggestedReplies: vi.fn() });
      openDialog();
      const dialog = screen.getByTestId("chat-suggested-replies-dialog");
      const backdrop = dialog.parentElement as HTMLElement;
      const dropped = new File(["x"], "dialog.txt", { type: "text/plain" });
      expect(fireEvent.dragOver(dialog, { dataTransfer: { types: ["Files"] } })).toBe(false);
      expect(fireEvent.drop(dialog, { dataTransfer: { files: [dropped] } })).toBe(false);
      expect(fireEvent.drop(backdrop, { dataTransfer: { files: [dropped] } })).toBe(false);
      await new Promise((r) => setTimeout(r, 50));

      fireEvent.click(screen.getByRole("button", { name: "關閉" }));
      const room = new File(["y"], "room.txt", { type: "text/plain" });
      fireEvent.drop(container.querySelector(".chat") as HTMLElement, { dataTransfer: { files: [room] } });
      await waitFor(() =>
        expect(Array.from(container.querySelectorAll(".chat__preview-file")).map((f) => f.textContent)).toEqual(["room.txt1 B×"]),
      );
    });

    it("dragged text is left for the browser to drop into an input", () => {
      renderChat({ suggestedReplies: ["a"], onSaveSuggestedReplies: vi.fn() });
      openDialog();
      const input = screen.getByTestId("chat-suggested-replies-dialog").querySelector("input") as HTMLInputElement;
      expect(fireEvent.dragOver(input, { dataTransfer: { types: ["text/plain"] } })).toBe(true);
      expect(fireEvent.drop(input, { dataTransfer: { types: ["text/plain"], files: [] } })).toBe(true);
    });

    it("Tab stays inside the dialog and reaches the inputs", () => {
      renderChat({ suggestedReplies: ["a"], onSaveSuggestedReplies: vi.fn() });
      openDialog();
      const dialog = screen.getByTestId("chat-suggested-replies-dialog");
      const input = dialog.querySelector("input") as HTMLInputElement;
      input.focus();
      fireEvent.keyDown(input, { key: "Tab", shiftKey: true });
      expect(dialog.contains(document.activeElement)).toBe(true);
      expect(document.activeElement).not.toBe(input);
      fireEvent.keyDown(document.activeElement as HTMLElement, { key: "Tab" });
      expect(document.activeElement).toBe(input);
    });
  });
});
