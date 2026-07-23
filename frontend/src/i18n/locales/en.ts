import type { Dict } from "./zh";
import type { Effort } from "../../types";

// Day-divider label pieces (chat.dateOn / dateOnYear): index 0 = Sunday /
// January, matching Date#getDay() and 1-based month - 1.
const WEEKDAYS_EN = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const MONTHS_EN = [
  "Jan", "Feb", "Mar", "Apr", "May", "Jun",
  "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

export const en: Dict = {
  orgName: "AI Office",
  user: "CEO (You)",
  common: {
    apply: "Apply",
    cancel: "Cancel",
  },
  nav: {
    office: "Office",
    officeUnread: "Unread messages",
    replies: "Ask",
    tasks: "Task",
    monitor: "Monitor",
    // 使用說明 — the rightmost main nav tab (owner: it belongs next to Monitor,
    // not buried in Settings). Separate key from the page title on purpose: a
    // tab label has to stay short.
    guide: "Guide",
    // Top-left logo = home entry (aria-label/title).
    home: "Home",
  },
  // ── User guide (the embedded product docs) ──
  // Promoted out of the settings namespace when it became a top-level tab.
  guide: {
    title: "User guide",
    loadError: "Failed to load the user guide. Please try again.",
    empty: "No guide pages yet",
  },
  // ── Tasks page (M3 task cards) ──
  tasks: {
    title: "Tasks",
    openTitle: "Open",
    closedTitle: "Closed",
    emptyNone: "No tasks yet",
    emptyFiltered: "No tasks match the current filters",
    loadError: "Failed to load tasks. Please try again.",
    clearFilters: "Clear filters",
    filterExecutorAll: "Everyone",
    filterTypeAll: "All types",
    filterStatusAll: "All statuses",
    // Multi-select summary nouns — the "· N" form when 2+ are picked (T-be18).
    filterExecutorNoun: "Assignees",
    filterTypeNoun: "Types",
    filterStatusNoun: "Statuses",
    outsource: "Outsource",
    unassigned: "Unassigned",
    adhoc: "Ad-hoc",
    // Card-head label column (T-705e): equal-width labels, chip values. The
    // ☑ #T-xxxx id badge sits on the badge row (v2) — no field label.
    typeLabel: "Task type",
    assigneeLabel: "Assignee",
    creatorLabel: "Creator",
    keyLabel: "Identity key",
    // A pre-column task has no creator → render "—", not clickable.
    creatorUnknown: "—",
    typeSettingsLink: "Open task-type settings",
    messageAssignee: "Message the assignee",
    messageCreator: "Message the creator",
    previousAssigneeLabel: "Handed over from",
    messagePreviousAssignee: "Message the predecessor",
    effortOf: {
      low: "low effort",
      medium: "mid effort",
      high: "high effort",
    } as Record<string, string>,
    status: {
      not_started: "Not started",
      in_progress: "In progress",
      waiting_owner: "Awaiting my reply",
      waiting_external: "Waiting on external",
      done: "Done",
      terminated: "Terminated",
      duplicated: "Duplicate",
    } as Record<string, string>,
    // 轉派中 LOCK overlay badge (T-9ca5): orthogonal to status — a reassigned
    // task keeps its derived status and carries this until the new executor
    // claims it. `reassigning` is no longer a status value.
    lockReassigning: "Reassigning",
    priority: {
      high: "High",
      mid: "Mid",
      low: "Low",
      frozen: "Frozen",
    } as Record<string, string>,
    stepStatus: {
      pending: "To do",
      in_progress: "In progress",
      done: "Done",
      waiting_owner: "Awaiting my reply",
      // Same wording as task-level status.waiting_external and the special
      // stepWaitingExternal badge (T-6f11) — the map entry is the backstop so
      // no path can leak the raw key.
      waiting_external: "Waiting on external",
      superseded: "Superseded",
    } as Record<string, string>,
    // Step-level external-wait badge (T-9ca5): the step's own 等待外部, distinct
    // from the owner-facing 等我回覆.
    stepWaitingExternal: "Waiting on external",
    gateAnnounced: "Awaiting my reply",
    stepCardAnswered: "Answered",
    stepCardExpired: "Expired",
    progress: (done: number, total: number) => `Step ${done}/${total}`,
    elapsed: (t: string) => `Elapsed ${t}`,
    expandCard: "Expand workflow",
    collapseCard: "Collapse workflow",
    workflow: "Workflow",
    dod: "DoD",
    parallel: (n: number) => `In parallel · ${n} items`,
    waitingAssign: "Awaiting assignment",
    planningBy: (name: string) => `Waiting for ${name} to create steps`,
    stepsLoading: "Loading…",
    stepsLoadError: "Couldn't load the workflow.",
    stepsRetry: "Retry",
    waitingLabel: "Waiting",
    blockedBy: (taskNo: string) => `Waiting on ${taskNo}`,
    // T-1d82: a dep row whose task cannot be resolved (deleted / bad id). Keeps
    // the raw id — it is the only handle left — but says plainly that there is
    // nothing to open, so the row is not mistaken for a broken link.
    blockedByMissing: (depId: string) => `Waiting on ${depId} (task not found)`,
    depJump: (taskNo: string) => `Open ${taskNo}`,
    openKeyLink: "Open link",
    messagePlaceholder: (name: string) => `Message ${name}…`,
    send: "Send",
    messageError: "Failed to send the message. Please try again.",
    statusMenuLabel: "Status actions",
    priorityLabel: "Priority",
    // Click-to-copy task-no chip (owner 2026-07-19).
    copyTaskNo: (taskNo: string) => `Copy task number ${taskNo}`,
    taskNoCopied: "Copied",
    // Jump to the embedded 等我回覆 reply card. Since v5 this is an ITEM in the
    // status dropdown (owner's informed ruling — the old one-click badge jump
    // is now two steps).
    statusJump: "Show the waiting reply card",
    // Jump to the waiting_external STEP (T-c514, owner 2026-07-20). Same family
    // as statusJump — both mean "take me to where this is stuck" — so the two
    // sit together at the TOP of the menu. This one only became necessary once
    // T-c514 removed the task-level reason: the reason now lives in the step
    // alone, which turns navigating to it from convenience into a requirement.
    statusJumpExternal: "Show the waiting-external step",
    terminate: "Terminate",
    terminateConfirmBody: (title: string) =>
      `Terminate “${title}”? The task moves to Closed and cannot be resumed; the backend will notify the executor to wind it down.`,
    terminateConfirm: "Terminate",
    // Mark duplicate (T-02c9): the executor points at the original and closes it
    markDuplicate: "Mark duplicate",
    markDuplicateBody: (taskNo: string) =>
      `Mark “${taskNo}” a duplicate of another task? It moves to Closed and cannot be resumed. Pick the original:`,
    markDuplicatePick: "Select the original task",
    markDuplicateConfirm: "Mark duplicate",
    duplicateOf: (taskNo: string) => `Duplicate of ${taskNo}`,
    duplicateJump: "Jump to the original",
    actionError: "The action failed. Please try again.",
    // Reassign (T-160e, owner + assistant only): hand the task to another staff
    // member, or mint a fresh outsource worker on the spot (the same model /
    // effort / machine knobs the task type's assignee carries). The task enters
    // Reassigning and BOTH sides are notified; the new executor reports it back
    // to in-progress themselves — the FE never flips it.
    reassign: "Reassign…",
    reassignTitle: (taskNo: string) => `Reassign ${taskNo}`,
    reassignBody:
      "The task moves to Reassigning and both sides are notified to hand over. The new executor reports it back to in-progress once they have read the handover.",
    reassignToMember: "To a member",
    reassignToOutsource: "To outsource",
    reassignPickMember: "Pick who takes it over",
    reassignPickMachine: "Pick a machine to run it on",
    reassignNoMembers: "No member is available to take this over",
    reassignNote: "Handover note (optional)",
    reassignNotePlaceholder: "Anything the new executor should know…",
    reassignConfirm: "Reassign",
    reassignError: "The reassign failed. Please try again.",
    replyHeader: "Ask",
    replyBadge: "Your call",
    replyInChat: "Reply in chat",
    gateMark: "Approval",
    replyAnsweredTag: "Answered",
    expandReply: "Expand reply card",
    collapseReply: "Collapse reply card",
    // Artifact set (T-3dc5): the deliverables (file/image/link) pinned onto a
    // task card. The 「Artifacts N」 count badge sits in the coloured badge row;
    // clicking opens a popover with three gallery-style tabs. 0 ⇒ badge hidden.
    artifacts: {
      badge: "Artifacts",
      open: "View artifacts",
      panelTitle: "Artifacts",
      imageName: "Image",
      empty: "No artifacts yet",
      close: "Close artifacts",
      remove: "Remove artifact",
      removeConfirm: "Remove this artifact from the task card? (The file itself is kept.)",
      downloadHint: "Download",
      openLinkHint: "Open link",
    },
  },
  // ── Awaiting-reply page (M2 reply cards, B2) ──
  replies: {
    waitingTitle: "Ask",
    handledTitle: "Recently handled",
    handledHint:
      "Items you've answered or expired · answers changeable within a day",
    empty: "✓ No pending asks",
    loadError: "Failed to load your asks. Please try again.",
    waited: (t: string) => `Waiting ${t}`,
    // Opened/answered stamps are always absolute with the date (e.g. 7/13
    // 09:05) — no relative time, no "Today" special case.
    openedAt: (time: string) => `Opened ${time}`,
    answeredAt: (time: string) => `Answered ${time}`,
    expiredAt: (time: string) => `Expired ${time}`,
    // Mark expired (owner-only terminal; not an answer; no undo) — the button
    // opens a double-confirm.
    expire: "Mark expired",
    expireConfirm: "Confirm mark expired",
    expireConfirmBody: (summary: string) =>
      `Mark "${summary}" as expired? This cannot be undone and does not count as an answer — the member is notified and will open a fresh card if the question still matters.`,
    expireError: "Marking expired failed. Please try again.",
    expiredTag: "Expired",
    expiredNote:
      "You marked this expired without answering; the member will re-ask if it still matters",
    aiPick: "AI pick",
    yourPick: "Your choice",
    jumpToChat: "View in chat",
    inputPlaceholder: "Type a reply…",
    answerError: "Reply failed. Please try again.",
    answerStale:
      "This card can no longer be answered — its task has closed, or the card was already handled. If it is still listed, close it with “Mark expired” on the card.",
    viewOptions: "View original options",
    collapseOptions: "Collapse options",
    currentTag: "current",
    redecide: "Change my decision",
    redecideHint: "Pick again, or type a new reply",
    redecidePlaceholder: "Or type a new reply…",
    taskBadge: "Task",
    viewTask: "View task details",
  },
  office: {
    membersTitle: "Office members",
    // Top 正職/外包 text tabs (T-66a8): staffTitle doubles as the tab label.
    staffTitle: "Staff",
    // The small count line under each tab: Staff "N people".
    staffSub: (n: number) => `${n} ${n === 1 ? "person" : "people"}`,
    // The recruit button pinned at the sidebar bottom (routes by active tab).
    recruit: "Recruit a member",
    // T-3451: roster row / chat header current-task empty state (no open task).
    noCurrentTask: "No active task",
    role: {
      assistant: "Assistant",
    },
    // Accessible labels for the presence dot — one per lifecycle visual state.
    presence: {
      offline: "Offline",
      waking: "Waking",
      "online-awake": "Online",
      stopping: "Stopping",
      stopped: "Stopped",
    },
    viewProfile: "Member details",
    backToMembers: "Back to members",
    loadError: "Failed to load office members. Please try again.",
    chatUnavailableTitle: "This conversation partner is no longer listed",
    chatUnavailableSub:
      "This member is no longer in the office; the history below is read-only.",
    outsource: {
      title: "Outsource",
      // The tab's count line: Outsource "N people" + a "· cap M" suffix
      // (omitted when settings are not loaded).
      workerSub: (n: number) => `${n} ${n === 1 ? "person" : "people"}`,
      capSuffix: (cap: string) => ` · cap ${cap}`,
      // Single source of the outsource identity label (T-3ed8): chat header /
      // sender label, task-card chips, sidebar 外包 row and monitor session row
      // all render through this so 「Outsource · 代號」never drifts.
      label: (codename: string) => `Outsource · ${codename}`,
      paused: "Assignment paused",
      capTitle: "Outsource cap",
      capHint:
        "Cap how many outsource workers can be hired at once; unlimited removes the cap.",
      capMaxLabel: "Max hires",
      capUnlimited: "Unlimited",
      capDecrease: "Decrease",
      capIncrease: "Increase",
      capSave: "Done",
      capError: "Didn't save. Please try again.",
      loadError: "Failed to load outsource workers. Please try again.",
      viewDetail: "Outsource details",
      openTask: "Open task details",
      releasedChatTitle: "Outsource · released",
      releasedChatSub:
        "This outsource worker was released when its task closed; the history below is read-only.",
    },
  },
  workerDetail: {
    back: "Back",
    codename: "Codename",
    model: "Model",
    effort: "Effort",
    status: "Status",
    statusLabel: (s: string) =>
      (({ assigned: "Assigned", active: "Active", released: "Released" }) as Record<
        string,
        string
      >)[s] ?? s,
    task: "Delegated task",
    delegator: "Delegated by",
    // Shown only when the owner personally created the bound task (a real
    // source, no longer an unconditional placeholder).
    delegatorOwner: "System owner",
    // Honest fallback when creator_id is blank (pre-column / server-scheduled),
    // replacing the former hardcoded "System owner".
    delegatorSystem: "System-scheduled",
    // ── T-f190: fields aligned with the member detail panel ───────────────
    machine: "Machine",
    claudeAccount: "Claude Account",
    runtime: "Runtime",
    context: "context",
    estimatedCost: "est. $",
    notAssigned: "Not yet assigned",
    starting: "Starting",
    offline: "Offline",
    working: "Working",
    // ── T-32e1/T-f190 lifecycle ops (aligned with the member detail panel) ──
    stopped: "Stopped",
    refocus: "Refocus",
    refocusOfflineHint: "Refocus requires the worker online",
    refocusing: "Refocusing…",
    refocusDone: "Sent",
    refocusError: "Refocus failed",
    refocusSubmittedNote: "Refocus sent · worker respawning…",
    refocusSinceLabel: (t: string) => `Last handover ${t}`,
    stop: "Stop",
    stopping: "Stopping…",
    restart: "Restart",
    restarting: "Starting…",
    stopError: "Action failed, please retry",
    modelSave: "Save",
    modelCancel: "Cancel",
    modelError: "Save failed, please retry",
    modelNextSpawnNote:
      "Takes effect now while working; on the next spawn if only assigned",
    relocateTitle: "Choose a machine to move to",
    relocateConfirm: "Move to this machine",
    noOnlineMachine: "No online machine",
    lastOp: "Last operation",
    lastOpStart: "Start",
    lastOpStop: "Stop",
    lastOpOk: "OK",
    lastOpFail: "Failed",
    lastOpLogLabel: "View log",
    terminal: "Terminal · TMUX",
    copyCommand: "Copy command",
    copied: "Copied",
    terminalHint:
      "Paste this in your own terminal to attach to this worker's session.",
    // Initial-prompt preview (boot-context): a worker never stores its verbatim
    // dispatch-time persona, so the server re-assembles it from the CURRENT
    // task/manual — the hint and note both flag that it is today's version.
    initialPromptHint: "current re-assembly",
    initialPromptNote:
      "A preview re-assembled from the CURRENT task and manual — not a verbatim record of the dispatch-time text (edits to the task/manual since then will differ).",
    dash: "—",
  },
  lifecycle: {
    action: {
      // "Spawn" → "Wake" (owner acceptance): the action wakes an existing
      // member, it does not create a new one.
      spawn: "Wake",
      cancel: "Cancel",
      stop: "Stop",
      "force-stop": "Force stop",
    },
    message: {
      windDown: "Winding down…",
      dump: "Compacting context (dump)…",
      resumeReport: "Resume report · what's next and what's in hand",
      degraded: "Degraded · circuit breaker tripped",
    },
  },
  login: {
    title: "Sign in",
    passwordPlaceholder: "Deploy password",
    submit: "Sign in",
    submitting: "Signing in…",
    error: "Incorrect password, try again",
  },
  firstRun: {
    title: "Set the admin password",
    intro: "First time here — pick the password you will sign in with.",
    claimPlaceholder: "Claim code",
    claimHint:
      "The claim code is printed in the server's startup log — only this machine's owner can read it.",
    passwordPlaceholder: "New password (at least 8 characters)",
    confirmPlaceholder: "Repeat the new password",
    submit: "Get started",
    submitting: "Setting up…",
    errorClaim: "That claim code doesn't match — check it again",
    errorTooShort: "The password needs at least 8 characters",
    errorMismatch: "The two passwords don't match",
    errorTaken: "A password is already set — sign in instead",
    gotoLogin: "Go to sign in",
  },
  // T-ba62 first-run automation result banner. Shown ONLY when something did
  // not succeed: on success a live assistant in the cockpit IS the signal, and
  // on failure this is the only place the owner can read WHY.
  onboarding: {
    titleFailed: "Automatic setup did not finish",
    intro:
      "After you set your password the server installs this machine and wakes your assistant automatically. One step did not pass:",
    stepInstallWarden: "Install this machine",
    stepWakeAssistant: "Wake the assistant",
    detailShow: "Show details",
    detailHide: "Hide details",
    dismiss: "Got it",
  },
  // ── Undelivered-dispatch notice (T-7fa1) ─────────────────────────────────
  // 🔴 The copy's scope must equal the BOOL's scope (review r1 BLOCKER-1). The
  // first version named a cause the server never reports; see zh.ts for the full
  // reasoning and the two server probes that disproved it.
  dispatchAlert: {
    wakeTitle: "No wake command went out this time",
    wakeBody:
      "Nothing was dispatched on this attempt, so this click will not wake the member. The intent is saved and the server keeps retrying in the background.",
    wakeStep1:
      "The target machine (or its warden) may not be connected — check whether it is online under Monitor.",
    wakeStep2:
      "Or an earlier command may still be retrying — if this member's Last operation shows a reason, trust that line: it is more precise than this one.",
    relocateTitle: "No move command went out this time",
    relocateBody:
      "The new machine is pinned, but nothing was dispatched on this attempt — the machine that had to take the command is not connected. The server keeps retrying in the background.",
    relocateStep1:
      "Check under Monitor which machines are offline — this command could not go out because the one that had to take it is not connected.",
    relocateStep2:
      "Once that machine connects, the background retry sends this move out — no need to press again, the new machine is already saved.",
  },
  profile: {
    title: "Profile",
    rename: "Rename",
    renamePlaceholder: "Enter name",
    preferences: "Preferences",
    preferencesSub: "Name, appearance, language, password",
    logout: "Log out",
    back: "Preferences",
    theme: "Theme",
    themeManageHint: "Add & edit in Settings › Theme",
    themeOffice: "Office",
    themeXian: "Xian",
    themeImport: "Import",
    themeExport: "Export",
    themeCopy: "Copy",
    themeCopied: "Copied",
    themeEdit: "Edit",
    themeDelete: "Delete",
    themeExampleImport: "Import 修仙 example",
    themeExampleName: "修仙 example",
    themeImportTitle: "Import theme",
    themeImportPlaceholder: "Paste theme JSON here…",
    themeChooseFile: "Choose .json file",
    themeConfirmImport: "Import",
    themeImportDup: "A custom theme with that id already exists",
    themeImportReadFailed: "Could not read that file",
    themeLimitReached: "You've reached the custom-theme limit",
    themeEditTitle: "Edit theme",
    themeNameLabel: "Name",
    language: "Language",
    langZh: "中文",
    langEn: "English",
    changePassword: "Change password",
    changePasswordSub: "The password you sign in to this console with",
    currentPasswordPlaceholder: "Current password",
    newPasswordPlaceholder: "New password (at least 8 characters)",
    confirmPasswordPlaceholder: "Repeat the new password",
    save: "Save",
    saving: "Saving…",
    pwdChanged: "Password updated",
    pwdErrorCurrent: "That current password is wrong",
    pwdErrorTooShort: "The new password needs at least 8 characters",
    pwdErrorMismatch: "The two new passwords don't match",
  },
  chat: {
    offlineTitle: (name: string) => `${name} is offline`,
    offlineHint: "This member is offline. Wake them to start a conversation.",
    // T-94c1: offline/stopped can now be messaged (queues until wake).
    offlineQueueHint: (name: string) =>
      `You can still leave a message — ${name} will read it once back online.`,
    // T-94c1 wake row (offline/stopped composer): queue notice + in-place wake.
    wakeQueueHint: (name: string) =>
      `${name} is offline — your message will queue, or wake them now`,
    wakeButton: "Wake",
    wakePending: "Waking…",
    emptyRange: "No messages in this range yet",
    inputPlaceholder: (name: string) => `Reply to ${name}…`,
    // M2-4 composer lock: shown IN PLACE OF the reply input while the member
    // is not online (offline / stopped / waking / stopping).
    composerOffline: (name: string) => `${name} is currently offline`,
    composerOfflineWake: (name: string) =>
      `${name} is currently offline — open the member panel to wake them`,
    me: "Me",
    systemSender: "System",
    send: "Send",
    imageTooLarge: "Image is too large (20 MB max)",
    pastedImageAlt: "Pasted screenshot",
    imageAlt: "Chat image",
    viewImageLabel: "View full size",
    closeImageLabel: "Close image",
    attachLabel: "Attach a file",
    attachTooLarge: (maxMb: number) => `File is too large (${maxMb} MB max)`,
    attachTooMany: (max: number) => `At most ${max} attachments per message`,
    removeAttachmentLabel: "Remove attachment",
    downloadAttachment: "Download",
    read: "Read",
    // M2 batch 19 unread jump: the floating chip shown when a new message lands
    // while scrolled up; the thin divider above the first unread message on entry.
    newMessages: "New messages",
    unreadBelow: "Unread messages below",
    // T-bf82 scrollback: the top-of-thread marker once the history is
    // exhausted (hasMore=false).
    historyStart: "Beginning of conversation",
    // LINE-style day dividers in the message stream (centered pill at each day
    // crossing; sticky at the top while scrolling). weekday 0=Sun … 6=Sat; the
    // year only appears when it isn't the current year (LINE convention).
    dateToday: "Today",
    dateYesterday: "Yesterday",
    dateOn: (month: number, day: number, weekday: number) =>
      `${WEEKDAYS_EN[weekday]}, ${MONTHS_EN[month - 1]} ${day}`,
    dateOnYear: (year: number, month: number, day: number, weekday: number) =>
      `${WEEKDAYS_EN[weekday]}, ${MONTHS_EN[month - 1]} ${day}, ${year}`,
    interAgentExpand: (count: number) =>
      `${count} message${count === 1 ? "" : "s"} between agents · expand`,
    interAgentCollapse: "Collapse agent-to-agent messages",
    // M2-3 conversation file/image gallery (header icon → panel).
    tasksLink: "See this member's unfinished tasks",
    roleSettingsLink: "Open this role's definition settings",
    galleryLabel: "Files & images",
    galleryTabImages: "Images",
    galleryTabFiles: "Files",
    galleryEmptyImages: "No images yet",
    galleryEmptyFiles: "No files yet",
    // M2 batch 18: uploader filter chips (options derived from the actual
    // attachment senders, stacking with the Images/Files tabs).
    gallerySenderFilterLabel: "Filter by uploader",
    gallerySenderAll: "All",
    galleryClose: "Close gallery",
    galleryPreviewHint: "Preview in a new tab",
    galleryDownloadHint: "Download",
    // Permanent single-file share link (?sig= HMAC) — copied to the clipboard.
    copyShareLink: "Copy share link",
    shareLinkCopied: "Link copied",
    // In-cockpit preview of a .md attachment (T-a1c4): a separate action from
    // download; the overlay renders via Markdown.tsx (not the raw-source new tab).
    // T-7bc2: the chip itself is the trigger now — no separate "action" label.
    mdPreview: {
      download: "Download",
      close: "Close preview",
      loading: "Loading preview…",
      error: "Could not load the preview",
    },
  },
  mp: {
    back: "Back",
    rename: "Rename",
    renamePlaceholder: "Enter name",
    wake: "Wake",
    wakeManual: "Wake manually",
    // Instant feedback after clicking Wake, before server presence catches up.
    wakePendingNote: "Waking…",
    forceStopConfirmTitle: "Force stop?",
    forceStopConfirmBody: (name: string) =>
      `Force-stop ${name} immediately — kill the session now, skipping the graceful shutdown. Any unsaved work in progress is lost.`,
    forceStopConfirmAction: "Force stop",
    forceStopBusy: "Stopping…",
    model: "Model",
    effort: "EFFORT · Thinking",
    effortLevel: (e: Effort) =>
      ({ low: "Low", medium: "Medium", high: "High" })[e],
    modelEffortSave: "Save",
    modelEffortCancel: "Cancel",
    modelPlaceholder: "Custom model string (blank = default)",
    claudeAccount: "Claude Account",
    modelEffortNextWakeNote: "Changes take effect on the next wake / handover",
    modelEffortError: "Save failed. Please try again.",
    runtime: "Runtime",
    machine: "Machine",
    standby: "On standby",
    context: "context",
    refocus: "Refocus",
    refocusOfflineHint: "Refocus is available only when online",
    refocusing: "Refocusing…",
    refocusDone: "Sent",
    refocusError: "Refocus failed",
    refocusSubmittedNote: "Refocus sent · agent compacting context…",
    refocusSinceLabel: (t: string) => `Last refocus ${t}`,
    // fleet remote-ops stage 1 — last warden op receipt
    lastOp: "Last operation",
    lastOpStart: "Start",
    lastOpStop: "Stop",
    lastOpOk: "succeeded",
    lastOpFail: "failed",
    lastOpLogLabel: "View log",
    estimatedCost: "est. $",
    terminal: "Terminal · TMUX",
    copyCommand: "Copy command",
    copied: "Copied",
    terminalHint:
      "Paste this in your own terminal to attach to this member's session.",
    initialPrompt: "Initial prompt",
    promptLoading: "Loading…",
    promptError: "Failed to load initial prompt",
    lessons: "Past lessons",
    expandableHint: "applies on next wake / refocus",
    lessonsLoading: "Loading…",
    lessonsError: "Failed to load lessons",
    lessonsEmpty: "No lessons yet.",
    lessonsShared: "This role's learnings (shared by every agent of this role).",
    lessonsSaveError: "Failed to save lessons",
    // ── Webhook endpoints (M4) ──
    webhook: {
      title: "WEBHOOK ENDPOINTS",
      enabled: "Enabled",
      disabled: "Disabled",
      add: "Add webhook",
      endpointIdLabel: "Endpoint ID",
      endpointIdPlaceholder: "e.g. pr-events, immutable once created",
      purposeLabel: "Purpose",
      purposePlaceholder: "What this endpoint is for (optional)",
      create: "Create",
      cancel: "Cancel",
      copy: "Copy",
      copied: "Copied",
      deleteLabel: "Delete",
      deleteConfirm:
        "Delete this webhook endpoint? Its token is revoked permanently and cannot be restored.",
      createError:
        "Failed to create webhook (endpoint ID must be alphanumeric / _ / - and unique)",
      loadError: "Failed to load webhook endpoints",
      empty: "No webhook endpoints yet",
      // ── platform / signing-secret (M4 §2) ──
      platformLabel: "Platform type",
      platformGeneric: "Generic (URL token only)",
      platformSlack: "Slack",
      platformGithub: "GitHub",
      signingSecretLabel: "Signing Secret",
      signingSecretPlaceholder: "Shared secret for HMAC verification",
      signingSecretRequired: "A signing secret is required for Slack / GitHub",
      helperSlack:
        "Slack: use the Signing Secret from your app's Basic Information page.",
      helperGithub:
        "GitHub: use the secret you set when creating the webhook.",
      rotateSecret: "Rotate secret",
      rotateSecretSave: "Save secret",
      // ── observability counters (per-row "Event stats" entry → window) ──
      statsTitle: "Event stats",
      statsClose: "Close",
      statsNever: "No calls received yet",
      statsNeverHint:
        "This endpoint hasn't received any calls. Send a test event from the external service and it will show up here.",
      statsLastReceivedLabel: "Last received",
      statsDroppedLabel: "Dropped",
      statsAgo: (ago: string) => `${ago} ago`,
      dropReasonSigFailed: "signature failed",
      dropReasonDisabled: "hit while disabled",
      dropReasonMemberGone: "member gone",
      requestsTitle: "Recent requests",
      requestsLoading: "Loading…",
      requestsError: "Failed to load recent requests",
      requestsEmpty: "No requests recorded yet",
      outcomeDelivered: "Delivered",
      outcomeDropped: "Dropped",
      outcomeChallenge: "Challenge",
      outcomePing: "PING",
      requestHeaders: "HEADERS",
      requestBody: "BODY",
      requestBodyEmpty: "(empty)",
      requestTruncated: "truncated",
    },
    dash: "—",
  },
  machine: {
    noOnlineMachine: "No online machine",
    picker: {
      label: "Choose a machine",
      offlineOption: (name: string) => `${name} (offline)`,
      spawnTitle: "Choose a machine to run on",
      spawnConfirm: "Wake on this machine",
      relocateTitle: "Choose a machine to move to",
      relocateConfirm: "Move to this machine",
    },
  },
  monitor: {
    dash: "—",
    accountsTitle: "Accounts",
    machinesTitle: "Machines",
    sessionsTitle: "AI Sessions",
    renameMachine: "Rename machine",
    renameAccount: "Rename account",
    renamePlaceholder: "Enter display name",
    renameError: "Rename failed",
    accountsEmpty: "No account usage data yet",
    estimate: "est.",
    fiveHour: "5-hour window",
    sevenDay: "7-day window",
    usage: "usage",
    time: "time",
    overheated: "overheated",
    detail: {
      open: "Account details",
      title: "Account details",
      close: "Close",
      accountKey: "Account key",
      userId: "User ID (hash)",
      orgUuid: "Org UUID",
      email: "Email",
      org: "Organization",
      labelRaw: "Reported label",
      machines: "Machines",
      estCost: "Est. cost",
    },
    machineCol: {
      machine: "Machine",
      status: "Status",
      claude: "Claude",
      account: "Account",
      cpu: "CPU",
      ram: "RAM",
      battery: "Battery",
      power: "Power",
    },
    sessionCol: {
      member: "Member",
      machine: "Machine",
      account: "Account",
      model: "Model",
      context: "context",
      estCost: "est. $",
    },
    machine: {
      actionsCol: "Actions",
      copy: "Copy",
      copied: "Copied",
      close: "Close",
      machinesEmpty: "No machines yet — add a machine / onboard first",
      online: "Online",
      offline: "Offline",
      // onboard — the dashed button grows an inline row: type the machine
      // name, Enter/confirm creates, Esc/cancel collapses
      onboardEntry: "Add machine / onboard",
      onboardNamePlaceholder: "Machine name",
      onboardConfirm: "Create",
      onboardBusy: "Adding…",
      onboardError: "Failed to add machine",
      // ── three verbs: install / uninstall / delete ──
      install: "Install",
      uninstall: "Uninstall",
      deleteMachine: "Delete",
      // offline machine has no warden to uninstall (disabled-button tooltip)
      uninstallOfflineHint: "Machine is offline — no warden to uninstall",
      // uninstall intent armed, warden not yet disconnected — the same
      // in-progress treatment as "Installing…"
      uninstallInProgress: "Uninstalling…",
      // install dialog (non-server machines): a single screen — copy & run on it
      installTitle: "Install machine",
      installRemoteHint:
        "Copy the command below and run it on that machine to install the warden. The command re-mints a fresh token.",
      // copy the install command (GET /boot-command; re-mints a token)
      copyBootCmd: "Copy install command",
      copyBootCmdError: "Failed to fetch command",
      // install-on-server result (POST /bootstrap-here): failure-only (success
      // shows nothing — the row flips online)
      bootstrapBusy: "Installing…",
      bootstrapError: "Install request failed",
      // shown when the server returned an error detail (e.g. the 503
      // missing-ocwarden reason)
      bootstrapErrorDetail: (detail: string) => `Install request failed: ${detail}`,
      bootstrapFailed: (exitCode: number) =>
        `Install failed (exit code ${exitCode}). Reason:`,
      // T-ba62: the log is kept on SUCCESS too. The success branch used to
      // throw it away, so "installed" and "installed with warnings inside"
      // looked identical.
      bootstrapSucceeded: "Install finished. Log:",
      // uninstall (POST /uninstall): drive the uninstall RPC to the warden
      // (online-only)
      uninstallConfirmTitle: "Confirm uninstall",
      uninstallConfirmBody: (name: string) =>
        `Uninstall “${name}”? This asks the warden on that machine to run ocwarden uninstall; on success the machine goes offline, but the record is KEPT (re-installable).`,
      uninstallConfirm: "Confirm uninstall",
      uninstallBusy: "Working…",
      uninstallError: "Uninstall failed",
      uninstallResultTitle: "Uninstall result",
      uninstallDispatched:
        "Uninstall command sent — the machine will go offline once the warden reports back. The record is kept (re-installable).",
      uninstallAlreadyOffline:
        "The machine is already offline and treated as already uninstalled — nothing was dispatched. The record is kept (re-installable).",
      // uninstall guard: warn first when members are still ACTUALLY ONLINE on
      // this machine (offline members merely bound here never count — same
      // criterion as the server's 409 gate)
      uninstallWarnTitle: "Members still on this machine",
      uninstallWarnBody: (name: string, count: number) =>
        `“${name}” still has ${count} member(s) online on it. Uninstalling now tears the warden off the machine while they are still on it — take the related members offline first. Proceed anyway?`,
      uninstallWarnProceed: "Proceed anyway",
      // delete (DELETE /machines/{id}): a pure record delete, no warden command
      deleteConfirmTitle: "Confirm delete machine",
      deleteConfirmBody: (name: string) =>
        `Delete “${name}”? This only removes the machine's record from the list — it does NOT tear the warden off the machine (that is “Uninstall”).`,
      deleteConfirm: "Confirm delete",
      deleteBusy: "Deleting…",
      deleteError: "Delete failed",
      // ── one-click upgrade (T-5f01 rework: lives in the action group) ──
      upgrade: "Upgrade",
      upgrading: "Upgrading…",
      upgradeCurrentHint: "Already up to date",
      upgradeUnknownHint:
        "No version fingerprints reported yet — cannot tell whether an update is available",
      upgradeOfflineHint:
        "Machine offline — cannot dispatch an upgrade (it self-updates when it reconnects)",
      upgradeError: "Failed to dispatch the upgrade command — try again",
    },
  },
  settings: {
    title: "Settings",
    software: "Software update",
    roles: "Role journal",
    params: "Parameters",
    // ── theme management (T-16a1 P3b): moved here from the profile dropdown ──
    themeManage: "Theme",
    themeColorsSection: "Colours",
    themeColorPicker: "colour picker",
    themeWordingSection: "Wording",
    themeWordingHint:
      "Fill in a replacement to override interface wording; leave blank to keep the original.",
    themeWordingSearch: "Search wording…",
    themeWordingOverride: "replacement",
    themeBuiltinTag: "Built-in",
    themeWordingTag: "Wording",
    themeDeleteConfirm: (name: string) =>
      `Delete theme "${name}"? This cannot be undone.`,
    currentVersion: "Current version",
    upToDate: "Up to date",
    // Explicit check against GitHub Releases (GET /api/release/check)
    checkUpdate: "Check for updates",
    checkingUpdate: "Checking…",
    checkUnknown:
      "Could not reach GitHub to check for updates — try again later",
    checkFailed: "The update check failed — try again",
    viewRelease: "View release",
    updateSettings: "Update settings",
    // ── software-update toggles (receive_beta / auto_update, both default OFF) ──
    receiveBeta: "Receive beta versions",
    receiveBetaSub: "Update checks also follow GitHub prereleases · off = official releases only",
    autoUpdate: "Automatic updates",
    autoUpdateSub: "Upgrade and restart in the background when a newer version appears · off by default",
    upgradeFailed: "Upgrade failed",
    upgradeRestarting:
      "Upgrading — the new version is installed and the server is restarting; this page will reload by itself.",
    upgradeTimeout:
      "The server did not come back with the new version — check the server log; the previous binary is kept as ocserverd.bak.",
    updateAvailable: "A newer version is available",
    upgrade: "Update to latest",
    catalogHash: "MCP catalog hash",
    globalSection: "GLOBAL CONTEXT",
    systemName: "System interaction",
    systemSub: "How the system works, injected into every agent · read-only",
    readOnlyBadge: "System · read-only",
    customName: "User additions",
    customSub: "Custom content appended to every agent's boot context · editable",
    roleDefsSection: "Role definitions",
    bootName: "Boot sequence",
    bootSub: "Fixed studio SOP · read-only",
    bootBadge: "Studio SOP",
    defaultBadge: "Default",
    edit: "Edit",
    doneEdit: "Done",
    cancel: "Cancel",
    reset: "Reset",
    editorPlaceholder: "Write in Markdown…",
    loadError: "Failed to load role definitions. Please try again.",
    addRole: "Add role definition",
    addRoleName: "Role name",
    renameRole: "Rename role",
    addRoleSubmit: "Create",
    addRoleCancel: "Cancel",
    addRoleError: "Create failed. Check the role name.",
    customBadge: "Custom",
    deleteRole: "Delete",
    deleteRoleConfirm: (name: string) =>
      `Delete role "${name}"? Its members and their conversations and lessons will be removed permanently.`,
    deleteRoleConfirmAction: "Delete role",
    deleteRoleOnline: "A member is online — cannot delete",
    deleteRoleError: "Delete failed. Please try again.",
    paramsLoadError: "Failed to load parameters. Please try again.",
    paramsSaveError: "Didn't save — try again",
    sessionTtl: "Session length",
    sessionTtlSub: "How long before you have to sign in again",
    ttl12h: "12 hours",
    ttl24h: "24 hours",
    ttl7d: "7 days",
    ttl30d: "30 days",
    handover: "Auto-handover threshold",
    handoverSub:
      "When a teammate's memory fills to this level, it hands over to a fresh one (40–90%)",
    // ── Verified-save read-back (T-1c2e; lives in the software-update view
    // after the rework: secrets show only set/unset, never the plaintext, and
    // the auto-update switch verifies a save by reading the value back) ──
    configSecretSet: "Set",
    configValueUnset: "Not set",
    configSaving: "Saving…",
    configSaved: "Saved — read-back matches",
    // Covers both failure shapes (rejected write / verify read-back failed) —
    // never asserts what the server stored, only the UI's honest facts.
    configSaveFailed:
      "Couldn't confirm the save — showing the server's last confirmed value; try again",
    manuals: "Task manuals",
    manualsLoadError: "Failed to load task manuals. Please try again.",
    manualsEmpty: "No task types yet — add the first one below",
    addManual: "Add type",
    addManualName: "Display name (e.g. Review PR)",
    addManualSubmit: "Create",
    addManualCancel: "Cancel",
    addManualError: "Creation failed. Check the display name and try again.",
    deleteManual: "Delete",
    deleteManualConfirm: (key: string) =>
      `Delete the task type “${key}”? Its manual (definition, SOP, learnings) is removed with it and cannot be restored.`,
    deleteManualConfirmAction: "Delete",
    deleteManualOpenTasks:
      "This type still has open tasks — let them finish before deleting",
    deleteManualError: "Delete failed. Please try again.",
    manualTabDefinition: "Task definition",
    manualTabLearnings: "Learnings",
    manualDisplayName: "Display name",
    manualDisplayNamePlaceholder: "A readable name (blank shows the internal ID)…",
    manualQ1: "What is this task?",
    manualQ1Hint:
      "The intake window reads this to judge whether an incoming trigger becomes a task of this type.",
    manualQ1Placeholder: "Describe what this task type is for…",
    manualQ2: "What information is needed?",
    manualQ2Hint:
      "Fields required before execution. Mark one as the 🔑 identity key and the intake window uses it to tell whether it's the same task (e.g. the same PR link = the same task; later messages merge in instead of opening a new one).",
    manualQ3: "How is it done?",
    manualQ3Hint: "Playbook · the AI plans the workflow from it",
    manualEmptyHint: "Not filled in yet",
    manualFieldNamePlaceholder: "Field name",
    manualFieldRequired: "Required",
    manualFieldOptional: "Optional",
    manualFieldKey: "🔑 Identity key",
    manualAddField: "Add field",
    manualRemoveField: "Remove field",
    manualNoFields: "No fields defined yet",
    manualLearningsHint:
      "Feedback and corrections accumulated for this type, reused across tasks; agents write back on task close, and you can edit by hand.",
    manualSaveError: "Save failed. Please try again.",
    assigneeTitle: "Assigned executor",
    assigneeSummarySub: "Assigned executor · handles every task of this type",
    assigneeHint:
      "Who executes tasks of this type — a member, or outsource (model, effort and copies are set here; the server does the assigning).",
    assigneeUnset: "Not set",
    assigneeKindMember: "Member",
    assigneeKindOutsource: "Outsource",
    assigneeToggleMember: "Pick a member",
    assigneeToggleOutsource: "Outsource",
    assigneeModelLabel: "Model",
    assigneeModelPlaceholder: "Model (blank = default)",
    assigneeEffort: "Effort",
    assigneeMachineLabel: "Machine",
    assigneeMachineAuto: "Auto-assign",
    assigneeMachineAutoHint: "Picks the idlest machine",
    assigneeMachineIdle: "Idle",
    assigneeMachineBusy: "Busy",
    assigneeMachineOffline: "Offline",
    assigneeMachineNote:
      "If the chosen machine is offline at the time, auto-assign is used instead.",
    assigneeCopies: "Hire count",
    assigneeCopiesDecrease: "Decrease",
    assigneeCopiesIncrease: "Increase",
    assigneeUnlimited: "Unlimited",
    assigneeClear: "Clear setting",
    assigneeNoMembers: "No members available",
    manualPlanningSection: "Task planning",
    manualDefEntrySub: "What it is, what info it needs, how to do it",
    manualLearnEntrySub: "Feedback and corrections from past tasks",
  },
};
