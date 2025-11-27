#!/usr/bin/env python3
"""
Test script for Goverse TicTacToe server.

This script:
1. Builds and runs the TicTacToe server
2. Waits for the server to be ready
3. Uses HTTP REST API to start a new game with a random client ID and random TicTacToeService object
4. Plays a complete game by making random moves until the game ends
5. Stops the server and verifies it quits properly
"""

import base64
import json
import os
import random
import signal
import socket
import subprocess
import sys
import time
import uuid
from pathlib import Path

# Repo root (tests/integration/tictactoe/test_tictactoe.py -> repo root)
REPO_ROOT = Path(__file__).parent.parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT / 'tests' / 'integration'))

from BinaryHelper import BinaryHelper

# Import protobuf libraries
try:
    from google.protobuf import any_pb2
    from google.protobuf.message import Message
except ImportError:
    print("ERROR: protobuf library not found. Install with: pip install protobuf")
    sys.exit(1)

# Import TicTacToe protobuf messages
PROTO_PYTHON_DIR = REPO_ROOT / 'samples' / 'tictactoe' / 'proto' / 'python'
sys.path.insert(0, str(PROTO_PYTHON_DIR))

try:
    import tictactoe_pb2
except ImportError as e:
    print(f"ERROR: Failed to import tictactoe_pb2: {e}")
    print("Run: ./samples/tictactoe/proto/generate_python.sh")
    sys.exit(1)

# Server ports
NODE_PORT = 50051
GATE_PORT = 49000
HTTP_PORT = 8080

# Binary path
TICTACTOE_BINARY = '/tmp/tictactoe_server'


def generate_client_id():
    """Generate a random client ID."""
    return f"client-{uuid.uuid4().hex[:12]}"


def generate_game_id():
    """Generate a unique game ID for this test."""
    return f"test-{uuid.uuid4().hex[:8]}-{int(time.time())}"


def choose_service_id():
    """Choose a random TicTacToeService object (service-1 to service-10)."""
    service_number = random.randint(1, 10)
    return f"service-{service_number}"


def build_server():
    """Build the TicTacToe server binary."""
    return BinaryHelper.build_binary(
        './samples/tictactoe/server/',
        TICTACTOE_BINARY,
        'TicTacToe server'
    )


def check_port(port, timeout=30):
    """Wait for a port to be available."""
    start_time = time.time()
    while time.time() - start_time < timeout:
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(1)
            result = sock.connect_ex(('localhost', port))
            sock.close()
            if result == 0:
                return True
        except Exception:
            pass
        time.sleep(1)
    return False


def create_new_game_request(game_id):
    """Create a NewGameRequest protobuf wrapped in Any, base64 encoded."""
    # Create the protobuf message
    req = tictactoe_pb2.NewGameRequest(game_id=game_id)
    
    # Wrap in google.protobuf.Any
    any_msg = any_pb2.Any()
    any_msg.Pack(req)
    
    # Serialize and encode to base64
    return base64.b64encode(any_msg.SerializeToString()).decode('ascii')


def create_move_request(game_id, position):
    """Create a MoveRequest protobuf wrapped in Any, base64 encoded."""
    # Create the protobuf message
    req = tictactoe_pb2.MoveRequest(game_id=game_id, position=position)
    
    # Wrap in google.protobuf.Any
    any_msg = any_pb2.Any()
    any_msg.Pack(req)
    
    # Serialize and encode to base64
    return base64.b64encode(any_msg.SerializeToString()).decode('ascii')


def parse_game_state_response(response_base64):
    """Parse the GameState from a base64-encoded Any response.
    
    Returns a dict with game_id, board, status, winner, last_ai_move.
    """
    # Decode base64 and parse Any message
    response_bytes = base64.b64decode(response_base64)
    any_msg = any_pb2.Any()
    any_msg.ParseFromString(response_bytes)
    
    # Unpack to GameState
    game_state = tictactoe_pb2.GameState()
    any_msg.Unpack(game_state)
    
    return {
        'game_id': game_state.game_id,
        'board': list(game_state.board),
        'status': game_state.status,
        'winner': game_state.winner,
        'last_ai_move': game_state.last_ai_move
    }


def print_board(board):
    """Print the TicTacToe board in a readable format."""
    print("\n  Board:")
    for row in range(3):
        cells = []
        for col in range(3):
            pos = row * 3 + col
            cell = board[pos] if board[pos] else str(pos)
            cells.append(cell)
        print(f"    {cells[0]} | {cells[1]} | {cells[2]}")
        if row < 2:
            print("   -----------")


