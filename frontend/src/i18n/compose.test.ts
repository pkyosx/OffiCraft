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
import { makeMessages, effortText } from "./compose";
import { applyWording } from "./wording";
import { MESSAGE_KEYS } from "./messageKeys.generated";
import type { Effort } from "../types";

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

/** [language, message, arguments, the text that must appear on screen]. */
const EXPECTED: [Lang, string, (string | number | string[] | number[])[], string][] = [
    // The 429 credential brake. Pinned in BOTH languages because the space
    // after the number is language-specific and is the opposite of `sp`: zh
    // wants it, en does not. Getting that backwards yields "請於 42秒後再試。"
    // or "in 42 s." — which typecheck and the drift gate both accept happily.
    ["zh", "loginThrottled", [42], "目前同時處理的登入太多，請於 42 秒後再試。"],
    ["en", "loginThrottled", [42], "Too many logins in flight. Try again in 42s."],
    ["zh", "taskProgress", [3,7], "步驟 3/7"],
    ["zh", "taskElapsed", ["2 小時"], "已歷時 2 小時"],
    ["zh", "taskPlanningBy", ["Mira"], "等待 Mira 建立 Steps"],
    ["zh", "taskBlockedBy", ["T-1234"], "等 T-1234"],
    ["zh", "taskBlockedByMissing", ["T-dead"], "等 T-dead(查無此任務)"],
    ["zh", "taskCopyTaskNo", ["T-1234"], "複製任務編號 T-1234"],
    ["zh", "taskTerminateConfirmBody", ["修理電梯"], "確定要終止「修理電梯」嗎？任務將移入已結束區，無法恢復；後端會通知負責人做結束處理。"],
    ["zh", "taskMarkDuplicateBody", ["T-1234"], "把「T-1234」標記為某張原票的重複?任務將移入已結束區、無法恢復。請選擇原票:"],
    ["zh", "taskDuplicateOf", ["T-9999"], "重複於 T-9999"],
    ["zh", "taskReassignTitle", ["T-1234"], "轉派 T-1234"],
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
    ["zh", "replyOpenedAt", ["7/13 09:05"], "開卡 7/13 09:05"],
    ["zh", "replyAnsweredAt", ["7/13 09:05"], "已回覆 7/13 09:05"],
    ["zh", "replyExpiredAt", ["7/13 09:05"], "已過期 7/13 09:05"],
    ["zh", "replyExpireConfirmBody", ["要不要買新的"], "要把「要不要買新的」標為過期嗎?此動作不可復原、也不算回答——成員會收到通知,問題還在的話他會重新開一張新卡。"],
    ["zh", "replySelectedCount", [0], "已選 0 項"],
    ["zh", "replySelectedCount", [1], "已選 1 項"],
    ["zh", "replySelectedCount", [2], "已選 2 項"],
    ["zh", "replyPickedOptions", [["寄出"]], "寄出"],
    ["zh", "replyPickedOptions", [["寄出", "先不要"]], "寄出、先不要"],
    ["zh", "outsourceLabel", ["O-7"], "外包 · O-7"],
    ["zh", "workerRefocusSince", ["2 天"], "上次換手 2 天"],
    // 🔴 折疊,不是截斷。這一句只可以講「這則還在、只是折起來了」;
    // 「更早的訊息沒有被帶進來」是另一件事,而且刻意沒有 composer——
    // 共用一個組裝器正是兩者會開始共用詞彙的那條路。
    ["zh", "resumeBodyOmitted", [1284], "折起 1284"],
    ["zh", "chatOfflineTitle", ["Mira"], "Mira 目前離線"],
    ["zh", "chatOfflineQueueHint", ["Mira"], "你仍可在下方留言，Mira 上線後就會讀到。"],
    ["zh", "chatWakeQueueHint", ["Mira"], "Mira 目前離線中 — 訊息會排隊，或立即喚醒上線"],
    ["zh", "chatComposerOffline", ["Mira"], "Mira 目前離線中"],
    ["zh", "chatInterAgentExpand", [0], "0 則成員間對話 · 展開"],
    ["zh", "chatInterAgentExpand", [1], "1 則成員間對話 · 展開"],
    ["zh", "chatInterAgentExpand", [2], "2 則成員間對話 · 展開"],
    ["zh", "chatInterAgentExpand", [17], "17 則成員間對話 · 展開"],
    ["zh", "memberForceStopConfirmBody", ["Mira"], "立即強制停止 Mira——現在就砍掉 session、跳過正常收尾。進行中的未存工作會遺失。"],
    ["zh", "memberRefocusSince", ["2 天"], "上次重新聚焦 2 天"],
    ["zh", "memberMachineMovingTo", ["Alpha"], "→ 要換到 Alpha"],
    ["zh", "workerMachineMovingTo", ["Alpha"], "→ 要換到 Alpha"],
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
    ["zh", "machineBootstrapConfirmBody", ["Alpha"], "「Alpha」目前在線上,已經有一個正在服役的 warden。再安裝一次會直接覆蓋它:這台機器上的成員會全部斷線,而且此動作不可逆 —— 被覆蓋掉的 warden 無法還原,只能重新安裝並讓成員重新上線。"],
    ["zh", "machineUninstallConfirmBody", ["Alpha"], "確定要解除安裝「Alpha」嗎？這會請該機器上的 warden 執行 ocwarden uninstall;成功後機器會變為離線,但記錄會保留(可再次安裝)。"],
    ["zh", "machineUninstallWarnBody", ["Alpha",1], "「Alpha」上還有 1 位成員在線上。現在解除安裝會在成員仍在這台機器上時把 warden 拆除 —— 建議先將相關成員下線。仍要繼續嗎?"],
    ["zh", "machineUninstallWarnBody", ["Alpha",4], "「Alpha」上還有 4 位成員在線上。現在解除安裝會在成員仍在這台機器上時把 warden 拆除 —— 建議先將相關成員下線。仍要繼續嗎?"],
    ["zh", "machineDeleteConfirmBody", ["Alpha"], "確定要刪除「Alpha」嗎?該機器的憑證會立刻失效:機器無法再回報,還指派在這台機器上的 agent 也會一起失去存取權。機器上的 warden 不會被拆除(那是「解除安裝」),而且這個動作無法復原 —— 要恢復只能重新安裝。"],
    ["zh", "costResetConfirmBody", ["$37"], "這會把目前累計的 $37 歸零，從 0 重新開始累積。這個數字沒有留在任何其他地方，清掉就回不來了。"],
    ["zh", "accountCostResetConfirmBody", ["$37"], "這會把這個帳號累計的 $37 歸零，從 0 重新開始累積。底下成員各自的數字不會被動到。這個數字沒有留在任何其他地方，清掉就回不來了。"],
    ["zh", "themeImportSkipped", [2, ["nav.tasks", "profile.themeOffice"]], "已匯入,但有2個用詞代碼不認得、已略過:nav.tasks、profile.themeOffice"],
    ["zh", "themeImportSkipped", [30, ["a.b", "c.d", "e.f"]], "已匯入,但有30個用詞代碼不認得、已略過:a.b、c.d、e.f等"],
    ["zh", "themeDeleteConfirm", ["精靈村"], "刪除主題「精靈村」?此動作無法復原。"],
    ["zh", "deleteRoleConfirm", ["研究員"], "確定刪除角色「研究員」？該角色的成員及其對話、學習經驗將一併移除，無法復原。"],
    ["zh", "docHistoryRestoreConfirm", ["7/29 14:03"], "確定還原「7/29 14:03」這個版本？目前的內容會被覆蓋，但會存成新的版本紀錄。"],
    ["zh", "docHistoryBlockedReason", [["學習經驗", "SOP"], 10000], "「學習經驗、SOP」超過 10000 字上限，且不比目前的內容短——伺服器會拒絕這次還原。"],
    ["zh", "deleteManualConfirm", ["review-pr"], "確定刪除任務類型「review-pr」？其手冊（定義、SOP、學習經驗）將一併移除，無法復原。"],
    ["zh", "manualEditSection", ["這是什麼任務？"], "編輯「這是什麼任務？」"],
    ["zh", "docHistoryVersionLabel", ["7/29 14:03"], "此版本（7/29 14:03）"],
    // T-791e. Both numbers, both sentences: the retention line has to say the
    // count is in SAVES, and the over-cap line has to carry the current size
    // AND the limit — an owner cannot act on "too long".
    [
      "zh",
      "bootDocNoteHistory",
      [10],
      "版本紀錄只保留最近 10 版，而且是以「存檔次數」計、不是以時間計——連按幾次小修就會把較舊的版本沖掉。「還原出廠版」不受影響，永遠在。",
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
    ["en", "taskTerminateConfirmBody", ["修理電梯"], "Terminate “修理電梯”? The task moves to Closed and cannot be resumed; the backend will notify the executor to wind it down."],
    ["en", "taskMarkDuplicateBody", ["T-1234"], "Mark “T-1234” a duplicate of another task? It moves to Closed and cannot be resumed. Pick the original:"],
    ["en", "taskDuplicateOf", ["T-9999"], "Duplicate of T-9999"],
    ["en", "taskReassignTitle", ["T-1234"], "Reassign T-1234"],
    ["en", "replyWaited", ["3 小時"], "Waiting 3 小時"],
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
    ["en", "outsourceLabel", ["O-7"], "Outsource · O-7"],
    ["en", "workerRefocusSince", ["2 天"], "Last handover 2 天"],
    ["en", "resumeBodyOmitted", [1284], "folded 1284"],
    ["en", "chatOfflineTitle", ["Mira"], "Mira is offline"],
    ["en", "chatOfflineQueueHint", ["Mira"], "You can still leave a message — Mira will read it once back online."],
    ["en", "chatWakeQueueHint", ["Mira"], "Mira is offline — your message will queue, or wake them now"],
    ["en", "chatComposerOffline", ["Mira"], "Mira is currently offline"],
    // T-3b90 — the usage snapshot's age, printed beside the number on the
    // account card. zh runs the characters together around the duration and
    // puts 前 after it; en needs a space on both sides.
    ["zh", "monitorMeasuredAgo", ["3d"], "量於 3d 前"],
    ["zh", "monitorMeasuredAgo", ["2h 15m"], "量於 2h 15m 前"],
    ["en", "monitorMeasuredAgo", ["3d"], "measured 3d ago"],
    ["en", "chatInterAgentExpand", [0], "0 messages between agents · expand"],
    ["en", "chatInterAgentExpand", [1], "1 message between agents · expand"],
    ["en", "chatInterAgentExpand", [2], "2 messages between agents · expand"],
    ["en", "chatInterAgentExpand", [17], "17 messages between agents · expand"],
    ["en", "memberForceStopConfirmBody", ["Mira"], "Force-stop Mira immediately — kill the session now, skipping the graceful shutdown. Any unsaved work in progress is lost."],
    ["en", "memberRefocusSince", ["2 天"], "Last refocus 2 天"],
    ["en", "memberMachineMovingTo", ["Alpha"], "→ Moving to Alpha"],
    ["en", "workerMachineMovingTo", ["Alpha"], "→ Moving to Alpha"],
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
    ["en", "machineBootstrapConfirmBody", ["Alpha"], "“Alpha” is online and already running a warden. Installing again OVERWRITES the warden currently in service: every member on this machine is disconnected, and it CANNOT be undone — the replaced warden is not recoverable, the machine has to be installed again and its members brought back online."],
    ["en", "machineUninstallConfirmBody", ["Alpha"], "Uninstall “Alpha”? This asks the warden on that machine to run ocwarden uninstall; on success the machine goes offline, but the record is KEPT (re-installable)."],
    ["en", "machineUninstallWarnBody", ["Alpha",1], "“Alpha” still has 1 member(s) online on it. Uninstalling now tears the warden off the machine while they are still on it — take the related members offline first. Proceed anyway?"],
    ["en", "machineUninstallWarnBody", ["Alpha",4], "“Alpha” still has 4 member(s) online on it. Uninstalling now tears the warden off the machine while they are still on it — take the related members offline first. Proceed anyway?"],
    ["en", "machineDeleteConfirmBody", ["Alpha"], "Delete “Alpha”? Its credentials stop working immediately: the machine can no longer report in, and any agent still assigned to it loses access too. Nothing is torn down on the machine itself (that is “Uninstall”), and this cannot be undone — bringing it back means installing it again."],
    ["en", "costResetConfirmBody", ["$37"], "This resets the accumulated $37 to zero and starts counting again from 0. The figure is not kept anywhere else, so it cannot be recovered."],
    ["en", "accountCostResetConfirmBody", ["$37"], "This resets the account's accumulated $37 to zero and starts counting again from 0. No member's own figure is touched. The figure is not kept anywhere else, so it cannot be recovered."],
    ["en", "themeImportSkipped", [2, ["nav.tasks", "profile.themeOffice"]], "Imported, but 2 wording code(s) were not recognised and were skipped: nav.tasks, profile.themeOffice"],
    ["en", "themeImportSkipped", [30, ["a.b", "c.d", "e.f"]], "Imported, but 30 wording code(s) were not recognised and were skipped: a.b, c.d, e.f …"],
    ["en", "themeDeleteConfirm", ["精靈村"], "Delete theme \"精靈村\"? This cannot be undone."],
    ["en", "deleteRoleConfirm", ["研究員"], "Delete role \"研究員\"? Its members and their conversations and lessons will be removed permanently."],
    ["en", "docHistoryRestoreConfirm", ["7/29 14:03"], "Restore the version from \"7/29 14:03\"? The current content is overwritten, but is kept as a new revision."],
    ["en", "docHistoryBlockedReason", [["Lessons", "SOP"], 10000], "\"Lessons, SOP\" is over the 10000-character limit and no shorter than what is stored now — the server would refuse this restore."],
    ["en", "deleteManualConfirm", ["review-pr"], "Delete the task type “review-pr”? Its manual (definition, SOP, learnings) is removed with it and cannot be restored."],
    ["en", "manualEditSection", ["What is this task?"], "Edit “What is this task?”"],
    ["en", "docHistoryVersionLabel", ["7/29 14:03"], "This version (7/29 14:03)"],
    [
      "en",
      "bootDocNoteHistory",
      [10],
      "Version history keeps the last 10 versions, counted in SAVES rather than in time — a run of small saves pushes the older ones out. Restoring the factory version is never affected and is always available.",
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
        (...a: (string | number | string[] | number[])[]) => string
      >;
      expect(composed[name](...args)).toBe(want);
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
      "確定要終止「修理電梯」嗎？任務將移入已結束區，無法恢復；後端會通知負責人做結束處理。"
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
      "workerDetail.refocusSinceLabel",
      "chat.offlineTitleSuffix",
      "chat.offlineQueueHintLead",
      "chat.offlineQueueHintTail",
      "chat.wakeQueueHintSuffix",
      "chat.composerOfflineSuffix",
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
