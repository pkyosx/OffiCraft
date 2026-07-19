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
import { ChatReplyCard } from "./ChatReplyCard";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
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
    options: ["照這樣出", "先不要"],
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
  it("renders thumbnails + file chips on a waiting card (RepliesPage) and opens the lightbox on click", async () => {
    __injectMockReplyCard(
      mkCard({ attachments: [imgAtt(), fileAtt()] })
    );
    const { container, findByTestId } = render(
      <I18nProvider>
        <RepliesPage />
      </I18nProvider>
    );
    await findByTestId("waiting-card");

    // The strip: one image thumbnail + one download chip under its filename.
    const strip = container.querySelector(".reply-card__question-atts");
    expect(strip).not.toBeNull();
    const img = strip!.querySelector("img") as HTMLImageElement;
    expect(img.src).toBe(IMG_DATA_URI);
    const chip = strip!.querySelector("a.chat__msg-file") as HTMLAnchorElement;
    expect(chip.textContent).toContain("report.pdf");
    expect(chip.getAttribute("download")).toBe("report.pdf");

    // Click the thumbnail → the shared Lightbox opens full-size; × closes.
    expect(container.querySelector(".chat__lightbox")).toBeNull();
    fireEvent.click(img);
    const lightbox = container.querySelector(".chat__lightbox");
    expect(lightbox).not.toBeNull();
    expect(
      (lightbox!.querySelector(".chat__lightbox-image") as HTMLImageElement).src
    ).toBe(IMG_DATA_URI);
    fireEvent.click(
      lightbox!.querySelector(".chat__lightbox-close") as HTMLButtonElement
    );
    expect(container.querySelector(".chat__lightbox")).toBeNull();
  });

  it("renders the same strip on the inline chat card (shared implementation)", async () => {
    __injectMockReplyCard(mkCard({ attachments: [imgAtt()] }));
    const { container, findByTestId } = render(
      <I18nProvider>
        <ChatReplyCard replyCardId="rc-1" fallbackSummary="(summary)" />
      </I18nProvider>
    );
    await findByTestId("chat-reply-card");
    await waitFor(() => {
      expect(
        container.querySelector(".reply-card__question-atts img")
      ).not.toBeNull();
    });
    fireEvent.click(
      container.querySelector(".reply-card__question-atts img") as HTMLElement
    );
    expect(container.querySelector(".chat__lightbox")).not.toBeNull();
  });

  it("renders NO strip on a card without question attachments", async () => {
    __injectMockReplyCard(mkCard({}));
    const { container, findByTestId } = render(
      <I18nProvider>
        <RepliesPage />
      </I18nProvider>
    );
    await findByTestId("waiting-card");
    expect(container.querySelector(".reply-card__question-atts")).toBeNull();
  });
});
