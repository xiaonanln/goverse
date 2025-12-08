package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/util/postgres"
)

const (
	commandInit   = "init"
	commandVerify = "verify"
	commandReset  = "reset"
	commandStatus = "status"
)

// Schema constants - MUST be kept in sync with util/postgres/db.go:InitSchema()
// These constants define the database schema managed by this tool.
// When adding new tables or indexes, update both InitSchema() and these constants.
const (
	tableObjects  = "goverse_objects"
	tableRequests = "goverse_requests"
	dropSchemaSQL = `
		DROP TABLE IF EXISTS goverse_requests CASCADE;
		DROP TABLE IF EXISTS goverse_objects CASCADE;
		DROP FUNCTION IF EXISTS update_goverse_requests_timestamp() CASCADE;
	`
)

var (
	// Tables to manage
	tables = []string{tableObjects, tableRequests}

	// Indexes per table
	indexes = map[string][]string{
		tableObjects:  {"idx_goverse_objects_type", "idx_goverse_objects_updated_at"},
		tableRequests: {"idx_goverse_requests_object_status"},
	}
)

func main() {
	var (
		configFile = flag.String("config", "", "Path to YAML configuration file")
		host       = flag.String("host", "localhost", "PostgreSQL host")
		port       = flag.Int("port", 5432, "PostgreSQL port")
		user       = flag.String("user", "goverse", "PostgreSQL user")
		password   = flag.String("password", "goverse", "PostgreSQL password")
		database   = flag.String("database", "goverse", "PostgreSQL database")
		sslmode    = flag.String("sslmode", "disable", "PostgreSQL SSL mode")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <command>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "PostgreSQL database management tool for GoVerse.\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  init     Initialize database schema (create tables and indexes)\n")
		fmt.Fprintf(os.Stderr, "  verify   Verify database connection and schema\n")
		fmt.Fprintf(os.Stderr, "  reset    Drop and recreate database schema (WARNING: deletes all data)\n")
		fmt.Fprintf(os.Stderr, "  status   Show database status and statistics\n")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Initialize schema using config file\n")
		fmt.Fprintf(os.Stderr, "  %s --config config.yml init\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Initialize schema using flags\n")
		fmt.Fprintf(os.Stderr, "  %s --host localhost --user goverse --password goverse --database goverse init\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Verify connection\n")
		fmt.Fprintf(os.Stderr, "  %s --config config.yml verify\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Show database status\n")
		fmt.Fprintf(os.Stderr, "  %s --config config.yml status\n\n", os.Args[0])
	}

	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "Error: command required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	command := flag.Arg(0)
	if command != commandInit && command != commandVerify && command != commandReset && command != commandStatus {
		fmt.Fprintf(os.Stderr, "Error: unknown command '%s'\n\n", command)
		flag.Usage()
		os.Exit(1)
	}

	// Load configuration
	var pgConfig *postgres.Config
	if *configFile != "" {
		cfg, err := config.LoadConfig(*configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config file: %v\n", err)
			os.Exit(1)
		}
		pgConfig = &postgres.Config{
			Host:     cfg.Postgres.Host,
			Port:     cfg.Postgres.Port,
			User:     cfg.Postgres.User,
			Password: cfg.Postgres.Password,
			Database: cfg.Postgres.Database,
			SSLMode:  cfg.Postgres.SSLMode,
		}
		// Apply defaults if not set in config
		if pgConfig.Host == "" {
			pgConfig.Host = "localhost"
		}
		if pgConfig.Port == 0 {
			pgConfig.Port = 5432
		}
		if pgConfig.User == "" {
			pgConfig.User = "goverse"
		}
		if pgConfig.Database == "" {
			pgConfig.Database = "goverse"
		}
		if pgConfig.SSLMode == "" {
			pgConfig.SSLMode = "disable"
		}
	} else {
		pgConfig = &postgres.Config{
			Host:     *host,
			Port:     *port,
			User:     *user,
			Password: *password,
			Database: *database,
			SSLMode:  *sslmode,
		}
	}

	// Execute command
	ctx := context.Background()
	if err := executeCommand(ctx, command, pgConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func executeCommand(ctx context.Context, command string, config *postgres.Config) error {
	switch command {
	case commandInit:
		return initSchema(ctx, config)
	case commandVerify:
		return verifyDatabase(ctx, config)
	case commandReset:
		return resetSchema(ctx, config)
	case commandStatus:
		return showStatus(ctx, config)
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func initSchema(ctx context.Context, config *postgres.Config) error {
	fmt.Println("Initializing PostgreSQL schema...")
	fmt.Printf("  Host: %s:%d\n", config.Host, config.Port)
	fmt.Printf("  Database: %s\n", config.Database)
	fmt.Printf("  User: %s\n", config.User)

	db, err := postgres.NewDB(config)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	fmt.Println("✓ Connected to database")

	// Initialize schema
	if err := db.InitSchema(ctx); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}
	fmt.Println("✓ Schema initialized successfully")

	// Verify tables were created
	for _, table := range tables {
		exists, err := tableExists(ctx, db, table)
		if err != nil {
			return fmt.Errorf("failed to verify table %s: %w", table, err)
		}
		if exists {
			fmt.Printf("✓ Table '%s' created\n", table)
		} else {
			return fmt.Errorf("table '%s' was not created", table)
		}
	}

	fmt.Println("\nDatabase schema initialized successfully!")
	return nil
}

func verifyDatabase(ctx context.Context, config *postgres.Config) error {
	fmt.Println("Verifying PostgreSQL database...")
	fmt.Printf("  Host: %s:%d\n", config.Host, config.Port)
	fmt.Printf("  Database: %s\n", config.Database)

	db, err := postgres.NewDB(config)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	fmt.Println("✓ Connection successful")

	// Check tables
	allTablesExist := true
	for _, table := range tables {
		exists, err := tableExists(ctx, db, table)
		if err != nil {
			return fmt.Errorf("failed to check table %s: %w", table, err)
		}
		if exists {
			fmt.Printf("✓ Table '%s' exists\n", table)
		} else {
			fmt.Printf("✗ Table '%s' does not exist\n", table)
			allTablesExist = false
		}
	}

	if !allTablesExist {
		fmt.Println("\nSchema is incomplete. Run 'init' command to create tables.")
		return fmt.Errorf("schema verification failed")
	}

	// Check indexes
	for table, idxList := range indexes {
		for _, idx := range idxList {
			exists, err := indexExists(ctx, db, table, idx)
			if err != nil {
				return fmt.Errorf("failed to check index %s: %w", idx, err)
			}
			if exists {
				fmt.Printf("✓ Index '%s' exists\n", idx)
			} else {
				fmt.Printf("✗ Index '%s' does not exist\n", idx)
			}
		}
	}

	fmt.Println("\nDatabase verification complete!")
	return nil
}

func resetSchema(ctx context.Context, config *postgres.Config) error {
	fmt.Println("WARNING: This will delete all data in the database!")
	fmt.Print("Are you sure you want to continue? (yes/no): ")

	// Use bufio.Scanner for safe input handling
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return fmt.Errorf("failed to read input")
	}
	response := strings.ToLower(strings.TrimSpace(scanner.Text()))

	if response != "yes" {
		fmt.Println("Operation cancelled.")
		return nil
	}

	fmt.Println("\nResetting PostgreSQL schema...")
	fmt.Printf("  Host: %s:%d\n", config.Host, config.Port)
	fmt.Printf("  Database: %s\n", config.Database)

	db, err := postgres.NewDB(config)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Drop tables
	_, err = db.Connection().ExecContext(ctx, dropSchemaSQL)
	if err != nil {
		return fmt.Errorf("failed to drop tables: %w", err)
	}
	fmt.Println("✓ Dropped existing tables")

	// Reinitialize schema
	if err := db.InitSchema(ctx); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}
	fmt.Println("✓ Schema recreated successfully")

	fmt.Println("\nDatabase schema reset complete!")
	return nil
}

