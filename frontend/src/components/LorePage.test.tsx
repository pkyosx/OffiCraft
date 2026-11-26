// LorePage — what an open section draws from the server's numbers, the card,
// its actions, and the filters.
//
//   界線. `firstDroppedId` and `capChars` are the SERVER'S answer over the
//   whole scope, so the fixture's TEXT LENGTHS ARE DELIBERATELY NOT A CLUE: the
//   entry named by `firstDroppedId` is one of the SHORTEST rows on screen. A
//   local sum cannot produce this answer. `capChars === 0` means no line AND no
//   dimming.
//
//   撰寫人不在名冊上. The failure mode is a LIVE icon — a 傳訊息 button that
//   opens a chat with a peer who is gone — so the whole affordance must be a
//   non-button.
//
// Most specs open ONE section (Mira's) whose list request is stubbed: what the
// page does with the answer is under test here, not the mock's arithmetic.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { LorePage, loreBodyOverflow } from "./LorePage";
import { __resetMock } from "../api/mock";
import { api } from "../api";
// The 可變動作面 specs need to build a REFUSAL, and the refusal has to be the
// same class the adapters throw — `runAction` branches on `status` and reads
// `serverMessage`, neither of which a plain Error carries.
import { ApiError } from "../api/errors";
import { hasRegisteredUnsavedWork } from "../lib/unsavedInput";
import { codeForStatus } from "../api/errorCodes";
// The action-error copy is asserted by VALUE, not by "is non-empty": an error
// line saying the wrong sentence is a line that passed a non-empty check.
import { zh } from "../i18n/locales/zh";
import { clearLocale, useEnglishLocale } from "../test/effortOptions";
import type {
  ChatAttachmentInput,
  LoreBudgetTargetView,
  LoreEntryPageView,
  LoreEntryView,
} from "../api/adapter";

function mkEntry(over: Partial<LoreEntryView> & { id: string }): LoreEntryView {
  return {
    seq: 1,
    scopeKind: "agent",
    scopeKey: "mira",
    taskTypeKey: "",
    scopeOptions: [],
    title: `標題 ${over.id}`,
    body: `內容 ${over.id}`,
    authorId: "mira",
    sourceTaskId: "",
    state: "active",
    retireReason: "",
    effectiveTs: 1788400000,
    createdTs: 1788400000,
    updatedTs: 1788400000,
    loreType: "other",
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
  return {
    entries,
    limit: 30,
    offset: 0,
    usedChars: 0,
    firstDroppedChars: 0,
    budgetSegments: [],
    ...cap,
  };
}

const MIRA_TARGET: LoreBudgetTargetView = {
  scopeKind: "agent",
  scopeKey: "mira",
  capChars: 10000,
  usedChars: 0,
  entryCount: 0,
  droppedCount: 0,
};

function stubList(answer: LoreEntryPageView) {
  return vi.spyOn(api, "listLoreEntries").mockResolvedValue(answer);
}

function renderPage(props: { canSetScope?: boolean } = {}) {
  return render(
    <I18nProvider>
      <LorePage {...props} />
    </I18nProvider>,
  );
}

/** Render with Mira's section as the only target and open its section page
 * from the directory. */
async function renderOpen(props: { canSetScope?: boolean } = {}) {
  if (!vi.isMockFunction(api.listLoreBudgets)) {
    vi.spyOn(api, "listLoreBudgets").mockResolvedValue([MIRA_TARGET]);
  }
  history.replaceState(null, "", window.location.pathname);
  const r = renderPage(props);
  const toggle = await waitFor(() => {
    const el = r.container.querySelector<HTMLElement>('[data-testid="lore-target"]');
    if (!el) throw new Error("no directory target");
    return el;
  }, { timeout: 3000 });
  fireEvent.click(toggle);
  return r;
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
  // The page reads its section from the hash; every spec starts on the directory.
  history.replaceState(null, "", window.location.pathname);
  __resetMock();
  clearLocale();
  // jsdom has no layout; every body counts as overflowing its clamped line
  // unless a spec says otherwise, so the row stays an expand toggle.
  vi.spyOn(loreBodyOverflow, "measure").mockReturnValue(true);
});

afterEach(() => {
  vi.restoreAllMocks();
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

  it("dims firstDroppedId and everything under it, and draws the line above it with used / cap and what it needs", async () => {
    stubList({
      ...page(entries, { capChars: 10000, firstDroppedId: "mid" }),
      usedChars: 9640,
      firstDroppedChars: 520,
    });
    const { container } = await renderOpen();

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

    const lines = container.querySelectorAll('[data-testid="lore-cap-line"]');
    expect(lines).toHaveLength(1);
    expect(lines[0].textContent).toBe(
      "上限線　已用 9,640 / 10,000 字・mid 需要 520 字，從這裡以下讀不到",
    );

    // …and it sits BEFORE the row it names in document order.
    expect(
      lines[0].compareDocumentPosition(rowById(container, "mid")) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("draws no line and dims nothing when capChars is 0 (filter did not converge)", async () => {
    stubList(page(entries, { capChars: 0, firstDroppedId: "" }));
    const { container } = await renderOpen();

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
    const { container } = await renderOpen();

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
    const { container } = await renderOpen();

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
    const { container } = await renderOpen();

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
  const { container } = await renderOpen();
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
  it("names the manual, a built-in one in the UI language, and jumps to it; a member names but does not jump", async () => {
    useEnglishLocale();
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "builtin-role-design", displayName: "建立／修改角色", purpose: "", fields: [] },
    ] as never);

    const manual = await scopePillFor("manual", "builtin-role-design");
    expect(manual.tagName).toBe("BUTTON");
    expect(manual.textContent).toContain("Create / edit role");
    window.location.hash = "";
    fireEvent.click(manual);
    // The SAME jump the 任務卡's 類型 chip makes.
    expect(window.location.hash).toBe("#settings/manuals/builtin-role-design");
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
    const chip = orphan.closest('[data-testid="lore-scope"]')!;
    expect(chip.querySelectorAll(".lore-row__scope-glyph")).toHaveLength(0);
  });

  it("an everyone entry's badge reads 所有人 with the group glyph", async () => {
    const everyone = await scopePillFor("everyone", "");
    expect(everyone.tagName).toBe("SPAN");
    expect(everyone.textContent).toBe("所有人");
    const chip = everyone.closest('[data-testid="lore-scope"]')!;
    expect(
      Array.from(chip.querySelectorAll(".lore-row__scope-glyph")).map((g) =>
        g.getAttribute("class"),
      ),
    ).toEqual(["lore-row__scope-glyph lore-row__scope-glyph--everyone"]);
  });

  it("a viewer who may not switch scopes gets an inert badge; one who may gets a menu trigger", async () => {
    stubList(
      page([
        mkEntry({
          id: "s1",
          scopeKind: "agent",
          scopeKey: "mira",
          scopeOptions: ["agent", "everyone"],
        }),
      ]),
    );
    const viewer = await renderOpen({ canSetScope: false });
    await waitFor(() => expect(renderedIds(viewer.container)).toHaveLength(1));
    const inert = viewer.container.querySelector<HTMLElement>(
      '[data-testid="lore-scope-name"]',
    )!;
    expect(inert.tagName).toBe("SPAN");
    expect(inert.textContent).toBe("建立者：Mira");
    expect(inert.hasAttribute("aria-haspopup")).toBe(false);
    expect(inert.querySelector(".lore-row__scope-caret")).toBeNull();
    viewer.unmount();

    const owner = await renderOpen({ canSetScope: true });
    await waitFor(() => expect(renderedIds(owner.container)).toHaveLength(1));
    const trigger = owner.container.querySelector<HTMLElement>(
      '[data-testid="lore-scope-name"]',
    )!;
    expect(trigger.tagName).toBe("BUTTON");
    expect(trigger.textContent).toBe("建立者：Mira");
    expect(trigger.getAttribute("aria-haspopup")).toBe("menu");
    expect(trigger.querySelector(".lore-row__scope-caret")).not.toBeNull();
  });

  it("a non-owner clicking a manual badge opens that manual and no menu", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);
    stubList(
      page([
        mkEntry({
          id: "s1",
          scopeKind: "manual",
          scopeKey: "review-pr",
          taskTypeKey: "review-pr",
          sourceTaskId: "T-5",
          scopeOptions: ["manual", "agent", "everyone"],
        }),
      ]),
    );
    const { container } = await renderOpen({ canSetScope: false });
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-scope-name"]')?.textContent,
      ).toBe("任務：PR 審查"),
    );
    window.location.hash = "";
    fireEvent.click(container.querySelector('[data-testid="lore-scope-name"]')!);
    expect(window.location.hash).toBe("#settings/manuals/review-pr");
    expect(container.querySelector('[data-testid="lore-scope-options"]')).toBeNull();
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
    const { container } = await renderOpen();
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
    const { container } = await renderOpen();

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
    window.location.hash = "#lore/agent/mira";
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));

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
    const { container } = await renderOpen();
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

    expect(memberScope.textContent).toBe("建立者：Mira");
    expect(manualScope.textContent).toBe("任務：Mira");

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
    const { container } = await renderOpen();
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
    // The shape spells out WHICH KIND of id it carries since owner
    // rc-01a07b1b2a12 / rc-379631993586 「ok. B.」; the bare 「[L-7] 」 it replaced
    // was indistinguishable from the 任務 box's own bracket.
    expect(posted[0].body).toBe("[LoreID=L-7] 這條還適用嗎");
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
    expect(posted[0].body).toBe("[LoreID=L-31] 這條還適用嗎");
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
    expect(posted[0].body).toBe("[LoreID=L-7] ");
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
    const { container } = await renderOpen();
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

