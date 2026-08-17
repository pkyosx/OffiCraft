// T-76cd — STACKING, not geometry. TWO layers, and each one's rank has to be
// asserted separately, because the previous two rounds each fixed one by
// breaking the other.
//
// ① THE POPOVER vs ITS OWN CARD. `.task-artifacts` floats over the task card
//    that owns it. Part of what it covers is z-indexed (the 優先權 / 狀態 menus
//    at z:20) and the rest — the identity rows 負責探員 / 建立者 / 前任 /
//    識別鑰 with their avatars, and the 送出 button — is in flow BELOW it in DOM
//    order, so paint order alone does not put the panel on top. It needs its own
//    `z-index: 40`.
//
// ② THE PREVIEW OVERLAY vs THE APP CHROME. owner, on his iPhone: 「看不到按關閉的
//    按鈕, 被擋住了 且上面的 tab 全部都不能按 為什麼那個預覽畫面好像不是在最上面
//    而是在裡面一層的感覺」. "裡面一層" was literally right: the overlay rendered
//    INSIDE the popover, so `z-index: 40` on the popover scoped the overlay's own
//    `z-index: 1100` to that box.
//
// 🔴 WHY FOUR ROUNDS OF MEASUREMENT MISSED ②. Every earlier probe measured rects,
// computed max-height and scrollHeight/clientHeight. None of them measured PAINT
// ORDER, and the ancestor scan that did run checked transform/filter/
// backdrop-filter/perspective/contain/will-change/container-type — the
// containing-block traps — and never checked `z-index`, `opacity` or `isolation`,
// which are the stacking-context ones.
//
// 🔴 WHY THE FIFTH ROUND (b59c753) MISSED ①. It removed the popover's z-index,
// which freed the overlay and demoted the panel below its own card, and its cost
// table measured four layers OUTSIDE the card (`.tasks__ms-pop`,
// `.task-card__menu-pop`, `.confirm-modal`, `.machine-picker`) — not one element
// inside it. owner's screenshot then showed the identity rows and the 送出 button
// painted over the open panel. Hence CARD_PROBE below, which enumerates rather
// than names: it walks EVERY descendant of the card and asserts on the ones that
// actually overlap the panel, so a card element nobody thought of is covered by
// construction.
//
// The fix keeps both: the overlay portals to `document.body`
// (MarkdownPreviewOverlay.tsx), so NO ancestor can confine it — this z-index, a
// future one, an `opacity`, a `transform` — and the popover gets its 40 back.
// Portalling breaks DOM containment, which the popover's click-outside rule used
// to lean on, so that rule matches `.md-preview` by selector now; test ③ below is
// what says so in a real browser.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { ArtifactsStackingStory } from "./stories/ArtifactsStackingStory";

/** The competitor condition for ②. The shipped chrome is static/`z-index: auto`,
 *  so today the confinement would be LATENT here — 40 happens to win. That is not
 *  a reason to leave the overlay confined: the bug appears the moment anything in
 *  the chrome outranks 40, which is a one-line change anyone can make without
 *  ever touching this file. These tests therefore state the competition
 *  explicitly rather than waiting for someone to introduce it.
 *
 *  ⚠️ This is NOT a claim that owner's build has exactly this rule. His actual
 *  ancestor chain and his chrome's z-index values have never been read — no one
 *  has inspected the running station's DOM. What is guarded is the invariant:
 *  the overlay must win against chrome regardless of the chrome's z-index.
 *
 *  (An earlier revision of this comment argued from his nav labels
 *  「市政廳/待核准/案件/調度」 not appearing anywhere in the repo that he must be
 *  running a different codebase. That inference was WRONG and is recorded here
 *  so it is not made again: `nav.office`/`nav.replies`/`nav.tasks`/`nav.monitor`
 *  are on the theme wording-override whitelist in i18n/messageKeys.generated.ts,
 *  so custom labels are SUPPOSED to be absent from the repo — they live in the
 *  owner's theme pack. The shipped defaults 「辦公室」/「請示」 do appear, in
 *  README.md, docs/design/SPEC.md and conformance/test_*.py. The observation was
 *  true; the conclusion drawn from it was not.) */
const CHROME_OUTRANKS = ".topbar, .nav-tabs { position: relative; z-index: 50; }";

/** 🔴 `.md-preview` PORTALS TO `document.body`, so it is NOT under the mounted
 *  component root — `cmp.locator(".md-preview")` finds nothing. Every reach for
 *  the overlay goes through `page`. (The three tests in the previous revision of
 *  this file used `cmp` and all three fail on this build with "element(s) not
 *  found", which is the portal working, not the overlay missing.) */
