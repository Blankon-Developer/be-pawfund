-- +goose Up
CREATE TYPE campaign_status AS ENUM (
  'active',
  'completed',
  'cancelled'
);

CREATE TYPE campaign_deployment_status AS ENUM (
  'pending',
  'submitted',
  'deployed',
  'failed'
);

CREATE TABLE IF NOT EXISTS campaigns (
  id UUID PRIMARY KEY,
  fundraiser_id UUID NOT NULL REFERENCES fundraisers(id),
  event_id UUID UNIQUE REFERENCES blockchain_events(id),
  title VARCHAR(255) NOT NULL,
  short_description VARCHAR(255) NOT NULL,
  story TEXT NOT NULL,
  goal_amount BIGINT NOT NULL CHECK (goal_amount > 0),
  raised_amount BIGINT NOT NULL DEFAULT 0 CHECK (raised_amount >= 0),
  donor_count INTEGER NOT NULL DEFAULT 0 CHECK (donor_count >= 0),
  contract_address VARCHAR(255) UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  end_at TIMESTAMPTZ NOT NULL,
  image_object_key TEXT NOT NULL,
  country VARCHAR(255) NOT NULL,
  zip_code VARCHAR(20) NOT NULL,
  status campaign_status NOT NULL DEFAULT 'active',
  deployment_status campaign_deployment_status NOT NULL DEFAULT 'pending',
  idempotency_key VARCHAR(255) NOT NULL,
  UNIQUE (fundraiser_id, idempotency_key),
  CONSTRAINT campaigns_deployed_chain_data_check CHECK (
    deployment_status <> 'deployed'
    OR (event_id IS NOT NULL AND contract_address IS NOT NULL)
  )
);

CREATE INDEX campaigns_fundraiser_id_idx ON campaigns(fundraiser_id);

-- +goose Down
DROP TABLE IF EXISTS campaigns;
DROP TYPE IF EXISTS campaign_deployment_status;
DROP TYPE IF EXISTS campaign_status;
