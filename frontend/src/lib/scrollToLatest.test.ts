// ③ 跳到**最新那一則**,不是第一則未讀。
//
// 舊的「有新訊息」chip 捲到 `newMsgAnchorId`(第一則未見),十則湧進來就把讀者留在
// 第 1 則、下面還壓著五則。divider 標的是未讀區的**開頭**,跳轉要的是**結尾**。
//
// ⚠️ 這支原本還有第二半:一個 2.6s 的 ResizeObserver 修正迴圈,守著「圖片解到真
// 高度、卡片補撈完把目標推出視窗」。owner 在 rc-6c27f486ef9d 圈了「拿掉。圖片／
// 卡片展開把目標擠走我接受」,那半連同守它的兩條測試一起刪掉了。這裡不再有計時器、
// 沒有 observer、沒有 disposer —— 一次捲動,就這樣。
import { describe, it, expect } from "vitest";
import { scrollToLatest } from "./scrollToLatest";

function mkScroller(ids: string[]) {
  const scroller = document.createElement("div");
  const calls: Array<{ id: string | null; args: unknown }> = [];
  for (const id of ids) {
    const row = document.createElement("div");
    row.setAttribute("data-msg-id", id);
    row.scrollIntoView = ((args: unknown) => {
      calls.push({ id: row.getAttribute("data-msg-id"), args });
    }) as Element["scrollIntoView"];
    scroller.appendChild(row);
  }
  return { scroller, calls };
}

describe("scrollToLatest", () => {
  it("捲到最後一則，不是第一則未讀 —— 這正是舊 chip 的 bug", () => {
    const { scroller, calls } = mkScroller(["c1", "c2", "c3"]);
    scrollToLatest(scroller);
    expect(calls).toHaveLength(1);
    expect(calls[0].id).toBe("c3");
  });

  it("不是 smooth —— 動畫會被每一次 reflow 打斷重來，看起來像畫面在抽動", () => {
    const { scroller, calls } = mkScroller(["c1"]);
    scrollToLatest(scroller);
    expect(calls[0].args).toEqual({ block: "end" });
  });

  it("沒有任何訊息列時什麼都不做", () => {
    const scroller = document.createElement("div");
    expect(() => scrollToLatest(scroller)).not.toThrow();
  });
});
