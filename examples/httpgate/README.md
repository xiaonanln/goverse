# HTTP Gate Example

This example demonstrates how to encode protobuf messages for HTTP requests to the Goverse HTTP Gate REST API.

## Overview

The HTTP Gate provides a REST API for object operations:
- Create objects
- Call object methods
- Delete objects

All CallObject requests use base64-encoded protobuf `Any` messages as specified in the design document.

## Running the Example

```bash
cd examples/httpgate
go run main.go
```

This will print example curl commands showing how to:
1. Create objects
2. Call object methods with encoded protobuf messages
3. Delete objects

## Example Output

The example shows how to encode different types of protobuf messages (Int32Value, StringValue, etc.) into the base64 format required by the HTTP API.

## Request/Response Format

### Call Object

**Request:**
```json
{
  "request": "<base64-encoded protobuf Any bytes>"
}
```

**Response:**
```json
{
  "response": "<base64-encoded protobuf Any bytes>"
}
```

### Create Object

**Response:**
```json
{
  "id": "object-id"
}
```

### Delete Object

**Response:**
```json
{
  "success": true
}
```

### Error Response

**Response:**
```json
{
  "error": "error message",
  "code": "ERROR_CODE"
}
```

## Encoding Protobuf Messages

To encode a protobuf message for HTTP requests:

1. Create your protobuf message (e.g., `wrapperspb.Int32Value{Value: 1}`)
2. Wrap it in `google.protobuf.Any` using `anypb.New(msg)`
3. Marshal the Any to bytes using `proto.Marshal(anyMsg)`
4. Base64 encode the bytes using `base64.StdEncoding.EncodeToString(bytes)`
5. Send in JSON as `{"request": "base64-encoded-string"}`

The example code includes a helper function `callObjectHTTP` that demonstrates this process.

## Notes

- This example uses a mock persistence provider for simplicity
- No etcd is required for this example (single node setup)
- The Counter object is a simple demonstration - see `main.go` for implementation
- For production use with etcd, set the `EtcdAddress` in the gate configuration
