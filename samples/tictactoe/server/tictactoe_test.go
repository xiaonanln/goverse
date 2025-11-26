package main

import (
	"context"
	"testing"

	tictactoe_pb "github.com/xiaonanln/goverse/samples/tictactoe/proto"
)

func TestTicTacToe_NewGame(t *testing.T) {
	game := &TicTacToe{}
	game.OnInit(game, "test-game")
	game.OnCreated()

	state, err := game.NewGame(context.Background(), &tictactoe_pb.MoveRequest{})
	if err != nil {
		t.Fatalf("NewGame failed: %v", err)
	}

	if state.Status != "playing" {
		t.Errorf("Expected status 'playing', got '%s'", state.Status)
	}

	if len(state.Board) != 9 {
		t.Errorf("Expected board length 9, got %d", len(state.Board))
	}

	for i, cell := range state.Board {
		if cell != "" {
			t.Errorf("Expected empty cell at position %d, got '%s'", i, cell)
		}
	}

	if state.LastAiMove != -1 {
		t.Errorf("Expected LastAiMove -1, got %d", state.LastAiMove)
	}
}

func TestTicTacToe_MakeMove_ValidMove(t *testing.T) {
	game := &TicTacToe{}
	game.OnInit(game, "test-game")
	game.OnCreated()

	// Make a move at center
	state, err := game.MakeMove(context.Background(), &tictactoe_pb.MoveRequest{Position: 4})
	if err != nil {
		t.Fatalf("MakeMove failed: %v", err)
	}

	// Player should be at position 4
	if state.Board[4] != "X" {
		t.Errorf("Expected 'X' at position 4, got '%s'", state.Board[4])
	}

	// AI should have made a move
	aiMoved := false
	for i, cell := range state.Board {
		if cell == "O" {
			aiMoved = true
			if int32(i) != state.LastAiMove {
				t.Errorf("LastAiMove should match AI's position")
			}
			break
		}
	}

	if !aiMoved && state.Status == "playing" {
		t.Errorf("AI should have made a move")
	}
}

func TestTicTacToe_MakeMove_InvalidPosition(t *testing.T) {
	game := &TicTacToe{}
	game.OnInit(game, "test-game")
	game.OnCreated()

	// Try invalid position
	state, err := game.MakeMove(context.Background(), &tictactoe_pb.MoveRequest{Position: 10})
	if err != nil {
		t.Fatalf("MakeMove failed: %v", err)
	}

	// Board should remain empty
	for i, cell := range state.Board {
		if cell != "" {
			t.Errorf("Expected empty cell at position %d, got '%s'", i, cell)
		}
	}
}

func TestTicTacToe_MakeMove_OccupiedPosition(t *testing.T) {
	game := &TicTacToe{}
	game.OnInit(game, "test-game")
	game.OnCreated()

	// Make first move
	game.MakeMove(context.Background(), &tictactoe_pb.MoveRequest{Position: 4})

	// Try to move to same position
	state, err := game.MakeMove(context.Background(), &tictactoe_pb.MoveRequest{Position: 4})
	if err != nil {
		t.Fatalf("MakeMove failed: %v", err)
	}

	// Position 4 should still be X (not changed)
	if state.Board[4] != "X" {
		t.Errorf("Expected 'X' at position 4, got '%s'", state.Board[4])
	}
}

func TestTicTacToe_GetState(t *testing.T) {
	game := &TicTacToe{}
	game.OnInit(game, "test-game")
	game.OnCreated()

	// Make a move first
	game.MakeMove(context.Background(), &tictactoe_pb.MoveRequest{Position: 0})

	// Get state
	state, err := game.GetState(context.Background(), &tictactoe_pb.MoveRequest{})
	if err != nil {
		t.Fatalf("GetState failed: %v", err)
	}

	// Verify state matches what we expect
	if state.Board[0] != "X" {
		t.Errorf("Expected 'X' at position 0, got '%s'", state.Board[0])
	}
}

func TestTicTacToe_WinCondition(t *testing.T) {
	game := &TicTacToe{}
	game.OnInit(game, "test-game")
	game.OnCreated()

	// Manually set up a winning board for testing
	// We'll directly manipulate the board to test win detection
	game.mu.Lock()
	game.board[0] = "X"
	game.board[1] = "X"
	game.board[2] = "X"
	game.moves = 3
	game.updateStatus()
	game.mu.Unlock()

	if game.status != "x_wins" {
		t.Errorf("Expected status 'x_wins', got '%s'", game.status)
	}
}

func TestTicTacToe_DrawCondition(t *testing.T) {
	game := &TicTacToe{}
	game.OnInit(game, "test-game")
	game.OnCreated()

	// Manually set up a draw board
	// X O X
	// X O O
	// O X X
	game.mu.Lock()
	game.board[0] = "X"
	game.board[1] = "O"
	game.board[2] = "X"
	game.board[3] = "X"
	game.board[4] = "O"
	game.board[5] = "O"
	game.board[6] = "O"
	game.board[7] = "X"
	game.board[8] = "X"
	game.moves = 9
	game.updateStatus()
	game.mu.Unlock()

	if game.status != "draw" {
		t.Errorf("Expected status 'draw', got '%s'", game.status)
	}
}

func TestTicTacToe_AIBlocksWin(t *testing.T) {
	game := &TicTacToe{}
	game.OnInit(game, "test-game")
	game.OnCreated()

	// Set up a scenario where AI needs to block
	// Player has two in a row and AI should block
	game.mu.Lock()
	game.board[0] = "X"
	game.board[1] = "X"
	// Position 2 is empty - AI should block here
	game.moves = 2
	game.mu.Unlock()

	move := game.findAIMove()
	if move != 2 {
		t.Errorf("AI should block at position 2, but chose %d", move)
	}
}

func TestTicTacToe_AITakesWin(t *testing.T) {
	game := &TicTacToe{}
	game.OnInit(game, "test-game")
	game.OnCreated()

	// Set up a scenario where AI can win
	game.mu.Lock()
	game.board[0] = "O"
	game.board[1] = "O"
	// Position 2 is empty - AI should win here
	game.board[3] = "X"
	game.board[4] = "X"
	game.moves = 4
	game.mu.Unlock()

	move := game.findAIMove()
	if move != 2 {
		t.Errorf("AI should take winning move at position 2, but chose %d", move)
	}
}

func TestTicTacToe_AITakesCenter(t *testing.T) {
	game := &TicTacToe{}
	game.OnInit(game, "test-game")
	game.OnCreated()

	// Player takes a corner, AI should take center
	game.mu.Lock()
	game.board[0] = "X"
	game.moves = 1
	game.mu.Unlock()

	move := game.findAIMove()
	if move != 4 {
		t.Errorf("AI should take center (4), but chose %d", move)
	}
}
