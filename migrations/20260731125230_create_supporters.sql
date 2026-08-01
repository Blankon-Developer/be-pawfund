-- +goose Up
CREATE TABLE IF NOT EXISTS supporters (
  id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  name VARCHAR(255) NOT NULL,
  image_object_key TEXT
);

-- +goose Down
DROP TABLE IF EXISTS supporters;
