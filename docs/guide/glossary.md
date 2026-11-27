# Glossary

This page lists what each term in the console and the user guide means, and which page has the full explanation. Terms follow the wording of the default theme in the English interface; with a custom theme applied, the words on screen may differ.

## People and roles

- **CEO (You)**: the one user of the studio, that is, you. You set direction, answer reply cards, and decide where the risk limits are. A studio does not support more than one user.
- **Member**: a permanent AI colleague with a name, a role, a model, and memory kept across sessions. "Staff" on the left of the Office tab lists every member. See [Adding members and hiring outsource](members.md).
- **Staff**: a permanent member who holds one role and can take on all kinds of work over the long term.
- **Outsource**: a temporarily hired AI worker, referred to by a code name. One outsource member works on one task only and leaves when it is done. The server hires outsource members automatically when a task manual's "Assigned executor" is set to outsource, when a task is reassigned to outsource, or when a staff member contracts a piece of work out. See [Adding members and hiring outsource](members.md).
- **Assistant**: the role of the built-in member Mira. Mira is there from the start, handles day-to-day upkeep and the odd jobs you hand over, and can also answer reply cards on your behalf.
- **Role**: the position a member holds. Each role has one Duty and one Insight; when a member starts a new session, it still reads the same two documents. See [Making members remember what you taught them (Lore)](lore.md).
- **Duty**: the document that describes what a role is responsible for and how it works, under Settings › Roles.
- **Insight**: the trade-offs and judgement principles a role builds up through its work. Every member can read it; only the role itself, you, and the Assistant can change it. See [Making members remember what you taught them (Lore)](lore.md).
- **Roles**: the place in Settings where roles are managed. You can add, rename, and delete roles, and edit their Duty and Insight.
- **Recruit a member**: the button at the bottom of the Office sidebar. On the "Staff" tab it takes you to Roles to add a role; on the "Outsource" tab it opens the outsource cap setting.
- **Outsource cap (Max hires)**: the most outsource members that can be hired at the same time. It can be set to unlimited; set to 0, it shows "Assignment paused" and no new outsource members are hired.
- **Hire count**: the most outsource members that can work on one task manual's tasks at the same time.

## Member status and actions

- **Status dot**: the dot at the bottom right of the avatar. There are five: Waking, Online, Stopping, Stopped, Offline. See [Architecture and how it works](architecture.md).
- **Waking**: the start command has been sent, and the server is waiting for the member to connect.
- **Online**: the member's connection is up, so it can receive messages and work.
- **Stopping**: a stop has been requested, and the member is wrapping up.
- **Stopped**: a stop has been requested, and the member has ended. The server does not bring it back on its own.
- **Offline**: no one asked the member to stop, but its connection dropped. If the member is set to be online, the server wakes it again automatically.
- **Activity status**: the icon next to the name, showing whether the member is Working, Waiting, or Idle right now; before the member reports, it shows "Status unknown". Sub-agents and background waits are shown under "Runtime". See [Adding members and hiring outsource](members.md).
- **Warning (exclamation mark)**: the exclamation mark below the member's name, before the role. It means the member has hit its usage limit, its login has expired, the server has a problem, a model call failed (with a code), or the machine it runs on is not logged in to Claude / Codex, whether or not the member is online. See [Troubleshooting and FAQ](troubleshooting.md).
- **Wake**: makes a stopped or offline member start working. "Wake" in the member details wakes it immediately.
- **Stop / Accelerated stop / Force stop**: three stages of the same button. Stop asks the member to wrap up and then end; Accelerated stop gives the wrap-up a deadline; Force stop ends it immediately, and unsaved work is lost.
- **Refocus**: asks an online member to write its progress into a handover, end its current session, and start a new session that continues from the handover.
- **Handover**: the member ends its current session and a new session takes over. It happens when memory use reaches a threshold, on Refocus, when settings change, when the member asks for it itself, and when its login credential is about to expire. See [Architecture and how it works](architecture.md).
- **First notice / Final call**: the two memory-use thresholds that trigger a handover. Claude members are measured by percentage, Codex members by the number of memory compactions. Under Settings › General.
- **Memory used**: how much memory capacity the member's current session has used. Codex members also show the number of memory compactions.
- **Runtime**: the section in the member details that shows the current status, sub-agents, and background waits. A sub-agent is an extra AI worker a member opens to split up work and handle part of it; a background wait means the member is waiting on a command or schedule running in the background.
- **Last operation**: the result and reason of the latest wake or stop, in the member details. The reason a wake failed is recorded here.
- **AI runtime**: whether the member runs on Claude Code or Codex.
- **Effort**: how much effort the model puts in: Low, Medium, High, X-High, Max.
- **RESUME SUMMARY**: the wake snapshot the server puts together for a member when it wakes, with recent chat, open tasks, reply cards, the roster, and machines. The member details can preview what the member will get next time.

