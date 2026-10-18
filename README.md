# Pawfund Backend

Backend for Pawfund: an HTTP API (`cmd/api`) and a separate campaign deployer
worker (`cmd/deployer`) that consumes deployment tasks from RabbitMQ. Both
processes read configuration from environment variables; export `.env` into
your shell before running them (the binaries do not load `.env` automatically).

API documentation is maintained in [docs/pawfund-api.json](docs/pawfund-api.json).

## Local configuration

Copy `.env.example` to `.env` and replace `JWT_SECRET` with at least 32 random
bytes. `STORAGE_ENDPOINT` must be an HTTP(S) origin reachable by the frontend,
while the storage public base URL must already include the public bucket name.
Integration test variables (`TEST_*`) are documented in `.env.example`.

```dotenv
HTTP_ADDR=:8080
DEPLOYER_HTTP_ADDR=:8081
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
QUEUE_URL=amqp://pawfund:pawfund@localhost:5672/
SIWE_DOMAIN=localhost:3000
SIWE_URI=http://localhost:3000/login
SIWE_CHAIN_ID=84532
DEPLOYMENT_MODE=stub
CHAIN_RPC_URL=https://sepolia.base.org
CAMPAIGN_FACTORY_ADDRESS=0x64bD5192cB735CB25C082Db33730691E428fb9AB
DEPLOYER_PRIVATE_KEY=
DEPLOYMENT_CONFIRM_TIMEOUT=2m
SIWE_MESSAGE_TTL=5m
JWT_TTL=24h
CORS_ALLOWED_ORIGINS=http://localhost:3000
```

Start development dependencies, load environment variables, apply migrations,
and run the API and deployer (use separate terminals for the two Go processes):

```sh
docker compose up -d postgres minio redis rabbitmq
set -a
. ./.env
set +a
go run github.com/pressly/goose/v3/cmd/goose@v3.27.3 -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/api
```

In another terminal, with the same `set -a` / `. ./.env` / `set +a` step:

```sh
go run ./cmd/deployer
```

Run the deployer when you want pending campaigns to be processed from the
queue. By default `DEPLOYMENT_MODE=stub` simulates factory deployment for
local development. Set `DEPLOYMENT_MODE=live` with RPC, factory address, and a
deployer private key to call the Pawfund factory `createCampaign(bytes16,address,uint256,uint256)`
on Base Sepolia (see `.env.example` for the default testnet factory address).

### Deployer configuration

The deployer uses [`config.LoadDeployer()`](internal/config/deployer.go) and
does not require API-only secrets (JWT, storage, SIWE domain, and so on).

| Variable | Required | Notes |
| --- | --- | --- |
| `DATABASE_URL` | yes | Same database as the API |
| `QUEUE_URL` | yes | RabbitMQ AMQP URL (matches Compose `rabbitmq` defaults) |
| `SIWE_CHAIN_ID` | yes | Must match the API so queued tasks pass chain validation |
| `DEPLOYER_HTTP_ADDR` | no | Defaults to `:8081` |
| `DEPLOYMENT_MODE` | no | `stub` (default) or `live` |
| `CHAIN_RPC_URL` | live only | EVM JSON-RPC endpoint for the target chain |
| `CAMPAIGN_FACTORY_ADDRESS` | live only | Deployed Pawfund campaign factory |
| `DEPLOYER_PRIVATE_KEY` | live only | Hot wallet that sends factory transactions (never commit) |
| `DEPLOYMENT_CONFIRM_TIMEOUT` | no | Defaults to `2m` while waiting for receipts |

Health check: `GET http://localhost:8081/health` (or the host and port from
`DEPLOYER_HTTP_ADDR`). Campaign deployment messages use exchange `pawfund.tasks`
with routing key and queue `campaign.deploy`.

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
against isolated PostgreSQL, Redis, MinIO, and RabbitMQ services in the Compose
`test` profile and apply the real migrations before executing.

```sh
make test
make test-integration
make test-all
make test-db-down
```

The integration database listens on port `5433` by default and stores its data
in `tmpfs`, separate from the development database volume. The integration
Redis instance listens on port `6380` with persistence disabled. RabbitMQ for
integration tests listens on AMQP port `5673` by default. Override
`TEST_DATABASE_URL`, `TEST_CACHE_URL`, `TEST_QUEUE_URL`, `POSTGRES_TEST_PORT`,
or `REDIS_TEST_PORT` when needed.
