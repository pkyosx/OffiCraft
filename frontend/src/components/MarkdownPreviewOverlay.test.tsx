// MarkdownPreviewOverlay (T-a1c4). The overlay fetches
// the .md blob text and renders it through the shared Markdown.tsx (NOT the
// browser's raw-source tab); preview and download are two distinct actions, so
// the header keeps a 下載 link alongside the render.
//
// The overlay PORTALS to `document.body` (T-76cd), so it is not inside the
// container `render()` hands back — every DOM reach for it goes through
// `document.body` / `screen`. A `container.querySelector(".md-preview…")` here
// returns null and reads like the overlay never rendered.

import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, fireEvent, waitFor, screen, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { MarkdownPreviewOverlay } from "./MarkdownPreviewOverlay";
import { AlertTriangleIcon } from "./icons";
import { Markdown } from "./Markdown";
import { zh } from "../i18n/locales/zh";
import snippet from "../../../cli/ocagent/doc_comments_snippet.html?raw";

describe("MarkdownPreviewOverlay", () => {
  const realFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
    localStorage.removeItem("oc_token");
  });

  it("fetches the blob text and renders it as markdown, block images included", async () => {
    localStorage.setItem("oc_token", "tok-aaaaaaaaaaaa");
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () =>
        "# Hello\n\nsome **body**\n\n![架構圖](/api/chat/attachment/att-img)\n\n" +
        "![](https://cdn.example.com/a.png)\n\n![x](javascript:alert(1))",
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
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "Hello" })).toBeTruthy(),
    );
    expect(screen.getByText("body")).toBeTruthy();
    expect(
      [...document.body.querySelectorAll(".md-preview__md img")].map((img) => [
        img.getAttribute("src"),
        img.getAttribute("alt"),
      ]),
    ).toEqual([
      ["/api/chat/attachment/att-img?token=tok-aaaaaaaaaaaa", "架構圖"],
      ["https://cdn.example.com/a.png", ""],
    ]);
    expect(screen.getByText("![x](javascript:alert(1))")).toBeTruthy();
    // The download action is present and separate from the preview render.
    // Reached by ACCESSIBLE NAME, not by visible text: T-51 ④ made the header
    // controls icon-only (owner: 「又都有字太多了，可以一起改成圖示就好嘛」), so
    // 下載 is now the `aria-label` and nothing in the header prints it.
    const dl = screen.getByRole("link", { name: "下載" }) as HTMLAnchorElement;
    expect(dl.getAttribute("download")).toBe("design.md");
    expect(dl.getAttribute("href")).toContain("/api/chat/attachment/att-1");
  });

  it("renders an image in the shared header shell and changes its zoom", () => {
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="shot.png"
          url="/api/chat/attachment/att-image"
          attachmentId="att-image"
          mime="image/png"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    const image =
      document.body.querySelector<HTMLImageElement>(".md-preview__image")!;
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

  // ── Paging across a caller-owned list (T-51 ①) ────────────────────────────
  //
  // 🔴 THE ARROW KEYS WERE ALREADY SPOKEN FOR, and that is the whole risk in
  // this feature. Once an image is zoomed the wrap is a real scroll container
  // (T-7e68 made the zoom real layout precisely so the edges the zoom pushes
  // out of the frame stay reachable), and the arrows are the KEYBOARD's only
  // way to get to them. A pager that grabs ArrowLeft/Right unconditionally
  // re-opens the exact owner report that change exists to answer:
  // 「可以放大，但無法左右或上下移動」. Same for a text file, which scrolls with
  // the arrows and has no second way to reach its bottom.
  //
  // These three cases are the difference between "paging works" and "paging did
  // not cost anything"; the first alone is green with the bug present.

  function renderPaged(
    onGo: (i: number) => void,
    extra: { mime?: string; index?: number } = {},
  ) {
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="shot.png"
          url="/api/chat/attachment/att-image"
          attachmentId="att-image"
          mime={extra.mime ?? "image/png"}
          pager={{ index: extra.index ?? 1, total: 5, onGo }}
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    return document.body.querySelector<HTMLElement>(".md-preview")!;
  }

  it("steps through the caller's list from the arrow keys while the image is at 100%", () => {
    const onGo = vi.fn();
    const root = renderPaged(onGo);
    fireEvent.keyDown(root, { key: "ArrowRight" });
    expect(onGo).toHaveBeenCalledWith(2);
    fireEvent.keyDown(root, { key: "ArrowLeft" });
    expect(onGo).toHaveBeenLastCalledWith(0);
  });

  it("keeps paging from the arrow keys even while the image is zoomed", () => {
    // The owner's ruling (2026-09-02, `c-521c38a1de77`), made against the first
    // implementation, which handed the arrows back to the pan above 100%: a
    // zoomed image can still be moved by drag, wheel, scrollbar and touch, so
    // the arrows are not its only handle — while paging had no keyboard at all
    // whenever a picture happened to be zoomed. ⚠️ The cost he accepted, named
    // here so this is not reverted as a regression: a reader who uses ONLY a
    // keyboard can no longer pan a zoomed image.
    const onGo = vi.fn();
    const root = renderPaged(onGo);
    fireEvent.click(screen.getByRole("button", { name: "放大" }));
    expect(screen.getByText("125%")).toBeTruthy();
    fireEvent.keyDown(root, { key: "ArrowRight" });
    expect(onGo).toHaveBeenCalledWith(2);
  });

  it("leaves the arrow keys to a text body, which has no other way to scroll", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# Long\n\nbody",
    })) as unknown as typeof fetch;
    const onGo = vi.fn();
    const root = renderPaged(onGo, { mime: "text/markdown" });
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "Long" })).toBeTruthy(),
    );
    fireEvent.keyDown(root, { key: "ArrowRight" });
    expect(onGo).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "下一個" }));
    expect(onGo).toHaveBeenCalledWith(2);
  });

  it("stops at the ends instead of wrapping around", () => {
    const onGo = vi.fn();
    const root = renderPaged(onGo, { index: 0 });
    fireEvent.keyDown(root, { key: "ArrowLeft" });
    expect(
      onGo,
      "there is nothing before the first item — silently landing on the last one loses the reader's place",
    ).not.toHaveBeenCalled();
    expect(
      (screen.getByRole("button", { name: "上一個" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("carries no paging control when the caller has no list behind the item", () => {
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="shot.png"
          url="/api/chat/attachment/att-image"
          attachmentId="att-image"
          mime="image/png"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    expect(screen.queryByRole("button", { name: "下一個" })).toBeNull();
    expect(document.body.querySelector(".md-preview__pager-count")).toBeNull();
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
        <MarkdownPreviewOverlay
          title="pasted.png"
          imageSrc={dataUri}
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    const image =
      document.body.querySelector<HTMLImageElement>(".md-preview__image");
    // Named, not a `!` deref: "the image element is missing" is a distinct
    // failure from "it points at the wrong bytes", and a TypeError reports
    // neither.
    expect(
      image,
      "the staged bytes must render through the image branch",
    ).not.toBeNull();
    expect(image!.getAttribute("src")).toBe(dataUri);
    expect(document.body.querySelector("button.md-preview__share")).toBeNull();
    const download = document.body.querySelector<HTMLAnchorElement>(
      "a.md-preview__download",
    );
    expect(
      download,
      "staged bytes are a real file — the download stays",
    ).not.toBeNull();
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
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "Hello" })).toBeTruthy(),
    );
    const md = document.body.querySelector(".md-preview__md")!;
    expect(md.classList.contains("doc-md")).toBe(true);
  });

  // Second source mode (the chat 放大閱讀 button): text the caller already
  // holds. Nothing to fetch, and no file to download.
  it("renders an inline source without fetching, and offers no download", async () => {
    globalThis.fetch = vi.fn(() =>
      Promise.reject(new Error("must not fetch")),
    ) as unknown as typeof fetch;
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="Mira"
          source={"# Inline\n\nbody text\n\n![架構圖](/api/chat/attachment/att-img)"}
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    // Synchronous — an inline source never passes through the loading state.
    expect(screen.getByRole("heading", { name: "Inline" })).toBeTruthy();
    expect(document.body.querySelector(".md-preview__md img")).toBeNull();
    expect(screen.getByText("![架構圖](/api/chat/attachment/att-img)")).toBeTruthy();
    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(screen.queryByText("載入預覽中…")).toBeNull();
    expect(document.body.querySelector(".md-preview__download")).toBeNull();
    expect(
      document.body
        .querySelector(".md-preview__md")!
        .classList.contains("doc-md"),
    ).toBe(true);
  });

  it("fetches a JSON attachment and renders its contents with indentation", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => '{"name":"Mira","items":[1,2]}',
    })) as unknown as typeof fetch;
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="report.json"
          url="/api/chat/attachment/att-json"
          attachmentId="att-json"
          mime="application/octet-stream"
          onClose={() => {}}
        />
      </I18nProvider>,
    );

    await waitFor(() =>
      expect(document.body.querySelector("pre.md-preview__text")).toBeTruthy(),
    );
    expect(document.body.querySelector("pre.md-preview__text")?.textContent).toBe(
      '{\n  "name": "Mira",\n  "items": [\n    1,\n    2\n  ]\n}',
    );
  });

  it.each([
    ["ci.yaml", "text/plain", "# keep\nkey:  v\n", "pre.md-preview__text", "# keep\nkey:  v\n"],
    ["feed.xml", "text/xml", "<a><b>1</b></a>", "pre.md-preview__text", "<a>\n  <b>1</b>\n</a>"],
    ["main.py", "text/plain", "print(1)\n", "pre.md-preview__text", "print(1)\n"],
    ["t.csv", "text/csv", "a,b\n1,2\n", ".md-preview__csv table", "ab12"],
  ])("draws a %s attachment in the panel instead of sending it to a tab", async (title, mime, text, selector, shown) => {
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => text })) as unknown as typeof fetch;
    vi.spyOn(api, "getChatAttachmentShareLink").mockResolvedValue("/api/chat/attachment/att-t?sig=s");
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title={title}
          url="/api/chat/attachment/att-t"
          attachmentId="att-t"
          mime={mime}
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    await waitFor(() => expect(document.body.querySelector(selector)).toBeTruthy());
    expect(document.body.querySelector(selector)?.textContent).toBe(shown);
    expect(document.body.querySelector(".md-preview__status")).toBeNull();
  });

  it("shows invalid JSON unchanged with the unparseable explanation", async () => {
    const raw = '{"name":}';
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => raw,
    })) as unknown as typeof fetch;
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="broken.json"
          url="/api/chat/attachment/att-broken-json"
          attachmentId="att-broken-json"
          mime="application/json"
          onClose={() => {}}
        />
      </I18nProvider>,
    );

    await waitFor(() =>
      expect(screen.getByText(zh.chat.mdPreview.jsonUnparseable)).toBeTruthy(),
    );
    expect(document.body.querySelector("pre.md-preview__text")?.textContent).toBe(raw);
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
    expect(md.textContent).not.toContain(
      "先確認環境變數。 不要直接在 prod 跑。",
    );
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
  it("keeps the share link + download on a blob, with 編輯 first when the caller offers it, and gives an inline source none of them", async () => {
    const onEdit = vi.fn();
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
          onEdit={onEdit}
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    await waitFor(() =>
      expect(
        document.body.querySelector("button.md-preview__share"),
      ).toBeTruthy(),
    );
    await waitFor(() =>
      expect(
        Array.from(document.body.querySelectorAll(".md-preview__actions > *")).map((el) =>
          el.getAttribute("aria-label"),
        ),
      ).toEqual(["編輯", "複製分享連結", "在新頁面顯示", "下載", "關閉預覽"]),
    );
    fireEvent.click(screen.getByRole("button", { name: "編輯" }));
    expect(onEdit).toHaveBeenCalledTimes(1);
    fireEvent.click(document.body.querySelector("button.md-preview__share")!);
    // The share link is minted for the ATTACHMENT ID, never the serve path.
    await waitFor(() => expect(mint).toHaveBeenCalledWith("att-backing"));
    expect(mint).not.toHaveBeenCalledWith("/api/chat/attachment/att-doc");
    expect(document.body.querySelector("a.md-preview__download")).toBeTruthy();
    blob.unmount();

    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="Mira"
          source="# body"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    expect(
      Array.from(document.body.querySelectorAll(".md-preview__actions > *")).map((el) =>
        el.getAttribute("aria-label"),
      ),
    ).toEqual(["關閉預覽"]);
  });

  it.each([
    ["text/html", "pr-702.html", "allow-scripts allow-popups allow-popups-to-escape-sandbox"],
    ["application/pdf", "spec.pdf", null],
  ])("shows a %s attachment as its share-link page in a frame filling the body, loading until the link is minted, without fetching its bytes", async (mime, title, sandbox) => {
    let mint!: (href: string) => void;
    vi.spyOn(api, "getChatAttachmentShareLink").mockImplementation(
      () => new Promise((resolve) => (mint = resolve)),
    );
    globalThis.fetch = vi.fn() as unknown as typeof fetch;
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title={title}
          url="/api/chat/attachment/att-html"
          attachmentId="att-html"
          mime={mime}
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    expect(document.body.querySelector(".md-preview__body")?.textContent).toBe("載入預覽中…");
    expect(document.body.querySelector("iframe")).toBeNull();

    await act(async () => mint("/api/chat/attachment/att-html?sig=s1"));
    const frame = document.body.querySelector("iframe")!;
    expect({
      className: frame.className,
      src: frame.getAttribute("src"),
      sandbox: frame.getAttribute("sandbox"),
      referrerPolicy: frame.getAttribute("referrerpolicy"),
      title: frame.getAttribute("title"),
    }).toEqual({
      className: "md-preview__frame",
      src: `${window.location.origin}/api/chat/attachment/att-html?sig=s1`,
      sandbox,
      referrerPolicy: "no-referrer",
      title,
    });
    expect(document.body.querySelector(".md-preview__body--frame")).not.toBeNull();
    expect(document.body.querySelector(".md-preview__panel--frame")).not.toBeNull();
    expect(document.body.querySelector(".md-preview__status")).toBeNull();
    expect(screen.getByRole("link", { name: "在新頁面顯示" }).getAttribute("href")).toBe(
      `${window.location.origin}/api/chat/attachment/att-html?sig=s1`,
    );
    expect(document.body.querySelector("a.md-preview__download")?.getAttribute("download")).toBe(title);
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("shows an honest error state on a failed fetch (never a blank render)", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: false,
      status: 404,
    })) as unknown as typeof fetch;
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
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# x",
    })) as unknown as typeof fetch;
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
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "x" })).toBeTruthy(),
    );
    fireEvent.click(screen.getByLabelText("關閉預覽"));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  describe("comment mode", () => {
    const DOC =
      "# 週報\n\n第一段要修改的內容。\n\n第二段保持原樣。\n\n" +
      "![流程圖](/api/chat/attachment/att-dddddddddddd)\n\n" +
      "![](https://cdn.example.com/charts/q3-revenue.png)";
    const MIRA = { id: "m-mira", name: "Mira", roster: "active" as const };

    beforeEach(() => {
      localStorage.clear();
      Range.prototype.getBoundingClientRect = () => new DOMRect(40, 120, 200, 20);
      Range.prototype.getClientRects = function (this: Range) {
        return [this.getBoundingClientRect()] as unknown as DOMRectList;
      };
      globalThis.fetch = vi.fn(async () => ({
        ok: true,
        text: async () => DOC,
      })) as unknown as typeof fetch;
    });

    function renderDoc(props: {
      docComments?: Parameters<typeof MarkdownPreviewOverlay>[0]["docComments"];
      readOnly?: boolean;
      onClose?: () => void;
      title?: string;
      mime?: string;
    } = {}) {
      return render(
        <I18nProvider>
          <MarkdownPreviewOverlay
            title={props.title ?? "weekly.md"}
            url="/api/chat/attachment/att-aaaaaaaaaaaa"
            attachmentId="att-aaaaaaaaaaaa"
            mime={props.mime}
            docComments={props.readOnly ? undefined : (props.docComments ?? { recipient: MIRA })}
            onClose={props.onClose ?? (() => {})}
          />
        </I18nProvider>,
      );
    }

    async function openComments() {
      await waitFor(() => expect(screen.getByRole("heading", { name: "週報" })).toBeTruthy());
      fireEvent.click(screen.getByRole("button", { name: "註解" }));
    }

    function docImage(alt: string) {
      return [...document.body.querySelectorAll<HTMLImageElement>(".md-preview__md img")].find(
        (img) => img.alt === alt,
      )!;
    }

    function addImageComment(alt: string, comment: string) {
      fireEvent.click(docImage(alt));
      fireEvent.click(screen.getByRole("button", { name: "加註解" }));
      fireEvent.change(screen.getByRole("textbox", { name: "註解內容" }), {
        target: { value: comment },
      });
      fireEvent.click(screen.getByRole("button", { name: "儲存" }));
    }

    function commentedImages() {
      return [...document.body.querySelectorAll("img[data-doc-comment]")].map((img) => img.getAttribute("alt"));
    }

    function selectText(text: string) {
      const walker = document.createTreeWalker(
        document.body.querySelector(".md-preview__body")!,
        NodeFilter.SHOW_TEXT,
      );
      for (let n = walker.nextNode(); n; n = walker.nextNode()) {
        const at = (n as Text).data.indexOf(text);
        if (at < 0) continue;
        const range = document.createRange();
        range.setStart(n, at);
        range.setEnd(n, at + text.length);
        const sel = document.getSelection()!;
        sel.removeAllRanges();
        sel.addRange(range);
        fireEvent.mouseUp(document);
        return;
      }
      throw new Error(`text not in document: ${text}`);
    }

    function addComment(excerpt: string, comment: string) {
      selectText(excerpt);
      fireEvent.click(screen.getByRole("button", { name: "加註解" }));
      fireEvent.change(screen.getByRole("textbox", { name: "註解內容" }), {
        target: { value: comment },
      });
      fireEvent.click(screen.getByRole("button", { name: "儲存" }));
    }

    function listed() {
      return screen.getAllByTestId("doc-comment").map((el) => el.textContent);
    }

    const realScrollIntoView = Element.prototype.scrollIntoView;
    afterEach(() => {
      Element.prototype.scrollIntoView = realScrollIntoView;
    });

    function bubbles() {
      return [...document.body.querySelectorAll<HTMLButtonElement>(".md-preview__body .doc-comments__bubble")];
    }

    function bubbleInfo() {
      return bubbles().map((b) => ({
        name: b.getAttribute("aria-label"),
        count: b.dataset.count ?? null,
        image: b.classList.contains("doc-comments__bubble--image"),
      }));
    }

    it("offers the 註解 toggle on markdown, plain-text and JSON attachments, and not on an image attachment or a message body", async () => {
      const doc = renderDoc();
      await waitFor(() => expect(screen.getByRole("heading", { name: "週報" })).toBeTruthy());
      expect(screen.getByRole("button", { name: "註解" }).getAttribute("aria-pressed")).toBe("false");
      doc.unmount();

      for (const [title, mime] of [
        ["notes.txt", "text/plain"],
        ["run.log", "text/plain"],
        ["report.json", "application/json"],
      ]) {
        const plain = renderDoc({ title, mime });
        await waitFor(() => expect(document.body.querySelector("pre.md-preview__text")).toBeTruthy());
        expect(screen.getByRole("button", { name: "註解" })).toBeTruthy();
        plain.unmount();
      }

      const picture = renderDoc({ title: "shot.png", mime: "image/png" });
      expect(document.body.querySelector(".md-preview__image-viewport img")).toBeTruthy();
      expect(screen.queryByRole("button", { name: "註解" })).toBeNull();
      picture.unmount();

      render(
        <I18nProvider>
          <MarkdownPreviewOverlay title="Mira" source={DOC} onClose={() => {}} />
        </I18nProvider>,
      );
      expect(screen.getByRole("heading", { name: "週報" })).toBeTruthy();
      expect(screen.queryByRole("button", { name: "註解" })).toBeNull();
    });

    it("adds, edits and deletes comments on a selected excerpt or a clicked image, outlining the commented image", async () => {
      const onClose = vi.fn();
      renderDoc({ onClose });
      await waitFor(() => expect(screen.getByRole("heading", { name: "週報" })).toBeTruthy());
      fireEvent.click(docImage("流程圖"));
      expect(screen.queryByRole("button", { name: "加註解" })).toBeNull();
      await openComments();
      expect(screen.queryByRole("button", { name: "加註解" })).toBeNull();

      fireEvent.click(docImage("流程圖"));
      expect(screen.getByRole("button", { name: "加註解" })).toBeTruthy();
      fireEvent.click(screen.getByText("第二段保持原樣。"));
      expect(screen.queryByRole("button", { name: "加註解" })).toBeNull();

      addComment("第一段要修改", "改成條列");
      addComment("第二段保持原樣", "這段可以");
      addImageComment("流程圖", "換成新版");
      addImageComment("", "數字對不上");
      expect(listed()).toEqual([
        "第一段要修改1. 改成條列",
        "第二段保持原樣2. 這段可以",
        "圖片：流程圖3. 換成新版",
        "圖片：q3-revenue.png4. 數字對不上",
      ]);
      expect(commentedImages()).toEqual(["流程圖", ""]);
      expect(bubbleInfo()).toEqual([
        { name: "前往註解：第一段要修改", count: null, image: false },
        { name: "前往註解：第二段保持原樣", count: null, image: false },
        { name: "前往註解：圖片：流程圖", count: null, image: true },
        { name: "前往註解：圖片：q3-revenue.png", count: null, image: true },
      ]);

      fireEvent.click(screen.getAllByRole("button", { name: "編輯註解" })[2]);
      fireEvent.change(screen.getByRole("textbox", { name: "註解內容" }), {
        target: { value: "換成 v2 流程" },
      });
      fireEvent.click(screen.getByRole("button", { name: "儲存" }));
      fireEvent.click(screen.getAllByRole("button", { name: "刪除註解" })[3]);
      fireEvent.click(screen.getAllByRole("button", { name: "刪除註解" })[1]);
      expect(listed()).toEqual(["第一段要修改1. 改成條列", "圖片：流程圖2. 換成 v2 流程"]);
      expect(commentedImages()).toEqual(["流程圖"]);
      fireEvent.click(screen.getAllByRole("button", { name: "刪除註解" })[1]);
      expect(commentedImages()).toEqual([]);
      addComment("第二段保持原樣", "這段可以");
      expect(listed()).toEqual(["第一段要修改1. 改成條列", "第二段保持原樣2. 這段可以"]);

      fireEvent.click(screen.getAllByRole("button", { name: "編輯註解" })[0]);
      fireEvent.change(screen.getByRole("textbox", { name: "註解內容" }), {
        target: { value: "改成三點條列" },
      });
      fireEvent.click(screen.getByRole("button", { name: "儲存" }));
      expect(listed()).toEqual(["第一段要修改1. 改成三點條列", "第二段保持原樣2. 這段可以"]);

      fireEvent.click(screen.getAllByRole("button", { name: "編輯註解" })[1]);
      fireEvent.keyDown(screen.getByRole("textbox", { name: "註解內容" }), { key: "Escape" });
      expect(screen.queryByRole("textbox", { name: "註解內容" })).toBeNull();
      expect(onClose).not.toHaveBeenCalled();

      fireEvent.click(screen.getAllByRole("button", { name: "刪除註解" })[1]);
      expect(listed()).toEqual(["第一段要修改1. 改成三點條列"]);
      expect(bubbleInfo()).toEqual([{ name: "前往註解：第一段要修改", count: null, image: false }]);

      addComment("第一段要修改", "再加一句");
      expect(bubbleInfo()).toEqual([{ name: "2 則註解", count: "2", image: false }]);
    });

    it("jumps from a bubble to its passage's first comment, and from a listed comment to its passage or image, marking each briefly", async () => {
      const scrolled: Element[] = [];
      Element.prototype.scrollIntoView = function (this: Element) {
        scrolled.push(this);
      };
      const registry = new Map<string, { ranges: Range[] }>();
      vi.stubGlobal("CSS", { highlights: registry });
      vi.stubGlobal(
        "Highlight",
        class {
          ranges: Range[];
          constructor(...ranges: Range[]) {
            this.ranges = ranges;
          }
        },
      );
      try {
        renderDoc();
        await openComments();
        addComment("第二段保持原樣", "這段可以");
        addComment("第一段要修改", "改成條列");
        addComment("第一段要修改", "再加一句");
        addImageComment("流程圖", "換成新版");
        const items = screen.getAllByTestId("doc-comment");
        const flashed = () => items.map((el) => el.classList.contains("doc-comments__item--flash"));
        const goto = (i: number) => items[i].querySelector<HTMLElement>(".doc-comments__goto")!;
        vi.useFakeTimers();
        const elapse = (ms: number) => act(() => vi.advanceTimersByTime(ms));

        fireEvent.click(screen.getByRole("button", { name: "2 則註解" }));
        expect(flashed()).toEqual([false, true, false, false]);
        expect(scrolled).toEqual([items[1]]);
        expect(document.activeElement).toBe(goto(1));
        elapse(1499);
        expect(flashed()).toEqual([false, true, false, false]);
        elapse(1);
        expect(flashed()).toEqual([false, false, false, false]);

        fireEvent.click(bubbles().find((b) => b.classList.contains("doc-comments__bubble--image"))!);
        expect(flashed()).toEqual([false, false, false, true]);
        expect(document.activeElement).toBe(goto(3));

        scrolled.length = 0;
        fireEvent.click(goto(0));
        expect(registry.get("oc-doc-comment-focus")?.ranges.map((r) => r.toString())).toEqual(["第二段保持原樣"]);
        expect(scrolled.map((el) => el.textContent)).toEqual(["第二段保持原樣。"]);
        elapse(1499);
        expect(registry.has("oc-doc-comment-focus")).toBe(true);
        elapse(1);
        expect(registry.has("oc-doc-comment-focus")).toBe(false);

        fireEvent.click(goto(3));
        expect(docImage("流程圖").hasAttribute("data-doc-comment-focus")).toBe(true);
        expect(scrolled.at(-1)).toBe(docImage("流程圖"));
        elapse(1500);
        expect(docImage("流程圖").hasAttribute("data-doc-comment-focus")).toBe(false);
      } finally {
        vi.useRealTimers();
        vi.unstubAllGlobals();
      }
    });

    it("expands a collapsed comment list before showing the comment a bubble points at, or the box for a new comment", async () => {
      Element.prototype.scrollIntoView = vi.fn();
      renderDoc();
      await openComments();
      addComment("第一段要修改", "改成條列");
      const panel = document.body.querySelector(".doc-comments")!;
      fireEvent.click(screen.getByRole("button", { name: "收合註解清單" }));
      expect(panel.classList.contains("doc-comments--collapsed")).toBe(true);
      expect(screen.getByRole("button", { name: "展開註解清單" }).getAttribute("aria-expanded")).toBe("false");

      const toggle = screen.getByRole("button", { name: "展開註解清單" });
      expect(document.getElementById(toggle.getAttribute("aria-controls")!)).toBe(
        panel.querySelector(".doc-comments__list"),
      );

      fireEvent.click(screen.getByRole("button", { name: "前往註解：第一段要修改" }));
      expect(panel.classList.contains("doc-comments--collapsed")).toBe(false);
      expect(screen.getByRole("button", { name: "收合註解清單" }).getAttribute("aria-expanded")).toBe("true");
      expect(screen.getByTestId("doc-comment").classList.contains("doc-comments__item--flash")).toBe(true);
      expect(Element.prototype.scrollIntoView).toHaveBeenCalledTimes(1);

      fireEvent.click(screen.getByRole("button", { name: "收合註解清單" }));
      selectText("第二段保持原樣");
      fireEvent.click(screen.getByRole("button", { name: "加註解" }));
      expect(panel.classList.contains("doc-comments--collapsed")).toBe(false);
      expect(document.activeElement).toBe(screen.getByRole("textbox", { name: "註解內容" }));
    });

    it("restores the unsent comments after the document is closed and reopened, with bubbles only in comment mode", async () => {
      const first = renderDoc();
      await openComments();
      addComment("第一段要修改", "改成條列");
      first.unmount();

      const second = renderDoc();
      await waitFor(() => expect(screen.getByRole("heading", { name: "週報" })).toBeTruthy());
      expect(bubbles()).toEqual([]);
      fireEvent.click(screen.getByRole("button", { name: "註解" }));
      expect(listed()).toEqual(["第一段要修改1. 改成條列"]);
      expect(bubbleInfo()).toEqual([{ name: "前往註解：第一段要修改", count: null, image: false }]);
      second.unmount();

      renderDoc({ readOnly: true });
      await waitFor(() => expect(screen.getByRole("heading", { name: "週報" })).toBeTruthy());
      expect(screen.queryByRole("button", { name: "註解" })).toBeNull();
      expect(bubbles()).toEqual([]);
    });

    it("restores text drafts stored before image comments existed alongside image, place and whole-document comments, marking what the document no longer contains and dropping malformed entries", async () => {
      localStorage.setItem(
        "oc_doc_comments:att-aaaaaaaaaaaa",
        JSON.stringify([
          { id: "c1", excerpt: "第一段要修改", comment: "改" },
          { id: "c2", excerpt: "已被刪掉的句子", comment: "留著" },
          { id: "c3", kind: "image", alt: "流程圖", src: "/api/chat/attachment/att-dddddddddddd", comment: "換圖" },
          { id: "c4", kind: "image", alt: "舊圖", src: "/api/chat/attachment/att-eeeeeeeeeeee", comment: "刪掉" },
          { id: "c5", kind: "image", alt: "沒有位址", comment: "壞的" },
          { id: "c6", kind: "video", excerpt: "x", comment: "壞的" },
          { id: "c7", kind: "anchor", anchorId: "ac-AC-5", label: "AC-5", comment: "量法" },
          { id: "c8", kind: "anchor", anchorId: "ac-AC-6", comment: "壞的" },
          { id: "c9", kind: "document", comment: "字太小" },
        ]),
      );
      renderDoc();
      await openComments();
      expect(listed()).toEqual([
        "第一段要修改1. 改",
        "已被刪掉的句子文件中找不到這段原文，註解仍會送出。2. 留著",
        "圖片：流程圖3. 換圖",
        "圖片：舊圖文件中找不到這張圖片，註解仍會送出。4. 刪掉",
        "位置：AC-5（#ac-AC-5）5. 量法",
        "整份文件6. 字太小",
      ]);
      expect(commentedImages()).toEqual(["流程圖"]);
    });

    it("sends an image comment as a quote naming the image with a host-less link to its own address, never the owner's token", async () => {
      localStorage.setItem("oc_token", "tok-aaaaaaaaaaaa");
      localStorage.setItem(
        "oc_doc_comments:att-aaaaaaaaaaaa",
        JSON.stringify([
          { id: "c1", kind: "image", alt: "a]b [v2]", src: "/api/docs/assets/my chart (1).png", comment: "這張重畫" },
        ]),
      );
      const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
      renderDoc({ onClose: () => {} });
      await openComments();
      expect(docImage("流程圖").getAttribute("src")).toBe(
        "/api/chat/attachment/att-dddddddddddd?token=tok-aaaaaaaaaaaa",
      );
      addImageComment("流程圖", "標上單位");
      addImageComment("", "來源寫出來");
      expect(commentedImages()).toEqual(["流程圖", ""]);

      fireEvent.click(screen.getByRole("button", { name: "送出給 AI" }));
      await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
        const body = post.mock.calls[0][0].body;
      expect(body).toBe(
        "**文件註解**：[weekly.md](/api/chat/attachment/att-aaaaaaaaaaaa)\n" +
          "\n" +
          "1. 原文：\n" +
          "   > 圖片：[a\\]b \\[v2\\]](/api/docs/assets/my%20chart%20%281%29.png)\n" +
          "\n" +
          "   註解：這張重畫\n" +
          "\n" +
          "2. 原文：\n" +
          "   > 圖片：[流程圖](/api/chat/attachment/att-dddddddddddd)\n" +
          "\n" +
          "   註解：標上單位\n" +
          "\n" +
          "3. 原文：\n" +
          "   > 圖片：[q3-revenue.png](https://cdn.example.com/charts/q3-revenue.png)\n" +
          "\n" +
          "   註解：來源寫出來",
      );

      const bubble = render(<Markdown source={body} breaks />).container;
      expect([...bubble.querySelectorAll("ol > li > blockquote")].map((q) => q.textContent)).toEqual([
        "圖片：a]b [v2]",
        "圖片：流程圖",
        "圖片：q3-revenue.png",
      ]);
      expect([...bubble.querySelectorAll("blockquote a")].map((a) => a.getAttribute("href"))).toEqual([
        "/api/docs/assets/my%20chart%20%281%29.png",
        "/api/chat/attachment/att-dddddddddddd",
        "https://cdn.example.com/charts/q3-revenue.png",
      ]);
    });

    it("comments on a passage of a JSON attachment's indented view and sends it as a multi-line quote", async () => {
      globalThis.fetch = vi.fn(async () => ({
        ok: true,
        text: async () => '{"name":"Mira","items":[1,2]}',
      })) as unknown as typeof fetch;
      const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
      renderDoc({ title: "report.json", mime: "application/json" });
      await waitFor(() => expect(document.body.querySelector("pre.md-preview__text")).toBeTruthy());
      fireEvent.click(screen.getByRole("button", { name: "註解" }));
      expect(screen.getByText("在文件中選取一段文字，再按「加註解」。")).toBeTruthy();
      addComment('"name": "Mira",\n  "items"', "items 改名");
      fireEvent.click(screen.getByRole("button", { name: "對整份文件加註解" }));
      fireEvent.change(screen.getByRole("textbox", { name: "註解內容" }), { target: { value: "欄位排序" } });
      fireEvent.click(screen.getByRole("button", { name: "儲存" }));
      expect(listed()).toEqual(['"name": "Mira",\n  "items"1. items 改名', "整份文件2. 欄位排序"]);

      fireEvent.click(screen.getByRole("button", { name: "送出給 AI" }));
      await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
      expect(post.mock.calls[0][0].body).toBe(
        "**文件註解**：[report.json](/api/chat/attachment/att-aaaaaaaaaaaa)\n" +
          "\n" +
          "1. 原文：\n" +
          '   > "name": "Mira",\n' +
          '   > "items"\n' +
          "\n" +
          "   註解：items 改名\n" +
          "\n" +
          "2. 原文：\n" +
          "   > 整份文件\n" +
          "\n" +
          "   註解：欄位排序",
      );
    });

    it("sends every comment as one message to the sender and closes, clearing the draft", async () => {
      localStorage.setItem(
        "oc_doc_comments:att-aaaaaaaaaaaa",
        JSON.stringify([
          { id: "c1", excerpt: "第一段要修改的內容。\n\n第二段保持原樣。", comment: "兩段合併\n並縮短" },
          { id: "c2", excerpt: "週報_v2", comment: "標題加日期" },
        ]),
      );
      const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
      const onClose = vi.fn();
      renderDoc({ onClose });
      await openComments();
      expect(screen.getByText("Mira")).toBeTruthy();

      fireEvent.click(screen.getByRole("button", { name: "送出給 AI" }));
      await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
      expect(post.mock.calls).toEqual([
        [
          {
            to: "m-mira",
            body:
              "**文件註解**：[weekly.md](/api/chat/attachment/att-aaaaaaaaaaaa)\n" +
              "\n" +
              "1. 原文：\n" +
              "   > 第一段要修改的內容。\n" +
              "   >\n" +
              "   > 第二段保持原樣。\n" +
              "\n" +
              "   註解：兩段合併\n" +
              "   並縮短\n" +
              "\n" +
              "2. 原文：\n" +
              "   > 週報_v2\n" +
              "\n" +
              "   註解：標題加日期",
          },
        ],
      ]);
      expect(localStorage.getItem("oc_doc_comments:att-aaaaaaaaaaaa")).toBeNull();

      const bubble = render(<Markdown source={post.mock.calls[0][0].body} breaks />).container;
      expect([...bubble.querySelectorAll("ol > li > blockquote")].map((q) => q.textContent)).toEqual([
        "第一段要修改的內容。第二段保持原樣。",
        "週報_v2",
      ]);
      expect(bubble.querySelector("a")!.getAttribute("href")).toBe("/api/chat/attachment/att-aaaaaaaaaaaa");
    });

    it("after a send, keeps a comment another open copy of the document added while the send was in flight", async () => {
      localStorage.setItem(
        "oc_doc_comments:att-aaaaaaaaaaaa",
        JSON.stringify([{ id: "c1", excerpt: "週報", comment: "標題加日期" }]),
      );
      const added = { id: "c2", excerpt: "第二段保持原樣。", comment: "這段也改" };
      vi.spyOn(api, "postChat").mockImplementation(async () => {
        localStorage.setItem(
          "oc_doc_comments:att-aaaaaaaaaaaa",
          JSON.stringify([{ id: "c1", excerpt: "週報", comment: "標題加日期" }, added]),
        );
      });
      const onClose = vi.fn();
      renderDoc({ onClose });
      await openComments();

      fireEvent.click(screen.getByRole("button", { name: "送出給 AI" }));
      await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
      expect(JSON.parse(localStorage.getItem("oc_doc_comments:att-aaaaaaaaaaaa")!)).toEqual([
        { id: "c2", excerpt: "第二段保持原樣。", comment: "這段也改" },
      ]);
    });

    it("keeps comment lines that look like an excerpt or a numbered item, and link syntax in the filename, as plain text in the sent message", async () => {
      localStorage.setItem(
        "oc_doc_comments:att-aaaaaaaaaaaa",
        JSON.stringify([{ id: "c1", excerpt: "週報", comment: "line a\n> 偽造原文\n2. 原文：\n- 項目" }]),
      );
      const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
      renderDoc({ title: "a](javascript:alert(1)) [b", onClose: () => {} });
      await openComments();

      fireEvent.click(screen.getByRole("button", { name: "送出給 AI" }));
      await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
      const body = post.mock.calls[0][0].body;
      expect(body).toBe(
        "**文件註解**：[a\\](javascript:alert(1)) \\[b](/api/chat/attachment/att-aaaaaaaaaaaa)\n" +
          "\n" +
          "1. 原文：\n" +
          "   > 週報\n" +
          "\n" +
          "   註解：line a\n" +
          "   \\> 偽造原文\n" +
          "   2\\. 原文：\n" +
          "   \\- 項目",
      );

      const bubble = render(<Markdown source={body} breaks />).container;
      expect(bubble.querySelectorAll("blockquote").length).toBe(1);
      expect(bubble.querySelectorAll("ol, ul").length).toBe(1);
      expect(bubble.querySelectorAll("ol > li").length).toBe(1);
      expect(bubble.querySelector("ol > li > p")!.textContent).toBe("註解：line a> 偽造原文2. 原文：- 項目");
      const links = bubble.querySelectorAll("a");
      expect(links.length).toBe(1);
      expect(links[0].getAttribute("href")).toBe("/api/chat/attachment/att-aaaaaaaaaaaa");
      expect(links[0].textContent).toBe("a](javascript:alert(1)) [b");
    });

    it("names the task and the artifact when the document is a task artifact", async () => {
      const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
      render(
        <I18nProvider>
          <MarkdownPreviewOverlay
            title="週報"
            filename="weekly-0930.md"
            url="/api/chat/attachment/att-bbbbbbbbbbbb"
            attachmentId="att-bbbbbbbbbbbb"
            docComments={{
              recipient: { id: "ow-cccccccccccc", name: "外包 阿青", roster: "active" },
              task: { id: "T-12", taskNo: "T-12", artifactName: "週報" },
            }}
            onClose={() => {}}
          />
        </I18nProvider>,
      );
      await openComments();
      addComment("第二段保持原樣", "刪掉");
      fireEvent.click(screen.getByRole("button", { name: "送出給 AI" }));
      await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
      expect(post.mock.calls[0][0]).toEqual({
        to: "ow-cccccccccccc",
        body:
          "**文件註解**：[weekly-0930.md](/api/chat/attachment/att-bbbbbbbbbbbb)\n" +
          "任務 [T-12](/#tasks/T-12) 產物「週報」\n" +
          "\n" +
          "1. 原文：\n" +
          "   > 第二段保持原樣\n" +
          "\n" +
          "   註解：刪掉",
      });
    });

    it("sends a same-origin image's own query and a protocol-relative image's loaded path, without the owner's token", async () => {
      localStorage.setItem("oc_token", "tok-aaaaaaaaaaaa");
      globalThis.fetch = vi.fn(async () => ({
        ok: true,
        text: async () => "# 週報\n\n![版本圖](/x.png?v=2)\n\n![外站圖](//cdn.example.com/y.png)",
      })) as unknown as typeof fetch;
      const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
      renderDoc();
      await openComments();
      expect([docImage("版本圖").getAttribute("src"), docImage("外站圖").getAttribute("src")]).toEqual([
        "/x.png?v=2&token=tok-aaaaaaaaaaaa",
        "/y.png?token=tok-aaaaaaaaaaaa",
      ]);
      addImageComment("版本圖", "換新版");
      addImageComment("外站圖", "這張不要");
      fireEvent.click(screen.getByRole("button", { name: "送出給 AI" }));
      await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
      expect(post.mock.calls[0][0].body).toBe(
        "**文件註解**：[weekly.md](/api/chat/attachment/att-aaaaaaaaaaaa)\n" +
          "\n" +
          "1. 原文：\n" +
          "   > 圖片：[版本圖](/x.png?v=2)\n" +
          "\n" +
          "   註解：換新版\n" +
          "\n" +
          "2. 原文：\n" +
          "   > 圖片：[外站圖](/y.png)\n" +
          "\n" +
          "   註解：這張不要",
      );
    });

    it("after paging to another attachment, shows and saves that attachment's own draft and leaves the first one's alone", async () => {
      const draftA = [{ id: "a1", excerpt: "週報", comment: "A 的註解" }];
      localStorage.setItem("oc_doc_comments:att-aaaaaaaaaaaa", JSON.stringify(draftA));
      localStorage.setItem(
        "oc_doc_comments:att-bbbbbbbbbbbb",
        JSON.stringify([{ id: "b1", excerpt: "第一段要修改", comment: "B 的註解" }]),
      );
      const overlay = (id: string, title: string) => (
        <I18nProvider>
          <MarkdownPreviewOverlay
            title={title}
            url={`/api/chat/attachment/${id}`}
            attachmentId={id}
            docComments={{ recipient: MIRA }}
            pager={{ index: id === "att-aaaaaaaaaaaa" ? 0 : 1, total: 2, onGo: () => {} }}
            onClose={() => {}}
          />
        </I18nProvider>
      );
      const view = render(overlay("att-aaaaaaaaaaaa", "a.md"));
      await openComments();
      expect(listed()).toEqual(["週報1. A 的註解"]);

      view.rerender(overlay("att-bbbbbbbbbbbb", "b.md"));
      await waitFor(() => expect(listed()).toEqual(["第一段要修改1. B 的註解"]));
      addComment("第二段保持原樣", "B 再一則");
      expect(listed()).toEqual(["第一段要修改1. B 的註解", "第二段保持原樣2. B 再一則"]);
      expect(JSON.parse(localStorage.getItem("oc_doc_comments:att-aaaaaaaaaaaa")!)).toEqual(draftA);
      expect(
        JSON.parse(localStorage.getItem("oc_doc_comments:att-bbbbbbbbbbbb")!).map((c: { comment: string }) => c.comment),
      ).toEqual(["B 的註解", "B 再一則"]);
    });

    it("keeps the draft and says so when sending fails", async () => {
      vi.spyOn(api, "postChat").mockRejectedValue(new Error("http 500"));
      const onClose = vi.fn();
      renderDoc({ onClose });
      await openComments();
      addComment("第一段要修改", "改成條列");
      fireEvent.click(screen.getByRole("button", { name: "送出給 AI" }));
      await waitFor(() =>
        expect(screen.getByRole("alert").textContent).toBe("送出失敗，註解已保留（http 500）"),
      );
      expect(onClose).not.toHaveBeenCalled();
      expect(listed()).toEqual(["第一段要修改1. 改成條列"]);
      expect(JSON.parse(localStorage.getItem("oc_doc_comments:att-aaaaaaaaaaaa")!)).toHaveLength(1);
    });

    it("disables sending to a recipient who left the roster, with the reason", async () => {
      const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
      renderDoc({ docComments: { recipient: { ...MIRA, roster: "left" } } });
      await openComments();
      addComment("第一段要修改", "改成條列");
      const send = screen.getByRole("button", { name: "送出給 AI" }) as HTMLButtonElement;
      expect(send.disabled).toBe(true);
      expect(screen.getByText("對方已不在名冊上（已移除或已釋出），無法送出。")).toBeTruthy();
      fireEvent.click(send);
      expect(post).not.toHaveBeenCalled();
    });

    it("keeps send disabled without calling anyone departed while the roster is still loading", async () => {
      renderDoc({ docComments: { recipient: { ...MIRA, roster: "pending" } } });
      await openComments();
      addComment("第一段要修改", "改成條列");
      expect((screen.getByRole("button", { name: "送出給 AI" }) as HTMLButtonElement).disabled).toBe(true);
      expect(screen.queryByText("對方已不在名冊上（已移除或已釋出），無法送出。")).toBeNull();
      expect(screen.getByText("Mira")).toBeTruthy();
    });

    it("enables sending to an active recipient once a comment exists", async () => {
      renderDoc();
      await openComments();
      expect(screen.getByText("在文件中選取一段文字或點一張圖片，再按「加註解」。")).toBeTruthy();
      const send = screen.getByRole("button", { name: "送出給 AI" }) as HTMLButtonElement;
      expect(send.disabled).toBe(true);
      addComment("第一段要修改", "改成條列");
      expect(send.disabled).toBe(false);
      expect(screen.queryByText("對方已不在名冊上（已移除或已釋出），無法送出。")).toBeNull();
    });

    describe("on an HTML page", () => {
      const OC = { oc: "doc-comments", v: 1 } as const;

      async function renderPage(props: Parameters<typeof renderDoc>[0] = {}) {
        vi.spyOn(api, "getChatAttachmentShareLink").mockResolvedValue(
          "/api/chat/attachment/att-aaaaaaaaaaaa?sig=s1",
        );
        renderDoc({ title: "pr-702.html", mime: "text/html", ...props });
        await waitFor(() => expect(document.body.querySelector("iframe.md-preview__frame")).toBeTruthy());
        const frame = document.body.querySelector<HTMLIFrameElement>("iframe.md-preview__frame")!;
        const posted: unknown[] = [];
        frame.contentWindow!.postMessage = ((m: unknown) => posted.push(m)) as Window["postMessage"];
        const fromPage = (data: unknown, source: Window | null = frame.contentWindow) =>
          act(() => {
            window.dispatchEvent(new MessageEvent("message", { data, source }));
          });
        const toggle = () => fireEvent.click(screen.getByRole("button", { name: "註解" }));
        return { frame, posted, fromPage, toggle };
      }

      const pick = (id: unknown, label?: unknown) => ({ ...OC, type: "pick", anchor: { id, label } });

      function saveComposed(comment: string) {
        fireEvent.change(screen.getByRole("textbox", { name: "註解內容" }), { target: { value: comment } });
        fireEvent.click(screen.getByRole("button", { name: "儲存" }));
      }

      it("turns comment mode on in the page only once it said ready, offering only whole-document comments until then, and off again when comment mode closes", async () => {
        const { posted, fromPage, toggle } = await renderPage();
        const empty = () => document.body.querySelector(".doc-comments__empty")?.textContent;
        fromPage({ ...OC, type: "ready" }, window);
        toggle();
        expect(posted).toEqual([]);
        expect(empty()).toBe("這個頁面只能按「對整份文件加註解」。");
        expect(document.body.querySelector(".doc-comments__bubbles")).toBeNull();

        fromPage({ ...OC, type: "ready" });
        expect(posted).toEqual([
          { ...OC, type: "mode", on: true },
          { ...OC, type: "commented", ids: [] },
        ]);
        expect(empty()).toBe(
          "在頁面上選取一段文字再按「加註解」、點可註解的位置（滑過時有外框），或按「對整份文件加註解」。",
        );
        posted.length = 0;
        toggle();
        expect(posted).toEqual([{ ...OC, type: "mode", on: false }]);

        posted.length = 0;
        fromPage({ ...OC, type: "ready" });
        expect(posted).toEqual([]);
        toggle();
        expect(posted).toEqual([
          { ...OC, type: "mode", on: true },
          { ...OC, type: "commented", ids: [] },
        ]);
        expect(globalThis.fetch).not.toHaveBeenCalled();
      });

      it("starts a comment on the place the page reports picked in comment mode, ignoring another window, a malformed or oversize pick, any pick outside comment mode and any pick while a comment is half written", async () => {
        const { fromPage, toggle } = await renderPage();
        const composing = () => screen.queryByRole("textbox", { name: "註解內容" });
        fromPage(pick("ac-AC-1", "AC-1"));
        toggle();
        expect(composing()).toBeNull();

        fromPage(pick("ac-AC-1", "AC-1"), window);
        fromPage({ ...pick("ac-AC-1", "AC-1"), v: 2 });
        fromPage({ ...pick("ac-AC-1", "AC-1"), oc: "other" });
        fromPage({ ...OC, type: "picked", anchor: { id: "ac-AC-1" } });
        fromPage(pick(""));
        fromPage(pick(42, "AC-1"));
        fromPage(pick("x".repeat(129)));
        fromPage(pick("ac-AC-1", "y".repeat(201)));
        fromPage(pick("ac-AC-1", 7));
        fromPage("pick");
        expect(composing()).toBeNull();

        fromPage(pick("ac-AC-2"));
        expect(document.body.querySelector(".doc-comments__item--compose .doc-comments__excerpt")?.textContent).toBe(
          "位置：ac-AC-2（#ac-AC-2）",
        );
        saveComposed("補一條測試");
        fromPage(pick("ac-AC-3", "<b>AC-3</b> 離線"));
        saveComposed("看不懂");
        fromPage(pick("ac-AC-4"));
        fireEvent.change(screen.getByRole("textbox", { name: "註解內容" }), { target: { value: "寫到一半" } });
        fromPage(pick("ac-AC-5"));
        expect([
          document.body.querySelector(".doc-comments__item--compose .doc-comments__excerpt")?.textContent,
          (screen.getByRole("textbox", { name: "註解內容" }) as HTMLTextAreaElement).value,
        ]).toEqual(["位置：ac-AC-4（#ac-AC-4）", "寫到一半"]);
        fireEvent.click(screen.getByRole("button", { name: "儲存" }));
        expect(listed()).toEqual([
          "位置：ac-AC-2（#ac-AC-2）1. 補一條測試",
          "位置：<b>AC-3</b> 離線（#ac-AC-3）2. 看不懂",
          "位置：ac-AC-4（#ac-AC-4）3. 寫到一半",
        ]);
        expect(document.body.querySelector(".doc-comments b")).toBeNull();
      });

      it("offers 加註解 for the text the page reports selected once it said ready, and sends a comment quoting that text with markdown in it kept as plain text, ignoring another window, a malformed or oversize selection and a selection from before the page last said ready", async () => {
        const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
        const { fromPage, toggle } = await renderPage();
        toggle();
        const rect = { left: 10, top: 20, width: 80, height: 16 };
        const select = (text: unknown, r: unknown = rect) => ({ ...OC, type: "select", text, rect: r });
        const float = () => screen.queryByRole("button", { name: "加註解" });
        fromPage(select("在 GitHub 開啟 PR #702"));
        expect(float()).toBeNull();
        fromPage({ ...OC, type: "ready" });
        fromPage(select("在 GitHub 開啟 PR #702"));
        expect(float()).not.toBeNull();
        fromPage({ ...OC, type: "ready" });
        expect(float()).toBeNull();

        fromPage(select("在 GitHub 開啟 PR #702"), window);
        fromPage(select("在 GitHub 開啟 PR #702"), null);
        fromPage({ ...select("在 GitHub 開啟 PR #702"), v: 2 });
        fromPage(select(42));
        fromPage(select("x".repeat(4001)));
        fromPage(select("PR #702", null));
        fromPage(select("PR #702", { ...rect, width: -1 }));
        fromPage("select");
        expect(float()).toBeNull();

        fromPage(select("在 GitHub 開啟 PR #702"));
        expect(float()).not.toBeNull();
        fromPage(select(""));
        expect(float()).toBeNull();

        const hostile = "<b>粗</b> [x](https://evil.example) **b**\n```js\n# 標題\n開啟 PR #702";
        fromPage(select(`  ${hostile} `));
        fireEvent.click(float()!);
        expect(document.body.querySelector(".doc-comments__item--compose .doc-comments__excerpt")?.textContent).toBe(
          hostile,
        );
        expect(float()).toBeNull();
        saveComposed("這個連結打不開");
        expect(listed()).toEqual([`${hostile}1. 這個連結打不開`]);
        expect(document.body.querySelector(".doc-comments b")).toBeNull();

        fireEvent.click(screen.getByRole("button", { name: "送出給 AI" }));
        await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
        expect(post.mock.calls[0][0].body).toBe(
          "**文件註解**：[pr-702.html](/api/chat/attachment/att-aaaaaaaaaaaa)\n" +
            "\n" +
            "1. 原文：\n" +
            "   > \\<b\\>粗\\</b\\> \\[x\\](https\\://evil.example) \\*\\*b\\*\\*\n" +
            "   > \\`\\`\\`js\n" +
            "   > \\# 標題\n" +
            "   > 開啟 PR #702\n" +
            "\n" +
            "   註解：這個連結打不開",
        );
        const bubble = render(<Markdown source={post.mock.calls[0][0].body} breaks />).container;
        expect([...bubble.querySelectorAll("ol > li > blockquote")].map((q) => q.textContent)).toEqual([
          "<b>粗</b> [x](https://evil.example) **b**```js# 標題開啟 PR #702",
        ]);
        expect([...bubble.querySelectorAll("blockquote *")].map((el) => el.outerHTML)).toEqual([
          "<p>&lt;b&gt;粗&lt;/b&gt; [x](https://evil.example) **b**<br>```js<br># 標題<br>開啟 PR #702</p>",
          "<br>",
          "<br>",
          "<br>",
        ]);
      });

      it("tells the page which places carry comments whenever the draft changes, and asks it to show the place of a clicked comment", async () => {
        localStorage.setItem(
          "oc_doc_comments:att-aaaaaaaaaaaa",
          JSON.stringify([{ id: "c1", kind: "anchor", anchorId: "ac-AC-1", label: "AC-1", comment: "舊的" }]),
        );
        const { posted, fromPage, toggle } = await renderPage();
        toggle();
        expect(document.body.querySelector(".doc-comments__goto")).toBeNull();
        fromPage({ ...OC, type: "ready" });
        const commented = () =>
          posted.flatMap((m) => ((m as { type: string }).type === "commented" ? [(m as { ids: string[] }).ids] : []));
        expect(commented()).toEqual([["ac-AC-1"]]);

        fromPage(pick("ac-AC-2", "AC-2"));
        saveComposed("新的");
        fromPage(pick("ac-AC-1", "AC-1"));
        saveComposed("再一則");
        fireEvent.click(screen.getByRole("button", { name: "對整份文件加註解" }));
        saveComposed("字太小");
        fireEvent.click(screen.getAllByRole("button", { name: "刪除註解" })[1]);
        expect(commented()).toEqual([
          ["ac-AC-1"],
          ["ac-AC-1", "ac-AC-2"],
          ["ac-AC-1", "ac-AC-2"],
          ["ac-AC-1", "ac-AC-2"],
          ["ac-AC-1"],
        ]);

        posted.length = 0;
        const items = screen.getAllByTestId("doc-comment");
        expect(items.map((el) => el.querySelector(".doc-comments__goto") !== null)).toEqual([true, true, false]);
        fireEvent.click(items[1].querySelector(".doc-comments__goto")!);
        expect(posted).toEqual([{ ...OC, type: "focus", id: "ac-AC-1" }]);
      });

      it("sends place and whole-document comments as quotes naming the place, with markdown, tags and URLs in the page's names kept as plain text", async () => {
        localStorage.setItem(
          "oc_doc_comments:att-aaaaaaaaaaaa",
          JSON.stringify([
            { id: "c1", kind: "anchor", anchorId: "ac-AC-5", label: "AC-5：切到已結案 1 秒內可操作", comment: "「1 秒」怎麼量？" },
            {
              id: "c2",
              kind: "anchor",
              anchorId: "x]`y",
              label: "a [b](javascript:alert(1))\n**粗** _斜_ `碼` <img onerror=x> https://evil.example/p <https://x.example> xhttps://a.example _https://e.example HTTPS://u.example",
              comment: "怪名字",
            },
            { id: "c3", kind: "document", comment: "流程圖字太小" },
          ]),
        );
        const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
        const { toggle } = await renderPage();
        toggle();
        fireEvent.click(screen.getByRole("button", { name: "送出給 AI" }));
        await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
        const body = post.mock.calls[0][0].body;
        expect(body).toBe(
          "**文件註解**：[pr-702.html](/api/chat/attachment/att-aaaaaaaaaaaa)\n" +
            "\n" +
            "1. 原文：\n" +
            "   > 位置：AC-5：切到已結案 1 秒內可操作（#ac-AC-5）\n" +
            "\n" +
            "   註解：「1 秒」怎麼量？\n" +
            "\n" +
            "2. 原文：\n" +
            "   > 位置：a \\[b\\](javascript:alert(1)) \\*\\*粗\\*\\* \\_斜\\_ \\`碼\\` \\<img onerror=x\\> https\\://evil.example/p \\<https\\://x.example\\> xhttps\\://a.example \\_https\\://e.example HTTPS\\://u.example（#x\\]\\`y）\n" +
            "\n" +
            "   註解：怪名字\n" +
            "\n" +
            "3. 原文：\n" +
            "   > 整份文件\n" +
            "\n" +
            "   註解：流程圖字太小",
        );
        const bubble = render(<Markdown source={body} breaks />).container;
        expect([...bubble.querySelectorAll("ol > li > blockquote")].map((q) => q.textContent)).toEqual([
          "位置：AC-5：切到已結案 1 秒內可操作（#ac-AC-5）",
          "位置：a [b](javascript:alert(1)) **粗** _斜_ `碼` <img onerror=x> https://evil.example/p <https://x.example> xhttps://a.example _https://e.example HTTPS://u.example（#x]`y）",
          "整份文件",
        ]);
        expect([...bubble.querySelectorAll("blockquote p *")].map((el) => el.outerHTML)).toEqual([]);
      });

      it("runs the page script `ocagent upload --help` prints: comment mode outlines marked places, a click on one is a pick, selected text is reported and a click ending a selection, even on a link in a marked place, neither navigates nor picks, commented and focused places are marked, and outside comment mode a click is the page's own", async () => {
        const { frame, toggle } = await renderPage();
        const page = frame.contentWindow! as Window & typeof globalThis;
        const doc = frame.contentDocument!;
        // jsdom's postMessage leaves `source` null; a browser sets it to the sender.
        page.postMessage = ((data: unknown) =>
          setTimeout(() => page.dispatchEvent(new MessageEvent("message", { data, source: page.parent })))) as Window["postMessage"];
        const top = page.parent;
        const realPost = top.postMessage;
        const fromScript: string[] = [];
        const selections: unknown[] = [];
        top.postMessage = ((data: { type: string }) => {
          fromScript.push(data.type);
          if (data.type === "select") selections.push(data);
          setTimeout(() => window.dispatchEvent(new MessageEvent("message", { data, source: page })));
        }) as Window["postMessage"];
        const scrolled: string[] = [];
        page.Element.prototype.scrollIntoView = function (this: HTMLElement) {
          scrolled.push(this.dataset.ocAnchor ?? "");
        };
        try {
          if (!doc.documentElement) doc.append(doc.createElement("html"));
          doc.documentElement.replaceChildren(doc.createElement("head"), doc.createElement("body"));
          doc.body.innerHTML =
            '<p data-oc-anchor="ac-AC-1" data-oc-anchor-label="AC-1：忙碌">AC-1</p>' +
            '<p data-oc-anchor="ac-AC-2"><a href="#ac-2">AC-2</a></p><p id="plain">說明</p>';
          const script = doc.createElement("script");
          script.textContent = snippet.replace(/^<script>\n|<\/script>\n$/g, "");
          doc.body.append(script);
          const marks = () =>
            [...doc.querySelectorAll<HTMLElement>("[data-oc-anchor]")].map((el) => [
              el.dataset.ocAnchor,
              el.className,
            ]);
          const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 0)));
          await settle();
          expect(fireEvent.click(doc.querySelector("a")!)).toBe(true);
          await settle();
          expect(fromScript).toEqual(["ready"]);

          toggle();
          await settle();
          expect(marks()).toEqual([
            ["ac-AC-1", "oc-on"],
            ["ac-AC-2", "oc-on"],
          ]);

          expect(fireEvent.click(doc.querySelector("a")!)).toBe(false);
          await settle();
          saveComposed("第二條");
          fireEvent.click(doc.getElementById("plain")!);
          fireEvent.click(doc.querySelector("p")!);
          await settle();
          saveComposed("第一條");
          await settle();
          expect(listed()).toEqual(["位置：ac-AC-2（#ac-AC-2）1. 第二條", "位置：AC-1：忙碌（#ac-AC-1）2. 第一條"]);
          expect(marks()).toEqual([
            ["ac-AC-1", "oc-on oc-commented"],
            ["ac-AC-2", "oc-on oc-commented"],
          ]);

          page.Range.prototype.getBoundingClientRect = () => new page.DOMRect(5, 6, 30, 10);
          const link = doc.querySelector("a")!;
          const range = doc.createRange();
          range.selectNodeContents(link);
          doc.getSelection()!.removeAllRanges();
          doc.getSelection()!.addRange(range);
          fromScript.length = 0;
          selections.length = 0;
          fireEvent.mouseUp(link);
          expect(fireEvent.click(link)).toBe(false);
          await settle();
          expect([...new Set(fromScript)]).toEqual(["select"]);
          expect(selections.at(-1)).toEqual({
            ...OC,
            type: "select",
            text: "AC-2",
            rect: { left: 5, top: 6, width: 30, height: 10 },
          });
          fireEvent.click(screen.getByRole("button", { name: "加註解" }));
          saveComposed("連結文字");
          doc.getSelection()!.removeAllRanges();
          fireEvent.mouseUp(doc.body);
          await settle();
          expect(selections.at(-1)).toEqual({ ...OC, type: "select", text: "", rect: null });
          expect(listed()).toEqual([
            "位置：ac-AC-2（#ac-AC-2）1. 第二條",
            "位置：AC-1：忙碌（#ac-AC-1）2. 第一條",
            "AC-23. 連結文字",
          ]);

          vi.useFakeTimers();
          fireEvent.click(screen.getAllByTestId("doc-comment")[1].querySelector(".doc-comments__goto")!);
          await act(async () => vi.advanceTimersByTime(0));
          expect(scrolled).toEqual(["ac-AC-1"]);
          expect(marks()[0]).toEqual(["ac-AC-1", "oc-on oc-commented oc-focus"]);
          await act(async () => vi.advanceTimersByTime(1500));
          expect(marks()[0]).toEqual(["ac-AC-1", "oc-on oc-commented"]);
          vi.useRealTimers();

          toggle();
          await settle();
          expect(marks()).toEqual([
            ["ac-AC-1", "oc-commented"],
            ["ac-AC-2", "oc-commented"],
          ]);
          fromScript.length = 0;
          doc.getSelection()!.addRange(range);
          fireEvent.mouseUp(link);
          doc.getSelection()!.removeAllRanges();
          expect(fireEvent.click(doc.querySelector("a")!)).toBe(true);
          fireEvent.click(doc.querySelector("p")!);
          await settle();
          expect(fromScript).toEqual([]);
        } finally {
          vi.useRealTimers();
          top.postMessage = realPost;
        }
      });
    });
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
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "doc" })).toBeTruthy(),
    );
    const actions = document.body.querySelector(".md-preview__actions")!;
    const share = screen.getByRole("button", { name: "複製分享連結" });
    // Reached by ACCESSIBLE NAME, not by visible text: T-51 ④ made the header
    // controls icon-only (owner: 「又都有字太多了，可以一起改成圖示就好嘛」), so
    // 下載 is now the `aria-label` and nothing in the header prints it.
    const dl = screen.getByRole("link", { name: "下載" }) as HTMLAnchorElement;
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
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "doc" })).toBeTruthy(),
    );
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

  it("shows 複製連結失敗 with its own glyph when the mint fails instead of faking success", async () => {
    vi.spyOn(api, "getChatAttachmentShareLink").mockRejectedValue(
      new Error("boom"),
    );
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    renderOverlay();
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "doc" })).toBeTruthy(),
    );
    const idleGlyph = screen.getByRole("button", {
      name: "複製分享連結",
    }).innerHTML;
    fireEvent.click(screen.getByRole("button", { name: "複製分享連結" }));
    await waitFor(() => expect(writeText).not.toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: "已複製連結" })).toBeNull();
    const failed = screen.getByRole("button", { name: "複製連結失敗" });
    expect(failed).toBeTruthy();
    // T-51 ④ made these controls icon-only, so the accessible name above is the
    // half only a screen reader gets. A sighted reader has the glyph and
    // nothing else — a failure drawn as the idle copy icon is indistinguishable
    // from "nothing happened yet". These two assertions are what makes deleting
    // the copyFailed arm of the icon ternary go red: the name switch survives
    // it, the glyph does not.
    expect(failed.innerHTML).not.toBe(idleGlyph);
    const { container: warning } = render(<AlertTriangleIcon size={14} />);
    expect(failed.innerHTML).toBe(warning.innerHTML);
  });

  // T-59 — the COMPARE mode. A comparison stopped being an attachment the day
  // it became a url, so this overlay no longer resolves anything for it: it
  // hosts `DiffScreen` (which owns the read and its own tests) and, because
  // there is no blob involved, offers none of the blob actions.
  describe("compare mode", () => {
    const params = { before: "att-0123456789ab", after: "att-fedcba987654" };

    it("hosts the compare screen and offers no blob actions with it", async () => {
      globalThis.fetch = vi.fn(async () => ({
        ok: true,
        json: async () => ({
          before: { text: "alpha\nbravo", label: "改動前", gone: false },
          after: { text: "alpha\nBRAVO", label: "改動後", gone: false },
        }),
      })) as unknown as typeof fetch;

      render(
        <I18nProvider>
          <MarkdownPreviewOverlay
            title="逐行比對"
            diffParams={params}
            onClose={() => {}}
          />
        </I18nProvider>,
      );

      await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
      // No blob, so nothing to download and no FILE-level share link to mint —
      // one here would point at a file that does not exist.
      expect(screen.queryByText(zh.chat.mdPreview.download)).toBeNull();
      expect(screen.queryByLabelText(zh.chat.copyShareLink)).toBeNull();
      expect(screen.queryByText(zh.chat.mdPreview.openInNewTab)).toBeNull();
    });

    // T-59 — the comparison's OWN external link. This is the studio host of
    // that control: the owner is looking at the comparison and wants to hand it
    // to someone outside, which until now was a CLI action only.
    it("offers the comparison's external link as an icon in the header actions", async () => {
      const mint = vi
        .spyOn(api, "getDiffShareLink")
        .mockResolvedValue("/diff?before=att-0123456789ab&after=att-fedcba987654&sig=minted");
      const writeText = vi.fn(async () => {});
      Object.defineProperty(navigator, "clipboard", {
        value: { writeText },
        configurable: true,
      });
      globalThis.fetch = vi.fn(async () => ({
        ok: true,
        json: async () => ({
          before: { text: "alpha", label: "改動前", gone: false },
          after: { text: "beta", label: "改動後", gone: false },
        }),
      })) as unknown as typeof fetch;

      render(
        <I18nProvider>
          <MarkdownPreviewOverlay
            title="逐行比對"
            diffParams={params}
            onClose={() => {}}
          />
        </I18nProvider>,
      );

      await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
      const button = screen.getByRole("button", { name: zh.diff.copyShareLink });
      // It lives in the header action row, beside the close button — the same
      // slot the file-level share control occupies, not a second place to look.
      expect(
        document.body
          .querySelector(".md-preview__actions")!
          .contains(button),
      ).toBe(true);
      // Icon-only (owner 2026-09-03「1. 用圖示」).
      expect(button.textContent).toBe("");

      fireEvent.click(button);
      await waitFor(() => expect(mint).toHaveBeenCalledWith(params));
      await waitFor(() =>
        expect(writeText).toHaveBeenCalledWith(
          `${window.location.origin}/diff?before=att-0123456789ab&after=att-fedcba987654&sig=minted`,
        ),
      );
    });
  });
});
