// T-33 round 4 — 合併的單一入口（LorePendingSection），量出來的版本。
//
// owner 2026-09-05 逐字:「改成單一入口:只留一顆合併鈕,按了列出候選讓你挑,再
// 確認」。那一輪落地成三步:一列一顆鈕 → 列出候選、每個候選印它為什麼被判為相似
// → 確認,而且確認畫面明寫這個動作無法還原。
//
// WHY A REAL BROWSER: 這三件事全部是**版面**。今天守著它的只有
// LorePendingSection.test.tsx，那支跑在 jsdom 裡 —— jsdom 不解 flex、不解
// intrinsic min-width、offsetHeight 永遠是 0、@media 對不到 viewport。它證得到
// 那顆鈕、那五個候選、那段確認正文**被 render 了**，一個字都說不出它們**放不放
// 得下**。底下每一條斷言都是**量出來的矩形**（getBoundingClientRect ／
// scrollWidth-clientWidth），沒有一條可以被 class name 或一句 CSS 字串滿足。
//
// 這支蓋三格，每一格一顆 mutant（外加兩顆綠的，照實記在下面）:
//   ① 出口列上只有一顆合併鈕 —— 量鈕的矩形與出口列的高度。
//   ② 候選面板打開後五個候選各帶著理由，都放得下 —— 量每一列與每一段理由的矩形。
//   ③ 確認框的正文放得下，整個對話框留在畫面裡 —— 量正文與對話框的矩形。
//
// ─── MUTANT 表（每一顆只改一處，跑完從備份還原，不用 git checkout）────────
//
// M1（元件，第①格）LorePendingSection.tsx: 把「一列一顆合併鈕」換回 round 4 之
//   前的形狀 —— `row.similar.map(s => <button …>{t.lore.pendingMerge(s.canonical)}
//   </button>)`，每個候選各一顆。
//     320px  → 紅在 :139（本檔的斷言 (1)），逐字:
//       「出口列高 228px，而最高的一顆鈕是 56px（4.07 行）—— 鈕換行了，這一列上
//         不只一顆合併鈕」
//     1280px → 紅在 :158（本檔的斷言 (3)），逐字:
//       「出口列上有 6 顆鈕；應該只有核可與合併兩顆」
//     ⚠️ 桌機寬度下六顆鈕**不換行**，所以量高度的 (1) 在 1280px 是綠的 —— 那一
//     格靠的是數鈕的 (3)。兩條都是這一格自己的斷言，但它們的守備範圍不一樣，別
//     把 (1) 當成全寬度都成立。
//     ⚠️ 這顆 mutant 第一次跑的時候紅在前置行的 `toBeVisible()`（現在的 :114）（strict
//     mode violation: 5 elements），那條紅燈對底下量出來的東西零證據。所以那一行
//     改成 `.first()` —— 前置條件不該搶在被測的那一行前面炸。
//
// M2（CSS，第②格）lore.css `.lore-pending__candidate-reason`: `font-size: 0.85em`
//   → `font-size: 0`。四個 case 全紅，全紅在 :258（斷言 (2) 的理由寬度那一行），
//   逐字:
//     「第 1 個候選的理由「大小寫／符號正規化後完全相同」只有 0px 寬 —— 它被擠
//       掉了」
//   同一格另外兩顆（都紅，但紅在 :221 的面板溢出那一條，不是 :258）:
//     * `.lore-pending__candidate` 拿掉 `flex-wrap: wrap` → 320px 兩個 case 紅:
//       「候選面板橫向藏了 2px」（1280px 綠 —— 桌機寬度放得下）
//     * `.lore-pending__candidates` `flex-direction: column` → `row` → 320px 兩
//       個 case 紅:「候選面板橫向藏了 439px」（1280px 綠）
//
// M3（fixture，第③格）正文長度。**fixture 是我自己寫的，沒有任何東西規定它必須
//   是今天資料庫裡的長度**，所以「一段長到會爆的文字」就是這一格的 mutant:story
//   的 `bodyGrow` 把一段 impact 句型接在真正的正文後面（元件與 zh.ts 一個字都沒
//   動）。紅的時候逐字:
//     「對話框上緣在 -9.0625px（畫面外），正文 450 字、對話框高 586.125px，而畫
//       面只有 568px —— 正文開頭捲不回來」  ← :379，本檔的斷言 (3)
//
// 🔴 多長會紅？（遞增找出來的，不是估的）
//   今天線上的正文是 **207 字**。門檻按 viewport:
//     320×568（iPhone SE）: 448 字綠 → **450 字紅**（≈ 現況的 2.17 倍）
//     390×844            : 788 字綠 → **871 字紅**（≈ 4.2 倍）
//     1280×800           : 954 字綠 → **1037 字紅**（≈ 5.0 倍）
//   對照的餘裕（bodyGrow=0 量到的）: 對話框高 272.3 / 230.5 / 209.5px，上緣在
//   147.8 / 306.8 / 295.2px。
//   ⇒ 這條守的**確實是長度**，但門檻在最窄機型上是現況的兩倍出頭。之後那張票把
//   `problem` 換成 `impact` 句型，只要正文沒有翻倍，這條**不會**紅 —— 想早一點
//   叫，就把 CONFIRM_VIEWPORTS 加一格更矮的，或直接把 `BODY_GROW` 調到你要保證
//   的那個長度（旋鈕就在下面）。
//
// 🔴 綠掉的 mutant（照實記，不換一顆會紅的來湊）:
//   * confirm-modal.css `.confirm-modal__box` 拿掉 `flex-direction: column`
//     → 三個 viewport **全綠**（rc 0）。正文與按鈕列改成並排是一個真的畫面變
//     化，但三個 viewport 下都還放得下，所以這支守衛**不守對話框內部的堆疊方
//     向**。知道這件事比補一顆會紅的 mutant 有用。
import { test, expect } from "@playwright/experimental-ct-react";
import { LorePendingMergeStory } from "./stories/LorePendingMergeStory";

