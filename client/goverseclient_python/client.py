# Copyright (c) 2024 Goverse Authors. All rights reserved.
# Licensed under the MIT License.
"""
Goverse Python Client

Provides a client library for connecting to Goverse gate servers.
It handles connection management, automatic failover between multiple gate addresses,
and provides a user-friendly API for interacting with Goverse services.
"""

import logging
import threading
from dataclasses import dataclass, field
from typing import Callable, List, Optional

import grpc
from google.protobuf.any_pb2 import Any as AnyProto
from google.protobuf.message import Message

# Import gate proto - these are generated from client/proto/gate.proto
# The path depends on how protobuf files are generated
import sys
from pathlib import Path

# Add repository root directory to path to import client.proto
_current_dir = Path(__file__).parent
_repo_root = _current_dir.parent.parent
sys.path.insert(0, str(_repo_root))

try:
    from client.proto import gate_pb2, gate_pb2_grpc
except ImportError:
    # Try alternative import path (from client directory)
    try:
        _client_dir = _current_dir.parent
        sys.path.insert(0, str(_client_dir))
        from proto import gate_pb2, gate_pb2_grpc
    except ImportError as e:
        raise ImportError(
            "Failed to import gate_pb2. Please run ./script/compile-proto.sh to generate the proto files."
        ) from e

# Default timeouts in seconds
DEFAULT_CONNECTION_TIMEOUT = 30.0
DEFAULT_CALL_TIMEOUT = 30.0
DEFAULT_RECONNECT_INTERVAL = 5.0

# Message channel buffer size
MESSAGE_CHANNEL_BUFFER_SIZE = 100


class GoverseClientError(Exception):
    """Base exception for Goverse client errors."""
    pass


class NoAddressesError(GoverseClientError):
    """Raised when no gate addresses are provided."""
    pass


class NotConnectedError(GoverseClientError):
    """Raised when trying to call methods while not connected."""
    pass


class ClientClosedError(GoverseClientError):
    """Raised when trying to use a closed client."""
    pass


class ConnectionFailedError(GoverseClientError):
    """Raised when connection to all gates fails."""
    pass


# Type aliases for callbacks
MessageHandler = Callable[[Message], None]
ConnectCallback = Callable[[str], None]
DisconnectCallback = Callable[[Optional[Exception]], None]


@dataclass
class ClientOptions:
    """Configuration options for the Goverse client.

    Attributes:
        connection_timeout: Timeout in seconds for establishing a connection.
        call_timeout: Default timeout in seconds for RPC calls.
        reconnect_interval: Interval in seconds between reconnection attempts.
        grpc_options: Additional gRPC channel options.
        logger: Logger instance to use. If None, a default logger is created.
        on_connect: Callback called when a connection is established.
        on_disconnect: Callback called when the connection is lost.
        on_message: Callback called when a push message is received.
    """
    connection_timeout: float = DEFAULT_CONNECTION_TIMEOUT
    call_timeout: float = DEFAULT_CALL_TIMEOUT
    reconnect_interval: float = DEFAULT_RECONNECT_INTERVAL
    grpc_options: List[tuple] = field(default_factory=list)
    logger: Optional[logging.Logger] = None
    on_connect: Optional[ConnectCallback] = None
    on_disconnect: Optional[DisconnectCallback] = None
    on_message: Optional[MessageHandler] = None


