// HOTSPOT — 使用說明 in-app doc links, in a REAL browser (T-68f1).
//
// Bug (owner): a guide doc's `[介面說明](interface.md)` printed as the literal
// source text `[…](…)` and could not be clicked. Root cause was ONE thing:
// Markdown's scheme allowlist (http/https/mailto) has no notion of a
// repo-relative path, so it fell through to the literal-text default — which is
// why "shows raw syntax" and "is not clickable" were the same defect.
//
// Why a CT guard on top of the vitest suites: the fix renders a <button>, not
// an <a href>, and three of its properties are INVISIBLE to jsdom —
//   (1) does it actually LOOK like the link it replaced (colour + underline;
//       jsdom applies no stylesheet, so a button that renders as a grey
//       system-chrome button would pass every unit test);
//   (2) is it really hit-testable (visible, non-zero box, not covered);
//   (3) does clicking it leave the page URL alone — the property that makes
//       "no href" a SECURITY choice and not just a styling one. A real browser
//       is the only place a navigation can happen to be observed.
// The e2e_test/ suite needs a live server + a fleet host; this guard needs
// neither, and this page had zero Playwright coverage of any kind before.
//
// MUTANTS (each verified red, and each red assertion NAMED — see the impl
// report ow52-68f1-linkfix-impl.md for the observed failure lines):
//   • Markdown.tsx: drop the `opts?.resolveDocLink && DOC_REL_PATH_RE` branch
//       → "the in-app link must be a real control" goes red (locator not found)
//   • UserGuidePage.tsx: drop the `docs.some(...)` existence check
//       → "an unshipped target stays literal" goes red (button appears)
//   • settings.css: delete the `.doc-md .md-doclink` rule
//       → "must read as a link" goes red on text-decoration
import { test, expect } from "@playwright/experimental-ct-react";
import { GuideDocLinksStory } from "./stories/GuideDocLinksStory";

/** The rendered doc BODY's own <h1> — NOT the page header, which prints the
 * same title and would make "the body switched" indistinguishable from "the
 * header switched". */
const DOC_H1 = ".doc-md h1";

test("a repo-relative doc link is a real control that switches the doc", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<GuideDocLinksStory />);
  await expect(cmp.locator(DOC_H1)).toHaveText("為什麼是 OffiCraft");

  // The literal source text — the owner's actual symptom — is gone.
  await expect(cmp).not.toContainText("](interface.md)");

  const link = cmp.locator("button.md-doclink", { hasText: "介面說明" });
  await expect(
    link,
    "the in-app link must be a real control, not literal text"
  ).toBeVisible();

  // (3) Clicking must not navigate. Captured BEFORE, compared AFTER: a
  // <button> has no href, so there is nothing for an open redirect to aim at.
  const before = page.url();
  await link.click();
  await expect(cmp.locator(DOC_H1)).toHaveText("介面說明");
  expect(page.url(), "an in-app doc link must not navigate the page").toBe(
    before
  );

  // And it chains: the destination doc's own link goes back.
  await cmp.locator("button.md-doclink", { hasText: "為什麼是 OffiCraft" }).click();
  await expect(cmp.locator(DOC_H1)).toHaveText("為什麼是 OffiCraft");
});

test("the in-app link must READ as a link, not as a browser button", async ({
  mount,
}) => {
  // The `interface` doc carries BOTH kinds side by side: an in-app button and
  // a real external <a> styled by `.doc-md a`. Comparing them in one document
  // pins the fix to the sheet's own accent colour instead of hardcoding a value
  // this guard would then have to be edited to follow.
  const cmp = await mount(<GuideDocLinksStory start="interface" />);
  await expect(cmp.locator(DOC_H1)).toHaveText("介面說明");

  const style = (el: HTMLElement) => {
    const cs = getComputedStyle(el);
    return {
      color: cs.color,
      deco: cs.textDecorationLine,
      border: cs.borderTopWidth,
    };
  };

  const anchor = cmp.locator(".doc-md a").first();
  await expect(anchor).toBeVisible();
  const want = await anchor.evaluate(style);

  const btn = cmp.locator("button.md-doclink").first();
  await expect(btn).toBeVisible();
  const got = await btn.evaluate(style);

  expect(got.color, "in-app link colour must match .doc-md a").toBe(want.color);
  expect(got.deco, "in-app link must be underlined like .doc-md a").toBe(
    want.deco
  );
  expect(got.border, "in-app link must not render as a chrome button").toBe(
    "0px"
  );
  // A hit-testable box, not a 0x0 sliver.
  const box = await btn.boundingBox();
  expect(box!.width, "in-app link must have a clickable width").toBeGreaterThan(10);
  expect(box!.height, "in-app link must have a clickable height").toBeGreaterThan(8);
});

test("an UNSHIPPED target stays literal text (never a dead button)", async ({
  mount,
}) => {
  // ../dev/agent-env.md derives the slug "agent-env", which is deliberately NOT
  // embedded — it must degrade to literal text, never a button that 404s.
  const cmp = await mount(<GuideDocLinksStory start="install" />);
  await expect(cmp.locator(DOC_H1)).toHaveText("安裝、升級與移除");
  await expect(cmp).toContainText("[../dev/agent-env.md](../dev/agent-env.md)");
  await expect(cmp.locator("button.md-doclink")).toHaveCount(0);
  // The external link in the same doc still opens out, hardened.
  const a = cmp.locator(".doc-md a").first();
  await expect(a).toHaveAttribute("target", "_blank");
  await expect(a).toHaveAttribute("rel", "noopener noreferrer");
  // And the alert marker is consumed, not printed.
  await expect(cmp).not.toContainText("[!NOTE]");
  await expect(cmp.locator("blockquote.md-alert--note")).toHaveCount(1);
});

test("a javascript: target is inert literal text in the real browser", async ({
  mount,
}) => {
  const cmp = await mount(<GuideDocLinksStory start="why" />);
  await expect(cmp.locator(DOC_H1)).toHaveText("為什麼是 OffiCraft");
  await expect(cmp).toContainText("[別點我](javascript:alert(1))");
  // Nothing in the document carries the javascript: payload as a target, and
  // nothing labelled with it is clickable.
  await expect(cmp.locator('a[href^="javascript:"]')).toHaveCount(0);
  await expect(
    cmp.locator("button.md-doclink", { hasText: "別點我" })
  ).toHaveCount(0);
});
