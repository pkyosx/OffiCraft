// compose.test.ts — T-081b lane B guard: the parameterised messages now assemble
// themselves from OVERRIDABLE static fragments, and the words on screen must not
// have moved a single byte while doing it.
//
// EXPECTED is the exact rendering each message produced BEFORE the split, in
// both languages (lifted from the pre-change interpolation templates). It is the
// only thing standing between a fragment-and-join refactor and a silent copy
// change, so it is written out verbatim rather than derived.

import { describe, it, expect } from "vitest";
import { zh, type Dict } from "./locales/zh";
import { en } from "./locales/en";
import { makeMessages, effortText, type LoreBudgetSentence } from "./compose";
import { applyWording } from "./wording";
import { MESSAGE_KEYS } from "./messageKeys.generated";
import type { ActivityWait, Effort, MemberActivity } from "../types";
import type { DueRemaining } from "../lib/dueDate";

type Lang = "zh" | "en";
const DICTS: Record<Lang, Dict> = { zh, en };

/** The FULL sets, spelled out rather than abbreviated: "every day" on this wire
 * IS the list of every day, and the summary's whole job is to say so in one
 * phrase instead of 31 numbers. */
const ALL_MONTHS = Array.from({ length: 12 }, (_, i) => i + 1);
const ALL_DAYS = Array.from({ length: 31 }, (_, i) => i + 1);
const ALL_HOURS = Array.from({ length: 24 }, (_, i) => i);
const ALL_MINUTES = Array.from({ length: 60 }, (_, i) => i);
/** What the minute group's 全選 now hands over: the twelve cells it offers. */
const EVERY_FIVE = Array.from({ length: 12 }, (_, i) => i * 5);

/** Local-time epoch seconds (month 1-based), so the pins hold in any timezone. */
const at = (y: number, mo: number, d: number, h: number, mi: number): number =>
  new Date(y, mo - 1, d, h, mi, 0, 0).getTime() / 1000;
/** The reader's clock for the reset-time rows: 2026-10-04 10:00. */
const NOW = at(2026, 10, 4, 10, 0);

/** The reader's clock for the activity rows. */
const T = 1_800_000_000;
const act = (over: Partial<MemberActivity>): MemberActivity => ({
  state: "busy",
  busySince: null,
  subagents: [],
  waits: [],
  ...over,
});
const sub = (name: string, description: string, startedTs: number) => ({
  id: `${name}-${startedTs}`,
  parentId: null,
  name,
  description,
  startedTs,
});
const SUBS_4 = [sub("a", "", T), sub("b", "", T), sub("c", "", T), sub("d", "", T)];
const WAIT_BG = { kind: "background_command" as const, description: "npm test", nextTs: null, recurring: false };
const WAIT_MON = { kind: "monitor" as const, description: "watch the deploy log", nextTs: null, recurring: false };
const WAIT_CRON = { kind: "schedule" as const, description: "每小時看 CI", nextTs: null, recurring: false };
const NEXT = at(2026, 10, 4, 12, 15);
const cron = (nextTs: number | null, recurring: boolean) => ({ ...WAIT_CRON, nextTs, recurring });

type Arg =
  | string
  | number
  | string[]
  | number[]
  | null
  | MemberActivity
  | ActivityWait
  | DueRemaining
  | LoreBudgetSentence
  | { id: string; reason: string }[];
