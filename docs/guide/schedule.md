# Doing something on a schedule (Scheduled messages)

This page explains what a scheduled message is and what each setting of a scheduled message means.

## Scheduled messages

A scheduled message is a schedule set on one member. When the time comes, the message you wrote is sent to that member exactly as written, as if you had sent it in chat, and the member does what the message says. Scheduled messages suit routine work done every day or every week, for example summarizing the previous day's customer emails every morning, or writing a summary of last week's progress every Monday.

Scheduled messages are set up in the "SCHEDULED MESSAGES" card in "Member details". Only you and the Assistant, Mira, can set them up. Members do not schedule messages for themselves.

## What each scheduled message has

- **Name**: For your own reference. Optional.
- **Message**: The message sent to the member when the time comes. Write it as an instruction the member can act on after reading it, for example "Go through the customer emails received yesterday and list the ones that have not been answered yet." If the procedure is long and the same every time, write it as a task manual first and have the message only say "Do today's email summary following the daily email summary manual". See [Having AI follow an SOP for repeated work (Task manuals)](sop.md).
- **Repeats**: Daily, Weekly, Monthly, or Custom. Custom lets you specify a combination of months, days, hours, and minutes, for example 9 AM on the first day of each quarter, or once every 20 minutes.
- **Timezone**: The message is sent according to local time in this time zone. New scheduled messages default to Taipei time (`Asia/Taipei`). If you are in a different time zone, change it to yours.
- **Enabled / Disabled**: A disabled scheduled message is no longer sent. All its settings are kept, and enabling it again resumes sending.
- **Last sent**: The time of the most recent send. Use it to check whether the scheduled message is being sent on time.

Deleting a scheduled message **cannot be undone**.

## Situations to watch for

- **The member must be online to handle the message when the time comes.** If the member is offline or stopped, the scheduled message does not wake the member. The message stays and the member reads it the next time it comes online.
- **Set routine work on staff members.** An outsource member exists for a single task only. After the task is closed and the outsource member leaves, scheduled messages under that outsource member are no longer sent.
- **Missed times are not each sent later.** For times missed while the system was down or the scheduled message was disabled, only the most recent missed send is made once things resume.
- **If you choose the 29th, 30th, or 31st for a monthly schedule, months without that day are skipped entirely.** The message is not sent at the end of the month instead.

For the full description of each field and the sending rules, see "Scheduled messages" in [Interface reference](interface.md).

## Related pages

- [Having AI follow an SOP for repeated work (Task manuals)](sop.md)
- [Sending outside events to members automatically (Webhook endpoints)](webhook.md)
- [Adding members and hiring outsource](members.md)
- [Interface reference](interface.md)
