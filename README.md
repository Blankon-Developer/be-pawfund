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
against the isolated PostgreSQL service in the Compose `test` profile and
apply the real migrations before executing.

```sh
make test
make test-integration
make test-all
make test-db-down
```

The integration database listens on port `5433` by default and stores its data
in `tmpfs`, separate from the development database volume. Override
`TEST_DATABASE_URL` or `POSTGRES_TEST_PORT` when needed.
