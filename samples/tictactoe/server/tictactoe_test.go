package main

import (
	"context"
	"testing"

	tictactoe_pb "github.com/xiaonanln/goverse/samples/tictactoe/proto"
)

func TestTicTacToeGame_NewGame(t *testing.T) {
	game := newGame("test-game")

	if game.status != "playing" {
		t.Errorf("Expected status 'playing', got '%s'", game.status)
	}

	for i, cell := range game.board {
		if cell != "" {
			t.Errorf("Expected empty cell at position %d, got '%s'", i, cell)
		}
	}

	if game.lastAIMove != -1 {
		t.Errorf("Expected lastAIMove -1, got %d", game.lastAIMove)
	}
}

func TestTicTacToeService_NewGame(t *testing.T) {
	service := &TicTacToeService{}
	service.OnInit(service, "test-service")
	service.OnCreated()

	state, err := service.NewGame(context.Background(), &tictactoe_pb.NewGameRequest{GameId: "user-123"})
	if err != nil {
		t.Fatalf("NewGame failed: %v", err)
	}

	if state.Status != "playing" {
		t.Errorf("Expected status 'playing', got '%s'", state.Status)
	}

	if state.GameId != "user-123" {
		t.Errorf("Expected GameId 'user-123', got '%s'", state.GameId)
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

func TestTicTacToeService_MakeMove_ValidMove(t *testing.T) {
	service := &TicTacToeService{}
	service.OnInit(service, "test-service")
	service.OnCreated()

	// Start a new game first
	service.NewGame(context.Background(), &tictactoe_pb.NewGameRequest{GameId: "user-123"})

	// Make a move at center
	state, err := service.MakeMove(context.Background(), &tictactoe_pb.MoveRequest{GameId: "user-123", Position: 4})
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

func TestTicTacToeService_MakeMove_InvalidPosition(t *testing.T) {
	service := &TicTacToeService{}
	service.OnInit(service, "test-service")
	service.OnCreated()

	// Start a new game first
	service.NewGame(context.Background(), &tictactoe_pb.NewGameRequest{GameId: "user-123"})

	// Try invalid position
	state, err := service.MakeMove(context.Background(), &tictactoe_pb.MoveRequest{GameId: "user-123", Position: 10})
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

func TestTicTacToeService_MakeMove_OccupiedPosition(t *testing.T) {
	service := &TicTacToeService{}
	service.OnInit(service, "test-service")
	service.OnCreated()

	// Start a new game first
	service.NewGame(context.Background(), &tictactoe_pb.NewGameRequest{GameId: "user-123"})

	// Make first move
	service.MakeMove(context.Background(), &tictactoe_pb.MoveRequest{GameId: "user-123", Position: 4})

	// Try to move to same position
	state, err := service.MakeMove(context.Background(), &tictactoe_pb.MoveRequest{GameId: "user-123", Position: 4})
	if err != nil {
		t.Fatalf("MakeMove failed: %v", err)
	}

	// Position 4 should still be X (not changed)
	if state.Board[4] != "X" {
		t.Errorf("Expected 'X' at position 4, got '%s'", state.Board[4])
	}
}

func TestTicTacToeService_GetState(t *testing.T) {
	service := &TicTacToeService{}
	service.OnInit(service, "test-service")
	service.OnCreated()

	// Start a new game first
	service.NewGame(context.Background(), &tictactoe_pb.NewGameRequest{GameId: "user-123"})

	// Make a move first
	service.MakeMove(context.Background(), &tictactoe_pb.MoveRequest{GameId: "user-123", Position: 0})

	// Get state
	state, err := service.GetState(context.Background(), &tictactoe_pb.GetStateRequest{GameId: "user-123"})
	if err != nil {
		t.Fatalf("GetState failed: %v", err)
	}

	// Verify state matches what we expect
	if state.Board[0] != "X" {
		t.Errorf("Expected 'X' at position 0, got '%s'", state.Board[0])
	}
}

func TestTicTacToeGame_WinCondition(t *testing.T) {
	game := newGame("test-game")

	// Manually set up a winning board for testing
	game.board[0] = "X"
	game.board[1] = "X"
	game.board[2] = "X"
	game.moves = 3
	game.updateStatus()

	if game.status != "x_wins" {
		t.Errorf("Expected status 'x_wins', got '%s'", game.status)
	}
}

func TestTicTacToeGame_DrawCondition(t *testing.T) {
	game := newGame("test-game")

	// Manually set up a draw board
	// X O X
	// X O O
	// O X X
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

	if game.status != "draw" {
		t.Errorf("Expected status 'draw', got '%s'", game.status)
	}
}

func TestTicTacToeGame_AIBlocksWin(t *testing.T) {
	game := newGame("test-game")

	// Set up a scenario where AI needs to block
	// Player has two in a row and AI should block
	game.board[0] = "X"
	game.board[1] = "X"
	// Position 2 is empty - AI should block here
	game.moves = 2

	move := game.findAIMove()
	if move != 2 {
		t.Errorf("AI should block at position 2, but chose %d", move)
	}
}

func TestTicTacToeGame_AITakesWin(t *testing.T) {
	game := newGame("test-game")

	// Set up a scenario where AI can win
	game.board[0] = "O"
	game.board[1] = "O"
	// Position 2 is empty - AI should win here
	game.board[3] = "X"
	game.board[4] = "X"
	game.moves = 4

	move := game.findAIMove()
	if move != 2 {
		t.Errorf("AI should take winning move at position 2, but chose %d", move)
	}
}

func TestTicTacToeGame_AITakesCenter(t *testing.T) {
	game := newGame("test-game")

	// Player takes a corner, AI should take center
	game.board[0] = "X"
	game.moves = 1

	move := game.findAIMove()
	if move != 4 {
		t.Errorf("AI should take center (4), but chose %d", move)
	}
}

func TestTicTacToeService_MultipleGames(t *testing.T) {
	service := &TicTacToeService{}
	service.OnInit(service, "test-service")
	service.OnCreated()

	// Create two different games
	service.NewGame(context.Background(), &tictactoe_pb.NewGameRequest{GameId: "game-1"})
	service.NewGame(context.Background(), &tictactoe_pb.NewGameRequest{GameId: "game-2"})

	// Make moves in game-1
	service.MakeMove(context.Background(), &tictactoe_pb.MoveRequest{GameId: "game-1", Position: 0})

	// Check game-1 state
	state1, _ := service.GetState(context.Background(), &tictactoe_pb.GetStateRequest{GameId: "game-1"})
	if state1.Board[0] != "X" {
		t.Errorf("Game-1 should have X at position 0")
	}

	// Check game-2 state - should be empty
	state2, _ := service.GetState(context.Background(), &tictactoe_pb.GetStateRequest{GameId: "game-2"})
	for i, cell := range state2.Board {
		if cell != "" {
			t.Errorf("Game-2 position %d should be empty, got '%s'", i, cell)
		}
	}
}
