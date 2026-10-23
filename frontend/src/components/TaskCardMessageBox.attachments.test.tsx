// Task card message box — attachments (T-c5a6). The 傳訊息給 {executor}… box
// stages files exactly like the chat composer / ReplyComposer (the SHARED
// useAttachmentStaging machine): paste an image into the textarea, or pick
// files via the paperclip button; everything staged rides the SAME
// POST /api/tasks/{id}/message (the API/backend carried attachments all
// along — the card's box just never wired them up).

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { __resetMock, __injectMockTask } from "../api/mock";
import { api } from "../api";
import type { TaskView } from "../api/adapter";

const testPersistence = vi.hoisted(() => {
  const metadata = new Map<string, { text: string; attachmentKeys: string[] }>();
  const rows = new Map<string, Array<Record<string, unknown>>>();
  const refusedAttachmentWrites = new Set<string>();
  const persistence = {
    read(key: string) {
      return metadata.get(key);
    },
    saveText(key: string, text: string) {
      const current = metadata.get(key);
      const next = { text, attachmentKeys: current?.attachmentKeys ?? [] };
      if (!text && next.attachmentKeys.length === 0) metadata.delete(key);
      else metadata.set(key, next);
    },
    saveAttachments(key: string, attachments: Array<Record<string, unknown>>) {
      if (refusedAttachmentWrites.has(key)) return Promise.resolve();
      if (attachments.length) rows.set(key, attachments);
      else rows.delete(key);
      const current = metadata.get(key);
      const next = {
        text: current?.text ?? "",
        attachmentKeys: attachments.map((item) => String(item.key)),
      };
      if (!next.text && next.attachmentKeys.length === 0) metadata.delete(key);
      else metadata.set(key, next);
      return Promise.resolve();
    },
    loadAttachments(key: string) {
      const ids = new Set(metadata.get(key)?.attachmentKeys ?? []);
      return Promise.resolve((rows.get(key) ?? []).filter((item) => ids.has(String(item.key))));
    },
    clear(key: string) {
      metadata.delete(key);
      rows.delete(key);
      return Promise.resolve();
    },
  };
  return {
    persistence,
    reset() {
      metadata.clear();
      rows.clear();
      refusedAttachmentWrites.clear();
    },
    refuseAttachmentWrites(key: string) {
      refusedAttachmentWrites.add(key);
    },
  };
});

vi.mock("../lib/composerDraftPersistence", () => ({
  composerDraftPersistence: testPersistence.persistence,
  chatDraftStorageKey: (peerId: string) => `chat:${peerId}`,
  replyCardDraftStorageKey: (cardId: string) => `reply-card:${cardId}`,
  taskMessageDraftStorageKey: (taskId: string) => `task-message:${taskId}`,
}));

function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "task-att-1",
    taskNo: "T-2001",
    title: "可附檔的",
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
    </I18nProvider>
  );
}

function pngFile(name: string): File {
  // A tiny fake png — staging only cares about type/size.
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
    ".chat__preview-thumb, .chat__preview-file"
  ).length;
}

