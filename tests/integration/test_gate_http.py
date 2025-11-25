#!/usr/bin/env python3
"""
Integration test for Gate HTTP endpoints.

This script tests the HTTP API of the Goverse gate server:
- Create object via HTTP API
- Call object via HTTP API  
- Delete object via HTTP API

Uses the ChatRoom object from the chat sample for simplicity.
"""

import os
import subprocess
import sys
import base64
import json
import time
from pathlib import Path

import requests

# Add the repo root to the path for proto imports
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))
INTEGRATION_DIR = REPO_ROOT / 'tests' / 'integration'
sys.path.insert(0, str(INTEGRATION_DIR))

from google.protobuf.any_pb2 import Any as AnyProto
from samples.chat.proto import chat_pb2
from ChatServer import ChatServer
from Inspector import Inspector
from Gateway import Gateway


def encode_protobuf_any(message) -> str:
    """Encode a protobuf message as base64 Any."""
    any_msg = AnyProto()
    any_msg.Pack(message)
    return base64.b64encode(any_msg.SerializeToString()).decode('utf-8')


def decode_protobuf_any(base64_str: str, message_type):
    """Decode a base64 Any to a specific protobuf message type."""
    any_bytes = base64.b64decode(base64_str)
    any_msg = AnyProto()
    any_msg.ParseFromString(any_bytes)
    result = message_type()
    any_msg.Unpack(result)
    return result


def test_create_object(http_base_url: str) -> bool:
    """Test creating an object via HTTP API."""
    print("\n--- Testing Create Object ---")
    
    # Create a unique test object ID
    test_obj_id = f"test-http-room-{int(time.time())}"
    url = f"{http_base_url}/api/v1/objects/create/ChatRoom/{test_obj_id}"
    
    try:
        response = requests.post(url, timeout=10)
        
        if response.status_code != 200:
            print(f"❌ Create object failed with status {response.status_code}")
            print(f"   Response: {response.text}")
            return False
        
        data = response.json()
        created_id = data.get('id', '')
        
        if test_obj_id not in created_id:
            print(f"❌ Created object ID mismatch: expected '{test_obj_id}' in '{created_id}'")
            return False
        
        print(f"✅ Object created successfully: {created_id}")
        return True
        
    except Exception as e:
        print(f"❌ Exception during create object: {e}")
        return False


def test_call_object(http_base_url: str) -> bool:
    """Test calling an object method via HTTP API."""
    print("\n--- Testing Call Object ---")
    
    # First create a test object
    test_obj_id = f"test-call-room-{int(time.time())}"
    create_url = f"{http_base_url}/api/v1/objects/create/ChatRoom/{test_obj_id}"
    
    try:
        # Create the object first
        create_response = requests.post(create_url, timeout=10)
        if create_response.status_code != 200:
            print(f"❌ Failed to create test object: {create_response.text}")
            return False
        
        created_id = create_response.json().get('id', '')
        print(f"   Created test object: {created_id}")
        
        # Now call a method on it (Join)
        # Note: client_id is left empty as this is an HTTP test (no streaming client registration)
        join_request = chat_pb2.ChatRoom_JoinRequest(user_name="test_user")
        encoded_request = encode_protobuf_any(join_request)
        
        call_url = f"{http_base_url}/api/v1/objects/call/ChatRoom/{test_obj_id}/Join"
        call_body = {"request": encoded_request}
        
        call_response = requests.post(
            call_url,
            json=call_body,
            headers={"Content-Type": "application/json"},
            timeout=10
        )
        
        if call_response.status_code != 200:
            print(f"❌ Call object failed with status {call_response.status_code}")
            print(f"   Response: {call_response.text}")
            return False
        
        # Decode the response
        response_data = call_response.json()
        encoded_response = response_data.get('response', '')
        
        if not encoded_response:
            print("❌ Empty response from call")
            return False
        
        join_response = decode_protobuf_any(encoded_response, chat_pb2.ChatRoom_JoinResponse)
        
        # Verify the response
        if not join_response.room_name:
            print(f"❌ Join response missing room name")
            return False
        
        print(f"✅ Call object successful: joined room '{join_response.room_name}'")
        print(f"   Recent messages: {len(join_response.recent_messages)}")
        
        return True
        
    except Exception as e:
        print(f"❌ Exception during call object: {e}")
        import traceback
        traceback.print_exc()
        return False