// 篩選器: every filter travels on the section's own request.
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

  it("asks for 置頂 and 生效中 by default, and ticking 已失效 adds it to the same request", async () => {
    const spy = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    expect(lastOpts(spy)).toEqual({
      states: ["pinned", "active"],
      scopeKinds: ["agent"],
      scopeKeys: ["mira"],
      limit: 30,
      offset: 0,
    });

    tick(container, "lore-filter-state", "retired");

    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({
        states: ["pinned", "active", "retired"],
        scopeKinds: ["agent"],
        scopeKeys: ["mira"],
        limit: 30,
        offset: 0,
      }),
    );
  });

  it("sends the ticked types as loreTypes, lists all five in order, and 清除篩選 drops them", async () => {
    const spy = stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    expect(
      container.querySelector('[data-testid="lore-filter-clear"]'),
    ).toBeNull();

    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-type"]')!,
    );
    const options = Array.from(
      container.querySelectorAll('[data-testid^="lore-filter-type-opt-"]'),
    ).map((el) => [el.getAttribute("data-testid"), el.textContent]);
    expect(options).toEqual([
      ["lore-filter-type-opt-instruction_conflict", "指示衝突"],
      ["lore-filter-type-opt-instruction_supplement", "指示補充"],
      ["lore-filter-type-opt-owner_decision", "Owner 決策"],
      ["lore-filter-type-opt-owner_preference", "Owner 偏好"],
      ["lore-filter-type-opt-other", "其他"],
    ]);

    const mira = { scopeKinds: ["agent"], scopeKeys: ["mira"], limit: 30, offset: 0 };
    tick(container, "lore-filter-type", "owner_decision");
    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({
        states: ["pinned", "active"],
        loreTypes: ["owner_decision"],
        ...mira,
      }),
    );
    tick(container, "lore-filter-type", "other");
    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({
        states: ["pinned", "active"],
        loreTypes: ["owner_decision", "other"],
        ...mira,
      }),
    );

    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-clear"]')!,
    );
    await waitFor(() =>
      expect(lastOpts(spy)).toEqual({ states: ["pinned", "active"], ...mira }),
    );
    expect(
      container.querySelector('[data-testid="lore-filter-clear"]'),
    ).toBeNull();
  });

  it("has the id box and the three filters", async () => {
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    for (const present of [
      "lore-filter-id",
      "lore-filter-author",
      "lore-filter-type",
      "lore-filter-state",
    ]) {
      expect(container.querySelector(`[data-testid="${present}"]`)).not.toBeNull();
    }
  });

  it("names an unconstrained filter 「所有…」 and shows the default state set as two ticks", async () => {
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const labelOf = (testId: string) =>
      container
        .querySelector<HTMLElement>(`[data-testid="${testId}"]`)!
        .textContent!.replace(/\s+/g, "");

    expect(labelOf("lore-filter-author")).toBe("所有撰寫人");
    expect(labelOf("lore-filter-type")).toBe("所有類型");
    expect(labelOf("lore-filter-state")).toBe("狀態·2");
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
//   篩選列  編號 → 撰寫人 → 類型 → 狀態                (任務頁: 任務編號 → 負責人 → 類型 → 狀態)
//   列上    編號 → 類型 → 狀態 → 屬於                   (任務卡: 編號 → 優先權 → 狀態 → 類型)
describe("LorePage — 順序跟任務頁一致", () => {
  it("puts the id box and the three filters in the 任務頁 order, and keeps that order as they are used", async () => {
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const fields = ["lore-filter-id", "lore-filter-author", "lore-filter-type", "lore-filter-state"];
    const idsInDomOrder = () =>
      Array.from(container.querySelectorAll('[data-testid^="lore-filter-"]'))
        .map((el) => el.getAttribute("data-testid")!)
        .filter((id) => fields.includes(id));

    expect(idsInDomOrder()).toEqual(fields);

    fireEvent.click(container.querySelector<HTMLElement>('[data-testid="lore-filter-type"]')!);
    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-type-opt-other"] input')!,
    );
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    expect(idsInDomOrder()).toEqual(fields);
  });

  it("reads 編號 → 類型 → 狀態 → 屬於 on the row", async () => {
    stubList(page([mkEntry({ id: "L-9", state: "pinned" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));

    const row = rowById(container, "L-9");
    const marks = Array.from(
      row.querySelectorAll(
        '[data-testid="lore-entry-id"], [data-testid="lore-type"], [data-testid="lore-state"], [data-testid="lore-scope"]',
      ),
    ).map((el) => el.getAttribute("data-testid"));

    // 🔴 The id LEADS, the way 任務卡's #T-1 does: it is what you quote to
    // somebody else.
    expect(marks).toEqual(["lore-entry-id", "lore-type", "lore-state", "lore-scope"]);
    // Nothing sits between the checkbox and the id.
    expect(row.querySelector(".lore-row__badges")!.firstElementChild!.getAttribute("data-testid")).toBe(
      "lore-entry-id",
    );
  });

  it("shows the type tag right after the id on every collapsed row, ahead of the title", async () => {
    stubList(
      page([
        mkEntry({ id: "L-1", loreType: "instruction_conflict", effectiveTs: 6 }),
        mkEntry({ id: "L-2", loreType: "instruction_supplement", effectiveTs: 5 }),
        mkEntry({ id: "L-3", loreType: "owner_decision", effectiveTs: 4 }),
        mkEntry({ id: "L-4", loreType: "owner_preference", effectiveTs: 3 }),
        mkEntry({ id: "L-5", loreType: "other", effectiveTs: 2 }),
      ]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));

    const tags = ["L-1", "L-2", "L-3", "L-4", "L-5"].map((id) => {
      const row = rowById(container, id);
      expect(row.getAttribute("aria-expanded")).toBe("false");
      const order = Array.from(
        row.querySelectorAll('[data-testid="lore-type"], [data-testid="lore-entry-id"], .lore-row__title'),
      ).map((el) => el.getAttribute("data-testid") ?? "title");
      const tag = row.querySelector('[data-testid="lore-type"]');
      return [id, order, tag?.textContent ?? null];
    });
    const order = ["lore-entry-id", "lore-type", "title"];
    expect(tags).toEqual([
      ["L-1", order, "指示衝突"],
      ["L-2", order, "指示補充"],
      ["L-3", order, "Owner 決策"],
      ["L-4", order, "Owner 偏好"],
      ["L-5", order, "其他"],
    ]);
  });

  it("labels the type tag in English under the en language", async () => {
    localStorage.setItem("oc.language", "en");
    try {
      stubList(
        page([
          mkEntry({ id: "L-1", loreType: "instruction_conflict", effectiveTs: 5 }),
          mkEntry({ id: "L-2", loreType: "instruction_supplement", effectiveTs: 4 }),
          mkEntry({ id: "L-3", loreType: "owner_decision", effectiveTs: 3 }),
          mkEntry({ id: "L-4", loreType: "owner_preference", effectiveTs: 2 }),
          mkEntry({ id: "L-5", loreType: "other", effectiveTs: 1 }),
        ]),
      );
      const { container } = await renderOpen();
      await waitFor(() => expect(renderedIds(container)).toHaveLength(5));
      expect(
        ["L-1", "L-2", "L-3", "L-4", "L-5"].map(
          (id) => rowById(container, id).querySelector('[data-testid="lore-type"]')?.textContent,
        ),
      ).toEqual([
        "Instruction conflict",
        "Instruction supplement",
        "Owner decision",
        "Owner preference",
        "Other",
      ]);
    } finally {
      localStorage.removeItem("oc.language");
    }
  });

  it("draws the id badge like 任務卡's — the # and a glyph, not a bare number", async () => {
    stubList(page([mkEntry({ id: "L-9" })]));
    const { container } = await renderOpen();
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
// 卡片版面 (owner 2026-09-08).
describe("LorePage — 訊息框在最下方", () => {
  it("is the LAST thing on the card, closed and open alike", async () => {
    stubList(page([mkEntry({ id: "L-7", authorId: "mira" })]));
    const { container } = await renderOpen();
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
    const { container } = await renderOpen();
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
    const { container } = await renderOpen();
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
    const { container } = await renderOpen();
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
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    expect(
      container.querySelector('[data-testid="lore-body"]')!.textContent,
    ).toContain("note_size_chars");
  });
});

// ────────────────────────────────────────────────────────────────────────────
// 可變動作面 — 失效 / 生效 / 置頂 / 提到最新, and the 403 path `runAction`
// exists for.
//
// 🔴 WHY THESE ARE FOUR SPECS AND NOT ONE. The four actions share `runAction`
// but each one names its OWN api call and its OWN target value. A single spec
// that drove all four would go green while three of them were gutted, because
// the fourth's call would satisfy it. Split, a gutted action reddens exactly
// its own line.
//
// 🔴 AND WHY THE 403 SPEC IS THE ONE THAT MATTERS MOST. 置頂 is admin-only and
// 失效/生效/提到最新 is author-or-admin, so a refusal is a REACHABLE answer, not
// a bug. A swallowed 403 and 「什麼都沒發生」 are the same pixels: the row keeps
// its old badge either way. `lore-action-error` carrying WORDS is the only
// thing that tells those two apart, so the spec asserts the element exists AND
// that it says something.
describe("LorePage — 可變動作面", () => {
  /** Two rows, always. Acting on the SECOND one is what makes the id
   * assertion real — a call hardwired to the first entry would pass against a
   * one-row fixture. */
  function twoRows(secondState: LoreEntryView["state"] = "active") {
    return page([
      mkEntry({ id: "L-1", state: "active" }),
      mkEntry({ id: "L-2", state: secondState }),
    ]);
  }

  /** Open the 狀態 menu ON ONE ROW and click one of its items. */
  function chooseState(
    container: HTMLElement,
    entryId: string,
    next: LoreEntryView["state"],
  ) {
    const row = rowById(container, entryId);
    fireEvent.click(row.querySelector('[data-testid="lore-state"]')!);
    const item = row.querySelector(`[data-testid="lore-state-${next}"]`);
    if (!item) throw new Error(`no ${next} menu item on ${entryId}`);
    fireEvent.click(item);
  }

  function bump(container: HTMLElement, entryId: string) {
    fireEvent.click(
      rowById(container, entryId).querySelector('[data-testid="lore-bump"]')!,
    );
  }

  async function renderTwo(secondState: LoreEntryView["state"] = "active") {
    const list = stubList(twoRows(secondState));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    return { container, list };
  }

  it("失效 asks the server for retired, on the entry that was clicked", async () => {
    const set = vi
      .spyOn(api, "setLoreEntryState")
      .mockResolvedValue(undefined);
    const { container } = await renderTwo("active");

    chooseState(container, "L-2", "retired");

    await waitFor(() => expect(set).toHaveBeenCalledTimes(1));
    expect(set).toHaveBeenCalledWith("L-2", "retired");
  });

  it("生效 asks the server for active, on the entry that was clicked", async () => {
    const set = vi
      .spyOn(api, "setLoreEntryState")
      .mockResolvedValue(undefined);
    const { container } = await renderTwo("retired");

    chooseState(container, "L-2", "active");

    await waitFor(() => expect(set).toHaveBeenCalledTimes(1));
    expect(set).toHaveBeenCalledWith("L-2", "active");
  });

  it("置頂 asks the server for pinned on the entry that was clicked, and repaints that card where it stands without re-reading page one or moving the scroll", async () => {
    const set = vi
      .spyOn(api, "setLoreEntryState")
      .mockResolvedValue(undefined);
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.entryIds ? page([mkEntry({ id: "L-2", state: "pinned" })]) : twoRows("active"),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    const scroller = container.querySelector<HTMLElement>('[data-testid="lore-page"]')!;
    scroller.scrollTop = 240;

    chooseState(container, "L-2", "pinned");

    await waitFor(() => expect(set).toHaveBeenCalledTimes(1));
    expect(set).toHaveBeenCalledWith("L-2", "pinned");
    await waitFor(() =>
      expect(rowById(container, "L-2").querySelector('[data-testid="lore-state"]')!.textContent).toBe(
        zh.lore.statePinned,
      ),
    );
    expect(renderedIds(container)).toEqual(["L-1", "L-2"]);
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
      { scopeKinds: ["agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0 },
    ]);
    expect(scroller.scrollTop).toBe(240);
  });

  it("提到最新 calls bump on the entry that was clicked and re-reads only that entry", async () => {
    // 🔴 A DIFFERENT ROUTE, NOT A FOURTH STATE. 提到最新 moves `effectiveTs`
    // and leaves `state` alone, so it must NOT arrive as a setState call.
    const set = vi
      .spyOn(api, "setLoreEntryState")
      .mockResolvedValue(undefined);
    const bumpSpy = vi.spyOn(api, "bumpLoreEntry").mockResolvedValue(undefined);
    const { container, list } = await renderTwo("active");

    bump(container, "L-2");

    await waitFor(() => expect(bumpSpy).toHaveBeenCalledTimes(1));
    expect(bumpSpy).toHaveBeenCalledWith("L-2");
    expect(set).not.toHaveBeenCalled();
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));
    expect(list.mock.calls[1][0]).toEqual({
      scopeKinds: ["agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0,
    });
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-2", "L-1"]));
  });

  // ── 403 必須讀起來像被拒絕 ────────────────────────────────────────────────
  it("a refused 置頂 SAYS SO — the error line is on screen and carries words", async () => {
    vi.spyOn(api, "setLoreEntryState").mockRejectedValue(
      new ApiError("http 403 for POST /api/lore/L-2/state", 403, "forbidden", ""),
    );
    const { container } = await renderTwo("active");
    expect(
      container.querySelector('[data-testid="lore-action-error"]'),
    ).toBeNull();

    chooseState(container, "L-2", "pinned");

    const line = await waitFor(() => {
      const el = container.querySelector<HTMLElement>(
        '[data-testid="lore-action-error"]',
      );
      if (!el) throw new Error("no action error line");
      return el;
    });
    // Presence alone is not the property: an empty red div reads as a glitch,
    // not as a refusal. The envelope carried no sentence here, so this is our
    // own copy — and it must be the REFUSAL copy, not the generic failure one.
    expect(line.textContent).toBe(zh.lore.forbidden);
    expect(line.textContent).not.toBe(zh.lore.actionFailed);
  });

  it("a refused 提到最新 SAYS SO, in the server's own words when it sent any", async () => {
    // 🔴 The server's sentence says WHICH of the two rules refused
    // (admin-only vs author-or-admin). Dropping it for our generic copy loses
    // the only part the reader could act on.
    vi.spyOn(api, "bumpLoreEntry").mockRejectedValue(
      new ApiError(
        "http 403 for POST /api/lore/L-2/bump",
        403,
        "forbidden",
        "只有撰寫人或管理員可以提到最新",
      ),
    );
    const { container } = await renderTwo("active");

    bump(container, "L-2");

    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-action-error"]')?.textContent,
      ).toBe("只有撰寫人或管理員可以提到最新"),
    );
  });

  // ── 成功之後清單真的重讀 ──────────────────────────────────────────────────
  // ── 失敗之後畫面沒有假裝成功 ──────────────────────────────────────────────
  it("a refused action leaves the badge on its OLD value and re-asks nothing", async () => {
    // 🔴 READ, NOT ASSUMED: `runAction` writes NOTHING to `entries` before the
    // await — the badge is rendered from the server's `entry.state`, and the
    // only writer is the list load. So there is no optimistic value to roll
    // back, and the property to pin is that none appears. `setReloadNonce` is
    // inside the try AFTER the await, so a refusal must not re-read either —
    // a re-read on failure would repaint the row and read as "it went through,
    // then came back".
    vi.spyOn(api, "setLoreEntryState").mockRejectedValue(
      new ApiError("http 403 for POST /api/lore/L-2/state", 403, "forbidden", ""),
    );
    const { container, list } = await renderTwo("active");
    const badge = () =>
      rowById(container, "L-2").querySelector('[data-testid="lore-state"]')!
        .textContent;
    expect(badge()).toBe(zh.lore.stateActive);

    chooseState(container, "L-2", "pinned");

    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-action-error"]'),
      ).not.toBeNull(),
    );
    // Not 置頂. Not a row that moved into the 置頂 group.
    expect(badge()).toBe(zh.lore.stateActive);
    expect(renderedIds(container)).toEqual(["L-1", "L-2"]);
    expect(list).toHaveBeenCalledTimes(1);
  });
});

describe("LorePage — 提到最新移到最前面", () => {
  const ROW_H = 100;
  /** jsdom has no layout: rows stack ROW_H apart in DOM order under a
   * scrollport whose top is 0. */
  function fakeLayout(container: HTMLElement, viewH = 500, contentH = 5000) {
    const scroller = container.querySelector<HTMLElement>('[data-testid="lore-page"]')!;
    Object.defineProperty(scroller, "clientHeight", { configurable: true, get: () => viewH });
    Object.defineProperty(scroller, "scrollHeight", { configurable: true, get: () => contentH });
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
      const box = (top: number, h: number) =>
        ({ top, bottom: top + h, left: 0, right: 300, width: 300, height: h, x: 0, y: top, toJSON() {} }) as DOMRect;
      if (this === scroller) return box(0, viewH);
      if (this instanceof HTMLElement && this.dataset.testid === "lore-row") {
        const rows = Array.from(container.querySelectorAll('[data-testid="lore-row"]'));
        return box(rows.indexOf(this) * ROW_H - scroller.scrollTop, ROW_H);
      }
      return box(0, 0);
    });
    return scroller;
  }

  const rows = () => [
    mkEntry({ id: "P", state: "pinned", effectiveTs: 10 }),
    mkEntry({ id: "A1", effectiveTs: 9 }),
    mkEntry({ id: "A2", effectiveTs: 8 }),
    mkEntry({ id: "A3", effectiveTs: 7 }),
    mkEntry({ id: "A4", effectiveTs: 6 }),
  ];

  async function renderRows() {
    vi.spyOn(api, "bumpLoreEntry").mockResolvedValue(undefined);
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.entryIds ? page([mkEntry({ id: o.entryIds[0], effectiveTs: 99 })]) : page(rows()),
    );
    const r = await renderOpen();
    await waitFor(() => expect(renderedIds(r.container)).toEqual(["P", "A1", "A2", "A3", "A4"]));
    return { ...r, list };
  }

  it("moves the bumped card right after the pinned ones", async () => {
    const { container } = await renderRows();
    fireEvent.click(rowById(container, "A3").querySelector('[data-testid="lore-bump"]')!);
    await waitFor(() => expect(renderedIds(container)).toEqual(["P", "A3", "A1", "A2", "A4"]));
  });

  it("keeps the card that starts the view where it was on screen", async () => {
    const { container } = await renderRows();
    const scroller = fakeLayout(container);
    // A2 starts the view (top 0); A4 sits below it.
    scroller.scrollTop = 200;

    fireEvent.click(rowById(container, "A4").querySelector('[data-testid="lore-bump"]')!);

    await waitFor(() => expect(renderedIds(container)).toEqual(["P", "A4", "A1", "A2", "A3"]));
    // A4 landing above A2 pushes it down a row; the scroll follows.
    expect(scroller.scrollTop).toBe(300);
    expect(rowById(container, "A2").getBoundingClientRect().top).toBe(0);
  });

  it("shows 「已移到最前面 · 前往」; 前往 scrolls to the card and focuses it", async () => {
    const scrolled: string[] = [];
    Element.prototype.scrollIntoView = function (this: Element) {
      scrolled.push((this as HTMLElement).dataset.entryId ?? "");
    };
    try {
      const { container } = await renderRows();
      fireEvent.click(rowById(container, "A3").querySelector('[data-testid="lore-bump"]')!);
      const toast = await waitFor(() => {
        const el = container.querySelector<HTMLElement>('[data-testid="lore-toast"]');
        if (!el) throw new Error("no toast");
        return el;
      });
      expect(toast.textContent).toBe("已移到最前面·前往");
      fireEvent.click(toast.querySelector('[data-testid="lore-toast-go"]')!);
      expect(scrolled).toEqual(["A3"]);
      expect(document.activeElement).toBe(rowById(container, "A3"));
      expect(container.querySelector('[data-testid="lore-toast"]')).toBeNull();
    } finally {
      delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView;
    }
  });

  it("the toast goes away by itself", async () => {
    const { container } = await renderRows();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      fireEvent.click(rowById(container, "A3").querySelector('[data-testid="lore-bump"]')!);
      await waitFor(() => expect(container.querySelector('[data-testid="lore-toast"]')).not.toBeNull());
      await act(async () => {
        vi.advanceTimersByTime(5000);
      });
      expect(container.querySelector('[data-testid="lore-toast"]')).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("at the bottom of the page it holds the card after the moved one still instead", async () => {
    const { container } = await renderRows();
    // Five 100px rows in a 300px view: the page scrolls at most 200.
    const scroller = fakeLayout(container, 300, 500);
    scroller.scrollTop = 200;

    fireEvent.click(rowById(container, "A3").querySelector('[data-testid="lore-bump"]')!);

    await waitFor(() => expect(renderedIds(container)).toEqual(["P", "A3", "A1", "A2", "A4"]));
    expect(scroller.scrollTop).toBe(200);
    expect(rowById(container, "A4").getBoundingClientRect().top).toBe(200);
  });

  it("places by state, not by the re-read's effective_ts: an active card lands after the pinned ones, a pinned one on top", async () => {
    vi.spyOn(api, "bumpLoreEntry").mockResolvedValue(undefined);
    // The re-read comes back with an OLD effective_ts; the card still moves.
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => {
      const id = o?.entryIds?.[0];
      if (id === "A4") return page([mkEntry({ id: "A4", effectiveTs: 1 })]);
      if (id === "P2") return page([mkEntry({ id: "P2", state: "pinned", effectiveTs: 1 })]);
      return page([
        mkEntry({ id: "P", state: "pinned", effectiveTs: 10 }),
        mkEntry({ id: "P2", state: "pinned", effectiveTs: 9 }),
        mkEntry({ id: "A1", effectiveTs: 8 }),
        mkEntry({ id: "A4", effectiveTs: 5 }),
      ]);
    });
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(4));

    fireEvent.click(rowById(container, "A4").querySelector('[data-testid="lore-bump"]')!);
    await waitFor(() => expect(renderedIds(container)).toEqual(["P", "P2", "A4", "A1"]));

    fireEvent.click(rowById(container, "P2").querySelector('[data-testid="lore-bump"]')!);
    await waitFor(() => expect(renderedIds(container)).toEqual(["P2", "P", "A4", "A1"]));
  });

  it("a card already at the top of its group stays and shows no toast", async () => {
    const { container, list } = await renderRows();
    fireEvent.click(rowById(container, "A1").querySelector('[data-testid="lore-bump"]')!);
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));
    await new Promise((r) => setTimeout(r, 20));
    expect(renderedIds(container)).toEqual(["P", "A1", "A2", "A3", "A4"]);
    expect(container.querySelector('[data-testid="lore-toast"]')).toBeNull();
  });

  it("leaving and coming back clears the toast and the error line", async () => {
    const { container } = await renderRows();
    fireEvent.click(rowById(container, "A3").querySelector('[data-testid="lore-bump"]')!);
    await waitFor(() => expect(container.querySelector('[data-testid="lore-toast"]')).not.toBeNull());
    vi.mocked(api.bumpLoreEntry).mockRejectedValueOnce(new ApiError("http 403", 403, "forbidden", ""));
    fireEvent.click(rowById(container, "A4").querySelector('[data-testid="lore-bump"]')!);
    await waitFor(() => expect(container.querySelector('[data-testid="lore-action-error"]')).not.toBeNull());

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(container.querySelector('[data-testid="lore-target"]')).not.toBeNull());
    fireEvent.click(container.querySelector('[data-testid="lore-target"]')!);
    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));
    expect(container.querySelector('[data-testid="lore-toast"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-action-error"]')).toBeNull();
  });

  it("a refused bump moves nothing and shows no toast", async () => {
    vi.spyOn(api, "bumpLoreEntry").mockRejectedValue(
      new ApiError("http 403 for POST /api/lore/A3/bump", 403, "forbidden", ""),
    );
    stubList(page(rows()));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(5));
    fireEvent.click(rowById(container, "A3").querySelector('[data-testid="lore-bump"]')!);
    await waitFor(() => expect(container.querySelector('[data-testid="lore-action-error"]')).not.toBeNull());
    expect(renderedIds(container)).toEqual(["P", "A1", "A2", "A3", "A4"]);
    expect(container.querySelector('[data-testid="lore-toast"]')).toBeNull();
  });
});

