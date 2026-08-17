// MarkdownPreviewOverlay + isMarkdownAttachment (T-a1c4). The overlay fetches
// the .md blob text and renders it through the shared Markdown.tsx (NOT the
// browser's raw-source tab); preview and download are two distinct actions, so
// the header keeps a 下載 link alongside the render. isMarkdownAttachment gates
// which attachments get the 預覽 action.
//
// The overlay PORTALS to `document.body` (T-76cd), so it is not inside the
// container `render()` hands back — every DOM reach for it goes through
// `document.body` / `screen`. A `container.querySelector(".md-preview…")` here
// returns null and reads like the overlay never rendered.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
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
        <MarkdownPreviewOverlay
          title="design.md"
          url="/api/chat/attachment/att-1"
          attachmentId="att-1"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    // Heading rendered as an element (Markdown.tsx builds React elements).
    await waitFor(() => expect(screen.getByRole("heading", { name: "Hello" })).toBeTruthy());
    expect(screen.getByText("body")).toBeTruthy();
    // The download action is present and separate from the preview render.
    const dl = screen.getByText("下載").closest("a.md-preview__download")!;
    expect(dl.getAttribute("download")).toBe("design.md");
    expect(dl.getAttribute("href")).toContain("/api/chat/attachment/att-1");
  });

  it("renders an image in the shared header shell and changes its zoom", () => {
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay title="shot.png" url="/api/chat/attachment/att-image" attachmentId="att-image" mime="image/png" onClose={() => {}} />
      </I18nProvider>,
    );
    const image = document.body.querySelector<HTMLImageElement>(".md-preview__image")!;
    expect(image.src).toContain("/api/chat/attachment/att-image");
    expect(image.alt).toBe("shot.png");
    // The zoom cluster is reachable by name, not by the bare −/+ glyphs: those
    // announce as "minus"/"plus" and say nothing about what they act on, and
    // the group's label used to be a hard-coded English string invisible to the
    // wording overlay.
    expect(screen.getByRole("group", { name: "縮放圖片" })).toBeTruthy();
    expect(screen.getByText("100%")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "放大" }));
    expect(screen.getByText("125%")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "縮小" }));
    expect(screen.getByText("100%")).toBeTruthy();
  });

  // T-f014: staged composer attachments used to open a SECOND overlay
  // (the retired Lightbox) with nothing but an ×. They now land in
  // this shell — but the bytes have not been uploaded, so there is no blob id
  // and therefore nothing a share link could point at. Download stays: the
  // data: URI IS the file.
  it("previews a staged image from bytes in hand, with download but no share link", () => {
    const dataUri = "data:image/png;base64,iVBORw0KGgo=";
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay title="pasted.png" imageSrc={dataUri} onClose={() => {}} />
      </I18nProvider>,
    );
    const image = document.body.querySelector<HTMLImageElement>(".md-preview__image");
    // Named, not a `!` deref: "the image element is missing" is a distinct
    // failure from "it points at the wrong bytes", and a TypeError reports
    // neither.
    expect(image, "the staged bytes must render through the image branch").not.toBeNull();
    expect(image!.getAttribute("src")).toBe(dataUri);
    expect(document.body.querySelector("button.md-preview__share")).toBeNull();
    const download = document.body.querySelector<HTMLAnchorElement>("a.md-preview__download");
    expect(download, "staged bytes are a real file — the download stays").not.toBeNull();
    expect(download!.getAttribute("href")).toBe(dataUri);
    expect(download!.getAttribute("download")).toBe("pasted.png");
  });

  // The rendered body wears `.doc-md`, the shared markdown skin every other
  // render site uses (task manual, role doc, reply card, chat bubble). Without
  // it the overlay fell back to bare UA defaults — unstyled headings, code,
  // tables and callouts inside an otherwise themed panel (owner 2026-07-28:
  // 「明明我們聊天的樣式很漂亮，但 .md 閱覽器就沒有對應的上色」).
  it("wears the shared .doc-md markdown skin", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# Hello",
    })) as unknown as typeof fetch;
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="x.md"
          url="/api/chat/attachment/a"
          attachmentId="a"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    await waitFor(() => expect(screen.getByRole("heading", { name: "Hello" })).toBeTruthy());
    const md = document.body.querySelector(".md-preview__md")!;
    expect(md.classList.contains("doc-md")).toBe(true);
  });

  // Second source mode (the chat 放大閱讀 button): text the caller already
  // holds. Nothing to fetch, and no file to download.
  it("renders an inline source without fetching, and offers no download", async () => {
    globalThis.fetch = vi.fn(() => Promise.reject(new Error("must not fetch"))) as unknown as typeof fetch;
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay title="Mira" source={"# Inline\n\nbody text"} onClose={() => {}} />
      </I18nProvider>,
    );
    // Synchronous — an inline source never passes through the loading state.
    expect(screen.getByRole("heading", { name: "Inline" })).toBeTruthy();
    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(screen.queryByText("載入預覽中…")).toBeNull();
    expect(document.body.querySelector(".md-preview__download")).toBeNull();
    expect(document.body.querySelector(".md-preview__md")!.classList.contains("doc-md")).toBe(true);
  });

  // A message body is a CHAT surface: Enter means "new line" there, and the
  // bubble renders it with `breaks`. Reading the same text full-view must not
  // reflow it — standard markdown folds a single newline into a space, so a
  // plain multi-line message (the most common shape) came out as one run-on
  // line the moment it was opened (Seth 2026-07-28 on PR #18).
  it("keeps single newlines in an inline source (the bubble's own breaks)", () => {
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="Mira"
          source={"先確認環境變數。\n不要直接在 prod 跑。\n有問題隨時問我。"}
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    const md = document.body.querySelector(".md-preview__md")!;
    // Three source lines ⇒ two hard breaks inside one paragraph.
    expect(md.querySelectorAll("br").length).toBe(2);
    expect(md.querySelectorAll("p").length).toBe(1);
    // POSITIVE CONTROL: the lines are all there…
    expect(md.textContent).toContain("先確認環境變數。");
    expect(md.textContent).toContain("有問題隨時問我。");
    // …and were not welded together with a space (the exact default this guards).
    expect(md.textContent).not.toContain("先確認環境變數。 不要直接在 prod 跑。");
  });

  // The OTHER half of that split, pinned so "turn breaks on everywhere" cannot
  // pass as a fix: a stored .md file is a document, not a chat line, and keeps
  // standard markdown soft-wrap the way every other document surface does.
  it("keeps standard soft-wrap for a fetched .md blob (no hard breaks)", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "alpha line\nbeta line",
    })) as unknown as typeof fetch;
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="doc.md"
          url="/api/chat/attachment/att-doc"
          attachmentId="att-doc"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    await waitFor(() => expect(screen.getByText(/alpha line/)).toBeTruthy());
    const md = document.body.querySelector(".md-preview__md")!;
    expect(md.querySelectorAll("br").length).toBe(0);
    expect(md.textContent).toContain("alpha line beta line");
  });

  // The blob actions belong to `url` mode and must survive the message mode
  // being added beside them (T-4fdc gave the share link a REQUIRED attachment
  // id; PR #18 must not quietly drop either).
  it("keeps the share link + download on a blob, and gives an inline source neither", async () => {
    const mint = vi
      .spyOn(api, "getChatAttachmentShareLink")
      .mockResolvedValue("/api/chat/attachment/att-backing?sig=test");
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: vi.fn(async () => {}) },
      configurable: true,
    });
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# doc",
    })) as unknown as typeof fetch;

    const blob = render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="doc.md"
          url="/api/chat/attachment/att-doc"
          attachmentId="att-backing"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    await waitFor(() =>
      expect(document.body.querySelector("button.md-preview__share")).toBeTruthy(),
    );
    fireEvent.click(document.body.querySelector("button.md-preview__share")!);
    // The share link is minted for the ATTACHMENT ID, never the serve path.
    await waitFor(() => expect(mint).toHaveBeenCalledWith("att-backing"));
    expect(mint).not.toHaveBeenCalledWith("/api/chat/attachment/att-doc");
    expect(
      document.body.querySelector("a.md-preview__download"),
    ).toBeTruthy();
    blob.unmount();

    render(
      <I18nProvider>
        <MarkdownPreviewOverlay title="Mira" source="# body" onClose={() => {}} />
      </I18nProvider>,
    );
    expect(document.body.querySelector("button.md-preview__share")).toBeNull();
    expect(document.body.querySelector("a.md-preview__download")).toBeNull();
  });

  it("shows an honest error state on a failed fetch (never a blank render)", async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: false, status: 404 })) as unknown as typeof fetch;
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="gone.md"
          url="/api/chat/attachment/att-x"
          attachmentId="att-x"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    await waitFor(() => expect(screen.getByText("無法載入預覽")).toBeTruthy());
  });

  it("closes on the × button and on Esc", async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "# x" })) as unknown as typeof fetch;
    const onClose = vi.fn();
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="x.md"
          url="/api/chat/attachment/att-1"
          attachmentId="att-1"
          onClose={onClose}
        />
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

