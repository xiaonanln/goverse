# Trading System Sample - Design Document

## Overview

A simple orderbook-based trading system demonstrating the Goverse virtual actor model with real-time updates via Server-Sent Events (SSE). This sample shows how to build a single-symbol orderbook where users can place bids and asks at specific prices, and receive real-time orderbook updates.

## Goals

1. **Orderbook Management**: Maintain a price-time priority orderbook for a single trading symbol
2. **Order Operations**: Support placing bid (buy) and ask (sell) orders at specified prices
3. **Order Matching**: Automatically match crossing orders using price-time priority
4. **Real-time Updates**: Push orderbook changes to connected clients via SSE
5. **Distributed Architecture**: Demonstrate Goverse's ability to host stateful orderbook objects

## Non-Goals

- Multiple trading symbols (single symbol only for simplicity)
- Order cancellation (place only)
- Partial fills or advanced order types (market orders, stop-loss, etc.)
- User authentication and account management
- Trade history persistence
- High-frequency trading optimizations

## Architecture

```
┌─────────────────┐      SSE Stream        ┌─────────────────┐      gRPC        ┌─────────────────┐
│   Web Browser   │ ◄──────────────────────│   HTTP Gate     │ ◄────────────────│  Goverse Node   │
│   (HTML/JS)     │ ───────────────────────►   (:48000)      │ ────────────────►│  (Orderbook)    │
└─────────────────┘     HTTP REST          └─────────────────┘                  └─────────────────┘
                                                   ▲
                                                   │ Push notifications
                                                   │ (Orderbook updates)
                                                   ▼
                                          ┌─────────────────┐
                                          │      etcd       │  (Coordination)
                                          │     :2379       │
                                          └─────────────────┘
```

### Components

1. **Orderbook Object**: Virtual actor representing a single symbol's orderbook
   - Object ID format: `Orderbook-{symbol}` (e.g., `Orderbook-BTC`)
   - Maintains sorted bid and ask price levels
   - Matches crossing orders automatically
   - Pushes orderbook updates to subscribed clients

2. **Gate Server**: Handles HTTP requests and SSE connections
   - Standard Goverse Gate with HTTP API
   - SSE endpoint for real-time orderbook updates
   - Routes requests to the Orderbook object

3. **Web Client**: Browser-based trading interface
   - Displays real-time orderbook depth (bid/ask levels)
   - Allows placing bid and ask orders
   - Subscribes to orderbook updates via SSE

### Data Flow

1. **Order Placement**:
   ```
   Client → HTTP POST → Gate → gRPC → Orderbook.PlaceBid/PlaceAsk
                                              │
                                              ▼
                                      Order matching
                                              │
                                              ▼
                                      Push update to subscribers
   ```

2. **Real-time Updates**:
   ```
   Client ← SSE ← Gate ← gRPC Push ← Orderbook (on order change)
   ```

## Orderbook Object Design

### State Structure

```go
type Orderbook struct {
    goverseapi.BaseObject
    mu sync.Mutex
    
    symbol       string
    bids         []*PriceLevel  // Sorted descending by price (best bid first)
    asks         []*PriceLevel  // Sorted ascending by price (best ask first)
    lastTradeID  int64
    trades       []*Trade       // Recent trades (limited buffer)
    subscribers  []string       // Client IDs subscribed to updates
}

type PriceLevel struct {
    Price    int64    // Price in smallest unit (e.g., cents for USD, satoshis for BTC)
                      // Example: 5000000 = $50,000.00 or 50000 BTC satoshis
    Quantity int64    // Total quantity at this price level
    Orders   []*Order // Orders at this level, FIFO for time priority
}

type Order struct {
    ID        string
    Side      string // "bid" or "ask"
    Price     int64
    Quantity  int64
    Remaining int64
    ClientID  string
    Timestamp int64
}

type Trade struct {
    ID         int64
    Price      int64
    Quantity   int64
    BuyerID    string
    SellerID   string
    Timestamp  int64
}
```

### Order Matching Logic

Orders are matched using **price-time priority**:

1. When a **bid** (buy order) is placed:
   - Compare with asks starting from lowest price
   - If bid.Price >= ask.Price, match occurs
   - Fill orders at ask's price (taker gets maker's price)
   - Continue matching until bid is fully filled or no more matching asks

2. When an **ask** (sell order) is placed:
   - Compare with bids starting from highest price  
   - If ask.Price <= bid.Price, match occurs
   - Fill orders at bid's price
   - Continue matching until ask is fully filled or no more matching bids