describe("LorePage — 適用範圍選單", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.resetModules();
    localStorage.clear();
  });

  const MANUALS = [
    { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
  ];

  function openScopeMenu(container: HTMLElement, entryId: string): HTMLElement {
    const row = rowById(container, entryId);
    fireEvent.click(row.querySelector('[data-testid="lore-scope-name"]')!);
    const menu = row.querySelector<HTMLElement>(
      '[data-testid="lore-scope-options"]',
    );
    if (!menu) throw new Error(`no scope menu on ${entryId}`);
    return menu;
  }

  /** Every option as [kind, label, tag, checked, full text]. */
  function readOptions(menu: HTMLElement) {
    return Array.from(
      menu.querySelectorAll<HTMLElement>('[role="menuitemradio"]'),
    ).map((item) => [
      item.getAttribute("data-testid"),
      item.querySelector('[data-testid="lore-scope-option-label"]')!.textContent,
      item.querySelector('[data-testid="lore-scope-option-tag"]')?.textContent ??
        "",
      item.getAttribute("aria-checked"),
      item.textContent,
    ]);
  }

  const taskBound = mkEntry({
    id: "L-1",
    scopeKind: "manual",
    scopeKey: "review-pr",
    taskTypeKey: "review-pr",
    sourceTaskId: "T-5",
    scopeOptions: ["manual", "agent", "everyone"],
  });
  const staffNoTask = mkEntry({
    id: "L-2",
    scopeKind: "agent",
    scopeKey: "mira",
    scopeOptions: ["agent", "everyone"],
  });
  const everyoneNow = mkEntry({
    id: "L-3",
    scopeKind: "everyone",
    scopeKey: "",
    scopeOptions: ["agent", "everyone"],
  });

  async function renderRows(entries: LoreEntryView[]) {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue(MANUALS as never);
    const list = stubList(page(entries));
    const { container } = await renderOpen({ canSetScope: true });
    await waitFor(() =>
      expect(renderedIds(container)).toHaveLength(entries.length),
    );
    await waitFor(() => {
      const badges = Array.from(
        container.querySelectorAll('[data-testid="lore-scope-name"]'),
      ).map((el) => el.textContent ?? "");
      expect(badges.filter((t) => /review-pr|：mira$/.test(t))).toEqual([]);
    });
    return { container, list };
  }

  it("a task-bound entry's menu lists task, author, everyone with the task option checked and default", async () => {
    const { container } = await renderRows([taskBound]);
    expect(
      rowById(container, "L-1").querySelector('[data-testid="lore-scope-name"]')!
        .textContent,
    ).toBe("任務：PR 審查");

    const menu = openScopeMenu(container, "L-1");

    expect(menu.getAttribute("role")).toBe("menu");
    expect(menu.querySelector(".lore-row__scope-pop-title")!.textContent).toBe(
      "適用範圍",
    );
    expect(readOptions(menu)).toEqual([
      [
        "lore-scope-manual",
        "任務：PR 審查",
        "預設",
        "true",
        "任務：PR 審查預設",
      ],
      [
        "lore-scope-agent",
        "建立者：Mira",
        "",
        "false",
        "建立者：Mira",
      ],
      [
        "lore-scope-everyone",
        "所有人",
        "",
        "false",
        "所有人",
      ],
    ]);
    expect(
      Array.from(menu.querySelectorAll(".lore-row__menu-item--active")).map((el) =>
        el.getAttribute("data-testid"),
      ),
    ).toEqual(["lore-scope-manual"]);
    expect(menu.textContent).toBe(
      "適用範圍任務：PR 審查預設建立者：Mira所有人開啟任務手冊 PR 審查",
    );
  });

  it("a staff entry without a task offers author and everyone only", async () => {
    const { container } = await renderRows([staffNoTask]);
    const menu = openScopeMenu(container, "L-2");
    expect(readOptions(menu)).toEqual([
      [
        "lore-scope-agent",
        "建立者：Mira",
        "預設",
        "true",
        "建立者：Mira預設",
      ],
      [
        "lore-scope-everyone",
        "所有人",
        "",
        "false",
        "所有人",
      ],
    ]);
    expect(menu.textContent).toBe("適用範圍建立者：Mira預設所有人");
    expect(menu.querySelector('[data-testid="lore-scope-open-manual"]')).toBeNull();
  });

  it("a current scope that is not the default is tagged 目前 while the default keeps 預設", async () => {
    const { container } = await renderRows([everyoneNow]);
    expect(
      rowById(container, "L-3").querySelector('[data-testid="lore-scope-name"]')!
        .textContent,
    ).toBe("所有人");
    const menu = openScopeMenu(container, "L-3");
    expect(readOptions(menu).map((o) => [o[0], o[2], o[3]])).toEqual([
      ["lore-scope-agent", "預設", "false"],
      ["lore-scope-everyone", "目前", "true"],
    ]);
  });

  it("picking another scope sends it for that entry, closes the menu and updates that card in place without re-asking page one", async () => {
    const set = vi.spyOn(api, "setLoreEntryScope").mockResolvedValue(undefined);
    const { container, list } = await renderRows([taskBound, staffNoTask]);
    list.mockImplementation(async (o) =>
      o?.entryIds
        ? page([{ ...staffNoTask, scopeKind: "everyone", scopeKey: "" }])
        : page([taskBound, staffNoTask]),
    );

    const menu = openScopeMenu(container, "L-2");
    fireEvent.click(menu.querySelector('[data-testid="lore-scope-everyone"]')!);

    await waitFor(() =>
      expect(
        rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!
          .textContent,
      ).toBe("所有人"),
    );
    expect(set.mock.calls).toEqual([["L-2", "everyone"]]);
    expect(list.mock.calls).toEqual([
      [{ states: ["pinned", "active"], scopeKinds: ["agent"], scopeKeys: ["mira"], limit: 30, offset: 0 }],
      [{ scopeKinds: ["agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0 }],
    ]);
    expect(renderedIds(container)).toEqual(["L-1", "L-2"]);
    expect(
      container.querySelector('[data-testid="lore-scope-options"]'),
    ).toBeNull();
  });

  it("picking the current scope closes the menu and leaves the badge as it was", async () => {
    const { container } = await renderRows([staffNoTask]);

    const menu = openScopeMenu(container, "L-2");
    fireEvent.click(menu.querySelector('[data-testid="lore-scope-agent"]')!);

    expect(
      container.querySelector('[data-testid="lore-scope-options"]'),
    ).toBeNull();
    expect(
      rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!
        .textContent,
    ).toBe("建立者：Mira");
  });

  it("a refused switch shows the server's reason and does not reload the list", async () => {
    vi.spyOn(api, "setLoreEntryScope").mockRejectedValue(
      new ApiError(
        "http 400 for POST /api/lore/L-2/scope",
        400,
        codeForStatus(400),
        "lore entry L-2 has no task type to key a manual scope to",
      ),
    );
    const { container, list } = await renderRows([staffNoTask]);
    expect(container.querySelector('[data-testid="lore-action-error"]')).toBeNull();

    fireEvent.click(
      openScopeMenu(container, "L-2").querySelector(
        '[data-testid="lore-scope-everyone"]',
      )!,
    );

    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="lore-action-error"]')?.textContent,
      ).toBe("lore entry L-2 has no task type to key a manual scope to"),
    );
    expect(list).toHaveBeenCalledTimes(1);
    expect(
      rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!
        .textContent,
    ).toBe("建立者：Mira");
  });

  it("a mousedown outside the badge closes the menu; one inside keeps it open", async () => {
    const { container } = await renderRows([staffNoTask]);
    const menu = openScopeMenu(container, "L-2");

    fireEvent.mouseDown(menu);
    expect(
      container.querySelector('[data-testid="lore-scope-options"]'),
    ).not.toBeNull();

    fireEvent.mouseDown(document.body);
    expect(
      container.querySelector('[data-testid="lore-scope-options"]'),
    ).toBeNull();
  });

  it("for the owner a manual badge opens the menu, whose last row opens the manual", async () => {
    const { container } = await renderRows([taskBound]);
    window.location.hash = "";

    const menu = openScopeMenu(container, "L-1");
    expect(window.location.hash).toBe("");
    expect(rowById(container, "L-1").getAttribute("aria-expanded")).toBe("false");

    const jump = menu.querySelector<HTMLElement>(
      '[data-testid="lore-scope-open-manual"]',
    )!;
    expect(jump.textContent).toBe("開啟任務手冊 PR 審查");
    fireEvent.click(jump);
    expect(window.location.hash).toBe("#settings/manuals/review-pr");
    expect(
      container.querySelector('[data-testid="lore-scope-options"]'),
    ).toBeNull();
  });

  function jwt(claims: Record<string, unknown>): string {
    const b64url = (v: unknown) =>
      btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(v)))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
    return `${b64url({ alg: "HS256", typ: "JWT" })}.${b64url(claims)}.sig`;
  }

  async function renderAs(token: string) {
    vi.stubEnv("VITE_USE_MOCK", "false");
    vi.stubEnv("VITE_OC_TOKEN", "");
    localStorage.setItem("oc_token", token);
    vi.resetModules();
    const real = await import("../api");
    const { LorePage: RealLorePage } = await import("./LorePage");
    const { I18nProvider: RealI18nProvider } = await import("../i18n");
    expect(real.USE_MOCK).toBe(false);
    vi.spyOn(real.api, "subscribeEvents").mockReturnValue(() => {});
    vi.spyOn(real.api, "getServerSettings").mockReturnValue(new Promise(() => {}));
    vi.spyOn(real.api, "listThemes").mockReturnValue(new Promise(() => {}));
    vi.spyOn(real.api, "listOutsourceWorkers").mockResolvedValue([]);
    vi.spyOn(real.api, "listTaskManuals").mockResolvedValue([]);
    vi.spyOn(real.api, "listMembers").mockResolvedValue([
      { id: "mira", name: "Mira", role: "assistant", kind: "staff" },
      { id: "m-kyle", name: "Kyle", role: "officraft-developer", kind: "staff" },
    ] as never);
    vi.spyOn(real.api, "listLoreBudgets").mockResolvedValue([MIRA_TARGET]);
    vi.spyOn(real.api, "listLoreEntries").mockResolvedValue(
      page([
        mkEntry({
          id: "L-2",
          scopeKind: "agent",
          scopeKey: "mira",
          scopeOptions: ["agent", "everyone"],
        }),
      ]),
    );
    const { container } = render(
      <RealI18nProvider>
        <RealLorePage />
      </RealI18nProvider>,
    );
    fireEvent.click(
      await waitFor(() => {
        const el = container.querySelector<HTMLElement>('[data-testid="lore-target"]');
        if (!el) throw new Error("no directory target");
        return el;
      }),
    );
    await waitFor(() =>
      expect(
        rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!
          .textContent,
      ).toBe("建立者：Mira"),
    );
    return container;
  }

  it.each([
    ["the owner", { scope: "owner", sub: "owner" }],
    ["the assistant", { scope: "agent", sub: "mira" }],
  ])("%s opens the menu from the badge", async (_label, claims) => {
    const container = await renderAs(jwt(claims));
    await waitFor(() =>
      expect(
        rowById(container, "L-2")
          .querySelector('[data-testid="lore-scope-name"]')!
          .getAttribute("aria-haspopup"),
      ).toBe("menu"),
    );
    fireEvent.click(
      rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!,
    );
    expect(
      container.querySelector('[data-testid="lore-scope-options"]')?.textContent,
    ).toBe("適用範圍建立者：Mira預設所有人");
  });

  it("a plain member sees the current scope as text and clicking it opens nothing", async () => {
    const container = await renderAs(jwt({ scope: "agent", sub: "m-kyle" }));
    const badge = rowById(container, "L-2").querySelector(
      '[data-testid="lore-scope-name"]',
    )!;
    expect(badge.getAttribute("aria-haspopup")).toBeNull();
    fireEvent.click(badge);
    expect(container.querySelector('[data-testid="lore-scope-options"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-scope-agent"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-scope-everyone"]')).toBeNull();
  });
});