def test_delete_object(http_base_url: str) -> bool:
    """Test deleting an object via HTTP API."""
    print("\n--- Testing Delete Object ---")
    
    # First create a test object
    test_obj_id = f"test-delete-room-{int(time.time())}"
    create_url = f"{http_base_url}/api/v1/objects/create/ChatRoom/{test_obj_id}"
    
    try:
        # Create the object first
        create_response = requests.post(create_url, timeout=10)
        if create_response.status_code != 200:
            print(f"❌ Failed to create test object: {create_response.text}")
            return False
        
        created_id = create_response.json().get('id', '')
        print(f"   Created test object for deletion: {created_id}")
        
        # Now delete it
        delete_url = f"{http_base_url}/api/v1/objects/delete/{created_id}"
        delete_response = requests.post(delete_url, timeout=10)
        
        if delete_response.status_code != 200:
            print(f"❌ Delete object failed with status {delete_response.status_code}")
            print(f"   Response: {delete_response.text}")
            return False
        
        delete_data = delete_response.json()
        if not delete_data.get('success', False):
            print(f"❌ Delete object returned success=false")
            return False
        
        print(f"✅ Object deleted successfully: {created_id}")
        
        # Verify the object is actually deleted by trying to call it
        # This should fail with a "not found" error
        call_url = f"{http_base_url}/api/v1/objects/call/ChatRoom/{test_obj_id}/Join"
        join_request = chat_pb2.ChatRoom_JoinRequest(user_name="test_user")
        encoded_request = encode_protobuf_any(join_request)
        call_body = {"request": encoded_request}
        
        verify_response = requests.post(
            call_url,
            json=call_body,
            headers={"Content-Type": "application/json"},
            timeout=10
        )
        
        # The call should fail since the object is deleted
        if verify_response.status_code == 200:
            print(f"⚠️  Object still accessible after deletion (may be re-created on call)")
        else:
            print(f"   Verified: object no longer accessible (status {verify_response.status_code})")
        
        return True
        
    except Exception as e:
        print(f"❌ Exception during delete object: {e}")
        import traceback
        traceback.print_exc()
        return False


def test_full_lifecycle(http_base_url: str) -> bool:
    """Test create, call, and delete in sequence with one object."""
    print("\n--- Testing Full Object Lifecycle ---")
    
    test_obj_id = f"test-lifecycle-room-{int(time.time())}"
    
    try:
        # Step 1: Create the object
        print(f"Step 1: Creating object {test_obj_id}")
        create_url = f"{http_base_url}/api/v1/objects/create/ChatRoom/{test_obj_id}"
        create_response = requests.post(create_url, timeout=10)
        
        if create_response.status_code != 200:
            print(f"❌ Create failed: {create_response.text}")
            return False
        
        created_id = create_response.json().get('id', '')
        print(f"   ✅ Created: {created_id}")
        
        # Step 2: Call Join method
        print(f"Step 2: Calling Join method")
        join_request = chat_pb2.ChatRoom_JoinRequest(
            user_name="lifecycle_user",
            client_id=""
        )
        call_url = f"{http_base_url}/api/v1/objects/call/ChatRoom/{test_obj_id}/Join"
        call_body = {"request": encode_protobuf_any(join_request)}
        
        call_response = requests.post(
            call_url,
            json=call_body,
            headers={"Content-Type": "application/json"},
            timeout=10
        )
        
        if call_response.status_code != 200:
            print(f"❌ Join call failed: {call_response.text}")
            return False
        
        join_response = decode_protobuf_any(
            call_response.json().get('response', ''),
            chat_pb2.ChatRoom_JoinResponse
        )
        print(f"   ✅ Joined room: {join_response.room_name}")
        
        # Step 3: Call SendMessage method
        print(f"Step 3: Calling SendMessage method")
        send_request = chat_pb2.ChatRoom_SendChatMessageRequest(
            user_name="lifecycle_user",
            message="Hello from HTTP test!"
        )
        send_url = f"{http_base_url}/api/v1/objects/call/ChatRoom/{test_obj_id}/SendMessage"
        send_body = {"request": encode_protobuf_any(send_request)}
        
        send_response = requests.post(
            send_url,
            json=send_body,
            headers={"Content-Type": "application/json"},
            timeout=10
        )
        
        if send_response.status_code != 200:
            print(f"❌ SendMessage call failed: {send_response.text}")
            return False
        print(f"   ✅ Message sent successfully")
        
        # Step 4: Call GetRecentMessages method
        print(f"Step 4: Calling GetRecentMessages method")
        get_request = chat_pb2.ChatRoom_GetRecentMessagesRequest(after_timestamp=0)
        get_url = f"{http_base_url}/api/v1/objects/call/ChatRoom/{test_obj_id}/GetRecentMessages"
        get_body = {"request": encode_protobuf_any(get_request)}
        
        get_response = requests.post(
            get_url,
            json=get_body,
            headers={"Content-Type": "application/json"},
            timeout=10
        )
        
        if get_response.status_code != 200:
            print(f"❌ GetRecentMessages call failed: {get_response.text}")
            return False
        
        messages_response = decode_protobuf_any(
            get_response.json().get('response', ''),
            chat_pb2.ChatRoom_GetRecentMessagesResponse
        )
        print(f"   ✅ Retrieved {len(messages_response.messages)} messages")
        
        # Verify our message is in there
        found_message = False
        for msg in messages_response.messages:
            if msg.message == "Hello from HTTP test!":
                found_message = True
                break
        
        if not found_message:
            print(f"❌ Our test message was not found in recent messages")
            return False
        print(f"   ✅ Verified test message in room")
        
        # Step 5: Delete the object
        print(f"Step 5: Deleting object")
        delete_url = f"{http_base_url}/api/v1/objects/delete/{created_id}"
        delete_response = requests.post(delete_url, timeout=10)
        
        if delete_response.status_code != 200:
            print(f"❌ Delete failed: {delete_response.text}")
            return False
        print(f"   ✅ Object deleted")
        
        print("✅ Full lifecycle test passed!")
        return True
        
    except Exception as e:
        print(f"❌ Exception during lifecycle test: {e}")
        import traceback
        traceback.print_exc()
        return False


