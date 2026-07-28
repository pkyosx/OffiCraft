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
import { makeMessages, workerStatusText, effortText } from "./compose";
import { applyWording } from "./wording";
import { MESSAGE_KEYS } from "./messageKeys.generated";
import type { Effort } from "../types";

type Lang = "zh" | "en";
const DICTS: Record<Lang, Dict> = { zh, en };

/** [language, message, arguments, the text that must appear on screen]. */
const EXPECTED: [Lang, string, (string | number | string[])[], string][] = [
    // T-2658: the 任務 tab count's accessible name. Both the ordinary number
    // and the ">99" clamp, because the name must say what the eye sees.
    ["zh", "navOpenTasks", ["7"], "7件未結案"],
    ["zh", "navOpenTasks", ["99+"], "99+件未結案"],
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
    ["zh", "replyWaited", ["3 小時"], "已等你 3 小時"],
    ["zh", "replyOpenedAt", ["7/13 09:05"], "開卡 7/13 09:05"],
    ["zh", "replyAnsweredAt", ["7/13 09:05"], "已回覆 7/13 09:05"],
    ["zh", "replyExpiredAt", ["7/13 09:05"], "已過期 7/13 09:05"],
    ["zh", "replyExpireConfirmBody", ["要不要買新的"], "要把「要不要買新的」標為過期嗎?此動作不可復原、也不算回答——成員會收到通知,問題還在的話他會重新開一張新卡。"],
    ["zh", "outsourceLabel", ["O-7"], "外包 · O-7"],
    ["zh", "workerRefocusSince", ["2 天"], "上次換手 2 天"],
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
    ["zh", "machineOfflineOption", ["Alpha"], "Alpha（離線）"],
    ["zh", "machineBootstrapErrorDetail", ["ocwarden binary missing"], "安裝請求失敗:ocwarden binary missing"],
    ["zh", "machineBootstrapFailed", [3], "安裝失敗(結束碼 3),原因如下:"],
    ["zh", "machineUninstallConfirmBody", ["Alpha"], "確定要解除安裝「Alpha」嗎？這會請該機器上的 warden 執行 ocwarden uninstall;成功後機器會變為離線,但記錄會保留(可再次安裝)。"],
    ["zh", "machineUninstallWarnBody", ["Alpha",1], "「Alpha」上還有 1 位成員在線上。現在解除安裝會在成員仍在這台機器上時把 warden 拆除 —— 建議先將相關成員下線。仍要繼續嗎?"],
    ["zh", "machineUninstallWarnBody", ["Alpha",4], "「Alpha」上還有 4 位成員在線上。現在解除安裝會在成員仍在這台機器上時把 warden 拆除 —— 建議先將相關成員下線。仍要繼續嗎?"],
    ["zh", "machineDeleteConfirmBody", ["Alpha"], "確定要刪除「Alpha」嗎?該機器的憑證會立刻失效:機器無法再回報,還指派在這台機器上的 agent 也會一起失去存取權。機器上的 warden 不會被拆除(那是「解除安裝」),而且這個動作無法復原 —— 要恢復只能重新安裝。"],
    ["zh", "themeImportSkipped", [2, ["nav.tasks", "profile.themeOffice"]], "已匯入,但有2個用詞代碼不認得、已略過:nav.tasks、profile.themeOffice"],
    ["zh", "themeImportSkipped", [30, ["a.b", "c.d", "e.f"]], "已匯入,但有30個用詞代碼不認得、已略過:a.b、c.d、e.f等"],
    ["zh", "themeDeleteConfirm", ["精靈村"], "刪除主題「精靈村」?此動作無法復原。"],
    ["zh", "deleteRoleConfirm", ["研究員"], "確定刪除角色「研究員」？該角色的成員及其對話、學習經驗將一併移除，無法復原。"],
    ["zh", "deleteManualConfirm", ["review-pr"], "確定刪除任務類型「review-pr」？其手冊（定義、SOP、學習經驗）將一併移除，無法復原。"],
    ["en", "navOpenTasks", ["7"], "7 open"],
    ["en", "navOpenTasks", ["99+"], "99+ open"],
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
    ["en", "outsourceLabel", ["O-7"], "Outsource · O-7"],
    ["en", "workerRefocusSince", ["2 天"], "Last handover 2 天"],
    ["en", "chatOfflineTitle", ["Mira"], "Mira is offline"],
    ["en", "chatOfflineQueueHint", ["Mira"], "You can still leave a message — Mira will read it once back online."],
    ["en", "chatWakeQueueHint", ["Mira"], "Mira is offline — your message will queue, or wake them now"],
    ["en", "chatComposerOffline", ["Mira"], "Mira is currently offline"],
    ["en", "chatInterAgentExpand", [0], "0 messages between agents · expand"],
    ["en", "chatInterAgentExpand", [1], "1 message between agents · expand"],
    ["en", "chatInterAgentExpand", [2], "2 messages between agents · expand"],
    ["en", "chatInterAgentExpand", [17], "17 messages between agents · expand"],
    ["en", "memberForceStopConfirmBody", ["Mira"], "Force-stop Mira immediately — kill the session now, skipping the graceful shutdown. Any unsaved work in progress is lost."],
    ["en", "memberRefocusSince", ["2 天"], "Last refocus 2 天"],
    ["en", "machineOfflineOption", ["Alpha"], "Alpha (offline)"],
    ["en", "machineBootstrapErrorDetail", ["ocwarden binary missing"], "Install request failed: ocwarden binary missing"],
    ["en", "machineBootstrapFailed", [3], "Install failed (exit code 3). Reason:"],
    ["en", "machineUninstallConfirmBody", ["Alpha"], "Uninstall “Alpha”? This asks the warden on that machine to run ocwarden uninstall; on success the machine goes offline, but the record is KEPT (re-installable)."],
    ["en", "machineUninstallWarnBody", ["Alpha",1], "“Alpha” still has 1 member(s) online on it. Uninstalling now tears the warden off the machine while they are still on it — take the related members offline first. Proceed anyway?"],
    ["en", "machineUninstallWarnBody", ["Alpha",4], "“Alpha” still has 4 member(s) online on it. Uninstalling now tears the warden off the machine while they are still on it — take the related members offline first. Proceed anyway?"],
    ["en", "machineDeleteConfirmBody", ["Alpha"], "Delete “Alpha”? Its credentials stop working immediately: the machine can no longer report in, and any agent still assigned to it loses access too. Nothing is torn down on the machine itself (that is “Uninstall”), and this cannot be undone — bringing it back means installing it again."],
    ["en", "themeImportSkipped", [2, ["nav.tasks", "profile.themeOffice"]], "Imported, but 2 wording code(s) were not recognised and were skipped: nav.tasks, profile.themeOffice"],
    ["en", "themeImportSkipped", [30, ["a.b", "c.d", "e.f"]], "Imported, but 30 wording code(s) were not recognised and were skipped: a.b, c.d, e.f …"],
    ["en", "themeDeleteConfirm", ["精靈村"], "Delete theme \"精靈村\"? This cannot be undone."],
    ["en", "deleteRoleConfirm", ["研究員"], "Delete role \"研究員\"? Its members and their conversations and lessons will be removed permanently."],
    ["en", "deleteManualConfirm", ["review-pr"], "Delete the task type “review-pr”? Its manual (definition, SOP, learnings) is removed with it and cannot be restored."],
];