// ────────────────────────────────────────────────────────────────────────────
// Sections by boot-document target.

const TARGETS: LoreBudgetTargetView[] = [
  { scopeKind: "everyone", scopeKey: "", capChars: 10000, usedChars: 6240, entryCount: 14, droppedCount: 0 },
  { scopeKind: "agent", scopeKey: "mira", capChars: 10000, usedChars: 9640, entryCount: 11, droppedCount: 4 },
  { scopeKind: "agent", scopeKey: "ow-7d8ad859dd9b", capChars: 10000, usedChars: 7120, entryCount: 6, droppedCount: 0 },
  { scopeKind: "manual", scopeKey: "review-pr", capChars: 10000, usedChars: 3210, entryCount: 8, droppedCount: 0 },
];

const MIRA_SEGMENTS = [
  { scopeKind: "everyone" as const, entryCount: 14, chars: 6240, fittedCount: 14, fittedChars: 6240 },
  { scopeKind: "agent" as const, entryCount: 11, chars: 5380, fittedCount: 7, fittedChars: 3400 },
];

function stubTargets() {
  vi.spyOn(api, "listTaskManuals").mockResolvedValue([
    { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
  ] as never);
  vi.spyOn(api, "listOutsourceWorkers").mockResolvedValue([
    { id: "ow-7d8ad859dd9b", codename: "O-179" },
  ] as never);
  return vi.spyOn(api, "listLoreBudgets").mockResolvedValue(TARGETS);
}

function sectionEl(container: HTMLElement, key: string): HTMLElement {
  const el = container.querySelector<HTMLElement>(`[data-testid="lore-section"][data-section="${key}"]`);
  if (!el) throw new Error(`no section ${key}`);
  return el;
}

/** The directory rows' text, top to bottom. */
function headerTexts(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll('[data-testid="lore-target"]')).map(
    (el) => el.textContent ?? "",
  );
}

/** Click a directory row; the section page replaces the directory. */
function openSection(container: HTMLElement, key: string) {
  const target = container.querySelector<HTMLElement>(`[data-testid="lore-target"][data-section="${key}"]`);
  if (!target) throw new Error(`no directory row ${key}`);
  fireEvent.click(target);
}

function searchFor(container: HTMLElement, testId: string, text: string) {
  const box = container.querySelector<HTMLInputElement>(`[data-testid="${testId}"]`)!;
  fireEvent.change(box, { target: { value: text } });
  fireEvent.keyDown(box, { key: "Enter" });
}

/** Mira's section as the server would answer it: the 上限線 at L-212. */
function miraPage(entries: LoreEntryView[], over: Partial<LoreEntryPageView> = {}): LoreEntryPageView {
  return {
    ...page(entries, { capChars: 10000, firstDroppedId: "L-212" }),
    usedChars: 9640,
    firstDroppedChars: 520,
    budgetSegments: MIRA_SEGMENTS,
    ...over,
  };
}

