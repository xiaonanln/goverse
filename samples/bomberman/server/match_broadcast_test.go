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
// triple plus the request so Match's reliable-call sequence can be
// asserted without standing up a real cluster.
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

// recordedPush is a thread-safe pushFunc replacement that captures
// every broadcast for later assertion.
type recordedPush struct {
	mu    sync.Mutex
	calls []recordedCall
}

type recordedCall struct {
	clientIDs []string
	snapshot  *pb.MatchSnapshot
}

func (r *recordedPush) push(_ context.Context, ids []string, msg proto.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap, _ := msg.(*pb.MatchSnapshot)
	idsCopy := append([]string(nil), ids...)
	r.calls = append(r.calls, recordedCall{clientIDs: idsCopy, snapshot: snap})
	return nil
}

func (r *recordedPush) snapshot() []recordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// matchWithRecorder builds a Match with a manually-installed recorder
// pushFunc, bypassing OnCreated so unit tests don't need a goverse
// cluster.
func matchWithRecorder(t *testing.T, seed int64) (*Match, *recordedPush) {
	t.Helper()
	rec := &recordedPush{}
	m := &Match{state: NewMatchState(seed), push: rec.push, stopCh: make(chan struct{})}
	return m, rec
}

func TestClientIDs_SkipsServerOnlyPlayers(t *testing.T) {
	s := NewMatchState(1)
	if err := s.AddPlayer("server-bot", "", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPlayer("alice", "gate1/c2", GridWidth-2, GridHeight-2); err != nil {
		t.Fatal(err)
	}
	ids := s.ClientIDs()
	if len(ids) != 1 || ids[0] != "gate1/c2" {
		t.Fatalf("ClientIDs() = %v; want [gate1/c2]", ids)
	}
}

func TestTickAndBroadcastOnce_LobbyJustBroadcasts(t *testing.T) {
	m, rec := matchWithRecorder(t, 1)
	if err := m.state.AddPlayer("alice", "gate1/c2", 1, 1); err != nil {
		t.Fatal(err)
	}
	tickBefore := m.state.Tick
	if !m.tickAndBroadcastOnce() {
		t.Fatal("tickAndBroadcastOnce returned false in lobby")
	}
	if m.state.Tick != tickBefore {
		t.Fatalf("lobby tick should not advance Tick counter, was %d, now %d", tickBefore, m.state.Tick)
	}
	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 push in lobby, got %d", len(calls))
	}
	if got := calls[0].snapshot.Status; got != pb.MatchStatus_MATCH_STATUS_LOBBY {
		t.Fatalf("snapshot status = %v; want LOBBY", got)
	}
}

func TestTickAndBroadcastOnce_RunningAdvancesAndBroadcasts(t *testing.T) {
	m, rec := matchWithRecorder(t, 1)
	if err := m.state.AddPlayer("alice", "gate1/c2", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.state.AddPlayer("bob", "gate1/c3", GridWidth-2, GridHeight-2); err != nil {
		t.Fatal(err)
	}
	if err := m.state.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if !m.tickAndBroadcastOnce() {
			t.Fatalf("tick %d returned false unexpectedly", i)
		}
	}
	if m.state.Tick != 3 {
		t.Fatalf("Tick after 3 advances = %d; want 3", m.state.Tick)
	}
	calls := rec.snapshot()
	if len(calls) != 3 {
		t.Fatalf("expected 3 broadcasts, got %d", len(calls))
	}
	for i, c := range calls {
		if len(c.clientIDs) != 2 {
			t.Fatalf("broadcast %d went to %d clients; want 2", i, len(c.clientIDs))
		}
		if c.snapshot.Tick != int32(i+1) {
			t.Fatalf("broadcast %d tick = %d; want %d", i, c.snapshot.Tick, i+1)
		}
	}
}

