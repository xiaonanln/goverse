package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/xiaonanln/goverse/util/postgres"
)

// cleanupSchema removes all tables and functions for testing
func cleanupSchema(ctx context.Context, db *postgres.DB) error {
	_, err := db.Connection().ExecContext(ctx, dropSchemaSQL)
	return err
}

func TestTableExists(t *testing.T) {
	config := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}

	db, err := postgres.NewDB(config)
	if err != nil {
		t.Skipf("Skipping test - unable to connect to database: %v", err)
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Ping(ctx); err != nil {
		t.Skipf("Skipping test - unable to ping database: %v", err)
		return
	}

	// Create a temporary test table in the public schema
	testTableName := "test_table_exists_12345"
	_, err = db.Connection().ExecContext(ctx,
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INT)", testTableName))
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}
	defer func() {
		// Clean up test table
		db.Connection().ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", testTableName))
	}()

	// Test with the created table
	exists, err := tableExists(ctx, db, testTableName)
	if err != nil {
		t.Fatalf("tableExists failed: %v", err)
	}
	if !exists {
		t.Errorf("expected %s table to exist", testTableName)
	}

	// Test with a non-existent table
	exists, err = tableExists(ctx, db, "nonexistent_table_12345")
	if err != nil {
		t.Fatalf("tableExists failed: %v", err)
	}
	if exists {
		t.Error("expected nonexistent_table_12345 to not exist")
	}
}

func TestIndexExists(t *testing.T) {
	config := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}

	db, err := postgres.NewDB(config)
	if err != nil {
		t.Skipf("Skipping test - unable to connect to database: %v", err)
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Ping(ctx); err != nil {
		t.Skipf("Skipping test - unable to ping database: %v", err)
		return
	}

	// Test with a non-existent index
	exists, err := indexExists(ctx, db, "pg_class", "nonexistent_index_12345")
	if err != nil {
		t.Fatalf("indexExists failed: %v", err)
	}
	if exists {
		t.Error("expected nonexistent_index_12345 to not exist")
	}
}

func TestInitSchema(t *testing.T) {
	config := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}

	db, err := postgres.NewDB(config)
	if err != nil {
		t.Skipf("Skipping test - unable to connect to database: %v", err)
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Ping(ctx); err != nil {
		t.Skipf("Skipping test - unable to ping database: %v", err)
		return
	}

	// Clean up first
	if err := cleanupSchema(ctx, db); err != nil {
		t.Fatalf("Failed to clean up: %v", err)
	}

	// Initialize schema
	if err := initSchema(ctx, config); err != nil {
		t.Fatalf("initSchema failed: %v", err)
	}

	// Verify tables exist
	for _, table := range tables {
		exists, err := tableExists(ctx, db, table)
		if err != nil {
			t.Fatalf("Failed to check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("Expected table %s to exist after initSchema", table)
		}
	}

	// Verify indexes exist
	for table, idxList := range indexes {
		for _, idx := range idxList {
			exists, err := indexExists(ctx, db, table, idx)
			if err != nil {
				t.Fatalf("Failed to check index %s: %v", idx, err)
			}
			if !exists {
				t.Errorf("Expected index %s to exist after initSchema", idx)
			}
		}
	}

	// Clean up
	if err := cleanupSchema(ctx, db); err != nil {
		t.Logf("Warning: Failed to clean up after test: %v", err)
	}
}
