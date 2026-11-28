# Settings

After reading this page, you will know what each settings subpage covers, the range and default of every field on the General and Advanced pages, and which settings are not on the settings page and where to find them.

## Opening settings

Settings opens from the gear in the top bar. The settings home page lists, from top to bottom: System update & backup, Global context, Roles, Task manuals, General, Advanced, Theme, and User guide. The last one, "User guide", opens these guide pages; the others are the settings subpages. The path at the top of a subpage links back up one level.

## Settings outside the settings page

- **Theme, language, layout** — Avatar menu › Preferences. "Theme" in Preferences opens the theme settings, where you can choose a theme, set switching by time of day, the background animation, and showing seasonal themes; adding, editing, importing, and exporting themes is in "Settings › Theme". Language also decides what language members use to reply to you and write reports: after you change it, members switch the next time they start, and members already working are not notified.
- **Notification email** — Avatar menu › Notification email. Push notifications can only be turned on after you fill in this field; this email is given to the push service run by the browser vendor and does not stay only on your server. Push is turned on with the bell button in the top bar, separately on each device.
- **Password, two-factor authentication, passkeys** — Avatar menu › Account security. See [Protect your studio](security.md).
- **Outsource hiring cap** — Office tab › Outsource tab › Recruit a member, then open "Outsource cap". It can be set from 0 to 20 or "Unlimited"; the default is 3. Setting it to 0 pauses outsource assignment.
- **Member-specific suggested replies** — "Suggested replies" in the chat header, one list per member, for both staff and outsource. The chat lists this member's sentences first, then "Chat suggestions" from General; identical sentences are shown only once. When the task an outsource member holds has a type, the suggested replies of that task manual come first. Reply cards this member opens also list these sentences, after the task manual's suggested replies and before "Reply-card suggestions".
- **Scheduled messages, webhook endpoints** — Member details. See [Doing something on a schedule (Scheduled messages)](schedule.md) and [Send outside events to members automatically (webhooks)](webhook.md).
- **Secrets** — The Secrets tab. See [Give members your account keys (secrets)](secrets.md).

## System update & backup

From top to bottom:

- **Current version** and **Update settings** (Receive beta versions, Automatic updates, Update check interval, Let the assistant tell me about new-version features) — For checking for updates, upgrading, and the purpose and default of each field, see "Advanced: upgrade settings" in [Install, upgrade, and uninstall](install.md).
- **Backup health** — Whether scheduled backups keep producing restore points: "Backups are healthy", "Backups are failing", "Cannot tell". The card lists the latest scheduled backup, how long the failure has lasted, the freshness window, and the server diagnostic. "Cannot tell" does not mean there is no problem. For what each status means and how backup and restore work, see "Backup and restore" in [Troubleshooting and FAQ](troubleshooting.md).
- **Signing keys** — The keys the server uses to issue login credentials and share links, at the bottom of this page. Login credentials for the console, members, and machines, and the links made by "Copy share link" (for files) and "Copy external link" (for comparisons), are all signed with a signing key. For more, see "Signing keys" in [Protect your studio](security.md).
  - The card lists how many keys there are now: the one marked "In use" issues new credentials; one marked "Old key (still valid)" no longer issues any, but credentials and links signed with it remain valid.
  - "Create a new key": the new key takes over signing immediately. Nobody who is logged in is logged out, and no restart is needed. After that, every online machine automatically switches to credentials signed with the new key; offline machines switch once they come online.
  - "Remove": there is no undo, and it takes effect immediately. Every login credential signed with this key stops working, and console tabs using one must log in again; every share link made with this key stops working, and people holding the links are not notified; machines that have not yet switched to the new key disconnect immediately.

## Global context

All the documents sent to members are on this page; after you save, members read the new content the next time.

The two cards at the top, **User Language** and then **User additions**, can be edited at any time, developer mode or not:

- **User Language** tells members which language to write to you in. Every member reads it when it starts, before User additions; its character limit is 37500. Until you edit it, it follows the display language: Chinese gives the Chinese default, English (or not set) gives the English default. Once you edit it, members read your text whatever the display language. If you sync it to default, or save text that matches either default, it follows the display language again.
- **User additions** has no character limit; every member reads it when it starts.

