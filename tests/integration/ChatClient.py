#!/usr/bin/env python3
"""ChatClient helper using gRPC client for managing chat client interactions."""
import grpc
import threading
import time
from pathlib import Path
from google.protobuf import any_pb2

# Import generated proto files
import sys
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))

from client.proto import client_pb2, client_pb2_grpc
from samples.chat.proto import chat_pb2


class ChatClient:
    """Manages the Goverse chat client using gRPC."""
    
    def __init__(self):
        """Initialize the chat client."""
        self.name = "Chat Client"
        self.channel = None
        self.stub = None
        self.client_id = None
        self.stream = None
        self.room_name = None
        self.user_name = None
        self.last_msg_timestamp = 0
        self.output_buffer = []
        self.output_lock = threading.Lock()
        self.stream_thread = None
        self.running = False
    
    def start_interactive(self, server_port, username='testuser'):
        """Start the chat client as an interactive background process.
        
        Args:
            server_port: The server port to connect to
            username: Username for the chat client (default: testuser)
            
        Returns:
            True if client started successfully, False otherwise
        """
        try:
            # Connect to the server
            server_address = f'localhost:{server_port}'
            self.channel = grpc.insecure_channel(server_address)
            self.stub = client_pb2_grpc.ClientServiceStub(self.channel)
            self.user_name = username
            
            # Register the client (this will block until connection is established)
            self.stream = self.stub.Register(client_pb2.Empty())
            
            # Read the first message which should be RegisterResponse
            first_msg = next(self.stream)
            reg_response = client_pb2.RegisterResponse()
            first_msg.Unpack(reg_response)
            self.client_id = reg_response.client_id
            
            # Start background thread to listen for pushed messages
            self.running = True
            self.stream_thread = threading.Thread(target=self._listen_for_messages, daemon=True)
            self.stream_thread.start()
            
            return True
            
        except Exception as e:
            print(f"Error starting chat client: {e}")
            import traceback
            traceback.print_exc()
            return False
    
    def _listen_for_messages(self):
        """Background thread to listen for pushed messages from the server."""
        try:
            while self.running:
                try:
                    msg_any = next(self.stream)
                    
                    # Try to unpack as NewMessageNotification
                    notification = chat_pb2.Client_NewMessageNotification()
                    if msg_any.Unpack(notification):
                        chat_msg = notification.message
                        timestamp_str = time.strftime("%H:%M:%S", time.localtime(chat_msg.timestamp))
                        output_line = f"[{timestamp_str}] {chat_msg.user_name}: {chat_msg.message}"
                        
                        with self.output_lock:
                            self.output_buffer.append(output_line)
                        
                        # Update last message timestamp
                        if chat_msg.timestamp > self.last_msg_timestamp:
                            self.last_msg_timestamp = chat_msg.timestamp
                            
                except StopIteration:
                    break
                except Exception as e:
                    if self.running:
                        print(f"Error in message listener: {e}")
                    break
        except Exception as e:
            if self.running:
                print(f"Fatal error in message listener: {e}")
    
    def _call(self, method, request_proto):
        """Call a method on the client object.
        
        Args:
            method: Method name to call
            request_proto: Request protobuf message
            
        Returns:
            Response protobuf message
        """
        # Pack request into Any
        request_any = any_pb2.Any()
        request_any.Pack(request_proto)
        
        # Call the RPC
        call_request = client_pb2.CallRequest(
            client_id=self.client_id,
            method=method,
            request=request_any
        )
        
        response = self.stub.Call(call_request)
        return response.response
    
    def list_chatrooms(self):
        """List all available chatrooms.
        
        Returns:
            List of chatroom names
        """
        request = chat_pb2.Client_ListChatRoomsRequest()
        response_any = self._call("ListChatRooms", request)
        
        response = chat_pb2.Client_ListChatRoomsResponse()
        response_any.Unpack(response)
        
        with self.output_lock:
            self.output_buffer.append("Available Chatrooms:")
            for room in response.chat_rooms:
                self.output_buffer.append(f" - {room}")
        
        return list(response.chat_rooms)
    
    def join_chatroom(self, room_name):
        """Join a chatroom.
        
        Args:
            room_name: Name of the chatroom to join
            
        Returns:
            List of recent messages in the chatroom
        """
        request = chat_pb2.Client_JoinChatRoomRequest(
            room_name=room_name,
            user_name=self.user_name
        )
        response_any = self._call("Join", request)
        
        response = chat_pb2.Client_JoinChatRoomResponse()
        response_any.Unpack(response)
        
        self.room_name = room_name
        self.last_msg_timestamp = 0
        
        with self.output_lock:
            self.output_buffer.append(f"Joined chatroom {response.room_name}")
            for msg in response.recent_messages:
                timestamp_str = time.strftime("%H:%M:%S", time.localtime(msg.timestamp))
                self.output_buffer.append(f"[{timestamp_str}] {msg.user_name}: {msg.message}")
                if msg.timestamp > self.last_msg_timestamp:
                    self.last_msg_timestamp = msg.timestamp
        
        return list(response.recent_messages)
    
    def send_message(self, message):
        """Send a message to the current chatroom.
        
        Args:
            message: Message text to send
            
        Raises:
            RuntimeError: If not currently in a chatroom
        """
        if not self.room_name:
            error_msg = "Error: You must join a chatroom first"
            with self.output_lock:
                self.output_buffer.append(error_msg)
            raise RuntimeError(error_msg)
        
        request = chat_pb2.Client_SendChatMessageRequest(
            user_name=self.user_name,
            room_name=self.room_name,
            message=message
        )
        self._call("SendMessage", request)
    
    def get_recent_messages(self):
        """Get recent messages from the current chatroom.
        
        Returns:
            List of recent messages
            
        Raises:
            RuntimeError: If not currently in a chatroom
        """
        if not self.room_name:
            error_msg = "Error: You must join a chatroom first"
            with self.output_lock:
                self.output_buffer.append(error_msg)
            raise RuntimeError(error_msg)
        
        request = chat_pb2.Client_GetRecentMessagesRequest(
            room_name=self.room_name,
            after_timestamp=self.last_msg_timestamp
        )
        response_any = self._call("GetRecentMessages", request)
        
        response = chat_pb2.Client_GetRecentMessagesResponse()
        response_any.Unpack(response)
        
        with self.output_lock:
            self.output_buffer.append(f"Recent messages in [{self.room_name}]:")
            for msg in response.messages:
                timestamp_str = time.strftime("%H:%M:%S", time.localtime(msg.timestamp))
                self.output_buffer.append(f"[{timestamp_str}] {msg.user_name}: {msg.message}")
                if msg.timestamp > self.last_msg_timestamp:
                    self.last_msg_timestamp = msg.timestamp
        
        return list(response.messages)
    
    def get_output(self):
        """Get the current output from the client.
        
        Returns:
            The accumulated output as a string
        """
        with self.output_lock:
            output = '\n'.join(self.output_buffer)
        return output
    
    def stop(self):
        """Stop the running client and return output."""
        self.running = False
        
        if self.stream:
            try:
                # Close the stream
                pass  # Can't really close a streaming RPC from client side
            except Exception:
                pass
        
        if self.stream_thread and self.stream_thread.is_alive():
            self.stream_thread.join(timeout=2)
        
        if self.channel:
            try:
                self.channel.close()
            except Exception:
                pass
        
        return self.get_output()
    
    def run_test(self, server_port, username='testuser', messages=None, timeout=30):
        """Run the chat client with a test sequence and return the output.
        
        Args:
            server_port: The server port to connect to
            username: Username for the chat client (default: testuser)
            messages: List of messages to send (default: standard test messages)
            timeout: Command timeout in seconds (default: 30)
            
        Returns:
            The output from the chat client as a string
        """
        if messages is None:
            messages = ['Hello from test!', 'This is message 2', 'Final test message']
        
        try:
            # Connect to the server
            server_address = f'localhost:{server_port}'
            self.channel = grpc.insecure_channel(server_address)
            self.stub = client_pb2_grpc.ClientServiceStub(self.channel)
            self.user_name = username
            
            # Register the client (this will block until connection is established)
            self.stream = self.stub.Register(client_pb2.Empty())
            
            # Read the first message which should be RegisterResponse
            first_msg = next(self.stream)
            reg_response = client_pb2.RegisterResponse()
            first_msg.Unpack(reg_response)
            self.client_id = reg_response.client_id
            
            print(f"Running chat client with test (connecting to localhost:{server_port})...")
            print(f"Client registered with ID: {self.client_id}")
            
            # Start background thread to listen for pushed messages
            self.running = True
            self.stream_thread = threading.Thread(target=self._listen_for_messages, daemon=True)
            self.stream_thread.start()
            
            # Give it a moment to start
            time.sleep(0.5)
            
            # Run the test sequence
            self.list_chatrooms()
            time.sleep(0.2)
            
            self.join_chatroom('General')
            time.sleep(0.2)
            
            # Send the provided messages
            for msg in messages:
                self.send_message(msg)
                time.sleep(0.2)
            
            self.get_recent_messages()
            time.sleep(0.2)
            
            # Add goodbye message
            with self.output_lock:
                self.output_buffer.append("Goodbye!")
            
            # Give time for any async operations to complete
            time.sleep(2)
            
            # Get the output
            output = self.get_output()
            
            print("\nChat client output:")
            print(output)
            
            return output
            
        except Exception as e:
            print(f"Error running test: {e}")
            import traceback
            traceback.print_exc()
            return f"Error: {e}"
            
        finally:
            self.stop()
    
    def verify_output(self, output, expected_patterns=None):
        """Verify chat client output contains expected patterns.
        
        Args:
            output: The output string to verify
            expected_patterns: List of (pattern, description) or (pattern, description, check_func) tuples
                             If not provided, uses default test expectations
            
        Returns:
            True if all patterns are found, False otherwise
        """
        if expected_patterns is None:
            # Default test expectations
            expected_patterns = [
                ("Available Chatrooms:", "/list command executed successfully"),
                ("General", "Chatroom 'General' listed"),
                ("Technology", "Chatroom 'Technology' listed"),
                ("Joined chatroom General", "Joined chatroom 'General'", 
                 lambda s: "Joined chatroom General" in s or "[General]" in s),
                ("Hello from test!", "Message 'Hello from test!' sent and received"),
                ("This is message 2", "Message 'This is message 2' sent and received"),
                ("Final test message", "Message 'Final test message' sent and received"),
                ("Recent messages in", "/messages command executed successfully"),
            ]
        
        print("\nVerifying test results...")
        
        all_passed = True
        for test_data in expected_patterns:
            if len(test_data) == 2:
                search_str, success_msg = test_data
                check_func = lambda s, search=search_str: search in s
            else:
                search_str, success_msg, check_func = test_data
            
            if check_func(output):
                print(f"✅ {success_msg}")
            else:
                print(f"❌ {success_msg} - not found")
                all_passed = False
        
        return all_passed