3. If order is not fully filled:
   - Add remaining quantity to the orderbook
   - Orders at same price level are sorted by time (FIFO)

### Methods

| Method | Request | Response | Description |
|--------|---------|----------|-------------|
| `PlaceBid` | `PlaceOrderRequest{price, quantity}` | `PlaceOrderResponse` | Place a buy order |
| `PlaceAsk` | `PlaceOrderRequest{price, quantity}` | `PlaceOrderResponse` | Place a sell order |
| `GetOrderbook` | `GetOrderbookRequest{depth}` | `OrderbookSnapshot` | Get current orderbook state |
| `Subscribe` | `SubscribeRequest{}` | `SubscribeResponse` | Subscribe to real-time updates |
| `Unsubscribe` | `UnsubscribeRequest{}` | `UnsubscribeResponse` | Unsubscribe from updates |

## Protocol Buffers

```protobuf
syntax = "proto3";

package trading;

option go_package = "github.com/xiaonanln/goverse/samples/trading/proto;trading_pb";

// PlaceOrderRequest is the request to place a bid or ask order
message PlaceOrderRequest {
  int64 price = 1;     // Price in smallest unit (e.g., 5000000 = $50,000.00)
  int64 quantity = 2;  // Quantity to trade
}

// PlaceOrderResponse is the response after placing an order
message PlaceOrderResponse {
  string order_id = 1;        // Unique order ID
  string status = 2;          // "placed", "filled", "partial"
  int64 filled_quantity = 3;  // Quantity that was immediately filled
  int64 remaining = 4;        // Quantity remaining in orderbook
  repeated Trade trades = 5;  // Trades that occurred
}

// GetOrderbookRequest is the request to get orderbook snapshot
message GetOrderbookRequest {
  int32 depth = 1;  // Number of price levels to return (default: 10)
}

// OrderbookSnapshot is the current state of the orderbook
message OrderbookSnapshot {
  string symbol = 1;
  repeated PriceLevel bids = 2;  // Best bid first (descending price)
  repeated PriceLevel asks = 3;  // Best ask first (ascending price)
  int64 last_trade_price = 4;
  int64 last_trade_quantity = 5;
  int64 timestamp = 6;
}

// PriceLevel represents a single price level in the orderbook
message PriceLevel {
  int64 price = 1;
  int64 quantity = 2;
  int32 order_count = 3;  // Number of orders at this level
}

// Trade represents a completed trade
message Trade {
  int64 id = 1;
  int64 price = 2;
  int64 quantity = 3;
  int64 timestamp = 4;
}

// SubscribeRequest is the request to subscribe to orderbook updates
message SubscribeRequest {
  // Empty - client ID is obtained from call context
}

// SubscribeResponse is the response after subscribing
message SubscribeResponse {
  bool success = 1;
  string client_id = 2;
}

// UnsubscribeRequest is the request to unsubscribe from updates
message UnsubscribeRequest {
  // Empty - client ID is obtained from call context
}

// UnsubscribeResponse is the response after unsubscribing
message UnsubscribeResponse {
  bool success = 1;
}

// OrderbookUpdate is pushed to subscribers when orderbook changes
// This is sent via Gate push messaging (SSE)
message OrderbookUpdate {
  string symbol = 1;
  repeated PriceLevel bids = 2;
  repeated PriceLevel asks = 3;
  repeated Trade new_trades = 4;  // New trades since last update
  int64 timestamp = 5;
}
```

## API Design

### HTTP Endpoints

All requests use base64-encoded protobuf wrapped in JSON (standard Goverse format).

#### Place Bid Order
```bash
POST /api/v1/objects/call/Orderbook/Orderbook-BTC/PlaceBid
X-Client-ID: <client-id>  # Optional, for tracking orders
{
  "request": "<base64 of PlaceOrderRequest{price: 5000000, quantity: 100}>"
}
# Note: 5000000 = $50,000.00 (price in cents)
Response: {
  "response": "<base64 of PlaceOrderResponse>"
}
```

#### Place Ask Order
```bash
POST /api/v1/objects/call/Orderbook/Orderbook-BTC/PlaceAsk
X-Client-ID: <client-id>
{
  "request": "<base64 of PlaceOrderRequest{price: 5100000, quantity: 50}>"
}
Response: {
  "response": "<base64 of PlaceOrderResponse>"
}
```

