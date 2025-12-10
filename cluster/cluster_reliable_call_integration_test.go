package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestReliableCallObject_PostgresIntegration tests ReliableCallObject with actual PostgreSQL database
func TestReliableCallObject_PostgresIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use testutil to get a unique prefix for this test
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create PostgreSQL config
	pgConfig := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}

	// Create DB connection
	db, err := postgres.NewDB(pgConfig)
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	err = db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Create node and cluster
	nodeAddr := testutil.GetFreeAddress()
	n := node.NewNode(nodeAddr, testutil.TestNumShards)
	n.SetPersistenceProvider(provider)

	err = n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	cfg := Config{
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  etcdPrefix,
		MinQuorum:   1,
		NumShards:   testutil.TestNumShards,
	}

	cluster, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	err = cluster.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}
	defer cluster.Stop(ctx)

	t.Run("Insert new call", func(t *testing.T) {
		callID := "integration-test-call-1"
		objectType := "TestType"
		objectID := "test-obj-1"
		methodName := "TestMethod"
		requestData := []byte("test request data")

		rc, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if rc.CallID != callID {
			t.Errorf("Expected call_id %q, got %q", callID, rc.CallID)
		}
		if rc.Status != "pending" {
			t.Errorf("Expected status 'pending', got %q", rc.Status)
		}
		if rc.Seq <= 0 {
			t.Errorf("Expected positive seq, got %d", rc.Seq)
		}
	})

	t.Run("Deduplication - pending call", func(t *testing.T) {
		callID := "integration-test-call-2"
		objectType := "TestType"
		objectID := "test-obj-2"
		methodName := "TestMethod"
		requestData := []byte("test request data 2")

		// Insert first call
		rc1, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
		if err != nil {
			t.Fatalf("First call failed: %v", err)
		}

		// Insert duplicate call
		rc2, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
		if err != nil {
			t.Fatalf("Duplicate call failed: %v", err)
		}

		// Should return the same call
		if rc2.Seq != rc1.Seq {
			t.Errorf("Expected same seq %d, got %d", rc1.Seq, rc2.Seq)
		}
		if rc2.Status != "pending" {
			t.Errorf("Expected status 'pending', got %q", rc2.Status)
		}
	})

	t.Run("Deduplication - completed call", func(t *testing.T) {
		callID := "integration-test-call-3"
		objectType := "TestType"
		objectID := "test-obj-3"
		methodName := "TestMethod"
		requestData := []byte("test request data 3")
		resultData := []byte("test result")

		// Insert first call
		rc1, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
		if err != nil {
			t.Fatalf("First call failed: %v", err)
		}

		// Update status to completed
		err = provider.UpdateReliableCallStatus(ctx, rc1.Seq, "completed", resultData, "")
		if err != nil {
			t.Fatalf("Failed to update call status: %v", err)
		}

		// Try to insert duplicate call
		rc2, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
		if err != nil {
			t.Fatalf("Duplicate call failed: %v", err)
		}

		// Should return the completed call with cached result
		if rc2.Status != "completed" {
			t.Errorf("Expected status 'completed', got %q", rc2.Status)
		}
		if string(rc2.ResultData) != string(resultData) {
			t.Errorf("Expected result_data %q, got %q", resultData, rc2.ResultData)
		}
	})

	t.Run("Deduplication - failed call", func(t *testing.T) {
		callID := "integration-test-call-4"
		objectType := "TestType"
		objectID := "test-obj-4"
		methodName := "TestMethod"
		requestData := []byte("test request data 4")
		errorMessage := "test error message"

		// Insert first call
		rc1, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
		if err != nil {
			t.Fatalf("First call failed: %v", err)
		}

		// Update status to failed
		err = provider.UpdateReliableCallStatus(ctx, rc1.Seq, "failed", nil, errorMessage)
		if err != nil {
			t.Fatalf("Failed to update call status: %v", err)
		}

		// Try to insert duplicate call
		rc2, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
		if err != nil {
			t.Fatalf("Duplicate call failed: %v", err)
		}

		// Should return the failed call with cached error
		if rc2.Status != "failed" {
			t.Errorf("Expected status 'failed', got %q", rc2.Status)
		}
		if rc2.Error != errorMessage {
			t.Errorf("Expected error %q, got %q", errorMessage, rc2.Error)
		}
	})
}
