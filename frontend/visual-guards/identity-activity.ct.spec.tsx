// The avatar-corner presence dot and the activity status icon (or, for a member
// that is offline or stopped, the moon; waking, or without a report yet, the unknown mark)
// leading the name on the list rows, the chat
// header and the detail identity card, the activity tree in the 運行狀況 card, and the dialog
// the icon opens, at phone and desktop widths.
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

type Still = "offline" | "unknown";

async function stillIcons(page: Page, kind: Still) {
  return page.evaluate(
    (k) =>
      Array.from(document.querySelectorAll<HTMLElement>(`.activity-icon--${k}`)).map((icon) => {
        const b = icon.getBoundingClientRect();
        const hit = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
        const glyph = icon.querySelector<HTMLElement>(`.activity-icon__glyph--${k}`)!;
        const muted = document.createElement("span");
        muted.style.color = "var(--color-text-muted)";
        document.body.append(muted);
        const mutedColor = getComputedStyle(muted).color;
        muted.remove();
        return {
          tag: icon.tagName,
          tabIndex: icon.tabIndex,
          visibility: getComputedStyle(icon).visibility,
          opacity: getComputedStyle(icon).opacity,
          glyph: glyph.querySelector("svg")!.getBoundingClientRect().toJSON() as Box,
          color: getComputedStyle(glyph).color,
          mutedColor,
          cursor: getComputedStyle(icon).cursor,
          animations: [icon, ...Array.from(icon.querySelectorAll("*"))].map((el) => getComputedStyle(el).animationName),
          centreOnIcon: icon.contains(hit),
        };
      }),
    kind,
  );
}

function expectStillIcons(list: Awaited<ReturnType<typeof stillIcons>>, count: number, noun: string) {
  expect(list.length, `${noun}s drawn`).toBe(count);
  for (const [i, m] of list.entries()) {
    const label = `${noun} ${i + 1}`;
    expect([m.tag, m.tabIndex], `${label}: a plain span out of the tab order, not a button`).toEqual(["SPAN", -1]);
    expect([m.visibility, m.opacity], `${label}: visible`).toEqual(["visible", "1"]);
    expect(m.glyph.width, `${label}: glyph drawn at 14px`).toBe(14);
    expect(m.color, `${label}: muted grey`).toBe(m.mutedColor);
    expect(m.cursor, `${label}: no pointer cursor`).toBe("default");
    expect(m.animations.every((name) => name === "none"), `${label}: nothing inside it animates (${m.animations.join(",")})`).toBe(true);
    expect(m.centreOnIcon, `${label}: a pointer at its centre lands on it`).toBe(true);
  }
}

const UNKNOWN_HINT = "狀態未知";

