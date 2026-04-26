package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/xiaonanln/goverse/goverseapi"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	pb "github.com/xiaonanln/goverse/samples/bomberman/proto"
)

// reliableCallFunc is the cross-object reliable-call hook used for
// the one place in this sample that truly needs exactly-once
// semantics: Match.recordResults → Player.RecordMatchResult, where
// retries on a permanent stat record would otherwise turn one win
// into many. Production binds it to goverseapi.ReliableCallObject;
// tests override Match.reliableCall to capture invocations without
// a real cluster.
type reliableCallFunc = func(ctx context.Context, callID, objectType, objectID, method string, request proto.Message) (proto.Message, goverse_pb.ReliableCallStatus, error)

// TickPeriod is the wall-clock interval between authoritative match
// updates. Derived from TickHz so the constant stays in one place.
const TickPeriod = time.Second / TickHz

// pushFunc is the broadcast hook. Production binds it to
// goverseapi.PushMessageToClients; tests can override the package var
// to capture pushes without spinning up a real cluster.
type pushFunc = func(ctx context.Context, clientIDs []string, message proto.Message) error

// Match is the goverse object wrapper. It owns the tick goroutine and
// the per-tick broadcast of MatchSnapshot to every connected player's
// client_id.
type Match struct {
	goverseapi.BaseObject

	mu       sync.Mutex
	state    *MatchState
	stopCh   chan struct{}
	stopOnce sync.Once

	// push is overridable for tests. When nil (production), OnCreated
	// wires it to goverseapi.PushMessageToClients.
	push pushFunc

	// reliableCall is overridable for tests. When nil (production),
	// OnCreated wires it to goverseapi.ReliableCallObject so match-end
	// records are persisted exactly once per (match, player).
	reliableCall reliableCallFunc

	// resultsRecorded guards against firing the per-player record-
	// match-result reliable calls more than once even if the tick
	// observing ENDED ran twice for some reason. Reliable-call dedup
	// already protects the Player side; this is just a local short-
	// circuit so we don't waste calls.
	resultsRecorded bool
}

func (m *Match) OnCreated() {
	m.state = NewMatchState(seedFromID(m.Id()))
	m.stopCh = make(chan struct{})
	if m.push == nil {
		m.push = goverseapi.PushMessageToClients
	}
	if m.reliableCall == nil {
		m.reliableCall = goverseapi.ReliableCallObject
	}
	m.Logger.Infof("Match %s created", m.Id())
	go m.tickLoop()
}

// Stop ends the tick goroutine. Idempotent. Called when the match has
// reached MATCH_STATUS_ENDED, and exposed for tests / future
// shard-migration teardown paths.
func (m *Match) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
}

func (m *Match) tickLoop() {
	ticker := time.NewTicker(TickPeriod)
	defer ticker.Stop()
	// Watch the object's lifetime context as well as the local stopCh so
	// the tick goroutine exits cleanly when goverse destroys the object
	// (shard migration, node shutdown, manual delete) — not only when
	// the match reaches MATCH_STATUS_ENDED on its own. Without this,
	// stale snapshots could keep being pushed after ownership has
	// already moved away from this node.
	objDone := m.Context().Done()
	for {
		select {
		case <-m.stopCh:
			return
		case <-objDone:
			return
		case <-ticker.C:
			if !m.tickAndBroadcastOnce() {
				m.Stop()
				return
			}
		}
	}
}

// tickAndBroadcastOnce advances the match by one tick (no-op while
// the match is in lobby), builds a snapshot, and pushes it to every
// connected client. Returns false once the match has ended, signalling
// the tick loop to exit. Exposed package-private so unit tests can
// drive the broadcast deterministically without waiting on time.Ticker.
func (m *Match) tickAndBroadcastOnce() bool {
	m.mu.Lock()
	switch m.state.Status {
	case pb.MatchStatus_MATCH_STATUS_LOBBY:
		// In lobby — broadcast the current player list every tick so
		// joining clients see each other arrive without polling, but
		// don't run the simulation.
	case pb.MatchStatus_MATCH_STATUS_RUNNING:
		m.state.AdvanceTick()
	case pb.MatchStatus_MATCH_STATUS_ENDED:
		m.mu.Unlock()
		return false
	}
	clientIDs := append([]string(nil), m.state.ClientIDs()...)
	snap := m.state.Snapshot(m.Id())
	ended := m.state.Status == pb.MatchStatus_MATCH_STATUS_ENDED
	winnerID := m.state.WinnerID
	var results []playerResult
	if ended && !m.resultsRecorded {
		results = collectMatchResults(m.state)
		m.resultsRecorded = true
	}
	m.mu.Unlock()

	m.broadcast(clientIDs, snap)
	if len(results) > 0 {
		// Fire the per-player reliable result calls in a goroutine so
		// the tick loop isn't blocked on cross-object RPCs. Reliable-
		// call dedup ensures retries (this goroutine, or any external
		// retry) never double-count.
		go m.recordResults(results, winnerID)
	}
	return !ended
}

