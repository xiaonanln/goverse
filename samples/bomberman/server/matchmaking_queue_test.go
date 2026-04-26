package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	pb "github.com/xiaonanln/goverse/samples/bomberman/proto"
)

// recordedReliable captures every (call_id, target object, method)
// triple plus the request message so tests can assert the reliable-
// call sequence MatchmakingQueue issues without standing up a real
// cluster.
type recordedReliable struct {
	mu    sync.Mutex
	calls []reliableInvocation
}

type reliableInvocation struct {
	callID     string
	objectType string
	objectID   string
	method     string
	request    proto.Message
}

func (r *recordedReliable) call(_ context.Context, callID, objectType, objectID, method string, req proto.Message) (proto.Message, goverse_pb.ReliableCallStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, reliableInvocation{
		callID: callID, objectType: objectType, objectID: objectID, method: method,
		request: proto.Clone(req),
	})
	return nil, goverse_pb.ReliableCallStatus_SUCCESS, nil
}

func (r *recordedReliable) snapshot() []reliableInvocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]reliableInvocation, len(r.calls))
	copy(out, r.calls)
	return out
}

func newQueueForTest(t *testing.T) (*MatchmakingQueue, *recordedReliable) {
	t.Helper()
	rec := &recordedReliable{}
	createStub := func(_ context.Context, _, objID string) (string, error) { return objID, nil }
	q := &MatchmakingQueue{
		reliableCall: rec.call,
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
		t.Fatalf("expected no reliable calls below threshold, got %d", got)
	}
}

func TestQueue_SpawnIfReady_BatchesAndReliablyAdds(t *testing.T) {
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
		t.Fatalf("reliable-call count = %d; want 4", len(calls))
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
			t.Fatalf("no AddPlayer reliable call for %q", want)
		}
	}
	if !startSeen {
		t.Fatal("no StartMatch reliable call")
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
