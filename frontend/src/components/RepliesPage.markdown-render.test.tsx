// T-13af — the 等我回覆頁 card's body (卡片內文, ReplyCardDTO.body) is
// agent-authored free text and must render through the shared, XSS-safe
// `Markdown` component — same contract as the chat/task inline reply cards
// (ReplyCardBody.tsx is shared; the body row itself lives in each wrapper).

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RepliesPage } from "./RepliesPage";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
import type { ReplyCard } from "../api/adapter";

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
    createdTs: Date.now() / 1000 - 25 * 60,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

function renderPage() {
  return render(
    <I18nProvider>
      <RepliesPage />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("RepliesPage markdown render (T-13af)", () => {
  it("renders card.body markdown (bold/list) as elements", async () => {
    __injectMockReplyCard(
      mkCard({ body: "**注意**:\n- 會寄到客戶信箱\n- 無法撤回" })
    );
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");

    const body = card.querySelector(".reply-card__body")!;
    expect(body.querySelector("strong")?.textContent).toBe("注意");
    expect(body.querySelectorAll("ul > li").length).toBe(2);
    expect(body.textContent).not.toContain("**注意**");
  });

  // T-a80e — a fenced code block must land as <pre> INSIDE the .doc-md
  // container: the mobile overflow fix (`.doc-md pre { overflow-x: auto }`,
  // settings.css) only applies if that structure holds. jsdom can't measure
  // layout, so the visual half is covered by a real-browser check.
  it("renders a fenced code block as <pre> inside the .doc-md body", async () => {
    __injectMockReplyCard(
      mkCard({
        body: "跑這個：\n```\nkubectl --context arn:aws:eks:us-west-2 -n sit exec pod -- python manage.py cmd\n```",
      })
    );
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");

    const body = card.querySelector(".reply-card__body")!;
    expect(body.classList.contains("doc-md")).toBe(true);
    const pre = body.querySelector("pre");
    expect(pre).not.toBeNull();
    expect(pre?.querySelector("code")?.textContent).toContain("kubectl");
  });

  it("sanitizes a malicious card.body — no raw HTML injected", async () => {
    __injectMockReplyCard(mkCard({ body: "<img src=x onerror=alert(1)>" }));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");

    const body = card.querySelector(".reply-card__body")!;
    expect(body.querySelector("img")).toBeNull();
    expect(body.textContent).toContain("<img src=x onerror=alert(1)>");
  });

  // T-a20b — summary is the same agent-authored free text as body, and this
  // page renders it at TWO sites (waiting card + handled card); T-13af's fix
  // reached neither.
  it("renders card.summary markdown (bold/inline code) on a WAITING card", async () => {
    __injectMockReplyCard(
      mkCard({ summary: "要不要合 **fms #20054**（`919fe961`）？" })
    );
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");

    const summary = card.querySelector(".reply-card__summary")!;
    // positive control — the scope really holds the summary's text
    expect(summary.textContent).toContain("要不要合");
    expect(summary.querySelector("strong")?.textContent).toBe("fms #20054");
    expect(summary.querySelector("code")?.textContent).toBe("919fe961");
    expect(summary.textContent).not.toContain("**fms #20054**");
    expect(summary.textContent).not.toContain("`919fe961`");
  });

  it("renders card.summary markdown (bold/inline code) on a HANDLED card", async () => {
    __injectMockReplyCard(
      mkCard({
        summary: "要不要合 **fms #20054**（`919fe961`）？",
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const { findByTestId } = renderPage();
    // 近期已處理 is collapsed by default — open it to reach the handled card.
    fireEvent.click(await findByTestId("answered-toggle"));
    const card = await findByTestId("answered-card");

    const summary = card.querySelector(".reply-card__summary")!;
    // positive control — the scope really holds the summary's text
    expect(summary.textContent).toContain("要不要合");
    expect(summary.querySelector("strong")?.textContent).toBe("fms #20054");
    expect(summary.querySelector("code")?.textContent).toBe("919fe961");
    expect(summary.textContent).not.toContain("**fms #20054**");
    expect(summary.textContent).not.toContain("`919fe961`");
  });
});
