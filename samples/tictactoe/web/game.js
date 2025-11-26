// Tic Tac Toe Game Client
// Communicates with Goverse HTTP Gate

const API_BASE = 'http://localhost:8080/api/v1';
const OBJECT_TYPE = 'TicTacToeService';

let serviceID = null;  // Which service object to talk to (service-1 to service-10)
let userGameID = null; // Unique game ID for this user's session
let gameState = null;
let isLoading = false;

// Initialize game on page load
document.addEventListener('DOMContentLoaded', () => {
    initGame();
});

// Initialize or restore game
async function initGame() {
    // Get or create user's game ID from localStorage
    userGameID = localStorage.getItem('tictactoe_user_game_id');
    if (!userGameID) {
        userGameID = generateUserGameID();
        localStorage.setItem('tictactoe_user_game_id', userGameID);
    }
    
    // Choose a service object to talk to (1-10)
    serviceID = chooseServiceID();
    
    updateGameIDDisplay();
    
    // Setup cell click handlers
    const cells = document.querySelectorAll('.cell');
    cells.forEach(cell => {
        cell.addEventListener('click', () => handleCellClick(cell));
    });
    
    // Try to get current state, start new game if needed
    try {
        await getGameState();
    } catch (error) {
        console.log('Could not get state, starting new game:', error);
        await startNewGame();
    }
}

// Generate a unique game ID for this user
function generateUserGameID() {
    return 'user-' + Math.random().toString(36).substr(2, 9) + '-' + Date.now();
}

// Choose a service ID from the fixed pool (1-10)
function chooseServiceID() {
    const serviceNumber = Math.floor(Math.random() * 10) + 1;
    return 'service-' + serviceNumber;
}

// Update game ID display
function updateGameIDDisplay() {
    document.getElementById('game-id').textContent = 'Game: ' + userGameID.substring(0, 15) + '...';
}

// Handle cell click
async function handleCellClick(cell) {
    if (isLoading) return;
    
    const index = parseInt(cell.dataset.index);
    
    // Check if game is still playing
    if (gameState && gameState.status !== 'playing') {
        return;
    }
    
    // Check if cell is empty
    if (gameState && gameState.board[index] !== '') {
        return;
    }
    
    await makeMove(index);
}

// Make a move
async function makeMove(position) {
    setLoading(true);
    setStatus('Making move...');
    
    try {
        // Create MoveRequest protobuf
        const request = createMoveRequest(userGameID, position);
        const response = await callObject('MakeMove', request);
        gameState = parseGameState(response);
        updateBoard();
        updateStatus();
    } catch (error) {
        console.error('Move failed:', error);
        setStatus('Error: ' + error.message);
    } finally {
        setLoading(false);
    }
}

// Start a new game
async function startNewGame() {
    setLoading(true);
    setStatus('Starting new game...');
    
    try {
        // Call NewGame method with our game ID
        const request = createNewGameRequest(userGameID);
        const response = await callObject('NewGame', request);
        gameState = parseGameState(response);
        updateBoard();
        updateStatus();
    } catch (error) {
        console.error('Failed to start new game:', error);
        setStatus('Error: ' + error.message);
    } finally {
        setLoading(false);
    }
}

// Get current game state
async function getGameState() {
    setLoading(true);
    
    try {
        const request = createGetStateRequest(userGameID);
        const response = await callObject('GetState', request);
        gameState = parseGameState(response);
        updateBoard();
        updateStatus();
    } catch (error) {
        console.error('Failed to get state:', error);
        throw error;
    } finally {
        setLoading(false);
    }
}

// Call object method via HTTP Gate
async function callObject(method, requestBytes) {
    const url = `${API_BASE}/objects/call/${OBJECT_TYPE}/${serviceID}/${method}`;
    
    const response = await fetch(url, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({
            request: requestBytes
        })
    });
    
    const data = await response.json();
    
    if (!response.ok) {
        throw new Error(data.error || 'Request failed');
    }
    
    return data.response;
}