#### Get Orderbook Snapshot
```bash
POST /api/v1/objects/call/Orderbook/Orderbook-BTC/GetOrderbook
{
  "request": "<base64 of GetOrderbookRequest{depth: 10}>"
}
Response: {
  "response": "<base64 of OrderbookSnapshot>"
}
```

#### Subscribe to Updates
```bash
POST /api/v1/objects/call/Orderbook/Orderbook-BTC/Subscribe
X-Client-ID: <client-id>  # Required for push messaging
{
  "request": "<base64 of SubscribeRequest>"
}
Response: {
  "response": "<base64 of SubscribeResponse{success: true}>"
}
```

#### SSE Event Stream

Clients subscribe to real-time updates via SSE:

```bash
GET /api/v1/events/stream
Accept: text/event-stream
```

Events received:
1. `register`: Contains client ID to use for subscription
2. `message`: Contains orderbook updates (OrderbookUpdate protobuf)
3. `heartbeat`: Keep-alive signal

### Example Flow

1. **Connect to SSE**:
   ```javascript
   const eventSource = new EventSource('/api/v1/events/stream');
   eventSource.addEventListener('register', (e) => {
     const { clientId } = JSON.parse(e.data);
     // Use clientId for subsequent requests
   });
   ```

2. **Subscribe to Orderbook**:
   ```javascript
   fetch('/api/v1/objects/call/Orderbook/Orderbook-BTC/Subscribe', {
     method: 'POST',
     headers: { 'X-Client-ID': clientId },
     body: JSON.stringify({ request: encodeSubscribeRequest() })
   });
   ```

3. **Listen for Updates**:
   ```javascript
   eventSource.addEventListener('message', (e) => {
     const { payload } = JSON.parse(e.data);
     const update = decodeOrderbookUpdate(payload);
     updateOrderbookDisplay(update);
   });
   ```

4. **Place Orders**:
   ```javascript
   async function placeBid(price, quantity) {
     const response = await fetch(
       '/api/v1/objects/call/Orderbook/Orderbook-BTC/PlaceBid',
       {
         method: 'POST',
         headers: { 'X-Client-ID': clientId },
         body: JSON.stringify({
           request: encodePlaceOrderRequest(price, quantity)
         })
       }
     );
     return response.json();
   }
   ```

## Orderbook Implementation

### Core Logic

```go
// PlaceBid places a buy order
func (o *Orderbook) PlaceBid(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.PlaceOrderResponse, error) {
    o.mu.Lock()
    defer o.mu.Unlock()

    clientID := goverseapi.CallerClientID(ctx)
    order := o.createOrder("bid", req.Price, req.Quantity, clientID)
    
    // Try to match against asks
    trades := o.matchBid(order)
    
    // Add remaining to orderbook
    if order.Remaining > 0 {
        o.addBidToBook(order)
    }
    
    // Push update to subscribers
    o.notifySubscribers(ctx)
    
    return o.buildResponse(order, trades), nil
}

// matchBid matches a bid order against existing asks
func (o *Orderbook) matchBid(bid *Order) []*Trade {
    var trades []*Trade
    
    for len(o.asks) > 0 && bid.Remaining > 0 {
        bestAsk := o.asks[0]
        
        // No match if bid price < ask price
        if bid.Price < bestAsk.Price {
            break
        }
        
        // Match at ask's price (taker gets maker's price)
        for _, askOrder := range bestAsk.Orders {
            if bid.Remaining == 0 {
                break
            }
            
            fillQty := min(bid.Remaining, askOrder.Remaining)
            
            trade := o.createTrade(bestAsk.Price, fillQty, bid.ClientID, askOrder.ClientID)
            trades = append(trades, trade)
            
            bid.Remaining -= fillQty
            askOrder.Remaining -= fillQty
            bestAsk.Quantity -= fillQty
        }
        
        // Remove empty orders from ask level
        o.cleanupPriceLevel(bestAsk, "ask")
        
        // Remove empty price level
        if bestAsk.Quantity == 0 {
            o.asks = o.asks[1:]
        }
    }
    
    return trades
}
```

### Push Notification

```go
// notifySubscribers pushes orderbook update to all subscribers
func (o *Orderbook) notifySubscribers(ctx context.Context) {
    update := o.buildOrderbookUpdate()
    
    for _, clientID := range o.subscribers {
        err := goverseapi.PushMessageToClient(ctx, clientID, update)
        if err != nil {
            o.Logger.Warnf("Failed to push to client %s: %v", clientID, err)
            // Don't remove subscriber here - they might reconnect
        }
    }
}
```

