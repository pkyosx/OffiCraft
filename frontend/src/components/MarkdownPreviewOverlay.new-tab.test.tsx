// T-36 — 「在新頁面顯示」 on the attachment preview overlay.
//
// The owner's request, verbatim: 「html 應該要可以點開以後 popup new window 顯示
// 不然我都要複製以後找新的頁面貼很麻煩」 (card rc-d645ba36503d).
//
// TWO THINGS ARE PINNED HERE, and each is a separate mutant:
//   1. the button exists at all, opens the SHARE link, target=_blank + noopener
//      — an HTML page opened there may script (T-392), so a ?token= in its
//      address would hand it the owner's login;
//   2. it appears ONLY where the browser would DISPLAY the file — never on one
//      it would download, where the button would be a lie.
//
// The overlay PORTALS to document.body (T-76cd), so every reach goes through
// `document.body` / `screen`, never the render() container.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import {
  MarkdownPreviewOverlay,
  isInlineDisplayableMime,
} from "./MarkdownPreviewOverlay";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";

const NEW_TAB = "a.md-preview__new-tab";

function mountOverlay(mime: string, title = "mock.html") {
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

describe("isInlineDisplayableMime (mirror of the server's isPreviewableAttachment)", () => {
  it("accepts image/*, text/*, application/pdf and application/json", () => {
    for (const m of [
      "image/png",
      "image/svg+xml",
      "text/html",
      "text/plain",
      "text/markdown",
      "application/pdf",
      "application/json",
      "application/json; charset=utf-8",
    ]) {
      expect(`${m}=${isInlineDisplayableMime(m)}`).toBe(`${m}=true`);
    }
  });
  it("rejects the mimes the server sends as Content-Disposition: attachment", () => {
    for (const m of [
      "application/zip",
      "application/octet-stream",
      "application/octet-stream; name=report.pdf",
      "video/mp4",
      "",
    ]) {
      expect(`${m}=${isInlineDisplayableMime(m)}`).toBe(`${m}=false`);
    }
  });
});

describe("MarkdownPreviewOverlay — 在新頁面顯示 (T-36)", () => {
  const realFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  it.each([
    ["text/html", "mock.html"],
    ["application/pdf", "AI-Data-Security.pdf"],
  ])("renders the new-tab button on a %s attachment, pointing at the SHARE link, target=_blank rel=noopener", async (mime, title) => {
    const mint = vi
      .spyOn(api, "getChatAttachmentShareLink")
      .mockResolvedValue("/api/chat/attachment/att-36?sig=test-sig");
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "" })) as unknown as typeof fetch;

    mountOverlay(mime, title);

    await waitFor(() =>
      expect(
        document.body.querySelector(NEW_TAB) === null
          ? "T-36 REGRESSION: the 「在新頁面顯示」 button (a.md-preview__new-tab) is " +
            "not in the overlay header — the owner's request (html 點開以後 popup " +
            "new window 顯示) is unimplemented"
          : "present",
      ).toBe("present"),
    );
    const a = document.body.querySelector(NEW_TAB) as HTMLAnchorElement;
    // The SHARE link (?sig=), never the ?token= authed URL the download uses:
    // an owner session token must not ride into another tab's address bar.
    expect(mint).toHaveBeenCalledWith("att-36");
    expect(a.getAttribute("href")).toBe(
      `${window.location.origin}/api/chat/attachment/att-36?sig=test-sig`,
    );
    expect(a.getAttribute("href")).not.toContain("token=");
    expect(`target=${a.getAttribute("target")}`).toBe("target=_blank");
    expect(`rel=${a.getAttribute("rel")}`).toContain("noopener");
    expect(a.getAttribute("aria-label")).toBe(zh.chat.mdPreview.openInNewTab);
  });

  it("ships BOTH locales for the button", () => {
    for (const [name, bundle] of [["zh", zh], ["en", en]] as const) {
      expect(`${name}.openInNewTab=${typeof bundle.chat.mdPreview.openInNewTab}`).toBe(
        `${name}.openInNewTab=string`,
      );
      expect(bundle.chat.mdPreview.openInNewTab.length).toBeGreaterThan(0);
    }
  });

  it.each([
    ["application/zip", "bundle.zip"],
    ["application/octet-stream", "report.pdf"],
  ])("offers no button on a %s attachment the browser downloads", async (mime, title) => {
    const mint = vi
      .spyOn(api, "getChatAttachmentShareLink")
      .mockResolvedValue("/api/chat/attachment/att-36?sig=test-sig");
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "" })) as unknown as typeof fetch;

    mountOverlay(mime, title);

    // The overlay itself is up (this is not a "nothing rendered" false pass).
    await waitFor(() => expect(document.body.querySelector(".md-preview")).toBeTruthy());
    await waitFor(() =>
      expect(document.body.querySelector(".md-preview__status")).toBeTruthy(),
    );
    expect(
      document.body.querySelector(NEW_TAB) !== null
        ? `T-36 REGRESSION: 「在新頁面顯示」 is offered on ${mime} — the ` +
          "server sends that as Content-Disposition: attachment, so the button " +
          "would DOWNLOAD the file instead of showing it. The gate must mirror " +
          "the server's isPreviewableAttachment rule."
        : "absent",
    ).toBe("absent");
    // And no link was even minted for a file that cannot use one.
    expect(mint).not.toHaveBeenCalled();
  });

  it("renders no anchor when the share link cannot be minted (never a 404 href)", async () => {
    vi.spyOn(api, "getChatAttachmentShareLink").mockRejectedValue(new Error("boom"));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "" })) as unknown as typeof fetch;

    mountOverlay("text/html");

    await waitFor(() => expect(document.body.querySelector(".md-preview")).toBeTruthy());
    await waitFor(() =>
      expect(vi.mocked(console.warn).mock.calls.length).toBeGreaterThan(0),
    );
    expect(document.body.querySelector(NEW_TAB)).toBeNull();
  });
});
