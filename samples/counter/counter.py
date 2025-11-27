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
"""

import argparse
import base64
import json
import sys
import urllib.request
import urllib.error

# Base URL for the Gate HTTP API
BASE_URL = "http://localhost:48000/api/v1/objects"


def encode_any(type_url: str, value: bytes) -> bytes:
    """Encode a protobuf message as google.protobuf.Any."""
    # Manual Any encoding: type_url (field 1, string) + value (field 2, bytes)
    result = b""
    if type_url:
        # Field 1, wire type 2 (length-delimited) = 0x0a
        type_url_bytes = type_url.encode("utf-8")
        result += b"\x0a" + _encode_varint(len(type_url_bytes)) + type_url_bytes
    if value:
        # Field 2, wire type 2 (length-delimited) = 0x12
        result += b"\x12" + _encode_varint(len(value)) + value
    return result


def decode_any(data: bytes) -> tuple:
    """Decode a google.protobuf.Any message, returns (type_url, value)."""
    type_url = ""
    value = b""
    pos = 0
    while pos < len(data):
        if pos >= len(data):
            break
        tag = data[pos]
        pos += 1
        field_num = tag >> 3
        wire_type = tag & 0x07
        
        if wire_type == 2:  # Length-delimited
            length, bytes_read = _decode_varint(data[pos:])
            pos += bytes_read
            field_data = data[pos:pos + length]
            pos += length
            
            if field_num == 1:
                type_url = field_data.decode("utf-8")
            elif field_num == 2:
                value = field_data
    return type_url, value


def decode_counter_response(data: bytes) -> dict:
    """Decode a CounterResponse protobuf message."""
    name = ""
    value = 0
    pos = 0
    while pos < len(data):
        if pos >= len(data):
            break
        tag = data[pos]
        pos += 1
        field_num = tag >> 3
        wire_type = tag & 0x07
        
        if wire_type == 0:  # Varint
            val, bytes_read = _decode_varint(data[pos:])
            pos += bytes_read
            if field_num == 2:
                # Handle signed int32
                if val > 0x7FFFFFFF:
                    val -= 0x100000000
                value = val
        elif wire_type == 2:  # Length-delimited
            length, bytes_read = _decode_varint(data[pos:])
            pos += bytes_read
            field_data = data[pos:pos + length]
            pos += length
            if field_num == 1:
                name = field_data.decode("utf-8")
    return {"name": name, "value": value}


def _encode_varint(value: int) -> bytes:
    """Encode an integer as a varint."""
    result = []
    while value > 127:
        result.append((value & 0x7F) | 0x80)
        value >>= 7
    result.append(value)
    return bytes(result)


def _decode_varint(data: bytes) -> tuple:
    """Decode a varint, returns (value, bytes_consumed)."""
    result = 0
    shift = 0
    pos = 0
    while pos < len(data):
        byte = data[pos]
        result |= (byte & 0x7F) << shift
        pos += 1
        if (byte & 0x80) == 0:
            break
        shift += 7
    return result, pos


def encode_increment_request(amount: int) -> bytes:
    """Encode an IncrementRequest protobuf message."""
    # Field 1 (amount), wire type 0 (varint)
    if amount < 0:
        # Handle negative numbers as unsigned varint
        amount = amount & 0xFFFFFFFF
    return b"\x08" + _encode_varint(amount)


def encode_decrement_request(amount: int) -> bytes:
    """Encode a DecrementRequest protobuf message."""
    if amount < 0:
        amount = amount & 0xFFFFFFFF
    return b"\x08" + _encode_varint(amount)


def encode_get_request() -> bytes:
    """Encode a GetRequest protobuf message (empty)."""
    return b""


def encode_reset_request() -> bytes:
    """Encode a ResetRequest protobuf message (empty)."""
    return b""


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
    request_bytes = encode_get_request()
    any_bytes = encode_any("type.googleapis.com/counter.GetRequest", request_bytes)
    encoded = base64.b64encode(any_bytes).decode("utf-8")
    
    url = f"{BASE_URL}/call/Counter/Counter-{name}/Get"
    response = http_post(url, {"request": encoded})
    
    if "response" in response:
        resp_bytes = base64.b64decode(response["response"])
        _, value_bytes = decode_any(resp_bytes)
        counter = decode_counter_response(value_bytes)
        print(f"Counter {name} = {counter['value']}")
    else:
        print(f"Counter {name} = 0")


def increment_counter(name: str, amount: int):
    """Increment a counter by the specified amount."""
    request_bytes = encode_increment_request(amount)
    any_bytes = encode_any("type.googleapis.com/counter.IncrementRequest", request_bytes)
    encoded = base64.b64encode(any_bytes).decode("utf-8")
    
    url = f"{BASE_URL}/call/Counter/Counter-{name}/Increment"
    response = http_post(url, {"request": encoded})
    
    if "response" in response:
        resp_bytes = base64.b64decode(response["response"])
        _, value_bytes = decode_any(resp_bytes)
        counter = decode_counter_response(value_bytes)
        print(f"Counter {name} = {counter['value']}")


def decrement_counter(name: str, amount: int):
    """Decrement a counter by the specified amount."""
    request_bytes = encode_decrement_request(amount)
    any_bytes = encode_any("type.googleapis.com/counter.DecrementRequest", request_bytes)
    encoded = base64.b64encode(any_bytes).decode("utf-8")
    
    url = f"{BASE_URL}/call/Counter/Counter-{name}/Decrement"
    response = http_post(url, {"request": encoded})
    
    if "response" in response:
        resp_bytes = base64.b64decode(response["response"])
        _, value_bytes = decode_any(resp_bytes)
        counter = decode_counter_response(value_bytes)
        print(f"Counter {name} = {counter['value']}")


def reset_counter(name: str):
    """Reset a counter to zero."""
    request_bytes = encode_reset_request()
    any_bytes = encode_any("type.googleapis.com/counter.ResetRequest", request_bytes)
    encoded = base64.b64encode(any_bytes).decode("utf-8")
    
    url = f"{BASE_URL}/call/Counter/Counter-{name}/Reset"
    response = http_post(url, {"request": encoded})
    
    if "response" in response:
        resp_bytes = base64.b64decode(response["response"])
        _, value_bytes = decode_any(resp_bytes)
        counter = decode_counter_response(value_bytes)
        print(f"Counter {name} = {counter['value']}")


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
