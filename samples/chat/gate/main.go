package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/xiaonanln/goverse/cmd/gate/gateconfig"
	"github.com/xiaonanln/goverse/gate/gateserver"
	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/logger"
)

var log = logger.NewLogger("ChatGate")

// ChatAuthValidator accepts any non-empty username with the password "000000".
// In production, replace this with real credential verification (JWT, OAuth, etc).
type ChatAuthValidator struct{}

func (v *ChatAuthValidator) Validate(_ context.Context, headers map[string][]string) (*goverseapi.CallerIdentity, error) {
	usernames := headers["x-username"]
	if len(usernames) == 0 || usernames[0] == "" {
		return nil, fmt.Errorf("x-username header is required")
	}
	passwords := headers["x-password"]
	if len(passwords) == 0 || passwords[0] != "000000" {
		return nil, fmt.Errorf("invalid password")
	}
	return &goverseapi.CallerIdentity{UserID: usernames[0]}, nil
}

func main() {
	loader := gateconfig.NewLoader(nil)
	cfg, err := loader.Load(os.Args[1:])
	if err != nil {
		log.Fatalf("Failed to load gate config: %v", err)
	}
	cfg.AuthValidator = &ChatAuthValidator{}

	gs, err := gateserver.NewGateServer(cfg)
	if err != nil {
		log.Fatalf("Failed to create gate server: %v", err)
	}

	ctx := context.Background()
	if err := gs.Start(ctx); err != nil {
		log.Fatalf("Failed to start gate server: %v", err)
	}

	log.Infof("Chat gate server started (auth: x-username + x-password=000000)")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	gs.Stop()
	log.Infof("Chat gate stopped")
}
