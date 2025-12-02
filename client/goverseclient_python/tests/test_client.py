# Copyright (c) 2024 Goverse Authors. All rights reserved.
# Licensed under the MIT License.
"""Unit tests for the Goverse Python client library."""

import logging
import threading
import time
import unittest
from unittest.mock import MagicMock, patch

import sys
from pathlib import Path

# Add repository root to path for imports (to match how proto files are generated)
_current_dir = Path(__file__).parent
_repo_root = _current_dir.parent.parent.parent.parent
_client_dir = _current_dir.parent.parent
sys.path.insert(0, str(_repo_root))
sys.path.insert(0, str(_client_dir))

# Skip tests if grpc is not available
try:
    import grpc
except ImportError:
    grpc = None

# Import the client module
if grpc:
    from goverseclient_python import (
        Client,
        ClientOptions,
        GoverseClientError,
        NoAddressesError,
        NotConnectedError,
        ClientClosedError,
        ConnectionFailedError,
        DEFAULT_CONNECTION_TIMEOUT,
        DEFAULT_CALL_TIMEOUT,
        DEFAULT_RECONNECT_INTERVAL,
    )


@unittest.skipIf(grpc is None, "grpc not installed")
class TestClientCreation(unittest.TestCase):
    """Test client creation and initialization."""

    def test_new_client_valid_addresses(self):
        """Test creating a client with valid addresses."""
        client = Client(["localhost:48000", "localhost:48001"])
        self.assertIsNotNone(client)
        self.assertEqual(len(client._addresses), 2)
        client.close()

    def test_new_client_single_address(self):
        """Test creating a client with a single address."""
        client = Client(["localhost:48000"])
        self.assertIsNotNone(client)
        self.assertEqual(len(client._addresses), 1)
        client.close()

    def test_new_client_no_addresses(self):
        """Test creating a client with no addresses raises error."""
        with self.assertRaises(NoAddressesError):
            Client([])

    def test_new_client_none_addresses(self):
        """Test creating a client with None addresses raises error."""
        with self.assertRaises(NoAddressesError):
            Client(None)

    def test_new_client_addresses_copied(self):
        """Test that client addresses are copied, not referenced."""
        original = ["localhost:48000", "localhost:48001"]
        client = Client(original)

        # Modify the original list
        original[0] = "modified"

        # Client should have the original value
        self.assertEqual(client._addresses[0], "localhost:48000")
        client.close()


@unittest.skipIf(grpc is None, "grpc not installed")
class TestClientOptions(unittest.TestCase):
    """Test client configuration options."""

    def test_new_client_with_options(self):
        """Test creating a client with custom options."""
        options = ClientOptions(
            connection_timeout=60.0,
            call_timeout=10.0,
            reconnect_interval=2.0,
        )
        client = Client(["localhost:48000"], options=options)

        self.assertEqual(client._options.connection_timeout, 60.0)
        self.assertEqual(client._options.call_timeout, 10.0)
        self.assertEqual(client._options.reconnect_interval, 2.0)
        client.close()

    def test_client_default_options(self):
        """Test that default options are applied."""
        client = Client(["localhost:48000"])

        self.assertEqual(client._options.connection_timeout, DEFAULT_CONNECTION_TIMEOUT)
        self.assertEqual(client._options.call_timeout, DEFAULT_CALL_TIMEOUT)
        self.assertEqual(client._options.reconnect_interval, DEFAULT_RECONNECT_INTERVAL)
        self.assertIsNotNone(client._logger)
        client.close()

    def test_client_with_custom_logger(self):
        """Test creating a client with a custom logger."""
        custom_logger = logging.getLogger("CustomLogger")
        options = ClientOptions(logger=custom_logger)
        client = Client(["localhost:48000"], options=options)

        self.assertEqual(client._logger, custom_logger)
        client.close()


@unittest.skipIf(grpc is None, "grpc not installed")
class TestClientInitialState(unittest.TestCase):
    """Test client initial state before connection."""

    def test_initial_state(self):
        """Test client initial state is correct."""
        client = Client(["localhost:48000"])

        self.assertFalse(client.is_connected())
        self.assertEqual(client.client_id, "")
        self.assertEqual(client.current_address, "")
        client.close()


@unittest.skipIf(grpc is None, "grpc not installed")
class TestClientClose(unittest.TestCase):
    """Test client close functionality."""

    def test_close_not_connected(self):
        """Test closing a client that was never connected."""
        client = Client(["localhost:48000"])
        client.close()  # Should not raise

    def test_double_close(self):
        """Test closing a client twice is safe."""
        client = Client(["localhost:48000"])
        client.close()
        client.close()  # Should not raise

    def test_operations_after_close(self):
        """Test that operations after close raise ClientClosedError."""
        client = Client(["localhost:48000"])
        client.close()

        with self.assertRaises(ClientClosedError):
            client.connect()

        with self.assertRaises(ClientClosedError):
            client.call_object("Type", "ID", "Method")

        with self.assertRaises(ClientClosedError):
            client.create_object("Type", "ID")

        with self.assertRaises(ClientClosedError):
            client.delete_object("ID")

        with self.assertRaises(ClientClosedError):
            client.call_object_any("Type", "ID", "Method")


