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
let pollIntervalId = null;
let isLoading = false;

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
    
    // Load chat rooms
    loadChatRooms();
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
    
    setLoading(true);
    setStatus('Joining ' + roomName + '...');
    
    try {
        const request = createJoinRequest(userName);
        const response = await callObject('ChatRoom', 'ChatRoom-' + roomName, 'Join', request);
        const joinData = parseJoinResponse(response);
        
        currentRoom = roomName;
        lastMsgTimestamp = 0;
        
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
        
        // Start polling for new messages
        startPolling();
        
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
    stopPolling();
    currentRoom = null;
    lastMsgTimestamp = 0;
    
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
        
        // Display our own message immediately (optimistic update)
        const now = Date.now() * 1000; // Convert to microseconds
        displayMessage({
            userName: userName,
            message: message,
            timestamp: now
        });
        lastMsgTimestamp = now;
        
    } catch (error) {
        console.error('Failed to send message:', error);
        setStatus('Failed to send message: ' + error.message, 'error');
        // Restore the message in input
        messageInput.value = message;
    }
}

// Display a message in the chat
function displayMessage(msg) {
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
}

// Poll for new messages
async function pollMessages() {
    if (!currentRoom || isLoading) return;
    
    try {
        const request = createGetRecentMessagesRequest(lastMsgTimestamp);
        const response = await callObject('ChatRoom', 'ChatRoom-' + currentRoom, 'GetRecentMessages', request);
        const messages = parseGetRecentMessagesResponse(response);
        
        messages.forEach(msg => {
            // Don't show our own messages again (we already displayed them optimistically)
            if (msg.userName !== userName) {
                displayMessage(msg);
            }
            if (msg.timestamp > lastMsgTimestamp) {
                lastMsgTimestamp = msg.timestamp;
            }
        });
    } catch (error) {
        console.log('Poll error:', error);
    }
}

// Start polling for messages
function startPolling() {
    stopPolling();
    pollIntervalId = setInterval(pollMessages, 1000); // Poll every second
}

// Stop polling
function stopPolling() {
    if (pollIntervalId) {
        clearInterval(pollIntervalId);
        pollIntervalId = null;
    }
}

// ==================== API Communication ====================

// Call an object method via HTTP Gate
async function callObject(objectType, objectID, method, requestBytes) {
    const url = `${API_BASE}/objects/call/${objectType}/${objectID}/${method}`;
    
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

// ==================== Protobuf Encoding ====================

// Create ChatRoom_ListRequest (empty message)
function createListChatRoomsRequest() {
    // Empty message
    const reqBytes = new Uint8Array([]);
    return wrapInAny('chat.ChatRoom_ListRequest', reqBytes);
}

// Create ChatRoom_JoinRequest
function createJoinRequest(userName) {
    // ChatRoom_JoinRequest: user_name (string, field 1), client_id (string, field 2)
    const userNameBytes = new TextEncoder().encode(userName);
    // We don't need client_id for HTTP polling
    const reqBytes = new Uint8Array([
        0x0A, // field 1, wire type 2 (length-delimited)
        userNameBytes.length,
        ...userNameBytes
    ]);
    return wrapInAny('chat.ChatRoom_JoinRequest', reqBytes);
}

// Create ChatRoom_SendChatMessageRequest
function createSendMessageRequest(userName, message) {
    // ChatRoom_SendChatMessageRequest: user_name (string, field 1), message (string, field 2)
    const userNameBytes = new TextEncoder().encode(userName);
    const messageBytes = new TextEncoder().encode(message);
    const reqBytes = new Uint8Array([
        0x0A, // field 1, wire type 2
        userNameBytes.length,
        ...userNameBytes,
        0x12, // field 2, wire type 2
        messageBytes.length,
        ...messageBytes
    ]);
    return wrapInAny('chat.ChatRoom_SendChatMessageRequest', reqBytes);
}

// Create ChatRoom_GetRecentMessagesRequest
function createGetRecentMessagesRequest(afterTimestamp) {
    // ChatRoom_GetRecentMessagesRequest: after_timestamp (int64, field 1)
    // Encode int64 as varint
    const varintBytes = encodeVarint(afterTimestamp);
    const reqBytes = new Uint8Array([
        0x08, // field 1, wire type 0 (varint)
        ...varintBytes
    ]);
    return wrapInAny('chat.ChatRoom_GetRecentMessagesRequest', reqBytes);
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
            const len = any.value[offset++];
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
            const len = any.value[offset++];
            roomName = new TextDecoder().decode(any.value.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 2 && wireType === 2) {
            const len = any.value[offset++];
            const msgBytes = any.value.slice(offset, offset + len);
            recentMessages.push(parseChatMessage(msgBytes));
            offset += len;
        } else {
            offset = skipField(any.value, offset, wireType);
        }
    }
    
    return { roomName, recentMessages };
}

// Parse ChatRoom_GetRecentMessagesResponse
function parseGetRecentMessagesResponse(responseBase64) {
    const bytes = base64Decode(responseBase64);
    const any = parseAny(bytes);
    
    // ChatRoom_GetRecentMessagesResponse: messages (repeated ChatMessage, field 1)
    const messages = [];
    let offset = 0;
    
    while (offset < any.value.length) {
        const fieldWire = any.value[offset++];
        const fieldNumber = fieldWire >> 3;
        const wireType = fieldWire & 0x07;
        
        if (fieldNumber === 1 && wireType === 2) {
            const len = any.value[offset++];
            const msgBytes = any.value.slice(offset, offset + len);
            messages.push(parseChatMessage(msgBytes));
            offset += len;
        } else {
            offset = skipField(any.value, offset, wireType);
        }
    }
    
    return messages;
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
            const len = bytes[offset++];
            userName = new TextDecoder().decode(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 2 && wireType === 2) {
            const len = bytes[offset++];
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
            const len = bytes[offset++];
            typeUrl = new TextDecoder().decode(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 2 && wireType === 2) {
            // value (bytes)
            const len = bytes[offset++];
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
        // Length-delimited
        const len = bytes[offset++];
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
