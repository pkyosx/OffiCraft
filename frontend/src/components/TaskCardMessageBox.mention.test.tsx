import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import {
  __resetMock,
  __injectMockTask,
  __injectMockMember,
  __injectMockOutsourceWorker,
} from "../api/mock";
import { api } from "../api";
import { pushEscapeLayer } from "../lib/escapeLayers";
import type { TaskView } from "../api/adapter";

function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "task-msg-1",
    taskNo: "T-1001",
    title: "可傳話的",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "staff",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: Date.now() / 1000 - 3600,
    updatedTs: Date.now() / 1000 - 60,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
}

async function renderBox() {
  __injectMockTask(mkTask({}));
  const utils = render(
    <I18nProvider>
      <TasksPage />
    </I18nProvider>,
  );
  const input = (await utils.findByTestId("task-msg-input")) as HTMLTextAreaElement;
  const options = () =>
    Array.from(utils.container.querySelectorAll('[role="option"]'), (o) => o.textContent);
  const list = () => utils.container.querySelector('[role="listbox"]');
  const type = (value: string) => fireEvent.change(input, { target: { value } });
  // The roster lands after the task list; the box is usable before it.
  await waitFor(() => {
    type("@");
    expect(options()).toHaveLength(3);
  });
  type("");
  return { ...utils, input, options, list, type };
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
  __injectMockMember({
    id: "m-eva",
    kind: "staff",
    name: "Family Money Eva",
    role_key: "finance",
    role_name: "財務",
  });
  // Closed, so it stays off the default list and only names the worker's task.
  __injectMockTask(
    mkTask({ id: "t-492", taskNo: "T-492", status: "done", closedTs: Date.now() / 1000 }),
  );
  __injectMockOutsourceWorker({
    id: "ow-7",
    codename: "O-7",
    model: "",
    effort: "",
    status: "active",
    taskId: "t-492",
    taskNo: "T-492",
    taskTitle: "Fix chat mention list",
  });
});
afterEach(() => {
  vi.restoreAllMocks();
});

describe("task card message box @mention", () => {
  it("typing @ → lists staff with the office role label, assistant first, then outsource with task number and title", async () => {
    const { options, type } = await renderBox();
    type("問 @");
    expect(options()).toEqual([
      "Mira特助",
      "Family Money Eva財務",
      "外包 · O-7T-492Fix chat mention list",
    ]);
  });

  it("bare task digits after @ → only the worker on that task", async () => {
    const { options, type } = await renderBox();
    type("@492");
    expect(options()).toEqual(["外包 · O-7T-492Fix chat mention list"]);
  });

  it("Enter while the list is open → picks the row and sends nothing", async () => {
    const post = vi.spyOn(api, "postTaskMessage");
    const { input, list, type } = await renderBox();
    type("@fam");
    const prevented = !fireEvent.keyDown(input, { key: "Enter" });
    expect(prevented).toBe(true);
    expect(input.value).toBe("@Family Money Eva ");
    expect(list()).toBeNull();
    expect(post).not.toHaveBeenCalled();
  });

  it("clicking a row → picks it without folding the task card open or shut", async () => {
    const { container, input, type } = await renderBox();
    const card = container.querySelector(".task-card")!;
    const expandedBefore = card.getAttribute("aria-expanded");
    type("@");
    fireEvent.click(container.querySelectorAll('[role="option"]')[2]);
    expect(input.value).toBe("@外包 · O-7 ");
    expect(card.getAttribute("aria-expanded")).toBe(expandedBefore);
  });

  it("Esc while open → list closes, text unchanged, the host Esc layer stays open", async () => {
    const hostClosed = vi.fn();
    const release = pushEscapeLayer({ onEscape: hostClosed, element: () => null });
    try {
      const { input, list, type } = await renderBox();
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

  it("Enter while IME is composing → no pick, list stays", async () => {
    const { input, options, type } = await renderBox();
    type("@mir");
    fireEvent.compositionStart(input);
    fireEvent.keyDown(input, { key: "Enter", keyCode: 229 });
    expect(input.value).toBe("@mir");
    expect(options()).toEqual(["Mira特助"]);
  });

  it("send with the list closed → the message body carries the mentions as links", async () => {
    const post = vi.spyOn(api, "postTaskMessage");
    const { input, type } = await renderBox();
    type("@Family Money Eva 跟 @外包 · O-7 對一下");
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(post).toHaveBeenCalledTimes(1);
    expect(post.mock.calls[0][1].body).toBe(
      "[@Family Money Eva](/#office/member/m-eva) 跟 [@外包 · O-7](/#office/worker/ow-7) 對一下",
    );
  });

  it("send fails → the box keeps the plain @name text, not the links", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const post = vi
      .spyOn(api, "postTaskMessage")
      .mockRejectedValue(new Error("offline"));
    const { input, type } = await renderBox();
    type("@Mira 在嗎");
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(post.mock.calls[0][1].body).toBe("[@Mira](/#office/member/mira) 在嗎");
    await waitFor(() => expect(input.disabled).toBe(false));
    expect(input.value).toBe("@Mira 在嗎");
  });
});