type Box = { x: number; y: number; w: number; h: number };

/** 從 live layout 讀一個元素的矩形 ＋ 它藏了多少（scroll vs client）。 */
async function rectOf(locator: import("@playwright/test").Locator) {
  return locator.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return {
      x: r.x,
      y: r.y,
      w: r.width,
      h: r.height,
      overX: el.scrollWidth - el.clientWidth,
      overY: el.scrollHeight - el.clientHeight,
      text: (el.textContent ?? "").trim(),
    };
  });
}

const right = (b: Box) => b.x + b.w;
const bottom = (b: Box) => b.y + b.h;

// ─────────────────────────────────────────────────────────────────────────────
// 第一格：一列上只有一顆合併鈕。
// ─────────────────────────────────────────────────────────────────────────────
const ROW_WIDTHS = [320, 1280];

for (const viewport of ROW_WIDTHS) {
  test(`${viewport}px: 待審列的出口是兩顆鈕排成一列，不是一排並列的合併鈕`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 800 });
    const cmp = await mount(<LorePendingMergeStory />);

    const row = cmp.getByTestId("lore-pending-row").first();
    // 🔴 `.first()` 是刻意的。這一行只是「面板已經畫出來了」的前置條件，不是
    // 這一格的斷言 —— 用嚴格 locator 的話，「每個候選各一顆鈕」的 mutant 會先
    // 在這裡炸成 strict mode violation，那條紅燈對底下量出來的那幾行零證據。
    await expect(
      row.getByTestId("lore-pending-merge-start").first(),
    ).toBeVisible();

    const actions = row.locator(".lore-pending__actions");
    const actionsBox = await rectOf(actions);
    const btnBoxes = await actions.locator("button").evaluateAll((els) =>
      els.map((el) => {
        const r = el.getBoundingClientRect();
        return { x: r.x, y: r.y, w: r.width, h: r.height };
      }),
    );
    expect(btnBoxes.length, "出口列上一顆鈕都沒有").toBeGreaterThan(0);
    const tallest = Math.max(...btnBoxes.map((b) => b.h));

    // (1) CORE red→green：出口列**只有一行高**。
    //
    // 這是「一列一顆合併鈕」量得到的形狀。`.lore-pending__actions` 是
    // `flex-wrap: wrap`，所以每個候選各掛一顆鈕的舊做法在窄寬度下不會溢出 ——
    // 它會**換行**，把那一列變成一面按鈕牆。一個 class name 或一句
    // `toBeVisible()` 對這件事零證據：六顆鈕全都 visible，全都掛著同一個
    // class。
    expect(
      actionsBox.h,
      `出口列高 ${actionsBox.h}px，而最高的一顆鈕是 ${tallest}px（${(
        actionsBox.h / tallest
      ).toFixed(2)} 行）—— 鈕換行了，這一列上不只一顆合併鈕`,
    ).toBeLessThanOrEqual(tallest * 1.4);

    // (2) 每一顆鈕的右緣都還在那一列裡面。沒有這一條，(1) 可以被「鈕沒換行、
    // 只是整排衝出卡片外面」滿足。
    const rowBox = await rectOf(row);
    for (const [i, b] of btnBoxes.entries()) {
      expect(
        right(b),
        `第 ${i + 1} 顆鈕的右緣在 ${right(b)}px，待審列的右緣在 ${right(
          rowBox,
        )}px —— 鈕衝出卡片外面了`,
      ).toBeLessThanOrEqual(right(rowBox) + 1);
    }

    // (3) 這一行才是「一顆」的字面意思：出口列上恰好兩顆鈕（核可＋合併），而
    // 且它們的矩形不重疊 —— 不是一顆蓋在另一顆上面裝成一顆。
    expect(
      btnBoxes.length,
      `出口列上有 ${btnBoxes.length} 顆鈕；應該只有核可與合併兩顆`,
    ).toBe(2);
    const [b0, b1] = btnBoxes;
    const overlapX = Math.min(right(b0), right(b1)) - Math.max(b0.x, b1.x);
    const overlapY = Math.min(bottom(b0), bottom(b1)) - Math.max(b0.y, b1.y);
    expect(
      Math.min(overlapX, overlapY),
      `兩顆鈕的矩形重疊了 ${overlapX}×${overlapY}px`,
    ).toBeLessThanOrEqual(0);

    // (4) 對照列：沒有候選的那一列**沒有**那一顆。這條在真的 mutant 下是綠的，
    // 它擋的是另一種修法 ——「把合併鈕從所有列上拿掉」也會讓 (1)(3) 變綠。
    const lonely = cmp.getByTestId("lore-pending-row").nth(1);
    await expect(lonely.getByTestId("lore-pending-merge-start")).toHaveCount(0);
    const lonelyBtns = await lonely
      .locator(".lore-pending__actions button")
      .evaluateAll((els) => els.length);
    expect(
      lonelyBtns,
      "沒有候選的那一列應該只剩核可一顆鈕",
    ).toBe(1);
  });
}

