// Question-side reply-card attachments (T-5e8a 開卡帶附件). Locked here:
//   1. A card carrying question attachments renders them under the body on
//      the 等我回覆 page: image → thumbnail, non-image → download chip under
//      its stored filename.
//   2. Clicking an image thumbnail opens the shared Lightbox full-size;
//      the × control closes it.
//   3. The inline chat card (ChatReplyCard) renders the SAME strip — one
//      shared implementation, zero drift.
//   4. A card WITHOUT question attachments renders no strip at all (markup
//      parity with the pre-T-5e8a card).

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RepliesPage } from "./RepliesPage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import { ChatReplyCard } from "./ChatReplyCard";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
import { zh } from "../i18n/locales/zh";
import type { ChatAttachmentView, ReplyCard } from "../api/adapter";

const IMG_DATA_URI =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

function imgAtt(id = "att-img"): ChatAttachmentView {
  // Mock cards carry data-URI urls — AttachmentStrip serves them verbatim
  // (authedAttachmentUrl only decorates server-relative paths).
  return {
    id,
    url: IMG_DATA_URI,
    filename: "screenshot.png",
    mime: "image/png",
    isImage: true,
  };
}

function fileAtt(id = "att-file"): ChatAttachmentView {
  return {
    id,
    url: "data:application/pdf;base64,JVBERg==",
    filename: "report.pdf",
    mime: "application/pdf",
    isImage: false,
  };
}

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "看一下截圖，要照這樣出嗎？",
    body: "",
    options: [{ text: "照這樣出", aiPick: true }, { text: "先不要", aiPick: false }],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("reply-card question attachments", () => {
  it("renders thumbnails + file chips on a waiting card (RepliesPage) and opens the shared modal on click", async () => {
    __injectMockReplyCard(
      mkCard({ attachments: [imgAtt(), fileAtt()] })
    );
    const { container, findByTestId } = render(
      <I18nProvider>
        <ReplyCardsProvider><RepliesPage /></ReplyCardsProvider>
      </I18nProvider>
    );
    await findByTestId("waiting-card");

    // The strip: one image thumbnail + one shared-popup trigger under its filename.
    const strip = container.querySelector(".reply-card__question-atts");
    expect(strip).not.toBeNull();
    const img = strip!.querySelector("img") as HTMLImageElement;
    expect(img.src).toBe(IMG_DATA_URI);
    const chip = strip!.querySelector("button.chat__msg-file") as HTMLButtonElement;
    expect(chip.textContent).toContain("report.pdf");
    fireEvent.click(chip);
    expect(document.body.querySelector(".md-preview")).not.toBeNull();
    // T-36 — a .pdf cannot be DRAWN here, but the browser can show it in a tab
    // of its own, so once the share link is minted the central line points at
    // the 「在新頁面顯示」 button rather than back at 下載. The mint is async:
    // reading the status synchronously would pin the pre-mint 請下載 line and
    // pass for the wrong reason.
    await waitFor(() =>
      expect(document.body.querySelector(".md-preview__status")?.textContent).toBe(
        zh.chat.mdPreview.unavailableOpenInNewTab,
      ),
    );
    fireEvent.click(document.body.querySelector(".md-preview__close") as HTMLButtonElement);

    // The answered and waiting sides both use the attachment-owned modal.
    fireEvent.click(img);
    const modal = document.body.querySelector(".md-preview")!;
    expect(modal).toBeTruthy();
    const preview = modal.querySelector<HTMLImageElement>(".md-preview__image")!;
    expect(preview.src).toBe(IMG_DATA_URI);
    // Opens at 100%, and 100% means the STYLESHEET owns the size: the component
    // writes an inline width/height only above 1x, and that inline size is also
    // the box `measureFit` has to strip before it can read a fit box. Asserting
    // the zoom readout alone would be near-vacuous here — it is
    // `Math.round(useState(1) * 100)`, true the moment the component mounts.
    expect(preview.style.width).toBe("");
    expect(preview.style.maxWidth).toBe("");
    expect(modal.querySelector(".md-preview__zoom")!.textContent).toContain("100%");
    fireEvent.click(modal.querySelector(".md-preview__close") as HTMLButtonElement);
    expect(document.body.querySelector(".md-preview")).toBeNull();
  });

  it("renders the same strip on the inline chat card (shared implementation)", async () => {
    __injectMockReplyCard(mkCard({ attachments: [imgAtt()] }));
    const { container, findByTestId } = render(
      <I18nProvider>
        <ChatReplyCard replyCardId="rc-1" fallbackSummary="(summary)" />
      </I18nProvider>
    );
    // The inline card mounts collapsed (owner 2026-09-04); the strip is part of
    // the open card.
    fireEvent.click(await findByTestId("chat-reply-card-expand"));
    await waitFor(() => {
      expect(
        container.querySelector(".reply-card__question-atts img")
      ).not.toBeNull();
    });
    fireEvent.click(
      container.querySelector(".reply-card__question-atts img") as HTMLElement
    );
    const modal = document.body.querySelector(".md-preview");
    expect(modal).not.toBeNull();
    expect(modal!.getAttribute("role")).toBe("dialog");
  });

  it("renders NO strip on a card without question attachments", async () => {
    __injectMockReplyCard(mkCard({}));
    const { container, findByTestId } = render(
      <I18nProvider>
        <ReplyCardsProvider><RepliesPage /></ReplyCardsProvider>
      </I18nProvider>
    );
    await findByTestId("waiting-card");
    expect(container.querySelector(".reply-card__question-atts")).toBeNull();
  });
});