func showStatus(ctx context.Context, config *postgres.Config) error {
	fmt.Println("PostgreSQL Database Status")
	fmt.Println("==========================")
	fmt.Printf("Host:     %s:%d\n", config.Host, config.Port)
	fmt.Printf("Database: %s\n", config.Database)
	fmt.Printf("User:     %s\n", config.User)
	fmt.Println()

	db, err := postgres.NewDB(config)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Test connection
	start := time.Now()
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	latency := time.Since(start)
	fmt.Printf("Connection: ✓ (latency: %v)\n", latency)

	// Get version
	var version string
	err = db.Connection().QueryRowContext(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		return fmt.Errorf("failed to get version: %w", err)
	}
	// Truncate version string if too long
	if len(version) > 80 {
		version = version[:77] + "..."
	}
	fmt.Printf("Version:    %s\n\n", version)

	// Check tables and row counts
	fmt.Println("Tables:")
	fmt.Println("-------")

	for _, table := range tables {
		exists, err := tableExists(ctx, db, table)
		if err != nil {
			return fmt.Errorf("failed to check table %s: %w", table, err)
		}

		if !exists {
			fmt.Printf("  %s: ✗ (does not exist)\n", table)
			continue
		}

		// Get row count
		// Validate table name is in whitelist before using in query
		if !isValidTableName(table) {
			fmt.Printf("  %s: ✗ (invalid table name)\n", table)
			continue
		}
		var count int64
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		err = db.Connection().QueryRowContext(ctx, query).Scan(&count)
		if err != nil {
			fmt.Printf("  %s: ✓ (exists, unable to count rows)\n", table)
		} else {
			fmt.Printf("  %s: ✓ (%d rows)\n", table, count)
		}
	}

	// Get database size
	var dbSize string
	err = db.Connection().QueryRowContext(ctx,
		"SELECT pg_size_pretty(pg_database_size($1))", config.Database).Scan(&dbSize)
	if err == nil {
		fmt.Printf("\nDatabase Size: %s\n", dbSize)
	}

	return nil
}

// Helper functions

// isValidTableName checks if a table name is in our whitelist
func isValidTableName(tableName string) bool {
	for _, validTable := range tables {
		if tableName == validTable {
			return true
		}
	}
	return false
}

func tableExists(ctx context.Context, db *postgres.DB, tableName string) (bool, error) {
	var exists bool
	query := `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = $1
		)
	`
	err := db.Connection().QueryRowContext(ctx, query, tableName).Scan(&exists)
	return exists, err
}

func indexExists(ctx context.Context, db *postgres.DB, tableName, indexName string) (bool, error) {
	var exists bool
	query := `
		SELECT EXISTS (
			SELECT FROM pg_indexes 
			WHERE schemaname = 'public' 
			AND tablename = $1 
			AND indexname = $2
		)
	`
	err := db.Connection().QueryRowContext(ctx, query, tableName, indexName).Scan(&exists)
	return exists, err
}