## Chat and reply cards

- **Reply card**: a card a member opens when it needs your decision. One card is one question, with background and options; it appears in the Ask tab and in chat. See [Assigning work and answering reply cards](daily.md).
- **Later**: moves an unanswered reply card into "Snoozed"; at the time you pick, it returns to Ask on its own. The card still counts as unanswered, and the member who opened it is not notified.
- **Secret reply card**: a reply card in which a member asks you for a secret value. Only the member who opened the card can retrieve the value you enter; it is deleted automatically some time after it is retrieved, or if no one ever retrieves it. See [Give members your account keys (secrets)](secrets.md).
- **Suggested replies**: preset sentences below the input box that you can drop in with one tap. The chat has one set for each member, plus one set shared by all members; reply cards, task messages, and lore messages each have their own set too, and so does every task manual. A reply card's reply box lists, in order, the set from the manual of the task the card belongs to, the set for the member who opened the card, and the reply-card set; a task card's message box lists the task manual's set first, then the task-message set; an outsource member's chat lists the set from the manual of its task first, then the member's own set and the shared set. See [Settings](settings.md).
- **Deleting a message**: you can delete an ordinary chat message you sent within the last 24 hours; its place then shows "This message was deleted".
- **Single choice / multiple choice**: a card whose options are numbered is single choice, and tapping an option sends it; a card whose options have tick boxes is multiple choice, and you send it after ticking.
- **AI pick**: the label on the option the member recommends.
- **Change my decision**: changes the answer of a reply card you have already answered.
- **Mark expired**: ends a reply card without answering it. The member is notified and may open a new card.
- **Quoted passage**: a piece of the card's text you quote when answering a reply card.
- **Reply / Reply to this part**: in chat, quotes a whole message, or only a passage you selected, in your reply.
- **Mention (@)**: type `@` in the input box of chat, a task card, or a reply card to pick a member or outsource member. The person mentioned gets no extra notification.
- **Document comments**: on a document a member sent you, select text, click an image, click a marked spot on a web page, or comment on the whole document, and send it back to the member who sent the document.
- **Comparison page**: a page that shows the differences between two pieces of content. "Copy external link" lets someone without an account see it.

## Tasks

