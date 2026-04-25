package main

import (
	"testing"

	pb "github.com/xiaonanln/goverse/samples/bomberman/proto"
)

func TestNewMatchState_LayoutHasCornersOpen(t *testing.T) {
	s := NewMatchState(1)
	corners := [][2]int{{1, 1}, {GridWidth - 2, 1}, {1, GridHeight - 2}, {GridWidth - 2, GridHeight - 2}}
	for _, c := range corners {
		if got := s.tileAt(c[0], c[1]); got != TileEmpty {
			t.Fatalf("corner (%d,%d) want empty, got %v", c[0], c[1], got)
		}
	}
}

func TestNewMatchState_BordersAreWalls(t *testing.T) {
	s := NewMatchState(1)
	for x := 0; x < GridWidth; x++ {
		if s.tileAt(x, 0) != TileWall || s.tileAt(x, GridHeight-1) != TileWall {
			t.Fatalf("row border at x=%d not wall", x)
		}
	}
	for y := 0; y < GridHeight; y++ {
		if s.tileAt(0, y) != TileWall || s.tileAt(GridWidth-1, y) != TileWall {
			t.Fatalf("col border at y=%d not wall", y)
		}
	}
}

func TestStart_RequiresMinPlayers(t *testing.T) {
	s := NewMatchState(1)
	if err := s.AddPlayer("p1", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err == nil {
		t.Fatal("expected Start to fail with 1 player")
	}
	if err := s.AddPlayer("p2", GridWidth-2, GridHeight-2); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("expected Start to succeed with 2 players: %v", err)
	}
	if s.Status != pb.MatchStatus_MATCH_STATUS_RUNNING {
		t.Fatalf("status = %v; want RUNNING", s.Status)
	}
}