const MEMBER_DROPPING: LoreBudgetSentence = {
  capChars: 10000,
  usedChars: 9640,
  ownName: "小安",
  everyone: { count: 14, chars: 6240, fitted: 14, fittedChars: 6240 },
  own: { count: 11, fitted: 7, fittedChars: 3400 },
  firstDroppedId: "L-212",
  firstDroppedChars: 520,
};
const MEMBER_EVERYONE_OVER: LoreBudgetSentence = {
  capChars: 10000,
  usedChars: 9800,
  ownName: "小安",
  everyone: { count: 20, chars: 12000, fitted: 14, fittedChars: 9800 },
  own: { count: 7, fitted: 0, fittedChars: 0 },
  firstDroppedId: "L-212",
  firstDroppedChars: 520,
};
const MANUAL_FITS: LoreBudgetSentence = {
  capChars: 10000,
  usedChars: 3210,
  ownName: "review-pr",
  everyone: null,
  own: { count: 8, fitted: 8, fittedChars: 3210 },
  firstDroppedId: "",
  firstDroppedChars: 0,
};
/** [language, message, arguments, the text that must appear on screen]. */
const EXPECTED: [Lang, string, Arg[], string | string[]][] = [
    ["zh", "memberActivitySummary", [act({ busySince: T - 180 })], "主代理工作中"],
    ["en", "memberActivitySummary", [act({ busySince: T - 180 })], "Main agent working"],
    ["zh", "memberActivitySummary", [act({ busySince: T - 180, subagents: SUBS_4, waits: [WAIT_BG, WAIT_CRON] })], "主代理工作中，另有 4 個子代理工作中，正在等 2 個背景檢查或排程"],
    ["en", "memberActivitySummary", [act({ busySince: T - 180, subagents: SUBS_4, waits: [WAIT_BG, WAIT_CRON] })], "Main agent working, plus 4 sub-agents working, waiting on 2 background checks or schedules"],
    ["zh", "memberActivityDialogTitle", ["Mira"], "Mira · 運行狀況"],
    ["en", "memberActivityDialogTitle", ["Mira"], "Mira · Runtime"],
    ["zh", "memberActivityOpenLabel", ["busy"], "工作中，查看運行狀況"],
    ["en", "memberActivityOpenLabel", ["waiting"], "Waiting, see runtime details"],
    ["zh", "memberActivitySummary", [act({ busySince: T - 180, subagents: SUBS_4.slice(0, 1) })], "主代理工作中，另有 1 個子代理工作中"],
    ["en", "memberActivitySummary", [act({ busySince: T - 180, subagents: SUBS_4.slice(0, 1) })], "Main agent working, plus 1 sub-agent working"],
    ["zh", "memberActivitySummary", [act({ busySince: T - 180, waits: [WAIT_BG] })], "主代理工作中，正在等 1 個背景檢查或排程"],
    ["en", "memberActivitySummary", [act({ busySince: T - 180, waits: [WAIT_BG] })], "Main agent working, waiting on 1 background check or schedule"],
    ["zh", "memberActivitySummary", [act({ subagents: SUBS_4.slice(0, 2) })], "2 個子代理工作中"],
    ["en", "memberActivitySummary", [act({ subagents: SUBS_4.slice(0, 2) })], "2 sub-agents working"],
    ["en", "memberActivitySummary", [act({ subagents: SUBS_4.slice(0, 1) })], "1 sub-agent working"],
    ["zh", "memberActivitySummary", [act({ subagents: SUBS_4.slice(0, 1) })], "1 個子代理工作中"],
    ["zh", "memberActivitySummary", [act({ subagents: SUBS_4.slice(0, 2), waits: [WAIT_CRON] })], "2 個子代理工作中，正在等 1 個背景檢查或排程"],
    ["en", "memberActivitySummary", [act({ subagents: SUBS_4.slice(0, 2), waits: [WAIT_CRON] })], "2 sub-agents working, waiting on 1 background check or schedule"],
    ["zh", "memberActivitySummary", [act({ state: "waiting", waits: [WAIT_BG, WAIT_CRON, WAIT_MON] })], "正在等 3 個背景檢查或排程"],
    ["en", "memberActivitySummary", [act({ state: "waiting", waits: [WAIT_BG, WAIT_CRON, WAIT_MON] })], "Waiting on 3 background checks or schedules"],
    ["zh", "memberActivitySummary", [act({ state: "idle" })], "閒置"],
    ["zh", "memberActivitySince", [T - 180, T], "已 3 分鐘"],
    ["zh", "memberActivitySince", [T - 40, T], "已 40 秒"],
    ["en", "memberActivitySince", [T - 180, T], "for 3 min"],
    ["zh", "memberActivityDuration", [T - 40, T], "40 秒"],
    ["zh", "memberActivityDuration", [T - 125, T], "2 分鐘"],
    ["en", "memberActivityDuration", [T - 40, T], "40 sec"],
    ["zh", "memberActivityWaitTag", [WAIT_BG, NOW], "背景指令"],
    ["en", "memberActivityWaitTag", [WAIT_BG, NOW], "background command"],
    ["zh", "memberActivityWaitTag", [WAIT_MON, NOW], "監看"],
    ["en", "memberActivityWaitTag", [WAIT_MON, NOW], "monitor"],
    ["zh", "memberActivityWaitTag", [cron(NEXT, false), NOW], "排程 · 下次 12:15"],
    ["zh", "memberActivityWaitTag", [cron(NEXT, true), NOW], "排程 · 下次 12:15 · 重複"],
    ["zh", "memberActivityWaitTag", [cron(at(2026, 10, 4, 23, 59), false), NOW], "排程 · 下次 23:59"],
    ["zh", "memberActivityWaitTag", [cron(at(2026, 10, 5, 0, 0), false), NOW], "排程 · 下次 10/5 00:00"],
    ["zh", "memberActivityWaitTag", [cron(at(2026, 10, 11, 9, 5), true), NOW], "排程 · 下次 10/11 09:05 · 重複"],
    ["zh", "memberActivityWaitTag", [cron(at(2027, 1, 2, 9, 5), true), NOW], "排程 · 下次 2027/1/2 09:05 · 重複"],
    ["zh", "memberActivityWaitTag", [cron(null, false), NOW], "排程"],
    ["zh", "memberActivityWaitTag", [cron(null, true), NOW], "排程 · 重複"],
    ["en", "memberActivityWaitTag", [cron(NEXT, true), NOW], "schedule · next 12:15 · repeats"],
    ["en", "memberActivityWaitTag", [cron(at(2026, 10, 5, 9, 5), true), NOW], "schedule · next 10/5 09:05 · repeats"],
    ["en", "memberActivitySummary", [act({ state: "idle" })], "Idle"],
    // The 429 credential brake. Pinned in BOTH languages because the space
    // after the number is language-specific and is the opposite of `sp`: zh
    // wants it, en does not. Getting that backwards yields "請於 42秒後再試。"
    // or "in 42 s." — which typecheck and the drift gate both accept happily.
    ["zh", "loginThrottled", [42], "目前同時處理的登入太多，請於 42 秒後再試。"],
    ["en", "loginThrottled", [42], "Too many logins in flight. Try again in 42s."],
    ["zh", "passkeyIpHint", ["http://localhost:8787"], "要用通行密鑰，請改用 http://localhost:8787 開啟"],
    ["en", "passkeyIpHint", ["http://localhost:8787"], "To use a passkey, open http://localhost:8787 instead"],
    ["zh", "taskProgress", [3,7], "步驟 3/7"],
    ["zh", "taskElapsed", ["2 小時"], "已歷時 2 小時"],
    ["zh", "taskPlanningBy", ["Mira"], "等待 Mira 建立 Steps"],
    ["zh", "taskBlockedBy", ["T-1234"], "等 T-1234"],
    ["zh", "taskBlockedByMissing", ["T-dead"], "等 T-dead(查無此任務)"],
    ["zh", "taskCopyTaskNo", ["T-1234"], "複製任務編號 T-1234"],
    ["zh", "taskTerminateConfirmBody", ["修理電梯"], "確定要終止「修理電梯」嗎？任務將結案為終止，無法恢復；負責人會收到通知並收尾。"],
    ["zh", "taskMarkDuplicateBody", ["T-1234"], "把「T-1234」標記為某張原票的重複?任務將結案為重複、無法恢復。請選擇原票:"],
    ["zh", "taskDuplicateOf", ["T-9999"], "重複於 T-9999"],
    ["zh", "taskReassignTitle", ["T-1234"], "轉派 T-1234"],
    ["zh", "taskDueDate", [1793375999], "2026/10/30（五）"],
    ["zh", "taskDueDateTime", [1792650600], "2026/10/22（四） 14:30"],
    ["zh", "taskOverdueDays", [1], "已逾期 1 天"],
    ["zh", "taskOverdueDays", [2], "已逾期 2 天"],
    ["zh", "taskDueRemaining", [{ overdue: false, amount: 5, unit: "day" }], "還有 5 天"],
    ["zh", "taskDueRemaining", [{ overdue: false, amount: 19, unit: "hour" }], "還有 19 小時"],
    ["zh", "taskDueRemaining", [{ overdue: false, amount: 25, unit: "minute" }], "還有 25 分鐘"],
    ["zh", "taskDueRemaining", [{ overdue: true, amount: 2, unit: "day" }], "已逾期 2 天"],
    ["zh", "taskDueRemaining", [{ overdue: true, amount: 3, unit: "hour" }], "已逾期 3 小時"],
    ["zh", "taskDueLead", [1440], "1 天"],
    ["zh", "taskDueLead", [120], "2 小時"],
    ["zh", "taskDueLead", [90], "90 分鐘"],
    ["zh", "taskDueLeadWorkspace", [1440], "全站設定 1 天"],
    ["zh", "taskDueLeadWorkspace", [120], "全站設定 2 小時"],
    ["zh", "taskCountSummary", [2, 1, 12], "已逾期 2 · 即將到期 1 · 進行中 12"],
    ["zh", "taskCountSummary", [3, 0, 4], "已逾期 3 · 進行中 4"],
    ["zh", "taskCountSummary", [0, 0, 4], "進行中 4"],
    // T-60 — the artifact version reader's four. The count is the `sp` case
    // again (zh runs the characters together, en needs the space), and it is
    // only ever printed for n > 1, so there is no singular rendering to pin.
    ["zh", "taskArtifactVersionCount", [3], "3版"],
    ["en", "taskArtifactVersionCount", [3], "3 versions"],
    ["zh", "taskArtifactVersionLabel", ["7/13 09:05"], "版本 7/13 09:05"],
    ["en", "taskArtifactVersionLabel", ["7/13 09:05"], "Version 7/13 09:05"],
    ["zh", "taskArtifactVersionBy", ["mira"], "修改者 mira"],
    ["en", "taskArtifactVersionBy", ["mira"], "by mira"],
    ["zh", "taskArtifactOpaque", ["image/png"], "這不是文字檔(image/png),只能切換前後各看一次。"],
    ["en", "taskArtifactOpaque", ["image/png"], "Not a text file (image/png) — look at the two versions one at a time instead."],
    ["zh", "replyWaited", ["3 小時"], "已等你 3 小時"],
    ["zh", "replyWaitedFull", [23 * 3600 + 59 * 60], "已等你 23 小時 59 分"],
    ["zh", "replyWaitedFull", [3 * 86400 + 4 * 3600 + 5 * 60 + 59], "已等你 3 天 4 小時 5 分"],
    ["zh", "replyWaitedFull", [10], "已等你 1 分"],
    ["zh", "replyOpenedAt", ["7/13 09:05"], "開卡 7/13 09:05"],
    ["zh", "replyAnsweredAt", ["7/13 09:05"], "已回覆 7/13 09:05"],
    ["zh", "replyExpiredAt", ["7/13 09:05"], "已過期 7/13 09:05"],
    ["zh", "replyExpireConfirmBody", ["要不要買新的"], "要把「要不要買新的」標為過期嗎?此動作不可復原、也不算回答——成員會收到通知,問題還在的話他會重新開一張新卡。"],
    ["zh", "replySelectedCount", [0], "已選 0 項"],
    ["zh", "replySelectedCount", [1], "已選 1 項"],
    ["zh", "replySelectedCount", [2], "已選 2 項"],
    ["zh", "replyPickedOptions", [["寄出"]], "寄出"],
    ["zh", "replyPickedOptions", [["寄出", "先不要"]], "寄出、先不要"],
    ["zh", "replySnoozeIn", [1, "hour"], "1 小時後"],
    ["zh", "replySnoozeIn", [3, "day"], "3 天後"],
    ["zh", "replySnoozeBack", [1792650600], "2026/10/22（四） 14:30 回來"],
    ["zh", "loreCopyEntryId", ["L-7"], "複製條目編號 L-7"],
    ["zh", "loreOpenManual", ["PR 審查"], "開啟任務手冊 PR 審查"],
    ["zh", "loreScopeManual", ["PR 審查"], "任務：PR 審查"],
    ["zh", "loreScopeAgent", ["Mira"], "建立者：Mira"],
    ["zh", "loreEntryCount", [14], "14 筆"],
    ["zh", "loreChars", [6240], "6,240 字"],
    ["zh", "loreDropped", [4], "4 筆讀不到"],
    ["zh", "loreEveryoneUsed", [6240], "6,240 字・每位成員先讀"],
    ["zh", "loreDirectoryNotFound", ["L-212"], "找不到 L-212"],
    ["zh", "loreDirectorySectionGone", ["L-212"], "L-212 所在的段落已經不在了（成員已離開或手冊已刪除）"],
    ["zh", "loreHighlightMissing", ["L-212"], "L-212 不在目前的篩選結果裡（已失效的條目要勾「已失效」才會列出）"],
    ["zh", "loreHighlightLoadFailed", ["L-212"], "載入更多條目失敗，還沒找到 L-212；往下捲可以再試一次"],
    ["zh", "loreMultiStaged", [3], "3 筆未儲存"],
    ["zh", "loreBudgetSentence", [MEMBER_DROPPING], "上限 10,000 字，已用 9,640 字：所有人的傳承 14 筆（6,240 字）排在最前面，小安自己 7 筆（3,400 字）；從 L-212 開始 4 筆讀不到（L-212 需要 520 字，只剩 360 字）。"],
    ["zh", "loreBudgetSentence", [MEMBER_EVERYONE_OVER], "上限 10,000 字，已用 9,800 字：所有人的傳承就超過上限，20 筆裡只放得下 14 筆（9,800 字）；小安自己的 7 筆都讀不到。"],
    ["zh", "loreBudgetSentence", [MANUAL_FITS], "上限 10,000 字，已用 3,210 字：8 筆（3,210 字）放得下；全部讀得到。"],
    ["zh", "loreCapLine", [9640, 10000, "L-212", 520], "上限線　已用 9,640 / 10,000 字・L-212 需要 520 字，從這裡以下讀不到"],
    ["zh", "loreCapLineAway", ["L-212"], "上限線在 L-212，不在目前的結果裡"],
    ["zh", "loreEveryoneRef", [14], "所有人的傳承 14 筆排在最前面（在「所有人」那一段調整）"],
    ["zh", "loreBatchSelected", [2], "已選 2 筆"],
    ["zh", "loreBatchResult", [["L-1", "L-3"], [{ id: "L-2", reason: "你沒有權限做這個動作" }]], "已完成 L-1、L-3；沒有成功：L-2（你沒有權限做這個動作）"],
    ["zh", "secretCopyId", ["sec-00000000000a"], "複製密鑰 ID sec-00000000000a"],
    ["zh", "secretDeleteConfirmBody", ["部署權杖"], "要刪除「部署權杖」嗎？刪除後無法復原。"],
    ["zh", "sharedDocCopyId", ["sd-0c41e97a2b58"], "複製檔案 ID sd-0c41e97a2b58"],
    ["zh", "sharedDocDeleteConfirmBody", ["Review Rule"], "要永久刪除「Review Rule」嗎？檔案與所有版本都會刪除，無法復原。"],
    ["zh", "sharedDocEditHint", [], "下載後在電腦上修改，再用「上傳新版本」換上去。"],
    ["zh", "sharedDocUpdated", ["Mira", "11/17 16:05"], "Mira · 11/17 16:05"],
    ["zh", "outsourceLabel", ["O-7"], "外包 · O-7"],
    ["zh", "groupHeading", ["前端", 3], "前端 · 3"],
    ["zh", "groupDeleteConfirm", ["前端"], "刪除「前端」？成員會回到未分組。"],
    ["zh", "groupCurrentlyIn", ["審查"], "目前在「審查」"],
    ["zh", "groupOptions", ["核心"], "群組選項：核心"],
    ["zh", "groupAutoJoinTitle", ["前端外包"], "自動加入 · 前端外包"],
    ["zh", "groupFilterLabel", [0], "篩選"],
    ["zh", "groupFilterLabel", [2], "篩選 · 2"],
    ["zh", "groupBringInTitle", ["PR 審查", "審查"], "要把目前的「PR 審查」外包搬進「審查」嗎？"],
    ["zh", "groupDragPickedUp", ["Mira"], "已拿起 Mira。"],
    ["zh", "groupDragOverGroup", ["Mira", "前端"], "Mira 會移到「前端」。"],
    ["zh", "groupDragDroppedGroup", ["Mira", "前端"], "Mira 已移到「前端」。"],
    ["zh", "groupDragOverPosition", ["前端", 2], "前端 會移到第 2 個群組。"],
    ["zh", "groupDragDroppedPosition", ["前端", 2], "前端 已移到第 2 個群組。"],
    ["zh", "groupDragCancelled", ["Mira"], "已取消拖曳，Mira 回到原位。"],
    // 🔴 折疊,不是截斷。這一句只可以講「這則還在、只是折起來了」;
    // 「更早的訊息沒有被帶進來」是另一件事,而且刻意沒有 composer——
    // 共用一個組裝器正是兩者會開始共用詞彙的那條路。
    ["zh", "resumeBodyOmitted", [1284], "折起 1284"],
    ["zh", "chatOfflineTitle", ["Mira"], "Mira 目前離線"],
    ["zh", "chatOfflineQueueHint", ["Mira"], "你仍可在下方留言，Mira 上線後就會讀到。"],
    ["zh", "chatWakeQueueHint", ["Mira"], "Mira 目前離線中 — 訊息會排隊，或立即喚醒上線"],
    ["zh", "chatStoppingQueueHint", ["Mira"], "Mira 正在停止 — 訊息會排隊"],
    ["zh", "chatInterAgentExpand", [0], "0 則成員間對話 · 展開"],
    ["zh", "chatInterAgentExpand", [1], "1 則成員間對話 · 展開"],
    ["zh", "chatInterAgentExpand", [2], "2 則成員間對話 · 展開"],
    ["zh", "chatInterAgentExpand", [17], "17 則成員間對話 · 展開"],
    ["zh", "chatSearchEmpty", ["deploy"], "找不到含「deploy」的訊息"],
    ["zh", "chatSearchCount", [7, 7], "7/7"],
    ["zh", "memberForceStopConfirmBody", ["Mira"], "立即強制停止 Mira——跳過正常收尾，進行中未存的工作會遺失。"],
    ["zh", "memberRefocusSince", ["2 天"], "上次重新聚焦 2 天"],
    ["zh", "memberMachineMovingTo", ["Alpha"], "→ 要換到 Alpha"],
    ["zh", "workerMachineMovingTo", ["Alpha"], "→ 要換到 Alpha"],
    ["zh", "runtimeLoginWarning", ["seth-m1", "claude"], "seth-m1 未登入 Claude"],
    ["zh", "runtimeLoginWarning", ["seth-m1", "codex"], "seth-m1 未登入 Codex"],
    ["zh", "modelCallAuthWarning", ["claude"], "Claude 登入失效"],
    ["zh", "modelCallAuthWarning", ["codex"], "Codex 登入失效"],
    ["zh", "modelCallOtherWarning", ["claude", "invalid_request"], "Claude 模型呼叫失敗（invalid_request）"],
    ["zh", "modelCallRateLimitWarning", ["claude", at(2026, 10, 4, 16, 39), NOW], "Claude 已達用量上限 · 今天 16:39 重置"],
    ["zh", "modelCallRateLimitWarning", ["claude", at(2026, 10, 5, 16, 39), NOW], "Claude 已達用量上限 · 明天 16:39 重置"],
    ["zh", "modelCallRateLimitWarning", ["claude", at(2026, 10, 7, 16, 39), NOW], "Claude 已達用量上限 · 10/7 16:39 重置"],
    ["zh", "modelCallRateLimitWarning", ["claude", at(2027, 1, 2, 16, 39), NOW], "Claude 已達用量上限 · 2027/1/2 16:39 重置"],
    ["zh", "modelCallRateLimitWarning", ["claude", null, NOW], "Claude 已達用量上限"],
    ["zh", "modelCallRateLimitWarning", ["codex", at(2026, 10, 4, 16, 39), NOW], "Codex 已達用量上限 · 今天 16:39 重置"],
    ["zh", "modelCallRateLimitWarning", ["codex", null, NOW], "Codex 已達用量上限"],
    ["zh", "runtimeSignInHint", ["claude"], "可到「監控」頁的機器資訊，在 Claude 欄按版本號 →「登入」"],
    ["zh", "runtimeSignInHint", ["codex"], "可到「監控」頁的機器資訊，在 Codex 欄按版本號 →「登入」"],
    ["zh", "modelCallServerWarning", ["claude"], "Claude 伺服器異常"],
    ["zh", "modelCallServerWarning", ["codex"], "Codex 伺服器異常"],
    ["zh", "memberModelCallLastSuccess", ["3m"], "3m 前"],
    ["zh", "monitorLimitReached", [at(2026, 10, 4, 16, 39), NOW], "已達上限 · 今天 16:39 重置"],
    ["zh", "monitorLimitReached", [at(2026, 10, 5, 16, 39), NOW], "已達上限 · 明天 16:39 重置"],
    ["zh", "monitorLimitReached", [at(2026, 10, 7, 16, 39), NOW], "已達上限 · 10/7 16:39 重置"],
    ["zh", "monitorLimitReached", [at(2027, 1, 2, 16, 39), NOW], "已達上限 · 2027/1/2 16:39 重置"],
    ["zh", "monitorLimitReached", [null, NOW], "已達上限"],
    ["zh", "agentPendingChange", ["Codex"], "→ 要換成 Codex"],
    [
      "zh",
      "agentWindDownForChange",
      ["14:32"],
      "正在收尾以套用你的改動·最晚 14:32 生效",
    ],
    [
      "zh",
      "agentWindDownOnDeadline",
      ["14:32"],
      "正在收尾，已給死線·最晚 14:32 生效",
    ],
    ["zh", "machineOfflineOption", ["Alpha"], "Alpha（離線）"],
    ["zh", "machineBootstrapErrorDetail", ["ocwarden binary missing"], "安裝請求失敗:ocwarden binary missing"],
    ["zh", "machineBootstrapFailed", [3], "安裝失敗(結束碼 3),原因如下:"],
    ["zh", "machineBootstrapConfirmBody", ["Alpha"], "「Alpha」目前在線上。重新安裝會讓這台機器上的成員全部斷線，而且無法復原；之後要讓成員重新上線。"],
    ["zh", "machineUninstallConfirmBody", ["Alpha"], "確定要解除安裝「Alpha」嗎？成功後機器會變成離線，但紀錄會保留，之後可以再安裝。"],
    ["zh", "machineUninstallWarnBody", ["Alpha",1], "「Alpha」上還有 1 位成員在線上。現在解除安裝會讓他們斷線——建議先讓相關成員下線。仍要繼續嗎？"],
    ["zh", "machineUninstallWarnBody", ["Alpha",4], "「Alpha」上還有 4 位成員在線上。現在解除安裝會讓他們斷線——建議先讓相關成員下線。仍要繼續嗎？"],
    ["zh", "machineDeleteConfirmBody", ["Alpha"], "確定要刪除「Alpha」嗎？這台機器會立刻無法連線，還在上面的成員也會一起斷線。機器上安裝的程式不會被移除（那是「解除安裝」）。此動作無法復原，要恢復只能重新安裝。"],
    ["zh", "costResetConfirmBody", ["$37"], "這會把目前累計的 $37 歸零，從 0 重新開始累積。這個數字沒有留在任何其他地方，清掉就回不來了。"],
    ["zh", "accountCostResetConfirmBody", ["$37"], "這會把這個帳號累計的 $37 歸零，從 0 重新開始累積。底下成員各自的數字不會被動到。這個數字沒有留在任何其他地方，清掉就回不來了。"],
    ["zh", "themeImportSkipped", [2, ["nav.tasks", "profile.themeOffice"]], "已匯入,但有2個用詞代碼不認得、已略過:nav.tasks、profile.themeOffice"],
    ["zh", "themeImportSkipped", [30, ["a.b", "c.d", "e.f"]], "已匯入,但有30個用詞代碼不認得、已略過:a.b、c.d、e.f等"],
    ["zh", "themeDeleteConfirm", ["精靈村"], "刪除主題「精靈村」?此動作無法復原。"],
    ["zh", "themeEntryLabel", ["精靈村"], "主題：精靈村，開啟主題設定"],
    ["zh", "themeEntryLabel", ["精靈村", "依時段"], "主題：精靈村（依時段），開啟主題設定"],
    ["zh", "deleteRoleConfirm", ["研究員"], "確定刪除角色「研究員」？該角色的正職成員會標記為已離開，聊天紀錄保留為唯讀歷史；角色定義與 Insight 會永久刪除，無法復原。"],
    ["zh", "docHistoryRestoreConfirm", ["7/29 14:03"], "確定還原「7/29 14:03」這個版本？目前的內容會被覆蓋，但會存成新的版本紀錄。"],
    ["zh", "docHistoryBlockedReason", [["判準", "SOP"], 10000], "「判準、SOP」超過 10000 字上限，而且沒有比目前的內容短，所以無法還原。"],
    ["zh", "deleteManualConfirm", ["review-pr"], "確定刪除任務類型「review-pr」？其手冊（定義、SOP）將一併移除，無法復原。"],
    ["zh", "manualEditSection", ["這是什麼任務？"], "編輯「這是什麼任務？」"],
    ["zh", "docHistoryVersionLabel", ["7/29 14:03"], "此版本（7/29 14:03）"],
    // T-791e. Both numbers, both sentences: the retention line has to say the
    // count is in SAVES, and the over-cap line has to carry the current size
    // AND the limit — an owner cannot act on "too long".
    [
      "zh",
      "bootDocNoteHistory",
      [10],
      "版本紀錄只保留最近 10 版，而且是以「存檔次數」計、不是以時間計——連按幾次小修就會把較舊的版本沖掉。「同步預設內容」不受影響，永遠在。",
    ],
    ["zh", "docOverCap", [61234, 60000], "現在 61234 字，超過上限 60000 字，請先刪掉一些再儲存。"],
    ["zh", "docHistoryActor", ["Kyle", "m-f663"], "Kyle（m-f663）"],
    ["zh", "docHistoryActor", ["", "ow-c975"], "ow-c975"],
    ["zh", "diffTooLarge", [2400], "內容太長，無法逐行比對（2400 行）。"],
    // ── 定期訊息 · 自訂頻率 (T-49e7) ──
    // 每一組的三態各釘一次:整組選滿、部分、空的。空的那一態是可被拒絕的狀態,
    // 說出來才不會與「全選」看起來一樣。
    ["zh", "schedCustomMonths", [ALL_MONTHS], "每個月"],
    ["zh", "schedCustomMonths", [[3, 6, 9, 12]], "每年 3、6、9、12 月"],
    ["zh", "schedCustomMonths", [[]], "尚未選擇"],
    ["zh", "schedCustomDays", [ALL_DAYS], "每天"],
    ["zh", "schedCustomDays", [[1, 15, 31]], "每月 1、15、31 號"],
    // Scattered past the cap: four numbers, then how many were left unsaid.
    // 另 makes the 2 a REMAINDER; a bare 等 2 個 would read as the total and
    // contradict the four numbers standing right before it.
    ["zh", "schedCustomDays", [[1, 3, 5, 7, 9, 11]], "每月 1、3、5、7 號等,另 2 個"],
    ["zh", "schedCustomDays", [[]], "尚未選擇"],
    ["zh", "schedCustomHours", [ALL_HOURS], "每小時"],
    ["zh", "schedCustomHours", [[9, 17]], "第 9、17 點"],
    ["zh", "schedCustomHours", [[]], "尚未選擇"],
    ["zh", "schedCustomMinutes", [ALL_MINUTES], "每分鐘"],
    // Evenly spaced from 0 IS an interval, and that is how the owner reads it
    // back — this is the case that made him think the control only did intervals.
    ["zh", "schedCustomMinutes", [[0, 20, 40]], "每 20 分鐘"],
    ["zh", "schedCustomMinutes", [EVERY_FIVE], "每 5 分鐘"],
    ["zh", "schedCustomMinutes", [[0, 30]], "每 30 分鐘"],
    // …but an offset run is NOT: 「每 20 分鐘」 would not say WHICH minutes.
    ["zh", "schedCustomMinutes", [[15, 35, 55]], "第 15、35、55 分"],
    ["zh", "schedCustomMinutes", [[0, 20, 45]], "第 0、20、45 分"],
    ["zh", "schedCustomMinutes", [[7]], "第 7 分"],
    ["zh", "schedCustomMinutes", [[]], "尚未選擇"],
    // 全年 carries no information on a row (00053 backfilled every existing row
    // with all twelve), so the row summary — and ONLY the row summary — leaves
    // the month phrase out. The other three groups are byte-for-byte unchanged.
    ["zh", "schedCustomSummary", [ALL_MONTHS, ALL_DAYS, ALL_HOURS, [0, 20, 40]], "每天 · 每小時 · 每 20 分鐘"],
    // Anything short of the whole year still names the months.
    ["zh", "schedCustomSummary", [[3], [1, 15], [9], [30]], "每年 3 月 · 每月 1、15 號 · 第 9 點 · 第 30 分"],
    ["zh", "schedCustomSummary", [[3, 6, 9, 12], ALL_DAYS, ALL_HOURS, [0]], "每年 3、6、9、12 月 · 每天 · 每小時 · 第 0 分"],
    // 🔴 An empty month set is a 422 and must never render as the omitted 全年.
    // It is named because the leading slot is no longer the months slot.
    ["zh", "schedCustomSummary", [[], ALL_DAYS, ALL_HOURS, [0, 20, 40]], "幾月:尚未選擇 · 每天 · 每小時 · 每 20 分鐘"],
    ["zh", "schedMinuteStep", [20], "每 20 分鐘"],
    ["en", "taskProgress", [3,7], "Step 3/7"],
    ["en", "taskElapsed", ["2h"], "Elapsed 2h"],
    ["en", "taskPlanningBy", ["Mira"], "Waiting for Mira to create steps"],
    ["en", "taskBlockedBy", ["T-1234"], "Waiting on T-1234"],
    ["en", "taskBlockedByMissing", ["T-dead"], "Waiting on T-dead (task not found)"],
    ["en", "taskCopyTaskNo", ["T-1234"], "Copy task number T-1234"],
    ["en", "taskTerminateConfirmBody", ["修理電梯"], "Terminate “修理電梯”? The task moves to Terminated and cannot be resumed; the executor will be notified to wrap it up."],
    ["en", "taskMarkDuplicateBody", ["T-1234"], "Mark “T-1234” a duplicate of another task? It moves to Duplicate and cannot be resumed. Pick the original:"],
    ["en", "taskDuplicateOf", ["T-9999"], "Duplicate of T-9999"],
    ["en", "taskReassignTitle", ["T-1234"], "Reassign T-1234"],
    ["en", "taskDueDate", [1793375999], "Fri, Oct 30, 2026"],
    ["en", "taskDueDateTime", [1792631100], "Thu, Oct 22, 2026 09:05"],
    ["en", "taskOverdueDays", [1], "Overdue 1 day"],
    ["en", "taskOverdueDays", [2], "Overdue 2 days"],
    ["en", "taskDueRemaining", [{ overdue: false, amount: 1, unit: "day" }], "1 day left"],
    ["en", "taskDueRemaining", [{ overdue: false, amount: 19, unit: "hour" }], "19 hours left"],
    ["en", "taskDueRemaining", [{ overdue: true, amount: 2, unit: "day" }], "Overdue 2 days"],
    ["en", "taskDueRemaining", [{ overdue: true, amount: 1, unit: "minute" }], "Overdue 1 minute"],
    ["en", "taskDueLead", [1440], "1 day"],
    ["en", "taskDueLead", [2880], "2 days"],
    ["en", "taskDueLead", [60], "1 hour"],
    ["en", "taskDueLead", [1], "1 minute"],
    ["en", "taskDueLead", [90], "90 minutes"],
    ["en", "taskDueLeadWorkspace", [1440], "Workspace 1 day"],
    ["en", "taskDueLeadWorkspace", [120], "Workspace 2 hours"],
    ["en", "taskCountSummary", [2, 1, 12], "2 overdue · 1 due soon · 12 open"],
    ["en", "taskCountSummary", [3, 0, 4], "3 overdue · 4 open"],
    ["en", "taskCountSummary", [0, 0, 4], "4 open"],
    ["en", "replyWaited", ["3 小時"], "Waiting 3 小時"],
    ["en", "replyWaitedFull", [23 * 3600 + 59 * 60], "Waiting 23 hours 59 minutes"],
    ["en", "replyWaitedFull", [86400 + 3600 + 60], "Waiting 1 day 1 hour 1 minute"],
    ["en", "replyWaitedFull", [2 * 86400], "Waiting 2 days"],
    ["en", "replyOpenedAt", ["7/13 09:05"], "Opened 7/13 09:05"],
    ["en", "replyAnsweredAt", ["7/13 09:05"], "Answered 7/13 09:05"],
    ["en", "replyExpiredAt", ["7/13 09:05"], "Expired 7/13 09:05"],
    ["en", "replyExpireConfirmBody", ["要不要買新的"], "Mark \"要不要買新的\" as expired? This cannot be undone and does not count as an answer — the member is notified and will open a fresh card if the question still matters."],
    ["en", "replySelectedCount", [0], "Selected 0 options"],
    // n=1 is its own row: en inflects and the other two rows cannot see it.
    ["en", "replySelectedCount", [1], "Selected 1 option"],
    ["en", "replySelectedCount", [2], "Selected 2 options"],
    ["en", "replyPickedOptions", [["Send it"]], "Send it"],
    ["en", "replyPickedOptions", [["Send it", "Not yet"]], "Send it, Not yet"],
    ["en", "replySnoozeIn", [1, "hour"], "In 1 hour"],
    ["en", "replySnoozeIn", [3, "day"], "In 3 days"],
    ["en", "replySnoozeBack", [1792650600], "Back Thu, Oct 22, 2026 14:30"],
    ["en", "loreCopyEntryId", ["L-7"], "Copy entry id L-7"],
    ["en", "loreOpenManual", ["PR review"], "Open task manual PR review"],
    ["en", "loreScopeManual", ["PR review"], "Task: PR review"],
    ["en", "loreScopeAgent", ["Mira"], "Author: Mira"],
    ["en", "loreEntryCount", [1], "1 entry"],
    ["en", "loreEntryCount", [14], "14 entries"],
    ["en", "loreChars", [6240], "6,240 chars"],
    ["en", "loreDropped", [4], "4 not read"],
    ["en", "loreEveryoneUsed", [6240], "6,240 chars · read first by every member"],
    ["en", "loreDirectoryNotFound", ["L-212"], "No entry L-212"],
    ["en", "loreDirectorySectionGone", ["L-212"], "L-212's section is gone (the member left or the manual was deleted)"],
    ["en", "loreHighlightMissing", ["L-212"], "L-212 is not in the current results (tick Retired to list retired entries)"],
    ["en", "loreHighlightLoadFailed", ["L-212"], "Loading more entries failed before L-212 was found; scroll down to try again"],
    ["en", "loreMultiStaged", [3], "3 unsaved"],
    ["en", "loreBudgetSentence", [MEMBER_DROPPING], "Cap 10,000 chars, 9,640 used: everyone's lore, 14 entries (6,240 chars), comes first, then 小安's own 7 entries (3,400 chars); from L-212 on, 4 are not read (L-212 needs 520 chars, only 360 left)."],
    ["en", "loreBudgetSentence", [MEMBER_EVERYONE_OVER], "Cap 10,000 chars, 9,800 used: everyone's lore alone is over the cap — of 20 entries only 14 fit (9,800 chars); none of 小安's own 7 entries are read."],
    ["en", "loreBudgetSentence", [MANUAL_FITS], "Cap 10,000 chars, 3,210 used: 8 entries (3,210 chars) fit; everything is read."],
    ["en", "loreCapLine", [9640, 10000, "L-212", 520], "Cap line · used 9,640 / 10,000 chars · L-212 needs 520 chars; nothing from here down is read"],
    ["en", "loreCapLineAway", ["L-212"], "The cap line is at L-212, which is not in the current results"],
    ["en", "loreEveryoneRef", [14], "Everyone's lore, 14 entries, comes first (change it in the Everyone section)"],
    ["en", "loreBatchSelected", [2], "2 selected"],
    ["en", "loreBatchResult", [["L-1"], [{ id: "L-2", reason: "admin only" }, { id: "L-4", reason: "" }]], "Done: L-1; Did not go through: L-2 (admin only), L-4"],
    ["en", "secretCopyId", ["sec-00000000000a"], "Copy secret ID sec-00000000000a"],
    ["en", "secretDeleteConfirmBody", ["Deploy token"], "Delete \u201cDeploy token\u201d? This cannot be undone."],
    ["en", "sharedDocCopyId", ["sd-0c41e97a2b58"], "Copy file ID sd-0c41e97a2b58"],
    ["en", "sharedDocDeleteConfirmBody", ["Review Rule"], "Permanently delete \u201cReview Rule\u201d? The file and every version are deleted. This cannot be undone."],
    ["en", "sharedDocEditHint", [], "Download it, edit it on your computer, then put it back with \u201cUpload new version\u201d."],
    ["en", "sharedDocUpdated", ["Mira", "11/17 16:05"], "Mira · 11/17 16:05"],
    ["en", "outsourceLabel", ["O-7"], "Outsource · O-7"],
    ["en", "groupHeading", ["Frontend", 3], "Frontend · 3"],
    ["en", "groupDeleteConfirm", ["Frontend"], "Delete \u201cFrontend\u201d? Its members go back to Ungrouped."],
    ["en", "groupCurrentlyIn", ["Review"], "Now in \u201cReview\u201d"],
    ["en", "groupOptions", ["Frontend"], "Group options: Frontend"],
    ["en", "groupAutoJoinTitle", ["Frontend"], "Auto-join · Frontend"],
    ["en", "groupFilterLabel", [0], "Filter"],
    ["en", "groupFilterLabel", [3], "Filter · 3"],
    ["en", "groupBringInTitle", ["PR review", "Review"], "Move the current \u201cPR review\u201d outsource workers into \u201cReview\u201d?"],
    ["en", "groupDragPickedUp", ["Mira"], "Picked up Mira."],
    ["en", "groupDragOverGroup", ["Mira", "Frontend"], "Mira will move to \u201cFrontend\u201d."],
    ["en", "groupDragDroppedGroup", ["Mira", "Frontend"], "Mira moved to \u201cFrontend\u201d."],
    ["en", "groupDragOverPosition", ["Frontend", 2], "Frontend will move to group position 2."],
    ["en", "groupDragDroppedPosition", ["Frontend", 2], "Frontend moved to group position 2."],
    ["en", "groupDragCancelled", ["Mira"], "Drag cancelled. Mira is back in place."],
    ["en", "resumeBodyOmitted", [1284], "folded 1284"],
    ["en", "chatOfflineTitle", ["Mira"], "Mira is offline"],
    ["en", "chatOfflineQueueHint", ["Mira"], "You can still leave a message — Mira will read it once back online."],
    ["en", "chatWakeQueueHint", ["Mira"], "Mira is offline — your message will queue, or wake them now"],
    ["en", "chatStoppingQueueHint", ["Mira"], "Mira is stopping — your message will queue"],
    // T-3b90 — the usage snapshot's age, printed beside the number on the
    // account card. zh runs the characters together around the duration and
    // puts 前 after it; en needs a space on both sides.
    ["zh", "monitorMeasuredAgo", ["3d"], "量於 3d 前"],
    ["zh", "monitorMeasuredAgo", ["2h 15m"], "量於 2h 15m 前"],
    ["en", "monitorMeasuredAgo", ["3d"], "measured 3d ago"],
    ["zh", "monitorDiskOtherMembers", [1], "其餘 1 位合計"],
    ["zh", "monitorDiskOtherMembers", [12], "其餘 12 位合計"],
    ["en", "monitorDiskOtherMembers", [1], "Remaining 1 member"],
    ["en", "monitorDiskOtherMembers", [12], "Remaining 12 members"],
    ["en", "chatInterAgentExpand", [0], "0 messages between agents · expand"],
    ["en", "chatInterAgentExpand", [1], "1 message between agents · expand"],
    ["en", "chatInterAgentExpand", [2], "2 messages between agents · expand"],
    ["en", "chatInterAgentExpand", [17], "17 messages between agents · expand"],
    ["en", "chatSearchEmpty", ["deploy"], "No messages containing “deploy”"],
    ["en", "chatSearchCount", [2, 3], "2/3"],
    ["en", "memberForceStopConfirmBody", ["Mira"], "Force-stop Mira immediately — skipping the graceful shutdown. Any unsaved work in progress is lost."],
    ["en", "memberRefocusSince", ["2 天"], "Last refocus 2 天"],
    ["en", "memberMachineMovingTo", ["Alpha"], "→ Moving to Alpha"],
    ["en", "workerMachineMovingTo", ["Alpha"], "→ Moving to Alpha"],
    ["en", "runtimeLoginWarning", ["seth-m1", "claude"], "seth-m1 signed out of Claude"],
    ["en", "runtimeLoginWarning", ["seth-m1", "codex"], "seth-m1 signed out of Codex"],
    ["en", "modelCallAuthWarning", ["claude"], "Claude sign-in expired"],
    ["en", "modelCallAuthWarning", ["codex"], "Codex sign-in expired"],
    ["en", "modelCallOtherWarning", ["codex", "contextWindowExceeded"], "Codex model call failed (contextWindowExceeded)"],
    ["en", "modelCallRateLimitWarning", ["claude", at(2026, 10, 4, 16, 39), NOW], "Claude usage limit reached · resets today 16:39"],
    ["en", "modelCallRateLimitWarning", ["claude", at(2026, 10, 5, 16, 39), NOW], "Claude usage limit reached · resets tomorrow 16:39"],
    ["en", "modelCallRateLimitWarning", ["claude", at(2026, 10, 7, 16, 39), NOW], "Claude usage limit reached · resets 10/7 16:39"],
    ["en", "modelCallRateLimitWarning", ["claude", at(2027, 1, 2, 16, 39), NOW], "Claude usage limit reached · resets 2027/1/2 16:39"],
    ["en", "modelCallRateLimitWarning", ["claude", null, NOW], "Claude usage limit reached"],
    ["en", "modelCallRateLimitWarning", ["codex", at(2026, 10, 4, 16, 39), NOW], "Codex usage limit reached · resets today 16:39"],
    ["en", "modelCallRateLimitWarning", ["codex", null, NOW], "Codex usage limit reached"],
    ["en", "runtimeSignInHint", ["claude"], "To sign in: Monitor → Machines, click the version in the Claude column → Sign in"],
    ["en", "runtimeSignInHint", ["codex"], "To sign in: Monitor → Machines, click the version in the Codex column → Sign in"],
    ["en", "modelCallServerWarning", ["claude"], "Claude server error"],
    ["en", "modelCallServerWarning", ["codex"], "Codex server error"],
    ["en", "memberModelCallLastSuccess", ["2h 15m"], "2h 15m ago"],
    ["en", "monitorLimitReached", [at(2026, 10, 4, 16, 39), NOW], "Limit reached · resets today 16:39"],
    ["en", "monitorLimitReached", [at(2026, 10, 5, 16, 39), NOW], "Limit reached · resets tomorrow 16:39"],
    ["en", "monitorLimitReached", [at(2027, 1, 2, 16, 39), NOW], "Limit reached · resets 2027/1/2 16:39"],
    ["en", "monitorLimitReached", [null, NOW], "Limit reached"],
    ["en", "agentPendingChange", ["Codex"], "→ Changing to Codex"],
    [
      "en",
      "agentWindDownForChange",
      ["14:32"],
      "Winding down to apply your change · by 14:32 at the latest",
    ],
    [
      "en",
      "agentWindDownOnDeadline",
      ["14:32"],
      "Winding down on a deadline · by 14:32 at the latest",
    ],
    ["en", "machineOfflineOption", ["Alpha"], "Alpha (offline)"],
    ["en", "machineBootstrapErrorDetail", ["ocwarden binary missing"], "Install request failed: ocwarden binary missing"],
    ["en", "machineBootstrapFailed", [3], "Install failed (exit code 3). Reason:"],
    ["en", "machineBootstrapConfirmBody", ["Alpha"], "“Alpha” is online. Installing again disconnects every member on this machine and CANNOT be undone; its members will need to be brought back online afterwards."],
    ["en", "machineUninstallConfirmBody", ["Alpha"], "Uninstall “Alpha”? On success the machine goes offline, but its record is kept and it can be installed again."],
    ["en", "machineUninstallWarnBody", ["Alpha",1], "“Alpha” still has 1 member(s) online on it. Uninstalling now disconnects them — take the related members offline first. Proceed anyway?"],
    ["en", "machineUninstallWarnBody", ["Alpha",4], "“Alpha” still has 4 member(s) online on it. Uninstalling now disconnects them — take the related members offline first. Proceed anyway?"],
    ["en", "machineDeleteConfirmBody", ["Alpha"], "Delete “Alpha”? The machine can no longer connect, effective immediately, and any member still on it is disconnected too. Nothing installed on the machine is removed (that is “Uninstall”), and this cannot be undone — bringing it back means installing it again."],
    ["en", "costResetConfirmBody", ["$37"], "This resets the accumulated $37 to zero and starts counting again from 0. The figure is not kept anywhere else, so it cannot be recovered."],
    ["en", "accountCostResetConfirmBody", ["$37"], "This resets the account's accumulated $37 to zero and starts counting again from 0. No member's own figure is touched. The figure is not kept anywhere else, so it cannot be recovered."],
    ["en", "themeImportSkipped", [2, ["nav.tasks", "profile.themeOffice"]], "Imported, but 2 wording code(s) were not recognised and were skipped: nav.tasks, profile.themeOffice"],
    ["en", "themeImportSkipped", [30, ["a.b", "c.d", "e.f"]], "Imported, but 30 wording code(s) were not recognised and were skipped: a.b, c.d, e.f …"],
    ["en", "themeDeleteConfirm", ["精靈村"], "Delete theme \"精靈村\"? This cannot be undone."],
    ["en", "themeEntryLabel", ["Elf Village"], "Theme: Elf Village, open theme settings"],
    ["en", "themeEntryLabel", ["Elf Village", "Scheduled"], "Theme: Elf Village (Scheduled), open theme settings"],
    ["en", "deleteRoleConfirm", ["研究員"], "Delete role \"研究員\"? Its staff members will be marked as departed and their chats kept as read-only history; the role definition and Insight will be deleted permanently."],
    ["en", "docHistoryRestoreConfirm", ["7/29 14:03"], "Restore the version from \"7/29 14:03\"? The current content is overwritten, but is kept as a new revision."],
    ["en", "docHistoryBlockedReason", [["Insight", "SOP"], 10000], "\"Insight, SOP\" is over the 10000-character limit and no shorter than what is stored now, so it cannot be restored."],
    ["en", "deleteManualConfirm", ["review-pr"], "Delete the task type “review-pr”? Its manual (definition, SOP) is removed with it and cannot be restored."],
    ["en", "manualEditSection", ["What is this task?"], "Edit “What is this task?”"],
    ["en", "docHistoryVersionLabel", ["7/29 14:03"], "This version (7/29 14:03)"],
    [
      "en",
      "bootDocNoteHistory",
      [10],
      "Version history keeps the last 10 versions, counted in SAVES rather than in time — a run of small saves pushes the older ones out. Sync to default is never affected and is always available.",
    ],
    [
      "en",
      "docOverCap",
      [61234, 60000],
      "Now 61234 characters, over the limit of 60000 — remove some before saving.",
    ],
    ["en", "docHistoryActor", ["Kyle", "m-f663"], "Kyle (m-f663)"],
    ["en", "docHistoryActor", ["", "ow-c975"], "ow-c975"],
    ["en", "diffTooLarge", [2400], "Too long to compare line by line (2400 lines)."],
    ["en", "schedCustomMonths", [ALL_MONTHS], "Every month"],
    ["en", "schedCustomMonths", [[3, 6, 9, 12]], "Months 3, 6, 9, 12 of the year"],
    ["en", "schedCustomMonths", [[]], "Nothing selected"],
    ["en", "schedCustomDays", [ALL_DAYS], "Daily"],
    ["en", "schedCustomDays", [[1, 15, 31]], "Days 1, 15, 31 of the month"],
    ["en", "schedCustomDays", [[1, 3, 5, 7, 9, 11]], "Days 1, 3, 5, 7 of the month and 2 more"],
    ["en", "schedCustomDays", [[]], "Nothing selected"],
    ["en", "schedCustomHours", [ALL_HOURS], "Every hour"],
    ["en", "schedCustomHours", [[9, 17]], "Hours 9, 17 of the day"],
    ["en", "schedCustomHours", [[]], "Nothing selected"],
    ["en", "schedCustomMinutes", [ALL_MINUTES], "Every minute"],
    ["en", "schedCustomMinutes", [[0, 20, 40]], "Every 20 minutes"],
    ["en", "schedCustomMinutes", [EVERY_FIVE], "Every 5 minutes"],
    ["en", "schedCustomMinutes", [[0, 30]], "Every 30 minutes"],
    ["en", "schedCustomMinutes", [[15, 35, 55]], "Minutes 15, 35, 55 of the hour"],
    ["en", "schedCustomMinutes", [[0, 20, 45]], "Minutes 0, 20, 45 of the hour"],
    ["en", "schedCustomMinutes", [[7]], "Minutes 7 of the hour"],
    ["en", "schedCustomMinutes", [[]], "Nothing selected"],
    ["en", "schedCustomSummary", [ALL_MONTHS, ALL_DAYS, ALL_HOURS, [0, 20, 40]], "Daily · Every hour · Every 20 minutes"],
    ["en", "schedCustomSummary", [[3], [1, 15], [9], [30]], "Months 3 of the year · Days 1, 15 of the month · Hours 9 of the day · Minutes 30 of the hour"],
    ["en", "schedCustomSummary", [[3, 6, 9, 12], ALL_DAYS, ALL_HOURS, [0]], "Months 3, 6, 9, 12 of the year · Daily · Every hour · Minutes 0 of the hour"],
    ["en", "schedCustomSummary", [[], ALL_DAYS, ALL_HOURS, [0, 20, 40]], "Which months: Nothing selected · Daily · Every hour · Every 20 minutes"],
    ["en", "schedMinuteStep", [20], "Every 20 minutes"],
];