describe("LorePage — 目錄與段落頁", () => {
  it("lands on the directory: every target with its count, usage and how many cannot be read, and no entries asked for", async () => {
    stubTargets();
    const list = stubList(page([]));
    const { container } = renderPage();

    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    await waitFor(() =>
      expect(headerTexts(container)).toEqual([
        "所有人所有人14 筆6,240 字・每位成員先讀全部讀得到",
        "Mira成員11 筆9,640 / 10,0004 筆讀不到",
        "O-179成員6 筆7,120 / 10,000全部讀得到",
        "PR 審查任務手冊8 筆3,210 / 10,000全部讀得到",
      ]),
    );
    expect(
      Array.from(container.querySelectorAll(".lore__group-title")).map((el) => el.textContent),
    ).toEqual(["所有人", "成員", "任務手冊"]);
    // Each row is a link to its own section page.
    expect(
      Array.from(container.querySelectorAll('[data-testid="lore-target"]')).map((el) => el.getAttribute("href")),
    ).toEqual(["#lore/everyone", "#lore/agent/mira", "#lore/agent/ow-7d8ad859dd9b", "#lore/manual/review-pr"]);
    expect(container.querySelector('[data-testid="lore-section-dropped"]')!.textContent).toBe(
      "4 筆讀不到",
    );
    // Filters belong to a section page.
    expect(container.querySelector('[data-testid="lore-filter"]')).toBeNull();
    expect(renderedIds(container)).toEqual([]);
    expect(list).not.toHaveBeenCalled();
  });

  it("opening a member's section shows the sentence, the 所有人 reference row, the cards and the line above the first entry that cannot be read", async () => {
    stubTargets();
    const list = stubList(
      miraPage([
        mkEntry({ id: "L-198", state: "pinned" }),
        mkEntry({ id: "L-231" }),
        mkEntry({ id: "L-212" }),
        mkEntry({ id: "L-205" }),
      ]),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));

    openSection(container, "agent:mira");

    const body = await waitFor(() => {
      const el = sectionEl(container, "agent:mira").querySelector<HTMLElement>(
        '[data-testid="lore-section-body"]',
      );
      if (!el || renderedIds(el).length === 0) throw new Error("section not loaded");
      return el;
    });
    expect(window.location.hash).toBe("#lore/agent/mira");
    expect(container.querySelector('[data-testid="lore-target"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-back"]')!.getAttribute("href")).toBe("#lore");
    expect(container.querySelector('[data-testid="lore-section-head"]')!.textContent).toBe(
      "Mira成員11 筆9,640 / 10,0004 筆讀不到",
    );
    expect(list.mock.calls).toEqual([
      [{ states: ["pinned", "active"], scopeKinds: ["agent"], scopeKeys: ["mira"], limit: 30, offset: 0 }],
    ]);
    expect(body.querySelector('[data-testid="lore-section-summary"]')!.textContent).toBe(
      "上限 10,000 字，已用 9,640 字：所有人的傳承 14 筆（6,240 字）排在最前面，Mira自己 7 筆（3,400 字）；從 L-212 開始 4 筆讀不到（L-212 需要 520 字，只剩 360 字）。",
    );
    expect(body.querySelector('[data-testid="lore-everyone-ref"]')!.textContent).toBe(
      "所有人的傳承 14 筆排在最前面（在「所有人」那一段調整）6,240 字",
    );
    const sequence = Array.from(
      body.querySelectorAll(
        '[data-testid="lore-section-summary"], [data-testid="lore-everyone-ref"], [data-testid="lore-row"], [data-testid="lore-cap-line"]',
      ),
    ).map((el) => el.getAttribute("data-entry-id") ?? el.getAttribute("data-testid"));
    expect(sequence).toEqual([
      "lore-section-summary",
      "lore-everyone-ref",
      "L-198",
      "L-231",
      "lore-cap-line",
      "L-212",
      "L-205",
    ]);
    expect(
      renderedIds(body).map((id) => [id, rowById(body, id).dataset.dimmed]),
    ).toEqual([
      ["L-198", "false"],
      ["L-231", "false"],
      ["L-212", "true"],
      ["L-205", "true"],
    ]);
    expect(body.querySelector('[data-testid="lore-cap-away"]')).toBeNull();
  });

  it("when the first entry that cannot be read is not among the rows, draws no line and says where the line is; the next page brings the line in", async () => {
    stubTargets();
    const first = Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${100 + i}` }));
    const list = vi
      .spyOn(api, "listLoreEntries")
      .mockImplementation(async (o) =>
        (o?.offset ?? 0) === 0
          ? miraPage(first)
          : miraPage([mkEntry({ id: "L-212" }), mkEntry({ id: "L-213" })], { offset: 30 }),
      );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await waitFor(() => expect(renderedIds(container)).toHaveLength(30));

    expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-cap-away"]')!.textContent).toBe(
      "上限線在 L-212，不在目前的結果裡",
    );
    expect(
      Array.from(container.querySelectorAll('[data-testid="lore-row"]')).filter(
        (el) => (el as HTMLElement).dataset.dimmed === "true",
      ),
    ).toEqual([]);

    fireEvent.scroll(container.querySelector('[data-testid="lore-page"]')!);

    await waitFor(() => expect(renderedIds(container)).toHaveLength(32));
    expect(list.mock.calls[1]).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["agent"], scopeKeys: ["mira"], limit: 30, offset: 30 },
    ]);
    const line = container.querySelector('[data-testid="lore-cap-line"]')!;
    expect(line.compareDocumentPosition(rowById(container, "L-212")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(rowById(container, "L-129").dataset.dimmed).toBe("false");
    expect(rowById(container, "L-212").dataset.dimmed).toBe("true");
    expect(container.querySelector('[data-testid="lore-cap-away"]')).toBeNull();
  });

  it("when 所有人 alone is over the cap, the line sits right under the 所有人 row and every own card is dimmed", async () => {
    stubTargets();
    stubList(
      miraPage([mkEntry({ id: "L-231" }), mkEntry({ id: "L-212" })], {
        usedChars: 9800,
        firstDroppedId: "L-231",
        firstDroppedChars: 300,
        budgetSegments: [
          { scopeKind: "everyone", entryCount: 20, chars: 12400, fittedCount: 14, fittedChars: 9800 },
          { scopeKind: "agent", entryCount: 2, chars: 600, fittedCount: 0, fittedChars: 0 },
        ],
      }),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));

    openSection(container, "agent:mira");

    const body = await waitFor(() => {
      const el = sectionEl(container, "agent:mira").querySelector<HTMLElement>(
        '[data-testid="lore-section-body"]',
      );
      if (!el || renderedIds(el).length === 0) throw new Error("section not loaded");
      return el;
    });
    expect(body.querySelector('[data-testid="lore-section-summary"]')!.textContent).toBe(
      "上限 10,000 字，已用 9,800 字：所有人的傳承就超過上限，20 筆裡只放得下 14 筆（9,800 字）；Mira自己的 2 筆都讀不到。",
    );
    expect(
      Array.from(
        body.querySelectorAll('[data-testid="lore-everyone-ref"], [data-testid="lore-row"], [data-testid="lore-cap-line"]'),
      ).map((el) => el.getAttribute("data-entry-id") ?? el.getAttribute("data-testid")),
    ).toEqual(["lore-everyone-ref", "lore-cap-line", "L-231", "L-212"]);
    expect(renderedIds(body).map((id) => rowById(body, id).dataset.dimmed)).toEqual(["true", "true"]);
  });

  it("a title search whose results leave out the cap-line entry says where the line is and dims nothing", async () => {
    stubTargets();
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.q === "審查" && o.scopeKeys?.[0] === "mira"
        ? miraPage([mkEntry({ id: "L-231" }), mkEntry({ id: "L-205" })])
        : page([]),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await waitFor(() => expect(container.querySelector('[data-testid="lore-filter-id"]')).not.toBeNull());

    searchFor(container, "lore-filter-id", "審查");

    await waitFor(() => expect(renderedIds(container)).toEqual(["L-231", "L-205"]));
    expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull();
    expect(container.querySelector('[data-testid="lore-cap-away"]')!.textContent).toBe(
      "上限線在 L-212，不在目前的結果裡",
    );
    expect(renderedIds(container).map((id) => rowById(container, id).dataset.dimmed)).toEqual([
      "false",
      "false",
    ]);
  });

  it("hides 已失效 entries by default and shows them at the end of the section once 已失效 is ticked", async () => {
    stubTargets();
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      miraPage(
        [
          mkEntry({ id: "L-1", state: "pinned" }),
          mkEntry({ id: "L-2" }),
          mkEntry({ id: "L-9", state: "retired", retireReason: "併進 SOP" }),
        ].filter((e) => o?.states?.includes(e.state) ?? true),
        { firstDroppedId: "", firstDroppedChars: 0 },
      ),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2"]));

    fireEvent.click(container.querySelector<HTMLElement>('[data-testid="lore-filter-state"]')!);
    fireEvent.click(
      container.querySelector<HTMLElement>('[data-testid="lore-filter-state-opt-retired"] input')!,
    );

    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-9"]));
    expect(rowById(container, "L-9").querySelector('[data-testid="lore-retire-reason"]')!.textContent).toBe(
      "併進 SOP",
    );
  });

  it("an id typed on the directory opens that entry's section page with the entry highlighted", async () => {
    stubTargets();
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.scopeKeys === undefined
        ? page([mkEntry({ id: "L-212", scopeKind: "agent", scopeKey: "mira" })])
        : miraPage([mkEntry({ id: "L-198" }), mkEntry({ id: "L-212" })]),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));

    searchFor(container, "lore-directory-id", "212");

    await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira/entry/L-212"));
    expect(list.mock.calls[0][0]).toEqual({
      entryIds: ["L-212"], states: ["pinned", "active", "retired"], limit: 1, offset: 0,
    });
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-198", "L-212"]));
    const row = rowById(container, "L-212");
    expect(row.classList.contains("lore-row--highlight")).toBe(true);
    expect(rowById(container, "L-198").classList.contains("lore-row--highlight")).toBe(false);
    await waitFor(() => expect(document.activeElement).toBe(row));
  });

  it("a highlighted entry beyond the first page is paged in", async () => {
    stubTargets();
    const first = Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${100 + i}` }));
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      (o?.offset ?? 0) === 0 ? miraPage(first) : miraPage([mkEntry({ id: "L-212" })], { offset: 30 }),
    );
    window.location.hash = "#lore/agent/mira/entry/L-212";
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(31));
    expect(list.mock.calls.map((c) => c[0]?.offset)).toEqual([0, 30]);
    expect(rowById(container, "L-212").classList.contains("lore-row--highlight")).toBe(true);
    expect(container.querySelector('[data-testid="lore-highlight-missing"]')).toBeNull();
  });

  it("a retired entry found from the directory opens its section with 已失效 ticked, so it is listed without paging", async () => {
    stubTargets();
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.scopeKeys === undefined
        ? page([mkEntry({ id: "L-9", state: "retired" })])
        : miraPage(
            [mkEntry({ id: "L-198" }), mkEntry({ id: "L-9", state: "retired" })].filter((e) =>
              o?.states?.includes(e.state),
            ),
          ),
    );
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    searchFor(container, "lore-directory-id", "L-9");

    await waitFor(() => expect(renderedIds(container)).toEqual(["L-198", "L-9"]));
    expect(list.mock.calls.slice(1).map((c) => c[0])).toEqual([
      { states: ["pinned", "active", "retired"], scopeKinds: ["agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
    ]);
    expect(rowById(container, "L-9").classList.contains("lore-row--highlight")).toBe(true);
  });

  it("an id whose section is gone says so once and stays on the directory", async () => {
    stubTargets();
    stubList(page([mkEntry({ id: "L-5", scopeKind: "agent", scopeKey: "m-left" })]));
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    searchFor(container, "lore-directory-id", "L-5");
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-directory-note"]')?.textContent).toBe(
        "L-5 所在的段落已經不在了（成員已離開或手冊已刪除）",
      ),
    );
    expect(window.location.hash).toBe("");
  });

  it("paging for a highlight stops at the cap and says the entry is not there", async () => {
    stubTargets();
    let n = 0;
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      miraPage(Array.from({ length: 30 }, () => mkEntry({ id: `L-${1000 + n++}` })), { offset: o?.offset ?? 0 }),
    );
    window.location.hash = "#lore/agent/mira/entry/L-9";
    const { container } = renderPage();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-highlight-missing"]')).not.toBeNull(), {
      timeout: 3000,
    });
    // The first page and ten more, then it gives up.
    expect(list).toHaveBeenCalledTimes(11);
    await new Promise((r) => setTimeout(r, 20));
    expect(list).toHaveBeenCalledTimes(11);
  });

  it("paging for a highlight stops once the reader changes a filter", async () => {
    stubTargets();
    const page2 = held<LoreEntryPageView>();
    let n = 0;
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      (o?.offset ?? 0) === 30
        ? page2.promise
        : miraPage(Array.from({ length: 30 }, () => mkEntry({ id: `L-${1000 + n++}` }))),
    );
    window.location.hash = "#lore/agent/mira/entry/L-9";
    const { container } = renderPage();
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));

    fireEvent.click(container.querySelector<HTMLElement>('[data-testid="lore-filter-state"]')!);
    fireEvent.click(container.querySelector<HTMLElement>('[data-testid="lore-filter-state-opt-retired"] input')!);
    await waitFor(() => expect(list).toHaveBeenCalledTimes(3));
    page2.resolve(miraPage([]));
    await new Promise((r) => setTimeout(r, 30));

    expect(list.mock.calls.map((c) => c[0]?.offset)).toEqual([0, 30, 0]);
  });

  it("a pasted link that gave up does not stop the directory search for the same entry from paging to it", async () => {
    stubTargets();
    const thirty = (from: number) => Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${from + i}` }));
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => {
      if (o?.scopeKeys === undefined) return page([mkEntry({ id: "L-9", state: "retired" })]);
      const withRetired = o.states?.includes("retired");
      if (!withRetired) return miraPage([mkEntry({ id: "L-198" })]);
      return (o.offset ?? 0) === 0 ? miraPage(thirty(100)) : miraPage([mkEntry({ id: "L-9", state: "retired" })]);
    });
    window.location.hash = "#lore/agent/mira/entry/L-9";
    const { container } = renderPage();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-highlight-missing"]')).not.toBeNull());

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    searchFor(container, "lore-directory-id", "L-9");

    await waitFor(() => expect(renderedIds(container)).toHaveLength(31));
    expect(rowById(container, "L-9").classList.contains("lore-row--highlight")).toBe(true);
    expect(container.querySelector('[data-testid="lore-highlight-missing"]')).toBeNull();
  });

  it("a page that fails to load while paging for a highlight stops and says loading failed", async () => {
    stubTargets();
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => {
      if ((o?.offset ?? 0) === 30) throw new Error("network");
      return miraPage(Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${100 + i}` })));
    });
    window.location.hash = "#lore/agent/mira/entry/L-9";
    const { container } = renderPage();
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-highlight-failed"]')?.textContent).toBe(
        "載入更多條目失敗，還沒找到 L-9；往下捲可以再試一次",
      ),
    );
    await new Promise((r) => setTimeout(r, 30));
    expect(list).toHaveBeenCalledTimes(2);
    expect(container.querySelector('[data-testid="lore-highlight-missing"]')).toBeNull();
  });

  it("a highlighted entry the section does not list says so", async () => {
    stubTargets();
    stubList(miraPage([mkEntry({ id: "L-198" })]));
    window.location.hash = "#lore/agent/mira/entry/L-9";
    const { container } = renderPage();
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-highlight-missing"]')?.textContent).toBe(
        "L-9 不在目前的篩選結果裡（已失效的條目要勾「已失效」才會列出）",
      ),
    );
  });

  it("the directory search takes ids only: a title asks nothing and says where title search is; an unknown id says so", async () => {
    stubTargets();
    const list = stubList(page([]));
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));

    searchFor(container, "lore-directory-id", "審查");
    expect(container.querySelector('[data-testid="lore-directory-note"]')!.textContent).toBe(
      "這裡只找條目編號（例如 L-212）；要用標題找，先進到一段裡",
    );
    expect(list).not.toHaveBeenCalled();

    searchFor(container, "lore-directory-id", "L-999");
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-directory-note"]')!.textContent).toBe("找不到 L-999"),
    );
    expect(window.location.hash).toBe("");
  });

  it("the section page's search box sends an id as entryIds and any other text as a title search q, within that section", async () => {
    stubTargets();
    const list = stubList(page([]));
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await waitFor(() => expect(list).toHaveBeenCalled());
    const sentFor = async (typed: string) => {
      list.mockClear();
      searchFor(container, "lore-filter-id", typed);
      await waitFor(() => expect(list).toHaveBeenCalled());
      const { entryIds, q, scopeKeys } = list.mock.calls[0][0]!;
      return { entryIds, q, scopeKeys };
    };

    expect(await sentFor("L-212")).toEqual({ entryIds: ["L-212"], q: undefined, scopeKeys: ["mira"] });
    expect(await sentFor(" l-213 ")).toEqual({ entryIds: ["L-213"], q: undefined, scopeKeys: ["mira"] });
    expect(await sentFor("214")).toEqual({ entryIds: ["L-214"], q: undefined, scopeKeys: ["mira"] });
    expect(await sentFor(" 審查 PR ")).toEqual({ entryIds: undefined, q: "審查 PR", scopeKeys: ["mira"] });
    expect(await sentFor("L-2a")).toEqual({ entryIds: undefined, q: "L-2a", scopeKeys: ["mira"] });
  });

  it("a search nothing matches says so", async () => {
    stubTargets();
    stubList(page([]));
    const { container } = renderPage();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    openSection(container, "agent:mira");
    await waitFor(() => expect(container.querySelector('[data-testid="lore-filter-id"]')).not.toBeNull());

    searchFor(container, "lore-filter-id", "L-999");

    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-empty"]')?.textContent).toBe(
        "沒有符合篩選條件的傳承",
      ),
    );
    expect(renderedIds(container)).toEqual([]);
  });

  it("a section's hash opens its page directly, loads only that section, and the back link returns to the directory with filters reset", async () => {
    stubTargets();
    const list = stubList(page([mkEntry({ id: "L-1", scopeKind: "manual", scopeKey: "review-pr" })]));
    window.location.hash = "#lore/manual/review-pr";
    const { container } = renderPage();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1"]));
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["manual"], scopeKeys: ["review-pr"], limit: 30, offset: 0 },
    ]);
    expect(container.querySelector('[data-testid="lore-section-name"]')!.textContent).toBe("PR 審查");

    searchFor(container, "lore-filter-id", "審查");
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(headerTexts(container)).toHaveLength(4));
    expect(window.location.hash).toBe("#lore");

    openSection(container, "agent:mira");
    await waitFor(() => expect(list).toHaveBeenCalledTimes(3));
    expect(list.mock.calls[2][0]).toEqual({
      states: ["pinned", "active"], scopeKinds: ["agent"], scopeKeys: ["mira"], limit: 30, offset: 0,
    });
    expect(container.querySelector<HTMLInputElement>('[data-testid="lore-filter-id"]')!.value).toBe("");
  });

  it("a section the server does not list says so and offers the way back", async () => {
    stubTargets();
    const list = stubList(page([]));
    window.location.hash = "#lore/agent/nobody";
    const { container } = renderPage();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-section-missing"]')).not.toBeNull());
    expect(container.querySelector('[data-testid="lore-back"]')).not.toBeNull();
    expect(list).not.toHaveBeenCalled();
  });
});

