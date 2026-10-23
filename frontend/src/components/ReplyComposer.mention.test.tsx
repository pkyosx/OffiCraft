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
});
