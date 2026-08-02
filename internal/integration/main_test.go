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
)

var testDatabase *sql.DB

func TestMain(m *testing.M) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "TEST_DATABASE_URL is required for integration tests")
		os.Exit(1)
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open test database: %v\n", err)
		os.Exit(1)
	}
	if err := waitForDatabase(db, 15*time.Second); err != nil {
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "wait for test database: %v\n", err)
		os.Exit(1)
	}

	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "set migration dialect: %v\n", err)
		os.Exit(1)
	}
	if err := goose.Up(db, migrationsDirectory()); err != nil {
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "apply test migrations: %v\n", err)
		os.Exit(1)
	}

	testDatabase = db
	code := m.Run()
	if err := db.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close test database: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
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
