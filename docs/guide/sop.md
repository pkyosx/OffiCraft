# Having AI follow an SOP for repeated work (Task manuals)

This page explains what a task manual contains and what each part means.

## Task manuals

A task manual records how to do one kind of work that comes up again and again. Once a manual exists, every task of that kind follows it, so you do not have to explain the work again each time.

When a member receives a request, the member compares it with the existing manuals to decide which kind of work it is. If no manual fits, the member still finishes the work, but plans it from scratch each time, and the task type on the task card shows "Ad-hoc".

You add and manage manuals in **Settings › Task manuals**. Each manual has:

- **Task definition**: the answers to three questions: "What is this task?", "What information is needed?", and "How is it done?".
- **Assigned executor**: who does this kind of task.
- **Task lore**: experience that members have built up while doing this kind of task.

## What is this task?

Members use this section to decide whether a request belongs to this kind of task: what work this kind of task covers, what result it must reach, and, where it is easy to confuse with another manual, where the boundary between them lies. **If this section is vague, members are more likely to put work in the wrong kind.**

## What information is needed?

The information that must be available before doing this kind of task. Each item is one field. A field can be set as:

- **Required**: this information must be present when the task is created.
- **🔑 Identity key**: used to prevent two tasks from being opened for the same piece of work. When the identity key values are the same and an older task of the same kind is not yet closed, the member continues the older task instead of opening another one.

**For the identity key, choose "what this work is", not "where this request came from this time".** For example, the identity key for writing up meeting minutes holds only the meeting name and date. If "which conversation thread it came from" is also set as an identity key, a second task is opened when the same meeting is brought up in a different thread.

## How is it done?

This section is the SOP (standard operating procedure): the standard steps for doing this kind of work. When a member plans the steps of each task, the member uses the SOP as the template. If the SOP states the following, the member follows them when planning:

- **Definition of done (DoD)**: what counts as finished for each step. Write it as a result that can be seen once the step is done, for example "The meeting minutes have been sent to all attendees", not "Handle the meeting minutes".
- **Gate**: a step that needs your decision or acceptance. When the member reaches that step, the member opens a reply card and waits for your reply before continuing. Gates suit decisions that have a cost and cannot be undone, for example whether to send something, whether to merge, or whether to spend money.
- **Limits**: what must not be done, and in which situations the member must ask you first.

When the SOP is saved, older versions are kept and can be restored. See "Version history" in [Settings and parameters](settings.md). When a member finds that the SOP itself should change, the member first opens a reply card that shows the original text and the changed text side by side, and **changes the SOP only after you agree**.

## Assigned executor

Only you and the Assistant, Mira, can set the "Assigned executor". There are two options:

- **Pick a member**: one staff member handles all tasks of this kind.
- **Outsource**: each task gets its own outsource member, who leaves once the task is closed. You also set the model, the machine, and how many outsource members can be hired at the same time.

**When you choose Outsource, you must choose a machine.** If no machine is chosen, or the chosen machine is offline, outsource members do not start working, and the work is not moved to another machine.

For the difference between outsource members and staff members, see [Adding members and hiring outsource](members.md).

## Task lore

Experience that a member learns while doing this kind of task, and that will still be useful later, is written down as task lore and attached to this manual. Every member who later does this kind of task, whether a staff member or an outsource member, reads it. When the SOP itself should change, the member opens a card to ask for your approval and does not use lore in place of that change. See [Making members remember what you taught them (Lore)](lore.md).

## Example: writing up meeting minutes and sending them to attendees

**What is this task?**

> After a meeting, someone provides a transcript of the meeting recording or notes, which need to be written up as meeting minutes and sent to the attendees. The goal is that each attendee, after reading them, knows what the meeting decided and what they need to do next, not just to forward the transcript.

**What information is needed?**

| Field | Setting |
| --- | --- |
| Meeting name | 🔑 Identity key (Required) |
| Meeting date | 🔑 Identity key (Required) |
| Transcript or notes | Required |
| Attendee list | Required |
| Which conversation thread it came from, if any | Optional |

**How is it done?**

```markdown
1. Read the transcript or notes, and confirm the meeting name, date, and attendee list.
2. Write up the meeting minutes: what was discussed, what was decided, who owns each action item, and when it is due.
3. Gate: send the full meeting minutes and the recipient list to the owner for confirmation, and send them only after approval.
4. Send the meeting minutes to all attendees. If the request came from a conversation thread, reply in that thread with the result of the sending.

Limits:
- Do not send to anyone who is not on the attendee list.
- If the transcript is unclear, or it is not clear who owns a decision, open a card to ask. Do not guess.
```

## Related pages

- [Tracking task progress](tasks.md)
- [Assigning work and answering reply cards](daily.md)
- [Making members remember what you taught them (Lore)](lore.md)
- [Adding members and hiring outsource](members.md)
- [Settings and parameters](settings.md)
