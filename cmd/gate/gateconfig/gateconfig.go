// Package gateconfig handles command-line flags and config file loading
// for the gate process, returning a GateServerConfig.
package gateconfig

import (
	"flag"
	"fmt"
	"os"

	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/gate/gateserver"
)

const (
	DefaultListenAddr    = ":49000"
	DefaultAdvertiseAddr = "localhost:49000"
	DefaultEtcdAddr      = "localhost:2379"
	DefaultEtcdPrefix    = "/goverse"
)

// Loader handles parsing of command-line flags and config file loading.
// It can be instantiated with a custom FlagSet for testing.
type Loader struct {
	fs             *flag.FlagSet
	configPath     *string
	gateID         *string
	listenAddr     *string
	advertiseAddr  *string
	httpListenAddr *string
	etcdAddr       *string
	etcdPrefix     *string
}

// NewLoader creates a new Loader with flags registered on the provided FlagSet.
// If fs is nil, the default flag.CommandLine is used.
func NewLoader(fs *flag.FlagSet) *Loader {
	if fs == nil {
		fs = flag.CommandLine
	}
	l := &Loader{fs: fs}
	l.configPath = fs.String("config", "", "Path to YAML config file")
	l.gateID = fs.String("gate-id", "", "Gate ID (required if config is provided)")
	l.listenAddr = fs.String("listen", DefaultListenAddr, "Gate listen address")
	l.advertiseAddr = fs.String("advertise", DefaultAdvertiseAddr, "Gate advertise address")
	l.httpListenAddr = fs.String("http-listen", "", "HTTP listen address for REST API and metrics (optional, e.g., ':8080')")
	l.etcdAddr = fs.String("etcd", DefaultEtcdAddr, "Etcd address")
	l.etcdPrefix = fs.String("etcd-prefix", DefaultEtcdPrefix, "Etcd key prefix")
	return l
}

// Load parses the flags (if not already parsed) and returns a GateServerConfig.
// It loads the config file if --config is provided, and merges with CLI flags.
// Returns an error if configuration is invalid.
func (l *Loader) Load(args []string) (*gateserver.GateServerConfig, error) {
	if !l.fs.Parsed() {
		if err := l.fs.Parse(args); err != nil {
			return nil, fmt.Errorf("failed to parse flags: %w", err)
		}
	}

	var numShards int
	if *l.configPath != "" {
		// Load config from file
		cfg, err := config.LoadConfig(*l.configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		if *l.gateID == "" {
			return nil, fmt.Errorf("--gate-id is required when using --config")
		}
		gateCfg, err := cfg.GetGateByID(*l.gateID)
		if err != nil {
			return nil, fmt.Errorf("failed to find gate config: %w", err)
		}
		// Use config values, with flags as overrides only if they differ from defaults
		if *l.listenAddr == DefaultListenAddr {
			*l.listenAddr = gateCfg.GRPCAddr
		}
		if *l.advertiseAddr == DefaultAdvertiseAddr {
			if gateCfg.AdvertiseAddr != "" {
				*l.advertiseAddr = gateCfg.AdvertiseAddr
			} else {
				*l.advertiseAddr = gateCfg.GRPCAddr
			}
		}
		if *l.httpListenAddr == "" && gateCfg.HTTPAddr != "" {
			*l.httpListenAddr = gateCfg.HTTPAddr
		}
		if *l.etcdAddr == DefaultEtcdAddr {
			*l.etcdAddr = cfg.GetEtcdAddress()
		}
		if *l.etcdPrefix == DefaultEtcdPrefix {
			*l.etcdPrefix = cfg.GetEtcdPrefix()
		}
		numShards = cfg.GetNumShards()
	}

	return &gateserver.GateServerConfig{
		ListenAddress:     *l.listenAddr,
		AdvertiseAddress:  *l.advertiseAddr,
		HTTPListenAddress: *l.httpListenAddr,
		EtcdAddress:       *l.etcdAddr,
		EtcdPrefix:        *l.etcdPrefix,
		NumShards:         numShards,
	}, nil
}

// Get is a convenience function that creates a Loader with default flags,
// parses os.Args[1:], and returns the GateServerConfig.
// It panics on error.
func Get() *gateserver.GateServerConfig {
	loader := NewLoader(nil)
	cfg, err := loader.Load(os.Args[1:])
	if err != nil {
		panic(fmt.Sprintf("Failed to load gate config: %v", err))
	}
	return cfg
}
