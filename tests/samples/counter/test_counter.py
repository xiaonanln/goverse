#!/usr/bin/env python3
"""
Test script for Goverse Counter sample.

This script:
1. Builds and runs the Counter server
2. Builds and runs the Gate server
3. Tests counter operations via HTTP REST API
4. Stops the servers and verifies they quit properly
"""

import base64
import json
import os
import signal
import socket
import subprocess
import sys
import time
import uuid
from pathlib import Path

# Repo root (tests/samples/counter/test_counter.py -> repo root)
REPO_ROOT = Path(__file__).parent.parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT / 'tests' / 'samples'))

from BinaryHelper import BinaryHelper

# Import protobuf libraries
try:
    from google.protobuf import any_pb2
except ImportError:
    print("ERROR: protobuf library not found. Install with: pip install protobuf")
    sys.exit(1)

# Import Counter protobuf messages
PROTO_DIR = REPO_ROOT / 'samples' / 'counter' / 'proto'
sys.path.insert(0, str(PROTO_DIR))

try:
    import counter_pb2
except ImportError as e:
    print(f"ERROR: Failed to import counter_pb2: {e}")
    print("Proto files should be compiled beforehand via ./samples/counter/compile-proto.sh")
    sys.exit(1)

# Server ports
NODE_PORT = 47000
GATE_PORT = 48000
HTTP_PORT = 48000  # Gate HTTP and gRPC on same port

# Binary paths
COUNTER_BINARY = '/tmp/counter_server'
GATE_BINARY = '/tmp/gate_server'


def generate_counter_name():
    """Generate a unique counter name for this test."""
    return f"test-counter-{uuid.uuid4().hex[:8]}"


def build_counter_server():
    """Build the Counter server binary."""
    return BinaryHelper.build_binary(
        './samples/counter/server/',
        COUNTER_BINARY,
        'Counter server'
    )


def build_gate_server():
    """Build the Gate server binary."""
    return BinaryHelper.build_binary(
        './cmd/gate/',
        GATE_BINARY,
        'Gate server'
    )


def check_port(port, timeout=30):
    """Wait for a port to be available."""
    start_time = time.time()
    while time.time() - start_time < timeout:
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(1)
            result = sock.connect_ex(('localhost', port))
            sock.close()
            if result == 0:
                return True
        except Exception:
            pass
        time.sleep(1)
    return False


def create_get_request():
    """Create a GetRequest protobuf wrapped in Any, base64 encoded."""
    req = counter_pb2.GetRequest()
    any_msg = any_pb2.Any()
    any_msg.Pack(req)
    return base64.b64encode(any_msg.SerializeToString()).decode('ascii')


def create_increment_request(amount):
    """Create an IncrementRequest protobuf wrapped in Any, base64 encoded."""
    req = counter_pb2.IncrementRequest(amount=amount)
    any_msg = any_pb2.Any()
    any_msg.Pack(req)
    return base64.b64encode(any_msg.SerializeToString()).decode('ascii')


def create_decrement_request(amount):
    """Create a DecrementRequest protobuf wrapped in Any, base64 encoded."""
    req = counter_pb2.DecrementRequest(amount=amount)
    any_msg = any_pb2.Any()
    any_msg.Pack(req)
    return base64.b64encode(any_msg.SerializeToString()).decode('ascii')


def create_reset_request():
    """Create a ResetRequest protobuf wrapped in Any, base64 encoded."""
    req = counter_pb2.ResetRequest()
    any_msg = any_pb2.Any()
    any_msg.Pack(req)
    return base64.b64encode(any_msg.SerializeToString()).decode('ascii')


def parse_counter_response(response_base64):
    """Parse the CounterResponse from a base64-encoded Any response.

    Returns a dict with name and value.
    """
    response_bytes = base64.b64decode(response_base64)
    any_msg = any_pb2.Any()
    any_msg.ParseFromString(response_bytes)

    counter_resp = counter_pb2.CounterResponse()
    any_msg.Unpack(counter_resp)

    return {
        'name': counter_resp.name,
        'value': counter_resp.value
    }


