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
  it("tells a 角色傳承 from a 任務傳承 when both are named 特助, without opening either row", async () => {
    vi.spyOn(api, "listRoles").mockResolvedValue([
      { key: "assistant", name: "特助" },
    ] as never);
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      // A manual that happens to carry the SAME display name as the role.
      { typeKey: "assistant-work", displayName: "特助", purpose: "", fields: [] },
    ] as never);

    stubList(
      page([
        mkEntry({ id: "L-1", scopeKind: "role", scopeKey: "assistant" }),
        mkEntry({ id: "L-2", scopeKind: "manual", scopeKey: "assistant-work" }),
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
    const roleText = scopeOf("L-1");
    const manualText = scopeOf("L-2");

    // …and they do not read the same. 🔴 This inequality is the assertion that
    // survives a rename of either word; asserting the literal 「角色傳承 · 特助」
    // would also pass for a page that printed the kind and dropped the name.
    expect(roleText).not.toBe(manualText);
    expect(roleText).toContain("角色傳承");
    expect(roleText).toContain("特助");
    expect(manualText).toContain("任務傳承");
    expect(manualText).toContain("特助");
  });

  it("names 成員傳承 as its own kind, not as a role", async () => {
    // An outsource worker has no role; its lore hangs off its own member id.
    // Reading it as 角色傳承 would tell the reader to look somewhere that does
    // not hold it.
    const agent = await scopePillFor("agent", "ow-nobody");
    const chip = agent.closest('[data-testid="lore-scope"]')!;
    expect(chip.textContent).toContain("成員傳承");
    expect(chip.textContent).not.toContain("角色傳承");
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
// 篩選器改複選 (owner rc-0376bf875757 option ②: 全部改複選, 範圍複選時上限線就不畫).
//
// 🔴 THE ONE THAT FAILS SILENTLY IS THE ORPHANED KEY. `scope_kinds` and
// `scope_keys` are ANDed on the wire. Tick 角色傳承, tick a role, then UNTICK
// 角色傳承: the role list disappears from the screen, so the reader believes
// they widened the page — but if the key set is still sent, the request is now
// 「any kind, but only this role's key」, which narrows or empties a page they
// asked to broaden. Nothing errors. The screen shows a filter row with one less
// constraint on it and a list with fewer rows in it, and the two never meet.
//
// The state is KEPT so re-ticking the kind restores the picks; it is the
// REQUEST that must drop them. So the assertion is on what was sent.
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

  it("stops sending a role key once 角色傳承 is unticked", async () => {
    vi.spyOn(api, "listRoles").mockResolvedValue([
      { key: "assistant", name: "特助" },
    ] as never);
    const spy = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    tick(container, "lore-filter-scope", "role");
    await waitFor(() => expect(lastOpts(spy).scopeKinds).toEqual(["role"]));

    tick(container, "lore-filter-role", "assistant");
    await waitFor(() => expect(lastOpts(spy).scopeKeys).toEqual(["assistant"]));

    // Untick the KIND. The role list leaves the screen; the key must leave the
    // request with it.
    tick(container, "lore-filter-scope", "role");
    await waitFor(() => expect(lastOpts(spy).scopeKinds).toBeUndefined());
    // 🔴 THE WHOLE POINT. `scopeKeys` still carrying ["assistant"] here is a
    // request for 「every kind, but only entries keyed assistant」 — narrower
    // than the screen says, with nothing to show for it.
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

  it("shows the manual list only while 任務傳承 is ticked", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    // 🔴 This is the owner's original complaint: the manual list used to sit in
    // the same dropdown as the two non-task scopes, so a list of tasks appeared
    // to have things in it that are not tasks. Splitting them is only a fix if
    // the manual list is genuinely absent until asked for.
    expect(
      container.querySelector('[data-testid="lore-filter-manual"]'),
    ).toBeNull();

    tick(container, "lore-filter-scope", "manual");
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-filter-manual"]'),
      ).not.toBeNull(),
    );

    tick(container, "lore-filter-scope", "manual");
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-filter-manual"]'),
      ).toBeNull(),
    );
  });
});
