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
});