type playerResult struct {
	PlayerID string
	Won      bool
}

// collectMatchResults builds the (PlayerID, Won) tuples that get fired
// to each Player on match end. Caller holds m.mu.
func collectMatchResults(s *MatchState) []playerResult {
	out := make([]playerResult, 0, len(s.Players))
	for _, p := range s.Players {
		out = append(out, playerResult{PlayerID: p.ID, Won: p.ID == s.WinnerID})
	}
	return out
}

// recordResults issues a reliable RecordMatchResult call into each
// Player object, retrying each call until it succeeds or the local
// retry budget is exhausted. Reliable-call dedup is keyed on the
// stable per-player call_id, so any retry that hits an already-
// landed call returns the cached result rather than double-counting.
//
// Without retry, a single transient failure (timeout, brief routing
// hiccup) would silently drop the stat write — and unlike orchestration
// hops, this one *is* permanent reward state, so eating the loss is
// the worst-case outcome we have to design against.
//
// Sequential rather than concurrent — a typical match has at most
// MaxPlayers participants, so the wall-clock cost is bounded and the
// simpler control flow makes test invariants easier to assert.
func (m *Match) recordResults(results []playerResult, winnerID string) {
	if m.reliableCall == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), recordResultsBudget)
	defer cancel()
	matchID := m.Id()
	for _, r := range results {
		callID := fmt.Sprintf("match-%s-record-%s", matchID, r.PlayerID)
		req := &pb.RecordMatchResultRequest{MatchId: matchID, Won: r.Won}
		if err := m.recordOnePlayerResultWithRetry(ctx, callID, r, req); err != nil {
			m.Logger.Errorf("Reliable RecordMatchResult(%s, %s won=%v) gave up after retries: %v",
				matchID, r.PlayerID, r.Won, err)
		}
	}
	m.Logger.Infof("Match %s ended (winner=%q), recorded results for %d players", matchID, winnerID, len(results))
}

// recordResultsBudget caps the total time recordResults will spend
// retrying. Generous since this runs on the match-end path which
// isn't latency-sensitive.
const recordResultsBudget = 60 * time.Second

// recordResultsRetryDelays defines the exponential backoff schedule
// between RecordMatchResult retries. After the last entry, we give
// up and log — the call_id is preserved in our log so an operator
// could replay it, and a future replayer would dedup naturally.
var recordResultsRetryDelays = []time.Duration{
	0, // first attempt
	200 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
}

func (m *Match) recordOnePlayerResultWithRetry(
	ctx context.Context, callID string, r playerResult, req *pb.RecordMatchResultRequest,
) error {
	var lastErr error
	for i, delay := range recordResultsRetryDelays {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		_, _, err := m.reliableCall(ctx, callID, "Player", "Player-"+r.PlayerID, "RecordMatchResult", req)
		if err == nil {
			return nil
		}
		lastErr = err
		m.Logger.Warnf("Reliable RecordMatchResult(%s, %s won=%v) attempt %d failed: %v",
			m.Id(), r.PlayerID, r.Won, i+1, err)
	}
	return lastErr
}

// broadcast pushes the snapshot to every client_id in this match.
// Failures are logged but not retried — the next tick will try again.
func (m *Match) broadcast(clientIDs []string, snap *pb.MatchSnapshot) {
	if len(clientIDs) == 0 || m.push == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := m.push(ctx, clientIDs, snap); err != nil {
		m.Logger.Warnf("Failed to push snapshot to %d clients: %v", len(clientIDs), err)
	}
}

// AddPlayer places a fresh player on the grid. The Match must still
// be in LOBBY status; spawn coordinates outside the grid or onto a
// non-empty cell are rejected. The caller's client id (taken from the
// request context) is recorded so subsequent snapshots are pushed to
// it.
func (m *Match) AddPlayer(ctx context.Context, req *pb.AddPlayerRequest) (*pb.AddPlayerResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	clientID := goverseapi.CallerClientID(ctx)
	if err := m.state.AddPlayer(req.PlayerId, clientID, int(req.SpawnX), int(req.SpawnY)); err != nil {
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
// every tick; production clients should rely on the push broadcast
// instead of polling, but the RPC is useful for one-shot checks (e.g.
// stress test verification).
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
