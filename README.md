# Pawfund Backend

Backend API for Pawfund.

## Local configuration

Copy `.env.example` to `.env` and replace `JWT_SECRET` with at least 32 random
bytes. The storage public base URL must already include the public bucket name.

```dotenv
HTTP_ADDR=:8080
DATABASE_URL=postgres://pawfund:pawfund@localhost:5432/pawfund?sslmode=disable
JWT_SECRET=replace-this-with-at-least-32-random-bytes
STORAGE_PUBLIC_BASE_URL=http://localhost:9000/pawfund
CACHE_URL=redis://localhost:6379/0
CACHE_KEY_PREFIX=pawfund
SIWE_DOMAIN=localhost:3000
SIWE_URI=http://localhost:3000/login
SIWE_CHAIN_ID=84532
SIWE_MESSAGE_TTL=5m
JWT_TTL=24h
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

`STORAGE_PUBLIC_BASE_URL` only builds public object URLs. Bucket creation,
uploads, and bucket access policies are managed separately.

## Sign in with Ethereum

SIWE domain and URI must match the frontend origin. The example configuration
uses Base Sepolia (`84532`). Message and verify responses use the common API
envelope shown elsewhere in this document.

Create a SIWE message with `POST /v1/auth/message`:

```json
{
  "address": "0x1234567890123456789012345678901234567890"
}
```

The success data contains the EIP-4361 message that must be signed exactly as
returned:

```json
{
  "message": "localhost:3000 wants you to sign in with your Ethereum account:\n..."
}
```

Send that same message and the resulting 65-byte Ethereum signature to
`POST /v1/auth/verify`:

```json
{
  "message": "localhost:3000 wants you to sign in with your Ethereum account:\n...",
  "signature": "0x..."
}
```

A verified wallet receives an access token. Wallets that have not registered a
profile can use this token for registration; their profile fields are `null`:

```json
{
  "accessToken": "eyJ...",
  "isNotRegistered": true,
  "address": "0x1234567890123456789012345678901234567890",
  "name": null,
  "role": null,
  "imageUrl": null
}
```

Registered roles are returned as `supporter` or `fundraiser`. A message is
short-lived and single-use; expired, unknown, invalid, or replayed messages
are rejected.

## Get authenticated profile

`GET /v1/auth/me` retrieves the registered profile associated with the wallet
address in the access token:

```http
Authorization: Bearer <access-token>
```

A registered wallet receives:

```json
{
  "status": "success",
  "code": "PROFILE_RETRIEVED",
  "message": "Profile retrieved successfully.",
  "data": {
    "address": "0x1234567890123456789012345678901234567890",
    "name": "Cat Lover",
    "role": "supporter",
    "imageUrl": "http://localhost:9000/pawfund/profiles/cat.png"
  },
  "errors": null
}
```

If the token is valid but its wallet has no registered supporter or fundraiser
profile, the endpoint returns HTTP `404` with code `PROFILE_NOT_FOUND`.

## Register supporter

`POST /v1/register/supporter` requires `Content-Type: application/json` and an
access token containing a non-empty `wallet_address` claim:

```http
Authorization: Bearer <access-token>
Content-Type: application/json
```

```json
{
  "name": "Cat Lover",
  "email": "cat@example.com",
  "imageObjectKey": "profiles/cat.png"
}
```

The authenticated wallet is read from request context after the middleware
verifies the token. A client-provided `walletAddress` is rejected.

Successful response:

```json
{
  "status": "success",
  "code": "SUPPORTER_REGISTERED",
  "message": "Supporter account created successfully.",
  "data": {
    "name": "Cat Lover",
    "email": "cat@example.com",
    "walletAddress": "0xabc",
    "imageUrl": "http://localhost:9000/pawfund/profiles/cat.png",
    "role": "supporter"
  },
  "errors": null
}
```

Validation failures use HTTP `422` and report every invalid field:

```json
{
  "status": "error",
  "code": "VALIDATION_ERROR",
  "message": "One or more fields are invalid.",
  "data": null,
  "errors": {
    "name": ["name is required!"],
    "email": ["email format is not valid!"]
  }
}
```

## Tests

Unit and integration tests use table-driven test cases. Integration tests run
against isolated PostgreSQL and Redis services in the Compose `test` profile
and apply the real migrations before executing.

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