The other documents are locked while developer mode is off (turn it on in Settings › Advanced › Developer mode). While locked, the document rows carry no "editable" tag; you can open a document and read it in full, but there is no edit button. Once developer mode is on, every document can be edited.

The other documents come in three groups:

| Group | Document | When members read it | Character limit |
| --- | --- | --- | --- |
| BOOT | System interaction | When each member starts | 150000 |
| BOOT | Term glossary | When each member starts, right after User Language | — |
| BOOT | Boot steps (Claude Code) | Claude Code members follow it when they start | 37500 |
| BOOT | Boot steps (Codex CLI) | Codex members follow it when they start | 37500 |
| STOP | Stop | When a member is stopped | 37500 |
| STOP | Accelerated stop | When a member is asked to wrap up early, with a deadline | 37500 |
| TASK EVENTS | Task close-out | When a task is judged finished | 37500 |
| TASK EVENTS | Task reassignment · to the predecessor | When a task the member holds is reassigned to someone else | 37500 |
| TASK EVENTS | Task reassignment · to the successor | When taking over a task (the same document is used whether or not there is a predecessor) | 37500 |
| TASK EVENTS | The task blocking yours is released | When a task it depends on is released | 37500 |
| TASK EVENTS | Your task is ready to be closed | Each time a task becomes ready to close | 37500 |

