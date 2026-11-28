// 金鑰卡 at phone and desktop width, measured by the browser's own layout.
//
// The note: a long one is cut to three lines with a fade, and the note itself
// opens it to its full height and closes it again (click, Enter); the seed's
// one-line note shows whole and is not a button. jsdom cannot tell the two
// apart, so this is the only place the overflow detection itself is exercised.
//
// The card: id badge left and the edit / delete icons right on the first line,
// the name under it, the note, then 最後存取／最後更新 as label/value rows —
// all inside the card with no sideways scroll, in zh and en. A secret never
// read shows 「-」 as its 最後存取, in the same plain style as a time.
import { test, expect } from "@playwright/experimental-ct-react";
import { SecretNoteStory } from "./stories/SecretNoteStory";

type Box = { left: number; right: number; top: number; bottom: number };

for (const width of [390, 1440]) {
  test(`a long note is clamped to three lines and the note itself opens and closes it; a short one shows whole and is not a button @${width}px`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<SecretNoteStory />);
    const rows = cmp.locator('[data-testid="secret-row"]');
    await expect(rows).toHaveCount(3);
    const [short, long] = [rows.nth(0), rows.nth(1)];

    const shortBody = short.getByTestId("secret-note-text-body");
    await expect(shortBody).toBeVisible();
    await expect(shortBody).not.toHaveAttribute("role", "button");
    const shortMeasure = await shortBody.evaluate((el) => ({
      hidden: el.scrollHeight - el.clientHeight,
      mask: getComputedStyle(el).maskImage,
    }));
    expect.soft(shortMeasure.hidden, `[${width}px] short note clipped`).toBeLessThanOrEqual(1);
    expect.soft(shortMeasure.mask, `[${width}px] short note faded`).toBe("none");

    const body = long.getByTestId("secret-note-text-body");
    await expect(body).toHaveAttribute("role", "button");
    await expect(body).toHaveAttribute("aria-expanded", "false");
    await expect(cmp.getByTestId("secret-note-text-toggle")).toHaveCount(0);
    const clamped = await body.evaluate((el) => {
      const lh = parseFloat(getComputedStyle(el).lineHeight);
      return {
        lines: el.clientHeight / lh,
        hidden: el.scrollHeight - el.clientHeight,
        mask: getComputedStyle(el).maskImage,
        cursor: getComputedStyle(el).cursor,
      };
    });
    expect.soft(Math.round(clamped.lines), `[${width}px] clamped note lines`).toBe(3);
    expect.soft(clamped.hidden, `[${width}px] text hidden below the clamp`).toBeGreaterThan(1);
    expect.soft(clamped.mask, `[${width}px] clamped note fades`).not.toBe("none");
    expect.soft(clamped.cursor, `[${width}px] clamped note pointer`).toBe("pointer");
    const closedBox = (await body.boundingBox())!;

    await body.click();
    await expect(body).toHaveAttribute("aria-expanded", "true");
    const open = await body.evaluate((el) => ({
      hidden: el.scrollHeight - el.clientHeight,
      mask: getComputedStyle(el).maskImage,
      height: el.clientHeight,
    }));
    expect.soft(open.hidden, `[${width}px] opened note still clipped`).toBeLessThanOrEqual(1);
    expect.soft(open.mask, `[${width}px] opened note faded`).toBe("none");
    expect.soft(open.height, `[${width}px] opened note taller than the clamp`).toBeGreaterThan(closedBox.height + 1);
    await expect(cmp.getByTestId("secret-edit-form")).toHaveCount(0);

    await body.click();
    await expect(body).toHaveAttribute("aria-expanded", "false");
    expect.soft((await body.boundingBox())!.height, `[${width}px] closed again`).toBeCloseTo(closedBox.height, 0);

    await body.focus();
    await page.keyboard.press("Enter");
    await expect(body).toHaveAttribute("aria-expanded", "true");
  });
}

