// T-48 fix12 — 「內容還沒到」 on screen, in a REAL browser.
//
// 🔴 WHAT ONLY A BROWSER CAN ANSWER HERE.
//   · the show-after delay is a TIMER against a real event loop; jsdom's fake
//     and real clocks both make it whatever the test says it is;
//   · 「撈完才 render」 is a claim about what reached the SCREEN, and React 18
//     batches every update inside `act()`, so jsdom answers the same for one
//     commit and for four (measured: a mutant that commits the anchor window
//     first and then walks left `useChat.scrollback.test.ts` entirely green);
//   · the spinner is a rotating bordered box — its geometry does not exist in
//     jsdom at all.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatThreadLoadingStory } from "./stories/ChatThreadLoadingStory";
import {
  LOADING_TOTAL,
  LOADING_ANCHOR,
} from "./stories/chatThreadLoadingFixtures";

const WIDTHS = [
  { name: "narrow-390", px: 390 },
  { name: "wide-1280", px: 1280 },
];
// The two doors owner c-de666642e77b named: 「不管是進聊天室,或點選元訊息都是
// 這樣」.
const ENTRANCES = ["plain", "anchor"] as const;

for (const w of WIDTHS) {
  for (const entrance of ENTRANCES) {
    test(`載入指示在「${entrance === "plain" ? "進聊天室" : "點原訊息"}」這個入口出現,而且內容一到就消失 — ${w.name}`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width: Math.max(w.px, 400), height: 800 });
      // 🔴 1200ms per page. Two reasons, and both are about leaving the
      // measurement room to happen in rather than about the product:
      //   · past the 150ms show-after delay — the plain entrance is ONE page,
      //     so at the story's default 120ms it would land BEFORE the delay and
      //     correctly draw nothing (that is the last test's subject, not this
      //     one's);
      //   · long enough that the rotation can be sampled TWICE while the
      //     element still exists. At 400ms the plain entrance's spinner lived
      //     ~250ms and the second sample raced its unmount — a flake that is
      //     the guard's own, not the product's.
      await mount(
        <ChatThreadLoadingStory
          entrance={entrance}
          widthPx={w.px}
          latencyMs={1200}
        />,
      );

      const spinner = page.locator(".chat__loading");
      // 🔴 IT IS ACTUALLY ON SCREEN — not merely in the DOM. A spinner with no
      // box is the same as no spinner.
      await expect(spinner).toBeVisible();
      await expect(spinner).toBeInViewport();
      const ring = page.locator(".chat__loading-spinner");
      const box = await ring.boundingBox();
      expect(box, "轉圈本身要有實際的框").not.toBeNull();
      expect(box!.width).toBeGreaterThan(6);
      expect(box!.height).toBeGreaterThan(6);
      // …and it really rotates: sample the computed transform twice.
      const [t1, t2] = await ring.evaluate(async (el) => {
        const a = getComputedStyle(el).transform;
        await new Promise((r) => setTimeout(r, 100));
        return [a, getComputedStyle(el).transform];
      });
      expect(t1, "轉圈沒有在轉 —— 動畫是這個元件的全部").not.toBe(t2);

      // 內容一到就走。
      await expect(page.locator(".chat__msg").first()).toBeVisible({
        timeout: 20_000,
      });
      await expect(spinner).toHaveCount(0);
      // 兩個入口都要真的把該載的載到:一般進房是最新一頁,錨點進房是**一路到
      // 活尾巴**(fix12 的正題)。
      const rows = await page.locator(".chat__msg").count();
      if (entrance === "anchor") {
        await expect(
          page.locator(`[data-msg-id="L${LOADING_TOTAL - 1}"]`),
          "錨點進房要一路撈到最新那一則",
        ).toBeAttached();
        await expect(
          page.locator(`[data-msg-id="${LOADING_ANCHOR}"]`),
          "而且要停在目標上",
        ).toBeInViewport();
      } else {
        expect(rows, "一般進房只載最新一頁").toBeLessThan(LOADING_TOTAL);
      }
    });
  }
}

