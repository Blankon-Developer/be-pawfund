//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"
)

var (
	testDatabase    *sql.DB
	testCache       *redis.Client
	testDatabaseURL string
	testCacheURL    string
)

func TestMain(m *testing.M) {
	testDatabaseURL = os.Getenv("TEST_DATABASE_URL")
	if testDatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "TEST_DATABASE_URL is required for integration tests")
		os.Exit(1)
	}
	testCacheURL = os.Getenv("TEST_CACHE_URL")
	if testCacheURL == "" {
		fmt.Fprintln(os.Stderr, "TEST_CACHE_URL is required for integration tests")
		os.Exit(1)
	}

	db, err := sql.Open("pgx", testDatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open test database: %v\n", err)
		os.Exit(1)
	}
	if err := waitForDatabase(db, 15*time.Second); err != nil {
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "wait for test database: %v\n", err)
		os.Exit(1)
	}

	redisOptions, err := redis.ParseURL(testCacheURL)
	if err != nil {
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "parse test cache URL: %v\n", err)
		os.Exit(1)
	}
	redisClient := redis.NewClient(redisOptions)
	if err := waitForCache(redisClient, 15*time.Second); err != nil {
		_ = redisClient.Close()
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "wait for test cache: %v\n", err)
		os.Exit(1)
	}

	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		_ = redisClient.Close()
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "set migration dialect: %v\n", err)
		os.Exit(1)
	}
	if err := goose.Up(db, migrationsDirectory()); err != nil {
		_ = redisClient.Close()
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "apply test migrations: %v\n", err)
		os.Exit(1)
	}

	testDatabase = db
	testCache = redisClient
	if err := testCache.FlushDB(context.Background()).Err(); err != nil {
		_ = testCache.Close()
		_ = testDatabase.Close()
		fmt.Fprintf(os.Stderr, "clean test cache: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	if err := testCache.FlushDB(context.Background()).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "clean test cache: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	if err := testCache.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close test cache: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	if err := db.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close test database: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func waitForCache(client *redis.Client, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var lastError error
	for {
		if err := client.Ping(ctx).Err(); err == nil {
			return nil
		} else {
			lastError = err
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: last ping error: %v", ctx.Err(), lastError)
		case <-ticker.C:
		}
	}
}

func waitForDatabase(db *sql.DB, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var lastError error
	for {
		if err := db.PingContext(ctx); err == nil {
			return nil
		} else {
			lastError = err
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: last ping error: %v", ctx.Err(), lastError)
		case <-ticker.C:
		}
	}
}

func migrationsDirectory() string {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate integration test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations"))
}

func cleanDatabase(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := testDatabase.ExecContext(ctx, "TRUNCATE TABLE users CASCADE"); err != nil {
		t.Fatalf("clean test database: %v", err)
	}
}

func cleanCache(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := testCache.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("clean test cache: %v", err)
	}
}

func cleanTestState(t *testing.T) {
	t.Helper()
	cleanDatabase(t)
	cleanCache(t)
}