- **Task**: a piece of work split into several steps, each with its own definition of done. See [Tracking task progress](tasks.md).
- **Task status**: eight in all: Not started, In progress, Awaiting my reply, Waiting on external, Ready to close, Done, Terminated, Duplicate. Done, Terminated, and Duplicate are closed states.
- **Ready to close**: every step is done, and the task is waiting for its owner to wrap it up and close it. A task never moves from Ready to close to Done on its own.
- **Reassigning**: a marker shown next to the status while a task is being handed to another member or outsource member.
- **Priority**: High, Mid, Low, Frozen. Frozen tasks are paused.
- **Due date**: the task's deadline, optionally with a time. Within the "Due-soon lead time" before it, the task is Due soon; past the due time, it is Overdue. See [Tracking task progress](tasks.md).
- **Step**: one stage of a task. Its status is To do, In progress, Done, Awaiting my reply, Waiting on external, or Superseded. Each step can have a note.
- **DoD (definition of done)**: a description of what counts as "finished" for a step, written as results that can be checked objectively. See [Having AI follow an SOP for repeated work (Task manuals)](sop.md).
- **Acceptance criteria (AC)**: the observable, verifiable results the whole task must achieve when it is complete, written on the confirmation reply card sent before the task is created (labelled "AC" on the card). Acceptance criteria are for the whole task; a DoD is for a single step. See [Tracking task progress](tasks.md).
- **Gate**: a step marked when the task is planned as needing your approval. The task card announces it as soon as the plan is submitted, and when the step is reached, the member opens a reply card and waits for you. Steps that have opened a reply card or are marked as gates are labelled "Approval" on the task card. See [Tracking task progress](tasks.md).
- **In parallel**: a group of steps that do not depend on each other and can be done at the same time. The task card shows "In parallel · N items".
- **Artifacts**: files a task produces and attaches to itself. Versions are kept and can be compared.
- **Files**: reference files that belong to no task and that every member can read and update; one of the main tabs. Members read them by an ID starting with `sd-`, and the last few old versions are kept. See [Interface reference](interface.md).
- **Reassign**: hands a task to another member or outsource member, optionally with a handover note.
- **Terminate**: ends a task that no longer needs doing.
- **Mark duplicate**: marks a task as a duplicate of another task; you pick the original.
- **Force close**: closes a task while some steps are not done. A reason is optional. This cannot be undone.

## Letting the studio run itself

- **Task manual**: the standard way to handle one kind of work, with its purpose, the fields it needs, an SOP, and the assigned executor, under Settings › Task manuals. See [Having AI follow an SOP for repeated work (Task manuals)](sop.md).
- **Task definition**: the page in a task manual that describes "what this task is, what information it needs, and how to do it".
- **SOP**: the "How should it be done?" part of a task manual. Members plan a task's steps from it.
- **Identity key**: the fields in a task manual used to decide "is this the same piece of work". If the identity key matches and the original task is not closed yet, no new task is opened.
- **Assigned executor**: the member or outsource that a task manual names to handle this kind of task. Choosing outsource requires choosing a machine.
- **Lore**: things members learn on the job and need to remember for the long term; one of the main tabs. See [Making members remember what you taught them (Lore)](lore.md).
- **Member lore / Task lore / Everyone**: the scope a lore entry applies to: one member, one task manual, or all members.
- **Lore type**: Instruction conflict, Instruction supplement, Owner decision, Owner preference, Other.
- **Pinned / Active / Retired**: the states of a lore entry. Entries collapsed to one line and tagged "Not read" on the Lore tab cannot be read by members.
- **Global context**: documents every member reads, in three groups: start-up (BOOT), shut-down (STOP), and task events (TASK), under Settings › Global context. See [Settings](settings.md).
- **User additions**: the document in Global context kept for you to write to all members.
- **Default content / Sync to default**: the factory content of a document; Sync to default changes the document back to its factory content.
- **Version history**: documents in Settings keep their last few edits, which you can compare or restore. See [Settings](settings.md).
- **Secrets**: secret values handed to members, such as account passwords and API keys; one of the main tabs. Values are stored encrypted, and members fetch them from the server when they need them. See [Give members your account keys (secrets)](secrets.md).
- **vault.key**: the key file used to encrypt secrets, in the same folder as the database. Database backups do not include it; if you move a backup to another computer to restore it without this file, the secrets cannot be read. See [Protect your studio](security.md).
- **Scheduled message**: a message sent to a member automatically at the frequency, time of day, and time zone you set, configured in the member details. See [Doing something on a schedule (Scheduled messages)](schedule.md).
- **Webhook endpoint**: a URL that belongs to one member; when an outside service calls it, the content becomes a chat message to that member. See [Send outside events to members automatically (webhooks)](webhook.md).
- **Endpoint ID**: the name in a webhook endpoint's URL. It cannot be changed after the endpoint is created.
- **Signing Secret**: a shared secret that Slack and GitHub webhook endpoints use to verify a request really came from that service. See [Send outside events to members automatically (webhooks)](webhook.md).

