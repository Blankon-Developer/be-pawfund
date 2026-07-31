-- +goose Up
CREATE TABLE IF NOT EXISTS supporters (
  id UUID PRIMARY KEY,
  user_id UUID UNIQUE NOT NULL REFERENCES users(id),
  name VARCHAR(255) NOT NULL,
  image_object_key TEXT
);

-- +goose Down
DROP TABLE IF EXISTS supporters;