@unittest.skipIf(grpc is None, "grpc not installed")
class TestClientNotConnected(unittest.TestCase):
    """Test client behavior when not connected."""

    def test_operations_when_not_connected(self):
        """Test that operations when not connected raise NotConnectedError."""
        client = Client(["localhost:48000"])

        with self.assertRaises(NotConnectedError):
            client.call_object("Type", "ID", "Method")

        with self.assertRaises(NotConnectedError):
            client.create_object("Type", "ID")

        with self.assertRaises(NotConnectedError):
            client.delete_object("ID")

        with self.assertRaises(NotConnectedError):
            client.call_object_any("Type", "ID", "Method")

        client.close()


@unittest.skipIf(grpc is None, "grpc not installed")
class TestClientConnectToNonExistentServer(unittest.TestCase):
    """Test connection to non-existent servers."""

    def test_connect_to_non_existent_server(self):
        """Test that connecting to non-existent servers fails."""
        client = Client(["localhost:1", "localhost:2"])

        with self.assertRaises(ConnectionFailedError):
            # Use a short timeout to avoid waiting too long
            client.connect(timeout=2.0)

        client.close()


@unittest.skipIf(grpc is None, "grpc not installed")
class TestClientCallbacks(unittest.TestCase):
    """Test callback configuration."""

    def test_with_callbacks(self):
        """Test that callbacks are properly stored."""
        connect_called = [False]
        disconnect_called = [False]
        message_called = [False]

        def on_connect(client_id):
            connect_called[0] = True

        def on_disconnect(err):
            disconnect_called[0] = True

        def on_message(msg):
            message_called[0] = True

        options = ClientOptions(
            on_connect=on_connect,
            on_disconnect=on_disconnect,
            on_message=on_message,
        )
        client = Client(["localhost:48000"], options=options)

        self.assertIsNotNone(client._options.on_connect)
        self.assertIsNotNone(client._options.on_disconnect)
        self.assertIsNotNone(client._options.on_message)

        client.close()


@unittest.skipIf(grpc is None, "grpc not installed")
class TestWaitForConnection(unittest.TestCase):
    """Test wait_for_connection functionality."""

    def test_wait_for_connection_timeout(self):
        """Test that wait_for_connection times out when not connected."""
        client = Client(["localhost:48000"])

        with self.assertRaises(TimeoutError):
            client.wait_for_connection(timeout=0.2)

        client.close()


@unittest.skipIf(grpc is None, "grpc not installed")
class TestReconnect(unittest.TestCase):
    """Test reconnection functionality."""

    def test_reconnect_when_closed(self):
        """Test that reconnect after close raises ClientClosedError."""
        client = Client(["localhost:48000"])
        client.close()

        with self.assertRaises(ClientClosedError):
            client.reconnect()

    def test_reconnect_to_non_existent_server(self):
        """Test that reconnecting to non-existent servers fails."""
        client = Client(["localhost:1"])

        with self.assertRaises(ConnectionFailedError):
            client.reconnect(timeout=2.0)

        client.close()


@unittest.skipIf(grpc is None, "grpc not installed")
class TestClientOptionsDefaults(unittest.TestCase):
    """Test ClientOptions default values."""

    def test_default_values(self):
        """Test that ClientOptions has correct default values."""
        options = ClientOptions()

        self.assertEqual(options.connection_timeout, DEFAULT_CONNECTION_TIMEOUT)
        self.assertEqual(options.call_timeout, DEFAULT_CALL_TIMEOUT)
        self.assertEqual(options.reconnect_interval, DEFAULT_RECONNECT_INTERVAL)
        self.assertEqual(options.grpc_options, [])
        self.assertIsNone(options.logger)
        self.assertIsNone(options.on_connect)
        self.assertIsNone(options.on_disconnect)
        self.assertIsNone(options.on_message)


@unittest.skipIf(grpc is None, "grpc not installed")
class TestExceptions(unittest.TestCase):
    """Test exception hierarchy."""

    def test_exception_hierarchy(self):
        """Test that all exceptions inherit from GoverseClientError."""
        self.assertTrue(issubclass(NoAddressesError, GoverseClientError))
        self.assertTrue(issubclass(NotConnectedError, GoverseClientError))
        self.assertTrue(issubclass(ClientClosedError, GoverseClientError))
        self.assertTrue(issubclass(ConnectionFailedError, GoverseClientError))


if __name__ == "__main__":
    unittest.main()
