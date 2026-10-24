// T-d593 — every unread-badge render site must reach the ONE ring slot.
//
// WHY A SOURCE SCAN, and why in vitest rather than only CT:
//   * The claim "all SEVEN render sites are themed by one token" is a chain with
//     two links: 7 sites → 3 CSS classes, and 3 classes → the ring token. The
//     browser guard (visual-guards/badge-ring-token.ct.spec.tsx) proves the
//     SECOND link by measuring real painted colour. Nothing proved the FIRST —
//     `npm run lint:token-roles` checks the three SELECTORS, never which
//     elements wear them, so an eighth badge with its own class, or one site
//     quietly switched to another class, passes every existing gate.
//   * 🔴 And `npm run test:ct` does not run on EVERY cloud gate. Since T-0fef it
//     runs in the cloud as its own cell (make test-frontend-ct), separate from
//     the cell that runs vitest (make test-frontend-unit). A failing cell stops
//     its own steps, so a CT-only guard is invisible to the unit cell's verdict.
//     This file is the half that runs in the unit cell.
//     ⚠️ The older wording here said test:ct was in NO cloud gate at all; that
//     stopped being true at T-0fef. The reason this file exists did not change —
//     only the size of the gap it covers.
//
// Same shape as styleOwnership.test.ts: read the shipped source, assert a
// structural invariant no runtime test can see.
import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const RING_TOKEN = "--color-danger-badge-ring";

/** The three badge classes, and the file whose rule defines each. */
const BADGE_RULES: Array<{ cls: string; sheet: string }> = [
  { cls: "nav-tab__badge", sheet: "chrome.css" },
  { cls: "office__tab-badge", sheet: "office.css" },
  { cls: "member-card__unread", sheet: "office.css" },
];

// Every render site, verbatim from the audit (T-d593 node 1) — SEVEN of them.
//
// ⚠️ Seven render sites are NOT seven className literals, and an earlier draft of
// this file asserted they were and went red. Sites 4 and 5 are ONE piece of JSX:
// `SidebarTab` in OfficeSidebarTabs.tsx carries a single
// `className="office__tab-badge"` and is INVOKED TWICE (unread={staffUnread} and
// unread={outsourceUnread}). So the two axes are counted separately below:
// literal occurrences per file, and invocations for the shared component.
const RENDER_SITES: Array<{ file: string; cls: string; what: string }> = [
  { file: "../App.tsx", cls: "nav-tab__badge", what: "nav tab — office unread" },
  { file: "../App.tsx", cls: "nav-tab__badge", what: "nav tab — replies waiting" },
  { file: "../App.tsx", cls: "nav-tab__badge", what: "nav tab — open tasks" },
  { file: "./OfficeSidebarTabs.tsx", cls: "office__tab-badge", what: "sidebar tab — staff unread" },
  { file: "./OfficeSidebarTabs.tsx", cls: "office__tab-badge", what: "sidebar tab — outsource unread" },
  { file: "./MemberCard.tsx", cls: "member-card__unread", what: "roster row — member unread" },
  { file: "./OutsourcePanel.tsx", cls: "member-card__unread", what: "roster row — worker unread" },
];

/** How many literal `className="…"` occurrences each file is expected to carry. */
const LITERALS_PER_FILE: Array<{ file: string; cls: string; count: number; note?: string }> = [
  { file: "../App.tsx", cls: "nav-tab__badge", count: 3 },
  {
    file: "./OfficeSidebarTabs.tsx",
    cls: "office__tab-badge",
    count: 1,
    note: "one shared SidebarTab, invoked twice — see the two unreadTestid props",
  },
  { file: "./MemberCard.tsx", cls: "member-card__unread", count: 1 },
  { file: "./OutsourcePanel.tsx", cls: "member-card__unread", count: 1 },
];

const read = (rel: string) =>
  readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

