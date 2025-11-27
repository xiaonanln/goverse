#!/usr/bin/env python3
"""
Counter CLI - A simple client to interact with the Counter sample.

Usage:
    python counter.py create <name>
    python counter.py get <name>
    python counter.py increment <name> <amount>
    python counter.py decrement <name> <amount>
    python counter.py reset <name>
    python counter.py delete <name>

Examples:
    python counter.py create visitors
    python counter.py increment visitors 5
    python counter.py get visitors
    python counter.py reset visitors
    python counter.py delete visitors

Prerequisites:
    pip install grpcio grpcio-tools protobuf
"""

import argparse
import base64
import json
import sys
import urllib.request
import urllib.error

from google.protobuf.any_pb2 import Any

from proto import counter_pb2

# Base URL for the Gate HTTP API
BASE_URL = "http://localhost:48000/api/v1/objects"


def http_post(url: str, data: dict = None) -> dict:
    """Make an HTTP POST request and return the JSON response."""
    body = json.dumps(data).encode("utf-8") if data else b"{}"
    req = urllib.request.Request(
        url,
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST"
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        error_body = e.read().decode("utf-8")
        try:
            error_json = json.loads(error_body)
            print(f"Error: {error_json.get('error', 'Unknown error')}", file=sys.stderr)
        except json.JSONDecodeError:
            print(f"Error: HTTP {e.code} - {error_body}", file=sys.stderr)
        sys.exit(1)
    except urllib.error.URLError as e:
        print(f"Error: Cannot connect to server - {e.reason}", file=sys.stderr)
        print("Make sure the Gate server is running on http://localhost:48000", file=sys.stderr)
        sys.exit(1)


def create_counter(name: str):
    """Create a new counter."""
    url = f"{BASE_URL}/create/Counter/Counter-{name}"
    response = http_post(url, {"request": ""})
    print(f"Created counter: {response.get('id', name)}")


def get_counter(name: str):
    """Get the current value of a counter."""
    request = counter_pb2.GetRequest()
    any_msg = Any()
    any_msg.Pack(request)
    encoded = base64.b64encode(any_msg.SerializeToString()).decode("utf-8")
    
    url = f"{BASE_URL}/call/Counter/Counter-{name}/Get"
    response = http_post(url, {"request": encoded})
    
    if "response" in response:
        resp_bytes = base64.b64decode(response["response"])
        any_resp = Any()
        any_resp.ParseFromString(resp_bytes)
        counter_resp = counter_pb2.CounterResponse()
        any_resp.Unpack(counter_resp)
        print(f"Counter {name} = {counter_resp.value}")
    else:
        print(f"Counter {name} = 0")


def increment_counter(name: str, amount: int):
    """Increment a counter by the specified amount."""
    request = counter_pb2.IncrementRequest(amount=amount)
    any_msg = Any()
    any_msg.Pack(request)
    encoded = base64.b64encode(any_msg.SerializeToString()).decode("utf-8")
    
    url = f"{BASE_URL}/call/Counter/Counter-{name}/Increment"
    response = http_post(url, {"request": encoded})
    
    if "response" in response:
        resp_bytes = base64.b64decode(response["response"])
        any_resp = Any()
        any_resp.ParseFromString(resp_bytes)
        counter_resp = counter_pb2.CounterResponse()
        any_resp.Unpack(counter_resp)
        print(f"Counter {name} = {counter_resp.value}")


def decrement_counter(name: str, amount: int):
    """Decrement a counter by the specified amount."""
    request = counter_pb2.DecrementRequest(amount=amount)
    any_msg = Any()
    any_msg.Pack(request)
    encoded = base64.b64encode(any_msg.SerializeToString()).decode("utf-8")
    
    url = f"{BASE_URL}/call/Counter/Counter-{name}/Decrement"
    response = http_post(url, {"request": encoded})
    
    if "response" in response:
        resp_bytes = base64.b64decode(response["response"])
        any_resp = Any()
        any_resp.ParseFromString(resp_bytes)
        counter_resp = counter_pb2.CounterResponse()
        any_resp.Unpack(counter_resp)
        print(f"Counter {name} = {counter_resp.value}")


def reset_counter(name: str):
    """Reset a counter to zero."""
    request = counter_pb2.ResetRequest()
    any_msg = Any()
    any_msg.Pack(request)
    encoded = base64.b64encode(any_msg.SerializeToString()).decode("utf-8")
    
    url = f"{BASE_URL}/call/Counter/Counter-{name}/Reset"
    response = http_post(url, {"request": encoded})
    
    if "response" in response:
        resp_bytes = base64.b64decode(response["response"])
        any_resp = Any()
        any_resp.ParseFromString(resp_bytes)
        counter_resp = counter_pb2.CounterResponse()
        any_resp.Unpack(counter_resp)
        print(f"Counter {name} = {counter_resp.value}")


def delete_counter(name: str):
    """Delete a counter."""
    url = f"{BASE_URL}/delete/Counter-{name}"
    http_post(url)
    print(f"Deleted counter: Counter-{name}")


def main():
    parser = argparse.ArgumentParser(
        description="Counter CLI - interact with the Counter sample",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python counter.py create visitors      Create a new counter named 'visitors'
  python counter.py get visitors         Get the current value
  python counter.py increment visitors 5 Increment by 5
  python counter.py decrement visitors 3 Decrement by 3
  python counter.py reset visitors       Reset to 0
  python counter.py delete visitors      Delete the counter
"""
    )
    
    subparsers = parser.add_subparsers(dest="command", help="Command to execute")
    
    # Create command
    create_parser = subparsers.add_parser("create", help="Create a new counter")
    create_parser.add_argument("name", help="Counter name")
    
    # Get command
    get_parser = subparsers.add_parser("get", help="Get counter value")
    get_parser.add_argument("name", help="Counter name")
    
    # Increment command
    incr_parser = subparsers.add_parser("increment", help="Increment counter")
    incr_parser.add_argument("name", help="Counter name")
    incr_parser.add_argument("amount", type=int, help="Amount to increment")
    
    # Decrement command
    decr_parser = subparsers.add_parser("decrement", help="Decrement counter")
    decr_parser.add_argument("name", help="Counter name")
    decr_parser.add_argument("amount", type=int, help="Amount to decrement")
    
    # Reset command
    reset_parser = subparsers.add_parser("reset", help="Reset counter to 0")
    reset_parser.add_argument("name", help="Counter name")
    
    # Delete command
    delete_parser = subparsers.add_parser("delete", help="Delete counter")
    delete_parser.add_argument("name", help="Counter name")
    
    args = parser.parse_args()
    
    if not args.command:
        parser.print_help()
        sys.exit(1)
    
    if args.command == "create":
        create_counter(args.name)
    elif args.command == "get":
        get_counter(args.name)
    elif args.command == "increment":
        increment_counter(args.name, args.amount)
    elif args.command == "decrement":
        decrement_counter(args.name, args.amount)
    elif args.command == "reset":
        reset_counter(args.name)
    elif args.command == "delete":
        delete_counter(args.name)


if __name__ == "__main__":
    main()
