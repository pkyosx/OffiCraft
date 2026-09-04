// HOTSPOT — the version reader's purely-VISUAL contracts (T-60). The vitest
// suite pins the model (which pane, which version, which side); none of the
// facts below survive jsdom, which applies no layout and evaluates no @media.
//
//   ① IT MUST ACTUALLY OVERLAY the task page. jsdom has no stacking context and
//      no hit testing, so a panel painted under the page — or a scrim the page
//      still receives clicks through — is invisible there.
//   ② WIDE — the version list and the content pane sit SIDE BY SIDE, and a
//      60-line version scrolls inside `.ta-versions__body` with the PAGE left
//      unscrollable in both directions (the long-token rule, T-d451).
//   ④ IT FOLLOWS THE THEME. Every colour on this surface is a token, so a
//      shipped light pack must actually move them. `lint:tokens` proves no raw
//      literal is WRITTEN; only a browser proves the written token is the one
//      that reaches the pixel.
//   ③ NARROW (360) — the same two panes STACK, the panel stays inside the
//      viewport, and the row's 「N版」 entry keeps its text instead of being
//      clipped into the 24px icon square its siblings are.
//
// MUTANTS (each RUN and verified red, one assertion each):
//   drop BOTH overflow rules from .ta-versions__body        → ② red (body scrolls nowhere)
//     ⚠️ Dropping `overflow-y: auto` ALONE stays green, and that is the CSS
//     being honest rather than the guard being weak: with `overflow-x: hidden`
//     still declared, the visible axis computes to `auto` on its own. The
//     declaration is explicitness, not the mechanism — measured, not reasoned.
//   drop `flex-direction: column` from the 720 query        → ③ red (panes stay side by side)
//   drop `width: auto` from .task-artifacts__versions       → ③ red (entry clipped to 24px)
//   hard-code .ta-versions__panel's background              → ④ red (it stops moving)
//   `position: static` on .ta-versions                      → ① red (page shows through)
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskArtifactVersionsStory } from "./stories/TaskArtifactVersionsStory";
import { LIGHT_PACK } from "./stories/ThemeContrastStory";

test("the reader covers the task page behind it, and the page cannot scroll", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskArtifactVersionsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  await cmp.getByTestId("task-artifact-versions-ta-file").click();

  const panel = page.locator(".ta-versions__panel");
  await expect(panel).toBeVisible();
  const box = (await panel.boundingBox())!;
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(1024 + 1);
  expect(box.y).toBeGreaterThanOrEqual(0);
  expect(box.y + box.height).toBeLessThanOrEqual(800 + 1);

  // ① HIT TESTING: the panel is topmost at its own centre, and the page corner
  // belongs to the SCRIM — not to the task page, which must not be clickable.
  const atPanel = await page.evaluate(
    ({ x, y }) =>
      (document.elementFromPoint(x, y) as HTMLElement)?.closest(".ta-versions__panel") !==
      null,
    { x: box.x + box.width / 2, y: box.y + box.height / 2 },
  );
  expect(atPanel, "the panel must be topmost at its own centre").toBe(true);
  const overPage = await page.evaluate(() => {
    const el = document.elementFromPoint(8, 8) as HTMLElement | null;
    return {
      onScrim: el?.classList.contains("ta-versions") ?? false,
      onPage: el?.closest('[data-testid="page-behind"]') !== null,
    };
  });
  expect(overPage.onScrim, "the scrim must cover the page corner").toBe(true);
  expect(overPage.onPage, "the page behind must not be reachable").toBe(false);
});

