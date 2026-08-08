# Repository Guidelines

## Project Structure & Module Organization

This Go backend starts at `cmd/api/main.go`. Application code lives under `internal/`: `api` contains HTTP handlers and DTOs, `routes` maps Chi routes, `service` implements business rules, `repository` handles PostgreSQL, and `domain` defines core types. Supporting packages cover auth, caching, configuration, middleware, response helpers, and storage URLs. Timestamped Goose migrations live in `migrations/`. Unit tests sit beside their packages; broader tests live in `internal/integration/`.

Keep dependencies flowing from transport to service to repository/domain. Wire new components in `internal/app/app.go` and register endpoints in `internal/routes/routes.go`.

## Build, Test, and Development Commands

- `docker compose up -d postgres minio redis` starts local dependencies.
- `go run github.com/pressly/goose/v3/cmd/goose@v3.27.3 -dir migrations postgres "$DATABASE_URL" up` applies migrations.
- `go run ./cmd/api` starts the API using environment variables from `.env`.
- `make test` runs all non-integration Go tests.
- `make test-integration` starts isolated PostgreSQL/Redis services and runs `integration`-tagged tests.
- `make test-all` runs both suites; `make test-db-down` removes test services.
- `go fmt ./...` formats Go sources; `golangci-lint run` applies the checked-in lint configuration.

## Coding Style & Naming Conventions

Use `gofmt` formatting (tabs for Go indentation) and idiomatic import grouping. Package names are short and lowercase; exported identifiers use `PascalCase`, locals use `camelCase`, and filenames use lowercase snake case, such as `supporter_postgres.go`. Accept `context.Context` at I/O boundaries, wrap errors with operation context and `%w`, and keep HTTP envelopes in `internal/httpx`.

## Testing Guidelines

Use Go's `testing` package, `*_test.go` filenames, and `TestXxx` functions. Prefer table-driven cases with descriptive `t.Run` names. Add unit tests beside changed logic and integration coverage for database, Redis, migration, or endpoint behavior. No coverage threshold is enforced; cover success, validation, and failure paths. Integration tests must retain `//go:build integration` and clean shared state between cases.

## Commit & Pull Request Guidelines

Recent history follows Conventional Commits: `feat(auth): ...`, `fix(routes): ...`, `test(integration): ...`, and `chore(compose): ...`. Keep commits focused and use an accurate scope. Pull requests should explain behavior and architectural impact, link relevant issues, list verification commands, and call out new migrations, endpoints, or environment variables. Include example request/response payloads for API changes; screenshots are generally unnecessary.

## Security & Configuration

Copy `.env.example` to `.env`, never commit secrets, and use a random `JWT_SECRET` of at least 32 bytes. Keep SIWE domain, URI, and chain ID aligned with the frontend environment. Treat migrations as append-only once shared.
