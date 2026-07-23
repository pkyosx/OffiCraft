// HOTSPOT — product-guide DOC page, phone-width horizontal overflow (T-23df).
//
// Bug (owner, phone, v0.5.19): opening a guide doc made the type jump huge and
// the page slide sideways; some docs were fine, some not. Two symptoms, one
// root cause: `.settings` is a flex column with overflow-y:auto, and the
// centring rule's `margin-inline:auto` on that flex ITEM overrode
// align-items:stretch — so `.doc-card` collapsed to content width and grew to
// its max-width:900px. overflow-y:auto coerces overflow-x to auto, so `.settings`
// absorbs that 900px as an internal horizontal PAN while the page stays 390
// (pageOver=0). On a phone the pan is the sideways slide, and iOS Safari's
// -webkit-text-size-adjust then inflates the text UNEVENLY (owner: "有些字大有些
// 字小"). `width:100%` on the centring rule restores fill and kills both.
//
// Why the T-d451 guard (docmd-longtoken-wrap) missed it: it asserts page-level
// scrollWidth, but the overflow lived on the `.settings` SURFACE, not the page —
// pageOver was 0 throughout. This guard measures the SURFACE, so it bites. It
// renders every real doc's *.md against the real shell + sheets at 390px.
//
// Faithfulness: the markdown + referenced assets are read off disk here (Node)
// and handed to the story — assets as data: URLs so images load at their true
// intrinsic size with no server. (The architecture SVG is NOT the cause: the
// block-image renderer already inlines max-width:100% on it; the base
// `.doc-md img { max-width:100% }` rule is a defensive backstop, not the fix.)
//
// MUTANTS (each verified red):
//   drop `width: 100%` from the `.guide-view` centring rules (settings.css)
//     → `.doc-card` grows to its 900px max-width; the `.settings` surface
//       overflows up to +558px on 10 docs while pageOver stays 0 — the exact
//       case a page-only assert misses. The surface assertion below goes red.
//   drop `-webkit-text-size-adjust: 100%` (settings.css)
//     → the text-size-adjust assertion below goes red
import { test, expect } from "@playwright/experimental-ct-react";
import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";
import { GuideDocOverflowStory } from "./stories/GuideDocOverflowStory";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const GUIDE_DIR = path.join(HERE, "..", "..", "docs", "guide");

/** Every guide doc, in the shipped reading order (api_docs.go docReadingOrder).
 * Kept explicit so a doc added without a viewport check fails loudly here. */
const DOCS = [
  "why",
  "install",
  "quickstart",
  "interface",
  "members",
  "tasks",
  "settings",
  "best-practices",
  "architecture",
  "glossary",
  "mobile",
  "troubleshooting",
];

const MIME: Record<string, string> = {
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
};

/** Read a doc's *.md + build {filename → data: URL} for every asset it refs. */
function loadDoc(slug: string): { markdown: string; assets: Record<string, string> } {
  const markdown = fs.readFileSync(path.join(GUIDE_DIR, `${slug}.md`), "utf8");
  const assets: Record<string, string> = {};
  const re = /\]\((?:\.\/)?assets\/([^)\s]+)\)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(markdown))) {
    const name = m[1];
    if (assets[name]) continue;
    const bytes = fs.readFileSync(path.join(GUIDE_DIR, "assets", name));
    const ext = path.extname(name).toLowerCase();
    const mime = MIME[ext] ?? "application/octet-stream";
    assets[name] = `data:${mime};base64,${bytes.toString("base64")}`;
  }
  return { markdown, assets };
}

/** After mount, wait for every <img> to finish loading (or error) so intrinsic
 * width is real before we measure. */
async function waitImages(page: any) {
  await page.evaluate(
    () =>
      Promise.all(
        Array.from(document.images).map((img) =>
          img.complete
            ? Promise.resolve()
            : new Promise<void>((r) => {
                img.onload = () => r();
                img.onerror = () => r();
              })
        )
      )
  );
}

