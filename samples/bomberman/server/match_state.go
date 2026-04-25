package main

import (
	"fmt"
	"math/rand"

	pb "github.com/xiaonanln/goverse/samples/bomberman/proto"
)

// Match grid + tick parameters. Numbers picked to feel like classic
// Super Bomberman: 13x11 grid, 10Hz tick, 2 s bomb fuse, 0.5 s
// explosion afterglow, 30 % drop chance per destroyed block.
const (
	GridWidth         = 13
	GridHeight        = 11
	TickHz            = 10
	BombFuseTicks     = 20
	ExplosionTicks    = 5
	MatchTimeLimit    = 1800 // 3 minutes at 10 Hz
	BlockDropPowerup  = 0.30
	DefaultBombCap    = 1
	DefaultBombPower  = 2
	DefaultSpeed      = 1
	MinPlayersToStart = 2
	MaxPlayers        = 8
)

// Tile values mirror the proto Tile enum so we can convert in O(1).
type Tile int8

const (
	TileEmpty Tile = iota
	TileWall
	TileBlock
)

type PowerupKind int8

const (
	PowerupBomb PowerupKind = iota
	PowerupPower
	PowerupSpeed
)

type Player struct {
	ID           string
	X, Y         int
	Alive        bool
	BombCapacity int
	BombPower    int
	Speed        int
	ActiveBombs  int
}

type Bomb struct {
	X, Y           int
	OwnerID        string
	Power          int
	TicksRemaining int
}

type Explosion struct {
	X, Y           int
	TicksRemaining int
}

type Powerup struct {
	X, Y int
	Kind PowerupKind
}

// Input is a single client intent queued for the next tick.
type Input struct {
	Move      pb.Direction
	PlaceBomb bool
}

// MatchState is the entire authoritative game state. All mutation goes
// through methods on this type; the goverse Match wrapper only adds
// locking + lifecycle. Pure (no goverse imports) so it's directly
// unit-testable.
type MatchState struct {
	Width, Height int
	Tiles         []Tile // row-major, len = Width*Height
	Players       map[string]*Player
	Bombs         []*Bomb
	Explosions    []*Explosion
	Powerups      []*Powerup
	Tick          int
	Status        pb.MatchStatus
	WinnerID      string

	pendingInputs map[string]*Input
	rng           *rand.Rand
}

// NewMatchState builds a fresh match in LOBBY status with the classic
// Bomberman layout: indestructible border + checkerboard pillars,
// destructible blocks fill everything else, four 3-cell L-shaped
// pockets in the corners kept empty so corner spawns aren't trapped.
func NewMatchState(seed int64) *MatchState {
	w, h := GridWidth, GridHeight
	s := &MatchState{
		Width:         w,
		Height:        h,
		Tiles:         make([]Tile, w*h),
		Players:       make(map[string]*Player),
		Status:        pb.MatchStatus_MATCH_STATUS_LOBBY,
		pendingInputs: make(map[string]*Input),
		rng:           rand.New(rand.NewSource(seed)),
	}
	s.layoutGrid()
	return s
}

func (s *MatchState) idx(x, y int) int { return y*s.Width + x }

func (s *MatchState) tileAt(x, y int) Tile {
	if x < 0 || x >= s.Width || y < 0 || y >= s.Height {
		return TileWall
	}
	return s.Tiles[s.idx(x, y)]
}

func (s *MatchState) setTile(x, y int, t Tile) {
	if x < 0 || x >= s.Width || y < 0 || y >= s.Height {
		return
	}
	s.Tiles[s.idx(x, y)] = t
}

func (s *MatchState) layoutGrid() {
	for y := 0; y < s.Height; y++ {
		for x := 0; x < s.Width; x++ {
			switch {
			case x == 0 || y == 0 || x == s.Width-1 || y == s.Height-1:
				s.setTile(x, y, TileWall)
			case x%2 == 0 && y%2 == 0:
				s.setTile(x, y, TileWall)
			default:
				s.setTile(x, y, TileBlock)
			}
		}
	}
	// Carve open the four corner spawn pockets so corner players aren't
	// boxed in by destructible blocks at start.
	corners := [][2]int{{1, 1}, {s.Width - 2, 1}, {1, s.Height - 2}, {s.Width - 2, s.Height - 2}}
	for _, c := range corners {
		for _, off := range [][2]int{{0, 0}, {1, 0}, {0, 1}} {
			x, y := c[0]+off[0], c[1]+off[1]
			// keep within bounds
			if x > 0 && x < s.Width-1 && y > 0 && y < s.Height-1 {
				s.setTile(x, y, TileEmpty)
			}
		}
	}
}

