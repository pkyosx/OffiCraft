# Interface reference

After reading this page, you can recognize the name and purpose of every tab, section and field in the console, and look up the details and exceptions that apply when you use them. When you want step-by-step instructions, each section links to the page that covers them.

The screens and wording on this page follow the default time-of-day theme and the English interface. A custom theme can replace the interface wording; after that, the text on screen will differ from this page. See [Themes (look and wording)](theme.md).

## Overall layout

![OffiCraft console](assets/cockpit-office.png)

The console has three parts: the top bar, the tab bar, and the content of the current tab. Settings is not a tab; it is a layer that opens over the screen when you press the gear.

**IDs on the screen** — An ID with a copy icon in front of it (a task number, a lore entry, a secret, a file or a reply card on its own card) copies the ID when clicked; the icon turns into ✓ and "Copied" shows for a moment. An ID with an external-link arrow in front of it (a task named on an outsource member, on a reply card or in a task card's "Duplicate of" and "Waiting on" rows) takes you to that task. An ID with no icon cannot be clicked. Both kinds of clickable ID are written with a "#" in front (for example `#T-755` or `#rc-83935a`); what is copied is the ID without the "#".

## Top bar

From left to right:

- **Studio logo** — click it to go back to the Office tab.
- **Studio name** — the pencil icon next to it lets you rename it.
- **Reload** — reloads the whole page.
- **Notification button ("Turn on notifications")** — a bell icon. A slash through the bell means push notifications are not yet turned on for this device; press it to turn them on. No slash means they are on; pressing it again turns them off ("Turn off notifications").
- **Gear** — opens Settings: the sub-pages ("System update & backup", "Security", "Global context", "Roles", "Task manuals", "General", "Advanced", "Theme") and the "User guide".
- **Avatar** — opens the avatar menu: "Profile", "Preferences", "Notification email", "Account security" and "Log out".

The current version number is not in the top bar; it is in "Settings › System update & backup".

When the live connection drops, a "Live updates disconnected" banner appears above the tab bar, with a "Reload" button. While the banner is shown, the content on screen may not be up to date.

After the site switches to a new version, a console that is already open shows the "A new version of the console is available" banner above the tab bar, with a "Reload" button. Until you reload, the screen is still the old version, and some screens (for example, a seasonal theme switching to the next time slot) may not display correctly. A console that is not in the foreground (switched to another tab, browser window minimized, phone switched to another app, or screen locked) reloads by itself. A console in the foreground always only shows the banner, including when, on a computer, its window is only covered by other windows. If there is half-typed text or an attachment not yet sent, or a message is being sent, it does not reload by itself and only shows the banner. When you press "Reload" yourself, any content not yet sent is lost.

**Push notification details**:

- Before turning them on, you must first fill in "Notification email" in the avatar menu. Without it, you cannot turn them on, and the screen says "Add a notification email from the Profile menu first."
- If the browser does not support push, or notification permission has been blocked, the bell is not shown. If notification permission is blocked, you must allow OffiCraft notifications again in the browser or system settings before the bell appears.
- iPhone and iPad need iOS/iPadOS 16.4 or later, and you must first add the console to the Home Screen and open it from the Home Screen before you can turn on notifications.
- You need to turn them on once on each device.
- Notifications are only reminders. The console has no offline mode and cannot be used without a network connection.

For the setup steps on a phone, see [Use it on your phone](mobile.md).

## Tab bar

Seven tabs, from left to right:

| Tab | Badge | Purpose |
| --- | --- | --- |
| Office | Total unread chat messages | Member list and chat |
| Ask | Number of reply cards waiting for your answer | Reply cards members open for you |
| Task | Number of tasks not yet closed; when any task is overdue, a red warning icon appears next to it, and when tasks are only close to their due date it is orange-yellow. Hovering the mouse over it shows, for example, "1 overdue · 2 due soon · 5 open" | Task board |
| Secrets | None | Account keys for members to use |
| Files | None | Reference files that do not belong to any task and that all members can read and update |
| Lore | None | Lore entries accumulated by members and task manuals |
| Monitor | None | Account usage, machines, AI sessions |

## Office

On the left is "Office members"; on the right is the chat. On a phone, only one side is shown at a time; press "Back to members" to return to the list.

**Member list**

- **Staff / Outsource** — the two tabs at the top of the list. The Staff tab shows the number of people; the Outsource tab shows the number of people and the limit on how many can be hired at once (for example, "1 person · cap 3"). When the limit is set to 0, it shows "Assignment paused".
- **Member row** — avatar, name, role (outsource members show their code name and the title of their current task; with no task in progress it shows "No active task"), and on the right, this member's unread count.
- **Status dot** — the dot at the bottom-right of the avatar: Offline, Waking, Online, Stopping, Stopped. Hovering over or clicking the dot shows the status text.
- **Exclamation mark** — a warning below the name, in front of the role. There are two kinds of reasons: the machine reports "signed out of Claude" / "signed out of Codex", or this member's most recent model call failed (sign-in expired, usage limit reached with the reset time, server error, model call failed with a code). Clicking the exclamation mark lists each reason and points you to "Monitor › Machines" to sign in.
- **Recruit a member** — the button at the bottom of the list. Pressed on the Staff tab, it goes to "Settings › Roles" to add a role; pressed on the Outsource tab, it opens "Outsource cap" (the maximum number to hire, or choose "Unlimited").

**Groups and filters**

The Staff and Outsource tabs each have their own groups.

- **Toolbar** — below the tabs. On the left is "Filter"; on the right are "Collapse all" (or "Expand all") and "New group". A new group appears at the top; groups in the same tab cannot have the same name.
- **Group heading** — shows "group name · member count"; click the heading to collapse or expand the group. When collapsed, it shows the group's total unread count. An empty group shows "Drag members here".
- **Group options "⋯"** — on the right of the group heading: "Rename" and "Delete group", plus "Auto-join…" for outsource groups. After a group is deleted, its members go back to "Ungrouped". On a computer, right-clicking does not open a menu; with a keyboard, the menu key or Shift+F10 opens the group options. Clicking outside the menu only closes the menu.
- **Dragging** — with a mouse, press and hold a member row or group heading and move it to drag; a single click on a member still opens the chat, and a click on a group heading still collapses or expands it. On a touch screen such as a phone, you must first long-press and wait for the member row or group heading to lift before dragging; swiping before it lifts scrolls the list. Drag a group heading to change the group order; drag a member to reorder it within the group or move it to another group, and drag it to "Ungrouped" to remove it from a group. You cannot drag while a filter is active. With a keyboard, press Space to pick up, move with the up and down arrow keys, press Space again to drop, and press Esc to cancel.
- **Ungrouped** — members not in any group are listed at the bottom, in the tab's original order, which cannot be changed manually. When no group has been created yet, the "Ungrouped" heading is not shown and the list looks the same as before.
- **Auto-join** — the "Auto-join…" window of an outsource group lists task types. Once the ticked task types are saved, a new outsource member whose task type matches is placed in this group automatically. A task type can belong to only one group; ticking the same task type in another group moves it from its original group. When you tick a type and there are already matching outsource members that are not in this group (in "Ungrouped" or another group), a window opens listing them one by one, all ticked by default, with two options: "Move the ticked ones" and "Apply to new outsource members only".
- **Filter** — you can filter members with "Search names…" and the checkboxes in three sections: "Unread", "Connection" (Offline, Waking, Online, Stopping, Stopped) and "Status" (Working, Waiting, Idle). Each item shows the current number of members next to it. When you tick several items in the same section, a member matching any one of them is shown; when you tick items in different sections, a member must match every section to be shown. The ticked items are remembered in this browser; the search text is not.
- **Where it is stored** — groups, group order, which group each member belongs to, and the auto-join settings are stored on the server, so they are the same when you open the console on another device, and other open pages update in real time. Whether a group is collapsed or expanded is remembered only in this browser. When a staff member is deleted, or an outsource member's task ends, that member's group record is cleared as well.
- **What it affects** — grouping only affects this member list; the order of the list that appears when you @ mention a member does not change.

**Outsource list**

In the Outsource tab, ungrouped outsource members are listed by task creation time, newest first. Each row shows:

- Avatar and presence dot, activity status icon, code name.
- Exclamation-mark warning (if any), task number, task type. Clicking the task number jumps to that task.
- Task title; with no task in progress it shows "No active task".
- Number of unread messages.

Click a row to open the chat with this outsource member; click the avatar to open "Outsource details".

After a task is closed, the outsource member is released and disappears from the list. The outsource member's chat history is kept; when opened from an old message or an old link, it shows "Outsource · released" and "This outsource member was released when its task closed; what you see here is a read-only record.", and actions such as wake or stop are no longer available.

**Chat**

- **Chat header** — the other party's name and status, plus the activity status icon (Working, Waiting, Idle; when online but nothing has been reported yet, it shows "Status unknown"). Click the activity status to "see runtime details". In an outsource member's chat, the header also shows its task title on at most two lines; a longer title is cut off with "…", and hovering over it shows the full title. The icons on the right of the header are, in order: "Asks from this member still waiting on you", "See this member's unfinished tasks", "Open this role's definition settings", "Suggested replies", "Search this conversation", and "Files & images". Staff and outsource members both have the first one. Its red number is how many reply cards this member opened that are still waiting for your answer (cards set to "Later" are not counted); when there are none, no number is shown. Pressing it opens the Ask page on "Awaiting" with "Opener" set to this member.
- **Message area** — date dividers, the "Unread messages below" divider, conversations between members, and reply cards embedded in messages. When you scroll up to read older messages, "Jump to the latest message" appears; press it to go back to the bottom.
- **Conversations between members** — messages that members send to each other also appear in the chat. Several in a row are collapsed into one line, "N messages between agents · expand". Press it to expand; press "Collapse agent-to-agent messages" to collapse them again.
- **Input box** — on the left is "Attach a file"; on the right is "Send". On a computer, Enter also sends and Shift+Enter starts a new line; on a phone, Enter starts a new line and you must press "Send". You can also paste a screenshot directly. Each image can be up to 20 MB; the number of files you can attach to one message is limited, and when you exceed it the screen states the limit.
- **When the other party is offline** — above the input box it says "is offline — your message will queue, or wake them now" with a "Wake" button. If you just leave a message, the member reads it after coming online.
- **Files & images** — all attachments in this chat, split into two pages, "Images" and "Files", which you can "Filter by uploader".
- **Search this conversation** — press the magnifying glass in the header and a search bar appears below the header. It looks for the keyword in the text of messages between you and this member, including older messages not yet loaded. For reply cards, it matches the title, the question text and the reply text you typed (a reply made only by clicking an option has no text to match). Conversations between members and attachments are not searched. English letters are not case-sensitive. The search runs only when you press Enter; when matches are found it jumps to the newest one and shows "current match / total". The up and down arrows jump to the older and newer match. Keywords in the conversation are highlighted, and the message of the current match gets an outline. The conversation icon on the right of the search bar opens a list of all results; click one to jump to it. On a phone, the list closes automatically after you click. The X at the far right ends the search. Jumping to a result does not mark the messages in between as read. When no message matches, it shows "No messages containing “…”".
- **Message actions** — on a computer, when you move the mouse over a message (or move to its buttons with the keyboard), a row of small buttons appears at the top-right edge of the message: for the other party's messages, "Reply", "Copy message" and "Open full view"; for your own messages, "Reply", "Copy message" and "Delete"; for reply cards, "Reply" and "Copy message". They disappear when you move away, and the message text does not shift. When you press with a finger (touch), long-pressing a message or reply card opens a menu that rises from the bottom of the screen, listing the same actions plus "Select text". The message you are pressing gets an outline; tap the darkened area above the menu to close it. Touch-only phones and tablets do not have the row of small buttons; devices with both a mouse and a touch screen can use both.
- **Deleting a message** — for ordinary chat messages you sent within the last 24 hours, the message actions include "Delete". After deletion, the other party is notified and the original position shows "This message was deleted"; clicking it still shows the original text. Deletion cannot be undone. The member may already have started working on the original text before you deleted it. Task messages and reply cards cannot be deleted.
- **Copy message** — for messages with text, the message actions include "Copy message", which copies the whole message's original text: Markdown formatting is kept, member mentions become `@name`, and attachments and quoted messages are not included. For a reply card, it copies the question: the title, a blank line, and the question text, without options, answers or attachments. When you press it with a mouse, the copy button briefly changes to ✓ on success; when you copy from the menu after a long press with a finger, the menu closing means the copy is done, and nothing else is shown. If the copy fails, "Copy failed" appears above the message. Messages with only attachments and deleted messages do not have this action.
- **Suggested replies** — a button in the chat header that sets sentences for this member that you can insert with one click; it works the same for staff and outsource members. The sentences are listed below the input box; clicking one only fills it into the input box, and you press Send. The "Chat suggestions" in "Settings › General" is one list shared by all members and is listed after the member's own sentences; identical sentences are shown only once. When an outsource member's task has a type, that task manual's suggested replies are listed first; staff members do not have this list. For reply cards this member opens, the reply box also lists the member's own sentences, after the suggested replies of the task manual for the card's task and before "Reply-card suggestions". When a member leaves the roster, their own sentences are deleted too.

**Replying to a message or part of it**

- **Reply** — press "Reply" in the message actions and a quote bar ("Replying to Name") appears above the input box. In an outsource member's chat, the input box and the quote bar name the member by its code name alone, for example "Reply to O-7…". After you send, "Quoted message from Name" appears above your message; clicking the quote bar ("View the original message") shows the full quoted message in an overlay. "View in chat" at the top-right of the overlay closes the overlay and returns to that message's position in the chat thread; if the original message is in someone else's chat, it switches to that chat. When the original message has been deleted, there is no "View in chat". Press "Cancel reply" to remove the quote.
- **Reply to this part** — select part of the text in a message and "Reply to this part" appears next to it; pressing it quotes only the text you selected. The selection can span mentioned members and links. A long press with a finger brings up the message actions, so you must first press "Select text" to enter **selection mode**: other messages are blurred and cannot be pressed, and only this one stays clear. For an ordinary message, the whole text is selected first; a reply card is expanded first and you select the text yourself. Selection mode is only for selecting, copying and replying: tapping a link, mention, attachment, image or quote in this message does not open anything (on iPhone, long-pressing a link or image does not show the open preview either), and tapping a reply card does not collapse it. Drag the selection to adjust it to the part you want. Meanwhile the input box, the offline notice and so on below are hidden, and only a row of "Cancel", "Copy" and "Reply" remains at the bottom (after you leave selection mode, the input box comes back as it was, with any half-typed text still there). "Copy" copies the part you selected (the button briefly shows ✓, and you stay in selection mode); "Reply" quotes that part and ends selection mode. A reply card in selection mode shows only its title and question text; its task line, "Mark expired", attachments, options and answer box are hidden (they come back after you leave selection mode). Both the title and the text can be selected, and the whole card can be selected too. When the selection includes the quoted message, "Copy" and "Reply" cannot be pressed. For a waiting reply card, the quote goes into that card's answer box; an answered or expired reply card can only be copied, and "Reply" cannot be pressed. Before any text is selected, neither button can be pressed. The chat thread ends at the top edge of that row, so messages do not go underneath it; if the selected message extends past it, it scrolls up automatically to above the row, and the last line of a long message can also be scrolled to above the row. When you enter by long-pressing in the middle of a long message, the line you pressed stays in place. If the end of the selection is too close to the row, the chat thread scrolls up a little after you let go, so the selection handle moves away from the row of buttons. Press "Cancel", press Esc, or tap the blurred area to end selection mode; the reply card returns to how it was before you entered (if it was collapsed, it is collapsed again). When you use "Reply" to quote from a reply card, that card stays expanded so you can keep typing in its answer box. For cards on the Ask page, you also long-press and then press "Select text". The number of characters you can select is limited; when you go over it, "Selection is over N characters" is shown.

### @ mentioning members

When you type `@` in the chat input box, a task card's message box or a reply card's reply box, the "Mention a member" list appears:

- Staff members show their name and role; outsource members show "Outsource · code name", together with the task number and title when they have a task.
- You can filter by name, role, task number or task title. Move with the up and down keys, select with Enter, Tab or a click, and close the list with Esc.
- In the box, an `@name` that matches a member is highlighted, and after sending it becomes a clickable name; `@` text that is not highlighted is sent as typed.
- A highlighted name is treated as one unit: pressing Backspace after the name, or Delete before it, deletes the whole name; pressing the left or right key moves past the whole name at once.
- After sending, clicking a name in the message opens that member's details. When you came in from the "Ask" or "Task" tab, the back button says "Back to Ask" or "Back to Task"; pressing it returns to that page and shows only the original card, which stays expanded. Clearing the number filter on the page returns you to the full list.

A mention only marks a person in the message. The mentioned member is not notified, and the message is only sent to the members of the current chat.

### Member details

Click the avatar on a member's row to open "Member details"; for an outsource member it opens "Outsource details". "Outsource details" has the same wake, change, stop, refocus and avatar actions as "Member details", and also shows the machine, memory used, estimated $, last operation and the "Terminal · TMUX" command. Fields:

- **Avatar, name** — "Change avatar", "Remove avatar"; "Rename".
- **Wake / Stop** — one button that changes with the member's status to Wake, Stop, Accelerated stop or Force stop; Force stop asks you to confirm first.
- **Model, AI runtime, EFFORT · Thinking** — the values this member reported the last time it started; "—" if it has never reported them. Model shows the full name of the model actually in use.
- **Claude Account / Codex Account** — the account this member uses.
- **Last successful model call** — shown as "X ago".
- **Machine** — the machine the member runs on; while it is moving, this shows "→ Moving to <machine name>", and the label disappears once the move is done.
- **Runtime** — details on the main agent, sub-agents and background waits.
- **Memory used** — how much of its memory the member is using now; for Codex members, the number of memory compactions is also shown.
- **Refocus** — available only when the member is online. The member writes its progress into a handover, ends its current session, and starts a new session that continues from the handover. Its tasks are not affected. When a member's memory is nearly full, the system does the same thing automatically; the threshold is in Settings › Advanced.
- **Last operation** — the result of the most recent wake or stop, with "View log". Reasons a start failed (for example "<machine name> is not logged in to Claude") are also shown here.
- **est. $** — accumulated estimated spend. You can "Reset" it; once reset, it cannot be recovered.
- **Terminal · TMUX** — "Copy command", then paste it into your own terminal to attach to this member's session.
- **Initial prompt** — the startup content rebuilt from the current settings. It may differ from what the member actually received when it started.
- **WEBHOOK ENDPOINTS** — see "Webhook endpoints" on this page.
- **SCHEDULED MESSAGES** — see "Scheduled messages" on this page.
- **RESUME SUMMARY** — the wake snapshot the member will receive the next time it is woken.

If an action fails, "Action failed, please retry" appears below the button.

**Launch settings (Wake and Change)**

When you click "Wake" in "Member details", a launch settings form opens first; the member starts only after you confirm. All four fields start with the member's current settings:

- **AI runtime**: Claude Code or Codex.
- **Model**: Claude Code has quick picks for fable, opus, sonnet and haiku; Codex has quick picks for four model families: astra, sol, terra and luna. You can also type a full model name in the input box, and it is used exactly as typed; leave it empty to use the machine's default model.
- **EFFORT · Thinking**: Low / Medium / High / X-High / Max.
- **Machine**: the machine to run on.

When you pick a Codex model family, each time the member starts it uses the newest model in that family that the machine's Codex recognizes. So after a machine change or a Codex upgrade, the actual model may be different. If the machine's Codex has no model in that family, the member does not start, and "Last operation" shows that Codex's version and its available models.

The same setting has different labels on different screens: in "Member details" and the launch settings it is "EFFORT · Thinking"; in a task manual's "Assigned executor" setting, in "Outsource details" and on task cards it is "Effort" (for example "high effort").

Newly added staff members and the built-in Mira start with no AI runtime selected. The "Claude Code" shown in the launch settings is only a default display value; submitting it unchanged does not save it. Which runtime is actually used is decided the first time the member is sent to a machine; see "Whether a member uses Claude Code or Codex" in [Architecture and how it works](architecture.md). Only a runtime you explicitly picked in the launch settings is saved.

While the member is waking, online or stopping, "Wake" is replaced by "Change", which opens the same launch settings. Below the form, a note explains what will happen when you confirm, based on the member's current status:

- **Online**: the member wraps up what it is doing, then starts again with the new settings. While it wraps up, "Member details" shows "Winding down to apply your change".
- **Stopping**: the new settings are saved, and the member starts again with them after it has stopped.
- **Waking**: if you changed the machine, the member moves to the new machine and starts again with the new settings; if you changed only the model, runtime or effort, the change takes effect the next time it starts.

When the member is stopped, clicking "Wake" starts it directly with the settings you confirmed.

**The three stages of the Stop button**

"Stop → Accelerated stop → Force stop" is one button:

- "Stop" asks the member to wrap up normally and then stop, with no time limit. After you click it, the member starts wrapping up and the button changes to "Accelerated stop".
- "Accelerated stop" gives the wrap-up a fixed deadline and tells the member. The number of seconds is "Accelerated stop deadline" in Settings › Advanced. When the member's memory used reaches "Claude first notice" (for Claude members, the memory-used percentage) or "Codex first notice" (for Codex members, the number of memory compactions), the system starts the wrap-up automatically, and the button also changes to "Accelerated stop".
- Once the wrap-up has a deadline, the button changes to "Force stop". When memory used reaches "Claude final call" or "Codex final round", the wrap-up already has a deadline, so the button changes straight to "Force stop". Clicking it asks you to confirm: "skipping the graceful shutdown. Any unsaved work in progress is lost."
- Right after the button changes stage, it cannot be clicked for a short time, so a double click does not jump straight to the next stage.

**Activity status and warnings**

When a member is online, clicking its activity status icon opens "Runtime". "Current status" shows text such as "Main agent working, plus 2 sub-agents working, waiting on 1 background check or schedule", and lists how long the main agent and each sub-agent have been working, as well as the background work and schedules it is waiting on. Every row other than the main agent starts with a label showing the type exactly as Claude Code or Codex reports it, for example subagent, teammate, workflow, shell or monitor; a schedule's label is "schedule", with its next run time on the right. Background work that only runs a program and does not use the model is listed as waiting: shell (a background command), monitor (command output or external events the member is watching with Monitor; background monitors that Claude Code opens itself, such as the one that waits for comments after an Artifact page is published, are also listed here), MCP task and code. Other background work, such as workflow, cloud session, Claude Code's own memory consolidation (dream), and any new type OffiCraft does not know yet, counts as working and is listed at the sub-agent level under its type name; when the type has no name, it shows unknown. The main agent is the member itself; sub-agents are AI helpers the member starts itself to share the work. A teammate the member starts with Claude Code's agent teams is also listed as a sub-agent while it works, with its own workers listed under it; when a teammate has finished its work and is standing by, it is still listed, with a coffee cup, but it does not count as the member working or waiting: a member with only standing-by teammates left shows Idle. When a sub-agent stops to wait for a sub-agent it started, its row shows a coffee cup; when the main agent is waiting for a foreground sub-agent to finish, the main agent's row shows a coffee cup too. As long as any sub-agent is working, the member as a whole is Working.

Reasons for the exclamation mark:

- **signed out of Claude** / **signed out of Codex**: the runtime on that machine is not signed in.
- **sign-in expired**: the sign-in has expired and you need to sign in again.
- **usage limit reached**: the account's usage has run out; the reset time is shown too when it is known.
- **server error**, **model call failed**: an error on the model service side.

An exclamation mark caused by a failed model call stays until this member later makes a successful call, the usage limit's reset time passes, or the member switches to another AI runtime or account. The usage limit is shared by the whole account: every member on the same account shows "usage limit reached", and as soon as any member on that account makes a successful call, this reason disappears for all of them. If the member that reported the limit moves to another account, the other members on the original account still show "usage limit reached".

The hint for sign-in reasons points to "Monitor › Machines"; there, the version menu in the Claude and Codex columns has "Sign in".

When you use a theme, the icon style may look different; the meaning of each status is the same.

**Avatar**

Only you can change avatars; members cannot change their own avatar or anyone else's. PNG, JPEG and WEBP are supported, and the file must be 64 KiB or smaller. Avatars appear in the member list, chat, tasks, reply cards, details panels and the Monitor page. If a member has no avatar of its own or the image fails to load, the avatar set by the theme is used, and then the built-in icon. An avatar belongs to the member itself, so renaming the member does not affect it.

### Scheduled messages

Both "Member details" and "Outsource details" have a "SCHEDULED MESSAGES" card. The card lists all of this member's scheduled messages; when there are none yet, it shows "No scheduled messages yet". You and the Assistant Mira can set up scheduled messages; other members cannot.

**Fields when adding one**

- **Name**: optional. If left empty, the row shows "(unnamed)".
- **Message**: the message to send when the time comes.
- **Repeats**: Daily, Weekly, Monthly or Custom.
- **Time**: the fields shown depend on the frequency. Daily: hour and minute. Weekly: day of week, plus hour and minute. Monthly: day of month (1 to 31), plus hour and minute. Custom: four groups of checkboxes for month, day, hour and minute.
- **Timezone**: an IANA timezone name, for example `Asia/Taipei`. A new form defaults to `Asia/Taipei`. The timezone must be the name of a real place; only then are messages sent by that place's local time.

**Custom frequency**

A custom frequency is the intersection of the four checkbox groups "Which months", "Which days", "Which hours" and "Which minutes": a message is sent at every moment that matches all four groups. Custom is the only frequency that can send many times a day.

- Each group has "Select all" and "Clear".
- Each of the four groups needs at least one selection. An empty group does not mean "all", and the schedule cannot be saved.
- "Which minutes" lists 0, 5, 10 up to 55: twelve boxes in total.
- Example: select all months, days and hours, and tick minutes 0, 20 and 40: the message is sent every 20 minutes.
- Example: tick months 3, 6, 9 and 12, day 1, hour 9 and minute 0: the message is sent at 09:00 on the first day of each quarter.

The row shows a summary of the four groups, separated by " · ". When the minutes are evenly spaced, they are written in the form "Every 20 minutes"; when all twelve months are selected, the month part is not shown.

**Dates or times that do not exist**

- **Monthly on the 29th, 30th or 31st**: months that do not have that day are skipped entirely; the message is not moved to the end of the month. If you pick the 31st, it is never sent in February. The form shows the same reminder when you pick one of these days. The same applies to "Which days" in a custom frequency.
- **Clocks go forward for daylight saving time and the set time does not exist that day**: for Daily, Weekly and Monthly, the message is sent at the first local time that exists. For example, if it is set for 02:30 and on that day the clock jumps from 02:00 straight to 03:00, that message is sent at 03:00; the next one goes back to the original time. Custom is different: times that fall in the skipped period are not sent that day.
- **Clocks go back for daylight saving time and the same time occurs twice**: the message is sent only once.

**Rules for sending and catching up**

- Times that have already passed when you create the scheduled message are not sent. For example, if you create a daily 09:00 scheduled message at 10:00 a.m., it is first sent at 09:00 the next day.
- The system checks once a minute. Each time is sent only once, and a system restart does not send it again.
- Times missed while the system was down are not sent one by one afterwards; after it recovers, only the most recent missed one is sent.
- Times missed while the scheduled message was disabled are not sent one by one afterwards; when you enable it again, if any times were missed, only the most recent missed one is sent.
- If you edit a time-related field, a time that the edit has just moved past is not sent. For example, if at noon you change a daily 09:00 to 08:00, the 08:00 message is not sent that day. When you change the frequency, your custom frequency selections are kept and are still there if you switch back to Custom.
- Deleting a scheduled message and creating it again makes a new schedule, which is not the same as editing the original one.
- If the member is online, it receives the message immediately. If the member is offline or stopped, the scheduled message does not wake it; the message is kept, and the member reads it the next time it comes online.
- Nothing is sent if the member no longer exists. After an outsource member is released, the scheduled messages under it are no longer sent.

If the message is long, the row shows only the first few lines. Click "Show the whole message" to expand it and "Show less" to collapse it. Collapsing only affects what is shown on screen; nothing is left out of the message that is sent.

For what scheduled messages are good for, see [Doing something on a schedule (Scheduled messages)](schedule.md).

### Webhook endpoints

Both "Member details" and "Outsource details" have a "WEBHOOK ENDPOINTS" card. The card lists all of this member's webhook endpoints; when there are none yet, it shows "No webhook endpoints yet".

**Platform type**

- **Generic (URL token only)**: identified only by the token in the URL; no signature is checked. Anyone who has the URL can send content in.
- **Slack**: the system checks Slack's signature and automatically answers the verification request Slack sends when you set the URL.
- **GitHub**: the system checks GitHub's signature.

If creating an endpoint fails, the screen shows "Failed to create webhook (endpoint ID must be alphanumeric / _ / - and unique)". Usually the endpoint ID contains a character that is not allowed, or it is the same as an endpoint ID this member already has.

**URL**

Each endpoint row shows the first part of the URL, with the token hidden behind dots. Clicking "Copy" copies the full URL, in this format:

```text
https://<address you opened the console at>/in?t=<token>
```

The URL uses the address you opened the console at when you copied it. The token in the URL is the only credential for the generic type.

- The first time you set the URL in Slack, Slack sends a verification request. The system answers it automatically, and it is recorded in recent requests as "Challenge".
- The test event GitHub sends after you create a webhook is recorded in recent requests as "PING" and is not sent to the member.
- When the system responds to an external service, it does not reveal whether the endpoint exists: whether the request was delivered, dropped, or the token does not exist at all, the external service gets the same response (except for request bodies over 1 MiB and Slack's verification handshake).
- If a request body is over 1 MiB, the system rejects the whole request and responds to the external service with 413; it does not truncate and forward it. It is recorded in recent requests as "Dropped · oversize".

**Signing Secret**

Once a Signing Secret is saved, it is not shown again. "Rotate secret" saves a new secret and it takes effect immediately; after that, requests signed with the old secret are dropped with the reason "signature failed". The generic type has no Signing Secret and no "Rotate secret".

**Event stats**

- Before any request has been received, it shows "No calls received yet".
- **Last received**: how long ago the most recent request was received.
- **Dropped**: the number of dropped requests, and the reason the most recent one was dropped:
  - **signature failed**: the signature does not match, usually because the two sides have different Signing Secrets.
  - **hit while disabled**: a request received while the endpoint was disabled.
  - **member gone**: the member this endpoint belongs to no longer exists.
  - **oversize**: the request body is over 1 MiB. This reason is shown on screen as the raw code `oversize`.
- **Recent requests**: the last 5 requests, newest first. Each one is marked with its result: "Delivered", "Dropped" (with the reason), "Challenge" or "PING". Click one to see its "HEADERS" and "BODY"; if the content is too long, only the beginning is kept and it is marked "truncated".

For what webhook endpoints are good for and how members handle what they receive, see [Send outside events to members automatically (webhooks)](webhook.md); for how to add members, use outsource members, and choose models and machines, see [Adding members and hiring outsource](members.md).

## Ask

- **Labels** — Above the list are three labels, and the list shows the cards of the one you pick: "Awaiting" (default), "Later" and "Answered". Each label is a separate button with no number on it. The line under the labels says how many cards the selected label lists once the filters are applied, for example "4 cards"; under "Answered" it reads "3 cards · last 24h". With a filter in force, "Answered" shows its number only after its list has loaded. On a very narrow phone the labels shorten to "Waiting", "Later" and "Done"; the labels, the funnel and "Display" always stay on one line. Use the arrow keys to move between the labels. The page opens on "Awaiting" every time.
- **Awaiting** — Cards waiting for your answer, in the order chosen under "Display". Each card shows the member who opened it and how long it has been "Waiting". These cards are not subject to the 24-hour limit; they stay no matter how long they wait.
- **Later** — Cards you pressed "Later" on, by return time. When the time comes, a card moves back to "Awaiting" by itself. You can still answer them, mark them expired, or press "Bring back now".
- **Answered** — Cards you have answered or marked expired in the last 24 hours (the note "last 24h" sits next to the labels on a computer, and on its own line just above the list on a phone), most recently handled first. Under the person who asked, each card has a line "Handled at" with the time it was answered or marked expired; which of the two is shown by the icon at the start of the card's first line. The page loads these cards only when you open this label. Older cards can still be found by filtering on their ID. Answered cards can be changed with "Change my decision".
- A card you have open stays where it is until you switch label, even if it is answered elsewhere, its "Later" time runs out, or another window sets it to "Later".
- **Filters** — The funnel button ("Filters") to the left of "Display". The number on it is how many fields hold a condition. It opens "Filter reply cards": a panel that drops down under the funnel on a computer, or a panel rising from the bottom of the screen on a phone, closed with "Done". The fields are "Reply-card id", "Opener", "Task priority" and "Task due", and they narrow whichever label is selected, along with its number. Every change applies to the list straight away; there is no apply button. The ID field takes effect only when you press Enter or click outside the field. "Clear filters" at the bottom of the panel clears every field and leaves the selected label as it is. On a computer, Esc, clicking anywhere outside the panel or clicking the funnel again closes it; on a phone, "Done" or tapping the dimmed area does.
  - Entering an ID (or opening a link to a reply card) selects the label that card belongs under, so the card is found whatever its status; its label's number is 1 and the other labels show 0.
- **From a chat header** — "Asks from this member still waiting on you" opens the Ask page on "Awaiting" with "Opener" set to that member, so the cards shown are the ones counted on the button. This still applies after you reload the page. "Clear filters" there returns to the plain Ask page.
- **Display** — Changes the sort order of the "Awaiting" cards: opened newest first (default), oldest first, task priority high to low, task due date soonest first. The current order is shown only inside "Display"; when it is not the default, a small dot appears at the top right of the "Display" button. On a computer "Display" drops down under its button; on a phone it rises from the bottom of the screen like "Filters", closed with "Done", by tapping the dimmed area or by picking an order.
- **Card** — From top to bottom:
  - The first line: an icon for where the card stands (a person with a clock: waiting; a clock: snoozed; a check in a circle: answered; an archive box: expired), then the card's own ID, such as `#rc-83935a` (the first six characters after `rc-`; click it to copy the full ID; the copy icon turns into ✓ and "Copied" for a moment). At the far right are the buttons, the waiting time and the arrow that opens or collapses the card.
  - A card that belongs to a task has a box right under the first line: the task number and priority, then the task name on one line (cut off with "…" when too long), then the due date, written like the task card's (for example "11/28 12:06") and coloured the same way. Only the task number can be clicked; it takes you to the task ("View task"). The box itself cannot be clicked.
  - The question's title in bold, then "Asked by" with the person who opened the card (avatar and name only, an outsource member by its code name such as "O-7", with a chat bubble after the name; click it to open their chat). For an outsource member, the task it is working on is shown under its name as in the office's left column: the task number and type, then the task title. An answered or expired card then has "Handled at" with the time (a secret card has no such line). Waiting and snoozed cards have no time line.
  - Opened, the card draws a thin line under these rows, and below it shows the question, its attachments, single-choice or multiple-choice options (multiple choice shows "Selected N"), the "AI pick" tag, suggested reply sentences, the "Type a reply…" text box and "Quoted"; an answered card shows your answer, "View original options" and "Change my decision".
- **Buttons on the card's first line** — All buttons are icons; hover over one to see its name: View in chat (external-link arrow), Later (clock; on a snoozed card it is Bring back now, a clock with a return arrow), Mark expired (archive box), Copy the whole card (copy icon). "Mark expired" on reply cards in chat and "View in chat" when viewing the original message use the same icons. On touch-only devices such as phones and tablets, the card does not show these buttons; you use the menu that opens when you long-press the card instead (see "Using touch" below). On a narrow screen with a mouse, the buttons, the waiting time and the arrow move together to a line of their own under the ID. On every device, a waiting or snoozed card shows the waiting time as a small pill just left of the arrow: an hourglass plus a time in one unit (for example 5m, 3h, 2d). On a snoozed card it is a clock plus the return date (for example 10/22). Hover over it to see the full "Waiting 3 hours 12 minutes" or the return date and time. The name and the role are cut off with "…" when too long; hover over them to see the full text. On a phone, an outsource member's task is also one line.
- **Copy the whole card** — The copy icon at the end of the buttons copies the card's question: the title, a blank line, then the question text. Mentioned members become `@name`. Options, answers and attachments are not included. You can copy even when the card is collapsed. On success the button briefly turns into ✓; if copying fails, "Copy failed" appears next to it.
- **Using touch** — Long-pressing a card opens the same menu that rises from the bottom of the screen as in chat: first "View in chat"; for an unanswered card, then "Later" ("Bring back now" on a snoozed card) and "Mark expired"; and last "Copy the whole card" and "Select text". These do the same things as the buttons on the card's first line. (You answer in the card's own answer box, so there is no "Reply" here.) Long-pressing does not open or collapse the card. "Select text" enters the same selection mode as in chat: the card opens and shows only the title and the question text, the rest of the page is blurred and cannot be clicked, and links and mentions in the card do not open when tapped. At the bottom is a row of "Cancel", "Copy" and "Reply"; "Reply" puts the selected passage into this card's answer box. Press "Cancel", press Esc, or tap outside the card to finish; the card returns to how it was before.
- **Empty list** — "✓ No pending asks" under "Awaiting", "No reply cards set aside for later" under "Later" and "No reply cards answered in the last 24 hours" under "Answered"; when a filter is in force, any label says "No asks match the current filter" instead.
- **When a card cannot be found** — "No asks match the current filter" means the ID does not exist or is outside the range. "Could not confirm whether this id exists right now. Please try again." means the lookup failed; it does not mean the card does not exist.

**Where reply cards appear**

Reply cards appear in three places. Answering in any one of them has the same effect:

- The "Ask" tab. The number on the tab is the count of cards still waiting for your answer, not counting snoozed cards.
- In a member's chat, between messages. In chat, reply cards are collapsed into a single row by default, labeled "Pending", "Snoozed", "Answered" or "Expired". "Snoozed" is a card you pressed "Later" on whose time has not come yet; when the time comes, or when you bring it back, it changes back to "Pending" by itself. Click the collapsed row to expand it; once expanded, click an empty area of the card to collapse it again. Expanded, it has the same first line (with only "Mark expired" as a button), task box and title as on the Ask tab, but no "Asked by" line: the chat is with the person who asked. An answered or expired card has the same "Handled at" line under the title (except a secret card). The thin line comes under that line, or under the title when there is none.
- Inside a task card's step. Opened, it is laid out like a card on the Ask tab: the same first line (the status icon, the card's ID and "View in chat"), the title, "Asked by", "Handled at" on an answered or expired card (except a secret card), the thin line, then the question. It has no task box: the card is already inside its task. A waiting card is shown open; an answered or expired card is a single row, and the arrow at the right of its first line folds it back to that row.

