// Bomberman web client. Talks to the goverse HTTP gate:
//   GET  /api/v1/events/stream                                   (SSE)
//   POST /api/v1/objects/call/{type}/{id}/{method}                (RPC)
//
// Wire format: every RPC body is base64-encoded protobuf wrapped in
// google.protobuf.Any. SSE message events arrive in the same shape,
// minus the JSON wrapper. We hand-roll a tiny varint encoder/decoder
// here rather than pulling in protobuf.js — only MatchSnapshot and a
// handful of request messages need to cross the wire.

const API_BASE = inferApiBase();
const TICK_HZ = 10;
const BOARD_WIDTH = 13;
const BOARD_HEIGHT = 11;

// ---------- DOM refs ----------

const elNickname = document.getElementById('nickname');
const elJoinBtn  = document.getElementById('join-button');
const elLobby    = document.getElementById('lobby');
const elGame     = document.getElementById('game');
const elStatus   = document.getElementById('lobby-status');
const elMatchID  = document.getElementById('match-id');
const elMatchSt  = document.getElementById('match-status');
const elMyStats  = document.getElementById('my-stats');
const elPlayers  = document.getElementById('players-list');
const elBoard    = document.getElementById('board');
const ctx2d      = elBoard.getContext('2d');

// ---------- session state ----------

let clientID = null;            // assigned by SSE register event
let playerID = null;            // entered by user
let matchID  = null;            // first MatchSnapshot fills this in
let lastSnapshot = null;
let inputState = { move: 0, placeBomb: false };
let inputTimer = null;
let eventSource = null;
// queueState tracks whether the user is sitting in the matchmaking
// queue waiting to be slotted into a Match. The pagehide handler
// uses it to decide whether to fire a best-effort LeaveQueue beacon
// — if the user has already been matched (state === 'in-match') we
// leave them alone; closing the tab mid-game shouldn't yank them
// out of the queue (they're not in it any more) or out of the match
// (the right thing for that is server-side staleness, future PR).
let queueState = 'idle';        // 'idle' | 'queued' | 'in-match'

// ---------- entry ----------

document.addEventListener('DOMContentLoaded', () => {
  elBoard.tabIndex = 0; // focusable for keyboard
  setStatus('Connecting…');
  connectSSE();
  elJoinBtn.addEventListener('click', onFindMatch);
  elNickname.addEventListener('keydown', e => {
    if (e.key === 'Enter' && !elJoinBtn.disabled) onFindMatch();
  });
  // If the tab is closed while the user is still queued, fire a
  // best-effort LeaveQueue so they don't sit in the FIFO as a ghost
  // (the next spawn batch would otherwise pick them up and the match
  // would start with phantom players). pagehide is the modern signal
  // for "the page is going away" — fires for tab close, navigation,
  // and on iOS where beforeunload is unreliable. sendBeacon is the
  // only API the browser will actually deliver during teardown
  // (fetch/XHR are cancelled). Crashes / hard kills aren't covered;
  // server-side staleness eviction is the proper backstop and is
  // out of scope for this fix.
  window.addEventListener('pagehide', flushLeaveQueueOnClose);
});

function inferApiBase() {
  const params = new URLSearchParams(window.location.search);
  const o = params.get('api');
  if (o) return o;
  const proto = window.location.protocol;
  const host  = window.location.hostname;
  return `${proto}//${host}:8080/api/v1`;
}

function setStatus(msg, kind) {
  elStatus.textContent = msg;
  elStatus.className = 'status' + (kind ? ' ' + kind : '');
}

// ---------- SSE ----------

function connectSSE() {
  eventSource = new EventSource(`${API_BASE}/events/stream`);
  eventSource.addEventListener('register', e => {
    const data = JSON.parse(e.data);
    clientID = data.clientId;
    elJoinBtn.disabled = false;
    elJoinBtn.textContent = 'Find Match';
    setStatus(`Connected as ${clientID}. Pick a nickname and queue up.`, 'success');
  });
  eventSource.addEventListener('message', e => {
    const data = JSON.parse(e.data);
    if (!data.type || !data.type.endsWith('bomberman.MatchSnapshot')) return;
    const bytes = base64Decode(data.payload);
    // Payload is a marshalled Any whose .value is the MatchSnapshot.
    const inner = decodeAnyValue(bytes);
    const snap = decodeMatchSnapshot(inner);
    onSnapshot(snap);
  });
  eventSource.addEventListener('heartbeat', () => {});
  eventSource.onerror = () => {
    if (eventSource.readyState === EventSource.CLOSED) {
      setStatus('Disconnected — reconnecting…', 'error');
      setTimeout(connectSSE, 2000);
    }
  };
}

