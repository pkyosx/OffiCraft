// A finger, and the reader's motion settings, for the touch guards.
import { expect } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/** A finger, through the browser's own touch input (CDP), so the press goes
 * through the same gesture handling a phone's does. */
export async function touch(
  page: Page,
  at: { x: number; y: number },
  { holdMs, moveY = 0 }: { holdMs: number; moveY?: number },
) {
  const cdp = await page.context().newCDPSession(page);
  const point = (y: number) => [{ x: at.x, y, id: 1 }];
  await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: point(at.y) });
  for (let i = 1; i <= 6 && moveY; i++) {
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchMove",
      touchPoints: point(at.y + (moveY * i) / 6),
    });
  }
  await page.waitForTimeout(holdMs);
  await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
  await cdp.detach();
}

/** Pin prefers-reduced-motion and prefers-reduced-transparency: a runner's
 * own accessibility settings would otherwise pick the selection mode's blur
 * or its plain fade for us. The session stays open, or the emulation ends. */
export async function preferReduced(page: Page, reduced: boolean) {
  const cdp = await page.context().newCDPSession(page);
  const value = reduced ? "reduce" : "no-preference";
  await cdp.send("Emulation.setEmulatedMedia", {
    features: [
      { name: "prefers-reduced-motion", value },
      { name: "prefers-reduced-transparency", value },
    ],
  });
  expect(
    await page.evaluate(
      (q) => matchMedia(q).matches,
      `(prefers-reduced-motion: ${value}) and (prefers-reduced-transparency: ${value})`,
    ),
    "the media emulation did not take",
  ).toBe(true);
}

export async function centreOf(locator: Locator) {
  await locator.scrollIntoViewIfNeeded();
  // A scroll closes the sheet; let this one settle before the press.
  await locator.page().waitForTimeout(150);
  const b = (await locator.boundingBox())!;
  return { x: b.x + Math.min(b.width / 2, 40), y: b.y + b.height / 2 };
}
