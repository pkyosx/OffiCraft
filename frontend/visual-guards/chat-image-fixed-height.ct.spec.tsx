// T-48 — 聊天縮圖是一個**預留好的框**，不是一張載入後才撐開版面的圖。
//
// owner rc-7a6a159a102f:「圖片我們在聊天視窗是不是可以給固定的尺寸？我們要看還是
// 會點開來看」。他要的是**落地時高度就是最終高度**。
//
// 為什麼這件事非量不可：`.chat__msg-image` 原本是 `width/height: auto` 加一組
// 300x320 的上限 ⇒ **bytes 沒到之前那一列是零高**，解碼完一次撐開（實測每張
// +46.7 ~ +318px）。那正是「跳轉之後上方的圖片把目標擠走」的主因（實測 +418.69px，
// 而視窗只有 433px 高 —— 差不多整整一個螢幕）。固定框是從源頭消掉那次回流，
// 而不是事後補償它。
//
// 🔴 jsdom 量不到這件事，而且會給假綠：vitest 在 jsdom 裡跑，沒有版面引擎，
// `<img>` 不解碼、`getBoundingClientRect()` 全 0，所以任何「載入前後高度相同」的
// 斷言在那一層**恆真**。這是 harness 的界限，不是取捨 —— 所以護欄在這裡。
//
// MUTANT（已驗紅，摘要見 commit aea7182 的 [test] 段）：把
// `.chat__msg-image` 的 `height: 220px` 換回 `height: auto; max-height: 320px`
// ⇒ (1) 立刻紅在「這一列的高度差必須是 0」上。
//
// MUTANT 2（已驗紅）：把 `object-fit: scale-down` 換回 `contain`
// ⇒ 「小圖不准被放大」那一條紅在畫出來的尺寸上：80x60 的圖被放大成 291x218。
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatImageSizeStory } from "./stories/ChatImageSizeStory";

/** A real, decodable PNG of a given size, as a data: URI. Built here rather than
 * fetched: the guard must control WHEN the bytes arrive, and a data: URI decodes
 * on demand without a network hop that could be flaky. */
