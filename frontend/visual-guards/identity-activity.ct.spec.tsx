// The avatar-corner presence dot and the activity status icon on the list rows
// and the chat header, the 運作狀況 card's tree, and the dialog the icon opens,
// at phone and desktop widths.
// Every check compares boxes with each other in a real browser (CT pages lack
// the production fonts, so no absolute text widths).
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { IdentityActivityStory } from "./stories/IdentityActivityStory";

type Box = { left: number; right: number; top: number; bottom: number; width: number; height: number };

async function boxes(page: Page, root: string, sel: Record<string, string>) {
  return page.evaluate(
    ([rootSel, sels]: readonly [string, Record<string, string>]) => {
      const r = document.querySelector(rootSel)!;
      const out: Record<string, Box | null> = {};
      for (const [k, s] of Object.entries(sels)) {
        const el = s === ":scope" ? r : r.querySelector(s);
        out[k] = el ? (el.getBoundingClientRect().toJSON() as Box) : null;
      }
      return out;
    },
    [root, sel] as const,
  );
}

const hscroll = (page: Page) =>
  page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);

function expectCornerDot(dot: Box, avatar: Box, label: string) {
  const cx = dot.left + dot.width / 2;
  const cy = dot.top + dot.height / 2;
  expect(cx, `${label}: dot sits on the avatar's bottom-right corner (x)`).toBeGreaterThan(avatar.left + avatar.width * 0.75);
  expect(cy, `${label}: dot sits on the avatar's bottom-right corner (y)`).toBeGreaterThan(avatar.top + avatar.height * 0.75);
  expect(cx, `${label}: dot centre within the avatar's reach`).toBeLessThanOrEqual(avatar.right + 2);
  expect(cy, `${label}: dot centre within the avatar's reach`).toBeLessThanOrEqual(avatar.bottom + 2);
}

