// Configuration
const API_BASE = 'http://localhost:8080/api/v1/objects';
const SERVICE_TYPE = 'HandwritingService';
const SERVICE_ID = 'HandwritingService-1'; // Use first service

// State
let currentSheetId = null;
let currentSvgContent = null;

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    setupEventListeners();
    loadMySheets();
});

function setupEventListeners() {
    document.getElementById('generate-btn').addEventListener('click', generateSheet);
    document.getElementById('download-btn').addEventListener('click', downloadSheet);
    document.getElementById('print-btn').addEventListener('click', printSheet);
    document.getElementById('new-sheet-btn').addEventListener('click', resetForm);
}

async function generateSheet() {
    const text = document.getElementById('text-input').value.trim();
    const style = document.getElementById('style-select').value;
    const repetitions = parseInt(document.getElementById('repetitions-input').value);

    if (!text) {
        showStatus('Please enter text to practice', 'error');
        return;
    }

    showStatus('Generating sheet...', 'info');

    try {
        // Generate unique sheet ID
        const timestamp = Date.now();
        const sheetId = `sheet-${timestamp}`;

        // Create request
        const request = {
            sheet_id: sheetId,
            text: text,
            style: style,
            repetitions: repetitions
        };

        // Call the service
        const response = await callObjectMethod(SERVICE_ID, 'GenerateSheet', request);

        if (response && response.svg_content) {
            currentSheetId = sheetId;
            currentSvgContent = response.svg_content;
            displaySheet(response);
            saveToLocalStorage(sheetId, text, style);
            showStatus('Sheet generated successfully!', 'success');
            setTimeout(() => hideStatus(), 3000);
        } else {
            showStatus('Failed to generate sheet', 'error');
        }
    } catch (error) {
        console.error('Error generating sheet:', error);
        showStatus('Error: ' + error.message, 'error');
    }
}

function displaySheet(sheetData) {
    const previewSection = document.getElementById('preview-section');
    const sheetPreview = document.getElementById('sheet-preview');
    
    // Display SVG
    sheetPreview.innerHTML = sheetData.svg_content;
    
    // Show preview section
    previewSection.classList.remove('hidden');
    
    // Scroll to preview
    previewSection.scrollIntoView({ behavior: 'smooth' });
}

