# Stop

Receiving these instructions means you have received a **regular stop order** and must now stop your current session. This kind of stop has no wrap-up countdown, so fully complete the handover and wrap-up first, then end the session.

Before going offline, sort out the tasks in your hands and make sure all data that needs to be kept has been written back to the remote; after a restart, another machine may take over.

* Tasks in progress: push the git commits that need to be kept to the remote, and upload the artifacts that need to be kept to the task, so important data does not stay only on this machine.
* Tasks waiting on an external response: write back to the task who you are waiting on, the condition for resuming, and the first action after resuming, then handle the other tasks that need wrapping up. If there are no other tasks to handle, you may go offline; do not stay online waiting.

## 1. Start going offline

* Report stopping: call MCP `report_stopping()`.
* Record the handover state: use `post_chat` to send yourself a message recording the current state, in-flight work, blockers, and next steps.

## 2. End sub-agents

* Wait for completion: wait for every sub-agent you started to finish; do not start any new sub-agent while going offline.
* Record results: as each sub-agent finishes, immediately update its results into the corresponding task step, and post the same update with `post_chat`.

## 3. Wrap up

1. Clear temporary data: use `rm -r <full path>` to remove the temporary files or folders this work created; do not run it attached to a terminal (Codex's tty is set to false). You must write out the full path literally; do not use variables. If a path does not exist or cannot be deleted, record the path in your report and continue the wrap-up — do not stop at this step.
2. Write Lore: if this session produced experience worth keeping, write it as Lore with MCP `write_lore_entry`. Whether to write it and in what format follows the "Lore" rules in the boot instructions; if there is no experience worth keeping, write nothing.
3. Report stopped: call MCP `report_stopped()`, and handle it according to the `stop_effect` in the response:

   * `collected`: the stop procedure has started. If you are still alive afterwards or receive new work, do not call `report_stopped()` again; use `post_chat` to notify someone with the authority to change `desired_state`.
   * `already_reported`: a stop was already reported earlier; this call does not re-run the stop procedure. Act on the result of the first `report_stopped()`.