describe("LorePage — 就地編輯", () => {
  const rows = () => [
    mkEntry({ id: "L-1", title: "第一筆", body: "一" }),
    mkEntry({ id: "L-2", title: "AAAA", body: "BBBBBBBBBBBBBBBB" }),
    mkEntry({ id: "L-3", title: "第三筆", body: "三" }),
  ];
  const cap = { capChars: 1000, firstDroppedId: "L-3" };

  it("saving updates that card where it stands, keeps the others in place and re-reads only that entry", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.entryIds
        ? { ...page([mkEntry({ id: "L-2", title: "AAAA", body: "BB" })], cap), usedChars: 976, firstDroppedChars: 0, firstDroppedId: "" }
        : { ...page(rows(), cap), usedChars: 990, firstDroppedChars: 20 },
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]));
    expect(container.querySelector('[data-testid="lore-cap-line"]')).not.toBeNull();

    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    expect(editor.querySelector<HTMLInputElement>('[data-testid="lore-edit-title"]')!.value).toBe("AAAA");
    fireEvent.change(editor.querySelector('[data-testid="lore-edit-body"]')!, { target: { value: "BB" } });

    fireEvent.click(editor.querySelector('[data-testid="lore-edit-save"]')!);

    await waitFor(() =>
      expect(rowById(container, "L-2").querySelector('[data-testid="lore-editor"]')).toBeNull(),
    );
    expect(edit.mock.calls).toEqual([["L-2", { body: "BB" }]]);
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-body"]')!.textContent).toBe("BB");
    expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]);
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
      { scopeKinds: ["agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0 },
    ]);
    await waitFor(() => expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull());
  });

  it("edits title and body only: no type control, no character counters, no effect sentence", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    stubList({ ...page(rows(), cap), usedChars: 990, firstDroppedChars: 4 });
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));

    fireEvent.click(rowById(container, "L-3").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-3").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    expect(
      Array.from(editor.querySelectorAll("input, textarea")).map((el) => el.getAttribute("data-testid")),
    ).toEqual(["lore-edit-title", "lore-edit-body"]);
    expect(editor.querySelector('[role="radiogroup"], [data-testid^="lore-edit-type"]')).toBeNull();
    expect(editor.querySelector('[data-testid$="-count"], [data-testid="lore-edit-effect"]')).toBeNull();
    expect(editor.textContent).not.toMatch(/\d+ \/ \d+/);

    fireEvent.change(editor.querySelector('[data-testid="lore-edit-title"]')!, { target: { value: "新標題" } });
    fireEvent.click(editor.querySelector('[data-testid="lore-edit-save"]')!);
    await waitFor(() => expect(edit).toHaveBeenCalled());
    expect(edit.mock.calls).toEqual([["L-3", { title: "新標題" }]]);
  });

  it("an entry already over a lowered cap saves a shorter or untouched field and refuses one that grows", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    const longBody = "長".repeat(600);
    stubList(page([mkEntry({ id: "L-2", title: "AAAA", body: longBody })], cap));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-2"]));

    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    const save = editor.querySelector<HTMLButtonElement>('[data-testid="lore-edit-save"]')!;
    fireEvent.change(editor.querySelector('[data-testid="lore-edit-title"]')!, { target: { value: " BBBB " } });
    expect(save.disabled).toBe(false);
    expect(editor.querySelector('[data-testid="lore-edit-error"]')).toBeNull();

    fireEvent.change(editor.querySelector('[data-testid="lore-edit-body"]')!, { target: { value: longBody + "長" } });
    expect(save.disabled).toBe(true);
    expect(editor.querySelector('[data-testid="lore-edit-error"]')!.textContent).toBe("超過字數上限");

    fireEvent.change(editor.querySelector('[data-testid="lore-edit-body"]')!, { target: { value: "長".repeat(550) } });
    expect(save.disabled).toBe(false);
    fireEvent.click(save);
    await waitFor(() => expect(edit).toHaveBeenCalled());
    expect(edit.mock.calls).toEqual([["L-2", { title: "BBBB", body: "長".repeat(550) }]]);
  });

  it("a refused save keeps the editor and the draft and says why", async () => {
    vi.spyOn(api, "editLoreEntry").mockRejectedValue(
      new ApiError("http 403 for PATCH /api/lore/L-2", 403, "forbidden", "置頂的傳承只有管理員能改"),
    );
    const list = stubList({ ...page(rows(), cap), usedChars: 990, firstDroppedChars: 20 });
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));

    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    fireEvent.change(editor.querySelector('[data-testid="lore-edit-title"]')!, { target: { value: "新標題" } });
    fireEvent.click(editor.querySelector('[data-testid="lore-edit-save"]')!);

    await waitFor(() =>
      expect(editor.querySelector('[data-testid="lore-edit-error"]')?.textContent).toBe(
        "置頂的傳承只有管理員能改",
      ),
    );
    expect(editor.querySelector<HTMLInputElement>('[data-testid="lore-edit-title"]')!.value).toBe("新標題");
    expect(list).toHaveBeenCalledTimes(1);
  });
});

describe("LorePage — 類型選單", () => {
  const rows = () => [
    mkEntry({ id: "L-1", effectiveTs: 3 }),
    mkEntry({ id: "L-2", loreType: "other", effectiveTs: 2 }),
    mkEntry({ id: "L-3", effectiveTs: 1 }),
  ];

  it("the type tag opens a menu of the five types with the current one marked", async () => {
    stubList(page(rows()));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    const row = rowById(container, "L-2");
    const tag = row.querySelector<HTMLElement>('[data-testid="lore-type"]')!;
    expect(tag.tagName).toBe("BUTTON");
    expect(tag.getAttribute("aria-haspopup")).toBe("menu");
    expect(row.querySelector('[data-testid="lore-type-options"]')).toBeNull();

    fireEvent.click(tag);
    const menu = row.querySelector<HTMLElement>('[data-testid="lore-type-options"]')!;
    expect(menu.getAttribute("role")).toBe("menu");
    const items = Array.from(menu.querySelectorAll<HTMLElement>('[role="menuitem"]'));
    expect(items.map((el) => el.textContent)).toEqual([
      "指示衝突",
      "指示補充",
      "Owner 決策",
      "Owner 偏好",
      "其他",
    ]);
    expect(
      items.filter((el) => el.classList.contains("lore-row__menu-item--active")).map((el) => el.textContent),
    ).toEqual(["其他"]);
    // Opening the menu is not a row toggle.
    expect(row.getAttribute("aria-expanded")).toBe("false");

    fireEvent.mouseDown(document.body);
    expect(row.querySelector('[data-testid="lore-type-options"]')).toBeNull();
  });

  it("choosing a type sends only loreType and re-reads that card where it stands", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.entryIds
        ? page([mkEntry({ id: "L-2", loreType: "owner_decision", effectiveTs: 2 })])
        : page(rows()),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]));
    const before = rowById(container, "L-2");

    fireEvent.click(before.querySelector('[data-testid="lore-type"]')!);
    fireEvent.click(before.querySelector('[data-testid="lore-type-owner_decision"]')!);

    await waitFor(() =>
      expect(rowById(container, "L-2").querySelector('[data-testid="lore-type"]')!.textContent).toBe(
        "Owner 決策",
      ),
    );
    expect(edit.mock.calls).toEqual([["L-2", { loreType: "owner_decision" }]]);
    expect(rowById(container, "L-2")).toBe(before);
    expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]);
    expect(list.mock.calls[1][0]).toEqual({
      scopeKinds: ["agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0,
    });
    expect(before.querySelector('[data-testid="lore-type-options"]')).toBeNull();
  });

  it("choosing the current type sends nothing", async () => {
    const edit = vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    stubList(page(rows()));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    const row = rowById(container, "L-2");
    fireEvent.click(row.querySelector('[data-testid="lore-type"]')!);
    fireEvent.click(row.querySelector('[data-testid="lore-type-other"]')!);
    expect(row.querySelector('[data-testid="lore-type-options"]')).toBeNull();
    expect(edit).not.toHaveBeenCalled();
  });

  it("a refused type change says so on the action error line", async () => {
    vi.spyOn(api, "editLoreEntry").mockRejectedValue(
      new ApiError("http 403 for PATCH /api/lore/L-2", 403, "forbidden", ""),
    );
    stubList(page(rows()));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    const row = rowById(container, "L-2");
    fireEvent.click(row.querySelector('[data-testid="lore-type"]')!);
    fireEvent.click(row.querySelector('[data-testid="lore-type-instruction_conflict"]')!);
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-action-error"]')?.textContent).toBe(
        zh.lore.forbidden,
      ),
    );
    expect(row.querySelector('[data-testid="lore-type"]')!.textContent).toBe("其他");
  });
});

describe("LorePage — 只有放不下一行的內容才能展開", () => {
  const LONG = "一段很長的內容。".repeat(40);

  async function renderTwoBodies() {
    vi.spyOn(loreBodyOverflow, "measure").mockImplementation(
      (el) => (el.textContent ?? "").length > 50,
    );
    stubList(
      page([
        mkEntry({ id: "short", body: "一行就放得下", effectiveTs: 2 }),
        mkEntry({ id: "long", body: LONG, effectiveTs: 1 }),
      ]),
    );
    const r = await renderOpen();
    await waitFor(() => expect(renderedIds(r.container)).toHaveLength(2));
    return r;
  }

  it("a body that fits its line has no chevron, is not a toggle, and a click does not expand it", async () => {
    const { container } = await renderTwoBodies();
    const row = rowById(container, "short");
    expect(row.querySelector('[data-testid="lore-expand-mark"]')).toBeNull();
    expect(row.hasAttribute("role")).toBe(false);
    expect(row.hasAttribute("aria-expanded")).toBe(false);
    // Out of the tab order; focusable only from code (前往, a highlight).
    expect(row.getAttribute("tabindex")).toBe("-1");
    expect(row.classList.contains("lore-row--toggle")).toBe(false);

    fireEvent.click(row.querySelector('[data-testid="lore-body"]')!);
    expect(row.querySelector('[data-testid="lore-body"]')!.classList.contains("lore-row__body--clamped")).toBe(
      true,
    );
    expect(row.querySelector('[data-testid="lore-expand-mark"]')).toBeNull();
  });

  it("a body that overflows shows the chevron and toggles on a row click, both ways", async () => {
    const { container } = await renderTwoBodies();
    const row = rowById(container, "long");
    expect(row.querySelector('[data-testid="lore-expand-mark"]')).not.toBeNull();
    expect(row.getAttribute("role")).toBe("button");
    expect(row.getAttribute("aria-expanded")).toBe("false");
    expect(row.classList.contains("lore-row--toggle")).toBe(true);

    fireEvent.click(row.querySelector('[data-testid="lore-body"]')!);
    expect(row.getAttribute("aria-expanded")).toBe("true");
    expect(row.querySelector('[data-testid="lore-body"]')!.classList.contains("lore-row__body--clamped")).toBe(
      false,
    );
    // Unclamped, the body no longer overflows; the row must still collapse.
    expect(row.querySelector('[data-testid="lore-expand-mark"]')).not.toBeNull();
    fireEvent.click(row.querySelector('[data-testid="lore-body"]')!);
    expect(row.getAttribute("aria-expanded")).toBe("false");
  });

  it("re-measures when the body changes", async () => {
    vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined);
    vi.spyOn(loreBodyOverflow, "measure").mockImplementation(
      (el) => (el.textContent ?? "").length > 50,
    );
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.entryIds ? page([mkEntry({ id: "s", body: LONG })]) : page([mkEntry({ id: "s", body: "短" })]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toEqual(["s"]));
    expect(rowById(container, "s").querySelector('[data-testid="lore-expand-mark"]')).toBeNull();

    fireEvent.click(rowById(container, "s").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "s").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    fireEvent.change(editor.querySelector('[data-testid="lore-edit-body"]')!, { target: { value: LONG } });
    fireEvent.click(editor.querySelector('[data-testid="lore-edit-save"]')!);

    await waitFor(() =>
      expect(rowById(container, "s").querySelector('[data-testid="lore-expand-mark"]')).not.toBeNull(),
    );
  });
});

