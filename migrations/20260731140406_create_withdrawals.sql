-- +goose Up
CREATE TABLE IF NOT EXISTS withdrawals (
  id UUID PRIMARY KEY,
  campaign_id UUID NOT NULL REFERENCES campaigns(id),
  event_id UUID UNIQUE NOT NULL REFERENCES blockchain_events(id),
  amount BIGINT NOT NULL CHECK (amount > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX withdrawals_campaign_id_idx ON withdrawals(campaign_id);

-- +goose Down
DROP TABLE IF EXISTS withdrawals;