When a card relates to a task, the Ask tab and chat show the same task box under the card's first line. Clicking the task number ("View task") takes you to the task, the same tag as an outsource member's task. On a task card the reply card sits inside its step; on a phone its options and answer box take the full width, with the "AI pick" tag under the option's text and the answer box on its own line above the paper clip and send buttons. "View in chat" takes you to the message in chat where the card was opened.

**Answering in detail**

- **Single-choice card**: options are numbered 1, 2, 3, 4, with at most 4 options. Clicking an option sends it right away, and you cannot take it back; to change your answer, use "Change my decision".
- **Multiple-choice card**: options have checkboxes, with at most 20 options. You can check several; the screen shows "Selected N". Check what you want, then press send. When the card opens, the options tagged "AI pick" are already checked.
- On a single-choice card, if you type text first and then click an option, the text is sent together with the option.
- **Answering only one part of a card**: when you select text in the card's question, "Reply to this part" appears (with touch, long-press the card first, press "Select text", select the text, then press "Reply" at the bottom). After you press it, "Quoted" and the selected text appear above the reply box, and the option or text you send next is sent together with the quote. The member knows your answer refers to the quoted text. "Remove the quote" removes the quote.
- **Change my decision**: choose a different option, or type a new reply. The new answer replaces the old one, and the member is notified. "View original options" shows the options as they were.
- **Mark expired**: cards on the "Ask" tab and in chat can both be marked expired. Marking a card expired does not count as an answer and cannot be undone. The member is notified, and if the question still matters, the member opens a new card.