describe("LorePage — 編輯多筆", () => {
  const rows = () => [
    mkEntry({ id: "L-1", state: "active", loreType: "other", scopeOptions: ["agent", "everyone"] }),
    mkEntry({ id: "L-2", state: "active", loreType: "other", scopeOptions: ["agent", "everyone"] }),
    mkEntry({ id: "L-3", state: "active", loreType: "other", scopeOptions: ["agent", "everyone"] }),
  ];

  function spies() {
    return {
      state: vi.spyOn(api, "setLoreEntryState").mockResolvedValue(undefined),
      edit: vi.spyOn(api, "editLoreEntry").mockResolvedValue(undefined),
      scope: vi.spyOn(api, "setLoreEntryScope").mockResolvedValue(undefined),
    };
  }

  async function renderMode() {
    const list = stubList(page(rows()));
    const r = await renderOpen({ canSetScope: true });
    await waitFor(() => expect(renderedIds(r.container)).toHaveLength(3));
    fireEvent.click(r.container.querySelector('[data-testid="lore-multi-toggle"]')!);
    return { ...r, list };
  }

  function pick(container: HTMLElement, id: string, chip: string, item: string) {
    const row = rowById(container, id);
    fireEvent.click(row.querySelector(`[data-testid="${chip}"]`)!);
    fireEvent.click(row.querySelector(`[data-testid="${item}"]`)!);
  }

  const count = (c: HTMLElement) => c.querySelector('[data-testid="lore-multi-count"]')?.textContent;
  const unsaved = (c: HTMLElement) =>
    renderedIds(c).filter((id) => rowById(c, id).querySelector('[data-testid="lore-unsaved"]') !== null);

  it("the mode hides the checkboxes and the batch bar and shows its own bar", async () => {
    stubList(page(rows()));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    fireEvent.click(rowById(container, "L-1").querySelector('[data-testid="lore-select"]')!);
    expect(container.querySelector('[data-testid="lore-batch-bar"]')).not.toBeNull();

    fireEvent.click(container.querySelector('[data-testid="lore-multi-toggle"]')!);

    expect(container.querySelectorAll('[data-testid="lore-select"]')).toHaveLength(0);
    expect(container.querySelector('[data-testid="lore-batch-bar"]')).toBeNull();
    expect(count(container)).toBe("0 筆未儲存");
    expect(container.querySelector<HTMLButtonElement>('[data-testid="lore-multi-save"]')!.disabled).toBe(true);
    // Leaving the mode brings the checkboxes back, with nothing selected.
    fireEvent.click(container.querySelector('[data-testid="lore-multi-toggle"]')!);
    expect(container.querySelectorAll('[data-testid="lore-select"]')).toHaveLength(3);
    expect(container.querySelector('[data-testid="lore-batch-bar"]')).toBeNull();
  });

  it("state, type and scope chips stage instead of sending, and the card shows the staged value marked 未儲存", async () => {
    const s = spies();
    const { container } = await renderMode();

    pick(container, "L-1", "lore-state", "lore-state-retired");
    pick(container, "L-2", "lore-type", "lore-type-owner_decision");
    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!);
    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-scope-everyone"]')!);

    expect(s.state).not.toHaveBeenCalled();
    expect(s.edit).not.toHaveBeenCalled();
    expect(s.scope).not.toHaveBeenCalled();
    expect(rowById(container, "L-1").querySelector('[data-testid="lore-state"]')!.textContent).toBe(zh.lore.stateRetired);
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-type"]')!.textContent).toBe("Owner 決策");
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!.textContent).toBe("所有人");
    expect(unsaved(container)).toEqual(["L-1", "L-2"]);
    expect(count(container)).toBe("2 筆未儲存");

    // Choosing the stored value again un-stages it.
    pick(container, "L-1", "lore-state", "lore-state-active");
    expect(unsaved(container)).toEqual(["L-2"]);
    expect(count(container)).toBe("1 筆未儲存");
  });

  it("全部儲存 sends every staged change through the per-entry routes, re-reads the section once, and keeps a refused card staged with its reason", async () => {
    const s = spies();
    s.state.mockImplementation(async (id) => {
      if (id === "L-3") throw new ApiError("http 403", 403, "forbidden", "置頂的傳承只有管理員能改");
    });
    const { container, list } = await renderMode();
    const budgets = vi.mocked(api.listLoreBudgets);
    budgets.mockClear();
    list.mockClear();

    pick(container, "L-1", "lore-state", "lore-state-retired");
    pick(container, "L-2", "lore-type", "lore-type-owner_decision");
    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-scope-name"]')!);
    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-scope-everyone"]')!);
    pick(container, "L-3", "lore-state", "lore-state-retired");
    fireEvent.click(container.querySelector('[data-testid="lore-multi-save"]')!);

    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-multi-result"]')?.textContent).toBe(
        "已完成 L-1、L-2；沒有成功：L-3（置頂的傳承只有管理員能改）",
      ),
    );
    expect(s.state.mock.calls).toEqual([["L-1", "retired"], ["L-3", "retired"]]);
    expect(s.edit.mock.calls).toEqual([["L-2", { loreType: "owner_decision" }]]);
    expect(s.scope.mock.calls).toEqual([["L-2", "everyone"]]);
    // One re-read of the section for the cards that changed, one of the numbers.
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { scopeKinds: ["agent"], scopeKeys: ["mira"], entryIds: ["L-1", "L-2"], limit: 2, offset: 0 },
    ]);
    expect(budgets).toHaveBeenCalledTimes(1);
    expect(unsaved(container)).toEqual(["L-3"]);
    expect(count(container)).toBe("1 筆未儲存");
    expect(rowById(container, "L-3").querySelector('[data-testid="lore-state"]')!.textContent).toBe(zh.lore.stateRetired);
  });

  it("取消 discards every staged change and sends nothing", async () => {
    const s = spies();
    const { container } = await renderMode();
    pick(container, "L-1", "lore-state", "lore-state-retired");
    pick(container, "L-2", "lore-type", "lore-type-owner_decision");

    fireEvent.click(container.querySelector('[data-testid="lore-multi-cancel"]')!);

    expect(container.querySelector('[data-testid="lore-multi-bar"]')).toBeNull();
    expect(unsaved(container)).toEqual([]);
    expect(rowById(container, "L-1").querySelector('[data-testid="lore-state"]')!.textContent).toBe(zh.lore.stateActive);
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-type"]')!.textContent).toBe("其他");
    expect(s.state).not.toHaveBeenCalled();
    expect(s.edit).not.toHaveBeenCalled();
  });

  it("提到最新 and 編輯 still apply at once inside the mode", async () => {
    const s = spies();
    const bump = vi.spyOn(api, "bumpLoreEntry").mockResolvedValue(undefined);
    const { container } = await renderMode();
    pick(container, "L-1", "lore-state", "lore-state-retired");

    fireEvent.click(rowById(container, "L-3").querySelector('[data-testid="lore-bump"]')!);
    await waitFor(() => expect(bump).toHaveBeenCalledWith("L-3"));

    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    fireEvent.change(editor.querySelector('[data-testid="lore-edit-title"]')!, { target: { value: "新標題" } });
    fireEvent.click(editor.querySelector('[data-testid="lore-edit-save"]')!);
    await waitFor(() => expect(s.edit).toHaveBeenCalledWith("L-2", { title: "新標題" }));
    // The staged change is still waiting.
    expect(s.state).not.toHaveBeenCalled();
    expect(unsaved(container)).toEqual(["L-1"]);
  });

  it("the filters are locked while the mode is on, with a hint", async () => {
    stubList(page(rows()));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    fireEvent.click(container.querySelector('[data-testid="lore-multi-toggle"]')!);
    for (const id of ["lore-filter-id", "lore-filter-author", "lore-filter-type", "lore-filter-state"]) {
      expect(container.querySelector<HTMLInputElement | HTMLButtonElement>(`[data-testid="${id}"]`)!.disabled).toBe(true);
    }
    expect(container.querySelector('[data-testid="lore-multi-filters-locked"]')!.textContent).toBe(
      "編輯多筆時篩選先鎖住；全部儲存或取消後再改。",
    );
    fireEvent.click(container.querySelector('[data-testid="lore-multi-toggle"]')!);
    expect(container.querySelector<HTMLInputElement>('[data-testid="lore-filter-id"]')!.disabled).toBe(false);
    expect(container.querySelector('[data-testid="lore-multi-filters-locked"]')).toBeNull();
  });

  it("while 全部儲存 runs, the chips and the bar are locked and leaving the section is refused", async () => {
    const s = spies();
    const gate = held<void>();
    s.edit.mockReturnValue(gate.promise);
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const { container } = await renderMode();
    pick(container, "L-1", "lore-type", "lore-type-owner_decision");
    fireEvent.click(container.querySelector('[data-testid="lore-multi-save"]')!);
    await waitFor(() => expect(s.edit).toHaveBeenCalled());

    for (const chip of ["lore-state", "lore-type", "lore-scope-name"]) {
      expect(rowById(container, "L-2").querySelector<HTMLButtonElement>(`[data-testid="${chip}"]`)!.disabled).toBe(true);
    }
    expect(container.querySelector<HTMLButtonElement>('[data-testid="lore-multi-save"]')!.disabled).toBe(true);
    expect(container.querySelector<HTMLButtonElement>('[data-testid="lore-multi-cancel"]')!.disabled).toBe(true);

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    expect(window.location.hash).toBe("#lore/agent/mira");
    expect(confirm).not.toHaveBeenCalled();

    gate.resolve();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-multi-result"]')).not.toBeNull());
    expect(unsaved(container)).toEqual([]);
    expect(rowById(container, "L-2").querySelector<HTMLButtonElement>('[data-testid="lore-state"]')!.disabled).toBe(false);
  });

  it("a browser Back while 全部儲存 runs is refused with a status line, and the page stays put", async () => {
    const s = spies();
    const gate = held<void>();
    s.edit.mockReturnValue(gate.promise);
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const { container } = await renderMode();
    pick(container, "L-1", "lore-type", "lore-type-owner_decision");
    fireEvent.click(container.querySelector('[data-testid="lore-multi-save"]')!);
    await waitFor(() => expect(s.edit).toHaveBeenCalled());

    history.back();
    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-multi-leave-blocked"]')?.textContent).toBe(
        "儲存中，完成後才能離開",
      ),
    );
    await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));
    await new Promise((r) => setTimeout(r, 30));
    expect(window.location.hash).toBe("#lore/agent/mira");
    expect(renderedIds(container)).toHaveLength(3);
    expect(confirm).not.toHaveBeenCalled();

    gate.resolve();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-multi-result"]')).not.toBeNull());
    expect(container.querySelector('[data-testid="lore-multi-leave-blocked"]')).toBeNull();
    // Back works again once the save is done.
    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(headerTexts(container)).toHaveLength(1));
  });

  it("staged changes count as unsaved work for the stale-build reload, and a successful 全部儲存 removes every guard", async () => {
    spies();
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { container } = await renderMode();
    const unload = () => {
      const e = new Event("beforeunload", { cancelable: true });
      window.dispatchEvent(e);
      return e.defaultPrevented;
    };
    expect(hasRegisteredUnsavedWork()).toBe(false);
    pick(container, "L-1", "lore-state", "lore-state-retired");
    expect(hasRegisteredUnsavedWork()).toBe(true);
    expect(unload()).toBe(true);

    fireEvent.click(container.querySelector('[data-testid="lore-multi-save"]')!);
    await waitFor(() => expect(container.querySelector('[data-testid="lore-multi-result"]')).not.toBeNull());

    expect(hasRegisteredUnsavedWork()).toBe(false);
    expect(unload()).toBe(false);
    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(headerTexts(container)).toHaveLength(1));
    expect(confirm).not.toHaveBeenCalled();
  });

  it("with staged changes, leaving the section asks first and closing the page is held", async () => {
    spies();
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { container } = await renderMode();

    const unload = () => {
      const e = new Event("beforeunload", { cancelable: true });
      window.dispatchEvent(e);
      return e.defaultPrevented;
    };
    expect(unload()).toBe(false);
    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    expect(confirm).not.toHaveBeenCalled();
    await waitFor(() => expect(headerTexts(container)).toHaveLength(1));

    openSection(container, "agent:mira");
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    fireEvent.click(container.querySelector('[data-testid="lore-multi-toggle"]')!);
    pick(container, "L-1", "lore-state", "lore-state-retired");

    expect(unload()).toBe(true);
    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(window.location.hash).toBe("#lore/agent/mira");
    expect(unsaved(container)).toEqual(["L-1"]);

    // A browser back / hand-edited hash is put back too.
    window.location.hash = "#tasks";
    await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));
    expect(confirm).toHaveBeenCalledTimes(2);

    confirm.mockReturnValue(true);
    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(headerTexts(container)).toHaveLength(1));
    expect(unload()).toBe(false);
  });
});

describe("LorePage — 多選批次", () => {
  function select(container: HTMLElement, ...ids: string[]) {
    for (const id of ids) {
      fireEvent.click(rowById(container, id).querySelector('[data-testid="lore-select"]')!);
    }
  }

  it("retiring three with one refusal runs them in order with one reason, refreshes once, and names which went through and which did not", async () => {
    const set = vi.spyOn(api, "setLoreEntryState").mockImplementation(async (id) => {
      if (id === "L-2") {
        throw new ApiError("http 403 for POST /api/lore/L-2/state", 403, "forbidden", "只有撰寫人或管理員可以失效");
      }
    });
    const list = vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.entryIds
        ? page([
            mkEntry({ id: "L-1", state: "retired", retireReason: "併進 SOP" }),
            mkEntry({ id: "L-3", state: "retired", retireReason: "併進 SOP" }),
          ])
        : page([mkEntry({ id: "L-1" }), mkEntry({ id: "L-2" }), mkEntry({ id: "L-3" })]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(3));
    const budgets = api.listLoreBudgets as unknown as ReturnType<typeof vi.fn>;
    await waitFor(() => expect(budgets).toHaveBeenCalledTimes(1));

    select(container, "L-1", "L-2", "L-3");
    expect(container.querySelector('[data-testid="lore-batch-count"]')!.textContent).toBe("已選 3 筆");
    fireEvent.click(container.querySelector('[data-testid="lore-batch-retire"]')!);
    fireEvent.change(container.querySelector('[data-testid="lore-batch-reason"]')!, {
      target: { value: "併進 SOP" },
    });
    fireEvent.click(container.querySelector('[data-testid="lore-batch-retire-confirm"]')!);

    await waitFor(() =>
      expect(container.querySelector('[data-testid="lore-batch-result"]')?.textContent).toBe(
        "已完成 L-1、L-3；沒有成功：L-2（只有撰寫人或管理員可以失效）知道了",
      ),
    );
    expect(set.mock.calls).toEqual([
      ["L-1", "retired", "併進 SOP"],
      ["L-2", "retired", "併進 SOP"],
      ["L-3", "retired", "併進 SOP"],
    ]);
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
      { scopeKinds: ["agent"], scopeKeys: ["mira"], entryIds: ["L-1", "L-3"], limit: 2, offset: 0 },
    ]);
    expect(budgets).toHaveBeenCalledTimes(2);
    expect(renderedIds(container)).toEqual(["L-1", "L-2", "L-3"]);
    expect(
      renderedIds(container).map((id) => rowById(container, id).querySelector('[data-testid="lore-state"]')!.textContent),
    ).toEqual(["已失效", "生效中", "已失效"]);
    expect(container.querySelector('[data-testid="lore-batch-count"]')!.textContent).toBe("已選 1 筆");
    expect(
      rowById(container, "L-2").querySelector<HTMLInputElement>('[data-testid="lore-select"]')!.checked,
    ).toBe(true);
  });

  it("offers 置頂 in the batch bar only to a viewer who may pin", async () => {
    stubList(page([mkEntry({ id: "L-1" })]));
    const viewer = await renderOpen({ canSetScope: false });
    await waitFor(() => expect(renderedIds(viewer.container)).toHaveLength(1));
    select(viewer.container, "L-1");
    expect(
      Array.from(viewer.container.querySelectorAll('[data-testid="lore-batch-bar"] button')).map(
        (b) => b.textContent,
      ),
    ).toEqual(["設為生效", "設為失效…", "取消選取"]);
    viewer.unmount();

    const admin = await renderOpen({ canSetScope: true });
    await waitFor(() => expect(renderedIds(admin.container)).toHaveLength(1));
    select(admin.container, "L-1");
    expect(
      Array.from(admin.container.querySelectorAll('[data-testid="lore-batch-bar"] button')).map(
        (b) => b.textContent,
      ),
    ).toEqual(["置頂", "設為生效", "設為失效…", "取消選取"]);
  });
});