describe("unread badge ring — one themeable slot, all render sites", () => {
  it("every badge CSS rule paints its outline from the ring token", () => {
    for (const { cls, sheet } of BADGE_RULES) {
      const css = read(`./${sheet}`);
      // The rule block for this class, up to its closing brace.
      const at = css.indexOf(`.${cls} {`);
      expect(at, `.${cls} rule must exist in ${sheet}`).toBeGreaterThan(-1);
      const block = css.slice(at, css.indexOf("}", at));
      expect(block, `.${cls} must declare an outline`).toMatch(/outline\s*:/);
      expect(
        block,
        `.${cls} must take its ring from ${RING_TOKEN} — a hardcoded colour or a ` +
          `borrowed token means a theme author repaints some badges and not others`
      ).toContain(`var(${RING_TOKEN})`);
    }
  });

  it("no badge rule still borrows the page background for its ring", () => {
    // The exact pre-T-d593 shape. Named so a revert reads as a revert.
    for (const { cls, sheet } of BADGE_RULES) {
      const css = read(`./${sheet}`);
      const at = css.indexOf(`.${cls} {`);
      const block = css.slice(at, css.indexOf("}", at));
      expect(
        block,
        `.${cls} must not go back to outline: … var(--color-bg) — that is the ` +
          `borrowing this ticket removed (the ring's DEFAULT still follows ` +
          `--color-bg, but via the ring slot's alias in theme.css, not here)`
      ).not.toMatch(/outline[^;]*var\(--color-bg\)/);
    }
  });

  it("all seven render sites wear one of the three badge classes", () => {
    const known = new Set(BADGE_RULES.map((r) => r.cls));
    for (const site of RENDER_SITES) {
      expect(known.has(site.cls), `${site.what}: unknown badge class ${site.cls}`).toBe(true);
      expect(
        read(site.file).includes(`className="${site.cls}"`),
        `${site.what} (${site.file}) must render className="${site.cls}"`
      ).toBe(true);
    }
    expect(RENDER_SITES.length, "the audit found seven render sites").toBe(7);
  });

  it("the literal count per file matches the audit — a badge added or dropped shows up here", () => {
    for (const { file, cls, count, note } of LITERALS_PER_FILE) {
      const actual = read(file).split(`className="${cls}"`).length - 1;
      expect(
        actual,
        `${file} must render className="${cls}" exactly ${count}×` +
          (note ? ` (${note})` : "") +
          ` — a new badge site must be added to this table AND get the ring token`
      ).toBe(count);
    }
  });

  it("the shared sidebar badge really is used twice (sites 4 and 5 are one JSX)", () => {
    // Without this, the single literal above could be one tab's badge while the
    // other silently lost its pill, and the count check would still pass.
    const src = read("./OfficeSidebarTabs.tsx");
    for (const testid of ["staff-tab-unread", "outsource-tab-unread"]) {
      expect(src, `SidebarTab must be invoked with unreadTestid="${testid}"`).toContain(
        `unreadTestid="${testid}"`
      );
    }
    expect(
      src.split("<SidebarTab").length - 1,
      "exactly two SidebarTab invocations (staff + outsource)"
    ).toBe(2);
  });

  it("the ring token is a real theme slot, defaulting to the page colour by ALIAS", () => {
    const theme = read("../styles/theme.css");
    expect(theme, `${RING_TOKEN} must be declared in theme.css (the token SSOT)`).toContain(
      `${RING_TOKEN}:`
    );
    // Alias, not a baked solid. gen-theme-tokens.mjs reads this to decide
    // THEME_ALIAS_DEFAULT_TOKENS, which is what keeps it out of exported
    // bundles while it still follows --color-bg.
    expect(
      theme,
      `${RING_TOKEN} must default to var(--color-bg), not a baked hex — a solid ` +
        `default silently breaks every theme that repaints --color-bg`
    ).toMatch(new RegExp(`${RING_TOKEN}:\\s*var\\(--color-bg\\)`));

    const generated = read("../styles/themeTokens.generated.ts");
    expect(
      generated,
      `${RING_TOKEN} must be in the generated whitelist — run npm run gen:tokens`
    ).toContain(RING_TOKEN);
  });

  it("the ring has an owner-facing label in both languages", () => {
    const meta = read("../lib/themeTokenMeta.ts");
    const at = meta.indexOf(`"${RING_TOKEN}"`);
    expect(at, `${RING_TOKEN} must have a themeTokenMeta entry`).toBeGreaterThan(-1);
    const entry = meta.slice(at, meta.indexOf("},", at));
    expect(entry, "must not fall into the raw-name 'other' group").not.toContain('group: "other"');
    expect(entry, "needs a zh label").toMatch(/zh:\s*"[^"]+"/);
    expect(entry, "needs an en label").toMatch(/en:\s*"[^"]+"/);
    const zh = /zh:\s*"([^"]+)"/.exec(entry)?.[1];
    const en = /en:\s*"([^"]+)"/.exec(entry)?.[1];
    expect(zh && en && zh !== en, "zh must not be copied verbatim into en").toBe(true);
  });
});
