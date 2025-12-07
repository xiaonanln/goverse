package postgres

import (
	"context"
	"os"
	"testing"
)

// skipIfNoPostgres skips the test if PostgreSQL is not available
func skipIfNoPostgres(t *testing.T) *Config {
	t.Helper()

	// Check if we should skip PostgreSQL tests
	if os.Getenv("SKIP_POSTGRES_TESTS") == "1" {
		t.Skip("Skipping PostgreSQL integration test (SKIP_POSTGRES_TESTS=1)")
	}

	// Use environment variables if set, otherwise use defaults
	config := &Config{
		Host:     getEnvOrDefault("POSTGRES_HOST", "localhost"),
		Port:     5432,
		User:     getEnvOrDefault("POSTGRES_USER", "postgres"),
		Password: getEnvOrDefault("POSTGRES_PASSWORD", "postgres"),
		Database: getEnvOrDefault("POSTGRES_DB", "postgres"),
		SSLMode:  "disable",
	}

	return config
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// cleanupTestTable removes all test data from the goverse_objects and goverse_requests tables
func cleanupTestTable(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	_, err := db.conn.ExecContext(ctx, "DELETE FROM goverse_objects")
	if err != nil {
		t.Logf("Warning: failed to cleanup goverse_objects table: %v", err)
	}
	_, err = db.conn.ExecContext(ctx, "DELETE FROM goverse_requests")
	if err != nil {
		t.Logf("Warning: failed to cleanup goverse_requests table: %v", err)
	}
}

func TestNewDB_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	// Verify connection is working
	ctx := context.Background()
	err = db.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping() failed: %v", err)
	}
}

func TestDB_InitSchema_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Drop functions and tables first to ensure a clean state
	// Drop functions before tables to avoid dependency issues
	_, _ = db.conn.ExecContext(ctx, "DROP FUNCTION IF EXISTS update_goverse_requests_timestamp CASCADE")
	_, _ = db.conn.ExecContext(ctx, "DROP TABLE IF EXISTS goverse_objects CASCADE")
	_, _ = db.conn.ExecContext(ctx, "DROP TABLE IF EXISTS goverse_requests CASCADE")

	// Initialize schema
	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("InitSchema() failed: %v", err)
	}

	// Verify goverse_objects table exists by querying it
	var objectCount int
	err = db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM goverse_objects").Scan(&objectCount)
	if err != nil {
		t.Fatalf("Failed to query goverse_objects table: %v", err)
	}

	if objectCount != 0 {
		t.Fatalf("Expected 0 rows in new goverse_objects table, got %d", objectCount)
	}

	// Verify goverse_requests table exists by querying it
	var requestCount int
	err = db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM goverse_requests").Scan(&requestCount)
	if err != nil {
		t.Fatalf("Failed to query goverse_requests table: %v", err)
	}

	if requestCount != 0 {
		t.Fatalf("Expected 0 rows in new goverse_requests table, got %d", requestCount)
	}

	// Verify trigger function exists
	var funcExists bool
	err = db.conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_proc WHERE proname = 'update_goverse_requests_timestamp'
		)
	`).Scan(&funcExists)
	if err != nil {
		t.Fatalf("Failed to check for update_goverse_requests_timestamp function: %v", err)
	}
	if !funcExists {
		t.Fatal("update_goverse_requests_timestamp function was not created")
	}

	// Verify we can run InitSchema multiple times (idempotent)
	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("InitSchema() second call failed: %v", err)
	}
}

func TestDB_Ping_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	err = db.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping() failed: %v", err)
	}
}

func TestDB_Close_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}

	err = db.Close()
	if err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Verify connection is closed by attempting to ping
	ctx := context.Background()
	err = db.Ping(ctx)
	if err == nil {
		t.Fatal("Ping() should fail after Close()")
	}
}

func TestDB_Connection_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	config := skipIfNoPostgres(t)

	db, err := NewDB(config)
	if err != nil {
		t.Fatalf("NewDB() failed: %v", err)
	}
	defer db.Close()

	conn := db.Connection()
	if conn == nil {
		t.Fatal("Connection() returned nil")
	}

	// Verify we can use the raw connection
	ctx := context.Background()
	err = conn.PingContext(ctx)
	if err != nil {
		t.Fatalf("PingContext() on raw connection failed: %v", err)
	}
}