- "Boot steps" is a single row in the list; open it to see one row per document, "Boot steps (Claude Code)" and "Boot steps (Codex CLI)", each opening its own page.
- "Term glossary" lists the English and Chinese names of OffiCraft features side by side, after one line telling members that a Chinese screen name from the table means the English term in the same row. Nobody can edit it, even with developer mode on; it changes only with a new release.
- The character limits are factory values, and these fields are not on the Advanced page. For a document with a limit, the card's title bar shows "current characters / limit".
- Some documents begin with a read-only section written by the program (for example the member's name or the deadline), marked "Read-only (written by the program, not editable)"; you can only change the paragraphs after the read-only section. Saving replaces the editable part with the contents of the editor.
- A document with default content is marked "In sync with default" or "Modified" on its card.
- You are asked to confirm before saving. Take extra care with the boot steps: a broken version keeps members from coming online the next time they start, with no error message at all. "Default content" in the version history can sync the boot steps back to the factory content.

**User additions** are the long-term instructions you write for all members. For how to write them, see [Making members remember what you taught them (Lore)](lore.md).

## Roles

One page per role, with two documents on it:

- **Duty** — What this role is responsible for and which rules it follows. After a change, the role's members read the new content the next time they start.
- **Insight** — The trade-offs and boundaries this role uses again and again, usually written by the role's own members. Every member can read every role's Insight; only that role's own members, you, and the Assistant can write it. The on-screen note says: "Judgement principles this role keeps for its own work. Only this role's members read them at boot; other members don't read them by default." The Assistant's Insight has default content; other roles' Insight starts empty.

Every member can look up Duty and Insight, but they are added automatically only to the startup content of that role's members.

The role list has "Add role", "Rename role", and "Delete", which you and the Assistant can both use. When a role is deleted:

- The role's staff members are marked as having left, and their chat history is kept read-only.
- The Duty and Insight are deleted permanently and cannot be recovered; the version history is deleted too.
- It cannot be deleted while a member is online; the screen shows "A member is online — cannot delete". It can be deleted after all of the role's members have stopped.
- The built-in assistant role cannot be deleted.

Adding a role means hiring one more staff member. See [Adding members and hiring outsource](members.md).

## Task manuals

The working manual for each kind of task, in two groups: "Built-in" and "Custom".

**How manuals and task plans divide the work**

| | Who writes it | Content | Where it lives |
| --- | --- | --- | --- |
| Task manual | You, or a member with your approval | The purpose of this kind of work, the information it needs, the SOP, the assigned executor | Settings › Task manuals, lasting across tasks |
| Task plan | The member responsible for the task | Which steps this particular task is actually split into, and each step's definition of done | On the task card, closed out with the task |

**The manual page**

- The manual page has three parts: the "Assigned executor" card, the "Task definition" in the "Task planning" area (what this task is, what information it needs, and how it is done), and "Suggested replies".
- "Suggested replies" are sentences specific to this kind of task, at most 20 sentences of at most 120 characters each, visible only to you. These sentences are listed first in three places: the reply box of a reply card that is linked to this kind of task or asks about one; the message box on the task card of this kind of task; and the chat with an outsource member holding this kind of task. Staff members' chats do not list them, because a staff member may be responsible for several kinds of tasks at once. Deleting the manual deletes these sentences; syncing a built-in manual to its default content keeps them.
- When you add a manual with "Add task manual", the system generates its internal ID automatically; you do not need to fill it in and cannot. A new manual starts empty.
- The display name can be changed directly in the title on the manual page; when it is left empty, the internal ID is shown.
- Members can change a manual's purpose, fields, and SOP; when a member wants to change the SOP, it first opens a reply card and waits for your approval.
- The SOP has a character limit, set by "Task manual SOP size cap" in "Advanced".

**Required information and identity keys**

- Each field can be switched to "Required" (otherwise it is optional), and can also be set as a "🔑 Identity key"; a field set as an identity key becomes required automatically.
- Identity keys are matched only within the same task type, and only against tasks that have not ended. Once the old task is done, terminated, or marked as a duplicate, the same identity key value coming in again opens a new task.
- Values must be exactly the same to count as the same matter. Leading and trailing spaces are ignored; letter case must match.
- When a match with an existing task is found, the member reuses that task instead of opening a new one.
- Manuals without an identity key, and ad-hoc tasks, are not deduplicated automatically.

**Assigned executor**

Only you and the Assistant Mira can set it; ordinary members cannot change it.

- **Pick a member**: choose one of the staff members.
- **Outsource**: each task hires one outsource member to carry it out, and the member is released after the task is closed. For outsource you also set:
  - **AI runtime**: Claude Code or Codex.
  - **Model**: can be left empty to use the default model.
  - **Effort**: Low, Medium, High, X-High, Max.
  - **Machine**: which machine the outsource member runs on. The list shows whether each machine is currently idle, busy, or offline. If no machine is chosen, or the machine is offline or does not have the chosen runtime installed, outsource members for this kind of task are never woken and are not moved to another machine; the reason is shown on the outsource member.
  - **Hire count**: how many outsource members this kind of task can hire at the same time at most. Pressing "Unlimited" (shown as ∞) means this kind of task has no cap of its own, but the studio-wide outsource cap still applies.
- **Clear setting**: clears the assigned executor setting.

**Built-in manuals and deleting manuals**

- OffiCraft has three built-in manuals: "Create / edit role", "Create / edit task manual", and "Allow remote connections". Built-in manuals can be changed but not deleted. "Default content" in the SOP version history can sync the whole manual (including the assigned executor) back to its default settings.
- The trash-can icon at the right of each row in the list (it shows "Delete" on hover) deletes a custom manual, and you must press "Delete" to confirm first; a built-in manual's trash can is grayed out and cannot be used.
- Deleting removes the manual's definition, SOP, and version history together, and cannot be undone. A type that still has unfinished tasks cannot be deleted; it can be deleted after those tasks have all ended.

For how to write a manual and choose identity keys and gates, see [Having AI follow an SOP for repeated work (Task manuals)](sop.md).

## Version history

Every Global context document, Duty, Insight, and task manual SOP has a version history:

- System interaction, User Language, Boot steps, Stop, Accelerated stop, and the 5 task event documents each keep the last 10 changes; other documents (User additions, Duty, Insight, SOPs, and so on) keep the last 3. This counts saves, not time: a few small edits in a row push out the older versions, and versions pushed out cannot be recovered.
- It opens from edit mode: after you press "Edit", the button row gains a "Version history" button.
- Each row in the list shows when that version was replaced and who changed it. Click a row to open that version, and switch between "Version content" and "Changes vs current"; the comparison uses what is stored on the server and does not include unsaved changes in the editor.
- "Restore this version" asks you to confirm first, then overwrites the current content; the current content is saved as a new version, so you can go back again after restoring.
- For documents with default content (for example User additions, the Assistant's Duty and Insight, and built-in task manuals), the last row in the list is "Default content". Open it to view the content and the comparison, then press "Sync to default". The default content of "User additions" is empty, so syncing clears it. When a built-in task manual syncs to its default content, the whole manual (name, purpose, identity keys, SOP, and assigned executor) goes back to its default settings.
- For task manuals, only the SOP keeps versions; changes to the purpose and identity keys keep none.
- Lore entry titles and bodies also have a version history, keeping the last 3 changes for each entry. It opens from the entry's edit box on the Lore tab; see "Lore" in [Interface reference](interface.md).
- When a whole role or custom manual is deleted, its version history is deleted too and cannot be restored.
- A version that is over the current character limit and not shorter than the current content is marked "Cannot restore" in the list, with the reason.

## General

Each field on the screen, from top to bottom:

| Parameter | Purpose | Range | Default |
| --- | --- | --- | --- |
| Reply-card suggestions | Sentences you can insert with one click when answering a reply card, listed after the task manual's sentences and those specific to the member who opened the card | At most 20 sentences, at most 120 characters each | — |
| Task-message suggestions | Sentences you can insert with one click when sending a message on a task, listed after the task manual's sentences | At most 20 sentences, at most 120 characters each | — |
| Lore-message suggestions | Sentences you can insert with one click in a lore entry's message box | At most 20 sentences, at most 120 characters each | — |
| Chat suggestions | Sentences shared by all member chats that you can insert with one click, listed after the member-specific sentences | At most 20 sentences, at most 120 characters each | — |
| Due-soon lead time | How long before its due time a task starts counting as "Due soon"; individual tasks can set their own on the task card | 1 minute–30 days | 1 day |

**Suggested replies**: the four lists are saved separately. The reply box of a reply card lists, in order: the "Suggested replies" of the task manual of the task the card belongs to, the sentences specific to the member who opened the card, and "Reply-card suggestions". The message box of a task card lists, in order: the "Suggested replies" of this task's task manual, and "Task-message suggestions". The chat with an outsource member lists, in order: the "Suggested replies" of the task manual of the task it holds, the sentences specific to this member, and "Chat suggestions". The message box of a lore card lists "Lore-message suggestions". Identical sentences are shown only once in each box. When there is no matching task, the task is ad-hoc, or a list has no sentences, that list is not shown. Clicking a sentence only puts it into the input box and does not send it for you; if the input box already has text, the sentence is added on a new line after it. A list can be left empty, in which case no suggestions are shown under the matching input box; when the chat or reply card list is empty, the member-specific sentences are still shown. A sentence over the character limit is rejected when saving, not shortened automatically.

## Advanced

Each field on the screen, from top to bottom:

| Parameter | Purpose | Range | Default |
| --- | --- | --- | --- |
| Session length | How long after you log in before you must enter your password again | 12 hours / 24 hours / 7 days / 30 days | 24 hours |
| Member login lifetime | How long before newly started staff and outsource members must get a new login credential; lowering it does not affect credentials already issued | 12 hours / 24 hours / 7 days / 30 days | 7 days |
| Claude first notice | When a Claude member's memory reaches this share, Stop is sent asking it to wrap up and hand over | 1–89%, must be below the final call | 40% |
| Claude final call | When memory reaches this share, the final call is sent and the handover happens automatically; after that the session is collected forcibly once the Accelerated stop deadline passes | 40–90% | 50% |
| Codex first notice | After which memory compaction a Codex member is sent Stop | 1–10 rounds, must be below the final round | 1 less than the final round |
| Codex final round | After this many memory compactions, the member refocuses automatically | 1–10 rounds | 3 rounds |
| Accelerated stop deadline | How many seconds a member has to wrap up after Accelerated stop is pressed or the Claude final call is reached; the member is told this moment verbatim | 10–3600 seconds | 120 seconds |
| Reassign handover timeout | After a reassignment, how long an outsource predecessor can go without updating the task before it is collected; every task update restarts the timer, and staff predecessors are not affected | 60–86400 seconds | 1800 seconds |
| Machine credential lifetime | How long a machine's login credential is valid; machines renew it automatically before it expires, and a machine that misses renewal must be reinstalled; lowering it does not affect credentials already issued | 86400–34560000 seconds (1–400 days) | 30 days |
| Login check interval | How often each machine checks again while Claude or Codex is signed in | 30–3600 seconds | 300 seconds |
| Logged-out recheck interval | How often each machine checks again while Claude or Codex is signed out or the check failed | 30–3600 seconds | 30 seconds |
| Disk usage interval | How often to measure the disk space OffiCraft uses on each machine; it only measures and deletes nothing | 600–86400 seconds | 3600 seconds |
| Monitoring refresh interval | When events arrive in a row, how often at most the Monitor page refreshes | 1–60 seconds | 5 seconds |
| Duty size cap | The most characters one role's Duty can have | 100–250000 | 2500 |
| Insight size cap | The most characters one role's Insight can have | 100–250000 | 37500 |
| Task manual SOP size cap | The most characters one task manual's SOP can have | 100–250000 | 37500 |
| Task step note size cap | The most characters one task step's note can have | 1000–100000 | 25000 |
| Secret card: time to retrieve | After you provide the content, how long the member has to retrieve it before the content is deleted and the card expires; affects only content provided afterwards | 1 minute–30 days | 24 hours |
| Secret card: kept after retrieval | After the member first retrieves it, how long the content is kept, during which the member can retrieve it again; 0 means it is deleted on retrieval; affects only later retrievals | 0 seconds–1 day | 10 minutes |
| Wake chat budget | The most characters of recent chat a member brings back when it wakes | 1000–13000 | 6000 |
| Member lore size cap | The most characters of lore each member reads when it starts; lore for "Everyone" and the member's own lore are merged into one list by pinned state and effective period, and both count toward this cap | 100–100000 | 25000 |
| Task manual lore size cap | The most characters of task lore attached when a task manual is read | 100–100000 | 25000 |
| Lore title size cap | The most characters a lore entry's title can have | 10–10000 | 200 |
| Lore body size cap | The most characters a lore entry's body can have | 10–10000 | 1250 |
| Backups kept | How many database backups are kept | 1–20 | 5 |
| Developer mode | While off, every document in Global context except User Language and User additions is locked and nobody can change it; once on, you can edit them, sync them back to their default content, or restore earlier versions. User Language and User additions are always editable | On / Off | Off |

**Handover thresholds**: when a member's memory reaches the threshold, it first wraps up and leaves a handover, then the server starts a new session to carry on. Lower thresholds mean more frequent handovers; higher ones let each round run longer. Claude members only look at the two percentage fields, and Codex members only look at the two memory-compaction fields; setting the wrong pair gives no warning at all. "Refocus" in member details makes the member hand over once immediately.

**Character limits**: lowering any character limit never truncates or deletes content that is already saved.

- Duty, Insight, SOPs: the limit is checked only on write. A document already over the limit can still be saved as long as the new version is shorter than the old one. When members read these three kinds of documents, they also see the current character count and the limit.
- Step notes: a note already over the new limit can still be read in full, but must be cut below the limit before it can be changed again. This field only governs step notes; chat messages and reassignment handover notes have their own fixed limits that do not follow this field.
- Member lore and task lore: when the limit is exceeded, each entry that does not fit is skipped as a whole (entries are never cut short), members cannot read it, and there is no error message; on the Lore tab those entries are collapsed to one line and tagged "Not read".
- Lore titles and bodies: a new entry over the limit is rejected as a whole. Entries already saved can still be read in full; a title or body already over the new limit can be saved when edited as long as it is shorter than before, and a field you did not change is not checked. Restoring an earlier version follows the same rule.

**Wake chat budget**: lowering it only makes members bring back fewer messages the next time they wake; it deletes no chat history, and members can read the rest themselves. Raising it lets members read more of the conversation when they wake, at the cost of spending those characters on every wake.

**Backups kept** is the only parameter where lowering the value deletes something:

- It counts backups, not days. How far back you can go depends on how many backups were actually made in that period.
- Daily backups and pre-upgrade backups each keep this many, so there can be up to twice as many files on disk.
- Backups over the count are permanently deleted at the next backup and cannot be recovered.

## Theme

"Settings › Theme" has two areas: "Appearance" chooses which theme to use, sets switching by time of day, the background animation (saved only on this device), turns seasonal themes on or off, and restores the default; "Theme library" lets you add, import, export, edit, and delete themes. "Theme" in Avatar menu › Preferences opens the same appearance settings. For what each appearance setting means, see [Themes (look and wording)](theme.md).

**Editor sections**

When you add a theme, the system makes a copy based on Office · Dark (no background) and opens the editor directly, with the name prefilled as "New theme". There can be at most 100 custom themes. Everything except colors can be left empty, in which case the built-in design is kept:

- **Colours** — Seventy-odd named colors, grouped by use: Brand & accent, Background & surface, Text, Status, Presence dots, Icons, Dependencies, Decoration, Other. Each one is labeled with a readable name; click it to pick a color with the color picker, and you can also adjust its opacity.
- **Wording** — A searchable list of interface wording; fill in the "replacement" for the line you want to change to override it, or leave it empty to keep the original.
- **Fonts** — "Body font" and "Title font" are each picked from a built-in list of safe fonts (including Chinese fonts such as Kaiti, Heiti, Songti, and rounded fonts); choose "Default (theme font)" to keep the original font.
- **Avatars** — Staff avatar, Outsource avatar, CEO avatar, Assistant avatar; one image can be uploaded for each (PNG, JPEG, WEBP, max 64 KB).
- **Studio logo** — The logo at the top left of the top bar (PNG, JPEG, WEBP, max 64 KB).
- **Navigation icons** — Icons for the seven tabs Office, Ask, Tasks, Secrets, Files, Lore, and Monitor; one image can be uploaded for each (PNG, JPEG, WEBP, max 64 KB). Older theme files may still carry a User guide icon; they import as usual, but that icon is not shown anywhere, and it is removed without notice when you press Save in the theme editor.
- **Outer canvas** — An image laid over the base color (PNG, JPEG, WEBP, max 1 MB), with three layouts:
  - **Tile** — Repeats to fill the frame on both sides of the content column.
  - **Sides** — Places one copy of the same image on each side, without mirroring.
  - **Cover** — One image fills the whole window. It is only visible when this theme sets the colors of the top bar, tab bar, and content area to semi-transparent (color values with transparency, such as `#RRGGBBAA` or `rgba`); the system does not adjust the contrast between the image and the text.
  - Tile and Sides are drawn only on both sides of the content column, so they are not visible on phones, in narrow windows, or in the wide layout.

Colors can only be concrete color values, fonts can only be chosen from the safe font list, and wording can only override existing interface wording; you cannot add arbitrary CSS (a web page's style code), font files, or code.

**Exporting and importing**

- Exporting a custom theme exports its full content: colors, wording, fonts, avatars, logo, navigation icons, and outer canvas are all in the file. Exporting a built-in theme exports that theme's full set of colors.
- There are three ways to import: paste the theme file's content into "Paste theme JSON here…"; "Choose .json file"; or paste the theme file's web address into "…or import from a link" and press "Fetch and import". Theme files with a background image are usually large, so pasting the content directly is inconvenient.
- The content is checked before import, and invalid color values, arbitrary CSS, and oversized content are blocked.
- When a theme file has wording codes that are not recognized, it is still imported, but the skipped codes are listed.
- When the same custom theme already exists, you are told "This custom theme already exists".
- A theme file whose id (each theme's identifier) is the same as a built-in seasonal theme cannot be imported.

**Other theme behavior**

- The login page uses the theme this browser used last.
- Pressing "Log out" resets this browser's theme to Office · Dark (no background), so the next person who logs in on this computer does not see the previous person's choice; when the login expires instead of you pressing Log out, the login page keeps the original theme.
- Each seasonal theme's start and end times come with the version and cannot be adjusted; they are calculated in your computer's local time. When two periods overlap, only one is shown at a time.
- A seasonal theme does not change the theme or time-of-day settings you chose, is not in the theme list, and cannot be chosen for long-term use.

## Limits on risk and cost

For when members should stop and open a reply card to ask you, and how to write those limits, see "Limits on risk and cost" in [Making members remember what you taught them (Lore)](lore.md); for how to read usage and estimated spending, see [Manage AI accounts and usage](usage.md).

## Related pages

- [Interface reference](interface.md)
- [Protect your studio](security.md)
- [Themes (look and wording)](theme.md)
- [Having AI follow an SOP for repeated work (Task manuals)](sop.md)
- [Adding members and hiring outsource](members.md)
