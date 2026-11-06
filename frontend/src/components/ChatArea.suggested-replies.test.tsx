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
      expect(screen.getByRole("alert").textContent).toContain("121");
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

    it("the close button closes, and a save failure is shown", () => {
      renderChat({ suggestedReplies: ["a"], suggestedRepliesSaveFailed: true, onSaveSuggestedReplies: vi.fn() });
      openDialog();
      expect(screen.getByRole("alert").textContent).toBe("沒有存成功，畫面已換回伺服器上的內容。");
      fireEvent.click(screen.getByRole("button", { name: "關閉" }));
      expect(screen.queryByTestId("chat-suggested-replies-dialog")).toBeNull();
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
