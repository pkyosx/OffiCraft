// WHAT THE OWNER SEES in the body of the preview overlay for a file it cannot
// draw: 請下載 for a file the browser would only download, and for an HTML page
// or a PDF whose preview link could not be made. The header's 「在新頁面顯示」
// button, its href and its gate are pinned by
// MarkdownPreviewOverlay.new-tab.test.tsx.
//
// The overlay PORTALS to document.body (T-76cd): every reach goes through
// `document.body` / `screen`, never render()'s container.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { MarkdownPreviewOverlay } from "./MarkdownPreviewOverlay";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";

const NEW_TAB = "a.md-preview__new-tab";
const STATUS = ".md-preview__status";

function mountOverlay(mime: string, title: string) {
  return render(
    <I18nProvider>
      <MarkdownPreviewOverlay
        title={title}
        url="/api/chat/attachment/att-36"
        attachmentId="att-36"
        mime={mime}
        onClose={() => {}}
      />
    </I18nProvider>,
  );
}

function mintOk() {
  return vi
    .spyOn(api, "getChatAttachmentPreviewLink")
    .mockResolvedValue("/api/chat/attachment/att-36?sig=test-sig");
}

describe("the body copy of a file shown only in a new tab points at the button, not at 下載", () => {
  const realFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  it("keeps 請下載 where there is genuinely no button — a downloadable attachment", async () => {
    const mint = mintOk();
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "" })) as unknown as typeof fetch;

    mountOverlay("application/zip", "bundle.zip");

    await waitFor(() => expect(document.body.querySelector(STATUS)).toBeTruthy());
    expect(document.body.querySelector(NEW_TAB)).toBeNull();
    expect(mint).not.toHaveBeenCalled();
    expect(document.body.querySelector(STATUS)?.textContent).toBe(
      zh.chat.mdPreview.unavailable,
    );
  });

  it.each([
    ["application/pdf", "report.pdf"],
    ["text/html", "mock.html"],
  ])("falls back to 請下載 on a %s attachment when the share link could not be minted", async (mime, title) => {
    vi.spyOn(api, "getChatAttachmentPreviewLink").mockRejectedValue(new Error("boom"));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "" })) as unknown as typeof fetch;

    mountOverlay(mime, title);

    await waitFor(() =>
      expect(vi.mocked(console.warn).mock.calls.length).toBeGreaterThan(0),
    );
    await waitFor(() =>
      expect(document.body.querySelector(STATUS)?.textContent).toBe(
        zh.chat.mdPreview.unavailable,
      ),
    );
    expect(document.body.querySelector(NEW_TAB)).toBeNull();
    expect(document.body.querySelector("iframe")).toBeNull();
  });

  it("ships the new line in BOTH locales, and it names no internal jargon", () => {
    for (const [name, bundle] of [["zh", zh], ["en", en]] as const) {
      expect(
        `${name}.unavailableOpenInNewTab=${typeof bundle.chat.mdPreview.unavailableOpenInNewTab}`,
      ).toBe(`${name}.unavailableOpenInNewTab=string`);
      expect(bundle.chat.mdPreview.unavailableOpenInNewTab.length).toBeGreaterThan(0);
      expect(bundle.chat.mdPreview.unavailableOpenInNewTab).not.toBe(
        bundle.chat.mdPreview.unavailable,
      );
    }
    for (const banned of ["CSP", "sandbox", "沙箱", "origin", "script", "JavaScript", "JS"]) {
      expect(
        `${banned} in zh line: ${zh.chat.mdPreview.unavailableOpenInNewTab.includes(banned)}`,
      ).toBe(`${banned} in zh line: false`);
      expect(
        `${banned} in en line: ${en.chat.mdPreview.unavailableOpenInNewTab.toLowerCase().includes(banned.toLowerCase())}`,
      ).toBe(`${banned} in en line: false`);
    }
  });
});