// SpawnPositions returns up to MaxPlayers spawn coordinates. The first
// four are the classic corners, the rest are mid-edges so 8-player
// matches still have non-overlapping starts.
func (s *MatchState) SpawnPositions() [][2]int {
	return [][2]int{
		{1, 1},
		{s.Width - 2, 1},
		{1, s.Height - 2},
		{s.Width - 2, s.Height - 2},
		{s.Width / 2, 1},
		{s.Width / 2, s.Height - 2},
		{1, s.Height / 2},
		{s.Width - 2, s.Height / 2},
	}
}

// AddPlayer places a new player at (x,y). Fails if a slot already
// exists, the match has already started, or the cell isn't empty.
func (s *MatchState) AddPlayer(id string, x, y int) error {
	if s.Status != pb.MatchStatus_MATCH_STATUS_LOBBY {
		return fmt.Errorf("match is not in lobby")
	}
	if _, ok := s.Players[id]; ok {
		return fmt.Errorf("player %s already added", id)
	}
	if len(s.Players) >= MaxPlayers {
		return fmt.Errorf("match is full")
	}
	if s.tileAt(x, y) != TileEmpty {
		return fmt.Errorf("spawn cell (%d,%d) is not empty", x, y)
	}
	s.Players[id] = &Player{
		ID:           id,
		X:            x,
		Y:            y,
		Alive:        true,
		BombCapacity: DefaultBombCap,
		BombPower:    DefaultBombPower,
		Speed:        DefaultSpeed,
	}
	return nil
}

// Start transitions the match from LOBBY to RUNNING. Requires at least
// MinPlayersToStart participants.
func (s *MatchState) Start() error {
	if s.Status != pb.MatchStatus_MATCH_STATUS_LOBBY {
		return fmt.Errorf("match already started")
	}
	if len(s.Players) < MinPlayersToStart {
		return fmt.Errorf("need at least %d players to start, have %d", MinPlayersToStart, len(s.Players))
	}
	s.Status = pb.MatchStatus_MATCH_STATUS_RUNNING
	return nil
}

// QueueInput records a player's intent for the next tick. The latest
// input wins — this matches typical client behavior of resending the
// desired direction every frame.
func (s *MatchState) QueueInput(playerID string, in Input) error {
	p, ok := s.Players[playerID]
	if !ok {
		return fmt.Errorf("player %s not in match", playerID)
	}
	if !p.Alive {
		return nil // silently drop inputs from dead players
	}
	s.pendingInputs[playerID] = &in
	return nil
}

// AdvanceTick advances the match by one tick: applies queued inputs,
// resolves bombs, applies powerups, evaluates end conditions. Pure
// function over MatchState — caller is responsible for locking.
func (s *MatchState) AdvanceTick() {
	if s.Status != pb.MatchStatus_MATCH_STATUS_RUNNING {
		return
	}

	s.applyInputs()
	s.advanceBombs()
	s.advanceExplosions()
	s.collectPowerups()

	s.Tick++
	s.evaluateEnd()
}

func (s *MatchState) applyInputs() {
	for pid, in := range s.pendingInputs {
		p, ok := s.Players[pid]
		if !ok || !p.Alive {
			continue
		}
		// Movement: discrete one cell in the requested direction. Players
		// step into a cell only if it's empty, no bomb sits there, and no
		// other player occupies it.
		if in.Move != pb.Direction_DIR_NONE {
			dx, dy := dirDelta(in.Move)
			nx, ny := p.X+dx, p.Y+dy
			if s.canEnter(nx, ny, pid) {
				p.X, p.Y = nx, ny
			}
		}
		if in.PlaceBomb && p.ActiveBombs < p.BombCapacity && !s.bombAt(p.X, p.Y) {
			s.Bombs = append(s.Bombs, &Bomb{
				X: p.X, Y: p.Y,
				OwnerID:        pid,
				Power:          p.BombPower,
				TicksRemaining: BombFuseTicks,
			})
			p.ActiveBombs++
		}
	}
	s.pendingInputs = make(map[string]*Input)
}

