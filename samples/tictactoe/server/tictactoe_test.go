package main

import (
	"context"
	"testing"
	"time"

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

func TestTicTacToeService_CleanupFinishedGames(t *testing.T) {
	service := &TicTacToeService{}
	service.OnInit(service, "test-service")
	// Initialize games map directly to avoid starting cleanup routine
	service.games = make(map[string]*TicTacToeGame)
	service.stopCh = make(chan struct{})

	// Create a finished game
	game1 := newGame("finished-game")
	game1.status = "x_wins"
	service.games["finished-game"] = game1

	// Create an active game
	game2 := newGame("active-game")
	service.games["active-game"] = game2

	// Verify both games exist
	if len(service.games) != 2 {
		t.Fatalf("Expected 2 games, got %d", len(service.games))
	}

	// Run cleanup
	service.cleanupGames()

	// Verify only active game remains
	if len(service.games) != 1 {
		t.Fatalf("Expected 1 game after cleanup, got %d", len(service.games))
	}

	if _, exists := service.games["active-game"]; !exists {
		t.Errorf("Expected active-game to still exist")
	}

	if _, exists := service.games["finished-game"]; exists {
		t.Errorf("Expected finished-game to be cleaned up")
	}
}

func TestTicTacToeService_CleanupExpiredGames(t *testing.T) {
	service := &TicTacToeService{}
	service.OnInit(service, "test-service")
	// Initialize games map directly to avoid starting cleanup routine
	service.games = make(map[string]*TicTacToeGame)
	service.stopCh = make(chan struct{})

	// Create an expired game (older than maxGameAge)
	expiredGame := newGame("expired-game")
	expiredGame.createdAt = time.Now().Add(-15 * time.Minute) // 15 minutes old
	service.games["expired-game"] = expiredGame

	// Create a fresh game
	freshGame := newGame("fresh-game")
	service.games["fresh-game"] = freshGame

	// Verify both games exist
	if len(service.games) != 2 {
		t.Fatalf("Expected 2 games, got %d", len(service.games))
	}

	// Run cleanup
	service.cleanupGames()

	// Verify only fresh game remains
	if len(service.games) != 1 {
		t.Fatalf("Expected 1 game after cleanup, got %d", len(service.games))
	}

	if _, exists := service.games["fresh-game"]; !exists {
		t.Errorf("Expected fresh-game to still exist")
	}

	if _, exists := service.games["expired-game"]; exists {
		t.Errorf("Expected expired-game to be cleaned up")
	}
}

func TestTicTacToeService_Persistence(t *testing.T) {
	service := &TicTacToeService{}
	service.OnInit(service, "test-service")
	// Initialize games map directly to avoid starting cleanup routine
	service.games = make(map[string]*TicTacToeGame)
	service.stopCh = make(chan struct{})

	// Create a game with some state
	game := newGame("persist-game")
	game.board[0] = "X"
	game.board[4] = "O"
	game.board[1] = "X"
	game.moves = 3
	game.status = "playing"
	game.lastAIMove = 4
	service.games["persist-game"] = game

	// Serialize to data
	data, err := service.ToData()
	if err != nil {
		t.Fatalf("ToData failed: %v", err)
	}

	if data == nil {
		t.Fatalf("ToData returned nil data")
	}

	// Create a new service and restore from data
	service2 := &TicTacToeService{}
	service2.OnInit(service2, "test-service2")
	service2.games = make(map[string]*TicTacToeGame)
	service2.stopCh = make(chan struct{})

	err = service2.FromData(data)
	if err != nil {
		t.Fatalf("FromData failed: %v", err)
	}

	// Verify the game was restored
	if len(service2.games) != 1 {
		t.Fatalf("Expected 1 game after FromData, got %d", len(service2.games))
	}

	restoredGame, exists := service2.games["persist-game"]
	if !exists {
		t.Fatalf("Expected persist-game to exist after FromData")
	}

	if restoredGame.board[0] != "X" {
		t.Errorf("Expected board[0] = 'X', got '%s'", restoredGame.board[0])
	}

	if restoredGame.board[4] != "O" {
		t.Errorf("Expected board[4] = 'O', got '%s'", restoredGame.board[4])
	}

	if restoredGame.board[1] != "X" {
		t.Errorf("Expected board[1] = 'X', got '%s'", restoredGame.board[1])
	}

	if restoredGame.moves != 3 {
		t.Errorf("Expected moves = 3, got %d", restoredGame.moves)
	}

	if restoredGame.status != "playing" {
		t.Errorf("Expected status = 'playing', got '%s'", restoredGame.status)
	}

	if restoredGame.lastAIMove != 4 {
		t.Errorf("Expected lastAIMove = 4, got %d", restoredGame.lastAIMove)
	}
}

func TestTicTacToeService_Persistence_MultipleGames(t *testing.T) {
	service := &TicTacToeService{}
	service.OnInit(service, "test-service")
	// Initialize games map directly to avoid starting cleanup routine
	service.games = make(map[string]*TicTacToeGame)
	service.stopCh = make(chan struct{})

	// Create multiple games with different states
	game1 := newGame("game-1")
	game1.board[0] = "X"
	game1.status = "playing"
	service.games["game-1"] = game1

	game2 := newGame("game-2")
	game2.board[4] = "O"
	game2.status = "o_wins"
	service.games["game-2"] = game2

	// Serialize to data
	data, err := service.ToData()
	if err != nil {
		t.Fatalf("ToData failed: %v", err)
	}

	// Create a new service and restore from data
	service2 := &TicTacToeService{}
	service2.OnInit(service2, "test-service2")
	service2.games = make(map[string]*TicTacToeGame)
	service2.stopCh = make(chan struct{})

	err = service2.FromData(data)
	if err != nil {
		t.Fatalf("FromData failed: %v", err)
	}

	// Verify both games were restored
	if len(service2.games) != 2 {
		t.Fatalf("Expected 2 games after FromData, got %d", len(service2.games))
	}

	restoredGame1, exists := service2.games["game-1"]
	if !exists {
		t.Fatalf("Expected game-1 to exist after FromData")
	}
	if restoredGame1.board[0] != "X" {
		t.Errorf("game-1: Expected board[0] = 'X', got '%s'", restoredGame1.board[0])
	}

	restoredGame2, exists := service2.games["game-2"]
	if !exists {
		t.Fatalf("Expected game-2 to exist after FromData")
	}
	if restoredGame2.status != "o_wins" {
		t.Errorf("game-2: Expected status = 'o_wins', got '%s'", restoredGame2.status)
	}
}

func TestTicTacToeService_Persistence_EmptyGames(t *testing.T) {
	service := &TicTacToeService{}
	service.OnInit(service, "test-service")
	// Initialize games map directly to avoid starting cleanup routine
	service.games = make(map[string]*TicTacToeGame)
	service.stopCh = make(chan struct{})

	// Serialize empty games to data
	data, err := service.ToData()
	if err != nil {
		t.Fatalf("ToData failed: %v", err)
	}

	// Create a new service and restore from data
	service2 := &TicTacToeService{}
	service2.OnInit(service2, "test-service2")
	service2.games = make(map[string]*TicTacToeGame)
	service2.stopCh = make(chan struct{})

	err = service2.FromData(data)
	if err != nil {
		t.Fatalf("FromData failed: %v", err)
	}

	// Verify no games were restored
	if len(service2.games) != 0 {
		t.Errorf("Expected 0 games after FromData with empty data, got %d", len(service2.games))
	}
}

func TestTicTacToeGame_CreatedAtOnReset(t *testing.T) {
	game := newGame("test-game")
	originalTime := game.createdAt

	// Wait a tiny bit and reset
	time.Sleep(1 * time.Millisecond)
	game.reset()

	// createdAt should be updated
	if !game.createdAt.After(originalTime) {
		t.Errorf("Expected createdAt to be updated after reset")
	}
}
