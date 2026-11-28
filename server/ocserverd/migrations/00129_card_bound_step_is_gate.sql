-- +goose Up
-- A step a reply card was ever bound to is an approval step for good; binding
-- now sets is_gate, so steps bound before that are marked here.
UPDATE task_step SET is_gate = 1 WHERE reply_card_id <> '' AND is_gate = 0;

-- +goose Down
-- Nothing to undo: an older binary already treated a card-bound step as an approval step.
SELECT 1;