function downloadSheet() {
    if (!currentSvgContent) {
        showStatus('No sheet to download', 'error');
        return;
    }

    const blob = new Blob([currentSvgContent], { type: 'image/svg+xml' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `handwriting-sheet-${currentSheetId}.svg`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    
    showStatus('Sheet downloaded!', 'success');
    setTimeout(() => hideStatus(), 2000);
}

function printSheet() {
    window.print();
}

function resetForm() {
    document.getElementById('preview-section').classList.add('hidden');
    document.getElementById('text-input').focus();
    hideStatus();
}

// API Communication
async function callObjectMethod(objectId, method, request) {
    const url = `${API_BASE}/call/${SERVICE_TYPE}/${objectId}/${method}`;
    
    // Encode request as protobuf Any
    let requestBase64;
    if (method === 'GenerateSheet') {
        requestBase64 = createGenerateSheetRequest(request.sheet_id, request.text, request.style, request.repetitions);
    } else if (method === 'GetSheet') {
        requestBase64 = createGetSheetRequest(request.sheet_id);
    } else if (method === 'ListSheets') {
        requestBase64 = createListSheetsRequest();
    } else {
        throw new Error('Unknown method: ' + method);
    }

    const requestData = {
        request: requestBase64
    };

    const response = await fetch(url, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(requestData)
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`HTTP ${response.status}: ${errorText}`);
    }

    const data = await response.json();
    
    // Decode response
    if (data.response) {
        if (method === 'GenerateSheet' || method === 'GetSheet') {
            return parseSheetResponse(data.response);
        } else if (method === 'ListSheets') {
            return parseListSheetsResponse(data.response);
        }
    }
    
    return data;
}

// Protobuf encoding helpers
function createGenerateSheetRequest(sheetId, text, style, repetitions) {
    const sheetIdBytes = encodeString(sheetId);
    const textBytes = encodeString(text);
    const styleBytes = encodeString(style);
    
    const reqBytes = new Uint8Array([
        0x0A, sheetIdBytes.length, ...sheetIdBytes,  // field 1: sheet_id
        0x12, textBytes.length, ...textBytes,         // field 2: text
        0x1A, styleBytes.length, ...styleBytes,       // field 3: style
        0x20, repetitions & 0x7F                      // field 4: repetitions (varint)
    ]);
    
    return wrapInAny('handwriting.GenerateSheetRequest', reqBytes);
}

function createGetSheetRequest(sheetId) {
    const sheetIdBytes = encodeString(sheetId);
    const reqBytes = new Uint8Array([
        0x0A, sheetIdBytes.length, ...sheetIdBytes   // field 1: sheet_id
    ]);
    
    return wrapInAny('handwriting.GetSheetRequest', reqBytes);
}

function createListSheetsRequest() {
    // Empty message
    const reqBytes = new Uint8Array([]);
    return wrapInAny('handwriting.ListSheetsRequest', reqBytes);
}

function encodeString(str) {
    return new TextEncoder().encode(str);
}

function wrapInAny(typeName, messageBytes) {
    const typeUrl = 'type.googleapis.com/' + typeName;
    const typeUrlBytes = encodeString(typeUrl);
    
    const anyBytes = new Uint8Array([
        0x0A, typeUrlBytes.length, ...typeUrlBytes,      // field 1: type_url
        0x12, messageBytes.length, ...messageBytes       // field 2: value
    ]);
    
    return base64Encode(anyBytes);
}

function base64Encode(bytes) {
    let binary = '';
    for (let i = 0; i < bytes.length; i++) {
        binary += String.fromCharCode(bytes[i]);
    }
    return btoa(binary);
}

function base64Decode(base64) {
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
        bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
}

function parseSheetResponse(responseBase64) {
    const bytes = base64Decode(responseBase64);
    const any = parseAny(bytes);
    return parseSheetFromBytes(any.value);
}

function parseListSheetsResponse(responseBase64) {
    const bytes = base64Decode(responseBase64);
    const any = parseAny(bytes);
    return parseListSheetsFromBytes(any.value);
}

function parseAny(bytes) {
    let offset = 0;
    let typeUrl = '';
    let value = new Uint8Array(0);
    
    while (offset < bytes.length) {
        const fieldWire = bytes[offset++];
        const fieldNumber = fieldWire >> 3;
        const wireType = fieldWire & 0x07;
        
        if (fieldNumber === 1 && wireType === 2) {
            const len = bytes[offset++];
            typeUrl = decodeString(bytes.slice(offset, offset + len));
            offset += len;
        } else if (fieldNumber === 2 && wireType === 2) {
            const len = bytes[offset++];
            value = bytes.slice(offset, offset + len);
            offset += len;
        }
    }
    
    return { typeUrl, value };
}

function parseSheetFromBytes(bytes) {
    let offset = 0;
    const result = {
        sheet_id: '',
        text: '',
        style: '',
        repetitions: 0,
        svg_content: '',
        created_at: 0,
        exists: true
    };
    
    while (offset < bytes.length) {
        const fieldWire = bytes[offset++];
        const fieldNumber = fieldWire >> 3;
        const wireType = fieldWire & 0x07;
        
        if (wireType === 2) {
            const len = bytes[offset++];
            const value = decodeString(bytes.slice(offset, offset + len));
            offset += len;
            
            if (fieldNumber === 1) result.sheet_id = value;
            else if (fieldNumber === 2) result.text = value;
            else if (fieldNumber === 3) result.style = value;
            else if (fieldNumber === 5) result.svg_content = value;
        } else if (wireType === 0) {
            const value = bytes[offset++];
            if (fieldNumber === 4) result.repetitions = value;
            else if (fieldNumber === 7) result.exists = value !== 0;
        }
    }
    
    return result;
}

function parseListSheetsFromBytes(bytes) {
    let offset = 0;
    const sheetIds = [];
    
    while (offset < bytes.length) {
        const fieldWire = bytes[offset++];
        const fieldNumber = fieldWire >> 3;
        const wireType = fieldWire & 0x07;
        
        if (fieldNumber === 1 && wireType === 2) {
            const len = bytes[offset++];
            const sheetId = decodeString(bytes.slice(offset, offset + len));
            offset += len;
            sheetIds.push(sheetId);
        }
    }
    
    return { sheet_ids: sheetIds };
}

function decodeString(bytes) {
    return new TextDecoder().decode(bytes);
}

// LocalStorage for tracking sheets
function saveToLocalStorage(sheetId, text, style) {
    let sheets = JSON.parse(localStorage.getItem('handwritingSheets') || '[]');
    sheets.unshift({ id: sheetId, text: text, style: style, timestamp: Date.now() });
    // Keep only last 10
    sheets = sheets.slice(0, 10);
    localStorage.setItem('handwritingSheets', JSON.stringify(sheets));
    loadMySheets();
}

function loadMySheets() {
    const sheets = JSON.parse(localStorage.getItem('handwritingSheets') || '[]');
    const sheetsList = document.getElementById('sheets-list');
    const mySheetsSection = document.getElementById('my-sheets');

    if (sheets.length === 0) {
        mySheetsSection.classList.add('hidden');
        return;
    }

    mySheetsSection.classList.remove('hidden');
    sheetsList.innerHTML = '';

    sheets.forEach(sheet => {
        const card = document.createElement('div');
        card.className = 'sheet-card';
        card.innerHTML = `
            <h3>${sheet.text}</h3>
            <p>Style: ${sheet.style}</p>
            <p><small>${new Date(sheet.timestamp).toLocaleDateString()}</small></p>
        `;
        card.addEventListener('click', () => loadSheet(sheet.id));
        sheetsList.appendChild(card);
    });
}

async function loadSheet(sheetId) {
    showStatus('Loading sheet...', 'info');
    
    try {
        const request = { sheet_id: sheetId };
        const response = await callObjectMethod(SERVICE_ID, 'GetSheet', request);
        
        if (response && response.exists && response.svg_content) {
            currentSheetId = sheetId;
            currentSvgContent = response.svg_content;
            
            // Update form
            document.getElementById('text-input').value = response.text;
            document.getElementById('style-select').value = response.style;
            document.getElementById('repetitions-input').value = response.repetitions;
            
            displaySheet(response);
            showStatus('Sheet loaded!', 'success');
            setTimeout(() => hideStatus(), 2000);
        } else {
            showStatus('Sheet not found on server', 'error');
        }
    } catch (error) {
        console.error('Error loading sheet:', error);
        showStatus('Error loading sheet: ' + error.message, 'error');
    }
}

// Status messages
function showStatus(message, type) {
    const status = document.getElementById('status');
    status.textContent = message;
    status.className = `status ${type}`;
    status.classList.remove('hidden');
}

function hideStatus() {
    const status = document.getElementById('status');
    status.classList.add('hidden');
}
