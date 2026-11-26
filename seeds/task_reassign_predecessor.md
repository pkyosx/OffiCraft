[{task_no}] This task has been reassigned to a new assignee.

<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->

Receiving these instructions means the current task needs to be handed over to another executor. Stop moving it forward and complete the necessary wrap-up, so the new assignee can get the current results and the full context remotely:

* Preserve results: push the git commits that need to be kept to the remote; upload the files that need to be kept with `ocagent upload` and write the attachment ids into the step notes. Do not leave results that only this machine can reach.
* Write the handover: write the current progress, in-flight items, risks to watch, and next steps into the task's step notes; you can keep adding to them until the new assignee claims the task, after which they can no longer be written. Reply Cards opened before the reassignment have expired automatically; if anything is still waiting on an Owner decision or action, write it into the step notes, and the new assignee will open a new card after claiming. During the handover, do not open any more Reply Cards bound to this task.
* Handle sub-agents: if any sub-agent is running, ask it to wrap up and write its results back to the corresponding task step.

Once the above is done, the handover is complete. If the new assignee is online and reaches out, confirm any details then; otherwise there is no need to wait for or go looking for the new assignee.
