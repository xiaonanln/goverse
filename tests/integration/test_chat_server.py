#!/usr/bin/env python3
"""
Unit tests for the ChatServer class in test_chat.py.
"""

import unittest
from unittest.mock import Mock, patch, MagicMock
import sys
from pathlib import Path

# Add the repo root to the path
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()
sys.path.insert(0, str(REPO_ROOT))

# Import grpc and proto modules
try:
    import grpc
    from proto import goverse_pb2
    from proto import goverse_pb2_grpc
except ImportError as e:
    print(f"Failed to import required modules: {e}")
    print("Run: ./script/compile-proto.sh")
    print("And: pip install grpcio grpcio-tools")
    sys.exit(1)

# Import the ChatServer class from test_chat
from tests.integration.test_chat import ChatServer


class TestChatServer(unittest.TestCase):
    """Test cases for the ChatServer class."""
    
    def setUp(self):
        """Set up test fixtures."""
        self.host = 'localhost'
        self.port = 47000
    
    @patch('grpc.insecure_channel')
    def test_init(self, mock_channel):
        """Test ChatServer initialization."""
        mock_channel_instance = MagicMock()
        mock_channel.return_value = mock_channel_instance
        
        server = ChatServer(self.host, self.port)
        
        self.assertEqual(server.host, self.host)
        self.assertEqual(server.port, self.port)
        mock_channel.assert_called_once_with(f'{self.host}:{self.port}')
        self.assertEqual(server.channel, mock_channel_instance)
    
    @patch('grpc.insecure_channel')
    def test_close(self, mock_channel):
        """Test ChatServer close method."""
        mock_channel_instance = MagicMock()
        mock_channel.return_value = mock_channel_instance
        
        server = ChatServer(self.host, self.port)
        server.close()
        
        mock_channel_instance.close.assert_called_once()
    
    @patch('grpc.insecure_channel')
    def test_context_manager(self, mock_channel):
        """Test ChatServer as context manager."""
        mock_channel_instance = MagicMock()
        mock_channel.return_value = mock_channel_instance
        
        with ChatServer(self.host, self.port) as server:
            self.assertIsNotNone(server)
        
        mock_channel_instance.close.assert_called_once()
    
    @patch('grpc.insecure_channel')
    def test_call_status_success(self, mock_channel):
        """Test successful call_status."""
        mock_channel_instance = MagicMock()
        mock_stub = MagicMock()
        mock_channel.return_value = mock_channel_instance
        
        # Mock the status response
        mock_response = MagicMock()
        mock_response.advertise_addr = 'localhost:47000'
        mock_response.num_objects = 5
        mock_response.uptime_seconds = 123
        mock_stub.Status.return_value = mock_response
        
        with patch.object(goverse_pb2_grpc, 'GoverseStub', return_value=mock_stub):
            server = ChatServer(self.host, self.port)
            result = server.call_status()
            
            # Verify the response is JSON formatted
            self.assertIn('"advertiseAddr"', result)
            self.assertIn('localhost:47000', result)
            self.assertIn('"numObjects"', result)
            self.assertIn('"5"', result)
            self.assertIn('"uptimeSeconds"', result)
            self.assertIn('"123"', result)
    
    @patch('grpc.insecure_channel')
    def test_call_status_rpc_error(self, mock_channel):
        """Test call_status with RPC error."""
        mock_channel_instance = MagicMock()
        mock_stub = MagicMock()
        mock_channel.return_value = mock_channel_instance
        
        # Mock RPC error
        rpc_error = grpc.RpcError()
        rpc_error.code = lambda: grpc.StatusCode.UNAVAILABLE
        rpc_error.details = lambda: "Service unavailable"
        mock_stub.Status.side_effect = rpc_error
        
        with patch.object(goverse_pb2_grpc, 'GoverseStub', return_value=mock_stub):
            server = ChatServer(self.host, self.port)
            result = server.call_status()
            
            # Verify error message format
            self.assertIn('Error: RPC failed', result)
    
    @patch('grpc.insecure_channel')
    def test_list_objects_success(self, mock_channel):
        """Test successful list_objects."""
        mock_channel_instance = MagicMock()
        mock_stub = MagicMock()
        mock_channel.return_value = mock_channel_instance
        
        # Mock the list objects response
        obj1 = MagicMock()
        obj1.id = 'ChatRoom-General'
        obj1.type = 'ChatRoom'
        
        obj2 = MagicMock()
        obj2.id = 'ChatRoom-Technology'
        obj2.type = 'ChatRoom'
        
        mock_response = MagicMock()
        mock_response.objects = [obj1, obj2]
        mock_stub.ListObjects.return_value = mock_response
        
        with patch.object(goverse_pb2_grpc, 'GoverseStub', return_value=mock_stub):
            server = ChatServer(self.host, self.port)
            result = server.list_objects()
            
            # Verify the response contains object information
            self.assertIn('"objectCount"', result)
            self.assertIn('2', result)
            self.assertIn('ChatRoom-General', result)
            self.assertIn('ChatRoom-Technology', result)
    
    @patch('grpc.insecure_channel')
    def test_list_objects_empty(self, mock_channel):
        """Test list_objects with no objects."""
        mock_channel_instance = MagicMock()
        mock_stub = MagicMock()
        mock_channel.return_value = mock_channel_instance
        
        # Mock empty response
        mock_response = MagicMock()
        mock_response.objects = []
        mock_stub.ListObjects.return_value = mock_response
        
        with patch.object(goverse_pb2_grpc, 'GoverseStub', return_value=mock_stub):
            server = ChatServer(self.host, self.port)
            result = server.list_objects()
            
            # Verify the response shows zero objects
            self.assertIn('"objectCount"', result)
            self.assertIn('0', result)
    
    @patch('grpc.insecure_channel')
    def test_call_method(self, mock_channel):
        """Test call_method for generic RPC calls."""
        mock_channel_instance = MagicMock()
        mock_stub = MagicMock()
        mock_channel.return_value = mock_channel_instance
        
        # Mock the call object response
        mock_response = MagicMock()
        mock_stub.CallObject.return_value = mock_response
        
        with patch.object(goverse_pb2_grpc, 'GoverseStub', return_value=mock_stub):
            server = ChatServer(self.host, self.port)
            
            # Create a real Any message instead of a mock
            from google.protobuf import any_pb2
            request = any_pb2.Any()
            
            result = server.call_method('object-id', 'MethodName', request)
            
            # Verify CallObject was called with correct parameters
            mock_stub.CallObject.assert_called_once()
            call_args = mock_stub.CallObject.call_args
            self.assertEqual(call_args[0][0].id, 'object-id')
            self.assertEqual(call_args[0][0].method, 'MethodName')
            self.assertEqual(call_args[0][0].request, request)
            self.assertEqual(result, mock_response)


if __name__ == '__main__':
    unittest.main()
