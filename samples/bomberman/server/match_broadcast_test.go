package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/xiaonanln/goverse/samples/bomberman/proto"
)

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

func TestTickLoop_RealTickerStopsOnEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real time.Ticker — slow")
	}
	rec := &recordedPush{}
	m := &Match{state: NewMatchState(1), push: rec.push, stopCh: make(chan struct{})}
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
