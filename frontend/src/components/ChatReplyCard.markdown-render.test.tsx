// T-13af — the reply card's body (卡片內文, ReplyCardDTO.body) is agent-authored
// free text and must render through the shared, XSS-safe `Markdown` component.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatReplyCard } from "./ChatReplyCard";
import type { ReplyCard } from "../api/adapter";
import { __resetMock, __injectMockReplyCard } from "../api/mock";

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要幫你寄出這封信嗎？",
    body: "",
    options: ["寄出", "先不要"],
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

function renderCard(id = "rc-1") {
  return render(
    <I18nProvider>
      <ChatReplyCard replyCardId={id} fallbackSummary="(summary)" />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ChatReplyCard markdown render (T-13af)", () => {
  it("renders card.body markdown (bold/list) as elements", async () => {
    __injectMockReplyCard(
      mkCard({ body: "**注意**:\n- 會寄到客戶信箱\n- 無法撤回" })
    );
    const { container, findByTestId } = renderCard();
    await findByTestId("chat-reply-card");

    const body = container.querySelector(".reply-card__body")!;
    expect(body.querySelector("strong")?.textContent).toBe("注意");
    expect(body.querySelectorAll("ul > li").length).toBe(2);
    expect(body.textContent).not.toContain("**注意**");
  });

  it("sanitizes a malicious card.body — no raw HTML injected", async () => {
    __injectMockReplyCard(
      mkCard({ body: "<img src=x onerror=alert(1)>" })
    );
    const { container, findByTestId } = renderCard();
    await findByTestId("chat-reply-card");

    const body = container.querySelector(".reply-card__body")!;
    expect(body.querySelector("img")).toBeNull();
    expect(body.textContent).toContain("<img src=x onerror=alert(1)>");
  });

  // T-a20b — summary is the same agent-authored free text as body, written in
  // the same call. It rendered as plain text while its sibling one line down
  // went through Markdown.
  it("renders card.summary markdown (bold/inline code) as elements", async () => {
    __injectMockReplyCard(
      mkCard({ summary: "要不要合 **fms #20054**（`919fe961`）？", body: "內文" })
    );
    const { container, findByTestId } = renderCard();
    await findByTestId("chat-reply-card");

    const summary = container.querySelector(".reply-card__summary")!;
    // positive control — the scope really holds the summary's text
    expect(summary.textContent).toContain("要不要合");
    expect(summary.querySelector("strong")?.textContent).toBe("fms #20054");
    expect(summary.querySelector("code")?.textContent).toBe("919fe961");
    expect(summary.textContent).not.toContain("**fms #20054**");
    expect(summary.textContent).not.toContain("`919fe961`");
  });

  it("still renders the fallbackSummary when the card has not landed", async () => {
    // no __injectMockReplyCard → component falls back; the fallback path must
    // survive the switch to <Markdown>.
    const { container, findByTestId } = renderCard("rc-missing");
    await findByTestId("chat-reply-card");

    const summary = container.querySelector(".reply-card__summary")!;
    expect(summary.textContent).toContain("(summary)");
  });
});
