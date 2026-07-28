// compose.ts — T-081b lane B: the parameterised messages, composed from
// OVERRIDABLE static fragments.
//
// WHY THIS FILE EXISTS
// A dictionary leaf used to be allowed to be an interpolation FUNCTION
// (`terminateConfirmBody: (title) => \`Terminate “${title}”? …\``). The
// message-key whitelist only admits STRING leaves (a function cannot be
// replaced by a flat string), so every word inside such a template was
// invisible to a theme bundle's `wording` overlay. The symptom the owner hit:
// a theme pack re-worded the 「終止」 menu item and the 「確認終止」 button, and
// the confirmation dialog BODY kept the product's original vocabulary.
//
// So the words moved OUT of the templates into plain string leaves, and the
// assembly moved HERE. `makeMessages(t, language)` closes over the dict the
// provider already laid the wording overlay onto — so a re-worded fragment
// reaches the screen — and every call site calls `m.<name>(…)` instead of
// `t.<ns>.<leaf>(…)`.
//
// THE TWO JOIN SHAPES (both must render byte-identically to the old templates)
//   * LABEL shape — `label + " " + param`. Used only where zh and en BOTH put
//     exactly one space there, so the fragment values stay free of invisible
//     leading/trailing whitespace (a wording editor shows them verbatim).
//   * LEAD/TAIL shape — straight concatenation, each fragment carrying its own
//     punctuation. Used for the sentence splits where a quoted parameter sits
//     mid-sentence: the quote glyphs differ (“” vs 「」) and zh needs a trailing
//     particle (嗎？) that en has no counterpart for, so the two languages are
//     filled independently and the fragments are deliberately NOT parallel.
//
// Where only the SPACING differs between the languages, the join — not the
// value — carries it (`sp` below). Punctuation that belongs to the sentence
// stays inside the (overridable) fragment.

import type { Dict } from "./locales/zh";
import type { Effort } from "../types";

/** The UI languages a wording overlay keys on (mirrors i18n's `Language`;
 * declared locally so this module never imports the provider back). */
type Lang = "zh" | "en";

export interface Messages {
  // ── nav ──
  /** Accessible name for the 任務 tab's open-count (T-2658). The count is a
   *  bare number on screen, so the name is what tells a reader what it counts.
   *  Takes the DISPLAYED string, "99+" included — the name must say the same
   *  thing the eye sees, not the un-clamped total. */
  navOpenTasks: (count: string) => string;
  // ── tasks ──
  taskProgress: (done: number, total: number) => string;
  taskElapsed: (elapsed: string) => string;
  taskPlanningBy: (name: string) => string;
  taskBlockedBy: (taskNo: string) => string;
  taskBlockedByMissing: (depId: string) => string;
  taskCopyTaskNo: (taskNo: string) => string;
  taskTerminateConfirmBody: (title: string) => string;
  taskMarkDuplicateBody: (taskNo: string) => string;
  taskDuplicateOf: (taskNo: string) => string;
  taskReassignTitle: (taskNo: string) => string;
  // ── replies ──
  replyWaited: (elapsed: string) => string;
  replyOpenedAt: (time: string) => string;
  replyAnsweredAt: (time: string) => string;
  replyExpiredAt: (time: string) => string;
  replyExpireConfirmBody: (summary: string) => string;
  // ── office ──
  outsourceLabel: (codename: string) => string;
  // ── worker detail ──
  workerRefocusSince: (elapsed: string) => string;
  // ── chat ──
  chatOfflineTitle: (name: string) => string;
  chatOfflineQueueHint: (name: string) => string;
  chatWakeQueueHint: (name: string) => string;
  chatComposerOffline: (name: string) => string;
  chatInterAgentExpand: (count: number) => string;
  // ── member panel ──
  memberForceStopConfirmBody: (name: string) => string;
  memberRefocusSince: (elapsed: string) => string;
  // ── machine picker ──
  machineOfflineOption: (name: string) => string;
  // ── monitor › machines ──
  machineBootstrapErrorDetail: (detail: string) => string;
  machineBootstrapFailed: (exitCode: number) => string;
  machineUninstallConfirmBody: (name: string) => string;
  machineUninstallWarnBody: (name: string, count: number) => string;
  machineDeleteConfirmBody: (name: string) => string;
  // ── settings ──
  themeImportSkipped: (count: number, sample: string[]) => string;
  themeDeleteConfirm: (name: string) => string;
  deleteRoleConfirm: (name: string) => string;
  deleteManualConfirm: (key: string) => string;
}

