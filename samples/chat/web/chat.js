// Chat Client for Goverse HTTP Gate
// Communicates with the gate server to interact with ChatRoom objects

// API base URL - can be overridden via query param ?api=...
const API_BASE = getApiBase();

function getApiBase() {
    // Check for override in query params
    const params = new URLSearchParams(window.location.search);
    const apiOverride = params.get('api');
    if (apiOverride) {
        return apiOverride;
    }
    
    // Default: same host as page, port 8080, same protocol
    const protocol = window.location.protocol;
    const hostname = window.location.hostname;
    
    // For GitHub Codespaces, the port is in the hostname (e.g., xxx-8080.app.github.dev)
    if (hostname.includes('.app.github.dev')) {
        // Replace the port in the hostname
        const baseHostname = hostname.replace(/-\d+\.app\.github\.dev$/, '');
        return `${protocol}//${baseHostname}-8080.app.github.dev/api/v1`;
    }
    
    // For local development
    return `${protocol}//${hostname}:8080/api/v1`;
}

// State
let userName = '';
let currentRoom = null;
let lastMsgTimestamp = 0;
let isLoading = false;
let displayedMessages = new Set(); // Track displayed messages to avoid duplicates

// SSE (Server-Sent Events) state
let eventSource = null;
let clientID = null;

// Room icons mapping
const roomIcons = {
    'General': '💬',
    'Technology': '💻',
    'Crypto': '🪙',
    'Sports': '⚽',
    'Movies': '🎬'
};

// Initialize on page load
document.addEventListener('DOMContentLoaded', () => {
    initChat();
});

// Initialize chat
function initChat() {
    // Load saved username
    userName = localStorage.getItem('chat_username') || '';
    document.getElementById('username-input').value = userName;
    
    // Setup event listeners
    document.getElementById('username-input').addEventListener('input', (e) => {
        userName = e.target.value.trim();
        localStorage.setItem('chat_username', userName);
    });
    
    document.getElementById('message-input').addEventListener('keypress', (e) => {
        if (e.key === 'Enter') {
            sendMessage();
        }
    });
    
    // Connect to SSE for push messages
    connectSSE();
    
    // Load chat rooms
    loadChatRooms();
}

// ==================== SSE (Server-Sent Events) ====================

// Connect to SSE endpoint for push messages
function connectSSE() {
    // Construct SSE URL based on API_BASE
    const sseUrl = API_BASE.replace(/\/api\/v1$/, '/api/v1/events/stream');
    console.log('Connecting to SSE:', sseUrl);
    
    eventSource = new EventSource(sseUrl);
    
    eventSource.addEventListener('register', (event) => {
        const data = JSON.parse(event.data);
        clientID = data.clientId;
        console.log('SSE registered with clientId:', clientID);
    });
    
    eventSource.addEventListener('message', (event) => {
        const data = JSON.parse(event.data);
        handlePushMessage(data);
    });
    
    eventSource.addEventListener('heartbeat', (event) => {
        console.log('SSE heartbeat received');
    });
    
    eventSource.onerror = (error) => {
        console.error('SSE error:', error);
        // Attempt to reconnect after a delay
        if (eventSource.readyState === EventSource.CLOSED) {
            console.log('SSE connection closed, reconnecting in 3 seconds...');
            setTimeout(connectSSE, 3000);
        }
    };
}

