// LorePage — the 建議回覆 row under the 傳承 entry's message box (T-33).
//
// The third of the owner's 建議回覆 lists (rc-01a07b1b2a12 [2]) has a settings
// row of its own — SettingsPage.suggested-replies-lore-t33.test.tsx pins that
// end — and this file pins the other end: the box actually reads THAT list.
//
// 🔴 "IT RENDERS CHIPS" IS NOT THE ASSERTION. A box wired to the 任務 list would
// pass it while offering the owner a sentence written for a different
// conversation, one tap from being sent to a person. So the fixture gives the
// three lists DIFFERENT sentences and the assertion names which one may appear.
//
// The empty case is the shipped state (and the answer to a failed settings
// read), so it is asserted too: nothing at all, not an empty row.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { LorePage } from "./LorePage";
import { __resetMock, mockApi } from "../api/mock";
import { api } from "../api";
import type { LoreEntryPageView, LoreEntryView } from "../api/adapter";

function mkEntry(over: Partial<LoreEntryView> & { id: string }): LoreEntryView {
  return {
    seq: 1,
    scopeKind: "agent",
    scopeKey: "mira",
    title: `標題 ${over.id}`,
    body: `內容 ${over.id}`,
    authorId: "mira",
    sourceTaskId: "",
    state: "active",
    retireReason: "",
    effectiveTs: 1788400000,
    createdTs: 1788400000,
    updatedTs: 1788400000,
    ...over,
  };
}

function page(entries: LoreEntryView[]): LoreEntryPageView {
  return { entries, limit: 30, offset: 0, capChars: 0, firstDroppedId: "" };
}

function stubList(entries: LoreEntryView[]) {
  return vi.spyOn(api, "listLoreEntries").mockResolvedValue(page(entries));
}

function renderPage() {
  return render(
    <I18nProvider>
      <LorePage />
    </I18nProvider>,
  );
}

beforeEach(() => {
  __resetMock();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("LorePage — 傳承訊息建議回覆接的是自己那一份清單", () => {
  it("renders the 傳承 list's sentences, not the 任務 or 請示卡 one's", async () => {
    // Three DIFFERENT sentences, so "chips appeared" cannot be mistaken for
    // "the right list appeared".
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["請示卡那一份"],
      suggestedRepliesTaskMessage: ["任務那一份"],
      suggestedRepliesLoreMessage: ["這條還適用嗎", "這條可以退場了"],
    });
    stubList([mkEntry({ id: "L-11", authorId: "mira" })]);
    const { container } = renderPage();

    const chips = await waitFor(() => {
      const el = container.querySelector<HTMLElement>(
        '[data-testid="lore-suggested-replies"]',
      );
      if (!el) throw new Error("no 建議回覆 row under the 傳承 box");
      return el;
    });
    const texts = Array.from(chips.querySelectorAll("button")).map((b) =>
      b.textContent?.trim(),
    );
    expect(texts).toEqual(["這條還適用嗎", "這條可以退場了"]);
    expect(texts).not.toContain("任務那一份");
    expect(texts).not.toContain("請示卡那一份");

    // A pick FILLS the box; it does not send. The message goes to a person and
    // cannot be recalled, so this half is the point of the component.
    const posted: unknown[] = [];
    vi.spyOn(api, "postChat").mockImplementation(async (m) => {
      posted.push(m);
    });
    fireEvent.click(chips.querySelectorAll("button")[0]);
    const box = container.querySelector<HTMLTextAreaElement>(
      '[data-testid="lore-msg-input"]',
    )!;
    await waitFor(() => expect(box.value).toBe("這條還適用嗎"));
    expect(posted).toHaveLength(0);
  });

  it("renders nothing at all when the 傳承 list is empty", async () => {
    // The shipped state, and the answer to a failed settings read. The box has
    // to look untouched in it — not an empty row, not a heading.
    await mockApi.patchServerSettings({
      suggestedRepliesTaskMessage: ["任務那一份"],
      suggestedRepliesLoreMessage: [],
    });
    stubList([mkEntry({ id: "L-12", authorId: "mira" })]);
    const { container } = renderPage();
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-msg-input"]'),
      ).not.toBeNull(),
    );
    expect(
      container.querySelector('[data-testid="lore-suggested-replies"]'),
    ).toBeNull();
  });
});
