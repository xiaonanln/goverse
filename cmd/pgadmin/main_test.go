package main

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/util/postgres"
)

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

	// Test with a known system table
	exists, err := tableExists(ctx, db, "pg_class")
	if err != nil {
		t.Fatalf("tableExists failed: %v", err)
	}
	if !exists {
		t.Error("expected pg_class table to exist")
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
	dropSQL := `
		DROP TABLE IF EXISTS goverse_requests CASCADE;
		DROP TABLE IF EXISTS goverse_objects CASCADE;
		DROP FUNCTION IF EXISTS update_goverse_requests_timestamp() CASCADE;
	`
	_, err = db.Connection().ExecContext(ctx, dropSQL)
	if err != nil {
		t.Fatalf("Failed to clean up: %v", err)
	}

	// Initialize schema
	if err := initSchema(ctx, config); err != nil {
		t.Fatalf("initSchema failed: %v", err)
	}

	// Verify tables exist
	tables := []string{"goverse_objects", "goverse_requests"}
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
	indexes := map[string][]string{
		"goverse_objects":  {"idx_goverse_objects_type", "idx_goverse_objects_updated_at"},
		"goverse_requests": {"idx_goverse_requests_object_status"},
	}

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
	_, err = db.Connection().ExecContext(ctx, dropSQL)
	if err != nil {
		t.Logf("Warning: Failed to clean up after test: %v", err)
	}
}
