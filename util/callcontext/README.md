# callcontext

Package `callcontext` provides utilities for managing call context metadata in GoVerse object methods.

## Overview

When an object method is called, the call can originate from three different sources:

1. **Client call from gate** - Has a `client_id` (format: `"gateAddress/uniqueId"`)
2. **Object call from other node** - No `client_id`
3. **Local call from local cluster** - No `client_id`

The `callcontext` package allows object methods to determine the source of a call by checking for the presence of a `client_id` in the context.

## Usage

### In Object Methods

Use the high-level API from `goverseapi`:

```go
import (
    "context"
    "github.com/xiaonanln/goverse/goverseapi"
)

type MyObject struct {
    goverseapi.BaseObject
}

func (obj *MyObject) MyMethod(ctx context.Context, req *MyRequest) (*MyResponse, error) {
    // Check if the call came from a client
    if goverseapi.HasClientID(ctx) {
        clientID := goverseapi.GetClientID(ctx)
        obj.Logger.Infof("Client %s called MyMethod", clientID)
        
        // You can send push notifications back to this client
        notification := &MyNotification{Message: "Hello!"}
        err := goverseapi.PushMessageToClient(ctx, clientID, notification)
        if err != nil {
            obj.Logger.Errorf("Failed to push message: %v", err)
        }
    } else {
        obj.Logger.Infof("Internal call to MyMethod (from another object or local cluster)")
    }
    
    // ... rest of method logic
    return &MyResponse{}, nil
}
```

### Call Sources

| Source | `HasClientID(ctx)` | `GetClientID(ctx)` |
|--------|-------------------|-------------------|
| Client via Gate | `true` | `"localhost:7001/abc123"` |
| Object on Other Node | `false` | `""` |
| Local Cluster Call | `false` | `""` |

## Implementation Details

The package uses typed context keys to avoid collisions:

```go
type contextKey int

const (
    clientIDKey contextKey = iota
)
```

This ensures that context values set by this package don't conflict with other context values in the system.

## API Reference

### `WithClientID(ctx context.Context, clientID string) context.Context`

Returns a new context with the client ID stored. This is called internally by the gateway server when processing client requests.

### `GetClientID(ctx context.Context) string`

Retrieves the client ID from the context. Returns empty string if no client ID is present.

### `HasClientID(ctx context.Context) bool`

Checks if the context contains a client ID. Returns `true` if the call originated from a client via the gate.

## See Also

- [GATEWAY_DESIGN.md](../../../GATEWAY_DESIGN.md) - Gateway architecture documentation
- [goverseapi](../../../goverseapi) - High-level API for object methods
