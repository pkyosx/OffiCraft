// LorePage — the owner rulings on card rc-01a07b1b2a12 / rc-379631993586 that
// land on ONE box: the message a reader sends to the person who wrote a 傳承
// entry. (The THIRD ruling from that card — the 建議回覆 list under this box —
// lives in LorePage.suggested-replies-t33.test.tsx, beside the settings row it
// is configured from.)
//
//   A. The visible prefix is 「[LoreID=L-7] 」, not 「[L-7] 」. The old shape put
//      an id on the wire without ever saying what kind of id it was, next to a
//      任務 box sending 「[T-33] 」 — two namespaces, one indistinguishable
//      bracket. The owner picked the spelled-out form OVER a translated label
//      precisely because it must not move with the cockpit's language, so the
//      assertions below are on the LITERAL BYTES. A test that rebuilt the
//      expected string from the same template the component uses would agree
//      with the old shape too.
//
//   B. `meta.lore_entry_id` rides beside it. The prefix is for a PERSON and is
//      display — the owner has already deleted one thing that sat next to it —
//      while an agent receiving the message must be able to say WHICH ENTRY it
//      is about without parsing a human-facing string. Asserting the prefix
//      alone would pass for a message no program can route.
//
//   D. A 外包 who has left gets his NAME on the row and NO message box. Before
//      this, a released worker whose codename still resolved got a live-looking
//      composer and the refusal arrived from the SERVER after the reader had
//      typed and pressed send. The two halves are one spec on purpose: a test
//      that only checked "the box is gone" would also pass for a row that had
//      quietly stopped saying who wrote the entry, which is the thing the
//      ruling explicitly protects.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { LorePage } from "./LorePage";
import { __resetMock } from "../api/mock";
import { __resetWorkerCodenameCache } from "../hooks/useWorkerCodenames";
import { api } from "../api";
import type {
  LoreEntryPageView,
  LoreEntryView,
  OutsourceWorkerView,
} from "../api/adapter";

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

function rowById(container: HTMLElement, id: string): HTMLElement {
  const el = container.querySelector<HTMLElement>(
    `[data-testid="lore-row"][data-entry-id="${id}"]`,
  );
  if (!el) throw new Error(`no rendered row for ${id}`);
  return el;
}

beforeEach(() => {
  __resetMock();
  __resetWorkerCodenameCache();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("LorePage — 傳承訊息的前綴與 meta (rc-01a07b1b2a12 / rc-379631993586)", () => {
  /** Send one message from the box on entry `entryId` and answer what
   * `api.postChat` was handed — the WHOLE argument, not a chosen field: `meta`
   * is exactly the part an assertion on `body` alone cannot see. */
  async function sendFrom(entryId: string, typed: string) {
    const posted: Parameters<typeof api.postChat>[0][] = [];
    vi.spyOn(api, "postChat").mockImplementation(async (m) => {
      posted.push(m);
    });
    stubList([mkEntry({ id: entryId, authorId: "mira" })]);
    const { container } = renderPage();
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-msg-input"]'),
      ).not.toBeNull(),
    );
    fireEvent.change(
      container.querySelector<HTMLTextAreaElement>(
        '[data-testid="lore-msg-input"]',
      )!,
      { target: { value: typed } },
    );
    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-msg-send"]')!,
    );
    await waitFor(() => expect(posted).toHaveLength(1));
    return posted[0];
  }

  it("leads the body with 「[LoreID=…] 」 — the literal, not the bare id", async () => {
    const sent = await sendFrom("L-7", "這條還適用嗎");
    // 🔴 THE WHOLE STRING. `toContain("L-7")` passes for 「[L-7] …」, which is
    // the shape this ruling replaced, and for an id appended after the
    // sentence, which does not solve what the prefix is for.
    expect(sent.body).toBe("[LoreID=L-7] 這條還適用嗎");
    expect(sent.to).toBe("mira");
  });

  it("carries meta.lore_entry_id, which no reader has to parse the body for", async () => {
    const sent = await sendFrom("L-31", "這條還適用嗎");
    expect(sent.meta).toEqual({ lore_entry_id: "L-31" });
    // Both halves, in one spec, because they answer to different readers and
    // either one can be lost without touching the other.
    expect(sent.body).toBe("[LoreID=L-31] 這條還適用嗎");
  });
});

