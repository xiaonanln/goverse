package postgres

import (
	"fmt"
)

// Default connection-pool limits applied when a Config leaves MaxOpenConns /
// MaxIdleConns at zero. Exposed as constants so callers (and tests) can verify
// what they'll get without replicating the numbers.
const (
	DefaultMaxOpenConns = 25
	DefaultMaxIdleConns = 5
)

// Config holds PostgreSQL database connection configuration
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string // disable, require, verify-ca, verify-full

	// MaxOpenConns caps the total connections this pool will open. Leave at 0
	// to use DefaultMaxOpenConns. Deployments with many nodes sharing one
	// Postgres instance should lower this so N*MaxOpenConns stays under the
	// server's max_connections.
	MaxOpenConns int
	// MaxIdleConns caps idle connections held in the pool. Leave at 0 to use
	// DefaultMaxIdleConns.
	MaxIdleConns int
}

// DefaultConfig returns a default configuration for local development
func DefaultConfig() *Config {
	return &Config{
		Host:     "localhost",
		Port:     5432,
		User:     "goverse",
		Password: "goverse",
		Database: "goverse",
		SSLMode:  "disable",
	}
}

// ConnectionString returns a PostgreSQL connection string
func (c *Config) ConnectionString() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Database, c.SSLMode)
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}
	if c.Port <= 0 {
		return fmt.Errorf("port must be positive")
	}
	if c.User == "" {
		return fmt.Errorf("user is required")
	}
	if c.Database == "" {
		return fmt.Errorf("database is required")
	}
	if c.SSLMode == "" {
		c.SSLMode = "disable"
	}
	return nil
}
