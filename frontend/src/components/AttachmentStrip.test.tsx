// AttachmentStrip — the shared renderer for STORED attachments, and the owner
// of the preview overlay every one of its items opens.
//
// 🔴 WHAT THIS FILE EXISTS FOR (T-48, R11-1). The strip is mounted from four
// places — a chat message row, both reply-card faces and the task-artifacts
// popover — and three of them are not inside a "conversation" at all, so the
// per-visit keying that protects `ChatArea`'s own overlays has nothing to say
// here. The invariant that DOES hold everywhere is narrower and stronger: an
// open preview is an item of the list this strip is rendering right now. Hand
// the strip a different list and the overlay goes with the old one, on the same
// render, with no effect to fire and no guard to forget.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { AttachmentStrip } from "./AttachmentStrip";
import type { ChatAttachmentView } from "../api/adapter";

function att(id: string, filename: string): ChatAttachmentView {
  return {
    id,
    url: `/api/chat/attachment/${id}`,
    filename,
    mime: "text/markdown",
    isImage: false,
  };
}

function img(id: string, filename: string): ChatAttachmentView {
  return {
    id,
    url: `/api/chat/attachment/${id}`,
    filename,
    mime: "image/png",
    isImage: true,
  };
}

function openTitle(): string | undefined {
  return document.body.querySelector(".md-preview__title")?.textContent ?? undefined;
}

function pagerCount(): string | undefined {
  return document.body.querySelector(".md-preview__pager-count")?.textContent ?? undefined;
}

function renderStrip(attachments: ChatAttachmentView[]) {
  return render(
    <I18nProvider>
      <AttachmentStrip
        attachments={attachments}
        className="chat__msg-attachments"
        imageClassName="chat__msg-image"
      />
    </I18nProvider>,
  );
}

beforeEach(() => {
  localStorage.setItem("oc_token", "jwt-1");
  globalThis.fetch = vi.fn(async () => ({
    ok: true,
    text: async () => "# 內容",
  })) as unknown as typeof fetch;
});

