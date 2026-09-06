// The 建議回覆 row itself (T-122) — what it draws, and what a click hands back.
// The two boxes that mount it are pinned separately
// (ReplyComposer.suggested-replies.test.tsx,
// TaskCardMessageBox.suggested-replies.test.tsx); this file is the row alone.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { appendSuggestion, SuggestedReplies } from "./SuggestedReplies";

describe("SuggestedReplies", () => {
  it("draws one button per configured sentence, in order", () => {
    const { getByTestId } = render(
      <SuggestedReplies
        replies={["收到", "先擱著", "照做"]}
        onPick={() => {}}
        testId="row"
      />
    );
    const chips = getByTestId("row").querySelectorAll("button");
    expect([...chips].map((c) => c.textContent)).toEqual([
      "收到",
      "先擱著",
      "照做",
    ]);
  });

  it("renders nothing at all when the owner has configured no sentences", () => {
    const { queryByTestId, container } = render(
      <SuggestedReplies replies={[]} onPick={() => {}} testId="row" />
    );
    expect(queryByTestId("row")).toBeNull();
    expect(container.innerHTML).toBe("");
  });

  it("hands the clicked sentence to the box verbatim", () => {
    const onPick = vi.fn();
    const { getByText } = render(
      <SuggestedReplies
        replies={["收到", "先擱著"]}
        onPick={onPick}
        testId="row"
      />
    );
    fireEvent.click(getByText("先擱著"));
    expect(onPick).toHaveBeenCalledTimes(1);
    expect(onPick).toHaveBeenCalledWith("先擱著");
  });

  it("draws a chip per entry even when two entries read the same", () => {
    const { getByTestId } = render(
      <SuggestedReplies replies={["收到", "收到"]} onPick={() => {}} testId="row" />
    );
    expect(getByTestId("row").querySelectorAll("button")).toHaveLength(2);
  });

  // 🔴 A BAD `replies` MUST DRAW NOTHING, NOT THROW.
  //
  // This is a SHARED component and the value reaches it from a settings payload
  // the frontend cannot verify. `replies.length` on undefined/null and
  // `replies.map` on a string both throw DURING RENDER, and a render throw
  // unmounts the whole subtree — which at both mount points is the reply box
  // with the owner's half-typed draft in it. An independent review reproduced
  // all three by resolving a settings read with the fields absent / null / a
  // string. The type says `readonly string[]`; the type is not the guard.
  it.each([
    ["absent", undefined],
    ["null", null],
    ["a string", "收到"],
    ["an array of non-strings", [42, null, { a: 1 }]],
  ])("draws nothing, without throwing, when replies is %s", (_label, bad) => {
    const { queryByTestId } = render(
      <SuggestedReplies
        replies={bad as unknown as readonly string[]}
        onPick={() => {}}
        testId="row"
      />
    );
    expect(queryByTestId("row")).toBeNull();
  });

  it("keeps the usable sentences when only SOME entries are unusable", () => {
    const { getByTestId } = render(
      <SuggestedReplies
        replies={["收到", 42, null, "照做"] as unknown as readonly string[]}
        onPick={() => {}}
        testId="row"
      />
    );
    expect(
      [...getByTestId("row").querySelectorAll("button")].map((b) => b.textContent)
    ).toEqual(["收到", "照做"]);
  });
});

describe("appendSuggestion", () => {
  it("is the whole draft when nothing has been typed yet", () => {
    expect(appendSuggestion("", "收到")).toBe("收到");
  });

  it("goes on its own line after a draft already in progress, keeping it", () => {
    expect(appendSuggestion("我看過了", "收到")).toBe("我看過了\n收到");
  });

  it("keeps every earlier pick when several are folded in", () => {
    expect(appendSuggestion(appendSuggestion("", "收到"), "照做")).toBe(
      "收到\n照做"
    );
  });
});
