// A secret card behaves the same on all three surfaces that show one: the same
// box and faces, the same write, and focus only when the owner opens the card.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatReplyCard } from "./ChatReplyCard";
import { TaskReplyCard } from "./TaskReplyCard";
import { RepliesPage } from "./RepliesPage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import { api } from "../api";
import { ApiError } from "../api/errors";
import {
  __injectMockReplyCard,
  __mockRetrieveSecretCard,
  __resetMock,
} from "../api/mock";
import type { ReplyCard } from "../api/adapter";
import { watchSecretValue } from "../test/secretValueWatch";

const SENTINEL = "dummy-aaaaaaaaaaaa";
const CARD_ID = "rc-aaaaaaaaaaaa";

function mkCard(over: Partial<ReplyCard> = {}): ReplyCard {
  return {
    id: CARD_ID,
    from: "mira",
    kind: "secret",
    summary: "部署金鑰",
    body: "放到 deploy 主機",
    options: [],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 60,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    secret: { state: "waiting", providedTs: null, retrievedTs: null, expiresTs: null },
    ...over,
  };
}

type Utils = ReturnType<typeof render>;

function withBystander(node: React.ReactNode) {
  return (
    <I18nProvider>
      <input data-testid="bystander" />
      {node}
    </I18nProvider>
  );
}

const surfaces: {
  name: string;
  mount: () => Utils;
  open: (u: Utils) => Promise<void>;
  /** Whether opening means a gesture the box may answer with focus. */
  focusesOnOpen: boolean;
  /** Brings a settled card back into view; the replies page files it under a
   * collapsed pane, as it does any answered card. */
  showSettled: (u: Utils) => Promise<void>;
}[] = [
  {
    name: "chat",
    mount: () =>
      render(
        withBystander(
          <ChatReplyCard replyCardId={CARD_ID} fallbackSummary="部署金鑰" initialStatus="waiting" />,
        ),
      ),
    open: async (u) => {
      fireEvent.click(u.getByTestId("chat-reply-card-expand"));
    },
    focusesOnOpen: true,
    showSettled: async () => {},
  },
  {
    name: "task",
    mount: () => render(withBystander(<TaskReplyCard replyCardId={CARD_ID} />)),
    open: async () => {},
    focusesOnOpen: false,
    showSettled: async () => {},
  },
  {
    name: "replies page",
    mount: () =>
      render(
        withBystander(
          <ReplyCardsProvider>
            <RepliesPage />
          </ReplyCardsProvider>,
        ),
      ),
    open: async (u) => {
      const article = await waitFor(() => {
        const el = u.container.querySelector(`[data-reply-card-id="${CARD_ID}"][aria-expanded]`);
        expect(el).not.toBeNull();
        return el as HTMLElement;
      });
      fireEvent.click(article);
    },
    focusesOnOpen: true,
    showSettled: async (u) => {
      const toggle = await u.findByTestId("answered-toggle");
      if (toggle.getAttribute("aria-expanded") === "false") fireEvent.click(toggle);
    },
  },
];

