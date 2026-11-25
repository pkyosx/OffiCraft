import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { MarkdownPreviewOverlay } from "./MarkdownPreviewOverlay";

describe("MarkdownPreviewOverlay — a video attachment", () => {
  const realFetch = globalThis.fetch;
  beforeEach(() => {
    localStorage.setItem("oc_token", "tok-aaaaaaaaaaaa");
  });
  afterEach(() => {
    globalThis.fetch = realFetch;
    localStorage.removeItem("oc_token");
    vi.restoreAllMocks();
  });

  it("plays the attachment at its share link with five speeds, keeps download and 在新頁面顯示 in the header, and never reads the bytes as text", async () => {
    vi.spyOn(api, "getChatAttachmentShareLink").mockResolvedValue("/api/chat/attachment/att-735?sig=test-sig");
    const fetchSpy = vi.fn(async () => ({ ok: true, text: async () => "" }));
    globalThis.fetch = fetchSpy as unknown as typeof fetch;

    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="demo.mp4"
          url="/api/chat/attachment/att-735"
          attachmentId="att-735"
          mime="video/mp4"
          onClose={() => {}}
        />
      </I18nProvider>,
    );

    const video = await waitFor(() => {
      const v = document.body.querySelector(".md-preview video");
      expect(v).not.toBeNull();
      return v!;
    });
    expect(video.getAttribute("src")).toBe(`${window.location.origin}/api/chat/attachment/att-735?sig=test-sig`);
    expect(video.getAttribute("aria-label")).toBe("demo.mp4");
    expect(
      Array.from(document.body.querySelectorAll(".md-preview__speed-option")).map((b) => b.textContent),
    ).toEqual(["0.5×", "1×", "1.25×", "1.5×", "2×"]);

    const download = document.body.querySelector("a.md-preview__download:not(.md-preview__share)")!;
    expect(download.getAttribute("href")).toBe("/api/chat/attachment/att-735?token=tok-aaaaaaaaaaaa");
    expect(download.getAttribute("download")).toBe("demo.mp4");
    await waitFor(() =>
      expect(document.body.querySelector("a.md-preview__new-tab")?.getAttribute("href")).toBe(
        `${window.location.origin}/api/chat/attachment/att-735?sig=test-sig`,
      ),
    );
    expect(document.body.querySelector(".md-preview__status")).toBeNull();
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