def call_new_game(service_id, game_id, client_id):
    """Call the NewGame method via HTTP REST API.
    
    Returns the parsed GameState response.
    """
    import urllib.request
    import urllib.error
    
    url = f'http://localhost:{HTTP_PORT}/api/v1/objects/call/TicTacToeService/{service_id}/NewGame'
    request_body = json.dumps({
        'request': create_new_game_request(game_id)
    }).encode('utf-8')
    
    req = urllib.request.Request(
        url,
        data=request_body,
        headers={
            'Content-Type': 'application/json',
            'X-Client-ID': client_id
        },
        method='POST'
    )
    
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            response_data = json.loads(response.read().decode('utf-8'))
            return parse_game_state_response(response_data['response'])
    except urllib.error.HTTPError as e:
        error_body = e.read().decode('utf-8')
        raise RuntimeError(f"HTTP {e.code}: {error_body}")


def call_make_move(service_id, game_id, position, client_id):
    """Call the MakeMove method via HTTP REST API.
    
    Returns the parsed GameState response.
    """
    import urllib.request
    import urllib.error
    
    url = f'http://localhost:{HTTP_PORT}/api/v1/objects/call/TicTacToeService/{service_id}/MakeMove'
    request_body = json.dumps({
        'request': create_move_request(game_id, position)
    }).encode('utf-8')
    
    req = urllib.request.Request(
        url,
        data=request_body,
        headers={
            'Content-Type': 'application/json',
            'X-Client-ID': client_id
        },
        method='POST'
    )
    
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            response_data = json.loads(response.read().decode('utf-8'))
            return parse_game_state_response(response_data['response'])
    except urllib.error.HTTPError as e:
        error_body = e.read().decode('utf-8')
        raise RuntimeError(f"HTTP {e.code}: {error_body}")


def wait_for_cluster_ready(service_id, game_id, client_id, timeout=60):
    """Wait for the cluster to be ready by polling NewGame until it succeeds.
    
    Returns the GameState on success, raises RuntimeError on timeout.
    """
    import urllib.error
    
    start_time = time.time()
    last_error = None
    
    while time.time() - start_time < timeout:
        try:
            game_state = call_new_game(service_id, game_id, client_id)
            return game_state
        except RuntimeError as e:
            last_error = e
            error_str = str(e)
            # Check if the error is about cluster not ready (shard not assigned)
            if 'no node assigned to shard' in error_str or 'CALL_FAILED' in error_str:
                print(f"   Cluster not ready yet, retrying... ({error_str[:80]})")
                time.sleep(2)
                continue
            # For other errors, raise immediately
            raise
        except Exception as e:
            last_error = e
            print(f"   Error: {e}, retrying...")
            time.sleep(2)
            continue
    
    raise RuntimeError(f"Timeout waiting for cluster to be ready. Last error: {last_error}")


def start_server():
    """Start the TicTacToe server process."""
    print(f"Starting TicTacToe server...")
    
    # Start the process
    process = subprocess.Popen(
        [TICTACTOE_BINARY],
        stdout=None,
        stderr=None
    )
    print(f"✅ TicTacToe server started with PID: {process.pid}")
    return process


def stop_server(process):
    """Stop the TicTacToe server gracefully and return exit code."""
    if process is None:
        return -1
    
    if process.poll() is not None:
        return process.returncode if process.returncode is not None else -1
    
    print(f"Gracefully stopping TicTacToe server (PID: {process.pid})...")
    
    try:
        process.send_signal(signal.SIGINT)
        process.wait(timeout=10)
        return process.returncode if process.returncode is not None else -1
    except subprocess.TimeoutExpired:
        pass
    except Exception:
        pass
    
    try:
        process.terminate()
        process.wait(timeout=5)
        return process.returncode if process.returncode is not None else -1
    except subprocess.TimeoutExpired:
        pass
    except Exception:
        pass
    
    try:
        process.kill()
        process.wait()
    except Exception:
        pass
    
    return process.returncode if process.returncode is not None else -1