// Handle pushed message from SSE
function handlePushMessage(msgEvent) {
    console.log('Received push message:', msgEvent);
    
    // Check if the message type is a new message notification
    if (!msgEvent.type || !msgEvent.type.includes('Client_NewMessageNotification')) {
        console.log('Ignoring non-chat message:', msgEvent.type);
        return;
    }
    
    // Only process messages if we're in a chat room
    if (!currentRoom) {
        console.log('Ignoring message, not in a chat room');
        return;
    }
    
    // Decode the payload (base64-encoded protobuf Any)
    try {
        const payloadBytes = base64Decode(msgEvent.payload);
        const any = parseAny(payloadBytes);
        
        // Parse Client_NewMessageNotification: message (ChatMessage, field 1)
        let offset = 0;
        while (offset < any.value.length) {
            const fieldWire = any.value[offset++];
            const fieldNumber = fieldWire >> 3;
            const wireType = fieldWire & 0x07;
            
            if (fieldNumber === 1 && wireType === 2) {
                const lenResult = decodeVarint(any.value, offset);
                const len = lenResult.value;
                offset = lenResult.offset;
                const msgBytes = any.value.slice(offset, offset + len);
                const chatMsg = parseChatMessage(msgBytes);
                
                // Display the message
                if (displayMessage(chatMsg)) {
                    if (chatMsg.timestamp > lastMsgTimestamp) {
                        lastMsgTimestamp = chatMsg.timestamp;
                    }
                }
                offset += len;
            } else {
                offset = skipField(any.value, offset, wireType);
            }
        }
    } catch (error) {
        console.error('Failed to parse push message:', error);
    }
}

// Disconnect SSE
function disconnectSSE() {
    if (eventSource) {
        eventSource.close();
        eventSource = null;
        clientID = null;
        console.log('SSE disconnected');
    }
}

// Load available chat rooms
async function loadChatRooms() {
    setStatus('Loading rooms...');
    
    try {
        const request = createListChatRoomsRequest();
        const response = await callObject('ChatRoomMgr', 'ChatRoomMgr0', 'ListChatRooms', request);
        const rooms = parseListChatRoomsResponse(response);
        
        displayRoomList(rooms);
        setStatus('');
    } catch (error) {
        console.error('Failed to load rooms:', error);
        setStatus('Failed to load rooms: ' + error.message, 'error');
        // Show retry option
        document.getElementById('room-list').innerHTML = `
            <div class="loading-message">
                Failed to load rooms. <br>
                <button onclick="loadChatRooms()" style="margin-top: 10px; padding: 8px 16px; cursor: pointer;">Retry</button>
            </div>
        `;
    }
}

// Display the list of chat rooms
function displayRoomList(rooms) {
    const roomListEl = document.getElementById('room-list');
    
    if (rooms.length === 0) {
        roomListEl.innerHTML = '<div class="loading-message">No chat rooms available</div>';
        return;
    }
    
    roomListEl.innerHTML = rooms.map(room => `
        <div class="room-item" onclick="joinChatroom('${room}')">
            <span class="room-icon">${roomIcons[room] || '💬'}</span>
            <span>${room}</span>
        </div>
    `).join('');
}

// Join a chatroom
async function joinChatroom(roomName) {
    if (!userName) {
        setStatus('Please enter your name first', 'error');
        document.getElementById('username-input').focus();
        return;
    }
    
    if (!clientID) {
        setStatus('Connecting to server, please wait...', 'error');
        // Try to reconnect SSE
        connectSSE();
        return;
    }
    
    setLoading(true);
    setStatus('Joining ' + roomName + '...');
    
    try {
        const request = createJoinRequest(userName, clientID);
        const response = await callObject('ChatRoom', 'ChatRoom-' + roomName, 'Join', request);
        const joinData = parseJoinResponse(response);
        
        currentRoom = roomName;
        lastMsgTimestamp = 0;
        displayedMessages.clear(); // Clear displayed messages when joining new room
        
        // Switch to chat view
        document.getElementById('room-list-view').classList.add('hidden');
        document.getElementById('chat-view').classList.remove('hidden');
        document.getElementById('room-title').textContent = roomName;
        document.getElementById('user-display').textContent = userName;
        
        // Clear messages and show welcome messages
        const messagesEl = document.getElementById('messages');
        messagesEl.innerHTML = '';
        
        if (joinData.recentMessages) {
            joinData.recentMessages.forEach(msg => {
                displayMessage(msg);
                if (msg.timestamp > lastMsgTimestamp) {
                    lastMsgTimestamp = msg.timestamp;
                }
            });
        }
        
        // Messages will be received via SSE push - no polling needed
        
        setStatus('');
        
        // Focus on message input
        document.getElementById('message-input').focus();
    } catch (error) {
        console.error('Failed to join room:', error);
        setStatus('Failed to join room: ' + error.message, 'error');
    } finally {
        setLoading(false);
    }
}

