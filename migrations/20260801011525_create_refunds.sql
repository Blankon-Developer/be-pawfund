-- +goose Up
CREATE TABLE IF NOT EXISTS refunds (
  id UUID PRIMARY KEY,
  donation_id UUID UNIQUE NOT NULL REFERENCES donations(id),
  event_id UUID UNIQUE NOT NULL REFERENCES blockchain_events(id),
  amount BIGINT NOT NULL CHECK (amount > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS refunds;