/** Horizontal overflow of the doc SCROLL SURFACE (`.settings.guide-view`) — the
 * element the reader actually pans on the phone — plus the page and the single
 * widest offending descendant (for a diagnosis message that names what to cap).
 *
 * Why the surface and not just the page: `.settings` declares `overflow-y:auto`,
 * which per the overflow spec COERCES its overflow-x to `auto` too — so an
 * over-wide child is ABSORBED as an internal horizontal scroll and the page
 * (document.scrollingElement) stays 390. That silent absorption IS the owner's
 * "頁面左右滑動" (and the text-size-adjust trigger), yet a page-only assertion
 * sails straight over it — the exact false-green that let this regression ship.
 * scrollWidth<=clientWidth on the surface is the assertion that actually bites. */
async function measure(page: any) {
  return page.evaluate(() => {
    const se = document.scrollingElement!;
    const pageOver = se.scrollWidth - se.clientWidth;
    const surface = document.querySelector(".settings.guide-view") as HTMLElement;
    const surfaceOver = surface ? surface.scrollWidth - surface.clientWidth : -1;
    const vw = window.innerWidth;
    let worst = { desc: "(none)", over: 0 };
    for (const el of Array.from(document.querySelectorAll("*"))) {
      const r = (el as HTMLElement).getBoundingClientRect();
      const over = Math.round(r.right - vw);
      if (over > worst.over) {
        const e = el as HTMLElement;
        const cls = e.className && typeof e.className === "string" ? "." + e.className.trim().split(/\s+/).join(".") : "";
        worst = { desc: `${e.tagName.toLowerCase()}${cls}`, over };
      }
    }
    return { pageOver, surfaceOver, worst };
  });
}

for (const slug of DOCS) {
  test(`390px: guide doc "${slug}" does not scroll the page sideways`, async ({
    mount,
    page,
  }) => {
    const { markdown, assets } = loadDoc(slug);
    await page.setViewportSize({ width: 390, height: 844 });
    await mount(
      <GuideDocOverflowStory title={slug} markdown={markdown} assets={assets} />
    );
    await waitImages(page);

    const { pageOver, surfaceOver, worst } = await measure(page);
    // Repro log — surfaces the per-doc overflow + the offending element.
    console.log(
      `[guide-overflow] ${slug}: surfaceOver=${surfaceOver}px pageOver=${pageOver}px worst=${worst.desc} (+${worst.over}px)`
    );
    expect(
      surfaceOver,
      `[390px] "${slug}" doc surface must not pan sideways — widest offender ${worst.desc} (+${worst.over}px)`
    ).toBeLessThanOrEqual(1);
    // The page itself must not scroll either (a doc surface that lost its
    // overflow-y:auto would hand the overflow up to the page instead).
    expect(
      pageOver,
      `[390px] "${slug}" page must not scroll sideways`
    ).toBeLessThanOrEqual(1);
  });
}

// text-size-adjust regression pin: the doc surface must declare
// `-webkit-text-size-adjust: 100%` (the hard stop against iOS's uneven auto
// zoom) AND base prose must stay at its 14px CSS size (no accidental huge
// font). architecture.md is the representative image-bearing doc.
test("390px: guide doc pins text size (no iOS auto-inflate, base 14px)", async ({
  mount,
  page,
}) => {
  const { markdown, assets } = loadDoc("architecture");
  await page.setViewportSize({ width: 390, height: 844 });
  await mount(
    <GuideDocOverflowStory title="architecture" markdown={markdown} assets={assets} />
  );
  await waitImages(page);

  const adjust = await page.evaluate(() => {
    const el = document.querySelector(".guide-view--doc") as HTMLElement;
    const cs = getComputedStyle(el) as any;
    return cs.webkitTextSizeAdjust ?? cs.getPropertyValue("-webkit-text-size-adjust");
  });
  expect(adjust, "doc surface must hard-stop iOS text-size-adjust").toBe("100%");

  const pFont = await page.evaluate(() => {
    const p = document.querySelector(".doc-md p") as HTMLElement;
    return getComputedStyle(p).fontSize;
  });
  expect(pFont, "base prose must stay at its 14px CSS size").toBe("14px");
});
