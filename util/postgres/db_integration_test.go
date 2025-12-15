package postgres

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/util/testutil"
)

func TestNewDB_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Create test database with automatic cleanup
	db := testutil.CreateTestDatabase(t)
	if db == nil {
		return
	}

	// Verify connection is working
	ctx := context.Background()
	err := db.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping() failed: %v", err)
	}
}

func TestDB_InitSchema_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Create test database with automatic cleanup
	db := testutil.CreateTestDatabase(t)
	if db == nil {
		return
	}

	ctx := context.Background()

	// Initialize schema
	err := db.InitSchema(ctx)
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

	// Verify goverse_reliable_calls table exists by querying it
	var requestCount int
	err = db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM goverse_reliable_calls").Scan(&requestCount)
	if err != nil {
		t.Fatalf("Failed to query goverse_reliable_calls table: %v", err)
	}

	if requestCount != 0 {
		t.Fatalf("Expected 0 rows in new goverse_reliable_calls table, got %d", requestCount)
	}

	// Verify trigger function exists
	var funcExists bool
	err = db.conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_proc WHERE proname = 'update_goverse_reliable_calls_timestamp'
		)
	`).Scan(&funcExists)
	if err != nil {
		t.Fatalf("Failed to check for update_goverse_reliable_calls_timestamp function: %v", err)
	}
	if !funcExists {
		t.Fatal("update_goverse_reliable_calls_timestamp function was not created")
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

	// Create test database with automatic cleanup
	db := testutil.CreateTestDatabase(t)
	if db == nil {
		return
	}

	ctx := context.Background()
	err := db.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping() failed: %v", err)
	}
}

func TestDB_Close_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Create test database with automatic cleanup
	db := testutil.CreateTestDatabase(t)
	if db == nil {
		return
	}

	err := db.Close()
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

	// Create test database with automatic cleanup
	db := testutil.CreateTestDatabase(t)
	if db == nil {
		return
	}

	conn := db.Connection()
	if conn == nil {
		t.Fatal("Connection() returned nil")
	}

	// Verify we can use the raw connection
	ctx := context.Background()
	err := conn.PingContext(ctx)
	if err != nil {
		t.Fatalf("PingContext() on raw connection failed: %v", err)
	}
}
