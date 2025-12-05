package leaderelection

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/xiaonanln/goverse/util/logger"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

const (
	// DefaultSessionTTL is the default TTL for the etcd session in seconds
	DefaultSessionTTL = 10
)

// LeaderElection manages leader election using etcd's concurrency API
type LeaderElection struct {
	client   *clientv3.Client
	session  *concurrency.Session
	election *concurrency.Election
	nodeID   string
	prefix   string
	ttl      int
	isLeader atomic.Bool
	logger   *logger.Logger

	// For observing leader changes
	observeCtx    context.Context
	observeCancel context.CancelFunc
	observeWg     sync.WaitGroup

	// Current observed leader
	mu            sync.RWMutex
	currentLeader string

	// Campaign management
	campaignCtx    context.Context
	campaignCancel context.CancelFunc
	campaignWg     sync.WaitGroup
}

// NewLeaderElection creates a new leader election instance
// prefix: etcd key prefix for the election (e.g., "/goverse/leader")
// nodeID: unique identifier for this node (typically the advertise address)
// ttl: session TTL in seconds (lease duration before auto-expiry on crash)
func NewLeaderElection(client *clientv3.Client, prefix string, nodeID string, ttl int) (*LeaderElection, error) {
	if client == nil {
		return nil, fmt.Errorf("etcd client cannot be nil")
	}
	if nodeID == "" {
		return nil, fmt.Errorf("nodeID cannot be empty")
	}
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}

	le := &LeaderElection{
		client: client,
		nodeID: nodeID,
		prefix: prefix,
		ttl:    ttl,
		logger: logger.NewLogger("LeaderElection"),
	}

	return le, nil
}

// Start initializes the election session and starts observing leader changes
func (le *LeaderElection) Start(ctx context.Context) error {
	// Create a new session with the specified TTL
	session, err := concurrency.NewSession(le.client, concurrency.WithTTL(le.ttl))
	if err != nil {
		return fmt.Errorf("failed to create etcd session with TTL %d: %w", le.ttl, err)
	}

	le.session = session
	le.election = concurrency.NewElection(session, le.prefix)

	// Start observing leader changes
	le.observeCtx, le.observeCancel = context.WithCancel(ctx)
	le.observeWg.Add(1)
	go le.observeLeader()

	le.logger.Infof("LeaderElection started for node %s with TTL %d seconds", le.nodeID, le.ttl)
	return nil
}

// Campaign attempts to become the leader and blocks until elected or context is cancelled
// This should be called in a goroutine if non-blocking behavior is desired
func (le *LeaderElection) Campaign(ctx context.Context) error {
	if le.election == nil {
		return fmt.Errorf("election not initialized, call Start() first")
	}

	le.logger.Infof("Node %s campaigning for leadership", le.nodeID)

	// Campaign with the node ID as the value
	err := le.election.Campaign(ctx, le.nodeID)
	if err != nil {
		if err == context.Canceled || err == context.DeadlineExceeded {
			le.logger.Infof("Campaign cancelled for node %s", le.nodeID)
			return err
		}
		return fmt.Errorf("campaign failed: %w", err)
	}

	le.isLeader.Store(true)
	le.logger.Infof("Node %s became leader", le.nodeID)
	return nil
}

// StartCampaign starts campaigning for leadership in the background
func (le *LeaderElection) StartCampaign(ctx context.Context) {
	le.campaignCtx, le.campaignCancel = context.WithCancel(ctx)
	le.campaignWg.Add(1)
	go func() {
		defer le.campaignWg.Done()
		err := le.Campaign(le.campaignCtx)
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			le.logger.Errorf("Campaign failed: %v", err)
		}
	}()
}

// Resign voluntarily steps down from leadership
// This allows for graceful leadership handoff during maintenance
func (le *LeaderElection) Resign(ctx context.Context) error {
	if le.election == nil {
		return fmt.Errorf("election not initialized")
	}

	if !le.isLeader.Load() {
		le.logger.Debugf("Node %s is not leader, nothing to resign", le.nodeID)
		return nil
	}

	le.logger.Infof("Node %s resigning from leadership", le.nodeID)

	err := le.election.Resign(ctx)
	if err != nil {
		return fmt.Errorf("resign failed: %w", err)
	}

	le.isLeader.Store(false)
	le.logger.Infof("Node %s resigned from leadership", le.nodeID)
	return nil
}

// IsLeader returns true if this node is currently the leader
func (le *LeaderElection) IsLeader() bool {
	return le.isLeader.Load()
}

// GetLeader returns the current leader's node ID
func (le *LeaderElection) GetLeader() string {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.currentLeader
}

// observeLeader watches for leader changes and updates the current leader
func (le *LeaderElection) observeLeader() {
	defer le.observeWg.Done()

	if le.election == nil {
		le.logger.Errorf("Cannot observe leader: election not initialized")
		return
	}

	observeChan := le.election.Observe(le.observeCtx)
	le.logger.Infof("Started observing leader changes")

	for {
		select {
		case <-le.observeCtx.Done():
			le.logger.Infof("Stopped observing leader changes")
			return
		case resp, ok := <-observeChan:
			if !ok {
				le.logger.Warnf("Leader observation channel closed")
				return
			}
			
			// Handle both leader present and absent cases
			var leader string
			if len(resp.Kvs) > 0 {
				leader = string(resp.Kvs[0].Value)
			}
			// If len(resp.Kvs) == 0, leader remains empty string (no leader)

			le.mu.Lock()
			oldLeader := le.currentLeader
			le.currentLeader = leader
			
			// Update local leadership status under the same lock
			wasLeader := le.isLeader.Load()
			nowLeader := leader != "" && leader == le.nodeID
			le.isLeader.Store(nowLeader)
			le.mu.Unlock()

			if oldLeader != leader {
				if leader == "" {
					le.logger.Infof("Leader changed from %s to <none> (no leader)", oldLeader)
				} else {
					le.logger.Infof("Leader changed from %s to %s", oldLeader, leader)
				}
			}

			// Log leadership transitions
			if !wasLeader && nowLeader {
				le.logger.Infof("Node %s became leader", le.nodeID)
			} else if wasLeader && !nowLeader {
				le.logger.Infof("Node %s lost leadership", le.nodeID)
			}
		}
	}
}

// Close cleans up resources and closes the session
func (le *LeaderElection) Close() error {
	// Cancel campaign if running
	if le.campaignCancel != nil {
		le.campaignCancel()
	}
	le.campaignWg.Wait()

	// Stop observing
	if le.observeCancel != nil {
		le.observeCancel()
	}
	le.observeWg.Wait()

	// Close session (this also revokes the lease)
	if le.session != nil {
		err := le.session.Close()
		if err != nil {
			le.logger.Warnf("Failed to close session: %v", err)
			return err
		}
	}

	le.logger.Infof("LeaderElection closed for node %s", le.nodeID)
	return nil
}