const preview = (page: Page) => page.locator(".md-preview");

// eslint-disable-next-line @typescript-eslint/no-explicit-any
async function openPopover(cmp: any) {
  await cmp.getByTestId("task-artifacts-badge").click();
  await expect(cmp.locator(".task-artifacts")).toBeVisible();
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
async function openPreview(cmp: any, page: Page) {
  await openPopover(cmp);
  await cmp.getByRole("button", { name: "Global Context.md" }).click();
  await expect(preview(page)).toBeVisible();
}

/** `page.evaluate` with a STRING body returns `unknown` — there is no source for
 *  tsc to infer from — so the shapes are declared here and applied at the call
 *  sites. Without them every `seen.x` is a TS18046, which nothing in this repo
 *  would have told us: `visual-guards/` is in NO tsconfig (tsconfig.json
 *  includes only `src`, tsconfig.guards.json only `paint-guards`). */
type Probe = {
  overTopbar: string;
  overFirstTab: string;
  overClose: string;
  nearestStackingContextOfOverlay: string;
};

type CardRow = {
  el: string;
  position: string;
  zIndex: string;
  stackingContext: boolean;
  overlapsPanel: boolean;
  probedAt: string;
  hit: string;
  hitInsidePanel: boolean;
};

/** What is actually on top at a point — the hit-test oracle. Paint order and
 *  hit-testing follow the same rules, so this answers "who is painted above". */
const PROBE = `
(() => {
  const at = (x, y) => {
    const el = document.elementFromPoint(x, y);
    if (!el) return "nothing";
    const cls = (typeof el.className === "string" ? el.className : "") || "";
    const name = el.tagName.toLowerCase() + (cls ? "." + cls.split(/\\s+/)[0] : "");
    const where = el.closest(".md-preview__close") ? "CLOSE:"
      : el.closest(".md-preview") ? "OVERLAY:" : "BLOCKED:";
    return where + name;
  };
  const box = (s) => { const e = document.querySelector(s); return e && e.getBoundingClientRect(); };
  const tb = box(".topbar"), nb = box(".nav-tabs"), cb = box(".md-preview__close");
  return {
    overTopbar: tb ? at(tb.left + tb.width / 2, tb.top + tb.height / 2) : "no topbar",
    overFirstTab: nb ? at(nb.left + 40, nb.top + nb.height / 2) : "no tabs",
    overClose: cb ? at(cb.left + cb.width / 2, cb.top + cb.height / 2) : "no close",
    nearestStackingContextOfOverlay: (() => {
      let n = document.querySelector(".md-preview");
      if (!n) return "no overlay";
      for (n = n.parentElement; n; n = n.parentElement) {
        const cs = getComputedStyle(n);
        const pos = cs.position, z = cs.zIndex;
        const sc =
          ((pos === "absolute" || pos === "relative") && z !== "auto") ||
          pos === "fixed" || pos === "sticky" ||
          cs.opacity !== "1" || cs.isolation === "isolate" ||
          cs.transform !== "none" || cs.filter !== "none" ||
          n === document.documentElement;
        if (sc) {
          const cls = (typeof n.className === "string" ? n.className : "") || "";
          return n.tagName.toLowerCase() + (cls ? "." + cls.split(/\\s+/)[0] : "") + " z=" + z;
        }
      }
      return "none";
    })(),
  };
})()
`;

/** DoD ① — THE ENUMERATION, and the query method is the point of it.
 *
 *  It does NOT name the elements it expects to find. It walks
 *  `card.querySelectorAll("*")` — every descendant of `.task-card`, in document
 *  order — drops the ones inside `.task-artifacts` itself and the ones with a
 *  zero-area box, and for each survivor computes the intersection of its border
 *  box with the panel's. Anything with a ≥1px×1px overlap is a candidate to be
 *  painted over the panel, and gets hit-tested at the CENTRE OF THAT OVERLAP —
 *  a point that is inside the panel's rect by construction, so the panel covers
 *  it geometrically and the only question left is who wins the paint order.
 *  Non-overlapping elements are still reported (with a hit test at their own
 *  centre) so the inventory is the full positioned/z-indexed census the previous
 *  round's cost table was missing, not just the subset that happens to collide.
 *
 *  So the assertion below is "every card descendant that overlaps the panel
 *  hit-tests to something inside the panel". An element nobody thought of is
 *  covered because nothing here is a name — including elements a future card
 *  layout adds. */
const CARD_PROBE = `
(() => {
  const desc = (el) => {
    if (!el) return "nothing";
    const cls = (typeof el.className === "string" ? el.className : "") || "";
    return el.tagName.toLowerCase() + (cls ? "." + cls.trim().split(/\\s+/)[0] : "");
  };
  const panel = document.querySelector(".task-artifacts");
  const card = document.querySelector(".task-card");
  if (!panel || !card) return { error: "no panel or no card" };
  const pr = panel.getBoundingClientRect();
  const rows = [];
  for (const el of Array.from(card.querySelectorAll("*"))) {
    if (el === panel || panel.contains(el)) continue;
    const r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) continue;
    const cs = getComputedStyle(el);
    if (cs.visibility === "hidden" || cs.display === "none") continue;
    const x0 = Math.max(r.left, pr.left), x1 = Math.min(r.right, pr.right);
    const y0 = Math.max(r.top, pr.top), y1 = Math.min(r.bottom, pr.bottom);
    const overlaps = x1 - x0 >= 1 && y1 - y0 >= 1;
    const px = overlaps ? (x0 + x1) / 2 : r.left + r.width / 2;
    const py = overlaps ? (y0 + y1) / 2 : r.top + r.height / 2;
    const hit = document.elementFromPoint(px, py);
    const stacking =
      ((cs.position === "absolute" || cs.position === "relative") && cs.zIndex !== "auto") ||
      cs.position === "fixed" || cs.position === "sticky" ||
      cs.opacity !== "1" || cs.isolation === "isolate" ||
      cs.transform !== "none" || cs.filter !== "none" || cs.willChange !== "auto";
    rows.push({
      el: desc(el),
      position: cs.position,
      zIndex: cs.zIndex,
      stackingContext: stacking,
      overlapsPanel: overlaps,
      probedAt: Math.round(px) + "," + Math.round(py),
      hit: desc(hit),
      hitInsidePanel: !!(hit && (hit === panel || panel.contains(hit))),
    });
  }
  return rows;
})()
`;

// DoD ① — the popover must outrank EVERY element of its own card.
//
// MUTANT (measured, `npx playwright test -c playwright-ct.config.ts
// visual-guards/artifacts-stacking.ct.spec.tsx`): delete the `z-index: 40`
// declaration from `.task-artifacts` in src/components/tasks.css (the b59c753
// state) ⇒ THIS test fails and the other four pass. The failure names the
// offenders one by one — the identity rows, their avatars and the 送出 button —
// which is owner's screenshot in list form. The numbers are in the report.
test("with the artifacts panel open, nothing in its own task card paints above it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<ArtifactsStackingStory />);
  // The card is EXPANDED first (the whole card is the toggle surface — there is
  // no chevron button; T-70fb). Collapsed, the description and workflow blocks
  // are not in the tree at all, so the probe would only ever see the head. The
  // identity rows and the 送出 composer are in the head and show either way; the
  // expansion is what puts everything else in front of it too.
  await cmp.locator(".task-card__title").click();
  await expect(cmp.locator(".task-card__meta")).toBeVisible();
  await expect(cmp.getByTestId("task-msg-send")).toBeVisible();
  await openPopover(cmp);

  const rows = (await page.evaluate(CARD_PROBE)) as CardRow[];

  // A probe that found nothing to compare would pass vacuously — and vacuous is
  // exactly what the previous round's cost table was. Measured on this build:
  // 65 card descendants with a non-zero box, 46 of them overlapping the panel,
  // 10 of them positioned or establishing a stacking context. The floors are set
  // below those so a layout change does not red them, but well above zero so a
  // story that stops rendering the card, stops expanding it, or stops opening
  // the panel fails here instead of passing silently.
  expect(Array.isArray(rows)).toBe(true);
  expect(rows.length).toBeGreaterThan(40);
  const overlapping = rows.filter((r) => r.overlapsPanel);
  expect(overlapping.length).toBeGreaterThan(30);
  // The two the regression screenshot named must be among what was compared —
  // the panel has to be tall enough to actually reach them.
  expect(overlapping.map((r) => r.el)).toContain("button.task-card__send");
  expect(overlapping.map((r) => r.el)).toContain("span.avatar");

  const above = overlapping.filter((r) => !r.hitInsidePanel);
  expect(
    above,
    "card elements painted ABOVE the open artifacts panel: " + JSON.stringify(above, null, 2),
  ).toEqual([]);
});

