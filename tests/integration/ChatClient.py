#!/usr/bin/env python3
"""ChatClient helper for managing the chat client process."""
import os
import subprocess
import tempfile
import time
import threading
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
        self.process = None
        self.stdin_pipe = None
        self.output_file = None
        self.output_file_path = None
        
        # Build binary if needed
        if not os.path.exists(self.binary_path):
            if not BinaryHelper.build_binary('./samples/chat/client/', self.binary_path, 'chat client'):
                raise RuntimeError(f"Failed to build chat client binary at {self.binary_path}")
    
    def start_interactive(self, server_port, username='testuser'):
        """Start the chat client as an interactive background process.
        
        Args:
            server_port: The server port to connect to
            username: Username for the chat client (default: testuser)
            
        Returns:
            True if client started successfully, False otherwise
        """
        # Create temporary file for output (secure version)
        fd, self.output_file_path = tempfile.mkstemp(suffix='.txt', text=True)
        os.close(fd)  # Close the file descriptor, we'll open it for writing
        self.output_file = open(self.output_file_path, 'w')
        
        # Start the chat client process
        self.process = subprocess.Popen(
            [self.binary_path, '-server', f'localhost:{server_port}', '-user', username],
            stdin=subprocess.PIPE,
            stdout=self.output_file,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1  # Line buffered
        )
        self.stdin_pipe = self.process.stdin
        
        # Give it a moment to connect
        time.sleep(1)
        
        return self.process.poll() is None
    
    def send_input(self, text):
        """Send input to the running client process.
        
        Args:
            text: Text to send (will add newline automatically)
        """
        if self.stdin_pipe:
            self.stdin_pipe.write(text + '\n')
            self.stdin_pipe.flush()
    
    def get_output(self):
        """Get the current output from the client.
        
        Returns:
            The accumulated output as a string
        """
        if self.output_file:
            self.output_file.flush()
        
        if self.output_file_path and os.path.exists(self.output_file_path):
            with open(self.output_file_path, 'r') as f:
                return f.read()
        return ""
    
    def stop(self):
        """Stop the running client process and return output."""
        if self.process and self.process.poll() is None:
            self.send_input('/quit')
            time.sleep(0.5)
            if self.process.poll() is None:
                self.process.terminate()
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait()
        
        output = self.get_output()
        
        # Clean up
        if self.stdin_pipe:
            try:
                self.stdin_pipe.close()
            except Exception:
                pass
        if self.output_file:
            try:
                self.output_file.close()
            except Exception:
                pass
        if self.output_file_path:
            try:
                os.unlink(self.output_file_path)
            except Exception:
                pass
        
        return output
    
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
