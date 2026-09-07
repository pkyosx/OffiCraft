// LorePage — the four properties spec §6 can be got WRONG SILENTLY.
//
// Every one of these is a case where the broken screen and the correct screen
// look alike to anybody who is not holding the server's answer next to them:
//
//   ① 三群的順序. A group order that drifts still shows every entry, still
//      groups them, still looks tidy. So the fixture below arrives from the
//      server in a DELIBERATELY SCRAMBLED order (active, retired, pinned) and
//      the test asserts the RENDERED order — that makes the assertion about
//      LorePage's own LORE_GROUPS walk rather than about the mock's sort.
//
//   ② / ③ 界線. 🔴 THIS IS THE ONE THE SPEC PUTS IN RED. `firstDroppedId` and
//      `capChars` are the SERVER'S answer over the whole scope; a page that
//      re-derived the cutoff by adding up the title+body lengths it can see
//      would be right on page 1 and wrong on every page after it — and a line
//      in the wrong place is pixel-identical to a line in the right place. So
//      the fixture's TEXT LENGTHS ARE DELIBERATELY NOT A CLUE: the entry named
//      by `firstDroppedId` is one of the SHORTEST rows on screen. A local sum
//      cannot produce this answer.
//      ③ pins the empty answer (`capChars === 0`): no line AND no dimming.
//      The two are one decision — a page that dimmed without drawing the line
//      would grey rows for a budget it cannot name.
//
//   ④ 撰寫人不在名冊上. The failure mode is not a missing icon, it is a LIVE
//      icon: a 傳訊息 button that opens a chat with a peer who is gone. The
//      test therefore asserts the whole affordance is a non-button, not just
//      that the glyph is absent.
//
// The list request is stubbed rather than driven through the mock adapter,
// because ②/③ need `capChars`/`firstDroppedId` set to values the mock would
// only produce for particular scope+budget combinations — pinning them through
// the mock's own cap arithmetic would make these specs a test of that
// arithmetic instead of a test of what the page does with the two fields.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { LorePage } from "./LorePage";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import type { LoreEntryPageView, LoreEntryView } from "../api/adapter";