beforeEach(() => {
  __resetMock();
  testPersistence.reset();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("task card message box — attachments", () => {
  it("pasting an image into the textarea stages it in the preview strip", async () => {
    __injectMockTask(mkTask({}));
    const { container, findByTestId } = renderPage();
    const input = await findByTestId("task-msg-input");
    const file = pngFile("shot.png");
    fireEvent.paste(input, {
      clipboardData: {
        items: [{ type: file.type, getAsFile: () => file }],
      },
    });
    await waitFor(() => expect(previewCount(container)).toBe(1));
    expect(container.querySelectorAll(".chat__preview-thumb").length).toBe(1);
  });

  it("picking files via the paperclip's hidden input stages them (image thumb + file chip)", async () => {
    __injectMockTask(mkTask({}));
    const { container, findByTestId } = renderPage();
    await findByTestId("task-msg-attach"); // the paperclip affordance exists
    const fileInput = container.querySelector(
      ".chat__file-input"
    ) as HTMLInputElement;
    expect(fileInput).toBeTruthy();
    expect(fileInput.multiple).toBe(true);
    fireEvent.change(fileInput, {
      target: { files: [pngFile("a.png"), txtFile("b.txt")] },
    });
    await waitFor(() => expect(previewCount(container)).toBe(2));
    expect(container.querySelectorAll(".chat__preview-thumb").length).toBe(1);
    expect(container.querySelectorAll(".chat__preview-file").length).toBe(1);
  });

  it("send carries ALL staged attachments on the API call, alongside the body", async () => {
    __injectMockTask(mkTask({}));
    const spy = vi.spyOn(api, "postTaskMessage");
    const { container, findByTestId } = renderPage();
    await findByTestId("task-msg-attach");
    const fileInput = container.querySelector(
      ".chat__file-input"
    ) as HTMLInputElement;
    fireEvent.change(fileInput, {
      target: { files: [pngFile("a.png"), txtFile("b.txt")] },
    });
    await waitFor(() => expect(previewCount(container)).toBe(2));

    const input = await findByTestId("task-msg-input");
    fireEvent.change(input, { target: { value: "附上檔案" } });
    fireEvent.click(await findByTestId("task-msg-send"));

    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));
    const [id, msg] = spy.mock.calls[0];
    expect(id).toBe("task-att-1");
    expect(msg.body).toBe("附上檔案");
    expect(msg.attachments).toHaveLength(2);
    expect(Object.keys(msg.attachments![0]).sort()).toEqual(["dataB64", "filename"]);
    expect(msg.attachments![0].filename).toBe("a.png");
    expect(msg.attachments![0].dataB64.startsWith("data:image/png")).toBe(true);
    expect(Object.keys(msg.attachments![1]).sort()).toEqual(["dataB64", "filename"]);
    expect(msg.attachments![1].filename).toBe("b.txt");
  });

  it("a successful send clears the staged attachments AND the draft", async () => {
    __injectMockTask(mkTask({}));
    const { container, findByTestId } = renderPage();
    await findByTestId("task-msg-attach");
    const fileInput = container.querySelector(
      ".chat__file-input"
    ) as HTMLInputElement;
    fireEvent.change(fileInput, { target: { files: [pngFile("a.png")] } });
    await waitFor(() => expect(previewCount(container)).toBe(1));

    const input = (await findByTestId("task-msg-input")) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "收到請看附件" } });
    fireEvent.click(await findByTestId("task-msg-send"));

    await waitFor(() => expect(previewCount(container)).toBe(0));
    expect(input.value).toBe("");
  });

  it("restores a task message after reload and clears the stored draft after sending", async () => {
    __injectMockTask(mkTask({}));
    const beforeReload = renderPage();
    const inputBefore = (await beforeReload.findByTestId("task-msg-input")) as HTMLTextAreaElement;
    fireEvent.change(inputBefore, { target: { value: "重載後要保留" } });
    fireEvent.change(
      beforeReload.container.querySelector(".chat__file-input") as HTMLInputElement,
      { target: { files: [pngFile("task-reload.png")] } },
    );
    await waitFor(() => expect(previewCount(beforeReload.container)).toBe(1));
    beforeReload.unmount();

    const afterReload = renderPage();
    const inputAfter = (await afterReload.findByTestId("task-msg-input")) as HTMLTextAreaElement;
    expect(inputAfter.value).toBe("重載後要保留");
    await waitFor(() => expect(previewCount(afterReload.container)).toBe(1));
    fireEvent.click(await afterReload.findByTestId("task-msg-send"));
    await waitFor(() => expect(inputAfter.value).toBe(""));
    await waitFor(() => expect(previewCount(afterReload.container)).toBe(0));
    afterReload.unmount();

    const afterSend = renderPage();
    const finalInput = (await afterSend.findByTestId("task-msg-input")) as HTMLTextAreaElement;
    expect(finalInput.value).toBe("");
    expect(previewCount(afterSend.container)).toBe(0);
  });

  it("keeps the task message input, file preview, and send usable when draft storage refuses files", async () => {
    const task = mkTask({});
    __injectMockTask(task);
    testPersistence.refuseAttachmentWrites("task-message:task-att-1");
    const spy = vi.spyOn(api, "postTaskMessage");
    const { container, findByTestId } = renderPage();
    const input = (await findByTestId("task-msg-input")) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "仍可送出的任務訊息" } });
    fireEvent.change(
      container.querySelector(".chat__file-input") as HTMLInputElement,
      { target: { files: [pngFile("task.png")] } },
    );
    await waitFor(() => expect(previewCount(container)).toBe(1));
    expect(input.value).toBe("仍可送出的任務訊息");

    fireEvent.click(await findByTestId("task-msg-send"));

    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));
    expect(spy.mock.calls[0][1]).toMatchObject({
      body: "仍可送出的任務訊息",
      attachments: [expect.objectContaining({ filename: "task.png" })],
    });
  });
});
