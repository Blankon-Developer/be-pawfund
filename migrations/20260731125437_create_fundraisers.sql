-- +goose Up
CREATE TABLE IF NOT EXISTS fundraisers (
  id UUID PRIMARY KEY,
  user_id UUID UNIQUE NOT NULL REFERENCES users(id),
  name VARCHAR(255) NOT NULL,
  image_object_key TEXT,
  contact_name VARCHAR(255) NOT NULL,
  contact_phone VARCHAR(255) NOT NULL,
  social_url TEXT,
  country VARCHAR(255) NOT NULL,
  zip_code VARCHAR(20) NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS fundraisers;
