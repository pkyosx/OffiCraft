// T-7bc2: a .md attachment on a reply card (question side AND answer side)
// gets the SAME in-cockpit 預覽 trigger ChatArea's chat attachments and the
// task artifacts popover already have — the chip itself renders as a
// <button> instead of the download <a> (owner 2026-07-21: no separate 眼睛
// button). Mirrors ChatArea.md-preview.test.tsx.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { RepliesPage } from "./RepliesPage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import { ReplyCardAnsweredBody } from "./ReplyCardBody";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
import type { ChatAttachmentView, ReplyCard } from "../api/adapter";

function mdAtt(id = "att-md"): ChatAttachmentView {
  return {
    id,
    url: "/api/chat/attachment/" + id,
    filename: "design-proposal.md",
    mime: "text/markdown",
    isImage: false,
  };
}

function pdfAtt(id = "att-pdf"): ChatAttachmentView {
  return {
    id,
    url: "/api/chat/attachment/" + id,
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
    summary: "看一下這份設計文件，可以嗎？",
    body: "",
    options: [{ text: "可以", aiPick: true }, { text: "再調整", aiPick: false }],
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

/** Open every card the 請示 page is showing. Every card starts COLLAPSED (owner
 * 2026-09-11「預設全部折疊」) and a card's interior is READ when it is opened, so
 * a test that asserts on the interior has to open it first. The whole card is
 * the toggle, so the click lands on the article itself.
 *
 * 🔴 IT FAILS WHEN IT FINDS NOTHING. The version this replaces looped over a
 * `querySelectorAll` and did nothing at all when the selector stopped matching —
 * a silent pass in every caller. If the seam moves again, this line goes red
 * instead. */
async function openCards() {
  const cards = document.querySelectorAll<HTMLElement>(
    "[data-reply-card-id][aria-expanded]"
  );
  expect(
    cards.length,
    "openCards() found no reply card to open — the toggle seam moved"
  ).toBeGreaterThan(0);
  for (const el of cards) {
    if (el.getAttribute("aria-expanded") === "false") fireEvent.click(el);
  }
  await waitFor(() =>
    expect(document.querySelectorAll('[data-testid="card-loading"]')).toHaveLength(0)
  );
}

afterEach(() => {
  vi.restoreAllMocks();
  __resetMock();
});

describe("reply-card question attachments: .md preview (T-7bc2)", () => {
  it("renders every stored file chip as a popup trigger", async () => {
    __resetMock();
    __injectMockReplyCard(mkCard({ attachments: [mdAtt(), pdfAtt()] }));
    const { container, findByTestId } = render(
      <I18nProvider>
        <ReplyCardsProvider><RepliesPage /></ReplyCardsProvider>
      </I18nProvider>
    );
    await findByTestId("waiting-card");
    await openCards();
    const mdButtons = container.querySelectorAll(
      ".reply-card__question-atts button.chat__msg-file"
    );
    const fileLinks = container.querySelectorAll(
      ".reply-card__question-atts a.chat__msg-file"
    );
    expect(mdButtons.length).toBe(2);
    expect(fileLinks.length).toBe(0);
  });

  it("opens the preview overlay and renders the markdown on click", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# design-proposal\n\nthe **plan**",
    })) as unknown as typeof fetch;
    const mint = vi
      .spyOn(api, "getChatAttachmentShareLink")
      .mockResolvedValue("/api/chat/attachment/att-md?sig=test-sig");
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    __resetMock();
    __injectMockReplyCard(mkCard({ attachments: [mdAtt()] }));
    const { container, findByTestId, getByRole } = render(
      <I18nProvider>
        <ReplyCardsProvider><RepliesPage /></ReplyCardsProvider>
      </I18nProvider>
    );
    await findByTestId("waiting-card");
    await openCards();
    fireEvent.click(container.querySelector("button.chat__msg-file")!);
    await waitFor(() =>
      expect(getByRole("heading", { name: "design-proposal" })).toBeTruthy()
    );
    const dl = document.body.querySelector("a.md-preview__download") as HTMLAnchorElement;
    expect(dl.getAttribute("download")).toBe("design-proposal.md");
    const actions = document.body.querySelector(".md-preview__actions") as HTMLElement;
    const share = within(actions).getByRole("button", { name: "複製分享連結" });
    fireEvent.click(share);
    await waitFor(() => expect(mint).toHaveBeenCalledWith("att-md"));
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(
        `${window.location.origin}/api/chat/attachment/att-md?sig=test-sig`,
      ),
    );
  });

  it("opens the common popup for a non-previewable PDF", async () => {
    __resetMock();
    __injectMockReplyCard(mkCard({ attachments: [pdfAtt()] }));
    const { container, findByTestId } = render(
      <I18nProvider>
        <ReplyCardsProvider><RepliesPage /></ReplyCardsProvider>
      </I18nProvider>
    );
    await findByTestId("waiting-card");
    await openCards();
    const file = container.querySelector<HTMLButtonElement>("button.chat__msg-file");
    expect(file).not.toBeNull();
    expect(container.querySelector("a.chat__msg-file")).toBeNull();
    fireEvent.click(file!);
    // T-36 (B1) — a PDF still cannot be DRAWN in this panel, so the status line
    // is still what the body shows. What changed is where that line sends the
    // reader: the header now carries 「在新頁面顯示」 for a pdf (the browser
    // displays it), so the line points at that button instead of back at 下載.
    // Before the share link is minted there is no button yet and the original
    // 請下載 line stands, which is why this waits for the settled text.
    await waitFor(() =>
      expect(document.body.querySelector(".md-preview__status")?.textContent).toBe(
        "此檔案無法在這裡預覽，請用上方的「在新頁面顯示」開啟。",
      )
    );
  });
});

describe("reply-card ANSWER attachments: .md preview (T-7bc2)", () => {
  it("renders the answer's .md chip as a <button> and opens the overlay on click", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# answer-doc",
    })) as unknown as typeof fetch;
    const card = mkCard({
      status: "answered",
      answer: {
        optionIdxs: null,
        text: "附上結果文件",
        attachments: [mdAtt("att-ans-md")],
      },
    });
    const { container, getByRole } = render(
      <I18nProvider>
        <ReplyCardAnsweredBody card={card} onReanswer={() => Promise.resolve()} />
      </I18nProvider>
    );
    const btn = container.querySelector("button.chat__msg-file");
    expect(btn).not.toBeNull();
    fireEvent.click(btn!);
    await waitFor(() =>
      expect(getByRole("heading", { name: "answer-doc" })).toBeTruthy()
    );
  });
});