for (const width of [390, 1280]) {
  for (const state of ["busy", "waiting", "idle"] as const) {
    test(`width ${width}, ${state}: list rows carry the corner dot and the icon after the name without overflow`, async ({ mount, page }) => {
      await page.setViewportSize({ width, height: 900 });
      await mount(<IdentityActivityStory surface="list" state={state} mobile={width < 720} />);
      const rows = [
        { row: ".member-card:nth-of-type(1)", name: ".member-card__name", avatar: ".member-card__avatar", unread: ".member-card__unread" },
        { row: ".outsource-row", name: ".outsource-row__codename", avatar: ".outsource-row__avatar-btn", unread: ".member-card__unread" },
      ];
      for (const r of rows) {
        const b = await boxes(page, r.row, {
          row: ":scope",
          name: r.name,
          avatar: r.avatar,
          dot: ".presence-avatar__dot .lifecycle-dot",
          icon: ".activity-icon",
          glyph: ".activity-icon svg",
          unread: r.unread,
        });
        const rowBox = (await page.locator(r.row).first().boundingBox())!;
        const label = `${r.row}`;
        expectCornerDot(b.dot!, b.avatar!, label);
        expect(b.icon!.left, `${label}: icon after the name`).toBeGreaterThanOrEqual(b.name!.left);
        expect(b.icon!.width, `${label}: icon keeps its natural width`).toBeGreaterThanOrEqual(b.glyph!.width - 0.5);
        expect(b.icon!.right, `${label}: icon clear of the unread badge`).toBeLessThanOrEqual(b.unread!.left);
        expect(b.unread!.right, `${label}: unread badge inside the row`).toBeLessThanOrEqual(rowBox.x + rowBox.width + 0.5);
        expect(b.dot!.right, `${label}: dot clear of the name`).toBeLessThanOrEqual(b.name!.left);
      }
      expect(await hscroll(page), "no page hscroll").toBeLessThanOrEqual(1);
    });
  }

  test(`width ${width}: the chat header keeps its height with the icon, the dot sits on the avatar corner`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mount(<IdentityActivityStory surface="header" state="busy" mobile={width < 720} />);
    const withIcon = await boxes(page, '[data-testid="header-with"]', {
      header: ":scope",
      avatar: ".avatar",
      dot: ".presence-avatar__dot .lifecycle-dot",
      icon: ".activity-icon",
      glyph: ".activity-icon svg",
      button: ".chat__header-action",
    });
    const bare = await boxes(page, '[data-testid="header-without"]', { header: ":scope" });
    expect(Math.abs(withIcon.header!.height - bare.header!.height), "header height unchanged by the icon").toBeLessThanOrEqual(0.5);
    expectCornerDot(withIcon.dot!, withIcon.avatar!, "header");
    expect(withIcon.icon!.width, "icon keeps its natural width").toBeGreaterThanOrEqual(withIcon.glyph!.width - 0.5);
    expect(withIcon.icon!.right, "icon left of the header buttons").toBeLessThanOrEqual(withIcon.button!.left);
    expect(await hscroll(page), "no page hscroll").toBeLessThanOrEqual(1);
  });

  test(`width ${width}: the 運作狀況 card's tree wraps inside the card and indents each level`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mount(<IdentityActivityStory surface="card" state="busy" mobile={width < 720} />);
    const m = await page.evaluate(() => {
      const card = document.querySelector('[data-testid="mp-activity"]') as HTMLElement;
      const rows = Array.from(card.querySelectorAll<HTMLElement>(".mp-activity__row")).map((row) => {
        const r = row.getBoundingClientRect();
        const aside = row.querySelector<HTMLElement>(".mp-activity__aside");
        const a = aside?.getBoundingClientRect();
        return {
          left: r.left,
          right: r.right,
          overflow: row.scrollWidth - row.clientWidth,
          asideRight: a ? a.right : null,
          asideLines: aside ? Math.round(a!.height / parseFloat(getComputedStyle(aside).lineHeight)) : 0,
          dashed: getComputedStyle(row).borderTopStyle === "dashed",
        };
      });
      const c = card.getBoundingClientRect();
      return { cardRight: c.right, cardOverflow: card.scrollWidth - card.clientWidth, rows };
    });
    expect(m.cardOverflow, "card does not overflow").toBeLessThanOrEqual(0);
    expect(m.rows.length).toBe(5);
    const [main, a1, a2, bg, cron] = m.rows;
    expect(a1.left - main.left, "child indented under the main agent").toBeGreaterThanOrEqual(8);
    expect(a2.left - a1.left, "grandchild indented under its parent").toBeGreaterThanOrEqual(8);
    expect(bg.left, "waits hang under the main agent").toBe(a1.left);
    expect([bg.dashed, cron.dashed, main.dashed], "waits are dashed nodes").toEqual([true, true, false]);
    for (const r of m.rows) {
      expect(r.overflow, "row does not overflow").toBeLessThanOrEqual(0);
      expect(r.right, "row inside the card").toBeLessThanOrEqual(m.cardRight);
      if (r.asideRight !== null) {
        expect(r.asideRight, "aside inside its row").toBeLessThanOrEqual(r.right);
      }
    }
    expect(main.asideLines, "aside on one line").toBe(1);
    expect(a2.asideLines, "aside on one line").toBe(1);
    expect(await hscroll(page), "no page hscroll").toBeLessThanOrEqual(1);
  });
}

