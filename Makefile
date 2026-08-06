TEST_DATABASE_URL ?= postgres://pawfund_test:pawfund_test@localhost:5433/pawfund_test?sslmode=disable
TEST_CACHE_URL ?= redis://localhost:6380/0

.PHONY: test test-all test-db-up test-db-down test-deps-up test-deps-down test-integration

test:
	go test ./...

test-deps-up:
	docker compose --profile test up -d --wait postgres-test redis-test

test-deps-down:
	docker compose --profile test rm --stop --force postgres-test redis-test

test-db-up: test-deps-up

test-db-down: test-deps-down

test-integration: test-deps-up
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" TEST_CACHE_URL="$(TEST_CACHE_URL)" go test -tags=integration ./...

test-all: test test-integration