For how to answer reply cards, see [Assigning work and answering reply cards](daily.md).

## The file viewer

Chat attachments, task artifacts, files in Files, and their earlier versions are shown the same way everywhere:

- **Markdown** (`.md`) is shown formatted, including images that sit on their own line in the document.
- **JSON** and **XML** are indented automatically. If the file itself is malformed, a note at the top says it cannot be formatted, and the file is shown as stored below it.
- **CSV** is laid out as a table, with the first row as the header. If it is malformed or too large, it is shown as stored.
- **YAML**, plain text, logs and code are shown as stored, with no reformatting and no changes to comments.
- **Web pages** (`.html`) are shown as pages.
- **Images** are shown directly; in the viewer you can zoom and drag them.
- **PDF** is shown directly on screen, and "Open in a new tab" and download are still available. If it cannot be shown, the reason is given and a download is offered.
- **Videos** (`.mp4`, `.webm`, `.mov`) play directly, with play, a progress bar and volume. Below the video you can choose a playback speed of 0.5×, 1×, 1.25×, 1.5× or 2×; it starts at 1× every time you open it. Formats the browser cannot play are offered as a download.
- Other files that the browser can only download are offered as a download.

## Document comments

When you open a text file (`.md`, `.txt`, `.json`, `.yaml`, `.csv`, etc.) or an `.html` document from a chat attachment, a task artifact or a link in a message, the title bar has a "Comments" button. An `.html` document (web page) is shown directly in the viewer; "Open in a new tab" in the title bar opens it on a separate page.

