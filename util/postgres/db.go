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

// InitSchema initializes the database schema for object persistence
func (db *DB) InitSchema(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS goverse_objects (
		object_id VARCHAR(255) PRIMARY KEY,
		object_type VARCHAR(255) NOT NULL,
		data JSONB NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_goverse_objects_type ON goverse_objects(object_type);
	CREATE INDEX IF NOT EXISTS idx_goverse_objects_updated_at ON goverse_objects(updated_at);
	`

	_, err := db.conn.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	return nil
}
