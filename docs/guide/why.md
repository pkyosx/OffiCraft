# Why OffiCraft

After reading this page, you can decide whether OffiCraft suits you, and you will know which page to read next.

**Craft your own AI office.** OffiCraft is an AI studio that runs on your own computer. You bring AI members in to work: a staff member is responsible for one area over the long term, and an outsource member joins for one piece of work and leaves when it is done. You hand a whole piece of work to a member and follow the member's progress in the web console. When something needs your decision, the member opens a reply card and waits for your answer.

Behind each member is a Claude Code or Codex session on your computer. When a session's memory (context) is nearly full, OffiCraft has the member summarize what it needs to remember and continue in a new session. The member's tasks are not affected.

---

## OffiCraft suits you if…

- You have several terminals open, each running Claude Code, and you can no longer tell which one is doing what.
- You want to hand off a whole piece of work and only make the decisions yourself: the member breaks the work into steps and finishes it, and stops to ask you only when something needs your decision, instead of deciding first and telling you afterwards.
- You want the studio to run on its own: repeated work follows a task manual, and work at set times is given through scheduled messages.
- You want members to remember what you taught them once: your corrections and preferences are recorded as lore, which members read when they start work, including a different member who takes over.
- You have more than one computer and want each of them to work for the same studio.

## What OffiCraft is not

| | |
| --- | --- |
| **Not a framework for writing AI programs.** | OffiCraft does not teach you how to write AI programs. OffiCraft manages how a group of AI members divide the work, hand it over, and report back. |
| **Not a cloud service.** | There is no account to sign up for. The studio's data is stored on your computer. |
| **Not a team collaboration tool.** | A studio has only one owner: you. In the console you are shown as "CEO (You)". |
| **Not a chatbot.** | Members have roles, long-term memory, and tasks in progress. A member is more than a chat window. |

---

## A typical day

| | You | Members |
| --- | --- | --- |
| **Morning** | In chat, you say "Go through this month's customer feedback and write a report of the key points" | The member first opens a reply card asking you to confirm the task's scope and completion criteria. After you confirm, the member creates a task and breaks it into several steps, each stating what counts as done |
| **Noon** | You work on something else | When a step needs your decision on direction, the member opens a reply card, and that step stops and waits for you |
| **Evening** | You open the "Ask" tab, read the question and the options, and click one. When a member asks you to review the result, you look at it before replying | The member finishes the remaining steps based on your decision, attaches the result to the task card, and opens another reply card asking you to review it. After you approve it, the task is closed |

---

## What you get

OffiCraft's features fall into three groups: giving and tracking work, letting the studio run on its own, and managing the studio.

### Giving and tracking work

| Feature | What it does |
| --- | --- |
| **Members** | AI colleagues who work for you. A staff member is responsible for one area over the long term and has a role and memory that is kept across conversations. An outsource member joins for one piece of work and leaves when it is done. Members keep working after you close the browser. |
| **Chat** | Give work, @ a member, and upload screenshots and documents. In a document a member delivered, you can select text, write a comment, and send it back to the member to revise. A compare link shows what changed between versions. |
| **Tasks** | A piece of work broken into several steps, each with a definition of done. You can always see which step the task is on and what it is waiting for. After all steps are done, the responsible member wraps up and closes the task. |
| **Reply cards** | A card with options that sets out something you need to decide. It waits for your answer in the "Ask" tab. An option marked "AI pick" is the approach the member recommends. |
| **Member collaboration** | Each member knows who their colleagues are and what each is responsible for, and asks a colleague for help directly when needed, without going through you. |

### Letting the studio run on its own

| Feature | What it does |
| --- | --- |
| **Task manuals** | A fixed process written down as a manual. When you give a piece of work, the member finds the matching manual, follows its steps, and hands the work to the member or outsource member the manual names. |
| **Scheduled messages** | At set times, a message you wrote in advance is sent to a member, for example "Every morning, summarize yesterday's progress". |
| **Webhook endpoints** | Notifications sent by other services are passed to the responsible member, for example a new Slack message. The member decides what to do based on the endpoint's purpose, does not carry out requests outside that purpose, and opens a reply card to ask you when it cannot decide. |
| **Lore** | Records your corrections, your preferences, and the judgment members learn while working. A member who takes over, or a member whose model was changed, reads it when starting work. |

### Managing the studio

| Feature | What it does |
| --- | --- |
| **Multiple machines** | The console gives you a one-line install command. After you run it on another computer, that computer joins the same studio, and members can be assigned to work on it. |
| **Claude Code and Codex** | Members use the Claude Code or Codex on your computer, with your existing login and settings. Claude Code skills, plugins, and MCP all work as usual. |
| **Secrets** | Store keys, tokens, and other confidential values in the "Secrets" tab. Once stored, the original value is no longer shown on screen; members retrieve it when they need it. |
| **Monitoring** | Lists which machine each member is on, which model it uses, and its estimated cost, plus each account's usage in the 5-hour window and the 7-day window. |
| **Login protection** | The console requires a login. You can add two-factor authentication or use passkeys instead. |
| **Mobile layout** | You can add the console to your phone's home screen and receive push notifications, so you can answer reply cards and check progress on the go. |

---

## How OffiCraft works

```
You (CEO)
 └─ Member ── backed by a Claude Code or Codex session
      └─ Task ── broken into steps, each with a definition of done
           └─ Needs your decision ──> opens a reply card ──> you reply ──> work continues
```