// ─────────────────────────────────────────────────────────────────────────────
// 第二格：候選面板打開後，五個候選各自帶著理由，而且放得下。
// ─────────────────────────────────────────────────────────────────────────────
const PICKER_CASES = [
  { name: "真名字", viewport: 320, longNames: false },
  { name: "真名字", viewport: 1280, longNames: false },
  { name: "長名字", viewport: 320, longNames: true },
  { name: "長名字", viewport: 1280, longNames: true },
];

for (const { name, viewport, longNames } of PICKER_CASES) {
  test(`${viewport}px / ${name}: 五個候選各自帶著理由，沒有被裁掉也沒有溢出面板`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<LorePendingMergeStory longNames={longNames} />);

    const row = cmp.getByTestId("lore-pending-row").first();
    await row.getByTestId("lore-pending-merge-start").click();
    const picker = row.getByTestId("lore-pending-merge-picker");
    await expect(picker).toBeVisible();

    const candidates = picker.getByTestId("lore-pending-merge-candidate");
    // 非空性：五種 reason 都要真的被排進去，否則底下量的是一個比實際短的面板。
    await expect(candidates).toHaveCount(5);

    const pickerBox = await rectOf(picker);
    const rowBox = await rectOf(row);

    // (1) 面板本身放得下：不比那一列寬，而且自己沒有藏東西。
    expect(
      right(pickerBox),
      `候選面板的右緣在 ${right(pickerBox)}px，待審列的右緣在 ${right(
        rowBox,
      )}px`,
    ).toBeLessThanOrEqual(right(rowBox) + 1);
    expect(
      pickerBox.overX,
      `候選面板橫向藏了 ${pickerBox.overX}px`,
    ).toBeLessThanOrEqual(1);

    // (2) CORE red→green：**每一個**候選列的矩形都在面板裡面、都沒有被裁掉，
    // 而且它自己那一段理由真的佔到寬度。
    //
    // 這是這一格存在的理由。`same_normalized` 幾乎一定是同一個東西，
    // `prefix` / `substring` 常常是兩個真的不同的名字 —— 理由那一段被裁掉、被
    // `overflow` 吃掉、或被擠成 0 寬，使用者就是在猜，而那顆鈕按下去不可逆。
    // jsdom 那支證得到理由**被 render**，證不到它**放得下**。
    const reasons: string[] = [];
    for (let i = 0; i < 5; i++) {
      const label = candidates.nth(i);
      const lb = await rectOf(label);
      const reason = label.locator(".lore-pending__candidate-reason");
      const rb = await rectOf(reason);
      reasons.push(rb.text);

      expect(lb.h, `第 ${i + 1} 個候選列高 0`).toBeGreaterThan(0);
      expect(
        right(lb),
        `第 ${i + 1} 個候選列的右緣在 ${right(lb)}px，面板右緣在 ${right(
          pickerBox,
        )}px —— 候選溢出面板`,
      ).toBeLessThanOrEqual(right(pickerBox) + 1);
      expect(
        lb.overX,
        `第 ${i + 1} 個候選列橫向被裁掉 ${lb.overX}px`,
      ).toBeLessThanOrEqual(1);
      expect(
        lb.overY,
        `第 ${i + 1} 個候選列縱向被裁掉 ${lb.overY}px`,
      ).toBeLessThanOrEqual(1);

      // 理由那一段：不是 0 寬、沒有被裁、右緣還在面板裡面。
      expect(
        rb.w,
        `第 ${i + 1} 個候選的理由「${rb.text}」只有 ${rb.w}px 寬 —— 它被擠掉了`,
      ).toBeGreaterThan(24);
      expect(
        rb.overX,
        `第 ${i + 1} 個候選的理由橫向被裁掉 ${rb.overX}px（「${rb.text}」）`,
      ).toBeLessThanOrEqual(1);
      expect(
        rb.overY,
        `第 ${i + 1} 個候選的理由縱向被裁掉 ${rb.overY}px（「${rb.text}」）`,
      ).toBeLessThanOrEqual(1);
      expect(
        right(rb),
        `第 ${i + 1} 個候選的理由右緣在 ${right(rb)}px，面板右緣在 ${right(
          pickerBox,
        )}px`,
      ).toBeLessThanOrEqual(right(pickerBox) + 1);
    }

    // (3) 非空性：五段理由互不相同。五個候選印同一句話也會讓 (2) 全綠，而那時
    // 「每個候選印它為什麼被判為相似」就是假的。
    expect(
      new Set(reasons).size,
      `五個候選只印出 ${new Set(reasons).size} 種理由：${reasons.join(" / ")}`,
    ).toBe(5);

    // (4) 整頁不橫向平移 —— 溢出的寬度總得落在某個祖先身上。
    const pageOver = await page.evaluate(
      () =>
        document.scrollingElement!.scrollWidth -
        document.scrollingElement!.clientWidth,
    );
    expect(pageOver, "整頁橫向平移了").toBeLessThanOrEqual(1);
  });
}

