package main

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/xiaonanln/goverse/goverseapi"
	pb "github.com/xiaonanln/goverse/samples/bomberman/proto"
)

// Player is a per-user persistent stats object. The runtime saves
// the contents via ToData/FromData; clients query via GetStats and
// Match drives match-end stat updates via the RecordMatchResult
// reliable call.
type Player struct {
	goverseapi.BaseObject

	mu    sync.Mutex
	stats *pb.PlayerStats
}

func (p *Player) OnCreated() {
	if p.stats == nil {
		p.stats = &pb.PlayerStats{}
	}
	if p.stats.PlayerId == "" {
		// FromData wasn't called (fresh object) — initialise with the
		// object id so the stats record carries identity.
		p.stats.PlayerId = p.Id()
	}
	p.Logger.Infof("Player %s created (matches=%d wins=%d losses=%d)",
		p.Id(), p.stats.TotalMatches, p.stats.Wins, p.stats.Losses)
}

func (p *Player) ToData() (proto.Message, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stats == nil {
		return &pb.PlayerData{}, nil
	}
	// proto.Clone keeps the persisted blob independent of further
	// mutations; without it, the runtime might serialize a snapshot
	// that gets modified mid-flight.
	return &pb.PlayerData{Stats: proto.Clone(p.stats).(*pb.PlayerStats)}, nil
}

func (p *Player) FromData(data proto.Message) error {
	if data == nil {
		return nil
	}
	d, ok := data.(*pb.PlayerData)
	if !ok {
		return fmt.Errorf("Player.FromData: expected *PlayerData, got %T", data)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if d.Stats != nil {
		p.stats = proto.Clone(d.Stats).(*pb.PlayerStats)
	}
	return nil
}

// GetStats returns a copy of the current stats. Cheap.
func (p *Player) GetStats(ctx context.Context, req *pb.GetPlayerStatsRequest) (*pb.GetPlayerStatsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stats == nil {
		return &pb.GetPlayerStatsResponse{Stats: &pb.PlayerStats{PlayerId: p.Id()}}, nil
	}
	return &pb.GetPlayerStatsResponse{Stats: proto.Clone(p.stats).(*pb.PlayerStats)}, nil
}

// RecordMatchResult is the reliable-call entry point invoked by Match
// when a match ends. Reliable-call dedup ensures wins/losses cannot be
// double-counted even if the caller retries on a transient error.
//
// Returns the updated stats so the caller (typically a Match) can log
// the final state for observability.
func (p *Player) RecordMatchResult(ctx context.Context, req *pb.RecordMatchResultRequest) (*pb.RecordMatchResultResponse, error) {
	if req.MatchId == "" {
		return nil, fmt.Errorf("RecordMatchResult: match_id is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stats == nil {
		p.stats = &pb.PlayerStats{PlayerId: p.Id()}
	}
	p.stats.TotalMatches++
	if req.Won {
		p.stats.Wins++
	} else {
		p.stats.Losses++
	}
	p.stats.LastMatchId = req.MatchId
	return &pb.RecordMatchResultResponse{Stats: proto.Clone(p.stats).(*pb.PlayerStats)}, nil
}