// ---------- HTTP RPC ----------

async function callObject(objectType, objectID, method, requestBytes) {
  // requestBytes is the marshalled Any (already base64 elsewhere).
  const body = JSON.stringify({ request: base64Encode(requestBytes) });
  const url  = `${API_BASE}/objects/call/${objectType}/${objectID}/${method}`;
  const r    = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body,
  });
  if (!r.ok) throw new Error(`${url}: HTTP ${r.status}`);
  const j = await r.json();
  if (!j.response) return null;
  const respBytes = base64Decode(j.response);
  return decodeAnyValue(respBytes); // returns the inner message bytes
}

// ---------- Lobby flow ----------

async function onFindMatch() {
  const nick = elNickname.value.trim();
  if (!nick) { setStatus('Enter a nickname first.', 'error'); return; }
  if (!clientID) { setStatus('Still connecting; try again in a second.', 'error'); return; }
  playerID = nick;
  elJoinBtn.disabled = true;
  elJoinBtn.textContent = 'Queued…';
  setStatus(`Queued as ${playerID}. Waiting for opponents…`);

  const req = encodeJoinQueueRequest({ playerId: playerID, clientId: clientID });
  const reqAny = wrapAny('type.googleapis.com/bomberman.JoinQueueRequest', req);
  try {
    const respBytes = await callObject('MatchmakingQueue', 'MatchmakingQueue', 'JoinQueue', reqAny);
    const resp = decodeJoinQueueResponse(respBytes);
    if (!resp.ok) {
      setStatus(`Queue rejected: ${resp.reason || 'unknown'}`, 'error');
      elJoinBtn.disabled = false;
      elJoinBtn.textContent = 'Find Match';
      return;
    }
    queueState = 'queued';
    setStatus(`In queue at position ${resp.queuePosition}. The match will start once enough players are queued.`);
  } catch (err) {
    setStatus(`Queue error: ${err.message}`, 'error');
    elJoinBtn.disabled = false;
    elJoinBtn.textContent = 'Find Match';
  }
}

// flushLeaveQueueOnClose runs from the pagehide handler. Uses
// navigator.sendBeacon — fetch() / XHR are aborted when the page
// goes away, but sendBeacon is delivered by the browser
// asynchronously after the tab is gone, which is exactly what we
// need.
function flushLeaveQueueOnClose() {
  if (queueState !== 'queued') return;
  if (!playerID) return;
  if (typeof navigator.sendBeacon !== 'function') return;
  const inner = encodeLeaveQueueRequest({ playerId: playerID });
  const reqAny = wrapAny('type.googleapis.com/bomberman.LeaveQueueRequest', inner);
  const body = JSON.stringify({ request: base64Encode(reqAny) });
  const url = `${API_BASE}/objects/call/MatchmakingQueue/MatchmakingQueue/LeaveQueue`;
  try {
    navigator.sendBeacon(url, new Blob([body], { type: 'application/json' }));
  } catch (_) { /* best effort */ }
}

// ---------- Snapshot handler ----------

function onSnapshot(snap) {
  // First snapshot identifies our match. After that, ignore snapshots
  // belonging to other matches (e.g. spectator tabs sharing a node).
  if (!matchID) {
    matchID = snap.matchId;
    queueState = 'in-match';
    elLobby.classList.add('hidden');
    elGame.classList.remove('hidden');
    elBoard.focus();
    bindKeyboard();
    startInputTimer();
  } else if (snap.matchId !== matchID) {
    return;
  }
  lastSnapshot = snap;
  render(snap);
  if (snap.status === 3 /* ENDED */) onMatchEnded(snap);
}

function onMatchEnded(snap) {
  stopInputTimer();
  const winner = snap.winnerId || 'no one (draw)';
  elMatchSt.textContent = `Match over — winner: ${winner}`;
  elJoinBtn.disabled = false;
  elJoinBtn.textContent = 'Find another match';
  // Reset matchID + queueState so the next match can take over the
  // canvas and pagehide once again becomes a leave-queue trigger.
  matchID = null;
  queueState = 'idle';
}

// ---------- Rendering ----------

const TILE_SIZE = 40;
const COLORS = {
  empty:        '#1a1a22',
  wall:         '#444455',
  block:        '#7a5b35',
  bomb:         '#222',
  bombFlash:    '#cc4422',
  explosion:    '#ff8833',
  powerupBomb:  '#ff66aa',
  powerupPower: '#66aaff',
  powerupSpeed: '#aaff66',
  playerColors: ['#5a7aff', '#ff5a7a', '#5aff7a', '#ffaa5a', '#aa5aff', '#5affff', '#ffff5a', '#ff5aff'],
};

