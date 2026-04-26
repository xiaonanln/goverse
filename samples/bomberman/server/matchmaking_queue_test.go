package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/xiaonanln/goverse/samples/bomberman/proto"
)

// recordedCallObject captures every (target object, method) pair plus
// the request so the queue test can assert the call sequence without
// standing up a real cluster.
type recordedCallObject struct {
	mu    sync.Mutex
	calls []callObjectInvocation
}

type callObjectInvocation struct {
	objectType string
	objectID   string
	method     string
	request    proto.Message
}

func (r *recordedCallObject) call(_ context.Context, objectType, objectID, method string, req proto.Message) (proto.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, callObjectInvocation{
		objectType: objectType, objectID: objectID, method: method,
		request: proto.Clone(req),
	})
	return nil, nil
}

func (r *recordedCallObject) snapshot() []callObjectInvocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]callObjectInvocation, len(r.calls))
	copy(out, r.calls)
	return out
}

func newQueueForTest(t *testing.T) (*MatchmakingQueue, *recordedCallObject) {
	t.Helper()
	rec := &recordedCallObject{}
	createStub := func(_ context.Context, _, objID string) (string, error) { return objID, nil }
	q := &MatchmakingQueue{
		callObject:   rec.call,
		createObject: createStub,
		stopCh:       make(chan struct{}),
	}
	q.OnInit(q, MatchmakingQueueID)
	return q, rec
}

func TestQueue_JoinIsIdempotent(t *testing.T) {
	q, _ := newQueueForTest(t)
	resp, err := q.JoinQueue(context.Background(), &pb.JoinQueueRequest{PlayerId: "alice"})
	if err != nil || !resp.Ok || resp.QueuePosition != 1 {
		t.Fatalf("first join: %+v err=%v", resp, err)
	}
	resp, _ = q.JoinQueue(context.Background(), &pb.JoinQueueRequest{PlayerId: "alice"})
	if !resp.Ok || resp.QueuePosition != 1 {
		t.Fatalf("duplicate join changed state: %+v", resp)
	}
	if got := len(q.queued); got != 1 {
		t.Fatalf("queue length after duplicate join = %d; want 1", got)
	}
}

func TestQueue_JoinRequiresPlayerID(t *testing.T) {
	q, _ := newQueueForTest(t)
	resp, _ := q.JoinQueue(context.Background(), &pb.JoinQueueRequest{})
	if resp.Ok {
		t.Fatal("empty player_id should be rejected")
	}
}

func TestQueue_LeaveIsNoOpIfAbsent(t *testing.T) {
	q, _ := newQueueForTest(t)
	resp, err := q.LeaveQueue(context.Background(), &pb.LeaveQueueRequest{PlayerId: "ghost"})
	if err != nil || !resp.Ok {
		t.Fatalf("leave on empty queue: %+v err=%v", resp, err)
	}
}

func TestQueue_LeaveRemovesPresentPlayer(t *testing.T) {
	q, _ := newQueueForTest(t)
	for _, name := range []string{"alice", "bob", "carol"} {
		_, _ = q.JoinQueue(context.Background(), &pb.JoinQueueRequest{PlayerId: name})
	}
	resp, err := q.LeaveQueue(context.Background(), &pb.LeaveQueueRequest{PlayerId: "bob"})
	if err != nil || !resp.Ok {
		t.Fatalf("leave: %+v err=%v", resp, err)
	}
	if got := len(q.queued); got != 2 {
		t.Fatalf("queue size after leave = %d; want 2", got)
	}
	for _, qp := range q.queued {
		if qp.playerID == "bob" {
			t.Fatal("bob still queued after leave")
		}
	}
}

func TestQueue_StopUnblocksSpawnLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real time.Ticker — slow")
	}
	q, _ := newQueueForTest(t)
	loopDone := make(chan struct{})
	go func() {
		q.spawnLoop()
		close(loopDone)
	}()
	q.Stop()
	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("spawnLoop did not exit within 2s of Stop()")
	}
	// Idempotent: a second Stop() must not panic.
	q.Stop()
}

func TestQueue_DestroyUnblocksSpawnLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real time.Ticker — slow")
	}
	q, _ := newQueueForTest(t)
	loopDone := make(chan struct{})
	go func() {
		q.spawnLoop()
		close(loopDone)
	}()
	q.Destroy()
	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("spawnLoop did not exit within 2s of Destroy()")
	}
}

func TestQueue_SpawnIfReady_BelowMinJustWaits(t *testing.T) {
	q, rec := newQueueForTest(t)
	_, _ = q.JoinQueue(context.Background(), &pb.JoinQueueRequest{PlayerId: "alice"})
	q.spawnIfReady()
	if got := q.matchesSpawned; got != 0 {
		t.Fatalf("matchesSpawned = %d; want 0 below MinPlayersToStart", got)
	}
	if got := len(rec.snapshot()); got != 0 {
		t.Fatalf("expected no calls below threshold, got %d", got)
	}
}

