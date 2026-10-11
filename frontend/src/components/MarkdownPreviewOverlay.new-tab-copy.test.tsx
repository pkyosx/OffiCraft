// T-36 — WHAT THE OWNER ACTUALLY SEES on the preview overlay once the
// 「在新頁面顯示」 button exists. The button itself, its href and its gate are
// pinned by MarkdownPreviewOverlay.new-tab.test.tsx; this file pins the
// product-facing regression that survived it:
//
//   B1  Opening an .html put 「此檔案無法預覽，請下載」 in the biggest, most
//       central spot on the screen — while the ticket's own words were 「不然我
//       都要複製以後找新的頁面貼很麻煩」. The body copy must point at the
//       button, not back at 下載.
//
// The overlay PORTALS to document.body (T-76cd): every reach goes through
// `document.body` / `screen`, never render()'s container.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, waitFor, screen } from "@testing-library/react";
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
    .spyOn(api, "getChatAttachmentShareLink")
    .mockResolvedValue("/api/chat/attachment/att-36?sig=test-sig");
}

describe("T-36 B1 — the .html body copy points at the button, not at 下載", () => {
  const realFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  it("does NOT tell the reader to download an html attachment that has a new-tab button", async () => {
    mintOk();
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "" })) as unknown as typeof fetch;

    mountOverlay("text/html", "mock.html");

    // The button really is there — otherwise 「請下載」 would be the honest line
    // and this test would be pinning the wrong thing.
    await waitFor(() => expect(document.body.querySelector(NEW_TAB)).toBeTruthy());
    const status = document.body.querySelector(STATUS);
    expect(
      status !== null && status.textContent === zh.chat.mdPreview.unavailable
        ? "T-36 B1 REGRESSION: the HTML preview is still telling the owner to " +
          "DOWNLOAD the file (「此檔案無法預覽，請下載」) in the largest, most " +
          "central line on screen — while a 「在新頁面顯示」 button sits in the " +
          "header right above it. His words on this ticket were 「不然我都要複製" +
          "以後找新的頁面貼很麻煩」; this line is asking him to do exactly that."
        : "points elsewhere",
    ).toBe("points elsewhere");
    expect(status?.textContent).toBe(zh.chat.mdPreview.unavailableOpenInNewTab);
    // A render assertion, not just a string compare on the bundle.
    expect(screen.getByText(zh.chat.mdPreview.unavailableOpenInNewTab)).toBeTruthy();
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

  it("falls back to 請下載 when the share link could not be minted", async () => {
    vi.spyOn(api, "getChatAttachmentShareLink").mockRejectedValue(new Error("boom"));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "" })) as unknown as typeof fetch;

    mountOverlay("text/html", "mock.html");

    await waitFor(() =>
      expect(vi.mocked(console.warn).mock.calls.length).toBeGreaterThan(0),
    );
    await waitFor(() =>
      expect(document.body.querySelector(STATUS)?.textContent).toBe(
        zh.chat.mdPreview.unavailable,
      ),
    );
    expect(document.body.querySelector(NEW_TAB)).toBeNull();
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
