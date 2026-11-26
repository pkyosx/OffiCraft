# Adding members and hiring outsource

This page explains the difference between staff members and outsource members, the settings and actions in the member details, what each member status icon means, and how outsource members are brought in to work.

## Staff and Outsource

"Office members" on the left side of the Office tab has two tabs: "Staff" and "Outsource". When OffiCraft is first installed, the studio has only you and the Assistant, Mira.

| | Staff | Outsource |
| --- | --- | --- |
| Identity | Has a name and a role, for example the Assistant Mira, who is there from the start | Has no role and is referred to by a code name, for example "Outsource · O-7" |
| How long it exists | Permanent; stays in the studio | Exists for one task only and leaves after the task is closed |
| How it is created | You add a role | When a task manual or a reassignment specifies an outsource member, or when a staff member contracts work out to one, the system finds an outsource member automatically |
| What it remembers | The User additions you wrote for all members, the description of its own role, and the experience it has built up | The User additions you wrote for all members, plus the task manual and the experience built up for this kind of task |
| What it can do | Can create tasks and can contract work out to outsource members | Only does the one task given to it; cannot create tasks and cannot contract work out further |

Staff members and outsource members both work in the background. They do not stop when you close the browser.

**Adding a staff member or using an outsource member**: a staff member is responsible for a long-term area of work, for example handling customer support email. When other members come across something in that area, they go to that staff member. An outsource member is responsible only for what one task clearly defines. Work that comes up again and again and is done the same way each time is usually written as a task manual and given to outsource members. The experience an outsource member leaves behind is written as task lore, which the next member who does the same kind of task can read.

## Roles and staff members

Each staff member corresponds to one role. To add a staff member, you add a role in **Settings › Role journal** and write its role definition: what this role is responsible for and what it must not do. See [Making members remember what you taught them (Lore)](lore.md). "Recruit a member" on the "Staff" tab of the Office tab takes you to the same place. A new member is offline when it is created and starts working only after it is woken. The role name and the member's display name can both be changed later.

**Deleting a role**: the staff member of that role is marked as having left, and its chat history is kept as read-only. **The role definition and its Insight are deleted permanently and cannot be recovered.** A role cannot be deleted while its member is online; stop the member first.

Both you and the Assistant can add, rename, and delete roles.

## Member details

In "Office", click a member's avatar to open the member details.

**The four settings used when waking a member**:

- **AI runtime**: whether to use Claude Code or Codex.
- **Model**: a stronger model does better work and also costs more.
- **Effort**: the higher it is, the longer the member thinks, and the more it costs.
- **Machine**: when you have several machines, which one the member runs on.

**Actions**:

- **Wake**: makes an offline member start working. While the member is working, this button becomes "Change". After you change the settings, the member first wraps up what it is doing and then continues with the new settings. It is not interrupted halfway through.
- **Stop**: the member first wraps up what it is doing and then stops. A normal wrap-up has no time limit. To stop sooner, use "Accelerated stop" (gives the wrap-up a deadline) or "Force stop" (ends immediately). **Force stop skips the wrap-up, and work in progress that has not been saved is lost.** Use it only when there is no other choice.
- **Refocus**: makes an online member write its current progress as a handover, end its current session, and start a new session that continues from the handover. An AI can keep only a limited amount in mind within one session, and the more it holds, the more likely its responses are to get worse. Starting a new session does not affect the tasks the member is working on. The system also does this automatically when a member's memory is almost full.
- **Avatar**: the avatar can be changed in both the member details and the outsource details. Only you can change it.

When waking or stopping does not succeed, the reason is shown under "Last operation" in the member details. For what to do, see [FAQ and troubleshooting](troubleshooting.md). For a description of each setting, see "Office" in [Interface reference](interface.md).

## Reading a member's status

The **dot** at the bottom right of a member's avatar shows whether the member is online:

| Status | Meaning |
| --- | --- |
| Offline | Not running |
| Waking | Starting up |
| Online | Ready to chat |
| Stopping | Wrapping up |
| Stopped | Has stopped |

The **icon** in front of a member's name shows what the member is doing now:

| Icon | Status |
| --- | --- |
| Star | Working |
| Hourglass | Waiting, for example for a command running in the background or for a schedule |
| Coffee cup | Idle |
| Question mark in a circle | Status unknown: the member is online but has not reported its status yet |
| Moon | Offline or stopped |

An **exclamation mark** next to the name means this member cannot use the AI model normally right now. Click the exclamation mark to see the reason:

- **signed out of Claude** / **signed out of Codex**, **sign-in expired**: go to the machine information on the "Monitor" tab and sign that machine in to Claude or Codex again.
- **usage limit reached**: the account's usage has run out. It recovers when the reset time comes.
- **server error**, **model call failed**: an error on the side of the AI model service.

While a member is offline, you can still leave a message in the chat box. The member reads it after coming online. For how the status is determined, see "How a member's status is determined" in [Architecture and how it works](architecture.md).

## Outsource

You do not need to find outsource members one by one. In the three cases below, the system finds an outsource member automatically:

- The "Assigned executor" of a task manual is set to Outsource. The manual also sets the AI runtime, model, and machine the outsource members use, and the "Hire count": the maximum number of outsource members working on this kind of task at the same time. Only you and the Assistant can set a manual's assigned executor. See [Having AI follow an SOP for repeated work (Task manuals)](sop.md).
- You choose "Reassign…" next to "Assignee" on a task card to hand a task to an outsource member. See [Tracking task progress](tasks.md).
- A staff member contracts work out to an outsource member, whether or not that work has a task manual.

**"Machine" must be chosen.** An outsource member works only on the chosen machine. If no machine is chosen, or the chosen machine is offline or does not have the chosen AI runtime installed, the outsource member does not start at all and does not move to another machine. The reason is shown on that outsource member.

**The outsource cap for the whole studio** is set in "Recruit a member" on the "Outsource" tab of the Office tab. The hire count in a task manual is also limited by this cap:

- **Max hires N**: at most N outsource members work in the whole studio at the same time. When the cap is reached, tasks wait in a queue, and the next one is picked up automatically when a place opens.
- **Unlimited**: no cap.
- **0**: pauses assignment. Outsource members already working finish their tasks, but no new outsource members are brought in.

Outsource members also use your Claude or Codex account, and their usage counts against your account. **The outsource cap is the main way to control how much is used at the same time.**

After a task is closed, its outsource member leaves the list. The outsource member's chat history is kept and can be viewed as read-only.

## Related pages

- [Having AI follow an SOP for repeated work (Task manuals)](sop.md)
- [Making members remember what you taught them (Lore)](lore.md)
- [Doing something on a schedule (Scheduled messages)](schedule.md)
- [Sending outside events to members automatically (Webhook endpoints)](webhook.md)
- [Assigning work and answering reply cards](daily.md)
- [Interface reference](interface.md)
- [FAQ and troubleshooting](troubleshooting.md)