beforeEach(() => {
  __resetMock();
  localStorage.clear();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe.each(surfaces)("$name", ({ mount, open, focusesOnOpen, showSettled }) => {
  async function provideInBox(u: Utils) {
    fireEvent.change(u.getByTestId("reply-secret-input"), { target: { value: SENTINEL } });
    fireEvent.click(u.getByTestId("reply-secret-send"));
    await waitFor(() => expect(u.queryByTestId("reply-secret-input")).toBeNull());
    await showSettled(u);
    await u.findByTestId("secret-face");
  }

  async function opened() {
    __injectMockReplyCard(mkCard());
    const u = mount();
    await open(u);
    await u.findByTestId("reply-secret-box");
    return u;
  }

  it("shows the box and the composer in place of options", async () => {
    const u = await opened();
    expect(u.queryAllByTestId("reply-option")).toEqual([]);
    expect(u.container.querySelector(".reply-composer")).not.toBeNull();
  });

  it(`${focusesOnOpen ? "focuses the box when opened" : "has no open gesture, so never takes focus"}`, async () => {
    const u = await opened();
    const input = u.getByTestId("reply-secret-input");
    if (focusesOnOpen) expect(document.activeElement).toBe(input);
    else expect(document.activeElement).not.toBe(input);
  });

  it("does not take focus back on a re-render", async () => {
    const u = await opened();
    const bystander = u.getByTestId("bystander") as HTMLInputElement;
    bystander.focus();
    await act(async () => {
      __injectMockReplyCard(mkCard({ id: "rc-bbbbbbbbbbbb", summary: "另一張" }));
    });
    await act(async () => {});
    expect(document.activeElement).toBe(bystander);
  });

  it("provides through the secret route, clears the box and shows the provided face", async () => {
    const placesHoldingValue = watchSecretValue(SENTINEL);
    const provide = vi.spyOn(api, "provideReplyCardSecret");
    const answer = vi.spyOn(api, "answerReplyCard");
    const u = await opened();
    await provideInBox(u);
    expect(placesHoldingValue()).toEqual([]);
    const face = await u.findByTestId("secret-face");
    expect(face.getAttribute("data-secret-state")).toBe("provided");
    expect(provide).toHaveBeenCalledWith(CARD_ID, SENTINEL);
    expect(answer).not.toHaveBeenCalled();
    expect(face.textContent).toBe("已提供");
    expect(u.container.textContent).not.toContain(SENTINEL);
    expect(u.queryByTestId("reply-secret-input")).toBeNull();
  });

  it("keeps the value and says so when the write fails, without quoting it", async () => {
    const placesHoldingValue = watchSecretValue(SENTINEL);
    vi.spyOn(api, "provideReplyCardSecret").mockRejectedValue(new ApiError("http 500", 500, "internal_error", "boom"));
    const u = await opened();
    const input = u.getByTestId("reply-secret-input") as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: SENTINEL } });
    fireEvent.click(u.getByTestId("reply-secret-send"));
    await waitFor(() => expect(u.container.textContent).toContain("送出失敗"));
    expect(input.value).toBe(SENTINEL);
    const errors = u.container.querySelectorAll(".reply-card__error, .replies__error");
    expect(errors.length).toBeGreaterThan(0);
    for (const line of errors) expect(line.textContent).not.toContain(SENTINEL);
    expect(placesHoldingValue()).toEqual([]);
  });

  it("a typed answer goes through the composer as usual and reads as replied", async () => {
    const u = await opened();
    const composer = u.container.querySelector(".reply-composer textarea") as HTMLTextAreaElement;
    fireEvent.change(composer, { target: { value: "哪個環境?" } });
    fireEvent.keyDown(composer, { key: "Enter" });
    await waitFor(() => expect(u.queryByTestId("reply-secret-input")).toBeNull());
    await showSettled(u);
    const answer = await u.findByTestId("final-answer");
    expect(answer.textContent).toContain("哪個環境?");
    expect(u.queryByTestId("secret-face")).toBeNull();
  });

  it("follows the author's retrieval in place", async () => {
    const u = await opened();
    await provideInBox(u);
    expect((await u.findByTestId("secret-face")).textContent).toBe("已提供");
    await act(async () => {
      __mockRetrieveSecretCard(CARD_ID);
    });
    await waitFor(() =>
      expect(u.getByTestId("secret-face").getAttribute("data-secret-state")).toBe("retrieved"),
    );
    expect(u.getByTestId("secret-face").textContent).toBe("已取得");
  });

});