// Leave the current chatroom
function leaveChatroom() {
    currentRoom = null;
    lastMsgTimestamp = 0;
    displayedMessages.clear(); // Clear displayed messages when leaving
    
    // Switch back to room list view
    document.getElementById('chat-view').classList.add('hidden');
    document.getElementById('room-list-view').classList.remove('hidden');
    
    setStatus('');
}

// Send a message
async function sendMessage() {
    const messageInput = document.getElementById('message-input');
    const message = messageInput.value.trim();
    
    if (!message) return;
    if (!currentRoom) return;
    
    // Clear input immediately for better UX
    messageInput.value = '';
    
    try {
        const request = createSendMessageRequest(userName, message);
        await callObject('ChatRoom', 'ChatRoom-' + currentRoom, 'SendMessage', request);
        // Message will be displayed when received via SSE push
    } catch (error) {
        console.error('Failed to send message:', error);
        setStatus('Failed to send message: ' + error.message, 'error');
        // Restore the message in input
        messageInput.value = message;
    }
}

// Generate a unique message ID for deduplication
function getMessageId(msg) {
    return `${msg.userName}|${msg.timestamp}|${msg.message}`;
}

// Display a message in the chat (returns true if displayed, false if duplicate)
function displayMessage(msg) {
    // Check for duplicate
    const msgId = getMessageId(msg);
    if (displayedMessages.has(msgId)) {
        return false;
    }
    displayedMessages.add(msgId);
    
    const messagesEl = document.getElementById('messages');
    
    const isOwn = msg.userName === userName;
    const isSystem = msg.userName === 'SYSTEM';
    
    let msgClass = 'message';
    if (isSystem) {
        msgClass += ' system';
    } else if (isOwn) {
        msgClass += ' own';
    } else {
        msgClass += ' other';
    }
    
    // Format timestamp (microseconds to readable time)
    const time = new Date(msg.timestamp / 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    
    const msgEl = document.createElement('div');
    msgEl.className = msgClass;
    
    if (isSystem) {
        msgEl.innerHTML = `
            <div class="message-text">${escapeHtml(msg.message)}</div>
        `;
    } else {
        msgEl.innerHTML = `
            ${!isOwn ? `<div class="message-sender">${escapeHtml(msg.userName)}</div>` : ''}
            <div class="message-text">${escapeHtml(msg.message)}</div>
            <div class="message-time">${time}</div>
        `;
    }
    
    messagesEl.appendChild(msgEl);
    
    // Scroll to bottom
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return true;
}

// ==================== API Communication ====================

// Call an object method via HTTP Gate
async function callObject(objectType, objectID, method, requestBytes) {
    const url = `${API_BASE}/objects/call/${objectType}/${objectID}/${method}`;
    
    const headers = {
        'Content-Type': 'application/json'
    };
    
    // Include client ID if available (for push message routing)
    if (clientID) {
        headers['X-Client-ID'] = clientID;
    }
    
    const response = await fetch(url, {
        method: 'POST',
        headers: headers,
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

// ==================== Protobuf Encoding ====================

// Create ChatRoom_ListRequest (empty message)
function createListChatRoomsRequest() {
    // Empty message
    const reqBytes = new Uint8Array([]);
    return wrapInAny('chat.ChatRoom_ListRequest', reqBytes);
}

// Create ChatRoom_JoinRequest
function createJoinRequest(userName, clientId) {
    // ChatRoom_JoinRequest: user_name (string, field 1), client_id (string, field 2)
    const userNameBytes = new TextEncoder().encode(userName);
    const field1 = encodeField(1, 2, userNameBytes);
    
    // Include client_id for SSE push notifications
    let reqBytes;
    if (clientId) {
        const clientIdBytes = new TextEncoder().encode(clientId);
        const field2 = encodeField(2, 2, clientIdBytes);
        reqBytes = new Uint8Array([...field1, ...field2]);
    } else {
        reqBytes = field1;
    }
    return wrapInAny('chat.ChatRoom_JoinRequest', reqBytes);
}

// Create ChatRoom_SendChatMessageRequest
function createSendMessageRequest(userName, message) {
    // ChatRoom_SendChatMessageRequest: user_name (string, field 1), message (string, field 2)
    const userNameBytes = new TextEncoder().encode(userName);
    const messageBytes = new TextEncoder().encode(message);
    const field1 = encodeField(1, 2, userNameBytes);
    const field2 = encodeField(2, 2, messageBytes);
    const reqBytes = new Uint8Array([...field1, ...field2]);
    return wrapInAny('chat.ChatRoom_SendChatMessageRequest', reqBytes);
}

// Encode a protobuf field with proper varint length encoding
function encodeField(fieldNumber, wireType, data) {
    const tag = (fieldNumber << 3) | wireType;
    if (wireType === 2) {
        // Length-delimited
        const lenBytes = encodeVarint(data.length);
        return new Uint8Array([tag, ...lenBytes, ...data]);
    } else {
        // For varint, data should already be encoded
        return new Uint8Array([tag, ...data]);
    }
}

// Wrap a message in google.protobuf.Any
function wrapInAny(typeName, messageBytes) {
    const typeUrl = 'type.googleapis.com/' + typeName;
    const typeUrlBytes = new TextEncoder().encode(typeUrl);
    
    // Build Any message with proper varint length encoding
    const field1 = encodeField(1, 2, typeUrlBytes);
    const field2 = messageBytes.length > 0 ? encodeField(2, 2, messageBytes) : new Uint8Array([]);
    const anyBytes = new Uint8Array([...field1, ...field2]);
    
    return base64Encode(anyBytes);
}

// ==================== Protobuf Decoding ====================

// Parse ChatRoom_ListResponse
function parseListChatRoomsResponse(responseBase64) {
    const bytes = base64Decode(responseBase64);
    const any = parseAny(bytes);
    
    // Parse ChatRoom_ListResponse: chat_rooms (repeated string, field 1)
    const rooms = [];
    let offset = 0;
    while (offset < any.value.length) {
        const fieldWire = any.value[offset++];
        const fieldNumber = fieldWire >> 3;
        const wireType = fieldWire & 0x07;
        
        if (fieldNumber === 1 && wireType === 2) {
            const lenResult = decodeVarint(any.value, offset);
            const len = lenResult.value;
            offset = lenResult.offset;
            rooms.push(new TextDecoder().decode(any.value.slice(offset, offset + len)));
            offset += len;
        } else {
            offset = skipField(any.value, offset, wireType);
        }
    }
    
    return rooms;
}

// Parse ChatRoom_JoinResponse
function parseJoinResponse(responseBase64) {
    const bytes = base64Decode(responseBase64);
    const any = parseAny(bytes);
    
    // ChatRoom_JoinResponse: room_name (string, field 1), recent_messages (repeated ChatMessage, field 2)
    let roomName = '';
    const recentMessages = [];
    let offset = 0;
    
    while (offset < any.value.length) {
        const fieldWire = any.value[offset++];
        const fieldNumber = fieldWire >> 3;
        const wireType = fieldWire & 0x07;
        
        if (fieldNumber === 1 && wireType === 2) {
            const lenResult = decodeVarint(any.value, offset);
            const len = lenResult.value;
            offset = lenResult.offset;
            roomName = new TextDecoder().decode(any.value.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 2 && wireType === 2) {
            const lenResult = decodeVarint(any.value, offset);
            const len = lenResult.value;
            offset = lenResult.offset;
            const msgBytes = any.value.slice(offset, offset + len);
            recentMessages.push(parseChatMessage(msgBytes));
            offset += len;
        } else {
            offset = skipField(any.value, offset, wireType);
        }
    }
    
    return { roomName, recentMessages };
}

// Parse ChatMessage
function parseChatMessage(bytes) {
    // ChatMessage: user_name (string, field 1), message (string, field 2), timestamp (int64, field 3)
    let userName = '';
    let message = '';
    let timestamp = 0;
    let offset = 0;
    
    while (offset < bytes.length) {
        const fieldWire = bytes[offset++];
        const fieldNumber = fieldWire >> 3;
        const wireType = fieldWire & 0x07;
        
        if (fieldNumber === 1 && wireType === 2) {
            const lenResult = decodeVarint(bytes, offset);
            const len = lenResult.value;
            offset = lenResult.offset;
            userName = new TextDecoder().decode(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 2 && wireType === 2) {
            const lenResult = decodeVarint(bytes, offset);
            const len = lenResult.value;
            offset = lenResult.offset;
            message = new TextDecoder().decode(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 3 && wireType === 0) {
            // Decode varint
            const result = decodeVarint(bytes, offset);
            timestamp = result.value;
            offset = result.offset;
        } else {
            offset = skipField(bytes, offset, wireType);
        }
    }
    
    return { userName, message, timestamp };
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
            const lenResult = decodeVarint(bytes, offset);
            const len = lenResult.value;
            offset = lenResult.offset;
            typeUrl = new TextDecoder().decode(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 2 && wireType === 2) {
            // value (bytes)
            const lenResult = decodeVarint(bytes, offset);
            const len = lenResult.value;
            offset = lenResult.offset;
            value = bytes.slice(offset, offset + len);
            offset += len;
        } else {
            offset = skipField(bytes, offset, wireType);
        }
    }
    
    return { typeUrl, value };
}

// Skip unknown field
function skipField(bytes, offset, wireType) {
    if (wireType === 0) {
        // Varint
        while (offset < bytes.length) {
            const b = bytes[offset++];
            if ((b & 0x80) === 0) break;
        }
    } else if (wireType === 2) {
        // Length-delimited - use varint for length
        const lenResult = decodeVarint(bytes, offset);
        const len = lenResult.value;
        offset = lenResult.offset;
        offset += len;
    } else if (wireType === 5) {
        // Fixed32
        offset += 4;
    } else if (wireType === 1) {
        // Fixed64
        offset += 8;
    }
    return offset;
}

// ==================== Encoding/Decoding Helpers ====================

// Encode varint (for int64)
function encodeVarint(value) {
    // Handle BigInt for large numbers
    let n = BigInt(value);
    const bytes = [];
    
    do {
        let byte = Number(n & 0x7Fn);
        n = n >> 7n;
        if (n > 0n) {
            byte |= 0x80;
        }
        bytes.push(byte);
    } while (n > 0n);
    
    return bytes;
}

// Decode varint
function decodeVarint(bytes, offset) {
    let value = 0n;
    let shift = 0n;
    
    while (offset < bytes.length) {
        const b = bytes[offset++];
        value |= BigInt(b & 0x7F) << shift;
        if ((b & 0x80) === 0) break;
        shift += 7n;
    }
    
    return { value: Number(value), offset };
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

// ==================== UI Helpers ====================

// Set status message
function setStatus(message, type = '') {
    const statusEl = document.getElementById('status');
    statusEl.textContent = message;
    statusEl.className = 'status';
    if (type) {
        statusEl.classList.add(type);
    }
}

// Set loading state
function setLoading(loading) {
    isLoading = loading;
    document.body.classList.toggle('loading', loading);
}

// Escape HTML to prevent XSS
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}
