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
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_goverse_objects_type ON goverse_objects(object_type);
	CREATE INDEX IF NOT EXISTS idx_goverse_objects_updated_at ON goverse_objects(updated_at);

	-- Requests table for exactly-once semantics in inter-object calls
	CREATE TABLE IF NOT EXISTS goverse_requests (
		request_id VARCHAR(255) PRIMARY KEY,
		object_id VARCHAR(255) NOT NULL,
		object_type VARCHAR(255) NOT NULL,
		method_name VARCHAR(255) NOT NULL,
		request_data BYTEA NOT NULL,
		result_data BYTEA,
		error_message TEXT,
		status VARCHAR(50) NOT NULL DEFAULT 'pending' 
			CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
		node_id VARCHAR(255),
		caller_object_id VARCHAR(255),
		caller_client_id VARCHAR(255),
		retry_count INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		processed_at TIMESTAMP,
		expires_at TIMESTAMP,
		CONSTRAINT valid_completed_state CHECK (
			status != 'completed' OR result_data IS NOT NULL
		),
		CONSTRAINT valid_failed_state CHECK (
			status != 'failed' OR error_message IS NOT NULL
		),
		CONSTRAINT valid_processed_at CHECK (
			status NOT IN ('completed', 'failed') OR processed_at IS NOT NULL
		)
	);

	CREATE INDEX IF NOT EXISTS idx_goverse_requests_object_status 
		ON goverse_requests(object_id, status);
	CREATE INDEX IF NOT EXISTS idx_goverse_requests_expires_at 
		ON goverse_requests(expires_at) 
		WHERE expires_at IS NOT NULL;
	CREATE INDEX IF NOT EXISTS idx_goverse_requests_status_created 
		ON goverse_requests(status, created_at);
	CREATE INDEX IF NOT EXISTS idx_goverse_requests_node_status 
		ON goverse_requests(node_id, status) 
		WHERE node_id IS NOT NULL;
	CREATE INDEX IF NOT EXISTS idx_goverse_requests_active 
		ON goverse_requests(request_id, object_id) 
		WHERE status IN ('pending', 'processing');

	-- Trigger function to automatically update updated_at timestamp
	CREATE OR REPLACE FUNCTION update_goverse_requests_timestamp()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = CURRENT_TIMESTAMP;
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;

	-- Trigger to update timestamp on row updates
	DROP TRIGGER IF EXISTS trigger_update_goverse_requests_timestamp ON goverse_requests;
	CREATE TRIGGER trigger_update_goverse_requests_timestamp
		BEFORE UPDATE ON goverse_requests
		FOR EACH ROW
		EXECUTE FUNCTION update_goverse_requests_timestamp();

	-- Helper function to delete expired requests
	CREATE OR REPLACE FUNCTION cleanup_expired_requests()
	RETURNS INTEGER AS $$
	DECLARE
		deleted_count INTEGER;
	BEGIN
		DELETE FROM goverse_requests
		WHERE expires_at IS NOT NULL AND expires_at < CURRENT_TIMESTAMP;
		
		GET DIAGNOSTICS deleted_count = ROW_COUNT;
		RETURN deleted_count;
	END;
	$$ LANGUAGE plpgsql;

	-- Helper function to mark stuck requests as failed
	CREATE OR REPLACE FUNCTION mark_stuck_requests_as_failed(
		stuck_duration INTERVAL DEFAULT '5 minutes'
	)
	RETURNS INTEGER AS $$
	DECLARE
		affected_count INTEGER;
	BEGIN
		UPDATE goverse_requests
		SET status = 'failed',
			error_message = 'Request stuck in processing state (node may have crashed)',
			processed_at = CURRENT_TIMESTAMP
		WHERE status = 'processing' 
		  AND updated_at < (CURRENT_TIMESTAMP - stuck_duration);
		
		GET DIAGNOSTICS affected_count = ROW_COUNT;
		RETURN affected_count;
	END;
	$$ LANGUAGE plpgsql;
	`

	_, err := db.conn.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	return nil
}
