[{blocked_task_no}] The prerequisite task that was blocking this task has finished; it no longer blocks you.

<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->

- **Not started yet**: read it with get_task, plan the steps with submit_plan, then start executing.
- **Already in progress**: keep moving it forward; there is no need to re-plan.
- **Priority is frozen**: first find out why it was frozen, and wait until someone who can unfreeze it does so before you act.
