// useSuggestedReplies (T-122) — the suggestions are a convenience laid over a
// reply box that must keep working on its own, so the one thing this hook may
// never do is take its host down.
//
// The synchronous-throw case is not hypothetical: it was MEASURED. A settings
// read that threw rather than rejected crashed the whole task card —
// `TaskCard.artifact-freeze.test.tsx` went red with
// "api.getServerSettings is not a function" and React unmounted the card,
// message box and all. A `.catch` alone does not cover it.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import {
  useSuggestedRepliesReplyCard,
  useSuggestedRepliesTaskMessage,
} from "./useSuggestedReplies";
import * as shared from "./sharedServerSettings";

function Host() {
  const replyCard = useSuggestedRepliesReplyCard();
  const taskMessage = useSuggestedRepliesTaskMessage();
  return (
    <div>
      <textarea data-testid="box" />
      <span data-testid="count">{replyCard.length}</span>
      <span data-testid="first">{replyCard[0]}</span>
      <span data-testid="second">{replyCard[1]}</span>
      <span data-testid="task-count">{taskMessage.length}</span>
    </div>
  );
}

describe("useSuggestedReplies", () => {
  beforeEach(() => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("answers the owner's configured sentences once the settings read lands", async () => {
    vi.spyOn(shared, "loadServerSettings").mockResolvedValue({
      suggestedRepliesReplyCard: ["收到", "照做"],
      suggestedRepliesTaskMessage: ["先擱著"],
    } as Awaited<ReturnType<typeof shared.loadServerSettings>>);
    const { getByTestId } = render(<Host />);
    await waitFor(() => expect(getByTestId("count").textContent).toBe("2"));
    expect(getByTestId("task-count").textContent).toBe("1");
  });

  it("reads its OWN list — one being empty leaves the other alone", async () => {
    vi.spyOn(shared, "loadServerSettings").mockResolvedValue({
      suggestedRepliesReplyCard: [] as string[],
      suggestedRepliesTaskMessage: ["先擱著", "之後再說"],
    } as Awaited<ReturnType<typeof shared.loadServerSettings>>);
    const { getByTestId } = render(<Host />);
    await waitFor(() => expect(getByTestId("task-count").textContent).toBe("2"));
    expect(getByTestId("count").textContent).toBe("0");
  });

  // 🔴 THE RETURN VALUE IS A DOOR TOO, AND IT WAS OPEN.
  //
  // The try/catch in this hook guards a read that THROWS or REJECTS. A read that
  // RESOLVES the wrong shape walks past it, and the hook then hands `undefined`
  // / `null` / a string to whoever asked — which throws in the CALLER's render.
  // An independent review reproduced exactly that. These cases assert at the
  // HOOK boundary on purpose: SuggestedReplies has its own guard, so a composer-
  // level test passes even with this one removed, and a guard nothing can fail
  // is a guard nobody will keep.
  it.each([
    ["the fields are absent", {}],
    ["a list is null", { suggestedRepliesReplyCard: null, suggestedRepliesTaskMessage: null }],
    ["a list is a string", { suggestedRepliesReplyCard: "收到", suggestedRepliesTaskMessage: "先擱著" }],
    ["the settings object is null", null],
    ["a list holds non-strings", { suggestedRepliesReplyCard: [42, null], suggestedRepliesTaskMessage: [{}] }],
  ])("answers the empty list when the read RESOLVES but %s", async (_l, payload) => {
    vi.spyOn(shared, "loadServerSettings").mockResolvedValue(
      payload as Awaited<ReturnType<typeof shared.loadServerSettings>>
    );
    const { getByTestId } = render(<Host />);
    await waitFor(() => expect(getByTestId("box")).toBeTruthy());
    expect(getByTestId("count").textContent).toBe("0");
    expect(getByTestId("task-count").textContent).toBe("0");
  });

  // 🔴 ASSERTED AT THE HOOK BOUNDARY ON PURPOSE. SuggestedReplies drops blanks
  // too, so a composer-level test passes with this layer removed — the same
  // mutual masking that made the F-1 hook guard untestable through a component.
  it("drops whitespace-only entries, and trims the ones it keeps", async () => {
    vi.spyOn(shared, "loadServerSettings").mockResolvedValue({
      suggestedRepliesReplyCard: ["   ", "收到", "  照做  "],
      suggestedRepliesTaskMessage: ["\t", " "],
    } as unknown as Awaited<ReturnType<typeof shared.loadServerSettings>>);
    const { getByTestId } = render(<Host />);
    await waitFor(() => expect(getByTestId("count").textContent).toBe("2"));
    expect(getByTestId("first").textContent).toBe("收到");
    expect(getByTestId("second").textContent).toBe("照做");
    // Every entry blank ⇒ the same answer as "none configured".
    expect(getByTestId("task-count").textContent).toBe("0");
  });

  it("answers the empty list, and leaves the box standing, when the read REJECTS", async () => {
    vi.spyOn(shared, "loadServerSettings").mockRejectedValue(
      new Error("http 500 for GET /api/settings")
    );
    const { getByTestId } = render(<Host />);
    await waitFor(() => expect(getByTestId("box")).toBeTruthy());
    expect(getByTestId("count").textContent).toBe("0");
    expect(getByTestId("task-count").textContent).toBe("0");
  });

  it("answers the empty list, and leaves the box standing, when the read THROWS", async () => {
    vi.spyOn(shared, "loadServerSettings").mockImplementation(() => {
      throw new TypeError("api.getServerSettings is not a function");
    });
    const { getByTestId } = render(<Host />);
    await waitFor(() => expect(getByTestId("box")).toBeTruthy());
    expect(getByTestId("count").textContent).toBe("0");
  });
});
