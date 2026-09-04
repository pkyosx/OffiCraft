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
  // ── task artifact versions (T-60) ──
  taskArtifactVersionCount: (n: number) => string;
  taskArtifactVersionLabel: (when: string) => string;
  taskArtifactVersionBy: (actorId: string) => string;
  taskArtifactOpaque: (mime: string) => string;
  // ── login / the credential-attempt brake ──
  loginThrottled: (secs: number) => string;
  // ── replies ──
  replyWaited: (elapsed: string) => string;
  replyOpenedAt: (time: string) => string;
  replyAnsweredAt: (time: string) => string;
  replyExpiredAt: (time: string) => string;
  replyExpireConfirmBody: (summary: string) => string;
  replySelectedCount: (n: number) => string;
  replyPickedOptions: (options: string[]) => string;
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
  memberMachineMovingTo: (machine: string) => string;
  agentPendingChange: (value: string) => string;
  workerMachineMovingTo: (machine: string) => string;
  /** `by` is the deadline text, or null when the wind-down is on NO clock —
   * which since T-ed79 is every cause except the second context threshold. The
   * no-clock sentence must not contain any time at all. */
  agentWindDownForChange: (by: string | null) => string;
  agentWindDownOnDeadline: (by: string | null) => string;
  // ── RESUME SUMMARY · the COLLAPSE marker (T-8b0d follow-up) ──
  // 🔴 There is deliberately NO composer for the TRUNCATION marker beside it.
  // A shared composer is exactly how the two would drift into sharing a word,
  // and "this message is folded" vs "older messages are not here at all" are
  // the two statements a reader must never confuse. The cut point is a plain
  // label leaf plus the SERVER's own hint, shown verbatim.
  resumeBodyOmitted: (chars: number) => string;
  // ── 定期訊息 · custom cadence (T-49e7) ──
  schedCustomMonths: (months: number[]) => string;
  schedCustomDays: (days: number[]) => string;
  schedCustomHours: (hours: number[]) => string;
  schedCustomMinutes: (minutes: number[]) => string;
  schedCustomSummary: (
    months: number[],
    days: number[],
    hours: number[],
    minutes: number[]
  ) => string;
  schedMinuteStep: (step: number) => string;
  // ── machine picker ──
  machineOfflineOption: (name: string) => string;
  // ── monitor › accounts ──
  monitorMeasuredAgo: (age: string) => string;
  // ── monitor › machines ──
  machineBootstrapErrorDetail: (detail: string) => string;
  machineBootstrapFailed: (exitCode: number) => string;
  machineBootstrapConfirmBody: (name: string) => string;
  machineUninstallConfirmBody: (name: string) => string;
  machineUninstallWarnBody: (name: string, count: number) => string;
  machineDeleteConfirmBody: (name: string) => string;
  /** 成本歸零 confirm. Takes the RENDERED amount (already through formatCost, or
   * the dash) rather than a number: the panel has it, and a second formatting
   * site is a second place for the rounding rule to drift. */
  costResetConfirmBody: (amount: string) => string;
  accountCostResetConfirmBody: (amount: string) => string;
  // ── settings ──
  themeImportSkipped: (count: number, sample: string[]) => string;
  themeDeleteConfirm: (name: string) => string;
  deleteRoleConfirm: (name: string) => string;
  docHistoryRestoreConfirm: (when: string) => string;
  docHistoryBlockedReason: (fields: string[], cap: number) => string;
  docHistoryVersionLabel: (when: string) => string;
  docHistoryActor: (name: string, actorId: string) => string;
  bootDocNoteHistory: (kept: number) => string;
  docOverCap: (size: number, cap: number) => string;
  deleteManualConfirm: (key: string) => string;
  manualEditSection: (section: string) => string;
  // ── diff ──
  diffTooLarge: (lines: number) => string;
}

/** The interval a minute set IS, or `null` when it is not one (T-49e7 round 2).
 *
 * A set qualifies only when it tiles the whole hour: it starts at minute 0, the
 * gap between neighbours never changes, and that gap divides 60 — so the last
 * value wraps onto the next hour's 0 at the same spacing. {0,20,40} qualifies
 * (every 20 minutes, forever); {15,35,55} does NOT, because 55 → 15 is a gap of
 * 20 but the phrase 「每 20 分鐘」 gives a reader no way to know the offset, and
 * {0,20,45} does not because it is not evenly spaced at all.
 *
 * 🔴 Two values minimum. A single tick has no gap to measure, and calling {7}
 * "every 60 minutes" would rewrite a choice as a statement its author never
 * made. */