- After you press "Comments", a "Document comments" list appears beside the document (below it on a phone). At the top is "Comment on the whole document", for remarks that are not about a specific passage.
- You can select a passage of text, or click an image in an `.md` document, and press "Add comment". Every comment can be edited or deleted.
- Commented text is highlighted, with an "N comments" marker beside it. Clicking the marker scrolls to the matching comment in the list; clicking the quoted text in the list scrolls back to it in the document.
- When an `.html` document (web page) includes the comment script a member pasted in, you can select any passage of text, as with an `.md` document (link text too), and press "Add comment". In comment mode, dragging to select link text does not open the link. On a web page, commented text is not highlighted, and the quoted text in the list cannot be clicked to go back to the page. When a member has marked places on the page that can take comments, an outline appears when you hover over them, and one click starts a comment on that place (expandable headings, buttons, input boxes and editable text inside the place work as usual; clicking them does not start a comment). In the list such a comment is shown as "Place: name", and clicking it scrolls the page to that place. A web page without the comment script can only use "Comment on the whole document".
- When you press "Send to AI", all comments are combined into one chat message: first a link to the document, then each quoted passage with your remark, one by one.

"To:" above the send button shows which member the comments go to:

- A document opened in a member's chat goes to that chat's member.
- A document opened from a reply card goes to the member who opened the card.
- A document opened from a task card or a task artifact goes to the task's assignee.

