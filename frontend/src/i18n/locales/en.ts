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
  notifications: {
    dismiss: "Dismiss notification",
    title: "Turn on notifications",
    description: "This device will notify you about new messages and asks that need your decision.",
    enable: "Turn on notifications",
    enabled: "Notifications are on for this device",
    disable: "Turn off notifications",
    unsupported: "This browser does not support push notifications.",
    denied: "Notifications are blocked. Allow OffiCraft notifications in your browser settings.",
    failed: "Could not set up notifications. Please try again.",
    contactRequired: "Add a notification email from the Profile menu first.",
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
    progressLabel: "Step",
    elapsedLabel: "Elapsed",
    expandCard: "Expand workflow",
    collapseCard: "Collapse workflow",
    workflow: "Workflow",
    dod: "DoD",
    parallel: (n: number) => `In parallel · ${n} items`,
    waitingAssign: "Awaiting assignment",
    planningByLead: "Waiting for",
    planningByTail: "to create steps",
    stepsLoading: "Loading…",
    stepsLoadError: "Couldn't load the workflow.",
    stepsRetry: "Retry",
    waitingLabel: "Waiting",
    blockedByLabel: "Waiting on",
    // T-1d82: a dep row whose task cannot be resolved (deleted / bad id). Keeps
    // the raw id — it is the only handle left — but says plainly that there is
    // nothing to open, so the row is not mistaken for a broken link.
    blockedByMissingSuffix: "(task not found)",
    depJump: (taskNo: string) => `Open ${taskNo}`,
    openKeyLink: "Open link",
    messagePlaceholder: (name: string) => `Message ${name}…`,
    send: "Send",
    messageError: "Failed to send the message. Please try again.",
    statusMenuLabel: "Status actions",
    priorityLabel: "Priority",
    // Click-to-copy task-no chip (owner 2026-07-19).
    copyTaskNoLabel: "Copy task number",
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
    terminateConfirmBodyLead: "Terminate “",
    terminateConfirmBodyTail:
      "”? The task moves to Closed and cannot be resumed; the backend will notify the executor to wind it down.",
    terminateConfirm: "Terminate",
    // Mark duplicate (T-02c9): the executor points at the original and closes it
    markDuplicate: "Mark duplicate",
    markDuplicateBodyLead: "Mark “",
    markDuplicateBodyTail:
      "” a duplicate of another task? It moves to Closed and cannot be resumed. Pick the original:",
    markDuplicatePick: "Select the original task",
    markDuplicateConfirm: "Mark duplicate",
    duplicateOfLabel: "Duplicate of",
    duplicateJump: "Jump to the original",
    actionError: "The action failed. Please try again.",
    // Reassign (T-160e, owner + assistant only): hand the task to another staff
    // member, or mint a fresh outsource worker on the spot (the same model /
    // effort / machine knobs the task type's assignee carries). The task enters
    // Reassigning and BOTH sides are notified; the new executor reports it back
    // to in-progress themselves — the FE never flips it.
    reassign: "Reassign…",
    reassignTitleLabel: "Reassign",
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
    waitedLabel: "Waiting",
    // Opened/answered stamps are always absolute with the date (e.g. 7/13
    // 09:05) — no relative time, no "Today" special case.
    openedAtLabel: "Opened",
    answeredAtLabel: "Answered",
    expiredAtLabel: "Expired",
    // Mark expired (owner/admin-agent terminal since T-6020; not an answer; no
    // undo) — the button opens a double-confirm.
    expire: "Mark expired",
    expireConfirm: "Confirm mark expired",
    expireConfirmBodyLead: 'Mark "',
    expireConfirmBodyTail:
      '" as expired? This cannot be undone and does not count as an answer — the member is notified and will open a fresh card if the question still matters.',
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
      // all render through compose.ts's outsourceLabel, which composes THIS
      // title so 「Outsource · 代號」never drifts (T-081b removed the second,
      // identically-worded template that used to live here).
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
    statusOf: {
      assigned: "Assigned",
      active: "Active",
      released: "Released",
    } as Record<string, string>,
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
    refocusSinceLabel: "Last handover",
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
  // ── Theme IDENTITY names (T-081b §6) ─────────────────────────────────────
  // Every leaf in this subtree is some theme's own `name`: the row in the theme
  // picker, the `name` written into the file when a theme is exported, the
  // default name a newly created theme gets. A theme bundle's wording overlay
  // must NOT be able to touch them — while it could, importing a 「精靈村」 pack
  // renamed the BUILT-IN theme to 「精靈村」 too and the owner lost the way back
  // (owner report 2026-07-27).
  //
  // So the whitelist generator (scripts/gen-message-keys.mjs) skips this whole
  // subtree — a STRUCTURAL rule, not a second hand-kept key list: add another
  // built-in theme later, put its name here, and it is non-overridable for free.
  //
  // ⚠️ The place name is NOT here: the nav tab's 「辦公室」 is nav.office and
  // stays overridable — a theme pack may rename the PLACE, never a THEME.
  themeIdentity: {
    office: "Office",
    newTheme: "New theme",
  },
  // ── The 內建 / 自訂 labels ─────────────────────────────────────────────────
  // Not any theme's name: the labels the Settings › Theme list groups rows
  // under. ONE semantic source, shared by every surface that says 內建 / 自訂.
  //
  // Rounds 3–4 held them non-overridable so a pack could not swap 內建 and 自訂.
  // Round 8 gave them back — owner: 「這是大家自己用的,自己要怎麼搞我們不用特別管,
  // 我們只要確定主題名稱不會隨著主題改變就好」. They are ordinary overridable wording;
  // themeIdentity above is the only subtree a pack cannot reach.
  themeMarkers: {
    builtinGroup: "Built-in",
    customGroup: "Custom",
  },
  profile: {
    title: "Profile",
    rename: "Rename",
    renamePlaceholder: "Enter name",
    preferences: "Preferences",
    preferencesSub: "Name, appearance, language, layout, notifications, password",
    logout: "Log out",
    back: "Preferences",
    theme: "Theme",
    themeManageHint: "Add & edit in Settings › Theme",
    themeAdd: "New",
    themeImport: "Import",
    themeExport: "Export",
    themeEdit: "Edit",
    themeDelete: "Delete",
    themeImportTitle: "Import theme",
    themeImportPlaceholder: "Paste theme JSON here…",
    themeChooseFile: "Choose .json file",
    themeConfirmImport: "Import",
    themeImportDup: "A custom theme with that id already exists",
    themeImportReadFailed: "Could not read that file",
    themeLimitReached: "You've reached the custom-theme limit",
    themeImportSkippedLead: "Imported, but",
    themeImportSkippedMid: "wording code(s) were not recognised and were skipped:",
    themeImportSkippedMore: "…",
    themeEditTitle: "Edit theme",
    themeNameLabel: "Name",
    language: "Language",
    langZh: "中文",
    langEn: "English",
    pushContactEmail: "Notification email",
    pushContactEmailSub: "A public contact address used to identify this cockpit to push services. Notifications are not sent until it is set.",
    pushContactEmailPlaceholder: "name@company.com",
    pushContactEmailError: "Enter a public email address.",
    layout: "Layout",
    layoutNarrow: "Narrow",
    layoutWide: "Wide",
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
    offlineTitleSuffix: "is offline",
    offlineHint: "This member is offline. Wake them to start a conversation.",
    // T-94c1: offline/stopped can now be messaged (queues until wake).
    offlineQueueHintLead: "You can still leave a message —",
    offlineQueueHintTail: "will read it once back online.",
    // T-94c1 wake row (offline/stopped composer): queue notice + in-place wake.
    wakeQueueHintSuffix: "is offline — your message will queue, or wake them now",
    wakeButton: "Wake",
    wakePending: "Waking…",
    emptyRange: "No messages in this range yet",
    inputPlaceholder: (name: string) => `Reply to ${name}…`,
    // M2-4 composer lock: shown IN PLACE OF the reply input while the member
    // is not online (offline / stopped / waking / stopping).
    composerOfflineSuffix: "is currently offline",
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
    // English carries an inline plural rule (message / messages) that a static
    // fragment cannot express, so the singular and the plural are two separate
    // overridable strings and only the BRANCH stays in code (compose.ts).
    interAgentExpandOne: "message between agents · expand",
    interAgentExpandMany: "messages between agents · expand",
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
    avatarUpload: "Change avatar",
    avatarRemove: "Remove avatar",
    avatarBusy: "Working…",
    avatarTypeError: "Use a PNG, JPEG, or WEBP image",
    avatarTooLarge: "Image must be 64 KiB or smaller",
    avatarSaveError: "Could not save the avatar. Please try again.",
    rename: "Rename",
    renamePlaceholder: "Enter name",
    wake: "Wake",
    wakeManual: "Wake manually",
    // Instant feedback after clicking Wake, before server presence catches up.
    wakePendingNote: "Waking…",
    forceStopConfirmTitle: "Force stop?",
    forceStopConfirmBodyLead: "Force-stop",
    forceStopConfirmBodyTail:
      "immediately — kill the session now, skipping the graceful shutdown. Any unsaved work in progress is lost.",
    forceStopConfirmAction: "Force stop",
    forceStopBusy: "Stopping…",
    model: "Model",
    agentRuntime: "AI runtime",
    effort: "EFFORT · Thinking",
    effortOf: { low: "Low", medium: "Medium", high: "High" } as Record<
      Effort,
      string
    >,
    modelEffortSave: "Save",
    modelEffortCancel: "Cancel",
    modelPlaceholder: "Custom model string (blank = default)",
    modelMachineDefault: "Use this machine's Codex default model",
    claudeAccount: "Claude Account",
    codexAccount: "Codex Account",
    modelEffortNextWakeNote: "Changes take effect on the next wake / handover",
    modelEffortError: "Save failed. Please try again.",
    runtime: "Runtime",
    machine: "Machine",
    standby: "On standby",
    context: "context",
    compactionCount: (n: number) => `compact: ${n}`,
    refocus: "Refocus",
    refocusOfflineHint: "Refocus is available only when online",
    refocusing: "Refocusing…",
    refocusDone: "Sent",
    refocusError: "Refocus failed",
    refocusSubmittedNote: "Refocus sent · agent compacting context…",
    refocusSinceLabel: "Last refocus",
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
    relocating: "Moving…",
    relocateTimeout: "No completion report yet — press again to retry.",
    relocateFailed: "The move could not be sent — press again to retry.",
    relocateSent: "Move request sent",
    picker: {
      label: "Choose a machine",
      offlineOptionSuffix: "(offline)",
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
      codex: "Codex",
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
      // When the server returned an error detail (e.g. the 503 missing-ocwarden
      // reason), compose.ts appends it to bootstrapError above — the same
      // sentence no longer lives under a second key.
      bootstrapFailedLead: "Install failed (exit code ",
      bootstrapFailedTail: "). Reason:",
      // T-ba62: the log is kept on SUCCESS too. The success branch used to
      // throw it away, so "installed" and "installed with warnings inside"
      // looked identical.
      bootstrapSucceeded: "Install finished. Log:",
      // uninstall (POST /uninstall): drive the uninstall RPC to the warden
      // (online-only)
      uninstallConfirmTitle: "Confirm uninstall",
      uninstallConfirmBodyLead: "Uninstall “",
      uninstallConfirmBodyTail:
        "”? This asks the warden on that machine to run ocwarden uninstall; on success the machine goes offline, but the record is KEPT (re-installable).",
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
      // Two parameters with the number wedged mid-sentence (English pseudo-plural
      // "member(s)", Chinese measure word 「位」), so each language gets THREE
      // fragments and the key names carry the join order: 1 + name + 2 + count + 3.
      uninstallWarnBody1: "“",
      uninstallWarnBody2: "” still has ",
      uninstallWarnBody3:
        " member(s) online on it. Uninstalling now tears the warden off the machine while they are still on it — take the related members offline first. Proceed anyway?",
      uninstallWarnProceed: "Proceed anyway",
      // delete (DELETE /machines/{id}): no warden command is sent, but this is
      // NOT the cheap bookkeeping edit the old copy described. T-9cf8 made the
      // roster the authority over credentials: taking the machine off the
      // roster revokes its token on the very next request, and every agent
      // still pinned to it goes with it. The old copy ("this only removes the
      // machine's record from the list") would now be buying consent with an
      // inaccurate description of the consequence — the same defect this repo
      // already booked once, when an install gate promised it would not
      // interrupt service and then did. Consent obtained from a wrong
      // description is not consent, so the copy states the real cost.
      deleteConfirmTitle: "Confirm delete machine",
      deleteConfirmBodyLead: "Delete “",
      deleteConfirmBodyTail:
        "”? Its credentials stop working immediately: the machine can no longer report in, and any agent still assigned to it loses access too. Nothing is torn down on the machine itself (that is “Uninstall”), and this cannot be undone — bringing it back means installing it again.",
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
      // ── runtime readiness (T-90be ⑤ + T-b36a): rendered WITH its age. The
      // server keeps stale capability values on the wire on purpose (they are
      // the only explanation for a worker parked on machine_unavailable), so
      // the UI's job is to show them without claiming they are current.
      runtimeStale: "stale",
      runtimeStaleHint:
        "Last probed a while ago — this machine has not reported since, so this readiness may no longer be true",
      runtimeUnknown: "Never probed — an older warden, or no heartbeat yet",
      // ── per-runtime version columns (T-674d). The Runtimes column's ✓/✗
      // digest is gone; Claude and Codex each print their probed version. The
      // ✗ states it used to carry still have to be sayable, because they are
      // the reason placement refuses the machine — so "not installed" and "not
      // signed in" are WORDS in the cell, never a silently missing version.
      runtimeNotInstalled: "not installed",
      runtimeNotInstalledHint:
        "The warden could not resolve this runtime's binary on the machine — it cannot launch here",
      runtimeNoVersion: "installed",
      runtimeNoVersionHint:
        "The binary resolved but its version probe returned nothing",
      runtimeLoggedOut: "signed out",
      runtimeLoggedOutHint:
        "Installed, but the provider login probe says not signed in — placement refuses this runtime on this machine",
      // ── hardware sample age (T-b36a). The server WITHHOLDS the numbers of an
      // expired sample, so cpu/ram/power fall back to a dash — the same dash a
      // machine that has never reported hardware shows. These two labels are
      // what keep those worlds apart on screen; only the second one is
      // actionable ("this box went dark", not "this box never spoke").
      hardwareStale: "stale",
      hardwareStaleHint:
        "Measured a while ago and not since — the numbers are withheld rather than shown as current",
      // ── wrongly-typed hardware value (T-aad2). A THIRD reason this cell
      // is blank, and the one that used to be indistinguishable from "never
      // measured": the probe DID report, with a value the server cannot read
      // (a string where a number belongs). Kept separate from the stale mark
      // on purpose — stale means nobody has looked lately, this means the
      // reporter itself is broken, and they send you to different places.
      hardwareBad: "bad value",
      hardwareBadHint:
        "This machine reported a value of the wrong type, so it cannot be shown — the probe ran, its reading is unusable. Check that machine's warden version.",
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
    themeColorOpacity: "Opacity",
    themeColorFollows: "follows",
    themeColorPicker: "colour picker",
    themeWordingSection: "Wording",
    themeWordingHint:
      "Fill in a replacement to override interface wording; leave blank to keep the original.",
    themeWordingSearch: "Search wording…",
    themeWordingOverride: "replacement",
    themeWordingTag: "Wording",
    // ── fonts (T-16a1 P4): pick body / title font from a safe allowlist ──
    themeFontsSection: "Fonts",
    themeFontsHint:
      "Pick a built-in, safe font family; leave on default to keep the theme's original font.",
    themeFontBody: "Body font",
    themeFontTitle: "Title font",
    themeFontDefault: "Default (theme font)",
    // ── avatars (T-16a1 P5): per-member-type avatar image upload ──
    themeAvatarsSection: "Avatars",
    themeAvatarsHint:
      "Upload an avatar per member type (PNG / JPEG / WEBP, max 64 KB). Leave empty to keep the built-in avatar.",
    themeAvatarMember: "Staff avatar",
    themeAvatarOutsource: "Outsource avatar",
    themeAvatarOwner: "CEO avatar",
    themeAvatarAssistant: "Assistant avatar",
    themeAvatarChoose: "Choose image",
    themeAvatarClear: "Clear",
    themeAvatarInvalid:
      "Invalid image — only a PNG / JPEG / WEBP file up to 64 KB is accepted.",
    // ── studio logo + nav-tab icons (T-ea81) ──
    themeLogoSection: "Studio logo",
    themeLogoHint:
      "Upload a top-bar logo (PNG / JPEG / WEBP, max 64 KB). Leave empty to keep the built-in mark.",
    themeLogo: "Logo",
    themeNavIconsSection: "Navigation icons",
    themeNavIconsHint:
      "Upload an icon per nav tab (PNG / JPEG / WEBP, max 64 KB). Leave empty to keep the built-in icon.",
    themeNavOffice: "Office icon",
    themeNavReplies: "Replies icon",
    themeNavTasks: "Tasks icon",
    themeNavMonitor: "Monitor icon",
    themeNavGuide: "User guide icon",
    // ── outer-canvas background image (T-081b) ──
    themeCanvasBgSection: "Outer canvas",
    themeCanvasBgHint:
      "Upload an image to lay over the background colour (PNG / JPEG / WEBP, max 64 KB); leave empty for the plain colour. Tile and Sides only paint the canvas beside the content column, so they are invisible on phones, in narrow windows, and in the wide layout (all have no side canvas); Cover fills the whole window.",
    themeCanvasBg: "Canvas tile",
    themeCanvasBgMode: "How to lay it down",
    themeCanvasBgModeTile: "Tile — repeat over the whole canvas",
    themeCanvasBgModeSides: "Sides — one copy against each edge",
    themeCanvasBgModeCover: "Cover — one copy filling the whole window",
    themeCanvasBgModeHint:
      "Sides suits art that stands on its own (a tree either side); it is not mirrored, so draw a symmetric image if you want the two sides to face each other.",
    themeCanvasBgModeCoverHint:
      "Cover only shows through where this theme also gives the top bar / tab bar / content area translucent colours (#RRGGBBAA or rgba). Those sit under text, so the image's contrast against that text is this theme's own responsibility.",
    themeDeleteConfirmLead: 'Delete theme "',
    themeDeleteConfirmTail: '"? This cannot be undone.',
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
    deleteRoleConfirmLead: 'Delete role "',
    deleteRoleConfirmTail:
      '"? Its members and their conversations and lessons will be removed permanently.',
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
    handover: "Claude auto-handover threshold",
    handoverSub:
      "When Claude Code's memory fills to this level, it hands over to a fresh one (40–90%)",
    codexHandover: "Codex auto-handover rounds",
    codexHandoverSub:
      "Automatically refocus after 3 completed context compactions; context percentage is not used.",
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
    deleteManualConfirmLead: "Delete the task type “",
    deleteManualConfirmTail:
      "”? Its manual (definition, SOP, learnings) is removed with it and cannot be restored.",
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
    assigneeMachineIdle: "Idle",
    assigneeMachineBusy: "Busy",
    assigneeMachineOffline: "Offline",
    assigneeMachineUnset: "No machine chosen",
    assigneeMachineNote:
      "Workers of this type boot on the chosen machine and nowhere else. While no machine is chosen, or the chosen one is offline, none is started \u2014 the reason is shown on the worker.",
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
