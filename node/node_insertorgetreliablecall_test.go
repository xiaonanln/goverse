package node

import (
	"context"
	"os"
	"testing"

	"github.com/xiaonanln/goverse/util/postgres"
	"google.golang.org/protobuf/types/known/structpb"
)

// getPostgresConfig returns the PostgreSQL configuration for testing
func getPostgresConfig() *postgres.Config {
	return &postgres.Config{
		Host:     getEnvOrDefault("POSTGRES_HOST", "localhost"),
		Port:     5432,
		User:     getEnvOrDefault("POSTGRES_USER", "postgres"),
		Password: getEnvOrDefault("POSTGRES_PASSWORD", "postgres"),
		Database: getEnvOrDefault("POSTGRES_DB", "postgres"),
		SSLMode:  "disable",
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// skipIfNoPostgres skips the test if PostgreSQL is not available
func skipIfNoPostgres(t *testing.T) *postgres.Config {
	t.Helper()

	if os.Getenv("SKIP_POSTGRES_TESTS") == "1" {
		t.Skip("Skipping PostgreSQL integration test (SKIP_POSTGRES_TESTS=1)")
	}

	return getPostgresConfig()
}

// cleanupTestTables removes all test data from the tables
func cleanupTestTables(t *testing.T, db *postgres.DB) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Connection().ExecContext(ctx, "DELETE FROM goverse_objects")
	if err != nil {
		t.Logf("Warning: failed to cleanup goverse_objects table: %v", err)
	}
	_, err = db.Connection().ExecContext(ctx, "DELETE FROM goverse_reliable_calls")
	if err != nil {
		t.Logf("Warning: failed to cleanup goverse_reliable_calls table: %v", err)
	}
}

// TestNode_InsertOrGetReliableCall tests the Node.InsertOrGetReliableCall function with real postgres
func TestNode_InsertOrGetReliableCall(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping postgres test in short mode")
	}
	config := skipIfNoPostgres(t)

	// Create database connection
	db, err := postgres.NewDB(config)
	if err != nil {
		t.Skipf("Skipping - postgres unavailable: %v", err)
		return
	}
	defer db.Close()

	ctx := context.Background()

	// Initialize schema
	err = db.InitSchema(ctx)
	if err != nil {
		t.Skipf("Skipping - postgres schema init failed: %v", err)
		return
	}
	defer cleanupTestTables(t, db)

	// Create node with postgres persistence
	node := NewNode("localhost:47000", testNumShards)
	provider := postgres.NewPostgresPersistenceProvider(db)
	node.SetPersistenceProvider(provider)

	// Test data
	callID := "test-call-123"
	objectType := "TestObject"
	objectID := "test-obj-123"
	methodName := "TestMethod"

	// Create a simple request message
	request, err := structpb.NewStruct(map[string]interface{}{
		"param1": "value1",
		"param2": 42,
	})
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Test 1: First call should insert a new reliable call
	rc1, err := node.InsertOrGetReliableCall(ctx, callID, objectType, objectID, methodName, request)
	if err != nil {
		t.Fatalf("InsertOrGetReliableCall() first call failed: %v", err)
	}

	// Verify the returned reliable call
	if rc1.CallID != callID {
		t.Fatalf("CallID = %s, want %s", rc1.CallID, callID)
	}
	if rc1.ObjectID != objectID {
		t.Fatalf("ObjectID = %s, want %s", rc1.ObjectID, objectID)
	}
	if rc1.ObjectType != objectType {
		t.Fatalf("ObjectType = %s, want %s", rc1.ObjectType, objectType)
	}
	if rc1.MethodName != methodName {
		t.Fatalf("MethodName = %s, want %s", rc1.MethodName, methodName)
	}
	if rc1.Status != "pending" {
		t.Fatalf("Status = %s, want pending", rc1.Status)
	}

	// Test 2: Second call with same callID should return the existing reliable call
	rc2, err := node.InsertOrGetReliableCall(ctx, callID, objectType, objectID, methodName, request)
	if err != nil {
		t.Fatalf("InsertOrGetReliableCall() second call failed: %v", err)
	}

	// Verify we got the same reliable call (same Seq)
	if rc2.Seq != rc1.Seq {
		t.Fatalf("Second call returned different Seq: %d, want %d", rc2.Seq, rc1.Seq)
	}
	if rc2.CallID != rc1.CallID {
		t.Fatalf("Second call returned different CallID: %s, want %s", rc2.CallID, rc1.CallID)
	}

	// Test 3: Call without persistence provider should return error
	nodeNoPersistence := NewNode("localhost:47001", testNumShards)
	_, err = nodeNoPersistence.InsertOrGetReliableCall(ctx, "test-call-456", objectType, objectID, methodName, request)
	if err == nil {
		t.Fatal("Expected error when persistence provider is not configured")
	}
	if err.Error() != "persistence provider is not configured" {
		t.Fatalf("Expected 'persistence provider is not configured' error, got: %v", err)
	}
}
