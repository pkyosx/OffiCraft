Your end time is {deadline}.

<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->

# Accelerated Stop

Receiving these instructions means you have received an **accelerated stop order** and must now stop your current session before the specified end time. Past the end time you will be terminated outright, so complete the necessary handover first, then handle the rest of the wrap-up.

Before going offline, sort out the tasks in your hands and make sure all data that needs to be kept has been written back to the remote; after a restart, another machine may take over.

* Tasks in progress: push the git commits that need to be kept to the remote, and upload the artifacts that need to be kept to the task, so important data does not stay only on this machine.
* Tasks waiting on an external response: write back to the task who you are waiting on, the condition for resuming, and the first action after resuming; do not stay online waiting.

## 1. Start going offline

* Report stopping: call MCP `report_stopping()`.
* Record the handover state: use `post_chat` to send yourself a message recording the current state, in-flight work, blockers, next steps, and any unfinished sub-agent work.

## 2. End sub-agents

* Ask them to wrap up: tell every sub-agent you started to wrap up immediately, return its current results, where the evidence is, the remaining work, and the scope it did not check, then exit.
* Record results: as each sub-agent exits, immediately update its results into the corresponding task step, and post the same update with `post_chat`.
* Do not wait out timeouts: if a sub-agent cannot exit in time, do not keep waiting; write the progress obtained so far and the unfinished work into the corresponding task step, and continue with the rest of the necessary handover.

## 3. Wrap up

1. Clear temporary data: use `rm -r <full path>` to remove the temporary files or folders this work created; do not run it attached to a terminal (Codex's tty is set to false). You must write out the full path literally; do not use variables. If a path does not exist, cannot be deleted, or there is not enough time, record the path in your report and continue the wrap-up — do not stop at this step.
2. Write Lore: if this session produced experience worth keeping, write it as Lore with MCP `write_lore_entry`. Whether to write it and in what format follows the "Lore" rules in the boot instructions; if there is no experience worth keeping, write nothing.
3. Report stopped: call MCP `report_stopped()`, and handle it according to the `stop_effect` in the response:

   * `collected`: the stop procedure has started. If you are still alive afterwards or receive new work, do not call `report_stopped()` again; use `post_chat` to notify someone with the authority to change `desired_state`.
   * `already_reported`: a stop was already reported earlier; this call does not re-run the stop procedure. Act on the result of the first `report_stopped()`.