function render(snap) {
  // Static layout
  ctx2d.fillStyle = COLORS.empty;
  ctx2d.fillRect(0, 0, elBoard.width, elBoard.height);
  for (let y = 0; y < snap.height; y++) {
    for (let x = 0; x < snap.width; x++) {
      const t = snap.tiles[y * snap.width + x];
      if (t === 1)      drawTile(x, y, COLORS.wall);
      else if (t === 2) drawTile(x, y, COLORS.block);
    }
  }
  // Powerups under everything else
  for (const p of snap.powerups) {
    let c = COLORS.powerupBomb;
    if (p.kind === 2) c = COLORS.powerupPower;
    if (p.kind === 3) c = COLORS.powerupSpeed;
    drawCircle(p.x, p.y, c, 0.30);
  }
  // Bombs
  for (const b of snap.bombs) {
    const flashing = b.ticksRemaining <= 5 && (b.ticksRemaining % 2 === 0);
    drawCircle(b.x, b.y, flashing ? COLORS.bombFlash : COLORS.bomb, 0.36);
  }
  // Explosions on top of everything except live players
  for (const e of snap.explosions) {
    drawTile(e.x, e.y, COLORS.explosion);
  }
  // Players
  const playersByID = {};
  snap.players.forEach((p, i) => {
    playersByID[p.playerId] = p;
    if (!p.alive) return;
    const color = COLORS.playerColors[i % COLORS.playerColors.length];
    drawCircle(p.x, p.y, color, 0.42);
  });

  updateHUD(snap, playersByID);
}

function drawTile(x, y, color) {
  ctx2d.fillStyle = color;
  ctx2d.fillRect(x * TILE_SIZE, y * TILE_SIZE, TILE_SIZE, TILE_SIZE);
}

function drawCircle(x, y, color, radiusFrac) {
  const cx = x * TILE_SIZE + TILE_SIZE / 2;
  const cy = y * TILE_SIZE + TILE_SIZE / 2;
  const r  = TILE_SIZE * radiusFrac;
  ctx2d.fillStyle = color;
  ctx2d.beginPath();
  ctx2d.arc(cx, cy, r, 0, Math.PI * 2);
  ctx2d.fill();
}

function updateHUD(snap, playersByID) {
  elMatchID.textContent = snap.matchId;
  const statusName = ['?', 'Lobby', 'Running', 'Ended'][snap.status] || '?';
  elMatchSt.textContent = `Tick ${snap.tick} · ${statusName}`;
  const me = playersByID[playerID];
  if (me) {
    elMyStats.textContent = `you  bombs:${me.bombCapacity}  power:${me.bombPower}  speed:${me.speed}`;
  }
  elPlayers.innerHTML = '';
  snap.players.forEach((p, i) => {
    const chip = document.createElement('span');
    chip.className = 'player-chip';
    if (!p.alive) chip.classList.add('dead');
    if (p.playerId === playerID) chip.classList.add('you');
    chip.style.borderColor = COLORS.playerColors[i % COLORS.playerColors.length];
    chip.textContent = p.playerId;
    elPlayers.appendChild(chip);
  });
}

// ---------- Keyboard input ----------

const KEY_MOVE = {
  'ArrowUp': 1, 'w': 1, 'W': 1,
  'ArrowDown': 2, 's': 2, 'S': 2,
  'ArrowLeft': 3, 'a': 3, 'A': 3,
  'ArrowRight': 4, 'd': 4, 'D': 4,
};

function bindKeyboard() {
  // Track held direction; release reverts to NONE so the player stops.
  let heldKey = null;
  const onDown = e => {
    if (e.key === ' ') { inputState.placeBomb = true; e.preventDefault(); return; }
    const dir = KEY_MOVE[e.key];
    if (dir === undefined) return;
    heldKey = e.key;
    inputState.move = dir;
    e.preventDefault();
  };
  const onUp = e => {
    if (e.key === heldKey) { heldKey = null; inputState.move = 0; }
  };
  document.addEventListener('keydown', onDown);
  document.addEventListener('keyup', onUp);
}

function startInputTimer() {
  if (inputTimer) return;
  inputTimer = setInterval(sendInput, 1000 / TICK_HZ);
}

function stopInputTimer() {
  if (inputTimer) { clearInterval(inputTimer); inputTimer = null; }
}

