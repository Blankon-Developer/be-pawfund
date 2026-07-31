-- +goose Up
CREATE TABLE IF NOT EXISTS donations (
  id UUID PRIMARY KEY,
  campaign_id UUID UNIQUE NOT NULL REFERENCES campaigns(id),
  supporter_id UUID UNIQUE NOT NULL REFERENCES supporters(id),
  event_id UUID UNIQUE NOT NULL REFERENCES blockchain_events(id),
  amount INTEGER NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS donations;