// DoD ② — the owner-reported symptom must not come back. Real chrome in the
// tree (`.topbar` / `.nav-tabs`), forced to outrank the popover.
//
// MUTANTS (measured, same command):
//   · overlay rendered in place instead of portalled, with `z-index: 40` on the
//     popover — the BEFORE state — ⇒ this test and the next one fail, and the
//     failure message is owner's report verbatim: overTopbar
//     "BLOCKED:header.topbar" (this test) and nearest stacking context
//     "div.task-artifacts z=40" (the next one).
//   · `z-index: 1200` on the popover, overlay in place ⇒ this test goes green
//     and the next one still reddens with "div.task-artifacts z=1200". That pair
//     is the whole reason the next test exists: outranking the current
//     competitor is not the same as not being confined, and only the second one
//     can tell them apart.
test("narrow 390: with chrome outranking the popover, the preview overlay still wins", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<ArtifactsStackingStory />);
  await page.addStyleTag({ content: CHROME_OUTRANKS });
  await openPreview(cmp, page);

  const seen = (await page.evaluate(PROBE)) as Probe;

  // The overlay is on top where the chrome is — the tabs owner could not use.
  expect(seen.overTopbar).toContain("OVERLAY:");
  expect(seen.overFirstTab).toContain("OVERLAY:");
  // …and the close button is reachable, which is the affordance he lost.
  // (The hit lands on the icon's <path>, which is INSIDE the button — hence
  // containment, not element identity. Asserting the exact tag here made this
  // red on a working build.)
  expect(seen.overClose).toContain("CLOSE:");
  await preview(page).locator(".md-preview__close").click();
  await expect(preview(page)).toHaveCount(0);
});

