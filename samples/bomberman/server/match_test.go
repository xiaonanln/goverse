package main

import (
	"context"
	"testing"

	pb "github.com/xiaonanln/goverse/samples/bomberman/proto"
)

// newMatchForTest constructs a Match with a populated state but
// without goverseapi's BaseObject lifecycle. This lets us drive the
// RPC wrappers directly in unit tests.
func newMatchForTest(t *testing.T, seed int64) *Match {
	t.Helper()
	return &Match{state: NewMatchState(seed)}
}

func TestMatchRPC_AddPlayer_AcceptsAndRejects(t *testing.T) {
	m := newMatchForTest(t, 1)
	ctx := context.Background()

	resp, err := m.AddPlayer(ctx, &pb.AddPlayerRequest{PlayerId: "p1", SpawnX: 1, SpawnY: 1})
	if err != nil || !resp.Ok {
		t.Fatalf("first AddPlayer should succeed: ok=%v reason=%q err=%v", resp.Ok, resp.Reason, err)
	}
	// Same id rejected.
	resp, _ = m.AddPlayer(ctx, &pb.AddPlayerRequest{PlayerId: "p1", SpawnX: 11, SpawnY: 9})
	if resp.Ok {
		t.Fatalf("duplicate AddPlayer should fail")
	}
	// Spawn onto a wall is rejected.
	resp, _ = m.AddPlayer(ctx, &pb.AddPlayerRequest{PlayerId: "p2", SpawnX: 0, SpawnY: 0})
	if resp.Ok {
		t.Fatalf("AddPlayer onto wall should fail; reason=%q", resp.Reason)
	}
}

func TestMatchRPC_StartMatch_RequiresPlayers(t *testing.T) {
	m := newMatchForTest(t, 1)
	ctx := context.Background()
	resp, _ := m.StartMatch(ctx, &pb.StartMatchRequest{})
	if resp.Ok {
		t.Fatal("StartMatch with no players should fail")
	}
	_, _ = m.AddPlayer(ctx, &pb.AddPlayerRequest{PlayerId: "p1", SpawnX: 1, SpawnY: 1})
	_, _ = m.AddPlayer(ctx, &pb.AddPlayerRequest{PlayerId: "p2", SpawnX: 11, SpawnY: 9})
	resp, _ = m.StartMatch(ctx, &pb.StartMatchRequest{})
	if !resp.Ok {
		t.Fatalf("StartMatch with two players should succeed: %s", resp.Reason)
	}
	// Calling Start again is a no-op-with-reason rather than an error.
	resp, _ = m.StartMatch(ctx, &pb.StartMatchRequest{})
	if resp.Ok {
		t.Fatal("StartMatch on already-running match should report ok=false")
	}
}

func TestMatchRPC_HandleInput_UnknownPlayer(t *testing.T) {
	m := newMatchForTest(t, 1)
	ctx := context.Background()
	_, _ = m.AddPlayer(ctx, &pb.AddPlayerRequest{PlayerId: "p1", SpawnX: 1, SpawnY: 1})
	_, _ = m.AddPlayer(ctx, &pb.AddPlayerRequest{PlayerId: "p2", SpawnX: 11, SpawnY: 9})
	_, _ = m.StartMatch(ctx, &pb.StartMatchRequest{})

	resp, _ := m.HandleInput(ctx, &pb.PlayerInputRequest{
		PlayerId: "ghost", Move: pb.Direction_DIR_RIGHT,
	})
	if resp.Ok {
		t.Fatal("HandleInput for unknown player should report ok=false")
	}
	resp, _ = m.HandleInput(ctx, &pb.PlayerInputRequest{
		PlayerId: "p1", Move: pb.Direction_DIR_RIGHT, PlaceBomb: true,
	})
	if !resp.Ok {
		t.Fatalf("HandleInput for valid player should succeed: %s", resp.Reason)
	}
}

func TestMatchRPC_GetSnapshot_ReturnsCurrentState(t *testing.T) {
	m := newMatchForTest(t, 1)
	ctx := context.Background()
	_, _ = m.AddPlayer(ctx, &pb.AddPlayerRequest{PlayerId: "p1", SpawnX: 1, SpawnY: 1})
	_, _ = m.AddPlayer(ctx, &pb.AddPlayerRequest{PlayerId: "p2", SpawnX: 11, SpawnY: 9})
	resp, err := m.GetSnapshot(ctx, &pb.GetSnapshotRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Snapshot == nil {
		t.Fatal("snapshot is nil")
	}
	if resp.Snapshot.Status != pb.MatchStatus_MATCH_STATUS_LOBBY {
		t.Fatalf("expected lobby status, got %v", resp.Snapshot.Status)
	}
	if len(resp.Snapshot.Players) != 2 {
		t.Fatalf("snapshot should report 2 players, got %d", len(resp.Snapshot.Players))
	}
}

func TestSeedFromID_Deterministic(t *testing.T) {
	a := seedFromID("match-42")
	b := seedFromID("match-42")
	c := seedFromID("match-43")
	if a != b {
		t.Fatalf("same id should produce same seed: %d vs %d", a, b)
	}
	if a == c {
		t.Fatal("different ids should generally produce different seeds")
	}
	// Empty id should still hash deterministically (no panic, stable).
	if seedFromID("") != seedFromID("") {
		t.Fatal("empty id seed should be stable")
	}
}