async function sendInput() {
  if (!matchID) return;
  // Snapshot and reset placeBomb so a single space-press fires once.
  const placeBomb = inputState.placeBomb;
  inputState.placeBomb = false;
  if (inputState.move === 0 && !placeBomb) return; // no-op tick
  const req = encodePlayerInputRequest({
    playerId: playerID,
    move: inputState.move,
    placeBomb,
  });
  const reqAny = wrapAny('type.googleapis.com/bomberman.PlayerInputRequest', req);
  try {
    await callObject('Match', matchID, 'HandleInput', reqAny);
  } catch (err) {
    console.warn('HandleInput failed:', err);
  }
}

// ==================================================================
// Tiny protobuf helpers (encoders/decoders for the messages we use)
// ==================================================================

function base64Encode(bytes) {
  let s = '';
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
  return btoa(s);
}
function base64Decode(b64) {
  const s = atob(b64);
  const out = new Uint8Array(s.length);
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i);
  return out;
}

// --- low-level wire format ---

function encodeVarint(v) {
  const out = [];
  while (v >= 0x80) { out.push((v & 0x7f) | 0x80); v >>>= 7; }
  out.push(v & 0x7f);
  return out;
}

function encodeTag(field, wire) { return encodeVarint((field << 3) | wire); }

function encodeLenDelim(field, bytes) {
  const out = [];
  out.push(...encodeTag(field, 2));
  out.push(...encodeVarint(bytes.length));
  for (let i = 0; i < bytes.length; i++) out.push(bytes[i]);
  return out;
}

function encodeStringField(field, str) {
  const u = new TextEncoder().encode(str);
  return encodeLenDelim(field, u);
}

function encodeVarintField(field, v) {
  return [...encodeTag(field, 0), ...encodeVarint(v)];
}

function encodeBoolField(field, v) { return encodeVarintField(field, v ? 1 : 0); }

function decodeVarint(buf, off) {
  let v = 0, shift = 0, b;
  do { b = buf[off++]; v |= (b & 0x7f) << shift; shift += 7; } while (b & 0x80);
  return [v >>> 0, off];
}

// wrapAny constructs the Any envelope used by every gate request /
// response: { type_url, value }.
function wrapAny(typeURL, valueBytes) {
  const out = [];
  out.push(...encodeStringField(1, typeURL));
  out.push(...encodeLenDelim(2, valueBytes));
  return new Uint8Array(out);
}

// decodeAnyValue parses the Any wrapper and returns the inner value.
function decodeAnyValue(bytes) {
  let off = 0;
  let valueBytes = null;
  while (off < bytes.length) {
    const [tag, next] = decodeVarint(bytes, off); off = next;
    const field = tag >>> 3, wire = tag & 7;
    if (wire === 2) {
      const [len, n2] = decodeVarint(bytes, off); off = n2;
      if (field === 2) valueBytes = bytes.slice(off, off + len);
      off += len;
    } else if (wire === 0) { const [, n2] = decodeVarint(bytes, off); off = n2; }
    else throw new Error(`unsupported wire type ${wire} in Any`);
  }
  return valueBytes || new Uint8Array();
}

// --- request encoders ---

function encodeJoinQueueRequest({ playerId, clientId }) {
  const out = [];
  if (playerId) out.push(...encodeStringField(1, playerId));
  if (clientId) out.push(...encodeStringField(2, clientId));
  return new Uint8Array(out);
}

function encodeLeaveQueueRequest({ playerId }) {
  const out = [];
  if (playerId) out.push(...encodeStringField(1, playerId));
  return new Uint8Array(out);
}

function encodePlayerInputRequest({ playerId, move, placeBomb }) {
  const out = [];
  if (playerId) out.push(...encodeStringField(1, playerId));
  if (move)     out.push(...encodeVarintField(2, move));
  if (placeBomb) out.push(...encodeBoolField(3, true));
  return new Uint8Array(out);
}

// --- response decoders ---

function decodeJoinQueueResponse(bytes) {
  const out = { ok: false, reason: '', queuePosition: 0 };
  let off = 0;
  while (off < bytes.length) {
    const [tag, n] = decodeVarint(bytes, off); off = n;
    const field = tag >>> 3, wire = tag & 7;
    if (wire === 0) {
      const [v, n2] = decodeVarint(bytes, off); off = n2;
      if (field === 1) out.ok = !!v;
      else if (field === 3) out.queuePosition = v;
    } else if (wire === 2) {
      const [len, n2] = decodeVarint(bytes, off); off = n2;
      const slice = bytes.slice(off, off + len); off += len;
      if (field === 2) out.reason = new TextDecoder().decode(slice);
    }
  }
  return out;
}