// DoD ② (structural half) — the overlay must not be CONFINED at all, which is
// strictly stronger than "it beats today's chrome". A z-index of 1200 on the
// popover passes the test above and fails this one.
test("the preview overlay participates in the root stacking context, not the popover's", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<ArtifactsStackingStory />);
  await openPreview(cmp, page);

  const seen = (await page.evaluate(PROBE)) as Probe;
  // `.md-preview` is itself `position: fixed`, so the nearest stacking context
  // ABOVE it is what decides where its 1100 is resolved. Anything other than
  // the root means the overlay is scoped inside a box again.
  expect(seen.nearestStackingContextOfOverlay).toContain("html");
});

// DoD ③ — the owner ruling this fix must not break (2026-07-20: 「點其他地方都
// 不會自動關閉,一定要點 X」). The overlay is no longer inside the anchor, so
// this is no longer free: the popover's click-outside rule has to recognise
// `.md-preview` explicitly, and this is the real-browser proof that it does —
// a real mousedown at real coordinates on the grey backdrop.
//
// MUTANT (measured): drop the `closest(".md-preview")` arm from
// TaskArtifactsPopover's `onDown` ⇒ this test fails on the last assertion
// (the panel is gone), while everything else stays green.
test("clicking the preview backdrop closes nothing — the popover survives (owner 2026-07-20)", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<ArtifactsStackingStory />);
  await openPreview(cmp, page);

  // A point on the backdrop: beside the panel, well clear of it.
  const panel = (await preview(page).locator(".md-preview__panel").boundingBox())!;
  await page.mouse.click(Math.max(3, panel.x / 2), panel.y + panel.height / 2);

  // The preview closes (backdrop dismissal is its own contract)…
  await expect(preview(page)).toHaveCount(0);
  // …and the popover behind it does NOT. This is the ruling.
  await expect(cmp.locator(".task-artifacts")).toBeVisible();
});

// DoD ③ (Esc half) — with both layers open, one Esc closes the INNER one only,
// and the second closes the popover. Portalling moved the overlay out of the
// popover's subtree, so `escapeLayers`' containment rule can no longer see the
// nesting and falls through to its registration-order tie-break. That tie-break
// gives the right answer here (the preview is opened by a later interaction, so
// it registers later) — but "gives the right answer" is exactly the kind of
// thing that must be measured rather than reasoned about, since the reasoning
// is what changed.
test("Esc closes the preview first and the popover second, still in that order", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<ArtifactsStackingStory />);
  await openPreview(cmp, page);

  await page.keyboard.press("Escape");
  await expect(preview(page)).toHaveCount(0);
  await expect(cmp.locator(".task-artifacts")).toBeVisible();

  await page.keyboard.press("Escape");
  await expect(cmp.locator(".task-artifacts")).toHaveCount(0);
});