// Create NewGameRequest protobuf (base64 encoded Any)
function createNewGameRequest(gameId) {
    // NewGameRequest: game_id (string, field 1)
    const gameIdBytes = new TextEncoder().encode(gameId);
    const reqBytes = new Uint8Array([
        0x0A, // field 1, wire type 2 (length-delimited): (1 << 3) | 2 = 10
        gameIdBytes.length,
        ...gameIdBytes
    ]);
    
    return wrapInAny('tictactoe.NewGameRequest', reqBytes);
}

// Create MoveRequest protobuf (base64 encoded Any)
function createMoveRequest(gameId, position) {
    // MoveRequest: game_id (string, field 1), position (int32, field 2)
    const gameIdBytes = new TextEncoder().encode(gameId);
    const reqBytes = new Uint8Array([
        0x0A, // field 1, wire type 2 (length-delimited): (1 << 3) | 2 = 10
        gameIdBytes.length,
        ...gameIdBytes,
        0x10, // field 2, wire type 0 (varint): (2 << 3) | 0 = 16
        position & 0x7F // varint encoding of position (0-8 fits in one byte)
    ]);
    
    return wrapInAny('tictactoe.MoveRequest', reqBytes);
}

// Create GetStateRequest protobuf (base64 encoded Any)
function createGetStateRequest(gameId) {
    // GetStateRequest: game_id (string, field 1)
    const gameIdBytes = new TextEncoder().encode(gameId);
    const reqBytes = new Uint8Array([
        0x0A, // field 1, wire type 2 (length-delimited): (1 << 3) | 2 = 10
        gameIdBytes.length,
        ...gameIdBytes
    ]);
    
    return wrapInAny('tictactoe.GetStateRequest', reqBytes);
}

// Wrap a message in google.protobuf.Any
function wrapInAny(typeName, messageBytes) {
    const typeUrl = 'type.googleapis.com/' + typeName;
    const typeUrlBytes = new TextEncoder().encode(typeUrl);
    
    // Build Any message
    const anyBytes = new Uint8Array([
        // Field 1: type_url (string)
        0x0A, // (1 << 3) | 2 = 10
        typeUrlBytes.length,
        ...typeUrlBytes,
        // Field 2: value (bytes)
        0x12, // (2 << 3) | 2 = 18
        messageBytes.length,
        ...messageBytes
    ]);
    
    return base64Encode(anyBytes);
}

// Parse GameState from response
function parseGameState(responseBase64) {
    const bytes = base64Decode(responseBase64);
    
    // Parse Any message
    const any = parseAny(bytes);
    
    // Parse GameState from Any.value
    return parseGameStateFromBytes(any.value);
}

