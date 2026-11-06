// Whether a typed value is covered on screen comes from a stylesheet, which jsdom
// does not apply: only a real browser can tell the box still masks.
import { test, expect } from "@playwright/experimental-ct-react";
import { ReplySecretStory } from "./stories/ReplySecretStory";

for (const viewport of [390, 1280]) {
  test(`${viewport}px: the box masks a multi-line value and sits inside the card above the composer`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<ReplySecretStory />);
    const input = cmp.getByTestId("reply-secret-input");

    await input.fill("dummy-aaaaaaaaaaaa\nline two");
    await expect(input).toHaveValue("dummy-aaaaaaaaaaaa\nline two");
    const mask = await input.evaluate(
      (el) => getComputedStyle(el).getPropertyValue("-webkit-text-security"),
    );
    expect(mask).toBe("disc");

    const card = (await page.getByTestId("card-secret").boundingBox())!;
    const box = (await cmp.getByTestId("reply-secret-box").boundingBox())!;
    const composer = (await cmp.locator(".reply-composer").boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(card.x - 0.5);
    expect(box.x + box.width).toBeLessThanOrEqual(card.x + card.width + 0.5);
    expect(box.y + box.height).toBeLessThanOrEqual(composer.y + 0.5);
  });
}