func dirDelta(d pb.Direction) (int, int) {
	switch d {
	case pb.Direction_DIR_UP:
		return 0, -1
	case pb.Direction_DIR_DOWN:
		return 0, 1
	case pb.Direction_DIR_LEFT:
		return -1, 0
	case pb.Direction_DIR_RIGHT:
		return 1, 0
	}
	return 0, 0
}

func (s *MatchState) canEnter(x, y int, byPlayer string) bool {
	if s.tileAt(x, y) != TileEmpty {
		return false
	}
	if s.bombAt(x, y) {
		return false
	}
	for id, p := range s.Players {
		if id == byPlayer {
			continue
		}
		if p.Alive && p.X == x && p.Y == y {
			return false
		}
	}
	return true
}

func (s *MatchState) bombAt(x, y int) bool {
	for _, b := range s.Bombs {
		if b.X == x && b.Y == y {
			return true
		}
	}
	return false
}

func (s *MatchState) advanceBombs() {
	// Iterate snapshot of bombs to handle chain detonation: a bomb
	// caught by another's blast detonates immediately at the same tick,
	// extending the explosion. We resolve by repeatedly walking the
	// list until nothing new exploded this tick.
	for {
		exploded := false
		remaining := s.Bombs[:0]
		for _, b := range s.Bombs {
			b.TicksRemaining--
			if b.TicksRemaining <= 0 {
				s.detonate(b)
				exploded = true
				continue
			}
			remaining = append(remaining, b)
		}
		s.Bombs = remaining
		if !exploded {
			return
		}
		// After a chain detonation, any other bombs sitting on a fresh
		// explosion tile should detonate too. Set their fuse to 0 so
		// the next loop iteration consumes them.
		for _, b := range s.Bombs {
			for _, e := range s.Explosions {
				if e.X == b.X && e.Y == b.Y && e.TicksRemaining == ExplosionTicks {
					b.TicksRemaining = 0
				}
			}
		}
	}
}

func (s *MatchState) detonate(b *Bomb) {
	if owner, ok := s.Players[b.OwnerID]; ok && owner.ActiveBombs > 0 {
		owner.ActiveBombs--
	}
	// Center cell always explodes.
	s.spawnExplosion(b.X, b.Y)
	// Four directions, stopping at indestructible walls; destructible
	// blocks are turned into empty (and may drop a powerup) but do
	// block further blast in that direction.
	for _, d := range []pb.Direction{pb.Direction_DIR_UP, pb.Direction_DIR_DOWN, pb.Direction_DIR_LEFT, pb.Direction_DIR_RIGHT} {
		dx, dy := dirDelta(d)
		for r := 1; r <= b.Power; r++ {
			x, y := b.X+dx*r, b.Y+dy*r
			t := s.tileAt(x, y)
			if t == TileWall {
				break
			}
			s.spawnExplosion(x, y)
			if t == TileBlock {
				s.setTile(x, y, TileEmpty)
				if s.rng.Float64() < BlockDropPowerup {
					s.Powerups = append(s.Powerups, &Powerup{
						X: x, Y: y, Kind: randomPowerup(s.rng),
					})
				}
				break
			}
		}
	}
	s.killPlayersOnExplosions()
}

func randomPowerup(r *rand.Rand) PowerupKind {
	switch r.Intn(3) {
	case 0:
		return PowerupBomb
	case 1:
		return PowerupPower
	default:
		return PowerupSpeed
	}
}

// spawnExplosion adds an explosion tile, removing any powerup that was
// sitting there (powerups are destroyed by a blast that touches them).
func (s *MatchState) spawnExplosion(x, y int) {
	s.Explosions = append(s.Explosions, &Explosion{X: x, Y: y, TicksRemaining: ExplosionTicks})
	pruned := s.Powerups[:0]
	for _, p := range s.Powerups {
		if p.X == x && p.Y == y {
			continue
		}
		pruned = append(pruned, p)
	}
	s.Powerups = pruned
}

