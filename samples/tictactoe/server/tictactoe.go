package main

import (
	"context"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/goverseapi"
	tictactoe_pb "github.com/xiaonanln/goverse/samples/tictactoe/proto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	// cleanupInterval is how often to run periodic cleanup
	cleanupInterval = 1 * time.Minute
	// maxGameAge is the maximum age of a game before it's cleaned up
	maxGameAge = 10 * time.Minute
)

// TicTacToeGame represents a single game instance (not a distributed object)
type TicTacToeGame struct {
	gameID     string
	board      [9]string // "", "X", or "O"
	status     string    // "playing", "x_wins", "o_wins", "draw"
	moves      int       // Total moves made
	lastAIMove int32     // Last AI move position, -1 if none
	createdAt  time.Time // Time when the game was created
}

// newGame creates a new TicTacToeGame with initial state
func newGame(gameID string) *TicTacToeGame {
	return &TicTacToeGame{
		gameID:     gameID,
		status:     "playing",
		lastAIMove: -1,
		createdAt:  time.Now(),
	}
}

// reset resets the game to initial state
func (g *TicTacToeGame) reset() {
	for i := range g.board {
		g.board[i] = ""
	}
	g.status = "playing"
	g.moves = 0
	g.lastAIMove = -1
	g.createdAt = time.Now()
}

// getGameState returns the current game state as proto message
func (g *TicTacToeGame) getGameState() *tictactoe_pb.GameState {
	board := make([]string, 9)
	for i, v := range g.board {
		board[i] = v
	}

	winner := ""
	if g.status == "x_wins" {
		winner = "X"
	} else if g.status == "o_wins" {
		winner = "O"
	}

	return &tictactoe_pb.GameState{
		GameId:     g.gameID,
		Board:      board,
		Status:     g.status,
		Winner:     winner,
		LastAiMove: g.lastAIMove,
	}
}

// updateStatus checks for a win or draw and updates the status
func (g *TicTacToeGame) updateStatus() {
	// Winning combinations
	winPatterns := [][3]int{
		{0, 1, 2}, // top row
		{3, 4, 5}, // middle row
		{6, 7, 8}, // bottom row
		{0, 3, 6}, // left column
		{1, 4, 7}, // middle column
		{2, 5, 8}, // right column
		{0, 4, 8}, // diagonal
		{2, 4, 6}, // anti-diagonal
	}

	for _, pattern := range winPatterns {
		a, b, c := pattern[0], pattern[1], pattern[2]
		if g.board[a] != "" && g.board[a] == g.board[b] && g.board[b] == g.board[c] {
			if g.board[a] == "X" {
				g.status = "x_wins"
			} else {
				g.status = "o_wins"
			}
			return
		}
	}

	// Check for draw
	if g.moves >= 9 {
		g.status = "draw"
	}
}

// findAIMove finds the best move for AI (O) using simple strategy
// 1. Take winning move if available
// 2. Block opponent's winning move
// 3. Take center if available
// 4. Take a corner
// 5. Take any edge
func (g *TicTacToeGame) findAIMove() int {
	// 1. Check for winning move
	if move := g.findWinningMove("O"); move >= 0 {
		return move
	}

	// 2. Block opponent's winning move
	if move := g.findWinningMove("X"); move >= 0 {
		return move
	}

	// 3. Take center if available
	if g.board[4] == "" {
		return 4
	}

	// 4. Take a corner
	corners := []int{0, 2, 6, 8}
	for _, corner := range corners {
		if g.board[corner] == "" {
			return corner
		}
	}

	// 5. Take any edge
	edges := []int{1, 3, 5, 7}
	for _, edge := range edges {
		if g.board[edge] == "" {
			return edge
		}
	}

	return -1 // No move available
}

// findWinningMove finds a winning move for the given player
func (g *TicTacToeGame) findWinningMove(player string) int {
	winPatterns := [][3]int{
		{0, 1, 2}, {3, 4, 5}, {6, 7, 8}, // rows
		{0, 3, 6}, {1, 4, 7}, {2, 5, 8}, // columns
		{0, 4, 8}, {2, 4, 6}, // diagonals
	}

	for _, pattern := range winPatterns {
		a, b, c := pattern[0], pattern[1], pattern[2]
		cells := [3]string{g.board[a], g.board[b], g.board[c]}

		// Count player marks and empty cells
		playerCount := 0
		emptyPos := -1
		for i, cell := range cells {
			if cell == player {
				playerCount++
			} else if cell == "" {
				emptyPos = pattern[i]
			}
		}

		// If two of player's marks and one empty, return the empty position
		if playerCount == 2 && emptyPos >= 0 {
			return emptyPos
		}
	}

	return -1
}