If that member is no longer on the roster, or the task has no assignee yet, you cannot send, and the screen says why. Comments you have not sent are kept in this device's browser; they are still there after you close and reopen the document, but you will not see them on another device.

## Comparison page

Comparison links that members give you start with `/diff?`. Opening one shows the differences between the two versions on the same page:

- "Comparison layout" switches between "Single column" and "Side by side". The whole content is shown, with nothing folded.
- The heading of each side shows where that side's content comes from: "Revision #N" is a version from the version history; "Default content" is the content that ships with the system; "Current saved content (read … — changes over time)" means that side is the content as it was when read, and it may be different when you open it again later.
- Click a heading to view that side's original on its own, and press "Back to comparison" to return.
- When the item on one side has been deleted, the screen says "One side of this comparison is no longer there".
- By default, you must be signed in to view a comparison link. A link copied with "Copy external link" opens without signing in, never expires, and cannot be revoked on its own. For how to make external links stop working, see "Signing keys" in [Protect your studio](security.md).

## Opening a task or reply card by URL

Add an ID to the end of the URL, and the page shows only the task or reply card with that ID:

- Task: `/#tasks/<task ID>`
- Reply card: `/#replies/card/<reply-card ID>`

Task and reply-card links that members give you in chat use this format; clicking one opens that task or reply card directly. Once it is open, the "Task ID" or "Reply-card id" field in the filter panel holds the ID, and the funnel button counts it; open the panel, clear the field and press Enter to return to the full list. If the ID does not exist, the Task page shows "No tasks match the current filters" and the Ask page shows "No asks match the current filter". If the server cannot be reached, the screen says "Could not confirm whether … exists right now. Please try again shortly." or shows a loading error; it does not say the item was not found.

## Task

- **Task list** — Open tasks come first, closed tasks after. 50 tasks are listed at first; when you scroll near the bottom, 50 more load, and "Loading…" briefly appears at the bottom.
- **Task count** — At the left end of the row with the funnel button: how many tasks the current filters list, for example "5 tasks". When tasks are grouped, it adds up every group, collapsed ones included.
- **Filters** — The funnel button ("Filters") to the left of "Display". The number on it is how many conditions are in force. It opens "Filter tasks": a panel that drops down under the funnel on a computer, or a panel rising from the bottom of the screen on a phone, closed with "Done". The fields are "Task ID", "Assignees", "Types", "Statuses" and "Due". A drop-down takes effect as soon as you choose; there is no apply button. The ID field takes effect only when you press Enter or click outside the field. "Clear filters" is at the bottom of the panel. It closes the same way as on the Ask page.
  - "Statuses" checks only the not-yet-closed statuses by default, and the button reads "Open". The "Open" and "Closed" rows at the top of the menu check or uncheck a whole group of statuses at once. Because statuses are filtered by default, the funnel button shows 1 and "Clear filters" is shown from the start. Pressing it clears every condition, including status, so done and terminated tasks are listed as well.
  - "Due" lets you check "Overdue" and "Due soon" to list only the open tasks that match.
- **Display** — To the right of the funnel button. Its "Group by" can be None, Due state, Priority, Assignee, Type or Status. With "Due state", tasks are split into the groups "Overdue", "Due soon", "Due later" and "No due date", sorted within each group by due date, soonest first; closed tasks are grouped by due date too. Each group can be collapsed; by default only the first group is expanded. The grouping is remembered in this browser. When it is not "None", a small dot appears at the top right of the "Display" button. "Display" opens and closes the same way as on the Ask page.
- **When a task cannot be found** — "No tasks match the current filters" means there really is no matching task. "Could not confirm whether "T-…" exists right now. Please try again shortly." means the lookup failed.

**The confirmation card before a task is created**

Before a member opens a task, it first checks which task manuals apply, then opens a reply card asking you to confirm the task name, the problem to solve, the task type, the acceptance criteria (AC), who will do it, and the machine, model and effort level to run it with. When no manual fits, the task type on this reply card reads ad-hoc, and once the task is created the task card shows "Ad-hoc". When a task manual sets an identity key, a second open task of the same type with the same identity key value is not created.

**Fields on the task card**

The first line of the card holds the status icon, the task number, the priority and, during a reassignment, the "Reassigning" marker. Below the title, "Assignee" and "Due date" are always shown, plus "Handed over from" and "Identity key" when they are set. When you open the card, "Type", "Artifacts" and "Creator" also appear, right under "Due date".

- **Status icon** — The first thing on the card, drawn in the status colour: a dashed circle for Not started, a circle with a thick progress arc for In progress, a person with a clock for Awaiting my reply, a circle with an arrow pointing out of it for Waiting on external, a flag for Ready to close, a green tick in a circle for Done, a cross in a circle for Terminated, and two overlapping squares for Duplicate. Hover over it to see the status name. Clicking (or tapping) it opens a menu headed by the status name: "Show the waiting reply card", "Show the waiting-external step" (when there is one), Mark duplicate, Terminate, Force close.
- **Task number** — The copy icon in front of it shows that clicking it copies the number ("Copy task number").
- **Priority** — High, Mid, Low, Frozen.
- **Reassigning** — A marker shown after a reassignment until the new assignee takes over or the task closes. It is not a status, so a task can be "In progress" and "Reassigning" at the same time.
- **Assignee** — The member currently working on the task, shown by name only; an outsource member is shown by its code name alone, such as "O-7" ("Handed over from" and "Creator" below are shown the same way). A chat bubble follows the name; clicking the name opens that member's chat. Every person shown this way on a card (task, reply, file and lore cards) has the same bubble and opens the same chat; you yourself have no chat, so your name has no bubble. When there is no assignee yet, it shows "Unassigned".
- **Due date** — On the line below "Assignee". Shows the month and day, plus the time when one is set, for example "11/30 09:06" or "11/30"; a date in another year starts with the year ("2027/1/5 09:00"). Every time on the five kinds of card (task, reply, secret, file and lore) is written this way. Due soon is shown in orange-yellow, overdue in red. Closed tasks are not coloured. When none is set, it shows "Not set".
- **Handed over from** — The previous assignee before the task was reassigned; not shown if it has never been reassigned.
- **Identity key** — A field the task manual designates to tell whether two tasks are about the same thing; clickable when it is a URL.
- **Type** (open card only) — "Open task-type settings" links to this type's task manual. A task without a manual shows "Ad-hoc".
- **Artifacts** (open card only) — How many artifacts the task has; click it to "View artifacts", where you can see versions and diffs. Not shown when there are none.
- **Creator** (open card only) — The member who opened the task; not shown when it is the same as the assignee.
- **Step done/total** and the time pill — Progress, and at the right end of the same line the time spent so far as a small pill: an hourglass plus a time in one unit (for example 2h, 1d). Hover over it to see the full "Elapsed 1d 6h". A closed task's time stops at closing.
- **Task description** — A framed block with its own title bar: an icon, "Task description" and an arrow. A long description shows its first few lines, fading out at the bottom, with "Show all" below it ("Show less" closes it again); a short description is shown whole, with no button.
- **Workflow** — A framed block like "Task description", with the list of steps, each step's DoD, step notes ("Show note"), and dependencies. Its title bar shows "4 steps · 1/4 done".
  - **Status icon on the line** — Each step's status is the icon on the line at its left, in the status colour, and the step's box is lightly tinted in the same colour: a dashed circle (grey) for To do, a circle with a thick progress arc for In progress, a person with a clock (amber) for Awaiting my reply, a circle with an arrow pointing out of it (purple) for Waiting on external, a tick in a circle (green) for Done, and a slashed circle for Superseded, whose whole step is faded. Steps running in parallel each have their own icon on the line too, level with their own title; the "In parallel · N items" row has no icon, and a thin grey bracket beside the line marks which icons belong to the group. Hover over an icon to see the status name.
  - **Shield** — An amber shield on the top-right corner of a step's box means the step needs your approval (a gate). It stays after the step is done.
  - **Time pill** — At the right end of the step name's first line: an hourglass plus the step's time in one unit, the same pill as on the Ask page. Hover over it to see the full "Elapsed 4h 59m". A step that has not started has an empty space there, so the pills line up down the list.
  - A step waiting on external has its reason under the name, after "Waiting". Clicking the title bar of either block collapses that block only, not the card; the collapsed workflow's title bar shows "1/4 done · In progress: <step name>".
- **Ask** — Reply cards embedded in the task, laid out like the cards on the Ask tab without the task box; "View in chat" on the card's first line takes you to the card in chat.
- **Reassign** — The swap icon next to "Assignee" (hover to see "Reassign…").
- **Message box** — The message box on the card sends a message to the assignee. Its placeholder names the assignee; an outsource member is named by its code name alone, for example "Message O-7…". The suggested replies below the box list this task manual's sentences first, then the "Task-message suggestions". Clicking the name of the assignee, creator or previous assignee (hover to see "Message the assignee", "Message the creator" or "Message the predecessor") opens your chat with that member.

**Setting the due date**

Clicking the due date (hover to see "Set due date") opens a small box where you can choose the date, the time ("No time" means the task is not overdue until the end of that day) and "Due soon from" (a number plus days, hours or minutes, then "before"). When "Due soon from" is left empty, the site-wide "Due-soon lead time" is used, and the number field shows that value in light gray; the site-wide setting is in Settings › General. Press ✓ "Apply", press Enter, or click outside the box to save; press ✗ "Cancel" or Esc to discard. The ✕ "Clear due date" next to the date empties the date, and saving removes the due date.

The people who can change the due date are the same as for priority: you, the Assistant Mira, and the assignee. Closed tasks cannot be changed. A member can also set a due date when opening a task, and it can be changed or cleared later in the same way.

**Status in detail**

