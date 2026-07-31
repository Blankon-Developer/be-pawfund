-- +goose Up
CREATE TABLE IF NOT EXISTS withdrawals (
  id UUID PRIMARY KEY,
  campaign_id UUID NOT NULL REFERENCES withdrawals(id),
  event_id UUID NOT NULL REFERENCES blockchain_events(id),
  amount INTEGER NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS withdrawals;
