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

// ────────────────────────────────────────────────────────────────────────────
// The three things the owner hit on the trial station (cards rc-11734523eb52 +
// c-c933a2b41c54). All three share one shape: the screen looked FINE. Nothing
// was missing in a way that draws the eye — a dropdown had one option too many,
// and a row simply did not say two facts about itself. So each guard below has
// to assert a POSITIVE next to the negative, or it would pass on an empty page.
// ────────────────────────────────────────────────────────────────────────────

/** The roster the 撰寫人 dropdown is built from. `m-server-self` is the real
 * seed's machine-layer member, name and all — the very row the owner circled. */
function stubRoster() {
  vi.spyOn(api, "listMembers").mockResolvedValue([
    {
      id: "mira",
      name: "Mira",
      kind: "staff",
      role: "assistant",
      status: "online",
      lifecycle: "online",
      unreadCount: 0,
    },
    {
      id: "m-server-self",
      name: "伺服器這一台",
      kind: "warden",
      role: "",
      status: "offline",
      lifecycle: "offline",
      unreadCount: 0,
    },
  ] as never);
}

function authorOptionValues(container: HTMLElement): string[] {
  const sel = container.querySelector<HTMLSelectElement>(
    '[data-testid="lore-filter-author"]',
  );
  if (!sel) throw new Error("no 撰寫人 dropdown");
  return Array.from(sel.options).map((o) => o.value);
}

describe("LorePage — 撰寫人下拉只列寫得出傳承的身分", () => {
  it("drops the machine-layer member and keeps the staff one", async () => {
    stubRoster();
    stubList(page([mkEntry({ id: "e1" })]));
    const { container } = renderPage();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    await waitFor(() =>
      expect(authorOptionValues(container)).toContain("mira"),
    );

    // 🔴 THE POSITIVE IS THE POINT. "m-server-self is absent" is also true of a
    // dropdown that failed to load, of a roster that arrived empty, and of a
    // testid that no longer matches — three bugs this guard must NOT pass. So
    // the staff member has to be present in the SAME assertion pass.
    const values = authorOptionValues(container);
    expect(values).toContain("mira");
    expect(values).not.toContain("m-server-self");
  });
});

/** Build a page whose single row rides `scopeKind` / `scopeKey`, expand it, and
 * hand back the 屬於 pill. */
async function scopePillFor(
  scopeKind: LoreEntryView["scopeKind"],
  scopeKey: string,
): Promise<HTMLElement> {
  stubList(page([mkEntry({ id: "s1", scopeKind, scopeKey })]));
  const { container } = renderPage();
  await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
  fireEvent.click(rowById(container, "s1"));
  const pill = await waitFor(() => {
    const el = container.querySelector<HTMLElement>('[data-testid="lore-scope"]');
    if (!el) throw new Error("no 屬於 pill");
    return el;
  });
  return pill;
}

describe("LorePage — 屬於", () => {
  it("names the manual and jumps to it; role and agent name but do not jump", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);
    vi.spyOn(api, "listRoles").mockResolvedValue([
      { key: "assistant", name: "特助" },
    ] as never);

    const manual = await scopePillFor("manual", "review-pr");
    expect(manual.tagName).toBe("BUTTON");
    expect(manual.textContent).toContain("PR 審查");
    window.location.hash = "";
    fireEvent.click(manual);
    // The SAME jump the 任務卡's 類型 chip makes.
    expect(window.location.hash).toBe("#settings/manuals/review-pr");
  });

  it("renders a role scope as plain text, never as a dead button", async () => {
    vi.spyOn(api, "listRoles").mockResolvedValue([
      { key: "assistant", name: "特助" },
    ] as never);
    const role = await scopePillFor("role", "assistant");
    // 🔴 Asserting only "no href" would pass for a pill that LOOKS clickable and
    // goes nowhere — the same failure LoreAuthorChip guards against. A role has
    // no page to land on, so it must not be a button at all.
    expect(role.tagName).toBe("SPAN");
    expect(role.textContent).toContain("特助");
  });

  it("falls back to the raw key rather than rendering an empty cell", async () => {
    // A manual that has since been deleted. The entry still rides its type_key
    // and the reader must still be told WHICH one — a blank cell reads as a
    // broken field, and it is the failure this fallback exists to prevent.
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([] as never);
    const gone = await scopePillFor("manual", "deleted-type");
    expect(gone.textContent).toContain("deleted-type");
  });

  it("never renames an unknown scope into one of the real three", async () => {
    // A cockpit older than the server. `unknown` is NOT a scope; carrying it
    // through unrenamed is what stops this page inventing a fourth meaning for
    // one of the three that exist.
    const unknown = await scopePillFor("unknown", "some-future-key");
    expect(unknown.tagName).toBe("SPAN");
    expect(unknown.textContent).toContain("some-future-key");
  });
});

describe("LorePage — 條目編號", () => {
  it("shows the id, copies it on click, and does NOT expand the row", async () => {
    const copied: string[] = [];
    Object.assign(navigator, {
      clipboard: {
        writeText: (s: string) => {
          copied.push(s);
          return Promise.resolve();
        },
      },
    });
    stubList(page([mkEntry({ id: "L-7" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const badge = container.querySelector<HTMLElement>(
      '[data-testid="lore-entry-id"]',
    )!;
    expect(badge).not.toBeNull();
    expect(badge.textContent).toContain("L-7");

    fireEvent.click(badge);
    await waitFor(() => expect(copied).toEqual(["L-7"]));

    // 🔴 The row must stay COLLAPSED. The id badge sits inside the row, whose
    // whole surface is the expand toggle; a badge that both copies and expands
    // would look like it worked while doing something the reader did not ask
    // for. 屬於 only renders when expanded, so its absence IS the assertion.
    expect(
      container.querySelector('[data-testid="lore-scope"]'),
    ).toBeNull();
  });
});
