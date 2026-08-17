// HOTSPOT — 手機上市政廳名冊比視窗寬，未讀圓標被切一半 (T-959d).
//
// Bug (owner, phone screenshot 2026-08-15): on 市政廳 › 外勤支援 the member list
// was wider than the window and the page could be dragged sideways; the unread
// pills on two rows were sliced in half at the right edge.
//
// 🔴 THE PILL IS NOT THE CAUSE. Measured at 390px with the real ancestor chain,
// every layer BELOW `.office` overflows by 0 — `.office__members`,
// `.outsource-panel__list`, `.outsource-row`, `.member-card`, the pill itself.
// The first layer wider than the window is `.office` and its
// `grid-template-columns: 1fr` under the 720px media query: `1fr` is
// `minmax(auto, 1fr)`, and that `auto` floor is the grid item's MIN-CONTENT.
// Content that refuses to shrink (the `T-xxxx` chip is `flex: none`, the type
// label is `nowrap`, `.member-card__name` declared no wrapping at all) pushes
// that floor past the viewport, the whole row widens with the track, and the
// unread pill — `flex: none` at the row's flex end — is what gets pushed out.
// Rows WITHOUT a pill widen exactly the same; they just have nothing at the
// right edge, which is why the screenshot made the pill look guilty.
//
// The fix is therefore at the track (`minmax(0, 1fr)` lets it shrink) PLUS the
// two contents that could then overflow the shrunk track instead: the rail
// row's type label and the roster name. Hiding or clipping the pill is
// explicitly not a fix (owner's AC) — the pill's own rect is asserted whole.
//
// NEGATIVE CONTROL (recon, measured): re-injecting the T-4aa0 change did not
// move any number here (doc 30→30, `.office` 52→52) and this page renders zero
// `.task-card` nodes, so this is not that regression.
//
// FIXTURE DATA IS REAL. `.outsource-row__type` renders task-manual display
// names, and the live studio carries names like 「OffiCraft · PR 審查（含外部 PR
// 接待）」 — past the ~16-CJK-character threshold at 390px, i.e. the owner walks
// this path daily. Short invented labels go green while the page is broken.
//
// MUTANTS (measured, not assumed):
//   · `grid-template-columns: 1fr` back on `.office` → 390/320 外包 red on
//     assertion (1): the page drags +31 / +101px.
//   · `overflow-wrap: anywhere` off `.member-card__name` → 390/320 正職 red on
//     assertion (5): the name paints to 377px over a pill at 322 / 252px, while
//     every page- and `.office`-level number stays 0 (the list is a scroll
//     container and absorbs it).
//   · ⚠️ The third change in this package — `.outsource-row__type` from
//     inline-flex to block, so `text-overflow: ellipsis` actually applies —
//     has NO measuring guard: reverting it leaves all five cases GREEN. Its
//     effect (text ending in 「…」 instead of being cut mid-character) is not a
//     geometry, and the only evidence for it is the before/after screenshots
//     pinned on the task. Do not read this file as covering it.
//
// jsdom has no layout engine, so this is a CT guard in real Chromium. 390 (the
// owner's phone) and 320 (the narrowest we support) are both asserted — width
// is an INPUT dimension.
import { test, expect } from "@playwright/experimental-ct-react";
import {
  OfficeRosterOverflowStory,
  OfficeRosterDesktopStory,
} from "./stories/OfficeRosterOverflowStory";

type Tab = "staff" | "outsource";

async function measure(page: any) {
  return await page.evaluate(() => {
    const q = (s: string) => document.querySelector(s) as HTMLElement | null;
    const doc = document.scrollingElement!;
    const office = q(".office");
    const over = (el: HTMLElement | null) => (el ? el.scrollWidth - el.clientWidth : -2);
    // Both rails render the SAME pill class at the row's flex end (the rail row
    // reuses `.member-card__unread` on purpose — one recipe, one slot).
    const pills = Array.from(document.querySelectorAll(".member-card__unread")).map((el) => {
      const r = (el as HTMLElement).getBoundingClientRect();
      return { left: r.left, right: r.right, width: r.width };
    });
    // Every row's own label box, paired with the pill that shares its row: the
    // page can measure zero while a name runs straight OVER the pill, because
    // the list around it is a scroll container that absorbs the overflow.
    const rows = Array.from(document.querySelectorAll(".member-card, .outsource-row")).map(
      (row) => {
        const label = row.querySelector(
          ".member-card__name, .outsource-row__type"
        ) as HTMLElement | null;
        const pill = row.querySelector(".member-card__unread") as HTMLElement | null;
        const r = (el: HTMLElement | null) => (el ? el.getBoundingClientRect().right : null);
        return { labelRight: r(label), pillLeft: pill ? pill.getBoundingClientRect().left : null };
      }
    );
    return {
      rows,
      page: over(doc as HTMLElement),
      office: over(office),
      officeWidth: office ? Math.round(office.getBoundingClientRect().width) : -2,
      viewport: window.innerWidth,
      pills,
    };
  });
}