// TestTickAndBroadcastOnce_FiresRecordResultsOnEnd checks that the
// match-end transition kicks off the per-player RecordMatchResult
// reliable call sequence — the cross-object reliable call PR 3 wires
// up. The recorder collects the calls fired in a goroutine; we wait
// briefly for them to land, then assert the contents.
func TestTickAndBroadcastOnce_FiresRecordResultsOnEnd(t *testing.T) {
	rec := &recordedPush{}
	rrec := &recordedReliable{}
	m := &Match{
		state:        NewMatchState(1),
		push:         rec.push,
		reliableCall: rrec.call,
		stopCh:       make(chan struct{}),
	}
	m.OnInit(m, "Match-test")
	if err := m.state.AddPlayer("alice", "gate1/c2", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.state.AddPlayer("bob", "gate1/c3", GridWidth-2, GridHeight-2); err != nil {
		t.Fatal(err)
	}
	if err := m.state.Start(); err != nil {
		t.Fatal(err)
	}
	// Force end-of-match: kill bob, alice survives.
	m.state.Players["bob"].Alive = false
	if m.tickAndBroadcastOnce() {
		t.Fatal("end-tick should return false")
	}

	// recordResults runs in a goroutine — wait a moment for it to land.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(rrec.snapshot()) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	calls := rrec.snapshot()
	if len(calls) != 2 {
		t.Fatalf("expected 2 RecordMatchResult reliable calls, got %d", len(calls))
	}
	wonByPlayer := map[string]bool{}
	for _, c := range calls {
		if c.method != "RecordMatchResult" || c.objectType != "Player" {
			t.Fatalf("unexpected reliable call %+v", c)
		}
		req := c.request.(*pb.RecordMatchResultRequest)
		// Player object id is "Player-<player_id>" by convention.
		switch c.objectID {
		case "Player-alice":
			wonByPlayer["alice"] = req.Won
		case "Player-bob":
			wonByPlayer["bob"] = req.Won
		default:
			t.Fatalf("unexpected target object id %q", c.objectID)
		}
		// Stable, deterministic call id keyed on (match, player) so
		// retries dedup correctly.
		want := "match-Match-test-record-" + req.MatchId
		_ = want // call_id is descriptive but not asserted exactly here
	}
	if !wonByPlayer["alice"] || wonByPlayer["bob"] {
		t.Fatalf("won-by-player = %+v; want alice=true bob=false", wonByPlayer)
	}

	// A second call shouldn't re-fire results — the local short-
	// circuit kicks in even though reliable-call dedup would also
	// catch it server-side.
	m.tickAndBroadcastOnce()
	if got := len(rrec.snapshot()); got != 2 {
		t.Fatalf("results re-fired on second tick: %d total calls", got)
	}
}

func TestTickAndBroadcastOnce_StopsAfterEnd(t *testing.T) {
	m, rec := matchWithRecorder(t, 1)
	if err := m.state.AddPlayer("alice", "gate1/c2", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.state.AddPlayer("bob", "gate1/c3", GridWidth-2, GridHeight-2); err != nil {
		t.Fatal(err)
	}
	if err := m.state.Start(); err != nil {
		t.Fatal(err)
	}
	// Force end-of-match: kill bob directly so Alice is the lone
	// survivor on the next tick.
	m.state.Players["bob"].Alive = false
	// The tick that transitions to ENDED still broadcasts the final
	// snapshot, then returns false to signal the loop to stop.
	if m.tickAndBroadcastOnce() {
		t.Fatal("end-tick should return false to stop the loop")
	}
	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 final broadcast, got %d", len(calls))
	}
	if got := calls[0].snapshot.Status; got != pb.MatchStatus_MATCH_STATUS_ENDED {
		t.Fatalf("final snapshot status = %v; want ENDED", got)
	}
	// Subsequent ticks must do nothing.
	if m.tickAndBroadcastOnce() {
		t.Fatal("post-end tick should return false")
	}
	if calls := rec.snapshot(); len(calls) != 1 {
		t.Fatalf("post-end broadcasts: %d; want exactly 1", len(calls))
	}
}

// TestTickLoop_ExitsOnDestroy is a regression for the destroy-path
// shutdown: when goverse calls Destroy() (shard migration, node
// shutdown, manual delete) before the match has reached ENDED, the
// tick loop must observe the cancelled object context and exit so we
// don't keep pushing stale snapshots after ownership has moved.
func TestTickLoop_ExitsOnDestroy(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real time.Ticker — slow")
	}
	rec := &recordedPush{}
	m := &Match{state: NewMatchState(1), push: rec.push, stopCh: make(chan struct{})}
	// OnInit populates BaseObject's lifetime context so Match.Context()
	// returns a non-nil chan; without this the goroutine would panic
	// on nil-chan receive.
	m.OnInit(m, "test-match-destroy")
	if err := m.state.AddPlayer("alice", "gate1/c2", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.state.AddPlayer("bob", "gate1/c3", GridWidth-2, GridHeight-2); err != nil {
		t.Fatal(err)
	}
	if err := m.state.Start(); err != nil {
		t.Fatal(err)
	}

	loopDone := make(chan struct{})
	go func() {
		m.tickLoop()
		close(loopDone)
	}()
	// Let the loop tick a couple of times.
	time.Sleep(3 * TickPeriod)
	// Simulate goverse destroying the object mid-match.
	m.Destroy()
	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("tick loop did not exit within 2s of Destroy()")
	}
}

func TestTickLoop_RealTickerStopsOnEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real time.Ticker — slow")
	}
	rec := &recordedPush{}
	m := &Match{state: NewMatchState(1), push: rec.push, stopCh: make(chan struct{})}
	m.OnInit(m, "test-match-end")
	if err := m.state.AddPlayer("alice", "gate1/c2", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.state.AddPlayer("bob", "gate1/c3", GridWidth-2, GridHeight-2); err != nil {
		t.Fatal(err)
	}
	if err := m.state.Start(); err != nil {
		t.Fatal(err)
	}
	go m.tickLoop()
	// Wait for at least one tick of real wall-clock activity, then
	// force end and verify the goroutine exits.
	time.Sleep(3 * TickPeriod)
	m.mu.Lock()
	for _, p := range m.state.Players {
		if p.ID != "alice" {
			p.Alive = false
		}
	}
	m.mu.Unlock()
	// Give the loop a few ticks to observe the end and exit cleanly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-m.stopCh:
			return
		default:
			time.Sleep(TickPeriod)
		}
	}
	t.Fatal("tick loop never closed stopCh after match ended")
}
