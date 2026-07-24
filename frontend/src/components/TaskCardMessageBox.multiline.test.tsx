// Multi-line message box — the task card's 傳訊息給 {executor}… box (owner →
// executor, POST /api/tasks/{id}/message).
//
// Bug report (owner 2026-07-14, screenshot): the message box was a single-line
// <input> — Shift+Enter could not break a line and a long message was clipped
// horizontally. Same fix as the chat composer / ReplyComposer: a <textarea>
// where a bare Enter submits and Shift+Enter breaks a line; the executor-side
// display is already white-space: pre-wrap (office.css .chat__msg-bubble), so
// a multi-line message round-trips verbatim.

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { __resetMock, __injectMockTask } from "../api/mock";
import { api } from "../api";
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
    executorKind: "member",
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

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

describe("task card message box — multi-line input", () => {
  it("the message input is a TEXTAREA (multi-line capable), not a single-line input", async () => {
    __injectMockTask(mkTask({}));
    const { findByTestId } = renderPage();
    const input = await findByTestId("task-msg-input");
    expect(input.tagName).toBe("TEXTAREA");
  });

  it("bare Enter submits the typed message", async () => {
    __injectMockTask(mkTask({}));
    const { findByTestId } = renderPage();
    const input = await findByTestId("task-msg-input");
    fireEvent.change(input, { target: { value: "開始吧" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    await waitFor(async () => {
      const thread = await api.peekChat("mira");
      expect(thread.some((m) => m.body.includes("開始吧"))).toBe(true);
    });
  });

  it("Shift+Enter does NOT submit and is NOT prevented (native newline goes through)", async () => {
    __injectMockTask(mkTask({}));
    const { findByTestId } = renderPage();
    const input = await findByTestId("task-msg-input");
    fireEvent.change(input, { target: { value: "第一行" } });
    const notPrevented = fireEvent.keyDown(input, {
      key: "Enter",
      shiftKey: true,
    });
    expect(notPrevented).toBe(true);
    const thread = await api.peekChat("mira");
    expect(thread.some((m) => m.body.includes("第一行"))).toBe(false);
  });

  it("a multi-line message is sent verbatim — line breaks preserved", async () => {
    __injectMockTask(mkTask({}));
    const { findByTestId } = renderPage();
    const input = await findByTestId("task-msg-input");
    fireEvent.change(input, { target: { value: "第一行\n第二行" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    await waitFor(async () => {
      const thread = await api.peekChat("mira");
      expect(thread.some((m) => m.body.includes("第一行\n第二行"))).toBe(true);
    });
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

describe("task card message box — phone viewport", () => {
  afterEach(() => {
    delete (window as unknown as { matchMedia?: unknown }).matchMedia;
  });

  it("a bare Enter does NOT submit and is NOT prevented (native newline; send is via the button)", async () => {
    stubMobileViewport();
    __injectMockTask(mkTask({}));
    const { findByTestId } = renderPage();
    const input = await findByTestId("task-msg-input");
    fireEvent.change(input, { target: { value: "開始吧" } });
    const notPrevented = fireEvent.keyDown(input, { key: "Enter" });
    expect(notPrevented).toBe(true);
    const thread = await api.peekChat("mira");
    expect(thread.some((m) => m.body.includes("開始吧"))).toBe(false);
  });

  it("the send button still submits on a phone", async () => {
    stubMobileViewport();
    __injectMockTask(mkTask({}));
    const { findByTestId } = renderPage();
    const input = await findByTestId("task-msg-input");
    fireEvent.change(input, { target: { value: "開始吧" } });
    await act(async () => {
      fireEvent.click(await findByTestId("task-msg-send"));
    });
    await waitFor(async () => {
      const thread = await api.peekChat("mira");
      expect(thread.some((m) => m.body.includes("開始吧"))).toBe(true);
    });
  });
});
