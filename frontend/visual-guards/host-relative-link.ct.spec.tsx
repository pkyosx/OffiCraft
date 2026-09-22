// HOTSPOT — a link with no host, judged by a REAL browser.
//
// Bug (owner, three cards in a row): an agent running on the station's own
// machine only knows the loopback address it calls the API with, so every
// studio link it wrote opened nowhere else. The fix lets a link target be a
// same-origin path with exactly one leading slash.
//
// Why a CT guard on top of the vitest suite: the unit tests can only assert
// the href ATTRIBUTE — the string we wrote. The claim this change makes is
// about the address a click would GO to, and that is the browser's own URL
// parser, not ours. Three shapes (`//h`, `/\h`, `/<TAB>/h`) reach a browser as
// a DIFFERENT HOST despite the leading slash, and jsdom cannot settle that:
// asserting "we refused it" in jsdom is asserting our own regex back to
// ourselves. Here the same string is read by Chromium.
//
// MUTANTS (each verified red, with the named assertion):
//   • drop the `(?![/\\])` lookahead in SAFE_PATH_RE
//       → "a shape a browser reads as another host is never a link" goes red
//         on the protocol-relative row (a link appears, off-origin)
//   • drop the URL_STRIPPED_CHARS_RE normalisation
//       → the same test goes red on the tab row
import { test, expect } from "@playwright/experimental-ct-react";
import { HostRelativeLinkStory } from "./stories/HostRelativeLinkStory";
import { TARGETS } from "./stories/hostRelativeLinkRows";

const SAFE_ROWS = ["task", "card"];
const OFF_ORIGIN_ROWS = ["protocol-relative", "backslash", "tab"];

test("a one-slash path resolves against the page's own origin", async ({
  mount,
  page,
}) => {
  await mount(<HostRelativeLinkStory />);
  const origin = await page.evaluate(() => window.location.origin);

  for (const row of SAFE_ROWS) {
    const a = page.locator(`[data-row="${row}"] a`);
    await expect(a, `${row} must render a link`).toHaveCount(1);
    // `.href` is the RESOLVED address — what a click goes to — not the
    // attribute we wrote. That resolution is the whole claim.
    const resolved = await a.evaluate((el) => (el as HTMLAnchorElement).href);
    expect(resolved.startsWith(origin + "/#"), `${row} resolved to ${resolved}`).toBe(true);
  }
});

test("a shape a browser reads as another host is never a link", async ({
  mount,
  page,
}) => {
  await mount(<HostRelativeLinkStory />);
  const origin = await page.evaluate(() => window.location.origin);

  for (const row of OFF_ORIGIN_ROWS) {
    const a = page.locator(`[data-row="${row}"] a`);
    // The assertion is deliberately in two halves. "No link" is the behaviour
    // we want; the second half says WHY it matters by measuring what the
    // browser would have done with that exact string — so a future reader can
    // see the shape is genuinely off-origin and not merely unfamiliar.
    await expect(a, `${row} must stay literal text`).toHaveCount(0);
    const dest = await page.evaluate(
      (s: string) => new URL(s, window.location.href).origin,
      TARGETS[row],
    );
    expect(dest, `${row} is only interesting if it really leaves the origin`).not.toBe(origin);
  }
});

test("an ordinary absolute url is untouched by any of this", async ({ mount, page }) => {
  await mount(<HostRelativeLinkStory />);
  const a = page.locator('[data-row="absolute"] a');
  await expect(a).toHaveCount(1);
  await expect(a).toHaveAttribute("href", "https://evil.example/x");
});
