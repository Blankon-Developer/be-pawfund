TEST_DATABASE_URL ?= postgres://pawfund_test:pawfund_test@localhost:5433/pawfund_test?sslmode=disable

.PHONY: test test-all test-db-up test-db-down test-integration

test:
	go test ./...

test-db-up:
	docker compose --profile test up -d --wait postgres-test

test-db-down:
	docker compose --profile test rm --stop --force postgres-test

test-integration: test-db-up
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -tags=integration ./...

test-all: test test-integration