// TicTacToeService is a distributed object that manages multiple TicTacToeGame instances.
// Note: The cleanup goroutine runs for the lifetime of this service object.
// Since TicTacToeService objects are designed as long-lived service singletons,
// the cleanup goroutine will be stopped when the server process terminates.
type TicTacToeService struct {
	goverseapi.BaseObject

	mu        sync.Mutex
	games     map[string]*TicTacToeGame // gameID -> game
	stopCh    chan struct{}             // channel to signal cleanup goroutine to stop
	cleanupWg sync.WaitGroup            // wait group for cleanup goroutine
}

// OnCreated is called when the service object is created
func (s *TicTacToeService) OnCreated() {
	s.Logger.Infof("TicTacToeService %s created", s.Id())
	if s.games == nil {
		s.games = make(map[string]*TicTacToeGame)
	}
	s.stopCh = make(chan struct{})
	s.startCleanupRoutine()
}

// StopCleanup stops the cleanup goroutine gracefully.
// This method can be called to stop the cleanup routine if needed.
func (s *TicTacToeService) StopCleanup() {
	select {
	case <-s.stopCh:
		// Already closed
	default:
		close(s.stopCh)
	}
	s.cleanupWg.Wait()
}

// startCleanupRoutine starts a goroutine that periodically cleans up old and finished games
func (s *TicTacToeService) startCleanupRoutine() {
	s.cleanupWg.Add(1)
	go func() {
		defer s.cleanupWg.Done()
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stopCh:
				s.Logger.Infof("TicTacToeService %s: cleanup routine stopped", s.Id())
				return
			case <-ticker.C:
				s.cleanupGames()
			}
		}
	}()
	s.Logger.Infof("TicTacToeService %s: cleanup routine started (interval: %v, max age: %v)", s.Id(), cleanupInterval, maxGameAge)
}

// cleanupGames removes finished games and games that are older than maxGameAge
func (s *TicTacToeService) cleanupGames() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var finishedCount, expiredCount int

	for gameID, game := range s.games {
		// Cleanup finished games (x_wins, o_wins, draw)
		if game.status != "playing" {
			delete(s.games, gameID)
			finishedCount++
			continue
		}

		// Cleanup games older than maxGameAge
		if now.Sub(game.createdAt) > maxGameAge {
			delete(s.games, gameID)
			expiredCount++
		}
	}

	if finishedCount > 0 || expiredCount > 0 {
		s.Logger.Infof("TicTacToeService %s: cleaned up %d finished games, %d expired games (%d remaining)",
			s.Id(), finishedCount, expiredCount, len(s.games))
	}
}

