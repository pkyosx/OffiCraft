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
  diff: {
    ariaLabel: "Line-by-line comparison",
    beforeLabel: "Previous version",
    afterLabel: "Current content",
    addedLine: "Added line",
    removedLine: "Removed line",
    contextLine: "Unchanged line",
    noChanges: "These two versions are identical",
    viewLabel: "Comparison layout",
    viewUnified: "Single column",
    viewSplit: "Side by side",
    wholeDocNote: "Whole document (nothing folded)",
    tooLargeLead: "Too long to compare line by line (",
    tooLargeTail: " lines).",
  },
  connection: {
    lostTitle: "Live updates disconnected",
    lostBody:
      "Reconnecting automatically — what you see may be out of date.",
    reload: "Reload",
    ariaLabel: "Live connection status",
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
  // ── 傳承 / Lore (T-33) ──
  // Half the copy on this tab says "there is nothing to put here, and here is
  // the route that is missing". That is the ticket, not filler: the station
  // serves six lore routes (write, search, read one, read one revision, retire,
  // revive), and half the mockup's blocks need a subject CATALOGUE, a PENDING
  // list, or APPROVE/MERGE — none of which exist. A 0 with no producer reads as
  // "we looked, there is none", so those blocks name the missing route and
  // print no number at all.
  lore: {
    pendingEmpty: "Nothing is waiting for you.",
    pendingFailed: "Could not load the queue:",
    pendingLoading: "Loading…",
    pendingEntries: (n: number) => `${n} memories filed under it`,
    pendingNoEntries: "No memories filed under it yet",
    pendingSuggestApprove: "Suggestion: this is a new subject",
    pendingSuggestMerge: (name: string) => `Suggestion: merge into ${name}`,
    pendingSuggestNone: "No clear suggestion — this one is yours to judge",
    pendingSimilarLead: "Close to:",
    pendingApprove: "Approve",
    pendingMerge: (name: string) => `Merge into ${name}`,
    pendingBusy: "Working…",
    pendingActionFailed: "That one did not go through:",
    reasonSameNormalized: "identical once case and separators are normalised",
    reasonEditDistance1: "one character apart",
    reasonEditDistance2: "two characters apart",
    reasonPrefix: "one starts the other",
    reasonSubstring: "one contains the other",
    listCount: (n: number) => `${n} memories`,
    listTruncated: (n: number) => `Only the most recent ${n} are loaded — there are more.`,
    listLoading: "Loading…",
    listEmpty: "No memories have been written yet.",
    listFailed: "Could not load the memories:",
    listFilterPlaceholder: "Type to filter",
    listFilterNoHit: "No memory matches that.",
    listNoSubject: "Unfiled",
    listGroupExpand: "Expand",
    listGroupCollapse: "Collapse",
    pendingTitle: "Waiting for you",
    entriesTitle: "Memories",
    title: "Lore",
    entryOriginLabel: "From",
    entryOpen: "Open this entry",
    entryClose: "Close this entry",
    entryLoading: "Loading…",
    entryFailed: "Could not read this entry. This is what the server said:",

    fieldTrigger: "Trigger · when you would want to remember this",
    fieldContent: "Content · the only cell that enters an agent's memory",
    fieldRetireWhen: "Retire when · when this stops being needed",
    fieldProblem: "Problem · what went wrong before",
    fieldEvents: "Events · when / what / who / where / what was touched",
    fieldEmpty: "(blank — whoever wrote it left this empty)",
    eventsEmpty: "No events are attached to this entry.",
    eventWhen: "When",
    eventActor: "Who",
    eventPlace: "Where",
    eventObject: "What was touched",
    eventBlank: "(not recorded)",
    fieldsNote:
      "Every cell prints its name, blank ones included, and so does the events section when it is empty. “Blank” and “no such section” must not look the same. Inside an event, who / where / what-was-touched may legitimately be empty, so they are marked “not recorded” rather than filled in — “nobody could find out” and “nobody has looked yet” are different facts.",
    detailStatusLabel: "Status",
    detailWrittenByLabel: "Latest revision written by",
    detailSupersedesLabel: "Supersedes",
    originalTitle: "The original as written (latest revision)",
    originalEmpty: "This entry has no original — it was written before the mechanism existed.",
    shaLabel: "Digest",
    shaEmpty: "(this response carries no digest, so nothing here can be checked against what was stored)",
    revisionsTitle: "Revision timeline",
    revisionsEmpty: "This entry has no revision rows.",
    revisionLabel: "Revision ",
    revisionLabelTail: " ",
    revisionShrinkLead: "hollowed out by ",
    revisionShrinkTail: " characters",
    revisionNoShrink: "not shortened",
    revisionView: "Read this revision",
    revisionHide: "Hide this revision",
    revisionFailed: "Could not read this revision. This is what the server said:",
    revisionsNote:
      "The “hollowed out by N characters” row is the most valuable cell on this tab: when an entry is emptied, the entry COUNT does not move, so no metric built on “how many are left” ever notices.",
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
    // ☑ #<task id> badge sits on the badge row (v2) — no field label.
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
      max: "max effort",
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
    // T-cc3e: the step's working note — where this step got to and what comes
    // next. Label only; the note itself is agent-written free text and renders
    // through <Markdown>, same as the waiting reason above.
    stepNoteLabel: "Note",
    // T-e5b1: the note is COLLAPSED by default (owner: the timeline got too
    // long). These two label the per-step disclosure. The word "note" stays in
    // the collapsed label on purpose — it is the only thing that tells a step
    // WITH a note apart from one without while both are closed.
    stepNoteExpand: "Show note",
    // T-66: the note text is fetched ON OPEN (owner rc-4c8065fb30a5). The card
    // carries only its size, so there is a real gap between the click and the
    // text — and the fetch can fail. Without these two the failed overlay is
    // blank, which reads as "this step's note is empty" — exactly what the
    // entry control already denied by being there at all.
    stepNoteLoading: "Loading note…",
    stepNoteFailed: "Could not load the note. Close and try again.",
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
    // T-e5b1 (owner 2026-08-15): the in-place title / description editors were
    // removed from the task UI, and their whole label family went with them.
    // The capability is untouched — this is the screen's vocabulary, not the
    // capability's. (T-646a folded both tools into `update_task`.)
    descEmpty: "No description yet",
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
      removeConfirm:
        "Remove this artifact from the task card? The file it points at NOW is kept, but if this artifact was ever replaced, every earlier version kept behind it is deleted for good — those files included.",
      loading: "Loading artifacts…",
      loadFailed: "Could not load artifacts — close and reopen to retry",
      downloadHint: "Download",
      openLinkHint: "Open link",
      versionsEntry: "View versions",
      versionsCountTail: "versions",
      versionsTitle: "Artifact versions",
      versionsClose: "Close versions",
      versionsPaneLabel: "View",
      versionsPaneContent: "Content",
      versionsPaneDiff: "Diff",
      versionsCurrent: "Current version",
      versionsVersionLabel: "Version",
      versionsByLabel: "by",
      versionsEmpty: "No earlier versions",
      versionsLoading: "Loading…",
      versionsLoadError: "The version history could not be read",
      versionsContentError: "This version's content could not be read",
      versionsContentGone: "This version points at nothing",
      versionsUnnamed: "Untitled",
      versionsUnpinned: "This artifact is no longer pinned on the task",
      versionsOpaqueLead: "Not a text file (",
      versionsOpaqueTail: ") — look at the two versions one at a time instead.",
    },
  },
  // ── Awaiting-reply page (M2 reply cards, B2) ──
  replies: {
    waitingTitle: "Ask",
    handledTitle: "Recently handled",
    handledHint:
      "Items answered or expired · answers can still be changed",
    empty: "✓ No pending asks",
    loadError: "Failed to load your asks. Please try again.",
    waitedLabel: "Waiting",
    // Opened/answered stamps are always absolute with the date (e.g. 7/13
    // 09:05) — no relative time, no "Today" special case.
    openedAtLabel: "Opened",
    answeredAtLabel: "Answered",
    expiredAtLabel: "Expired",
    // Mark expired (terminal; not an answer; no undo) — the button opens a
    // double-confirm. The "owner/admin-agent since T-6020" framing is still
    // true as history, but T-1b88 (owner 2026-08-07, card rc-3ff94b116970)
    // widened the API to the card's own AUTHOR as well: an agent may retire the
    // unanswered card IT opened. This cockpit button is still the owner's
    // entry point and its behaviour did not change. NOTE the STRINGS did: the
    // expired-card note and the 'recently handled' subtitle used to say "you"
    // marked it expired, and an author-withdrawn card lands in the same pane —
    // a card carries no presser field, so the cockpit cannot tell them apart
    // and the wording no longer claims who pressed it.
    expire: "Mark expired",
    expireConfirm: "Confirm mark expired",
    expireConfirmBodyLead: 'Mark "',
    expireConfirmBodyTail:
      '" as expired? This cannot be undone and does not count as an answer — the member is notified and will open a fresh card if the question still matters.',
    expireError: "Marking expired failed. Please try again.",
    expiredTag: "Expired",
    expiredNote:
      "Expired without an answer; the member will re-ask if it still matters",
    aiPick: "AI pick",
    // The line above the options: what KIND of card this is and, more
    // importantly, what a click DOES. A single-select click answers the card on
    // the spot, and a reply card is one-shot — "I tapped it to see" cannot be
    // undone. The tick box / radio says single-or-several; this says consequence.
    selectedCountLead: "Selected",
    selectedCountTailOne: "option",
    selectedCountTailMany: "options",
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
      // The ONE home for the "released" sentence — read by BOTH entries (chat
      // and the detail panel). Deliberately entry-neutral; see zh.ts.
      releasedTitle: "Outsource · released",
      releasedSub:
        "This outsource worker was released when its task closed; what you see here is a read-only record.",
    },
  },
  workerDetail: {
    back: "Back",
    codename: "Codename",
    model: "Model",
    effort: "Effort",
    // T-7526: the 狀態 cell and its statusOf lookup are retired — see zh.ts.
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
    // T-7526: the four presence words retired with the 狀態 cell — see zh.ts.
    // ── T-32e1/T-f190 lifecycle ops (aligned with the member detail panel) ──
    refocus: "Refocus",
    refocusOfflineHint: "Refocus requires the worker online",
    refocusing: "Refocusing…",
    refocusDone: "Sent",
    refocusError: "Refocus failed",
    refocusSubmittedNote: "Refocus sent · worker respawning…",
    refocusSinceLabel: "Last handover",
    // ⚠️ No `restart` leaf: the wake word is the member panel's
    // `lifecycle.action.spawn` on both panels — see zh.ts.
    stop: "Stop",
    stopping: "Stopping…",
    stopError: "Action failed, please retry",
    modelSave: "Save",
    modelCancel: "Cancel",
    modelError: "Save failed, please retry",
    modelNextSpawnNote:
      "Takes effect now while working; on the next wake if only assigned",
    relocateTitle: "Choose a machine to move to",
    relocateConfirm: "Move to this machine",
    noOnlineMachine: "No online machine",
    lastOp: "Last operation",
    lastOpStart: "Wake",
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
    // dispatch-time persona, so the server re-runs the same assembly — the hint
    // and note both flag that it is today's version. Since T-4595 that assembly
    // is the STAFF boot context minus the whole persona — the role definition
    // (Duty), its insight and its lessons (a worker has no role, so it has none
    // of the three);
    // it contains neither the task nor the manual, so the old
    // "re-assembled from the current task and manual" wording was simply false.
    initialPromptHint: "current re-assembly",
    initialPromptNote:
      "A preview re-assembled from the CURRENT boot documents — not a verbatim record of the dispatch-time text (edits to them since then will differ). It is the staff boot context minus the whole persona — the role definition (Duty), its insight and its lessons: a worker has no role, so it has none of the three, and it picks its task and manual up itself after booting.",
    dash: "—",
  },
  lifecycle: {
    action: {
      // "Spawn" → "Wake" (owner acceptance): the action wakes an existing
      // member, it does not create a new one.
      spawn: "Wake",
      cancel: "Cancel",
      stop: "Stop",
      // The middle rung of the owner's escalation (2026-08-21, 停止 → 加速停止
      // → 強制停止): put the close-out already under way on a clock and tell
      // the member the instant. Not a kill, so no confirm.
      // 🔴 owner 2026-08-22: these three words are ONE button's label at three
      // stages, not three buttons side by side (「同一個按鈕 升級的概念」). Same
      // keys, same words — what changed is that one slot now carries them all.
      "accelerated-stop": "Accelerated stop",
      "force-stop": "Force stop",
    },
    // The one ladder slot has exactly two unpressable presentations, and both
    // say why.
    reason: {
      alreadyStopping: "Already winding down — this button upgrades to Accelerated stop once the close-out is on a clock",
      justAppeared: "Just upgraded — pausing a moment so a repeat click cannot escalate for you",
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
    // Named BOTH fields on purpose. The server answers one indistinguishable
    // 401 for a wrong password and a wrong code, because saying which failed
    // would confirm a correct password to someone who guessed only that half.
    // So the wall cannot know either — and must not pretend it does.
    errorWithCode: "Incorrect password or code, try again",
    codePlaceholder: "6-digit code",
    codeHint: "From your authenticator app",
    // 429 + Retry-After: the pool of concurrent verifications is full — not a
    // wrong password, and not an attempt count; that counter no longer exists.
    //
    // LEAD/TAIL static leaves, assembled by i18n/compose.ts — NOT an
    // interpolation function in the dictionary. theming-and-i18n.md forbids the
    // latter, and the reason is invisible rather than cosmetic: the message-key
    // whitelist admits STRING leaves only, so every word inside a template
    // function is unreachable by a theme's wording overlay AND absent from the
    // generated key list, which leaves the drift gate green while the sentence
    // is silently un-overridable.
    throttledLead: "Too many logins in flight. Try again in",
    throttledTail: "s.",
    // Shown when a refused sign-in turns out to be a MISSING code rather than a
    // wrong password — i.e. this wall was out of date and has just grown its
    // code field. It must explain why the field appeared, or the owner reads it
    // as the password having been wrong.
    codeNowRequired:
      "This server now asks for a code as well. Enter the one in your authenticator app.",
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
    // Failure reasons (T-0648), keyed by the server's `code` — see zh.ts for
    // WHY these live here instead of being shipped as the server's `reason`,
    // and for why only the codes whose sentence is entirely fixed text are
    // listed (the four that embed a Go error string keep the server's own
    // wording, which IS the diagnosis).
    reasons: {
      install_failed:
        "This machine could not be installed, so the assistant was not woken — waking one onto a machine that is not set up would just leave a grey member with no reason. The details below are the installer's full output.",
      roster_missing:
        "This server's own machine record is missing from the roster — the out-of-box setup did not finish. Restart the server and try again.",
      assistant_missing:
        "The assistant that ships with the studio is missing from the roster — the out-of-box setup did not finish. Restart the server and try again.",
      interrupted:
        "Automatic setup was interrupted partway through (the server restarted while it was running), so it never finished. Install this machine yourself from Monitor › Machines › Install, then bring the assistant online.",
      faulted:
        "Automatic setup stopped with an internal error. The server log has the details.",
    },
    detailShow: "Show details",
    detailHide: "Hide details",
    dismiss: "Don't show again",
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
    themeImportLinkLabel: "…or import from a link",
    themeImportLinkPlaceholder: "https://…/theme.json",
    themeImportFromLink: "Fetch and import",
    themeImportLinkWorking: "Fetching…",
    themeImportLinkFailed: "Could not fetch that link",
    themeImportLinkShareNote:
      "A share link carries no identity and never expires — anyone who can reach this studio and has the link can read the theme, including any private images inside it. A single link cannot be withdrawn; the only way to void one is coarse: remove the key that signed it under Settings › Signing keys, which voids every link that key signed at once.",
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
    // 429 from the shared in-flight cap on concurrent credential verifications —
    // never from a failure count, which no longer exists. Without its own branch
    // this renders as "that current password is wrong", which would tell an owner
    // their CORRECT password is wrong. The wait is a moment, not minutes.
    pwdErrorThrottled: "Too many verifications in flight — wait a moment and retry",
    // ── second factor (TOTP) ──
    mfa: "Two-factor authentication",
    mfaSubOff: "Off — your password is the only key",
    // The ship-dark rollout flag. Its own sentence because "the feature is not
    // switched on for this server" and "you have not set it up" are different
    // facts, and conflating them sends an owner looking for a button that is
    // deliberately not there.
    mfaSubUnavailable: "Not enabled on this server",
    mfaOfferIntro:
      "Two-factor is off for this server. Turn it on to let it be set up — this only makes the option available, it does not switch anything on for anyone.",
    mfaOfferOn: "Enable two-factor for this server",
    mfaOfferOff: "Disable the feature for this server",
    // Said out loud because it is the whole safety property of the flag.
    mfaOfferOffHint:
      "This only hides the set-up option. A second factor that is already switched on keeps being required at sign-in, and can still be turned off above.",
    mfaErrorOffer: "Could not change that setting",
    mfaSubOn: "On — an authenticator code is required to sign in",
    mfaIntro:
      "Add a code from your phone's authenticator app to every sign-in. Recommended if this server is reachable from outside your machine.",
    mfaEnrollStart: "Set up two-factor",
    mfaEnrollStarting: "Preparing…",
    mfaScanQrHint:
      "Scan this with your authenticator app, or enter the setup key by hand.",
    mfaQrAlt: "Setup QR code for two-factor authentication",
    mfaScanHint:
      "Add this to your authenticator app, then enter the code it shows to confirm.",
    mfaSecretLabel: "Setup key",
    mfaOpenInApp: "Open in authenticator app",
    mfaCodePlaceholder: "6-digit code",
    // Activating now re-proves the password, because arming a factor is as
    // destructive as removing one — a stolen session must not be able to do it.
    mfaActivateHint:
      "Confirm with your password and the code from your authenticator.",
    mfaErrorActivate: "That password or code is wrong",
    // A 401 that is really a dead session, not a bad credential. These forms
    // deliberately do not bounce to the login wall on 401 (a wrong credential
    // must stay an inline error), so an expired token needs saying out loud.
    mfaErrorSession: "Your session expired — sign in again",
    mfaActivate: "Confirm and turn on",
    mfaActivating: "Confirming…",
    mfaActivated: "Two-factor is on",
    mfaDisable: "Turn off two-factor",
    mfaDisableHint:
      "Confirm with your password and a current code. If you have lost your authenticator, run `ocserverd mfa-disable` on this machine instead.",
    mfaDisabling: "Turning off…",
    mfaDisabled: "Two-factor is off",
    mfaErrorDisable: "That password or code is wrong",
    // A DIFFERENT failure from a wrong code: the view could not read the
    // current state at all, so it does not know what to offer. Saying "that
    // code is wrong" here would name a code the owner never submitted.
    mfaErrorLoad: "Could not read the two-factor status",
    mfaRetry: "Try again",
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
    // 🔴 T-b0bb: a refetched newest page did not join onto the loaded thread
    // and the backfill could not close the seam, so messages are missing from
    // the MIDDLE — count and identity unknown. This string exists because the
    // server has already marked those messages read: the unread count will not
    // betray it and nothing else on screen looks wrong.
    gapSuspected: "Some messages may be missing from this conversation (could not be recovered)",
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
    // M2 batch 18: the uploader filter (options derived from the actual
    // attachment senders, stacking with the Images/Files tabs). Reshaped from a
    // chip row into the checkbox dropdown below by T-51 ②.
    gallerySenderFilterLabel: "Filter by uploader",
    gallerySenderAll: "All",
    // T-51 ②: the chip row became a Jira-style checkbox dropdown, one line
    // collapsed. There is deliberately NO search box — an early version had one
    // and the owner removed it (2026-09-02): the whole objection that shaped
    // this control was 「我怎麼會知道有誰，沒辦法打字」, and the list is sorted by
    // how much each person sent, so the names worth finding are already on top.
    gallerySenderSelected: (n: number) => `${n} selected`,
    gallerySenderClear: "Clear selection",
    // The empty state WITH a filter on. Distinct from galleryEmptyImages /
    // galleryEmptyFiles on purpose: those describe the gallery, this one
    // describes the filter, and saying the first while the second is true tells
    // the reader their files are gone.
    galleryEmptyFiltered: "No files from the uploaders you picked",
    galleryClose: "Close gallery",
    galleryPreviewHint: "Preview in a new tab",
    galleryDownloadHint: "Download",
    // Single-file share link (?sig= HMAC) — copied to the clipboard. No expiry,
    // but not permanent: it follows the signing-key ring, so removing the key
    // that signed it voids it (T-62). Kept in step with its Chinese twin in
    // zh.ts — the first pass fixed one and not the other.
    copyShareLink: "Copy share link",
    shareLinkCopied: "Link copied",
    shareLinkCopyFailed: "Failed to copy link",
    // In-cockpit preview of a .md attachment (T-a1c4): a separate action from
    // download; the overlay renders via Markdown.tsx (not the raw-source new tab).
    // T-7bc2: the chip itself is the trigger now — no separate "action" label.
    mdPreview: {
      download: "Download",
      close: "Close preview",
      loading: "Loading preview…",
      error: "Could not load the preview",
      unavailable: "This file cannot be previewed. Please download it.",
      // T-36 — same "cannot be drawn here", but when the header's new-tab
      // button is present the line must point at THAT, not back at Download:
      // not having to copy the file elsewhere is the whole request.
      unavailableOpenInNewTab:
        "This file cannot be previewed here. Use “Open in a new tab” above.",
      // T-36 — open the attachment in a tab of its own (the share link), for
      // files the browser shows rather than downloads. The note beside it is
      // deliberately in plain words: it describes what the reader will SEE, not
      // the mechanism behind it.
      openInNewTab: "Open in a new tab",
      newTabStaticNote:
        "The new tab only shows the file as it is — buttons and boxes on it will not respond.",
      // T-51 ① — the two paging chevrons. They are the ONLY control for a
      // zoomed image or a text file, where the arrow keys stay with the pan and
      // the scroll, so the accessible name has to stand on its own.
      previous: "Previous item",
      next: "Next item",
      zoomControls: "Zoom image",
      zoomIn: "Zoom in",
      zoomOut: "Zoom out",
      pan: "Drag the image to move it, or scroll with the arrow keys",
    },
    // Corner button on an incoming bubble: reopen this message body in the same
    // full-view overlay (a long answer is hard to read in the thread column).
    // Own messages do not carry it.
    expandMessage: "Open full view",
    // T-4e95 reply-to-a-message: the per-row reply entry, the "replying to"
    // banner above the composer and its x, and the quote line that points a
    // message back at the one it answers.
    //
    // replyQuoteGone is the QUOTE ROW's miss sentence, and it is FIXED. Since
    // 2026-08-21 the server ships the quoted message alongside every reply on
    // every read, so the browser never waits for one: the only way this line
    // appears is that the original is genuinely gone (cleared, or its sender
    // removed). It is not retried and never re-resolves into something else.
    replyAction: "Reply",
    replyingTo: (name: string) => `Replying to ${name}`,
    replyCancel: "Cancel reply",
    // 🔴 THE LABEL CHANGED WITH THE BEHAVIOUR (owner ruling 2026-08-21). It was
    // "Go to the original message" while the control scrolled the thread. It no
    // longer scrolls anything: it reads that one message back and opens it in
    // the full-view overlay. A button that says "go to" and opens a dialog is a
    // small lie told on every reply row, so the words moved with the mechanism.
    replyQuoteJump: "View the original message",
    replyQuoteGone: "This message no longer exists",
    // The read behind replyQuoteJump failed. NOT a claim about whether the
    // original exists — that is replyQuoteGone's job and it lives on the quote
    // line itself. This one only says the fetch did not come back, and it is
    // said once, beside the button that was pressed.
    replyQuoteOpenFailed: "Could not load that message",
    // 🔴 THE BANNER'S MISS LINE IS NOT THE ROW'S, and the two must never be
    // swapped. The ROW asks "did this read build a quote?" — a no there means
    // the original really is gone, so that line is entitled to assert it.
    // The BANNER asks something else entirely: it resolves the target from the
    // LOADED WINDOW alone (messageById). Scroll back, aim at an old message,
    // switch peers and come back to a freshly-loaded newest page, and the
    // message is still there and the send still succeeds — the stored
    // `reply_to` is right and the quote comes back whole — while the banner
    // cannot see it. Printing the row's assertion here tells the owner
    // something he can disprove himself.
    // So the banner says the state-independent true thing instead.
    replyingToEarlier: "Replying to an earlier message",
    // The quote row's own accessible name. This repo has no sr-only utility
    // (see MemberCard.presence-a11y.test.tsx), so the "this is a quotation, not
    // what this person is saying now" fact travels as an aria-label on the row.
    // Without it the accessibility tree linearises a reply as
    // "Mira. Mira. what they said. View the original message. what I said" —
    // the one thing this feature exists to convey is the thing a screen reader
    // could not hear. replyQuoteRole is the version for a quote whose original
    // is gone, so there is no sender to name; replyQuoteRoleWho names them when
    // there is one.
    replyQuoteRole: "Quoted message",
    replyQuoteRoleWho: (name: string) => `Quoted message from ${name}`,
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
    change: "Change",
    settingsSaveOnly: "Save without waking",
    modelReportedTag: "reported at last boot",
    settingsIntentNote: "These are the values to wake WITH.",
    settingsIntentNoteReported: "The model on the card above is what the agent reported at its most recent boot, which can differ from what is set here.",
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
    effortOf: { low: "Low", medium: "Medium", high: "High", max: "Max" } as Record<
      Effort,
      string
    >,
    modelEffortSave: "Save",
    modelEffortCancel: "Cancel",
    modelPlaceholder: "Custom model string (blank = default)",
    modelMachineDefault: "Use this machine's Codex default model",
    claudeAccount: "Claude Account",
    codexAccount: "Codex Account",
    // T-b6d9 — see the zh note: an online member now hands over automatically
    // on save and comes back on the new value; only an offline one waits for a
    // wake. The key keeps its historical spelling (theme wording overlays are
    // keyed on it); the copy is the part humans read.
    modelEffortNextWakeNote:
      "Applied via an automatic handover when online; on the next wake when offline",
    modelEffortError: "Save failed. Please try again.",
    runtime: "Runtime",
    machine: "Machine",
    machineMovingToLabel: "→ Moving to",
    pendingChangeLabel: "→ Changing to",
    windDownForChangeLabel: "Winding down to apply your change",
    // 加速停止 / the second context threshold (T-ed79): the ONLY two causes that
    // put the member on a clock. Saying "winding down" without the time would
    // leave the owner blind to the deadline he just started.
    windDownDeadlineLabel: "Winding down on a deadline",
    windDownByLabel: "by",
    windDownEffectSuffix: "at the latest",
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
    lastOpStart: "Wake",
    lastOpStop: "Stop",
    lastOpOk: "succeeded",
    lastOpFail: "failed",
    lastOpLogLabel: "View log",
    estimatedCost: "est. $",
    costReset: "Reset",
    costResetHint: "Reset this member's accumulated estimated spend to zero. This cannot be undone.",
    costResetConfirm: "Reset to zero",
    costResetError: "Reset failed — the figure was not cleared.",
    costResetConfirmBodyLead: "This resets the accumulated ",
    costResetConfirmBodyTail:
      " to zero and starts counting again from 0. The figure is not kept anywhere else, so it cannot be recovered.",
    terminal: "Terminal · TMUX",
    copyCommand: "Copy command",
    copied: "Copied",
    terminalHint:
      "Paste this in your own terminal to attach to this member's session.",
    initialPrompt: "Initial prompt",
    promptLoading: "Loading…",
    promptError: "Failed to load initial prompt",
    promptRetry: "Retry",
    lessons: "Past lessons",
    expandableHint: "applies on next wake / refocus",
    lessonsLoading: "Loading…",
    lessonsError: "Failed to load lessons",
    lessonsEmpty: "No lessons yet.",
    lessonsShared: "This role's learnings (shared by every agent of this role).",
    lessonsSaveError: "Failed to save lessons",
    // ── Insight (T-3809) — the role journal's THIRD block. Deliberately not
    // worded as a variant of lessons: the whole point of the ticket is that
    // "how this role weighs a call" and "what happened last time" are not the
    // same document. ──
    insight: "Insight (judgement calls)",
    insightLoading: "Loading…",
    insightError: "Failed to load insight",
    insightEmpty:
      "This role has no Insight yet. Nobody has moved any judgement calls over — every role starts empty here.",
    insightShared:
      "Insight is SEPARATE, not private — any authenticated identity can read any role's Insight; only this role's own agent and an admin can write it.",
    insightSaveError: "Failed to save insight",
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
    // ── scheduled messages (T-f059 — the webhook's twin, triggered by the
    // clock instead of an inbound call) ──
    schedmsg: {
      title: "SCHEDULED MESSAGES",
      enabled: "Enabled",
      disabled: "Disabled",
      add: "Add scheduled message",
      empty: "No scheduled messages yet",
      loadError: "Failed to load scheduled messages",
      createError:
        "Failed to create the scheduled message (check the text, the time and the timezone)",
      updateError:
        "Failed to save the changes (check the text, the time and the timezone). Nothing has been changed.",
      unlabeled: "(unnamed)",
      create: "Create",
      save: "Save",
      cancel: "Cancel",
      editLabel: "Edit",
      deleteLabel: "Delete",
      deleteConfirm:
        "Delete this scheduled message? It will never be sent again and this cannot be undone.",
      labelLabel: "Name",
      labelPlaceholder: "A human-facing name, e.g. “Daily check” (optional)",
      bodyLabel: "Message",
      bodyPlaceholder:
        "The message to send when the time comes — delivered to this member verbatim",
      // The row shows the first few lines and offers these. Both words describe
      // what the ROW is showing — the stored message is untouched either way.
      bodyExpand: "Show the whole message",
      bodyCollapse: "Show less",
      cadenceLabel: "Repeats",
      cadenceDaily: "Daily",
      cadenceWeekly: "Weekly",
      cadenceMonthly: "Monthly",
      cadenceCustom: "Custom",
      // ── Custom cadence (T-49e7). The schedule is the INTERSECTION of four
      // sets — which months × which days × which hours × which minutes — and
      // each is listed explicitly: an empty set is a 422, because "fires every
      // time" and "never fires" must not be one keystroke apart.
      // The four headings answer "which <unit>", matching the zh set the owner
      // picked (幾月 / 幾號 / 幾點 / 幾分). ──
      customMonthsLabel: "Which months",
      customDaysLabel: "Which days",
      customHoursLabel: "Which hours",
      customMinutesLabel: "Which minutes",
      customSelectAll: "Select all",
      customClear: "Clear",
      customEmptyHint:
        "Each of the four needs at least one pick, or the schedule has no time to fire at — and the server will refuse it.",
      customNone: "Nothing selected",
      // Summary phrases: each stands on its own under its group heading, and the
      // row summary joins the four with a middle dot.
      customEveryMonth: "Every month",
      customMonthsLead: "Months ",
      customMonthsTail: " of the year",
      customDaysLead: "Days ",
      customDaysTail: " of the month",
      customEveryHour: "Every hour",
      customHoursLead: "Hours ",
      customHoursTail: " of the day",
      customEveryMinute: "Every minute",
      customMinutesLead: "Minutes ",
      customMinutesTail: " of the hour",
      // Evenly spaced minutes collapse to this (ticking only 0, 20, 40 reads as
      // "Every 20 minutes").
      customStepLead: "Every ",
      customStepTail: " minutes",
      // A scattered pick lists the first few and lets this carry the REST —
      // N is how many were not printed.
      customMoreLead: "and ",
      customMoreTail: " more",
      // Row-summary phrases: the word order differs per language, so these are
      // interpolated rather than glued together from fragments in the component.
      weeklyOn: (weekday: string) => `Every ${weekday}`,
      monthlyOn: (day: number) => `Day ${day} of every month`,
      dayOfWeekLabel: "Day of week",
      weekdaySun: "Sunday",
      weekdayMon: "Monday",
      weekdayTue: "Tuesday",
      weekdayWed: "Wednesday",
      weekdayThu: "Thursday",
      weekdayFri: "Friday",
      weekdaySat: "Saturday",
      dayOfMonthLabel: "Day of month",
      // 🔴 Owner ruling 2026-08-10 (card rc-aeef15360ab5): the range stays
      // 1-31, and the price is that a month without that day is skipped whole.
      // This line puts that price in front of whoever is choosing, instead of
      // leaving them to infer it from "why has February been silent".
      dayOfMonthSkipHint:
        "A month that doesn't have this day is skipped entirely — it is not moved to the month's end. Pick 31 and February never fires.",
      hourLabel: "Hour",
      minuteLabel: "Minute",
      timezoneLabel: "Timezone",
      timezonePlaceholder: "IANA timezone name, e.g. Asia/Taipei",
      lastFiredLabel: "Last sent",
      lastFiredNever: "Not sent yet",
    },
    // ── RESUME SUMMARY (T-8b0d — the same wake snapshot resume_summary
    // returns, here for the owner to view) ──
    resumeSummary: {
      title: "RESUME SUMMARY",
      loading: "Loading…",
      error: "Failed to load the wake snapshot",
      retry: "Retry",
      chatCount: "Recent messages",
      // Sizes the WHOLE chat block, not the bodies. Deliberately NOT itemised
      // here: the server's resumeSnapshotParts is the only place that says what
      // goes into it, and every prose copy that listed the ingredients listed
      // an incomplete set (all of them dropped the same one, the cut hint — a
      // fixed block of several hundred runes). "Message chars" was not wrong, but the owner reads these numbers
      // to budget context and would take it for body length — i.e. under-read
      // the real cost.
      chatChars: "Chat block chars",
      tasksReturned: "Tasks returned",
      tasksOpenTotal: "Open tasks total",
      tasksDetailChars: "Task detail chars",
      cardsWaiting: "Waiting reply cards",
      cardsAnsweredRecent: "Recently answered cards",
      stepsOnAnsweredCard: "Steps on answered cards",
      answeredCardStepChars: "Answered-card step chars",
      chatSection: "Recent chat",
      chatEmpty: "No chat messages",
      tasksSection: "Open tasks",
      tasksEmpty: "No open tasks",
      // This is a pointer from the server, not a completion marker; the
      // owner's answer may require a change.
      answeredCardSteps: "Steps on answered cards (read the card; not done)",
      // T-91 — the label for the REVERSE dependency edge the wake snapshot's
      // task row gained. There is deliberately no second key beside it for the
      // handover hold: the cockpit already words that badge once, as
      // `tasks.lockReassigning`, and the resume panel reuses it rather than
      // minting a synonym that could later disagree with it.
      blockingLabel: "Tickets waiting on this one:",
      generatedAtLabel: "This snapshot was taken at",
      // 🔴 THE PER-MESSAGE MARK IS A MARK, NOT A SENTENCE. It used to be
      // "This message is folded — 46 characters kept on the server (re-read it
      // with get_chat)", repeated under EVERY folded message. On a snapshot with
      // hundreds of rows that template outweighed what the folds saved, and the
      // owner called it (2026-08-13). The recovery convention is stated ONCE, in
      // `bodyOmittedNote` at the top of the chat block; each message then carries
      // only the count.
      //
      // 🔴 It still may not share a word with `chatCutLabel` — folded and absent
      // are different failures and the two-way vocabulary guard in the
      // payload-parity test holds them apart. "folded" is this side's word.
      bodyOmittedMark: "folded",
      // Stated once per chat block, so no message has to repeat it.
      bodyOmittedNote: "folded = shortened here, whole text still on the server (re-read with get_chat)",
      // 🔴 "may", not "were": the server raises this marker as soon as a line
      // was cut at its read window, and it never looks past the cut — so it is
      // raised even when nothing older exists (see resumeChatCutHint).
      // 🔴 This NAMES the block; the hint below it states the case. It used to
      // restate the hint's own first sentence, so the reader was told the same
      // thing twice, back to back. The hint is the half that cannot change: an
      // agent receives that string alone and never sees this label, so the hint
      // must stand on its own — this label is cockpit-only wording.
      chatCutLabel: "This line was cut:",
      cardOptionsLabel: "Options offered",
      cardAiPickTag: "AI pick",
      cardPickedTag: "Picked",
      cardAnswerTextLabel: "Free text",
      cardAnsweredAtLabel: "Answered at",
      cardUnanswered: "Not answered yet",
      cardAttachmentsLabel: "Attachments",
      replyCardStatusLabel: "Reply card",
      rosterSection: "Roster",
      rosterEmpty: "This snapshot carries no roster section",
      rosterDutyLabel: "Duty",
      rosterCurrentTaskLabel: "Bound task",
      machinesSection: "Machines",
      machinesEmpty: "This snapshot carries no machine section",
      machinesYouAreOnLabel: "Standing on",
      machinesYouAreOnNone: "No machine binding",
      machineOnline: "online",
      machineOffline: "offline",
      rosterChars: "Roster chars",
      machinesChars: "Machine chars",
      collapse: "Collapse",
      expand: "Expand",
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
    // 帳號歸零 (T-53, owner ruling rc-5c5d7c7c6dcd) — the ACCOUNT's own figure,
    // cleared without touching any member's.
    costReset: "Reset",
    costResetHint: "Reset this account's accumulated spend to zero. No member's figure is touched. This cannot be undone.",
    costResetConfirm: "Reset to zero",
    costResetError: "Reset failed — the figure was not cleared.",
    costResetConfirmBodyLead: "This resets the account's accumulated ",
    costResetConfirmBodyTail:
      " to zero and starts counting again from 0. No member's own figure is touched. The figure is not kept anywhere else, so it cannot be recovered.",
    estimate: "est.",
    fiveHour: "5-hour window",
    sevenDay: "7-day window",
    usage: "usage",
    time: "time",
    overheated: "overheated",
    // T-3b90: usage% is a snapshot from the last report; time% is recomputed
    // now. Without this the two read as if taken together.
    measuredAgoLead: "measured",
    measuredAgoTail: "ago",
    detail: {
      open: "Account details",
      title: "Account details",
      close: "Close",
      accountKey: "Account key",
      accountIdentifier: "Account identifier",
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
      reinstall: "Reinstall",
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
      // Reinstall-over-a-live-warden confirm (server-self row, machine ONLINE).
      // The wording is deliberately NOT shared with the remote-machine install:
      // that one only renders a command to copy, this one overwrites a warden
      // that is serving right now.
      bootstrapConfirmTitle: "Confirm reinstall on the server",
      bootstrapConfirmBodyLead: "“",
      bootstrapConfirmBodyTail:
        "” is online and already running a warden. Installing again OVERWRITES the warden currently in service: every member on this machine is disconnected, and it CANNOT be undone — the replaced warden is not recoverable, the machine has to be installed again and its members brought back online.",
      bootstrapConfirm: "Overwrite and reinstall",
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
        "The warden could not resolve this runtime's binary on the machine — it cannot wake one here",
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
      // ── the cutover mark. Of the four states only ONE speaks — the proven
      // failure; the other three (measured and confirmed in effect / measured
      // but undecidable / never measured) render nothing at all.
      //
      // 🔴 owner 2026-08-04 picked ① on rc-aaa0e7967f8a: drop all three long
      // sentences, keep one very short mark on the proven failure. Verbatim:
      // "these three are all too long, and can the people who see them do
      // anything? do they even understand what happened?" All three complaints
      // hold:
      //   1. Too long — each was a full line of prose eating the machine's row.
      //   2. Not actionable — the old comment itself wrote "a warning nobody
      //      can act on is not a warning" and then, three lines later, "none of
      //      them tells anyone to restart anything". **It contradicted itself**,
      //      and not one of the three told the reader what to do.
      //   3. Not understandable — the old copy already avoided anchor / legacy,
      //      but "a change to how it runs its agents" is itself an internal
      //      concept: the reader does not know what that is or how bad it is.
      //
      // ⇒ **The short mark does not pretend to explain; it only says "something
      // is off here".** Not spelling out what is off is a deliberate trade: the
      // person who sees it has to come and ask, and that beats a sentence whose
      // every word is legible but whose point is unusable.
      //
      // ⚠️ The three sentences were added to fix a real incident: before them,
      // three states shared one blank, so a machine whose cutover had NOT taken
      // effect looked healthy for three hours. **That incident is still fenced
      // off** — the proven failure still has a face, it is just a short one.
      // Only the two "no answer" states fall back to silence, and they never
      // had anything to say (reading them leads to no action).
      cutoverNotInEffect: "Not in effect",
    },
  },
  // ── Backup health (T-da06) — is the scheduled backup still producing
  // retreat points? Shared by BOTH surfaces: the always-mounted topbar
  // indicator and the monitor page's card. The PRIMARY sentence is always
  // derived from `code` (the reason* keys below); the server's `detail` is
  // shown only as secondary diagnostic text.
  // Signing-key rotation (T-62)
  signingKeys: {
    title: "Signing keys",
    intro:
      "The server signs login credentials with a signing key. Several can exist at once: only one signs, the rest still verify — that is the transition window when a key is being replaced.",
    loading: "Loading…",
    signingBadge: "signing",
    retiredBadge: "verify only",
    createdLabel: "Created",
    createdUnknown: "In use since before this was recorded",
    countLabel: (n: number) => `${n} key${n === 1 ? "" : "s"} in the ring`,
    rotateButton: "Create a new key",
    rotateHint:
      "Mints a new key and hands signing over to it. Nobody is logged out: the old key stays and keeps verifying, it just never signs again. Takes effect immediately — no restart.",
    removeButton: "Remove",
    removeConfirmTitle: "Remove this key?",
    removeConfirmBody:
      "Everything this key signed stops working the moment you confirm, with no grace period and no notice to anyone: credentials signed by it are refused, and file share links produced under it break too.",
    removeConfirmWarden:
      "⚠️ Machine (warden) credentials carry no expiry and never lapse on their own. What decides whether this is safe is whether every machine has reconnected — not how many days have passed.",
    removeConfirmCancel: "Cancel",
    removeConfirmOk: "Remove it",
    actionFailed: "That action did not go through, and the server gave no reason.",
    emptyState: "The keys could not be read.",
  },
  backupHealth: {
    title: "Backup health",
    // `unknown` is not a quieter `healthy` — it means "we cannot tell", and
    // the point of this ticket is that a missing retreat point must never look
    // like a present one.
    statusHealthy: "Backups are healthy",
    statusUnhealthy: "Backups are failing",
    statusUnknown: "Cannot tell",
    reasonNeverRan: "No scheduled backup has ever produced a retreat point.",
    reasonStale:
      "The newest scheduled backup is past its freshness window \u2014 the schedule may have stopped.",
    reasonFailed:
      "The most recent scheduled backup failed or was skipped, so no new retreat point was created.",
    reasonUnknown:
      "The watchdog has not evaluated yet, or could not read its own state, so whether you have a retreat point is unknown.",
    reasonUnavailable:
      "The backup status could not be loaded from the server, so whether you have a retreat point is unknown.",
    newestLabel: "Newest scheduled backup",
    newestNever: "never",
    sinceLabel: "Failing for",
    staleAfterLabel: "Freshness window",
    detailLabel: "Server diagnostic",
    ago: "ago",
    loading: "Loading\u2026",
  },
  settings: {
    title: "Settings",
    software: "System update & backup",
    // Global context (T-a241): the boot / lifecycle documents are their own
    // Settings section now, no longer inside the role journal.
    globalContext: "Global context",
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
      "Upload an image to lay over the background colour (PNG / JPEG / WEBP, max 512 KB); leave empty for the plain colour. Tile and Sides only paint the canvas beside the content column, so they are invisible on phones, in narrow windows, and in the wide layout (all have no side canvas); Cover fills the whole window.",
    // The background has its own cap (512 KB), so it cannot reuse the shared
    // themeAvatarInvalid — that one says 64 KB, which is false here (T-72da).
    themeCanvasBgInvalid:
      "Invalid image — only a PNG / JPEG / WEBP file up to 512 KB is accepted.",
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
    globalSection: "BOOT",
    systemName: "System interaction",
    systemSub: "How the system works, injected into every agent · editable",
    customName: "User additions",
    customSub: "Custom content appended to every agent's boot context · editable",
    roleDefsSection: "Role definitions",
    bootName: "Boot steps",
    bootSub: "What an AI follows while starting up · one per runtime · editable",
    bootRuntimeClaude: "Standard",
    bootRuntimeCodex: "Codex",
    bootClaudeName: "Boot steps (Claude Code)",
    bootClaudeSub: "Boot SOP for the Claude Code runtime · editable",
    bootCodexName: "Boot steps (Codex CLI)",
    bootCodexSub: "Boot SOP for the Codex App Server runtime · editable",
    offboardName: "Stop",
    offboardSub:
      "Wrap-up instructions handed to an agent when the server is about to collect its session · editable",
    stopSection: "STOP",
    taskEventSection: "TASK EVENTS",
    acceleratedStopName: "Accelerated stop",
    acceleratedStopSub:
      "What an agent is told when it is asked to wrap up early · carries a deadline · editable",
    taskCloseoutName: "Task close-out",
    taskCloseoutSub:
      "What an agent is told when one of its tasks is judged finished · editable",
    taskReassignPredecessorName: "Task reassignment · to the predecessor",
    taskReassignPredecessorSub:
      "What an agent is told when a task it held is handed to somebody else · editable",
    taskTakeoverWithPredecessorName: "Task reassignment · to the successor",
    taskTakeoverWithPredecessorSub:
      "What an agent is told when it picks up a task somebody else worked on · editable",
    taskTakeoverFreshName: "New task",
    taskTakeoverFreshSub:
      "What an agent is told the first time a task is assigned to it · editable",
    taskUnblockedName: "The ticket blocking your task is released",
    taskUnblockedSub:
      "What an agent is told when the task blocking it is released · editable",
    bootDocReadOnlyNote:
      "This document is shown so you can see exactly what agents are told. Nobody may edit it, and it has no version other than the shipped one.",
    bootDocSaveConfirmAcceleratedStop:
      "Save this accelerated-stop procedure? Every agent asked to wrap up early reads this content, and reads it with only a short window left — it has to be finishable in that time.",
    bootDocSaveConfirmTaskEvent:
      "Save this task-event procedure? Every agent notified of this event from now on reads this content.",
    bootDocNoteHistoryLead: "Version history keeps the last ",
    bootDocNoteHistoryTail:
      " versions, counted in SAVES rather than in time — a run of small saves pushes the older ones out. Restoring the factory version is never affected and is always available.",
    bootDocSaveConfirmBoot:
      "Save these boot steps? Broken boot steps stop agents booting after it from attaching to SSE, so they never come online — silently, with no error anywhere, and with nobody online to fix it. Check the preview first; if it does go wrong, press Restore factory version.",
    bootDocSaveConfirmSystem:
      "Save this system-interaction document? Every agent that boots after the save reads this content.",
    bootDocSaveConfirmOffboard:
      "Save this Stop document? Every session collected after the save reads this content, with nobody online to ask — and NOTHING on this path is counting: a plain stop, a refocus, a machine or model change, a token about to expire, the first context threshold all wait on the agent's own report. The one countdown lives in the Accelerated stop document, not this one. So this text has to be finishable with no clock on it.",
    bootDocSaveConfirmAction: "Save",
    // The click-to-open heading of a stacked document (T-6278). Both boot
    // sequences start closed so the page shows both at once; the label is on
    // the ACTION, so it says what pressing does, not what the state is.
    docExpand: "Expand this document",
    docCollapse: "Collapse this document",
    historyBootSystemTitle: "System interaction · version history",
    historyBootClaudeTitle: "Boot steps (Claude Code) · version history",
    historyBootCodexTitle: "Boot steps (Codex CLI) · version history",
    historyBootOffboardTitle: "Stop · version history",
    historyAcceleratedStopTitle: "Accelerated stop · version history",
    historyTaskCloseoutTitle: "Task close-out · version history",
    historyTaskReassignPredecessorTitle:
      "Task reassignment · to the predecessor · version history",
    historyTaskTakeoverWithPredecessorTitle:
      "Task reassignment · to the successor · version history",
    historyTaskTakeoverFreshTitle: "New task · version history",
    historyTaskUnblockedTitle:
      "The ticket blocking your task is released · version history",
    defaultBadge: "Default",
    edit: "Edit",
    doneEdit: "Done",
    cancel: "Cancel",
    reset: "Reset",
    editorPlaceholder: "Write in Markdown…",
    docReplaceNote:
      "Saving REPLACES the editable half of this document with what is in the editor — there is no per-section merge, so anything not pasted back is gone. The read-only part above is untouched, and there is no way to send an edit to it.",
    docReadOnlyHead: "Read-only (written by the program, not editable)",
    docActionFailed: "That did not go through — try again.",
    docOverCapLead: "Now ",
    docOverCapMid: " characters, over the limit of ",
    docOverCapTail: " — remove some before saving.",
    historyTitle: "Version history",
    historySub:
      "The last 3 revisions are kept; restoring overwrites the current content.",
    historyDeleteNote:
      "Version history covers edits to this document only; deleting the document itself keeps no history and cannot be restored here.",
    historyLoading: "Loading version history…",
    historyError: "Failed to load version history. Please try again.",
    historyEmpty: "No revisions retained yet",
    historyNoContent: "(was empty)",
    historyDefaultContent: "(was on the shipped default)",
    historyByLabel: "Edited by",
    historyDefaultBadge: "Was the default content",
    historyRestore: "Restore this version",
    historyRestoreConfirmLead: 'Restore the version from "',
    historyRestoreConfirmTail:
      '"? The current content is overwritten, but is kept as a new revision.',
    historyRestoreConfirmAction: "Restore",
    historyRestoreError: "Restore failed. Please try again.",
    historySeedTitle: "Initial version",
    historySeedNote: "The content this document shipped with.",
    historySeedRestore: "Restore the initial version",
    historySeedConfirm:
      "Restore the initial version? The current content is overwritten.",
    historySeedUnavailable:
      "The initial version's content cannot be read right now, so it cannot be shown or compared. Restoring it still works.",
    historyBack: "Back to the version list",
    historyBlockedBadge: "Cannot restore",
    historyBlockedReasonLead: '"',
    historyBlockedReasonMid: '" is over the ',
    historyBlockedReasonTail:
      "-character limit and no shorter than what is stored now — the server would refuse this restore.",
    historyOpen: "View this version",
    historyPaneLabel: "View mode",
    historyPaneContent: "Version content",
    historyPaneDiff: "Changes vs current",
    historyDiffNote:
      "Compared against the content stored on the server; unsaved edits in the editor are not included.",
    historyDiffPending:
      "The current content has not finished loading, so there is nothing to compare against yet.",
    historyVersionLabelLead: "This version (",
    historyVersionLabelTail: ")",
    historyActorLead: " (",
    historyActorTail: ")",
    historyCurrentLabel: "Current saved content",
    historyModalEmpty: "This version has no content.",
    historyModalDefaultContent:
      "This version was on the content this document shipped with.",
    historyDefaultUnreadable:
      "This version was on the content this document shipped with, but that default cannot be read right now, so it cannot be shown or compared. Restoring this version still works.",
    historyClose: "Close",
    historyRoleDefTitle: "Role definition · version history",
    historyLessonsTitle: "Lessons · version history",
    historyInsightTitle: "Insight · version history",
    historyGlobalTitle: "Global context · version history",
    historyManualLearningsTitle: "Lessons · version history",
    historySopTitle: "SOP version history",
    historySopSub:
      "Only the SOP is versioned; edits to the purpose and the identifier fields keep no history. The last 3 revisions are kept, and restoring overwrites the SOP only.",
    historyField: {
      text: "Content",
      name: "Name",
      definition_md: "Role definition",
      purpose: "Purpose",
      fields: "Fields",
      sop_md: "SOP",
      learnings: "Lessons",
    },
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
    agentTokenTtl: "Agent token lifetime",
    agentTokenTtlSub: "How long newly started members and outsource workers keep their token",
    ttl12h: "12 hours",
    ttl24h: "24 hours",
    ttl7d: "7 days",
    ttl30d: "30 days",
    notice: "Claude first notice",
    noticeSub:
      "At this level the Stop document is sent, and the agent is asked to close out and hand over under its own power (must be below the final call)",
    handover: "Claude final call",
    handoverSub:
      "At this level the final notice goes out and the handover fires; the session is collected once stop.accelerated_grace_secs elapses (40–90%)",
    codexNotice: "Codex first notice",
    codexNoticeSub:
      "The compaction round at which the Stop document is sent (must be below the final round)",
    codexHandover: "Codex final round",
    codexHandoverSub:
      "Automatically refocus after this many completed context compactions; context percentage is not used.",
    monitoringRefresh: "Monitoring refresh interval",
    monitoringRefreshSub: "Minimum seconds between monitoring refreshes (1–60)",
    seconds: "seconds",
    acceleratedGrace: "Accelerated stop deadline",
    acceleratedGraceSub:
      "How long an agent has once 加速停止 is pressed — and the same clock the second context threshold runs. The agent is told this exact instant (10–3600)",
    rounds: "rounds",
    // T-ae38 (split again by T-30f1): one cap became many. Deleting from these
    // documents costs wildly different amounts — a role definition is a
    // standing description, a lessons doc is append-only environment Q&A — so
    // they no longer share one ruler.
    docCapDuty: "Duty size cap",
    docCapDutySub:
      "Per-role limit on the role definition. The floor is this segment's own shipped default (smaller than every other segment's) and the ceiling is 100000, so this can only be raised — lowering it would leave documents that are legal today able to shrink only.",
    docCapInsight: "Insight size cap",
    docCapInsightSub:
      "Per-role limit on the insight doc. The floor is the shipped default and the ceiling is 100000, so this can only be raised.",
    docCapLearning: "Learning size cap",
    docCapLearningSub:
      "Per-role limit on the lessons doc. The floor is the shipped default and the ceiling is 100000, so this can only be raised.",
    docCapManualSop: "Task manual SOP size cap",
    docCapManualSopSub:
      "Limit on a task manual's SOP (the plan blueprint). Independent of the field below — the SOP is refined in place while the learnings accumulate, so one number could only ever be right for one of them. The floor is the shipped default and the ceiling is 100000, so this can only be raised.",
    docCapManualLearnings: "Task manual learnings size cap",
    docCapManualLearningsSub:
      "Limit on a task manual's learnings doc, independent of the SOP cap above. The floor is the shipped default and the ceiling is 100000, so this can only be raised.",
    // T-c9b4: the wake snapshot's chat budget. Deliberately not folded into the
    // doc-cap wording above — those floors are their own shipped defaults and
    // can only be raised; this one moves in both directions.
    // T-8: backup retention N. The sub-label carries the two facts the integer
    // cannot — versions-not-days and per-pool-not-per-directory — because the
    // person who needs them is the one turning the knob.
    backupRetain: "Backups kept",
    backupRetainSub:
      "How many database backup files are kept. Everything past this number is DELETED from disk on the next backup — it is not moved aside and it cannot be recovered. Two things this number is NOT. It counts VERSIONS, NOT DAYS: it is a count of files, so how far back it reaches depends entirely on how many backups those days happened to produce — a busy day can use the whole allowance in under three days, a quiet one can stretch it past a week. And it is PER POOL, NOT PER DIRECTORY: routine backups (scheduled and manual) and pre-migration backups keep separate allowances, so 5 here means up to TEN files on disk, not five. The range is 1 to 20; the ceiling is a disk budget, since the space used is roughly two times this number times the size of one backup.",
    backupRetainUnit: "backups per pool",
    chatBudget: "Wake chat budget",
    chatBudgetSub:
      "How many characters the chat block of a wake snapshot (resume_summary) may spend — the messages, their folded cards, the snapshot header and the cut hint; the peek sizes itself against the same number. The range is 1000 to 13000 and it can be lowered as well as raised: the chat block is repacked on every read, so a smaller budget simply carries fewer messages, and whatever was left out is still reported as omitted.",
    docUsage: "Used",
    chars: "characters",
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
    manualEditSectionLead: "Edit “",
    manualEditSectionTail: "”",
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
      "Workers of this type wake on the chosen machine and nowhere else. While no machine is chosen, or the chosen one is offline, none is woken \u2014 the reason is shown on the worker.",
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