/** Build the composed messages for one (already wording-overlaid) dict. */
export function makeMessages(t: Dict, language: Lang): Messages {
  // The separator between a parameter and an adjacent fragment where the two
  // languages disagree: en writes a space, zh runs the characters together.
  const sp = language === "zh" ? "" : " ";
  const tasks = t.tasks;
  const replies = t.replies;
  const chat = t.chat;
  const mp = t.mp;
  const mach = t.monitor.machine;
  const set = t.settings;
  const prof = t.profile;
  // The list separator between two codes: a join, not vocabulary.
  const listSep = language === "zh" ? "、" : ", ";
  return {
    navOpenTasks: (count) => `${count}${sp}${t.nav.openTasksSuffix}`,

    taskProgress: (done, total) =>
      `${tasks.progressLabel} ${done}/${total}`,
    // 「步驟 N/M · 已歷時 X」 is ONE visible string. Leaving elapsed as a template
    // left 步驟 overridable and 已歷時 not, inside that one sentence.
    taskElapsed: (elapsed) => `${tasks.elapsedLabel} ${elapsed}`,
    taskPlanningBy: (name) =>
      `${tasks.planningByLead} ${name} ${tasks.planningByTail}`,
    taskBlockedBy: (taskNo) => `${tasks.blockedByLabel} ${taskNo}`,
    taskBlockedByMissing: (depId) =>
      `${tasks.blockedByLabel} ${depId}${sp}${tasks.blockedByMissingSuffix}`,
    taskCopyTaskNo: (taskNo) => `${tasks.copyTaskNoLabel} ${taskNo}`,
    taskTerminateConfirmBody: (title) =>
      `${tasks.terminateConfirmBodyLead}${title}${tasks.terminateConfirmBodyTail}`,
    taskMarkDuplicateBody: (taskNo) =>
      `${tasks.markDuplicateBodyLead}${taskNo}${tasks.markDuplicateBodyTail}`,
    taskDuplicateOf: (taskNo) => `${tasks.duplicateOfLabel} ${taskNo}`,
    taskReassignTitle: (taskNo) => `${tasks.reassignTitleLabel} ${taskNo}`,

    replyWaited: (elapsed) => `${replies.waitedLabel} ${elapsed}`,
    replyOpenedAt: (time) => `${replies.openedAtLabel} ${time}`,
    replyAnsweredAt: (time) => `${replies.answeredAtLabel} ${time}`,
    replyExpiredAt: (time) => `${replies.expiredAtLabel} ${time}`,
    replyExpireConfirmBody: (summary) =>
      `${replies.expireConfirmBodyLead}${summary}${replies.expireConfirmBodyTail}`,

    // The outsource identity label has ONE source of the word 外包 now: the
    // section title. It used to have two (a title leaf and an identically
    // worded template), so a theme could re-word one and not the other.
    outsourceLabel: (codename) =>
      `${t.office.outsource.title} · ${codename}`,

    workerRefocusSince: (elapsed) =>
      `${t.workerDetail.refocusSinceLabel} ${elapsed}`,

    chatOfflineTitle: (name) => `${name} ${chat.offlineTitleSuffix}`,
    chatOfflineQueueHint: (name) =>
      `${chat.offlineQueueHintLead}${sp}${name} ${chat.offlineQueueHintTail}`,
    chatWakeQueueHint: (name) => `${name} ${chat.wakeQueueHintSuffix}`,
    chatComposerOffline: (name) => `${name} ${chat.composerOfflineSuffix}`,
    // Only the plural BRANCH is code; both branches' words are overridable.
    chatInterAgentExpand: (count) =>
      `${count} ${count === 1 ? chat.interAgentExpandOne : chat.interAgentExpandMany}`,

    memberForceStopConfirmBody: (name) =>
      `${mp.forceStopConfirmBodyLead} ${name}${sp}${mp.forceStopConfirmBodyTail}`,
    memberRefocusSince: (elapsed) => `${mp.refocusSinceLabel} ${elapsed}`,

    machineOfflineOption: (name) =>
      `${name}${sp}${t.machine.picker.offlineOptionSuffix}`,

    // Reuses the plain bootstrapError sentence; only the colon differs between
    // the languages, and punctuation like that is a join, not vocabulary.
    machineBootstrapErrorDetail: (detail) =>
      `${mach.bootstrapError}${language === "zh" ? ":" : ": "}${detail}`,
    machineBootstrapFailed: (exitCode) =>
      `${mach.bootstrapFailedLead}${exitCode}${mach.bootstrapFailedTail}`,
    machineUninstallConfirmBody: (name) =>
      `${mach.uninstallConfirmBodyLead}${name}${mach.uninstallConfirmBodyTail}`,
    machineUninstallWarnBody: (name, count) =>
      `${mach.uninstallWarnBody1}${name}${mach.uninstallWarnBody2}${count}${mach.uninstallWarnBody3}`,
    machineDeleteConfirmBody: (name) =>
      `${mach.deleteConfirmBodyLead}${name}${mach.deleteConfirmBodyTail}`,

    // `sample` is the SHORT head of the skipped set, never the whole thing; the
    // count carries the rest and the trailing marker says it was cut.
    themeImportSkipped: (count, sample) =>
      `${prof.themeImportSkippedLead}${sp}${count}${sp}${prof.themeImportSkippedMid}${sp}` +
      sample.join(listSep) +
      (sample.length < count ? `${sp}${prof.themeImportSkippedMore}` : ""),

    themeDeleteConfirm: (name) =>
      `${set.themeDeleteConfirmLead}${name}${set.themeDeleteConfirmTail}`,
    deleteRoleConfirm: (name) =>
      `${set.deleteRoleConfirmLead}${name}${set.deleteRoleConfirmTail}`,
    deleteManualConfirm: (key) =>
      `${set.deleteManualConfirmLead}${key}${set.deleteManualConfirmTail}`,
  };
}

/** The two former lookup-map leaves are plain object leaves now (the shape
 * tasks.status / tasks.priority already use), so each entry is individually
 * overridable. These readers keep the honest fallbacks the templates had. */
export function workerStatusText(t: Dict, status: string): string {
  return t.workerDetail.statusOf[status] ?? status;
}

export function effortText(t: Dict, effort: Effort): string {
  return t.mp.effortOf[effort];
}