## Account, security, and settings

- **Account security**: the place in the avatar menu for changing your password, two-factor authentication, and passkeys. See [Protect your studio](security.md).
- **Two-factor authentication**: at sign-in, besides the password, you also enter a 6-digit code from an authenticator app.
- **Passkeys**: sign in with your device's biometrics or screen lock, without typing a password.
- **Claim code**: the one-time code needed the first time you set the admin password.
- **Session length / Member login lifetime**: how long after you sign in to the console you must sign in again; how long before a member must get a new login credential.
- **Signing keys**: the keys the server uses to issue login credentials and share links, under Settings › System update & backup. Removing an old key immediately invalidates the logins and links it signed. See [Protect your studio](security.md).
- **Machine credential lifetime**: how long each machine's warden login credential is valid. It is renewed automatically before it expires; a machine that misses the renewal has to be reinstalled.
- **Preferences**: the place in the avatar menu for adjusting theme, language, and layout.
- **Notification email**: the email address you must enter before turning on push notifications. See [Use it on your phone](mobile.md).
- **Theme**: the look of the console (colors, fonts, avatars, icons) plus optional wording overrides. Five are built in: Office · Morning light, Office · Dusk, Office · Night lamp, Office · Dark (no background), Office · Light (no background); a new studio switches among the first three by time of day by default. See [Themes (look and wording)](theme.md).
- **Switch by time of day**: changes the theme automatically at the times you set, based on your device's local time. See [Themes (look and wording)](theme.md).
- **Seasonal theme**: a theme applied automatically during holidays; some also switch to different versions by time of day. It can be turned off with "Show seasonal theme" in the theme settings.
- **Background animation**: whether the theme background plays an animation. The options are Auto, Play, and Still, saved only on this device; with "Auto", phones and tablets show a still image. See [Themes (look and wording)](theme.md).
- **General**: the system parameters in Settings (thresholds, size caps, intervals, number of backups kept, and so on). See [Settings](settings.md).
- **System update & backup**: the Settings page for checking for updates, upgrading, automatic updates, the update check interval, backup health, and signing keys. See [Settings](settings.md).
- **Receive beta versions**: also includes pre-release versions when checking for updates.
- **Backup health**: shows whether scheduled backups are working (Backups are healthy, Backups are failing, Cannot tell). See [Troubleshooting and FAQ](troubleshooting.md).

## System and operation

- **server (ocserverd)**: the main program that stores all data and decides which members should be online or stopped. It serves the console web page and only accepts connections from the same computer. See [Architecture and how it works](architecture.md).
- **warden**: a background program on each machine that starts or stops members as the server instructs and reports the machine's status.
- **ocagent**: the tool inside a member's session that keeps the connection to the server, sends and receives attachments, fetches secrets, and reports status.
- **officraft**: the program that starts the warden on macOS; it is the name shown in macOS permission prompts.
- **Machine**: one computer that runs members (a Mac, Linux, or WSL2 on Windows). A studio can have several machines, managed under Monitor › Machines.
- **Uninstall (machine)**: asks that machine's warden to remove itself. The machine goes offline, its record is kept, and it can be installed again.
- **Delete (machine)**: removes the machine from the list and immediately invalidates its credential. It does not remove the programs on the machine.
- **tmux**: a terminal tool that keeps programs running in the background. Each member runs in its own tmux session on its machine. "Terminal · TMUX" in the member details gives the command to attach to that session and see the member's screen directly.
- **SSE**: an always-open connection over which the server keeps sending new messages and updates to a member. Whether a member is online is decided by whether this connection is still up.
- **MCP**: a standard set of commands members use to ask the server to do things (send messages, open cards, create tasks, report progress).

## Related pages

- [Architecture and how it works](architecture.md)
- [Interface reference](interface.md)
- [Settings](settings.md)
- [Troubleshooting and FAQ](troubleshooting.md)
