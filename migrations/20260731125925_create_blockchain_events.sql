-- +goose Up
CREATE TYPE blockchain_event_type AS ENUM (
  'campaign_created',
  'donation_created',
  'withdrawal'
);

CREATE TABLE IF NOT EXISTS blockchain_events (
  id UUID PRIMARY KEY,
  tx_hash varchar(255) NOT NULL,
  type blockchain_event_type NOT NULL,
  block_number INTEGER NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS blockchain_events;
DROP TYPE IF EXISTS blockchain_event_type;