function mkEntry(over: Partial<LoreEntryView> & { id: string }): LoreEntryView {
  return {
    seq: 1,
    scopeKind: "role",
    scopeKey: "assistant",
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

/** One page answer. `entries.length < 30` ⇒ the page stops asking for more, so
 * these fixtures are also the "there is nothing after this" signal. */
function page(
  entries: LoreEntryView[],
  cap: { capChars: number; firstDroppedId: string } = {
    capChars: 0,
    firstDroppedId: "",
  },
): LoreEntryPageView {
  return { entries, limit: 30, offset: 0, ...cap };
}

function stubList(answer: LoreEntryPageView) {
  return vi.spyOn(api, "listLoreEntries").mockResolvedValue(answer);
}

function renderPage() {
  return render(
    <I18nProvider>
      <LorePage />
    </I18nProvider>,
  );
}

/** The rendered rows, top to bottom, as entry ids. */
function renderedIds(container: HTMLElement): string[] {
  return Array.from(
    container.querySelectorAll('[data-testid="lore-row"]'),
  ).map((el) => el.getAttribute("data-entry-id") ?? "");
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
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("LorePage — 三群的順序", () => {
  it("renders 置頂 → 生效中 → 已失效 even when the answer arrives scrambled", async () => {
    stubList(
      page([
        mkEntry({ id: "a1", state: "active" }),
        mkEntry({ id: "r1", state: "retired", retireReason: "併進 SOP" }),
        mkEntry({ id: "p1", state: "pinned" }),
        mkEntry({ id: "a2", state: "active" }),
        mkEntry({ id: "p2", state: "pinned" }),
      ]),
    );
    const { container } = renderPage();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));

    // The GROUP sections, in DOM order.
    expect(
      Array.from(
        container.querySelectorAll('[data-testid^="lore-group-"]'),
      ).map((el) => el.getAttribute("data-testid")),
    ).toEqual(["lore-group-pinned", "lore-group-active", "lore-group-retired"]);

    // …and the rows inside them. Order WITHIN a group is the server's and is
    // preserved exactly (p1 before p2, a1 before a2).
    expect(renderedIds(container)).toEqual(["p1", "p2", "a1", "a2", "r1"]);
  });
});

describe("LorePage — 界線", () => {
  // 🔴 The line falls where the SERVER says. `mid` is one of the SHORTEST
  // entries on screen and still the first dropped: any local title+body sum
  // would put the line somewhere else.
  const entries = [
    mkEntry({ id: "p1", state: "pinned", title: "很長很長的置頂標題".repeat(8) }),
    mkEntry({ id: "top", state: "active", title: "上面這一筆".repeat(20) }),
    mkEntry({ id: "mid", state: "active", title: "短", body: "短" }),
    mkEntry({ id: "low", state: "active" }),
    mkEntry({ id: "r1", state: "retired", retireReason: "重複" }),
  ];

  it("dims firstDroppedId and everything under it, and nothing above it", async () => {
    stubList(page(entries, { capChars: 10000, firstDroppedId: "mid" }));
    const { container } = renderPage();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));
    expect(renderedIds(container)).toEqual(["p1", "top", "mid", "low", "r1"]);

    // Above the line: NOT dimmed.
    expect(rowById(container, "p1").dataset.dimmed).toBe("false");
    expect(rowById(container, "top").dataset.dimmed).toBe("false");
    // The named entry itself is BELOW the line — it is the first one that does
    // not get loaded — and so is everything after it, groups included.
    expect(rowById(container, "mid").dataset.dimmed).toBe("true");
    expect(rowById(container, "low").dataset.dimmed).toBe("true");
    expect(rowById(container, "r1").dataset.dimmed).toBe("true");

    // The line is drawn once, immediately above that entry, and it is NAMED:
    // it carries the budget it belongs to. No percentage, no 已用 x/y.
    const lines = container.querySelectorAll('[data-testid="lore-cap-line"]');
    expect(lines).toHaveLength(1);
    expect(lines[0].textContent).toContain("10000");
    expect(lines[0].textContent).not.toContain("%");

    // …and it sits BEFORE the row it names in document order.
    expect(
      lines[0].compareDocumentPosition(rowById(container, "mid")) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("draws no line and dims nothing when capChars is 0 (filter did not converge)", async () => {
    stubList(page(entries, { capChars: 0, firstDroppedId: "" }));
    const { container } = renderPage();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));

    expect(container.querySelectorAll('[data-testid="lore-cap-line"]')).toHaveLength(
      0,
    );
    for (const id of ["p1", "top", "mid", "low", "r1"]) {
      expect(rowById(container, id).dataset.dimmed).toBe("false");
    }
  });

  it("draws no line when the whole scope fits (capChars set, firstDroppedId empty)", async () => {
    stubList(page(entries, { capChars: 10000, firstDroppedId: "" }));
    const { container } = renderPage();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));

    expect(container.querySelectorAll('[data-testid="lore-cap-line"]')).toHaveLength(
      0,
    );
    for (const id of ["p1", "top", "mid", "low", "r1"]) {
      expect(rowById(container, id).dataset.dimmed).toBe("false");
    }
  });
});

describe("LorePage — 撰寫人", () => {
  it("drops the whole 傳訊息 affordance when the author is off the roster", async () => {
    stubList(
      page([
        // "mira" IS on the mock roster; "m-gone" wrote an entry and has since
        // left. The entry stays either way — only the live affordance differs.
        mkEntry({ id: "here", state: "active", authorId: "mira" }),
        mkEntry({ id: "gone", state: "active", authorId: "m-gone" }),
      ]),
    );
    const { container } = renderPage();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));

    // The 撰寫人 row lives in the expanded body — open both rows.
    fireEvent.click(rowById(container, "here"));
    fireEvent.click(rowById(container, "gone"));

    const here = rowById(container, "here");
    const gone = rowById(container, "gone");

    await waitFor(() =>
      expect(here.querySelector('[data-testid="lore-author-link"]')).not.toBeNull(),
    );
    // The reachable author's pill carries the icon INSIDE it.
    const link = here.querySelector<HTMLElement>('[data-testid="lore-author-link"]')!;
    expect(link.tagName).toBe("BUTTON");
    expect(link.querySelector("svg")).not.toBeNull();

    // 🔴 The departed author's pill is not a button at all. Asserting only
    // "no svg" would pass for a dead clickable pill, which is the actual bug.
    expect(gone.querySelector('[data-testid="lore-author-link"]')).toBeNull();
    const plain = gone.querySelector<HTMLElement>('[data-testid="lore-author-row"]')!;
    expect(plain).not.toBeNull();
    expect(plain.tagName).toBe("SPAN");
    expect(plain.querySelector("svg")).toBeNull();
    // The entry itself is untouched — the author is still NAMED.
    expect(plain.textContent).toContain("m-gone");
  });
});