test.describe("the 運作狀況 dialog the icon opens", () => {
  const dialogBox = (page: Page) =>
    page.evaluate(() => {
      const box = document.querySelector(".activity-dialog__box") as HTMLElement;
      const card = box.getBoundingClientRect();
      const rows = Array.from(box.querySelectorAll(".mp-activity__row")).map((r) => r.getBoundingClientRect().right);
      return {
        box: card.toJSON() as Box,
        boxOverflow: box.scrollWidth - box.clientWidth,
        rowsRight: Math.max(...rows),
        vw: document.documentElement.clientWidth,
        vh: window.innerHeight,
        listIcon: document.querySelector(".member-card .activity-icon")!.getBoundingClientRect().toJSON() as Box,
      };
    });

  test("width 390: a bottom sheet across the viewport, inside it, with no horizontal scroll", async ({ mount, page }) => {
    await page.setViewportSize({ width: 390, height: 780 });
    await mount(<IdentityActivityStory surface="list" state="busy" mobile />);
    await page.locator(".member-card .activity-icon").first().click();
    await expect(page.getByRole("dialog")).toBeVisible();
    const m = await dialogBox(page);
    expect(Math.abs(m.box.bottom - m.vh), "sheet sits on the bottom edge").toBeLessThanOrEqual(1);
    expect(m.box.left, "sheet spans the width").toBeLessThanOrEqual(0.5);
    expect(Math.abs(m.box.right - m.vw), "sheet spans the width").toBeLessThanOrEqual(0.5);
    expect(m.box.top, "sheet top inside the viewport").toBeGreaterThan(0);
    expect(m.boxOverflow, "sheet does not overflow sideways").toBeLessThanOrEqual(0);
    expect(m.rowsRight, "tree rows inside the sheet").toBeLessThanOrEqual(m.box.right);
    expect(await hscroll(page), "no page hscroll").toBeLessThanOrEqual(1);
  });

  test("width 1280: a centred dialog about 420 wide", async ({ mount, page }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await mount(<IdentityActivityStory surface="list" state="busy" mobile={false} />);
    await page.locator(".member-card .activity-icon").first().click();
    await expect(page.getByRole("dialog")).toBeVisible();
    const m = await dialogBox(page);
    const cx = m.box.left + m.box.width / 2;
    const cy = m.box.top + m.box.height / 2;
    expect(Math.abs(cx - m.vw / 2), "centred horizontally").toBeLessThanOrEqual(1);
    expect(Math.abs(cy - m.vh / 2), "centred vertically").toBeLessThanOrEqual(1);
    expect(m.box.width, "about 420 wide").toBeGreaterThan(m.vw * 0.3);
    expect(m.box.width, "about 420 wide").toBeLessThanOrEqual(420.5);
    expect(m.box.width, "about 420 wide").toBeGreaterThanOrEqual(400);
    expect(m.rowsRight, "tree rows inside the dialog").toBeLessThanOrEqual(m.box.right);
    expect(await hscroll(page), "no page hscroll").toBeLessThanOrEqual(1);
  });

  test.describe("on a touch screen", () => {
    test.use({ hasTouch: true, isMobile: true, viewport: { width: 390, height: 780 } });

    test("the icon takes taps over at least 32px while its chip stays the size it shows", async ({ mount, page }) => {
      await mount(<IdentityActivityStory surface="list" state="busy" mobile />);
      const hit = await page.evaluate(() => {
        const icon = document.querySelector(".member-card .activity-icon") as HTMLElement;
        const r = icon.getBoundingClientRect();
        const cx = r.left + r.width / 2;
        const cy = r.top + r.height / 2;
        const lands = (x: number, y: number) => icon.contains(document.elementFromPoint(x, y));
        return {
          chip: r.toJSON() as Box,
          across: lands(cx - 15.5, cy) && lands(cx + 15.5, cy),
          upDown: lands(cx, cy - 15.5) && lands(cx, cy + 12),
        };
      });
      expect(hit.chip.height, "the chip itself stays smaller than its tap area").toBeLessThan(32);
      expect(hit.across, "32px across").toBe(true);
      expect(hit.upDown, "taps above and below land on the icon").toBe(true);
    });
  });
});

test("the 工作中 sparkle animates, and stands still under reduced motion", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mount(<IdentityActivityStory surface="list" state="busy" mobile={false} />);
  const animations = () =>
    page.evaluate(() =>
      [".sparkle-icon__star", ".sparkle-icon__twinkle--top", ".sparkle-icon__twinkle--bottom"].map(
        (s) => getComputedStyle(document.querySelector(`.member-card ${s}`)!).animationName,
      ),
    );
  expect(await animations()).not.toContain("none");
  await page.emulateMedia({ reducedMotion: "reduce" });
  expect(await animations()).toEqual(["none", "none", "none"]);
});