- The first five statuses (Not started, In progress, Awaiting my reply, Waiting on external, Ready to close) are worked out automatically from the steps; the last three are produced only by closing actions.
- **Awaiting my reply**: a task can have several reply cards waiting for you at once. As long as one of them is unanswered, the task stays at Awaiting my reply. Once you answer, the task returns to the status worked out from its steps, which may be In progress, Waiting on external or Ready to close.
- **Waiting on external**: used only when the matter is out of the member's hands. Waiting for CI to finish or for test results is part of the member's own work and counts as In progress. When a member sets a step to Waiting on external, it writes down the reason, which is shown on that step.
- **Ready to close**: the card says "Every step is done — this task is waiting for its assignee to wrap it up." If a step is added during wrap-up, the task goes back to In progress.

**Steps in detail**

- Step statuses are: To do, In progress, Done, Awaiting my reply, Waiting on external, Superseded.
- DoD is required; a plan with a blank DoD cannot be submitted.
- **In parallel**: unrelated steps can run in parallel; the card shows "In parallel · N items".
- **Notes**: progress records the assignee leaves on a step. When members hand over, the member taking over also learns from here how far the work has got. Once the task is closed, notes can no longer be written.
- **Dependencies**: when a task is blocked by another task, the card shows "Waiting on T-xx", with an external-link arrow right before the number; press the row ("Open T-xx") to see the task that is blocking this one. A dependency is only a label; it does not change the status.
- The assignee can add or delete a single step or change the order along the way, without affecting other steps' notes and reply cards. When the assignee replans the whole thing: completed steps are kept; unfinished steps whose reply card has already been answered or marked expired are kept if the new plan has a step with the same name, and otherwise become "Superseded", keeping the question-and-answer record from that time; all other unfinished steps are removed, together with their notes.
- "Superseded" steps are not counted in "Step done/total", so replanning does not make the progress numbers suddenly jump or go backward. A member cannot set a step to Superseded directly.

**Gates and reply cards in detail**

- A gate step carries the shield ("Needs your approval"), from the moment the plan is submitted and after it is done. Its status is its own stage, like any other step: To do until the member reaches it, Awaiting my reply while its reply card waits for you.
- Once you answer a step's reply card, or the card is marked expired, the step shows In progress: the work is back with the member. "Answered" and "Expired" appear only on the reply card embedded in the step.
- On any unfinished step, when a member runs into something you need to decide, it can open a reply card on the spot. That step becomes a gate for good and carries the shield. Cards can be opened while the task is In progress, Awaiting my reply or Waiting on external; not once the step is done or the task is closed.
- When a member opens a card about a task someone else is responsible for, the card is not tied to a step, and the task does not stop because of it; after you answer, the assignee of the task in question is notified.

**Who can do what**

- **Freeze and unfreeze**: you, the Assistant Mira, and the assignee. Before a member resumes a frozen task, it first opens a card asking for your approval.
- **Terminate**: the task closes as Terminated and cannot be resumed; the assignee is notified and wraps it up.
- **Mark duplicate**: in the search box, type the original task's number or part of its title (all tasks are searched, including closed ones), and pick the original from the results. The task closes as Duplicate and cannot be resumed; the card shows "Duplicate of" and you can "Jump to the original". Members do the same when they find that their task duplicates another one.
- **Reassign**: the task becomes Reassigning, and both the previous and the new assignee receive a handover notice.
- **Force close**: only you and the Assistant can do this, on any open task. After closing, the card shows "Force-closed by" and the reason ("No reason given" if none was entered). This cannot be undone.
- **Artifacts**: "Remove artifact" keeps the current file but permanently deletes its earlier versions. Once the task is closed, artifacts can no longer be added or replaced.

For task statuses, steps, gates and ways of closing, see [Tracking task progress](tasks.md).

## Lore

The Lore tab is divided into sections by who reads the entries: one section for "Everyone", one for each member, and one for each task manual. Each section lists exactly the lore that member reads when it starts (or that a member reads with that task manual).