describe("MarkdownPreviewOverlay 複製分享連結 (T-d10b)", () => {
  const realFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  function renderOverlay() {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# doc",
    })) as unknown as typeof fetch;
    return render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="design.md"
          url="/api/chat/attachment/att-7"
          attachmentId="att-7"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
  }

  it("sits LEFT of 下載 in the header action row", async () => {
    renderOverlay();
    await waitFor(() => expect(screen.getByRole("heading", { name: "doc" })).toBeTruthy());
    const actions = document.body.querySelector(".md-preview__actions")!;
    const share = screen.getByRole("button", { name: "複製分享連結" });
    const dl = screen.getByText("下載").closest("a.md-preview__download")!;
    expect(actions.contains(share)).toBe(true);
    expect(share.compareDocumentPosition(dl) & 4).toBe(4);
  });

  it("mints THIS attachment's share link and flashes 已複製連結", async () => {
    const mint = vi
      .spyOn(api, "getChatAttachmentShareLink")
      .mockResolvedValue("/api/chat/attachment/att-7?sig=test-sig");
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    renderOverlay();
    await waitFor(() => expect(screen.getByRole("heading", { name: "doc" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "複製分享連結" }));

    await waitFor(() => expect(mint).toHaveBeenCalledWith("att-7"));
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(
        `${window.location.origin}/api/chat/attachment/att-7?sig=test-sig`,
      ),
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "已複製連結" })).toBeTruthy(),
    );
  });

  it("shows 複製連結失敗 when the mint fails instead of faking success", async () => {
    vi.spyOn(api, "getChatAttachmentShareLink").mockRejectedValue(new Error("boom"));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    renderOverlay();
    await waitFor(() => expect(screen.getByRole("heading", { name: "doc" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "複製分享連結" }));
    await waitFor(() => expect(writeText).not.toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: "已複製連結" })).toBeNull();
    expect(screen.getByRole("button", { name: "複製連結失敗" })).toBeTruthy();
  });
});