function pngDataUri(w: number, h: number): string {
  // A 1x1 PNG scaled by the browser is enough — `object-fit` cares about the
  // INTRINSIC ratio, and the intrinsic ratio is what an SVG can state exactly.
  return `data:image/svg+xml,${encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><rect width="${w}" height="${h}" fill="lime"/></svg>`,
  )}`;
}

/** Mount, measure the image row, load the image, measure again. */
async function heightAcrossLoad(
  mount: Parameters<Parameters<typeof test>[1]>[0]["mount"],
  page: Parameters<Parameters<typeof test>[1]>[0]["page"],
  width: number,
  aspect: { w: number; h: number },
) {
  await page.setViewportSize({ width, height: 844 });
  const cmp = await mount(<ChatImageSizeStory />);
  const row = cmp.getByTestId("image-row");
  await expect(row).toBeVisible();

  const before = await row.evaluate((el) => el.getBoundingClientRect().height);
  const beforeAfterRowTop = await cmp
    .getByTestId("row-after")
    .evaluate((el) => el.getBoundingClientRect().top);

  await cmp.getByTestId("chat-image").evaluate(async (el, uri) => {
    (el as HTMLImageElement).src = uri as string;
    await (el as HTMLImageElement).decode();
  }, pngDataUri(aspect.w, aspect.h));

  const after = await row.evaluate((el) => el.getBoundingClientRect().height);
  const afterAfterRowTop = await cmp
    .getByTestId("row-after")
    .evaluate((el) => el.getBoundingClientRect().top);
  const img = await cmp
    .getByTestId("chat-image")
    .evaluate((el) => {
      const i = el as HTMLImageElement;
      const r = i.getBoundingClientRect();
      return {
        w: Number(r.width.toFixed(2)),
        h: Number(r.height.toFixed(2)),
        naturalW: i.naturalWidth,
        naturalH: i.naturalHeight,
        objectFit: getComputedStyle(i).objectFit,
      };
    });
  return { cmp, before, after, beforeAfterRowTop, afterAfterRowTop, img };
}

/** The size the image is actually PAINTED at inside its box, in CSS px.
 *
 * 🔴 `getBoundingClientRect()` cannot answer this. The box is a fixed
 * `min(300px,100%) x 220px` whether the image is upscaled into it or drawn small
 * and centred — the element rect reads 293x220 either way, so a rect-based
 * assertion would be green under both. Only the pixels distinguish them: the
 * element is screenshotted, decoded back INSIDE the page (Node here has no PNG
 * decoder) and scanned for the fixture's fill colour. Desktop Chrome runs at
 * deviceScaleFactor 1, so one screenshot pixel is one CSS px. */
async function paintedSize(
  page: Parameters<Parameters<typeof test>[1]>[0]["page"],
  shot: Buffer,
) {
  return page.evaluate(async (b64) => {
    const blob = await (await fetch(`data:image/png;base64,${b64}`)).blob();
    const bmp = await createImageBitmap(blob);
    const canvas = document.createElement("canvas");
    canvas.width = bmp.width;
    canvas.height = bmp.height;
    const ctx = canvas.getContext("2d")!;
    ctx.drawImage(bmp, 0, 0);
    const { data } = ctx.getImageData(0, 0, canvas.width, canvas.height);
    let minX = Infinity;
    let minY = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    for (let y = 0; y < canvas.height; y += 1) {
      for (let x = 0; x < canvas.width; x += 1) {
        const i = (y * canvas.width + x) * 4;
        const lime =
          data[i + 3] > 200 &&
          data[i + 1] > 180 &&
          data[i] < 120 &&
          data[i + 2] < 120;
        if (!lime) continue;
        if (x < minX) minX = x;
        if (x > maxX) maxX = x;
        if (y < minY) minY = y;
        if (y > maxY) maxY = y;
      }
    }
    return maxX < 0
      ? { w: 0, h: 0 }
      : { w: maxX - minX + 1, h: maxY - minY + 1 };
  }, shot.toString("base64"));
}

for (const width of [390, 1280]) {
  for (const aspect of [
    { w: 400, h: 300, name: "4:3 landscape" },
    { w: 1600, h: 400, name: "4:1 panorama" },
    { w: 300, h: 1200, name: "1:4 portrait" },
  ]) {
    test(`${width}px · ${aspect.name}: 圖片載入前後,那一列的高度一模一樣`, async ({
      mount,
      page,
    }) => {
      const m = await heightAcrossLoad(mount, page, width, aspect);

      // 前提誠實:圖片真的解碼了。少了這一行,整條可能只是「圖沒載到,所以什麼
      // 都沒動過」的空綠。
      expect(
        m.img.naturalH,
        `the image must actually have decoded (got ${JSON.stringify(m.img)})`,
      ).toBeGreaterThan(0);

      // 🔴 這是護欄本體。差 0,不是「差得不多」。
      expect(
        m.after - m.before,
        `含圖那一列的高度在載入前後必須完全一樣 —— 量到 ${JSON.stringify(m)}`,
      ).toBe(0);

      // …而「那一列沒變高」要能證明成「它下面的東西沒有被推走」,因為那才是
      // owner 看得到的那件事。
      expect(
        m.afterAfterRowTop - m.beforeAfterRowTop,
        `下一列不准被推走 —— 量到 ${JSON.stringify(m)}`,
      ).toBe(0);

      // 完整顯示,不裁切:整張圖縮進框內。`cover` 會裁掉,owner 明確不要。
      expect(m.img.objectFit).toBe("scale-down");

      // 框本身是固定的:高度就是 CSS 說的那個數字,寬度照舊受 300px / 容器寬夾住。
      expect(m.img.h, `框高必須固定 —— 量到 ${JSON.stringify(m.img)}`).toBe(220);
      expect(m.img.w, `框寬不得溢出 —— 量到 ${JSON.stringify(m.img)}`).toBeLessThanOrEqual(
        Math.min(300, width),
      );
    });
  }
}

test("小圖不准被放大 —— 框照樣是固定的 220,圖照原尺寸畫在框裡", async ({
  mount,
  page,
}) => {
  const m = await heightAcrossLoad(mount, page, 390, { w: 80, h: 60 });

  expect(
    m.img.naturalH,
    `the image must actually have decoded (got ${JSON.stringify(m.img)})`,
  ).toBe(60);

  // 零回流那一半對小圖同樣要成立 —— 換 `object-fit` 不准動到預留的框。
  expect(
    m.after - m.before,
    `含小圖那一列的高度在載入前後必須完全一樣 —— 量到 ${JSON.stringify(m)}`,
  ).toBe(0);
  expect(
    m.afterAfterRowTop - m.beforeAfterRowTop,
    `下一列不准被推走 —— 量到 ${JSON.stringify(m)}`,
  ).toBe(0);
  expect(m.img.h, `框高必須固定 —— 量到 ${JSON.stringify(m.img)}`).toBe(220);

  const painted = await paintedSize(
    page,
    await m.cmp.getByTestId("chat-image").screenshot(),
  );
  expect(
    painted.w,
    `80x60 的小圖必須照原尺寸畫,不准放大填滿框 —— 畫出來是 ${JSON.stringify(painted)}`,
  ).toBeLessThanOrEqual(81);
  expect(
    painted.h,
    `80x60 的小圖必須照原尺寸畫,不准放大填滿框 —— 畫出來是 ${JSON.stringify(painted)}`,
  ).toBeLessThanOrEqual(61);
  expect(
    Math.min(painted.w, painted.h),
    `圖必須真的畫出來了,否則整條是空綠 —— 畫出來是 ${JSON.stringify(painted)}`,
  ).toBeGreaterThanOrEqual(59);
});

test("框的尺寸不歸主題管 —— 換一套 theme token,幾何逐位元相同", async ({
  mount,
  page,
}) => {
  // 「各 theme 都看過」的可執行版本:主題在這個 repo 是 CSS 變數層,而這一格的
  // 幾何裡沒有任何一個 token 參與。與其逐一截圖,不如把那句話直接斷言掉 ——
  // 哪天有人把框高改成一個 token,這條會紅。
  await page.setViewportSize({ width: 390, height: 844 });
  const cmp = await mount(<ChatImageSizeStory />);
  const read = () =>
    cmp.getByTestId("chat-image").evaluate((el) => {
      const r = el.getBoundingClientRect();
      return `${r.width.toFixed(2)}x${r.height.toFixed(2)}`;
    });
  const base = await read();
  // ⚠️ THE INJECTED `:root{…}` BLOCK IS THE WHOLE OF THE THEME SWAP HERE.
  // There is no `[data-theme]` selector anywhere in this tree (grep: zero
  // hits) — a theme in this repo is a set of custom-property VALUES, painted
  // onto `:root` by lib/themePaint, and `<html data-theme>` only names which
  // bundle the provider resolved. So stamping that attribute in this story
  // would change nothing and would make the spec read as if it had covered a
  // second theme; substituting the tokens is what actually exercises the claim.
  await page.evaluate(() => {
    const s = document.createElement("style");
    s.textContent = `:root{--color-overlay:#0ff;--color-text:#012;--color-bg:#fed;--color-accent:#f0f;--color-accent-cta-bg:#0f0;}`;
    document.head.appendChild(s);
  });
  expect(await read(), "換主題不准動到縮圖的幾何").toBe(base);
});