test("wide: the list sits beside the content, and a long version scrolls inside the reader", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskArtifactVersionsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  await cmp.getByTestId("task-artifact-versions-ta-file").click();
  await expect(page.getByTestId("ta-versions-content-text")).toBeVisible();

  const list = (await page.locator(".ta-versions__list").boundingBox())!;
  const body = (await page.locator(".ta-versions__body").boundingBox())!;
  expect(
    body.x,
    `the content pane must start after the list (list ends at ${list.x + list.width})`,
  ).toBeGreaterThanOrEqual(list.x + list.width - 1);

  // ② The reader's BODY is the SCROLLER for a 60-line version — asked by
  // actually scrolling it, not by comparing scrollHeight to clientHeight
  // (`overflow: visible` content reports the same difference while scrolling
  // nowhere, which is exactly the mutant this has to catch).
  const bodyScrolled = await page.evaluate(() => {
    const el = document.querySelector(".ta-versions__body")! as HTMLElement;
    el.scrollTop = 9999;
    return el.scrollTop;
  });
  expect(bodyScrolled, "the reader body must be the vertical scroller").toBeGreaterThan(0);

  // …and the page is not handed a SIDEWAYS scrollbar, 300-char token included.
  // (Its vertical one is the story's own 1200px page behind the scrim, which
  // exists precisely so there is something to cover — asserting on it would be
  // measuring the fixture, not the reader.)
  const pageOverX = await page.evaluate(
    () => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth,
  );
  expect(pageOverX, `page must not scroll sideways (got +${pageOverX}px)`).toBeLessThanOrEqual(1);
});

test("narrow: the two panes stack, the panel fits, and the 「N版」 entry keeps its text", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 360, height: 720 });
  const cmp = await mount(<TaskArtifactVersionsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();

  // ③a The entry is a TEXT chip, not one of the 24px icon squares beside it. A
  // fixed-width square would clip 「2版」 to a sliver.
  const entry = cmp.getByTestId("task-artifact-versions-ta-file");
  const entryBox = (await entry.boundingBox())!;
  expect(
    entryBox.width,
    `the versions entry must fit its own text (got ${entryBox.width}px)`,
  ).toBeGreaterThan(26);
  const entryClipped = await entry.evaluate(
    (el) => el.scrollWidth - el.clientWidth,
  );
  expect(entryClipped, "the entry's own text must not be clipped").toBeLessThanOrEqual(1);

  await entry.click();
  await expect(page.getByTestId("ta-versions-content-text")).toBeVisible();

  // ③b The panes STACK below 720px — a 220px list beside the content would
  // leave neither readable.
  const list = (await page.locator(".ta-versions__list").boundingBox())!;
  const body = (await page.locator(".ta-versions__body").boundingBox())!;
  expect(
    body.y,
    `the content pane must start below the list (list ends at ${list.y + list.height})`,
  ).toBeGreaterThanOrEqual(list.y + list.height - 1);

  const panel = (await page.locator(".ta-versions__panel").boundingBox())!;
  expect(panel.x).toBeGreaterThanOrEqual(0);
  expect(panel.x + panel.width).toBeLessThanOrEqual(360 + 1);
  expect(panel.y + panel.height).toBeLessThanOrEqual(720 + 1);

  const pageOverX = await page.evaluate(
    () => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth,
  );
  expect(pageOverX, `page must not scroll sideways (got +${pageOverX}px)`).toBeLessThanOrEqual(1);
});

test("the reader follows the theme rather than painting its own colours", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskArtifactVersionsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  await cmp.getByTestId("task-artifact-versions-ta-file").click();
  await expect(page.locator(".ta-versions__panel")).toBeVisible();

  const read = () =>
    page.evaluate(() => ({
      panel: getComputedStyle(document.querySelector(".ta-versions__panel")!)
        .backgroundColor,
      title: getComputedStyle(document.querySelector(".ta-versions__title")!).color,
      selected: getComputedStyle(
        document.querySelector(".ta-versions__row--on .ta-versions__row-name")!,
      ).color,
    }));

  const dark = await read();
  // A REAL shipped pack, applied the way the product applies one.
  await page.evaluate((pack) => {
    for (const [k, v] of Object.entries(pack))
      document.documentElement.style.setProperty(k, v);
  }, LIGHT_PACK);
  const light = await read();

  expect(light.panel, "the panel must take the theme's card colour").toBe(
    "rgb(253, 251, 241)",
  );
  expect(light.title, "the title must take the theme's strong text colour").toBe(
    "rgb(30, 28, 16)",
  );
  expect(
    light.selected,
    "the selected version must take the theme's accent",
  ).toBe("rgb(43, 69, 11)");
  // …and all three actually MOVED — a colour that is identical in both themes
  // is a hard-coded one wearing a token's name.
  expect(light.panel).not.toBe(dark.panel);
  expect(light.title).not.toBe(dark.title);
  expect(light.selected).not.toBe(dark.selected);
});
