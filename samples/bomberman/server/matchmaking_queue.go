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

// MatchmakingQueueID is the singleton id auto-loaded on every node
// (configured in YAML). Players send JoinQueue / LeaveQueue here; an
// internal tick periodically pops players and creates Match objects.
const MatchmakingQueueID = "MatchmakingQueue"

// MatchSpawnInterval is how often the queue tick fires. Kept lazier
// than the per-Match TickPeriod (10 Hz) since matchmaking doesn't need
// real-time precision and the work per tick involves cross-object
// reliable calls.
const MatchSpawnInterval = time.Second

// MatchPlayersPerSpawn is how many queued players the tick groups
// into one match when ≥ MinPlayersToStart are waiting. Capped at
// MaxPlayers so Match.AddPlayer never rejects on capacity.
const MatchPlayersPerSpawn = 4

// reliableCallFunc is the cross-object reliable-call hook. Production
// binds it to goverseapi.ReliableCallObject; tests can override
// MatchmakingQueue.reliableCall to capture invocations without
// standing up a real cluster.
type reliableCallFunc = func(ctx context.Context, callID, objectType, objectID, method string, request proto.Message) (proto.Message, goverse_pb.ReliableCallStatus, error)

// createObjectFunc is the object-creation hook. Production binds it
// to goverseapi.CreateObject; tests override to skip the real
// cluster.
type createObjectFunc = func(ctx context.Context, objType, objID string) (string, error)

// MatchmakingQueue is a singleton object (auto-loaded on every node;
// the leader actually services the queue). It holds the FIFO of
// waiting players, runs a tick loop that batches them into Matches,
// and exposes JoinQueue / LeaveQueue / QueueStatus RPCs.
type MatchmakingQueue struct {
	goverseapi.BaseObject

	mu             sync.Mutex
	queued         []queuedPlayer
	matchesSpawned int64

	stopCh   chan struct{}
	stopOnce sync.Once

	reliableCall reliableCallFunc
	createObject createObjectFunc
}

type queuedPlayer struct {
	playerID string
	clientID string
}

func (q *MatchmakingQueue) OnCreated() {
	q.stopCh = make(chan struct{})
	if q.reliableCall == nil {
		q.reliableCall = goverseapi.ReliableCallObject
	}
	if q.createObject == nil {
		q.createObject = goverseapi.CreateObject
	}
	q.Logger.Infof("MatchmakingQueue %s ready (spawn interval %s, %d players per match)",
		q.Id(), MatchSpawnInterval, MatchPlayersPerSpawn)
	go q.spawnLoop()
}

func (q *MatchmakingQueue) Stop() {
	q.stopOnce.Do(func() { close(q.stopCh) })
}

// JoinQueue appends a player to the FIFO. Idempotent: a player already
// queued gets ok=true with their existing position, not a duplicate
// entry. The connecting client_id is captured from ctx so the spawned
// match knows where to push snapshots.
func (q *MatchmakingQueue) JoinQueue(ctx context.Context, req *pb.JoinQueueRequest) (*pb.JoinQueueResponse, error) {
	if req.PlayerId == "" {
		return &pb.JoinQueueResponse{Ok: false, Reason: "player_id is required"}, nil
	}
	clientID := goverseapi.CallerClientID(ctx)
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, qp := range q.queued {
		if qp.playerID == req.PlayerId {
			return &pb.JoinQueueResponse{Ok: true, QueuePosition: int32(i + 1)}, nil
		}
	}
	q.queued = append(q.queued, queuedPlayer{playerID: req.PlayerId, clientID: clientID})
	return &pb.JoinQueueResponse{Ok: true, QueuePosition: int32(len(q.queued))}, nil
}

// LeaveQueue removes a player from the FIFO if present. Returns ok
// regardless — a no-op leave is valid (player may already have been
// matched into a Match by a tick that ran between their decision to
// leave and this RPC arriving).
func (q *MatchmakingQueue) LeaveQueue(ctx context.Context, req *pb.LeaveQueueRequest) (*pb.LeaveQueueResponse, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, qp := range q.queued {
		if qp.playerID == req.PlayerId {
			q.queued = append(q.queued[:i], q.queued[i+1:]...)
			break
		}
	}
	return &pb.LeaveQueueResponse{Ok: true}, nil
}

