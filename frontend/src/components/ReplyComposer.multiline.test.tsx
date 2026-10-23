// Multi-line composer — ReplyComposer (the reply card's typed-reply input,
// shared by RepliesPage and the in-chat ChatReplyCard).
//
// Bug report (2026-07-13): the reply input was a single-line <input> — no line
// breaks (Shift+Enter dead) and a long reply mostly invisible. Same fix as the
// chat composer: a <textarea> where Enter submits and Shift+Enter breaks a
// line. These tests pin that contract on the SHARED component so both reply
// surfaces (等我回覆 page + in-chat card) are covered at once.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, act, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ReplyComposer } from "./ReplyComposer";

const testPersistence = vi.hoisted(() => {
  const metadata = new Map<string, {
    text: string;
    attachmentKeys: string[];
  }>();
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

const onSend = vi.fn(() => Promise.resolve());

function renderComposer(draftKey?: string) {
  const utils = render(
    <I18nProvider>
      <ReplyComposer
        placeholder="輸入回覆…"
        draftKey={draftKey}
        onSend={onSend}
      />
    </I18nProvider>,
  );
  const input = utils.container.querySelector(
    ".chat__input",
  ) as HTMLTextAreaElement;
  return { ...utils, input };
}

function pngFile(name: string): File {
  return new File([new Uint8Array([137, 80, 78, 71])], name, {
    type: "image/png",
  });
}

describe("ReplyComposer multi-line input", () => {
  beforeEach(() => {
    onSend.mockClear();
    testPersistence.reset();
  });

  it("the reply input is a TEXTAREA (multi-line capable), not a single-line input", () => {
    const { input } = renderComposer();
    expect(input).toBeTruthy();
    expect(input.tagName).toBe("TEXTAREA");
  });

  it("bare Enter submits the typed reply", async () => {
    const { input } = renderComposer();
    fireEvent.change(input, { target: { value: "my answer" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend).toHaveBeenCalledWith("my answer", []);
  });

  it("Shift+Enter does NOT submit and is NOT prevented (native newline goes through)", () => {
    const { input } = renderComposer();
    fireEvent.change(input, { target: { value: "line one" } });
    const notPrevented = fireEvent.keyDown(input, {
      key: "Enter",
      shiftKey: true,
    });
    expect(notPrevented).toBe(true);
    expect(onSend).not.toHaveBeenCalled();
  });

  it("a multi-line reply is sent verbatim — line breaks preserved", async () => {
    const { input } = renderComposer();
    fireEvent.change(input, { target: { value: "第一行\n第二行" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend).toHaveBeenCalledWith("第一行\n第二行", []);
  });

  it("restores the card's text and files after a full page reload, then clears them after sending", async () => {
    const key = "reply-card:c-reload";
    const beforeReload = renderComposer(key);
    fireEvent.change(beforeReload.input, { target: { value: "保留的回覆" } });
    fireEvent.change(
      beforeReload.container.querySelector(".chat__file-input") as HTMLInputElement,
      { target: { files: [pngFile("reply.png")] } },
    );
    await waitFor(() =>
      expect(beforeReload.container.querySelectorAll(".chat__preview-thumb")).toHaveLength(1),
    );

    beforeReload.unmount();
    const afterReload = renderComposer(key);
    expect(afterReload.input.value).toBe("保留的回覆");
    await waitFor(() =>
      expect(afterReload.container.querySelectorAll(".chat__preview-thumb")).toHaveLength(1),
    );

    await act(async () => {
      fireEvent.click(afterReload.container.querySelector(".chat__send") as HTMLButtonElement);
    });
    expect(onSend).toHaveBeenCalledWith(
      "保留的回覆",
      [expect.objectContaining({ filename: "reply.png" })],
    );
    afterReload.unmount();

    const afterSend = renderComposer(key);
    expect(afterSend.input.value).toBe("");
    await waitFor(() =>
      expect(afterSend.container.querySelectorAll(".chat__preview-thumb")).toHaveLength(0),
    );
  });

  it("keeps the reply input, attachment preview, and send usable when draft storage refuses files", async () => {
    const key = "reply-card:c-storage-failure";
    testPersistence.refuseAttachmentWrites(key);
    const { input, container } = renderComposer(key);
    fireEvent.change(input, { target: { value: "仍可送出的回覆" } });
    fireEvent.change(
      container.querySelector(".chat__file-input") as HTMLInputElement,
      { target: { files: [pngFile("reply.png")] } },
    );
    await waitFor(() => expect(container.querySelectorAll(".chat__preview-thumb")).toHaveLength(1));
    expect(input.value).toBe("仍可送出的回覆");

    await act(async () => {
      fireEvent.click(container.querySelector(".chat__send") as HTMLButtonElement);
    });

    expect(onSend).toHaveBeenCalledWith(
      "仍可送出的回覆",
      [expect.objectContaining({ filename: "reply.png" })],
    );
  });
});

// Force useIsMobile's matchMedia probe to report a phone viewport. jsdom ships
// no matchMedia, so the hook otherwise defaults to desktop.
function stubMobileViewport() {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: (query: string) => ({
      matches: /max-width/.test(query),
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  });
}

describe("ReplyComposer multi-line input — phone viewport", () => {
  beforeEach(() => {
    onSend.mockClear();
    stubMobileViewport();
  });
  afterEach(() => {
    delete (window as unknown as { matchMedia?: unknown }).matchMedia;
  });

  it("a bare Enter does NOT submit and is NOT prevented (native newline; send is via the button)", () => {
    const { input } = renderComposer();
    fireEvent.change(input, { target: { value: "my answer" } });
    const notPrevented = fireEvent.keyDown(input, { key: "Enter" });
    expect(notPrevented).toBe(true);
    expect(onSend).not.toHaveBeenCalled();
  });

  it("the send button still submits on a phone", async () => {
    const { input, container } = renderComposer();
    fireEvent.change(input, { target: { value: "my answer" } });
    const sendBtn = container.querySelector(".chat__send") as HTMLButtonElement;
    await act(async () => {
      fireEvent.click(sendBtn);
    });
    expect(onSend).toHaveBeenCalledWith("my answer", []);
  });
});