function decodeMatchSnapshot(bytes) {
  const out = {
    matchId: '', status: 0, tick: 0, width: 0, height: 0,
    tiles: [], players: [], bombs: [], explosions: [], powerups: [],
    winnerId: '',
  };
  let off = 0;
  while (off < bytes.length) {
    const [tag, n] = decodeVarint(bytes, off); off = n;
    const field = tag >>> 3, wire = tag & 7;
    if (wire === 0) {
      const [v, n2] = decodeVarint(bytes, off); off = n2;
      if (field === 2) out.status = v;
      else if (field === 3) out.tick = v;
      else if (field === 4) out.width = v;
      else if (field === 5) out.height = v;
      else if (field === 6) out.tiles.push(v); // packed=false fallback
    } else if (wire === 2) {
      const [len, n2] = decodeVarint(bytes, off); off = n2;
      const slice = bytes.slice(off, off + len); off += len;
      switch (field) {
        case 1: out.matchId = new TextDecoder().decode(slice); break;
        case 6: { // tiles repeated enum, packed
          let p = 0;
          while (p < slice.length) {
            const [v, np] = decodeVarint(slice, p); p = np;
            out.tiles.push(v);
          }
          break;
        }
        case 7:  out.players.push(decodePlayerState(slice)); break;
        case 8:  out.bombs.push(decodeBombState(slice)); break;
        case 9:  out.explosions.push(decodeExplosionState(slice)); break;
        case 10: out.powerups.push(decodePowerupState(slice)); break;
        case 11: out.winnerId = new TextDecoder().decode(slice); break;
      }
    }
  }
  return out;
}

function decodePlayerState(bytes) {
  const out = { playerId: '', x: 0, y: 0, alive: false, bombCapacity: 0, bombPower: 0, speed: 0, activeBombs: 0 };
  let off = 0;
  while (off < bytes.length) {
    const [tag, n] = decodeVarint(bytes, off); off = n;
    const field = tag >>> 3, wire = tag & 7;
    if (wire === 0) {
      const [v, n2] = decodeVarint(bytes, off); off = n2;
      if (field === 2) out.x = v;
      else if (field === 3) out.y = v;
      else if (field === 4) out.alive = !!v;
      else if (field === 5) out.bombCapacity = v;
      else if (field === 6) out.bombPower = v;
      else if (field === 7) out.speed = v;
      else if (field === 8) out.activeBombs = v;
    } else if (wire === 2) {
      const [len, n2] = decodeVarint(bytes, off); off = n2;
      const slice = bytes.slice(off, off + len); off += len;
      if (field === 1) out.playerId = new TextDecoder().decode(slice);
    }
  }
  return out;
}

function decodeBombState(bytes) {
  const out = { x: 0, y: 0, ownerId: '', power: 0, ticksRemaining: 0 };
  let off = 0;
  while (off < bytes.length) {
    const [tag, n] = decodeVarint(bytes, off); off = n;
    const field = tag >>> 3, wire = tag & 7;
    if (wire === 0) {
      const [v, n2] = decodeVarint(bytes, off); off = n2;
      if (field === 1) out.x = v;
      else if (field === 2) out.y = v;
      else if (field === 4) out.power = v;
      else if (field === 5) out.ticksRemaining = v;
    } else if (wire === 2) {
      const [len, n2] = decodeVarint(bytes, off); off = n2;
      const slice = bytes.slice(off, off + len); off += len;
      if (field === 3) out.ownerId = new TextDecoder().decode(slice);
    }
  }
  return out;
}

function decodeExplosionState(bytes) {
  const out = { x: 0, y: 0, ticksRemaining: 0 };
  let off = 0;
  while (off < bytes.length) {
    const [tag, n] = decodeVarint(bytes, off); off = n;
    const field = tag >>> 3, wire = tag & 7;
    if (wire === 0) {
      const [v, n2] = decodeVarint(bytes, off); off = n2;
      if (field === 1) out.x = v;
      else if (field === 2) out.y = v;
      else if (field === 3) out.ticksRemaining = v;
    }
  }
  return out;
}

function decodePowerupState(bytes) {
  const out = { x: 0, y: 0, kind: 0 };
  let off = 0;
  while (off < bytes.length) {
    const [tag, n] = decodeVarint(bytes, off); off = n;
    const field = tag >>> 3, wire = tag & 7;
    if (wire === 0) {
      const [v, n2] = decodeVarint(bytes, off); off = n2;
      if (field === 1) out.x = v;
      else if (field === 2) out.y = v;
      else if (field === 3) out.kind = v;
    }
  }
  return out;
}