func TestQueue_SpawnIfReady_BatchesAndCalls(t *testing.T) {
	q, rec := newQueueForTest(t)
	for _, name := range []string{"alice", "bob", "carol"} {
		_, err := q.JoinQueue(context.Background(), &pb.JoinQueueRequest{PlayerId: name})
		if err != nil {
			t.Fatal(err)
		}
	}
	q.spawnIfReady()

	if got := q.matchesSpawned; got != 1 {
		t.Fatalf("matchesSpawned = %d; want 1", got)
	}
	if got := len(q.queued); got != 0 {
		t.Fatalf("queued after spawn = %d; want 0", got)
	}

	calls := rec.snapshot()
	// Expect 3 AddPlayer calls + 1 StartMatch call.
	if len(calls) != 4 {
		t.Fatalf("call count = %d; want 4", len(calls))
	}
	addPlayerSeen := map[string]bool{}
	startSeen := false
	for _, c := range calls {
		if c.objectType != "Match" {
			t.Fatalf("unexpected target object type %q in %+v", c.objectType, c)
		}
		switch c.method {
		case "AddPlayer":
			req := c.request.(*pb.AddPlayerRequest)
			addPlayerSeen[req.PlayerId] = true
		case "StartMatch":
			startSeen = true
		default:
			t.Fatalf("unexpected method %q", c.method)
		}
	}
	for _, want := range []string{"alice", "bob", "carol"} {
		if !addPlayerSeen[want] {
			t.Fatalf("no AddPlayer call for %q", want)
		}
	}
	if !startSeen {
		t.Fatal("no StartMatch call")
	}
}

func TestQueue_SpawnIfReady_CapsAtMatchPlayersPerSpawn(t *testing.T) {
	q, _ := newQueueForTest(t)
	// Queue one more than MatchPlayersPerSpawn — only the cap should
	// be popped; the leftover stays for the next tick.
	for i := 0; i <= MatchPlayersPerSpawn; i++ {
		_, _ = q.JoinQueue(context.Background(), &pb.JoinQueueRequest{PlayerId: "p" + string(rune('a'+i))})
	}
	q.spawnIfReady()
	if got := len(q.queued); got != 1 {
		t.Fatalf("leftover queued = %d; want 1 (cap is %d)", got, MatchPlayersPerSpawn)
	}
}

// TestQueue_SpawnFailureRequeuesPlayers regression: a transient
// CreateObject failure used to silently drop the popped batch
// because spawnIfReady removed them from q.queued before calling
// spawnMatch. Verify the players land back at the front of the
// queue so the next tick can retry them with a fresh matchID.
func TestQueue_SpawnFailureRequeuesPlayers(t *testing.T) {
	rec := &recordedCallObject{}
	failingCreate := func(_ context.Context, _, _ string) (string, error) {
		return "", errSpawnFailure
	}
	q := &MatchmakingQueue{
		callObject:   rec.call,
		createObject: failingCreate,
		stopCh:       make(chan struct{}),
	}
	q.OnInit(q, MatchmakingQueueID)

	for _, name := range []string{"alice", "bob", "carol"} {
		_, _ = q.JoinQueue(context.Background(), &pb.JoinQueueRequest{PlayerId: name})
	}
	q.spawnIfReady()

	if got := len(q.queued); got != 3 {
		t.Fatalf("queue size after failed spawn = %d; want 3 (all requeued)", got)
	}
	want := []string{"alice", "bob", "carol"}
	for i, qp := range q.queued {
		if qp.playerID != want[i] {
			t.Fatalf("requeued order[%d] = %q; want %q (FIFO must be preserved)", i, qp.playerID, want[i])
		}
	}
	if got := q.matchesSpawned; got != 1 {
		t.Fatalf("matchesSpawned = %d; want 1 (counter still increments to skip the failed id)", got)
	}
}

// errSpawnFailure is a deterministic transient error returned by the
// requeue test's failing createObject stub.
var errSpawnFailure = errFakeSpawn{msg: "transient: cluster unhealthy"}

type errFakeSpawn struct{ msg string }

func (e errFakeSpawn) Error() string { return e.msg }

// TestQueue_SpawnForwardsPlayerClientID is the regression that the
// queue → Match.AddPlayer hop carries the player's original
// gate-assigned client_id rather than the queue's own. Without it,
// matches spawned via matchmaking would push snapshots back to the
// queue object's stream instead of the players who joined.
func TestQueue_SpawnForwardsPlayerClientID(t *testing.T) {
	q, rec := newQueueForTest(t)
	for _, name := range []string{"alice", "bob"} {
		// JoinQueueRequest.client_id is the explicit override path
		// browsers will use after learning their SSE id.
		_, err := q.JoinQueue(context.Background(), &pb.JoinQueueRequest{
			PlayerId: name, ClientId: "gate1/" + name,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	q.spawnIfReady()

	calls := rec.snapshot()
	addPlayerByID := map[string]*pb.AddPlayerRequest{}
	for _, c := range calls {
		if c.method == "AddPlayer" {
			req := c.request.(*pb.AddPlayerRequest)
			addPlayerByID[req.PlayerId] = req
		}
	}
	for _, name := range []string{"alice", "bob"} {
		req, ok := addPlayerByID[name]
		if !ok {
			t.Fatalf("no AddPlayer call for %q", name)
		}
		if got, want := req.ClientId, "gate1/"+name; got != want {
			t.Fatalf("AddPlayer(%s).client_id = %q; want %q", name, got, want)
		}
	}
}

func TestQueue_QueueStatus_ReportsCounts(t *testing.T) {
	q, _ := newQueueForTest(t)
	_, _ = q.JoinQueue(context.Background(), &pb.JoinQueueRequest{PlayerId: "alice"})
	_, _ = q.JoinQueue(context.Background(), &pb.JoinQueueRequest{PlayerId: "bob"})
	resp, err := q.QueueStatus(context.Background(), &pb.QueueStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Queued != 2 || resp.MatchesSpawned != 0 {
		t.Fatalf("status = %+v; want Queued=2 MatchesSpawned=0", resp)
	}
}