describe("makeMessages", () => {
  it.each(EXPECTED)(
    "%s %s renders the same text it did before the fragment split",
    (lang, name, args, want) => {
      const composed = makeMessages(DICTS[lang], lang) as unknown as Record<
        string,
        (...a: (string | number | string[])[]) => string
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
      "nav.openTasksSuffix",
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
      "replies.waitedLabel",
      "replies.openedAtLabel",
      "replies.answeredAtLabel",
      "replies.expiredAtLabel",
      "replies.expireConfirmBodyLead",
      "replies.expireConfirmBodyTail",
      "office.outsource.title",
      "workerDetail.statusOf.assigned",
      "workerDetail.statusOf.active",
      "workerDetail.statusOf.released",
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
      "settings.deleteManualConfirmLead",
      "settings.deleteManualConfirmTail",
    ]) {
      expect(keys.has(code), `${code} must be overridable`).toBe(true);
    }
  });
});

describe("workerStatusText", () => {
  it.each([
    ["zh", "assigned", "已指派"],
    ["zh", "active", "工作中"],
    ["zh", "released", "已釋放"],
    ["en", "assigned", "Assigned"],
    ["en", "active", "Active"],
    ["en", "released", "Released"],
  ] as [Lang, string, string][])(
    "%s %s reads the same as the lookup template did",
    (lang, status, want) => {
      expect(workerStatusText(DICTS[lang], status)).toBe(want);
    }
  );

  it("shows an unknown status verbatim rather than blank", () => {
    expect(workerStatusText(zh, "quarantined")).toBe("quarantined");
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