describe("AttachmentStrip", () => {
  it("opens a chip's preview in the shared overlay", async () => {
    const { container } = renderStrip([att("a1", "設計.md")]);
    fireEvent.click(container.querySelector("button.chat__msg-file")!);
    await waitFor(() =>
      expect(document.body.querySelector(".md-preview")).toBeTruthy(),
    );
    expect(document.body.textContent).toContain("設計.md");
  });

  it("drops an open preview whose item is no longer in the list", async () => {
    const first = att("a1", "第一份.md");
    const second = att("a2", "第二份.md");
    const { container, rerender } = renderStrip([first, second]);
    fireEvent.click(container.querySelectorAll("button.chat__msg-file")[0]);
    await waitFor(() =>
      expect(document.body.querySelector(".md-preview")).toBeTruthy(),
    );

    // The list this strip renders is replaced — an artifact un-pinned from the
    // popover, a reply card swapped, a message row reused. Whatever the caller
    // is, the file behind the overlay is not on their screen any more.
    rerender(
      <I18nProvider>
        <AttachmentStrip
          attachments={[second]}
          className="chat__msg-attachments"
          imageClassName="chat__msg-image"
        />
      </I18nProvider>,
    );
    expect(document.body.querySelector(".md-preview")).toBeNull();
    expect(document.body.textContent).not.toContain("第一份.md");
  });

  // T-123 — the reader opened one attachment of a message that carries several
  // and wants the next one without going back to the row. The overlay已經
  // supports paging; what these pin down is that this strip HANDS IT the list.

  it("steps to the next attachment of the same strip from the next control", async () => {
    const { container } = renderStrip([att("a1", "第一份.md"), att("a2", "第二份.md")]);
    fireEvent.click(container.querySelectorAll("button.chat__msg-file")[0]);
    await waitFor(() => expect(document.body.querySelector(".md-preview")).toBeTruthy());
    expect(openTitle()).toBe("第一份.md");

    fireEvent.click(document.body.querySelector("button.md-preview__pager--next")!);
    await waitFor(() => expect(openTitle()).toBe("第二份.md"));
    expect(pagerCount()).toBe("2 / 2");
  });

  it("steps back to the previous attachment from the previous control", async () => {
    const { container } = renderStrip([att("a1", "第一份.md"), att("a2", "第二份.md")]);
    fireEvent.click(container.querySelectorAll("button.chat__msg-file")[1]);
    await waitFor(() => expect(openTitle()).toBe("第二份.md"));

    fireEvent.click(document.body.querySelector("button.md-preview__pager--prev")!);
    await waitFor(() => expect(openTitle()).toBe("第一份.md"));
    expect(pagerCount()).toBe("1 / 2");
  });

  it("steps between images with the arrow keys", async () => {
    const { container } = renderStrip([img("i1", "第一張.png"), img("i2", "第二張.png")]);
    fireEvent.click(container.querySelectorAll("img.chat__msg-image")[0]);
    await waitFor(() => expect(openTitle()).toBe("第一張.png"));

    fireEvent.keyDown(document, { key: "ArrowRight" });
    await waitFor(() => expect(openTitle()).toBe("第二張.png"));

    fireEvent.keyDown(document, { key: "ArrowLeft" });
    await waitFor(() => expect(openTitle()).toBe("第一張.png"));
  });

  it("stays on the last attachment at the end of the list instead of wrapping", async () => {
    const { container } = renderStrip([img("i1", "第一張.png"), img("i2", "第二張.png")]);
    fireEvent.click(container.querySelectorAll("img.chat__msg-image")[1]);
    await waitFor(() => expect(openTitle()).toBe("第二張.png"));

    expect(
      document.body.querySelector("button.md-preview__pager--next")!.hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.keyDown(document, { key: "ArrowRight" });
    await waitFor(() => expect(pagerCount()).toBe("2 / 2"));
    expect(openTitle()).toBe("第二張.png");
  });

  it("stays on the first attachment at the start of the list instead of wrapping", async () => {
    const { container } = renderStrip([img("i1", "第一張.png"), img("i2", "第二張.png")]);
    fireEvent.click(container.querySelectorAll("img.chat__msg-image")[0]);
    await waitFor(() => expect(openTitle()).toBe("第一張.png"));

    expect(
      document.body.querySelector("button.md-preview__pager--prev")!.hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.keyDown(document, { key: "ArrowLeft" });
    await waitFor(() => expect(pagerCount()).toBe("1 / 2"));
    expect(openTitle()).toBe("第一張.png");
  });

  it("walks a mixed strip in its drawn order, images and files alike", async () => {
    const { container } = renderStrip([img("i1", "圖.png"), att("a1", "說明.md")]);
    fireEvent.click(container.querySelectorAll("img.chat__msg-image")[0]);
    await waitFor(() => expect(openTitle()).toBe("圖.png"));
    expect(pagerCount()).toBe("1 / 2");

    fireEvent.click(document.body.querySelector("button.md-preview__pager--next")!);
    await waitFor(() => expect(openTitle()).toBe("說明.md"));
    expect(pagerCount()).toBe("2 / 2");
  });

  // 🔴 THE ONE THAT GUARDS `findIndex` (T-123 review, B1). Every test above
  // keeps the same list from first click to last assertion, and a strip that
  // REMEMBERED the position instead of re-deriving it passes all of them. The
  // task-artifacts popover's list is live (SSE), and its rows are grouped
  // files-then-images rather than appended, so a file pinned while the reader
  // has an image open lands BEFORE the open one and shifts it. A remembered
  // position then says "2 / 4" while the reader is on the third, and its next
  // control steps back onto the item already on screen.
  it("re-derives the position when the list grows in front of the open item", async () => {
    const a1 = att("a1", "第一份.md");
    const a2 = att("a2", "第二份.md");
    const a3 = att("a3", "第三份.md");
    const { container, rerender } = renderStrip([a1, a2, a3]);
    fireEvent.click(container.querySelectorAll("button.chat__msg-file")[1]);
    await waitFor(() => expect(openTitle()).toBe("第二份.md"));
    expect(pagerCount()).toBe("2 / 3");

    const a0 = att("a0", "插隊的.md");
    rerender(
      <I18nProvider>
        <AttachmentStrip
          attachments={[a0, a1, a2, a3]}
          className="chat__msg-attachments"
          imageClassName="chat__msg-image"
        />
      </I18nProvider>,
    );

    await waitFor(() => expect(pagerCount()).toBe("3 / 4"));
    expect(openTitle()).toBe("第二份.md");

    fireEvent.click(document.body.querySelector("button.md-preview__pager--next")!);
    await waitFor(() => expect(openTitle()).toBe("第三份.md"));
    expect(pagerCount()).toBe("4 / 4");
  });

  it("shows no paging control when the strip holds a single attachment", async () => {
    const { container } = renderStrip([img("i1", "唯一.png")]);
    fireEvent.click(container.querySelector("img.chat__msg-image")!);
    await waitFor(() => expect(document.body.querySelector(".md-preview")).toBeTruthy());

    expect(document.body.querySelector("button.md-preview__pager--next")).toBeNull();
    expect(document.body.querySelector("button.md-preview__pager--prev")).toBeNull();
    expect(document.body.querySelector(".md-preview__pager-count")).toBeNull();

    fireEvent.keyDown(document, { key: "ArrowRight" });
    await waitFor(() => expect(openTitle()).toBe("唯一.png"));
  });
});
