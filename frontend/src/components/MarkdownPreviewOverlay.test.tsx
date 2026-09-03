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
import { AlertTriangleIcon } from "./icons";

describe("isMarkdownAttachment", () => {
  it("accepts markdown mimes and .md/.markdown filenames", () => {
    expect(isMarkdownAttachment("text/markdown", "x")).toBe(true);
    expect(isMarkdownAttachment("text/x-markdown", "x")).toBe(true);
    expect(isMarkdownAttachment("text/plain", "design.md")).toBe(true);
    expect(
      isMarkdownAttachment("application/octet-stream", "NOTES.MARKDOWN"),
    ).toBe(true);
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
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "Hello" })).toBeTruthy(),
    );
    expect(screen.getByText("body")).toBeTruthy();
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
          source={"# Inline\n\nbody text"}
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    // Synchronous — an inline source never passes through the loading state.
    expect(screen.getByRole("heading", { name: "Inline" })).toBeTruthy();
    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(screen.queryByText("載入預覽中…")).toBeNull();
    expect(document.body.querySelector(".md-preview__download")).toBeNull();
    expect(
      document.body
        .querySelector(".md-preview__md")!
        .classList.contains("doc-md"),
    ).toBe(true);
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
      expect(
        document.body.querySelector("button.md-preview__share"),
      ).toBeTruthy(),
    );
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
    expect(document.body.querySelector("button.md-preview__share")).toBeNull();
    expect(document.body.querySelector("a.md-preview__download")).toBeNull();
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

  // T-59 — the COMPARE attachment. Its bytes are a pointer pair, so the overlay
  // makes three fetches (the pair, then the two blobs it names) and hands the
  // two texts to the SAME DiffView the document version history uses.
  describe("compare attachment", () => {
    const PAIR = JSON.stringify({
      before: { attachment_id: "att-old", label: "9/2 21:12" },
      after: { attachment_id: "att-new", label: "目前存檔內容" },
    });

    /** Answer per blob id; a null body is a blob that will not load. */
    const serve = (bodies: Record<string, string | null>) =>
      vi.fn(async (input: unknown) => {
        const asked = String(input);
        for (const [id, body] of Object.entries(bodies)) {
          if (!asked.includes(id)) continue;
          return body === null
            ? { ok: false, status: 404, text: async () => "" }
            : { ok: true, text: async () => body };
        }
        throw new Error("unexpected fetch: " + asked);
      }) as unknown as typeof fetch;

    const openCompare = () =>
      render(
        <I18nProvider>
          <MarkdownPreviewOverlay
            title="change.diff"
            url="/api/chat/attachment/att-pair"
            attachmentId="att-pair"
            mime="application/vnd.officraft.diff"
            onClose={() => {}}
          />
        </I18nProvider>,
      );

    it("resolves both sides and compares them in the shared diff surface", async () => {
      globalThis.fetch = serve({
        "att-pair": PAIR,
        "att-old": "alpha\nbravo\ncharlie",
        "att-new": "alpha\nBRAVO\ncharlie",
      });

      openCompare();

      await waitFor(() => expect(screen.getByTestId("md-preview-diff")).toBeTruthy());
      // The changed line appears on both sides — i.e. a real comparison of the
      // two RESOLVED blobs, not of the pointer pair's own JSON.
      expect(screen.getByText("bravo")).toBeTruthy();
      expect(screen.getByText("BRAVO")).toBeTruthy();
      // The pointer pair's own text must never be what gets compared.
      expect(screen.queryByText(/attachment_id/)).toBeNull();

      // WHICH SIDE IS WHICH. Presence alone survives the two sides being
      // swapped, and a swap is the failure that lies loudest: the reader sees
      // the new version struck out and the old one added, and concludes the
      // change did the opposite of what it did. Assert the direction, not just
      // that both texts arrived.
      const diff = screen.getByTestId("md-preview-diff");
      expect(diff.querySelector('[data-kind="removed"] .diff-view__text')?.textContent).toBe("bravo");
      expect(diff.querySelector('[data-kind="added"] .diff-view__text')?.textContent).toBe("BRAVO");
      // …and the labels travel with their own side.
      expect(diff.querySelector(".diff-view__label--before")?.textContent).toContain("9/2 21:12");
      expect(diff.querySelector(".diff-view__label--after")?.textContent).toContain("目前存檔內容");
    });

    // A pair whose second blob is gone must say so. Rendering the half it DID
    // get would draw every line of the surviving side as a deletion — a
    // confident, wrong answer to "what changed".
    it("reports a failure when one of the two sides cannot be loaded", async () => {
      globalThis.fetch = serve({
        "att-pair": PAIR,
        "att-old": "alpha\nbravo",
        "att-new": null,
      });

      openCompare();

      // The line names WHICH failure this is. "無法載入預覽" would be true of a
      // document that simply would not load; a comparison missing one half is a
      // different fact about a different object, and the reader can act on the
      // difference (a pruned revision is not worth retrying).
      await waitFor(() =>
        expect(
          screen.getByText("這個比較有一側已經不在了，畫不出來。"),
        ).toBeTruthy(),
      );
      expect(screen.queryByTestId("md-preview-diff")).toBeNull();
      // Nothing of the SURVIVING side reaches the screen. Drawing it alone
      // marks every one of its lines as deleted — a confident wrong answer, not
      // a partial one.
      expect(screen.queryByText("bravo")).toBeNull();
      expect(screen.queryByText("alpha")).toBeNull();
    });

    // T-59 second round (owner on rc-8bf26b440e6e): a side may name ONE FIELD
    // OF A DOCUMENT instead of a blob. Nothing is copied — the revision already
    // has an address — so these sides are read through the same three faces the
    // version-history screen uses, and `field` picks the same slice on each.
    const docPair = (before: unknown, after: unknown) =>
      JSON.stringify({ before, after });

    it("compares a retained revision against the live document", async () => {
      globalThis.fetch = serve({
        "att-pair": docPair(
          { doc: { kind: "lessons", key: "mira", at: "12", field: "text" } },
          { doc: { kind: "lessons", key: "mira", at: "current", field: "text" } },
        ),
      });
      vi.spyOn(api, "getDocumentRevision").mockResolvedValue({
        id: 12,
        content: { text: "alpha\nbravo" },
      });
      vi.spyOn(api, "getLessons").mockResolvedValue({
        roleKey: "mira",
        text: "alpha\nBRAVO",
        capChars: 60000,
      } as Awaited<ReturnType<typeof api.getLessons>>);

      openCompare();

      await waitFor(() => expect(screen.getByTestId("md-preview-diff")).toBeTruthy());
      expect(screen.getByText("bravo")).toBeTruthy();
      expect(screen.getByText("BRAVO")).toBeTruthy();
      // The ADDRESS reached the reader intact: the revision was asked for by
      // kind, key and id, not by some default this screen picked.
      expect(api.getDocumentRevision).toHaveBeenCalledWith("lessons", "mira", 12);

      // A side carrying no label of its own gets the reader's own heading — and
      // the `current` side says it is LIVE, because the same attachment opened
      // next month compares against a different document. Without this the two
      // columns are indistinguishable and the reader cannot tell that one of
      // them moves.
      expect(screen.getByText("版本 #12")).toBeTruthy();
      expect(
        screen.getByText("目前存檔內容（此刻的內容，之後會不一樣）"),
      ).toBeTruthy();
    });

    it("compares the shipped default against an uploaded file", async () => {
      globalThis.fetch = serve({
        "att-pair": docPair(
          { doc: { kind: "global_context", key: "global", at: "seed", field: "text" } },
          { attachment_id: "att-new", label: "我的版本" },
        ),
        "att-new": "alpha\nBRAVO",
      });
      vi.spyOn(api, "getDocumentSeed").mockResolvedValue({
        kind: "global_context",
        key: "global",
        content: { text: "alpha\nbravo" },
      });

      openCompare();

      await waitFor(() => expect(screen.getByTestId("md-preview-diff")).toBeTruthy());
      // The two shapes are exclusive PER SIDE, not per attachment: an uploaded
      // file compares against a document.
      expect(screen.getByText("bravo")).toBeTruthy();
      expect(screen.getByText("BRAVO")).toBeTruthy();
      expect(screen.getByText("初始版本")).toBeTruthy();
      expect(screen.getByText("我的版本")).toBeTruthy();
    });

    // A field the document does not carry is the same miss as a pruned
    // revision: the address named something that is not there. It must NOT
    // resolve to "" — that would draw every line of the other side as added.
    it("reports a side as gone when the document has no such field", async () => {
      globalThis.fetch = serve({
        "att-pair": docPair(
          { doc: { kind: "lessons", key: "mira", at: "12", field: "sop_md" } },
          { attachment_id: "att-new" },
        ),
        "att-new": "alpha",
      });
      vi.spyOn(api, "getDocumentRevision").mockResolvedValue({
        id: 12,
        content: { text: "alpha\nbravo" },
      });

      openCompare();

      await waitFor(() =>
        expect(
          screen.getByText("這個比較有一側已經不在了，畫不出來。"),
        ).toBeTruthy(),
      );
      expect(screen.queryByTestId("md-preview-diff")).toBeNull();
      expect(screen.queryByText("alpha")).toBeNull();
    });

    // The server validates that the address is SAYABLE, never that the kind is
    // one this build knows — a kind list there would be a second enumeration
    // going stale silently. So the reader is where an unknown kind is caught,
    // and it is caught as "this side is gone", not as a crash.
    it("reports a side as gone when the kind is not one this cockpit knows", async () => {
      globalThis.fetch = serve({
        "att-pair": docPair(
          { doc: { kind: "some_future_kind", key: "x", at: "current", field: "text" } },
          { attachment_id: "att-new" },
        ),
        "att-new": "alpha",
      });

      openCompare();

      await waitFor(() =>
        expect(
          screen.getByText("這個比較有一側已經不在了，畫不出來。"),
        ).toBeTruthy(),
      );
      expect(screen.queryByTestId("md-preview-diff")).toBeNull();
    });

    // The type is the MIME. A text file that happens to be NAMED like a diff is
    // still a text file — keying on the extension would make the compare screen
    // open on documents that carry no pointer pair at all.
    it("does not treat a text attachment named like a diff as a comparison", async () => {
      globalThis.fetch = serve({ "att-doc": "# just a document" });

      render(
        <I18nProvider>
          <MarkdownPreviewOverlay
            title="change.diff"
            url="/api/chat/attachment/att-doc"
            attachmentId="att-doc"
            mime="text/markdown"
            onClose={() => {}}
          />
        </I18nProvider>,
      );

      await waitFor(() =>
        expect(screen.getByRole("heading", { name: "just a document" })).toBeTruthy(),
      );
      expect(screen.queryByTestId("md-preview-diff")).toBeNull();
    });
  });
});
