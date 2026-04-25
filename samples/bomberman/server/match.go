package main

import (
	"context"
	"sync"

	"github.com/xiaonanln/goverse/goverseapi"
	pb "github.com/xiaonanln/goverse/samples/bomberman/proto"
)

// Match is the goverse object wrapper around MatchState. PR 1 only
// exposes lobby + input + snapshot RPCs and runs the tick logic
// synchronously when the test or stress driver calls AdvanceTickRPC.
// PR 2 will replace AdvanceTickRPC with an internal goroutine driven
// by time.Ticker and broadcast snapshots via push messaging.
type Match struct {
	goverseapi.BaseObject

	mu    sync.Mutex
	state *MatchState
}

func (m *Match) OnCreated() {
	m.state = NewMatchState(seedFromID(m.Id()))
	m.Logger.Infof("Match %s created", m.Id())
}

// AddPlayer places a fresh player on the grid. The Match must still
// be in LOBBY status; spawn coordinates outside the grid or onto a
// non-empty cell are rejected.
func (m *Match) AddPlayer(ctx context.Context, req *pb.AddPlayerRequest) (*pb.AddPlayerResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.state.AddPlayer(req.PlayerId, int(req.SpawnX), int(req.SpawnY)); err != nil {
		return &pb.AddPlayerResponse{Ok: false, Reason: err.Error()}, nil
	}
	return &pb.AddPlayerResponse{Ok: true}, nil
}

// StartMatch transitions the match into RUNNING. Idempotent: calling
// it on an already-running match returns ok=false rather than erroring.
func (m *Match) StartMatch(ctx context.Context, req *pb.StartMatchRequest) (*pb.StartMatchResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.state.Start(); err != nil {
		return &pb.StartMatchResponse{Ok: false, Reason: err.Error()}, nil
	}
	return &pb.StartMatchResponse{Ok: true}, nil
}

// HandleInput queues a player intent for the next tick. Returns ok=false
// only for unknown players; movement that's blocked by a wall is silent.
func (m *Match) HandleInput(ctx context.Context, req *pb.PlayerInputRequest) (*pb.PlayerInputResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	in := Input{Move: req.Move, PlaceBomb: req.PlaceBomb}
	if err := m.state.QueueInput(req.PlayerId, in); err != nil {
		return &pb.PlayerInputResponse{Ok: false, Reason: err.Error()}, nil
	}
	return &pb.PlayerInputResponse{Ok: true}, nil
}

// GetSnapshot returns the current authoritative state. Cheap to call
// every tick; clients should rely on push (PR 2) instead of polling.
func (m *Match) GetSnapshot(ctx context.Context, req *pb.GetSnapshotRequest) (*pb.GetSnapshotResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &pb.GetSnapshotResponse{Snapshot: m.state.Snapshot(m.Id())}, nil
}

// seedFromID derives a deterministic RNG seed from the object ID so
// powerup drops in unit tests / replays are reproducible.
func seedFromID(id string) int64 {
	var h int64 = 1469598103934665603
	for _, b := range []byte(id) {
		h = (h ^ int64(b)) * 1099511628211
	}
	return h
}