describe("the replies page's handled pane", () => {
  it("a card opened by its link follows the author's retrieval in place", async () => {
    const ts = Date.now() / 1000 - 60;
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: ts,
        answer: { optionIdxs: null, text: "", attachments: [] },
        secret: { state: "provided", providedTs: ts, retrievedTs: null, expiresTs: ts + 3600 },
      }),
    );
    const u = render(
      withBystander(
        <ReplyCardsProvider>
          <RepliesPage />
        </ReplyCardsProvider>,
      ),
    );
    const idField = await u.findByTestId("filter-reply-card-id");
    fireEvent.change(idField, { target: { value: CARD_ID } });
    fireEvent.keyDown(idField, { key: "Enter" });
    const article = await waitFor(() => {
      const el = u.container.querySelector(`[data-reply-card-id="${CARD_ID}"][aria-expanded]`);
      expect(el).not.toBeNull();
      return el as HTMLElement;
    });
    if (article.getAttribute("aria-expanded") === "false") fireEvent.click(article);
    expect((await u.findByTestId("secret-face")).textContent).toBe("已提供");
    await act(async () => {
      __mockRetrieveSecretCard(CARD_ID);
    });
    await waitFor(() => expect(u.getByTestId("secret-face").textContent).toBe("已取得"));
  });

  it("stamps a settled secret card with its state and no time", async () => {
    const ts = Date.now() / 1000 - 60;
    const settled = { status: "answered" as const, answeredTs: ts };
    const answer = { optionIdxs: null, text: "", attachments: [] };
    const held = (state: "provided" | "retrieved") => ({
      state,
      providedTs: ts,
      retrievedTs: state === "retrieved" ? ts : null,
      expiresTs: ts + 3600,
    });
    __injectMockReplyCard(mkCard({ id: "rc-aaaaaaaaaaa1", ...settled, answer, secret: held("provided") }));
    __injectMockReplyCard(mkCard({ id: "rc-aaaaaaaaaaa2", ...settled, answer, secret: held("retrieved") }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-aaaaaaaaaaa3",
        status: "expired",
        expiredTs: ts,
        secret: { state: "expired", providedTs: ts, retrievedTs: null, expiresTs: null },
      }),
    );
    const u = render(
      withBystander(
        <ReplyCardsProvider>
          <RepliesPage />
        </ReplyCardsProvider>,
      ),
    );
    fireEvent.click(await u.findByTestId("answered-toggle"));
    const stamp = async (id: string) =>
      waitFor(() => {
        const el = u.container.querySelector(`[data-reply-card-id="${id}"] .reply-card__answered-at`);
        expect(el).not.toBeNull();
        return el!.textContent;
      });
    expect(await stamp("rc-aaaaaaaaaaa1")).toBe("已提供");
    expect(await stamp("rc-aaaaaaaaaaa2")).toBe("已取得");
    expect(await stamp("rc-aaaaaaaaaaa3")).toBe("已失效");
  });
});

describe("arrival", () => {
  it("a card that merely arrives on the task surface leaves focus where it was", async () => {
    const u = render(withBystander(<TaskReplyCard replyCardId={CARD_ID} initialStatus="waiting" />));
    const bystander = u.getByTestId("bystander") as HTMLInputElement;
    bystander.focus();
    await act(async () => {
      __injectMockReplyCard(mkCard());
    });
    await u.findByTestId("reply-secret-box");
    expect(document.activeElement).toBe(bystander);
  });

  it("a card that arrives on the replies page stays shut and takes no focus", async () => {
    const u = render(
      withBystander(
        <ReplyCardsProvider>
          <RepliesPage />
        </ReplyCardsProvider>,
      ),
    );
    const bystander = u.getByTestId("bystander") as HTMLInputElement;
    bystander.focus();
    await act(async () => {
      __injectMockReplyCard(mkCard());
    });
    await waitFor(() =>
      expect(u.container.querySelector(`[data-reply-card-id="${CARD_ID}"]`)).not.toBeNull(),
    );
    expect(u.queryByTestId("reply-secret-box")).toBeNull();
    expect(document.activeElement).toBe(bystander);
  });

  it("a collapsed chat card stays a stub when a delta re-reads it", async () => {
    __injectMockReplyCard(mkCard());
    const u = render(
      withBystander(<ChatReplyCard replyCardId={CARD_ID} fallbackSummary="部署金鑰" initialStatus="waiting" />),
    );
    const bystander = u.getByTestId("bystander") as HTMLInputElement;
    bystander.focus();
    await act(async () => {
      __injectMockReplyCard(mkCard({ id: "rc-bbbbbbbbbbbb" }));
    });
    expect(u.queryByTestId("reply-secret-box")).toBeNull();
    expect(document.activeElement).toBe(bystander);
  });
});
