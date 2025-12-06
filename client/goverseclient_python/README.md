# Goverse Python Client

A Python client library for connecting to Goverse gate servers. It handles connection management, automatic failover between multiple gate addresses, and provides a user-friendly API for interacting with Goverse services.

## Installation

```bash
pip install grpcio protobuf
```

Or for development:

```bash
pip install grpcio protobuf grpcio-tools pytest
```

## Prerequisites

Before using the client, you need to generate the Python protobuf files:

```bash
cd /path/to/goverse
./script/compile-proto.sh
```

This will generate the required `gate_pb2.py` and `gate_pb2_grpc.py` files in `gate/proto/`.

## Quick Start

```python
from client.goverseclient_python import Client, ClientOptions

# Create client with gate addresses
client = Client(["localhost:48000"])

# Connect to gate
client.connect()

# Check connection status
if client.is_connected():
    print(f"Connected with client ID: {client.client_id}")

# Create an object
object_id = client.create_object("Counter", "Counter-myid")

# Call a method on the object
from google.protobuf.any_pb2 import Any
from samples.counter.proto import counter_pb2

request = counter_pb2.IncrementRequest(amount=5)
response_any = client.call_object("Counter", "Counter-myid", "Increment", request)

# Unpack the response
response = counter_pb2.CounterResponse()
response_any.Unpack(response)
print(f"Counter value: {response.value}")

# Delete the object
client.delete_object("Counter-myid")

# Close the client
client.close()
```

## API Reference

### Client

The main class for interacting with Goverse services.

#### Constructor

```python
Client(addresses: List[str], options: Optional[ClientOptions] = None)
```

- `addresses`: List of gate server addresses to try connecting to
- `options`: Optional client configuration options

#### Methods

- `connect(timeout: Optional[float] = None)` - Establish a connection to a gate server
- `close()` - Close the client and release all resources
- `is_connected() -> bool` - Return True if connected to a gate
- `reconnect(timeout: Optional[float] = None)` - Reconnect to a gate server
- `wait_for_connection(timeout: float = 30.0)` - Wait until connected or timeout
- `create_object(object_type: str, object_id: str, timeout: Optional[float] = None) -> str` - Create a new object
- `delete_object(object_id: str, timeout: Optional[float] = None)` - Delete an object
- `call_object(object_type: str, object_id: str, method: str, request: Optional[Message] = None, timeout: Optional[float] = None) -> Optional[Any]` - Call a method on an object
- `call_object_any(object_type: str, object_id: str, method: str, request: Optional[Any] = None, timeout: Optional[float] = None) -> Optional[Any]` - Call a method using protobuf Any directly

#### Properties

- `client_id: str` - The client ID assigned by the gate
- `current_address: str` - The address of the currently connected gate

### ClientOptions

Configuration options for the client.

```python
@dataclass
class ClientOptions:
    connection_timeout: float = 30.0  # Connection timeout in seconds
    call_timeout: float = 30.0        # RPC call timeout in seconds
    reconnect_interval: float = 5.0   # Reconnection interval in seconds
    grpc_options: List[tuple] = []    # Additional gRPC channel options
    logger: Optional[logging.Logger] = None  # Logger instance
    on_connect: Optional[Callable[[str], None]] = None      # Connection callback
    on_disconnect: Optional[Callable[[Optional[Exception]], None]] = None  # Disconnect callback
    on_message: Optional[Callable[[Message], None]] = None  # Message callback
```

### Exceptions

- `GoverseClientError` - Base exception for all client errors
- `NoAddressesError` - Raised when no gate addresses are provided
- `NotConnectedError` - Raised when trying to call methods while not connected
- `ClientClosedError` - Raised when trying to use a closed client
- `ConnectionFailedError` - Raised when connection to all gates fails

## Using Callbacks

```python
def on_connect(client_id: str):
    print(f"Connected with ID: {client_id}")

def on_disconnect(error: Optional[Exception]):
    print(f"Disconnected: {error}")

def on_message(msg: Any):
    print(f"Received message: {msg.type_url}")

options = ClientOptions(
    on_connect=on_connect,
    on_disconnect=on_disconnect,
    on_message=on_message,
)

client = Client(["localhost:48000"], options=options)
```

## Thread Safety

The `Client` class is thread-safe. Multiple threads can call methods on the same client instance concurrently.

## License

Apache-2.0 License - see the LICENSE file for details.
