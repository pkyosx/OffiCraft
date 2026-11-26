[{task_no}] Every step of this task has been reported complete. It is now parked at ready to close (ready_for_done) and will not finish on its own. This is visit {visit_no} to ready to close.

<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->

All Steps are complete; the closeout process starts now. Once the Task reaches a terminal state, its Artifacts, Step notes, and the required records on the Task are frozen and can no longer be added to, so first complete the following wrap-up in order:

- Organize results and verification records: attach results that need acceptance or will be used to the Task with add_task_artifact; write verification results and other required records back to the corresponding Task or Step.
- Ask the Owner for acceptance: open a Reply Card with the results, the verification results, and any risks or open items the Owner needs to know about. While the Owner has not replied, has asked for changes, or there are still open items, do not continue the closeout.
- Handle Lore: after the Owner accepts, decide per the "Lore" section of the boot instructions whether there is experience worth keeping; write it only if there is, otherwise write nothing.
- Clean up temporary data: use rm -r <full path> to remove the temporary files and folders this run created; do not run it attached to a terminal (Codex's tty is set to false). Write the full path literally; do not use variables. If a path does not exist or cannot be deleted, record the path in your report and continue the wrap-up.
- Remove the external resources you opened.
- Check the cards you opened that are still waiting for the Owner's reply (look them up with list_reply_cards): if you still need an answer, do not close yet; mark the ones you no longer need as expired, then close.
- Complete the closeout: after confirming everything above is done, call MCP mark_task_done() to close the Task, and stop updating the Task.

If it is not yet appropriate to close, do not call mark_task_done(); leave the Task in the ready-to-close state. If a Step is added later, the Task returns to in progress.
