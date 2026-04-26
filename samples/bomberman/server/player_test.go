package main

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	pb "github.com/xiaonanln/goverse/samples/bomberman/proto"
)

// newPlayerForTest constructs a Player with BaseObject lifecycle wired
// up via OnInit, so tests can call methods that touch m.Logger /
// m.Id() without standing up a goverse cluster.
func newPlayerForTest(t *testing.T, id string) *Player {
	t.Helper()
	p := &Player{}
	p.OnInit(p, id)
	return p
}

func TestPlayer_OnCreated_PopulatesPlayerID(t *testing.T) {
	p := newPlayerForTest(t, "Player-alice")
	p.OnCreated()
	if p.stats.PlayerId != "Player-alice" {
		t.Fatalf("stats.PlayerId = %q; want Player-alice", p.stats.PlayerId)
	}
}

func TestPlayer_RecordMatchResult_AccumulatesWinsAndLosses(t *testing.T) {
	p := newPlayerForTest(t, "Player-alice")
	p.OnCreated()
	ctx := context.Background()

	resp, err := p.RecordMatchResult(ctx, &pb.RecordMatchResultRequest{MatchId: "m1", Won: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Stats; got.Wins != 1 || got.Losses != 0 || got.TotalMatches != 1 || got.LastMatchId != "m1" {
		t.Fatalf("after first win: %+v", got)
	}
	resp, err = p.RecordMatchResult(ctx, &pb.RecordMatchResultRequest{MatchId: "m2", Won: false})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Stats; got.Wins != 1 || got.Losses != 1 || got.TotalMatches != 2 || got.LastMatchId != "m2" {
		t.Fatalf("after one win and one loss: %+v", got)
	}
}

func TestPlayer_RecordMatchResult_RequiresMatchID(t *testing.T) {
	p := newPlayerForTest(t, "Player-alice")
	p.OnCreated()
	if _, err := p.RecordMatchResult(context.Background(), &pb.RecordMatchResultRequest{}); err == nil {
		t.Fatal("expected error for empty match_id")
	}
}

func TestPlayer_GetStats_ReturnsClone(t *testing.T) {
	p := newPlayerForTest(t, "Player-alice")
	p.OnCreated()
	_, _ = p.RecordMatchResult(context.Background(), &pb.RecordMatchResultRequest{MatchId: "m1", Won: true})
	resp, err := p.GetStats(context.Background(), &pb.GetPlayerStatsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// Mutating the returned stats must not affect future GetStats calls.
	resp.Stats.Wins = 999
	resp2, _ := p.GetStats(context.Background(), &pb.GetPlayerStatsRequest{})
	if resp2.Stats.Wins != 1 {
		t.Fatalf("internal state was mutated via returned stats; got Wins=%d", resp2.Stats.Wins)
	}
}

func TestPlayer_FromData_RestoresStats(t *testing.T) {
	p := newPlayerForTest(t, "Player-alice")
	saved := &pb.PlayerData{Stats: &pb.PlayerStats{
		PlayerId:     "Player-alice",
		TotalMatches: 5,
		Wins:         3,
		Losses:       2,
		LastMatchId:  "m-prev",
	}}
	if err := p.FromData(saved); err != nil {
		t.Fatal(err)
	}
	p.OnCreated()
	resp, _ := p.GetStats(context.Background(), &pb.GetPlayerStatsRequest{})
	if resp.Stats.TotalMatches != 5 || resp.Stats.Wins != 3 || resp.Stats.Losses != 2 || resp.Stats.LastMatchId != "m-prev" {
		t.Fatalf("FromData did not restore stats: %+v", resp.Stats)
	}
}

func TestPlayer_FromData_RoundTrips(t *testing.T) {
	p := newPlayerForTest(t, "Player-alice")
	p.OnCreated()
	for i := 0; i < 4; i++ {
		_, err := p.RecordMatchResult(context.Background(), &pb.RecordMatchResultRequest{
			MatchId: "m-roundtrip", Won: i%2 == 0,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	data, err := p.ToData()
	if err != nil {
		t.Fatal(err)
	}

	// Hydrate a fresh Player from the saved data and confirm all
	// fields match — this is the persistence contract that survives
	// shard migrations / node restarts.
	q := newPlayerForTest(t, "Player-alice")
	if err := q.FromData(data); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(p.stats, q.stats) {
		t.Fatalf("roundtrip mismatch:\nbefore: %+v\nafter:  %+v", p.stats, q.stats)
	}
}

func TestPlayer_FromData_RejectsWrongType(t *testing.T) {
	p := newPlayerForTest(t, "Player-alice")
	if err := p.FromData(&pb.PlayerStats{}); err == nil {
		t.Fatal("expected error for wrong proto type")
	}
}
