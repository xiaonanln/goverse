package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// DB wraps a PostgreSQL database connection with utility methods
type DB struct {
	conn   *sql.DB
	config *Config
}

// NewDB creates a new database connection using the provided configuration
func NewDB(config *Config) (*DB, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	conn, err := sql.Open("postgres", config.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)

	return &DB{
		conn:   conn,
		config: config,
	}, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	if db.conn != nil {
		return db.conn.Close()
	}
	return nil
}

// Connection returns the underlying sql.DB connection
func (db *DB) Connection() *sql.DB {
	return db.conn
}

// Ping checks if the database connection is alive
func (db *DB) Ping(ctx context.Context) error {
	return db.conn.PingContext(ctx)
}

// InitSchema initializes the database schema for object persistence and request tracking
func (db *DB) InitSchema(ctx context.Context) error {
	schema := `
	-- Objects table for persistent object state
	CREATE TABLE IF NOT EXISTS goverse_objects (
		object_id VARCHAR(255) PRIMARY KEY,
		object_type VARCHAR(255) NOT NULL,
		data JSONB NOT NULL,
		next_rcseq BIGINT NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_goverse_objects_type ON goverse_objects(object_type);
	CREATE INDEX IF NOT EXISTS idx_goverse_objects_updated_at ON goverse_objects(updated_at);

	-- Reliable calls table for exactly-once semantics in inter-object calls
	CREATE TABLE IF NOT EXISTS goverse_reliable_calls (
		-- Auto-incrementing integer sequence for ordering and reference
		seq BIGSERIAL UNIQUE,
		call_id VARCHAR(255) PRIMARY KEY,
		object_id VARCHAR(255) NOT NULL,
		object_type VARCHAR(255) NOT NULL,
		method_name VARCHAR(255) NOT NULL,
		request_data BYTEA NOT NULL,
		result_data BYTEA,
		error_message TEXT,
		status VARCHAR(50) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT valid_completed_state CHECK (
			status != 'completed' OR result_data IS NOT NULL
		),
		CONSTRAINT valid_failed_state CHECK (
			status != 'failed' OR error_message IS NOT NULL
		)
	);

	CREATE INDEX IF NOT EXISTS idx_goverse_reliable_calls_object_status 
		ON goverse_reliable_calls(object_id, status);

	-- Trigger function to automatically update updated_at timestamp
	CREATE OR REPLACE FUNCTION update_goverse_reliable_calls_timestamp()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = CURRENT_TIMESTAMP;
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	-- Trigger to update timestamp on row updates
	-- Note: PostgreSQL doesn't support CREATE OR REPLACE for triggers, so we use DROP IF EXISTS
	-- This ensures the trigger is created correctly even if InitSchema is called multiple times
	DROP TRIGGER IF EXISTS trigger_update_goverse_reliable_calls_timestamp ON goverse_reliable_calls;
	CREATE TRIGGER trigger_update_goverse_reliable_calls_timestamp
		BEFORE UPDATE ON goverse_reliable_calls
		FOR EACH ROW
		EXECUTE FUNCTION update_goverse_reliable_calls_timestamp();
	`

	_, err := db.conn.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	return nil
}
