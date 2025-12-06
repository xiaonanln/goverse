# Copyright (c) 2024 Goverse Authors. All rights reserved.
# Licensed under the Apache-2.0 License.
"""
Goverse Python Client Library

A Python client library for connecting to Goverse gate servers.
It handles connection management, automatic failover between multiple gate addresses,
and provides a user-friendly API for interacting with Goverse services.

Example usage:
    from goverseclient_python import Client

    # Create client with gate addresses
    client = Client(["localhost:48000"])

    # Connect to gate
    client.connect()

    # Create an object
    object_id = client.create_object("Counter", "Counter-myid")

    # Call a method on the object
    response = client.call_object("Counter", "Counter-myid", "Increment", request)

    # Delete the object
    client.delete_object("Counter-myid")

    # Close the client
    client.close()
"""

from .client import (
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

__version__ = "0.1.0"

__all__ = [
    "Client",
    "ClientOptions",
    "GoverseClientError",
    "NoAddressesError",
    "NotConnectedError",
    "ClientClosedError",
    "ConnectionFailedError",
    "DEFAULT_CONNECTION_TIMEOUT",
    "DEFAULT_CALL_TIMEOUT",
    "DEFAULT_RECONNECT_INTERVAL",
]
