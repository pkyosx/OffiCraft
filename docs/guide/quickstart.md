# Your first office

After reading this page, you can go from opening the console for the first time to giving your first piece of work, answering your first reply card, and seeing the task through to being closed. The whole process takes about ten minutes.

Before you start, installation must be finished and the terminal must have printed a setup link. If you have not installed OffiCraft yet, see [Install, upgrade, and uninstall](install.md) first.

Right after installation, the studio has only two people: you, and your Assistant, **Mira**.

---

## Step 1: Open the console and set a sign-in password

1. On the computer where OffiCraft is installed, open the setup link printed at the end of installation in a browser. The link starts with `http://127.0.0.1:7755/?code=`. If OffiCraft is installed inside WSL2 on Windows, open it in a Windows browser.
2. The screen is "Set the admin password", and the claim code is already filled in. Enter a new password twice (at least 8 characters) and click "Get started".

After the password is set, the system automatically prepares this computer and wakes Mira. You do not need to do anything else.

To come back to the console later, open `http://localhost:7755` and sign in with the password you just set.

Two things to note:

- The setup link **can be used only once**. After the password is set, the link no longer works.
- The console **can be opened only on this computer**, and this cannot be changed. To use it from a phone or another computer, first see [Protect your studio](security.md), then set it up following [Use it on your phone](mobile.md).

If "Automatic setup did not finish" appears at the top of the console, one of the automatic preparation steps failed, and the banner shows the reason. For what to do, see [Troubleshooting and FAQ](troubleshooting.md).

---

## Step 2: Meet Mira

After you sign in, you see the "Office" tab. Mira is on the "Staff" tab of "Office members" on the left.

The dot at the bottom right of Mira's avatar shows whether Mira is online right now. The first time you come in, Mira is usually still starting up.

Move the mouse over the dot (on a phone, tap the dot) to see the status name. When the status changes to "Online", you can begin.

While Mira is online, the chat title bar also shows what Mira is doing now: Working, Waiting, or Idle.

Mira is your Assistant. Mira handles the day-to-day running of the studio and also takes on the work you give. For what each status of the dot means, see [Adding members and hiring outsource](members.md).

---

## Step 3: Send your first message

Click Mira, and a chat with Mira opens on the right. Try sending a message, for example:

> Hi, please introduce yourself, and tell me who is in this studio right now.

After you send it, Mira replies in the chat.

You and the members communicate only through chat in the console. You can attach screenshots, design files, and documents to chat messages, and members send back finished work as attachments too.

Members keep their replies as short as they can and cover only the main points. When you need details, ask the member to expand.

You can leave a message for Mira even while Mira is offline. Mira reads it after coming online.

For other ways to use chat, see [Assigning work and answering reply cards](daily.md).

---

## Step 4: Give your first piece of work

Give a piece of work that takes several steps. The example below needs no files prepared, so you can copy it into the chat and try it:

> Plan a three-day, two-night trip to Kyoto, Japan for me, written as an itinerary. For each day, list the sights, a restaurant for lunch and one for dinner, and how to get between the sights. Please create a task for this. Before you start planning the itinerary, ask me what kind of sights I want to see and roughly what my budget is.

Mira decides on its own whether a piece of work needs a task. Simple things may be answered directly in the chat. If you want it tracked as a task, say "create a task" directly, as in the example.

Under the default working rules, before creating a task Mira first opens a reply card listing the task name, the problem to solve, the completion criteria, and who will do it, and asks you to confirm:

1. Open the "Ask" tab, or view the reply card directly in the chat.
2. If the content is fine, choose the option that agrees. To change the scope or the completion criteria, type a reply.
3. After you confirm, Mira creates the task.

Open the "Task" tab to see the task. The task is broken into several steps, and each step states what must be done for it to count as complete. On the task card you can see:

- Which step the work is on now.
- The status of each step.
- The note for each step.

To understand each field on the task card, see [Tracking task progress](tasks.md).

---

## Step 5: Answer a reply card

When the work reaches a point where something can only be decided by you (for example, in the trip example, what kind of sights you want and what your budget is, or whether to go ahead and which direction to take), Mira does not decide for you. Instead, Mira opens a **reply card** and stops to wait for you. The status of that step changes to "Awaiting my reply".

The number on the "Ask" tab is the number of cards still waiting for your reply (cards you clicked "Later" on are not counted).

When you open a reply card, you see:

- A question, with the background and trade-offs you need to decide.
- Several options. The one marked "AI pick" is the approach Mira recommends. It is not necessarily listed first.
- If the options have numbers in front of them, it is a **single-choice card**: clicking an option sends it right away.
- If the options have checkboxes in front of them, it is a **multiple-choice card**: the "AI pick" options are already checked. You can add or remove checks, then send.
- You can also type a reply in "Type a reply…". On a single-choice card, text you have already typed is sent together with the option you click.

After you reply, Mira continues the work. Cards you have answered are in "Recently handled", where you can click "Change my decision" to change your answer.

For other ways to use reply cards, see [Assigning work and answering reply cards](daily.md).

---

## Step 6: Review and close the task

After all steps are done, the task status changes to "**Ready to close**". The task is not finished yet at this point:

1. Mira puts the results (reports, links, produced files) in the task card's artifacts, then opens a reply card asking you to review them.
2. You look over the results. If everything is fine, reply that the review is approved. If something needs changing, say so directly; Mira makes the changes and asks you to review again.
3. After the review is approved, Mira closes the task, the status changes to "**Done**", and the task moves to the "Closed" section.

A task is **not** closed automatically when time runs out. If the assigned member can no longer finish it, you can click the status label on the task card (for example "Ready to close") and choose "**Force close**" from the menu. **Force close cannot be undone.**

You have now gone through the whole process once:

```
Give a piece of work ─> Confirm the task content ─> Member breaks it into steps with completion criteria ─> Opens a reply card when you need to decide ─> You reply ─> All steps done (Ready to close) ─> Review and close (Done)
```

---

## What to do next

- First write your [User additions](lore.md): who you are, what the project is about, and what must not be touched. Every member reads it at startup.
- Write work that comes up again and again as a [task manual](sop.md).
- To use it from your phone, first see [Protect your studio](security.md), then set it up following [Use it on your phone](mobile.md).

---

## Related pages

- [Protect your studio](security.md)
- [Assigning work and answering reply cards](daily.md)
- [Tracking task progress](tasks.md)
- [Adding members and hiring outsource](members.md)
- [Troubleshooting and FAQ](troubleshooting.md)
