# Pawfund Backend

Backend API for Pawfund.

API documentation is maintained in [docs/pawfund-api.json](docs/pawfund-api.json).

## Local configuration

Copy `.env.example` to `.env` and replace `JWT_SECRET` with at least 32 random
bytes. `STORAGE_ENDPOINT` must be an HTTP(S) origin reachable by the frontend,
while the storage public base URL must already include the public bucket name.

```dotenv
HTTP_ADDR=:8080
DATABASE_URL=postgres://pawfund:pawfund@localhost:5432/pawfund?sslmode=disable
JWT_SECRET=replace-this-with-at-least-32-random-bytes
STORAGE_PUBLIC_BASE_URL=http://localhost:9000/pawfund
STORAGE_ENDPOINT=http://localhost:9000
STORAGE_ACCESS_KEY=minioadmin
STORAGE_SECRET_KEY=minioadmin
STORAGE_BUCKET=pawfund
STORAGE_REGION=us-east-1
STORAGE_PRESIGN_TTL=15m
CACHE_URL=redis://localhost:6379/0
CACHE_KEY_PREFIX=pawfund
SIWE_DOMAIN=localhost:3000
SIWE_URI=http://localhost:3000/login
SIWE_CHAIN_ID=84532
SIWE_MESSAGE_TTL=5m
JWT_TTL=24h
CORS_ALLOWED_ORIGINS=http://localhost:3000
```

Start the development dependencies, apply migrations, and run the API:

```sh
docker compose up -d postgres minio redis
set -a
. ./.env
set +a
go run github.com/pressly/goose/v3/cmd/goose@v3.27.3 -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/api
```

`STORAGE_REGION` defaults to `us-east-1`, and `STORAGE_PRESIGN_TTL` defaults to
`15m`. `CORS_ALLOWED_ORIGINS` is a comma-separated list of frontend origins
allowed to call the API from a browser, for example
`http://localhost:3000,https://app.example.com`. Leave it empty to deny
cross-origin browser requests. The application does not create the bucket.
Bucket creation, public-read policy, lifecycle rules, and CORS for the frontend
origin and `PUT` method are managed separately.

Presigned uploads land under `tmp/profiles/` or `tmp/campaigns/`. Successful
register, profile update, and campaign-create requests copy that object to
`profiles/` or `campaigns/`, persist the canonical key, then delete the
staging object. Failed commits leave the file in `tmp/` so the client can
retry; production Cloudflare R2 expires leftover `tmp/` objects after 7 days.
Public GET access should be limited to the committed prefixes, not `tmp/`.

## Tests

Unit and integration tests use table-driven test cases. Integration tests run
against isolated PostgreSQL, Redis, and MinIO services in the Compose `test`
profile and apply the real migrations before executing.

```sh
make test
make test-integration
make test-all
make test-db-down
```

The integration database listens on port `5433` by default and stores its data
in `tmpfs`, separate from the development database volume. The integration
Redis instance listens on port `6380` with persistence disabled. Override
`TEST_DATABASE_URL`, `TEST_CACHE_URL`, `POSTGRES_TEST_PORT`, or
`REDIS_TEST_PORT` when needed.
