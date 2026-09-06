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
import { useSuggestedReplies } from "./useSuggestedReplies";
import * as shared from "./sharedServerSettings";

function Host() {
  const replies = useSuggestedReplies();
  return (
    <div>
      <textarea data-testid="box" />
      <span data-testid="count">{replies.length}</span>
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
      suggestedReplies: ["收到", "照做"],
    } as Awaited<ReturnType<typeof shared.loadServerSettings>>);
    const { getByTestId } = render(<Host />);
    await waitFor(() => expect(getByTestId("count").textContent).toBe("2"));
  });

  it("answers the empty list, and leaves the box standing, when the read REJECTS", async () => {
    vi.spyOn(shared, "loadServerSettings").mockRejectedValue(
      new Error("http 500 for GET /api/settings")
    );
    const { getByTestId } = render(<Host />);
    await waitFor(() => expect(getByTestId("box")).toBeTruthy());
    expect(getByTestId("count").textContent).toBe("0");
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