// Parse google.protobuf.Any
function parseAny(bytes) {
    let offset = 0;
    let typeUrl = '';
    let value = new Uint8Array(0);
    
    while (offset < bytes.length) {
        const fieldWire = bytes[offset++];
        const fieldNumber = fieldWire >> 3;
        const wireType = fieldWire & 0x07;
        
        if (fieldNumber === 1 && wireType === 2) {
            // type_url (string)
            const len = bytes[offset++];
            typeUrl = new TextDecoder().decode(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 2 && wireType === 2) {
            // value (bytes)
            const len = bytes[offset++];
            value = bytes.slice(offset, offset + len);
            offset += len;
        } else {
            // Skip unknown field
            if (wireType === 0) {
                // Skip varint bytes: each byte except the last has the high bit (0x80) set
                // We skip all continuation bytes then skip the final byte
                while (offset < bytes.length) {
                    const b = bytes[offset++];
                    if ((b & 0x80) === 0) break; // Last byte of varint
                }
            } else if (wireType === 2) {
                const len = bytes[offset++];
                offset += len;
            }
        }
    }
    
    return { typeUrl, value };
}

// Parse GameState protobuf
// GameState fields:
//   1: game_id (string)
//   2: board (repeated string)
//   3: status (string)
//   4: winner (string)
//   5: last_ai_move (int32)
function parseGameStateFromBytes(bytes) {
    let offset = 0;
    let gameId = '';
    const board = ['', '', '', '', '', '', '', '', ''];
    let status = 'playing';
    let winner = '';
    let lastAiMove = -1;
    let boardIndex = 0;
    
    while (offset < bytes.length) {
        const fieldWire = bytes[offset++];
        const fieldNumber = fieldWire >> 3;
        const wireType = fieldWire & 0x07;
        
        if (fieldNumber === 1 && wireType === 2) {
            // game_id (string)
            const len = bytes[offset++];
            gameId = new TextDecoder().decode(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 2 && wireType === 2) {
            // board (repeated string)
            const len = bytes[offset++];
            if (len > 0) {
                board[boardIndex] = new TextDecoder().decode(bytes.slice(offset, offset + len));
            }
            boardIndex++;
            offset += len;
        } else if (fieldNumber === 3 && wireType === 2) {
            // status (string)
            const len = bytes[offset++];
            status = new TextDecoder().decode(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 4 && wireType === 2) {
            // winner (string)
            const len = bytes[offset++];
            winner = new TextDecoder().decode(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 5 && wireType === 0) {
            // last_ai_move (int32, varint)
            let value = 0;
            let shift = 0;
            let b;
            do {
                b = bytes[offset++];
                value |= (b & 0x7F) << shift;
                shift += 7;
            } while (b & 0x80);
            // Convert to signed 32-bit integer
            lastAiMove = value | 0;
        } else {
            // Skip unknown field
            if (wireType === 0) {
                while (offset < bytes.length) {
                    const b = bytes[offset++];
                    if ((b & 0x80) === 0) break;
                }
            } else if (wireType === 2) {
                const len = bytes[offset++];
                offset += len;
            }
        }
    }
    
    return { gameId, board, status, winner, lastAiMove };
}

// Base64 encode
function base64Encode(bytes) {
    let binary = '';
    for (let i = 0; i < bytes.length; i++) {
        binary += String.fromCharCode(bytes[i]);
    }
    return btoa(binary);
}

// Base64 decode
function base64Decode(str) {
    const binary = atob(str);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
        bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
}

// Update the board display
function updateBoard() {
    const cells = document.querySelectorAll('.cell');
    cells.forEach((cell, index) => {
        const value = gameState.board[index];
        cell.textContent = value;
        cell.className = 'cell';
        if (value) {
            cell.classList.add(value);
        }
        if (gameState.lastAiMove === index) {
            cell.classList.add('last-ai-move');
        }
    });
    
    // Highlight winning cells if there's a winner
    if (gameState.winner) {
        highlightWinningCells();
    }
}

// Highlight winning cells
function highlightWinningCells() {
    const winPatterns = [
        [0, 1, 2], [3, 4, 5], [6, 7, 8], // rows
        [0, 3, 6], [1, 4, 7], [2, 5, 8], // columns
        [0, 4, 8], [2, 4, 6]  // diagonals
    ];
    
    const cells = document.querySelectorAll('.cell');
    
    for (const pattern of winPatterns) {
        const [a, b, c] = pattern;
        if (gameState.board[a] && 
            gameState.board[a] === gameState.board[b] && 
            gameState.board[b] === gameState.board[c]) {
            cells[a].classList.add('winning');
            cells[b].classList.add('winning');
            cells[c].classList.add('winning');
            break;
        }
    }
}

// Update status message
function updateStatus() {
    const statusEl = document.getElementById('status');
    statusEl.className = 'status';
    
    switch (gameState.status) {
        case 'playing':
            setStatus('Your turn!');
            break;
        case 'x_wins':
            setStatus('🎉 You win!');
            statusEl.classList.add('win');
            break;
        case 'o_wins':
            setStatus('😞 AI wins!');
            statusEl.classList.add('lose');
            break;
        case 'draw':
            setStatus('🤝 It\'s a draw!');
            statusEl.classList.add('draw');
            break;
        default:
            setStatus('Your turn!');
    }
}

// Set status message
function setStatus(message) {
    document.getElementById('status').textContent = message;
}

// Set loading state
function setLoading(loading) {
    isLoading = loading;
    document.body.classList.toggle('loading', loading);
}
