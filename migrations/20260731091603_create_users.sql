-- +goose Up
CREATE TYPE user_role AS ENUM (
  'fundraiser',
  'supporter'
);

CREATE TABLE IF NOT EXISTS users(
  id UUID PRIMARY KEY,
  role user_role NOT NULL,
  email VARCHAR(255) NOT NULL,
  wallet_address VARCHAR(255) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX users_email_key ON users (email) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX users_wallet_address_key ON users (wallet_address) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS users;
DROP TYPE IF EXISTS user_role;
