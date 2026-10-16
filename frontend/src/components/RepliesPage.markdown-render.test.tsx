// T-13af — the 等我回覆頁 card's body (卡片內文, ReplyCardDTO.body) is
// agent-authored free text and must render through the shared, XSS-safe
// `Markdown` component — same contract as the chat/task inline reply cards
// (ReplyCardBody.tsx is shared; the body row itself lives in each wrapper).

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RepliesPage } from "./RepliesPage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import { AttachmentModalHost } from "./AttachmentModalHost";
import { api } from "../api";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
import type { ReplyCard } from "../api/adapter";

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要幫你寄出這封信嗎？",
    body: "",
    options: [{ text: "寄出", aiPick: true }, { text: "先不要", aiPick: false }],
    selectMode: "single",
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
      <AttachmentModalHost>
        <ReplyCardsProvider>
          <RepliesPage />
        </ReplyCardsProvider>
      </AttachmentModalHost>
    </I18nProvider>
  );
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
    await openCards();

    const body = card.querySelector(".reply-card__body")!;
    expect(body.querySelector("strong")?.textContent).toBe("注意");
    expect(body.querySelectorAll("ul > li").length).toBe(2);
    expect(body.textContent).not.toContain("**注意**");
  });

  it.each(["answered", "expired"] as const)(
    "shows the complete original question as Markdown when an %s card is opened",
    async (status) => {
      __injectMockReplyCard(
        mkCard({
          body: "**注意**:\n- 會寄到客戶信箱\n- 無法撤回",
          status,
          answeredTs: status === "answered" ? Date.now() / 1000 - 60 : null,
          expiredTs: status === "expired" ? Date.now() / 1000 - 60 : null,
          answer: status === "answered"
            ? { optionIdxs: [0], text: "", attachments: [] }
            : null,
        })
      );
      const { findByTestId } = renderPage();
      fireEvent.click(await findByTestId("answered-toggle"));
      const card = await findByTestId(status + "-card");
      expect(card.querySelector(".reply-card__body")).toBeNull();
      await openCards();

      const bodies = card.querySelectorAll(".reply-card__body");
      expect(bodies).toHaveLength(1);
      expect(bodies[0].querySelector("strong")?.textContent).toBe("注意");
      expect([...bodies[0].querySelectorAll("ul > li")].map((li) => li.textContent))
        .toEqual(["會寄到客戶信箱", "無法撤回"]);
      expect(bodies[0].textContent).toBe("注意:會寄到客戶信箱無法撤回");
      expect(card.querySelector('[data-testid="' +
        (status === "answered" ? "final-answer" : "expired-note") + '"]')).not.toBeNull();

      fireEvent.click(card);
      await waitFor(() => expect(card.querySelector(".reply-card__body")).toBeNull());
    }
  );

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
    await openCards();

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
    await openCards();

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
    await openCards();

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
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    const { findByTestId } = renderPage();
    // 近期已處理 is collapsed by default — open it to reach the handled card.
    fireEvent.click(await findByTestId("answered-toggle"));
    const card = await findByTestId("answered-card");
    await openCards();

    const summary = card.querySelector(".reply-card__summary")!;
    // positive control — the scope really holds the summary's text
    expect(summary.textContent).toContain("要不要合");
    expect(summary.querySelector("strong")?.textContent).toBe("fms #20054");
    expect(summary.querySelector("code")?.textContent).toBe("919fe961");
    expect(summary.textContent).not.toContain("**fms #20054**");
    expect(summary.textContent).not.toContain("`919fe961`");
  });

  it("sends comments on a document linked from a card's body to the card's author", async () => {
    const realFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "# 合約" })) as unknown as typeof fetch;
    const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
    localStorage.setItem(
      "oc_doc_comments:att-eeeeeeeeeeee",
      JSON.stringify([{ id: "c1", excerpt: "合約", comment: "第三條要改" }]),
    );
    __injectMockReplyCard(mkCard({ body: "請看 [合約.md](/api/chat/attachment/att-eeeeeeeeeeee)" }));
    try {
      const { findAllByTestId, findByRole, getByRole } = renderPage();
      const [card] = await findAllByTestId("waiting-card");
      await openCards();

      fireEvent.click(within(card).getByRole("link", { name: "合約.md" }));
      await findByRole("heading", { name: "合約" });
      const overlay = document.body.querySelector<HTMLElement>(".md-preview[role=dialog]")!;
      fireEvent.click(within(overlay).getByRole("button", { name: "註解" }));
      expect(overlay.querySelector(".doc-comments__recipient strong")?.textContent).toBe("Mira");
      await waitFor(() =>
        expect((getByRole("button", { name: "送出給 AI" }) as HTMLButtonElement).disabled).toBe(false),
      );
      fireEvent.click(getByRole("button", { name: "送出給 AI" }));
      await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
      expect(post.mock.calls[0][0].to).toBe("mira");
    } finally {
      globalThis.fetch = realFetch;
      localStorage.clear();
    }
  });

  it("opens a document linked from an owner-authored card read-only, and names the reason on a departed author's card", async () => {
    const realFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "# 合約" })) as unknown as typeof fetch;
    __injectMockReplyCard(
      mkCard({ id: "rc-owner", from: "owner", body: "[自己的.md](/api/chat/attachment/att-eeeeeeeeeee1)" }),
    );
    __injectMockReplyCard(
      mkCard({ id: "rc-gone", from: "m-1892d870ded7", body: "[交接.md](/api/chat/attachment/att-eeeeeeeeeee2)" }),
    );
    const overlay = () => document.body.querySelector<HTMLElement>(".md-preview[role=dialog]");
    try {
      const { findAllByTestId, findByRole, findByText } = renderPage();
      await findAllByTestId("waiting-card");
      await openCards();

      fireEvent.click(await findByRole("link", { name: "自己的.md" }));
      await findByRole("heading", { name: "合約" });
      expect(within(overlay()!).queryByRole("button", { name: "註解" })).toBeNull();
      fireEvent.keyDown(window, { key: "Escape" });
      await waitFor(() => expect(overlay()).toBeNull());

      fireEvent.click(await findByRole("link", { name: "交接.md" }));
      await findByRole("heading", { name: "合約" });
      fireEvent.click(within(overlay()!).getByRole("button", { name: "註解" }));
      expect(await findByText("對方已不在名冊上（已移除或已釋出），無法送出。")).toBeTruthy();
    } finally {
      globalThis.fetch = realFetch;
    }
  });
});
