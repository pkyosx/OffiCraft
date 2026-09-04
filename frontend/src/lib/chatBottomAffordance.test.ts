// 兩個底部元件的互斥規則。owner 的裁定是「有新訊息就不用出現向下箭頭」，
// 也就是三態而不是兩個各自獨立的布林值 —— 這支測試釘的就是「三態」本身。
import { describe, it, expect } from "vitest";
import { chatBottomAffordance } from "./chatBottomAffordance";

describe("chatBottomAffordance", () => {
  it("在底部而且沒有新訊息時什麼都不出現", () => {
    expect(
      chatBottomAffordance({
        latestInView: true,
        hasNewMsgPreview: false,
        windowHasNewer: false,
      }),
    ).toBe("none");
  });

  it("不在底部但沒有新訊息時出箭頭 —— 條件是「最新那一則不在視窗內」，不是「有新訊息」", () => {
    expect(
      chatBottomAffordance({
        latestInView: false,
        hasNewMsgPreview: false,
        windowHasNewer: false,
      }),
    ).toBe("arrow");
  });

  it("有新訊息時一律是預覽列，箭頭讓位", () => {
    expect(
      chatBottomAffordance({
        latestInView: false,
        hasNewMsgPreview: true,
        windowHasNewer: false,
      }),
    ).toBe("preview");
  });

  it("有新訊息但人已經在底部時仍然只有預覽列 —— 這格是互斥最容易寫壞的一格", () => {
    // 這個狀態在 <ChatArea> 裡短暫存在（捲到底的那一幀，strip 還沒被清掉）。
    // 把箭頭的條件寫成 `!latestInView`（最自然、也最錯的寫法）在這一格看不出來，
    // 但在上一格會同時畫出兩個元件。兩格一起才把那個 mutant 圍住。
    expect(
      chatBottomAffordance({
        latestInView: true,
        hasNewMsgPreview: true,
        windowHasNewer: false,
      }),
    ).toBe("preview");
  });

  it("捲到「跳到原訊息」開出來的那個視窗底部時，箭頭仍然要在 —— 那一則不是最新的", () => {
    // T-48 ③：跳到一則很舊的訊息之後，畫面上載的是歷史中的一段視窗。
    // 捲到那段的底部，`latestInView` 這個純幾何事實是 true，但最新那一則還在
    // 下面沒被撈進來。少了 windowHasNewer，箭頭會剛好在最需要它的地方消失。
    expect(
      chatBottomAffordance({
        latestInView: true,
        hasNewMsgPreview: false,
        windowHasNewer: true,
      }),
    ).toBe("arrow");
  });
});
