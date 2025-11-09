# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

### Breaking Changes

#### CallObject API now requires object type parameter

The `CallObject` method signature has been updated to require the object type as an explicit parameter. This affects all layers of the API stack.

**Previous Signature:**
```go
CallObject(ctx context.Context, id string, method string, request proto.Message) (proto.Message, error)
```

**New Signature:**
```go
CallObject(ctx context.Context, objType, id string, method string, request proto.Message) (proto.Message, error)
```

**Migration Guide:**

All calls to `CallObject` must now include the object type as the second parameter:

```go
// Old
resp, err := goverseapi.CallObject(ctx, "ChatRoom-general", "SendMessage", req)

// New - provide the object type ("ChatRoom")
resp, err := goverseapi.CallObject(ctx, "ChatRoom", "ChatRoom-general", "SendMessage", req)
```

For client objects, pass the client object type:
```go
// Calling a client object method
resp, err := goverseapi.CallObject(ctx, "ChatClient", clientID, "MethodName", req)
```

**Why This Change:**

- **Type Safety:** The node can now validate that the object type matches before attempting to call methods, providing earlier and clearer error messages.
- **Better Routing:** Enables future optimizations where type information can be used for routing decisions.
- **Improved Logging:** All CallObject operations now log the object type for better observability and debugging.

**What Changed:**

- `goverseapi.CallObject()` - Updated signature
- `cluster.Cluster.CallObject()` - Updated signature, passes type to remote nodes
- `node.Node.CallObject()` - Updated signature, validates type matches object
- `server.Server.CallObject()` - Updated to read type from protobuf request
- `proto.CallObjectRequest` - Added `type` field (field number 3)
- All tests and samples updated to pass object type

**Impact on Generated Code:**

The protobuf message `CallObjectRequest` now includes a `type` field. All generated `.pb.go` files are included in the repository, so no protoc compilation is required during build.
