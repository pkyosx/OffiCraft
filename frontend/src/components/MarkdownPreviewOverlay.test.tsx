// MarkdownPreviewOverlay + isMarkdownAttachment (T-a1c4). The overlay fetches
// the .md blob text and renders it through the shared Markdown.tsx (NOT the
// browser's raw-source tab); preview and download are two distinct actions, so
// the header keeps a 下載 link alongside the render. isMarkdownAttachment gates
// which attachments get the 預覽 action.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import {
  MarkdownPreviewOverlay,
  isMarkdownAttachment,
} from "./MarkdownPreviewOverlay";

describe("isMarkdownAttachment", () => {
  it("accepts markdown mimes and .md/.markdown filenames", () => {
    expect(isMarkdownAttachment("text/markdown", "x")).toBe(true);
    expect(isMarkdownAttachment("text/x-markdown", "x")).toBe(true);
    expect(isMarkdownAttachment("text/plain", "design.md")).toBe(true);
    expect(isMarkdownAttachment("application/octet-stream", "NOTES.MARKDOWN")).toBe(true);
  });
  it("rejects non-markdown", () => {
    expect(isMarkdownAttachment("application/pdf", "report.pdf")).toBe(false);
    expect(isMarkdownAttachment("image/png", "shot.png")).toBe(false);
    expect(isMarkdownAttachment("text/plain", "notes.txt")).toBe(false);
  });
});

describe("MarkdownPreviewOverlay", () => {
  const realFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  it("fetches the blob text and renders it as markdown", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# Hello\n\nsome **body**",
    })) as unknown as typeof fetch;

    render(
      <I18nProvider>
        <MarkdownPreviewOverlay title="design.md" url="/api/chat/attachment/att-1" onClose={() => {}} />
      </I18nProvider>,
    );
    // Heading rendered as an element (Markdown.tsx builds React elements).
    await waitFor(() => expect(screen.getByRole("heading", { name: "Hello" })).toBeTruthy());
    expect(screen.getByText("body")).toBeTruthy();
    // The download action is present and separate from the preview render.
    const dl = screen.getByText("下載").closest("a")!;
    expect(dl.getAttribute("download")).toBe("design.md");
    expect(dl.getAttribute("href")).toContain("/api/chat/attachment/att-1");
  });

  it("shows an honest error state on a failed fetch (never a blank render)", async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: false, status: 404 })) as unknown as typeof fetch;
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay title="gone.md" url="/api/chat/attachment/att-x" onClose={() => {}} />
      </I18nProvider>,
    );
    await waitFor(() => expect(screen.getByText("無法載入預覽")).toBeTruthy());
  });

  it("closes on the × button and on Esc", async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "# x" })) as unknown as typeof fetch;
    const onClose = vi.fn();
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay title="x.md" url="/api/chat/attachment/att-1" onClose={onClose} />
      </I18nProvider>,
    );
    // Let the fetch settle so the close click is the only state change asserted.
    await waitFor(() => expect(screen.getByRole("heading", { name: "x" })).toBeTruthy());
    fireEvent.click(screen.getByLabelText("關閉預覽"));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
