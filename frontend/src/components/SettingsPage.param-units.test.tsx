// T-d05a · 參數列單位的「性質」護欄（不是文案測試）。
//
// owner 的裁定：不要斷言「這一格的字等於『次』」——那種測試只會把今天的字面
// 抄一份到測試裡，改字就紅、漏字卻不一定紅。這裡釘的是兩條性質：
//
//   1) 每一個帶數字輸入的參數列（.param-pct）都必須渲染得出一個單位標記
//      （.param-pct__sign），而且它不是空字串。漏字 ⇒ 紅。
//   2) 那個單位是「從 i18n 解出來的」——用真的英文字典渲染時，任何單位格
//      裡都不准出現 CJK 字元。硬編中文 ⇒ 紅。
//
// 兩條合起來就是這張票要守的洞：漏單位、以及補了單位卻寫死中文。

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";

/** 任何漢字/假名區段——單位格裡出現這些就代表沒走翻譯。 */
const CJK = /[　-〿぀-ヿ㐀-䶿一-鿿豈-﫿]/;

/** 目前設定頁的分母：帶數字輸入 ⇒ 帶單位的參數列共 11 列
 *  （Claude 通知/換手 2 + Codex 通知/回合 2 + 監控刷新 1 + 文件上限 5 + 聊天預算 1）。
 *  這個數字寫死是刻意的：新增一列忘了給單位時，這條會先紅。 */
const UNIT_ROWS = 11;

async function openParams(dict: typeof zh) {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByLabelText(dict.settings.sessionTtl); // settings loaded
  return utils;
}

beforeEach(() => {
  __resetMock();
});
afterEach(() => {
  window.localStorage.removeItem("oc.language");
});

describe("SettingsPage · 參數列單位（性質）", () => {
  it("每一個帶輸入框的參數列都渲染得出一個非空的單位標記", async () => {
    const utils = await openParams(zh);
    const cells = Array.from(
      utils.container.querySelectorAll<HTMLElement>(".param-pct")
    );
    expect(cells.length).toBe(UNIT_ROWS);

    const missing = cells
      .map((cell, i) => {
        const sign = cell.querySelector<HTMLElement>(".param-pct__sign");
        const label =
          cell
            .closest(".param-row")
            ?.querySelector(".param-row__name")?.textContent?.trim() ?? `#${i}`;
        return sign && (sign.textContent ?? "").trim() !== "" ? null : label;
      })
      .filter((v): v is string => v !== null);

    expect(missing).toEqual([]);
  });

  it("單位字是從 i18n 解出來的——英文介面下沒有任何單位格是中文", async () => {
    // 語言偏好存在 localStorage，provider 掛載時讀它，所以這裡渲染的是「真的
    // 英文字典」，而不是拿 key 對 key 自我證明。
    window.localStorage.setItem("oc.language", "en");
    const utils = await openParams(en);
    const signs = Array.from(
      utils.container.querySelectorAll<HTMLElement>(".param-pct__sign")
    );
    expect(signs.length).toBe(UNIT_ROWS);

    const hardcoded = signs
      .map((s) => (s.textContent ?? "").trim())
      .filter((text) => CJK.test(text));

    expect(hardcoded).toEqual([]);
  });
});