describe("LorePage — 已離開的外包只留名牌，不留輸入框", () => {
  const RELEASED = "ow-gone";
  const LIVE = "ow-live";

  function worker(id: string, codename: string): OutsourceWorkerView {
    return {
      id,
      codename,
      model: "claude-opus-5",
      effort: "medium",
      taskId: "t-1111aaaabbbb",
    } as OutsourceWorkerView;
  }

  /** The roster the page resolves against. `members` deliberately holds NEITHER
   * worker: the server drops roster_status='removed' rows from /api/members, so
   * a departed 外包 is exactly an id that list cannot answer for — which is why
   * the ow- arm has to decide reachability from the LIVE outsource list. */
  function stubRoster(live: OutsourceWorkerView[]) {
    vi.spyOn(api, "listMembers").mockResolvedValue([]);
    vi.spyOn(api, "listOutsourceWorkers").mockResolvedValue(live);
    // The per-id route DOES serve released rows — that is what still resolves a
    // departed writer's codename (useWorkerCodenames).
    vi.spyOn(api, "getOutsourceWorker").mockImplementation(async (id) => {
      if (id === RELEASED) return worker(RELEASED, "O-9");
      if (id === LIVE) return worker(LIVE, "O-1");
      throw new Error(`unknown worker ${id}`);
    });
  }

  it("keeps the codename on the row and drops the composer under it", async () => {
    stubRoster([]); // nobody is on the live roster any more
    stubList([mkEntry({ id: "L-9", authorId: RELEASED })]);
    const { container } = renderPage();

    const row = await waitFor(() => rowById(container, "L-9"));

    // ① THE NAME SURVIVES. He wrote this entry and that does not stop being
    //    true — the row must still say 外包 · O-9, not fall back to a raw id
    //    and not go blank.
    //
    //    🔴 IT IS READ OFF **EITHER** PILL ELEMENT ON PURPOSE. The two arms are
    //    different elements (a <button> when reachable, a <span> when not), so
    //    a query naming only the span would throw a shape error under the very
    //    mutant this spec exists to catch — a red in the wrong place, saying
    //    nothing about whether the name survived. This half must be readable
    //    whichever arm rendered; ② is where the arm itself is judged.
    const pill = await waitFor(() => {
      const el = row.querySelector<HTMLElement>(
        '[data-testid="lore-author-row"], [data-testid="lore-author-link"]',
      );
      if (!el) throw new Error("no 撰寫人 pill on the row at all");
      return el;
    });
    expect(pill.textContent).toContain("O-9");

    // ② THE AFFORDANCE GOES — both halves of it. The pill is a plain span (a
    //    dead clickable pill would pass an "is the icon gone" check), and the
    //    box is not rendered at all.
    expect(row.querySelector('[data-testid="lore-author-composer"]')).toBeNull();
    expect(row.querySelector('[data-testid="lore-msg-input"]')).toBeNull();
    expect(row.querySelector('[data-testid="lore-author-link"]')).toBeNull();
    expect(pill.tagName).toBe("SPAN");
  });

  it("still renders the composer for a 外包 who is STILL on the live roster", async () => {
    // The other direction, and it is not decoration: a fix that hid the box for
    // every ow- author would pass the spec above and take the live case with
    // it — the two together are what say "departed", not "outsource".
    stubRoster([worker(LIVE, "O-1")]);
    stubList([mkEntry({ id: "L-10", authorId: LIVE })]);
    const { container } = renderPage();

    const row = await waitFor(() => rowById(container, "L-10"));
    await waitFor(() =>
      expect(
        row.querySelector('[data-testid="lore-author-composer"]'),
      ).not.toBeNull(),
    );
    expect(
      row.querySelector<HTMLElement>('[data-testid="lore-author-link"]')
        ?.textContent,
    ).toContain("O-1");
  });
});
