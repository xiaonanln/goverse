package testutil

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/xiaonanln/goverse/util/postgres"
)

// sanitizeDBName converts a test name to a valid PostgreSQL database name.
// PostgreSQL database names must be <= 63 chars, start with letter/underscore,
// and contain only letters, digits, and underscores.
func sanitizeDBName(testName string) string {
	// Replace invalid characters with underscores
	re := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	name := re.ReplaceAllString(testName, "_")

	// Ensure it starts with a letter or underscore
	if len(name) > 0 && name[0] >= '0' && name[0] <= '9' {
		name = "t_" + name
	}

	// Convert to lowercase for consistency
	name = strings.ToLower(name)

	// Truncate to 63 characters (PostgreSQL limit)
	if len(name) > 63 {
		name = name[:63]
	}

	return name
}

// CreateTestDatabase creates a test database based on the test name and returns a connection to it.
// The database name is automatically derived from t.Name().
// It drops any existing database with the same name, creates a fresh one, and registers
// a cleanup function to drop the database when the test completes.
// If PostgreSQL is not available, the test is skipped.
func CreateTestDatabase(t *testing.T) *postgres.DB {
	t.Helper()

	dbName := sanitizeDBName(t.Name())

	// First, connect to the default postgres database to manage the test database
	adminConfig := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}

	adminDB, err := postgres.NewDB(adminConfig)
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return nil
	}

	// Drop the database if it exists (force disconnect any existing connections)
	_, _ = adminDB.Connection().ExecContext(context.Background(),
		fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", dbName))

	// Create fresh database
	_, err = adminDB.Connection().ExecContext(context.Background(),
		fmt.Sprintf("CREATE DATABASE %s", dbName))
	if err != nil {
		adminDB.Close()
		t.Skipf("Failed to create test database: %v", err)
		return nil
	}
	adminDB.Close()

	// Now connect to the test database
	testConfig := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: dbName,
		SSLMode:  "disable",
	}

	db, err := postgres.NewDB(testConfig)
	if err != nil {
		t.Skipf("Skipping test - Failed to connect to test database: %v", err)
		return nil
	}

	// Register cleanup to drop the database when test completes
	t.Cleanup(func() {
		db.Close()

		// Reconnect to admin database to drop test database
		cleanupDB, err := postgres.NewDB(adminConfig)
		if err != nil {
			t.Logf("Warning: Failed to connect for cleanup: %v", err)
			return
		}
		defer cleanupDB.Close()

		_, err = cleanupDB.Connection().ExecContext(context.Background(),
			fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", dbName))
		if err != nil {
			t.Logf("Warning: Failed to drop test database: %v", err)
		}
	})

	return db
}