func (q *MatchmakingQueue) QueueStatus(ctx context.Context, req *pb.QueueStatusRequest) (*pb.QueueStatusResponse, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return &pb.QueueStatusResponse{Queued: int32(len(q.queued)), MatchesSpawned: q.matchesSpawned}, nil
}

func (q *MatchmakingQueue) spawnLoop() {
	ticker := time.NewTicker(MatchSpawnInterval)
	defer ticker.Stop()
	objDone := q.Context().Done()
	for {
		select {
		case <-q.stopCh:
			return
		case <-objDone:
			return
		case <-ticker.C:
			q.spawnIfReady()
		}
	}
}

// spawnIfReady pulls up to MatchPlayersPerSpawn players off the front
// of the queue when ≥ MinPlayersToStart are waiting, then issues the
// reliable-call sequence to materialise a Match. Exposed package-
// private so unit tests can drive a single spawn deterministically.
func (q *MatchmakingQueue) spawnIfReady() {
	q.mu.Lock()
	if len(q.queued) < MinPlayersToStart {
		q.mu.Unlock()
		return
	}
	n := len(q.queued)
	if n > MatchPlayersPerSpawn {
		n = MatchPlayersPerSpawn
	}
	batch := append([]queuedPlayer(nil), q.queued[:n]...)
	q.queued = q.queued[n:]
	q.matchesSpawned++
	matchSeq := q.matchesSpawned
	q.mu.Unlock()

	matchID := fmt.Sprintf("Match-%s-%d", q.Id(), matchSeq)
	q.Logger.Infof("Spawning %s with %d players", matchID, len(batch))
	if err := q.spawnMatch(matchID, batch); err != nil {
		q.Logger.Errorf("Failed to spawn %s: %v", matchID, err)
	}
}

// spawnMatch creates the Match object and reliably adds each player +
// starts the match. Failures are logged but not retried within this
// method — a follow-up spawn tick will not pick the same players up
// again because they were already removed from the queue, but reliable-
// call dedup means the operations themselves are safe to retry by an
// external operator if needed.
func (q *MatchmakingQueue) spawnMatch(matchID string, batch []queuedPlayer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := q.createObject(ctx, "Match", matchID); err != nil {
		return fmt.Errorf("CreateObject Match %s: %w", matchID, err)
	}

	spawns := defaultSpawnPositions()
	for i, qp := range batch {
		spawn := spawns[i%len(spawns)]
		req := &pb.AddPlayerRequest{PlayerId: qp.playerID, SpawnX: int32(spawn[0]), SpawnY: int32(spawn[1])}
		callID := fmt.Sprintf("queue-%s-add-%s-%s", q.Id(), matchID, qp.playerID)
		// AddPlayer is invoked here from the queue — the server-side
		// recorded client_id will be the queue object's callcontext,
		// not the original player's. Carrying the player's client_id
		// across actors is a future enhancement (PR 4 on web UI / PR 5
		// on stress test); for now the queue logs the mapping so the
		// stress test driver can wire push routing externally.
		if _, _, err := q.reliableCall(ctx, callID, "Match", matchID, "AddPlayer", req); err != nil {
			q.Logger.Errorf("Reliable AddPlayer(%s, %s): %v", matchID, qp.playerID, err)
		}
	}

	startCallID := fmt.Sprintf("queue-%s-start-%s", q.Id(), matchID)
	if _, _, err := q.reliableCall(ctx, startCallID, "Match", matchID, "StartMatch", &pb.StartMatchRequest{}); err != nil {
		return fmt.Errorf("Reliable StartMatch %s: %w", matchID, err)
	}
	return nil
}

// defaultSpawnPositions exposes the static spawn list without
// allocating a MatchState. Mirrors MatchState.SpawnPositions().
func defaultSpawnPositions() [][2]int {
	tmp := NewMatchState(0)
	return tmp.SpawnPositions()
}