async function assertFits(page: any, width: number, tab: Tab) {
  const m = await measure(page);
  const at = `[${width}px ${tab}]`;

  // (1) CORE — the surface the phone actually drags.
  expect(m.page, `${at} page scrolls sideways by +${m.page}px`).toBeLessThanOrEqual(1);

  // (2) …and the layer the recon named, so a fix that merely CLIPS the page
  // scroll cannot turn (1) green while the grid track is still oversized.
  expect(m.office, `${at} .office never rendered`).not.toBe(-2);
  expect(
    m.office,
    `${at} .office overflows by +${m.office}px — the grid track is still sized by min-content`
  ).toBeLessThanOrEqual(1);
  expect(
    m.officeWidth,
    `${at} .office is ${m.officeWidth}px inside a ${m.viewport}px viewport`
  ).toBeLessThanOrEqual(m.viewport);

  // (3) NON-VACUITY — the pills must exist, or this file measures nothing.
  expect(m.pills.length, `${at} no unread pill rendered; the fixture stopped covering it`).toBeGreaterThan(0);

  // (4) The owner's actual symptom: every pill whole and inside the window.
  // Hiding or clipping it is not a fix, so its own width is asserted too.
  for (const [i, p] of m.pills.entries()) {
    expect(
      p.width,
      `${at} unread pill #${i} is only ${Math.round(p.width)}px wide — it was shrunk or hidden, not fixed`
    ).toBeGreaterThanOrEqual(18);
    expect(
      p.left,
      `${at} unread pill #${i} starts off-screen at ${Math.round(p.left)}px`
    ).toBeGreaterThanOrEqual(-0.5);
    expect(
      p.right,
      `${at} unread pill #${i} is cut at the right edge: it ends at ${Math.round(p.right)}px ` +
        `in a ${m.viewport}px viewport`
    ).toBeLessThanOrEqual(m.viewport + 0.5);
  }

  // (5) …and "whole" means READABLE, not merely inside the window. A label that
  // refuses to wrap paints straight over the pill while every measurement above
  // stays 0, because the list around it is a scroll container that absorbs it.
  // This is the assertion the 正職 half fails without `overflow-wrap: anywhere`.
  const paired = m.rows.filter((r: any) => r.labelRight !== null && r.pillLeft !== null);
  expect(paired.length, `${at} no row rendered a label and a pill together`).toBeGreaterThan(0);
  for (const [i, r] of paired.entries()) {
    expect(
      r.labelRight,
      `${at} row #${i}'s label runs to ${Math.round(r.labelRight)}px, over its unread pill ` +
        `at ${Math.round(r.pillLeft)}px`
    ).toBeLessThanOrEqual(r.pillLeft + 0.5);
  }
}

for (const width of [390, 320]) {
  for (const tab of ["outsource", "staff"] as Tab[]) {
    test(`${width}px ${tab}: the roster never widens past the window`, async ({ mount, page }) => {
      await page.setViewportSize({ width, height: 900 });
      await mount(<OfficeRosterOverflowStory tab={tab} />);
      await assertFits(page, width, tab);
    });
  }
}

// The shared `.office` grid also lays out the DESKTOP two-column page. The fix
// touches only the phone media query, and this case is what would catch it
// leaking: the rail must keep its fixed 264px column and the chat pane the rest.
test("1280px: the desktop two-column office is unchanged", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mount(<OfficeRosterDesktopStory />);
  const m = await page.evaluate(() => {
    const q = (s: string) => document.querySelector(s) as HTMLElement | null;
    const office = q(".office");
    const rail = q(".office__members");
    const chat = q(".office__chat");
    return {
      cols: office ? getComputedStyle(office).gridTemplateColumns : "",
      railWidth: rail ? Math.round(rail.getBoundingClientRect().width) : -2,
      chatWidth: chat ? Math.round(chat.getBoundingClientRect().width) : -2,
      officeOver: office ? office.scrollWidth - office.clientWidth : -2,
      pageOver: document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth,
    };
  });
  expect(m.railWidth, "the desktop rail lost its fixed 264px column").toBe(264);
  expect(m.chatWidth, "the desktop chat pane never rendered / collapsed").toBeGreaterThan(400);
  expect(m.officeOver, `desktop .office overflows by +${m.officeOver}px`).toBeLessThanOrEqual(1);
  expect(m.pageOver, `desktop page scrolls sideways by +${m.pageOver}px`).toBeLessThanOrEqual(1);
});