func (s *MatchState) advanceExplosions() {
	remaining := s.Explosions[:0]
	for _, e := range s.Explosions {
		e.TicksRemaining--
		if e.TicksRemaining > 0 {
			remaining = append(remaining, e)
		}
	}
	s.Explosions = remaining
}

func (s *MatchState) killPlayersOnExplosions() {
	for _, e := range s.Explosions {
		if e.TicksRemaining != ExplosionTicks {
			continue // only kill on the spawn tick
		}
		for _, p := range s.Players {
			if p.Alive && p.X == e.X && p.Y == e.Y {
				p.Alive = false
			}
		}
	}
}

func (s *MatchState) collectPowerups() {
	if len(s.Powerups) == 0 {
		return
	}
	pruned := s.Powerups[:0]
	for _, pu := range s.Powerups {
		consumed := false
		for _, p := range s.Players {
			if !p.Alive {
				continue
			}
			if p.X == pu.X && p.Y == pu.Y {
				switch pu.Kind {
				case PowerupBomb:
					p.BombCapacity++
				case PowerupPower:
					p.BombPower++
				case PowerupSpeed:
					p.Speed++
				}
				consumed = true
				break
			}
		}
		if !consumed {
			pruned = append(pruned, pu)
		}
	}
	s.Powerups = pruned
}

func (s *MatchState) evaluateEnd() {
	alive := 0
	var lastAlive string
	for _, p := range s.Players {
		if p.Alive {
			alive++
			lastAlive = p.ID
		}
	}
	if alive <= 1 {
		s.Status = pb.MatchStatus_MATCH_STATUS_ENDED
		if alive == 1 {
			s.WinnerID = lastAlive
		}
		return
	}
	if s.Tick >= MatchTimeLimit {
		// Sudden-death: time limit reached, declare draw (no winner).
		// A more elaborate version would shrink the arena; for the demo
		// a clean cut is sufficient.
		s.Status = pb.MatchStatus_MATCH_STATUS_ENDED
	}
}

// Snapshot builds the wire format for the current state. Cheap enough
// to call every tick; allocates fresh slices each time so the caller
// can ship the message off without aliasing concerns.
func (s *MatchState) Snapshot(matchID string) *pb.MatchSnapshot {
	tiles := make([]pb.Tile, len(s.Tiles))
	for i, t := range s.Tiles {
		tiles[i] = pb.Tile(t)
	}
	players := make([]*pb.PlayerState, 0, len(s.Players))
	for _, p := range s.Players {
		players = append(players, &pb.PlayerState{
			PlayerId:     p.ID,
			X:            int32(p.X),
			Y:            int32(p.Y),
			Alive:        p.Alive,
			BombCapacity: int32(p.BombCapacity),
			BombPower:    int32(p.BombPower),
			Speed:        int32(p.Speed),
			ActiveBombs:  int32(p.ActiveBombs),
		})
	}
	bombs := make([]*pb.BombState, 0, len(s.Bombs))
	for _, b := range s.Bombs {
		bombs = append(bombs, &pb.BombState{
			X:              int32(b.X),
			Y:              int32(b.Y),
			OwnerId:        b.OwnerID,
			Power:          int32(b.Power),
			TicksRemaining: int32(b.TicksRemaining),
		})
	}
	explosions := make([]*pb.ExplosionState, 0, len(s.Explosions))
	for _, e := range s.Explosions {
		explosions = append(explosions, &pb.ExplosionState{
			X:              int32(e.X),
			Y:              int32(e.Y),
			TicksRemaining: int32(e.TicksRemaining),
		})
	}
	powerups := make([]*pb.PowerupState, 0, len(s.Powerups))
	for _, pu := range s.Powerups {
		powerups = append(powerups, &pb.PowerupState{
			X:    int32(pu.X),
			Y:    int32(pu.Y),
			Kind: pb.PowerupKind(pu.Kind + 1), // proto enum offsets POWERUP_UNSPECIFIED=0
		})
	}
	return &pb.MatchSnapshot{
		MatchId:    matchID,
		Status:     s.Status,
		Tick:       int32(s.Tick),
		Width:      int32(s.Width),
		Height:     int32(s.Height),
		Tiles:      tiles,
		Players:    players,
		Bombs:      bombs,
		Explosions: explosions,
		Powerups:   powerups,
		WinnerId:   s.WinnerID,
	}
}