| Term | What it is |
| --- | --- |
| **You (CEO)** | The owner of the studio. There is only one per studio. |
| **Member** | An AI colleague in the studio. There are staff members, who are responsible for one area over the long term, and outsource members, who join for one piece of work and leave when it is done. Behind each member is a Claude Code or Codex session; when its memory is nearly full, the member automatically continues in a new session. |
| **Role** | A staff member's position. Each role has its own role definition and Insight (judgement calls). When a different member takes over, this memory stays with the role. |
| **Task** | A piece of work to track until it is closed, broken into several steps, each with a definition of done. |
| **Reply card** | A card a member opens when it needs your decision. The member stops at that step and waits for your answer; it does not decide for you. |
| **Console** | The web page you use to run the studio. Its tabs are Office, Ask, Task, Secrets, Files, Lore, and Monitor. The gear icon in the top right opens Settings, and the User guide is also in Settings. |

Right after installation, the studio has only two people: you, and your Assistant, **Mira**.

To see which programs OffiCraft actually runs on your computer, see [Architecture and how it works](architecture.md).

---

## Limitations to know before you start

- **Members can operate your computer without asking for confirmation.** Members work in the background with no one present to click "Allow", so Claude Code and Codex are both set to skip permission prompts and run commands directly. As a result, members can do a lot on your computer. What limits members is the work rules they read and the rule that anything needing your decision requires a reply card, not the system's permission controls. **Accept this before you install.** For the actual settings, see "Member permission settings" in [Architecture and how it works](architecture.md).
- **Supported: Macs with Apple Silicon (M-series chips), Linux on x86_64 and arm64, and Windows through WSL2.** WSL2 is a built-in Windows feature that runs Linux inside Windows. Running directly on Windows is not supported. The installer refuses any other computer. See [Install, upgrade, and uninstall](install.md).
- **Every computer that members work on needs `tmux` and at least one of Claude Code or Codex.** `tmux` is a terminal tool that keeps programs running in the background. For version requirements, see [Install, upgrade, and uninstall](install.md).
- **Members use your own Claude or Codex account's usage.** Conversations are sent to Anthropic or OpenAI as usual.
- **A few features connect to outside services.** Checking for updates reads the release page on GitHub. When push notifications are on, notifications pass through the browser vendor's push service, and the notification email you enter is also given to that service.

---

## What to read next

The user guide is split into five groups. You do not need to read all of it; read what you need.

**Getting started**

Read these when you are new to OffiCraft, getting ready to install, or have just installed and want to try it out.

- [Install, upgrade, and uninstall](install.md) — Prerequisites, installing, upgrading, uninstalling, and adding or removing other machines.
- [Your first office](quickstart.md) — Ten minutes to set a password, hand off your first piece of work, and answer your first reply card.
- [Protecting your studio](security.md) — The password, two-factor authentication, and passkeys to set up before you connect from your phone or from outside.

**Daily use**

Read these for giving work, answering reply cards, and checking progress and usage every day.

- [Assigning work and answering reply cards](daily.md) — Chat, attachments, @ a member, grouping members, answering reply cards, and commenting on documents members deliver.
- [Tracking task progress](tasks.md) — Understanding a task's status, steps, and gates, reviewing results, and force-closing a task.
- [Managing AI accounts and usage](usage.md) — In the "Monitor" tab, see each account's usage, estimated cost, and the account and model each member uses; log a machine in to an account; upgrade Claude Code.
- [Using it on your phone](mobile.md) — Connect to the console from your phone, add it to your home screen, and turn on push notifications.

**Letting the studio run on its own**

Read these when you want to hand off repeated, timed, or externally triggered work, or want to add members.

- [Having AI follow an SOP for repeated work (Task manuals)](sop.md) — Write task manuals and decide which steps stop and wait for you.
- [Doing something on a schedule (Scheduled messages)](schedule.md) — Set up scheduled messages so that a member receives a message you wrote in advance at set times (the member must be online to handle it).
- [Sending outside events to members automatically (Webhook endpoints)](webhook.md) — Create webhook endpoints that pass notifications from other services (for example, new Slack messages) to the responsible member.
- [Making members remember what you taught them (Lore)](lore.md) — Write down who you are, what must not be touched (user-defined), each role's responsibilities, and what members have learned (lore).
- [Giving members account keys (Secrets)](secrets.md) — Store secrets so members can use them without your pasting them into chat.
- [Adding members and hiring outsource](members.md) — Add staff members, hire outsource members, wake and stop members, change models and machines, understand status icons, and decide whether to use outsource or add a permanent member.

**Lookup and reference**

Read these when you want to look up a field, a setting, or a term, or want to know how things work underneath.

- [Interface reference](interface.md) — A guide to every tab and field.
- [Settings and parameters](settings.md) — The six Settings subpages and the range of every parameter.
- [Themes (look and wording)](theme.md) — Change the look, switch by time of day, background animations, custom themes and importing/exporting them, and seasonal themes.
- [Architecture and how it works](architecture.md) — How the server, machines, and members divide the work.
- [Glossary](glossary.md) — Terms used in OffiCraft.

**When something goes wrong**

Read this when you run into a problem installing or using OffiCraft, or need to back up or restore the database yourself.

- [FAQ and troubleshooting](troubleshooting.md) — Failed installs, members that do not start, connection problems, upgrades that did not take effect, and backup and restore.