// twoPlayerStarted spawns two players on opposite corners far enough
// apart that they don't interfere unless the test arranges it.
func twoPlayerStarted(t *testing.T) *MatchState {
	t.Helper()
	s := NewMatchState(1)
	if err := s.AddPlayer("p1", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPlayer("p2", GridWidth-2, GridHeight-2); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestQueueInput_MovesPlayer(t *testing.T) {
	s := twoPlayerStarted(t)
	if err := s.QueueInput("p1", Input{Move: pb.Direction_DIR_RIGHT}); err != nil {
		t.Fatal(err)
	}
	s.AdvanceTick()
	if p := s.Players["p1"]; p.X != 2 || p.Y != 1 {
		t.Fatalf("p1 at (%d,%d); want (2,1)", p.X, p.Y)
	}
}

func TestQueueInput_BlockedByBlock(t *testing.T) {
	s := twoPlayerStarted(t)
	// The corner pocket carves (1,1), (2,1), (1,2) empty, so verify the
	// next-cell-over (3,1) is a destructible block and that moving into
	// it is silently rejected.
	if got := s.tileAt(3, 1); got != TileBlock {
		t.Fatalf("expected block at (3,1), got %v", got)
	}
	// First step into the empty pocket cell (2,1).
	if err := s.QueueInput("p1", Input{Move: pb.Direction_DIR_RIGHT}); err != nil {
		t.Fatal(err)
	}
	s.AdvanceTick()
	if p := s.Players["p1"]; p.X != 2 || p.Y != 1 {
		t.Fatalf("p1 didn't reach the pocket extension: at (%d,%d)", p.X, p.Y)
	}
	// Second step would land on the (3,1) block — must be a no-op.
	if err := s.QueueInput("p1", Input{Move: pb.Direction_DIR_RIGHT}); err != nil {
		t.Fatal(err)
	}
	s.AdvanceTick()
	if p := s.Players["p1"]; p.X != 2 || p.Y != 1 {
		t.Fatalf("p1 walked into a block: now at (%d,%d)", p.X, p.Y)
	}
}

func TestPlaceBomb_FuseToExplosionToCleanup(t *testing.T) {
	s := twoPlayerStarted(t)
	// Drop a bomb manually in the middle of the grid where neither
	// spawn-corner player will be hit, so the lifecycle test isn't
	// short-circuited by a player death ending the match.
	s.setTile(5, 5, TileEmpty)
	s.Bombs = append(s.Bombs, &Bomb{X: 5, Y: 5, OwnerID: "p1", Power: 1, TicksRemaining: BombFuseTicks})
	s.Players["p1"].ActiveBombs = 1
	// Step the fuse down to zero.
	for i := 0; i < BombFuseTicks; i++ {
		s.AdvanceTick()
	}
	if len(s.Bombs) != 0 {
		t.Fatalf("bomb should have exploded after fuse, still %d", len(s.Bombs))
	}
	if len(s.Explosions) == 0 {
		t.Fatal("expected explosions after detonation")
	}
	// Owner regained their bomb slot.
	if p := s.Players["p1"]; p.ActiveBombs != 0 {
		t.Fatalf("active bombs should reset to 0, got %d", p.ActiveBombs)
	}
	// Explosions decay over ExplosionTicks ticks.
	for i := 0; i < ExplosionTicks; i++ {
		s.AdvanceTick()
	}
	if len(s.Explosions) != 0 {
		t.Fatalf("explosions should clear, got %d", len(s.Explosions))
	}
}

func TestExplosion_KillsPlayerAndEndsMatch(t *testing.T) {
	s := twoPlayerStarted(t)
	// Move p2 onto an inner cell adjacent to where the bomb will land
	// (5,5). Drop a bomb owned by p1 right next to p2 so the explosion
	// kills p2 but leaves p1 (in the far corner) alive — that gives us
	// a clean winner=p1 outcome for the assertion.
	s.setTile(5, 5, TileEmpty)
	s.setTile(6, 5, TileEmpty)
	s.Players["p2"].X, s.Players["p2"].Y = 6, 5
	s.Bombs = append(s.Bombs, &Bomb{X: 5, Y: 5, OwnerID: "p1", Power: 2, TicksRemaining: 1})
	s.AdvanceTick() // bomb detonates this tick
	if s.Players["p2"].Alive {
		t.Fatal("p2 should have died in the blast")
	}
	if !s.Players["p1"].Alive {
		t.Fatal("p1 should still be alive in the far corner")
	}
	if s.Status != pb.MatchStatus_MATCH_STATUS_ENDED {
		t.Fatalf("match should be ended, status=%v", s.Status)
	}
	if s.WinnerID != "p1" {
		t.Fatalf("winner = %q; want p1", s.WinnerID)
	}
}

func TestExplosion_StopsAtIndestructibleWall(t *testing.T) {
	s := NewMatchState(1)
	// Wall at (2,2) is indestructible per layout. Bomb at (2,1) with
	// power 5 should not damage past the wall going down.
	if err := s.AddPlayer("p1", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPlayer("p2", GridWidth-2, GridHeight-2); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	// Manually drop a bomb at (2,1) — but (2,1) is not in the corner
	// pocket: it's a destructible block. Clear it for the test.
	s.setTile(2, 1, TileEmpty)
	s.Bombs = append(s.Bombs, &Bomb{X: 2, Y: 1, OwnerID: "p1", Power: 5, TicksRemaining: 1})
	s.Players["p1"].ActiveBombs = 1
	s.AdvanceTick() // detonate
	for _, e := range s.Explosions {
		if e.X == 2 && e.Y == 3 {
			t.Fatal("explosion leaked past indestructible wall at (2,2)")
		}
	}
}

func TestBlockBlast_DropsAndCollectsPowerup(t *testing.T) {
	// With BlockDropPowerup=0.30, seed 1 will eventually produce one;
	// rather than depend on RNG luck, force a powerup directly and
	// drive the player onto it.
	s := twoPlayerStarted(t)
	s.Powerups = append(s.Powerups, &Powerup{X: 2, Y: 1, Kind: PowerupBomb})
	s.setTile(2, 1, TileEmpty)
	if err := s.QueueInput("p1", Input{Move: pb.Direction_DIR_RIGHT}); err != nil {
		t.Fatal(err)
	}
	s.AdvanceTick()
	if p := s.Players["p1"]; p.BombCapacity != DefaultBombCap+1 {
		t.Fatalf("bomb capacity = %d; want %d", p.BombCapacity, DefaultBombCap+1)
	}
	if len(s.Powerups) != 0 {
		t.Fatalf("powerup should have been consumed, %d remain", len(s.Powerups))
	}
}

func TestSnapshot_ReflectsState(t *testing.T) {
	s := twoPlayerStarted(t)
	if err := s.QueueInput("p1", Input{PlaceBomb: true}); err != nil {
		t.Fatal(err)
	}
	s.AdvanceTick()
	snap := s.Snapshot("test-match")
	if snap.MatchId != "test-match" {
		t.Fatalf("match_id = %q", snap.MatchId)
	}
	if snap.Width != GridWidth || snap.Height != GridHeight {
		t.Fatalf("snapshot dims = %dx%d", snap.Width, snap.Height)
	}
	if len(snap.Tiles) != GridWidth*GridHeight {
		t.Fatalf("tiles len = %d", len(snap.Tiles))
	}
	if len(snap.Players) != 2 {
		t.Fatalf("players = %d", len(snap.Players))
	}
	if len(snap.Bombs) != 1 {
		t.Fatalf("bombs = %d", len(snap.Bombs))
	}
}
