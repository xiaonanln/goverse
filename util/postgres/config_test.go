package postgres

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	if config.Host != "localhost" {
		t.Errorf("Expected host 'localhost', got '%s'", config.Host)
	}
	if config.Port != 5432 {
		t.Errorf("Expected port 5432, got %d", config.Port)
	}
	if config.User != "goverse" {
		t.Errorf("Expected user 'goverse', got '%s'", config.User)
	}
	if config.Database != "goverse" {
		t.Errorf("Expected database 'goverse', got '%s'", config.Database)
	}
	if config.SSLMode != "disable" {
		t.Errorf("Expected sslmode 'disable', got '%s'", config.SSLMode)
	}
}

func TestConfig_ConnectionString(t *testing.T) {
	config := &Config{
		Host:     "testhost",
		Port:     5433,
		User:     "testuser",
		Password: "testpass",
		Database: "testdb",
		SSLMode:  "require",
	}

	expected := "host=testhost port=5433 user=testuser password=testpass dbname=testdb sslmode=require"
	got := config.ConnectionString()

	if got != expected {
		t.Errorf("ConnectionString() = %s; want %s", got, expected)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Host:     "localhost",
				Port:     5432,
				User:     "user",
				Password: "pass",
				Database: "db",
				SSLMode:  "disable",
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: &Config{
				Port:     5432,
				User:     "user",
				Database: "db",
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: &Config{
				Host:     "localhost",
				Port:     0,
				User:     "user",
				Database: "db",
			},
			wantErr: true,
		},
		{
			name: "missing user",
			config: &Config{
				Host:     "localhost",
				Port:     5432,
				Database: "db",
			},
			wantErr: true,
		},
		{
			name: "missing database",
			config: &Config{
				Host: "localhost",
				Port: 5432,
				User: "user",
			},
			wantErr: true,
		},
		{
			name: "missing sslmode sets default",
			config: &Config{
				Host:     "localhost",
				Port:     5432,
				User:     "user",
				Database: "db",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			// Check if SSLMode default is set when missing and no error
			if !tt.wantErr && tt.name == "missing sslmode sets default" {
				if tt.config.SSLMode != "disable" {
					t.Errorf("Expected SSLMode to default to 'disable', got '%s'", tt.config.SSLMode)
				}
			}
		})
	}
}
