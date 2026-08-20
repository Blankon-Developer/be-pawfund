//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/pressly/goose/v3"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
)

var (
	testDatabase         *sql.DB
	testCache            *redis.Client
	testQueue            *amqp.Connection
	testDatabaseURL      string
	testCacheURL         string
	testQueueURL         string
	testStorageEndpoint  string
	testStorageAccessKey string
	testStorageSecretKey string
	testStorageBucket    string
	testStorageRegion    string
	testStorageClient    *minio.Client
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
	testQueueURL = os.Getenv("TEST_QUEUE_URL")
	if testQueueURL == "" {
		fmt.Fprintln(os.Stderr, "TEST_QUEUE_URL is required for integration tests")
		os.Exit(1)
	}
	testStorageEndpoint = os.Getenv("TEST_STORAGE_ENDPOINT")
	testStorageAccessKey = os.Getenv("TEST_STORAGE_ACCESS_KEY")
	testStorageSecretKey = os.Getenv("TEST_STORAGE_SECRET_KEY")
	testStorageBucket = os.Getenv("TEST_STORAGE_BUCKET")
	testStorageRegion = os.Getenv("TEST_STORAGE_REGION")
	if testStorageEndpoint == "" || testStorageAccessKey == "" || testStorageSecretKey == "" || testStorageBucket == "" || testStorageRegion == "" {
		fmt.Fprintln(os.Stderr, "TEST_STORAGE_ENDPOINT, TEST_STORAGE_ACCESS_KEY, TEST_STORAGE_SECRET_KEY, TEST_STORAGE_BUCKET, and TEST_STORAGE_REGION are required for integration tests")
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

	queueConn, err := waitForQueue(testQueueURL, 30*time.Second)
	if err != nil {
		_ = redisClient.Close()
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "wait for test queue: %v\n", err)
		os.Exit(1)
	}

	storageEndpointURL, err := url.Parse(testStorageEndpoint)
	if err != nil || storageEndpointURL.Host == "" {
		_ = queueConn.Close()
		_ = redisClient.Close()
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "parse test storage endpoint: %v\n", err)
		os.Exit(1)
	}
	storageClient, err := minio.New(storageEndpointURL.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(testStorageAccessKey, testStorageSecretKey, ""),
		Secure: storageEndpointURL.Scheme == "https",
		Region: testStorageRegion,
	})
	if err != nil {
		_ = queueConn.Close()
		_ = redisClient.Close()
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "create test storage client: %v\n", err)
		os.Exit(1)
	}
	if err := waitForStorage(storageClient, 15*time.Second); err != nil {
		_ = queueConn.Close()
		_ = redisClient.Close()
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "wait for test storage: %v\n", err)
		os.Exit(1)
	}
	exists, err := storageClient.BucketExists(context.Background(), testStorageBucket)
	if err != nil {
		_ = queueConn.Close()
		_ = redisClient.Close()
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "check test storage bucket: %v\n", err)
		os.Exit(1)
	}
	if !exists {
		if err := storageClient.MakeBucket(
			context.Background(),
			testStorageBucket,
			minio.MakeBucketOptions{Region: testStorageRegion},
		); err != nil {
			_ = queueConn.Close()
			_ = redisClient.Close()
			_ = db.Close()
			fmt.Fprintf(os.Stderr, "create test storage bucket: %v\n", err)
			os.Exit(1)
		}
	}

	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		_ = queueConn.Close()
		_ = redisClient.Close()
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "set migration dialect: %v\n", err)
		os.Exit(1)
	}
	if err := goose.Up(db, migrationsDirectory()); err != nil {
		_ = queueConn.Close()
		_ = redisClient.Close()
		_ = db.Close()
		fmt.Fprintf(os.Stderr, "apply test migrations: %v\n", err)
		os.Exit(1)
	}

	testDatabase = db
	testCache = redisClient
	testQueue = queueConn
	testStorageClient = storageClient
	if err := testCache.FlushDB(context.Background()).Err(); err != nil {
		_ = testQueue.Close()
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
	if err := testQueue.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close test queue: %v\n", err)
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

func waitForStorage(client *minio.Client, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var lastError error
	for {
		if _, err := client.ListBuckets(ctx); err == nil {
			return nil
		} else {
			lastError = err
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: last storage error: %v", ctx.Err(), lastError)
		case <-ticker.C:
		}
	}
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

func waitForQueue(url string, timeout time.Duration) (*amqp.Connection, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastError error
	for {
		conn, err := amqp.Dial(url)
		if err == nil {
			return conn, nil
		}
		lastError = err

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: last dial error: %v", ctx.Err(), lastError)
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