class Client:
    """Main client for interacting with Goverse services via a gate.

    It manages connections to gate servers, handles automatic reconnection,
    and provides methods for object operations.

    Example:
        >>> client = Client(["localhost:48000"])
        >>> client.connect()
        >>> client.create_object("Counter", "Counter-test")
        'Counter-test'
        >>> client.close()
    """

    def __init__(
        self,
        addresses: List[str],
        options: Optional[ClientOptions] = None,
    ) -> None:
        """Create a new Goverse client.

        Args:
            addresses: List of gate server addresses to try connecting to.
            options: Client configuration options. If None, uses defaults.

        Raises:
            NoAddressesError: If no addresses are provided.
        """
        if not addresses:
            raise NoAddressesError("No gate addresses provided")

        # Copy addresses to prevent external modification
        self._addresses = list(addresses)
        self._options = options or ClientOptions()

        # Set up logger
        if self._options.logger is None:
            self._logger = logging.getLogger("GoverseClient")
        else:
            self._logger = self._options.logger

        # Connection state
        self._channel: Optional[grpc.Channel] = None
        self._stub: Optional[gate_pb2_grpc.GateServiceStub] = None
        self._stream: Optional[grpc.Future] = None
        self._client_id: str = ""
        self._current_addr: str = ""

        # Thread safety
        self._lock = threading.RLock()
        self._connected = False
        self._closed = False

        # Message receiving
        self._stream_thread: Optional[threading.Thread] = None
        self._running = False

    def connect(self, timeout: Optional[float] = None) -> None:
        """Establish a connection to a gate server.

        Tries each gate address in order until one succeeds.
        After connecting, starts a background thread to receive push messages.

        Args:
            timeout: Connection timeout in seconds. If None, uses the default.

        Raises:
            ClientClosedError: If the client has been closed.
            ConnectionFailedError: If connection to all gates fails.
        """
        with self._lock:
            if self._closed:
                raise ClientClosedError("Client is closed")
            if self._connected:
                return

        timeout = timeout or self._options.connection_timeout

        # Try to connect to each address
        for addr in self._addresses:
            try:
                self._connect_to_address(addr, timeout)
                return
            except Exception as e:
                self._logger.warning(f"Failed to connect to {addr}: {e}")

        raise ConnectionFailedError("Failed to connect to any gate server")

    def _connect_to_address(self, addr: str, timeout: float) -> None:
        """Attempt to connect to a specific gate address.

        Args:
            addr: Gate server address (host:port).
            timeout: Connection timeout in seconds.

        Raises:
            ClientClosedError: If the client was closed during connection.
            Exception: If connection fails.
        """
        self._logger.info(f"Connecting to gate at {addr}")

        # Create gRPC channel options
        options = [
            ("grpc.enable_retries", 1),
            ("grpc.keepalive_time_ms", 30000),
            ("grpc.keepalive_timeout_ms", 10000),
        ]
        options.extend(self._options.grpc_options)

        # Create channel
        channel = grpc.insecure_channel(addr, options=options)

        # Wait for channel to be ready
        try:
            grpc.channel_ready_future(channel).result(timeout=timeout)
        except grpc.FutureTimeoutError as e:
            channel.close()
            raise ConnectionFailedError(f"Timeout connecting to {addr}") from e

        # Create stub
        stub = gate_pb2_grpc.GateServiceStub(channel)

        # Register with the gate
        try:
            stream = stub.Register(gate_pb2.Empty())

            # Receive the registration response
            first_msg = next(stream)
            reg_response = gate_pb2.RegisterResponse()
            if not first_msg.Unpack(reg_response):
                channel.close()
                raise ConnectionFailedError(
                    f"Unexpected response type: {first_msg.type_url}"
                )

            client_id = reg_response.client_id

        except grpc.RpcError as e:
            channel.close()
            raise ConnectionFailedError(f"Failed to register with gate: {e}") from e

        # Update client state
        with self._lock:
            if self._closed:
                channel.close()
                raise ClientClosedError("Client is closed")

            self._channel = channel
            self._stub = stub
            self._stream = stream
            self._client_id = client_id
            self._current_addr = addr
            self._connected = True
            self._running = True

        self._logger.info(f"Connected to gate at {addr}, client ID: {client_id}")

        # Notify connection callback
        if self._options.on_connect:
            try:
                self._options.on_connect(client_id)
            except Exception as e:
                self._logger.warning(f"Error in on_connect callback: {e}")

        # Start message receiver thread
        self._stream_thread = threading.Thread(
            target=self._receive_messages,
            daemon=True,
            name="GoverseClient-MessageReceiver",
        )
        self._stream_thread.start()

    def _receive_messages(self) -> None:
        """Continuously receive push messages from the gate."""
        while True:
            with self._lock:
                stream = self._stream
                closed = self._closed
                connected = self._connected
                running = self._running

            if closed or not connected or stream is None or not running:
                return

            try:
                msg_any = next(stream)

                # Unmarshal and handle the message
                if self._options.on_message:
                    try:
                        self._options.on_message(msg_any)
                    except Exception as e:
                        self._logger.warning(f"Error in on_message callback: {e}")

            except StopIteration:
                # Stream ended
                self._handle_disconnect(None)
                return
            except grpc.RpcError as e:
                with self._lock:
                    was_closed = self._closed

                if was_closed:
                    return

                self._logger.error(f"Error receiving message from stream: {e}")

                # Notify disconnect callback
                if self._options.on_disconnect:
                    try:
                        self._options.on_disconnect(e)
                    except Exception as cb_err:
                        self._logger.warning(f"Error in on_disconnect callback: {cb_err}")

                self._handle_disconnect(e)
                return

    def _handle_disconnect(self, error: Optional[Exception]) -> None:
        """Handle a disconnection event.

        Args:
            error: The error that caused the disconnect, if any.
        """
        with self._lock:
            if self._closed:
                return

            self._connected = False
            self._running = False

            if self._channel:
                try:
                    self._channel.close()
                except Exception:
                    pass
                self._channel = None

            self._stream = None
            self._stub = None
            self._client_id = ""
            self._current_addr = ""

    def close(self) -> None:
        """Close the client and release all resources."""
        with self._lock:
            if self._closed:
                return

            self._closed = True
            self._connected = False
            self._running = False

            if self._channel:
                try:
                    self._channel.close()
                except Exception:
                    pass

        # Wait for stream thread to finish
        if self._stream_thread and self._stream_thread.is_alive():
            self._stream_thread.join(timeout=5.0)

        self._logger.info("Client closed")

    def is_connected(self) -> bool:
        """Return True if the client is connected to a gate.

        Returns:
            True if connected, False otherwise.
        """
        with self._lock:
            return self._connected and not self._closed

    @property
    def client_id(self) -> str:
        """Return the current client ID assigned by the gate.

        Returns:
            The client ID, or empty string if not connected.
        """
        with self._lock:
            return self._client_id

    @property
    def current_address(self) -> str:
        """Return the address of the currently connected gate.

        Returns:
            The gate address, or empty string if not connected.
        """
        with self._lock:
            return self._current_addr

    def call_object(
        self,
        object_type: str,
        object_id: str,
        method: str,
        request: Optional[Message] = None,
        timeout: Optional[float] = None,
    ) -> Optional[AnyProto]:
        """Call a method on an object and return the response.

        Args:
            object_type: The type of the object to call.
            object_id: The ID of the object to call.
            method: The method name to call.
            request: The request protobuf message. Can be None.
            timeout: Call timeout in seconds. If None, uses the default.

        Returns:
            The response as a protobuf Any message, or None if no response.

        Raises:
            ClientClosedError: If the client has been closed.
            NotConnectedError: If the client is not connected.
            grpc.RpcError: If the RPC call fails.
        """
        with self._lock:
            if self._closed:
                raise ClientClosedError("Client is closed")
            if not self._connected:
                raise NotConnectedError("Client is not connected")
            stub = self._stub
            client_id = self._client_id

        timeout = timeout or self._options.call_timeout

        # Marshal request to Any (handle None request)
        any_req = None
        if request is not None:
            any_req = AnyProto()
            any_req.Pack(request)

        # Make the call
        req = gate_pb2.CallObjectRequest(
            client_id=client_id,
            type=object_type,
            id=object_id,
            method=method,
            request=any_req,
        )

        resp = stub.CallObject(req, timeout=timeout)

        if resp is None or resp.response is None:
            return None

        return resp.response

    def call_object_any(
        self,
        object_type: str,
        object_id: str,
        method: str,
        request: Optional[AnyProto] = None,
        timeout: Optional[float] = None,
    ) -> Optional[AnyProto]:
        """Call a method on an object using protobuf Any directly.

        This is useful when working with generic protobuf messages.

        Args:
            object_type: The type of the object to call.
            object_id: The ID of the object to call.
            method: The method name to call.
            request: The request as a protobuf Any message. Can be None.
            timeout: Call timeout in seconds. If None, uses the default.

        Returns:
            The response as a protobuf Any message, or None if no response.

        Raises:
            ClientClosedError: If the client has been closed.
            NotConnectedError: If the client is not connected.
            grpc.RpcError: If the RPC call fails.
        """
        with self._lock:
            if self._closed:
                raise ClientClosedError("Client is closed")
            if not self._connected:
                raise NotConnectedError("Client is not connected")
            stub = self._stub
            client_id = self._client_id

        timeout = timeout or self._options.call_timeout

        req = gate_pb2.CallObjectRequest(
            client_id=client_id,
            type=object_type,
            id=object_id,
            method=method,
            request=request,
        )

        resp = stub.CallObject(req, timeout=timeout)

        if resp is None:
            return None

        return resp.response

    def create_object(
        self,
        object_type: str,
        object_id: str,
        timeout: Optional[float] = None,
    ) -> str:
        """Create a new object of the specified type with the given ID.

        Args:
            object_type: The type of object to create.
            object_id: The ID for the new object.
            timeout: Call timeout in seconds. If None, uses the default.

        Returns:
            The ID of the created object.

        Raises:
            ClientClosedError: If the client has been closed.
            NotConnectedError: If the client is not connected.
            grpc.RpcError: If the RPC call fails.
        """
        with self._lock:
            if self._closed:
                raise ClientClosedError("Client is closed")
            if not self._connected:
                raise NotConnectedError("Client is not connected")
            stub = self._stub

        timeout = timeout or self._options.call_timeout

        req = gate_pb2.CreateObjectRequest(
            type=object_type,
            id=object_id,
        )

        resp = stub.CreateObject(req, timeout=timeout)

        return resp.id

    def delete_object(
        self,
        object_id: str,
        timeout: Optional[float] = None,
    ) -> None:
        """Delete an object by its ID.

        Args:
            object_id: The ID of the object to delete.
            timeout: Call timeout in seconds. If None, uses the default.

        Raises:
            ClientClosedError: If the client has been closed.
            NotConnectedError: If the client is not connected.
            grpc.RpcError: If the RPC call fails.
        """
        with self._lock:
            if self._closed:
                raise ClientClosedError("Client is closed")
            if not self._connected:
                raise NotConnectedError("Client is not connected")
            stub = self._stub

        timeout = timeout or self._options.call_timeout

        req = gate_pb2.DeleteObjectRequest(id=object_id)

        stub.DeleteObject(req, timeout=timeout)

    def reconnect(self, timeout: Optional[float] = None) -> None:
        """Attempt to reconnect to a gate server.

        First closes any existing connection, then tries to connect to any available gate.

        Args:
            timeout: Connection timeout in seconds. If None, uses the default.

        Raises:
            ClientClosedError: If the client has been closed.
            ConnectionFailedError: If connection to all gates fails.
        """
        with self._lock:
            if self._closed:
                raise ClientClosedError("Client is closed")

            # Clean up existing connection
            self._connected = False
            self._running = False

            if self._channel:
                try:
                    self._channel.close()
                except Exception:
                    pass
                self._channel = None

            self._stream = None
            self._stub = None
            self._client_id = ""
            self._current_addr = ""

        # Wait for stream thread to finish
        if self._stream_thread and self._stream_thread.is_alive():
            self._stream_thread.join(timeout=2.0)

        # Try to connect
        self.connect(timeout=timeout)

    def wait_for_connection(self, timeout: float = 30.0) -> None:
        """Wait until the client is connected or timeout expires.

        Args:
            timeout: Maximum time to wait in seconds.

        Raises:
            TimeoutError: If connection is not established within timeout.
        """
        import time

        start_time = time.time()
        while time.time() - start_time < timeout:
            if self.is_connected():
                return
            time.sleep(0.1)

        raise TimeoutError("Timed out waiting for connection")
