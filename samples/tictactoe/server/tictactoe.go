package main

import (
	"context"
	"sync"

	"github.com/xiaonanln/goverse/goverseapi"
	tictactoe_pb "github.com/xiaonanln/goverse/samples/tictactoe/proto"
)

// TicTacToe represents a Tic Tac Toe game instance
type TicTacToe struct {
	goverseapi.BaseObject

	mu         sync.Mutex
	board      [9]string // "", "X", or "O"
	status     string    // "playing", "x_wins", "o_wins", "draw"
	moves      int       // Total moves made
	lastAIMove int32     // Last AI move position, -1 if none
}

// OnCreated is called when the object is created
func (g *TicTacToe) OnCreated() {
	g.Logger.Infof("TicTacToe %s created", g.Id())
	g.resetGame()
}

// resetGame resets the game to initial state
func (g *TicTacToe) resetGame() {
	for i := range g.board {
		g.board[i] = ""
	}
	g.status = "playing"
	g.moves = 0
	g.lastAIMove = -1
}

// NewGame resets the game and returns the current state
func (g *TicTacToe) NewGame(ctx context.Context, req *tictactoe_pb.MoveRequest) (*tictactoe_pb.GameState, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.Logger.Infof("TicTacToe %s: NewGame called", g.Id())
	g.resetGame()

	return g.getGameState(), nil
}

// MakeMove handles a player move and returns the game state after AI response
func (g *TicTacToe) MakeMove(ctx context.Context, req *tictactoe_pb.MoveRequest) (*tictactoe_pb.GameState, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	position := int(req.GetPosition())
	g.Logger.Infof("TicTacToe %s: MakeMove called with position %d", g.Id(), position)

	// Validate game is still playing
	if g.status != "playing" {
		g.Logger.Warnf("TicTacToe %s: Game already ended with status %s", g.Id(), g.status)
		return g.getGameState(), nil
	}

	// Validate position
	if position < 0 || position > 8 {
		g.Logger.Warnf("TicTacToe %s: Invalid position %d", g.Id(), position)
		return g.getGameState(), nil
	}

	// Check if position is already taken
	if g.board[position] != "" {
		g.Logger.Warnf("TicTacToe %s: Position %d already taken", g.Id(), position)
		return g.getGameState(), nil
	}

	// Player makes move (X)
	g.board[position] = "X"
	g.moves++
	g.lastAIMove = -1

	// Check for win or draw after player move
	g.updateStatus()
	if g.status != "playing" {
		return g.getGameState(), nil
	}

	// AI makes move (O)
	aiMove := g.findAIMove()
	if aiMove >= 0 {
		g.board[aiMove] = "O"
		g.moves++
		g.lastAIMove = int32(aiMove)
		g.Logger.Infof("TicTacToe %s: AI moves to position %d", g.Id(), aiMove)
	}

	// Check for win or draw after AI move
	g.updateStatus()

	return g.getGameState(), nil
}

// GetState returns the current game state
func (g *TicTacToe) GetState(ctx context.Context, req *tictactoe_pb.MoveRequest) (*tictactoe_pb.GameState, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.Logger.Infof("TicTacToe %s: GetState called", g.Id())
	return g.getGameState(), nil
}

// getGameState returns the current game state as proto message
func (g *TicTacToe) getGameState() *tictactoe_pb.GameState {
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
		Board:      board,
		Status:     g.status,
		Winner:     winner,
		LastAiMove: g.lastAIMove,
	}
}

// updateStatus checks for a win or draw and updates the status
func (g *TicTacToe) updateStatus() {
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
func (g *TicTacToe) findAIMove() int {
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
func (g *TicTacToe) findWinningMove(player string) int {
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
