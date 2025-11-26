// Tic Tac Toe Game Client
// Communicates with Goverse HTTP Gate

const API_BASE = 'http://localhost:8080/api/v1';
const OBJECT_TYPE = 'TicTacToe';

let gameID = null;
let gameState = null;
let isLoading = false;

// Initialize game on page load
document.addEventListener('DOMContentLoaded', () => {
    initGame();
});

// Initialize or restore game
async function initGame() {
    // Get or create game ID from localStorage
    gameID = localStorage.getItem('tictactoe_game_id');
    if (!gameID) {
        gameID = chooseGameID();
        localStorage.setItem('tictactoe_game_id', gameID);
    }
    
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

// Choose a game ID from the fixed pool (1-10)
function chooseGameID() {
    const gameNumber = Math.floor(Math.random() * 10) + 1;
    return 'game-' + gameNumber;
}

// Update game ID display
function updateGameIDDisplay() {
    document.getElementById('game-id').textContent = 'Game: ' + gameID;
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
        const request = createMoveRequest(position);
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
        // First ensure the object exists
        await createObjectIfNeeded();
        
        // Call NewGame method
        const request = createEmptyRequest();
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
        const request = createEmptyRequest();
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

// Create object if it doesn't exist
async function createObjectIfNeeded() {
    try {
        const url = `${API_BASE}/objects/create/${OBJECT_TYPE}/${gameID}`;
        const response = await fetch(url, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            }
        });
        
        const data = await response.json();
        if (!response.ok && !data.error?.includes('already exists')) {
            console.log('Create response:', data);
        }
    } catch (error) {
        console.log('Create object error (may already exist):', error);
    }
}

// Call object method via HTTP Gate
async function callObject(method, requestBytes) {
    const url = `${API_BASE}/objects/call/${OBJECT_TYPE}/${gameID}/${method}`;
    
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

// Create MoveRequest protobuf (base64 encoded Any)
function createMoveRequest(position) {
    // MoveRequest: position (int32, field 1)
    // Protobuf encoding: field_number << 3 | wire_type
    // int32 uses wire type 0 (varint)
    const moveReqBytes = new Uint8Array([
        0x08, // field 1, wire type 0 (varint): (1 << 3) | 0 = 8
        position & 0x7F // varint encoding of position (0-8 fits in one byte)
    ]);
    
    // Wrap in google.protobuf.Any
    // Any has two fields:
    // 1. type_url (string, field 1)
    // 2. value (bytes, field 2)
    const typeUrl = 'type.googleapis.com/tictactoe.MoveRequest';
    const typeUrlBytes = new TextEncoder().encode(typeUrl);
    
    // Build Any message
    const anyBytes = new Uint8Array([
        // Field 1: type_url (string)
        0x0A, // (1 << 3) | 2 = 10
        typeUrlBytes.length, // length
        ...typeUrlBytes,
        // Field 2: value (bytes)
        0x12, // (2 << 3) | 2 = 18
        moveReqBytes.length, // length
        ...moveReqBytes
    ]);
    
    return base64Encode(anyBytes);
}

// Create empty request (for NewGame and GetState)
// These methods don't use the position field, so we send an empty MoveRequest
function createEmptyRequest() {
    // Create an empty MoveRequest protobuf wrapped in Any
    // Empty message has no fields, so just the type URL and empty value
    const typeUrl = 'type.googleapis.com/tictactoe.MoveRequest';
    const typeUrlBytes = new TextEncoder().encode(typeUrl);
    
    // Build Any message with empty value
    const anyBytes = new Uint8Array([
        // Field 1: type_url (string)
        0x0A, // (1 << 3) | 2 = 10
        typeUrlBytes.length, // length
        ...typeUrlBytes,
        // Field 2: value (bytes) - empty
        0x12, // (2 << 3) | 2 = 18
        0x00  // length = 0
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
                // Skip varint - continue while high bit is set
                while (offset < bytes.length && (bytes[offset] & 0x80)) {
                    offset++;
                }
                // Skip the last byte (with high bit not set)
                if (offset < bytes.length) {
                    offset++;
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
function parseGameStateFromBytes(bytes) {
    let offset = 0;
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
            // board (repeated string)
            const len = bytes[offset++];
            if (len > 0) {
                board[boardIndex] = new TextDecoder().decode(bytes.slice(offset, offset + len));
            }
            boardIndex++;
            offset += len;
        } else if (fieldNumber === 2 && wireType === 2) {
            // status (string)
            const len = bytes[offset++];
            status = new TextDecoder().decode(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 3 && wireType === 2) {
            // winner (string)
            const len = bytes[offset++];
            winner = new TextDecoder().decode(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 4 && wireType === 0) {
            // last_ai_move (int32, varint)
            // Protobuf int32 uses standard varint encoding
            // Negative values are encoded as 10-byte varints (full 64-bit two's complement)
            let value = 0;
            let shift = 0;
            let b;
            do {
                b = bytes[offset++];
                value |= (b & 0x7F) << shift;
                shift += 7;
            } while (b & 0x80);
            // For int32, negative values have the sign bit set (bit 31)
            // JavaScript bitwise operations treat numbers as 32-bit signed integers
            // Using bitwise OR with 0 converts to 32-bit signed integer
            lastAiMove = value | 0;
        } else {
            // Skip unknown field
            if (wireType === 0) {
                // Skip varint - continue while high bit is set
                while (offset < bytes.length && (bytes[offset] & 0x80)) {
                    offset++;
                }
                // Skip the last byte (with high bit not set)
                if (offset < bytes.length) {
                    offset++;
                }
            } else if (wireType === 2) {
                const len = bytes[offset++];
                offset += len;
            }
        }
    }
    
    return { board, status, winner, lastAiMove };
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