// ToData serializes the TicTacToeService state for persistence
// Thread-safe implementation with mutex to handle concurrent access during periodic persistence
func (s *TicTacToeService) ToData() (proto.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Serialize each game to a map structure
	gamesData := make(map[string]interface{})
	for gameID, game := range s.games {
		// Convert board to list of strings
		boardList := make([]interface{}, 9)
		for i, cell := range game.board {
			boardList[i] = cell
		}

		gameData := map[string]interface{}{
			"gameID":     game.gameID,
			"board":      boardList,
			"status":     game.status,
			"moves":      float64(game.moves),
			"lastAIMove": float64(game.lastAIMove),
			"createdAt":  game.createdAt.UnixNano(),
		}
		gamesData[gameID] = gameData
	}

	data, err := structpb.NewStruct(map[string]interface{}{
		"id":    s.Id(),
		"type":  s.Type(),
		"games": gamesData,
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// FromData deserializes the TicTacToeService state from persistence
// Thread-safe implementation with mutex to handle concurrent access during object initialization
func (s *TicTacToeService) FromData(data proto.Message) error {
	if data == nil {
		return nil
	}

	structData, ok := data.(*structpb.Struct)
	if !ok {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Initialize games map if needed
	if s.games == nil {
		s.games = make(map[string]*TicTacToeGame)
	}

	// Load games from the saved data
	gamesField, ok := structData.Fields["games"]
	if !ok {
		return nil
	}

	gamesStruct := gamesField.GetStructValue()
	if gamesStruct == nil {
		return nil
	}

	for gameID, gameValue := range gamesStruct.Fields {
		gameStruct := gameValue.GetStructValue()
		if gameStruct == nil {
			continue
		}

		game := &TicTacToeGame{
			gameID:     gameID,
			status:     "playing",
			lastAIMove: -1,
			createdAt:  time.Now(),
		}

		// Restore gameID
		if v, ok := gameStruct.Fields["gameID"]; ok {
			game.gameID = v.GetStringValue()
		}

		// Restore board
		if v, ok := gameStruct.Fields["board"]; ok {
			boardList := v.GetListValue()
			if boardList != nil {
				for i, cell := range boardList.Values {
					if i < 9 {
						game.board[i] = cell.GetStringValue()
					}
				}
			}
		}

		// Restore status
		if v, ok := gameStruct.Fields["status"]; ok {
			game.status = v.GetStringValue()
		}

		// Restore moves
		if v, ok := gameStruct.Fields["moves"]; ok {
			game.moves = int(v.GetNumberValue())
		}

		// Restore lastAIMove
		if v, ok := gameStruct.Fields["lastAIMove"]; ok {
			game.lastAIMove = int32(v.GetNumberValue())
		}

		// Restore createdAt
		if v, ok := gameStruct.Fields["createdAt"]; ok {
			game.createdAt = time.Unix(0, int64(v.GetNumberValue()))
		}

		s.games[gameID] = game
	}

	s.Logger.Infof("TicTacToeService %s: loaded %d games from persistence", s.Id(), len(s.games))
	return nil
}

// getOrCreateGame retrieves an existing game or creates a new one
func (s *TicTacToeService) getOrCreateGame(gameID string) *TicTacToeGame {
	game, exists := s.games[gameID]
	if !exists {
		game = newGame(gameID)
		s.games[gameID] = game
		s.Logger.Infof("TicTacToeService %s: created new game %s", s.Id(), gameID)
	}
	return game
}

// NewGame creates or resets a game and returns the current state
func (s *TicTacToeService) NewGame(ctx context.Context, req *tictactoe_pb.NewGameRequest) (*tictactoe_pb.GameState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	gameID := req.GetGameId()
	if gameID == "" {
		gameID = "default"
	}

	s.Logger.Infof("TicTacToeService %s: NewGame called for game %s", s.Id(), gameID)

	game := s.getOrCreateGame(gameID)
	game.reset()

	return game.getGameState(), nil
}

// MakeMove handles a player move and returns the game state after AI response
func (s *TicTacToeService) MakeMove(ctx context.Context, req *tictactoe_pb.MoveRequest) (*tictactoe_pb.GameState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	gameID := req.GetGameId()
	if gameID == "" {
		gameID = "default"
	}
	position := int(req.GetPosition())

	s.Logger.Infof("TicTacToeService %s: MakeMove called for game %s, position %d", s.Id(), gameID, position)

	game := s.getOrCreateGame(gameID)

	// Validate game is still playing
	if game.status != "playing" {
		s.Logger.Warnf("TicTacToeService %s: Game %s already ended with status %s", s.Id(), gameID, game.status)
		return game.getGameState(), nil
	}

	// Validate position
	if position < 0 || position > 8 {
		s.Logger.Warnf("TicTacToeService %s: Invalid position %d for game %s", s.Id(), position, gameID)
		return game.getGameState(), nil
	}

	// Check if position is already taken
	if game.board[position] != "" {
		s.Logger.Warnf("TicTacToeService %s: Position %d already taken for game %s", s.Id(), position, gameID)
		return game.getGameState(), nil
	}

	// Player makes move (X)
	game.board[position] = "X"
	game.moves++
	game.lastAIMove = -1

	// Check for win or draw after player move
	game.updateStatus()
	if game.status != "playing" {
		return game.getGameState(), nil
	}

	// AI makes move (O)
	aiMove := game.findAIMove()
	if aiMove >= 0 {
		game.board[aiMove] = "O"
		game.moves++
		game.lastAIMove = int32(aiMove)
		s.Logger.Infof("TicTacToeService %s: AI moves to position %d for game %s", s.Id(), aiMove, gameID)
	}

	// Check for win or draw after AI move
	game.updateStatus()

	return game.getGameState(), nil
}

// GetState returns the current game state
func (s *TicTacToeService) GetState(ctx context.Context, req *tictactoe_pb.GetStateRequest) (*tictactoe_pb.GameState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	gameID := req.GetGameId()
	if gameID == "" {
		gameID = "default"
	}

	s.Logger.Infof("TicTacToeService %s: GetState called for game %s", s.Id(), gameID)

	game := s.getOrCreateGame(gameID)
	return game.getGameState(), nil
}
