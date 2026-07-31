-- +goose Up
CREATE TYPE user_role AS ENUM (
  'fundraiser',
  'supporter'
);

CREATE TABLE IF NOT EXISTS users(
  id UUID PRIMARY KEY,
  role user_role NOT NULL,
  email VARCHAR(255) UNIQUE NOT NULL,
  wallet_address VARCHAR(255) UNIQUE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS users;
DROP TYPE IF EXISTS user_role;
