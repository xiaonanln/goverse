#!/usr/bin/env python3
"""ChatClient helper for managing the chat client process."""
import os
import subprocess
import tempfile
from pathlib import Path
from BinaryHelper import BinaryHelper

# Repo root (tests/integration/ChatClient.py -> repo root)
REPO_ROOT = Path(__file__).parent.parent.parent.resolve()


class ChatClient:
    """Manages the Goverse chat client process and interactions."""
    
    def __init__(self, binary_path=None, base_cov_dir=None):
        """Initialize and optionally build the chat client.
        
        Args:
            binary_path: Path to chat client binary (defaults to /tmp/chat_client)
            build_if_needed: Whether to build the binary if it doesn't exist
            base_cov_dir: Base directory for coverage data (optional)
        """
        self.binary_path = binary_path if binary_path is not None else '/tmp/chat_client'
        self.base_cov_dir = base_cov_dir
        self.name = "Chat Client"
        
        # Build binary if needed
        if not os.path.exists(self.binary_path):
            if not BinaryHelper.build_binary('./samples/chat/client/', self.binary_path, 'chat client'):
                raise RuntimeError(f"Failed to build chat client binary at {self.binary_path}")
    
    def run_test(self, server_port, username='testuser', test_input=None, timeout=30):
        """Run the chat client with test input and return the output.
        
        Args:
            server_port: The server port to connect to
            username: Username for the chat client (default: testuser)
            test_input: Input commands as a string (default: basic test commands)
            timeout: Command timeout in seconds (default: 30)
            
        Returns:
            The output from the chat client as a string
        """
        if test_input is None:
            test_input = """/list
/join General
Hello from test!
This is message 2
Final test message
/messages
/quit
"""
        
        # Create temporary files for input and output
        with tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.txt') as input_file:
            input_file.write(test_input)
            input_file_path = input_file.name
        
        output_file_path = tempfile.mktemp(suffix='.txt')
        
        try:
            print(f"Running chat client with test input (connecting to localhost:{server_port})...")
            
            # Run the chat client with timeout
            with open(input_file_path, 'r') as stdin_file, \
                 open(output_file_path, 'w') as stdout_file:
                try:
                    # Child processes inherit GOCOVERDIR from the environment if set
                    subprocess.run(
                        [self.binary_path, '-server', f'localhost:{server_port}', '-user', username],
                        stdin=stdin_file, 
                        stdout=stdout_file, 
                        stderr=subprocess.STDOUT,
                        timeout=timeout
                    )
                except subprocess.TimeoutExpired:
                    print("Chat client timed out (this is expected)")
            
            # Read and return output
            with open(output_file_path, 'r') as f:
                output = f.read()
            
            print("\nChat client output:")
            print(output)
            
            return output
            
        finally:
            # Clean up temporary files
            try:
                os.unlink(input_file_path)
            except:
                pass
            try:
                os.unlink(output_file_path)
            except:
                pass
    
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
