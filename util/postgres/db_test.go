package postgres

import (
	"testing"
)

func TestNewDB_InvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name: "missing host",
			config: &Config{
				Port:     5432,
				User:     "user",
				Database: "db",
			},
		},
		{
			name: "invalid port",
			config: &Config{
				Host:     "localhost",
				Port:     -1,
				User:     "user",
				Database: "db",
			},
		},
		{
			name: "missing user",
			config: &Config{
				Host:     "localhost",
				Port:     5432,
				Database: "db",
			},
		},
		{
			name: "missing database",
			config: &Config{
				Host: "localhost",
				Port: 5432,
				User: "user",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := NewDB(tt.config)
			if err == nil {
				if db != nil {
					db.Close()
				}
				t.Error("NewDB() should return error for invalid config")
			}
		})
	}
}

func TestDB_Close_Nil(t *testing.T) {
	db := &DB{}
	err := db.Close()
	if err != nil {
		t.Errorf("Close() on DB with nil connection should not error, got: %v", err)
	}
}

func TestDB_Connection(t *testing.T) {
	db := &DB{}
	conn := db.Connection()
	if conn != nil {
		t.Error("Connection() should return nil when db.conn is nil")
	}
}

// Note: Tests that require actual database connection are skipped
// They would require a running PostgreSQL instance and proper test isolation
// These should be integration tests rather than unit tests
