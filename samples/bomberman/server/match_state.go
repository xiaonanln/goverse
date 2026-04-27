package main

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"

	pb "github.com/xiaonanln/goverse/samples/bomberman/proto"
)

// Match grid + tick parameters. Numbers picked to feel like classic
// Super Bomberman: 13x11 grid, 10Hz tick, 2 s bomb fuse, 0.5 s
// explosion afterglow, 30 % drop chance per destroyed block.
const (
	GridWidth                   = 13
	GridHeight                  = 11
	TickHz                      = 10
	BombFuseTicks               = 20
	ExplosionTicks              = 5
	DefaultMatchTimeLimitTicks  = 1800 // 3 minutes at 10 Hz
	BlockDropPowerup            = 0.30
	DefaultBombCap              = 1
	DefaultBombPower            = 2
	DefaultSpeed                = 1
	MinPlayersToStart           = 2
	MaxPlayers                  = 8
)

// MatchTimeLimit is read once at process start. It defaults to
// DefaultMatchTimeLimitTicks (3 minutes) but can be overridden via
// BOMBERMAN_MATCH_TIME_LIMIT_TICKS so the stress test can drive short
// matches and exercise many end-of-match cycles in a CI window.
var MatchTimeLimit = readMatchTimeLimit()

func readMatchTimeLimit() int {
	if v := os.Getenv("BOMBERMAN_MATCH_TIME_LIMIT_TICKS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return DefaultMatchTimeLimitTicks
}

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

// MatchPlayer is the in-match record for one participant. Distinct
// from the persistent Player goverse object (per-user stats); a
// MatchPlayer's identity (ID) typically matches a Player.Id() so the
// match-end reliable call lands on the correct account.
type MatchPlayer struct {
	ID           string
	ClientID     string // gate-assigned id of the connected client; empty for server-driven test players
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
	Players       map[string]*MatchPlayer
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
		Players:       make(map[string]*MatchPlayer),
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
	// boxed in by destructible blocks at start. Each pocket is an L
	// (corner cell + horizontal neighbor + vertical neighbor) carved
	// **toward the map interior**: the top-right pocket extends left
	// and down, the bottom-left extends right and up, etc. Using the
	// same positive offsets for every corner — as a previous version
	// did — would push three of the four pockets onto border cells,
	// which the bounds check would silently skip, leaving the
	// (W-2,H-2) spawn fully boxed in by blocks at game start.
	pockets := [][3][2]int{
		{{1, 1}, {2, 1}, {1, 2}},
		{{s.Width - 2, 1}, {s.Width - 3, 1}, {s.Width - 2, 2}},
		{{1, s.Height - 2}, {2, s.Height - 2}, {1, s.Height - 3}},
		{{s.Width - 2, s.Height - 2}, {s.Width - 3, s.Height - 2}, {s.Width - 2, s.Height - 3}},
	}
	for _, pocket := range pockets {
		for _, cell := range pocket {
			s.setTile(cell[0], cell[1], TileEmpty)
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
// clientID is the gate-assigned id of the connected client (used as
// the push target for snapshots); pass empty for tests that don't
// have a real client.
func (s *MatchState) AddPlayer(id, clientID string, x, y int) error {
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
	s.Players[id] = &MatchPlayer{
		ID:           id,
		ClientID:     clientID,
		X:            x,
		Y:            y,
		Alive:        true,
		BombCapacity: DefaultBombCap,
		BombPower:    DefaultBombPower,
		Speed:        DefaultSpeed,
	}
	return nil
}

// ClientIDs returns the gate-assigned client ids of every player
// currently in the match (in unspecified order). Empty client ids
// (from server-driven test players) are skipped so the result is safe
// to pass directly to PushMessageToClients.
func (s *MatchState) ClientIDs() []string {
	ids := make([]string, 0, len(s.Players))
	for _, p := range s.Players {
		if p.ClientID != "" {
			ids = append(ids, p.ClientID)
		}
	}
	return ids
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
		// Movement: step in the requested direction up to p.Speed cells
		// (default 1, +1 per Speed powerup picked up). Stops at the first
		// blocked cell — wall, bomb, or another player — so a higher
		// speed never lets a player teleport through obstacles.
		if in.Move != pb.Direction_DIR_NONE {
			dx, dy := dirDelta(in.Move)
			for step := 0; step < p.Speed; step++ {
				nx, ny := p.X+dx, p.Y+dy
				if !s.canEnter(nx, ny, pid) {
					break
				}
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
	// Decrement every bomb's fuse exactly once per tick. A previous
	// version did the decrement inside the chain-resolution loop,
	// which incorrectly burned extra fuse off unrelated bombs whenever
	// any chain detonation occurred — bombs would explode earlier
	// than configured and match timing would jitter under load.
	for _, b := range s.Bombs {
		b.TicksRemaining--
	}

	// Resolve all detonations this tick. Each pass: any bomb with
	// fuse <= 0 explodes; if its blast lands on another live bomb,
	// that bomb's fuse is set to 0 so it detonates on the next pass.
	// Loop until no new detonations.
	for {
		exploded := false
		remaining := s.Bombs[:0]
		for _, b := range s.Bombs {
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