async function expectHintOnly(page: Page, sel: string, word: string) {
  await page.locator(sel).hover();
  await expect(page.getByRole("tooltip")).toHaveText(word);
  await page.locator(sel).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(sel)).not.toBeFocused();
  expect(await page.evaluate(() => document.body.getAttribute("data-opened")), `${sel}: the click opened nothing`).toBeNull();
  await expect(
    page.locator(
      ".activity-icon--offline.activity-icon--button, .activity-icon--offline button, .activity-icon--unknown.activity-icon--button, .activity-icon--unknown button",
    ),
  ).toHaveCount(0);
}

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
    test(`width ${width}, ${state}: list rows carry the corner dot and the icon before the name without overflow`, async ({ mount, page }) => {
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
        expect(b.icon!.right, `${label}: icon before the name`).toBeLessThanOrEqual(b.name!.left);
        const lineHeight = await page.locator(`${r.row} ${r.name}`).first().evaluate((n) => parseFloat(getComputedStyle(n).lineHeight));
        expect(
          Math.abs(b.icon!.top + b.icon!.height / 2 - (b.name!.top + lineHeight / 2)),
          `${label}: icon centred on the name's first line`,
        ).toBeLessThanOrEqual(1);
        expect(b.icon!.width, `${label}: icon keeps its natural width`).toBeGreaterThanOrEqual(b.glyph!.width - 0.5);
        expect(b.icon!.right, `${label}: icon clear of the unread badge`).toBeLessThanOrEqual(b.unread!.left);
        expect(b.unread!.right, `${label}: unread badge inside the row`).toBeLessThanOrEqual(rowBox.x + rowBox.width + 0.5);
        expect(b.dot!.right, `${label}: dot clear of the icon`).toBeLessThanOrEqual(b.icon!.left);
      }
      expect(await hscroll(page), "no page hscroll").toBeLessThanOrEqual(1);
    });

    test(`width ${width}, ${state}: each list's icons share one x, an online row without activity gets a visible, still unknown mark and an offline row a visible, still moon, so the names line up`, async ({ mount, page }) => {
      await page.setViewportSize({ width, height: 900 });
      await mount(<IdentityActivityStory surface="list" state={state} mobile={width < 720} />);
      const lists = [
        { row: ".member-card", name: ".member-card__name" },
        { row: ".outsource-row", name: ".outsource-row__codename" },
      ];
      for (const l of lists) {
        const rows = await page.evaluate(
          ([rowSel, nameSel]) =>
            Array.from(document.querySelectorAll(rowSel)).map((row) => {
              const icon = row.querySelector(".activity-icon")!;
              return {
                kind: icon.classList.contains("activity-icon--unknown")
                  ? "unknown"
                  : icon.classList.contains("activity-icon--offline")
                    ? "moon"
                    : "icon",
                icon: icon.getBoundingClientRect().toJSON() as Box,
                name: row.querySelector(nameSel)!.getBoundingClientRect().toJSON() as Box,
              };
            }),
          [l.row, l.name] as const,
        );
        expect(rows.map((r) => r.kind), `${l.row}: an online row without activity, then an offline row, last`).toEqual([
          ...Array(rows.length - 2).fill("icon"),
          "unknown",
          "moon",
        ]);
        const [first, ...rest] = rows;
        for (const [i, r] of rest.entries()) {
          const label = `${l.row} ${i + 2}`;
          expect(Math.abs(r.icon.left - first.icon.left), `${label}: icon at the first row's x`).toBeLessThanOrEqual(0.5);
          expect(Math.abs(r.icon.width - first.icon.width), `${label}: icon, unknown mark or moon as wide as the first row's icon`).toBeLessThanOrEqual(0.5);
          expect(Math.abs(r.name.left - first.name.left), `${label}: name at the first row's x`).toBeLessThanOrEqual(0.5);
        }
      }
      expectStillIcons(await stillIcons(page, "unknown"), 2, "unknown mark");
      expectStillIcons(await stillIcons(page, "offline"), 2, "moon");
      await expectHintOnly(page, ".member-card .activity-icon--unknown", UNKNOWN_HINT);
      await expectHintOnly(page, ".outsource-row .activity-icon--unknown", UNKNOWN_HINT);
      await expectHintOnly(page, ".member-card .activity-icon--offline", "已停止");
      await expectHintOnly(page, ".outsource-row .activity-icon--offline", "離線");
    });
  }

  test(`width ${width}: a list mixing every icon — star, hourglass, coffee, unknown mark, moon — keeps them and the names on one x`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mount(<IdentityActivityStory surface="list" state="busy" mobile={width < 720} mixed />);
    for (const l of [
      { row: ".member-card", name: ".member-card__name" },
      { row: ".outsource-row", name: ".outsource-row__codename" },
    ]) {
      const rows = await page.evaluate(
        ([rowSel, nameSel]) =>
          Array.from(document.querySelectorAll(rowSel)).map((row) => {
            const icon = row.querySelector(".activity-icon")!;
            return {
              glyph: icon.querySelector(".activity-icon__glyph")!.className.replace("activity-icon__glyph activity-icon__glyph--", ""),
              icon: icon.getBoundingClientRect().toJSON() as Box,
              name: row.querySelector(nameSel)!.getBoundingClientRect().toJSON() as Box,
            };
          }),
        [l.row, l.name] as const,
      );
      expect(rows.map((r) => r.glyph), `${l.row}: one row per icon`).toEqual(["working", "waiting", "idle", "unknown", "offline"]);
      const [first, ...rest] = rows;
      for (const [i, r] of rest.entries()) {
        const label = `${l.row} ${r.glyph}`;
        expect(Math.abs(r.icon.left - first.icon.left), `${label}: icon at the star row's x`).toBeLessThanOrEqual(0.5);
        expect(Math.abs(r.icon.width - first.icon.width), `${label}: as wide as the star row's icon`).toBeLessThanOrEqual(0.5);
        expect(Math.abs(r.name.left - first.name.left), `${label}: name at the star row's x (row ${i + 2})`).toBeLessThanOrEqual(0.5);
      }
    }
    expect(await hscroll(page), "no page hscroll").toBeLessThanOrEqual(1);
  });

  test(`width ${width}: the chat header keeps its height with the icon, the unknown mark or the offline moon before the ellipsized name, the dot sits on the avatar corner`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mount(<IdentityActivityStory surface="header" state="busy" mobile={width < 720} />);
    const withIcon = await boxes(page, '[data-testid="header-with"]', {
      header: ":scope",
      avatar: ".avatar",
      dot: ".presence-avatar__dot .lifecycle-dot",
      icon: ".activity-icon",
      glyph: ".activity-icon svg",
      name: ".chat__header-name > span:not(.activity-icon)",
      button: ".chat__header-action",
    });
    const bare = await boxes(page, '[data-testid="header-without"]', {
      header: ":scope",
      unknown: ".activity-icon--unknown",
      name: ".chat__header-name > span:not(.activity-icon)",
      avatar: ".avatar",
    });
    const off = await boxes(page, '[data-testid="header-offline"]', {
      header: ":scope",
      moon: ".activity-icon--offline",
      name: ".chat__header-name > span:not(.activity-icon)",
      avatar: ".avatar",
    });
    expect(Math.abs(off.header!.height - bare.header!.height), "header height unchanged by the moon").toBeLessThanOrEqual(0.5);
    expect(off.moon!.right, "moon before the name").toBeLessThanOrEqual(off.name!.left);
    expect(Math.abs(off.moon!.left - off.avatar!.right - (withIcon.icon!.left - withIcon.avatar!.right)), "moon where the icon sits").toBeLessThanOrEqual(0.5);
    expect(Math.abs(off.name!.left - withIcon.name!.left), "name where it sits after the icon").toBeLessThanOrEqual(0.5);
    expectStillIcons(await stillIcons(page, "offline"), 1, "moon");
    await expectHintOnly(page, '[data-testid="header-offline"] .activity-icon--offline', "離線");
    expect(Math.abs(bare.unknown!.left - bare.avatar!.right - (withIcon.icon!.left - withIcon.avatar!.right)), "unknown mark where the icon sits").toBeLessThanOrEqual(0.5);
    expect(Math.abs(bare.name!.left - withIcon.name!.left), "name where it sits after the icon").toBeLessThanOrEqual(0.5);
    expectStillIcons(await stillIcons(page, "unknown"), 1, "unknown mark");
    await expectHintOnly(page, '[data-testid="header-without"] .activity-icon--unknown', UNKNOWN_HINT);
    expect(Math.abs(withIcon.header!.height - bare.header!.height), "header height unchanged by the icon").toBeLessThanOrEqual(0.5);
    expect(withIcon.icon!.right, "icon before the name").toBeLessThanOrEqual(withIcon.name!.left);
    expect(withIcon.dot!.right, "dot clear of the icon").toBeLessThanOrEqual(withIcon.icon!.left);
    const ellipsized = await page.evaluate(() => {
      const n = document.querySelector('[data-testid="header-with"] .chat__header-name > span:not(.activity-icon)') as HTMLElement;
      return { cut: n.scrollWidth > n.clientWidth, ellipsis: getComputedStyle(n).textOverflow };
    });
    expect(ellipsized.ellipsis, "the name still ellipsizes").toBe("ellipsis");
    if (width < 720) expect(ellipsized.cut, "the long name is cut on a phone").toBe(true);
    expectCornerDot(withIcon.dot!, withIcon.avatar!, "header");
    expect(withIcon.icon!.width, "icon keeps its natural width").toBeGreaterThanOrEqual(withIcon.glyph!.width - 0.5);
    expect(withIcon.icon!.right, "icon left of the header buttons").toBeLessThanOrEqual(withIcon.button!.left);
    expect(await hscroll(page), "no page hscroll").toBeLessThanOrEqual(1);
  });

  test(`width ${width}: the detail identity card leads the name with the icon, offline with the moon, and online without activity with the unknown mark`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mount(<IdentityActivityStory surface="detail" state="busy" mobile={width < 720} />);
    const withIcon = await boxes(page, '[data-testid="identity-with"]', {
      card: ":scope",
      avatar: ".avatar",
      dot: ".presence-avatar__dot .lifecycle-dot",
      icon: ".activity-icon",
      glyph: ".activity-icon svg",
      name: ".mp-identity__name",
    });
    const bare = await boxes(page, '[data-testid="identity-without"]', { card: ":scope", unknown: ".activity-icon--unknown", name: ".mp-identity__name" });
    const off = await boxes(page, '[data-testid="identity-offline"]', { card: ":scope", moon: ".activity-icon--offline", name: ".mp-identity__name" });
    expect(Math.abs(off.moon!.left - withIcon.icon!.left), "moon where the icon sits").toBeLessThanOrEqual(0.5);
    expect(off.moon!.right, "moon before the name").toBeLessThanOrEqual(off.name!.left);
    expect(Math.abs(off.name!.left - withIcon.name!.left), "name where it sits after the icon").toBeLessThanOrEqual(0.5);
    expect(off.moon!.top, "moon on the name's line").toBeLessThan(off.name!.bottom);
    expect(off.name!.right, "name inside the card").toBeLessThanOrEqual(off.card!.right);
    expectStillIcons(await stillIcons(page, "offline"), 1, "moon");
    await expectHintOnly(page, '[data-testid="identity-offline"] .activity-icon--offline', "離線");
    expect(Math.abs(bare.unknown!.left - withIcon.icon!.left), "unknown mark where the icon sits").toBeLessThanOrEqual(0.5);
    expect(bare.unknown!.right, "unknown mark before the name").toBeLessThanOrEqual(bare.name!.left);
    expect(Math.abs(bare.name!.left - withIcon.name!.left), "name where it sits after the icon").toBeLessThanOrEqual(0.5);
    expect(bare.unknown!.top, "unknown mark on the name's line").toBeLessThan(bare.name!.bottom);
    expect(bare.name!.right, "name inside the card").toBeLessThanOrEqual(bare.card!.right);
    expectStillIcons(await stillIcons(page, "unknown"), 1, "unknown mark");
    await expectHintOnly(page, '[data-testid="identity-without"] .activity-icon--unknown', UNKNOWN_HINT);
    expectCornerDot(withIcon.dot!, withIcon.avatar!, "identity");
    expect(withIcon.icon!.right, "icon before the name").toBeLessThanOrEqual(withIcon.name!.left);
    expect(withIcon.icon!.width, "icon keeps its natural width").toBeGreaterThanOrEqual(withIcon.glyph!.width - 0.5);
    expect(withIcon.name!.right, "name inside the card").toBeLessThanOrEqual(withIcon.card!.right);
    expect(await hscroll(page), "no page hscroll").toBeLessThanOrEqual(1);
  });

  test(`width ${width}: the activity tree in the 運行狀況 card wraps inside the card and indents each level`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mount(<IdentityActivityStory surface="card" state="busy" mobile={width < 720} />);
    const m = await page.evaluate(() => {
      const card = document.querySelector('[data-testid="mp-activity"]')!.closest(".mp-runtime") as HTMLElement;
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

test.describe("the 運行狀況 dialog the icon opens", () => {
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

  test("width 1280: closing with the mouse hands focus back without leaving the hint up; Esc keeps it for the keyboard", async ({ mount, page }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await mount(<IdentityActivityStory surface="list" state="busy" mobile={false} />);
    const icon = page.locator(".member-card .activity-icon").first();
    await icon.click();
    await page.getByRole("dialog").getByRole("button").click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(icon).toBeFocused();
    await expect(page.getByRole("tooltip")).toHaveCount(0);
    await icon.press("Enter");
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(icon).toBeFocused();
    await expect(page.getByRole("tooltip")).toHaveCount(1);
  });

  for (const width of [390, 1280]) {
    test.describe(`on a touch screen ${width} wide`, () => {
      test.use({ hasTouch: true, isMobile: true, viewport: { width, height: 780 } });

      test("the icon takes taps over at least 32px each way, clear of the warning mark below and of the avatar and its corner dot beside it, while its chip stays the size it shows", async ({ mount, page }) => {
        await mount(<IdentityActivityStory surface="list" state="busy" mobile={width < 720} warning />);
        const rows = await page.evaluate(() =>
          Array.from(document.querySelectorAll<HTMLElement>(".member-card, .outsource-row"))
            .filter((row) => row.querySelector(".activity-icon--button"))
            .map((row) => {
              const icon = row.querySelector(".activity-icon") as HTMLElement;
              const dot = row.querySelector(".presence-avatar__dot .lifecycle-dot") as HTMLElement;
              const avatar = row.querySelector(".member-card__avatar, .outsource-row__avatar-btn") as HTMLElement;
              const mark = row.querySelector<HTMLElement>(".runtime-login-warning");
              const hitArea = (el: HTMLElement) => {
                const b = el.getBoundingClientRect();
                const cx = b.left + b.width / 2;
                const cy = b.top + b.height / 2;
                const lands = (x: number, y: number) => el.contains(document.elementFromPoint(x, y));
                const reach = (dx: number, dy: number) => {
                  let d = 0;
                  while (d < 100 && lands(cx + dx * (d + 0.5), cy + dy * (d + 0.5))) d += 0.5;
                  return d;
                };
                return {
                  left: cx - reach(-1, 0),
                  right: cx + reach(1, 0),
                  top: cy - reach(0, -1),
                  bottom: cy + reach(0, 1),
                  centreLands: lands(cx, cy),
                };
              };
              return {
                chip: icon.getBoundingClientRect().toJSON() as Box,
                chipAtRest: [getComputedStyle(icon).borderTopColor, getComputedStyle(icon).backgroundColor],
                hit: hitArea(icon),
                dotHit: hitArea(dot),
                avatar: avatar.getBoundingClientRect().toJSON() as Box,
                avatarCentreLands: (() => {
                  const a = avatar.getBoundingClientRect();
                  return avatar.contains(document.elementFromPoint(a.left + a.width / 2, a.top + a.height / 2));
                })(),
                mark: mark ? (mark.getBoundingClientRect().toJSON() as Box) : null,
                markHit: mark ? hitArea(mark) : null,
              };
            }),
        );
        expect(rows.length, "two member rows and one outsource row carry the icon").toBe(3);
        const overlap = (a: { left: number; right: number; top: number; bottom: number }, b: typeof a) =>
          a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
        for (const [i, m] of rows.entries()) {
          const label = `row ${i + 1}`;
          expect(m.chipAtRest, `${label}: no chip at rest`).toEqual(["rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)"]);
          expect(m.chip.height, `${label}: the chip itself stays smaller than its tap area`).toBeLessThan(32);
          expect(m.hit.right - m.hit.left, `${label}: tap area width`).toBeGreaterThanOrEqual(32);
          expect(m.hit.bottom - m.hit.top, `${label}: tap area height`).toBeGreaterThanOrEqual(32);
          expect(m.hit.centreLands, `${label}: a tap on the icon lands on it`).toBe(true);
          expect(overlap(m.hit, m.dotHit), `${label}: tap area clear of the corner dot's tap area`).toBe(false);
          expect(m.hit.left, `${label}: tap area clear of the avatar`).toBeGreaterThanOrEqual(m.avatar.right);
          expect(m.dotHit.centreLands, `${label}: a tap on the corner dot still lands on it`).toBe(true);
          expect(m.avatarCentreLands, `${label}: a tap on the avatar still lands on it`).toBe(true);
          if (m.mark && m.markHit) {
            expect(overlap(m.hit, m.markHit), `${label}: tap area clear of the warning mark's tap area`).toBe(false);
            expect(m.hit.bottom, `${label}: tap area ends above the warning mark's`).toBeLessThanOrEqual(m.mark.top);
            expect(m.markHit.centreLands, `${label}: a tap on the warning mark still lands on it`).toBe(true);
          }
        }
        expect(rows.filter((m) => m.mark).length, "both member rows carry the warning mark").toBe(2);
      });

      test("a tap on an online row's unknown mark only pins its hint and a tap on an offline row's moon only its presence word; neither opens the dialog or the row", async ({ mount, page }) => {
        await mount(<IdentityActivityStory surface="list" state="idle" mobile={width < 720} />);
        for (const [sel, word] of [
          [".member-card .activity-icon--unknown", UNKNOWN_HINT],
          [".outsource-row .activity-icon--unknown", UNKNOWN_HINT],
          [".member-card .activity-icon--offline", "已停止"],
        ] as const) {
          await page.locator(sel).tap();
          await expect(page.getByRole("tooltip")).toHaveText(word);
          await expect(page.getByRole("dialog")).toHaveCount(0);
          expect(await page.evaluate(() => document.body.getAttribute("data-opened")), `${sel}: the tap opened no row`).toBeNull();
        }
      });
    });
  }
});

test("each state glyph animates — the 工作中 sparkle, the 等待中 hourglass flip, the 閒置 steam — and all stand still under reduced motion; the unknown mark and the offline moon never move", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mount(<IdentityActivityStory surface="glyphs" state="busy" mobile={false} />);
  const parts = {
    busy: [".sparkle-icon__star", ".sparkle-icon__twinkle--top", ".sparkle-icon__twinkle--bottom"],
    waiting: ["svg"],
    idle: [".coffee-icon__steam--left", ".coffee-icon__steam--mid", ".coffee-icon__steam--right"],
  };
  const animations = () =>
    page.evaluate(
      (p) =>
        Object.fromEntries(
          Object.entries(p).map(([state, sels]) => [
            state,
            sels.map((sel) => {
              const cs = getComputedStyle(document.querySelector(`[data-testid="glyph-${state}"] ${sel}`)!);
              return { name: cs.animationName, seconds: parseFloat(cs.animationDuration) };
            }),
          ]),
        ),
      parts,
    );
  const moving = await animations();
  expectStillIcons(await stillIcons(page, "unknown"), 1, "unknown mark");
  expectStillIcons(await stillIcons(page, "offline"), 1, "moon");
  for (const [state, list] of Object.entries(moving)) {
    for (const a of list) {
      expect(a.name, `${state}: animated`).not.toBe("none");
      expect(a.seconds, `${state}: runs for a while`).toBeGreaterThan(0);
    }
  }
  const slowest = (state: string) => Math.min(...moving[state].map((a) => a.seconds));
  expect(slowest("waiting"), "等待中 calmer than 工作中").toBeGreaterThan(slowest("busy"));
  expect(slowest("idle"), "閒置 calmer than 工作中").toBeGreaterThan(slowest("busy"));
  await page.emulateMedia({ reducedMotion: "reduce" });
  const still = await animations();
  expect(Object.values(still).flat().map((a) => a.name)).toEqual(Array(7).fill("none"));
});

test("width 1280: the clickable icon shows only its glyph at rest and its chip on hover and keyboard focus", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mount(<IdentityActivityStory surface="list" state="busy" mobile={false} />);
  const icon = page.locator(".member-card .activity-icon").first();
  const chip = () =>
    icon.evaluate((el) => {
      const cs = getComputedStyle(el);
      return { border: cs.borderTopColor, background: cs.backgroundColor, ring: cs.boxShadow };
    });
  const TRANSPARENT = "rgba(0, 0, 0, 0)";
  const rest = await chip();
  expect(rest, "no chip at rest").toEqual({ border: TRANSPARENT, background: TRANSPARENT, ring: "none" });
  const restBox = (await icon.boundingBox())!;
  await icon.hover();
  await expect.poll(async () => (await chip()).border, "chip border on hover").not.toBe(TRANSPARENT);
  await expect.poll(async () => (await chip()).ring, "chip ring on hover").not.toBe("none");
  const hoverBox = (await icon.boundingBox())!;
  expect([hoverBox.width, hoverBox.height], "the chip does not change the box").toEqual([restBox.width, restBox.height]);
  await page.mouse.move(0, 0);
  await expect.poll(async () => (await chip()).border, "chip gone after hover").toBe(TRANSPARENT);
  for (let i = 0; i < 6 && !(await icon.evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press("Tab");
  }
  await expect(icon).toBeFocused();
  await expect.poll(async () => (await chip()).border, "chip border on keyboard focus").not.toBe(TRANSPARENT);
});