def main():
    """Main test execution."""
    # Change to repo root
    os.chdir(REPO_ROOT)
    print(f"Working directory: {os.getcwd()}")
    
    print("=" * 60)
    print("Goverse TicTacToe Server Test")
    print("=" * 60)
    print()
    
    process = None
    
    try:
        # Add go bin to PATH
        go_bin_path = subprocess.run(
            ['go', 'env', 'GOPATH'],
            capture_output=True, text=True, check=True
        ).stdout.strip()
        os.environ['PATH'] = f"{os.environ['PATH']}:{go_bin_path}/bin"
        
        # Step 1: Build the server
        print("\n--- Step 1: Building TicTacToe server ---")
        if not build_server():
            print("❌ Failed to build TicTacToe server")
            return 1
        
        # Step 2: Start the server
        print("\n--- Step 2: Starting TicTacToe server ---")
        process = start_server()
        
        # Step 3: Wait for server to be ready
        print("\n--- Step 3: Waiting for server to be ready ---")
        print(f"Checking HTTP port {HTTP_PORT}...")
        if not check_port(HTTP_PORT, timeout=30):
            print(f"❌ TicTacToe server HTTP port {HTTP_PORT} not available after 30 seconds")
            return 1
        print(f"✅ HTTP port {HTTP_PORT} is ready")
        
        # Step 4: Start a new game using HTTP API
        print("\n--- Step 4: Starting a new game ---")
        
        # Generate random identifiers
        client_id = generate_client_id()
        game_id = generate_game_id()
        service_id = choose_service_id()
        
        print(f"Client ID: {client_id}")
        print(f"Game ID: {game_id}")
        print(f"Service ID: {service_id}")
        
        try:
            # Wait for cluster to be ready and start new game
            print("Waiting for cluster to be ready...")
            game_state = wait_for_cluster_ready(service_id, game_id, client_id, timeout=60)
            print(f"✅ New game created successfully!")
            print(f"   Game ID: {game_state['game_id']}")
            print(f"   Status: {game_state['status']}")
            print(f"   Board: {game_state['board']}")
            
            # Verify the game state
            if game_state['game_id'] != game_id:
                print(f"❌ Game ID mismatch: expected {game_id}, got {game_state['game_id']}")
                return 1
            
            if game_state['status'] != 'playing':
                print(f"❌ Game status should be 'playing', got {game_state['status']}")
                return 1
            
            # Check that board is empty (all empty strings)
            if any(cell != '' for cell in game_state['board']):
                print(f"❌ Board should be empty for new game, got {game_state['board']}")
                return 1
            
            print("✅ Game state verified successfully!")
            print_board(game_state['board'])
            
        except Exception as e:
            print(f"❌ Failed to start new game: {e}")
            return 1
        
        # Step 5: Play the game until it ends
        print("\n--- Step 5: Playing the game ---")
        
        move_count = 0
        max_moves = 20  # Safety limit (shouldn't need more than 9 moves for a full board)
        
        try:
            while game_state['status'] == 'playing' and move_count < max_moves:
                # Find empty positions
                empty_positions = [i for i, cell in enumerate(game_state['board']) if cell == '']
                
                if not empty_positions:
                    print("❌ No empty positions but game status is still 'playing'")
                    return 1
                
                # Pick a random empty position
                position = random.choice(empty_positions)
                print(f"\n  Move {move_count + 1}: Player X plays at position {position}")
                
                # Make the move
                game_state = call_make_move(service_id, game_id, position, client_id)
                move_count += 1
                
                # Display the board
                print_board(game_state['board'])
                print(f"  Status: {game_state['status']}")
                
                if game_state['last_ai_move'] >= 0:
                    print(f"  AI (O) played at position {game_state['last_ai_move']}")
                
                if game_state['status'] != 'playing':
                    print(f"\n  🎮 Game ended: {game_state['status']}")
                    if game_state['winner']:
                        print(f"  🏆 Winner: {game_state['winner']}")
                    break
            
            if move_count >= max_moves:
                print(f"❌ Game did not end after {max_moves} moves")
                return 1
            
            # Verify game ended with a valid status
            valid_end_states = ['x_wins', 'o_wins', 'draw']
            if game_state['status'] not in valid_end_states:
                print(f"❌ Invalid end status: {game_state['status']}")
                return 1
            
            print(f"\n✅ Game completed successfully after {move_count} moves!")
            
        except Exception as e:
            print(f"❌ Failed during gameplay: {e}")
            import traceback
            traceback.print_exc()
            return 1
        
        # Step 6: Stop the server
        print("\n--- Step 5: Stopping TicTacToe server ---")
        exit_code = stop_server(process)
        process = None  # Prevent double cleanup
        
        print(f"TicTacToe server exited with code {exit_code}")
        
        if exit_code != 0:
            print(f"❌ TicTacToe server exited with non-zero status: {exit_code}")
            return 1
        
        print("\n" + "=" * 60)
        print("✅ All TicTacToe server tests passed!")
        print("=" * 60)
        return 0
        
    except KeyboardInterrupt:
        print("\n\nTest interrupted by user")
        return 1
    except Exception as e:
        print(f"\n❌ Unexpected error: {e}")
        import traceback
        traceback.print_exc()
        return 1
    finally:
        # Cleanup: stop server if still running
        if process is not None:
            stop_server(process)


if __name__ == '__main__':
    sys.exit(main())
