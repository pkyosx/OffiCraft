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
import type {
  ChatAttachmentInput,
  LoreEntryPageView,
  LoreEntryView,
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
    const orphan = await scopePillFor("unknown", "r-legacy-orphan");
    expect(orphan.tagName).toBe("SPAN");
    // The raw key, so the reader can at least say WHICH one it is.
    expect(orphan.textContent).toContain("r-legacy-orphan");
    // 🔴 AND NO KIND AT ALL. Stamping it with the person glyph or the gear
    // would assert an owner the migration explicitly refused to choose — the
    // one thing this arm exists to avoid.
    //
    // ⚠️ THE TEXT HALF OF THIS ASSERTION IS NOW VACUOUS AND THE GLYPH HALF IS
    // NOT. Since the kind word was removed from every badge (owner 2026-09-08),
    // 「no 成員傳承 text」 is true of EVERY row and would pass for an orphan
    // wearing the member glyph. The glyph lines below are what this spec now
    // rests on; the two text lines are kept only to catch a re-introduced
    // prefix landing on the one arm that must never name a kind.
    const chip = orphan.closest('[data-testid="lore-scope"]')!;
    expect(chip.querySelectorAll(".lore-row__scope-glyph")).toHaveLength(0);
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
    // ⚠️ THIS ASSERTION HAS NOW OUTLIVED TWO PROXIES, WHICH IS WHY IT IS ON THE
    // STATE ITSELF. It first read 「屬於 is absent」 and 屬於 moved onto the
    // collapsed row; it then read 「撰寫人 is absent」 and 撰寫人 moved onto the
    // collapsed row too (owner 2026-09-08: 展開／收合 governs the CONTENT and
    // nothing else). Each move turned the guard ALWAYS-TRUE without failing.
    // `aria-expanded` is the row's own state, so it cannot be relocated out
    // from under this line. The clamp says the same thing a second way, and it
    // is the only remaining thing 展開 actually changes.
    expect(
      container
        .querySelector('[data-testid="lore-row"]')!
        .getAttribute("aria-expanded"),
    ).toBe("false");
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

    // NOT expanded — no click anywhere. Asserted on the rows' own
    // `aria-expanded`, because every element that used to be expanded-only
    // (屬於, then 撰寫人) has since moved onto the closed row; a proxy that keeps
    // moving is a proxy that keeps going silently always-true.
    for (const id of ["L-1", "L-2"]) {
      expect(rowById(container, id).getAttribute("aria-expanded")).toBe("false");
    }

    // 🔴 PRESENCE IS ITS OWN ASSERTION, on its own line. Reading .textContent
    // off a `!`-asserted querySelector would make a MISSING pill fail inside an
    // accessor instead of at a spec line — a red that says 「something threw」
    // where the spec meant to say 「屬於 is not on the closed row」.
    const scopeOf = (id: string) => {
      const el = rowById(container, id).querySelector('[data-testid="lore-scope"]');
      expect(el).not.toBeNull();
      return el as HTMLElement;
    };
    const memberScope = scopeOf("L-1");
    const manualScope = scopeOf("L-2");

    // 🔴 THE TEXT NO LONGER DISCRIMINATES AT ALL, AND THAT IS THE POINT OF THIS
    // REWRITE. Until 2026-09-08 the badge carried a word prefix (「成員傳承 · 」
    // / 「任務傳承 · 」) and this spec asserted on it. Owner removed the prefix
    // (「這個不必要」), so both rows now read exactly 「Mira」 — the two are
    // character-for-character identical and the GLYPH is the only thing left
    // telling them apart. Assert that first, so this line fails if anyone drops
    // an icon back out of the badge.
    expect(memberScope.textContent!.trim()).toBe(manualScope.textContent!.trim());

    const member = memberScope.querySelector(".lore-row__scope-glyph--member");
    const task = manualScope.querySelector(".lore-row__scope-glyph--task");
    expect(member).not.toBeNull();
    expect(task).not.toBeNull();
    // 🔴 AND THE TWO GLYPHS ARE ACTUALLY DIFFERENT DRAWINGS. A class name is a
    // label; two <svg>s wearing different class names could be the same icon,
    // which would look — on the closed list this spec exists for — exactly like
    // no discrimination at all. Comparing the rendered SVG bodies is what makes
    // that impossible.
    expect(member!.innerHTML).not.toBe(task!.innerHTML);

    // 🔴 一次只出現一個 (owner 2026-09-08). Not two glyphs on one badge, and
    // never the other kind's glyph on this one.
    expect(memberScope.querySelectorAll(".lore-row__scope-glyph")).toHaveLength(1);
    expect(manualScope.querySelectorAll(".lore-row__scope-glyph")).toHaveLength(1);
    expect(
      memberScope.querySelector(".lore-row__scope-glyph--task"),
    ).toBeNull();
    expect(
      manualScope.querySelector(".lore-row__scope-glyph--member"),
    ).toBeNull();

    // The name itself is still there — a badge that lost the name and kept the
    // glyph would pass every line above.
    expect(memberScope.textContent).toContain("Mira");
    expect(manualScope.textContent).toContain("Mira");
  });

  it("names an outsource member's entry the same way a staff member's is named", async () => {
    // 🔴 THE TWO USED TO BE DIFFERENT KINDS — staff lore was 角色傳承 and only a
    // contractor's was 成員傳承. Since the collapse both are 成員傳承 keyed by a
    // member id, and this pins that they are not drifting back apart on the
    // display side while sharing one scope on the wire.
    const agent = await scopePillFor("agent", "ow-nobody");
    const chip = agent.closest('[data-testid="lore-scope"]')!;
    // The kind is the GLYPH now, not a word (owner 2026-09-08 removed the text
    // prefix), so 「same way a staff member's is named」 is asserted on the
    // person glyph rather than on 「成員傳承」.
    expect(chip.querySelector(".lore-row__scope-glyph--member")).not.toBeNull();
    expect(chip.querySelector(".lore-row__scope-glyph--task")).toBeNull();
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
  /** 🔴 THIS HELPER NO LONGER OPENS ANYTHING, AND THAT IS AN ASSERTION.
   * It used to click the row, because the box lived in the expanded body. Owner
   * moved it onto the collapsed row (「訊息輸入框預設就要在（不用展開）」, the
   * 任務卡 behaviour), so every spec below now also states that the box is
   * reachable without a click — a `waitFor` that never resolves is how a
   * regression back into `expanded &&` shows up here.
   *
   * ⚠️ IT NO LONGER CHECKS 「撰寫人 is absent」. That line was a second reading
   * of 「the row is closed」, and 撰寫人 stopped being expanded-only on
   * 2026-09-08 — keeping it would have been a guard that can only fail for the
   * wrong reason. `aria-expanded` below is the state itself. */
  async function openComposer(entryId = "L-7", authorId = "mira") {
    stubList(page([mkEntry({ id: entryId, state: "active", authorId })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-msg-input"]'),
      ).not.toBeNull(),
    );
    // The row really is closed. 🔴 ASSERT ON `aria-expanded`, WHICH IS THE
    // STATE ITSELF. This used to read `lore-author-row` and that testid is
    // rendered ONLY on the DEPARTED-author branch (peerId === ""), so for the
    // reachable author every call here uses it was null whether the row was
    // open or shut — a guard that could not fail. Verified by planting the
    // mutant it was supposed to catch: it stayed green.
    expect(
      container
        .querySelector('[data-testid="lore-row"]')!
        .getAttribute("aria-expanded"),
    ).toBe("false");
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

  // ⚠️ THIS SPEC USED TO ASSERT THE OPPOSITE, and the flip is owner's, not a
  // convenience. It read 「tells the reader the id will be prepended, before
  // they send anything」 and required `lore-msg-prefix-note` to be PRESENT and
  // to name the entry — that line was the only place on screen saying that what
  // goes out is not what was typed. Owner removed the line in the round that
  // made this row look like a 任務卡 (任務卡 has no such line) and ruled the
  // round 外觀-only, so THE PREFIX ITSELF STAYS.
  //
  // 🔴 So the spec keeps the half that still holds and states the cost of the
  // half that changed: no note, and the id still leads the wire. Deleting the
  // spec instead would have left 「the note is gone」 unrecorded and 「the prefix
  // survived the note's removal」 unguarded — and the prefix is the half whose
  // loss lands on the OTHER person's side, where nothing on this screen shows
  // it.
  it("no longer shows the prefix note, and still sends the id ahead of the text", async () => {
    const posted: { to: string; body: string }[] = [];
    vi.spyOn(api, "postChat").mockImplementation(async (m) => {
      posted.push({ to: m.to, body: m.body });
    });

    const container = await openComposer("L-31");
    expect(
      container.querySelector('[data-testid="lore-msg-prefix-note"]'),
    ).toBeNull();

    const box = container.querySelector<HTMLTextAreaElement>(
      '[data-testid="lore-msg-input"]',
    )!;
    fireEvent.change(box, { target: { value: "這條還適用嗎" } });
    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-msg-send"]')!,
    );

    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0].body).toBe("[L-31] 這條還適用嗎");
  });

  // 🔴 THE BOX ON THE COLLAPSED ROW SITS ON THE ROW'S OWN TOGGLE SURFACE.
  // Every click inside `.lore-row` that is not filtered out by the row's
  // closest() list flips expanded — so a composer that renders while collapsed
  // and is NOT in that list folds the entry away as soon as the reader reaches
  // for it. On screen the box still accepts the click; what happens is the card
  // shuts. The textarea and the buttons are covered by the element entries in
  // that list; this asserts the SURFACE AROUND them is too.
  it("does not expand or collapse the row when the composer is clicked", async () => {
    const container = await openComposer("L-7");
    const composer = container.querySelector<HTMLElement>(
      '[data-testid="lore-author-composer"]',
    )!;

    fireEvent.click(composer);
    expect(
      container
        .querySelector('[data-testid="lore-row"]')!
        .getAttribute("aria-expanded"),
    ).toBe("false");

    const box = container.querySelector<HTMLTextAreaElement>(
      '[data-testid="lore-msg-input"]',
    )!;
    fireEvent.click(box);
    fireEvent.change(box, { target: { value: "打字不該把卡片摺起來" } });
    expect(
      container
        .querySelector('[data-testid="lore-row"]')!
        .getAttribute("aria-expanded"),
    ).toBe("false");
    expect(box.value).toBe("打字不該把卡片摺起來");
  });

  // The 📎 owner asked for (「加上附加檔案按鈕」): staged files ride the SAME
  // postChat call as the text, and a message that is ONLY files is sendable —
  // the 任務卡 rule. A 📎 whose files never reach `attachments` looks identical
  // on screen to one whose files do.
  it("carries staged files on the same message, and sends with no text at all", async () => {
    const posted: {
      body: string;
      attachments?: ChatAttachmentInput[];
    }[] = [];
    vi.spyOn(api, "postChat").mockImplementation(async (m) => {
      posted.push({ body: m.body, attachments: m.attachments });
    });

    const container = await openComposer("L-7");
    const send = container.querySelector<HTMLButtonElement>(
      '[data-testid="lore-msg-send"]',
    )!;
    // Empty box, nothing staged ⇒ the button cannot fire.
    expect(send.disabled).toBe(true);
    expect(
      container.querySelector('[data-testid="lore-msg-attach"]'),
    ).not.toBeNull();

    const fileInput = container.querySelector<HTMLInputElement>(
      '[data-testid="lore-author-composer"] input[type="file"]',
    )!;
    const file = new File(["hello"], "note.txt", { type: "text/plain" });
    fireEvent.change(fileInput, { target: { files: [file] } });

    // The strip appears, and the send button comes alive with NO text typed.
    await waitFor(() => expect(send.disabled).toBe(false));

    fireEvent.click(send);
    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0].body).toBe("[L-7] ");
    expect(posted[0].attachments).toHaveLength(1);
    expect(posted[0].attachments![0].filename).toBe("note.txt");
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

    // No click: the box is on the collapsed row now, and so is its absence.
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

  it("names the manual kind on every request, and drops both axes when nothing is ticked", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
      { typeKey: "deploy", displayName: "上線", purpose: "", fields: [] },
    ] as never);
    const spy = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    // 🔴 THE KIND STILL TRAVELS, even though only one kind can be ticked here
    // now (owner 2026-09-07: 「你第二個 filter 應該只需要放任務」). A request that
    // sent keys without kinds would narrow on one axis only, and the screen —
    // one ticked name, some rows — looks identical either way. The kind is also
    // what stops a manual key colliding with a member id on the other axis.
    tick(container, "lore-filter-belongs", "manual:review-pr");
    await waitFor(() => expect(lastOpts(spy).scopeKinds).toEqual(["manual"]));
    expect(lastOpts(spy).scopeKeys).toEqual(["review-pr"]);

    // A second manual ⇒ two keys, and the kind list does NOT grow a duplicate.
    tick(container, "lore-filter-belongs", "manual:deploy");
    await waitFor(() =>
      expect(lastOpts(spy).scopeKeys).toEqual(["review-pr", "deploy"]),
    );
    expect(lastOpts(spy).scopeKinds).toEqual(["manual"]);

    // Untick one ⇒ back to a single key, kind unchanged.
    tick(container, "lore-filter-belongs", "manual:deploy");
    await waitFor(() => expect(lastOpts(spy).scopeKeys).toEqual(["review-pr"]));
    expect(lastOpts(spy).scopeKinds).toEqual(["manual"]);

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

  it("has exactly four fields, and 範圍 / 角色 / 手冊 are gone", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    // 🔴 ABSENCE IS THE ASSERTION HERE. The owner named FOUR controls on
    // 2026-09-08 (所有撰寫人 / 所有成員傳承 / 所有任務傳承 / 所有狀態), and a
    // page that ADDED the member axis while keeping the old 範圍 / 角色 / 手冊
    // trio would satisfy every other spec in this file: each control works, the
    // order test still finds its four, and the screen merely has three extra
    // dropdowns nobody mentioned.
    for (const gone of [
      "lore-filter-scope",
      "lore-filter-role",
      "lore-filter-manual",
    ]) {
      expect(container.querySelector(`[data-testid="${gone}"]`)).toBeNull();
    }
    for (const present of [
      "lore-filter-author",
      "lore-filter-member",
      "lore-filter-belongs",
      "lore-filter-state",
    ]) {
      expect(container.querySelector(`[data-testid="${present}"]`)).not.toBeNull();
    }
  });

  // 🔴 ALL FOUR SAY 「所有」, NOT 「全部」 (owner 2026-09-08, verbatim:
  // 「所有撰寫人 / 所有成員傳承 / 所有任務傳承 / 所有狀態」). Three of these
  // labels already existed and two of them said 「全部」 — the row was mixing
  // two words for one idea, which teaches a reader that the two mean different
  // kinds of "no constraint". Nothing else in this file reads a default label,
  // so without this spec the row can drift back a word at a time.
  it("names all four unconstrained states 「所有…」, and never 「全部」", async () => {
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const labelOf = (testId: string) =>
      container
        .querySelector<HTMLElement>(`[data-testid="${testId}"]`)!
        .textContent!.replace(/\s+/g, "");

    expect(labelOf("lore-filter-author")).toContain("所有撰寫人");
    expect(labelOf("lore-filter-member")).toContain("所有成員傳承");
    expect(labelOf("lore-filter-belongs")).toContain("所有任務傳承");
    expect(labelOf("lore-filter-state")).toContain("所有狀態");

    // 🔴 AND THE OTHER WORD IS NOWHERE ON THE ROW. 「所有 X」 being present does
    // not say 「全部 X」 is gone: a row rendering both would pass every line
    // above. This is the half that fails when one label is reverted.
    const row = container.querySelector<HTMLElement>(
      '[data-testid="lore-filter"]',
    )!;
    expect(row.textContent).not.toContain("全部");
  });

  it("offers every manual and NO members — the member axis is 撰寫人", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
      { typeKey: "deploy", displayName: "上線", purpose: "", fields: [] },
    ] as never);
    // 🔴 A LIVE WORKER AND A STAFF MEMBER ARE STUBBED IN ON PURPOSE, and they
    // are the discriminating half of this test. The assertion below is that
    // members are ABSENT; with no members on the roster at all it would pass
    // for a control that still tried to list them. Both kinds are present and
    // both must be missing from this dropdown.
    vi.spyOn(api, "listOutsourceWorkers").mockResolvedValue([
      { id: "ow-7d8ad859dd9b", codename: "O-179" },
    ] as never);
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-belongs"]')!,
    );

    // Every manual is reachable without touching any other control.
    for (const value of ["manual:review-pr", "manual:deploy"]) {
      expect(
        container.querySelector(
          `[data-testid="lore-filter-belongs-opt-${value}"]`,
        ),
      ).not.toBeNull();
    }

    // 🔴 AND NO MEMBER IS OFFERED IN THIS ONE. Members have their OWN control
    // since 2026-09-08 (lore-filter-member, immediately to the left); this is
    // the manuals control and it must stay manuals-only, because the two
    // together are what make 「exactly one kind, exactly one key」 reachable at
    // all. A single dropdown listing both was what the owner rejected on
    // 2026-09-07 (「你第二個 filter 應該只需要放任務」) and putting members back
    // in here would rebuild it.
    for (const absent of ["agent:mira", "agent:ow-7d8ad859dd9b"]) {
      expect(
        container.querySelector(
          `[data-testid="lore-filter-belongs-opt-${absent}"]`,
        ),
      ).toBeNull();
    }

    // 🔴 THE LABEL IS THE MANUAL'S OWN NAME, with no kind word in front of it.
    // The kind word was there while the list mixed people and manuals and a
    // bare name could not say which; one kind needs no disambiguator, and the
    // design mock names this control 「所有任務」.
    const label = (value: string) =>
      container.querySelector(
        `[data-testid="lore-filter-belongs-opt-${value}"]`,
      )!.textContent!;
    expect(label("manual:review-pr")).toContain("PR 審查");
    expect(label("manual:review-pr")).not.toContain("任務傳承");
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

    // \U0001f534 EACH FILTER MUST BE OPENED BEFORE ITS OWN COUNT IS ASSERTED.
    // The shared control renders `${testId}-count-<value>` ONLY while its
    // dropdown is open, so asserting a closed filter's count selector is empty
    // is true no matter what the component does — it passed with a hardcoded
    // `count: 7` on every option. All four filters are walked here for that
    // reason, one open per assertion.
    for (const id of [
      "lore-filter-author",
      "lore-filter-member",
      "lore-filter-belongs",
      "lore-filter-state",
    ]) {
      const control = container.querySelector<HTMLElement>(
        `[data-testid="${id}"]`,
      );
      expect(control, `${id} is missing — this walk asserts nothing`).not.toBeNull();
      fireEvent.click(control!);
      // The dropdown really is open: its options are in the DOM. Without this
      // the loop would go back to measuring a closed control.
      expect(
        container.querySelectorAll(`[data-testid^="${id}-opt-"]`).length,
        `${id} did not open — the count assertion below would be vacuous`,
      ).toBeGreaterThan(0);
      expect(
        container.querySelectorAll(`[data-testid^="${id}-count-"]`),
        `${id} renders a per-option count badge — owner ruled 「we dont need count」`,
      ).toHaveLength(0);
      fireEvent.click(control!);
    }
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
      { typeKey: "deploy", displayName: "上線", purpose: "", fields: [] },
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

    // Two scopes ⇒ two budgets ⇒ no honest place for one line.
    //
    // ⚠️ THE PAIR IS TWO MANUALS NOW, not a member and a manual. This control
    // stopped offering members on 2026-09-07 (owner: 「你第二個 filter 應該只需要
    // 放任務」), so the two-KINDS case can no longer be produced from this row at
    // all — and a test that keeps naming it would be describing a screen nobody
    // can reach. Two budgets is still two budgets; that is the rule under test.
    tick("lore-filter-belongs", "manual:review-pr");
    tick("lore-filter-belongs", "manual:deploy");
    await waitFor(() =>
      expect(spy.mock.calls[spy.mock.calls.length - 1][0]?.scopeKeys).toEqual([
        "review-pr",
        "deploy",
      ]),
    );
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-cap-line"]'),
      ).toBeNull(),
    );

    // Converge: drop one manual, leaving exactly one. The line must return.
    tick("lore-filter-belongs", "manual:deploy");

    const line = await waitFor(() => {
      const el = container.querySelector('[data-testid="lore-cap-line"]');
      if (!el) throw new Error("the 上限線 did not come back");
      return el;
    });
    // 🔴 NAMED, not merely present. A line that appeared carrying the wrong
    // scope's name would be worse than none: it would tell the reader a budget
    // they are not looking at.
    expect(line.textContent).toContain("任務傳承");
    expect(line.textContent).toContain("PR 審查");
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
//   篩選列  撰寫人 → 成員傳承 → 任務傳承 → 狀態      (owner 2026-09-08, verbatim)
//   列上    編號 → 狀態 → 屬於                          (任務卡: 編號 → 優先權 → 狀態 → 類型)
describe("LorePage — 順序跟任務頁一致", () => {
  it("puts the four filters in the owner's order, and keeps that order as they are used", async () => {
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
            "lore-filter-member",
            "lore-filter-belongs",
            "lore-filter-state",
          ].includes(id),
        );

    expect(idsInDomOrder()).toEqual([
      "lore-filter-author",
      "lore-filter-member",
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
        "lore-filter-member",
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

// ────────────────────────────────────────────────────────────────────────────
// 成員傳承的上限線 (owner 2026-09-08).
//
// 🔴 WHY THIS EXISTS AT ALL. The server answers `capChars` only for a request
// carrying EXACTLY ONE scope kind and EXACTLY ONE scope key. Between 2026-09-07
// and this change the only place a member appeared on this page was 撰寫人,
// which sends `authorIds` and produces NO scope — so there was no sequence of
// clicks that could make a 成員傳承 cap line appear. Nothing was broken on
// screen: the page rendered, the filter worked, the rows were right, and the
// line was simply unreachable. That is the shape of defect a test has to be
// written for deliberately, because no screenshot shows it.
//
// The three specs below are the three paths, and they only mean something
// TOGETHER: ① the line comes back, ② two scopes still silence it (the honest
// existing behaviour, which a fix aimed at ① could easily trample), ③ 撰寫人
// does not touch it (which is what says ② is about SCOPES and not about
// "any two filters").
describe("LorePage — 成員傳承的上限線", () => {
  /** Answers as the server does: a cap ONLY when the request converged on a
   * single kind AND a single key. Everything below asserts against this rule
   * rather than restating it, so a page that invented its own line fails. */
  function stubCapWhenSingleScope() {
    return vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => {
      const one = o?.scopeKinds?.length === 1 && o?.scopeKeys?.length === 1;
      return {
        entries: [
          mkEntry({ id: "L-1", state: "active" }),
          mkEntry({ id: "L-2", state: "active" }),
        ],
        limit: 30,
        offset: 0,
        capChars: one ? 8000 : 0,
        firstDroppedId: one ? "L-2" : "",
      };
    });
  }

  function tick(container: HTMLElement, testId: string, value: string) {
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

  it("① one member ticked, no manual ⇒ the line comes back, named 成員傳承 · Mira", async () => {
    const spy = stubCapWhenSingleScope();
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    // Nothing ticked ⇒ no scope ⇒ no line. Asserted so the line below is a
    // CHANGE and not a thing that was always on screen.
    expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull();

    tick(container, "lore-filter-member", "agent:mira");

    // 🔴 THE REQUEST IS THE FIRST ASSERTION, because it is what the server
    // judges. A page that drew a line without sending one kind and one key
    // would be drawing a budget nobody answered for.
    await waitFor(() => {
      const sent = spy.mock.calls[spy.mock.calls.length - 1][0];
      expect(sent?.scopeKinds).toEqual(["agent"]);
      expect(sent?.scopeKeys).toEqual(["mira"]);
    });

    const line = await waitFor(() => {
      const el = container.querySelector('[data-testid="lore-cap-line"]');
      if (!el) throw new Error("the 成員傳承 上限線 did not appear");
      return el;
    });
    // Named, not merely present: a line carrying the wrong scope's name tells
    // the reader about a budget they are not looking at.
    expect(line.textContent).toContain("成員傳承");
    expect(line.textContent).toContain("Mira");
    expect(line.textContent).toContain("8000");
  });

  it("② one member AND one manual ⇒ two kinds, two keys, and NO line", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);
    const spy = stubCapWhenSingleScope();
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));

    tick(container, "lore-filter-member", "agent:mira");
    // The line is up at this point — asserting it FIRST is what makes the
    // disappearance below an event rather than a state that never changed.
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-cap-line"]'),
      ).not.toBeNull(),
    );

    tick(container, "lore-filter-belongs", "manual:review-pr");

    // 🔴 BOTH CONTROLS RIDE THE SAME TWO WIRE AXES. That is the mechanism the
    // "no line" answer follows from, so it is asserted rather than assumed.
    await waitFor(() => {
      const sent = spy.mock.calls[spy.mock.calls.length - 1][0];
      expect([...(sent?.scopeKinds ?? [])].sort()).toEqual(["agent", "manual"]);
      expect([...(sent?.scopeKeys ?? [])].sort()).toEqual(["mira", "review-pr"]);
    });
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-cap-line"]'),
      ).toBeNull(),
    );
  });

  it("③ 撰寫人 never touches the line — it sends no scope at all", async () => {
    const spy = stubCapWhenSingleScope();
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));

    // With a member scope in force the line is up…
    tick(container, "lore-filter-member", "agent:mira");
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-cap-line"]'),
      ).not.toBeNull(),
    );

    // …and ticking 撰寫人 must not disturb it. 撰寫人 is `authorIds`: a
    // different axis entirely, and one the server's cap rule never reads.
    tick(container, "lore-filter-author", "mira");
    await waitFor(() => {
      const sent = spy.mock.calls[spy.mock.calls.length - 1][0];
      expect(sent?.authorIds).toEqual(["mira"]);
      // 🔴 THE SCOPE AXES ARE UNCHANGED. If 撰寫人 ever started contributing a
      // scope key, this page would silently gain a second key and lose the
      // line — the failure would look like "the line disappeared for no
      // reason", which is the hardest kind to trace back here.
      expect(sent?.scopeKinds).toEqual(["agent"]);
      expect(sent?.scopeKeys).toEqual(["mira"]);
    });
    expect(
      container.querySelector('[data-testid="lore-cap-line"]'),
    ).not.toBeNull();
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 卡片版面 (owner 2026-09-08).
describe("LorePage — 訊息框在最下方", () => {
  it("is the LAST thing on the card, closed and open alike", async () => {
    stubList(page([mkEntry({ id: "L-7", authorId: "mira" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "L-7");

    // 🔴 ASSERTED AS "LAST CHILD", NOT AS "AFTER 內容". Owner's words were
    // 「訊息匡應該在最下方」 and the box used to sit between 內容 and
    // 撰寫人／生效期 — a version that merely moved it below 內容 would still be
    // in the middle of the card and would pass a weaker check.
    const lastOf = (el: HTMLElement) =>
      (el.lastElementChild as HTMLElement).getAttribute("data-testid");
    expect(lastOf(row)).toBe("lore-author-composer");
    expect(row.getAttribute("aria-expanded")).toBe("false");

    // And it stays last once the row is open — the two states are separate
    // arrangements of the same JSX and only one of them is exercised by a test
    // that never clicks.
    fireEvent.click(row.querySelector('[data-testid="lore-body"]')!);
    await waitFor(() =>
      expect(row.getAttribute("aria-expanded")).toBe("true"),
    );
    expect(lastOf(row)).toBe("lore-author-composer");
  });
});

describe("LorePage — 折疊的只有內容", () => {
  it("keeps 撰寫人 / 生效期 on the CLOSED row and clamps only the body", async () => {
    stubList(page([mkEntry({ id: "L-7", authorId: "mira" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "L-7");

    // 🔴 CLOSED, and the three meta rows are already there (owner 2026-09-08:
    // 「唯一要折疊的是 content」). They were expanded-only until this change.
    expect(row.getAttribute("aria-expanded")).toBe("false");
    expect(row.querySelector('[data-testid="lore-author-link"]')).not.toBeNull();
    expect(row.querySelector('[data-testid="lore-effective"]')).not.toBeNull();
    expect(row.querySelector('[data-testid="lore-bump"]')).not.toBeNull();

    // …and the body — the one thing 展開 still governs — is clamped.
    const body = () =>
      row.querySelector<HTMLElement>('[data-testid="lore-body"]')!;
    expect(body().className).toContain("lore-row__body--clamped");

    fireEvent.click(body());
    await waitFor(() => expect(row.getAttribute("aria-expanded")).toBe("true"));
    // 🔴 THE OTHER DIRECTION IS THE HALF THAT FAILS FOR A CARD THAT DROPPED
    // 展開／收合 ENTIRELY. Owner asked for the content to unfold, not for the
    // fold to go away; a body that is never clamped passes the line above.
    expect(body().className).not.toContain("lore-row__body--clamped");
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 內容走 markdown (owner 2026-09-08: 「要支援 md format」).
describe("LorePage — 內容是 markdown", () => {
  it("renders markdown structure instead of printing the source characters", async () => {
    stubList(
      page([
        mkEntry({
          id: "L-1",
          body: "**重點** 和 `code`\n\n- 一\n- 二",
        }),
      ]),
    );
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const body = container.querySelector<HTMLElement>(
      '[data-testid="lore-body"]',
    )!;

    // 🔴 STRUCTURE, NOT TEXT. `.textContent` contains 「重點」 both before and
    // after this change — a plain-text <div> would pass any assertion written
    // against the words. The ELEMENTS are what only a renderer can produce.
    expect(body.querySelector("strong")?.textContent).toBe("重點");
    expect(body.querySelector("code")?.textContent).toBe("code");
    expect(body.querySelectorAll("li")).toHaveLength(2);
    // And the source markers are gone from the reading surface.
    expect(body.textContent).not.toContain("**");
  });

  it("does not inject markup — the renderer builds elements, never HTML", async () => {
    // 🔴 A 傳承 body is AGENT-authored free text. The shared renderer never uses
    // dangerouslySetInnerHTML, and this pins that the wiring did not route
    // around it: the tag must survive as TEXT and must not become an element.
    stubList(page([mkEntry({ id: "L-1", body: "<img src=x onerror=1>" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const body = container.querySelector<HTMLElement>(
      '[data-testid="lore-body"]',
    )!;
    expect(body.querySelector("img")).toBeNull();
    expect(body.textContent).toContain("<img src=x onerror=1>");
  });

  it("leaves an underscore-bearing identifier alone", async () => {
    // The corpus that already exists was written as PLAIN TEXT, so the worry is
    // that a stored body's punctuation is now read as syntax. `_` is not inline
    // syntax in this renderer, and this is the case that prompted the question
    // (a live entry carrying `note_size_chars`), pinned so a future switch to a
    // fuller markdown engine has to notice it.
    stubList(page([mkEntry({ id: "L-1", body: "看 note_size_chars 這個欄位" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    expect(
      container.querySelector('[data-testid="lore-body"]')!.textContent,
    ).toContain("note_size_chars");
  });
});
