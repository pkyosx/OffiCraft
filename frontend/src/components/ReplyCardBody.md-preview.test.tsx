// T-7bc2: a .md attachment on a reply card (question side AND answer side)
// gets the SAME in-cockpit 預覽 trigger ChatArea's chat attachments and the
// task artifacts popover already have — the chip itself renders as a
// <button> instead of the download <a> (owner 2026-07-21: no separate 眼睛
// button). Mirrors ChatArea.md-preview.test.tsx.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RepliesPage } from "./RepliesPage";
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
    options: ["可以", "再調整"],
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

afterEach(() => {
  vi.restoreAllMocks();
  __resetMock();
});

describe("reply-card question attachments: .md preview (T-7bc2)", () => {
  it("renders the .md chip as a <button> (preview) and the pdf chip as an <a> (download)", async () => {
    __resetMock();
    __injectMockReplyCard(mkCard({ attachments: [mdAtt(), pdfAtt()] }));
    const { container, findByTestId } = render(
      <I18nProvider>
        <RepliesPage />
      </I18nProvider>
    );
    await findByTestId("waiting-card");
    const mdButtons = container.querySelectorAll(
      ".reply-card__question-atts button.chat__msg-file"
    );
    const pdfLinks = container.querySelectorAll(
      ".reply-card__question-atts a.chat__msg-file"
    );
    expect(mdButtons.length).toBe(1);
    expect(pdfLinks.length).toBe(1);
  });

  it("opens the preview overlay and renders the markdown on click", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# design-proposal\n\nthe **plan**",
    })) as unknown as typeof fetch;
    __resetMock();
    __injectMockReplyCard(mkCard({ attachments: [mdAtt()] }));
    const { container, findByTestId, getByRole } = render(
      <I18nProvider>
        <RepliesPage />
      </I18nProvider>
    );
    await findByTestId("waiting-card");
    fireEvent.click(container.querySelector("button.chat__msg-file")!);
    await waitFor(() =>
      expect(getByRole("heading", { name: "design-proposal" })).toBeTruthy()
    );
    const dl = container.querySelector(".md-preview__download") as HTMLAnchorElement;
    expect(dl.getAttribute("download")).toBe("design-proposal.md");
  });

  it("renders no preview <button> when the card carries no .md attachment (pdf stays a plain <a>)", async () => {
    __resetMock();
    __injectMockReplyCard(mkCard({ attachments: [pdfAtt()] }));
    const { container, findByTestId } = render(
      <I18nProvider>
        <RepliesPage />
      </I18nProvider>
    );
    await findByTestId("waiting-card");
    expect(container.querySelector("button.chat__msg-file")).toBeNull();
    expect(container.querySelector("a.chat__msg-file")).not.toBeNull();
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
        optionIdx: null,
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