def http_post(url, data=None, timeout=30):
    """Make an HTTP POST request and return the JSON response."""
    import urllib.request
    import urllib.error

    body = json.dumps(data).encode('utf-8') if data else b'{}'
    req = urllib.request.Request(
        url,
        data=body,
        headers={'Content-Type': 'application/json'},
        method='POST'
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            return json.loads(response.read().decode('utf-8'))
    except urllib.error.HTTPError as e:
        error_body = e.read().decode('utf-8')
        raise RuntimeError(f"HTTP {e.code}: {error_body}")


def wait_for_cluster_ready(counter_name, timeout=90):
    """Wait for the cluster to be ready by polling create until it succeeds.

    Returns True on success, raises RuntimeError on timeout.
    """
    start_time = time.time()
    last_error = None

    while time.time() - start_time < timeout:
        try:
            url = f'http://localhost:{HTTP_PORT}/api/v1/objects/create/Counter/Counter-{counter_name}'
            http_post(url, {'request': ''})
            return True
        except RuntimeError as e:
            last_error = e
            error_str = str(e)
            # Check if the error is about cluster not ready (shard not assigned, not claimed, etc.)
            if ('no node assigned to shard' in error_str or
                'no current node' in error_str or
                'CALL_FAILED' in error_str or
                'CREATE_FAILED' in error_str):
                print(f"   Cluster not ready yet, retrying... ({error_str[:100]})")
                time.sleep(3)
                continue
            # For other errors, raise immediately
            raise
        except Exception as e:
            last_error = e
            print(f"   Error: {e}, retrying...")
            time.sleep(3)
            continue

    raise RuntimeError(f"Timeout waiting for cluster to be ready. Last error: {last_error}")


def call_get(counter_name):
    """Call the Get method via HTTP REST API."""
    url = f'http://localhost:{HTTP_PORT}/api/v1/objects/call/Counter/Counter-{counter_name}/Get'
    response = http_post(url, {'request': create_get_request()})
    return parse_counter_response(response['response'])


def call_increment(counter_name, amount):
    """Call the Increment method via HTTP REST API."""
    url = f'http://localhost:{HTTP_PORT}/api/v1/objects/call/Counter/Counter-{counter_name}/Increment'
    response = http_post(url, {'request': create_increment_request(amount)})
    return parse_counter_response(response['response'])


def call_decrement(counter_name, amount):
    """Call the Decrement method via HTTP REST API."""
    url = f'http://localhost:{HTTP_PORT}/api/v1/objects/call/Counter/Counter-{counter_name}/Decrement'
    response = http_post(url, {'request': create_decrement_request(amount)})
    return parse_counter_response(response['response'])


def call_reset(counter_name):
    """Call the Reset method via HTTP REST API."""
    url = f'http://localhost:{HTTP_PORT}/api/v1/objects/call/Counter/Counter-{counter_name}/Reset'
    response = http_post(url, {'request': create_reset_request()})
    return parse_counter_response(response['response'])


def call_delete(counter_name):
    """Call the Delete method via HTTP REST API."""
    url = f'http://localhost:{HTTP_PORT}/api/v1/objects/delete/Counter-{counter_name}'
    http_post(url)
    return True


def start_counter_server():
    """Start the Counter server process."""
    print("Starting Counter server...")

    process = subprocess.Popen(
        [COUNTER_BINARY, '-listen', f'localhost:{NODE_PORT}', '-advertise', f'localhost:{NODE_PORT}'],
        stdout=None,
        stderr=None
    )
    print(f"✅ Counter server started with PID: {process.pid}")
    return process


def start_gate_server():
    """Start the Gate server process."""
    print("Starting Gate server...")

    process = subprocess.Popen(
        [GATE_BINARY, '--http-listen', f':{HTTP_PORT}'],
        stdout=None,
        stderr=None
    )
    print(f"✅ Gate server started with PID: {process.pid}")
    return process


def stop_server(process, name="Server"):
    """Stop a server gracefully and return exit code."""
    if process is None:
        return -1

    if process.poll() is not None:
        return process.returncode if process.returncode is not None else -1

    print(f"Gracefully stopping {name} (PID: {process.pid})...")

    try:
        process.send_signal(signal.SIGINT)
        process.wait(timeout=10)
        return process.returncode if process.returncode is not None else -1
    except subprocess.TimeoutExpired:
        pass
    except Exception:
        pass

    try:
        process.terminate()
        process.wait(timeout=5)
        return process.returncode if process.returncode is not None else -1
    except subprocess.TimeoutExpired:
        pass
    except Exception:
        pass

    try:
        process.kill()
        process.wait()
    except Exception:
        pass

    return process.returncode if process.returncode is not None else -1


def run_counter_tests(counter_name):
    """Run all counter tests and return True if all pass."""
    print("\n--- Testing Counter Operations ---")
    success = True

    # Test 1: Get initial value (should be 0)
    print("\n  Test 1: Get initial value")
    try:
        result = call_get(counter_name)
        if result['value'] == 0:
            print(f"    ✅ Initial value is 0")
        else:
            print(f"    ❌ Expected 0, got {result['value']}")
            success = False
    except Exception as e:
        print(f"    ❌ Failed: {e}")
        success = False

    # Test 2: Increment by 5
    print("\n  Test 2: Increment by 5")
    try:
        result = call_increment(counter_name, 5)
        if result['value'] == 5:
            print(f"    ✅ Value after increment: {result['value']}")
        else:
            print(f"    ❌ Expected 5, got {result['value']}")
            success = False
    except Exception as e:
        print(f"    ❌ Failed: {e}")
        success = False

    # Test 3: Increment by 10
    print("\n  Test 3: Increment by 10")
    try:
        result = call_increment(counter_name, 10)
        if result['value'] == 15:
            print(f"    ✅ Value after increment: {result['value']}")
        else:
            print(f"    ❌ Expected 15, got {result['value']}")
            success = False
    except Exception as e:
        print(f"    ❌ Failed: {e}")
        success = False

    # Test 4: Get current value
    print("\n  Test 4: Get current value")
    try:
        result = call_get(counter_name)
        if result['value'] == 15:
            print(f"    ✅ Current value: {result['value']}")
        else:
            print(f"    ❌ Expected 15, got {result['value']}")
            success = False
    except Exception as e:
        print(f"    ❌ Failed: {e}")
        success = False

    # Test 5: Decrement by 3
    print("\n  Test 5: Decrement by 3")
    try:
        result = call_decrement(counter_name, 3)
        if result['value'] == 12:
            print(f"    ✅ Value after decrement: {result['value']}")
        else:
            print(f"    ❌ Expected 12, got {result['value']}")
            success = False
    except Exception as e:
        print(f"    ❌ Failed: {e}")
        success = False

    # Test 6: Reset
    print("\n  Test 6: Reset counter")
    try:
        result = call_reset(counter_name)
        if result['value'] == 0:
            print(f"    ✅ Value after reset: {result['value']}")
        else:
            print(f"    ❌ Expected 0, got {result['value']}")
            success = False
    except Exception as e:
        print(f"    ❌ Failed: {e}")
        success = False

    # Test 7: Delete counter
    print("\n  Test 7: Delete counter")
    try:
        call_delete(counter_name)
        print("    ✅ Counter deleted successfully")
    except Exception as e:
        print(f"    ❌ Failed: {e}")
        success = False

    return success


def main():
    """Main test execution."""
    # Change to repo root
    os.chdir(REPO_ROOT)
    print(f"Working directory: {os.getcwd()}")

    print("=" * 60)
    print("Goverse Counter Sample Test")
    print("=" * 60)
    print()

    counter_process = None
    gate_process = None

    try:
        # Add go bin to PATH
        go_bin_path = subprocess.run(
            ['go', 'env', 'GOPATH'],
            capture_output=True, text=True, check=True
        ).stdout.strip()
        os.environ['PATH'] = f"{os.environ['PATH']}:{go_bin_path}/bin"

        # Step 1: Build the servers
        print("\n--- Step 1: Building servers ---")
        if not build_counter_server():
            print("❌ Failed to build Counter server")
            return 1

        if not build_gate_server():
            print("❌ Failed to build Gate server")
            return 1

        # Step 2: Start the Counter server
        print("\n--- Step 2: Starting Counter server ---")
        counter_process = start_counter_server()

        # Give counter server time to register with etcd and claim shards
        time.sleep(5)

        # Step 3: Start the Gate server
        print("\n--- Step 3: Starting Gate server ---")
        gate_process = start_gate_server()

        # Step 4: Wait for Gate server to be ready
        print("\n--- Step 4: Waiting for Gate server to be ready ---")
        print(f"Checking HTTP port {HTTP_PORT}...")
        if not check_port(HTTP_PORT, timeout=30):
            print(f"❌ Gate server HTTP port {HTTP_PORT} not available after 30 seconds")
            return 1
        print(f"✅ HTTP port {HTTP_PORT} is ready")

        # Step 5: Wait for cluster to be ready and create a counter
        print("\n--- Step 5: Waiting for cluster to be ready ---")
        counter_name = generate_counter_name()
        print(f"Counter name: {counter_name}")

        try:
            wait_for_cluster_ready(counter_name, timeout=90)
            print("✅ Cluster is ready and counter created!")
        except Exception as e:
            print(f"❌ Failed to create counter: {e}")
            return 1

        # Step 6: Run counter tests
        print("\n--- Step 6: Running counter tests ---")
        if not run_counter_tests(counter_name):
            print("\n❌ Some counter tests failed!")
            return 1

        print("\n✅ All counter tests passed!")

        # Step 7: Stop the servers
        print("\n--- Step 7: Stopping servers ---")

        gate_exit = stop_server(gate_process, "Gate server")
        gate_process = None
        print(f"Gate server exited with code {gate_exit}")

        counter_exit = stop_server(counter_process, "Counter server")
        counter_process = None
        print(f"Counter server exited with code {counter_exit}")

        if gate_exit != 0:
            print(f"❌ Gate server exited with non-zero status: {gate_exit}")
            return 1

        if counter_exit != 0:
            print(f"❌ Counter server exited with non-zero status: {counter_exit}")
            return 1

        print("\n" + "=" * 60)
        print("✅ All Counter sample tests passed!")
        print("=" * 60)
        return 0

    except KeyboardInterrupt:
        print("\n\nTest interrupted by user")
        return 1
    except Exception as e:
        print(f"\n❌ Unexpected error: {e}")
        import traceback
        traceback.print_exc()
        return 1
    finally:
        # Cleanup: stop servers if still running
        if gate_process is not None:
            stop_server(gate_process, "Gate server")
        if counter_process is not None:
            stop_server(counter_process, "Counter server")


if __name__ == '__main__':
    sys.exit(main())
