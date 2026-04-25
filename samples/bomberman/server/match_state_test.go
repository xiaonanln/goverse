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

// TestNewMatchState_AllCornersHaveEscapeRoutes is a regression for the
// asymmetric L-pocket bug: every corner spawn must have at least two
// reachable empty neighbors so the player isn't trapped at start.
func TestNewMatchState_AllCornersHaveEscapeRoutes(t *testing.T) {
	s := NewMatchState(1)
	corners := [][2]int{{1, 1}, {GridWidth - 2, 1}, {1, GridHeight - 2}, {GridWidth - 2, GridHeight - 2}}
	for _, c := range corners {
		empties := 0
		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if s.tileAt(c[0]+d[0], c[1]+d[1]) == TileEmpty {
				empties++
			}
		}
		if empties < 2 {
			t.Fatalf("corner (%d,%d) only has %d empty neighbors; player is trapped", c[0], c[1], empties)
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

func TestSpawnPositions_AreUniqueAndInBounds(t *testing.T) {
	s := NewMatchState(1)
	positions := s.SpawnPositions()
	if len(positions) < MaxPlayers {
		t.Fatalf("need at least %d spawn positions, got %d", MaxPlayers, len(positions))
	}
	seen := make(map[[2]int]bool)
	for _, p := range positions {
		if p[0] <= 0 || p[0] >= GridWidth-1 || p[1] <= 0 || p[1] >= GridHeight-1 {
			t.Fatalf("spawn (%d,%d) is on or outside the border", p[0], p[1])
		}
		if seen[p] {
			t.Fatalf("duplicate spawn position (%d,%d)", p[0], p[1])
		}
		seen[p] = true
	}
}

func TestTileAt_OutOfBoundsReturnsWall(t *testing.T) {
	s := NewMatchState(1)
	for _, c := range [][2]int{{-1, 0}, {0, -1}, {GridWidth, 0}, {0, GridHeight}} {
		if got := s.tileAt(c[0], c[1]); got != TileWall {
			t.Fatalf("out-of-bounds tile (%d,%d) = %v; want wall", c[0], c[1], got)
		}
	}
}

func TestSetTile_OutOfBoundsIsNoop(t *testing.T) {
	s := NewMatchState(1)
	// Should not panic and should not mutate anything in-bounds.
	s.setTile(-1, -1, TileBlock)
	s.setTile(GridWidth, GridHeight, TileBlock)
	for i, want := range s.Tiles {
		got := NewMatchState(1).Tiles[i]
		if got != want {
			t.Fatalf("setTile out-of-bounds altered cell %d", i)
		}
	}
}

func TestRandomPowerup_CoversAllKinds(t *testing.T) {
	// randomPowerup picks one of 3 kinds; sample 1000 times and assert
	// each appears at least once. Probability of missing any is
	// (2/3)^1000 ≈ 0, well under any flakiness threshold.
	s := NewMatchState(42)
	seen := map[PowerupKind]bool{}
	for i := 0; i < 1000; i++ {
		seen[randomPowerup(s.rng)] = true
	}
	for _, want := range []PowerupKind{PowerupBomb, PowerupPower, PowerupSpeed} {
		if !seen[want] {
			t.Fatalf("randomPowerup never produced %v in 1000 draws", want)
		}
	}
}

func TestCanEnter_BlockedByOtherPlayer(t *testing.T) {
	s := twoPlayerStarted(t)
	// Move p2 onto (2,1) (cleared corner-pocket cell next to p1).
	s.Players["p2"].X, s.Players["p2"].Y = 2, 1
	if err := s.QueueInput("p1", Input{Move: pb.Direction_DIR_RIGHT}); err != nil {
		t.Fatal(err)
	}
	s.AdvanceTick()
	if p := s.Players["p1"]; p.X != 1 || p.Y != 1 {
		t.Fatalf("p1 walked through p2: now (%d,%d)", p.X, p.Y)
	}
}

func TestPlaceBomb_RespectsCapacity(t *testing.T) {
	s := twoPlayerStarted(t)
	// Default bomb capacity is 1: placing twice on the same player
	// without the first having exploded should leave only one bomb.
	s.QueueInput("p1", Input{PlaceBomb: true})
	s.AdvanceTick()
	if len(s.Bombs) != 1 {
		t.Fatalf("first place: %d bombs", len(s.Bombs))
	}
	// Walk away one cell so the placer isn't standing on a bomb.
	s.QueueInput("p1", Input{Move: pb.Direction_DIR_RIGHT})
	s.AdvanceTick()
	// Try to place a second bomb — should be rejected by capacity check.
	s.QueueInput("p1", Input{PlaceBomb: true})
	s.AdvanceTick()
	if len(s.Bombs) != 1 {
		t.Fatalf("second place over capacity should be rejected, have %d bombs", len(s.Bombs))
	}
}

// TestSpeedPowerup_IncreasesCellsPerTick verifies that picking up a
// SPEED powerup actually translates to more movement per tick — the
// proto and powerup pickup logic both bumped p.Speed but applyInputs
// previously ignored it, so the field had no gameplay effect.
func TestSpeedPowerup_IncreasesCellsPerTick(t *testing.T) {
	s := twoPlayerStarted(t)
	// Carve a long horizontal corridor for p1 to walk down.
	for x := 2; x <= 6; x++ {
		s.setTile(x, 1, TileEmpty)
	}
	s.Players["p1"].Speed = 3
	if err := s.QueueInput("p1", Input{Move: pb.Direction_DIR_RIGHT}); err != nil {
		t.Fatal(err)
	}
	s.AdvanceTick()
	if p := s.Players["p1"]; p.X != 4 || p.Y != 1 {
		t.Fatalf("p1 with Speed=3 should be at (4,1), got (%d,%d)", p.X, p.Y)
	}
}

// TestSpeedPowerup_StopsAtObstacle ensures higher speed never lets a
// player teleport past an intervening wall.
func TestSpeedPowerup_StopsAtObstacle(t *testing.T) {
	s := twoPlayerStarted(t)
	// (3,1) is a destructible block, blocks p1 mid-stride.
	s.Players["p1"].Speed = 5
	if err := s.QueueInput("p1", Input{Move: pb.Direction_DIR_RIGHT}); err != nil {
		t.Fatal(err)
	}
	s.AdvanceTick()
	if p := s.Players["p1"]; p.X != 2 || p.Y != 1 {
		t.Fatalf("p1 with Speed=5 should stop at (2,1) before the (3,1) block, got (%d,%d)", p.X, p.Y)
	}
}

func TestQueueInput_DeadPlayerIsSilent(t *testing.T) {
	s := twoPlayerStarted(t)
	s.Players["p1"].Alive = false
	if err := s.QueueInput("p1", Input{Move: pb.Direction_DIR_RIGHT}); err != nil {
		t.Fatalf("queueing for a dead player should be silent, got %v", err)
	}
	if _, queued := s.pendingInputs["p1"]; queued {
		t.Fatal("dead player's input should not be queued")
	}
}

func TestQueueInput_UnknownPlayer(t *testing.T) {
	s := twoPlayerStarted(t)
	if err := s.QueueInput("ghost", Input{Move: pb.Direction_DIR_RIGHT}); err == nil {
		t.Fatal("expected error for unknown player")
	}
}

// TestChainDetonation_DoesNotBurnExtraFuse pins the fix for the
// "every chain reaction takes a tick off all unrelated bombs" bug.
// One bomb is set to detonate this tick; a second, unrelated bomb is
// far away and should lose exactly one tick of fuse — not two.
func TestChainDetonation_DoesNotBurnExtraFuse(t *testing.T) {
	s := twoPlayerStarted(t)
	s.setTile(5, 5, TileEmpty)
	s.setTile(9, 5, TileEmpty)
	s.Bombs = append(s.Bombs,
		&Bomb{X: 5, Y: 5, OwnerID: "p1", Power: 1, TicksRemaining: 1},  // detonates this tick
		&Bomb{X: 9, Y: 5, OwnerID: "p2", Power: 1, TicksRemaining: 10}, // far away
	)
	s.Players["p1"].ActiveBombs = 1
	s.Players["p2"].ActiveBombs = 1
	s.AdvanceTick()
	var distantFuse int = -1
	for _, b := range s.Bombs {
		if b.X == 9 && b.Y == 5 {
			distantFuse = b.TicksRemaining
		}
	}
	if distantFuse != 9 {
		t.Fatalf("distant bomb fuse = %d after one tick with a chain elsewhere; want 9", distantFuse)
	}
}

func TestChainDetonation_TriggersAdjacentBomb(t *testing.T) {
	s := twoPlayerStarted(t)
	// Two bombs side-by-side: one with a 1-tick fuse, the other
	// with a long fuse. When the first explodes, the second is
	// caught in its blast and must detonate the same tick.
	s.setTile(5, 5, TileEmpty)
	s.setTile(6, 5, TileEmpty)
	s.Bombs = append(s.Bombs,
		&Bomb{X: 5, Y: 5, OwnerID: "p1", Power: 2, TicksRemaining: 1},
		&Bomb{X: 6, Y: 5, OwnerID: "p1", Power: 1, TicksRemaining: BombFuseTicks},
	)
	s.Players["p1"].ActiveBombs = 2
	s.AdvanceTick()
	if len(s.Bombs) != 0 {
		t.Fatalf("both bombs should have detonated, %d remain", len(s.Bombs))
	}
}

func TestExplosion_DestroysExistingPowerup(t *testing.T) {
	s := twoPlayerStarted(t)
	s.setTile(5, 5, TileEmpty)
	s.Powerups = append(s.Powerups, &Powerup{X: 5, Y: 5, Kind: PowerupBomb})
	s.Bombs = append(s.Bombs, &Bomb{X: 5, Y: 5, OwnerID: "p1", Power: 1, TicksRemaining: 1})
	s.Players["p1"].ActiveBombs = 1
	s.AdvanceTick()
	for _, p := range s.Powerups {
		if p.X == 5 && p.Y == 5 {
			t.Fatal("powerup should have been destroyed by the blast")
		}
	}
}

func TestTimeLimit_EndsMatchAsDraw(t *testing.T) {
	s := twoPlayerStarted(t)
	s.Tick = MatchTimeLimit
	s.AdvanceTick()
	if s.Status != pb.MatchStatus_MATCH_STATUS_ENDED {
		t.Fatalf("status = %v; want ENDED", s.Status)
	}
	if s.WinnerID != "" {
		t.Fatalf("draw should leave winner empty, got %q", s.WinnerID)
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