export function evenMinuteStep(minutes: number[]): number | null {
  if (minutes.length < 2) return null;
  if (minutes[0] !== 0) return null;
  if (60 % minutes.length !== 0) return null;
  const step = 60 / minutes.length;
  for (let i = 0; i < minutes.length; i++) {
    if (minutes[i] !== i * step) return null;
  }
  return step;
}

/** Build the composed messages for one (already wording-overlaid) dict. */
export function makeMessages(t: Dict, language: Lang): Messages {
  // The separator between a parameter and an adjacent fragment where the two
  // languages disagree: en writes a space, zh runs the characters together.
  const sp = language === "zh" ? "" : " ";
  const tasks = t.tasks;
  const login = t.login;
  const replies = t.replies;
  const chat = t.chat;
  const mp = t.mp;
  const sched = t.mp.schedmsg;
  const resume = t.mp.resumeSummary;
  const mon = t.monitor;
  const mach = t.monitor.machine;
  const set = t.settings;
  const prof = t.profile;
  const diff = t.diff;
  // The list separator between two codes: a join, not vocabulary.
  const listSep = language === "zh" ? "、" : ", ";
  /** How many numbers a scattered `custom` set prints before the rest is
   * carried by a count (owner ruling, T-49e7 round 2: 「最多列 4 個」). */
  const LIST_CAP = 4;
  /** Render one `custom` set as at most LIST_CAP numbers plus, when there are
   * more, a phrase naming HOW MANY WERE NOT PRINTED. The tail sits inside the
   * listed phrase's own tail ("第 0、7、13、22 分" + "等,另 2 個") so the
   * sentence still reads as one clause in either language. The zh wording says
   * 另 rather than a bare 等 N 個 because that idiom counts the TOTAL; the
   * count here is the remainder, the same thing en's "and N more" says. */
  const cappedList = (values: number[], lead: string, tail: string): string => {
    const shown = values.slice(0, LIST_CAP);
    const listed = `${lead}${shown.join(listSep)}${tail}`;
    if (values.length <= LIST_CAP) return listed;
    const rest = values.length - shown.length;
    return `${listed}${sp}${sched.customMoreLead}${rest}${sched.customMoreTail}`;
  };
  // Named rather than returned inline: the row summary is literally the three
  // group phrases joined, so it composes them through this same object instead
  // of keeping a second copy of the every-set/empty-set rules.
  const messages: Messages = {
    // 「量於 3d 前」/「measured 3d ago」— the age rides beside the usage number
    // so a frozen snapshot can never read as a live one (T-3b90).
    //
    // BOTH languages put a space on either side of the duration, so this is the
    // LABEL shape twice over and the separator is a plain literal. It is NOT
    // written with `sp`: that variable is `"" | " "`, so `sp || " "` collapses
    // to a constant `" "` while READING as if zh were spaceless — a comment and
    // an expression that quietly disagree with the pinned zh expectation
    // (「量於 3d 前」, with spaces). Caught in independent review.
    monitorMeasuredAgo: (age) =>
      `${mon.measuredAgoLead} ${age} ${mon.measuredAgoTail}`,
    // 「3版」/「3 versions」 — the artifact row's versions entry (T-60). Only
    // ever printed for n > 1 (one version is not a history), so the tail needs
    // no singular twin.
    taskArtifactVersionCount: (n) =>
      `${n}${sp}${tasks.artifacts.versionsCountTail}`,
    // Which version the `-` side of a comparison IS. Same reason the document
    // reader names its own: two unlabelled columns do not say which is which.
    taskArtifactVersionLabel: (when) =>
      `${tasks.artifacts.versionsVersionLabel} ${when}`,
    // The raw actor id, never a resolved display name — this panel holds no
    // roster, and inventing a name for an id it cannot resolve would misname
    // whoever replaced the deliverable.
    taskArtifactVersionBy: (actorId) =>
      `${tasks.artifacts.versionsByLabel} ${actorId}`,
    // A file this panel can neither render nor compare. It names the mime the
    // SERVER reported rather than calling the version empty.
    taskArtifactOpaque: (mime) =>
      `${tasks.artifacts.versionsOpaqueLead}${mime}${tasks.artifacts.versionsOpaqueTail}`,
    taskProgress: (done, total) =>
      `${tasks.progressLabel} ${done}/${total}`,
    // 「步驟 N/M · 已歷時 X」 is ONE visible string. Leaving elapsed as a template
    // left 步驟 overridable and 已歷時 not, inside that one sentence.
    // The 429 wait. The space AFTER the number is language-specific and is the
    // OPPOSITE of `sp`: zh writes 「請於 42 秒後再試。」 (spaces both sides), en
    // writes "in 42s." (none). It cannot reuse `sp`, and it is not baked into
    // the tail fragment because a wording editor renders fragments verbatim and
    // a leading space there would be invisible to whoever edits it.
    loginThrottled: (secs) =>
      `${login.throttledLead} ${secs}${language === "zh" ? " " : ""}${login.throttledTail}`,
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
    // How many options are ticked on a multi-select card. LITERAL spaces on
    // both sides of the number — deliberately NOT `sp`: zh writes 「已選 2 項」
    // with the digit spaced off the han characters exactly as en writes
    // "Selected 2 options", so this is not one of the joins that differ by
    // language and must not be routed through the variable that says they do.
    replySelectedCount: (n) =>
      `${replies.selectedCountLead} ${n} ${
        n === 1 ? replies.selectedCountTailOne : replies.selectedCountTailMany
      }`,
    // Every circled option on one line (the collapsed task-card row). Joined by
    // the locale's own list separator — the same one every other list in this
    // file uses, so a multi-select decision is punctuated like a list and not
    // like a sentence someone concatenated.
    replyPickedOptions: (options) => options.join(listSep),

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
    // LABEL shape: zh and en both put exactly one space before the machine name.
    memberMachineMovingTo: (machine) => `${mp.machineMovingToLabel} ${machine}`,
    // The other three cells: same LABEL shape, one space before the value.
    agentPendingChange: (value) => `${mp.pendingChangeLabel} ${value}`,
    // Machines get 「→ 要換到」 (a place) rather than 「→ 要換成」 (a value) —
    // the wording the member panel has always used, now on both panels.
    workerMachineMovingTo: (machine) => `${mp.machineMovingToLabel} ${machine}`,
    // 「正在收尾以套用你的改動 · 最晚 14:32 生效」 — the deadline is a CEILING
    // (the collect fires as soon as the agent reports stopped), so the wording
    // says 最晚 rather than promising a time.
    //
    // 🔴 `by === null` is the NORMAL case since T-ed79, not a degenerate one:
    // relocate and runtime/model are 停止 (no clock), so refocus_deadline
    // arrives as 0 → null and there is no time to quote. The label alone IS the
    // sentence then — appending 「最晚 … 生效」 with a placeholder, or falling
    // back to the 「上次重新聚焦」 history line, would both put back the
    // misreading T-7f28 removed. Nothing time-shaped may appear in this arm.
    agentWindDownForChange: (by) =>
      by === null
        ? mp.windDownForChangeLabel
        : `${mp.windDownForChangeLabel}${sp}·${sp}${mp.windDownByLabel} ${by} ${mp.windDownEffectSuffix}`,

    // 「正在收尾，已給死線 · 最晚 14:32 生效」 — the two CLOCKED causes
    // (accelerated_stop, context_high). Same 最晚 wording as above and for the
    // same reason: the collect fires the instant the agent reports stopped, so
    // the time is a ceiling. 🔴 Unlike the arm above, `by === null` here is NOT
    // an ordinary answer — a clocked cause always carries a deadline — so the
    // fallback exists only so a stale/absent field degrades to a true sentence
    // rather than a placeholder time.
    agentWindDownOnDeadline: (by) =>
      by === null
        ? mp.windDownDeadlineLabel
        : `${mp.windDownDeadlineLabel}${sp}·${sp}${mp.windDownByLabel} ${by} ${mp.windDownEffectSuffix}`,

    // MARK shape, not sentence shape: one word plus the count, and nothing
    // else. What "folded" MEANS is stated once per chat block
    // (`bodyOmittedNote`) instead of once per message — the template used to be
    // repeated under every folded row and cost more than the folds saved
    // (owner, 2026-08-13).
    //
    // 🔴 A LITERAL SPACE, deliberately NOT `sp`. `sp` is "" for zh, which is
    // right where a fragment runs into CJK punctuation — and wrong here. Seen
    // on the trial station it rendered 「折起188」, where the numeral abuts the
    // word and the whole thing reads as one token. A space between CJK text and
    // a Latin numeral is correct in both languages, so this separator is not
    // locale-dependent and must not be routed through `sp`.
    resumeBodyOmitted: (chars) => `${resume.bodyOmittedMark} ${chars}`,

    // 定期訊息 · 自訂頻率 (T-49e7 round 2). The whole-set day phrase reuses the
    // cadence menu's own 每天 / Daily rather than keeping a second word for the
    // same idea — one leaf, so a theme that re-words it cannot re-word only
    // half. Each of the four phrases stands ALONE under its own group heading,
    // so none of them may borrow grammar from a neighbour; the row summary is
    // those same four joined by a separator, which is a join and not
    // vocabulary.
    //
    // 🔴 Three shapes, in this order (owner ruling, round 2):
    //   whole set     say it in words — 每個月/每天/每小時 — never 12, 31 or 24
    //                 numbers.
    //   even interval (minutes only) 「每 N 分鐘」. Ticking 0, 20, 40 IS every
    //                 twenty minutes, and that is the sentence the owner reads
    //                 the schedule back as.
    //   scattered     list at most LIST_CAP of them and let 等 N 個 carry the
    //                 REST. A row summary that prints eleven numbers is a row
    //                 summary nobody reads.
    // An EMPTY set still says so out loud — it is a refusable state, never a
    // silent "all".
    schedCustomMonths: (months) =>
      months.length === 0
        ? sched.customNone
        : months.length === 12
          ? sched.customEveryMonth
          : cappedList(months, sched.customMonthsLead, sched.customMonthsTail),
    schedCustomDays: (days) =>
      days.length === 0
        ? sched.customNone
        : days.length === 31
          ? sched.cadenceDaily
          : cappedList(days, sched.customDaysLead, sched.customDaysTail),
    schedCustomHours: (hours) =>
      hours.length === 0
        ? sched.customNone
        : hours.length === 24
          ? sched.customEveryHour
          : cappedList(hours, sched.customHoursLead, sched.customHoursTail),
    schedCustomMinutes: (minutes) => {
      if (minutes.length === 0) return sched.customNone;
      if (minutes.length === 60) return sched.customEveryMinute;
      const step = evenMinuteStep(minutes);
      if (step !== null) return messages.schedMinuteStep(step);
      return cappedList(
        minutes,
        sched.customMinutesLead,
        sched.customMinutesTail
      );
    },
    // 🔴 The ROW summary drops the month phrase when the whole year is
    // selected — and ONLY there. Migration 00053 backfilled every existing row
    // with all twelve, so 「每個月」 would lead almost every row while saying
    // nothing. The other three groups keep the every-set phrase: 每天/每小時 sit
    // beside a neighbour that narrows them, while a full year narrows nothing.
    // The GROUP phrase (`schedCustomMonths`, printed under 幾月) is untouched —
    // there the reader is choosing months and needs to be told what is chosen.
    //
    // 🔴 An EMPTY month set stays loud, and it is NAMED: once the full year is
    // omitted the leading phrase is no longer "the months slot", so a bare
    // 尚未選擇 would read as any group's. Empty is a 422 and may never look like
    // 全年.
    schedCustomSummary: (months, days, hours, minutes) => {
      const rest = [
        messages.schedCustomDays(days),
        messages.schedCustomHours(hours),
        messages.schedCustomMinutes(minutes),
      ];
      if (months.length === 12) return rest.join(" · ");
      const head =
        months.length === 0
          ? `${sched.customMonthsLabel}${language === "zh" ? ":" : ": "}${sched.customNone}`
          : messages.schedCustomMonths(months);
      return [head, ...rest].join(" · ");
    },
    schedMinuteStep: (step) =>
      `${sched.customStepLead}${step}${sched.customStepTail}`,

    machineOfflineOption: (name) =>
      `${name}${sp}${t.machine.picker.offlineOptionSuffix}`,

    // Reuses the plain bootstrapError sentence; only the colon differs between
    // the languages, and punctuation like that is a join, not vocabulary.
    machineBootstrapErrorDetail: (detail) =>
      `${mach.bootstrapError}${language === "zh" ? ":" : ": "}${detail}`,
    machineBootstrapFailed: (exitCode) =>
      `${mach.bootstrapFailedLead}${exitCode}${mach.bootstrapFailedTail}`,
    machineBootstrapConfirmBody: (name) =>
      `${mach.bootstrapConfirmBodyLead}${name}${mach.bootstrapConfirmBodyTail}`,
    machineUninstallConfirmBody: (name) =>
      `${mach.uninstallConfirmBodyLead}${name}${mach.uninstallConfirmBodyTail}`,
    machineUninstallWarnBody: (name, count) =>
      `${mach.uninstallWarnBody1}${name}${mach.uninstallWarnBody2}${count}${mach.uninstallWarnBody3}`,
    machineDeleteConfirmBody: (name) =>
      `${mach.deleteConfirmBodyLead}${name}${mach.deleteConfirmBodyTail}`,
    costResetConfirmBody: (amount) =>
      `${t.mp.costResetConfirmBodyLead}${amount}${t.mp.costResetConfirmBodyTail}`,
    // The ACCOUNT's own figure (T-53, owner ruling rc-5c5d7c7c6dcd). Its own
    // pair of strings rather than the member one with a different noun: this
    // sentence has to say that no member figure is touched, which is the whole
    // difference the owner asked for and the thing he checks before pressing.
    accountCostResetConfirmBody: (amount) =>
      `${t.monitor.costResetConfirmBodyLead}${amount}${t.monitor.costResetConfirmBodyTail}`,

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
    // `when` is the revision's own timestamp — the only thing that tells two
    // retained versions of the same doc apart in one sentence.
    docHistoryRestoreConfirm: (when) =>
      `${set.historyRestoreConfirmLead}${when}${set.historyRestoreConfirmTail}`,
    // Names the FIELDS that breach the cap, not just "this revision" — a task
    // manual caps two of them and the owner cannot act on an unnamed one.
    docHistoryBlockedReason: (fields, cap) =>
      `${set.historyBlockedReasonLead}${fields.join(listSep)}` +
      `${set.historyBlockedReasonMid}${cap}${set.historyBlockedReasonTail}`,
    deleteManualConfirm: (key) =>
      `${set.deleteManualConfirmLead}${key}${set.deleteManualConfirmTail}`,
    // 任務定義 has THREE identical-looking 編輯 buttons since the blocks became
    // separately editable — the accessible name has to carry which block, and
    // the block's own question is the only label the reader already knows.
    manualEditSection: (section) =>
      `${set.manualEditSectionLead}${section}${set.manualEditSectionTail}`,
    // The `-` side of the history diff. Naming the version by its timestamp is
    // what stops the two columns reading as an unlabelled before/after pair —
    // the reader has to know WHICH version they are looking at.
    docHistoryVersionLabel: (when) =>
      `${set.historyVersionLabelLead}${when}${set.historyVersionLabelTail}`,
    // Name AND id, never the name alone: a display name is editable and gets
    // reused, while the id is what the history row was actually written under.
    // Callers pass name="" for an actor the roster cannot resolve (dismissed
    // member, released outsource worker, the owner himself) — then the id
    // stands alone rather than wearing empty brackets.
    docHistoryActor: (name, actorId) =>
      name
        ? `${name}${set.historyActorLead}${actorId}${set.historyActorTail}`
        : actorId,

    // The retention number is a PARAMETER, not a word in the sentence: it is
    // the adapter's own cap and the sentence is wrong the moment the two
    // disagree. Saying "counted in saves" here rather than in a tooltip is the
    // point — an owner who reads it as "the last 10 days" will read a normal
    // afternoon of edits as the cockpit losing his work.
    bootDocNoteHistory: (kept) =>
      `${set.bootDocNoteHistoryLead}${kept}${set.bootDocNoteHistoryTail}`,
    // BOTH numbers, always. "Too long" without the current size gives an owner
    // nothing to act on, and without the limit he cannot tell how far over he
    // is — that is the difference between this and a silent truncation.
    docOverCap: (size, cap) =>
      `${set.docOverCapLead}${size}${set.docOverCapMid}${cap}${set.docOverCapTail}`,

    // Reports the longer side's line count, the only number the refused diff
    // still knows (lib/lineDiff returns the counts even when it declines).
    diffTooLarge: (lines) => `${diff.tooLargeLead}${lines}${diff.tooLargeTail}`,
  };
  return messages;
}

/** The former lookup-map leaf is a plain object leaf now (the shape
 * tasks.status / tasks.priority already use), so each entry is individually
 * overridable. This reader keeps the honest fallback the template had.
 * (Its twin `workerStatusText` retired with the 外包 狀態 cell — T-7526.) */
export function effortText(t: Dict, effort: Effort): string {
  return t.mp.effortOf[effort];
}
