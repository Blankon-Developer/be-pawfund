-- +goose Up
CREATE TYPE campaign_status AS ENUM (
  'active',
  'completed',
  'cancelled'
);

CREATE TABLE IF NOT EXISTS campaigns (
  id UUID PRIMARY KEY,
  fundraiser_id UUID NOT NULL REFERENCES fundraisers(id),
  event_id UUID UNIQUE NOT NULL REFERENCES blockchain_events(id),
  title VARCHAR(255) NOT NULL,
  short_description VARCHAR(255) NOT NULL,
  story TEXT NOT NULL,
  goal_amount INTEGER NOT NULL CHECK (goal_amount > 0),
  raised_amount INTEGER NOT NULL DEFAULT 0 CHECK (raised_amount >= 0),
  donor_count INTEGER NOT NULL DEFAULT 0 CHECK (donor_count >= 0),
  contract_address VARCHAR(255) UNIQUE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ended_at TIMESTAMPTZ,
  image_object_key TEXT NOT NULL,
  country VARCHAR(255) NOT NULL,
  zip_code VARCHAR(20) NOT NULL,
  status campaign_status NOT NULL DEFAULT 'active'
);

CREATE INDEX campaigns_fundraiser_id_idx ON campaigns(fundraiser_id);

-- +goose Down
DROP TABLE IF EXISTS campaigns;
