-- +goose Up
CREATE TYPE campaign_status AS ENUM (
  'active',
  'completed'
);

CREATE TABLE IF NOT EXISTS campaigns (
  id UUID PRIMARY KEY,
  fundraiser_id UUID UNIQUE NOT NULL REFERENCES fundraisers(id),
  event_id UUID UNIQUE NOT NULL REFERENCES blockchain_events(id),
  title VARCHAR(255) NOT NULL,
  short_description VARCHAR(255) NOT NULL,
  story TEXT NOT NULL,
  goal_amount INTEGER NOT NULL,
  raised_amount INTEGER NOT NULL DEFAULT 0,
  donor_count INTEGER NOT NULL DEFAULT 0,
  contract_address VARCHAR(255) UNIQUE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ended_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  image_object_key TEXT NOT NULL,
  country VARCHAR(255) NOT NULL,
  zip_code INTEGER NOT NULL,
  status campaign_status NOT NULL DEFAULT 'active'
);

-- +goose Down
DROP TABLE IF EXISTS campaigns;
