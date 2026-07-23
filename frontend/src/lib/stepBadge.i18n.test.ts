// stepBadge × i18n 枚舉守衛(T-6f11)。
//
// Owner bug:座艙 step 狀態徽章曾漏出原始英文值(waiting_external),任務層
// 卻正常顯「等待外部」——根因是三語 stepStatus map 都缺 waiting_external 這個
// key,而 render site 的 map-miss fallback 直印 raw key;resolver 的特殊徽章
// 分支只是恰好遮住唯一 render site,一旦分支被改動/新增狀態就漏。這檔把
// 「每個 step 狀態值在每個語系都有翻譯」釘成紅燈條件:
//
//   ① 枚舉 STEP_STATUSES(closed set 的唯一出處,lib/stepBadge.ts)× 三語
//      locales:tasks.stepStatus[status] 必須存在、非空、且不等於 raw key。
//      新增狀態忘記補翻譯 → 這裡先紅,raw key 進不了座艙。
//   ② waiting_external 三處同詞:step map 項 == 特殊徽章 stepWaitingExternal
//      == 任務層 tasks.status.waiting_external —— 兩層(任務/步驟)與兩條
//      render 路徑(特殊徽章/後防 map)永遠讀成同一個詞。
//   ③ resolver 全輸入空間掃描:status × isGate × replyCardId ×
//      replyCardStatus 每種組合的徽章,經三語字典解出的文字都不得長得像
//      raw key(^[a-z_]+$)——特殊徽章 key(gateAnnounced 等)缺譯也會在
//      這裡紅,不只 stepStatus map。
import { describe, it, expect } from "vitest";
import { resolveStepBadge, STEP_STATUSES, type StepBadge } from "./stepBadge";
import { zh, type Dict } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";

const DICTS: Record<string, Dict> = { zh, en };

/** The exact text TaskCard.renderStepBadge shows for a resolved badge. */
function badgeText(t: Dict, badge: StepBadge): string {
  switch (badge.kind) {
    case "gate-announced":
      return t.tasks.gateAnnounced;
    case "card-answered":
      return t.tasks.stepCardAnswered;
    case "card-expired":
      return t.tasks.stepCardExpired;
    case "waiting-external":
      return t.tasks.stepWaitingExternal;
    default:
      return t.tasks.stepStatus[badge.status] ?? badge.status;
  }
}

/** A raw status key: lowercase snake_case — what the cockpit must never show. */
const RAW_KEY = /^[a-z_]+$/;

describe("step 狀態 i18n 枚舉守衛 (T-6f11)", () => {
  it("每個 step 狀態值在三語 stepStatus map 都有翻譯(非 raw key)", () => {
    for (const [name, t] of Object.entries(DICTS)) {
      for (const status of STEP_STATUSES) {
        const text = t.tasks.stepStatus[status];
        expect(text, `${name}.tasks.stepStatus.${status} 缺翻譯`).toBeTruthy();
        expect(
          text,
          `${name}.tasks.stepStatus.${status} 是 raw key`
        ).not.toMatch(RAW_KEY);
      }
    }
  });

  it("waiting_external 三處同詞:step map == stepWaitingExternal == 任務層", () => {
    for (const [name, t] of Object.entries(DICTS)) {
      expect(
        t.tasks.stepStatus.waiting_external,
        `${name}: step map 與特殊徽章不同詞`
      ).toBe(t.tasks.stepWaitingExternal);
      expect(
        t.tasks.stepStatus.waiting_external,
        `${name}: step 層與任務層不同詞`
      ).toBe(t.tasks.status.waiting_external);
    }
  });

  it("resolver 全輸入空間 × 三語:沒有任何組合渲染出 raw key", () => {
    for (const status of STEP_STATUSES) {
      for (const isGate of [false, true]) {
        for (const replyCardId of ["", "rc-1"]) {
          for (const replyCardStatus of [
            null,
            "waiting",
            "answered",
            "expired",
          ] as const) {
            const badge = resolveStepBadge({
              status,
              isGate,
              replyCardId,
              replyCardStatus,
            });
            for (const [name, t] of Object.entries(DICTS)) {
              const text = badgeText(t, badge);
              expect(
                text,
                `${name} status=${status} gate=${isGate} card=${replyCardId} ` +
                  `rcs=${replyCardStatus} kind=${badge.kind} 渲染出 raw key`
              ).not.toMatch(RAW_KEY);
            }
          }
        }
      }
    }
  });

  it("waiting_external step 走自己的特殊徽章(非 gate、無卡)——語意零回歸", () => {
    expect(
      resolveStepBadge({
        status: "waiting_external",
        isGate: false,
        replyCardId: "",
        replyCardStatus: null,
      })
    ).toEqual({ kind: "waiting-external" });
  });
});