## Web Client Design

```
┌────────────────────────────────────────────────────────────────┐
│                    Trading System - BTC                         │
├──────────────────────────────┬─────────────────────────────────┤
│         ASKS (Sell)          │           BIDS (Buy)            │
├──────────────────────────────┼─────────────────────────────────┤
│  Price     Quantity  Orders  │  Price     Quantity  Orders     │
│  $510.00      50        2    │  $500.00     100        3       │
│  $505.00     100        5    │  $499.00      75        2       │
│  $503.00      30        1    │  $498.00     200        8       │
│  $502.00      80        4    │  $497.00      50        1       │
│  $501.00      25        1    │  $495.00     150        5       │
├──────────────────────────────┴─────────────────────────────────┤
│                    Last Trade: $500.50 x 25                     │
├────────────────────────────────────────────────────────────────┤
│                       PLACE ORDER                               │
│  ┌────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ │
│  │ [BID]  │ │ Price: 499.00│ │ Quantity: 10 │ │   [Submit]   │ │
│  │ [ASK]  │ └──────────────┘ └──────────────┘ └──────────────┘ │
└────────────────────────────────────────────────────────────────┘
```

### Features

- Real-time orderbook display with price levels
- Visual indication of best bid/ask spread
- Order form with side (bid/ask), price, and quantity
- Recent trades ticker
- Connection status indicator

## Project Structure

```
samples/trading/
├── DESIGN.md              # This file
├── README.md              # User documentation
├── compile-proto.sh       # Proto compilation script
├── proto/
│   └── trading.proto      # Protocol buffer definitions
├── server/
│   ├── main.go           # Server entry point
│   └── orderbook.go      # Orderbook object implementation
└── web/
    ├── index.html        # Trading UI
    ├── style.css         # Styling
    └── trading.js        # Trading logic & SSE client
```

## Running the Sample

### Prerequisites

- etcd running on localhost:2379
- Go 1.21+
- Compiled proto files

### Start the System

```bash
# Terminal 1: Start etcd
./script/codespace/start-etcd.sh

# Terminal 2: Start Gate server
cd cmd/gate
go run .

# Terminal 3: Start Trading server
cd samples/trading/server
go run .

# Terminal 4: Serve web client
cd samples/trading/web
python3 -m http.server 3000

# Open browser
open http://localhost:3000
```

### Test with curl

```bash
# Get orderbook 
# Note: The request requires base64-encoded protobuf. Empty GetOrderbookRequest
# with default depth can be encoded as an empty protobuf message.
# For proper testing, use the helper scripts or a proper client.
curl -X POST http://localhost:48000/api/v1/objects/call/Orderbook/Orderbook-BTC/GetOrderbook \
  -H "Content-Type: application/json" \
  -d '{"request": "CAo="}'  # base64 of GetOrderbookRequest{depth: 10}
```

## Implementation Plan

### Phase 1: Core Objects (Go)
1. Create proto file and generate Go code
2. Implement Orderbook object with basic structure
3. Implement PlaceBid/PlaceAsk with matching logic
4. Implement GetOrderbook

### Phase 2: Real-time Updates
1. Implement Subscribe/Unsubscribe methods
2. Add push notification on orderbook changes
3. Test SSE integration with Gate

### Phase 3: Web Client
1. Create index.html with orderbook display
2. Create style.css for visual design
3. Create trading.js with SSE client and order placement
4. Add protobuf.js for encoding/decoding

### Phase 4: Integration & Polish
1. End-to-end testing
2. Error handling and edge cases
3. README with setup instructions

## Learning Outcomes

After studying this sample, developers will understand:

1. **Stateful Object Design**: How to design complex stateful objects with internal data structures
2. **Order Matching**: Basic price-time priority order matching logic
3. **Real-time Push**: How to push updates to connected clients via SSE
4. **Subscription Management**: How to track and manage client subscriptions
5. **Distributed Architecture**: How Goverse handles orderbook distribution

## Future Enhancements

Potential extensions for learning:

1. **Order Cancellation**: Add ability to cancel pending orders
2. **Multiple Symbols**: Support multiple trading pairs
3. **Order Types**: Add market orders, stop-loss, etc.
4. **Persistence**: Save orderbook state to database
5. **Trade History**: Maintain and query historical trades
6. **WebSocket**: Alternative to SSE for bidirectional communication
7. **Authentication**: User accounts and order ownership