- **Directory** — Opening the tab shows the directory first: one card per section, one card per row, grouped, in order, under four groups: "Everyone", "Staff members", "Outsource members" and "Task manual"; each group name shows the number of sections next to it (for example "Staff members · 4"), and a group with no sections is not shown. Each card shows the name (members have their avatar), the number of entries, "N not read" in red or "All read", and below that a usage bar with the characters used and the cap (for example "24,500 / 25,000 chars"). The "Everyone" card shows only the characters used and "read by every member", with no usage bar, because "Everyone" lore counts toward each member's own cap. Entry counts include only pinned and active entries; a member's card also counts the "Everyone" entries, so it matches the number of entries listed in that section. Click a card to open that section's page.
- **Collapse and expand** — Click a group name to collapse that group; the arrow after the group name changes from "▾" to "›", and clicking again expands it. Several groups can be expanded at the same time. The directory has the same toolbar row as a section page: on the left, how many lore entries the office holds in all, pinned, active and retired alike, for example "5 entries"; on the right, the filter button and, beside it, "Collapse all", which becomes "Expand all" when every group is collapsed. The first time you open the directory, only "Everyone" is expanded and the other three groups are collapsed; after that it stays the way you last left it, remembered in this browser, and on another browser or device it goes back to how it looked the first time.
- **Find an entry ID** — The directory's filter button opens a panel with one field, "Entry ID" (on a phone, a sheet from the bottom). Typing "212" or "L-212" there and pressing Enter (or clicking elsewhere) goes straight to the section that holds that entry, outlines the entry and scrolls it into view; if the entry is retired, the section's state filter automatically adds "Retired". If the entry's section no longer exists (the member left or the manual was deleted), the directory says so and does not open a section. Once an ID is entered, the filter button shows 1, and "Clear filters" empties the field. This field only finds IDs; to search by title, open a section first and use that section's filters.
- **Section page** — "Lore directory" at the top returns to the directory. Each section has its own URL (for example `#lore/agent/<member ID>`), which you can send to someone so they open that section directly. From top to bottom, a section page has "Lore directory", the section heading and usage bar, a toolbar row, and then the entries. The toolbar row has the same layout as on the Tasks tab: on the left, how many entries match the current filters across the whole section (not only the ones loaded so far), for example "5 entries"; on the right, the filter button. Switching to another section resets the filters to their defaults.
- **Section heading** — One line of small text: the name and, when some entries are not read, "M not read" in red. Below it is a segmented usage bar: a member's section first draws the "Everyone" lore (light color), then the lore of this member that is read (dark color); the two are interleaved in the entry list, and the bar only draws their character counts separately. Two lines sit below the usage bar: the first shows the characters used on the left (for example "**24,890** chars") and "Cap 25,000" on the right; the second shows how many entries from each source are read out of the total, for example "Everyone 7/9 · Kyle 2/5", each preceded by a color block matching the usage bar. A task manual's section shows only the manual's own item. Because the two sources are interleaved, a member's newer entries can still be read even when some "Everyone" entries are not read. A member's section also lists the "Everyone" entries one by one, mixed in with this member's own entries. The "Everyone" section has no cap of its own (it counts toward each member's cap), so its heading shows only the characters used and "read by every member", with no usage bar. In a task manual's section heading, the manual's name is clickable and opens that task manual.
- **Entry order** — The order in which members read them: pinned entries first, then the rest by effective time, newest first; in a member's section, the "Everyone" entries and this member's own entries are sorted together in this order. The order cannot be changed by hand. Scrolling down loads more entries.
- **Collapse and expand an entry** — Click anywhere on a card, as on task cards and reply cards, to collapse that entry. A collapsed entry is still the whole card, with its first line, title, "Author" and "Effective" rows, but the body is cut to two lines, the arrow at the top right points right ("›"), and there is no message box. "Edit" (the pencil) stays on the first line, as a task card's buttons do; pressing it on a collapsed entry opens the entry straight into its editor. Clicking a button, link, menu or the message box inside the card does not collapse it, and neither does selecting text. With the keyboard, move to the card and press Enter or Space. Clicking the collapsed card (or moving to it with the keyboard and pressing Enter or Space) expands it in place; the body is then shown in full and the arrow points down. Every entry starts collapsed, pinned and in-force ones included; what you collapse or expand is not remembered, and leaving the page collapses every entry again. A card cannot be collapsed or expanded, and its arrow is not shown, while it has unsaved changes, is being edited, or has an unsent message.
- **Entries that are not read** — Their card has "Not read" in red after its other tags, and the title, body, author and effective time are in gray, collapsed or expanded; the state icon and buttons keep their usual colors and can still be clicked. These entries are still stored; members just cannot read them. Entries found with the filters or the search start collapsed too; the entry found by searching its ID in the directory opens expanded. Retired entries keep their retired look. The numbers in the section heading and usage bar are always counted over the whole section and are not affected by the filters.
- **Filters** — On a section page, the funnel button at the right end of the toolbar row above the entries opens the filters, the same way as on the Tasks and Replies tabs: on a computer a panel drops down under the button, on a phone a sheet rises from the bottom with "Done". The filters start closed each time you open a section. When filters are applied, the button shows how many fields are set (a state filter other than the default Pinned and Active counts as one). Each change applies immediately; there is no Apply button. The filters cover only that section: ID or title (type "212" or "L-212" to find that entry; type any other text to find entries whose title contains it — bodies are not searched), type, and state, which can be combined with multiple choices; "Clear filters" at the bottom of the panel returns to the default filters. A section already is one member, one task manual or Everyone, so there is no author filter. By default the state filter ticks only Pinned and Active; retired entries are listed only after you tick "Retired" in the state filter. When no entry matches, it says "No lore matches these filters".
- **Entry** — Each entry consists of a type, a title and a body. From left to right, the first line shows the state icon (a pin for Pinned, a check in a circle for Active, a slashed circle for Retired; hover over it to see the state's name), the entry ID (click it to "Copy entry id"), the type tag and the scope tag ("Everyone", the member's name or the task manual's name; you and the Assistant can click it to change the scope, everyone else can only see it); the top right has "Edit" (a pencil) and an arrow showing whether the card is expanded. Below are the title in bold and the body, then the rows "Author" with the author's avatar and name (with a chat bubble after it: click it to open the author's chat), and "Effective" with the time; on a retired entry "Effective" shows a range from that time to when it was retired, for example "11/2 10:30 – 11/27 23:30" (entries retired before the office started recording that time, or bumped to latest after being retired, show only the start). When a retire reason was entered, a "Retire reason" row follows. A retired entry's title is gray. While the author is still on the roster, the bottom of the card has a message box, and messages you send go to the author; on entries you wrote, or when the author has left, there is no message box. When the entry is expanded, the body is shown in full.
- **Change state, type or scope** — Picking a new value on an entry does not save it yet: the entry shows the new value, its border becomes dashed, the changed tag (or the state icon) also gets a dashed outline, and a small round ↺ appears at the top right. A changed entry never gets taller, so the entries below it are never pushed down; on a narrow screen it can get shorter when the new tag is shorter and the tags fit on fewer lines. Picking the original value again means that item no longer counts as a change; when no changes are left, the dashed lines and ↺ disappear.
- **↺ (revert one item)** — Hovering over ↺ shows the original value it will go back to, for example "Revert to “Active”". Clicking ↺ (the same on a computer or a phone) puts that one item back to its original value; other changes on the same entry and other entries are not affected. Clicking the tag itself still opens the menu to change the value.
- **Before saving** — A toolbar stays fixed at the bottom of the screen and remains visible while you scroll: "N unsaved changes" on the left, "Discard all" and "Save changes" on the right. The order of entries, which entries are not read, and the entry and character counts still reflect the state before saving; they are recalculated and re-sorted only after saving. You can go on changing other entries and then press "Save changes" once to save them all; "Discard all" puts everything back to the original values. When you scroll to the bottom, the last entry stops above the toolbar instead of being hidden by it.
- **After "Save changes"** — The whole section is re-sorted by the new result: the entry order, the entries that are not read, the section heading and the numbers in the directory all match what you would see after reloading the page; entries you did not expand by hand are collapsed, and entries you expanded stay expanded. For example, an entry that is unpinned moves to the position its effective time gives it; an entry set to Retired is no longer listed when "Retired" is not ticked in the state filter. Below an entry whose scope was changed, a line such as "L-212 now applies to Everyone" appears and disappears after a few seconds; an entry moved to another section is no longer listed in this one. If some entries were not saved, the toolbar says "Did not go through: L-212 (reason)", those entries keep their dashed border, and you can press "Save changes" again. While saving, the tags and ↺ on entries and the toolbar buttons cannot be pressed, and "Save changes" reads "Saving…".
- **Edit the title or body** — Pressing "Save" saves it; you do not need to press "Save changes" as well. After saving, the whole section is likewise re-sorted by the new result, and other unsaved entries are left as they are. "Restore this version" in the version history works the same way.
- **Leaving before saving** — With unsaved changes, going back to the directory, switching to another tab or pressing the browser's Back button first asks whether you want to leave; if you choose to stay, the entries keep their values. While saving, you cannot leave the section, and the toolbar says "Saving — you can leave once it is done". When you close or reload the page, the browser shows a leave prompt; and when the console has a new version, this page is not reloaded automatically.
- **Phone** — The layout is the same as on a computer, with "Edit" and the arrow at the top right as well; tags that do not fit on one line wrap to the next line.

**Type tags**

| Type | When to write it |
| --- | --- |
| **Instruction conflict** | After actually doing the work, a member finds that an SOP, a system document or an operating instruction does not match the correct way to do it, and following the original instruction would cause errors. Once the original instruction is fixed, this entry should be retired. |
| **Instruction supplement** | The original instruction is not wrong, but it is missing a condition, exception or step that affects the result. Once the original instruction is completed, this entry should be retired. |
| **Owner decision** | Your judgement differed from what the member expected, the member confirmed the reasoning behind it, and that reasoning also applies to similar situations later. |
| **Owner preference** | A preference you stated clearly and expect to apply long-term, such as how to report or the format of deliverables. One-off requests do not count. |
| **Other** | Entries that fit none of the four types above. Early lore entries without a type also fall into this type. |

Clicking an entry's type tag opens the "Change type" menu, which lists these five types with the current type highlighted in the accent color; after you pick another type, the entry's border becomes dashed, and the type changes only when you press "Save changes" in the toolbar at the bottom of the screen. The people who can change the type are the same as those who can edit the entry.

**Scope**

- If a member writes an entry without naming a task, or names a task that has no task type, the entry belongs to the author (member lore). If it names a task that has a task type, the entry belongs to that task manual (task lore).
- "Everyone" lore and each member's own member lore share the same character cap.
- Clicking the scope tag on an entry opens the "Scope" menu (only you and the Assistant can click it). The options are "Task: <manual name>", "Author: <author name>" and "Everyone"; the one tagged "Default" is the original scope when the entry was written, and the one tagged "Current" is the scope now. Entries whose source task has no type have no "Task" option; entries whose author is no longer on the roster have no "Author" option. For task lore, the bottom of the menu has "Open task manual", which opens that manual directly.
- Only you and the Assistant can change the scope. The change takes effect the next time the member starts or reads the manual.

**How the character cap is counted**

The system adds entries one by one to what the member reads, in the order "pinned first, then the rest by effective time, newest first", and stops at the first entry that does not fit. Older entries after that point are not read, and no error message is shown. For the default value and range of each cap, see "Advanced" in [Settings](settings.md).

**States and actions**

Clicking an entry's state icon opens the "Change state" menu, with the options Retire, Activate and Pin.

- **Pinned**: only you and the Assistant can pin or unpin an entry.
- **Retired**: when a member retires an entry, it gives a reason, which is shown on the entry as "Retire reason", and the time of retiring becomes the end of "Effective". A retired entry can be activated again, which clears that end time; retiring it again records a new one.
- **Bump to latest**: a member can change an entry's effective time to now, which moves the entry to the first position after the pinned entries; the original creation time does not change. The Lore tab has no button for this.
- Members can also retire, activate, bump to latest and edit entries they wrote themselves; you and the Assistant can do this to any entry. Once an entry is pinned, only you and the Assistant can retire, activate or edit it.
- **Message box**: the message box at the bottom of an entry card works like the message box on a task card. You can attach files, and below the box are the "Lore-message suggestions". A message you send begins with this entry's ID, for example "[LoreID=L-212] ". The message goes to the author (staff or outsource), pinned entries included; on entries you wrote, or when the author is no longer on the roster, the card has no message box. The placeholder text in the input box says who the message will go to; an outsource author is named by its code name alone, as on a task card. The author name on an entry is only a label and cannot be clicked.

**Edit**

After you press "Edit", the entry's title and body become input boxes, and you can change these two fields. The type is not changed in the edit box; click the type tag on the entry to change it.

- When the title is blank, or a changed field is over its character cap and is not shorter than before, "Save" cannot be pressed, and the reason is shown below the input box.
- After saving, the section's characters used and which entries are not read are updated accordingly.
- Editing does not change the entry's ID, scope, state or effective time, so the entry stays in the same position.
- **Version history**: in the edit box's button row; it is the same window as the version history of documents in Settings. When you change the title or body, the title and body from before the change are kept; each entry keeps its last 3 versions, counted by saves, and versions that are pushed out cannot be recovered. Changing only the type keeps no version. "Restore this version" overwrites the current title and body and does not change the type; the people who can restore are the same as those who can edit.

For how to use lore, see [Making members remember what you taught them (Lore)](lore.md).

## Files

Files in "Files" do not belong to any task, and every member can read and update them. Members read a file by its file ID (starting with `sd-`), and a task manual or a task can point to it by that ID. The ID stays the same when the content is replaced with a new version.

- **File count** — At the left end of the row with "+ Add": how many files the list holds, for example "4 files".
- **+ Add** — The button at the top right of the list. Pressing it shows two items: "Upload file" and "Add link". "Upload file" opens the file picker. After you pick a file, a small dialog appears where you fill in the name (defaults to the file name without its extension) and a description. The file is uploaded and becomes a new row in the list only when you press "Save"; pressing "Cancel" adds nothing. "Add link" opens the same small dialog, where you fill in the URL, name and description in that order. Press "Save" to add it, or "Cancel" to add nothing. If the name is left blank, it is taken from the URL's site and last path segment (for example `docs.google.com/edit`), up to 128 characters. If the URL is invalid or rejected, the reason is shown in the small dialog. The description can be left blank and changed later with the pencil on the item's card in the list. Once created, an item stays a file, an image or a link; its type cannot change.
- **List** — Each item is its own card, like task and lore cards, on a phone and a computer alike. The first line starts with the file ID, shortened to its first six characters after `sd-` (for example `#sd-0c41e9`) behind a copy icon; clicking it copies the full ID ("Copy the full file ID"). Right after the ID is the type as an icon: a page for "File", a chain link for "Link" or a photo for "Image"; hover over it to see the type's name. At the right of the first line are three buttons: "Edit name and description" (a pencil; "Edit link" on a link), "Upload new version" and "Delete" (on a narrow phone the type icon can move to its own line under the ID). Below come the name in bold, drawn like a link (a link has an arrow after it), the description, and two rows: "Created by" with the creator's avatar and name (with a chat bubble after it: click it to open their chat; your own name has none), and "Updated" with who made the last change and when (for example "Mira · 11/25 10:00"). A name too long for its line ends with "…". Click the name to open the content: a link opens in a new tab, and other files open in the viewer above the list. For text files, the viewer's title bar has "Edit", which lets you edit the file as plain text in the same window; saving creates a new version. (If the file is too large to edit on the web, the viewer instead tells you to download it, edit it, and upload a new version.) The pencil on the first line lets you change the name and description, and for a link also the URL. Clicking "Updated" opens this item's version history above the list (the same window as "Versions" on the file page); after a restore, the list updates. The URL `/#shared-docs/<file ID>` opens this item's file page directly.
- **Actions** — Every row has the same two buttons in the same position: "Upload new version" and "Delete permanently". A button that does not apply to this kind of file is dimmed, and hovering over it explains why:
  - **Upload new version**: opens the file picker directly, and the file you pick becomes this item's new version. An image can only be replaced by an image, and other files cannot be replaced by an image. A file that does not match is blocked, and the reason is shown above the list. To change a link's URL, use "Edit link". If an upload fails, the reason is also shown above the list.
- **File page** — At the top are the name (the pencil next to it renames it), the description (click it to change it), and who last modified it and when. Below is the content, shown the same way as described in "The file viewer" section above (a PDF has "Open in a new tab" and a download option below it). A link shows its URL. Renaming or changing the description does not create a new version and does not push out any of the kept old versions.
  - **Edit**: available for all text files (Markdown, plain text, JSON, YAML, CSV, HTML, code and so on). You edit as plain text, and saving creates a new version. For HTML you edit the source code; when reading, it is still shown as a web page.
  - **Upload new version**: replaces the content with another file (for a link, another URL), creating a new version. As in the list, images and non-images cannot replace each other.
  - **Versions**: the same window as the version history of documents in Settings. It first lists the kept old versions with who made each change and when. Click a version to open it. By default it shows that version's content (shown the same way as above); for text files you can switch to "Changes vs current" at the top right for a line-by-line comparison. For an image it shows that version's image and file name. For a link it lists that version's URL and the current URL. "Back to the version list" returns to the list. "Restore this version" asks for confirmation first; after you confirm, that version's content becomes the current version, the name and description stay as they are now, and the previous current version moves into the version history.

**Only the most recent few old versions are kept.** When a new version is saved, the oldest one is permanently discarded.

**The trash can in the list (Delete permanently) cannot be undone**: only you can see this button. The file and all its versions are deleted. Places in task manuals or tasks that reference this ID are not updated automatically, and will no longer find the file.

## Secrets

- **Secret count** — At the left end of the row with "+ Add", above the list: how many secrets there are, for example "3 secrets". The same row as on the Files page.
- **+ Add** — The button at the top right of the list, where the Files page has its own. Pressing it shows the "Add secret" form at the top of the list; the button cannot be pressed again while the form is open.
- **Secret card** — The first line has the secret ID (click it to "Copy secret ID") and, at the right, the edit and delete icons. Below are the name in bold and the note (not shown if empty; a note longer than three lines is cut off, and clicking the note shows it in full or cuts it again), then the rows "Last accessed" (shows "-" if the secret has never been retrieved) and "Updated". On every card, a row whose value is missing shows "-", for example a task's "Created by" on an old task.
- **Add secret** — The form "+ Add" opens. Three fields: name, value and note.

**Field rules**

- **Secret ID**: a fixed ID starting with `sec-`, generated by the system. Members specify a secret by its ID when retrieving it.
- **Name**: a label for people to read. Names can repeat; up to 128 characters.
- **Value**: the secret itself, up to 65536 characters. Once saved, the console never shows it again; you cannot read the value back from either the console or the API.
- **Note**: usage notes written for members. Optional.
- **When editing**: leaving the value field blank keeps the existing value; to change the value, enter the full new value. Editing does not reset the last accessed time.
- **Last accessed**: the last time a member successfully retrieved the value. When a member only views the list or a single secret's information, the value is not retrieved and the last accessed time is not updated.

**Secret reply card**

A card a member opens when it needs a value that is not in the list. What you type in the input box is masked by default; press the eye icon to show it. After you submit, it is never shown again, and only the member that opened the card can retrieve it. When the card is collapsed, it shows answered or expired like other reply cards; when expanded, it shows "Provided", "Retrieved" or "No longer valid". You can also reply with a message in the reply box below the card; once you reply, the card is closed.

For how members retrieve secrets, see "How members fetch secrets" in [Architecture and how it works](architecture.md). For how to add secrets and hand them to members, see [Give members your account keys (secrets)](secrets.md).

## Monitor

**Accounts**

- **5-hour window, 7-day window** — Usage compared with how much of the window has elapsed.
- **Overheated** — When usage in the 7-day window is running too fast compared with how much time has passed, a red dot and "overheated" are shown. This only appears on accounts that still have members reporting.
- **Measured X ago** — How long ago the usage figure was measured.
- **Limit reached** — Appears when the account has reached its usage limit, with the reset time (today, tomorrow or a date).
- **est.** — Accumulated estimated spend. It can be "Reset"; once reset, it cannot be recovered.
- **Account details** — Email, organization, raw reported label, machines, estimated cost. You can "Rename account" here.

**Machines**

- **Machine** — Connection dot, machine name and ID. When members on the machine are still running in the old environment, a "Not in effect" exclamation mark is added at the end. Click the machine name to open a menu: "Details", "Rename", "Install" ("Reinstall" when online), "Uninstall", "Delete".
- **Status, Claude, Codex, Account, CPU, RAM, Battery, Power** — Data reported by the machine. The version numbers in the Claude and Codex columns can be clicked, and the menu includes "Sign in"; the Claude column also has "Upgrade Claude Code".
- **Disk** — Space OffiCraft uses on this machine. Click the number to expand the breakdown: "Database", "Automatic database backups", "Member workspaces", "Logs", "Upgrade leftovers", "Old database copies", "Other", "Disk free / total". Shows "Not measured" if it has not been measured yet. This only measures; it does not delete anything.

**AI Sessions**

- Member, Machine, Account, Model, Memory used, est. $. Outsource members have an extra outsource badge.

For installing machines and signing in to Claude and Codex, see [Install, upgrade, and uninstall](install.md). For resolving warnings, see [Troubleshooting and FAQ](troubleshooting.md).

## Settings

Open it with the gear in the top bar. It opens over the current tab; click any tab to go back to that tab. The home page itself has no path at the top. After you go into an entry, use the path at the top to go back up a level:

- **System update & backup** — Current version, "Check for updates", update settings ("Receive beta versions", "Automatic updates", "Update check interval"), "Backup health".
- **Security** — "Signing keys".
- **Global context** — Documents sent to members: "User Language" and "User additions" at the top under "User preferences", and the rest in three groups, BOOT, STOP and TASK EVENTS. While "Developer mode" (in Advanced) is off, every document except "User Language" and "User additions" is locked.
- **Roles** — Each role's Duty and Insight.
- **Task manuals** — The manual for each kind of task, split into built-in and custom.
- **General** — The suggested-reply lists and the due-soon lead time.
- **Advanced** — Every other site-wide setting, ending with "Developer mode".
- **Theme** — Choose a theme, "Switch by time of day", "Background animation", "Show seasonal theme", plus adding, importing, exporting, editing and deleting themes.
- **User guide** — The guide pages you are reading now. It opens to a list of guide pages; click one to open it. The path at the top of the page is "Settings › User guide › the title of this page", and "User guide" goes back to the list.

For the contents of each page and every setting, see [Settings](settings.md). For themes, see [Themes (look and wording)](theme.md).

### The ⓘ after a title

In Settings, the avatar menu and their dialogs, a row or a setting whose title does not say everything you need to know (its unit, its range, when a change takes effect, who reads it) has an ⓘ after its title with the explanation. Rows whose title says it all, such as "Session length" or "Account security", have no ⓘ. On a computer, hover over the ⓘ (or move to it with Tab) to read it; on a phone, tap it. An ⓘ inside a row that opens a page is not a Tab stop of its own: a screen reader reads its explanation as the row's description. A tapped explanation stays open until you tap it again, tap anywhere else, press Esc or scroll; tapping an ⓘ inside a row does not open that row. Warnings stay on the page as before, for example "Notifications are not sent until this is set." under Notification email, or "Anything past this number is permanently deleted on the next backup." under Backups kept. In Global context a document row shows "editable" (or "not editable") as a small tag beside its name; "User Language" and "User additions" are always editable and carry no tag.

## Avatar menu

- **Profile** — Your name, which you can "Rename".
- **Preferences** — "Theme" (opens the theme settings: choose a theme, "Switch by time of day", "Background animation", "Show seasonal theme"), "Language" (中文 / English), "Layout" ("Narrow" / "Wide"). Until a language is chosen the console is in English. The login page and the first-time setup page use the station's language once one is chosen, otherwise the language this browser last used; logging out keeps the language.
- **Notification email** — The email address push notifications need; until it is set, the row says that notifications are not sent. This address is given to the push services run by browser vendors (the ⓘ on the row says so too); it does not stay only on your server.
- **Account security** — "Change password", "Two-factor authentication", "Passkeys". See "Account security and sign-in" on this page and [Protect your studio](security.md).
- **Log out**

## Account security and sign-in

**Sign-in password**

- The password must be at least 8 characters.
- After you change the password, consoles signed in on other devices and tabs are all signed out; the tab you changed the password in stays signed in. Changing the password does not affect registered passkeys, or the connections of members and machines.
- After every failed sign-in, the system waits about 3 seconds before responding; a correct password is let through immediately. At most 4 sign-in requests are handled at the same time. When that limit is reached, the screen shows "Too many logins in flight. Try again in N s." Failed sign-ins are not counted and do not lock the account.
- With two-factor authentication on, if someone enters the correct password but a wrong code, the password may have leaked. The system sends a message to the Assistant Mira, asking Mira to remind you to change your password. Only one message is sent within a given period, and it includes the running count.

**Two-factor authentication**

- **Enable two-factor for this server** — Only after you press this does "Set up two-factor" appear on the screen. Enabling it does not by itself turn on two-factor authentication.
- **Set up two-factor** — Shows a QR code, a "Setup key", and an "Open in authenticator app" link. All three give the same key to the authenticator app (the app on your phone that generates sign-in codes); use any one of them.
- The QR code is generated in your browser, and the setup key is not sent to any third party.
- **Turn off two-factor**: requires your password and a current code.
- "Disable the feature for this server" is not the same as turning off two-factor authentication: it only hides the setup option, and this button only appears while two-factor authentication is not yet on. If two-factor authentication is already on, you still have to enter a code when signing in.

**Passkeys**

- Adding one asks for your password again, so that someone else who gets hold of your signed-in session cannot quietly add their own passkey.
- Requires Chrome / Edge 129, Firefox 119, Safari 18.4 or later. If the browser version is too old, the screen shows "This browser version does not support passkeys; please update the browser".
- Every passkey in the list shows "Bound to". Ones that are not bound to the current address are marked "Not usable at this address". If you see a "Bound to" address you do not recognize, delete that passkey.
- "May have been copied; deleting it is advised": when someone signed in with this passkey, the usage count it reported was lower than the previous one, which suggests it has been copied. That sign-in was rejected.
- "Last used": the last time this passkey was used to sign in. Shows "Not used yet" if it has never been used.
- After you delete a passkey, it can no longer be used to sign in, effective immediately. Tabs already signed in with it stay signed in until the sign-in expires or you change the password.

For setup steps, see [Protect your studio](security.md).

## Markdown supported in messages and documents

Chat messages, reply cards, task descriptions and steps, Duty documents, task manuals and this user guide all use the same formatting. The following syntax is supported; anything else is shown as plain text:

- Headings: `#` to `######` (there must be a space after `#`).
- Lists: items starting with `- ` or `* `, and numbered lists starting with `1. `. Indented content under an item belongs to that item.
- Quotes: `> `. If the first line of a quote is `> [!NOTE]` (or `TIP`, `IMPORTANT`, `WARNING`, `CAUTION`), it becomes a callout box.
- Code: `` `inline` `` and blocks fenced with ```.
- Tables: a header row followed by a `|---|` separator row.
- Horizontal rule: a line containing only three or more `-`, `*` or `_`.
- Bold: `**bold**`.
- Links: `[text](URL)`. The URL can be http, https, mailto, or an in-site path starting with a single slash (for example `/#tasks/T-12`). Pasted http / https URLs and `/diff?` comparison links also become links automatically.
- A backslash before a symbol (for example `\>`) makes that symbol display as plain text.

Not supported: lists starting with `+ `, `*italic*`, `~~strikethrough~~`, and numbering with a parenthesis like `1) `.

In chat messages, a single line break is a line break; everywhere else you need a blank line to start a new paragraph.

## Related documents

- [Assigning work and answering reply cards](daily.md)
- [Settings](settings.md)
- [Tracking task progress](tasks.md)
- [Glossary](glossary.md)
- [Troubleshooting and FAQ](troubleshooting.md)
