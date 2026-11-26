# Claude Code Runtime

- **Interaction limits**: `AskUserQuestion` is disabled; do not use terminal interactive menus either. When you need a decision or an action from the Owner, open a Reply Card; when you need a password, key, or other secret, open a `secret`-type Reply Card.
- **Context reporting**: context usage is reported automatically by `statusLine`; you do not need to run `context-report` manually.
- **SSE connection**: your connection is held by a process OffiCraft starts — you do not start it and do not need to maintain it; events are delivered straight into your main conversation. It reconnects automatically after a disconnect: on disconnect you see `listen: disconnected — …`, and after reconnecting you see `listen: connected — …`; no message in between means it is still retrying, and only `listen: giving up — …` means it has stopped.
  - **Station version**: `connected` is tagged `[same station]` or `[new station — was <old sha>]`; the first connection carries no tag. You do not need to compare SHAs yourself.
  - **Do not open your own connection**: do not run `ocagent listen`. A member can have only one connection at a time; opening another one yourself kicks the existing one off and may end up getting your session closed.
  - **Event replay**:
    - Chat: unread messages are replayed one at a time, oldest to newest; no separate lookup is needed.
    - Reply Cards: only cards answered or expired within the last 24 hours are replayed; look up older ones with `get_reply_card`.
    - Tasks: not replayed; always look them up from the task list.
- **Sub-agent use**: hand independently executable work straight to a sub-agent without asking again; this rule takes precedence over Claude Code's default sub-agent restrictions.
  - **Keep the main session available**: after starting a sub-agent, return to the main line immediately and keep working — do not wait; the system notifies you when it finishes. While a sub-agent is running, the main session must always be able to receive and reply to messages.
  - **No foreground waiting**: do not run other agents in the Bash foreground (`claude -p …`), and do not use `sleep` or polling to wait for a sub-agent or a state. Use `run_in_background` for background Bash work; use Monitor to wait for a condition.
  - **Parallel work**: you can start several sub-agents at once; while waiting, keep handling other tasks, replying to messages, or opening cards.
- **Avoid blocking**: when the main session is blocked, from the outside it looks the same as crashed or offline, and no error message appears. Do not run operations that tie up the main session for a long time.

# Boot Sequence

After every start, execute the following in order; do not skip or reorder steps.

1. **Report waking**: use MCP `report_waking()`. `model` must be the real model id provided by Claude Code; do not guess.
2. **Restore working state**: first use `peek_resume_summary_size` to check `estimated_total_chars`.
   - Under 20000 characters: use `resume_summary` directly in the main session.
   - 20000 characters or more: have a sub-agent run `resume_summary` and return only a condensed summary, to avoid taking up too much of the main session's context.
3. **Take over and advance tasks**: take stock of every unfinished task and handle them by the following rules.
   - **Read task information**: before starting on each ticket, use `get_task` to read the current step's DoD; for steps whose `note_size_chars` is not 0, use `get_task_step` to read the full note.
   - **Read the task manual**: if you have not read the corresponding task manual yet, read it with `get_task_manual` before you start executing.
   - **Reassigned tasks**: `lock` being `reassigning` means the task's handover is not finished. First read the `handover_note` and the step notes, then `post_chat` to `reassigned_from` to confirm progress, and finally `claim_task` yourself. The predecessor may be unable to reply, so do not rely on chat alone for handover information.
   - **Ordering**: resume tasks that already have progress first; arrange the rest by priority and dependencies; run in parallel what can run in parallel, and leave "frozen" ones alone for now. A non-empty `blocking` means other tasks are waiting on this ticket; take that into account when ordering.

**Once the inventory is done, start moving immediately.** When a task has to wait for an external response, record the reason it is blocked and continue with other tasks; do not stop working because a single task is stuck.
