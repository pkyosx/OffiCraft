-- +goose Up
-- T-ba6b cost banking — outsource-worker BANKED_COST, a DIRECT mirror of
-- member.banked_cost (migrations/00001): the persistent historical cumulative
-- cost (USD) folded in whenever the worker's live session ends — the SSE
-- last-disconnect edge and every kill+respawn funnel (refocus / 換 model /
-- relocate / stop / context-high auto-handover) all bank through the ONE
-- shared bankLiveCost helper the member path uses. Before this column a
-- worker's est.$ silently reset to zero on every handover (recon T-ba6b §6-1:
-- the cumulative outsource spend was invisible to the owner). Kept SEPARATE
-- from the live telemetry figure (never overlapping — the fold POPs the live
-- field exactly once per edge); the DTO serves live + banked side by side and
-- the panel sums them, exactly the member presentation. Additive column,
-- member-parity default, reversible.
ALTER TABLE outsource_worker ADD COLUMN banked_cost REAL NOT NULL DEFAULT 0.0;

-- +goose Down
ALTER TABLE outsource_worker DROP COLUMN banked_cost;
