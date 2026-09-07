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

/** The 撰寫人 axis's option values. It is a MultiSelectFilter now (owner
 * rc-0376bf875757 [1] made all three axes multi-select), so the options live in
 * a popover that has to be OPENED — a helper that read the closed trigger would
 * find nothing and report an empty option list, which is exactly the false
 * negative the spec below refuses to accept. */
function authorOptionValues(container: HTMLElement): string[] {
  const trigger = container.querySelector<HTMLElement>(
    '[data-testid="lore-filter-author"]',
  );
  if (!trigger) throw new Error("no 撰寫人 filter");
  if (trigger.getAttribute("aria-expanded") !== "true") {
    fireEvent.click(trigger);
  }
  return Array.from(
    container.querySelectorAll('[data-testid^="lore-filter-author-opt-"]'),
  ).map(
    (el) =>
      el.getAttribute("data-testid")!.slice("lore-filter-author-opt-".length),
  );
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
/** The 屬於 pill's NAME half, read off a row NOBODY OPENED. The helper does not
 * click the row on purpose: the collapsed list is where the reader who cannot
 * tell one scope from another is standing, so every one of these specs is also
 * a statement that the pill is reachable without expanding anything. */
async function scopePillFor(
  scopeKind: LoreEntryView["scopeKind"],
  scopeKey: string,
): Promise<HTMLElement> {
  stubList(page([mkEntry({ id: "s1", scopeKind, scopeKey })]));
  const { container } = renderPage();
  await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
  const pill = await waitFor(() => {
    const el = container.querySelector<HTMLElement>(
      '[data-testid="lore-scope-name"]',
    );
    if (!el) throw new Error("no 屬於 pill");
    return el;
  });
  return pill;
}

describe("LorePage — 屬於", () => {
  it("names the manual and jumps to it; a member names but does not jump", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);

    const manual = await scopePillFor("manual", "review-pr");
    expect(manual.tagName).toBe("BUTTON");
    expect(manual.textContent).toContain("PR 審查");
    window.location.hash = "";
    fireEvent.click(manual);
    // The SAME jump the 任務卡's 類型 chip makes.
    expect(window.location.hash).toBe("#settings/manuals/review-pr");
  });

  it("renders a member scope as plain text, never as a dead button", async () => {
    const member = await scopePillFor("agent", "mira");
    // 🔴 Asserting only "no href" would pass for a pill that LOOKS clickable and
    // goes nowhere — the same failure LoreAuthorChip guards against. A member
    // has no page to land on, so it must not be a button at all.
    expect(member.tagName).toBe("SPAN");
    expect(member.textContent).toContain("Mira");
  });

  it("renders an ORPHAN (unknown kind) with its raw key, and names no kind", async () => {
    // 🔴 THIS IS A LIVE CASE, NOT FUTURE-PROOFING. The server's migration
    // deliberately left `scope_kind='role'` on every entry whose owning member
    // it could not determine, and `toLoreEntry` maps that to "unknown". Such a
    // row arrives on the unfiltered page — the page this one opens on.
    const orphan = await scopePillFor("unknown", "r-9f31c0d84a17");
    expect(orphan.tagName).toBe("SPAN");
    // The raw key, so the reader can at least say WHICH one it is.
    expect(orphan.textContent).toContain("r-9f31c0d84a17");
    // 🔴 AND NO KIND WORD. Labelling it 成員傳承 or 任務傳承 would assert an
    // owner the migration explicitly refused to choose — the one thing this arm
    // exists to avoid.
    const chip = orphan.closest('[data-testid="lore-scope"]')!;
    expect(chip.textContent).not.toContain("成員傳承");
    expect(chip.textContent).not.toContain("任務傳承");
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
    // for.
    //
    // ⚠️ This used to assert 「屬於 is absent」 and read that absence as 「still
    // collapsed」. That proxy died the day 屬於 moved onto the collapsed row —
    // and a proxy that dies by becoming ALWAYS-TRUE or ALWAYS-FALSE takes the
    // spec with it silently. 撰寫人 is the expanded-only element now, so the
    // signal is its absence, and the clamp on the body says the same thing a
    // second way.
    expect(
      container.querySelector('[data-testid="lore-author-row"]'),
    ).toBeNull();
    expect(
      container.querySelector('[data-testid="lore-author-link"]'),
    ).toBeNull();
    expect(
      container.querySelector('[data-testid="lore-body"]')?.className,
    ).toContain("lore-row__body--clamped");
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 撰寫人跳過去要「帶著編號」(owner rc-abf2c90d887c option ②).
//
// 🔴 THE JUMP IS NOT THE THING BEING GUARDED — the seed is. A jump that lands
// on the right person with an EMPTY composer looks completely correct on
// screen: the office opens, the chat is the right one, the cursor blinks. The
// reader types 「這條還適用嗎」 and the author receives a sentence with no
// subject, holding dozens of entries, and has to ask which one back — which is
// the exact question the jump existed to save. Nothing errors, nothing is
// missing from the screen, and the damage lands on the OTHER person's side.
//
// So the assertion is on the ROUTE THAT GETS WRITTEN, and it is written twice
// with two different rows. One row alone would still pass if every row seeded
// the FIRST entry's id (the `entry.id` inside the row map is the value at
// risk); it takes a second row with a different id to tell "carries this row's
// id" apart from "carries some id".
describe("LorePage — 撰寫人跳過去帶著條目編號", () => {
  beforeEach(() => {
    window.location.hash = "";
  });

  it("seeds the composer with THIS row's entry id, not merely with an id", async () => {
    stubList(
      page([
        mkEntry({ id: "L-7", state: "active", authorId: "mira" }),
        mkEntry({ id: "L-31", state: "active", authorId: "mira" }),
      ]),
    );
    const { container } = renderPage();

    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));

    // 撰寫人 lives in the expanded body.
    fireEvent.click(rowById(container, "L-7"));
    const first = rowById(container, "L-7");
    await waitFor(() =>
      expect(
        first.querySelector('[data-testid="lore-author-link"]'),
      ).not.toBeNull(),
    );
    fireEvent.click(
      first.querySelector<HTMLElement>('[data-testid="lore-author-link"]')!,
    );

    // The whole route, not a substring: 「有帶東西過去」 and 「帶對了」 are
    // different claims, and `toContain("compose")` would accept the first for
    // the second.
    expect(window.location.hash).toBe("#office/chat/mira/compose/L-7");

    // The SECOND row, same author, different id. A page that hardcoded or
    // captured one entry's id would still be sitting on L-7 here.
    fireEvent.click(rowById(container, "L-31"));
    const second = rowById(container, "L-31");
    await waitFor(() =>
      expect(
        second.querySelector('[data-testid="lore-author-link"]'),
      ).not.toBeNull(),
    );
    fireEvent.click(
      second.querySelector<HTMLElement>('[data-testid="lore-author-link"]')!,
    );

    expect(window.location.hash).toBe("#office/chat/mira/compose/L-31");
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 屬於 on the COLLAPSED row (owner, card rc-11734523eb52 — and the second
// attempt at it).
//
// 🔴 THE FIRST FIX FOR THIS PASSED EVERY REVIEW AND DID NOT SOLVE THE PROBLEM.
// It put 屬於 in the row's expanded body, and the reason written down for
// adding it at all was that 全部 — where the page OPENS — mixes every scope
// together and the reader cannot tell a 角色傳承 from one manual's. That reader
// is looking at a list of CLOSED rows. An answer they must open a row to reach
// arrives only for somebody who already knew to go looking, which is not the
// person who was lost. On screen the two versions are indistinguishable until
// you notice you are the one doing the clicking.
//
// So this spec asserts on rows NOBODY OPENED, and it uses two entries whose
// scopes have THE SAME DISPLAY NAME. That is the whole discrimination: a chip
// carrying the name alone renders identical text for both rows and would pass
// any assertion written against one of them.
describe("LorePage — 屬於 在收合的列上就說得出是哪一種", () => {
  it("tells a 成員傳承 from a 任務傳承 when both are named Mira, without opening either row", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      // A manual that happens to carry the SAME display name as the member.
      { typeKey: "mira-work", displayName: "Mira", purpose: "", fields: [] },
    ] as never);

    stubList(
      page([
        mkEntry({ id: "L-1", scopeKind: "agent", scopeKey: "mira" }),
        mkEntry({ id: "L-2", scopeKind: "manual", scopeKey: "mira-work" }),
      ]),
    );
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));

    // NOT expanded — no click anywhere. The 撰寫人 row is the expanded-only
    // element, so its absence is what says these rows are still closed.
    expect(
      container.querySelectorAll('[data-testid="lore-author-row"]'),
    ).toHaveLength(0);
    expect(
      container.querySelectorAll('[data-testid="lore-author-link"]'),
    ).toHaveLength(0);

    // 🔴 PRESENCE IS ITS OWN ASSERTION, on its own line. Reading .textContent
    // off a `!`-asserted querySelector would make a MISSING pill fail inside an
    // accessor instead of at a spec line — a red that says 「something threw」
    // where the spec meant to say 「屬於 is not on the closed row」.
    const scopeOf = (id: string) => {
      const el = rowById(container, id).querySelector('[data-testid="lore-scope"]');
      expect(el).not.toBeNull();
      return el!.textContent!.replace(/\s+/g, " ").trim();
    };

    // Both are reachable while closed…
    const memberText = scopeOf("L-1");
    const manualText = scopeOf("L-2");

    // …and they do not read the same. 🔴 This inequality is the assertion that
    // survives a rename of either word; asserting the literal 「成員傳承 · Mira」
    // would also pass for a page that printed the kind and dropped the name.
    expect(memberText).not.toBe(manualText);
    expect(memberText).toContain("成員傳承");
    expect(memberText).toContain("Mira");
    expect(manualText).toContain("任務傳承");
    expect(manualText).toContain("Mira");
  });

  it("names an outsource member's entry the same way a staff member's is named", async () => {
    // 🔴 THE TWO USED TO BE DIFFERENT KINDS — staff lore was 角色傳承 and only a
    // contractor's was 成員傳承. Since the collapse both are 成員傳承 keyed by a
    // member id, and this pins that they are not drifting back apart on the
    // display side while sharing one scope on the wire.
    const agent = await scopePillFor("agent", "ow-nobody");
    const chip = agent.closest('[data-testid="lore-scope"]')!;
    expect(chip.textContent).toContain("成員傳承");
    // The member is not on the live roster, so the raw id is the honest label.
    expect(chip.textContent).toContain("ow-nobody");
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 內嵌輸入框 (owner, card rc-abf2c90d887c option ②).
//
// Three properties here, and all three fail with a screen that looks correct:
//
//   ① THE ENTRY ID LEADS THE MESSAGE. Drop the prefix and the box still
//      accepts text, still clears, still says 已送出 — and the author receives
//      「這條還適用嗎」 with no subject, holding dozens of entries. The damage
//      is entirely on the recipient's side and nothing on this screen shows it.
//      The assertion is therefore on the BODY THAT WAS SENT, not on the box.
//   ② A FAILED SEND KEEPS THE DRAFT AND SAYS SO. A box that clears itself on a
//      failure is pixel-identical to one that succeeded; what is lost is the
//      reader's own sentence, and they have no way to know it never left.
//   ③ A DEPARTED AUTHOR GETS NO BOX AT ALL. Not a disabled one — the pill for
//      such an author is deliberately not a button, and a composer that sends
//      into nothing is that same dead affordance in a larger shape.
describe("LorePage — 內嵌輸入框", () => {
  async function openComposer(entryId = "L-7", authorId = "mira") {
    stubList(page([mkEntry({ id: entryId, state: "active", authorId })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    fireEvent.click(rowById(container, entryId));
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-msg-input"]'),
      ).not.toBeNull(),
    );
    return container;
  }

  it("sends the entry id AHEAD of what was typed", async () => {
    const posted: { to: string; body: string }[] = [];
    vi.spyOn(api, "postChat").mockImplementation(async (m) => {
      posted.push({ to: m.to, body: m.body });
    });

    const container = await openComposer("L-7");
    const box = container.querySelector<HTMLTextAreaElement>(
      '[data-testid="lore-msg-input"]',
    )!;
    fireEvent.change(box, { target: { value: "這條還適用嗎" } });
    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-msg-send"]')!,
    );

    await waitFor(() => expect(posted).toHaveLength(1));
    // 🔴 The WHOLE body, and the id at the FRONT. `toContain("L-7")` would also
    // pass for an id appended after the sentence, which does not solve the
    // problem the prefix exists for — the author reads the first words.
    expect(posted[0].body).toBe("[L-7] 這條還適用嗎");
    expect(posted[0].to).toBe("mira");

    // The box empties on success, and the screen says it went.
    await waitFor(() => expect(box.value).toBe(""));
    expect(
      container.querySelector('[data-testid="lore-msg-sent"]'),
    ).not.toBeNull();
  });

  it("tells the reader the id will be prepended, before they send anything", async () => {
    const container = await openComposer("L-31");
    // 🔴 This note is the ONLY place on screen that says what goes out differs
    // from what was typed. Without it the prefix is a silent rewrite of the
    // reader's own words.
    const note = container.querySelector('[data-testid="lore-msg-prefix-note"]');
    expect(note).not.toBeNull();
    expect(note!.textContent).toContain("L-31");
  });

  it("keeps the draft and says so when the send fails", async () => {
    vi.spyOn(api, "postChat").mockRejectedValue(new Error("boom"));
    vi.spyOn(console, "warn").mockImplementation(() => {});

    const container = await openComposer("L-7");
    const box = container.querySelector<HTMLTextAreaElement>(
      '[data-testid="lore-msg-input"]',
    )!;
    fireEvent.change(box, { target: { value: "這條還適用嗎" } });
    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-msg-send"]')!,
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-msg-failed"]'),
      ).not.toBeNull(),
    );
    // 🔴 BOTH halves. A visible notice with an emptied box still costs the
    // reader their sentence; a kept draft with no notice reads as 「it just did
    // not send yet」.
    expect(box.value).toBe("這條還適用嗎");
    expect(
      container.querySelector('[data-testid="lore-msg-sent"]'),
    ).toBeNull();
  });

  it("gives a departed author no box at all, not a disabled one", async () => {
    stubList(
      page([
        mkEntry({ id: "here", state: "active", authorId: "mira" }),
        mkEntry({ id: "gone", state: "active", authorId: "m-gone" }),
      ]),
    );
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    fireEvent.click(rowById(container, "here"));
    fireEvent.click(rowById(container, "gone"));

    await waitFor(() =>
      expect(
        rowById(container, "here").querySelector(
          '[data-testid="lore-author-composer"]',
        ),
      ).not.toBeNull(),
    );
    // 🔴 Absent, not disabled. Asserting only 「the send button is disabled」
    // would pass for a box that renders, invites typing, and can never deliver.
    expect(
      rowById(container, "gone").querySelector(
        '[data-testid="lore-author-composer"]',
      ),
    ).toBeNull();
    expect(
      rowById(container, "gone").querySelector('[data-testid="lore-msg-input"]'),
    ).toBeNull();
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 篩選器 (owner rc-0376bf875757 option ②: 全部改複選; then rc-a43100fd0486:
// 四格收成三格 —— 撰寫人 / 屬於 / 狀態).
//
// 🔴 THE 範圍 DROPDOWN IS GONE AND ITS ABSENCE IS ASSERTED, not assumed. The
// old shape asked the kind and the key as two controls, which is why the
// orphaned-key hazard existed at all (untick the kind, keep sending the key,
// and the wire's AND silently narrows a page the reader believes they widened).
// 屬於 carries both halves in ONE option value, so that hazard is gone by
// construction — but only for as long as nobody re-introduces a second control.
//
// 🔴 WHAT REPLACED IT HAS ITS OWN SILENT FAILURE, and it is the reason these
// specs assert on the REQUEST rather than the screen: 屬於's options mix members
// and manuals, and the wire wants them split across `scope_kinds` and
// `scope_keys`. A page that sent both kinds unconditionally would look correct
// — the pill reads the same, the rows come back — while every member-only
// filter silently also admitted manuals.
describe("LorePage — 篩選器複選", () => {
  /** The options of the most recent list request. */
  function lastOpts(spy: ReturnType<typeof stubList>) {
    const calls = spy.mock.calls;
    if (calls.length === 0) throw new Error("listLoreEntries was never called");
    return (calls[calls.length - 1][0] ?? {}) as Record<string, unknown>;
  }

  function tick(container: HTMLElement, testId: string, value: string) {
    const trigger = container.querySelector<HTMLElement>(
      `[data-testid="${testId}"]`,
    );
    if (!trigger) throw new Error(`no ${testId} filter`);
    if (trigger.getAttribute("aria-expanded") !== "true") {
      fireEvent.click(trigger);
    }
    const box = container.querySelector<HTMLElement>(
      `[data-testid="${testId}-opt-${value}"] input`,
    );
    if (!box) throw new Error(`no ${testId} option ${value}`);
    fireEvent.click(box);
  }

  it("derives the KIND set from what is ticked — a member alone never asks for manuals", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);
    const spy = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    // One member ⇒ exactly one kind and one key. 🔴 The kind matters as much as
    // the key: sending ["agent","manual"] here would also return every manual
    // entry, and the screen — one ticked name, some rows — looks identical.
    tick(container, "lore-filter-belongs", "agent:mira");
    await waitFor(() => expect(lastOpts(spy).scopeKinds).toEqual(["agent"]));
    expect(lastOpts(spy).scopeKeys).toEqual(["mira"]);

    // Add a manual ⇒ BOTH kinds, both keys.
    tick(container, "lore-filter-belongs", "manual:review-pr");
    await waitFor(() =>
      expect(lastOpts(spy).scopeKinds).toEqual(["agent", "manual"]),
    );
    expect(lastOpts(spy).scopeKeys).toEqual(["mira", "review-pr"]);

    // Untick the member ⇒ the manual kind ALONE. This is the half that fails
    // silently if the kind set is a constant: `agent` lingering here asks for
    // every member's 傳承 on a screen showing one manual ticked.
    tick(container, "lore-filter-belongs", "agent:mira");
    await waitFor(() => expect(lastOpts(spy).scopeKinds).toEqual(["manual"]));
    expect(lastOpts(spy).scopeKeys).toEqual(["review-pr"]);

    // Untick everything ⇒ no constraint on either axis, not an empty array.
    tick(container, "lore-filter-belongs", "manual:review-pr");
    await waitFor(() => expect(lastOpts(spy).scopeKinds).toBeUndefined());
    expect(lastOpts(spy).scopeKeys).toBeUndefined();
  });

  it("sends a SET on the state axis, not the last thing ticked", async () => {
    const spy = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    tick(container, "lore-filter-state", "pinned");
    tick(container, "lore-filter-state", "retired");

    // Both, in one request. A control that kept only the newest tick would send
    // ["retired"] and look completely normal doing it.
    await waitFor(() =>
      expect(lastOpts(spy).states).toEqual(["pinned", "retired"]),
    );
  });

  it("has exactly three fields, and 範圍 / 角色 / 手冊 are gone", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    // 🔴 ABSENCE IS THE ASSERTION HERE. The owner asked for three filters
    // (「我從使用者或是任務手冊作為 filter 另外就是狀態 三個而已」), and a page
    // that ADDED 屬於 while keeping the old three would satisfy every other spec
    // in this file: 屬於 works, the order test still finds its three, and the
    // screen merely has two extra dropdowns nobody mentioned.
    for (const gone of [
      "lore-filter-scope",
      "lore-filter-role",
      "lore-filter-manual",
    ]) {
      expect(container.querySelector(`[data-testid="${gone}"]`)).toBeNull();
    }
    for (const present of [
      "lore-filter-author",
      "lore-filter-belongs",
      "lore-filter-state",
    ]) {
      expect(container.querySelector(`[data-testid="${present}"]`)).not.toBeNull();
    }
  });

  it("offers every member AND every manual in the one 屬於 list", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);
    // 🔴 THE LIVE WORKER IS STUBBED IN, and the mock's default of NONE is why.
    // Outsource workers exist on the mock only while bound to a task, so
    // without this the list is staff + manuals and the spec below would be
    // asserting that 屬於 offers two kinds of thing when it had only ever been
    // handed one kind plus manuals — it would pass for a list built from
    // `members` alone.
    vi.spyOn(api, "listOutsourceWorkers").mockResolvedValue([
      { id: "ow-7d8ad859dd9b", codename: "O-179" },
    ] as never);
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-belongs"]')!,
    );

    // A staff member, an outsource member, and a manual — all three reachable
    // without changing any other control. The outsource row is what says this
    // list is not just "the staff roster".
    //
    // ⚠️ WHAT THIS LIST CANNOT OFFER, so nobody reads the spec as claiming
    // otherwise: a RELEASED outsource worker. Its 傳承 outlives it (the entries
    // are keyed by its member id and are never deleted) but it is on neither
    // the live worker list nor the staff roster, so nothing can put it in this
    // dropdown. That is the same limit the 撰寫人 filter already has and states,
    // and it is not made worse here — before this control existed, 成員傳承 had
    // no key filter of any kind.
    for (const value of [
      "agent:mira",
      "agent:ow-7d8ad859dd9b",
      "manual:review-pr",
    ]) {
      expect(
        container.querySelector(
          `[data-testid="lore-filter-belongs-opt-${value}"]`,
        ),
      ).not.toBeNull();
    }

    // 🔴 THE OPTION LABELS SAY WHICH KIND. One list holding people and manuals
    // cannot rely on the name alone — a station may have a manual named after a
    // member, which is exactly the collision the row chip already had to solve.
    const label = (value: string) =>
      container.querySelector(
        `[data-testid="lore-filter-belongs-opt-${value}"]`,
      )!.textContent!;
    expect(label("agent:mira")).toContain("成員傳承");
    expect(label("manual:review-pr")).toContain("任務傳承");
  });

  it("offers no per-option statistic on any filter", async () => {
    // 🔴 WHICH NUMBER IS BEING REFUSED, because there are two and only one is.
    // The 任務頁's 負責人 dropdown puts a COUNT BADGE beside each name (how many
    // tasks that person holds — MultiSelectFilter's `count`, rendered as
    // `${testId}-count-<value>`). That is the 統計數字 the owner ruled out for
    // this page: 「we dont need count」. The page could not produce an honest one
    // anyway — 傳承 loads by scrolling, so any per-option number would count the
    // rows fetched so far and drift as the reader scrolls.
    //
    // The pill's own 「· N」 summary is NOT that number and is NOT refused: it
    // says how many boxes are ticked, which is a fact about the control the
    // reader just operated, and it is what makes a multi-select legible at all.
    // Asserting against it here would fight the shared component and make this
    // page's filters read differently from every other page's.
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-belongs"]')!,
    );
    expect(
      container.querySelectorAll('[data-testid^="lore-filter-belongs-count-"]'),
    ).toHaveLength(0);

    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-state"]')!,
    );
    expect(
      container.querySelectorAll('[data-testid^="lore-filter-state-count-"]'),
    ).toHaveLength(0);
    expect(
      container.querySelectorAll('[data-testid^="lore-filter-author-count-"]'),
    ).toHaveLength(0);
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 上限線 ↔ 篩選器, end to end through the CONTROLS.
//
// 🔴 THE 界線 SPECS ABOVE NEVER TOUCH A FILTER. They hand the page a stubbed
// answer that already carries `capChars`/`firstDroppedId` and check what the
// page draws with it — which is the right shape for testing the drawing, and
// says nothing about whether converging the filter is what makes the server
// answer that way. After the axes became multi-select (owner rc-0376bf875757
// [1]) that gap matters: 「範圍複選時上限線就不畫」 is a decision the owner
// weighed a cost for, so the page has to be able to get the line BACK.
//
// A test that only asserted 「two scopes ⇒ no line」 would pass for a page that
// never shows a line at all — the exact one-sided check the ticket's own DoD
// calls out. So both directions are here, in that order, in one spec.
describe("LorePage — 收斂到單一範圍時，上限線回得來", () => {
  it("goes quiet for two scopes and comes back — named — for one", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);

    const rows = [
      mkEntry({ id: "L-1", state: "active" }),
      mkEntry({ id: "L-2", state: "active" }),
    ];
    // The stub answers as the SERVER does: a cap only when the request named a
    // single scope. That is the rule under test — the page must not invent one.
    const spy = vi
      .spyOn(api, "listLoreEntries")
      .mockImplementation(async (o) => {
        const one =
          o?.scopeKinds?.length === 1 && o?.scopeKeys?.length === 1;
        return {
          entries: rows,
          limit: 30,
          offset: 0,
          capChars: one ? 10000 : 0,
          firstDroppedId: one ? "L-2" : "",
        };
      });

    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));

    function tick(testId: string, value: string) {
      const trigger = container.querySelector<HTMLElement>(
        `[data-testid="${testId}"]`,
      )!;
      if (trigger.getAttribute("aria-expanded") !== "true") {
        fireEvent.click(trigger);
      }
      fireEvent.click(
        container.querySelector<HTMLElement>(
          `[data-testid="${testId}-opt-${value}"] input`,
        )!,
      );
    }

    // Two scopes ⇒ two budgets ⇒ no honest place for one line. They are a
    // MEMBER and a MANUAL on purpose: two members would also span two budgets,
    // but this pair also spans two KINDS, which is the case the wire's
    // convergence rule is written about.
    tick("lore-filter-belongs", "agent:mira");
    tick("lore-filter-belongs", "manual:review-pr");
    await waitFor(() =>
      expect(spy.mock.calls[spy.mock.calls.length - 1][0]?.scopeKinds).toEqual([
        "agent",
        "manual",
      ]),
    );
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-cap-line"]'),
      ).toBeNull(),
    );

    // Converge: drop the manual, leaving one member. The line must return.
    tick("lore-filter-belongs", "manual:review-pr");

    const line = await waitFor(() => {
      const el = container.querySelector('[data-testid="lore-cap-line"]');
      if (!el) throw new Error("the 上限線 did not come back");
      return el;
    });
    // 🔴 NAMED, not merely present. A line that appeared carrying the wrong
    // scope's name would be worse than none: it would tell the reader a budget
    // they are not looking at.
    expect(line.textContent).toContain("成員傳承");
    expect(line.textContent).toContain("Mira");
    expect(line.textContent).toContain("10000");
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 順序 (owner, 2026-09-07: 「The order of buttons / filters matters」
// 「make them consistent with task」「The consistency is very important」).
//
// 🔴 ORDER IS THE ONE PROPERTY NOTHING ELSE ON THIS PAGE PROTECTS. Every other
// spec here asks whether a thing is PRESENT and whether it CARRIES the right
// value; a row whose badges are shuffled passes all of them, renders cleanly,
// and is wrong only against the other page — which no test on this page ever
// looks at. So it drifts by the ordinary act of adding a field near the top of
// a JSX block, silently, and the person who notices is the reader who has to
// re-learn a layout they already knew.
//
// The two orders are copied FROM 任務頁, and this spec writes them out
// literally so a future change has to state the new order rather than arrive at
// one by accident:
//   篩選列  撰寫人 → 屬於 → 狀態                       (任務頁: 負責人 → 類型 → 狀態)
//   列上    編號 → 狀態 → 屬於                          (任務卡: 編號 → 優先權 → 狀態 → 類型)
describe("LorePage — 順序跟任務頁一致", () => {
  it("puts the three filters in the 任務頁 order, and keeps that order as they are used", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const idsInDomOrder = () =>
      Array.from(
        container.querySelectorAll(
          '[data-testid^="lore-filter-"]:not([data-testid*="-opt-"])',
        ),
      )
        .map((el) => el.getAttribute("data-testid")!)
        // FilterPanel's own wrapper ids share the prefix; only the FIELDS are
        // being ordered here.
        .filter((id) =>
          [
            "lore-filter-author",
            "lore-filter-belongs",
            "lore-filter-state",
          ].includes(id),
        );

    expect(idsInDomOrder()).toEqual([
      "lore-filter-author",
      "lore-filter-belongs",
      "lore-filter-state",
    ]);

    // 🔴 AND IT DOES NOT MOVE WHEN A FILTER IS USED. The previous shape grew a
    // field on tick and lost it on untick, so the row's layout depended on what
    // the reader had done — this pins that the three are now fixed, which is
    // half of what 「抄 task」 means.
    const trigger = container.querySelector<HTMLElement>(
      '[data-testid="lore-filter-belongs"]',
    )!;
    fireEvent.click(trigger);
    fireEvent.click(
      container.querySelector<HTMLElement>(
        '[data-testid="lore-filter-belongs-opt-manual:review-pr"] input',
      )!,
    );
    await waitFor(() =>
      expect(idsInDomOrder()).toEqual([
        "lore-filter-author",
        "lore-filter-belongs",
        "lore-filter-state",
      ]),
    );
  });

  it("puts the entry id FIRST on the row, ahead of the state badge", async () => {
    stubList(page([mkEntry({ id: "L-9", state: "pinned" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const row = rowById(container, "L-9");
    const marks = Array.from(
      row.querySelectorAll(
        '[data-testid="lore-entry-id"], [data-testid="lore-state"], [data-testid="lore-scope"]',
      ),
    ).map((el) => el.getAttribute("data-testid"));

    // 🔴 The id LEADS, the way 任務卡's #T-1 does. It is what you quote to
    // somebody else, so it is what the eye should land on first — and it landed
    // second here until the owner put the two screenshots side by side.
    expect(marks).toEqual(["lore-entry-id", "lore-state", "lore-scope"]);
  });

  it("draws the id badge like 任務卡's — the # and a glyph, not a bare number", async () => {
    stubList(page([mkEntry({ id: "L-9" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const badge = container.querySelector<HTMLElement>(
      '[data-testid="lore-entry-id"]',
    )!;
    // Same three parts 任務卡's badge has: a glyph, a `#`, the number.
    expect(badge.querySelector("svg")).not.toBeNull();
    expect(badge.textContent).toContain("#L-9");
  });
});