describe("LorePage — 手機寬度", () => {
  beforeEach(() => {
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      writable: true,
      value: (query: string) => ({
        matches: /max-width/.test(query),
        media: query,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }),
    });
  });
  afterEach(() => {
    delete (window as unknown as { matchMedia?: unknown }).matchMedia;
  });

  it("folds 提到最新 and 編輯 into the 「⋯」 menu, and 編輯 from there opens the editor", async () => {
    stubList(page([mkEntry({ id: "L-1" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(1));
    const row = rowById(container, "L-1");
    expect(row.querySelector('[data-testid="lore-edit"]')).toBeNull();
    expect(row.querySelector('[data-testid="lore-bump"]')).toBeNull();

    fireEvent.click(row.querySelector('[data-testid="lore-more-actions"]')!);
    expect(
      Array.from(row.querySelectorAll('[data-testid="lore-more-menu"] [role="menuitem"]')).map(
        (el) => el.textContent,
      ),
    ).toEqual(["提到最新", "編輯"]);
    fireEvent.click(row.querySelector('[data-testid="lore-edit"]')!);

    expect(row.querySelector('[data-testid="lore-more-menu"]')).toBeNull();
    expect(row.querySelector('[data-testid="lore-editor"]')).not.toBeNull();
  });
});

describe("LorePage — 版本紀錄", () => {
  async function openMiraWithEditedL2() {
    await api.editLoreEntry("L-2", { title: "改過的標題", body: "改過的內容" });
    const list = vi.spyOn(api, "listLoreEntries");
    const r = renderPage();
    const toggle = await waitFor(() => {
      const el = r.container.querySelector<HTMLElement>(
        '[data-testid="lore-target"][data-section="agent:mira"]',
      );
      if (!el) throw new Error("no section for Mira");
      return el;
    });
    fireEvent.click(toggle);
    await waitFor(() => expect(renderedIds(r.container)).toEqual(["L-1", "L-2"]));
    return { ...r, list };
  }

  it("is reached from the editor, shows the old title and body, compares them with the card, and a restore leaves edit mode with them back on that card in place", async () => {
    const { container, list } = await openMiraWithEditedL2();
    const titleOf = (id: string) =>
      rowById(container, id).querySelector(".lore-row__title")!.textContent;
    expect(titleOf("L-2")).toBe("改過的標題");

    expect(rowById(container, "L-2").querySelector('[data-testid="doc-history-entry-lore_entry"]')).toBeNull();
    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-edit"]')!);
    const editor = rowById(container, "L-2").querySelector<HTMLElement>('[data-testid="lore-editor"]')!;
    expect(
      Array.from(editor.querySelectorAll(".lore-row__editor-buttons button")).map((b) => b.textContent),
    ).toEqual(["版本紀錄", "取消", "儲存"]);
    fireEvent.click(editor.querySelector('[data-testid="doc-history-entry-lore_entry"]')!);
    const rowOpen = await waitFor(() => {
      const el = document.querySelector<HTMLElement>('[data-testid^="doc-history-open-"]');
      if (!el) throw new Error("no retained version listed");
      return el;
    });
    expect(document.querySelectorAll('[data-testid^="doc-history-open-"]')).toHaveLength(1);
    fireEvent.click(rowOpen);

    const modal = await waitFor(() => {
      const el = document.querySelector<HTMLElement>('[data-testid="doc-history-modal"]');
      if (!el || !el.textContent?.includes("零命中的預設解讀是查法寫錯了")) {
        throw new Error("old version not shown yet");
      }
      return el;
    });
    expect(
      Array.from(modal.querySelectorAll(".doc-hist-modal__field-name")).map((h) => h.textContent),
    ).toEqual(["標題", "內容"]);

    fireEvent.click(modal.querySelector('[data-testid="doc-history-pane-diff"]')!);
    expect(modal.querySelector('[data-testid="doc-history-diff-title"]')).not.toBeNull();
    expect(modal.querySelector('[data-testid="doc-history-diff-body"]')).not.toBeNull();

    fireEvent.click(modal.querySelector('[data-testid="doc-history-modal-restore"]')!);
    fireEvent.click(
      await waitFor(() => {
        const el = document.querySelector<HTMLElement>('[data-testid="doc-history-restore-confirm-btn"]');
        if (!el) throw new Error("no confirm");
        return el;
      }),
    );

    await waitFor(() => expect(titleOf("L-2")).toBe("零命中的預設解讀是查法寫錯了"));
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-editor"]')).toBeNull();
    expect(rowById(container, "L-2").querySelector('[data-testid="lore-body"]')!.textContent).toBe(
      "掃描回空的時候先跑一次陽性對照，確認量具本身還會命中，再去解釋那個零。",
    );
    expect(renderedIds(container)).toEqual(["L-1", "L-2"]);
    expect(list.mock.calls.map((c) => c[0])).toEqual([
      { states: ["pinned", "active"], scopeKinds: ["agent"], scopeKeys: ["mira"], limit: 30, offset: 0 },
      { scopeKinds: ["agent"], scopeKeys: ["mira"], entryIds: ["L-2"], limit: 1, offset: 0 },
    ]);
  });

  it("an entry never edited lists no retained version", async () => {
    const { container } = await openMiraWithEditedL2();

    fireEvent.click(rowById(container, "L-1").querySelector('[data-testid="lore-edit"]')!);
    fireEvent.click(
      rowById(container, "L-1").querySelector('[data-testid="doc-history-entry-lore_entry"]')!,
    );

    await waitFor(() =>
      expect(document.querySelector('[data-testid="doc-history-list"]')?.textContent).toContain(
        "還沒有保留任何版本",
      ),
    );
    expect(document.querySelectorAll('[data-testid^="doc-history-open-"]')).toHaveLength(0);
  });
});

function held<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function chooseStateOn(container: HTMLElement, entryId: string, next: LoreEntryView["state"]) {
  const row = rowById(container, entryId);
  fireEvent.click(row.querySelector('[data-testid="lore-state"]')!);
  fireEvent.click(row.querySelector(`[data-testid="lore-state-${next}"]`)!);
}

function search(container: HTMLElement, text: string) {
  const box = container.querySelector<HTMLInputElement>('[data-testid="lore-filter-id"]')!;
  fireEvent.change(box, { target: { value: text } });
  fireEvent.keyDown(box, { key: "Enter" });
}

function sectionHead(container: HTMLElement): string {
  return container.querySelector('[data-testid="lore-section-head"]')?.textContent ?? "";
}

describe("LorePage — 晚到的回應", () => {
  it("a first page answered after a newer search does not replace the newer answer", async () => {
    const slow = held<LoreEntryPageView>();
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.q === "bbb" ? page([mkEntry({ id: "L-2" })]) : slow.promise,
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-filter-id"]')).not.toBeNull());

    search(container, "bbb");
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-2"]));
    slow.resolve(page([mkEntry({ id: "L-1" })]));
    await new Promise((r) => setTimeout(r, 20));

    expect(renderedIds(container)).toEqual(["L-2"]);
  });

  it("leaving a section while its first page is held: the answer lands nowhere", async () => {
    vi.spyOn(api, "listLoreBudgets").mockResolvedValue([MIRA_TARGET]);
    const first = held<LoreEntryPageView>();
    const list = vi.spyOn(api, "listLoreEntries").mockReturnValue(first.promise);
    const { container } = await renderOpen();
    await waitFor(() => expect(list).toHaveBeenCalledTimes(1));

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);
    await waitFor(() => expect(headerTexts(container)).toHaveLength(1));
    first.resolve(page([mkEntry({ id: "L-1" })]));
    await new Promise((r) => setTimeout(r, 20));

    expect(renderedIds(container)).toEqual([]);
    expect(container.textContent).not.toContain(zh.lore.loadingMore);
    expect(list).toHaveBeenCalledTimes(1);
  });

  it("an older refresh answering last does not bring back its cap line or its header numbers", async () => {
    vi.spyOn(api, "setLoreEntryState").mockResolvedValue(undefined);
    const firstRefresh = held<LoreEntryPageView>();
    const firstBudgets = held<LoreBudgetTargetView[]>();
    const rows = [mkEntry({ id: "L-1" }), mkEntry({ id: "L-2" })];
    const lineAtL2 = page(rows, { capChars: 1000, firstDroppedId: "L-2" });
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => {
      if (o?.entryIds?.[0] === "L-1") return firstRefresh.promise;
      if (o?.entryIds?.[0] === "L-2") return page([mkEntry({ id: "L-2", state: "retired" })], { capChars: 1000, firstDroppedId: "" });
      return lineAtL2;
    });
    let budgetCalls = 0;
    vi.spyOn(api, "listLoreBudgets").mockImplementation(async () => {
      budgetCalls += 1;
      if (budgetCalls === 2) return firstBudgets.promise;
      return [{ ...MIRA_TARGET, entryCount: budgetCalls === 1 ? 2 : 5 }];
    });
    const { container } = await renderOpen();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-cap-line"]')).not.toBeNull());

    chooseStateOn(container, "L-1", "retired");
    await waitFor(() => expect(budgetCalls).toBe(2));
    chooseStateOn(container, "L-2", "retired");
    await waitFor(() => expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull());
    await waitFor(() => expect(sectionHead(container)).toContain("5 筆"));

    firstRefresh.resolve(lineAtL2);
    firstBudgets.resolve([{ ...MIRA_TARGET, entryCount: 2 }]);
    await new Promise((r) => setTimeout(r, 20));

    expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull();
    expect(sectionHead(container)).toContain("5 筆");
  });

  it("a next page answering after a refresh adds its rows but keeps the refresh's cap", async () => {
    vi.spyOn(api, "setLoreEntryState").mockResolvedValue(undefined);
    const first = Array.from({ length: 30 }, (_, i) => mkEntry({ id: `L-${i + 1}` }));
    const nextPage = held<LoreEntryPageView>();
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) => {
      if (o?.entryIds) return page([mkEntry({ id: "L-1", state: "retired" })], { capChars: 1000, firstDroppedId: "" });
      if (o?.offset === 30) return nextPage.promise;
      return page(first, { capChars: 1000, firstDroppedId: "L-2" });
    });
    const { container } = await renderOpen();
    await waitFor(() => expect(container.querySelector('[data-testid="lore-cap-line"]')).not.toBeNull());

    fireEvent.scroll(container.querySelector('[data-testid="lore-page"]')!);
    chooseStateOn(container, "L-1", "retired");
    await waitFor(() => expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull());
    nextPage.resolve(page([mkEntry({ id: "L-31" })], { capChars: 1000, firstDroppedId: "L-2" }));

    await waitFor(() => expect(renderedIds(container)).toHaveLength(31));
    expect(container.querySelector('[data-testid="lore-cap-line"]')).toBeNull();
  });
});

describe("LorePage — 選取只留畫面上的卡", () => {
  it("leaving the section drops its cards from the selection, so no batch bar is left", async () => {
    stubList(page([mkEntry({ id: "L-1" }), mkEntry({ id: "L-2" })]));
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    fireEvent.click(rowById(container, "L-1").querySelector('[data-testid="lore-select"]')!);
    expect(container.querySelector('[data-testid="lore-batch-count"]')!.textContent).toBe("已選 1 筆");

    fireEvent.click(container.querySelector('[data-testid="lore-back"]')!);

    await waitFor(() => expect(container.querySelector('[data-testid="lore-batch-bar"]')).toBeNull());
  });

  it("a filter that leaves a selected card off screen drops it, and a batch then reaches only what is shown", async () => {
    const set = vi.spyOn(api, "setLoreEntryState").mockResolvedValue(undefined);
    vi.spyOn(api, "listLoreEntries").mockImplementation(async (o) =>
      o?.q === "二" ? page([mkEntry({ id: "L-2" })]) : page([mkEntry({ id: "L-1" }), mkEntry({ id: "L-2" })]),
    );
    const { container } = await renderOpen();
    await waitFor(() => expect(renderedIds(container)).toHaveLength(2));
    fireEvent.click(rowById(container, "L-1").querySelector('[data-testid="lore-select"]')!);
    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-select"]')!);

    search(container, "二");
    await waitFor(() => expect(renderedIds(container)).toEqual(["L-2"]));
    fireEvent.click(rowById(container, "L-2").querySelector('[data-testid="lore-select"]')!);
    expect(container.querySelector('[data-testid="lore-batch-count"]')!.textContent).toBe("已選 1 筆");
    fireEvent.click(container.querySelector('[data-testid="lore-batch-retire"]')!);
    fireEvent.click(container.querySelector('[data-testid="lore-batch-retire-confirm"]')!);

    await waitFor(() => expect(set).toHaveBeenCalled());
    expect(set.mock.calls.map((c) => c[0])).toEqual(["L-2"]);
  });
});
