// Whether a typed value is covered on screen, and how tall the box is, comes from a
// stylesheet, which jsdom does not apply: only a real browser can tell.
import { test, expect } from "@playwright/experimental-ct-react";
import { ReplySecretStory } from "./stories/ReplySecretStory";

for (const viewport of [390, 1280]) {
  test(`${viewport}px: the box masks a value, starts one line tall beside its send button, grows then scrolls, inside the card above the composer`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<ReplySecretStory />);
    const input = cmp.getByTestId("reply-secret-input");
    const height = async () => (await input.boundingBox())!.height;

    const empty = await height();
    expect(empty).toBeLessThan(24);

    await input.fill("dummy-aaaaaaaaaaaa");
    expect(await height()).toBe(empty);
    const mask = await input.evaluate((el) => getComputedStyle(el).getPropertyValue("-webkit-text-security"));
    expect(mask).toBe("disc");

    const threeLines = "dummy-aaaaaaaaaaaa\nline two\nline three";
    await input.fill(threeLines);
    await expect(input).toHaveValue(threeLines);
    expect(await height()).toBeGreaterThan(empty * 2.5);

    await input.fill(Array.from({ length: 40 }, () => "dummy-aaaaaaaaaaaa").join("\n"));
    expect(await height()).toBe(132);
    const scrolls = await input.evaluate((el) => el.scrollHeight > el.clientHeight);
    expect(scrolls).toBe(true);

    await input.fill("");
    expect(await height()).toBe(empty);

    const send = (await cmp.getByTestId("reply-secret-send").boundingBox())!;
    const field = (await input.boundingBox())!;
    expect(field.x + field.width).toBeLessThanOrEqual(send.x);
    expect(Math.abs(field.y + field.height / 2 - (send.y + send.height / 2))).toBeLessThan(1);

    const card = (await page.getByTestId("card-secret").boundingBox())!;
    const box = (await cmp.getByTestId("reply-secret-box").boundingBox())!;
    const composer = (await cmp.locator(".reply-composer").boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(card.x - 0.5);
    expect(box.x + box.width).toBeLessThanOrEqual(card.x + card.width + 0.5);
    expect(box.y + box.height).toBeLessThanOrEqual(composer.y + 0.5);
  });
}
