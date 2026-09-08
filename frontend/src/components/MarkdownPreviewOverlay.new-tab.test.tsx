// T-36 — 「在新頁面顯示」 on the attachment preview overlay.
//
// The owner's request, verbatim: 「html 應該要可以點開以後 popup new window 顯示
// 不然我都要複製以後找新的頁面貼很麻煩」. The approved shape (card rc-d645ba36503d,
// option 0): add the button, leave the server's sandbox exactly as it is, and
// say on screen that what opens there will not react to clicks.
//
// THREE THINGS ARE PINNED HERE, and each is a separate mutant:
//   1. the button exists at all, opens the SHARE link, target=_blank + noopener;
//   2. the plain-words note is on screen beside it, in zh AND en;
//   3. it appears ONLY where the browser would DISPLAY the file — never on one
//      it would download, where the button would be a lie.
//
// The overlay PORTALS to document.body (T-76cd), so every reach goes through
// `document.body` / `screen`, never the render() container.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import {
  MarkdownPreviewOverlay,
  isInlineDisplayableMime,
} from "./MarkdownPreviewOverlay";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";

const NEW_TAB = "a.md-preview__new-tab";
const NOTE = ".md-preview__new-tab-note";

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
  it("accepts a .json filename when the mime is octet-stream", () => {
    expect(isInlineDisplayableMime("application/octet-stream", "report.json")).toBe(true);
    expect(isInlineDisplayableMime("application/octet-stream", "REPORT.JSON")).toBe(true);
    expect(isInlineDisplayableMime("application/octet-stream", "report.zip")).toBe(false);
    expect(isInlineDisplayableMime("application/zip", "report.json")).toBe(false);
  });
  it("rejects the mimes the server sends as Content-Disposition: attachment", () => {
    for (const m of [
      "application/zip",
      "application/octet-stream",
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

  it("renders the new-tab button on an html attachment, pointing at the SHARE link, target=_blank rel=noopener", async () => {
    const mint = vi
      .spyOn(api, "getChatAttachmentShareLink")
      .mockResolvedValue("/api/chat/attachment/att-36?sig=test-sig");
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "" })) as unknown as typeof fetch;

    mountOverlay("text/html");

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

  it("shows the plain-words note beside the button — and it names no internal jargon", async () => {
    vi.spyOn(api, "getChatAttachmentShareLink").mockResolvedValue(
      "/api/chat/attachment/att-36?sig=test-sig",
    );
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "" })) as unknown as typeof fetch;

    mountOverlay("text/html");

    await waitFor(() =>
      expect(
        document.body.querySelector(NOTE) === null
          ? "T-36 REGRESSION: the note (.md-preview__new-tab-note) telling the " +
            "reader that the new tab does not react to clicks is missing — the " +
            "owner asked for that sentence to be ON SCREEN (「我會在畫面上講一句」)"
          : "present",
      ).toBe("present"),
    );
    const note = document.body.querySelector(NOTE)!;
    expect(note.textContent).toBe(zh.chat.mdPreview.newTabStaticNote);
    expect(screen.getByText(zh.chat.mdPreview.newTabStaticNote)).toBeTruthy();
    // 🔴 Plain words only. The owner cannot read internal nouns and said so.
    for (const banned of ["CSP", "sandbox", "沙箱", "origin", "script", "JavaScript", "JS"]) {
      expect(
        `${banned} in zh note: ${zh.chat.mdPreview.newTabStaticNote.includes(banned)}`,
      ).toBe(`${banned} in zh note: false`);
      expect(
        `${banned} in en note: ${en.chat.mdPreview.newTabStaticNote.toLowerCase().includes(banned.toLowerCase())}`,
      ).toBe(`${banned} in en note: false`);
    }
  });

  it("ships BOTH locales for the button and the note", () => {
    for (const [name, bundle] of [["zh", zh], ["en", en]] as const) {
      expect(`${name}.openInNewTab=${typeof bundle.chat.mdPreview.openInNewTab}`).toBe(
        `${name}.openInNewTab=string`,
      );
      expect(`${name}.newTabStaticNote=${typeof bundle.chat.mdPreview.newTabStaticNote}`).toBe(
        `${name}.newTabStaticNote=string`,
      );
      expect(bundle.chat.mdPreview.openInNewTab.length).toBeGreaterThan(0);
      expect(bundle.chat.mdPreview.newTabStaticNote.length).toBeGreaterThan(0);
    }
    expect(zh.chat.mdPreview.newTabStaticNote).not.toBe(en.chat.mdPreview.newTabStaticNote);
  });

  it("offers NEITHER button nor note on an attachment the browser downloads", async () => {
    const mint = vi
      .spyOn(api, "getChatAttachmentShareLink")
      .mockResolvedValue("/api/chat/attachment/att-36?sig=test-sig");
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "" })) as unknown as typeof fetch;

    mountOverlay("application/zip", "bundle.zip");

    // The overlay itself is up (this is not a "nothing rendered" false pass).
    await waitFor(() => expect(document.body.querySelector(".md-preview")).toBeTruthy());
    await waitFor(() =>
      expect(document.body.querySelector(".md-preview__status")).toBeTruthy(),
    );
    expect(
      document.body.querySelector(NEW_TAB) !== null
        ? "T-36 REGRESSION: 「在新頁面顯示」 is offered on application/zip — the " +
          "server sends that as Content-Disposition: attachment, so the button " +
          "would DOWNLOAD the file instead of showing it. The gate must mirror " +
          "the server's isPreviewableAttachment rule."
        : "absent",
    ).toBe("absent");
    expect(
      document.body.querySelector(NOTE) !== null
        ? "T-36 REGRESSION: the new-tab note is on screen for application/zip, " +
          "where there is no new-tab button for it to describe"
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
    expect(document.body.querySelector(NOTE)).toBeNull();
  });
});
