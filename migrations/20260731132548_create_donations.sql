-- +goose Up
CREATE TABLE IF NOT EXISTS donations (
  id UUID PRIMARY KEY,
  campaign_id UUID NOT NULL REFERENCES campaigns(id),
  supporter_id UUID NOT NULL REFERENCES supporters(id),
  event_id UUID UNIQUE NOT NULL REFERENCES blockchain_events(id),
  amount INTEGER NOT NULL CHECK (amount > 0)
);

CREATE INDEX donations_campaign_id_idx ON donations(campaign_id);
CREATE INDEX donations_supporter_id_idx ON donations(supporter_id);

-- +goose Down
DROP TABLE IF EXISTS donations;
