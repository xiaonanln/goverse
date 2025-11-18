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

// cleanupTestTable removes all test data from the goverse_objects table
func cleanupTestTable(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	_, err := db.conn.ExecContext(ctx, "DELETE FROM goverse_objects")
	if err != nil {
		t.Logf("Warning: failed to cleanup test table: %v", err)
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

	// Drop the table first to ensure a clean state
	_, _ = db.conn.ExecContext(ctx, "DROP TABLE IF EXISTS goverse_objects")

	// Initialize schema
	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("InitSchema() failed: %v", err)
	}

	// Verify table exists by querying it
	var count int
	err = db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM goverse_objects").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query goverse_objects table: %v", err)
	}

	if count != 0 {
		t.Fatalf("Expected 0 rows in new table, got %d", count)
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