// 🔴 ONE STATE, NOT TWO — the claim owner c-d24ebd7f8d78 asked for in as many
// words. Both entrances are driven from the SAME mount, in the SAME run, and the
// mutant that removes the single flag must redden BOTH. If only one goes red the
// flag is really two flags and this test says so.
test("兩個入口共用同一個狀態 —— 拿掉它,兩邊都不見", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const seen: Record<string, boolean> = {};
  for (const entrance of ENTRANCES) {
    const c = await mount(
      <ChatThreadLoadingStory
        entrance={entrance}
        widthPx={1280}
        latencyMs={1200}
      />,
    );
    // 🔴 WAIT for it rather than SAMPLE for it. A bare `isVisible()` right after
    // mount races the 150ms show-after delay, which makes this guard flake on
    // its own timing rather than on the product (measured: 2 reds in 3 runs).
    // The wait is bounded well inside the 1200ms page, so「沒出現」 still means
    // 沒出現.
    seen[entrance] = await page
      .locator(".chat__loading")
      .waitFor({ state: "visible", timeout: 5_000 })
      .then(() => true)
      .catch(() => false);
    await expect(page.locator(".chat__msg").first()).toBeVisible({
      timeout: 20_000,
    });
    await c.unmount();
  }
  expect(seen, "兩個入口都要有載入指示,而且是同一個狀態驅動的").toEqual({
    plain: true,
    anchor: true,
  });
});

// 🔴 撈完才 render (owner c-6a973512ed77). The anchor entrance walks three
// pages; if each committed, the thread would appear at 30 → 130 → 229 → 260
// rows. Sampled every animation frame, the row count must go straight from 0 to
// its final value: no intermediate thread ever reaches the screen.
test("錨點進房是一次落地 —— 畫面上不准出現中途的列數", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await mount(
    <ChatThreadLoadingStory entrance="anchor" widthPx={1280} latencyMs={60} />,
  );
  const samples = await page.evaluate(async () => {
    const seen: number[] = [];
    const deadline = performance.now() + 20_000;
    for (;;) {
      const n = document.querySelectorAll(".chat__msg").length;
      if (seen[seen.length - 1] !== n) seen.push(n);
      if (n > 0) break;
      if (performance.now() > deadline) break;
      await new Promise((r) => requestAnimationFrame(() => r(null)));
    }
    // Keep watching a little longer, so a LATER extra commit is seen too.
    const until = performance.now() + 600;
    while (performance.now() < until) {
      const n = document.querySelectorAll(".chat__msg").length;
      if (seen[seen.length - 1] !== n) seen.push(n);
      await new Promise((r) => requestAnimationFrame(() => r(null)));
    }
    return seen;
  });
  // 0 → final, and nothing else. A page-by-page commit puts 30/130/229 in here.
  expect(
    samples,
    "中途的列數上了畫面 —— 走訪又變回一頁一頁 commit 了",
  ).toEqual([0, 260]);
});

// 🔴 快的時候不准閃 (the delay's own reason). With a page that lands well inside
// the show-after window, the element must never exist at all.
test("很快就載完的時候,轉圈完全不出現 —— 閃一下比不出現更糟", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  let appeared = false;
  await page.exposeFunction("__rcSeen", () => {
    appeared = true;
  });
  await mount(
    <ChatThreadLoadingStory entrance="plain" widthPx={1280} latencyMs={5} />,
  );
  await page.evaluate(() => {
    const ob = new MutationObserver(() => {
      if (document.querySelector(".chat__loading")) {
        (window as unknown as { __rcSeen: () => void }).__rcSeen();
      }
    });
    ob.observe(document.body, { childList: true, subtree: true });
    if (document.querySelector(".chat__loading")) {
      (window as unknown as { __rcSeen: () => void }).__rcSeen();
    }
  });
  await expect(page.locator(".chat__msg").first()).toBeVisible({
    timeout: 20_000,
  });
  await page.waitForTimeout(300);
  expect(appeared, "載得很快卻還是閃了一下轉圈").toBe(false);
});
