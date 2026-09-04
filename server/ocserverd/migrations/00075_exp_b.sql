-- +goose Up
CREATE TABLE exp_b (id TEXT PRIMARY KEY);
-- +goose Down
DROP TABLE exp_b;