for (const width of [320, 390, 1440]) {
  for (const language of ["zh", "en"] as const) {
    test(`width ${width}, ${language}: every secret card keeps its id, icons, name and rows inside it, with no sideways scroll, and a never-read secret's 最後存取 is a plain 「-」`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 1200 });
      await page.evaluate((lang) => localStorage.setItem("oc.language", lang), language);
      const cmp = await mount(<SecretNoteStory />);
      await expect(cmp.locator('[data-testid="secret-row"]')).toHaveCount(3);
      const g = await page.evaluate(() => {
        const r = (el: Element | null) => {
          if (!el) return null;
          const b = el.getBoundingClientRect();
          return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
        };
        const se = document.scrollingElement!;
        const list = document.querySelector(".secrets") as HTMLElement | null;
        return {
          pageOver: se.scrollWidth - se.clientWidth,
          listOver: list ? list.scrollWidth - list.clientWidth : -2,
          rows: [...document.querySelectorAll('[data-testid="secret-row"]')].map((row) => {
            const accessed = row.querySelector('[data-testid="secret-last-accessed"] .secrets__time');
            const updated = row.querySelector('[data-testid="secret-updated"] .secrets__time');
            return {
              id: row.getAttribute("data-secret-id") ?? "",
              row: r(row)!,
              idBadge: r(row.querySelector('[data-testid="secret-id"]')),
              buttons: [...row.querySelectorAll(".lore-row__actions button")].map((b) => ({
                box: r(b)!,
                border: getComputedStyle(b).borderTopColor,
                bg: getComputedStyle(b).backgroundColor,
              })),
              name: r(row.querySelector('[data-testid="secret-name"]')),
              labels: [...row.querySelectorAll(".card-meta__label")].map((l) => r(l)!),
              values: [...row.querySelectorAll(".card-meta__label + *")].map((v) => r(v)!),
              never:
                accessed?.textContent === "-"
                  ? {
                      style: getComputedStyle(accessed).fontStyle,
                      color: getComputedStyle(accessed).color,
                      timeColor: getComputedStyle(updated!).color,
                    }
                  : null,
            };
          }),
        };
      });
      const tag = `[${width}px ${language}]`;
      const inside = (inner: Box, outer: Box) =>
        inner.left >= outer.left - 1 && inner.right <= outer.right + 1 && inner.top >= outer.top - 1 && inner.bottom <= outer.bottom + 1;
      const mid = (b: Box) => (b.top + b.bottom) / 2;
      expect.soft(g.pageOver, `${tag} page horizontal scroll`).toBeLessThanOrEqual(1);
      expect.soft(g.listOver, `${tag} .secrets missing`).not.toBe(-2);
      expect.soft(g.listOver, `${tag} .secrets horizontal scroll`).toBeLessThanOrEqual(1);
      expect(g.rows.length).toBe(3);
      for (const row of g.rows) {
        const t = `${tag} ${row.id}`;
        expect.soft(row.idBadge, `${t} id badge missing`).not.toBeNull();
        expect.soft(row.name, `${t} name missing`).not.toBeNull();
        expect.soft(row.buttons.length, `${t} edit + delete`).toBe(2);
        if (!row.idBadge || !row.name) continue;
        expect.soft(inside(row.idBadge, row.row), `${t} id badge outside the card`).toBe(true);
        for (const b of row.buttons) {
          expect.soft(inside(b.box, row.row), `${t} icon outside the card`).toBe(true);
          expect.soft(Math.abs(mid(b.box) - mid(row.idBadge)), `${t} icon not on the id's line`).toBeLessThanOrEqual(4);
          expect.soft(b.box.left, `${t} icon overlaps the id`).toBeGreaterThanOrEqual(row.idBadge.right);
          expect.soft(b.border, `${t} icon has a visible border`).toBe("rgba(0, 0, 0, 0)");
        }
        expect.soft(row.name.top, `${t} name not under the first line`).toBeGreaterThanOrEqual(row.idBadge.bottom - 1);
        expect.soft(inside(row.name, row.row), `${t} name outside the card`).toBe(true);
        expect.soft(row.labels.length, `${t} 最後存取 + 最後更新`).toBe(2);
        expect.soft(row.values.length, `${t} values`).toBe(2);
        row.labels.forEach((label, i) => {
          const value = row.values[i];
          if (!value) return;
          expect.soft(inside(value, row.row), `${t} value ${i} outside the card`).toBe(true);
          expect.soft(value.left, `${t} value ${i} not right of its label`).toBeGreaterThanOrEqual(label.right);
          expect.soft(Math.abs(mid(value) - mid(label)), `${t} value ${i} not on its label's line`).toBeLessThanOrEqual(4);
        });
        if (row.values.length === 2)
          expect.soft(Math.abs(row.values[0].left - row.values[1].left), `${t} values share one column`).toBeLessThanOrEqual(1);
      }
      const nevers = g.rows.filter((row) => row.never);
      expect.soft(nevers.length, `${tag} never-read secrets`).toBe(2);
      for (const row of nevers) {
        expect.soft(row.never!.style, `${tag} ${row.id} 「-」 upright`).toBe("normal");
        expect.soft(row.never!.color, `${tag} ${row.id} 「-」 in the time's colour`).toBe(row.never!.timeColor);
      }
    });
  }
}
