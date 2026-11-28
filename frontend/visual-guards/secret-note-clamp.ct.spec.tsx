// 金鑰卡的備註 at phone and desktop width, measured by the browser's own layout:
// a long note is cut to three lines with a fade and a 展開全文 toggle that opens
// it to its full height and closes it again; the seed's one-line note shows its
// whole text with neither. jsdom cannot tell the two apart, so this is the only
// place the overflow detection itself is exercised.
import { test, expect } from "@playwright/experimental-ct-react";
import { SecretNoteStory } from "./stories/SecretNoteStory";

for (const width of [390, 1440]) {
  test(`a long note is clamped to three lines behind 展開全文 and a short one shows whole with no toggle @${width}px`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<SecretNoteStory />);
    const rows = cmp.locator('[data-testid="secret-row"]');
    await expect(rows).toHaveCount(2);
    const [short, long] = [rows.nth(0), rows.nth(1)];

    await expect(short.getByTestId("secret-note-text-body")).toBeVisible();
    await expect(short.getByTestId("secret-note-text-toggle")).toHaveCount(0);
    const shortBody = await short.getByTestId("secret-note-text-body").evaluate((el) => ({
      hidden: el.scrollHeight - el.clientHeight,
      mask: getComputedStyle(el).maskImage,
    }));
    expect.soft(shortBody.hidden, `[${width}px] short note clipped`).toBeLessThanOrEqual(1);
    expect.soft(shortBody.mask, `[${width}px] short note faded`).toBe("none");

    const body = long.getByTestId("secret-note-text-body");
    const toggle = long.getByTestId("secret-note-text-toggle");
    await expect(toggle).toBeVisible();
    await expect(toggle).toHaveText("展開全文");
    const clamped = await body.evaluate((el) => {
      const lh = parseFloat(getComputedStyle(el).lineHeight);
      return {
        lines: el.clientHeight / lh,
        hidden: el.scrollHeight - el.clientHeight,
        mask: getComputedStyle(el).maskImage,
      };
    });
    expect.soft(Math.round(clamped.lines), `[${width}px] clamped note lines`).toBe(3);
    expect.soft(clamped.hidden, `[${width}px] text hidden below the clamp`).toBeGreaterThan(1);
    expect.soft(clamped.mask, `[${width}px] clamped note fades`).not.toBe("none");

    const bodyBox = (await body.boundingBox())!;
    const toggleBox = (await toggle.boundingBox())!;
    expect.soft(toggleBox.y, `[${width}px] toggle under the note`).toBeGreaterThanOrEqual(bodyBox.y + bodyBox.height - 0.5);

    await toggle.click();
    await expect(toggle).toHaveText("收起");
    const open = await body.evaluate((el) => ({
      hidden: el.scrollHeight - el.clientHeight,
      mask: getComputedStyle(el).maskImage,
      height: el.clientHeight,
    }));
    expect.soft(open.hidden, `[${width}px] opened note still clipped`).toBeLessThanOrEqual(1);
    expect.soft(open.mask, `[${width}px] opened note faded`).toBe("none");
    expect.soft(open.height, `[${width}px] opened note taller than the clamp`).toBeGreaterThan(bodyBox.height + 1);
    await expect(cmp.getByTestId("secret-edit-form")).toHaveCount(0);

    await toggle.click();
    await expect(toggle).toHaveText("展開全文");
    expect.soft((await body.boundingBox())!.height, `[${width}px] closed again`).toBeCloseTo(bodyBox.height, 0);

    const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect.soft(over, `[${width}px] page horizontal scroll`).toBeLessThanOrEqual(1);
  });
}
