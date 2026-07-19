-- +goose Up
-- M3 §6.3 close-out report — the executor's "結束後續已處理完" mark. After a
-- task lands in a terminal status (done/terminated) the executor writes the
-- learnings back and cleans the task's scratch, then reports the follow-ups
-- DONE through MCP report_task_closeout. One REAL column is the whole record
-- (minimal persistence, the closed_ts pattern): 0.0 = not yet reported, a
-- timestamp = reported (idempotent — the first report stamps, repeats no-op).
-- The outsource-dismissal consequence (SPEC §6.3 step 2: the server releases
-- the worker's runtime once the close-out lands) reads this stamp from the
-- worker-lifecycle batch — deliberately NOT a column/table of its own here.
ALTER TABLE task ADD COLUMN closeout_ts REAL NOT NULL DEFAULT 0.0;

-- +goose Down
ALTER TABLE task DROP COLUMN closeout_ts;
