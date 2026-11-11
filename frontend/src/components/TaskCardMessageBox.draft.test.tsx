import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { __resetMock, __injectMockTask } from "../api/mock";
import { api } from "../api";
import type { TaskView } from "../api/adapter";
import {
  getChatAttachError,
  saveChatDraftText,
  setChatAttachError,
  taskMessageDraftKey,
} from "../lib/chatDraftStore";

function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "task-draft-1",
    taskNo: "T-3001",
    title: "留草稿的",
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

function renderPage() {
  return render(
    <I18nProvider>
      <TasksPage />
    </I18nProvider>,
  );
}

function stagedFileNames(container: HTMLElement): string[] {
  return [...container.querySelectorAll(".chat__preview-file")].map(
    (e) => e.textContent ?? "",
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("TaskCard message box", () => {
  it("restores the typed text and staged file after leaving the task page and coming back", async () => {
    __injectMockTask(mkTask({}));
    const first = renderPage();
    const input = await first.findByTestId("task-msg-input");
    fireEvent.change(input, { target: { value: "還沒寫完" } });
    fireEvent.change(
      first.container.querySelector(".chat__file-input") as HTMLInputElement,
      { target: { files: [new File(["hello"], "notes.txt", { type: "text/plain" })] } },
    );
    await waitFor(() => expect(stagedFileNames(first.container)).toHaveLength(1));
    first.unmount();

    const second = renderPage();
    const restored = (await second.findByTestId("task-msg-input")) as HTMLTextAreaElement;
    expect(restored.value).toBe("還沒寫完");
    expect(stagedFileNames(second.container)).toEqual(["notes.txt5 B×"]);
  });

  it("drops a staging refusal after leaving the task page", async () => {
    __injectMockTask(mkTask({}));
    const page = renderPage();
    await page.findByTestId("task-msg-input");
    setChatAttachError(taskMessageDraftKey("task-draft-1"), "檔案太大");
    page.unmount();
    await Promise.resolve();

    expect(getChatAttachError(taskMessageDraftKey("task-draft-1"))).toBeNull();
  });

  it("keeps each task's draft to itself", async () => {
    __injectMockTask(mkTask({ id: "task-a", taskNo: "T-3002", title: "甲" }));
    __injectMockTask(mkTask({ id: "task-b", taskNo: "T-3003", title: "乙" }));
    const first = renderPage();
    await waitFor(() => expect(first.getAllByTestId("task-msg-input")).toHaveLength(2));
    const card = (title: string) =>
      first.getByText(title).closest("[data-task-id]") as HTMLElement;
    const inputOf = (title: string) =>
      card(title).querySelector("[data-testid='task-msg-input']") as HTMLTextAreaElement;
    fireEvent.change(inputOf("甲"), { target: { value: "只給甲" } });
    first.unmount();

    const second = renderPage();
    await waitFor(() => expect(second.getAllByTestId("task-msg-input")).toHaveLength(2));
    const valueOf = (title: string) =>
      (
        (second.getByText(title).closest("[data-task-id]") as HTMLElement).querySelector(
          "[data-testid='task-msg-input']",
        ) as HTMLTextAreaElement
      ).value;
    expect(valueOf("甲")).toBe("只給甲");
    expect(valueOf("乙")).toBe("");
  });

  it("starts empty after a send that succeeds once the page is already gone", async () => {
    __injectMockTask(mkTask({}));
    let settle: () => void = () => {};
    vi.spyOn(api, "postTaskMessage").mockImplementation(
      () => new Promise<void>((resolve) => (settle = resolve)),
    );
    const first = renderPage();
    fireEvent.change(await first.findByTestId("task-msg-input"), {
      target: { value: "送出途中離開" },
    });
    fireEvent.click(await first.findByTestId("task-msg-send"));
    first.unmount();
    settle();
    await new Promise((resolve) => setTimeout(resolve, 0));
    vi.restoreAllMocks();

    const second = renderPage();
    const input = (await second.findByTestId("task-msg-input")) as HTMLTextAreaElement;
    expect(input.value).toBe("");
  });

  it("shows an empty box after coming back while a send is in flight, and puts the text back there if the send fails", async () => {
    __injectMockTask(mkTask({}));
    let fail: () => void = () => {};
    vi.spyOn(api, "postTaskMessage").mockImplementation(
      () => new Promise<void>((_, reject) => (fail = () => reject(new Error("offline")))),
    );
    const first = renderPage();
    fireEvent.change(await first.findByTestId("task-msg-input"), {
      target: { value: "送出途中切頁" },
    });
    fireEvent.click(await first.findByTestId("task-msg-send"));
    first.unmount();

    const second = renderPage();
    const input = (await second.findByTestId("task-msg-input")) as HTMLTextAreaElement;
    expect(input.value).toBe("");
    expect((second.getByTestId("task-msg-send") as HTMLButtonElement).disabled).toBe(true);

    fail();
    await waitFor(() => expect(input.value).toBe("送出途中切頁"));
  });

  it("does not show a chat room's draft whose peer id equals the task id", async () => {
    __injectMockTask(mkTask({}));
    saveChatDraftText("task-draft-1", "聊天室的草稿");
    const page = renderPage();
    const input = (await page.findByTestId("task-msg-input")) as HTMLTextAreaElement;
    expect(input.value).toBe("");
  });

  it("starts empty after a successful send and keeps the text after a failed one", async () => {
    __injectMockTask(mkTask({}));
    const first = renderPage();
    const input = (await first.findByTestId("task-msg-input")) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "送出去了" } });
    fireEvent.click(await first.findByTestId("task-msg-send"));
    await waitFor(() => expect(input.value).toBe(""));
    first.unmount();

    const second = renderPage();
    const empty = (await second.findByTestId("task-msg-input")) as HTMLTextAreaElement;
    expect(empty.value).toBe("");

    vi.spyOn(api, "postTaskMessage").mockRejectedValue(new Error("offline"));
    fireEvent.change(empty, { target: { value: "送不出去" } });
    fireEvent.click(await second.findByTestId("task-msg-send"));
    await waitFor(() =>
      expect(second.container.querySelector(".task-card__error")).not.toBeNull(),
    );
    second.unmount();

    const third = renderPage();
    const kept = (await third.findByTestId("task-msg-input")) as HTMLTextAreaElement;
    expect(kept.value).toBe("送不出去");
  });
});