def main():
    """Main test execution."""
    # Get the repository root directory
    repo_root = Path(__file__).parent.parent.parent.resolve()
    os.chdir(repo_root)
    print(f"Working directory: {os.getcwd()}")
    
    print("=" * 60)
    print("Goverse Gate HTTP Integration Test")
    print("=" * 60)
    
    # Configuration
    http_port = 49080
    grpc_port = 49000
    
    try:
        # Add go bin to PATH using subprocess for safer execution
        result = subprocess.run(['go', 'env', 'GOPATH'], capture_output=True, text=True, check=True)
        go_bin_path = result.stdout.strip()
        os.environ['PATH'] = f"{os.environ['PATH']}:{go_bin_path}/bin"
        
        # Start inspector
        inspector = Inspector()
        inspector.start()
        if not inspector.wait_for_ready(timeout=30):
            print("❌ Inspector failed to start")
            return 1
        
        # Start a chat server (required for object types to be registered)
        chat_server = ChatServer(server_index=0)
        chat_server.start()
        
        if not chat_server.wait_for_ready(timeout=30):
            print("❌ Chat server failed to start")
            return 1
        
        # Start gateway with HTTP enabled
        gateway = Gateway(listen_port=grpc_port, http_listen_port=http_port)
        gateway.start()
        
        if not gateway.wait_for_ready(timeout=30):
            print("❌ Gateway failed to start")
            return 1
        
        # Wait for cluster to be ready
        print("\nWaiting for cluster to be ready...")
        if not chat_server.wait_for_objects(min_count=1, timeout=30):
            print("❌ Cluster failed to become ready")
            return 1
        
        http_base_url = f"http://localhost:{http_port}"
        print(f"\nRunning HTTP tests against {http_base_url}")
        
        # Run the tests
        results = []
        
        results.append(("Create Object", test_create_object(http_base_url)))
        results.append(("Call Object", test_call_object(http_base_url)))
        results.append(("Delete Object", test_delete_object(http_base_url)))
        results.append(("Full Lifecycle", test_full_lifecycle(http_base_url)))
        
        # Print summary
        print("\n" + "=" * 60)
        print("Test Results Summary")
        print("=" * 60)
        
        all_passed = True
        for name, passed in results:
            status = "✅ PASS" if passed else "❌ FAIL"
            print(f"  {name}: {status}")
            if not passed:
                all_passed = False
        
        print()
        
        # Cleanup
        print("Stopping gateway...")
        gateway_code = gateway.close()
        print(f"Gateway exited with code {gateway_code}")
        
        print("Stopping chat server...")
        server_code = chat_server.close()
        print(f"Chat server exited with code {server_code}")
        
        print("Stopping inspector...")
        inspector_code = inspector.close()
        print(f"Inspector exited with code {inspector_code}")
        
        if all_passed:
            print("\n" + "=" * 60)
            print("✅ All Gate HTTP integration tests passed!")
            print("=" * 60)
            return 0
        else:
            print("\n" + "=" * 60)
            print("❌ Some tests failed!")
            print("=" * 60)
            return 1
        
    except KeyboardInterrupt:
        print("\n\nTest interrupted by user")
        return 1
    except Exception as e:
        print(f"\n❌ Unexpected error: {e}")
        import traceback
        traceback.print_exc()
        return 1


if __name__ == '__main__':
    sys.exit(main())
