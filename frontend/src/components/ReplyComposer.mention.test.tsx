import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider } from "../i18n";
import { ReplyComposer } from "./ReplyComposer";
import { MentionRosterContext, useMentionRoster } from "../hooks/useMentionComposer";
import { pushEscapeLayer } from "../lib/escapeLayers";
import type { Member } from "../types";
import type { OutsourceWorkerView } from "../api/adapter";

function mkMember(id: string, name: string, role: string, roleName: string): Member {
  return {
    id,
    name,
    role,
    roleName,
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
  };
}

const members = [
  mkMember("m-eva", "Family Money Eva", "custom", "財務"),
  mkMember("m1", "Mira", "assistant", "Assistant"),
];
const workers: OutsourceWorkerView[] = [
  {
    id: "ow-7",
    codename: "O-7",
    model: "",
    effort: "",
    status: "active",
    taskId: "t-492",
    taskNo: "T-492",
    taskTitle: "Fix chat mention list",
  },
];

function Roster({ children }: { children: ReactNode }) {
  const roster = useMentionRoster(members, workers, { backTo: "replies" });
  return <MentionRosterContext.Provider value={roster}>{children}</MentionRosterContext.Provider>;
}

const onSend = vi.fn((_body: string, _atts: unknown[]) => Promise.resolve());

function renderComposer({ withRoster = true } = {}) {
  const composer = <ReplyComposer placeholder="輸入回覆…" onSend={onSend} />;
  const utils = render(
    <I18nProvider>{withRoster ? <Roster>{composer}</Roster> : composer}</I18nProvider>,
  );
  const input = utils.container.querySelector(".chat__input") as HTMLTextAreaElement;
  const list = () => utils.container.querySelector('[role="listbox"]');
  const options = () =>
    Array.from(utils.container.querySelectorAll('[role="option"]'), (o) => o.textContent);
  const type = (value: string) => fireEvent.change(input, { target: { value } });
  return { ...utils, input, list, options, type };
}

beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
  onSend.mockReset();
  onSend.mockImplementation(() => Promise.resolve());
});