describe("makeMessages", () => {
  it.each(EXPECTED)(
    "%s %s renders the same text it did before the fragment split",
    (lang, name, args, want) => {
      const composed = makeMessages(DICTS[lang], lang) as unknown as Record<
        string,
        (...a: Arg[]) => string | string[]
      >;
      expect(composed[name](...args)).toEqual(want);
    }
  );

  it("covers every composed message", () => {
    const named = new Set(EXPECTED.map(([, name]) => name));
    const all = Object.keys(makeMessages(zh, "zh"));
    expect([...all].sort()).toEqual([...named].sort());
  });

  it("re-words the task-terminate confirmation body from a theme overlay", () => {
    // The symptom that motivated T-081b: a theme pack could re-word the 終止
    // menu item and the 確認終止 button (plain string leaves) while the dialog
    // BODY — an interpolation function — kept the product's own vocabulary.
    const overlay = {
      "tasks.terminate": "封印",
      "tasks.terminateConfirmBodyLead": "確定要封印「",
      "tasks.terminateConfirmBodyTail": "」嗎？此案將移入卷宗庫，無法復原。",
    };
    const themed = applyWording(zh, overlay);
    expect(themed.tasks.terminate).toBe("封印");
    expect(makeMessages(themed, "zh").taskTerminateConfirmBody("修理電梯")).toBe(
      "確定要封印「修理電梯」嗎？此案將移入卷宗庫，無法復原。"
    );
    // …and the un-overlaid base is untouched (the overlay copies, never mutates).
    expect(makeMessages(zh, "zh").taskTerminateConfirmBody("修理電梯")).toBe(
      "確定要終止「修理電梯」嗎？任務將結案為終止，無法恢復；負責人會收到通知並收尾。"
    );
  });

  it("lets a theme re-word both halves of the 步驟 … · 已歷時 … progress line", () => {
    // The two are ONE visible string on the task card. 已歷時 was still an
    // interpolation template, so a pack could re-word 步驟 and not 已歷時 —
    // the "same sentence, half of it swappable" defect this ticket removed.
    const themed = applyWording(zh, {
      "tasks.progressLabel": "關卡",
      "tasks.elapsedLabel": "耗費",
    });
    const m = makeMessages(themed, "zh");
    expect(`${m.taskProgress(3, 7)} · ${m.taskElapsed("2 小時")}`).toBe(
      "關卡 3/7 · 耗費 2 小時"
    );
  });

  it("re-words every part of the activity sentence and the tree's time and wait labels from a theme overlay", () => {
    const themed = applyWording(zh, {
      "activity.mainBusy": "主幽靈施法中",
      "activity.elapsedLead": "已過",
      "activity.elapsedMinutes": "刻",
      "activity.subagentsAlsoLead": "，還有",
      "activity.subagentsAlsoTailMany": "個分身在施法",
      "activity.waitsLead": "，正在守候",
      "activity.waitsOnlyLead": "正在守候",
      "activity.waitsTailOne": "個背景咒語或排程鐘聲",
      "activity.waitKind.schedule": "排程鐘聲",
      "activity.nextLead": "下次鐘響",
      "activity.recurring": "輪迴",
      "activity.state.idle": "飄盪中",
    });
    const m = makeMessages(themed, "zh");
    expect(m.memberActivitySummary(act({ state: "waiting", waits: [WAIT_BG] }))).toBe("正在守候 1 個背景咒語或排程鐘聲");
    expect(m.memberActivitySummary(act({ state: "idle" }))).toBe("飄盪中");
    const a = act({
      busySince: T - 180,
      subagents: [sub("Explore", "", T - 120), sub("Plan", "", T - 60)],
      waits: [WAIT_CRON],
    });
    expect(m.memberActivitySummary(a)).toBe(
      "主幽靈施法中，還有 2 個分身在施法，正在守候 1 個背景咒語或排程鐘聲",
    );
    expect(m.memberActivitySince(T - 180, T)).toBe("已過 3 刻");
    expect(m.memberActivityDuration(T - 120, T)).toBe("2 刻");
    expect(m.memberActivityWaitTag(cron(NEXT, true), NOW)).toBe("排程鐘聲 · 下次鐘響 12:15 · 輪迴");
  });

  it("names the monitor's own labels in the sign-in hint, so re-wording them reaches it", () => {
    const themed = applyWording(zh, {
      "nav.monitor": "瞭望台",
      "monitor.machinesTitle": "機房",
      "monitor.runtimeLogin.login": "簽到",
    });
    expect(makeMessages(themed, "zh").runtimeSignInHint("claude")).toBe(
      "可到「瞭望台」頁的機房，在 Claude 欄按版本號 →「簽到」"
    );
  });

  it("exposes every fragment it composes from as an overridable message key", () => {
    // A fragment missing from the whitelist is a word no theme can reach — the
    // exact defect this ticket removed, so it must not creep back in.
    const keys = new Set(MESSAGE_KEYS);
    for (const code of [
      "tasks.progressLabel",
      "tasks.elapsedLabel",
      "tasks.planningByLead",
      "tasks.planningByTail",
      "tasks.blockedByLabel",
      "tasks.blockedByMissingSuffix",
      "tasks.copyTaskNoLabel",
      "tasks.terminateConfirmBodyLead",
      "tasks.terminateConfirmBodyTail",
      "tasks.markDuplicateBodyLead",
      "tasks.markDuplicateBodyTail",
      "tasks.duplicateOfLabel",
      "tasks.reassignTitleLabel",
      "mp.machineMovingToLabel",
      "replies.waitedLabel",
      "replies.openedAtLabel",
      "replies.answeredAtLabel",
      "replies.expiredAtLabel",
      "replies.expireConfirmBodyLead",
      "replies.expireConfirmBodyTail",
      "office.outsource.title",
      "office.groups.pickedUpLead",
      "office.groups.countSep",
      "office.groups.bringInTitleMid",
      "office.groups.overGroupMid",
      "office.groups.droppedPositionMid",
      "office.groups.cancelledLead",
      "chat.offlineTitleSuffix",
      "chat.offlineQueueHintLead",
      "chat.offlineQueueHintTail",
      "chat.wakeQueueHintSuffix",
      "chat.interAgentExpandOne",
      "chat.interAgentExpandMany",
      "mp.forceStopConfirmBodyLead",
      "mp.forceStopConfirmBodyTail",
      "mp.effortOf.low",
      "mp.effortOf.medium",
      "mp.effortOf.high",
      "mp.refocusSinceLabel",
      "machine.picker.offlineOptionSuffix",
      "monitor.machine.bootstrapError",
      "monitor.machine.bootstrapFailedLead",
      "monitor.machine.bootstrapFailedTail",
      "monitor.machine.bootstrapConfirmBodyLead",
      "monitor.machine.bootstrapConfirmBodyTail",
      "monitor.machine.uninstallConfirmBodyLead",
      "monitor.machine.uninstallConfirmBodyTail",
      "monitor.machine.uninstallWarnBody1",
      "monitor.machine.uninstallWarnBody2",
      "monitor.machine.uninstallWarnBody3",
      "monitor.machine.deleteConfirmBodyLead",
      "monitor.machine.deleteConfirmBodyTail",
      "profile.themeImportSkippedLead",
      "profile.themeImportSkippedMid",
      "profile.themeImportSkippedMore",
      "settings.themeDeleteConfirmLead",
      "settings.themeDeleteConfirmTail",
      "profile.themeEntryLabelLead",
      "profile.themeEntryLabelTail",
      "profile.themeEntryLabelTagLead",
      "profile.themeEntryLabelTagTail",
      "settings.deleteRoleConfirmLead",
      "settings.deleteRoleConfirmTail",
      "settings.historyRestoreConfirmLead",
      "settings.historyRestoreConfirmTail",
      "settings.historyBlockedReasonLead",
      "settings.historyBlockedReasonMid",
      "settings.historyBlockedReasonTail",
      "settings.deleteManualConfirmLead",
      "settings.deleteManualConfirmTail",
      "settings.historyVersionLabelLead",
      "settings.historyVersionLabelTail",
      "diff.tooLargeLead",
      "diff.tooLargeTail",
      "mp.schedmsg.cadenceDaily",
      "mp.schedmsg.customEveryMonth",
      "mp.schedmsg.customMonthsLabel",
      "mp.schedmsg.customMonthsLead",
      "mp.schedmsg.customMonthsTail",
      "mp.schedmsg.customMoreLead",
      "mp.schedmsg.customMoreTail",
      "mp.schedmsg.customDaysLead",
      "mp.schedmsg.customDaysTail",
      "mp.schedmsg.customEveryHour",
      "mp.schedmsg.customHoursLead",
      "mp.schedmsg.customHoursTail",
      "mp.schedmsg.customEveryMinute",
      "mp.schedmsg.customMinutesLead",
      "mp.schedmsg.customMinutesTail",
      "mp.schedmsg.customNone",
      "mp.schedmsg.customStepLead",
      "mp.schedmsg.customStepTail",
      "lifecycle.modelCallWarning.resetsToday",
      "lifecycle.modelCallWarning.resetsTomorrow",
      "lifecycle.signInHint.lead",
      "lifecycle.signInHint.pageTail",
      "lifecycle.signInHint.sectionTail",
      "lifecycle.signInHint.columnTail",
      "lifecycle.signInHint.tail",
      "activity.state.busy",
      "activity.state.waiting",
      "activity.state.idle",
      "activity.mainBusy",
      "activity.elapsedLead",
      "activity.elapsedSeconds",
      "activity.elapsedMinutes",
      "activity.subagentsAlsoLead",
      "activity.subagentsAlsoTailOne",
      "activity.subagentsAlsoTailMany",
      "activity.waitsLead",
      "activity.waitsOnlyLead",
      "activity.waitsTailOne",
      "activity.waitsTailMany",
      "activity.mainAgent",
      "activity.nextLead",
      "activity.recurring",
      "activity.waitKind.background_command",
      "activity.waitKind.schedule",
      "activity.dialogTitleSep",
      "activity.currentState",
      "activity.openLabelSep",
      "activity.openLabel",
      "activity.close",
    ]) {
      expect(keys.has(code), `${code} must be overridable`).toBe(true);
    }
  });
});

describe("effortText", () => {
  it.each([
    ["zh", "low", "低"],
    ["zh", "medium", "中"],
    ["zh", "high", "高"],
    ["en", "low", "Low"],
    ["en", "medium", "Medium"],
    ["en", "high", "High"],
  ] as [Lang, Effort, string][])(
    "%s %s reads the same as the lookup template did",
    (lang, effort, want) => {
      expect(effortText(DICTS[lang], effort)).toBe(want);
    }
  );
});