// ─────────────────────────────────────────────────────────────────────────────
// 第三格：確認框的正文放得下。
// ─────────────────────────────────────────────────────────────────────────────
const CONFIRM_VIEWPORTS = [
  { w: 320, h: 568 }, // iPhone SE：這個座艙支援的最窄／最矮的真機
  { w: 390, h: 844 },
  { w: 1280, h: 800 },
];

/** 正文成長階梯。0 ＝ 今天線上的字。其餘幾格是 story 把一段 impact 句型接在後
 * 面 —— 之後那張票要換上去的就是這種句子。門檻寫在檔頭。 */
const BODY_GROW = 0;

for (const { w, h } of CONFIRM_VIEWPORTS) {
  test(`${w}x${h}: 確認框的正文放得下，而且整個對話框留在畫面裡`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: w, height: h });
    const cmp = await mount(<LorePendingMergeStory bodyGrow={BODY_GROW} />);

    const row = cmp.getByTestId("lore-pending-row").first();
    await row.getByTestId("lore-pending-merge-start").click();
    await row
      .getByTestId("lore-pending-merge-candidate")
      .first()
      .locator("input[type=radio]")
      .check();
    await row.getByTestId("lore-pending-merge-next").click();

    const dialog = page.getByTestId("lore-pending-merge-confirm");
    await expect(dialog).toBeVisible();
    const box = dialog.locator(".confirm-modal__box");
    const body = page.getByTestId("lore-pending-merge-confirm-body");

    const boxBox = await rectOf(box);
    const bodyBox = await rectOf(body);
    const overlayBox = await rectOf(dialog);

    // 非空性：這一格量的是「一段長正文」。正文短到只有一兩行的時候，底下每一
    // 條都會因為太寬鬆而變成恆真。
    expect(
      bodyBox.text.length,
      `正文只有 ${bodyBox.text.length} 個字 —— 太短，底下的斷言等於恆真`,
    ).toBeGreaterThan(90);

    // (1) CORE red→green：正文**沒有被截斷**。整段字的自然高度不超過它自己的
    // 盒子。jsdom 這裡永遠是 0 vs 0，所以那支測試只證得到字被 render 了。
    expect(
      bodyBox.overY,
      `確認框正文被截掉了 ${bodyBox.overY}px（scrollHeight 比 clientHeight 多這麼多）` +
        `，正文長 ${bodyBox.text.length} 字，盒子高 ${bodyBox.h}px`,
    ).toBeLessThanOrEqual(1);
    expect(
      bodyBox.overX,
      `確認框正文橫向被截掉了 ${bodyBox.overX}px`,
    ).toBeLessThanOrEqual(1);

    // (2) 正文的矩形在對話框裡面 —— 上下左右四邊都量。
    expect(
      bodyBox.x,
      `正文左緣 ${bodyBox.x}px 在對話框左緣 ${boxBox.x}px 外面`,
    ).toBeGreaterThanOrEqual(boxBox.x - 1);
    expect(
      right(bodyBox),
      `正文右緣 ${right(bodyBox)}px 在對話框右緣 ${right(boxBox)}px 外面`,
    ).toBeLessThanOrEqual(right(boxBox) + 1);
    expect(
      bodyBox.y,
      `正文上緣 ${bodyBox.y}px 在對話框上緣 ${boxBox.y}px 外面`,
    ).toBeGreaterThanOrEqual(boxBox.y - 1);
    expect(
      bottom(bodyBox),
      `正文下緣 ${bottom(bodyBox)}px 在對話框下緣 ${bottom(boxBox)}px 外面`,
    ).toBeLessThanOrEqual(bottom(boxBox) + 1);

    // (3) 🔴 整個對話框留在畫面裡，**上緣也要**。
    //
    // 這一條是為「正文之後會變長」寫的。`.confirm-modal` 是
    // `display:flex; align-items:center` 而且沒有 `overflow:auto` —— 內容一旦
    // 超過畫面高度，盒子會**往上下兩邊同時溢出**，而上面那一截捲不回來：正文
    // 的第一句就永遠讀不到了。所以下緣與上緣要分開量，只量下緣會漏掉真正致命
    // 的那一半。
    expect(
      boxBox.y,
      `對話框上緣在 ${boxBox.y}px（畫面外），正文 ${bodyBox.text.length} 字、` +
        `對話框高 ${boxBox.h}px，而畫面只有 ${h}px —— 正文開頭捲不回來`,
    ).toBeGreaterThanOrEqual(-1);
    expect(
      bottom(boxBox),
      `對話框下緣在 ${bottom(boxBox)}px，畫面只有 ${h}px —— 確認鈕被推出畫面`,
    ).toBeLessThanOrEqual(h + 1);
    expect(
      overlayBox.overY,
      `遮罩層縱向藏了 ${overlayBox.overY}px`,
    ).toBeLessThanOrEqual(1);

    // (4) 確認鈕的矩形本身也要在畫面裡 —— 一個讀得到但按不到的確認，跟沒有這
    // 一步是一樣的。
    const ok = await rectOf(page.getByTestId("lore-pending-merge-confirm-ok"));
    expect(ok.w, "確認鈕 0 寬").toBeGreaterThan(0);
    expect(bottom(ok), `確認鈕下緣在 ${bottom(ok)}px，畫面 ${h}px`).toBeLessThanOrEqual(
      h + 1,
    );
    expect(ok.y, `確認鈕上緣在 ${ok.y}px`).toBeGreaterThanOrEqual(-1);
  });
}