describe("ReplyComposer @mention", () => {
  it("typing @ → lists staff with the office role label, assistant first, then outsource with task number and title", () => {
    const { options, type } = renderComposer();
    type("交給 @");
    expect(options()).toEqual([
      "Mira特助",
      "Family Money Eva財務",
      "外包 · O-7T-492Fix chat mention list",
    ]);
  });

  it("bare task digits after @ → only the worker on that task", () => {
    const { options, type } = renderComposer();
    type("@492");
    expect(options()).toEqual(["外包 · O-7T-492Fix chat mention list"]);
  });

  it("Enter while the list is open → picks the row and answers nothing", () => {
    const { input, list, type } = renderComposer();
    type("@fam");
    const prevented = !fireEvent.keyDown(input, { key: "Enter" });
    expect(prevented).toBe(true);
    expect(input.value).toBe("@Family Money Eva ");
    expect(list()).toBeNull();
    expect(onSend).not.toHaveBeenCalled();
  });

  it("Esc while open → list closes, text unchanged, the host Esc layer stays open", () => {
    const hostClosed = vi.fn();
    const release = pushEscapeLayer({ onEscape: hostClosed, element: () => null });
    try {
      const { input, list, type } = renderComposer();
      type("@mi");
      const prevented = !fireEvent.keyDown(input, { key: "Escape" });
      expect(prevented).toBe(true);
      expect(list()).toBeNull();
      expect(input.value).toBe("@mi");
      expect(hostClosed).not.toHaveBeenCalled();
    } finally {
      release();
    }
  });

  it("Enter while IME is composing → no pick, list stays", () => {
    const { input, options, type } = renderComposer();
    type("@mir");
    fireEvent.compositionStart(input);
    fireEvent.keyDown(input, { key: "Enter", keyCode: 229 });
    expect(input.value).toBe("@mir");
    expect(options()).toEqual(["Mira特助"]);
  });

  it("answer sent with the list closed → the answer text carries the mentions as links", async () => {
    const { input, type } = renderComposer();
    type("請 @Family Money Eva 和 @外包 · O-7 處理");
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend.mock.calls[0][0]).toBe(
      "請 [@Family Money Eva](/#office/member/m-eva) 和 [@外包 · O-7](/#office/worker/ow-7) 處理",
    );
  });

  it("answer fails → the box keeps the plain @name text, not the links", async () => {
    onSend.mockImplementation(() => Promise.reject(new Error("offline")));
    const { input, type } = renderComposer();
    type("@Mira 決定");
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend.mock.calls[0][0]).toBe("[@Mira](/#office/member/m1) 決定");
    expect(input.value).toBe("@Mira 決定");
  });

  it("no roster around the card → no list, and the answer is sent as typed", async () => {
    const { input, list, type } = renderComposer({ withRoster: false });
    type("@Mira 決定");
    expect(list()).toBeNull();
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend.mock.calls[0][0]).toBe("@Mira 決定");
  });

  it("names that match the roster → marked in the box exactly as they will be linked; other @ text is not", () => {
    const { container, type } = renderComposer();
    type("@Mira 和 @Bob 看 `@Mira` 跟 @Family Money Eva");
    const marks = Array.from(
      container.querySelectorAll(".mention-highlight__mark"),
      (m) => m.textContent,
    );
    expect(marks).toEqual(["@Mira", "@Family Money Eva"]);
    expect(container.querySelector(".mention-highlight")?.textContent).toBe(
      "@Mira 和 @Bob 看 `@Mira` 跟 @Family Money Eva ",
    );
  });

  it("no mention in the box → no highlight layer", () => {
    const { container, type } = renderComposer();
    type("@Bob 你好");
    expect(container.querySelector(".mention-highlight")).toBeNull();
  });

  it("Backspace right after a mention → the whole @name goes and the caret sits where it began", () => {
    const { input, type, container } = renderComposer();
    type("請 @Family Money Eva 看");
    input.setSelectionRange(19, 19);
    const prevented = !fireEvent.keyDown(input, { key: "Backspace" });
    expect(prevented).toBe(true);
    expect(input.value).toBe("請  看");
    expect(input.selectionStart).toBe(2);
    expect(container.querySelector(".mention-highlight")).toBeNull();
  });

  it("Delete right before a mention → the whole @name goes", () => {
    const { input, type } = renderComposer();
    type("請 @Mira 看");
    input.setSelectionRange(2, 2);
    const prevented = !fireEvent.keyDown(input, { key: "Delete" });
    expect(prevented).toBe(true);
    expect(input.value).toBe("請  看");
    expect(input.selectionStart).toBe(2);
  });

  it("Backspace away from a mention, with a modifier, or over a selection → left to the textarea", () => {
    const { input, type } = renderComposer();
    type("@Mira 看");
    input.setSelectionRange(6, 6);
    expect(fireEvent.keyDown(input, { key: "Backspace" })).toBe(true);
    input.setSelectionRange(5, 5);
    expect(fireEvent.keyDown(input, { key: "Backspace", altKey: true })).toBe(true);
    input.setSelectionRange(3, 5);
    expect(fireEvent.keyDown(input, { key: "Backspace" })).toBe(true);
    expect(input.value).toBe("@Mira 看");
  });

  it("Backspace while the IME is composing → left to the IME", () => {
    const { input, type } = renderComposer();
    type("@Mira 看");
    input.setSelectionRange(5, 5);
    fireEvent.compositionStart(input);
    expect(fireEvent.keyDown(input, { key: "Backspace", keyCode: 229 })).toBe(true);
    expect(input.value).toBe("@Mira 看");
  });

  it("Backspace after a typed full name while the list is still open → the whole @name goes", () => {
    const { input, options, type } = renderComposer();
    type("@Mira");
    input.setSelectionRange(5, 5);
    expect(options()).toEqual(["Mira特助"]);
    expect(fireEvent.keyDown(input, { key: "Backspace" })).toBe(false);
    expect(input.value).toBe("");
  });

  it("caret placed inside a mention → moved past its end, or before its @ when it came from the right", () => {
    const { input, type } = renderComposer();
    type("hi @Mira ok");
    input.focus();
    input.setSelectionRange(1, 1);
    fireEvent.select(input);
    input.setSelectionRange(5, 5);
    fireEvent.select(input);
    expect(input.selectionStart).toBe(8);
    input.setSelectionRange(10, 10);
    fireEvent.select(input);
    input.setSelectionRange(6, 6);
    fireEvent.select(input);
    expect(input.selectionStart).toBe(3);
  });
});
