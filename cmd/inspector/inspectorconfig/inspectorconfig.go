// Package inspectorconfig handles command-line flags and config file loading
// for the inspector process, returning a Config.
package inspectorconfig

import (
	"flag"
	"fmt"
	"os"

	"github.com/xiaonanln/goverse/cmd/inspector/inspectserver"
	"github.com/xiaonanln/goverse/config"
)

const (
	DefaultGRPCAddr   = ":8081"
	DefaultHTTPAddr   = ":8080"
	DefaultEtcdAddr   = "localhost:2379"
	DefaultEtcdPrefix = "/goverse"
	DefaultStaticDir  = "cmd/inspector/web"
)

// Loader handles parsing of command-line flags and config file loading.
// It can be instantiated with a custom FlagSet for testing.
type Loader struct {
	fs         *flag.FlagSet
	configPath *string
	grpcAddr   *string
	httpAddr   *string
	staticDir  *string
	etcdAddr   *string
	etcdPrefix *string
}

// NewLoader creates a new Loader with flags registered on the provided FlagSet.
// If fs is nil, the default flag.CommandLine is used.
func NewLoader(fs *flag.FlagSet) *Loader {
	if fs == nil {
		fs = flag.CommandLine
	}
	l := &Loader{fs: fs}
	l.configPath = fs.String("config", "", "Path to YAML config file")
	l.grpcAddr = fs.String("grpc-addr", DefaultGRPCAddr, "gRPC server address (cannot be used with --config)")
	l.httpAddr = fs.String("http-addr", DefaultHTTPAddr, "HTTP server address (cannot be used with --config)")
	l.staticDir = fs.String("static-dir", DefaultStaticDir, "Static files directory for web UI (cannot be used with --config)")
	l.etcdAddr = fs.String("etcd-addr", "", "etcd server address (optional, cannot be used with --config)")
	l.etcdPrefix = fs.String("etcd-prefix", DefaultEtcdPrefix, "etcd key prefix (cannot be used with --config)")
	return l
}

// Load parses the flags (if not already parsed) and returns a Config.
// When --config is provided, other flags are forbidden.
// When --config is not provided, CLI flags are used.
// Returns an error if configuration is invalid.
func (l *Loader) Load(args []string) (*inspectserver.Config, error) {
	if !l.fs.Parsed() {
		if err := l.fs.Parse(args); err != nil {
			return nil, fmt.Errorf("failed to parse flags: %w", err)
		}
	}

	if *l.configPath != "" {
		// Config file mode: only --config is allowed

		// Check that other flags are not specified (still at their defaults)
		if *l.grpcAddr != DefaultGRPCAddr {
			return nil, fmt.Errorf("--grpc-addr cannot be used with --config; configure in config file instead")
		}
		if *l.httpAddr != DefaultHTTPAddr {
			return nil, fmt.Errorf("--http-addr cannot be used with --config; configure in config file instead")
		}
		if *l.staticDir != DefaultStaticDir {
			return nil, fmt.Errorf("--static-dir cannot be used with --config; configure in config file instead")
		}
		if *l.etcdAddr != "" {
			return nil, fmt.Errorf("--etcd-addr cannot be used with --config; configure in config file instead")
		}
		if *l.etcdPrefix != DefaultEtcdPrefix {
			return nil, fmt.Errorf("--etcd-prefix cannot be used with --config; configure in config file instead")
		}

		// Load config from file
		cfg, err := config.LoadConfig(*l.configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}

		// Inspector configuration is optional in the config file
		if cfg.Inspector.GRPCAddr == "" {
			return nil, fmt.Errorf("inspector configuration is missing in config file")
		}

		return &inspectserver.Config{
			GRPCAddr:   cfg.Inspector.GRPCAddr,
			HTTPAddr:   cfg.Inspector.HTTPAddr,
			StaticDir:  DefaultStaticDir, // Static dir is not configurable in config file
			EtcdAddr:   cfg.GetEtcdAddress(),
			EtcdPrefix: cfg.GetEtcdPrefix(),
		}, nil
	}

	// CLI-only mode: use flag values
	return &inspectserver.Config{
		GRPCAddr:   *l.grpcAddr,
		HTTPAddr:   *l.httpAddr,
		StaticDir:  *l.staticDir,
		EtcdAddr:   *l.etcdAddr,
		EtcdPrefix: *l.etcdPrefix,
	}, nil
}

// Get is a convenience function that creates a Loader with default flags,
// parses os.Args[1:], and returns the Config.
// It panics on error.
func Get() *inspectserver.Config {
	loader := NewLoader(nil)
	cfg, err := loader.Load(os.Args[1:])
	if err != nil {
		panic(fmt.Sprintf("Failed to load inspector config: %v", err))
	}
	return cfg
}
